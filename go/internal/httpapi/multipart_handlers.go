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
	"github.com/JasonTM17/PipeForge/go/internal/multipart"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxMultipartJSONRequestBytes = 2 * 1024 * 1024

type multipartHandlers struct {
	service *multipart.Service
}

type initiateMultipartRequest struct {
	Filename               string `json:"filename"`
	ContentType            string `json:"contentType"`
	ExpectedSize           int64  `json:"expectedSize"`
	ExpectedChecksumSHA256 string `json:"expectedChecksumSha256,omitempty"`
	IdempotencyKey         string `json:"idempotencyKey,omitempty"`
}

type registerMultipartPartRequest struct {
	ETag      string `json:"etag"`
	SizeBytes int64  `json:"sizeBytes"`
}

func registerMultipartRoutes(router chi.Router, identityService *identity.Service, service *multipart.Service) {
	handlers := multipartHandlers{service: service}
	router.Group(func(r chi.Router) {
		r.Use(identityAuthMiddleware(identityService))
		r.Post("/v1/datasets/{datasetID}/uploads", handlers.initiate)
		r.Get("/v1/uploads/{sessionID}", handlers.get)
		r.Get("/v1/uploads/{sessionID}/parts", handlers.listParts)
		r.Post("/v1/uploads/{sessionID}/parts/{partNumber}/presign", handlers.presignPart)
		r.Put("/v1/uploads/{sessionID}/parts/{partNumber}", handlers.registerPart)
		r.Post("/v1/uploads/{sessionID}/complete", handlers.complete)
		r.Post("/v1/uploads/{sessionID}/abort", handlers.abort)
	})
}

func (h multipartHandlers) initiate(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	datasetID, err := parseDatasetID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	var request initiateMultipartRequest
	if !decodeMultipartJSON(w, r, &request) {
		return
	}
	session, err := h.service.Initiate(r.Context(), principal, datasetID, multipart.InitiateRequest{
		Filename: request.Filename, ContentType: request.ContentType, ExpectedSize: request.ExpectedSize,
		ExpectedChecksum: request.ExpectedChecksumSHA256, IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (h multipartHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	detail, err := h.service.Get(r.Context(), principal, sessionID)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h multipartHandlers) listParts(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	parts, err := h.service.ListParts(r.Context(), principal, sessionID)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": parts})
}

func (h multipartHandlers) presignPart(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	partNumber, err := parseMultipartPartNumber(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	partURL, err := h.service.PresignPart(r.Context(), principal, sessionID, partNumber)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, partURL)
}

func (h multipartHandlers) registerPart(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	partNumber, err := parseMultipartPartNumber(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	var request registerMultipartPartRequest
	if !decodeMultipartJSON(w, r, &request) {
		return
	}
	part, err := h.service.RegisterPart(r.Context(), principal, sessionID, partNumber, request.ETag, request.SizeBytes)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, part)
}

func (h multipartHandlers) complete(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	var request multipart.CompleteRequest
	if !decodeMultipartJSON(w, r, &request) {
		return
	}
	version, err := h.service.Complete(r.Context(), principal, sessionID, request)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (h multipartHandlers) abort(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeMultipartError(w, r, authz.ErrUnauthenticated)
		return
	}
	sessionID, err := parseMultipartSessionID(r)
	if err != nil {
		writeMultipartError(w, r, err)
		return
	}
	if err := h.service.Abort(r.Context(), principal, sessionID); err != nil {
		writeMultipartError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeMultipartJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartJSONRequestBytes)
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

func parseMultipartSessionID(r *http.Request) (uuid.UUID, error) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: sessionID must be a UUID", multipart.ErrInvalidInput)
	}
	return sessionID, nil
}

func parseMultipartPartNumber(r *http.Request) (int, error) {
	value := strings.TrimSpace(chi.URLParam(r, "partNumber"))
	partNumber, err := strconv.Atoi(value)
	if err != nil || partNumber < 1 {
		return 0, fmt.Errorf("%w: partNumber must be a positive integer", multipart.ErrInvalidInput)
	}
	return partNumber, nil
}

func writeMultipartError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, multipart.ErrInvalidInput):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The multipart request is invalid.", nil)
	case errors.Is(err, multipart.ErrUploadTooLarge):
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "The upload exceeds the configured size limit.", nil)
	case errors.Is(err, multipart.ErrSizeMismatch):
		WriteProblem(w, r, http.StatusBadRequest, "UPLOAD_SIZE_MISMATCH", "The upload part or object size does not match the request metadata.", nil)
	case errors.Is(err, multipart.ErrChecksumMismatch):
		WriteProblem(w, r, http.StatusBadRequest, "CHECKSUM_MISMATCH", "The upload checksum does not match.", nil)
	case errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, multipart.ErrSessionNotFound), errors.Is(err, multipart.ErrPartNotFound), errors.Is(err, dataset.ErrDatasetNotFound):
		WriteProblem(w, r, http.StatusNotFound, "NOT_FOUND", "The requested upload resource was not found.", nil)
	case errors.Is(err, multipart.ErrSessionExpired):
		WriteProblem(w, r, http.StatusConflict, "SESSION_EXPIRED", "The upload session has expired.", nil)
	case errors.Is(err, multipart.ErrSessionCompleted):
		WriteProblem(w, r, http.StatusConflict, "SESSION_COMPLETED", "The upload session is already completed.", nil)
	case errors.Is(err, multipart.ErrSessionState), errors.Is(err, multipart.ErrSessionInProgress), errors.Is(err, multipart.ErrIdempotencyConflict), errors.Is(err, multipart.ErrPartConflict), errors.Is(err, dataset.ErrVersionState):
		WriteProblem(w, r, http.StatusConflict, "RESOURCE_STATE_CONFLICT", "The upload session cannot be changed in its current state.", nil)
	case errors.Is(err, multipart.ErrRemoteStorage), errors.Is(err, dataset.ErrObjectStorage):
		WriteProblem(w, r, http.StatusBadGateway, "STORAGE_ERROR", "The upload could not be stored or verified.", nil)
	case errors.Is(err, dataset.ErrDatasetDeleted):
		WriteProblem(w, r, http.StatusConflict, "RESOURCE_STATE_CONFLICT", "The dataset is not available for this operation.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the request.", nil)
	}
}
