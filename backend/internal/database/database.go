// Package database owns the PostgreSQL connection pool (pgxpool) and the
// migration runner. Migrations are embedded from backend/migrations and
// applied in filename order, tracked in schema_migrations.
package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect opens a pool and verifies it with a ping. The caller must call
// Close. Connection errors are returned (callers decide fallback vs fatal).
func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases the pool.
func (d *DB) Close() { d.Pool.Close() }

// Health runs a trivial query; nil means the database is reachable.
func (d *DB) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var one int
	return d.Pool.QueryRow(ctx, "SELECT 1").Scan(&one)
}

// Migrate applies the given migration filesystem (backend/migrations)
// in filename order. The whole run happens inside ONE transaction on a
// single pooled connection holding an advisory lock, so two processes
// starting at the same time (e.g. api + worker under compose) cannot
// race: the second waits, then sees every file already applied.
// Each file is recorded in schema_migrations; already applied files
// are skipped, so Migrate is safe to run on every startup.
func Migrate(ctx context.Context, db *DB, migrationsFS fs.FS) ([]string, error) {
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection for migrations: %w", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin migrations: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialize concurrent migrators (api + worker starting together).
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(727271)"); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var applied []string
	for _, name := range names {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", name).Scan(&exists); err != nil {
			return applied, fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}
		sql, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return applied, fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return applied, fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", name); err != nil {
			return applied, fmt.Errorf("record migration %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	if err := tx.Commit(ctx); err != nil {
		return applied, fmt.Errorf("commit migrations: %w", err)
	}
	return applied, nil
}
