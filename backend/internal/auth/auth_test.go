package auth

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// bcryptGenerateForTest produces an M2-era bcrypt hash (cost 12) so
// tests can simulate a pre-M9 user without depending on old code.
func bcryptGenerateForTest(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func TestRegisterAndLogin(t *testing.T) {
	s := NewStore("test-secret")
	u, err := s.Register("Ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Email != "ada@example.com" {
		t.Fatalf("email should be lower-cased, got %q", u.Email)
	}
	if u.PasswordHash != "" {
		t.Fatalf("Public() must not expose the password hash")
	}
	got, err := s.Authenticate("ada@example.com", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("Authenticate returned wrong user")
	}
	if _, err := s.Authenticate("ada@example.com", "wrong-password"); err == nil {
		t.Fatalf("wrong password must fail")
	}
	if _, err := s.Authenticate("nobody@example.com", "correct-horse"); err == nil {
		t.Fatalf("unknown user must fail")
	}
}

func TestRegisterValidation(t *testing.T) {
	s := NewStore("test-secret")
	if _, err := s.Register("not-an-email", "Ada", "correct-horse"); err != ErrInvalidEmail {
		t.Fatalf("bad email: got %v, want ErrInvalidEmail", err)
	}
	if _, err := s.Register("ada@example.com", "Ada", "short"); err != ErrPasswordTooShort {
		t.Fatalf("short password: got %v, want ErrPasswordTooShort", err)
	}
	if _, err := s.Register("ada@example.com", "Ada", "correct-horse"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := s.Register("ADA@example.com", "Ada", "correct-horse"); err != ErrUserExists {
		t.Fatalf("duplicate email (case-insensitive): got %v, want ErrUserExists", err)
	}
}

func TestTokenRoundTripAndTamper(t *testing.T) {
	s := NewStore("test-secret")
	u, err := s.Register("ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	token, exp := s.IssueToken(u.ID)
	if time.Until(exp) < 11*time.Hour {
		t.Fatalf("token TTL should be ~12h, expiry %v", exp)
	}
	// A JWT has three dot-separated segments.
	if len(strings.Split(token, ".")) != 3 {
		t.Fatalf("JWT should have 3 segments, got %q", token)
	}
	id, err := s.ValidateToken(token)
	if err != nil || id != u.ID {
		t.Fatalf("ValidateToken: id=%q err=%v", id, err)
	}
	// Tamper with the payload segment: flip its first character.
	// The signature must then fail verification.
	parts := strings.Split(token, ".")
	repl := byte('A')
	if parts[1][0] == 'A' {
		repl = 'B'
	}
	tampered := parts[0] + "." + string(repl) + parts[1][1:] + "." + parts[2]
	if _, err := s.ValidateToken(tampered); err == nil {
		t.Fatalf("tampered token must fail validation")
	}
	// Tamper with the signature segment.
	tampered = parts[0] + "." + parts[1] + "." + string(repl) + parts[2][1:]
	if _, err := s.ValidateToken(tampered); err == nil {
		t.Fatalf("token with a tampered signature must fail validation")
	}
	if _, err := s.ValidateToken("not-a-token"); err == nil {
		t.Fatalf("malformed token must fail validation")
	}
}

func TestTokenWrongSecretAndAlgorithm(t *testing.T) {
	s := NewStore("test-secret")
	u, err := s.Register("ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	token, _ := s.IssueToken(u.ID)

	other := NewStore("different-secret")
	if _, err := other.ValidateToken(token); err == nil {
		t.Fatalf("token signed with another secret must fail validation")
	}

	// An unsigned ("none") token must be rejected outright.
	parts := strings.Split(token, ".")
	noneHeader := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0" // {"alg":"none","typ":"JWT"}
	if _, err := s.ValidateToken(noneHeader + "." + parts[1] + "."); err == nil {
		t.Fatalf("unsigned token must fail validation")
	}
}

func TestArgon2idHashFormatAndVerify(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !IsArgon2idHash(hash) {
		t.Fatalf("new hash should be Argon2id PHC format, got %q", hash)
	}
	if ok, err := VerifyPassword(hash, "correct-horse"); err != nil || !ok {
		t.Fatalf("VerifyPassword correct: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(hash, "wrong-password"); err != nil || ok {
		t.Fatalf("VerifyPassword wrong: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyPassword("not-a-hash", "correct-horse"); err != ErrInvalidHash {
		t.Fatalf("unrecognised hash format: got err=%v, want ErrInvalidHash", err)
	}
	// Two hashes of the same password must differ (random salt).
	hash2, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if hash == hash2 {
		t.Fatalf("hashes must be salted (identical output for the same password)")
	}
}

// TestLegacyBcryptLoginRehashes covers the M9 dual-verify migration:
// a user whose stored hash is still M2-era bcrypt can log in, and
// the successful login upgrades the stored hash to Argon2id.
func TestLegacyBcryptLoginRehashes(t *testing.T) {
	s := NewStore("test-secret")
	u, err := s.Register("ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-M9 user: replace the Argon2id hash with a bcrypt one.
	bcryptHash, err := bcryptGenerateForTest("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	stored := s.byEmail["ada@example.com"]
	stored.PasswordHash = bcryptHash
	s.mu.Unlock()

	if NeedsRehash(bcryptHash) != true {
		t.Fatalf("bcrypt hash must be reported as needing a rehash")
	}
	if NeedsRehash(stored.PasswordHash) != true {
		t.Fatalf("stored bcrypt hash must be reported as needing a rehash")
	}

	got, err := s.Authenticate("ada@example.com", "correct-horse")
	if err != nil {
		t.Fatalf("legacy bcrypt login must succeed: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("Authenticate returned wrong user")
	}
	// The stored hash must now be Argon2id.
	s.mu.RLock()
	upgraded := s.byEmail["ada@example.com"].PasswordHash
	s.mu.RUnlock()
	if !IsArgon2idHash(upgraded) {
		t.Fatalf("login should have re-hashed bcrypt to Argon2id, stored hash is %q", upgraded)
	}
	if NeedsRehash(upgraded) {
		t.Fatalf("upgraded hash must not need another rehash")
	}
	// And the upgraded hash still verifies the same password.
	if _, err := s.Authenticate("ada@example.com", "correct-horse"); err != nil {
		t.Fatalf("login with upgraded hash must succeed: %v", err)
	}
}

func TestLegacyBcryptWrongPassword(t *testing.T) {
	s := NewStore("test-secret")
	if _, err := s.Register("ada@example.com", "Ada", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	bcryptHash, err := bcryptGenerateForTest("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.byEmail["ada@example.com"].PasswordHash = bcryptHash
	s.mu.Unlock()

	if _, err := s.Authenticate("ada@example.com", "wrong-password"); err != ErrInvalidPassword {
		t.Fatalf("wrong password against a bcrypt hash: got %v, want ErrInvalidPassword", err)
	}
	// A failed login must NOT re-hash.
	s.mu.RLock()
	still := s.byEmail["ada@example.com"].PasswordHash
	s.mu.RUnlock()
	if still != bcryptHash {
		t.Fatalf("failed login must not re-hash the stored password")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := NewStore("test-secret")
	u, err := s.Register("ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	token, _ := s.IssueToken(u.ID)
	// Move the store's clock past expiry.
	s.now = func() time.Time { return time.Now().Add(13 * time.Hour) }
	if _, err := s.ValidateToken(token); err != ErrTokenExpired {
		t.Fatalf("expired token: got %v, want ErrTokenExpired", err)
	}
}
