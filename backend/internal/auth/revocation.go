// Package auth — token revocation (Phase 7, risk R3).
//
// JWTs are stateless HS256 tokens, so a stolen token is valid until
// expiry (12h) unless something remembers it. Revocation is a
// denylist of token ids (jti):
//
//   - Every issued token carries a random jti claim.
//   - Logout revokes the caller's jti until its expiry.
//   - An admin can revoke all of a user's sessions: every jti the
//     user was issued (tracked at issue time) is revoked.
//   - ValidateToken consults the revoker on every request.
//
// Three Revoker backends exist, all implementing the same interface:
// MemoryRevoker (in-process, used by unit tests and the in-memory
// fallback), RedisRevoker (Redis SETEX + a per-user SET of issued
// jtis, used when Redis is up and Postgres is not), and the Postgres
// revoker in internal/store (revoked_tokens table, migration 013 —
// the durable backend used when Postgres is up). Entries expire
// with the token, so the denylist cannot grow without bound; the
// Postgres backend also deletes expired rows opportunistically.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Revoker is a token-denylist backend.
type Revoker interface {
	// RecordIssued remembers that jti was issued to userID and
	// expires at exp, so RevokeUser can find it later. Best-effort:
	// a failure must never fail a login.
	RecordIssued(ctx context.Context, jti, userID string, exp time.Time) error
	// Revoke denies jti until exp (no-op after exp).
	Revoke(ctx context.Context, jti, userID string, exp time.Time) error
	// IsRevoked reports whether jti is currently denied.
	IsRevoked(ctx context.Context, jti string) (bool, error)
	// RevokeUser denies every jti recorded for userID and returns
	// how many were revoked.
	RevokeUser(ctx context.Context, userID string) (int, error)
}

// NewJTI returns a random token id (128 bits, hex).
func NewJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "jti-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

// --- in-memory revoker --------------------------------------------------

// MemoryRevoker is the in-process Revoker: a map of revoked jtis to
// their expiry plus a per-user index of issued jtis. Expired entries
// are dropped lazily on access.
type MemoryRevoker struct {
	mu      sync.Mutex
	revoked map[string]time.Time            // jti -> expiry
	issued  map[string]map[string]time.Time // userID -> jti -> expiry
}

// NewMemoryRevoker returns an empty in-memory revoker.
func NewMemoryRevoker() *MemoryRevoker {
	return &MemoryRevoker{
		revoked: map[string]time.Time{},
		issued:  map[string]map[string]time.Time{},
	}
}

func (m *MemoryRevoker) RecordIssued(ctx context.Context, jti, userID string, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.issued[userID] == nil {
		m.issued[userID] = map[string]time.Time{}
	}
	m.issued[userID][jti] = exp
	return nil
}

func (m *MemoryRevoker) Revoke(ctx context.Context, jti, userID string, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Now().After(exp) {
		return nil
	}
	m.revoked[jti] = exp
	return nil
}

func (m *MemoryRevoker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.revoked[jti]
	if !ok {
		return false, nil
	}
	if time.Now().After(exp) {
		delete(m.revoked, jti)
		return false, nil
	}
	return true, nil
}

func (m *MemoryRevoker) RevokeUser(ctx context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	n := 0
	for jti, exp := range m.issued[userID] {
		if now.After(exp) {
			delete(m.issued[userID], jti)
			continue
		}
		m.revoked[jti] = exp
		n++
	}
	return n, nil
}
