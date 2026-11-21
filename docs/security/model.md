# CloudForge Security Model (Milestone 9)

How CloudForge protects its users, their credentials and their labs —
and what it deliberately does **not** protect against. This document
describes the local implementation (Go control plane, PostgreSQL,
Redis, chi router, host agent, Terraform, curriculum), not the
GitHub MVP it was backported from.

## Threat model (what this is for)

CloudForge is a **local learning platform**: one deployment per
classroom or self-learner, on hardware the operator controls. The
model assumes:

- The operator is trusted; the network between the containers is
  trusted (compose networks, no public ingress except the frontend
  on `:80`).
- Users are learners who may be curious or careless, not
  professional attackers. The controls below stop casual abuse
  (credential stuffing, token forgery, enumeration, accidental
  cross-user access, runaway Terraform) — they do not make the
  deployment safe to expose to the open internet.
- Secrets live in `.env` / the operator's environment, never in the
  repo. A leaked `CLOUDFORGE_JWT_SECRET` compromises every session
  (see "Secret rotation").

Out of scope: DDoS protection, WAF rules, intrusion detection,
per-tenant isolation beyond the ownership checks below, and
encryption at rest (Postgres/Redis volumes are local files).

## Authentication

### Passwords — Argon2id with dual-verify migration (M9)

- New passwords are hashed with **Argon2id** (PHC format
  `$argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>`, 64 MiB, 3
  iterations, 2 lanes, 16-byte salt, 32-byte key) in
  `backend/internal/auth/password.go`. Parameters follow OWASP
  interactive-login guidance.
- **Existing users are not locked out.** The M2-era bcrypt hashes
  (`$2a$`/`$2b$`/`$2y$`) still verify; on a successful login the
  stored hash is upgraded to Argon2id in place — in memory
  (`auth.Store.Authenticate`) and in PostgreSQL
  (`PostgresUserStore.Authenticate`, best-effort `UPDATE` that never
  fails the login). A failed login never re-hashes. The
  `password_hash` column is `TEXT`, so no migration was needed.
- Passwords are at least 8 characters; the hash is never serialised
  (`User.Public()` strips it) and never logged.
- **Breaking change (tokens only):** M2 hand-rolled HMAC bearer
  tokens are no longer accepted. Users must log in again after the
  M9 upgrade; passwords are unaffected.

### Sessions — HS256 JWT (M9)

- `POST /api/v1/auth/login` and `/register` return a signed
  **HS256 JWT** (`golang-jwt/jwt/v5`), 12h TTL
  (`CLOUDFORGE_TOKEN_TTL`), signed with `CLOUDFORGE_JWT_SECRET`.
  Claims: `sub` (user id), `iss: cloudforge`, `iat`, `exp`. No
  roles or PII are embedded in the token.
- Validation (`auth.Store.ValidateToken`) requires the HS256
  algorithm explicitly (`WithValidMethods(["HS256"])`) — `none` and
  foreign-algorithm tokens are rejected — checks the issuer, and
  maps expiry to `ErrTokenExpired`. Tokens signed with another
  secret fail.
- The secret **must** be set in any shared deployment
  (`openssl rand -hex 32`); startup warns loudly when the
  `dev-only-change-me` default is in use. The compose file and
  `.env.example` use the name `CLOUDFORGE_JWT_SECRET` — an earlier
  `JWT_SECRET` name was never read by config and silently fell back
  to the dev default; it was renamed in M9.
- There is no server-side session store and no revocation list: a
  token is valid until it expires. Rotating the secret invalidates
  **all** sessions at once (see runbook).

### Roles and ownership

- Roles: `learner` (default), `instructor`, `admin`. The first user
  registering with `CLOUDFORGE_ADMIN_EMAIL` is promoted to admin
  (bootstrap). Admin-only: user list, course CRUD, audit list,
  curriculum re-seed.
- Every lab/resource/terraform/terminal handler checks ownership:
  a lab belonging to another user returns **404, not 403**, so one
  user cannot enumerate another's labs. The same rule applies to
  lab resources, topology, validation, terminal tokens and
  Terraform runs (all scoped through the lab id).
- The terminal WebSocket is authenticated by a short-lived (5 min),
  single-use terminal token in the `?token=` query parameter —
  never by a Bearer token in a URL. Only the SHA-256 hash of the
  token is persisted.

## HTTP surface

- Routing is **go-chi/chi v5** (`backend/internal/httpapi`,
  `Server.Routes()`); the M1–M8 route table is preserved verbatim.
- Every response carries `X-Request-ID` (echoed when supplied,
  generated otherwise) and is logged as structured JSON.
- Errors use the envelope `{error:{code,message,request_id}}`.
  Auth failures deliberately do not distinguish "unknown email"
  from "wrong password" (`invalid_credentials`, 401).
- There is **no CORS configuration**: the browser uses same-origin
  requests through the nginx proxy (Docker) or the Vite dev proxy
  (`npm run dev`). `VITE_API_URL` is only for split deployments.
- Request bodies are decoded with `DisallowUnknownFields`.

## Rate limiting (M9)

Redis-backed fixed-window counters (`backend/internal/ratelimit`,
Lua `INCR` + `PEXPIRE`, atomic). Keys are
`ratelimit:<scope>:<client-ip>`; the client IP is the first
`X-Forwarded-For` entry (nginx sets it) or the peer address.
Limits are per IP, not per user, so a classroom behind one NAT
shares a budget — documented, not hidden.

| Scope | Default | On Redis error |
|---|---|---|
| Auth (`register`, `login`) | 10 attempts / 5 min per IP | **Fail-closed: 503** `rate_limiter_unavailable`. Brute-force protection must not silently disappear; operators get a loud error instead of a silent hole. |
| General `/api/v1` | 300 requests / 1 min per IP | **Fail-open: request proceeds** (warning logged). The API is designed to serve without Redis (fail-open cache); rate limiting must not become a hard dependency for reads. |

Over-limit responses are `429 rate_limited` with a `Retry-After`
header, in the standard error envelope. `/healthz`, `/readyz` and
`/metrics` are never rate limited. Limits are configurable:
`CLOUDFORGE_AUTH_RATE_LIMIT` / `CLOUDFORGE_AUTH_RATE_WINDOW`,
`CLOUDFORGE_API_RATE_LIMIT` / `CLOUDFORGE_API_RATE_WINDOW`.
When no Redis client is configured at all (nil limiter) the
limiters are disabled — existing tests and Redis-less dev runs are
unthrottled.

## Observability (M9)

- `GET /metrics` — Prometheus scrape endpoint:
  `cloudforge_http_requests_total{method,route,status}`,
  `cloudforge_http_request_duration_seconds{method,route}`,
  `cloudforge_network_engine_up`. The `route` label is the chi
  route pattern (`/api/v1/labs/{id}`), never the raw path — no
  user-identifying or secret labels, and no high-cardinality ids.
  `/metrics` itself is excluded from the counters.
- `GET /readyz` — honest readiness: live-checks Postgres
  (`SELECT 1`), Redis (`PING`) and the engine (gRPC Health) on
  every call, with per-component `status`/`latency_ms`/`error`.
  Overall readiness is gated **only on Postgres when configured**
  (503 `not_ready` while it is down); Redis and the engine are
  reported but do not gate, because both are fail-open/degraded-mode
  dependencies by design. In the in-memory fallback (no Postgres)
  the postgres check reports `disabled` and does not gate.
- `GET /healthz` stays a plain liveness probe (`{"status":"ok"}`).
- The compose `api` healthcheck uses `/readyz`.

## Data and infrastructure

- **PostgreSQL** (pgx): users, roles, courses, labs, resources,
  Terraform runs, terminal sessions, jobs, audit, curriculum.
  Mutations that must be atomic run in one transaction (e.g. lab
  creation writes `lab_sessions` + `lab_resources` + provision job
  + audit row). Migrations 001–005 are advisory-locked so api +
  worker can start concurrently.
- **Redis**: 60s course/capability cache (invalidated on mutation),
  terminal-token backing, rate-limit counters, job queue transport.
  Fail-open everywhere except auth rate limiting (see above).
- **Host agent** (M3): the API only ever calls it through the
  typed client; operations are allowlisted (`hostagent.Validate`),
  commands are allowlisted, and the host agent refuses to start in
  docker/libvirt mode without `HOST_AGENT_TOKEN`. Simulated mode
  never runs a host shell.
- **Terraform** (M7): runs in an isolated container with only the
  lab workdir mounted; configuration is validated (≤ 64 KiB, no
  amazonaws.com, no embedded keys); `apply` requires an approved,
  unexpired plan (1h). The runner reports `Unavailable` (503)
  when Docker is absent instead of faking a run.
- **Network engine** (M4/Rust): the API reports engine health only
  from the gRPC Health RPC; the TCP probe is labelled as such and
  is never read as engine health. The engine's default backend in
  this sandbox is SIMULATED; real netlink/nftables work is gated
  behind capability checks on a real host.
- **Audit**: every mutation is audited (actor, action, resource,
  request id); audit listing is admin-only with pagination.

## Secret rotation

- `CLOUDFORGE_JWT_SECRET`: generate a new value, restart api +
  worker, and **all users must log in again** (all outstanding
  tokens stop validating). There is no graceful dual-secret period.
- `POSTGRES_PASSWORD` / `MINIO_ROOT_PASSWORD`: rotate in `.env`
  and in the service, then restart the stack. Database credentials
  are never logged (`config.String()` redacts the DSN password).

## Known limitations

- No revocation list: a stolen JWT is valid until expiry (≤ 12h).
- Rate limiting is per IP: a shared NAT (classroom) shares one
  budget, and `X-Forwarded-For` is trusted from the proxy — do not
  put the API directly on the internet behind an untrusted proxy.
- No encryption at rest; no DDoS/WAF protection; the deployment is
  for a trusted local network, not the open internet.
- `POST /api/v1/labs` quota (per-role, admin-configurable since M9
  Part 3 — learner 3 labs by default) and Terraform plan expiry
  (1h) are the only abuse caps besides rate limiting.

## M9 Part 3 review — findings and fixes

### SSRF: one shared guard, enforced at every choke point

Finding: the Terraform runner had the only SSRF guard
(`terraform.ValidateEndpoint`, loopback-only), while the M5
provider backends fetched operator-configured endpoints
(DynamoDB, LocalStack emulator, MinIO) with no validation at
all. A misconfigured `DYNAMODB_ENDPOINT` / `AWS_EMULATOR_ENDPOINT`
/ `MINIO_ENDPOINT` could make the API probe or fetch an arbitrary
host — including the cloud metadata address `169.254.169.254`.

Fix (`backend/internal/security`, new package — stdlib only, so
any component can import it without a cycle):

- `security.ValidateEndpointURL(raw, allowedHosts...)`: http(s)
  only, no embedded credentials, host must be `localhost`,
  `127.0.0.0/8` or `::1`, or an explicitly allowed hostname
  (exact match — intended for Docker service names such as
  `minio`, which are operator-configured, not user input).
  Lookalikes (`localhost.localdomain`, `127.0.0.1.evil.com`) and
  private LAN addresses (`10/8`, `192.168/16`) are refused.
- `security.ValidateEndpointHost(hostPort, allowedHosts...)`:
  the same rule for the bare `host:port` MinIO form.
- `terraform.ValidateEndpoint` now delegates to it (loopback-only
  behaviour preserved; existing tests still pass), and the guard is
  enforced at construction/probe time in the providers:
  `NewS3Adapter` (allows `minio`), `ProbeEmulator` (refuses
  without making any request), and `NewAWS` (fail-fast at startup
  for DynamoDB/emulator endpoints).
- The host-agent address is deliberately NOT guarded the same way:
  it is operator-configured and may legitimately be a LAN address
  (the KVM host). It is never derived from user input.

### Secrets redaction in logs and audit

Finding: the slog call sites themselves were already clean
(`config.String()` redacts the DSN password and reports secrets
as `<set>`/`<unset>`; the HTTP logger records method/path only,
never headers or bodies), but audit metadata was recorded
verbatim — a future handler passing a token in metadata would
have persisted the secret to `audit_logs`.

Fix:

- `security.ScrubMap` / `RedactString` / `RedactURL` /
  `security.Secret` (a `slog.LogValuer` that always renders as
  `<redacted>`). `httpapi.audit()` scrubs metadata before
  recording, so a token, password, API key, `Authorization`
  header value or credential-bearing URL can never reach an audit
  row. Verified by `TestAuditMetadataIsScrubbed`.
- Residual risk, accepted: job-failure log lines include the
  job's error string, and a Terraform runner error could embed
  an endpoint URL. Runner errors never include credentials
  (embedded credentials are refused by the SSRF guard before
  any request), so the exposure is limited to hostnames.

### Admin-configurable quotas (M9 Part 3)

`GET/PUT /api/v1/admin/quotas` (admin-only, migration `006`):
per-role limits for active labs, VM size (vCPU/RAM/disk per VM)
and lab duration. Enforced by the `quota` package at the API
layer — lab creation checks count + duration
(`handleCreateLab`), instance creation checks the VM profile
against the role's size limits (`handleCreateResource`) — with
`429 quota_exceeded` naming the limit. The lab store's own
ceiling (`labs.Store` / `PostgresLabStore`, default 3) is only a
safety backstop, wired at startup to the highest configured role
limit so it cannot veto a legitimate admin lab. See the runbook
for the defaults and the retention note below.

### Audit-log retention

Audit rows are retained **90 days**: the worker's
`audit_cleanup` job calls `AuditStore.CleanupOlderThan(90)`.
There is no separate archival step — entries older than 90 days
are deleted, not exported. Operators who need longer retention
must snapshot the `audit_logs` table (e.g. with the backup
script in `scripts/backup.sh`) before the cleanup job runs.
