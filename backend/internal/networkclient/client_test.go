package networkclient

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

// fakeEngine is an in-process NetworkEngine server for tests. It
// records the x-request-id metadata it saw and can be told to fail
// AddRoute with ROUTE_CONFLICT and InjectFault with
// BACKEND_UNAVAILABLE, mirroring the real engine's stable codes.
type fakeEngine struct {
	gen.UnimplementedNetworkEngineServer
	seenRequestIDs []string
	failRoute      bool
	failFault      bool
}

func (f *fakeEngine) note(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		f.seenRequestIDs = append(f.seenRequestIDs, md.Get("x-request-id")...)
	}
}

func (f *fakeEngine) Health(ctx context.Context, _ *gen.HealthRequest) (*gen.HealthResponse, error) {
	f.note(ctx)
	return &gen.HealthResponse{Service: "network-engine", Version: "0.4.0-m4", Ok: true, Detail: "backend=SIMULATED", UptimeSeconds: 42}, nil
}

func (f *fakeEngine) CreateNetwork(ctx context.Context, req *gen.CreateNetworkRequest) (*gen.CreateNetworkResponse, error) {
	f.note(ctx)
	return &gen.CreateNetworkResponse{
		Result:    &gen.OperationResult{OperationId: "op-1", State: gen.OperationState_OPERATION_STATE_SUCCEEDED},
		NetworkId: req.GetNetworkId(), Cidr: req.GetCidr(),
	}, nil
}

func (f *fakeEngine) AllocateAddress(ctx context.Context, req *gen.AllocateAddressRequest) (*gen.AllocateAddressResponse, error) {
	f.note(ctx)
	return &gen.AllocateAddressResponse{
		Result:  &gen.OperationResult{OperationId: "op-2", State: gen.OperationState_OPERATION_STATE_SUCCEEDED},
		Address: "10.10.0.5", Cidr: req.GetCidr(),
	}, nil
}

func (f *fakeEngine) CreateSubnet(ctx context.Context, req *gen.CreateSubnetRequest) (*gen.CreateSubnetResponse, error) {
	f.note(ctx)
	return &gen.CreateSubnetResponse{
		Result: &gen.OperationResult{OperationId: "op-3", State: gen.OperationState_OPERATION_STATE_SUCCEEDED},
		Subnet: &gen.Subnet{SubnetId: req.GetSubnetId(), NetworkId: req.GetNetworkId(), Name: req.GetName(), Cidr: req.GetCidr()},
	}, nil
}

func (f *fakeEngine) AddRoute(ctx context.Context, req *gen.AddRouteRequest) (*gen.OperationResult, error) {
	f.note(ctx)
	if f.failRoute {
		return nil, status.Error(codes.FailedPrecondition, "ROUTE_CONFLICT: route to "+req.GetDestination()+" conflicts with an existing next hop")
	}
	return &gen.OperationResult{OperationId: "op-4", State: gen.OperationState_OPERATION_STATE_SUCCEEDED}, nil
}

func (f *fakeEngine) CreateFirewallPolicy(ctx context.Context, req *gen.CreateFirewallPolicyRequest) (*gen.OperationResult, error) {
	f.note(ctx)
	if len(req.GetRules()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "INVALID_ARGUMENT: firewall policy requires at least one rule")
	}
	return &gen.OperationResult{OperationId: "op-5", State: gen.OperationState_OPERATION_STATE_SUCCEEDED}, nil
}

func (f *fakeEngine) ApplyFirewallPolicy(ctx context.Context, _ *gen.ApplyFirewallPolicyRequest) (*gen.OperationResult, error) {
	f.note(ctx)
	return &gen.OperationResult{OperationId: "op-6", State: gen.OperationState_OPERATION_STATE_SUCCEEDED}, nil
}

func (f *fakeEngine) CreateNAT(ctx context.Context, req *gen.CreateNATRequest) (*gen.OperationResult, error) {
	f.note(ctx)
	if req.GetKind() == gen.NatKind_NAT_KIND_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "INVALID_ARGUMENT: NAT kind is required")
	}
	return &gen.OperationResult{OperationId: "op-7", State: gen.OperationState_OPERATION_STATE_SUCCEEDED}, nil
}

func (f *fakeEngine) InjectFault(ctx context.Context, req *gen.InjectFaultRequest) (*gen.InjectFaultResponse, error) {
	f.note(ctx)
	if f.failFault {
		return nil, status.Error(codes.Unavailable, "BACKEND_UNAVAILABLE: fault injection is not enforced on this backend")
	}
	return &gen.InjectFaultResponse{
		Result:  &gen.OperationResult{OperationId: "op-8", State: gen.OperationState_OPERATION_STATE_SUCCEEDED},
		FaultId: req.GetFault().GetFaultId(), ExpiresAtUnixSeconds: 1700003600,
	}, nil
}

func (f *fakeEngine) CollectTelemetry(ctx context.Context, _ *gen.CollectTelemetryRequest) (*gen.CollectTelemetryResponse, error) {
	f.note(ctx)
	return &gen.CollectTelemetryResponse{
		Result: &gen.OperationResult{OperationId: "op-9", State: gen.OperationState_OPERATION_STATE_SUCCEEDED},
		Points: []*gen.TelemetryPoint{
			{Name: "operations_total", Value: 3, TimestampUnixSeconds: 1700000000, Labels: map[string]string{"op": "CreateNetwork"}},
			{Name: "subnets_created", Value: 1, TimestampUnixSeconds: 1700000000},
		},
	}, nil
}

// startFake spins up the fake engine on a bufconn listener and
// returns a Client dialled to it plus the fake for assertions.
func startFake(t *testing.T, fake *fakeEngine) (*Client, *fakeEngine) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	gen.RegisterNetworkEngineServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := New("bufnet")
	c.mu.Lock()
	c.conn = conn
	c.stub = gen.NewNetworkEngineClient(conn)
	c.mu.Unlock()
	return c, fake
}

func meta() *gen.RequestMeta {
	return Meta("lab-1", "tenant-1", "req-1", "idem-1")
}

func TestHealthRPC(t *testing.T) {
	c, fake := startFake(t, &fakeEngine{})
	st, err := c.Health(ContextWithRequestID(context.Background(), "req-health-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK || !st.GRPC {
		t.Fatalf("health = %+v, want ok+grpc", st)
	}
	if st.Service != "network-engine" || st.Detail != "backend=SIMULATED" {
		t.Fatalf("health = %+v, want service network-engine backend=SIMULATED", st)
	}
	if len(fake.seenRequestIDs) == 0 || fake.seenRequestIDs[0] != "req-health-1" {
		t.Fatalf("x-request-id metadata = %v, want [req-health-1]", fake.seenRequestIDs)
	}
}

func TestReachableUsesGRPCHealth(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	ok, detail := c.Reachable(context.Background())
	if !ok {
		t.Fatalf("reachable must be true against the fake engine: %s", detail)
	}
	if detail == "" {
		t.Fatal("detail must explain what was verified")
	}
}

func TestReachableFalseWhenEngineDown(t *testing.T) {
	c := New("127.0.0.1:1") // closed port: nothing can be listening
	ok, detail := c.Reachable(context.Background())
	if ok {
		t.Fatal("reachable must be false when the engine is down")
	}
	if detail == "" {
		t.Fatal("detail must explain the failure")
	}
}

func TestTCPReachableIsOnlyAListenerProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c := New(ln.Addr().String())
	ok, detail := c.TCPReachable(context.Background())
	if !ok {
		t.Fatal("TCP probe of a live listener must report reachable")
	}
	// The same listener is NOT gRPC health: Reachable must fail.
	grpcOK, _ := c.Reachable(context.Background())
	if grpcOK {
		t.Fatal("a bare TCP listener must not count as gRPC-reachable")
	}
	if !contains(detail, "only that a listener exists") {
		t.Fatalf("TCP probe detail must carry its limitation label, got %q", detail)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestM1RPCsRoundTrip(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	ctx := context.Background()
	created, err := c.CreateNetwork(ctx, &gen.CreateNetworkRequest{Meta: meta(), NetworkId: "net-1", Name: "lab-net", Cidr: "10.10.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetNetworkId() != "net-1" || created.GetCidr() != "10.10.0.0/16" {
		t.Fatalf("CreateNetwork = %+v", created)
	}
	alloc, err := c.AllocateAddress(ctx, &gen.AllocateAddressRequest{Meta: meta(), NetworkId: "net-1", Cidr: "10.10.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	if alloc.GetAddress() != "10.10.0.5" {
		t.Fatalf("AllocateAddress address = %q", alloc.GetAddress())
	}
}

func TestM4SubnetAndRoute(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	ctx := context.Background()
	sub, err := c.CreateSubnet(ctx, &gen.CreateSubnetRequest{Meta: meta(), SubnetId: "sub-1", NetworkId: "net-1", Name: "a", Cidr: "10.10.1.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.GetSubnet().GetCidr() != "10.10.1.0/24" {
		t.Fatalf("CreateSubnet subnet = %+v", sub.GetSubnet())
	}
	// Route conflict: FailedPrecondition + ROUTE_CONFLICT.
	c2, fake2 := startFake(t, &fakeEngine{failRoute: true})
	_, err = c2.AddRoute(ctx, &gen.AddRouteRequest{Meta: meta(), RouteId: "r-1", Destination: "0.0.0.0/0", Gateway: "10.10.0.1", Interface: "eth0"})
	if err == nil {
		t.Fatal("conflicting route must fail")
	}
	if !IsRouteConflict(err) {
		t.Fatalf("err = %v, want ROUTE_CONFLICT", err)
	}
	if got := ErrorCode(err); got != "ROUTE_CONFLICT" {
		t.Fatalf("ErrorCode = %q, want ROUTE_CONFLICT", got)
	}
	_ = fake2
}

func TestM4FirewallAndNAT(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	ctx := context.Background()
	rule := &gen.FirewallRule{
		RuleId: "rule-1", Direction: gen.FirewallDirection_FIREWALL_DIRECTION_INGRESS,
		Protocol: gen.FirewallProtocol_FIREWALL_PROTOCOL_TCP, PortStart: 22, PortEnd: 22,
		Cidr: "0.0.0.0/0", Action: gen.FirewallAction_FIREWALL_ACTION_ALLOW, Priority: 10,
	}
	_, err := c.CreateFirewallPolicy(ctx, &gen.CreateFirewallPolicyRequest{
		Meta: meta(), PolicyId: "pol-1", NetworkId: "net-1", Name: "ssh", Rules: []*gen.FirewallRule{rule},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ApplyFirewallPolicy(ctx, &gen.ApplyFirewallPolicyRequest{Meta: meta(), PolicyId: "pol-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateNAT(ctx, &gen.CreateNATRequest{
		Meta: meta(), NatId: "nat-1", Kind: gen.NatKind_NAT_KIND_SNAT,
		SourceCidr: "10.10.0.0/16", TranslatedAddress: "203.0.113.1", Interface: "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Unspecified NAT kind is rejected with the engine code.
	_, err = c.CreateNAT(ctx, &gen.CreateNATRequest{Meta: meta(), NatId: "nat-2", Kind: gen.NatKind_NAT_KIND_UNSPECIFIED})
	if err == nil || ErrorCode(err) != "INVALID_ARGUMENT" {
		t.Fatalf("err = %v, want INVALID_ARGUMENT", err)
	}
}

func TestM4FaultBackendUnavailable(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{failFault: true})
	_, err := c.InjectFault(context.Background(), &gen.InjectFaultRequest{
		Meta:  meta(),
		Fault: &gen.FaultSpec{FaultId: "f-1", Kind: gen.FaultKind_FAULT_KIND_LATENCY, Magnitude: 100, DurationSeconds: 60},
	})
	if err == nil {
		t.Fatal("fault injection on the unavailable backend must fail")
	}
	if !IsBackendUnavailable(err) {
		t.Fatalf("err = %v, want BACKEND_UNAVAILABLE", err)
	}
	if got := ErrorCode(err); got != "BACKEND_UNAVAILABLE" {
		t.Fatalf("ErrorCode = %q, want BACKEND_UNAVAILABLE", got)
	}
}

func TestM4Telemetry(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	resp, err := c.CollectTelemetry(context.Background(), &gen.CollectTelemetryRequest{Meta: meta()})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetPoints()) != 2 {
		t.Fatalf("points = %d, want 2", len(resp.GetPoints()))
	}
	if resp.GetPoints()[0].GetName() != "operations_total" || resp.GetPoints()[0].GetValue() != 3 {
		t.Fatalf("first point = %+v", resp.GetPoints()[0])
	}
}

func TestDefaultAddr(t *testing.T) {
	if New("").Addr != DefaultAddr {
		t.Fatalf("empty addr should default to %q", DefaultAddr)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c, _ := startFake(t, &fakeEngine{})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close must not fail: %v", err)
	}
}
