package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Serving is configuration for how the interceptor serves the proxy
// and admin server
type Serving struct {
	// CurrentNamespace is the namespace that the interceptor is
	// currently running in
	CurrentNamespace string `envconfig:"KEDA_HTTP_CURRENT_NAMESPACE" required:"true"`
	// WatchNamespace is the namespace to watch for new HTTPScaledObjects.
	// Leave this empty to watch HTTPScaledObjects in all namespaces.
	WatchNamespace string `envconfig:"KEDA_HTTP_WATCH_NAMESPACE" default:""`
	// ProxyHost is the host that the public proxy should run on
	ProxyHost string `envconfig:"KEDA_HTTP_PROXY_HOST" required:"true" default:"keda-add-ons-http-interceptor-proxy"`
	// ProxyPort is the port that the public proxy should run on
	ProxyPort int `envconfig:"KEDA_HTTP_PROXY_PORT" required:"true"`
	// AdminPort is the port that the internal admin server should run on.
	// This is the server that the external scaler will issue metrics
	// requests to
	AdminPort int `envconfig:"KEDA_HTTP_ADMIN_PORT" required:"true"`
	// EnvoyStatsMetricSinkPort is the port that the external envoy proxies can send metrics to.
	EnvoyStatsMetricSinkPort int `envconfig:"KEDA_HTTP_ENVOY_STATS_METRIC_SINK_PORT" default:"9901"`
	// EnvoyControlPlaneServiceName is the name of the service for the envoy control plane
	EnvoyControlPlaneServiceName string `envconfig:"KEDA_HTTP_ENVOY_CONTROL_PLANE_SERVICE_NAME" default:"keda-add-ons-http-interceptor-kedify-proxy-metric-sink"`
	// EnvoyControlPlanePort is the port that the envoy control plane server should run on.
	EnvoyControlPlanePort int `envconfig:"KEDA_HTTP_ENVOY_CONTROL_PLANE_PORT" default:"5678"`
	// ClusterDomain is the domain that the interceptor should for internal DNS
	ClusterDomain string `envconfig:"KEDA_HTTP_CLUSTER_DOMAIN" default:"cluster.local"`
	// ConfigMapCacheRsyncPeriod is the time interval
	// for the configmap informer to rsync the local cache.
	ConfigMapCacheRsyncPeriod time.Duration `envconfig:"KEDA_HTTP_SCALER_CONFIG_MAP_INFORMER_RSYNC_PERIOD" default:"60m"`
	// Deprecated: The interceptor has an internal process that periodically fetches the state
	// of deployment that is running the servers it forwards to.
	//
	// This is the interval (in milliseconds) representing how often to do a fetch
	DeploymentCachePollIntervalMS int `envconfig:"KEDA_HTTP_DEPLOYMENT_CACHE_POLLING_INTERVAL_MS" default:"250"`
	// The interceptor has an internal process that periodically fetches the state
	// of endpoints that is running the servers it forwards to.
	//
	// This is the interval (in milliseconds) representing how often to do a fetch
	EndpointsCachePollIntervalMS int `envconfig:"KEDA_HTTP_ENDPOINTS_CACHE_POLLING_INTERVAL_MS" default:"250"`
	// ProxyTLSEnabled is a flag to specify whether the interceptor proxy should
	// be running using a TLS enabled server
	ProxyTLSEnabled bool `envconfig:"KEDA_HTTP_PROXY_TLS_ENABLED" default:"false"`
	// TLSCertPath is the path to read the certificate file from for the TLS server
	TLSCertPath string `envconfig:"KEDA_HTTP_PROXY_TLS_CERT_PATH" default:"/certs/tls.crt"`
	// TLSKeyPath is the path to read the private key file from for the TLS server
	TLSKeyPath string `envconfig:"KEDA_HTTP_PROXY_TLS_KEY_PATH" default:"/certs/tls.key"`
	// TLSPort is the port that the server should serve on if TLS is enabled
	TLSPort int `envconfig:"KEDA_HTTP_PROXY_TLS_PORT" default:"8443"`
}

// Parse parses standard configs using envconfig and returns a pointer to the
// newly created config. Returns nil and a non-nil error if parsing failed
func MustParseServing() *Serving {
	ret := new(Serving)
	envconfig.MustProcess("", ret)
	return ret
}
