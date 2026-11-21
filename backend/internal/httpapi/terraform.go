// Milestone 7 Terraform handlers.
//
// Every mutating action is an async job: the API validates the
// request, creates a terraform_runs row (QUEUED), enqueues a
// terraform_<action> job for the worker and answers 202 with the run
// id. Nothing runs on the API process and nothing runs on the host.
// Apply requires an approved, unexpired plan: without one it is a
// 409. When Docker is absent the ContainerRunner reports
// Unavailable and the API answers 503 before enqueueing anything.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
)

var terraformActions = map[string]struct {
	jobType string
	op      terraform.Operation
}{
	"init":     {store.JobTerraformInit, terraform.OpInit},
	"validate": {store.JobTerraformValidate, terraform.OpValidate},
	"plan":     {store.JobTerraformPlan, terraform.OpPlan},
	"apply":    {store.JobTerraformApply, terraform.OpApply},
	"destroy":  {store.JobTerraformDestroy, terraform.OpDestroy},
}

type terraformRequest struct {
	Configuration string `json:"configuration"`
	PlanID        string `json:"plan_id"`
}

func (s *Server) terraformRunView(r *store.TerraformRun) map[string]any {
	return map[string]any{
		"id": r.ID, "run_id": r.ID, "lab_id": r.LabID, "user_id": r.UserID,
		"action": r.Action, "state": r.State, "plan_id": r.PlanID,
		"plan_json": r.PlanJSON, "plan_output": r.PlanOutput,
		"apply_output": r.ApplyOutput, "approved_at": r.ApprovedAt,
		"approved_by": r.ApprovedBy, "plan_expires_at": r.PlanExpiresAt,
		"job_id": r.JobID, "error": r.Error,
		"created_at": r.CreatedAt, "updated_at": r.UpdatedAt,
	}
}

// checkTerraformAvailable validates the provider config and the
// runner before any run is created. A 503 here means "Docker (or
// the emulator endpoint) is not available", never a faked run.
func (s *Server) checkTerraformAvailable(w http.ResponseWriter, r *http.Request) bool {
	if err := terraform.ValidateProviderConfig(s.EmulatorEndpoint, s.MinIOEndpoint); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_provider_config", err.Error())
		return false
	}
	if ok, reason := s.TerraformRunner.Available(r.Context()); !ok {
		s.writeJSON(w, r, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{
				"code": "terraform_unavailable", "message": reason,
				"request_id": RequestID(r.Context()),
			},
			"capability": map[string]any{
				"service": "terraform", "operation": "run",
				"detail": reason, "image": terraform.DefaultImage,
			},
		})
		return false
	}
	return true
}

func (s *Server) enqueueTerraform(w http.ResponseWriter, r *http.Request, u *auth.User, lab *labs.Lab, action string, req terraformRequest) {
	meta, ok := terraformActions[action]
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "unknown terraform action "+action)
		return
	}
	if req.Configuration != "" {
		if err := terraform.ValidateConfiguration(req.Configuration); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if !s.checkTerraformAvailable(w, r) {
		return
	}
	run, err := s.TerraformRuns.CreateRun(r.Context(), &store.TerraformRun{
		UserID: u.ID, LabID: lab.ID, Action: action, State: "QUEUED",
		Configuration: req.Configuration,
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	payload := map[string]any{"lab_id": lab.ID, "run_id": run.ID, "action": action, "user_id": u.ID}
	if req.Configuration != "" {
		payload["configuration"] = req.Configuration
	}
	if req.PlanID != "" {
		payload["plan_id"] = req.PlanID
	}
	job, _, err := s.Jobs.Enqueue(r.Context(), meta.jobType, payload, "terraform:"+action+":"+run.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "could not enqueue terraform job: "+err.Error())
		return
	}
	run.JobID = job.ID
	if _, err := s.TerraformRuns.UpdateRun(r.Context(), run); err != nil {
		s.Logger.Warn("could not link terraform job to run", "run_id", run.ID, "error", err)
	}
	s.audit(r, u.ID, "terraform_"+action+"_requested", "terraform_run", run.ID,
		map[string]any{"lab_id": lab.ID, "job_id": job.ID})
	s.writeJSON(w, r, http.StatusAccepted, map[string]any{
		"run_id": run.ID, "job_id": job.ID, "action": action, "state": "QUEUED",
		"run": s.terraformRunView(run),
	})
}

func (s *Server) decodeTerraformRequest(w http.ResponseWriter, r *http.Request) (terraformRequest, bool) {
	var req terraformRequest
	if r.ContentLength == 0 {
		return req, true
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return req, false
	}
	return req, true
}

func (s *Server) handleTerraformInit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	s.enqueueTerraform(w, r, u, lab, "init", req)
}

func (s *Server) handleTerraformValidate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	s.enqueueTerraform(w, r, u, lab, "validate", req)
}

func (s *Server) handleTerraformPlan(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	s.enqueueTerraform(w, r, u, lab, "plan", req)
}

func (s *Server) handleTerraformDestroy(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	s.enqueueTerraform(w, r, u, lab, "destroy", req)
}

// handleTerraformApprove records an explicit approval for a
// succeeded plan. The plan must exist, belong to this lab, be
// SUCCEEDED and unexpired; approving anything else is a 409.
func (s *Server) handleTerraformApprove(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "plan_id is required")
		return
	}
	planRun, err := s.TerraformRuns.GetRunByPlanID(r.Context(), req.PlanID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan with that plan_id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if planRun.LabID != lab.ID {
		s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan with that plan_id")
		return
	}
	if planRun.Action != "plan" || planRun.State != "SUCCEEDED" {
		s.writeError(w, r, http.StatusConflict, "plan_not_approved",
			fmt.Sprintf("plan %s is %s (action %s); only a succeeded plan can be approved", req.PlanID, planRun.State, planRun.Action))
		return
	}
	if planRun.PlanExpiresAt == nil || time.Now().UTC().After(*planRun.PlanExpiresAt) {
		s.writeError(w, r, http.StatusConflict, "plan_expired", "the plan has expired; run plan again")
		return
	}
	now := time.Now().UTC()
	planRun.ApprovedAt = &now
	planRun.ApprovedBy = u.ID
	if _, err := s.TerraformRuns.UpdateRun(r.Context(), planRun); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "terraform_plan_approved", "terraform_run", planRun.ID, map[string]any{"plan_id": req.PlanID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"plan_id": req.PlanID, "approved": true,
		"approved_at": now, "expires_at": planRun.PlanExpiresAt,
	})
}

// handleTerraformApply requires an approved, unexpired plan:
// missing approval is 409, an expired plan is 409, an unknown plan
// is 404. The plan_id in the body is the only way to apply.
func (s *Server) handleTerraformApply(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	req, ok := s.decodeTerraformRequest(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "plan_id is required: apply needs an approved plan")
		return
	}
	planRun, err := s.TerraformRuns.GetRunByPlanID(r.Context(), req.PlanID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan with that plan_id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if planRun.LabID != lab.ID {
		s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan with that plan_id")
		return
	}
	if planRun.Action != "plan" || planRun.State != "SUCCEEDED" {
		s.writeError(w, r, http.StatusConflict, "plan_not_ready",
			fmt.Sprintf("plan %s is %s; only a succeeded plan can be applied", req.PlanID, planRun.State))
		return
	}
	if planRun.ApprovedAt == nil {
		s.writeError(w, r, http.StatusConflict, "plan_not_approved",
			"plan "+req.PlanID+" has not been approved; POST /terraform/approve first")
		return
	}
	if planRun.PlanExpiresAt == nil || time.Now().UTC().After(*planRun.PlanExpiresAt) {
		s.writeError(w, r, http.StatusConflict, "plan_expired", "the approved plan has expired; run plan again")
		return
	}
	s.enqueueTerraform(w, r, u, lab, "apply", req)
}

func (s *Server) handleTerraformRuns(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	limit, _ := parsePagination(r)
	runs, err := s.TerraformRuns.ListRunsByLab(r.Context(), lab.ID, limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		views = append(views, s.terraformRunView(run))
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"runs": views})
}
