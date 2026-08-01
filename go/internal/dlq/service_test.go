package dlq

import (
	"context"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/google/uuid"
)

type fakeDLQStore struct {
	item   Record
	called bool
}

func (s *fakeDLQStore) List(context.Context, ListQuery) (Page, error) {
	return Page{Items: []Record{s.item}, Page: 1, PageSize: 20, Total: 1}, nil
}

func (s *fakeDLQStore) Get(context.Context, uuid.UUID) (Record, error) { return s.item, nil }

func (s *fakeDLQStore) Replay(_ context.Context, command ReplayCommand) (ReplayResult, error) {
	s.called = true
	return ReplayResult{Record: s.item, JobID: s.item.JobID, AttemptID: uuid.New()}, nil
}

func TestServiceScopesDeadLetterListAndReplayToOwner(t *testing.T) {
	ownerID, otherID := uuid.New(), uuid.New()
	store := &fakeDLQStore{item: Record{ID: uuid.New(), OwnerUserID: ownerID, JobID: uuid.New(), ErrorCode: "LEASE_EXPIRED", ErrorMessage: "worker lease expired"}}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	owner := auth.Principal{UserID: ownerID, Role: auth.RoleUser, Scopes: authz.DefaultUserScopes()}
	other := auth.Principal{UserID: otherID, Role: auth.RoleUser, Scopes: authz.DefaultUserScopes()}
	if page, err := service.List(context.Background(), owner, 0, 0, true); err != nil || page.Total != 1 {
		t.Fatalf("owner list failed: page=%+v err=%v", page, err)
	}
	if _, err := service.Replay(context.Background(), other, ReplayCommand{RecordID: store.item.ID}); err == nil {
		t.Fatal("cross-owner replay was accepted")
	}
	if _, err := service.Replay(context.Background(), owner, ReplayCommand{RecordID: store.item.ID}); err != nil {
		t.Fatalf("owner replay failed: %v", err)
	}
	if !store.called {
		t.Fatal("replay store was not called")
	}
}
