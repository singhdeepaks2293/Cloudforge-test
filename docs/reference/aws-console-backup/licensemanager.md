# License Manager — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `licensemanager` |
| Name | License Manager |
| Category | Additional |
| Registry route | `/services/licensemanager` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | license |
| Learning tracks linked | none linked |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

License Manager is the licence-governance service: track software licences (per-core, per-instance, per-user), define license configurations with hard/soft limits, grant licences to accounts, and get alerts when usage exceeds what was bought. A learner meets it when an organisation runs licensed software (databases, operating systems) on EC2 and needs to prove compliance instead of discovering it in an audit.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Licenses, Granted licenses, License configurations, Settings, (License conversion).
**Home — Licenses list:** columns License name, Status (Available / Disabled), Type (Perpetual / Subscription), Expiration, Entitlements (count). Filters: status, search.
**License detail:** entitlements table (name, value, unit), usage (consumed vs purchased), grants, issuer, validity dates.
**License configurations list:** columns Name, License counting type (vCPU / Instance / Core / Socket), Rules (hard limit / soft limit), Status, Associated resources (count). Detail: rules table, resource inventory (instances using the licence), usage history.
**Granted licenses list:** columns License name, Grantee, Status, Created.
**Wizard — Create license configuration:** 1. Name + description. 2. License counting type (vCPU/Instance/Core/Socket) + number of licences. 3. Rules (hard limit: stop launching when exceeded; soft limit: warn). 4. Associate resources (tags / instance ARNs). 5. Review.
**Wizard — Create license:** upload/define licence (issuer, product, entitlements, validity), grant to an account.
**Actions:** Create license, Create grant, Create license configuration, Accept grant, Disable license, Delete configuration.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Software licences._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** licenses CRUD `POST/GET/DELETE /api/v1/aws/licensemanager/licenses`, grants `/api/v1/aws/licensemanager/grants`, license configurations CRUD `/api/v1/aws/licensemanager/configurations`, usage `GET /api/v1/aws/licensemanager/configurations/{id}/usage`.
**Implementation (small, mostly records + evaluation):** a license configuration's usage is computed from real lab resources — count lab instances (EC2 console) matching the configuration's tag/instance filter, multiply by vCPUs per the instance profile (Micro=1, Small=2, Medium=4 — the teaching profiles), and compare against the purchased count; hard limit = refuse to launch a new matching instance (hook into the lab/instance launch path with a 429 + reason); soft limit = show a warning banner and record a finding. No licence files are validated cryptographically — the licence record is the source of truth and the console says so. Grants are records between CloudForge users (grantee) and licences.
**Data model:** Postgres `pX_lm_licenses` (id, name, type, issuer, product, entitlements jsonb, valid_from, valid_to, status, created_at) + `pX_lm_grants` (id, license_id, grantee_id, status, created_at) + `pX_lm_configurations` (id, name, counting_type, license_count, hard_limit, soft_limit, resource_filter jsonb, status, created_at).
**Labs / lessons:** one short lesson in the `iam`/`monitoring` area: create a configuration with a hard limit of 2 vCPUs, launch instances until the limit blocks the third, then raise the limit. Reuse the EC2 launch flow.
**Lite feasibility:** Yes — pure records + a launch-path check; the 1-lab lite cap makes hard limits easy to demonstrate.

## 5. Lite feasibility and scope

- **Scope: small.** License/configuration records + usage computed from real lab instances + hard-limit launch hook.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `licensemanager`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
