// Milestone 8 curriculum handlers: tracks, enrollment, lesson
// completion, assessments, user progress/achievements, and the
// admin curriculum re-seed endpoint.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/cloudforge/cloudforge/backend/internal/achievements"
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

// evaluateAchievements loads the seeded achievement definitions and
// the user's stats, awards every newly-met achievement (with its XP,
// via the store), and returns the newly awarded ones. A store error
// is logged and yields no awards — it never fails the user's
// request.
func (s *Server) evaluateAchievements(r *http.Request, userID string) []*curriculum.Achievement {
	if s.Curriculum == nil {
		return nil
	}
	defs, err := s.Curriculum.ListAchievements(r.Context())
	if err != nil {
		s.Logger.Warn("list achievements failed", "error", err)
		return nil
	}
	stats, err := s.Curriculum.UserStats(r.Context(), userID)
	if err != nil {
		s.Logger.Warn("user stats failed", "error", err)
		return nil
	}
	ids := achievements.Evaluate(defs, stats)
	if len(ids) == 0 {
		return nil
	}
	awarded, err := s.Curriculum.AwardAchievements(r.Context(), userID, ids)
	if err != nil {
		s.Logger.Warn("award achievements failed", "error", err)
		return nil
	}
	return awarded
}

func trackDetailView(t *curriculum.Track, prog *store.Progress) map[string]any {
	modules := make([]any, 0, len(t.Modules))
	for _, m := range t.Modules {
		lessons := make([]any, 0, len(m.Lessons))
		for _, l := range m.Lessons {
			lessons = append(lessons, map[string]any{
				"id": l.Slug, "db_id": l.DBID, "title": l.Title, "theory": l.Theory,
				"key_points": l.KeyPoints, "diagram": l.Diagram,
			})
		}
		modules = append(modules, map[string]any{"id": m.ID, "title": m.Title, "lessons": lessons})
	}
	publicQuestions := make([]any, 0, len(t.Assessment))
	for _, q := range t.Assessment {
		publicQuestions = append(publicQuestions, q.PublicQuestion())
	}
	v := map[string]any{
		"id": t.ID, "title": t.Title, "description": t.Description,
		"difficulty": t.Difficulty, "estimated_hours": t.EstimatedHours,
		"learning_objectives": t.LearningObjectives, "modules": modules,
		"guided_lab": map[string]any{
			"lab_definition_id": t.GuidedLab.ID, "title": t.GuidedLab.Title,
			"tasks": t.GuidedLab.Tasks, "validation_rules": t.GuidedLab.ValidationRules,
		},
		"challenge": map[string]any{
			"lab_definition_id": t.Challenge.ID, "title": t.Challenge.Title,
			"task": t.Challenge.Description, "validation_rules": t.Challenge.ValidationRules,
		},
		"assessment": publicQuestions, "xp_reward": t.XPReward, "prerequisites": t.Prerequisites,
		"lesson_count": t.LessonCount(),
	}
	if prog != nil {
		v["progress"] = prog
	}
	return v
}

func (s *Server) handleListTracks(w http.ResponseWriter, r *http.Request) {
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	tracks, err := s.Curriculum.ListTracks(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Progress per track when the caller is authenticated; the list
	// is public otherwise.
	var userID string
	if u, ok := s.currentUser(r); ok {
		userID = u.ID
	}
	out := make([]any, 0, len(tracks))
	for _, t := range tracks {
		v := map[string]any{
			"id": t.ID, "title": t.Title, "description": t.Description,
			"difficulty": t.Difficulty, "estimated_hours": t.EstimatedHours,
			"xp_reward": t.XPReward, "lesson_count": t.LessonCount, "module_count": t.ModuleCount,
		}
		if userID != "" {
			if prog, err := s.Courses.GetProgress(r.Context(), userID, t.ID); err == nil {
				pct := 0
				if prog.TotalLessons > 0 {
					pct = prog.CompletedLessons * 100 / prog.TotalLessons
				}
				v["enrolled"] = prog.Enrolled
				v["progress_percent"] = pct
				v["completed_lessons"] = prog.CompletedLessons
			}
		}
		out = append(out, v)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tracks": out})
}

func (s *Server) handleGetTrack(w http.ResponseWriter, r *http.Request) {
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	t, err := s.Curriculum.GetTrack(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "track_not_found", "no track with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	var prog *store.Progress
	if u, ok := s.currentUser(r); ok {
		if p, err := s.Courses.GetProgress(r.Context(), u.ID, id); err == nil {
			prog = p
		}
	}
	s.writeJSON(w, r, http.StatusOK, trackDetailView(t, prog))
}

func (s *Server) handleEnrollTrack(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	courseID := chi.URLParam(r, "id")
	if err := s.Courses.Enroll(r.Context(), u.ID, courseID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "track_not_found", "no track with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.audit(r, u.ID, "track_enrolled", "course", courseID, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"enrolled": true, "track_id": courseID})
}

func (s *Server) handleCompleteCurriculumLesson(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	lessonID := chi.URLParam(r, "lessonId")
	courseID, uuid, err := s.Curriculum.FindLesson(r.Context(), lessonID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "lesson_not_found", "no lesson with that id")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	prog, awarded, err := s.Courses.CompleteLesson(r.Context(), u.ID, courseID, uuid)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, r, http.StatusNotFound, "not_found", err.Error())
		case errors.Is(err, store.ErrNotEnrolled):
			s.writeError(w, r, http.StatusConflict, "not_enrolled", "enroll in the track before completing lessons")
		default:
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	var newly []*curriculum.Achievement
	if awarded > 0 {
		s.audit(r, u.ID, "lesson_completed", "lesson", uuid, map[string]any{"course_id": courseID, "xp_awarded": awarded})
		newly = s.evaluateAchievements(r, u.ID)
	}
	if newly == nil {
		newly = []*curriculum.Achievement{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"progress": prog, "xp_awarded": awarded, "course_id": courseID,
		"achievements_awarded": newly,
	})
}

type assessmentSubmitBody struct {
	Answers map[int]int `json:"answers"`
}

func (s *Server) handleSubmitAssessment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	trackID := chi.URLParam(r, "trackId")
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "could not read request body")
		return
	}
	answers, ok := parseAssessmentAnswers(raw)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "body must carry answers as an object {\"0\": 1, ...} or a list [{\"question\": 0, \"answer\": 1}]")
		return
	}
	// Grading is server-side: the store grades `answers` against
	// the correct answers in the database; nothing the client
	// sends about correctness is trusted.
	res, err := s.Curriculum.SubmitAssessment(r.Context(), u.ID, trackID, answers)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, r, http.StatusNotFound, "track_not_found", "no assessment for that track")
		case errors.Is(err, store.ErrNotEnrolled):
			s.writeError(w, r, http.StatusConflict, "not_enrolled", "enroll in the track before taking its assessment")
		default:
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	var newly []*curriculum.Achievement
	if res.Passed {
		newly = s.evaluateAchievements(r, u.ID)
	}
	if newly == nil {
		newly = []*curriculum.Achievement{}
	}
	s.audit(r, u.ID, "assessment_submitted", "course", trackID, map[string]any{"score": res.Score, "passed": res.Passed, "xp_awarded": res.XPAwarded})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"assessment": res, "achievements_awarded": newly,
	})
}

// parseAssessmentAnswers accepts both {"0": 1} and
// [{"question": 0, "answer": 1}] shapes.
func parseAssessmentAnswers(raw []byte) (map[int]int, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var body struct {
		Answers json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Answers) == 0 {
		return nil, false
	}
	var byIndex map[string]int
	if err := json.Unmarshal(body.Answers, &byIndex); err == nil {
		out := make(map[int]int, len(byIndex))
		for k, v := range byIndex {
			idx, err := strconv.Atoi(k)
			if err != nil {
				return nil, false
			}
			out[idx] = v
		}
		return out, true
	}
	var list []struct {
		Question int `json:"question"`
		Answer   int `json:"answer"`
	}
	if err := json.Unmarshal(body.Answers, &list); err != nil {
		return nil, false
	}
	out := make(map[int]int, len(list))
	for _, item := range list {
		out[item.Question] = item.Answer
	}
	return out, true
}

func (s *Server) handleMyProgress(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	tracks := []*curriculum.TrackSummary{}
	if s.Curriculum != nil {
		if list, err := s.Curriculum.ListTracks(r.Context()); err == nil {
			tracks = list
		}
	}
	if len(tracks) == 0 {
		if list, err := s.Courses.List(r.Context()); err == nil {
			for _, c := range list {
				tracks = append(tracks, &curriculum.TrackSummary{ID: c.ID, Title: c.Title})
			}
		}
	}
	perTrack := make([]any, 0, len(tracks))
	for _, t := range tracks {
		prog, err := s.Courses.GetProgress(r.Context(), u.ID, t.ID)
		if err != nil {
			continue
		}
		pct := 0
		if prog.TotalLessons > 0 {
			pct = prog.CompletedLessons * 100 / prog.TotalLessons
		}
		perTrack = append(perTrack, map[string]any{
			"track_id": t.ID, "title": t.Title, "enrolled": prog.Enrolled,
			"total_lessons": prog.TotalLessons, "completed_lessons": prog.CompletedLessons,
			"completed_lesson_ids": prog.CompletedLessonIDs, "percent": pct,
			"completed_at": prog.CompletedAt,
		})
	}
	totalXP, streak := 0, 0
	if s.Curriculum != nil {
		if stats, err := s.Curriculum.UserStats(r.Context(), u.ID); err == nil {
			totalXP, streak = stats.TotalXP, stats.StreakDays
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"user_id": u.ID, "enrollments": perTrack, "total_xp": totalXP,
		"streak_days": streak,
	})
}

func (s *Server) handleMyAchievements(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	defs, err := s.Curriculum.ListAchievements(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	stats, err := s.Curriculum.UserStats(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	earned := make([]any, 0)
	locked := make([]any, 0)
	for _, a := range defs {
		v := map[string]any{
			"id": a.ID, "title": a.Title, "description": a.Description,
			"xp": a.XP, "trigger": a.Trigger,
		}
		if stats.Earned != nil && stats.Earned[a.ID] {
			v["earned"] = true
			earned = append(earned, v)
		} else {
			v["earned"] = false
			v["locked_reason"] = achievements.LockedReason(a, stats)
			locked = append(locked, v)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"earned": earned, "locked": locked})
}

func (s *Server) handleSeedCurriculum(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if s.Curriculum == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_unavailable", "curriculum store is not configured")
		return
	}
	dir, err := curriculum.ResolveDir(s.CurriculumDir)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "curriculum_dir_missing", err.Error())
		return
	}
	rep, err := curriculum.SeedDir(r.Context(), s.Curriculum, dir)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "seed_failed", err.Error())
		return
	}
	s.audit(r, admin.ID, "curriculum_seeded", "curriculum", "", map[string]any{
		"tracks": rep.Tracks, "lessons": rep.Lessons, "scenarios": rep.Scenarios,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"seeded": rep, "dir": dir})
}
