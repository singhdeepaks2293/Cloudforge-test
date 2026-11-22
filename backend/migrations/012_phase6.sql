-- Phase 6 (CloudForge v2) — advanced services.
--
-- Same generic-entity shape as migrations 009-011 (one p6_* table per
-- collection: id, name, data JSONB). The Phase 3 store
-- (store.Phase3Store) serves these collections too; see
-- store/phase3.go P3Collections for the allowlist.
--
-- Collections and what `data` holds:
--   athena_queries / athena_executions (Athena-style SQL: saved
--     queries + execution history; queries run read-only against
--     the platform Postgres or an RDS instance database),
--   glue_crawlers / glue_tables (Glue-style data catalog crawled
--     from Postgres information_schema, DynamoDB tables, S3 buckets),
--   kinesis_streams / kinesis_records (durable stream shards in
--     Postgres with sequence numbers),
--   firehose_streams / firehose_deliveries (delivery streams to
--     S3/MinIO or a Postgres table, with real delivery records),
--   quicksight_dashboards (saved query + chart type; charts render
--     from live Athena results — no fake BI data),
--   sagemaker_notebooks / sagemaker_jobs / sagemaker_models /
--     sagemaker_endpoints (notebooks = sandboxed Python cells;
--     training = real OLS regression in Go on a built-in or
--     user-supplied tiny dataset; models = versioned JSON artifacts
--     on disk; endpoints serve real predictions from stored weights),
--   bedrock_invocations (SIMULATED concept playground — template
--     responses only, labelled as such),
--   dms_tasks / dms_logs (real row copies between platform Postgres
--     databases and RDS instances), datasync_tasks / datasync_logs
--     (real file/object copies with SHA-256 verification),
--   findings (ONE table for GuardDuty + Inspector + Config +
--     Trusted Advisor findings; Security Hub aggregates it),
--   inspector_assessments (real checks against real lab resources),
--   config_items / config_rules / config_results (recorded
--     configuration history + rules evaluated against real state),
--   waf_acls (real rule evaluation against supplied requests;
--     SIMULATED — not inline in the API request path),
--   kms_keys (durable keys; key material wrapped with a master key
--     derived from CLOUDFORGE_KMS_MASTER_KEY or the JWT secret),
--     secrets (Secrets Manager: values envelope-encrypted with a
--     KMS key, versioned),
--   vpc_peerings / transit_gateways / tgw_attachments /
--     vpc_endpoints / netfirewalls (advanced networking; real
--     route-table entries in lab state — the engine has no
--     peering/TGW RPC, so fidelity is SIMULATED with real state),
--   flowlog_groups / flowlog_records (records built ONLY from real
--     engine telemetry snapshots — never invented flows),
--   ssm_commands (Systems Manager run-command invocations via the
--     allowlisted host-agent ops), advisor_results (Trusted Advisor
--     checks against real resource state), cost_budgets (budgets
--     evaluated against the real educational cost model),
--   org_units (Organizations: units + member platform users).
--
-- Transfer Family is UNSUPPORTED (no local SFTP server binary in
-- the sandbox or on a stock host) and has no table; Migration Hub
-- has no table either — it aggregates dms_tasks + datasync_tasks.


CREATE TABLE IF NOT EXISTS p6_athena_queries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_athena_executions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_glue_crawlers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_glue_tables (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_kinesis_streams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_kinesis_records (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_firehose_streams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_firehose_deliveries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_quicksight_dashboards (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_sagemaker_notebooks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_sagemaker_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_sagemaker_models (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_sagemaker_endpoints (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_bedrock_invocations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_dms_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_dms_logs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_datasync_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_datasync_logs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_findings (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_inspector_assessments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_config_items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_config_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_config_results (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_waf_acls (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_kms_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_secrets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_vpc_peerings (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_transit_gateways (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_tgw_attachments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_vpc_endpoints (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_netfirewalls (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_flowlog_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_flowlog_records (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_ssm_commands (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_advisor_results (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_cost_budgets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p6_org_units (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS p6_kinesis_records_stream_idx ON p6_kinesis_records ((data->>'stream_id'));
CREATE INDEX IF NOT EXISTS p6_findings_source_idx ON p6_findings ((data->>'source'));
CREATE INDEX IF NOT EXISTS p6_findings_status_idx ON p6_findings ((data->>'status'));
CREATE INDEX IF NOT EXISTS p6_flowlog_records_group_idx ON p6_flowlog_records ((data->>'group_id'));
CREATE INDEX IF NOT EXISTS p6_athena_executions_query_idx ON p6_athena_executions ((data->>'query_id'));
