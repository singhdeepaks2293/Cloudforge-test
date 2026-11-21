-- CloudForge Milestone 8 schema: curriculum content.
--
-- Only tables/columns M8 code actually uses. Existing tables are
-- extended, never rewritten; the M1/M2 seed data (vpc-fundamentals,
-- iam-security, ec2-basics) is untouched — the curriculum tracks are
-- new course rows (networking, iam, ec2, ...) seeded by the M8
-- seeder, not by SQL.
--
-- Double-XP prevention relies on existing unique constraints
-- (user_progress PK (user_id, lesson_id); user_achievements PK
-- (user_id, achievement_id)) plus per-reason guards in the store
-- code (assessment_submissions and break_fix_attempts below are
-- keyed so a second award for the same reason is a no-op).

-- Courses: track metadata from the curriculum YAML.
ALTER TABLE courses ADD COLUMN IF NOT EXISTS difficulty TEXT NOT NULL DEFAULT 'beginner'
    CHECK (difficulty IN ('beginner','intermediate','advanced'));
ALTER TABLE courses ADD COLUMN IF NOT EXISTS estimated_hours DOUBLE PRECISION NOT NULL DEFAULT 0
    CHECK (estimated_hours >= 0);
ALTER TABLE courses ADD COLUMN IF NOT EXISTS xp_reward INT NOT NULL DEFAULT 0
    CHECK (xp_reward >= 0);

-- Lessons: stable YAML slug + full lesson content (theory, key
-- points, mermaid diagram). slug is the curriculum lesson id
-- (e.g. 'cloud-networking'); the UUID id stays the primary key.
ALTER TABLE lessons ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';
ALTER TABLE lessons ADD COLUMN IF NOT EXISTS theory TEXT NOT NULL DEFAULT '';
ALTER TABLE lessons ADD COLUMN IF NOT EXISTS key_points JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lessons ADD COLUMN IF NOT EXISTS diagram TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_lessons_module_slug ON lessons(module_id, slug) WHERE slug <> '';

-- Lab definitions: validation rules / tasks / kind / fault spec, so
-- a seeded definition carries everything the M6 validation panel
-- and the break-and-fix flow need. kind is 'guided' | 'challenge' |
-- 'break-fix'; fault/hints/fix_steps are only populated for
-- break-fix definitions.
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS validation_rules JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS tasks JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'guided'
    CHECK (kind IN ('guided','challenge','break-fix'));
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS fault JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS hints JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS fix_steps JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lab_definitions ADD COLUMN IF NOT EXISTS xp_reward INT NOT NULL DEFAULT 0
    CHECK (xp_reward >= 0);

-- Achievements: the trigger (JSONB) that decides when an
-- achievement is earned; achievements seeded by M2 have no trigger
-- and are never auto-awarded by the M8 evaluator.
ALTER TABLE achievements ADD COLUMN IF NOT EXISTS trigger JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Assessments: one row per track question. correct_answer is
-- server-side only; the API never returns it to the learner.
CREATE TABLE IF NOT EXISTS assessments (
    course_id      TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    question_index INT NOT NULL CHECK (question_index >= 0),
    question       TEXT NOT NULL,
    options        JSONB NOT NULL DEFAULT '[]'::jsonb,
    correct_answer INT NOT NULL CHECK (correct_answer BETWEEN 0 AND 3),
    explanation    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (course_id, question_index)
);

-- Assessment submissions: one row per attempt. XP is awarded only
-- for the first passing (>= 80%) submission per user+track; the
-- store checks for an earlier passing row before awarding.
CREATE TABLE IF NOT EXISTS assessment_submissions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id  TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    score      INT NOT NULL CHECK (score BETWEEN 0 AND 100),
    total      INT NOT NULL CHECK (total > 0),
    correct    INT NOT NULL CHECK (correct >= 0),
    passed     BOOLEAN NOT NULL DEFAULT FALSE,
    xp_awarded INT NOT NULL DEFAULT 0 CHECK (xp_awarded >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_assessment_submissions_user_course
    ON assessment_submissions(user_id, course_id, created_at DESC);

-- Lab completions: a guided lab / challenge counts as completed
-- when its validation passes (recorded by the API, idempotent per
-- user+lab). This is what the labs_completed / challenges_completed
-- achievement triggers observe.
CREATE TABLE IF NOT EXISTS lab_completions (
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lab_id        UUID NOT NULL REFERENCES lab_sessions(id) ON DELETE CASCADE,
    definition_id TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT 'lab' CHECK (kind IN ('lab','challenge')),
    completed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, lab_id)
);
CREATE INDEX IF NOT EXISTS idx_lab_completions_user ON lab_completions(user_id, completed_at);

-- Break-and-fix attempts: one row per user+lab+scenario. A scenario
-- is solved at most once per user (solved_at set once, XP awarded
-- once); starting the same scenario again replays the open attempt.
CREATE TABLE IF NOT EXISTS break_fix_attempts (
    lab_id       UUID NOT NULL REFERENCES lab_sessions(id) ON DELETE CASCADE,
    scenario_id  TEXT NOT NULL,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fault_status TEXT NOT NULL DEFAULT 'fault_pending'
        CHECK (fault_status IN ('injected','fault_pending')),
    xp_reward    INT NOT NULL DEFAULT 0 CHECK (xp_reward >= 0),
    xp_awarded   INT NOT NULL DEFAULT 0 CHECK (xp_awarded >= 0),
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    solved_at    TIMESTAMPTZ,
    PRIMARY KEY (lab_id, scenario_id)
);
CREATE INDEX IF NOT EXISTS idx_break_fix_user ON break_fix_attempts(user_id, solved_at);
