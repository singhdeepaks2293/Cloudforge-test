# Polly — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `polly` |
| Name | Polly |
| Category | Machine Learning and AI |
| Registry route | `/services/polly` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | voice |
| Learning tracks linked | none linked |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

Polly is the text-to-speech service: send text and get back lifelike speech in dozens of languages and voices (standard, neural, long-form, generative engines), with SSML for pronunciation, pauses and emphasis, and lexicons for custom pronunciations. A learner meets it as the voice behind audio content, accessibility features, and voice assistants — the 'make it speak' service.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Text-to-Speech, Lexicons, Speech synthesis tasks.
**Home — Text-to-Speech (single-page synthesiser):** text box (type or paste text, SSML toggle), voice selector (language → voice → engine: Standard / Neural / Long-form / Generative), output format (MP3 / OGG Vorbis / PCM), sample rate, Listen button (plays audio in-browser), Download button (saves the audio file), "Save to S3" option.
**Lexicons list:** columns Lexicon name, Language, Alphabet (IPA / X-SAMPA), Size, Last modified. Detail: lexicon XML viewer/editor.
**Speech synthesis tasks list:** columns Task ID, Status (inProgress / completed / failed), Output S3 bucket/key, Text type, Voice, Created.
**Wizard — Start speech synthesis task:** text (or S3 input), output S3 bucket, voice, engine, output format, lexicon names, SNS topic for completion.
**Actions:** Synthesize speech (real-time), Download audio, Create lexicon, Upload lexicon, Start speech synthesis task, Delete lexicon.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Text-to-speech._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** `POST /api/v1/aws/polly/synthesize` (text → audio, returns audio bytes or a download URL), `POST /api/v1/aws/polly/tasks` (async synthesis to S3), `GET /api/v1/aws/polly/tasks`, lexicons CRUD `/api/v1/aws/polly/lexicons`.
**Speech implementation (local TTS, honestly labelled):** a real TTS engine is the hard part. Options in order: (a) if the host has `espeak-ng` or `festival` installed, shell out to it through the sandboxed runner (capability check; record which engine produced the audio); (b) otherwise, implement a tiny formant-free fallback — no, do not fake audio. The honest rule: if no TTS binary is present, Synthesize returns a clear 503 naming the missing binary and how to install it; never return silence or a tone and call it speech. SSML: parse a subset (`<speak>`, `<break>`, `<emphasis>`, `<prosody rate/pitch>`, `<phoneme>`, `<say-as>`) and pass through what the engine supports, ignoring the rest with a note. Lexicons: store PLS XML, apply `<grapheme>`→`<phoneme>` replacements to the text before synthesis. Async tasks write MP3 (or WAV from espeak) to a MinIO bucket (full mode) or local disk (lite) and notify via SNS.
**Data model:** Postgres `pX_polly_tasks` (id, text_type, voice_id, engine, output_format, output_location, status, created_at, finished_at) + `pX_polly_lexicons` (id, name, language, alphabet, content, created_at).
**Labs / lessons:** one short lesson in the ML/AI area: synthesise the same text in two voices, apply a lexicon to fix a brand-name pronunciation, run an async task and fetch the audio. Keep the "engine is espeak-ng, not a neural voice" label visible.
**Lite feasibility:** Partial — the API and lexicons work in lite; actual audio needs a TTS binary on the host (capability-gated, 503 when absent). Async tasks to S3 need MinIO (full mode).

## 5. Lite feasibility and scope

- **Scope: small.** Local TTS binary (espeak-ng) via the sandboxed runner + lexicon application; 503 when absent.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `polly`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
