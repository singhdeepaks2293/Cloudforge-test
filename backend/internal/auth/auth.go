// Package auth provides Milestone 9 authentication.
//
// M9: Argon2id password hashing (see password.go) with dual-verify
// for legacy M2 bcrypt hashes (re-hashed to Argon2id on successful
// login), roles (learner / instructor / admin), and HS256 JWT bearer
// tokens (golang-jwt/jwt v5, 12h TTL, CLOUDFORGE_JWT_SECRET). The
// PostgreSQL-backed implementation lives in internal/store. The
// in-memory Store below remains as the test/fallback implementation.
//
// BREAKING CHANGE (M9, tokens only): M2 hand-rolled HMAC tokens are no
// longer accepted; any token issued before the M9 upgrade must be
// re-issued by logging in again. Passwords are NOT affected —
// existing bcrypt hashes keep working and are upgraded silently.
package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

var (
	ErrUserExists       = errors.New("user already exists")
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenRevoked     = errors.New("token revoked")
	ErrInvalidEmail     = errors.New("invalid email")
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrInvalidRole      = errors.New("invalid role")
)

// Roles.
const (
	RoleLearner    = "learner"
	RoleInstructor = "instructor"
	RoleAdmin      = "admin"
)

// ValidRole reports whether role is a known role.
func ValidRole(role string) bool {
	switch role {
	case RoleLearner, RoleInstructor, RoleAdmin:
		return true
	default:
		return false
	}
}

// TokenTTL is the default bearer-token lifetime (overridden by config).
const TokenTTL = 12 * time.Hour

// User is a registered CloudForge user. PasswordHash is never serialised.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Public returns the user without any credential material.
func (u *User) Public() *User {
	if u == nil {
		return nil
	}
	return &User{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt}
}

// Store is a mutex-guarded in-memory user store (test/fallback path;
// production M2 uses the PostgreSQL store in internal/store).
type Store struct {
	mu      sync.RWMutex
	byID    map[string]*User
	byEmail map[string]*User
	secret  []byte
	now     func() time.Time // overridable in tests
	// TokenTTL overrides the package default when set.
	TokenTTL time.Duration
	// Revoker is the token denylist (Phase 7). Nil means no
	// revocation: ValidateToken accepts any well-formed token.
	// SetRevoker installs it (memory by default in tests; Redis or
	// Postgres in production via cmd/api).
	Revoker Revoker
	// fallback is the in-process denylist used when Revoker is set
	// but its backend is unreachable (e.g. Redis down): revocation
	// degrades to single-process scope rather than disappearing.
	fallback *MemoryRevoker
}

// SetRevoker installs revoker as the token denylist and initialises
// the in-memory fallback used when the revoker errors.
func (s *Store) SetRevoker(r Revoker) {
	s.Revoker = r
	s.fallback = NewMemoryRevoker()
}

// NewStore returns a Store using secret to sign bearer tokens.
func NewStore(secret string) *Store {
	s := &Store{
		byID:     make(map[string]*User),
		byEmail:  make(map[string]*User),
		secret:   []byte(secret),
		now:      time.Now,
		TokenTTL: TokenTTL,
	}
	// A memory revoker is installed by default, so logout works
	// even in tests and in the in-memory fallback; cmd/api replaces
	// it with the Postgres (or Redis) revoker when those backends
	// are up.
	s.SetRevoker(NewMemoryRevoker())
	return s
}

func newID() string { return ids.New() }

// ValidateEmail reports whether email looks like an email address.
func ValidateEmail(email string) bool { return validEmail(email) }

func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1 && strings.Contains(email[at+1:], ".")
}

// Register creates a new user with the learner role. Email comparison is
// case-insensitive.
func (s *Store) Register(email, name, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if !validEmail(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[email]; exists {
		return nil, ErrUserExists
	}
	u := &User{ID: newID(), Email: email, Name: name, Role: RoleLearner, PasswordHash: hash, CreatedAt: s.now().UTC()}
	s.byID[u.ID] = u
	s.byEmail[email] = u
	return u.Public(), nil
}

// Authenticate verifies credentials and returns the user. When the
// stored hash is a legacy bcrypt hash, a successful login upgrades
// it to Argon2id in place (dual-verify migration, M9).
func (s *Store) Authenticate(email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	u, ok := s.byEmail[email]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrUserNotFound
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidPassword
	}
	if NeedsRehash(u.PasswordHash) {
		if hash, err := HashPassword(password); err == nil {
			s.mu.Lock()
			u.PasswordHash = hash
			s.mu.Unlock()
		}
	}
	return u.Public(), nil
}

// GetByID returns the public view of a user by ID.
func (s *Store) GetByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u.Public(), nil
}

// List returns public views of all users, sorted by email.
func (s *Store) List() ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.byID))
	for _, u := range s.byID {
		out = append(out, u.Public())
	}
	// Deterministic order for tests and admin listings.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Email < out[i].Email {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// SetRole changes a user's role.
func (s *Store) SetRole(id, role string) (*User, error) {
	if !ValidRole(role) {
		return nil, ErrInvalidRole
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	u.Role = role
	return u.Public(), nil
}

// sessionClaims is the HS256 JWT payload. Only the user id and the
// session-scoped effective role are carried; the account's DB role
// and any other PII are never embedded in the token. The effective
// role is chosen at sign-in ("Sign in as root" picks admin for an
// admin account, otherwise the session is learner-scoped) and is
// HS256-signed, so a client cannot forge a wider session than the
// one the server issued. ID is the token id (jti, Phase 7): logout
// and admin session-revocation deny-list this value, so a stolen
// token stops working before its 12h expiry.
type sessionClaims struct {
	jwt.RegisteredClaims
	// EffectiveRole is the role this session acts with ("admin" or
	// "learner"). Empty on tokens issued before v4.1: those fall back
	// to the account's DB role, so existing sessions keep working.
	EffectiveRole string `json:"role,omitempty"`
}

// IssueToken returns a signed HS256 JWT for userID valid for the
// configured TTL, plus its expiry time. The token carries a fresh
// random jti, recorded with the revoker (best-effort) so an admin
// can later revoke all of the user's sessions. No effective-role
// claim is set: sessions from this token fall back to the account's
// DB role (pre-v4.1 behaviour, kept for registration and callers
// that do not scope the session).
func (s *Store) IssueToken(userID string) (string, time.Time) {
	return s.IssueTokenWithRole(userID, "")
}

// IssueTokenWithRole is IssueToken with the session's effective role
// baked into the token ("admin" or "learner"). The caller must have
// already checked the account is entitled to that role — the claim
// is trusted on later requests, so it must never be set from
// client input. An empty role leaves the claim unset (DB-role
// fallback); any other value must be a known role.
func (s *Store) IssueTokenWithRole(userID, effectiveRole string) (string, time.Time) {
	ttl := s.TokenTTL
	if ttl <= 0 {
		ttl = TokenTTL
	}
	now := s.now()
	exp := now.Add(ttl)
	role := effectiveRole
	if role != "" && !ValidRole(role) {
		role = ""
	}
	claims := sessionClaims{
		EffectiveRole: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ID:        NewJTI(),
			Issuer:    "cloudforge",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		// Signing with an in-memory key cannot fail in practice;
		// return an empty token rather than panicking the handler.
		return "", exp
	}
	if s.Revoker != nil {
		_ = s.Revoker.RecordIssued(context.Background(), claims.ID, userID, exp)
	}
	return token, exp
}

// TokenEffectiveRole returns the session-scoped effective role
// carried by a valid token ("" when the token has no role claim, so
// the caller falls back to the account's DB role). The token's
// signature is verified; an invalid or expired token is an error.
func (s *Store) TokenEffectiveRole(token string) (string, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return "", err
	}
	if claims.EffectiveRole == "" {
		return "", nil
	}
	if !ValidRole(claims.EffectiveRole) {
		return "", ErrInvalidToken
	}
	return claims.EffectiveRole, nil
}

// parseToken parses and verifies token, returning its claims.
// It does NOT check revocation; ValidateToken does that.
func (s *Store) parseToken(token string) (*sessionClaims, error) {
	claims := &sessionClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	}, jwt.WithTimeFunc(s.now), jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("cloudforge"))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}
	if claims.Subject == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// TokenJTI extracts the jti of a valid token ("", nil when the
// token has no jti — pre-Phase-7 tokens issued before the jti claim
// existed; those cannot be individually revoked, only by rotating
// the secret or revoking the user via a re-login).
func (s *Store) TokenJTI(token string) (string, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return "", err
	}
	return claims.ID, nil
}

// TokenExpiry extracts the expiry of a valid token.
func (s *Store) TokenExpiry(token string) (time.Time, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return time.Time{}, err
	}
	if claims.ExpiresAt == nil {
		return time.Time{}, ErrInvalidToken
	}
	return claims.ExpiresAt.Time, nil
}

// isRevoked consults the revoker, falling back to the in-memory
// denylist when the revoker errors (Redis/Postgres down). A
// revoker error never turns a valid token into an accepted one
// when the fallback knows it is revoked.
func (s *Store) isRevoked(jti string) bool {
	if jti == "" || s.Revoker == nil {
		return false
	}
	revoked, err := s.Revoker.IsRevoked(context.Background(), jti)
	if err == nil {
		return revoked
	}
	if s.fallback != nil {
		if fb, ferr := s.fallback.IsRevoked(context.Background(), jti); ferr == nil {
			return fb
		}
	}
	return false
}

// RevokeToken denies token until its expiry. It returns false when
// the token is invalid or has no jti (nothing to revoke).
func (s *Store) RevokeToken(token string) bool {
	claims, err := s.parseToken(token)
	if err != nil || claims.ID == "" || s.Revoker == nil {
		return false
	}
	exp := time.Now().Add(s.TokenTTL)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}
	_ = s.Revoker.Revoke(context.Background(), claims.ID, claims.Subject, exp)
	if s.fallback != nil {
		_ = s.fallback.Revoke(context.Background(), claims.ID, claims.Subject, exp)
	}
	return true
}

// RevokeUser denies every session issued to userID. It returns how
// many sessions were revoked (0 when none were recorded — e.g.
// tokens issued before revocation was enabled).
func (s *Store) RevokeUser(userID string) int {
	if s.Revoker == nil {
		return 0
	}
	n, err := s.Revoker.RevokeUser(context.Background(), userID)
	if err != nil && s.fallback != nil {
		if fb, ferr := s.fallback.RevokeUser(context.Background(), userID); ferr == nil {
			return fb
		}
	}
	if err != nil {
		return 0
	}
	return n
}

// ValidateToken verifies a JWT and returns the user ID it carries.
// An expired token yields ErrTokenExpired; any other failure yields
// ErrInvalidToken (including a token signed with another secret or
// with a non-HMAC algorithm). A revoked token (Phase 7) yields
// ErrTokenRevoked.
func (s *Store) ValidateToken(token string) (string, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return "", err
	}
	if s.isRevoked(claims.ID) {
		return "", ErrTokenRevoked
	}
	return claims.Subject, nil
}

// Count returns the number of registered users.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}
