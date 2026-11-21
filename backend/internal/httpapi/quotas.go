// Milestone 9, Part 3: admin-configurable per-role quotas.
//
// GET  /api/v1/admin/quotas — list the configured quota set per role.
// PUT  /api/v1/admin/quotas — upsert one role's limits.
// Both are admin-only. Enforcement lives in handleCreateLab (lab
// count + lab duration) and handleCreateResource (VM profile size),
// all through the quota package — the store's own ceiling
// (labs.Store maxLabs / PostgresLabStore) is only a backstop and is
// wired at startup to the highest configured role limit.
package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// roleLimits returns the configured limits for a role, falling back
// to the built-in defaults when no row is configured or the quota
// store is unavailable. In lightweight mode (s.Lite) the result is
// capped at quota.LiteLimits, so a database seeded with full-mode
// defaults still enforces lite limits — the cap, not the stored
// row, is what a lite classroom gets.
func (s *Server) roleLimits(ctx context.Context, role string) quota.Limits {
	var limits quota.Limits
	if s.Quotas != nil {
		if q, err := s.Quotas.GetQuota(ctx, role); err == nil && q != nil {
			limits = quota.Limits{
				MaxLabs: q.MaxLabs, MaxCPU: q.MaxCPU, MaxMemoryMB: q.MaxMemoryMB,
				MaxDiskGB: q.MaxDiskGB, MaxLabHours: q.MaxLabHours,
				MaxLabMinutes: q.MaxLabMinutes,
			}
		} else {
			limits = quota.DefaultLimits(role)
		}
	} else {
		limits = quota.DefaultLimits(role)
	}
	if s.Lite {
		limits = limits.CapAt(quota.LiteLimits(role))
	}
	return limits
}

// checkLabQuotas enforces the lab-count and lab-duration limits for
// the caller's role. It runs before Labs.Create so the error names
// the configured limit.
func (s *Server) checkLabQuotas(ctx context.Context, u *auth.User, definitionID string) error {
	limits := s.roleLimits(ctx, u.Role)
	if active, err := s.Labs.ActiveCount(ctx, u.ID); err == nil {
		if err := limits.CheckActiveLabs(active); err != nil {
			return err
		}
	}
	if def, ok := labs.GetDefinition(definitionID); ok {
		if err := limits.CheckLabDuration(def.DurationMin); err != nil {
			return err
		}
	}
	return nil
}

// checkInstanceProfile enforces the per-VM size limits for the
// caller's role before a VM is provisioned.
func (s *Server) checkInstanceProfile(ctx context.Context, u *auth.User, profileName string) error {
	prof, err := hostagent.ValidateProfile(profileName)
	if err != nil {
		return err
	}
	return s.roleLimits(ctx, u.Role).CheckProfile(prof.VCPU, prof.MemoryMB, prof.DiskGB)
}

func (s *Server) handleGetQuotas(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	sets, err := s.Quotas.GetQuotas(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if sets == nil {
		sets = []*store.QuotaSet{}
	}
	s.audit(r, admin.ID, "quotas_listed", "quota", "", nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"quotas": sets})
}

type updateQuotaRequest struct {
	Role          string `json:"role"`
	MaxLabs       *int   `json:"max_labs"`
	MaxCPU        *int   `json:"max_cpu"`
	MaxMemoryMB   *int   `json:"max_memory_mb"`
	MaxDiskGB     *int   `json:"max_disk_gb"`
	MaxLabHours   *int   `json:"max_lab_hours"`
	MaxLabMinutes *int   `json:"max_lab_minutes"`
}

// quotaBounds are the sanity caps for a single value: an admin can
// raise a limit, but not to a value that would let one role exhaust
// the host. max_labs 0 means "built-in default (3)".
var quotaBounds = map[string]int{
	"max_labs": 100, "max_cpu": 64, "max_memory_mb": 262144,
	"max_disk_gb": 2000, "max_lab_hours": 168, "max_lab_minutes": 10080,
}

func (s *Server) handleUpdateQuotas(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req updateQuotaRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	validRole := false
	for _, role := range quota.ValidRoles {
		if req.Role == role {
			validRole = true
		}
	}
	if !validRole {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "role must be one of learner|instructor|admin")
		return
	}
	values := map[string]*int{
		"max_labs": req.MaxLabs, "max_cpu": req.MaxCPU, "max_memory_mb": req.MaxMemoryMB,
		"max_disk_gb": req.MaxDiskGB, "max_lab_hours": req.MaxLabHours,
		"max_lab_minutes": req.MaxLabMinutes,
	}
	for name, v := range values {
		if v == nil {
			continue
		}
		if *v < 0 || *v > quotaBounds[name] {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request",
				"quota "+name+" must be between 0 and "+strconv.Itoa(quotaBounds[name]))
			return
		}
	}
	// Start from the current (or default) row so a partial update
	// only changes the fields the admin sent.
	current := s.roleLimits(r.Context(), req.Role)
	set := &store.QuotaSet{
		Role: req.Role, MaxLabs: current.MaxLabs, MaxCPU: current.MaxCPU,
		MaxMemoryMB: current.MaxMemoryMB, MaxDiskGB: current.MaxDiskGB,
		MaxLabHours: current.MaxLabHours, MaxLabMinutes: current.MaxLabMinutes,
	}
	if req.MaxLabs != nil {
		set.MaxLabs = *req.MaxLabs
	}
	if req.MaxCPU != nil {
		set.MaxCPU = *req.MaxCPU
	}
	if req.MaxMemoryMB != nil {
		set.MaxMemoryMB = *req.MaxMemoryMB
	}
	if req.MaxDiskGB != nil {
		set.MaxDiskGB = *req.MaxDiskGB
	}
	if req.MaxLabHours != nil {
		set.MaxLabHours = *req.MaxLabHours
	}
	if req.MaxLabMinutes != nil {
		set.MaxLabMinutes = *req.MaxLabMinutes
	}
	if err := s.Quotas.UpsertQuota(r.Context(), set); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// The store-level ceiling is a backstop for the highest role
	// limit; raising a role above it must raise the ceiling too, or
	// the store would veto legitimate labs for that role.
	if all, err := s.Quotas.GetQuotas(r.Context()); err == nil {
		max := 0
		for _, q := range all {
			if l := (quota.Limits{MaxLabs: q.MaxLabs}).MaxLabsOrDefault(); l > max {
				max = l
			}
		}
		if max > 0 {
			s.Labs.SetMaxLabs(max)
		}
	}
	s.audit(r, admin.ID, "quota_updated", "quota", req.Role, map[string]any{
		"role": req.Role, "max_labs": set.MaxLabs, "max_cpu": set.MaxCPU,
		"max_memory_mb": set.MaxMemoryMB, "max_disk_gb": set.MaxDiskGB,
		"max_lab_hours": set.MaxLabHours, "max_lab_minutes": set.MaxLabMinutes,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"quota": set})
}
