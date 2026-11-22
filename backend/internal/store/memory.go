// In-memory store implementations.
//
// These wrap the M1 stores (auth.Store, labs.Store, the seeded course
// catalogue) and satisfy the store interfaces. They are the fallback when
// PostgreSQL is unavailable and the implementation unit tests run against.
// They are NOT the production path when a database is configured.

package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/courses"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
)

// --- Memory lab store -----------------------------------------------------

type MemoryLabStore struct {
	inner *labs.Store
}

// NewMemoryLabStore wraps a labs.Store.
func NewMemoryLabStore(s *labs.Store) *MemoryLabStore { return &MemoryLabStore{inner: s} }

func (m *MemoryLabStore) Create(ctx context.Context, ownerID, definitionID, name, key string) (*labs.Lab, bool, error) {
	return m.inner.Create(ownerID, definitionID, name, key)
}
func (m *MemoryLabStore) Get(ctx context.Context, id string) (*labs.Lab, error) {
	return m.inner.Get(id)
}
func (m *MemoryLabStore) ListByOwner(ctx context.Context, ownerID string) ([]*labs.Lab, error) {
	return m.inner.ListByOwner(ownerID), nil
}
func (m *MemoryLabStore) Stop(ctx context.Context, id string) (*labs.Lab, error) {
	return m.inner.Stop(id)
}
func (m *MemoryLabStore) Destroy(ctx context.Context, id string) (*labs.Lab, error) {
	return m.inner.Destroy(id)
}

// Start re-provisions a STOPPED lab through the in-memory store's
// Retry path (STOPPED -> PROVISIONING -> READY/FAILED via the engine
// reachability check, exactly like a failed lab's retry).
func (m *MemoryLabStore) Start(ctx context.Context, id string) (*labs.Lab, error) {
	return m.inner.Retry(id)
}

// Transition validates and persists the new state via the in-memory
// store (its async goroutines may still race it; tests use short
// ProvisionDelay values to avoid that).
func (m *MemoryLabStore) Transition(ctx context.Context, id, to, errMsg string) (*labs.Lab, error) {
	return m.inner.Transition(id, to, errMsg)
}
func (m *MemoryLabStore) ExpireDue(ctx context.Context) ([]*labs.Lab, error) {
	return m.inner.ExpireDue(), nil
}
func (m *MemoryLabStore) CleanupResources(ctx context.Context, labID string) error { return nil }
func (m *MemoryLabStore) ActiveCount(ctx context.Context, ownerID string) (int, error) {
	return m.inner.ActiveCount(ownerID), nil
}

func (m *MemoryLabStore) SetMaxLabs(n int) { m.inner.SetMaxLabs(n) }

func (m *MemoryLabStore) ListAll(ctx context.Context) ([]*labs.Lab, error) {
	return m.inner.ListAll(), nil
}

// --- Memory course store --------------------------------------------------

type MemoryCourseStore struct {
	mu           sync.Mutex
	courses      map[string]*Course
	order        []string
	enrolled     map[string]map[string]bool // userID -> courseID -> true
	completed    map[string]map[string]bool // userID -> lessonID -> true
	xp           map[string]int
	achievements map[string]map[string]bool
	// xpRefs guards once-only XP awards by (userID, ref) — e.g.
	// "track_completion:<courseID>" — so a re-run never double-awards.
	xpRefs map[string]map[string]bool
}

// NewMemoryCourseStore seeds from the M1 catalogue (one "Core" module
// per course, lessons in catalogue order).
func NewMemoryCourseStore() *MemoryCourseStore {
	s := &MemoryCourseStore{
		courses:      make(map[string]*Course),
		enrolled:     make(map[string]map[string]bool),
		completed:    make(map[string]map[string]bool),
		xp:           make(map[string]int),
		achievements: make(map[string]map[string]bool),
		xpRefs:       make(map[string]map[string]bool),
	}
	for _, c := range courses.List() {
		lessons := make([]Lesson, 0, len(c.Lessons))
		for i, title := range c.Lessons {
			lessons = append(lessons, Lesson{ID: ids.New(), ModuleID: "", Position: i + 1, Title: title})
		}
		moduleID := ids.New()
		for i := range lessons {
			lessons[i].ModuleID = moduleID
		}
		cp := &Course{ID: c.ID, Title: c.Title, Track: c.Track, Description: c.Description,
			Modules: []Module{{ID: moduleID, CourseID: c.ID, Position: 1, Title: "Core", Lessons: lessons}}}
		s.courses[c.ID] = cp
		s.order = append(s.order, c.ID)
	}
	return s
}

func (s *MemoryCourseStore) copyCourse(c *Course) *Course {
	cp := *c
	cp.Modules = append([]Module(nil), c.Modules...)
	for i := range cp.Modules {
		cp.Modules[i].Lessons = append([]Lesson(nil), c.Modules[i].Lessons...)
	}
	return &cp
}

func (s *MemoryCourseStore) List(ctx context.Context) ([]*Course, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Course, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.copyCourse(s.courses[id]))
	}
	return out, nil
}

func (s *MemoryCourseStore) Get(ctx context.Context, id string) (*Course, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.courses[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s.copyCourse(c), nil
}

func validSlug(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func (s *MemoryCourseStore) Create(ctx context.Context, c *Course) (*Course, error) {
	if !validSlug(c.ID) || strings.TrimSpace(c.Title) == "" {
		return nil, fmt.Errorf("%w: course id must be a lowercase slug and title is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.courses[c.ID]; exists {
		return nil, fmt.Errorf("%w: course %q already exists", ErrInvalidInput, c.ID)
	}
	if len(c.Modules) == 0 {
		c.Modules = []Module{{ID: ids.New(), CourseID: c.ID, Position: 1, Title: "Core"}}
	}
	for i := range c.Modules {
		if c.Modules[i].ID == "" {
			c.Modules[i].ID = ids.New()
		}
		c.Modules[i].CourseID = c.ID
		for j := range c.Modules[i].Lessons {
			if c.Modules[i].Lessons[j].ID == "" {
				c.Modules[i].Lessons[j].ID = ids.New()
			}
			c.Modules[i].Lessons[j].ModuleID = c.Modules[i].ID
		}
	}
	cp := s.copyCourse(c)
	s.courses[c.ID] = cp
	s.order = append(s.order, c.ID)
	return s.copyCourse(cp), nil
}

func (s *MemoryCourseStore) Update(ctx context.Context, id, title, track, description string) (*Course, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.courses[id]
	if !ok {
		return nil, ErrNotFound
	}
	c.Title, c.Track, c.Description = title, track, description
	return s.copyCourse(c), nil
}

func (s *MemoryCourseStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.courses[id]; !ok {
		return ErrNotFound
	}
	delete(s.courses, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (s *MemoryCourseStore) Enroll(ctx context.Context, userID, courseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.courses[courseID]; !ok {
		return ErrNotFound
	}
	if s.enrolled[userID] == nil {
		s.enrolled[userID] = make(map[string]bool)
	}
	s.enrolled[userID][courseID] = true
	return nil
}

func (s *MemoryCourseStore) progressLocked(userID string, c *Course) *Progress {
	done := []string{}
	if s.completed[userID] != nil {
		for _, m := range c.Modules {
			for _, l := range m.Lessons {
				if s.completed[userID][l.ID] {
					done = append(done, l.ID)
				}
			}
		}
	}
	sort.Strings(done)
	return &Progress{
		CourseID:           c.ID,
		Enrolled:           s.enrolled[userID] != nil && s.enrolled[userID][c.ID],
		TotalLessons:       c.LessonCount(),
		CompletedLessons:   len(done),
		CompletedLessonIDs: done,
		XP:                 s.xp[userID],
	}
}

func (s *MemoryCourseStore) GetProgress(ctx context.Context, userID, courseID string) (*Progress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.courses[courseID]
	if !ok {
		return nil, ErrNotFound
	}
	return s.progressLocked(userID, c), nil
}

// LessonXP is the XP awarded for completing one lesson.
const LessonXP = 10

func (s *MemoryCourseStore) CompleteLesson(ctx context.Context, userID, courseID, lessonID string) (*Progress, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.courses[courseID]
	if !ok {
		return nil, 0, ErrNotFound
	}
	if _, ok := c.FindLesson(lessonID); !ok {
		return nil, 0, fmt.Errorf("%w: no lesson %q in course %q", ErrNotFound, lessonID, courseID)
	}
	if s.enrolled[userID] == nil || !s.enrolled[userID][courseID] {
		return nil, 0, ErrNotEnrolled
	}
	if s.completed[userID] == nil {
		s.completed[userID] = make(map[string]bool)
	}
	if s.completed[userID][lessonID] {
		return s.progressLocked(userID, c), 0, nil // idempotent: no double XP
	}
	s.completed[userID][lessonID] = true
	awarded := LessonXP
	if s.achievements[userID] == nil {
		s.achievements[userID] = make(map[string]bool)
	}
	if !s.achievements[userID]["first-lesson"] {
		s.achievements[userID]["first-lesson"] = true
		awarded += 10 // first-lesson achievement bonus
	}
	// Track-completion bonus (M8, mirrors the Postgres store): the
	// track's xp_reward split across lessons — 10 XP per lesson
	// plus the remainder on the final lesson. Guarded by xpRefs.
	if c.XPReward > 0 && s.progressLocked(userID, c).CompletedLessons == c.LessonCount() {
		if bonus := c.XPReward - LessonXP*c.LessonCount(); bonus > 0 {
			ref := "track_completion:" + courseID
			if s.xpRefs[userID] == nil {
				s.xpRefs[userID] = make(map[string]bool)
			}
			if !s.xpRefs[userID][ref] {
				s.xpRefs[userID][ref] = true
				awarded += bonus
			}
		}
	}
	s.xp[userID] += awarded
	return s.progressLocked(userID, c), awarded, nil
}

// addXPOnce adds XP for a once-only reason (assessment pass,
// achievement, break-and-fix solve). It returns false when the
// reason was already awarded to this user. Caller must hold s.mu.
func (s *MemoryCourseStore) addXPOnce(userID string, amount int, ref string) bool {
	if s.xpRefs[userID] == nil {
		s.xpRefs[userID] = make(map[string]bool)
	}
	if s.xpRefs[userID][ref] {
		return false
	}
	s.xpRefs[userID][ref] = true
	s.xp[userID] += amount
	return true
}

// addXPOnceSafe is the locking wrapper of addXPOnce for callers
// outside this store (e.g. the memory curriculum store).
func (s *MemoryCourseStore) addXPOnceSafe(userID string, amount int, ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addXPOnce(userID, amount, ref)
}

// isEnrolled reports whether userID is enrolled in courseID.
func (s *MemoryCourseStore) isEnrolled(userID, courseID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enrolled[userID] != nil && s.enrolled[userID][courseID]
}

// markAchievement records an achievement id for a user; it returns
// false when the user already had it.
func (s *MemoryCourseStore) markAchievement(userID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.achievements[userID] == nil {
		s.achievements[userID] = make(map[string]bool)
	}
	if s.achievements[userID][id] {
		return false
	}
	s.achievements[userID][id] = true
	return true
}

// upsertCourse inserts or replaces a course, preserving existing
// lesson/module UUIDs so in-flight progress survives a re-seed.
func (s *MemoryCourseStore) upsertCourse(c *Course) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.courses[c.ID]; ok {
		for i := range c.Modules {
			if i < len(existing.Modules) {
				c.Modules[i].ID = existing.Modules[i].ID
				for j := range c.Modules[i].Lessons {
					if j < len(existing.Modules[i].Lessons) {
						c.Modules[i].Lessons[j].ID = existing.Modules[i].Lessons[j].ID
						c.Modules[i].Lessons[j].ModuleID = existing.Modules[i].ID
					}
				}
			}
		}
		missing := false
		for _, id := range s.order {
			if id == c.ID {
				missing = true
				break
			}
		}
		if !missing {
			s.order = append(s.order, c.ID)
		}
		s.courses[c.ID] = s.copyCourse(c)
		return
	}
	for i := range c.Modules {
		if c.Modules[i].ID == "" {
			c.Modules[i].ID = ids.New()
		}
		c.Modules[i].CourseID = c.ID
		for j := range c.Modules[i].Lessons {
			if c.Modules[i].Lessons[j].ID == "" {
				c.Modules[i].Lessons[j].ID = ids.New()
			}
			c.Modules[i].Lessons[j].ModuleID = c.Modules[i].ID
		}
	}
	s.courses[c.ID] = s.copyCourse(c)
	s.order = append(s.order, c.ID)
}

// courseStats returns the per-user aggregates the curriculum
// evaluator needs: lessons completed, completed tracks, completed
// tracks by difficulty, total XP, earned achievement ids.
func (s *MemoryCourseStore) courseStats(userID string) (lessons int, completed []string, byDiff map[string]int, xp int, earned map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byDiff = map[string]int{}
	earned = map[string]bool{}
	for _, id := range s.order {
		c := s.courses[id]
		done := 0
		for _, mod := range c.Modules {
			for _, l := range mod.Lessons {
				if s.completed[userID] != nil && s.completed[userID][l.ID] {
					done++
				}
			}
		}
		lessons += done
		if done == c.LessonCount() && c.LessonCount() > 0 && s.enrolled[userID] != nil && s.enrolled[userID][c.ID] {
			completed = append(completed, c.ID)
			diff := c.Difficulty
			if diff == "" {
				diff = "beginner"
			}
			byDiff[diff]++
		}
	}
	xp = s.xp[userID]
	for id := range s.achievements[userID] {
		earned[id] = true
	}
	return lessons, completed, byDiff, xp, earned
}

// --- Memory audit store ---------------------------------------------------

type MemoryAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

// NewMemoryAuditStore returns an empty in-memory audit log.
func NewMemoryAuditStore() *MemoryAuditStore { return &MemoryAuditStore{} }

func (m *MemoryAuditStore) Record(ctx context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := e
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	m.entries = append(m.entries, &cp)
	return nil
}

func (m *MemoryAuditStore) List(ctx context.Context, limit, offset int) ([]*AuditEntry, int, error) {
	return m.ListFiltered(ctx, AuditFilter{Limit: limit, Offset: offset})
}

// ListFiltered returns newest-first entries matching f.
func (m *MemoryAuditStore) ListFiltered(ctx context.Context, f AuditFilter) ([]*AuditEntry, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	// Newest first.
	ordered := make([]*AuditEntry, 0, len(m.entries))
	for i := len(m.entries) - 1; i >= 0; i-- {
		if f.Matches(m.entries[i]) {
			ordered = append(ordered, m.entries[i])
		}
	}
	total := len(ordered)
	if f.Offset >= total {
		return []*AuditEntry{}, total, nil
	}
	end := f.Offset + f.Limit
	if end > total {
		end = total
	}
	return ordered[f.Offset:end], total, nil
}

func (m *MemoryAuditStore) CleanupOlderThan(ctx context.Context, days int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var kept []*AuditEntry
	var removed int64
	for _, e := range m.entries {
		if e.CreatedAt.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	m.entries = kept
	return removed, nil
}

// --- Memory job store -----------------------------------------------------

type MemoryJobStore struct {
	mu    sync.Mutex
	jobs  map[string]*Job
	byKey map[string]string
	order []string
}

// NewMemoryJobStore returns an empty in-memory job queue (tests only;
// a separate worker process cannot share it, so production uses Postgres).
func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{jobs: make(map[string]*Job), byKey: make(map[string]string)}
}

func (m *MemoryJobStore) Enqueue(ctx context.Context, typ string, payload map[string]any, key string) (*Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key != "" {
		if id, ok := m.byKey[key]; ok {
			if j, ok := m.jobs[id]; ok {
				cp := *j
				return &cp, true, nil
			}
		}
	}
	j := &Job{ID: ids.New(), Type: typ, Payload: payload, State: "pending", MaxAttempts: 5, RunAt: time.Now().UTC(), IdempotencyKey: key, CreatedAt: time.Now().UTC()}
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	if key != "" {
		m.byKey[key] = j.ID
	}
	cp := *j
	return &cp, false, nil
}

func (m *MemoryJobStore) Claim(ctx context.Context, workerID string, limit int) ([]*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []*Job
	for _, id := range m.order {
		if len(out) >= limit {
			break
		}
		j := m.jobs[id]
		if (j.State == "pending" || j.State == "failed") && !j.RunAt.After(now) {
			j.State = "running"
			j.Attempts++
			cp := *j
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryJobStore) Complete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.State = "done"
	return nil
}

func (m *MemoryJobStore) Fail(ctx context.Context, id, errMsg string, retryAt time.Time, dead bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.LastError = errMsg
	if dead {
		j.State = "dead"
	} else {
		j.State = "failed"
		j.RunAt = retryAt
	}
	return nil
}

func (m *MemoryJobStore) Counts(ctx context.Context) (map[string]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	for _, j := range m.jobs {
		out[j.State]++
	}
	return out, nil
}

// --- Memory lab-resource store (Milestone 6) ---------------------------

type MemoryResourceStore struct {
	mu    sync.Mutex
	byLab map[string]map[string]*LabResource // labID -> id -> resource
}

func NewMemoryResourceStore() *MemoryResourceStore {
	return &MemoryResourceStore{byLab: make(map[string]map[string]*LabResource)}
}

func copyResource(r *LabResource) *LabResource {
	cp := *r
	if r.Labels != nil {
		cp.Labels = make(map[string]string, len(r.Labels))
		for k, v := range r.Labels {
			cp.Labels[k] = v
		}
	}
	if r.Config != nil {
		cp.Config = make(map[string]any, len(r.Config))
		for k, v := range r.Config {
			cp.Config[k] = v
		}
	}
	return &cp
}

func (m *MemoryResourceStore) ListResources(ctx context.Context, labID string) ([]*LabResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*LabResource
	for _, r := range m.byLab[labID] {
		out = append(out, copyResource(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryResourceStore) GetResource(ctx context.Context, labID, id string) (*LabResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.byLab[labID][id]; ok {
		return copyResource(r), nil
	}
	return nil, ErrNotFound
}

func (m *MemoryResourceStore) CreateResource(ctx context.Context, r *LabResource) (*LabResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := copyResource(r)
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	if m.byLab[cp.LabID] == nil {
		m.byLab[cp.LabID] = make(map[string]*LabResource)
	}
	m.byLab[cp.LabID][cp.ID] = cp
	return copyResource(cp), nil
}

// UpdateResource replaces the stored row (matched by LabID+ID).
func (m *MemoryResourceStore) UpdateResource(ctx context.Context, r *LabResource) (*LabResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byLab[r.LabID][r.ID]; !ok {
		return nil, ErrNotFound
	}
	cp := copyResource(r)
	m.byLab[cp.LabID][cp.ID] = cp
	return copyResource(cp), nil
}

func (m *MemoryResourceStore) DeleteResource(ctx context.Context, labID, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byLab[labID][id]; !ok {
		return false, nil
	}
	delete(m.byLab[labID], id)
	return true, nil
}

// --- Memory terraform-run store (Milestone 7) ---------------------------

type MemoryTerraformRunStore struct {
	mu     sync.Mutex
	runs   map[string]*TerraformRun
	byLab  map[string][]string
	byPlan map[string]string
}

func NewMemoryTerraformRunStore() *MemoryTerraformRunStore {
	return &MemoryTerraformRunStore{runs: make(map[string]*TerraformRun), byLab: make(map[string][]string), byPlan: make(map[string]string)}
}

func copyRun(r *TerraformRun) *TerraformRun {
	cp := *r
	if r.PlanJSON != nil {
		cp.PlanJSON = make(map[string]any, len(r.PlanJSON))
		for k, v := range r.PlanJSON {
			cp.PlanJSON[k] = v
		}
	}
	return &cp
}

func (m *MemoryTerraformRunStore) CreateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := copyRun(r)
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	cp.UpdatedAt = cp.CreatedAt
	m.runs[cp.ID] = cp
	m.byLab[cp.LabID] = append(m.byLab[cp.LabID], cp.ID)
	if cp.PlanID != "" {
		m.byPlan[cp.PlanID] = cp.ID
	}
	return copyRun(cp), nil
}

func (m *MemoryTerraformRunStore) GetRun(ctx context.Context, id string) (*TerraformRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[id]; ok {
		return copyRun(r), nil
	}
	return nil, ErrNotFound
}

func (m *MemoryTerraformRunStore) GetRunByPlanID(ctx context.Context, planID string) (*TerraformRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.byPlan[planID]; ok {
		if r, ok := m.runs[id]; ok {
			return copyRun(r), nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryTerraformRunStore) ListRunsByLab(ctx context.Context, labID string, limit int) ([]*TerraformRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.byLab[labID]
	var out []*TerraformRun
	for i := len(ids) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		if r, ok := m.runs[ids[i]]; ok {
			out = append(out, copyRun(r))
		}
	}
	return out, nil
}

func (m *MemoryTerraformRunStore) UpdateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.runs[r.ID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := copyRun(r)
	cp.CreatedAt = existing.CreatedAt
	cp.UpdatedAt = time.Now().UTC()
	m.runs[r.ID] = cp
	if cp.PlanID != "" {
		m.byPlan[cp.PlanID] = cp.ID
	}
	return copyRun(cp), nil
}

// --- Memory terminal-session store (Milestone 6) ------------------------

type MemoryTerminalSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*TerminalSession
}

func NewMemoryTerminalSessionStore() *MemoryTerminalSessionStore {
	return &MemoryTerminalSessionStore{sessions: make(map[string]*TerminalSession)}
}

func (m *MemoryTerminalSessionStore) CreateSession(ctx context.Context, s *TerminalSession) (*TerminalSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	m.sessions[cp.ID] = &cp
	return &cp, nil
}

func (m *MemoryTerminalSessionStore) MarkSessionUsed(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.Used = true
	return nil
}

func (m *MemoryTerminalSessionStore) EndSession(ctx context.Context, id string, endedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.EndedAt = &endedAt
	return nil
}

func (m *MemoryTerminalSessionStore) GetSession(ctx context.Context, id string) (*TerminalSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, ErrNotFound
}

// --- Memory quota store (Milestone 9, Part 3) ----------------------------

// MemoryQuotaStore holds per-role quota sets in memory, seeded with
// the built-in defaults so a fresh server behaves like a seeded
// database. UpsertQuota replaces the row for the role.
type MemoryQuotaStore struct {
	mu     sync.Mutex
	quotas map[string]*QuotaSet
}

// NewMemoryQuotaStore returns a quota store seeded with the default
// limits for every valid role (quota.DefaultLimits).
func NewMemoryQuotaStore() *MemoryQuotaStore {
	m := &MemoryQuotaStore{quotas: map[string]*QuotaSet{}}
	for _, role := range quota.ValidRoles {
		l := quota.DefaultLimits(role)
		m.quotas[role] = &QuotaSet{
			Role: role, MaxLabs: l.MaxLabs, MaxCPU: l.MaxCPU,
			MaxMemoryMB: l.MaxMemoryMB, MaxDiskGB: l.MaxDiskGB,
			MaxLabHours: l.MaxLabHours, MaxLabMinutes: l.MaxLabMinutes,
			UpdatedAt: time.Now().UTC(),
		}
	}
	return m
}

func (m *MemoryQuotaStore) GetQuotas(ctx context.Context) ([]*QuotaSet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*QuotaSet, 0, len(m.quotas))
	for _, role := range quota.ValidRoles {
		if q, ok := m.quotas[role]; ok {
			cp := *q
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryQuotaStore) GetQuota(ctx context.Context, role string) (*QuotaSet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q, ok := m.quotas[role]; ok {
		cp := *q
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryQuotaStore) UpsertQuota(ctx context.Context, q *QuotaSet) error {
	if q == nil || q.Role == "" {
		return ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *q
	cp.UpdatedAt = time.Now().UTC()
	m.quotas[q.Role] = &cp
	return nil
}

// Ensure the memory adapters satisfy the interfaces at compile time.
var (
	_ UserStore            = (*auth.Store)(nil)
	_ LabStore             = (*MemoryLabStore)(nil)
	_ CourseStore          = (*MemoryCourseStore)(nil)
	_ AuditStore           = (*MemoryAuditStore)(nil)
	_ JobStore             = (*MemoryJobStore)(nil)
	_ LabResourceStore     = (*MemoryResourceStore)(nil)
	_ TerraformRunStore    = (*MemoryTerraformRunStore)(nil)
	_ TerminalSessionStore = (*MemoryTerminalSessionStore)(nil)
	_ QuotaStore           = (*MemoryQuotaStore)(nil)
)
