# X-Ray — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `xray` |
| Name | X-Ray |
| Category | Developer Tools |
| Registry route | `/services/xray` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | trace |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

X-Ray is the distributed-tracing service: instrument your application (SDK or ADOT), and X-Ray collects traces (segments and subsegments with timings), draws a service map of how requests flow between services, and lets you query traces by latency, errors, or annotations. A learner meets it as the 'why is this request slow?' tool — the observability layer that turns a slow Lambda or API call into a visible chain of timed segments.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Traces, Service map, Insights, Groups, Sampling, Analytics, (Traces: Trace list, Trace details).
**Home — Service map:** graph of services (nodes) and edges (calls) with health colouring (green/yellow/red by error rate) and latency labels; time-range selector (5 min / 1 h / 6 h / custom); click a node for its details (latency, error rate, requests).
**Traces list:** columns Trace ID, Response time, Status (OK / Error / Fault / Throttle), Method + URL, Start time, Duration. Filters: service name, URL, response time (>, <), annotation key/value, time range. Click a row for the trace timeline.
**Trace detail:** timeline (waterfall of segments/subsegments with start time + duration, nested), segment detail (name, start/end, annotations, metadata, errors), raw trace JSON.
**Groups list:** columns Group name, Filter expression, Insights (enabled/disabled), Created. Create group wizard: name, filter expression (e.g. `http.url CONTAINS "/api"`), insights configuration.
**Sampling rules list:** columns Rule name, Priority, Reservoir size, Fixed rate, Host, URL path, Service name, Version. Create sampling rule wizard: rule name, priority, reservoir, fixed rate, host, service, URL path, HTTP method, version.
**Insights:** list of detected anomalies (service, impact, start time, state), click for the insight detail (affected traces, root-cause hints).
**Actions:** Query traces (filter), Create group, Create sampling rule, Delete group/rule, View trace, View service map.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Distributed traces._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** traces ingest `POST /api/v1/aws/xray/traces` (segment documents), `GET /api/v1/aws/xray/traces` (query with filters), `GET /api/v1/aws/xray/traces/{id}`, service map `GET /api/v1/aws/xray/service-map` (computed from traces), groups CRUD `/api/v1/aws/xray/groups`, sampling rules CRUD `/api/v1/aws/xray/sampling-rules`, insights `GET /api/v1/aws/xray/insights`.
**Tracing implementation (real, local — the honest core):** X-Ray only works if something emits traces. Instrument the CloudForge API itself: a chi middleware that creates a segment per HTTP request (name = route pattern, start/end, status code, annotations: user id, route) and subsegments for the expensive calls inside handlers (Postgres query, Redis call, engine gRPC call, host-agent call) using a small in-process tracer (no SDK dependency). Lambda/ECS task invocations emit a segment per invocation (the sandboxed runners already record start/finish — add segment emission there). The API Gateway invoke path emits a segment per gateway request. Traces are stored in Postgres (`pX_xray_segments`: trace_id, segment_id, parent_id, name, service, start_time, end_time, status, annotations jsonb, metadata jsonb) with a retention cap (24 h in lite, 7 days in full). The service map is computed from the segments (caller → callee edges, error rates, p50/p95 latencies) — never hard-coded. Sampling rules are enforced at ingest (reservoir + fixed rate per rule, evaluated in order of priority). Insights = a simple anomaly detector over the stored traces (error rate or p95 latency > 2× the 24 h baseline for a service) — computed, not invented.
**Data model:** `pX_xray_segments` (above) + `pX_xray_groups` (id, name, filter_expression, insights_enabled, created_at) + `pX_xray_sampling_rules` (id, name, priority, reservoir_size, fixed_rate, host, service_name, url_path, http_method, version) + `pX_xray_insights` (id, service_name, state, impact, started_at, summary jsonb).
**Labs / lessons:** one lesson in the `monitoring` track: make a slow API call (or a Lambda that sleeps), find its trace, read the waterfall, identify the slow subsegment; create a group for `/api/v1/aws/lambda/*` and watch it fill. Reuse the CloudWatch lesson.
**Lite feasibility:** Yes — in-process tracing + Postgres storage need no Docker/KVM; keep retention short in lite.

## 5. Lite feasibility and scope

- **Scope: medium.** In-process tracer (API middleware + Lambda/ECS runners) + trace store + computed service map.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `xray`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
