package storage

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestMinIOPresignUsesPublicEndpoint(t *testing.T) {
	store, err := NewMinIO(MinIOConfig{
		Endpoint: "minio:9000", PublicEndpoint: "localhost:59010",
		AccessKey: "access", SecretKey: "secret", Bucket: "datasets",
	})
	if err != nil {
		t.Fatalf("NewMinIO returned error: %v", err)
	}
	presigned, err := store.PresignPart(context.Background(), "datasets/key", "upload-id", 1, time.Minute)
	if err != nil {
		t.Fatalf("PresignPart returned error: %v", err)
	}
	parsed, err := url.Parse(presigned)
	if err != nil || parsed.Host != "localhost:59010" {
		t.Fatalf("unexpected presigned URL=%q err=%v", presigned, err)
	}
}

func TestMinIOPresignUsesPublicTLSSetting(t *testing.T) {
	store, err := NewMinIO(MinIOConfig{
		Endpoint: "minio:9000", PublicEndpoint: "uploads.example.test:443", PublicSecure: true,
		AccessKey: "access", SecretKey: "secret", Bucket: "datasets",
	})
	if err != nil {
		t.Fatalf("NewMinIO returned error: %v", err)
	}
	presigned, err := store.PresignPart(context.Background(), "datasets/key", "upload-id", 1, time.Minute)
	if err != nil {
		t.Fatalf("PresignPart returned error: %v", err)
	}
	parsed, err := url.Parse(presigned)
	if err != nil || parsed.Scheme != "https" {
		t.Fatalf("unexpected presigned URL=%q err=%v", presigned, err)
	}
}
