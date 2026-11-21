package jobs

// Milestone 9, Part 3: cleanup reconciliation tests, using fake
// engine/host-agent stores (no gRPC server, no Docker/KVM).
import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// fakeEngine is an in-memory EngineTopology.
type fakeEngine struct {
	nodes    map[string][]TopologyNode // labID -> nodes
	deleted  []string                  // subnet/network ids removed
	failNext error
}

func (f *fakeEngine) InspectTopology(ctx context.Context, labID, ownerID string) ([]TopologyNode, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	return f.nodes[labID], nil
}

func (f *fakeEngine) DeleteSubnet(ctx context.Context, labID, ownerID, subnetID string) error {
	f.deleted = append(f.deleted, "subnet:"+subnetID)
	nodes := f.nodes[labID]
	kept := nodes[:0]
	for _, n := range nodes {
		if n.ID != subnetID {
			kept = append(kept, n)
		}
	}
	f.nodes[labID] = kept
	return nil
}

func (f *fakeEngine) DeleteNetwork(ctx context.Context, labID, ownerID string) error {
	f.deleted = append(f.deleted, "network:"+labID)
	f.nodes[labID] = nil
	return nil
}

// fakeHostAgent is an in-memory hostagent.Provisioner.
type fakeHostAgent struct {
	vms      map[string][]string // labID -> vm ids
	deleted  []string
	failNext error
}

func (f *fakeHostAgent) Do(ctx context.Context, req hostagent.OpRequest) (hostagent.OpResult, error) {
	if f.failNext != nil {
		return hostagent.OpResult{}, f.failNext
	}
	switch req.Op {
	case hostagent.OpListVMs:
		var items []map[string]any
		for _, id := range f.vms[req.LabID] {
			items = append(items, map[string]any{"id": id, "name": id, "state": "running"})
		}
		return hostagent.OpResult{Op: req.Op, Items: items}, nil
	case hostagent.OpDeleteVM:
		f.deleted = append(f.deleted, req.ID)
		kept := []string{}
		for _, id := range f.vms[req.LabID] {
			if id != req.ID {
				kept = append(kept, id)
			}
		}
		f.vms[req.LabID] = kept
		return hostagent.OpResult{Op: req.Op, ID: req.ID}, nil
	default:
		return hostagent.OpResult{}, errors.New("fake host agent: unsupported op " + string(req.Op))
	}
}

func (f *fakeHostAgent) CleanupLab(ctx context.Context, labID, ownerID string) error {
	f.vms[labID] = nil
	return nil
}

func reconcileService(t *testing.T) (*Service, *fakeEngine, *fakeHostAgent, *store.MemoryResourceStore, context.Context) {
	t.Helper()
	inner := labs.NewStore()
	eng := &fakeEngine{nodes: map[string][]TopologyNode{}}
	ha := &fakeHostAgent{vms: map[string][]string{}}
	res := store.NewMemoryResourceStore()
	svc := &Service{
		Jobs:        store.NewMemoryJobStore(),
		Labs:        store.NewMemoryLabStore(inner),
		Audit:       store.NewMemoryAuditStore(),
		Resources:   res,
		Engine:      eng,
		HostAgent:   ha,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxAttempts: 2,
		WorkerID:    "test-reconcile",
	}
	return svc, eng, ha, res, context.Background()
}

func seedLabWithResources(t *testing.T, svc *Service, res *store.MemoryResourceStore, ctx context.Context) string {
	t.Helper()
	lab, _, err := svc.Labs.Create(ctx, "u1", "vpc-fundamentals", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Healthy baseline: VPC row + engine network, one subnet row +
	// engine subnet, one instance row + host-agent VM.
	_, err = res.CreateResource(ctx, &store.LabResource{LabID: lab.ID, OwnerID: "u1", Kind: "vpc", Name: "main-vpc", State: "created", Provider: "rust-network-engine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.CreateResource(ctx, &store.LabResource{LabID: lab.ID, OwnerID: "u1", Kind: "subnet", Name: "public", State: "created", Provider: "rust-network-engine", ProviderResourceID: "subnet-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := res.CreateResource(ctx, &store.LabResource{LabID: lab.ID, OwnerID: "u1", Kind: "instance", Name: "web", State: "defined", Provider: "host-agent", ProviderResourceID: "vm-1"}); err != nil {
		t.Fatal(err)
	}
	return lab.ID
}

func TestReconcileHealthyLabReportsNoOrphans(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	eng.nodes[labID] = []TopologyNode{{ID: labID, Kind: "network"}, {ID: "subnet-1", Kind: "subnet"}}
	ha.vms[labID] = []string{"vm-1"}

	rep, err := svc.Reconcile(ctx, labID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DBOrphans) != 0 || len(rep.LiveOrphans) != 0 {
		t.Fatalf("healthy lab should have no orphans: %+v", rep)
	}
	if rep.RemovedDB != 0 || rep.RemovedLive != 0 {
		t.Fatalf("dry-run must not remove anything: %+v", rep)
	}
}

func TestReconcileDetectsOrphansBothWaysDryRun(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	// DB subnet row "subnet-1" has no engine counterpart; engine
	// has an extra subnet "subnet-9" and the host agent an extra
	// VM "vm-9", neither recorded in the DB.
	eng.nodes[labID] = []TopologyNode{{ID: labID, Kind: "network"}, {ID: "subnet-9", Kind: "subnet"}}
	ha.vms[labID] = []string{"vm-1", "vm-9"}

	rep, err := svc.Reconcile(ctx, labID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DBOrphans) != 1 || rep.DBOrphans[0].ID == "" {
		t.Fatalf("expected 1 DB orphan, got %+v", rep.DBOrphans)
	}
	if len(rep.LiveOrphans) != 2 {
		t.Fatalf("expected 2 live orphans, got %+v", rep.LiveOrphans)
	}
	if rep.RemovedDB != 0 || rep.RemovedLive != 0 || len(eng.deleted) != 0 || len(ha.deleted) != 0 {
		t.Fatal("dry-run must not delete anything")
	}
	// Nothing was deleted, so the DB still has all rows.
	rows, _ := res.ListResources(ctx, labID)
	if len(rows) != 3 {
		t.Fatalf("dry-run must leave DB rows alone, got %d", len(rows))
	}
}

func TestReconcileEnforcementRemovesOrphanedSide(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	eng.nodes[labID] = []TopologyNode{{ID: labID, Kind: "network"}, {ID: "subnet-9", Kind: "subnet"}}
	ha.vms[labID] = []string{"vm-1", "vm-9"}

	rep, err := svc.Reconcile(ctx, labID, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RemovedDB != 1 || rep.RemovedLive != 2 {
		t.Fatalf("expected 1 DB + 2 live removals, got %+v", rep)
	}
	rows, _ := res.ListResources(ctx, labID)
	for _, r := range rows {
		if r.Kind == "subnet" {
			t.Fatal("orphan subnet row should have been deleted")
		}
	}
	if len(eng.nodes[labID]) != 1 || eng.nodes[labID][0].ID != labID {
		t.Fatalf("live orphan subnet should be gone from the engine: %v", eng.nodes[labID])
	}
	if len(ha.vms[labID]) != 1 || ha.vms[labID][0] != "vm-1" {
		t.Fatalf("live orphan VM should be gone from the host agent: %v", ha.vms[labID])
	}
	// Audit events were recorded for the removals.
	entries, _, _ := svc.Audit.List(ctx, 100, 0)
	removed := 0
	for _, e := range entries {
		if e.Action == "cleanup_reconcile_removed_db" || e.Action == "cleanup_reconcile_removed_live" {
			removed++
		}
	}
	if removed != 3 {
		t.Fatalf("expected 3 removal audit events, got %d", removed)
	}
}

func TestReconcileEngineDownSkipsEngineChecks(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	eng.failNext = errors.New("connection refused")
	ha.vms[labID] = []string{"vm-1"}

	rep, err := svc.Reconcile(ctx, labID, false)
	if err != nil {
		t.Fatal(err)
	}
	// The subnet row cannot be judged (engine unknown) so it must
	// NOT be treated as an orphan and must NOT be deleted.
	if len(rep.DBOrphans) != 0 {
		t.Fatalf("engine-down run must not report DB orphans: %+v", rep.DBOrphans)
	}
	if len(rep.Errors) == 0 {
		t.Fatal("engine failure must be reported in the report errors")
	}
	rows, _ := res.ListResources(ctx, labID)
	if len(rows) != 3 {
		t.Fatalf("engine-down enforcement must not delete DB rows, got %d", len(rows))
	}
}

func TestReconcileDestroyedLabLiveResourcesAreOrphans(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	if _, err := svc.Labs.Destroy(ctx, labID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Labs.Transition(ctx, labID, labs.StateDestroyed, ""); err != nil {
		t.Fatal(err)
	}
	// Live resources survived the destroy: engine subnet + VM.
	eng.nodes[labID] = []TopologyNode{{ID: labID, Kind: "network"}, {ID: "subnet-1", Kind: "subnet"}}
	ha.vms[labID] = []string{"vm-1"}

	rep, err := svc.Reconcile(ctx, labID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DBOrphans) != 0 {
		t.Fatalf("destroyed lab's DB rows are not orphans: %+v", rep.DBOrphans)
	}
	// network + subnet + VM are live orphans of a destroyed lab.
	if len(rep.LiveOrphans) != 3 || rep.RemovedLive != 3 {
		t.Fatalf("expected 3 live orphans removed, got %+v", rep)
	}
}

func TestCleanupReconcileJobDefaultsToDryRun(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	labID := seedLabWithResources(t, svc, res, ctx)
	eng.nodes[labID] = []TopologyNode{{ID: labID, Kind: "network"}, {ID: "subnet-9", Kind: "subnet"}}
	ha.vms[labID] = []string{"vm-1"}
	// Payload without dry_run: the job must default to dry-run.
	if _, _, err := svc.Jobs.Enqueue(ctx, store.JobCleanupReconcile, map[string]any{"lab_id": labID}, "rec:"+labID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(eng.nodes[labID]) != 2 {
		t.Fatal("job without dry_run=false must not delete the live orphan")
	}
	counts, _ := svc.Jobs.Counts(ctx)
	if counts["done"] != 1 {
		t.Fatalf("reconcile job should complete, counts=%v", counts)
	}
}

func TestReconcileAllCoversEveryLab(t *testing.T) {
	svc, eng, ha, res, ctx := reconcileService(t)
	lab1 := seedLabWithResources(t, svc, res, ctx)
	lab2, _, err := svc.Labs.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	eng.nodes[lab1] = []TopologyNode{{ID: lab1, Kind: "network"}, {ID: "subnet-1", Kind: "subnet"}}
	eng.nodes[lab2.ID] = []TopologyNode{{ID: lab2.ID, Kind: "network"}}
	ha.vms[lab1] = []string{"vm-1"}

	reports, err := svc.ReconcileAll(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
}
