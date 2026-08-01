package dataset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

func TestUploadVersionStreamsAndFinalizes(t *testing.T) {
	ownerID := uuid.New()
	store, dataset := newMemoryDatasetStore(ownerID)
	objects := newMemoryObjectStore()
	service, err := NewService(store, objects, 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	body := []byte("{\"id\":1}\n{\"id\":2}\n")
	sum := sha256.Sum256(body)
	version, err := service.UploadVersion(context.Background(), datasetPrincipal(ownerID), dataset.ID, UploadRequest{
		Filename:         "events.jsonl",
		ContentType:      "application/jsonl; charset=utf-8",
		ExpectedChecksum: upload.ChecksumHex(sum),
		ContentLength:    int64(len(body)),
		Body:             bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("UploadVersion returned error: %v", err)
	}
	if version.State != "AVAILABLE" || version.Format != upload.FormatJSONL || version.SizeBytes == nil || *version.SizeBytes != int64(len(body)) {
		t.Fatalf("unexpected finalized version: %+v", version)
	}
	if len(objects.objects[version.ObjectKey]) != len(body) || len(store.aborted) != 0 {
		t.Fatalf("upload did not persist exactly once: objects=%v aborted=%v", objects.objects, store.aborted)
	}
}

func TestUploadVersionRejectsOversizeAndCleansUp(t *testing.T) {
	ownerID := uuid.New()
	store, dataset := newMemoryDatasetStore(ownerID)
	objects := newMemoryObjectStore()
	service, err := NewService(store, objects, 8)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	body := []byte("a,b\n123456,2\n")
	_, err = service.UploadVersion(context.Background(), datasetPrincipal(ownerID), dataset.ID, UploadRequest{
		Filename:      "events.csv",
		ContentType:   "text/csv",
		ContentLength: -1,
		Body:          bytes.NewReader(body),
	})
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("expected oversize error, got %v", err)
	}
	if len(objects.objects) != 0 || len(objects.deleted) != 1 || len(store.versions) != 0 || len(store.aborted) != 1 {
		t.Fatalf("oversize cleanup incomplete: objects=%v deleted=%v versions=%v aborted=%v", objects.objects, objects.deleted, store.versions, store.aborted)
	}
}

func TestUploadVersionBoundsLargeStreamBeforeCleanup(t *testing.T) {
	ownerID := uuid.New()
	store, dataset := newMemoryDatasetStore(ownerID)
	objects := newMemoryObjectStore()
	service, err := NewService(store, objects, 128*1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	body := &repeatingReader{remaining: 1024 * 1024, pattern: []byte("1,2\n")}
	_, err = service.UploadVersion(context.Background(), datasetPrincipal(ownerID), dataset.ID, UploadRequest{
		Filename:      "large.csv",
		ContentType:   "text/csv",
		ContentLength: -1,
		Body:          body,
	})
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("expected oversize error, got %v", err)
	}
	if body.ReadBytes > service.MaxUploadBytes+1 {
		t.Fatalf("stream reader was not bounded: read %d bytes", body.ReadBytes)
	}
	if len(objects.objects) != 0 || len(store.versions) != 0 || len(store.aborted) != 1 {
		t.Fatalf("large stream cleanup incomplete: objects=%v versions=%v aborted=%v", objects.objects, store.versions, store.aborted)
	}
}

func TestUploadVersionRejectsChecksumMismatchAndCleansUp(t *testing.T) {
	ownerID := uuid.New()
	store, dataset := newMemoryDatasetStore(ownerID)
	objects := newMemoryObjectStore()
	service, err := NewService(store, objects, 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	_, err = service.UploadVersion(context.Background(), datasetPrincipal(ownerID), dataset.ID, UploadRequest{
		Filename:         "events.csv",
		ContentType:      "text/csv",
		ExpectedChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContentLength:    8,
		Body:             bytes.NewReader([]byte("a,b\n1,2\n")),
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum error, got %v", err)
	}
	if len(objects.objects) != 0 || len(store.versions) != 0 || len(store.aborted) != 1 {
		t.Fatalf("checksum cleanup incomplete: objects=%v versions=%v aborted=%v", objects.objects, store.versions, store.aborted)
	}
}

func TestUploadVersionDeniesCrossOwnerBeforeReadingBody(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	store, dataset := newMemoryDatasetStore(ownerID)
	objects := newMemoryObjectStore()
	service, err := NewService(store, objects, 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	body := &readTrackingReader{Reader: bytes.NewReader([]byte("a,b\n1,2\n"))}
	_, err = service.UploadVersion(context.Background(), datasetPrincipal(otherID), dataset.ID, UploadRequest{
		Filename:      "events.csv",
		ContentType:   "text/csv",
		ContentLength: -1,
		Body:          body,
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
	if body.Reads != 0 || len(store.versions) != 0 {
		t.Fatalf("cross-owner request read or reserved data: reads=%d versions=%v", body.Reads, store.versions)
	}
}

func TestDatasetListRejectsInvalidPaginationAndState(t *testing.T) {
	ownerID := uuid.New()
	store, _ := newMemoryDatasetStore(ownerID)
	service, err := NewService(store, newMemoryObjectStore(), 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	principal := datasetPrincipal(ownerID)
	if _, err := service.List(context.Background(), principal, 1, 101, "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid page size, got %v", err)
	}
	if _, err := service.List(context.Background(), principal, 1, 20, "UNKNOWN", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid state, got %v", err)
	}
}

func datasetPrincipal(userID uuid.UUID) auth.Principal {
	return auth.Principal{UserID: userID, Role: auth.RoleUser, Scopes: authz.DefaultUserScopes()}
}

type readTrackingReader struct {
	Reader *bytes.Reader
	Reads  int
}

type repeatingReader struct {
	remaining int64
	pattern   []byte
	offset    int
	ReadBytes int64
}

func (r *repeatingReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	count := len(buffer)
	if int64(count) > r.remaining {
		count = int(r.remaining)
	}
	for index := 0; index < count; index++ {
		buffer[index] = r.pattern[r.offset]
		r.offset = (r.offset + 1) % len(r.pattern)
	}
	r.remaining -= int64(count)
	r.ReadBytes += int64(count)
	return count, nil
}

func (r *readTrackingReader) Read(buffer []byte) (int, error) {
	r.Reads++
	return r.Reader.Read(buffer)
}
