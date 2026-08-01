package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const operationTimeout = 30 * time.Second

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Secure    bool
	Bucket    string
}

type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIO(config MinIOConfig) (*MinIOStore, error) {
	if config.Endpoint == "" || config.AccessKey == "" || config.SecretKey == "" || config.Bucket == "" {
		return nil, fmt.Errorf("MinIO endpoint, credentials, and bucket are required")
	}
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	return &MinIOStore{client: client, bucket: config.Bucket}, nil
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
		return ObjectInfo{}, fmt.Errorf("head object %s: %w", key, err)
	}
	return ObjectInfo{Key: key, Size: info.Size, ETag: info.ETag, ContentType: info.ContentType}, nil
}

func (s *MinIOStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("MinIO store is not configured")
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), operationTimeout)
	object, err := s.client.GetObject(operationCtx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		cancel()
		return nil, fmt.Errorf("stat object %s: %w", key, err)
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
