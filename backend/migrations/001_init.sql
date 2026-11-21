-- CloudForge Milestone 1 schema (PostgreSQL).
--
-- M1 runs the API against in-memory stores; this migration is the schema
-- Milestone 2 wires up (pgx + a repository layer). It is included now so
-- the data model is versioned from the start. Apply with psql or your
-- migration runner of choice; it is idempotent for fresh databases only
-- (CREATE TABLE IF NOT EXISTS is used, but there is no down migration).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users and roles -------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE, -- admin | instructor | student
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

-- Courses and lessons ---------------------------------------------------
CREATE TABLE IF NOT EXISTS courses (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    track       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS course_modules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id  TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    position   INT NOT NULL,
    title      TEXT NOT NULL,
    UNIQUE (course_id, position)
);

CREATE TABLE IF NOT EXISTS lessons (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id  UUID NOT NULL REFERENCES course_modules(id) ON DELETE CASCADE,
    position   INT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    UNIQUE (module_id, position)
);

-- Lab definitions and sessions ------------------------------------------
CREATE TABLE IF NOT EXISTS lab_definitions (
    id           TEXT PRIMARY KEY,
    course_id    TEXT REFERENCES courses(id) ON DELETE SET NULL,
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    duration_min INT NOT NULL DEFAULT 60 CHECK (duration_min > 0)
);

CREATE TABLE IF NOT EXISTS lab_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    definition_id   TEXT NOT NULL REFERENCES lab_definitions(id),
    name            TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'CREATED' CHECK (state IN (
                        'CREATED','PROVISIONING','READY','RUNNING','VALIDATING',
                        'COMPLETED','STOPPING','STOPPED','FAILED','EXPIRED',
                        'DESTROYING','DESTROYED')),
    error           TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_lab_sessions_owner_state ON lab_sessions(owner_id, state);
CREATE INDEX IF NOT EXISTS idx_lab_sessions_expires ON lab_sessions(expires_at) WHERE state NOT IN ('DESTROYED','EXPIRED');

CREATE TABLE IF NOT EXISTS lab_resources (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lab_id              UUID NOT NULL REFERENCES lab_sessions(id) ON DELETE CASCADE,
    owner_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL, -- aws-emulator | rust-network-engine | docker | libvirt | simulator
    resource_type       TEXT NOT NULL, -- vpc | subnet | instance | bucket | ...
    provider_resource_id TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL DEFAULT 'CREATED',
    configuration       JSONB NOT NULL DEFAULT '{}'::jsonb,
    cleanup_status      TEXT NOT NULL DEFAULT 'PENDING',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_lab_resources_lab ON lab_resources(lab_id);
CREATE INDEX IF NOT EXISTS idx_lab_resources_cleanup ON lab_resources(cleanup_status) WHERE cleanup_status <> 'CLEANED';

-- Audit ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    lab_id     UUID REFERENCES lab_sessions(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    details    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON audit_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_lab ON audit_logs(lab_id, created_at);
