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
	defaultHTTPAddr                 = ":8080"
	defaultMaxConnections           = int32(10)
	defaultShutdown                 = 10 * time.Second
	defaultAccessTokenTTL           = 15 * time.Minute
	defaultRefreshTokenTTL          = 30 * 24 * time.Hour
	defaultDevelopmentJWTKey        = "pipeforge-local-jwt-key-change-me"
	defaultDevelopmentMinIOKey      = "pipeforge"
	defaultDevelopmentMinIOSecret   = "pipeforge-local-minio-secret"
	defaultDevelopmentRabbitMQURL   = "amqp://pipeforge:pipeforge-local-rabbitmq@localhost:55672/"
	defaultDatasetBucket            = "datasets"
	defaultMaxUploadBytes           = int64(64 * 1024 * 1024)
	defaultMultipartPartSize        = int64(8 * 1024 * 1024)
	defaultMultipartMaxParts        = 10000
	defaultMultipartMaxBytes        = int64(5 * 1024 * 1024 * 1024)
	defaultMultipartSessionTTL      = 24 * time.Hour
	defaultMultipartURLTTL          = 15 * time.Minute
	defaultMultipartCompletionGrace = time.Hour
	defaultMultipartCleanupLimit    = 100
)

// Config contains validated runtime settings. Secrets are kept in memory only
// and are never included in String or logging output.
type Config struct {
	Environment              string
	LogLevel                 string
	HTTPAddr                 string
	DatabaseHost             string
	DatabasePort             uint16
	DatabaseName             string
	DatabaseUser             string
	DatabasePassword         string
	DatabaseMaxConns         int32
	ShutdownTimeout          time.Duration
	JWTSigningKey            string
	AccessTokenTTL           time.Duration
	RefreshTokenTTL          time.Duration
	MinIOEndpoint            string
	MinIOPublicEndpoint      string
	MinIOAccessKey           string
	MinIOSecretKey           string
	MinIOSecure              bool
	MinIOPublicSecure        bool
	RabbitMQURL              string
	DatasetBucket            string
	MaxUploadBytes           int64
	MultipartPartSize        int64
	MultipartMaxParts        int
	MultipartMaxBytes        int64
	MultipartSessionTTL      time.Duration
	MultipartURLTTL          time.Duration
	MultipartCompletionGrace time.Duration
	MultipartCleanupLimit    int
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
	minioPublicEndpoint := getenv("MINIO_PUBLIC_ENDPOINT")
	rabbitMQURL := getenv("RABBITMQ_URL")
	if environment == "development" {
		minioAccessKey = valueOrDefault(minioAccessKey, defaultDevelopmentMinIOKey)
		minioSecretKey = valueOrDefault(minioSecretKey, defaultDevelopmentMinIOSecret)
		minioEndpoint = valueOrDefault(minioEndpoint, "localhost:59010")
		minioPublicEndpoint = valueOrDefault(minioPublicEndpoint, "localhost:59010")
		rabbitMQURL = valueOrDefault(rabbitMQURL, defaultDevelopmentRabbitMQURL)
	}
	cfg := Config{
		Environment:              environment,
		LogLevel:                 valueOrDefault(getenv("PIPEFORGE_LOG_LEVEL"), "info"),
		HTTPAddr:                 valueOrDefault(getenv("PIPEFORGE_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseHost:             valueOrDefault(getenv("POSTGRES_HOST"), "localhost"),
		DatabaseName:             valueOrDefault(getenv("POSTGRES_DATABASE"), "pipeforge"),
		DatabaseUser:             valueOrDefault(getenv("POSTGRES_USER"), "pipeforge"),
		DatabasePassword:         getenv("POSTGRES_PASSWORD"),
		DatabaseMaxConns:         defaultMaxConnections,
		ShutdownTimeout:          defaultShutdown,
		JWTSigningKey:            jwtSigningKey,
		AccessTokenTTL:           defaultAccessTokenTTL,
		RefreshTokenTTL:          defaultRefreshTokenTTL,
		MinIOEndpoint:            minioEndpoint,
		MinIOPublicEndpoint:      minioPublicEndpoint,
		MinIOAccessKey:           minioAccessKey,
		MinIOSecretKey:           minioSecretKey,
		RabbitMQURL:              rabbitMQURL,
		DatasetBucket:            valueOrDefault(getenv("MINIO_DATASET_BUCKET"), defaultDatasetBucket),
		MaxUploadBytes:           defaultMaxUploadBytes,
		MultipartPartSize:        defaultMultipartPartSize,
		MultipartMaxParts:        defaultMultipartMaxParts,
		MultipartMaxBytes:        defaultMultipartMaxBytes,
		MultipartSessionTTL:      defaultMultipartSessionTTL,
		MultipartURLTTL:          defaultMultipartURLTTL,
		MultipartCompletionGrace: defaultMultipartCompletionGrace,
		MultipartCleanupLimit:    defaultMultipartCleanupLimit,
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
	cfg.MinIOPublicSecure = cfg.MinIOSecure
	if raw := getenv("MINIO_PUBLIC_SECURE"); raw != "" {
		publicSecure, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return Config{}, errors.New("MINIO_PUBLIC_SECURE must be true or false")
		}
		cfg.MinIOPublicSecure = publicSecure
	}
	if raw := getenv("PIPEFORGE_MAX_UPLOAD_BYTES"); raw != "" {
		maxBytes, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || maxBytes < 1 {
			return Config{}, errors.New("PIPEFORGE_MAX_UPLOAD_BYTES must be a positive integer")
		}
		cfg.MaxUploadBytes = maxBytes
	}
	if raw := getenv("PIPEFORGE_MULTIPART_PART_SIZE"); raw != "" {
		partSize, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || partSize < 5*1024*1024 {
			return Config{}, errors.New("PIPEFORGE_MULTIPART_PART_SIZE must be at least 5242880 bytes")
		}
		cfg.MultipartPartSize = partSize
	}
	if raw := getenv("PIPEFORGE_MULTIPART_MAX_PARTS"); raw != "" {
		maxParts, parseErr := strconv.Atoi(raw)
		if parseErr != nil || maxParts < 1 || maxParts > 10000 {
			return Config{}, errors.New("PIPEFORGE_MULTIPART_MAX_PARTS must be between 1 and 10000")
		}
		cfg.MultipartMaxParts = maxParts
	}
	if raw := getenv("PIPEFORGE_MULTIPART_MAX_BYTES"); raw != "" {
		maxBytes, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || maxBytes < 1 {
			return Config{}, errors.New("PIPEFORGE_MULTIPART_MAX_BYTES must be positive")
		}
		cfg.MultipartMaxBytes = maxBytes
	}
	if raw := getenv("PIPEFORGE_MULTIPART_SESSION_TTL"); raw != "" {
		cfg.MultipartSessionTTL, err = parsePositiveDuration(raw, "PIPEFORGE_MULTIPART_SESSION_TTL")
		if err != nil {
			return Config{}, err
		}
	}
	if raw := getenv("PIPEFORGE_MULTIPART_URL_TTL"); raw != "" {
		cfg.MultipartURLTTL, err = parsePositiveDuration(raw, "PIPEFORGE_MULTIPART_URL_TTL")
		if err != nil {
			return Config{}, err
		}
	}
	if raw := getenv("PIPEFORGE_MULTIPART_COMPLETION_GRACE"); raw != "" {
		cfg.MultipartCompletionGrace, err = parsePositiveDuration(raw, "PIPEFORGE_MULTIPART_COMPLETION_GRACE")
		if err != nil {
			return Config{}, err
		}
	}
	if raw := getenv("PIPEFORGE_MULTIPART_CLEANUP_LIMIT"); raw != "" {
		cleanupLimit, parseErr := strconv.Atoi(raw)
		if parseErr != nil || cleanupLimit < 1 || cleanupLimit > 1000 {
			return Config{}, errors.New("PIPEFORGE_MULTIPART_CLEANUP_LIMIT must be between 1 and 1000")
		}
		cfg.MultipartCleanupLimit = cleanupLimit
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
	if c.Environment != "development" && strings.TrimSpace(c.MinIOPublicEndpoint) == "" {
		return errors.New("MINIO_PUBLIC_ENDPOINT must be set outside development")
	}
	if strings.TrimSpace(c.MinIOAccessKey) == "" || strings.TrimSpace(c.MinIOSecretKey) == "" || strings.TrimSpace(c.DatasetBucket) == "" {
		return errors.New("MinIO credentials and dataset bucket must not be empty")
	}
	if strings.TrimSpace(c.RabbitMQURL) == "" {
		return errors.New("RABBITMQ_URL must be set")
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
	if c.DatabasePort == 0 || c.DatabaseMaxConns < 1 || c.ShutdownTimeout <= 0 || c.AccessTokenTTL <= 0 || c.RefreshTokenTTL <= 0 || c.MaxUploadBytes <= 0 || c.MultipartPartSize < 5*1024*1024 || c.MultipartPartSize > 5*1024*1024*1024 || c.MultipartMaxParts < 1 || c.MultipartMaxParts > 10000 || c.MultipartMaxBytes <= 0 || c.MultipartSessionTTL <= 0 || c.MultipartURLTTL <= 0 || c.MultipartURLTTL > 7*24*time.Hour || c.MultipartCompletionGrace <= 0 || c.MultipartCleanupLimit < 1 || c.MultipartCleanupLimit > 1000 {
		return errors.New("PostgreSQL, connection, timeout, upload, and multipart limits must be positive and bounded")
	}
	if c.MultipartMaxBytes < c.MultipartPartSize || c.MultipartMaxBytes > c.MultipartPartSize*int64(c.MultipartMaxParts) {
		return errors.New("multipart maximum bytes must fit within configured part bounds")
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
