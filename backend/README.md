# CloudForge Backend — Go Control Plane (Milestones 4–9)

Go 1.26 control plane with PostgreSQL persistence (pgx v5), a durable
job queue, Argon2id passwords + HS256 JWT sessions (dual-verify
migration for legacy bcrypt hashes), roles, a typed, allowlisted host
agent (simulated / Docker / libvirt) for VM and container provisioning,
a generated gRPC client for the Rust network engine, AWS provider
integration (MinIO S3, DynamoDB Local, IAM evaluation, educational
STS/KMS/SQS/SNS/EventBridge simulators, LocalStack/Floci emulator
probe), the Milestone 6 lab resources / live topology / validation /
terminal surface, Milestone 7 Terraform orchestration (isolated
container execution, plan/approve/apply/destroy), Milestone 8
curriculum, and Milestone 9 production readiness (chi routing,
Prometheus `/metrics` + honest `/readyz`, Redis-backed rate limiting,
root `Makefile`, same-origin nginx proxy).

## Run

```bash
export PATH=/usr/local/go/bin:$PATH   # if go is not on PATH yet
cd backend
go build ./...
go test ./...

# API server (default :8080) — connects to Postgres if reachable,
# otherwise runs with in-memory stores and logs a warning.
PORT=8080 NETWORK_ENGINE_ADDR=localhost:50051 CLOUDFORGE_JWT_SECRET=change-me \
  go run ./cmd/api

# Seed the curriculum even when CURRICULUM_SEED=0 (default is on):
go run ./cmd/api -seed-curriculum

# Re-seed a running API (admin only):
#   POST /api/v1/admin/seed-curriculum

# Background worker (default :8081)
go run ./cmd/worker

# Host agent (default :8082, simulated mode; see "Host agent" below)
HOST_AGENT_TOKEN=change-me go run ./cmd/host-agent
```

Or build the binaries:

```bash
go build -o /tmp/cloudforge-api ./cmd/api
go build -o /tmp/cloudforge-worker ./cmd/worker
go build -o /tmp/cloudforge-host-agent ./cmd/host-agent
```

## PostgreSQL + Redis locally

Without Docker, install the system packages (Ubuntu 24.04):

```bash
sudo apt-get install -y postgresql redis-server
sudo service postgresql start
sudo -u postgres psql -c "CREATE USER cloudforge WITH PASSWORD 'cloudforge';"
sudo -u postgres psql -c "CREATE DATABASE cloudforge OWNER cloudforge;"
# Then start the API/worker with the default DATABASE_URL — migrations
# 001–004 run automatically on startup.
```

With Docker (see `deployments/compose/`):

```bash
cd ../deployments/compose && docker compose up -d postgres redis
# DATABASE_URL=postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable
```

Integration tests (skipped without a reachable database):

```bash
DATABASE_URL=postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable \
  go test ./internal/store/ -run TestPostgresIntegration -v -count=1
```

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | API listen port (ignored if `CLOUDFORGE_ADDR` is set) |
| `CLOUDFORGE_ADDR` | `:8080` | Full API listen address |
| `DATABASE_URL` | `postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable` | Postgres DSN; invalid values fail startup fast |
| `REDIS_ADDR` | `localhost:6379` | Redis address; unreachable Redis only disables the cache (never fails the API) — except auth rate limiting, which is fail-closed (see Milestone 9). In compose this is `redis:6379`; the old `REDIS_URL` name was never read. |
| `NETWORK_ENGINE_ADDR` | `localhost:50051` | Rust network engine address; `/api/v1/health` reports `network_engine_grpc` (real gRPC Health RPC) and `network_engine_reachable` (TCP listener probe, labelled as such) |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO S3 endpoint (host:port) for the S3 adapter |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | `minioadmin` / `minioadmin` | MinIO credentials (dev defaults; the secret is never logged) |
| `MINIO_USE_SSL` | `false` | Use https for MinIO |
| `DYNAMODB_ENDPOINT` | `http://localhost:8000` | DynamoDB Local / emulator URL |
| `AWS_EMULATOR_ENDPOINT` | `http://localhost:4566` | LocalStack/Floci emulator URL (health-probed; unreachable → emulator ops `unavailable`) |
| `CLOUDFORGE_JWT_SECRET` | `dev-only-change-me` | HMAC secret for the HS256 JWT sessions. **Set this in any shared deployment** — startup logs a warning when the default is used. Rotating it invalidates all sessions. (M9 renamed the compose/`.env.example` name from `JWT_SECRET`, which config never read.) |
| `CLOUDFORGE_TOKEN_TTL` | `12h` | JWT lifetime |
| `CLOUDFORGE_AUTH_RATE_LIMIT` | `10` | Auth (register/login) attempts per window per client IP (M9, fail-closed on Redis error) |
| `CLOUDFORGE_AUTH_RATE_WINDOW` | `5m` | Auth rate-limit window |
| `CLOUDFORGE_API_RATE_LIMIT` | `300` | General `/api/v1` requests per window per client IP (M9, fail-open on Redis error) |
| `CLOUDFORGE_API_RATE_WINDOW` | `1m` | General API rate-limit window |
| `CLOUDFORGE_MODE` | `full` | Mode switch: `lite` \| `full`. Lite = classroom laptop (see `docs/operations/lightweight.md`): caps quotas at 1 lab / Micro / 30 min for learners, simulated host agent, no KVM required. Full is the default and changes nothing. `CLOUDFORGE_LITE=true` is an alias for `MODE=lite`; setting both to different modes fails startup. |
| `CLOUDFORGE_LITE` | _(unset)_ | Alias for `CLOUDFORGE_MODE=lite` (`true`/`false`/`1`/`0`/`yes`/`no`/`on`/`off`). |
| `CLOUDFORGE_ADMIN_EMAIL` | _(unset)_ | The first user registering with this email is promoted to `admin` (bootstrap) |
| `WORKER_ADDR` | `:8081` | Worker listen address |
| `WORKER_POLL_INTERVAL` | `2s` | Jobs poll interval |
| `WORKER_MAX_ATTEMPTS` | `5` | Max job attempts before dead-lettering |
| `HOST_AGENT_MODE` | `simulated` | Host-agent backend: `simulated` \| `docker` \| `libvirt` \| `auto` |
| `HOST_AGENT_ADDR` | `:8082` | Host-agent listen address (host-agent process) and dial address (API/worker). The API/worker only use it when the variable is **explicitly set** in their environment; unset means "no host agent, use the network-engine probe path" |
| `HOST_AGENT_TOKEN` | _(unset)_ | Shared bearer token for the host-agent API. **Required in docker/libvirt mode** (host-agent refuses to start without it); in simulated mode an insecure dev default is used with a warning when unset |
| `TERRAFORM_IMAGE` | `hashicorp/terraform:1.9.8` | Pinned Terraform image for M7 container runs; set to `opentofu/opentofu:1.9.2` to use OpenTofu instead |
| `TERRAFORM_WORKDIR_ROOT` | `/var/lib/cloudforge/labs` | Per-lab Terraform workdirs live under `<root>/<lab-id>/terraform` (the only host mount the Terraform container gets) |
| `CLOUDFORGE_LAB_ROOT` | `/var/lib/cloudforge/labs` | Lab directories root |
| `CURRICULUM_DIR` | _(auto-resolved)_ | Curriculum source directory (the repo's `curriculum/`). When unset the API tries `../curriculum`, `curriculum`, and paths relative to the binary; an explicitly set directory that cannot be resolved fails startup |
| `CURRICULUM_SEED` | `1` (on) | Seed the curriculum at startup. Only an explicit `0`/`false`/`no`/`off` disables it; `-seed-curriculum` forces a seed even when it is off |

Secrets are never logged (`config.String()` redacts the DSN password and
the token secret).

## API (all under `/api/v1` unless noted)

| Method & path | Auth | Notes |
|---|---|---|
| `GET /healthz` | — | Liveness: `{"status":"ok"}` |
| `GET /readyz` | — | **M9.** Honest readiness: live-checks Postgres, Redis and the engine on every call; 200 `ready` or 503 `not_ready` (gated only on Postgres when configured). See `docs/api/reference.md`. |
| `GET /metrics` | — | **M9.** Prometheus scrape endpoint: `cloudforge_http_requests_total{method,route,status}`, `cloudforge_http_request_duration_seconds{method,route}`, `cloudforge_network_engine_up`. |
| `GET /api/v1/health` | — | Service, version, uptime, `network_engine_grpc` (real gRPC Health RPC), `network_engine_reachable` (TCP listener probe only — **not** engine health), `database_connected`, `redis_connected`, plus `host_agent` (`mode`/`reachable`/`detail`) when a host agent is configured |
| `GET /api/v1/aws/{service}/capabilities` | — | Per-service capabilities with freshly probed health; `service` ∈ s3, dynamodb, iam, sts, kms, sqs, sns, eventbridge, ec2, lambda, vpc, eks; unknown service → 404 |
| `POST /api/v1/aws/s3/buckets` | Bearer | `{name}` → 201; MinIO unreachable → 503 with `capability` info |
| `GET /api/v1/aws/s3/buckets` | — | MinIO unreachable → 503 with `capability` info |
| `POST /api/v1/aws/iam/evaluate` | Bearer | `{policies, action, resource, username?}` → `{decision: Allow\|Deny, reason, matched_statement?}` (real evaluation of a documented IAM subset) |
| `POST /api/v1/auth/register` | — | `{email, name, password}` (password ≥ 8 chars, Argon2id); 409 on duplicate email; new users are `learner` unless the bootstrap admin email matches. Rate limited (M9): 10 attempts / 5 min per IP, fail-closed. |
| `POST /api/v1/auth/login` | — | Returns `{token, expires_at, user}` (HS256 JWT, 12h). Legacy bcrypt hashes still verify and are upgraded to Argon2id on success. Rate limited (M9): 10 attempts / 5 min per IP, fail-closed. |
| `GET /api/v1/auth/me` | Bearer | Current user |
| `GET /api/v1/users/me` | Bearer | Alias of `/auth/me` |
| `GET /api/v1/users` | Admin | All users (public views) |
| `GET /api/v1/courses` | — | Course list (Redis-cached 60s when available) |
| `POST /api/v1/courses` | Admin | `{id (slug), title, track, description?, modules?}`; invalidates the course cache |
| `GET /api/v1/courses/{id}` | — | Course detail incl. lesson titles |
| `PUT /api/v1/courses/{id}` | Admin | Update title/track/description |
| `DELETE /api/v1/courses/{id}` | Admin | Deletes the course (cascades modules/lessons) |
| `GET /api/v1/courses/{id}/modules` | — | Modules with lessons (lesson IDs needed for progress) |
| `POST /api/v1/courses/{id}/enroll` | Bearer | Enroll the caller |
| `GET /api/v1/courses/{id}/progress` | Bearer | Completed lessons, XP, enrollment state |
| `POST /api/v1/courses/{id}/progress` | Bearer | `{lesson_id}` — completes a lesson, awards 10 XP (+10 first-lesson achievement bonus); re-completion awards 0; 409 when not enrolled |
| `POST /api/v1/labs` | Bearer | `{definition_id, name?, idempotency_key?}`; 400 unknown definition, **429 when the 3-active-lab quota is hit**. Postgres mode returns **202 Accepted** (worker provisions); in-memory mode returns 201 |
| `GET /api/v1/labs` | Bearer | Own labs only, newest first |
| `GET /api/v1/labs/{id}` | Bearer | Own lab only (other users' labs return 404) |
| `POST /api/v1/labs/{id}/stop` | Bearer | READY/RUNNING → STOPPING → STOPPED; 409 on illegal transition |
| `POST /api/v1/labs/{id}/destroy` | Bearer | → DESTROYING (worker completes → DESTROYED in Postgres mode); 409 on illegal transition |
| `GET /api/v1/labs/{id}/resources` | Bearer | **M6.** Real lab resources from the store (+ host-agent VMs merged in); empty list + `note` when none are recorded — never invented resources |
| `POST /api/v1/labs/{id}/resources` | Bearer | **M6.** `{kind, name, parent_id?, cidr?, profile?, config?}`; kind ∈ vpc\|subnet\|instance\|gateway\|loadbalancer\|route-table\|security-group. Subnet goes through the Rust engine `CreateSubnet` (network auto-created from the lab VPC CIDR when missing), instance through host-agent `CreateVM`; engine/host-agent failure → **502** with the structured engine error |
| `DELETE /api/v1/labs/{id}/resources/{resourceId}` | Bearer | **M6.** Deletes via engine/host-agent + DB row; idempotent (already gone → 200) |
| `GET /api/v1/labs/{id}/topology` | Bearer | **M6.** Live topology: engine `InspectTopology` merged with DB resources; engine unreachable → **503** with capability info (the frontend shows 503 as a real failure, 404/501 as "pending") |
| `POST /api/v1/labs/{id}/validate` | Bearer | **M6.** `{rules?}` (empty body = the lab definition's defaults) → `{lab_id, passed, xp_eligible, checks:[{rule, rule_id, status: pass\|fail\|unknown, passed, detail, message}]}`. Checks inspect real state (DB resources, engine topology, host-agent VMs); unknowable rules are `unknown`, never `pass`. No XP is awarded here (M8) |
| `POST /api/v1/labs/{id}/terminal/token` | Bearer | **M6.** Short-lived (5 min), single-use terminal token `{token, expires_at, redis_backed}` bound to lab+user; stored in Redis when available, in-memory otherwise; only the SHA-256 hash is persisted (`terminal_sessions`) |
| `GET /api/v1/labs/{id}/terminal` | Terminal token (`?token=`, WebSocket) | **M6.** WebSocket shell. Auth is the terminal token query param only — never the Bearer token in a URL. JSON `{type:"resize",cols,rows}` / `{type:"ping"}` handled; anything else is shell input. The shell runs only via host-agent `ExecInLab` (allowlisted commands); simulated mode returns a clearly-labelled simulated shell, never a host shell |
| `POST /api/v1/labs/{id}/terraform/init` | Bearer | **M7.** Enqueues a `terraform_init` job → **202** `{run_id, job_id, action, state:"QUEUED"}` |
| `POST /api/v1/labs/{id}/terraform/validate` | Bearer | **M7.** Enqueues `terraform_validate` → 202 |
| `POST /api/v1/labs/{id}/terraform/plan` | Bearer | **M7.** `{configuration?}` (HCL, ≤ 64 KiB, no amazonaws.com / embedded keys) → enqueues `terraform_plan` → 202; the worker stores the parsed `terraform show -json` plan |
| `POST /api/v1/labs/{id}/terraform/approve` | Bearer | **M7.** `{plan_id}` — explicit approval for a succeeded, unexpired plan; anything else → 409 |
| `POST /api/v1/labs/{id}/terraform/apply` | Bearer | **M7.** `{plan_id}` — requires an approved, unexpired plan (no approval → 409, expired → 409, unknown → 404); enqueues `terraform_apply` → 202; the worker syncs created resources into `lab_resources` |
| `POST /api/v1/labs/{id}/terraform/destroy` | Bearer | **M7.** Enqueues `terraform_destroy` → 202; the worker removes the lab's emulator-managed resources |
| `GET /api/v1/labs/{id}/terraform/runs` | Bearer | **M7.** Run history from `terraform_runs`, newest first |
| `GET /api/v1/curriculum/tracks` | — (progress when authed) | **M8.** Track list; when a Bearer token is supplied each track carries `enrolled` / `progress_percent` / `completed_lessons` |
| `GET /api/v1/curriculum/tracks/{id}` | — (progress when authed) | **M8.** Full track: modules/lessons with theory, key points and diagrams, guided lab + challenge with validation rules, assessment questions **without** `correct_answer` |
| `POST /api/v1/curriculum/tracks/{id}/enroll` | Bearer | **M8.** Enroll the caller in a track |
| `POST /api/v1/curriculum/lessons/{lessonId}/complete` | Bearer | **M8.** `{lessonId}` is the YAML slug or the lesson UUID. Enrolled only (409 otherwise); awards 10 XP (+10 first-lesson bonus, + the track-completion remainder when the last lesson completes — see Milestone 8). Re-completion awards 0 and returns the newly earned achievements |
| `POST /api/v1/curriculum/assessments/{trackId}/submit` | Bearer | **M8.** `{answers: {"0": 1, ...}}` (or `[{"question": 0, "answer": 1}]`). Graded **server-side** against the DB answers; returns score, per-question correct/incorrect + explanation, and awards 20 XP only on the first pass at ≥ 80% |
| `GET /api/v1/users/me/progress` | Bearer | **M8.** Per-track enrollment/percent, `total_xp`, `streak_days` (UTC calendar days) |
| `GET /api/v1/users/me/achievements` | Bearer | **M8.** Earned + locked achievements; locked entries carry `locked_reason` |
| `POST /api/v1/admin/seed-curriculum` | Admin | **M8.** Re-runs the curriculum seed from `CURRICULUM_DIR` (idempotent); returns the seed counts |
| `POST /api/v1/labs/{id}/break-fix/start` | Bearer | **M8.** `{scenario_id}` — injects the scenario fault via the Rust engine `InjectFault`; engine unreachable → the attempt is `fault_pending` (lab created without the fault, honestly reported), never claimed broken |
| `POST /api/v1/labs/{id}/break-fix/solve` | Bearer | **M8.** Runs the scenario's validation rules against real lab state; only when **all** pass is the scenario solved (+ scenario XP, once) and achievements evaluated. Otherwise returns `solved: false` with the failing checks |
| `GET /api/v1/labs/{id}` | Bearer | Own lab only. **M8:** adds `validation_rules` (and `scenario_id`/`fault_status` when a break-and-fix attempt exists) when the lab was created from a curriculum lab definition; the fields are omitted otherwise, so existing clients are unaffected |
| `GET /api/v1/providers/capabilities` | — | Capability registry with fidelity labels; nothing claims `REAL` (Redis-cached 60s when available) |
| `GET /api/v1/audit` | Admin | Audit entries, newest first; `?limit=` (default 50, max 100) and `?offset=` |

Errors use the envelope `{error:{code,message,request_id}}`. Every response
carries `X-Request-ID` (echoed when supplied, generated otherwise) and is
logged as structured JSON via `slog`.

Worker endpoints: `GET /healthz` and `GET /status` (database_connected,
job counts by state, uptime) on `:8081`.

Host-agent endpoints (port `:8082`, bearer token required except
`/healthz`):

| Method & path | Notes |
|---|---|
| `GET /healthz` | Liveness, incl. `mode` and `version` (`0.3.0-m3`) |
| `GET /v1/inventory` | Real host inventory (CPU/mem from `/proc`, disk via `statfs`, load) |
| `POST /v1/ops` | One allowlisted operation: `{op, lab_id, owner_id?, name?, id?, image?, profile?, host_path?, workdir?, command?, target?, cpu?, memory_mb?, disk_gb?, hostname?, username?, ssh_key?, idempotency_key?}`; idempotent replay by `(lab_id, op, idempotency_key)` |

Ops: `ListContainers`, `StartContainer`, `StopContainer`,
`RemoveContainer`, `CreateContainer`, `ListVMs`, `StartVM`, `StopVM`,
`RebootVM`, `DeleteVM`, `CreateVM`, `HostInventory`, `ExecInLab`.
Anything else is rejected with 400; an absent Docker/libvirt backend
returns 503 `host_agent_unavailable`. See "Host agent" below.

## Layout

```
cmd/api         HTTP server, graceful shutdown, config + store wiring
cmd/worker      Background worker: polls jobs, handles lab_provision /
                lab_destroy / lab_expire / audit_cleanup, expiry sweep,
                /healthz + /status
cmd/host-agent  M1 skeleton: starts, logs, /healthz — no host ops yet (M3)
internal/auth       bcrypt passwords, roles (learner/instructor/admin),
                    HMAC bearer tokens (config-driven TTL)
internal/cache      fail-open Redis cache (courses, capabilities; 60s TTL)
internal/config     env-driven config, fail-fast DATABASE_URL validation,
                    secret redaction
internal/courses    seeded M1 course catalogue (source for the memory store)
internal/database   pgxpool pool, health check, embedded migration runner
internal/httpapi    handlers, router, error envelope, middleware
internal/hostagent  M3 host agent: allowlisted ops + validation,
                    simulated/Docker/libvirt agents, VM profiles +
                    cloud-init, /proc inventory, typed HTTP client
                    and authenticated server (POST /v1/ops,
                    GET /v1/inventory, GET /healthz)
internal/ids        UUIDv4 generation (stdlib)
internal/jobs       worker service: claim/handle/retry/dead-letter, expiry
internal/labs       lab state machine, idempotency, async provisioning
                    (in-memory path), Transition/ExpireDue for the worker
internal/providers  capability registry (fidelity labels, health
                    probes with checked-at timestamps), MinIO S3
                    adapter (minio-go v7), DynamoDB Local HTTP
                    adapter, IAM policy evaluator (real subset),
                    STS/KMS/SQS/SNS/EventBridge educational
                    simulators, LocalStack/Floci emulator probe,
                    AWS wiring (NewAWS)
internal/quota      per-user active-lab quota (max 3)
internal/networkclient  generated gRPC client for the Rust network
                    engine (gen/cloudforge/v1, protoc 27.3 +
                    protoc-gen-go v1.36.12 + protoc-gen-go-grpc
                    1.6.2; see "Regenerating the gRPC client" below),
                    typed wrappers for all M1+M4 RPCs, structured
                    errors (ROUTE_CONFLICT, BACKEND_UNAVAILABLE),
                    real gRPC Health + labelled TCP fallback probe
internal/store      repository interfaces + in-memory fakes + pgx
                    implementations (users, courses, labs, audit, jobs,
                    lab resources, terraform runs, terminal sessions)
internal/terminal   M6 terminal tokens (Redis with in-memory fallback,
                    single-use, 5 min) + simulated / host-agent shells
internal/terraform  M7 Terraform: ContainerRunner (Docker Engine API,
                    pinned image), TestStubRunner (tests only), plan-JSON
                    parsing, endpoint/credential validation
internal/achievements M8 achievement trigger evaluation (pure: definitions
                    + user stats → newly earned ids; unobservable triggers
                    never award)
internal/curriculum M8 curriculum YAML loader (gopkg.in/yaml.v3 v3.0.1),
                    seeder (idempotent upserts), fault→engine mapping,
                    UTC streak computation
internal/validation M6 validation engine: real-state rule checks
                    (pass/fail/unknown, never a faked pass). M8 added
                    no_route_to_internet, security_group_denies,
                    load_balancer_healthy_targets, instance_reachable,
                    a real-state dns_resolves, and metric_below.
migrations/         001_init.sql (M1 schema), 002_enrollments_progress.sql,
                    003_jobs_audit.sql, 004_terminal_terraform.sql (M6/M7:
                    lab_resources name/parent/IP/CIDR columns,
                    terraform_runs plan/approval/job columns,
                    terminal_sessions lifecycle columns),
                    005_curriculum.sql (M8: course/lesson/lab-definition/
                    achievement columns, assessments,
                    assessment_submissions, lab_completions,
                    break_fix_attempts);
                    embedded via migrations.go
```

## Persistence model

- **Postgres mode** (DATABASE_URL reachable): users, courses, labs,
  enrollments, progress, XP, achievements, audit and jobs live in
  PostgreSQL. Lab creation writes `lab_sessions` + a `lab_resources`
  row + a `lab_provision` job + an audit row **in one transaction**;
  duplicate `(user_id, idempotency_key)` replays the stored lab.
  State transitions go through `labs.ValidateTransition` and persist
  atomically — an illegal transition returns 409 with no partial write.
- **In-memory mode** (no Postgres): the M1 stores are used and the API
  logs a warning. Data is lost on restart. Lab provisioning runs in the
  API process (M1 behaviour); the worker runs degraded
  (`database_connected:false`, no jobs processed).
- **Redis mode**: course list and capabilities are cached for 60s and
  invalidated on course mutation. If Redis is down at startup the API
  runs without cache and `/api/v1/health` reports `redis_connected:false`.

## Lab lifecycle

`CREATED → PROVISIONING → READY → RUNNING → VALIDATING → COMPLETED`,
plus `STOPPING/STOPPED`, `FAILED`, `EXPIRED`, `DESTROYING/DESTROYED`.
Transitions are validated by `labs.ValidateTransition` (see
`internal/labs/labs.go`); illegal transitions return 409. M2 adds
transitions *into* `EXPIRED` from active states so the worker's expiry
sweep can expire labs (previously `EXPIRED` was unreachable).

In Postgres mode the worker drives provisioning: `CREATED → PROVISIONING
→ READY`, or `FAILED` with the error recorded when the network engine is
unreachable — never faked as success. When a host agent is configured
(`HOST_AGENT_ADDR` set, M3) the worker provisions the lab VM through
the typed host-agent client instead, and the network-engine probe is
only the fallback when the host agent is unavailable. Expired labs are swept every ~30s
(`EXPIRED → DESTROYING → DESTROYED`, resources marked `CLEANED`).

## Jobs

Durable `jobs` table, claimed with `SELECT ... FOR UPDATE SKIP LOCKED`.
Retry is bounded exponential backoff (2^attempts seconds, capped at 60s);
after `WORKER_MAX_ATTEMPTS` (default 5) a job is dead-lettered
(`state='dead'`). A lab reaching `FAILED` because the engine is down is a
terminal lab outcome, not a job retry — that job completes.

## Host agent (Milestone 3)

The host agent (`cmd/host-agent`, `internal/hostagent`) is the only
privileged boundary on a host. The API and worker reach it **only**
through the typed client (`hostagent.Client`); lab provisioning and
destroy go through it when `HOST_AGENT_ADDR` is set in the worker's
environment, and fall back to the network-engine probe path (with a
logged reason) when the host agent is unavailable.

### Modes (`HOST_AGENT_MODE`, default `simulated`)

| Mode | What it does |
|---|---|
| `simulated` | In-memory containers/VMs with real lifecycle state transitions, resource accounting (host CPU/mem capacity), and cleanup. `ExecInLab` is validated but never runs — the result says so. Used for tests and in this sandbox. |
| `docker` | Real containers via a minimal typed HTTP client over `/var/run/docker.sock` (Engine API v1.41: list/create/start/stop/remove/inspect, exec, stats). Resource limits come from the profile/request; containers are labelled `cloudforge.lab-id` / `cloudforge.owner` / `cloudforge.name`; cleanup removes all of a lab's containers. VM ops return Unavailable. |
| `libvirt` | Real VMs via `virsh` (`qemu:///system`, socket `/var/run/libvirt/libvirt-sock`). VM lifecycle operates on a typed VM record store; the hypervisor call is isolated in one `hypervisor` method (fixed `virsh` argv, validated domain name only). Domain XML is rendered from typed structs, disk is a qcow2 backing-file plan, cloud-init user-data is generated from a validated struct. Container ops return Unavailable. |
| `auto` | Docker if its socket exists, else libvirt if its socket exists, else simulated (logged). |

A backend that is absent returns a structured `Unavailable` error
(HTTP 503 `host_agent_unavailable`) — never fake success. In this
sandbox Docker and libvirt are **not installed**, so `docker`/`libvirt`
mode honestly reports unavailable and the API/worker fall back to
`simulated` (or to the probe path when no host agent is configured).

### Why a socket client instead of github.com/docker/docker

`docker/docker` v28.3.3 was tried first: it fetched, but it does not
build with Go 1.25 — `go mod tidy` pulled a go1.26 toolchain and ~25
transitive dependencies, and the build still failed
(`undefined: sockets.DialPipe` in `client/client.go`, an API mismatch
between the `+incompatible` v28.3.3 module and the resolved
go-connections version). The Engine API is small and stable, so the
agent implements the calls it needs as typed structs over the Unix
socket with no third-party dependency. `go.mod`/`go.sum` were restored
to their pre-Docker state afterwards.

### Allowlist and validation (`hostagent.Validate`)

Only the 13 ops above exist. `Validate` rejects: unknown ops and
op/request mismatches; shell metacharacters (`; | & \` $ ( ) < >`
quotes, globs, newlines…) in every string param; absolute host paths
outside `/var/lib/cloudforge/labs` and `/srv/cloudforge/labs` (and
outside the lab's own directory, and `..` escapes); `ExecInLab`
commands not on the explicit allowlist (`aws`, `terraform`, `tofu`,
`docker` read-only subcommands, `kubectl` read-only verbs, `ping`,
`curl`, `ls`, `cat`) — `curl`/`ping` targets must be lab-internal
(`localhost`, `*.lab.internal`/`*.internal`, private/loopback IPs),
`ls`/`cat` paths must stay in the lab workdir; unknown VM profiles and
out-of-range vCPU/RAM/disk. There is no arbitrary shell execution
anywhere: no `os/exec` with user-supplied strings, no `sh -c`.

### VM profiles and cloud-init

| Profile | vCPU | RAM | Disk |
|---|---|---|---|
| `micro` | 1 | 1 GiB | 8 GiB |
| `small` | 2 | 2 GiB | 16 GiB |
| `medium` | 2 | 4 GiB | 30 GiB |
| `database` | 2 | 4 GiB | 30 GiB |
| `network-appliance` | 1 | 1 GiB | 8 GiB |

(Micro is 1 GiB, inside the spec's 512 MB–1 GiB range.) Creation
checks total host capacity against live inventory. Cloud-init
user-data is rendered from a typed struct with validated
hostname/username/SSH key and quoted YAML scalars — no template
injection.

### Auth and ownership

`POST /v1/ops` and `GET /v1/inventory` require
`Authorization: Bearer <HOST_AGENT_TOKEN>` (constant-time compare).
The token may carry a lab scope as a `:lab:<lab-id>` suffix
(`SECRET:lab:abc123`); a scoped token may only operate on that lab,
a token without a scope is the control-plane token (API/worker). This
is the interim ownership model — **M9 upgrades it to mTLS** with
per-lab certificates. `HOST_AGENT_TOKEN` is required in
docker/libvirt mode; in simulated mode an insecure dev default is
used with a warning when unset (same pattern as the API JWT secret).
Requests carry `X-Request-ID` and are logged as structured JSON via
`slog`.

### Real-host requirements (docker/libvirt mode)

- Docker: `docker.io` or `docker-ce`, daemon running, socket at
  `/var/run/docker.sock` readable by the host-agent user.
- libvirt/QEMU: `qemu-kvm`, `libvirt-daemon-system`,
  `libvirt-clients` (`virsh`), `virtinst`, `cloud-image-utils`,
  a `cloudforge-br0` bridge, and a base image
  (`ubuntu-24.04-base.qcow2`) for the qcow2 backing-file plan.
- The host agent must run on the host (or in a privileged container
  with the sockets mounted); the Docker/libvirt sockets are never
  exposed to the API container.

### What is real vs simulated in this sandbox

- Real: allowlist validation, VM profiles + capacity checks,
  cloud-init generation, `/proc` inventory parsing (verified against
  live `/proc`: 4 CPUs, AMD EPYC, 7934 MiB), simulated-agent
  lifecycle/accounting/cleanup, the authenticated HTTP API, and the
  worker provisioning path (verified end-to-end: API → worker →
  host agent → lab READY with a running VM, then destroy → 0 VMs).
- Simulated: container/VM state in `simulated` mode (no hypervisor
  or daemon behind it) and `ExecInLab` in that mode.
- Unavailable in this sandbox: Docker (no socket/daemon) and libvirt
  (no socket, no `virsh`, no QEMU) — both report 503 honestly.

## Milestone 3 limitations (honest)

- **Audit is transactional only where the store owns the transaction**
  (lab creation/transitions, which record audit rows in the same tx).
  Other mutations (register/login, course CRUD, enroll, progress) are
  audited best-effort by the HTTP layer after the mutation succeeds.
- **Postgres integration is now verified in this sandbox.**
  Postgres 16 + Redis 7 run on localhost; the integration test passes
  against `cloudforge_test`, and the M3 end-to-end flow (register →
  lab → worker provisions via host agent → READY → destroy) was run
  against the live `cloudforge` database.
- **Tokens are still hand-rolled HMAC, not JWTs**, and host-agent
  auth is a shared bearer token with a lab scope, not mTLS (M9).
- **Libvirt has no Go client library in this build** (no CGO
  libvirt-go): it drives `virsh` with fixed argv. `virsh domstats`
  per-VM CPU reporting is not wired yet — libvirt usage reports
  configured memory and zero CPU rather than a fabricated sample.
- **Docker stats/exec output is not streamed**: exec returns the exit
  code with output discarded; stats are a single non-streaming sample.
- **No rate limiting, no TLS.**
- **Disk inventory is best-effort**: when the lab volume path does
  not exist, `statfs` fails and disk fields report 0 ("unknown");
  capacity checks skip unknown dimensions rather than rejecting.
- `terraform_runs` and `terminal_sessions` tables are now written by
  the M6/M7 code (migration 004 extends the schema 002 created):
  terminal tokens/sessions on every token issue + WebSocket close,
  terraform runs on every init/validate/plan/apply/destroy.
- `backend/Dockerfile` landed in M9 Part 3 (see below).

## Milestone 4 — generated gRPC network-engine client

`internal/networkclient` is now a real typed gRPC client generated
from `proto/cloudforge/v1/*.proto` (all M1 RPCs plus the 12 M4 RPCs:
CreateSubnet/DeleteSubnet, CreateVethPair, AttachInterface,
AddRoute/DeleteRoute, CreateFirewallPolicy/ApplyFirewallPolicy,
CreateNAT/DeleteNAT, InjectFault, CollectTelemetry). Health is a real
gRPC Health RPC with a 2s timeout and `x-request-id` metadata; lab
provisioning (`labs`/`jobs` `CheckNetwork`) gates on it. The only TCP
dial left is `TCPReachable`, used solely for `/api/v1/health`'s
legacy `network_engine_reachable` boolean and labelled as "proves
only that a listener exists". Structured errors carry the engine's
stable code (`ROUTE_CONFLICT` → gRPC FailedPrecondition,
`BACKEND_UNAVAILABLE` → Unavailable); see `IsRouteConflict` /
`IsBackendUnavailable`. Live-verified against the real Rust engine
(simulated backend) on 2026-11-21: Health, CreateNetwork,
AllocateAddress with hint, CreateSubnet, route conflict, firewall
create/apply, 11 telemetry points, NAT create/delete, InjectFault,
CleanupLab — see `internal/networkclient/live_test.go`
(`CLOUDFORGE_LIVE_ENGINE=1`, skipped by default).

### Regenerating the gRPC client

System protoc is not installed in this sandbox; the M4 codegen used
the official protoc 27.3 release zip plus Go plugins, pinned here:

- `protoc` 27.3 (`github.com/protocolbuffers/protobuf` releases,
  `protoc-27.3-linux-x86_64.zip`)
- `protoc-gen-go` v1.36.12 (`google.golang.org/protobuf`
  v1.36.12, `go install .../cmd/protoc-gen-go@latest`)
- `protoc-gen-go-grpc` 1.6.2 (`google.golang.org/grpc` v1.78.0 —
  deliberately **not** v1.85.0, which requires Go 1.26; the repo
  stays on Go 1.25, with `golang.org/x/crypto` v0.44.0 and friends
  pinned to go-1.25-compatible versions in `go.mod`)

```bash
export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin
cd ../..  # repo root
protoc -I proto \
  --plugin=protoc-gen-go=$HOME/go/bin/protoc-gen-go \
  --plugin=protoc-gen-go-grpc=$HOME/go/bin/protoc-gen-go-grpc \
  --go_out=backend/internal/networkclient/gen --go_opt=paths=source_relative \
  --go-grpc_out=backend/internal/networkclient/gen --go-grpc_opt=paths=source_relative \
  proto/cloudforge/v1/common.proto proto/cloudforge/v1/network.proto
cd backend && go mod tidy && go test ./internal/networkclient/ -count=1
```

Generated files live in `backend/internal/networkclient/gen/` and are
committed (no protoc needed to build). Note the Rust side renders the
proto `NATRule` message as `Nat` in Rust only (prost naming); the Go
generated type is `NATRule` — same wire format, no interop issue.

## Milestone 5 — AWS provider integration

See `../docs/providers.md` for the full investigation (LocalStack
Community vs Pro after the March 2026 licensing change, Floci
status) and the per-service fidelity/limitations table. Summary:

- **S3** — real S3-compatible calls via MinIO (`minio-go/v7`
  v7.0.95) at `MINIO_ENDPOINT`; `CreateBucket`/`ListBuckets`/
  `PutObject`/`GetObject` when reachable, else 503 with capability
  info. Fidelity `API_EMULATED` (real local object storage, not AWS).
- **DynamoDB** — raw HTTP JSON adapter (`X-Amz-Target`) at
  `DYNAMODB_ENDPOINT`; `CreateTable`/`PutItem`/`GetItem`/`ListTables`
  when reachable, else 503. Fidelity `API_EMULATED`.
- **IAM** — real evaluation of a documented subset in
  `internal/providers/iam.go` (Allow/Deny, wildcards, explicit deny
  wins, `StringEquals` on `aws:username`); fidelity `PARTIAL`.
- **STS/KMS/SQS/SNS/EventBridge** — educational in-memory simulators
  (fidelity `SIMULATED`): `AssumeRole`/`GetCallerIdentity`,
  AES-256-GCM key create/encrypt/decrypt round-trip (labelled
  educational, **not** real KMS), queue send/receive (receive
  removes — no visibility timeout), topic publish/subscribe fan-out,
  rule matching on source + detail-type.
- **Emulator** — `AWS_EMULATOR_ENDPOINT` health-probed at startup
  and on demand (`GET {endpoint}/_localstack/health`); EC2 capability
  health reflects what the probe actually finds. No EC2 API calls are
  issued in M5.
- **Health** — every capability carries `health` + `checked_at`;
  nothing is assumed ok. The registry is the source of truth (the
  store has no capability table; capabilities are seeded in memory,
  not persisted to Postgres).
- **Sandbox status (2026-11-21):** no MinIO, DynamoDB Local, or
  LocalStack/Floci is reachable here, so S3/DynamoDB/EC2 operations
  honestly return 503/`unavailable`; the simulators and IAM
  evaluation work in-process. Run MinIO / DynamoDB Local / Floci on
  their default ports to verify the real paths.

## Milestone 4 needs (remaining)

Libvirt per-domain stats via `virsh domstats`.

## Milestone 6 — lab resources, topology, validation, terminal

`internal/validation` evaluates the curriculum's rule vocabulary
against real state: DB `lab_resources`, engine `InspectTopology`
nodes, host-agent VM states. Implemented rules: `lab_exists`,
`vpc_exists`, `subnet_count`, `subnet_exists`, `route_to_igw`,
`route_to_nat`, `nat_exists` / `nat_gateway_exists`,
`security_group_allows`, `security_group_allows_ssh`,
`instance_running`, and (since M8) `dns_resolves` (real-state check:
a VPC recording `dns_enabled`, else the control-plane resolver for
public names only), `no_route_to_internet`, `security_group_denies`,
`load_balancer_healthy_targets`, `instance_reachable`, and
`metric_below` (metrics recorded in a resource's config; unknown
without a metrics pipeline). Any other rule name is `unknown` with the
missing source named. Overall `passed` requires every check to pass;
`xp_eligible` mirrors it — no XP is awarded here (that is M8).
Resource `config` is where kind-specific truth lives: subnet CIDRs,
route-table `routes: [{destination, target}]`, security-group
`rules: [{port, source}]`, gateway `gateway_type: nat`.

The terminal WebSocket uses `golang.org/x/net/websocket` — already in
the module graph as a grpc dependency, Go 1.25 compatible, so no new
dependency was added. `nhooyr.io/websocket` was considered and not
used for exactly that reason; `gorilla/websocket` was not used
because it would pull a newer Go toolchain.

**Live-verified 2026-11-21** against Postgres 16 + Redis 7 and the
real Rust engine (SIMULATED backend) on `localhost:50051`: lab
create → provisioned VPC row listed; subnet create via the engine
(network auto-created from the lab VPC CIDR, 201); topology merged
engine + DB nodes (200); validate passed all 5 vpc-fundamentals
checks (`passed: true`, `xp_eligible: true`); subnet delete via the
engine (200); terminal token issued with `redis_backed: true`.
Two bugs found by that smoke test were fixed in the same pass: the
engine rejects `InspectTopology` with an empty idempotency key
(now `topology:<request-id>`), and subnet creation failed
`NOT_FOUND` because lab provisioning never creates an engine
network (now created lazily from the lab VPC CIDR, retried once).

**Known limitations:** the frontend `Terminal.tsx` still passes the
long-lived Bearer token as the WebSocket token (see
`frontend/src/components/Terminal.tsx`); the backend correctly
rejects it — the frontend must call `POST .../terminal/token` first
(M6 frontend follow-up, out of scope for the backend). The
simulated shell has no pty, so `resize` is acknowledged but has
nothing to resize. `ExecInLab` through the Docker host agent
returns only the exit code/message (output is discarded by the
agent), so the real shell is terse. Validation rules that need
AWS-provider state (buckets, queues, Lambda, …) report `unknown`.

## Milestone 7 — Terraform

`internal/terraform` runs Terraform only inside a Docker container
(`ContainerRunner`, Docker Engine API over `/var/run/docker.sock`,
image `hashicorp/terraform:1.9.8` pinned; `opentofu/opentofu:1.9.2`
is the documented alternative via `TERRAFORM_IMAGE`). The container
mounts only the lab workdir (`<TERRAFORM_WORKDIR_ROOT>/<lab-id>/
terraform`), runs with 1 vCPU / 512 MiB, and gets the provider
config (`provider.tf`, generated per lab) pointing at the emulator /
MinIO endpoints with dummy local credentials. `ValidateEndpoint`
SSRF-guards endpoints (loopback only: `localhost`, `127.0.0.0/8`,
`::1`); `ValidateProviderConfig` refuses non-emulator AWS
credentials from the environment; `ValidateConfiguration` rejects
HCL referencing `amazonaws.com` or embedding keys. Plan output is
parsed with `ParsePlanJSON` (requires `format_version` +
`planned_values`) and stored; applies require an explicit approval
of a succeeded, unexpired plan (1h TTL) and sync the plan's
resources (`aws_vpc`→`vpc`, `aws_subnet`→`subnet`,
`aws_instance`→`instance`, `aws_internet_gateway`/`aws_nat_gateway`
→`gateway`, `aws_lb`→`loadbalancer`, `aws_route_table`→`route-table`,
`aws_security_group`→`security-group`) into `lab_resources`.
`TestStubRunner` (labelled `Stub=true` in every result) is used
only in tests. The worker (`jobs.handleTerraform`) marks a failed
run `FAILED` and completes the job — except a Docker `Unavailable`
error, which retries like any transient outage.

**Sandbox status (2026-11-21):** no Docker socket exists here, so
`ContainerRunner` honestly reports unavailable and every Terraform
action answers **503 `terraform_unavailable`** before anything is
enqueued (verified by test + live smoke test); no Terraform ever
runs on the host. The full plan → approve → apply → destroy flow
is verified in tests with the stub runner and the real worker
service, including resource sync and destroy cleanup. The
container-execution path itself (create/start/wait/logs/archive
against a real daemon) is implemented but **unverified live** — it
needs a host with Docker to exercise. The container also uses
bridge networking with `host.docker.internal:host-gateway` so the
emulator endpoint on the host stays reachable; that mapping is the
one place the "no network except the emulator endpoint" rule is
approximated rather than enforced by a network namespace.

## Milestone 8 — Curriculum backend (seed + API + XP + Break-and-Fix)

`internal/curriculum` loads the repo's `curriculum/` YAML
(`gopkg.in/yaml.v3` v3.0.1, the version that installs for Go 1.25)
and seeds it into Postgres through `internal/store`
(`PostgresCurriculumStore`; `MemoryCurriculumStore` shares the
memory course store, so seeded tracks are visible through the
existing `CourseStore` and lesson completion works in both modes).
Seeding is idempotent — every upsert keys on a stable id (track id,
module position, lesson slug, question index, lab-definition id,
achievement id, scenario id) — and runs at API startup when
`CURRICULUM_SEED=1` (the default), via the `-seed-curriculum`
flag, or on demand through `POST /api/v1/admin/seed-curriculum`
(admin only). Seed counts for the shipped content: **14 tracks, 70
lessons, 84 assessment questions, 6 break-and-fix scenarios, 15
achievements** (plus 2 guided/challenge lab definitions per track =
34 lab definitions).

Migration `005_curriculum.sql` adds only what M8 uses: course
`difficulty`/`estimated_hours`/`xp_reward`; lesson `slug`/`theory`/
`key_points`/`diagram`; lab-definition `validation_rules`/`tasks`/
`kind`/`fault`/`hints`/`fix_steps`/`xp_reward`; achievement
`trigger` (JSONB); and the new `assessments`,
`assessment_submissions`, `lab_completions`, `break_fix_attempts`
tables.

**M1 alignment (verified, not changed):** the M1 seeded courses
match the curriculum YAML lesson order/topics — `vpc-fundamentals`
(6) → `networking`, `iam-security` (5) → `iam`,
`ec2-basics` (5) → `ec2` (titles differ only in capitalisation).
The M1 course rows are left untouched; the curriculum tracks are
new course rows (`networking`, `iam`, `ec2`, …), so the old
`/api/v1/courses` catalogue still lists the M1 courses and the new
`/api/v1/curriculum/tracks` surface lists all 17.

**XP:** lesson completion still awards 10 XP (+10 first-lesson
bonus) through the existing `xp_transactions` path, and a
re-completion awards 0 — the `user_progress` PK
`(user_id, lesson_id)` is the unique constraint that prevents
double-awards, plus per-reason guards for assessments and
break-and-fix solves. A track's `xp_reward` is split across its
lessons: 10 XP per lesson plus, on the final lesson, the remainder
(`xp_reward − 10 × lessonCount`), so a completed track awards
exactly `xp_reward` in lesson XP (e.g. networking: 250). Courses
with `xp_reward = 0` (all pre-M8 courses) pay no bonus, so M2
behaviour is unchanged. Assessments award 20 XP only on the first
pass at ≥ 80% (server-side grading; the client never supplies
correctness). Break-and-fix solves award the scenario XP once.

**Achievements + streaks:** `internal/achievements` evaluates the
triggers in `achievements.yaml` on every XP/progress event (lesson
completion, assessment pass, passing lab validation, break-and-fix
solve). All 15 shipped triggers are observable and live
(`lessons_completed`, `labs_completed`, `challenges_completed`,
`break_fix_solved`, `track_completed`, `tracks_completed`,
`assessment_score`, `xp_total`, `streak_days`). The three M2 legacy
achievements (`first-lesson`, `vpc-builder` — no trigger — and the
YAML-overwritten `first-lab`) are not auto-awarded by the evaluator;
`first-lesson` is still awarded directly by the M2 lesson path.
Streaks are consecutive UTC calendar days with a lesson, lab, or
assessment activity (a streak ending yesterday still counts; the
current day not being over yet does not break it).

**Break-and-fix:** the 6 scenarios are seeded as lab definitions
(`break-fix-<id>`, kind `break-fix`) with their fault spec.
`POST /labs/{id}/break-fix/start` maps the YAML fault kind to the
engine `FaultSpec` (`route-blackhole`→5/magnitude 1,
`latency`→1/`latency_ms`, `loss`→2/`loss_percent`, `dns`→4/100) and
calls `InjectFault`; when the engine is unreachable the attempt is
recorded as `fault_pending` and the lab is honestly reported as
created without the fault. `POST /labs/{id}/break-fix/solve` runs
the scenario's rules through the shared validation path and awards
XP + achievements only when every rule passes.

**Validation (M8 additions):** `no_route_to_internet` (no
`0.0.0.0/0` route to an IGW/NAT in any route table),
`security_group_denies` (no SG rule allows the port from the
source), `load_balancer_healthy_targets` (recorded
`healthy_targets` in the LB resource config),
`instance_reachable` (a running instance with a recorded address),
`dns_resolves` (a VPC resource recording `dns_enabled`/`dns_hostnames`,
else the control-plane resolver for public names only — internal
names are never asked of the host resolver; unknown when there is
no DNS source at all), and `metric_below` (metrics recorded in a
resource's `config.metrics`; unknown without a metrics pipeline —
there is no live metrics pipeline yet). The two M6 tests that
asserted `dns_resolves` is always unknown were updated to the new
semantics. `POST /labs/{id}/validate` now defaults to the lab
definition's seeded rules when the lab comes from a curriculum
definition, and a passing validation records a lab/challenge
completion (break-and-fix labs are completed only via the solve
endpoint).

**Known limitations:** no live metrics pipeline, so `metric_below`
is unknown unless a resource records metrics in its config, which
is what makes the `latency-spike` scenario solvable only after
such a recording; `dns_resolves` is not an in-VPC lookup; the
`first-lesson` / `vpc-builder` legacy achievements have no
curriculum trigger and are never auto-awarded by the evaluator;
the in-memory curriculum store's streak is best-effort (lesson
completions are not timestamped in the memory course store, so it
counts today when any lesson is complete); the admin re-seed
endpoint was verified only through `SeedDir` in tests, not live
against the running API.

## Milestone 9 — Production readiness (backport, part 1)

Backports the GitHub MVP's foundation (Argon2id/JWT, chi,
Prometheus, rate limiting, root Makefile, same-origin proxy,
security/API/runbook docs) onto the local M5–M8 architecture.
Full detail: `docs/security/model.md`, `docs/api/reference.md`,
`docs/operations/runbook.md`.

- **Auth (`internal/auth/password.go`, `auth.go`):** Argon2id
  (`$argon2id$v=19$m=65536,t=3,p=2$`, OWASP interactive-login
  parameters) replaces bcrypt for new passwords; HS256 JWT
  (`golang-jwt/jwt v5.3.1`, 12h TTL, `CLOUDFORGE_JWT_SECRET`)
  replaces hand-rolled HMAC tokens. **Dual-verify migration:**
  legacy bcrypt hashes still verify and are upgraded to Argon2id
  on successful login (memory store + Postgres `UPDATE`, best
  effort). No migration was needed — `password_hash` is `TEXT`.
  Old HMAC tokens are no longer accepted (re-login required).
- **Routing (`internal/httpapi`):** go-chi/chi v5.5.0 replaces the
  stdlib ServeMux; every M1–M8 route, the X-Request-ID middleware,
  the `{error:{code,message,request_id}}` envelope and the
  auth/ownership checks are preserved. Path params are read with
  `chi.URLParam`.
- **Observability (`internal/metrics`, `handleReadyz`):**
  `GET /metrics` (request count/duration by method + chi route
  pattern, `cloudforge_network_engine_up`) and `GET /readyz`
  (live Postgres/Redis/engine checks; overall readiness gated
  only on Postgres when configured — Redis and engine are
  reported but do not gate, because both are fail-open by design).
  `GET /healthz` is unchanged.
- **Rate limiting (`internal/ratelimit`):** Redis-backed
  fixed-window counters (atomic Lua `INCR` + `PEXPIRE`), per
  client IP. Auth endpoints: 10/5 min, **fail-closed** (Redis
  down → 503 `rate_limiter_unavailable`). General `/api/v1`:
  300/1 min, **fail-open** (Redis down → request proceeds).
  Over-limit → 429 `rate_limited` + `Retry-After`. Config:
  `CLOUDFORGE_AUTH_RATE_LIMIT` / `..._WINDOW`,
  `CLOUDFORGE_API_RATE_LIMIT` / `..._WINDOW`. Nil limiter =
  disabled (tests, Redis-less dev).
- **Root `Makefile`:** `verify` (go vet + go test + cargo test +
  frontend build/lint/test, no Docker needed), `backend-test`,
  `engine-test`, `frontend-test`, `frontend-build`,
  `frontend-lint`, `fmt`, `up`, `down`, `logs`, `ps`, `clean`.
  `up`/`down`/`logs`/`ps` fail with a clear message when Docker
  is absent. `make verify` must pass in the sandbox.
- **Same-origin proxy:** `frontend/nginx.conf` proxies `/api`,
  `/healthz`, `/readyz`, `/metrics` to `api:8080` (Docker DNS
  resolver, per-request resolution; dedicated WebSocket location
  for `/api/v1/labs/{id}/terminal` with Upgrade headers).
  `frontend/src/services/api.ts` defaults to same-origin relative
  URLs when `VITE_API_URL` is unset; `frontend/vite.config.ts`
  proxies the same paths to `localhost:8080` under `npm run dev`.
  Compose builds the frontend with an empty `VITE_API_URL` by
  default. Compose also fixed two env-name bugs: `JWT_SECRET` →
  `CLOUDFORGE_JWT_SECRET` and `REDIS_URL` → `REDIS_ADDR` (neither
  old name was ever read by config); the api healthcheck is now
  `/readyz`, and frontend waits for api healthy.
- **Go version:** `go.mod` now targets Go 1.26.0 — forced by
  `prometheus/client_golang v1.25.0`, which requires Go ≥ 1.26
  (its `go.mod` says `go 1.26.0`). The sandbox's cached 1.26.0
  toolchain is used; `GOTOOLCHAIN=local` prevents downloads.

**Known limitations (M9):** no token revocation (a stolen JWT is
valid until expiry); rate limiting is per IP (a classroom behind
one NAT shares a budget); `nginx -t` / `docker compose build` /
`up` were not run here (no nginx or Docker in this sandbox) —
Docker-based verification is pending on a real host. The
`backend/Dockerfile` landed in M9 Part 3 (see below).

## Milestone 9 — Production readiness (part 3, final)

- **Dockerfile (`backend/Dockerfile`, `backend/.dockerignore`):**
  multi-stage — `golang:1.26.0-bookworm` builder (digest-pinned)
  builds `/app/api` and `/app/worker` (CGO_ENABLED=0, `-trimpath`,
  `-ldflags="-s -w"`), runtime is
  `gcr.io/distroless/static-debian12:nonroot` (tag-pinned; the
  digest could not be resolved through the sandbox proxy — pin it
  in CI via `docker buildx imagetools inspect`). Non-root
  (`USER nonroot:nonroot`), no secrets baked in, CA certs copied
  from the builder, `HEALTHCHECK` runs `/app/api --health-check`
  (new flag in `cmd/api/main.go`: probes `GET
  http://127.0.0.1:<port>/readyz`, exit 0 only on HTTP 200).
  Validated with hadolint 2.12.0 (0 warnings); `docker build`
  itself was not run here (no Docker). Compose was fixed alongside:
  api healthcheck uses the flag (distroless has no wget/curl),
  network-engine build context is the repo root, its healthcheck
  is a TCP probe (the Rust binary ignores argv, so a `--health-check`
  invocation could never pass), frontend runs nginx as `nginx` on
  port 8080.
- **Security (`internal/security/`):** one shared SSRF guard —
  `ValidateEndpointURL` / `ValidateEndpointHost` accept only
  loopback (`localhost`, `127.0.0.0/8`, `::1`) or an exact-match
  allowed host (e.g. Docker service name `minio`); rejects
  embedded credentials, `169.254.169.254`, RFC1918, `0.0.0.0`,
  non-http(s) schemes, and lookalike hosts
  (`127.0.0.1.evil.com`). Wired into the Terraform runner
  (`terraform.ValidateEndpoint` delegates to it), the S3 adapter
  (`NewS3Adapter`), the emulator probe (`ProbeEmulator`), and
  `providers.NewAWS` (fail-fast at startup). Secrets redaction:
  `RedactString` / `RedactURL` / `ScrubMap` / `Secret` (slog
  `LogValuer`); audit metadata is scrubbed before recording.
  Findings + fixes: `docs/security/model.md` (appended M9 Part 3
  review; audit-log retention: 90 days via the worker's
  `audit_cleanup` job, no archival — snapshot `audit_logs` first).
- **Quotas (`internal/quota/`, `GET/PUT /api/v1/admin/quotas`):**
  per-role limits — labs, CPU, memory, disk, lab hours — persisted
  in the `quotas` table (migration `006_quotas.sql`, seeded with
  learner 3/4/8192 MB/50 GB/8 h, instructor 5/8/16384/100/12 h,
  admin 10/16/32768/200/24 h). Admin-only; partial updates;
  per-value caps (max_labs 100, max_cpu 64, max_memory_mb 262144,
  max_disk_gb 2000, max_lab_hours 168). Enforced at lab creation
  (active count + definition duration) and instance creation
  (profile cpu/mem/disk vs the role limit) with `429
  quota_exceeded`; the store ceiling (`LabStore.SetMaxLabs`) is a
  backstop set to the highest configured role limit.
- **Monitoring (`internal/metrics`):** new series
  `cloudforge_host_agent_up` (set by `/api/v1/health`),
  `cloudforge_lab_events_total{event}` (lab_created /
  lab_destroyed), `cloudforge_jobs_total{type,result}`.
  Dashboards + scrape config: `infrastructure/monitoring/`
  (Grafana overview + labs dashboards, Prometheus config, optional
  `monitoring` compose profile for prometheus/grafana).
- **Backup/restore (`scripts/backup.sh`, `scripts/restore.sh`):**
  `pg_dump -Fc` / `pg_restore`, env-driven `DATABASE_URL`,
  timestamped files under `BACKUP_DIR` (default `./backups`,
  keeps newest `BACKUP_KEEP=7`), `set -euo pipefail`, `--help` /
  `--dry-run` on both; restore refuses a non-empty target without
  `--force`. Round-trip verified against a scratch database
  (21 users, 3 quota rows, 24 tables restored exactly).
- **Cleanup reconciliation (`internal/jobs/reconcile.go`,
  `POST /api/v1/admin/cleanup-reconcile`):** `cleanup_reconcile`
  job type + admin endpoint detect orphans both ways — DB
  `lab_resources` with no live counterpart (engine subnet/network,
  host-agent VM) and live engine/host-agent resources with no DB
  row. Dry-run is the default (endpoint `dry_run` absent = true;
  job payload `dry_run` absent = true); enforcement
  (`dry_run=false`) deletes the orphaned side and audits every
  removal. An unreachable backend is reported, never treated as
  "everything is gone". Engine access goes through the
  `EngineTopology` interface (`jobs.NewEngineAdapter` adapts the
  gRPC client); host-agent access reuses `hostagent.Provisioner`.
  Per-lab (`lab_id`) or all labs (new `LabStore.ListAll`).
  Tested with fake engine/host-agent stores (7 jobs tests + 2
  endpoint tests); the E2E test exercises it against the real API.
- **End-to-end acceptance test (`tests/e2e/`):** black-box,
  stdlib-only Go test that builds the real `api` binary and the
  `backend/cmd/e2e-fakes` binary (fake gRPC engine + simulated
  host agent), starts them, and drives register → course → lab →
  VPC → 2 subnets → NAT/SG → VM → terminal token → topology →
  fault injection → diagnose → repair → validation (5/5) → XP →
  destroy → reconcile-verified cleanup. Passes in ~12 s; skips
  gracefully when `CLOUDFORGE_E2E_SKIP=1` or the toolchain/fakes
  cannot start. Run: `cd tests/e2e && go test -v .`
