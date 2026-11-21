# Lightweight mode (education / classroom laptops)

CloudForge has two modes. **Lite is an option alongside full mode —
it does not replace it.** Full mode (the default) is unchanged: KVM
VMs, Terraform containers, real netlink, Prometheus/Grafana/MinIO.
Lite is the same code with a trimmed stack and enforced small
defaults, for a machine that cannot run the full lab server.

|  | Lite | Full (default) |
| --- | --- | --- |
| Start | `make up-lite` | `make up` |
| Compose | `deployments/compose/docker-compose.light.yml` | `deployments/compose/docker-compose.yml` |
| Mode switch | `CLOUDFORGE_MODE=lite` | `CLOUDFORGE_MODE=full` (or unset) |
| Hardware | 2–4 cores / 4 GB RAM / 20 GB disk | 8+ cores / 24–32 GB RAM / 250 GB SSD |
| KVM | not needed | required (`/dev/kvm`, libvirt) |
| Services | frontend, api, worker, network-engine, postgres, redis | + prometheus, grafana, minio, aws-emulator |
| Resource limits | yes, every service (see below) | none |
| Labs | 1 active lab, Micro only, 30 min | admin quota (default 3 labs, 8 h) |
| Networking | simulated only | simulated or real netlink |
| Monitoring | health endpoint only | Prometheus + Grafana |
| Terraform | containers only (needs Docker); no KVM VMs | containers + KVM VMs |

## What lite is

The same Go control plane, Rust network engine and React frontend,
running with:

- a **trimmed compose file** — no Prometheus, Grafana, MinIO or AWS
  emulator, and a worker service the full compose file does not
  define (lab provisioning/destroy/expiry are worker jobs; without
  it a lite lab would sit in `CREATED` forever);
- **per-service `deploy.resources.limits`** enforced by the Docker
  daemon (cgroups) — a runaway service is killed, it does not take
  the host down;
- **tuned-down Postgres and Redis** (see below);
- **simulated networking** (`NETWORK_ENGINE_BACKEND=simulated`,
  the engine default) — the lite file carries **no capabilities**;
  full mode is the only file that grants `NET_ADMIN` to the engine;
- **lite quota defaults enforced in code**, not just in docs:
  `CLOUDFORGE_MODE=lite` (or `CLOUDFORGE_LITE=true`) makes the API
  seed and cap quotas at learner 1 lab / 1 CPU / 1024 MiB / 8 GB /
  30 min, instructor 2 / 1 / 1024 / 8 / 60 min, admin 3 / 2 / 2048 /
  16 / 120 min (`backend/internal/quota.LiteLimits`, capped again
  in `httpapi` so a database seeded with full defaults is still
  capped). Running the API with the lite flag outside Docker gives
  the same limits.

## Resource table

Compose limits (ceilings, not usage):

| Service | Memory limit | CPU limit | Why |
| --- | --- | --- | --- |
| api | 256 MiB | 0.50 | Go control plane; measured ~90 MiB RSS idle + request bursts. 256 MiB leaves room for the Argon2id login path. |
| worker | 128 MiB | 0.25 | Same binary, mostly idle between 2 s job polls; measured ~21 MiB. |
| network-engine | 128 MiB | 0.25 | Rust engine, simulated backend holds topology in memory only; measured ~6 MiB. |
| frontend | 128 MiB | 0.25 | nginx serving static files (measured ~10–20 MiB in practice). |
| postgres | 512 MiB | 0.50 | The biggest consumer; also tuned down internally (below). |
| redis | 128 MiB | 0.25 | Cache + rate-limit counters only; `--maxmemory 128mb` + LRU matches the limit, so it evicts instead of being OOM-killed. |
| **Platform total** | **1,280 MiB** | **2.00** | **≈ 1.25 GiB of limits** |
| Per-lab Micro | ~512 MiB–1 GiB | 1 vCPU | The Micro profile (1 vCPU / 1 GiB RAM / 8 GB disk). |

Postgres low-memory settings (lite compose `command`):
`shared_buffers=64MB, work_mem=4MB, maintenance_work_mem=64MB,
max_connections=20, effective_cache_size=256MB`. Stock Postgres
defaults assume a server; these keep the database small while still
running all migrations + the curriculum seed.

Redis lite command: `redis-server --appendonly no --maxmemory 128mb
--maxmemory-policy allkeys-lru`. Nothing durable lives in Redis, so
there is no AOF and no disk write.

## Measured memory (this sandbox, 2026-11-21)

Measured live on a 4-core / 7.7 GiB Ubuntu sandbox (the same class
of machine lite targets), with the lite stack run manually — API +
worker + Rust engine + frontend dev server against the host's
Postgres 16.15 / Redis 7.0.15, `CLOUDFORGE_MODE=lite`:

| Process | RSS |
| --- | --- |
| api (compiled binary; `go run` parent excluded) | ~90 MiB |
| worker | ~21 MiB |
| network-engine (debug build) | ~6 MiB |
| postgres (all processes, **stock host settings**, not lite-tuned) | ~144 MiB |
| redis | ~6 MiB |
| vite dev server | ~200 MiB |
| **Platform total, excluding the dev server** | **~267 MiB** |

`free -m` before the stack: 7,934 MiB total, 1,110 MiB available.
After the stack (incl. vite): 854 MiB available. The box was
already heavily loaded by other work (6.8 GiB used before the
stack started, 366 MiB of swap in use), so the `free` delta is
noisy — the per-process RSS numbers above are the reliable ones.

Honest caveats on these numbers:

- The vite dev server (~200 MiB) is **dev-only**. In Docker the
  frontend is nginx serving static files under a 128 MiB limit;
  expect ~10–20 MiB, not 200 MiB.
- The host Postgres measured here ran with **stock settings**
  (shared_buffers 128 MB); the lite compose file tunes it down
  further, so Docker Postgres should use less than the 144 MiB
  measured here — but that was not measured in Docker, because
  **there is no Docker in this sandbox** (`docker compose config`
  / `docker compose up` were never run; the lite file was validated
  by YAML parse only).
- The `go run` parents (~35 MiB each) are dev-only; the Docker
  image runs the compiled binaries.
- **Per-lab Micro memory (~0.5–1 GiB) was not measured live.**
  The lab ran end-to-end (see below) but with the simulated host
  agent, so no real container/VM was started. The figure is the
  Micro profile's allocation, not a measurement.
- **Minimum hardware (2–4 cores / 4 GB RAM / 20 GB disk) is an
  extrapolation, not a verified run:** measured platform ~0.3 GiB
  + one Micro lab ~1 GiB + OS ~1 GiB ≈ 2.3 GiB, which fits 4 GiB
  on paper, but this was never run on a 4 GiB machine. Test on the
  actual classroom hardware before a rollout.

## What is disabled / changed vs full mode

- No Prometheus, Grafana, MinIO, AWS emulator.
- No KVM VMs. Labs are containers-first with the simulated host
  agent; a KVM VM is a full-mode feature.
- No Terraform containers without Docker (and no KVM-backed
  Terraform applies in lite).
- No real netlink / nftables networking — simulated backend only.
- Only 30-minute lab definitions are usable in lite: `ec2-basics`
  and `iam-security` (30 min). `vpc-fundamentals` (60 min) and
  `three-tier-app` (120 min) are **refused with 429
  `quota_exceeded`** in lite mode — this is enforced, not just
  documented. A second concurrent lab is also refused (1-lab cap).
- Single-user / small-class assumptions: one learner at a time per
  laptop is the design point. Shared-classroom use is one lite
  instance per laptop, not one lite server for a whole class.
- Lite and full use **separate volumes**
  (`cloudforge_lite_pgdata` / `cloudforge_lite_redisdata` vs the
  full-mode volumes). Do not point both modes at the same volume:
  the schema is the same but the quota rows differ (lite seeds
  1-lab/Micro/30-min limits at startup).

## Run instructions (lite)

```bash
cp .env.example .env
# Edit .env: set CLOUDFORGE_JWT_SECRET, POSTGRES_PASSWORD.
# (MINIO_ROOT_PASSWORD is not needed in lite.)
# The template defaults to CLOUDFORGE_MODE=full; the lite compose
# file sets CLOUDFORGE_MODE=lite / CLOUDFORGE_LITE=true for its
# services either way, so no .env edit is needed to go lite.
make up-lite
# or: docker compose -f deployments/compose/docker-compose.light.yml up -d --build
open http://localhost          # frontend on :80
curl http://localhost:8080/api/v1/health   # expect "lite":true
make logs-lite   # follow logs
make down-lite   # stop (volumes are kept)
```

To run the API directly without Docker (development):

```bash
CLOUDFORGE_MODE=lite DATABASE_URL=postgres://cloudforge:cloudforge@localhost:5432/cloudforge?sslmode=disable \
  NETWORK_ENGINE_ADDR=localhost:50051 CLOUDFORGE_JWT_SECRET=... \
  go run ./cmd/api   # from backend/
```

`CLOUDFORGE_LITE=true` is accepted as an alias for
`CLOUDFORGE_MODE=lite`. Setting both to different modes
(`MODE=full` + `LITE=true`, or `MODE=lite` + `LITE=false`) is a
startup error — the API refuses to start rather than silently
running the wrong mode. An unknown mode (`MODE=medium`) is also
rejected.

## Verified live (2026-11-21, this sandbox)

With `CLOUDFORGE_MODE=lite` against live Postgres/Redis and the
simulated engine:

- `/api/v1/health` reports `"lite":true`, engine
  `backend=SIMULATED`, database + Redis connected.
- Migration `007_lite_lab_minutes.sql` applied; quota rows seeded
  learner 1/1/1024/8/30 min, instructor 2/1/1024/8/60 min, admin
  3/2/2048/16/120 min.
- Registered a learner; `vpc-fundamentals` (60 min) → **429**;
  `ec2-basics` (30 min) → created and worker-provisioned to
  **READY** in ~6 s; a second lab → **429**; destroy → 200.
- Frontend dev server served HTTP 200 on `:5173`.
- `go test ./...` (backend, 19 packages) pass; frontend
  `npm run build` + 51/51 tests + `npm run lint` (0 warnings)
  pass; `gofmt`/`go vet` clean.
- Full compose file **untouched** (mtime 2026-11-21 18:08, before
  this work; `docker-compose.yml` still defines api, frontend,
  network-engine with `NET_ADMIN`, postgres, redis, prometheus,
  grafana, minio — no limits added, no service removed).
- `make -n up-lite` / `make -n up` resolve to the correct compose
  files; `docker compose config` could not run (no Docker here).

## What remains unverified

- `docker compose config` / `docker compose up -d` for the lite
  file (no Docker in this sandbox) — the file is YAML-valid and
  follows the full file's structure, but it has never been booted
  by Docker.
- Real Docker memory under the cgroup limits (the limits are
  ceilings; actual container RSS was not measured).
- A 4 GiB laptop run (see the extrapolation caveat above).
- KVM is not required in lite and was not tested in lite; KVM
  behaviour is unchanged in full mode.
