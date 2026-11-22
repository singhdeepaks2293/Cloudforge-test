// VPC networking detail API (Phase 2).
//
// Route tables, security-group rules, gateway attach/detach and NAT
// gateways on top of the lab-resource store (the same resources the
// VPC console lists) and the Rust network engine where it owns the
// state:
//
//   - Routes: the engine owns route state (AddRoute/DeleteRoute).
//     Engine failure is a 502 and nothing is persisted. The engine
//     needs a (gateway, interface) pair — resolveRouteNextHop maps
//     the user's target ("local", a gateway, an instance, a subnet)
//     to it: interface is the lab id (the engine bridge for the
//     lab's network) and the gateway is the VPC router address for
//     local/IGW targets, the NAT translated address, or the
//     instance's private IP.
//   - NAT: the engine owns NAT state (CreateNAT/DeleteNAT). In lite
//     mode the simulated engine answers; where the real netlink
//     backend runs, the NAT is real. Labelled SIMULATED in lite.
//   - Security-group rules: the DB is authoritative — the validation
//     engine evaluates config.rules (see internal/validation
//     sgRulesOf). The engine firewall policy is a snapshot: it is
//     created+applied when the first rule lands; the engine has no
//     update/delete RPC for policies, so later edits update the DB
//     and record engine_policy_applied=false with a note instead of
//     pretending the engine was updated.
//   - Internet-gateway attach/detach: recorded in the DB only — the
//     engine has no IGW RPC — and labelled SIMULATED.
//   - Network ACLs: the engine has no NACL RPC and there is no NACL
//     store. They stay UNSUPPORTED with the reason (no fake NACLs).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// --- CIDR helpers ----------------------------------------------------------

var errEngineUnavailable = errors.New("network engine client is not configured")

// newRouteID returns a fresh id for a route/rule (ids.New, short
// enough for the engine's idempotency keys).
func newRouteID() string { return ids.New() }

// isEngineUnavailable reports whether err is a transport-level
// engine failure (the engine is down) as opposed to an engine
// rejection of the request (which is a real 502 with a reason).
func isEngineUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if ee, ok := networkclient.AsEngineError(err); ok {
		return ee.Code.String() == "Unavailable"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "context deadline exceeded")
}

func parseCIDR(cidr string) (*net.IPNet, error) {
	_, n, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %v", cidr, err)
	}
	return n, nil
}

func cidrsOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

// validateSubnetCIDR checks a new subnet CIDR: valid CIDR, inside the
// parent VPC CIDR when one is known, and not overlapping any sibling
// subnet in the same lab.
func validateSubnetCIDR(vpcCIDR string, siblings []*store.LabResource, cidr string) error {
	n, err := parseCIDR(cidr)
	if err != nil {
		return err
	}
	if ones, bits := n.Mask.Size(); bits == 32 && (ones < 16 || ones > 28) {
		return fmt.Errorf("subnet prefix length must be between /16 and /28 (got /%d)", ones)
	}
	if vpcCIDR != "" {
		vpc, err := parseCIDR(vpcCIDR)
		if err == nil && !vpc.Contains(n.IP) {
			return fmt.Errorf("subnet CIDR %s is not inside the VPC CIDR %s", cidr, vpcCIDR)
		}
	}
	for _, sib := range siblings {
		if sib.Kind != "subnet" || sib.CIDR == "" {
			continue
		}
		other, err := parseCIDR(sib.CIDR)
		if err != nil {
			continue
		}
		if cidrsOverlap(n, other) {
			return fmt.Errorf("subnet CIDR %s overlaps existing subnet %s (%s)", cidr, sib.Name, sib.CIDR)
		}
	}
	return nil
}

// --- config helpers --------------------------------------------------------

// cfgList reads a []map[string]any list from resource config.
func cfgList(cfg map[string]any, key string) []map[string]any {
	raw, ok := cfg[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func cfgSetList(cfg map[string]any, key string, list []map[string]any) {
	anyList := make([]any, 0, len(list))
	for _, m := range list {
		anyList = append(anyList, m)
	}
	cfg[key] = anyList
}

func cfgString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func cfgInt(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	return 0, false
}

// --- Route tables ----------------------------------------------------------

type vpcRouteRequest struct {
	Destination string `json:"destination"`
	Target      string `json:"target"`
}

func (s *Server) handleAddRoute(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	res, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if res.Kind != "route-table" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "routes can only be added to a route-table resource")
		return
	}
	var req vpcRouteRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if _, err := parseCIDR(req.Destination); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Target) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "target is required (a gateway, NAT gateway or instance resource id, or 'local')")
		return
	}
	if req.Target != "local" {
		if _, err := s.Resources.GetResource(r.Context(), lab.ID, req.Target); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "target does not reference a resource in this lab")
			return
		}
	}
	// Engine first: routes are engine state, and a route must not
	// be recorded when the engine rejected it. The engine's
	// AddRoute needs a real (gateway, interface) pair — see
	// resolveRouteNextHop: the target the user picks ("local", a
	// gateway, an instance) is not itself a next-hop address.
	if s.Network == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "engine_unavailable", "network engine client is not configured")
		return
	}
	gateway, iface, err := resolveRouteNextHop(r.Context(), s, lab.ID, res, req.Target)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	routeID := "rt-" + shortSuffix(newRouteID())
	ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
	if _, err := s.Network.AddRoute(ctx, &gen.AddRouteRequest{
		Meta:    networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "add-route:"+routeID),
		RouteId: routeID, Destination: req.Destination, Gateway: gateway, Interface: iface, Namespace: lab.ID,
	}); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	if res.Config == nil {
		res.Config = map[string]any{}
	}
	routes := cfgList(res.Config, "routes")
	routes = append(routes, map[string]any{"id": routeID, "destination": req.Destination, "target": req.Target, "gateway": gateway, "interface": iface, "status": "active"})
	cfgSetList(res.Config, "routes", routes)
	updated, err := s.Resources.UpdateResource(r.Context(), res)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_route_added", "lab_resource", res.ID, map[string]any{"lab_id": lab.ID, "destination": req.Destination, "target": req.Target})
	s.writeJSON(w, r, http.StatusCreated, viewResource(updated))
}

// resolveRouteNextHop maps a route target to the (gateway,
// interface) pair the engine's AddRoute requires. The engine
// validates both: interface must be non-empty and gateway must
// parse as an IPv4 address (on the real netlink backend the route
// becomes `ip route add <dst> via <gw> dev <iface>`).
//
//   - Interface is always the lab id: ensureEngineNetwork creates
//     the lab's engine network with NetworkId = lab id, and the
//     real backend names that bridge after the network id.
//   - "local" (or the VPC itself) routes via the VPC router: the
//     first usable address of the route table's parent VPC CIDR
//     (10.0.0.1 for 10.0.0.0/16 — the AWS .1 convention).
//   - A NAT gateway routes via its translated address
//     (203.0.113.x, documentation range).
//   - An internet gateway has no address of its own; it is reached
//     through the VPC router, so it also uses the VPC gateway
//     address.
//   - An instance routes via its recorded private IP; an instance
//     with no private IP recorded cannot be a next hop (400).
//   - A subnet routes via the first address of that subnet's CIDR.
//
// Before this mapping the handler passed the raw target ("local"
// or a resource id) as the gateway and left the interface empty,
// so the engine rejected every route with INVALID_ARGUMENT and no
// route could ever be added.
func resolveRouteNextHop(ctx context.Context, s *Server, labID string, rt *store.LabResource, target string) (gateway, iface string, err error) {
	iface = labID
	vpcGateway, vpcErr := vpcGatewayAddress(ctx, s, labID, rt)
	if target == "local" {
		if vpcErr != nil {
			return "", "", vpcErr
		}
		return vpcGateway, iface, nil
	}
	t, err := s.Resources.GetResource(ctx, labID, target)
	if err != nil {
		return "", "", fmt.Errorf("target does not reference a resource in this lab")
	}
	switch t.Kind {
	case "vpc":
		if vpcErr != nil {
			return "", "", vpcErr
		}
		return vpcGateway, iface, nil
	case "gateway":
		if cfgString(t.Config, "gateway_type") == "nat" {
			addr := cfgString(t.Config, "translated_address")
			if addr == "" {
				return "", "", fmt.Errorf("NAT gateway %q has no translated address recorded", t.Name)
			}
			return addr, iface, nil
		}
		// Internet gateway: reached through the VPC router.
		if vpcErr != nil {
			return "", "", vpcErr
		}
		return vpcGateway, iface, nil
	case "instance":
		if t.PrivateIP == "" {
			return "", "", fmt.Errorf("instance %q has no private IP recorded and cannot be a route next hop", t.Name)
		}
		return t.PrivateIP, iface, nil
	case "subnet":
		if t.CIDR == "" {
			return "", "", fmt.Errorf("subnet %q has no CIDR recorded", t.Name)
		}
		addr, err := firstHostAddress(t.CIDR)
		return addr, iface, err
	default:
		return "", "", fmt.Errorf("a %s resource cannot be a route target (use local, a gateway, an instance or a subnet)", t.Kind)
	}
}

// vpcGatewayAddress returns the first usable address of the VPC
// that owns the route table (the table's parent, else the lab's
// first VPC, else the lab default CIDR).
func vpcGatewayAddress(ctx context.Context, s *Server, labID string, rt *store.LabResource) (string, error) {
	cidr := ""
	if rt.ParentID != "" {
		if p, err := s.Resources.GetResource(ctx, labID, rt.ParentID); err == nil && p.Kind == "vpc" {
			cidr = p.CIDR
			if cidr == "" {
				cidr = cfgString(p.Config, "cidr")
			}
		}
	}
	if cidr == "" {
		cidr = s.labVPCCIDR(ctx, labID)
	}
	if cidr == "" {
		return "", fmt.Errorf("no VPC CIDR is recorded for this lab, so a gateway address cannot be derived")
	}
	addr, err := firstHostAddress(cidr)
	if err != nil {
		return "", err
	}
	return addr, nil
}

// firstHostAddress returns the first usable (network + 1) address
// of an IPv4 CIDR, e.g. 10.0.0.1 for 10.0.0.0/16.
func firstHostAddress(cidr string) (string, error) {
	_, n, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %v", cidr, err)
	}
	ip := n.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("CIDR %q is not IPv4", cidr)
	}
	next := make(net.IP, len(ip))
	copy(next, ip)
	next[3]++
	if !n.Contains(next) {
		return "", fmt.Errorf("CIDR %q has no usable host address", cidr)
	}
	return next.String(), nil
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	res, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if res.Kind != "route-table" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "routes can only be deleted from a route-table resource")
		return
	}
	routeID := chi.URLParam(r, "routeId")
	routes := cfgList(res.Config, "routes")
	kept := []map[string]any{}
	found := false
	for _, rt := range routes {
		if cfgString(rt, "id") == routeID {
			found = true
			continue
		}
		kept = append(kept, rt)
	}
	if !found {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no route with that id on this route table")
		return
	}
	// Engine first (a NOT_FOUND there is fine — the route may
	// already be gone from the engine after a lab rebuild).
	if s.Network != nil {
		ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
		if _, err := s.Network.DeleteRoute(ctx, &gen.DeleteRouteRequest{
			Meta:    networkclient.Meta(lab.ID, u.ID, RequestID(r.Context()), "delete-route:"+routeID),
			RouteId: routeID,
		}); err != nil && !isEngineNotFound(err) {
			s.writeEngineError(w, r, err)
			return
		}
	}
	cfgSetList(res.Config, "routes", kept)
	updated, err := s.Resources.UpdateResource(r.Context(), res)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_route_deleted", "lab_resource", res.ID, map[string]any{"lab_id": lab.ID, "route_id": routeID})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}

// --- Security groups -------------------------------------------------------

type sgRuleInput struct {
	Direction string `json:"direction"` // ingress | egress
	Protocol  string `json:"protocol"`  // tcp | udp | icmp | any
	FromPort  int    `json:"from_port"`
	ToPort    int    `json:"to_port"`
	CIDR      string `json:"cidr"`
	Action    string `json:"action"` // allow | deny
}

func validateSGRule(in sgRuleInput) error {
	switch strings.ToLower(in.Direction) {
	case "ingress", "egress":
	default:
		return fmt.Errorf("direction must be ingress or egress")
	}
	switch strings.ToLower(in.Protocol) {
	case "tcp", "udp", "icmp", "any", "":
	default:
		return fmt.Errorf("protocol must be tcp, udp, icmp or any")
	}
	if in.FromPort < 0 || in.FromPort > 65535 || in.ToPort < 0 || in.ToPort > 65535 {
		return fmt.Errorf("ports must be between 0 and 65535")
	}
	if in.FromPort > in.ToPort && !(in.FromPort == 0 && in.ToPort == 0) {
		return fmt.Errorf("from_port must not exceed to_port")
	}
	if _, err := parseCIDR(in.CIDR); err != nil {
		return err
	}
	switch strings.ToLower(in.Action) {
	case "allow", "deny", "":
	default:
		return fmt.Errorf("action must be allow or deny")
	}
	return nil
}

func sgRuleToEngine(r map[string]any) *gen.FirewallRule {
	dir := gen.FirewallDirection_FIREWALL_DIRECTION_INGRESS
	if cfgString(r, "direction") == "egress" {
		dir = gen.FirewallDirection_FIREWALL_DIRECTION_EGRESS
	}
	proto := gen.FirewallProtocol_FIREWALL_PROTOCOL_ANY
	switch cfgString(r, "protocol") {
	case "tcp":
		proto = gen.FirewallProtocol_FIREWALL_PROTOCOL_TCP
	case "udp":
		proto = gen.FirewallProtocol_FIREWALL_PROTOCOL_UDP
	case "icmp":
		proto = gen.FirewallProtocol_FIREWALL_PROTOCOL_ICMP
	}
	action := gen.FirewallAction_FIREWALL_ACTION_ALLOW
	if cfgString(r, "action") == "deny" {
		action = gen.FirewallAction_FIREWALL_ACTION_DENY
	}
	from, _ := cfgInt(r, "from_port")
	to, _ := cfgInt(r, "to_port")
	return &gen.FirewallRule{
		RuleId: cfgString(r, "id"), Direction: dir, Protocol: proto,
		PortStart: int32(from), PortEnd: int32(to), Cidr: cfgString(r, "cidr"),
		Action: action, Priority: 100,
	}
}

// syncSGToEngine attempts to create+apply the SG's full rule set as
// one engine firewall policy. It returns (applied, note): applied is
// false when the engine is unreachable (rules stay DB-authoritative)
// or when a policy already exists (the engine has no update RPC).
// A hard engine rejection (bad rule) is an error: the caller must
// not persist the rule.
func (s *Server) syncSGToEngine(r *http.Request, labID, userID string, sg *store.LabResource, rules []map[string]any) (bool, string, error) {
	if s.Network == nil {
		return false, "network engine client is not configured; rules are recorded in CloudForge and evaluated by lab validation, not by the engine", nil
	}
	if applied, _ := sg.Config["engine_policy_applied"].(bool); applied {
		return false, "engine firewall policies cannot be updated (the engine has no update RPC); rules are recorded in CloudForge and evaluated by lab validation — delete and recreate the security group to re-sync the engine snapshot", nil
	}
	engineRules := make([]*gen.FirewallRule, 0, len(rules))
	for _, r := range rules {
		engineRules = append(engineRules, sgRuleToEngine(r))
	}
	ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
	name := dnsLabel(sg.Name)
	if name == "" {
		name = "sg-" + shortSuffix(sg.ID)
	}
	if _, err := s.Network.CreateFirewallPolicy(ctx, &gen.CreateFirewallPolicyRequest{
		Meta:     networkclient.Meta(labID, userID, RequestID(r.Context()), "sg-policy:"+sg.ID),
		PolicyId: sg.ID, NetworkId: labID, Name: name, Rules: engineRules,
	}); err != nil {
		if isEngineNotFound(err) {
			// The engine has no network for this lab yet (lab
			// provisioning does not create one, and the network is
			// otherwise created lazily by subnet creation): create
			// it from the lab's VPC CIDR and retry once, exactly
			// like subnet creation does. Found live 2026-11-22:
			// adding the first SG rule to a lab with no subnet
			// 502'd with NOT_FOUND instead of recording the rule.
			if nerr := s.ensureEngineNetwork(ctx, labID, userID, s.labVPCCIDR(ctx, labID)); nerr != nil {
				return false, "", nerr
			}
			if _, err := s.Network.CreateFirewallPolicy(ctx, &gen.CreateFirewallPolicyRequest{
				Meta:     networkclient.Meta(labID, userID, RequestID(r.Context()), "sg-policy:"+sg.ID+":retry"),
				PolicyId: sg.ID, NetworkId: labID, Name: name, Rules: engineRules,
			}); err != nil {
				if isEngineUnavailable(err) {
					return false, "network engine unreachable; rules are recorded in CloudForge and evaluated by lab validation, not by the engine", nil
				}
				return false, "", err
			}
		} else {
			if isEngineUnavailable(err) {
				return false, "network engine unreachable; rules are recorded in CloudForge and evaluated by lab validation, not by the engine", nil
			}
			return false, "", err
		}
	}
	if _, err := s.Network.ApplyFirewallPolicy(ctx, &gen.ApplyFirewallPolicyRequest{
		Meta:     networkclient.Meta(labID, userID, RequestID(r.Context()), "sg-apply:"+sg.ID),
		PolicyId: sg.ID,
	}); err != nil {
		if isEngineUnavailable(err) {
			return false, "network engine unreachable while applying the firewall policy; the policy was created but may not be applied", nil
		}
		return false, "", err
	}
	return true, "firewall policy created and applied in the network engine", nil
}

// dnsLabel sanitises a name into a DNS label for the engine (which
// validates firewall-policy names as DNS labels).
func dnsLabel(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Server) handleAddSGRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	sg, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if sg.Kind != "security-group" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rules can only be added to a security-group resource")
		return
	}
	var in sgRuleInput
	if !s.decodeJSON(w, r, &in) {
		return
	}
	if in.Protocol == "" {
		in.Protocol = "tcp"
	}
	if in.Action == "" {
		in.Action = "allow"
	}
	if err := validateSGRule(in); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if sg.Config == nil {
		sg.Config = map[string]any{}
	}
	rules := cfgList(sg.Config, "rules")
	ruleID := "rule-" + shortSuffix(newRouteID())
	rule := map[string]any{
		"id": ruleID, "direction": strings.ToLower(in.Direction),
		"protocol":  strings.ToLower(in.Protocol),
		"from_port": in.FromPort, "to_port": in.ToPort,
		// "source" is the key the validation engine reads
		// (sgRulesOf); "cidr" is the display key.
		"cidr": in.CIDR, "source": in.CIDR,
		"port": in.FromPort, "action": strings.ToLower(in.Action),
	}
	newRules := append(append([]map[string]any{}, rules...), rule)
	applied, note, err := s.syncSGToEngine(r, lab.ID, u.ID, sg, newRules)
	if err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	cfgSetList(sg.Config, "rules", newRules)
	sg.Config["engine_policy_applied"] = applied
	sg.Config["engine_sync_note"] = note
	updated, err := s.Resources.UpdateResource(r.Context(), sg)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_sg_rule_added", "lab_resource", sg.ID, map[string]any{"lab_id": lab.ID, "rule_id": ruleID})
	s.writeJSON(w, r, http.StatusCreated, viewResource(updated))
}

func (s *Server) handleDeleteSGRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	sg, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if sg.Kind != "security-group" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rules can only be deleted from a security-group resource")
		return
	}
	ruleID := chi.URLParam(r, "ruleId")
	rules := cfgList(sg.Config, "rules")
	kept := []map[string]any{}
	found := false
	for _, rl := range rules {
		if cfgString(rl, "id") == ruleID {
			found = true
			continue
		}
		kept = append(kept, rl)
	}
	if !found {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no rule with that id on this security group")
		return
	}
	// No engine update RPC exists: the DB is updated and the engine
	// snapshot is marked stale (syncSGToEngine reports this).
	sg.Config["engine_policy_applied"] = false
	sg.Config["engine_sync_note"] = "engine firewall policies cannot be updated (the engine has no update RPC); rules are recorded in CloudForge and evaluated by lab validation — delete and recreate the security group to re-sync the engine snapshot"
	cfgSetList(sg.Config, "rules", kept)
	updated, err := s.Resources.UpdateResource(r.Context(), sg)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_sg_rule_deleted", "lab_resource", sg.ID, map[string]any{"lab_id": lab.ID, "rule_id": ruleID})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}

func (s *Server) handleAttachSGInstance(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	sg, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if sg.Kind != "security-group" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "only a security group can be attached to an instance")
		return
	}
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	inst, err := s.Resources.GetResource(r.Context(), lab.ID, req.InstanceID)
	if err != nil || inst.Kind != "instance" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "instance_id does not reference an instance resource in this lab")
		return
	}
	if sg.Config == nil {
		sg.Config = map[string]any{}
	}
	attached := cfgStringList(sg.Config, "attached_instances")
	if !containsStr(attached, req.InstanceID) {
		attached = append(attached, req.InstanceID)
		cfgSetStringList(sg.Config, "attached_instances", attached)
	}
	if inst.Config == nil {
		inst.Config = map[string]any{}
	}
	sgs := cfgStringList(inst.Config, "security_groups")
	if !containsStr(sgs, sg.ID) {
		sgs = append(sgs, sg.ID)
		cfgSetStringList(inst.Config, "security_groups", sgs)
	}
	if _, err := s.Resources.UpdateResource(r.Context(), inst); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	updated, err := s.Resources.UpdateResource(r.Context(), sg)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_sg_attached", "lab_resource", sg.ID, map[string]any{"lab_id": lab.ID, "instance_id": req.InstanceID})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}

func cfgStringList(cfg map[string]any, key string) []string {
	raw, ok := cfg[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func cfgSetStringList(cfg map[string]any, key string, list []string) {
	anyList := make([]any, 0, len(list))
	for _, s := range list {
		anyList = append(anyList, s)
	}
	cfg[key] = anyList
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// --- Gateways ------------------------------------------------------------

type gatewayAttachRequest struct {
	VPCID string `json:"vpc_id"`
}

func (s *Server) handleGatewayAttach(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	gw, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if gw.Kind != "gateway" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "only a gateway resource can be attached")
		return
	}
	var req gatewayAttachRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	vpc, err := s.Resources.GetResource(r.Context(), lab.ID, req.VPCID)
	if err != nil || vpc.Kind != "vpc" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "vpc_id does not reference a VPC resource in this lab")
		return
	}
	if gw.Config == nil {
		gw.Config = map[string]any{}
	}
	gw.Config["attached_vpc_id"] = req.VPCID
	gw.State = "attached"
	updated, err := s.Resources.UpdateResource(r.Context(), gw)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_gateway_attached", "lab_resource", gw.ID, map[string]any{"lab_id": lab.ID, "vpc_id": req.VPCID})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}

func (s *Server) handleGatewayDetach(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	gw, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	if gw.Kind != "gateway" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "only a gateway resource can be detached")
		return
	}
	if gw.Config == nil {
		gw.Config = map[string]any{}
	}
	delete(gw.Config, "attached_vpc_id")
	gw.State = "available"
	updated, err := s.Resources.UpdateResource(r.Context(), gw)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "vpc_gateway_detached", "lab_resource", gw.ID, map[string]any{"lab_id": lab.ID})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}

// allocateNATAddress assigns the next documentation-range address
// (TEST-NET-3, 203.0.113.0/24) for a NAT gateway in this lab. These
// are not real public IPs — they are placeholders the simulated
// engine records as the translated address, and the UI says so.
func allocateNATAddress(siblings []*store.LabResource) string {
	n := 0
	for _, sib := range siblings {
		if sib.Kind == "gateway" {
			if gt, _ := sib.Config["gateway_type"].(string); gt == "nat" {
				n++
			}
		}
	}
	return fmt.Sprintf("203.0.113.%d", 10+n)
}

// createNATGateway creates the engine NAT for a gateway resource
// whose config.gateway_type is "nat". Engine failure is a 502 and
// the caller must not persist the resource.
func (s *Server) createNATGateway(r *http.Request, labID, userID string, gw *store.LabResource, siblings []*store.LabResource) error {
	if s.Network == nil {
		return errEngineUnavailable
	}
	sourceCIDR := gw.CIDR
	if sourceCIDR == "" {
		sourceCIDR = s.labVPCCIDR(r.Context(), labID)
	}
	addr := allocateNATAddress(siblings)
	ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
	if _, err := s.Network.CreateNAT(ctx, &gen.CreateNATRequest{
		Meta:  networkclient.Meta(labID, userID, RequestID(r.Context()), "create-nat:"+gw.ID),
		NatId: gw.ID, Kind: gen.NatKind_NAT_KIND_SNAT,
		SourceCidr: sourceCIDR, TranslatedAddress: addr,
	}); err != nil {
		return err
	}
	gw.PublicIP = addr
	if gw.Config == nil {
		gw.Config = map[string]any{}
	}
	gw.Config["translated_address"] = addr
	gw.Config["source_cidr"] = sourceCIDR
	return nil
}

// deleteNATGateway removes the engine NAT for a NAT gateway resource.
// A NOT_FOUND from the engine is fine (already gone).
func (s *Server) deleteNATGateway(r *http.Request, labID, userID string, gw *store.LabResource) error {
	if s.Network == nil {
		return nil
	}
	ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
	if _, err := s.Network.DeleteNAT(ctx, &gen.DeleteNATRequest{
		Meta:  networkclient.Meta(labID, userID, RequestID(r.Context()), "delete-nat:"+gw.ID),
		NatId: gw.ID,
	}); err != nil && !isEngineNotFound(err) {
		return err
	}
	return nil
}

// --- Tags ----------------------------------------------------------------

type tagsRequest struct {
	Tags map[string]string `json:"tags"`
}

// handleUpdateTags replaces a resource's tags (labels). Keys starting
// with "aws:" are reserved and rejected, as in AWS. An empty map
// clears all tags.
func (s *Server) handleUpdateTags(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	res, err := s.Resources.GetResource(r.Context(), lab.ID, chi.URLParam(r, "resourceId"))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id in this lab")
		return
	}
	var req tagsRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	for k := range req.Tags {
		if strings.HasPrefix(strings.ToLower(k), "aws:") {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "tag keys starting with \"aws:\" are reserved")
			return
		}
		if len(k) == 0 || len(k) > 128 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "tag keys must be 1-128 characters")
			return
		}
	}
	if req.Tags == nil {
		req.Tags = map[string]string{}
	}
	res.Labels = req.Tags
	updated, err := s.Resources.UpdateResource(r.Context(), res)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "lab_resource_tags_updated", "lab_resource", res.ID, map[string]any{"lab_id": lab.ID, "tags": req.Tags})
	s.writeJSON(w, r, http.StatusOK, viewResource(updated))
}
