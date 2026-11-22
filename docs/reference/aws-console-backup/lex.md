# Lex — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `lex` |
| Name | Lex |
| Category | Machine Learning and AI |
| Registry route | `/services/lex` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | bot |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Lex is the conversational-AI service for chatbots and voice bots: define intents (what the user wants), utterances (example phrases), slots (parameters to collect), and fulfilment (a Lambda function or a response); Lex handles natural-language understanding, slot elicitation, and conversation state. A learner meets it as the 'build a chatbot' service, and as the voice/text front end that can call Lambda and Lex's own fulfilment.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Bots, (per bot) Intents, Slot types, Test, Aliases, Versions, Channels.
**Home — Bots list:** columns Bot name, Status (Building / Ready / Failed), Version, Language, Created. Filters: search, status.
**Bot detail tabs:** Intents (table: intent name, sample utterances count, slots count; click to edit), Slot types (built-in + custom), Build (build status + Build button), Test (chat test window: type a message, see intent/slots/confidence, inspect the conversation), Aliases (alias name → version), Versions (snapshot list), Channels (Facebook, Slack, Twilio integrations), Monitoring (conversation metrics).
**Intent editor:** intent name, sample utterances list (add/remove), slots table (slot name, type, prompt, required), confirmation prompt, fulfilment (Lambda function / return parameters), closing response, error handling.
**Wizard — Create bot:** Step 1 Bot configuration (bot name, IAM role, COPPA, idle session timeout). Step 2 Add language (language, voice for speech). Step 3 Review. Then add intents.
**Actions:** Create bot, Create intent, Add utterance/slot, Build bot, Test bot, Create alias/version, Delete bot, Publish (create version + alias).

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Conversational bots._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** bots CRUD `POST/GET/DELETE /api/v1/aws/lex/bots`, intents CRUD `/api/v1/aws/lex/bots/{id}/intents`, slot types CRUD `/api/v1/aws/lex/bots/{id}/slot-types`, aliases/versions, `POST /api/v1/aws/lex/bots/{id}/build`, `POST /api/v1/aws/lex/bots/{id}/test` (converse: send text, get intent + slots + response).
**NLU implementation (local, honestly labelled):** intent matching by utterance similarity — normalise the input (lowercase, strip punctuation), score each sample utterance by token overlap / Jaccard similarity plus a small synonym table, pick the best intent above a threshold (0.5) and return its confidence as the score; slot filling by extracting values after slot prompts (slot types: built-in AMAZON.Number/Date/Time/City via patterns, custom slot types as value lists with synonyms). Fulfilment: call the configured Lambda function with the intent + slots (real invocation, same as other triggers) or return the configured closing response. Conversation state is kept per session id in Postgres (or Redis) with the idle timeout. This is a teaching NLU, not a real model — the test window shows the matched intent, the score, and which utterances scored close, so learners see WHY it matched.
**Data model:** Postgres `pX_lex_bots` (id, name, status, language, voice, idle_timeout_seconds, created_at) + `pX_lex_intents` (id, bot_id, name, sample_utterances text[], slots jsonb, confirmation_prompt, fulfilment_type, fulfilment_lambda_id, closing_response) + `pX_lex_slot_types` (id, bot_id, name, values text[], synonyms jsonb) + `pX_lex_aliases` (id, bot_id, name, version) + `pX_lex_sessions` (id, bot_id, session_id, state jsonb, updated_at).
**Labs / lessons:** one lesson in the `serverless` track: build a "book a table" bot with two intents and a Lambda fulfilment, test it, then break it with an ambiguous utterance and add a sample utterance to fix it.
**Lite feasibility:** Yes — pure Go + Postgres (+ Lambda, which is lite); no Docker/KVM.

## 5. Lite feasibility and scope

- **Scope: medium.** Bot/intent/slot model + similarity-based NLU + Lambda fulfilment, labelled as teaching NLU.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `lex`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
