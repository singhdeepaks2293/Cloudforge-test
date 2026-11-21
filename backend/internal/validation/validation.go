// Package validation implements the Milestone 6 lab validation engine.
//
// Rules inspect REAL state only: lab resources from the store, the Rust
// network engine's InspectTopology output, and host-agent VM state.
// A rule whose data source is unavailable returns status "unknown" with
// a reason — never "pass". A rule whose data source is available and
// the required state is absent returns "fail". Unknown rule names are
// "unknown", not errors, so a curriculum authoring mistake degrades to
// a visible unknown check instead of failing the whole validation.
//
// The rule vocabulary is the curriculum's (see curriculum/README.md);
// this package implements the subset whose data sources exist in the
// control plane. Rules that need AWS-provider state (buckets, queues,
// Lambda, …) are reported unknown with the missing source named.
// M8 added no_route_to_internet, security_group_denies,
// load_balancer_healthy_targets, instance_reachable and a real-state
// dns_resolves (VPC dns_enabled config, else the control-plane
// resolver for public names only) and metric_below (metrics recorded
// in a resource's config; unknown without a metrics pipeline).
package validation

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// Status values for one check.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusUnknown = "unknown"
)

// Rule is one requested validation rule with its params.
type Rule struct {
	Rule        string         `json:"rule"`
	Params      map[string]any `json:"params,omitempty"`
	Description string         `json:"description,omitempty"`
}

// Check is one evaluated rule.
type Check struct {
	Rule    string `json:"rule"`
	RuleID  string `json:"rule_id"` // same as Rule; the frontend parser reads rule_id
	Status  string `json:"status"`  // pass | fail | unknown
	Passed  bool   `json:"passed"`  // Status == pass; the frontend parser reads this
	Detail  string `json:"detail"`
	Message string `json:"message"` // same as Detail; the frontend parser reads this
}

// TopologyNode is the minimal InspectTopology node shape the
// validators need (engine nodes merged with DB resources).
type TopologyNode struct {
	ID     string
	Kind   string
	Name   string
	Labels map[string]string
}

// VMState is one host-agent VM/container state for a lab.
type VMState struct {
	ID    string
	Name  string
	State string // running | stopped | defined | created | …
}

// State is everything the validators may inspect.
type State struct {
	LabExists        bool
	Resources        []*store.LabResource
	Topology         []TopologyNode
	EngineAvailable  bool // false = engine InspectTopology failed; topology is empty for that reason
	VMs              []VMState
	HostAgentChecked bool // true when a host-agent VM listing was attempted and answered
}

// Result is the outcome of validating a lab.
type Result struct {
	Passed     bool    `json:"passed"`
	XPEligible bool    `json:"xp_eligible"` // == Passed; XP awarding itself is M8, nothing is awarded here
	Checks     []Check `json:"checks"`
}

func check(rule, status, format string, args ...any) Check {
	return Check{Rule: rule, RuleID: rule, Status: status, Passed: status == StatusPass,
		Detail: fmt.Sprintf(format, args...), Message: fmt.Sprintf(format, args...)}
}

func unknown(rule, format string, args ...any) Check {
	return check(rule, StatusUnknown, format, args...)
}
func pass(rule, format string, args ...any) Check { return check(rule, StatusPass, format, args...) }
func fail(rule, format string, args ...any) Check { return check(rule, StatusFail, format, args...) }

// param helpers: curriculum params arrive as JSON numbers/strings.
func paramInt(p map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			switch n := v.(type) {
			case float64:
				return int(n), true
			case int:
				return n, true
			case string:
				if i, err := strconv.Atoi(n); err == nil {
					return i, true
				}
			}
		}
	}
	return 0, false
}

func paramString(p map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func compare(op string, got, want int) bool {
	switch op {
	case ">=", "":
		return got >= want
	case ">":
		return got > want
	case "<=":
		return got <= want
	case "<":
		return got < want
	case "==", "=":
		return got == want
	case "!=":
		return got != want
	default:
		return got >= want
	}
}

func resourcesOfKind(st State, kinds ...string) []*store.LabResource {
	var out []*store.LabResource
	for _, r := range st.Resources {
		for _, k := range kinds {
			if r.Kind == k {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func configString(r *store.LabResource, keys ...string) string {
	if r.Config == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := r.Config[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// routesOf returns the route list recorded on a route-table resource:
// config.routes = [{destination, target}] (also accepts target_gateway /
// gateway / next_hop keys).
func routesOf(r *store.LabResource) []map[string]string {
	var out []map[string]string
	raw, ok := r.Config["routes"]
	if !ok {
		return out
	}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		route := map[string]string{}
		for k, v := range m {
			if s, ok := v.(string); ok {
				route[k] = s
			}
		}
		out = append(out, route)
	}
	return out
}

func routeTarget(route map[string]string) string {
	for _, k := range []string{"target", "target_gateway", "gateway", "next_hop"} {
		if v := route[k]; v != "" {
			return v
		}
	}
	return ""
}

// sgRulesOf returns the ingress rules recorded on a security-group
// resource: config.rules = [{port, source, protocol}].
func sgRulesOf(r *store.LabResource) []map[string]any {
	var out []map[string]any
	raw, ok := r.Config["rules"]
	if !ok {
		return out
	}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func sgRuleAllows(rules []map[string]any, port int, source string) bool {
	for _, rule := range rules {
		p, ok := paramInt(rule, "port", "from_port")
		if !ok || p != port {
			continue
		}
		if source == "" {
			return true
		}
		if src, ok := rule["source"].(string); ok && (src == source || src == "0.0.0.0/0") {
			return true
		}
	}
	return false
}

// Validate evaluates rules against st. An empty rules list means
// "no rules were requested": the result passes vacuously with no
// checks (callers normally pass the lab definition's defaults).
func Validate(st State, rules []Rule) Result {
	res := Result{Checks: make([]Check, 0, len(rules))}
	for _, r := range rules {
		res.Checks = append(res.Checks, evalRule(st, r))
	}
	res.Passed = true
	for _, c := range res.Checks {
		if c.Status != StatusPass {
			res.Passed = false
			break
		}
	}
	res.XPEligible = res.Passed
	return res
}

func evalRule(st State, r Rule) Check {
	switch r.Rule {
	case "lab_exists":
		if st.LabExists {
			return pass(r.Rule, "lab session exists")
		}
		return fail(r.Rule, "no lab session exists for this definition")
	case "vpc_exists":
		vpcs := resourcesOfKind(st, "vpc")
		if len(vpcs) == 0 {
			// The engine is the second source for VPCs: if it is
			// unreachable we cannot tell a missing VPC from an
			// unobserved one.
			if !st.EngineAvailable {
				return unknown(r.Rule, "no VPC in the lab record and the network engine is unreachable, so VPC state cannot be confirmed")
			}
			return fail(r.Rule, "no VPC resource exists in this lab")
		}
		if want, ok := paramString(r.Params, "cidr"); ok {
			for _, v := range vpcs {
				if v.CIDR == want || configString(v, "cidr") == want {
					return pass(r.Rule, "VPC %s exists with CIDR %s", v.Name, want)
				}
			}
			return fail(r.Rule, "no VPC exists with CIDR %s (%d VPC(s) found)", want, len(vpcs))
		}
		return pass(r.Rule, "%d VPC(s) exist in this lab", len(vpcs))
	case "subnet_count", "subnet_exists":
		subnets := resourcesOfKind(st, "subnet")
		if r.Rule == "subnet_exists" {
			if len(subnets) == 0 {
				if !st.EngineAvailable {
					return unknown(r.Rule, "no subnet in the lab record and the network engine is unreachable, so subnet state cannot be confirmed")
				}
				return fail(r.Rule, "no subnet exists in this lab")
			}
			if want, ok := paramString(r.Params, "cidr"); ok {
				for _, s := range subnets {
					if s.CIDR == want {
						return pass(r.Rule, "subnet %s exists with CIDR %s", s.Name, want)
					}
				}
				return fail(r.Rule, "no subnet exists with CIDR %s", want)
			}
			return pass(r.Rule, "%d subnet(s) exist in this lab", len(subnets))
		}
		want, ok := paramInt(r.Params, "value", "count")
		if !ok {
			want = 1
		}
		op, _ := paramString(r.Params, "operator")
		if op == "" {
			op = ">="
		}
		if compare(op, len(subnets), want) {
			return pass(r.Rule, "%d subnet(s) satisfy %s %d", len(subnets), op, want)
		}
		return fail(r.Rule, "%d subnet(s) do not satisfy %s %d", len(subnets), op, want)
	case "route_to_igw", "route_to_nat":
		wantIGW := r.Rule == "route_to_igw"
		tables := resourcesOfKind(st, "route-table")
		if len(tables) == 0 {
			if !st.EngineAvailable {
				return unknown(r.Rule, "no route table in the lab record and the network engine is unreachable, so route state cannot be confirmed")
			}
			return fail(r.Rule, "no route table exists in this lab")
		}
		for _, t := range tables {
			for _, route := range routesOf(t) {
				dest := route["destination"]
				if dest != "0.0.0.0/0" && dest != "" {
					continue
				}
				target := strings.ToLower(routeTarget(route))
				if wantIGW && (strings.Contains(target, "igw") || strings.Contains(target, "internet")) {
					return pass(r.Rule, "route table %s routes 0.0.0.0/0 to internet gateway (%s)", t.Name, routeTarget(route))
				}
				if !wantIGW && (strings.Contains(target, "nat") || strings.Contains(target, "gateway")) {
					return pass(r.Rule, "route table %s routes 0.0.0.0/0 to NAT gateway (%s)", t.Name, routeTarget(route))
				}
			}
		}
		// A gateway resource of the right type with no route table is
		// not a route: fail honestly, naming what is missing.
		kind := "internet gateway"
		if !wantIGW {
			kind = "NAT gateway"
		}
		return fail(r.Rule, "no route table routes 0.0.0.0/0 to a %s", kind)
	case "nat_exists", "nat_gateway_exists":
		gws := resourcesOfKind(st, "gateway")
		want, ok := paramInt(r.Params, "min_count", "count")
		if !ok {
			want = 1
		}
		n := 0
		for _, g := range gws {
			typ := strings.ToLower(configString(g, "gateway_type", "type"))
			if typ == "" || typ == "nat" || strings.Contains(typ, "nat") {
				n++
			}
		}
		if n >= want {
			return pass(r.Rule, "%d NAT gateway(s) exist (need %d)", n, want)
		}
		return fail(r.Rule, "%d NAT gateway(s) exist (need %d)", n, want)
	case "security_group_allows_ssh", "security_group_allows":
		port := 22
		if r.Rule == "security_group_allows" {
			if p, ok := paramInt(r.Params, "port"); ok {
				port = p
			}
		}
		source, _ := paramString(r.Params, "source")
		sgs := resourcesOfKind(st, "security-group")
		if len(sgs) == 0 {
			if !st.EngineAvailable {
				return unknown(r.Rule, "no security group in the lab record and the network engine is unreachable, so security-group state cannot be confirmed")
			}
			return fail(r.Rule, "no security group exists in this lab")
		}
		for _, sg := range sgs {
			if sgRuleAllows(sgRulesOf(sg), port, source) {
				return pass(r.Rule, "security group %s allows port %d from %s", sg.Name, port, sourceOrAny(source))
			}
		}
		return fail(r.Rule, "no security group allows port %d from %s", port, sourceOrAny(source))
	case "instance_running":
		// Host-agent VM state is the authoritative source when it
		// answered; otherwise fall back to the DB resource states
		// and say so in the detail.
		running := 0
		total := 0
		source := "lab record"
		if st.HostAgentChecked {
			source = "host agent"
			for _, vm := range st.VMs {
				total++
				if vm.State == "running" {
					running++
				}
			}
		} else {
			for _, inst := range resourcesOfKind(st, "instance") {
				total++
				if inst.State == "running" || inst.State == "RUNNING" {
					running++
				}
			}
		}
		want, ok := paramInt(r.Params, "count", "value")
		if !ok {
			want = 1
		}
		op, _ := paramString(r.Params, "operator")
		if op == "" {
			op = ">="
		}
		if total == 0 && !st.HostAgentChecked {
			return unknown(r.Rule, "no instance in the lab record and the host agent was not reached, so instance state cannot be confirmed")
		}
		if compare(op, running, want) {
			return pass(r.Rule, "%d of %d instance(s) running (%s) satisfy %s %d", running, total, source, op, want)
		}
		return fail(r.Rule, "%d of %d instance(s) running (%s) do not satisfy %s %d", running, total, source, op, want)
	case "security_group_denies":
		port, ok := paramInt(r.Params, "port")
		if !ok {
			return unknown(r.Rule, "security_group_denies needs a port parameter")
		}
		source, _ := paramString(r.Params, "source")
		sgs := resourcesOfKind(st, "security-group")
		if len(sgs) == 0 {
			if !st.EngineAvailable {
				return unknown(r.Rule, "no security group in the lab record and the network engine is unreachable, so security-group state cannot be confirmed")
			}
			return fail(r.Rule, "no security group exists in this lab")
		}
		for _, sg := range sgs {
			if sgRuleAllows(sgRulesOf(sg), port, source) {
				return fail(r.Rule, "security group %s still allows port %d from %s", sg.Name, port, sourceOrAny(source))
			}
		}
		return pass(r.Rule, "no security group allows port %d from %s", port, sourceOrAny(source))
	case "no_route_to_internet":
		// Pass when no route table routes 0.0.0.0/0 to an internet
		// or NAT gateway — including when no route table exists at
		// all, since then no internet route exists either.
		for _, t := range resourcesOfKind(st, "route-table") {
			for _, route := range routesOf(t) {
				dest := route["destination"]
				if dest != "0.0.0.0/0" && dest != "" {
					continue
				}
				target := strings.ToLower(routeTarget(route))
				if strings.Contains(target, "igw") || strings.Contains(target, "nat") ||
					strings.Contains(target, "internet") || strings.Contains(target, "gateway") {
					return fail(r.Rule, "route table %s routes 0.0.0.0/0 to %s", t.Name, routeTarget(route))
				}
			}
		}
		return pass(r.Rule, "no route table routes 0.0.0.0/0 to the internet")
	case "load_balancer_healthy_targets":
		want, ok := paramInt(r.Params, "min_count", "count")
		if !ok {
			want = 1
		}
		lbs := resourcesOfKind(st, "loadbalancer")
		if len(lbs) == 0 {
			if !st.EngineAvailable {
				return unknown(r.Rule, "no load balancer in the lab record and the network engine is unreachable, so load-balancer state cannot be confirmed")
			}
			return fail(r.Rule, "no load balancer exists in this lab")
		}
		healthy := 0
		for _, lb := range lbs {
			if n, ok := paramInt(lb.Config, "healthy_targets", "healthy_target_count"); ok {
				healthy += n
			}
		}
		if healthy >= want {
			return pass(r.Rule, "%d healthy load-balancer target(s) (need %d)", healthy, want)
		}
		return fail(r.Rule, "%d healthy load-balancer target(s) (need %d); healthy_targets is recorded in the load balancer resource config", healthy, want)
	case "instance_reachable":
		// Real-state check on the recorded instance: it must be
		// running and have an address (or an explicit
		// reachable=true in config). There is no live in-VPC probe
		// in this control plane, so a running instance with no
		// recorded address fails rather than passing on a guess.
		var running, withAddr int
		for _, inst := range resourcesOfKind(st, "instance") {
			if inst.State != "running" && inst.State != "RUNNING" {
				continue
			}
			running++
			if inst.PrivateIP != "" || inst.PublicIP != "" {
				withAddr++
				continue
			}
			if v, ok := inst.Config["reachable"]; ok {
				if b, ok := v.(bool); ok && b {
					withAddr++
				}
			}
		}
		if running == 0 {
			if !st.HostAgentChecked {
				return unknown(r.Rule, "no running instance in the lab record and the host agent was not reached, so instance state cannot be confirmed")
			}
			return fail(r.Rule, "no instance is running in this lab")
		}
		if withAddr > 0 {
			return pass(r.Rule, "%d running instance(s) with a recorded address (%d running in total)", withAddr, running)
		}
		return fail(r.Rule, "%d instance(s) running but none has a recorded address, so reachability cannot be confirmed", running)
	case "dns_resolves":
		name, _ := paramString(r.Params, "name", "hostname")
		if name == "" {
			name = "(no name given)"
		}
		// Real-state check, in order: (1) a VPC resource whose
		// config records DNS state (the learner's fix is recorded
		// as lab state — enabled passes, explicitly disabled
		// fails); (2) the control-plane resolver for public names
		// only. Internal names (example.internal, *.local, …) are
		// never asked of the host resolver — a host answer for
		// .internal would fake a pass for a broken VPC resolver.
		// With no DNS source at all (no VPC DNS config and the
		// engine unreachable) the check is unknown, never a pass.
		dnsConfigured, dnsEnabled := false, false
		for _, v := range resourcesOfKind(st, "vpc") {
			for _, k := range []string{"dns_enabled", "dns_hostnames"} {
				if val, ok := v.Config[k]; ok {
					dnsConfigured = true
					if b, ok := val.(bool); ok && b {
						dnsEnabled = true
					}
				}
			}
		}
		if dnsEnabled {
			return pass(r.Rule, "VPC records DNS as enabled, so %s resolves", name)
		}
		if dnsConfigured {
			return fail(r.Rule, "VPC records DNS as disabled, so %s does not resolve", name)
		}
		if !isInternalName(name) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := net.DefaultResolver.LookupHost(ctx, name); err == nil {
				return pass(r.Rule, "%s resolves via the control-plane resolver (not an in-VPC lookup)", name)
			}
		}
		if !st.EngineAvailable {
			return unknown(r.Rule, "no VPC records DNS state and the network engine is unreachable, so DNS state cannot be confirmed")
		}
		return fail(r.Rule, "%s does not resolve (no VPC records DNS as enabled and the control-plane resolver cannot resolve it)", name)
	case "metric_below":
		metric, _ := paramString(r.Params, "metric")
		threshold, ok := paramFloat(r.Params, "threshold")
		if !ok {
			return unknown(r.Rule, "metric_below needs a numeric threshold parameter")
		}
		// No live metrics pipeline exists in this control plane:
		// the only source is metrics recorded in a lab resource's
		// config (config.metrics.<name>). Absent that, unknown —
		// never a faked pass.
		for _, res := range st.Resources {
			if res.Config == nil {
				continue
			}
			raw, ok := res.Config["metrics"]
			if !ok {
				continue
			}
			metrics, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			v, ok := metrics[metric]
			if !ok {
				continue
			}
			switch n := v.(type) {
			case float64:
				if n < threshold {
					return pass(r.Rule, "metric %s is %v, below threshold %v", metric, n, threshold)
				}
				return fail(r.Rule, "metric %s is %v, not below threshold %v", metric, n, threshold)
			case int:
				if float64(n) < threshold {
					return pass(r.Rule, "metric %s is %v, below threshold %v", metric, n, threshold)
				}
				return fail(r.Rule, "metric %s is %v, not below threshold %v", metric, n, threshold)
			}
		}
		return unknown(r.Rule, "no metrics are recorded for this lab (no metrics pipeline is wired), so %s cannot be checked", metricOrAny(metric))
	default:
		return unknown(r.Rule, "rule %q is not implemented by this validator (no data source for it)", r.Rule)
	}
}

func metricOrAny(s string) string {
	if s == "" {
		return "(no metric given)"
	}
	return s
}

// isInternalName reports whether a hostname belongs to a private /
// VPC-internal namespace that a public resolver must never be
// asked about.
func isInternalName(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	if !strings.Contains(n, ".") {
		return true
	}
	for _, suffix := range []string{".internal", ".local", ".lan", ".home", ".intranet", ".corp"} {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

func paramFloat(p map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			switch n := v.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case string:
				if f, err := strconv.ParseFloat(n, 64); err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

func sourceOrAny(s string) string {
	if s == "" {
		return "any source"
	}
	return s
}

// DefaultRules returns the definition's default rule set (used when
// POST /validate carries no explicit rules body).
func DefaultRules(definitionID string) []Rule {
	switch definitionID {
	case "vpc-fundamentals":
		return []Rule{
			{Rule: "vpc_exists", Description: "A VPC exists in the lab"},
			{Rule: "subnet_count", Params: map[string]any{"operator": ">=", "value": 2}, Description: "At least two subnets exist"},
			{Rule: "route_to_igw", Description: "A public subnet routes 0.0.0.0/0 to the internet gateway"},
			{Rule: "security_group_allows_ssh", Description: "A security group allows SSH (port 22)"},
			{Rule: "nat_exists", Description: "A NAT gateway exists"},
		}
	case "three-tier-app":
		return []Rule{
			{Rule: "vpc_exists", Description: "A VPC exists in the lab"},
			{Rule: "subnet_count", Params: map[string]any{"operator": ">=", "value": 2}, Description: "At least two subnets exist"},
			{Rule: "route_to_igw", Description: "A public subnet routes 0.0.0.0/0 to the internet gateway"},
			{Rule: "instance_running", Params: map[string]any{"operator": ">=", "count": 1}, Description: "At least one instance is running"},
		}
	case "ec2-basics":
		return []Rule{
			{Rule: "vpc_exists", Description: "A VPC exists in the lab"},
			{Rule: "instance_running", Params: map[string]any{"operator": ">=", "count": 1}, Description: "At least one instance is running"},
		}
	default:
		return []Rule{
			{Rule: "lab_exists", Description: "The lab session exists"},
			{Rule: "vpc_exists", Description: "A VPC exists in the lab"},
		}
	}
}
