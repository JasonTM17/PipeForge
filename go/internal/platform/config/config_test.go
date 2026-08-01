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
}

func TestLoadParsesOverrides(t *testing.T) {
	values := map[string]string{
		"PIPEFORGE_ENV":                "test",
		"PIPEFORGE_HTTP_ADDR":          "127.0.0.1:9091",
		"POSTGRES_PORT":                "55433",
		"PIPEFORGE_DATABASE_MAX_CONNS": "4",
		"PIPEFORGE_SHUTDOWN_TIMEOUT":   "3s",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DatabasePort != 55433 || cfg.DatabaseMaxConns != 4 || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected overrides: %+v", cfg)
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
