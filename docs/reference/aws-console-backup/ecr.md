# ECR — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `ecr` |
| Name | ECR |
| Category | Containers |
| Registry route | `/services/ecr` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/EcrConsole.tsx, route `/services/ecr` |
| Resource types | repository, image |
| Learning tracks linked | containers |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

ECR is the managed container image registry: private repositories that store Docker/OCI images, with image scanning, lifecycle policies, replication, and push/pull through the standard Docker Registry API. A learner meets it as the place images live before ECS, Fargate, App Runner or CodeBuild use them — the 'push once, run anywhere in the platform' step of the container workflow.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Repositories, Images (per repository), Lifecycle rules, Replication, Settings.
**Home — Repositories list:** columns Repository name, URI (`<account>.dkr.ecr.<region>.amazonaws.com/<name>`), Created, Image count, Last push. Filters: search, visibility.
**Repository detail tabs:** Images (table: Tag, Digest, Size, Pushed at, Scan status, Vulnerabilities; actions Push/Pull/Delete), Permissions (repository policy JSON editor), Lifecycle rules (rules table: rule priority, status, action expire), Tags.
**Wizard — Create repository:** 1. Repository name + visibility settings (private/public). 2. Tag immutability (mutable/immutable). 3. Scan on push (enable/disable). 4. Encryption (AES-256 / KMS). 5. Review.
**Push commands panel:** the console shows the exact commands (`aws ecr get-login-password | docker login`, `docker tag`, `docker push`) — in CloudForge these show the local registry commands instead.
**Actions:** Create repository, Delete repository, Push image, Delete image, Set lifecycle policy, Run image scan, View push commands.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Container image repositories — real local registry (Postgres catalogue + content-addressed blobs)._
- **What the current console shows (registry mirrors note):** Repositories list; image push/pull flow; image detail with layers/size/digest; scan results; lifecycle rules
- **How it differs / what is missing (registry differs note):** Real local registry speaking the Docker Registry HTTP API v2 subset (catalog, tags, manifests, monolithic blob upload) on /v2/; blobs are content-addressed files on local disk (or a registry:2 container when ECR_REGISTRY_URL is set). No Docker Hub compatibility. Image scan is an integrity scan (blob digest re-verification), NOT a vulnerability scan.
- **Console today:** Yes — frontend/src/pages/EcrConsole.tsx, route `/services/ecr`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add auth, chunked uploads and lifecycle enforcement, do not rebuild the registry.** Current: real local registry speaking the Docker Registry HTTP API v2 subset (catalog, tags, manifests, monolithic blob upload) on `/v2/`; blobs content-addressed on local disk (or a registry:2 container when `ECR_REGISTRY_URL` is set); scan is an integrity scan (digest re-verification), NOT a vulnerability scan (backend/internal/phase4/ecr.go, httpapi/phase4.go `p4_ecr_*`). Console: EcrConsole.tsx. To build later, in order:
1. **Authentication:** token auth on `/v2/` (a short-lived token issued by the API for the logged-in user, checked by the registry handlers) — currently there is no auth (local-only, Phase 7 item). Push/pull without a token must then fail with 401, honestly.
2. **Chunked/resumable uploads:** implement the blob-upload session flow (POST → PATCH chunks → PUT complete) alongside the existing monolithic upload; keep monolithic working.
3. **Lifecycle policy enforcement:** evaluate stored lifecycle rules on a worker tick (expire untagged images older than N days, keep last N images) and delete the blobs; record what was expired.
4. **OCI index / multi-arch manifests:** accept and serve index manifests (list of per-architecture manifests) instead of only single manifests.
5. **Replication:** out of scope (single local registry); the console shows replication as unavailable with the reason.
**Data model (exists, extend):** `p4_ecr_*`; add `pX_ecr_lifecycle_runs` (id, repository_id, expired_images, run_at) and token issuance is stateless (JWT with `ecr` scope) — no table needed.
**Labs / lessons:** add one lesson to the `containers` track: push an image, scan it, set a lifecycle rule, pull it into an ECS task. Reuse the existing ECR lesson.
**Lite feasibility:** Yes — the local-disk backend already works in lite; the registry:2 container is full-mode only. Auth and lifecycle are pure Go.

## 5. Lite feasibility and scope

- **Scope: medium.** Token auth on /v2/ + chunked uploads + lifecycle enforcement on the existing registry.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `ecr`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
