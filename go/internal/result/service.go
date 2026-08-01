package result

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/google/uuid"
)

type Service struct {
	Store   Store
	Objects ObjectReader
}

func NewService(store Store, objects ObjectReader) (*Service, error) {
	if store == nil {
		return nil, errors.New("result service requires a store")
	}
	return &Service{Store: store, Objects: objects}, nil
}

func (s *Service) Process(ctx context.Context, envelope queue.Envelope) (Outcome, error) {
	if err := s.requireConfigured(); err != nil {
		return Outcome{}, err
	}
	return s.Store.Process(ctx, envelope)
}

func (s *Service) ListArtifacts(ctx context.Context, principal auth.Principal, jobID *uuid.UUID, kind string, page, pageSize int) (ArtifactPage, error) {
	if err := s.requireConfigured(); err != nil {
		return ArtifactPage{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeArtifactsRead); err != nil {
		return ArtifactPage{}, err
	}
	page, pageSize, err := normalizeArtifactPagination(page, pageSize)
	if err != nil {
		return ArtifactPage{}, err
	}
	var ownerID *uuid.UUID
	if principal.Role != auth.RoleAdmin {
		ownerID = &principal.UserID
	}
	return s.Store.ListArtifacts(ctx, ArtifactListQuery{OwnerUserID: ownerID, JobID: jobID, Kind: kind, Page: page, PageSize: pageSize})
}

func (s *Service) GetArtifact(ctx context.Context, principal auth.Principal, artifactID uuid.UUID) (Artifact, error) {
	if err := s.requireConfigured(); err != nil {
		return Artifact{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeArtifactsRead); err != nil {
		return Artifact{}, err
	}
	item, err := s.Store.GetArtifact(ctx, artifactID)
	if err != nil {
		return Artifact{}, err
	}
	if item.State != "CANONICAL" {
		return Artifact{}, ErrArtifactNotFound
	}
	if !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeArtifactsRead) {
		return Artifact{}, authz.ErrForbidden
	}
	return item, nil
}

func (s *Service) OpenArtifact(ctx context.Context, principal auth.Principal, artifactID uuid.UUID) (Artifact, io.ReadCloser, error) {
	item, err := s.GetArtifact(ctx, principal, artifactID)
	if err != nil {
		return Artifact{}, nil, err
	}
	if item.SizeBytes > MaxArtifactDownloadBytes {
		return Artifact{}, nil, ErrArtifactTooLarge
	}
	if s.Objects == nil {
		return Artifact{}, nil, ErrArtifactUnavailable
	}
	reader, err := s.Objects.Get(ctx, item.ObjectKey)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return Artifact{}, nil, ErrArtifactUnavailable
	}
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("open artifact object: %w", err)
	}
	return item, reader, nil
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Store == nil {
		return errors.New("result service is not configured")
	}
	return nil
}

func normalizeArtifactPagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: page and pageSize must be positive and pageSize must not exceed 100", ErrInvalidInput)
	}
	return page, pageSize, nil
}
