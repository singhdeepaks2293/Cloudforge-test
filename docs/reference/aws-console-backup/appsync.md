# AppSync — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `appsync` |
| Name | AppSync |
| Category | Serverless |
| Registry route | `/services/appsync` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | api, schema |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

AppSync is the managed GraphQL service: it hosts a GraphQL API with a schema, resolvers that map fields to data sources (DynamoDB, Lambda, HTTP, RDS, OpenSearch, EventBridge), real-time subscriptions over WebSockets, and offline sync. A learner meets it when an application needs one flexible API endpoint instead of many REST routes, and when subscriptions push live updates to clients.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** APIs, Data sources, Settings (per API: Schema, Queries, Data sources, Settings, Monitoring, Logs).
**Home / list page:** APIs table — columns API name, API ID, Endpoint (GraphQL URL), Authorization type, Created. Filter by name.
**Detail page tabs:** Schema (schema editor with syntax highlighting, Save schema, Actions: import/export schema, attach resolvers per field), Queries (query editor with explorer, run query, variables, results pane), Data sources (table: Name, Type, Resource, Actions), Settings (API details, authorization modes, logging, caching, WAF), Monitoring (request/latency/error graphs), Logs (CloudWatch log link).
**Wizard — Create API:** choose "Build from scratch" (name + auth) / "Import from existing schema" / "Use a sample" (Event App, etc.), then authorization (API key / Cognito user pool / IAM / OIDC / Lambda authorizer), then schema.
**Actions:** Create API, Create data source, Create resolver (per schema field: data source, request/response mapping templates VTL or JavaScript), Run query, Update API key, Delete API.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _GraphQL APIs and schemas._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** `POST /api/v1/aws/appsync/apis`, `GET /api/v1/aws/appsync/apis`, `GET /api/v1/aws/appsync/apis/{id}`, `DELETE /api/v1/aws/appsync/apis/{id}`, `PUT /api/v1/aws/appsync/apis/{id}/schema`, `GET /api/v1/aws/appsync/apis/{id}/schema`, data sources CRUD under `/api/v1/aws/appsync/apis/{id}/datasources`, resolvers CRUD under `.../resolvers`, `POST /api/v1/aws/appsync/apis/{id}/query` (execute a query against the stored schema with a toy resolver engine).
**Resolver engine (honest scope):** implement a small GraphQL executor in Go (parse query, walk schema, call resolvers) — do NOT claim full GraphQL spec. Data source types to support first: DynamoDB (CloudForge Postgres subset), Lambda (real invocation), HTTP (SSRF-guarded), NONE (direct resolver). Subscriptions/WebSockets are out of scope for v1 — the Queries tab runs one-shot queries; subscriptions are shown disabled with the reason. Auth modes: API key only in v1 (key stored hashed, shown once); Cognito/IAM/OIDC are disabled with reasons.
**Data model:** Postgres `pX_appsync_apis` (id, name, endpoint, auth_type, api_key_hash, schema_text, created_by, created_at) + `pX_appsync_datasources` (id, api_id, name, type, config jsonb) + `pX_appsync_resolvers` (id, api_id, type_name, field_name, datasource_id, request_template, response_template).
**Labs / lessons:** one lesson in the `serverless` track: build a tiny todo API (schema + DynamoDB data source + queries), then add a Lambda resolver. Reuse DynamoDB and Lambda lessons.
**Lite feasibility:** Yes — pure Go + Postgres, no Docker/KVM. The GraphQL executor is the main work; subscriptions stay out of lite too.

## 5. Lite feasibility and scope

- **Scope: large.** Small GraphQL executor + schema/datasource/resolver model; subscriptions out of scope.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `appsync`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
