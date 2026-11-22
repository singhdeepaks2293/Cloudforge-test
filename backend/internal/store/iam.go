// Package store — IAM identities (Phase 2).
//
// CloudForge platform users (auth.User, the users table) are NOT AWS
// IAM. The IAM store below holds the teaching IAM identities the IAM
// console manages: users, groups (+ memberships), roles, managed
// policies (+ attachments), access keys and assumed-role sessions.
//
// Secrets rule: an access-key secret and an assumed-role session token
// are shown to the caller exactly once, at creation. Only their
// SHA-256 hash is ever persisted (SecretHash / TokenHash). Policy
// documents are validated by providers.ValidatePolicyDocument before
// they reach this store; the store never validates policy semantics.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

// ErrIAMConflict is returned when an IAM name (or key id) already exists.
var ErrIAMConflict = errors.New("iam entity already exists")

// IAMNameRE is the AWS-style name rule enforced here: letters, digits
// and +=,.@_- (path is "/" by default and must start/end with "/").
var IAMNameRE = regexp.MustCompile(`^[\w+=,.@-]+$`)

// ValidateIAMName rejects empty names, names with characters AWS IAM
// would reject, and over-long names.
func ValidateIAMName(name string) error {
	if name == "" || len(name) > 64 || !IAMNameRE.MatchString(name) {
		return errors.New("name must be 1-64 characters of letters, digits and +=,.@_-")
	}
	return nil
}

// IAMARN builds the local teaching ARN for an IAM entity. It is a
// CloudForge-local identifier, never an AWS ARN.
func IAMARN(kind, name string) string {
	return "arn:cloudforge:iam::local:" + kind + "/" + name
}

// HashSecret returns the hex SHA-256 of a secret/token for storage.
// The raw value is never persisted.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// IAMUser is one AWS-style IAM user (not a CloudForge platform user).
type IAMUser struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	ARN       string    `json:"arn"`
	Groups    []string  `json:"groups,omitempty"`
	Policies  []string  `json:"policies,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// IAMGroup is one IAM group with its member user ids and attached policies.
type IAMGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	ARN       string    `json:"arn"`
	Members   []string  `json:"members,omitempty"`
	Policies  []string  `json:"policies,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// IAMRole is one IAM role: a trust policy document plus attached policies.
type IAMRole struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Path           string          `json:"path"`
	ARN            string          `json:"arn"`
	TrustPolicy    json.RawMessage `json:"trust_policy"`
	MaxSessionSecs int             `json:"max_session_duration_seconds"`
	Policies       []string        `json:"policies,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// IAMPolicy is one customer-managed policy: name + validated document.
type IAMPolicy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Document    json.RawMessage `json:"document"`
	CreatedAt   time.Time       `json:"created_at"`
}

// IAMAccessKey is one access key. The secret is NEVER stored and never
// returned after creation; only SecretHash is persisted.
type IAMAccessKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Status     string     `json:"status"` // Active | Inactive
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// IAMSession is one assumed-role session. Only TokenHash is persisted;
// the token itself is shown once at creation. Expired sessions are
// rejected by ValidateSession.
type IAMSession struct {
	ID        string    `json:"id"`
	RoleID    string    `json:"role_id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// IAMStore persists IAM identities. The memory implementation backs
// unit tests and the in-memory fallback; Postgres backs production
// (migration 008).
type IAMStore interface {
	// Users
	CreateIAMUser(ctx context.Context, u *IAMUser) (*IAMUser, error)
	GetIAMUser(ctx context.Context, id string) (*IAMUser, error)
	GetIAMUserByName(ctx context.Context, name string) (*IAMUser, error)
	ListIAMUsers(ctx context.Context) ([]*IAMUser, error)
	DeleteIAMUser(ctx context.Context, id string) error
	// Groups
	CreateIAMGroup(ctx context.Context, g *IAMGroup) (*IAMGroup, error)
	GetIAMGroup(ctx context.Context, id string) (*IAMGroup, error)
	ListIAMGroups(ctx context.Context) ([]*IAMGroup, error)
	DeleteIAMGroup(ctx context.Context, id string) error
	AddGroupMember(ctx context.Context, groupID, userID string) error
	RemoveGroupMember(ctx context.Context, groupID, userID string) error
	// Roles
	CreateIAMRole(ctx context.Context, r *IAMRole) (*IAMRole, error)
	GetIAMRole(ctx context.Context, id string) (*IAMRole, error)
	ListIAMRoles(ctx context.Context) ([]*IAMRole, error)
	DeleteIAMRole(ctx context.Context, id string) error
	// Policies
	CreateIAMPolicy(ctx context.Context, p *IAMPolicy) (*IAMPolicy, error)
	GetIAMPolicy(ctx context.Context, id string) (*IAMPolicy, error)
	ListIAMPolicies(ctx context.Context) ([]*IAMPolicy, error)
	DeleteIAMPolicy(ctx context.Context, id string) error
	// Attachments (attachable: "user" | "group" | "role")
	AttachPolicy(ctx context.Context, attachable, targetID, policyID string) error
	DetachPolicy(ctx context.Context, attachable, targetID, policyID string) error
	AttachedPolicies(ctx context.Context, attachable, targetID string) ([]*IAMPolicy, error)
	// Access keys. CreateIAMAccessKey stores only the hash and
	// returns the key WITHOUT the secret; the caller supplies the
	// one-time secret separately.
	CreateIAMAccessKey(ctx context.Context, userID, keyID, secretHash string) (*IAMAccessKey, error)
	ListIAMAccessKeys(ctx context.Context, userID string) ([]*IAMAccessKey, error)
	SetIAMAccessKeyStatus(ctx context.Context, userID, keyID, status string) (*IAMAccessKey, error)
	DeleteIAMAccessKey(ctx context.Context, userID, keyID string) error
	// Sessions. CreateIAMSession stores only the token hash.
	CreateIAMSession(ctx context.Context, roleID, userID, tokenHash string, expiresAt time.Time) (*IAMSession, error)
	GetIAMSession(ctx context.Context, id string) (*IAMSession, error)
}

// --- Memory implementation -------------------------------------------------

type MemoryIAMStore struct {
	mu       sync.Mutex
	users    map[string]*IAMUser
	groups   map[string]*IAMGroup
	roles    map[string]*IAMRole
	policies map[string]*IAMPolicy
	// attachable -> target -> set of policy ids
	attach map[string]map[string]map[string]bool
	keys   map[string]*IAMAccessKey // key id -> key
	// keyHashes / sessionHashes hold the stored hashes only; they
	// are never returned by any List/Get view.
	keyHashes map[string]string
	// token hash -> session (sessions are looked up by id; the hash
	// proves possession when the caller presents the token)
	sessions      map[string]*IAMSession
	sessionHashes map[string]string
}

func NewMemoryIAMStore() *MemoryIAMStore {
	return &MemoryIAMStore{
		users: map[string]*IAMUser{}, groups: map[string]*IAMGroup{},
		roles: map[string]*IAMRole{}, policies: map[string]*IAMPolicy{},
		attach: map[string]map[string]map[string]bool{},
		keys:   map[string]*IAMAccessKey{}, sessions: map[string]*IAMSession{},
		keyHashes: map[string]string{}, sessionHashes: map[string]string{},
	}
}

var _ IAMStore = (*MemoryIAMStore)(nil)

func (m *MemoryIAMStore) attached(attachable, targetID string) []string {
	var out []string
	for pid := range m.attach[attachable][targetID] {
		out = append(out, pid)
	}
	return out
}

func (m *MemoryIAMStore) CreateIAMUser(ctx context.Context, u *IAMUser) (*IAMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ValidateIAMName(u.Name); err != nil {
		return nil, err
	}
	for _, ex := range m.users {
		if ex.Name == u.Name {
			return nil, ErrIAMConflict
		}
	}
	cp := *u
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.Path == "" {
		cp.Path = "/"
	}
	cp.ARN = IAMARN("user", cp.Name)
	cp.CreatedAt = time.Now().UTC()
	m.users[cp.ID] = &cp
	return m.userCopy(&cp), nil
}

func (m *MemoryIAMStore) userCopy(u *IAMUser) *IAMUser {
	cp := *u
	cp.Groups = nil
	for _, g := range m.groups {
		for _, mid := range g.Members {
			if mid == u.ID {
				cp.Groups = append(cp.Groups, g.ID)
			}
		}
	}
	cp.Policies = m.attached("user", u.ID)
	return &cp
}

func (m *MemoryIAMStore) GetIAMUser(ctx context.Context, id string) (*IAMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return m.userCopy(u), nil
}

func (m *MemoryIAMStore) GetIAMUserByName(ctx context.Context, name string) (*IAMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Name == name {
			return m.userCopy(u), nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryIAMStore) ListIAMUsers(ctx context.Context) ([]*IAMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*IAMUser{}
	for _, u := range m.users {
		out = append(out, m.userCopy(u))
	}
	return out, nil
}

func (m *MemoryIAMStore) DeleteIAMUser(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return ErrNotFound
	}
	delete(m.users, id)
	delete(m.attach["user"], id)
	for _, g := range m.groups {
		for i, mid := range g.Members {
			if mid == id {
				g.Members = append(g.Members[:i], g.Members[i+1:]...)
				break
			}
		}
	}
	for kid, k := range m.keys {
		if k.UserID == id {
			delete(m.keys, kid)
		}
	}
	return nil
}

func (m *MemoryIAMStore) groupCopy(g *IAMGroup) *IAMGroup {
	cp := *g
	cp.Policies = m.attached("group", g.ID)
	return &cp
}

func (m *MemoryIAMStore) CreateIAMGroup(ctx context.Context, g *IAMGroup) (*IAMGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ValidateIAMName(g.Name); err != nil {
		return nil, err
	}
	for _, ex := range m.groups {
		if ex.Name == g.Name {
			return nil, ErrIAMConflict
		}
	}
	cp := *g
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.Path == "" {
		cp.Path = "/"
	}
	cp.ARN = IAMARN("group", cp.Name)
	cp.CreatedAt = time.Now().UTC()
	m.groups[cp.ID] = &cp
	return m.groupCopy(&cp), nil
}

func (m *MemoryIAMStore) GetIAMGroup(ctx context.Context, id string) (*IAMGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[id]
	if !ok {
		return nil, ErrNotFound
	}
	return m.groupCopy(g), nil
}

func (m *MemoryIAMStore) ListIAMGroups(ctx context.Context) ([]*IAMGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*IAMGroup{}
	for _, g := range m.groups {
		out = append(out, m.groupCopy(g))
	}
	return out, nil
}

func (m *MemoryIAMStore) DeleteIAMGroup(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[id]; !ok {
		return ErrNotFound
	}
	delete(m.groups, id)
	delete(m.attach["group"], id)
	return nil
}

func (m *MemoryIAMStore) AddGroupMember(ctx context.Context, groupID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[groupID]
	if !ok {
		return ErrNotFound
	}
	if _, ok := m.users[userID]; !ok {
		return ErrNotFound
	}
	for _, mid := range g.Members {
		if mid == userID {
			return nil // idempotent
		}
	}
	g.Members = append(g.Members, userID)
	return nil
}

func (m *MemoryIAMStore) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[groupID]
	if !ok {
		return ErrNotFound
	}
	for i, mid := range g.Members {
		if mid == userID {
			g.Members = append(g.Members[:i], g.Members[i+1:]...)
			return nil
		}
	}
	return nil // idempotent
}

func (m *MemoryIAMStore) roleCopy(r *IAMRole) *IAMRole {
	cp := *r
	cp.Policies = m.attached("role", r.ID)
	return &cp
}

func (m *MemoryIAMStore) CreateIAMRole(ctx context.Context, r *IAMRole) (*IAMRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ValidateIAMName(r.Name); err != nil {
		return nil, err
	}
	for _, ex := range m.roles {
		if ex.Name == r.Name {
			return nil, ErrIAMConflict
		}
	}
	cp := *r
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.Path == "" {
		cp.Path = "/"
	}
	if cp.MaxSessionSecs <= 0 {
		cp.MaxSessionSecs = 3600
	}
	cp.ARN = IAMARN("role", cp.Name)
	cp.CreatedAt = time.Now().UTC()
	m.roles[cp.ID] = &cp
	return m.roleCopy(&cp), nil
}

func (m *MemoryIAMStore) GetIAMRole(ctx context.Context, id string) (*IAMRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.roles[id]
	if !ok {
		return nil, ErrNotFound
	}
	return m.roleCopy(r), nil
}

func (m *MemoryIAMStore) ListIAMRoles(ctx context.Context) ([]*IAMRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*IAMRole{}
	for _, r := range m.roles {
		out = append(out, m.roleCopy(r))
	}
	return out, nil
}

func (m *MemoryIAMStore) DeleteIAMRole(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.roles[id]; !ok {
		return ErrNotFound
	}
	delete(m.roles, id)
	delete(m.attach["role"], id)
	return nil
}

func (m *MemoryIAMStore) CreateIAMPolicy(ctx context.Context, p *IAMPolicy) (*IAMPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ValidateIAMName(p.Name); err != nil {
		return nil, err
	}
	for _, ex := range m.policies {
		if ex.Name == p.Name {
			return nil, ErrIAMConflict
		}
	}
	cp := *p
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	cp.CreatedAt = time.Now().UTC()
	m.policies[cp.ID] = &cp
	return &cp, nil
}

func (m *MemoryIAMStore) GetIAMPolicy(ctx context.Context, id string) (*IAMPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemoryIAMStore) ListIAMPolicies(ctx context.Context) ([]*IAMPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*IAMPolicy{}
	for _, p := range m.policies {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryIAMStore) DeleteIAMPolicy(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[id]; !ok {
		return ErrNotFound
	}
	delete(m.policies, id)
	for _, targets := range m.attach {
		for _, set := range targets {
			delete(set, id)
		}
	}
	return nil
}

func (m *MemoryIAMStore) AttachPolicy(ctx context.Context, attachable, targetID, policyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if attachable != "user" && attachable != "group" && attachable != "role" {
		return errors.New("attachable must be user, group or role")
	}
	if _, ok := m.policies[policyID]; !ok {
		return ErrNotFound
	}
	switch attachable {
	case "user":
		if _, ok := m.users[targetID]; !ok {
			return ErrNotFound
		}
	case "group":
		if _, ok := m.groups[targetID]; !ok {
			return ErrNotFound
		}
	case "role":
		if _, ok := m.roles[targetID]; !ok {
			return ErrNotFound
		}
	}
	if m.attach[attachable] == nil {
		m.attach[attachable] = map[string]map[string]bool{}
	}
	if m.attach[attachable][targetID] == nil {
		m.attach[attachable][targetID] = map[string]bool{}
	}
	m.attach[attachable][targetID][policyID] = true
	return nil
}

func (m *MemoryIAMStore) DetachPolicy(ctx context.Context, attachable, targetID, policyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attach[attachable][targetID], policyID)
	return nil
}

func (m *MemoryIAMStore) AttachedPolicies(ctx context.Context, attachable, targetID string) ([]*IAMPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*IAMPolicy{}
	for pid := range m.attach[attachable][targetID] {
		if p, ok := m.policies[pid]; ok {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryIAMStore) CreateIAMAccessKey(ctx context.Context, userID, keyID, secretHash string) (*IAMAccessKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := m.keys[keyID]; ok {
		return nil, ErrIAMConflict
	}
	k := &IAMAccessKey{ID: keyID, UserID: userID, Status: "Active", CreatedAt: time.Now().UTC()}
	m.keys[keyID] = k
	// The hash is kept out of the returned view: the store keeps it
	// only in the Postgres implementation's column; the memory store
	// holds the hash in a side map so it is never exposed by List.
	m.keyHashes[keyID] = secretHash
	return &IAMAccessKey{ID: k.ID, UserID: k.UserID, Status: k.Status, CreatedAt: k.CreatedAt}, nil
}

func (m *MemoryIAMStore) ListIAMAccessKeys(ctx context.Context, userID string) ([]*IAMAccessKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return nil, ErrNotFound
	}
	out := []*IAMAccessKey{}
	for _, k := range m.keys {
		if k.UserID == userID {
			cp := *k
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryIAMStore) SetIAMAccessKeyStatus(ctx context.Context, userID, keyID, status string) (*IAMAccessKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status != "Active" && status != "Inactive" {
		return nil, errors.New("status must be Active or Inactive")
	}
	k, ok := m.keys[keyID]
	if !ok || k.UserID != userID {
		return nil, ErrNotFound
	}
	k.Status = status
	cp := *k
	return &cp, nil
}

func (m *MemoryIAMStore) DeleteIAMAccessKey(ctx context.Context, userID, keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.UserID != userID {
		return ErrNotFound
	}
	delete(m.keys, keyID)
	delete(m.keyHashes, keyID)
	return nil
}

func (m *MemoryIAMStore) CreateIAMSession(ctx context.Context, roleID, userID, tokenHash string, expiresAt time.Time) (*IAMSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.roles[roleID]; !ok {
		return nil, ErrNotFound
	}
	s := &IAMSession{ID: ids.New(), RoleID: roleID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC()}
	m.sessions[s.ID] = s
	m.sessionHashes[s.ID] = tokenHash
	return &IAMSession{ID: s.ID, RoleID: s.RoleID, UserID: s.UserID, ExpiresAt: s.ExpiresAt, CreatedAt: s.CreatedAt}, nil
}

func (m *MemoryIAMStore) GetIAMSession(ctx context.Context, id string) (*IAMSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}
