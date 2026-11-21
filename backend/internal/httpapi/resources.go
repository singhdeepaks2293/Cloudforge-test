// Milestone 6 lab-resource handlers.
//
// GET returns the lab's real resources from the store. In Postgres
// mode lab creation already writes a VPC row, so a provisioned lab
// is never empty by construction; when the store genuinely has no
// rows the response is an empty list with a note — never invented
// resources. Host-agent VMs are merged in (best effort) so resources
// provisioned by the worker show up even before a DB row exists.
// POST validates the kind, calls the Rust engine (subnet) or the
// host agent (instance) as appropriate, and only then persists the
// row; an engine/host-agent failure is a 502 with the structured
// engine error, never a faked success. DELETE is idempotent.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/go-chi/chi/v5"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

var resourceKinds = map[string]bool{
	"vpc": true, "subnet": true, "instance": true, "gateway": true,
	"loadbalancer": true, "route-table": true, "security-group": true,
}

// resourceView is the API shape. The frontend parser reads id, kind
// (or type), name, state/status, private_ip, public_ip, parent_id and
// labels; config is extra detail the UI ignores.
type resourceView struct {
	ID                 string            `json:"id"`
	Kind               string            `json:"kind"`
	Type               string            `json:"type"`
	Name               string            `json:"name"`
	State              string            `json:"state"`
	Provider           string            `json:"provider,omitempty"`
	ProviderResourceID string            `json:"provider_resource_id,omitempty"`
	ParentID           string            `json:"parent_id,omitempty"`
	PrivateIP          string            `json:"private_ip,omitempty"`
	PublicIP           string            `json:"public_ip,omitempty"`
	CIDR               string            `json:"cidr,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Config             map[string]any    `json:"config,omitempty"`
}

func viewResource(r *store.LabResource) resourceView {
	name := r.Name
	if name == "" {
		name = r.Kind + "-" + shortSuffix(r.ID)
	}
	return resourceView{
		ID: r.ID, Kind: r.Kind, Type: r.Kind, Name: name, State: r.State,
		Provider: r.Provider, ProviderResourceID: r.ProviderResourceID,
		ParentID: r.ParentID, PrivateIP: r.PrivateIP, PublicIP: r.PublicIP,
		CIDR: r.CIDR, Labels: r.Labels, Config: r.Config,
	}
}

func shortSuffix(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (s *Server) handleListResources(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	list, err := s.Resources.ListResources(r.Context(), lab.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := make([]resourceView, 0, len(list))
	seen := map[string]bool{}
	for _, res := range list {
		views = append(views, viewResource(res))
		seen[res.ID] = true
		if res.ProviderResourceID != "" {
			seen[res.ProviderResourceID] = true
		}
	}
	note := ""
	if len(views) == 0 {
		note = "no resources recorded for this lab yet"
	}
	// Merge host-agent VMs (worker-provisioned) that have no DB row.
	if s.HostAgent != nil {
		if vms, err := s.listHostAgentVMs(r.Context(), lab.ID, u.ID); err == nil {
			for _, vm := range vms {
				if seen[vm.ID] {
					continue
				}
				views = append(views, resourceView{
					ID: "hostagent:" + vm.ID, Kind: "instance", Type: "instance",
					Name: vm.Name, State: vm.State, Provider: "host-agent",
					ProviderResourceID: vm.ID, PrivateIP: vm.IP,
					Config: map[string]any{"source": "host-agent", "profile": vm.Profile},
				})
			}
			if note != "" && len(views) > 0 {
				note = ""
			}
		} else if len(views) == 0 {
			note = "no resources recorded for this lab yet; the host agent could not be reached to list provisioned VMs (" + err.Error() + ")"
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"resources": views, "note": note})
}

// listHostAgentVMs returns the lab's VMs via the typed client. An
// unavailable host agent is an error the caller reports honestly.
func (s *Server) listHostAgentVMs(ctx context.Context, labID, ownerID string) ([]vmInfo, error) {
	res, err := s.HostAgent.Do(ctx, hostagent.OpRequest{
		Op: hostagent.OpListVMs, LabID: labID, OwnerID: ownerID,
	})
	if err != nil {
		return nil, err
	}
	var out []vmInfo
	for _, item := range res.Items {
		v := vmInfo{}
		v.ID, _ = item["id"].(string)
		v.Name, _ = item["name"].(string)
		v.State, _ = item["state"].(string)
		v.IP, _ = item["ip"].(string)
		v.Profile, _ = item["profile"].(string)
		if v.ID == "" {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

type vmInfo struct {
	ID      string
	Name    string
	State   string
	IP      string
	Profile string
}

type createResourceRequest struct {
	Kind     string         `json:"kind"`
	Type     string         `json:"type"` // accepted alias for kind
	Name     string         `json:"name"`
	ParentID string         `json:"parent_id"`
	CIDR     string         `json:"cidr"`
	Profile  string         `json:"profile"` // instance VM profile
	Config   map[string]any `json:"config"`
}

func (s *Server) handleCreateResource(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	var req createResourceRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = req.Type
	}
	if !resourceKinds[kind] {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"kind must be one of vpc|subnet|instance|gateway|loadbalancer|route-table|security-group")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	cfg := req.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	if req.CIDR != "" {
		cfg["cidr"] = req.CIDR
	}
	if req.ParentID != "" {
		if _, err := s.Resources.GetResource(r.Context(), lab.ID, req.ParentID); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "parent_id does not reference a resource in this lab")
			return
		}
	}
	res := &store.LabResource{
		ID: ids.New(), LabID: lab.ID, OwnerID: u.ID, Kind: kind, Name: req.Name,
		State: "CREATED", Provider: "simulator", ParentID: req.ParentID,
		CIDR: req.CIDR, Config: cfg, CreatedAt: time.Now().UTC(),
	}
	// Kind-specific provisioning through the real backends.
	switch kind {
	case "subnet":
		if req.CIDR == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "subnet creation requires a cidr")
			return
		}
		if s.Network == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "engine_unavailable", "network engine client is not configured")
			return
		}
		ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
		resp, err := s.Network.CreateSubnet(ctx, &gen.CreateSubnetRequest{
			Meta:      networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "resource:"+res.ID),
			SubnetId:  res.ID,
			NetworkId: lab.ID,
			Name:      req.Name,
			Cidr:      req.CIDR,
		})
		if err != nil && isEngineNotFound(err) {
			// The engine has no network for this lab yet (lab
			// provisioning does not create one — see jobs.Service):
			// create it from the lab's VPC CIDR (or the lab default
			// 10.0.0.0/16) and retry once. A failure here is a real
			// 502, not a faked subnet.
			if nerr := s.ensureEngineNetwork(r.Context(), lab.ID, u.ID, s.labVPCCIDR(r.Context(), lab.ID)); nerr != nil {
				s.writeEngineError(w, r, nerr)
				return
			}
			resp, err = s.Network.CreateSubnet(ctx, &gen.CreateSubnetRequest{
				Meta:      networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "resource:"+res.ID+":retry"),
				SubnetId:  res.ID,
				NetworkId: lab.ID,
				Name:      req.Name,
				Cidr:      req.CIDR,
			})
		}
		if err != nil {
			s.writeEngineError(w, r, err)
			return
		}
		res.Provider = "rust-network-engine"
		res.State = "available"
		if resp.GetSubnet() != nil {
			res.ProviderResourceID = resp.GetSubnet().GetSubnetId()
			res.CIDR = resp.GetSubnet().GetCidr()
		}
	case "instance":
		if s.HostAgent == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "host_agent_unavailable",
				"instance creation needs a host agent, and none is configured (set HOST_AGENT_ADDR)")
			return
		}
		profile := req.Profile
		if profile == "" {
			if p, ok := cfg["profile"].(string); ok && p != "" {
				profile = p
			} else {
				profile = "micro"
			}
		}
		// Per-role VM size limits (M9 Part 3): refuse a profile the
		// caller's role cannot afford before provisioning anything.
		if err := s.checkInstanceProfile(r.Context(), u, profile); err != nil {
			if errors.Is(err, quota.ErrQuotaExceeded) {
				s.writeError(w, r, http.StatusTooManyRequests, "quota_exceeded", err.Error())
			} else {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			}
			return
		}
		name := "lab-" + lab.ID
		if len(name) > 40 {
			name = name[:40]
		}
		created, err := s.HostAgent.Do(r.Context(), hostagent.OpRequest{
			Op: hostagent.OpCreateVM, LabID: lab.ID, OwnerID: u.ID,
			Name: name + "-" + shortSuffix(res.ID), Profile: profile,
			Hostname: req.Name, Username: "cloudforge",
			IdempotencyKey: "resource:" + res.ID,
		})
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "host_agent_error", "host agent CreateVM failed: "+err.Error())
			return
		}
		res.Provider = "host-agent"
		res.ProviderResourceID = created.ID
		res.State = "defined"
		cfg["profile"] = profile
		res.Config = cfg
	case "security-group":
		// Recorded in the DB; engine firewall-policy creation is a
		// separate step (the engine needs policy rules, not just a
		// name) and is not faked here.
		res.Provider = "rust-network-engine"
	case "vpc":
		res.Provider = "rust-network-engine"
		if res.CIDR != "" {
			cfg["cidr"] = res.CIDR
		}
	}
	created, err := s.Resources.CreateResource(r.Context(), res)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "lab_resource_created", "lab_resource", created.ID,
		map[string]any{"lab_id": lab.ID, "kind": kind, "name": req.Name})
	s.writeJSON(w, r, http.StatusCreated, viewResource(created))
}

// ensureEngineNetwork creates the lab's network in the Rust engine.
// It is called lazily from subnet creation because lab provisioning
// (jobs.Service) does not create an engine network.
func (s *Server) ensureEngineNetwork(ctx context.Context, labID, ownerID, cidr string) error {
	_, err := s.Network.CreateNetwork(ctx, &gen.CreateNetworkRequest{
		Meta:      networkclient.Meta(labID, ownerID, RequestID(ctx), "ensure-network:"+labID),
		NetworkId: labID,
		Name:      "lab-" + labID,
		Cidr:      cidr,
	})
	return err
}

// labVPCCIDR returns the CIDR of the lab's VPC resource, or the lab
// default 10.0.0.0/16 when the lab has no VPC CIDR recorded.
func (s *Server) labVPCCIDR(ctx context.Context, labID string) string {
	if list, err := s.Resources.ListResources(ctx, labID); err == nil {
		for _, r := range list {
			if r.Kind == "vpc" {
				if r.CIDR != "" {
					return r.CIDR
				}
				if c, ok := r.Config["cidr"].(string); ok && c != "" {
					return c
				}
			}
		}
	}
	return "10.0.0.0/16"
}

// writeEngineError renders a 502 with the structured engine error
// (gRPC code + the engine's stable error code when present).
func (s *Server) writeEngineError(w http.ResponseWriter, r *http.Request, err error) {
	body := map[string]any{
		"error": map[string]any{
			"code": "engine_error", "message": err.Error(),
			"request_id": RequestID(r.Context()),
		},
	}
	if ee, ok := networkclient.AsEngineError(err); ok {
		body["error"].(map[string]any)["engine_code"] = ee.EngineCode
		body["error"].(map[string]any)["grpc_code"] = ee.Code.String()
	}
	s.writeJSON(w, r, http.StatusBadGateway, body)
}

func (s *Server) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	id := chi.URLParam(r, "resourceId")
	res, err := s.Resources.GetResource(r.Context(), lab.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Idempotent delete: already gone is a success.
			s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Delete through the backend that owns the resource first; a
	// backend failure keeps the DB row (honest, retryable).
	switch res.Kind {
	case "subnet":
		if s.Network != nil {
			subnetID := res.ProviderResourceID
			if subnetID == "" {
				subnetID = res.ID
			}
			ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
			if _, err := s.Network.DeleteSubnet(ctx, &gen.DeleteSubnetRequest{
				Meta:     networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "delete-resource:"+id),
				SubnetId: subnetID,
			}); err != nil {
				if !isEngineNotFound(err) {
					s.writeEngineError(w, r, err)
					return
				}
			}
		}
	case "instance":
		if s.HostAgent != nil && res.ProviderResourceID != "" {
			if _, err := s.HostAgent.Do(r.Context(), hostagent.OpRequest{
				Op: hostagent.OpDeleteVM, LabID: lab.ID, OwnerID: u.ID, ID: res.ProviderResourceID,
			}); err != nil && !hostagent.IsUnavailable(err) {
				// A missing VM is idempotent; other errors fail honestly.
				if !strings.Contains(err.Error(), "not found") {
					s.writeError(w, r, http.StatusBadGateway, "host_agent_error", "host agent DeleteVM failed: "+err.Error())
					return
				}
			}
		}
	}
	if _, err := s.Resources.DeleteResource(r.Context(), lab.ID, id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "lab_resource_deleted", "lab_resource", id, map[string]any{"lab_id": lab.ID, "kind": res.Kind})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

func isEngineNotFound(err error) bool {
	if ee, ok := networkclient.AsEngineError(err); ok {
		return ee.EngineCode == "NOT_FOUND" || ee.Code.String() == "NotFound"
	}
	return strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NOT_FOUND")
}
