// Phase 3 — small go-redis wrapper for the ElastiCache console.
//
// The platform Redis is shared (lite mode), so every operation here
// is scoped to one cluster's key prefix. ACL users are best-effort:
// a platform Redis that forbids ACL SETUSER (or the fake stub used
// in tests) reports the error and the caller falls back to
// prefix-only isolation, stated in the cluster record.
package httpapi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/redis/go-redis/v9"
)

type phase3Redis struct {
	client *redis.Client
	addr   string
}

func newPhase3Redis(addr string) *phase3Redis {
	return &phase3Redis{client: phase3.NewRedisClient(addr), addr: addr}
}

func (r *phase3Redis) Close() error { return r.client.Close() }

func (r *phase3Redis) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// CreateACLUser creates a namespaced user allowed only on the
// cluster prefix (read+write, no admin commands). Errors are
// returned so the caller can record the fallback honestly.
func (r *phase3Redis) CreateACLUser(ctx context.Context, username, prefix string) error {
	// ACL SETUSER <user> on >password resetkeys ~prefix* +get +set +del +ttl +type +scan +info +ping
	// The password is a random per-cluster secret the API does not
	// hand out (the console talks to Redis through the API only);
	// it exists so the user cannot be used without it.
	pw, err := randomToken(16)
	if err != nil {
		return err
	}
	return r.client.Do(ctx, "ACL", "SETUSER", username, "on", ">"+pw,
		"resetkeys", "~"+prefix+"*", "+get", "+set", "+del", "+ttl", "+type", "+scan", "+info", "+ping").Err()
}

func (r *phase3Redis) DeleteACLUser(ctx context.Context, username string) error {
	return r.client.Do(ctx, "ACL", "DELUSER", username).Err()
}

// ScanPrefix returns up to limit keys matching pattern (a glob;
// callers pass prefix + user pattern).
func (r *phase3Redis) ScanPrefix(ctx context.Context, pattern string, limit int) ([]string, error) {
	if pattern == "" {
		pattern = "*"
	}
	var cursor uint64
	keys := []string{}
	for {
		var batch []string
		var err error
		batch, cursor, err = r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if cursor == 0 || len(keys) >= limit {
			break
		}
	}
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}

func (r *phase3Redis) TTL(ctx context.Context, key string) (int64, error) {
	d, err := r.client.TTL(ctx, key).Result()
	if err != nil {
		return -1, err
	}
	if d < 0 {
		return -1, nil // no expiry
	}
	return int64(d / time.Second), nil
}

func (r *phase3Redis) Type(ctx context.Context, key string) (string, error) {
	return r.client.Type(ctx, key).Result()
}

func (r *phase3Redis) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *phase3Redis) Set(ctx context.Context, key, value string, ttlSeconds int) error {
	if ttlSeconds > 0 {
		return r.client.Set(ctx, key, value, time.Duration(ttlSeconds)*time.Second).Err()
	}
	return r.client.Set(ctx, key, value, 0).Err()
}

func (r *phase3Redis) Del(ctx context.Context, keys []string) (int, error) {
	n, err := r.client.Del(ctx, keys...).Result()
	return int(n), err
}

// Info returns the numeric/string fields the metrics view needs
// (used_memory, keyspace_hits/misses, connected_clients).
func (r *phase3Redis) Info(ctx context.Context) (map[string]int64, error) {
	raw, err := r.client.Info(ctx).Result()
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "used_memory", "keyspace_hits", "keyspace_misses", "connected_clients":
			if n, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64); err == nil {
				out[kv[0]] = n
			}
		case "used_memory_human":
			// kept as a string elsewhere; not numeric.
		}
	}
	// used_memory_human is string-valued; fetch it separately.
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "used_memory_human:") {
			out["used_memory_human_bytes"] = 0 // marker; human string is re-read by the caller via InfoRaw
		}
	}
	return out, nil
}

// InfoHuman returns the used_memory_human string from INFO.
func (r *phase3Redis) InfoHuman(ctx context.Context) string {
	raw, err := r.client.Info(ctx, "memory").Result()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "used_memory_human:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "used_memory_human:"))
		}
	}
	return ""
}
