// Command worker is the CloudForge background-job worker (Milestone 2).
//
// It polls the durable jobs table (SELECT ... FOR UPDATE SKIP LOCKED),
// handles lab_provision / lab_destroy / lab_expire / audit_cleanup with
// bounded retries and dead-lettering, and sweeps expired labs.
// Without a reachable PostgreSQL it runs degraded: /healthz stays ok,
// /status reports database_connected=false and no jobs are processed.
// That degraded mode is explicit — it never pretends jobs ran.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/config"
	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/jobs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	// Phase 7 (R4): same fail-closed secret check as the API.
	if warns, err := cfg.ValidateSecrets(); err != nil {
		logger.Error("refusing to start: production secrets are not configured", "error", err)
		os.Exit(1)
	} else {
		for _, w := range warns {
			logger.Warn(w)
		}
	}
	logger.Info("cloudforge worker starting", "config", cfg.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, dbErr := database.Connect(ctx, cfg.DatabaseURL)
	cancel()

	var svc *jobs.Service
	dbOK := false
	if dbErr != nil {
		logger.Warn("PostgreSQL unavailable; worker running degraded (no jobs will be processed)", "error", dbErr)
	} else {
		defer db.Close()
		migCtx, migCancel := context.WithTimeout(context.Background(), 30*time.Second)
		applied, migErr := database.Migrate(migCtx, db, migrations.FS)
		migCancel()
		if migErr != nil {
			logger.Error("migration failed", "error", migErr)
			os.Exit(1)
		}
		if len(applied) > 0 {
			logger.Info("applied migrations", "migrations", applied)
		}
		netClient := networkclient.NewWithToken(cfg.NetworkEngineAddr, cfg.EngineToken)
		svc = &jobs.Service{
			Jobs:        store.NewPostgresJobStore(db),
			Labs:        store.NewPostgresLabStore(db),
			Audit:       store.NewPostgresAuditStore(db),
			MaxAttempts: cfg.WorkerMaxAttempts,
			WorkerID:    "worker-" + cfg.WorkerAddr,
			Logger:      logger,
			CheckNetwork: func() bool {
				ok, _ := netClient.Reachable(context.Background())
				return ok
			},
			TerraformRunner: terraform.NewContainerRunner(cfg.TerraformImage, "").WithNetworkMode(cfg.TFNetworkMode),
			TerraformRuns:   store.NewPostgresTerraformRunStore(db),
			Resources:       store.NewPostgresResourceStore(db),
			TerraformCfg: jobs.TerraformConfig{
				EmulatorEndpoint: cfg.EmulatorEndpoint,
				MinIOEndpoint:    cfg.MinIOEndpoint,
				WorkdirRoot:      cfg.TerraformWorkdirRoot,
			},
			// M9 Part 3: engine topology for cleanup_reconcile jobs.
			Engine: jobs.NewEngineAdapter(netClient),
			// Phase 7 (R10): audit_cleanup archives before deleting.
			AuditExportDir: filepath.Join(cfg.TerraformWorkdirRoot, "audit-exports"),
		}
		// M3: use the host agent for lab provisioning when it is
		// explicitly configured (HOST_AGENT_ADDR set in the
		// environment). Unset means the network-engine gRPC Health path.
		if addr, ok := os.LookupEnv("HOST_AGENT_ADDR"); ok && addr != "" {
			svc.HostAgent = hostagent.NewClient(cfg.HostAgentAddr, cfg.HostAgentToken)
			logger.Info("host agent configured for lab provisioning", "addr", cfg.HostAgentAddr, "mode", cfg.HostAgentMode)
		}
		dbOK = true
	}

	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"cloudforge-worker","version":"0.7.0-m6m7"}`))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		counts := map[string]int{}
		if svc != nil {
			if c, err := svc.Jobs.Counts(r.Context()); err == nil {
				counts = c
			} else {
				logger.Warn("job counts failed", "error", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "cloudforge-worker", "version": "0.7.0-m6m7",
			"database_connected": dbOK, "jobs": counts,
			"uptime_seconds": int64(time.Since(started).Seconds()),
		})
	})
	srv := &http.Server{Addr: cfg.WorkerAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Job loop.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	if svc != nil {
		go func() {
			ticker := time.NewTicker(cfg.WorkerPoll)
			defer ticker.Stop()
			sweepEvery := 15 // sweep expired labs every 15 polls (~30s at default)
			n := 0
			for {
				select {
				case <-loopCtx.Done():
					return
				case <-ticker.C:
					n++
					if err := svc.RunOnce(loopCtx); err != nil {
						logger.Error("job run failed", "error", err)
					}
					if n%sweepEvery == 0 {
						if err := svc.SweepExpired(loopCtx); err != nil {
							logger.Error("expiry sweep failed", "error", err)
						}
					}
				}
			}
		}()
	}

	go func() {
		logger.Info("cloudforge worker listening", "addr", cfg.WorkerAddr, "database", dbOK)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker server error", "error", err)
			os.Exit(1)
		}
	}()
	<-sigCtx.Done()
	loopCancel()
	logger.Info("shutting down worker")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
