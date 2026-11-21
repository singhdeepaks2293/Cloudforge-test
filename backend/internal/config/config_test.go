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
	if cfg.RedisAddr != "localhost:6379" || cfg.Addr != ":8080" || cfg.WorkerMaxAttempts != 5 {
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

func TestMain(m *testing.M) {
	// Ensure tests do not leak env between cases (t.Setenv handles it).
	os.Exit(m.Run())
}
