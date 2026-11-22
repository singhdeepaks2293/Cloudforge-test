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

## Lightweight mode (2026-11-21, after M9) — lite as an option alongside full

The user asked for a lightweight mode for education. Lite is an
ADDITION, not a replacement: full/heavy mode is unchanged
(`make up` / `docker-compose.yml` — Prometheus, Grafana, MinIO,
KVM VMs, Terraform containers, real netlink, all M9 features).
`docker-compose.yml` was not edited (mtime 2026-11-21 18:08,
before this work; still 8 services, `NET_ADMIN` only on the
engine, no limits added). `make up` still resolves to the full
file; `make up-lite` is the new lite entry point.

### Files created

- `deployments/compose/docker-compose.light.yml` — standalone
  compose file (not an override): frontend, api, **worker** (the
  full file defines no worker service; lite runs one so labs
  actually provision), network-engine (SIMULATED, no
  capabilities), postgres, redis. No prometheus/grafana/minio/
  emulator. Per-service `deploy.resources.limits`: api 256 MiB /
  0.5 CPU, worker 128 MiB / 0.25, engine 128 MiB / 0.25, frontend
  128 MiB / 0.25, postgres 512 MiB / 0.5, redis 128 MiB / 0.25 —
  1,280 MiB total. Postgres low-memory command
  (shared_buffers=64MB, work_mem=4MB, maintenance_work_mem=64MB,
  max_connections=20, effective_cache_size=256MB). Redis
  `--appendonly no --maxmemory 128mb --maxmemory-policy
  allkeys-lru`. Lite-specific volumes/networks
  (`cloudforge_lite_*`) so lite and full keep separate databases.
- `docs/operations/lightweight.md` — mode table, resource table
  with the reason for each limit, measured memory, what is
  disabled/changed vs full, run instructions, honest caveats.

### Files changed

- `backend/internal/quota/quota.go` — `MaxLabMinutes` on
  `Limits` (wins over `MaxLabHours` when > 0; both zero =
  unlimited), `LiteMaxLabMinutes = 30`, `LiteLimits(role)`
  (learner 1 lab / 1 CPU / 1024 MiB / 8 GB / 30 min; instructor
  2 / 1 / 1024 / 8 / 60; admin 3 / 2 / 2048 / 16 / 120),
  `LimitsFor(role, lite)`, `CapAt(cap)`, `DurationMinutes()`;
  `CheckLabDuration` now reports minutes.
- `backend/internal/quota/quota_test.go` — `TestLiteLimits`,
  `TestLimitsCapAt`.
- `backend/internal/store/store.go`, `postgres_quotas.go`,
  `memory.go` — `QuotaSet.MaxLabMinutes` threaded through.
- `backend/migrations/007_lite_lab_minutes.sql` — adds
  `quotas.max_lab_minutes` (default 0 = hour-based behaviour, so
  full mode is unchanged for existing rows).
- `backend/internal/config/config.go` — `Lite bool`,
  `CLOUDFORGE_LITE` (1/0, true/false, yes/no, on/off; anything
  else is a startup error) and **`CLOUDFORGE_MODE=lite|full`**
  (default full). Both switches accepted; setting them to
  different modes, or an unknown mode, fails startup. Tests:
  `TestLoadLiteFlag`, `TestLoadModeFlag`.
- `backend/internal/httpapi/` — `Deps`/`Server` gain `Lite`;
  `roleLimits` caps stored rows at `LiteLimits` when lite (so a
  DB seeded with full defaults is still capped); `PUT /admin/
  quotas` accepts `max_lab_minutes` (0–10080); `/api/v1/health`
  reports `"lite"`. Test `TestLiteModeCapsQuotas` (60-min lab →
  429, 30-min lab allowed, second lab → 429).
- `backend/cmd/api/main.go` — in lite mode, upserts lite limits
  for every role at startup and logs it.
- `Makefile` — `up-lite`, `down-lite`, `logs-lite`, `ps-lite`
  (`COMPOSE_LITE`, lite file); `up`/`down`/`logs`/`ps` unchanged
  (full file). `make -n` verified both resolve correctly.
- `.env.example` — `CLOUDFORGE_MODE=full` (default) with the lite
  explanation; `CLOUDFORGE_LITE=true` noted as an alias.
- `backend/README.md` — env rows for `CLOUDFORGE_MODE` /
  `CLOUDFORGE_LITE`.
- `README.md` — "Choose a mode first: Lite or Full" table at the
  top of "How to run"; `make up-lite` documented next to
  `make up`.

### Verification (this sandbox, 2026-11-21, 4 CPU / 7.7 GiB)

- `gofmt -l .` clean; `go vet ./...` clean.
- `go test ./...` (backend): **all 19 packages pass** (incl.
  `TestLiteLimits`, `TestLimitsCapAt`, `TestLoadLiteFlag`,
  `TestLoadModeFlag`, `TestLiteModeCapsQuotas`).
  Note: the full `httpapi` package run initially failed only
  `TestAuthRateLimitTriggers`, `TestAPIRateLimitTriggers`,
  `TestAuthRateLimitIsPerIP` with Redis connection-refused —
  the sandbox VM had been replaced and the Postgres/Redis
  server packages were gone (only client libs remained). This
  was environmental, not caused by the lite changes: after
  `apt-get install postgresql redis-server` and starting both,
  all three pass.
- Frontend: `npm run build` pass, **51/51 tests pass**,
  `npm run lint` 0 warnings / 0 errors.
- Lite compose file: YAML-parses; every service has the
  documented limits; no `cap_add` anywhere. `docker compose
  config` / `up` **not run — no Docker in this sandbox**.
- Lite stack run live here (API + worker + Rust engine +
  frontend dev server against Postgres 16.15 / Redis 7.0.15,
  `CLOUDFORGE_MODE=lite`): health reports `"lite":true`,
  engine `backend=SIMULATED`; migration 007 applied and lite
  quota rows seeded (learner 1/1/1024/8/30 min); registered a
  learner — `vpc-fundamentals` (60 min) → 429,
  `ec2-basics` (30 min) → created, worker-provisioned to READY
  in ~6 s, second lab → 429, destroy → 200; frontend served
  HTTP 200 on :5173. All lite processes stopped afterwards.
- Measured RSS (per-process, the reliable numbers): api ~90 MiB,
  worker ~21 MiB, network-engine ~6 MiB, postgres (stock host
  settings, not lite-tuned) ~144 MiB total, redis ~6 MiB,
  vite dev server ~200 MiB (dev-only; Docker frontend is nginx).
  Platform total excluding the dev server: **~267 MiB**.
  `free -m` available fell 1,110 → 854 MiB, but the box was
  already heavily loaded by other work, so that delta is noisy.

### Honest limitations

- `docker compose config` / `up` for the lite file never ran
  (no Docker here); the file is YAML-valid and follows the full
  file's structure, but it has never been booted by Docker.
- Real container RSS under the cgroup limits was not measured.
- Per-lab Micro memory (~0.5–1 GiB) is the profile's allocation,
  not a live measurement (the lab ran with the simulated host
  agent; no real container/VM started).
- The 4 GB RAM minimum for lite is an extrapolation from the
  ~267 MiB measured platform + ~1 GiB lab + ~1 GiB OS — it was
  never run on a 4 GiB machine. Test on classroom hardware first.
- Only 30-minute lab definitions work in lite (`ec2-basics`,
  `iam-security`); `vpc-fundamentals` (60 min) and
  `three-tier-app` (120 min) are refused with 429 in lite mode.

## Launch lab fix (2026-11-22, after the user verified lite mode live on Fedora)

### What was broken
- The frontend had NO lab-creation UI at all: Labs "Launch lab" was
  only a `<Link to="/learn">`, Learn/Courses had no launch action,
  and `api.ts` had no `createLab` (only `createLabResource`). The
  backend `POST /api/v1/labs` existed and was tested; the UI never
  called it. Live API logs confirmed it: register + GET /labs +
  GET /courses appeared, but NO `POST /api/v1/labs` ever arrived
  when the user clicked Launch.
- The Labs page likewise had no destroy action at all.
- Curriculum was never seeded in Docker: the API log said
  "curriculum seeding skipped: no curriculum directory found".
  The api build context is `backend/` only, so repo-root
  `curriculum/` was never in the image and no compose file mounted
  it or set CURRICULUM_DIR. Lab definitions / courses are hardcoded
  in Go, so those still worked — M8 tracks/lessons/achievements
  were missing in Docker.

### A. Frontend Launch Lab (main fix)
- `frontend/src/services/api.ts`: added `createLab(definitionId,
  name?)` — POST /api/v1/labs, Bearer token, parses the
  `{ lab, replayed }` envelope (201 in-memory / 202 worker-async /
  200 replay); 400 unknown_definition, 401, 429 quota_exceeded
  surface as ApiError with the backend message. Added
  `destroyLab(id)` — POST /api/v1/labs/{id}/destroy, 200 bare lab,
  404 lab_not_found, 409 invalid_transition. `parseHealth` now
  reads the backend's `"lite"` flag (absent = full mode).
- `frontend/src/types/index.ts`: `LabDefinition` +
  `LAB_DEFINITIONS` (the 4 definitions, mirroring
  backend/internal/labs/labs.go) + `LITE_MAX_LAB_MINUTES = 30` +
  `isDefinitionDisabledInLite`.
- `frontend/src/components/LabLauncher.tsx` (new): definition
  picker (duration shown; 60/120-min labs disabled in lite mode
  with the reason) + per-course `LaunchLabButton`. Mutation/error
  helpers live in `frontend/src/hooks/useLaunchLab.ts`
  (`launchErrorMessage`, `useCreateLab`, `useLiteMode`) so the
  component file only exports components (oxlint).
- `frontend/src/pages/Labs.tsx`: "Launch lab" now opens the real
  picker; a launched lab is polled (1 s) until it leaves
  CREATED/PROVISIONING, with workstation/topology links at READY;
  Learn/Courses navigate here as `/labs?launched=<id>` and the
  page polls that lab too. Stale "provisioning arrives in
  Milestones 2–3" empty text removed.
- `frontend/src/pages/Learn.tsx` / `Courses.tsx`: a Launch button
  per course whose id is a lab definition (course ids match
  definition ids); on launch, navigates to `/labs?launched=<id>`.
- Backend API contract unchanged (no backend code touched for A).

### A2. Destroy — single + bulk (user follow-ups)
- Per-row Destroy on Labs with a two-click confirm; success
  refreshes the table (row moves to DESTROYING → DESTROYED),
  401/404/409 shown, not swallowed.
- Bulk destroy: checkbox per row + select-all checkbox in the
  table header (`AwsTable` column headers now accept a ReactNode),
  "Destroy selected (N)" when 1+ selected, "Destroy all labs (N)"
  for everything owned. Both confirm naming the count, then call
  the existing destroy endpoint one lab at a time via
  `frontend/src/hooks/useBulkDestroy.ts`
  (`destroyLabsSequentially` + `toggleSelected` /
  `toggleSelectAll` / `areAllSelected` / `bulkDestroySummary`):
  per-lab progress ("Destroying lab 2 of 3…"), per-lab
  success/failure results, list refresh afterwards, failures
  listed with the lab name and backend message. No new backend
  endpoint. One failure never aborts the rest.

### B. Curriculum in Docker
- Volume mount, NOT a build-context change (backend/Dockerfile
  untouched): `../../curriculum:/curriculum:ro` on the api
  service in both compose files (+ worker in lite) and
  `CURRICULUM_DIR: /curriculum` in the service environment.
  With CURRICULUM_DIR set explicitly, a missing mount fails API
  startup loudly instead of silently running with no curriculum.
  Relative bind paths resolve against the compose file's
  directory, so `../../curriculum` is the repo-root curriculum/
  from any working directory. Compose YAML validated with
  PyYAML (both files parse; api/worker CURRICULUM_DIR and
  volumes confirmed in the parsed output).

### Previously fixed on the user's host, now baked into the repo
All three were fixed manually on the user's Fedora host during
the first lite-mode run (2026-11-22) and are verified present
here, so a fresh checkout/zip works without manual fixes:
1. Compose .env: `Makefile` lines 19 and 26 read
   `docker compose --env-file .env -f deployments/compose/...`.
   Without it, `docker compose -f` in a subdir sets the project
   directory to the compose file's directory and silently ignores
   the root .env (every `${...:?}` variable reports missing).
   Verified: `make -n up` / `make -n up-lite` print the
   `--env-file .env` commands from the repo root, and no `.env`
   exists under `deployments/compose/` in the repo.
2. Rust engine Dockerfile: `protobuf-compiler` is UNPINNED
   (`rust-network-engine/Dockerfile:9`, no `=3.21.12-3`;
   `grep -c 3.21.12` returns 0). Debian Bookworm no longer
   serves that revision — the pinned build failed with apt exit
   100 on the user's host.
3. Frontend nginx pid: `frontend/nginx.conf` contains NO `pid`
   directive (verified: `grep -E "^\s*pid\s"` finds nothing —
   only comments mention pid). `frontend/Dockerfile` rewrites
   the stock nginx.conf pid line with the whitespace-tolerant
   sed `s|pid[[:space:]]*[^;]*;|pid /tmp/nginx.pid;|` plus a
   `grep '^pid'` check that fails the build if it did not match
   (the stock line is `pid        /var/run/nginx.pid;` with
   multiple spaces, which a single-space pattern silently
   missed); COPY is `--chown=nginx:nginx`; CMD is plain
   `["nginx", "-g", "daemon off;"]` (no second pid via -g — the
   stock config already has one and a duplicate is fatal, and
   /var/run/nginx.pid is not writable by the unprivileged nginx
   user, which was the third restart-loop error in sequence:
   "not allowed here" → "duplicate" → "Permission denied").

### Verification (real, in this sandbox)
- Frontend: `npm test` 72/72 pass (was 51 before this fix: +5
  createLab, +1 parseHealth lite, +3 picker lite-mode, +4
  destroyLab, +5 bulk-destroy helpers, +3 Labs selection
  rendering); `npm run build` clean; `npm run lint` 0 warnings,
  0 errors (the 3 only-export-components warnings from the first
  draft were fixed by moving helpers to hooks/useLaunchLab.ts).
- Backend: `gofmt`/`go vet` clean; `go test ./...` all 19
  packages pass; `httpapi` + `store` re-run against a real
  Postgres 16 + Redis 7 (DATABASE_URL/REDIS_ADDR set) also pass.
- Live lite stack (API + worker + Rust engine + Vite dev server,
  CLOUDFORGE_MODE=lite, CURRICULUM_DIR set, fresh `cloudforge`
  database): API log says `curriculum seeded` (tracks 14,
  lessons 70, questions 84, scenarios 6, achievements 15) —
  no more "seeding skipped" warning — and `lite quotas seeded
  (learner 1 lab / Micro / 30 min)`; health `lite:true`,
  `readyz` ready, engine SIMULATED.
  End-to-end transcript (curl, user e2e@test.local):
  register → 200 + token; GET /api/v1/courses → 17 courses
  (seeded curriculum expands the 3 hardcoded courses);
  GET /api/v1/curriculum/tracks → 17 tracks; POST
  /api/v1/labs ec2-basics → 202, CREATED → READY in ~3 s;
  POST vpc-fundamentals (60 min) → 429 quota_exceeded; second
  lab while 1 active → 429; POST /destroy → 200 DESTROYING →
  DESTROYED; then iam-security → 202 → READY → destroy 200
  (the bulk sequence, one by one). Vite :5173 serves the new
  UI (HTTP 200; Labs.tsx served contains "Destroy selected" /
  "Destroy all labs"; /api proxy returns lite health).
  Measured RSS: api ~92 MiB, worker ~20 MiB, engine ~7 MiB,
  vite dev ~216 MiB (dev-only). All test processes stopped
  afterwards.
- Note: the brief's "GET courses (3 courses)" expectation is
  pre-seed; after seeding the endpoint returns 17 courses and
  17 tracks (seed log itself reports 14 tracks — the tracks
  endpoint count differs from the seed report; not
  investigated, the seeded content is present either way).

### Honest limitations
- `docker compose up` / `docker compose config` never ran here
  (no Docker/KVM in this sandbox): the curriculum mount,
  the Makefile --env-file fix, and the nginx/Dockerfile fixes
  are verified in the files and (for the mount) by the live
  manual run with the equivalent env, but not by a real
  `make up-lite` on a Docker host. The user already ran
  `make up-lite` successfully on Fedora with the manual
  versions of fixes 1–3; the repo versions are the same
  content, but a fresh-checkout Docker run is still untested.
- The Launch/destroy/bulk UI was verified via unit tests and
  the served source, not by clicking it in a browser (no
  browser here): the picker disabling, createLab/destroyLab
  calls and bulk sequencing are covered by vitest, and the
  backend flow they call was verified live by curl.
- Real KVM VMs / Terraform containers / real netlink remain
  unverified, as before.

## Phase 0 — Audit — 2026-11-22

Read-only audit of all 248 source files (no code changed). Full report in
`docs/IMPLEMENTATION_STATUS.md`, service table in
`docs/SERVICE_CATALOG.md`, fidelity rules in `docs/SERVICE_FIDELITY.md`.

### What was audited

Auth (Argon2id + HS256 JWT, dual-verify bcrypt), labs + lifecycle + quotas,
lab resources/topology/validation/terminal, curriculum (14 tracks, 70
lessons, 84 questions, 6 break-and-fix, 15 achievements), Rust network
engine (23 gRPC RPCs, simulated default + real rtnetlink/nftables),
providers registry (27 seeded capabilities), S3/MinIO, DynamoDB, IAM
evaluate, STS/KMS/SQS/SNS/EventBridge simulators, Terraform (container-only,
pinned `hashicorp/terraform:1.9.8`), monitoring/audit/backup/reconcile,
frontend (9 routes, 17-entry service catalog), both compose files, all 7
migrations, `Makefile`, `docs/`, `curriculum/`, `infrastructure/`.

### Key findings

- Works end-to-end (backend + frontend + tests, verified live): auth,
  labs incl. Launch/Destroy/bulk-destroy UI, resources/topology/validate,
  terminal, curriculum/assessments/achievements, Terraform jobs, S3 buckets
  via MinIO, IAM policy evaluation (subset), audit, quotas, lite mode.
- Registry-only (registry entry, no HTTP route, no page): DynamoDB, SQS,
  SNS, EventBridge, STS, KMS, Lambda, EC2-as-an-API. The EC2 entry claims
  an emulator backend that is not deployed; the Lambda entry claims a
  Docker invoker that does not exist in code — both treated as not started
  until wired or downgraded.
- Not started: RDS, EBS, ELB/ALB, Route 53, CloudWatch/CloudTrail consoles,
  ECS/ECR, CloudFormation, API Gateway, Step Functions, and §6 categories
  K–N (Analytics, ML/AI, Migration, Additional) almost entirely.
- Frontend is a console shell, not per-service consoles: the 17 catalog
  entries mostly redirect to `/labs`, `/learn` or `/console`. No resource
  explorer, account menu, favourites, or per-service detail tabs yet.
- **Critical gap: full compose has no `worker` service** (and no host-agent
  service), yet Postgres mode returns 202 and enqueues provisioning jobs —
  full-mode labs would sit in `CREATED` with no worker to run them. Lite
  compose does run a worker. Fix before Phase 1.
- Unresolved count discrepancy: seed log reports 14 tracks, but after
  seeding `GET /api/v1/courses` and `/curriculum/tracks` each return 17.
- Three empty package dirs (`backend/internal/audit|events|users/`) are
  placeholders — remove or implement in Phase 1.

### Port discrepancy — RESOLVED by user decision (2026-11-22)

Before (repo as audited, verified working on the user's Fedora host):
frontend host `:80` → nginx `:8080` in-container; API host `:8080`;
worker internal `:8081`; host agent `:8082`. After (decided target,
master prompt §3): frontend host **`:8080`**, API host **`:8081`**, worker
moved off `:8081` to internal **`:8083`** (filled in Phase 1 from the
port-change work — `WORKER_ADDR` default `:8083`, both compose files pin
`WORKER_ADDR: ":8083"`); host agent stays **`:8082`**. Operator action on the Fedora host: `make
down-lite` / `make up-lite` again from the new zip and update bookmarks
and firewall rules from `:80`/`:8080` to **`:8080` (frontend) / `:8081`
(API)**. Vite dev proxy (`:5173` → API) must follow the API to `:8081`.

### AWS-duplicate UI — core requirement (user decision, 2026-11-22)

Every service must get an AWS-duplicate UI mirroring the real AWS console's
layout, navigation, tables, detail tabs, wizards and action names/placement
(EC2 Instances table + tabs, S3 Buckets/Objects/Properties/Permissions,
VPC Your VPCs/Subnets/Route Tables/Gateways/SGs/NACLs, IAM Users/Groups/
Roles/Policies/Policy Simulator, RDS Databases/Snapshots/Monitoring, Lambda
Functions/Code/Configuration/Triggers/Permissions/Monitoring — same depth
for every service, Phases 1–6). A generic list page does NOT count as a
service console. Global console must mirror AWS navigation: service search,
region selector, account menu, breadcrumbs, Recently Visited, Favorites,
and the §4 main-navigation areas. Guardrails: CloudForge branding only —
no AWS logos/trademarks/proprietary icons/copied assets; never claim it is
AWS. UI must reflect actual backend capabilities: SIMULATED / PARTIAL /
UNSUPPORTED operations are labelled and disabled, never shown as working.
`docs/SERVICE_CATALOG.md` records per service which AWS screens it mirrors
and what it deliberately differs on. Verification rule (Phases 1–6): each
service console is verified by walking create → list → detail tabs →
action → delete end-to-end in a live sandbox run; no resource is shown
`READY` until its operation actually succeeded
(`docs/SERVICE_FIDELITY.md` §3).

### Top risks (full list + evidence: `docs/IMPLEMENTATION_STATUS.md` §5)

1. Unauthenticated plaintext gRPC engine on `:50051` (published on host).
2. No worker in full compose (labs stuck `CREATED`).
3. No JWT revocation (12 h token lifetime, logout is client-side only).
4. Dev-default secrets ship in code (JWT, MinIO, host-agent, Grafana).
5. `/metrics` unauthenticated and publicly proxied by nginx.
6. JWT in `localStorage` (XSS = account takeover); no CSP headers.
7. `X-Forwarded-For` trusted unconditionally (rate-limit evasion).
8. Per-IP-only rate limiting; general API fails open when Redis is down.
9. Simulators (KMS/STS/SQS) that look like real AWS services.
10. Audit retention is delete-only (90 days, no export).

### Roadmap pointer

Phases 0–7 in `docs/IMPLEMENTATION_STATUS.md` §7: Phase 1 console
foundation (registry-driven nav/search, resource explorer, fidelity
badges, worker fix); Phase 2 core infrastructure (IAM, VPC, EC2, S3,
CloudWatch/CloudTrail-style); Phase 3 data/messaging/serverless; Phase 4
containers/dev tools; Phase 5 management/analytics/ML/migration; Phase 6
Terraform Learning + Learning Center; Phase 7 production hardening
(mTLS engine auth, JWT revocation, fail-closed secrets, TLS, audit
export). Each phase is verified by tests + a live E2E walk per service.

### Sandbox limits (honest)

No Docker/KVM/`/dev/kvm`/libvirt/nginx in this sandbox, so `docker
compose build/up`, real KVM provisioning, the real netlink backend,
Terraform container execution, nginx syntax, and Grafana-against-live-
Prometheus remain unverified here — as in all prior milestones. The port
change itself is parallel work and was not verified by this audit.

## Phase 1 — Console Foundation — 2026-11-22

Global console, registry-driven, mirroring AWS navigation patterns
(master prompt §4, §5, §19 Phase 1). The hardcoded 17-entry frontend
catalog is gone: nav, search and All Services render from the backend
service registry. Every console surface is wired to a real API and
tested; unsupported operations are labelled and disabled, never shown
as working.

### Worker port — TBD resolved

The Phase 0 audit recorded the worker port as "TBD". It is **:8083**
(`WORKER_ADDR` default `:8083` in `backend/internal/config`, both
compose files pin `WORKER_ADDR: ":8083"`). The two TBD spots
(`docs/IMPLEMENTATION_STATUS.md:45`, this log's Phase 0 entry) now say
:8083.

### What was built

- **Service registry (backend):** `GET /api/v1/services` (86 services)
  and `GET /api/v1/services/{id}` (`backend/internal/httpapi/services.go`).
  Console metadata (id, name, category, description, icon key, route,
  availability, fidelity, resource types, docs link, learning exercises,
  mirrors/differs) lives in one Go table; operations and their fidelity
  are derived from the existing `providers.Registry` — no duplicate
  registry. Adding a service is one table row (plus registry entries
  when it gains real operations); the frontend needs no code change.
- **Resource Explorer (backend):** `GET /api/v1/explorer/resources` —
  real data only: own labs (admin: all), lab resources, courses, users
  (admin-only). Filters `q`/`type`/`status`, sort `name`/`created_at`/
  `status`, pagination ≤100. Region scoping is display-only: every row
  is `cf-local-1`, `region_scoped: false`; another region returns an
  empty list + `region_note`, never invented rows.
- **Global console shell (frontend):** `Layout` / `AwsHeader` /
  `AwsNav` render from the registry. Top nav: CloudForge wordmark,
  service search (names, descriptions, categories, API operations,
  resource types, learning exercises — results show category + fidelity
  and navigate to the real service page), region selector
  (`cf-local-1/2/3`), notifications from live `/api/v1/health`,
  help/docs menu, account menu (profile + role from `/auth/me`, account
  settings, sign out). Left nav per §4 areas: Home, All Services,
  Recently Visited, Favorites, Resource Explorer, registry categories
  (Compute, Storage, Networking, Databases, Security and Identity,
  Application Integration, Containers, Serverless, Developer Tools,
  Management and Governance, Monitoring, Migration and Transfer,
  Analytics, Machine Learning and AI, Terraform Learning, Learning
  Center), Account Settings, Administration. Breadcrumbs via
  `PageHeader` on every page. Recently Visited + Favorites are per-user
  localStorage (keyed by the JWT `sub` claim).
- **Shared layouts + fidelity badges:** `ServiceConsole` (header, left
  sub-nav, tabs) and `FidelityBadge` (REAL / API_EMULATED / SIMULATED /
  PARTIAL / UNSUPPORTED + footnote), used on every console.
- **EC2 console** (`/services/ec2`): Instances table (Instance ID /
  Name / State / Type / VPC / Subnet / AZ / Launch time) from real labs
  + lab resources; detail tabs Details / Security / Networking /
  Storage / Monitoring / Status Checks / Logs / Tags / Events; Launch
  (→ Labs), Stop and Terminate wired to the real API; Start/Reboot
  disabled — no backend operation exists.
- **S3 console** (`/services/s3`): real buckets from MinIO, create-bucket
  wizard wired to `POST /aws/s3/buckets`, tabs Objects / Properties /
  Permissions / Management / Metrics. Object upload disabled —
  PutObject/GetObject have no HTTP route yet. MinIO down → honest 503.
- **VPC console** (`/services/vpc`): Your VPCs / Subnets / Route Tables /
  Internet Gateways / NAT Gateways / Security Groups / Network ACLs from
  the explorer; delete wired to the real resource API; create disabled
  (VPCs are created as lab resources); NACLs show UNSUPPORTED.
- **IAM console** (`/services/iam`): Policy Simulator wired to real
  `POST /aws/iam/evaluate`; Users list real (admin-only); Groups /
  Roles / Policies / Access Analyzer disabled with reasons.
- **Generic service console** (`/services/:id`): overview, operations
  table (fidelity + HTTP route + limitations), learning exercises — for
  every other registry service. **All Services** (`/services`),
  **Resource Explorer** (`/explorer`), **Account Settings** (`/account`),
  **Administration** (`/admin`: real quotas + audit, admin-only).
- **14-vs-17 track count — FIXED.** Cause: `ListTracks` listed every
  course row (3 legacy M1 courses + 14 curriculum tracks = 17) while
  the seed report counts YAML tracks (14). The legacy courses even
  have guided lab definitions (the 4 launchable lab definitions), so
  the first fix attempt (EXISTS guided-or-challenge) still returned
  17 — found by the live check, not by tests. The real discriminator:
  a track has BOTH a guided and a challenge lab definition (every
  seeded track upserts both; legacy courses only ever gained guided
  ones — verified in SQL: 14 rows match both). Memory store records
  seeded track ids explicitly (`tracks` set in `UpsertTrack`).
  `TestListTracksExcludesLegacyCourses` pins it: tracks = 14 =
  seed report, courses = 17 (3 legacy + 14, correct).
- **Worker in full compose (R2) — FIXED:** `worker` service added to
  `deployments/compose/docker-compose.yml` mirroring lite
  (`WORKER_ADDR: ":8083"`, same env/curriculum mount/networks,
  `depends_on` postgres+redis healthy). Full-mode labs no longer sit
  in `CREATED`. Verified by reading the compose file (YAML parses;
  `docker compose config` still unverified here — no Docker) plus a
  live lite lab provisioned to `READY` by the worker.
- **Empty placeholder packages removed:** `backend/internal/audit|events|users/`
  (empty dirs, nothing imported them).

### Verification

- `make verify` exit 0: gofmt/vet clean; `go test ./...` all packages
  pass; `cargo test --workspace` 74 passed (Rust untouched); frontend
  build + vitest **77/77** + oxlint 0 warnings, 0 errors.
- New tests: `TestServicesRegistry` + `TestExplorerResources`
  (`backend/internal/httpapi/services_test.go`), `TestListTracksExcludesLegacyCourses`
  + updated `TestSeedIdempotentMemory` (store), `registry.test.ts`
  (parse/search/group), rewritten `catalog.test.ts` (3 regions,
  recent/favorites), rewritten `aws.test.tsx` (registry-driven header/nav,
  fidelity badges). Pre-existing tests updated where the spec changed:
  EC2 now resolves to `/services/ec2` (not `/labs`), VPC to
  `/services/vpc` (not `/console`), regions are now 3, not 2.

### Live transcript (this sandbox, 2026-11-22 ~20:20–20:23 UTC)

Lite stack: API `:8081` (lite=true), worker `:8083`, Rust engine
`:50051` (SIMULATED), Postgres 16 + Redis 7 on localhost, frontend
production build on `:8080` (Vite preview; nginx is Docker-only).

- `GET /api/v1/health` → `ok:true, lite:true, network_engine_grpc:true,
  database_connected:true, redis_connected:true`
- `GET /readyz` → `ready` (postgres/redis/engine ok)
- `GET /api/v1/services` → 200, **86 services**; ec2 PARTIAL/available
  `/services/ec2` (ops DescribeInstances, RunInstances); s3 PARTIAL
  (CreateBucket/ListBuckets carry `http_route`, Put/GetObject carry
  none); dynamodb API_EMULATED/registry-only; eks UNSUPPORTED; lambda
  SIMULATED/registry-only
- `GET /api/v1/curriculum/tracks` → **14**; `GET /api/v1/courses` → **17**
- `POST /api/v1/labs` (`ec2-basics`) → **202**, state CREATED →
  worker → **READY** in ~2 s, with a VPC lab resource
  (`rust-network-engine`, state CREATED)
- `GET /api/v1/explorer/resources` → total **19** (17 courses + lab +
  VPC); `?q=p1-live-lab` → 1; `?region=cf-local-2` → 0 + note
  ("Region scoping is display-only in Phase 1…"); `?type=lab` → 1,
  fidelity SIMULATED, `can_delete: true`
- `POST /api/v1/aws/iam/evaluate` (Allow s3:ListBuckets) →
  `{"decision":"Allow","reason":"allowed by matching statement"}`
- `GET /api/v1/aws/s3/buckets` → **503** `backend_unavailable`
  (MinIO at localhost:9000 unreachable — honest, no fake empty list)
- `POST /api/v1/labs/{id}/destroy` → DESTROYING → **DESTROYED**
  (worker `/status`: jobs done 7)
- Frontend `:8080`: `/`, `/services`, `/explorer` → 200;
  `/api/v1/services` via the preview proxy → 200, 86 services;
  built bundle contains the new routes (`services/ec2`,
  "Resource Explorer", "Policy Simulator")

### Fidelity table (Phase 1 deliverables)

| Surface | Fidelity | Evidence |
|---|---|---|
| Service registry | REAL (local data) | `GET /api/v1/services` live, 86 services; ops from providers registry |
| Resource Explorer | REAL | Live walk: 19 real rows, search/filter/region behaviour verified |
| EC2 console | PARTIAL (labs SIMULATED) | Live lab READY via worker; Start/Reboot disabled, no backend op |
| S3 console | PARTIAL (buckets API_EMULATED via MinIO; objects adapter-only) | Live 503 when MinIO down; upload disabled with reason |
| VPC console | PARTIAL (SIMULATED engine) | Real lab resources listed/deleted live |
| IAM console | PARTIAL (evaluate is a real subset) | Live `Allow` evaluation; CRUD disabled, no backend API |
| Generic service consoles | Per-service fidelity from registry | Ops table shows fidelity + route per operation |
| Account / Admin | REAL | `/auth/me`, quotas, audit are real APIs |

### Honest limitations

- `docker compose config/up`, real KVM, real netlink, Terraform
  containers and nginx config syntax remain unverified here (no
  Docker/KVM/nginx) — as in all prior milestones. The full-compose
  worker fix is verified by reading the compose file + the live lite
  worker, not by `docker compose up`.
- The loud startup check for "Postgres mode with no worker" (R2
  follow-up) is not built yet.
- Region scoping is display-only (all resources are `cf-local-1`).
- S3 objects, IAM CRUD, VPC create wizard, CloudWatch alarms and
  CloudTrail export are Phase 2–3 work; their console actions are
  disabled with reasons, not faked.
- Notifications are live control-plane health only (no per-resource
  alert feed yet).
- Favorites/Recently Visited are per-user localStorage; backend
  persistence is a stretch goal, not done.
- Frontend on `:8080` was verified with Vite preview of the production
  build, not the Docker nginx image (no Docker here).

## Phase 2 — Core Infrastructure — 2026-11-22

Deepened the four Phase 1 consoles into working core infrastructure
(master prompt §19 Phase 2, §6 A/C/E/J): IAM, VPC networking, EC2-style
compute, S3 objects, and CloudWatch-style monitoring + CloudTrail-style
audit UI. No Rust changes — the existing engine RPCs were reused as-is.

### What was built

- **IAM** (`backend/migrations/008_iam.sql`,
  `backend/internal/store/iam.go`, `postgres_iam.go`,
  `backend/internal/httpapi/iam.go`, `frontend/src/pages/IamConsole.tsx`):
  real CRUD for Users (admin-only), Groups, Roles, Policies; group
  members; attach/detach; Access Keys (create/list/revoke, secret shown
  once, SHA-256 hash at rest); AssumeRole (short-lived session, expiry
  enforced); Policy Simulator extended to stored identities
  (user/group/role/policy ids) and inline policies with the evaluation
  trail (explicit deny / allow / default deny + which statement decided
  it). Every stored policy document is probe-evaluated before storage
  (`ValidatePolicyDocument` / `ValidateTrustPolicy` in
  `backend/internal/providers/iam.go`): rejects bad JSON, wrong Version,
  empty statements, empty Action/Resource, `${...}` variables,
  NotAction/NotResource, Principal in identity policies, and conditions
  other than StringEquals on `aws:username`. Trust policies are recorded,
  not evaluated. IAM identities are separate teaching identities —
  CloudForge platform users are NOT AWS IAM; ARNs are local
  (`arn:cloudforge:iam::local:<kind>/<name>`). All 8 new IAM registry
  capabilities are `API_EMULATED` (a `REAL` claim was caught by
  `TestNoCapabilityClaimsReal` and corrected). Access Analyzer is still
  not built (UI says so).
- **VPC** (`backend/internal/httpapi/vpc.go`, `resources.go`,
  `frontend/src/pages/VpcConsole.tsx`): VPC/subnet create/list/delete
  with CIDR validation (subnet inside its VPC, no overlap); route tables
  + add/delete routes (engine-owned: engine failure = 502, nothing
  persisted); security groups with ingress/egress rules
  (DB-authoritative) + attach-to-instance; IGW attach/detach (DB-only,
  labelled SIMULATED — no engine IGW RPC); NAT create/delete via engine
  (translated address from TEST-NET-3 `203.0.113.10+n`); Tags PATCH
  (`aws:` prefix rejected). NACLs stay UNSUPPORTED (no engine RPC).
  Engine firewall policy is a one-shot snapshot (no update/delete RPC):
  later rule edits mark `engine_policy_applied=false` with a note.
- **EC2** (`backend/internal/httpapi/instances.go`,
  `frontend/src/pages/Ec2Console.tsx`): Start wired (`POST
  /labs/{id}/start`, 202 PROVISIONING, re-provisions a STOPPED lab
  through the same path as Create); Reboot wired (real host-agent
  `OpRebootVM`; 503 `reboot_unavailable` with no host agent or no VM);
  Launch wizard from `GET /labs/definitions` (>30-min definitions
  disabled in lite); Monitoring tab from live metrics; Logs/Events tabs
  from real lab audit history; per-resource Tags CRUD.
- **S3** (`backend/internal/httpapi/s3.go`,
  `backend/internal/providers/s3.go`, `frontend/src/pages/S3Console.tsx`):
  objects list/upload/download/delete via the real MinIO adapter
  (upload through the backend, base64 JSON, 10 MiB cap — no direct
  browser→MinIO credentials leak), prefixes/folders, object metadata,
  versioning toggle, bucket policy get/set, bucket delete. Object ACLs
  are NOT exposed by minio-go — Permissions shows policy only and says
  so. MinIO down → honest 503.
- **Monitoring + audit** (`backend/internal/httpapi/monitoring.go`,
  `frontend/src/pages/CloudWatchConsole.tsx`,
  `CloudTrailConsole.tsx`): `GET /api/v1/monitoring` (live Prometheus
  series + readiness as JSON; absent series are absent, never
  zero-filled); alarms are threshold rules evaluated live, definitions
  in server memory (lost on restart — UI/docs say so), creating an alarm
  on a never-observed metric is rejected with the known-metrics list;
  `GET /api/v1/audit` filtered (actor/action/resource/lab/q/from/to);
  `GET /api/v1/audit/export?format=csv|json` admin-only, max 1000 events;
  `GET /api/v1/labs/{id}/events` per-lab audit history.
- **Registry** (`backend/internal/providers/providers.go`,
  `backend/internal/httpapi/services.go`): seeded capabilities for s3
  objects (API_EMULATED), iam CRUD/AssumeRole (API_EMULATED),
  cloudwatch GetMetrics (PARTIAL) / CreateAlarm (SIMULATED), cloudtrail
  LookupEvents/ExportEvents (PARTIAL); `httpRouteFor` maps all new ops
  to real routes. `LabStore` gained `Start`, `LabResourceStore` gained
  `UpdateResource`, `AuditStore` gained `ListFiltered` (memory +
  Postgres).

### Bugs found live and fixed (not just UI)

1. **Routes could never be added.** `handleAddRoute` passed the raw
   target (`"local"` or a resource id) as the engine `Gateway` and never
   set `Interface`, but the engine's `RouteEntry::validate` requires a
   non-empty interface and an IPv4 gateway — every add returned 502
   `INVALID_ARGUMENT: interface must not be empty`. Fixed with
   `resolveRouteNextHop` / `vpcGatewayAddress` / `firstHostAddress`:
   interface is always the lab id (engine network id = lab id); gateway
   is the VPC router address (first host of the VPC CIDR) for
   `local`/VPC/IGW targets, the NAT translated address for NAT targets,
   the instance's recorded private IP for instance targets, or the first
   host of a subnet's CIDR. Route records now store `gateway` +
   `interface`. Tests: `TestResolveRouteNextHop`, `TestFirstHostAddress`.
2. **Launched instances were never started.** Instance creation called
   `OpCreateVM` only, leaving every API-created VM in state `"defined"`
   — so reboot could never succeed and a launched instance was not
   actually running. Now CreateVM → StartVM → record state `"running"`
   + private IP (via `listHostAgentVMs`); if StartVM fails the defined VM
   is deleted again and the API returns 502 with nothing persisted.
   Test: `TestInstanceCreateStartsVMAndRebootWorks` (real simulated
   host-agent server in-process).

### Verification (2026-11-22, this sandbox, lite mode)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all 19
  packages pass, `cargo test --workspace` 74 passed (Rust untouched),
  frontend build + vitest **89/89** (was 77/77; +12 Phase 2
  parser/endpoint tests) + oxlint 0 warnings.
- **Live E2E walk — 63/63 checks pass** (script `/tmp/cf-live/walk.sh`,
  transcript `/tmp/cf-live/transcript.txt`; stack: API :8081, worker
  :8083 degraded — no Postgres, `database_connected=false`, fake Redis
  stub on :6379 for the rate-limit tests — Rust engine :50051
  SIMULATED, host agent :8082 simulated, frontend dev :5173 + Vite
  preview :8080). Walk: health; admin+learner register; learner IAM
  create → 403; IAM user 201 / duplicate 409; policy rejections
  (`StringLike` → 400, `${aws:username}` → 400, nothing stored);
  allow+deny policies, evaluate Allow with trail, explicit deny wins,
  implicit deny; group + member + group policy; access key secret-once
  + absent from list + revoke; role assume (token once, session
  validates); lab `ec2-basics` → READY; bad VPC CIDR → 400, VPC/subnets
  (outside-VPC → 400, overlap → 400); route table + local route 201 +
  delete 200; SG + rule (engine policy applied) + instance created
  **running with a private IP** + SG attach 200 + rule delete; IGW
  attach/detach 200; NAT 201 with translated address; tags (`aws:` →
  400, valid → 200); stop → STOPPED, start → 202 → READY, reboot → 200;
  lab events real audit history; S3 list/upload/objects → 503 (MinIO
  down); monitoring 200 + alarm on unknown metric → 400 + alarm on real
  metric 201 + delete 200; audit filter + CSV/JSON export 200; delete
  IAM user + destroy lab 200. Frontend dev :5173 and preview :8080
  both 200, `/api/v1/health` proxied 200 (`host_agent reachable`,
  simulated; `lite: true`).
- Failure cases proving nothing shows READY on failure: invalid CIDR
  → 400 (no row), IAM evaluate explicit/implicit deny with trail,
  S3 with MinIO down → 503, alarm on unknown metric → 400, start on a
  STOPPED lab → 202 PROVISIONING (not READY) until the worker succeeds,
  reboot without a host agent/VM → 503 (unit-tested).

### Fidelity summary

| Surface | Fidelity | Evidence |
|---|---|---|
| IAM console | PARTIAL (evaluate) / API_EMULATED (CRUD) | Live walk: CRUD + trail + secret-once + assume; validation rejections 400 |
| VPC console | SIMULATED (engine) / REAL (DB-authoritative parts) | Live walk: CIDR validation, routes via engine, SG attach, IGW/NAT; NACLs UNSUPPORTED |
| EC2 console | PARTIAL (labs SIMULATED) | Live walk: Start 202→READY, Reboot 200 via simulated host agent, instance running with IP |
| S3 console | PARTIAL (buckets/objects API_EMULATED via MinIO) | Live 503 when MinIO down; object flows unit-tested (no MinIO binary obtainable) |
| CloudWatch console | PARTIAL | Live monitoring 200 (real Prometheus series); alarms in-memory only |
| CloudTrail console | PARTIAL | Live audit filter + CSV/JSON export 200; real audit events |
| Generic service consoles | Per-service fidelity from registry | Ops table shows fidelity + route per operation |

### Honest limitations

- `docker compose config/up`, real KVM, real netlink, Terraform
  containers and nginx config syntax remain unverified here (no
  Docker/KVM/nginx) — as in all prior milestones.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns
  410; the GitHub release `RELEASE.2025-10-15T17-29-55Z` ships no binary
  assets — MinIO moved binaries behind its commercial download portal).
  S3 object flows are verified by unit tests + the live 503 path only.
- No Postgres in this sandbox: worker runs degraded (no jobs
  processed), the Postgres IAM integration test
  (`postgres_iam_test.go`) skips, and the rate-limit tests need Redis
  (a minimal RESP stub at `/tmp/fake-redis.py`, sandbox test aid only,
  was used for `make verify`). Migration 008 + Postgres IAM code follow
  the same patterns as migrations 001–007.
- Alarms are in-memory only (lost on restart). Trust policies are
  recorded, not evaluated. S3 object ACLs unavailable via minio-go.
  Engine firewall policy is a one-shot snapshot (no update/delete RPC).
  Access Analyzer is still not built. NAT translated addresses are
  TEST-NET-3 documentation-range placeholders, not real public IPs.
- The loud startup check for "Postgres mode with no worker" (R2
  follow-up) is not built yet.
- Region scoping is display-only (all resources are `cf-local-1`).
- Frontend on `:8080` was verified with Vite preview of the production
  build, not the Docker nginx image (no Docker here).

## Phase 3 — Data and Application Services — 2026-11-22

Built working local data and messaging services in priority order
(master prompt §19 Phase 3, §6 D/F/G): DynamoDB, RDS, ElastiCache,
ELB + Route 53, SQS / SNS / EventBridge, Lambda + API Gateway. All ten
have real HTTP routes, durable Postgres-backed state (migration
`009_phase3.sql`, 23 `p3_*` tables), AWS-duplicate consoles, and a live
end-to-end walk. No Rust changes.

### What was built

- **DynamoDB** (`backend/internal/providers/dynamodb.go`,
  `backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/DynamoDbConsole.tsx`): real DynamoDB JSON API to
  DynamoDB Local when it answers (probe cached 60 s), else a durable
  Postgres-backed subset (CreateTable/PutItem/GetItem/Query/Scan/
  DeleteItem/DeleteTable; hash key only, no sort key/indexes, Scan
  capped at 100). Fidelity `API_EMULATED` via Local, `PARTIAL` via the
  subset; the console names the live backend on every response. Plain
  items and DynamoDB-typed items both accepted.
- **RDS** (`backend/internal/phase3/phase3.go`,
  `backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/RdsConsole.tsx`): each instance is a real
  Postgres database + role on the platform Postgres
  ("PostgreSQL-compatible, not Aurora"). Engine postgres 14/15/16 only
  (mysql/mariadb → 400). Master password generated, shown exactly once,
  only its SHA-256 hash stored. Snapshot = real `pg_dump -Fc` file;
  restore creates a NEW instance via `pg_restore`. Start/stop per
  instance is UNSUPPORTED → 409. Rollback on failure (role + DB dropped
  again).
- **ElastiCache** (`backend/internal/httpapi/phase3.go`,
  `phase3_redis.go`, `frontend/src/pages/ElastiCacheConsole.tsx`):
  lite mode provisions a real namespaced Redis database on the platform
  Redis (key prefix `cf-cache:<cluster-id>:` + best-effort ACL user).
  Engine redis only; full mode (dedicated container) → honest 503.
  Keys browser (read-only SCAN, max 100), set capped 64 KiB, flush
  deletes only that cluster's prefix, metrics from INFO.
- **ELB** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/ElbConsole.tsx`): SIMULATED with a working
  in-process round-robin to lab targets. Targets probed on demand
  (HTTP GET to `http://host:port<health_check_path>` when set, else TCP
  dial, 2 s timeout); only targets whose last probe said `healthy` are
  eligible. `/route` returns the target a request would go to; no
  traffic is actually proxied. DNS name is a local teaching name.
- **Route 53** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/Route53Console.tsx`): SIMULATED, durable
  zones/records in Postgres. A/CNAME/TXT only; one record set per
  (name, type); zone cannot be deleted while it holds records.
  `GET .../resolve` answers against stored records including `*.`
  wildcards; no DNS server serves the records to the lab network.
- **SQS** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/SqsConsole.tsx`): durable, Postgres-persisted
  (NOT in-memory). Receive hides the message until the visibility
  timeout (does not delete); at `max_receives` it moves to the DLQ
  instead of being returned; redrive re-enqueues (new message id).
  Retention enforced by purging expired messages. FIFO not built; no
  long polling.
- **SNS** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/SnsConsole.tsx`): durable topics/subscriptions/
  deliveries. Real fan-out to SQS queues, HTTP(S) endpoints (real POST,
  SSRF-guarded to loopback), Lambda functions (real invocation). Every
  delivery recorded. Email/SMS not built → 400.
- **EventBridge** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/EventBridgeConsole.tsx`): durable rules. Pattern
  subset (source / detail-type arrays + nested detail subset matching).
  Targets SQS/SNS/Lambda, validated at rule creation. ENABLED/DISABLED;
  AWS service events are NOT generated (PutEvents only). No schedule
  expressions, no input transformers.
- **Lambda** (`backend/internal/phase3/phase3.go` (`RunLambda`),
  `backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/LambdaConsole.tsx`): real execution in a
  sandboxed subprocess (python3/node runtimes that actually execute
  here). Timeout, output cap 64 KiB, `prlimit` memory cap, fresh temp
  workdir, minimal env, no host shell, uid 65534 (nobody) when the API
  runs as root. The result names the sandbox that actually applied.
  This is NOT container isolation and NOT AWS Lambda — the console and
  docs say so, and the old registry claim ("functions run in isolated
  Docker containers") was corrected to PARTIAL/subprocess. Versions/
  aliases; triggers from SQS and EventBridge.
- **API Gateway** (`backend/internal/httpapi/phase3.go`,
  `frontend/src/pages/ApiGatewayConsole.tsx`): REST/HTTP only
  (WebSocket not built). Lambda + SSRF-guarded HTTP integrations.
  Deploy snapshots resources into a stage; invoke via
  `/api/v1/gateway/{apiId}/{stage}/*` (platform auth required); every
  invocation logged. Lambda error → 502, timeout → 504.
- **Registry** (`backend/internal/providers/providers.go`,
  `backend/internal/httpapi/services.go`): SQS/SNS/EventBridge upgraded
  from registry-only SIMULATED to real `API_EMULATED` implementations;
  Lambda SIMULATED/docker → PARTIAL/subprocess; DynamoDB gained
  Query/Scan/DeleteItem/DeleteTable (`API_EMULATED`) with the fallback
  limitation; RDS/ElastiCache `PARTIAL`; ELB/Route 53 `SIMULATED`; API
  Gateway `PARTIAL`. `httpRouteFor` maps all new ops to real routes.
- **Consoles** (`frontend/src/pages/{DynamoDb,Rds,ElastiCache,Elb,
  Route53,Sqs,Sns,EventBridge,Lambda,ApiGateway}Console.tsx`,
  `frontend/src/App.tsx`): all 10 wired before the generic
  `services/:id` route; each mirrors its AWS console's layout/tables/
  detail tabs/wizards/action names in CloudForge branding, with
  fidelity badges and honest not-built notes. Render tests for all 10
  (`frontend/src/pages/phase3-consoles.test.tsx`).

### Bugs found live and fixed (not just UI)

1. **ELB health checks marked every target unhealthy.**
   `handleTGHealth` passed only the target's `address` (`"localhost"`)
   to `phase3.ProbeTarget`, dropping the stored `port` and the target
   group's `health_check_path` — the probe dialled a bare host and
   failed with "missing port in address", and the route correctly
   refused with 503 `no_healthy_targets`. Fixed by changing
   `ProbeTarget(ctx, address, port, healthCheckPath)`: a bare host is
   probed as an HTTP GET to `http://host:port<health_check_path>` when
   a path is set, else a TCP dial of `host:port`. Tests:
   `TestProbeTarget` now includes a live `httptest` server positive
   case and a 404-path negative case.
2. **RDS provisioning failed on role ownership.** The platform Postgres
   user `cloudforge` had `CREATEDB` but not `CREATEROLE` (sandbox
   setup), so `CREATE ROLE` failed with 42501 — the API honestly
   returned 502 `rds_provision_failed` with nothing persisted. After
   `ALTER USER cloudforge WITH CREATEROLE`, the next step failed:
   `CREATE DATABASE ... OWNER cf_walkdb` requires the provisioning
   user to be a member of the new role ("must be able to SET ROLE").
   Fixed in `RDSCreateDatabase`: after creating the role,
   `GRANT <role> TO <provisioning-user>` (from the DSN; the role
   creator has ADMIN OPTION). Test:
   `TestRDSCreateDropIntegration` (live Postgres, gated on
   `DATABASE_URL`). Deployment note: the platform Postgres user needs
   `CREATEDB` + `CREATEROLE`.
3. **DynamoDB Local was never used — the adapter sent unsigned
   requests.** The adapter posted with no `Authorization` header, so
   every call failed with `MissingAuthenticationToken` and the probe
   silently fell back to the Postgres subset (caught because the walk
   showed `backend: postgres-subset` throughout). Fixed by sending a
   dummy SigV4 `Authorization` + `X-Amz-Date` header — Local never
   validates the signature. Two subtleties found by direct probing:
   the credential must be alphanumeric (Local rejects
   `cloudforge-local` with `UnrecognizedClientException` — the hyphen
   is the problem; `cloudforgelocal` works), and Local's error body
   uses capital-M `Message`, which the adapter previously ignored
   (bare "400 Bad Request"). `do()` now returns a typed
   `DynamoError{Target, Type, Msg}`. Test: `fakeDynamo` now rejects
   unsigned requests like the real Local.
4. **Duplicate DynamoDB table returned 502 instead of 409.** Every
   adapter error was mapped to 502 `dynamodb_error`. Local returns
   `__type: "com.amazonaws.dynamodb.v20120810#ResourceInUseException"`
   (fully qualified — the first fix compared the bare name and
   missed it). Fixed: `CreateTable` maps that type to sentinel
   `ErrDynamoTableExists`, the handler returns 409 `already_exists`;
   the fake-server test asserts the sentinel for both the bare and
   qualified type names.
5. **TypeScript build broke on the Phase 3 consoles.**
   `AwsTable<T extends { id: string }>` rejected the consoles'
   `Record<string, unknown>` rows (TS2741), which in TS 7 also
   produced phantom TS6133 "unused variable" errors for names plainly
   used inside the same JSX subtree. Fixed by giving each console's
   `Rec` an `id: string` and having `listOf`/`itemsOf` synthesize an
   id from `name` or the row index when the API row has none;
   request-body objects wrongly typed `Rec` are now plain
   `Record<string, unknown>`. Also fixed: `TestServicesRegistry`
   still expected DynamoDB to be `registry-only` (updated to assert
   `available` + HTTP routes for all seven DynamoDB operations).
6. **Walk-script issues (script, not product):** the first re-run
   registered the same admin twice → 409 → empty token → everything
   401 (which incidentally proved users persist in Postgres across
   API restarts); the script now uses a per-run email suffix. The SQS
   delete step extracted `id` from the receive response, which
   carries `message_id`/`receipt_handle` instead → now uses
   `receipt_handle`. The Lambda timeout step expected 200; the API
   honestly returns 504 with `timed_out: true` → the script now
   expects 504.

### Verification (2026-11-22, this sandbox, lite mode)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all
  packages pass (with `DATABASE_URL` set so the Postgres integration
  tests run, including `TestPostgresPhase3Integration` and
  `TestRDSCreateDropIntegration`), `cargo test --workspace` 74 passed
  (Rust untouched), frontend build + vitest **99/99** (was 89/89; +10
  Phase 3 console render tests) + oxlint 0 warnings.
- **Live E2E walk — 120/120 checks pass** (script
  `/tmp/cf-live/walk-phase3.sh`, transcript
  `/tmp/cf-live/transcript-phase3.txt`; stack: API :8081, worker :8083,
  Rust engine :50051 SIMULATED, real Postgres 16, real Redis 7.0.15,
  DynamoDB Local :8000, host agent :8082 simulated, frontend dev :5173
  + Vite preview :8080). Walk: health (`lite: true`,
  `database_connected: true`, `redis_connected: true`);
  unauthenticated Phase 3 call → 401; DynamoDB via Local
  (`backend: dynamodb-local`, `fidelity: API_EMULATED`) create/Put/Get/
  Query/Scan, empty name → 400, duplicate → 409, delete item → GetItem
  404, delete table; RDS create 201 (real DB `walkdb` + role
  `cf_walkdb` verified in `pg_database`/`pg_roles`, master password
  present in the create response only, no `password_hash` leaked),
  mysql → 400, stop → 409, snapshot 201 (real `pg_dump -Fc` file,
  verified as "PostgreSQL custom database dump" on disk), restore-as-
  new 201, delete both (DBs + roles gone, count 0), get deleted → 404;
  ElastiCache create lite 201 (real prefixed keys verified in Redis
  with `--scan`), memcached → 400, full mode → 503, set/get/list keys,
  metrics, flush (prefix empty afterwards), delete; ELB create LB +
  target group + healthy (`:8081`) and unhealthy (`:1`) targets,
  health check 1/2 healthy, route → `localhost:8081`, empty address →
  400, delete TG/LB; Route 53 zone + A record, duplicate → 409,
  MX → 400, wildcard A, resolve hit + wildcard hit + miss, delete
  non-empty zone → 409, delete records + zone; SQS durable queues (row
  count verified in Postgres), send/receive (invisible while in
  flight), visibility-timeout expiry + re-receive, at max receives the
  message moves to the DLQ (`moved_to_dead_letter_queue: 1`), redrive
  back, delete by receipt handle, purge, delete queues; SNS topic,
  email → 400, non-loopback HTTP → 400 (SSRF guard), SQS subscribe,
  publish → 1/1 deliveries delivered, fan-out message received from
  the queue, deliveries list, unsubscribe, delete topic; EventBridge
  rule to SQS, missing target → 404, matching event → 1 matched + 1
  delivery + message in queue, non-matching → 0, disable → 0 matched,
  delete rule; Lambda python echoer invoke (output correct, sandbox
  named), unsupported runtime → 400, failing function → 200 with
  `status: error`, sleeper (1 s timeout) → honest 504 with
  `timed_out: true` and the invocation recorded, alias `prod` invoke
  by qualifier, invocations list; API Gateway create API + POST `/echo`
  Lambda resource, websocket integration → 400, deploy `prod`, invoke
  via `/api/v1/gateway/{api}/prod/echo` → Lambda output, undeployed
  stage → 404, HTTP `/health` resource + redeploy (200, existing stage
  updated), invoke → 200, request logs list; registry shows all 10
  services `available` with honest fidelity; frontend preview
  `/services/sqs` + `/services/lambda` 200, dev `/services/dynamodb`
  200, the three console titles found in the production bundle;
  DynamoDB fallback: DynamoDB Local killed, 65 s wait (probe cache is
  60 s), create table → `backend: postgres-subset`, `fidelity:
  PARTIAL`, Put/Get work, item row verified in `p3_dynamodb_items`,
  delete table. Frontend production build served on :8080 (Vite
  preview; nginx is Docker-only).
- Failure cases proving nothing shows READY on failure: RDS create
  with the platform user lacking `CREATEROLE` → 502 with nothing
  persisted (list empty, no DB/role created); ELB route with no
  healthy targets → 503 `no_healthy_targets`; Lambda timeout → 504
  with `timed_out: true`; DynamoDB duplicate table → 409; SQS bad
  queue name → 400; SNS non-loopback HTTP → 400; EventBridge missing
  target → 404; API Gateway undeployed stage → 404; ElastiCache full
  mode → 503 naming the missing container path.

### Fidelity summary

| Surface | Fidelity | Evidence |
|---|---|---|
| DynamoDB console | API_EMULATED (via DynamoDB Local) / PARTIAL (Postgres subset) | Live walk: Local backend named on every response; fallback verified with Local killed |
| RDS console | PARTIAL (real Postgres DB + role) | Live walk: real DB/role created + dropped; real `pg_dump -Fc` snapshot file; restore-as-new |
| ElastiCache console | PARTIAL (real namespaced Redis, lite) | Live walk: real prefixed keys in Redis; flush empties only the prefix |
| ELB console | SIMULATED (round-robin + health checks) | Live walk: 1/2 healthy, route picks the healthy target; 503 with none healthy |
| Route 53 console | SIMULATED (durable records) | Live walk: records + wildcard resolve; duplicate/non-empty-zone 409s |
| SQS console | API_EMULATED (durable Postgres) | Live walk: visibility timeout, DLQ move at max receives, redrive, delete by receipt |
| SNS console | API_EMULATED (durable Postgres, real fan-out) | Live walk: SQS fan-out delivered; email/non-loopback HTTP rejected |
| EventBridge console | API_EMULATED (durable Postgres rules) | Live walk: pattern match/miss, disabled rule matches nothing |
| Lambda console | PARTIAL (subprocess sandbox) | Live walk: real python execution, error + timeout recorded honestly |
| API Gateway console | PARTIAL (real routing + logs) | Live walk: Lambda + HTTP integrations invoked; undeployed stage 404; logs list |
| Generic service consoles | Per-service fidelity from registry | Ops table shows fidelity + route per operation |

### Honest limitations

- `docker compose config/up`, real KVM, real netlink, Terraform
  containers and nginx config syntax remain unverified here (no
  Docker/KVM/nginx) — as in all prior milestones.
- **DynamoDB Local is not in the Docker Compose files** (it runs on
  `:8000` on the dev host / sandbox only). On a real host, start
  DynamoDB Local (or point `DYNAMODB_ENDPOINT` at one) for the
  `API_EMULATED` path; without it the Postgres subset takes over
  honestly (`PARTIAL`). The dummy SigV4 credential is local-only and
  must never be sent to AWS.
- **RDS/ElastiCache full mode is not built**: no containerised
  per-instance Postgres and no dedicated Redis container — there is no
  Docker in this sandbox to verify either against. Full-mode
  ElastiCache create returns an honest 503 naming the missing
  container path; RDS always provisions on the platform Postgres.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io returns
  410). S3 object flows are unit-tested + live 503 only, as in Phase 2.
- ELB is SIMULATED: round-robin + health checks only; no traffic is
  proxied, the DNS name is not registered, no engine/nginx
  integration. Route 53 is SIMULATED: records are durable but no DNS
  server serves them to the lab network.
- Lambda is a subprocess sandbox, NOT container isolation and NOT AWS
  Lambda: no VPC attachment, managed scaling, layers, or
  execution-role model; do not run untrusted code from strangers. API
  Gateway has no WebSocket APIs, no API keys / usage plans.
- SQS has no FIFO queues and no long polling; SNS has no email/SMS
  delivery; EventBridge has no schedule expressions, no input
  transformers, and no AWS service events (PutEvents only). DynamoDB
  subset: hash key only, no sort key, no secondary indexes, Scan
  capped at 100, no pagination.
- The platform Postgres user needs `CREATEDB` + `CREATEROLE` for RDS;
  the sandbox `cloudforge` user now has both.
- Region scoping is display-only (all resources are `cf-local-1`).
- The loud startup check for "Postgres mode with no worker" (R2
  follow-up) is not built yet.
- Frontend on `:8080` was verified with Vite preview of the production
  build, not the Docker nginx image (no Docker here).

## Phase 4 — Containers and Developer Tools — 2026-11-22

Built working local containers and CI/CD in priority order (master
prompt §19 Phase 4, §6 H/I): ECR, ECS + Fargate, EKS (decision),
CodeBuild / CodePipeline / CodeDeploy, CloudFormation. All have real
HTTP routes, durable Postgres-backed state (migration
`010_phase4.sql`, 15 `p4_*` tables), AWS-duplicate consoles, and a
live end-to-end walk. No Rust changes; full-mode `registry:2`
compose addition is unverified (no Docker here).

### What was built

- **ECR** (`backend/internal/phase4/ecr.go`,
  `backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/EcrConsole.tsx`): durable Postgres catalogue +
  real image layers on local disk (content-addressed blobs, manifest
  + blob upload/download, tag listing via the Docker Registry HTTP
  API subset on `/v2/`). Full mode: a real `registry:2.8.3`
  container behind `ECR_REGISTRY_URL` (compose `optional` profile,
  not exposed on a host port). Push accepts manifest+blobs or the
  `layers` convenience shape; scan is an integrity scan (re-read +
  digest-verify, explicitly NOT a CVE scan); lifecycle rules
  (`keep_last_n_tagged` / `expire_untagged_days`, both rule shapes
  accepted) applied on push and on demand. Fidelity PARTIAL.
- **ECS + Fargate** (`backend/internal/phase4/phase4.go`
  `TaskRunner`, `backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/EcsConsole.tsx`): clusters, task definitions
  (family+revision, per-container cpu/memory), services (desired
  count, rolling deployments), tasks with real status from the
  runtime. The runtime is honest: the task command runs as a
  sandboxed subprocess (argv, timeout, output cap, `prlimit` memory
  cap, temp workdir, uid 65534) — no Docker container is started,
  no image is pulled. Fargate = the same runtime, `launch_type:
  "FARGATE"`, no cluster. **Reconcile circuit breaker**: 5+ task
  starts in 60 s pauses restarts (`reconcile_throttled`, cleared by
  a service update). Fidelity: ECS PARTIAL, Fargate SIMULATED.
- **EKS — DECIDED UNSUPPORTED** (`backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/EksConsole.tsx`): `GET /api/v1/aws/eks/info`
  returns the decision, the reason, and a 6-row Kubernetes learning
  path (Pod, Deployment, Service, ConfigMap/Secret, Namespace,
  Job/CronJob) mapped onto the ECS runtime. Reason: no
  Docker/Kubernetes in this sandbox to verify a cluster against;
  a managed-Kubernetes control plane would dominate lite mode;
  the concepts map directly onto the ECS runtime built here. A
  k3s-based EKS remains a possible future phase on a real Docker/KVM
  host, gated behind a capability check. No static diagram is
  presented as a cluster.
- **CodeBuild** (`backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/CodeBuildConsole.tsx`): real builds from
  local sources only (git clone when the source has `.git`, else a
  directory copy; buildspec inline or `buildspec.yml`; phases in
  order; real logs on disk; artifacts on disk; SUCCEEDED/FAILED
  with `failure_reason`). Clone runs as the API user and the tree
  is chowned to 65534; a failed clone falls back to a filesystem
  copy (see Bugs). Fidelity PARTIAL.
- **CodePipeline** (`backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/CodePipelineConsole.tsx`): source → build →
  deploy stages, executed for real. Source stage clones/copies and
  records the commit; build stage runs CodeBuild against that
  checkout; deploy is a real ECS rolling deployment, a real Lambda
  code update, or an honest Terraform failure
  (`terraform_unavailable`). A failed stage stops the execution.
  Fidelity PARTIAL.
- **CodeDeploy** (`backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/CodeDeployConsole.tsx`): applications,
  deployments with real status, rollback to the previous revision
  (ECS task def / Lambda code+version). Only a SUCCEEDED deployment
  can be rolled back. Fidelity PARTIAL.
- **CloudFormation** (`backend/internal/httpapi/phase4.go`,
  `frontend/src/pages/CloudFormationConsole.tsx`): a separate
  template engine with its own stack state — NOT Terraform. JSON/YAML
  parsing, parameter defaults, validate-template, change sets as a
  real diff (CREATE/UPDATE/DELETE/NO_CHANGE/UNSUPPORTED), create /
  update / delete with real provisioning and stack events, rollback
  on failure. 6 resource types (PARTIAL): SQS, SNS, DynamoDB
  (Postgres subset), Lambda (inline `Code.ZipFile`), S3 (MinIO),
  RDS (platform Postgres). Other types are UNSUPPORTED in the change
  set, never silently skipped.
- **Registry + catalog**: capabilities added for all eight services
  (`ecr`/`ecs`/`fargate`/`eks`/`codebuild`/`codepipeline`/
  `codedeploy`/`cloudformation`) with honest fidelity; `httpRouteFor`
  maps every new op to a real route; consoles wired into `App.tsx`
  before the generic `services/:id` route.
- **Env vars (new)**: `ECR_REGISTRY_URL` (empty = local-disk
  registry) and `CLOUDFORGE_BUILD_SOURCE_ROOT` (empty = workdir
  root). Documented in `.env.example`.

### Verification (2026-11-22, this sandbox, lite mode)

- `make verify` exit 0: `gofmt`/`go vet` clean, `go test ./...` all
  21 packages pass (with `DATABASE_URL` set so the Postgres
  integration tests run), `cargo test --workspace` 74 passed (Rust
  untouched), frontend build + vitest 106/106 (was 99/99; +7 Phase 4
  console render tests) + oxlint 0 warnings.
- Live lite stack (API :8081, worker :8083, Rust engine :50051
  SIMULATED, real Postgres 16, real Redis 7.0.15, frontend dev
  :5173 + Vite preview :8080): **walk script
  `/tmp/cf-live/walk-phase4.sh`, 73/73 checks pass, 0 fail**
  (transcript `/tmp/cf-live/transcript-phase4.txt`). Every console
  walked create → list → detail → action → delete, with at least
  one failure case per service proving nothing shows READY on
  failure (bad ECR name 400, task def without command 400,
  remote CodeBuild source 400, failing build FAILED with the real
  log, Terraform deploy honestly FAILED, unsupported CFN type 400,
  S3 stack CREATE_FAILED with rollback, unsupported-only change
  set not executable → 409). The ECS circuit breaker was exercised
  live (echo service throttles, task count bounded, scale-to-0
  clears it). On-disk evidence verified (ECR blobs + manifests,
  CodeBuild logs, ECS task logs). The sandbox VM was replaced
  during Phase 4 (second replacement this session); Postgres 16 +
  Redis 7.0.15 were reinstalled via apt and the fresh-DB migration
  (all 15 `p4_*` tables) was verified on API startup.

### Bugs found live and fixed

- ECR lifecycle silently ignored the console's shorthand rule shape
  (`{"keep_last_n_tagged": 1}`) — the handler only read
  `{"type": …, "count": …}`; the old image was never deleted and
  the repo then refused deletion with 409. Both shapes are now
  accepted (regression test added; the walk asserts the post-push
  list is exactly `['v2']`).
- Pipeline build stage: `git clone` of a chowned checkout failed
  with exit 128. Probed to the root cause: `-c safe.directory` and
  `GIT_CONFIG_*` env trust do not reach the `upload-pack` child a
  local clone spawns, and git 2.43 matches `safe.directory`
  against `<repo>/.git`, not the worktree. Fix: `p4GitCmd` passes
  both trust values via env (used for `rev-parse`), and both clone
  sites fall back to a filesystem copy when the clone fails
  (equivalent for a local-only source, logged in the build log).
  Regression test: a pipeline with a real git source now SUCCEEDS.
- ECS reconcile runaway loop: an instantly-exiting service
  restarted forever (3,200 task rows in ~2 min). Fixed with the
  circuit breaker (5 starts in 60 s → restarts pause,
  `reconcile_throttled`; a service update clears it). Regression
  test + walk step 7b; the console shows the throttle reason.
- ECR silently lowercased repo names (fixed: validate as given,
  uppercase → 400); ECR push rejected the `layers` shape (fixed:
  accepted, manifest synthesised server-side); ECS task-def
  register rejected per-container `cpu` (fixed: `cpu` /
  `memory_mb` fields added and validated); pipeline build stage
  only read `project_id` (fixed: resolves by id or name); deploy
  stage field names, logs field (`logs` not `log`), task stop
  route (`/stop`), run-task 201 (not 202), CodeDeploy flat
  revision fields, CFN planner NO_CHANGE, CFN template-as-string
  contract — all caught by the live walk and fixed in code (full
  list in `docs/IMPLEMENTATION_STATUS.md` §13).

### Honest limitations

- No Docker, KVM, or `/dev/kvm` in this sandbox: the full-mode
  `registry:2` container path is **unverified** (compose addition
  parses but has never run); first verified run must be on a real
  Docker host. Real VMs, containers, real netlink, Terraform
  containers and nginx config syntax remain unverified here.
- **No MinIO binary is obtainable in this sandbox** (dl.min.io
  returns 410) — S3 object flows are unit-tested + live 503 only;
  CloudFormation S3 buckets therefore CREATE_FAILED honestly here.
- **No DynamoDB Local in the current sandbox VM** (no Java after
  the VM replacement) — no Phase 4 flow needs it.
- ECR: no chunked/resumable uploads, no OCI index, no auth
  (local-only), no Docker Hub compatibility, no CVE scanning
  (integrity scan only); orphaned blobs are never garbage-collected.
- ECS/Fargate: no container is started and no image is pulled —
  the runtime is a sandboxed subprocess; no VPC networking, service
  discovery, or load balancer integration; desired count max 10;
  task timeout 3600 s; task logs capped at a 64 KiB tail.
- EKS is UNSUPPORTED by decision — no cluster, no kubectl, no
  managed node groups.
- CodeBuild: local sources only, no Docker-based builds, no build
  caching. CodePipeline: no manual approvals, no parallel stages.
  CodeDeploy: no canary/blue-green traffic shifting. CloudFormation:
  6 resource types only; no `DependsOn`, `Fn::GetAtt`, nested
  stacks, or drift detection; NO_CHANGE is best-effort; resources
  deleted by an update are NOT restored on rollback.
- Frontend on `:8080` was verified with Vite preview of the
  production build, not the Docker nginx image (no Docker here).
- Region scoping is display-only (all resources are `cf-local-1`).

## Phase 5 — Terraform Learning — 2026-11-22

Master prompt §19 numbering: Phase 5 = Terraform Learning (§9),
Phase 6 = Advanced services, Phase 7 = Production hardening. (The
Phase 0 audit's roadmap numbered these differently; the master prompt
wins — see `docs/IMPLEMENTATION_STATUS.md` §14.)

First-class main-navigation item (`/terraform-learning` and
`/services/terraform-learning`), not a documentation page. All
features are real backend behaviour; nothing is a mock of a working
component. Container execution delegates to the existing M7
Terraform manager — no second runner was built.

### Files created

- `backend/migrations/011_tflearn.sql` — 6 generic collections
  `p5_tflearn_projects/files/progress/runs/states/locks`
  (`id, name, data JSONB`; files `<project_id>:<path>`, progress
  `<user_id>:<lesson_id>` UNIQUE, runs
  `<project_id>:<op>:<nanos>`, states `<project_id>:v<version>`,
  locks = project id UNIQUE).
- `backend/internal/tflearn/doc.go` — package doc (content model,
  layer separation, delegation to M7, append-only state).
- `backend/internal/tflearn/lessons.go` — 55 lessons (L1 15, L2 20,
  L3 20) + 7 Level 4 projects (VPC, compute instance, S3 bucket,
  three-tier, serverless app, complete environment, reusable
  modules). Each lesson: explanation, real-world use case, code
  example, guided exercise, expected output, common mistakes,
  knowledge check, XP. L1 lessons name their matching lesson in
  the existing curriculum Terraform track
  (`curriculum/terraform/terraform.yaml`) via `curriculum_lesson`
  — no second copy of that track; L2–L3 are new content living
  only here. `SkillLevel()` derives Novice/Beginner/Intermediate/
  Advanced/Expert from real completed counts.
- `backend/internal/tflearn/hcl.go` — `ValidateHCL` (delimiter
  balance, block structure, undeclared variables/resources,
  forbidden `amazonaws.com`, embedded secrets, unsupported-type
  warnings), `FormatHCL` (indentation + spacing, heredocs
  untouched), `ExtractResources`, `ComputePlan` (local static
  diff), `DetectDrift` (declared vs state vs live). 9
  `cloudforge_*` + 17 `aws_*` supported types.
- `backend/internal/tflearn/provider.go` — 9 `cloudforge_*`
  resource types (vpc, subnet, security_group, instance, s3_bucket,
  iam_role, sqs_queue, dynamodb_table, lambda_function), each with
  arguments, attributes, mapped CloudForge API, fidelity, example;
  plus `ProviderInstallDocs` (learning provider, no registry
  download; AWS mode only via a local emulator; real AWS
  credentials refused).
- `backend/internal/tflearn/tflearn_test.go` — course-structure
  integrity (counts 15/20/20, 55 total, 7 projects, unique IDs,
  quiz answers in range, L1 curriculum mapping), HCL
  validate/format/heredoc, extract/plan/drift, provider
  completeness.
- `backend/internal/httpapi/tflearn.go` — 25 routes under
  `/api/v1/terraform-learning` (see below).
- `backend/internal/httpapi/tflearn_test.go` — 6 tests:
  `TestTFlearnDashboardAndCourses`,
  `TestTFlearnProvider`, `TestTFlearnProjectEditorFlow`,
  `TestTFlearnExecutionGating`, `TestTFlearnState`,
  `TestTFlearnOwnership`.
- `frontend/src/pages/TerraformLearning.tsx` — Dashboard, Courses
  (lesson detail + knowledge check), Projects & Editor (file
  explorer, textarea + HCL highlighting preview, save/validate/
  format/plan-preview, execution panel with confirm checkbox,
  state panel with lock/backup/restore/drift), Provider tab.
- `frontend/src/pages/terraform-learning.test.tsx` — 3 render
  tests (dashboard from real-shaped data incl.
  `terraform_unavailable` + lite mode; courses tab; local-check
  labelling).
- `docs/TERRAFORM_LEARNING.md` (new) — what is real per piece,
  curriculum reuse, layer/fidelity table, honest limits.

### Files changed

- `backend/internal/store/phase3.go` — 6 collections added to
  `P3Collections` (shared Phase 3 store, Postgres + in-memory
  fallback; no new store interface).
- `backend/internal/httpapi/terraform.go` — M7 refactor, no
  behaviour change: `enqueueTerraform` split into
  `enqueueTerraformRun` (creates run + enqueues job, no HTTP
  response) + wrapper preserving lab-endpoint responses;
  `terraformAvailability` extracted; `loadApprovablePlan` shared
  by the M7 apply handler and the tflearn apply path. Existing
  M7 tests pass unmodified.
- `backend/internal/httpapi/httpapi.go` — `registerTFlearnRoutes`
  called after `registerPhase4Routes`.
- `frontend/src/App.tsx` — routes `/terraform-learning` and
  `/services/terraform-learning` (nav already linked to the
  latter).
- `docs/IMPLEMENTATION_STATUS.md` — §14 (Phase 5 + §19
  renumbering note).
- `docs/SERVICE_CATALOG.md` — `terraform-learning` row rewritten
  for Phase 5; stale duplicate row removed.
- `docs/SERVICE_FIDELITY.md` — new "Terraform Learning" row
  (PARTIAL).
- `backend/cmd/e2e-fakes/main.go` — `CreateNAT`/`DeleteNAT`
  in-memory stubs (test support only; fixes a pre-existing Phase 2
  regression — see "Bugs found" below).

### API (25 routes, `/api/v1/terraform-learning`)

- `GET /dashboard` — real progress/projects/plans/applies/recent
  executions/achievements/skill level; `terraform_available`,
  `mode_lite`.
- `GET /courses`, `GET /courses/{level}/{lessonId}`,
  `POST /courses/{level}/{lessonId}/complete` — correct answer
  required; wrong answer → 200 `{correct:false,
  completed:false}`; lesson detail never leaks `correct_answer`.
- `GET /provider`, `GET /provider/resources`.
- Projects CRUD (`POST/GET /projects`, `GET/PUT/DELETE
  /projects/{id}`; per-user unique name 409; optional `template`
  seeds starter files; optional `lab_id` link verified
  owner-or-admin; initial state v1 empty on create; delete
  cascades files/runs/states/locks).
- Files (`GET /projects/{id}/files`, `POST.../files` upsert,
  `DELETE.../files` with `{path}` body; relative paths,
  `.tf/.tfvars/.md` only, no `..`).
- `POST.../validate` (200 valid / 400 invalid, labelled
  `"check":"local"`), `POST.../format` (persists formatted files,
  reports changed list).
- `POST.../plan-preview` (local static diff,
  `source:"cloudforge-local-diff"`, create/update/delete/no-op
  counts; creates a `plan-preview` run),
  `POST.../plans/{planId}/approve` (1 h TTL; a learning record,
  NOT a container apply gate).
- `POST.../executions` `{operation, plan_id?, confirm?}` —
  init/validate/plan/apply/destroy delegate to M7 via
  `enqueueTerraformRun` with the project's concatenated `.tf`
  files; apply requires `confirm=true` + approved unexpired M7
  plan; destroy requires `confirm=true`; no lab linked → 409
  `lab_required` (FAILED run recorded); Docker absent → 503
  `terraform_unavailable` (FAILED run recorded).
- `GET.../projects/{id}/executions`, `GET /executions/{id}`
  (status/log refreshed from the linked M7 run),
  `POST.../projects/{id}/executions/{runId}/approve` (approves an
  M7 plan run, mirroring M7 approve checks).
- State: `GET.../state` (refreshes from the linked lab's latest
  succeeded M7 plan, new version only when addresses differ),
  `GET.../state/history`, `POST.../state/lock` (10 min),
  `POST.../state/unlock` (`{force}`; holder or admin only for
  active locks), `POST.../state/backup` (new version),
  `POST.../state/restore` `{version}` (creates a NEW version
  copied from the restored one; history never rewritten),
  `GET.../state/drift` (declared vs recorded state vs live lab
  resources mapped kind→`cloudforge_*` address; no lab → live
  empty).
- Ownership everywhere: entities carry `owner_id`; non-admin sees
  only own (404 for others' projects, no existence leak); admins
  see all. Lists are list-then-filter in Go (Phase 3 pattern).

Two plan concepts, kept separate: (a) local plan preview = static
diff, approving it is a learning record only; (b) container apply
gate = approved, unexpired M7 plan + `confirm=true`. State is
append-only: restores/backups create new versions; never
string-replace state.

### Test results (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean.
- `go test ./...` — all 22 packages pass (`DATABASE_URL` with the
  dev password + `REDIS_ADDR=localhost:6379`,
  `GOTOOLCHAIN=local`, `-count=1`): achievements, auth, cache,
  config, courses, curriculum, hostagent, httpapi (29.3s),
  jobs, labs, networkclient, phase3, phase4, providers, quota,
  ratelimit, security, store, terminal, terraform, tflearn,
  validation. Log: `/tmp/cf-live/go-test-phase5b.log`.
- `cargo test --workspace` — 74 passed, 0 failed (clippy clean
  per M4; Rust untouched by Phase 5).
- Frontend — build passes, vitest 109/109 (was 106/106, +3 new),
  oxlint 0 warnings / 0 errors.
- `make verify` — exit 0 (run twice; second run after the
  E2E-fake fix). Logs: `/tmp/cf-live/p5/make-verify.log`,
  `/tmp/cf-live/p5/make-verify2.log`.
- `tests/e2e` — passes (6.9s) after the fake-engine fix below.
  Log: `/tmp/cf-live/p5/e2e2.log`.

### Live walk (2026-11-22, lite mode; script `/tmp/cf-live/p5/walk-phase5.sh`, transcript `/tmp/cf-live/p5/walk.log`)

Rebuilt API `:8081` + worker `:8083` + Rust engine `:50051`
(SIMULATED) + real Postgres 16 `:5432` + Redis 7 `:6379` +
Vite dev `:5173` and preview `:8080`. 19 steps, all PASS
(`FAIL=0`):

0. health + readyz — 200, `lite:true`, engine SIMULATED,
   curriculum seeded 14/70/84/6/15.
1. Register user — 201; unauthenticated dashboard — 401.
2. Dashboard fresh — 200: `total_lessons:55,
   completed_lessons:0, skill_level:"Novice",
   pending_exercises:55`, 0 projects.
3. Courses — 200: levels `[15,20,20]`, 7 projects.
4. Lesson `l1-13-plan` — 200; no `correct_answer` in payload.
5. Knowledge check — wrong answer (index 0): 200
   `completed:false`; right answer (index 1): 200
   `completed:true`; dashboard then `completed_lessons:1`.
6. Provider resources — 200: 9 types, all with
   `cloudforge_api` and `fidelity`.
7. Create project from `p4-01-vpc` template — 201;
   `main.tf` seeded.
8. Save invalid HCL (`var.missing`) — 400 naming
   `var.missing`; path traversal `../escape.tf` — 400.
9. Save valid cross-file reference (`extra.tf` →
   `cloudforge_vpc.main` in `main.tf`) — 201.
10. Local validate — 200 `valid:true`; format fixed
    `messy.tf` (reported in changed list).
11. Plan preview — 200, `source:"cloudforge-local-diff"`, 4
    creates (`cloudforge_vpc.main`,
    `cloudforge_subnet.public_a`, `cloudforge_subnet.extra`,
    `aws_vpc.m`); approve — 200.
12. Apply without `confirm` — 409; destroy without
    `confirm` — 409; plan without a linked lab — 409.
13. Launch lab `ec2-basics` — 202, READY after 3s; link to
    project — 200.
14. Container plan with lab, no Docker — 503
    `terraform_unavailable` ("Docker socket
    /var/run/docker.sock not present"), FAILED run recorded in
    execution history (plus the earlier 409 lab_required FAILED
    run from step 12).
15. State — v1 → backup v2 → restore v1 as v3; history
    `[1,2,3]`.
16. State lock — 200; double-lock — 409 `state_locked`;
    unlock — 200.
17. Drift — 200: 4 declared missing, 1 unmanaged live
    `cloudforge_vpc.<lab-vpc-id>`.
18. Frontend — Vite dev `/terraform-learning` 200 and
    `/services/terraform-learning` 200; production preview
    `:8080/terraform-learning` also 200.
19. Cleanup — delete project 200; destroy lab 200
    (lab reached DESTROYED; walk users removed from the DB).

### Bugs found and fixed

1. **HCL validation was per-file, so valid projects were
   refused** (found live by test). Saving `extra.tf`
   referencing `cloudforge_vpc.main` (declared in `main.tf`)
   failed "reference to undeclared resource". HCL is
   project-scoped: save and plan-preview now validate the
   concatenated project configuration; the per-file view reports
   syntax issues only. Regression test:
   `TestTFlearnProjectEditorFlow` (cross-file save succeeds;
   genuinely undeclared `var.missing` still 400s).
2. **E2E regression, pre-existing since Phase 2** (found during
   Phase 5 verification, not caused by Phase 5). `tests/e2e`
   failed at `create gateway nat-1` with `CreateNAT not
   implemented`: Phase 2 made NAT-gateway creation call the
   engine, but the `e2e-fakes` fake engine never implemented
   `CreateNAT`/`DeleteNAT`. Added both as in-memory stubs in
   `backend/cmd/e2e-fakes/main.go` (test support only — no
   production code changed); the E2E then passes.
3. **RDS integration test "failure" was environmental** (not a
   product bug). The first full `go test ./...` run failed only
   on `TestRDSCreateDropIntegration` with SASL auth failure:
   the run used a password-less `DATABASE_URL`, but the sandbox
   Postgres role requires a password over TCP
   (`scram-sha-256`). The dev DSN with the password (recorded
   in `/tmp/cf-live/walk-phase4.sh`) makes the whole suite pass,
   including the RDS integration test. No Phase 3 code or test
   was changed.

### Honest limitations / still open

- Container execution is **unverified in this sandbox** (no
  Docker): the success path is implemented via M7 and
  unit-tested with the M7 stub runner, but no real `terraform
  plan/apply` ran here. In lite mode execution is unavailable
  by design (no Docker).
- No Terraform Cloud / remote backend / module registry; no
  language-server autocomplete; the local formatter is
  indentation + spacing, not `terraform fmt`.
- Multi-file projects are concatenated into one document for
  container execution (the M7 worker writes the payload as the
  lab's `main.tf`); local validation/format keep working per
  file.
- State refreshes from the linked lab's latest succeeded
  **plan**; after an apply, re-plan to refresh recorded state.
- The plan preview is a LOCAL static diff and approving it is a
  learning record, not a container apply gate — the apply gate
  is the M7 approved plan + confirmation.
- The walk script had two of its own bugs (lab field is
  `state`, not `status`; lab destroy is
  `POST /labs/{id}/destroy`, not DELETE) — fixed in the script;
  the product was correct in both cases.

## Phase 6 — Advanced services — 2026-11-22

Master prompt §19 numbering: Phase 6 = Advanced services (§6 K–N +
remaining J/E), Phase 7 = Production hardening. Priority order
(binding): Analytics, ML/AI, Migration/data transfer, Security
(remaining §6 E), Advanced networking, Management/Governance
(remaining §6 J). Every service does something real locally and is
labelled honestly; a static explainer page does not count, and no
number displayed comes from anywhere but a real store, log, or
computation in the run.

### Files created

- `backend/migrations/012_phase6.sql` — 37 tables `p6_*` (same
  generic shape as 009–011).
- `backend/internal/phase6/phase6.go` — Athena SQL validation,
  OLS linear regression (`TrainLinear`, builtin `housing`/`study`
  datasets), Bedrock template response, Kinesis shard hashing,
  WAF/firewall evaluation, envelope crypto (AES-256-GCM), cost
  model.
- `backend/internal/phase6/phase6_test.go` — SQL validation,
  housing R² ≥ 0.9 + prediction range, degenerate-data error,
  WAF/firewall evaluation incl. default deny, envelope round-trip
  + wrong-key failure, exact cost total, shard stability, Bedrock
  labelling.
- `backend/internal/httpapi/phase6.go` — ~124 handlers,
  `registerPhase6Routes` (Athena, Glue, Kinesis, Firehose,
  QuickSight, SageMaker, Bedrock, DMS, DataSync, Transfer info,
  Migration Hub, GuardDuty, Security Hub, Inspector, Config, WAF,
  KMS, Secrets Manager, VPC Peering, Transit Gateway, VPC
  Endpoints, Network Firewall, Flow Logs, SSM, Trusted Advisor,
  Cost Explorer/Budgets, Organizations) + `POST
  /aws/athena/execute` (ad-hoc, added for the query editor).
- `backend/internal/httpapi/phase6_test.go` — 11 `TestPhase6*`
  tests (Athena validation, Kinesis ordering, Firehose Postgres
  without DB, SageMaker notebook + training + endpoint, Bedrock
  labelling, KMS/Secrets round-trips + rotation + in-use delete,
  WAF + GuardDuty incl. the 6-failed-logins finding, DataSync
  local copy + path escape, misc honest paths).
- `frontend/src/pages/AnalyticsConsoles.tsx` — Athena, Glue,
  Kinesis, Firehose, QuickSight consoles.
- `frontend/src/pages/MlConsoles.tsx` — SageMaker, Bedrock
  consoles.
- `frontend/src/pages/MigrationConsoles.tsx` — DMS, DataSync,
  Transfer (UNSUPPORTED), Migration Hub consoles.
- `frontend/src/pages/SecurityConsoles.tsx` — GuardDuty, Security
  Hub, Inspector, Config, WAF, KMS, Secrets Manager consoles.
- `frontend/src/pages/NetworkingConsoles.tsx` — VPC Peering,
  Transit Gateway, VPC Endpoints, Network Firewall, Flow Logs
  consoles.
- `frontend/src/pages/ManagementConsoles.tsx` — SSM, Trusted
  Advisor, Cost Explorer, Organizations consoles.
- `frontend/src/pages/phase6-consoles.test.tsx` — 27 render
  tests, one per console (stubbed fetch + localStorage, retries
  off, as in `phase3-consoles.test.tsx`).

### Files changed

- `backend/internal/store/phase3.go` — all 37 Phase 6
  collections in `P3Collections` + unique-name collections in
  `P3UniqueName`.
- `backend/internal/httpapi/httpapi.go` — `registerPhase6Routes`,
  `Deps.P6MasterKey` / `Server.P6MasterKey`, failed-login audit
  (`auth.login_failed` with client IP + email); failed-login
  actor changed from the literal `"anonymous"` to `""` (stored
  as NULL) — see bug 1 below.
- `backend/cmd/api/main.go` — `kmsMasterKey()` (`CLOUDFORGE_KMS_MASTER_KEY`
  else the JWT secret), wired as `P6MasterKey`.
- `backend/internal/providers/providers.go` — KMS updated from
  `kms-simulator`/SIMULATED to `cloudforge-kms`/PARTIAL
  (durable keys); Phase 6 capabilities for all services above.
- `backend/internal/httpapi/services.go` — catalog rows updated
  (kms, secretsmanager, waf, guardduty, config, organizations,
  ssm, costexplorer, budgets, trustedadvisor, athena, glue,
  kinesis, quicksight, sagemaker, bedrock, dms, datasync,
  transfer); new rows (firehose, securityhub, inspector,
  networkfirewall, transitgateway, vpcpeering, vpcendpoints,
  flowlogs, migrationhub); `httpRouteFor` mappings for all
  Phase 6 ops.
- `backend/internal/httpapi/vpc.go` — `syncSGToEngine` retries
  once after `ensureEngineNetwork` when the engine reports the
  lab network missing (see bug 2).
- `backend/internal/store/store_integration_test.go` —
  anonymous-actor audit regression test in
  `TestPostgresIntegration`.
- `frontend/src/App.tsx` — 27 Phase 6 routes before the generic
  `services/:id` route.
- `docs/IMPLEMENTATION_STATUS.md` (§15), `docs/SERVICE_CATALOG.md`
  (Mirrors/Differs + fidelity for every Phase 6 service),
  `docs/SERVICE_FIDELITY.md` (25 Phase 6 rows; KMS SIMULATED →
  PARTIAL).

### Fidelity table (delivered)

| Service | Fidelity | Honest note |
|---|---|---|
| Athena | PARTIAL | PostgreSQL SQL, not Presto/Athena |
| Glue | PARTIAL | Real crawls; catalog only, no ETL jobs |
| Kinesis | API_EMULATED | Durable Postgres streams |
| Firehose | PARTIAL | Real delivery records; S3 needs MinIO |
| QuickSight | PARTIAL | Charts from live query results only |
| SageMaker | PARTIAL | Pure-Go OLS linear regression; no sklearn here |
| Bedrock | SIMULATED | Template playground; no real model |
| DMS | PARTIAL | Full-load only, no CDC |
| DataSync | PARTIAL | Real copy + SHA-256 verification |
| Transfer Family | UNSUPPORTED | No sshd/sftp-server on the host |
| Migration Hub | PARTIAL | Aggregation only |
| GuardDuty | PARTIAL | Real audit-log evidence only |
| Security Hub | PARTIAL | Aggregation only |
| Inspector | PARTIAL | Real resource checks; no CVE scanning |
| Config | PARTIAL | 3 rule types, real evaluation |
| WAF | SIMULATED | Rules really evaluated; evaluation only |
| KMS | PARTIAL | Durable local envelope encryption |
| Secrets Manager | PARTIAL | Encrypted at rest; no auto-rotation |
| VPC Peering | SIMULATED | Real route entries; no engine peering RPC |
| Transit Gateway | SIMULATED | Real route entries; no engine TGW RPC |
| VPC Endpoints | SIMULATED | Endpoint records; teaching DNS name |
| Network Firewall | SIMULATED | Rules really evaluated; engine sync best-effort |
| Flow Logs | SIMULATED | Engine-telemetry snapshots only |
| SSM | PARTIAL | Needs a host agent (honest 503 without one) |
| Trusted Advisor | PARTIAL | 5 real checks |
| Cost Explorer | SIMULATED | Educational estimates, not an AWS bill |
| Budgets | PARTIAL | Alert evaluated on every read |
| Organizations | PARTIAL | Real users; no AWS accounts |

### Verification (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean; `go test ./...` all packages pass (23
  packages with tests); `DATABASE_URL` set so the Postgres
  integration tests run; `cargo test --workspace` 74 passed
  (Rust untouched); frontend build + vitest 136/136 (was
  109/109; +27 Phase 6 console render tests) + oxlint 0
  warnings; `make verify` exit 0.
- Live lite walk against a rebuilt API (:8081) + worker (:8083)
  + engine (:50051 simulated) + Postgres 16 + Redis 7.0.15 +
  frontend dev :5173 / Vite preview :8080: **walk script
  `/tmp/cf-live/p6/walk-phase6.sh`, 174/174 checks pass, 0 fail**
  (transcript `/tmp/cf-live/p6/walk.log`). Highlights, all from
  real responses: Athena DELETE and stacked statements → 400,
  duplicate name → 409; Glue Postgres crawl SUCCEEDED, 114
  tables found; Kinesis 2 records in sequence order,
  `after_sequence` returns only record 2; Firehose Postgres
  delivery DELIVERED, S3 delivery 502 FAILED (MinIO down);
  QuickSight chart from the live query (`a=5, b=3`); SageMaker
  cell `print(2+2)` → `4`, housing job COMPLETED R²=0.9997
  RMSE=1742.58, endpoint prediction 308580.18 for 1500 sqft/3br,
  degenerate custom job FAILED with the real error; Bedrock
  invoke labelled SIMULATED; DMS platform→RDS copy
  rows_copied=3 (verified with psql in the target DB: 3 rows);
  DataSync 2 files / 2 SHA-256 verified + content diff clean;
  GuardDuty credential-stuffing HIGH finding (20 failed logins
  for 4 distinct accounts from ::1 — the shared audit log
  escalated the rule across walk runs), rescan dedupes 0,
  Security Hub open_findings=1; Inspector lab assessment
  COMPLETED with open-SG HIGH + unattached-SG findings; Config
  evaluate non-compliant on the open-SSH SG; WAF `/admin` →
  block, `/home` → allow; KMS rotate to v2 (old ciphertext
  still decrypts), delete in-use key → 409; peering A–B with
  real route entries in both VPC configs, routes removed on
  delete; TGW delete refused 409 while attached; Flow Logs
  capture wrote 6 records, all `engine-telemetry`; SSM
  inventory/run command honestly 503 without a host agent;
  Advisor created open-SG HIGH, unattached-SG LOW and
  quota-usage MEDIUM findings; Cost Explorer total $0.00016,
  budget alert=false at $100000 and alert=true under the real
  total; Organizations duplicate member 409, parent delete
  refused 409 with a child; cleanup deleted every created
  resource and destroyed the lab (DESTROYED).

### Bugs found live and fixed (not just UI)

1. **Failed-login audit silently dropped (GuardDuty blind).**
   `handleLogin` audited failures with actor `"anonymous"`;
   `PostgresAuditStore.Record` casts the actor to UUID, so every
   insert failed with `invalid input syntax for type uuid:
   "anonymous"` (WARN in the API log only) and GuardDuty scans
   returned `failed_logins_scanned: 0` after 6 real failed
   logins. The in-memory audit store in the unit tests has no
   UUID cast, so `TestPhase6WAFAndGuardDuty` passed while the
   live path was broken. Fixed: audit with actor `""` (NULL) +
   regression test in `TestPostgresIntegration`.
2. **SG rule add 502'd when the lab had no subnet.**
   `syncSGToEngine` called engine `CreateFirewallPolicy` with
   `NetworkId: labID`, but the engine network is created lazily
   by subnet creation only. Fixed with the retry-once-after-
   `ensureEngineNetwork` pattern subnet creation already uses.
3. **Flow Log capture always 503'd.** The handler sent an empty
   `CollectTelemetryRequest`; the engine requires `RequestMeta`
   on every RPC (`INVALID_ARGUMENT: meta is required`). Fixed:
   resolve the lab from the group's VPC resource and pass
   `Meta(...)` with a fresh idempotency key per capture.
4. **Flow Log capture read the wrong field.** First fix read
   `vpc_resource_id`; the group stores the VPC id as `vpc_id`.
   Fixed to `vpc_id`.
5. **KMS key version invisible to the console.** The backend
   stores `key_version`; the console reads `version`. Fixed:
   `version` is now stored alongside `key_version` at create
   and rotate; the unit test asserts both are 2 after rotation.
6. **Walk-script bugs (script, not product):** lab create is
   202 with the id under `lab.id` (not top-level `id`); response
   field names (Glue run status `SUCCEEDED`, Bedrock `output`,
   Security Hub `open_findings`, Config `non_compliant` list,
   Advisor `findings`, budget `alert` on GET not create, KMS
   `key_version`); training-job names are globally unique and
   jobs have no delete route, so the walk uses per-run names;
   the GuardDuty audit log is global across runs, so the
   finding escalates from brute-force to credential stuffing as
   distinct walk users accumulate; python asserts in the walk
   now count as FAILs (previously silent tracebacks on stderr).
   Also `pkill -f` self-match when restarting the API (same
   class as Phase 4 bug 16) — restarts now kill by PID.

### Honest limitations / still open

- No Docker, KVM, or `/dev/kvm` in this sandbox: real VMs,
  containers, real netlink, Terraform containers and nginx
  config syntax remain unverified here. The Rust engine default
  backend is SIMULATED; its real rtnetlink/nftables backend
  compiles but is unverified.
- No MinIO binary obtainable (dl.min.io returns 410) — S3
  object flows unit-test + live-503 only; Firehose S3 delivery
  and the Inspector S3 check fail/skip honestly here.
- No sklearn — SageMaker training is pure-Go OLS linear
  regression on tiny built-in datasets, labelled as such.
- No sshd/sftp-server — Transfer Family UNSUPPORTED by
  decision.
- No engine peering/Transit Gateway RPC — peering/TGW are
  SIMULATED with real route-table entries in lab state.
- DMS full-load only (≤5000 rows/table), no CDC; DataSync
  local paths confined to the workdir; WAF evaluated only via
  `/evaluate`; Flow Logs are engine-telemetry snapshots; KMS/
  Secrets are durable local envelope encryption, not AWS
  KMS/HSM; Cost Explorer is educational estimates; Bedrock is a
  SIMULATED template playground.
- Training jobs, GuardDuty detectors and Inspector assessments
  have no delete route (jobs/assessments are immutable
  history); training-job names are globally unique.
- Config evaluates 3 rule types only; SSM needs a host agent;
  Trusted Advisor runs 5 checks only.
- Test-ordering fragility (pre-existing, found during Phase 6
  verification): the hostagent disk-capacity check enforces
  16 GiB against the lab volume path
  (`/var/lib/cloudforge/labs`) when it exists, but skips when
  it does not exist (`DiskFreeGB == 0`). This sandbox's overlay
  is 7.5 GiB, so `make verify` fails
  `TestLibvirtUnavailableInSandbox` /
  `TestInstanceCreateStartsVMAndRebootWorks` once that
  directory exists (the Phase 6 walk created it via DataSync
  sources + SageMaker artifacts). The walk cleanup now removes
  the tree and `make verify` passes again. On a real host with
  ≥16 GiB free this does not trigger. No Phase 6 code changed
  the check.
- Frontend on `:8080` was verified with Vite preview of the
  production build, not the Docker nginx image (no Docker
  here).
- Region scoping is display-only (all resources are
  `cf-local-1`).

## Phase 7 — Production hardening — 2026-11-22

The final v2 phase. Worked through the Phase 0 audit §5 risk
checklist (R1–R14) and master prompt §15/§21. No working feature
was broken; every hardening change kept the existing suites green
and the lite stack working end-to-end.

### What was built

- **Engine auth (R1)** — shared-secret token on gRPC metadata
  `x-cloudforge-engine-token`, enforced on every RPC including
  Health in the Rust server (`EngineAuthInterceptor`,
  constant-time compare) and the Go client (`NewWithToken`).
  Engine fails closed without a token unless `CLOUDFORGE_DEV=1`.
  `:50051` no longer published on the host in either compose
  file. mTLS was **not** built — documented next step.
- **JWT revocation (R3)** — `jti` on every token; Revoker
  interface with memory/Redis/Postgres backends; Postgres
  denylist (`revoked_tokens`/`issued_tokens`, migration 013)
  checked on every authenticated request. `POST
  /api/v1/auth/logout` revokes the caller's `jti`; admin can
  revoke all of a user's sessions (`POST
  /api/v1/admin/users/{id}/sessions/revoke`). Dual-verify
  (bcrypt-era users) untouched.
- **Fail-closed secrets (R4)** — `ValidateSecrets()`: API and
  worker refuse to start in non-dev mode when a production secret
  is unset or still a known dev default, naming the variable. Dev
  defaults require the explicit `CLOUDFORGE_DEV=1` opt-in.
  Grafana admin password is now required by compose (was an
  empty default).
- **`/metrics` restriction (R5)** — token auth (`X-Metrics-Token`
  or `Authorization: Bearer`); loopback-only when no token is
  configured. nginx returns 404 for `/metrics`. Prometheus
  scrapes with the token.
- **Sessions/CSP (R6)** — `HttpOnly`, `SameSite=Lax` cookie
  `cf_session` (`Secure` only when `CLOUDFORGE_TLS=1`); the
  `Authorization` header path still works for API clients/tests
  during transition. CSP + `X-Content-Type-Options` +
  `X-Frame-Options: DENY` + `Referrer-Policy` +
  `Permissions-Policy` in the API and nginx; HSTS only when TLS
  is on. `logout()` now POSTs `/auth/logout`.
- **Trusted proxy / per-user limits (R7, R8)** — XFF honoured
  only from loopback, private (RFC 1918/ULA), or explicitly
  configured trusted proxies; otherwise the peer IP is used.
  Per-user limiter (default 300/min) in addition to per-IP;
  unauthenticated requests skip it.
- **Audit export (R10)** — export jobs: create/list/download
  (CSV/JSON, cap 10,000 events), files under
  `<workdir>/audit-exports/` with `.meta.json` sidecar, retention
  newest 7 by default. Worker 90-day cleanup exports to
  `audit-archive-<date>.csv` **before** deleting; a failed export
  blocks the delete. CloudTrail console gained an "Export jobs"
  card.
- **Terraform isolation (R11)** —
  `CLOUDFORGE_TF_NETWORK_MODE=isolated` runs the container with
  `--network none` (no egress at all); default `bridge` keeps the
  emulator route and only approximates isolation.
- **TLS option (R12)** — opt-in only: `scripts/gen-tls-cert.sh`
  (self-signed cert, refuses to overwrite),
  `deployments/compose/docker-compose.tls.yml` (replaces the
  frontend nginx config with `frontend/nginx.tls.conf`; host
  `:443` → container `:8443`, `:8080` redirects to HTTPS).
  `CLOUDFORGE_TLS=1` enables API HSTS + Secure cookies.
- **Full-mode resource limits** — added (previously unlimited):
  API 512M/1.0 CPU, worker 256M/0.50, engine 256M/0.50, frontend
  256M/0.50, Postgres 1G/1.0, Redis 256M/0.50. Lite limits
  unchanged (1,280 MiB total).
- **Docs (§20)** — created `docs/ARCHITECTURE.md`,
  `docs/SECURITY.md`, `docs/DEPLOYMENT.md`, `docs/TESTING.md`,
  `docs/KNOWN_LIMITATIONS.md`, `docs/ROADMAP.md`; updated
  `docs/IMPLEMENTATION_STATUS.md` (§5 risk table, §7, new §16),
  `docs/security/model.md`, `docs/api/reference.md`,
  `docs/TERRAFORM_LEARNING.md`, `docs/SERVICE_CATALOG.md`,
  `docs/SERVICE_FIDELITY.md`.

New API routes: `POST /api/v1/auth/logout`, `POST
/api/v1/admin/users/{id}/sessions/revoke`, `POST
/api/v1/audit/export/jobs`, `GET /api/v1/audit/export/jobs`,
`GET /api/v1/audit/export/jobs/{id}`. New env vars:
`CLOUDFORGE_DEV`, `CLOUDFORGE_ENGINE_TOKEN`,
`CLOUDFORGE_METRICS_TOKEN`, `CLOUDFORGE_TLS`,
`CLOUDFORGE_TRUSTED_PROXIES`, `CLOUDFORGE_API_USER_RATE_LIMIT`
/ `_WINDOW`, `CLOUDFORGE_AUDIT_EXPORT_RETENTION`,
`CLOUDFORGE_TF_NETWORK_MODE`, `GRAFANA_ADMIN_PASSWORD`.

### Verification (2026-11-22, this sandbox)

- `gofmt`/`go vet` clean; `cargo fmt --all --check` clean;
  `cargo clippy` clean except pre-existing workspace warnings.
- `go test ./...` — all 23 packages pass (Postgres + Redis live).
- `cargo test --workspace` — **75 passed, 0 failed** (74 + 1 new
  `grpc_auth` test).
- Frontend: build passes, vitest **138/138** (was 136/136),
  oxlint 0 warnings.
- `make verify` — **exit 0**.
- `tests/e2e` — `TestEndToEnd` **PASS** (14.8 s).
- Live lite walk (`/tmp/cf-live/p7/walk-phase7.sh`, engine
  `:50051` + API `:8081` + worker `:8083` + frontend preview
  `:8080`, real Postgres 16 + Redis 7, `CLOUDFORGE_DEV=1`) —
  **FAIL=0**. Real curl responses for every hardening item:
  fail-closed API + engine startup (errors name
  `CLOUDFORGE_JWT_SECRET` / `CLOUDFORGE_ENGINE_TOKEN`); engine
  gRPC up with the right token, down with a wrong token;
  `/metrics` 401 without/wrong token, 200 with token (both
  header forms), series present; CSP/XCTO/XFO present, no HSTS
  over plain HTTP; `cf_session` cookie set + cookie-only auth;
  per-user limit 429 on the 6th request with `Retry-After`,
  other user unaffected; spoofed-XFF auth limit 429 on the 11th
  bad login; logout revokes → 401; admin session revoke → 401,
  admin token still works; lab `ec2-basics` → READY in ~3 s →
  DESTROYED; one flow per phase (IAM evaluate, SQS send/receive,
  ECR create, Terraform Learning courses, Athena execute,
  GuardDuty detector, Cost Explorer); audit export job
  create/list/download with real rows; frontend `/`,
  `/services`, `/services/cloudtrail` 200.
- Backup/restore round-trip re-verified: 248 KB dump via
  `scripts/backup.sh`; `scripts/restore.sh --force` into a
  scratch database restored 3 users, 26 audit rows, 3 revoked
  tokens, 13 migrations — matching the live DB; scratch DB
  dropped.

### Bugs found live and fixed (walk harness, not product code)

- Engine `NETWORK_ENGINE_ADDR` must be an IP literal: the walk
  exported `localhost:50051`, which Rust `SocketAddr` parsing
  rejects, so the engine refused to start. Fixed in the walk
  (engine gets `0.0.0.0:50051`); documented in
  `docs/KNOWN_LIMITATIONS.md`.
- `-seed-curriculum` is fatal without a curriculum dir: the walk
  ran from `/home/hatch`, so the API exited after "curriculum
  directory could not be resolved". By design; the walk now sets
  `CURRICULUM_DIR` explicitly.
- Per-user rate-limit accounting: the first walk run forgot the
  cookie `/auth/me` call had consumed 1 of the 5 budget — the
  5th GET 429'd correctly. Test expectation fixed, not the
  limiter.

### Honest limitations / still open

- mTLS not built (token auth instead — documented next step).
- ECR `registry:2` still has no auth (local-only).
- Terraform bridge mode still approximates isolation (isolated
  mode is the real one); no network-namespace + emulator-allowlist
  mode, no HCL fuzzing.
- HSTS/TLS compose override unverified in sandbox (no
  Docker/nginx); cert generation verified with openssl only.
- nginx config syntax unverified (no nginx binary).
- Full-mode compose limits added but never run (no Docker).
- Pre-Phase-7 tokens cannot be individually revoked (no `jti`).
- Header auth still works during the cookie transition;
  making it API-client-only is a follow-up.
- Audit cleanup without an export directory still deletes
  (warning logged).
- R2's loud startup check (no worker in Postgres mode), R13
  (Postgres-required startup in compose), and R14 (SSRF grep
  gate) were not built in Phase 7 — see `docs/ROADMAP.md`.
- The sandbox VM was replaced mid-Phase-7 (the walk's first two
  runs were lost with `/tmp`, and Postgres/Redis had to be
  reinstalled from apt: Postgres 16 + Redis 7.0.15, fresh empty
  database, migrated on API startup). All test/live results above
  are from the rebuilt environment, recorded after the rebuild.
- Frontend on `:8080` was verified with Vite preview of the
  production build, not the Docker nginx image (no Docker here).
- Region scoping is display-only (all resources are `cf-local-1`).

## Frontend fidelity pass — AWS console interaction patterns — 2026-11-22

Second pass over the M9 Part 2 redesign, still **AWS-console-style in
CloudForge branding**: no AWS marks, logos, service icons or Amazon
Ember font. Frontend-only plus this log; backend, engine, proto,
curriculum and deployment files untouched. The pre-existing modified
`deployments/compose/docker-compose.light.yml` was left alone.

### What changed

- `components/AwsShell.tsx` (new): reusable AWS patterns — `AwsToolbar`,
  `AwsActionsMenu` / `AwsActionItem`, `AwsRefreshButton`,
  `AwsPreferencesButton`, `AwsFlash`, `AwsCopy`, `AwsWizard`,
  `AwsEmptyState`, `AwsStat`, `AwsKeyValue`, `AwsBackLink`. The wizard
  footer now matches AWS: Cancel on the left; Previous (link) plus
  Next (primary) or the submit label on the right.
- `AwsHeader`: one `openMenu` state instead of six booleans; opening
  any menu or starting a search closes the others; outside-click on
  `[data-aws-menu]` and Escape close; three-column Services mega-menu
  with "View all services"; AWS-style wide search placeholder.
- `AwsNav`: registry categories are collapsible groups (chevron,
  auto-open the active service's category, including when the registry
  arrives after mount — state adjusted during render, no effect);
  `All Services` uses `end`; only the canonical service for a shared
  route is highlighted (VPC aliases no longer light up several rows);
  duplicate `Learning Center` / `Terraform Learning` registry
  categories are filtered because static links already exist.
- `AwsTable`: optional AWS checkbox selection (select all, per-row,
  selected-row tint) while staying backward compatible.
- `ServiceConsole`: optional service icon tile, fidelity strip
  (`FidelityBadge` + "local learning cloud — not AWS"), sticky left
  sub-nav, `ConsoleTabs` export.
- `Dashboard`: Console Home layout — welcome hero, recently visited,
  service health, honest cost/usage, labs/courses/favorites rail,
  "Build a solution"; the Services stat shows "Registry unavailable"
  when the registry request fails instead of inventing a count.
- `Ec2Console`: instance table with real lab-backed rows, five-step
  launch wizard, stats, Instance state / Actions / Refresh /
  Preferences toolbar, filter + state filter, selectable table and
  nine detail tabs.
- `S3Console`: buckets sub-nav, five-step create wizard, honest
  MinIO-unavailable error, summary tiles, actions toolbar, selectable
  buckets, objects/properties/permissions/management/metrics tabs.
- `IamConsole`: Dashboard section is the default landing view with
  counts, security recommendations, Access Analyzer entry point and
  quick links.
- `VpcConsole`: lab selector, flash notices, resource-count stats,
  Actions ("Delete selected") / Refresh / Preferences toolbar.
- `LambdaConsole`: rewritten as "Lambda — Functions" with a four-step
  create wizard, selectable function table, and Code / Test /
  Configuration / Permissions / Monitoring / Triggers tabs; keeps the
  honest "subprocess sandbox · not containers · not AWS Lambda" note.
- `Services`: All Services index — search box, "X of Y services",
  category sections and a bordered tile grid (icon tile, fidelity
  badge, description, availability/operation count, favorite star);
  fidelity footnotes are suppressed in the dense grid so the pill is
  not duplicated by its default explanatory text.
- `index.css`: removed the Amazon Ember font reference (Helvetica/
  system stack), added orange `:focus-visible` outlines, `::selection`
  and shared `.aws-panel` / `.aws-table-head` / `.aws-subnav-active`
  utilities.

### Verification

- `./node_modules/.bin/tsc -b` — clean.
- `./node_modules/.bin/vitest run` — **12 files, 138 tests passed**.
- `npm run lint` (oxlint) — **0 warnings, 0 errors**.
- `npm run build` — passes (`✓ 2266 modules transformed`, `✓ built in
  1.41s`); the existing large-chunk warning remains.
- Visual check with headless Chrome at 1440×1000 against the Vite dev
  server on `:5199`: `/`, `/services`, `/services/ec2`,
  `/services/s3`, `/services/iam`, `/services/vpc` and
  `/services/lambda` all render the AWS-style shell, nav, tables,
  wizards and empty/error states. Screenshots are in `/tmp/cfshots/`.

### Known limitations

- `make verify` was not re-run; the Go/Rust suites were unchanged.
- The wizards were reviewed by render + code, not by clicking through
  every step in a live browser.
- Backend/MinIO availability was intermittent during the visual pass,
  so some screenshots show the intended error/empty states rather than
  populated tables.
- Consoles outside the five in scope (RDS, DynamoDB, SQS, etc.) keep
  their earlier layouts.
- The likeness is structural (spacing, colour, tables, toolbars); no
  AWS brand assets or fonts are used.

## Certification tracks, industry incidents and coverage map

### Content

- Added `curriculum/aws-saa/aws-saa.yaml`: SAA-C03 track with three
  domains, nine lessons, an advanced two-AZ guided lab (`saa-ha-web`),
  a cost-and-availability challenge, and five exam-style questions.
- Added `curriculum/aws-sap/aws-sap.yaml`: SAP-C02 track covering
  multi-account governance, hybrid networking and migration/DR, with
  nine lessons, a segmented hybrid lab (`sap-hybrid-network`), a
  multi-Region challenge and five exam-style questions.
- Added four industry break-and-fix scenarios using only rules the
  validation engine can evaluate: `checkout-sg-regression`,
  `nat-egress-collapse`, `alb-target-latency` and `hybrid-dns-outage`.
- Added four achievements (`saa-architect`, `sap-architect`,
  `incident-commander`, `certification-builder`) and raised the
  break-and-fix completion achievement to the new ten-scenario set.
- Added `saa-ha-web` and `sap-hybrid-network` to the launchable lab
  definitions at 30 minutes each, so both work inside the lite quota.
- Catalogued the missing SAA/SAP services honestly as `UNSUPPORTED`
  (Auto Scaling, Global Accelerator, Direct Connect, Control Tower,
  IAM Identity Center, RAM, Storage Gateway, Snowball, Lake Formation,
  Macie, Compute Optimizer, Savings Plans, Audit Manager, Artifact).
- Added `docs/certifications/AWS_CERTIFICATION_MAP.md` mapping each
  exam domain to the services it needs, the CloudForge fidelity, and
  the practice lab or lesson.

### Verification

- `python3 curriculum/validate.py` — 16 tracks, 88 lessons, 94
  questions, 10 scenarios, 19 achievements.
- `go test ./internal/curriculum ./internal/labs ./internal/achievements ./internal/store`
  — pass.
- `go test ./internal/httpapi -run 'TestCurriculumFlow|TestCurriculumLabValidationRulesField|TestLiteModeCapsQuotas'`
  — pass.

### Known limitations

- Non-network services (S3, IAM, Lambda, SQS, RDS) still cannot be
  graded by lab validation; that needs provider state in the
  validation state.
- The new lab definitions are content-complete but were not launched
  against a live lite stack in this change.

## CloudForge v3 — exact AWS-reference UIs, exam services in lite, Advanced section — 2026-11-22

Coordinator build, five workstreams + integration. Local-only; no AWS account, no AWS logos/trademarks, CloudForge branding only (dark navy header, orange primary buttons).

### Sync first (GitHub remote 213c33c "more examples", 2026-11-22 12:28 UTC)
- Cloned https://github.com/singhdeepaks2293/Cloudforge-test to /tmp, diffed against the local v2 tree (41 differing entries), merged selectively. In: SAA/SAP tracks (curriculum/aws-saa, aws-sap), 4 break-and-fix scenarios (10 total), docs/certifications/AWS_CERTIFICATION_MAP.md, 14 exam services in the registry, 2 new 30-min labs, frontend fidelity pass (new AwsShell.tsx), updated counts/tests. Preserved: local MinIO fix in the full compose file (remote had dropped it), v2 ports (frontend :8080 / API :8081 — remote's re-added port 80 rejected). Merged from remote into the light compose: worker CLOUDFORGE_METRICS_TOKEN, explicit subnets 172.28.10/11/12 (LAN-overlap fix). Post-sync validator: 16 tracks, 88 lessons, 94 questions, 10 scenarios, 19 achievements.
- Registry is 109 services, not 100 (grew with the SAA/SAP services). Lite set is 61 services, all present in the registry.

### Workstream 1 — exact AWS-reference UI per service (lite set)
- Every lite service got a Home dashboard (VPC pattern: banner, action links, Resources-by-Region resource cards, Service Health, Settings, Additional Information; no table on Home) + list pages with tables + detail tabs, via new ServiceHome.tsx / LiteServiceConsole.tsx / liteServices.ts. Home added to 14 dedicated consoles; 18 grouped consoles wrapped with Home + list tabs; 20 backend-less services use the generic console (Compute Optimizer and Savings Plans caught missing in a final count check and added). Wired to real backend endpoints where they exist; SIMULATED/PARTIAL/UNSUPPORTED actions labelled and disabled, never fake.

### Workstream 1b — AWS console survey reconciliation
- Survey at docs/reference/aws-console-survey-2026-11-22.md (read-only, 45/46 services, 29 lite + 16 backup; DataSync unchecked — browser unavailable). Tables aligned to the exact survey columns (S3, IAM Roles, Lambda, Load Balancers 11 columns, Cost Explorer, Budgets, Transit Gateway attachments, VPN, CloudFront, EKS, CloudFormation, CodeBuild, CodePipeline). Grouping mirrored: ELB/Auto Scaling/EBS are EC2 console sections; Transit Gateway/VPN are VPC console sections (old routes redirect, e.g. /services/elb -> /services/ec2?section=load-balancers). Restricted services (GuardDuty, Security Hub, Organizations, Redshift, Kinesis, DMS) labelled as following public docs, not the restriction dialog; DataSync marked UNVERIFIED; wizard steps marked inferred, not observed. Fixed 4 pre-existing quoted-literal "health" lines.

### Workstream 2 — exam services in lite (SAA-C03 + SAP-C02)
- All 61 lite services present and working in lite. The 24 previously UNSUPPORTED services now have a real API-emulated backend: one shared collection (migration 014_exam_services.sql, table p7_exam_services), 41 routes under /api/v1/aws/<service>/..., 92 registry operations, durable Postgres/memory state, per-owner isolation. Backup runs are real pg_dump (size + SHA-256). New labs saa-exam-services / sap-exam-services (30 min) + 6 lessons + 6 assessment questions. Only cap change: per-user exam-service records, lite learner 10 / instructor 25 / admin 50 (429 over cap), full uncapped. Lab count/profile/duration and compose limits unchanged; full compose untouched.
- Fixed stale test counts (tree already had 23 tracks from Workstream 3; the brief's 16/88/94 matched the remote, not the tree), two bugs its tests caught (Global Accelerator validator, NACL duplicate-rule int/float), and a YAML scalar bug.

### Workstream 3 — Advanced section (job-ready, industry, end-to-end)
- New top-level Advanced section: 7 tracks (multi-tier, HA+DR, migration, cost optimisation, incident response, CI/CD, production rollout), 6 tough multi-fault troubleshooting scenarios (adv-*), 8 interview walkthroughs, skills checklist (Cloud / DevOps / Solutions Architect), portfolio artefacts (not a certificate, no AWS-certification claim), 11 new achievements. Grading uses only rules the validation engine implements; pipeline/canary/rollback/alerting are graded through observable outcomes. Wired by the coordinator: routes /advanced, /advanced/projects, /advanced/troubleshooting, /advanced/job-ready + AwsNav/AwsHeader entries, and /api/v1/advanced (projects, interviews, skills-checklist, checklist-state GET/PUT, portfolio/{project}) behind requireAuth.

### Workstream 4 — backup of the other services
- docs/reference/aws-console-backup/: 48 files (one per non-lite registry service) + BACKUP_INDEX.md. Each: purpose, real console layout, CloudForge fidelity today (27 UNSUPPORTED, 17 PARTIAL, 3 SIMULATED, 1 REAL), what would be needed to build it later, scope (11 small, 29 medium, 8 large). Reference notes only.

### Integration fixes (coordinator)
- E2E regression found and fixed: Advanced guided labs reused M1 lab ids (vpc-fundamentals, ec2-basics x4, three-tier-app, iam-security). Curriculum lab definitions are keyed by id, so seeding upserted the Advanced rules under the M1 ids — a vpc-fundamentals lab then validated against the 7 HA-DR rules instead of its 5 DefaultRules, failing E2E. Fix: Advanced guided labs got their own ids (adv-multi-tier, adv-ha-dr, adv-migration, adv-cost, adv-incident, adv-cicd, adv-rollout), added as 7 definitions in backend/internal/labs/labs.go, catalogue + frontend AdvancedData.ts + docs updated. The test that had baked the bug in (TestCurriculumLabValidationRulesField asserting 7 rules on vpc-fundamentals) was corrected to assert no attached rules on vpc-fundamentals and 7 on adv-ha-dr.
- incident-commander achievement description clarified to "Solve ten break-and-fix incidents, including industry scenarios" (was "every industry incident" at a threshold of 10 while 16 scenarios exist).
- Rate-limit tests need Redis at localhost:6379 (fail-closed 503 without it, pre-existing); a local redis-server was started for verification.

### Verification (2026-11-22, sandbox)
- `make verify` exit 0 (Go 1.26.0, local redis-server running for the rate-limit tests).
- `go test ./...`: 24 packages pass, 0 fail. `cargo test --workspace`: 75 passed, 0 failed (19 test binaries). Frontend: 161/161 tests (14 files), build pass, oxlint 0 warnings / 0 errors (88 files), tsc clean, clippy clean, gofmt clean.
- Curriculum validator: OK — 23 tracks, 122 lessons, 142 assessment questions, 16 break-and-fix scenarios, 30 achievements, 17 warnings (pre-existing "no mermaid diagram" on older lessons).
- E2E acceptance test: PASS, run twice (~15s and ~6s).
- Registry: 109 services. Lite set: 61. Backup: 48 files + index. Survey: 45/46 services (DataSync unchecked).

### Honest limits (unverified in this sandbox)
- No Docker, KVM, or nginx here: compose up, real KVM VMs, real netlink, Terraform containers, nginx image, TLS override, and pg_dump-backed Backup runs on a real host are unverified. Migration 014 verified only via the memory store (Postgres integration tests skip — no Postgres here). DataSync console layout unverified (survey never reached it). /api/v1/advanced routes are registered and the advanced package has 5 unit tests, but there is no httpapi-level test hitting them yet. Skills checklist state is in-memory per process (documented); durable persistence needs a store migration. No in-console break-fix start button yet (pre-existing gap; scenarios start via POST /api/v1/labs/{id}/break-fix/start). Region scoping display-only; pre-v2 tokens (no jti) cannot be individually revoked.

### Fedora upgrade (host steps, not run here)
- No compose or .env changes in v3. Unzip the v3 zip over /home/deepak/Cloudforge-test (do not delete the folder — .env is not in the zip), then `make down-lite` + `make up-lite`. Migration 014 applies automatically on startup. MinIO fix and :8080/:8081 ports are already in the host's files; the two v2 tokens (CLOUDFORGE_ENGINE_TOKEN, CLOUDFORGE_METRICS_TOKEN) were already verified present in the host .env.
