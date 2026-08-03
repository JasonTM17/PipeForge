package httpapi

import (
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var ErrInvalidAuthRateLimitConfig = errors.New("invalid authentication rate-limit configuration")

type AuthRateLimitConfig struct {
	RequestsPerMinute int
	Burst             int
	MaxClients        int
	EntryTTL          time.Duration
	Now               func() time.Time
}

type authRateBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

// AuthRateLimiter is a process-local token bucket for public credential routes.
// It intentionally keys the TCP peer address and does not trust forwarded headers.
type AuthRateLimiter struct {
	mu         sync.Mutex
	rate       float64
	burst      float64
	maxClients int
	entryTTL   time.Duration
	now        func() time.Time
	clients    map[string]*authRateBucket
}

func NewAuthRateLimiter(cfg AuthRateLimitConfig) (*AuthRateLimiter, error) {
	if cfg.RequestsPerMinute < 1 || cfg.Burst < 1 || cfg.MaxClients < 1 || cfg.EntryTTL <= 0 {
		return nil, ErrInvalidAuthRateLimitConfig
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AuthRateLimiter{
		rate:       float64(cfg.RequestsPerMinute) / float64(time.Minute/time.Second),
		burst:      float64(cfg.Burst),
		maxClients: cfg.MaxClients,
		entryTTL:   cfg.EntryTTL,
		now:        now,
		clients:    make(map[string]*authRateBucket),
	}, nil
}

func (l *AuthRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := l.allow(clientAddress(r.RemoteAddr))
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
			WriteProblem(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Too many authentication attempts. Try again later.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *AuthRateLimiter) allow(client string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.clients[client]
	if bucket == nil {
		l.removeExpired(now)
		if len(l.clients) >= l.maxClients {
			l.removeOldest()
		}
		bucket = &authRateBucket{tokens: l.burst, lastRefill: now, lastSeen: now}
		l.clients[client] = bucket
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	if elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.lastRefill = now
	}
	bucket.lastSeen = now
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	return false, time.Duration(math.Ceil((1 - bucket.tokens) / l.rate * float64(time.Second)))
}

func (l *AuthRateLimiter) removeExpired(now time.Time) {
	for client, bucket := range l.clients {
		if now.Sub(bucket.lastSeen) >= l.entryTTL {
			delete(l.clients, client)
		}
	}
}

func (l *AuthRateLimiter) removeOldest() {
	var oldestClient string
	var oldest time.Time
	for client, bucket := range l.clients {
		if oldestClient == "" || bucket.lastSeen.Before(oldest) {
			oldestClient = client
			oldest = bucket.lastSeen
		}
	}
	if oldestClient != "" {
		delete(l.clients, oldestClient)
	}
}

func clientAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return "unknown"
	}
	return remoteAddr
}
