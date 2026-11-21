# CloudForge architecture overview (Milestone 1)

## Product in one paragraph

CloudForge is a local-first cloud learning platform: a Go control plane
orchestrates labs, users, courses and providers; an independent Rust
engine owns Linux networking; Docker and KVM/QEMU provide real compute;
AWS-compatible emulators provide API fidelity where real infrastructure
is impractical. Nothing requires an AWS account.

## Service boundaries

| Service | Language | Owns | Does NOT own |
| --- | --- | --- | --- |
| Frontend | React 19 / TypeScript / Vite / Tailwind v4 | Console UI, topology rendering, terminal (M6), data fetching via TanStack Query | Any infrastructure state; it only renders what the API returns |
| Go API (control plane) | Go 1.25 | REST API, auth/roles, users, courses/lessons, lab lifecycle + scheduler, provider registry, Terraform orchestration, quotas, audit, Postgres + Redis | Privileged host operations; raw netlink/nftables calls |
| Go worker | Go | Durable background jobs (Asynq/Redis): provisioning, cleanup retries, expiry | Synchronous request handling |
| Go host agent | Go | Typed, allowlisted host operations: Docker, libvirt/QEMU, disks, cloud-init, inventory | Arbitrary shell execution; it accepts only typed operations from the control plane |
| Rust network engine | Rust (Tokio/Tonic) | Namespaces, bridges, veth, IPAM, routing, NAT, nftables security groups / NACLs, traffic simulation, fault injection, telemetry, cleanup | Users, courses, billing, anything outside its lab's network boundary |
| AWS provider adapters | Go | Capability registry, emulator (official AWS emulator, primary), Floci (optional), MinIO, DynamoDB Local, educational simulators | CloudForge's own learning APIs — AWS emulation is kept separate |

Principles: modular monolith for the Go control plane (no unnecessary
microservices); the Rust engine is independently deployable because it
has a different privilege boundary; no service invokes another by
constructing shell commands; privileged operations are never exposed
directly to frontend API endpoints.

```
React Frontend
  | REST / WebSocket
  v
Go API ── PostgreSQL ── Redis (jobs)
  |  gRPC + Protobuf
  +── Rust Network Engine ── netlink / nftables (CAP_NET_ADMIN only)
  +── Go Host Agent ── Docker socket (agent only) · libvirt/QEMU
  +── Provider adapters ── AWS emulator / Floci / MinIO / simulators
```

## gRPC contract (Go ↔ Rust)

Defined in `proto/cloudforge/v1/` — versioned (`cloudforge.v1`),
shared by both implementations; generated code lives under each
service's tree, never hand-copied.

- `common.proto` — `RequestMeta` (every infrastructure operation MUST
  carry `lab_id`, `tenant_id`, `request_id`, `idempotency_key`,
  `timeout_ms`), `OperationState` (PENDING → RUNNING → SUCCEEDED /
  FAILED / ROLLED_BACK / CANCELLED), `OperationResult`
  (`operation_id`, `state`, `message`, `error_code`, `duration_ms`),
  `HealthRequest`/`HealthResponse`
  (`service`, `version`, `ok`, `detail`, `uptime_seconds`).
- `network.proto` — `NetworkEngine` service. M1 implements health,
  `CreateNetwork`, `DeleteNetwork`, `CreateNamespace`,
  `DeleteNamespace`, `CreateBridge`, `AllocateAddress`,
  `ReleaseAddress`, `InspectTopology`, `ValidateNetwork`,
  `CleanupLab`. The remaining operations (`CreateSubnet`,
  `CreateVethPair`, `AddRoute`, `CreateFirewallPolicy`, `CreateNAT`,
  `InjectFault`, `CollectTelemetry`, …) are defined as the contract
  and return `UNIMPLEMENTED` with a structured error until Milestone 4
  implements them — never a silent fake success.

Contract rules:

- Health checks, timeouts, retries with bounded backoff, structured
  errors, request correlation IDs (`X-Correlation-Id` end to end),
  authentication between services, idempotency keys, explicit
  operation states, cancellation support, resource ownership
  validation (tenant/lab ownership is enforced server-side; resource
  labels are not an authorization mechanism).
- Unix domain sockets for local communication where appropriate.
- The REST surface mirrors the contract: `GET /api/v1/health` returns
  `{service, version, ok, uptime_seconds, network_engine_reachable}`
  — `network_engine_reachable` is the API's live gRPC health probe
  result, not a constant.

## REST API (Milestone 1, Go backend)

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/v1/health` | API health + network-engine reachability |
| GET | `/api/v1/courses` | Course catalogue (array or `{courses:[...]}`) |
| GET | `/api/v1/labs` | Caller-owned lab sessions; Bearer token required |
| POST | `/api/v1/auth/register` | `{name, email, password}` → `{token, user}` |
| POST | `/api/v1/auth/login` | `{email, password}` → `{token, user}` |
| GET | `/api/v1/network/topology` | M4; InspectTopology data when available |

Frontend types for these live in `frontend/src/types/index.ts` and the
client in `frontend/src/services/api.ts`.

## Fidelity labels

Every AWS-style operation in the provider capability registry reports:

| Label | Meaning |
| --- | --- |
| `REAL` | Real local infrastructure (KVM VM, Linux namespace, Docker container) |
| `API_EMULATED` | AWS-like API implementation (official AWS emulator, DynamoDB Local) |
| `SIMULATED` | Educational implementation (IAM policy simulator, KMS simulator) |
| `PARTIAL` | Some operations supported; the registry lists which |
| `UNSUPPORTED` | Not implemented — the API must say so, never fake success |

Example registry entry:

```json
{"service": "ec2", "operation": "RunInstances",
 "implementation": "aws-emulator", "fidelity": "emulated",
 "supported": true, "limitations": []}
```

## Lab lifecycle (state machine)

```
CREATED → PROVISIONING → READY → RUNNING → VALIDATING → COMPLETED
              |              |         |
              v              v         v
           FAILED        STOPPING → STOPPED
              |              |
              v              v
           EXPIRED → DESTROYING → DESTROYED
```

Transitions are validated; operations are idempotent; provisioning
supports rollback; failed cleanup is retried; expired labs are
destroyed automatically; orphan resources are detected and reconciled.
Every lab resource carries resource ID, lab ID, owner ID, provider,
resource type, provider resource ID, state, configuration, timestamps
and cleanup status.

## Security boundaries (M1 posture)

- Non-root API containers; no added Linux capabilities on
  api/frontend/postgres/redis. Only `network-engine` gets `NET_ADMIN`,
  and only on the KVM host (M4).
- The Docker socket is never exposed to the public API container; only
  the host agent touches Docker/libvirt, via typed allowlisted
  operations.
- Lab networks are isolated per lab; default lab egress is disabled,
  with an explicit controlled-egress option. The host management
  network is isolated from lab networks (separate compose networks:
  `frontend_net`, `backend_net`, `emulator_net`, `management_net`;
  the last three are `internal: true`).
- Short-lived terminal credentials (M6), secrets redaction, audit
  logs, rate limiting, SSRF protection, cloud-metadata endpoint
  protection. No secrets are committed (`.env.example` holds
  placeholders only).
- The Rust engine validates every requested network operation before
  changing the host; the Go host agent accepts only typed, authorized
  operations — no arbitrary shell execution anywhere.

## Deployment (M1)

`deployments/compose/docker-compose.yml` (pinned images, healthchecks,
named volumes) + `docker-compose.dev.yml` (dev overrides). The Rust
engine and Go host agent need host privileges and KVM; they do not run
meaningfully in a sandbox without `/dev/kvm` — verify with
`infrastructure/scripts/check-host.sh` first. Target host: Ubuntu
24.04 LTS, 8+ cores, 24–32 GB RAM recommended, 250 GB SSD.

## Milestones

1. **Foundation** (current) — repo, Go/Rust/React setup, protobuf,
   Postgres + Redis, health checks, gRPC connectivity, dashboard.
2. Go Control Plane — API, models, users, courses, lab state machine, worker, audit.
3. Host Agent — Docker, libvirt, templates, container/VM lifecycle.
4. Rust Network Engine — namespaces, bridges, veth, IPAM, routing, nftables, cleanup.
5. AWS Provider Integration — emulator, Floci, S3, DynamoDB, IAM simulator, capability registry.
6. Lab Workstation — terminal, topology editor, console pages, validation engine.
7. Terraform — isolated execution, plan/approve/apply/destroy.
8. Curriculum — courses, Break-and-Fix, assessments, XP, achievements.
9. Production Readiness — security review, quotas, monitoring, backup/restore, cleanup reconciliation, docs.
