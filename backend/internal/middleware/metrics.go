package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsPath is the Prometheus scrape target.
const MetricsPath = "/metrics"

// Registry is this process's collector registry.
//
// A dedicated registry rather than prometheus.DefaultRegisterer: the default is
// global mutable state, so a dependency that registers a colliding metric name
// panics the process at init. An explicit registry also keeps tests from
// leaking series into one another.
var Registry = prometheus.NewRegistry()

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "idp_http_requests_total",
			Help: "Total HTTP requests by method, route and status code.",
		},
		// Labelled by route rather than raw path — see routeLabel.
		[]string{"method", "route", "status"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "idp_http_request_duration_seconds",
			Help: "HTTP request latency by method and route.",
			// Default buckets top out at 10s, which is useless here: log
			// streaming and port-forward setup are legitimately slower, and
			// everything above 10s would collapse into +Inf.
			Buckets: []float64{0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"method", "route"},
	)

	inFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "idp_http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		},
	)
)

func init() {
	Registry.MustRegister(
		requestsTotal,
		requestDuration,
		inFlight,
		// Go runtime and process metrics come free and answer the first
		// question of any incident: was the process itself healthy?
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// MetricsHandler serves the Prometheus scrape endpoint.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		// A broken collector should not take the endpoint down; report what
		// can be gathered rather than returning nothing.
		ErrorHandling: promhttp.ContinueOnError,
	})
}

// Metrics records request counts, latency, and concurrency.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Scraping itself is not interesting and would otherwise dominate the
		// request count on an idle deployment.
		if r.URL.Path == MetricsPath {
			next.ServeHTTP(w, r)
			return
		}

		route := routeLabel(r.URL.Path)
		start := time.Now()

		inFlight.Inc()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		inFlight.Dec()

		requestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(wrapped.statusCode)).Inc()
	})
}

// routeLabel collapses a path into a bounded label value.
//
// Cardinality is the whole point. /apps/{namespace}/{name} and
// /webhooks/git/{uuid} embed user-controlled identifiers; labelling by raw path
// would mint a new time series per namespace, per workload, per repository —
// which is how a Prometheus server runs out of memory. Every dynamic segment is
// therefore folded into one stable label.
func routeLabel(path string) string {
	switch {
	case strings.HasPrefix(path, "/apps/"):
		return "/apps/*"
	case strings.HasPrefix(path, "/webhooks/git"):
		return "/webhooks/git/*"
	case strings.HasPrefix(path, "/idp.v1."):
		// Connect procedures are already a closed set, so the full name is
		// safe and far more useful than a single "/rpc" bucket.
		return path
	case strings.HasPrefix(path, "/api/"):
		return path
	case path == "/healthz" || path == "/readyz":
		return path
	default:
		return "other"
	}
}
