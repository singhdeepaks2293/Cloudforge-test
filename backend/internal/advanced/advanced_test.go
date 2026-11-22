package advanced

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

// fakeStore is a ProgressStore backed by fixed data.
type fakeStore struct {
	completed map[string]bool
	stats     *curriculum.Stats
}

func (f *fakeStore) UserStats(_ context.Context, _ string) (*curriculum.Stats, error) {
	return f.stats, nil
}

func (f *fakeStore) GetTrack(_ context.Context, id string) (*curriculum.Track, error) {
	return &curriculum.Track{
		ID:             id,
		Title:          "Test Track " + id,
		Description:    "A test track.",
		EstimatedHours: 8,
		LearningObjectives: []string{
			"Do the thing",
			"Prove the thing",
		},
		Modules: []curriculum.Module{
			{ID: "m1", Title: "Module One", Lessons: []curriculum.Lesson{{Slug: "l1", Title: "Lesson One"}}},
		},
		XPReward: 300,
	}, nil
}

func TestCatalogue(t *testing.T) {
	if len(Projects) != 7 {
		t.Fatalf("expected 7 advanced projects, got %d", len(Projects))
	}
	lite := 0
	for _, p := range Projects {
		if p.ID == "" || p.Title == "" || p.LabDefinitionID == "" {
			t.Fatalf("project %+v is missing an id, title or lab definition", p)
		}
		if p.Kind != KindIndustry && p.Kind != KindEndToEnd {
			t.Fatalf("project %s has unknown kind %q", p.ID, p.Kind)
		}
		if p.XPReward <= 0 {
			t.Fatalf("project %s has no xp reward", p.ID)
		}
		if len(p.GradingRules) == 0 {
			t.Fatalf("project %s has no grading rules", p.ID)
		}
		if p.LiteLaunchable {
			lite++
			if p.DurationMin > 30 {
				t.Fatalf("project %s claims lite launchable with %d minutes", p.ID, p.DurationMin)
			}
		}
		if _, ok := FindProject(p.ID); !ok {
			t.Fatalf("FindProject(%s) failed", p.ID)
		}
	}
	if lite != 5 {
		t.Fatalf("expected 5 lite-launchable projects, got %d", lite)
	}
	if _, ok := FindProject("no-such-project"); ok {
		t.Fatal("FindProject accepted an unknown id")
	}
	// Every grading rule must be one the validation engine
	// actually implements (see backend/internal/validation):
	// an unimplemented rule grades "unknown" and can never pass.
	implemented := map[string]bool{
		"lab_exists": true, "vpc_exists": true, "subnet_count": true,
		"subnet_exists": true, "route_to_igw": true, "route_to_nat": true,
		"nat_gateway_exists": true, "nat_exists": true,
		"security_group_allows": true, "security_group_allows_ssh": true,
		"security_group_denies": true, "no_route_to_internet": true,
		"instance_running": true, "instance_reachable": true,
		"load_balancer_healthy_targets": true, "dns_resolves": true,
		"metric_below": true,
	}
	for _, p := range Projects {
		for _, r := range p.GradingRules {
			if !implemented[r] {
				t.Fatalf("project %s uses unimplemented grading rule %q", p.ID, r)
			}
		}
	}
}

func TestGeneratePortfolio(t *testing.T) {
	stats := &curriculum.Stats{
		LabsCompleted: 3, ChallengesCompleted: 2, BreakFixSolved: 4,
		CompletedTracks:     []string{"advanced-multi-tier"},
		BestAssessmentScore: 90, TotalXP: 1200,
	}
	svc := NewService("", &fakeStore{stats: stats})

	// Completed project: artefact contains the evidence and the
	// not-a-certificate footer.
	fn, md, err := svc.GeneratePortfolio(context.Background(), "user-1", "advanced-multi-tier")
	if err != nil {
		t.Fatalf("GeneratePortfolio failed: %v", err)
	}
	if fn != "cloudforge-portfolio-advanced-multi-tier.md" {
		t.Fatalf("unexpected filename %q", fn)
	}
	for _, want := range []string{
		"Test Track advanced-multi-tier", "Labs completed: 3",
		"Break-and-fix scenarios solved: 4", "Best assessment score: 90%",
		"Total XP: 1200", "not a certificate",
		"no claim of AWS certification",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("artefact is missing %q", want)
		}
	}

	// Not completed: no artefact, ever.
	if _, _, err := svc.GeneratePortfolio(context.Background(), "user-1", "advanced-ha-dr"); !errors.Is(err, ErrProjectNotCompleted) {
		t.Fatalf("expected ErrProjectNotCompleted, got %v", err)
	}
	// Unknown project.
	if _, _, err := svc.GeneratePortfolio(context.Background(), "user-1", "nope"); !errors.Is(err, ErrUnknownProject) {
		t.Fatalf("expected ErrUnknownProject, got %v", err)
	}
}

func TestChecklistState(t *testing.T) {
	svc := NewService("", nil)
	if got := svc.Checklist("u1"); len(got) != 0 {
		t.Fatalf("expected empty checklist, got %v", got)
	}
	stored := svc.SetChecklist("u1", map[string]bool{"cloud-engineer:0": true, "cloud-engineer:1": false, "devops-engineer:2": true})
	if stored["cloud-engineer:0"] != true || stored["devops-engineer:2"] != true {
		t.Fatalf("stored checklist lost entries: %v", stored)
	}
	if _, ok := stored["cloud-engineer:1"]; ok {
		t.Fatalf("unchecked skill should not be stored: %v", stored)
	}
	if got := svc.Checklist("u1"); len(got) != 2 {
		t.Fatalf("expected 2 checked skills, got %v", got)
	}
	// Another user is independent.
	if got := svc.Checklist("u2"); len(got) != 0 {
		t.Fatalf("users share checklist state: %v", got)
	}
}

func TestContentLoading(t *testing.T) {
	dir := t.TempDir()
	adv := filepath.Join(dir, "advanced")
	if err := os.MkdirAll(adv, 0o755); err != nil {
		t.Fatal(err)
	}
	interviews := `{"walkthroughs": [{"id": "interview-test", "title": "Test", "role_focus": "cloud engineer", "difficulty": "advanced", "problem": "P", "expected_approach": ["step"], "rubric": [{"criterion": "C", "good_looks_like": "G"}], "pitfalls": ["pit"], "related_projects": ["advanced-multi-tier"]}]}`
	if err := os.WriteFile(filepath.Join(adv, "interviews.json"), []byte(interviews), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := `{"archetypes": [{"id": "cloud-engineer", "title": "Cloud Engineer", "summary": "S", "skills": [{"name": "Design networks", "required": true, "advanced_projects": ["advanced-multi-tier"]}]}]}`
	if err := os.WriteFile(filepath.Join(adv, "skills-checklist.json"), []byte(skills), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(dir, nil)
	list, err := svc.ListInterviews()
	if err != nil {
		t.Fatalf("ListInterviews failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "interview-test" || len(list[0].Rubric) != 1 {
		t.Fatalf("unexpected interviews: %+v", list)
	}
	arch, err := svc.ListArchetypes()
	if err != nil {
		t.Fatalf("ListArchetypes failed: %v", err)
	}
	if len(arch) != 1 || arch[0].Skills[0].Name != "Design networks" {
		t.Fatalf("unexpected archetypes: %+v", arch)
	}

	// Missing curriculum dir: honest unavailability, not empty data.
	broken := NewService("", nil)
	if _, err := broken.ListInterviews(); err == nil {
		t.Fatal("expected an error with no curriculum dir")
	}
	if _, err := broken.ListArchetypes(); err == nil {
		t.Fatal("expected an error with no curriculum dir")
	}
}

func TestHandlers(t *testing.T) {
	stats := &curriculum.Stats{
		CompletedTracks:     []string{"advanced-multi-tier"},
		BestAssessmentScore: 100, TotalXP: 500,
	}
	svc := NewService("", &fakeStore{stats: stats})

	// Projects catalogue.
	rec := httptest.NewRecorder()
	svc.HandleProjects(rec, httptest.NewRequest(http.MethodGet, "/api/v1/advanced/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("projects: status %d", rec.Code)
	}
	var projBody struct {
		Projects []Project `json:"projects"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&projBody); err != nil || len(projBody.Projects) != 7 {
		t.Fatalf("projects: bad body: %v", err)
	}

	// Portfolio: not completed -> 409, completed -> markdown download.
	rec = httptest.NewRecorder()
	svc.HandlePortfolio(rec, httptest.NewRequest(http.MethodPost, "/api/v1/advanced/portfolio/advanced-ha-dr", nil), "user-1")
	if rec.Code != http.StatusConflict {
		t.Fatalf("portfolio incomplete: expected 409, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	svc.HandlePortfolio(rec, httptest.NewRequest(http.MethodPost, "/api/v1/advanced/portfolio/advanced-multi-tier", nil), "user-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("portfolio completed: expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("portfolio: unexpected content type %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "not a certificate") {
		t.Fatal("portfolio: missing not-a-certificate footer")
	}

	// Checklist state round-trip through the handler.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/advanced/checklist-state", strings.NewReader(`{"checked": {"cloud-engineer:0": true}}`))
	svc.HandleChecklistState(rec, req, "user-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("checklist PUT: status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	svc.HandleChecklistState(rec, httptest.NewRequest(http.MethodGet, "/api/v1/advanced/checklist-state", nil), "user-1")
	var chk struct {
		Checked map[string]bool `json:"checked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&chk); err != nil || !chk.Checked["cloud-engineer:0"] {
		t.Fatalf("checklist GET: bad body: %v", err)
	}
}
