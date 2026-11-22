# CloudForge Architecture

CloudForge is a locally deployable AWS-style cloud learning platform:
a Go control plane, a Rust network engine, and a React/TypeScript
frontend, run with Docker Compose (full mode) or directly from source
(lite mode, no Docker/KVM). Tagline: "Build Cloud Skills. Not Cloud
Bills." No AWS account, subscription, credit card, or external cloud
provider is involved. See `docs/architecture/overview.md` for the
deeper design notes; this file is the map.

## Components

| Component | Code | Runs on | Talks to |
|---|---|---|---|
| Frontend (React + TypeScript, Vite) | `frontend/` | host `:8080` (nginx `:8080` in Docker) | API over same-origin `/api` proxy |
| API (Go) | `backend/cmd/api` | host `:8081` (container `:8080`) | Postgres, Redis, engine gRPC `:50051`, worker `:8083`, host agent `:8082`, MinIO, emulator |
| Worker (Go) | `backend/cmd/worker` | host `:8083` | Postgres, Redis, engine gRPC, Terraform runner (Docker), host agent |
| Network engine (Rust) | `rust-network-engine/` | `:50051` (gRPC, token-authenticated since Phase 7) | kernel rtnetlink/nftables in `netlink` backend; in-memory model in `simulated` backend (default) |
| Host agent (Go) | `backend/cmd/hostagent` | `:8082` | libvirt/KVM (full mode) or simulated (lite) |
| Postgres 16 | compose service | `:5432` | — |
| Redis 7 | compose service | `:6379` | — |
| Prometheus + Grafana | compose service (full mode only) | `:9090` / `:3000` | API `/metrics` (token-authenticated since Phase 7) |
| MinIO (S3) | compose service (full mode only) | `:9000` | — |

## Request flow

1. The browser hits the frontend; the frontend calls `/api/v1/...`
   on the same origin. In Docker, nginx proxies `/api` to the API
   container. Running the API directly, it listens on `:8081`.
2. The API authenticates (HS256 JWT, `jti` + revocation denylist
   since Phase 7), authorises by role/ownership, rate-limits
   (per-IP + per-user, Phase 7), and serves from Postgres (durable)
   with Redis as cache/rate-limit store. If Postgres is unreachable
   the API falls back to in-memory stores with a warning — dev only;
   in compose, startup fails instead (see `docs/DEPLOYMENT.md`).
3. Lab lifecycle is asynchronous: `POST /api/v1/labs` returns 202
   with state `CREATED`; the worker picks the job up, asks the
   engine to provision the isolated network, asks the host agent to
   start VMs, and moves the lab to `READY`. Only `READY` means the
   lab actually works — nothing reports success before the operation
   succeeded (the READY rule, `docs/SERVICE_FIDELITY.md` §3).
4. The engine owns networks, subnets, NAT, firewall policies, and
   flow-log telemetry. Its gRPC channel is authenticated with a
   shared-secret token on metadata `x-cloudforge-engine-token`
   (Phase 7, R1); the old unauthenticated path refuses connections.
   The engine is no longer published on the host in compose.
5. Terraform runs execute in a Docker container (`--network none`
   in isolated mode, Phase 7) driven by the worker; the Terraform
   Learning UI is the learner-facing surface.

## Modes

- **Full (default):** `make up`. Lab server: Prometheus/Grafana/
  MinIO, KVM VMs, Terraform containers, real netlink engine backend
  available. Minimum 8 GB RAM / 50 GB disk (`scripts/check-host.sh`
  fails below that).
- **Lite:** `make up-lite` / `CLOUDFORGE_MODE=lite`. Classroom
  laptop: no Prometheus/Grafana/MinIO, tuned-down Postgres/Redis,
  simulated networking, no KVM. Hard limits total 1,280 MiB.
  Defaults: 1 lab, Micro only, 30-minute labs; longer labs are
  refused with 429. Lite and full use separate volumes/networks.

## Data model

Postgres is the source of truth. Key tables: `users` (+ `user_roles`,
`roles`), `lab_definitions` / `lab_sessions` / `lab_resources`,
`audit_logs`, `revoked_tokens` + `issued_tokens` (migration 013,
Phase 7), `quotas`, `p3_*` (Phase 3 services), `p4_*` (Phase 4),
`p5_tflearn_*` (Terraform Learning), `p6_*` (Phase 6 advanced
services), `schema_migrations` (13 migrations after Phase 7).
All schema changes are migrations in `backend/migrations/`; never
edit a table by hand — add a migration.

## Fidelity model

Every service is labelled REAL / API_EMULATED / SIMULATED / PARTIAL /
UNSUPPORTED (`docs/SERVICE_FIDELITY.md`). The UI shows the badge and
disables operations the backend cannot do. A simulator is never
promoted to REAL. This is a learning platform: where AWS would charge
or need a real account, CloudForge teaches the concept with a local
equivalent and says so on the surface.

## Ports (decided — do not change)

Frontend host `:8080` (container nginx `:8080`), API host `:8081`
(container `:8080`), worker `:8083`, host agent `:8082`, engine
gRPC `:50051`. TLS (opt-in, Phase 7) moves the frontend to host
`:443` → container `:8443`, with `:8080` redirecting to HTTPS.
