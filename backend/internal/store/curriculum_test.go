// Milestone 8 store tests: seed idempotency (memory + Postgres),
// server-side grading, double-XP prevention, and break-fix solve
// idempotency.
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/migrations"
)

func curriculumDir() string {
	// internal/store -> repo root curriculum/.
	return filepath.Join("..", "..", "..", "curriculum")
}

func seedMemory(t *testing.T) (*MemoryCourseStore, *MemoryCurriculumStore, *curriculum.SeedReport) {
	t.Helper()
	courses := NewMemoryCourseStore()
	curr := NewMemoryCurriculumStore(courses)
	rep, err := curriculum.SeedDir(context.Background(), curr, curriculumDir())
	if err != nil {
		t.Fatalf("SeedDir: %v", err)
	}
	return courses, curr, rep
}

func TestSeedIdempotentMemory(t *testing.T) {
	_, curr, rep := seedMemory(t)
	if rep.Tracks != 16 || rep.Lessons != 88 || rep.Questions != 94 || rep.Scenarios != 10 || rep.Achievements != 19 {
		t.Fatalf("seed report: %+v", rep)
	}
	if rep.LabDefinitions != 16*2+10 {
		t.Fatalf("lab definitions: got %d, want %d", rep.LabDefinitions, 16*2+10)
	}
	tracks, err := curr.ListTracks(context.Background())
	// Phase 1 fix (16-vs-19): tracks are the 16 seeded curriculum
	// tracks only. The 3 legacy M1 courses (vpc-fundamentals,
	// iam-security, ec2-basics) are courses, not tracks, and must not
	// be listed here — they still appear in GET /api/v1/courses (17).
	if err != nil || len(tracks) != 16 {
		t.Fatalf("ListTracks after seed: %d tracks err=%v", len(tracks), err)
	}
	// Re-seed: counts must be identical and no duplicate tracks.
	rep2, err := curriculum.SeedDir(context.Background(), curr, curriculumDir())
	if err != nil {
		t.Fatalf("second SeedDir: %v", err)
	}
	if *rep != *rep2 {
		t.Fatalf("seed is not idempotent: %+v vs %+v", rep, rep2)
	}
	tracks2, _ := curr.ListTracks(context.Background())
	if len(tracks2) != len(tracks) {
		t.Fatalf("re-seed changed track count: %d -> %d", len(tracks), len(tracks2))
	}
	// Lesson UUIDs survive a re-seed (progress keeps working).
	tr, err := curr.GetTrack(context.Background(), "networking")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if len(tr.Modules) != 1 || len(tr.Modules[0].Lessons) != 6 {
		t.Fatalf("networking track modules/lessons: %+v", tr.Modules)
	}
	if tr.Modules[0].Lessons[0].Theory == "" || len(tr.Modules[0].Lessons[0].KeyPoints) == 0 {
		t.Fatal("lesson theory/key points were not seeded")
	}
	if tr.GuidedLab.ID != "lab-networking-vpc" || len(tr.GuidedLab.ValidationRules) == 0 {
		t.Fatalf("guided lab not seeded: %+v", tr.GuidedLab)
	}
	if len(tr.Assessment) != 6 {
		t.Fatalf("assessment questions: %d", len(tr.Assessment))
	}
}

func TestGradingAndDoubleXP(t *testing.T) {
	courses, curr, _ := seedMemory(t)
	ctx := context.Background()
	userID := ids.New()
	if err := courses.Enroll(ctx, userID, "networking"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	// Grading is server-side: all-wrong answers score 0 and award nothing.
	qs, err := curr.GetAssessment(ctx, "networking")
	if err != nil {
		t.Fatalf("GetAssessment: %v", err)
	}
	wrong := map[int]int{}
	for i, q := range qs {
		wrong[i] = (q.CorrectAnswer + 1) % 4
	}
	res, err := curr.SubmitAssessment(ctx, userID, "networking", wrong)
	if err != nil {
		t.Fatalf("submit wrong: %v", err)
	}
	if res.Score != 0 || res.Passed || res.XPAwarded != 0 {
		t.Fatalf("wrong answers: %+v", res)
	}
	if res.PerQuestion[0].Correct || res.PerQuestion[0].Explanation == "" {
		t.Fatalf("per-question grading: %+v", res.PerQuestion[0])
	}
	// All-correct passes (>= 80%) and awards 20 XP once.
	right := map[int]int{}
	for i, q := range qs {
		right[i] = q.CorrectAnswer
	}
	res, err = curr.SubmitAssessment(ctx, userID, "networking", right)
	if err != nil {
		t.Fatalf("submit right: %v", err)
	}
	if res.Score != 100 || !res.Passed || res.XPAwarded != curriculum.AssessmentPassXP {
		t.Fatalf("right answers: %+v", res)
	}
	// A second passing submission awards no more XP.
	res, err = curr.SubmitAssessment(ctx, userID, "networking", right)
	if err != nil {
		t.Fatalf("submit right again: %v", err)
	}
	if res.XPAwarded != 0 {
		t.Fatalf("second pass awarded %d XP", res.XPAwarded)
	}
	// Lesson completion: 10 XP + 10 first-lesson bonus once; a
	// re-completion awards 0.
	courseID, lessonUUID, err := curr.FindLesson(ctx, "cloud-networking")
	if err != nil || courseID != "networking" {
		t.Fatalf("FindLesson: %q %q err=%v", courseID, lessonUUID, err)
	}
	prog, awarded, err := courses.CompleteLesson(ctx, userID, courseID, lessonUUID)
	if err != nil {
		t.Fatalf("CompleteLesson: %v", err)
	}
	if awarded != 20 {
		t.Fatalf("first lesson awarded %d, want 20", awarded)
	}
	if _, awarded, err := courses.CompleteLesson(ctx, userID, courseID, lessonUUID); err != nil || awarded != 0 {
		t.Fatalf("re-completion awarded %d err=%v", awarded, err)
	}
	if prog.XP != 20+20 {
		t.Fatalf("total XP %d, want 40 (20 lesson + 20 assessment)", prog.XP)
	}
	// Completing the whole track pays the track-completion bonus:
	// total lesson XP for networking must equal its xp_reward (250).
	tr, _ := curr.GetTrack(ctx, "networking")
	for _, m := range tr.Modules {
		for _, l := range m.Lessons {
			if l.Slug == "cloud-networking" {
				continue
			}
			if _, _, err := courses.CompleteLesson(ctx, userID, "networking", l.DBID); err != nil {
				t.Fatalf("complete %s: %v", l.Slug, err)
			}
		}
	}
	stats, err := curr.UserStats(ctx, userID)
	if err != nil {
		t.Fatalf("UserStats: %v", err)
	}
	// Lesson XP: 250 track reward + 10 first-lesson bonus; plus 20 assessment XP.
	if stats.TotalXP != 250+10+20 {
		t.Fatalf("total XP after track completion: %d, want 280", stats.TotalXP)
	}
	if !stats.HasCompletedTrack("networking") {
		t.Fatalf("networking should be complete: %+v", stats.CompletedTracks)
	}
}

func TestBreakFixSolveIdempotentMemory(t *testing.T) {
	_, curr, _ := seedMemory(t)
	ctx := context.Background()
	userID, labID := ids.New(), ids.New()
	attempt, err := curr.StartBreakFix(ctx, userID, labID, "missing-igw-route", "fault_pending")
	if err != nil {
		t.Fatalf("StartBreakFix: %v", err)
	}
	if attempt.FaultStatus != "fault_pending" || attempt.XPReward != 120 {
		t.Fatalf("attempt: %+v", attempt)
	}
	// Starting again replays the same attempt.
	again, err := curr.StartBreakFix(ctx, userID, labID, "missing-igw-route", "injected")
	if err != nil || again.FaultStatus != "fault_pending" {
		t.Fatalf("re-start changed the attempt: %+v err=%v", again, err)
	}
	solved, xp, err := curr.MarkBreakFixSolved(ctx, userID, labID)
	if err != nil || !solved.Solved || xp != 120 {
		t.Fatalf("solve: %+v xp=%d err=%v", solved, xp, err)
	}
	// Solving again awards nothing.
	if _, xp, err := curr.MarkBreakFixSolved(ctx, userID, labID); err != nil || xp != 0 {
		t.Fatalf("second solve awarded %d err=%v", xp, err)
	}
	stats, err := curr.UserStats(ctx, userID)
	if err != nil {
		t.Fatalf("UserStats: %v", err)
	}
	if stats.BreakFixSolved != 1 || stats.TotalXP != 120 {
		t.Fatalf("stats after solve: %+v", stats)
	}
	if _, err := curr.StartBreakFix(ctx, userID, ids.New(), "no-such-scenario", "fault_pending"); err == nil {
		t.Fatal("unknown scenario should fail")
	}
}

func TestPostgresCurriculumIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is unset; integration test skipped (no PostgreSQL available)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL unreachable (%v); integration test skipped", err)
	}
	defer db.Close()
	if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	curr := NewPostgresCurriculumStore(db)
	rep, err := curriculum.SeedDir(ctx, curr, curriculumDir())
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rep.Tracks != 16 || rep.Lessons != 88 || rep.Questions != 94 || rep.Scenarios != 10 || rep.Achievements != 19 {
		t.Fatalf("seed report: %+v", rep)
	}
	// Idempotent: a second seed reports the same counts and the
	// track/lesson row counts are unchanged.
	var tracksBefore, lessonsBefore int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM courses").Scan(&tracksBefore); err != nil {
		t.Fatalf("count courses: %v", err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM lessons").Scan(&lessonsBefore); err != nil {
		t.Fatalf("count lessons: %v", err)
	}
	rep2, err := curriculum.SeedDir(ctx, curr, curriculumDir())
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if *rep != *rep2 {
		t.Fatalf("seed not idempotent: %+v vs %+v", rep, rep2)
	}
	var tracksAfter, lessonsAfter int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM courses").Scan(&tracksAfter)
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM lessons").Scan(&lessonsAfter)
	if tracksAfter != tracksBefore || lessonsAfter != lessonsBefore {
		t.Fatalf("re-seed changed row counts: courses %d->%d lessons %d->%d",
			tracksBefore, tracksAfter, lessonsBefore, lessonsAfter)
	}
	// Grading + double-XP prevention against Postgres.
	suffix := ids.New()[:8]
	signer := auth.NewStore("integration-secret-m8")
	users := NewPostgresUserStore(db, signer)
	u, err := users.Register(fmt.Sprintf("it-m8-%s@example.com", suffix), "Integration M8", "correct-horse")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	courses := NewPostgresCourseStore(db)
	if err := courses.Enroll(ctx, u.ID, "networking"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	courseID, lessonUUID, err := curr.FindLesson(ctx, "cloud-networking")
	if err != nil || courseID != "networking" {
		t.Fatalf("FindLesson: %q %q err=%v", courseID, lessonUUID, err)
	}
	_, awarded, err := courses.CompleteLesson(ctx, u.ID, courseID, lessonUUID)
	if err != nil || awarded != 20 {
		t.Fatalf("first lesson: awarded=%d err=%v", awarded, err)
	}
	if _, awarded, err := courses.CompleteLesson(ctx, u.ID, courseID, lessonUUID); err != nil || awarded != 0 {
		t.Fatalf("re-completion awarded %d err=%v", awarded, err)
	}
	qs, err := curr.GetAssessment(ctx, "networking")
	if err != nil {
		t.Fatalf("GetAssessment: %v", err)
	}
	right := map[int]int{}
	for i, q := range qs {
		right[i] = q.CorrectAnswer
	}
	res, err := curr.SubmitAssessment(ctx, u.ID, "networking", right)
	if err != nil || !res.Passed || res.XPAwarded != curriculum.AssessmentPassXP {
		t.Fatalf("assessment: %+v err=%v", res, err)
	}
	res, err = curr.SubmitAssessment(ctx, u.ID, "networking", right)
	if err != nil || res.XPAwarded != 0 {
		t.Fatalf("second pass awarded %d err=%v", res.XPAwarded, err)
	}
	stats, err := curr.UserStats(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserStats: %v", err)
	}
	if stats.LessonsCompleted != 1 || stats.TotalXP != 40 || stats.StreakDays < 1 {
		t.Fatalf("stats: %+v", stats)
	}
	// Break-fix attempt + solve is idempotent in Postgres too.
	labStore := NewPostgresLabStore(db)
	lab, _, err := labStore.Create(ctx, u.ID, "lab-networking-vpc", "it m8 lab", "it-m8-"+suffix)
	if err != nil {
		t.Fatalf("create lab: %v", err)
	}
	attempt, err := curr.StartBreakFix(ctx, u.ID, lab.ID, "missing-igw-route", "fault_pending")
	if err != nil || attempt.FaultStatus != "fault_pending" {
		t.Fatalf("start break-fix: %+v err=%v", attempt, err)
	}
	solved, xp, err := curr.MarkBreakFixSolved(ctx, u.ID, lab.ID)
	if err != nil || !solved.Solved || xp != 120 {
		t.Fatalf("solve: %+v xp=%d err=%v", solved, xp, err)
	}
	if _, xp, err := curr.MarkBreakFixSolved(ctx, u.ID, lab.ID); err != nil || xp != 0 {
		t.Fatalf("second solve awarded %d err=%v", xp, err)
	}
}

func TestListTracksExcludesLegacyCourses(t *testing.T) {
	courses, curr, rep := seedMemory(t)
	if rep.Tracks != 16 {
		t.Fatalf("seed report tracks: %d, want 16", rep.Tracks)
	}
	tracks, err := curr.ListTracks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != rep.Tracks {
		t.Fatalf("ListTracks = %d, seed report = %d: the two must agree", len(tracks), rep.Tracks)
	}
	for _, tr := range tracks {
		switch tr.ID {
		case "vpc-fundamentals", "iam-security", "ec2-basics":
			t.Fatalf("ListTracks must not include legacy M1 course %q (it is a course, not a track)", tr.ID)
		}
	}
	// The courses list is a different surface and still holds all 19:
	// 3 legacy M1 courses + 16 curriculum tracks.
	all, err := courses.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 19 {
		t.Fatalf("courses list = %d, want 19 (3 legacy + 16 tracks)", len(all))
	}
}
