// Postgres IAM store (Phase 2, migration 008).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

type PostgresIAMStore struct{ db *database.DB }

func NewPostgresIAMStore(db *database.DB) *PostgresIAMStore { return &PostgresIAMStore{db: db} }

var _ IAMStore = (*PostgresIAMStore)(nil)

func (p *PostgresIAMStore) attachedIDs(ctx context.Context, attachable, targetID string) ([]string, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT policy_id::text FROM iam_attachments WHERE attachable=$1 AND target_id=$2`, attachable, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) groupIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT group_id::text FROM iam_group_members WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func isIAMUnique(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func (p *PostgresIAMStore) CreateIAMUser(ctx context.Context, u *IAMUser) (*IAMUser, error) {
	if err := ValidateIAMName(u.Name); err != nil {
		return nil, err
	}
	if u.ID == "" {
		u.ID = ids.New()
	}
	if u.Path == "" {
		u.Path = "/"
	}
	arn := IAMARN("user", u.Name)
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_users(id, name, path, arn) VALUES($1,$2,$3,$4)`, u.ID, u.Name, u.Path, arn)
	if err != nil {
		if isIAMUnique(err) {
			return nil, ErrIAMConflict
		}
		return nil, err
	}
	return p.GetIAMUser(ctx, u.ID)
}

func (p *PostgresIAMStore) GetIAMUser(ctx context.Context, id string) (*IAMUser, error) {
	var u IAMUser
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text, name, path, arn, created_at FROM iam_users WHERE id=$1`, id).
		Scan(&u.ID, &u.Name, &u.Path, &u.ARN, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Groups, _ = p.groupIDs(ctx, u.ID)
	u.Policies, _ = p.attachedIDs(ctx, "user", u.ID)
	return &u, nil
}

func (p *PostgresIAMStore) GetIAMUserByName(ctx context.Context, name string) (*IAMUser, error) {
	var id string
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text FROM iam_users WHERE name=$1`, name).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p.GetIAMUser(ctx, id)
}

func (p *PostgresIAMStore) ListIAMUsers(ctx context.Context) ([]*IAMUser, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id::text FROM iam_users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IAMUser
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		u, err := p.GetIAMUser(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) DeleteIAMUser(ctx context.Context, id string) error {
	tag, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_users WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresIAMStore) CreateIAMGroup(ctx context.Context, g *IAMGroup) (*IAMGroup, error) {
	if err := ValidateIAMName(g.Name); err != nil {
		return nil, err
	}
	if g.ID == "" {
		g.ID = ids.New()
	}
	if g.Path == "" {
		g.Path = "/"
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_groups(id, name, path, arn) VALUES($1,$2,$3,$4)`,
		g.ID, g.Name, g.Path, IAMARN("group", g.Name))
	if err != nil {
		if isIAMUnique(err) {
			return nil, ErrIAMConflict
		}
		return nil, err
	}
	return p.GetIAMGroup(ctx, g.ID)
}

func (p *PostgresIAMStore) GetIAMGroup(ctx context.Context, id string) (*IAMGroup, error) {
	var g IAMGroup
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text, name, path, arn, created_at FROM iam_groups WHERE id=$1`, id).
		Scan(&g.ID, &g.Name, &g.Path, &g.ARN, &g.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := p.db.Pool.Query(ctx, `SELECT user_id::text FROM iam_group_members WHERE group_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		g.Members = append(g.Members, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	g.Policies, _ = p.attachedIDs(ctx, "group", g.ID)
	return &g, nil
}

func (p *PostgresIAMStore) ListIAMGroups(ctx context.Context) ([]*IAMGroup, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id::text FROM iam_groups ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IAMGroup
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		g, err := p.GetIAMGroup(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) DeleteIAMGroup(ctx context.Context, id string) error {
	tag, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_groups WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresIAMStore) AddGroupMember(ctx context.Context, groupID, userID string) error {
	if _, err := p.GetIAMGroup(ctx, groupID); err != nil {
		return err
	}
	if _, err := p.GetIAMUser(ctx, userID); err != nil {
		return err
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_group_members(group_id, user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, groupID, userID)
	return err
}

func (p *PostgresIAMStore) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	_, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_group_members WHERE group_id=$1 AND user_id=$2`, groupID, userID)
	return err
}

func (p *PostgresIAMStore) CreateIAMRole(ctx context.Context, r *IAMRole) (*IAMRole, error) {
	if err := ValidateIAMName(r.Name); err != nil {
		return nil, err
	}
	if r.ID == "" {
		r.ID = ids.New()
	}
	if r.Path == "" {
		r.Path = "/"
	}
	if r.MaxSessionSecs <= 0 {
		r.MaxSessionSecs = 3600
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_roles(id, name, path, arn, trust_policy, max_session_secs)
		VALUES($1,$2,$3,$4,$5,$6)`, r.ID, r.Name, r.Path, IAMARN("role", r.Name), []byte(r.TrustPolicy), r.MaxSessionSecs)
	if err != nil {
		if isIAMUnique(err) {
			return nil, ErrIAMConflict
		}
		return nil, err
	}
	return p.GetIAMRole(ctx, r.ID)
}

func (p *PostgresIAMStore) GetIAMRole(ctx context.Context, id string) (*IAMRole, error) {
	var r IAMRole
	var raw []byte
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text, name, path, arn, trust_policy, max_session_secs, created_at FROM iam_roles WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Path, &r.ARN, &raw, &r.MaxSessionSecs, &r.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.TrustPolicy = raw
	r.Policies, _ = p.attachedIDs(ctx, "role", r.ID)
	return &r, nil
}

func (p *PostgresIAMStore) ListIAMRoles(ctx context.Context) ([]*IAMRole, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id::text FROM iam_roles ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IAMRole
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		r, err := p.GetIAMRole(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) DeleteIAMRole(ctx context.Context, id string) error {
	tag, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_roles WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresIAMStore) CreateIAMPolicy(ctx context.Context, pol *IAMPolicy) (*IAMPolicy, error) {
	if err := ValidateIAMName(pol.Name); err != nil {
		return nil, err
	}
	if pol.ID == "" {
		pol.ID = ids.New()
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_policies(id, name, description, document) VALUES($1,$2,$3,$4)`,
		pol.ID, pol.Name, pol.Description, []byte(pol.Document))
	if err != nil {
		if isIAMUnique(err) {
			return nil, ErrIAMConflict
		}
		return nil, err
	}
	return p.GetIAMPolicy(ctx, pol.ID)
}

func (p *PostgresIAMStore) GetIAMPolicy(ctx context.Context, id string) (*IAMPolicy, error) {
	var pol IAMPolicy
	var raw []byte
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text, name, description, document, created_at FROM iam_policies WHERE id=$1`, id).
		Scan(&pol.ID, &pol.Name, &pol.Description, &raw, &pol.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	pol.Document = raw
	return &pol, nil
}

func (p *PostgresIAMStore) ListIAMPolicies(ctx context.Context) ([]*IAMPolicy, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id::text FROM iam_policies ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IAMPolicy
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		pol, err := p.GetIAMPolicy(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, pol)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) DeleteIAMPolicy(ctx context.Context, id string) error {
	tag, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_policies WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresIAMStore) AttachPolicy(ctx context.Context, attachable, targetID, policyID string) error {
	if attachable != "user" && attachable != "group" && attachable != "role" {
		return errors.New("attachable must be user, group or role")
	}
	if _, err := p.GetIAMPolicy(ctx, policyID); err != nil {
		return err
	}
	switch attachable {
	case "user":
		if _, err := p.GetIAMUser(ctx, targetID); err != nil {
			return err
		}
	case "group":
		if _, err := p.GetIAMGroup(ctx, targetID); err != nil {
			return err
		}
	case "role":
		if _, err := p.GetIAMRole(ctx, targetID); err != nil {
			return err
		}
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_attachments(attachable, target_id, policy_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		attachable, targetID, policyID)
	return err
}

func (p *PostgresIAMStore) DetachPolicy(ctx context.Context, attachable, targetID, policyID string) error {
	_, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_attachments WHERE attachable=$1 AND target_id=$2 AND policy_id=$3`,
		attachable, targetID, policyID)
	return err
}

func (p *PostgresIAMStore) AttachedPolicies(ctx context.Context, attachable, targetID string) ([]*IAMPolicy, error) {
	ids, err := p.attachedIDs(ctx, attachable, targetID)
	if err != nil {
		return nil, err
	}
	out := []*IAMPolicy{}
	for _, id := range ids {
		pol, err := p.GetIAMPolicy(ctx, id)
		if err != nil {
			continue // attachment to a deleted policy is skipped, not fatal
		}
		out = append(out, pol)
	}
	return out, nil
}

func (p *PostgresIAMStore) CreateIAMAccessKey(ctx context.Context, userID, keyID, secretHash string) (*IAMAccessKey, error) {
	if _, err := p.GetIAMUser(ctx, userID); err != nil {
		return nil, err
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_access_keys(id, user_id, status, secret_hash) VALUES($1,$2,'Active',$3)`,
		keyID, userID, secretHash)
	if err != nil {
		if isIAMUnique(err) {
			return nil, ErrIAMConflict
		}
		return nil, err
	}
	return &IAMAccessKey{ID: keyID, UserID: userID, Status: "Active", CreatedAt: time.Now().UTC()}, nil
}

func (p *PostgresIAMStore) ListIAMAccessKeys(ctx context.Context, userID string) ([]*IAMAccessKey, error) {
	if _, err := p.GetIAMUser(ctx, userID); err != nil {
		return nil, err
	}
	rows, err := p.db.Pool.Query(ctx, `SELECT id, user_id, status, created_at, last_used_at FROM iam_access_keys WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*IAMAccessKey{}
	for rows.Next() {
		var k IAMAccessKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Status, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (p *PostgresIAMStore) SetIAMAccessKeyStatus(ctx context.Context, userID, keyID, status string) (*IAMAccessKey, error) {
	if status != "Active" && status != "Inactive" {
		return nil, errors.New("status must be Active or Inactive")
	}
	tag, err := p.db.Pool.Exec(ctx, `UPDATE iam_access_keys SET status=$1 WHERE id=$2 AND user_id=$3`, status, keyID, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	keys, err := p.ListIAMAccessKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.ID == keyID {
			return k, nil
		}
	}
	return nil, ErrNotFound
}

func (p *PostgresIAMStore) DeleteIAMAccessKey(ctx context.Context, userID, keyID string) error {
	tag, err := p.db.Pool.Exec(ctx, `DELETE FROM iam_access_keys WHERE id=$1 AND user_id=$2`, keyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresIAMStore) CreateIAMSession(ctx context.Context, roleID, userID, tokenHash string, expiresAt time.Time) (*IAMSession, error) {
	if _, err := p.GetIAMRole(ctx, roleID); err != nil {
		return nil, err
	}
	id := ids.New()
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO iam_sessions(id, role_id, user_id, token_hash, expires_at) VALUES($1,$2,$3,$4,$5)`,
		id, roleID, userID, tokenHash, expiresAt)
	if err != nil {
		return nil, err
	}
	return &IAMSession{ID: id, RoleID: roleID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC()}, nil
}

func (p *PostgresIAMStore) GetIAMSession(ctx context.Context, id string) (*IAMSession, error) {
	var s IAMSession
	err := p.db.Pool.QueryRow(ctx, `SELECT id::text, role_id::text, user_id, expires_at, created_at FROM iam_sessions WHERE id=$1`, id).
		Scan(&s.ID, &s.RoleID, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}
