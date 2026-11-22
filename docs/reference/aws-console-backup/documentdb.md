# DocumentDB — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `documentdb` |
| Name | DocumentDB |
| Category | Databases |
| Registry route | `/services/documentdb` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | cluster |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

DocumentDB is the managed document database, compatible with the MongoDB API: store JSON-like documents in collections, query them with MongoDB-style queries, and scale reads with replica instances. A learner meets it as the document-database alternative to DynamoDB (richer queries, secondary indexes) and to self-hosted MongoDB, and as the database behind document-oriented applications.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Clusters, Instances, Snapshots, Subnet groups, Parameter groups, Events.
**Home — Clusters list:** columns Cluster identifier, Status (Available / Creating / Modifying / Failed), Engine (docdb 4.0/5.0), Writer instance, Reader instances (count), Created. Filters: status, search.
**Cluster detail tabs:** Connectivity & security (cluster endpoint, reader endpoint, port, VPC, security groups, TLS, master username), Instances (table: instance identifier, class, role writer/reader, status), Snapshots (manual + automated, create/restore), Logs & events (profiler/slow-query logs, events), Configuration (parameter group, maintenance window, backup retention), Maintenance & backups.
**Wizard — Create cluster:** 1. Engine options (engine version). 2. Configuration (cluster identifier, master username/password, instance class, number of instances). 3. Connectivity (VPC, subnet group, security groups, publicly accessible). 4. Additional configuration (backup retention, maintenance window, encryption, tags). 5. Review.
**Actions:** Create cluster, Add instance (reader), Create snapshot, Restore from snapshot, Reboot instance, Failover (promote a reader), Modify cluster, Delete cluster.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Document database clusters._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** clusters CRUD `POST/GET/DELETE /api/v1/aws/documentdb/clusters`, instances `/api/v1/aws/documentdb/clusters/{id}/instances`, snapshots `/api/v1/aws/documentdb/clusters/{id}/snapshots` + restore, plus a document API `POST /api/v1/aws/documentdb/clusters/{id}/collections/{name}/documents` (insert/find/update/delete) speaking a MongoDB-like JSON query subset.
**Storage reuse:** store documents as JSONB rows in Postgres (one table per cluster: `pX_docdb_docs` with cluster_id, collection, doc_id, doc jsonb, updated_at) and implement the query subset in Go: equality, `$gt/$lt/$gte/$lte/$in`, `$and/$or`, projection, sort, limit, and dot-notation field paths. Secondary indexes = Postgres GIN indexes on the JSONB column. This is a document store over Postgres, NOT MongoDB — label it PARTIAL and list the unsupported operators on the console. Cluster instances = the same "real Postgres database + role" pattern as RDS (one database per cluster) for the cluster record; readers are additional connections, not real replicas — say so. Snapshots = `pg_dump -Fc` of the cluster's documents, same as RDS snapshots.
**Data model:** `pX_docdb_clusters` (id, identifier, engine_version, status, master_username, master_password_hash, instance_class, instances, endpoint, created_at) + `pX_docdb_instances` (id, cluster_id, identifier, class, role, status) + `pX_docdb_docs` (cluster_id, collection, doc_id, doc jsonb, updated_at) + `pX_docdb_snapshots` (id, cluster_id, name, file_path, created_at).
**Labs / lessons:** one lesson in the `databases` track: create a cluster, insert documents, run queries with filters/projection/sort, add a reader, snapshot and restore. Compare with the DynamoDB lesson (same data, different query model).
**Lite feasibility:** Yes — Postgres-backed, no Docker/KVM. The MongoDB wire protocol is out of scope; the API is JSON over HTTP.

## 5. Lite feasibility and scope

- **Scope: medium.** JSONB document store + MongoDB-like query subset over Postgres; snapshots reuse RDS pattern.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `documentdb`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
