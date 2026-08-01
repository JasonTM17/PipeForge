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
	if cfg.MinIOEndpoint == "" || cfg.MinIOPublicEndpoint == "" || cfg.MinIOAccessKey == "" || cfg.MinIOSecretKey == "" || cfg.RabbitMQURL != defaultDevelopmentRabbitMQURL || cfg.DatasetBucket != defaultDatasetBucket || cfg.MaxUploadBytes != defaultMaxUploadBytes || cfg.MultipartPartSize != defaultMultipartPartSize || cfg.MultipartMaxParts != defaultMultipartMaxParts || cfg.MultipartMaxBytes != defaultMultipartMaxBytes || cfg.MultipartSessionTTL != defaultMultipartSessionTTL || cfg.MultipartURLTTL != defaultMultipartURLTTL || cfg.MultipartCompletionGrace != defaultMultipartCompletionGrace || cfg.MultipartCleanupLimit != defaultMultipartCleanupLimit {
		t.Fatalf("unexpected storage defaults: %+v", cfg)
	}
}

func TestLoadParsesOverrides(t *testing.T) {
	values := map[string]string{
		"PIPEFORGE_ENV":                        "test",
		"PIPEFORGE_HTTP_ADDR":                  "127.0.0.1:9091",
		"POSTGRES_PORT":                        "55433",
		"PIPEFORGE_DATABASE_MAX_CONNS":         "4",
		"PIPEFORGE_SHUTDOWN_TIMEOUT":           "3s",
		"PIPEFORGE_ACCESS_TOKEN_TTL":           "10m",
		"PIPEFORGE_REFRESH_TOKEN_TTL":          "48h",
		"JWT_SIGNING_KEY":                      "test-jwt-signing-key-with-32-bytes!!",
		"MINIO_ENDPOINT":                       "minio:9000",
		"MINIO_PUBLIC_ENDPOINT":                "localhost:59010",
		"MINIO_ACCESS_KEY":                     "access",
		"MINIO_SECRET_KEY":                     "secret",
		"MINIO_DATASET_BUCKET":                 "custom-datasets",
		"MINIO_SECURE":                         "true",
		"MINIO_PUBLIC_SECURE":                  "false",
		"RABBITMQ_URL":                         "amqp://user:pass@rabbitmq:5672/",
		"PIPEFORGE_MAX_UPLOAD_BYTES":           "1048576",
		"PIPEFORGE_MULTIPART_PART_SIZE":        "5242880",
		"PIPEFORGE_MULTIPART_MAX_PARTS":        "1000",
		"PIPEFORGE_MULTIPART_MAX_BYTES":        "5242880000",
		"PIPEFORGE_MULTIPART_SESSION_TTL":      "12h",
		"PIPEFORGE_MULTIPART_URL_TTL":          "10m",
		"PIPEFORGE_MULTIPART_COMPLETION_GRACE": "45m",
		"PIPEFORGE_MULTIPART_CLEANUP_LIMIT":    "25",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DatabasePort != 55433 || cfg.DatabaseMaxConns != 4 || cfg.ShutdownTimeout != 3*time.Second || cfg.AccessTokenTTL != 10*time.Minute || cfg.RefreshTokenTTL != 48*time.Hour || cfg.MinIOEndpoint != "minio:9000" || cfg.MinIOPublicEndpoint != "localhost:59010" || cfg.RabbitMQURL != "amqp://user:pass@rabbitmq:5672/" || !cfg.MinIOSecure || cfg.MinIOPublicSecure || cfg.MaxUploadBytes != 1048576 || cfg.MultipartPartSize != 5242880 || cfg.MultipartMaxParts != 1000 || cfg.MultipartMaxBytes != 5242880000 || cfg.MultipartSessionTTL != 12*time.Hour || cfg.MultipartURLTTL != 10*time.Minute || cfg.MultipartCompletionGrace != 45*time.Minute || cfg.MultipartCleanupLimit != 25 {
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

func TestLoadRequiresMinIOEndpointOutsideDevelopment(t *testing.T) {
	values := map[string]string{
		"PIPEFORGE_ENV":        "production",
		"JWT_SIGNING_KEY":      "production-signing-key-with-32-bytes!!",
		"MINIO_ACCESS_KEY":     "access",
		"MINIO_SECRET_KEY":     "secret",
		"MINIO_DATASET_BUCKET": "datasets",
	}
	_, err := Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "MINIO_ENDPOINT") {
		t.Fatalf("expected explicit production MinIO endpoint error, got %v", err)
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
