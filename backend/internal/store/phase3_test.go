// Phase 3 store tests: memory CRUD + Postgres integration (skipped
// when DATABASE_URL is unset or unreachable, like the other store
// integration tests). Migration 009 must apply before the Postgres
// test runs.
package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func TestMemoryPhase3StoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryPhase3Store()
	e, err := s.P3Create(ctx, "sqs_queues", &P3Entity{Name: "q1", Data: map[string]any{"visibility_timeout_seconds": 30}})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" || e.CreatedAt.IsZero() {
		t.Error("created entity needs an id and timestamps")
	}
	if _, err := s.P3Create(ctx, "sqs_queues", &P3Entity{Name: "q1", Data: map[string]any{}}); !errors.Is(err, ErrP3Conflict) {
		t.Errorf("duplicate name must conflict, got %v", err)
	}
	got, err := s.P3GetByName(ctx, "sqs_queues", "q1")
	if err != nil || got.ID != e.ID {
		t.Fatalf("get by name: %v", err)
	}
	got.Data["visibility_timeout_seconds"] = 60
	updated, err := s.P3Update(ctx, "sqs_queues", got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Data["visibility_timeout_seconds"] != 60 {
		t.Error("update did not persist")
	}
	list, err := s.P3List(ctx, "sqs_queues")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d, err %v", len(list), err)
	}
	if err := s.P3Delete(ctx, "sqs_queues", e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.P3Get(ctx, "sqs_queues", e.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted entity must be gone, got %v", err)
	}
	if _, err := s.P3Create(ctx, "no-such-collection", &P3Entity{Name: "x"}); err == nil {
		t.Error("unknown collection must be rejected")
	}
	if _, err := s.P3Create(ctx, "sqs_queues", &P3Entity{Name: ""}); err == nil {
		t.Error("empty name must be rejected")
	}
}

func TestPostgresPhase3Integration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is unset; integration test skipped (no PostgreSQL available)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL at DATABASE_URL is unreachable (%v); integration test skipped", err)
	}
	defer db.Close()
	if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := NewPostgresPhase3Store(db)
	e, err := s.P3Create(ctx, "sqs_queues", &P3Entity{Name: "pg-q1", Data: map[string]any{"visibility_timeout_seconds": 30}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.P3Delete(ctx, "sqs_queues", e.ID) }()
	if _, err := s.P3Create(ctx, "sqs_queues", &P3Entity{Name: "pg-q1", Data: map[string]any{}}); !errors.Is(err, ErrP3Conflict) {
		t.Errorf("duplicate name must conflict in Postgres, got %v", err)
	}
	got, err := s.P3Get(ctx, "sqs_queues", e.ID)
	if err != nil || got.Name != "pg-q1" {
		t.Fatalf("postgres get: %v", err)
	}
	// A message row in a different collection, then list it back.
	m, err := s.P3Create(ctx, "sqs_messages", &P3Entity{Name: "msg-1", Data: map[string]any{"queue_id": e.ID, "body": "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.P3Delete(ctx, "sqs_messages", m.ID) }()
	list, err := s.P3List(ctx, "sqs_messages")
	if err != nil || len(list) == 0 {
		t.Fatalf("postgres list messages: %d, err %v", len(list), err)
	}
}
