# Rekognition — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `rekognition` |
| Name | Rekognition |
| Category | Machine Learning and AI |
| Registry route | `/services/rekognition` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | collection |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

Rekognition is the image-and-video analysis service: detect labels (objects/scenes), faces (detect, compare, search in collections), celebrities, text in images, unsafe content, and custom labels trained on your own images — all by API, with no ML expertise. A learner meets it as the 'what is in this picture?' service, and as the vision counterpart to Comprehend for text.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Face comparison, Face search, Celebrity recognition, Label detection, Text detection, Content moderation, Custom Labels, Collections, (Video analysis).
**Home — demo pages (each is a try-it panel):** Face comparison (upload two images → similarity score + bounding boxes), Label detection (upload image → labels table: label, confidence), Text detection (upload image → detected text lines), Content moderation (upload image → moderation labels + confidence), Celebrity recognition (upload image → celebrity name + confidence + URLs).
**Collections list:** columns Collection name, Face count, Created. Collection detail: faces table (face ID, external image ID, bounding box, indexed at), Search faces (upload an image → matching faces with similarity).
**Custom Labels list:** columns Project name, Status, Created. Project detail: datasets (train/test images), model versions (status, training metrics), Detect custom labels (upload image → labels).
**Video analysis:** start a video job (S3 location, analysis type), job list (status, results in S3/JSON).
**Actions:** Create collection, Index faces, Search faces by image, Detect labels/moderation/text/celebrities, Create Custom Labels project, Train model, Start video analysis job.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Image and video analysis._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** collections CRUD `POST/GET/DELETE /api/v1/aws/rekognition/collections`, faces `/api/v1/aws/rekognition/collections/{id}/faces` (index, list, search), `POST /api/v1/aws/rekognition/detect-labels|detect-moderation|detect-text|compare-faces|recognize-celebrities`, Custom Labels projects/models, video jobs `POST /api/v1/aws/rekognition/video-jobs`.
**Vision implementation (local, honestly labelled — no model downloads at runtime):** use a small local approach and say so on every results page: label detection via image metadata + a colour/shape heuristic is NOT acceptable as a fake — instead, implement (a) if the host has a local vision model available (e.g. an ONNX model file configured by the operator), run it through a local inference binary and label results with the model name; (b) otherwise, implement collections + face search with perceptual hashing (average hash / dHash over the image, Hamming distance as similarity) — genuinely working similarity search, labelled as hash-based, not face recognition; (c) text detection via a local OCR binary (tesseract, capability check, 503 when absent); (d) label/moderation/celebrity detection are NOT implemented without a model — the console shows them disabled with the reason, never with invented labels. Custom Labels: train a nearest-neighbour classifier over perceptual hashes of the training images — real, small, and honestly described. Video jobs: extract frames with ffmpeg (capability check) and run the same image analysis per frame.
**Data model:** Postgres `pX_rekognition_collections` (id, name, face_count, created_at) + `pX_rekognition_faces` (id, collection_id, external_image_id, image_hash, hash_bits, bounding_box jsonb, created_at) + `pX_rekognition_projects` (id, name, status, created_at) + `pX_rekognition_models` (id, project_id, version, status, metrics jsonb, created_at) + `pX_rekognition_video_jobs` (id, input_location, analysis_type, status, output_location, created_at).
**Labs / lessons:** one lesson in the ML/AI area: build a collection, index faces, search by image, and see why hash similarity is not face recognition; run OCR on a document image. Keep the "no real vision model" label front and centre.
**Lite feasibility:** Partial — collections + hash search + OCR work in lite when tesseract is present; anything needing a model binary or ffmpeg is capability-gated (503 when absent).

## 5. Lite feasibility and scope

- **Scope: large.** Collections + perceptual-hash face search + tesseract OCR; label/moderation stay disabled without a model.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `rekognition`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
