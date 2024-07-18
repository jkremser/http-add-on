package envoycp

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/go-logr/logr"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workqueue "k8s.io/client-go/util/workqueue"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	runtimeservice "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	secretservice "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/log"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"

	envoysink "github.com/kedacore/http-add-on/interceptor/envoysink"
	httpaddonv1alpha1 "github.com/kedacore/http-add-on/operator/apis/http/v1alpha1"
	clientset "github.com/kedacore/http-add-on/operator/generated/clientset/versioned"
	"github.com/kedacore/http-add-on/pkg/k8s"
)

const (
	// NodeID is the node ID for the Envoy snapshot belonging to the kedify-proxy
	NodeID = "kedify-proxy"
)

// Options are the options for the Envoy control plane server
type Options struct {
	ClusterDomain     string
	ControlPlaneHost  string
	ControlPlanePort  uint32
	InterceptorHost   string
	InterceptorPort   uint32
	ConnectionTimeout time.Duration
}

// Server is the interface for the Envoy control plane server
type Server interface {
	Run(ctx context.Context) error
	HandleHSO(ctx context.Context, hso *httpaddonv1alpha1.HTTPScaledObject)
	OnActivationCheck(ctx context.Context, namespace, name string, active bool) error
}

// server is the implementation of the Server interface
type server struct {
	logger    logr.Logger
	cache     cachev3.SnapshotCache
	queue     workqueue.RateLimitingInterface
	k8sClient clientset.Interface
	epCache   k8s.EndpointsCache
	cp        Options
}

// NewServer creates a new instance of the Server interface
func NewServer(l logr.Logger, k8sClient clientset.Interface, epCache k8s.EndpointsCache, cp Options) Server {
	logAdapter := log.LoggerFuncs{
		WarnFunc:  func(f string, args ...any) { l.Info(fmt.Sprintf("[WARN]  "+f, args...)) },
		ErrorFunc: func(f string, args ...any) { l.Info(fmt.Sprintf("[ERROR] "+f, args...)) },
	}
	if l.V(1).Enabled() {
		logAdapter.InfoFunc = func(f string, args ...any) { l.Info(fmt.Sprintf("[INFO]  "+f, args...)) }
	}
	if l.V(2).Enabled() {
		logAdapter.DebugFunc = func(f string, args ...any) { l.Info(fmt.Sprintf("[DEBUG] "+f, args...)) }
	}
	return &server{
		cp:        cp,
		cache:     cachev3.NewSnapshotCache(false, cachev3.IDHash{}, logAdapter),
		logger:    l,
		queue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "envoy-control-plane"),
		k8sClient: k8sClient,
		epCache:   epCache,
	}
}

// loggingUnaryInterceptor is optional gRPC endpoint log for single requests
func (s *server) loggingUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	s.logger.V(4).Info("received unary request", "method", info.FullMethod)
	return handler(ctx, req)
}

// loggingStreamInterceptor is optional gRPC endpoint log for streaming requests
func (s *server) loggingStreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	s.logger.V(4).Info("received stream request", "method", info.FullMethod)
	return handler(srv, ss)
}

// Run starts the Envoy control plane server
func (s *server) Run(ctx context.Context) error {
	s.runWorkqueue(ctx)
	srv3 := serverv3.NewServer(ctx, s.cache, nil)
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(s.loggingUnaryInterceptor),
		grpc.StreamInterceptor(s.loggingStreamInterceptor),
	)
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.cp.ControlPlanePort))
	if err != nil {
		return err
	}

	reflection.Register(grpcServer)
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv3)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv3)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv3)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv3)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv3)
	secretservice.RegisterSecretDiscoveryServiceServer(grpcServer, srv3)
	runtimeservice.RegisterRuntimeDiscoveryServiceServer(grpcServer, srv3)

	return grpcServer.Serve(lis)
}

// HandleHSO adds the HTTPScaledObject to the workqueue for further processing and forming envoy snapshot cache
func (s *server) HandleHSO(ctx context.Context, hso *httpaddonv1alpha1.HTTPScaledObject) {
	s.logger.V(4).Info("adding HSO to workqueue", "namespace", hso.Namespace, "name", hso.Name)
	s.queue.Add(hso)
}

// OnActivationCheck is called when the interceptor has checked the application's activation status before it
// sends the result back to KEDA, each interceptor should adjust the envoy fleet routing to interceptor if the
// application is getting deactivated
func (s *server) OnActivationCheck(ctx context.Context, namespace, name string, active bool) error {
	s.logger.V(4).Info("handling activation check", "namespace", namespace, "name", name, "active", active)
	cacheSnap, err := s.cache.GetSnapshot(NodeID)
	if err != nil { // only returned when there is no snapshot
		return nil
	}
	snap, ok := cacheSnap.(*cachev3.Snapshot)
	if !ok {
		return fmt.Errorf("failed to cast snapshot to cachev3.Snapshot")
	}
	hso, err := s.k8sClient.HttpV1alpha1().HTTPScaledObjects(namespace).Get(ctx, name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		s.logger.V(4).Info("HSO not found, skipping activation", "namespace", namespace, "name", name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get HTTPScaledObject: %w", err)
	}
	return s.updateSnapshot(snap, hso, active)
}

// handleHSO updates the envoy fleet configuration in snapshot cache and sets the metric aggregation
// key in HTTPScaledObject annotation
func (s *server) handleHSO(ctx context.Context, hso *httpaddonv1alpha1.HTTPScaledObject) error {
	s.logger.V(4).Info("handling HSO", "namespace", hso.Namespace, "name", hso.Name)
	hso.Annotations[envoysink.EnvoyClusterNameAnnotation] = fmt.Sprintf("%s/%s", hso.Namespace, hso.Name)
	cacheSnap, err := s.cache.GetSnapshot(NodeID)
	if err != nil {
		if err := s.initNewSnapshot(ctx); err != nil {
			return fmt.Errorf("failed to init envoy snapshot: %w", err)
		}
		return nil
	}
	snap, ok := cacheSnap.(*cachev3.Snapshot)
	if !ok {
		return fmt.Errorf("failed to cast snapshot to cachev3.Snapshot")
	}
	active, err := s.isActive(ctx, *hso)
	if err != nil {
		return fmt.Errorf("failed to check if HTTPScaledObject is active: %w", err)
	}
	if err := s.updateSnapshot(snap, hso, active); err != nil {
		return fmt.Errorf("failed to update snapshot: %w", err)
	}
	return nil
}

// runWorkqueue runs the workqueue for handling HTTPScaledObjects
func (s *server) runWorkqueue(ctx context.Context) {
	go func() {
		for {
			obj, shutdown := s.queue.Get()
			if shutdown {
				return
			}
			defer s.queue.Done(obj)
			hso, ok := obj.(*httpaddonv1alpha1.HTTPScaledObject)
			if !ok {
				s.logger.Info("invalid object type in workqueue", "type", fmt.Sprintf("%T", obj))
				s.queue.Forget(obj)
				continue
			}
			if err := s.handleHSO(ctx, hso); err != nil {
				s.logger.Error(err, "failed to handle HTTPScaledObject for envoy cache update", "namespace", hso.Namespace, "name", hso.Name)
				s.queue.AddRateLimited(obj)
			} else {
				s.queue.Forget(obj)
			}
		}
	}()
}

// updateSnapshot updates the snapshot with the new HTTPScaledObject
func (s *server) updateSnapshot(snap *cachev3.Snapshot, hso *httpaddonv1alpha1.HTTPScaledObject, active bool) error {
	resources := make(map[resource.Type][]types.Resource)
	resources[resource.ListenerType] = maps.Values(snap.GetResources(resource.ListenerType))
	resources[resource.ClusterType] = maps.Values(snap.GetResources(resource.ClusterType))

	if err := addResources(resources, s.cp, *hso); err != nil {
		return fmt.Errorf("failed to add resources: %w", err)
	}
	if !active {
		setupColdStart(resources, s.cp, *hso)
	}
	version := getResourcesVersion(resources)
	newSnap, err := cachev3.NewSnapshot(version, resources)
	if err != nil {
		return fmt.Errorf("failed to create new snapshot with updated resources: %w", err)
	}
	if snapVersion(snap) == snapVersion(newSnap) {
		s.logger.V(1).Info("snapshot version is the same, skipping update", "version", version, "nodeID", NodeID)
		return nil
	}

	s.logger.V(1).Info("setting new snapshot", "version", version, "nodeID", NodeID)
	if err := s.cache.SetSnapshot(context.Background(), NodeID, newSnap); err != nil {
		return fmt.Errorf("failed to set snapshot: %w", err)
	}
	return nil
}

// initNewSnapshot initializes a new snapshot when there is no existing snapshot in the cache
func (s *server) initNewSnapshot(ctx context.Context) error {
	hsoList, err := s.k8sClient.HttpV1alpha1().HTTPScaledObjects("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list HTTPScaledObjects: %w", err)
	}
	resources := make(map[resource.Type][]types.Resource)
	if err := addResources(resources, s.cp, hsoList.Items...); err != nil {
		return fmt.Errorf("failed to add resources: %w", err)
	}

	for _, hso := range hsoList.Items {
		active, err := s.isActive(ctx, hso)
		if err != nil {
			s.logger.Error(err, "failed to check if HTTPScaledObject is active", "namespace", hso.Namespace, "name", hso.Name)
		}
		if !active {
			setupColdStart(resources, s.cp, hso)
		}
	}

	version := getResourcesVersion(resources)
	snap, err := cachev3.NewSnapshot(version, resources)
	if err != nil {
		return fmt.Errorf("failed to create new snapshot: %w", err)
	}

	s.logger.V(1).Info("setting new snapshot", "version", version, "nodeID", NodeID)
	if err := s.cache.SetSnapshot(ctx, NodeID, snap); err != nil {
		return fmt.Errorf("failed to set snapshot: %w", err)
	}
	return nil
}

// isActive checks if the HTTPScaledObject is active from the endpoints cache
func (s *server) isActive(ctx context.Context, hso httpaddonv1alpha1.HTTPScaledObject) (bool, error) {
	endpoints, err := s.epCache.Get(hso.Namespace, hso.Spec.ScaleTargetRef.Service)
	if kerrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get endpoints: %w", err)
	}
	return len(endpoints.Subsets) > 0, nil
}
