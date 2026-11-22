# CloudForge v2 — Implementation Status (Phase 0 Audit)

> **Audit date:** 2026-11-22 IST (2026-11-21 UTC) · **Auditor:** Phase 0 (read-only)
> **Scope:** all 248 source files in `~/workspace/cloudforge/` — read, not edited.
> No source code was changed by this audit. The only writes are this file,
> `docs/SERVICE_CATALOG.md`, `docs/SERVICE_FIDELITY.md`, and one appended
> entry in `MILESTONE_LOG.md`.
> **Method:** every claim below is grounded in a file read during the audit
> (paths cited). Where docs and code disagree, code wins and the disagreement
> is flagged. Where the sandbox cannot run something, it is marked
> **unverified here**, never claimed as verified.

## 0. Headline verdict

CloudForge v2 as audited is a **real, working learning platform for a narrow
slice of AWS, not a full AWS duplicate yet**. What works end-to-end (backend +
frontend + tests, verified live in this sandbox against real Postgres 16.15 /
Redis 7): auth, labs (launch / destroy / bulk destroy), lab resources and
topology, validation, terminal, curriculum + assessments + achievements,
Terraform plan/approve/apply as containerised jobs, S3 buckets via local
MinIO, IAM policy evaluation (subset), audit, quotas, lite mode. What is
**registry-only** (an entry in `backend/internal/providers/providers.go`
with no HTTP route and, for most, no frontend page): DynamoDB, SQS, SNS,
EventBridge, STS, KMS, Lambda, EC2-as-an-API. What does not exist at all:
RDS, EBS, ELB/ALB, Route 53, CloudWatch/CloudTrail consoles, ECS/ECR,
CloudFormation, API Gateway, Step Functions, and almost everything in
§6 categories I–N. The frontend today is a **generic console shell with
17 catalog entries that mostly redirect to `/labs`, `/learn` or `/console`**
(`frontend/src/services/catalog.ts`) — it is not yet a per-service AWS
duplicate. That gap is exactly what Phases 1–6 close (see §7 and the
AWS-duplicate UI rule in §8).

## 1. Port bindings — discrepancy RESOLVED by user decision

The master prompt §3 and the repo disagreed. The user has decided in favour
of the master prompt. **This audit records the decision; a separate agent is
making the code change in parallel. This audit did not edit compose, config,
or the Makefile, and the "after" column below is the decided target, not a
verified state of the repo at audit time.**

| Service | Before (repo as audited, verified working on the user's Fedora host) | After (decided target, master prompt §3) |
|---|---|---|
| Frontend (nginx) | Host `:80` → nginx `:8080` inside the container (`deployments/compose/docker-compose.yml:126`, `frontend/nginx.conf` `listen 8080`) | Host **`:8080`** |
| API (Go) | Host `:8080` (`API_PORT=8080`, `backend/internal/config/config.go:169,210`) | Host **`:8081`** |
| Worker | Internal `:8081` (`config.go:20,210` `WORKER_ADDR` default `:8081`; `backend/cmd/worker/main.go:118`) — collided with the API's new port, so it must move | **Internal `:8083`** (verified live in the port-change work: `WORKER_ADDR` default `:8083`, `backend/cmd/worker` listens on `:8083`; both compose files pin `WORKER_ADDR: ":8083"`). Host agent stays `:8082`. |
| Host agent | `:8082` (`config.go:31`, `backend/cmd/host-agent/main.go:3`) | **Unchanged: `:8082`** |
| Network engine (gRPC) | `:50051` (unauthenticated, see §5 R1) | Unchanged: `:50051` (auth/TLS is a Phase 7 hardening item, §5) |
| Postgres / Redis | `:5432` / `:6379`, dev profile only (`docker-compose.dev.yml`) | Unchanged |
| Prometheus / Grafana / MinIO console | `:9090` / `:3000` / `:9001` (optional/monitoring profiles) | Unchanged |

**Operator action (user's Fedora host):** after the new zip lands,
`make down-lite` / `make up-lite` again (or `make down` / `make up` for full
mode) from the new checkout, and update bookmarks, firewall rules, and any
hard-coded URLs from `:80`/`:8080` to **`:8080` (frontend) / `:8081` (API)**.
The Vite dev server (`:5173`, `frontend/vite.config.ts`) proxies `/api` to
`localhost:8081` following the port change (verified in the port-change
work). Worker port is `:8083` (see the table above).

## 2. Existing-feature inventory (a) — what actually exists in code

Status key: **Implemented** = backend + frontend + tests exist and the path
was verified in this sandbox or on the user's host as noted. **Partial** =
backend exists but frontend or live verification is missing. **Registry-only**
= capability-registry entry only, no HTTP route. **Unverified here** = built
for a real host, cannot run in this sandbox (no Docker/KVM/`/dev/kvm`/nginx).

### 2.1 Auth, users, roles

| Feature | Status | Evidence |
|---|---|---|
| Register / login, Argon2id + HS256 JWT (12 h TTL), dual-verify legacy bcrypt → Argon2id on login | Implemented, verified live | `backend/internal/auth/auth.go`, `password.go`; `backend/cmd/api/main.go:64`; routes `POST /api/v1/auth/register|login`, `GET /api/v1/auth/me` in `backend/internal/httpapi/httpapi.go:1227-1230` |
| Roles learner/instructor/admin, admin bootstrap via `CLOUDFORGE_ADMIN_EMAIL`, admin-only user list + course CRUD | Implemented | `auth.go:46`, `httpapi.go:1232-1237`; `docs/security/model.md` |
| JWT revocation / logout | **Not implemented** — no server-side session store, no revocation list, no `jti`; rotating the secret invalidates *all* sessions at once | `docs/security/model.md` ("no revocation list"); grep for `revok|blacklist|jti|logout` in `backend/` returns no session-revocation code |
| Password policy | Partial — ≥8 chars only; no strength meter, no reset flow | `auth.go`, `docs/security/model.md` |
| Frontend Auth pages | Implemented | `frontend/src/pages/Auth.tsx`, token in `localStorage` (`frontend/src/services/api.ts:66`) — see §5 R6 |

### 2.2 Labs (the core that works)

| Feature | Status | Evidence |
|---|---|---|
| Lab definitions (4): `vpc-fundamentals` (60 min), `iam-security` (30), `ec2-basics` (30), `three-tier-app` (120) | Implemented | `backend/internal/labs/labs.go:90-95` |
| Lifecycle `CREATED → PROVISIONING → READY → … → DESTROYING → DESTROYED`, `FAILED` on engine-down, idempotency keys, expiry sweep | Implemented, verified live | `labs.go:24-58,187-279`; `backend/internal/jobs/jobs.go`; E2E `tests/e2e/e2e_test.go` |
| Launch Lab UI, Destroy UI (per-row + bulk selected + destroy-all) | Implemented, verified live on Fedora + vitest | `frontend/src/pages/Labs.tsx`, `hooks/useBulkDestroy.ts`, `hooks/useLaunchLab.ts`, `components/LabLauncher.tsx`; MILESTONE_LOG tail (72/72 frontend tests) |
| Lab resources CRUD (kinds `vpc|subnet|instance|gateway|loadbalancer|route-table|security-group`), topology, validation rules | Implemented, verified live | `backend/internal/httpapi/resources.go`, `topology.go`; `POST /labs/{id}/validate` (rules incl. `route_to_igw`, `security_group_allows_ssh`; `dns_resolves` always `unknown` — no in-VPC DNS resolver) |
| Terminal (WS, 5-min single-use token, SHA-256 hash persisted, allowlisted commands only, simulated shell labelled as simulated) | Implemented | `backend/internal/httpapi/terminal.go`, `backend/internal/terminal/terminal.go:211` allowlist, `backend/internal/hostagent/ops.go:237` |
| Quotas per role + admin `GET/PUT /api/v1/admin/quotas`; lite caps (1 lab / Micro / 30 min, 429 on breach) | Implemented, verified live | `backend/internal/quota/quota.go:58-83`, `backend/internal/httpapi/quotas.go`; migration `006_quotas.sql`, `007_lite_lab_minutes.sql` |
| Break-and-fix scenarios (6) with XP | Implemented | `curriculum/break-and-fix/*.yaml` (6 files), `backend/internal/httpapi/breakfix.go` |
| **Worker in full compose** | **FIXED in Phase 1 (2026-11-22).** `deployments/compose/docker-compose.yml` now runs a `worker` service mirroring lite (`WORKER_ADDR: ":8083"`, same env/mounts/networks, `depends_on` postgres+redis healthy). Full-mode labs no longer sit in `CREATED`. Verified by reading the compose file (YAML parses; `docker compose config` still unverified here — no Docker) plus a live lite lab provisioned to `READY` by the worker. Host agent is still not deployed by either compose file (simulated mode by default). | `deployments/compose/docker-compose.yml:87` (worker), `docker-compose.light.yml:121`, `backend/internal/jobs/jobs.go` |
| Host agent as a deployed service | **Not deployed by either compose file.** `HOST_AGENT_ADDR` is honoured only if set in env (`cmd/api/main.go`); default mode is `simulated`. The host-agent binary exists (`backend/cmd/host-agent/main.go`, `:8082`) but nothing in compose starts it. | compose files; `backend/internal/hostagent/` (simulated / Docker / libvirt backends) |

### 2.3 Courses / curriculum / Learning Center (as it exists today)

| Feature | Status | Evidence |
|---|---|---|
| Curriculum: 14 tracks, 70 lessons, 84 questions, 6 break-and-fix, 15 achievements; YAML in `curriculum/` (14 track files), seeded at startup from a read-only Docker mount (`../../curriculum:/curriculum:ro`, `CURRICULUM_DIR=/curriculum`) | Implemented, verified live (seed log: tracks 14, lessons 70, questions 84, scenarios 6, achievements 15) | `curriculum/*/ *.yaml`, `backend/internal/curriculum/seed.go`, `backend/internal/curriculum/curriculum.go`, compose files |
| Tracks / lessons / enrolment / progress / assessments / achievements / XP APIs + frontend Learn/Courses pages | Implemented | `httpapi.go:1264-1271`, `curriculum.go`; `frontend/src/pages/Learn.tsx`, `Courses.tsx` |
| **Count discrepancy — FIXED in Phase 1 (2026-11-22).** Cause: `ListTracks` listed *every* course row (3 legacy M1 courses + 14 curriculum tracks = 17) while the seed report counts YAML tracks (14). The memory store now records which course ids were seeded as tracks (`tracks` set in `MemoryCurriculumStore.UpsertTrack`); Postgres `ListTracks` requires BOTH a guided and a challenge lab definition (every track upserts both; the legacy courses only ever gained guided definitions). Live-verified: `GET /api/v1/curriculum/tracks` = 14, `GET /api/v1/courses` = 17 (3 legacy + 14 — that count is correct). Test: `TestListTracksExcludesLegacyCourses` (store). | Fixed | `backend/internal/store/memory_curriculum.go`, `postgres_curriculum.go`, `curriculum_test.go` |
| Guided labs / challenges per track | Partial — YAML `guided_lab`/`challenge` blocks exist; only 4 hardcoded lab definitions are launchable (`labs.go:90`) and most tracks have no matching launchable lab | `curriculum/curriculum.go:286-290`, `labs.go:90-95` |

### 2.4 Network engine (Rust)

| Feature | Status | Evidence |
|---|---|---|
| gRPC service, 23 RPCs: Health, Create/DeleteNetwork, namespaces, bridges, IPAM, InspectTopology, ValidateNetwork, CleanupLab + M4: subnets, veth pairs, routes, firewall policies, NAT, fault injection, telemetry | Implemented; `cargo test --workspace` 74 passed | `proto/cloudforge/v1/network.proto`, `rust-network-engine/crates/grpc-server/src/service.rs`, `crates/engine-core/src/` |
| Backends: SIMULATED (default, `NETWORK_ENGINE_BACKEND=simulated`) vs real rtnetlink/nftables (`netlink`, needs `CAP_NET_ADMIN` + `nft` binary) | Implemented, **real backend unverified here** (compiles; no netlink in sandbox) | `crates/engine-core/src/backend.rs:27-51`, `crates/grpc-server/src/lib.rs:56-68`; full compose grants `NET_ADMIN` only to `network-engine` |
| Fault injection (latency/loss/partition/DNS/blackhole, 1–3600 s), telemetry (real in-memory counters, "no traffic is fabricated") | Implemented (simulated) | `network.proto` FaultSpec/TelemetryPoint; `crates/fault-injection/`, `crates/telemetry/` |
| **gRPC auth/TLS** | **None.** Plaintext `insecure.NewCredentials()` (`backend/internal/networkclient/client.go:117`), server listens `0.0.0.0:50051` with no auth metadata, port published on the host in both compose files. Anyone who can reach `:50051` can create/delete networks. | See §5 R1 |

### 2.5 Providers (AWS simulators / emulators)

| Feature | Status | Evidence |
|---|---|---|
| Capability registry, 27 seeded capabilities, health probes (`unknown` until probed, never assumed ok) | Implemented | `backend/internal/providers/providers.go` (`seeded`, `RefreshHealth`, 2 s probe timeout) |
| S3 via local MinIO (CreateBucket/ListBuckets/PutObject/GetObject) — **HTTP-exposed**: `POST/GET /api/v1/aws/s3/buckets` only; Put/GetObject exist in the adapter but have no HTTP route | Partial (adapter full, API surface 2 of 4 ops) | `providers/s3.go`, `httpapi/aws.go:78-106`; MinIO optional profile, `minio/minio:RELEASE.2024-08-17T01-12-02Z` |
| DynamoDB via DynamoDB Local (CreateTable/PutItem/GetItem/ListTables) | **Registry-only** — adapter exists (`providers/dynamodb.go`), no HTTP route, no frontend page; default endpoint `http://localhost:8000`, no such service in compose | `providers/dynamodb.go`, route table `httpapi.go:1213-1282` |
| IAM policy evaluation (subset: Allow/Deny, wildcards, explicit-deny-wins, `StringEquals` on `aws:username` only; no ARNs/principals/variables/other conditions) — **HTTP-exposed**: `POST /api/v1/aws/iam/evaluate` | Partial, honestly labelled | `providers/iam.go`, `httpapi/aws.go:132` |
| STS AssumeRole/GetCallerIdentity, KMS CreateKey/Encrypt/Decrypt, SQS, SNS, EventBridge | **Registry-only + in-memory simulators.** No HTTP routes. KMS keys are AES-256-GCM keys held in process memory, **lost on restart**; STS credentials are locally generated and grant no AWS access; SQS ReceiveMessage **removes** messages (no visibility timeout, no DLQ, no FIFO) | `providers/simulators.go`, `providers.go:336-481` |
| EC2 RunInstances/DescribeInstances | **Registry-only, and the registry claim is misleading.** Registered as `API_EMULATED` via `aws-emulator` requiring `AWS_EMULATOR_ENDPOINT` (default `http://localhost:4566`); no emulator service exists in compose, no EC2 HTTP handler, no code calls the emulator for instances. Real instance provisioning goes through labs/host-agent, not this registry entry. | `providers.go` (ec2 entries), `providers/emulator.go`, route table |
| Lambda InvokeFunction | **Registry-only, claim unverified.** Registered as `SIMULATED`/`docker` ("functions run in isolated Docker containers") — no Lambda invoker, runtime, or HTTP handler was found in code. Treat as **not started** in practice. | `providers.go` (lambda entry); grep for lambda invoker code: none |
| EKS CreateCluster | Explicitly **UNSUPPORTED** in the registry | `providers.go` (eks entry) |
| AWS emulator (LocalStack) / Floci | **Not wired.** `AWS_EMULATOR_ENDPOINT` config exists; no emulator service in compose, README says "not wired in M1" (`.env.example`) | `config.go:227`, `.env.example` |

### 2.6 Terraform

| Feature | Status | Evidence |
|---|---|---|
| Container-only runner, pinned `hashicorp/terraform:1.9.8` (alt `opentofu/opentofu:1.9.2` via `TERRAFORM_IMAGE`), Docker Engine API over Unix socket, 1 vCPU/512 MiB, per-lab workdir, generated `provider.tf` pointing at emulator/MinIO only | Implemented; **container execution unverified here** (no Docker; returns honest 503 `terraform_unavailable`) | `backend/internal/terraform/terraform.go`, `backend/internal/jobs/terraform.go`, `httpapi/terraform.go` |
| init/validate/plan/approve/apply/destroy as async jobs (202 + run id), plan JSON parsed + validated, apply requires explicit approval of a succeeded, unexpired (1 h) plan (else 409), destroy syncs/removes resources | Implemented | `httpapi/terraform.go`, `terraform.go` |
| SSRF guards (`internal/security.ValidateEndpointURL` — loopback/emulator/MinIO only), HCL rejecting `amazonaws.com` / embedded keys, refusal of non-emulator AWS credentials in env | Implemented | `backend/internal/security/security.go`, `providers_security_test.go`, `httpapi_security_test.go` |
| Frontend Terraform UI | **Not found** — no Terraform page/component in `frontend/src/`; Terraform is API-only today | `frontend/src/pages/`, `components/` listing |

### 2.7 Monitoring / audit / ops

| Feature | Status | Evidence |
|---|---|---|
| Prometheus `/metrics` (request counts/duration by chi route pattern, engine-up gauge; `/metrics` excluded from counters), `/readyz` (live Postgres/Redis/engine checks; readiness gated only on Postgres), `/healthz`, `/api/v1/health` | Implemented; **`/metrics` is unauthenticated and proxied publicly by nginx** (see §5 R5) | `backend/internal/metrics/metrics.go`, `httpapi.go:587-666,1215-1217`, `frontend/nginx.conf` (`location = /metrics`) |
| Grafana dashboards (overview, labs) + Prometheus scrape config, optional `monitoring` compose profile | Implemented, **unverified live** (no Docker) | `infrastructure/monitoring/` |
| Audit: every mutation audited; admin-only `GET /api/v1/audit` (limit ≤100); retention **90 days, deleted not exported** (`audit_cleanup` job) | Implemented | `backend/internal/store/postgres.go:286`, `docs/security/model.md:269-274`, migration `003_jobs_audit.sql` |
| Cleanup reconciliation (`cleanup_reconcile` job + `POST /api/v1/admin/cleanup-reconcile`, dry-run by default) | Implemented | `backend/internal/jobs/reconcile.go`, `httpapi/reconcile.go` |
| Backup/restore (`scripts/backup.sh`/`restore.sh`, pg_dump/pg_restore, round-trip verified on a scratch DB; restore refuses non-empty target without `--force`) | Implemented, verified (scratch DB) | `scripts/` |
| Host readiness `infrastructure/scripts/check-host.sh` (KVM/Docker/libvirt/RAM/disk; installs nothing, changes no firewall) | Implemented | `infrastructure/scripts/check-host.sh` |
| E2E acceptance test (register → lab → VPC → subnets → VM → fault → repair → validate → XP → destroy → cleanup, against fake gRPC engine + simulated host agent) | Implemented, 1 test in `tests/e2e/e2e_test.go` | `tests/e2e/` |

### 2.8 Frontend pages (as audited)

Routes in `frontend/src/App.tsx`: `/` Dashboard, `/learn`, `/courses`,
`/labs`, `/labs/:id/topology`, `/labs/:id/workstation`, `/console`,
`/login`, `/register`. Components: AWS-console-style header/nav/table/card/
button/badge in CloudForge branding (`AwsHeader`, `AwsNav`, `AwsTable`,
`AwsCard`, `AwsButton`, `AwsBadge`), `LabLauncher`, `Terminal`, `TopologyCanvas`
(`@xyflow/react`). Service catalog (pre-Phase 1): **17 entries** in
`frontend/src/services/catalog.ts` — but most are **redirects, not consoles**:
Lambda→`/learn`, S3→`/courses`, RDS/DynamoDB/IAM/KMS→`/learn`, EBS→`/labs`,
VPC/Subnets/Route Tables/SGs/LBs→`/console`, CloudWatch→`/`. Only
EC2→`/labs`, Courses, Labs, Console Home map to a real page. Regions are
local only: `cf-local-1` / `cf-local-2` (no real AWS regions). Search caps
at 8 results, substring match. There is **no per-service console page, no
resource explorer, no account menu, no favourites UI, no global nav areas
beyond the header/nav shell** — that is Phase 1 work (§7).

**Phase 1 (2026-11-22) replaced the hardcoded catalog:** the frontend nav,
search and All Services page now render from `GET /api/v1/services`
(86 services, operations + fidelity derived from the live providers
registry); dedicated consoles exist for EC2, S3, VPC and IAM, plus a
generic service console for every other registry service, a Resource
Explorer (`/explorer`), account menu + Account Settings (`/account`),
Administration (`/admin`, quotas + audit), Recently Visited + Favorites
(per-user localStorage), three local regions (`cf-local-1/2/3`), and a
`FidelityBadge` on every service/operation/resource. See §10.

### 2.9 Compose modes

| | Lite (`docker-compose.light.yml`) | Full (`docker-compose.yml`) |
|---|---|---|
| Services | frontend, api, **worker**, network-engine, postgres, redis | frontend, api, **worker** (added Phase 1), network-engine, postgres, redis (+ optional MinIO, + monitoring Prometheus/Grafana) — still no host-agent service (§2.2) |
| Limits | 1,280 MiB total caps; Postgres 64 MB buffers/20 conns; Redis 128 MB, no AOF | No resource limits set |
| Networking | Simulated engine, no `NET_ADMIN`, `HOST_AGENT_MODE=simulated` | Engine `NET_ADMIN`; real netlink selectable but unverified |
| Labs | 1 lab / Micro / 30 min, enforced in code (`quota.LiteLimits`) | Default 3 labs, up to 8 h (admin-configurable) |
| Verified | Live lite lab `READY` in ~6 s; 2nd lab 429; 60-min lab 429 (MILESTONE_LOG) | Verified working on user's Fedora host per task context; `docker compose up` itself never ran in this sandbox |

## 3. Missing-feature inventory (b) — vs master prompt §6 A–N, §9, §10

Per-service status: **Implemented** (backend + frontend + tests) /
**Partial** (some ops, or backend-only) / **Registry-only** (registry entry,
no route/page) / **Not started**. "Reuse" = what Phase 1–6 can build on.
Full per-service detail (operations, fidelity, mirrored screens) is in
`docs/SERVICE_CATALOG.md`; this is the summary.

| § | Category | Implemented | Partial / Registry-only | Not started |
|---|---|---|---|---|
| A | Compute | Labs-as-EC2-instances (via host agent, simulated/Docker/libvirt) | EC2 API (registry-only), Lambda (registry-only, invoker missing) | ECS, EKS (explicitly UNSUPPORTED), Fargate, Elastic Beanstalk, Batch, Lightsail |
| B | Storage | S3 buckets via MinIO (Create/List over HTTP; Put/Get adapter-only) | EBS (concept only — no volume API; catalog redirects to `/labs`) | EFS, FSx, Storage Gateway, S3 Glacier / lifecycle / versioning / replication |
| C | Networking | VPC/subnet/route/SG via lab resources + Rust engine (simulated default; real netlink unverified) | VPC CreateVpc (registry-only as an API; the real path is lab resources) | IGW/NAT GW as first-class resources (only `gateway` kind in lab resources), NACLs, ELB/ALB/NLB, Route 53, CloudFront, Direct Connect, VPN, Transit Gateway |
| D | Databases | — | DynamoDB (registry-only + adapter, no route/page) | RDS, Aurora, ElastiCache, Redshift, Neptune, DocumentDB, QLDB |
| E | Security / IAM | IAM policy evaluation subset (HTTP-exposed); roles (learner/instructor/admin) as CloudForge roles, not AWS IAM | STS, KMS (in-memory simulators, no routes) | IAM Users/Groups/Roles/Policies CRUD, Access Analyzer, Policy Simulator UI, Secrets Manager, Cognito, WAF, Shield, GuardDuty, CloudHSM, ACM |
| F | Messaging | — | SQS, SNS, EventBridge (in-memory simulators, no routes) | SES, MQ, AppSync (messaging), EventBridge Scheduler |
| G | Serverless | — | Lambda (registry-only) | API Gateway, Step Functions, AppSync, EventBridge Scheduler |
| H | Containers | Docker via host agent (containers as lab VMs) | — | ECS, EKS, ECR, Fargate, App Runner |
| I | Developer Tools | Terraform (containerised, §2.6) | Terminal (allowlisted shell) as a Cloud9-like surface | CodeBuild, CodeDeploy, CodePipeline, Cloud9, X-Ray, CodeCommit |
| J | Management / Governance | Audit log (90 d), quotas, `/metrics` + `/readyz`, Grafana dashboards | CloudWatch-style monitoring (health/metrics only — no alarms, logs, dashboards UI in-app) | CloudTrail-style console (audit is API-only, no UI), Config, Organizations, Systems Manager, Cost Explorer/Budgets, Trusted Advisor, Resource Explorer (no such page) |
| K | Analytics | — | — | Athena, Glue, EMR, Kinesis, QuickSight, Redshift, Lake Formation, OpenSearch |
| L | ML / AI | — | — | SageMaker, Bedrock, Rekognition, Comprehend, Lex, Polly, Transcribe |
| M | Migration | — | — | DMS, Application Migration Service, DataSync, Transfer Family, Snowball |
| N | Additional | — | — | CloudFormation, Service Catalog, Backup (beyond `scripts/`), License Manager, Marketplace |
| §9 | Terraform Learning | Terraform execution (§2.6) + `curriculum/terraform/terraform.yaml` track | No Terraform UI, no state/lock visualisation, no plan-diff UI | Terraform Cloud-style workspace UI, module registry, policy (Sentinel/OPA) teaching |
| §10 | Learning Center | Curriculum tracks/lessons/assessments/achievements/XP/break-and-fix (§2.3) | Guided labs mostly not launchable; no Learning Center home, paths, or certification-style tracks UI | Learning paths, skill assessments UI, sandbox challenges catalogue, instructor dashboards |

**Biggest gaps, in order:** (1) no per-service consoles at all — the
AWS-duplicate UI (§8) is the largest single missing piece; (2) no worker in
full compose (§2.2); (3) DynamoDB/SQS/SNS/KMS/STS/Lambda are registry
fiction without routes — either wire them or downgrade the registry;
(4) RDS/ELB/Route 53/CloudWatch/CloudTrail/ECS have no backend at all;
(5) categories K–N are entirely not started (acceptable: they are Phases 5–6,
not Phase 1–2).

## 4. Dependency graph (d)

```
Browser ── nginx :8080 (in container; host :80 before / :8080 after port change)
  └─► Go API :8080 before / :8081 after ── chi router ── Postgres 16 (pgx) ── Redis 7 (cache + rate limit)
       ├─► Rust network engine :50051 (gRPC, plaintext) ── simulated | rtnetlink/nftables
       ├─► Host agent :8082 (only if HOST_AGENT_ADDR set; simulated | Docker | libvirt)
       ├─► MinIO :9000 (S3, optional profile) · DynamoDB Local :8000 (not in compose)
       ├─► Terraform containers (hashicorp/terraform:1.9.8, Docker socket)
       └─► Worker (Postgres jobs table, FOR UPDATE SKIP LOCKED) — lite compose only
  Prometheus :9090 scrapes API /metrics · Grafana :3000 (monitoring profile)
```

### Go (`backend/go.mod`, module `github.com/cloudforge/cloudforge/backend`, `go 1.26.0`)

| Dependency | Version | Used for |
|---|---|---|
| `github.com/go-chi/chi/v5` | v5.5.0 | Routing |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | HS256 JWT |
| `github.com/jackc/pgx/v5` | v5.7.5 | Postgres |
| `github.com/minio/minio-go/v7` | v7.0.95 | S3 via MinIO |
| `github.com/prometheus/client_golang` | v1.25.0 | `/metrics` (forces Go 1.26) |
| `github.com/redis/go-redis/v9` | v9.7.3 | Cache + rate limiting |
| `golang.org/x/crypto` | v0.57.0 | Argon2id + bcrypt |
| `golang.org/x/net` | v0.59.0 | Terminal WebSocket |
| `google.golang.org/grpc` / `protobuf` | v1.78.0 / v1.36.12 | Engine gRPC client |
| `gopkg.in/yaml.v3` | v3.0.1 | Curriculum YAML |

No `docker/docker` client (deliberately removed; raw Engine API v1.41 over
the socket in `hostagent/docker.go`), no ORM, no Asynq (jobs are a Postgres
table poller, despite README's "Redis (jobs)" diagram label — correct that
diagram in Phase 1: jobs live in Postgres, Redis holds cache + rate-limit
counters only). Three empty package dirs (`audit/`, `events/`, `users/`)
are placeholders — remove or implement in Phase 1, do not leave empty dirs
as implied scope.

### Rust (`rust-network-engine/Cargo.toml`, workspace, edition 2021, rust 1.75+)

Crates: `engine-core`, `ipam`, `grpc-server` (binary `network-engine`),
`firewall`, `routing`, `fault-injection`, `telemetry`. External:
`tonic 0.12`, `prost 0.13`, `tokio 1`, `tracing 0.1`, `serde 1`,
`rtnetlink 0.14`, `nftables 0.5` (real backend only), `tonic-build 0.12` +
`protoc-bin-vendored 3` (build). Depends on: `proto/cloudforge/v1/*.proto`
(shared contract; Go stubs generated into
`backend/internal/networkclient/gen/`).

### Frontend (`frontend/package.json`)

React 19.3.1, `react-router-dom` 7.18.6, `@tanstack/react-query` 5.108.2,
`@xyflow/react` 13.0.2 (topology), `@xterm/xterm` 6.0.0 + `addon-fit`
(terminal), `lucide-react` 1.77.0 (icons — no AWS icons), Tailwind 4.3.5,
Vite 8.3.6, TypeScript ~7.0.2, Vitest 5.1.1, oxlint 1.90.0. Build:
`tsc -b && vite build`.

### Docker images (all pinned — no `:latest`)

| Image | Tag / digest | Where |
|---|---|---|
| `golang` → distroless | `1.26.0-bookworm@sha256:2a0ba…` → `gcr.io/distroless/static-debian12:nonroot` | `backend/Dockerfile` (non-root, `EXPOSE 8080`) |
| `node` → `nginx` | `24.12.0-alpine` → `nginx:1.27.0-alpine` | `frontend/Dockerfile` (USER nginx, pid `/tmp/nginx.pid`) |
| `rust` → `debian` | `rust:1.100-bookworm` → `debian:bookworm-slim` | `rust-network-engine/Dockerfile` (USER cloudforge; `protobuf-compiler` unpinned — deliberate, Debian dropped the pinned revision) |
| `postgres` / `redis` | `16.4` / `7.4` | compose (full + lite) |
| `minio/minio` | `RELEASE.2024-08-17T01-12-02Z` | compose optional profile |
| `prom/prometheus` / `grafana/grafana` | `v3.3.0` / `11.5.2` | compose monitoring profile |
| `hashicorp/terraform` | `1.9.8` (alt `opentofu/opentofu:1.9.2`) | `config.go:228` / Terraform runner |

### External services

Postgres 16 (users, labs, resources, jobs, audit, curriculum, quotas,
Terraform runs, terminal sessions — migrations `001`–`007`,
`backend/migrations/`), Redis 7 (course cache 60 s, terminal-token hash
fallback, rate-limit counters; fail-open except auth rate limiting),
MinIO (S3), DynamoDB Local (expected at `:8000`, not deployed),
AWS emulator/LocalStack (expected at `:4566`, not deployed). No real AWS
account, no external cloud provider — by design.

## 5. Architecture / security risks (c) — file-grounded

Ordered by severity. Each item cites the file that proves it; nothing here
is invented.

| # | Risk | Evidence | Fix (phase) |
|---|---|---|---|
| R1 | **Unauthenticated plaintext gRPC engine on `:50051`.** No TLS, no auth metadata, port published on the host in both compose files. Anyone reaching `:50051` can create/delete networks, subnets, firewall policies, NAT, inject faults. The internal network segmentation (`management_net` internal) helps only if the host port is not published — it is. | `backend/internal/networkclient/client.go:117` (`insecure.NewCredentials()`); `rust-network-engine/crates/grpc-server/src/main.rs` (`0.0.0.0:50051`); compose `ports: "50051:50051"` | **CLOSED (token) in Phase 7, mTLS still open:** shared-secret token on gRPC metadata (`x-cloudforge-engine-token`), enforced on every RPC incl. Health in the Rust server + Go client; engine fails closed without a token; `:50051` no longer published in either compose file. mTLS is the documented next step, not built. Evidence: `grpc_auth.rs` (3 cases), live walk §1/§1d. |
| R2 | **No worker in full compose → labs never provision** (§2.2). Silent failure mode: API returns 202, lab stays `CREATED`, user sees a stuck lab. | `deployments/compose/docker-compose.yml` (no worker); `httpapi.go:1057-1060` | **Fixed in Phase 1:** worker service added to full compose (`docker-compose.yml:87`, `WORKER_ADDR: ":8083"`). The loud startup check (fail when Postgres mode has no worker) is still open — no such check exists yet. |
| R3 | **No JWT revocation.** A stolen token is valid for up to 12 h; logout is client-side only; secret rotation invalidates everyone. No `jti`, no denylist. | `docs/security/model.md`; no revocation code in `backend/internal/auth/` | **CLOSED in Phase 7:** `jti` on every token; Postgres denylist (`revoked_tokens`/`issued_tokens`, migration 013), Redis, then in-memory fallback; logout revokes; admin can revoke a user's sessions. Checked on every authenticated request. Pre-Phase-7 tokens (no `jti`) cannot be individually revoked. Evidence: `revocation_test.go`, `TestPostgresRevocationIntegration`, live walk §6. |
| R4 | **Dev-default secrets ship in code.** JWT `dev-only-change-me` (`cmd/api/main.go:64`, warns loudly but still starts), MinIO `minioadmin`/`minioadmin` defaults (`config.go:223-224`), host-agent `DevToken = "dev-only-change-me-host-agent"` (`hostagent/server.go:37-39`), Grafana `admin/admin` default (compose comment). Any shared deployment that forgets `.env` is wide open. | Cited files | **CLOSED in Phase 7:** `ValidateSecrets()` — API and worker refuse to start in non-dev mode when a production secret is unset or still a known dev default, naming the variable; dev defaults require the explicit `CLOUDFORGE_DEV=1` opt-in. Grafana password is now required by compose. Evidence: `TestValidateSecretsFailsClosed`, live walk §0a. |
| R5 | **`/metrics` unauthenticated and publicly proxied.** nginx proxies `/metrics` with no auth (`frontend/nginx.conf`); `httpapi.go:1217` mounts it outside the auth group. Exposes request counts/durations and engine state to anyone. Low sensitivity (route patterns only, no IDs — `httpapi.go:420`), but it is an unauthenticated information endpoint by design, not by accident. | `httpapi.go:1217`, `nginx.conf` | **CLOSED in Phase 7:** token auth (`X-Metrics-Token` or Bearer), loopback-only when no token is configured; nginx returns 404 for `/metrics`; Prometheus scrapes with the token. Evidence: `security_p7_test.go` (`TestMetricsRequiresToken`, `TestMetricsLoopbackOnlyWithoutToken`, `TestMetricsAcceptsBearerToken`), live walk §1b. |
| R6 | **JWT in `localStorage` (XSS = full account takeover).** Any XSS in the React app (e.g. via rendered curriculum/lesson content) can exfiltrate the token. No CSP headers in nginx, no `HttpOnly` cookie alternative. | `frontend/src/services/api.ts:66` (`localStorage.setItem(TOKEN_KEY…)`); `nginx.conf` (no CSP/`X-Frame-Options` headers) | **PARTIALLY CLOSED in Phase 7:** session token in an `HttpOnly`, `SameSite=Lax` cookie (`cf_session`); CSP + `X-Content-Type-Options` + `X-Frame-Options: DENY` + `Referrer-Policy` + `Permissions-Policy` in the API and nginx; HSTS only when TLS is on. Residual: the `Authorization` header path still works for API clients/tests during transition, and the frontend still keeps a token copy in memory — the follow-up is to make the header path API-client-only. Evidence: `security_p7_test.go`, live walk §1c/§2. |
| R7 | **`X-Forwarded-For` trusted unconditionally → rate-limit evasion.** `clientIP()` takes the first XFF entry with no trusted-proxy check (`httpapi.go:331-341`). Anyone calling the API directly (bypassing nginx) can spoof an IP per request and defeat the per-IP auth limiter (10/5 min). Behind nginx the header is set by nginx, but the API port is directly reachable too. | `httpapi.go:331-341` | **CLOSED in Phase 7:** XFF honoured only when the peer is loopback, a private (RFC 1918/ULA) address, or an explicitly configured `CLOUDFORGE_TRUSTED_PROXIES` IP/CIDR; otherwise the peer IP is used. Residual: the API port must not be published to the open internet. Evidence: `TestTrustedProxyClientIP`, live walk §4. |
| R8 | **Rate limiting is per-IP only, and fail-open for the general API.** A classroom behind one NAT shares one 300/min budget; Redis-down means the general limiter silently disappears (documented, fail-open by design) while auth fails closed (503). Both behaviours are documented in `docs/security/model.md` — the risk is that operators do not read it. | `ratelimit/ratelimit.go`, `docs/security/model.md` | **PARTIALLY CLOSED in Phase 7:** per-user limiter (default 300/min) in addition to per-IP; unauthenticated requests skip it; fail-open on Redis error like the general limiter. Still open: surfacing limiter state on the admin dashboard. Evidence: `TestPerUserRateLimitTriggers`, live walk §3. |
| R9 | **Simulators that look like real AWS services.** KMS keys live only in process memory and vanish on restart; STS credentials are fake and grant nothing; SQS has no visibility timeout/DLQ/FIFO. All are honestly labelled in `providers.go` limitations — the risk is a learner (or a future UI) presenting them as working AWS features. The AWS-duplicate UI rule (§8) exists to prevent exactly this: label + disable. | `providers/simulators.go`, `providers.go:336-481` | Phases 1–6: fidelity badges + disabled actions (§8); never promote a simulator to `REAL`. |
| R10 | **Audit retention is delete-only (90 days, no export).** Compliance-relevant history is silently deleted by `audit_cleanup`; there is no archival step. | `store/postgres.go:286`, `docs/security/model.md:269-274` | **CLOSED in Phase 7:** audit export jobs (CSV/JSON, cap 10,000 events, retention newest 7 by default); the worker's 90-day cleanup exports rows to `audit-archive-<date>.csv` **before** deleting, and a failed export blocks the delete. Retention is configurable (`CLOUDFORGE_AUDIT_EXPORT_RETENTION`). Residual: if `AuditExportDir` is unset the worker logs a warning and still deletes. Evidence: `TestAuditExportJobLifecycle`, `TestExportAuditArchive`, live walk §9. |
| R11 | **Terraform network isolation is approximated, not enforced.** The container uses bridge networking with `host.docker.internal:host-gateway`; the "no network except the emulator endpoint" rule is approximated, not enforced by a network namespace (MILESTONE_LOG M6–M7 limitations). A malicious `provider.tf`/HCL escape could reach more than the emulator. | MILESTONE_LOG M6–M7; `terraform/terraform.go` | **PARTIALLY CLOSED in Phase 7:** `CLOUDFORGE_TF_NETWORK_MODE=isolated` runs the container with `--network none` (no egress at all — the emulator is unreachable, plans needing it fail honestly). Default `bridge` mode still only approximates isolation. No network-namespace + emulator-allowlist mode was built; HCL fuzzing was not done. Evidence: `TestTFHostConfigIsolated`. |
| R12 | **No TLS anywhere; no CORS/CSP/security headers.** HTTP-only by design for a local platform (`docs/security/model.md` threat model: trusted operator, trusted container networks, not safe for the open internet). The risk is scope creep: the moment this is exposed beyond localhost, every credential and token is plaintext on the wire. | `nginx.conf`, `cmd/api/main.go` (plain `ListenAndServe`), `docs/security/model.md` | **PARTIALLY CLOSED in Phase 7:** security headers in the API and nginx (CSP, XCTO, XFO, Referrer-Policy, Permissions-Policy; HSTS only when TLS is on) + an opt-in TLS termination path (self-signed cert generator, compose override, host `:443` → container `:8443`). HTTP-only remains the default for local dev. Residual: the TLS compose/nginx path is **unverified in the sandbox** (no Docker/nginx) — verify on a real host. Evidence: `TestSecurityHeadersPresent`, live walk §1c; cert generation verified with openssl. |
| R13 | **In-memory fallback silently loses data.** If Postgres is unreachable the API starts with in-memory stores "with a warning" and all data is lost on restart; `/readyz` reports Postgres as `disabled` rather than failing. Fine for dev, dangerous if a misconfigured `.env` puts a real classroom in fallback mode. | `cmd/api/main.go` (fallback branch) | Phase 1: in compose, fail startup when Postgres is configured but unreachable; keep fallback for local dev only. |
| R14 | **SSRF guard is allowlist-based but narrow.** `ValidateEndpointURL` permits loopback/emulator/MinIO hosts; Terraform endpoint validation and provider endpoints share it. Any new user-supplied URL field must be routed through it — there is no central "all outbound fetches go here" enforcement. | `security/security.go:42-98` | Phase 2+: lint/grep gate in `make verify` that fails on new `http.Get`/`http.Post` to user-supplied URLs outside the guard. |

## 6. Sandbox limits (what this audit did NOT verify)

This sandbox has no Docker, KVM, `/dev/kvm`, libvirt, or nginx binary; the
Go toolchain may need reinstalling to 1.26 (`go.mod` targets Go 1.26) and
Rust lives at `~/.cargo/bin/cargo`. Therefore **not verified here**:
`docker compose build/up/config`, real KVM/libvirt VM provisioning, the Rust
real-netlink/nftables backend (compiles; default is SIMULATED), Terraform
container execution (honest 503 here), nginx config syntax, Grafana
dashboards against live Prometheus, and the port change itself (parallel
work, §1). Verified here / on the user's Fedora host instead: Go tests
(19 packages), frontend tests (72 vitest), Rust tests (74), live lite lab
provisioning, curriculum seeding, launch/destroy/bulk-destroy flows. Say so
honestly in every phase report — do not upgrade "built" to "verified".

## 7. Phased roadmap (e) — Phases 0–7

**Phase 0 — Audit (this document).** Deliverable: this file +
`SERVICE_CATALOG.md` + `SERVICE_FIDELITY.md` + MILESTONE_LOG entry.
Verified by: repo-wide read; every claim cites a file. Done.

**Phase 1 — Console foundation — DONE 2026-11-22 (see §10).** Global console mirroring AWS navigation:
service search (over the full registry, not 17 hardcoded entries), left nav
per §4 main-navigation areas, service registry as the single source of truth,
resource explorer (cross-service list/search of real lab resources),
region selector (`cf-local-1`/`cf-local-2`, extensible), account menu,
breadcrumbs, Recently Visited, Favorites, common layouts (list/detail/wizard
shells), fidelity badges on every resource, all wired to real APIs (no
dead links, no buttons for unsupported ops). Plus: add the worker to full
compose (R2), remove or implement the empty `audit/`/`events/`/`users/`
packages, fix the 14-vs-17 curriculum count. Verified by: `make verify`
(Go + Rust + frontend tests) + a live walk of every nav area against a
running lite stack, with each link landing on a real page or an honest
"not yet built" state — never a dead button.

**Phase 2 — Core infrastructure.** IAM (users/groups/roles/policies CRUD +
policy simulator UI, replacing the evaluate-only endpoint), VPC (Your VPCs,
subnets, route tables, IGW/NAT, security groups, NACLs as first-class
resources on the Rust engine), EC2 compute (Instances table + detail tabs,
launch wizard, start/stop/terminate through the host agent), S3 (Buckets →
Objects → Properties/Permissions/Management/Metrics, Put/Get/Delete over
HTTP), CloudWatch-style monitoring (metrics, alarms, logs views over the
real Prometheus data) and CloudTrail-style audit (audit-log UI with filters
and export). Verified by: backend + frontend tests per service + live E2E
walk per service (create → list → detail tabs → action → delete, §8).

**Phase 3 — Data, messaging, serverless.** DynamoDB (tables/items over the
real adapter, HTTP routes added), RDS (Postgres/MySQL as containerised
databases in labs), SQS/SNS/EventBridge (durable, persisted simulators —
not in-memory-only), Lambda (real Docker-based invoker with runtimes,
replacing the registry-only claim), API Gateway (routes → Lambda/containers).
Verified by: per-service tests + live E2E walks; simulators graduate from
in-memory to Postgres-persisted or are explicitly downgraded in fidelity.

**Phase 4 — Containers & developer tools.** ECS/Fargate (tasks/services),
ECR (local registry), EKS (explicit decision: local k3s/kind or stays
UNSUPPORTED with reasons), CodeBuild/CodePipeline teaching surfaces,
Cloud9-style workstation hardening. Verified by: per-service tests + live
E2E walks + container-lifecycle E2E (push → run → stop → clean).

**Phase 5 — Management, analytics, ML, migration.** Config/Systems Manager/
Cost Explorer teaching surfaces, Resource Explorer completion, Athena/Glue/
Kinesis teaching simulators, SageMaker/Bedrock concept labs (clearly
SIMULATED — no real model hosting claims), DMS/DataSync teaching flows.
Verified by: per-service tests + live E2E walks; every ML/analytics surface
carries a fidelity badge and a "what is simulated" note.

**Phase 6 — Terraform Learning + Learning Center.** Terraform UI (runs,
plan-diff, state view, approvals), module registry, Learning Center home,
learning paths, instructor dashboards, guided labs wired to launchable lab
definitions (close the 14-track/4-lab gap), certification-style tracks.
Verified by: per-service tests + live E2E walks + a full learner journey
(enrol → lesson → guided lab → assessment → achievement) end-to-end.

**Phase 7 — Production readiness & hardening — DONE 2026-11-22 (see §16).**
Engine token auth (R1, mTLS still open), JWT revocation (R3), fail-closed
secrets (R4), `/metrics` restriction (R5), cookie sessions/CSP (R6,
header path still works during transition), trusted-proxy XFF (R7),
per-user rate limits (R8), audit export + archive-before-delete (R10),
Terraform `--network none` isolated mode (R11, bridge mode still
approximates), TLS option + security headers (R12, TLS compose path
unverified in sandbox), full-mode compose resource limits,
backup/restore re-verified, §20 tracking docs completed. Verified by:
`make verify` (exit 0) + E2E acceptance test + a live lite walk with
real curl responses for every hardening item + the Phase 1–6
regression flows. Still open from §5: R2's loud startup check (no
worker in Postgres mode), R13 (Postgres-required startup in compose),
R14 (SSRF grep gate), and the Phase 7 residuals in §16.

## 8. AWS-duplicate UI — core requirement (user decision, 2026-11-22)

**Every service must get an AWS-duplicate UI, so a user learns to navigate
the real AWS console by using CloudForge.** Mirror the real AWS console's
layout, navigation, and workflows per service as closely as possible — same
left-nav structure, same table columns, same detail tabs, same wizard steps,
same action names and placement — so muscle memory transfers to real AWS.
Examples (minimum depth per service): EC2 (Instances table: Instance ID /
Name / State / Type / IPs / VPC / Subnet / AZ / Launch time; detail tabs
Details / Security / Networking / Storage / Monitoring / Status Checks /
Logs / Tags / Events); S3 (Buckets / Objects / Properties / Permissions /
Management / Metrics); VPC (Your VPCs / Subnets / Route Tables / Gateways /
Security Groups / Network ACLs); IAM (Users / Groups / Roles / Policies /
Access Analyzer / Policy Simulator); RDS (Databases / Snapshots /
Connectivity / Monitoring / Logs / Backups); Lambda (Functions / Code /
Configuration / Triggers / Permissions / Monitoring). Apply that depth to
every service in Phases 1–6 — **a generic list page does NOT count as a
service console.**

The global console must also mirror AWS navigation patterns: service search,
region selector, account menu, breadcrumbs, Recently Visited, Favorites, and
the §4 main-navigation areas (Home, All Services, Resource Explorer,
Compute, Storage, Networking, Databases, Security and Identity, Application
Integration, Containers, Serverless, Developer Tools, Management and
Governance, Monitoring, Migration and Transfer, Analytics, ML and AI,
Terraform Learning, Learning Center, Account Settings, Administration).

**Guardrails (non-negotiable):** CloudForge's own branding only — no AWS
logos, trademarks, proprietary icons, or copied assets; never claim it is
AWS or operated by Amazon. Mirror interaction patterns and workflows, not
the brand. **UI must reflect actual backend capabilities: do NOT display
active buttons for unsupported operations — SIMULATED / PARTIAL /
UNSUPPORTED operations must be labelled as such and disabled or clearly
marked, never shown as a working AWS feature.** The per-service "mirrors /
differs" field in `docs/SERVICE_CATALOG.md` records this per service.

**Phase 1–6 verification rule (binding):** each service console is verified
by walking **create → list → detail tabs → action → delete end-to-end in a
live sandbox run** (real Postgres/Redis/engine, lite or full stack as
applicable). A service is not marked complete on UI alone, on backend alone,
or on tests alone — all three plus the live walk are required. The walk must
also check that every disabled/labelled operation is actually disabled (click
it and confirm it does nothing / explains why) and that no resource is shown
`READY` until its operation actually succeeded (see
`docs/SERVICE_FIDELITY.md`).

## 9. What to do first (for the parent orchestrator)

1. Land the port change (§1) and fill in the worker port from its report;
   re-run `make up-lite` on Fedora and confirm `:8080`/`:8081`.
2. Fix R2 (worker in full compose) before Phase 1 — it is a one-service
   addition and it silently breaks full-mode labs today.
3. Start Phase 1 with the service registry as the source of truth for nav +
   search (replace the hardcoded 17-entry catalog), then build the first
   full AWS-duplicate console (EC2) as the template every later service
   copies.

## 10. Phase 1 — Console foundation (delivered 2026-11-22)

### What was built

| Piece | Where | Notes |
|---|---|---|
| Service registry endpoint | `GET /api/v1/services`, `GET /api/v1/services/{id}` (`backend/internal/httpapi/services.go`) | 86 services; metadata (name, category, description, icon, route, availability, fidelity, resource types, docs link, learning exercises, mirrors/differs) in one Go table; operations + fidelity derived from the existing `providers.Registry` — no duplicate registry. S3 `PutObject`/`GetObject` correctly report no HTTP route; DynamoDB/STS/KMS/SQS/SNS/EventBridge/Lambda are `registry-only`. |
| Resource Explorer endpoint | `GET /api/v1/explorer/resources` (`backend/internal/httpapi/services.go`) | Real data only: own labs (admin: all), lab resources, courses, users (admin-only). Filters `q`/`type`/`status`, sort `name`/`created_at`/`status`, pagination ≤100. Region scoping is display-only: every row is `cf-local-1`, `region_scoped: false`; another region returns an empty list + `region_note`. |
| Global console shell | `frontend/src/components/Layout.tsx`, `AwsHeader.tsx`, `AwsNav.tsx` | Registry-driven nav/search; top nav (logo, service search, region selector cf-local-1/2/3, notifications from live `/api/v1/health`, help/docs, account menu with profile + role + sign out); left nav per §4 areas (Home, All Services, Recently Visited, Favorites, Resource Explorer, registry categories, Learning Center, Terraform Learning, Account Settings, Administration); breadcrumbs via `PageHeader` on every page. Recently Visited + Favorites are per-user localStorage (JWT `sub` key). |
| Common layouts + fidelity badges | `components/ServiceConsole.tsx`, `components/AwsBadge.tsx` (`FidelityBadge`) | Shared console shell (header, left sub-nav, tabs) and fidelity badge (REAL/API_EMULATED/SIMULATED/PARTIAL/UNSUPPORTED + footnote) used on every console. |
| EC2 console | `frontend/src/pages/Ec2Console.tsx` (`/services/ec2`) | Instances table (ID/Name/State/Type/VPC/Subnet/AZ/Launch time) from real labs + lab resources; detail tabs Details/Security/Networking/Storage/Monitoring/Status Checks/Logs/Tags/Events; Launch (→ Labs), Stop and Terminate wired to the real API; Start/Reboot disabled (no backend op). |
| S3 console | `frontend/src/pages/S3Console.tsx` (`/services/s3`) | Real buckets from MinIO; create-bucket wizard wired to `POST /aws/s3/buckets`; tabs Objects/Properties/Permissions/Management/Metrics. Upload disabled: Put/GetObject have no HTTP route. MinIO down → honest 503 state. |
| VPC console | `frontend/src/pages/VpcConsole.tsx` (`/services/vpc`) | Your VPCs/Subnets/Route Tables/Gateways/Security Groups from the explorer; delete wired to the real resource API; create disabled (VPCs are created as lab resources); NACLs show UNSUPPORTED. |
| IAM console | `frontend/src/pages/IamConsole.tsx` (`/services/iam`) | Policy Simulator wired to real `POST /aws/iam/evaluate`; Users list real (admin-only); Groups/Roles/Policies/Access Analyzer disabled with reasons (no backend API). |
| Generic service console | `frontend/src/pages/ServiceDetail.tsx` (`/services/:id`) | Every other registry service: overview, operations table (fidelity + HTTP route + limitations), learning exercises. Unsupported ops shown disabled. |
| All Services / Explorer / Account / Admin | `pages/Services.tsx`, `pages/Explorer.tsx`, `pages/Account.tsx`, `pages/Admin.tsx` | Search over the registry; explorer create→list→detail→delete walk; account (profile/role/region/favorites/sign out); admin (real quotas + audit, admin-only). |
| Worker in full compose (R2) | `deployments/compose/docker-compose.yml:87` | Worker service added mirroring lite, `WORKER_ADDR: ":8083"`. |
| 14-vs-17 track count | `backend/internal/store/memory_curriculum.go`, `postgres_curriculum.go` | See §2.3. `GET /curriculum/tracks` = 14, `GET /courses` = 17 (correct). |
| Empty placeholder packages | `backend/internal/audit|events|users/` | Removed (empty dirs, nothing imported them). |
| Worker port docs | `docs/IMPLEMENTATION_STATUS.md` §1, `MILESTONE_LOG.md` Phase 0 entry | TBD → **:8083**. |

### Verification (2026-11-22, this sandbox)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all packages pass, `cargo test --workspace` 74 passed (Rust untouched), frontend build + vitest 77/77 + oxlint 0 warnings.
- Live lite stack (API :8081, worker :8083, Rust engine :50051 simulated, Postgres 16, Redis 7): register → `GET /api/v1/services` = 86 services → tracks 14 / courses 17 → lab `ec2-basics` 202 → worker → `READY` (~2 s) with a VPC resource → explorer total 19 (17 courses + lab + VPC), search `p1-live-lab` = 1, region `cf-local-2` = 0 + note → IAM evaluate `Allow` → S3 list honest 503 (MinIO down) → destroy → `DESTROYED`. Frontend production build served on :8080 (Vite preview; nginx is Docker-only) — `/`, `/services`, `/explorer` all 200, `/api/v1/services` proxied 200 with 86 services.
- `docker compose config/up`, real KVM, real netlink, Terraform containers and nginx config syntax remain unverified here (no Docker/KVM/nginx) — as in all prior milestones.

### Honest limitations / still open

- The loud startup check for "Postgres mode with no worker" (R2 follow-up) is not built yet.
- Region scoping is display-only (all resources are `cf-local-1`); labs carry no per-region field.
- S3 objects, IAM CRUD, VPC create wizard, CloudWatch alarms, CloudTrail export are Phase 2–3 work; their console actions are disabled with reasons, not faked.
- Notifications are live control-plane health only (no per-resource alert feed yet).
- Favorites/Recently Visited are per-user localStorage; backend persistence is a stretch goal, not done.

## 11. Phase 2 — Core infrastructure (delivered 2026-11-22)

### What was built

| Piece | Where | Notes |
|---|---|---|
| IAM store + migration | `backend/migrations/008_iam.sql`, `backend/internal/store/iam.go`, `postgres_iam.go` | Tables `iam_users`, `iam_groups`, `iam_group_members`, `iam_roles`, `iam_policies`, `iam_attachments`, `iam_access_keys`, `iam_sessions`. IAM identities are separate teaching identities — CloudForge platform users (`auth.User`) are NOT AWS IAM. Access-key secrets and session tokens shown exactly once at creation; only SHA-256 hash stored (`store.HashSecret`). ARNs are local: `arn:cloudforge:iam::local:<kind>/<name>`. Admin-only CRUD; AssumeRole any authenticated user. |
| IAM policy validation + evaluation trail | `backend/internal/providers/iam.go` (`ValidatePolicyDocument`, `ValidateTrustPolicy`, `EvaluateWithTrail`) | Every stored document is probe-evaluated via `providers.Evaluate` before storage — policy JSON is never stored without being evaluated. Rejects: bad JSON, wrong Version, empty statements, empty Action/Resource, `${...}` variables, NotAction/NotResource, Principal in identity policies, conditions other than StringEquals on `aws:username`. `EvaluateWithTrail` returns decision + per-statement trail (explicit deny / allow / default deny + which statement decided it). Trust policies are recorded, not evaluated — assuming a role is an explicit admin API call. |
| IAM HTTP API | `backend/internal/httpapi/iam.go` | Users/Groups/Roles/Policies CRUD, group members, attach/detach, access keys (create/list/revoke, `POST .../status`), `POST /aws/iam/roles/{id}/assume` (short-lived session, expiry enforced), `GET /aws/iam/sessions/{id}`, `POST /aws/iam/evaluate` (inline policies + `user_id`/`group_ids`/`role_ids`/`policy_ids`, trail + `policies_evaluated`). All 8 new IAM registry capabilities are `API_EMULATED` — `REAL` is forbidden ("nothing here is AWS"). |
| VPC detail API | `backend/internal/httpapi/vpc.go`, `resources.go` | VPC/subnet create/list/delete with CIDR validation (subnet must sit inside its VPC, no overlap); route tables + add/delete routes (engine-owned: engine failure = 502, nothing persisted); security groups with ingress/egress rules (DB-authoritative; engine firewall policy is a one-shot snapshot — the engine has NO update/delete RPC for firewall policies, so later edits mark `engine_policy_applied=false` with a note instead of pretending an update happened) + attach-to-instance; IGW attach/detach (DB-only, labelled SIMULATED — engine has no IGW RPC); NAT create/delete via engine (translated address auto-allocated from TEST-NET-3 `203.0.113.10+n`, documented as documentation-range placeholders); Tags PATCH (`aws:` prefix rejected). NACLs stay UNSUPPORTED (engine has no NACL RPC). |
| Route next-hop fix | `backend/internal/httpapi/vpc.go` (`resolveRouteNextHop`, `vpcGatewayAddress`, `firstHostAddress`) | **Real bug found live:** `handleAddRoute` passed the raw target (`"local"` or a resource id) as the engine `Gateway` and never set `Interface`, but the engine's `RouteEntry::validate` requires a non-empty interface and an IPv4 gateway — so no route could ever be added. Fixed by mapping targets to the engine's required `(gateway, interface)` pair: interface is always the lab id (engine network id = lab id), gateway is the VPC router address (first host of the VPC CIDR) for `local`/VPC/IGW targets, the NAT translated address for NAT targets, the instance's recorded private IP for instance targets, or the first host of a subnet's CIDR. Route records now store `gateway` + `interface`. |
| EC2 Start / Reboot | `backend/internal/httpapi/instances.go`, `backend/internal/jobs/jobs.go`, `backend/internal/store/store.go` (`LabStore.Start`) | `POST /labs/{id}/start` re-provisions a STOPPED lab through the same path as Create (202 PROVISIONING, never READY until the worker succeeds). `POST /labs/{id}/reboot` is a real host-agent `OpRebootVM` on the lab's instance resource; 503 `reboot_unavailable` when no host agent or no VM recorded — never faked. |
| Instance launch starts the VM | `backend/internal/httpapi/resources.go` (instance case) | **Real bug found live:** instance creation called `OpCreateVM` only, leaving every API-created VM in state `"defined"` (created, never started) — so reboot could never succeed and a launched instance was not actually running. Now CreateVM → StartVM → record state `"running"` + private IP (via `listHostAgentVMs`); if StartVM fails, the defined VM is deleted again and the API returns 502 with nothing persisted. |
| S3 object API | `backend/internal/httpapi/s3.go`, `backend/internal/providers/s3.go` | Objects list/upload (base64 JSON through the backend, 10 MiB cap — no direct browser→MinIO credentials leak)/download/delete, metadata, versioning get/set, bucket policy get/set, bucket delete. Object ACLs are NOT exposed by minio-go — the Permissions tab shows policy only and says so. MinIO down → honest 503, as in Phase 1. |
| Monitoring + audit API | `backend/internal/httpapi/monitoring.go` | `GET /api/v1/monitoring` gathers the real Prometheus default registry into JSON families + the same live readiness checks `/readyz` runs (shared `Server.readiness()`); absent series are absent, never zero-filled. Alarms are threshold rules evaluated live against current values; definitions live in server memory (lost on restart — UI/docs say so); creating an alarm on a never-observed metric is rejected with the known-metrics list. `GET /api/v1/audit` now filtered (`actor_id`/`action`/`resource_type`/`resource_id`/`lab_id`/`q`/`from`/`to`, RFC3339); `GET /api/v1/audit/export?format=csv\|json` admin-only, max 1000 events. `GET /api/v1/labs/{id}/events` returns the lab's audit history. |
| Consoles (frontend) | `frontend/src/pages/IamConsole.tsx`, `VpcConsole.tsx`, `S3Console.tsx`, `Ec2Console.tsx`, `CloudWatchConsole.tsx`, `CloudTrailConsole.tsx` | IAM: full CRUD for Users/Groups/Roles/Policies, access keys (secret-once panel), assume role (token-once panel), simulator with Stored-identities vs Inline-JSON modes + trail table + deciding statement; Access Analyzer still UNSUPPORTED. VPC: lab picker, per-section create forms with CIDR validation notes, route-table detail, SG detail + engine-sync note, gateway detail, TagsEditor, topology link; NACLs UNSUPPORTED. S3: objects tab with prefix breadcrumb navigation, upload/download/delete, metadata detail; Properties (versioning toggle); Permissions (policy editor + "object ACLs not exposed by minio-go" note); Management; Metrics links to CloudWatch. EC2: Start (STOPPED only) and Reboot (READY/RUNNING only) wired; launch wizard from `GET /labs/definitions` (>30-min definitions disabled in lite); Monitoring tab from live monitoring; Logs/Events tabs from real lab audit history; per-resource Tags CRUD. CloudWatch (`/services/cloudwatch`): component health, alarms create/delete, metric families, 15s refetch, in-memory-alarm note. CloudTrail (`/services/cloudtrail`): audit filters incl. datetime-local from/to, detail view, CSV/JSON export, admin-only note. |
| Registry + catalog | `backend/internal/providers/providers.go`, `backend/internal/httpapi/services.go` | Seeded capabilities: s3 ListObjects/DeleteObject/Get/SetBucketVersioning/Get/SetBucketPolicy/DeleteBucket (API_EMULATED), iam CreateUser/ListUsers/DeleteUser/CreateGroup/CreateRole/CreatePolicy/CreateAccessKey/AssumeRole (API_EMULATED), cloudwatch GetMetrics (PARTIAL) / CreateAlarm (SIMULATED), cloudtrail LookupEvents/ExportEvents (PARTIAL). `httpRouteFor` maps all new ops to real routes. |

### Verification (2026-11-22, this sandbox)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all 19 packages pass, `cargo test --workspace` 74 passed (Rust untouched), frontend build + vitest 89/89 (was 77/77; +12 Phase 2 parser/endpoint tests) + oxlint 0 warnings.
- Live lite stack (API :8081, worker :8083 degraded — no Postgres, no jobs, `database_connected=false`, fake Redis stub on :6379 for the rate-limit tests, Rust engine :50051 SIMULATED, host agent :8082 simulated, frontend dev :5173 + Vite preview :8080): **walk script `/tmp/cf-live/walk.sh`, 63/63 checks pass** — health; admin+learner register; learner IAM create → 403; IAM user 201 / duplicate 409; policy validation rejections (`StringLike` condition → 400, `${aws:username}` → 400, nothing stored); allow+deny policies attached; evaluate Allow with trail; explicit deny wins; implicit deny; group + member + group policy; access key secret-once + absent from list + revoke; role assume (token once, session validates); lab `ec2-basics` → READY; bad VPC CIDR → 400, VPC/subnets (outside-VPC → 400, overlap → 400); route table + local route 201 + delete 200; SG + rule (engine policy applied) + instance created **running with a private IP** + SG attach 200 + rule delete; IGW attach/detach 200; NAT 201 with translated address; tags (`aws:` → 400, valid → 200); stop → STOPPED, start → 202 → READY, reboot → 200; lab events real audit history; S3 list/upload/objects → 503 (MinIO down); monitoring 200 + alarm on unknown metric → 400 + alarm on real metric 201 + delete 200; audit filter + CSV/JSON export 200; delete IAM user + destroy lab 200. Frontend dev :5173 and preview :8080 both 200, `/api/v1/health` proxied 200 (`host_agent reachable`, simulated; `lite: true`).
- Two real backend bugs were found and fixed during this live walk (see the table above): routes could never be added (missing interface/gateway mapping) and launched instances were never started (VM left `"defined"`).
- The Postgres IAM integration test (`postgres_iam_test.go`) skips here — no Postgres server in this sandbox. Migration 008 + Postgres IAM code follow the same patterns as migrations 001–007 (verified earlier when Postgres was up).

### Honest limitations / still open

- No Docker, KVM, or `/dev/kvm` in this sandbox: real VMs, containers, real netlink, Terraform containers and nginx config syntax remain unverified here. The Rust engine default backend is SIMULATED; its real rtnetlink/nftables backend compiles but is unverified.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns 410; the GitHub release `RELEASE.2025-10-15T17-29-55Z` ships no binary assets — MinIO moved binaries behind its commercial download portal). S3 object flows are verified by unit tests + the live 503 path only, not against a live MinIO.
- No Postgres in this sandbox: worker runs degraded (no jobs processed), Postgres IAM integration test skips; rate-limit tests need Redis (a minimal RESP stub at `/tmp/fake-redis.py`, sandbox test aid only, was used for `make verify`).
- Alarms are in-memory only (lost on restart). Trust policies are recorded, not evaluated. S3 object ACLs unavailable via minio-go. Engine firewall policy is a one-shot snapshot (no update/delete RPC — later SG rule edits mark `engine_policy_applied=false` with an explanatory note). Access Analyzer is still not built. NAT translated addresses are TEST-NET-3 documentation-range placeholders, not real public IPs.
- Region scoping is display-only (all resources are `cf-local-1`); labs carry no per-region field.
- The loud startup check for "Postgres mode with no worker" (R2 follow-up) is not built yet.
- Notifications are live control-plane health only (no per-resource alert feed yet).
- Favorites/Recently Visited are per-user localStorage; backend persistence is a stretch goal, not done.

## 12. Phase 3 — Data and application services (delivered 2026-11-22)

### What was built

| Piece | Where | Notes |
|---|---|---|
| Phase 3 store + migration | `backend/migrations/009_phase3.sql`, `backend/internal/store/phase3.go` | 23 tables `p3_*` (generic shape: `id TEXT PK, name TEXT, data JSONB, created_at, updated_at`; unique index on `name` where the service requires it; expression indexes on `data->>'…'` for child collections). `Phase3Store` interface with memory + Postgres implementations; table names come only from an allowlist (`P3Collections`), never caller input. |
| Phase 3 logic package | `backend/internal/phase3/phase3.go` | Service semantics shared by handlers: DynamoDB plain/typed item conversion; SQS visibility/DLQ rules; EventBridge pattern subset matching; DNS record validation + wildcard resolution; in-process round-robin + target probing; Lambda subprocess runner (timeout, output cap, `prlimit` memory cap, temp workdir, uid 65534 when root); RDS helpers (`ValidatePGIdentifier`, real create/drop via pgx, `pg_dump -Fc` snapshot, `pg_restore` restore); Redis namespaced-client helpers. |
| DynamoDB | `backend/internal/providers/dynamodb.go`, `backend/internal/httpapi/phase3.go`, `frontend/src/pages/DynamoDbConsole.tsx` (`/services/dynamodb`) | Real DynamoDB JSON API to DynamoDB Local when it answers (probe cached 60 s), else a durable Postgres-backed subset (CreateTable/PutItem/GetItem/Query/Scan/DeleteItem/DeleteTable; hash key only, no sort key/indexes, Scan capped at 100). Fidelity: `API_EMULATED` via Local, `PARTIAL` via the subset; the console names the live backend on every response. Plain items (`{"id":"1"}`) and DynamoDB-typed items both accepted. |
| RDS | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/RdsConsole.tsx` (`/services/rds`) | Each instance is a real Postgres database + role on the platform Postgres ("PostgreSQL-compatible, not Aurora"). Engine postgres 14/15/16 only (mysql/mariadb → 400). Master password generated (20 chars), shown exactly once at creation, only its SHA-256 hash stored. Snapshot = real `pg_dump -Fc` file under the workdir; restore creates a NEW instance via `pg_restore`. Start/stop per instance is UNSUPPORTED → 409 (shared platform database). Rollback: failed create drops the role + DB again; failed restore drops the new DB + role again. |
| ElastiCache | `backend/internal/httpapi/phase3.go`, `phase3_redis.go`, `frontend/src/pages/ElastiCacheConsole.tsx` (`/services/elasticache`) | Lite: a real namespaced Redis database on the platform Redis — key prefix `cf-cache:<cluster-id>:` + best-effort ACL user (ACL failure recorded in `acl_note`, cluster still works on prefix-only isolation). Engine redis only (memcached → 400). Full mode (dedicated container) → honest 503 naming the missing container path. Keys browser is a read-only SCAN of the prefix (max 100); set capped 64 KiB; flush deletes only that cluster's prefix; metrics memory/hit-rate are platform-Redis-wide, `keys_in_cluster` is prefix-only. |
| ELB | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/ElbConsole.tsx` (`/services/elb`) | SIMULATED with a working in-process round-robin to lab targets. Targets probed on demand (HTTP GET to `http://host:port<health_check_path>` when the target group sets one, else TCP dial; 2 s timeout); only targets whose last probe said `healthy` are eligible — unprobed targets are never assumed healthy. `/route` returns the target a request would go to; no traffic is actually proxied. DNS name is a local teaching name `<name>.cf-local-1.elb.local`, not registered. No engine RPC, no nginx reconfiguration. |
| Route 53 | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/Route53Console.tsx` (`/services/route53`) | SIMULATED: durable zones/records in Postgres. A/CNAME/TXT only (other types → 400); one record set per (name, type) — duplicate → 409; a zone cannot be deleted while it holds records → 409. `GET .../resolve` answers against stored records including `*.` wildcards; no DNS server serves the records to the lab network. |
| SQS | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/SqsConsole.tsx` (`/services/sqs`) | Durable, Postgres-persisted (NOT in-memory). Queues/messages in `p3_sqs_queues` / `p3_sqs_messages`. Receive does NOT delete: the message is invisible until `visible_at`, `receive_count` increments, and at `max_receives` (>0) it moves to the DLQ instead of being returned. Redrive re-enqueues DLQ messages back (new message id). Retention enforced by purging expired messages on list/receive. DLQ cannot itself have a DLQ. FIFO not built; no long polling; receipt handle = message id; body capped 256 KiB. |
| SNS | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/SnsConsole.tsx` (`/services/sns`) | Durable topics/subscriptions/deliveries in Postgres. Real fan-out: SQS queues (durable enqueue), HTTP(S) endpoints (real POST, SSRF-guarded to loopback via `security.ValidateEndpointURL`), Lambda functions (real invocation). Every delivery recorded with status/detail. Email/SMS not built → 400. |
| EventBridge | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/EventBridgeConsole.tsx` (`/services/eventbridge`) | Durable rules in Postgres. Pattern subset: `source` / `detail-type` arrays + nested `detail` subset matching (keys absent from the pattern ignored; extra detail keys ignored). Targets SQS/SNS/Lambda, validated at rule creation (missing target → 404). State ENABLED/DISABLED; disabled rules do not match. AWS service events are NOT generated; events arrive via PutEvents only (1–10 entries). No schedule expressions, no input transformers. |
| Lambda | `backend/internal/phase3/phase3.go` (`RunLambda`), `backend/internal/httpapi/phase3.go`, `frontend/src/pages/LambdaConsole.tsx` (`/services/lambda`) | Real execution in a sandboxed subprocess (runtimes that actually execute here: `python3.12`/`python3.11`/`python3`, `nodejs20`/`nodejs22`/`node`). Code is a whole program: event JSON on stdin, result on stdout. Timeout (default 10 s, max 60 s), output cap 64 KiB, `prlimit --as=<memory>` when the binary exists, fresh temp workdir, minimal env (PATH/HOME + function env), no host shell (`exec.Command` argv, never `sh -c`), uid 65534 (nobody) when the API runs as root. The result names the sandbox that actually applied. This is NOT container isolation and NOT AWS Lambda — the console and docs say so; the old registry claim ("functions run in isolated Docker containers") was corrected to PARTIAL/subprocess. Versions increment on code update; aliases map name → version; invoke by alias or `$LATEST`. Triggers from SQS (async, fire-and-forget on send) and EventBridge (via rule targets); API Gateway triggers are configured on the API, not the function. |
| API Gateway | `backend/internal/httpapi/phase3.go`, `frontend/src/pages/ApiGatewayConsole.tsx` (`/services/apigateway`) | REST/HTTP only (WebSocket not built). Resources/methods with integrations `lambda` (function id/name resolved) or `http` (URL must pass the SSRF guard). Deploy snapshots current resources into a stage; later edits need redeploy. Invoke surface `/api/v1/gateway/{apiId}/{stage}/*` requires platform auth; every invocation logged to `p3_apigw_logs` (method, path, status, duration, detail). Lambda output: stdout parsed as JSON when possible, else `{"output": …}`. Lambda error → 502 with error/output; timeout → 504. |
| Registry + catalog | `backend/internal/providers/providers.go`, `backend/internal/httpapi/services.go`, `services_test.go` | Seeded capabilities upgraded from registry-only to real implementations: SQS CreateQueue/SendMessage/ReceiveMessage/DeleteMessage → `cloudforge-sqs`, `API_EMULATED`; SNS CreateTopic/Publish/Subscribe → `cloudforge-sns`, `API_EMULATED`; EventBridge PutEvents/PutRule → `cloudforge-eventbridge`, `API_EMULATED`; Lambda InvokeFunction + CreateFunction `PARTIAL` (was SIMULATED/docker); DynamoDB Query/Scan/DeleteItem/DeleteTable `API_EMULATED` (+ CreateTable/PutItem/GetItem/ListTables limitations updated: fallback to the Postgres subset when DynamoDB Local is unreachable, no longer "otherwise unavailable (503)"); RDS CreateDBInstance/DeleteDBInstance/CreateDBSnapshot/RestoreDBInstanceFromDBSnapshot `PARTIAL`; ElastiCache CreateCacheCluster/DeleteCacheCluster `PARTIAL`; ELB CreateLoadBalancer/CreateListener/CreateTargetGroup/RegisterTargets `SIMULATED`; Route53 CreateHostedZone/ChangeResourceRecordSets `SIMULATED`; API Gateway CreateRestApi/CreateResource/CreateDeployment `PARTIAL`. `httpRouteFor` maps all new ops to real routes; `TestServicesRegistry` updated (DynamoDB is `available` with HTTP routes now, no longer `registry-only`). |
| Consoles (frontend) | `frontend/src/pages/{DynamoDb,Rds,ElastiCache,Elb,Route53,Sqs,Sns,EventBridge,Lambda,ApiGateway}Console.tsx`, `frontend/src/App.tsx`, `frontend/src/pages/phase3-consoles.test.tsx` | All 10 consoles wired into `App.tsx` before the generic `services/:id` route. Each mirrors its AWS console's layout/tables/detail tabs/wizards/action names in CloudForge branding, with fidelity badges and honest not-built notes (RDS Start/Stop disabled with the reason; Lambda states the subprocess sandbox, not Docker; ELB/Route 53 labelled SIMULATED). Render tests for all 10 consoles (stubbed fetch + localStorage, React Query retries off). |

### Verification (2026-11-22, this sandbox)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all packages pass (with `DATABASE_URL` set so the Postgres integration tests run, including `TestPostgresPhase3Integration`), `cargo test --workspace` 74 passed (Rust untouched), frontend build + vitest 99/99 (was 89/89; +10 Phase 3 console render tests) + oxlint 0 warnings.
- Live lite stack (API :8081, worker :8083, Rust engine :50051 SIMULATED, real Postgres 16, real Redis 7.0.15, DynamoDB Local :8000, host agent :8082 simulated, frontend dev :5173 + Vite preview :8080): **walk script `/tmp/cf-live/walk-phase3.sh`, 120/120 checks pass** (transcript `/tmp/cf-live/transcript-phase3.txt`) — health (`lite: true`, `database_connected: true`, `redis_connected: true`); unauthenticated Phase 3 call → 401; DynamoDB via Local (`backend: dynamodb-local`, `fidelity: API_EMULATED`) create/Put/Get/Query/Scan, empty name → 400, duplicate → 409, delete item → GetItem 404, delete table; RDS create 201 (real DB `walkdb` + role `cf_walkdb` verified in `pg_database`/`pg_roles`, master password present in the create response only, no `password_hash` leaked), mysql → 400, stop → 409, snapshot 201 (real `pg_dump -Fc` file, verified as "PostgreSQL custom database dump" on disk), restore-as-new 201 (second real DB), delete both (DBs + roles gone, verified by count 0), get deleted → 404; ElastiCache create lite 201 (real prefixed keys verified in Redis with `--scan`), memcached → 400, full mode → 503, set/get/list keys, metrics, flush (prefix empty afterwards), delete; ELB create LB + target group + register healthy (`:8081`) and unhealthy (`:1`) targets, health check 1/2 healthy, route → `localhost:8081`, empty address → 400, delete TG/LB; Route 53 zone + A record, duplicate → 409, MX → 400, wildcard A, resolve hit + wildcard hit + miss, delete non-empty zone → 409, delete records + zone; SQS durable queues (row count verified in Postgres), send/receive (invisible while in flight), visibility-timeout expiry + re-receive, at max receives the message moves to the DLQ (`moved_to_dead_letter_queue: 1`, DLQ holds it), redrive back, delete by receipt handle, purge, delete queues; SNS topic, email → 400, non-loopback HTTP → 400 (SSRF guard), SQS subscribe, publish → 1/1 deliveries delivered, fan-out message received from the queue, deliveries list, unsubscribe, delete topic; EventBridge rule to SQS, missing target → 404, matching event → 1 matched + 1 delivery + message in queue, non-matching → 0, disable → 0 matched, delete rule; Lambda python echoer invoke (output correct, sandbox named), unsupported runtime → 400, failing function → 200 with `status: error`, sleeper (1 s timeout) → honest 504 with `timed_out: true` and the invocation recorded, alias `prod` invoke by qualifier, invocations list; API Gateway create API + POST `/echo` Lambda resource, websocket integration → 400, deploy `prod`, invoke via `/api/v1/gateway/{api}/prod/echo` → Lambda output, undeployed stage → 404, HTTP `/health` resource + redeploy (200, existing stage updated), invoke → 200, request logs list; registry shows all 10 services `available` with honest fidelity; frontend preview `/services/sqs` + `/services/lambda` 200, dev `/services/dynamodb` 200, the three console titles found in the production bundle; DynamoDB fallback: DynamoDB Local killed, 65 s wait (probe cache is 60 s), create table → `backend: postgres-subset`, `fidelity: PARTIAL`, Put/Get work, item row verified in `p3_dynamodb_items`, delete table. Frontend production build served on :8080 (Vite preview; nginx is Docker-only).
- Three real bugs were found and fixed during this live walk (see below); the walk was re-run to 120/120 after each fix. Two more were found by the walk's failure cases and fixed in the same session (see "Bugs found live and fixed").

### Bugs found live and fixed (not just UI)

1. **ELB health checks marked every target unhealthy.** `handleTGHealth` passed only the target's `address` (`"localhost"`) to `phase3.ProbeTarget`, dropping the stored `port` (8081) and the target group's `health_check_path` — so the probe dialled a bare host and failed with "missing port in address", and the route correctly refused with 503 `no_healthy_targets`. Fixed by changing `ProbeTarget(ctx, address, port, healthCheckPath)`: a bare host is probed as an HTTP GET to `http://host:port<health_check_path>` when a path is set, else a TCP dial of `host:port`; the caller now passes the target's port and the group's path. Tests: `TestProbeTarget` now includes a live `httptest` server positive case (host+port+path joined correctly) and a 404-path negative case.
2. **RDS provisioning failed on role ownership.** The platform Postgres user `cloudforge` had `CREATEDB` but not `CREATEROLE` (sandbox setup), so `CREATE ROLE` failed with 42501 — the API honestly returned 502 `rds_provision_failed` with nothing persisted. After `ALTER USER cloudforge WITH CREATEROLE`, the next step failed: `CREATE DATABASE ... OWNER cf_walkdb` requires the provisioning user to be a member of the new role ("must be able to SET ROLE"). Fixed in `RDSCreateDatabase`: after creating the role, `GRANT <role> TO <provisioning-user>` (from the DSN; the role creator has ADMIN OPTION), so the user can own, drop, and dump the database. Test: `TestRDSCreateDropIntegration` (live Postgres, gated on `DATABASE_URL`) creates a DB + role, asserts the owner is the new role, and drops both. Deployment note: the platform Postgres user needs `CREATEDB` + `CREATEROLE` — document this for real hosts (the sandbox `cloudforge` user now has both).
3. **DynamoDB Local was never used — the adapter sent unsigned requests.** The adapter posted to DynamoDB Local with no `Authorization` header, so every call failed with `MissingAuthenticationToken` and the probe silently fell back to the Postgres subset (the walk's first runs showed `backend: postgres-subset` throughout, which is how this was caught). Fixed by sending a dummy SigV4 `Authorization` + `X-Amz-Date` header — Local never validates the signature. Two subtleties found by direct probing: the credential must be alphanumeric (Local rejects `cloudforge-local` with `UnrecognizedClientException: The Access Key ID or security token is invalid` — the hyphen is the problem; `cloudforgelocal` works), and Local's error body uses capital-M `Message`, which the adapter previously ignored (it fell back to the bare "400 Bad Request"). Fixed both; `do()` now returns a typed `DynamoError{Target, Type, Msg}` so callers can distinguish error kinds. Test: `fakeDynamo` now rejects unsigned requests like the real Local, and the adapter test asserts the header is sent.
4. **Duplicate DynamoDB table returned 502 instead of 409.** Every adapter error was mapped to 502 `dynamodb_error`. Local returns `__type: "com.amazonaws.dynamodb.v20120810#ResourceInUseException"` (fully qualified — the first fix compared the bare name and missed it). Fixed: `CreateTable` maps that type to sentinel `ErrDynamoTableExists`, the handler returns 409 `already_exists`; the fake-server test asserts the sentinel for both the bare and qualified type names.
5. **TypeScript build broke on the Phase 3 consoles (found by `npm run build`, fixed in-session).** `AwsTable<T extends { id: string }>` rejected the consoles' `Record<string, unknown>` rows (TS2741), which in TS 7 also produced phantom TS6133 "unused variable" errors for names that were plainly used inside the same JSX subtree. Fixed by giving each console's `Rec` an `id: string` and having `listOf`/`itemsOf` synthesize an id from `name` (DynamoDB tables) or the row index (deliveries, items) when the API row has none; request-body objects that were wrongly typed `Rec` are now plain `Record<string, unknown>`. Also fixed: `TestServicesRegistry` still expected DynamoDB to be `registry-only` (updated to assert `available` + HTTP routes for all seven DynamoDB operations).
6. **Walk-script issues (script, not product):** the first re-run registered the same admin twice → 409 → empty token → everything 401 (which incidentally proved users persist in Postgres across API restarts); the script now uses a per-run email suffix. The SQS delete step extracted `id` from the receive response, which carries `message_id`/`receipt_handle` instead → now uses `receipt_handle`. The Lambda timeout step expected 200; the API honestly returns 504 with `timed_out: true` → the script now expects 504.

### Honest limitations / still open

- No Docker, KVM, or `/dev/kvm` in this sandbox: real VMs, containers, real netlink, Terraform containers and nginx config syntax remain unverified here. The Rust engine default backend is SIMULATED; its real rtnetlink/nftables backend compiles but is unverified.
- **DynamoDB Local is not in the Docker Compose files** (it runs on `:8000` on the dev host / sandbox only). On a real host, start DynamoDB Local (or point `DYNAMODB_ENDPOINT` at one) for the `API_EMULATED` path; without it the Postgres subset takes over honestly (`PARTIAL`). The dummy SigV4 credential is local-only and must never be sent to AWS.
- **RDS/ElastiCache full mode is not built**: no containerised per-instance Postgres and no dedicated Redis container — there is no Docker in this sandbox to verify either against, and building an unverifiable Docker path would repeat the Lambda mistake the Phase 0 audit flagged. Full-mode ElastiCache create returns an honest 503 naming the missing container path; RDS has no full-mode path (it always provisions on the platform Postgres).
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns 410) — S3 object flows are unit-tested + live 503 only, as in Phase 2.
- ELB is SIMULATED: round-robin + health checks only; no traffic is proxied, the DNS name is not registered, and there is no engine/nginx integration. Route 53 is SIMULATED: records are durable but no DNS server serves them to the lab network.
- Lambda is a subprocess sandbox, NOT container isolation and NOT AWS Lambda: no VPC attachment, managed scaling, layers, or execution-role model; do not run untrusted code from strangers. API Gateway has no WebSocket APIs, no API keys / usage plans.
- SQS has no FIFO queues and no long polling; SNS has no email/SMS delivery; EventBridge has no schedule expressions, no input transformers, and no AWS service events (PutEvents only). DynamoDB subset: hash key only, no sort key, no secondary indexes, Scan capped at 100, no pagination.
- The platform Postgres user needs `CREATEDB` + `CREATEROLE` for RDS (see bug 2 above); the sandbox `cloudforge` user now has both.
- Region scoping is display-only (all resources are `cf-local-1`); labs carry no per-region field.
- The loud startup check for "Postgres mode with no worker" (R2 follow-up) is not built yet.
- Notifications are live control-plane health only (no per-resource alert feed yet).
- Favorites/Recently Visited are per-user localStorage; backend persistence is a stretch goal, not done.

## 13. Phase 4 — Containers and Developer Tools (delivered 2026-11-22)

### What was built

| Piece | Where | Notes |
|---|---|---|
| Phase 4 store + migration | `backend/migrations/010_phase4.sql`, `backend/internal/store/phase3.go` | 15 tables `p4_*` (same generic shape as Phase 3: `id TEXT PK, name TEXT, data JSONB, created_at, updated_at`), added to the `P3Collections` allowlist; unique-name collections for repos, clusters, services, build projects, pipelines, deploy apps, stacks. Verified on a fresh database after the sandbox VM was replaced: all 15 tables exist after the API's startup migration run. |
| Phase 4 logic package | `backend/internal/phase4/phase4.go`, `ecr.go`, `phase4_test.go` | ECR naming validation (`ValidateECRRepoName`, `ValidateImageTag`, `ParseImageRef`, `Digest`); `RunShell` (buildspec shell, `sh -c`, per-command, timeout/output cap/`prlimit`/uid 65534); `RunArgv` + `TaskRunner` (ECS subprocess runtime, `Start`/`Stop`/`IsRunning`/`ReadLog` 64 KiB tail, `OnStop` callback); buildspec parsing (`install`/`pre_build`/`build`/`post_build`); CloudFormation template parsing (JSON-first then YAML), validation, change planning (CREATE/UPDATE/DELETE/NO_CHANGE/UNSUPPORTED), `Ref`/`Fn::Join`/`Fn::Sub` resolution; the EKS decision (`EKSDecision = "UNSUPPORTED"` + reason) and the Kubernetes learning path (Pod, Deployment, Service, ConfigMap/Secret, Namespace, Job/CronJob — each with its CloudForge equivalent and route). |
| ECR registry backend | `backend/internal/phase4/ecr.go` | `RegistryBackend` interface; `LocalDiskBackend` (content-addressed blobs `<workdir>/ecr/blobs/sha256/<hex>`, manifests `<workdir>/ecr/manifests/<repo>/<tag>.json` + `manifests-by-digest/` index, schemaVersion 2 validation, blob-exists check before a manifest is stored, digest verified on store); `RemoteRegistryBackend` (proxies to a real `registry:2` when `ECR_REGISTRY_URL` is set — monolithic upload POST→PUT; blob-delete returns an error because registry:2 has no blob-delete API). Docker Registry HTTP API subset: `GET /v2/`, `GET /v2/_catalog`, `GET /v2/<name>/tags/list`, `GET/PUT/DELETE /v2/<name>/manifests/<ref>`, `GET /v2/<name>/blobs/<digest>`, `POST /v2/<name>/blobs/uploads/` (monolithic `PUT?digest=`). No chunked/resumable uploads, no OCI index, no auth (local-only — same posture as the unauthenticated engine gRPC, a Phase 7 item). No Docker Hub compatibility claimed. |
| ECR (API) | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/EcrConsole.tsx` (`/services/ecr`) | Repos list/create/delete (non-empty repo → 409; names validated, NOT silently lowercased — uppercase → 400); images list/push/get/delete; push accepts two shapes — strict `{tag, manifest, blobs: [{digest, content_base64}]}` (client-computed digests) and convenience `{tag, layers: ["<base64>", …]}` (manifest synthesised server-side); blobs ≤10 MiB each, digest-verified, immutable-tag re-push → 409; scan = integrity scan (re-read + digest-verify every layer; `scan_status` SUCCEEDED/FAILED, explicitly "NOT a vulnerability (CVE) scan"); lifecycle evaluate (`keep_last_n_tagged` count N, `expire_untagged_days`) applied on push and on demand — accepts both rule shapes, `{"type": "keep_last_n_tagged", "count": N}` and the shorthand `{"keep_last_n_tagged": N}` the console sends. Fidelity PARTIAL. |
| ECS + Fargate | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/EcsConsole.tsx` (`/services/ecs`) | Clusters CRUD (409 if running tasks or active services; list includes `running_tasks`); task definitions register (family+revision, ≥1 container with name/image/command — missing command → 400; per-container cpu/memory validated, memory 0–4096), list (family filter), delete (409 if in use); services create/list/update/delete (launch EC2 requires a cluster, FARGATE clears it; desired 0–10; update = rolling deployment, deployment record COMPLETED when the desired count is RUNNING on the new revision); tasks run (count 1–10, FARGATE needs no cluster; returns **201**), list (filters `cluster_id`/`service_id`/`launch_type`, `running` from the runner), get, stop via `POST /tasks/{id}/stop` (409 if not RUNNING; 409 if service-managed), logs (real process output, field `logs`). Runtime is the honest one: the task command runs as a sandboxed subprocess (`phase4.TaskRunner`: argv, no shell, timeout 3600 s default, output cap, `prlimit` memory cap, temp workdir, uid 65534 when root). No Docker container is started, no image is pulled — the image is recorded and matched against the ECR catalogue; the console says so. Task status comes from the real process (PENDING → RUNNING → STOPPED with a real exit code). **Reconcile circuit breaker:** if 5+ tasks for a service were created in the last 60 s, restarts pause and the service reports `reconcile_throttled` + reason + timestamp (a service update clears it); without this, an instantly-exiting command under a service restarts forever (seen live: 3,200 task rows in ~2 min). Fargate = the same runtime with `launch_type: "FARGATE"`, no cluster required/used. Fidelity: ECS PARTIAL, Fargate SIMULATED. |
| EKS | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/EksConsole.tsx` (`/services/eks`) | **DECISION: UNSUPPORTED (option (b)).** `GET /api/v1/aws/eks/info` returns `{service: "eks", fidelity: "UNSUPPORTED", supported: false, decision, reason, learning_path: [6]}`. Reason: (1) no Docker/Kubernetes in this sandbox to verify a cluster against — building an unverifiable cluster path would repeat the Lambda mistake the Phase 0 audit flagged; (2) a managed-Kubernetes control plane is the heaviest component and would dominate lite classroom-laptop mode; (3) the Kubernetes concepts needed first — Pod, Deployment, Service, ConfigMap, Namespace, Job/CronJob — map directly onto the ECS runtime built in Phase 4, so the learning path teaches them for real instead of against a diagram. The console shows the decision + reason + learning-path table (concept, what it is, CloudForge equivalent, route link, try-it); Create cluster is disabled. A k3s-based EKS remains a possible future phase on a real Docker/KVM host, gated behind a capability check. No static Kubernetes diagram is presented as a cluster. |
| CodeBuild | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/CodeBuildConsole.tsx` (`/services/codebuild`) | Projects list/create/delete (409 if a build is IN_PROGRESS; source validated — local paths only under `CLOUDFORGE_BUILD_SOURCE_ROOT`, no `http(s)/ssh/git@` remotes); start build → 202 IN_PROGRESS, runs in a goroutine; builds list (project filter)/get/logs (256 KiB tail, field `logs`). The build is real: git clone when the source has `.git` (as the API user, then the tree is chowned to 65534 so build steps can write), else a directory copy (symlinks skipped, never followed); buildspec inline or `buildspec.yml`; env from project + buildspec `env.variables`; phases in order, failure names the phase + exit code, timeout → FAILED; artifacts copied via globs to disk; status SUCCEEDED/FAILED with `failure_reason`, `phases_run`, `artifacts`, `sandbox`. Logs are the real log file, not canned text. Fidelity PARTIAL. |
| CodePipeline | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/CodePipelineConsole.tsx` (`/services/codepipeline`) | Pipelines list/create/delete (409 if an execution is IN_PROGRESS; stage validation: source/build/deploy, source path validated, build project must exist by id **or** name, deploy target must be `ecs-service`/`lambda-function`/`terraform`); start → 202; executions list/get with stage visualisation. The run is real: source stage clones/copies to `<workdir>/codepipeline/<exec-id>/source` and records the commit; build stage runs `p4RunBuild` against that checkout (not a re-checkout of the project source); deploy stage is a real ECS rolling deployment (target + service_id + task_definition_id), a real Lambda code update (target + function_id + code_file, version increments), or an honest Terraform failure (`terraform_unavailable`, no Docker here). A failed stage stops the execution; later stages never run; `failure_reason` names the stage. Fidelity PARTIAL. |
| CodeDeploy | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/CodeDeployConsole.tsx` (`/services/codedeploy`) | Applications list/create/delete (409 if a deployment is IN_PROGRESS; target validated at creation — unknown target → the app row is deleted again, 404); create deployment → 202 (captures `previous_revision`: ECS previous task def, Lambda previous code+version); deployments list/get; rollback creates a new deployment restoring the previous revision (only a SUCCEEDED deployment can be rolled back). The deploy is real: ECS — update the service, stop old tasks, reconcile, wait ≤60 s for the desired count RUNNING on the new revision, else FAILED; Lambda — update the code, version+1, verify the stored code == deployed code, else FAILED. Create takes flat fields (`task_definition_id` / `code`), not a nested `revision` object. Fidelity PARTIAL. |
| CloudFormation | `backend/internal/httpapi/phase4.go`, `frontend/src/pages/CloudFormationConsole.tsx` (`/services/cloudformation`) | Own template engine + stack state, NOT Terraform. Template parsing JSON/YAML, parameter defaults (missing parameter with no default → 400), validate-template endpoint (`{valid, problems, supported_types, resource_count}`). Supported types (6, PARTIAL): `AWS::SQS::Queue`, `AWS::SNS::Topic`, `AWS::DynamoDB::Table` (Postgres subset), `AWS::Lambda::Function` (inline `Code.ZipFile`), `AWS::S3::Bucket` (MinIO — no adapter/MinIO down → CREATE_FAILED honestly), `AWS::RDS::DBInstance` (platform Postgres, generated password, hash stored). Other types are reported UNSUPPORTED in the change set, never silently skipped. Change sets are a real diff against current state via `PlanCFNChanges` (CREATE/UPDATE/DELETE/NO_CHANGE/UNSUPPORTED; NO_CHANGE when desired properties equal the properties stored at provision time — best-effort, templates using `Ref` in properties may report UPDATE spuriously, which only re-applies); a change set with only UNSUPPORTED/NO_CHANGE changes is created so the console can show why, but is not executable (409 on execute). Create/update/delete stacks provision through the existing resource APIs synchronously; stack events carry real status; on failure, rollback deletes the resources created in the failed execution (resources deleted by an update are NOT restored — the event log says so). |
| Registry + catalog | `backend/internal/providers/providers.go`, `backend/internal/httpapi/services.go` | Capabilities added: ecr `CreateRepository/DescribeImages/PutImage` (PARTIAL), ecs `CreateCluster/RegisterTaskDefinition/CreateService` (PARTIAL) + `RunTask` (SIMULATED), fargate `RunTask` (SIMULATED), codebuild `CreateProject/StartBuild` (PARTIAL), codepipeline `CreatePipeline/StartPipelineExecution` (PARTIAL), codedeploy `CreateApplication/CreateDeployment` (PARTIAL), cloudformation `CreateStack/CreateChangeSet` (PARTIAL); eks `CreateCluster` limitation updated to the Phase 4 decision reason. Catalog entries updated: `ecs` available/PARTIAL, `eks` unavailable/UNSUPPORTED with decision text, `fargate` available/SIMULATED (route `/services/ecs`), `ecr`/`codebuild`/`codepipeline`/`codedeploy`/`cloudformation` available/PARTIAL. `httpRouteFor` maps all Phase 4 ops to real routes. |
| Consoles (frontend) | `frontend/src/pages/{Ecr,Ecs,Eks,CodeBuild,CodePipeline,CodeDeploy,CloudFormation}Console.tsx`, `frontend/src/App.tsx`, `frontend/src/pages/phase4-consoles.test.tsx` | All 7 consoles wired into `App.tsx` before the generic `services/:id` route. Each mirrors its AWS console's layout/tables/detail tabs/wizards/action names in CloudForge branding, with fidelity badges and honest not-built notes (EKS Create disabled with the decision; ECS states the subprocess runtime; ECR scan states it is an integrity scan, not a CVE scan; CloudFormation states it is not Terraform). Render tests for all 7 consoles. |
| Compose (full mode only) | `deployments/compose/docker-compose.yml`, `.env.example` | `registry:2.8.3` service under the `optional` profile (container `cloudforge-registry`, volume `registrydata`, network `emulator_net`, healthcheck `wget http://localhost:5000/v2/`, NOT exposed on a host port, no auth — local-only, Phase 7 item); api service env gains `ECR_REGISTRY_URL: http://registry:5000` and `CLOUDFORGE_BUILD_SOURCE_ROOT: /var/lib/cloudforge/labs`. Compose YAML parses. **UNVERIFIED here (no Docker in this sandbox)** — the first verified run must be on a real Docker host. Light compose untouched (local-disk backend by default). `.env.example` documents `ECR_REGISTRY_URL=` (empty = local-disk) and `CLOUDFORGE_BUILD_SOURCE_ROOT=` (empty = workdir root). |

### Verification (2026-11-22, this sandbox)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all 21 packages pass (with `DATABASE_URL` set so the Postgres integration tests run, including the fresh-DB migration check — all 15 `p4_*` tables present), `cargo test --workspace` 74 passed (Rust untouched, run by `make verify`), frontend build + vitest 106/106 (was 99/99; +7 Phase 4 console render tests) + oxlint 0 warnings.
- Live lite stack (API :8081, worker :8083, Rust engine :50051 SIMULATED, real Postgres 16, real Redis 7.0.15, frontend dev :5173 + Vite preview :8080): **walk script `/tmp/cf-live/walk-phase4.sh`, 73/73 checks pass, 0 fail** (transcript `/tmp/cf-live/transcript-phase4.txt`) — health (`lite: true`, `database_connected: true`, `redis_connected: true`); unauthenticated Phase 4 call → 401; registry lists all 8 Phase 4 services with honest fidelity; ECR bad name → 400 (nothing created), create repo, push v1 (layers shape) + v2, lifecycle keeps only v2 (asserted), scan SUCCEEDED, registry API `/v2/_catalog` + tags + manifest + blob download, manifest referencing a missing blob → 400, lifecycle evaluate 200, delete image + repo; ECS task def without command → 400, cluster + task def + Fargate run 201 (echo task STOPPED with real exit code 0, logs read back), service desired 1 with `sleep 60` (running_count 1), **circuit-breaker step: an `echo` service throttles after 5 starts in 60 s (task count bounded, `reconcile_throttled: true`), then a scale-to-0 update clears it**; EKS info UNSUPPORTED with the 6-row learning path; CodeBuild remote source → 400, real git build SUCCEEDED (real log lines, artifact `built.txt` on disk), failing build FAILED with the real log naming exit code 3; CodePipeline source → build → deploy (ECS) SUCCEEDED with the service's task definition actually updated (build ran from a git checkout — see bug 2), Terraform deploy honestly FAILED (`terraform_unavailable`); CodeDeploy deploy a new task def (service updated), rollback (service restored to the previous revision); CloudFormation unsupported type → 400 (nothing stored), validate-template, create stack (SQS + SNS + Lambda, `CREATE_COMPLETE`, queue verified in the SQS list, events listed), change set with a real CREATE + an UNSUPPORTED EC2 type (executable), execute (queue2 created), unsupported-only change set not executable → 409 on execute, S3 stack `CREATE_FAILED` with rollback (no MinIO here — honest failure, nothing READY), delete stack (queue gone from the SQS list); cleanup deletes every Phase 4 resource; frontend preview `/services/ecr` + `/services/ecs` 200 and dev `/services/cloudformation` 200; on-disk evidence verified (ECR blobs + manifests, CodeBuild logs, ECS task logs).
- The sandbox VM was replaced **twice** during Phase 4 (second replacement 2026-11-21 ~22:35 UTC, between walk runs 2 and 3): `/tmp` wiped, all processes killed, Postgres/Redis gone. Home survived (Go 1.26 toolchains, `~/workspace/cloudforge`, the Rust engine binary, frontend `node_modules`). Postgres 16 + Redis 7.0.15 were reinstalled via apt; Redis started directly (`redis-server --daemonize yes`, systemd is unavailable); role + DB recreated; no Java in the new VM, so no DynamoDB Local — no Phase 4 flow needs it.

### Bugs found live and fixed (not just UI)

1. **ECR lifecycle silently ignored the console's rule shape.** The walk/console send `{"keep_last_n_tagged": 1}` (shorthand, no `type` field); `p4ApplyLifecycle` only read `{"type": "keep_last_n_tagged", "count": N}`, so the rule matched nothing and the old image was never deleted — which then made the repo refuse deletion with 409 (non-empty) at cleanup. Fixed: the handler normalises both shapes (`type`+`count`/`days`, and the shorthand `keep_last_n_tagged`/`expire_untagged_days`); a rule matching neither is ignored, never treated as a deletion. Regression test: a second lifecycle repo using the shorthand shape. The walk now asserts the post-push list is exactly `['v2']` instead of only printing it.
2. **Pipeline build stage: `git clone` of a chowned checkout failed with exit 128 ("detected dubious ownership").** The pipeline source stage clones the git repo, chowns the checkout to uid 65534, and the build stage clones FROM that checkout. Two fixes were needed, found by direct probing: (a) a `-c safe.directory=...` flag is not inherited by the `upload-pack` child a local clone spawns, and the `GIT_CONFIG_*` env vars do not reach that child either (verified: env trust works for direct commands like `rev-parse`, and global-config trust works for clone, but env trust does not satisfy clone's child); (b) git 2.43 matches `safe.directory` against the git-dir path (`<repo>/.git`), not the worktree root. Fix: `p4GitCmd` passes the trust via `GIT_CONFIG_*` env with both values (worktree + worktree/`.git`) — used for `rev-parse` — and both clone sites fall back to a filesystem copy of the source tree when the clone fails, which for a local-only source is equivalent (full tree + `.git`) and is logged in the build log. Regression test: a pipeline whose source is a real git repo (source clone → chown → build re-clone) now SUCCEEDS and records the commit.
3. **ECS reconcile runaway loop.** A service whose tasks exit immediately restarted forever (OnStop → reconcile → start → exit → OnStop), creating 3,200 task rows in ~2 min for an `echo` service. Fixed with the circuit breaker (5 starts in 60 s → restarts pause, `reconcile_throttled` + reason on the service; a service update clears it). The service keeps ACTIVE status — only restarts pause. Regression test in `TestPhase4ECS` (echo service, sleep 4 s, ≤8 tasks, throttled, scale-to-0 clears it) and walk step 7b. The EcsConsole shows the throttle reason in the Services table.
4. **ECR silently lowercased repo names** instead of rejecting uppercase: the handler did `strings.ToLower` before validation. Fixed: validate as given; `Bad_Name` → 400.
5. **ECR push rejected the `layers` shape** (`json: unknown field "layers"`): the handler only accepted manifest+blobs, but the console and walk send `{tag, layers: [base64]}`. Fixed: the handler also accepts `layers` and synthesises the manifest server-side; invalid base64 → 400; neither manifest nor layers → 400.
6. **ECS task-def register rejected per-container `cpu`** (`json: unknown field "cpu"`): `ecsContainerDef` had no cpu/memory fields. Fixed: added `CPU int json:"cpu"`, `MemoryMB int json:"memory_mb"`, validated (memory 0–4096, cpu ≥ 0), stored per container.
7. **Git clone "detected dubious ownership" (exit 128) when cloning a root-owned repo as uid 65534.** Fixed: clone as the API user with `p4GitCmd` trust, then `p4ChownTree` to 65534 — in both CodeBuild and the CodePipeline source stage. (Superseded in part by bug 2's fallback for the re-clone path.)
8. **Pipeline build stage only read `project_id`**: console/walk send `build_project_name` → "references unknown CodeBuild project \"\"". Fixed: `p4ResolveBuildProject` resolves by id or name (used in create-validation and run).
9. **Deploy stage field names**: handler reads `target` + `service_id`/`task_definition_id` (ECS) / `function_id`/`code_file` (Lambda); console/walk sent `deploy_target` + `target_id`. Fixed console + walk to the real contract.
10. **Logs field is `logs`, not `log`**: EcsConsole, CodeBuildConsole, and the walk read `log`. Fixed all three to `logs`.
11. **Task stop route is `/stop`**: console/walk used `POST /tasks/{id}` → 405. Fixed to `POST /tasks/{id}/stop`.
12. **Run-task returns 201, not 202**: walk expected 202. Fixed walk to 201.
13. **CodeDeploy deployment create takes flat fields** (`task_definition_id` / `code`), not `{"revision": {...}}`: console + walk sent nested revision → 400 "unknown field revision". Fixed console (spreads parsed JSON flat) + walk (flat body).
14. **CFN planner had no NO_CHANGE:** every existing resource planned as UPDATE. Fixed: store resolved `properties` on each stack resource; the planner compares JSON and emits NO_CHANGE when equal (best-effort — `Ref`-using templates may report UPDATE spuriously, which only re-applies). Change-set `executable` counts only CREATE/UPDATE/DELETE.
15. **CFN template is a JSON string, not an object:** the walk sent `"template": {…object…}` → 400 `cannot unmarshal object into Go struct field .template of type string`. The API contract (same as the console textarea) is a string. Fixed the walk to build the body with `json.dumps` (template as a string). The S3-stack call already sent a string and was correct.
16. **Walk-script issues (script, not product):** ECR repo name collided across runs (409 → empty id → cascading 404s) — the walk now uses a per-run `demo/app-$RUN`; services in the walk use `sleep 60` task defs (instantly-exiting commands under a service trip the circuit breaker by design; that path is tested separately as step 7b); a `pkill -f '/tmp/cf-api'` in a restart command matched the exec session's own command line and SIGTERM'd it — restarts now avoid `pkill -f` with a self-matching pattern.
17. **Sandbox umask 0007 turns `MkdirAll(0755)` into 0750** (same class as Phase 3): build/pipeline/task dirs were untraversable by uid 65534. Fixed with explicit `os.Chmod(..., 0o755)` after MkdirAll on `codebuild`/`codepipeline` parents, build base, artifact dir, and `ecs-tasks` base; `TaskRunner.Start` chowns per-task workdirs to 65534:65534 when the API runs as root.
18. Test-expectation errors fixed along the way (test, not product): lifecycle test assumed explicit evaluate deletes 1 (push already applies lifecycle); changeset executability expectations; the CodeDeploy rollback assertion expected td2 after the git-pipeline step made tdGit the pre-deploy revision.

### Honest limitations / still open

- No Docker, KVM, or `/dev/kvm` in this sandbox: the full-mode `registry:2` container path is **unverified** — the compose addition parses but has never run; the first verified run must be on a real Docker host. Real VMs, containers, real netlink, Terraform containers and nginx config syntax remain unverified here, as in Phases 1–3. The Rust engine default backend is SIMULATED; its real rtnetlink/nftables backend compiles but is unverified.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns 410) — S3 object flows are unit-tested + live 503 only, as in Phase 2; CloudFormation S3 buckets therefore CREATE_FAILED honestly here (verified live with rollback).
- **No DynamoDB Local in the current sandbox VM** (no Java after the VM replacement) — no Phase 4 flow needs it, but the Phase 3 fallback note still applies.
- ECR: no chunked/resumable uploads, no OCI index, no auth (local-only), no Docker Hub compatibility, no vulnerability (CVE) scanning — the scan is an integrity check only. The local-disk backend stores blobs under the workdir; blobs are never garbage-collected (orphaned blobs from deleted images stay on disk until a cleanup pass exists).
- ECS/Fargate: no container is started and no image is pulled — the runtime is a sandboxed subprocess; no VPC networking, service discovery, or load balancer integration; desired count max 10; task timeout 3600 s. Task logs are capped at a 64 KiB tail.
- EKS is UNSUPPORTED by decision (see above) — no cluster, no kubectl, no managed node groups. The learning path is a table of concepts mapped to the ECS runtime, not a cluster.
- CodeBuild: local sources only (no remote git); no Docker-based builds; no build caching; no parallel builds per project beyond the IN_PROGRESS guard. CodePipeline: no manual approvals, no parallel stages, no cross-account; Terraform deploy honestly fails without Docker. CodeDeploy: no canary/blue-green traffic shifting (no load balancer integration to shift against); rollback restores the previous revision but does not restore resources deleted by an update.
- CloudFormation: 6 resource types only (PARTIAL); no `DependsOn`, no `Fn::GetAtt`, no nested stacks, no drift detection, no intrinsic functions beyond `Ref`/`Fn::Join`/`Fn::Sub`; NO_CHANGE detection is best-effort and may report UPDATE spuriously for `Ref`-using templates; resources deleted by an update are NOT restored on rollback; stack delete failures leave `DELETE_FAILED` + 502.
- Frontend on `:8080` was verified with Vite preview of the production build, not the Docker nginx image (no Docker here).
- Region scoping is display-only (all resources are `cf-local-1`).

## 14. Phase 5 — Terraform Learning (delivered 2026-11-22)

> **Renumbering note (master prompt §19 wins).** The Phase 0 audit's
> roadmap (§7) numbered Phases 5–6 as "Management/analytics/ML/
> migration" and "Terraform Learning + Learning Center". The master
> prompt §19 renumbers from here on: **Phase 5 = Terraform Learning
> (§9), Phase 6 = Advanced services, Phase 7 = Production hardening.**
> This section is Phase 5 under the master-prompt numbering. The
> Phase 0 roadmap's old "Phase 5" (management/analytics/ML/migration)
> and old "Phase 6" (Terraform Learning + Learning Center) are
> superseded by this numbering; the work itself is unchanged, only
> the phase numbers moved.

### What was built

| Piece | Where | Notes |
|---|---|---|
| Course content | `backend/internal/tflearn/lessons.go` | 55 lessons (L1 15, L2 20, L3 20) + 7 Level 4 projects, each lesson with explanation, use case, code example, guided exercise, expected output, common mistakes, knowledge check. L1 reuses the existing curriculum Terraform track (`curriculum/terraform/terraform.yaml`) via the `curriculum_lesson` field — no second copy of that track. L2–L3 are new content living only here. |
| HCL engine | `backend/internal/tflearn/hcl.go` | Project-wide `ValidateHCL` (delimiter balance, block structure, undeclared variables/resources, forbidden hosts, embedded secrets, unsupported-type warnings), `FormatHCL` (indentation + spacing, heredocs untouched), `ExtractResources`, `ComputePlan` (local static diff), `DetectDrift` (declared vs state vs live). |
| Provider docs | `backend/internal/tflearn/provider.go` | 9 `cloudforge_*` resource types with arguments, attributes, mapped CloudForge API, fidelity, example + install/versioning docs. Only resources with a real API mapping are listed. |
| Store + migration | `backend/migrations/011_tflearn.sql`, `backend/internal/store/phase3.go` (`P3Collections`) | 6 generic collections `p5_tflearn_*` (projects, files, progress, runs, states, locks) on the shared Phase 3 store (Postgres + in-memory fallback). |
| HTTP API | `backend/internal/httpapi/tflearn.go` (25 routes) | Dashboard (real progress/projects/runs/achievements), courses + lesson complete (correct answer required), provider, projects CRUD, files (save validates project-wide), local validate/format/plan-preview, plan-preview approve (1 h TTL, learning record only), container executions delegating to the M7 runner (`enqueueTerraformRun` — refactored out of `enqueueTerraform`, and `loadApprovablePlan` shared with the M7 apply handler), execution history (status refreshed from the linked M7 run), state (versioned snapshots, 10-min locks, backup/restore-as-new-version, drift). |
| M7 refactor | `backend/internal/httpapi/terraform.go` | `enqueueTerraform` split into `enqueueTerraformRun` (no HTTP response) + a wrapper that preserves the lab endpoints' responses exactly; `terraformAvailability` extracted; `loadApprovablePlan` shared by the M7 apply handler and the tflearn apply path. No M7 behaviour changed (existing M7 tests pass unmodified). |
| Frontend | `frontend/src/pages/TerraformLearning.tsx`, routes `/terraform-learning` + `/services/terraform-learning` in `App.tsx` | Dashboard, Courses (lesson detail + knowledge check), Projects & Editor (file explorer, textarea + HCL highlighting preview, save/validate/format/plan-preview, execution panel with confirm checkbox, state panel with lock/backup/restore/drift), Provider tab. Local checks labelled "local"; container execution labelled with the honest 503 when Docker is absent. |
| Docs | `docs/TERRAFORM_LEARNING.md` (new), `docs/SERVICE_CATALOG.md` (`terraform-learning` row), `docs/SERVICE_FIDELITY.md` (Terraform Learning row) | Fidelity table for what was delivered and what was not. |

### Verification (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean; `go test ./...` all 22 packages pass (new: `tflearn` package tests, 6 `TestTFlearn*` httpapi tests); `cargo test --workspace` 74 passed; frontend build + vitest 109/109 + oxlint 0 warnings; `make verify` exit 0 (run twice, second run after the E2E-fake fix below). E2E acceptance test `tests/e2e` passes (6.9s).
- **RDS integration test resolved (environmental, not a product bug).** The first full `go test ./...` run failed only on `TestRDSCreateDropIntegration` with SASL auth failure: the run used a password-less `DATABASE_URL`, but the sandbox Postgres role requires a password over TCP (`scram-sha-256`). The dev DSN with the password (recorded in `/tmp/cf-live/walk-phase4.sh`) makes the whole suite pass, including the RDS integration test. No Phase 3 code or test was changed.
- **E2E regression found and fixed (pre-existing, Phase 2).** `tests/e2e` failed at `create gateway nat-1` with `CreateNAT not implemented`: Phase 2 made NAT-gateway creation call the engine, but the `e2e-fakes` fake engine never implemented `CreateNAT`/`DeleteNAT`. Added both as in-memory stubs in `backend/cmd/e2e-fakes/main.go` (test support only — no production code changed); the E2E then passes. SG creation does not call the engine at create time, so no other fake RPC was needed.
- Live lite walk against a rebuilt API (:8081) + worker (:8083) + engine (:50051 simulated) + Postgres/Redis: dashboard, a Level 1 lesson with its knowledge check (wrong answer → not completed, right answer → completed), project creation from the VPC template, file save (valid + invalid HCL → 400 with the real issues), validate, format, plan preview + approve, apply/destroy without confirmation → 409, execution without a lab → 409, execution with a lab but no Docker → 503 `terraform_unavailable` with a FAILED run recorded. Full transcript in the MILESTONE_LOG Phase 5 entry. Walk script: `/tmp/cf-live/p5/walk-phase5.sh` (19 steps, all PASS; transcript `/tmp/cf-live/p5/walk.log`).

### Bugs found live and fixed (not just UI)

1. **HCL validation was per-file, so valid projects were refused.** Saving `extra.tf` referencing `cloudforge_vpc.main` (declared in `main.tf`) failed with "reference to undeclared resource". HCL is project-scoped: save and plan-preview now validate the concatenated project configuration, and the per-file view reports syntax issues only. Regression test: `TestTFlearnProjectEditorFlow` (cross-file save succeeds; genuinely undeclared `var.missing` still 400s).

### Honest limitations / still open

- Container execution is **unverified in this sandbox** (no Docker): the success path is implemented via M7 and unit-tested with the M7 stub runner, but no real `terraform plan/apply` ran here. In lite mode execution is unavailable by design (no Docker).
- No Terraform Cloud / remote backend / module registry; no language-server autocomplete; the local formatter is indentation + spacing, not `terraform fmt`.
- Multi-file projects are concatenated into one document for container execution (the M7 worker writes the payload as the lab's `main.tf`); local validation/format keep working per file.
- State refreshes from the linked lab's latest succeeded **plan**; after an apply, re-plan to refresh recorded state.
- The plan preview is a LOCAL static diff and approving it is a learning record, not a container apply gate — the apply gate is the M7 approved plan + confirmation.

## 15. Phase 6 — Advanced services (delivered 2026-11-22)

Priority order (binding): Analytics, ML/AI, Migration/data transfer, Security (remaining §6 E), Advanced networking, Management/Governance (remaining §6 J).

### What was built

| Piece | Where | Notes |
|---|---|---|
| Phase 6 store + migration | `backend/migrations/012_phase6.sql`, `backend/internal/store/phase3.go` | 37 tables `p6_*` (same generic shape as 009–011: `id TEXT PK, name TEXT, data JSONB, created_at, updated_at`), added to the `P3Collections` allowlist; unique-name collections for queries, crawlers, streams, dashboards, notebooks/jobs/models/endpoints, DMS/DataSync tasks, Config rules, WAF ACLs, KMS keys, secrets, peerings, transit gateways, VPC endpoints, flow-log groups, budgets, org units. Verified on the live Postgres after the API's startup migration run. Transfer Family has no table (UNSUPPORTED); Migration Hub has no table (aggregates DMS + DataSync). |
| Phase 6 logic package | `backend/internal/phase6/phase6.go`, `phase6_test.go` | `ValidateAthenaSQL` (single read-only SELECT/WITH only); `ValidateIdentifier`; SageMaker training — `BuiltinDatasets` (`housing`: size_sqft/rooms → price_usd, `study`: study_hours/sleep_hours → exam_score, 10 rows each), `TrainLinear` (ordinary least squares via normal equations/Gaussian elimination, returns real MSE/RMSE/R², errors on <3 rows, non-finite values, singular/collinear matrices instead of fabricated accuracy); `BedrockTemplateResponse` (labelled SIMULATED template, echoes the prompt truncated to 200 chars, states no model ran); `KinesisShard` (SHA-256 partition-key hash mod shard count); WAF evaluation (first match wins); Network Firewall evaluation (priority order, default deny); envelope crypto (`MasterKey`, `WrapKey`/`UnwrapKey`, `EncryptValue`/`DecryptValue` — AES-256-GCM, random nonce); cost model (`DefaultPricing`, `ComputeCost` — only prices non-zero real usage). |
| Analytics (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/AnalyticsConsoles.tsx` (`/services/athena`, `/services/glue`, `/services/kinesis`, `/services/firehose`, `/services/quicksight`) | Athena: saved queries + ad-hoc execute (`POST /aws/athena/execute`, added for the query editor), read-only, limit 100 default / 1000 max, 30 s timeout, runs against platform Postgres or an RDS instance DB, executions recorded (SUCCEEDED/FAILED, row count, duration). Fidelity PARTIAL, engine note "PostgreSQL SQL, not Presto/Athena". Glue: crawlers (postgres/dynamodb/s3), run crawls Postgres `information_schema`, DynamoDB key attributes, S3 via MinIO; per-source status, run PARTIAL if any source fails. Kinesis: streams (1–8 shards), PutRecord (1 MiB cap, shard + sequence), GetRecords (sequence order, `after_sequence`, shard filter, limit ≤1000). Firehose: streams to S3 (MinIO) or Postgres (`firehose_<name>` table, JSONB rows); a real delivery record per put, FAILED delivery returns 502, counters only on DELIVERED. QuickSight: dashboards (bar/line) linked to a saved Athena query; the chart is rendered from the live query results (≤100 points); `chart_error` when there is no query / the query is gone / the DB errors. No sample data anywhere. |
| ML/AI (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/MlConsoles.tsx` (`/services/sagemaker`, `/services/bedrock`) | SageMaker notebooks: create/list/get/delete (python3/nodejs), cell execution via `phase3.RunLambda` (sandboxed subprocess, 30 s, 256 MB), last 20 executions kept with real output/error/status/duration/sandbox. Training jobs: builtin `housing`/`study` or `custom` rows, async run, COMPLETED with real MSE/RMSE/R²/weights/intercept or FAILED with the real error; registers a versioned model and writes the artifact JSON to `<workdir>/sagemaker/models/<jobID>.json`. Models: list/get/delete (refuses delete while an IN_SERVICE endpoint serves it; removes the artifact file). Endpoints: create requires a READY model + artifact file (else 409/503), invoke reads the artifact off disk, validates feature count (400 on mismatch), returns the real prediction + model/R². No sklearn in this sandbox, so training is pure-Go OLS linear regression — labelled as such; never a fake "accuracy 98%". Bedrock: 4 concept models (all `supported: false`), invoke returns the SIMULATED template response and records the invocation; unknown model → 400. No paid third-party AI API is called anywhere. |
| Migration (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/MigrationConsoles.tsx` (`/services/dms`, `/services/datasync`, `/services/transfer`, `/services/migrationhub`) | DMS: tasks platform↔RDS (same DB refused 400, unknown RDS 404, tables validated as identifiers), async row-by-row copy (≤5000 rows/table, batches of 100), target table created from source `information_schema` column types, real row counts + per-table logs, FAILED with the real error. Full-load only, no CDC. DataSync: tasks `s3:<bucket>[/prefix]` or `local:<path>` (local paths confined to the workdir root, `..` rejected outright); local↔local, S3↔S3, mixed; SHA-256 verification per file, real files/bytes/verified counts, FAILED on mismatch/error. Transfer Family: `GET /aws/transfer/info` returns UNSUPPORTED with the decision/reason, the binaries checked (`sshd`, `sftp-server` — neither present), and DataSync as the alternative. Migration Hub: `GET /aws/migrationhub/tasks` aggregates the real DMS + DataSync tasks with a status summary; creates nothing. |
| Security (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/SecurityConsoles.tsx` (`/services/guardduty`, `/services/securityhub`, `/services/inspector`, `/services/config`, `/services/waf`, `/services/kms`, `/services/secretsmanager`) | GuardDuty: detectors + scan over the real audit log (`auth.login_failed`, last 24 h, limit 1000), grouped by client IP; ≥5 failures → brute-force MEDIUM, ≥5 + ≥3 distinct emails → credential stuffing HIGH; dedupes open findings; a scan with no evidence creates 0. Findings status workflow (NEW/ACKNOWLEDGED/RESOLVED/SUPPRESSED). Security Hub: summary + findings list aggregating the shared `findings` table only (GuardDuty/Inspector/Config/Advisor). Inspector: async assessments against real state — open SGs (0.0.0.0/0, HIGH for 22/3389 else MEDIUM), public S3 bucket policies, unencrypted lab instances; S3 check skipped + recorded when MinIO is down. Config: rules (`s3-bucket-versioning-enabled`, `sg-no-open-ssh`, `sg-no-open-ingress`), evaluate stores one result row per non-compliant resource + one finding each; snapshots record current lab resources as config items. WAF: ACLs with validated rules, `/evaluate` runs the real rule evaluation against a supplied request and increments the evaluation count; fidelity SIMULATED with the note "Evaluation only — does not block live traffic". KMS: durable envelope encryption (replaces the in-memory simulator) — create/list (wrapped key never returned), encrypt/decrypt (decrypt tries previous versions after rotation), rotate (version+1, previous wrapped keys kept), delete (409 if a secret uses the key); master key from `CLOUDFORGE_KMS_MASTER_KEY` else the JWT secret. Secrets Manager: create (fresh data key encrypts the value, data key wrapped by the KMS key, version 1), list (no ciphertext/wrapped key), get (decrypts), put value (new version), delete. No automatic rotation. |
| Advanced networking (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/NetworkingConsoles.tsx` (`/services/vpcpeering`, `/services/transitgateway`, `/services/vpcendpoints`, `/services/networkfirewall`, `/services/flowlogs`) | VPC Peering: both endpoints must be lab VPC resources, same VPC refused, overlapping CIDRs refused (400); on success writes real route entries (`pcx-<id>`) into both VPCs' `config.routes`; delete removes them. Fidelity SIMULATED (no engine peering RPC). Transit Gateway: create/list/delete (409 while attached), attach writes `0.0.0.0/0 → tgw-<id>` + increments the count, detach removes it. VPC Endpoints: s3/dynamodb/sqs/sns only, VPC kind check, endpoint recorded in the VPC config, DNS name `<service>.cf-local-1.vpce.local` (teaching name, not registered). Network Firewall: rules validated, best-effort engine sync via `CreateFirewallPolicy` when a lab is attached — `engine_policy_applied` is true only on success, else false + reason; `/evaluate` runs the real 5-tuple evaluation. Flow Logs: groups (VPC kind check), `/capture` calls engine `CollectTelemetry` and writes one record per real telemetry point (`engine-telemetry`); engine down → honest 503, zero records. |
| Management/Governance (API) | `backend/internal/httpapi/phase6.go`, `frontend/src/pages/ManagementConsoles.tsx` (`/services/ssm`, `/services/trustedadvisor`, `/services/costexplorer`, `/services/organizations`) | SSM: inventory via host-agent `Inventory()` (503 without a host agent), instances = real labs + instance resources, run command via allowlisted host-agent `OpExecInLab` (503 without an agent, 502 on agent error), real output/state recorded. Trusted Advisor: run = 5 checks on real state (idle labs READY >24 h with no resources, unattached SGs, open SGs, quota usage vs role MaxLabs, S3 buckets without versioning); one Advisor finding per match. Cost Explorer: `p6CurrentCost` from real usage — lab hours (capped 720 h, Micro except `three-tier-app` → Small), S3 bytes (MinIO unreachable → priced 0 + note), SQS messages, Lambda invocations, RDS/cache hours (capped 720 h, owner-filtered); currency "USD (educational estimates — not an AWS bill)". Budgets: create/list/get/delete; `current_cost_usd`, `alert` (total ≥ limit), `usage_percent` evaluated on every read; a zero/negative limit is refused (400, "a positive limit_usd is required"). Organizations: units (parent validated), members (user must exist, duplicate → 409), delete refused (409) while child units exist; list resolves member IDs to real user emails. |
| Registry + catalog | `backend/internal/providers/providers.go`, `backend/internal/httpapi/services.go` | Capabilities added for athena, glue, kinesis, firehose, quicksight, sagemaker, bedrock (SIMULATED), dms, datasync, transfer (UNSUPPORTED), guardduty, securityhub, inspector, config, waf, kms, secretsmanager, vpc peering/endpoint/flow-logs, transitgateway, networkfirewall, ssm, trustedadvisor, costexplorer, budgets, organizations. KMS updated from `kms-simulator`/SIMULATED to `cloudforge-kms`/PARTIAL (durable keys). Catalog rows updated for kms, secretsmanager, waf, guardduty, config, organizations, ssm, costexplorer, budgets, trustedadvisor, athena, glue, kinesis, quicksight, sagemaker, bedrock, dms, datasync, transfer; new rows for firehose, securityhub, inspector, networkfirewall, transitgateway, vpcpeering, vpcendpoints, flowlogs, migrationhub. `httpRouteFor` maps all Phase 6 ops to real routes. KMS is no longer registry-only (see the reconciliation note in `docs/SERVICE_CATALOG.md`). |
| Consoles (frontend) | `frontend/src/pages/{Analytics,Ml,Migration,Security,Networking,Management}Consoles.tsx`, `frontend/src/App.tsx`, `frontend/src/pages/phase6-consoles.test.tsx` | 27 consoles (Athena, Glue, Kinesis, Firehose, QuickSight, SageMaker, Bedrock, DMS, DataSync, Transfer, Migration Hub, GuardDuty, Security Hub, Inspector, Config, WAF, KMS, Secrets Manager, VPC Peering, Transit Gateway, VPC Endpoints, Network Firewall, Flow Logs, SSM, Trusted Advisor, Cost Explorer, Organizations) wired into `App.tsx` before the generic `services/:id` route. Each mirrors its AWS console's layout/tables/detail tabs/wizards/action names in CloudForge branding, with fidelity badges and honest not-built notes (Bedrock SIMULATED banner on every invoke; QuickSight charts only from live query results; Transfer shows the UNSUPPORTED decision + checked binaries; SSM states the 503 without a host agent). Render tests for all 27 consoles. Athena ad-hoc execute needed a new backend route (`POST /aws/athena/execute`). |
| Login audit (bug fix, found live) | `backend/internal/httpapi/httpapi.go`, `backend/internal/store/store_integration_test.go` | Failed logins are audited with actor `""` (stored as NULL), not the literal `"anonymous"`: `audit_logs.user_id` is a UUID column, so every failed-login insert failed with `invalid input syntax for type uuid: "anonymous"` and GuardDuty scans saw zero evidence. Regression test in `TestPostgresIntegration` (empty actor records + is listable by action; a non-UUID actor still fails, pinning the constraint). |

### Verification (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean; `go test ./...` all packages pass (23 packages with tests, incl. new `phase6` package tests and 11 `TestPhase6*` httpapi tests); `DATABASE_URL` set so the Postgres integration tests run (incl. the new anonymous-actor audit regression); `cargo test --workspace` 74 passed (Rust untouched); frontend build + vitest 136/136 (was 109/109; +27 Phase 6 console render tests) + oxlint 0 warnings; `make verify` exit 0.
- Live lite walk against a rebuilt API (:8081) + worker (:8083) + engine (:50051 simulated) + Postgres 16 + Redis 7.0.15 + frontend dev :5173 / Vite preview :8080: **walk script `/tmp/cf-live/p6/walk-phase6.sh`, 174/174 checks pass, 0 fail** (transcript `/tmp/cf-live/p6/walk.log`) — health (`lite: true`, `database_connected: true`, `redis_connected: true`); unauthenticated Phase 6 call → 401; lab `ec2-basics` → READY; Athena save/execute/history + DELETE and stacked-statement rejections (400), duplicate name 409; Glue Postgres crawl SUCCEEDED with 114 tables found, S3 crawl PARTIAL (MinIO down), unknown source 400; Kinesis put/get in sequence order + `after_sequence`, shard_count 9 → 400; Firehose Postgres delivery DELIVERED, S3 delivery honestly 502 FAILED (MinIO down); QuickSight dashboard chart from the live query (`a=5, b=3`), no-query dashboard `chart_error`; SageMaker notebook cell `print(2+2)` → `4`, housing training COMPLETED R²=0.9997 RMSE=1742.58, endpoint prediction 308580.18 for 1500 sqft/3br, wrong feature count 400, degenerate custom job FAILED with the real error; Bedrock invoke labelled SIMULATED, unknown model 400; DMS same-DB 400, unknown RDS 404, real platform→RDS copy rows_copied=3 (verified with psql in the target DB: 3 rows); DataSync local copy 2 files / 2 SHA-256 verified + content diff clean, `..` escape 400; Transfer UNSUPPORTED with checked binaries; Migration Hub aggregates DMS + DataSync; GuardDuty empty scan 0 findings, then 6 failed logins → credential-stuffing HIGH finding (20 failed logins / 4 distinct accounts from ::1 across the walk runs — the shared audit log escalated the rule), rescan dedupes 0, Security Hub open_findings=1, resolve works; Inspector lab assessment COMPLETED (open-SG HIGH + unattached-SG findings once the SG rule existed; S3 check skipped honestly when MinIO is down); Config rule evaluate non-compliant on the open-SSH SG, snapshot recorded 5 items; WAF `/admin` → block, `/home` → allow; KMS encrypt/decrypt round-trip, wrong-key decrypt 400, rotate to v2 (old ciphertext still decrypts), delete in-use key 409; Secrets value round-trip + version bump; peering A–B created with real route entries in both VPC configs (same-VPC and overlapping-CIDR peerings 400), routes removed on delete; TGW attach/detach, delete refused 409 while attached; VPC endpoint `s3.cf-local-1.vpce.local`, bad service 400; Network Firewall packet 443 → allow, 22 → deny (default); Flow Logs capture wrote 6 records, all `engine-telemetry`; SSM inventory/run command honestly 503 without a host agent; Trusted Advisor run created real findings (open SG HIGH, unattached SG LOW, quota MEDIUM); Cost Explorer total $0.00016 (lab + RDS hours, S3 priced 0 with the MinIO note), budget alert=false at $100000, alert=true under the real total, zero limit 400; Organizations member add (unknown user 404, duplicate 409), parent delete refused 409 with a child; registry `/services/firehose`, `/services/securityhub`, `/services/transitgateway`, `/services/migrationhub` all 200; frontend consoles 200 on dev + preview; cleanup deleted every created resource and destroyed the lab (DESTROYED).
- The walk took 6 runs to go green (174/174 on run 6). The first runs found 3 real product bugs (login audit, SG sync, Flow Logs meta) and 1 real field-naming bug (KMS `version`), all fixed with regression coverage where a unit/integration test can cover them; the remaining early failures were walk-script bugs (lab create is 202 with the id under `lab.id`; response field names: Glue run status is `SUCCEEDED` not `COMPLETED`, Bedrock returns `output` not `response`, Security Hub summary is `open_findings` not `open`, Config evaluate returns `non_compliant` list not `non_compliant_count`, Advisor list returns `findings` not `results`, budget `alert` is evaluated on GET not create, KMS rotate stores `key_version`; training-job names are globally unique and jobs have no delete route, so the walk now uses per-run names; the GuardDuty audit log is global across runs, so the finding escalates from brute-force to credential stuffing as distinct walk users accumulate).

### Bugs found live and fixed (not just UI)

1. **Failed-login audit silently dropped (GuardDuty blind).** `handleLogin` audited failures with actor `"anonymous"`; `PostgresAuditStore.Record` casts the actor to UUID (`NULLIF($2,'')::uuid`), so every insert failed with `invalid input syntax for type uuid: "anonymous"` (visible only as a WARN in the API log) and GuardDuty scans returned `failed_logins_scanned: 0` after 6 real failed logins. The in-memory audit store used by the unit tests has no UUID cast, so `TestPhase6WAFAndGuardDuty` passed while the live path was broken. Fixed: audit with actor `""` (stored as NULL) + comment explaining why; regression test in `TestPostgresIntegration` (empty actor records and is listable by action; non-UUID actor still fails).
2. **SG rule add 502'd when the lab had no subnet.** `syncSGToEngine` called engine `CreateFirewallPolicy` with `NetworkId: labID`, but the engine network is created lazily by subnet creation only — a lab with VPCs/SGs but no subnet has no engine network, so the first SG rule failed `NOT_FOUND: network <lab-id> not found`. Fixed with the same retry-once-after-`ensureEngineNetwork` pattern subnet creation already uses; the Inspector/Config/Advisor checks that depend on SG rules now see real rules.
3. **Flow Log capture always 503'd.** `handleFlowLogCapture` sent an empty `CollectTelemetryRequest`; the engine requires `RequestMeta` on every RPC (`INVALID_ARGUMENT: meta is required`). Fixed: resolve the lab from the group's VPC resource and pass `Meta(labID, userID, requestID, "flowlog-capture:<id>:<seq>")` with a fresh idempotency key per capture.
4. **Flow Log capture read the wrong field.** The first fix read `vpc_resource_id` from the group data; the group stores the VPC id as `vpc_id` (same convention as peerings/endpoints), so capture 404'd. Fixed to `vpc_id`.
5. **KMS key version invisible to the console.** The backend stores `key_version`; the console (and the walk) read `version`, so the Version column was blank and rotate asserted on a missing field. Fixed: `version` is now stored alongside `key_version` at create and rotate (alias); the unit test asserts both fields are 2 after rotation.
6. **`pkill -f` self-match (walk/script, not product).** Restarting the API with `pkill -f '/tmp/cf-api-p6'` matched the exec session's own command line and SIGTERM'd it (same class as Phase 4 bug 16). Restarts now kill by PID.

### Honest limitations / still open

- No Docker, KVM, or `/dev/kvm` in this sandbox: real VMs, containers, real netlink, Terraform containers and nginx config syntax remain unverified here, as in Phases 1–5. The Rust engine default backend is SIMULATED; its real rtnetlink/nftables backend compiles but is unverified.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns 410) — S3 object flows are unit-tested + live-503 only, as in Phase 2; Firehose S3 delivery and the Inspector S3 check therefore fail/skip honestly here (verified live: Firehose S3 put → 502 FAILED delivery, Inspector `s3_check: skipped: s3 backend unavailable`).
- **No sklearn in this sandbox** — SageMaker training is pure-Go OLS linear regression on tiny built-in datasets, not scikit-learn; labelled as such in code, console and docs. No other ML library was installed.
- **No sshd/sftp-server** — Transfer Family is UNSUPPORTED by decision (the console shows the checked binaries).
- **No engine peering/Transit Gateway RPC** — peering/TGW are SIMULATED with real route-table entries in lab state, not kernel peering.
- DMS is full-load only (≤5000 rows/table), no CDC; DataSync local paths are confined to the workdir; WAF rules are evaluated only via `/evaluate`, not inline in the API request path; Flow Logs are engine-telemetry snapshots, not per-packet flows; KMS/Secrets are durable local envelope encryption, not AWS KMS/HSM; Cost Explorer is educational estimates, never an AWS bill; Bedrock is a SIMULATED template playground, no real model, no paid third-party API.
- Training jobs have no delete route (jobs are immutable history; models/endpoints/notebooks can be deleted) and their names are globally unique, so repeated walk runs must use per-run names. GuardDuty detectors and Inspector assessments likewise have no delete route.
- Config evaluates 3 rule types only; the SG rules are evaluated from the lab state, not from live engine enforcement. The S3 versioning rule needs MinIO to evaluate.
- SSM needs a host agent (honest 503 without one); Trusted Advisor runs 5 checks only.
- **Test-ordering fragility (pre-existing, found during Phase 6 verification):** the hostagent disk-capacity check enforces 16 GiB against the lab volume path (`/var/lib/cloudforge/labs`) when it exists, but skips when it does not exist (`DiskFreeGB == 0`). This sandbox's overlay is 7.5 GiB, so `make verify` fails `TestLibvirtUnavailableInSandbox` / `TestInstanceCreateStartsVMAndRebootWorks` once that directory exists (the Phase 6 walk created it via DataSync sources + SageMaker artifacts). The walk cleanup now removes the tree and `make verify` passes again. On a real host with ≥16 GiB free this does not trigger; on a small overlay it does. No Phase 6 code changed the check.
- Frontend on `:8080` was verified with Vite preview of the production build, not the Docker nginx image (no Docker here).
- Region scoping is display-only (all resources are `cf-local-1`).

## 16. Phase 7 — Production hardening (delivered 2026-11-22)

The final phase. Worked through the Phase 0 audit §5 risk checklist
and master prompt §15/§21. No working feature was broken; every
hardening change kept the existing suites green and the lite stack
working end-to-end.

### What was built

| Piece | Where | Notes |
|---|---|---|
| Engine auth (R1) | `rust-network-engine/crates/grpc-server/src/lib.rs`, `src/main.rs`; `backend/internal/networkclient/client.go` | Shared-secret token on gRPC metadata `x-cloudforge-engine-token`, enforced on every RPC including Health by `EngineAuthInterceptor` (constant-time compare); old unauthenticated path refuses connections. Engine fails closed without a token unless `CLOUDFORGE_DEV=1`. Go client: `NewWithToken` + `withMeta`. `:50051` no longer published in either compose file. **mTLS was not built** — documented next step. |
| JWT revocation (R3) | `backend/internal/auth/revocation.go`, `redis_revocation.go`; `backend/internal/store/revocation.go`; `backend/migrations/013_revocation.sql` | `jti` on every token (`NewJTI`); Revoker interface with memory/Redis/Postgres backends; `ValidateToken` checks the denylist on every authenticated request; logout revokes the caller's `jti`; admin revokes all of a user's recorded sessions. Dual-verify (bcrypt-era users) untouched. |
| Fail-closed secrets (R4) | `backend/internal/config/config.go` (`ValidateSecrets`); `backend/cmd/api/main.go`, `backend/cmd/worker/main.go` | Non-dev mode: API/worker refuse to start when a production secret is unset or still a known dev default, naming the variable. Dev defaults require the explicit `CLOUDFORGE_DEV=1` opt-in. New env vars: `CLOUDFORGE_DEV`, `CLOUDFORGE_ENGINE_TOKEN`, `CLOUDFORGE_METRICS_TOKEN`, `CLOUDFORGE_TLS`, `CLOUDFORGE_TRUSTED_PROXIES`, `CLOUDFORGE_API_USER_RATE_LIMIT/_WINDOW`, `CLOUDFORGE_AUDIT_EXPORT_RETENTION`, `CLOUDFORGE_TF_NETWORK_MODE`. |
| `/metrics` restriction (R5) | `backend/internal/httpapi/security_p7.go` (`handleMetrics`); `frontend/nginx.conf`; `infrastructure/monitoring/prometheus.yml` | Token auth (`X-Metrics-Token` or `Authorization: Bearer`); loopback-only when no token is configured (dev). nginx returns 404 for `/metrics`. Prometheus scrapes with `authorization: credentials: ${CLOUDFORGE_METRICS_TOKEN}`. |
| Sessions / CSP (R6) | `backend/internal/httpapi/security_p7.go`, `httpapi.go`; `frontend/src/services/api.ts`, `AwsHeader.tsx`, `Account.tsx` | `HttpOnly`, `SameSite=Lax` cookie `cf_session` (`Secure` only when `CLOUDFORGE_TLS=1`); `Authorization` header path kept working for API clients/tests during transition. CSP + `X-Content-Type-Options` + `X-Frame-Options: DENY` + `Referrer-Policy` + `Permissions-Policy` in the API and nginx; HSTS only when TLS is on. `logout()` now POSTs `/auth/logout` (best-effort, still clears the local token on failure). |
| Trusted proxy / per-user limits (R7, R8) | `backend/internal/httpapi/security_p7.go` (`trustedPeer`, `Server.clientIP`, `withUserAPIRateLimit`) | XFF honoured only from loopback, private (RFC 1918/ULA), or explicitly configured trusted proxies; otherwise the peer IP is used. Per-user limiter (default 300/min) in addition to per-IP; unauthenticated requests skip it; fail-open on Redis error like the general limiter. |
| Audit export (R10) | `backend/internal/httpapi/security_p7.go` (export jobs); `backend/internal/jobs/audit_export.go`, `jobs.go`; `frontend/src/pages/CloudTrailConsole.tsx` | Export jobs: create/list/download (CSV/JSON, cap 10,000 events), files under `<workdir>/audit-exports/` with `.meta.json` sidecar, retention newest N (default 7). Worker 90-day cleanup exports to `audit-archive-<date>.csv` **before** deleting; a failed export blocks the delete. CloudTrail console has an "Export jobs" card with create + download. |
| Terraform isolation (R11) | `backend/internal/terraform/terraform.go` (`NetworkMode`, `WithNetworkMode`, `tfHostConfig`) | `CLOUDFORGE_TF_NETWORK_MODE=isolated` → container `--network none` (no ExtraHosts, no egress); default `bridge` keeps the emulator route. |
| TLS option (R12) | `scripts/gen-tls-cert.sh`; `deployments/compose/docker-compose.tls.yml`; `frontend/nginx.tls.conf` | Opt-in only. Self-signed cert generator (verified with openssl; refuses to overwrite). Override replaces the frontend nginx config: host `:443` → container `:8443`, `:8080` redirects to HTTPS. `CLOUDFORGE_TLS=1` enables API HSTS + Secure cookies. |
| Full-mode resource limits | `deployments/compose/docker-compose.yml` | Added (full mode previously had none): API 512M/1.0 CPU, worker 256M/0.50, engine 256M/0.50, frontend 256M/0.50, Postgres 1G/1.0, Redis 256M/0.50. Lite limits unchanged (1,280 MiB total). |
| Docs (§20) | `docs/ARCHITECTURE.md`, `docs/SECURITY.md`, `docs/DEPLOYMENT.md`, `docs/TESTING.md`, `docs/KNOWN_LIMITATIONS.md`, `docs/ROADMAP.md` (new); `docs/security/model.md`, `docs/api/reference.md`, `docs/TERRAFORM_LEARNING.md`, this file, `MILESTONE_LOG.md` (updated) | Every §20 tracking doc now exists and is current for the final state. |

### Verification (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean; `cargo fmt --all --check` clean (one diff
  fixed during the run); `cargo clippy` clean except pre-existing
  workspace warnings.
- `go test ./...` (all 23 packages, Postgres + Redis live): all pass.
- `cargo test --workspace`: **75 passed, 0 failed** (74 + 1 new
  `grpc_auth` test).
- Frontend: build passes, vitest **138/138** (was 136/136), oxlint
  0 warnings, 0 errors.
- `make verify`: **exit 0**.
- `tests/e2e`: `TestEndToEnd` **PASS** (14.8 s), exercising the
  token-authenticated fake engine path.
- Live lite walk (`/tmp/cf-live/p7/walk-phase7.sh`, engine `:50051`
  + API `:8081` + worker `:8083` + frontend preview `:8080`,
  real Postgres 16 + Redis 7, `CLOUDFORGE_DEV=1`): **FAIL=0** —
  every hardening item with real curl responses (fail-closed API +
  engine startup; engine gRPC up with token, down with a wrong
  token; `/metrics` 401 without/wrong token, 200 with token;
  CSP/XCTO/XFO present, no HSTS over plain HTTP; `cf_session`
  cookie set + cookie-only auth; per-user limit 429 on the 6th
  request with `Retry-After`, other user unaffected; spoofed-XFF
  auth limit 429 on the 11th bad login; logout revokes → 401;
  admin session revoke → 401, admin token still works; lab
  `ec2-basics` → READY in ~3 s → DESTROYED; one flow per phase
  — IAM evaluate, SQS send/receive, ECR create, Terraform
  Learning courses, Athena execute, GuardDuty detector, Cost
  Explorer; audit export job create/list/download with real rows;
  frontend `/`, `/services`, `/services/cloudtrail` 200).
- Backup/restore round-trip re-verified: `scripts/backup.sh`
  wrote a 248 KB dump; `scripts/restore.sh --force` into a scratch
  database restored 3 users, 26 audit rows, 3 revoked tokens,
  13 migrations — matching the live DB; scratch DB dropped.

### Bugs found live and fixed (not just UI)

- **Walk harness: engine `NETWORK_ENGINE_ADDR` must be an IP
  literal.** The walk exported `NETWORK_ENGINE_ADDR=localhost:50051`
  for the Go clients, which leaked into the engine's environment;
  Rust `SocketAddr` parsing rejects hostnames, so the engine
  refused to start ("invalid NETWORK_ENGINE_ADDR"). Fixed in the
  walk (engine gets `0.0.0.0:50051`); documented in
  `docs/KNOWN_LIMITATIONS.md`. No product code changed.
- **`-seed-curriculum` is fatal without a curriculum dir.**
  The walk ran from `/home/hatch`, so the API exited silently
  after "curriculum directory could not be resolved". This is by
  design (the flag explicitly demands the curriculum); the walk
  now sets `CURRICULUM_DIR` explicitly. Documented in
  `docs/DEPLOYMENT.md`.
- **Per-user rate-limit accounting:** the first walk run counted
  5 GETs against a limit of 5 but forgot the cookie `/auth/me`
  call had already consumed 1 of the budget — the 5th GET 429'd
  correctly. Test expectation fixed, not the limiter.

### Honest limitations / still open

- **mTLS not built** (token auth instead — documented next step,
  `docs/ROADMAP.md`).
- **ECR `registry:2` still has no auth** (local-only).
- **Terraform bridge mode still approximates isolation** (isolated
  mode is the real one); no network-namespace + emulator-allowlist
  mode, no HCL fuzzing.
- **HSTS/TLS compose override unverified in sandbox** (no
  Docker/nginx); cert generation verified with openssl only.
- **nginx config syntax unverified** (no nginx binary).
- **Full-mode compose limits added but never run** (no Docker).
- **Pre-Phase-7 tokens cannot be individually revoked** (no `jti`).
- **Header auth still works during the cookie transition**;
  making it API-client-only is a follow-up.
- **Audit cleanup without an export directory still deletes**
  (warning logged).
- R2's loud startup check (no worker in Postgres mode), R13
  (Postgres-required startup in compose), and R14 (SSRF grep gate)
  were not built in Phase 7 — see `docs/ROADMAP.md`.
- The sandbox VM was replaced mid-Phase-7 (the walk's first two
  runs were lost with `/tmp`, and Postgres/Redis had to be
  reinstalled from apt: Postgres 16 + Redis 7.0.15, fresh empty
  database, migrated on API startup). All test/live results above
  are from the rebuilt environment, recorded after the rebuild.
- Frontend on `:8080` was verified with Vite preview of the
  production build, not the Docker nginx image (no Docker here).
- Region scoping is display-only (all resources are `cf-local-1`).
