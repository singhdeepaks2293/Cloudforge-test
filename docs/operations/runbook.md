# CloudForge Operations Runbook (Milestone 9)

How to run, verify, monitor and fix the CloudForge stack. Written
for the local architecture (Go API + worker + host agent, Rust
network engine, PostgreSQL, Redis, React frontend behind nginx).
For the API contract see [docs/api/reference.md](../api/reference.md);
for the security model see [docs/security/model.md](../security/model.md).

## Quick start (real host)

```bash
# 0. Host readiness (Ubuntu 24.04 LTS target; installs nothing)
bash infrastructure/scripts/check-host.sh

# 1. Configure
cp .env.example .env
# Edit .env: set CLOUDFORGE_JWT_SECRET, POSTGRES_PASSWORD,
# MINIO_ROOT_PASSWORD. (M9 renamed JWT_SECRET → CLOUDFORGE_JWT_SECRET
# and REDIS_URL → REDIS_ADDR; the old names were never read.)

# 2. Start
docker compose -f deployments/compose/docker-compose.yml up -d --build
# or: make up   (from the repo root; fails clearly without Docker)

# 3. Verify
curl -s localhost/readyz | python3 -m json.tool   # via nginx
curl -s localhost:8080/api/v1/health | python3 -m json.tool
```

Services: frontend `:80`, API `:8080`, network engine gRPC
`:50051`, PostgreSQL `:5432` (dev only), Redis `:6379` (dev only).

## Verify without Docker (`make verify`)

```bash
make verify   # go vet + go test + cargo test + frontend build/lint/test
make backend-test backend-vet engine-test frontend-test frontend-build frontend-lint
make fmt      # gofmt + cargo fmt
```

`make verify` needs no Docker and no running services (Postgres /
Redis integration tests run when reachable, and skip with a clear
message otherwise — but a green `verify` with skipped integration
tests is not a green deployment; see "Pre-flight checks").

## Pre-flight checks (before calling a deployment healthy)

1. `curl localhost/readyz` → 200, `postgres: ok`, `redis: ok`.
   `engine: down` is acceptable if no labs need provisioning yet —
   but lab creation will fail honestly until it is up.
2. `curl localhost:8080/api/v1/health` → `database_connected:
   true`, `redis_connected: true`.
3. Register a user and log in (see API reference examples); the
   token must be a JWT (three dot-separated segments).
4. `curl localhost/metrics | grep cloudforge_http_requests_total`
   returns series after a few requests.
5. `docker compose ps` — all services `healthy` (api healthcheck
   is `/readyz`).

## Environment variables

The full list is in `backend/README.md` ("Environment variables").
The M9-relevant ones:

| Variable | Default | Notes |
|---|---|---|
| `CLOUDFORGE_JWT_SECRET` | `dev-only-change-me` | **Must be set** (32+ random hex chars). Startup warns when unset. Rotating it logs out every user. |
| `CLOUDFORGE_TOKEN_TTL` | `12h` | JWT lifetime. |
| `REDIS_ADDR` | `localhost:6379` | In compose: `redis:6379`. A wrong value breaks the cache (fail-open) **and** auth rate limiting (fail-closed → logins 503). |
| `CLOUDFORGE_AUTH_RATE_LIMIT` / `CLOUDFORGE_AUTH_RATE_WINDOW` | `10` / `5m` | Register/login attempts per client IP. |
| `CLOUDFORGE_API_RATE_LIMIT` / `CLOUDFORGE_API_RATE_WINDOW` | `300` / `1m` | General `/api/v1` requests per client IP. |
| `VITE_API_URL` | _(empty)_ | Leave empty for same-origin (nginx proxy). Set only for a split deployment. |
| `CURRICULUM_SEED` | `1` | Seed curriculum at startup; `POST /api/v1/admin/seed-curriculum` re-seeds a running API. |

## Interpreting `/readyz`

| Component | `ok` | `down` | `disabled` / `not_configured` |
|---|---|---|---|
| `postgres` | `SELECT 1` succeeded | Overall status becomes `not_ready`, HTTP **503**. Fix Postgres (see below). | No Postgres configured (in-memory fallback). Expected in dev-without-DB only; **not** acceptable in production. |
| `redis` | `PING` succeeded | Reported only. Cache + rate-limit counters are degraded; **logins return 503** (fail-closed auth limiting) while reads keep working (fail-open). | Redis not configured / unreachable at startup. Same operational effect as `down`. |
| `engine` | gRPC Health ok | Reported only. Lab provisioning/validation/topology fail honestly (502/503) until it recovers; auth/courses keep working. | No engine client configured. |

## Common incidents

### Logins return 503 `rate_limiter_unavailable`

Redis is unreachable from the api container. Check
`docker compose ps` (redis healthy?), `docker compose logs redis`,
and that `REDIS_ADDR=redis:6379` is set in the api environment
(compose sets it; a hand-rolled deployment must too). Reads still
work. Restarting Redis restores logins immediately — no api
restart needed (the limiter client dials lazily). If Redis will be
down for a while and logins are more important than brute-force
protection, that is an operator decision: there is no kill-switch
for the fail-closed behaviour; restarting the api without Redis
does not help (the limiter is built from `REDIS_ADDR` at startup).

### `/readyz` returns 503, `postgres: down`

Check `docker compose logs postgres`, disk space on the `pgdata`
volume, and that `DATABASE_URL` credentials match
`POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`. The api falls
back to in-memory stores only at **startup** when Postgres is
unreachable; a Postgres that dies later makes Postgres-backed
requests fail — restart the api after Postgres recovers.

### `429 rate_limited` on register/login

The client IP hit the auth budget (10/5 min). This is per IP, so
a classroom behind one NAT shares it. Check whether it is abuse
(`docker compose logs api` — look for `rate_limited` + the IP) or
a shared-IP classroom; raise `CLOUDFORGE_AUTH_RATE_LIMIT` or the
window if it is the latter. `Retry-After` tells the client when
to retry.

### Frontend shows "API unreachable"

1. `curl localhost/api/v1/health` — if this works, the nginx proxy
   is fine and the problem is the browser (check `VITE_API_URL`:
   it must be empty in the built image for same-origin).
2. If it fails, `curl localhost:8080/api/v1/health` — if that
   works, nginx is the problem (`docker compose logs frontend`,
   check `frontend/nginx.conf` proxy locations).
3. If that fails, the api is down — see Postgres/Redis above.

### Engine down (`network_engine_grpc: false`)

Lab provisioning, topology, validation and break-and-fix fault
injection fail honestly (502/503) until it recovers. Check
`docker compose logs network-engine`; on this sandbox the engine
runs SIMULATED and needs a real KVM host for real networking (see
`infrastructure/scripts/check-host.sh`).

### A user cannot log in after the M9 upgrade

M9 did **not** invalidate passwords: bcrypt hashes still verify
and are upgraded on first login. If a user cannot log in, check:
(1) they are using the right password (the error is deliberately
generic); (2) their account exists in the **Postgres** users table
(in-memory fallback users do not survive a restart); (3) Redis is
up (see the 503 case above). Old M2 HMAC tokens are invalid —
users must log in again for a fresh JWT.

## Routine operations

- **Logs:** `make logs` (all services) or
  `docker compose logs -f api`.
- **Stop/start:** `make down` / `make up` (volumes are kept).
- **Rotate the JWT secret:** set a new `CLOUDFORGE_JWT_SECRET` in
  `.env`, `docker compose up -d api` (only the api validates
  tokens). **All users must log in again.** There is no
  dual-secret period.
- **Rotate DB/MinIO passwords:** change in `.env` and the service,
  restart the stack.
- **Re-seed curriculum:** `POST /api/v1/admin/seed-curriculum`
  (admin token) or restart the api with `-seed-curriculum`.
- **Backups:** `scripts/backup.sh` / `scripts/restore.sh` (see
  "Backup and restore" below). Redis is cache + rate limits +
  terminal tokens — no backup needed.
- **Quotas:** `GET /api/v1/admin/quotas` to inspect, `PUT` to
  change per-role limits (see "Quotas" below).
- **Cleanup reconciliation:** `POST /api/v1/admin/cleanup-reconcile`
  (see "Cleanup reconciliation" below); the worker also runs it
  as a `cleanup_reconcile` job.
- **Metrics:** scrape `http://<api>:8080/metrics` (or via nginx
  at `/metrics`). Alert on `cloudforge_network_engine_up == 0`
  and on rising `cloudforge_http_requests_total{status="503"}`.
  Optional Prometheus + Grafana: `docker compose --profile
  monitoring up -d` (dashboards in `infrastructure/monitoring/`).

## Backup and restore (M9 Part 3)

Postgres only — Redis holds no durable data.

```bash
# Back up (timestamped file under ./backups, keeps the newest 7):
DATABASE_URL=postgres://cloudforge:<password>@127.0.0.1:5432/cloudforge \
  scripts/backup.sh
# --dir and --dry-run supported; BACKUP_DIR / BACKUP_KEEP env vars.

# Restore into an empty database (refuses a non-empty target
# unless --force is given):
DATABASE_URL=postgres://cloudforge:<password>@127.0.0.1:5432/cloudforge \
  scripts/restore.sh --force backups/cloudforge-20261121T180000Z.dump
```

Stop the api and worker before restoring into a live database,
and let the api re-run migrations at startup afterwards (it does
so automatically). The backup is a `pg_dump -Fc` file, so
`pg_restore --list` can inspect it. The restore drops and
recreates CloudForge objects in the target database only.

## Quotas (M9 Part 3)

Per-role limits (labs, CPU, memory, disk, lab hours) live in the
`quotas` table (migration `006_quotas.sql`, seeded with learner
3 labs / 4 CPU / 8 GiB / 50 GB / 8 h, instructor 5 / 8 / 16 GiB /
100 GB / 12 h, admin 10 / 16 / 32 GiB / 200 GB / 24 h).

```bash
# Inspect (admin token required):
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  localhost:8080/api/v1/admin/quotas
# Change one role (partial update; 0 = unlimited, except
# max_labs 0 = the built-in default of 3):
curl -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"role":"learner","max_labs":5}' \
  localhost:8080/api/v1/admin/quotas
```

Enforcement: lab creation is refused with `429 quota_exceeded`
when the role's active-lab count or the definition's duration
exceeds the limit; instance creation is refused the same way when
the VM profile (cpu/mem/disk) exceeds the role's limits. The
store-level ceiling is a safety backstop set to the highest
configured role limit.

## Cleanup reconciliation (M9 Part 3)

Detects orphaned lab resources in both directions — DB
`lab_resources` rows with no live counterpart (engine/host-agent
says the resource is gone) and live engine subnets/networks or
host-agent VMs with no DB row — and, in enforcement mode, removes
the orphaned side with audit events. **Dry-run is the default.**

```bash
# Dry-run (default): report only, nothing is removed.
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"dry_run": true}' \
  localhost:8080/api/v1/admin/cleanup-reconcile
# One lab only: {"lab_id": "<id>", "dry_run": true}
# Enforce: {"dry_run": false} — deletes the orphaned side.
```

An unreachable engine or host agent is reported in the report's
`errors` list and its resources are **skipped, never deleted**:
reconcile never treats "cannot check" as "gone". The worker runs
the same code as a `cleanup_reconcile` job (payload
`{lab_id?, dry_run?}` — absent `dry_run` means dry-run). To
schedule it, enqueue the job type `cleanup_reconcile` with
`dry_run=false` once the dry-run reports have been reviewed.

## Known limitations

- No Docker/KVM in this sandbox: `docker compose config/build/up`
  were never run here; compose files were validated for YAML
  syntax only, and the nginx config was manually reviewed
  (`nginx -t` needs nginx or Docker). Docker-based verification
  is pending on a real host.
- `backend/Dockerfile` exists (M9 Part 3, multi-stage Go 1.26 →
  distroless non-root, `/readyz` HEALTHCHECK). `docker compose
  build` is still unverified in this sandbox (no Docker).
- The sandbox has no Docker, KVM, or `/dev/kvm`: real VMs,
  containers and kernel networking are built for a real host,
  gated behind capability checks, and unverified here; the
  engine's default backend is SIMULATED.
