// Command api is the CloudForge Go control-plane HTTP server (Milestones 4-5).
//
// Wiring: config -> PostgreSQL (pgxpool) + migrations when reachable,
// otherwise an in-memory fallback with a loud warning; Redis cache
// fail-open. Lab provisioning is worker-driven (job queue) in Postgres
// mode and in-process (M1 behaviour) in fallback mode.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/config"
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/httpapi"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/ratelimit"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func main() {
	// -seed-curriculum forces a curriculum seed at startup even
	// when CURRICULUM_SEED=0.
	seedFlag := flag.Bool("seed-curriculum", false, "seed the curriculum from CURRICULUM_DIR at startup")
	// --health-check is the container HEALTHCHECK probe (see
	// backend/Dockerfile): GET /readyz on the API port and exit 0
	// only on HTTP 200. It runs before config load so a broken
	// configuration still reports unhealthy instead of hanging.
	// Distroless has no wget/curl, so the binary probes itself.
	healthCheckFlag := flag.Bool("health-check", false, "probe /readyz and exit 0 when healthy, 1 otherwise")
	flag.Parse()
	if *healthCheckFlag {
		os.Exit(healthCheck())
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("cloudforge api starting", "config", cfg.String())

	// Phase 7 (R4): fail closed on production secrets. Without
	// CLOUDFORGE_DEV=1, an unset or dev-default JWT/engine/metrics
	// secret (or MinIO default) refuses startup with an error naming
	// the variable. In dev mode the well-known defaults are allowed
	// only via the explicit opt-in, and the warning is loud.
	if warns, err := cfg.ValidateSecrets(); err != nil {
		logger.Error("refusing to start: production secrets are not configured", "error", err)
		os.Exit(1)
	} else {
		for _, w := range warns {
			logger.Warn(w)
		}
	}
	secret := cfg.JWTSecret
	if secret == "" {
		// Dev mode only (ValidateSecrets already enforced this):
		// the dev default JWT secret.
		secret = config.DevJWTSecret
	}

	// PostgreSQL: try to connect and migrate; fall back to in-memory
	// stores (with a warning) when unreachable so the API still runs
	// for development without a database.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, dbErr := database.Connect(ctx, cfg.DatabaseURL)
	cancel()
	var (
		users     store.UserStore
		labStore  store.LabStore
		courses   store.CourseStore
		audit     store.AuditStore
		resources store.LabResourceStore
		tfRuns    store.TerraformRunStore
		termSess  store.TerminalSessionStore
		jobStore  store.JobStore
		curr      store.CurriculumStore
		quotas    store.QuotaStore
		iamStore  store.IAMStore
		p3Store   store.Phase3Store
		dbOK      bool
		asyncProv bool
	)
	signer := auth.NewStore(secret)
	signer.TokenTTL = cfg.TokenTTL
	// Phase 7 (R3): durable token revocation. The revoker is
	// installed after the store choice below (Postgres when the DB
	// is up, Redis when it is reachable, memory otherwise); see
	// the installRevoker call after the if/else.
	if dbErr != nil {
		logger.Warn("PostgreSQL unavailable; running with in-memory stores (data is lost on restart)", "error", dbErr)
		memLabs := labs.NewStore()
		memLabs.CheckNetwork = func() bool {
			ok, _ := networkclient.NewWithToken(cfg.NetworkEngineAddr, cfg.EngineToken).Reachable(context.Background())
			return ok
		}
		users = signer
		labStore = store.NewMemoryLabStore(memLabs)
		courses = store.NewMemoryCourseStore()
		audit = store.NewMemoryAuditStore()
		resources = store.NewMemoryResourceStore()
		tfRuns = store.NewMemoryTerraformRunStore()
		termSess = store.NewMemoryTerminalSessionStore()
		jobStore = store.NewMemoryJobStore()
		curr = store.NewMemoryCurriculumStore(courses.(*store.MemoryCourseStore))
		quotas = store.NewMemoryQuotaStore()
		iamStore = store.NewMemoryIAMStore()
		p3Store = store.NewMemoryPhase3Store()
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
		users = store.NewPostgresUserStore(db, signer)
		labStore = store.NewPostgresLabStore(db)
		courses = store.NewPostgresCourseStore(db)
		audit = store.NewPostgresAuditStore(db)
		resources = store.NewPostgresResourceStore(db)
		tfRuns = store.NewPostgresTerraformRunStore(db)
		termSess = store.NewPostgresTerminalSessionStore(db)
		jobStore = store.NewPostgresJobStore(db)
		curr = store.NewPostgresCurriculumStore(db)
		quotas = store.NewPostgresQuotaStore(db)
		iamStore = store.NewPostgresIAMStore(db)
		p3Store = store.NewPostgresPhase3Store(db)
		dbOK = true
		asyncProv = true
	}

	// Lightweight mode: persist the lite per-role limits so
	// GET /api/v1/admin/quotas shows what is actually enforced.
	// Enforcement does not depend on this seed — httpapi.roleLimits
	// caps stored rows at quota.LiteLimits whenever Lite is on, so
	// a database seeded with full-mode defaults is still capped.
	// An admin can raise limits afterwards with PUT /admin/quotas,
	// but the lite cap still applies while Lite is on.
	if cfg.Lite {
		for _, role := range quota.ValidRoles {
			l := quota.LiteLimits(role)
			if err := quotas.UpsertQuota(context.Background(), &store.QuotaSet{
				Role: role, MaxLabs: l.MaxLabs, MaxCPU: l.MaxCPU,
				MaxMemoryMB: l.MaxMemoryMB, MaxDiskGB: l.MaxDiskGB,
				MaxLabHours: l.MaxLabHours, MaxLabMinutes: l.MaxLabMinutes,
			}); err != nil {
				logger.Warn("could not seed lite quotas", "role", role, "error", err)
			}
		}
		logger.Info("lightweight mode: lite quotas seeded (learner 1 lab / Micro / 30 min)",
			"host_agent_mode", cfg.HostAgentMode)
	}

	// M9 Part 3: the store-level lab ceiling is a backstop for the
	// highest configured role limit; the API enforces each role's
	// own limit (see httpapi.checkLabQuotas). Without this, the
	// store's built-in default (3) would veto legitimate admin
	// labs even after an admin raised the admin limit.
	if qs, err := quotas.GetQuotas(context.Background()); err == nil {
		max := 0
		for _, q := range qs {
			if l := (quota.Limits{MaxLabs: q.MaxLabs}).MaxLabsOrDefault(); l > max {
				max = l
			}
		}
		if max > 0 {
			labStore.SetMaxLabs(max)
		}
	} else {
		logger.Warn("could not load quotas; store lab ceiling stays at the built-in default", "error", err)
	}

	// Milestone 8: seed the curriculum at startup when enabled
	// (CURRICULUM_SEED, default on) or forced with
	// -seed-curriculum. Seeding is idempotent. An explicitly
	// configured CURRICULUM_DIR that cannot be resolved is fatal;
	// auto-resolution failing (no curriculum/ next to the binary)
	// only warns, so the API still runs without content.
	if cfg.CurriculumSeed || *seedFlag {
		dir, err := curriculum.ResolveDir(cfg.CurriculumDir)
		if err != nil {
			if cfg.CurriculumDir != "" || *seedFlag {
				logger.Error("curriculum directory could not be resolved", "error", err)
				os.Exit(1)
			}
			logger.Warn("curriculum seeding skipped: no curriculum directory found", "error", err)
		} else if rep, err := curriculum.SeedDir(context.Background(), curr, dir); err != nil {
			logger.Error("curriculum seed failed", "error", err, "dir", dir)
			os.Exit(1)
		} else {
			logger.Info("curriculum seeded", "dir", dir, "tracks", rep.Tracks, "lessons", rep.Lessons,
				"questions", rep.Questions, "scenarios", rep.Scenarios, "achievements", rep.Achievements)
		}
	}

	// Redis: fail-open. A disabled cache is reported in /api/v1/health.
	var redisCache *cache.Cache
	redisCache, cacheErr := cache.New(cfg.RedisAddr)
	if cacheErr != nil {
		logger.Warn("Redis unavailable; running without cache", "error", cacheErr)
		redisCache = &cache.Cache{}
	} else {
		defer redisCache.Close()
	}

	// Phase 7 (R3): install the token revoker on the shared signer.
	// Postgres is durable across restarts; Redis is shared across
	// replicas; memory is single-process only (in-memory fallback).
	if dbOK {
		signer.SetRevoker(store.NewPostgresRevoker(db))
	} else if redisCache != nil && redisCache.Available() {
		signer.SetRevoker(auth.NewRedisRevoker(ratelimit.NewClient(cfg.RedisAddr)))
	}
	// (Otherwise the memory revoker from auth.NewStore stays.)

	netClient := networkclient.NewWithToken(cfg.NetworkEngineAddr, cfg.EngineToken)
	// M5: AWS provider backends (MinIO S3, DynamoDB Local, the
	// educational simulators, LocalStack emulator probe). Client
	// construction performs no network I/O; reachability is
	// established by the startup probe below and per operation.
	registry := providers.NewRegistry()
	awsProviders, err := providers.NewAWS(providers.AWSConfig{
		MinIOEndpoint:    cfg.MinIOEndpoint,
		MinIOAccessKey:   cfg.MinIOAccessKey,
		MinIOSecretKey:   cfg.MinIOSecretKey,
		MinIOUseSSL:      cfg.MinIOUseSSL,
		DynamoDBEndpoint: cfg.DynamoDBEndpoint,
		EmulatorEndpoint: cfg.EmulatorEndpoint,
	}, registry)
	if err != nil {
		logger.Error("aws providers failed to initialise", "error", err)
		os.Exit(1)
	}
	// Startup health probe (non-blocking): capability health is
	// never assumed ok, and the capabilities API re-probes on demand.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		awsProviders.RefreshHealth(ctx)
	}()
	// M3: when a host agent is explicitly configured (HOST_AGENT_ADDR
	// set in the environment), the API reports its state in
	// /api/v1/health and the worker provisions labs through it. The
	// host agent itself is only ever called via the typed client.
	var hostAgentClient *hostagent.Client
	if addr, ok := os.LookupEnv("HOST_AGENT_ADDR"); ok && addr != "" {
		hostAgentClient = hostagent.NewClient(cfg.HostAgentAddr, cfg.HostAgentToken)
		logger.Info("host agent configured", "addr", cfg.HostAgentAddr, "mode", cfg.HostAgentMode)
	}
	// M9: live Postgres probe for /readyz (nil in the in-memory
	// fallback, where /readyz reports postgres as "disabled").
	var dbHealth func(context.Context) error
	if dbOK {
		dbHealth = db.Health
	}
	// M9: Redis-backed rate limiters (fail-closed for auth,
	// fail-open for the general API — see internal/ratelimit).
	// The client is lazy: if Redis is down at startup the limiters
	// still build, and requests see the documented failure
	// behaviour (503 for auth, unthrottled for general API).
	rlClient := ratelimit.NewClient(cfg.RedisAddr)
	authLimiter := ratelimit.New(rlClient, "auth", cfg.AuthRateLimit, cfg.AuthRateWindow)
	apiLimiter := ratelimit.New(rlClient, "api", cfg.APIRateLimit, cfg.APIRateWindow)
	srv := httpapi.NewServerWithDeps(httpapi.Deps{
		Users: users, Labs: labStore, Courses: courses, Audit: audit,
		Cache: redisCache, Registry: registry, AWS: awsProviders, Network: netClient,
		Logger: logger, AdminEmail: cfg.AdminEmail,
		ProvisionAsync: asyncProv, DBConnected: dbOK,
		HostAgent: hostAgentClient, HostAgentMode: cfg.HostAgentMode,
		Resources: resources, TerraformRuns: tfRuns, TerminalSessions: termSess,
		Jobs: jobStore, TerraformRunner: terraform.NewContainerRunner(cfg.TerraformImage, "").WithNetworkMode(cfg.TFNetworkMode),
		EmulatorEndpoint: cfg.EmulatorEndpoint, MinIOEndpoint: cfg.MinIOEndpoint,
		WorkdirRoot: cfg.TerraformWorkdirRoot,
		Curriculum:  curr, CurriculumDir: cfg.CurriculumDir,
		DBHealth:    dbHealth,
		AuthLimiter: authLimiter, APILimiter: apiLimiter,
		UserAPILimiter: ratelimit.New(rlClient, "api-user", cfg.UserAPIRateLimit, cfg.UserAPIRateWindow),
		MetricsToken:   cfg.MetricsToken, TLSEnabled: cfg.TLSEnabled,
		TrustedProxies:       cfg.TrustedProxies,
		AuditExportRetention: cfg.AuditExportRetention,
		Quotas:               quotas, Lite: cfg.Lite,
		IAM: iamStore, P3: p3Store,
		P3DatabaseURL: cfg.DatabaseURL, P3RedisAddr: cfg.RedisAddr,
		P3DynamoEndpoint: cfg.DynamoDBEndpoint, P3WorkdirRoot: cfg.TerraformWorkdirRoot,
		P4RegistryURL: cfg.ECRRegistryURL, P4BuildSourceRoot: cfg.BuildSourceRoot,
		P6MasterKey: kmsMasterKey(),
	})
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("cloudforge api listening", "addr", cfg.Addr, "version", httpapi.Version, "database", dbOK)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	logger.Info("shutting down api server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("api server stopped")
}

// healthCheck probes the API's own /readyz. The port follows the
// same rules as config.Load: CLOUDFORGE_ADDR (":8081" form) wins,
// else PORT, else 8081.
func healthCheck() int {
	addr := os.Getenv("CLOUDFORGE_ADDR")
	port := "8081"
	if addr != "" {
		if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
			port = p
		}
	} else if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/readyz", nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// kmsMasterKey returns the Phase 6 KMS master secret:
// CLOUDFORGE_KMS_MASTER_KEY when set, else the JWT secret.
func kmsMasterKey() string {
	if v := os.Getenv("CLOUDFORGE_KMS_MASTER_KEY"); v != "" {
		return v
	}
	return os.Getenv("CLOUDFORGE_JWT_SECRET")
}
