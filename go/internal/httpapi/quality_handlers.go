package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/quality"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxQualityRuleRequestBytes = 64 * 1024

type qualityHandlers struct {
	service *quality.Service
}

type createQualityRuleRequest struct {
	DatasetID     uuid.UUID        `json:"datasetId"`
	Name          string           `json:"name"`
	ColumnScope   []string         `json:"columnScope"`
	RuleType      quality.RuleType `json:"ruleType"`
	Configuration map[string]any   `json:"configuration"`
	Severity      quality.Severity `json:"severity"`
	Enabled       *bool            `json:"enabled"`
}

type patchQualityRuleRequest struct {
	Name          *string           `json:"name"`
	ColumnScope   *[]string         `json:"columnScope"`
	RuleType      *quality.RuleType `json:"ruleType"`
	Configuration *map[string]any   `json:"configuration"`
	Severity      *quality.Severity `json:"severity"`
	Enabled       *bool             `json:"enabled"`
}

type qualityRuleListResponse struct {
	Items    []quality.Rule `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int64          `json:"total"`
}

func registerQualityRoutes(router chi.Router, identityService *identity.Service, service *quality.Service) {
	handlers := qualityHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		for _, prefix := range []string{"/v1", "/api/v1"} {
			r.Post(prefix+"/quality-rules", handlers.create)
			r.Get(prefix+"/quality-rules", handlers.list)
			r.Get(prefix+"/quality-rules/{ruleID}", handlers.get)
			r.Patch(prefix+"/quality-rules/{ruleID}", handlers.update)
			r.Delete(prefix+"/quality-rules/{ruleID}", handlers.delete)
			r.Post(prefix+"/datasets/{datasetID}/quality-rules", handlers.create)
			r.Get(prefix+"/datasets/{datasetID}/quality-rules", handlers.list)
		}
	})
}

func (h qualityHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeQualityError(w, r, authz.ErrUnauthenticated)
		return
	}
	var request createQualityRuleRequest
	if !decodeQualityJSON(w, r, &request) {
		return
	}
	pathDatasetID, err := optionalQualityDatasetID(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	if pathDatasetID != nil {
		if request.DatasetID != uuid.Nil && request.DatasetID != *pathDatasetID {
			writeQualityError(w, r, fmt.Errorf("%w: datasetId does not match the path", quality.ErrInvalidInput))
			return
		}
		request.DatasetID = *pathDatasetID
	}
	created, err := h.service.Create(r.Context(), principal, quality.CreateRequest{
		DatasetID: request.DatasetID, Name: request.Name, ColumnScope: request.ColumnScope,
		RuleType: request.RuleType, Configuration: request.Configuration,
		Severity: request.Severity, Enabled: request.Enabled,
	})
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h qualityHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeQualityError(w, r, authz.ErrUnauthenticated)
		return
	}
	datasetID, err := qualityDatasetFilter(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	page, pageSize, err := parseQualityPagination(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	result, err := h.service.List(r.Context(), principal, datasetID, page, pageSize)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, qualityRuleListResponse{Items: result.Items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
}

func (h qualityHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeQualityError(w, r, authz.ErrUnauthenticated)
		return
	}
	ruleID, err := parseQualityRuleID(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), principal, ruleID)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h qualityHandlers) update(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeQualityError(w, r, authz.ErrUnauthenticated)
		return
	}
	ruleID, err := parseQualityRuleID(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	var request patchQualityRuleRequest
	if !decodeQualityJSON(w, r, &request) {
		return
	}
	updated, err := h.service.Update(r.Context(), principal, ruleID, quality.UpdateRequest{
		Name: request.Name, ColumnScope: request.ColumnScope, RuleType: request.RuleType,
		Configuration: request.Configuration, Severity: request.Severity, Enabled: request.Enabled,
	})
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h qualityHandlers) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeQualityError(w, r, authz.ErrUnauthenticated)
		return
	}
	ruleID, err := parseQualityRuleID(r)
	if err != nil {
		writeQualityError(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), principal, ruleID); err != nil {
		writeQualityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeQualityJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxQualityRuleRequestBytes)
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

func optionalQualityDatasetID(r *http.Request) (*uuid.UUID, error) {
	value := strings.TrimSpace(chi.URLParam(r, "datasetID"))
	if value == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%w: datasetID must be a UUID", quality.ErrInvalidInput)
	}
	return &parsed, nil
}

func qualityDatasetFilter(r *http.Request) (*uuid.UUID, error) {
	pathID, err := optionalQualityDatasetID(r)
	if err != nil {
		return nil, err
	}
	queryValue := strings.TrimSpace(r.URL.Query().Get("datasetId"))
	if queryValue == "" {
		return pathID, nil
	}
	queryID, err := uuid.Parse(queryValue)
	if err != nil {
		return nil, fmt.Errorf("%w: datasetId must be a UUID", quality.ErrInvalidInput)
	}
	if pathID != nil && *pathID != queryID {
		return nil, fmt.Errorf("%w: datasetId does not match the path", quality.ErrInvalidInput)
	}
	return &queryID, nil
}

func parseQualityRuleID(r *http.Request) (uuid.UUID, error) {
	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: ruleID must be a UUID", quality.ErrInvalidInput)
	}
	return ruleID, nil
}

func parseQualityPagination(r *http.Request) (int, int, error) {
	page, err := parsePositiveQueryInt(r, "page")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", quality.ErrInvalidInput, err)
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", quality.ErrInvalidInput, err)
	}
	if pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: pageSize must be between 1 and 100", quality.ErrInvalidInput)
	}
	return page, pageSize, nil
}

func writeQualityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, quality.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The quality rule request is invalid.", nil)
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, quality.ErrRuleNameUsed):
		WriteProblem(w, r, http.StatusConflict, "QUALITY_RULE_NAME_USED", "A quality rule with this name already exists for the dataset.", nil)
	case errors.Is(err, quality.ErrRuleNotFound), errors.Is(err, dataset.ErrDatasetNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The requested quality rule resource was not found.", nil)
	case errors.Is(err, dataset.ErrDatasetDeleted):
		WriteProblem(w, r, http.StatusConflict, "RESOURCE_STATE_CONFLICT", "The dataset is not available for this operation.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the request.", nil)
	}
}
