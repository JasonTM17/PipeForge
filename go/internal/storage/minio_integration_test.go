//go:build integration

package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMinIOObjectLifecycle(t *testing.T) {
	endpoint := os.Getenv("PIPEFORGE_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("PIPEFORGE_TEST_MINIO_ENDPOINT is not set")
	}
	store, err := NewMinIO(MinIOConfig{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("PIPEFORGE_TEST_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("PIPEFORGE_TEST_MINIO_SECRET_KEY"),
		Secure:    os.Getenv("PIPEFORGE_TEST_MINIO_SECURE") == "true",
		Bucket:    os.Getenv("PIPEFORGE_TEST_MINIO_BUCKET"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "integration-tests/" + uuid.NewString() + ".txt"
	body := "pipeforge-minio-integration"
	_, err = store.Put(ctx, key, strings.NewReader(body), int64(len(body)), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })

	info, err := store.Head(ctx, key)
	if err != nil || info.Size != int64(len(body)) {
		t.Fatalf("Head returned info=%+v err=%v", info, err)
	}
	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	value, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(value) != body {
		t.Fatalf("Get returned value=%q readErr=%v closeErr=%v", value, readErr, closeErr)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Head(ctx, key); err == nil {
		t.Fatal("deleted object was still readable")
	}
}

func TestMinIOMultipartPresignedLifecycle(t *testing.T) {
	endpoint := os.Getenv("PIPEFORGE_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("PIPEFORGE_TEST_MINIO_ENDPOINT is not set")
	}
	store, err := NewMinIO(MinIOConfig{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("PIPEFORGE_TEST_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("PIPEFORGE_TEST_MINIO_SECRET_KEY"),
		Secure:    os.Getenv("PIPEFORGE_TEST_MINIO_SECURE") == "true",
		Bucket:    os.Getenv("PIPEFORGE_TEST_MINIO_BUCKET"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "integration-tests/" + uuid.NewString() + "-multipart.txt"
	uploadID, err := store.InitiateMultipart(ctx, key, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	t.Cleanup(func() {
		if !completed {
			_ = store.AbortMultipart(context.Background(), key, uploadID)
		}
		_ = store.Delete(context.Background(), key)
	})
	presigned, err := store.PresignPart(ctx, key, uploadID, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, bytes.NewReader([]byte("multipart-body")))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("presigned part upload status=%d body=%q readErr=%v closeErr=%v", response.StatusCode, responseBody, readErr, closeErr)
	}
	parts, err := store.ListMultipartParts(ctx, key, uploadID)
	if err != nil || len(parts) != 1 || parts[0].PartNumber != 1 || parts[0].Size != int64(len("multipart-body")) {
		t.Fatalf("unexpected multipart parts=%+v err=%v", parts, err)
	}
	if _, err := store.CompleteMultipart(ctx, key, uploadID, parts, "text/plain"); err != nil {
		t.Fatal(err)
	}
	completed = true
	info, err := store.Head(ctx, key)
	if err != nil || info.Size != int64(len("multipart-body")) {
		t.Fatalf("unexpected completed object info=%+v err=%v", info, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
}
