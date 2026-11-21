package validation

import (
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func res(kind, name string, cfg map[string]any) *store.LabResource {
	return &store.LabResource{Kind: kind, Name: name, State: "available", Config: cfg}
}

func fullState() State {
	return State{
		LabExists:       true,
		EngineAvailable: true,
		Resources: []*store.LabResource{
			res("vpc", "main", map[string]any{"cidr": "10.0.0.0/16"}),
			res("subnet", "public-a", map[string]any{"cidr": "10.0.1.0/24"}),
			res("subnet", "private-a", map[string]any{"cidr": "10.0.2.0/24"}),
			res("route-table", "public-rt", map[string]any{"routes": []any{
				map[string]any{"destination": "0.0.0.0/0", "target": "igw-123"},
			}}),
			res("security-group", "web-sg", map[string]any{"rules": []any{
				map[string]any{"port": float64(22), "source": "0.0.0.0/0"},
			}}),
			res("gateway", "nat-1", map[string]any{"gateway_type": "nat"}),
			{Kind: "instance", Name: "web", State: "running"},
		},
	}
}

func TestValidateFullLabPasses(t *testing.T) {
	result := Validate(fullState(), DefaultRules("vpc-fundamentals"))
	if !result.Passed {
		t.Fatalf("expected pass, got %+v", result.Checks)
	}
	if !result.XPEligible {
		t.Fatal("passing validation must be XP-eligible")
	}
	for _, c := range result.Checks {
		if c.Status != StatusPass {
			t.Fatalf("check %s: status %s (%s)", c.Rule, c.Status, c.Detail)
		}
		if c.RuleID != c.Rule || !c.Passed || c.Message == "" {
			t.Fatalf("check %s: frontend fields missing: %+v", c.Rule, c)
		}
	}
}

func TestValidateEmptyLabFailsHonestly(t *testing.T) {
	st := State{LabExists: true, EngineAvailable: true}
	result := Validate(st, DefaultRules("vpc-fundamentals"))
	if result.Passed {
		t.Fatal("empty lab must not pass")
	}
	for _, c := range result.Checks {
		if c.Status != StatusFail {
			t.Fatalf("check %s: expected fail with engine available, got %s (%s)", c.Rule, c.Status, c.Detail)
		}
	}
}

func TestValidateUnknownWhenEngineDown(t *testing.T) {
	st := State{LabExists: true, EngineAvailable: false}
	result := Validate(st, []Rule{{Rule: "vpc_exists"}, {Rule: "route_to_igw"}})
	if result.Passed {
		t.Fatal("unknown checks must not pass")
	}
	for _, c := range result.Checks {
		if c.Status != StatusUnknown {
			t.Fatalf("check %s: expected unknown when engine is down, got %s", c.Rule, c.Status)
		}
	}
}

func TestValidateSubnetCountOperator(t *testing.T) {
	st := fullState()
	if got := Validate(st, []Rule{{Rule: "subnet_count", Params: map[string]any{"operator": ">=", "value": 3}}}); got.Passed {
		t.Fatal("2 subnets must not satisfy >= 3")
	}
	if got := Validate(st, []Rule{{Rule: "subnet_count", Params: map[string]any{"operator": "==", "value": 2}}}); !got.Passed {
		t.Fatalf("2 subnets must satisfy == 2: %+v", got.Checks)
	}
}

func TestValidateInstanceRunningFromHostAgent(t *testing.T) {
	st := State{LabExists: true, HostAgentChecked: true,
		VMs: []VMState{{ID: "vm-1", Name: "web", State: "running"}}}
	if got := Validate(st, []Rule{{Rule: "instance_running", Params: map[string]any{"count": 1}}}); !got.Passed {
		t.Fatalf("running VM must pass: %+v", got.Checks)
	}
	st.VMs[0].State = "stopped"
	if got := Validate(st, []Rule{{Rule: "instance_running", Params: map[string]any{"count": 1}}}); got.Passed {
		t.Fatal("stopped VM must not pass instance_running")
	}
}

func TestValidateDNSIsUnknown(t *testing.T) {
	// M8: dns_resolves is a real-state check now (it was always
	// unknown in M6). It is unknown only when there is no DNS
	// source at all: no VPC records DNS state and the engine is
	// unreachable.
	st := State{LabExists: true, EngineAvailable: false}
	result := Validate(st, []Rule{{Rule: "dns_resolves", Params: map[string]any{"name": "example.internal"}}})
	if len(result.Checks) != 1 || result.Checks[0].Status != StatusUnknown {
		t.Fatalf("dns_resolves with no data source must be unknown: %+v", result.Checks)
	}
	if result.Passed {
		t.Fatal("unknown check must not pass overall")
	}
	// A lab observed by the engine with a VPC that records no DNS
	// state fails for an internal name instead of passing on a
	// host-resolver guess.
	result = Validate(fullState(), []Rule{{Rule: "dns_resolves", Params: map[string]any{"name": "example.internal"}}})
	if result.Checks[0].Status != StatusFail {
		t.Fatalf("dns_resolves without a DNS-enabled VPC must fail: %+v", result.Checks)
	}
	// A VPC recording DNS as enabled passes.
	st = fullState()
	st.Resources[0].Config["dns_enabled"] = true
	result = Validate(st, []Rule{{Rule: "dns_resolves", Params: map[string]any{"name": "example.internal"}}})
	if result.Checks[0].Status != StatusPass {
		t.Fatalf("dns_resolves with a DNS-enabled VPC must pass: %+v", result.Checks)
	}
}

func TestValidateUnknownRuleIsUnknown(t *testing.T) {
	result := Validate(fullState(), []Rule{{Rule: "bucket_exists", Params: map[string]any{"name": "x"}}})
	if result.Checks[0].Status != StatusUnknown {
		t.Fatalf("unimplemented rule must be unknown, got %+v", result.Checks[0])
	}
}

func TestValidateSecurityGroupDenies(t *testing.T) {
	st := fullState()
	// Remove the SSH rule: no SG allows port 22 anymore.
	for _, r := range st.Resources {
		if r.Kind == "security-group" {
			r.Config = map[string]any{"rules": []any{
				map[string]any{"port": float64(80), "source": "0.0.0.0/0"},
			}}
		}
	}
	result := Validate(st, []Rule{{Rule: "security_group_allows_ssh"}})
	if result.Checks[0].Status != StatusFail {
		t.Fatalf("SSH not allowed must fail: %+v", result.Checks[0])
	}
}
