# CodeDeploy — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `codedeploy` |
| Name | CodeDeploy |
| Category | Developer Tools |
| Registry route | `/services/codedeploy` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/CodeDeployConsole.tsx, route `/services/codedeploy` |
| Resource types | deployment |
| Learning tracks linked | devops |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

CodeDeploy is the deployment service: take a revision (a bundle of code + an AppSpec file) and deploy it to a fleet — EC2 instances, Lambda functions, or ECS services — with deployment strategies (one-at-a-time, half-at-a-time, all-at-once, canary, linear), health checks, automatic rollback on failure, and a full deployment history. A learner meets it as the 'deploy safely, roll back fast' service, and as the deploy stage inside CodePipeline.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Applications, Deployments, Deployment configurations, On-premises instances.
**Home — Applications list:** columns Application name, Compute platform (EC2/ECS/Lambda/Server), Created. Click an application for its deployment groups.
**Deployment groups (per application):** columns Group name, Deployment configuration, Service role, Target (instances/service/function), Created.
**Deployments list (global):** columns Deployment ID, Application, Deployment group, Status (Created / InProgress / Succeeded / Failed / Stopped), Started, Duration.
**Deployment detail:** status header + lifecycle events timeline (ApplicationStop → DownloadBundle → BeforeInstall → Install → AfterInstall → ApplicationStart → ValidateService, each with status + duration), target instances table (instance ID, status, lifecycle events), revision details, rollback information.
**Wizard — Create application:** name, compute platform (EC2/ECS/Lambda), tags.
**Wizard — Create deployment group:** group name, service role, deployment configuration (OneAtATime / HalfAtATime / AllAtOnce / CodeDeployDefault.Canary10Percent5Minutes / Linear10PercentEvery1Minute), load balancer (for ECS/EC2 blue-green), alarms, auto-rollback triggers.
**Wizard — Create deployment:** application, deployment group, revision type (S3 / GitHub / CodeCommit), revision location, description, file exists behaviour.
**Actions:** Create deployment, Stop deployment, Rollback deployment (to previous revision), Create deployment group, Create application, Retry deployment.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Deployments with real status and rollback to the previous revision._
- **What the current console shows (registry mirrors note):** Applications; Deployment groups; Deployments with status and rollback action
- **How it differs / what is missing (registry differs note):** Deployments apply the revision for real and wait for the target to reach it (ECS desired count RUNNING on the new revision; Lambda code verified). Rollback deploys the previous revision through the same path. No EC2/on-premises instances, no blue/green traffic shifting.
- **Console today:** Yes — frontend/src/pages/CodeDeployConsole.tsx, route `/services/codedeploy`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add blue/green and Lambda traffic shifting, do not rebuild the core.** Current: real deployments + rollback for ECS (rolling update, waits for desired count RUNNING on the new revision) and Lambda (code update, version+1, stored code verified); only a SUCCEEDED deployment can be rolled back (backend/internal/httpapi/phase4.go `p4_codedeploy_*`). Console: CodeDeployConsole.tsx. To build later, in order:
1. **Lambda traffic shifting:** canary/linear deployments for Lambda — shift a percentage of invocations to the new version over time (the Lambda invoker needs a weighted alias: alias points at two versions with weights; the invoke path already exists, so this is an alias-weight table + a worker tick that steps the weights). Endpoints on the Lambda alias resource; deployment record shows the current traffic split.
2. **ECS blue/green:** deploy the new task set alongside the old, shift ELB traffic (the ELB round-robin router needs a weight per target group), then terminate the old set; on failure, shift back. This is the largest item — it needs target-group weights in the ELB router first.
3. **EC2/on-premises targets:** out of scope until there is a real host agent fleet with the CodeDeploy agent; the console already labels EC2 compute platform as unavailable with the reason. Do not build a fake agent.
4. **Deployment configuration presets:** the full preset list (time-based canary/linear for Lambda, one/half/all for ECS) as selectable configs instead of the current subset.
5. **AppSpec lifecycle hooks:** run BeforeInstall/AfterInstall/ValidateService hooks as sandboxed subprocess commands (same runner as Lambda/ECS) and record their output in the lifecycle timeline; a failing hook fails the deployment and triggers rollback.
**Data model (exists, extend):** `p4_codedeploy_*`; add `pX_codedeploy_traffic` (id, deployment_id, new_version_percent, updated_at) for Lambda shifting.
**Labs / lessons:** add one lesson to the `devops` track: canary-deploy a Lambda function 10% at a time, watch the traffic split, roll back. Reuse the existing CodeDeploy lesson.
**Lite feasibility:** Yes — ECS/Lambda targets already work in lite; blue/green needs the ELB weight feature, which is itself lite-feasible (in-process router).

## 5. Lite feasibility and scope

- **Scope: medium.** Lambda traffic shifting + ECS blue/green (needs ELB target-group weights) + AppSpec hooks.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `codedeploy`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
