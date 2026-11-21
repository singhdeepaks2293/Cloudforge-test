// Package curriculum loads the CloudForge curriculum YAML content
// (tracks, break-and-fix scenarios, achievements) and seeds it into
// a Store. The YAML files under curriculum/ are the source of truth
// for content; the database is the runtime copy the API serves.
//
// Streaks are consecutive calendar days (UTC) with a completed
// lesson/lab or an assessment submission.
package curriculum

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Rule is one machine-checkable validation rule from the curriculum
// vocabulary (see curriculum/README.md). It mirrors
// validation.Rule but lives here so this package does not import
// the validation engine (which depends on the store).
type Rule struct {
	Rule        string         `json:"rule" yaml:"rule"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// Question is one multiple-choice assessment question. CorrectAnswer
// is server-side only and must never be sent to the learner except
// as part of a graded submission result.
type Question struct {
	Question      string   `json:"question" yaml:"question"`
	Options       []string `json:"options" yaml:"options"`
	CorrectAnswer int      `json:"correct_answer" yaml:"correct_answer"`
	Explanation   string   `json:"explanation" yaml:"explanation"`
}

// PublicQuestion is a question without the answer, for API responses.
func (q Question) PublicQuestion() map[string]any {
	return map[string]any{"question": q.Question, "options": q.Options}
}

// Lesson is one lesson inside a module. Slug is the stable YAML
// lesson id (e.g. "cloud-networking").
type Lesson struct {
	Slug      string   `json:"id" yaml:"id"`
	Title     string   `json:"title" yaml:"title"`
	Theory    string   `json:"theory" yaml:"theory"`
	KeyPoints []string `json:"key_points" yaml:"key_points"`
	Diagram   string   `json:"diagram,omitempty" yaml:"architecture_diagram_mermaid,omitempty"`
	// DBID is the lessons-table UUID, filled in by GetTrack.
	DBID string `json:"db_id,omitempty" yaml:"-"`
}

// Module groups lessons inside a track.
type Module struct {
	ID      string   `json:"id" yaml:"id"`
	Title   string   `json:"title" yaml:"title"`
	Lessons []Lesson `json:"lessons" yaml:"lessons"`
}

// LabDefinition is a guided lab, a challenge, or a break-and-fix
// scenario definition. Kind is "guided", "challenge" or "break-fix".
type LabDefinition struct {
	ID              string   `json:"id"`
	CourseID        string   `json:"course_id,omitempty"`
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	Tasks           []string `json:"tasks,omitempty"`
	ValidationRules []Rule   `json:"validation_rules,omitempty"`
	Kind            string   `json:"kind"`
	XPReward        int      `json:"xp_reward,omitempty"`
}

// Track is one curriculum track (one YAML file).
type Track struct {
	ID                 string        `json:"id" yaml:"id"`
	Title              string        `json:"title" yaml:"title"`
	Description        string        `json:"description" yaml:"description"`
	Difficulty         string        `json:"difficulty" yaml:"difficulty"`
	EstimatedHours     float64       `json:"estimated_hours" yaml:"estimated_hours"`
	LearningObjectives []string      `json:"learning_objectives" yaml:"learning_objectives"`
	Modules            []Module      `json:"modules" yaml:"modules"`
	GuidedLab          LabDefinition `json:"guided_lab" yaml:"-"`
	Challenge          LabDefinition `json:"challenge" yaml:"-"`
	Assessment         []Question    `json:"assessment" yaml:"assessment"`
	XPReward           int           `json:"xp_reward" yaml:"xp_reward"`
	Prerequisites      []string      `json:"prerequisites" yaml:"prerequisites"`
}

// trackYAML is the on-disk shape (guided_lab/challenge differ from
// LabDefinition, so they are decoded separately).
type trackYAML struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Description        string   `yaml:"description"`
	Difficulty         string   `yaml:"difficulty"`
	EstimatedHours     float64  `yaml:"estimated_hours"`
	LearningObjectives []string `yaml:"learning_objectives"`
	Modules            []Module `yaml:"modules"`
	GuidedLab          struct {
		LabDefinitionID string   `yaml:"lab_definition_id"`
		Title           string   `yaml:"title"`
		Tasks           []string `yaml:"tasks"`
		ValidationRules []Rule   `yaml:"validation_rules"`
	} `yaml:"guided_lab"`
	Challenge struct {
		Title           string `yaml:"title"`
		Task            string `yaml:"task"`
		ValidationRules []Rule `yaml:"validation_rules"`
	} `yaml:"challenge"`
	Assessment    []Question `yaml:"assessment"`
	XPReward      int        `yaml:"xp_reward"`
	Prerequisites []string   `yaml:"prerequisites"`
}

// LessonCount is the total number of lessons in the track.
func (t *Track) LessonCount() int {
	n := 0
	for _, m := range t.Modules {
		n += len(m.Lessons)
	}
	return n
}

// Fault is the injected fault of a break-and-fix scenario.
type Fault struct {
	Kind       string         `json:"kind" yaml:"kind"`
	Target     string         `json:"target" yaml:"target"`
	Parameters map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// Scenario is one break-and-fix scenario.
type Scenario struct {
	ID              string   `json:"id" yaml:"id"`
	Title           string   `json:"title" yaml:"title"`
	Symptom         string   `json:"symptom" yaml:"symptom"`
	Fault           Fault    `json:"injected_fault" yaml:"injected_fault"`
	Hints           []string `json:"diagnosis_hints" yaml:"diagnosis_hints"`
	FixSteps        []string `json:"fix_steps" yaml:"fix_steps"`
	ValidationRules []Rule   `json:"validation_rules" yaml:"validation_rules"`
	XPReward        int      `json:"xp_reward" yaml:"xp_reward"`
}

// Trigger describes when an achievement is earned.
type Trigger struct {
	Type       string `json:"type" yaml:"type"`
	Operator   string `json:"operator,omitempty" yaml:"operator,omitempty"`
	Value      int    `json:"value,omitempty" yaml:"value,omitempty"`
	Track      string `json:"track,omitempty" yaml:"track,omitempty"`
	Difficulty string `json:"difficulty,omitempty" yaml:"difficulty,omitempty"`
}

// Achievement is one achievement definition.
type Achievement struct {
	ID          string  `json:"id" yaml:"id"`
	Title       string  `json:"title" yaml:"title"`
	Description string  `json:"description" yaml:"description"`
	Trigger     Trigger `json:"trigger" yaml:"trigger"`
	XP          int     `json:"xp" yaml:"xp"`
	// Earned / LockedReason are filled in by the API, not the YAML.
	Earned       bool   `json:"earned,omitempty" yaml:"-"`
	LockedReason string `json:"locked_reason,omitempty" yaml:"-"`
}

// TrackSummary is a track list entry.
type TrackSummary struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	Difficulty     string  `json:"difficulty"`
	EstimatedHours float64 `json:"estimated_hours"`
	XPReward       int     `json:"xp_reward"`
	LessonCount    int     `json:"lesson_count"`
	ModuleCount    int     `json:"module_count"`
	// Progress is filled in by the API when the caller is authed.
	ProgressPercent *int `json:"progress_percent,omitempty"`
	Enrolled        bool `json:"enrolled,omitempty"`
}

// Bundle is everything Load returns.
type Bundle struct {
	Tracks       []*Track
	Scenarios    []*Scenario
	Achievements []*Achievement
}

// Load reads the curriculum directory: one track YAML per
// <track>/<track>.yaml subdirectory, break-and-fix/*.yaml scenarios
// and achievements.yaml. The directory must exist; a missing or
// unreadable directory is an error naming the path.
func Load(dir string) (*Bundle, error) {
	if dir == "" {
		return nil, fmt.Errorf("curriculum directory is empty (set CURRICULUM_DIR)")
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("curriculum directory %q not found: %w (set CURRICULUM_DIR to the curriculum/ directory)", dir, err)
	}
	b := &Bundle{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read curriculum directory %q: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "break-and-fix" {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read track directory %q: %w", e.Name(), err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			t, err := loadTrack(filepath.Join(dir, e.Name(), f.Name()))
			if err != nil {
				return nil, err
			}
			b.Tracks = append(b.Tracks, t)
		}
	}
	sort.Slice(b.Tracks, func(i, j int) bool { return b.Tracks[i].ID < b.Tracks[j].ID })
	baf := filepath.Join(dir, "break-and-fix")
	if files, err := os.ReadDir(baf); err == nil {
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			var s Scenario
			if err := loadYAML(filepath.Join(baf, f.Name()), &s); err != nil {
				return nil, fmt.Errorf("break-and-fix %s: %w", f.Name(), err)
			}
			if s.ID == "" {
				return nil, fmt.Errorf("break-and-fix %s: missing id", f.Name())
			}
			sc := s
			b.Scenarios = append(b.Scenarios, &sc)
		}
		sort.Slice(b.Scenarios, func(i, j int) bool { return b.Scenarios[i].ID < b.Scenarios[j].ID })
	}
	achPath := filepath.Join(dir, "achievements.yaml")
	var achDoc struct {
		Achievements []*Achievement `yaml:"achievements"`
	}
	if err := loadYAML(achPath, &achDoc); err != nil {
		return nil, fmt.Errorf("achievements.yaml: %w", err)
	}
	b.Achievements = achDoc.Achievements
	return b, nil
}

func loadYAML(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func loadTrack(path string) (*Track, error) {
	var ty trackYAML
	if err := loadYAML(path, &ty); err != nil {
		return nil, err
	}
	if ty.ID == "" {
		return nil, fmt.Errorf("%s: track is missing id", path)
	}
	switch ty.Difficulty {
	case "beginner", "intermediate", "advanced":
	default:
		return nil, fmt.Errorf("%s: track %q has invalid difficulty %q", path, ty.ID, ty.Difficulty)
	}
	t := &Track{
		ID: ty.ID, Title: ty.Title, Description: ty.Description,
		Difficulty: ty.Difficulty, EstimatedHours: ty.EstimatedHours,
		LearningObjectives: ty.LearningObjectives, Modules: ty.Modules,
		Assessment: ty.Assessment, XPReward: ty.XPReward, Prerequisites: ty.Prerequisites,
	}
	t.GuidedLab = LabDefinition{
		ID: ty.GuidedLab.LabDefinitionID, CourseID: ty.ID, Title: ty.GuidedLab.Title,
		Tasks: ty.GuidedLab.Tasks, ValidationRules: ty.GuidedLab.ValidationRules, Kind: "guided",
	}
	t.Challenge = LabDefinition{
		ID: "challenge-" + ty.ID, CourseID: ty.ID, Title: ty.Challenge.Title,
		Description: ty.Challenge.Task, Tasks: []string{ty.Challenge.Task},
		ValidationRules: ty.Challenge.ValidationRules, Kind: "challenge",
	}
	return t, nil
}

// ResolveDir finds the curriculum directory: an explicit dir wins;
// otherwise candidates are tried in order (relative to the working
// directory and to the executable, covering `go run ./cmd/api`
// from backend/ and a built binary next to the repo). The first
// candidate containing achievements.yaml wins; otherwise an error
// lists every path tried.
func ResolveDir(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "achievements.yaml")); err != nil {
			return "", fmt.Errorf("CURRICULUM_DIR %q does not contain achievements.yaml: %w", explicit, err)
		}
		return explicit, nil
	}
	candidates := []string{"../curriculum", "curriculum"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "curriculum"),
			filepath.Join(filepath.Dir(exe), "..", "..", "curriculum"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "achievements.yaml")); err == nil {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs, nil
			}
			return c, nil
		}
	}
	return "", fmt.Errorf("curriculum directory not found; tried %s (set CURRICULUM_DIR to the curriculum/ directory)", strings.Join(candidates, ", "))
}

// --- Fault mapping -------------------------------------------------------

// FaultKind maps a YAML fault kind to the Rust engine FaultKind enum
// value (see proto/cloudforge/v1: LATENCY=1, LOSS=2, DNS=4,
// ROUTE_BLACKHOLE=5). Magnitude is the engine's magnitude for that
// kind: latency_ms for latency, loss_percent for loss, 100 for a
// dns failure affecting all names, 1 for a route blackhole.
func (f Fault) EngineKind() (int32, error) {
	switch f.Kind {
	case "latency":
		return 1, nil
	case "loss":
		return 2, nil
	case "dns":
		return 4, nil
	case "route-blackhole":
		return 5, nil
	default:
		return 0, fmt.Errorf("unknown fault kind %q", f.Kind)
	}
}

// EngineMagnitude extracts the engine magnitude from the fault
// parameters (0 when absent, which the engine treats as "no effect"
// for loss/latency — callers should treat a 0 magnitude as a
// content problem, not silently inject a no-op fault).
func (f Fault) EngineMagnitude() float64 {
	switch f.Kind {
	case "latency":
		if v, ok := paramFloat(f.Parameters, "latency_ms"); ok {
			return v
		}
	case "loss":
		if v, ok := paramFloat(f.Parameters, "loss_percent"); ok {
			return v
		}
	case "dns":
		return 100
	case "route-blackhole":
		return 1
	}
	return 0
}

func paramFloat(p map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			switch n := v.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case int64:
				return float64(n), true
			}
		}
	}
	return 0, false
}

// --- Stats / streak ------------------------------------------------------

// Stats is everything the achievement evaluator and the progress
// API need about one user. ActivityDates are UTC calendar days with
// any learning activity (lesson/lab/assessment).
type Stats struct {
	LessonsCompleted      int
	LabsCompleted         int
	ChallengesCompleted   int
	BreakFixSolved        int
	CompletedTracks       []string
	CompletedByDifficulty map[string]int
	BestAssessmentScore   int
	TotalXP               int
	StreakDays            int
	Earned                map[string]bool
}

// HasCompletedTrack reports whether the user completed a track.
func (s *Stats) HasCompletedTrack(id string) bool {
	for _, t := range s.CompletedTracks {
		if t == id {
			return true
		}
	}
	return false
}

// ComputeStreak counts consecutive UTC calendar days with activity,
// ending today (or yesterday when there is no activity today — a
// streak is not broken by the current day not being over yet).
// Dates are truncated to UTC calendar days; future dates are ignored.
func ComputeStreak(dates []time.Time, now time.Time) int {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	daySet := map[time.Time]bool{}
	for _, d := range dates {
		day := time.Date(d.UTC().Year(), d.UTC().Month(), d.UTC().Day(), 0, 0, 0, 0, time.UTC)
		if !day.After(today) {
			daySet[day] = true
		}
	}
	day := today
	if !daySet[day] {
		day = day.AddDate(0, 0, -1)
	}
	streak := 0
	for daySet[day] {
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}
