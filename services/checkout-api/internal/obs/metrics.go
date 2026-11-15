package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns the service's Prometheus registry and instruments HTTP routes.
// Label values are bounded (method, route pattern, status code) to keep
// cardinality predictable.
type Metrics struct {
	registry       *prometheus.Registry
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	inFlight       prometheus.Gauge
	ordersCreated  prometheus.Counter
	ordersRejected *prometheus.CounterVec
}

const serviceName = "checkout-api"

// NewMetrics builds a registry with Go runtime metrics, process metrics, and
// build metadata.
func NewMetrics(version, commit string) *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	serviceLabels := prometheus.Labels{"service": serviceName}

	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "http_requests_total",
			Help:        "Total number of HTTP requests handled.",
			ConstLabels: serviceLabels,
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "http_request_duration_seconds",
			Help:        "HTTP request latency in seconds.",
			Buckets:     prometheus.ExponentialBuckets(0.005, 2, 11),
			ConstLabels: serviceLabels,
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "http_requests_in_flight",
			Help:        "Current number of HTTP requests being served.",
			ConstLabels: serviceLabels,
		}),
		ordersCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "orders_created_total",
			Help:        "Total number of orders successfully created.",
			ConstLabels: serviceLabels,
		}),
		ordersRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "orders_rejected_total",
			Help:        "Total number of order creation requests rejected, by reason.",
			ConstLabels: serviceLabels,
		}, []string{"reason"}),
	}

	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "build_info",
		Help: "Build metadata; always 1.",
		ConstLabels: prometheus.Labels{
			"service": serviceName,
			"version": version,
			"commit":  commit,
		},
	})
	buildInfo.Set(1)

	registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
		m.ordersCreated,
		m.ordersRejected,
		buildInfo,
	)

	return m
}

// Handler exposes the registry in OpenMetrics format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

var uninstrumentedRoutes = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
	"/drainz":  {},
	"/metrics": {},
}

// Middleware records request count and latency for business routes. Probes and
// the scrape endpoint are excluded from SLO math on purpose.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := "unmatched"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		if _, skip := uninstrumentedRoutes[route]; skip {
			return
		}

		m.requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// OrderCreated records a successful create.
func (m *Metrics) OrderCreated() {
	m.ordersCreated.Inc()
}

// OrderRejected records a rejected create. reason must be a bounded constant.
func (m *Metrics) OrderRejected(reason string) {
	switch reason {
	case "invalid", "conflict", "error":
		m.ordersRejected.WithLabelValues(reason).Inc()
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// Unwrap allows http.ResponseController access to the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
