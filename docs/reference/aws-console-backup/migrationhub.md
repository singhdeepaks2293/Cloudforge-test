# Migration Hub — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `migrationhub` |
| Name | Migration Hub |
| Category | Migration and Transfer |
| Registry route | `/services/migrationhub` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/MigrationConsoles.tsx `MigrationHubConsole`, route `/services/migrationhub` |
| Resource types | migration-task |
| Learning tracks linked | databases |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

Migration Hub is the single pane for a migration: it tracks servers, applications and migration tasks from DMS, DataSync and Application Migration, and shows progress toward the migration goal. A learner meets it as the 'are we done migrating yet?' dashboard — the aggregation layer over the individual migration tools, not a migration tool itself.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Dashboard, Migrate (Servers, Applications), Track (Migration tasks), Discover (Discovery data, Connectors).
**Home — Dashboard:** migration summary cards (servers discovered, applications, tasks by status), progress chart, recent tasks.
**Migration tasks list:** columns Task name, Status (Not started / In progress / Completed / Failed), Type (DMS / DataSync / MGN), Source, Target, Started, Progress (%). Filters: type, status.
**Servers list:** columns Server name, Status, Applications (count), Last seen. Applications list: columns Application name, Servers (count), Migration status.
**Task detail:** progress bar, associated resources (source/target), status history, links out to the underlying DMS/DataSync task.
**Actions:** Create migration task (choose type → jumps to the DMS/DataSync/MGN creation flow), Import discovery data, Register server/application, Update task status.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Tracking for the real DMS and DataSync tasks._
- **What the current console shows (registry mirrors note):** Migration tasks list (DMS + DataSync) with status summary
- **How it differs / what is missing (registry differs note):** Migration Hub tracks the real DMS and DataSync tasks — it creates nothing of its own.
- **Console today:** Yes — frontend/src/pages/MigrationConsoles.tsx `MigrationHubConsole`, route `/services/migrationhub`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add servers/applications and progress roll-up, do not rebuild the aggregation.** Current: Migration Hub tracks the real DMS and DataSync tasks — it creates nothing of its own (backend/internal/httpapi/phase6.go `GET /api/v1/aws/migrationhub/tasks`). Console: MigrationConsoles.tsx `MigrationHubConsole`. To build later, in order:
1. **Servers and applications:** `pX_migrationhub_servers` (id, name, hostname, os, status, last_seen) + `pX_migrationhub_applications` (id, name, description, server_ids text[], migration_status) + endpoints to register/list them; servers can be imported from the SSM managed-instances list (real host-agent inventory) — one click "Import from Systems Manager".
2. **Task progress roll-up:** progress % for a DMS task = migrated rows / total rows (already recorded); for DataSync = verified files / total files; roll up to application level (average of its tasks) and to the dashboard. The dashboard cards are computed from these, never hard-coded.
3. **Task creation:** "Create migration task" stays a chooser that deep-links to the DMS/DataSync task wizards with the application pre-selected; the created task is then linked to the application.
4. **Discovery import:** accept a CSV of servers (name, hostname, OS, applications) and create server/application records — the local substitute for the Discovery Connector.
**Data model (extend):** existing aggregation endpoint; add the two tables above + `pX_migrationhub_task_links` (id, task_type, task_id, application_id).
**Labs / lessons:** one lesson in the `databases`/migration area: register two servers, link a DMS task, watch the dashboard progress hit 100%. Reuse the DMS/DataSync lessons.
**Lite feasibility:** Yes — pure Postgres aggregation; no Docker/KVM.

## 5. Lite feasibility and scope

- **Scope: small.** Servers/applications records + progress roll-up on the existing aggregation endpoint.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `migrationhub`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
