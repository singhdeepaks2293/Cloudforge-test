# App Runner — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `apprunner` |
| Name | App Runner |
| Category | Containers |
| Registry route | `/services/apprunner` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | service |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

App Runner is the fastest path from source code or a container image to a running, publicly reachable web application: it builds the code (or pulls the image), runs it behind a managed load balancer with TLS, and scales it to zero and back up, with no VPC, cluster or load-balancer configuration. A learner meets it as the 'just deploy my web app' service, between Lambda (functions) and ECS (full container control).

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Services, Connected accounts, Auto scaling configurations.
**Home / list page:** Services table — columns Service name, Status (Creating / Running / Paused / Deleted / Operation in progress), Default domain (clickable `*.awsapprunner.com` URL), Source (ECR image or source repository + branch), Created. Filter text box scopes the list.
**Service dashboard:** header shows service name, status, health, ARN; Service overview section (default domain, source, image ID, CPU/memory, port). Tabs below the overview: **Activity** (deployment history), **Logs** (application + deployment logs, search box, link out to CloudWatch), **Configuration** (sections: Source and deployment, Configure build, Configure service — each with Edit), **Metrics** (request count, latency, CPU/memory graphs), **Custom domains** (domain + certificate status + DNS records).
**Wizard — Create an App Runner service:** Step 1 Source & deployment — source type (source code repository via a connected GitHub/Bitbucket account, or container registry ECR), deployment settings (automatic on push / manual, ECR access role). Step 2 Configure service — Service settings (service name, virtual CPU & memory, environment variables, port), Auto scaling (default or custom configuration: min/max instances, concurrency), Health check (protocol, path, interval, timeout, healthy/unhealthy thresholds), Security (instance IAM role), Tags. Step 3 Review & create, then Create & deploy.
**Actions:** Create service, Pause, Resume, Deploy (start a new deployment), Delete, Associate custom domain, Create auto scaling configuration.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Containerised web services._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** `POST /api/v1/aws/apprunner/services`, `GET /api/v1/aws/apprunner/services`, `GET /api/v1/aws/apprunner/services/{id}`, `POST /api/v1/aws/apprunner/services/{id}/pause|resume|deploy`, `DELETE /api/v1/aws/apprunner/services/{id}`, `GET /api/v1/aws/apprunner/services/{id}/logs`, auto-scaling configs CRUD under `/api/v1/aws/apprunner/autoscaling-configurations`, connected accounts are out of scope (no GitHub OAuth locally — source is a local git path or an ECR repository, same constraint as CodeBuild).
**Runtime reuse:** build on the existing ECS sandboxed-subprocess `TaskRunner` (backend/internal/phase4/phase4.go) — an App Runner service is a long-running task from an ECR image record or a local source checkout built by the CodeBuild runner, fronted by the ELB in-process round-robin router with a local teaching DNS name. Health checks reuse the ELB probe. Pause stops the task; resume restarts it; deploy restarts with the new revision. No Docker container is started — label SIMULATED, same honesty as ECS/Fargate.
**Data model:** Postgres `pX_apprunner_services` (id, name, source_type, source_repo/image, branch, cpu, memory, port, env jsonb, autoscaling_config_id, health_check jsonb, status, default_domain, task_id, created_by, created_at) + `pX_apprunner_deployments` (id, service_id, revision, status, started_at, finished_at) + `pX_apprunner_autoscaling` (id, name, min_size, max_size, max_concurrency).
**Labs / lessons:** one lesson in the `containers` track: deploy a Flask/Express app from a local repo, open its URL, pause/resume, watch auto-scaling config. Reuse the CodeBuild local-source constraint (local paths only).
**Lite feasibility:** Yes — subprocess runtime + in-process routing need no Docker/KVM. Custom domains stay teaching-only (no real DNS/TLS); the default domain is a local teaching name.

## 5. Lite feasibility and scope

- **Scope: medium.** App Runner service/deployment model on the ECS subprocess runtime + ELB routing.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `apprunner`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
