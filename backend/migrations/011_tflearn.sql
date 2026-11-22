-- Phase 5 (CloudForge v2) — Terraform Learning.
--
-- Same generic-entity shape as migrations 009/010 (one p5_* table per
-- collection: id, name, data JSONB). The Phase 3 store
-- (store.Phase3Store) serves these collections too; see
-- store/phase3.go P3Collections for the allowlist.
--
-- Collections and what `data` holds:
--   p5_tflearn_projects  (owner_id, description, level, template,
--     lab_id — the lab linked for container execution, or null)
--   p5_tflearn_files     (project_id, owner_id, path, content;
--     name is "<project_id>:<path>")
--   p5_tflearn_progress  (user_id, lesson_id, status, quiz_score,
--     quiz_total, completed_at; name is "<user_id>:<lesson_id>")
--   p5_tflearn_runs      (project_id, owner_id, operation, status,
--     plan_id, approved, log, error, terraform_run_id; name is
--     "<project_id>:<operation>:<created-seq>" — unique per run)
--   p5_tflearn_states    (project_id, owner_id, version, resources
--     JSON, locked_by, locked_at, source; name is
--     "<project_id>:v<version>")
--   p5_tflearn_locks     (project_id, owner_id, locked_by, locked_at,
--     expires_at; name is the project id — one lock per project)
--
-- State rule (see tflearn package docs): state versions are
-- append-only snapshots. A restore never rewrites history — it
-- creates a new version whose content is copied from the restored
-- one. State is never edited by string replacement.

CREATE TABLE IF NOT EXISTS p5_tflearn_projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p5_tflearn_files (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p5_tflearn_files_name_idx ON p5_tflearn_files (name);
CREATE TABLE IF NOT EXISTS p5_tflearn_progress (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p5_tflearn_runs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p5_tflearn_runs_name_idx ON p5_tflearn_runs (name);
CREATE TABLE IF NOT EXISTS p5_tflearn_states (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p5_tflearn_states_name_idx ON p5_tflearn_states (name);
CREATE TABLE IF NOT EXISTS p5_tflearn_locks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
