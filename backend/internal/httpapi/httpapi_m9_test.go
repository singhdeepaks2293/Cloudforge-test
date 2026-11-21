package httpapi

// Milestone 9 tests: /readyz honesty, Prometheus /metrics, rate
// limiting (step 4) and chi routing (step 2) behaviour.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

func TestReadyzReportsComponentsHonestly(t *testing.T) {
	_, ts := testServer(t)
	// testServer: no Postgres (DBHealth nil), no Redis cache, engine
	// pointed at 127.0.0.1:1 (down). Postgres/Redis are "disabled"
	// and do not gate; the dead engine is reported but also does
	// not gate, so overall status is still ready.
	status, body := do(t, "GET", ts.URL+"/readyz", "", nil)
	if status != http.StatusOK {
		t.Fatalf("readyz: status %d, body %v", status, body)
	}
	if body["status"] != "ready" {
		t.Fatalf("readyz status: %v", body)
	}
	components, ok := body["components"].(map[string]any)
	if !ok {
		t.Fatalf("readyz has no components map: %v", body)
	}
	comp := func(name string) map[string]any {
		c, ok := components[name].(map[string]any)
		if !ok {
			t.Fatalf("readyz missing component %q: %v", name, body)
		}
		return c
	}
	if got := comp("postgres")["status"]; got != "disabled" {
		t.Fatalf("postgres (unconfigured) should be disabled, got %v", got)
	}
	if got := comp("redis")["status"]; got != "disabled" {
		t.Fatalf("redis (unconfigured) should be disabled, got %v", got)
	}
	if got := comp("engine")["status"]; got != "down" {
		t.Fatalf("engine (127.0.0.1:1, down) should be down, got %v", got)
	}
}

func TestReadyzReturns503WhenPostgresDown(t *testing.T) {
	srv, ts := testServer(t)
	srv.DBHealth = func(ctx context.Context) error {
		return errors.New("connection refused")
	}
	status, body := do(t, "GET", ts.URL+"/readyz", "", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("readyz with dead postgres: status %d, body %v", status, body)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("readyz status: %v", body)
	}
	components, _ := body["components"].(map[string]any)
	pg, _ := components["postgres"].(map[string]any)
	if pg["status"] != "down" {
		t.Fatalf("postgres should be down, got %v", pg)
	}
	// Redis/engine stay reported even when postgres gates.
	if components["redis"] == nil || components["engine"] == nil {
		t.Fatalf("readyz must still report redis and engine: %v", body)
	}
}

func TestReadyzReturns200WhenPostgresUp(t *testing.T) {
	srv, ts := testServer(t)
	srv.DBHealth = func(ctx context.Context) error { return nil }
	status, body := do(t, "GET", ts.URL+"/readyz", "", nil)
	if status != http.StatusOK {
		t.Fatalf("readyz with healthy postgres: status %d, body %v", status, body)
	}
	if body["status"] != "ready" {
		t.Fatalf("readyz status: %v", body)
	}
}

func TestMetricsEndpointExposesRequestCounts(t *testing.T) {
	_, ts := testServer(t)
	// Generate a counted request first.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/health", "", nil); status != http.StatusOK {
		t.Fatalf("health: %d", status)
	}
	status, raw := getBody(t, ts.URL+"/metrics")
	if status != http.StatusOK {
		t.Fatalf("/metrics: %d", status)
	}
	// Counted by chi route pattern, not raw path.
	if !strings.Contains(raw, `cloudforge_http_requests_total{method="GET",route="/api/v1/health",status="200"}`) {
		t.Fatalf("/metrics is missing the health request series:\n%s", firstLines(raw, 40))
	}
	if !strings.Contains(raw, "cloudforge_http_request_duration_seconds") {
		t.Fatalf("/metrics is missing the duration histogram:\n%s", firstLines(raw, 40))
	}
	// The engine gauge is set by /readyz.
	if status, _ := do(t, "GET", ts.URL+"/readyz", "", nil); status != http.StatusOK {
		t.Fatalf("readyz: %d", status)
	}
	_, raw = getBody(t, ts.URL+"/metrics")
	if !strings.Contains(raw, "cloudforge_network_engine_up 0") {
		t.Fatalf("/metrics should report engine down (0) after readyz:\n%s", firstLines(raw, 40))
	}
}

func TestMetricsEndpointDoesNotCountItself(t *testing.T) {
	_, ts := testServer(t)
	if _, err := http.Get(ts.URL + "/metrics"); err != nil {
		t.Fatal(err)
	}
	_, raw := getBody(t, ts.URL+"/metrics")
	if strings.Contains(raw, `route="/metrics"`) {
		t.Fatalf("/metrics must not record itself:\n%s", firstLines(raw, 40))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// --- rate limiting (M9 step 4) -----------------------------------------

// doRaw is do() with caller-set headers and access to response headers.
func doRaw(t *testing.T, method, url, token string, body any, headers map[string]string) (int, http.Header, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: could not decode JSON body: %v", method, url, err)
	}
	return resp.StatusCode, resp.Header, out
}

func testServerWithLimiters(t *testing.T, authLimiter, apiLimiter *ratelimit.Limiter) *httptest.Server {
	t.Helper()
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServerWithDeps(Deps{
		Users:       users,
		Labs:        store.NewMemoryLabStore(labStore),
		Registry:    providers.NewRegistry(),
		Network:     networkclient.New("127.0.0.1:1"),
		AuthLimiter: authLimiter,
		APILimiter:  apiLimiter,
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func testLimiter(t *testing.T, scope string, limit int, window time.Duration) *ratelimit.Limiter {
	t.Helper()
	return ratelimit.New(ratelimit.NewClient("localhost:6379"), scope, limit, window)
}

func uniqueScope(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("m9test-%s-%d", name, time.Now().UnixNano())
}

func TestAuthRateLimitTriggers(t *testing.T) {
	ts := testServerWithLimiters(t,
		testLimiter(t, uniqueScope(t, "auth"), 3, time.Minute), nil)
	for i := 0; i < 3; i++ {
		status, _, body := doRaw(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("user%d@example.com", i), "name": "User", "password": "correct-horse",
		}, nil)
		if status != http.StatusCreated {
			t.Fatalf("register %d: status %d, body %v", i, status, body)
		}
	}
	status, headers, body := doRaw(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "user3@example.com", "name": "User", "password": "correct-horse",
	}, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("4th register in window: status %d, body %v", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "rate_limited" {
		t.Fatalf("4th register error envelope: %v", body)
	}
	if headers.Get("Retry-After") == "" {
		t.Fatalf("429 must carry a Retry-After header")
	}
}

func TestAPIRateLimitTriggers(t *testing.T) {
	ts := testServerWithLimiters(t,
		nil, testLimiter(t, uniqueScope(t, "api"), 3, time.Minute))
	for i := 0; i < 3; i++ {
		if status, _ := do(t, "GET", ts.URL+"/api/v1/courses", "", nil); status != http.StatusOK {
			t.Fatalf("courses %d: %d", i, status)
		}
	}
	status, headers, body := doRaw(t, "GET", ts.URL+"/api/v1/courses", "", nil, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("4th request in window: status %d, body %v", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "rate_limited" {
		t.Fatalf("4th request error envelope: %v", body)
	}
	if headers.Get("Retry-After") == "" {
		t.Fatalf("429 must carry a Retry-After header")
	}
	// Health probes outside /api/v1 are never rate limited.
	if status, _ := do(t, "GET", ts.URL+"/healthz", "", nil); status != http.StatusOK {
		t.Fatalf("healthz must stay unthrottled: %d", status)
	}
}

func TestAuthRateLimitIsPerIP(t *testing.T) {
	ts := testServerWithLimiters(t,
		testLimiter(t, uniqueScope(t, "auth-ip"), 2, time.Minute), nil)
	registerAs := func(ip string, n int) int {
		status, _, _ := doRaw(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("ip-%s-%d@example.com", strings.ReplaceAll(ip, ".", "-"), n),
			"name":  "User", "password": "correct-horse",
		}, map[string]string{"X-Forwarded-For": ip})
		return status
	}
	if s := registerAs("203.0.113.1", 0); s != http.StatusCreated {
		t.Fatalf("first register from 203.0.113.1: %d", s)
	}
	if s := registerAs("203.0.113.1", 1); s != http.StatusCreated {
		t.Fatalf("second register from 203.0.113.1: %d", s)
	}
	if s := registerAs("203.0.113.1", 2); s != http.StatusTooManyRequests {
		t.Fatalf("third register from 203.0.113.1 should be 429, got %d", s)
	}
	// A different client IP has its own budget.
	if s := registerAs("203.0.113.2", 0); s != http.StatusCreated {
		t.Fatalf("first register from 203.0.113.2 should be allowed, got %d", s)
	}
}

func TestAuthRateLimitFailsClosedWhenRedisDown(t *testing.T) {
	dead := ratelimit.New(ratelimit.NewClient("127.0.0.1:6399"), uniqueScope(t, "auth-dead"), 3, time.Minute)
	ts := testServerWithLimiters(t, dead, nil)
	status, _, body := doRaw(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "down@example.com", "name": "User", "password": "correct-horse",
	}, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("register with Redis down: status %d, body %v (want 503 fail-closed)", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "rate_limiter_unavailable" {
		t.Fatalf("register with Redis down error envelope: %v", body)
	}
}

func TestAPIRateLimitFailsOpenWhenRedisDown(t *testing.T) {
	dead := ratelimit.New(ratelimit.NewClient("127.0.0.1:6399"), uniqueScope(t, "api-dead"), 3, time.Minute)
	ts := testServerWithLimiters(t, nil, dead)
	status, _, body := doRaw(t, "GET", ts.URL+"/api/v1/courses", "", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("courses with Redis down: status %d, body %v (want 200 fail-open)", status, body)
	}
}
