package multipart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/google/uuid"
)

func TestServiceCompletesMultipartUploadAndPromotesVersion(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, etags := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")

	version, err := service.Complete(context.Background(), principal, session.ID, CompleteRequest{Parts: []CompletePart{{PartNumber: 1, ETag: etags[0]}, {PartNumber: 2, ETag: etags[1]}}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if version.State != "AVAILABLE" || version.SizeBytes == nil || *version.SizeBytes != int64(len(payload)) {
		t.Fatalf("unexpected version: %+v", version)
	}
	if sessions.sessions[session.ID].State != StateCompleted {
		t.Fatalf("session was not completed: %+v", sessions.sessions[session.ID])
	}
	if _, err := objects.Head(context.Background(), session.ObjectKey); err != nil {
		t.Fatalf("completed object is missing: %v", err)
	}
}

func TestServiceCompleteRejectsReorderedPartsAndResetsSession(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, etags := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")

	_, err := service.Complete(context.Background(), principal, session.ID, CompleteRequest{Parts: []CompletePart{{PartNumber: 2, ETag: etags[1]}, {PartNumber: 1, ETag: etags[0]}}})
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ordered-part validation error, got %v", err)
	}
	if sessions.sessions[session.ID].State != StateInitiated {
		t.Fatalf("failed completion did not reset session: %+v", sessions.sessions[session.ID])
	}
	if len(datasets.versions) != 1 || datasets.versions[session.VersionID].State != "UPLOADING" {
		t.Fatalf("failed completion changed dataset version unexpectedly: %+v", datasets.versions)
	}
}

func TestServiceCompleteChecksumMismatchFailsVersionAndDeletesObject(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	wrongChecksum := strings.Repeat("0", sha256.Size*2)
	session, etags := prepareSession(t, service, datasets, objects, principal, datasetID, payload, wrongChecksum)

	_, err := service.Complete(context.Background(), principal, session.ID, CompleteRequest{Parts: []CompletePart{{PartNumber: 1, ETag: etags[0]}, {PartNumber: 2, ETag: etags[1]}}})
	if err == nil || !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if sessions.sessions[session.ID].State != StateFailed {
		t.Fatalf("checksum mismatch did not fail session: %+v", sessions.sessions[session.ID])
	}
	if datasets.versions[session.VersionID].State != "FAILED" {
		t.Fatalf("checksum mismatch did not fail version: %+v", datasets.versions[session.VersionID])
	}
	if _, err := objects.Head(context.Background(), session.ObjectKey); err == nil {
		t.Fatal("invalid completed object was not deleted")
	}
}

func TestServiceAbortAndCleanupAreIdempotent(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, _ := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")
	if err := service.Abort(context.Background(), principal, session.ID); err != nil {
		t.Fatalf("Abort returned error: %v", err)
	}
	if err := service.Abort(context.Background(), principal, session.ID); err != nil {
		t.Fatalf("repeated Abort returned error: %v", err)
	}
	if sessions.sessions[session.ID].State != StateAborted {
		t.Fatalf("session was not aborted: %+v", sessions.sessions[session.ID])
	}

	expiredSession, _ := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")
	now := time.Now().UTC().Add(2 * time.Hour)
	service.Now = func() time.Time { return now }
	report, err := service.CleanupExpired(context.Background(), 10)
	if err != nil {
		t.Fatalf("CleanupExpired returned error: %v", err)
	}
	if report.Expired != 1 || sessions.sessions[expiredSession.ID].State != StateExpired {
		t.Fatalf("unexpected cleanup report/state: %+v session=%+v", report, sessions.sessions[expiredSession.ID])
	}
	retry, err := service.CleanupExpired(context.Background(), 10)
	if err != nil || retry.Claimed != 0 {
		t.Fatalf("cleanup rerun was not idempotent: report=%+v err=%v", retry, err)
	}
}

func TestServiceCleanupRetriesWhenObjectCheckFails(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, _ := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")
	objects.headErr = errors.New("object store temporarily unavailable")
	service.Now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }

	report, err := service.CleanupExpired(context.Background(), 10)
	if err != nil {
		t.Fatalf("CleanupExpired returned error: %v", err)
	}
	if report.Claimed != 1 || report.Failed != 1 || report.Expired != 0 {
		t.Fatalf("unexpected cleanup report: %+v", report)
	}
	if sessions.sessions[session.ID].State != StateAborting {
		t.Fatalf("transient object-store error should leave session retryable: %+v", sessions.sessions[session.ID])
	}
	if datasets.versions[session.VersionID].State != "UPLOADING" {
		t.Fatalf("transient object-store error changed version state: %+v", datasets.versions[session.VersionID])
	}
}

func TestServiceCleanupLeavesFreshCompletionClaimableByItsOwner(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, _ := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")
	now := time.Now().UTC()
	session.State = StateCompleting
	session.ExpiresAt = now.Add(-time.Minute)
	session.UpdatedAt = now
	sessions.sessions[session.ID] = session
	service.Now = func() time.Time { return now }

	report, err := service.CleanupExpired(context.Background(), 10)
	if err != nil {
		t.Fatalf("CleanupExpired returned error: %v", err)
	}
	if report.Claimed != 0 || sessions.sessions[session.ID].State != StateCompleting {
		t.Fatalf("fresh completion was incorrectly claimed: report=%+v session=%+v", report, sessions.sessions[session.ID])
	}
}

func TestServiceCleanupReconcilesCompletedObjectWithoutDeletingIt(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	payload := multipartPayload(service.Config.PartSize + 3)
	session, etags := prepareSession(t, service, datasets, objects, principal, datasetID, payload, "")
	if _, err := objects.CompleteMultipart(context.Background(), session.ObjectKey, session.UploadID, []storage.MultipartPart{
		{PartNumber: 1, ETag: etags[0], Size: int64(service.Config.PartSize)},
		{PartNumber: 2, ETag: etags[1], Size: int64(len(payload)) - service.Config.PartSize},
	}, session.ContentType); err != nil {
		t.Fatalf("failed to create completed remote object: %v", err)
	}
	now := time.Now().UTC().Add(2 * time.Hour)
	session.State, session.ExpiresAt, session.UpdatedAt = StateAborting, now.Add(-time.Minute), now.Add(-2*time.Hour)
	sessions.sessions[session.ID] = session
	service.Now = func() time.Time { return now }

	report, err := service.CleanupExpired(context.Background(), 10)
	if err != nil {
		t.Fatalf("CleanupExpired returned error: %v", err)
	}
	if report.Reconciled != 1 || report.Failed != 0 || sessions.sessions[session.ID].State != StateCompleted {
		t.Fatalf("completed object was not reconciled: report=%+v session=%+v", report, sessions.sessions[session.ID])
	}
	if datasets.versions[session.VersionID].State != "AVAILABLE" {
		t.Fatalf("reconciled version was not promoted: %+v", datasets.versions[session.VersionID])
	}
	if _, err := objects.Head(context.Background(), session.ObjectKey); err != nil {
		t.Fatalf("valid completed object was deleted: %v", err)
	}
}

func TestServiceInitiatePersistsOrphanWhenCompensationFails(t *testing.T) {
	service, datasets, sessions, objects, principal, datasetID := newMultipartService(t)
	sessions.createErrors = []error{errors.New("database unavailable"), nil}
	objects.abortMultipartErr = errors.New("object store unavailable")

	_, err := service.Initiate(context.Background(), principal, datasetID, InitiateRequest{
		Filename: "events.csv", ContentType: "text/csv", ExpectedSize: service.Config.PartSize,
	})
	if err == nil || !strings.Contains(err.Error(), "object store unavailable") {
		t.Fatalf("expected primary and compensation errors, got %v", err)
	}
	if len(sessions.sessions) != 1 {
		t.Fatalf("compensation failure did not persist an orphan session: %+v", sessions.sessions)
	}
	for _, session := range sessions.sessions {
		if session.State != StateAborting || session.ExpiresAt.After(time.Now().UTC()) || session.IdempotencyKey != "" {
			t.Fatalf("orphan session is not immediately retryable: %+v", session)
		}
	}
	if len(datasets.versions) != 0 {
		t.Fatalf("unexpected staged versions after compensation: %+v", datasets.versions)
	}
}

func newMultipartService(t *testing.T) (*Service, *fakeDatasetStore, *fakeSessionStore, *fakeObjectStore, auth.Principal, uuid.UUID) {
	t.Helper()
	ownerID := uuid.New()
	datasets, item := newFakeDatasetStore(ownerID)
	sessions := newFakeSessionStore()
	objects := newFakeObjectStore()
	service, err := NewService(datasets, sessions, objects, Config{
		PartSize: 5 * 1024 * 1024, MaxParts: 10, MaxBytes: 50 * 1024 * 1024,
		SessionTTL: time.Hour, PartURLTTL: 10 * time.Minute, CompletionGrace: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	principal := auth.Principal{UserID: ownerID, Role: auth.RoleUser, Scopes: []string{authz.ScopeDatasetsRead, authz.ScopeDatasetsWrite}}
	return service, datasets, sessions, objects, principal, item.ID
}

func prepareSession(t *testing.T, service *Service, datasets *fakeDatasetStore, objects *fakeObjectStore, principal auth.Principal, datasetID uuid.UUID, payload []byte, expectedChecksum string) (Session, []string) {
	t.Helper()
	session, err := service.Initiate(context.Background(), principal, datasetID, InitiateRequest{Filename: "events.csv", ContentType: "text/csv", ExpectedSize: int64(len(payload)), ExpectedChecksum: expectedChecksum})
	if err != nil {
		t.Fatalf("Initiate returned error: %v", err)
	}
	firstSize := int(service.Config.PartSize)
	parts := [][]byte{payload[:firstSize], payload[firstSize:]}
	etags := make([]string, 0, len(parts))
	for index, partPayload := range parts {
		partNumber := index + 1
		etag := objects.PutRemotePart(session.UploadID, partNumber, partPayload)
		registered, registerErr := service.RegisterPart(context.Background(), principal, session.ID, partNumber, etag, int64(len(partPayload)))
		if registerErr != nil {
			t.Fatalf("RegisterPart(%d) returned error: %v", partNumber, registerErr)
		}
		etags = append(etags, registered.ETag)
	}
	_ = datasets
	return session, etags
}

func multipartPayload(size int64) []byte {
	payload := bytes.Repeat([]byte("a"), int(size))
	copy(payload, []byte("a,b\n1,2\n"))
	return payload
}
