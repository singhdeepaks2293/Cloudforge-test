# MQ — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `mq` |
| Name | MQ |
| Category | Application Integration |
| Registry route | `/services/mq` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | broker |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

MQ is the managed message-broker service for Apache ActiveMQ and RabbitMQ: a real broker (not a queue API) with topics, queues, durable subscriptions, JMS/AMQP/STOMP/MQTT/OpenWire protocols, and a web console per broker. A learner meets it when an application already speaks JMS or AMQP and cannot be rewritten for SQS — the 'lift your existing messaging' service — and as the contrast to SQS/SNS, which are API-only.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Brokers, Configurations.
**Home — Brokers list:** columns Broker name, Engine type (ActiveMQ / RabbitMQ), Deployment mode (Single instance / Active/standby multi-AZ / Cluster multi-AZ), State (Creation in progress / Running / Reboot in progress / Critical action required / Deletion in progress), Created. Filters: engine, state.
**Broker detail tabs:** Details (broker ID, endpoints per protocol — OpenWire/AMQP/STOMP/MQTT/WebSocket for ActiveMQ, AMQP for RabbitMQ — public accessibility, VPC, security groups, storage, maintenance window, engine version), Users (table: username, groups, console access; Add user), Configurations (current configuration + revision history), Logs (audit + general logs), Metrics (CPU, memory, connections, message counts), Maintenance (pending actions).
**Configurations list:** columns Name, Engine type, Description, Created. Detail: XML/configuration editor + revisions.
**Wizard — Create broker:** Step 1 Choose broker engine (ActiveMQ / RabbitMQ) + deployment mode. Step 2 Configure settings (broker name, instance type, storage type/size, deployment mode options). Step 3 Configure access (public access, VPC/subnets/security groups, encryption). Step 4 Users (username/password, groups, console access). Step 5 Review and create. (RabbitMQ: cluster deployment mode, broker version, instance type.)
**Actions:** Create broker, Reboot broker, Delete broker, Edit configuration, Create user, Delete user, Update broker (engine version).

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Managed message brokers._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** brokers CRUD `POST/GET/DELETE /api/v1/aws/mq/brokers`, `POST /api/v1/aws/mq/brokers/{id}/reboot`, users CRUD `/api/v1/aws/mq/brokers/{id}/users`, configurations CRUD `/api/v1/aws/mq/configurations` + revisions, `GET /api/v1/aws/mq/brokers/{id}/metrics`.
**Runtime (honest scope — a real broker is heavy; do it in two stages):**
- **Stage 1 (lite-feasible, recommended first):** a protocol-compatible teaching broker is NOT attempted. Instead, implement the broker as a durable queue/topic store over Postgres with AMQP 0-9-1 framing only for RabbitMQ-style publish/consume via a small Go AMQP server on a local port, OR — simpler and more honest — expose the broker through the same HTTP API as SQS (queues/topics) and label the broker SIMULATED with the protocol endpoints shown as "not serving traffic". Stage 1 teaches broker concepts (exchanges, queues, bindings, users, configurations) with real durable state and real message delivery through the HTTP API. Do not fake protocol endpoints.
- **Stage 2 (full mode only):** run a real RabbitMQ or ActiveMQ container (Docker) as the broker backend, managed by the host agent; the API proxies management (users, queues, bindings) to the broker's management API and the console links to the real management UI. Gate behind the Docker capability check; in lite, Stage 2 is disabled and Stage 1 is the offering.
**Data model:** Postgres `pX_mq_brokers` (id, name, engine_type, deployment_mode, state, instance_type, storage_gb, public_access, vpc_id, endpoints jsonb, created_at) + `pX_mq_users` (id, broker_id, username, password_hash, groups text[], console_access) + `pX_mq_configurations` (id, name, engine_type, description, data, created_at) + `pX_mq_config_revisions` (id, configuration_id, revision, data, created_at) + `pX_mq_queues` (id, broker_id, name, durable, created_at) + `pX_mq_messages` (id, broker_id, queue, body, created_at).
**Labs / lessons:** one lesson in the `messaging` track: create a RabbitMQ broker, declare a queue, publish/consume messages, compare with SQS (same task, different protocol). Reuse the SQS lesson.
**Lite feasibility:** Stage 1 yes; Stage 2 (real broker container) no — full mode only.

## 5. Lite feasibility and scope

- **Scope: medium.** Broker/user/configuration model + durable queues (Stage 1); real RabbitMQ/ActiveMQ container is Stage 2, full-mode only.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `mq`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
