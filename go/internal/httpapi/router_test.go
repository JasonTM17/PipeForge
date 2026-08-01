package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/observability"
)

func TestLiveAndMetricsEndpoints(t *testing.T) {
	metrics := observability.NewMetrics()
	router := NewRouter(Dependencies{Metrics: metrics})

	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || live.Header().Get(observability.RequestIDHeader) == "" {
		t.Fatalf("unexpected live response: %d headers=%v", live.Code, live.Header())
	}

	metricsResponse := httptest.NewRecorder()
	router.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || metricsResponse.Body.Len() == 0 {
		t.Fatalf("unexpected metrics response: %d", metricsResponse.Code)
	}
}

func TestReadinessReturnsDependencyFailure(t *testing.T) {
	router := NewRouter(Dependencies{Readiness: func(context.Context) error { return errors.New("database unavailable") }})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
}
