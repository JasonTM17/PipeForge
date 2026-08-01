package quality

import (
	"context"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/google/uuid"
)

type Service struct {
	Store    Store
	Datasets dataset.Store
}

func NewService(store Store, datasets dataset.Store) (*Service, error) {
	if store == nil || datasets == nil {
		return nil, errors.New("quality service dependencies are not configured")
	}
	return &Service{Store: store, Datasets: datasets}, nil
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, request CreateRequest) (Rule, error) {
	if err := s.requireConfigured(); err != nil {
		return Rule{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return Rule{}, err
	}
	datasetItem, err := s.authorizeDataset(ctx, principal, request.DatasetID, authz.ScopeDatasetsWrite)
	if err != nil {
		return Rule{}, err
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	definition, err := ValidateDefinition(Definition{
		Name: request.Name, ColumnScope: request.ColumnScope, RuleType: request.RuleType,
		Configuration: request.Configuration, Severity: request.Severity, Enabled: enabled,
	})
	if err != nil {
		return Rule{}, err
	}
	request.Name, request.ColumnScope, request.RuleType = definition.Name, definition.ColumnScope, definition.RuleType
	request.Configuration, request.Severity, request.Enabled = definition.Configuration, definition.Severity, &definition.Enabled
	request.DatasetID = datasetItem.ID
	return s.Store.Create(ctx, datasetItem.OwnerUserID, principal.UserID, request)
}

func (s *Service) List(ctx context.Context, principal auth.Principal, datasetID *uuid.UUID, page, pageSize int) (Page, error) {
	if err := s.requireConfigured(); err != nil {
		return Page{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsRead); err != nil {
		return Page{}, err
	}
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return Page{}, err
	}
	if datasetID != nil {
		if _, err := s.authorizeDataset(ctx, principal, *datasetID, authz.ScopeDatasetsRead); err != nil {
			return Page{}, err
		}
	}
	var ownerID *uuid.UUID
	if principal.Role != auth.RoleAdmin {
		ownerID = &principal.UserID
	}
	return s.Store.List(ctx, ListQuery{OwnerUserID: ownerID, DatasetID: datasetID, Page: page, PageSize: pageSize})
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, ruleID uuid.UUID) (Rule, error) {
	if err := s.requireConfigured(); err != nil {
		return Rule{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsRead); err != nil {
		return Rule{}, err
	}
	rule, err := s.Store.Get(ctx, ruleID)
	if err != nil {
		return Rule{}, err
	}
	if _, err := s.authorizeDataset(ctx, principal, rule.DatasetID, authz.ScopeDatasetsRead); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (s *Service) Update(ctx context.Context, principal auth.Principal, ruleID uuid.UUID, request UpdateRequest) (Rule, error) {
	if err := s.requireConfigured(); err != nil {
		return Rule{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return Rule{}, err
	}
	existing, err := s.Store.Get(ctx, ruleID)
	if err != nil {
		return Rule{}, err
	}
	if _, err := s.authorizeDataset(ctx, principal, existing.DatasetID, authz.ScopeDatasetsWrite); err != nil {
		return Rule{}, err
	}
	definition := Definition{
		Name: existing.Name, ColumnScope: existing.ColumnScope, RuleType: existing.RuleType,
		Configuration: existing.Configuration, Severity: existing.Severity, Enabled: existing.Enabled,
	}
	if request.Name != nil {
		definition.Name = *request.Name
	}
	if request.ColumnScope != nil {
		definition.ColumnScope = *request.ColumnScope
	}
	if request.RuleType != nil {
		definition.RuleType = *request.RuleType
	}
	if request.Configuration != nil {
		definition.Configuration = *request.Configuration
	}
	if request.Severity != nil {
		definition.Severity = *request.Severity
	}
	if request.Enabled != nil {
		definition.Enabled = *request.Enabled
	}
	validated, err := ValidateDefinition(definition)
	if err != nil {
		return Rule{}, err
	}
	request.Name = &validated.Name
	request.ColumnScope = &validated.ColumnScope
	request.RuleType = &validated.RuleType
	request.Configuration = &validated.Configuration
	request.Severity = &validated.Severity
	request.Enabled = &validated.Enabled
	return s.Store.Update(ctx, ruleID, principal.UserID, request)
}

func (s *Service) Delete(ctx context.Context, principal auth.Principal, ruleID uuid.UUID) error {
	if err := s.requireConfigured(); err != nil {
		return err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return err
	}
	rule, err := s.Store.Get(ctx, ruleID)
	if err != nil {
		return err
	}
	if _, err := s.authorizeDataset(ctx, principal, rule.DatasetID, authz.ScopeDatasetsWrite); err != nil {
		return err
	}
	return s.Store.Delete(ctx, ruleID, principal.UserID)
}

func (s *Service) authorizeDataset(ctx context.Context, principal auth.Principal, datasetID uuid.UUID, scope string) (dataset.Dataset, error) {
	if datasetID == uuid.Nil {
		return dataset.Dataset{}, fmt.Errorf("%w: dataset is required", ErrInvalidInput)
	}
	item, err := s.Datasets.FindDataset(ctx, datasetID)
	if err != nil {
		return dataset.Dataset{}, err
	}
	if item.DeletedAt != nil || !authz.CanAccessOwner(principal, item.OwnerUserID, scope) {
		return dataset.Dataset{}, authz.ErrForbidden
	}
	return item, nil
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Store == nil || s.Datasets == nil {
		return errors.New("quality service is not configured")
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
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and 100", ErrInvalidInput)
	}
	return page, pageSize, nil
}
