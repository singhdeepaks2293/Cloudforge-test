// Package config loads CloudForge configuration from the environment.
//
// Fail-fast rule: an invalid DATABASE_URL aborts startup with an error.
// Secrets are never logged; Config.String redacts them.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the API and worker.
type Config struct {
	Addr              string // API listen address, e.g. ":8080"
	WorkerAddr        string // worker listen address, e.g. ":8081"
	DatabaseURL       string
	RedisAddr         string
	NetworkEngineAddr string
	JWTSecret         string
	AdminEmail        string // first user registering with this email becomes admin
	TokenTTL          time.Duration
	WorkerPoll        time.Duration // jobs poll interval
	WorkerMaxAttempts int
	// Host agent (Milestone 3).
	HostAgentMode  string // simulated | docker | libvirt | auto (default simulated)
	HostAgentAddr  string // host-agent address, e.g. ":8082" (listen) / "localhost:8082" (dial)
	HostAgentToken string // shared bearer token for the host-agent API
	// AWS providers (Milestone 5).
	MinIOEndpoint    string // MinIO S3 endpoint, host:port (default localhost:9000)
	MinIOAccessKey   string // MinIO access key (default minioadmin)
	MinIOSecretKey   string // MinIO secret key (default minioadmin) — never logged
	MinIOUseSSL      bool   // use https for MinIO (default false)
	DynamoDBEndpoint string // DynamoDB Local endpoint URL (default http://localhost:8000)
	EmulatorEndpoint string // AWS emulator (LocalStack) endpoint URL (default http://localhost:4566)
	// Terraform (Milestone 7).
	TerraformImage       string // container image for terraform runs (default hashicorp/terraform:1.9.8)
	TerraformWorkdirRoot string // per-lab workdirs live under <root>/<lab-id>/terraform
	LabRoot              string // lab directories root (default /var/lib/cloudforge/labs)
	// Curriculum (Milestone 8).
	CurriculumDir  string // curriculum/ source dir; "" = auto-resolve
	CurriculumSeed bool   // seed the curriculum at startup (CURRICULUM_SEED; default on)
	// Rate limiting (Milestone 9). Auth endpoints are fail-closed
	// when Redis is down; the general API limiter is fail-open.
	// See internal/ratelimit for the policy.
	AuthRateLimit  int           // auth attempts per window per IP (default 10)
	AuthRateWindow time.Duration // auth window (default 5m)
	APIRateLimit   int           // API requests per window per IP (default 300)
	APIRateWindow  time.Duration // API window (default 1m)
}

// DefaultDatabaseURL is the local-dev Postgres DSN.
const DefaultDatabaseURL = "postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	return d, nil
}

// ValidateDatabaseURL checks that u is a usable Postgres DSN.
// It does not connect; connection errors are reported by database.Connect.
func ValidateDatabaseURL(u string) error {
	if u == "" {
		return errors.New("DATABASE_URL is empty")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("invalid DATABASE_URL: scheme must be postgres or postgresql, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("invalid DATABASE_URL: missing host")
	}
	return nil
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dbURL := envOr("DATABASE_URL", DefaultDatabaseURL)
	if err := ValidateDatabaseURL(dbURL); err != nil {
		return nil, err
	}
	tokenTTL, err := envDuration("CLOUDFORGE_TOKEN_TTL", 12*time.Hour)
	if err != nil {
		return nil, err
	}
	poll, err := envDuration("WORKER_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	authRateLimit := 10
	if v := os.Getenv("CLOUDFORGE_AUTH_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid CLOUDFORGE_AUTH_RATE_LIMIT %q: must be a positive integer", v)
		}
		authRateLimit = n
	}
	authRateWindow, err := envDuration("CLOUDFORGE_AUTH_RATE_WINDOW", 5*time.Minute)
	if err != nil {
		return nil, err
	}
	apiRateLimit := 300
	if v := os.Getenv("CLOUDFORGE_API_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid CLOUDFORGE_API_RATE_LIMIT %q: must be a positive integer", v)
		}
		apiRateLimit = n
	}
	apiRateWindow, err := envDuration("CLOUDFORGE_API_RATE_WINDOW", time.Minute)
	if err != nil {
		return nil, err
	}
	maxAttempts := 5
	if v := os.Getenv("WORKER_MAX_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid WORKER_MAX_ATTEMPTS %q: must be a positive integer", v)
		}
		maxAttempts = n
	}
	addr := os.Getenv("CLOUDFORGE_ADDR")
	if addr == "" {
		addr = ":" + envOr("PORT", "8080")
	}
	// CURRICULUM_SEED defaults to on (dev default): only an
	// explicit 0/false disables startup seeding.
	curriculumSeed := true
	if v := os.Getenv("CURRICULUM_SEED"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "no", "off":
			curriculumSeed = false
		case "1", "true", "yes", "on":
			curriculumSeed = true
		default:
			return nil, fmt.Errorf("invalid CURRICULUM_SEED %q: must be 1/0, true/false, yes/no or on/off", v)
		}
	}
	return &Config{
		Addr:                 addr,
		WorkerAddr:           envOr("WORKER_ADDR", ":8081"),
		DatabaseURL:          dbURL,
		RedisAddr:            envOr("REDIS_ADDR", "localhost:6379"),
		NetworkEngineAddr:    envOr("NETWORK_ENGINE_ADDR", "localhost:50051"),
		JWTSecret:            os.Getenv("CLOUDFORGE_JWT_SECRET"),
		AdminEmail:           os.Getenv("CLOUDFORGE_ADMIN_EMAIL"),
		TokenTTL:             tokenTTL,
		WorkerPoll:           poll,
		WorkerMaxAttempts:    maxAttempts,
		HostAgentMode:        envOr("HOST_AGENT_MODE", "simulated"),
		HostAgentAddr:        envOr("HOST_AGENT_ADDR", ":8082"),
		HostAgentToken:       os.Getenv("HOST_AGENT_TOKEN"),
		MinIOEndpoint:        envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:       envOr("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:       envOr("MINIO_SECRET_KEY", "minioadmin"),
		MinIOUseSSL:          strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true"),
		DynamoDBEndpoint:     envOr("DYNAMODB_ENDPOINT", "http://localhost:8000"),
		EmulatorEndpoint:     envOr("AWS_EMULATOR_ENDPOINT", "http://localhost:4566"),
		TerraformImage:       envOr("TERRAFORM_IMAGE", "hashicorp/terraform:1.9.8"),
		LabRoot:              envOr("CLOUDFORGE_LAB_ROOT", "/var/lib/cloudforge/labs"),
		TerraformWorkdirRoot: envOr("TERRAFORM_WORKDIR_ROOT", "/var/lib/cloudforge/labs"),
		CurriculumDir:        os.Getenv("CURRICULUM_DIR"),
		CurriculumSeed:       curriculumSeed,
		AuthRateLimit:        authRateLimit,
		AuthRateWindow:       authRateWindow,
		APIRateLimit:         apiRateLimit,
		APIRateWindow:        apiRateWindow,
	}, nil
}

// String returns a log-safe summary with secrets redacted.
func (c *Config) String() string {
	secret := "<unset>"
	if c.JWTSecret != "" {
		secret = "<set>"
	}
	hostAgentToken := "<unset>"
	if c.HostAgentToken != "" {
		hostAgentToken = "<set>"
	}
	return fmt.Sprintf("addr=%s worker_addr=%s database=%s redis=%s network_engine=%s jwt_secret=%s token_ttl=%s host_agent_mode=%s host_agent_addr=%s host_agent_token=%s minio=%s dynamodb=%s emulator=%s",
		c.Addr, c.WorkerAddr, redactDSN(c.DatabaseURL), c.RedisAddr, c.NetworkEngineAddr, secret, c.TokenTTL, c.HostAgentMode, c.HostAgentAddr, hostAgentToken, c.MinIOEndpoint, c.DynamoDBEndpoint, c.EmulatorEndpoint)
}

// redactDSN removes the password from a Postgres DSN for logging.
func redactDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable-dsn>"
	}
	if parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
	}
	return parsed.String()
}
