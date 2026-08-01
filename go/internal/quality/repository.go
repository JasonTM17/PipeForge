package quality

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("quality repository requires a database")
	}
	return &Repository{pool: pool}, nil
}
