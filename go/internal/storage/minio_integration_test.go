//go:build integration

package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

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
