package httpapi

import (
	"net/http"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/go-chi/chi/v5"
)

type jobHandlers struct {
	service *job.Service
}

type jobListResponse struct {
	Items    []job.Job `json:"items"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Total    int64     `json:"total"`
}

type cancelJobRequest struct {
	Reason string `json:"reason"`
}

func registerJobRoutes(router chi.Router, identityService *identity.Service, service *job.Service) {
	handlers := jobHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		r.Post("/v1/datasets/{datasetVersionID}/jobs", handlers.create)
		r.Post("/v1/dataset-versions/{datasetVersionID}/jobs", handlers.create)
		r.Post("/api/v1/datasets/{datasetVersionID}/jobs", handlers.create)
		r.Post("/api/v1/dataset-versions/{datasetVersionID}/jobs", handlers.create)
		r.Get("/v1/jobs", handlers.list)
		r.Get("/v1/jobs/{jobID}", handlers.get)
		r.Post("/v1/jobs/{jobID}/cancel", handlers.cancel)
		r.Post("/v1/jobs/{jobID}/retry", handlers.retry)
		r.Get("/api/v1/jobs", handlers.list)
		r.Get("/api/v1/jobs/{jobID}", handlers.get)
		r.Post("/api/v1/jobs/{jobID}/cancel", handlers.cancel)
		r.Post("/api/v1/jobs/{jobID}/retry", handlers.retry)
	})
}

func (h jobHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeJobError(w, r, authz.ErrUnauthenticated)
		return
	}
	var request job.CreateRequest
	if !decodeJobJSON(w, r, &request) {
		return
	}
	versionID, err := parseDatasetVersionID(r)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	created, replayed, err := h.service.Create(r.Context(), principal, versionID, request, r.Header.Get("Idempotency-Key"), requestID(r))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, created)
}

func (h jobHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeJobError(w, r, authz.ErrUnauthenticated)
		return
	}
	page, pageSize, err := parseJobPagination(r)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	result, err := h.service.List(r.Context(), principal, page, pageSize, r.URL.Query().Get("state"))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, jobListResponse{Items: result.Items, Page: page, PageSize: pageSize, Total: result.Total})
}

func (h jobHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeJobError(w, r, authz.ErrUnauthenticated)
		return
	}
	jobID, err := parseJobID(r)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), principal, jobID)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h jobHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeJobError(w, r, authz.ErrUnauthenticated)
		return
	}
	jobID, err := parseJobID(r)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	reason, ok := decodeCancelJobRequest(w, r)
	if !ok {
		return
	}
	item, err := h.service.Cancel(r.Context(), principal, jobID, reason, requestID(r))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h jobHandlers) retry(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeJobError(w, r, authz.ErrUnauthenticated)
		return
	}
	jobID, err := parseJobID(r)
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	item, err := h.service.Retry(r.Context(), principal, jobID, requestID(r))
	if err != nil {
		writeJobError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
