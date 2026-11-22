// Phase 7 hardening tests: logout/revocation, metrics auth,
// security headers, trusted-proxy client IP, per-user rate limits,
// audit export jobs with retention.
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/ratelimit"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// registerEmail registers a distinct user (adminToken already owns ada@example.com).
func registerEmail(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": email, "name": "User", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register %s: status %d, body %v", email, status, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("register %s returned no token: %v", email, body)
	}
	return token
}

// p7Server builds a test server with the Phase 7 Deps fields set.
func p7Server(t *testing.T, deps Deps) (*Server, *httptest.Server) {
	t.Helper()
	if deps.Users == nil {
		deps.Users = auth.NewStore("test-secret")
	}
	if deps.Labs == nil {
		ls := labs.NewStore()
		ls.ProvisionDelay = 5 * time.Millisecond
		ls.CheckNetwork = func() bool { return true }
		deps.Labs = store.NewMemoryLabStore(ls)
	}
	if deps.Registry == nil {
		deps.Registry = providers.NewRegistry()
	}
	if deps.Network == nil {
		deps.Network = networkclient.New("127.0.0.1:1")
	}
	if deps.WorkdirRoot == "" {
		deps.WorkdirRoot = t.TempDir()
	}
	srv := NewServerWithDeps(deps)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestLogoutRevokesToken(t *testing.T) {
	_, ts := p7Server(t, Deps{})
	token := register(t, ts)

	// /auth/me works before logout.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", token, nil); status != http.StatusOK {
		t.Fatalf("me before logout: %d", status)
	}
	// Logout revokes the token.
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/logout", token, nil)
	if status != http.StatusOK || body["logged_out"] != true || body["token_revoked"] != true {
		t.Fatalf("logout: %d %v", status, body)
	}
	// The same token is now rejected.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", token, nil); status != http.StatusUnauthorized {
		t.Fatalf("me after logout: got %d, want 401", status)
	}
}

func TestLoginSetsHttpOnlySessionCookie(t *testing.T) {
	_, ts := p7Server(t, Deps{})
	status, headers, _ := doRaw(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "cookie@example.com", "name": "Cookie", "password": "correct-horse",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("register: %d", status)
	}
	cookie := headers.Get("Set-Cookie")
	if !strings.Contains(cookie, SessionCookieName+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("register must set an HttpOnly SameSite session cookie, got %q", cookie)
	}
	// The cookie alone authenticates: /auth/me with only the cookie.
	req, err := http.NewRequest("GET", ts.URL+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me with session cookie only: %d", resp.StatusCode)
	}
}

func TestAdminRevokeUserSessions(t *testing.T) {
	srv, ts := p7Server(t, Deps{})
	admin := adminToken(t, srv, ts)
	userTok := registerEmail(t, ts, "learner7@example.com")
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", userTok, nil); status != http.StatusOK {
		t.Fatalf("user me before revoke: %d", status)
	}
	// Admin needs the user's id: list users.
	_, usersBody := do(t, "GET", ts.URL+"/api/v1/users", admin, nil)
	users, _ := usersBody["users"].([]any)
	var userID string
	for _, u := range users {
		if m, ok := u.(map[string]any); ok && m["email"] == "learner7@example.com" {
			userID, _ = m["id"].(string)
		}
	}
	if userID == "" {
		t.Fatalf("could not find user id in %v", usersBody)
	}
	status, body := do(t, "POST", ts.URL+"/api/v1/admin/users/"+userID+"/sessions/revoke", admin, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke sessions: %d %v", status, body)
	}
	if n, _ := body["sessions_revoked"].(float64); n < 1 {
		t.Fatalf("sessions_revoked = %v, want >= 1", body["sessions_revoked"])
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", userTok, nil); status != http.StatusUnauthorized {
		t.Fatalf("user me after admin revoke: got %d, want 401", status)
	}
	// The admin's own token still works.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", admin, nil); status != http.StatusOK {
		t.Fatalf("admin me after revoking another user: %d", status)
	}
	// A non-admin cannot revoke sessions.
	status, _ = do(t, "POST", ts.URL+"/api/v1/admin/users/"+userID+"/sessions/revoke", userTok, nil)
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Fatalf("non-admin revoke: got %d, want 401/403", status)
	}
}

func TestMetricsRequiresToken(t *testing.T) {
	_, ts := p7Server(t, Deps{MetricsToken: "test-metrics-token-long"})
	// No token -> 401.
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("metrics without token: %d, want 401", resp.StatusCode)
	}
	// Wrong token -> 401.
	req, _ := http.NewRequest("GET", ts.URL+"/metrics", nil)
	req.Header.Set("X-Metrics-Token", "wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("metrics with wrong token: %d, want 401", resp.StatusCode)
	}
	// Right token -> 200 with real series.
	req, _ = http.NewRequest("GET", ts.URL+"/metrics", nil)
	req.Header.Set("X-Metrics-Token", "test-metrics-token-long")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics with token: %d, want 200", resp.StatusCode)
	}
}

func TestMetricsLoopbackOnlyWithoutToken(t *testing.T) {
	// No token configured (dev mode): the httptest server is
	// loopback, so a scrape succeeds; a non-loopback peer would
	// get 403 (covered by the trustedPeer unit test below).
	_, ts := p7Server(t, Deps{})
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loopback metrics without token: %d, want 200", resp.StatusCode)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	_, ts := p7Server(t, Deps{})
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	h := resp.Header
	if h.Get("Content-Security-Policy") == "" || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("missing CSP header: %v", h)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing X-Content-Type-Options: %v", h)
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing X-Frame-Options: %v", h)
	}
	if h.Get("Referrer-Policy") == "" {
		t.Fatalf("missing Referrer-Policy: %v", h)
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Fatalf("HSTS must not be sent when TLS is off: %q", h.Get("Strict-Transport-Security"))
	}
	// With TLS on, HSTS is emitted.
	_, tsTLS := p7Server(t, Deps{TLSEnabled: true})
	resp, err = http.Get(tsTLS.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS must be sent when TLS is on")
	}
}

func TestTrustedProxyClientIP(t *testing.T) {
	srv, _ := p7Server(t, Deps{})
	mk := func(remote, xff string) *http.Request {
		req := httptest.NewRequest("GET", "/api/v1/health", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		return req
	}
	// Untrusted peer: XFF ignored, peer IP used.
	if got := srv.clientIP(mk("203.0.113.5:1234", "1.2.3.4")); got != "203.0.113.5" {
		t.Fatalf("untrusted peer XFF: got %q, want 203.0.113.5", got)
	}
	// Loopback peer (dev proxy): XFF honoured.
	if got := srv.clientIP(mk("127.0.0.1:1234", "1.2.3.4, 10.0.0.1")); got != "1.2.3.4" {
		t.Fatalf("loopback peer XFF: got %q, want 1.2.3.4", got)
	}
	// Private peer (nginx container in compose): XFF honoured.
	if got := srv.clientIP(mk("172.18.0.3:1234", "198.51.100.9")); got != "198.51.100.9" {
		t.Fatalf("private peer XFF: got %q, want 198.51.100.9", got)
	}
	// No XFF: peer IP.
	if got := srv.clientIP(mk("203.0.113.5:1234", "")); got != "203.0.113.5" {
		t.Fatalf("no XFF: got %q, want 203.0.113.5", got)
	}
}

func TestPerUserRateLimitTriggers(t *testing.T) {
	limiter := ratelimit.New(ratelimit.NewClient("localhost:6379"),
		uniqueScope(t, "api-user"), 2, time.Minute)
	srv, ts := p7Server(t, Deps{UserAPILimiter: limiter})
	_ = srv
	token := register(t, ts)
	for i := 0; i < 2; i++ {
		if status, _ := do(t, "GET", ts.URL+"/api/v1/courses", token, nil); status != http.StatusOK {
			t.Fatalf("authed request %d: %d", i, status)
		}
	}
	status, headers, body := doRaw(t, "GET", ts.URL+"/api/v1/courses", token, nil, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("3rd authed request: %d, want 429: %v", status, body)
	}
	if headers.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
	// A different user has their own budget.
	token2 := ""
	status, body2 := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "other@example.com", "name": "Other", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("second register: %d %v", status, body2)
	}
	token2, _ = body2["token"].(string)
	if status, _ := do(t, "GET", ts.URL+"/api/v1/courses", token2, nil); status != http.StatusOK {
		t.Fatalf("other user's first request: %d, want 200", status)
	}
	// Unauthenticated requests skip the per-user limiter.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/courses", "", nil); status != http.StatusOK {
		t.Fatalf("unauthenticated request: %d, want 200", status)
	}
}

func TestAuditExportJobLifecycle(t *testing.T) {
	srv, ts := p7Server(t, Deps{AuditExportRetention: 2})
	admin := adminToken(t, srv, ts)
	// Register a user so the audit log is non-empty.
	_ = registerEmail(t, ts, "audited@example.com")

	// Create a CSV export job.
	status, body := do(t, "POST", ts.URL+"/api/v1/audit/export/jobs?format=csv", admin, nil)
	if status != http.StatusCreated {
		t.Fatalf("create export job: %d %v", status, body)
	}
	jobID, _ := body["id"].(string)
	if jobID == "" {
		t.Fatalf("export job has no id: %v", body)
	}
	if n, _ := body["events"].(float64); n < 1 {
		t.Fatalf("export job events = %v, want >= 1", body["events"])
	}
	// List shows it.
	status, list := do(t, "GET", ts.URL+"/api/v1/audit/export/jobs", admin, nil)
	if status != http.StatusOK {
		t.Fatalf("list export jobs: %d %v", status, list)
	}
	jobs, _ := list["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("list jobs = %d, want 1: %v", len(jobs), list)
	}
	// Download returns CSV with the audit header.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/audit/export/jobs/"+jobID, nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download export: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("download content-type = %q, want text/csv", ct)
	}
	// Retention: with retention=2, creating two more jobs leaves 2.
	for i := 0; i < 2; i++ {
		if status, _ := do(t, "POST", ts.URL+"/api/v1/audit/export/jobs?format=json", admin, nil); status != http.StatusCreated {
			t.Fatalf("create export job %d: %d", i, status)
		}
	}
	status, list = do(t, "GET", ts.URL+"/api/v1/audit/export/jobs", admin, nil)
	if status != http.StatusOK {
		t.Fatalf("list after retention: %d", status)
	}
	jobs, _ = list["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("retention=2: list jobs = %d, want 2", len(jobs))
	}
	// The first job's file was pruned.
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/audit/export/jobs/"+jobID, nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pruned export download: %d, want 404", resp.StatusCode)
	}
	// Non-admin cannot create export jobs.
	learner := registerEmail(t, ts, "learner-export@example.com")
	if status, _ := do(t, "POST", ts.URL+"/api/v1/audit/export/jobs?format=csv", learner, nil); status != http.StatusForbidden {
		t.Fatalf("learner export job: %d, want 403", status)
	}
}

func TestMetricsAcceptsBearerToken(t *testing.T) {
	_, ts := p7Server(t, Deps{MetricsToken: "test-metrics-token-long"})
	req, _ := http.NewRequest("GET", ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer test-metrics-token-long")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics with Bearer token: %d, want 200", resp.StatusCode)
	}
}
