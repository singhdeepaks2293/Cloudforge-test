# Cognito — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `cognito` |
| Name | Cognito |
| Category | Security and Identity |
| Registry route | `/services/cognito` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | user-pool, identity-pool |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

Cognito is the managed identity service for applications: user pools (sign-up, sign-in, MFA, hosted UI, social/federated login for your app's users) and identity pools (temporary credentials for authenticated or guest users to call other services). A learner meets it when an application needs its own users — distinct from IAM, which is for operators of the account — and when a mobile/web app needs sign-in without building auth from scratch.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** User pools, Identity pools, Federated identities.
**User pools list:** columns Name, Pool ID, Created. Click a pool for its detail.
**User pool detail tabs:** Users (table: Username, Status, Email verified, Created; actions Create user, Disable, Reset password, Delete), Groups (table: Group name, Description, Precedence, Role), Sign-in experience (sign-in options, MFA, password policy, account recovery), Messaging (email/SMS configuration), App integration (app clients, hosted UI domain, callback URLs), Security (advanced security, compromised credentials), Devices.
**Identity pools list:** columns Name, Pool ID, Unauthenticated identities (enabled/disabled), Created. Detail: authenticated/unauthenticated IAM roles, identity providers.
**Wizard — Create user pool:** Step 1 Configure sign-in experience (sign-in options: username/email/phone; Cognito user pool as identity provider). Step 2 Configure security requirements (password policy, MFA: optional/required, authenticator app/SMS, account recovery). Step 3 Configure sign-up experience (self-registration, required attributes, verification messages). Step 4 Configure message delivery (email provider SES/Cognito, SMS). Step 5 Integrate your app (app name, client type public/confidential, hosted UI, callback/sign-out URLs). Step 6 Review and create.
**Actions:** Create user pool, Create user, Create group, Create app client, Delete pool, Reset user password, Confirm user, Enable/disable user.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _User pools and identity pools._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started — local JWT auth is the CloudForge model, not Cognito.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** user pools CRUD `POST/GET/DELETE /api/v1/aws/cognito/user-pools`, users CRUD + admin actions `/api/v1/aws/cognito/user-pools/{id}/users` (create, disable, confirm, reset-password, delete), groups CRUD `/api/v1/aws/cognito/user-pools/{id}/groups`, app clients `/api/v1/aws/cognito/user-pools/{id}/clients`, identity pools CRUD `/api/v1/aws/cognito/identity-pools`, plus a sign-in endpoint `POST /api/v1/aws/cognito/user-pools/{id}/auth` (username/password → tokens) for the hosted-UI teaching flow.
**Auth reuse (do not build a second auth system):** pool users are stored as CloudForge users with a `pool_id` marker, or in a dedicated table; passwords use the existing Argon2id hasher (backend/internal/auth/password.go) and tokens reuse the existing HS256 JWT issuer with a `pool_id` claim. MFA: TOTP only (the platform already has no SMS/email sender — SMS/email verification is labelled unavailable; TOTP enrolment/verification is implementable with a standard TOTP library and a QR code rendered in the console). Hosted UI = a CloudForge-branded sign-in page at `/cognito/{poolId}/login` that issues the pool token. Identity pools issue short-lived tokens via the existing AssumeRole session mechanism (STS simulator) — guest identities are labelled as local-only.
**Data model:** Postgres `pX_cognito_pools` (id, name, signin_options text[], mfa, password_policy jsonb, self_signup, created_at) + `pX_cognito_users` (id, pool_id, username, email, phone, status, email_verified, password_hash, totp_secret_encrypted, created_at) + `pX_cognito_groups` (id, pool_id, name, description, precedence, role) + `pX_cognito_clients` (id, pool_id, name, client_secret_hash, callback_urls text[], created_at) + `pX_cognito_identity_pools` (id, name, unauthenticated_enabled, auth_role, unauth_role, created_at).
**Labs / lessons:** one lesson in the `iam` track: create a user pool, register a user, sign in through the hosted UI, call an API Gateway endpoint with the token; then create an identity pool and watch the temporary credentials. Reuse the IAM lesson structure.
**Lite feasibility:** Yes — pure Go + Postgres, no Docker/KVM. SMS/email delivery stays unavailable in both modes (no mail sender); TOTP works in lite.

## 5. Lite feasibility and scope

- **Scope: large.** User pools/users/groups/app clients on the existing Argon2id/JWT auth; TOTP MFA; hosted UI.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `cognito`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
