# Snowball — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `snowball` |
| Name | Snowball |
| Category | Storage |
| Registry route | `/services/snowball` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | job |
| Learning tracks linked | aws-sap |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

Snowball is the petabyte-scale offline data-transfer service: order a rugged device, copy data onto it locally, ship it back, and the data lands in S3 — for migrations where the network is too slow or the dataset too large to upload. A learner meets it as the answer to 'how do you move 100 TB when your uplink is 100 Mbps?' and as the physical counterpart to DataSync.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Jobs, Addresses.
**Home — Jobs list:** columns Job ID, Job type (Import / Export), Status (Job created / Preparing device / In transit to customer / At customer / In transit to AWS / At AWS / Completed / Cancelled), Device (Snowball Edge Storage Optimized / Compute Optimized), Created. Filters: type, status.
**Job detail:** status timeline (created → preparing → shipped → at customer → return shipped → at AWS → completed), shipping details (address, tracking numbers), data transfer summary (S3 bucket, prefix, bytes transferred, files), device details (device ID, EKS/AWS services on device), notifications (SNS topic), job metadata.
**Addresses list:** columns Address name, Address, Created. Create address wizard: name, street, city, state, postal code, country, phone.
**Wizard — Create job:** Step 1 Job type (import into S3 / export from S3). Step 2 Device (Snowball Edge Storage Optimized / Compute Optimized; EKS Anywhere on device option). Step 3 S3 bucket + prefix + IAM role (import) or S3 objects (export). Step 4 Shipping address (select/create). Step 5 Notifications (SNS topic). Step 6 Review and create.
**Actions:** Create job, Cancel job, Clone job, Update job (before shipping), List jobs.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Petabyte-scale offline data transfer._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started — DataSync and DMS records are the available migration practice.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** jobs CRUD `POST/GET/DELETE /api/v1/aws/snowball/jobs`, `POST /api/v1/aws/snowball/jobs/{id}/cancel`, addresses CRUD `/api/v1/aws/snowball/addresses`, `POST /api/v1/aws/snowball/jobs/{id}/advance` (move the job to the next status — the teaching control).
**Implementation (a tracker, not a device):** there is no physical device locally and there never will be — Snowball in CloudForge is a job tracker that teaches the workflow and the data-transfer math. A job records the S3 bucket/prefix and the byte count; the status timeline advances only when the learner clicks "Advance" (or on a schedule the learner sets), and each transition is logged with a timestamp. The teaching value is the transfer calculator: given the dataset size and the uplink speed, show the network transfer time vs the Snowball turnaround (days) so learners see WHEN offline transfer wins. Import jobs: when the job reaches "Completed", the listed objects are verified to exist in the bucket (real check against MinIO/local disk). Export jobs: the listed objects are copied to a local export directory. No device is emulated, no data is "shipped" — the console says the device is simulated and the transfer math is real.
**Data model:** Postgres `pX_snowball_jobs` (id, job_type, status, device_type, bucket, prefix, bytes_total, files_total, address_id, notification_topic, created_at, status_history jsonb) + `pX_snowball_addresses` (id, name, address jsonb, created_at).
**Labs / lessons:** one short lesson in the `storage`/migration area: size a 50 TB migration three ways (network, DataSync, Snowball) and pick the winner with the calculator; create a Snowball job and walk its timeline.
**Lite feasibility:** Yes — pure Postgres tracker; the S3 verification needs MinIO in full mode, local-disk check in lite.

## 5. Lite feasibility and scope

- **Scope: small.** Job/address tracker + transfer-time calculator; no device emulation.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `snowball`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
