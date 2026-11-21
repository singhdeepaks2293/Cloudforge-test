-- CloudForge Milestone 2 schema, part 2: durable jobs + audit columns.
--
-- 001's audit_logs table is NOT rewritten: it already has user_id,
-- lab_id, action, request_id, details (JSONB metadata) and created_at.
-- This migration only adds the resource_type / resource_id columns the
-- M2 audit API needs (details remains the metadata JSONB).

-- Durable job queue (polled by cmd/worker) ------------------------------
CREATE TABLE IF NOT EXISTS jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL, -- lab_provision | lab_destroy | lab_expire | audit_cleanup
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    state           TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','failed','dead','done')),
    attempts        INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts    INT NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    run_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by       TEXT NOT NULL DEFAULT '',
    locked_at       TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One pending/running job per idempotency key; completed jobs keep the
-- key reserved so a replayed enqueue is a no-op (NULL keys exempt).
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency ON jobs(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs(state, run_at) WHERE state IN ('pending','failed');

-- Audit: resource columns -------------------------------------------------
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS resource_type TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS resource_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);
