-- CloudForge Milestones 6 + 7 schema.
--
-- 002 already created terraform_runs and terminal_sessions as
-- schema-ready tables; this migration extends them with only the
-- columns M6/M7 code actually uses, and adds the lab_resources
-- columns the M6 resource API reads/writes. No earlier table is
-- rewritten and no column is dropped.

-- lab_resources: explicit columns for the fields the M6 resource API
-- returns (kind is resource_type; name/parent/IPs/CIDR previously
-- lived only inside the configuration JSONB). Configuration remains
-- the home for kind-specific detail (routes, security-group rules,
-- profiles); the new columns are the queryable subset.
ALTER TABLE lab_resources ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_resources ADD COLUMN IF NOT EXISTS parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_resources ADD COLUMN IF NOT EXISTS private_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_resources ADD COLUMN IF NOT EXISTS public_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_resources ADD COLUMN IF NOT EXISTS cidr TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_lab_resources_lab_type ON lab_resources(lab_id, resource_type);

-- terraform_runs (M7): plan identity + approval gating + job linkage.
-- plan_id is the stable handle POST .../terraform/approve takes;
-- plan_json is the parsed `terraform show -json` output (validated
-- JSON, not raw stdout); approved_at/approved_by record the explicit
-- approval; plan_expires_at bounds it (1h after the plan succeeds);
-- job_id links the run to the durable jobs-table job that executed it;
-- error carries the terminal failure message.
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS plan_id TEXT NOT NULL DEFAULT '';
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS plan_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS approved_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS plan_expires_at TIMESTAMPTZ;
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE terraform_runs ADD COLUMN IF NOT EXISTS configuration TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_terraform_runs_plan_id ON terraform_runs(plan_id) WHERE plan_id <> '';
CREATE INDEX IF NOT EXISTS idx_terraform_runs_lab ON terraform_runs(lab_id, created_at DESC);

-- terminal_sessions (M6): session lifecycle. created_at is the
-- session start; ended_at is set when the WebSocket closes; used
-- marks a single-use token as consumed; mode records whether the
-- shell was simulated or routed through the host agent.
ALTER TABLE terminal_sessions ADD COLUMN IF NOT EXISTS ended_at TIMESTAMPTZ;
ALTER TABLE terminal_sessions ADD COLUMN IF NOT EXISTS used BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE terminal_sessions ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'simulated';
CREATE UNIQUE INDEX IF NOT EXISTS idx_terminal_sessions_token_hash ON terminal_sessions(token_hash);
