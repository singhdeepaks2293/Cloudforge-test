package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
)

// PostgresLabStore implements LabStore against PostgreSQL. Lab creation,
// resource rows, job enqueue and the audit row happen in ONE transaction;
// state transitions validate via labs.ValidateTransition and persist
// atomically (illegal transition = 409, no partial write).
type PostgresLabStore struct {
	db      *database.DB
	maxLabs int
}

// NewPostgresLabStore builds a Postgres-backed lab store.
func NewPostgresLabStore(db *database.DB) *PostgresLabStore {
	return &PostgresLabStore{db: db, maxLabs: quota.DefaultMaxActiveLabs}
}

// SetMaxLabs sets the store-level lab-count ceiling; see
// labs.Store.SetMaxLabs for why this is a backstop, not the
// per-role limit.
func (p *PostgresLabStore) SetMaxLabs(n int) {
	if n > 0 {
		p.maxLabs = n
	}
}

func (p *PostgresLabStore) ListAll(ctx context.Context) ([]*labs.Lab, error) {
	rows, err := p.db.Pool.Query(ctx, labSelect+` ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*labs.Lab
	for rows.Next() {
		l, err := scanLab(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func scanLab(row pgx.Row) (*labs.Lab, error) {
	var l labs.Lab
	var key *string
	if err := row.Scan(&l.ID, &l.OwnerID, &l.DefinitionID, &l.Name, &l.State, &l.Error, &key, &l.CreatedAt, &l.UpdatedAt, &l.ExpiresAt); err != nil {
		return nil, err
	}
	if key != nil {
		l.IdempotencyKey = *key
	}
	return &l, nil
}

const labSelect = `SELECT id, owner_id, definition_id, name, state, error, idempotency_key,
	created_at, updated_at, expires_at FROM lab_sessions`

func (p *PostgresLabStore) Create(ctx context.Context, ownerID, definitionID, name, idempotencyKey string) (*labs.Lab, bool, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	// Idempotency replay (unique per user, enforced by the DB constraint).
	if idempotencyKey != "" {
		var existing *labs.Lab
		row := tx.QueryRow(ctx, labSelect+" WHERE owner_id=$1 AND idempotency_key=$2", ownerID, idempotencyKey)
		existing, err = scanLab(row)
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return nil, false, err
			}
			return existing, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}
	}

	var duration int
	if err := tx.QueryRow(ctx, "SELECT duration_min FROM lab_definitions WHERE id=$1", definitionID).Scan(&duration); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, labs.ErrUnknownDefinition
		}
		return nil, false, err
	}
	if name == "" {
		var title string
		if err := tx.QueryRow(ctx, "SELECT title FROM lab_definitions WHERE id=$1", definitionID).Scan(&title); err == nil {
			name = title
		}
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM lab_sessions WHERE owner_id=$1 AND state IN
		('CREATED','PROVISIONING','READY','RUNNING','VALIDATING','STOPPING')`, ownerID).Scan(&active); err != nil {
		return nil, false, err
	}
	if err := quota.Check(active, p.maxLabs); err != nil {
		return nil, false, err
	}

	now := time.Now().UTC()
	id := ids.New()
	expires := now.Add(time.Duration(duration) * time.Minute)
	var keyArg any
	if idempotencyKey != "" {
		keyArg = idempotencyKey
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lab_sessions(id, owner_id, definition_id, name, state, expires_at, idempotency_key)
		VALUES($1,$2,$3,$4,'CREATED',$5,$6)`, id, ownerID, definitionID, name, expires, keyArg); err != nil {
		if isUniqueViolation(err) {
			// Lost a race with a concurrent identical create: replay it.
			_ = tx.Rollback(ctx)
			row := p.db.Pool.QueryRow(ctx, labSelect+" WHERE owner_id=$1 AND idempotency_key=$2", ownerID, idempotencyKey)
			existing, err := scanLab(row)
			if err != nil {
				return nil, false, fmt.Errorf("idempotency conflict but original lab not found: %w", err)
			}
			return existing, true, nil
		}
		return nil, false, err
	}
	cfg, _ := json.Marshal(map[string]string{"definition_id": definitionID})
	if _, err := tx.Exec(ctx, `INSERT INTO lab_resources(lab_id, owner_id, provider, resource_type, state, configuration, cleanup_status)
		VALUES($1,$2,'rust-network-engine','vpc','CREATED',$3,'PENDING')`, id, ownerID, cfg); err != nil {
		return nil, false, err
	}
	payload, _ := json.Marshal(map[string]string{"lab_id": id})
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(type, payload, idempotency_key) VALUES('lab_provision',$1,$2)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`, payload, "lab_provision:"+id); err != nil {
		return nil, false, err
	}
	if err := RecordTx(ctx, tx, AuditEntry{ActorID: ownerID, Action: "lab_created", ResourceType: "lab", ResourceID: id,
		Metadata: map[string]any{"definition_id": definitionID}}); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	lab, err := p.Get(ctx, id)
	return lab, false, err
}

func (p *PostgresLabStore) Get(ctx context.Context, id string) (*labs.Lab, error) {
	l, err := scanLab(p.db.Pool.QueryRow(ctx, labSelect+" WHERE id=$1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, labs.ErrLabNotFound
		}
		return nil, err
	}
	return l, nil
}

func (p *PostgresLabStore) ListByOwner(ctx context.Context, ownerID string) ([]*labs.Lab, error) {
	rows, err := p.db.Pool.Query(ctx, labSelect+" WHERE owner_id=$1 ORDER BY created_at DESC", ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*labs.Lab
	for rows.Next() {
		l, err := scanLab(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// transitionInTx validates and persists a state change inside tx.
func transitionInTx(ctx context.Context, tx pgx.Tx, id, to, errMsg string) (*labs.Lab, error) {
	var current, owner string
	if err := tx.QueryRow(ctx, "SELECT state, owner_id FROM lab_sessions WHERE id=$1 FOR UPDATE", id).Scan(&current, &owner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, labs.ErrLabNotFound
		}
		return nil, err
	}
	if err := labs.ValidateTransition(current, to); err != nil {
		return nil, err
	}
	var l *labs.Lab
	row := tx.QueryRow(ctx, `UPDATE lab_sessions SET state=$1, error=$2, updated_at=now()
		WHERE id=$3 RETURNING id, owner_id, definition_id, name, state, error, idempotency_key, created_at, updated_at, expires_at`,
		to, errMsg, id)
	l, err := scanLab(row)
	if err != nil {
		return nil, err
	}
	if err := RecordTx(ctx, tx, AuditEntry{ActorID: owner, Action: "lab_transition", ResourceType: "lab", ResourceID: id,
		Metadata: map[string]any{"from": current, "to": to, "error": errMsg}}); err != nil {
		return nil, err
	}
	return l, nil
}

func (p *PostgresLabStore) Transition(ctx context.Context, id, to, errMsg string) (*labs.Lab, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	l, err := transitionInTx(ctx, tx, id, to, errMsg)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

func (p *PostgresLabStore) Stop(ctx context.Context, id string) (*labs.Lab, error) {
	// No real resources to stop in M2, so STOPPING -> STOPPED completes
	// in one transaction (two validated transitions, atomically).
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := transitionInTx(ctx, tx, id, labs.StateStopping, ""); err != nil {
		return nil, err
	}
	l, err := transitionInTx(ctx, tx, id, labs.StateStopped, "")
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

// Start re-provisions a STOPPED lab: STOPPED -> PROVISIONING in one
// transaction plus a fresh lab_provision job for the worker. The key
// is unique per start (a replayed create key would return the old,
// already-done provision job and the lab would never re-provision).
// The worker's provision handler drives PROVISIONING -> READY/FAILED,
// so READY is only reported when provisioning actually succeeded.
func (p *PostgresLabStore) Start(ctx context.Context, id string) (*labs.Lab, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	l, err := transitionInTx(ctx, tx, id, labs.StateProvisioning, "")
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"lab_id": id})
	key := fmt.Sprintf("lab_start:%s:%d", id, time.Now().UnixNano())
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(type, payload, idempotency_key) VALUES('lab_provision',$1,$2)`, payload, key); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

func (p *PostgresLabStore) Destroy(ctx context.Context, id string) (*labs.Lab, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	l, err := transitionInTx(ctx, tx, id, labs.StateDestroying, "")
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"lab_id": id})
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(type, payload, idempotency_key) VALUES('lab_destroy',$1,$2)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`, payload, "lab_destroy:"+id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

func (p *PostgresLabStore) ExpireDue(ctx context.Context) ([]*labs.Lab, error) {
	rows, err := p.db.Pool.Query(ctx, labSelect+` WHERE expires_at < now() AND state IN
		('CREATED','PROVISIONING','READY','RUNNING','STOPPED') ORDER BY expires_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*labs.Lab
	for rows.Next() {
		l, err := scanLab(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (p *PostgresLabStore) CleanupResources(ctx context.Context, labID string) error {
	_, err := p.db.Pool.Exec(ctx, `UPDATE lab_resources SET cleanup_status='CLEANED', state='DESTROYED', updated_at=now()
		WHERE lab_id=$1 AND cleanup_status <> 'CLEANED'`, labID)
	return err
}

func (p *PostgresLabStore) ActiveCount(ctx context.Context, ownerID string) (int, error) {
	var n int
	err := p.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM lab_sessions WHERE owner_id=$1 AND state IN
		('CREATED','PROVISIONING','READY','RUNNING','VALIDATING','STOPPING')`, ownerID).Scan(&n)
	return n, err
}

var _ LabStore = (*PostgresLabStore)(nil)
