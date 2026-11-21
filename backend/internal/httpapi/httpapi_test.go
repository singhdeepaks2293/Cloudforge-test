package httpapi

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServer(users, labStore, providers.NewRegistry(), networkclient.New("127.0.0.1:1"), nil)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts
}

func do(t *testing.T, method, url, token string, body any) (int, map[string]any) {
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: could not decode JSON body: %v", method, url, err)
	}
	return resp.StatusCode, out
}

func register(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "ada@example.com", "name": "Ada", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register: status %d, body %v", status, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("register returned no token: %v", body)
	}
	return token
}

func TestHealthzAndHealthReportEngineDownHonestly(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/healthz", "", nil)
	if status != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz: %d %v", status, body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/health", "", nil)
	if status != http.StatusOK {
		t.Fatalf("health: %d %v", status, body)
	}
	// The test server's network client points at a closed port, so the
	// engine must be reported unreachable — never faked as ok.
	if body["network_engine_reachable"] != false {
		t.Fatalf("health must report network_engine_reachable=false when the engine is down: %v", body)
	}
	if body["service"] != "cloudforge-api" || body["version"] != Version {
		t.Fatalf("health missing service/version: %v", body)
	}
}

func TestHealthReportsEngineReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	users := auth.NewStore("test-secret")
	srv := NewServer(users, labs.NewStore(), providers.NewRegistry(), networkclient.New(ln.Addr().String()), nil)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	_, body := do(t, "GET", ts.URL+"/api/v1/health", "", nil)
	if body["network_engine_reachable"] != true {
		t.Fatalf("health must report reachable=true for a live listener: %v", body)
	}
}

func TestAuthFlow(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)

	// Duplicate registration -> 409.
	if status, body := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "ada@example.com", "name": "Ada", "password": "correct-horse",
	}); status != http.StatusConflict {
		t.Fatalf("duplicate register: got %d, want 409: %v", status, body)
	}

	// Wrong password -> 401 with the error envelope.
	if status, body := do(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]any{
		"email": "ada@example.com", "password": "wrong-password",
	}); status != http.StatusUnauthorized {
		t.Fatalf("bad login: got %d, want 401: %v", status, body)
	} else if body["error"] == nil {
		t.Fatalf("error responses must use the {error:{code,message,request_id}} envelope: %v", body)
	}

	// Login works and /me returns the user.
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]any{
		"email": "ada@example.com", "password": "correct-horse",
	})
	if status != http.StatusOK || body["token"] == "" {
		t.Fatalf("login: %d %v", status, body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/auth/me", token, nil)
	if status != http.StatusOK || body["email"] != "ada@example.com" {
		t.Fatalf("me: %d %v", status, body)
	}
	// No token -> 401; bad token -> 401.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("me without token: got %d, want 401", status)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/auth/me", "bogus", nil); status != http.StatusUnauthorized {
		t.Fatalf("me with bad token: got %d, want 401", status)
	}
}

func TestCourses(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/courses", "", nil)
	if status != http.StatusOK {
		t.Fatalf("courses: %d %v", status, body)
	}
	list, _ := body["courses"].([]any)
	if len(list) != 3 {
		t.Fatalf("expected 3 seeded courses, got %d: %v", len(list), body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/courses/vpc-fundamentals", "", nil)
	if status != http.StatusOK || body["title"] != "VPC Fundamentals" {
		t.Fatalf("course detail: %d %v", status, body)
	}
	if _, ok := body["lessons"]; !ok {
		t.Fatalf("course detail must include lessons: %v", body)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/courses/nope", "", nil); status != http.StatusNotFound {
		t.Fatalf("unknown course: got %d, want 404", status)
	}
}

func TestCapabilitiesNeverClaimReal(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/providers/capabilities", "", nil)
	if status != http.StatusOK {
		t.Fatalf("capabilities: %d %v", status, body)
	}
	list, _ := body["capabilities"].([]any)
	if len(list) < 4 {
		t.Fatalf("expected at least 4 capabilities, got %d", len(list))
	}
	for _, item := range list {
		c, _ := item.(map[string]any)
		if c["fidelity"] == "REAL" {
			t.Fatalf("capability %v claims REAL fidelity", c)
		}
	}
}

func waitLabState(t *testing.T, ts *httptest.Server, token, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, body := do(t, "GET", ts.URL+"/api/v1/labs/"+id, token, nil)
		if body["state"] == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("lab %s did not reach %s", id, want)
}

func TestLabLifecycleAndQuota(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)

	// Unauthenticated lab creation -> 401.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs", "", map[string]any{"definition_id": "ec2-basics"}); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated lab create: got %d, want 401", status)
	}
	// Unknown definition -> 400.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{"definition_id": "nope"}); status != http.StatusBadRequest {
		t.Fatalf("unknown definition: got %d, want 400", status)
	}

	// Create with an idempotency key; replay returns the same lab.
	status, body := do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{
		"definition_id": "ec2-basics", "idempotency_key": "idem-1",
	})
	if status != http.StatusCreated {
		t.Fatalf("lab create: %d %v", status, body)
	}
	lab, _ := body["lab"].(map[string]any)
	labID, _ := lab["id"].(string)
	if labID == "" {
		t.Fatalf("lab create returned no id: %v", body)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{
		"definition_id": "ec2-basics", "idempotency_key": "idem-1",
	})
	if status != http.StatusOK || body["replayed"] != true {
		t.Fatalf("idempotent replay: got %d %v, want 200 replayed=true", status, body)
	}

	// Provisioning completes asynchronously to READY.
	waitLabState(t, ts, token, labID, "READY")

	// Fill the quota (3 active labs) and verify the 4th is refused with 429.
	for i := 0; i < 2; i++ {
		if status, _ := do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{"definition_id": "vpc-fundamentals"}); status != http.StatusCreated {
			t.Fatalf("lab create %d: got %d", i, status)
		}
	}
	if status, body := do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{"definition_id": "ec2-basics"}); status != http.StatusTooManyRequests {
		t.Fatalf("4th lab: got %d, want 429: %v", status, body)
	} else if body["error"] == nil {
		t.Fatalf("quota error must use the error envelope: %v", body)
	}

	// Stop then destroy the first lab.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/stop", token, nil)
	if status != http.StatusOK {
		t.Fatalf("lab stop: %d %v", status, body)
	}
	waitLabState(t, ts, token, labID, "STOPPED")
	status, _ = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/destroy", token, nil)
	if status != http.StatusOK {
		t.Fatalf("lab destroy: %d", status)
	}
	waitLabState(t, ts, token, labID, "DESTROYED")

	// A destroyed lab can no longer be stopped -> 409.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/stop", token, nil); status != http.StatusConflict {
		t.Fatalf("stop destroyed lab: got %d, want 409", status)
	}
	// Unknown lab -> 404.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/labs/nope", token, nil); status != http.StatusNotFound {
		t.Fatalf("unknown lab: got %d, want 404", status)
	}
}

func TestLabOwnershipIsolation(t *testing.T) {
	_, ts := testServer(t)
	tokenA := register(t, ts)
	// Second user, registered directly against a fresh email.
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": "bob@example.com", "name": "Bob", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register bob: %d %v", status, body)
	}
	tokenB, _ := body["token"].(string)

	_, body = do(t, "POST", ts.URL+"/api/v1/labs", tokenA, map[string]any{"definition_id": "ec2-basics"})
	lab, _ := body["lab"].(map[string]any)
	labID, _ := lab["id"].(string)
	// Bob must not see or stop Ada's lab (404, not 403, to avoid leaking it).
	if status, _ := do(t, "GET", ts.URL+"/api/v1/labs/"+labID, tokenB, nil); status != http.StatusNotFound {
		t.Fatalf("other user's lab GET: got %d, want 404", status)
	}
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/stop", tokenB, nil); status != http.StatusNotFound {
		t.Fatalf("other user's lab stop: got %d, want 404", status)
	}
	// And Bob's own list is empty.
	if status, body := do(t, "GET", ts.URL+"/api/v1/labs", tokenB, nil); status != http.StatusOK {
		t.Fatalf("list labs: %d", status)
	} else if list, _ := body["labs"].([]any); len(list) != 0 {
		t.Fatalf("bob's lab list should be empty, got %v", list)
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	_, ts := testServer(t)
	req, err := http.NewRequest("GET", ts.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", "test-correlation-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Request-ID"); got != "test-correlation-123" {
		t.Fatalf("X-Request-ID should be echoed, got %q", got)
	}
	// Without a supplied ID the server generates one.
	resp2, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("X-Request-ID") == "" {
		t.Fatal("server must generate X-Request-ID when absent")
	}
}
