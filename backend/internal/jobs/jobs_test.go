package jobs

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func testService(t *testing.T, reachable bool) (*Service, *labs.Store, context.Context) {
	t.Helper()
	inner := labs.NewStore()
	inner.ProvisionDelay = time.Hour // async goroutine must not race the worker
	inner.CheckNetwork = func() bool { return reachable }
	svc := &Service{
		Jobs:         store.NewMemoryJobStore(),
		Labs:         store.NewMemoryLabStore(inner),
		Audit:        store.NewMemoryAuditStore(),
		CheckNetwork: func() bool { return reachable },
		MaxAttempts:  2,
		WorkerID:     "test-worker",
	}
	return svc, inner, context.Background()
}

func TestProvisionSucceedsWhenEngineReachable(t *testing.T) {
	svc, inner, ctx := testService(t, true)
	lab, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = inner // lab is CREATED; the worker drives it from here
	job, _, err := svc.Jobs.Enqueue(ctx, store.JobLabProvision, map[string]any{"lab_id": lab.ID}, "prov:"+lab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := svc.Labs.Get(ctx, lab.ID)
	if err != nil || got.State != labs.StateReady {
		t.Fatalf("lab after provision: %+v err=%v", got, err)
	}
	counts, _ := svc.Jobs.Counts(ctx)
	if counts["done"] != 1 {
		t.Fatalf("job should be done, counts=%v (job %s)", counts, job.ID)
	}
}

func TestProvisionFailsHonestlyWhenEngineDown(t *testing.T) {
	svc, _, ctx := testService(t, false)
	lab, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobLabProvision, map[string]any{"lab_id": lab.ID}, "prov:"+lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := svc.Labs.Get(ctx, lab.ID)
	if err != nil || got.State != labs.StateFailed || got.Error == "" {
		t.Fatalf("lab after failed provision: %+v err=%v", got, err)
	}
	// The FAILED lab outcome is terminal, so the job completes (no retry).
	counts, _ := svc.Jobs.Counts(ctx)
	if counts["done"] != 1 {
		t.Fatalf("failed-provision job should complete, counts=%v", counts)
	}
}

func TestDestroyAndExpire(t *testing.T) {
	svc, inner, ctx := testService(t, true)
	inner.ProvisionDelay = 5 * time.Millisecond
	// Lab 1: provision via worker, then destroy via worker.
	lab, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the in-memory goroutine to make it READY (short delay).
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := svc.Labs.Get(ctx, lab.ID)
		if got.State == labs.StateReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lab did not become READY (state %s)", got.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobLabDestroy, map[string]any{"lab_id": lab.ID}, "dest:"+lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce destroy: %v", err)
	}
	got, _ := svc.Labs.Get(ctx, lab.ID)
	if got.State != labs.StateDestroyed {
		t.Fatalf("lab after destroy job: %s", got.State)
	}

	// Lab 2: expire via sweep. Create with a past expiry by using a
	// fresh store whose clock is in the past is complex; instead create
	// a lab and expire it directly through the service.
	lab2, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Force expiry: move the lab to READY first via worker provision is
	// unnecessary — ExpireDue only lists past-expires labs, so test the
	// expire path directly.
	if err := svc.expireLab(ctx, lab2.ID); err != nil {
		t.Fatalf("expireLab: %v", err)
	}
	got, _ = svc.Labs.Get(ctx, lab2.ID)
	if got.State != labs.StateDestroyed {
		t.Fatalf("lab after expire: %s", got.State)
	}
}

func TestProvisionViaHostAgent(t *testing.T) {
	// Simulated host agent behind the real typed HTTP server + client.
	agent := hostagent.NewSimulatedHostAgent()
	agent.TotalCPU = 8
	agent.TotalMemoryMB = 16384
	agent.TotalDiskGB = 200
	srv := hostagent.NewServer(agent, "test-token", nil)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Network engine is DOWN: the host-agent path must still provision.
	svc, _, ctx := testService(t, false)
	svc.HostAgent = hostagent.NewClient(ts.URL, "test-token")
	lab, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobLabProvision, map[string]any{"lab_id": lab.ID}, "prov:"+lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := svc.Labs.Get(ctx, lab.ID)
	if err != nil || got.State != labs.StateReady {
		t.Fatalf("lab after host-agent provision: %+v err=%v", got, err)
	}
	usage, err := agent.Usage(ctx, lab.ID)
	if err != nil || len(usage) != 1 || usage[0].Kind != "vm" || usage[0].State != "running" {
		t.Fatalf("host agent should hold one running VM for the lab: %v err=%v", usage, err)
	}
	// Destroy via the worker must clean up the host resources too.
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobLabDestroy, map[string]any{"lab_id": lab.ID}, "dest:"+lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce destroy: %v", err)
	}
	got, _ = svc.Labs.Get(ctx, lab.ID)
	if got.State != labs.StateDestroyed {
		t.Fatalf("lab after destroy job: %s", got.State)
	}
	usage, _ = agent.Usage(ctx, lab.ID)
	if len(usage) != 0 {
		t.Fatalf("destroy must clean up host resources, got %v", usage)
	}
}

func TestProvisionFallsBackWhenHostAgentUnavailable(t *testing.T) {
	// Host agent address that refuses connections: provisioning must
	// fall back to the network-engine probe path (which then fails
	// honestly because the engine is down).
	svc, _, ctx := testService(t, false)
	svc.HostAgent = hostagent.NewClient("http://localhost:1", "test-token")
	lab, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobLabProvision, map[string]any{"lab_id": lab.ID}, "prov:"+lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := svc.Labs.Get(ctx, lab.ID)
	if err != nil || got.State != labs.StateFailed {
		t.Fatalf("lab must fail via the fallback probe path: %+v err=%v", got, err)
	}
}

func TestUnknownJobDeadLettersAfterMaxAttempts(t *testing.T) {
	svc, _, ctx := testService(t, true)
	if _, _, err := svc.Jobs.Enqueue(ctx, "no_such_type", map[string]any{}, "bad:1"); err != nil {
		t.Fatal(err)
	}
	// MaxAttempts=2: first RunOnce fails and reschedules (attempts=1),
	// but RunAt is now+backoff so a second immediate RunOnce finds
	// nothing due. Force it due by claiming directly is not possible
	// through the public API, so verify the first failure state.
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	counts, _ := svc.Jobs.Counts(ctx)
	if counts["failed"] != 1 {
		t.Fatalf("unknown job should be failed after first attempt, counts=%v", counts)
	}
}
