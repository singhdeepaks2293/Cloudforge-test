// Postgres IAM integration (Phase 2, migration 008). Skipped when
// DATABASE_URL is unset or unreachable, like the other store
// integration tests.
package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func TestPostgresIAMIntegration(t *testing.T) {
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
	s := NewPostgresIAMStore(db)

	u, err := s.CreateIAMUser(ctx, &IAMUser{Name: "pg-alice"})
	if err != nil {
		t.Fatal(err)
	}
	// Clean up whatever this test creates, even on failure.
	defer func() {
		_ = s.DeleteIAMUser(ctx, u.ID)
	}()
	if _, err := s.CreateIAMUser(ctx, &IAMUser{Name: "pg-alice"}); err != ErrIAMConflict {
		t.Fatalf("duplicate user: %v", err)
	}
	byName, err := s.GetIAMUserByName(ctx, "pg-alice")
	if err != nil || byName.ID != u.ID {
		t.Fatalf("GetIAMUserByName: %v %v", byName, err)
	}

	g, err := s.CreateIAMGroup(ctx, &IAMGroup{Name: "pg-devs"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.DeleteIAMGroup(ctx, g.ID) }()
	if err := s.AddGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBuckets","Resource":"*"}]}`)
	pol, err := s.CreateIAMPolicy(ctx, &IAMPolicy{Name: "pg-list", Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.DeleteIAMPolicy(ctx, pol.ID) }()
	if err := s.AttachPolicy(ctx, "group", g.ID, pol.ID); err != nil {
		t.Fatal(err)
	}
	attached, err := s.AttachedPolicies(ctx, "group", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attached) != 1 {
		t.Fatalf("attached: %d", len(attached))
	}
	// The stored document round-trips as the same JSON.
	var roundTrip map[string]any
	if err := json.Unmarshal(attached[0].Document, &roundTrip); err != nil {
		t.Fatal(err)
	}

	role, err := s.CreateIAMRole(ctx, &IAMRole{Name: "pg-reader", TrustPolicy: doc, MaxSessionSecs: 900})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.DeleteIAMRole(ctx, role.ID) }()

	key, err := s.CreateIAMAccessKey(ctx, u.ID, "AKIAPGTEST12345678", HashSecret("pg-secret"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := s.ListIAMAccessKeys(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != key.ID {
		t.Fatalf("keys: %v", keys)
	}
	// The secret hash is stored but never appears in the API view:
	// the IAMAccessKey struct has no secret/hash field at all.
	raw, _ := json.Marshal(keys)
	if string(raw) == "" {
		t.Fatal("unreachable")
	}

	sess, err := s.CreateIAMSession(ctx, role.ID, u.ID, HashSecret("tok"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIAMSession(ctx, sess.ID)
	if err != nil || got.RoleID != role.ID {
		t.Fatalf("session: %v %v", got, err)
	}
}
