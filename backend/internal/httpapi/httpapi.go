// Package httpapi implements the CloudForge HTTP API (net/http ServeMux,
// /api/v1 prefix) and the JSON error envelope.
//
// Milestone 2: handlers depend on store interfaces (UserStore, LabStore,
// CourseStore, AuditStore) so unit tests use the in-memory fakes and
// production uses PostgreSQL. New M2 endpoints: user management,
// course CRUD (admin), modules/enroll/progress, audit listing.
package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/advanced"
	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/jobs"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/metrics"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/cloudforge/cloudforge/backend/internal/phase4"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/ratelimit"
	"github.com/cloudforge/cloudforge/backend/internal/security"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terminal"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
	"github.com/go-chi/chi/v5"
)

// Version is the v4 API version string.
const Version = "4.0.0"

type ctxKey string

const requestIDKey ctxKey = "request_id"

// RequestID returns the correlation ID for a request, or "".
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + time.Now().UTC().Format("20060102150405")
	}
	return hex.EncodeToString(b)
}

// Deps wires a Server. All stores are interfaces; NewServer builds the
// M1-compatible in-memory set, cmd/api builds the Postgres set.
type Deps struct {
	Users          store.UserStore
	Labs           store.LabStore
	Courses        store.CourseStore
	Audit          store.AuditStore
	Cache          *cache.Cache
	Registry       *providers.Registry
	AWS            *providers.AWS // nil = AWS providers not configured; S3/IAM endpoints return 503
	Network        *networkclient.Client
	HostAgent      *hostagent.Client // nil = host agent not configured
	HostAgentMode  string
	Logger         *slog.Logger
	AdminEmail     string // first user registering with this email becomes admin
	ProvisionAsync bool   // true when labs are provisioned by the worker (Postgres mode)
	DBConnected    bool   // true when Postgres is the active store
	// Milestone 6/7 stores and runners. Nil values get in-memory
	// defaults in NewServerWithDeps (ContainerRunner for Terraform).
	Resources        store.LabResourceStore
	TerraformRuns    store.TerraformRunStore
	TerminalSessions store.TerminalSessionStore
	Jobs             store.JobStore
	TerraformRunner  terraform.Runner
	// Terraform provider endpoints (validated per request; only
	// loopback/emulator endpoints are accepted).
	EmulatorEndpoint string
	MinIOEndpoint    string
	WorkdirRoot      string // per-lab terraform workdirs: <root>/<lab-id>/terraform
	// Milestone 8 curriculum store + source directory. Nil
	// Curriculum gets an in-memory default in NewServerWithDeps.
	Curriculum    store.CurriculumStore
	CurriculumDir string
	// v3 Advanced section service (nil = constructed in
	// NewServerWithDeps from CurriculumDir + Curriculum).
	Advanced *advanced.Service
	// Milestone 9: live Postgres health probe for /readyz. Nil when
	// the API runs on the in-memory fallback (no Postgres); /readyz
	// then reports postgres as "disabled" instead of failing.
	DBHealth func(ctx context.Context) error
	// Milestone 9: Redis-backed rate limiters. Nil means "not
	// configured" and disables that limiter (existing tests and
	// Redis-less dev runs are unthrottled).
	AuthLimiter *ratelimit.Limiter // register/login; fail-closed on Redis error
	APILimiter  *ratelimit.Limiter // general /api/v1; fail-open on Redis error
	// Phase 7 hardening (see security_p7.go).
	UserAPILimiter *ratelimit.Limiter // per-user /api/v1; fail-open on Redis error
	MetricsToken   string             // X-Metrics-Token for GET /metrics ("" = loopback-only)
	TLSEnabled     bool               // HSTS + Secure cookies when a TLS proxy fronts the API
	TrustedProxies []string           // extra peer IPs/CIDRs trusted for X-Forwarded-For
	// AuditExportRetention is how many audit-export job files are
	// kept (default 7; 0 = default).
	AuditExportRetention int
	// Milestone 9, Part 3: per-role quota sets. Nil gets an
	// in-memory default (seeded with quota.DefaultLimits) in
	// NewServerWithDeps.
	Quotas store.QuotaStore
	// Lightweight mode (CLOUDFORGE_LITE): role limits are capped
	// at quota.LiteLimits (see roleLimits in quotas.go).
	Lite bool
	// Milestone 9, Part 3: cleanup reconciler. Nil means the
	// handler builds a jobs.Service from this server's own
	// stores/engine/host-agent on demand.
	Reconciler jobs.Reconciler
	// Phase 2: IAM identity store. Nil gets an in-memory default in
	// NewServerWithDeps (Postgres in cmd/api when a DB is up).
	IAM store.IAMStore
	// Phase 2: CloudWatch-style alarm store (in-memory definitions).
	// Nil gets a fresh store in NewServerWithDeps.
	Alarms *alarmStore
	// Phase 3: data/messaging/serverless store + backends. Nil P3
	// gets an in-memory default in NewServerWithDeps (Postgres in
	// cmd/api when a DB is up). Empty DatabaseURL means RDS cannot
	// provision (503, honestly); empty RedisAddr defaults to
	// localhost:6379; empty DynamoEndpoint defaults to
	// http://localhost:8000.
	P3               store.Phase3Store
	P3DatabaseURL    string
	P3RedisAddr      string
	P3DynamoEndpoint string
	P3WorkdirRoot    string
	// Phase 6: advanced services. P6MasterKey is the KMS master
	// secret (CLOUDFORGE_KMS_MASTER_KEY, else the JWT secret);
	// empty means the dev default (keys still work, but the
	// wrapping key is the well-known dev string).
	P6MasterKey string
	// Phase 4: containers + developer tools. Empty P4RegistryURL
	// means the local-disk registry backend; a URL points at a real
	// registry:2 container (full mode). Empty P4BuildSourceRoot
	// defaults to P3WorkdirRoot: CodeBuild/CodePipeline sources must
	// be local paths under it.
	P4RegistryURL     string
	P4BuildSourceRoot string
}

// Server wires the API handlers to their stores.
type Server struct {
	Users            store.UserStore
	Labs             store.LabStore
	Courses          store.CourseStore
	Audit            store.AuditStore
	Cache            *cache.Cache
	Registry         *providers.Registry
	AWS              *providers.AWS
	Network          *networkclient.Client
	HostAgent        *hostagent.Client
	HostAgentMode    string
	Logger           *slog.Logger
	Started          time.Time
	AdminEmail       string
	ProvisionAsync   bool
	DBConnected      bool
	Resources        store.LabResourceStore
	TerraformRuns    store.TerraformRunStore
	TerminalSessions store.TerminalSessionStore
	Jobs             store.JobStore
	TerraformRunner  terraform.Runner
	Terminal         *terminal.Service
	EmulatorEndpoint string
	MinIOEndpoint    string
	WorkdirRoot      string
	Curriculum       store.CurriculumStore
	CurriculumDir    string
	Advanced         *advanced.Service
	DBHealth         func(ctx context.Context) error
	AuthLimiter      *ratelimit.Limiter
	APILimiter       *ratelimit.Limiter
	// Phase 7 hardening (see security_p7.go).
	UserAPILimiter       *ratelimit.Limiter
	MetricsToken         string
	TLSEnabled           bool
	TrustedProxies       []string
	AuditExportRetention int
	Quotas               store.QuotaStore
	Reconciler           jobs.Reconciler
	Lite                 bool // lightweight mode: cap role limits at quota.LiteLimits
	IAM                  store.IAMStore
	Alarms               *alarmStore
	// Phase 3 stores and backend addresses (see Deps).
	P3               store.Phase3Store
	P3DatabaseURL    string
	P3RedisAddr      string
	P3DynamoEndpoint string
	P3WorkdirRoot    string
	// Phase 4 config (see Deps).
	P4RegistryURL     string
	P4BuildSourceRoot string
	// Phase 6 config (see Deps).
	P6MasterKey string
	// Phase 3 runtime state: DynamoDB backend probe cache and the
	// per-load-balancer round-robin counters (process-local).
	dynamoMu          sync.Mutex
	dynamoBackendName string
	dynamoCheckedAt   time.Time
	elbMu             sync.Mutex
	elbRR             map[string]*phase3.RoundRobin
	// Phase 4 runtime state: ECR registry backend (local disk by
	// default; a registry:2 container when P4RegistryURL is set),
	// the ECS/Fargate task runner, and the once-only orphan sweep.
	p4Mu         sync.Mutex
	p4Reg        phase4.RegistryBackend
	p4TaskRunner *phase4.TaskRunner
	p4OrphanOnce sync.Once
}

// NewServer builds a Server from the M1 concrete stores (in-memory).
// It is kept so existing callers and tests keep working.
func NewServer(users *auth.Store, labStore *labs.Store, registry *providers.Registry, netClient *networkclient.Client, logger *slog.Logger) *Server {
	return NewServerWithDeps(Deps{
		Users:    users,
		Labs:     store.NewMemoryLabStore(labStore),
		Courses:  store.NewMemoryCourseStore(),
		Audit:    store.NewMemoryAuditStore(),
		Registry: registry,
		Network:  netClient,
		Logger:   logger,
	})
}

// NewServerWithDeps builds a Server from interface dependencies.
func NewServerWithDeps(d Deps) *Server {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Registry == nil {
		d.Registry = providers.NewRegistry()
	}
	if d.Users == nil {
		d.Users = auth.NewStore("dev-only-change-me")
	}
	if d.Courses == nil {
		d.Courses = store.NewMemoryCourseStore()
	}
	if d.Labs == nil {
		d.Labs = store.NewMemoryLabStore(labs.NewStore())
	}
	if d.Audit == nil {
		d.Audit = store.NewMemoryAuditStore()
	}
	if d.Resources == nil {
		d.Resources = store.NewMemoryResourceStore()
	}
	if d.TerraformRuns == nil {
		d.TerraformRuns = store.NewMemoryTerraformRunStore()
	}
	if d.TerminalSessions == nil {
		d.TerminalSessions = store.NewMemoryTerminalSessionStore()
	}
	if d.Jobs == nil {
		d.Jobs = store.NewMemoryJobStore()
	}
	if d.TerraformRunner == nil {
		d.TerraformRunner = terraform.NewContainerRunner("", "")
	}
	if d.EmulatorEndpoint == "" {
		d.EmulatorEndpoint = "http://localhost:4566"
	}
	if d.MinIOEndpoint == "" {
		d.MinIOEndpoint = "localhost:9000"
	}
	if d.WorkdirRoot == "" {
		d.WorkdirRoot = "/var/lib/cloudforge/labs"
	}
	if d.Curriculum == nil {
		// Share the course store when it is the memory one, so a
		// seeded track is visible through CourseStore as well.
		if mem, ok := d.Courses.(*store.MemoryCourseStore); ok {
			d.Curriculum = store.NewMemoryCurriculumStore(mem)
		} else {
			d.Curriculum = store.NewMemoryCurriculumStore(nil)
		}
	}
	if d.Quotas == nil {
		d.Quotas = store.NewMemoryQuotaStore()
	}
	if d.Advanced == nil {
		d.Advanced = advanced.NewService(d.CurriculumDir, d.Curriculum)
	}
	if d.IAM == nil {
		d.IAM = store.NewMemoryIAMStore()
	}
	if d.Alarms == nil {
		d.Alarms = newAlarmStore()
	}
	if d.P3 == nil {
		d.P3 = store.NewMemoryPhase3Store()
	}
	if d.P3RedisAddr == "" {
		d.P3RedisAddr = "localhost:6379"
	}
	if d.P3DynamoEndpoint == "" {
		d.P3DynamoEndpoint = providers.DefaultDynamoDBEndpoint
	}
	if d.P3WorkdirRoot == "" {
		d.P3WorkdirRoot = d.WorkdirRoot
	}
	return &Server{
		Users: d.Users, Labs: d.Labs, Courses: d.Courses, Audit: d.Audit,
		Cache: d.Cache, Registry: d.Registry, AWS: d.AWS, Network: d.Network, Logger: d.Logger,
		HostAgent: d.HostAgent, HostAgentMode: d.HostAgentMode,
		Started: time.Now(), AdminEmail: d.AdminEmail,
		ProvisionAsync: d.ProvisionAsync, DBConnected: d.DBConnected,
		Resources: d.Resources, TerraformRuns: d.TerraformRuns,
		TerminalSessions: d.TerminalSessions, Jobs: d.Jobs,
		TerraformRunner:  d.TerraformRunner,
		Terminal:         terminal.NewService(d.Cache),
		EmulatorEndpoint: d.EmulatorEndpoint, MinIOEndpoint: d.MinIOEndpoint,
		WorkdirRoot: d.WorkdirRoot,
		Curriculum:  d.Curriculum, CurriculumDir: d.CurriculumDir,
		Advanced:    d.Advanced,
		DBHealth:    d.DBHealth,
		AuthLimiter: d.AuthLimiter, APILimiter: d.APILimiter,
		UserAPILimiter: d.UserAPILimiter, MetricsToken: d.MetricsToken,
		TLSEnabled: d.TLSEnabled, TrustedProxies: d.TrustedProxies,
		AuditExportRetention: d.AuditExportRetention,
		Quotas:               d.Quotas, Reconciler: d.Reconciler, Lite: d.Lite,
		IAM: d.IAM, Alarms: d.Alarms,
		P3: d.P3, P3DatabaseURL: d.P3DatabaseURL, P3RedisAddr: d.P3RedisAddr,
		P3DynamoEndpoint: d.P3DynamoEndpoint, P3WorkdirRoot: d.P3WorkdirRoot,
		P4RegistryURL: d.P4RegistryURL, P4BuildSourceRoot: d.P4BuildSourceRoot, P6MasterKey: d.P6MasterKey,
		elbRR: map[string]*phase3.RoundRobin{},
	}
}

// --- JSON helpers -------------------------------------------------------

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.writeJSON(w, r, status, errorEnvelope{Error: errorBody{Code: code, Message: message, RequestID: RequestID(r.Context())}})
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// --- middleware ---------------------------------------------------------

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.Logger.Info("http_request",
			"request_id", RequestID(r.Context()),
			"method", r.Method, "path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// statusRecorder captures the response status for metrics. It
// delegates Flush/Hijack so the terminal WebSocket (which hijacks
// the connection) keeps working behind the metrics middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
	}
	return h.Hijack()
}

func retryAfterSeconds(reset time.Time) int {
	secs := int(time.Until(reset).Seconds()) + 1
	if secs < 1 {
		secs = 1
	}
	return secs
}

// withAuthRateLimit throttles register/login per client IP
// (fail-CLOSED: a Redis error is a 503, never a silent bypass —
// brute-force protection must not disappear with the cache).
func (s *Server) withAuthRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AuthLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		res, err := s.AuthLimiter.Allow(r.Context(), s.clientIP(r))
		if err != nil {
			s.Logger.Warn("auth rate limiter unavailable", "error", err)
			s.writeError(w, r, http.StatusServiceUnavailable, "rate_limiter_unavailable", "request throttling is temporarily unavailable; try again shortly")
			return
		}
		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(s.AuthLimiter.Limit(), 10))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(res.Reset)))
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts; wait a minute and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAPIRateLimit throttles general /api/v1 traffic per client IP
// (fail-OPEN: a Redis error logs a warning and lets the request
// through, matching the fail-open cache design — rate limiting must
// not become a hard dependency for reads).
func (s *Server) withAPIRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.APILimiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		res, err := s.APILimiter.Allow(r.Context(), s.clientIP(r))
		if err != nil {
			s.Logger.Warn("api rate limiter unavailable; failing open", "error", err)
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(s.APILimiter.Limit(), 10))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(res.Reset)))
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests; wait a moment and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withMetrics records Prometheus request count/duration by method and
// chi route pattern (never the raw path, so ids stay out of labels).
// /metrics itself is excluded so scrapes do not feed the counters.
// The route pattern is read after next returns, when chi has matched.
func (s *Server) withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(rec.status)
		metrics.RequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		metrics.RequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// --- auth ---------------------------------------------------------------

func (s *Server) currentUser(r *http.Request) (*auth.User, bool) {
	// Phase 7: the session token comes from the HttpOnly cookie
	// first, then the Authorization header (API clients/tests).
	token := s.sessionToken(r)
	if token == "" {
		return nil, false
	}
	id, err := s.Users.ValidateToken(token)
	if err != nil {
		return nil, false
	}
	u, err := s.Users.GetByID(id)
	if err != nil {
		return nil, false
	}
	return u, true
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	u, ok := s.currentUser(r)
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "a valid Bearer token is required")
		return nil, false
	}
	return u, true
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return nil, false
	}
	if u.Role != auth.RoleAdmin {
		s.writeError(w, r, http.StatusForbidden, "forbidden", "admin role required")
		return nil, false
	}
	return u, true
}

// audit records a mutation best-effort: an audit failure is logged but
// never fails the user's request (the Postgres lab/course stores record
// their own audit rows inside the mutation transaction where possible).
// Metadata is scrubbed first (security.ScrubMap): a caller that
// accidentally passes a token or secret in metadata can never have it
// persisted to the audit log.
func (s *Server) audit(r *http.Request, actorID, action, resourceType, resourceID string, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	if err := s.Audit.Record(r.Context(), store.AuditEntry{
		ActorID: actorID, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, RequestID: RequestID(r.Context()), Metadata: security.ScrubMap(metadata),
	}); err != nil {
		s.Logger.Warn("audit record failed", "action", action, "error", err)
	}
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit, offset = 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// --- handlers -----------------------------------------------------------

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "ok"})
}

// componentHealth is one /readyz dependency check. Status is "ok",
// "down", "disabled" (dependency not configured — the API runs
// without it by design) or "not_configured".
type componentHealth struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// handleReadyz is the honest readiness probe (M9): it live-checks
// Postgres, Redis and the network engine on every call — nothing is
// cached or assumed from startup. The checks live in readiness()
// (monitoring.go), shared with GET /api/v1/monitoring.
//
// Overall readiness is gated ONLY on Postgres when it is
// configured: Redis is a fail-open cache by design (the API serves
// without it) and the engine is a provisioning dependency (the API
// serves auth/courses/labs metadata without it), so a Redis or
// engine outage is reported per-component but does not pull the API
// out of rotation. When Postgres is not configured (in-memory
// fallback mode) its check reports "disabled" and does not gate.
// A failed Postgres ping returns 503 with status "not_ready".
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	status, components, pgErr := s.readiness(ctx)
	code := http.StatusOK
	if pgErr != nil {
		code = http.StatusServiceUnavailable
	}
	s.writeJSON(w, r, code, map[string]any{
		"status": status, "components": components, "checked_at": time.Now().UTC(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// gRPC Health is the honest engine-health signal; the TCP probe
	// below only feeds the legacy network_engine_reachable boolean
	// and must never be read as engine health.
	grpcOK := false
	grpcDetail := "network engine client not configured"
	tcpOK := false
	tcpDetail := "network engine client not configured"
	if s.Network != nil {
		ctx := networkclient.ContextWithRequestID(r.Context(), RequestID(r.Context()))
		st, err := s.Network.Health(ctx)
		if err != nil {
			grpcDetail = "gRPC Health failed: " + err.Error()
		} else {
			grpcOK = st.GRPC && st.OK
			grpcDetail = st.Detail
			if !st.OK {
				grpcDetail = "engine reports not-ok: " + st.Detail
			}
		}
		tcpOK, tcpDetail = s.Network.TCPReachable(r.Context())
	}
	// Keep the engine gauge fresh here as well as in /readyz:
	// dashboards read cloudforge_network_engine_up, and /health is
	// the endpoint operators poll most often.
	metrics.SetEngineUp(grpcOK)
	redisConnected := false
	if s.Cache != nil {
		// Live check: a Redis that died after startup must be
		// reported as down, not as "connected at startup".
		redisConnected = s.Cache.Ping(r.Context())
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"service":                   "cloudforge-api",
		"version":                   Version,
		"ok":                        true,
		"uptime_seconds":            int64(time.Since(s.Started).Seconds()),
		"network_engine_grpc":       grpcOK,
		"network_engine_detail":     grpcDetail,
		"network_engine_reachable":  tcpOK,
		"network_engine_tcp_detail": tcpDetail,
		"database_connected":        s.DBConnected,
		"redis_connected":           redisConnected,
		"lite":                      s.Lite,
		"host_agent":                s.hostAgentHealth(r),
	})
}

// hostAgentHealth probes the configured host agent (2s timeout) and
// reports its mode and reachability honestly. Nil when no host agent
// is configured.
func (s *Server) hostAgentHealth(r *http.Request) any {
	if s.HostAgent == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	err := s.HostAgent.Health(ctx)
	mode := s.HostAgentMode
	if mode == "" {
		mode = "simulated"
	}
	detail := "host agent reachable"
	if err != nil {
		detail = err.Error()
	}
	metrics.SetHostAgentUp(err == nil)
	return map[string]any{
		"mode": mode, "reachable": err == nil, "detail": detail,
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	User      *auth.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Users.Register(req.Email, req.Name, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUserExists):
			s.writeError(w, r, http.StatusConflict, "user_exists", "a user with that email already exists")
		case errors.Is(err, auth.ErrInvalidEmail):
			s.writeError(w, r, http.StatusBadRequest, "invalid_email", err.Error())
		case errors.Is(err, auth.ErrPasswordTooShort):
			s.writeError(w, r, http.StatusBadRequest, "password_too_short", err.Error())
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return
	}
	// Bootstrap: the configured admin email becomes admin on register.
	if s.AdminEmail != "" && strings.EqualFold(u.Email, s.AdminEmail) && u.Role != auth.RoleAdmin {
		if admin, err := s.Users.SetRole(u.ID, auth.RoleAdmin); err == nil {
			u = admin
		} else {
			s.Logger.Warn("could not promote bootstrap admin", "email", u.Email, "error", err)
		}
	}
	s.audit(r, u.ID, "user_registered", "user", u.ID, map[string]any{"email": u.Email})
	token, exp := s.Users.IssueToken(u.ID)
	s.setSessionCookie(w, token, exp)
	s.writeJSON(w, r, http.StatusCreated, authResponse{Token: token, ExpiresAt: exp, User: u})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	u, err := s.Users.Authenticate(req.Email, req.Password)
	if err != nil {
		// Do not distinguish "unknown email" from "wrong password".
		// The attempt IS audited (Phase 6 GuardDuty reads it). The
		// actor is "" (stored as NULL): audit_logs.user_id is a UUID
		// column, so the literal "anonymous" made every failed-login
		// insert fail with "invalid input syntax for type uuid" and
		// GuardDuty silently saw zero evidence (found live 2026-11-22).
		s.audit(r, "", "auth.login_failed", "user", "", map[string]any{"client_ip": s.clientIP(r), "email": req.Email})
		s.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	s.audit(r, u.ID, "user_logged_in", "user", u.ID, nil)
	token, exp := s.Users.IssueToken(u.ID)
	s.setSessionCookie(w, token, exp)
	s.writeJSON(w, r, http.StatusOK, authResponse{Token: token, ExpiresAt: exp, User: u})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, u)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	users, err := s.Users.List()
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if users == nil {
		users = []*auth.User{}
	}
	s.audit(r, admin.ID, "users_listed", "user", "", nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"users": users})
}

// --- courses ------------------------------------------------------------

type courseView struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Track       string   `json:"track"`
	Description string   `json:"description"`
	Lessons     []string `json:"lessons,omitempty"`
	LessonCount int      `json:"lesson_count"`
}

func viewCourse(c *store.Course, detail bool) courseView {
	v := courseView{ID: c.ID, Title: c.Title, Track: c.Track, Description: c.Description, LessonCount: c.LessonCount()}
	if detail {
		v.Lessons = c.LessonTitles()
	}
	return v
}

func (s *Server) handleListCourses(w http.ResponseWriter, r *http.Request) {
	// Serve from Redis when available (60s TTL, invalidated on mutation).
	if s.Cache != nil && s.Cache.Available() {
		var cached []*store.Course
		if s.Cache.GetCourses(r.Context(), &cached) {
			views := make([]courseView, 0, len(cached))
			for _, c := range cached {
				views = append(views, viewCourse(c, false))
			}
			s.writeJSON(w, r, http.StatusOK, map[string]any{"courses": views, "cached": true})
			return
		}
	}
	list, err := s.Courses.List(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if s.Cache != nil {
		s.Cache.SetCourses(r.Context(), list)
	}
	views := make([]courseView, 0, len(list))
	for _, c := range list {
		views = append(views, viewCourse(c, false))
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"courses": views})
}

func (s *Server) handleGetCourse(w http.ResponseWriter, r *http.Request) {
	c, err := s.Courses.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, viewCourse(c, true))
}

type courseInput struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Track       string `json:"track"`
	Description string `json:"description"`
	Modules     []struct {
		Title   string   `json:"title"`
		Lessons []string `json:"lessons"`
	} `json:"modules,omitempty"`
}

func (s *Server) handleCreateCourse(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var in courseInput
	if !s.decodeJSON(w, r, &in) {
		return
	}
	c := &store.Course{ID: in.ID, Title: in.Title, Track: in.Track, Description: in.Description}
	for i, m := range in.Modules {
		mod := store.Module{Position: i + 1, Title: m.Title}
		for j, title := range m.Lessons {
			mod.Lessons = append(mod.Lessons, store.Lesson{Position: j + 1, Title: title})
		}
		c.Modules = append(c.Modules, mod)
	}
	created, err := s.Courses.Create(r.Context(), c)
	if err != nil {
		if errors.Is(err, store.ErrInvalidInput) {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if s.Cache != nil {
		s.Cache.InvalidateCourses(r.Context())
	}
	s.audit(r, admin.ID, "course_created", "course", created.ID, map[string]any{"title": created.Title})
	s.writeJSON(w, r, http.StatusCreated, viewCourse(created, true))
}

func (s *Server) handleUpdateCourse(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var in courseInput
	if !s.decodeJSON(w, r, &in) {
		return
	}
	updated, err := s.Courses.Update(r.Context(), chi.URLParam(r, "id"), in.Title, in.Track, in.Description)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
		case errors.Is(err, store.ErrInvalidInput):
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	if s.Cache != nil {
		s.Cache.InvalidateCourses(r.Context())
	}
	s.audit(r, admin.ID, "course_updated", "course", updated.ID, nil)
	s.writeJSON(w, r, http.StatusOK, viewCourse(updated, true))
}

func (s *Server) handleDeleteCourse(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.Courses.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if s.Cache != nil {
		s.Cache.InvalidateCourses(r.Context())
	}
	s.audit(r, admin.ID, "course_deleted", "course", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	c, err := s.Courses.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	modules := c.Modules
	if modules == nil {
		modules = []store.Module{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"modules": modules})
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	courseID := chi.URLParam(r, "id")
	if err := s.Courses.Enroll(r.Context(), u.ID, courseID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "course_enrolled", "course", courseID, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"enrolled": true, "course_id": courseID})
}

func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	prog, err := s.Courses.GetProgress(r.Context(), u.ID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "course_not_found", "no course with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, prog)
}

type completeLessonRequest struct {
	LessonID string `json:"lesson_id"`
}

func (s *Server) handleCompleteLesson(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req completeLessonRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.LessonID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "lesson_id is required")
		return
	}
	courseID := chi.URLParam(r, "id")
	prog, awarded, err := s.Courses.CompleteLesson(r.Context(), u.ID, courseID, req.LessonID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, r, http.StatusNotFound, "not_found", err.Error())
		case errors.Is(err, store.ErrNotEnrolled):
			s.writeError(w, r, http.StatusConflict, "not_enrolled", "enroll in the course before completing lessons")
		default:
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	if awarded > 0 {
		s.audit(r, u.ID, "lesson_completed", "lesson", req.LessonID, map[string]any{"course_id": courseID, "xp_awarded": awarded})
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"progress": prog, "xp_awarded": awarded})
}

// --- labs ---------------------------------------------------------------

type createLabRequest struct {
	DefinitionID   string `json:"definition_id"`
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) handleCreateLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req createLabRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	// Per-role quotas (M9 Part 3): lab count + lab duration, checked
	// before Create so the error names the configured role limit.
	if err := s.checkLabQuotas(r.Context(), u, req.DefinitionID); err != nil {
		if errors.Is(err, quota.ErrQuotaExceeded) {
			s.writeError(w, r, http.StatusTooManyRequests, "quota_exceeded", err.Error())
		} else {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return
	}
	lab, replayed, err := s.Labs.Create(r.Context(), u.ID, req.DefinitionID, req.Name, key)
	if err != nil {
		switch {
		case errors.Is(err, labs.ErrUnknownDefinition):
			s.writeError(w, r, http.StatusBadRequest, "unknown_definition", "unknown lab definition_id")
		case errors.Is(err, labs.ErrQuotaExceeded):
			s.writeError(w, r, http.StatusTooManyRequests, "quota_exceeded", "lab quota exceeded: at most 3 active labs per user")
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return
	}
	// The Postgres store already audits lab creation inside its
	// transaction; only audit here for the in-memory path.
	if !s.ProvisionAsync {
		s.audit(r, u.ID, "lab_created", "lab", lab.ID, map[string]any{"definition_id": lab.DefinitionID, "replayed": replayed})
	}
	if !replayed {
		metrics.LabEventsTotal.WithLabelValues("lab_created").Inc()
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	} else if s.ProvisionAsync {
		// Worker-driven provisioning: accepted, not yet provisioned.
		status = http.StatusAccepted
	}
	s.writeJSON(w, r, status, map[string]any{"lab": lab, "replayed": replayed})
}

func (s *Server) handleListLabs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	list, err := s.Labs.ListByOwner(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if list == nil {
		list = []*labs.Lab{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"labs": list})
}

func (s *Server) ownLab(w http.ResponseWriter, r *http.Request, u *auth.User) (*labs.Lab, bool) {
	lab, err := s.Labs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil || lab.OwnerID != u.ID {
		// 404 for other users' labs: do not leak their existence.
		s.writeError(w, r, http.StatusNotFound, "lab_not_found", "no lab with that id")
		return nil, false
	}
	return lab, true
}

func (s *Server) handleGetLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	// M8: enrich with the lab definition's validation rules (the
	// M6 validation panel data) and any active break-and-fix
	// attempt. Work on a copy so the stored lab is untouched.
	cp := *lab
	lab = &cp
	if s.Curriculum != nil {
		if def, err := s.Curriculum.GetLabDefinition(r.Context(), lab.DefinitionID); err == nil && len(def.ValidationRules) > 0 {
			rules := make([]map[string]any, 0, len(def.ValidationRules))
			for _, cr := range def.ValidationRules {
				rules = append(rules, map[string]any{
					"rule": cr.Rule, "params": cr.Params, "description": cr.Description,
				})
			}
			lab.ValidationRules = rules
		}
		if attempt, err := s.Curriculum.GetBreakFixAttemptByLab(r.Context(), lab.ID); err == nil {
			lab.ScenarioID = attempt.ScenarioID
			lab.FaultStatus = attempt.FaultStatus
		}
	}
	s.writeJSON(w, r, http.StatusOK, lab)
}

func (s *Server) handleStopLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownLab(w, r, u); !ok {
		return
	}
	lab, err := s.Labs.Stop(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, labs.ErrInvalidTransition) {
			s.writeError(w, r, http.StatusConflict, "invalid_transition", err.Error())
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !s.ProvisionAsync {
		s.audit(r, u.ID, "lab_stopped", "lab", lab.ID, nil)
	}
	s.writeJSON(w, r, http.StatusOK, lab)
}

func (s *Server) handleDestroyLab(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.ownLab(w, r, u); !ok {
		return
	}
	lab, err := s.Labs.Destroy(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, labs.ErrInvalidTransition) {
			s.writeError(w, r, http.StatusConflict, "invalid_transition", err.Error())
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !s.ProvisionAsync {
		s.audit(r, u.ID, "lab_destroyed", "lab", lab.ID, nil)
	}
	metrics.LabEventsTotal.WithLabelValues("lab_destroyed").Inc()
	s.writeJSON(w, r, http.StatusOK, lab)
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.Cache != nil && s.Cache.Available() {
		var cached []providers.Capability
		if s.Cache.GetCapabilities(r.Context(), &cached) {
			s.writeJSON(w, r, http.StatusOK, map[string]any{"capabilities": cached, "cached": true})
			return
		}
	}
	caps := s.Registry.List()
	if s.Cache != nil {
		s.Cache.SetCapabilities(r.Context(), caps)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"capabilities": caps})
}

// Routes returns the full HTTP handler tree (go-chi/chi v5, M9).
//
// Every route from the M1–M8 ServeMux is preserved verbatim: same
// methods, same paths, same {id} parameters (read with
// chi.URLParam), same handlers, same X-Request-ID/logging middleware
// order (request-ID outermost, so logging sees the correlation ID).
// Unknown paths still get chi's plain-text 404 and wrong-method
// requests a 405, as with the stdlib mux.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.withRequestID, s.withLogging, s.withMetrics, s.withSecurityHeaders)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	// Phase 7 (R5): /metrics requires the scrape token (or is
	// loopback-only in dev); nginx no longer proxies it.
	r.Get("/metrics", s.handleMetrics)
	// Phase 4: Docker Registry HTTP API subset (unauthenticated,
	// local-only — a docker client cannot present a CloudForge JWT).
	s.registerECRRegistryRoutes(r)
	r.Route("/api/v1", func(r chi.Router) {
		// General API limiter (fail-open) covers every /api/v1
		// route; /healthz, /readyz and /metrics are outside it.
		// The per-user limiter (Phase 7, R8) runs alongside it.
		r.Use(s.withAPIRateLimit, s.withUserAPIRateLimit)
		r.Get("/health", s.handleHealth)
		// Auth endpoints get the stricter, fail-closed limiter
		// on top of the general one.
		r.Group(func(r chi.Router) {
			r.Use(s.withAuthRateLimit)
			r.Post("/auth/register", s.handleRegister)
			r.Post("/auth/login", s.handleLogin)
		})
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/auth/me", s.handleMe)
		r.Get("/users/me", s.handleMe)
		r.Get("/users", s.handleListUsers)
		r.Get("/courses", s.handleListCourses)
		r.Post("/courses", s.handleCreateCourse)
		r.Get("/courses/{id}", s.handleGetCourse)
		r.Put("/courses/{id}", s.handleUpdateCourse)
		r.Delete("/courses/{id}", s.handleDeleteCourse)
		r.Get("/courses/{id}/modules", s.handleListModules)
		r.Post("/courses/{id}/enroll", s.handleEnroll)
		r.Get("/courses/{id}/progress", s.handleGetProgress)
		r.Post("/courses/{id}/progress", s.handleCompleteLesson)
		r.Post("/labs", s.handleCreateLab)
		r.Get("/labs", s.handleListLabs)
		r.Get("/labs/definitions", s.handleLabDefinitions)
		r.Get("/labs/{id}", s.handleGetLab)
		r.Post("/labs/{id}/start", s.handleStartLab)
		r.Post("/labs/{id}/stop", s.handleStopLab)
		r.Post("/labs/{id}/reboot", s.handleRebootLab)
		r.Post("/labs/{id}/destroy", s.handleDestroyLab)
		r.Get("/labs/{id}/events", s.handleLabEvents)
		// Milestone 6: lab resources, live topology, validation, terminal.
		r.Get("/labs/{id}/resources", s.handleListResources)
		r.Post("/labs/{id}/resources", s.handleCreateResource)
		r.Delete("/labs/{id}/resources/{resourceId}", s.handleDeleteResource)
		// Phase 2: VPC detail — routes, security-group rules, gateway
		// attach/detach, tags.
		r.Post("/labs/{id}/resources/{resourceId}/routes", s.handleAddRoute)
		r.Delete("/labs/{id}/resources/{resourceId}/routes/{routeId}", s.handleDeleteRoute)
		r.Post("/labs/{id}/resources/{resourceId}/rules", s.handleAddSGRule)
		r.Delete("/labs/{id}/resources/{resourceId}/rules/{ruleId}", s.handleDeleteSGRule)
		r.Post("/labs/{id}/resources/{resourceId}/attach-instance", s.handleAttachSGInstance)
		r.Post("/labs/{id}/resources/{resourceId}/attach", s.handleGatewayAttach)
		r.Post("/labs/{id}/resources/{resourceId}/detach", s.handleGatewayDetach)
		r.Put("/labs/{id}/resources/{resourceId}/tags", s.handleUpdateTags)
		r.Get("/labs/{id}/topology", s.handleLabTopology)
		r.Post("/labs/{id}/validate", s.handleValidateLab)
		r.Post("/labs/{id}/terminal/token", s.handleTerminalToken)
		r.Method(http.MethodGet, "/labs/{id}/terminal", s.terminalWSHandler())
		// Milestone 7: Terraform (async jobs; 202 with run id).
		r.Post("/labs/{id}/terraform/init", s.handleTerraformInit)
		r.Post("/labs/{id}/terraform/validate", s.handleTerraformValidate)
		r.Post("/labs/{id}/terraform/plan", s.handleTerraformPlan)
		r.Post("/labs/{id}/terraform/approve", s.handleTerraformApprove)
		r.Post("/labs/{id}/terraform/apply", s.handleTerraformApply)
		r.Post("/labs/{id}/terraform/destroy", s.handleTerraformDestroy)
		r.Get("/labs/{id}/terraform/runs", s.handleTerraformRuns)
		// Milestone 8: curriculum, progress, achievements, break-and-fix.
		r.Get("/curriculum/tracks", s.handleListTracks)
		r.Get("/curriculum/tracks/{id}", s.handleGetTrack)
		r.Post("/curriculum/tracks/{id}/enroll", s.handleEnrollTrack)
		r.Post("/curriculum/lessons/{lessonId}/complete", s.handleCompleteCurriculumLesson)
		r.Post("/curriculum/assessments/{trackId}/submit", s.handleSubmitAssessment)
		r.Get("/users/me/progress", s.handleMyProgress)
		r.Get("/users/me/achievements", s.handleMyAchievements)
		r.Post("/admin/seed-curriculum", s.handleSeedCurriculum)
		r.Post("/labs/{id}/break-fix/start", s.handleBreakFixStart)
		r.Post("/labs/{id}/break-fix/solve", s.handleBreakFixSolve)
		// Advanced section (v3 Workstream 3): catalogue, interviews,
		// skills checklist, checklist state, portfolio artefacts.
		r.Route("/advanced", func(r chi.Router) {
			r.Get("/projects", func(w http.ResponseWriter, req *http.Request) {
				if _, ok := s.requireAuth(w, req); !ok {
					return
				}
				s.Advanced.HandleProjects(w, req)
			})
			r.Get("/interviews", func(w http.ResponseWriter, req *http.Request) {
				if _, ok := s.requireAuth(w, req); !ok {
					return
				}
				s.Advanced.HandleInterviews(w, req)
			})
			r.Get("/skills-checklist", func(w http.ResponseWriter, req *http.Request) {
				if _, ok := s.requireAuth(w, req); !ok {
					return
				}
				s.Advanced.HandleSkillsChecklist(w, req)
			})
			r.Get("/checklist-state", func(w http.ResponseWriter, req *http.Request) {
				u, ok := s.requireAuth(w, req)
				if !ok {
					return
				}
				s.Advanced.HandleChecklistState(w, req, u.ID)
			})
			r.Put("/checklist-state", func(w http.ResponseWriter, req *http.Request) {
				u, ok := s.requireAuth(w, req)
				if !ok {
					return
				}
				s.Advanced.HandleChecklistState(w, req, u.ID)
			})
			r.Post("/portfolio/{project}", func(w http.ResponseWriter, req *http.Request) {
				u, ok := s.requireAuth(w, req)
				if !ok {
					return
				}
				s.Advanced.HandlePortfolio(w, req, u.ID)
			})
		})
		r.Get("/providers/capabilities", s.handleCapabilities)
		// Phase 1: backend-driven service registry + resource explorer.
		r.Get("/services", s.handleListServices)
		r.Get("/services/{id}", s.handleGetService)
		r.Get("/explorer/resources", s.handleExplorerResources)
		r.Get("/aws/{service}/capabilities", s.handleAWSCapabilities)
		r.Post("/aws/s3/buckets", s.handleS3CreateBucket)
		r.Get("/aws/s3/buckets", s.handleS3ListBuckets)
		r.Delete("/aws/s3/buckets/{bucket}", s.handleS3DeleteBucket)
		// Phase 2: S3 objects, versioning and bucket policy.
		r.Get("/aws/s3/buckets/{bucket}/objects", s.handleS3ListObjects)
		r.Post("/aws/s3/buckets/{bucket}/objects", s.handleS3UploadObject)
		r.Get("/aws/s3/buckets/{bucket}/objects/*", s.handleS3DownloadObject)
		r.Delete("/aws/s3/buckets/{bucket}/objects/*", s.handleS3DeleteObject)
		r.Get("/aws/s3/buckets/{bucket}/metadata/*", s.handleS3ObjectMetadata)
		r.Get("/aws/s3/buckets/{bucket}/versioning", s.handleS3GetVersioning)
		r.Put("/aws/s3/buckets/{bucket}/versioning", s.handleS3SetVersioning)
		r.Get("/aws/s3/buckets/{bucket}/policy", s.handleS3GetPolicy)
		r.Put("/aws/s3/buckets/{bucket}/policy", s.handleS3SetPolicy)
		// Phase 2: IAM identities (admin-only CRUD) + simulator.
		r.Post("/aws/iam/evaluate", s.handleIAMEvaluate)
		r.Get("/aws/iam/users", s.handleIAMListUsers)
		r.Post("/aws/iam/users", s.handleIAMCreateUser)
		r.Get("/aws/iam/users/{id}", s.handleIAMGetUser)
		r.Delete("/aws/iam/users/{id}", s.handleIAMDeleteUser)
		r.Post("/aws/iam/users/{id}/access-keys", s.handleIAMCreateAccessKey)
		r.Get("/aws/iam/users/{id}/access-keys", s.handleIAMListAccessKeys)
		r.Delete("/aws/iam/users/{id}/access-keys/{keyId}", s.handleIAMDeleteAccessKey)
		r.Post("/aws/iam/users/{id}/access-keys/{keyId}/status", s.handleIAMAccessKeyStatus)
		r.Post("/aws/iam/users/{id}/policies", s.handleIAMAttachPolicy("user"))
		r.Delete("/aws/iam/users/{id}/policies/{policyId}", s.handleIAMDetachPolicy("user"))
		r.Get("/aws/iam/groups", s.handleIAMListGroups)
		r.Post("/aws/iam/groups", s.handleIAMCreateGroup)
		r.Get("/aws/iam/groups/{id}", s.handleIAMGetGroup)
		r.Delete("/aws/iam/groups/{id}", s.handleIAMDeleteGroup)
		r.Post("/aws/iam/groups/{id}/members", s.handleIAMAddGroupMember)
		r.Delete("/aws/iam/groups/{id}/members/{userId}", s.handleIAMRemoveGroupMember)
		r.Post("/aws/iam/groups/{id}/policies", s.handleIAMAttachPolicy("group"))
		r.Delete("/aws/iam/groups/{id}/policies/{policyId}", s.handleIAMDetachPolicy("group"))
		r.Get("/aws/iam/roles", s.handleIAMListRoles)
		r.Post("/aws/iam/roles", s.handleIAMCreateRole)
		r.Get("/aws/iam/roles/{id}", s.handleIAMGetRole)
		r.Delete("/aws/iam/roles/{id}", s.handleIAMDeleteRole)
		r.Post("/aws/iam/roles/{id}/assume", s.handleIAMAssumeRole)
		r.Post("/aws/iam/roles/{id}/policies", s.handleIAMAttachPolicy("role"))
		r.Delete("/aws/iam/roles/{id}/policies/{policyId}", s.handleIAMDetachPolicy("role"))
		r.Get("/aws/iam/sessions/{id}", s.handleIAMGetSession)
		r.Get("/aws/iam/policies", s.handleIAMListPolicies)
		r.Post("/aws/iam/policies", s.handleIAMCreatePolicy)
		r.Get("/aws/iam/policies/{id}", s.handleIAMGetPolicy)
		r.Delete("/aws/iam/policies/{id}", s.handleIAMDeletePolicy)
		// Phase 2: monitoring + audit UI backend.
		r.Get("/monitoring", s.handleMonitoring)
		r.Post("/monitoring/alarms", s.handleCreateAlarm)
		r.Delete("/monitoring/alarms/{id}", s.handleDeleteAlarm)
		r.Get("/audit", s.handleListAudit)
		r.Get("/audit/export", s.handleAuditExport)
		// Phase 7 (R10): durable audit export jobs with retention.
		r.Post("/audit/export/jobs", s.handleCreateAuditExportJob)
		r.Get("/audit/export/jobs", s.handleListAuditExportJobs)
		r.Get("/audit/export/jobs/{id}", s.handleGetAuditExportJob)
		r.Post("/admin/users/{id}/sessions/revoke", s.handleRevokeUserSessions)
		// Phase 3: data, messaging and serverless.
		s.registerPhase3Routes(r)
		// Phase 4: containers and developer tools.
		s.registerPhase4Routes(r)
		// Phase 5: Terraform Learning.
		s.registerTFlearnRoutes(r)
		// Phase 6: advanced services.
		s.registerPhase6Routes(r)
		// v3: exam services (SAA-C03 / SAP-C02) in lite mode.
		s.registerExamRoutes(r)
		r.Get("/admin/quotas", s.handleGetQuotas)
		r.Put("/admin/quotas", s.handleUpdateQuotas)
		r.Post("/admin/cleanup-reconcile", s.handleCleanupReconcile)
	})
	return r
}
