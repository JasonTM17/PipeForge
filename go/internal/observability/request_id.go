package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

const RequestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(requestID) {
			requestID = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(raw[:])
}
