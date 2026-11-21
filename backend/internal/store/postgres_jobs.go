package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/database"
)

// PostgresJobStore implements JobStore against PostgreSQL.
type PostgresJobStore struct {
	db *database.DB
}

// NewPostgresJobStore builds a Postgres-backed job store.
func NewPostgresJobStore(db *database.DB) *PostgresJobStore {
	return &PostgresJobStore{db: db}
}

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var raw []byte
	var key *string
	if err := row.Scan(&j.ID, &j.Type, &raw, &j.State, &j.Attempts, &j.MaxAttempts, &j.RunAt, &j.LastError, &key, &j.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &j.Payload)
	if j.Payload == nil {
		j.Payload = map[string]any{}
	}
	if key != nil {
		j.IdempotencyKey = *key
	}
	return &j, nil
}

const jobSelect = `SELECT id, type, payload, state, attempts, max_attempts, run_at, last_error, idempotency_key, created_at FROM jobs`

func (p *PostgresJobStore) Enqueue(ctx context.Context, typ string, payload map[string]any, key string) (*Job, bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	var keyArg any
	if key != "" {
		keyArg = key
	}
	var id string
	err = p.db.Pool.QueryRow(ctx, `INSERT INTO jobs(type, payload, idempotency_key) VALUES($1,$2,$3)
		RETURNING id`, typ, raw, keyArg).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) && key != "" {
			j, err := scanJob(p.db.Pool.QueryRow(ctx, jobSelect+" WHERE idempotency_key=$1", key))
			if err != nil {
				return nil, false, err
			}
			return j, true, nil
		}
		return nil, false, err
	}
	j, err := scanJob(p.db.Pool.QueryRow(ctx, jobSelect+" WHERE id=$1", id))
	if err != nil {
		return nil, false, err
	}
	return j, false, nil
}

func (p *PostgresJobStore) Claim(ctx context.Context, workerID string, limit int) ([]*Job, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id, type, payload, state, attempts, max_attempts, run_at, last_error, idempotency_key, created_at
		FROM jobs WHERE state IN ('pending','failed') AND run_at <= now()
		ORDER BY run_at LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, j := range out {
		if _, err := tx.Exec(ctx, `UPDATE jobs SET state='running', locked_by=$1, locked_at=now(), attempts=attempts+1, updated_at=now() WHERE id=$2`,
			workerID, j.ID); err != nil {
			return nil, err
		}
		j.State = "running"
		j.Attempts++
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *PostgresJobStore) Complete(ctx context.Context, id string) error {
	_, err := p.db.Pool.Exec(ctx, "UPDATE jobs SET state='done', locked_by='', updated_at=now() WHERE id=$1", id)
	return err
}

func (p *PostgresJobStore) Fail(ctx context.Context, id, errMsg string, retryAt time.Time, dead bool) error {
	state := "failed"
	if dead {
		state = "dead"
	}
	_, err := p.db.Pool.Exec(ctx, `UPDATE jobs SET state=$1, last_error=$2, run_at=$3, locked_by='', updated_at=now() WHERE id=$4`,
		state, errMsg, retryAt, id)
	return err
}

func (p *PostgresJobStore) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := p.db.Pool.Query(ctx, "SELECT state, COUNT(*) FROM jobs GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}

var _ JobStore = (*PostgresJobStore)(nil)
