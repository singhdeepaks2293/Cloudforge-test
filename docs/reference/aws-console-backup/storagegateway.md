# Storage Gateway — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `storagegateway` |
| Name | Storage Gateway |
| Category | Storage |
| Registry route | `/services/storagegateway` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | gateway |
| Learning tracks linked | aws-sap |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Storage Gateway is the hybrid-storage service: a virtual appliance (or hardware appliance) on-premises that presents file shares (NFS/SMB), iSCSI volumes, or a virtual tape library, and transparently stores the data in S3, EBS snapshots, or Glacier. A learner meets it as the answer to 'how does the office file server become cloud storage without changing how anyone works?' — the hybrid bridge between on-premises and S3.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Gateways, Volumes, File shares, Tapes, (File System / FSx gateways).
**Home — Gateways list:** columns Gateway name, Gateway ID, Type (S3 File / FSx File / Volume / Tape), Status (Running / Offline / Maintenance), Region, Created. Filters: type, status.
**Gateway detail tabs:** Details (gateway ARN, time zone, activation key, EC2 instance / host, bandwidth, cache size), Volumes (for volume gateways: table of volumes with size, status, iSCSI target), File shares (for file gateways: table of shares with S3 bucket, protocol NFS/SMB, status), Tapes (for tape gateways: virtual tapes with barcode, status), CloudWatch alarms, Tags.
**Wizard — Create gateway:** Step 1 Gateway type (S3 File / FSx File / Volume / Tape). Step 2 Host platform (EC2 / VMware ESXi / Hyper-V / KVM). Step 3 Activation (activation key from the deployed appliance, time zone). Step 4 Configure (gateway name, cache disk, upload buffer). Step 5 Review.
**Wizard — Create file share:** gateway, S3 bucket, file share name, protocol (NFS/SMB), access (client IPs, squash, read-only), cache settings.
**Wizard — Create volume:** gateway, volume size, iSCSI target name, snapshot schedule.
**Actions:** Create gateway, Add file share, Add volume, Create snapshot (volume), Delete gateway/volume/share, Refresh cache, Start/stop gateway.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Hybrid file, volume and tape gateways._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** gateways CRUD `POST/GET/DELETE /api/v1/aws/storagegateway/gateways`, file shares CRUD `/api/v1/aws/storagegateway/gateways/{id}/file-shares`, volumes CRUD `/api/v1/aws/storagegateway/gateways/{id}/volumes`, tapes CRUD `/api/v1/aws/storagegateway/gateways/{id}/tapes`, snapshots `/api/v1/aws/storagegateway/volumes/{id}/snapshots`, `POST /api/v1/aws/storagegateway/gateways/{id}/refresh-cache`.
**Implementation (file gateway first, honestly scoped):** a real virtual appliance is out of scope. Implement the S3 File Gateway faithfully as a local service: a gateway is a record + a local directory (the "cache"); a file share maps a local directory to a MinIO bucket (full mode) or a local-disk bucket (lite) — files written to the share directory are synced to the bucket (a worker tick that uploads new/changed files and downloads on read-through), and Refresh cache re-lists the bucket. SMB/NFS protocols are NOT served (no Samba/NFS server locally) — the share is accessed as a local path in the lab workdir, and the console says the protocol endpoint is not served. Volume Gateway: volumes are files (sparse) in the workdir with snapshots = copies; iSCSI targets are not served (labelled). Tape Gateway: virtual tapes are records with barcodes; "retrieval" copies the tape's objects back to S3. The gateway status is derived from the sync worker's last run, never hard-coded.
**Data model:** Postgres `pX_sgw_gateways` (id, name, type, status, host_platform, timezone, cache_size_gb, created_at) + `pX_sgw_shares` (id, gateway_id, name, bucket, protocol, local_path, status) + `pX_sgw_volumes` (id, gateway_id, name, size_gb, iscsi_target, status) + `pX_sgw_tapes` (id, gateway_id, barcode, status, size_bytes) + `pX_sgw_snapshots` (id, volume_id, name, created_at).
**Labs / lessons:** one lesson in the `storage` track: create a file gateway, write files to the share, watch them appear in S3, refresh the cache, delete locally and recover from S3. Reuse the S3 lesson.
**Lite feasibility:** Partial — file shares over local disk work in lite; the MinIO-backed sync needs MinIO (full mode). Volume/tape gateways are records + local files in both modes.

## 5. Lite feasibility and scope

- **Scope: medium.** File gateway sync (local dir <-> S3/MinIO) + volume/tape records; no NFS/SMB/iSCSI serving.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `storagegateway`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
