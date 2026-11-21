package curriculum

import (
	"context"
	"fmt"
)

// Store is the persistence surface the seeder and the curriculum
// API need. It is implemented by the store package (Postgres and
// in-memory); this package never imports the store package, so the
// dependency points one way (store -> curriculum).
type Store interface {
	UpsertTrack(ctx context.Context, t *Track) error
	UpsertAchievement(ctx context.Context, a *Achievement) error
	UpsertScenario(ctx context.Context, s *Scenario) error
	ListTracks(ctx context.Context) ([]*TrackSummary, error)
	GetTrack(ctx context.Context, id string) (*Track, error)
	GetAssessment(ctx context.Context, courseID string) ([]Question, error)
	FindLesson(ctx context.Context, lessonID string) (courseID, lessonUUID string, err error)
	GetLabDefinition(ctx context.Context, id string) (*LabDefinition, error)
	ListScenarios(ctx context.Context) ([]*Scenario, error)
	GetScenario(ctx context.Context, id string) (*Scenario, error)
	ListAchievements(ctx context.Context) ([]*Achievement, error)
	UserStats(ctx context.Context, userID string) (*Stats, error)
	SubmitAssessment(ctx context.Context, userID, courseID string, answers map[int]int) (*AssessmentResult, error)
	AwardAchievements(ctx context.Context, userID string, ids []string) ([]*Achievement, error)
	RecordLabCompletion(ctx context.Context, userID, labID, definitionID, kind string) error
	StartBreakFix(ctx context.Context, userID, labID, scenarioID, faultStatus string) (*BreakFixAttempt, error)
	GetBreakFixAttemptByLab(ctx context.Context, labID string) (*BreakFixAttempt, error)
	MarkBreakFixSolved(ctx context.Context, userID, labID string) (*BreakFixAttempt, int, error)
}

// AssessmentResult is a graded submission.
type AssessmentResult struct {
	CourseID    string           `json:"course_id"`
	Score       int              `json:"score"` // percent
	Correct     int              `json:"correct"`
	Total       int              `json:"total"`
	Passed      bool             `json:"passed"` // score >= 80
	XPAwarded   int              `json:"xp_awarded"`
	PerQuestion []QuestionResult `json:"per_question"`
}

// QuestionResult is one graded question.
type QuestionResult struct {
	Index         int    `json:"index"`
	Correct       bool   `json:"correct"`
	CorrectAnswer int    `json:"correct_answer"`
	Explanation   string `json:"explanation"`
}

// BreakFixAttempt is one user's attempt at a scenario in one lab.
type BreakFixAttempt struct {
	LabID       string `json:"lab_id"`
	ScenarioID  string `json:"scenario_id"`
	UserID      string `json:"user_id"`
	FaultStatus string `json:"fault_status"` // "injected" | "fault_pending"
	XPReward    int    `json:"xp_reward"`
	XPAwarded   int    `json:"xp_awarded"`
	Solved      bool   `json:"solved"`
}

// AssessmentPassThreshold is the minimum assessment score (percent)
// that earns XP; AssessmentPassXP is the XP awarded for the first
// passing submission per user+track.
const (
	AssessmentPassThreshold = 80
	AssessmentPassXP        = 20
)

type SeedReport struct {
	Tracks         int `json:"tracks"`
	Modules        int `json:"modules"`
	Lessons        int `json:"lessons"`
	Questions      int `json:"questions"`
	LabDefinitions int `json:"lab_definitions"`
	Scenarios      int `json:"scenarios"`
	Achievements   int `json:"achievements"`
}

// Seed upserts a loaded Bundle into the store. It is idempotent:
// upserts key on stable ids (track id, module position, lesson
// slug, question index, lab-definition id, achievement id,
// scenario id), so re-running changes nothing.
func Seed(ctx context.Context, st Store, b *Bundle) (*SeedReport, error) {
	rep := &SeedReport{}
	for _, t := range b.Tracks {
		if err := st.UpsertTrack(ctx, t); err != nil {
			return rep, fmt.Errorf("seed track %q: %w", t.ID, err)
		}
		rep.Tracks++
		rep.Modules += len(t.Modules)
		rep.Lessons += t.LessonCount()
		rep.Questions += len(t.Assessment)
		rep.LabDefinitions += 2 // guided lab + challenge
	}
	for _, s := range b.Scenarios {
		if err := st.UpsertScenario(ctx, s); err != nil {
			return rep, fmt.Errorf("seed scenario %q: %w", s.ID, err)
		}
		rep.Scenarios++
		rep.LabDefinitions++
	}
	for _, a := range b.Achievements {
		if err := st.UpsertAchievement(ctx, a); err != nil {
			return rep, fmt.Errorf("seed achievement %q: %w", a.ID, err)
		}
		rep.Achievements++
	}
	return rep, nil
}

// SeedDir loads a curriculum directory and seeds it in one call.
func SeedDir(ctx context.Context, st Store, dir string) (*SeedReport, error) {
	b, err := Load(dir)
	if err != nil {
		return nil, err
	}
	return Seed(ctx, st, b)
}
