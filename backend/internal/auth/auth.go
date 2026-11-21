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
}

// NewStore returns a Store using secret to sign bearer tokens.
func NewStore(secret string) *Store {
	return &Store{
		byID:     make(map[string]*User),
		byEmail:  make(map[string]*User),
		secret:   []byte(secret),
		now:      time.Now,
		TokenTTL: TokenTTL,
	}
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

// sessionClaims is the HS256 JWT payload. Only the user id is
// carried; no roles or PII are embedded in the token.
type sessionClaims struct {
	jwt.RegisteredClaims
}

// IssueToken returns a signed HS256 JWT for userID valid for the
// configured TTL, plus its expiry time.
func (s *Store) IssueToken(userID string) (string, time.Time) {
	ttl := s.TokenTTL
	if ttl <= 0 {
		ttl = TokenTTL
	}
	now := s.now()
	exp := now.Add(ttl)
	claims := sessionClaims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    "cloudforge",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		// Signing with an in-memory key cannot fail in practice;
		// return an empty token rather than panicking the handler.
		return "", exp
	}
	return token, exp
}

// ValidateToken verifies a JWT and returns the user ID it carries.
// An expired token yields ErrTokenExpired; any other failure yields
// ErrInvalidToken (including a token signed with another secret or
// with a non-HMAC algorithm).
func (s *Store) ValidateToken(token string) (string, error) {
	claims := &sessionClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	}, jwt.WithTimeFunc(s.now), jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("cloudforge"))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", ErrTokenExpired
		}
		return "", ErrInvalidToken
	}
	if claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}

// Count returns the number of registered users.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}
