package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxDatasetMetadataRequestBytes = 64 * 1024

type datasetHandlers struct {
	service *dataset.Service
}

type createDatasetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type datasetListResponse struct {
	Items    []dataset.Dataset `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	Total    int64             `json:"total"`
}

type datasetDetailResponse struct {
	Dataset  dataset.Dataset          `json:"dataset"`
	Versions []dataset.DatasetVersion `json:"versions"`
}

func registerDatasetRoutes(router chi.Router, identityService *identity.Service, service *dataset.Service) {
	handlers := datasetHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		r.Post("/v1/datasets", handlers.create)
		r.Get("/v1/datasets", handlers.list)
		r.Get("/v1/datasets/{datasetID}", handlers.get)
		r.Delete("/v1/datasets/{datasetID}", handlers.delete)
		r.Post("/v1/datasets/{datasetID}/versions", handlers.uploadVersion)
	})
}

func (h datasetHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDatasetError(w, r, authz.ErrUnauthenticated)
		return
	}
	var request createDatasetRequest
	if !decodeDatasetJSON(w, r, &request) {
		return
	}
	created, err := h.service.Create(r.Context(), principal, request.Name, request.Description)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h datasetHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDatasetError(w, r, authz.ErrUnauthenticated)
		return
	}
	page, pageSize, err := parseDatasetPagination(r)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	result, err := h.service.List(r.Context(), principal, page, pageSize, r.URL.Query().Get("state"), r.URL.Query().Get("name"))
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, datasetListResponse{Items: result.Items, Page: page, PageSize: pageSize, Total: result.Total})
}

func (h datasetHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDatasetError(w, r, authz.ErrUnauthenticated)
		return
	}
	datasetID, err := parseDatasetID(r)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	item, versions, err := h.service.Get(r.Context(), principal, datasetID)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, datasetDetailResponse{Dataset: item, Versions: versions})
}

func (h datasetHandlers) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDatasetError(w, r, authz.ErrUnauthenticated)
		return
	}
	datasetID, err := parseDatasetID(r)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), principal, datasetID); err != nil {
		writeDatasetError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h datasetHandlers) uploadVersion(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDatasetError(w, r, authz.ErrUnauthenticated)
		return
	}
	datasetID, err := parseDatasetID(r)
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	filename := strings.TrimSpace(r.Header.Get("X-Filename"))
	if filename == "" {
		writeDatasetError(w, r, fmt.Errorf("%w: X-Filename is required", dataset.ErrInvalidInput))
		return
	}
	defer r.Body.Close()
	version, err := h.service.UploadVersion(r.Context(), principal, datasetID, dataset.UploadRequest{
		Filename:         filename,
		ContentType:      r.Header.Get("Content-Type"),
		ExpectedChecksum: r.Header.Get("X-Checksum-SHA256"),
		ContentLength:    r.ContentLength,
		Body:             r.Body,
	})
	if err != nil {
		writeDatasetError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func decodeDatasetJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxDatasetMetadataRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The request body is invalid.", nil)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The request body must contain one JSON value.", nil)
		return false
	}
	return true
}

func parseDatasetID(r *http.Request) (uuid.UUID, error) {
	datasetID, err := uuid.Parse(chi.URLParam(r, "datasetID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: datasetID must be a UUID", dataset.ErrInvalidInput)
	}
	return datasetID, nil
}

func parseDatasetPagination(r *http.Request) (int, int, error) {
	page, err := parsePositiveQueryInt(r, "page")
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize")
	if err != nil {
		return 0, 0, err
	}
	if pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: pageSize must be between 1 and 100", dataset.ErrInvalidInput)
	}
	return page, pageSize, nil
}

func parsePositiveQueryInt(r *http.Request, name string) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", dataset.ErrInvalidInput, name)
	}
	return parsed, nil
}

func writeDatasetError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, dataset.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid.", nil)
	case errors.Is(err, dataset.ErrUploadTooLarge):
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "The upload exceeds the configured size limit.", nil)
	case errors.Is(err, dataset.ErrUploadSizeMismatch):
		WriteProblem(w, r, http.StatusBadRequest, "UPLOAD_SIZE_MISMATCH", "The upload size does not match the request metadata.", nil)
	case errors.Is(err, dataset.ErrChecksumMismatch):
		WriteProblem(w, r, http.StatusBadRequest, "CHECKSUM_MISMATCH", "The upload checksum does not match.", nil)
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, dataset.ErrDatasetNameUsed):
		WriteProblem(w, r, http.StatusConflict, "DATASET_NAME_USED", "A dataset with this name already exists for the owner.", nil)
	case errors.Is(err, dataset.ErrDatasetNotFound), errors.Is(err, dataset.ErrVersionNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The requested dataset resource was not found.", nil)
	case errors.Is(err, dataset.ErrDatasetDeleted), errors.Is(err, dataset.ErrVersionState):
		WriteProblem(w, r, http.StatusConflict, "RESOURCE_STATE_CONFLICT", "The dataset resource is not available for this operation.", nil)
	case errors.Is(err, dataset.ErrObjectStorage):
		WriteProblem(w, r, http.StatusBadGateway, "STORAGE_ERROR", "The upload could not be stored.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the request.", nil)
	}
}
