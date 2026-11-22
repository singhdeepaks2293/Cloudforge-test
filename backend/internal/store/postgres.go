// PostgreSQL store implementations (pgx). These are the production
// path when DATABASE_URL is reachable; see memory.go for the fallback.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

// Postgres is the shared handle for all Postgres stores.
type Postgres struct {
	DB *database.DB
}

// NewPostgres wraps a connected database.DB.
func NewPostgres(db *database.DB) *Postgres { return &Postgres{DB: db} }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// --- Postgres users -------------------------------------------------------

// PostgresUserStore implements UserStore against PostgreSQL. It embeds
// auth.Store solely for the HMAC token methods (IssueToken/ValidateToken),
// which need only the secret.
type PostgresUserStore struct {
	*auth.Store
	db *database.DB
}

// NewPostgresUserStore builds a Postgres-backed user store sharing the
// token signer of the given auth.Store.
func NewPostgresUserStore(db *database.DB, signer *auth.Store) *PostgresUserStore {
	return &PostgresUserStore{Store: signer, db: db}
}

func scanUser(row pgx.Row) (*auth.User, string, error) {
	var u auth.User
	var hash string
	var role string
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &hash, &u.CreatedAt, &role); err != nil {
		return nil, "", err
	}
	u.Role = role
	u.PasswordHash = hash
	return &u, hash, nil
}

const userSelect = `SELECT u.id, u.email, u.name, u.password_hash, u.created_at,
	COALESCE((SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id
	  WHERE ur.user_id = u.id ORDER BY CASE r.name WHEN 'admin' THEN 3 WHEN 'instructor' THEN 2 ELSE 1 END DESC LIMIT 1), 'learner')
	FROM users u`

func (p *PostgresUserStore) Register(email, name, password string) (*auth.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if !auth.ValidateEmail(email) {
		return nil, auth.ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, auth.ErrPasswordTooShort
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	id := ids.New()
	if _, err := tx.Exec(ctx, "INSERT INTO users(id, email, name, password_hash) VALUES($1,$2,$3,$4)", id, email, name, hash); err != nil {
		if isUniqueViolation(err) {
			return nil, auth.ErrUserExists
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id)
		SELECT $1, id FROM roles WHERE name='learner'`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return nil, auth.ErrUserExists
		}
		return nil, err
	}
	return p.GetByID(id)
}

func (p *PostgresUserStore) Authenticate(email, password string) (*auth.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, hash, err := scanUser(p.db.Pool.QueryRow(ctx, userSelect+" WHERE u.email=$1", email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrUserNotFound
		}
		return nil, err
	}
	if !auth.CheckPassword(hash, password) {
		return nil, auth.ErrInvalidPassword
	}
	// M9 dual-verify migration: a successful login with a legacy
	// bcrypt hash upgrades it to Argon2id. The upgrade is
	// best-effort — a failed UPDATE must not fail the login.
	if auth.NeedsRehash(hash) {
		if newHash, err := auth.HashPassword(password); err == nil {
			_, _ = p.db.Pool.Exec(ctx, "UPDATE users SET password_hash=$2 WHERE id=$1", u.ID, newHash)
		}
	}
	return u.Public(), nil
}

func (p *PostgresUserStore) GetByID(id string) (*auth.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, _, err := scanUser(p.db.Pool.QueryRow(ctx, userSelect+" WHERE u.id=$1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrUserNotFound
		}
		return nil, err
	}
	return u.Public(), nil
}

func (p *PostgresUserStore) List() ([]*auth.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := p.db.Pool.Query(ctx, userSelect+" ORDER BY u.email")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*auth.User
	for rows.Next() {
		u, _, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u.Public())
	}
	return out, rows.Err()
}

func (p *PostgresUserStore) SetRole(id, role string) (*auth.User, error) {
	if !auth.ValidRole(role) {
		return nil, auth.ErrInvalidRole
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)", id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, auth.ErrUserNotFound
	}
	if _, err := tx.Exec(ctx, "DELETE FROM user_roles WHERE user_id=$1", id); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id)
		SELECT $1, id FROM roles WHERE name=$2`, id, role)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, fmt.Errorf("role %q is not seeded in the roles table", role)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetByID(id)
}

// --- Postgres audit -------------------------------------------------------

// PostgresAuditStore implements AuditStore against PostgreSQL.
type PostgresAuditStore struct {
	db *database.DB
}

// NewPostgresAuditStore builds a Postgres-backed audit store.
func NewPostgresAuditStore(db *database.DB) *PostgresAuditStore {
	return &PostgresAuditStore{db: db}
}

func (p *PostgresAuditStore) Record(ctx context.Context, e AuditEntry) error {
	details, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = ids.New()
	}
	labID := ""
	if e.ResourceType == "lab" {
		labID = e.ResourceID
	}
	var labArg any
	if labID != "" {
		labArg = labID
	}
	_, err = p.db.Pool.Exec(ctx, `INSERT INTO audit_logs(id, user_id, lab_id, action, resource_type, resource_id, request_id, details)
		VALUES($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.ActorID, labArg, e.Action, e.ResourceType, e.ResourceID, e.RequestID, details)
	return err
}

// RecordTx inserts an audit row inside an existing transaction.
func RecordTx(ctx context.Context, tx pgx.Tx, e AuditEntry) error {
	details, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = ids.New()
	}
	var labArg any
	if e.ResourceType == "lab" && e.ResourceID != "" {
		labArg = e.ResourceID
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(id, user_id, lab_id, action, resource_type, resource_id, request_id, details)
		VALUES($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.ActorID, labArg, e.Action, e.ResourceType, e.ResourceID, e.RequestID, details)
	return err
}

func (p *PostgresAuditStore) List(ctx context.Context, limit, offset int) ([]*AuditEntry, int, error) {
	return p.ListFiltered(ctx, AuditFilter{Limit: limit, Offset: offset})
}

// ListFiltered translates AuditFilter.Matches into SQL. The Q filter
// is a case-insensitive substring over action / resource type /
// resource id; LabID matches the lab resource itself or entries whose
// JSON details name the lab.
func (p *PostgresAuditStore) ListFiltered(ctx context.Context, f AuditFilter) ([]*AuditEntry, int, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var conds []string
	var args []any
	ph := func() string {
		args = append(args, nil) // placeholder slot; value set below
		return "$" + strconv.Itoa(len(args))
	}
	if f.ActorID != "" {
		n := ph()
		conds = append(conds, "user_id::text="+n)
		args[len(args)-1] = f.ActorID
	}
	if f.Action != "" {
		n := ph()
		conds = append(conds, "action="+n)
		args[len(args)-1] = f.Action
	}
	if f.ResourceType != "" {
		n := ph()
		conds = append(conds, "resource_type="+n)
		args[len(args)-1] = f.ResourceType
	}
	if f.ResourceID != "" {
		n := ph()
		conds = append(conds, "resource_id="+n)
		args[len(args)-1] = f.ResourceID
	}
	if f.LabID != "" {
		n1, n2 := ph(), ph()
		conds = append(conds, "(resource_id="+n1+" OR details->>'lab_id'="+n2+")")
		args[len(args)-2], args[len(args)-1] = f.LabID, f.LabID
	}
	if f.Q != "" {
		n1, n2, n3 := ph(), ph(), ph()
		conds = append(conds, "(action ILIKE "+n1+" OR resource_type ILIKE "+n2+" OR resource_id ILIKE "+n3+")")
		pat := "%" + f.Q + "%"
		args[len(args)-3], args[len(args)-2], args[len(args)-1] = pat, pat, pat
	}
	if !f.From.IsZero() {
		n := ph()
		conds = append(conds, "created_at >= "+n)
		args[len(args)-1] = f.From
	}
	if !f.To.IsZero() {
		n := ph()
		conds = append(conds, "created_at <= "+n)
		args[len(args)-1] = f.To
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := p.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limitN := "$" + strconv.Itoa(len(args)+1)
	offsetN := "$" + strconv.Itoa(len(args)+2)
	args = append(args, f.Limit, f.Offset)
	rows, err := p.db.Pool.Query(ctx, `SELECT id, COALESCE(user_id::text,''), action, resource_type, resource_id,
		request_id, details, created_at FROM audit_logs `+where+` ORDER BY created_at DESC LIMIT `+limitN+` OFFSET `+offsetN, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID, &e.RequestID, &raw, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &e.Metadata)
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

func (p *PostgresAuditStore) CleanupOlderThan(ctx context.Context, days int) (int64, error) {
	tag, err := p.db.Pool.Exec(ctx, "DELETE FROM audit_logs WHERE created_at < now() - ($1 || ' days')::interval", days)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

var (
	_ UserStore  = (*PostgresUserStore)(nil)
	_ AuditStore = (*PostgresAuditStore)(nil)
)
