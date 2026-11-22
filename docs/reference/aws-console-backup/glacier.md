# S3 Glacier — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `glacier` |
| Name | S3 Glacier |
| Category | Storage |
| Registry route | `/services/glacier` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | archive |
| Learning tracks linked | storage |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

S3 Glacier is the archival storage class for data that is rarely accessed: vaults hold archives, retrieval takes minutes to hours depending on the tier (Expedited / Standard / Bulk), and the cost is far below standard storage. A learner meets it as the cold end of the storage lifecycle — the destination of S3 lifecycle rules — and as the service where retrieval time, not just price, is part of the design.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Vaults, Jobs (per vault), Data retrieval policies.
**Home — Vaults list:** columns Vault name, Created, Size (number of archives / total bytes), Last inventory date. Filters: search.
**Vault detail tabs:** Overview (vault ARN, creation date, size, number of archives), Jobs (table: Job ID, Action — Inventory retrieval / Archive retrieval / Selective retrieval — Status, Started, Completed, Output location), Inventory (last inventory listing, Retrieve inventory button), Notifications (SNS topic), Tags, Data retrieval policy.
**Wizard — Create vault:** vault name, notifications (SNS topic), tags, review.
**Wizard — Initiate job:** job type (inventory retrieval / archive retrieval / selective retrieval with expression), archive ID (for retrieval), description, SNS topic for completion, tier (Expedited / Standard / Bulk), review.
**Actions:** Create vault, Delete vault, Upload archive (multipart upload), Initiate job (inventory/archive/selective retrieval), Delete archive, Set vault notifications, Set data retrieval policy.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Archive storage and retrieval._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** vaults CRUD `POST/GET/DELETE /api/v1/aws/glacier/vaults`, archives `POST /api/v1/aws/glacier/vaults/{id}/archives` (upload), `GET .../archives`, `DELETE .../archives/{archiveId}`, jobs `POST /api/v1/aws/glacier/vaults/{id}/jobs` (initiate), `GET .../jobs`, `GET .../jobs/{jobId}/output`.
**Storage reuse:** a vault is a MinIO bucket with a `glacier` marker (or a local directory under the workdir in lite); archives are objects with Glacier metadata (archive ID = SHA-256 of content, size, description, creation date). Retrieval tiers are simulated honestly as delays: Expedited completes in ~1 minute, Standard in ~5 minutes, Bulk in ~15 minutes (configurable, and the job page shows the countdown) — the archive bytes are already local, so the delay is the teaching point, not a real restore. Inventory retrieval returns the real archive listing from the vault's stored metadata. Upload uses the existing S3 PutObject path (multipart: reuse the ECR-style chunked approach or accept a single upload with the 10 MiB cap, documented). Lifecycle integration: an S3 lifecycle rule that transitions objects to Glacier creates a Glacier archive record — implement this as the bridge between the S3 console and Glacier, after S3 lifecycle rules exist.
**Data model:** Postgres `pX_glacier_vaults` (id, name, arn, notification_topic, created_at) + `pX_glacier_archives` (id, vault_id, archive_id, description, size_bytes, sha256, location, created_at) + `pX_glacier_jobs` (id, vault_id, job_type, tier, status, archive_id, output_location, requested_at, completed_at).
**Labs / lessons:** one lesson in the `storage` track: create a vault, upload an archive, retrieve it with each tier and compare wait times and (teaching) costs; set an S3 lifecycle rule that archives old objects.
**Lite feasibility:** Yes — MinIO is full-mode, so in lite vaults live on local disk under the workdir and the S3-backed option is disabled; retrieval delays are pure timers.

## 5. Lite feasibility and scope

- **Scope: small.** Vault/archive/job model over MinIO (or local disk in lite) with honest retrieval-tier delays.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `glacier`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
