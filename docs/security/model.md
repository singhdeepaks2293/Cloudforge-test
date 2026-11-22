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
  on `:8080`).
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

### Sessions — HS256 JWT (M9), revocation + cookies (Phase 7)

- `POST /api/v1/auth/login` and `/register` return a signed
  **HS256 JWT** (`golang-jwt/jwt/v5`), 12h TTL
  (`CLOUDFORGE_TOKEN_TTL`), signed with `CLOUDFORGE_JWT_SECRET`.
  Claims: `sub` (user id), `iss: cloudforge`, `iat`, `exp`, and
  since Phase 7 a `jti` (unique token id, `auth.NewJTI`).
  No roles or PII are embedded in the token.
- **Revocation (Phase 7, R3):** tokens are checked against a
  denylist on every authenticated request. `POST
  /api/v1/auth/logout` revokes the caller's `jti`; an admin can
  revoke all of a user's sessions (`POST
  /api/v1/admin/users/{id}/sessions/revoke`). The denylist is
  Postgres (`revoked_tokens` / `issued_tokens`, migration 013)
  when the DB is up, else Redis, else in-memory. Pre-Phase-7
  tokens (no `jti`) cannot be individually revoked.
- **Session cookie (Phase 7, R6):** login/register also set an
  `HttpOnly`, `SameSite=Lax` cookie `cf_session` (`Secure` only
  when `CLOUDFORGE_TLS=1`). The `Authorization: Bearer` header
  path still works for API clients/tests during a transition;
  the plan is to make it API-client-only (see `docs/SECURITY.md`).
- Validation (`auth.Store.ValidateToken`) requires the HS256
  algorithm explicitly (`WithValidMethods(["HS256"])`) — `none` and
  foreign-algorithm tokens are rejected — checks the issuer, and
  maps expiry to `ErrTokenExpired`. Tokens signed with another
  secret fail. A revoked token maps to `ErrTokenRevoked` (401).
- The secret **must** be set in any shared deployment
  (`openssl rand -hex 32`); since Phase 7 the API and worker
  **refuse to start** in non-dev mode when the secret is unset or
  still the `dev-only-change-me` default (`ValidateSecrets`,
  naming the variable). Dev defaults work only with the explicit
  `CLOUDFORGE_DEV=1` opt-in. The compose file and
  `.env.example` use the name `CLOUDFORGE_JWT_SECRET` — an earlier
  `JWT_SECRET` name was never read by config and silently fell back
  to the dev default; it was renamed in M9.

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
Since Phase 7 the cleanup **exports rows to
`audit-archive-<date>.csv` before deleting them**; a failed
export blocks the delete. Admins can also export on demand as
jobs (`POST /api/v1/audit/export/jobs`, CSV/JSON, cap 10,000
events, retention newest 7 by default). If the worker's export
directory is unset, it logs a warning and still deletes
(documented in `docs/SECURITY.md`). Operators who need longer
retention should also snapshot the `audit_logs` table (e.g.
with the backup script in `scripts/backup.sh`) before the
cleanup job runs.

## Phase 7 — Production hardening (2026-11-22)

Phase 7 closed the Phase 0 audit §5 risks R1, R3–R8, R10–R12
(`docs/IMPLEMENTATION_STATUS.md` §5, §16). Operator summary in
`docs/SECURITY.md`; this section records the mechanisms.

### Engine auth (R1)

The Rust engine's gRPC channel is authenticated with a
shared-secret token on metadata `x-cloudforge-engine-token`,
enforced by `EngineAuthInterceptor` on **every** RPC including
Health, in the server and in the Go client
(`networkclient.NewWithToken`). The engine fails closed at startup
without a token (`CLOUDFORGE_ENGINE_TOKEN`) unless
`CLOUDFORGE_DEV=1`. The old unauthenticated path refuses
connections. `:50051` is no longer published on the host in
either compose file. mTLS is the documented next step, not built.

### Metrics (R5)

`GET /metrics` requires a token (`X-Metrics-Token` or
`Authorization: Bearer <CLOUDFORGE_METRICS_TOKEN>`); with no token
configured it is loopback-only (dev). nginx no longer proxies
`/metrics` (404). Prometheus scrapes with the token
(`infrastructure/monitoring/prometheus.yml`).

### Security headers (R6, R12)

API middleware (`withSecurityHeaders`) and nginx set CSP
(`default-src 'self'`, `frame-ancestors 'none'`, …),
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: strict-origin-when-cross-origin`,
`Permissions-Policy: camera=(), microphone=(), geolocation=()`.
HSTS (`max-age=31536000; includeSubDomains`) only when
`CLOUDFORGE_TLS=1`. TLS itself is opt-in (self-signed cert via
`scripts/gen-tls-cert.sh`, compose override
`docker-compose.tls.yml`); the TLS compose/nginx path is
unverified in the sandbox (no Docker/nginx).

### Trusted proxy and rate limits (R7, R8)

`X-Forwarded-For` is honoured only when the peer is loopback, a
private (RFC 1918/ULA) address, or an explicitly configured
`CLOUDFORGE_TRUSTED_PROXIES` IP/CIDR; otherwise the peer IP is
used. The per-IP limiters from M9 are unchanged; a per-user
limiter (default 300/min, `CLOUDFORGE_API_USER_RATE_LIMIT`) now
runs in addition on `/api/v1`. Unauthenticated requests skip it.
Both fail open on Redis error (documented, by design); the auth
limiter fails closed (503).

### Terraform isolation (R11)

`CLOUDFORGE_TF_NETWORK_MODE=isolated` runs the Terraform
container with `--network none` (no ExtraHosts, no egress at
all). Default `bridge` keeps the `host.docker.internal` emulator
route and only approximates isolation. See
`docs/TERRAFORM_LEARNING.md`.
