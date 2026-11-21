// Password hashing for Milestone 9: Argon2id (PHC format) with
// dual-verify support for legacy bcrypt hashes.
//
// M9 replaced the M2 bcrypt hashes with Argon2id. Existing users are
// NOT locked out: VerifyPassword accepts a stored bcrypt hash
// ($2a$/$2b$/$2y$) as well as Argon2id, and NeedsRehash reports
// whether a stored hash should be upgraded to Argon2id on the next
// successful login (both auth.Store and PostgresUserStore do this).
//
// Argon2id parameters follow the GitHub MVP backport source and OWASP
// guidance for interactive logins: 64 MiB memory, 3 iterations,
// 2 lanes, 16-byte salt, 32-byte key.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters (OWASP interactive-login guidance).
const (
	argonMemory  uint32 = 64 * 1024 // KiB = 64 MiB
	argonTime    uint32 = 3
	argonThreads uint8  = 2
	argonSaltLen        = 16
	argonKeyLen  uint32 = 32
)

// ErrInvalidHash is returned when a stored hash cannot be parsed.
var ErrInvalidHash = errors.New("auth: malformed password hash")

// HashPassword returns a PHC-formatted Argon2id hash for password:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// IsArgon2idHash reports whether s looks like a PHC Argon2id hash.
func IsArgon2idHash(s string) bool { return strings.HasPrefix(s, "$argon2id$") }

// IsBcryptHash reports whether s looks like a bcrypt hash ($2a$/$2b$/$2y$).
// These are the M2-era hashes; they verify but should be re-hashed.
func IsBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// NeedsRehash reports whether a stored hash should be upgraded to
// Argon2id on the next successful login. True for legacy bcrypt
// hashes; false for current Argon2id hashes. Unknown formats are
// reported as needing a rehash too, but they never verify, so no
// rehash can happen for them.
func NeedsRehash(stored string) bool { return !IsArgon2idHash(stored) }

// VerifyPassword checks password against a stored hash. It accepts
// both current Argon2id hashes and legacy bcrypt hashes (M2).
// It returns (false, nil) for a wrong password and (false,
// ErrInvalidHash) for a stored hash in an unrecognised format.
func VerifyPassword(stored, password string) (bool, error) {
	switch {
	case IsArgon2idHash(stored):
		return verifyArgon2id(stored, password)
	case IsBcryptHash(stored):
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
		if err == nil {
			return true, nil
		}
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return false, err
	default:
		return false, ErrInvalidHash
	}
}

// CheckPassword verifies a stored hash (Argon2id or legacy bcrypt).
// It is the boolean form of VerifyPassword used by the stores.
func CheckPassword(stored, password string) bool {
	ok, _ := VerifyPassword(stored, password)
	return ok
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeArgon2id(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	var p argonParams
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return argonParams{}, nil, nil, ErrInvalidHash
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return argonParams{}, nil, nil, ErrInvalidHash
		}
		switch k {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.time = uint32(n)
		case "p":
			p.threads = uint8(n)
		default:
			return argonParams{}, nil, nil, ErrInvalidHash
		}
	}
	if p.memory == 0 || p.time == 0 || p.threads == 0 {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}

func verifyArgon2id(encoded, password string) (bool, error) {
	params, salt, expected, err := decodeArgon2id(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
