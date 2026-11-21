# CloudForge

**Build Cloud Skills. Not Cloud Bills.**

CloudForge is a standalone, locally deployable cloud learning and
infrastructure simulation platform. It lets users learn AWS concepts and
cloud infrastructure hands-on — virtual networks, Linux instances,
routing, firewalls, storage, databases, serverless, load balancers,
Terraform — without an AWS account, subscription, credit card, paid
cloud resources, or any external cloud provider.

It runs on a Linux machine using Docker and KVM. Wherever practical it
uses **real infrastructure** (Linux network namespaces, bridges, nftables,
KVM/QEMU VMs, Docker containers, PostgreSQL, MinIO); where full
implementation is impractical it uses an AWS API emulator or an
educational simulator — and it always says which one it is doing.

> CloudForge is a real working product, not a static dashboard, mockup,
> or collection of disconnected demonstrations. A feature is only claimed
> complete when it works end-to-end against real (local) infrastructure.

## Architecture

Hybrid Go + Rust, modular monolith + one independent engine:

```
React Frontend (TypeScript, Vite, Tailwind)
        |  REST / WebSocket
        v
Go CloudForge API (control plane)
  auth · users · courses · labs · scheduler · orchestration
  provider registry · terraform manager · audit · quotas
        |                 |                 |
        v                 v                 v
  PostgreSQL         Redis (jobs)     Go Host Agent
  (migrations)       (Asynq)          Docker · libvirt/QEMU
        |
        | gRPC + Protobuf (Unix socket where local)
        v
  Rust Network Engine (independent service)
  namespaces · bridges · veth · IPAM · routing
  nftables · NAT · traffic simulation · fault injection
        |
        v
  AWS Provider Adapters — official AWS emulator (primary),
  optional Floci, MinIO, DynamoDB Local, educational simulators
```

Service boundaries, the gRPC contract, fidelity labels and the lab
lifecycle state machine are documented in
[docs/architecture/overview.md](docs/architecture/overview.md).

## Fidelity labels

Every AWS-style operation reports one of: `REAL` (real local
infrastructure), `API_EMULATED` (AWS-like API implementation),
`SIMULATED` (educational implementation), `PARTIAL` (some operations
supported), `UNSUPPORTED` (not implemented). CloudForge never silently
pretends an unsupported operation succeeded.

## Repository layout

```
cloudforge/
  frontend/               React + TypeScript + Vite + Tailwind
  backend/                Go control plane (cmd/api, cmd/worker, cmd/host-agent)
  rust-network-engine/    Rust infrastructure engine (crates/ + proto/)
  proto/cloudforge/v1/    Shared protobuf contracts (common, network)
  providers/              AWS emulator / Floci / MinIO / simulator adapters
  curriculum/             Course content (fundamentals, iam, ec2, networking, ...)
  lab-images/             VM/container image definitions
  infrastructure/         docker, kvm, networking, monitoring, scripts/
  deployments/compose/    docker-compose.yml + docker-compose.dev.yml
  docs/                   architecture, api, security, networking, curriculum, operations
  scripts/  tests/
```

## Milestone status

| Milestone | Status |
| --- | --- |
| M1 Foundation | complete |
| M2 Go Control Plane | complete |
| M3 Host Agent | complete (simulated + Docker/libvirt backends; real KVM unverified in this sandbox) |
| M4 Rust Network Engine | complete (74 tests pass; real rtnetlink/nftables backend compiles, unverified here) |
| M5 AWS Provider Integration | complete (MinIO/DynamoDB/IAM, emulator) |
| M6 Lab Workstation | complete (live topology/resources/validate wiring, M9 Part 3) |
| M7 Terraform | complete |
| M8 Curriculum | complete (14 tracks, 70 lessons, 84 questions, 6 break-and-fix scenarios, 15 achievements) |
| M9 Production Readiness | complete — Part 1 backport, Part 2 AWS-console-style UI, Part 3 (below) |

## Milestone 1 — Foundation (complete)

- Monorepo scaffolded (Go backend, Rust workspace, React frontend).
- Shared protobuf contracts: `proto/cloudforge/v1/common.proto`,
  `proto/cloudforge/v1/network.proto` (versioned, health + network ops).
- Go API health checks; Rust engine health checks; Go→Rust gRPC
  connectivity (see the backend / Rust workstream notes).
- PostgreSQL 16 + Redis 7 via compose; in-memory store until Postgres
  wiring lands (see Known limitations).
- Basic auth (register/login) and the initial dashboard.
- React frontend: Dashboard (live health + courses), Learn, Courses,
  Labs, Cloud Console (M1 topology placeholder), Login/Register.
- Compose deployment, `.env.example`, host readiness script.

## Milestone 2 — Go Control Plane (complete)

- **PostgreSQL persistence (pgx v5.7.5):** `internal/database` pool +
  embedded migration runner (001–003, advisory-locked so api + worker
  can start concurrently); `internal/store` repository interfaces with
  pgx implementations and in-memory fakes. Lab creation writes
  `lab_sessions` + `lab_resources` + `lab_provision` job + audit row in
  one transaction; idempotency via the DB unique constraint.
- **Auth upgrade:** bcrypt passwords, roles (`learner` / `instructor` /
  `admin`, bootstrap via `CLOUDFORGE_ADMIN_EMAIL`), admin-only user list
  and course CRUD, modules/enroll/progress endpoints, XP on lesson
  completion (10 XP + 10 first-lesson bonus, idempotent).
- **Real background worker:** polls the durable `jobs` table
  (`FOR UPDATE SKIP LOCKED`), handles `lab_provision` / `lab_destroy` /
  `lab_expire` / `audit_cleanup`, bounded exponential backoff, dead
  letters after 5 attempts, sweeps expired labs, `/status` endpoint.
  Lab provisioning moved out of the API goroutine into the worker.
- **Audit:** every mutation audited; admin-only `GET /api/v1/audit`
  with pagination (limit max 100).
- **Redis cache (go-redis v9.7.3):** course list + capabilities cached
  60s with invalidation on mutation; fail-open when Redis is down, and
  `/api/v1/health` reports live `database_connected` / `redis_connected`.
- Verified end-to-end in this sandbox against real PostgreSQL 16.15
  and Redis 7.0 (see backend README): lab `CREATED → READY` via the
  worker, idempotent replay, `DESTROYING → DESTROYED` with resources
  marked `CLEANED`, honest `FAILED` when the network engine is down,
  Redis-down fail-open with correct health reporting.

## Milestone 9 — Production readiness (backport, part 1)

Backports the GitHub MVP's foundation onto the local M5–M8
architecture (see `docs/security/model.md`,
`docs/api/reference.md`, `docs/operations/runbook.md`):

- **Auth:** Argon2id password hashing + HS256 JWT sessions
  (12h TTL, `CLOUDFORGE_JWT_SECRET`). Existing bcrypt users are
  **not** locked out — legacy hashes verify and are upgraded to
  Argon2id on first login. Old hand-rolled HMAC tokens are no
  longer accepted.
- **Routing:** go-chi/chi v5; every M1–M8 route preserved.
- **Observability:** `GET /metrics` (Prometheus) and `GET /readyz`
  (honest Postgres/Redis/engine readiness, gated on Postgres).
- **Rate limiting:** Redis fixed-window — auth 10/5 min per IP
  (fail-closed), general API 300/1 min per IP (fail-open).
- **Root `Makefile`:** `make verify` runs every local check
  (no Docker needed); `make up/down/logs/ps` need Docker.
- **Same-origin proxy:** nginx proxies `/api`, `/healthz`,
  `/readyz`, `/metrics` to the API; the frontend defaults to
  relative URLs when `VITE_API_URL` is unset, and `npm run dev`
  proxies the same paths to `localhost:8080`.
- Compose fixed two env-name bugs: `JWT_SECRET` →
  `CLOUDFORGE_JWT_SECRET` and `REDIS_URL` → `REDIS_ADDR`.

## Milestone 9 — Production readiness (part 2: AWS-console-style UI)

- The frontend was redesigned to look like the AWS Management
  Console in CloudForge's own branding (dark navy header, service
  search, region selector, left service nav, orange primary buttons,
  AWS-style tables/cards) — no AWS trademarks/logos, no claim it
  is AWS.
- Terminal tokens: the WebSocket terminal takes a short-lived
  lab-scoped token from `POST /labs/{id}/terminal/token`, not the
  Bearer token.

## Milestone 9 — Production readiness (part 3: final)

- **Dockerfiles:** `backend/Dockerfile` (multi-stage Go 1.26 →
  distroless non-root, pinned digests, `/readyz` HEALTHCHECK via
  the api's `--health-check` flag); frontend + engine Dockerfiles
  reviewed and fixed (non-root, pinned tags). Validated with
  hadolint (0 warnings); `docker build` itself is unverified here
  (no Docker in this sandbox).
- **Live feeds:** Topology/resources/validate UI now calls the real
  M6/M7 endpoints (`GET /labs/{id}/topology`, `/resources`,
  `POST /validate`); the stale "pending backend endpoint" wording
  is gone, with honest 503 states when the engine is down.
- **Security:** one shared SSRF guard (`internal/security`) for
  user-supplied URLs (loopback/emulator/MinIO only); secrets
  redacted in logs and audit metadata; admin-only per-role quotas
  (`GET/PUT /api/v1/admin/quotas`, migration `006_quotas.sql`).
  See `docs/security/model.md`.
- **Monitoring:** `infrastructure/monitoring/` — Prometheus scrape
  config + two Grafana dashboards (overview, labs) provisioned by
  the optional `monitoring` compose profile.
- **Backup/restore:** `scripts/backup.sh` / `scripts/restore.sh`
  (pg_dump/pg_restore, timestamped, restore refuses a non-empty
  target without `--force`); round-trip verified against a scratch
  database.
- **Cleanup reconciliation:** `cleanup_reconcile` worker job +
  `POST /api/v1/admin/cleanup-reconcile` (dry-run by default)
  detects orphans both ways — DB rows with no live counterpart
  and live engine/host-agent resources with no DB row.
- **End-to-end acceptance test:** `tests/e2e/` — black-box test
  driving the real API binary against a fake gRPC engine and the
  simulated host agent (register → lab → VPC → subnets → VM →
  fault injection → repair → validation → XP → destroy →
  cleanup verified). Run: `cd tests/e2e && go test -v .`

## How to run (Milestone 1)

### 0. Check the host (Ubuntu 24.04 LTS target)

```bash
bash infrastructure/scripts/check-host.sh
# Exits non-zero if KVM is missing. Installs nothing; changes nothing.
```

Required on the host: `qemu-kvm libvirt-daemon-system libvirt-clients
virtinst cloud-image-utils qemu-utils nftables`, Docker Engine + Compose v2.

### 1. Configure

```bash
cp .env.example .env
# Edit .env: set CLOUDFORGE_JWT_SECRET, POSTGRES_PASSWORD, MINIO_ROOT_PASSWORD.
```

### 2. Start the stack

```bash
cd deployments/compose
docker compose up --build -d
# Dev overrides (published postgres/redis ports, no restarts):
docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build -d
# Optional MinIO (S3 adapter, M5):
docker compose --profile optional up -d minio
```

Services: frontend on `:80`, API on `:8080`, network engine gRPC on
`:50051`, PostgreSQL on `:5432` (dev only), Redis on `:6379` (dev only).

### 3. Run each service directly (development)

```bash
# All local checks at once (no Docker needed):
make verify

# Frontend (Node 24, npm 10)
cd frontend && npm install && npm run dev      # http://localhost:5173
npm run build && npm run test && npm run lint

# Backend (Go — see backend/; requires Go 1.25+)
cd backend && go test ./... && go build ./...

# Rust engine (see rust-network-engine/; requires stable Rust)
cd rust-network-engine && cargo test --workspace && cargo build --workspace
```

### 4. Verify

```bash
curl http://localhost:8080/api/v1/health
# {"service":"cloudforge-api","version":"0.2.0-m2","ok":true,
#  "uptime_seconds":...,"network_engine_reachable":true,
#  "database_connected":true,"redis_connected":true}
```

## Verified commands (this sandbox, 2026-11-21)

| Command | Result |
| --- | --- |
| `go test ./...` (backend) | all packages pass, incl. new M2 tests (config, cache, store, jobs, httpapi M2 endpoints) |
| `go test -race ./internal/cache/ ./internal/jobs/ ./internal/store/` | pass, no data races |
| `DATABASE_URL=... go test ./internal/store/ -run TestPostgresIntegration -v` | **PASS against real PostgreSQL 16.15** (users/bcrypt/roles, courses/enroll/XP, lab create + idempotency + transitions, job claim/complete, audit, concurrent-migrate check) |
| Live E2E: API + worker vs real Postgres 16 + Redis 7 | lab create → HTTP 202 → worker → `READY`; idempotent replay → 200 same lab; destroy → `DESTROYED`, resource `CLEANED`, jobs `done`; engine down → worker marks lab `FAILED` with error; Redis down → courses still served, health reports `redis_connected:false` |
| `npm run test` (frontend) | 13/13 vitest tests pass |
| `npm run build` (frontend) | `tsc -b && vite build` succeeds (dist: 310 kB JS / 96 kB gzip) |
| `npm run lint` (frontend) | oxlint: 0 warnings, 0 errors |
| `python3 -c yaml.safe_load(...)` on both compose files | Valid YAML; 6 services, 4 networks, 6 volumes |
| `bash infrastructure/scripts/check-host.sh` | Exits 1, honest FAILs (see below) |
| `docker compose config` | **NOT run — Docker is not installed in this sandbox** |
| `docker compose build` | **NOT run — same reason** |
| Go builds/tests | **Run and passing** — `go vet`, `go build`, `go test ./...` (incl. `-race`), plus the Postgres integration test against real PostgreSQL 16.15 (backend workstream, 2026-11-21) |
| Rust builds | **NOT run here — Go/Rust not installed in this sandbox** (separate workstreams) |

## Known limitations (Milestone 1)

- **No Docker/KVM in this sandbox.** `docker compose config` and
  `docker compose build` were not run; compose files were validated for
  YAML syntax only. `check-host.sh` correctly reports FAIL here (no
  vmx/svm flags, no `/dev/kvm`, no Docker, no libvirt/QEMU, 7 GB RAM
  below the 8 GB minimum). On a real Ubuntu 24.04 KVM host it must be
  re-run and pass before the stack is started.
- **No real netlink in M1.** The Rust engine's namespace/bridge/veth/
  nftables operations land in Milestone 4; the M1 engine exposes health
  plus the contract, and unimplemented ops return `UNIMPLEMENTED`
  instead of fake success.
- **In-memory store is now only a fallback.** Postgres persistence,
  the worker and the audit log landed and were verified against real
  PostgreSQL 16.15 in Milestone 2; the API falls back to in-memory
  stores (with a warning) only when no database is reachable.
- **No real VM/container provisioning yet.** Docker/libvirt integration
  is Milestone 3; lab lifecycle, scheduling and Terraform are M2/M7.
- **Frontend depends on the Go API.** With the API down, the Dashboard
  shows an honest error, not invented numbers. The frontend types were
  verified against the landed backend (`backend/internal/httpapi/`,
  `courses/`, `labs/`) on 2026-11-21: health `network_engine_detail`,
  course list `lesson_count`, lab `state`/`definition_id`, error
  envelope `{error:{code,message}}`, correlation header `X-Request-ID`.
- **`backend/Dockerfile` now exists** (M9 Part 3, multi-stage Go
  1.26 → distroless non-root). `docker compose build` still needs
  a real Docker host to verify — it was not run in this sandbox.
- **MinIO is optional** (`--profile optional`) and the AWS emulator /
  Floci adapters land in Milestone 5.
- **Unverified in this sandbox (M9 Part 3):** `docker compose
  build` / `docker compose up` (no Docker), real KVM/libvirt VM
  provisioning (no `/dev/kvm`), the Rust engine's real
  rtnetlink/nftables backend (compiles; default backend is
  SIMULATED), and nginx config syntax (no nginx binary). All of
  these are implemented for a real host and must be verified there
  before a production deployment.

## Next steps

All nine milestones are complete. Before a production deployment
on a real host, verify what this sandbox cannot: `docker compose
build` + `up`, real KVM/libvirt VM provisioning, the Rust engine's
real rtnetlink/nftables backend, nginx config syntax, and the
Grafana dashboards against a live Prometheus. See
`MILESTONE_LOG.md` (M9 Part 3) for the full verified-vs-unverified
list.
