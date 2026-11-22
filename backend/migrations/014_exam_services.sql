-- 014_exam_services.sql — v3 exam services (SAA-C03 / SAP-C02) in lite mode.
--
-- The 24 exam services added in v3 (Auto Scaling, Global Accelerator,
-- Direct Connect, Control Tower, IAM Identity Center, RAM, Storage
-- Gateway, Snowball, Lake Formation, Macie, Compute Optimizer, Savings
-- Plans, Audit Manager, Artifact, Aurora, Backup, MGN, FSx, Shield,
-- EBS, EFS, CloudFront, VPN, NACLs) share ONE generic-entity table,
-- p7_exam_services, served by the Phase 3 store as collection
-- "exam_services" (see store/phase3.go P3Collections). They share it
-- deliberately: every one of these services is a teaching-scale record
-- store with identical CRUD semantics (create/list/get/delete plus one
-- or two small actions), and the service discriminator lives in the
-- document (`data->>'service'`). One table keeps the migration small
-- and the lite Postgres (64 MB buffers, 20 connections) unburdened;
-- a service that later grows real behaviour of its own (like Kinesis
-- did in Phase 6) gets its own collection then.
--
-- Name uniqueness is NOT enforced by this table: names are unique
-- per (service, owner) only, enforced in the HTTP handler
-- (backend/internal/httpapi/exam.go), because two different services
-- may legitimately share a name (e.g. a "web" Auto Scaling group and
-- a "web" EBS volume). The indexes support the two hot queries:
-- list-by-service and list-by-service-and-owner.
CREATE TABLE IF NOT EXISTS p7_exam_services (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_p7_exam_services_service
    ON p7_exam_services ((data ->> 'service'));
CREATE INDEX IF NOT EXISTS idx_p7_exam_services_service_owner
    ON p7_exam_services ((data ->> 'service'), (data ->> 'owner_id'));
