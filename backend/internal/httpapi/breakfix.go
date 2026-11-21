// Milestone 8 break-and-fix handlers.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/validation"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

type breakFixStartRequest struct {
	ScenarioID string `json:"scenario_id"`
}

// handleBreakFixStart attaches a scenario to an existing lab and
// injects its fault through the Rust engine. When the engine is
// unreachable (or not configured) the lab is created WITHOUT the
// fault and the attempt is marked fault_pending with an honest
// status — never claimed broken.
func (s *Server) handleBreakFixStart(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	var req breakFixStartRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.ScenarioID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "scenario_id is required")
		return
	}
	sc, err := s.Curriculum.GetScenario(r.Context(), req.ScenarioID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "scenario_not_found", "no break-and-fix scenario with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	faultStatus := "fault_pending"
	engineDetail := "network engine client is not configured"
	if s.Network != nil {
		kind, kindErr := sc.Fault.EngineKind()
		if kindErr == nil {
			ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
			_, err := s.Network.InjectFault(ctx, &gen.InjectFaultRequest{
				Meta: networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "break-fix:"+sc.ID+":"+lab.ID),
				Fault: &gen.FaultSpec{
					FaultId:         "break-fix-" + sc.ID + "-" + lab.ID,
					Kind:            gen.FaultKind(kind),
					Magnitude:       sc.Fault.EngineMagnitude(),
					DurationSeconds: 3600,
					Target:          sc.Fault.Target,
				},
			})
			if err == nil {
				faultStatus = "injected"
				engineDetail = "fault injected via the network engine"
			} else {
				engineDetail = "network engine InjectFault failed: " + err.Error()
			}
		} else {
			engineDetail = kindErr.Error()
		}
	}
	attempt, err := s.Curriculum.StartBreakFix(r.Context(), u.ID, lab.ID, sc.ID, faultStatus)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "break_fix_started", "lab", lab.ID, map[string]any{
		"scenario_id": sc.ID, "fault_status": attempt.FaultStatus,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"attempt": attempt,
		"scenario": map[string]any{
			"id": sc.ID, "title": sc.Title, "symptom": sc.Symptom,
			"diagnosis_hints": sc.Hints, "fix_steps": sc.FixSteps,
			"validation_rules": sc.ValidationRules, "xp_reward": sc.XPReward,
		},
		"fault_status":  attempt.FaultStatus,
		"engine_detail": engineDetail,
	})
}

// handleBreakFixSolve runs the scenario's validation rules against
// real lab state and, ONLY when every rule passes, marks the
// scenario solved and awards its XP (once) plus any achievements
// the solve unlocks. A failing or unknown check returns solved:false
// with the checks, so the learner can see what is still broken.
func (s *Server) handleBreakFixSolve(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	attempt, err := s.Curriculum.GetBreakFixAttemptByLab(r.Context(), lab.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "break_fix_not_started", "start a break-and-fix scenario for this lab first")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	sc, err := s.Curriculum.GetScenario(r.Context(), attempt.ScenarioID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	rules := make([]validation.Rule, 0, len(sc.ValidationRules))
	for _, cr := range sc.ValidationRules {
		rules = append(rules, validation.Rule{Rule: cr.Rule, Params: cr.Params, Description: cr.Description})
	}
	result := s.runLabValidation(r, lab, u, rules)
	if !result.Passed {
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"lab_id": lab.ID, "scenario_id": attempt.ScenarioID, "solved": false,
			"passed": false, "xp_awarded": 0, "checks": result.Checks,
			"fault_status": attempt.FaultStatus,
		})
		return
	}
	solved, xpAwarded, err := s.Curriculum.MarkBreakFixSolved(r.Context(), u.ID, lab.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	var newly []*curriculum.Achievement
	if xpAwarded > 0 {
		newly = s.evaluateAchievements(r, u.ID)
	}
	if newly == nil {
		newly = []*curriculum.Achievement{}
	}
	s.audit(r, u.ID, "break_fix_solved", "lab", lab.ID, map[string]any{
		"scenario_id": attempt.ScenarioID, "xp_awarded": xpAwarded,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"lab_id": lab.ID, "scenario_id": attempt.ScenarioID, "solved": solved.Solved,
		"passed": true, "xp_awarded": xpAwarded, "checks": result.Checks,
		"fault_status": attempt.FaultStatus, "achievements_awarded": newly,
	})
}
