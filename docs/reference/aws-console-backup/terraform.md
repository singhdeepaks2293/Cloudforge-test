# Terraform — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `terraform` |
| Name | Terraform |
| Category | Developer Tools |
| Registry route | `/services/terraform` |
| Availability today | partial |
| Fidelity today | **PARTIAL** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | run, workspace |
| Learning tracks linked | terraform, devops |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Terraform is the infrastructure-as-code execution service in CloudForge (not an AWS service): write HCL, run init/validate/plan, approve the plan, apply it, and destroy it — with runs executed in containers, state versioned, and providers pointed at the local emulator/MinIO only. A learner meets it as the declarative counterpart to CloudFormation, and as the skill the Terraform Learning product teaches.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav (mirror Terraform Cloud workspace patterns, CloudForge branding):** Runs, State, Variables, Settings, (Workspaces).
**Home — Runs list:** columns Run ID, Status (Planned / Applied / Failed / Destroyed / Awaiting approval), Triggered by, Created, Duration. Filters: status, search.
**Run detail tabs:** Plan output (the plan diff: resources to add/change/destroy with a summary line), Approvals (approve/reject buttons with the approver and timestamp, only for plans awaiting approval), Apply log (live/complete apply output), Variables (effective variables for the run), State version (link to the state snapshot this run produced).
**State view:** state file viewer (JSON, syntax-highlighted, read-only) + version history (version, run, created, Restore button creating a new version) + lock status (locked by/holder, admin force-unlock).
**Variables page:** table of variables (key, value masked for sensitive, category Terraform/environment, description), Add variable, Edit, Delete.
**Workspaces list:** columns Workspace name, Runs, Last run, Created.
**Actions:** New run (upload HCL / link a lab), Approve plan, Reject plan, Apply (after approval), Destroy, Restore state version, Force-unlock state.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: partial). Registry description: _Infrastructure as code — real containerised Terraform runs (init/validate/plan/approve/apply/destroy)._
- **What the current console shows (registry mirrors note):** Runs list; run detail (Plan output / diff, Approvals, Apply log); State view; Variables; Workspace screens
- **How it differs / what is missing (registry differs note):** Terraform runs only in containers (never on host); providers point at the local emulator/MinIO only; no Terraform Cloud, no module registry yet (Phase 6); no dedicated UI console yet — runs are API-driven and listed on the service page.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — build the missing console, do not rebuild the runner.** Current: real containerised Terraform runs (init/validate/plan/approve/apply/destroy, pinned hashicorp/terraform:1.9.8, Docker Engine API v1.41, execution is REAL, infrastructure managed is the local emulator/simulated stack; container execution unverified in this sandbox — honest 503 `terraform_unavailable` without Docker) (backend/internal/terraform/terraform.go, backend/internal/jobs/terraform.go, httpapi/terraform.go). There is NO Terraform console page — Terraform is API-only today (IMPLEMENTATION_STATUS §2.6). The Terraform Learning product (separate registry entry) has a console; the Terraform service itself does not. To build later:
1. **Console (the main gap):** a Terraform service console following the layout above, reusing the existing components (AwsTable, AwsBadge, ServiceConsole shell) and the existing Terraform API routes (list runs, run detail, approve, apply, destroy, state, variables). No new backend endpoints are needed for v1 of the console — verify each route the console needs exists first, and add only the missing ones (e.g. a variables list endpoint if absent).
2. **Plan-diff rendering:** the plan JSON is already parsed + validated; render the diff as a resource table (add/change/destroy/no-op with the changed attributes) instead of raw JSON.
3. **Workspace scoping:** runs are already per-lab; expose workspaces as the grouping (one workspace per lab, plus a personal workspace) in the console.
4. **Provider coverage note:** the console must show which provider resources the local emulator supports (the 9 `cloudforge_*` types) and refuse others at plan time with the reason — this list comes from the provider implementation, not from a hard-coded UI list.
**Data model:** exists (Terraform runs/jobs/state tables); no new tables for v1.
**Labs / lessons:** the Terraform Learning product owns the lessons; the service console is the surface those lessons run against. Add one cross-link: every Terraform Learning run deep-links to its run in this console.
**Lite feasibility:** No for execution (containers need Docker — the honest 503 stays); Yes for the console itself (viewing runs/state/variables works in lite, with Apply disabled and the reason shown).

## 5. Lite feasibility and scope

- **Scope: medium.** Service console (runs/state/variables) over the existing runner API; execution stays Docker-gated.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `terraform`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
