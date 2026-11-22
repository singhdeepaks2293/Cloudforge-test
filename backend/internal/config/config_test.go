package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("PORT", "")
	t.Setenv("CLOUDFORGE_ADDR", "")
	t.Setenv("CLOUDFORGE_JWT_SECRET", "")
	t.Setenv("WORKER_MAX_ATTEMPTS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if cfg.DatabaseURL != DefaultDatabaseURL {
		t.Fatalf("default DATABASE_URL: %q", cfg.DatabaseURL)
	}
	if cfg.RedisAddr != "localhost:6379" || cfg.Addr != ":8081" || cfg.WorkerAddr != ":8083" || cfg.WorkerMaxAttempts != 5 {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
}

func TestLoadRejectsInvalidDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "mysql://localhost/db")
	if _, err := Load(); err == nil {
		t.Fatal("mysql DATABASE_URL must be rejected")
	}
	t.Setenv("DATABASE_URL", "postgres://")
	if _, err := Load(); err == nil {
		t.Fatal("DATABASE_URL without a host must be rejected")
	}
	t.Setenv("DATABASE_URL", "://bad url")
	if _, err := Load(); err == nil {
		t.Fatal("unparseable DATABASE_URL must be rejected")
	}
}

func TestStringRedactsSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:s3cret-password@localhost:5432/db?sslmode=disable")
	t.Setenv("CLOUDFORGE_JWT_SECRET", "also-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.String()
	if strings.Contains(s, "s3cret-password") || strings.Contains(s, "also-secret") {
		t.Fatalf("config string must not contain secrets: %q", s)
	}
	if !strings.Contains(s, "user@localhost") {
		t.Fatalf("redacted DSN should keep the username/host: %q", s)
	}
}

func TestLoadHostAgentDefaults(t *testing.T) {
	t.Setenv("HOST_AGENT_MODE", "")
	t.Setenv("HOST_AGENT_ADDR", "")
	t.Setenv("HOST_AGENT_TOKEN", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HostAgentMode != "simulated" || cfg.HostAgentAddr != ":8082" || cfg.HostAgentToken != "" {
		t.Fatalf("host agent defaults wrong: mode=%q addr=%q token set=%v",
			cfg.HostAgentMode, cfg.HostAgentAddr, cfg.HostAgentToken != "")
	}
	if s := cfg.String(); strings.Contains(s, "host_agent_token=<set>") {
		t.Fatalf("token must be reported unset: %q", s)
	}
	t.Setenv("HOST_AGENT_MODE", "docker")
	t.Setenv("HOST_AGENT_TOKEN", "top-secret")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HostAgentMode != "docker" {
		t.Fatalf("mode = %q", cfg.HostAgentMode)
	}
	if s := cfg.String(); strings.Contains(s, "top-secret") {
		t.Fatalf("config string must not contain the host agent token: %q", s)
	}
}

func TestLoadLiteFlag(t *testing.T) {
	t.Setenv("CLOUDFORGE_LITE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lite {
		t.Fatal("lite must default to off")
	}
	t.Setenv("CLOUDFORGE_LITE", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Lite {
		t.Fatal("CLOUDFORGE_LITE=true must enable lite mode")
	}
	if s := cfg.String(); !strings.Contains(s, "lite=true") {
		t.Fatalf("config string must report lite mode: %q", s)
	}
	for _, v := range []string{"1", "yes", "on"} {
		t.Setenv("CLOUDFORGE_LITE", v)
		cfg, err = Load()
		if err != nil || !cfg.Lite {
			t.Fatalf("CLOUDFORGE_LITE=%q must enable lite mode (err=%v)", v, err)
		}
	}
	t.Setenv("CLOUDFORGE_LITE", "tru")
	if _, err := Load(); err == nil {
		t.Fatal("CLOUDFORGE_LITE=tru must be rejected, not silently ignored")
	}
}

func TestLoadModeFlag(t *testing.T) {
	// CLOUDFORGE_MODE is the documented switch; full is the
	// default, so existing behaviour does not change.
	t.Setenv("CLOUDFORGE_MODE", "")
	t.Setenv("CLOUDFORGE_LITE", "")
	cfg, err := Load()
	if err != nil || cfg.Lite {
		t.Fatalf("default mode must be full (lite off): %+v %v", cfg, err)
	}
	t.Setenv("CLOUDFORGE_MODE", "full")
	cfg, err = Load()
	if err != nil || cfg.Lite {
		t.Fatalf("MODE=full must be full mode: %+v %v", cfg, err)
	}
	t.Setenv("CLOUDFORGE_MODE", "lite")
	t.Setenv("CLOUDFORGE_LITE", "")
	cfg, err = Load()
	if err != nil || !cfg.Lite {
		t.Fatalf("MODE=lite must enable lite mode: %+v %v", cfg, err)
	}
	t.Setenv("CLOUDFORGE_MODE", "medium")
	if _, err := Load(); err == nil {
		t.Fatal("MODE=medium must be rejected")
	}
	// Both switches set and disagreeing must fail fast, not
	// silently pick one.
	t.Setenv("CLOUDFORGE_MODE", "full")
	t.Setenv("CLOUDFORGE_LITE", "true")
	if _, err := Load(); err == nil {
		t.Fatal("MODE=full + LITE=true must be rejected")
	}
	t.Setenv("CLOUDFORGE_MODE", "lite")
	t.Setenv("CLOUDFORGE_LITE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("MODE=lite + LITE=false must be rejected")
	}
	// Agreeing is fine.
	t.Setenv("CLOUDFORGE_MODE", "lite")
	t.Setenv("CLOUDFORGE_LITE", "true")
	cfg, err = Load()
	if err != nil || !cfg.Lite {
		t.Fatalf("MODE=lite + LITE=true must enable lite mode: %+v %v", cfg, err)
	}
}

func TestMain(m *testing.M) {
	// Ensure tests do not leak env between cases (t.Setenv handles it).
	os.Exit(m.Run())
}

// --- Phase 7: fail-closed secrets ---------------------------------------

// TestValidateSecretsFailsClosed verifies R4: without CLOUDFORGE_DEV=1,
// unset or dev-default secrets refuse startup with an error naming
// the variable; with the opt-in, dev defaults are allowed (with a
// warning) and production secrets pass clean.
func TestValidateSecretsFailsClosed(t *testing.T) {
	t.Setenv("CLOUDFORGE_DEV", "")
	t.Setenv("CLOUDFORGE_JWT_SECRET", "")
	t.Setenv("CLOUDFORGE_ENGINE_TOKEN", "")
	t.Setenv("CLOUDFORGE_METRICS_TOKEN", "")
	t.Setenv("MINIO_ACCESS_KEY", "minioadmin")
	t.Setenv("MINIO_SECRET_KEY", "minioadmin")
	t.Setenv("HOST_AGENT_TOKEN", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.ValidateSecrets(); err == nil {
		t.Fatal("ValidateSecrets must fail with unset/dev-default secrets and no dev opt-in")
	} else {
		for _, want := range []string{"CLOUDFORGE_JWT_SECRET", "CLOUDFORGE_ENGINE_TOKEN", "CLOUDFORGE_METRICS_TOKEN"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q must name %s", err.Error(), want)
			}
		}
	}

	// Dev opt-in: dev defaults are usable, with a warning.
	t.Setenv("CLOUDFORGE_DEV", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load dev: %v", err)
	}
	if !cfg.Dev || cfg.EngineToken != DevEngineToken {
		t.Fatalf("dev mode must set Dev and the dev engine token, got %+v", cfg)
	}
	warns, err := cfg.ValidateSecrets()
	if err != nil {
		t.Fatalf("dev mode must not fail validation: %v", err)
	}
	if len(warns) == 0 {
		t.Error("dev mode must return a warning naming the opt-in")
	}

	// Production secrets pass.
	t.Setenv("CLOUDFORGE_DEV", "")
	t.Setenv("CLOUDFORGE_JWT_SECRET", "a-very-long-production-jwt-secret-0123456789")
	t.Setenv("CLOUDFORGE_ENGINE_TOKEN", "engine-token-long-enough")
	t.Setenv("CLOUDFORGE_METRICS_TOKEN", "metrics-token-long-enough")
	t.Setenv("MINIO_ACCESS_KEY", "prod-access-key")
	t.Setenv("MINIO_SECRET_KEY", "prod-secret-key-long")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load prod: %v", err)
	}
	if warns, err := cfg.ValidateSecrets(); err != nil || len(warns) != 0 {
		t.Fatalf("production secrets must validate clean: warns=%v err=%v", warns, err)
	}
	// A short JWT secret is still refused.
	t.Setenv("CLOUDFORGE_JWT_SECRET", "short")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.ValidateSecrets(); err == nil {
		t.Fatal("short JWT secret must fail validation")
	}
}
