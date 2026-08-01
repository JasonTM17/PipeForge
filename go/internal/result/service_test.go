package result

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

type fakeResultStore struct {
	artifacts map[uuid.UUID]Artifact
}

func (s *fakeResultStore) Process(context.Context, queue.Envelope) (Outcome, error) {
	return Outcome{Status: OutcomeApplied}, nil
}

func (s *fakeResultStore) ListArtifacts(_ context.Context, query ArtifactListQuery) (ArtifactPage, error) {
	items := make([]Artifact, 0)
	for _, item := range s.artifacts {
		if item.State != "CANONICAL" || (query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID) || (query.JobID != nil && item.JobID != *query.JobID) || (query.Kind != "" && item.Kind != query.Kind) {
			continue
		}
		items = append(items, item)
	}
	return ArtifactPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: int64(len(items))}, nil
}

func (s *fakeResultStore) GetArtifact(_ context.Context, artifactID uuid.UUID) (Artifact, error) {
	item, ok := s.artifacts[artifactID]
	if !ok {
		return Artifact{}, ErrArtifactNotFound
	}
	return item, nil
}

type fakeObjectReader struct{ content string }

func (r fakeObjectReader) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}

func TestServiceEnforcesScopeOwnerAndCanonicalState(t *testing.T) {
	ownerID, otherID := uuid.New(), uuid.New()
	canonicalID, stagedID := uuid.New(), uuid.New()
	store := &fakeResultStore{artifacts: map[uuid.UUID]Artifact{
		canonicalID: {ID: canonicalID, OwnerUserID: ownerID, ObjectKey: "reports/profile.json", State: "CANONICAL", SizeBytes: 7},
		stagedID:    {ID: stagedID, OwnerUserID: ownerID, ObjectKey: "reports/staged.json", State: "STAGED", SizeBytes: 6},
	}}
	service, err := NewService(store, fakeObjectReader{content: "profile"})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	owner := auth.Principal{UserID: ownerID, Role: auth.RoleUser, Scopes: authz.DefaultUserScopes()}
	other := auth.Principal{UserID: otherID, Role: auth.RoleUser, Scopes: authz.DefaultUserScopes()}

	page, err := service.ListArtifacts(context.Background(), owner, nil, "", 0, 0)
	if err != nil || page.Page != 1 || page.PageSize != 20 || page.Total != 1 {
		t.Fatalf("unexpected artifact list: page=%+v err=%v", page, err)
	}
	if _, err := service.GetArtifact(context.Background(), other, canonicalID); err == nil || !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("cross-owner artifact access was not denied: %v", err)
	}
	if _, err := service.GetArtifact(context.Background(), owner, stagedID); err == nil || !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("staged artifact was exposed: %v", err)
	}
	_, reader, err := service.OpenArtifact(context.Background(), owner, canonicalID)
	if err != nil {
		t.Fatalf("OpenArtifact returned error: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "profile" {
		t.Fatalf("unexpected artifact body: %q err=%v", body, err)
	}
}
