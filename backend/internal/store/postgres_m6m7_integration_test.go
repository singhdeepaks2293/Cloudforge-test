// Postgres integration for the Milestone 6/7 stores (migration 004):
// lab resources, terraform runs (plan/approval), terminal sessions.
// Skipped when DATABASE_URL is unset or unreachable, like the main
// integration test.
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
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func TestPostgresM6M7Integration(t *testing.T) {
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
	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Logf("migrations applied in this run: %v (004 must have applied at least once on a fresh DB)", applied)

	suffix := ids.New()[:8]
	signer := auth.NewStore("integration-secret-m6m7")
	users := NewPostgresUserStore(db, signer)
	u, err := users.Register(fmt.Sprintf("it-m6m7-%s@example.com", suffix), "Integration M6M7", "correct-horse")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	labStore := NewPostgresLabStore(db)
	lab, _, err := labStore.Create(ctx, u.ID, "ec2-basics", "it m6m7 lab", "it-m6m7-"+suffix)
	if err != nil {
		t.Fatalf("create lab: %v", err)
	}

	// Lab resources: the VPC row written at lab creation must be
	// listable, and a new resource round-trips with its config.
	resources := NewPostgresResourceStore(db)
	list, err := resources.ListResources(ctx, lab.ID)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("lab creation must have written a VPC resource row")
	}
	if list[0].Kind != "vpc" {
		t.Fatalf("first resource kind: %q", list[0].Kind)
	}
	created, err := resources.CreateResource(ctx, &LabResource{
		LabID: lab.ID, OwnerID: u.ID, Kind: "subnet", Name: "public",
		State: "available", Provider: "rust-network-engine", CIDR: "10.0.1.0/24",
		Config: map[string]any{"cidr": "10.0.1.0/24"},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	got, err := resources.GetResource(ctx, lab.ID, created.ID)
	if err != nil || got.CIDR != "10.0.1.0/24" {
		t.Fatalf("get resource: %+v err=%v", got, err)
	}
	deleted, err := resources.DeleteResource(ctx, lab.ID, created.ID)
	if err != nil || !deleted {
		t.Fatalf("delete resource: deleted=%v err=%v", deleted, err)
	}
	deleted, err = resources.DeleteResource(ctx, lab.ID, created.ID)
	if err != nil || deleted {
		t.Fatalf("second delete must be idempotent (deleted=false): %v %v", deleted, err)
	}

	// Terraform runs: create, plan fields, approval, lookup by plan.
	runs := NewPostgresTerraformRunStore(db)
	run, err := runs.CreateRun(ctx, &TerraformRun{
		UserID: u.ID, LabID: lab.ID, Action: "plan", State: "QUEUED",
		Configuration: `resource "aws_vpc" "lab" {}`,
	})
	if err != nil {
		t.Fatalf("create terraform run: %v", err)
	}
	run.State = "SUCCEEDED"
	run.PlanID = "plan-" + suffix
	run.PlanJSON = map[string]any{"format_version": "1.2", "planned_values": map[string]any{}}
	exp := time.Now().UTC().Add(time.Hour)
	run.PlanExpiresAt = &exp
	now := time.Now().UTC()
	run.ApprovedAt = &now
	run.ApprovedBy = u.ID
	updated, err := runs.UpdateRun(ctx, run)
	if err != nil || updated.PlanID != run.PlanID || updated.ApprovedBy != u.ID {
		t.Fatalf("update run: %+v err=%v", updated, err)
	}
	byPlan, err := runs.GetRunByPlanID(ctx, "plan-"+suffix)
	if err != nil || byPlan.ID != run.ID {
		t.Fatalf("get run by plan id: %+v err=%v", byPlan, err)
	}
	history, err := runs.ListRunsByLab(ctx, lab.ID, 10)
	if err != nil || len(history) == 0 {
		t.Fatalf("list runs: %d err=%v", len(history), err)
	}

	// Terminal sessions: create, mark used, end.
	sessions := NewPostgresTerminalSessionStore(db)
	sess, err := sessions.CreateSession(ctx, &TerminalSession{
		UserID: u.ID, LabID: lab.ID,
		TokenHash: "deadbeef" + suffix, ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		Mode: "simulated",
	})
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	if err := sessions.MarkSessionUsed(ctx, sess.ID); err != nil {
		t.Fatalf("mark session used: %v", err)
	}
	ended := time.Now().UTC()
	if err := sessions.EndSession(ctx, sess.ID, ended); err != nil {
		t.Fatalf("end session: %v", err)
	}
	gotSess, err := sessions.GetSession(ctx, sess.ID)
	if err != nil || !gotSess.Used || gotSess.EndedAt == nil {
		t.Fatalf("get session: %+v err=%v", gotSess, err)
	}
}
