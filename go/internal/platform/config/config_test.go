package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.HTTPAddr != defaultHTTPAddr || cfg.DatabasePort != 5432 || cfg.DatabaseMaxConns != defaultMaxConnections {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ShutdownTimeout != defaultShutdown {
		t.Fatalf("unexpected shutdown timeout: %s", cfg.ShutdownTimeout)
	}
	if cfg.AccessTokenTTL != defaultAccessTokenTTL || cfg.RefreshTokenTTL != defaultRefreshTokenTTL || cfg.JWTSigningKey == "" {
		t.Fatalf("unexpected identity defaults: %+v", cfg)
	}
}

func TestLoadParsesOverrides(t *testing.T) {
	values := map[string]string{
		"PIPEFORGE_ENV":                "test",
		"PIPEFORGE_HTTP_ADDR":          "127.0.0.1:9091",
		"POSTGRES_PORT":                "55433",
		"PIPEFORGE_DATABASE_MAX_CONNS": "4",
		"PIPEFORGE_SHUTDOWN_TIMEOUT":   "3s",
		"PIPEFORGE_ACCESS_TOKEN_TTL":   "10m",
		"PIPEFORGE_REFRESH_TOKEN_TTL":  "48h",
		"JWT_SIGNING_KEY":              "test-jwt-signing-key-with-32-bytes!!",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DatabasePort != 55433 || cfg.DatabaseMaxConns != 4 || cfg.ShutdownTimeout != 3*time.Second || cfg.AccessTokenTTL != 10*time.Minute || cfg.RefreshTokenTTL != 48*time.Hour {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestLoadRejectsShortJWTSigningKey(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "JWT_SIGNING_KEY" {
			return "too-short"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "JWT_SIGNING_KEY") {
		t.Fatalf("expected JWT key validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidHTTPAddress(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "PIPEFORGE_HTTP_ADDR" {
			return "localhost"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "PIPEFORGE_HTTP_ADDR") {
		t.Fatalf("expected HTTP address validation error, got %v", err)
	}
}

func TestLoadRejectsNonNumericHTTPPort(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "PIPEFORGE_HTTP_ADDR" {
			return ":http"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "positive numeric port") {
		t.Fatalf("expected HTTP port validation error, got %v", err)
	}
}

func TestDatabaseDSNDoesNotChangeConfiguredValues(t *testing.T) {
	cfg, err := Load(func(key string) string {
		if key == "POSTGRES_PASSWORD" {
			return "local-only"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(cfg.DatabaseDSN(), "postgres://pipeforge:local-only@localhost:5432/pipeforge") {
		t.Fatalf("unexpected DSN: %s", cfg.DatabaseDSN())
	}
}
