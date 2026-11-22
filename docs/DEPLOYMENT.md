# CloudForge Deployment

## Fresh-checkout checklist

1. Clone the repo and check the host: `scripts/check-host.sh`
   (full mode needs ≥8 GB RAM and ≥50 GB free disk; it fails below
   that. Recommended for comfort: 8+ cores, 24–32 GB RAM, 250 GB
   SSD — but 4 cores / 8 GB / 50 GB works for solo learning, one
   lab at a time, Micro/Small profiles).
2. Copy the environment file and fill in real secrets:
   `cp .env.example .env` — then set `CLOUDFORGE_JWT_SECRET`
   (≥32 chars), `CLOUDFORGE_ENGINE_TOKEN` (≥16), `CLOUDFORGE_METRICS_TOKEN`
   (≥16), `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` (not `minioadmin`),
   `GRAFANA_ADMIN_PASSWORD`. The API/worker refuse to start in
   non-dev mode without them, naming the variable
   (`docs/SECURITY.md`). `openssl rand -hex 32` generates values.
3. Build and start — full mode (default): `make up`. Lite mode
   (classroom laptop, no Prometheus/Grafana/MinIO, no KVM):
   `make up-lite` (sets `CLOUDFORGE_MODE=lite`). Stop with
   `make down` / `make down-lite`.
4. Verify: `curl http://localhost:8081/api/v1/health` should report
   `"ok":true` and `"lite":true` in lite mode. Open
   `http://localhost:8080/` and register the first user — the first
   user registered with `CLOUDFORGE_ADMIN_EMAIL` becomes admin
   (bootstrap admin; see `docs/security/model.md`).
5. Run the checks: `make verify` (see `docs/TESTING.md`).

## Ports

Frontend host `:8080` (container nginx `:8080`), API host `:8081`
(container `:8080`), worker `:8083`, host agent `:8082`, engine
gRPC `:50051` (not published on the host since Phase 7 — it is only
reachable inside the compose network). Do not change these bindings;
bookmarks and firewall rules from the old `:80`/`:8080` layout must
be updated to `:8080`/`:8081`. The API port must not be published
to the open internet.

## Compose files

| File | Purpose |
|---|---|
| `deployments/compose/docker-compose.yml` | Full mode |
| `deployments/compose/docker-compose.light.yml` | Lite mode (frontend, API, worker, engine, Postgres, Redis only) |
| `deployments/compose/docker-compose.tls.yml` | TLS override (opt-in, Phase 7) — replaces the frontend nginx config with `frontend/nginx.tls.conf`, host `:443` → container `:8443`, `:8080` redirects to HTTPS |
| `deployments/compose/docker-compose.dev.yml` | Dev overrides |

## TLS (opt-in)

1. `scripts/gen-tls-cert.sh` — creates a self-signed cert at
   `deployments/tls/fullchain.pem` + `privkey.pem` (0600, refuses
   to overwrite). Replace with a real cert for a shared host.
2. Set `CLOUDFORGE_TLS=1` in `.env` (enables API HSTS + Secure
   session cookies).
3. Start with the TLS override: `docker compose -f
   deployments/compose/docker-compose.yml -f
   deployments/compose/docker-compose.tls.yml up -d`.
4. HTTPS is on host `:443` deliberately: HSTS is host-wide, so
   HTTPS must be on the default port or a browser that learned
   HSTS would upgrade `http://host:8080` to `https://host:8080`
   and fail. **Unverified in the sandbox** (no Docker/nginx) —
   verify on a real host before relying on it.

## Resource limits

Full mode (added Phase 7; previously unlimited): API 512 MiB /
1.0 CPU, worker 256 MiB / 0.50, network engine 256 MiB / 0.50,
frontend 256 MiB / 0.50, Postgres 1 GiB / 1.0, Redis 256 MiB / 0.50.
Lite mode (unchanged): hard limits totalling 1,280 MiB, Postgres
64 MB buffers / 20 connections, Redis capped 128 MB with no
append-only. These limits have **never been run** (no Docker in
the sandbox) — they are sane on paper; watch `docker stats` on
first real deployment.

## Exam services in lite (v3, 2026-11-22)

The SAA-C03 / SAP-C02 exam services are available in lite mode with
no new containers, no compose changes and no new environment
variables. The 24 services that were catalogued `UNSUPPORTED` —
Auto Scaling, Global Accelerator, Direct Connect, Control Tower,
IAM Identity Center, RAM, Storage Gateway, Snowball, Lake
Formation, Macie, Compute Optimizer, Savings Plans, Audit Manager,
Artifact, Aurora, Backup, MGN, FSx, Shield, EBS, EFS, CloudFront,
VPN and NACLs — are now API-emulated (Backup runs are real
`pg_dump`; see `docs/SERVICE_FIDELITY.md` §6) at
`/api/v1/aws/<service>/...`, backed by one shared Postgres table
`p7_exam_services` (migration `014_exam_services.sql`, applied
automatically on startup by the embedded migrations — no operator
step). The full-mode compose file is untouched.

**Caps — what changed and why.** Exam-service records are tiny
Postgres rows (no container, VM or engine capacity), so the lite
cap is generous for study but bounds table growth on a classroom
laptop:

| Cap | Old value | New value |
|---|---|---|
| Exam-service records per user, per service (all kinds) | No exam services, no cap | Lite: learner 10, instructor 25, admin 50; full mode: uncapped |
| Labs (count / profile / duration) | 1 lab / Micro only / 30 min | Unchanged |
| Compose resource limits (lite, 1,280 MiB total) | As above | Unchanged |

A create over the cap returns 429 `quota_exceeded`. Two new
30-minute labs exercise the services in lite:
`saa-exam-services` (aws-saa) and `sap-exam-services` (aws-sap);
the SAA/SAP tracks each gained a 3-lesson "Exam Services in
Practice" module and 3 assessment questions (12 lessons / 8
questions each).

**Unchanged lite limits.** MinIO is still not in the lite compose
file, so S3 object operations still return an honest 503 in lite.
Backup runs need `pg_dump` on the API host and a configured
platform database (same dependency as RDS snapshots); without
them the job is recorded FAILED with the reason, never a fake
success. Ports are unchanged (frontend `:8080`, API `:8081`).

## Environment variables (Phase 7 additions)

`CLOUDFORGE_DEV=1` (explicit dev opt-in), `CLOUDFORGE_ENGINE_TOKEN`,
`CLOUDFORGE_METRICS_TOKEN`, `CLOUDFORGE_TLS=1`,
`CLOUDFORGE_TRUSTED_PROXIES` (IP/CIDR list),
`CLOUDFORGE_API_USER_RATE_LIMIT` / `_WINDOW` (default 300/1m),
`CLOUDFORGE_AUDIT_EXPORT_RETENTION` (default 7 jobs),
`CLOUDFORGE_TF_NETWORK_MODE` (`bridge` | `isolated`, default
`bridge`), `GRAFANA_ADMIN_PASSWORD`. Full list: `.env.example`.

## Backup and restore

`scripts/backup.sh [--dir DIR] [--db DATABASE_URL]` dumps Postgres
with `pg_dump -Fc`; `scripts/restore.sh --force [--db URL] FILE`
restores it (destructive for the target DB). Verified end-to-end
in Phase 7 (2026-11-22, sandbox): 248 KB dump, restored into a
scratch database — 3 users, 26 audit rows, 3 revoked tokens,
13 migrations, matching the live DB; scratch DB dropped afterwards.
Back up before upgrades; the audit archive (`audit-archive-<date>.csv`)
is written by the worker before the 90-day cleanup deletes rows.

## Troubleshooting

- API exits at startup naming a secret → set it in `.env`
  (fail-closed, Phase 7). Do not set `CLOUDFORGE_DEV=1` on a
  shared host to work around it.
- API exits with "curriculum directory could not be resolved" →
  `-seed-curriculum` requires the curriculum: run from the repo
  root or set `CURRICULUM_DIR` to `curriculum/`.
- Engine exits naming `CLOUDFORGE_ENGINE_TOKEN` → set the token
  (same value in the API/worker env and the engine env).
- Lab stuck in `CREATED` → the worker is not running or cannot
  reach the engine; check `worker` logs and
  `GET /api/v1/health` (`network_engine_grpc`).
- `/metrics` returns 401 → pass the token (`X-Metrics-Token` or
  `Authorization: Bearer <CLOUDFORGE_METRICS_TOKEN>`).
- `make verify` fails `TestLibvirtUnavailableInSandbox` /
  `TestInstanceCreateStartsVMAndRebootWorks` → a
  `/var/lib/cloudforge/labs` directory exists on a disk with
  <16 GiB free; remove it (the hostagent disk-capacity check
  enforces 16 GiB when the path exists). On a real host with
  ≥16 GiB free this does not trigger.
