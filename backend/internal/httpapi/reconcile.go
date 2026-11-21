// Milestone 9, Part 3: admin cleanup reconciliation endpoint.
//
// POST /api/v1/admin/cleanup-reconcile {lab_id?, dry_run?}
//   - Admin-only. lab_id empty means "all labs".
//   - dry_run defaults to true when the field is absent: detection
//     only, nothing is removed. Send dry_run=false explicitly to
//     enforce (delete the orphaned side). This mirrors the worker
//     job type cleanup_reconcile, which has the same default.
//   - Returns the reconcile report(s). Per-backend failures (engine
//     or host agent unreachable) are reported in each report's
//     errors list, not as a 500 — the run still reconciles whatever
//     it can check.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/jobs"
)

type cleanupReconcileRequest struct {
	LabID  string `json:"lab_id,omitempty"`
	DryRun *bool  `json:"dry_run,omitempty"`
}

type cleanupReconcileResponse struct {
	DryRun  bool                    `json:"dry_run"`
	Reports []*jobs.ReconcileReport `json:"reports"`
}

func (s *Server) reconciler() jobs.Reconciler {
	if s.Reconciler != nil {
		return s.Reconciler
	}
	// Build one from this server's own wiring: same stores, same
	// engine client, same host-agent client as the rest of the API.
	var ha hostagent.Provisioner
	if s.HostAgent != nil {
		ha = s.HostAgent
	}
	return &jobs.Service{
		Labs:      s.Labs,
		Resources: s.Resources,
		Audit:     s.Audit,
		HostAgent: ha,
		Engine:    jobs.NewEngineAdapter(s.Network),
		Logger:    s.Logger,
	}
}

func (s *Server) handleCleanupReconcile(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req cleanupReconcileRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	rec := s.reconciler()
	if req.LabID == "" {
		reports, err := rec.ReconcileAll(r.Context(), dryRun)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "reconcile_failed", err.Error())
			return
		}
		s.audit(r, admin.ID, "cleanup_reconcile", "lab", "", map[string]any{
			"dry_run": dryRun, "labs": len(reports),
		})
		s.writeJSON(w, r, http.StatusOK, cleanupReconcileResponse{DryRun: dryRun, Reports: reports})
		return
	}
	rep, err := rec.Reconcile(r.Context(), req.LabID, dryRun)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "reconcile_failed", err.Error())
		return
	}
	s.audit(r, admin.ID, "cleanup_reconcile", "lab", req.LabID, map[string]any{
		"dry_run":    dryRun,
		"db_orphans": len(rep.DBOrphans), "live_orphans": len(rep.LiveOrphans),
	})
	s.writeJSON(w, r, http.StatusOK, cleanupReconcileResponse{DryRun: dryRun, Reports: []*jobs.ReconcileReport{rep}})
}
