# Athena — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `athena` |
| Name | Athena |
| Category | Analytics |
| Registry route | `/services/athena` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/AnalyticsConsoles.tsx `AthenaConsole`, route `/services/athena` |
| Resource types | query |
| Learning tracks linked | databases |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Athena is the serverless interactive query service: write SQL, run it against data in object storage or catalogued tables, and get results without provisioning a database or cluster. A learner meets it as the 'ask questions of your data lake' tool — the query layer over the Glue Data Catalog and S3 — and as the engine behind QuickSight dashboards in CloudForge.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Query editor, Saved queries, Query history, Workgroups, Data sources, Settings.
**Home — Query editor:** left panel Database selector + table list (expand a table to see columns and types, preview table); centre SQL editor (syntax highlighting, Format query, Run, Save as, Create table from query); results pane below (Results table with column headers, Download results as CSV, result location, query stats: runtime, data scanned, output location). Tabs in the results area: Results / Query details.
**Saved queries list:** columns Name, Description, Database, Created, Last modified. Actions: Run, Edit, Delete.
**Query history / executions list:** columns Query ID, Status (SUCCEEDED / FAILED / CANCELLED), Runtime, Data scanned, Started, Query text (truncated). Click a row for full SQL + results.
**Workgroups list:** columns Name, State, Engine version, Bytes scanned cut-off, Created. Create workgroup wizard: name, query result location, bytes-scanned limit, engine version, tags.
**Settings:** query result location (S3 path), workgroup defaults, encryption.
**Actions:** Run query, Save query, Create workgroup, Cancel query, Download results.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Interactive SQL queries — real PostgreSQL SQL, not Presto/Athena._
- **What the current console shows (registry mirrors note):** Query editor; Saved queries; Query history / executions; Results table
- **How it differs / what is missing (registry differs note):** PARTIAL: queries run read-only (SELECT/WITH only, single statement) against the platform Postgres or an RDS instance database. Saved queries and execution history are durable. There is no Presto/Athena engine.
- **Console today:** Yes — frontend/src/pages/AnalyticsConsoles.tsx `AthenaConsole`, route `/services/athena`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend, do not rebuild.** Current: `POST /api/v1/aws/athena/queries/{id}/execute` runs read-only SELECT/WITH against platform Postgres or an RDS instance DB; saved queries + execution history are durable (backend/internal/httpapi/phase6.go, `p6_athena_*`). Missing pieces to build later:
- **Workgroups:** `pX_athena_workgroups` table + CRUD endpoints + per-workgroup result location and bytes-scanned cutoff enforced in the executor (estimate scanned bytes from the Postgres query plan / row count; cut off honestly when exceeded).
- **CTAS / DDL:** currently read-only SQL only. Add `CREATE TABLE ... AS` against a Postgres schema namespaced per workgroup, plus `DESCRIBE`/`SHOW TABLES` support in the validator — keep the single-statement, read-only-or-CTAS rule and reject everything else.
- **Catalog browsing:** the query editor's table list should read the Glue catalog (`p6_glue_tables`) as well as Postgres `information_schema`, so Athena queries can target crawled tables.
- **Results export:** CSV download already exists; add result caching (re-run from stored results) and query history filters.
**Data model (add):** `pX_athena_workgroups` (id, name, result_location, bytes_scanned_cutoff, engine_version, created_by, created_at); extend executions with `workgroup_id`, `bytes_scanned`, `result_location`.
**Labs / lessons:** add one lesson to the `databases` track: crawl an S3 bucket with Glue, then query the catalogued table in Athena and build a QuickSight chart from it. One break-and-fix: a query that scans too much data and hits the workgroup cutoff.
**Lite feasibility:** Yes — Postgres-backed execution already works in lite; workgroups and CTAS add no new infrastructure.

## 5. Lite feasibility and scope

- **Scope: medium.** Workgroups + CTAS + Glue-catalog browsing on the existing Athena executor.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `athena`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
