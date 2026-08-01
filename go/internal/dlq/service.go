package dlq

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/google/uuid"
)

type Service struct{ Store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("dead-letter service requires a store")
	}
	return &Service{Store: store}, nil
}

func (s *Service) List(ctx context.Context, principal auth.Principal, page, pageSize int, openOnly bool) (Page, error) {
	if err := s.requireConfigured(); err != nil {
		return Page{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsRead); err != nil {
		return Page{}, err
	}
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return Page{}, err
	}
	var ownerID *uuid.UUID
	if principal.Role != auth.RoleAdmin {
		ownerID = &principal.UserID
	}
	return s.Store.List(ctx, ListQuery{OwnerUserID: ownerID, Page: page, PageSize: pageSize, OpenOnly: openOnly})
}

func (s *Service) Replay(ctx context.Context, principal auth.Principal, command ReplayCommand) (ReplayResult, error) {
	if err := s.requireConfigured(); err != nil {
		return ReplayResult{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsWrite); err != nil {
		return ReplayResult{}, err
	}
	if command.RecordID == uuid.Nil {
		return ReplayResult{}, fmt.Errorf("%w: record ID is required", ErrInvalidInput)
	}
	item, err := s.Store.Get(ctx, command.RecordID)
	if err != nil {
		return ReplayResult{}, err
	}
	if !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeJobsWrite) {
		return ReplayResult{}, authz.ErrForbidden
	}
	command.ActorUserID = principal.UserID
	command.AllowCrossOwner = principal.Role == auth.RoleAdmin
	command.TraceID = strings.TrimSpace(command.TraceID)
	command.RequestID = strings.TrimSpace(command.RequestID)
	return s.Store.Replay(ctx, command)
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Store == nil {
		return errors.New("dead-letter service is not configured")
	}
	return nil
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > MaxPageSize {
		return 0, 0, fmt.Errorf("%w: page and pageSize are invalid", ErrInvalidInput)
	}
	return page, pageSize, nil
}
