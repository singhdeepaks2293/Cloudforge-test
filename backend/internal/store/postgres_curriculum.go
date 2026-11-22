package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/database"
)

// PostgresCurriculumStore implements curriculum.Store against
// PostgreSQL (migration 005). Seeding is idempotent: every upsert
// keys on a stable id, so re-running a seed changes nothing.
type PostgresCurriculumStore struct {
	db *database.DB
}

// NewPostgresCurriculumStore builds a Postgres-backed curriculum store.
func NewPostgresCurriculumStore(db *database.DB) *PostgresCurriculumStore {
	return &PostgresCurriculumStore{db: db}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

func unmarshalRules(raw []byte) []curriculum.Rule {
	var out []curriculum.Rule
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []curriculum.Rule{}
	}
	return out
}

func unmarshalStrings(raw []byte) []string {
	var out []string
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []string{}
	}
	return out
}

func (p *PostgresCurriculumStore) upsertLabDefTx(ctx context.Context, tx pgx.Tx, d curriculum.LabDefinition) error {
	tasks := mustJSON(d.Tasks)
	if d.Tasks == nil {
		tasks = []byte("[]")
	}
	rules := mustJSON(d.ValidationRules)
	if d.ValidationRules == nil {
		rules = []byte("[]")
	}
	_, err := tx.Exec(ctx, `INSERT INTO lab_definitions(id, course_id, title, description, duration_min,
		validation_rules, tasks, kind, xp_reward)
		VALUES($1, NULLIF($2,''), $3, $4, 60, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET course_id=EXCLUDED.course_id, title=EXCLUDED.title,
		description=EXCLUDED.description, validation_rules=EXCLUDED.validation_rules,
		tasks=EXCLUDED.tasks, kind=EXCLUDED.kind, xp_reward=EXCLUDED.xp_reward`,
		d.ID, d.CourseID, d.Title, d.Description, rules, tasks, d.Kind, d.XPReward)
	return err
}

func (p *PostgresCurriculumStore) UpsertTrack(ctx context.Context, t *curriculum.Track) error {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO courses(id, title, track, description, difficulty, estimated_hours, xp_reward)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, track=EXCLUDED.track,
		description=EXCLUDED.description, difficulty=EXCLUDED.difficulty,
		estimated_hours=EXCLUDED.estimated_hours, xp_reward=EXCLUDED.xp_reward`,
		t.ID, t.Title, t.Title, t.Description, courseDifficulty(t.Difficulty), t.EstimatedHours, t.XPReward); err != nil {
		return err
	}
	for i, mod := range t.Modules {
		var moduleID string
		err := tx.QueryRow(ctx, "SELECT id FROM course_modules WHERE course_id=$1 AND position=$2", t.ID, i+1).Scan(&moduleID)
		if err != nil {
			if err == pgx.ErrNoRows {
				if err := tx.QueryRow(ctx, "INSERT INTO course_modules(course_id, position, title) VALUES($1,$2,$3) RETURNING id",
					t.ID, i+1, mod.Title).Scan(&moduleID); err != nil {
					return err
				}
			} else {
				return err
			}
		} else if _, err := tx.Exec(ctx, "UPDATE course_modules SET title=$1 WHERE id=$2", mod.Title, moduleID); err != nil {
			return err
		}
		for j, les := range mod.Lessons {
			kps := mustJSON(les.KeyPoints)
			if les.KeyPoints == nil {
				kps = []byte("[]")
			}
			var lessonID string
			if les.Slug != "" {
				err := tx.QueryRow(ctx, "SELECT id FROM lessons WHERE module_id=$1 AND slug=$2", moduleID, les.Slug).Scan(&lessonID)
				if err != nil && err != pgx.ErrNoRows {
					return err
				}
				if err == pgx.ErrNoRows {
					lessonID = ""
				}
			}
			if lessonID == "" {
				if err := tx.QueryRow(ctx, `INSERT INTO lessons(module_id, position, title, body, slug, theory, key_points, diagram)
					VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
					moduleID, j+1, les.Title, les.Theory, les.Slug, les.Theory, kps, les.Diagram).Scan(&lessonID); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE lessons SET position=$1, title=$2, body=$3, theory=$4,
				key_points=$5, diagram=$6 WHERE id=$7`,
				j+1, les.Title, les.Theory, les.Theory, kps, les.Diagram, lessonID); err != nil {
				return err
			}
		}
	}
	if err := p.upsertLabDefTx(ctx, tx, t.GuidedLab); err != nil {
		return err
	}
	if err := p.upsertLabDefTx(ctx, tx, t.Challenge); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM assessments WHERE course_id=$1", t.ID); err != nil {
		return err
	}
	for i, q := range t.Assessment {
		opts := mustJSON(q.Options)
		if _, err := tx.Exec(ctx, `INSERT INTO assessments(course_id, question_index, question, options, correct_answer, explanation)
			VALUES($1,$2,$3,$4,$5,$6)`, t.ID, i, q.Question, opts, q.CorrectAnswer, q.Explanation); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *PostgresCurriculumStore) UpsertAchievement(ctx context.Context, a *curriculum.Achievement) error {
	trigger := mustJSON(a.Trigger)
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO achievements(id, title, description, xp_reward, trigger)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, description=EXCLUDED.description,
		xp_reward=EXCLUDED.xp_reward, trigger=EXCLUDED.trigger`,
		a.ID, a.Title, a.Description, a.XP, trigger)
	return err
}

func (p *PostgresCurriculumStore) UpsertScenario(ctx context.Context, s *curriculum.Scenario) error {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	def := curriculum.LabDefinition{
		ID: "break-fix-" + s.ID, Title: s.Title, Description: s.Symptom,
		ValidationRules: s.ValidationRules, Kind: "break-fix", XPReward: s.XPReward,
	}
	if err := p.upsertLabDefTx(ctx, tx, def); err != nil {
		return err
	}
	fault := mustJSON(s.Fault)
	hints := mustJSON(s.Hints)
	if s.Hints == nil {
		hints = []byte("[]")
	}
	steps := mustJSON(s.FixSteps)
	if s.FixSteps == nil {
		steps = []byte("[]")
	}
	if _, err := tx.Exec(ctx, "UPDATE lab_definitions SET fault=$1, hints=$2, fix_steps=$3 WHERE id=$4",
		fault, hints, steps, def.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *PostgresCurriculumStore) ListTracks(ctx context.Context) ([]*curriculum.TrackSummary, error) {
	// Phase 1 fix (14-vs-17): return curriculum tracks only. The
	// courses table also holds the 3 legacy M1 courses
	// (vpc-fundamentals, iam-security, ec2-basics), which are courses
	// but not tracks; listing every course inflated the count to 17
	// while the seed report (correctly) said 14. A track is a course
	// with BOTH a guided and a challenge lab definition seeded for
	// it — every seeded track upserts both, while the legacy courses
	// only ever gained guided definitions (the 4 launchable lab
	// definitions, course_id = the definition or its track). An
	// EXISTS on either kind alone would still match the legacy
	// courses, so both are required.
	rows, err := p.db.Pool.Query(ctx, `SELECT c.id, c.title, c.description, COALESCE(c.difficulty,'beginner'),
		COALESCE(c.estimated_hours,0), COALESCE(c.xp_reward,0),
		(SELECT COUNT(*) FROM lessons l JOIN course_modules m ON m.id=l.module_id WHERE m.course_id=c.id),
		(SELECT COUNT(*) FROM course_modules m WHERE m.course_id=c.id)
		FROM courses c
		WHERE EXISTS (SELECT 1 FROM lab_definitions ld WHERE ld.course_id=c.id AND ld.kind='guided')
		  AND EXISTS (SELECT 1 FROM lab_definitions ld WHERE ld.course_id=c.id AND ld.kind='challenge')
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*curriculum.TrackSummary
	for rows.Next() {
		var s curriculum.TrackSummary
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.Difficulty, &s.EstimatedHours, &s.XPReward, &s.LessonCount, &s.ModuleCount); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (p *PostgresCurriculumStore) GetTrack(ctx context.Context, id string) (*curriculum.Track, error) {
	cs := NewPostgresCourseStore(p.db)
	c, err := cs.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	t := &curriculum.Track{
		ID: c.ID, Title: c.Title, Description: c.Description,
		Difficulty: c.Difficulty, EstimatedHours: c.EstimatedHours, XPReward: c.XPReward,
	}
	for _, m := range c.Modules {
		mod := curriculum.Module{ID: m.ID, Title: m.Title}
		for _, l := range m.Lessons {
			mod.Lessons = append(mod.Lessons, curriculum.Lesson{
				Slug: l.Slug, Title: l.Title, Theory: l.Theory,
				KeyPoints: l.KeyPoints, Diagram: l.Diagram, DBID: l.ID,
			})
		}
		t.Modules = append(t.Modules, mod)
	}
	for _, kind := range []string{"guided", "challenge"} {
		def, err := p.getLabDefByKind(ctx, id, kind)
		if err == nil {
			if kind == "guided" {
				t.GuidedLab = *def
			} else {
				t.Challenge = *def
			}
		}
	}
	qs, err := p.GetAssessment(ctx, id)
	if err == nil {
		t.Assessment = qs
	}
	return t, nil
}

func (p *PostgresCurriculumStore) getLabDefByKind(ctx context.Context, courseID, kind string) (*curriculum.LabDefinition, error) {
	row := p.db.Pool.QueryRow(ctx, `SELECT id, COALESCE(course_id,''), title, COALESCE(description,''),
		COALESCE(tasks,'[]'::jsonb), COALESCE(validation_rules,'[]'::jsonb), kind, COALESCE(xp_reward,0)
		FROM lab_definitions WHERE course_id=$1 AND kind=$2 ORDER BY id LIMIT 1`, courseID, kind)
	return scanLabDef(row)
}

func scanLabDef(row pgx.Row) (*curriculum.LabDefinition, error) {
	var d curriculum.LabDefinition
	var tasksRaw, rulesRaw []byte
	if err := row.Scan(&d.ID, &d.CourseID, &d.Title, &d.Description, &tasksRaw, &rulesRaw, &d.Kind, &d.XPReward); err != nil {
		return nil, err
	}
	d.Tasks = unmarshalStrings(tasksRaw)
	d.ValidationRules = unmarshalRules(rulesRaw)
	return &d, nil
}

func (p *PostgresCurriculumStore) GetAssessment(ctx context.Context, courseID string) ([]curriculum.Question, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT question, COALESCE(options,'[]'::jsonb), correct_answer, COALESCE(explanation,'')
		FROM assessments WHERE course_id=$1 ORDER BY question_index`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []curriculum.Question
	for rows.Next() {
		var q curriculum.Question
		var optsRaw []byte
		if err := rows.Scan(&q.Question, &optsRaw, &q.CorrectAnswer, &q.Explanation); err != nil {
			return nil, err
		}
		q.Options = unmarshalStrings(optsRaw)
		out = append(out, q)
	}
	return out, rows.Err()
}

func (p *PostgresCurriculumStore) FindLesson(ctx context.Context, lessonID string) (string, string, error) {
	var courseID, uuid string
	err := p.db.Pool.QueryRow(ctx, `SELECT m.course_id, l.id FROM lessons l
		JOIN course_modules m ON m.id=l.module_id
		WHERE l.slug=$1 OR l.id::text=$1 LIMIT 1`, lessonID).Scan(&courseID, &uuid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return courseID, uuid, nil
}

func (p *PostgresCurriculumStore) GetLabDefinition(ctx context.Context, id string) (*curriculum.LabDefinition, error) {
	row := p.db.Pool.QueryRow(ctx, `SELECT id, COALESCE(course_id,''), title, COALESCE(description,''),
		COALESCE(tasks,'[]'::jsonb), COALESCE(validation_rules,'[]'::jsonb), kind, COALESCE(xp_reward,0)
		FROM lab_definitions WHERE id=$1`, id)
	d, err := scanLabDef(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (p *PostgresCurriculumStore) ListScenarios(ctx context.Context) ([]*curriculum.Scenario, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id, title, COALESCE(description,''),
		COALESCE(validation_rules,'[]'::jsonb), COALESCE(fault,'{}'::jsonb),
		COALESCE(hints,'[]'::jsonb), COALESCE(fix_steps,'[]'::jsonb), COALESCE(xp_reward,0)
		FROM lab_definitions WHERE kind='break-fix' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*curriculum.Scenario
	for rows.Next() {
		s, err := scanScenario(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanScenario(rows pgx.Rows) (*curriculum.Scenario, error) {
	var s curriculum.Scenario
	var defID string
	var rulesRaw, faultRaw, hintsRaw, stepsRaw []byte
	if err := rows.Scan(&defID, &s.Title, &s.Symptom, &rulesRaw, &faultRaw, &hintsRaw, &stepsRaw, &s.XPReward); err != nil {
		return nil, err
	}
	s.ID = defID
	if len(defID) > len("break-fix-") {
		s.ID = defID[len("break-fix-"):]
	}
	s.ValidationRules = unmarshalRules(rulesRaw)
	_ = json.Unmarshal(faultRaw, &s.Fault)
	s.Hints = unmarshalStrings(hintsRaw)
	s.FixSteps = unmarshalStrings(stepsRaw)
	return &s, nil
}

func (p *PostgresCurriculumStore) GetScenario(ctx context.Context, id string) (*curriculum.Scenario, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id, title, COALESCE(description,''),
		COALESCE(validation_rules,'[]'::jsonb), COALESCE(fault,'{}'::jsonb),
		COALESCE(hints,'[]'::jsonb), COALESCE(fix_steps,'[]'::jsonb), COALESCE(xp_reward,0)
		FROM lab_definitions WHERE kind='break-fix' AND (id=$1 OR id=$2) LIMIT 1`, id, "break-fix-"+id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanScenario(rows)
	}
	return nil, ErrNotFound
}

func (p *PostgresCurriculumStore) ListAchievements(ctx context.Context) ([]*curriculum.Achievement, error) {
	rows, err := p.db.Pool.Query(ctx, `SELECT id, title, COALESCE(description,''), COALESCE(xp_reward,0),
		COALESCE(trigger,'{}'::jsonb) FROM achievements ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*curriculum.Achievement
	for rows.Next() {
		var a curriculum.Achievement
		var trigRaw []byte
		if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.XP, &trigRaw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(trigRaw, &a.Trigger)
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (p *PostgresCurriculumStore) UserStats(ctx context.Context, userID string) (*curriculum.Stats, error) {
	s := &curriculum.Stats{Earned: map[string]bool{}, CompletedByDifficulty: map[string]int{}}
	pool := p.db.Pool
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_progress WHERE user_id=$1", userID).Scan(&s.LessonsCompleted); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM lab_completions WHERE user_id=$1 AND kind='lab'", userID).Scan(&s.LabsCompleted); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM lab_completions WHERE user_id=$1 AND kind='challenge'", userID).Scan(&s.ChallengesCompleted); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM break_fix_attempts WHERE user_id=$1 AND solved_at IS NOT NULL", userID).Scan(&s.BreakFixSolved); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT c.id, c.difficulty FROM courses c
		WHERE EXISTS (SELECT 1 FROM course_enrollments e WHERE e.user_id=$1 AND e.course_id=c.id)
		AND (SELECT COUNT(*) FROM lessons l JOIN course_modules m ON m.id=l.module_id WHERE m.course_id=c.id) > 0
		AND (SELECT COUNT(*) FROM lessons l JOIN course_modules m ON m.id=l.module_id WHERE m.course_id=c.id)
			= (SELECT COUNT(*) FROM user_progress up WHERE up.user_id=$1 AND up.course_id=c.id)`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, diff string
		if err := rows.Scan(&id, &diff); err != nil {
			rows.Close()
			return nil, err
		}
		s.CompletedTracks = append(s.CompletedTracks, id)
		s.CompletedByDifficulty[diff]++
	}
	rows.Close()
	if err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(score),0) FROM assessment_submissions WHERE user_id=$1", userID).Scan(&s.BestAssessmentScore); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount),0) FROM xp_transactions WHERE user_id=$1", userID).Scan(&s.TotalXP); err != nil {
		return nil, err
	}
	dateRows, err := pool.Query(ctx, `SELECT DISTINCT DATE(completed_at) FROM user_progress WHERE user_id=$1
		UNION SELECT DISTINCT DATE(completed_at) FROM lab_completions WHERE user_id=$1
		UNION SELECT DISTINCT DATE(created_at) FROM assessment_submissions WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	var dates []time.Time
	for dateRows.Next() {
		var d time.Time
		if err := dateRows.Scan(&d); err != nil {
			dateRows.Close()
			return nil, err
		}
		dates = append(dates, d)
	}
	dateRows.Close()
	s.StreakDays = curriculum.ComputeStreak(dates, time.Now())
	earnedRows, err := pool.Query(ctx, "SELECT achievement_id FROM user_achievements WHERE user_id=$1", userID)
	if err != nil {
		return nil, err
	}
	defer earnedRows.Close()
	for earnedRows.Next() {
		var id string
		if err := earnedRows.Scan(&id); err != nil {
			return nil, err
		}
		s.Earned[id] = true
	}
	return s, earnedRows.Err()
}

func (p *PostgresCurriculumStore) SubmitAssessment(ctx context.Context, userID, courseID string, answers map[int]int) (*curriculum.AssessmentResult, error) {
	qs, err := p.GetAssessment(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if len(qs) == 0 {
		return nil, fmt.Errorf("%w: no assessment for course %q", ErrNotFound, courseID)
	}
	// Enrollment is required, like lesson completion.
	var enrolled bool
	if err := p.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM course_enrollments WHERE user_id=$1 AND course_id=$2)", userID, courseID).Scan(&enrolled); err != nil {
		return nil, err
	}
	if !enrolled {
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
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO assessment_submissions(user_id, course_id, score, total, correct, passed, xp_awarded)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, userID, courseID, res.Score, res.Total, res.Correct, res.Passed, 0); err != nil {
		return nil, err
	}
	if res.Passed {
		var hadPass bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assessment_submissions WHERE user_id=$1 AND course_id=$2 AND passed AND xp_awarded>0)", userID, courseID).Scan(&hadPass); err != nil {
			return nil, err
		}
		// The row just inserted has xp_awarded=0, so hadPass only
		// sees an earlier passing, XP-awarded submission.
		if !hadPass {
			if _, err := tx.Exec(ctx, "UPDATE assessment_submissions SET xp_awarded=$1 WHERE user_id=$2 AND course_id=$3 AND score=$4 AND xp_awarded=0",
				curriculum.AssessmentPassXP, userID, courseID, res.Score); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,$2,'assessment_passed','assessment',$3)",
				userID, curriculum.AssessmentPassXP, courseID); err != nil {
				return nil, err
			}
			res.XPAwarded = curriculum.AssessmentPassXP
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

func (p *PostgresCurriculumStore) AwardAchievements(ctx context.Context, userID string, ids []string) ([]*curriculum.Achievement, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var awarded []*curriculum.Achievement
	for _, id := range ids {
		var a curriculum.Achievement
		var trigRaw []byte
		err := tx.QueryRow(ctx, "SELECT id, title, COALESCE(description,''), COALESCE(xp_reward,0), COALESCE(trigger,'{}'::jsonb) FROM achievements WHERE id=$1", id).
			Scan(&a.ID, &a.Title, &a.Description, &a.XP, &trigRaw)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return nil, err
		}
		_ = json.Unmarshal(trigRaw, &a.Trigger)
		tag, err := tx.Exec(ctx, "INSERT INTO user_achievements(user_id, achievement_id) VALUES($1,$2) ON CONFLICT DO NOTHING", userID, id)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if a.XP > 0 {
			if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,$2,'achievement','achievement',$3)",
				userID, a.XP, id); err != nil {
				return nil, err
			}
		}
		a.Earned = true
		awarded = append(awarded, &a)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return awarded, nil
}

func (p *PostgresCurriculumStore) RecordLabCompletion(ctx context.Context, userID, labID, definitionID, kind string) error {
	if kind != "lab" && kind != "challenge" {
		kind = "lab"
	}
	_, err := p.db.Pool.Exec(ctx, `INSERT INTO lab_completions(user_id, lab_id, definition_id, kind)
		VALUES($1,$2,$3,$4) ON CONFLICT (user_id, lab_id) DO NOTHING`, userID, labID, definitionID, kind)
	return err
}

func (p *PostgresCurriculumStore) StartBreakFix(ctx context.Context, userID, labID, scenarioID, faultStatus string) (*curriculum.BreakFixAttempt, error) {
	sc, err := p.GetScenario(ctx, scenarioID)
	if err != nil {
		return nil, err
	}
	if faultStatus != "injected" && faultStatus != "fault_pending" {
		faultStatus = "fault_pending"
	}
	var existing *curriculum.BreakFixAttempt
	row := p.db.Pool.QueryRow(ctx, `SELECT lab_id, scenario_id, user_id, fault_status, xp_reward, xp_awarded,
		CASE WHEN solved_at IS NULL THEN false ELSE true END FROM break_fix_attempts WHERE lab_id=$1 AND scenario_id=$2`, labID, scenarioID)
	existing, err = scanAttempt(row)
	if err == nil {
		return existing, nil
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}
	if _, err := p.db.Pool.Exec(ctx, `INSERT INTO break_fix_attempts(lab_id, scenario_id, user_id, fault_status, xp_reward)
		VALUES($1,$2,$3,$4,$5)`, labID, scenarioID, userID, faultStatus, sc.XPReward); err != nil {
		return nil, err
	}
	return &curriculum.BreakFixAttempt{LabID: labID, ScenarioID: scenarioID, UserID: userID, FaultStatus: faultStatus, XPReward: sc.XPReward}, nil
}

func scanAttempt(row pgx.Row) (*curriculum.BreakFixAttempt, error) {
	var a curriculum.BreakFixAttempt
	if err := row.Scan(&a.LabID, &a.ScenarioID, &a.UserID, &a.FaultStatus, &a.XPReward, &a.XPAwarded, &a.Solved); err != nil {
		return nil, err
	}
	return &a, nil
}

func (p *PostgresCurriculumStore) GetBreakFixAttemptByLab(ctx context.Context, labID string) (*curriculum.BreakFixAttempt, error) {
	row := p.db.Pool.QueryRow(ctx, `SELECT lab_id, scenario_id, user_id, fault_status, xp_reward, xp_awarded,
		CASE WHEN solved_at IS NULL THEN false ELSE true END FROM break_fix_attempts
		WHERE lab_id=$1 ORDER BY started_at DESC LIMIT 1`, labID)
	a, err := scanAttempt(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

func (p *PostgresCurriculumStore) MarkBreakFixSolved(ctx context.Context, userID, labID string) (*curriculum.BreakFixAttempt, int, error) {
	tx, err := p.db.Pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT lab_id, scenario_id, user_id, fault_status, xp_reward, xp_awarded,
		CASE WHEN solved_at IS NULL THEN false ELSE true END FROM break_fix_attempts
		WHERE lab_id=$1 AND user_id=$2 ORDER BY started_at DESC LIMIT 1 FOR UPDATE`, labID, userID)
	a, err := scanAttempt(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	if a.Solved {
		if err := tx.Commit(ctx); err != nil {
			return nil, 0, err
		}
		return a, 0, nil
	}
	if _, err := tx.Exec(ctx, "UPDATE break_fix_attempts SET solved_at=now(), xp_awarded=xp_reward WHERE lab_id=$1 AND scenario_id=$2",
		a.LabID, a.ScenarioID); err != nil {
		return nil, 0, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO xp_transactions(user_id, amount, reason, ref_type, ref_id) VALUES($1,$2,'break_fix_solved','break_fix',$3)",
		userID, a.XPReward, a.ScenarioID); err != nil {
		return nil, 0, err
	}
	a.Solved = true
	a.XPAwarded = a.XPReward
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return a, a.XPReward, nil
}

var _ curriculum.Store = (*PostgresCurriculumStore)(nil)
