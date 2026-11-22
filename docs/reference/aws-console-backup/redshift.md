# Redshift — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `redshift` |
| Name | Redshift |
| Category | Databases |
| Registry route | `/services/redshift` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | cluster |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Redshift is the data-warehouse service: a columnar, massively parallel database for analytical queries over terabytes, with provisioned clusters or serverless workgroups, a query editor, datashares, and integration with the data lake (Spectrum over S3). A learner meets it as the 'real warehouse' — the place where Athena's interactive queries become scheduled, governed analytics at scale.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Clusters, Serverless (Workgroups, Namespaces), Query editor v2, Snapshots, Reserved nodes, Advisor, Datashares.
**Home — Clusters list:** columns Cluster identifier, Status (Available / Creating / Modifying / Resizing / Paused), Node type, Nodes, Created. Filters: status, search.
**Cluster detail tabs:** Properties (endpoint, port, database, master username, node type/count, encryption, maintenance), Workload (query monitoring: running queries, queues), Queries & loads (query history table: query ID, status, duration, rows, user), Datashares (inbound/outbound), Maintenance (snapshots, parameter groups, events), Monitoring (CPU, storage, query latency graphs).
**Serverless — Workgroups list:** columns Workgroup name, Status, Namespace, Base capacity (RPU), Created. Namespaces list: columns Namespace, Database, Admin user.
**Query editor v2:** database selector, SQL editor (with history, saved queries, notebooks), results table, query plan (EXPLAIN), export.
**Snapshots list:** columns Snapshot identifier, Cluster, Status, Created, Size. Actions: Restore, Delete.
**Wizard — Create cluster:** 1. Cluster identifier + node type (ra3/dc2) + number of nodes. 2. Database configuration (database name, admin username/password). 3. Cluster permissions (IAM role for S3/Spectrum). 4. Network and security (VPC, security groups, publicly accessible, encryption). 5. Backup (automated snapshot retention, manual snapshots). 6. Maintenance (window, version). 7. Review.
**Wizard — Create serverless workgroup:** namespace + admin, base capacity (RPU), network/security, review.
**Actions:** Create cluster, Query (open editor), Create snapshot, Restore from snapshot, Resize cluster, Pause/Resume cluster, Reboot, Delete cluster, Create datashare.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Data warehouse clusters and query editor._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5 decision).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** clusters CRUD `POST/GET/DELETE /api/v1/aws/redshift/clusters`, serverless workgroups/namespaces CRUD `/api/v1/aws/redshift/workgroups`, snapshots `/api/v1/aws/redshift/clusters/{id}/snapshots` + restore, query `POST /api/v1/aws/redshift/clusters/{id}/query` (SQL), datashares CRUD.
**Warehouse implementation (Postgres, honest subset):** a Redshift cluster is a Postgres database + role on the platform Postgres (exactly the RDS pattern — one database per cluster, columnar storage is NOT emulated and the console says so). The query path is the Athena executor pointed at the cluster database, with a Redshift-flavoured validator (allow the same read-only SELECT/WITH plus `COPY` from S3 via MinIO and `UNLOAD` to S3 as the two Redshift-specific statements, implemented as import/export through the existing S3 paths). Serverless workgroups are the same thing with a capacity label (RPU recorded, not enforced as real compute). Datashares = read access grants on a cluster database to another CloudForge user (a Postgres GRANT under the hood, recorded in a shares table). Snapshots = `pg_dump -Fc`, same as RDS. Pause/resume: pause disconnects the database (revoke connect) and resume restores it — record the state honestly; resize changes the recorded node count (no real re-provisioning — say so).
**Data model:** Postgres `pX_redshift_clusters` (id, identifier, node_type, node_count, status, database_name, master_username, master_password_hash, endpoint, created_at) + `pX_redshift_workgroups` (id, name, namespace, base_capacity_rpu, status, created_at) + `pX_redshift_snapshots` (id, cluster_id, name, file_path, created_at) + `pX_redshift_datashares` (id, name, cluster_id, shared_with_user_id, objects jsonb, created_at).
**Labs / lessons:** one lesson in the `databases` track: create a cluster, COPY data from S3, run an analytical query with a window function, snapshot and restore; compare query time with Athena on the same data. Reuse the RDS/Athena lessons.
**Lite feasibility:** Yes — Postgres-backed; COPY/UNLOAD to S3 need MinIO (full mode) and are disabled in lite.

## 5. Lite feasibility and scope

- **Scope: medium.** Cluster/workgroup model on the RDS Postgres pattern + Redshift-flavoured query path (COPY/UNLOAD).
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `redshift`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
