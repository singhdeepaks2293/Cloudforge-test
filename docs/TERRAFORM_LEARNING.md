# Terraform Learning (CloudForge v2, Phase 5 — master prompt §9)

A first-class main-navigation section at `/terraform-learning` (and
`/services/terraform-learning`), not a documentation page. It mirrors
the real Terraform/AWS console workflows where applicable (projects
list, editor, plan diff view, state inspector) in CloudForge branding
only — no AWS logos/trademarks, no claim it is AWS.

## What it is

| Piece | Where it lives | What is real |
|---|---|---|
| Dashboard | `GET /api/v1/terraform-learning/dashboard` | Progress, current course, pending exercises, projects, successful plans/applies, recent executions, skill level, achievements — all computed from the progress store, projects, and execution history. No canned numbers. |
| Courses | `backend/internal/tflearn/lessons.go` | Level 1 Fundamentals (15 lessons), Level 2 Intermediate (20), Level 3 Advanced (20), Level 4 AWS-style Infrastructure Projects (7). Every lesson: explanation, real-world use case, code example, guided exercise, expected output, common mistakes, knowledge check. Completion requires a correct knowledge-check answer; progress is per user in `p5_tflearn_progress`. |
| Curriculum reuse | `curriculum/terraform/terraform.yaml` | The existing Terraform track (7 lessons) already teaches the Level 1 core. Each Level 1 lesson names its matching track lesson in `curriculum_lesson` — the content is NOT duplicated into a second copy. Levels 2–4 have no counterpart in the existing curriculum and are new content that lives only in `lessons.go`. |
| Editor | `frontend/src/pages/TerraformLearning.tsx` + `POST .../projects/{id}/files` | Multi-file projects (`main.tf`, `variables.tf`, `outputs.tf`, `providers.tf`, `versions.tf`, `terraform.tfvars`, `modules/`), syntax highlighting, save with **project-wide** HCL validation (cross-file references are legal), formatting, copy/download via the file API. Editor state persists per user/project in `p5_tflearn_files`. No autocomplete against a language server (not built); documentation links go to the provider endpoint. |
| Local checks | `backend/internal/tflearn/hcl.go` | `ValidateHCL` (delimiter balance, block structure, undeclared variables/resources, forbidden hosts, embedded secrets), `FormatHCL` (indentation + spacing, heredocs untouched), `ComputePlan` (static diff). All labelled **"local"** in every response — they do not run the Terraform binary and do not need Docker. A green local check does not guarantee the container validate will pass. |
| Plan preview | `POST .../projects/{id}/plan-preview` | A LOCAL static diff of declared configuration vs recorded state, `source: "cloudforge-local-diff"`, with create/update/delete/no-op counts. Approving a preview (`POST .../plans/{planId}/approve`, 1 h TTL) records that the learner reviewed it — it is a learning record, **not** a container apply gate. |
| Container execution | `POST .../projects/{id}/executions` → M7 | Delegates to the existing Milestone 7 manager (`enqueueTerraformRun`, `terraform_runs`, the worker) through the lab a project is linked to. **No second Terraform runner was built.** Multi-file projects are concatenated into one document for the container run (the M7 worker writes the payload as the lab's `main.tf`); local validation/format keep working per file. Without Docker the availability check answers **503 `terraform_unavailable`** and a FAILED run is recorded — never a faked success. **Network isolation (Phase 7, R11):** `CLOUDFORGE_TF_NETWORK_MODE=isolated` runs the container with `--network none` (no egress at all — the emulator is unreachable, plans needing it fail honestly); the default `bridge` mode keeps the `host.docker.internal` emulator route and only approximates isolation. |
| Apply / destroy gating | same endpoint | Apply: `confirm=true` **and** an approved, unexpired M7 plan (`plan_id`); otherwise 409 (`confirm_required` / `plan_not_approved` / `plan_expired`). Destroy: `confirm=true`; otherwise 409. Nothing auto-applies. |
| Execution history | `GET .../projects/{id}/executions`, `GET .../executions/{id}` | Every attempt is recorded (including honest failures). Container runs refresh their status/log from the linked M7 run, since the worker updates `terraform_runs`, not `tflearn_runs`. |
| State | `GET .../projects/{id}/state` (+ `/history`, `/lock`, `/unlock`, `/backup`, `/restore`, `/drift`) | Append-only versioned snapshots in `p5_tflearn_states`, refreshed from the linked lab's latest succeeded container plan. **State is never edited by string replacement.** Restore creates a NEW version copied from the restored one; history is never rewritten. Locks are 10-minute, holder-or-admin force-unlock; saves/executions/restores by another user are refused while locked (409 `state_locked`). Drift compares declared configuration vs recorded state vs live lab resources (when a lab is linked). |
| Provider | `GET .../provider`, `GET .../provider/resources` | 9 `cloudforge_*` resource types, each with arguments, attributes, the real CloudForge API it maps to, fidelity, and an example. Only resources with an actual provider + API mapping are listed. Installation/versioning docs are served by the endpoint: the provider is a learning surface tracking the platform version; no registry download; AWS-provider learning mode works only with a local emulator — real AWS credentials are refused (`ValidateProviderConfig`). |

## What is NOT built (honest limits)

- No Terraform Cloud / HCP Terraform remote backend or workspaces; no
  module registry (modules are local paths under `modules/`).
- No real AWS provider execution: configuration referencing
  `amazonaws.com` or embedding AWS keys is rejected (400).
- Container execution needs Docker and a linked lab. In this sandbox
  (no Docker) and in lite mode (no Docker by design), execution is
  honestly unavailable — the local layer (validate/format/plan-preview/
  state/drift) is the part that works here.
- State refreshes from the linked lab's latest succeeded **plan**; after
  an apply, re-plan to refresh recorded state.
- The editor has syntax highlighting but no language-server
  autocomplete; the local formatter is indentation + spacing, not
  `terraform fmt`.

## Sandbox (no Docker)

The sandbox cannot run the container path. What was verified live
here (see `MILESTONE_LOG.md`, Phase 5 entry): dashboard, lesson +
knowledge check, project creation from a template, file save
(valid + invalid HCL → 400 with the real issues), validate, format,
plan preview + approve, apply/destroy without confirmation → 409,
execution without a lab → 409, execution with a lab but no Docker →
503 `terraform_unavailable` (honest, with a FAILED run recorded).
The container success path is implemented (M7, tested with the
stub runner in the M7 suite) but unverified in this sandbox.

## Files

- Backend: `backend/internal/tflearn/{doc,lessons,hcl,provider}.go`,
  `backend/internal/tflearn/tflearn_test.go`,
  `backend/internal/httpapi/tflearn.go`,
  `backend/internal/httpapi/tflearn_test.go`,
  `backend/migrations/011_tflearn.sql`
- Frontend: `frontend/src/pages/TerraformLearning.tsx`,
  `frontend/src/pages/terraform-learning.test.tsx`,
  routes in `frontend/src/App.tsx`
- Docs: this file, `docs/IMPLEMENTATION_STATUS.md` §14,
  `docs/SERVICE_CATALOG.md` (`terraform-learning` row),
  `docs/SERVICE_FIDELITY.md` (Terraform Learning row)
