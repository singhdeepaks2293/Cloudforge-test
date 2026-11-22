# CloudForge Security

Operator-facing security guide. The detailed model (threat model,
per-endpoint behaviour, M9 review findings) lives in
`docs/security/model.md`; this file is what an operator needs to run
CloudForge safely. Phase 7 (2026-11-22) closed the Phase 0 audit §5
risks R1, R3–R8, R10–R12 as far as they can be closed on a local
platform; the residual risks are listed at the end, honestly.

## Threat model

CloudForge is a **local learning platform**, not a public SaaS. The
model is: a trusted operator, a trusted local network (classroom or
home lab), container networks that are trusted, and learners who are
not assumed to be hostile to each other but can submit arbitrary
Terraform/HCL and API requests. It is **not safe for the open
internet**: do not publish the API port (`:8081`) or the engine port
(`:50051`) publicly. The engine port is not published in compose at
all since Phase 7.

## What Phase 7 changed

| Control | What it is | Where |
|---|---|---|
| Engine auth (R1) | gRPC token on metadata `x-cloudforge-engine-token`, enforced on **every** RPC including Health, in the Rust server and the Go client. Wrong/missing token → `UNAUTHENTICATED`. `:50051` no longer published on the host in compose. mTLS is the documented next step, **not** built. | `rust-network-engine/crates/grpc-server/src/lib.rs`, `backend/internal/networkclient/client.go` |
| JWT revocation (R3) | Tokens carry a `jti`. Logout revokes the caller's `jti`; an admin can revoke all of a user's sessions (`POST /api/v1/admin/users/{id}/sessions/revoke`). Denylist is Postgres (`revoked_tokens`/`issued_tokens`, migration 013) when the DB is up, else Redis, else in-memory. Checked on every authenticated request. Pre-Phase-7 tokens (no `jti`) cannot be individually revoked. | `backend/internal/auth/revocation.go`, `backend/internal/store/revocation.go` |
| Fail-closed secrets (R4) | Without `CLOUDFORGE_DEV=1`, the API and worker refuse to start when a production secret is unset or still a known dev default, naming the variable. Dev defaults only work with the explicit opt-in. | `backend/internal/config/config.go` (`ValidateSecrets`) |
| `/metrics` restriction (R5) | Token auth (`X-Metrics-Token` or `Authorization: Bearer`); loopback-only when no token is configured (dev). nginx no longer proxies `/metrics` (404). Prometheus scrapes with the token. | `backend/internal/httpapi/security_p7.go`, `frontend/nginx.conf`, `infrastructure/monitoring/prometheus.yml` |
| Sessions / CSP (R6) | Session token in an `HttpOnly`, `SameSite=Lax` cookie (`cf_session`; `Secure` only when `CLOUDFORGE_TLS=1`). The `Authorization` header path still works for API clients/tests (transition — see below). CSP + `X-Content-Type-Options` + `X-Frame-Options: DENY` + `Referrer-Policy` + `Permissions-Policy` on API and nginx responses; HSTS only when TLS is on. | `backend/internal/httpapi/security_p7.go`, `frontend/nginx.conf` |
| Trusted proxy (R7) | `X-Forwarded-For` is honoured only when the peer is loopback, a private (RFC 1918/ULA) address, or an explicitly configured `CLOUDFORGE_TRUSTED_PROXIES` IP/CIDR. Otherwise the peer IP is used. | `backend/internal/httpapi/security_p7.go` |
| Per-user rate limits (R8) | Per-user limiter in addition to per-IP (default 300/min, `CLOUDFORGE_API_USER_RATE_LIMIT`); unauthenticated requests skip it; fail-open on Redis error like the general limiter. | `backend/internal/httpapi/security_p7.go` |
| Audit export (R10) | Export jobs: `POST /api/v1/audit/export/jobs` (CSV/JSON, cap 10,000 events), list/download, retention (newest 7 by default, `CLOUDFORGE_AUDIT_EXPORT_RETENTION`). The worker's 90-day cleanup exports rows to `audit-archive-<date>.csv` **before** deleting; a failed export blocks the delete. | `backend/internal/httpapi/security_p7.go`, `backend/internal/jobs/audit_export.go` |
| Terraform isolation (R11) | `CLOUDFORGE_TF_NETWORK_MODE=isolated` runs the Terraform container with `--network none` (no egress at all — the emulator is unreachable, plans needing it fail honestly). Default `bridge` keeps the emulator route and only approximates isolation. | `backend/internal/terraform/terraform.go` |
| TLS option (R12) | Opt-in only. `scripts/gen-tls-cert.sh` creates a self-signed cert; `deployments/compose/docker-compose.tls.yml` replaces the frontend nginx config with `frontend/nginx.tls.conf` (host `:443` → container `:8443`; `:8080` redirects to HTTPS). `CLOUDFORGE_TLS=1` enables API HSTS + Secure cookies. The TLS compose/nginx path is **unverified in the sandbox** (no Docker/nginx) — cert generation itself was verified with openssl. | `scripts/gen-tls-cert.sh`, `deployments/compose/docker-compose.tls.yml`, `frontend/nginx.tls.conf` |

## Secrets and environment

Required in production (non-dev) mode — the API/worker refuse to
start without them:

- `CLOUDFORGE_JWT_SECRET` — ≥32 chars, not `dev-only-change-me`
- `CLOUDFORGE_ENGINE_TOKEN` — ≥16 chars, not the dev default; shared with the engine
- `CLOUDFORGE_METRICS_TOKEN` — ≥16 chars; shared with Prometheus
- `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` — not `minioadmin`
- `CLOUDFORGE_HOST_AGENT_TOKEN` — not the dev default
- `GRAFANA_ADMIN_PASSWORD` — required by compose (was an empty default)

Dev mode is an explicit opt-in: `CLOUDFORGE_DEV=1`. It logs a
warning and uses the well-known dev defaults. Never set it on a
shared host. `.env.example` documents every variable; copy it to
`.env` and fill in real values (see `docs/DEPLOYMENT.md`).

## Sessions — transition plan

The cookie is the session now; the `Authorization: Bearer` header
still works for API clients and tests during a transition period.
The frontend sends the cookie automatically (same-origin) and still
keeps a copy of the token in memory for API calls. The planned
follow-up (not built in Phase 7) is to stop issuing the token to
browser clients at all and make the header path API-client-only;
until then, treat any XSS as able to read the in-memory token —
which is why CSP + sanitised lesson content matter. Tokens live
12 h; logout revokes immediately.

## Residual risks (open, with next step)

- **mTLS for the engine is not built.** Token auth is in place;
  the next step is a generated CA + client certs issued at deploy
  time (see `docs/ROADMAP.md`).
- **ECR `registry:2` has no auth** (local-only). Do not expose it.
- **Terraform bridge mode only approximates isolation.** Use
  `isolated` mode for untrusted HCL.
- **The API port must not be published to the open internet.**
  The trusted-proxy rule assumes the nginx container (or loopback)
  is the only thing setting `X-Forwarded-For`; a directly reachable
  API port on a public network breaks that assumption.
- **TLS compose/nginx path unverified here** (no Docker/nginx in
  the sandbox). Verify on a real host before relying on it.
- **Pre-Phase-7 tokens cannot be individually revoked** (no `jti`).
  They expire within 12 h; rotating `CLOUDFORGE_JWT_SECRET`
  invalidates all of them at once.
