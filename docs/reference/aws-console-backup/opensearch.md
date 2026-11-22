# OpenSearch — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `opensearch` |
| Name | OpenSearch |
| Category | Analytics |
| Registry route | `/services/opensearch` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | domain |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

OpenSearch is the managed search-and-analytics engine (the open-source fork of Elasticsearch): index documents, run full-text and aggregations queries, visualise in Dashboards, and alert on log patterns. A learner meets it as the search engine behind product search and the log-analytics engine behind operational dashboards — the 'find it fast' service.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Domains, Managed clusters, (Dashboards link per domain).
**Home — Domains list:** columns Domain name, Status (Active / Processing / Deleting), Version (OpenSearch 2.x / Elasticsearch 7.x), Created, Instance count, Data nodes. Filters: status, search.
**Domain detail tabs:** Overview (endpoint, version, instance types/counts, EBS, status), Cluster health (green/yellow/red, nodes, shards, docs), Indices (table: index name, health, status, docs, size, created), Logs (error logs, search slow logs, index slow logs), Security (fine-grained access control, master user, SAML), Snapshots (manual snapshot repository), Tags.
**Wizard — Create domain:** 1. Domain name + creation method (standard create / production). 2. Templates (production / dev-test). 3. Deployment options (domain with standby, instance types, data nodes count, storage). 4. Network (VPC / public access). 5. Fine-grained access control (master user, SAML). 6. Access policy (JSON). 7. Review.
**Actions:** Create domain, Delete domain, Create snapshot, Restore snapshot, Update domain configuration, Create index, Delete index, Open Dashboards.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Search and analytics domains._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** domains CRUD `POST/GET/DELETE /api/v1/aws/opensearch/domains`, indices CRUD `/api/v1/aws/opensearch/domains/{id}/indices`, documents `POST /api/v1/aws/opensearch/domains/{id}/indices/{index}/documents` (index), `POST .../search` (query), snapshots `/api/v1/aws/opensearch/domains/{id}/snapshots`.
**Search implementation (two stages, be honest about both):**
- **Stage 1 (lite-feasible):** a Postgres full-text search backend — documents stored as JSONB, search via `to_tsvector`/`to_tsquery` (English config) plus term filters and simple aggregations (terms agg = GROUP BY, date histogram = date_trunc buckets). Query DSL subset: `match`, `term`, `range`, `bool` (must/should/must_not), `aggs` (terms, avg, date_histogram). Anything else returns a clear error. This teaches search concepts with real results; it is NOT OpenSearch and the console says so.
- **Stage 2 (full mode only):** a real OpenSearch container (Docker, single-node) as the domain backend, with the API proxying index/search to it and the console linking to the real Dashboards UI. Gate behind the Docker capability check.
**Data model:** Postgres `pX_os_domains` (id, name, version, status, instance_type, data_nodes, storage_gb, endpoint, created_at) + `pX_os_indices` (id, domain_id, name, mappings jsonb, created_at) + `pX_os_docs` (domain_id, index_name, doc_id, doc jsonb, search_vector tsvector, created_at) + `pX_os_snapshots` (id, domain_id, name, created_at).
**Labs / lessons:** one lesson in the `databases`/analytics area: index product documents, run a full-text query with filters and a terms aggregation, compare with the equivalent Postgres query. Reuse the Athena lesson's data.
**Lite feasibility:** Stage 1 yes; Stage 2 (real OpenSearch container) no — full mode only.

## 5. Lite feasibility and scope

- **Scope: medium.** Postgres full-text search backend with a query-DSL subset (Stage 1); real OpenSearch container is Stage 2.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `opensearch`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
