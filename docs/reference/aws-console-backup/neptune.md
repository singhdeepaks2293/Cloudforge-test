# Neptune — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `neptune` |
| Name | Neptune |
| Category | Databases |
| Registry route | `/services/neptune` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | cluster |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Neptune is the managed graph database: store vertices and edges and query them with Gremlin or openCypher (property graph) or SPARQL (RDF), with read replicas and point-in-time recovery. A learner meets it as the database for connected data — recommendations, fraud rings, knowledge graphs, identity graphs — where joins in a relational database become painful and a graph traversal is the natural query.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Clusters (Databases), Instances, Snapshots, Subnet groups, Parameter groups, Events, (Notebooks — Neptune notebooks).
**Home — Clusters list:** columns Cluster identifier, Status (Available / Creating / Modifying / Failed), Engine (neptune 1.x), Writer instance, Reader instances (count), Created. Filters: status, search.
**Cluster detail tabs:** Connectivity & security (cluster endpoint, reader endpoint, port 8182, VPC, security groups, IAM authentication), Instances (table: instance identifier, class, role writer/reader, status), Snapshots (create/restore), Logs & events (audit logs, slow-query logs), Configuration (parameter group, maintenance window, backup retention), Maintenance & backups.
**Wizard — Create cluster:** 1. Engine options (engine version). 2. Configuration (cluster identifier, master username/password — for IAM auth, instance class, number of instances). 3. Connectivity (VPC, subnet group, security groups, publicly accessible). 4. Additional configuration (backup retention, maintenance window, encryption, audit logs, tags). 5. Review.
**Actions:** Create cluster, Add instance (reader), Create snapshot, Restore from snapshot, Reboot instance, Failover (promote a reader), Modify cluster, Delete cluster, Run query (Gremlin/openCypher/SPARQL in the query workbench).

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Graph database clusters._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** clusters CRUD `POST/GET/DELETE /api/v1/aws/neptune/clusters`, instances `/api/v1/aws/neptune/clusters/{id}/instances`, snapshots `/api/v1/aws/neptune/clusters/{id}/snapshots` + restore, graph API `POST /api/v1/aws/neptune/clusters/{id}/query` (Gremlin subset / openCypher subset), `POST .../vertices`, `POST .../edges`.
**Graph storage (Postgres, honest subset):** vertices and edges as Postgres rows (`pX_neptune_vertices`: cluster_id, vertex_id, label, properties jsonb; `pX_neptune_edges`: cluster_id, edge_id, label, from_id, to_id, properties jsonb) with GIN indexes on properties. Query language: implement a small subset — Gremlin-style traversals (`g.V().hasLabel('person').out('knows')`) and openCypher-style `MATCH (a:Label)-[:REL]->(b:Label) RETURN a,b` — parsed in Go into SQL over the two tables; anything outside the subset returns a clear "unsupported traversal" error with the supported list. This is a graph store over Postgres, NOT Neptune — label it PARTIAL and list the unsupported query forms. Cluster/instance lifecycle reuses the RDS pattern (one Postgres database + role per cluster for the cluster record; readers are recorded, not real replicas). Snapshots = `pg_dump -Fc` of the graph tables, same as RDS.
**Data model:** `pX_neptune_clusters` (id, identifier, engine_version, status, master_username, master_password_hash, instance_class, instances, endpoint, created_at) + `pX_neptune_instances` (id, cluster_id, identifier, class, role, status) + vertices/edges tables above + `pX_neptune_snapshots` (id, cluster_id, name, file_path, created_at).
**Labs / lessons:** one lesson in the `databases` track: model a small social graph, run a 2-hop traversal, find the shortest path between two people, compare with the equivalent SQL joins in RDS. Reuse the RDS snapshot lesson.
**Lite feasibility:** Yes — Postgres-backed, no Docker/KVM. The query-subset parser is the main work.

## 5. Lite feasibility and scope

- **Scope: medium.** Vertex/edge store + Gremlin/openCypher query subset over Postgres; snapshots reuse RDS pattern.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `neptune`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
