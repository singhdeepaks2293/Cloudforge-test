// Milestone 9, Part 3: cleanup reconciliation.
//
// A reconcile run compares the lab's DB rows against the live
// world — the Rust network engine (topology) and the host agent
// (VMs) — and reports orphans in BOTH directions:
//
//   - DB orphans: lab_resources rows with no live counterpart
//     (engine/host-agent says the resource is gone).
//   - Live orphans: engine subnets/networks or host-agent VMs with
//     no DB row (e.g. a destroy that cleaned the DB but not the
//     backend, or a backend-side create that was never recorded).
//
// Dry-run is the default: detection only, nothing is removed, and
// the report is logged + audited. Enforcement (dry_run=false)
// deletes the orphaned side — DB rows via the resource store,
// engine subnets/networks via the engine client, VMs via the host
// agent — and audits every removal. An unreachable backend is
// reported, never treated as "everything is gone": resources whose
// live side cannot be checked are skipped, not deleted.
//
// The same code runs from the worker (cleanup_reconcile jobs) and
// from the admin endpoint POST /api/v1/admin/cleanup-reconcile.
package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/store"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

// TopologyNode is one live engine node, as seen by the reconciler.
type TopologyNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// EngineTopology is the subset of the network engine the
// reconciler needs. The production implementation is
// networkEngineAdapter (over *networkclient.Client); tests use a
// fake.
type EngineTopology interface {
	InspectTopology(ctx context.Context, labID, ownerID string) ([]TopologyNode, error)
	DeleteSubnet(ctx context.Context, labID, ownerID, subnetID string) error
	DeleteNetwork(ctx context.Context, labID, ownerID string) error
}

// networkEngineAdapter adapts the gRPC client to EngineTopology.
type networkEngineAdapter struct {
	client *networkclient.Client
}

// NewEngineAdapter builds an EngineTopology over client, or nil
// when no engine client is configured.
func NewEngineAdapter(client *networkclient.Client) EngineTopology {
	if client == nil {
		return nil
	}
	return &networkEngineAdapter{client: client}
}

func (a *networkEngineAdapter) meta(ctx context.Context, labID, ownerID, what string) (context.Context, *gen.RequestMeta) {
	rid := ids.New()
	return networkclient.ContextWithRequestID(ctx, rid),
		networkclient.Meta(labID, ownerID, rid, "reconcile:"+what+":"+labID)
}

func (a *networkEngineAdapter) InspectTopology(ctx context.Context, labID, ownerID string) ([]TopologyNode, error) {
	ctx, meta := a.meta(ctx, labID, ownerID, "inspect")
	resp, err := a.client.InspectTopology(ctx, &gen.InspectTopologyRequest{Meta: meta, NetworkId: labID})
	if err != nil {
		return nil, err
	}
	out := make([]TopologyNode, 0, len(resp.GetNodes()))
	for _, n := range resp.GetNodes() {
		out = append(out, TopologyNode{ID: n.GetId(), Kind: n.GetKind()})
	}
	return out, nil
}

func (a *networkEngineAdapter) DeleteSubnet(ctx context.Context, labID, ownerID, subnetID string) error {
	ctx, meta := a.meta(ctx, labID, ownerID, "delete-subnet")
	_, err := a.client.DeleteSubnet(ctx, &gen.DeleteSubnetRequest{Meta: meta, SubnetId: subnetID})
	return err
}

func (a *networkEngineAdapter) DeleteNetwork(ctx context.Context, labID, ownerID string) error {
	ctx, meta := a.meta(ctx, labID, ownerID, "delete-network")
	_, err := a.client.DeleteNetwork(ctx, &gen.DeleteNetworkRequest{Meta: meta, NetworkId: labID})
	return err
}

// Reconciler runs cleanup reconciliation. *Service implements it.
type Reconciler interface {
	Reconcile(ctx context.Context, labID string, dryRun bool) (*ReconcileReport, error)
	ReconcileAll(ctx context.Context, dryRun bool) ([]*ReconcileReport, error)
}

// Orphan is one resource that exists on one side only.
type Orphan struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Side   string `json:"side"` // "db" | "engine" | "host-agent"
	Detail string `json:"detail"`
}

// ReconcileReport is the outcome of one lab's reconcile run.
type ReconcileReport struct {
	LabID       string   `json:"lab_id"`
	LabState    string   `json:"lab_state"`
	DryRun      bool     `json:"dry_run"`
	DBOrphans   []Orphan `json:"db_orphans"`
	LiveOrphans []Orphan `json:"live_orphans"`
	RemovedDB   int      `json:"removed_db"`
	RemovedLive int      `json:"removed_live"`
	Errors      []string `json:"errors,omitempty"`
}

// Reconcile reconciles one lab. dryRun=true detects only.
func (s *Service) Reconcile(ctx context.Context, labID string, dryRun bool) (*ReconcileReport, error) {
	rep := &ReconcileReport{LabID: labID, DryRun: dryRun, DBOrphans: []Orphan{}, LiveOrphans: []Orphan{}}
	lab, err := s.Labs.Get(ctx, labID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: lab %s: %w", labID, err)
	}
	rep.LabState = lab.State
	if s.Resources == nil {
		return nil, errors.New("reconcile: resource store is not configured")
	}
	resources, err := s.Resources.ListResources(ctx, labID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: list resources: %w", err)
	}

	// Live state, best-effort per backend. An unreachable backend
	// means "unknown", never "everything is gone".
	var engineNodes []TopologyNode
	engineKnown := false
	if s.Engine != nil {
		if nodes, err := s.Engine.InspectTopology(ctx, labID, lab.OwnerID); err != nil {
			rep.Errors = append(rep.Errors, "engine InspectTopology failed: "+err.Error())
		} else {
			engineNodes = nodes
			engineKnown = true
		}
	} else {
		rep.Errors = append(rep.Errors, "network engine is not configured")
	}
	var vmIDs map[string]bool
	hostKnown := false
	if s.HostAgent != nil {
		if vms, err := s.listHostAgentVMs(ctx, labID, lab.OwnerID); err != nil {
			rep.Errors = append(rep.Errors, "host agent ListVMs failed: "+err.Error())
		} else {
			vmIDs = map[string]bool{}
			for _, id := range vms {
				vmIDs[id] = true
			}
			hostKnown = true
		}
	} else {
		rep.Errors = append(rep.Errors, "host agent is not configured")
	}

	engineIDs := map[string]bool{}
	networkPresent := false
	for _, n := range engineNodes {
		engineIDs[n.ID] = true
		if n.Kind == "network" && n.ID == labID {
			networkPresent = true
		}
	}
	destroyed := lab.State == labs.StateDestroyed || lab.State == labs.StateDestroying

	// --- DB orphans -----------------------------------------------------
	// A destroyed lab's DB rows are the destroy job's business, not
	// orphans: skip them so reconcile never deletes rows a destroy
	// is still processing.
	if !destroyed {
		for _, res := range resources {
			switch res.Kind {
			case "subnet":
				if !engineKnown {
					continue
				}
				if !engineIDs[res.ID] && !engineIDs[res.ProviderResourceID] {
					rep.DBOrphans = append(rep.DBOrphans, Orphan{
						ID: res.ID, Kind: res.Kind, Side: "db",
						Detail: "subnet row has no engine subnet (engine topology has no such id)",
					})
				}
			case "vpc":
				if !engineKnown {
					continue
				}
				if !networkPresent {
					rep.DBOrphans = append(rep.DBOrphans, Orphan{
						ID: res.ID, Kind: res.Kind, Side: "db",
						Detail: "VPC row has no engine network for this lab",
					})
				}
			case "instance":
				if !hostKnown {
					continue
				}
				if res.ProviderResourceID == "" || !vmIDs[res.ProviderResourceID] {
					rep.DBOrphans = append(rep.DBOrphans, Orphan{
						ID: res.ID, Kind: res.Kind, Side: "db",
						Detail: "instance row has no host-agent VM (provider id " + res.ProviderResourceID + ")",
					})
				}
			default:
				// gateway, loadbalancer, route-table,
				// security-group are DB-only kinds: no live
				// counterpart exists by design, so they can
				// never be orphans.
			}
		}
	}

	// --- Live orphans ---------------------------------------------------
	dbIDs := map[string]bool{}
	for _, res := range resources {
		dbIDs[res.ID] = true
		if res.ProviderResourceID != "" {
			dbIDs[res.ProviderResourceID] = true
		}
	}
	if engineKnown {
		for _, n := range engineNodes {
			switch n.Kind {
			case "network":
				if n.ID == labID {
					if destroyed {
						rep.LiveOrphans = append(rep.LiveOrphans, Orphan{
							ID: n.ID, Kind: n.Kind, Side: "engine",
							Detail: "engine network remains for a destroyed lab",
						})
					}
				} else if !dbIDs[n.ID] {
					rep.LiveOrphans = append(rep.LiveOrphans, Orphan{
						ID: n.ID, Kind: n.Kind, Side: "engine",
						Detail: "engine network has no lab row",
					})
				}
			case "subnet":
				if destroyed || !dbIDs[n.ID] {
					rep.LiveOrphans = append(rep.LiveOrphans, Orphan{
						ID: n.ID, Kind: n.Kind, Side: "engine",
						Detail: "engine subnet has no DB resource row",
					})
				}
			default:
				// namespace/bridge/address nodes are engine
				// internals of a network, not lab resources;
				// they go away with their network.
			}
		}
	}
	if hostKnown {
		for id := range vmIDs {
			if destroyed || !dbIDs[id] {
				rep.LiveOrphans = append(rep.LiveOrphans, Orphan{
					ID: id, Kind: "instance", Side: "host-agent",
					Detail: "host-agent VM has no DB instance row",
				})
			}
		}
	}

	// --- Enforcement ----------------------------------------------------
	if !dryRun {
		for _, o := range rep.DBOrphans {
			if _, err := s.Resources.DeleteResource(ctx, labID, o.ID); err != nil {
				rep.Errors = append(rep.Errors, "delete DB resource "+o.ID+": "+err.Error())
				continue
			}
			rep.RemovedDB++
			s.auditReconcile(ctx, labID, "cleanup_reconcile_removed_db", o)
		}
		for _, o := range rep.LiveOrphans {
			var err error
			switch o.Side {
			case "engine":
				if o.Kind == "network" {
					err = s.Engine.DeleteNetwork(ctx, labID, lab.OwnerID)
				} else {
					err = s.Engine.DeleteSubnet(ctx, labID, lab.OwnerID, o.ID)
				}
			case "host-agent":
				_, err = s.HostAgent.Do(ctx, hostagent.OpRequest{
					Op: hostagent.OpDeleteVM, LabID: labID, OwnerID: lab.OwnerID, ID: o.ID,
				})
			}
			if err != nil {
				rep.Errors = append(rep.Errors, "remove live "+o.Side+" resource "+o.ID+": "+err.Error())
				continue
			}
			rep.RemovedLive++
			s.auditReconcile(ctx, labID, "cleanup_reconcile_removed_live", o)
		}
	}

	s.audit(ctx, labID, "cleanup_reconcile", map[string]any{
		"dry_run": dryRun, "db_orphans": len(rep.DBOrphans),
		"live_orphans": len(rep.LiveOrphans), "removed_db": rep.RemovedDB,
		"removed_live": rep.RemovedLive, "errors": len(rep.Errors),
	})
	s.log().Info("cleanup reconcile finished",
		"lab_id", labID, "dry_run", dryRun,
		"db_orphans", len(rep.DBOrphans), "live_orphans", len(rep.LiveOrphans),
		"removed_db", rep.RemovedDB, "removed_live", rep.RemovedLive,
		"errors", len(rep.Errors))
	return rep, nil
}

// ReconcileAll reconciles every lab, oldest first. One lab's failure
// does not stop the others; failures are reported per lab.
func (s *Service) ReconcileAll(ctx context.Context, dryRun bool) ([]*ReconcileReport, error) {
	all, err := s.Labs.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconcile all: list labs: %w", err)
	}
	reports := make([]*ReconcileReport, 0, len(all))
	for _, lab := range all {
		rep, err := s.Reconcile(ctx, lab.ID, dryRun)
		if err != nil {
			reports = append(reports, &ReconcileReport{
				LabID: lab.ID, LabState: lab.State, DryRun: dryRun,
				DBOrphans: []Orphan{}, LiveOrphans: []Orphan{},
				Errors: []string{err.Error()},
			})
			continue
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

func (s *Service) listHostAgentVMs(ctx context.Context, labID, ownerID string) ([]string, error) {
	res, err := s.HostAgent.Do(ctx, hostagent.OpRequest{
		Op: hostagent.OpListVMs, LabID: labID, OwnerID: ownerID,
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, item := range res.Items {
		if id, _ := item["id"].(string); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Service) audit(ctx context.Context, labID, action string, meta map[string]any) {
	if s.Audit == nil {
		return
	}
	if err := s.Audit.Record(ctx, store.AuditEntry{
		ActorID: "system:cleanup-reconcile", Action: action,
		ResourceType: "lab", ResourceID: labID, Metadata: meta,
	}); err != nil {
		s.log().Warn("reconcile audit record failed", "lab_id", labID, "action", action, "error", err)
	}
}

func (s *Service) auditReconcile(ctx context.Context, labID, action string, o Orphan) {
	s.audit(ctx, labID, action, map[string]any{
		"resource_id": o.ID, "kind": o.Kind, "side": o.Side, "detail": o.Detail,
	})
}

// cleanupReconcile handles a cleanup_reconcile job. Payload:
// {lab_id? (empty = all labs), dry_run? (absent = true —
// dry-run is the default; enforcement needs an explicit false)}.
func (s *Service) cleanupReconcile(ctx context.Context, j *store.Job) error {
	labID, _ := j.Payload["lab_id"].(string)
	dryRun := true
	if v, ok := j.Payload["dry_run"]; ok {
		if b, ok := v.(bool); ok {
			dryRun = b
		}
	}
	if labID == "" {
		reports, err := s.ReconcileAll(ctx, dryRun)
		if err != nil {
			return err
		}
		removedDB, removedLive := 0, 0
		for _, r := range reports {
			removedDB += r.RemovedDB
			removedLive += r.RemovedLive
		}
		s.log().Info("cleanup reconcile (all labs) finished",
			"labs", len(reports), "dry_run", dryRun,
			"removed_db", removedDB, "removed_live", removedLive)
		return nil
	}
	if _, err := s.Reconcile(ctx, labID, dryRun); err != nil {
		return err
	}
	return nil
}
