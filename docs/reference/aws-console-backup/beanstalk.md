# Elastic Beanstalk — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `beanstalk` |
| Name | Elastic Beanstalk |
| Category | Compute |
| Registry route | `/services/beanstalk` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | application, environment |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

Elastic Beanstalk is the platform-as-a-service for web applications: upload application code, pick a platform (Python, Node.js, Java, .NET, Go, PHP, Ruby, Docker), and it provisions the load balancer, instances, database, monitoring and deployments for you. A learner meets it as the 'deploy my app without touching infrastructure' service, and as the contrast to building the same stack by hand with EC2/ELB/RDS.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Applications, Environments, (per application) Environments list, Application versions, Saved configurations.
**Home — Applications list:** columns Application name, Environments (count), Created. **Environments list (per application):** columns Environment name, Health (OK / Warning / Degraded / Severe, colour-coded), URL (clickable), Platform, Running version, Created.
**Environment detail:** header (name, health, URL, platform, running version, configuration). Tabs/sections: **Overview** (health dashboard: environment health, instance health, request count, latency, CPU), **Events** (timestamped event stream), **Configuration** (cards: Capacity, Load balancer, Instances, Software, Security, Monitoring, Managed updates, Notifications, Network, Database — each with Edit), **Logs** (request last 100 lines / full logs / enable log rotation), **Alarms** (CloudWatch alarms), **Health** (enhanced health: instance-level health, causes).
**Wizard — Create application:** application name, description, tags.
**Wizard — Create environment:** environment tier (Web server environment / Worker environment), platform (managed platform + version), application code (sample application / upload version / existing version), environment name, domain, presets (Single instance / High availability), service role, EC2 key pair, VPC, instance profile.
**Actions:** Create environment, Deploy (upload & deploy a new version), Clone environment, Rebuild environment, Terminate environment, Swap environment URLs, Restart app server(s), Save configuration.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Application environments._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** applications CRUD `/api/v1/aws/beanstalk/applications`, environments CRUD + lifecycle `/api/v1/aws/beanstalk/environments` (create/deploy/clone/rebuild/terminate/swap-urls), application versions `/api/v1/aws/beanstalk/applications/{id}/versions`, events `/api/v1/aws/beanstalk/environments/{id}/events`, configuration `/api/v1/aws/beanstalk/environments/{id}/configuration`.
**Runtime reuse (composition, not new infrastructure):** an environment is a composed deployment of things CloudForge already has — an ELB load balancer (in-process round-robin), 1–N lab instances (EC2 console) running the uploaded code via the CodeBuild runner, and optionally an RDS instance. Deploy = upload a zip (store in the workdir, extract, run the platform start command via the sandboxed subprocess runner), then register the instances as ELB targets. Health = the ELB probe + instance state. This is a large composition job, but every primitive already exists; the new work is the orchestration, the platform start-command table (python `requirements.txt` + `python app.py`, node `npm install && npm start`, etc.), and the event log. Label environments SIMULATED (subprocess, not real instances behind a real load balancer).
**Data model:** Postgres `pX_beanstalk_applications` (id, name, description, created_at) + `pX_beanstalk_versions` (id, application_id, version_label, source_location, created_at) + `pX_beanstalk_environments` (id, application_id, name, tier, platform, version_label, health, url, status, config jsonb, elb_id, instance_ids text[], created_at) + `pX_beanstalk_events` (id, environment_id, message, severity, created_at).
**Labs / lessons:** one lesson in the `containers`/`serverless` track: deploy a sample app with Beanstalk, then deploy a new version and watch the rolling update; compare with the hand-built three-tier lab. One break-and-fix: a bad version that fails health checks and rolls back.
**Lite feasibility:** Partial — single-instance environments fit lite (1 lab cap is the binding constraint: a Beanstalk environment counts as one lab's worth of resources). High-availability (multi-instance) environments need full mode; in lite, creating a multi-instance environment is refused with a clear 429-style message, mirroring the existing lite lab caps.

## 5. Lite feasibility and scope

- **Scope: large.** Environment orchestration composing ELB + instances + RDS + CodeBuild; platform start-command table.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `beanstalk`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
