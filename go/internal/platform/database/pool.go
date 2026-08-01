package database

import (
	"context"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseDSN())
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	poolConfig.MaxConns = cfg.DatabaseMaxConns
	poolConfig.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	return pool, nil
}
