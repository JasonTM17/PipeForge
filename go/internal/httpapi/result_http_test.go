package httpapi

import (
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/result"
	"github.com/google/uuid"
)

func TestArtifactHTTPRoutesEnforceOwnershipAndStreamCanonicalObjects(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	ownerTokens := registerHTTPUser(t, identityService, "artifact-owner@example.com")
	otherTokens := registerHTTPUser(t, identityService, "artifact-other@example.com")

	artifactID := uuid.New()
	store := &httpResultStore{artifacts: map[uuid.UUID]result.Artifact{
		artifactID: {ID: artifactID, OwnerUserID: ownerTokens.User.ID, JobID: uuid.New(), AttemptID: uuid.New(), LeaseID: uuid.New(), Kind: "profile", ObjectKey: "reports/profile.json", SizeBytes: 7, ContentType: "application/json", State: "CANONICAL"},
	}}
	service, err := result.NewService(store, httpResultObjects{content: "profile"})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Result: service})

	listed := performJSONRequest(router, "GET", "/api/v1/artifacts?page=1&pageSize=10", "", ownerTokens.AccessToken)
	if listed.Code != 200 || !containsJSON(listed.Body.String(), `"total":1`) || !containsJSON(listed.Body.String(), artifactID.String()) {
		t.Fatalf("artifact list failed: %d %s", listed.Code, listed.Body.String())
	}
	metadata := performJSONRequest(router, "GET", "/v1/artifacts/"+artifactID.String(), "", ownerTokens.AccessToken)
	if metadata.Code != 200 || !containsJSON(metadata.Body.String(), `"kind":"profile"`) {
		t.Fatalf("artifact metadata failed: %d %s", metadata.Code, metadata.Body.String())
	}
	download := performJSONRequest(router, "GET", "/v1/artifacts/"+artifactID.String()+"/download", "", ownerTokens.AccessToken)
	if download.Code != 200 || download.Body.String() != "profile" || download.Header().Get("Content-Length") != "7" {
		t.Fatalf("artifact download failed: %d headers=%v body=%q", download.Code, download.Header(), download.Body.String())
	}
	head := performJSONRequest(router, "HEAD", "/v1/artifacts/"+artifactID.String()+"/download", "", ownerTokens.AccessToken)
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "7" {
		t.Fatalf("artifact HEAD failed: %d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
	crossOwner := performJSONRequest(router, "GET", "/v1/artifacts/"+artifactID.String(), "", otherTokens.AccessToken)
	if crossOwner.Code != 403 {
		t.Fatalf("cross-owner artifact access was not denied: %d %s", crossOwner.Code, crossOwner.Body.String())
	}
	invalid := performJSONRequest(router, "GET", "/v1/artifacts?pageSize=101", "", ownerTokens.AccessToken)
	if invalid.Code != 400 {
		t.Fatalf("invalid artifact pagination was not rejected: %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestProgressHTTPRoutesEnforceOwnershipAndExposeHistory(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	ownerTokens := registerHTTPUser(t, identityService, "progress-owner@example.com")
	otherTokens := registerHTTPUser(t, identityService, "progress-other@example.com")
	jobID := uuid.New()
	store := &httpResultStore{
		artifacts: map[uuid.UUID]result.Artifact{},
		progress: map[uuid.UUID]result.ProgressSnapshot{
			jobID: {OwnerUserID: ownerTokens.User.ID, JobID: jobID, AttemptID: uuid.New(), Stage: "PROCESS", ProcessedRows: 20, ProgressPercent: 50, Throughput: 10},
		},
	}
	service, err := result.NewService(store, httpResultObjects{content: "profile"})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Result: service})

	latest := performJSONRequest(router, "GET", "/api/v1/jobs/"+jobID.String()+"/progress", "", ownerTokens.AccessToken)
	if latest.Code != 200 || !containsJSON(latest.Body.String(), `"progressPercent":50`) {
		t.Fatalf("progress snapshot failed: %d %s", latest.Code, latest.Body.String())
	}
	history := performJSONRequest(router, "GET", "/v1/jobs/"+jobID.String()+"/progress/history?page=1&pageSize=10", "", ownerTokens.AccessToken)
	if history.Code != 200 || !containsJSON(history.Body.String(), `"total":1`) {
		t.Fatalf("progress history failed: %d %s", history.Code, history.Body.String())
	}
	crossOwner := performJSONRequest(router, "GET", "/v1/jobs/"+jobID.String()+"/progress", "", otherTokens.AccessToken)
	if crossOwner.Code != 404 {
		t.Fatalf("cross-owner progress access was not hidden: %d %s", crossOwner.Code, crossOwner.Body.String())
	}
}

type httpResultStore struct {
	mu        sync.Mutex
	artifacts map[uuid.UUID]result.Artifact
	progress  map[uuid.UUID]result.ProgressSnapshot
}

func (s *httpResultStore) Process(context.Context, queue.Envelope) (result.Outcome, error) {
	return result.Outcome{Status: result.OutcomeApplied}, nil
}

func (s *httpResultStore) ListArtifacts(_ context.Context, query result.ArtifactListQuery) (result.ArtifactPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]result.Artifact, 0, len(s.artifacts))
	for _, item := range s.artifacts {
		if item.State != "CANONICAL" || (query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID) || (query.JobID != nil && item.JobID != *query.JobID) || (query.Kind != "" && item.Kind != query.Kind) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })
	return result.ArtifactPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: int64(len(items))}, nil
}

func (s *httpResultStore) GetArtifact(_ context.Context, artifactID uuid.UUID) (result.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.artifacts[artifactID]
	if !ok {
		return result.Artifact{}, result.ErrArtifactNotFound
	}
	return item, nil
}

func (s *httpResultStore) GetProgress(_ context.Context, query result.ProgressQuery) (result.ProgressSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.progress[query.JobID]
	if !ok || query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID {
		return result.ProgressSnapshot{}, result.ErrProgressNotFound
	}
	return item, nil
}

func (s *httpResultStore) ListProgress(_ context.Context, query result.ProgressQuery) (result.ProgressPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.progress[query.JobID]
	if !ok || query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID {
		return result.ProgressPage{Items: []result.ProgressSnapshot{}, Page: query.Page, PageSize: query.PageSize}, nil
	}
	return result.ProgressPage{Items: []result.ProgressSnapshot{item}, Page: query.Page, PageSize: query.PageSize, Total: 1}, nil
}

type httpResultObjects struct{ content string }

func (o httpResultObjects) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(o.content)), nil
}
