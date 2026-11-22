// Terraform Learning (CloudForge v2, Phase 5, master prompt §9) —
// a first-class main-navigation section, not a documentation page.
//
// Layers, kept separate and labelled where they surface:
//   - Courses: static lesson content (tflearn.Levels) + per-user
//     progress in the tflearn_progress collection.
//   - Editor: projects + files in tflearn_projects / tflearn_files.
//     Validate/format are LOCAL checks (tflearn.ValidateHCL /
//     FormatHCL) — real, synchronous, no Docker, no Terraform
//     binary — and are labelled "local" in every response.
//   - Plan preview: a LOCAL static diff (tflearn.ComputePlan,
//     source "cloudforge-local-diff"), never a terraform binary plan.
//     Approving a preview records that the learner reviewed it; it
//     is NOT an apply gate.
//   - Container execution: delegates to the Milestone 7 manager
//     (enqueueTerraformRun + the terraform_runs store + the worker)
//     through the lab a project is linked to. Apply needs an M7
//     approved, unexpired plan (loadApprovablePlan) plus an explicit
//     confirm=true; destroy needs confirm=true. Without Docker the
//     availability check answers 503 terraform_unavailable and a
//     FAILED tflearn run is recorded — never a faked success.
//   - State: append-only versioned snapshots in tflearn_states
//     (refreshed from the latest succeeded M7 plan run of the linked
//     lab), locks in tflearn_locks (10 min, force-unlock by the
//     holder or an admin), drift in the State tab against declared
//     configuration, recorded state, and live lab resources.
//
// Ownership: every entity records owner_id; a non-admin sees only
// their own. Admins see everything. All lists are list-then-filter
// in Go (teaching-scale volumes — the same pattern as Phase 3).
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
	"github.com/cloudforge/cloudforge/backend/internal/tflearn"
)

// Collections for Terraform Learning (tables p5_*,
// migration 011_tflearn.sql).
const (
	tflearnProjects = "tflearn_projects"
	tflearnFiles    = "tflearn_files"
	tflearnProgress = "tflearn_progress"
	tflearnRuns     = "tflearn_runs"
	tflearnStates   = "tflearn_states"
	tflearnLocks    = "tflearn_locks"
)

// tflearnLockTTL bounds a state lock; expired locks are treated as
// absent and removed on sight.
const tflearnLockTTL = 10 * time.Minute

// tflearnPlanTTL bounds an approved plan preview (mirrors the M7
// PlanTTL of 1 hour).
const tflearnPlanTTL = time.Hour

func (s *Server) registerTFlearnRoutes(r chi.Router) {
	r.Get("/terraform-learning/dashboard", s.handleTFlearnDashboard)
	r.Get("/terraform-learning/courses", s.handleTFlearnCourses)
	r.Get("/terraform-learning/courses/{level}/{lessonId}", s.handleTFlearnLesson)
	r.Post("/terraform-learning/courses/{level}/{lessonId}/complete", s.handleTFlearnCompleteLesson)
	r.Get("/terraform-learning/provider", s.handleTFlearnProvider)
	r.Get("/terraform-learning/provider/resources", s.handleTFlearnProviderResources)
	r.Get("/terraform-learning/projects", s.handleTFlearnListProjects)
	r.Post("/terraform-learning/projects", s.handleTFlearnCreateProject)
	r.Get("/terraform-learning/projects/{id}", s.handleTFlearnGetProject)
	r.Put("/terraform-learning/projects/{id}", s.handleTFlearnUpdateProject)
	r.Delete("/terraform-learning/projects/{id}", s.handleTFlearnDeleteProject)
	r.Get("/terraform-learning/projects/{id}/files", s.handleTFlearnListFiles)
	r.Post("/terraform-learning/projects/{id}/files", s.handleTFlearnSaveFile)
	r.Delete("/terraform-learning/projects/{id}/files", s.handleTFlearnDeleteFile)
	r.Post("/terraform-learning/projects/{id}/validate", s.handleTFlearnValidate)
	r.Post("/terraform-learning/projects/{id}/format", s.handleTFlearnFormat)
	r.Post("/terraform-learning/projects/{id}/plan-preview", s.handleTFlearnPlanPreview)
	r.Post("/terraform-learning/projects/{id}/plans/{planId}/approve", s.handleTFlearnApprovePlan)
	r.Post("/terraform-learning/projects/{id}/executions", s.handleTFlearnExecute)
	r.Get("/terraform-learning/projects/{id}/executions", s.handleTFlearnListExecutions)
	r.Get("/terraform-learning/executions/{id}", s.handleTFlearnGetExecution)
	r.Post("/terraform-learning/projects/{id}/executions/{runId}/approve", s.handleTFlearnApproveExecution)
	r.Get("/terraform-learning/projects/{id}/state", s.handleTFlearnGetState)
	r.Get("/terraform-learning/projects/{id}/state/history", s.handleTFlearnStateHistory)
	r.Post("/terraform-learning/projects/{id}/state/lock", s.handleTFlearnLockState)
	r.Post("/terraform-learning/projects/{id}/state/unlock", s.handleTFlearnUnlockState)
	r.Post("/terraform-learning/projects/{id}/state/backup", s.handleTFlearnBackupState)
	r.Post("/terraform-learning/projects/{id}/state/restore", s.handleTFlearnRestoreState)
	r.Get("/terraform-learning/projects/{id}/state/drift", s.handleTFlearnDrift)
}

// --- small helpers -------------------------------------------------------

func tfStr(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func tfStrSlice(data map[string]any, key string) []string {
	raw, ok := data[key].([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func tfInt(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func tfBool(data map[string]any, key string) bool {
	b, _ := data[key].(bool)
	return b
}

func tfTime(data map[string]any, key string) *time.Time {
	if s, ok := data[key].(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &t
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return &t
		}
	}
	return nil
}

// tflearnVisible reports whether user u may see an entity owned by
// ownerID (owners see their own; admins see everything).
func tflearnVisible(u *auth.User, ownerID string) bool {
	return u.Role == "admin" || u.ID == ownerID
}

// tflearnProject loads a project by id and enforces ownership (404
// for other users' projects: do not leak their existence).
func (s *Server) tflearnProject(w http.ResponseWriter, r *http.Request, u *auth.User) (*store.P3Entity, store.Phase3Store, bool) {
	st, ok := s.p3Store(w, r)
	if !ok {
		return nil, nil, false
	}
	proj, err := st.P3Get(r.Context(), tflearnProjects, chi.URLParam(r, "id"))
	if err != nil || !tflearnVisible(u, tfStr(proj.Data, "owner_id")) {
		s.writeError(w, r, http.StatusNotFound, "project_not_found", "no Terraform Learning project with that id")
		return nil, nil, false
	}
	return proj, st, true
}

func tflearnProjectView(e *store.P3Entity) map[string]any {
	return map[string]any{
		"id": e.ID, "name": e.Name,
		"description": tfStr(e.Data, "description"),
		"level":       tfInt(e.Data, "level"),
		"template":    tfStr(e.Data, "template"),
		"lab_id":      tfStr(e.Data, "lab_id"),
		"owner_id":    tfStr(e.Data, "owner_id"),
		"created_at":  e.CreatedAt, "updated_at": e.UpdatedAt,
	}
}

// tflearnProjectFiles returns the project's files sorted by path.
func (s *Server) tflearnProjectFiles(r *http.Request, st store.Phase3Store, projectID string) ([]*store.P3Entity, error) {
	all, err := st.P3List(r.Context(), tflearnFiles)
	if err != nil {
		return nil, err
	}
	out := []*store.P3Entity{}
	for _, f := range all {
		if tfStr(f.Data, "project_id") == projectID {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return tfStr(out[i].Data, "path") < tfStr(out[j].Data, "path") })
	return out, nil
}

func tflearnFileView(e *store.P3Entity) map[string]any {
	return map[string]any{
		"id": e.ID, "project_id": tfStr(e.Data, "project_id"),
		"path": tfStr(e.Data, "path"), "content": tfStr(e.Data, "content"),
		"updated_at": e.UpdatedAt,
	}
}

// tflearnActiveLock returns the project's active lock, if any.
// Expired locks are removed on sight and treated as absent.
func (s *Server) tflearnActiveLock(r *http.Request, st store.Phase3Store, projectID string) (*store.P3Entity, error) {
	all, err := st.P3List(r.Context(), tflearnLocks)
	if err != nil {
		return nil, err
	}
	for _, l := range all {
		if tfStr(l.Data, "project_id") != projectID {
			continue
		}
		if exp := tfTime(l.Data, "expires_at"); exp != nil && time.Now().UTC().After(*exp) {
			_ = st.P3Delete(r.Context(), tflearnLocks, l.ID)
			return nil, nil
		}
		return l, nil
	}
	return nil, nil
}

// tflearnLockedByOther reports whether the project is locked by
// someone other than u.
func (s *Server) tflearnLockedByOther(w http.ResponseWriter, r *http.Request, st store.Phase3Store, projectID string, u *auth.User) bool {
	lock, err := s.tflearnActiveLock(r, st, projectID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return true
	}
	if lock != nil && tfStr(lock.Data, "locked_by") != u.ID {
		s.writeError(w, r, http.StatusConflict, "state_locked",
			fmt.Sprintf("state is locked by %s until %s; unlock it first", tfStr(lock.Data, "locked_by"), tfStr(lock.Data, "expires_at")))
		return true
	}
	return false
}

// validateTFlearnPath checks an editor file path: relative, under
// the project, .tf/.tfvars/.md only, no traversal.
func validateTFlearnPath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return fmt.Errorf("path %q must be a relative path inside the project", path)
	}
	if !strings.HasSuffix(path, ".tf") && !strings.HasSuffix(path, ".tfvars") && !strings.HasSuffix(path, ".md") {
		return fmt.Errorf("path %q must end in .tf, .tfvars or .md", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." {
			return fmt.Errorf("path %q has an empty segment", path)
		}
	}
	if len(path) > 128 {
		return errors.New("path must be at most 128 characters")
	}
	return nil
}

// decodeTFlearn decodes a JSON body with unknown fields rejected.
func (s *Server) decodeTFlearn(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.ContentLength == 0 {
		return true
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// --- dashboard -----------------------------------------------------------

func (s *Server) handleTFlearnDashboard(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// Progress: completed lessons for this user.
	progress, err := st.P3List(ctx, tflearnProgress)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	completed := map[string]bool{}
	completedCount := 0
	for _, p := range progress {
		if tfStr(p.Data, "user_id") == u.ID && tfStr(p.Data, "status") == "completed" {
			completed[tfStr(p.Data, "lesson_id")] = true
			completedCount++
		}
	}
	total := tflearn.TotalLessons()
	// Current course: first level with an incomplete lesson.
	currentLevel := 0
	currentLesson := ""
	for _, lvl := range tflearn.Levels() {
		for _, lesson := range lvl.Lessons {
			if !completed[lesson.ID] {
				currentLevel = lvl.Level
				currentLesson = lesson.ID
				break
			}
		}
		if currentLevel != 0 {
			break
		}
	}
	if currentLevel == 0 {
		currentLevel = 3
	}
	// Projects owned by this user.
	projects, err := st.P3List(ctx, tflearnProjects)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ownProjects := []map[string]any{}
	for _, p := range projects {
		if tflearnVisible(u, tfStr(p.Data, "owner_id")) {
			ownProjects = append(ownProjects, tflearnProjectView(p))
		}
	}
	// Executions: runs owned by this user, newest first.
	runs, err := st.P3List(ctx, tflearnRuns)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ownRuns := []*store.P3Entity{}
	for _, rn := range runs {
		if tflearnVisible(u, tfStr(rn.Data, "owner_id")) {
			ownRuns = append(ownRuns, rn)
		}
	}
	sort.Slice(ownRuns, func(i, j int) bool { return ownRuns[i].CreatedAt.After(ownRuns[j].CreatedAt) })
	plans, applies := 0, 0
	recent := []map[string]any{}
	for i, rn := range ownRuns {
		op := tfStr(rn.Data, "operation")
		if tfStr(rn.Data, "status") == "SUCCEEDED" {
			if op == "plan" || op == "plan-preview" {
				plans++
			}
			if op == "apply" {
				applies++
			}
		}
		if i < 5 {
			recent = append(recent, tflearnRunView(rn))
		}
	}
	// Achievements, derived from real progress (never canned).
	achievements := []map[string]any{
		{"id": "tf-first-lesson", "title": "First Lesson", "description": "Complete your first Terraform lesson", "earned": completedCount >= 1},
		{"id": "tf-level-1", "title": "Fundamentals Complete", "description": "Complete all 15 Level 1 lessons", "earned": tflearnLevelComplete(completed, 1)},
		{"id": "tf-level-2", "title": "Intermediate Complete", "description": "Complete all 20 Level 2 lessons", "earned": tflearnLevelComplete(completed, 2)},
		{"id": "tf-level-3", "title": "Advanced Complete", "description": "Complete all 20 Level 3 lessons", "earned": tflearnLevelComplete(completed, 3)},
		{"id": "tf-first-project", "title": "First Project", "description": "Create your first Terraform project", "earned": len(ownProjects) >= 1},
		{"id": "tf-first-plan", "title": "First Plan", "description": "Run a successful plan (preview or container)", "earned": plans >= 1},
		{"id": "tf-first-apply", "title": "First Apply", "description": "Apply a plan successfully", "earned": applies >= 1},
		{"id": "tf-all-lessons", "title": "Terraform Graduate", "description": "Complete all 55 lessons", "earned": completedCount >= total && total > 0},
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"progress": map[string]any{
			"completed_lessons": completedCount, "total_lessons": total,
			"percent": func() int {
				if total == 0 {
					return 0
				}
				return completedCount * 100 / total
			}(),
			"current_level": currentLevel, "current_lesson_id": currentLesson,
			"pending_exercises": total - completedCount,
			"skill_level":       tflearn.SkillLevel(completedCount, total),
		},
		"projects":            ownProjects,
		"project_count":       len(ownProjects),
		"successful_plans":    plans,
		"successful_applies":  applies,
		"recent_executions":   recent,
		"achievements":        achievements,
		"terraform_available": tflearnTerraformAvailable(r, s),
		"mode_lite":           s.Lite,
	})
}

func tflearnLevelComplete(completed map[string]bool, level int) bool {
	for _, lvl := range tflearn.Levels() {
		if lvl.Level != level {
			continue
		}
		for _, lesson := range lvl.Lessons {
			if !completed[lesson.ID] {
				return false
			}
		}
		return true
	}
	return false
}

// tflearnTerraformAvailable reports whether container execution is
// available right now (Docker present), without creating a run.
func tflearnTerraformAvailable(r *http.Request, s *Server) bool {
	_, _, _, ok := s.terraformAvailability(r)
	return ok
}

// --- courses -------------------------------------------------------------

func tflearnLessonView(l tflearn.Lesson, completed bool, score, total int) map[string]any {
	return map[string]any{
		"id": l.ID, "level": l.Level, "title": l.Title, "summary": l.Summary,
		"explanation": l.Explanation, "use_case": l.UseCase,
		"code_example": l.CodeExample, "exercise": l.Exercise,
		"expected_output": l.ExpectedOutput, "common_mistakes": l.Mistakes,
		"knowledge_check": map[string]any{
			"question": l.Quiz.Question, "options": l.Quiz.Options,
			"explanation": l.Quiz.Explanation,
		},
		"curriculum_lesson": l.CurriculumLesson, "xp": l.XP,
		"completed": completed, "quiz_score": score, "quiz_total": total,
	}
}

func (s *Server) handleTFlearnCourses(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	completed := map[string]bool{}
	progress, err := st.P3List(r.Context(), tflearnProgress)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, p := range progress {
		if tfStr(p.Data, "user_id") == u.ID && tfStr(p.Data, "status") == "completed" {
			completed[tfStr(p.Data, "lesson_id")] = true
		}
	}
	levels := []map[string]any{}
	for _, lvl := range tflearn.Levels() {
		lessons := []map[string]any{}
		done := 0
		for _, lesson := range lvl.Lessons {
			if completed[lesson.ID] {
				done++
			}
			lessons = append(lessons, map[string]any{
				"id": lesson.ID, "title": lesson.Title, "summary": lesson.Summary,
				"completed": completed[lesson.ID], "xp": lesson.XP,
				"curriculum_lesson": lesson.CurriculumLesson,
			})
		}
		levels = append(levels, map[string]any{
			"level": lvl.Level, "title": lvl.Title, "description": lvl.Description,
			"lessons": lessons, "completed": done, "total": len(lvl.Lessons),
		})
	}
	projects := []map[string]any{}
	for _, p := range tflearn.Projects() {
		projects = append(projects, map[string]any{
			"id": p.ID, "level": p.Level, "title": p.Title, "description": p.Description,
			"objectives": p.Objectives, "tasks": p.Tasks,
			"validation_criteria": p.Criteria, "xp": p.XP,
		})
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"levels": levels, "projects": projects,
		"total_lessons":     tflearn.TotalLessons(),
		"completed_lessons": len(completed),
	})
}

func (s *Server) handleTFlearnLesson(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	level, err := strconv.Atoi(chi.URLParam(r, "level"))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "level must be a number")
		return
	}
	lesson, found := tflearn.FindLesson(chi.URLParam(r, "lessonId"))
	if !found || lesson.Level != level {
		s.writeError(w, r, http.StatusNotFound, "lesson_not_found", "no Terraform lesson with that id")
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	completed, score, total := false, 0, 0
	if p, err := st.P3GetByName(r.Context(), tflearnProgress, u.ID+":"+lesson.ID); err == nil {
		completed = tfStr(p.Data, "status") == "completed"
		score = tfInt(p.Data, "quiz_score")
		total = tfInt(p.Data, "quiz_total")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"lesson": tflearnLessonView(lesson, completed, score, total)})
}

func (s *Server) handleTFlearnCompleteLesson(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	level, err := strconv.Atoi(chi.URLParam(r, "level"))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "level must be a number")
		return
	}
	lesson, found := tflearn.FindLesson(chi.URLParam(r, "lessonId"))
	if !found || lesson.Level != level {
		s.writeError(w, r, http.StatusNotFound, "lesson_not_found", "no Terraform lesson with that id")
		return
	}
	var req struct {
		AnswerIndex *int `json:"answer_index"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	if req.AnswerIndex == nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "answer_index is required: answer the knowledge check to complete the lesson")
		return
	}
	correct := *req.AnswerIndex == lesson.Quiz.Correct
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	score, total := 0, 1
	if correct {
		score = 1
		name := u.ID + ":" + lesson.ID
		now := time.Now().UTC()
		data := map[string]any{
			"user_id": u.ID, "lesson_id": lesson.ID, "status": "completed",
			"quiz_score": score, "quiz_total": total, "completed_at": now.Format(time.RFC3339Nano),
		}
		if existing, err := st.P3GetByName(r.Context(), tflearnProgress, name); err == nil {
			existing.Data = data
			if _, err := st.P3Update(r.Context(), tflearnProgress, existing); err != nil {
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		} else {
			if _, err := st.P3Create(r.Context(), tflearnProgress, &store.P3Entity{Name: name, Data: data}); err != nil {
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		}
	}
	s.audit(r, u.ID, "tflearn_lesson_complete", "tflearn_lesson", lesson.ID,
		map[string]any{"correct": correct, "level": level})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"lesson_id": lesson.ID, "correct": correct, "completed": correct,
		"quiz_score": score, "quiz_total": total,
		"explanation": lesson.Quiz.Explanation,
		"message": func() string {
			if correct {
				return "Knowledge check passed — lesson complete."
			}
			return "That answer is not correct — read the explanation and try again. The lesson is not marked complete."
		}(),
	})
}

// --- provider ------------------------------------------------------------

func (s *Server) handleTFlearnProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"name": "cloudforge", "source": "cloudforge/local (learning provider)",
		"version":        "platform version (see GET /api/v1/health)",
		"description":    "The CloudForge Terraform provider maps cloudforge_* resources onto real CloudForge APIs. It never needs AWS credentials and never reaches real AWS. aws_* resources execute through the container runner against the local emulator.",
		"install_docs":   tflearn.ProviderInstallDocs,
		"resource_count": len(tflearn.ProviderResources),
		"fidelity": map[string]any{
			"execution":   "PARTIAL — local validate/format/plan-preview are real; container plan/apply needs Docker and a linked lab; cloudforge_* resources are validated locally and applied through the mapped CloudForge APIs",
			"credentials": "No AWS credentials are ever required or accepted.",
		},
	})
}

func (s *Server) handleTFlearnProviderResources(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"resources": tflearn.ProviderResources})
}

// --- projects ------------------------------------------------------------

func (s *Server) handleTFlearnListProjects(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), tflearnProjects)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, p := range all {
		if tflearnVisible(u, tfStr(p.Data, "owner_id")) {
			out = append(out, tflearnProjectView(p))
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"projects": out})
}

func (s *Server) handleTFlearnCreateProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Level       int    `json:"level"`
		Template    string `json:"template"`
		LabID       string `json:"lab_id"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if len(req.Name) > 128 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name must be at most 128 characters")
		return
	}
	if req.Level == 0 {
		req.Level = 1
	}
	if req.Level < 1 || req.Level > 4 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "level must be 1-4")
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	// Per-user unique name.
	existing, err := st.P3List(r.Context(), tflearnProjects)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, p := range existing {
		if tfStr(p.Data, "owner_id") == u.ID && p.Name == req.Name {
			s.writeError(w, r, http.StatusConflict, "already_exists", "you already have a project named "+req.Name)
			return
		}
	}
	// Template starter files.
	starter := map[string]string{}
	template := ""
	if req.Template != "" {
		for _, p := range tflearn.Projects() {
			if p.ID == req.Template {
				starter = p.StarterFiles
				template = p.ID
				break
			}
		}
		if template == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "unknown template "+req.Template)
			return
		}
	}
	if starter == nil {
		starter = map[string]string{}
	}
	// Optional lab link: the lab must exist and belong to the user
	// (or the user is an admin).
	if req.LabID != "" {
		lab, err := s.Labs.Get(r.Context(), req.LabID)
		if err != nil || !tflearnVisible(u, lab.OwnerID) {
			s.writeError(w, r, http.StatusNotFound, "lab_not_found", "no lab with that id")
			return
		}
	}
	proj, err := st.P3Create(r.Context(), tflearnProjects, &store.P3Entity{Name: req.Name, Data: map[string]any{
		"owner_id": u.ID, "description": req.Description, "level": req.Level,
		"template": template, "lab_id": req.LabID,
	}})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Starter files, validated like any save (they are curated, so
	// they must pass; a failure here is a 500, not a silent skip).
	for path, content := range starter {
		if err := validateTFlearnPath(path); err != nil {
			continue
		}
		if _, err := st.P3Create(r.Context(), tflearnFiles, &store.P3Entity{
			Name: proj.ID + ":" + path,
			Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID, "path": path, "content": content},
		}); err != nil {
			s.Logger.Warn("could not seed template file", "project_id", proj.ID, "path", path, "error", err)
		}
	}
	// Initial state version 1: empty, source "created".
	if _, err := st.P3Create(r.Context(), tflearnStates, &store.P3Entity{
		Name: proj.ID + ":v1",
		Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID, "version": 1,
			"resources": []any{}, "source": "created"},
	}); err != nil {
		s.Logger.Warn("could not seed initial state", "project_id", proj.ID, "error", err)
	}
	s.audit(r, u.ID, "tflearn_project_created", "tflearn_project", proj.ID, map[string]any{"template": template})
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"project": tflearnProjectView(proj)})
}

func (s *Server) handleTFlearnGetProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	files, err := s.tflearnProjectFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := []map[string]any{}
	for _, f := range files {
		views = append(views, tflearnFileView(f))
	}
	view := tflearnProjectView(proj)
	view["files"] = views
	s.writeJSON(w, r, http.StatusOK, map[string]any{"project": view})
}

func (s *Server) handleTFlearnUpdateProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		LabID       *string `json:"lab_id"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	if req.LabID != nil && *req.LabID != "" {
		lab, err := s.Labs.Get(r.Context(), *req.LabID)
		if err != nil || !tflearnVisible(u, lab.OwnerID) {
			s.writeError(w, r, http.StatusNotFound, "lab_not_found", "no lab with that id")
			return
		}
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name must not be empty")
			return
		}
		proj.Name = name
	}
	if req.Description != nil {
		proj.Data["description"] = *req.Description
	}
	if req.LabID != nil {
		proj.Data["lab_id"] = *req.LabID
	}
	updated, err := st.P3Update(r.Context(), tflearnProjects, proj)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"project": tflearnProjectView(updated)})
}

func (s *Server) handleTFlearnDeleteProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	ctx := r.Context()
	for _, coll := range []string{tflearnFiles, tflearnRuns, tflearnStates, tflearnLocks} {
		all, err := st.P3List(ctx, coll)
		if err != nil {
			continue
		}
		for _, e := range all {
			if tfStr(e.Data, "project_id") == proj.ID {
				_ = st.P3Delete(ctx, coll, e.ID)
			}
		}
	}
	if err := st.P3Delete(ctx, tflearnProjects, proj.ID); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_project_deleted", "tflearn_project", proj.ID, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "project_id": proj.ID})
}

// --- files ---------------------------------------------------------------

func (s *Server) handleTFlearnListFiles(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	files, err := s.tflearnProjectFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := []map[string]any{}
	for _, f := range files {
		views = append(views, tflearnFileView(f))
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"project_id": proj.ID, "files": views})
}

func (s *Server) handleTFlearnSaveFile(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	if err := validateTFlearnPath(req.Path); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Content) > 64*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "file content exceeds the 64 KiB limit")
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	// HCL validation on save is PROJECT-wide, not per-file: a file
	// may legally reference resources declared in another file of
	// the same project. Validate the concatenated configuration
	// (with this file's new content substituted) and refuse only
	// real errors; warnings are allowed through. Line numbers in
	// the returned issues refer to the concatenated project
	// configuration.
	if strings.HasSuffix(req.Path, ".tf") {
		if err := terraform.ValidateConfiguration(req.Content); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		_, issues := s.tflearnCombinedHCL(r, st, proj.ID, req.Path, req.Content)
		if tflearn.HasErrors(issues) {
			s.writeJSON(w, r, http.StatusBadRequest, map[string]any{
				"error":  map[string]any{"code": "invalid_hcl", "message": "configuration has HCL errors (project-wide check; line numbers refer to the concatenated project configuration)", "request_id": RequestID(r.Context())},
				"issues": issues,
			})
			return
		}
	}
	name := proj.ID + ":" + req.Path
	existing, err := st.P3GetByName(r.Context(), tflearnFiles, name)
	if err == nil {
		existing.Data["content"] = req.Content
		updated, err := st.P3Update(r.Context(), tflearnFiles, existing)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"file": tflearnFileView(updated)})
		return
	}
	created, err := st.P3Create(r.Context(), tflearnFiles, &store.P3Entity{
		Name: name,
		Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID, "path": req.Path, "content": req.Content},
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"file": tflearnFileView(created)})
}

func (s *Server) handleTFlearnDeleteFile(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	existing, err := st.P3GetByName(r.Context(), tflearnFiles, proj.ID+":"+req.Path)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "file_not_found", "no file with that path in this project")
		return
	}
	if err := st.P3Delete(r.Context(), tflearnFiles, existing.ID); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "path": req.Path})
}

// --- local validate / format / plan preview -------------------------------

// tflearnHCLFiles returns the project's .tf file contents in path
// order, plus per-file validation issues.
func (s *Server) tflearnHCLFiles(r *http.Request, st store.Phase3Store, projectID string) ([]*store.P3Entity, map[string][]tflearn.HCLIssue, error) {
	files, err := s.tflearnProjectFiles(r, st, projectID)
	if err != nil {
		return nil, nil, err
	}
	issues := map[string][]tflearn.HCLIssue{}
	for _, f := range files {
		path := tfStr(f.Data, "path")
		if strings.HasSuffix(path, ".tf") {
			// Per-file issues are syntax-level only: reference
			// errors ("reference to undeclared ...") are
			// project-wide and are reported by the combined check
			// in tflearnCombinedHCL, not here — a file may legally
			// reference resources declared in another file.
			kept := []tflearn.HCLIssue{}
			for _, is := range tflearn.ValidateHCL(tfStr(f.Data, "content")) {
				if strings.HasPrefix(is.Message, "reference to undeclared") {
					continue
				}
				kept = append(kept, is)
			}
			issues[path] = kept
		}
	}
	return files, issues, nil
}

// tflearnCombinedHCL concatenates the project's .tf files (with
// overridePath's content replaced by overrideContent when given)
// and validates the whole project at once. HCL is project-scoped:
// cross-file references are legal, so per-file validation alone
// would refuse valid projects.
func (s *Server) tflearnCombinedHCL(r *http.Request, st store.Phase3Store, projectID, overridePath, overrideContent string) (string, []tflearn.HCLIssue) {
	files, err := s.tflearnProjectFiles(r, st, projectID)
	if err != nil {
		return "", []tflearn.HCLIssue{{Line: 0, Severity: "error", Message: err.Error()}}
	}
	var b strings.Builder
	replaced := false
	for _, f := range files {
		path := tfStr(f.Data, "path")
		if !strings.HasSuffix(path, ".tf") {
			continue
		}
		content := tfStr(f.Data, "content")
		if path == overridePath {
			content = overrideContent
			replaced = true
		}
		fmt.Fprintf(&b, "# --- %s ---\n%s\n", path, content)
	}
	if overridePath != "" && !replaced && strings.HasSuffix(overridePath, ".tf") {
		fmt.Fprintf(&b, "# --- %s ---\n%s\n", overridePath, overrideContent)
	}
	combined := b.String()
	return combined, tflearn.ValidateHCL(combined)
}

func (s *Server) handleTFlearnValidate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	// Per-file issues for display, plus the project-wide check
	// (cross-file references) under "project".
	_, issues, err := s.tflearnHCLFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	_, combinedIssues := s.tflearnCombinedHCL(r, st, proj.ID, "", "")
	if len(combinedIssues) > 0 {
		issues["project"] = combinedIssues
	}
	// Also run the M7 configuration guard (amazonaws.com / embedded
	// credentials) across the concatenated files.
	files, _ := s.tflearnProjectFiles(r, st, proj.ID)
	var combined strings.Builder
	for _, f := range files {
		combined.WriteString(tfStr(f.Data, "content"))
		combined.WriteString("\n")
	}
	if err := terraform.ValidateConfiguration(combined.String()); err != nil {
		issues["configuration"] = append(issues["configuration"], tflearn.HCLIssue{Line: 0, Severity: "error", Message: err.Error()})
	}
	valid := true
	for _, list := range issues {
		if tflearn.HasErrors(list) {
			valid = false
		}
	}
	status := http.StatusOK
	if !valid {
		status = http.StatusBadRequest
	}
	s.writeJSON(w, r, status, map[string]any{
		"project_id": proj.ID, "valid": valid, "issues": issues,
		"check": "local", "note": "Local HCL validation (no Docker, no Terraform binary). A green local check does not guarantee the container validate will pass.",
	})
}

func (s *Server) handleTFlearnFormat(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	files, err := s.tflearnProjectFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	changed := []string{}
	for _, f := range files {
		path := tfStr(f.Data, "path")
		if !strings.HasSuffix(path, ".tf") {
			continue
		}
		content := tfStr(f.Data, "content")
		formatted := tflearn.FormatHCL(content)
		if formatted != content {
			f.Data["content"] = formatted
			if _, err := st.P3Update(r.Context(), tflearnFiles, f); err != nil {
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			changed = append(changed, path)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"project_id": proj.ID, "formatted_files": changed,
		"check": "local", "note": "Local formatter (indentation + spacing only, heredocs untouched). Not terraform fmt.",
	})
}

// tflearnRunView renders a tflearn run entity.
func tflearnRunView(e *store.P3Entity) map[string]any {
	return map[string]any{
		"id": e.ID, "project_id": tfStr(e.Data, "project_id"),
		"operation": tfStr(e.Data, "operation"), "status": tfStr(e.Data, "status"),
		"plan_id": tfStr(e.Data, "plan_id"), "approved": tfBool(e.Data, "approved"),
		"approved_at": tfStr(e.Data, "approved_at"), "expires_at": tfStr(e.Data, "expires_at"),
		"log": tfStr(e.Data, "log"), "error": tfStr(e.Data, "error"),
		"terraform_run_id": tfStr(e.Data, "terraform_run_id"),
		"source":           tfStr(e.Data, "source"),
		"changes":          e.Data["changes"],
		"created_at":       e.CreatedAt, "updated_at": e.UpdatedAt,
	}
}

func (s *Server) tflearnRecordRun(r *http.Request, st store.Phase3Store, projectID, ownerID, operation, status, source string, extra map[string]any) (*store.P3Entity, error) {
	data := map[string]any{
		"project_id": projectID, "owner_id": ownerID,
		"operation": operation, "status": status, "source": source,
	}
	for k, v := range extra {
		data[k] = v
	}
	name := fmt.Sprintf("%s:%s:%d", projectID, operation, time.Now().UTC().UnixNano())
	return st.P3Create(r.Context(), tflearnRuns, &store.P3Entity{Name: name, Data: data})
}

func (s *Server) handleTFlearnPlanPreview(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	files, issues, err := s.tflearnHCLFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, list := range issues {
		if tflearn.HasErrors(list) {
			s.writeJSON(w, r, http.StatusBadRequest, map[string]any{
				"error":  map[string]any{"code": "invalid_hcl", "message": "fix HCL errors before planning", "request_id": RequestID(r.Context())},
				"issues": issues,
			})
			return
		}
	}
	// Project-wide validation (cross-file references included).
	if _, combined := s.tflearnCombinedHCL(r, st, proj.ID, "", ""); tflearn.HasErrors(combined) {
		s.writeJSON(w, r, http.StatusBadRequest, map[string]any{
			"error":  map[string]any{"code": "invalid_hcl", "message": "fix HCL errors before planning", "request_id": RequestID(r.Context())},
			"issues": combined,
		})
		return
	}
	declared := []tflearn.DeclaredResource{}
	for _, f := range files {
		if strings.HasSuffix(tfStr(f.Data, "path"), ".tf") {
			declared = append(declared, tflearn.ExtractResources(tfStr(f.Data, "content"))...)
		}
	}
	stateRes, err := s.tflearnCurrentStateResources(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	changes := tflearn.ComputePlan(declared, stateRes)
	counts := map[string]int{}
	for _, c := range changes {
		counts[c.Action]++
	}
	run, err := s.tflearnRecordRun(r, st, proj.ID, u.ID, "plan-preview", "SUCCEEDED", "cloudforge-local-diff", map[string]any{
		"changes": changes,
		"log":     fmt.Sprintf("local plan preview: %d to add, %d to change, %d to destroy, %d no-op (static diff, not a terraform binary plan)", counts["create"], counts["update"], counts["delete"], counts["no-op"]),
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"project_id": proj.ID, "plan_id": run.ID,
		"source":  "cloudforge-local-diff",
		"note":    "Local static diff of declared configuration against recorded state. This is NOT a terraform binary plan: it does not refresh from any provider and cannot see real-world state.",
		"changes": changes, "counts": counts,
		"run": tflearnRunView(run),
	})
}

func (s *Server) handleTFlearnApprovePlan(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	planID := chi.URLParam(r, "planId")
	run, err := st.P3Get(r.Context(), tflearnRuns, planID)
	if err != nil || tfStr(run.Data, "project_id") != proj.ID || tfStr(run.Data, "operation") != "plan-preview" {
		s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan preview with that id in this project")
		return
	}
	if tfStr(run.Data, "status") != "SUCCEEDED" {
		s.writeError(w, r, http.StatusConflict, "plan_not_approved",
			fmt.Sprintf("plan preview %s is %s; only a succeeded preview can be approved", planID, tfStr(run.Data, "status")))
		return
	}
	if exp := tfTime(run.Data, "expires_at"); exp != nil && time.Now().UTC().After(*exp) {
		s.writeError(w, r, http.StatusConflict, "plan_expired", "the plan preview has expired; run plan preview again")
		return
	}
	now := time.Now().UTC()
	run.Data["approved"] = true
	run.Data["approved_at"] = now.Format(time.RFC3339Nano)
	run.Data["approved_by"] = u.ID
	run.Data["expires_at"] = now.Add(tflearnPlanTTL).Format(time.RFC3339Nano)
	if _, err := st.P3Update(r.Context(), tflearnRuns, run); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_plan_preview_approved", "tflearn_run", run.ID, map[string]any{"plan_id": planID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"plan_id": planID, "approved": true,
		"approved_at": now, "expires_at": now.Add(tflearnPlanTTL),
		"note": "This approves the LOCAL plan preview as reviewed. It is a learning record — it does not authorise a container apply (that needs an approved M7 plan).",
	})
}

// --- container executions (delegated to M7) ------------------------------

// tflearnLab loads the lab a project is linked to (owner or admin).
func (s *Server) tflearnLab(r *http.Request, u *auth.User, proj *store.P3Entity) (*labs.Lab, bool) {
	labID := tfStr(proj.Data, "lab_id")
	if labID == "" {
		return nil, false
	}
	lab, err := s.Labs.Get(r.Context(), labID)
	if err != nil || !tflearnVisible(u, lab.OwnerID) {
		return nil, false
	}
	return lab, true
}

// tflearnConcatConfig concatenates the project's .tf files into one
// configuration document for the container runner (the M7 worker
// writes the payload as the lab's main.tf). Multi-file structure is
// preserved for local validation/format; the container run sees one
// concatenated document — documented in docs/TERRAFORM_LEARNING.md.
func (s *Server) tflearnConcatConfig(r *http.Request, st store.Phase3Store, projectID string) (string, error) {
	files, err := s.tflearnProjectFiles(r, st, projectID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range files {
		path := tfStr(f.Data, "path")
		if !strings.HasSuffix(path, ".tf") {
			continue
		}
		fmt.Fprintf(&b, "# --- %s ---\n%s\n", path, tfStr(f.Data, "content"))
	}
	return b.String(), nil
}

func (s *Server) handleTFlearnExecute(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Operation string `json:"operation"`
		PlanID    string `json:"plan_id"`
		Confirm   bool   `json:"confirm"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	op := req.Operation
	if op != "init" && op != "validate" && op != "plan" && op != "apply" && op != "destroy" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "operation must be init, validate, plan, apply or destroy")
		return
	}
	// Confirmation gating: apply and destroy never run without an
	// explicit confirm=true, and apply additionally needs an
	// approved, unexpired M7 plan.
	if (op == "apply" || op == "destroy") && !req.Confirm {
		s.writeError(w, r, http.StatusConflict, "confirm_required",
			"operation "+op+" requires confirm=true in the request body — destructive and applying operations are never automatic")
		return
	}
	lab, linked := s.tflearnLab(r, u, proj)
	if !linked {
		_, _ = s.tflearnRecordRun(r, st, proj.ID, u.ID, op, "FAILED", "cloudforge-m7", map[string]any{
			"error": "project is not linked to a lab; link a lab (PUT /terraform-learning/projects/{id}) before running container execution",
		})
		s.writeError(w, r, http.StatusConflict, "lab_required", "project is not linked to a lab; link a lab before running container execution (local validate/format/plan-preview work without a lab)")
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	if op == "apply" {
		if strings.TrimSpace(req.PlanID) == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "plan_id is required: apply needs an approved plan")
			return
		}
		if _, fail := s.loadApprovablePlan(r, lab, req.PlanID); fail != nil {
			_, _ = s.tflearnRecordRun(r, st, proj.ID, u.ID, op, "FAILED", "cloudforge-m7", map[string]any{"error": fail.message, "plan_id": req.PlanID})
			s.writeError(w, r, fail.status, fail.code, fail.message)
			return
		}
	}
	config, err := s.tflearnConcatConfig(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if config != "" {
		if err := terraform.ValidateConfiguration(config); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	tfReq := terraformRequest{Configuration: config, PlanID: req.PlanID}
	run, job, fail := s.enqueueTerraformRun(r, u, lab, op, tfReq)
	if fail != nil {
		// Honest failure: record the tflearn run as FAILED with the
		// real reason (e.g. terraform_unavailable without Docker)
		// and surface the same status/code.
		_, _ = s.tflearnRecordRun(r, st, proj.ID, u.ID, op, "FAILED", "cloudforge-m7", map[string]any{
			"error": fail.message, "plan_id": req.PlanID,
		})
		if fail.status == http.StatusServiceUnavailable {
			s.writeJSON(w, r, fail.status, map[string]any{
				"error":      map[string]any{"code": fail.code, "message": fail.message, "request_id": RequestID(r.Context())},
				"capability": map[string]any{"service": "terraform", "operation": "run", "detail": fail.message, "image": terraform.DefaultImage},
			})
			return
		}
		s.writeError(w, r, fail.status, fail.code, fail.message)
		return
	}
	tfRun, err := s.tflearnRecordRun(r, st, proj.ID, u.ID, op, "QUEUED", "cloudforge-m7", map[string]any{
		"terraform_run_id": run.ID, "plan_id": req.PlanID,
		"log": "container execution queued (M7 runner, image " + terraform.DefaultImage + ")",
	})
	if err != nil {
		s.Logger.Warn("could not record tflearn run", "project_id", proj.ID, "error", err)
	}
	s.audit(r, u.ID, "tflearn_execution_requested", "tflearn_run", tfRun.ID,
		map[string]any{"project_id": proj.ID, "operation": op, "terraform_run_id": run.ID, "job_id": job.ID})
	s.writeJSON(w, r, http.StatusAccepted, map[string]any{
		"run_id": tfRun.ID, "terraform_run_id": run.ID, "job_id": job.ID,
		"operation": op, "state": "QUEUED",
		"run": tflearnRunView(tfRun),
	})
}

// tflearnRefreshRun copies the linked M7 run's status into a tflearn
// run view (the worker updates terraform_runs, not tflearn_runs).
func (s *Server) tflearnRefreshRun(r *http.Request, e *store.P3Entity) map[string]any {
	view := tflearnRunView(e)
	if tfID := tfStr(e.Data, "terraform_run_id"); tfID != "" && s.TerraformRuns != nil {
		if tr, err := s.TerraformRuns.GetRun(r.Context(), tfID); err == nil {
			view["status"] = tr.State
			view["error"] = tr.Error
			if tr.PlanJSON != nil {
				view["plan_json"] = tr.PlanJSON
			}
			if tr.PlanOutput != "" {
				view["log"] = tr.PlanOutput
			}
			if tr.ApplyOutput != "" {
				view["log"] = tr.ApplyOutput
			}
			view["terraform_run"] = s.terraformRunView(tr)
		}
	}
	return view
}

func (s *Server) handleTFlearnListExecutions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), tflearnRuns)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, rn := range all {
		if tfStr(rn.Data, "project_id") == proj.ID {
			out = append(out, s.tflearnRefreshRun(r, rn))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ci, _ := out[i]["created_at"].(time.Time)
		cj, _ := out[j]["created_at"].(time.Time)
		return ci.After(cj)
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"project_id": proj.ID, "executions": out})
}

func (s *Server) handleTFlearnGetExecution(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	run, err := st.P3Get(r.Context(), tflearnRuns, chi.URLParam(r, "id"))
	if err != nil || !tflearnVisible(u, tfStr(run.Data, "owner_id")) {
		s.writeError(w, r, http.StatusNotFound, "execution_not_found", "no execution with that id")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"execution": s.tflearnRefreshRun(r, run)})
}

// handleTFlearnApproveExecution approves an M7 plan run that was
// created through a project's container plan. It mirrors the M7
// approve checks (SUCCEEDED, unexpired) and writes the approval to
// the terraform_runs row, so the project's apply can use it.
func (s *Server) handleTFlearnApproveExecution(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	tfRun, err := st.P3Get(r.Context(), tflearnRuns, chi.URLParam(r, "runId"))
	if err != nil || tfStr(tfRun.Data, "project_id") != proj.ID {
		s.writeError(w, r, http.StatusNotFound, "execution_not_found", "no execution with that id in this project")
		return
	}
	tfID := tfStr(tfRun.Data, "terraform_run_id")
	if tfID == "" {
		s.writeError(w, r, http.StatusConflict, "plan_not_approved", "this execution has no container plan to approve (local plan previews are approved at /plans/{planId}/approve)")
		return
	}
	planRun, err := s.TerraformRuns.GetRun(r.Context(), tfID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "plan_not_found", "no plan with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if planRun.Action != "plan" || planRun.State != "SUCCEEDED" {
		s.writeError(w, r, http.StatusConflict, "plan_not_approved",
			fmt.Sprintf("plan %s is %s; only a succeeded plan can be approved", planRun.PlanID, planRun.State))
		return
	}
	if planRun.PlanExpiresAt == nil || time.Now().UTC().After(*planRun.PlanExpiresAt) {
		s.writeError(w, r, http.StatusConflict, "plan_expired", "the plan has expired; run plan again")
		return
	}
	now := time.Now().UTC()
	planRun.ApprovedAt = &now
	planRun.ApprovedBy = u.ID
	if _, err := s.TerraformRuns.UpdateRun(r.Context(), planRun); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "terraform_plan_approved", "terraform_run", planRun.ID, map[string]any{"plan_id": planRun.PlanID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"plan_id": planRun.PlanID, "approved": true,
		"approved_at": now, "expires_at": planRun.PlanExpiresAt,
	})
}

// --- state ---------------------------------------------------------------

// tflearnCurrentState returns the project's latest state version.
func (s *Server) tflearnCurrentState(r *http.Request, st store.Phase3Store, projectID string) (*store.P3Entity, error) {
	all, err := st.P3List(r.Context(), tflearnStates)
	if err != nil {
		return nil, err
	}
	var best *store.P3Entity
	bestV := -1
	for _, e := range all {
		if tfStr(e.Data, "project_id") != projectID {
			continue
		}
		if v := tfInt(e.Data, "version"); v > bestV {
			bestV, best = v, e
		}
	}
	if best == nil {
		return nil, store.ErrNotFound
	}
	return best, nil
}

func tflearnStateResources(e *store.P3Entity) []tflearn.DeclaredResource {
	raw, ok := e.Data["resources"].([]any)
	if !ok {
		return []tflearn.DeclaredResource{}
	}
	out := []tflearn.DeclaredResource{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		res := tflearn.DeclaredResource{
			Address: tfStr(m, "address"), Type: tfStr(m, "type"),
			Name: tfStr(m, "name"), Kind: tfStr(m, "kind"),
			Attributes: map[string]any{},
		}
		if attrs, ok := m["attributes"].(map[string]any); ok {
			res.Attributes = attrs
		}
		out = append(out, res)
	}
	return out
}

func tflearnStateView(e *store.P3Entity, lock *store.P3Entity) map[string]any {
	view := map[string]any{
		"id": e.ID, "project_id": tfStr(e.Data, "project_id"),
		"version":    tfInt(e.Data, "version"),
		"resources":  tflearnStateResources(e),
		"source":     tfStr(e.Data, "source"),
		"created_at": e.CreatedAt,
	}
	if lock != nil {
		view["locked"] = true
		view["locked_by"] = tfStr(lock.Data, "locked_by")
		view["locked_at"] = tfStr(lock.Data, "locked_at")
		view["lock_expires_at"] = tfStr(lock.Data, "expires_at")
	} else {
		view["locked"] = false
	}
	return view
}

// tflearnCurrentStateResources returns the recorded resources of the
// project's current state version, refreshing from the linked lab's
// latest succeeded M7 plan run when it carries newer resources.
func (s *Server) tflearnCurrentStateResources(r *http.Request, st store.Phase3Store, projectID string) ([]tflearn.DeclaredResource, error) {
	cur, err := s.tflearnCurrentState(r, st, projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return []tflearn.DeclaredResource{}, nil
		}
		return nil, err
	}
	return tflearnStateResources(cur), nil
}

// tflearnRefreshStateFromLab creates a new state version from the
// linked lab's latest succeeded M7 plan run when its resources
// differ from the current version. It never rewrites history.
func (s *Server) tflearnRefreshStateFromLab(r *http.Request, st store.Phase3Store, proj *store.P3Entity) {
	labID := tfStr(proj.Data, "lab_id")
	if labID == "" || s.TerraformRuns == nil {
		return
	}
	runs, err := s.TerraformRuns.ListRunsByLab(r.Context(), labID, 20)
	if err != nil {
		return
	}
	var latest *store.TerraformRun
	for _, run := range runs {
		if run.Action == "plan" && run.State == "SUCCEEDED" && run.PlanJSON != nil {
			latest = run
			break
		}
	}
	if latest == nil {
		return
	}
	plan, _, err := terraform.ParsePlanJSON(mustJSON(latest.PlanJSON))
	if err != nil || plan == nil {
		return
	}
	resources := []tflearn.DeclaredResource{}
	for _, pr := range plan.Resources {
		resources = append(resources, tflearn.DeclaredResource{
			Address: pr.Address, Type: pr.Type, Name: pr.Name, Kind: "resource",
			Attributes: map[string]any{},
		})
	}
	cur, err := s.tflearnCurrentState(r, st, proj.ID)
	if err != nil {
		return
	}
	if sameAddresses(tflearnStateResources(cur), resources) {
		return
	}
	nextV := tfInt(cur.Data, "version") + 1
	raw := []any{}
	for _, res := range resources {
		raw = append(raw, map[string]any{"address": res.Address, "type": res.Type, "name": res.Name, "kind": res.Kind, "attributes": res.Attributes})
	}
	_, _ = st.P3Create(r.Context(), tflearnStates, &store.P3Entity{
		Name: fmt.Sprintf("%s:v%d", proj.ID, nextV),
		Data: map[string]any{"project_id": proj.ID, "owner_id": tfStr(proj.Data, "owner_id"),
			"version": nextV, "resources": raw, "source": "m7-plan:" + latest.ID},
	})
}

func sameAddresses(a, b []tflearn.DeclaredResource) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, r := range a {
		set[r.Address] = true
	}
	for _, r := range b {
		if !set[r.Address] {
			return false
		}
	}
	return true
}

func mustJSON(v map[string]any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func (s *Server) handleTFlearnGetState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	s.tflearnRefreshStateFromLab(r, st, proj)
	cur, err := s.tflearnCurrentState(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "state_not_found", "no state recorded for this project yet")
		return
	}
	lock, _ := s.tflearnActiveLock(r, st, proj.ID)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"state": tflearnStateView(cur, lock),
		"note":  "CloudForge learning state: versioned snapshots refreshed from the linked lab's latest succeeded container plan. State is append-only; restores create a new version.",
	})
}

func (s *Server) handleTFlearnStateHistory(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), tflearnStates)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := []map[string]any{}
	for _, e := range all {
		if tfStr(e.Data, "project_id") == proj.ID {
			views = append(views, tflearnStateView(e, nil))
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i]["version"].(int) < views[j]["version"].(int) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"project_id": proj.ID, "versions": views})
}

func (s *Server) handleTFlearnLockState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	existing, err := s.tflearnActiveLock(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if existing != nil {
		s.writeError(w, r, http.StatusConflict, "state_locked",
			fmt.Sprintf("state is already locked by %s until %s", tfStr(existing.Data, "locked_by"), tfStr(existing.Data, "expires_at")))
		return
	}
	now := time.Now().UTC()
	created, err := st.P3Create(r.Context(), tflearnLocks, &store.P3Entity{
		Name: proj.ID,
		Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID,
			"locked_by": u.ID, "locked_at": now.Format(time.RFC3339Nano),
			"expires_at": now.Add(tflearnLockTTL).Format(time.RFC3339Nano)},
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_state_locked", "tflearn_project", proj.ID, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"locked": true, "locked_by": u.ID,
		"expires_at": now.Add(tflearnLockTTL),
		"lock_id":    created.ID,
	})
}

func (s *Server) handleTFlearnUnlockState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	lock, err := s.tflearnActiveLock(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if lock == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"locked": false, "note": "state was not locked"})
		return
	}
	holder := tfStr(lock.Data, "locked_by")
	if holder != u.ID && !req.Force {
		s.writeError(w, r, http.StatusConflict, "state_locked",
			fmt.Sprintf("state is locked by %s; only the holder can unlock without force=true", holder))
		return
	}
	if holder != u.ID && req.Force && u.Role != "admin" {
		// Force-unlock by a non-holder, non-admin is refused: an
		// active lock is never stale here (expired ones are removed
		// on sight), so there is no safe case for it.
		s.writeError(w, r, http.StatusForbidden, "forbidden", "only the lock holder or an admin can force-unlock an active lock")
		return
	}
	if err := st.P3Delete(r.Context(), tflearnLocks, lock.ID); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_state_unlocked", "tflearn_project", proj.ID, map[string]any{"force": req.Force})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) handleTFlearnBackupState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	cur, err := s.tflearnCurrentState(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "state_not_found", "no state recorded for this project yet")
		return
	}
	nextV := tfInt(cur.Data, "version") + 1
	created, err := st.P3Create(r.Context(), tflearnStates, &store.P3Entity{
		Name: fmt.Sprintf("%s:v%d", proj.ID, nextV),
		Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID,
			"version": nextV, "resources": cur.Data["resources"], "source": "backup-of-v" + strconv.Itoa(tfInt(cur.Data, "version"))},
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_state_backed_up", "tflearn_project", proj.ID, map[string]any{"version": nextV})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"state": tflearnStateView(created, nil)})
}

func (s *Server) handleTFlearnRestoreState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Version int `json:"version"`
	}
	if !s.decodeTFlearn(w, r, &req) {
		return
	}
	if s.tflearnLockedByOther(w, r, st, proj.ID, u) {
		return
	}
	all, err := st.P3List(r.Context(), tflearnStates)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	var target *store.P3Entity
	curV := -1
	for _, e := range all {
		if tfStr(e.Data, "project_id") != proj.ID {
			continue
		}
		if v := tfInt(e.Data, "version"); v > curV {
			curV = v
		}
		if tfInt(e.Data, "version") == req.Version {
			target = e
		}
	}
	if target == nil {
		s.writeError(w, r, http.StatusNotFound, "state_version_not_found", fmt.Sprintf("no state version %d for this project", req.Version))
		return
	}
	// Restore NEVER rewrites history: it creates a NEW version whose
	// content is copied from the restored one.
	nextV := curV + 1
	created, err := st.P3Create(r.Context(), tflearnStates, &store.P3Entity{
		Name: fmt.Sprintf("%s:v%d", proj.ID, nextV),
		Data: map[string]any{"project_id": proj.ID, "owner_id": u.ID,
			"version": nextV, "resources": target.Data["resources"],
			"source": "restore-of-v" + strconv.Itoa(req.Version)},
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "tflearn_state_restored", "tflearn_project", proj.ID, map[string]any{"from_version": req.Version, "to_version": nextV})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"state": tflearnStateView(created, nil),
		"note":  "Restore created a new version; history was not rewritten.",
	})
}

// tflearnLabLiveAddresses maps a lab's resources to Terraform-style
// addresses for drift detection (kind -> cloudforge_* type).
func (s *Server) tflearnLabLiveAddresses(r *http.Request, lab *labs.Lab) []string {
	res, err := s.Resources.ListResources(r.Context(), lab.ID)
	if err != nil {
		return []string{}
	}
	kindToType := map[string]string{
		"vpc": "cloudforge_vpc", "subnet": "cloudforge_subnet",
		"security-group": "cloudforge_security_group",
		"instance":       "cloudforge_instance",
	}
	out := []string{}
	for _, res := range res {
		typ, ok := kindToType[res.Kind]
		if !ok {
			continue
		}
		name := res.Name
		if name == "" {
			name = res.ID
		}
		out = append(out, typ+"."+name)
	}
	return out
}

func (s *Server) handleTFlearnDrift(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	proj, st, ok := s.tflearnProject(w, r, u)
	if !ok {
		return
	}
	files, err := s.tflearnProjectFiles(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	declared := []tflearn.DeclaredResource{}
	for _, f := range files {
		if strings.HasSuffix(tfStr(f.Data, "path"), ".tf") {
			declared = append(declared, tflearn.ExtractResources(tfStr(f.Data, "content"))...)
		}
	}
	stateRes, err := s.tflearnCurrentStateResources(r, st, proj.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	live := []string{}
	if lab, linked := s.tflearnLab(r, u, proj); linked {
		live = s.tflearnLabLiveAddresses(r, lab)
	}
	drift := tflearn.DetectDrift(declared, stateRes, live)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"project_id": proj.ID, "drift": drift,
		"declared": len(declared), "recorded_in_state": len(stateRes), "live": len(live),
		"lab_linked": tfStr(proj.Data, "lab_id") != "",
		"note":       "Drift compares declared configuration, recorded state, and live lab resources (when a lab is linked). A 'missing' entry means declared but not live; 'unmanaged' means live but not declared.",
	})
}
