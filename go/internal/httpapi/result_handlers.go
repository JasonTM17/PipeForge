package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/result"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var artifactKindPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

type resultHandlers struct {
	service *result.Service
}

type artifactListResponse struct {
	Items    []result.Artifact `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	Total    int64             `json:"total"`
}

func registerResultRoutes(router chi.Router, identityService *identity.Service, service *result.Service) {
	handlers := resultHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		for _, prefix := range []string{"/v1", "/api/v1"} {
			r.Get(prefix+"/artifacts", handlers.list)
			r.Get(prefix+"/artifacts/{artifactID}", handlers.get)
			r.Get(prefix+"/artifacts/{artifactID}/download", handlers.download)
			r.Head(prefix+"/artifacts/{artifactID}/download", handlers.download)
			r.Get(prefix+"/jobs/{jobID}/artifacts", handlers.list)
		}
	})
}

func (h resultHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeResultError(w, r, authz.ErrUnauthenticated)
		return
	}
	jobID, err := optionalArtifactJobID(r)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	page, pageSize, err := parseArtifactPagination(r)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && !artifactKindPattern.MatchString(kind) {
		writeResultError(w, r, fmt.Errorf("%w: invalid artifact kind", result.ErrInvalidInput))
		return
	}
	resultPage, err := h.service.ListArtifacts(r.Context(), principal, jobID, kind, page, pageSize)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactListResponse{Items: resultPage.Items, Page: resultPage.Page, PageSize: resultPage.PageSize, Total: resultPage.Total})
}

func (h resultHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeResultError(w, r, authz.ErrUnauthenticated)
		return
	}
	artifactID, err := parseArtifactID(r)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	item, err := h.service.GetArtifact(r.Context(), principal, artifactID)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h resultHandlers) download(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeResultError(w, r, authz.ErrUnauthenticated)
		return
	}
	if r.Header.Get("Range") != "" {
		WriteProblem(w, r, http.StatusRequestedRangeNotSatisfiable, "RANGE_NOT_SUPPORTED", "Range downloads are not supported by this endpoint.", nil)
		return
	}
	artifactID, err := parseArtifactID(r)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	item, reader, err := h.service.OpenArtifact(r.Context(), principal, artifactID)
	if err != nil {
		writeResultError(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", item.SizeBytes))
	w.Header().Set("Content-Disposition", `attachment; filename="`+item.ID.String()+`"`)
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.CopyN(w, reader, item.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		return
	}
}

func optionalArtifactJobID(r *http.Request) (*uuid.UUID, error) {
	value := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if value == "" {
		value = strings.TrimSpace(r.URL.Query().Get("jobId"))
	}
	if value == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%w: jobId must be a UUID", result.ErrInvalidInput)
	}
	return &parsed, nil
}

func parseArtifactID(r *http.Request) (uuid.UUID, error) {
	parsed, err := uuid.Parse(chi.URLParam(r, "artifactID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: artifactID must be a UUID", result.ErrInvalidInput)
	}
	return parsed, nil
}

func parseArtifactPagination(r *http.Request) (int, int, error) {
	page, err := parsePositiveQueryInt(r, "page")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: page must be a positive integer", result.ErrInvalidInput)
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize")
	if err != nil || pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: pageSize must be between 1 and 100", result.ErrInvalidInput)
	}
	return page, pageSize, nil
}

func writeResultError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, result.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The artifact request is invalid.", nil)
	case errors.Is(err, result.ErrArtifactNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The requested artifact was not found.", nil)
	case errors.Is(err, result.ErrArtifactTooLarge):
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, "ARTIFACT_TOO_LARGE", "The artifact exceeds the download limit.", nil)
	case errors.Is(err, result.ErrArtifactUnavailable):
		WriteProblem(w, r, http.StatusBadGateway, "ARTIFACT_UNAVAILABLE", "The artifact is temporarily unavailable.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the artifact request.", nil)
	}
}
