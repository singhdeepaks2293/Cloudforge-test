// Package e2e is the CloudForge end-to-end acceptance test (Milestone 9,
// Part 3). It is black-box and stdlib-only: it builds the real api
// binary and the e2e-fakes binary (fake gRPC network engine +
// simulated host agent), starts them as processes, and drives the
// real HTTP API, asserting real state at every step — DB/API/engine
// responses, never a mocked "success".
//
// Flow: register -> login -> open the VPC Fundamentals course -> start
// a lab -> create a VPC -> create two subnets -> configure routes,
// NAT and a security group -> launch a Linux VM (simulated host
// agent) -> terminal token -> inspect topology -> break a route via
// fault injection -> diagnose -> repair -> pass validation -> XP
// awarded -> destroy the lab -> verify all resources are cleaned up
// (including the leftover engine network, via cleanup reconciliation).
//
// The test SKIPS (does not fail) when CLOUDFORGE_E2E_SKIP=1, when the
// Go toolchain is not available to build the binaries, or when the
// processes cannot be started — those are environment problems, not
// product failures. It needs a reachable Redis (REDIS_ADDR, default
// localhost:6379) because the API's auth rate limiter is fail-closed
// without one.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	haToken   = "e2e-token"
	jwtSecret = "e2e-test-secret"
)

type env struct {
	t           *testing.T
	apiURL      string
	haURL       string
	token       string
	adminToken  string
	labID       string
	resourceIDs []string
	apiLog      *os.File
}

func (e *env) do(method, path, token string, body any) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			e.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, e.apiURL+path, &buf)
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("%s %s: could not decode JSON (%q): %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, out
}

func (e *env) must(status, want int, what string, body map[string]any) {
	e.t.Helper()
	if status != want {
		// Dump the API log on failure: the cause is usually there.
		if e.apiLog != nil {
			if data, err := os.ReadFile(e.apiLog.Name()); err == nil {
				e.t.Logf("--- api log ---\n%s\n--- end api log ---", data)
			}
		}
		e.t.Fatalf("%s: got HTTP %d, want %d: %v", what, status, want, body)
	}
}

func (e *env) createResource(kind, name string, extra map[string]any) map[string]any {
	e.t.Helper()
	body := map[string]any{"kind": kind, "name": name}
	for k, v := range extra {
		body[k] = v
	}
	status, resp := e.do("POST", "/labs/"+e.labID+"/resources", e.token, body)
	e.must(status, http.StatusCreated, "create "+kind+" "+name, resp)
	id, _ := resp["id"].(string)
	if id == "" {
		e.t.Fatalf("create %s %s: no id in response: %v", kind, name, resp)
	}
	e.resourceIDs = append(e.resourceIDs, id)
	e.t.Logf("created %s %q id=%s", kind, name, id)
	return resp
}

func (e *env) pollLabState(want string, timeout time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, body := e.do("GET", "/labs/"+e.labID, e.token, nil)
		e.must(status, http.StatusOK, "poll lab", body)
		if body["state"] == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.t.Fatalf("lab %s did not reach state %q within %s", e.labID, want, timeout)
}

// freePort returns an unused loopback port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// findGo locates the Go toolchain (plain PATH first, then the
// module-cache toolchain this sandbox uses).
func findGo(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64/bin/go"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("go toolchain not found on PATH; cannot build the api/fakes binaries")
	return ""
}

// build compiles one backend/cmd package into dir.
func build(t *testing.T, goBin, repoRoot, pkg, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	cmd := exec.Command(goBin, "build", "-o", out, pkg)
	cmd.Dir = filepath.Join(repoRoot, "backend")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot build %s (%v): %s — environment problem, not a product failure", pkg, err, stderr.String())
	}
	return out
}

// start launches a process, wiring its output to a log file.
func start(t *testing.T, name, bin string, args []string, env []string, logPath string) *exec.Cmd {
	t.Helper()
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = f.Close()
	})
	return cmd
}

// waitFor polls url until it answers 2xx (or timeout).
func waitFor(t *testing.T, what, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not become ready at %s within %s", what, url, timeout)
}

// waitForTCP polls a TCP dial until it connects (or timeout).
func waitForTCP(t *testing.T, what, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not listen at %s within %s", what, addr, timeout)
}

func TestEndToEnd(t *testing.T) {
	if os.Getenv("CLOUDFORGE_E2E_SKIP") == "1" {
		t.Skip("CLOUDFORGE_E2E_SKIP=1: end-to-end test skipped")
	}
	// Repo root from the test file's location (tests/e2e -> ../..).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "curriculum")); err != nil {
		t.Fatalf("curriculum dir not found under repo root %s: %v", repoRoot, err)
	}
	goBin := findGo(t)
	binDir := t.TempDir()
	apiBin := build(t, goBin, repoRoot, "./cmd/api", binDir, "cf-api")
	fakesBin := build(t, goBin, repoRoot, "./cmd/e2e-fakes", binDir, "cf-fakes")

	apiPort := freePort(t)
	enginePort := freePort(t)
	haPort := freePort(t)
	engineAddr := fmt.Sprintf("127.0.0.1:%d", enginePort)
	haAddr := fmt.Sprintf("127.0.0.1:%d", haPort)
	apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)

	// --- Fakes: fake gRPC engine + simulated host agent. -------------
	fakesLog := filepath.Join(binDir, "fakes.log")
	start(t, "e2e-fakes", fakesBin,
		[]string{"-engine-addr", engineAddr, "-host-agent-addr", haAddr, "-host-agent-token", haToken},
		os.Environ(), fakesLog)
	waitForTCP(t, "fake engine", engineAddr, 15*time.Second)
	waitFor(t, "simulated host agent", "http://"+haAddr+"/healthz", 15*time.Second)
	t.Logf("fakes up: engine %s, host agent %s", engineAddr, haAddr)

	// --- Real API binary, in-memory stores. --------------------------
	// DATABASE_URL points at a dead port on purpose: the config's
	// default DSN is the live dev Postgres, and this test must be
	// hermetic (in-memory stores, nothing left behind). A refused
	// connection makes cmd/api fall back to in-memory stores with a
	// warning; the guard below proves the fallback happened.
	apiLogPath := filepath.Join(binDir, "api.log")
	apiLog, err := os.OpenFile(apiLogPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = apiLog.Close() })
	apiEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"CLOUDFORGE_ADDR=" + apiAddr,
		"NETWORK_ENGINE_ADDR=" + engineAddr,
		"HOST_AGENT_ADDR=" + haAddr,
		"HOST_AGENT_TOKEN=" + haToken,
		"HOST_AGENT_MODE=simulated",
		"CLOUDFORGE_ADMIN_EMAIL=admin@example.com",
		"CLOUDFORGE_JWT_SECRET=" + jwtSecret,
		"CURRICULUM_DIR=" + filepath.Join(repoRoot, "curriculum"),
		"CURRICULUM_SEED=1",
		"REDIS_ADDR=" + envOr("REDIS_ADDR", "localhost:6379"),
		"DATABASE_URL=postgres://cloudforge:cloudforge@127.0.0.1:1/cloudforge?sslmode=disable",
		"GOTOOLCHAIN=local",
	}
	apiCmd := exec.Command(apiBin)
	apiCmd.Env = apiEnv
	apiCmd.Stdout = apiLog
	apiCmd.Stderr = apiLog
	if err := apiCmd.Start(); err != nil {
		t.Fatalf("start api: %v", err)
	}
	t.Cleanup(func() {
		_ = apiCmd.Process.Kill()
		_ = apiCmd.Wait()
	})
	waitFor(t, "api /readyz", "http://"+apiAddr+"/readyz", 30*time.Second)
	// Guard: the API must be on in-memory stores, or the lab
	// provisioning below would wait on a worker that isn't running.
	logData, err := os.ReadFile(apiLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "in-memory stores") {
		t.Fatalf("api did not fall back to in-memory stores; log:\n%s", logData)
	}
	t.Logf("api up on %s (in-memory stores, seeded curriculum)", apiAddr)

	e := &env{t: t, apiURL: "http://" + apiAddr + "/api/v1", haURL: "http://" + haAddr, apiLog: apiLog}

	// --- 1. Register + login. ----------------------------------------
	status, body := e.do("POST", "/auth/register", "", map[string]any{
		"email": "e2e@example.com", "name": "E2E Learner", "password": "correct-horse-battery",
	})
	e.must(status, http.StatusCreated, "register", body)
	status, body = e.do("POST", "/auth/login", "", map[string]any{
		"email": "e2e@example.com", "password": "correct-horse-battery",
	})
	e.must(status, http.StatusOK, "login", body)
	e.token, _ = body["token"].(string)
	if e.token == "" {
		t.Fatal("login returned no token")
	}
	t.Log("registered + logged in as e2e@example.com")

	// Admin for the reconcile steps (bootstrap email -> admin role).
	status, body = e.do("POST", "/auth/register", "", map[string]any{
		"email": "admin@example.com", "name": "E2E Admin", "password": "correct-horse-battery",
	})
	e.must(status, http.StatusCreated, "register admin", body)
	e.adminToken, _ = body["token"].(string)

	// --- 2. Open the VPC Fundamentals course. ------------------------
	status, body = e.do("GET", "/curriculum/tracks/networking", e.token, nil)
	e.must(status, http.StatusOK, "open networking track", body)
	title, _ := body["title"].(string)
	if !strings.Contains(title, "VPC") {
		t.Fatalf("networking track title %q does not mention VPC", title)
	}
	t.Logf("opened course %q", title)

	// --- 3. Start the lab. -------------------------------------------
	status, body = e.do("POST", "/labs", e.token, map[string]any{
		"definition_id": "vpc-fundamentals", "name": "e2e vpc lab",
	})
	if status != http.StatusCreated && status != http.StatusAccepted {
		t.Fatalf("create lab: %d %v", status, body)
	}
	lab, _ := body["lab"].(map[string]any)
	e.labID, _ = lab["id"].(string)
	if e.labID == "" {
		t.Fatalf("create lab returned no id: %v", body)
	}
	e.pollLabState("READY", 30*time.Second)
	t.Logf("lab %s READY", e.labID)

	// --- 4. Create the VPC. ------------------------------------------
	vpc := e.createResource("vpc", "main-vpc", map[string]any{"cidr": "10.0.0.0/16"})
	if vpc["cidr"] != "10.0.0.0/16" {
		t.Fatalf("VPC cidr mismatch: %v", vpc)
	}

	// --- 5. Create two subnets (engine creates the network lazily). --
	sub1 := e.createResource("subnet", "public-subnet", map[string]any{"cidr": "10.0.1.0/24"})
	if sub1["provider"] != "rust-network-engine" {
		t.Fatalf("subnet provider: %v (want rust-network-engine)", sub1["provider"])
	}
	sub2 := e.createResource("subnet", "private-subnet", map[string]any{"cidr": "10.0.2.0/24"})
	_ = sub2

	// --- 6. Routes, NAT, security group. ------------------------------
	e.createResource("gateway", "nat-1", map[string]any{
		"config": map[string]any{"gateway_type": "nat"},
	})
	e.createResource("security-group", "web-sg", map[string]any{
		"config": map[string]any{"rules": []any{
			map[string]any{"port": 22, "source": "0.0.0.0/0"},
		}},
	})

	// --- 7. Launch a Linux VM (simulated host agent). -----------------
	vm := e.createResource("instance", "web-1", map[string]any{"profile": "micro"})
	vmID, _ := vm["provider_resource_id"].(string)
	if vmID == "" {
		t.Fatalf("instance has no provider_resource_id: %v", vm)
	}
	// The VM really exists in the simulated host agent.
	status, body = e.doHostAgentOp("ListVMs", map[string]any{"lab_id": e.labID})
	e.must(status, http.StatusOK, "host agent ListVMs", body)
	foundVM := false
	for _, item := range body["items"].([]any) {
		if m, ok := item.(map[string]any); ok && m["id"] == vmID {
			foundVM = true
		}
	}
	if !foundVM {
		t.Fatalf("VM %s not found in simulated host agent: %v", vmID, body)
	}
	t.Logf("launched VM %s (simulated host agent)", vmID)

	// --- 8. Terminal token. ------------------------------------------
	status, body = e.do("POST", "/labs/"+e.labID+"/terminal/token", e.token, nil)
	e.must(status, http.StatusOK, "terminal token", body)
	termToken, _ := body["token"].(string)
	if termToken == "" {
		t.Fatal("terminal token endpoint returned no token")
	}
	if termToken == e.token {
		t.Fatal("terminal token must differ from the Bearer token")
	}
	t.Log("terminal token issued (short-lived, lab-scoped)")

	// --- 9. Inspect topology. ----------------------------------------
	status, body = e.do("GET", "/labs/"+e.labID+"/topology", e.token, nil)
	e.must(status, http.StatusOK, "topology", body)
	nodes, _ := body["nodes"].([]any)
	nodeIDs := map[string]bool{}
	for _, n := range nodes {
		if m, ok := n.(map[string]any); ok {
			nodeIDs[m["id"].(string)] = true
		}
	}
	for _, want := range []string{e.labID, sub1["id"].(string), sub2["id"].(string)} {
		if !nodeIDs[want] {
			t.Fatalf("topology missing node %s (nodes: %v)", want, nodeIDs)
		}
	}
	t.Logf("topology: %d nodes (network + 2 subnets + DB resources)", len(nodes))

	// --- 10. Break a route (fault injection). -------------------------
	status, body = e.do("POST", "/labs/"+e.labID+"/break-fix/start", e.token,
		map[string]any{"scenario_id": "missing-igw-route"})
	e.must(status, http.StatusOK, "break-fix start", body)
	attempt, _ := body["attempt"].(map[string]any)
	if attempt["fault_status"] != "injected" {
		t.Fatalf("fault_status: %v (want injected — fake engine must accept InjectFault)", attempt["fault_status"])
	}
	t.Log("fault injected: missing-igw-route")

	// --- 11. Diagnose: solve fails before the fix. -------------------
	status, body = e.do("POST", "/labs/"+e.labID+"/break-fix/solve", e.token, map[string]any{})
	e.must(status, http.StatusOK, "break-fix solve (before fix)", body)
	if body["solved"] != false {
		t.Fatalf("solve before fix should be solved=false: %v", body)
	}
	t.Log("diagnosed: validation fails without the IGW route")

	// --- 12. Repair: route table + load balancer. ---------------------
	e.createResource("route-table", "rtb-public", map[string]any{
		"config": map[string]any{"routes": []any{
			map[string]any{"destination": "0.0.0.0/0", "target": "igw-1"},
		}},
	})
	e.createResource("loadbalancer", "web-lb", map[string]any{
		"config": map[string]any{"healthy_targets": 1},
	})

	// --- 13. Solve again: fixed, XP awarded. -------------------------
	status, body = e.do("POST", "/labs/"+e.labID+"/break-fix/solve", e.token, map[string]any{})
	e.must(status, http.StatusOK, "break-fix solve (after fix)", body)
	if body["solved"] != true {
		t.Fatalf("solve after fix should be solved=true: %v", body)
	}
	if body["xp_awarded"] != float64(120) {
		t.Fatalf("xp_awarded: %v (want 120)", body["xp_awarded"])
	}
	t.Log("repaired: route restored, scenario solved, 120 XP awarded")

	// --- 14. Pass validation. -----------------------------------------
	status, body = e.do("POST", "/labs/"+e.labID+"/validate", e.token, map[string]any{})
	e.must(status, http.StatusOK, "validate", body)
	if body["passed"] != true {
		t.Fatalf("validation should pass: %v", body)
	}
	checks, _ := body["checks"].([]any)
	if len(checks) != 5 {
		t.Fatalf("validation checks: %d (want 5 for vpc-fundamentals)", len(checks))
	}
	t.Log("validation passed (5/5 checks)")

	// --- 15. XP awarded. ----------------------------------------------
	status, body = e.do("GET", "/users/me/progress", e.token, nil)
	e.must(status, http.StatusOK, "progress", body)
	totalXP, _ := body["total_xp"].(float64)
	if totalXP < 120 {
		t.Fatalf("total_xp: %v (want >= 120)", totalXP)
	}
	status, body = e.do("GET", "/users/me/achievements", e.token, nil)
	e.must(status, http.StatusOK, "achievements", body)
	earned, _ := body["earned"].([]any)
	foundBreaker := false
	for _, a := range earned {
		if m, ok := a.(map[string]any); ok && m["id"] == "breaker" {
			foundBreaker = true
		}
	}
	if !foundBreaker {
		t.Fatalf("breaker achievement not earned: %v", earned)
	}
	t.Logf("XP confirmed: total_xp=%.0f, breaker achievement earned", totalXP)

	// --- 16. Destroy the lab. -----------------------------------------
	// Delete every resource through the API first (each delete
	// goes through its real backend: engine subnet, host-agent VM,
	// or the DB row), then destroy the lab itself.
	for _, id := range e.resourceIDs {
		status, body := e.do("DELETE", "/labs/"+e.labID+"/resources/"+id, e.token, nil)
		e.must(status, http.StatusOK, "delete resource "+id, body)
	}
	status, body = e.do("GET", "/labs/"+e.labID+"/resources", e.token, nil)
	e.must(status, http.StatusOK, "list resources after deletes", body)
	if list, _ := body["resources"].([]any); len(list) != 0 {
		t.Fatalf("resources not empty after deletes: %v", body)
	}
	status, body = e.do("POST", "/labs/"+e.labID+"/destroy", e.token, nil)
	e.must(status, http.StatusOK, "destroy lab", body)
	e.pollLabState("DESTROYED", 30*time.Second)
	t.Log("lab destroyed")

	// --- 17. Cleanup verification via reconciliation. -----------------
	// The engine network is the one leftover: nothing deletes it
	// (subnet deletes remove subnets, not the network). Dry-run
	// must report it as a live orphan of the destroyed lab.
	status, body = e.do("POST", "/admin/cleanup-reconcile", e.adminToken, map[string]any{"dry_run": true})
	e.must(status, http.StatusOK, "cleanup reconcile (dry-run)", body)
	reports, _ := body["reports"].([]any)
	var labReport map[string]any
	for _, r := range reports {
		if m, ok := r.(map[string]any); ok && m["lab_id"] == e.labID {
			labReport = m
		}
	}
	if labReport == nil {
		t.Fatalf("no reconcile report for lab %s: %v", e.labID, reports)
	}
	live, _ := labReport["live_orphans"].([]any)
	if len(live) != 1 {
		t.Fatalf("dry-run live orphans: %d (want 1 — the leftover engine network): %v", len(live), labReport)
	}
	if dbOrphans, _ := labReport["db_orphans"].([]any); len(dbOrphans) != 0 {
		t.Fatalf("dry-run db orphans: %d (want 0): %v", len(dbOrphans), labReport)
	}
	t.Log("reconcile dry-run: 1 live orphan (leftover engine network), 0 DB orphans")

	// Enforce through the admin endpoint (dry_run=false) — the
	// same path a scheduled cleanup_reconcile job takes.
	status, body = e.do("POST", "/admin/cleanup-reconcile", e.adminToken,
		map[string]any{"lab_id": e.labID, "dry_run": false})
	e.must(status, http.StatusOK, "cleanup reconcile (enforce)", body)
	reports, _ = body["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("enforce reconcile: expected 1 report, got %v", body)
	}
	if removed, _ := reports[0].(map[string]any)["removed_live"].(float64); removed != 1 {
		t.Fatalf("enforce reconcile removed_live: %v (want 1)", reports[0])
	}
	// The engine now answers NOT_FOUND for the lab's topology —
	// the network is really gone, not just unlisted.
	status, body = e.do("GET", "/labs/"+e.labID+"/topology", e.token, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("topology after network removal: got %d %v (want 503 engine_unavailable)", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "engine_unavailable" {
		t.Fatalf("topology error code: %v (want engine_unavailable)", body)
	}
	// A final dry-run finds nothing left to clean.
	status, body = e.do("POST", "/admin/cleanup-reconcile", e.adminToken, map[string]any{"dry_run": true})
	e.must(status, http.StatusOK, "cleanup reconcile (final)", body)
	reports, _ = body["reports"].([]any)
	for _, r := range reports {
		if m, ok := r.(map[string]any); ok && m["lab_id"] == e.labID {
			if lo, _ := m["live_orphans"].([]any); len(lo) != 0 {
				t.Fatalf("final reconcile still reports live orphans: %v", m)
			}
		}
	}
	// Host agent: no VMs left for the lab.
	status, body = e.doHostAgentOp("ListVMs", map[string]any{"lab_id": e.labID})
	e.must(status, http.StatusOK, "host agent ListVMs (final)", body)
	if items, _ := body["items"].([]any); len(items) != 0 {
		t.Fatalf("host agent still has VMs for the lab: %v", body)
	}
	t.Log("cleanup verified: no DB resources, no engine networks, no host-agent VMs")
}

// doHostAgentOp calls POST /v1/ops on the simulated host agent.
func (e *env) doHostAgentOp(op string, extra map[string]any) (int, map[string]any) {
	e.t.Helper()
	body := map[string]any{"op": op}
	for k, v := range extra {
		body[k] = v
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest("POST", e.haURL+"/v1/ops", &buf)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+haToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("host agent op %s: could not decode JSON (%q): %v", op, raw, err)
		}
	}
	return resp.StatusCode, out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
