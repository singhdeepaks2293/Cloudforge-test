# Step Functions — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `stepfunctions` |
| Name | Step Functions |
| Category | Serverless |
| Registry route | `/services/stepfunctions` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | state-machine, execution |
| Learning tracks linked | serverless |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

Step Functions is the serverless workflow-orchestration service: model a process as a state machine (Task, Choice, Parallel, Map, Wait, Succeed, Fail, Pass) in Amazon States Language, and it runs the workflow durably — retrying, catching errors, and recording every execution. A learner meets it as the 'coordinate several Lambdas into one reliable workflow' service, and as the durable alternative to chaining Lambda invocations by hand.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** State machines, Activities, Executions (per machine), (Workflow Studio is the visual editor inside a state machine).
**Home — State machines list:** columns Name, Type (Standard / Express), Status (Active / Deleting), Created. Filters: type, search.
**State machine detail tabs:** Definition (Workflow Studio graph — drag states from the palette, connect them, edit each state's JSON; plus the raw JSON/ASL editor with validation), Executions (table: Execution name/ARN, Status Running/Succeeded/Failed/Timed out/Aborted, Started, Duration), Logging (CloudWatch log group, log level), IAM (execution role), Tags.
**Execution detail:** visual graph with the execution path highlighted (each state green/red with input/output), Event history (table: timestamp, type — ExecutionStarted, TaskStateEntered, TaskSucceeded, ChoiceStateEntered, ExecutionSucceeded — with the state's input/output JSON), input JSON viewer.
**Wizard — Create state machine:** 1. Choose authoring method (Design a workflow in Workflow Studio / Write your workflow in code / Use a template: Hello World, Map, Parallel, Callback pattern, etc.). 2. Design/code the definition (states + transitions). 3. Configure (name, type Standard/Express, execution role, logging, X-Ray tracing). 4. Review and create.
**Actions:** Create state machine, Start execution (with JSON input), Stop execution, Update state machine, Delete state machine, View execution history, Export definition.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _State machines and executions._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 3).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** state machines CRUD `POST/GET/DELETE /api/v1/aws/stepfunctions/state-machines`, `PUT .../definition`, executions `POST /api/v1/aws/stepfunctions/state-machines/{id}/executions` (start), `GET .../executions`, `GET .../executions/{execId}`, `POST .../executions/{execId}/stop`, `GET .../executions/{execId}/history`.
**Execution engine (the real work — a small interpreter, honestly scoped):** implement an interpreter in Go for a subset of Amazon States Language: states Task (Lambda function / SQS send / SNS publish / Glue job start / Step Functions nested execution — reuse the real service calls), Choice (rules: StringEquals, NumericGreaterThan, BooleanEquals, IsPresent, And/Or/Not), Pass (Result/ResultPath), Wait (Seconds/Timestamp), Parallel (branches run sequentially in-process, results combined as an array — label that Parallel is sequential in-process, not concurrent), Map (iterate over an array in the input, sequential), Succeed, Fail. Retry/Catch per state (error names, intervals, backoff, max attempts) and per-state TimeoutSeconds. Execution history = one row per state transition with input/output JSON (Postgres jsonb), so the event-history table and the graph highlight are real. Express workflows are out of scope for v1 (Standard only, labelled). Workflow Studio = the frontend graph editor that emits the same ASL JSON the API validates; the raw JSON editor is the fallback and must always work.
**Data model:** Postgres `pX_sfn_machines` (id, name, type, definition jsonb, execution_role, log_level, status, created_at) + `pX_sfn_executions` (id, machine_id, name, status, input jsonb, output jsonb, started_at, finished_at) + `pX_sfn_events` (id, execution_id, timestamp, type, state_name, input jsonb, output jsonb).
**Labs / lessons:** one lesson in the `serverless` track: build an order-processing workflow (validate → charge via Lambda → Choice on success → notify via SNS → Succeed/Fail), run it with good and bad input, watch the event history. Reuse the Lambda/SNS lessons.
**Lite feasibility:** Yes — the interpreter is pure Go + Postgres; Task states call services that are themselves lite (Lambda/SQS/SNS).

## 5. Lite feasibility and scope

- **Scope: large.** ASL-subset interpreter (Task/Choice/Pass/Wait/Parallel/Map/Succeed/Fail + Retry/Catch) + event history.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `stepfunctions`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
