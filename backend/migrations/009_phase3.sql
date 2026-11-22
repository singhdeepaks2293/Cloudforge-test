-- Phase 3 (CloudForge v2) — data, messaging and serverless services.
--
-- One generic table per collection. Each row is an entity: a stable
-- id, a human name (unique where the AWS service requires it), and the
-- whole entity as JSONB in `data` (status, owner, config, counters).
-- This keeps the durable store (Postgres) and the in-memory fallback
-- the same shape: the store package reads/writes entities, handlers
-- own the service semantics. Volumes here are teaching-scale, so
-- list-then-filter in Go is acceptable and is documented in
-- backend/internal/store/phase3.go.
--
-- Collections and what `data` holds (see store/phase3.go for keys):
--   p3_dynamodb_tables, p3_dynamodb_items (Postgres-subset backend,
--     used only when DynamoDB Local is unreachable),
--   p3_rds_instances, p3_rds_snapshots,
--   p3_cache_clusters,
--   p3_load_balancers, p3_lb_listeners, p3_target_groups, p3_lb_targets,
--   p3_dns_zones, p3_dns_records,
--   p3_sqs_queues, p3_sqs_messages,
--   p3_sns_topics, p3_sns_subscriptions, p3_sns_deliveries,
--   p3_eventbridge_rules,
--   p3_lambda_functions, p3_lambda_invocations,
--   p3_apigw_apis, p3_apigw_resources, p3_apigw_stages, p3_apigw_logs.
--
-- Secrets rule (same as IAM, migration 008): a generated RDS master
-- password is shown once at creation; only its SHA-256 hash is stored
-- (data.password_hash). Lambda code and env vars are stored as given
-- — they are teaching code, not credentials; operators must not put
-- real secrets in function env vars.

CREATE TABLE IF NOT EXISTS p3_dynamodb_tables (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_dynamodb_items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_dynamodb_items_table_idx ON p3_dynamodb_items ((data->>'table_name'));
CREATE TABLE IF NOT EXISTS p3_rds_instances (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_rds_snapshots (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_rds_snapshots_instance_idx ON p3_rds_snapshots ((data->>'instance_id'));
CREATE TABLE IF NOT EXISTS p3_cache_clusters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_load_balancers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_lb_listeners (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_lb_listeners_lb_idx ON p3_lb_listeners ((data->>'load_balancer_id'));
CREATE TABLE IF NOT EXISTS p3_target_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_lb_targets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_lb_targets_tg_idx ON p3_lb_targets ((data->>'target_group_id'));
CREATE TABLE IF NOT EXISTS p3_dns_zones (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_dns_records (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_dns_records_zone_idx ON p3_dns_records ((data->>'zone_id'));
CREATE TABLE IF NOT EXISTS p3_sqs_queues (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_sqs_messages (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_sqs_messages_queue_idx ON p3_sqs_messages ((data->>'queue_id'));
CREATE TABLE IF NOT EXISTS p3_sns_topics (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_sns_subscriptions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_sns_subscriptions_topic_idx ON p3_sns_subscriptions ((data->>'topic_id'));
CREATE TABLE IF NOT EXISTS p3_sns_deliveries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_eventbridge_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_lambda_functions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_lambda_invocations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_lambda_invocations_fn_idx ON p3_lambda_invocations ((data->>'function_id'));
CREATE TABLE IF NOT EXISTS p3_apigw_apis (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p3_apigw_resources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_apigw_resources_api_idx ON p3_apigw_resources ((data->>'api_id'));
CREATE TABLE IF NOT EXISTS p3_apigw_stages (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_apigw_stages_api_idx ON p3_apigw_stages ((data->>'api_id'));
CREATE TABLE IF NOT EXISTS p3_apigw_logs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p3_apigw_logs_api_idx ON p3_apigw_logs ((data->>'api_id'));
