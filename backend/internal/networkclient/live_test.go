package networkclient

import (
	"context"
	"os"
	"testing"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

// TestLiveAgainstRealEngine is a MANUAL verification test: it dials a
// real Rust network engine (NETWORK_ENGINE_ADDR, default
// 127.0.0.1:50099) and exercises Health, CreateNetwork,
// AllocateAddress, CreateSubnet, route conflict, a firewall-policy
// round-trip, NAT, fault injection, telemetry and cleanup. It is
// skipped unless CLOUDFORGE_LIVE_ENGINE=1, so the regular suite never
// depends on a running engine.
func TestLiveAgainstRealEngine(t *testing.T) {
	if os.Getenv("CLOUDFORGE_LIVE_ENGINE") != "1" {
		t.Skip("set CLOUDFORGE_LIVE_ENGINE=1 with a real engine running")
	}
	addr := os.Getenv("NETWORK_ENGINE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:50099"
	}
	c := New(addr)
	defer c.Close()
	ctx := context.Background()

	st, err := c.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	t.Logf("Health: ok=%v service=%s version=%s detail=%q uptime=%d", st.OK, st.Service, st.Version, st.Detail, st.UptimeSeconds)
	if !st.OK {
		t.Fatalf("engine reports not-ok")
	}
	ok, detail := c.Reachable(ctx)
	if !ok {
		t.Fatalf("Reachable: %s", detail)
	}

	meta := Meta("live-lab-1", "tenant-1", "live-req-1", "live-idem-1")
	created, err := c.CreateNetwork(ctx, &gen.CreateNetworkRequest{Meta: meta, NetworkId: "live-net", Name: "live-net", Cidr: "10.20.0.0/16"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	t.Logf("CreateNetwork: %+v", created)

	alloc, err := c.AllocateAddress(ctx, &gen.AllocateAddressRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-2", "live-idem-2"), NetworkId: "live-net", Cidr: "10.20.0.0/16", Hint: "10.20.0.5"})
	if err != nil {
		t.Fatalf("AllocateAddress: %v", err)
	}
	t.Logf("AllocateAddress: address=%s cidr=%s", alloc.GetAddress(), alloc.GetCidr())
	if alloc.GetAddress() != "10.20.0.5" {
		t.Fatalf("hint not honoured: %s", alloc.GetAddress())
	}

	sub, err := c.CreateSubnet(ctx, &gen.CreateSubnetRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-3", "live-idem-3"), SubnetId: "live-sub", NetworkId: "live-net", Name: "a", Cidr: "10.20.1.0/24"})
	if err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}
	t.Logf("CreateSubnet: %+v", sub.GetSubnet())

	// Route conflict: same destination, different gateway must fail
	// with ROUTE_CONFLICT.
	_, err = c.AddRoute(ctx, &gen.AddRouteRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-4", "live-idem-4"), RouteId: "live-r1", Destination: "0.0.0.0/0", Gateway: "10.20.0.1", Interface: "eth0"})
	if err != nil {
		t.Fatalf("AddRoute 1: %v", err)
	}
	_, err = c.AddRoute(ctx, &gen.AddRouteRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-5", "live-idem-5"), RouteId: "live-r2", Destination: "0.0.0.0/0", Gateway: "10.20.0.2", Interface: "eth0"})
	if err == nil || !IsRouteConflict(err) {
		t.Fatalf("conflicting route: err=%v, want ROUTE_CONFLICT", err)
	}
	t.Logf("Route conflict correctly reported: %v", err)

	rule := &gen.FirewallRule{RuleId: "live-rule-1", Direction: gen.FirewallDirection_FIREWALL_DIRECTION_INGRESS, Protocol: gen.FirewallProtocol_FIREWALL_PROTOCOL_TCP, PortStart: 22, PortEnd: 22, Cidr: "0.0.0.0/0", Action: gen.FirewallAction_FIREWALL_ACTION_ALLOW, Priority: 10}
	if _, err := c.CreateFirewallPolicy(ctx, &gen.CreateFirewallPolicyRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-6", "live-idem-6"), PolicyId: "live-pol", NetworkId: "live-net", Name: "ssh", Rules: []*gen.FirewallRule{rule}}); err != nil {
		t.Fatalf("CreateFirewallPolicy: %v", err)
	}
	if _, err := c.ApplyFirewallPolicy(ctx, &gen.ApplyFirewallPolicyRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-7", "live-idem-7"), PolicyId: "live-pol"}); err != nil {
		t.Fatalf("ApplyFirewallPolicy: %v", err)
	}
	t.Logf("firewall policy created + applied")

	tel, err := c.CollectTelemetry(ctx, &gen.CollectTelemetryRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-8", "live-idem-8")})
	if err != nil {
		t.Fatalf("CollectTelemetry: %v", err)
	}
	t.Logf("telemetry points: %d", len(tel.GetPoints()))
	for _, p := range tel.GetPoints() {
		t.Logf("  %s = %v labels=%v", p.GetName(), p.GetValue(), p.GetLabels())
	}
	if len(tel.GetPoints()) == 0 {
		t.Fatal("expected telemetry points after live operations")
	}

	// NAT round-trip.
	if _, err := c.CreateNAT(ctx, &gen.CreateNATRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-9", "live-idem-9"), NatId: "live-nat", Kind: gen.NatKind_NAT_KIND_SNAT, SourceCidr: "10.20.0.0/16", TranslatedAddress: "203.0.113.1", Interface: "eth0"}); err != nil {
		t.Fatalf("CreateNAT: %v", err)
	}
	if _, err := c.DeleteNAT(ctx, &gen.DeleteNATRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-10", "live-idem-10"), NatId: "live-nat"}); err != nil {
		t.Fatalf("DeleteNAT: %v", err)
	}
	t.Logf("NAT create/delete ok")

	// Fault injection on the simulated backend records the fault.
	fault, err := c.InjectFault(ctx, &gen.InjectFaultRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-11", "live-idem-11"), Fault: &gen.FaultSpec{FaultId: "live-fault", Kind: gen.FaultKind_FAULT_KIND_LATENCY, Magnitude: 100, DurationSeconds: 60, Target: "live-net"}})
	if err != nil {
		t.Fatalf("InjectFault: %v", err)
	}
	t.Logf("InjectFault: fault_id=%s expires_at=%d", fault.GetFaultId(), fault.GetExpiresAtUnixSeconds())

	// Cleanup.
	if _, err := c.CleanupLab(ctx, &gen.CleanupLabRequest{Meta: Meta("live-lab-1", "tenant-1", "live-req-12", "live-idem-12")}); err != nil {
		t.Fatalf("CleanupLab: %v", err)
	}
	t.Logf("CleanupLab ok")
}
