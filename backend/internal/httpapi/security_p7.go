// Phase 7 production hardening (risks R3, R5–R8, R10).
//
// This file holds the security middleware and handlers added in
// Phase 7; the pre-existing middleware lives in httpapi.go.
//
//   - Security headers (CSP, X-Content-Type-Options,
//     X-Frame-Options, Referrer-Policy, HSTS when TLS is on) on
//     every response.
//   - Trusted-proxy client IP: X-Forwarded-For is honoured only
//     when the peer is loopback, a private (RFC 1918 / ULA)
//     address — the nginx proxy in compose — or an explicitly
//     configured trusted proxy. Any other peer's XFF is ignored,
//     so a direct caller cannot spoof an IP to defeat the
//     per-IP auth limiter (R7). Residual risk: the API port
//     must not be published to the open internet; see
//     docs/SECURITY.md.
//   - Cookie sessions: login/register set an HttpOnly SameSite
//     session cookie (Secure when TLS is on); currentUser reads
//     the cookie first, then the Authorization header, so API
//     clients and tests keep working during the transition (R6).
//   - Logout + admin session revocation (R3).
//   - /metrics authentication (R5).
//   - Per-user API rate limiting in addition to per-IP (R8).
//   - Audit export jobs with retention (R10).
package httpapi

import (
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/metrics"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// SessionCookieName is the HttpOnly session cookie (Phase 7).
const SessionCookieName = "cf_session"

// --- security headers (R6, R12) -----------------------------------------

// withSecurityHeaders adds the security headers to every response.
// The CSP is deliberately tight for a same-origin app: everything
// loads from 'self'; inline styles are allowed because the React
// app (Tailwind) emits them, but inline scripts are not. HSTS is
// emitted only when TLS is on — sending it over plain HTTP would
// be a lie and could brick a local dev host.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.TLSEnabled {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// --- trusted proxy (R7) ---------------------------------------------------

// trustedPeer reports whether the TCP peer may set X-Forwarded-For:
// loopback (dev / same-host proxy), a private address (the nginx
// container on the compose networks), or an explicitly configured
// trusted proxy IP/CIDR.
func (s *Server) trustedPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	for _, p := range s.TrustedProxies {
		if p == host {
			return true
		}
		if _, cidr, err := net.ParseCIDR(p); err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the caller IP for rate limiting and audit: the
// first X-Forwarded-For entry only when the peer is trusted (see
// trustedPeer); otherwise the peer address without its port. This
// replaces the old unconditional trust in httpapi.go.
func (s *Server) clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" && s.trustedPeer(r) {
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

// --- per-user rate limiting (R8) ------------------------------------------

// withUserAPIRateLimit throttles authenticated /api/v1 traffic per
// user id (in addition to the per-IP limiter). A classroom behind
// one NAT no longer lets one heavy user eat the whole shared IP
// budget silently — each user gets their own budget, and the IP
// budget still caps the aggregate. Fail-open on Redis error, like
// the general API limiter; unauthenticated requests skip it (the
// per-IP limiter still applies).
func (s *Server) withUserAPIRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.UserAPILimiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		u, ok := s.currentUser(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		res, err := s.UserAPILimiter.Allow(r.Context(), "user:"+u.ID)
		if err != nil {
			s.Logger.Warn("per-user rate limiter unavailable; failing open", "error", err)
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-RateLimit-User-Limit", strconv.FormatInt(s.UserAPILimiter.Limit(), 10))
		w.Header().Set("X-RateLimit-User-Remaining", strconv.FormatInt(res.Remaining, 10))
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(res.Reset)))
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests for this user; wait a moment and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- session cookie (R6) --------------------------------------------------

// setSessionCookie issues the HttpOnly SameSite session cookie.
// Secure is set only when TLS is on: a Secure cookie over plain
// HTTP would never be sent back, which would silently break login
// in local dev.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   s.TLSEnabled,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.TLSEnabled,
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionToken extracts the session token: the HttpOnly cookie
// first (the browser path), then the Authorization header (API
// clients, tests, and the transition period).
func (s *Server) sessionToken(r *http.Request) string {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// handleLogout revokes the caller's session token (R3) and clears
// the cookie. The token is rejected with 401 from this point on,
// even though its 12h expiry has not passed.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	token := s.sessionToken(r)
	revoked := false
	if token != "" {
		revoked = s.Users.RevokeToken(token)
	}
	s.clearSessionCookie(w)
	s.audit(r, u.ID, "user_logged_out", "user", u.ID, map[string]any{"token_revoked": revoked})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"logged_out": true, "token_revoked": revoked})
}

// handleRevokeUserSessions (admin) revokes every session issued to
// a user — the "log them out everywhere" control (R3). Returns how
// many sessions were revoked.
func (s *Server) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	targetID := chi.URLParam(r, "id")
	if _, err := s.Users.GetByID(targetID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "user_not_found", "no user with that id")
		return
	}
	n := s.Users.RevokeUser(targetID)
	s.audit(r, admin.ID, "user_sessions_revoked", "user", targetID, map[string]any{"sessions_revoked": n})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"user_id": targetID, "sessions_revoked": n})
}

// --- /metrics authentication (R5) ----------------------------------------

// handleMetrics serves the Prometheus exposition only to an
// authenticated scraper: when a metrics token is configured, the
// X-Metrics-Token header OR an Authorization: Bearer token must
// match (constant-time compare; Prometheus sends the latter — see
// infrastructure/monitoring/prometheus.yml). Otherwise (dev mode,
// no token) only loopback peers may scrape. nginx no longer
// proxies /metrics at all (frontend/nginx.conf), so the endpoint
// is not publicly reachable through the frontend.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.MetricsToken != "" {
		got := r.Header.Get("X-Metrics-Token")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.MetricsToken)) != 1 {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "a valid X-Metrics-Token header (or Bearer token) is required")
			return
		}
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			s.writeError(w, r, http.StatusForbidden, "forbidden", "metrics are loopback-only without a metrics token")
			return
		}
	}
	metrics.Handler().ServeHTTP(w, r)
}

// --- audit export jobs (R10) ----------------------------------------------

// An audit export job is a durable file under
// <workdir>/audit-exports/<id>.csv|json plus a sidecar
// <id>.meta.json carrying the job record. Files (not just DB rows)
// are the archive: the worker's audit_cleanup job exports rows
// older than 90 days to a file here before deleting them, and the
// retention policy keeps only the newest N export files.
type auditExportJob struct {
	ID        string    `json:"id"`
	Format    string    `json:"format"`
	Events    int       `json:"events"`
	Total     int       `json:"total"`
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	Filename  string    `json:"filename"`
}

func (s *Server) auditExportDir() string {
	return filepath.Join(s.WorkdirRoot, "audit-exports")
}

// writeAuditExport writes entries to dir as <id>.<format> plus a
// <id>.meta.json sidecar, and returns the job record.
func writeAuditExport(dir, format string, entries []*store.AuditEntry, total int, createdBy string) (*auditExportJob, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	id := newRequestID()
	job := &auditExportJob{
		ID: id, Format: format, Events: len(entries), Total: total,
		CreatedAt: time.Now().UTC(), CreatedBy: createdBy,
		Filename: fmt.Sprintf("%s.%s", id, format),
	}
	path := filepath.Join(dir, job.Filename)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		enc := json.NewEncoder(f)
		if err := enc.Encode(map[string]any{"events": entries, "total": total, "exported": len(entries)}); err != nil {
			_ = f.Close()
			return nil, err
		}
	default:
		cw := csv.NewWriter(f)
		_ = cw.Write([]string{"id", "created_at", "actor_id", "action", "resource_type", "resource_id", "request_id"})
		for _, e := range entries {
			_ = cw.Write([]string{e.ID, e.CreatedAt.Format(time.RFC3339), e.ActorID, e.Action, e.ResourceType, e.ResourceID, e.RequestID})
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil {
		job.Bytes = st.Size()
	}
	meta, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, id+".meta.json"), meta, 0o644); err != nil {
		return nil, err
	}
	return job, nil
}

// listAuditExportJobs reads the durable job records from dir.
func listAuditExportJobs(dir string) ([]*auditExportJob, error) {
	metas, err := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	if err != nil {
		return nil, err
	}
	var jobs []*auditExportJob
	for _, m := range metas {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var job auditExportJob
		if err := json.Unmarshal(b, &job); err != nil {
			continue
		}
		jobs = append(jobs, &job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs, nil
}

// pruneAuditExports deletes all but the newest keep export jobs
// (data file + sidecar). keep <= 0 keeps everything.
func pruneAuditExports(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	jobs, err := listAuditExportJobs(dir)
	if err != nil {
		return err
	}
	if len(jobs) <= keep {
		return nil
	}
	for _, job := range jobs[keep:] {
		_ = os.Remove(filepath.Join(dir, job.Filename))
		_ = os.Remove(filepath.Join(dir, job.ID+".meta.json"))
	}
	return nil
}

// handleCreateAuditExportJob (admin) runs a full filtered export as
// a durable job file (R10): the response carries the job record;
// the file is downloadable from GET .../jobs/{id}. At most 10,000
// events per job; the total tells the caller when it was partial.
func (s *Server) handleCreateAuditExportJob(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "format must be csv or json")
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// parseAuditFilter caps the limit at 100 (the list endpoint's
	// cap); an export job may ask for up to 10,000 events.
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 10000 {
				n = 10000
			}
			f.Limit = n
		}
	}
	entries, total, err := s.Audit.ListFiltered(r.Context(), f)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	job, err := writeAuditExport(s.auditExportDir(), format, entries, total, admin.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "could not write export file: "+err.Error())
		return
	}
	keep := s.AuditExportRetention
	if keep <= 0 {
		keep = 7
	}
	_ = pruneAuditExports(s.auditExportDir(), keep)
	s.audit(r, admin.ID, "audit_export_job_created", "audit", job.ID, map[string]any{"format": format, "events": job.Events, "total": total})
	s.writeJSON(w, r, http.StatusCreated, job)
}

// handleListAuditExportJobs (admin) lists export jobs, newest first.
func (s *Server) handleListAuditExportJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	jobs, err := listAuditExportJobs(s.auditExportDir())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if jobs == nil {
		jobs = []*auditExportJob{}
	}
	keep := s.AuditExportRetention
	if keep <= 0 {
		keep = 7
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"jobs": jobs, "retention": keep})
}

// handleGetAuditExportJob (admin) downloads one export file.
func (s *Server) handleGetAuditExportJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if strings.Contains(id, "/") || strings.Contains(id, ".") {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid export id")
		return
	}
	metaPath := filepath.Join(s.auditExportDir(), id+".meta.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no audit export with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	var job auditExportJob
	if err := json.Unmarshal(b, &job); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "corrupt export metadata")
		return
	}
	data, err := os.ReadFile(filepath.Join(s.auditExportDir(), job.Filename))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "export file is gone")
		return
	}
	ct := "text/csv"
	if job.Format == "json" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, job.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
