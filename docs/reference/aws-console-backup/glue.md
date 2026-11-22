# Glue — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `glue` |
| Name | Glue |
| Category | Analytics |
| Registry route | `/services/glue` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/AnalyticsConsoles.tsx `GlueConsole`, route `/services/glue` |
| Resource types | job, crawler |
| Learning tracks linked | databases |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Glue is the serverless data-integration service: crawlers infer schemas from S3, JDBC and DynamoDB sources into the Data Catalog; ETL jobs (Spark/Python shell) transform data between sources; triggers and workflows schedule them. A learner meets it as the 'build and maintain the data lake's catalogue and pipelines' service — the layer that makes raw S3 data queryable by Athena and visualisable by QuickSight.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Data Catalog (Databases, Tables, Connections, Crawlers, Classifiers, Schema registries), Data Integration (ETL jobs, Visual ETL, Workflows, Triggers, Dev endpoints), DataBrew, Settings.
**Home — Crawlers list:** columns Name, State (Ready / Running / Stopping), Schedule, Last run (status + duration), Tables added/updated. Filters: state, search.
**Databases list:** columns Name, Location, Description, Created. Tables list (per database): columns Name, Location, Classification, Last updated, Columns (count).
**Table detail:** schema (column name, type, comment), partitions, properties, data preview (via Athena).
**ETL jobs list:** columns Name, Type (Spark / Python shell / Streaming), Status (last run), Created. Job detail: job details, script editor (Spark script with Glue libraries), run history (table: run ID, status, started, duration, logs).
**Wizard — Create crawler:** 1. Crawler properties (name, description). 2. Data source (S3 path / JDBC / DynamoDB table; include path; sample size). 3. IAM role. 4. Schedule (on demand / cron). 5. Output (database, table prefix, configuration). 6. Review.
**Wizard — Create job:** name, IAM role, type (Spark/Python shell), Glue version, worker type + number, script location or editor, job parameters, schedule/trigger.
**Actions:** Run crawler, Create crawler/database/table, Create job, Start job run, Stop job run, Create trigger/workflow.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Data catalog crawled from real sources._
- **What the current console shows (registry mirrors note):** Crawlers list; Run-crawler action; Data Catalog tables (table / columns)
- **How it differs / what is missing (registry differs note):** Crawlers read real sources: Postgres information_schema, DynamoDB tables, S3 buckets (MinIO). A source that cannot be reached makes the run PARTIAL, never silently skipped. No ETL jobs, no Spark.
- **Console today:** Yes — frontend/src/pages/AnalyticsConsoles.tsx `GlueConsole`, route `/services/glue`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add ETL jobs and triggers, do not rebuild the crawler.** Current: crawlers read real sources (Postgres `information_schema`, DynamoDB tables, S3 via MinIO); per-source status; a run is PARTIAL if any source fails; catalog only — no ETL jobs (backend/internal/httpapi/phase6.go `p6_glue_crawlers`, `p6_glue_tables`). Console: AnalyticsConsoles.tsx `GlueConsole`. To build later, in order:
1. **ETL jobs (real, local):** a job is a Python script run through the sandboxed subprocess runner (same as Lambda) with the Glue catalog available as input: read a source table (Postgres table or S3 CSV/JSON via MinIO), transform with pandas if available (else stdlib csv/json — record which is used), write the result to a target table or S3 location, and register the output table in the catalog. Job runs are async via the worker jobs table with real logs. Label the runtime "Python shell job (local subprocess), not Spark" — Spark is out of scope.
2. **Triggers and workflows:** a trigger (on-demand, scheduled cron via the worker poller, or on job completion) starts a job run; a workflow chains jobs in order with failure handling. Cron scheduling reuses the existing jobs poller pattern.
3. **Connections:** JDBC-style connection records (Postgres connection parameters for the platform Postgres / RDS instances) used by crawlers and jobs; passwords stored envelope-encrypted like Secrets Manager.
4. **Classifiers:** custom classifiers (grok patterns / JSON / CSV) applied by crawlers when inferring schema — a small pattern table, evaluated during a crawl.
**Data model (exists, extend):** `p6_glue_crawlers`, `p6_glue_tables`; add `pX_glue_jobs` (id, name, type, script_location, worker_type, status, created_at) + `pX_glue_job_runs` (id, job_id, status, log_path, started_at, finished_at) + `pX_glue_triggers` (id, name, type, schedule, job_id, enabled) + `pX_glue_connections` (id, name, type, connection_properties jsonb, created_at).
**Labs / lessons:** add one lesson to the `databases` track: crawl an S3 bucket, then write an ETL job that cleans the data into a new table and query it in Athena. Reuse the Athena/Glue lessons.
**Lite feasibility:** Yes — Python-shell jobs run in the subprocess sandbox in lite; S3 sources need MinIO (full mode) so in lite jobs read Postgres/DynamoDB/local paths.

## 5. Lite feasibility and scope

- **Scope: medium.** Python-shell ETL jobs + triggers/workflows + connections on the existing crawler/catalog.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `glue`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
