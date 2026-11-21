-- CloudForge Milestone 2 schema, part 1.
--
-- Adds only tables M2 actually uses, plus terraform_runs and
-- terminal_sessions as schema-ready tables for M3/M6 (no M2 code writes
-- them yet — see backend README). Tables already created by 001
-- (roles, user_roles, course_modules, lessons, lab_definitions,
-- audit_logs) are NOT recreated here.
--
-- Seed data: the three M1 courses, their modules/lessons and lab
-- definitions, so a fresh Postgres starts with the same catalogue as
-- the in-memory fallback. Fixed UUIDs for modules keep the seed
-- deterministic and idempotent (ON CONFLICT DO NOTHING).

-- Roles: 001 created the table but seeded nothing. M2 roles are
-- learner (default), instructor, admin. (001's comment said "student";
-- the canonical M2 name is "learner".)
INSERT INTO roles (name, description) VALUES
    ('learner', 'Default role: take courses, run labs'),
    ('instructor', 'Create and grade courses and labs'),
    ('admin', 'Manage users, courses and the platform')
ON CONFLICT (name) DO NOTHING;

-- Enrollments -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS course_enrollments (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id    TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    enrolled_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, course_id)
);

-- Progress and XP --------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_progress (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id   TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id   UUID NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    xp_awarded  INT NOT NULL DEFAULT 0 CHECK (xp_awarded >= 0),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, lesson_id)
);
CREATE INDEX IF NOT EXISTS idx_user_progress_user_course ON user_progress(user_id, course_id);

CREATE TABLE IF NOT EXISTS xp_transactions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount     INT NOT NULL CHECK (amount <> 0),
    reason     TEXT NOT NULL,
    ref_type   TEXT NOT NULL DEFAULT '', -- e.g. 'lesson'
    ref_id     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_xp_user ON xp_transactions(user_id, created_at);

CREATE TABLE IF NOT EXISTS achievements (
    id          TEXT PRIMARY KEY, -- slug, e.g. 'first-lesson'
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    xp_reward   INT NOT NULL DEFAULT 0 CHECK (xp_reward >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_achievements (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    achievement_id TEXT NOT NULL REFERENCES achievements(id) ON DELETE CASCADE,
    awarded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, achievement_id)
);

INSERT INTO achievements (id, title, description, xp_reward) VALUES
    ('first-lesson', 'First Lesson', 'Completed your first lesson', 10),
    ('first-lab', 'First Lab', 'Completed your first lab', 25),
    ('vpc-builder', 'VPC Builder', 'Completed the VPC Fundamentals course', 50)
ON CONFLICT (id) DO NOTHING;

-- Schema-ready for M3/M6 (no M2 writes) ---------------------------------
CREATE TABLE IF NOT EXISTS terraform_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lab_id       UUID REFERENCES lab_sessions(id) ON DELETE SET NULL,
    action       TEXT NOT NULL CHECK (action IN ('init','validate','plan','apply','destroy')),
    state        TEXT NOT NULL DEFAULT 'QUEUED' CHECK (state IN ('QUEUED','RUNNING','SUCCEEDED','FAILED')),
    plan_output  TEXT NOT NULL DEFAULT '',
    apply_output TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS terminal_sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lab_id     UUID REFERENCES lab_sessions(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_terminal_lab ON terminal_sessions(lab_id, expires_at);

-- Seed catalogue (same content as the M1 in-memory catalogue) -----------
INSERT INTO courses (id, title, track, description) VALUES
    ('vpc-fundamentals', 'VPC Fundamentals', 'Networking',
     'Virtual private clouds, subnets, route tables, internet gateways and NAT — build an isolated network the way AWS VPCs work, on real Linux networking primitives.'),
    ('iam-security', 'IAM & Security', 'Security',
     'Users, groups, roles and policies. Learn least privilege by writing policies and watching them allow or deny real operations.'),
    ('ec2-basics', 'EC2 Basics', 'Compute',
     'Launch, connect to and manage Linux instances. AMIs, key pairs, disks and the instance lifecycle — backed by local KVM VMs.')
ON CONFLICT (id) DO NOTHING;

INSERT INTO course_modules (id, course_id, position, title) VALUES
    ('11111111-1111-4111-8111-111111111111', 'vpc-fundamentals', 1, 'Core'),
    ('22222222-2222-4222-8222-222222222222', 'iam-security', 1, 'Core'),
    ('33333333-3333-4333-8333-333333333333', 'ec2-basics', 1, 'Core')
ON CONFLICT (course_id, position) DO NOTHING;

INSERT INTO lessons (module_id, position, title) VALUES
    ('11111111-1111-4111-8111-111111111111', 1, 'Cloud networking concepts'),
    ('11111111-1111-4111-8111-111111111111', 2, 'VPCs and CIDR blocks'),
    ('11111111-1111-4111-8111-111111111111', 3, 'Public and private subnets'),
    ('11111111-1111-4111-8111-111111111111', 4, 'Route tables and internet gateways'),
    ('11111111-1111-4111-8111-111111111111', 5, 'NAT gateways'),
    ('11111111-1111-4111-8111-111111111111', 6, 'Security groups and network ACLs'),
    ('22222222-2222-4222-8222-222222222222', 1, 'Users, groups and roles'),
    ('22222222-2222-4222-8222-222222222222', 2, 'Writing IAM policies'),
    ('22222222-2222-4222-8222-222222222222', 3, 'Least privilege in practice'),
    ('22222222-2222-4222-8222-222222222222', 4, 'Roles vs long-lived credentials'),
    ('22222222-2222-4222-8222-222222222222', 5, 'Troubleshooting access denied'),
    ('33333333-3333-4333-8333-333333333333', 1, 'Instances and AMIs'),
    ('33333333-3333-4333-8333-333333333333', 2, 'Key pairs and SSH'),
    ('33333333-3333-4333-8333-333333333333', 3, 'Instance lifecycle'),
    ('33333333-3333-4333-8333-333333333333', 4, 'Disks and EBS concepts'),
    ('33333333-3333-4333-8333-333333333333', 5, 'Instance metadata and cloud-init')
ON CONFLICT (module_id, position) DO NOTHING;

INSERT INTO lab_definitions (id, course_id, title, description, duration_min) VALUES
    ('vpc-fundamentals', 'vpc-fundamentals', 'Build a VPC with public and private subnets', 'Create an isolated VPC, two subnets, route tables and a NAT gateway.', 60),
    ('iam-security', 'iam-security', 'Fix a broken IAM policy', 'Diagnose an access-denied error and apply least privilege.', 30),
    ('ec2-basics', 'ec2-basics', 'Launch a Linux web server', 'Launch a KVM-backed Linux instance and serve a web page.', 30),
    ('three-tier-app', 'vpc-fundamentals', 'Build a three-tier application', 'Web, application and database tiers across public and private subnets.', 120)
ON CONFLICT (id) DO NOTHING;
