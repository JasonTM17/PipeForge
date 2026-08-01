package job

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/google/uuid"
)

type Service struct {
	Store    Store
	Queue    QueueStore
	Datasets dataset.Store
}

func NewService(store Store, datasets dataset.Store) (*Service, error) {
	if store == nil || datasets == nil {
		return nil, errors.New("job service dependencies are not configured")
	}
	queueStore, _ := store.(QueueStore)
	return &Service{Store: store, Queue: queueStore, Datasets: datasets}, nil
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, versionID uuid.UUID, request CreateRequest, idempotencyKey, requestID string) (Job, bool, error) {
	if err := s.requireConfigured(); err != nil {
		return Job{}, false, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsWrite); err != nil {
		return Job{}, false, err
	}
	if versionID == uuid.Nil {
		return Job{}, false, fmt.Errorf("%w: dataset version is required", ErrInvalidInput)
	}
	version, err := s.Datasets.FindVersionByID(ctx, versionID)
	if err != nil {
		return Job{}, false, err
	}
	datasetItem, err := s.Datasets.FindDataset(ctx, version.DatasetID)
	if err != nil {
		return Job{}, false, err
	}
	if datasetItem.DeletedAt != nil || !authz.CanAccessOwner(principal, datasetItem.OwnerUserID, authz.ScopeJobsWrite) {
		return Job{}, false, authz.ErrForbidden
	}
	if version.State != "AVAILABLE" {
		return Job{}, false, ErrVersionUnavailable
	}
	request.Priority, request.MaxAttempts = normalizeCreateDefaults(request.Priority, request.MaxAttempts)
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return Job{}, false, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return s.Store.Create(ctx, CreateCommand{
		OwnerUserID: datasetItem.OwnerUserID, ActorUserID: principal.UserID, DatasetVersionID: versionID,
		Operations: request.Operations, IdempotencyKey: key, Priority: request.Priority,
		MaxAttempts: request.MaxAttempts, TraceID: requestID, CausationID: requestID,
	})
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, jobID uuid.UUID) (Job, error) {
	if err := s.requireConfigured(); err != nil {
		return Job{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsRead); err != nil {
		return Job{}, err
	}
	item, err := s.Store.Get(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeJobsRead) {
		return Job{}, authz.ErrForbidden
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, principal auth.Principal, page, pageSize int, state string) (JobPage, error) {
	if err := s.requireConfigured(); err != nil {
		return JobPage{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsRead); err != nil {
		return JobPage{}, err
	}
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return JobPage{}, err
	}
	state = strings.TrimSpace(state)
	if state != "" && !IsKnownState(state) {
		return JobPage{}, fmt.Errorf("%w: unknown job state", ErrInvalidInput)
	}
	var ownerID *uuid.UUID
	if principal.Role != auth.RoleAdmin {
		ownerID = &principal.UserID
	}
	return s.Store.List(ctx, ListQuery{OwnerUserID: ownerID, Page: page, PageSize: pageSize, State: state})
}

func (s *Service) Cancel(ctx context.Context, principal auth.Principal, jobID uuid.UUID, reason, requestID string) (Job, error) {
	if err := s.requireConfigured(); err != nil {
		return Job{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsWrite); err != nil {
		return Job{}, err
	}
	item, err := s.Store.Get(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeJobsWrite) {
		return Job{}, authz.ErrForbidden
	}
	return s.Store.RequestCancel(ctx, CancelCommand{
		JobID: jobID, ActorUserID: principal.UserID, Reason: reason, TraceID: requestID,
		RequestID: requestID, AllowCrossOwner: principal.Role == auth.RoleAdmin,
	})
}

func (s *Service) Retry(ctx context.Context, principal auth.Principal, jobID uuid.UUID, requestID string) (Job, error) {
	if err := s.requireConfigured(); err != nil {
		return Job{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeJobsWrite); err != nil {
		return Job{}, err
	}
	item, err := s.Store.Get(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeJobsWrite) {
		return Job{}, authz.ErrForbidden
	}
	return s.Store.RequestRetry(ctx, RetryCommand{
		JobID: jobID, ActorUserID: principal.UserID, TraceID: requestID,
		RequestID: requestID, AllowCrossOwner: principal.Role == auth.RoleAdmin,
	})
}

func normalizeCreateDefaults(priority, maxAttempts int16) (int16, int16) {
	if maxAttempts == 0 {
		maxAttempts = DefaultMaxAttempts
	}
	return priority, maxAttempts
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > MaxIdempotencyKeySize || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%w: idempotency key must be 1-%d bytes and contain no newlines", ErrInvalidInput, MaxIdempotencyKeySize)
	}
	return value, nil
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and 100", ErrInvalidInput)
	}
	return page, pageSize, nil
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Store == nil || s.Datasets == nil {
		return errors.New("job service is not configured")
	}
	return nil
}
