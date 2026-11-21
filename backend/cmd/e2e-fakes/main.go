// Command e2e-fakes runs the test doubles the end-to-end acceptance
// test needs: a fake Rust network engine (in-memory gRPC
// NetworkEngineServer) and the simulated host agent (real HTTP
// server). It is test support only — never deployed.
//
// Usage:
//
//	e2e-fakes -engine-addr 127.0.0.1:50051 -host-agent-addr 127.0.0.1:18082
//
// The fake engine behaves like the real one where the API depends
// on it: CreateSubnet on a missing network returns NOT_FOUND (the
// API creates the network and retries once), InspectTopology on a
// missing network returns NOT_FOUND, InjectFault records the fault.
// Everything else is in-memory and deterministic.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"

	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
)

type fakeNetwork struct {
	cidr    string
	subnets map[string]*gen.Subnet
	faults  []string
}

type fakeEngine struct {
	gen.UnimplementedNetworkEngineServer
	mu       sync.Mutex
	networks map[string]*fakeNetwork
}

func okResult() *gen.OperationResult {
	return &gen.OperationResult{State: gen.OperationState_OPERATION_STATE_SUCCEEDED, Message: "ok"}
}

func (f *fakeEngine) Health(_ context.Context, _ *gen.HealthRequest) (*gen.HealthResponse, error) {
	return &gen.HealthResponse{Service: "network-engine", Version: "e2e-fake", Ok: true, Detail: "fake engine up"}, nil
}

func (f *fakeEngine) CreateNetwork(_ context.Context, req *gen.CreateNetworkRequest) (*gen.CreateNetworkResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.networks[req.GetNetworkId()]; !ok {
		f.networks[req.GetNetworkId()] = &fakeNetwork{cidr: req.GetCidr(), subnets: map[string]*gen.Subnet{}}
	}
	return &gen.CreateNetworkResponse{Result: okResult(), NetworkId: req.GetNetworkId(), Cidr: req.GetCidr()}, nil
}

func (f *fakeEngine) DeleteNetwork(_ context.Context, req *gen.DeleteNetworkRequest) (*gen.OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.networks, req.GetNetworkId())
	return okResult(), nil
}

func (f *fakeEngine) CreateSubnet(_ context.Context, req *gen.CreateSubnetRequest) (*gen.CreateSubnetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nw, ok := f.networks[req.GetNetworkId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "network "+req.GetNetworkId()+" not found")
	}
	sub := &gen.Subnet{SubnetId: req.GetSubnetId(), NetworkId: req.GetNetworkId(), Name: req.GetName(), Cidr: req.GetCidr()}
	nw.subnets[req.GetSubnetId()] = sub
	return &gen.CreateSubnetResponse{Result: okResult(), Subnet: sub}, nil
}

func (f *fakeEngine) DeleteSubnet(_ context.Context, req *gen.DeleteSubnetRequest) (*gen.OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nw := range f.networks {
		delete(nw.subnets, req.GetSubnetId())
	}
	return okResult(), nil
}

func (f *fakeEngine) InspectTopology(_ context.Context, req *gen.InspectTopologyRequest) (*gen.InspectTopologyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nw, ok := f.networks[req.GetNetworkId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "network "+req.GetNetworkId()+" not found")
	}
	nodes := []*gen.TopologyNode{{Id: req.GetNetworkId(), Kind: "network", Name: "lab-" + req.GetNetworkId()}}
	for _, s := range nw.subnets {
		nodes = append(nodes, &gen.TopologyNode{Id: s.GetSubnetId(), Kind: "subnet", Name: s.GetName(), Labels: map[string]string{"cidr": s.GetCidr()}})
	}
	return &gen.InspectTopologyResponse{Result: okResult(), Nodes: nodes}, nil
}

func (f *fakeEngine) InjectFault(_ context.Context, req *gen.InjectFaultRequest) (*gen.InjectFaultResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nw, ok := f.networks[req.GetMeta().GetLabId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "network "+req.GetMeta().GetLabId()+" not found")
	}
	nw.faults = append(nw.faults, req.GetFault().GetFaultId())
	return &gen.InjectFaultResponse{Result: okResult(), FaultId: req.GetFault().GetFaultId()}, nil
}

func (f *fakeEngine) CleanupLab(_ context.Context, req *gen.CleanupLabRequest) (*gen.OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.networks, req.GetMeta().GetLabId())
	return okResult(), nil
}

func main() {
	engineAddr := flag.String("engine-addr", "127.0.0.1:50051", "gRPC listen address for the fake network engine")
	haAddr := flag.String("host-agent-addr", "127.0.0.1:18082", "HTTP listen address for the simulated host agent")
	haToken := flag.String("host-agent-token", "e2e-token", "shared bearer <redacted> for the host-agent API")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	lis, err := net.Listen("tcp", *engineAddr)
	if err != nil {
		logger.Error("fake engine listen failed", "addr", *engineAddr, "error", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer()
	gen.RegisterNetworkEngineServer(grpcSrv, &fakeEngine{networks: map[string]*fakeNetwork{}})
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("fake engine serve failed", "error", err)
		}
	}()
	logger.Info("fake network engine listening", "addr", lis.Addr().String())

	agent := hostagent.NewSimulatedHostAgent()
	haSrv := &http.Server{Addr: *haAddr, Handler: hostagent.NewServer(agent, *haToken, logger).Routes()}
	go func() {
		if err := haSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("simulated host agent serve failed", "error", err)
		}
	}()
	logger.Info("simulated host agent listening", "addr", *haAddr)

	fmt.Println("e2e-fakes ready")

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()
	grpcSrv.GracefulStop()
	_ = haSrv.Shutdown(context.Background())
}
