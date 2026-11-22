// Integration tests against a real PostgreSQL (and, where relevant,
// the jobs/audit tables). Skipped with a clear message when DATABASE_URL
// is unset or unreachable — same pattern as the hive project.
//
// Run with, e.g.:
//
//	DATABASE_URL=postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable \
//	go test ./internal/store/ -run TestPostgresIntegration -v -count=1
package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/migrations"

	"golang.org/x/crypto/bcrypt"
)

func TestPostgresIntegration(t *testing.T) {
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
	// Migrations are idempotent: a second run must apply nothing new.
	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second migrate should apply nothing, applied %v", applied)
	}
	// Concurrent migrators (api + worker starting together) must not
	// race: both must succeed (advisory lock serializes them).
	{
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				_, err := database.Migrate(context.Background(), db, migrations.FS)
				errs <- err
			}()
		}
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("concurrent migrate: %v", err)
			}
		}
	}

	suffix := ids.New()[:8]
	email := fmt.Sprintf("it-%s@example.com", suffix)
	signer := auth.NewStore("integration-secret")
	users := NewPostgresUserStore(db, signer)
	courses := NewPostgresCourseStore(db)
	labStore := NewPostgresLabStore(db)
	auditStore := NewPostgresAuditStore(db)
	jobStore := NewPostgresJobStore(db)

	// Users: Argon2id register/authenticate, roles.
	u, err := users.Register(email, "Integration", "correct-horse")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.Role != auth.RoleLearner {
		t.Fatalf("new user role: %q", u.Role)
	}
	if _, err := users.Register(email, "Integration", "correct-horse"); err != auth.ErrUserExists {
		t.Fatalf("duplicate register: got %v, want ErrUserExists", err)
	}
	if _, err := users.Authenticate(email, "wrong-password"); err != auth.ErrInvalidPassword {
		t.Fatalf("wrong password: got %v, want ErrInvalidPassword", err)
	}
	got, err := users.Authenticate(email, "correct-horse")
	if err != nil || got.ID != u.ID {
		t.Fatalf("authenticate: %v %+v", err, got)
	}
	// M9 dual-verify: downgrade the stored hash to a legacy bcrypt
	// hash and confirm a successful login upgrades it to Argon2id.
	legacy, err := bcrypt.GenerateFromPassword([]byte("correct-horse"), 12)
	if err != nil {
		t.Fatalf("bcrypt legacy hash: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE users SET password_hash=$2 WHERE id=$1", u.ID, string(legacy)); err != nil {
		t.Fatalf("downgrade hash: %v", err)
	}
	if _, err := users.Authenticate(email, "correct-horse"); err != nil {
		t.Fatalf("legacy bcrypt login: %v", err)
	}
	var storedHash string
	if err := db.Pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", u.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read upgraded hash: %v", err)
	}
	if !auth.IsArgon2idHash(storedHash) {
		t.Fatalf("login should have re-hashed bcrypt to Argon2id, stored hash is %q", storedHash)
	}
	admin, err := users.SetRole(u.ID, auth.RoleAdmin)
	if err != nil || admin.Role != auth.RoleAdmin {
		t.Fatalf("SetRole admin: %v %+v", err, admin)
	}
	list, err := users.List()
	if err != nil || len(list) == 0 {
		t.Fatalf("List users: %v %d", err, len(list))
	}

	// Courses: seeded catalogue, enroll, progress + XP.
	all, err := courses.List(ctx)
	if err != nil || len(all) < 3 {
		t.Fatalf("seeded courses: %d err=%v", len(all), err)
	}
	c, err := courses.Get(ctx, "vpc-fundamentals")
	if err != nil || c.LessonCount() != 6 {
		t.Fatalf("vpc course: %+v err=%v", c, err)
	}
	lessonID := c.Modules[0].Lessons[0].ID
	if err := courses.Enroll(ctx, u.ID, "vpc-fundamentals"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	prog, awarded, err := courses.CompleteLesson(ctx, u.ID, "vpc-fundamentals", lessonID)
	if err != nil || awarded != 20 {
		t.Fatalf("complete lesson: prog=%+v awarded=%d err=%v", prog, awarded, err)
	}
	_, awarded, err = courses.CompleteLesson(ctx, u.ID, "vpc-fundamentals", lessonID)
	if err != nil || awarded != 0 {
		t.Fatalf("re-complete must award 0 XP: awarded=%d err=%v", awarded, err)
	}

	// Labs: create writes session + resource + job in one transaction,
	// idempotency replays, transitions persist atomically.
	lab, replayed, err := labStore.Create(ctx, u.ID, "ec2-basics", "it lab", "it-key-"+suffix)
	if err != nil || replayed {
		t.Fatalf("lab create: %+v replayed=%v err=%v", lab, replayed, err)
	}
	if lab.State != labs.StateCreated {
		t.Fatalf("new lab state: %s", lab.State)
	}
	lab2, replayed, err := labStore.Create(ctx, u.ID, "ec2-basics", "other", "it-key-"+suffix)
	if err != nil || !replayed || lab2.ID != lab.ID {
		t.Fatalf("idempotent replay: %+v replayed=%v err=%v", lab2, replayed, err)
	}
	counts, err := jobStore.Counts(ctx)
	if err != nil {
		t.Fatalf("job counts: %v", err)
	}
	if counts["pending"] < 1 {
		t.Fatalf("lab_provision job should be pending, counts=%v", counts)
	}
	// Claim + complete the provision job through the queue.
	claimed, err := jobStore.Claim(ctx, "integration-worker", 5)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	var provisioned *Job
	for _, j := range claimed {
		if j.Type == JobLabProvision {
			if payloadLab, ok := j.Payload["lab_id"].(string); ok && payloadLab == lab.ID {
				provisioned = j
			}
		}
	}
	if provisioned == nil {
		t.Fatalf("provision job for lab %s not claimed: %+v", lab.ID, claimed)
	}
	if err := jobStore.Complete(ctx, provisioned.ID); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	ready, err := labStore.Transition(ctx, lab.ID, labs.StateProvisioning, "")
	if err != nil || ready.State != labs.StateProvisioning {
		t.Fatalf("transition to PROVISIONING: %v %+v", err, ready)
	}
	if _, err := labStore.Transition(ctx, lab.ID, labs.StateReady, ""); err != nil {
		t.Fatalf("transition to READY: %v", err)
	}
	if _, err := labStore.Transition(ctx, lab.ID, labs.StateCompleted, ""); err == nil {
		t.Fatal("READY -> COMPLETED must be an illegal transition (no partial write)")
	}
	still, err := labStore.Get(ctx, lab.ID)
	if err != nil || still.State != labs.StateReady {
		t.Fatalf("illegal transition must not change state: %+v err=%v", still, err)
	}

	// Audit: rows were written by lab creation/transitions; list works.
	if err := auditStore.Record(ctx, AuditEntry{ActorID: u.ID, Action: "integration_check", ResourceType: "lab", ResourceID: lab.ID}); err != nil {
		t.Fatalf("audit record: %v", err)
	}
	entries, total, err := auditStore.List(ctx, 10, 0)
	if err != nil || total < 1 || len(entries) == 0 {
		t.Fatalf("audit list: total=%d entries=%d err=%v", total, len(entries), err)
	}
	// Regression (Phase 6, found live 2026-11-22): a failed login is
	// audited with an empty actor (no user authenticated). The old
	// literal "anonymous" made the Postgres insert fail — user_id is
	// a UUID column — so GuardDuty scans saw zero failed logins.
	// An empty actor must record as NULL and stay listable by action;
	// a non-UUID actor must keep failing, so nobody reintroduces a
	// literal actor that silently drops audit evidence.
	if err := auditStore.Record(ctx, AuditEntry{ActorID: "anonymous", Action: "auth.login_failed", ResourceType: "user", ResourceID: ""}); err == nil {
		t.Fatalf("audit record with non-UUID actor %q should fail (user_id is a UUID column)", "anonymous")
	}
	if err := auditStore.Record(ctx, AuditEntry{ActorID: "", Action: "auth.login_failed", ResourceType: "user", ResourceID: "", Metadata: map[string]any{"client_ip": "203.0.113.9", "email": email}}); err != nil {
		t.Fatalf("audit record with empty actor: %v", err)
	}
	failed, _, err := auditStore.ListFiltered(ctx, AuditFilter{Action: "auth.login_failed", Limit: 10})
	if err != nil {
		t.Fatalf("audit list filtered: %v", err)
	}
	found := false
	for _, e := range failed {
		if e.Metadata["client_ip"] == "203.0.113.9" {
			found = true
		}
	}
	if !found {
		t.Fatalf("anonymous failed-login audit entry not found by ListFiltered (got %d auth.login_failed entries)", len(failed))
	}
}

// TestPostgresRevocationIntegration verifies the Phase 7 durable
// denylist (migration 013): a revoked jti is denied, another
// user's jti is not, and RevokeUser denies every recorded jti.
func TestPostgresRevocationIntegration(t *testing.T) {
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
	rev := NewPostgresRevoker(db)
	suffix := ids.New()[:8]
	userA := "rev-user-a-" + suffix
	userB := "rev-user-b-" + suffix
	exp := time.Now().Add(time.Hour)
	jtiA1, jtiA2, jtiB := "jti-a1-"+suffix, "jti-a2-"+suffix, "jti-b-"+suffix
	for _, rec := range [][3]string{{jtiA1, userA, ""}, {jtiA2, userA, ""}, {jtiB, userB, ""}} {
		if err := rev.RecordIssued(ctx, rec[0], rec[1], exp); err != nil {
			t.Fatalf("RecordIssued %s: %v", rec[0], err)
		}
	}
	if revoked, err := rev.IsRevoked(ctx, jtiA1); err != nil || revoked {
		t.Fatalf("fresh jti must not be revoked: %v %v", revoked, err)
	}
	if err := rev.Revoke(ctx, jtiA1, userA, exp); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked, err := rev.IsRevoked(ctx, jtiA1); err != nil || !revoked {
		t.Fatalf("revoked jti must be denied: %v %v", revoked, err)
	}
	// jtiA1 was already revoked individually, so RevokeUser inserts
	// only the one remaining jti (n counts newly revoked rows).
	if n, err := rev.RevokeUser(ctx, userA); err != nil || n != 1 {
		t.Fatalf("RevokeUser(userA) = %d, err %v, want 1 newly revoked jti", n, err)
	}
	for _, jti := range []string{jtiA1, jtiA2} {
		if revoked, err := rev.IsRevoked(ctx, jti); err != nil || !revoked {
			t.Fatalf("jti %s must be denied after RevokeUser: %v %v", jti, revoked, err)
		}
	}
	if revoked, err := rev.IsRevoked(ctx, jtiB); err != nil || revoked {
		t.Fatalf("other user's jti must not be revoked: %v %v", revoked, err)
	}
	// Cleanup test rows.
	_, _ = db.Pool.Exec(ctx, "DELETE FROM revoked_tokens WHERE user_id IN ($1,$2)", userA, userB)
	_, _ = db.Pool.Exec(ctx, "DELETE FROM issued_tokens WHERE user_id IN ($1,$2)", userA, userB)
}
