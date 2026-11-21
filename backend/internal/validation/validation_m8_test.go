package validation_test

import (
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/validation"
)

func resWith(kind, name string, config map[string]any) *store.LabResource {
	return &store.LabResource{ID: name, Kind: kind, Name: name, State: "available", Config: config}
}

func TestNewRules(t *testing.T) {
	routeTable := resWith("route-table", "rtb", map[string]any{
		"routes": []any{map[string]any{"destination": "0.0.0.0/0", "target": "igw-1"}},
	})
	sg := resWith("security-group", "sg", map[string]any{
		"rules": []any{map[string]any{"port": 22, "source": "bastion"}},
	})
	lb := resWith("loadbalancer", "lb", map[string]any{"healthy_targets": 2})
	inst := &store.LabResource{ID: "i-1", Kind: "instance", Name: "app", State: "running", PrivateIP: "10.0.1.5"}
	vpcDNS := resWith("vpc", "vpc", map[string]any{"cidr": "10.0.0.0/16", "dns_enabled": true})
	st := validation.State{
		LabExists: true, EngineAvailable: true,
		Resources: []*store.LabResource{routeTable, sg, lb, inst, vpcDNS},
	}

	check := func(rule string, params map[string]any) validation.Check {
		res := validation.Validate(st, []validation.Rule{{Rule: rule, Params: params}})
		if len(res.Checks) != 1 {
			t.Fatalf("rule %s: %d checks", rule, len(res.Checks))
		}
		return res.Checks[0]
	}

	if c := check("route_to_igw", nil); c.Status != validation.StatusPass {
		t.Fatalf("route_to_igw: %v", c)
	}
	if c := check("no_route_to_internet", nil); c.Status != validation.StatusFail {
		t.Fatalf("no_route_to_internet with an IGW route should fail: %v", c)
	}
	if c := check("security_group_denies", map[string]any{"port": 22, "source": "0.0.0.0/0"}); c.Status != validation.StatusPass {
		t.Fatalf("security_group_denies 0.0.0.0/0: %v", c)
	}
	if c := check("security_group_denies", map[string]any{"port": 22, "source": "bastion"}); c.Status != validation.StatusFail {
		t.Fatalf("security_group_denies bastion should fail (SSH is allowed from bastion): %v", c)
	}
	if c := check("load_balancer_healthy_targets", map[string]any{"min_count": 1}); c.Status != validation.StatusPass {
		t.Fatalf("load_balancer_healthy_targets: %v", c)
	}
	if c := check("load_balancer_healthy_targets", map[string]any{"min_count": 5}); c.Status != validation.StatusFail {
		t.Fatalf("load_balancer_healthy_targets min 5 should fail: %v", c)
	}
	if c := check("instance_reachable", map[string]any{"protocol": "ssh", "port": 22}); c.Status != validation.StatusPass {
		t.Fatalf("instance_reachable: %v", c)
	}
	if c := check("dns_resolves", map[string]any{"name": "example.internal"}); c.Status != validation.StatusPass {
		t.Fatalf("dns_resolves with dns_enabled VPC: %v", c)
	}

	// No internet route at all: no_route_to_internet passes.
	st2 := validation.State{LabExists: true, EngineAvailable: true,
		Resources: []*store.LabResource{resWith("route-table", "rtb2", map[string]any{
			"routes": []any{map[string]any{"destination": "10.0.0.0/16", "target": "local"}},
		})}}
	if res := validation.Validate(st2, []validation.Rule{{Rule: "no_route_to_internet"}}); !res.Passed {
		t.Fatalf("no_route_to_internet with only a local route should pass: %+v", res.Checks)
	}

	// metric_below: unknown without recorded metrics, pass/fail with them.
	if c := check("metric_below", map[string]any{"metric": "p99_latency_ms", "threshold": 500}); c.Status != validation.StatusUnknown {
		t.Fatalf("metric_below without a metrics source should be unknown: %v", c)
	}
	st3 := validation.State{LabExists: true, EngineAvailable: true,
		Resources: []*store.LabResource{resWith("instance", "app", map[string]any{
			"metrics": map[string]any{"p99_latency_ms": 120.0},
		})}}
	if res := validation.Validate(st3, []validation.Rule{{Rule: "metric_below", Params: map[string]any{"metric": "p99_latency_ms", "threshold": 500}}}); !res.Passed {
		t.Fatalf("metric_below 120 < 500 should pass: %+v", res.Checks)
	}
	st3.Resources[0].Config["metrics"] = map[string]any{"p99_latency_ms": 5000.0}
	if res := validation.Validate(st3, []validation.Rule{{Rule: "metric_below", Params: map[string]any{"metric": "p99_latency_ms", "threshold": 500}}}); res.Passed {
		t.Fatalf("metric_below 5000 < 500 should fail: %+v", res.Checks)
	}

	// dns_resolves with no VPC and no engine: unknown, never a pass.
	st4 := validation.State{LabExists: true, EngineAvailable: false}
	if res := validation.Validate(st4, []validation.Rule{{Rule: "dns_resolves", Params: map[string]any{"name": "example.internal"}}}); res.Checks[0].Status != validation.StatusUnknown {
		t.Fatalf("dns_resolves with no data source should be unknown: %+v", res.Checks)
	}
}
