package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec
	HTTPErrors   *prometheus.CounterVec
	HTTPLatency  *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		Registry: registry,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pipeforge",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests handled by the control plane.",
		}, []string{"method", "route", "status"}),
		HTTPErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "pipeforge",
			Subsystem: "http",
			Name:      "errors_total",
			Help:      "Total HTTP responses with status code 400 or higher.",
		}, []string{"method", "route", "status"}),
		HTTPLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "pipeforge",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
		}, []string{"method", "route"}),
	}
	registry.MustRegister(metrics.HTTPRequests, metrics.HTTPErrors, metrics.HTTPLatency)
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(recorder.status)
		m.HTTPRequests.WithLabelValues(r.Method, route, status).Inc()
		if recorder.status >= http.StatusBadRequest {
			m.HTTPErrors.WithLabelValues(r.Method, route, status).Inc()
		}
		m.HTTPLatency.WithLabelValues(r.Method, route).Observe(time.Since(started).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}
