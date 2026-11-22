// Milestone 8 HTTP tests: curriculum tracks, lesson completion,
// server-side grading, progress/achievements, and the break-and-fix
// start/solve flow (engine unreachable, so fault_pending).
package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

func seedTestCurriculum(t *testing.T, srv *Server) {
	t.Helper()
	rep, err := curriculum.SeedDir(context.Background(), srv.Curriculum, filepath.Join("..", "..", "..", "curriculum"))
	if err != nil {
		t.Fatalf("SeedDir: %v", err)
	}
	if rep.Tracks != 23 || rep.Lessons != 122 {
		t.Fatalf("seed report: %+v", rep)
	}
}

func TestCurriculumFlow(t *testing.T) {
	srv, ts := m2Server(t)
	seedTestCurriculum(t, srv)
	token := registerAs(t, ts, "learner-m8@example.com")

	// Track list is public; networking is one of the seeded tracks.
	status, body := do(t, "GET", ts.URL+"/api/v1/curriculum/tracks", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list tracks: %d %v", status, body)
	}
	tracks, _ := body["tracks"].([]any)
	if len(tracks) < 23 {
		t.Fatalf("tracks: %d, want >= 23", len(tracks))
	}

	// Track detail includes theory and hides correct answers.
	status, body = do(t, "GET", ts.URL+"/api/v1/curriculum/tracks/networking", token, nil)
	if status != http.StatusOK {
		t.Fatalf("get track: %d %v", status, body)
	}
	modules, _ := body["modules"].([]any)
	if len(modules) != 1 {
		t.Fatalf("networking modules: %d", len(modules))
	}
	lessons, _ := modules[0].(map[string]any)["lessons"].([]any)
	if len(lessons) != 6 {
		t.Fatalf("networking lessons: %d", len(lessons))
	}
	if lessons[0].(map[string]any)["theory"] == "" {
		t.Fatal("lesson theory is empty in track detail")
	}
	assessment, _ := body["assessment"].([]any)
	if len(assessment) != 6 {
		t.Fatalf("assessment questions: %d", len(assessment))
	}
	if _, hasAnswer := assessment[0].(map[string]any)["correct_answer"]; hasAnswer {
		t.Fatal("track detail must not expose correct_answer")
	}

	// Lesson completion requires enrollment.
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/lessons/cloud-networking/complete", token, map[string]any{})
	if status != http.StatusConflict {
		t.Fatalf("complete before enroll: %d %v (want 409)", status, body)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/tracks/networking/enroll", token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("enroll: %d %v", status, body)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/lessons/cloud-networking/complete", token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("complete lesson: %d %v", status, body)
	}
	if body["xp_awarded"] != float64(20) {
		t.Fatalf("first lesson xp_awarded: %v, want 20", body["xp_awarded"])
	}
	awarded, _ := body["achievements_awarded"].([]any)
	foundFirstSteps := false
	for _, a := range awarded {
		if a.(map[string]any)["id"] == "first-steps" {
			foundFirstSteps = true
		}
	}
	if !foundFirstSteps {
		t.Fatalf("first lesson should award first-steps, got %v", awarded)
	}
	// Re-completion awards nothing.
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/lessons/cloud-networking/complete", token, map[string]any{})
	if status != http.StatusOK || body["xp_awarded"] != float64(0) {
		t.Fatalf("re-complete: %d %v (want xp_awarded 0)", status, body)
	}

	// Assessment: wrong answers fail with no XP; right answers pass.
	// (Networking answers, from the seeded content: 1,1,0,1,1,1.)
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/assessments/networking/submit", token,
		map[string]any{"answers": map[string]any{"0": 0, "1": 0, "2": 1, "3": 0, "4": 0, "5": 0}})
	if status != http.StatusOK {
		t.Fatalf("submit wrong: %d %v", status, body)
	}
	ass, _ := body["assessment"].(map[string]any)
	if ass["passed"] != false || ass["xp_awarded"] != float64(0) {
		t.Fatalf("wrong assessment: %v", ass)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/assessments/networking/submit", token,
		map[string]any{"answers": map[string]any{"0": 1, "1": 1, "2": 0, "3": 1, "4": 1, "5": 1}})
	if status != http.StatusOK {
		t.Fatalf("submit right: %d %v", status, body)
	}
	ass, _ = body["assessment"].(map[string]any)
	if ass["score"] != float64(100) || ass["passed"] != true || ass["xp_awarded"] != float64(20) {
		t.Fatalf("right assessment: %v", ass)
	}
	perQ, _ := ass["per_question"].([]any)
	if len(perQ) != 6 || perQ[0].(map[string]any)["correct"] != true || perQ[0].(map[string]any)["explanation"] == "" {
		t.Fatalf("per-question results: %v", perQ)
	}
	// Second pass awards no XP.
	status, body = do(t, "POST", ts.URL+"/api/v1/curriculum/assessments/networking/submit", token,
		map[string]any{"answers": map[string]any{"0": 1, "1": 1, "2": 0, "3": 1, "4": 1, "5": 1}})
	if status != http.StatusOK || body["assessment"].(map[string]any)["xp_awarded"] != float64(0) {
		t.Fatalf("second pass: %d %v (want xp_awarded 0)", status, body)
	}

	// Progress + achievements.
	status, body = do(t, "GET", ts.URL+"/api/v1/users/me/progress", token, nil)
	if status != http.StatusOK {
		t.Fatalf("my progress: %d %v", status, body)
	}
	// 20 lesson + 25 first-steps + 20 assessment + 100 exam-ace.
	if body["total_xp"] != float64(165) {
		t.Fatalf("total_xp: %v, want 165", body["total_xp"])
	}
	if body["streak_days"] != float64(1) {
		t.Fatalf("streak_days: %v, want 1", body["streak_days"])
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/users/me/achievements", token, nil)
	if status != http.StatusOK {
		t.Fatalf("my achievements: %d %v", status, body)
	}
	earned, _ := body["earned"].([]any)
	earnedIDs := map[string]bool{}
	for _, a := range earned {
		earnedIDs[a.(map[string]any)["id"].(string)] = true
	}
	if !earnedIDs["first-steps"] || !earnedIDs["exam-ace"] {
		t.Fatalf("earned achievements: %v", earnedIDs)
	}
	locked, _ := body["locked"].([]any)
	if len(locked) == 0 {
		t.Fatal("expected locked achievements")
	}
	for _, a := range locked {
		if a.(map[string]any)["locked_reason"] == "" {
			t.Fatalf("locked achievement without a reason: %v", a)
		}
	}

	// Break-and-fix: create a lab, start a scenario (engine at
	// 127.0.0.1:1 is unreachable, so the fault is pending, not
	// injected), solve fails until the fix is recorded, then passes.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs", token,
		map[string]any{"definition_id": "vpc-fundamentals", "name": "m8 break-fix lab"})
	if status != http.StatusCreated {
		t.Fatalf("create lab: %d %v", status, body)
	}
	lab, _ := body["lab"].(map[string]any)
	labID := lab["id"].(string)

	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/break-fix/start", token,
		map[string]any{"scenario_id": "missing-igw-route"})
	if status != http.StatusOK {
		t.Fatalf("break-fix start: %d %v", status, body)
	}
	if body["fault_status"] != "fault_pending" {
		t.Fatalf("fault_status with unreachable engine: %v (want fault_pending)", body["fault_status"])
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/break-fix/solve", token, map[string]any{})
	if status != http.StatusOK || body["solved"] != false {
		t.Fatalf("solve before fix: %d %v (want solved=false)", status, body)
	}
	// Record the fix as real lab state: a route table routing
	// 0.0.0.0/0 to the IGW and a load balancer with a healthy target.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token,
		map[string]any{"kind": "route-table", "name": "rtb-public",
			"config": map[string]any{"routes": []any{map[string]any{"destination": "0.0.0.0/0", "target": "igw-1"}}}})
	if status != http.StatusCreated {
		t.Fatalf("create route table: %d %v", status, body)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/resources", token,
		map[string]any{"kind": "loadbalancer", "name": "web-lb",
			"config": map[string]any{"healthy_targets": 1}})
	if status != http.StatusCreated {
		t.Fatalf("create load balancer: %d %v", status, body)
	}
	// GET /labs/{id} now carries the lab definition's rules — but
	// this lab's definition is the M1 one, so no rules are expected
	// here (validation_rules only appears for curriculum definitions).
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/break-fix/solve", token, map[string]any{})
	if status != http.StatusOK || body["solved"] != true {
		t.Fatalf("solve after fix: %d %v (want solved=true)", status, body)
	}
	if body["xp_awarded"] != float64(120) {
		t.Fatalf("solve xp_awarded: %v, want 120", body["xp_awarded"])
	}
	// Solving again awards nothing.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/break-fix/solve", token, map[string]any{})
	if status != http.StatusOK || body["xp_awarded"] != float64(0) {
		t.Fatalf("second solve: %d %v (want xp_awarded 0)", status, body)
	}
	// Breaker achievement is now earned.
	status, body = do(t, "GET", ts.URL+"/api/v1/users/me/achievements", token, nil)
	if status != http.StatusOK {
		t.Fatalf("achievements after solve: %d %v", status, body)
	}
	earned, _ = body["earned"].([]any)
	earnedIDs = map[string]bool{}
	for _, a := range earned {
		earnedIDs[a.(map[string]any)["id"].(string)] = true
	}
	if !earnedIDs["breaker"] {
		t.Fatalf("breaker should be earned after a solve: %v", earnedIDs)
	}
}

func TestCurriculumLabValidationRulesField(t *testing.T) {
	srv, ts := m2Server(t)
	seedTestCurriculum(t, srv)
	token := registerAs(t, ts, "learner-m8b@example.com")
	// A lab created from a curriculum lab definition carries its
	// validation rules in GET /labs/{id}. M1 lab ids have NO
	// curriculum definition of their own (the networking track uses
	// lab-networking-vpc), so a vpc-fundamentals lab carries no
	// attached rules and validates against its 5 DefaultRules at
	// validate time. (v3 regression, fixed in integration: the
	// Advanced HA-DR track reused the id "vpc-fundamentals" for its
	// guided lab, upserting 7 stricter rules under the M1 id and
	// breaking the E2E acceptance test. Advanced guided labs now use
	// their own adv-* ids; the 7 rules live on adv-ha-dr.)
	status, body := do(t, "POST", ts.URL+"/api/v1/labs", token,
		map[string]any{"definition_id": "vpc-fundamentals", "name": "m8 rules lab"})
	if status != http.StatusCreated {
		t.Fatalf("create lab: %d %v", status, body)
	}
	labID := body["lab"].(map[string]any)["id"].(string)
	status, body = do(t, "GET", ts.URL+"/api/v1/labs/"+labID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get lab: %d %v", status, body)
	}
	if rules, ok := body["validation_rules"].([]any); ok && len(rules) != 0 {
		t.Fatalf("vpc-fundamentals lab must carry no attached validation_rules (it uses DefaultRules), got %v", rules)
	}
	// The Advanced HA-DR lab carries its own 7 rules under adv-ha-dr.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs", token,
		map[string]any{"definition_id": "adv-ha-dr", "name": "adv ha-dr rules lab"})
	if status != http.StatusCreated {
		t.Fatalf("create adv-ha-dr lab: %d %v", status, body)
	}
	advLabID := body["lab"].(map[string]any)["id"].(string)
	status, body = do(t, "GET", ts.URL+"/api/v1/labs/"+advLabID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get adv-ha-dr lab: %d %v", status, body)
	}
	rules, ok := body["validation_rules"].([]any)
	if !ok || len(rules) != 7 {
		t.Fatalf("adv-ha-dr lab should carry its 7 validation_rules, got %v", body["validation_rules"])
	}
	// The curriculum definition itself does carry rules.
	def, err := srv.Curriculum.GetLabDefinition(context.Background(), "lab-networking-vpc")
	if err != nil || len(def.ValidationRules) != 6 {
		t.Fatalf("lab-networking-vpc definition: %+v err=%v", def, err)
	}
}
