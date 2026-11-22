# Transfer Family — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `transfer` |
| Name | Transfer Family |
| Category | Migration and Transfer |
| Registry route | `/services/transfer` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | Yes — frontend/src/pages/MigrationConsoles.tsx `TransferConsole`, route `/services/transfer` (UNSUPPORTED explainer console) |
| Resource types | server |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

Transfer Family is the managed file-transfer service: SFTP, FTPS and FTP servers (service-managed, directory-service or custom identity providers) whose storage is S3, with per-user home directories, IAM roles, and logging. A learner meets it as the 'let a partner upload files by SFTP' service — the managed replacement for running an SFTP server on an EC2 instance — and as the file-transfer counterpart to DataSync.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Servers, Users (per server), (Workflows).
**Home — Servers list:** columns Server ID, Endpoint type (Public / VPC), Status (Online / Offline / Starting / Stopping), Domain (S3 / EFS), Protocols (SFTP/FTPS/FTP), Created. Filters: status, search.
**Server detail tabs:** Users (table: User name, Home directory, IAM role, SSH public keys, Status; Add user), Additional details (endpoint, host key fingerprint, logging role, security policy, protocols, identity provider), Monitoring (metrics: bytes in/out, files, failed logins), Tags.
**User detail:** home directory, IAM role, SSH keys (manage), POSIX profile (UID/GID, secondary GIDs).
**Wizard — Create server:** Step 1 Choose protocols (SFTP / FTPS / FTP) + identity provider (Service managed / AWS Directory Service / Custom via API Gateway/Lambda). Step 2 Choose an endpoint (Publicly accessible / VPC hosted, VPC/subnets/security groups, Elastic IP). Step 3 Choose a domain (Amazon S3 / Amazon EFS). Step 4 Configure additional details (logging role, CloudWatch logging, security policy, custom hostname + certificate). Step 5 Review and create.
**Wizard — Add user:** user name, home directory (S3 path), IAM role, SSH public key(s), POSIX profile, tags.
**Actions:** Create server, Start server, Stop server, Delete server, Add user, Delete user, Update server (protocols/logging), Test identity provider.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Managed file transfer — UNSUPPORTED (no local SFTP server)._
- **What the current console shows (registry mirrors note):** Servers list — the console states UNSUPPORTED and why
- **How it differs / what is missing (registry differs note):** Phase 6 decision: UNSUPPORTED — a managed SFTP endpoint needs a real local SFTP server and this host has no sshd/sftp-server binary. Use DataSync for file transfer in CloudForge.
- **Console today:** Yes — frontend/src/pages/MigrationConsoles.tsx `TransferConsole`, route `/services/transfer` (UNSUPPORTED explainer console). Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend: explicit UNSUPPORTED decision — do not build silently; the decision is recorded, build only as a gated phase.** Current: Phase 6 decision — UNSUPPORTED, because a managed SFTP endpoint needs a real local SFTP server and this host has no sshd/sftp-server binary. `GET /api/v1/aws/transfer/info` returns the decision, the reason, the binaries checked, and DataSync as the alternative; the console (MigrationConsoles.tsx `TransferConsole`) states UNSUPPORTED and why (backend/internal/httpapi/phase6.go). To build later (full mode only, gated behind a capability check):
1. **Runtime:** an SFTP server is the prerequisite. Options: (a) the host's OpenSSH `sftp-server` subsystem if present (capability check at startup, recorded in the info endpoint); (b) a pure-Go SFTP server library (e.g. pkg/sftp) speaking the SFTP protocol over SSH — no system sshd needed, and the server process is managed by the API/host agent. Option (b) is the portable path and the recommended one; it still needs a real SSH listener on a local port and host keys generated at first start.
2. **Backend endpoints (new):** servers CRUD `POST/GET/DELETE /api/v1/aws/transfer/servers`, `POST .../{id}/start|stop`, users CRUD `/api/v1/aws/transfer/servers/{id}/users`, `GET /api/v1/aws/transfer/servers/{id}/metrics`. A server record maps to a running SFTP listener (port allocated per server, recorded); users map to S3 (MinIO) home directories with an IAM role record; authentication = SSH public key (stored) or password (hashed, shown once). FTPS/FTP are out of scope for v1 (SFTP only, labelled).
3. **Storage:** user home directories are MinIO bucket prefixes (full mode) or local-disk directories (lite); uploads land in the bucket and appear in the S3 console.
4. **Logging:** every login/upload/download is written to the audit log and shown on the server's Monitoring tab; failed logins feed the GuardDuty brute-force check (same audit-log evidence pattern).
**Data model:** Postgres `pX_transfer_servers` (id, server_id, endpoint_type, status, domain, protocols text[], identity_provider, port, host_key_fingerprint, logging_role, created_at) + `pX_transfer_users` (id, server_id, username, home_directory, role, ssh_keys text[], posix jsonb, created_at).
**Labs / lessons:** one lesson in the `storage`/migration area: create a server, add a user with an SSH key, upload a file by SFTP, find it in S3. Until the runtime exists, the lesson is the Transfer console's "why unsupported + use DataSync" page.
**Lite feasibility:** Partial — a pure-Go SFTP server could run in lite (no Docker/KVM), but the port + host-key management and the MinIO dependency make it full-mode-first; lite keeps the UNSUPPORTED explainer until the Go server is proven.

## 5. Lite feasibility and scope

- **Scope: large.** Pure-Go SFTP server (portable path) + server/user model over MinIO home directories; SFTP only in v1.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `transfer`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
