# Bedrock — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `bedrock` |
| Name | Bedrock |
| Category | Machine Learning and AI |
| Registry route | `/services/bedrock` |
| Availability today | available |
| Fidelity today | **SIMULATED** |
| CloudForge console today | Yes — frontend/src/pages/MlConsoles.tsx `BedrockConsole`, route `/services/bedrock` |
| Resource types | model |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Bedrock is the managed foundation-model service: call text, chat, image and embedding models from several providers through one API, build knowledge bases over your own documents, and assemble agents that call tools — without hosting a model yourself. A learner meets it as the 'use an AI model by API' service, and as the contrast to SageMaker, where you train and host your own model.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Foundation models, Playgrounds (Text / Chat / Image), Knowledge Bases, Agents, Flows, Prompt management, Model evaluation, Guardrails, Provisioned throughput, Custom models, Model import, Data automation.
**Home — Foundation models list:** columns Model name, Provider, Modality (Text / Image / Embedding / Multimodal), Lifecycle status, Created. Filter by provider/modality. Click a model for its detail (model ID, inference parameters, example prompts).
**Playground — Text/Chat:** model selector dropdown, prompt textarea, inference parameters panel (temperature, Top P, max tokens, stop sequences), Run button, response pane with token count and latency, Save prompt.
**Knowledge Bases list:** columns Name, Status, Data source, Embeddings model, Created. Detail: data sources, sync status, test (ask a question, see retrieved chunks + generated answer).
**Agents list:** columns Name, Status, Foundation model, Created. Detail: instructions, action groups (Lambda functions), test chat.
**Guardrails list:** columns Name, Status, Created. Detail: content filters, denied topics, PII filters.
**Actions:** Invoke model (playground), Create knowledge base, Create agent, Create guardrail, Start model evaluation job, Purchase provisioned throughput.

## 3. CloudForge fidelity today

- **Fidelity: SIMULATED** (availability: available). Registry description: _Foundation-model playground — SIMULATED concept entries only._
- **What the current console shows (registry mirrors note):** Foundation models list; Playground invoke flow
- **How it differs / what is missing (registry differs note):** SIMULATED: template responses only — no foundation model runs, nothing is sent to any AI provider, no paid third-party API. For a real model result, train a SageMaker model and invoke its endpoint.
- **Console today:** Yes — frontend/src/pages/MlConsoles.tsx `BedrockConsole`, route `/services/bedrock`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists as a labelled simulator — extend carefully, do not fake a model.** Current: `POST /api/v1/aws/bedrock/invoke` returns a labelled template response for 4 concept models; nothing is sent to any provider (backend/internal/httpapi/phase6.go, `BedrockTemplateResponse`). The console (MlConsoles.tsx `BedrockConsole`) shows the models and the playground with the SIMULATED badge. To build later, in order:
1. **Knowledge Bases (real, local):** data source = an S3 bucket (MinIO) or a local path; chunk documents into text; store chunks in Postgres; retrieval = keyword/BM25-style matching over chunks (no embeddings model locally — say so); generation = the template response quoting the retrieved chunks. Endpoints `POST /api/v1/aws/bedrock/knowledge-bases`, `POST .../{id}/sync`, `POST .../{id}/retrieve`, `POST .../{id}/query`.
2. **Agents (real orchestration, template model):** agent = instructions + action groups (Lambda function ids); an agent run calls the Lambda actions in order and assembles a template answer. No model reasoning is claimed.
3. **Guardrails (real filters):** denied-topic word lists + PII regex (email/credit-card patterns) applied to prompt and response; blocked invocations return a labelled refusal.
4. **Real models are OUT OF SCOPE** for CloudForge: no paid third-party API, no local LLM binary in the sandbox. If a host later has a local model server (e.g. Ollama on localhost), add an opt-in `BEDROCK_LOCAL_MODEL_URL` that proxies prompts to it and labels responses with the model actually used — never present a template as a model.
**Data model (add):** `pX_bedrock_kbs` (id, name, datasource_type, datasource_location, status, created_at) + `pX_bedrock_chunks` (id, kb_id, source, chunk_text, created_at) + `pX_bedrock_agents` (id, name, instructions, model_id, action_groups jsonb, created_at) + `pX_bedrock_guardrails` (id, name, denied_topics text[], pii_filters text[], created_at).
**Labs / lessons:** one lesson in the `databases`/`serverless` area: build a knowledge base over the curriculum docs, ask questions, inspect retrieved chunks; then add a guardrail and watch a blocked prompt. Keep the "no real model runs" label front and centre.
**Lite feasibility:** Yes — keyword retrieval + template responses + Lambda actions need no Docker/KVM. A local-model proxy, if ever added, is full-mode only.

## 5. Lite feasibility and scope

- **Scope: medium.** Knowledge Bases (local chunk retrieval) + Agents (Lambda action groups) + Guardrails; real models stay out of scope.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `bedrock`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
