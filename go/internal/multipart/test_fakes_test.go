package multipart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/google/uuid"
)

type fakeDatasetStore struct {
	mu       sync.Mutex
	datasets map[uuid.UUID]dataset.Dataset
	versions map[uuid.UUID]dataset.DatasetVersion
}

func newFakeDatasetStore(ownerID uuid.UUID) (*fakeDatasetStore, dataset.Dataset) {
	now := time.Now().UTC()
	item := dataset.Dataset{ID: uuid.New(), OwnerUserID: ownerID, Name: "events", State: "REGISTERED", CreatedAt: now, UpdatedAt: now}
	return &fakeDatasetStore{datasets: map[uuid.UUID]dataset.Dataset{item.ID: item}, versions: make(map[uuid.UUID]dataset.DatasetVersion)}, item
}

func (s *fakeDatasetStore) CreateDataset(_ context.Context, ownerID uuid.UUID, name, description string) (dataset.Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	item := dataset.Dataset{ID: uuid.New(), OwnerUserID: ownerID, Name: name, Description: description, State: "REGISTERED", CreatedAt: now, UpdatedAt: now}
	s.datasets[item.ID] = item
	return item, nil
}

func (s *fakeDatasetStore) FindDataset(_ context.Context, datasetID uuid.UUID) (dataset.Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.datasets[datasetID]
	if !ok {
		return dataset.Dataset{}, dataset.ErrDatasetNotFound
	}
	return item, nil
}

func (s *fakeDatasetStore) ListDatasets(_ context.Context, _ *uuid.UUID, _, _ int, _, _ string) (dataset.DatasetPage, error) {
	return dataset.DatasetPage{}, nil
}

func (s *fakeDatasetStore) DeleteDataset(_ context.Context, datasetID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.datasets[datasetID]
	if !ok {
		return dataset.ErrDatasetNotFound
	}
	now := time.Now().UTC()
	item.State, item.DeletedAt, item.UpdatedAt = "DELETED", &now, now
	s.datasets[datasetID] = item
	return nil
}

func (s *fakeDatasetStore) ReserveVersion(_ context.Context, reservation dataset.VersionReservation) (dataset.DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.datasets[reservation.DatasetID]
	if !ok {
		return dataset.DatasetVersion{}, dataset.ErrDatasetNotFound
	}
	if item.DeletedAt != nil {
		return dataset.DatasetVersion{}, dataset.ErrDatasetDeleted
	}
	versionNumber := int32(1)
	for _, version := range s.versions {
		if version.DatasetID == reservation.DatasetID && version.VersionNumber >= versionNumber {
			versionNumber = version.VersionNumber + 1
		}
	}
	now := time.Now().UTC()
	version := dataset.DatasetVersion{ID: reservation.ID, DatasetID: reservation.DatasetID, VersionNumber: versionNumber, State: "UPLOADING", OriginalFilename: reservation.OriginalFilename, ContentType: reservation.ContentType, Format: reservation.Format, ObjectKey: reservation.ObjectKey, SourceInfo: reservation.SourceInfo, ArtifactPrefix: reservation.ArtifactPrefix, CreatedBy: reservation.CreatedBy, CreatedAt: now}
	s.versions[version.ID] = version
	item.State, item.UpdatedAt = "UPLOADING", now
	s.datasets[item.ID] = item
	return version, nil
}

func (s *fakeDatasetStore) FindVersion(_ context.Context, datasetID, versionID uuid.UUID) (dataset.DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.DatasetVersion{}, dataset.ErrVersionNotFound
	}
	return version, nil
}

func (s *fakeDatasetStore) FinalizeVersion(_ context.Context, datasetID, versionID, _ uuid.UUID, size int64, checksum string) (dataset.DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.DatasetVersion{}, dataset.ErrVersionNotFound
	}
	if version.State == "AVAILABLE" {
		if version.SizeBytes == nil || *version.SizeBytes != size || version.ChecksumSHA256 == nil || *version.ChecksumSHA256 != checksum {
			return dataset.DatasetVersion{}, dataset.ErrVersionState
		}
		return version, nil
	}
	if version.State != "UPLOADING" {
		return dataset.DatasetVersion{}, dataset.ErrVersionState
	}
	now := time.Now().UTC()
	version.State, version.SizeBytes, version.ChecksumSHA256, version.AvailableAt = "AVAILABLE", &size, &checksum, &now
	s.versions[versionID] = version
	item := s.datasets[datasetID]
	item.State, item.UpdatedAt = "AVAILABLE", now
	s.datasets[datasetID] = item
	return version, nil
}

func (s *fakeDatasetStore) FailVersion(_ context.Context, datasetID, versionID, _ uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.ErrVersionNotFound
	}
	if version.State == "FAILED" {
		return nil
	}
	if version.State != "UPLOADING" {
		return dataset.ErrVersionState
	}
	version.State = "FAILED"
	s.versions[versionID] = version
	item := s.datasets[datasetID]
	item.State, item.UpdatedAt = "REGISTERED", time.Now().UTC()
	s.datasets[datasetID] = item
	return nil
}

func (s *fakeDatasetStore) AbortVersion(_ context.Context, datasetID, versionID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.versions[versionID]; !ok {
		return dataset.ErrVersionNotFound
	}
	delete(s.versions, versionID)
	item := s.datasets[datasetID]
	item.State, item.UpdatedAt = "REGISTERED", time.Now().UTC()
	s.datasets[datasetID] = item
	return nil
}

func (s *fakeDatasetStore) ListVersions(_ context.Context, datasetID uuid.UUID) ([]dataset.DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := make([]dataset.DatasetVersion, 0)
	for _, version := range s.versions {
		if version.DatasetID == datasetID {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

type fakeSessionStore struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]Session
	parts    map[uuid.UUID]map[int]Part
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: make(map[uuid.UUID]Session), parts: make(map[uuid.UUID]map[int]Part)}
}

func (s *fakeSessionStore) CreateSession(_ context.Context, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.sessions {
		if session.IdempotencyKey != "" && existing.OwnerUserID == session.OwnerUserID && existing.DatasetID == session.DatasetID && existing.IdempotencyKey == session.IdempotencyKey && existing.State != StateAborted && existing.State != StateExpired && existing.State != StateFailed {
			return ErrIdempotencyConflict
		}
	}
	s.sessions[session.ID] = session
	s.parts[session.ID] = make(map[int]Part)
	return nil
}

func (s *fakeSessionStore) FindSession(_ context.Context, sessionID uuid.UUID) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findSession(sessionID)
}

func (s *fakeSessionStore) FindActiveByIdempotency(_ context.Context, ownerID, datasetID uuid.UUID, key string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		if session.OwnerUserID == ownerID && session.DatasetID == datasetID && session.IdempotencyKey == key && session.State != StateAborted && session.State != StateExpired && session.State != StateFailed {
			return session, nil
		}
	}
	return Session{}, ErrSessionNotFound
}

func (s *fakeSessionStore) FindPart(_ context.Context, sessionID uuid.UUID, partNumber int) (Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	part, ok := s.parts[sessionID][partNumber]
	if !ok {
		return Part{}, ErrPartNotFound
	}
	return part, nil
}

func (s *fakeSessionStore) ListParts(_ context.Context, sessionID uuid.UUID) ([]Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := make([]Part, 0, len(s.parts[sessionID]))
	for _, part := range s.parts[sessionID] {
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

func (s *fakeSessionStore) RegisterPart(_ context.Context, sessionID uuid.UUID, partNumber int, etag string, size int64) (Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return Part{}, err
	}
	if session.State != StateInitiated {
		return Part{}, ErrSessionState
	}
	if existing, ok := s.parts[sessionID][partNumber]; ok {
		if existing.ETag != etag || existing.SizeBytes != size {
			return Part{}, ErrPartConflict
		}
		return existing, nil
	}
	part := Part{SessionID: sessionID, PartNumber: partNumber, ETag: etag, SizeBytes: size, CreatedAt: time.Now().UTC()}
	s.parts[sessionID][partNumber] = part
	return part, nil
}

func (s *fakeSessionStore) BeginComplete(_ context.Context, sessionID, ownerID uuid.UUID, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil || session.OwnerUserID != ownerID {
		return Session{}, ErrSessionNotFound
	}
	switch session.State {
	case StateCompleted:
		return session, ErrSessionCompleted
	case StateAborted, StateExpired, StateFailed, StateAborting:
		return Session{}, ErrSessionState
	case StateCompleting:
		return session, nil
	case StateInitiated:
		if !now.Before(session.ExpiresAt) {
			session.State = StateAborting
			session.LastError = stringPointer("session_expired")
			s.sessions[sessionID] = session
			return Session{}, ErrSessionExpired
		}
		session.State, session.UpdatedAt = StateCompleting, now
		s.sessions[sessionID] = session
		return session, nil
	default:
		return Session{}, ErrSessionState
	}
}

func (s *fakeSessionStore) BeginAbort(_ context.Context, sessionID, ownerID uuid.UUID, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil || session.OwnerUserID != ownerID {
		return Session{}, ErrSessionNotFound
	}
	switch session.State {
	case StateCompleted:
		return session, ErrSessionCompleted
	case StateAborted, StateExpired, StateAborting:
		return session, nil
	case StateCompleting:
		return Session{}, ErrSessionState
	case StateInitiated, StateFailed:
		session.State, session.UpdatedAt = StateAborting, now
		s.sessions[sessionID] = session
		return session, nil
	default:
		return Session{}, ErrSessionState
	}
}

func (s *fakeSessionStore) ResetCompletion(_ context.Context, sessionID uuid.UUID, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return err
	}
	if session.State != StateCompleting {
		return ErrSessionState
	}
	session.State, session.LastError = StateInitiated, stringPointer(lastError)
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) MarkCompleted(_ context.Context, sessionID uuid.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return err
	}
	if session.State == StateCompleted {
		return nil
	}
	if session.State != StateCompleting {
		return ErrSessionState
	}
	session.State, session.CompletedAt, session.UpdatedAt = StateCompleted, &at, at
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) MarkAborted(_ context.Context, sessionID uuid.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return err
	}
	if session.State == StateAborted {
		return nil
	}
	if session.State != StateAborting && session.State != StateFailed {
		return ErrSessionState
	}
	session.State, session.AbortedAt, session.UpdatedAt = StateAborted, &at, at
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) MarkFailed(_ context.Context, sessionID uuid.UUID, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return err
	}
	if session.State == StateCompleted {
		return ErrSessionState
	}
	session.State, session.LastError = StateFailed, stringPointer(lastError)
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) ClaimExpired(_ context.Context, now, completionStaleBefore time.Time, limit int) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	claimed := make([]Session, 0, limit)
	for id, session := range s.sessions {
		if len(claimed) >= limit || session.ExpiresAt.After(now) || (session.State != StateInitiated && session.State != StateCompleting && session.State != StateAborting) || (session.State == StateCompleting && session.UpdatedAt.After(completionStaleBefore)) {
			continue
		}
		session.State, session.UpdatedAt, session.LastError = StateAborting, now, stringPointer("session_expired")
		s.sessions[id] = session
		claimed = append(claimed, session)
	}
	return claimed, nil
}

func (s *fakeSessionStore) MarkExpired(_ context.Context, sessionID uuid.UUID, lastError string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.findSession(sessionID)
	if err != nil {
		return err
	}
	if session.State != StateAborting {
		return ErrSessionState
	}
	session.State, session.AbortedAt, session.UpdatedAt, session.LastError = StateExpired, &at, at, stringPointer(lastError)
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) findSession(sessionID uuid.UUID) (Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func stringPointer(value string) *string { return &value }

type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	uploads map[string]*fakeMultipartUpload
	nextID  int
	headErr error
}

type fakeMultipartUpload struct {
	Key         string
	ContentType string
	Parts       map[int]storage.MultipartPart
	Payloads    map[int][]byte
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string][]byte), uploads: make(map[string]*fakeMultipartUpload)}
}

func (s *fakeObjectStore) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	value, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	s.mu.Lock()
	s.objects[key] = append([]byte(nil), value...)
	s.mu.Unlock()
	return storage.ObjectInfo{Key: key, Size: int64(len(value)), ContentType: contentType}, nil
}

func (s *fakeObjectStore) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.headErr != nil {
		return storage.ObjectInfo{}, s.headErr
	}
	value, ok := s.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrObjectNotFound
	}
	return storage.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *fakeObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), value...))), nil
}

func (s *fakeObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *fakeObjectStore) InitiateMultipart(_ context.Context, key, contentType string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	uploadID := fmt.Sprintf("upload-%d", s.nextID)
	s.uploads[uploadID] = &fakeMultipartUpload{Key: key, ContentType: contentType, Parts: make(map[int]storage.MultipartPart), Payloads: make(map[int][]byte)}
	return uploadID, nil
}

func (s *fakeObjectStore) PresignPart(_ context.Context, key, uploadID string, partNumber int, _ time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, ok := s.uploads[uploadID]
	if !ok || upload.Key != key {
		return "", errors.New("multipart upload not found")
	}
	return fmt.Sprintf("https://minio.test/%s/%s?partNumber=%d", key, uploadID, partNumber), nil
}

func (s *fakeObjectStore) PutRemotePart(uploadID string, partNumber int, payload []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload := s.uploads[uploadID]
	etag := fmt.Sprintf("etag-%d", partNumber)
	upload.Parts[partNumber] = storage.MultipartPart{PartNumber: partNumber, ETag: etag, Size: int64(len(payload))}
	upload.Payloads[partNumber] = append([]byte(nil), payload...)
	return etag
}

func (s *fakeObjectStore) ListMultipartParts(_ context.Context, key, uploadID string) ([]storage.MultipartPart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, ok := s.uploads[uploadID]
	if !ok || upload.Key != key {
		return nil, errors.New("multipart upload not found")
	}
	parts := make([]storage.MultipartPart, 0, len(upload.Parts))
	for _, part := range upload.Parts {
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

func (s *fakeObjectStore) CompleteMultipart(_ context.Context, key, uploadID string, parts []storage.MultipartPart, contentType string) (storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, ok := s.uploads[uploadID]
	if !ok || upload.Key != key {
		return storage.ObjectInfo{}, errors.New("multipart upload not found")
	}
	var combined bytes.Buffer
	for _, requested := range parts {
		stored, ok := upload.Parts[requested.PartNumber]
		if !ok || stored.ETag != requested.ETag {
			return storage.ObjectInfo{}, errors.New("multipart part mismatch")
		}
		_, _ = combined.Write(upload.Payloads[requested.PartNumber])
	}
	s.objects[key] = combined.Bytes()
	delete(s.uploads, uploadID)
	return storage.ObjectInfo{Key: key, Size: int64(combined.Len()), ContentType: contentType}, nil
}

func (s *fakeObjectStore) AbortMultipart(_ context.Context, _, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.uploads, uploadID)
	return nil
}

var _ dataset.Store = (*fakeDatasetStore)(nil)
var _ Store = (*fakeSessionStore)(nil)
var _ storage.ObjectStore = (*fakeObjectStore)(nil)
var _ storage.MultipartStore = (*fakeObjectStore)(nil)
