# CloudForge API Reference (Milestone 9)

Base URL: same-origin in the Docker deployment (nginx proxies
`/api`, `/healthz`, `/readyz`, `/metrics` to the Go API), or
`http://localhost:8081` when running the API directly / with
`VITE_API_URL` set. All `/api/v1` paths below are relative to that
base. The frontend client (`frontend/src/services/api.ts`) uses
relative URLs when `VITE_API_URL` is unset.

Version: `0.9.0-m9` (see `GET /api/v1/health`).

## Conventions

- **Auth:** `Authorization: Bearer <JWT>` unless noted. The JWT is
  an HS256 token from `/auth/login` or `/auth/register`, 12h TTL.
  See [docs/security/model.md](../security/model.md).
- **Errors:** every error is `{error:{code,message,request_id}}`
  (see table). Auth failures return `invalid_credentials` (401)
  without distinguishing unknown email from wrong password.
- **Correlation:** every response carries `X-Request-ID` (echoed
  when supplied, generated otherwise); send one per request.
- **Rate limiting:** `POST /auth/register` and `/auth/login` are
  limited to 10 attempts / 5 min per client IP (fail-closed:
  503 `rate_limiter_unavailable` when Redis is down); all other
  `/api/v1` routes share 300 requests / 1 min per IP (fail-open).
  Over-limit: `429 rate_limited` with a `Retry-After` header.
  `/healthz`, `/readyz`, `/metrics` are never limited.
- **Pagination:** `?limit=` (default 50, max 100) and `?offset=`
  on list endpoints that support it.
- **JSON bodies:** decoded with `DisallowUnknownFields` — unknown
  fields are a 400 `invalid_json`.

### Error codes

| Status | Code | Meaning |
|---|---|---|
| 400 | `invalid_json` / `invalid_request` / `invalid_email` / `password_too_short` | Malformed body or input |
| 401 | `unauthorized` / `invalid_credentials` | Missing/invalid Bearer token, or bad login |
| 403 | `forbidden` | Authenticated but wrong role (admin required) |
| 404 | `course_not_found` / `lab_not_found` / `not_found` | No such resource (other users' labs also return 404 — see security model) |
| 409 | `user_exists` / `not_enrolled` / `invalid_transition` | Conflict with current state |
| 429 | `rate_limited` / `quota_exceeded` | Rate limit, or the 3-active-lab quota |
| 503 | `rate_limiter_unavailable` / `terraform_unavailable` | A dependency needed for this call is down (honest, not faked) |

## Health, readiness, metrics

| Method & path | Auth | Response |
|---|---|---|
| `GET /healthz` | — | Liveness: `{"status":"ok"}` |
| `GET /api/v1/health` | — | `{service, version, ok, uptime_seconds, network_engine_grpc, network_engine_detail, network_engine_reachable, network_engine_tcp_detail, database_connected, redis_connected, host_agent?}`. `network_engine_grpc` is the real gRPC Health RPC; `network_engine_reachable` is only a TCP probe, never engine health. |
| `GET /readyz` | — | `{status: ready\|not_ready, components: {postgres, redis, engine}, checked_at}`. Each component: `{status: ok\|down\|disabled\|not_configured, latency_ms, error?}`. Live-checked on every call. **200** when ready; **503** when Postgres (if configured) is down. Redis/engine outages are reported but do not gate readiness. |
| `GET /metrics` | Token or loopback | **Phase 7 (R5).** Prometheus text format: `cloudforge_http_requests_total{method,route,status}`, `cloudforge_http_request_duration_seconds{method,route}`, `cloudforge_network_engine_up`, plus Go/process collectors. The `route` label is the chi route pattern, never a raw id. Auth: `X-Metrics-Token: <CLOUDFORGE_METRICS_TOKEN>` or `Authorization: Bearer <token>`; without a token configured, loopback only (dev). No token → 401. nginx no longer proxies `/metrics` (404). |

## Auth & users

| Method & path | Auth | Notes |
|---|---|---|
| `POST /api/v1/auth/register` | — | `{email, name, password}` (password ≥ 8 chars). New passwords are Argon2id-hashed. 201 `{token, expires_at, user}`; 409 `user_exists`. First user with `CLOUDFORGE_ADMIN_EMAIL` becomes admin. |
| `POST /api/v1/auth/login` | — | `{email, password}` → 200 `{token, expires_at, user}`. Legacy bcrypt hashes still verify and are upgraded to Argon2id on success. Sets the `HttpOnly` `cf_session` cookie (Phase 7). |
| `POST /api/v1/auth/logout` | Bearer or cookie | **Phase 7 (R3).** Revokes the caller's token (`{logged_out: true, token_revoked: true}`) and clears the session cookie; the revoked token then gets 401. |
| `POST /api/v1/admin/users/{id}/sessions/revoke` | Admin | **Phase 7 (R3).** Revokes all recorded sessions of the given user (`{revoked: N}`); the admin's own token still works. |
| `GET /api/v1/auth/me` | Bearer or cookie | Current user (public view). Phase 7: the `cf_session` cookie alone authenticates. |
| `GET /api/v1/users/me` | Bearer or cookie | Alias of `/auth/me`. |
| `GET /api/v1/users` | Admin | All users (public views). |
| `GET /api/v1/users/me/progress` | Bearer | **M8.** Per-track enrollment/percent, `total_xp`, `streak_days`. |
| `GET /api/v1/users/me/achievements` | Bearer | **M8.** Earned + locked achievements (`locked_reason` on locked ones). |
| `GET /api/v1/audit` | Admin | Audit entries, newest first; `?limit=`/`?offset=`. |
| `POST /api/v1/audit/export/jobs?format=csv\|json` | Admin | **Phase 7 (R10).** Creates an audit export job (cap 10,000 events): 201 `{id, format, events, total, bytes, created_at, created_by, filename}`. Files live under `<workdir>/audit-exports/` with a `.meta.json` sidecar; retention keeps the newest N (default 7, `CLOUDFORGE_AUDIT_EXPORT_RETENTION`). |
| `GET /api/v1/audit/export/jobs` | Admin | **Phase 7 (R10).** Lists export jobs + the retention setting. |
| `GET /api/v1/audit/export/jobs/{id}` | Admin | **Phase 7 (R10).** Downloads the export file (CSV/JSON, real audit rows). |

## Courses & curriculum

| Method & path | Auth | Notes |
|---|---|---|
| `GET /api/v1/courses` | — | Course list (Redis-cached 60s when available). |
| `POST /api/v1/courses` | Admin | `{id (slug), title, track, description?, modules?}`; invalidates the course cache. |
| `GET /api/v1/courses/{id}` | — | Course detail incl. lesson titles. |
| `PUT /api/v1/courses/{id}` | Admin | Update title/track/description. |
| `DELETE /api/v1/courses/{id}` | Admin | Deletes the course (cascades modules/lessons). |
| `GET /api/v1/courses/{id}/modules` | — | Modules with lessons (lesson IDs needed for progress). |
| `POST /api/v1/courses/{id}/enroll` | Bearer | Enroll the caller. |
| `GET /api/v1/courses/{id}/progress` | Bearer | Completed lessons, XP, enrollment state. |
| `POST /api/v1/courses/{id}/progress` | Bearer | `{lesson_id}` — completes a lesson, awards 10 XP (+10 first-lesson bonus); re-completion awards 0; 409 when not enrolled. |
| `GET /api/v1/curriculum/tracks` | — (progress when authed) | **M8.** Track list; with a Bearer token each track carries `enrolled` / `progress_percent` / `completed_lessons`. |
| `GET /api/v1/curriculum/tracks/{id}` | — (progress when authed) | **M8.** Full track: modules/lessons with theory, key points, diagrams, guided lab + challenge with validation rules, assessment questions **without** `correct_answer`. |
| `POST /api/v1/curriculum/tracks/{id}/enroll` | Bearer | **M8.** Enroll the caller in a track. |
| `POST /api/v1/curriculum/lessons/{lessonId}/complete` | Bearer | **M8.** `{lessonId}` is the YAML slug or lesson UUID. Enrolled only (409 otherwise); awards 10 XP (+10 first-lesson bonus, + track-completion remainder on the final lesson). Re-completion awards 0 and returns newly earned achievements. |
| `POST /api/v1/curriculum/assessments/{trackId}/submit` | Bearer | **M8.** `{answers: {"0": 1, ...}}` (or `[{"question": 0, "answer": 1}]`). Graded **server-side**; returns score, per-question correct/incorrect + explanation, and awards 20 XP only on the first pass at ≥ 80%. |
| `POST /api/v1/admin/seed-curriculum` | Admin | **M8.** Re-runs the curriculum seed from `CURRICULUM_DIR` (idempotent); returns seed counts. |

## Labs

| Method & path | Auth | Notes |
|---|---|---|
| `POST /api/v1/labs` | Bearer | `{definition_id, name?, idempotency_key?}` (or `Idempotency-Key` header). 400 unknown definition; **429** when the 3-active-lab quota is hit. Postgres mode returns **202 Accepted** (worker provisions); in-memory mode returns 201; replaying an idempotency key returns 200 with the same lab. |
| `GET /api/v1/labs` | Bearer | Own labs only, newest first. |
| `GET /api/v1/labs/{id}` | Bearer | Own lab only (other users' labs → 404). **M8:** adds `validation_rules` (and `scenario_id`/`fault_status` when a break-and-fix attempt exists) for curriculum lab definitions. |
| `POST /api/v1/labs/{id}/stop` | Bearer | READY/RUNNING → STOPPING → STOPPED; 409 on illegal transition. |
| `POST /api/v1/labs/{id}/destroy` | Bearer | → DESTROYING (worker completes → DESTROYED in Postgres mode); 409 on illegal transition. |
| `GET /api/v1/labs/{id}/resources` | Bearer | **M6.** Real lab resources from the store (+ host-agent VMs merged in); empty list + `note` when none — never invented resources. |
| `POST /api/v1/labs/{id}/resources` | Bearer | **M6.** `{kind, name, parent_id?, cidr?, profile?, config?}`; kind ∈ vpc\|subnet\|instance\|gateway\|loadbalancer\|route-table\|security-group. Subnet goes through the Rust engine `CreateSubnet`, instance through host-agent `CreateVM`; engine/host-agent failure → **502** with the structured engine error. |
| `DELETE /api/v1/labs/{id}/resources/{resourceId}` | Bearer | **M6.** Deletes via engine/host-agent + DB row; idempotent (already gone → 200). |
| `GET /api/v1/labs/{id}/topology` | Bearer | **M6.** Live topology: engine `InspectTopology` merged with DB resources; engine unreachable → **503** with capability info. |
| `POST /api/v1/labs/{id}/validate` | Bearer | **M6/M8.** `{rules?}` (empty body = the lab definition's defaults) → `{lab_id, passed, xp_eligible, checks:[{rule, rule_id, status: pass\|fail\|unknown, passed, detail, message}]}`. Checks inspect real state; unknowable rules are `unknown`, never `pass`. |
| `POST /api/v1/labs/{id}/terminal/token` | Bearer | **M6.** Short-lived (5 min), single-use terminal token `{token, expires_at, redis_backed}` bound to lab+user; only the SHA-256 hash is persisted. |
| `GET /api/v1/labs/{id}/terminal` | Terminal token (`?token=`, WebSocket) | **M6.** WebSocket shell. Auth is the terminal token query param only — never the Bearer token in a URL. JSON `{type:"resize",cols,rows}` / `{type:"ping"}` handled; anything else is shell input. The shell runs only via host-agent `ExecInLab` (allowlisted commands); simulated mode returns a clearly-labelled simulated shell. |
| `POST /api/v1/labs/{id}/break-fix/start` | Bearer | **M8.** `{scenario_id}` — injects the scenario fault via the Rust engine `InjectFault`; engine unreachable → the attempt is `fault_pending`, never claimed broken. |
| `POST /api/v1/labs/{id}/break-fix/solve` | Bearer | **M8.** Runs the scenario's validation rules against real lab state; only when **all** pass is the scenario solved (+ scenario XP, once) and achievements evaluated. Otherwise `solved: false` with the failing checks. |

## Terraform (M7, async)

Every mutating action validates the request, creates a
`terraform_runs` row (`QUEUED`), enqueues a job for the worker and
answers **202** `{run_id, job_id, action, state:"QUEUED"}`. Nothing
runs on the API process. `apply` requires an approved, unexpired
plan (1h): no approval → 409, expired → 409, unknown → 404.
When Docker is absent the runner reports `Unavailable` and the API
answers 503 before enqueueing anything.

| Method & path | Auth | Notes |
|---|---|---|
| `POST /api/v1/labs/{id}/terraform/init` | Bearer | Enqueues `terraform_init`. |
| `POST /api/v1/labs/{id}/terraform/validate` | Bearer | Enqueues `terraform_validate`. |
| `POST /api/v1/labs/{id}/terraform/plan` | Bearer | `{configuration?}` (HCL, ≤ 64 KiB, no amazonaws.com / embedded keys) → enqueues `terraform_plan`; the worker stores the parsed `terraform show -json` plan. |
| `POST /api/v1/labs/{id}/terraform/approve` | Bearer | `{plan_id}` — explicit approval for a succeeded, unexpired plan; anything else → 409. |
| `POST /api/v1/labs/{id}/terraform/apply` | Bearer | `{plan_id}` → enqueues `terraform_apply`; the worker syncs created resources into `lab_resources`. |
| `POST /api/v1/labs/{id}/terraform/destroy` | Bearer | Enqueues `terraform_destroy`; the worker removes emulator-managed resources. |
| `GET /api/v1/labs/{id}/terraform/runs` | Bearer | Run history, newest first. |

## Providers (M5)

| Method & path | Auth | Notes |
|---|---|---|
| `GET /api/v1/providers/capabilities` | — | Capability registry with fidelity labels; nothing claims `REAL` (Redis-cached 60s when available). |
| `GET /api/v1/aws/{service}/capabilities` | — | Per-service capabilities with freshly probed health; `service` ∈ s3, dynamodb, iam, sts, kms, sqs, sns, eventbridge, ec2, lambda, vpc, eks; unknown service → 404. |
| `POST /api/v1/aws/s3/buckets` | Bearer | `{name}` → 201; MinIO unreachable → 503 with `capability` info. |
| `GET /api/v1/aws/s3/buckets` | — | MinIO unreachable → 503 with `capability` info. |
| `POST /api/v1/aws/iam/evaluate` | Bearer | `{policies, action, resource, username?}` → `{decision: Allow\|Deny, reason, matched_statement?}` (real evaluation of a documented IAM subset). |

## Worker & host agent

- Worker (`:8083`): `GET /healthz`, `GET /status`
  (`database_connected`, job counts by state, uptime).
- Host agent (`:8082`, bearer token except `/healthz`):
  `GET /healthz`, `GET /v1/inventory`, `POST /v1/ops` (one
  allowlisted operation; see backend README "Host agent").

## Examples

```bash
# Register + call an authed endpoint (same-origin in Docker;
# http://localhost:8081 when running the API directly)
TOKEN=$(curl -s -X POST localhost:8081/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","name":"Ada","password":"correct-horse"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')

curl -s localhost:8081/api/v1/auth/me -H "Authorization: Bearer $TOKEN"
curl -s localhost:8081/readyz | python3 -m json.tool
curl -s localhost:8081/metrics | grep cloudforge_http_requests_total
```
