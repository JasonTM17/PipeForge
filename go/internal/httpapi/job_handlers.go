package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxJobRequestBytes = 64 * 1024

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
		r.Get("/v1/jobs", handlers.list)
		r.Get("/v1/jobs/{jobID}", handlers.get)
		r.Post("/v1/jobs/{jobID}/cancel", handlers.cancel)
		r.Post("/v1/jobs/{jobID}/retry", handlers.retry)
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

func decodeJobJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJobRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The job request body is invalid.", nil)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The request body must contain one JSON value.", nil)
		return false
	}
	return true
}

func decodeCancelJobRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return "", true
	}
	var request cancelJobRequest
	if !decodeJobJSON(w, r, &request) {
		return "", false
	}
	return request.Reason, true
}

func parseDatasetVersionID(r *http.Request) (uuid.UUID, error) {
	value := chi.URLParam(r, "datasetVersionID")
	versionID, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: dataset version ID must be a UUID", job.ErrInvalidInput)
	}
	return versionID, nil
}

func parseJobID(r *http.Request) (uuid.UUID, error) {
	jobID, err := uuid.Parse(chi.URLParam(r, "jobID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: job ID must be a UUID", job.ErrInvalidInput)
	}
	return jobID, nil
}

func parseJobPagination(r *http.Request) (int, int, error) {
	page, err := parsePositiveQueryInt(r, "page")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", job.ErrInvalidInput, err)
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", job.ErrInvalidInput, err)
	}
	if pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: pageSize must be between 1 and 100", job.ErrInvalidInput)
	}
	return page, pageSize, nil
}

func writeJobError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, job.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The job request is invalid.", nil)
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, job.ErrJobNotFound), errors.Is(err, job.ErrVersionNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The requested job resource was not found.", nil)
	case errors.Is(err, job.ErrIdempotencyConflict):
		WriteProblem(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "The idempotency key was reused with a different request.", nil)
	case errors.Is(err, job.ErrVersionUnavailable), errors.Is(err, job.ErrJobState), errors.Is(err, job.ErrInvalidTransition):
		WriteProblem(w, r, http.StatusConflict, "JOB_STATE_CONFLICT", "The job or dataset version is not available for this operation.", nil)
	case errors.Is(err, job.ErrQuotaExceeded):
		WriteProblem(w, r, http.StatusTooManyRequests, "JOB_QUOTA_EXCEEDED", "The job quota for this owner has been reached.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the job operation.", nil)
	}
}
