# Certificate Manager — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `acm` |
| Name | Certificate Manager |
| Category | Security and Identity |
| Registry route | `/services/acm` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | certificate |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Certificate Manager is the managed TLS certificate service: it issues, renews and stores public and private X.509 certificates so load balancers, distributions and APIs can terminate TLS without the operator running a certificate authority. A learner meets it when a load balancer or API needs HTTPS, when a domain needs a certificate, and when certificate expiry and renewal become an operational risk.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Certificates (List certificates), Request a certificate, Import a certificate.
**Home / list page:** Certificates table — columns Domain name, Status (Issued / Pending validation / Expired / Revoked / Failed), Type (Public / Private / Imported), In use? (yes/no with resource count), Renewal status (Managed / Ineligible), Key algorithm, Expiry date. Filters: search by domain, status filter, type filter. Banner on the home page when certificates expire within 45 days.
**Detail page tabs:** Description (ARN, domain name, status, type, key algorithm, serial, issued at, expiry, renewal eligibility), Domains (each domain + validation status), Tags, In use by (list of attached resources — load balancers, distributions).
**Wizard — Request a certificate:** 1. Certificate type (public / private). 2. Domain names (fully qualified names, wildcard `*.example.com` allowed) + validation method (DNS validation / email validation). 3. Key algorithm (RSA 2048 / ECDSA P-256 etc.). 4. Tags. 5. Review and request. After requesting: validation page shows the DNS CNAME record (name/value) to create, or the approval email sent.
**Wizard — Import a certificate:** paste certificate body, private key, certificate chain; tags; review.
**Actions:** Request, Import, Delete, Add domains (re-issue), Renew (managed renewal is automatic; manual renewal for imported), Export (private certificates only, with passphrase).

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _TLS certificates._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** `POST /api/v1/aws/acm/certificates` (request), `POST /api/v1/aws/acm/certificates/import`, `GET /api/v1/aws/acm/certificates`, `GET /api/v1/aws/acm/certificates/{id}`, `DELETE /api/v1/aws/acm/certificates/{id}`, `GET /api/v1/aws/acm/certificates/{id}/validation` (DNS record to create). No real CA is available locally, so certificates must be self-signed locally-issued certificates: generate key + self-signed cert with Go `crypto/x509` (or openssl via the sandboxed runner), store the private key envelope-encrypted with a KMS key (same pattern as Secrets Manager), and record expiry from the cert itself. Validation is simulated honestly: DNS validation shows the CNAME record and a "Mark validated" step that checks a Route 53 record exists in CloudForge's own DNS store; email validation cannot send mail, so it is labelled unavailable with the reason. Renewal = re-issue a fresh self-signed cert before expiry.
**Data model:** Postgres `pX_acm_certificates` (id, domain_names text[], type public/private/imported, status, key_algorithm, cert_pem, encrypted_key, chain_pem, serial, not_before, not_after, validation_method, validation_records jsonb, tags jsonb, created_by, created_at).
**Labs / lessons:** add one lesson to the `iam`/`networking` tracks: request a certificate, attach it to an ELB listener (lite) or API Gateway stage, watch expiry/renewal. One break-and-fix scenario: expired certificate on a load balancer.
**Lite feasibility:** Yes — self-signed generation is pure Go, no Docker/KVM, tiny storage. Certificate expiry/renewal teaching works fully in lite; only real public trust (public CA) is impossible locally and must stay labelled as such.

## 5. Lite feasibility and scope

- **Scope: medium.** Local self-signed certificate issuance + envelope-encrypted key storage + validation flow.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `acm`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
