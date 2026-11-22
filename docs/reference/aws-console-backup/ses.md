# SES — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `ses` |
| Name | SES |
| Category | Application Integration |
| Registry route | `/services/ses` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | identity |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

SES is the email-sending service: verify domains and email addresses, send transactional and bulk mail, track deliveries/bounces/complaints, and manage reputation (suppression lists, dedicated IPs, configuration sets with event destinations). A learner meets it as the 'send email from my application' service, and as the notification channel behind Cognito verification emails and SNS email subscriptions.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Identities, Configuration sets, Email receiving, Reputation dashboard, Suppression list, Dedicated IPs, Virtual Deliverability Manager.
**Home — Identities list:** columns Identity (domain or email address), Identity type (Domain / Email address), Status (Verified / Pending verification / Failed), DKIM (Enabled/Disabled), Created. Filters: type, status.
**Identity detail tabs:** DKIM (DKIM settings, DNS records to publish, Easy DKIM status), Notifications (bounce/complaint/delivery SNS topics), Tags, MAIL FROM domain, Authorized senders.
**Configuration sets list:** columns Name, Created. Detail: event destinations (SNS/Kinesis Firehose/CloudWatch), reputation tracking, sending options.
**Reputation dashboard:** account sending limits, bounce/complaint rates (with thresholds), daily sending quota usage.
**Suppression list:** table of suppressed addresses (email, reason bounce/complaint, last updated) with Remove action.
**Wizard — Create identity:** 1. Identity type (domain / email address). 2. Domain/email + DKIM settings (Easy DKIM signing key length, DKIM selector). 3. Tags. 4. Review. After creating a domain identity: DNS records panel (DKIM CNAMEs, MX/SPF for MAIL FROM) to publish.
**Wizard — Create configuration set:** name, event destinations (add SNS topic / Firehose stream / CloudWatch destination), reputation options.
**Actions:** Create identity, Verify identity (resend verification email), Delete identity, Send test email, Create SMTP credentials, Create configuration set, Remove from suppression list.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Email sending identities._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** identities CRUD `POST/GET/DELETE /api/v1/aws/ses/identities`, `POST /api/v1/aws/ses/identities/{id}/verify`, configuration sets CRUD `/api/v1/aws/ses/configuration-sets`, `POST /api/v1/aws/ses/send` (send email), `GET /api/v1/aws/ses/sent` (sent-mail log), suppression list `GET/DELETE /api/v1/aws/ses/suppression`, reputation `GET /api/v1/aws/ses/reputation`, SMTP credentials `POST /api/v1/aws/ses/smtp-credentials`.
**Mail delivery (the honest core):** CloudForge cannot send real internet email and must not pretend to. Implement a local mail sink: sent mail is stored (envelope + body + headers) in Postgres and viewable in a "Sent mail" mailbox page, with a delivery status of `delivered-locally` — and, if the operator configures an SMTP relay (`SES_SMTP_HOST` etc.), real delivery through that relay with the status recorded. Verification: domain identity verification checks for the DKIM CNAME records in CloudForge's own Route 53 store (the same pattern as ACM validation); email-address verification sends the verification mail to the local sink (or relay) and the recipient "clicks" a verification link served by CloudForge. Bounces/complaints: simulate by the sink (a test address pattern `bounce@...` records a bounce; `complaint@...` records a complaint) and show them on the reputation dashboard. Sending limits: a daily quota (e.g. 200/day in lite) enforced per user, shown on the reputation dashboard. SMTP credentials: generate a username/password pair (secret shown once, hashed at rest — same pattern as IAM access keys) that authenticates against the local sink endpoint.
**Data model:** Postgres `pX_ses_identities` (id, identity, type, status, dkim_enabled, dkim_records jsonb, mail_from_domain, created_at) + `pX_ses_config_sets` (id, name, event_destinations jsonb, created_at) + `pX_ses_sent` (id, from_identity, to_addresses text[], subject, body, configuration_set, status, created_at) + `pX_ses_suppression` (email, reason, last_updated) + `pX_ses_smtp_credentials` (id, username, secret_hash, created_by, created_at).
**Labs / lessons:** one lesson in the `messaging` track: verify an identity, send a test email, watch it land in the local mailbox, trigger a bounce and see the reputation dashboard move. Reuse the SNS lesson (SNS email subscriptions route here).
**Lite feasibility:** Yes — the local sink is pure Postgres; a real SMTP relay is an operator opt-in (full mode or lite, same config).

## 5. Lite feasibility and scope

- **Scope: medium.** Identities + local mail sink (stored mail, optional SMTP relay) + reputation/suppression.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `ses`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
