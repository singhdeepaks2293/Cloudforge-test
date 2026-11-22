// Package advanced serves the CloudForge Advanced section: the
// job-ready, industry and end-to-end project catalogue, the
// interview walkthroughs, the skills checklist, and portfolio
// artefact generation.
//
// The package is deliberately self-contained: it does not import
// the httpapi package (which owns the router) and it does not touch
// the lab, validation or curriculum stores beyond the small
// ProgressStore interface below. Router wiring lives in
// docs/advanced/ROUTES.md for the coordinator to apply; the
// handlers here take the already-authenticated user id as an
// argument so the router owns auth exactly as it does for the
// existing curriculum endpoints.
//
// Content note: the Advanced tracks, scenarios, interviews and
// skills checklist are defined in curriculum/advanced/ and are
// validated by curriculum/validate.py. This package only serves
// that content and records per-user progress evidence.
package advanced

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

// ProgressStore is the only persistence this package needs: the
// user's learning stats (for the completion check) and the track
// definition (for the artefact content). Both are satisfied by the
// existing curriculum store implementations.
type ProgressStore interface {
	UserStats(ctx context.Context, userID string) (*curriculum.Stats, error)
	GetTrack(ctx context.Context, id string) (*curriculum.Track, error)
}

// ErrProjectNotCompleted is returned when a portfolio artefact is
// requested for a project the user has not completed.
var ErrProjectNotCompleted = errors.New("project not completed")

// ErrUnknownProject is returned for a project id that is not in
// the Advanced catalogue.
var ErrUnknownProject = errors.New("unknown advanced project")

// Kind distinguishes the two kinds of Advanced project.
const (
	KindIndustry = "industry"   // real-world industry example
	KindEndToEnd = "end-to-end" // CI/CD / rollout project
)

// Project is one Advanced project in the catalogue. LabDefinitionID
// is an existing lab definition (see backend/internal/labs) so the
// project is launchable through the normal lab flow; LiteLaunchable
// is false when the lab exceeds the 30-minute lite limit.
type Project struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Kind            string   `json:"kind"`
	Summary         string   `json:"summary"`
	LabDefinitionID string   `json:"lab_definition_id"`
	DurationMin     int      `json:"duration_minutes"`
	LiteLaunchable  bool     `json:"lite_launchable"`
	XPReward        int      `json:"xp_reward"`
	GradingRules    []string `json:"grading_rules"`
}

// Projects is the Advanced catalogue. It mirrors the tracks in
// curriculum/advanced/; the track YAML is the source of truth for
// lessons, tasks and grading, this list is only the launch index.
var Projects = []Project{
	{
		ID: "advanced-multi-tier", Title: "Multi-Tier Production Deployment", Kind: KindIndustry,
		Summary:         "Public web tier, private application tier and isolated data tier across two Availability Zones, deployed with Terraform and validated against real state.",
		LabDefinitionID: "adv-multi-tier", DurationMin: 120, LiteLaunchable: false, XPReward: 300,
		GradingRules: []string{"vpc_exists", "subnet_count", "route_to_igw", "route_to_nat", "nat_gateway_exists", "instance_running", "load_balancer_healthy_targets", "security_group_denies"},
	},
	{
		ID: "advanced-ha-dr", Title: "High Availability & Disaster Recovery", Kind: KindIndustry,
		Summary:         "Multi-AZ design, failover that actually happens, backups that restore, and a disaster recovery plan you have rehearsed.",
		LabDefinitionID: "adv-ha-dr", DurationMin: 60, LiteLaunchable: false, XPReward: 300,
		GradingRules: []string{"vpc_exists", "subnet_count", "route_to_igw", "route_to_nat", "nat_gateway_exists", "instance_running", "load_balancer_healthy_targets"},
	},
	{
		ID: "advanced-migration", Title: "Migration: On-Prem to Cloud", Kind: KindIndustry,
		Summary:         "Discover an on-prem estate, choose a strategy per workload, cut over without a big-bang outage, and prove the new home works.",
		LabDefinitionID: "adv-migration", DurationMin: 30, LiteLaunchable: true, XPReward: 300,
		GradingRules: []string{"vpc_exists", "subnet_count", "instance_running", "instance_reachable", "security_group_allows", "dns_resolves"},
	},
	{
		ID: "advanced-cost-optimisation", Title: "Cost Optimisation in Production", Kind: KindIndustry,
		Summary:         "Measure the bill, right-size from evidence, commit only to steady usage, and keep the savings with guardrails.",
		LabDefinitionID: "adv-cost", DurationMin: 30, LiteLaunchable: true, XPReward: 250,
		GradingRules: []string{"vpc_exists", "subnet_count", "instance_running", "instance_reachable", "security_group_denies", "metric_below"},
	},
	{
		ID: "advanced-incident-response", Title: "Security Incident Response", Kind: KindIndustry,
		Summary:         "Detect a real incident, contain it without destroying evidence, eradicate the cause, recover safely, and write the review.",
		LabDefinitionID: "adv-incident", DurationMin: 30, LiteLaunchable: true, XPReward: 300,
		GradingRules: []string{"vpc_exists", "subnet_count", "instance_running", "instance_reachable", "security_group_denies"},
	},
	{
		ID: "advanced-cicd-pipeline", Title: "End-to-End CI/CD: Staging to Production", Kind: KindEndToEnd,
		Summary:         "One immutable artifact through staging to production: tested, approved, promoted, and rolled back when the release is bad.",
		LabDefinitionID: "adv-cicd", DurationMin: 30, LiteLaunchable: true, XPReward: 350,
		GradingRules: []string{"vpc_exists", "subnet_count", "instance_running", "load_balancer_healthy_targets", "instance_reachable", "security_group_allows", "metric_below"},
	},
	{
		ID: "advanced-production-rollout", Title: "Production Rollout: Canary, Rollback & Alerting", Kind: KindEndToEnd,
		Summary:         "Canary rollout with numeric health gates, a rollback that runs in minutes, and alerting that tells you before your users do.",
		LabDefinitionID: "adv-rollout", DurationMin: 30, LiteLaunchable: true, XPReward: 350,
		GradingRules: []string{"vpc_exists", "subnet_count", "instance_running", "load_balancer_healthy_targets", "instance_reachable", "security_group_allows", "metric_below"},
	},
}

// FindProject returns the catalogue entry for id.
func FindProject(id string) (Project, bool) {
	for _, p := range Projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// RubricCriterion is one graded dimension of an interview
// walkthrough: what good looks like, not a single correct answer.
type RubricCriterion struct {
	Criterion     string `json:"criterion"`
	GoodLooksLike string `json:"good_looks_like"`
}

// Interview is one job-ready scenario walkthrough.
type Interview struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	RoleFocus        string            `json:"role_focus"`
	Difficulty       string            `json:"difficulty"`
	Problem          string            `json:"problem"`
	ExpectedApproach []string          `json:"expected_approach"`
	Rubric           []RubricCriterion `json:"rubric"`
	Pitfalls         []string          `json:"pitfalls"`
	RelatedProjects  []string          `json:"related_projects"`
}

// Skill is one job-description skill mapped to the Advanced
// projects that teach it.
type Skill struct {
	Name             string   `json:"name"`
	Required         bool     `json:"required"`
	AdvancedProjects []string `json:"advanced_projects"`
}

// Archetype is one job-description archetype (a composite sketch
// of common postings, not a copy of any single listing).
type Archetype struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Skills  []Skill `json:"skills"`
}

// Service serves the Advanced section. CurriculumDir is the
// curriculum/ directory (for interviews.json and
// skills-checklist.json); Store is the curriculum progress store
// (for the portfolio completion check). Either may be nil: content
// endpoints then report unavailable, and the portfolio endpoint
// reports unavailable instead of inventing evidence.
//
// Skills checklist state is kept in memory, per process. That is
// an honest limitation: it survives no restart and no second
// replica. A durable version needs a store migration, which is
// out of scope for this workstream; the API shape (GET/PUT of a
// skill-id -> checked map) is kept stable so persistence can be
// added behind it later.
type Service struct {
	CurriculumDir string
	Store         ProgressStore

	mu        sync.Mutex
	checklist map[string]map[string]bool // userID -> skillID -> checked
}

// NewService returns a Service with empty checklist state.
func NewService(curriculumDir string, store ProgressStore) *Service {
	return &Service{CurriculumDir: curriculumDir, Store: store, checklist: map[string]map[string]bool{}}
}

func (s *Service) interviewsPath() string {
	return filepath.Join(s.CurriculumDir, "advanced", "interviews.json")
}

func (s *Service) skillsPath() string {
	return filepath.Join(s.CurriculumDir, "advanced", "skills-checklist.json")
}

// ListInterviews returns the interview walkthroughs from
// curriculum/advanced/interviews.json, sorted by id for stable output.
func (s *Service) ListInterviews() ([]Interview, error) {
	if s.CurriculumDir == "" {
		return nil, fmt.Errorf("curriculum directory is not configured")
	}
	raw, err := os.ReadFile(s.interviewsPath())
	if err != nil {
		return nil, fmt.Errorf("read interviews: %w", err)
	}
	var doc struct {
		Walkthroughs []Interview `json:"walkthroughs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse interviews: %w", err)
	}
	sort.Slice(doc.Walkthroughs, func(i, j int) bool { return doc.Walkthroughs[i].ID < doc.Walkthroughs[j].ID })
	return doc.Walkthroughs, nil
}

// ListArchetypes returns the job-description archetypes from
// curriculum/advanced/skills-checklist.json.
func (s *Service) ListArchetypes() ([]Archetype, error) {
	if s.CurriculumDir == "" {
		return nil, fmt.Errorf("curriculum directory is not configured")
	}
	raw, err := os.ReadFile(s.skillsPath())
	if err != nil {
		return nil, fmt.Errorf("read skills checklist: %w", err)
	}
	var doc struct {
		Archetypes []Archetype `json:"archetypes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse skills checklist: %w", err)
	}
	return doc.Archetypes, nil
}

// Checklist returns the user's checked skills (a copy; never nil).
func (s *Service) Checklist(userID string) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for k, v := range s.checklist[userID] {
		out[k] = v
	}
	return out
}

// SetChecklist replaces the user's checked skills. Unknown skill
// ids are kept as given — the frontend owns the skill list and the
// server must not silently drop a newer client's entries.
func (s *Service) SetChecklist(userID string, checked map[string]bool) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]bool{}
	for k, v := range checked {
		if v {
			next[k] = true
		}
	}
	s.checklist[userID] = next
	out := map[string]bool{}
	for k, v := range next {
		out[k] = v
	}
	return out
}
