// Package networkclient is the typed Go gRPC client for the Rust
// network engine (Milestone 4).
//
// The client is generated from proto/cloudforge/v1/*.proto with
// protoc 27.3 + protoc-gen-go v1.36.12 + protoc-gen-go-grpc 1.6.2
// (see backend/internal/networkclient/gen/ and backend/README.md for
// the regeneration command). Every RPC carries RequestMeta
// (lab_id, tenant_id, request_id, idempotency_key) and the
// x-request-id gRPC metadata header so a call can be correlated
// across the Go control plane and the Rust engine logs.
//
// Health is a real gRPC Health RPC with a 2s timeout — never a TCP
// dial. Reachable reports the gRPC Health result and is what lab
// provisioning gates on. TCPReachable is the only TCP probe left and
// exists solely so /api/v1/health can still report the legacy
// network_engine_reachable boolean when the gRPC dial itself fails;
// it must never be treated as engine health.
package networkclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

// DefaultAddr is the default Rust network engine address.
const DefaultAddr = "localhost:50051"

// ProbeTimeout bounds the gRPC Health RPC (and the TCP fallback probe).
const ProbeTimeout = 2 * time.Second

// DefaultRPCTimeout bounds every non-Health RPC when the caller's
// context carries no deadline of its own.
const DefaultRPCTimeout = 30 * time.Second

type ctxKey string

const requestIDKey ctxKey = "request_id"

// ContextWithRequestID attaches a correlation ID to ctx; Health and
// every RPC send it as the x-request-id metadata header.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok && v != "" {
		return v
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + time.Now().UTC().Format("20060102150405")
	}
	return hex.EncodeToString(b)
}

// Meta builds the RequestMeta every mutating RPC requires.
func Meta(labID, tenantID, requestID, idempotencyKey string) *gen.RequestMeta {
	return &gen.RequestMeta{
		LabId:          labID,
		TenantId:       tenantID,
		RequestId:      requestID,
		IdempotencyKey: idempotencyKey,
	}
}

// EngineTokenMetadata is the gRPC metadata key carrying the
// shared-secret engine token (Phase 7, risk R1). The Rust engine
// rejects every RPC without a matching token (Unauthenticated);
// the old unauthenticated path no longer exists.
const EngineTokenMetadata = "x-cloudforge-engine-token"

// Client is a lazily-dialled gRPC client for the network engine.
// It is safe for concurrent use. Close releases the connection.
type Client struct {
	Addr string
	// Token is the shared-secret engine token sent on every RPC
	// (empty = send none; the engine then refuses the call).
	Token string

	mu   sync.Mutex
	conn *grpc.ClientConn
	stub gen.NetworkEngineClient
}

// New returns a Client for addr (DefaultAddr when empty). No network
// I/O happens here; the connection is established on first use.
func New(addr string) *Client {
	if addr == "" {
		addr = DefaultAddr
	}
	return &Client{Addr: addr}
}

// NewWithToken returns a Client that authenticates every RPC with
// token (Phase 7 engine auth).
func NewWithToken(addr, token string) *Client {
	c := New(addr)
	c.Token = token
	return c
}

// Close closes the underlying gRPC connection, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn, c.stub = nil, nil
	return conn.Close()
}

func (c *Client) client() (gen.NetworkEngineClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stub != nil {
		return c.stub, nil
	}
	conn, err := grpc.NewClient(c.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.stub = gen.NewNetworkEngineClient(conn)
	return c.stub, nil
}

// withMeta attaches the x-request-id header (and the engine
// auth token, when configured) to ctx.
func (c *Client) withMeta(ctx context.Context) context.Context {
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", requestIDFrom(ctx))
	if c.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, EngineTokenMetadata, c.Token)
	}
	return ctx
}

// withTimeout applies d unless ctx already carries a deadline.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// --- errors -------------------------------------------------------------

// EngineError is a structured network-engine failure: the gRPC status
// code plus the engine's stable machine-readable error_code (parsed
// from the "CODE: message" prefix the Rust server emits, e.g.
// ROUTE_CONFLICT, BACKEND_UNAVAILABLE, CIDR_OVERLAP).
type EngineError struct {
	Code       codes.Code // gRPC status code
	EngineCode string     // stable code from the engine, "" when absent
	Message    string     // full server message
}

func (e *EngineError) Error() string { return e.Message }

// Unwrap exposes the underlying gRPC status error for errors.As.
func (e *EngineError) Unwrap() error {
	return status.Error(e.Code, e.Message)
}

// AsEngineError extracts an *EngineError from err, if it is one.
func AsEngineError(err error) (*EngineError, bool) {
	var ee *EngineError
	if errors.As(err, &ee) {
		return ee, true
	}
	return nil, false
}

// ErrorCode returns the engine's stable error code for err
// ("" when the error carries none).
func ErrorCode(err error) string {
	if ee, ok := AsEngineError(err); ok {
		return ee.EngineCode
	}
	return ""
}

// IsBackendUnavailable reports whether err means the engine's backend
// cannot perform the operation (gRPC Unavailable, e.g. fault injection
// on the real netlink backend, which returns BACKEND_UNAVAILABLE).
func IsBackendUnavailable(err error) bool {
	if ee, ok := AsEngineError(err); ok {
		return ee.Code == codes.Unavailable || ee.EngineCode == "BACKEND_UNAVAILABLE"
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.Unavailable
	}
	return false
}

// IsRouteConflict reports a ROUTE_CONFLICT failure (same destination,
// different next hop): gRPC FailedPrecondition.
func IsRouteConflict(err error) bool {
	if ee, ok := AsEngineError(err); ok {
		return ee.Code == codes.FailedPrecondition && ee.EngineCode == "ROUTE_CONFLICT"
	}
	return false
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	msg := st.Message()
	code := ""
	if i := strings.Index(msg, ": "); i > 0 {
		if cand := msg[:i]; cand == strings.ToUpper(cand) && !strings.Contains(cand, " ") {
			code = cand
		}
	}
	return &EngineError{Code: st.Code(), EngineCode: code, Message: msg}
}

// --- health -------------------------------------------------------------

// HealthStatus is the result of a gRPC Health RPC.
type HealthStatus struct {
	OK            bool
	Service       string
	Version       string
	Detail        string // carries backend=SIMULATED | backend=NETLINK
	UptimeSeconds int64
	GRPC          bool // true when the Health RPC itself succeeded
}

// Health performs the real gRPC Health RPC with a 2s timeout and the
// x-request-id metadata header. A nil error means the engine answered
// the RPC; check st.OK for the engine's own health verdict.
func (c *Client) Health(ctx context.Context) (HealthStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	stub, err := c.client()
	if err != nil {
		return HealthStatus{}, wrapErr(err)
	}
	resp, err := stub.Health(c.withMeta(ctx), &gen.HealthRequest{})
	if err != nil {
		return HealthStatus{}, wrapErr(err)
	}
	return HealthStatus{
		OK: resp.GetOk(), Service: resp.GetService(), Version: resp.GetVersion(),
		Detail: resp.GetDetail(), UptimeSeconds: resp.GetUptimeSeconds(), GRPC: true,
	}, nil
}

// Reachable reports whether the engine answers the gRPC Health RPC.
// This is the honest health signal lab provisioning gates on: true
// only when the Health RPC succeeds. detail explains what was (not)
// verified; it never claims a bare TCP connect is engine health.
func (c *Client) Reachable(ctx context.Context) (bool, string) {
	st, err := c.Health(ctx)
	if err != nil {
		return false, "network engine gRPC Health failed at " + c.Addr + ": " + err.Error()
	}
	if !st.OK {
		return false, "network engine answered gRPC Health but reports not-ok: " + st.Detail
	}
	return true, "gRPC Health ok (" + st.Detail + ")"
}

// TCPReachable is the clearly-labelled TCP fallback: it reports only
// whether something is listening on the engine address. It exists
// solely for /api/v1/health's legacy network_engine_reachable field
// when the gRPC dial itself fails, and must never gate provisioning
// or be presented as engine health.
func (c *Client) TCPReachable(ctx context.Context) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return false, "tcp connect to " + c.Addr + " failed: " + err.Error()
	}
	_ = conn.Close()
	return true, "tcp connect to " + c.Addr + " succeeded; this proves only that a listener exists, not gRPC Health"
}

// --- RPC wrappers -------------------------------------------------------
// Each wrapper applies DefaultRPCTimeout (unless the caller set a
// deadline), attaches x-request-id metadata, and translates failures
// into *EngineError with the stable engine error code.

func (c *Client) call(ctx context.Context, fn func(gen.NetworkEngineClient, context.Context) error) error {
	ctx, cancel := withTimeout(ctx, DefaultRPCTimeout)
	defer cancel()
	stub, err := c.client()
	if err != nil {
		return wrapErr(err)
	}
	return wrapErr(fn(stub, c.withMeta(ctx)))
}

// CreateNetwork creates a network (lab) in the engine.
func (c *Client) CreateNetwork(ctx context.Context, req *gen.CreateNetworkRequest) (*gen.CreateNetworkResponse, error) {
	var out *gen.CreateNetworkResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateNetwork(ctx, req)
		return err
	})
	return out, err
}

// DeleteNetwork deletes a network.
func (c *Client) DeleteNetwork(ctx context.Context, req *gen.DeleteNetworkRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.DeleteNetwork(ctx, req)
		return err
	})
	return out, err
}

// CreateNamespace creates a network namespace.
func (c *Client) CreateNamespace(ctx context.Context, req *gen.CreateNamespaceRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateNamespace(ctx, req)
		return err
	})
	return out, err
}

// DeleteNamespace deletes a network namespace.
func (c *Client) DeleteNamespace(ctx context.Context, req *gen.DeleteNamespaceRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.DeleteNamespace(ctx, req)
		return err
	})
	return out, err
}

// CreateBridge creates a bridge, optionally inside a namespace.
func (c *Client) CreateBridge(ctx context.Context, req *gen.CreateBridgeRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateBridge(ctx, req)
		return err
	})
	return out, err
}

// AllocateAddress allocates an address from a network's CIDR.
func (c *Client) AllocateAddress(ctx context.Context, req *gen.AllocateAddressRequest) (*gen.AllocateAddressResponse, error) {
	var out *gen.AllocateAddressResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.AllocateAddress(ctx, req)
		return err
	})
	return out, err
}

// ReleaseAddress releases a previously allocated address.
func (c *Client) ReleaseAddress(ctx context.Context, req *gen.ReleaseAddressRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.ReleaseAddress(ctx, req)
		return err
	})
	return out, err
}

// InspectTopology returns the engine's topology nodes for a network.
func (c *Client) InspectTopology(ctx context.Context, req *gen.InspectTopologyRequest) (*gen.InspectTopologyResponse, error) {
	var out *gen.InspectTopologyResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.InspectTopology(ctx, req)
		return err
	})
	return out, err
}

// ValidateNetwork validates a CIDR against existing CIDRs.
func (c *Client) ValidateNetwork(ctx context.Context, req *gen.ValidateNetworkRequest) (*gen.ValidateNetworkResponse, error) {
	var out *gen.ValidateNetworkResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.ValidateNetwork(ctx, req)
		return err
	})
	return out, err
}

// CleanupLab removes all of a lab's engine state.
func (c *Client) CleanupLab(ctx context.Context, req *gen.CleanupLabRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CleanupLab(ctx, req)
		return err
	})
	return out, err
}

// --- Milestone 4 RPCs ---------------------------------------------------

// CreateSubnet carves a subnet out of a network's CIDR.
func (c *Client) CreateSubnet(ctx context.Context, req *gen.CreateSubnetRequest) (*gen.CreateSubnetResponse, error) {
	var out *gen.CreateSubnetResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateSubnet(ctx, req)
		return err
	})
	return out, err
}

// DeleteSubnet deletes a subnet.
func (c *Client) DeleteSubnet(ctx context.Context, req *gen.DeleteSubnetRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.DeleteSubnet(ctx, req)
		return err
	})
	return out, err
}

// CreateVethPair creates a veth pair between two namespaces.
func (c *Client) CreateVethPair(ctx context.Context, req *gen.CreateVethPairRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateVethPair(ctx, req)
		return err
	})
	return out, err
}

// AttachInterface binds one peer of a veth pair to a bridge/namespace.
func (c *Client) AttachInterface(ctx context.Context, req *gen.AttachInterfaceRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.AttachInterface(ctx, req)
		return err
	})
	return out, err
}

// AddRoute adds a static route. A same-destination/different-next-hop
// conflict surfaces as FailedPrecondition / ROUTE_CONFLICT
// (see IsRouteConflict).
func (c *Client) AddRoute(ctx context.Context, req *gen.AddRouteRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.AddRoute(ctx, req)
		return err
	})
	return out, err
}

// DeleteRoute deletes a static route.
func (c *Client) DeleteRoute(ctx context.Context, req *gen.DeleteRouteRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.DeleteRoute(ctx, req)
		return err
	})
	return out, err
}

// CreateFirewallPolicy creates a security-group policy.
func (c *Client) CreateFirewallPolicy(ctx context.Context, req *gen.CreateFirewallPolicyRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateFirewallPolicy(ctx, req)
		return err
	})
	return out, err
}

// ApplyFirewallPolicy applies a previously created policy.
func (c *Client) ApplyFirewallPolicy(ctx context.Context, req *gen.ApplyFirewallPolicyRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.ApplyFirewallPolicy(ctx, req)
		return err
	})
	return out, err
}

// CreateNAT creates an SNAT or DNAT rule.
func (c *Client) CreateNAT(ctx context.Context, req *gen.CreateNATRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CreateNAT(ctx, req)
		return err
	})
	return out, err
}

// DeleteNAT deletes a NAT rule.
func (c *Client) DeleteNAT(ctx context.Context, req *gen.DeleteNATRequest) (*gen.OperationResult, error) {
	var out *gen.OperationResult
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.DeleteNAT(ctx, req)
		return err
	})
	return out, err
}

// InjectFault injects a fault into a lab. On the real netlink backend
// the engine returns Unavailable / BACKEND_UNAVAILABLE (tc/netem is
// not wired in M4); see IsBackendUnavailable.
func (c *Client) InjectFault(ctx context.Context, req *gen.InjectFaultRequest) (*gen.InjectFaultResponse, error) {
	var out *gen.InjectFaultResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.InjectFault(ctx, req)
		return err
	})
	return out, err
}

// CollectTelemetry returns the engine's per-lab telemetry counters.
func (c *Client) CollectTelemetry(ctx context.Context, req *gen.CollectTelemetryRequest) (*gen.CollectTelemetryResponse, error) {
	var out *gen.CollectTelemetryResponse
	err := c.call(ctx, func(stub gen.NetworkEngineClient, ctx context.Context) error {
		var err error
		out, err = stub.CollectTelemetry(ctx, req)
		return err
	})
	return out, err
}
