// Milestone 6 + 7 HTTP tests: resources, topology, validation,
// terminal token + WebSocket, and the Terraform plan/approve/apply
// flow (with the TestStubRunner — labelled test-only — and the real
// worker service driving the jobs).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/jobs"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
)

func createLab(t *testing.T, tsURL, token, definition string) (string, map[string]any) {
	t.Helper()
	status, body := do(t, "POST", tsURL+"/api/v1/labs", token, map[string]any{
		"definition_id": definition, "name": "m6 lab",
	})
	if status != http.StatusCreated && status != http.StatusAccepted {
		t.Fatalf("create lab: status %d, body %v", status, body)
	}
	lab, _ := body["lab"].(map[string]any)
	id, _ := lab["id"].(string)
	if id == "" {
		t.Fatalf("create lab returned no id: %v", body)
	}
	return id, lab
}

func userIDOf(t *testing.T, tsURL, token string) string {
	t.Helper()
	status, body := do(t, "GET", tsURL+"/api/v1/auth/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("me: %d %v", status, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("me returned no id: %v", body)
	}
	return id
}

func seedResource(t *testing.T, srv *Server, labID, userID, kind, name string, cfg map[string]any) string {
	t.Helper()
	r, err := srv.Resources.CreateResource(context.Background(), &store.LabResource{
		LabID: labID, OwnerID: userID, Kind: kind, Name: name,
		State: "available", Provider: "rust-network-engine", Config: cfg,
	})
	if err != nil {
		t.Fatalf("seed resource %s: %v", kind, err)
	}
	return r.ID
}

// --- resources ------------------------------------------------------------

func TestM6ResourcesCRUD(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")

	// List: empty store, honest note, no invented resources.
	status, body := do(t, "GET", ts.URL+"/api/v1/labs/"+labID+"/resources", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list resources: %d %v", status, body)
	}
	if res, _ := body["resources"].([]any); len(res) != 0 {
		t.Fatalf("fresh lab must list no resources, got %v", res)
	}
	if body["note"] == "" {
		t.Fatal("empty resource list must carry a note")
	}

	// Create a VPC (DB-only kind): 201.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token, map[string]any{
		"kind": "vpc", "name": "main", "cidr": "10.0.0.0/16",
	})
	if status != http.StatusCreated {
		t.Fatalf("create vpc: %d %v", status, body)
	}
	vpcID, _ := body["id"].(string)
	if vpcID == "" || body["kind"] != "vpc" {
		t.Fatalf("created vpc shape: %v", body)
	}

	// Invalid kind: 400.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token, map[string]any{
		"kind": "bucket", "name": "x",
	}); status != http.StatusBadRequest {
		t.Fatalf("invalid kind: got %d, want 400", status)
	}

	// Subnet creation calls the (down) engine: 502, never fake success.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token, map[string]any{
		"kind": "subnet", "name": "public", "cidr": "10.0.1.0/24", "parent_id": vpcID,
	})
	if status != http.StatusBadGateway {
		t.Fatalf("subnet with engine down: got %d, want 502: %v", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "engine_error" {
		t.Fatalf("subnet failure must carry the structured engine error: %v", body)
	}

	// Instance creation with no host agent configured: 503.
	status, _ = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token, map[string]any{
		"kind": "instance", "name": "web",
	})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("instance without host agent: got %d, want 503", status)
	}

	// List now shows the VPC only.
	status, body = do(t, "GET", ts.URL+"/api/v1/labs/"+labID+"/resources", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list: %d %v", status, body)
	}
	res, _ := body["resources"].([]any)
	if len(res) != 1 {
		t.Fatalf("list after create: %v", res)
	}

	// Delete is idempotent: twice, both 200.
	for i := 0; i < 2; i++ {
		status, _ = do(t, "DELETE", ts.URL+"/api/v1/labs/"+labID+"/resources/"+vpcID, token, nil)
		if status != http.StatusOK {
			t.Fatalf("delete vpc (attempt %d): got %d", i, status)
		}
	}
}

// --- topology -------------------------------------------------------------

func TestM6TopologyEngineDownIs503(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")
	status, body := do(t, "GET", ts.URL+"/api/v1/labs/"+labID+"/topology", token, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("topology with engine down: got %d, want 503: %v", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "engine_unavailable" {
		t.Fatalf("topology 503 must name the engine failure: %v", body)
	}
	// 503 is NOT a pending-endpoint error (404/501): the frontend
	// shows it as a real failure, which is the correct behaviour.
	if body["capability"] == nil {
		t.Fatal("topology 503 must carry capability info")
	}
}

// --- validation -----------------------------------------------------------

func TestM6ValidateLab(t *testing.T) {
	srv, ts := testServer(t)
	token := register(t, ts)
	userID := userIDOf(t, ts.URL, token)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")

	// Fresh lab: defaults fail honestly (engine is down, so the
	// missing-state rules are fail-or-unknown, never pass).
	status, body := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/validate", token, nil)
	if status != http.StatusOK {
		t.Fatalf("validate fresh lab: %d %v", status, body)
	}
	if body["passed"] != false {
		t.Fatalf("fresh lab must not pass: %v", body)
	}
	if body["xp_eligible"] != false {
		t.Fatalf("fresh lab must not be XP-eligible: %v", body)
	}
	checks, _ := body["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("validate must return per-rule checks: %v", body)
	}

	// Seed the real state the defaults check, then it must pass.
	seedResource(t, srv, labID, userID, "vpc", "main", map[string]any{"cidr": "10.0.0.0/16"})
	seedResource(t, srv, labID, userID, "subnet", "public-a", map[string]any{"cidr": "10.0.1.0/24"})
	seedResource(t, srv, labID, userID, "subnet", "private-a", map[string]any{"cidr": "10.0.2.0/24"})
	seedResource(t, srv, labID, userID, "route-table", "public-rt", map[string]any{"routes": []any{
		map[string]any{"destination": "0.0.0.0/0", "target": "igw-123"},
	}})
	seedResource(t, srv, labID, userID, "security-group", "web-sg", map[string]any{"rules": []any{
		map[string]any{"port": float64(22), "source": "0.0.0.0/0"},
	}})
	seedResource(t, srv, labID, userID, "gateway", "nat-1", map[string]any{"gateway_type": "nat"})
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/validate", token, nil)
	if status != http.StatusOK || body["passed"] != true || body["xp_eligible"] != true {
		t.Fatalf("seeded lab must pass: %d %v", status, body)
	}
	// Frontend-compatible check fields.
	checks, _ = body["checks"].([]any)
	for _, c := range checks {
		cm, _ := c.(map[string]any)
		if cm["rule_id"] == "" || cm["passed"] != true || cm["status"] != "pass" {
			t.Fatalf("check must carry rule_id/passed/status: %v", cm)
		}
	}

	// Explicit rule with no DNS source: dns_resolves is unknown
	// (M8: the VPC records no DNS state and the engine at
	// 127.0.0.1:1 is unreachable, so there is nothing to check).
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/validate", token, map[string]any{
		"rules": []any{map[string]any{"rule": "dns_resolves", "params": map[string]any{"name": "web.internal"}}},
	})
	if status != http.StatusOK {
		t.Fatalf("validate explicit rules: %d %v", status, body)
	}
	checks, _ = body["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("explicit rules: %v", body)
	}
	if cm, _ := checks[0].(map[string]any); cm["status"] != "unknown" || cm["passed"] != false {
		t.Fatalf("dns_resolves must be unknown, never pass: %v", cm)
	}
	if body["passed"] != false {
		t.Fatal("unknown check must not pass overall")
	}
}

// --- terminal -------------------------------------------------------------

func wsURL(t *testing.T, tsURL, labID, token string) string {
	t.Helper()
	u, err := url.Parse(tsURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "ws"
	u.Path = "/api/v1/labs/" + labID + "/terminal"
	u.RawQuery = "token=" + url.QueryEscape(token)
	return u.String()
}

func TestM6TerminalTokenAndWebSocket(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")

	// Issue a token.
	status, body := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terminal/token", token, nil)
	if status != http.StatusOK {
		t.Fatalf("terminal token: %d %v", status, body)
	}
	termToken, _ := body["token"].(string)
	if termToken == "" || body["expires_at"] == "" {
		t.Fatalf("terminal token shape: %v", body)
	}
	if termToken == token {
		t.Fatal("terminal token must not be the Bearer token")
	}

	// Connect with the token; the simulated banner must be labelled.
	ws, err := websocket.Dial(wsURL(t, ts.URL, labID, termToken), "", "http://localhost/")
	if err != nil {
		t.Fatalf("ws dial with terminal token: %v", err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var banner string
	if err := websocket.Message.Receive(ws, &banner); err != nil {
		t.Fatalf("ws banner: %v", err)
	}
	if !strings.Contains(strings.ToLower(banner), "simulated") {
		t.Fatalf("terminal banner must say simulated: %q", banner)
	}
	// Type a command: echo is keystroke-echoed, then executed on \r.
	if err := websocket.Message.Send(ws, "help\r"); err != nil {
		t.Fatalf("ws send: %v", err)
	}
	var out string
	deadline := time.Now().Add(5 * time.Second)
	gotHelp := false
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := websocket.Message.Receive(ws, &out); err != nil {
			break
		}
		if strings.Contains(out, "Allowlisted commands") || strings.Contains(out, "allowlisted") {
			gotHelp = true
			break
		}
	}
	if !gotHelp {
		t.Fatalf("simulated shell did not answer help (last chunk %q)", out)
	}
	// Ping/pong over JSON messages.
	if err := websocket.Message.Send(ws, `{"type":"ping"}`); err != nil {
		t.Fatalf("ws ping send: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var pong string
	if err := websocket.Message.Receive(ws, &pong); err != nil {
		t.Fatalf("ws pong: %v", err)
	}
	var pongMsg map[string]any
	if err := json.Unmarshal([]byte(pong), &pongMsg); err != nil || pongMsg["type"] != "pong" {
		t.Fatalf("expected pong JSON, got %q (err %v)", pong, err)
	}
	_ = ws.Close()

	// The token was single-use: a second dial must fail.
	if _, err := websocket.Dial(wsURL(t, ts.URL, labID, termToken), "", "http://localhost/"); err == nil {
		t.Fatal("reused terminal token must be rejected")
	}
	// A Bearer token in the URL must also be rejected.
	if _, err := websocket.Dial(wsURL(t, ts.URL, labID, token), "", "http://localhost/"); err == nil {
		t.Fatal("long-lived Bearer token must never authenticate the WebSocket")
	}
}

// --- terraform ------------------------------------------------------------

func terraformTestServer(t *testing.T, stub *terraform.TestStubRunner) (*Server, *httptest.Server, store.JobStore) {
	t.Helper()
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	jobStore := store.NewMemoryJobStore()
	srv := NewServerWithDeps(Deps{
		Users: users, Labs: store.NewMemoryLabStore(labStore),
		Courses: store.NewMemoryCourseStore(), Audit: store.NewMemoryAuditStore(),
		Registry: providers.NewRegistry(), Network: networkclient.New("127.0.0.1:1"),
		Jobs: jobStore, TerraformRunner: stub,
		EmulatorEndpoint: "http://localhost:4566", MinIOEndpoint: "localhost:9000",
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts, jobStore
}

func runWorkerOnce(t *testing.T, srv *Server, jobStore store.JobStore, workdir string) {
	t.Helper()
	svc := &jobs.Service{
		Jobs: jobStore, Labs: srv.Labs, Audit: srv.Audit,
		TerraformRunner: srv.TerraformRunner, TerraformRuns: srv.TerraformRuns,
		Resources: srv.Resources,
		TerraformCfg: jobs.TerraformConfig{
			EmulatorEndpoint: "http://localhost:4566",
			MinIOEndpoint:    "localhost:9000",
			WorkdirRoot:      workdir,
		},
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("worker RunOnce: %v", err)
	}
}

func getRun(t *testing.T, srv *Server, runID string) *store.TerraformRun {
	t.Helper()
	run, err := srv.TerraformRuns.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("get run %s: %v", runID, err)
	}
	return run
}

func TestM7TerraformPlanApproveApply(t *testing.T) {
	stub := &terraform.TestStubRunner{}
	srv, ts, jobStore := terraformTestServer(t, stub)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")
	workdir := t.TempDir()

	// Plan: 202 with a run id.
	status, body := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/plan", token, map[string]any{
		"configuration": `resource "aws_vpc" "lab" { cidr_block = "10.0.0.0/16" }`,
	})
	if status != http.StatusAccepted {
		t.Fatalf("plan: %d %v", status, body)
	}
	planRunID, _ := body["run_id"].(string)
	if planRunID == "" {
		t.Fatalf("plan response must carry run_id: %v", body)
	}

	// Apply BEFORE approval: 409.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/apply", token, map[string]any{
		"plan_id": "no-such-plan",
	})
	if status != http.StatusNotFound {
		t.Fatalf("apply unknown plan: got %d, want 404: %v", status, body)
	}

	// Drive the worker: the plan runs (stub) and succeeds.
	runWorkerOnce(t, srv, jobStore, workdir)
	planRun := getRun(t, srv, planRunID)
	if planRun.State != "SUCCEEDED" || planRun.PlanID == "" || len(planRun.PlanJSON) == 0 {
		t.Fatalf("plan run after worker: %+v", planRun)
	}
	if len(stub.Calls) == 0 || stub.Calls[0].Operation != terraform.OpPlan {
		t.Fatal("plan must have run through the test stub runner")
	}
	// Apply with the plan id but no approval yet: 409.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/apply", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusConflict {
		t.Fatalf("apply without approval: got %d, want 409: %v", status, body)
	}

	// Approve, then apply: 202.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/approve", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusOK || body["approved"] != true {
		t.Fatalf("approve: %d %v", status, body)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/apply", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusAccepted {
		t.Fatalf("apply after approval: %d %v", status, body)
	}
	applyRunID, _ := body["run_id"].(string)
	runWorkerOnce(t, srv, jobStore, workdir)
	applyRun := getRun(t, srv, applyRunID)
	if applyRun.State != "SUCCEEDED" {
		t.Fatalf("apply run after worker: %+v", applyRun)
	}
	// Apply synced the plan's resources back into lab_resources.
	list, err := srv.Resources.ListResources(context.Background(), labID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, r := range list {
		kinds[r.Kind] = true
	}
	for _, want := range []string{"vpc", "subnet", "instance"} {
		if !kinds[want] {
			t.Fatalf("apply did not sync %s resources: %+v", want, list)
		}
	}

	// Destroy: 202, worker succeeds, emulator resources removed.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/destroy", token, nil)
	if status != http.StatusAccepted {
		t.Fatalf("destroy: %d %v", status, body)
	}
	destroyRunID, _ := body["run_id"].(string)
	runWorkerOnce(t, srv, jobStore, workdir)
	if got := getRun(t, srv, destroyRunID); got.State != "SUCCEEDED" {
		t.Fatalf("destroy run: %+v", got)
	}
	list, _ = srv.Resources.ListResources(context.Background(), labID)
	for _, r := range list {
		if r.Provider == "aws-emulator" {
			t.Fatalf("destroy must remove emulator resources, still present: %+v", r)
		}
	}

	// Run history lists every run, newest first.
	status, body = do(t, "GET", ts.URL+"/api/v1/labs/"+labID+"/terraform/runs", token, nil)
	if status != http.StatusOK {
		t.Fatalf("runs history: %d %v", status, body)
	}
	runs, _ := body["runs"].([]any)
	if len(runs) != 3 {
		t.Fatalf("expected 3 terraform runs (plan/apply/destroy), got %d: %v", len(runs), runs)
	}
}

func TestM7TerraformExpiredPlanCannotApply(t *testing.T) {
	stub := &terraform.TestStubRunner{}
	srv, ts, jobStore := terraformTestServer(t, stub)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")
	workdir := t.TempDir()

	status, body := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/plan", token, nil)
	if status != http.StatusAccepted {
		t.Fatalf("plan: %d %v", status, body)
	}
	planRunID, _ := body["run_id"].(string)
	runWorkerOnce(t, srv, jobStore, workdir)
	planRun := getRun(t, srv, planRunID)
	// Approve, then expire the plan by hand, then apply must be 409.
	status, _ = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/approve", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusOK {
		t.Fatalf("approve: %d", status)
	}
	expired := time.Now().UTC().Add(-time.Minute)
	planRun.PlanExpiresAt = &expired
	if _, err := srv.TerraformRuns.UpdateRun(context.Background(), planRun); err != nil {
		t.Fatal(err)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/apply", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusConflict {
		t.Fatalf("apply with expired plan: got %d, want 409: %v", status, body)
	}
	// Approving an expired plan is also a 409.
	status, _ = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/approve", token, map[string]any{
		"plan_id": planRun.PlanID,
	})
	if status != http.StatusConflict {
		t.Fatalf("approve expired plan: got %d, want 409", status)
	}
}

func TestM7TerraformUnavailableWithoutDocker(t *testing.T) {
	// Default server wiring uses the ContainerRunner: no Docker in
	// this sandbox, so terraform actions are 503, never faked.
	_, ts := testServer(t)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")
	status, body := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/plan", token, nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("plan without Docker: got %d, want 503: %v", status, body)
	}
	if errBody, _ := body["error"].(map[string]any); errBody["code"] != "terraform_unavailable" {
		t.Fatalf("plan 503 must name terraform_unavailable: %v", body)
	}
	// No run row was created for the refused plan.
	status, body = do(t, "GET", ts.URL+"/api/v1/labs/"+labID+"/terraform/runs", token, nil)
	if status != http.StatusOK {
		t.Fatalf("runs: %d %v", status, body)
	}
	if runs, _ := body["runs"].([]any); len(runs) != 0 {
		t.Fatalf("refused plan must not create a run: %v", runs)
	}
}

func TestM7TerraformRejectsRealAWSConfig(t *testing.T) {
	stub := &terraform.TestStubRunner{}
	_, ts, _ := terraformTestServer(t, stub)
	token := register(t, ts)
	labID, _ := createLab(t, ts.URL, token, "vpc-fundamentals")
	status, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/terraform/plan", token, map[string]any{
		"configuration": `# endpoint "https://s3.amazonaws.com"`,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("config referencing amazonaws.com: got %d, want 400", status)
	}
}
