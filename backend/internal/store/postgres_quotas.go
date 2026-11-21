// Postgres implementation for the Milestone 9 per-role quotas
// (migration 006). GetQuota returns store.ErrNotFound when no row
// is configured for the role; callers fall back to
// quota.DefaultLimits.
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/cloudforge/cloudforge/backend/internal/database"
)

// PostgresQuotaStore implements QuotaStore against PostgreSQL.
type PostgresQuotaStore struct{ db *database.DB }

// NewPostgresQuotaStore builds a Postgres-backed quota store.
func NewPostgresQuotaStore(db *database.DB) *PostgresQuotaStore {
	return &PostgresQuotaStore{db: db}
}

const quotaSelect = `SELECT role, max_labs, max_cpu, max_memory_mb, max_disk_gb, max_lab_hours, updated_at FROM quotas`

func (p *PostgresQuotaStore) GetQuotas(ctx context.Context) ([]*QuotaSet, error) {
	rows, err := p.db.Pool.Query(ctx, quotaSelect+` ORDER BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*QuotaSet
	for rows.Next() {
		var q QuotaSet
		if err := rows.Scan(&q.Role, &q.MaxLabs, &q.MaxCPU, &q.MaxMemoryMB, &q.MaxDiskGB, &q.MaxLabHours, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

func (p *PostgresQuotaStore) GetQuota(ctx context.Context, role string) (*QuotaSet, error) {
	var q QuotaSet
	err := p.db.Pool.QueryRow(ctx, quotaSelect+` WHERE role=$1`, role).
		Scan(&q.Role, &q.MaxLabs, &q.MaxCPU, &q.MaxMemoryMB, &q.MaxDiskGB, &q.MaxLabHours, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &q, nil
}

func (p *PostgresQuotaStore) UpsertQuota(ctx context.Context, q *QuotaSet) error {
	if q == nil || q.Role == "" {
		return ErrInvalidInput
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO quotas (role, max_labs, max_cpu, max_memory_mb, max_disk_gb, max_lab_hours, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (role) DO UPDATE SET max_labs=EXCLUDED.max_labs, max_cpu=EXCLUDED.max_cpu,
			max_memory_mb=EXCLUDED.max_memory_mb, max_disk_gb=EXCLUDED.max_disk_gb,
			max_lab_hours=EXCLUDED.max_lab_hours, updated_at=now()`,
		q.Role, q.MaxLabs, q.MaxCPU, q.MaxMemoryMB, q.MaxDiskGB, q.MaxLabHours)
	return err
}

var _ QuotaStore = (*PostgresQuotaStore)(nil)
