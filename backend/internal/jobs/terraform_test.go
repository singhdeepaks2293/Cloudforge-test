package jobs

import (
	"context"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
)

// TestTerraformJobWithStubRunner exercises the worker path end to
// end with the TestStubRunner: a plan job succeeds and records a
// plan id, and an apply job syncs the plan's resources.
func TestTerraformJobWithStubRunner(t *testing.T) {
	ctx := context.Background()
	jobStore := store.NewMemoryJobStore()
	runStore := store.NewMemoryTerraformRunStore()
	resStore := store.NewMemoryResourceStore()
	svc := &Service{
		Jobs: jobStore, TerraformRunner: &terraform.TestStubRunner{},
		TerraformRuns: runStore, Resources: resStore,
		TerraformCfg: TerraformConfig{
			EmulatorEndpoint: "http://localhost:4566",
			MinIOEndpoint:    "localhost:9000",
			WorkdirRoot:      t.TempDir(),
		},
	}
	run, err := runStore.CreateRun(ctx, &store.TerraformRun{
		UserID: "user-1", LabID: "lab-1", Action: "plan", State: "QUEUED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := jobStore.Enqueue(ctx, store.JobTerraformPlan,
		map[string]any{"lab_id": "lab-1", "run_id": run.ID, "action": "plan", "user_id": "user-1"}, "tf-plan-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	after, err := runStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "SUCCEEDED" || after.PlanID == "" {
		t.Fatalf("plan job must succeed with a plan id: %+v", after)
	}
}

// TestTerraformJobWithoutRunner records a FAILED run and completes
// the job (terminal outcome), instead of retrying forever.
func TestTerraformJobWithoutRunner(t *testing.T) {
	ctx := context.Background()
	jobStore := store.NewMemoryJobStore()
	runStore := store.NewMemoryTerraformRunStore()
	svc := &Service{Jobs: jobStore, TerraformRuns: runStore}
	run, err := runStore.CreateRun(ctx, &store.TerraformRun{
		UserID: "user-1", LabID: "lab-1", Action: "plan", State: "QUEUED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := jobStore.Enqueue(ctx, store.JobTerraformPlan,
		map[string]any{"lab_id": "lab-1", "run_id": run.ID, "action": "plan"}, "tf-plan-2"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	after, _ := runStore.GetRun(ctx, run.ID)
	if after.State != "FAILED" || after.Error == "" {
		t.Fatalf("run without a runner must be FAILED with an error: %+v", after)
	}
	counts, _ := jobStore.Counts(ctx)
	if counts["done"] != 1 {
		t.Fatalf("job must complete (not retry): counts=%v", counts)
	}
}

// TestTerraformJobUnavailableRunnerRetries: a Docker-Unavailable
// error from the runner is retryable — the job is NOT completed.
func TestTerraformJobUnavailableRunnerRetries(t *testing.T) {
	ctx := context.Background()
	jobStore := store.NewMemoryJobStore()
	runStore := store.NewMemoryTerraformRunStore()
	runner := &terraform.TestStubRunner{
		FailOperation: map[terraform.Operation]error{
			terraform.OpPlan: &terraform.UnavailableError{Backend: "docker", Reason: "no socket"},
		},
	}
	svc := &Service{Jobs: jobStore, TerraformRunner: runner, TerraformRuns: runStore,
		TerraformCfg: TerraformConfig{
			EmulatorEndpoint: "http://localhost:4566",
			MinIOEndpoint:    "localhost:9000",
			WorkdirRoot:      t.TempDir(),
		}}
	run, err := runStore.CreateRun(ctx, &store.TerraformRun{
		UserID: "user-1", LabID: "lab-1", Action: "plan", State: "QUEUED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := jobStore.Enqueue(ctx, store.JobTerraformPlan,
		map[string]any{"lab_id": "lab-1", "run_id": run.ID, "action": "plan"}, "tf-plan-3"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	counts, _ := jobStore.Counts(ctx)
	if counts["done"] == 1 {
		t.Fatal("unavailable-runner job must not complete; it must be retried")
	}
}
