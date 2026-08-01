package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr               = ":8080"
	defaultMaxConnections         = int32(10)
	defaultShutdown               = 10 * time.Second
	defaultAccessTokenTTL         = 15 * time.Minute
	defaultRefreshTokenTTL        = 30 * 24 * time.Hour
	defaultDevelopmentJWTKey      = "pipeforge-local-jwt-key-change-me"
	defaultDevelopmentMinIOKey    = "pipeforge"
	defaultDevelopmentMinIOSecret = "pipeforge-local-minio-secret"
	defaultDatasetBucket          = "datasets"
	defaultMaxUploadBytes         = int64(64 * 1024 * 1024)
)

// Config contains validated runtime settings. Secrets are kept in memory only
// and are never included in String or logging output.
type Config struct {
	Environment      string
	LogLevel         string
	HTTPAddr         string
	DatabaseHost     string
	DatabasePort     uint16
	DatabaseName     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseMaxConns int32
	ShutdownTimeout  time.Duration
	JWTSigningKey    string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	MinIOEndpoint    string
	MinIOAccessKey   string
	MinIOSecretKey   string
	MinIOSecure      bool
	DatasetBucket    string
	MaxUploadBytes   int64
}

// Load reads environment variables through getenv so tests can provide a
// deterministic source without mutating the process environment.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	environment := valueOrDefault(getenv("PIPEFORGE_ENV"), "development")
	jwtSigningKey := getenv("JWT_SIGNING_KEY")
	if jwtSigningKey == "" && environment == "development" {
		jwtSigningKey = defaultDevelopmentJWTKey
	}
	minioAccessKey := getenv("MINIO_ACCESS_KEY")
	minioSecretKey := getenv("MINIO_SECRET_KEY")
	minioEndpoint := getenv("MINIO_ENDPOINT")
	if environment == "development" {
		minioAccessKey = valueOrDefault(minioAccessKey, defaultDevelopmentMinIOKey)
		minioSecretKey = valueOrDefault(minioSecretKey, defaultDevelopmentMinIOSecret)
		minioEndpoint = valueOrDefault(minioEndpoint, "localhost:59010")
	}
	cfg := Config{
		Environment:      environment,
		LogLevel:         valueOrDefault(getenv("PIPEFORGE_LOG_LEVEL"), "info"),
		HTTPAddr:         valueOrDefault(getenv("PIPEFORGE_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseHost:     valueOrDefault(getenv("POSTGRES_HOST"), "localhost"),
		DatabaseName:     valueOrDefault(getenv("POSTGRES_DATABASE"), "pipeforge"),
		DatabaseUser:     valueOrDefault(getenv("POSTGRES_USER"), "pipeforge"),
		DatabasePassword: getenv("POSTGRES_PASSWORD"),
		DatabaseMaxConns: defaultMaxConnections,
		ShutdownTimeout:  defaultShutdown,
		JWTSigningKey:    jwtSigningKey,
		AccessTokenTTL:   defaultAccessTokenTTL,
		RefreshTokenTTL:  defaultRefreshTokenTTL,
		MinIOEndpoint:    minioEndpoint,
		MinIOAccessKey:   minioAccessKey,
		MinIOSecretKey:   minioSecretKey,
		DatasetBucket:    valueOrDefault(getenv("MINIO_DATASET_BUCKET"), defaultDatasetBucket),
		MaxUploadBytes:   defaultMaxUploadBytes,
	}

	port, err := parseUint16(valueOrDefault(getenv("POSTGRES_PORT"), "5432"))
	if err != nil {
		return Config{}, fmt.Errorf("POSTGRES_PORT: %w", err)
	}
	cfg.DatabasePort = port

	if raw := getenv("PIPEFORGE_DATABASE_MAX_CONNS"); raw != "" {
		maxConns, parseErr := strconv.ParseInt(raw, 10, 32)
		if parseErr != nil || maxConns < 1 {
			return Config{}, errors.New("PIPEFORGE_DATABASE_MAX_CONNS must be a positive integer")
		}
		cfg.DatabaseMaxConns = int32(maxConns)
	}

	if raw := getenv("PIPEFORGE_SHUTDOWN_TIMEOUT"); raw != "" {
		cfg.ShutdownTimeout, err = parsePositiveDuration(raw, "PIPEFORGE_SHUTDOWN_TIMEOUT")
		if err != nil {
			return Config{}, err
		}
	}

	if raw := getenv("PIPEFORGE_ACCESS_TOKEN_TTL"); raw != "" {
		cfg.AccessTokenTTL, err = parsePositiveDuration(raw, "PIPEFORGE_ACCESS_TOKEN_TTL")
		if err != nil {
			return Config{}, err
		}
	}
	if raw := getenv("PIPEFORGE_REFRESH_TOKEN_TTL"); raw != "" {
		cfg.RefreshTokenTTL, err = parsePositiveDuration(raw, "PIPEFORGE_REFRESH_TOKEN_TTL")
		if err != nil {
			return Config{}, err
		}
	}
	if raw := getenv("MINIO_SECURE"); raw != "" {
		secure, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return Config{}, errors.New("MINIO_SECURE must be true or false")
		}
		cfg.MinIOSecure = secure
	}
	if raw := getenv("PIPEFORGE_MAX_UPLOAD_BYTES"); raw != "" {
		maxBytes, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || maxBytes < 1 {
			return Config{}, errors.New("PIPEFORGE_MAX_UPLOAD_BYTES must be a positive integer")
		}
		cfg.MaxUploadBytes = maxBytes
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Environment == "" {
		return errors.New("PIPEFORGE_ENV must not be empty")
	}
	if c.LogLevel == "" {
		return errors.New("PIPEFORGE_LOG_LEVEL must not be empty")
	}
	if len(c.JWTSigningKey) < 32 {
		return errors.New("JWT_SIGNING_KEY must be at least 32 bytes")
	}
	if strings.TrimSpace(c.MinIOEndpoint) == "" {
		return errors.New("MINIO_ENDPOINT must be set outside development")
	}
	if strings.TrimSpace(c.MinIOAccessKey) == "" || strings.TrimSpace(c.MinIOSecretKey) == "" || strings.TrimSpace(c.DatasetBucket) == "" {
		return errors.New("MinIO credentials and dataset bucket must not be empty")
	}
	_, httpPort, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return fmt.Errorf("PIPEFORGE_HTTP_ADDR must be host:port: %w", err)
	}
	parsedHTTPPort, err := strconv.ParseUint(httpPort, 10, 16)
	if err != nil || parsedHTTPPort == 0 {
		return errors.New("PIPEFORGE_HTTP_ADDR must contain a positive numeric port")
	}
	if strings.TrimSpace(c.DatabaseHost) == "" || strings.TrimSpace(c.DatabaseName) == "" || strings.TrimSpace(c.DatabaseUser) == "" {
		return errors.New("PostgreSQL host, database, and user must not be empty")
	}
	if c.DatabasePort == 0 || c.DatabaseMaxConns < 1 || c.ShutdownTimeout <= 0 || c.AccessTokenTTL <= 0 || c.RefreshTokenTTL <= 0 || c.MaxUploadBytes <= 0 {
		return errors.New("PostgreSQL port, connection limit, shutdown timeout, token TTLs, and upload limit must be positive")
	}
	return nil
}

func (c Config) DatabaseDSN() string {
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DatabaseUser, c.DatabasePassword),
		Host:   net.JoinHostPort(c.DatabaseHost, strconv.Itoa(int(c.DatabasePort))),
		Path:   "/" + c.DatabaseName,
	}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return 0, errors.New("must be a positive port number")
	}
	return uint16(parsed), nil
}

func parsePositiveDuration(value, name string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}
