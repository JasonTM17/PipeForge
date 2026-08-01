package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/multipart"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/google/uuid"
)

func TestMultipartHTTPLifecycleAndOwnership(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	objects := newHTTPMultipartObjectStore()
	datasetService, err := dataset.NewService(datasetStore, objects, 1024)
	if err != nil {
		t.Fatalf("dataset NewService returned error: %v", err)
	}
	sessions := newHTTPMultipartSessionStore()
	multipartService, err := multipart.NewService(datasetStore, sessions, objects, multipart.Config{
		PartSize: 5 * 1024 * 1024, MaxParts: 10, MaxBytes: 50 * 1024 * 1024,
		SessionTTL: time.Hour, PartURLTTL: 10 * time.Minute, CompletionGrace: time.Hour,
	})
	if err != nil {
		t.Fatalf("multipart NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Dataset: datasetService, Multipart: multipartService})

	registerResponse := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"multipart-owner@example.com","password":"correct horse battery staple"}`, "")
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d %s", registerResponse.Code, registerResponse.Body.String())
	}
	var ownerTokens tokenResponse
	if err := json.Unmarshal(registerResponse.Body.Bytes(), &ownerTokens); err != nil {
		t.Fatalf("decode owner tokens: %v", err)
	}
	createdResponse := performJSONRequest(router, http.MethodPost, "/v1/datasets", `{"name":"multipart-sales","description":"large files"}`, ownerTokens.AccessToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("dataset creation failed: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	var item dataset.Dataset
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode dataset: %v", err)
	}

	expectedSize := int64(5*1024*1024 + 3)
	initiation := fmt.Sprintf(`{"filename":"events.csv","contentType":"text/csv","expectedSize":%d,"idempotencyKey":"upload-1"}`, expectedSize)
	initiationResponse := performJSONRequest(router, http.MethodPost, "/v1/datasets/"+item.ID.String()+"/uploads", initiation, ownerTokens.AccessToken)
	if initiationResponse.Code != http.StatusCreated {
		t.Fatalf("multipart initiation failed: %d %s", initiationResponse.Code, initiationResponse.Body.String())
	}
	if strings.Contains(initiationResponse.Body.String(), "uploadId") || strings.Contains(initiationResponse.Body.String(), "objectKey") {
		t.Fatal("multipart initiation exposed remote upload credentials or object key")
	}
	var session multipart.Session
	if err := json.Unmarshal(initiationResponse.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode multipart session: %v", err)
	}
	storedSession := sessions.sessions[session.ID]
	firstPayload := bytes.Repeat([]byte("a"), 5*1024*1024)
	copy(firstPayload, []byte("a,b\n1,2\n"))
	secondPayload := []byte("a,b")
	etag1 := objects.putPart(storedSession.UploadID, 1, firstPayload)
	etag2 := objects.putPart(storedSession.UploadID, 2, secondPayload)

	presignResponse := performJSONRequest(router, http.MethodPost, "/v1/uploads/"+session.ID.String()+"/parts/1/presign", "", ownerTokens.AccessToken)
	if presignResponse.Code != http.StatusOK || !strings.Contains(presignResponse.Body.String(), "https://") {
		t.Fatalf("presign failed: %d %s", presignResponse.Code, presignResponse.Body.String())
	}
	registerPart := func(number int, etag string, size int64) *httptest.ResponseRecorder {
		return performJSONRequest(router, http.MethodPut, fmt.Sprintf("/v1/uploads/%s/parts/%d", session.ID, number), fmt.Sprintf(`{"etag":%q,"sizeBytes":%d}`, etag, size), ownerTokens.AccessToken)
	}
	if response := registerPart(1, etag1, int64(len(firstPayload))); response.Code != http.StatusOK {
		t.Fatalf("register part 1 failed: %d %s", response.Code, response.Body.String())
	}
	if response := registerPart(2, etag2, int64(len(secondPayload))); response.Code != http.StatusOK {
		t.Fatalf("register part 2 failed: %d %s", response.Code, response.Body.String())
	}
	completeResponse := performJSONRequest(router, http.MethodPost, "/v1/uploads/"+session.ID.String()+"/complete", fmt.Sprintf(`{"parts":[{"partNumber":1,"etag":%q},{"partNumber":2,"etag":%q}]}`, etag1, etag2), ownerTokens.AccessToken)
	if completeResponse.Code != http.StatusCreated {
		t.Fatalf("multipart completion failed: %d %s", completeResponse.Code, completeResponse.Body.String())
	}
	if sessions.sessions[session.ID].State != multipart.StateCompleted {
		t.Fatalf("session state was not completed: %+v", sessions.sessions[session.ID])
	}
	abortResponse := performJSONRequest(router, http.MethodPost, "/v1/uploads/"+session.ID.String()+"/abort", "", ownerTokens.AccessToken)
	if abortResponse.Code != http.StatusConflict {
		t.Fatalf("completed session abort was not rejected: %d %s", abortResponse.Code, abortResponse.Body.String())
	}

	otherRegister := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"multipart-other@example.com","password":"correct horse battery staple"}`, "")
	var otherTokens tokenResponse
	if otherRegister.Code != http.StatusCreated || json.Unmarshal(otherRegister.Body.Bytes(), &otherTokens) != nil {
		t.Fatalf("second registration failed: %d %s", otherRegister.Code, otherRegister.Body.String())
	}
	otherGet := performJSONRequest(router, http.MethodGet, "/v1/uploads/"+session.ID.String(), "", otherTokens.AccessToken)
	if otherGet.Code != http.StatusForbidden {
		t.Fatalf("cross-owner session read was not denied: %d %s", otherGet.Code, otherGet.Body.String())
	}
}

type httpMultipartObjectStore struct {
	objects map[string][]byte
	uploads map[string]*httpMultipartUpload
	nextID  int
}

type httpMultipartUpload struct {
	key      string
	parts    map[int]storage.MultipartPart
	payloads map[int][]byte
}

func newHTTPMultipartObjectStore() *httpMultipartObjectStore {
	return &httpMultipartObjectStore{objects: make(map[string][]byte), uploads: make(map[string]*httpMultipartUpload)}
}

func (s *httpMultipartObjectStore) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	value, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	s.objects[key] = value
	return storage.ObjectInfo{Key: key, Size: int64(len(value)), ContentType: contentType}, nil
}

func (s *httpMultipartObjectStore) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	value, ok := s.objects[key]
	if !ok {
		return storage.ObjectInfo{}, errors.New("object not found")
	}
	return storage.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *httpMultipartObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	value, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *httpMultipartObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *httpMultipartObjectStore) InitiateMultipart(_ context.Context, key, _ string) (string, error) {
	s.nextID++
	uploadID := fmt.Sprintf("http-upload-%d", s.nextID)
	s.uploads[uploadID] = &httpMultipartUpload{key: key, parts: make(map[int]storage.MultipartPart), payloads: make(map[int][]byte)}
	return uploadID, nil
}

func (s *httpMultipartObjectStore) PresignPart(_ context.Context, key, uploadID string, partNumber int, _ time.Duration) (string, error) {
	upload, ok := s.uploads[uploadID]
	if !ok || upload.key != key {
		return "", errors.New("multipart upload not found")
	}
	return fmt.Sprintf("https://minio.test/%s/%s?partNumber=%d", key, uploadID, partNumber), nil
}

func (s *httpMultipartObjectStore) putPart(uploadID string, number int, payload []byte) string {
	upload := s.uploads[uploadID]
	etag := fmt.Sprintf("etag-%d", number)
	upload.parts[number] = storage.MultipartPart{PartNumber: number, ETag: etag, Size: int64(len(payload))}
	upload.payloads[number] = payload
	return etag
}

func (s *httpMultipartObjectStore) ListMultipartParts(_ context.Context, key, uploadID string) ([]storage.MultipartPart, error) {
	upload, ok := s.uploads[uploadID]
	if !ok || upload.key != key {
		return nil, errors.New("multipart upload not found")
	}
	parts := make([]storage.MultipartPart, 0, len(upload.parts))
	for _, part := range upload.parts {
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

func (s *httpMultipartObjectStore) GetMultipartPart(_ context.Context, key, uploadID string, partNumber int) (storage.MultipartPart, error) {
	parts, err := s.ListMultipartParts(context.Background(), key, uploadID)
	if err != nil {
		return storage.MultipartPart{}, err
	}
	for _, part := range parts {
		if part.PartNumber == partNumber {
			return part, nil
		}
	}
	return storage.MultipartPart{}, storage.ErrMultipartPartNotFound
}

func (s *httpMultipartObjectStore) CompleteMultipart(_ context.Context, key, uploadID string, parts []storage.MultipartPart, _ string) (storage.ObjectInfo, error) {
	upload, ok := s.uploads[uploadID]
	if !ok || upload.key != key {
		return storage.ObjectInfo{}, errors.New("multipart upload not found")
	}
	var combined bytes.Buffer
	for _, part := range parts {
		stored, ok := upload.parts[part.PartNumber]
		if !ok || stored.ETag != part.ETag {
			return storage.ObjectInfo{}, errors.New("multipart part mismatch")
		}
		_, _ = combined.Write(upload.payloads[part.PartNumber])
	}
	s.objects[key] = combined.Bytes()
	delete(s.uploads, uploadID)
	return storage.ObjectInfo{Key: key, Size: int64(combined.Len())}, nil
}

func (s *httpMultipartObjectStore) AbortMultipart(_ context.Context, _, uploadID string) error {
	delete(s.uploads, uploadID)
	return nil
}

type httpMultipartSessionStore struct {
	sessions map[uuid.UUID]multipart.Session
	parts    map[uuid.UUID]map[int]multipart.Part
}

func newHTTPMultipartSessionStore() *httpMultipartSessionStore {
	return &httpMultipartSessionStore{sessions: make(map[uuid.UUID]multipart.Session), parts: make(map[uuid.UUID]map[int]multipart.Part)}
}

func (s *httpMultipartSessionStore) CreateSession(_ context.Context, session multipart.Session) error {
	for _, existing := range s.sessions {
		if session.IdempotencyKey != "" && existing.OwnerUserID == session.OwnerUserID && existing.DatasetID == session.DatasetID && existing.IdempotencyKey == session.IdempotencyKey && existing.State != multipart.StateAborted && existing.State != multipart.StateExpired && existing.State != multipart.StateFailed {
			return multipart.ErrIdempotencyConflict
		}
	}
	s.sessions[session.ID] = session
	s.parts[session.ID] = make(map[int]multipart.Part)
	return nil
}

func (s *httpMultipartSessionStore) FindSession(_ context.Context, id uuid.UUID) (multipart.Session, error) {
	session, ok := s.sessions[id]
	if !ok {
		return multipart.Session{}, multipart.ErrSessionNotFound
	}
	return session, nil
}

func (s *httpMultipartSessionStore) FindActiveByIdempotency(_ context.Context, ownerID, datasetID uuid.UUID, key string) (multipart.Session, error) {
	for _, session := range s.sessions {
		if session.OwnerUserID == ownerID && session.DatasetID == datasetID && session.IdempotencyKey == key && session.State != multipart.StateAborted && session.State != multipart.StateExpired && session.State != multipart.StateFailed {
			return session, nil
		}
	}
	return multipart.Session{}, multipart.ErrSessionNotFound
}

func (s *httpMultipartSessionStore) FindPart(_ context.Context, id uuid.UUID, number int) (multipart.Part, error) {
	part, ok := s.parts[id][number]
	if !ok {
		return multipart.Part{}, multipart.ErrPartNotFound
	}
	return part, nil
}

func (s *httpMultipartSessionStore) ListParts(_ context.Context, id uuid.UUID) ([]multipart.Part, error) {
	parts := make([]multipart.Part, 0, len(s.parts[id]))
	for _, part := range s.parts[id] {
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

func (s *httpMultipartSessionStore) RegisterPart(_ context.Context, id uuid.UUID, number int, etag string, size int64) (multipart.Part, error) {
	session, err := s.FindSession(context.Background(), id)
	if err != nil {
		return multipart.Part{}, err
	}
	if session.State != multipart.StateInitiated {
		return multipart.Part{}, multipart.ErrSessionState
	}
	if existing, ok := s.parts[id][number]; ok {
		if existing.ETag != etag || existing.SizeBytes != size {
			return multipart.Part{}, multipart.ErrPartConflict
		}
		return existing, nil
	}
	part := multipart.Part{SessionID: id, PartNumber: number, ETag: etag, SizeBytes: size, CreatedAt: time.Now().UTC()}
	s.parts[id][number] = part
	return part, nil
}

func (s *httpMultipartSessionStore) BeginComplete(_ context.Context, id, ownerID uuid.UUID, now, _ time.Time) (multipart.Session, error) {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.OwnerUserID != ownerID {
		return multipart.Session{}, multipart.ErrSessionNotFound
	}
	switch session.State {
	case multipart.StateCompleted:
		return session, multipart.ErrSessionCompleted
	case multipart.StateCompleting:
		return multipart.Session{}, multipart.ErrSessionInProgress
	case multipart.StateInitiated:
		if !now.Before(session.ExpiresAt) {
			session.State = multipart.StateAborting
			s.sessions[id] = session
			return multipart.Session{}, multipart.ErrSessionExpired
		}
		operationToken := uuid.New()
		session.State, session.OperationToken, session.CompletionStartedAt = multipart.StateCompleting, &operationToken, &now
		s.sessions[id] = session
		return session, nil
	default:
		return multipart.Session{}, multipart.ErrSessionState
	}
}

func (s *httpMultipartSessionStore) BeginReconciliation(_ context.Context, id uuid.UUID, now time.Time) (multipart.Session, error) {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.State != multipart.StateAborting {
		return multipart.Session{}, multipart.ErrSessionState
	}
	operationToken := uuid.New()
	session.State, session.OperationToken, session.CompletionStartedAt = multipart.StateCompleting, &operationToken, &now
	s.sessions[id] = session
	return session, nil
}

func (s *httpMultipartSessionStore) BeginAbort(_ context.Context, id, ownerID uuid.UUID, now time.Time) (multipart.Session, error) {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.OwnerUserID != ownerID {
		return multipart.Session{}, multipart.ErrSessionNotFound
	}
	if session.State == multipart.StateCompleted {
		return session, multipart.ErrSessionCompleted
	}
	if session.State == multipart.StateAborted || session.State == multipart.StateExpired {
		return session, nil
	}
	if session.State == multipart.StateCompleting {
		return multipart.Session{}, multipart.ErrSessionState
	}
	session.State = multipart.StateAborting
	s.sessions[id] = session
	_ = now
	return session, nil
}

func (s *httpMultipartSessionStore) ResetCompletion(_ context.Context, id, operationToken uuid.UUID, message string) error {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.State != multipart.StateCompleting || session.OperationToken == nil || *session.OperationToken != operationToken {
		return multipart.ErrSessionState
	}
	session.State, session.OperationToken, session.CompletionStartedAt = multipart.StateInitiated, nil, nil
	session.LastError = &message
	s.sessions[id] = session
	return nil
}

func (s *httpMultipartSessionStore) MarkCompleted(_ context.Context, id, operationToken uuid.UUID, at time.Time) error {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.State != multipart.StateCompleting || session.OperationToken == nil || *session.OperationToken != operationToken {
		return multipart.ErrSessionState
	}
	session.State, session.CompletedAt, session.OperationToken, session.CompletionStartedAt = multipart.StateCompleted, &at, nil, nil
	s.sessions[id] = session
	return nil
}

func (s *httpMultipartSessionStore) MarkAborted(_ context.Context, id uuid.UUID, at time.Time) error {
	session, err := s.FindSession(context.Background(), id)
	if err != nil || session.State != multipart.StateAborting {
		return multipart.ErrSessionState
	}
	session.State, session.AbortedAt = multipart.StateAborted, &at
	s.sessions[id] = session
	return nil
}

func (s *httpMultipartSessionStore) MarkFailed(_ context.Context, id, operationToken uuid.UUID, message string) error {
	session, err := s.FindSession(context.Background(), id)
	if err != nil {
		return err
	}
	if session.State == multipart.StateCompleting && (session.OperationToken == nil || *session.OperationToken != operationToken) {
		return multipart.ErrSessionState
	}
	session.State, session.LastError, session.OperationToken, session.CompletionStartedAt = multipart.StateFailed, &message, nil, nil
	s.sessions[id] = session
	return nil
}

func (s *httpMultipartSessionStore) ClaimExpired(context.Context, time.Time, time.Time, int) ([]multipart.Session, error) {
	return nil, nil
}

func (s *httpMultipartSessionStore) MarkExpired(context.Context, uuid.UUID, string, time.Time) error {
	return nil
}

var _ dataset.Store = (*httpDatasetStore)(nil)
var _ multipart.Store = (*httpMultipartSessionStore)(nil)
var _ storage.ObjectStore = (*httpMultipartObjectStore)(nil)
var _ storage.MultipartStore = (*httpMultipartObjectStore)(nil)
