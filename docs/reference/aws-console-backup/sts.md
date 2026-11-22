# STS — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `sts` |
| Name | STS |
| Category | Security and Identity |
| Registry route | `/services/sts` |
| Availability today | registry-only |
| Fidelity today | **SIMULATED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | credential |
| Learning tracks linked | iam |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

STS is the temporary-credentials service: AssumeRole, GetSessionToken, GetCallerIdentity, and federated variants return short-lived credentials (access key, secret, session token, expiry) that grant only the assumed role's permissions. A learner meets it as the mechanism behind every 'act as this role for a while' flow — Lambda execution roles, cross-account access, federated login — and as the reason long-lived keys should be rare.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** STS has no dedicated console in the real console — it is surfaced inside IAM (Roles → Assume role, and the "Switch role" flow) and as API/CLI calls. The CloudForge console for STS is therefore a small dedicated page, not a mirror of a non-existent console.
**Home — STS page:** current caller identity card (account/user/role ARN, user ID), active sessions table (Session name, Role assumed, Issued, Expires, Status Active/Expired), and an Assume-role form (role selector, session name, duration 15 min–12 h, policy (optional inline session policy JSON), external ID).
**Detail (per session):** session token (masked, shown once at creation), access key ID, expiry countdown, assumed role, session policy, Revoke button.
**Actions:** AssumeRole, GetCallerIdentity, GetSessionToken, AssumeRoleWithWebIdentity (disabled — no IdP locally, with the reason), Revoke session.

## 3. CloudForge fidelity today

- **Fidelity: SIMULATED** (availability: registry-only). Registry description: _Temporary security credentials (educational simulator)._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** In-memory simulator only, no HTTP route; credentials are locally generated, grant no AWS access, and live only in memory.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists as an in-memory simulator — make it durable and wire it to IAM, do not rebuild the token format.** Current: locally generated credentials, "grant no AWS access", in-memory only, no HTTP route (backend/internal/providers/simulators.go:80-109). Registry fidelity SIMULATED, availability `registry-only`. To build later, in order:
1. **Durable sessions:** Postgres `pX_sts_sessions` (id, role_id, session_name, access_key_id, secret_hash, token_hash, issued_at, expires_at, session_policy jsonb, revoked_at, created_by) + endpoints `POST /api/v1/aws/sts/assume-role`, `POST /api/v1/aws/sts/get-session-token`, `GET /api/v1/aws/sts/get-caller-identity`, `GET /api/v1/aws/sts/sessions`, `POST /api/v1/aws/sts/sessions/{id}/revoke`. Secrets shown once at creation (same pattern as IAM access keys), hashed at rest.
2. **Enforcement:** the API's auth middleware already accepts JWTs; add session-token auth — a request bearing an STS session token is authenticated as the assumed role, with the session policy intersected with the role's policies, and expiry enforced (this is the same expiry enforcement the IAM AssumeRole sessions already have — reuse it). Revoked/expired sessions are rejected.
3. **IAM wiring:** AssumeRole only works for roles that exist in the IAM store, and only when the caller's CloudForge role is allowed (admin, or a trust policy that names the caller — trust policies are recorded today, not evaluated; evaluating the trust policy's principal list is part of this build).
4. **Console:** a small STS page (assume-role form + sessions table + caller identity card) — the only service whose console is deliberately NOT a mirror of a real console, because the real service has none. Say that on the page.
**Data model:** the sessions table above; reuse the IAM roles/policies tables.
**Labs / lessons:** one lesson in the `iam` track: assume a read-only role, try a write and watch it denied, let the session expire and watch the next call fail, revoke a session early. Reuse the IAM lesson.
**Lite feasibility:** Yes — pure Go + Postgres; no Docker/KVM.

## 5. Lite feasibility and scope

- **Scope: small.** Durable sessions + session-token auth in the API middleware + trust-policy evaluation.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `sts`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
