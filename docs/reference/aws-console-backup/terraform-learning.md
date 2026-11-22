# Terraform Learning — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `terraform-learning` |
| Name | Terraform Learning |
| Category | Terraform Learning |
| Registry route | `/learn` |
| Availability today | partial |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/TerraformLearning.tsx, routes `/terraform-learning` and `/services/terraform-learning` |
| Resource types | run, module |
| Learning tracks linked | terraform |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Terraform Learning is CloudForge's own teaching product for Terraform (not an AWS service): structured courses (L1 foundations, L2 intermediate, L3 advanced, L4 projects), a browser HCL editor with local validation/formatting/plan preview, state and provider teaching, and achievements. A learner meets it as the guided path from 'what is HCL?' to 'I applied a real plan', and an instructor meets it as the course with hands-on runs attached.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Dashboard, Courses (L1/L2/L3), Projects (L4), Runs, Achievements, (Editor is inside a lesson/project).
**Home — Dashboard:** progress cards (lessons completed, projects completed, runs applied, XP), Continue where you left off, recent runs.
**Courses list:** L1 (15 lessons), L2 (20), L3 (20) — each lesson row: title, duration, type (concept / hands-on / quiz), completed checkmark. Lesson page: content + HCL editor (syntax highlighting, Format, Validate, Plan preview) + Run panel (link a lab, apply/destroy with confirmations).
**Projects (L4):** 7 projects — each with a brief, starter HCL, requirements checklist, and a run history.
**Runs list:** columns Run ID, Lesson/Project, Status, Created. Run detail: plan preview (static local diff), approvals, apply log.
**Achievements:** list with earned/locked states.
**Actions:** Start lesson, Validate HCL, Format HCL, Preview plan, Apply (approved plan + confirm), Destroy (confirm), Restore state, Claim achievement.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: partial). Registry description: _Terraform concepts with hands-on runs._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** No module registry / workspace UI yet (Phase 6).
- **Console today:** Yes — frontend/src/pages/TerraformLearning.tsx, routes `/terraform-learning` and `/services/terraform-learning`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add the module registry and workspace UI, do not rebuild the learning engine.** Current: L1 15 + L2 20 + L3 20 lessons, L4 7 projects; HCL validation/formatting/static plan diff/drift; 9 `cloudforge_*` provider types with API mappings; 25 routes; dashboard/progress/projects/runs/achievements are real backend data; local validate/format/plan-preview are real local checks labelled "local" (the preview is a static diff, not a terraform binary plan); container execution delegates to the M7 runner through a linked lab (honest 503 `terraform_unavailable` without Docker); state is append-only versioned snapshots refreshed from the linked lab's latest succeeded container plan; locks are 10-minute with holder/admin force-unlock (backend/internal/tflearn/, httpapi/tflearn.go, migration 011). Console: TerraformLearning.tsx. Fidelity PARTIAL. To build later, in order:
1. **Module registry:** `pX_tflearn_modules` (id, name, description, source, versions jsonb, created_at) + publish/browse/use flow — a module is a versioned HCL snippet learners can reference from their projects; the registry is local-only (no Terraform Registry network access).
2. **Workspace UI:** the service console gap (see the `terraform` backup file) — Terraform Learning runs deep-link to the Terraform service console; the learning product keeps its own Runs list as the learner-facing view.
3. **Policy teaching (Sentinel/OPA-style):** a small policy checker over the static plan diff (e.g. "no resource without tags", "no public S3 bucket") with pass/fail per policy — pure Go over the diff, labelled as teaching policies, not Sentinel.
4. **Drift detection UI:** the drift check exists in code (hcl.go); surface it as a "Check drift" button on a project with a results table.
5. **Multi-file projects:** today multi-file projects are concatenated into one document for container execution; add per-file tabs in the editor and a file tree.
**Data model (extend):** `p5_tflearn_*` exist; add `pX_tflearn_modules` + `pX_tflearn_policies` (id, name, rule jsonb, created_at).
**Labs / lessons:** content follows the existing YAML/lesson format; new lessons are content work, not backend work. The next content gap is an L4 project that uses a registry module.
**Lite feasibility:** Yes for learning (validate/format/plan preview/state are local); container apply/destroy needs Docker and stays an honest 503 in lite.

## 5. Lite feasibility and scope

- **Scope: medium.** Module registry + policy checker + drift UI + multi-file editor on the existing learning engine.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `terraform-learning`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
