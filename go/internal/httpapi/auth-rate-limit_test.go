package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthRateLimiterRejectsBurstWithProblemResponse(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	limiter, err := NewAuthRateLimiter(AuthRateLimitConfig{
		RequestsPerMinute: 60, Burst: 2, MaxClients: 10, EntryTTL: time.Minute,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewAuthRateLimiter returned error: %v", err)
	}
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for attempt := 0; attempt < 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		request.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt < 2 && response.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status=%d", attempt, response.Code)
		}
		if attempt == 2 {
			if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "1" {
				t.Fatalf("rate-limited response status=%d retry=%q", response.Code, response.Header().Get("Retry-After"))
			}
			var problem Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || problem.Code != "RATE_LIMITED" {
				t.Fatalf("unexpected problem response: err=%v problem=%+v", err, problem)
			}
		}
	}
}

func TestAuthRateLimiterIsolatesClientsAndRefills(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	limiter, err := NewAuthRateLimiter(AuthRateLimitConfig{
		RequestsPerMinute: 60, Burst: 1, MaxClients: 10, EntryTTL: time.Minute,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _ := limiter.allow("192.0.2.1"); !allowed {
		t.Fatal("first client should be allowed")
	}
	if allowed, _ := limiter.allow("192.0.2.1"); allowed {
		t.Fatal("exhausted client should be rejected")
	}
	if allowed, _ := limiter.allow("192.0.2.2"); !allowed {
		t.Fatal("second client should have an independent bucket")
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.allow("192.0.2.1"); !allowed {
		t.Fatal("client should refill after one second")
	}
}

func TestAuthRateLimiterBoundsAndExpiresClientState(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	limiter, err := NewAuthRateLimiter(AuthRateLimitConfig{
		RequestsPerMinute: 1, Burst: 1, MaxClients: 2, EntryTTL: time.Minute,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	limiter.allow("client-a")
	now = now.Add(time.Second)
	limiter.allow("client-b")
	limiter.allow("client-c")
	if len(limiter.clients) != 2 || limiter.clients["client-a"] != nil {
		t.Fatalf("expected oldest client eviction, clients=%v", limiter.clients)
	}
	now = now.Add(2 * time.Minute)
	limiter.allow("client-d")
	if len(limiter.clients) != 1 || limiter.clients["client-d"] == nil {
		t.Fatalf("expected expired entries removed, clients=%v", limiter.clients)
	}
}

func TestAuthRateLimiterRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewAuthRateLimiter(AuthRateLimitConfig{}); err == nil {
		t.Fatal("expected invalid configuration error")
	}
}

func TestClientAddressDoesNotTrustForwardedHeaders(t *testing.T) {
	if got := clientAddress("[2001:db8::1]:443"); got != "2001:db8::1" {
		t.Fatalf("unexpected IPv6 address: %q", got)
	}
	if got := clientAddress("192.0.2.1:1234"); got != "192.0.2.1" {
		t.Fatalf("unexpected IPv4 address: %q", got)
	}
}
