// Phase 5 HTTP tests: Terraform Learning — dashboard, courses +
// knowledge checks, provider docs, projects/files, local
// validate/format/plan-preview, confirmation gating, honest
// terraform_unavailable without Docker, state versions/locks/
// backup/restore/drift. Runs against the in-memory Phase 3 store.
package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func tflearnTestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	srv := NewServerWithDeps(Deps{
		P3:               store.NewMemoryPhase3Store(),
		P3DatabaseURL:    "",
		P3RedisAddr:      "localhost:1",
		P3DynamoEndpoint: "http://localhost:1",
		P3WorkdirRoot:    t.TempDir(),
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts, adminToken(t, srv, ts)
}

func tflearnProjectID(t *testing.T, out map[string]any) string {
	t.Helper()
	proj, _ := out["project"].(map[string]any)
	id, _ := proj["id"].(string)
	if id == "" {
		t.Fatalf("no project id in %v", out)
	}
	return id
}

func TestTFlearnDashboardAndCourses(t *testing.T) {
	_, ts, token := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"

	code, out := p3Call(t, "GET", base+"/dashboard", token, nil)
	if code != 200 {
		t.Fatalf("dashboard: %d %v", code, out)
	}
	progress, _ := out["progress"].(map[string]any)
	if progress["total_lessons"] != float64(55) {
		t.Errorf("dashboard total_lessons = %v, want 55", progress["total_lessons"])
	}
	if progress["skill_level"] != "Novice" {
		t.Errorf("fresh dashboard skill_level = %v, want Novice", progress["skill_level"])
	}
	if progress["pending_exercises"] != float64(55) {
		t.Errorf("fresh dashboard pending_exercises = %v, want 55", progress["pending_exercises"])
	}

	code, out = p3Call(t, "GET", base+"/courses", token, nil)
	if code != 200 {
		t.Fatalf("courses: %d %v", code, out)
	}
	levels, _ := out["levels"].([]any)
	if len(levels) != 3 {
		t.Fatalf("want 3 levels, got %d", len(levels))
	}
	l1, _ := levels[0].(map[string]any)
	if l1["total"] != float64(15) {
		t.Errorf("L1 total = %v, want 15", l1["total"])
	}

	// Lesson detail hides nothing it should not: no correct answer
	// in the knowledge check payload.
	code, out = p3Call(t, "GET", base+"/courses/1/l1-13-plan", token, nil)
	if code != 200 {
		t.Fatalf("lesson: %d %v", code, out)
	}
	lesson, _ := out["lesson"].(map[string]any)
	kc, _ := lesson["knowledge_check"].(map[string]any)
	if _, ok := kc["correct_answer"]; ok {
		t.Error("lesson detail leaks correct_answer")
	}

	// Wrong answer: not completed, 200 with correct=false.
	code, out = p3Call(t, "POST", base+"/courses/1/l1-13-plan/complete", token, map[string]any{"answer_index": 0})
	if code != 200 || out["correct"] != false || out["completed"] != false {
		t.Fatalf("wrong answer: %d %v", code, out)
	}
	// Right answer: completed.
	code, out = p3Call(t, "POST", base+"/courses/1/l1-13-plan/complete", token, map[string]any{"answer_index": 1})
	if code != 200 || out["correct"] != true || out["completed"] != true {
		t.Fatalf("right answer: %d %v", code, out)
	}
	// Dashboard now shows one completed lesson.
	code, out = p3Call(t, "GET", base+"/dashboard", token, nil)
	progress, _ = out["progress"].(map[string]any)
	if progress["completed_lessons"] != float64(1) {
		t.Errorf("dashboard completed_lessons = %v, want 1", progress["completed_lessons"])
	}
	// Unknown lesson -> 404.
	code, _ = p3Call(t, "GET", base+"/courses/1/nope", token, nil)
	if code != 404 {
		t.Errorf("unknown lesson: want 404, got %d", code)
	}
	// Missing answer -> 400.
	code, _ = p3Call(t, "POST", base+"/courses/1/l1-01-iac/complete", token, map[string]any{})
	if code != 400 {
		t.Errorf("missing answer: want 400, got %d", code)
	}
}

func TestTFlearnProvider(t *testing.T) {
	_, ts, token := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"
	code, out := p3Call(t, "GET", base+"/provider/resources", token, nil)
	if code != 200 {
		t.Fatalf("provider resources: %d %v", code, out)
	}
	res, _ := out["resources"].([]any)
	if len(res) < 5 {
		t.Fatalf("want >= 5 provider resources, got %d", len(res))
	}
	first, _ := res[0].(map[string]any)
	if first["cloudforge_api"] == "" || first["fidelity"] == "" {
		t.Errorf("provider resource missing API mapping/fidelity: %v", first)
	}
	code, out = p3Call(t, "GET", base+"/provider", token, nil)
	if code != 200 || out["install_docs"] == "" {
		t.Fatalf("provider docs: %d %v", code, out)
	}
}

func TestTFlearnProjectEditorFlow(t *testing.T) {
	_, ts, token := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"

	// Create from the VPC template: starter files are seeded.
	code, out := p3Call(t, "POST", base+"/projects", token, map[string]any{"name": "vpc-lab", "template": "p4-01-vpc"})
	if code != 201 {
		t.Fatalf("create project: %d %v", code, out)
	}
	id := tflearnProjectID(t, out)
	// Duplicate name for the same user -> 409.
	code, _ = p3Call(t, "POST", base+"/projects", token, map[string]any{"name": "vpc-lab"})
	if code != 409 {
		t.Errorf("duplicate project name: want 409, got %d", code)
	}
	// Unknown template -> 400.
	code, _ = p3Call(t, "POST", base+"/projects", token, map[string]any{"name": "other", "template": "p9-99"})
	if code != 400 {
		t.Errorf("unknown template: want 400, got %d", code)
	}

	// Files: template seeded main.tf.
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/files", token, nil)
	if code != 200 {
		t.Fatalf("list files: %d %v", code, out)
	}
	files, _ := out["files"].([]any)
	if len(files) < 3 {
		t.Fatalf("want >= 3 template files, got %d", len(files))
	}

	// Invalid HCL on save -> 400 with real issues.
	bad := "resource \"aws_vpc\" \"main\" {\n  cidr_block = var.missing\n}\n"
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/files", token, map[string]any{"path": "bad.tf", "content": bad})
	if code != 400 {
		t.Fatalf("invalid HCL save: want 400, got %d %v", code, out)
	}
	if _, ok := out["issues"]; !ok {
		t.Errorf("invalid HCL save should return issues: %v", out)
	}
	// Path traversal -> 400.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/files", token, map[string]any{"path": "../escape.tf", "content": ""})
	if code != 400 {
		t.Errorf("path traversal: want 400, got %d", code)
	}
	// Valid save.
	good := "resource \"aws_subnet\" \"extra\" {\n  vpc_id = cloudforge_vpc.main.id\n  cidr_block = \"10.0.9.0/24\"\n}\n"
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/files", token, map[string]any{"path": "extra.tf", "content": good})
	if code != 201 {
		t.Fatalf("valid save: %d %v", code, out)
	}

	// Local validate: the template references cloudforge_vpc.main,
	// which is declared, so it must be valid.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/validate", token, map[string]any{})
	if code != 200 || out["valid"] != true {
		t.Fatalf("validate: %d %v", code, out)
	}
	if out["check"] != "local" {
		t.Errorf("validate must be labelled local: %v", out)
	}

	// Format: unformatted file gets reformatted and persisted.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/files", token, map[string]any{"path": "messy.tf", "content": "resource \"aws_vpc\" \"m\" {\ncidr_block=\"10.2.0.0/16\"\n}\n"})
	if code != 201 {
		t.Fatalf("messy save: %d", code)
	}
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/format", token, map[string]any{})
	if code != 200 {
		t.Fatalf("format: %d %v", code, out)
	}
	formatted, _ := out["formatted_files"].([]any)
	found := false
	for _, f := range formatted {
		if f == "messy.tf" {
			found = true
		}
	}
	if !found {
		t.Errorf("format should report messy.tf changed: %v", out)
	}

	// Plan preview: local static diff, labelled as such.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/plan-preview", token, map[string]any{})
	if code != 200 {
		t.Fatalf("plan preview: %d %v", code, out)
	}
	if out["source"] != "cloudforge-local-diff" {
		t.Errorf("plan preview source = %v, want cloudforge-local-diff", out["source"])
	}
	changes, _ := out["changes"].([]any)
	if len(changes) == 0 {
		t.Errorf("plan preview should list creates: %v", out)
	}
	planID, _ := out["plan_id"].(string)
	// Approve the preview.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/plans/"+planID+"/approve", token, map[string]any{})
	if code != 200 || out["approved"] != true {
		t.Fatalf("approve preview: %d %v", code, out)
	}
	// Approve an unknown plan -> 404.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/plans/nope/approve", token, map[string]any{})
	if code != 404 {
		t.Errorf("approve unknown plan: want 404, got %d", code)
	}
}

func TestTFlearnExecutionGating(t *testing.T) {
	_, ts, token := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"

	code, out := p3Call(t, "POST", base+"/projects", token, map[string]any{"name": "exec-proj", "template": "p4-01-vpc"})
	if code != 201 {
		t.Fatalf("create project: %d %v", code, out)
	}
	id := tflearnProjectID(t, out)

	// No lab linked: apply without confirm -> 409 confirm_required
	// (gating runs before the lab check).
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/executions", token, map[string]any{"operation": "apply"})
	if code != 409 {
		t.Fatalf("apply without confirm: want 409, got %d %v", code, out)
	}
	// Destroy without confirm -> 409.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/executions", token, map[string]any{"operation": "destroy"})
	if code != 409 {
		t.Errorf("destroy without confirm: want 409, got %d", code)
	}
	// Plan with no lab -> 409 lab_required.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/executions", token, map[string]any{"operation": "plan"})
	if code != 409 {
		t.Fatalf("plan without lab: want 409, got %d %v", code, out)
	}
	// Unknown operation -> 400.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/executions", token, map[string]any{"operation": "explode"})
	if code != 400 {
		t.Errorf("unknown operation: want 400, got %d", code)
	}
	// Execution history records the failed plan attempt.
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/executions", token, nil)
	if code != 200 {
		t.Fatalf("list executions: %d %v", code, out)
	}
	execs, _ := out["executions"].([]any)
	if len(execs) == 0 {
		t.Errorf("failed executions should be recorded: %v", out)
	}
}

func TestTFlearnState(t *testing.T) {
	_, ts, token := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"

	code, out := p3Call(t, "POST", base+"/projects", token, map[string]any{"name": "state-proj", "template": "p4-02-compute"})
	if code != 201 {
		t.Fatalf("create project: %d %v", code, out)
	}
	id := tflearnProjectID(t, out)

	// Initial state is version 1, empty.
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/state", token, nil)
	if code != 200 {
		t.Fatalf("get state: %d %v", code, out)
	}
	state, _ := out["state"].(map[string]any)
	if state["version"] != float64(1) || state["locked"] != false {
		t.Errorf("initial state = %v, want v1 unlocked", state)
	}
	// Backup creates version 2.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/state/backup", token, map[string]any{})
	if code != 200 {
		t.Fatalf("backup: %d %v", code, out)
	}
	state, _ = out["state"].(map[string]any)
	if state["version"] != float64(2) {
		t.Errorf("backup version = %v, want 2", state["version"])
	}
	// Restore v1 creates version 3 (history is never rewritten).
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/state/restore", token, map[string]any{"version": 1})
	if code != 200 {
		t.Fatalf("restore: %d %v", code, out)
	}
	state, _ = out["state"].(map[string]any)
	if state["version"] != float64(3) {
		t.Errorf("restore version = %v, want 3", state["version"])
	}
	// History has all three versions.
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/state/history", token, nil)
	versions, _ := out["versions"].([]any)
	if len(versions) != 3 {
		t.Fatalf("history: want 3 versions, got %d %v", len(versions), out)
	}
	// Restore unknown version -> 404.
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/state/restore", token, map[string]any{"version": 99})
	if code != 404 {
		t.Errorf("restore unknown version: want 404, got %d", code)
	}
	// Lock: saves are refused while locked by another user... the
	// same user holds the lock here, so save succeeds; unlock works.
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/state/lock", token, map[string]any{})
	if code != 200 || out["locked"] != true {
		t.Fatalf("lock: %d %v", code, out)
	}
	code, _ = p3Call(t, "POST", base+"/projects/"+id+"/state/lock", token, map[string]any{})
	if code != 409 {
		t.Errorf("double lock: want 409, got %d", code)
	}
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/state", token, nil)
	state, _ = out["state"].(map[string]any)
	if state["locked"] != true {
		t.Errorf("state should report locked: %v", state)
	}
	code, out = p3Call(t, "POST", base+"/projects/"+id+"/state/unlock", token, map[string]any{})
	if code != 200 || out["locked"] != false {
		t.Fatalf("unlock: %d %v", code, out)
	}
	// Drift: declared resources vs empty state, no lab linked.
	code, out = p3Call(t, "GET", base+"/projects/"+id+"/state/drift", token, nil)
	if code != 200 {
		t.Fatalf("drift: %d %v", code, out)
	}
	if out["lab_linked"] != false {
		t.Errorf("drift lab_linked = %v, want false", out["lab_linked"])
	}
	drift, _ := out["drift"].([]any)
	if len(drift) != 0 {
		// Declared resources that differ from empty state are not
		// drift — they are unapplied changes. Drift must be empty
		// here (no live resources, state matches nothing declared
		// only via attributes, and DetectDrift only flags
		// attribute drift for resources present in state).
		t.Errorf("drift should be empty with no state and no lab: %v", drift)
	}
}

func TestTFlearnOwnership(t *testing.T) {
	srv, ts, admin := tflearnTestServer(t)
	base := ts.URL + "/api/v1/terraform-learning"
	code, out := p3Call(t, "POST", base+"/projects", admin, map[string]any{"name": "owned"})
	if code != 201 {
		t.Fatalf("create project: %d %v", code, out)
	}
	id := tflearnProjectID(t, out)
	// A second (non-admin) user must get 404, not the project.
	token := func() string {
		// Register a learner via the auth API.
		_, out := p3Call(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
			"name": "Learner", "email": "learner@example.com", "password": "learner-pass-123",
		})
		tok, _ := out["token"].(string)
		if tok == "" {
			t.Fatalf("learner register failed: %v", out)
		}
		return tok
	}()
	_ = srv
	code, _ = p3Call(t, "GET", base+"/projects/"+id, token, nil)
	if code != 404 {
		t.Errorf("other user's project: want 404, got %d", code)
	}
	// Unauthenticated dashboard -> 401.
	code, _ = p3Call(t, "GET", base+"/dashboard", "", nil)
	if code != 401 {
		t.Errorf("unauthenticated dashboard: want 401, got %d", code)
	}
	// Delete with a bad path segment is still a 404, not a leak.
	code, _ = p3Call(t, "GET", base+"/projects/does-not-exist", admin, nil)
	if code != 404 {
		t.Errorf("unknown project: want 404, got %d", code)
	}
	_ = strings.Contains // keep strings imported for future edits
}
