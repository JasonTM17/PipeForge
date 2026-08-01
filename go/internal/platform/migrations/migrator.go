package migrations

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JasonTM17/PipeForge/go/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationNamePattern = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.sql$`)

type Migration struct {
	Version uint64
	Name    string
	SQL     string
}

func Load() ([]Migration, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	loaded := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationNamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("migration filename %q must match NNNNNN_name.sql", entry.Name())
		}
		version, parseErr := strconv.ParseUint(match[1], 10, 64)
		if parseErr != nil || version == 0 {
			return nil, fmt.Errorf("migration filename %q has invalid version", entry.Name())
		}
		sql, readErr := migrations.Files.ReadFile(filepath.ToSlash(entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), readErr)
		}
		loaded = append(loaded, Migration{Version: version, Name: entry.Name(), SQL: string(sql)})
	}

	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Version < loaded[j].Version })
	for i := 1; i < len(loaded); i++ {
		if loaded[i-1].Version == loaded[i].Version {
			return nil, fmt.Errorf("duplicate migration version %d", loaded[i].Version)
		}
	}
	return loaded, nil
}

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	loaded, err := Load()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version BIGINT PRIMARY KEY,
            name TEXT NOT NULL,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, migration := range loaded {
		if err := applyOne(ctx, pool, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, migration Migration) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize migration runners so two control-plane instances cannot both
	// observe an unapplied version and race on the unique-key insert.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('pipeforge:schema_migrations'))`); err != nil {
		return fmt.Errorf("lock migrations for %s: %w", migration.Name, err)
	}

	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.Version).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", migration.Name, err)
	}
	if applied {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit already-applied migration %s: %w", migration.Name, err)
		}
		return nil
	}

	if _, err := tx.Exec(ctx, strings.TrimSpace(migration.SQL)); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.Name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.Name, err)
	}
	return nil
}
