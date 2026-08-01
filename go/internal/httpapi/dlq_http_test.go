package httpapi

import (
	"context"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/dlq"
	"github.com/google/uuid"
)

func TestDeadLetterHTTPRoutesEnforceOwnerReplay(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	ownerTokens := registerHTTPUser(t, identityService, "dlq-owner@example.com")
	otherTokens := registerHTTPUser(t, identityService, "dlq-other@example.com")
	store := &httpDLQStore{item: dlq.Record{ID: uuid.New(), OwnerUserID: ownerTokens.User.ID, JobID: uuid.New(), AttemptID: uuid.New(), AttemptNumber: 5, ErrorCode: "LEASE_EXPIRED", ErrorMessage: "worker lease expired", Retryable: true}}
	service, err := dlq.NewService(store)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, DLQ: service})

	listed := performJSONRequest(router, "GET", "/api/v1/dead-letters?openOnly=true", "", ownerTokens.AccessToken)
	if listed.Code != 200 || !containsJSON(listed.Body.String(), store.item.ID.String()) {
		t.Fatalf("dead-letter list failed: %d %s", listed.Code, listed.Body.String())
	}
	crossOwner := performJSONRequest(router, "POST", "/api/v1/dead-letters/"+store.item.ID.String()+"/replay", "", otherTokens.AccessToken)
	if crossOwner.Code != 403 {
		t.Fatalf("cross-owner replay was not denied: %d %s", crossOwner.Code, crossOwner.Body.String())
	}
	replayed := performJSONRequest(router, "POST", "/v1/dead-letters/"+store.item.ID.String()+"/replay", "", ownerTokens.AccessToken)
	if replayed.Code != 200 || !containsJSON(replayed.Body.String(), `"jobId"`) {
		t.Fatalf("owner replay failed: %d %s", replayed.Code, replayed.Body.String())
	}
}

type httpDLQStore struct{ item dlq.Record }

func (s *httpDLQStore) List(context.Context, dlq.ListQuery) (dlq.Page, error) {
	return dlq.Page{Items: []dlq.Record{s.item}, Page: 1, PageSize: 20, Total: 1}, nil
}

func (s *httpDLQStore) Get(context.Context, uuid.UUID) (dlq.Record, error) { return s.item, nil }

func (s *httpDLQStore) Replay(_ context.Context, _ dlq.ReplayCommand) (dlq.ReplayResult, error) {
	return dlq.ReplayResult{Record: s.item, JobID: s.item.JobID, AttemptID: uuid.New()}, nil
}
