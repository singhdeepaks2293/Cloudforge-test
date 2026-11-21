package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
)

func TestMemoryCourseEnrollProgressXP(t *testing.T) {
	ctx := context.Background()
	cs := NewMemoryCourseStore()
	list, err := cs.List(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("seeded courses: len=%d err=%v", len(list), err)
	}
	course := list[0]
	lessonID := course.Modules[0].Lessons[0].ID
	userID := "user-1"

	// Completing a lesson without enrolling is a conflict, not XP.
	if _, _, err := cs.CompleteLesson(ctx, userID, course.ID, lessonID); err != ErrNotEnrolled {
		t.Fatalf("complete without enroll: got %v, want ErrNotEnrolled", err)
	}
	if err := cs.Enroll(ctx, userID, course.ID); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	prog, awarded, err := cs.CompleteLesson(ctx, userID, course.ID, lessonID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if awarded != LessonXP+10 {
		t.Fatalf("first lesson should award lesson XP + first-lesson bonus, got %d", awarded)
	}
	if prog.CompletedLessons != 1 || prog.XP != awarded {
		t.Fatalf("progress after one lesson: %+v", prog)
	}
	// Re-completing is idempotent: no double XP.
	_, awarded, err = cs.CompleteLesson(ctx, userID, course.ID, lessonID)
	if err != nil || awarded != 0 {
		t.Fatalf("re-complete: awarded=%d err=%v, want 0/nil", awarded, err)
	}
	// Unknown lesson -> not found.
	if _, _, err := cs.CompleteLesson(ctx, userID, course.ID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown lesson: got %v, want ErrNotFound", err)
	}
}

func TestMemoryCourseCRUD(t *testing.T) {
	ctx := context.Background()
	cs := NewMemoryCourseStore()
	c, err := cs.Create(ctx, &Course{ID: "test-course", Title: "Test", Track: "Test",
		Modules: []Module{{Position: 1, Title: "Core", Lessons: []Lesson{{Position: 1, Title: "L1"}}}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Modules[0].Lessons[0].ID == "" {
		t.Fatal("created lesson must get an ID")
	}
	if _, err := cs.Create(ctx, &Course{ID: "BAD ID", Title: "x"}); err == nil {
		t.Fatal("invalid slug must be rejected")
	}
	u, err := cs.Update(ctx, "test-course", "Test 2", "Test", "desc")
	if err != nil || u.Title != "Test 2" {
		t.Fatalf("update: %v %+v", err, u)
	}
	if err := cs.Delete(ctx, "test-course"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := cs.Get(ctx, "test-course"); err != ErrNotFound {
		t.Fatalf("get deleted: got %v, want ErrNotFound", err)
	}
}

func TestMemoryAuditAndJobs(t *testing.T) {
	ctx := context.Background()
	a := NewMemoryAuditStore()
	if err := a.Record(ctx, AuditEntry{ActorID: "u1", Action: "test_action", ResourceType: "lab", ResourceID: "l1"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	entries, total, err := a.List(ctx, 10, 0)
	if err != nil || total != 1 || len(entries) != 1 || entries[0].Action != "test_action" {
		t.Fatalf("list: total=%d entries=%v err=%v", total, entries, err)
	}
	if _, _, err := a.List(ctx, 10, 99); err != nil {
		t.Fatalf("list beyond total: %v", err)
	}

	j := NewMemoryJobStore()
	job, replayed, err := j.Enqueue(ctx, JobLabProvision, map[string]any{"lab_id": "l1"}, "k1")
	if err != nil || replayed {
		t.Fatalf("enqueue: %v replayed=%v", err, replayed)
	}
	_, replayed, err = j.Enqueue(ctx, JobLabProvision, map[string]any{"lab_id": "l1"}, "k1")
	if err != nil || !replayed {
		t.Fatalf("duplicate enqueue must replay: %v replayed=%v", err, replayed)
	}
	claimed, err := j.Claim(ctx, "w1", 5)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	counts, _ := j.Counts(ctx)
	if counts["running"] != 1 {
		t.Fatalf("counts after claim: %v", counts)
	}
	if err := j.Complete(ctx, job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	counts, _ = j.Counts(ctx)
	if counts["done"] != 1 {
		t.Fatalf("counts after complete: %v", counts)
	}
}

func TestMemoryLabTransition(t *testing.T) {
	ctx := context.Background()
	inner := labs.NewStore()
	inner.ProvisionDelay = time.Hour // keep the async goroutine asleep
	inner.CheckNetwork = func() bool { return true }
	m := NewMemoryLabStore(inner)
	l, _, err := m.Create(ctx, "u1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Transition(ctx, l.ID, labs.StateReady, ""); err == nil {
		t.Fatal("CREATED -> READY must be illegal")
	}
	got, err := m.Transition(ctx, l.ID, labs.StateProvisioning, "")
	if err != nil || got.State != labs.StateProvisioning {
		t.Fatalf("transition to PROVISIONING: %v %+v", err, got)
	}
	got, err = m.Transition(ctx, l.ID, labs.StateFailed, "boom")
	if err != nil || got.State != labs.StateFailed || got.Error != "boom" {
		t.Fatalf("transition to FAILED: %v %+v", err, got)
	}
}

func TestAuthStoreSatisfiesUserStore(t *testing.T) {
	// Compile-time assertion lives in memory.go; this exercises the
	// role methods the M2 API depends on.
	var _ UserStore = auth.NewStore("secret")
	s := auth.NewStore("secret")
	u, err := s.Register("ada@example.com", "Ada", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != auth.RoleLearner {
		t.Fatalf("new user role: got %q, want learner", u.Role)
	}
	admin, err := s.SetRole(u.ID, auth.RoleAdmin)
	if err != nil || admin.Role != auth.RoleAdmin {
		t.Fatalf("SetRole: %v %+v", err, admin)
	}
	if _, err := s.SetRole(u.ID, "superuser"); err != auth.ErrInvalidRole {
		t.Fatalf("invalid role: got %v, want ErrInvalidRole", err)
	}
	users, err := s.List()
	if err != nil || len(users) != 1 {
		t.Fatalf("List: %v %d", err, len(users))
	}
}
