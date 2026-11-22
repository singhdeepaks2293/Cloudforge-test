# Batch — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `batch` |
| Name | Batch |
| Category | Compute |
| Registry route | `/services/batch` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | job-queue, job-definition, job |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Batch is the managed batch-computing service: submit jobs (containerised commands) into queues, and it provisions the right compute (EC2 or Fargate), runs the jobs in order of priority, retries failures, and scales to zero when the queue is empty. A learner meets it for embarrassingly parallel work — rendering, ETL, simulations — where Lambda's 15-minute limit is too short and a permanent cluster is wasteful.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Jobs, Job definitions, Job queues, Compute environments, Dashboard.
**Home — Dashboard:** queue depth, running jobs, failed jobs (24 h), compute environment utilisation.
**Jobs list:** columns Job name, Job ID, Status (Submitted / Pending / Runnable / Starting / Running / Succeeded / Failed), Job queue, Job definition, Started, Duration. Filters: status, queue. Click a row for detail.
**Job detail tabs:** Overview (definition, queue, attempts, timeout, environment variables, command), Attempts (per-attempt status, exit code, log stream link), Logs.
**Job definitions list:** columns Name, Revision, Status (Active / Inactive), Container image, vCPUs, Memory. Detail: JSON definition viewer.
**Job queues list:** columns Name, State (Enabled / Disabled), Priority, Compute environments (ordered), Status.
**Compute environments list:** columns Name, Type (Managed / Unmanaged), State, Compute resources (instance types, min/max/desired vCPUs), Status.
**Wizard — Create compute environment:** type (Managed EC2 / Fargate / unmanaged), name, service role, instance types, min/max/desired vCPUs, VPC/subnets/security groups, EC2 key pair, tags.
**Wizard — Create job queue:** name, priority, connected compute environments (ordered), tags.
**Wizard — Create job definition:** name, container image (ECR), command, vCPUs, memory, environment variables, job role, execution timeout, retry strategy (attempts, evaluate on exit), tags.
**Wizard — Submit job:** job name, job queue, job definition (revision), command override, environment overrides, depends-on (job dependencies), array size.
**Actions:** Submit job, Terminate job, Cancel job, Retry job, Enable/disable queue, Update compute environment, Deregister job definition revision.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Batch job queues, definitions and jobs._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** compute environments CRUD `POST/GET/DELETE /api/v1/aws/batch/compute-environments`, job queues CRUD `/api/v1/aws/batch/queues`, job definitions CRUD + revisions `/api/v1/aws/batch/job-definitions`, jobs `POST /api/v1/aws/batch/jobs` (submit), `GET /api/v1/aws/batch/jobs`, `GET /api/v1/aws/batch/jobs/{id}`, `POST /api/v1/aws/batch/jobs/{id}/terminate|cancel|retry`.
**Runtime reuse:** execute jobs with the existing ECS sandboxed-subprocess `TaskRunner` (backend/internal/phase4/phase4.go) — a job is a task run from the job definition's image record (image recorded, not pulled) with the definition's command, vCPUs/memory recorded as limits (prlimit memory cap), timeout from the definition, and retry attempts from the retry strategy. A tiny in-process scheduler (or a worker poller on the jobs table, same pattern as backend/internal/jobs/jobs.go) moves jobs Submitted → Runnable → Running → Succeeded/Failed, honouring queue priority and `dependsOn` (a job starts only when its dependencies succeeded). Job logs are real task output. Array jobs = N child jobs. Label the whole service SIMULATED (subprocess, not a container) with the same honesty as ECS.
**Data model:** Postgres `pX_batch_compute_envs` (id, name, type, state, instance_types text[], min_vcpus, max_vcpus, desired_vcpus, created_at) + `pX_batch_queues` (id, name, state, priority, compute_envs jsonb) + `pX_batch_job_definitions` (id, name, revision, status, image, command text[], vcpus, memory, env jsonb, timeout_seconds, retry_attempts, created_at) + `pX_batch_jobs` (id, name, queue_id, definition_id, revision, status, command_override text[], env_override jsonb, depends_on text[], array_index, attempt, exit_code, log_path, started_at, finished_at).
**Labs / lessons:** one lesson in the `containers` track: define a job that processes a file, submit 3 array jobs, watch priority/retries, terminate one. Reuse ECS/ECR lessons.
**Lite feasibility:** Yes — subprocess runtime, no Docker/KVM. Queue depth and compute-env vCPU caps are teaching values; enforce a small cap (e.g. max 2 concurrent jobs) in lite.

## 5. Lite feasibility and scope

- **Scope: medium.** Job queue/definitions/environments model + scheduler over the ECS subprocess runner.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `batch`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
