package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/observability"
	"github.com/go-chi/chi/v5"
)

type Dependencies struct {
	Logger    *slog.Logger
	Metrics   *observability.Metrics
	Readiness func(context.Context) error
	Identity  *identity.Service
	Dataset   *dataset.Service
}

func NewRouter(deps Dependencies) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Metrics == nil {
		deps.Metrics = observability.NewMetrics()
	}
	if deps.Readiness == nil {
		deps.Readiness = func(context.Context) error { return nil }
	}

	router := chi.NewRouter()
	router.Use(observability.RequestIDMiddleware)
	router.Use(deps.Metrics.Middleware)
	router.Use(recoverMiddleware(deps.Logger))
	router.Get("/health/live", liveHandler)
	router.Get("/health/ready", readyHandler(deps.Readiness))
	router.Handle("/metrics", deps.Metrics.Handler())
	if deps.Identity != nil {
		registerIdentityRoutes(router, deps.Identity)
		if deps.Dataset != nil {
			registerDatasetRoutes(router, deps.Identity, deps.Dataset)
		}
	}
	return router
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyHandler(check func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			WriteProblem(w, r, http.StatusServiceUnavailable, "NOT_READY", "A required dependency is not ready.", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.ErrorContext(r.Context(), "http handler panic", "error", recovered)
					WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the request.", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
