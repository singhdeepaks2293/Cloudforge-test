-- Phase 4 (CloudForge v2) — containers and developer tools.
--
-- Same generic-entity shape as migration 009 (one p4_* table per
-- collection: id, unique-where-required name, data JSONB). The
-- Phase 3 store (store.Phase3Store) serves these collections too;
-- see store/phase3.go P3Collections for the allowlist.
--
-- Collections and what `data` holds:
--   p4_ecr_repos, p4_ecr_images (ECR-style registry catalogue;
--     image layers themselves live on local disk under
--     <workdir>/ecr/blobs/sha256/<digest> — content-addressed —
--     or in an external registry:2 when ECR_REGISTRY_URL is set),
--   p4_ecs_clusters, p4_ecs_taskdefs, p4_ecs_services, p4_ecs_tasks
--     (ECS-style orchestration; tasks run as sandboxed subprocesses
--     on this host — SIMULATED runtime, honestly labelled),
--   p4_codebuild_projects, p4_codebuild_builds (real local builds:
--     git checkout from a local path + buildspec steps in a
--     subprocess, logs and artifacts on disk),
--   p4_codepipeline_pipelines, p4_codepipeline_executions
--     (source -> build -> deploy stages),
--   p4_codedeploy_apps, p4_codedeploy_deployments (deployments
--     with real status and rollback to the previous revision),
--   p4_cfn_stacks, p4_cfn_changesets, p4_cfn_events
--     (CloudFormation-style stacks: own template engine + stack
--     state, provisioning through the existing Phase 3 resources).
--
-- EKS is NOT here: the Phase 4 decision is UNSUPPORTED (no local
-- k3s/kind cluster — see docs/IMPLEMENTATION_STATUS.md §13), with
-- a Kubernetes-concepts learning path served by the ECS runtime.

CREATE TABLE IF NOT EXISTS p4_ecr_repos (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_ecr_images (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_ecr_images_repo_idx ON p4_ecr_images ((data->>'repository_id'));

CREATE TABLE IF NOT EXISTS p4_ecs_clusters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_ecs_taskdefs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_ecs_taskdefs_family_idx ON p4_ecs_taskdefs ((data->>'family'));
CREATE TABLE IF NOT EXISTS p4_ecs_services (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_ecs_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_ecs_tasks_service_idx ON p4_ecs_tasks ((data->>'service_id'));
CREATE INDEX IF NOT EXISTS p4_ecs_tasks_cluster_idx ON p4_ecs_tasks ((data->>'cluster_id'));

CREATE TABLE IF NOT EXISTS p4_codebuild_projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_codebuild_builds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_codebuild_builds_project_idx ON p4_codebuild_builds ((data->>'project_id'));

CREATE TABLE IF NOT EXISTS p4_codepipeline_pipelines (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_codepipeline_executions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_codepipeline_executions_pipeline_idx ON p4_codepipeline_executions ((data->>'pipeline_id'));

CREATE TABLE IF NOT EXISTS p4_codedeploy_apps (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_codedeploy_deployments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_codedeploy_deployments_app_idx ON p4_codedeploy_deployments ((data->>'application_id'));

CREATE TABLE IF NOT EXISTS p4_cfn_stacks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS p4_cfn_changesets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_cfn_changesets_stack_idx ON p4_cfn_changesets ((data->>'stack_id'));
CREATE TABLE IF NOT EXISTS p4_cfn_events (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS p4_cfn_events_stack_idx ON p4_cfn_events ((data->>'stack_id'));
