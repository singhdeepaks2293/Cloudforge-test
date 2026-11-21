package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testClient(t *testing.T) redis.Cmdable {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 2 * time.Second, MaxRetries: 1})
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis at %s is unavailable (%v); rate-limit tests skipped", addr, err)
	}
	return c
}

func scope(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

func TestAllowWithinLimitAndDeniesOver(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	l := New(c, scope(t), 3, time.Minute)
	for i := int64(1); i <= 3; i++ {
		res, err := l.Allow(ctx, "192.0.2.1")
		if err != nil {
			t.Fatalf("Allow %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		if res.Remaining != 3-i {
			t.Fatalf("request %d: remaining %d, want %d", i, res.Remaining, 3-i)
		}
	}
	res, err := l.Allow(ctx, "192.0.2.1")
	if err != nil {
		t.Fatalf("Allow over limit: %v", err)
	}
	if res.Allowed || res.Remaining != 0 {
		t.Fatalf("4th request should be denied with 0 remaining, got %+v", res)
	}
	// A different identifier has its own counter.
	res, err = l.Allow(ctx, "192.0.2.2")
	if err != nil || !res.Allowed {
		t.Fatalf("different IP should be allowed: %+v %v", res, err)
	}
}

func TestWindowResets(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	l := New(c, scope(t), 1, time.Second)
	if res, err := l.Allow(ctx, "192.0.2.1"); err != nil || !res.Allowed {
		t.Fatalf("first request: %+v %v", res, err)
	}
	if res, err := l.Allow(ctx, "192.0.2.1"); err != nil || res.Allowed {
		t.Fatalf("second request in window should be denied: %+v %v", res, err)
	}
	time.Sleep(1100 * time.Millisecond)
	if res, err := l.Allow(ctx, "192.0.2.1"); err != nil || !res.Allowed {
		t.Fatalf("request after window expiry should be allowed: %+v %v", res, err)
	}
}

func TestScopesAreIndependent(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	a := New(c, scope(t), 1, time.Minute)
	b := New(c, scope(t), 1, time.Minute)
	if _, err := a.Allow(ctx, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if res, err := b.Allow(ctx, "192.0.2.1"); err != nil || !res.Allowed {
		t.Fatalf("different scope must have its own counter: %+v %v", res, err)
	}
}

func TestRedisDownReturnsUnavailable(t *testing.T) {
	dead := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6399", DialTimeout: 200 * time.Millisecond, MaxRetries: 0,
	})
	t.Cleanup(func() { _ = dead.Close() })
	l := New(dead, scope(t), 3, time.Minute)
	_, err := l.Allow(context.Background(), "192.0.2.1")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Redis down: got err=%v, want ErrUnavailable", err)
	}
}
