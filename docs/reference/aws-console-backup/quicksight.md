# QuickSight — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `quicksight` |
| Name | QuickSight |
| Category | Analytics |
| Registry route | `/services/quicksight` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/AnalyticsConsoles.tsx `QuickSightConsole`, route `/services/quicksight` |
| Resource types | dashboard, analysis |
| Learning tracks linked | databases |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

QuickSight is the business-intelligence service: connect datasets (S3, Athena, RDS, uploaded files), build analyses with drag-and-drop visuals (bars, lines, pies, tables, KPIs), and publish dashboards that refresh on a schedule. A learner meets it as the 'turn query results into a chart someone else can read' service — the visualisation layer over Athena and Glue.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Analyses, Dashboards, Datasets, Data sources, (Admin: Manage users, SPICE capacity).
**Home — Dashboards list:** columns Name, Owner, Last modified, Shared with. Analyses list: columns Name, Owner, Last modified. Datasets list: columns Name, Source (Athena/S3/RDS/Upload), Last updated, Rows.
**Analysis editor:** left panel (Fields list from the dataset, Visual types, Filters), canvas (sheets with visuals: bar chart, line chart, pie, table, KPI, pivot), top bar (Publish as dashboard, Share, Save). Click a visual to edit its fields (X axis, Value, Colour, Filters).
**Dataset wizard — New dataset:** 1. Choose source (Athena / S3 / RDS / Upload a file). 2. Select table/file. 3. Import mode (SPICE / Direct query). 4. Edit/prepare data (rename columns, change types, calculated fields). 5. Save & visualise.
**Dashboard detail:** published visuals (read-only), Refresh button, Share, Export to PDF/CSV (per visual), Filters bar.
**Actions:** Create analysis, Create dashboard (publish), Create dataset, Share dashboard, Delete analysis/dataset, Refresh dataset.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Dashboards — real charts from live Athena results._
- **What the current console shows (registry mirrors note):** Dashboards list; Dashboard detail with chart from live query results
- **How it differs / what is missing (registry differs note):** A dashboard is a saved Athena query plus a bar/line chart rendered from the live query results — no sample data, no SPICE, no sharing.
- **Console today:** Yes — frontend/src/pages/AnalyticsConsoles.tsx `QuickSightConsole`, route `/services/quicksight`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend visuals and datasets, do not rebuild the dashboard model.** Current: a dashboard is a saved Athena query + a bar/line chart rendered from the live query results; `chart_error` when the query is missing or fails; no sample data, no SPICE, no sharing (backend/internal/httpapi/phase6.go `p6_quicksight_dashboards`). Console: AnalyticsConsoles.tsx `QuickSightConsole`. To build later, in order:
1. **More visual types:** pie/donut, KPI (single number + comparison), table, pivot table, and a line chart with multiple series — all rendered from the same Athena result rows (the frontend charting is the work; the backend stays a query + chart spec).
2. **More dataset sources:** RDS instance databases (run the saved query against an RDS instance — the Athena executor already supports RDS as a target), S3 CSV (read a CSV from MinIO into a temp Postgres table and query it), and file upload (CSV → temp table). Keep the "no SPICE" label: every dataset is a direct query, refreshed on view.
3. **Calculated fields and filters:** per-visual filters (equals, range, top-N) and calculated fields (arithmetic over result columns) evaluated in Go before rendering.
4. **Sharing:** share a dashboard with named CloudForge users (read-only); a shared dashboard appears in their Dashboards list. No public links.
5. **Scheduled refresh:** a dashboard refresh schedule (daily/hourly) that re-runs the query via the worker and caches the result; show the last-refresh time on the dashboard.
**Data model (exists, extend):** `p6_quicksight_dashboards`; add `pX_quicksight_datasets` (id, name, source_type, source_location, query, created_by, created_at) and extend dashboards with `visuals jsonb` (multiple visuals per dashboard) and `shared_with text[]`.
**Labs / lessons:** add one lesson to the `databases` track: build a dataset from an Athena query, create a dashboard with three visual types, share it with another learner. Reuse the Athena lesson.
**Lite feasibility:** Yes — direct-query dashboards already work in lite; S3 CSV datasets need MinIO (full mode) and are disabled in lite.

## 5. Lite feasibility and scope

- **Scope: medium.** More visual types + RDS/S3/file datasets + sharing on the existing dashboard model.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `quicksight`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
