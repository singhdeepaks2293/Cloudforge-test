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
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/jobs"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/metrics"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/ratelimit"
	"github.com/cloudforge/cloudforge/backend/internal/security"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terminal"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
	"github.com/go-chi/chi/v5"
)

// Version is the M9 API version string.
const Version = "0.9.0-m9"

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
	// Milestone 9: live Postgres health probe for /readyz. Nil when
	// the API runs on the in-memory fallback (no Postgres); /readyz
	// then reports postgres as "disabled" instead of failing.
	DBHealth func(ctx context.Context) error
	// Milestone 9: Redis-backed rate limiters. Nil means "not
	// configured" and disables that limiter (existing tests and
	// Redis-less dev runs are unthrottled).
	AuthLimiter *ratelimit.Limiter // register/login; fail-closed on Redis error
	APILimiter  *ratelimit.Limiter // general /api/v1; fail-open on Redis error
	// Milestone 9, Part 3: per-role quota sets. Nil gets an
	// in-memory default (seeded with quota.DefaultLimits) in
	// NewServerWithDeps.
	Quotas store.QuotaStore
	// Milestone 9, Part 3: cleanup reconciler. Nil means the
	// handler builds a jobs.Service from this server's own
	// stores/engine/host-agent on demand.
	Reconciler jobs.Reconciler
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
	DBHealth         func(ctx context.Context) error
	AuthLimiter      *ratelimit.Limiter
	APILimiter       *ratelimit.Limiter
	Quotas           store.QuotaStore
	Reconciler       jobs.Reconciler
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
		DBHealth:    d.DBHealth,
		AuthLimiter: d.AuthLimiter, APILimiter: d.APILimiter,
		Quotas: d.Quotas, Reconciler: d.Reconciler,
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

// clientIP returns the caller IP for rate limiting: the first
// X-Forwarded-For entry when present (the deployment sits behind
// nginx), otherwise the peer address without its port.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
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
		res, err := s.AuthLimiter.Allow(r.Context(), clientIP(r))
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
		res, err := s.APILimiter.Allow(r.Context(), clientIP(r))
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
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, false
	}
	id, err := s.Users.ValidateToken(strings.TrimPrefix(h, "Bearer "))
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
// cached or assumed from startup.
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
	components := map[string]componentHealth{}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Postgres (gating when configured).
	start := time.Now()
	pgErr := error(nil)
	if s.DBHealth == nil {
		components["postgres"] = componentHealth{Status: "disabled", LatencyMs: time.Since(start).Milliseconds(), Error: "postgres not configured (in-memory fallback)"}
	} else {
		pgErr = s.DBHealth(ctx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		if pgErr != nil {
			c.Status = "down"
			c.Error = pgErr.Error()
		}
		components["postgres"] = c
	}

	// Redis (reported, never gating: fail-open cache).
	start = time.Now()
	if s.Cache == nil || !s.Cache.Available() {
		components["redis"] = componentHealth{Status: "disabled", LatencyMs: time.Since(start).Milliseconds(), Error: "redis not configured or unavailable"}
	} else {
		ok := s.Cache.Ping(ctx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		if !ok {
			c.Status = "down"
			c.Error = "redis ping failed"
		}
		components["redis"] = c
	}

	// Network engine (reported, never gating).
	start = time.Now()
	engineUp := false
	if s.Network == nil {
		components["engine"] = componentHealth{Status: "not_configured", LatencyMs: time.Since(start).Milliseconds(), Error: "network engine client not configured"}
	} else {
		ectx := networkclient.ContextWithRequestID(ctx, RequestID(r.Context()))
		st, err := s.Network.Health(ectx)
		c := componentHealth{Status: "ok", LatencyMs: time.Since(start).Milliseconds()}
		switch {
		case err != nil:
			c.Status = "down"
			c.Error = err.Error()
		case !st.OK:
			c.Status = "down"
			c.Error = st.Detail
		default:
			engineUp = true
		}
		components["engine"] = c
	}
	metrics.SetEngineUp(engineUp)

	status := "ready"
	code := http.StatusOK
	if pgErr != nil {
		status = "not_ready"
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
		s.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	s.audit(r, u.ID, "user_logged_in", "user", u.ID, nil)
	token, exp := s.Users.IssueToken(u.ID)
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

// --- audit --------------------------------------------------------------

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	limit, offset := parsePagination(r)
	entries, total, err := s.Audit.List(r.Context(), limit, offset)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	_ = admin
	s.writeJSON(w, r, http.StatusOK, map[string]any{"audit": entries, "total": total, "limit": limit, "offset": offset})
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
	r.Use(s.withRequestID, s.withLogging, s.withMetrics)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Method(http.MethodGet, "/metrics", metrics.Handler())
	r.Route("/api/v1", func(r chi.Router) {
		// General API limiter (fail-open) covers every /api/v1
		// route; /healthz, /readyz and /metrics are outside it.
		r.Use(s.withAPIRateLimit)
		r.Get("/health", s.handleHealth)
		// Auth endpoints get the stricter, fail-closed limiter
		// on top of the general one.
		r.Group(func(r chi.Router) {
			r.Use(s.withAuthRateLimit)
			r.Post("/auth/register", s.handleRegister)
			r.Post("/auth/login", s.handleLogin)
		})
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
		r.Get("/labs/{id}", s.handleGetLab)
		r.Post("/labs/{id}/stop", s.handleStopLab)
		r.Post("/labs/{id}/destroy", s.handleDestroyLab)
		// Milestone 6: lab resources, live topology, validation, terminal.
		r.Get("/labs/{id}/resources", s.handleListResources)
		r.Post("/labs/{id}/resources", s.handleCreateResource)
		r.Delete("/labs/{id}/resources/{resourceId}", s.handleDeleteResource)
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
		r.Get("/providers/capabilities", s.handleCapabilities)
		r.Get("/aws/{service}/capabilities", s.handleAWSCapabilities)
		r.Post("/aws/s3/buckets", s.handleS3CreateBucket)
		r.Get("/aws/s3/buckets", s.handleS3ListBuckets)
		r.Post("/aws/iam/evaluate", s.handleIAMEvaluate)
		r.Get("/audit", s.handleListAudit)
		r.Get("/admin/quotas", s.handleGetQuotas)
		r.Put("/admin/quotas", s.handleUpdateQuotas)
		r.Post("/admin/cleanup-reconcile", s.handleCleanupReconcile)
	})
	return r
}
