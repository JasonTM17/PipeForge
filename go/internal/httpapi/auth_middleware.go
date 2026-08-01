package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
)

type principalContextKey struct{}

func identityAuthMiddleware(service *identity.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer, apiKey, ok := extractCredentials(r)
			if !ok {
				WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
				return
			}
			var principal auth.Principal
			var err error
			if bearer != "" {
				principal, err = service.AuthenticateBearer(r.Context(), bearer)
			} else {
				principal, err = service.AuthenticateAPIKey(r.Context(), apiKey)
			}
			if err != nil {
				WriteProblem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
				return
			}
			ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func principalFromRequest(r *http.Request) (auth.Principal, bool) {
	principal, ok := r.Context().Value(principalContextKey{}).(auth.Principal)
	return principal, ok && principal.UserID != [16]byte{}
}

func extractCredentials(r *http.Request) (string, string, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if authorization != "" && apiKey != "" {
		return "", "", false
	}
	if apiKey != "" {
		return "", apiKey, true
	}
	if authorization == "" {
		return "", "", false
	}
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
		return "", "", false
	}
	return parts[1], "", true
}
