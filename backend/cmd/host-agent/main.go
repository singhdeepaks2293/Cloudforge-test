// Command host-agent is the CloudForge host agent (Milestone 3).
//
// It exposes the typed, allowlisted host-agent API on :8082 by default:
// POST /v1/ops, GET /v1/inventory, GET /healthz (see
// internal/hostagent/server.go). It is the only privileged boundary on
// a host: the Docker socket and libvirt are never exposed to the API
// container, and this agent accepts only typed, authorized operations.
//
// HOST_AGENT_MODE selects the backend: simulated (default, in-memory),
// docker, libvirt, or auto. In docker/libvirt mode a missing backend
// is reported as Unavailable by the operations — the process still
// starts and serves /healthz so the failure is observable, never faked.
// HOST_AGENT_TOKEN is the shared bearer token; it is required in
// non-simulated modes and a dev default (with a loud warning) is used
// in simulated mode when unset. M9 upgrades auth to mTLS.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/config"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	mode := cfg.HostAgentMode
	agent, err := hostagent.NewAgent(mode)
	if err != nil {
		logger.Error("invalid host agent mode", "error", err)
		os.Exit(1)
	}
	// Log what the mode actually resolved to (auto may pick simulated).
	logger.Info("host agent backend selected", "configured_mode", mode, "mode", agent.Mode())
	if agent.Mode() != mode && mode != "" {
		logger.Warn("host agent mode differs from configured mode", "configured_mode", mode, "mode", agent.Mode())
	}

	token := cfg.HostAgentToken
	if token == "" {
		if agent.Mode() != "simulated" {
			logger.Error("HOST_AGENT_TOKEN is required in non-simulated mode; generate one and set it (never use the dev default on a real host)")
			os.Exit(1)
		}
		token = hostagent.DevToken
		logger.Warn("HOST_AGENT_TOKEN is not set; using insecure dev default in simulated mode — set it before any shared deployment")
	}

	// In docker mode, probe availability once at startup (read-only
	// ListContainers) so the operator sees the honest state in the
	// logs; operations keep reporting Unavailable per call either way.
	// Libvirt has no read-only backend probe that is side-effect free
	// (even listing is local state), so only the socket's presence is
	// logged for it; VM ops report Unavailable when it is absent.
	if agent.Mode() == "docker" {
		_, perr := agent.RunOp(context.Background(), hostagent.OpRequest{Op: hostagent.OpListContainers, LabID: "startup-probe"})
		if perr != nil && hostagent.IsUnavailable(perr) {
			logger.Warn("Docker unavailable; container operations will return Unavailable (not fake success)", "error", perr)
		} else if perr != nil {
			logger.Warn("Docker backend probe failed", "error", perr)
		} else {
			logger.Info("Docker backend reachable")
		}
	}
	if agent.Mode() == "libvirt" {
		if _, err := os.Stat(hostagent.DefaultLibvirtSocket); err != nil {
			logger.Warn("libvirt socket not present; VM operations will return Unavailable (not fake success)", "socket", hostagent.DefaultLibvirtSocket, "error", err)
		} else {
			logger.Info("libvirt socket present", "socket", hostagent.DefaultLibvirtSocket)
		}
	}

	srv := hostagent.NewServer(agent, token, logger)
	httpServer := &http.Server{
		Addr:              cfg.HostAgentAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("cloudforge host-agent listening", "addr", cfg.HostAgentAddr, "mode", agent.Mode(), "version", hostagent.Version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("host-agent server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	logger.Info("shutting down host-agent")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
