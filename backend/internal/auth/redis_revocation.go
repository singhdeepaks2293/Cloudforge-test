// Package auth — Redis-backed token revocation (Phase 7, risk R3).
//
// Keys (all with a TTL equal to the token's remaining lifetime, so
// nothing is kept after the token would have expired anyway):
//
//	revoked_token:<jti>        -> "1" (SETEX)
//	issued_tokens:<userID>     -> SET of jtis (each member expires
//	                              via a per-set TTL refreshed on issue)
//
// RevokeUser reads the user's issued set and revokes every member.
// If Redis is down, every method returns an error and the caller
// (auth.Store) falls back to its in-memory revoker, so a Redis
// outage degrades revocation to single-process scope instead of
// silently accepting a revoked token.
package auth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRevoker is a Revoker backed by Redis.
type RedisRevoker struct {
	client redis.Cmdable
}

// NewRedisRevoker builds a RedisRevoker over client (go-redis v9
// Cmdable: *redis.Client, *redis.ClusterClient, or a test stub).
func NewRedisRevoker(client redis.Cmdable) *RedisRevoker {
	return &RedisRevoker{client: client}
}

func revokedKey(jti string) string { return "revoked_token:" + jti }
func issuedKey(userID string) string {
	return "issued_tokens:" + userID
}

func (r *RedisRevoker) RecordIssued(ctx context.Context, jti, userID string, exp time.Time) error {
	ttl := time.Until(exp)
	if ttl <= 0 {
		return nil
	}
	pipe := r.client.Pipeline()
	pipe.SAdd(ctx, issuedKey(userID), jti)
	// The set only needs to outlive the longest-lived token in it;
	// refresh its TTL on every issue (12h tokens -> 12h TTL).
	pipe.Expire(ctx, issuedKey(userID), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisRevoker) Revoke(ctx context.Context, jti, userID string, exp time.Time) error {
	ttl := time.Until(exp)
	if ttl <= 0 {
		return nil
	}
	return r.client.Set(ctx, revokedKey(jti), "1", ttl).Err()
}

func (r *RedisRevoker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	n, err := r.client.Exists(ctx, revokedKey(jti)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *RedisRevoker) RevokeUser(ctx context.Context, userID string) (int, error) {
	jtis, err := r.client.SMembers(ctx, issuedKey(userID)).Result()
	if err != nil {
		return 0, err
	}
	if len(jtis) == 0 {
		return 0, nil
	}
	// Revoke each jti for the remainder of its token lifetime. The
	// issued set carries no per-jti expiry, so use the token TTL
	// (12h) as the bound: a revoked jti is harmless after the token
	// would have expired anyway.
	pipe := r.client.Pipeline()
	for _, jti := range jtis {
		pipe.Set(ctx, revokedKey(jti), "1", TokenTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return len(jtis), nil
}
