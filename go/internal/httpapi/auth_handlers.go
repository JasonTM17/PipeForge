package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/observability"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxIdentityRequestBytes = 1 << 20

type identityHandlers struct {
	service *identity.Service
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type createAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type userResponse struct {
	ID     uuid.UUID `json:"id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	Scopes []string  `json:"scopes"`
}

type tokenResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	TokenType    string       `json:"tokenType"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	User         userResponse `json:"user"`
}

type createdAPIKeyResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
	APIKey    string    `json:"apiKey"`
}

type apiKeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

func registerIdentityRoutes(router chi.Router, service *identity.Service, limiter *AuthRateLimiter) {
	handlers := identityHandlers{service: service}
	router.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			if limiter != nil {
				r.Use(limiter.Middleware)
			}
			r.Post("/auth/register", handlers.register)
			r.Post("/auth/login", handlers.login)
			r.Post("/auth/refresh", handlers.refresh)
		})
		r.Post("/auth/logout", handlers.logout)
		r.Group(func(r chi.Router) {
			r.Use(identityAuthMiddleware(service))
			r.Post("/api-keys", handlers.createAPIKey)
			r.Get("/api-keys", handlers.listAPIKeys)
			r.Delete("/api-keys/{keyID}", handlers.revokeAPIKey)
		})
	})
}

func (h identityHandlers) register(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeIdentityJSON(w, r, &request) {
		return
	}
	user, pair, err := h.service.Register(r.Context(), request.Email, request.Password, requestID(r))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokenResponseFrom(user, pair))
}

func (h identityHandlers) login(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeIdentityJSON(w, r, &request) {
		return
	}
	user, pair, err := h.service.Login(r.Context(), request.Email, request.Password, requestID(r))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponseFrom(user, pair))
}

func (h identityHandlers) refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if !decodeIdentityJSON(w, r, &request) {
		return
	}
	user, pair, err := h.service.Refresh(r.Context(), request.RefreshToken, requestID(r))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponseFrom(user, pair))
}

func (h identityHandlers) logout(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if !decodeIdentityJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.RefreshToken) == "" {
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "refreshToken is required.", nil)
		return
	}
	if err := h.service.Logout(r.Context(), request.RefreshToken, requestID(r)); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h identityHandlers) createAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	var request createAPIKeyRequest
	if !decodeIdentityJSON(w, r, &request) {
		return
	}
	created, err := h.service.CreateAPIKey(r.Context(), principal, request.Name, request.Scopes, requestID(r))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createdAPIKeyResponse{ID: created.Key.ID, Name: created.Key.Name, Prefix: created.Key.Prefix, Scopes: created.Key.Scopes, CreatedAt: created.Key.CreatedAt, APIKey: created.Plaintext})
}

func (h identityHandlers) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	keys, err := h.service.ListAPIKeys(r.Context(), principal)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	response := make([]apiKeyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, apiKeyResponse{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Scopes: key.Scopes, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt, RevokedAt: key.RevokedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": response})
}

func (h identityHandlers) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "keyID"))
	if err != nil {
		WriteProblem(w, r, http.StatusNotFound, "API_KEY_NOT_FOUND", "The API key was not found.", nil)
		return
	}
	if err := h.service.RevokeAPIKey(r.Context(), principal, keyID, requestID(r)); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeIdentityJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxIdentityRequestBytes)
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

func tokenResponseFrom(user identity.User, pair identity.TokenPair) tokenResponse {
	return tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    pair.AccessTokenExpiry,
		User:         userResponse{ID: user.ID, Email: user.Email, Role: string(user.Role), Scopes: append([]string(nil), user.Scopes...)},
	}
}

func requestID(r *http.Request) string {
	return observability.RequestID(r.Context())
}

func writeIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput), errors.Is(err, authz.ErrInvalidScope):
		WriteProblem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid.", nil)
	case errors.Is(err, identity.ErrEmailAlreadyExists):
		WriteProblem(w, r, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "The email is already registered.", nil)
	case errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrRefreshTokenInvalid), errors.Is(err, identity.ErrRefreshTokenReplay), errors.Is(err, identity.ErrRefreshTokenExpired), errors.Is(err, identity.ErrAPIKeyInvalid), errors.Is(err, authz.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
	case errors.Is(err, authz.ErrForbidden):
		WriteProblem(w, r, http.StatusForbidden, "FORBIDDEN", "The authenticated principal is not allowed to perform this operation.", nil)
	case errors.Is(err, identity.ErrAPIKeyNotFound):
		WriteProblem(w, r, http.StatusNotFound, "API_KEY_NOT_FOUND", "The API key was not found.", nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The server could not complete the request.", nil)
	}
}
