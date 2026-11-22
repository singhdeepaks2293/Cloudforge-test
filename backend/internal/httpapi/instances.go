// EC2-style instance lifecycle (Phase 2): Start, Reboot and the
// per-lab event history.
//
// Start re-provisions a STOPPED lab through the same provisioning
// path as Create (worker job in Postgres mode, in-process retry in
// memory mode), so the lab only reports READY when provisioning
// actually succeeded — never a faked state change.
//
// Reboot is a real host-agent operation (OpRebootVM) on the lab's
// VM. It only exists when a host agent is configured AND the lab has
// a host-agent VM; otherwise it is an honest 503 with the reason —
// never a faked reboot.
package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func (s *Server) handleStartLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownLab(w, r, u); !ok {
		return
	}
	lab, err := s.Labs.Start(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, labs.ErrInvalidTransition) {
			s.writeError(w, r, http.StatusConflict, "invalid_transition", "only a STOPPED lab can be started: "+err.Error())
			return
		}
		if errors.Is(err, labs.ErrLabNotFound) {
			s.writeError(w, r, http.StatusNotFound, "lab_not_found", "no lab with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "lab_started", "lab", lab.ID, nil)
	// 202: the lab is PROVISIONING, not READY — the worker (or the
	// in-process provisioner) reports READY only on success.
	s.writeJSON(w, r, http.StatusAccepted, lab)
}

func (s *Server) handleRebootLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	if lab.State != labs.StateReady && lab.State != labs.StateRunning {
		s.writeError(w, r, http.StatusConflict, "invalid_transition", "only a READY or RUNNING lab can be rebooted (this lab is "+lab.State+")")
		return
	}
	if s.HostAgent == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "reboot_unavailable",
			"reboot needs a host agent, and none is configured (set HOST_AGENT_ADDR); the lab has no VM to reboot")
		return
	}
	// Find the lab's host-agent VM id from its instance resources.
	vmID := ""
	if s.Resources != nil {
		if resources, err := s.Resources.ListResources(r.Context(), lab.ID); err == nil {
			for _, res := range resources {
				if res.Kind == "instance" && res.ProviderResourceID != "" {
					vmID = res.ProviderResourceID
					break
				}
			}
		}
	}
	if vmID == "" {
		s.writeError(w, r, http.StatusServiceUnavailable, "reboot_unavailable",
			"this lab has no host-agent VM recorded (it was provisioned without a host agent); there is nothing to reboot")
		return
	}
	if _, err := s.HostAgent.Do(r.Context(), hostagent.OpRequest{
		Op: hostagent.OpRebootVM, LabID: lab.ID, OwnerID: u.ID, ID: vmID,
		IdempotencyKey: "reboot:" + lab.ID + ":" + time.Now().UTC().Format("20060102150405"),
	}); err != nil {
		s.writeError(w, r, http.StatusBadGateway, "host_agent_error", "host agent RebootVM failed: "+err.Error())
		return
	}
	s.audit(r, u.ID, "lab_rebooted", "lab", lab.ID, map[string]any{"vm_id": vmID})
	s.writeJSON(w, r, http.StatusOK, lab)
}

// handleLabDefinitions lists the launchable lab definitions (the
// "images" the EC2 launch wizard offers). Public: definitions carry
// no user data.
func (s *Server) handleLabDefinitions(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{"definitions": labs.Definitions})
}

// handleLabEvents returns the lab's event history from the audit log
// (lab transitions, resource creates/deletes, validation, terminal
// and Terraform events whose metadata names the lab). The caller must
// own the lab; admins may read any lab's events.
func (s *Server) handleLabEvents(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, err := s.Labs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil || (lab.OwnerID != u.ID && u.Role != "admin") {
		s.writeError(w, r, http.StatusNotFound, "lab_not_found", "no lab with that id")
		return
	}
	limit, offset := parsePagination(r)
	entries, total, err := s.Audit.ListFiltered(r.Context(), store.AuditFilter{
		LabID: lab.ID, Limit: limit, Offset: offset,
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"events": entries, "total": total, "limit": limit, "offset": offset})
}
