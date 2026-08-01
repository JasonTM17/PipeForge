package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/google/uuid"
)

func TestJobHTTPLifecycleIdempotencyAndOwnership(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	ownerTokens := registerHTTPUser(t, identityService, "jobs-owner@example.com")
	otherTokens := registerHTTPUser(t, identityService, "jobs-other@example.com")
	versionID := uuid.New()
	datasetID := uuid.New()
	datasetStore.datasets[datasetID] = dataset.Dataset{ID: datasetID, OwnerUserID: ownerTokens.User.ID, Name: "jobs", State: "AVAILABLE"}
	datasetStore.versions[versionID] = dataset.DatasetVersion{ID: versionID, DatasetID: datasetID, State: "AVAILABLE", CreatedAt: time.Now().UTC()}
	jobStore := newHTTPJobStore()
	jobService, err := job.NewService(jobStore, datasetStore)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Job: jobService})

	body := `{"operations":[{"type":"PROFILE_DATASET","config":{}},{"type":"CHECK_MISSING_VALUES","config":{"columns":["email"]}}]}`
	created := performJobJSONRequest(router, http.MethodPost, "/v1/datasets/"+versionID.String()+"/jobs", body, ownerTokens.AccessToken, "job-key-1")
	if created.Code != http.StatusCreated {
		t.Fatalf("job creation failed: %d %s", created.Code, created.Body.String())
	}
	var first job.Job
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode created job: %v", err)
	}
	replayed := performJobJSONRequest(router, http.MethodPost, "/v1/datasets/"+versionID.String()+"/jobs", body, ownerTokens.AccessToken, "job-key-1")
	var replayedJob job.Job
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayedJob); err != nil {
		t.Fatalf("decode replayed job: %v", err)
	}
	if replayed.Code != http.StatusOK || replayedJob.ID != first.ID {
		t.Fatalf("idempotent replay failed: %d %s", replayed.Code, replayed.Body.String())
	}
	conflict := performJobJSONRequest(router, http.MethodPost, "/v1/datasets/"+versionID.String()+"/jobs", `{"operations":[{"type":"PROFILE_DATASET","config":{}}]}`, ownerTokens.AccessToken, "job-key-1")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"IDEMPOTENCY_CONFLICT"`) {
		t.Fatalf("idempotency conflict was not deterministic: %d %s", conflict.Code, conflict.Body.String())
	}

	list := performJSONRequest(router, http.MethodGet, "/v1/jobs?page=1&pageSize=10", "", ownerTokens.AccessToken)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"total":1`) {
		t.Fatalf("job list failed: %d %s", list.Code, list.Body.String())
	}
	otherGet := performJSONRequest(router, http.MethodGet, "/v1/jobs/"+first.ID.String(), "", otherTokens.AccessToken)
	if otherGet.Code != http.StatusForbidden {
		t.Fatalf("cross-owner job access was not denied: %d %s", otherGet.Code, otherGet.Body.String())
	}
	invalid := performJobJSONRequest(router, http.MethodPost, "/v1/dataset-versions/"+versionID.String()+"/jobs", `{"operations":[{"type":"DROP_TABLE","config":{}}]}`, ownerTokens.AccessToken, "job-key-2")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid operation was not rejected: %d %s", invalid.Code, invalid.Body.String())
	}
}

func performJobJSONRequest(router http.Handler, method, path, body, bearer, idempotencyKey string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

type httpJobStore struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]job.Job
	keys map[string]uuid.UUID
}

func newHTTPJobStore() *httpJobStore {
	return &httpJobStore{jobs: make(map[uuid.UUID]job.Job), keys: make(map[string]uuid.UUID)}
}

func (s *httpJobStore) Create(_ context.Context, command job.CreateCommand) (job.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fingerprint, err := job.ValidateCreateCommand(command)
	if err != nil {
		return job.Job{}, false, err
	}
	key := command.OwnerUserID.String() + ":" + command.IdempotencyKey
	if command.IdempotencyKey != "" {
		if existingID, ok := s.keys[key]; ok {
			existing := s.jobs[existingID]
			if existing.RequestFingerprint != fingerprint {
				return job.Job{}, false, job.ErrIdempotencyConflict
			}
			return existing, true, nil
		}
	}
	now := time.Now().UTC()
	item := job.Job{ID: uuid.New(), OwnerUserID: command.OwnerUserID, DatasetVersionID: command.DatasetVersionID, State: job.StateQueued, Operations: command.Operations, RequestFingerprint: fingerprint, Priority: command.Priority, MaxAttempts: command.MaxAttempts, CreatedAt: now, UpdatedAt: now, QueuedAt: &now}
	s.jobs[item.ID] = item
	if command.IdempotencyKey != "" {
		s.keys[key] = item.ID
	}
	return item, false, nil
}

func (s *httpJobStore) Get(_ context.Context, jobID uuid.UUID) (job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.jobs[jobID]
	if !ok {
		return job.Job{}, job.ErrJobNotFound
	}
	return item, nil
}

func (s *httpJobStore) List(_ context.Context, query job.ListQuery) (job.JobPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]job.Job, 0)
	for _, item := range s.jobs {
		if query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID {
			continue
		}
		if query.State != "" && item.State != query.State {
			continue
		}
		items = append(items, item)
	}
	return job.JobPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: int64(len(items))}, nil
}

func (s *httpJobStore) RequestCancel(_ context.Context, command job.CancelCommand) (job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.jobs[command.JobID]
	if !ok {
		return job.Job{}, job.ErrJobNotFound
	}
	if item.State == job.StateCancelRequested || item.State == job.StateCancelled {
		return item, nil
	}
	if err := job.ValidateTransition(item.State, job.StateCancelRequested); err != nil {
		return job.Job{}, job.ErrJobState
	}
	item.State = job.StateCancelRequested
	item.UpdatedAt = time.Now().UTC()
	s.jobs[item.ID] = item
	return item, nil
}

func (s *httpJobStore) RequestRetry(_ context.Context, command job.RetryCommand) (job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.jobs[command.JobID]
	if !ok {
		return job.Job{}, job.ErrJobNotFound
	}
	if item.State != job.StateFailedRetryable && item.State != job.StateFailedPermanent && item.State != job.StateDeadLettered {
		return job.Job{}, job.ErrJobState
	}
	item.State = job.StateQueued
	item.UpdatedAt = time.Now().UTC()
	s.jobs[item.ID] = item
	return item, nil
}

func registerHTTPUser(t *testing.T, service *identity.Service, email string) tokenResponse {
	t.Helper()
	user, pair, err := service.Register(context.Background(), email, "correct horse battery staple", "job-http-test")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	return tokenResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken, TokenType: "Bearer", ExpiresAt: pair.AccessTokenExpiry, User: userResponse{ID: user.ID, Email: user.Email, Role: string(user.Role), Scopes: user.Scopes}}
}

var _ job.Store = (*httpJobStore)(nil)
