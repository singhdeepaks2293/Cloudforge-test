// Milestone 6 live-topology and validation handlers.
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/validation"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

// handleLabTopology returns the live topology: Rust engine
// InspectTopology nodes merged with the lab's DB resources (as
// nodes, labelled with their resource type). When the engine is
// unreachable the answer is an honest 503 with capability info —
// the frontend treats 404/501 as "pending endpoint" and 503 as a
// real failure to display, which is the correct behaviour here.
func (s *Server) handleLabTopology(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	if s.Network == nil {
		s.writeJSON(w, r, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{
				"code": "engine_unavailable", "message": "network engine client is not configured",
				"request_id": RequestID(r.Context()),
			},
			"capability": map[string]any{
				"service": "network-engine", "operation": "InspectTopology",
				"detail": "no network engine client is configured on this server",
			},
		})
		return
	}
	ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
	resp, err := s.Network.InspectTopology(ctx, &gen.InspectTopologyRequest{
		Meta:      networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "topology:"+RequestID(r.Context())),
		NetworkId: lab.ID,
	})
	if err != nil {
		detail := "network engine InspectTopology failed: " + err.Error()
		if ee, ok := networkclient.AsEngineError(err); ok && ee.EngineCode != "" {
			detail += " (engine code " + ee.EngineCode + ")"
		}
		s.writeJSON(w, r, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{
				"code": "engine_unavailable", "message": detail,
				"request_id": RequestID(r.Context()),
			},
			"capability": map[string]any{
				"service": "network-engine", "operation": "InspectTopology",
				"detail": detail, "engine_address": s.Network.Addr,
			},
		})
		return
	}
	nodes := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, n := range resp.GetNodes() {
		nodes = append(nodes, map[string]any{
			"id": n.GetId(), "kind": n.GetKind(), "name": n.GetName(), "labels": n.GetLabels(),
		})
		seen[n.GetId()] = true
	}
	// Merge DB resources as nodes (deduped by id).
	if list, err := s.Resources.ListResources(r.Context(), lab.ID); err == nil {
		for _, res := range list {
			if seen[res.ID] {
				continue
			}
			labels := map[string]string{"resource_type": res.Kind, "state": res.State}
			if res.CIDR != "" {
				labels["cidr"] = res.CIDR
			}
			name := res.Name
			if name == "" {
				name = res.Kind + "-" + shortSuffix(res.ID)
			}
			nodes = append(nodes, map[string]any{
				"id": res.ID, "kind": res.Kind, "name": name, "labels": labels,
			})
			seen[res.ID] = true
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"nodes": nodes})
}

// --- validation -----------------------------------------------------------

type validateRequest struct {
	Rules []validation.Rule `json:"rules"`
}

// runLabValidation builds the validator state for a lab (DB
// resources, engine topology best-effort, host-agent VMs
// best-effort) and evaluates rules. It is shared by POST /validate
// and the break-and-fix solve endpoint so both judge the same way.
func (s *Server) runLabValidation(r *http.Request, lab *labs.Lab, u *auth.User, rules []validation.Rule) validation.Result {
	resources, _ := s.Resources.ListResources(r.Context(), lab.ID)
	st := validation.State{
		LabExists: true,
		Resources: resources,
	}
	// Engine topology (best effort): its presence/absence decides
	// whether missing-state rules are fail or unknown.
	if s.Network != nil {
		ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
		if resp, err := s.Network.InspectTopology(ctx, &gen.InspectTopologyRequest{
			Meta:      networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "topology:"+RequestID(r.Context())),
			NetworkId: lab.ID,
		}); err == nil {
			st.EngineAvailable = true
			for _, n := range resp.GetNodes() {
				st.Topology = append(st.Topology, validation.TopologyNode{
					ID: n.GetId(), Kind: n.GetKind(), Name: n.GetName(), Labels: n.GetLabels(),
				})
			}
		}
	}
	// Host-agent VM state (best effort).
	if s.HostAgent != nil {
		if vms, err := s.listHostAgentVMs(r.Context(), lab.ID, u.ID); err == nil {
			st.HostAgentChecked = true
			for _, vm := range vms {
				st.VMs = append(st.VMs, validation.VMState{ID: vm.ID, Name: vm.Name, State: vm.State})
			}
		}
	}
	return validation.Validate(st, rules)
}

// defaultLabRules returns the rules POST /validate uses when the
// request carries none: the lab definition's seeded rules when the
// lab was created from a curriculum definition, else the M6
// hardcoded defaults.
func (s *Server) defaultLabRules(r *http.Request, lab *labs.Lab) []validation.Rule {
	if s.Curriculum != nil {
		if def, err := s.Curriculum.GetLabDefinition(r.Context(), lab.DefinitionID); err == nil && len(def.ValidationRules) > 0 {
			rules := make([]validation.Rule, 0, len(def.ValidationRules))
			for _, cr := range def.ValidationRules {
				rules = append(rules, validation.Rule{Rule: cr.Rule, Params: cr.Params, Description: cr.Description})
			}
			return rules
		}
	}
	return validation.DefaultRules(lab.DefinitionID)
}

func (s *Server) handleValidateLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	// The frontend posts with no body; an explicit rules body is
	// accepted when present. An empty body means "definition defaults".
	var req validateRequest
	if r.ContentLength != 0 {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_json", "could not read request body")
			return
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
				return
			}
		}
	}
	rules := req.Rules
	if len(rules) == 0 {
		rules = s.defaultLabRules(r, lab)
	}
	result := s.runLabValidation(r, lab, u, rules)
	s.audit(r, u.ID, "lab_validated", "lab", lab.ID, map[string]any{
		"passed": result.Passed, "checks": len(result.Checks),
	})
	// M8: a passing validation completes the lab for achievement
	// purposes (guided labs and challenges only — break-and-fix
	// scenarios are completed through the solve endpoint, which
	// awards their XP). Idempotent per user+lab.
	if result.Passed && s.Curriculum != nil && !strings.HasPrefix(lab.DefinitionID, "break-fix-") {
		kind := "lab"
		if strings.HasPrefix(lab.DefinitionID, "challenge-") {
			kind = "challenge"
		}
		if err := s.Curriculum.RecordLabCompletion(r.Context(), u.ID, lab.ID, lab.DefinitionID, kind); err == nil {
			s.evaluateAchievements(r, u.ID)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"lab_id":      lab.ID,
		"passed":      result.Passed,
		"xp_eligible": result.XPEligible,
		"checks":      result.Checks,
	})
}
