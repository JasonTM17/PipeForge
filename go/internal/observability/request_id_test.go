package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewarePropagatesSafeHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set(RequestIDHeader, "client-request-1")
	recorder := httptest.NewRecorder()
	RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) != "client-request-1" {
			t.Fatalf("request ID was not stored in context")
		}
	})).ServeHTTP(recorder, request)
	if recorder.Header().Get(RequestIDHeader) != "client-request-1" {
		t.Fatalf("request ID was not returned")
	}
}

func TestRequestIDMiddlewareReplacesUnsafeHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set(RequestIDHeader, "contains spaces")
	recorder := httptest.NewRecorder()
	RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "contains spaces" || RequestID(r.Context()) == "" {
			t.Fatalf("unsafe request ID was not replaced")
		}
	})).ServeHTTP(recorder, request)
	if recorder.Header().Get(RequestIDHeader) == "contains spaces" {
		t.Fatalf("unsafe request ID was returned")
	}
}
