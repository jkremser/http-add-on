package queue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-logr/logr"
)

const countsPath = "/queue"

func AddCountsRoute(lggr logr.Logger, mux *http.ServeMux, q, eq CountReader) {
	lggr = lggr.WithName("pkg.queue.AddCountsRoute")
	lggr.Info("adding queue counts route", "path", countsPath)
	mux.Handle(countsPath, newSizeHandler(lggr, q, eq))
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
