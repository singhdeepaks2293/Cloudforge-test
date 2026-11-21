// Postgres implementations for the Milestone 6/7 stores: lab
// resources, terraform runs and terminal sessions (migration 004
// columns). All queries use only columns 004 guarantees; the
// configuration/plan_json JSONB columns carry kind-specific detail.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

// --- Lab resources -------------------------------------------------------

type PostgresResourceStore struct{ db *database.DB }

func NewPostgresResourceStore(db *database.DB) *PostgresResourceStore {
	return &PostgresResourceStore{db: db}
}

const resourceSelect = `SELECT id, lab_id, owner_id, resource_type, name, state, provider,
	provider_resource_id, parent_id, private_ip, public_ip, cidr, configuration, created_at
	FROM lab_resources`

func scanResource(row pgx.Row) (*LabResource, error) {
	var r LabResource
	var raw []byte
	if err := row.Scan(&r.ID, &r.LabID, &r.OwnerID, &r.Kind, &r.Name, &r.State, &r.Provider,
		&r.ProviderResourceID, &r.ParentID, &r.PrivateIP, &r.PublicIP, &r.CIDR, &raw, &r.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &r.Config)
	if r.Config == nil {
		r.Config = map[string]any{}
	}
	if labels, ok := r.Config["labels"].(map[string]any); ok {
		r.Labels = map[string]string{}
		for k, v := range labels {
			if s, ok := v.(string); ok {
				r.Labels[k] = s
			}
		}
	}
	return &r, nil
}

func (p *PostgresResourceStore) ListResources(ctx context.Context, labID string) ([]*LabResource, error) {
	rows, err := p.db.Pool.Query(ctx, resourceSelect+" WHERE lab_id=$1 ORDER BY created_at", labID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*LabResource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresResourceStore) GetResource(ctx context.Context, labID, id string) (*LabResource, error) {
	r, err := scanResource(p.db.Pool.QueryRow(ctx, resourceSelect+" WHERE lab_id=$1 AND id=$2", labID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (p *PostgresResourceStore) CreateResource(ctx context.Context, r *LabResource) (*LabResource, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	cfg := r.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	if len(r.Labels) > 0 {
		cfg["labels"] = r.Labels
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	_, err = p.db.Pool.Exec(ctx, `INSERT INTO lab_resources(id, lab_id, owner_id, provider, resource_type,
		name, state, provider_resource_id, parent_id, private_ip, public_ip, cidr, configuration)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		r.ID, r.LabID, r.OwnerID, r.Provider, r.Kind, r.Name, r.State,
		r.ProviderResourceID, r.ParentID, r.PrivateIP, r.PublicIP, r.CIDR, raw)
	if err != nil {
		return nil, err
	}
	return p.GetResource(ctx, r.LabID, r.ID)
}

func (p *PostgresResourceStore) DeleteResource(ctx context.Context, labID, id string) (bool, error) {
	tag, err := p.db.Pool.Exec(ctx, "DELETE FROM lab_resources WHERE lab_id=$1 AND id=$2", labID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- Terraform runs ------------------------------------------------------

type PostgresTerraformRunStore struct{ db *database.DB }

func NewPostgresTerraformRunStore(db *database.DB) *PostgresTerraformRunStore {
	return &PostgresTerraformRunStore{db: db}
}

const tfRunSelect = `SELECT id, user_id, lab_id, action, state, plan_id, plan_json,
	plan_output, apply_output, approved_at, approved_by, plan_expires_at,
	job_id, error, configuration, created_at, updated_at FROM terraform_runs`

func scanTerraformRun(row pgx.Row) (*TerraformRun, error) {
	var r TerraformRun
	var planRaw []byte
	var approvedAt, planExpiresAt *time.Time
	var approvedBy *string
	if err := row.Scan(&r.ID, &r.UserID, &r.LabID, &r.Action, &r.State, &r.PlanID, &planRaw,
		&r.PlanOutput, &r.ApplyOutput, &approvedAt, &approvedBy, &planExpiresAt,
		&r.JobID, &r.Error, &r.Configuration, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(planRaw, &r.PlanJSON)
	if r.PlanJSON == nil {
		r.PlanJSON = map[string]any{}
	}
	r.ApprovedAt = approvedAt
	r.PlanExpiresAt = planExpiresAt
	if approvedBy != nil {
		r.ApprovedBy = *approvedBy
	}
	return &r, nil
}

func (p *PostgresTerraformRunStore) CreateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	planRaw, _ := json.Marshal(r.PlanJSON)
	var approvedBy any
	if r.ApprovedBy != "" {
		approvedBy = r.ApprovedBy
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO terraform_runs(id, user_id, lab_id, action, state, plan_id,
		plan_json, plan_output, apply_output, approved_at, approved_by, plan_expires_at,
		job_id, error, configuration) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		r.ID, r.UserID, r.LabID, r.Action, r.State, r.PlanID, planRaw,
		r.PlanOutput, r.ApplyOutput, r.ApprovedAt, approvedBy, r.PlanExpiresAt,
		r.JobID, r.Error, r.Configuration)
	if err != nil {
		return nil, err
	}
	return p.GetRun(ctx, r.ID)
}

func (p *PostgresTerraformRunStore) GetRun(ctx context.Context, id string) (*TerraformRun, error) {
	r, err := scanTerraformRun(p.db.Pool.QueryRow(ctx, tfRunSelect+" WHERE id=$1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (p *PostgresTerraformRunStore) GetRunByPlanID(ctx context.Context, planID string) (*TerraformRun, error) {
	r, err := scanTerraformRun(p.db.Pool.QueryRow(ctx, tfRunSelect+" WHERE plan_id=$1", planID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (p *PostgresTerraformRunStore) ListRunsByLab(ctx context.Context, labID string, limit int) ([]*TerraformRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := p.db.Pool.Query(ctx, tfRunSelect+" WHERE lab_id=$1 ORDER BY created_at DESC LIMIT $2", labID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TerraformRun
	for rows.Next() {
		r, err := scanTerraformRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresTerraformRunStore) UpdateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error) {
	planRaw, _ := json.Marshal(r.PlanJSON)
	var approvedBy any
	if r.ApprovedBy != "" {
		approvedBy = r.ApprovedBy
	}
	tag, err := p.db.Pool.Exec(ctx, `UPDATE terraform_runs SET state=$1, plan_id=$2, plan_json=$3,
		plan_output=$4, apply_output=$5, approved_at=$6, approved_by=$7, plan_expires_at=$8,
		job_id=$9, error=$10, configuration=$11, updated_at=now() WHERE id=$12`,
		r.State, r.PlanID, planRaw, r.PlanOutput, r.ApplyOutput, r.ApprovedAt,
		approvedBy, r.PlanExpiresAt, r.JobID, r.Error, r.Configuration, r.ID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.GetRun(ctx, r.ID)
}

// --- Terminal sessions ---------------------------------------------------

type PostgresTerminalSessionStore struct{ db *database.DB }

func NewPostgresTerminalSessionStore(db *database.DB) *PostgresTerminalSessionStore {
	return &PostgresTerminalSessionStore{db: db}
}

const termSessionSelect = `SELECT id, user_id, lab_id, token_hash, expires_at, ended_at, used, mode, created_at
	FROM terminal_sessions`

func scanTerminalSession(row pgx.Row) (*TerminalSession, error) {
	var s TerminalSession
	if err := row.Scan(&s.ID, &s.UserID, &s.LabID, &s.TokenHash, &s.ExpiresAt, &s.EndedAt, &s.Used, &s.Mode, &s.CreatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (p *PostgresTerminalSessionStore) CreateSession(ctx context.Context, s *TerminalSession) (*TerminalSession, error) {
	if s.ID == "" {
		s.ID = ids.New()
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO terminal_sessions(id, user_id, lab_id, token_hash, expires_at, mode)
		VALUES($1,$2,$3,$4,$5,$6)`, s.ID, s.UserID, s.LabID, s.TokenHash, s.ExpiresAt, s.Mode)
	if err != nil {
		return nil, err
	}
	return p.GetSession(ctx, s.ID)
}

func (p *PostgresTerminalSessionStore) MarkSessionUsed(ctx context.Context, id string) error {
	_, err := p.db.Pool.Exec(ctx, "UPDATE terminal_sessions SET used=TRUE WHERE id=$1", id)
	return err
}

func (p *PostgresTerminalSessionStore) EndSession(ctx context.Context, id string, endedAt time.Time) error {
	_, err := p.db.Pool.Exec(ctx, "UPDATE terminal_sessions SET ended_at=$1 WHERE id=$2", endedAt, id)
	return err
}

func (p *PostgresTerminalSessionStore) GetSession(ctx context.Context, id string) (*TerminalSession, error) {
	s, err := scanTerminalSession(p.db.Pool.QueryRow(ctx, termSessionSelect+" WHERE id=$1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

var (
	_ LabResourceStore     = (*PostgresResourceStore)(nil)
	_ TerraformRunStore    = (*PostgresTerraformRunStore)(nil)
	_ TerminalSessionStore = (*PostgresTerminalSessionStore)(nil)
)
