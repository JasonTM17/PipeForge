package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dlq"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type dlqHandlers struct{ service *dlq.Service }

func registerDLQRoutes(router chi.Router, identityService *identity.Service, service *dlq.Service) {
	handlers := dlqHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		for _, prefix := range []string{"/v1", "/api/v1"} {
			r.Get(prefix+"/dead-letters", handlers.list)
			r.Post(prefix+"/dead-letters/{recordID}/replay", handlers.replay)
		}
	})
}

func (h dlqHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDLQError(w, r, authz.ErrUnauthenticated)
		return
	}
	page, pageSize, err := parseDLQPagination(r)
	if err != nil {
		writeDLQError(w, r, err)
		return
	}
	openOnly := true
	if raw := strings.TrimSpace(r.URL.Query().Get("openOnly")); raw != "" {
		openOnly, err = strconv.ParseBool(raw)
		if err != nil {
			writeDLQError(w, r, fmt.Errorf("%w: openOnly must be true or false", dlq.ErrInvalidInput))
			return
		}
	}
	result, err := h.service.List(r.Context(), principal, page, pageSize, openOnly)
	if err != nil {
		writeDLQError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h dlqHandlers) replay(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeDLQError(w, r, authz.ErrUnauthenticated)
		return
	}
	recordID, err := uuid.Parse(chi.URLParam(r, "recordID"))
	if err != nil {
		writeDLQError(w, r, fmt.Errorf("%w: recordID must be a UUID", dlq.ErrInvalidInput))
		return
	}
	result, err := h.service.Replay(r.Context(), principal, dlq.ReplayCommand{RecordID: recordID, RequestID: requestID(r), TraceID: requestID(r)})
	if err != nil {
		writeDLQError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseDLQPagination(r *http.Request) (int, int, error) {
	page, err := parsePositiveQueryInt(r, "page")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: page must be positive", dlq.ErrInvalidInput)
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize")
	if err != nil || pageSize > dlq.MaxPageSize {
		return 0, 0, fmt.Errorf("%w: pageSize must be between 1 and %d", dlq.ErrInvalidInput, dlq.MaxPageSize)
	}
	return page, pageSize, nil
}

func writeDLQError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, dlq.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The dead-letter request is invalid.", nil)
	case errors.Is(err, dlq.ErrNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The dead-letter record was not found.", nil)
	case errors.Is(err, dlq.ErrAlreadyReplayed), errors.Is(err, dlq.ErrJobState):
		WriteProblem(w, r, http.StatusConflict, "DLQ_STATE_CONFLICT", "The dead-letter record cannot be replayed in its current state.", nil)
	case errors.Is(err, dlq.ErrQuotaExceeded):
		WriteProblem(w, r, http.StatusTooManyRequests, "JOB_QUOTA_EXCEEDED", "The job quota for this owner has been reached.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the dead-letter operation.", nil)
	}
}
