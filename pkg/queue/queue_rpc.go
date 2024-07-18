package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-logr/logr"
	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"
)

type activationHandler interface {
	OnActivationCheck(ctx context.Context, ns, name string, activate bool) error
}

const (
	countsPath     = "/queue"
	activationPath = "/activate"
)

func AddCountsRoute(lggr logr.Logger, mux *http.ServeMux, q, eq CountReader) {
	lggr = lggr.WithName("pkg.queue.AddCountsRoute")
	lggr.Info("adding queue counts route", "path", countsPath)
	mux.Handle(countsPath, newSizeHandler(lggr, q, eq))
}

func AddActivationRoute(lggr logr.Logger, mux *http.ServeMux, envoycp activationHandler) {
	lggr = lggr.WithName("pkg.queue.AddActivationRoute")
	lggr.Info("adding queue activation route", "path", activationPath)
	parametrizedPath := fmt.Sprintf("%s/namespace/{namespace}/name/{name}", activationPath)
	mux.Handle(parametrizedPath, newActivationHandler(lggr, envoycp))
}

// newForwardingHandler takes in the service URL for the app backend
// and forwards incoming requests to it. Note that it isn't multitenant.
// It's intended to be deployed and scaled alongside the application itself
func newSizeHandler(
	lggr logr.Logger,
	q, eq CountReader,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// q contains the internal queue counters and metrics
		cur, err := q.Current()
		if err != nil {
			lggr.Error(err, "getting queue size")
			w.WriteHeader(500)
			if _, err := w.Write([]byte(
				"error getting queue size",
			)); err != nil {
				lggr.Error(
					err,
					"could not send error message to client",
				)
			}
			return
		}

		// eq contains the external queue counters and metrics pushed from
		// external reverse proxy or load balancer that offloads the data
		// path from the interceptor
		externalCur, _ := eq.Current()
		externalHosts := externalCur.Counts
		for k, v := range externalHosts {
			if c, ok := cur.Counts[k]; ok {
				c.Concurrency += v.Concurrency
				c.RPS += v.RPS
				cur.Counts[k] = c
			} else {
				cur.Counts[k] = v
			}
		}
		if err := json.NewEncoder(w).Encode(cur); err != nil {
			lggr.Error(err, "encoding QueueCounts")
			w.WriteHeader(500)
			if _, err := w.Write([]byte(
				"error encoding queue counts",
			)); err != nil {
				lggr.Error(
					err,
					"could not send error message to client",
				)
			}
			return
		}
	})
}

// newActivationHandler returns an http.Handler that activates or deactivates envoy routing through the interceptor
// for scale to zero, envoy fleet has to send the first request to the interceptor to activate the application
func newActivationHandler(lggr logr.Logger, envoycp activationHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := r.PathValue("namespace")
		name := r.PathValue("name")
		q := r.URL.Query()
		activate := q.Get("activate")
		if ns == "" || name == "" || activate == "" {
			w.WriteHeader(http.StatusBadRequest)
			if _, err := w.Write([]byte("namespace, name path parameters and activate query parameter are required")); err != nil {
				lggr.Error(err, "could not send error message to client")
			}
			return
		}

		var msg string
		if err := envoycp.OnActivationCheck(r.Context(), ns, name, activate == "true"); err != nil {
			lggr.Error(err, "failed to forward activation check")
			w.WriteHeader(http.StatusInternalServerError)
			msg = fmt.Sprintf("failed to forward activation check: %v", err)
		} else {
			lggr.V(4).Info("activation check forwarded")
			w.WriteHeader(http.StatusOK)
			msg = "activation check forwarded"
		}
		if _, err := w.Write([]byte(msg)); err != nil {
			lggr.Error(err, "failed to respond to client")
		}
	})
}

// GetQueueCounts issues an RPC call to get the queue counts
// from the given hostAndPort. Note that the hostAndPort should
// not end with a "/" and shouldn't include a path.
func GetCounts(
	httpCl *http.Client,
	interceptorURL url.URL,
) (*Counts, error) {
	interceptorURL.Path = countsPath
	resp, err := httpCl.Get(interceptorURL.String())
	if err != nil {
		return nil, fmt.Errorf("requesting the queue counts from %s: %w", interceptorURL.String(), err)
	}
	defer resp.Body.Close()
	counts := NewCounts()
	if err := json.NewDecoder(resp.Body).Decode(counts); err != nil {
		return nil, fmt.Errorf("decoding response from the interceptor at %s: %w", interceptorURL.String(), err)
	}

	return counts, nil
}

// ForwardIsActive forwards the IsActive call from KEDA through the scaler to a particular interceptor
func ForwardIsActive(httpCl *http.Client, interceptorURL url.URL, so *externalscaler.ScaledObjectRef, activate bool) error {
	interceptorURL.Path = fmt.Sprintf("%v/namespace/%v/name/%v", activationPath, so.Namespace, so.Name)
	queryParams := url.Values{}
	queryParams.Add("activate", fmt.Sprintf("%v", activate))
	interceptorURL.RawQuery = queryParams.Encode()
	resp, err := httpCl.Get(interceptorURL.String())
	if err != nil {
		return fmt.Errorf("requesting the interceptor at %s: %w", interceptorURL.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code %d from the interceptor at %s", resp.StatusCode, interceptorURL.String())
	}
	return nil
}
