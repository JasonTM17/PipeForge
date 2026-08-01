package lease

import (
	"context"
	"errors"
)

type Service struct{ Store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("lease service requires a store")
	}
	return &Service{Store: store}, nil
}

func (s *Service) Acquire(ctx context.Context, command AcquireCommand) (Lease, error) {
	if s == nil || s.Store == nil {
		return Lease{}, errors.New("lease service is not configured")
	}
	return s.Store.Acquire(ctx, command)
}

func (s *Service) Renew(ctx context.Context, command RenewCommand) (Lease, error) {
	if s == nil || s.Store == nil {
		return Lease{}, errors.New("lease service is not configured")
	}
	return s.Store.Renew(ctx, command)
}
