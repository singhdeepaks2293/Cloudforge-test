// Package ratelimit is a Redis-backed fixed-window rate limiter
// (Milestone 9).
//
// A window is one Redis key holding a counter: the first increment
// in a window sets the key's TTL (atomically, via a Lua script, so a
// crash between INCR and EXPIRE cannot leak a key without a TTL).
// Keys are `ratelimit:<scope>:<identifier>`; the scope separates
// the auth limiter from the general API limiter, and the identifier
// is the client IP (see httpapi for why it is the IP, not the user).
//
// Failure behaviour is decided by the CALLER, not by this package:
// Allow returns the Redis error and the caller chooses fail-open or
// fail-closed. CloudForge policy (documented in docs/security/model.md):
//
//   - Auth endpoints (register/login) are FAIL-CLOSED: if Redis is
//     unreachable the request is rejected with 503. Brute-force
//     protection must not silently disappear; operators get a loud
//     error instead of a silent hole.
//   - The general API limiter is FAIL-OPEN: if Redis is unreachable
//     the request proceeds. The API is designed to serve without
//     Redis (fail-open cache), and rate limiting must not become a
//     new hard dependency for reads.
//
// A nil *Limiter means "not configured" (no Redis client): Allow is
// never called in that case and requests are unthrottled.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrUnavailable is returned by Allow when Redis cannot be reached.
var ErrUnavailable = errors.New("ratelimit: redis unavailable")

// fixedWindow increments the counter for key and sets the window
// TTL on the first increment, atomically.
const fixedWindow = `
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current
`

// Result is the outcome of one Allow call.
type Result struct {
	Allowed   bool
	Remaining int64 // requests left in the current window (0 when denied)
	Reset     time.Time
}

// Limiter is one fixed-window counter (one scope, one limit).
type Limiter struct {
	client redis.Cmdable
	limit  int64
	window time.Duration
	scope  string
}

// New builds a Limiter. client must be non-nil; limit must be >= 1
// and window > 0 (invalid values are clamped to 1 / 1s so a bad
// configuration throttles hard rather than disabling limiting).
func New(client redis.Cmdable, scope string, limit int, window time.Duration) *Limiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &Limiter{client: client, limit: int64(limit), window: window, scope: scope}
}

// NewClient dials Redis for rate limiting with tight timeouts: a
// limiter must fail fast (200ms) rather than stall requests behind
// a dead Redis. Creation performs no I/O; errors surface on Allow.
func NewClient(addr string) redis.Cmdable {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		MaxRetries:   0,
		PoolSize:     4,
	})
}

// Key returns the Redis key for identifier in this limiter's scope.
func (l *Limiter) Key(identifier string) string {
	return fmt.Sprintf("ratelimit:%s:%s", l.scope, identifier)
}

// Allow increments the counter for identifier and reports whether
// the request is within the limit. On a Redis error it returns
// (Result{}, ErrUnavailable) — the caller decides fail-open/closed.
func (l *Limiter) Allow(ctx context.Context, identifier string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	count, err := l.client.Eval(ctx, fixedWindow, []string{l.Key(identifier)}, l.window.Milliseconds()).Int64()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	reset := time.Now().Add(l.window)
	remaining := l.limit - count
	if remaining < 0 {
		remaining = 0
	}
	return Result{Allowed: count <= l.limit, Remaining: remaining, Reset: reset}, nil
}

// Limit returns the configured per-window request limit.
func (l *Limiter) Limit() int64 { return l.limit }

// Window returns the configured window duration.
func (l *Limiter) Window() time.Duration { return l.window }
