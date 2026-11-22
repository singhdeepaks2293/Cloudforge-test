# Comprehend — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `comprehend` |
| Name | Comprehend |
| Category | Machine Learning and AI |
| Registry route | `/services/comprehend` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | job |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Comprehend is the natural-language-processing service: send it text and it returns entities (people, places, organisations), key phrases, sentiment, language, syntax, and personally identifiable information — or train a custom classifier / entity recogniser on your own labelled documents. A learner meets it as the 'understand text without training a model' service, and as the NLP counterpart to Rekognition for images.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Analysis jobs, Custom classification, Custom entity recognition, Endpoints, Events detection, (Real-time analysis demo page).
**Home — Analysis jobs list:** columns Job name, Job type (Entities / Key phrases / Sentiment / Language / Syntax / PII), Status (Submitted / In progress / Completed / Failed / Stopped), Language, Input data (S3 location), Output data (S3 location), Submitted. Filters: job type, status.
**Job detail:** input/output locations, language, job configuration, output files (download links to the S3 output objects).
**Custom classification list:** columns Classifier name, Status (Training / Trained / In error), Language, Created. Detail: training data location, labels, accuracy metrics (precision/recall/F1 per label), test results.
**Custom entity recognition list:** columns Recognizer name, Status, Language, Created. Detail: entity types, training metrics.
**Endpoints list:** columns Endpoint name, Status, Model (classifier/recognizer), Inference units, Created.
**Real-time analysis page:** text box + Analyse button; results panels (Entities table with type + confidence, Key phrases, Sentiment with scores, Language, Syntax tokens).
**Wizard — Start analysis job:** 1. Input data (S3 location, input format one document per file / one document per line). 2. Output data (S3 location). 3. Analysis type (entities / key phrases / language / sentiment / syntax / PII). 4. Language. 5. IAM role (access permissions). 6. Review.
**Actions:** Start analysis job, Stop job, Analyse text (real-time), Train classifier/recognizer, Create endpoint, Delete job.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Natural-language analysis._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** `POST /api/v1/aws/comprehend/jobs` (start analysis job), `GET /api/v1/aws/comprehend/jobs`, `GET /api/v1/aws/comprehend/jobs/{id}`, `POST /api/v1/aws/comprehend/jobs/{id}/stop`, `POST /api/v1/aws/comprehend/analyze` (real-time: entities/key-phrases/sentiment/language/syntax), custom classifiers/recognizers CRUD + train endpoints, endpoints CRUD.
**NLP implementation (local libraries, no model downloads at runtime):** use a small pure-Go NLP approach and label it honestly — language detection by stop-word/character n-gram scoring; sentiment by a lexicon (positive/negative word lists, negations); entities by pattern rules (capitalised sequences for person/org, a gazetteer for places, regex for dates/quantities); key phrases by noun-phrase-ish extraction (stop-word splitting + frequency); syntax by a tiny rule-based tokenizer/POS heuristic; PII by regex (email, phone, credit-card, SSN-shaped patterns). This is a teaching approximation, NOT a real NLP model — the console must say so on every results page, and confidence scores are the lexicon/rule scores, never invented model probabilities. Custom classifiers: train a naive-Bayes text classifier in Go on user-supplied labelled CSV (label + text) — genuinely trainable locally, with real precision/recall computed on a held-out split; custom entity recognition: train a gazetteer + pattern recogniser from labelled examples. Jobs run async via the worker jobs table (same pattern as Terraform jobs) and read input from S3 (MinIO) or a local path, writing JSON output back to S3.
**Data model:** Postgres `pX_comprehend_jobs` (id, name, job_type, status, language, input_location, output_location, created_by, created_at, finished_at) + `pX_comprehend_classifiers` (id, name, status, language, labels text[], metrics jsonb, model_path, created_at) + `pX_comprehend_recognizers` (id, name, status, language, entity_types text[], metrics jsonb, created_at) + `pX_comprehend_endpoints` (id, name, model_type, model_id, inference_units, status, created_at).
**Labs / lessons:** one lesson in the `databases`/ML area: run entity + sentiment analysis over support-ticket text in S3, then train a custom classifier on labelled tickets and compare accuracy. Reuse the S3 lesson for input/output.
**Lite feasibility:** Yes — pure Go + Postgres; S3 input/output needs MinIO, so in lite jobs read from local paths instead and the S3 option is disabled with the reason.

## 5. Lite feasibility and scope

- **Scope: medium.** Local lexicon/rule-based NLP + naive-Bayes custom classifiers, honestly labelled as approximations.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `comprehend`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
