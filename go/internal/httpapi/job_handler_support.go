package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxJobRequestBytes = 64 * 1024

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
	case errors.Is(err, job.ErrJobNotFound), errors.Is(err, job.ErrVersionNotFound), errors.Is(err, dataset.ErrDatasetNotFound), errors.Is(err, dataset.ErrVersionNotFound):
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
