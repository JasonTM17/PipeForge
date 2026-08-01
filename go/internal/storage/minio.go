package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	operationTimeout  = 30 * time.Second
	objectReadTimeout = 30 * time.Minute
)

type MinIOConfig struct {
	Endpoint       string
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Secure         bool
	Bucket         string
}

type MinIOStore struct {
	client        *minio.Client
	presignClient *minio.Client
	core          *minio.Core
	bucket        string
}

func NewMinIO(config MinIOConfig) (*MinIOStore, error) {
	if config.Endpoint == "" || config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
		return nil, fmt.Errorf("MinIO endpoint, credentials, and bucket are required")
	}
	options := &minio.Options{Creds: credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""), Secure: config.Secure}
	core, err := minio.NewCore(config.Endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	presignClient := core.Client
	if strings.TrimSpace(config.PublicEndpoint) != "" && strings.TrimSpace(config.PublicEndpoint) != strings.TrimSpace(config.Endpoint) {
		presignOptions := *options
		presignOptions.Region = "us-east-1"
		publicCore, publicErr := minio.NewCore(config.PublicEndpoint, &presignOptions)
		if publicErr != nil {
			return nil, fmt.Errorf("create MinIO presign client: %w", publicErr)
		}
		presignClient = publicCore.Client
	}
	return &MinIOStore{client: core.Client, presignClient: presignClient, core: core, bucket: config.Bucket}, nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if s == nil || s.client == nil {
		return ObjectInfo{}, fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	info, err := s.client.PutObject(operationCtx, s.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("put object %s: %w", key, err)
	}
	return ObjectInfo{Key: key, Size: info.Size, ETag: info.ETag, ContentType: contentType}, nil
}

func (s *MinIOStore) Head(ctx context.Context, key string) (ObjectInfo, error) {
	if s == nil || s.client == nil {
		return ObjectInfo{}, fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	info, err := s.client.StatObject(operationCtx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, wrapObjectError("head", key, err)
	}
	return ObjectInfo{Key: key, Size: info.Size, ETag: info.ETag, ContentType: info.ContentType}, nil
}

func (s *MinIOStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("MinIO store is not configured")
	}
	// Multipart completion verifies the SHA-256 by streaming the final object.
	// Keep reads bounded, but allow the configured 5 GiB upload ceiling to be
	// read over a slower local or development connection.
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), objectReadTimeout)
	object, err := s.client.GetObject(operationCtx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, wrapObjectError("get", key, err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		cancel()
		return nil, wrapObjectError("stat", key, err)
	}
	return &timedReadCloser{ReadCloser: object, cancel: cancel}, nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	if err := s.client.RemoveObject(operationCtx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}

func (s *MinIOStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	exists, err := s.client.BucketExists(operationCtx, s.bucket)
	if err != nil {
		return fmt.Errorf("check MinIO bucket %s: %w", s.bucket, err)
	}
	if !exists {
		return fmt.Errorf("MinIO bucket %s does not exist", s.bucket)
	}
	return nil
}

func (s *MinIOStore) InitiateMultipart(ctx context.Context, key, contentType string) (string, error) {
	if s == nil || s.core == nil {
		return "", fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	uploadID, err := s.core.NewMultipartUpload(operationCtx, s.bucket, key, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("initiate multipart object %s: %w", key, err)
	}
	return uploadID, nil
}

func (s *MinIOStore) PresignPart(ctx context.Context, key, uploadID string, partNumber int, expires time.Duration) (string, error) {
	if s == nil || s.presignClient == nil {
		return "", fmt.Errorf("MinIO store is not configured")
	}
	if partNumber < 1 || expires <= 0 || expires > 7*24*time.Hour {
		return "", fmt.Errorf("invalid multipart presign parameters")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	requestParams := url.Values{
		"partNumber": {strconv.Itoa(partNumber)},
		"uploadId":   {uploadID},
	}
	presigned, err := s.presignClient.Presign(operationCtx, http.MethodPut, s.bucket, key, expires, requestParams)
	if err != nil {
		return "", fmt.Errorf("presign multipart part %d: %w", partNumber, err)
	}
	return presigned.String(), nil
}

func (s *MinIOStore) ListMultipartParts(ctx context.Context, key, uploadID string) ([]MultipartPart, error) {
	if s == nil || s.core == nil {
		return nil, fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	parts := make([]MultipartPart, 0)
	marker := 0
	for {
		result, err := s.core.ListObjectParts(operationCtx, s.bucket, key, uploadID, marker, 10000)
		if err != nil {
			return nil, fmt.Errorf("list multipart parts for %s: %w", key, err)
		}
		for _, part := range result.ObjectParts {
			parts = append(parts, MultipartPart{PartNumber: part.PartNumber, ETag: strings.Trim(part.ETag, `"`), Size: part.Size})
		}
		if !result.IsTruncated || result.NextPartNumberMarker <= marker {
			return parts, nil
		}
		marker = result.NextPartNumberMarker
	}
}

func (s *MinIOStore) CompleteMultipart(ctx context.Context, key, uploadID string, parts []MultipartPart, contentType string) (ObjectInfo, error) {
	if s == nil || s.core == nil {
		return ObjectInfo{}, fmt.Errorf("MinIO store is not configured")
	}
	completeParts := make([]minio.CompletePart, 0, len(parts))
	for _, part := range parts {
		completeParts = append(completeParts, minio.CompletePart{PartNumber: part.PartNumber, ETag: strings.Trim(part.ETag, `"`)})
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	info, err := s.core.CompleteMultipartUpload(operationCtx, s.bucket, key, uploadID, completeParts, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("complete multipart object %s: %w", key, err)
	}
	return ObjectInfo{Key: key, Size: info.Size, ETag: info.ETag, ContentType: contentType}, nil
}

func (s *MinIOStore) AbortMultipart(ctx context.Context, key, uploadID string) error {
	if s == nil || s.core == nil {
		return fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	defer cancel()
	if err := s.core.AbortMultipartUpload(operationCtx, s.bucket, key, uploadID); err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code == "NoSuchUpload" {
			return nil
		}
		return fmt.Errorf("abort multipart object %s: %w", key, err)
	}
	return nil
}

type timedReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *timedReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func wrapObjectError(operation, key string, err error) error {
	response := minio.ToErrorResponse(err)
	if response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.Code == "NotFound" {
		return fmt.Errorf("%w: %s object %s: %v", ErrObjectNotFound, operation, key, err)
	}
	return fmt.Errorf("%s object %s: %w", operation, key, err)
}
