package auth

import (
	"context"
	"testing"
	"time"
)

// TestRevocationLogout verifies the Phase 7 contract: a token is
// valid before logout and rejected (ErrTokenRevoked) after its jti
// is revoked, while a second token for the same user keeps working.
func TestRevocationLogout(t *testing.T) {
	s := NewStore("test-secret-for-revocation")
	ctx := context.Background()
	_ = ctx

	tok1, _ := s.IssueToken("user-1")
	tok2, _ := s.IssueToken("user-1")

	if _, err := s.ValidateToken(tok1); err != nil {
		t.Fatalf("token valid before revoke: %v", err)
	}
	jti, err := s.TokenJTI(tok1)
	if err != nil || jti == "" {
		t.Fatalf("issued token must carry a jti: %q %v", jti, err)
	}
	if !s.RevokeToken(tok1) {
		t.Fatal("RevokeToken should succeed for a valid token")
	}
	if _, err := s.ValidateToken(tok1); err != ErrTokenRevoked {
		t.Fatalf("revoked token: got %v, want ErrTokenRevoked", err)
	}
	if _, err := s.ValidateToken(tok2); err != nil {
		t.Fatalf("other token must stay valid: %v", err)
	}
	// Revoking an invalid token is a no-op, not a panic.
	if s.RevokeToken("not-a-token") {
		t.Fatal("RevokeToken of an invalid token must return false")
	}
}

// TestRevocationRevokeUser verifies admin session revocation: all
// of a user's issued tokens stop working, another user's do not.
func TestRevocationRevokeUser(t *testing.T) {
	s := NewStore("test-secret-for-revocation")
	a1, _ := s.IssueToken("user-a")
	a2, _ := s.IssueToken("user-a")
	b1, _ := s.IssueToken("user-b")

	if n := s.RevokeUser("user-a"); n != 2 {
		t.Fatalf("RevokeUser(user-a) revoked %d, want 2", n)
	}
	for _, tok := range []string{a1, a2} {
		if _, err := s.ValidateToken(tok); err != ErrTokenRevoked {
			t.Fatalf("revoked user token: got %v, want ErrTokenRevoked", err)
		}
	}
	if _, err := s.ValidateToken(b1); err != nil {
		t.Fatalf("other user's token must stay valid: %v", err)
	}
	if n := s.RevokeUser("nobody"); n != 0 {
		t.Fatalf("RevokeUser(unknown) = %d, want 0", n)
	}
}

// TestMemoryRevokerExpiry verifies a revoked jti stops being denied
// after its expiry (the denylist cannot outlive the token).
func TestMemoryRevokerExpiry(t *testing.T) {
	m := NewMemoryRevoker()
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	if err := m.Revoke(ctx, "jti-old", "u", past); err != nil {
		t.Fatalf("revoke expired: %v", err)
	}
	revoked, err := m.IsRevoked(ctx, "jti-old")
	if err != nil || revoked {
		t.Fatalf("expired revocation must not deny: revoked=%v err=%v", revoked, err)
	}
	if err := m.Revoke(ctx, "jti-new", "u", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	revoked, err = m.IsRevoked(ctx, "jti-new")
	if err != nil || !revoked {
		t.Fatalf("live revocation must deny: revoked=%v err=%v", revoked, err)
	}
}
