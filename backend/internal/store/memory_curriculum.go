package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

// MemoryCurriculumStore implements curriculum.Store in memory. It
// shares the MemoryCourseStore for courses/modules/lessons (so a
// seeded track is visible through CourseStore and lesson completion
// works), and keeps assessments, lab definitions, achievements,
// scenarios, submissions, lab completions and break-fix attempts in
// its own maps. It is the store unit tests and the no-Postgres
// fallback run against.
type MemoryCurriculumStore struct {
	mu           sync.Mutex
	courses      *MemoryCourseStore
	assessments  map[string][]curriculum.Question
	labDefs      map[string]*curriculum.LabDefinition
	achievements map[string]*curriculum.Achievement
	scenarios    map[string]*curriculum.Scenario
	submissions  []assessmentSubmission
	labCompleted map[string]labCompletion               // userID|labID -> completion
	breakFix     map[string]*curriculum.BreakFixAttempt // labID -> attempt
	activity     map[string][]time.Time                 // userID -> activity dates
}

type assessmentSubmission struct {
	userID    string
	courseID  string
	score     int
	total     int
	correct   int
	passed    bool
	xpAwarded int
	at        time.Time
}

type labCompletion struct {
	userID       string
	labID        string
	definitionID string
	kind         string
	at           time.Time
}

// NewMemoryCurriculumStore builds a memory curriculum store sharing
// the given course store. A nil course store gets a fresh one.
func NewMemoryCurriculumStore(courses *MemoryCourseStore) *MemoryCurriculumStore {
	if courses == nil {
		courses = NewMemoryCourseStore()
	}
	return &MemoryCurriculumStore{
		courses:      courses,
		assessments:  make(map[string][]curriculum.Question),
		labDefs:      make(map[string]*curriculum.LabDefinition),
		achievements: make(map[string]*curriculum.Achievement),
		scenarios:    make(map[string]*curriculum.Scenario),
		labCompleted: make(map[string]labCompletion),
		breakFix:     make(map[string]*curriculum.BreakFixAttempt),
		activity:     make(map[string][]time.Time),
	}
}

func (m *MemoryCurriculumStore) recordActivity(userID string, at time.Time) {
	m.activity[userID] = append(m.activity[userID], at)
}

func (m *MemoryCurriculumStore) UpsertTrack(ctx context.Context, t *curriculum.Track) error {
	// Upsert into the shared course store first (it locks itself),
	// so CourseStore sees the track and lesson completion works.
	c := &Course{ID: t.ID, Title: t.Title, Track: t.Title, Description: t.Description,
		Difficulty: t.Difficulty, EstimatedHours: t.EstimatedHours, XPReward: t.XPReward}
	for i, mod := range t.Modules {
		mm := Module{Position: i + 1, Title: mod.Title}
		for j, les := range mod.Lessons {
			mm.Lessons = append(mm.Lessons, Lesson{Position: j + 1, Title: les.Title, Body: les.Theory,
				Slug: les.Slug, Theory: les.Theory, KeyPoints: append([]string(nil), les.KeyPoints...), Diagram: les.Diagram})
		}
		c.Modules = append(c.Modules, mm)
	}
	m.courses.upsertCourse(c)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assessments[t.ID] = append([]curriculum.Question(nil), t.Assessment...)
	guided := t.GuidedLab
	m.labDefs[guided.ID] = &guided
	challenge := t.Challenge
	m.labDefs[challenge.ID] = &challenge
	return nil
}

func (m *MemoryCurriculumStore) UpsertAchievement(ctx context.Context, a *curriculum.Achievement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.achievements[a.ID] = &cp
	return nil
}

func (m *MemoryCurriculumStore) UpsertScenario(ctx context.Context, s *curriculum.Scenario) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.scenarios[s.ID] = &cp
	def := &curriculum.LabDefinition{ID: "break-fix-" + s.ID, Title: s.Title, Description: s.Symptom,
		ValidationRules: append([]curriculum.Rule(nil), s.ValidationRules...), Kind: "break-fix", XPReward: s.XPReward}
	m.labDefs[def.ID] = def
	return nil
}

func (m *MemoryCurriculumStore) ListTracks(ctx context.Context) ([]*curriculum.TrackSummary, error) {
	list, err := m.courses.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []*curriculum.TrackSummary
	for _, c := range list {
		out = append(out, &curriculum.TrackSummary{ID: c.ID, Title: c.Title, Description: c.Description,
			Difficulty: c.Difficulty, EstimatedHours: c.EstimatedHours, XPReward: c.XPReward,
			LessonCount: c.LessonCount(), ModuleCount: len(c.Modules)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryCurriculumStore) GetTrack(ctx context.Context, id string) (*curriculum.Track, error) {
	c, err := m.courses.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := &curriculum.Track{ID: c.ID, Title: c.Title, Description: c.Description,
		Difficulty: c.Difficulty, EstimatedHours: c.EstimatedHours, XPReward: c.XPReward,
		Assessment: append([]curriculum.Question(nil), m.assessments[id]...)}
	for _, mod := range c.Modules {
		mm := curriculum.Module{ID: mod.ID, Title: mod.Title}
		for _, l := range mod.Lessons {
			mm.Lessons = append(mm.Lessons, curriculum.Lesson{Slug: l.Slug, Title: l.Title,
				Theory: l.Theory, KeyPoints: append([]string(nil), l.KeyPoints...), Diagram: l.Diagram, DBID: l.ID})
		}
		t.Modules = append(t.Modules, mm)
	}
	for _, def := range m.labDefs {
		if def.CourseID != id {
			continue
		}
		if def.Kind == "guided" {
			t.GuidedLab = *def
		} else if def.Kind == "challenge" {
			t.Challenge = *def
		}
	}
	return t, nil
}

func (m *MemoryCurriculumStore) GetAssessment(ctx context.Context, courseID string) ([]curriculum.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	qs, ok := m.assessments[courseID]
	if !ok || len(qs) == 0 {
		return nil, ErrNotFound
	}
	return append([]curriculum.Question(nil), qs...), nil
}

func (m *MemoryCurriculumStore) FindLesson(ctx context.Context, lessonID string) (string, string, error) {
	list, err := m.courses.List(ctx)
	if err != nil {
		return "", "", err
	}
	for _, c := range list {
		if l, ok := c.FindLesson(lessonID); ok {
			return c.ID, l.ID, nil
		}
	}
	return "", "", ErrNotFound
}

func (m *MemoryCurriculumStore) GetLabDefinition(ctx context.Context, id string) (*curriculum.LabDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.labDefs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *MemoryCurriculumStore) ListScenarios(ctx context.Context) ([]*curriculum.Scenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*curriculum.Scenario
	for _, s := range m.scenarios {
		cp := *s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryCurriculumStore) GetScenario(ctx context.Context, id string) (*curriculum.Scenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scenarios[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *MemoryCurriculumStore) ListAchievements(ctx context.Context) ([]*curriculum.Achievement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*curriculum.Achievement
	for _, a := range m.achievements {
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryCurriculumStore) UserStats(ctx context.Context, userID string) (*curriculum.Stats, error) {
	lessons, completed, byDiff, xp, earned := m.courses.courseStats(userID)
	s := &curriculum.Stats{Earned: earned, CompletedByDifficulty: byDiff,
		LessonsCompleted: lessons, CompletedTracks: completed, TotalXP: xp}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lc := range m.labCompleted {
		if lc.userID != userID {
			continue
		}
		if lc.kind == "challenge" {
			s.ChallengesCompleted++
		} else {
			s.LabsCompleted++
		}
	}
	for _, a := range m.breakFix {
		if a.UserID == userID && a.Solved {
			s.BreakFixSolved++
		}
	}
	for _, sub := range m.submissions {
		if sub.userID == userID && sub.score > s.BestAssessmentScore {
			s.BestAssessmentScore = sub.score
		}
	}
	// Activity dates: lesson completions are not timestamped in the
	// memory course store, so use recorded activity (lab
	// completions, submissions) plus "today" when the user has any
	// completed lesson — the memory store is the test/fallback
	// path, and a streak there is best-effort.
	dates := append([]time.Time(nil), m.activity[userID]...)
	if s.LessonsCompleted > 0 {
		dates = append(dates, time.Now().UTC())
	}
	s.StreakDays = curriculum.ComputeStreak(dates, time.Now())
	return s, nil
}

func (m *MemoryCurriculumStore) SubmitAssessment(ctx context.Context, userID, courseID string, answers map[int]int) (*curriculum.AssessmentResult, error) {
	qs, err := m.GetAssessment(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if !m.courses.isEnrolled(userID, courseID) {
		return nil, ErrNotEnrolled
	}
	res := &curriculum.AssessmentResult{CourseID: courseID, Total: len(qs)}
	for i, q := range qs {
		given, answered := answers[i]
		correct := answered && given == q.CorrectAnswer
		if correct {
			res.Correct++
		}
		res.PerQuestion = append(res.PerQuestion, curriculum.QuestionResult{
			Index: i, Correct: correct, CorrectAnswer: q.CorrectAnswer, Explanation: q.Explanation,
		})
	}
	res.Score = res.Correct * 100 / res.Total
	res.Passed = res.Score >= curriculum.AssessmentPassThreshold
	m.mu.Lock()
	if res.Passed {
		hadPass := false
		for _, sub := range m.submissions {
			if sub.userID == userID && sub.courseID == courseID && sub.passed && sub.xpAwarded > 0 {
				hadPass = true
				break
			}
		}
		if !hadPass {
			res.XPAwarded = curriculum.AssessmentPassXP
		}
	}
	m.submissions = append(m.submissions, assessmentSubmission{userID: userID, courseID: courseID,
		score: res.Score, total: res.Total, correct: res.Correct, passed: res.Passed, xpAwarded: res.XPAwarded, at: time.Now().UTC()})
	m.recordActivity(userID, time.Now().UTC())
	m.mu.Unlock()
	if res.XPAwarded > 0 {
		m.courses.addXPOnceSafe(userID, curriculum.AssessmentPassXP, "assessment:"+courseID)
	}
	return res, nil
}

func (m *MemoryCurriculumStore) AwardAchievements(ctx context.Context, userID string, ids []string) ([]*curriculum.Achievement, error) {
	m.mu.Lock()
	defs := make([]*curriculum.Achievement, 0, len(ids))
	for _, id := range ids {
		if a, ok := m.achievements[id]; ok {
			cp := *a
			defs = append(defs, &cp)
		}
	}
	m.mu.Unlock()
	var awarded []*curriculum.Achievement
	for _, a := range defs {
		if !m.courses.markAchievement(userID, a.ID) {
			continue
		}
		if a.XP > 0 {
			m.courses.addXPOnceSafe(userID, a.XP, "achievement:"+a.ID)
		}
		cp := *a
		cp.Earned = true
		awarded = append(awarded, &cp)
	}
	return awarded, nil
}

func (m *MemoryCurriculumStore) RecordLabCompletion(ctx context.Context, userID, labID, definitionID, kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind != "lab" && kind != "challenge" {
		kind = "lab"
	}
	key := userID + "|" + labID
	if _, ok := m.labCompleted[key]; ok {
		return nil
	}
	now := time.Now().UTC()
	m.labCompleted[key] = labCompletion{userID: userID, labID: labID, definitionID: definitionID, kind: kind, at: now}
	m.recordActivity(userID, now)
	return nil
}

func (m *MemoryCurriculumStore) StartBreakFix(ctx context.Context, userID, labID, scenarioID, faultStatus string) (*curriculum.BreakFixAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sc, ok := m.scenarios[scenarioID]
	if !ok {
		return nil, ErrNotFound
	}
	if existing, ok := m.breakFix[labID]; ok {
		cp := *existing
		return &cp, nil
	}
	if faultStatus != "injected" && faultStatus != "fault_pending" {
		faultStatus = "fault_pending"
	}
	a := &curriculum.BreakFixAttempt{LabID: labID, ScenarioID: scenarioID, UserID: userID,
		FaultStatus: faultStatus, XPReward: sc.XPReward}
	m.breakFix[labID] = a
	cp := *a
	return &cp, nil
}

func (m *MemoryCurriculumStore) GetBreakFixAttemptByLab(ctx context.Context, labID string) (*curriculum.BreakFixAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.breakFix[labID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *MemoryCurriculumStore) MarkBreakFixSolved(ctx context.Context, userID, labID string) (*curriculum.BreakFixAttempt, int, error) {
	m.mu.Lock()
	a, ok := m.breakFix[labID]
	if !ok || a.UserID != userID {
		m.mu.Unlock()
		return nil, 0, ErrNotFound
	}
	if a.Solved {
		cp := *a
		m.mu.Unlock()
		return &cp, 0, nil
	}
	a.Solved = true
	a.XPAwarded = a.XPReward
	m.recordActivity(userID, time.Now().UTC())
	cp := *a
	xp := a.XPReward
	scenario := a.ScenarioID
	m.mu.Unlock()
	m.courses.addXPOnceSafe(userID, xp, "break_fix:"+scenario)
	return &cp, xp, nil
}

var _ curriculum.Store = (*MemoryCurriculumStore)(nil)
