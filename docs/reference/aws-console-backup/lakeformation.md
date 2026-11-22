# Lake Formation — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `lakeformation` |
| Name | Lake Formation |
| Category | Analytics |
| Registry route | `/services/lakeformation` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | permission |
| Learning tracks linked | aws-sap |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Lake Formation is the data-lake governance service: register S3 locations, define databases and tables in the Data Catalog, and grant fine-grained permissions (database, table, column, row-level via LF-Tags) to principals — so Athena, Glue and Redshift queries are authorised centrally instead of per-service. A learner meets it as the 'who can see which columns' layer of the data lake, and as the governance answer to sharing data across teams.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Data catalog (Databases, Tables, Data locations), Permissions (Data permissions, Data locations, LF-Tags), Administration (Administrative roles and tasks, Data lake settings, Resource sharing).
**Home — Databases list:** columns Database name, Location, Description, Created. Tables list (per database): columns Table name, Database, Location, Classification, Columns.
**Data permissions list:** columns Principal, Resource (database/table/column), Permissions (Select / Describe / Alter / Drop / Insert / Delete / Grantable), Granted by. Filters: principal, resource type. Grant wizard: principals (IAM users/roles), LF-Tags or named catalog resources (catalog → database → table → columns), table permissions (Select/Describe/Alter/Drop/Insert/Delete + grantable), review and Grant.
**Data locations list:** columns S3 path, IAM role, Permissions. Register location wizard: S3 path, IAM role, hybrid access mode.
**LF-Tags list:** columns Tag key, Values, Created. Create LF-Tag wizard: key, values; assign tags to resources (database/table/column).
**Administration — Data lake settings:** default permissions for new databases/tables (use only IAM access control on/off), cross-account version settings, query-engine settings.
**Actions:** Grant, Revoke, Register location, Create database/table, Create LF-Tag, Add data lake administrator, Update settings.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Fine-grained data-lake permissions._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** databases/tables are the Glue catalog (reuse `p6_glue_tables` — do not duplicate the catalog); permissions CRUD `POST/GET/DELETE /api/v1/aws/lakeformation/permissions`, data locations CRUD `/api/v1/aws/lakeformation/locations`, LF-Tags CRUD `/api/v1/aws/lakeformation/tags` + assignments `/api/v1/aws/lakeformation/tags/assign`, settings `GET/PUT /api/v1/aws/lakeformation/settings`, plus an enforcement hook.
**Enforcement (the real work):** Lake Formation is only meaningful if something checks it. Implement a permission-check function used by the Athena executor and the Glue crawler: before running a query (or crawling a table), resolve the caller's CloudForge user → principals (user + groups + roles) → grants on the catalog resource (database/table/columns) and deny with a clear error when no grant covers the access, honouring column-level grants by filtering the selected columns and LF-Tags by matching tag assignments. Default-deny when Lake Formation mode is enabled for a database; when it is off (the current default), everything is allowed and the console says IAM-only mode. This is a genuine authorisation layer over the existing catalog — the most valuable part of the build.
**Data model:** Postgres `pX_lf_permissions` (id, principal_type, principal_id, resource_type, catalog, database_name, table_name, columns text[], permissions text[], grantable bool, granted_by, created_at) + `pX_lf_locations` (id, s3_path, iam_role, hybrid_access, created_at) + `pX_lf_tags` (id, tag_key, tag_values text[], created_at) + `pX_lf_tag_assignments` (id, tag_id, resource_type, database_name, table_name, column_name) + `pX_lf_settings` (id, use_only_iam bool, created_at).
**Labs / lessons:** one lesson in the `databases` track: register a location, grant Select on one table to one learner and deny another, verify in Athena that the second learner's query is refused; then grant a column-level permission and watch the column disappear from results.
**Lite feasibility:** Yes — pure Go + Postgres; enforcement hooks into the already-lite Athena executor.

## 5. Lite feasibility and scope

- **Scope: medium.** Permission/tag model + enforcement hook in the Athena executor and Glue crawler.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `lakeformation`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
