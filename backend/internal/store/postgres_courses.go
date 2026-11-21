package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

func jsonUnmarshal(raw []byte, dst any) error { return json.Unmarshal(raw, dst) }

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PostgresCourseStore implements CourseStore against PostgreSQL.
type PostgresCourseStore struct {
	db *database.DB
}

// NewPostgresCourseStore builds a Postgres-backed course store.
func NewPostgresCourseStore(db *database.DB) *PostgresCourseStore {
	return &PostgresCourseStore{db: db}
}

func (p *PostgresCourseStore) listCourses(ctx context.Context, where string, args ...any) ([]*Course, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT c.id, c.title, c.track, c.description,
		COALESCE(c.difficulty,'beginner'), COALESCE(c.estimated_hours,0), COALESCE(c.xp_reward,0),
		m.id, m.position, m.title, l.id, l.position, l.title, COALESCE(l.body,''),
		COALESCE(l.slug,''), COALESCE(l.theory,''), COALESCE(l.key_points,'[]'::jsonb), COALESCE(l.diagram,'')
		FROM courses c
		LEFT JOIN course_modules m ON m.course_id = c.id
		LEFT JOIN lessons l ON l.module_id = m.id
		`+where+`
		ORDER BY c.id, m.position, l.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]*Course)
	var order []string
	for rows.Next() {
		var cid, title, track, desc, difficulty string
		var estHours float64
		var xpReward int
		var mid, mtitle *string
		var mpos *int
		var lid, ltitle, lbody, lslug, ltheory, ldiagram *string
		var lpos *int
		var lkeypoints []byte
		if err := rows.Scan(&cid, &title, &track, &desc, &difficulty, &estHours, &xpReward, &mid, &mpos, &mtitle, &lid, &lpos, &ltitle, &lbody, &lslug, &ltheory, &lkeypoints, &ldiagram); err != nil {
			return nil, err
		}
		c, ok := byID[cid]
		if !ok {
			c = &Course{ID: cid, Title: title, Track: track, Description: desc, Difficulty: difficulty, EstimatedHours: estHours, XPReward: xpReward}
			byID[cid] = c
			order = append(order, cid)
		}
		if mid == nil {
			continue
		}
		var mod *Module
		for i := range c.Modules {
			if c.Modules[i].ID == *mid {
				mod = &c.Modules[i]
				break
			}
		}
		if mod == nil {
			c.Modules = append(c.Modules, Module{ID: *mid, CourseID: cid, Position: *mpos, Title: *mtitle})
			mod = &c.Modules[len(c.Modules)-1]
		}
		if lid != nil {
			var kps []string
			_ = jsonUnmarshal(lkeypoints, &kps)
			mod.Lessons = append(mod.Lessons, Lesson{ID: *lid, ModuleID: *mid, Position: *lpos, Title: *ltitle, Body: *lbody, Slug: strVal(lslug), Theory: strVal(ltheory), KeyPoints: kps, Diagram: strVal(ldiagram)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Course, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

func (p *PostgresCourseStore) List(ctx context.Context) ([]*Course, error) {
	return p.listCourses(ctx, "")
}

func (p *PostgresCourseStore) Get(ctx context.Context, id string) (*Course, error) {
	out, err := p.listCourses(ctx, "WHERE c.id = $1", id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out[0], nil
}

func (p *PostgresCourseStore) Create(ctx context.Context, c *Course) (*Course, error) {
	if !validSlug(c.ID) || c.Title == "" {
		return nil, fmt.Errorf("%w: course id must be a lowercase slug and title is required", ErrInvalidInput)
	}
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "INSERT INTO courses(id, title, track, description, difficulty, estimated_hours, xp_reward) VALUES($1,$2,$3,$4,$5,$6,$7)",
		c.ID, c.Title, c.Track, c.Description, courseDifficulty(c.Difficulty), c.EstimatedHours, c.XPReward); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: course %q already exists", ErrInvalidInput, c.ID)
		}
		return nil, err
	}
	modules := c.Modules
	if len(modules) == 0 {
		modules = []Module{{Position: 1, Title: "Core"}}
	}
	for i := range modules {
		mid := modules[i].ID
		if mid == "" {
			mid = ids.New()
		}
		if _, err := tx.Exec(ctx, "INSERT INTO course_modules(id, course_id, position, title) VALUES($1,$2,$3,$4)",
			mid, c.ID, modules[i].Position, modules[i].Title); err != nil {
			return nil, err
		}
		for j := range modules[i].Lessons {
			lid := modules[i].Lessons[j].ID
			if lid == "" {
				lid = ids.New()
			}
			kps, _ := json.Marshal(modules[i].Lessons[j].KeyPoints)
			if kps == nil {
				kps = []byte("[]")
			}
			if _, err := tx.Exec(ctx, "INSERT INTO lessons(id, module_id, position, title, body, slug, theory, key_points, diagram) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)",
				lid, mid, modules[i].Lessons[j].Position, modules[i].Lessons[j].Title, modules[i].Lessons[j].Body,
				modules[i].Lessons[j].Slug, modules[i].Lessons[j].Theory, kps, modules[i].Lessons[j].Diagram); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.Get(ctx, c.ID)
}

func (p *PostgresCourseStore) Update(ctx context.Context, id, title, track, description string) (*Course, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	tag, err := p.db.Pool.Exec(ctx, "UPDATE courses SET title=$1, track=$2, description=$3 WHERE id=$4",
		title, track, description, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.Get(ctx, id)
}

func (p *PostgresCourseStore) Delete(ctx context.Context, id string) error {
	tag, err := p.db.Pool.Exec(ctx, "DELETE FROM courses WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresCourseStore) Enroll(ctx context.Context, userID, courseID string) error {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1)", courseID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, "INSERT INTO course_enrollments(user_id, course_id) VALUES($1,$2) ON CONFLICT DO NOTHING", userID, courseID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *PostgresCourseStore) GetProgress(ctx context.Context, userID, courseID string) (*Progress, error) {
	c, err := p.Get(ctx, courseID)
	if err != nil {
		return nil, err
	}
	prog := &Progress{CourseID: courseID, TotalLessons: c.LessonCount(), CompletedLessonIDs: []string{}}
	var enrolled bool
	if err := p.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM course_enrollments WHERE user_id=$1 AND course_id=$2)", userID, courseID).Scan(&enrolled); err != nil {
		return nil, err
	}
	prog.Enrolled = enrolled
	rows, err := p.db.Pool.Query(ctx, "SELECT lesson_id FROM user_progress WHERE user_id=$1 AND course_id=$2 ORDER BY completed_at", userID, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var lid string
		if err := rows.Scan(&lid); err != nil {
			return nil, err
		}
		prog.CompletedLessonIDs = append(prog.CompletedLessonIDs, lid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prog.CompletedLessons = len(prog.CompletedLessonIDs)
	var totalXP int
	if err := p.db.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount),0) FROM xp_transactions WHERE user_id=$1", userID).Scan(&totalXP); err != nil {
		return nil, err
	}
	prog.XP = totalXP
	if prog.CompletedLessons == prog.TotalLessons && prog.TotalLessons > 0 {
		var doneAt *time.Time
		if err := p.db.Pool.QueryRow(ctx, "SELECT MAX(completed_at) FROM user_progress WHERE user_id=$1 AND course_id=$2", userID, courseID).Scan(&doneAt); err == nil {
			prog.CompletedAt = doneAt
		}
	}
	return prog, nil
}

func (p *PostgresCourseStore) CompleteLesson(ctx context.Context, userID, courseID, lessonID string) (*Progress, int, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	// Lesson must exist in this course.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lessons l
		JOIN course_modules m ON m.id = l.module_id WHERE l.id=$1 AND m.course_id=$2)`, lessonID, courseID).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, fmt.Errorf("%w: no lesson %q in course %q", ErrNotFound, lessonID, courseID)
	}
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM course_enrollments WHERE user_id=$1 AND course_id=$2)", userID, courseID).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, ErrNotEnrolled
	}
	tag, err := tx.Exec(ctx, "INSERT INTO user_progress(user_id, course_id, lesson_id, xp_awarded) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING",
		userID, courseID, lessonID, LessonXP)
	if err != nil {
		return nil, 0, err
	}
	if tag.RowsAffected() == 0 {
		// Already completed: idempotent, no double XP.
		if err := tx.Commit(ctx); err != nil {
			return nil, 0, err
		}
		prog, err := p.GetProgress(ctx, userID, courseID)
		return prog, 0, err
	}
	awarded := LessonXP
	if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,$2,'lesson_completed','lesson',$3)",
		userID, LessonXP, lessonID); err != nil {
		return nil, 0, err
	}
	// First-lesson achievement (once per user) with a bonus.
	var had bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_achievements WHERE user_id=$1 AND achievement_id='first-lesson')", userID).Scan(&had); err != nil {
		return nil, 0, err
	}
	if !had {
		if _, err := tx.Exec(ctx, "INSERT INTO user_achievements(user_id, achievement_id) VALUES($1,'first-lesson') ON CONFLICT DO NOTHING", userID); err != nil {
			return nil, 0, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,10,'achievement_first-lesson','achievement','first-lesson')", userID); err != nil {
			return nil, 0, err
		}
		awarded += 10
	}
	// Track-completion bonus (M8): the track's xp_reward is split
	// across its lessons — 10 XP per lesson plus, on the final
	// lesson, the remainder (xp_reward - 10*lessonCount), so a
	// completed track awards exactly xp_reward in lesson XP (the
	// first-lesson achievement bonus is separate). Courses with
	// xp_reward == 0 (all pre-M8 courses) pay no bonus, so M2
	// behaviour is unchanged. Guarded by the xp_transactions
	// ref so a re-run can never double-award.
	var totalLessons, xpReward int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM lessons l JOIN course_modules m ON m.id=l.module_id WHERE m.course_id=$1", courseID).Scan(&totalLessons); err != nil {
		return nil, 0, err
	}
	if err := tx.QueryRow(ctx, "SELECT COALESCE(xp_reward,0) FROM courses WHERE id=$1", courseID).Scan(&xpReward); err != nil {
		return nil, 0, err
	}
	var doneCount int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_progress WHERE user_id=$1 AND course_id=$2", userID, courseID).Scan(&doneCount); err != nil {
		return nil, 0, err
	}
	if totalLessons > 0 && doneCount >= totalLessons && xpReward > 0 {
		if bonus := xpReward - LessonXP*totalLessons; bonus > 0 {
			var hadBonus bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM xp_transactions WHERE user_id=$1 AND ref_type='track_completion' AND ref_id=$2)", userID, courseID).Scan(&hadBonus); err != nil {
				return nil, 0, err
			}
			if !hadBonus {
				if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,$2,'track_completed','track_completion',$3)",
					userID, bonus, courseID); err != nil {
					return nil, 0, err
				}
				awarded += bonus
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE course_enrollments SET completed_at=COALESCE(completed_at, now()) WHERE user_id=$1 AND course_id=$2", userID, courseID); err != nil {
			return nil, 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	prog, err := p.GetProgress(ctx, userID, courseID)
	if err != nil {
		return nil, 0, err
	}
	return prog, awarded, nil
}

func courseDifficulty(d string) string {
	switch d {
	case "beginner", "intermediate", "advanced":
		return d
	default:
		return "beginner"
	}
}

var _ CourseStore = (*PostgresCourseStore)(nil)
