// Package store — Postgres-backed token revocation (Phase 7, R3).
//
// PostgresRevoker implements auth.Revoker against the revoked_tokens
// / issued_tokens tables (migration 013). It is the durable backend
// used when Postgres is up: revocations survive an API restart, and
// a revoked token is denied by every API replica sharing the DB.
package store

import (
	"context"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/database"
)

// PostgresRevoker is an auth.Revoker backed by Postgres.
type PostgresRevoker struct {
	db *database.DB
}

// NewPostgresRevoker builds a PostgresRevoker over db.
func NewPostgresRevoker(db *database.DB) *PostgresRevoker {
	return &PostgresRevoker{db: db}
}

var _ auth.Revoker = (*PostgresRevoker)(nil)

func (r *PostgresRevoker) RecordIssued(ctx context.Context, jti, userID string, exp time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO issued_tokens(jti, user_id, expires_at) VALUES($1,$2,$3)
		 ON CONFLICT (jti) DO NOTHING`, jti, userID, exp)
	return err
}

func (r *PostgresRevoker) Revoke(ctx context.Context, jti, userID string, exp time.Time) error {
	if time.Now().After(exp) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO revoked_tokens(jti, user_id, expires_at) VALUES($1,$2,$3)
		 ON CONFLICT (jti) DO NOTHING`, jti, userID, exp)
	return err
}

func (r *PostgresRevoker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var n int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM revoked_tokens WHERE jti=$1 AND expires_at > now()`, jti).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *PostgresRevoker) RevokeUser(ctx context.Context, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Revoke every still-valid jti recorded for the user, then drop
	// the expired issued rows opportunistically.
	tag, err := r.db.Pool.Exec(ctx,
		`INSERT INTO revoked_tokens(jti, user_id, expires_at)
		 SELECT jti, user_id, expires_at FROM issued_tokens
		 WHERE user_id=$1 AND expires_at > now()
		 ON CONFLICT (jti) DO NOTHING`, userID)
	if err != nil {
		return 0, err
	}
	_, _ = r.db.Pool.Exec(ctx, `DELETE FROM issued_tokens WHERE expires_at <= now()`)
	_, _ = r.db.Pool.Exec(ctx, `DELETE FROM revoked_tokens WHERE expires_at <= now()`)
	return int(tag.RowsAffected()), nil
}
