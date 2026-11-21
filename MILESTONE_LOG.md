# CloudForge Milestone Log

> No MILESTONE_LOG.md existed before 2026-11-21; entries below start
> at Milestones 6–7 (backend). Earlier milestones are recorded in
> `backend/README.md` and the per-milestone notes in memory, not here.

## Milestone 6 (backend) + Milestone 7 (Terraform) — 2026-11-21

Backend only (`backend/`); `frontend/`, `rust-network-engine/`,
`proto/`, `curriculum/`, `deployments/`, `docs/` untouched.

### Milestone 6 — lab resources, topology, validation, terminal

- `GET/POST /api/v1/labs/{id}/resources`, `DELETE
  /api/v1/labs/{id}/resources/{resourceId}` — real resources from
  the store (Postgres `lab_resources` + in-memory fallback), with
  host-agent VMs merged into the list. Kinds validated
  (`vpc|subnet|instance|gateway|loadbalancer|route-table|security-group`).
  Subnet creation calls the Rust engine `CreateSubnet` (the lab
  network is created lazily from the lab VPC CIDR when the engine
  has none, retried once); instance creation goes through the
  host-agent `CreateVM`; engine/host-agent failure is a 502 with
  the structured engine error. Delete is idempotent.
- `GET /api/v1/labs/{id}/topology` — engine `InspectTopology`
  merged with DB resources; engine unreachable → honest 503 with
  capability info (frontend shows 503 as a real failure, which is
  correct; 404/501 remain the "pending endpoint" signal).
- `POST /api/v1/labs/{id}/validate` — new `internal/validation`
  engine over real state (DB resources, engine topology,
  host-agent VMs). Implemented: `lab_exists`, `vpc_exists`,
  `subnet_count`, `subnet_exists`, `route_to_igw`, `route_to_nat`,
  `nat_exists`/`nat_gateway_exists`, `security_group_allows`,
  `security_group_allows_ssh`, `instance_running`, `dns_resolves`
  (always `unknown` — no in-VPC DNS resolver yet). Per-rule
  `{rule, status pass|fail|unknown, detail}` plus overall `passed`
  and `xp_eligible`; no XP is awarded (M8). Response also carries
  the frontend's `rule_id`/`passed`/`message` fields.
- Terminal: `POST /api/v1/labs/{id}/terminal/token` (5 min,
  single-use, lab+user bound; Redis when available, in-memory
  fallback; only the SHA-256 hash persisted) and `WS GET
  /api/v1/labs/{id}/terminal` (`golang.org/x/net/websocket` —
  already in the module graph, no new dependency; `nhooyr` and
  `gorilla` deliberately not used). Auth is the token query param
  only — the Bearer token in a URL is rejected. Resize/ping/pong
  handled; the shell runs only via host-agent `ExecInLab`
  (allowlisted commands); simulated mode is a clearly-labelled
  simulated shell, never a host shell. `terminal_sessions` rows
  record start/end/used/mode.
- Migration `004_terminal_terraform.sql` extends the tables 002
  created (no rewrite): `lab_resources` gains
  `name/parent_id/private_ip/public_ip/cidr`;
  `terraform_runs` gains plan/approval/job columns;
  `terminal_sessions` gains `ended_at/used/mode`.

### Milestone 7 — Terraform

- New `internal/terraform`: `Runner` interface, `ContainerRunner`
  (Docker Engine API over the Unix socket, pinned image
  `hashicorp/terraform:1.9.8`; `opentofu/opentofu:1.9.2` documented
  alternative via `TERRAFORM_IMAGE`; lab workdir is the only host
  mount, 1 vCPU / 512 MiB) and `TestStubRunner` (tests only,
  results labelled `Stub=true`). Terraform never runs on the host.
- Endpoints (all async jobs, 202 with run id): `POST
  .../terraform/init|validate|plan|approve|apply|destroy`, `GET
  .../terraform/runs`. Plan JSON is parsed from `terraform show
  -json` and validated (`ParsePlanJSON`); apply requires an
  explicit approval of a succeeded, unexpired plan (1h TTL) —
  no approval → 409, expired → 409, unknown → 404 — and the worker
  syncs created resources into `lab_resources`; destroy removes
  the lab's emulator-managed resources. New job types
  `terraform_init/validate/plan/apply/destroy` driven by the
  worker; a failed run is a terminal `FAILED` (job completes),
  except Docker `Unavailable`, which retries.
- Provider isolation: per-lab workdir + generated `provider.tf`
  pointing at the emulator/MinIO endpoints only; `ValidateEndpoint`
  SSRF-guards endpoints to loopback; non-emulator AWS credentials
  in the environment are refused; HCL referencing
  `amazonaws.com` or embedding keys is rejected. Env:
  `TERRAFORM_IMAGE`, `TERRAFORM_WORKDIR_ROOT`,
  `CLOUDFORGE_LAB_ROOT`.

### Verification (2026-11-21)

- `gofmt`, `go vet ./...`, `go build ./...`, `go test ./...` — all
  clean, all packages pass (new tests: validation, terraform
  incl. plan parsing / approval gating / expiry / destroy /
  run history / ContainerRunner-unavailable / SSRF + credential
  validation, terminal tokens + simulated shell, httpapi M6/M7,
  jobs terraform worker path).
- Postgres integration (`TestPostgresIntegration`,
  `TestPostgresM6M7Integration`) passes against Postgres 16 +
  Redis 7 on localhost; migration 004 applied live.
- Live API smoke test against Postgres 16 + Redis 7 and the real
  Rust engine (SIMULATED backend) on `:50051`: subnet create via
  the engine (201, network auto-created), topology merged
  engine + DB nodes (200), validate passed all 5
  vpc-fundamentals checks (`passed: true`, `xp_eligible: true`),
  subnet delete via the engine (200), terminal token issued with
  `redis_backed: true`, Terraform plan → 503
  `terraform_unavailable` (no Docker socket) with no run row
  created. Two bugs found by the smoke test were fixed in the
  same pass: `InspectTopology` needs a non-empty idempotency key,
  and subnet creation failed `NOT_FOUND` because lab provisioning
  never creates an engine network (now created lazily).
- `go.mod` unchanged in versions: grpc v1.78.0, protobuf v1.36.12,
  Go 1.25; `golang.org/x/net` promoted from indirect to direct
  (already in the graph) for the terminal WebSocket.

### Known limitations

- No Docker in this sandbox, so Terraform container execution is
  implemented but unverified live; all Terraform actions honestly
  return 503 here. The container uses bridge networking with
  `host.docker.internal:host-gateway` so the emulator stays
  reachable — the "no network except the emulator endpoint" rule
  is approximated there, not enforced by a network namespace.
- Frontend `Terminal.tsx` still passes the long-lived Bearer token
  as the WebSocket token; the backend correctly rejects it — the
  frontend must call `POST .../terminal/token` first (M6 frontend
  follow-up, out of scope for the backend).
- `dns_resolves` is always `unknown` (no in-VPC DNS resolver
  wired); validation rules needing AWS-provider state (buckets,
  queues, Lambda, …) report `unknown`.
- The simulated shell has no pty (`resize` is acknowledged only);
  Docker host-agent exec returns exit code/message only (output
  discarded by the agent), so the real shell is terse.
- The worker's expiry sweep / provisioning do not create an engine
  network; subnet creation creates it lazily instead (see above).

## Milestone 8 — Curriculum backend (seed + API + XP + Break-and-Fix) — 2026-11-21

Backend only (`backend/` + `MILESTONE_LOG.md`); `frontend/`,
`rust-network-engine/`, `proto/`, `curriculum/`, `deployments/`,
`docs/` untouched. No frontend change was needed: the M6 note
about `Terminal.tsx` passing the Bearer token as the WebSocket
token is still open on the frontend side (out of scope here, as
instructed).

### Curriculum seed

- New `internal/curriculum`: YAML loader (`gopkg.in/yaml.v3`
  v3.0.1, the version that installs for Go 1.25) for
  `curriculum/<track>/*.yaml`, `curriculum/break-and-fix/*.yaml`,
  `curriculum/achievements.yaml`, plus an idempotent seeder
  (upserts keyed on track id, module position, lesson slug,
  question index, lab-definition id, achievement id, scenario id)
  and the YAML-fault → engine-`FaultSpec` mapping
  (`route-blackhole`→5/1, `latency`→1/`latency_ms`,
  `loss`→2/`loss_percent`, `dns`→4/100). `CURRICULUM_DIR` env
  (auto-resolves `../curriculum` / `curriculum` / binary-relative
  paths; an explicitly set directory that cannot be resolved fails
  startup with the tried paths named). Seeding runs at API startup
  when `CURRICULUM_SEED=1` (default on), via the
  `-seed-curriculum` flag, or via admin
  `POST /api/v1/admin/seed-curriculum`.
- Seed counts (shipped content): **14 tracks, 70 lessons, 84
  assessment questions, 6 break-and-fix scenarios, 15
  achievements**, 34 lab definitions (guided + challenge per
  track + 6 scenarios). Verified live against Postgres 16.15:
  17 courses (14 tracks + 3 M1), 86 lessons, 84 questions,
  17 achievements.
- Migration `005_curriculum.sql`: course
  `difficulty`/`estimated_hours`/`xp_reward`; lesson
  `slug`/`theory`/`key_points`/`diagram`; lab-definition
  `validation_rules`/`tasks`/`kind`/`fault`/`hints`/`fix_steps`/
  `xp_reward`; achievement `trigger` JSONB; new `assessments`,
  `assessment_submissions`, `lab_completions`,
  `break_fix_attempts` tables.
- **M1 alignment verified, content unchanged:** `vpc-fundamentals`
  (6) → `networking`, `iam-security` (5) → `iam`, `ec2-basics`
  (5) → `ec2`, same lesson order/topics (titles differ only in
  capitalisation). The M1 course rows are untouched; curriculum
  tracks are new course rows.

### API

- `GET /api/v1/curriculum/tracks` (public; progress per track when
  authed), `GET /api/v1/curriculum/tracks/{id}` (full track incl.
  theory/key points/diagrams; assessment questions without
  `correct_answer`), `POST .../tracks/{id}/enroll`,
  `POST /api/v1/curriculum/lessons/{lessonId}/complete` (slug or
  UUID; enrolled only), `POST
  /api/v1/curriculum/assessments/{trackId}/submit` (server-side
  grading against DB answers; score, per-question
  correct/incorrect + explanation; 20 XP only on the first pass
  at >= 80%), `GET /api/v1/users/me/progress` (per-track %,
  total XP, streak), `GET /api/v1/users/me/achievements` (earned +
  locked with `locked_reason`).
- `GET /api/v1/labs/{id}` now includes `validation_rules` (plus
  `scenario_id`/`fault_status` when a break-and-fix attempt
  exists) when the lab was created from a curriculum lab
  definition; the fields are omitted otherwise, so existing
  clients are unaffected. `POST /labs/{id}/validate` defaults to
  the definition's seeded rules for curriculum labs.
- XP still flows only through `xp_transactions`: 10 XP/lesson
  (+10 first-lesson bonus); a track's `xp_reward` is split as
  10/lesson plus the remainder on the final lesson, so a completed
  track awards exactly `xp_reward` (networking: 250). Double-award
  prevention: `user_progress` PK `(user_id, lesson_id)` for
  lessons, plus per-reason guards for assessments and break-fix
  solves. Pre-M8 courses (`xp_reward = 0`) pay no bonus, so M2
  behaviour is unchanged.

### Achievements + streaks

- New `internal/achievements`: trigger evaluation on every
  XP/progress event (lesson completion, assessment pass, passing
  lab validation, break-and-fix solve). **All 15** shipped
  triggers are observable and live (`lessons_completed`,
  `labs_completed`, `challenges_completed`, `break_fix_solved`,
  `track_completed`, `tracks_completed`, `assessment_score`,
  `xp_total`, `streak_days`). The M2 legacy achievements
  (`first-lesson`, `vpc-builder`) have no curriculum trigger and
  are never auto-awarded by the evaluator (`first-lesson` is
  still awarded directly by the M2 lesson path); the YAML
  `first-lab` overwrote the M2 `first-lab` row on seed.
- Streak: consecutive UTC calendar days with lesson/lab/assessment
  activity, computed from progress timestamps (a streak ending
  yesterday still counts). A passing validation records a lab /
  challenge completion (`lab_completions`, idempotent per
  user+lab); break-and-fix labs are completed only via the solve
  endpoint.

### Break-and-Fix

- Scenarios seeded as lab definitions `break-fix-<id>` (kind
  `break-fix`) with fault specs. `POST
  /labs/{id}/break-fix/start {scenario_id}` calls the engine
  `InjectFault`; engine unreachable → the attempt is
  `fault_pending` and the lab is honestly reported as created
  without the fault. `POST /labs/{id}/break-fix/solve` runs the
  scenario's rules through the shared validation path and awards
  the scenario XP + achievements only when every rule passes
  (otherwise `solved: false` with the checks).

### Validation engine (M8 additions)

- New real-state rules: `no_route_to_internet`,
  `security_group_denies`, `load_balancer_healthy_targets`
  (recorded `healthy_targets` in the LB resource config),
  `instance_reachable` (running instance with a recorded address),
  `dns_resolves` (VPC `dns_enabled`/`dns_hostnames` config, else
  the control-plane resolver for public names only — internal
  names are never asked of the host resolver, after the sandbox
  resolver was found to answer `example.internal`; unknown when
  there is no DNS source at all), `metric_below` (metrics recorded
  in a resource's `config.metrics`; unknown without a metrics
  pipeline). The two M6 tests asserting `dns_resolves` is always
  unknown were updated to the new semantics.

### Verification (2026-11-21)

- `gofmt`, `go vet ./...`, `go build ./...`, `go test ./...` —
  all clean, all 17 test packages pass (new: `curriculum`,
  `achievements`, `store` seed/grading/double-XP/break-fix tests,
  `validation` new-rule tests, `httpapi` curriculum flow test,
  Postgres integration `TestPostgresCurriculumIntegration`).
  Postgres integration passes against Postgres 16.15 + Redis
  7.0.15 on localhost; migration 005 applied live.
- Live API smoke test (binary built from this code, Postgres 16.15
  + Redis 7, no Rust engine): startup seed logged 14/70/84/6/15;
  register → tracks 17 → networking detail (6 lessons, theory
  167 words, no `correct_answer` leak) → enroll → lesson complete
  (first-steps awarded) → assessment 100/20 XP → progress
  (total_xp 165, streak 1) → achievements (earned: exam-ace,
  first-lesson, first-steps; 14 locked) → lab from
  `lab-networking-vpc` (`validation_rules`: 6) → break-fix start
  (`fault_pending`) → solve fails before the fix → route-table +
  load-balancer resources recorded → solve passes (+120 XP,
  total 385 incl. breaker +100).
- Note: this sandbox's VM was replaced mid-milestone (system Go,
  Postgres and Redis vanished); they were reinstalled from the
  image's apt cache / direct download (Postgres 16.15, Redis
  7.0.15) and the Go 1.26.0 toolchain from the module cache was
  used to build/test (go.mod still targets Go 1.25). The earlier
  full-suite run while Postgres was down had skipped the
  integration tests; the final run above re-ran them for real.

### Known limitations

- No live metrics pipeline: `metric_below` is unknown unless a
  resource records metrics in its config; that is what makes the
  `latency-spike` scenario solvable only after such a recording.
- `dns_resolves` is not an in-VPC lookup.
- The in-memory curriculum store's streak is best-effort (lesson
  completions are not timestamped in the memory course store).
- The admin re-seed endpoint was verified through `SeedDir` in
  tests, not live against the running API.
- Frontend `Terminal.tsx` Bearer-token issue remains open on the
  frontend side (unchanged by this milestone).

## Milestone 9 (part 1) — Production-readiness backport — 2026-11-21

Backports the GitHub MVP's foundation (Argon2id/JWT, chi, Prometheus,
rate limiting, root Makefile, same-origin proxy, security/API/runbook
docs) onto the local M5–M8 architecture. Done one step at a time, each
verified before the next. Baseline before any change: `go test ./...`
all 17 test packages pass; Postgres 16 + Redis 7 up on :5432/:6379.
Environment note: /usr/local/go is gone in this sandbox; the Go 1.26.0
toolchain in the module cache was used with `GOTOOLCHAIN=local`
(`go.mod` still targeted Go 1.25 at the start — see the forced bump
below). `rust-network-engine/`, `proto/` and `curriculum/` untouched.

### 1. Auth — Argon2id + HS256 JWT, dual-verify migration

- New: `backend/internal/auth/password.go` — Argon2id PHC hashes
  (`$argon2id$v=19$m=65536,t=3,p=2$`, 64 MiB / 3 iter / 2 lanes,
  16-byte salt, 32-byte key), `VerifyPassword` accepting Argon2id and
  legacy bcrypt (`$2a$`/`$2b$`/`$2y$`), `NeedsRehash`.
- `auth.go`: `HashPassword` is now Argon2id; `IssueToken` /
  `ValidateToken` are HS256 JWTs via `golang-jwt/jwt/v5` v5.3.1
  (claims `sub`/`iss: cloudforge`/`iat`/`exp`, 12h TTL unchanged,
  `CLOUDFORGE_JWT_SECRET` unchanged, `WithValidMethods(["HS256"])`
  so `none`/foreign-algorithm tokens are rejected, expiry maps to
  `ErrTokenExpired`). No new password-hash column: `password_hash`
  is `TEXT` and holds the longer PHC string.
- Dual-verify: a successful login with a bcrypt hash re-hashes to
  Argon2id in place — memory store (`auth.Store.Authenticate`) and
  Postgres (`PostgresUserStore.Authenticate`, best-effort `UPDATE`
  that never fails the login); a failed login never re-hashes.
  Tests: old-hash login, new-hash login, wrong password (both
  formats), expired/invalid/tampered JWT, wrong-secret and `none`
  tokens, plus a Postgres integration test that downgrades a hash
  to bcrypt, logs in, and asserts the stored hash is Argon2id.
- Breaking change (tokens only): M2 hand-rolled HMAC tokens are no
  longer accepted; users must log in again. Passwords unaffected.

### 2. Routing — go-chi/chi v5

- `go-chi/chi v5.5.0` replaces the stdlib ServeMux in
  `internal/httpapi` only (worker/host-agent muxes untouched).
  All 51 M1–M8 routes preserved verbatim (same methods, paths,
  `{id}`/`{resourceId}`/`{lessonId}`/`{trackId}`/`{service}`
  params, now read with `chi.URLParam`), same X-Request-ID/logging
  middleware order, same error envelope and auth/ownership checks.
  All existing httpapi tests pass unmodified (tokens are opaque to
  them).

### 3. Observability — Prometheus + /readyz

- New `backend/internal/metrics`: `GET /metrics` with
  `cloudforge_http_requests_total{method,route,status}`,
  `cloudforge_http_request_duration_seconds{method,route}`,
  `cloudforge_network_engine_up`. Route label is the chi route
  pattern, never the raw path — no user-identifying/secret labels.
  `/metrics` itself is excluded from the counters. The status
  recorder delegates `Flush`/`Hijack` so the terminal WebSocket
  keeps working behind the metrics middleware.
- `GET /readyz` (`handleReadyz`, `DBHealth` probe wired in
  `cmd/api` from `database.DB.Health`): live-checks Postgres
  (`SELECT 1`), Redis (`PING`) and the engine (gRPC Health) on
  every call, per-component `status`/`latency_ms`/`error`.
  Overall readiness is gated only on Postgres when configured
  (503 `not_ready` while down); Redis/engine are reported but do
  not gate (both are fail-open by design); unconfigured Postgres
  reports `disabled` and does not gate. `GET /healthz` unchanged.

### 4. Rate limiting — Redis fixed-window

- New `backend/internal/ratelimit`: atomic Lua `INCR` + `PEXPIRE`,
  keys `ratelimit:<scope>:<client-ip>` (IP = first `X-Forwarded-For`
  entry, else peer address), tight 200ms client timeouts.
- Auth (`register`/`login`): 10 attempts / 5 min per IP,
  **fail-closed** — Redis down → 503 `rate_limiter_unavailable`.
  General `/api/v1`: 300 requests / 1 min per IP, **fail-open** —
  Redis down → request proceeds with a warning log. Over-limit →
  429 `rate_limited` + `Retry-After`; `/healthz`, `/readyz`,
  `/metrics` never limited. Config:
  `CLOUDFORGE_AUTH_RATE_LIMIT` / `CLOUDFORGE_AUTH_RATE_WINDOW`,
  `CLOUDFORGE_API_RATE_LIMIT` / `CLOUDFORGE_API_RATE_WINDOW`.
  Nil limiter = disabled (unit tests, Redis-less dev).
- Tests assert both behaviours: limit triggers (auth + general),
  per-IP budgets, auth 503 when Redis is down, general API 200
  when Redis is down.

### 5. Root Makefile

- New repo-root `Makefile` (modelled on the GitHub MVP's, adjusted
  to local paths): `verify` (go vet + go test + cargo test +
  frontend build/lint/test), `backend-test`, `engine-test`,
  `frontend-test`, `frontend-build`, `frontend-lint`, `fmt`, `up`,
  `down`, `logs`, `ps`, `clean`, plus `backend-build`,
  `backend-vet`, `frontend-install`, `proto`, `help`.
  `up`/`down`/`logs`/`ps` fail with a clear message when Docker is
  absent (verified); `verify` needs no Docker. Go is taken from
  PATH, else the module-cache toolchain, else a clear error
  (`GO=/path/to/go` override supported); `GOTOOLCHAIN=local`.

### 6. Same-origin proxy

- `frontend/nginx.conf`: proxies `/api`, `/healthz`, `/readyz`,
  `/metrics` to `api:8080` (Docker embedded DNS `127.0.0.11`,
  per-request resolution via a variable, so recreating the api
  container needs no nginx restart); dedicated regex location for
  `/api/v1/labs/{id}/terminal` with WebSocket Upgrade headers and
  3600s timeouts. SPA fallback and `/assets/` caching kept.
- `frontend/src/services/api.ts`: `VITE_API_URL` unset/empty now
  means same-origin relative URLs (was `http://localhost:8080`);
  explicit `VITE_API_URL` still honoured; terminal WS URL is
  relative in same-origin mode. `frontend/vite.config.ts` proxies
  `/api`, `/healthz`, `/readyz`, `/metrics` to `localhost:8080`
  under `npm run dev` and `vite preview`.
- Compose: frontend builds with empty `VITE_API_URL` by default
  and waits for api healthy; api healthcheck is now `/readyz`.
  Two env-name bugs fixed: `JWT_SECRET` →
  `CLOUDFORGE_JWT_SECRET` and `REDIS_URL` → `REDIS_ADDR` (neither
  old name was ever read by config — the API in Docker silently
  used the dev JWT default and `localhost:6379` for Redis, which
  with fail-closed auth limiting would have 503'd every login).
  `.env.example`, `frontend/.env.example` and
  `frontend/Dockerfile` updated to match.

### 7. Docs

- New `docs/security/model.md` (Argon2id/JWT, dual-verify, roles/
  ownership, chi, rate-limit policy, observability, secrets,
  host agent, Terraform, rotation, limitations),
  `docs/api/reference.md` (full endpoint reference incl. `/readyz`,
  `/metrics`, rate limits, error codes, examples) and
  `docs/operations/runbook.md` (deploy, `make verify`, pre-flight
  checks, `/readyz` interpretation, incidents, routine ops).
- `backend/README.md` (M4–9, env vars, `/readyz`+`/metrics` rows,
  Argon2id register/login rows, M9 section) and root `README.md`
  (M9 section, `CLOUDFORGE_JWT_SECRET`, `make verify`) updated.
  API version bumped `0.8.0-m8` → `0.9.0-m9`.

### Verification (2026-11-21)

- `gofmt -l` clean; `go vet ./...` clean.
- `go test ./...` (with `DATABASE_URL` set, integration tests
  live): **19 packages pass** (auth 21.0s, httpapi 17.8–28.3s,
  store 12.1s incl. Postgres integration, providers 9.4s,
  ratelimit 1.3s, rest < 3s). New tests: auth dual-verify/JWT
  (7 new), httpapi `/readyz`+`/metrics` (5) and rate limiting
  (5), ratelimit package (4), Postgres dual-verify integration.
- `cargo test --workspace`: **74 passed, 0 failed**.
- Frontend: `npm run build` passes, `npm test` **32/32 pass**,
  `npm run lint` (oxlint) 0 warnings/errors.
- `DATABASE_URL=... make verify`: **exit 0** (~346s) — go vet +
  go test + cargo test + frontend build/lint/test.
- Live smoke test (real API binary vs Postgres 16 + Redis 7,
  engine down — honest reporting):
  - `GET /healthz` → `{"status":"ok"}`; `/api/v1/health` →
    `version 0.9.0-m9`, `database_connected: true`,
    `redis_connected: true`, `network_engine_grpc: false`.
  - `GET /readyz` → 200 `ready`
    (`postgres: ok, redis: ok, engine: down` with the dial error).
  - Fresh register → JWT (3 segments, `iss: cloudforge`,
    exp−iat = 12h), stored hash `$argon2id$`.
  - Bcrypt-era user (hash `$2a$12$…` inserted directly in the
    DB) logged in → 200 + JWT, stored hash now `$argon2id$`.
  - Crafted old-style HMAC token → 401.
  - `GET /metrics` → real series, e.g.
    `cloudforge_http_requests_total{method="POST",route="/api/v1/auth/login",status="200"} 1`,
    `cloudforge_network_engine_up 0`.
  - Rate limit: 10 bad logins from one X-Forwarded-For IP →
    401 each, 11th → **429**.
  - Vite dev proxy: `npm run dev` on :5173 proxied
    `/api/v1/health` and `/readyz` to the live API; SPA `/` → 200.
  - Smoke users deleted afterwards (DB back to 17 users).

### Known limitations

- No token revocation: a stolen JWT is valid until expiry (≤ 12h).
- Rate limiting is per IP: a classroom behind one NAT shares a
  budget; `X-Forwarded-For` is trusted from the proxy.
- `go.mod` now targets **Go 1.26.0** — forced by
  `prometheus/client_golang v1.25.0`, whose `go.mod` requires
  Go ≥ 1.26 (same for `prometheus/common`, `x/sys`, `x/text`).
  The sandbox's cached 1.26.0 toolchain was used.
- No Docker/KVM/nginx in this sandbox: `docker compose
  config/build/up`, `docker compose build` on `api`, and
  `nginx -t` were **not** run — the nginx config was manually
  reviewed and Docker-based verification is pending on a real
  host. `backend/Dockerfile` still does not exist, so
  `docker compose build` fails on `api` until it lands
  (pre-existing M1 note).
- The engine's default backend remains SIMULATED here; real
  netlink/nftables work is unverified in this sandbox.
- Frontend `Terminal.tsx` Bearer-token issue remains open on the
  frontend side (unchanged by this milestone).
- `make verify` takes ~6 minutes in this sandbox (dominated by
  the Go suite: Argon2id hashing + httpapi tests).

## Milestone 9 (part 2) — AWS-console-style frontend redesign — 2026-11-21

Redesigns the CloudForge frontend (`frontend/` only, plus this log) to
look like the AWS Management Console — **AWS-console-STYLE in
CloudForge's own branding, not AWS itself**: the header says
CloudForge, the tagline is "Build Cloud Skills. Not Cloud Bills.",
no AWS trademarks, logos or brand assets are used, and nothing claims
this is AWS. No backend, Rust engine, proto, curriculum, deployment
or docs files were touched. M9 Part 1's nginx.conf / vite proxy /
same-origin API client changes were preserved untouched.

### Before / after

- Before: slate sidebar layout (white sidebar, dark "Dashboard /
  Learn / Courses / Labs" links), ad-hoc per-page styling, no header
  search, no region selector, Labs as cards, Terminal passing the
  long-lived Bearer token as the WebSocket token (backend rejects it).
- After: dark navy (#232f3e) header on every page with CloudForge
  wordmark, Services grid menu, wide "Search services" input,
  `cf-local-1 (Local)` region selector, alerts bell and user menu with
  sign-out; collapsible left service nav (Compute / Networking /
  Storage / Databases / Security / Monitoring / Learn) with lucide
  icons and orange left-border + orange text on the active item;
  light-grey (#eaeded) content area, white 1px-border cards, orange
  (#ff9900) primary buttons, blue (#0073bb) links, AWS-style tables
  (grey header row, row borders, filter input, footer count), green /
  amber / red status pills, 14px base / 12px table text.

### Theme + shared components (all in `frontend/src/`)

- `index.css` — Tailwind v4 `@theme` tokens: `--color-aws-navy
  #232f3e`, `--color-aws-orange #ff9900`, `--color-aws-link #0073bb`,
  `--color-aws-bg #eaeded`, plus success/warning/error tones; system
  sans stack, 14px base.
- `services/catalog.ts` — the local service catalog the header search
  and nav are built from (EC2, VPC, S3, IAM, Lambda, RDS, …), each
  mapped to a real local route; `searchServices()` (case-insensitive,
  capped at 8), `LOCAL_REGIONS` (`cf-local-1`, `cf-local-2` — local
  regions only, no real AWS regions), recently-visited services in
  localStorage.
- `components/AwsHeader.tsx` — navy header; search navigates to the
  first matching local service on Enter; region choice persists in
  localStorage; user menu signs out via `clearToken()`.
- `components/AwsNav.tsx` — collapsible nav; active item gets the
  orange border/text; `/`-route entries use `end` so they do not
  highlight on every page. `ServiceIcon` resolves catalog icon keys
  to lucide components.
- `components/AwsTable.tsx`, `AwsCard.tsx` (card + breadcrumb +
  page header), `AwsButton.tsx` (orange primary), `AwsBadge.tsx`
  (state pill: running green, pending amber, stopped/failed red).
- `components/Layout.tsx` — the shell (header + collapsible nav +
  `#eaeded` content); still exports the legacy `Card` / `ErrorBox` /
  `Loading` / `StatusBadge` helpers so existing pages keep compiling.
- `lucide-react ^1.77.0` added for service icons — no emoji as UI icons.

### Pages (routes and API calls unchanged; no data invented)

- `/` Dashboard → Console Home: "Welcome to CloudForge" hero,
  "Recently visited services" tiles, "Build a solution" /
  getting-started cards, and widgets with real local data only —
  live control-plane health, real lab count (signed-in users),
  real course count/list. No fake costs or AWS numbers.
- `/labs` → EC2-style "Instances" table: Instance ID, Name, Instance
  state (pill), Instance type, Private/Public IP, Launch time,
  Actions (Open workstation / Topology), with filter input. IPs and
  type come from the lab's real `resources` when present, else "—".
- `/courses` → course catalogue as an AWS-style table with filter;
  `/learn` → tracks in AWS cards; `/console` → AWS-style tabs
  (Overview / Topology nodes) with the topology table; `/labs/:id/topology`
  → topology inside an AWS card with AWS toolbar; `/labs/:id/workstation`
  → same 3-pane layout with AWS header, tabs (Topology / Terminal),
  orange buttons and AWS cards; `/login`, `/register` → AWS cards with
  orange primary buttons. Honest "not yet available" messages are kept
  in AWS styling wherever they existed.

### Terminal fix (the known M6 frontend gap — now closed)

`components/Terminal.tsx` no longer passes the long-lived Bearer
token as the WebSocket token. Every (re)connect first calls
`POST /api/v1/labs/:id/terminal/token` (`requestTerminalToken` in
`services/api.ts`) and uses the returned short-lived token for
`buildTerminalWsUrl`. Terminal tokens are single-use, so reconnects
(and Retry) request a fresh token each time instead of reusing one.
`services/api.ts` comments updated to match (the old "token comes
from localStorage / endpoint pending" text was stale — the backend
landed the token + WS routes in the M6/M7 backend step).

### Files changed / created

Created: `src/services/catalog.ts`,
`src/services/catalog.test.ts`, `src/components/AwsHeader.tsx`,
`src/components/AwsNav.tsx`, `src/components/AwsTable.tsx`,
`src/components/AwsCard.tsx`, `src/components/AwsButton.tsx`,
`src/components/AwsBadge.tsx`, `src/components/aws.test.tsx`,
`src/pages/console-home.test.tsx`.
Changed: `src/index.css`, `src/components/Layout.tsx`,
`src/components/Terminal.tsx`, `src/components/TopologyCanvas.tsx`
(AWS toolbar/banners), `src/pages/Dashboard.tsx`,
`src/pages/Labs.tsx`, `src/pages/Courses.tsx`,
`src/pages/Learn.tsx`, `src/pages/Console.tsx`,
`src/pages/Topology.tsx`, `src/pages/LabWorkstation.tsx`,
`src/pages/Auth.tsx`, `src/App.tsx` (404 styling only),
`src/services/api.ts` (terminal-token comments only),
`package.json` (+ `lucide-react`), `frontend/README.md`.
Untouched as required: `backend/`, `rust-network-engine/`,
`proto/`, `curriculum/`, `deployments/`, `docs/`,
`frontend/nginx.conf`, `frontend/vite.config.ts`
(proxy preserved; `api.ts` still defaults to same-origin relative
URLs when `VITE_API_URL` is unset).

### Verification (2026-11-21)

- `npm run build` — `tsc -b && vite build`, passes (`✓ built in
  ~2.5–4.3s`; pre-existing "use no memo" rolldown note only).
- `npm run test` — vitest, **51 passed (5 files), 0 failed** (was
  32 before this milestone). New: 8 catalog tests (search
  navigation incl. EC2→/labs, VPC→/console; active-nav behaviour),
  10 AWS component tests (header branding/search/region, active-nav
  orange highlight, table rendering/filter/empty state, state pills,
  orange primary button), 2 page render tests (Console Home renders
  live health + real course; Instances table renders a real lab row
  with state pill and private IP).
- `npm run lint` — oxlint, **0 warnings, 0 errors** (28→29 files).
- Live dev-server check: Go API started from the cached 1.26.0
  toolchain (`GOTOOLCHAIN=local`, Postgres 16 + Redis 7 on
  :5432/:6379, engine down as expected in this sandbox) and
  `npm run dev` on :5173. SPA routes `/`, `/learn`, `/courses`,
  `/labs`, `/console`, `/login`, `/register`,
  `/labs/:id/workstation`, `/labs/:id/topology` all → 200; the Vite
  proxy served real data (`/api/v1/health` → `cloudforge-api`
  v0.9.0-m9, `ok: true`; `/api/v1/courses` → real catalogue;
  `/readyz` → `ready` with postgres/redis ok, engine down).
- Terminal path verified live against the running API: registered a
  throwaway user, created lab `9c2af1d7-…` (`vpc-fundamentals`),
  `POST …/terminal/token` returned a 64-hex short-lived token
  (different from the Bearer JWT), a raw WebSocket dial with it got
  `101 Switching Protocols` + the "CloudForge lab terminal —
  SIMULATED SHELL" banner, and the old behaviour (Bearer token as
  WS token) got `401 Unauthorized` — confirming the old code would
  fail and the new code works. Test lab destroyed afterwards
  (`DESTROYING`) and the throwaway user deleted from Postgres.
- No live browser was available in this sandbox, so the redesign was
  judged by code + SSR render tests, not by eye in a browser: the
  visual match to the AWS console still needs a human look at
  `npm run dev`. No console errors were observable without a browser.

### Known limitations

- No live-browser visual check in this sandbox (see above); the
  AWS-console likeness has not been eyeballed yet.
- The workstation's topology/terminal panes are now behind AWS-style
  tabs (Topology / Terminal) instead of both being visible at once;
  the 3-pane layout (instructions / centre / resources) is unchanged.
- The topology/resources/validate backend routes have landed since
  M6, but the frontend still shows the M6 "pending backend endpoint"
  wording for those three in TopologyCanvas / LabWorkstation — only
  the terminal wording was updated, since only the terminal flow was
  in scope here. Wiring the live topology/resources/validate feeds is
  a follow-up, not a placeholder.
- The lab created for the live terminal check stays in `DESTROYING`
  (no worker was running to complete the destroy); its owner user
  was deleted. User count is 20 after cleanup (was 17 at the end of
  M9 Part 1 — other agents may have created users meanwhile; only
  this milestone's throwaway user was deleted here).
- The engine's default backend remains SIMULATED here; real
  netlink/nftables work is unverified in this sandbox.

## Milestone 9, Part 3 — Production Readiness (final) — 2026-11-21

All nine milestones are now complete. Work was done in steps 1–8
in order, each verified before the next began. Baseline before any
change: `go test ./...` all backend packages passing.

### 1. Dockerfiles

- **Created `backend/Dockerfile`** (multi-stage): builder
  `golang:1.26.0-bookworm` digest-pinned (digest verified live via
  the Docker Hub registry API), runtime
  `gcr.io/distroless/static-debian12:nonroot` (tag only — the
  digest could not be resolved through the sandbox proxy; the
  Dockerfile documents pinning it in CI via `docker buildx
  imagetools inspect`). Builds both `/app/api` and `/app/worker`
  (CGO_ENABLED=0, `-trimpath`, `-ldflags="-s -w"`), copies CA certs
  from the builder, `USER nonroot:nonroot`, EXPOSE 8080,
  HEALTHCHECK `["/app/api", "--health-check"]`, no secrets.
- **`cmd/api/main.go`**: new `--health-check` flag (probes
  `GET http://127.0.0.1:<port>/readyz`, exit 0 only on HTTP 200;
  port from `CLOUDFORGE_ADDR`/`PORT`/8080). Verified live: built
  `/tmp/cf-api`, ran on PORT=18099, `/readyz` returned ready,
  `--health-check` exited 0 live and 1 with nothing listening.
- **Created `backend/.dockerignore`** (`.git`, `*.md`, `*_test.go`,
  `.env*`).
- **Compose fixes (`deployments/compose/docker-compose.yml`,
  YAML re-validated):** api healthcheck now uses the flag
  (distroless has no wget/curl); network-engine build context is
  the repo root (the old context would have failed the
  Dockerfile's repo-root-relative `COPY` steps); network-engine
  healthcheck is a TCP probe — the old
  `["CMD", ".../network-engine", "--health-check"]` could never
  pass because the Rust binary ignores argv; frontend runs nginx
  as `nginx` on port 8080.
- **Frontend Dockerfile:** runtime `nginx:1.27.0-alpine` (pinned,
  was unpinned `1.27-alpine`), `chown` of nginx dirs,
  `USER nginx`, EXPOSE 8080. `frontend/nginx.conf`: `listen 8080`,
  `pid /tmp/nginx.pid;` at main level.
- **Dev compose:** api volume mount `../../backend:/src:ro`
  (mounting over `/app` would hide the compiled binary).
- **hadolint 2.12.0** (downloaded to /tmp, installed cleanly):
  all three Dockerfiles pass, exit 0, no warnings.
- `docker build` was NOT run (no Docker in this sandbox).

### 2. Frontend live-feed wiring

- `Topology.tsx` / `LabWorkstation.tsx` / `TopologyCanvas.tsx`
  now call the live M6/M7 endpoints (`GET /labs/{id}/topology`,
  `/resources`, `POST /validate`). The stale M6 "pending backend
  endpoint" wording is gone: TopologyCanvas has a live green
  banner, an amber error box on feed failure (falling back to the
  lab's recorded resources), and a real add-resource form
  (create/delete call the API; host-agent-merged nodes cannot be
  deleted from the canvas). LabWorkstation has a live resources
  list (loading/error/empty states) and a validation panel with
  real check results. Engine-down stays an honest 503.
- Verified: `npm run build` ✓, `npm test` **51/51 pass (5 files)**,
  `npm run lint` (oxlint) **0 warnings, 0 errors**.

### 3. Security review + quotas

- **New `backend/internal/security/` (stdlib only):**
  `ValidateEndpointURL` / `ValidateEndpointHost` — http(s) only,
  no embedded credentials, host must be `localhost`,
  `127.0.0.0/8`, `::1`, or an exact-match allowed host (e.g.
  Docker service name `minio`); rejects `localhost.localdomain`,
  `127.0.0.1.evil.com`, RFC1918, `169.254.169.254`, `0.0.0.0`,
  ftp, empty. `RedactString` / `RedactURL` / `ScrubMap` /
  `Secret` (slog `LogValuer` rendering `<redacted>`).
- **SSRF wiring:** `terraform.ValidateEndpoint` delegates to the
  shared guard (loopback-only preserved); `providers.NewS3Adapter`
  validates the host (`minio` allowed); `providers.ProbeEmulator`
  refuses non-loopback before any request; `providers.NewAWS`
  fail-fasts on bad DynamoDB/emulator endpoints at startup.
  The host-agent address is deliberately NOT guarded
  (operator-configured, may legitimately be a LAN/KVM-host
  address, never user-derived).
- **Secrets redaction:** audit found `config.String()` already
  redacts DSN passwords and the HTTP logger records method/path
  only; the gap was audit metadata recorded verbatim — fixed
  (`httpapi.audit()` now scrubs via `security.ScrubMap`).
  Residual accepted risk: job-failure log lines include error
  strings, but runner errors never include credentials (refused
  by the SSRF guard pre-request).
- **Quotas (admin-only, per-role):** `quota.Limits{MaxLabs,
  MaxCPU, MaxMemoryMB, MaxDiskGB, MaxLabHours}` with
  `DefaultLimits` (learner 3/4/8192/50/8h, instructor
  5/8/16384/100/12h, admin 10/16/32768/200/24h); `quotas` table
  (migration `006_quotas.sql`, seeded) + `QuotaStore`
  (memory + Postgres); `GET/PUT /api/v1/admin/quotas` (partial
  update, per-value caps: max_labs 100, max_cpu 64,
  max_memory_mb 262144, max_disk_gb 2000, max_lab_hours 168).
  Enforced at lab creation (active count + definition duration)
  and instance creation (profile cpu/mem/disk) with `429
  quota_exceeded`; the store ceiling (`LabStore.SetMaxLabs`) is a
  backstop set to the highest configured role limit. Verified
  live: migration applied to the real Postgres; psql showed the
  three seeded rows.
- **Docs:** findings + fixes appended to
  `docs/security/model.md`, including audit-log retention (90 days
  via the worker `audit_cleanup` job; no archival — operators must
  snapshot `audit_logs`, e.g. via the backup script, first).

### 4. Monitoring

- **New metrics (`internal/metrics`):**
  `cloudforge_host_agent_up` (set by `/api/v1/health`),
  `cloudforge_lab_events_total{event}` (lab_created /
  lab_destroyed), `cloudforge_jobs_total{type,result}`.
- **`infrastructure/monitoring/`:** `prometheus.yml` (15s scrape;
  active job `cloudforge-api` → `api:8080`; engine + host-agent
  jobs commented out with the reason — neither exposes `/metrics`
  yet, their health is visible via the API-exported gauges),
  `grafana-cloudforge-overview.json` (7 panels: request rate by
  route, latency p50/p95, 5xx rate, engine up, host-agent up,
  worker jobs/min by result, lab events/min),
  `grafana-labs.json` (5 panels), Grafana provisioning
  (datasource `prometheus` → `http://prometheus:9090`, dashboard
  provider). All JSON/YAML validated as parsing. Image tags
  verified live via the Docker Hub registry API
  (`prom/prometheus:v3.3.0`, `grafana/grafana:11.5.2` → 200).
- **Compose:** prometheus + grafana services added under the
  `monitoring` profile (`docker compose --profile monitoring up
  -d`); volumes `prometheusdata`/`grafanadata`.
- **Bug found during live verification:** `SetEngineUp` was only
  called from `/readyz`, so `cloudforge_network_engine_up` went
  stale when operators polled `/api/v1/health` (the endpoint the
  dashboard's health panel implies). Fixed: `handleHealth` now
  also calls `metrics.SetEngineUp(grpcOK)`. Verified live: with
  the fake engine + host agent up, `/api/v1/health` then
  `/metrics` shows `cloudforge_host_agent_up 1` and
  `cloudforge_network_engine_up 1`; `lab_events_total{event=
  "lab_created"} 1` after a lab create. (Counters only appear in
  `/metrics` after first use — standard Prometheus behaviour for
  `CounterVec` children.)

### 5. Backup/restore

- **Created `scripts/backup.sh` / `scripts/restore.sh`**
  (`set -euo pipefail`): `pg_dump -Fc` / `pg_restore`,
  env-driven `DATABASE_URL`, timestamped files under `BACKUP_DIR`
  (default `./backups`, keeps newest `BACKUP_KEEP=7`), `--help` /
  `--dry-run` on both (dry-run redacts the password); restore
  refuses a non-empty target without `--force`.
- **Real round-trip verified** against a scratch database
  (`cloudforge_backup_test`, created as the `postgres`
  superuser — the `cloudforge` user has no CREATEDB): backup of
  the live DB (136 KB dump) restored exactly — 21 users, 3 quota
  rows, 24 tables, matching the source. Restore into the
  non-empty scratch DB without `--force` refused (exit 1, clear
  message); with `--force` succeeded (exit 0). Scratch DB dropped
  and temp files removed afterwards.

### 6. Cleanup reconciliation

- **New `store.JobCleanupReconcile = "cleanup_reconcile"`** and
  `LabStore.ListAll` (memory + Postgres).
- **`internal/jobs/reconcile.go`:** `Service.Reconcile(ctx, labID,
  dryRun)` + `ReconcileAll`, `Reconciler` interface,
  `EngineTopology` interface (`jobs.NewEngineAdapter` adapts the
  gRPC client). Detects orphans both ways per lab: DB subnet/VPC/
  instance rows with no live counterpart, and live engine
  subnets/networks + host-agent VMs with no DB row. DB-only kinds
  (gateway, loadbalancer, route-table, security-group) can never
  be orphans. A destroyed lab's DB rows are not orphans; its live
  resources are. **Dry-run is the default** (endpoint and job
  payload both default `dry_run` to true); enforcement deletes
  the orphaned side (DB rows, engine subnets/networks, host-agent
  VMs) and audits every removal plus a summary event. An
  unreachable backend is reported in `report.errors` and its
  resources are skipped, never deleted. Wired into the worker
  (`jobs.Service.Engine`) and the API (handler builds a
  `jobs.Service` from the server's own wiring when no explicit
  `Reconciler` is injected).
- **`POST /api/v1/admin/cleanup-reconcile`** (admin-only):
  `{lab_id?, dry_run?}`; empty `lab_id` reconciles all labs.
- **Tests:** 7 jobs tests with fake engine/host-agent stores
  (healthy lab, both-way detection in dry-run, enforcement
  removes + audits, engine-down skips engine checks, destroyed
  lab, job defaults to dry-run, reconcile-all) + 2 endpoint tests
  (403/401/500 handling, per-lab report with engine errors).
  All pass.

### 7. End-to-end acceptance test

- **`tests/e2e/`** (new module, stdlib-only, black-box): builds
  the real `api` binary and `backend/cmd/e2e-fakes` (fake gRPC
  network engine + simulated host agent — new test-support
  binary), starts them as processes, and drives the real HTTP API:
  register → login → open the VPC Fundamentals course ("VPC &
  Networking") → start lab (`vpc-fundamentals`, READY) → create
  VPC (10.0.0.0/16) → two subnets (10.0.1.0/24, 10.0.2.0/24) →
  NAT gateway + security group (SSH 22) → launch VM (simulated
  host agent, verified present via ListVMs) → terminal token
  (differs from the Bearer token) → topology (7 nodes) → break a
  route (`missing-igw-route`, fault_status `injected`) → diagnose
  (solve → `solved:false`) → repair (route-table 0.0.0.0/0 →
  igw-1 + load balancer healthy_targets 1) → solve again
  (`solved:true`, `xp_awarded: 120`) → validate (5/5 checks
  passed) → XP (`total_xp=270`, `breaker` achievement earned) →
  delete all resources → destroy lab (DESTROYED) → reconcile
  dry-run reports exactly 1 live orphan (the leftover engine
  network) and 0 DB orphans → enforce via the admin endpoint
  (`removed_live: 1`) → topology now 503 `engine_unavailable`
  (network really gone) → final dry-run clean, no host-agent VMs.
- **Result: PASS in ~12–14 s** (run twice). Skips gracefully
  when `CLOUDFORGE_E2E_SKIP=1` or the toolchain/fakes cannot
  start.
- **Two things the first run exposed and fixed:** (1) Go's
  internal-package rule forbids `tests/e2e` importing
  `backend/internal/*`, so the in-process design was replaced
  with the black-box design (real binaries) — the honest E2E.
  (2) The first black-box run left the lab stuck in CREATED:
  the config's default `DATABASE_URL` is the live dev Postgres,
  so the API used Postgres + async provisioning and waited on a
  worker that wasn't running. The test now points `DATABASE_URL`
  at a dead port (`127.0.0.1:1`) to force the in-memory fallback
  and asserts the API log contains "in-memory stores" before
  proceeding. The stray lab + two users from that run were
  deleted from Postgres afterwards.
- `backend/cmd/e2e-fakes/main.go` documents the fake engine's
  contract (NOT_FOUND semantics the API depends on).

### 8. Docs + log

- Root `README.md`: milestone status table (M1–M9), M2 marked
  complete, M9 Part 2 + Part 3 sections, stale
  "backend/Dockerfile missing" notes fixed, "Next steps" now
  points at real-host verification.
- `docs/operations/runbook.md`: appended backup/restore, quotas,
  cleanup reconciliation; routine-operations list updated;
  stale Dockerfile note fixed.
- `backend/README.md`: M9 Part 3 section (Dockerfile, security,
  quotas, monitoring, backup/restore, reconcile, E2E); stale
  Dockerfile notes fixed.
- This log entry.

### Final verification (2026-11-21, after the last code change)

- `gofmt -l .` (backend): **0 files** (one spacing nit in
  `internal/security/security.go` found and fixed during this
  pass).
- `go vet ./...` (backend): **clean**.
- `go test ./...` (backend, `-count=1`): **all packages pass**
  (httpapi 25.6s, auth 21.0s, providers 7.6s, networkclient 2.1s,
  store 2.5s, jobs 0.1s, security/quota/labs/others < 1s).
- `cargo test --workspace`: **74 passed, 0 failed** (20+2+7+10+
  7+1+12+10+5 across crates; 18 "test result: ok" lines).
- Frontend: `npm run build` ✓ (3.47s), `npm test` **51/51 pass
  (5 files)**, `npm run lint` **0 warnings, 0 errors**.
- `make verify`: **exit 0**.
- E2E (`cd tests/e2e && go test -v .`): **PASS** (13–14 s, run
  twice, last run after the final code change).
- Live metrics spot-check (real API + fakes): `/metrics` shows
  `cloudforge_host_agent_up 1`, `cloudforge_network_engine_up 1`
  after `/api/v1/health`, and
  `cloudforge_lab_events_total{event="lab_created"} 1` after a
  lab create.

### Known limitations

- **Unverified in this sandbox (needs a real host):**
  `docker build` / `docker compose up` (no Docker), real
  KVM/libvirt VM provisioning (no `/dev/kvm`), the Rust engine's
  real rtnetlink/nftables backend (compiles; default backend is
  SIMULATED), nginx config syntax (no nginx binary — `nginx -t`
  not run), Grafana dashboards against a live Prometheus (JSON
  validated as parsing only), Prometheus scrape of the engine +
  host agent (neither exposes `/metrics` yet — by design, their
  health is exported via the API gauges).
- Distroless runtime digest is tag-pinned, not digest-pinned
  (could not be resolved through the sandbox proxy); pin it in CI
  via `docker buildx imagetools inspect`.
- The `cloudforge_jobs_total` counter only appears in `/metrics`
  after the first job completes (Prometheus `CounterVec`
  behaviour); it is incremented in `jobs.RunOnce` and covered by
  the jobs tests, but was not observed live (no worker ran in the
  E2E).
- Audit-log retention is 90 days with no archival — operators
  must snapshot `audit_logs` before the worker's `audit_cleanup`
  job deletes old rows.
- No token revocation (pre-existing M9 limitation); rate limiting
  is per IP.
- User count in the live Postgres is 20 after cleanup (all
  throwaway E2E/metric-check users and labs deleted; no stray
  processes left running).
