package cache

import (
	"context"
	"testing"
)

func TestDisabledCacheIsFailOpen(t *testing.T) {
	ctx := context.Background()
	// Nothing listens on this port: New must report the failure and
	// return a disabled cache instead of erroring the caller.
	c, err := New("127.0.0.1:1")
	if err == nil {
		t.Fatal("New with an unreachable Redis must return an error")
	}
	if c.Available() {
		t.Fatal("unreachable Redis must not be reported available")
	}
	if c.Ping(ctx) {
		t.Fatal("Ping on a disabled cache must return false")
	}
	// All operations must be safe no-ops / misses.
	var dst []string
	if c.GetCourses(ctx, &dst) {
		t.Fatal("disabled cache must miss")
	}
	c.SetCourses(ctx, []string{"x"})
	c.InvalidateCourses(ctx)
	if err := c.Close(); err != nil {
		t.Fatalf("Close on disabled cache: %v", err)
	}
}

func TestPingOnNilCache(t *testing.T) {
	var c *Cache
	if c.Available() || c.Ping(context.Background()) {
		t.Fatal("nil cache must report unavailable")
	}
}
