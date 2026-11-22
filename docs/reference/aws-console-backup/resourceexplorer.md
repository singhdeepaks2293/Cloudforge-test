# Resource Explorer — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `resourceexplorer` |
| Name | Resource Explorer |
| Category | Management and Governance |
| Registry route | `/explorer` |
| Availability today | available |
| Fidelity today | **REAL** |
| CloudForge console today | Yes — frontend/src/pages/Explorer.tsx, route `/explorer` |
| Resource types | lab, vpc, subnet, instance, gateway, loadbalancer, route-table, security-group, course, user |
| Learning tracks linked | none linked |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

Resource Explorer is CloudForge's own cross-service search (mirroring the AWS service of the same name): one search box over every resource the platform knows — labs, lab resources, courses, users — instead of hunting through each service console. A learner meets it as the 'where is that thing?' tool, and an instructor meets it as the inventory view of a classroom.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav (CloudForge product):** Search, Views, (Saved views).
**Home — Search:** big search box with filters (resource type: lab/vpc/subnet/instance/gateway/loadbalancer/route-table/security-group/course/user; status; region — display-only `cf-local-1`), results table: columns Resource (name + ID), Type, Region, Status, Created, Detail link. Sort by name/created/status; pagination (max 100 per page).
**Views:** saved views (name + filter definition), Create view wizard (name, filters), Apply view.
**Actions:** Search, Filter, Sort, Create view, Export results (CSV), Open resource detail.

## 3. CloudForge fidelity today

- **Fidelity: REAL** (availability: available). Registry description: _Cross-service search over real local resources._
- **What the current console shows (registry mirrors note):** Cross-service resource search/list (the §4 Resource Explorer area)
- **How it differs / what is missing (registry differs note):** Searches local lab resources only; region scoping is display-only in Phase 1 (all resources live in cf-local-1).
- **Console today:** Yes — frontend/src/pages/Explorer.tsx, route `/explorer`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend filters, views and coverage, do not rebuild search.** Current: `GET /api/v1/explorer/resources` lists real data only (own labs, lab resources, courses, users admin-only); filters `q`/`type`/`status`, sort `name`/`created_at`/`status`, pagination ≤100; region scoping is display-only (every row is `cf-local-1`, another region returns an empty list + a note) (backend/internal/httpapi/services.go `handleExplorerResources`). Console: Explorer.tsx. Fidelity REAL. To build later, in order:
1. **More resource types:** index the Phase 2–6 service resources — S3 buckets, DynamoDB tables, SQS queues, Lambda functions, ECS services, IAM users, RDS instances, CloudWatch alarms — each as an explorer row with its service id and detail URL. The explorer becomes the single inventory across all services. Each type needs a lister that reads the real store (no synthesised rows — the existing rule).
2. **Saved views:** `pX_explorer_views` (id, user_id, name, filters jsonb, created_at) + CRUD endpoints; views are per-user.
3. **Tag search:** filter by tag key/value (resources carry labels — the explorer already returns tags; add a `tag:` query syntax).
4. **Export:** CSV export of the current filtered result set (same shape as the audit export, cap 1,000 rows).
5. **Multi-region display:** regions stay display-only (all resources live in cf-local-1); if a future phase adds per-lab regions, the explorer is the first place to honour them — do not fake it before then.
**Data model (add):** `pX_explorer_views` only; everything else reads existing stores.
**Labs / lessons:** one short lesson in the `fundamentals` track: find a resource three ways (search, type filter, saved view) and export the inventory. No new content format needed.
**Lite feasibility:** Yes — the explorer is already lite's inventory view; new resource types are only listed when their service works in lite.

## 5. Lite feasibility and scope

- **Scope: small.** Index Phase 2–6 service resources + saved views + tag search on the existing endpoint.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `resourceexplorer`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
