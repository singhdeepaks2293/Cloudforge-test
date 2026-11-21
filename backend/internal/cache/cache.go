// Package cache is a fail-open Redis cache (go-redis v9).
//
// If Redis is unreachable at startup, New returns a disabled cache and
// every method is a no-op / cache miss — the API must never fail because
// the cache is down. Callers report the mode via Available().
package cache

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// TTL for cached catalogue data.
const TTL = 60 * time.Second

// Cache wraps a Redis client; a nil client means "disabled".
type Cache struct {
	client *redis.Client
	avail  atomic.Bool
}

// New dials Redis and pings it with a 2s timeout. On failure it returns
// a disabled cache and the error, so the caller can log a warning and
// continue without cache.
func New(addr string) (*Cache, error) {
	c := &Cache{client: redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 2 * time.Second, MaxRetries: 1})}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.client.Ping(ctx).Err(); err != nil {
		_ = c.client.Close()
		return &Cache{}, err
	}
	c.avail.Store(true)
	return c, nil
}

// Available reports whether Redis is currently usable.
func (c *Cache) Available() bool { return c != nil && c.avail.Load() }

// Ping performs a live liveness check (bounded, 500ms). On failure the
// cache is marked unavailable so later calls fail open without retrying
// Redis on every request; a process restart re-enables it.
func (c *Cache) Ping(ctx context.Context) bool {
	if !c.Available() {
		return false
	}
	pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := c.client.Ping(pingCtx).Err(); err != nil {
		c.avail.Store(false)
		return false
	}
	return true
}

// Close releases the client (no-op when disabled).
func (c *Cache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *Cache) get(ctx context.Context, key string, dst any) bool {
	if !c.Available() {
		return false
	}
	raw, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

func (c *Cache) set(ctx context.Context, key string, v any) {
	if !c.Available() {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = c.client.Set(ctx, key, raw, TTL).Err()
}

func (c *Cache) del(ctx context.Context, keys ...string) {
	if !c.Available() {
		return
	}
	_ = c.client.Del(ctx, keys...).Err()
}

// GetCourses returns cached courses (JSON) and true on a hit.
func (c *Cache) GetCourses(ctx context.Context, dst any) bool {
	return c.get(ctx, "cloudforge:courses", dst)
}

// SetCourses caches the course list for TTL.
func (c *Cache) SetCourses(ctx context.Context, v any) { c.set(ctx, "cloudforge:courses", v) }

// InvalidateCourses drops the cached course list (call on mutation).
func (c *Cache) InvalidateCourses(ctx context.Context) { c.del(ctx, "cloudforge:courses") }

// GetCapabilities / SetCapabilities cache the capability registry.
func (c *Cache) GetCapabilities(ctx context.Context, dst any) bool {
	return c.get(ctx, "cloudforge:capabilities", dst)
}

// SetWithTTL stores one string value with an explicit TTL (used for
// short-lived terminal tokens). It is a no-op when Redis is down.
func (c *Cache) SetWithTTL(ctx context.Context, key, value string, ttl time.Duration) bool {
	if !c.Available() {
		return false
	}
	return c.client.Set(ctx, key, value, ttl).Err() == nil
}

// GetDel reads a key and deletes it atomically (single-use token
// consumption). The second return is false on a miss or when Redis
// is down.
func (c *Cache) GetDel(ctx context.Context, key string) (string, bool) {
	if !c.Available() {
		return "", false
	}
	v, err := c.client.GetDel(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return v, true
}

// Get reads a key without deleting it.
func (c *Cache) Get(ctx context.Context, key string) (string, bool) {
	if !c.Available() {
		return "", false
	}
	v, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return v, true
}

// SetCapabilities caches capabilities for TTL.
func (c *Cache) SetCapabilities(ctx context.Context, v any) {
	c.set(ctx, "cloudforge:capabilities", v)
}

// InvalidateCapabilities drops the cached capability registry.
func (c *Cache) InvalidateCapabilities(ctx context.Context) {
	c.del(ctx, "cloudforge:capabilities")
}
