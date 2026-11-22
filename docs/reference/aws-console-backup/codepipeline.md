# CodePipeline — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `codepipeline` |
| Name | CodePipeline |
| Category | Developer Tools |
| Registry route | `/services/codepipeline` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/CodePipelineConsole.tsx, route `/services/codepipeline` |
| Resource types | pipeline |
| Learning tracks linked | devops |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

CodePipeline is the continuous delivery service: model a release as a pipeline of stages (Source → Build → Test → Deploy → Approval), and every change to the source automatically flows through the stages, stopping on failure and recording each execution. A learner meets it as the 'automate my release' service — the orchestration layer over CodeBuild and CodeDeploy — and as the place where manual approval gates appear.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Pipelines, Settings.
**Home — Pipelines list:** columns Pipeline name, Status (last execution), Source (provider + repository), Last execution (time + duration). Filters: search, status.
**Pipeline detail:** visual stage diagram (boxes per stage with actions inside, connected by arrows; each action shows status colour + duration; click an action for its execution detail). Below: Execution history table (Execution ID, Status, Trigger, Started, Duration). Tabs: Visual (the diagram), History, Settings.
**Execution detail:** per-stage, per-action status, input/output artifact names, logs link (for build/deploy actions), "Stop execution" / "Retry stage" buttons.
**Wizard — Create pipeline:** Step 1 Pipeline settings (pipeline name, service role, artifact store S3 bucket, advanced settings). Step 2 Add source stage (source provider CodeCommit/GitHub/S3/CodeStar, repository, branch, change detection, output artifact name). Step 3 Add build stage (build provider CodeBuild/Jenkins, project name, output artifact). Step 4 Add deploy stage (deploy provider CodeDeploy/ECS/S3/CloudFormation/Lambda, configuration). Step 5 Review.
**Actions:** Create pipeline, Start execution (Release change), Stop execution, Retry failed stage, Edit pipeline (add stage/action, add manual approval action), Delete pipeline, View execution history.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _CI/CD pipelines — source → build → deploy, stages run for real._
- **What the current console shows (registry mirrors note):** Pipelines; Pipeline detail with stage visualisation; Executions with per-stage status
- **How it differs / what is missing (registry differs note):** Stages execute in order; a failed stage stops the execution. Deploy targets are the ECS service update and the Lambda code update that exist; Terraform deploy honestly fails without Docker.
- **Console today:** Yes — frontend/src/pages/CodePipelineConsole.tsx, route `/services/codepipeline`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add approvals and more deploy targets, do not rebuild the engine.** Current: stages execute in order (source → build → deploy), a failed stage stops the execution, deploy targets are ECS service update and Lambda code update, Terraform deploy honestly fails without Docker (backend/internal/httpapi/phase4.go `p4_codepipeline_*`). Console: CodePipelineConsole.tsx. To build later, in order:
1. **Manual approval actions:** an Approval action type that pauses the execution until an instructor/admin approves or rejects it (with an optional comment); execution shows `Waiting for approval`. Endpoints `POST /api/v1/aws/codepipeline/pipelines/{id}/executions/{execId}/approve|reject`. This is the single most-taught pipeline feature and it is currently missing.
2. **More deploy targets:** S3 website deploy (sync artifacts to a MinIO bucket — real PutObject), CloudFormation deploy (create/update a stack from the artifact template — reuse the CloudFormation engine), CodeDeploy is already there. Each target needs its stage config schema + validation at pipeline creation.
3. **Source polling / webhooks:** source stages currently run on manual release; add a "poll local source" trigger (hash the local git HEAD; start an execution when it changes) — no GitHub webhooks locally.
4. **Execution visualisation data:** the frontend already renders stages; add per-action input/output artifact names and durations to the execution record so the diagram can show them.
5. **Pipeline variables and triggers:** pipeline-level variables referenced as `#{Variable}` in stage config; scheduled triggers are out of scope (no scheduler beyond the worker poller).
**Data model (exists, extend):** `p4_codepipeline_*`; add `pX_codepipeline_approvals` (id, execution_id, stage, action, status, decided_by, comment, created_at, decided_at).
**Labs / lessons:** add one lesson to the `devops` track: build a pipeline with a manual approval gate between staging and production deploys; approve it, watch the deploy. Reuse the existing pipeline lesson.
**Lite feasibility:** Yes — all current targets work in lite; S3 deploy needs MinIO (full mode) and is disabled in lite with the reason.

## 5. Lite feasibility and scope

- **Scope: small.** Manual approval actions + S3/CloudFormation deploy targets on the existing engine.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `codepipeline`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
