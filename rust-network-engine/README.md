# CloudForge Rust Network Engine — Milestones 1 + 4

The independent Rust infrastructure service of CloudForge. It owns Linux
networking state for labs (namespaces, bridges, veth pairs, routing,
firewall, NAT, IPAM) behind a versioned gRPC API defined in
`proto/cloudforge/v1/network.proto`, which it shares with the Go control
plane. Go never shells out to this service and never constructs network
commands: all communication is typed gRPC with correlation IDs,
idempotency keys and structured errors.

## Fidelity statement (read this first)

**The default backend is `SIMULATED` — and the health response says
so.** Milestone 4 added a real rtnetlink/nftables backend, but it is
selected explicitly (`NETWORK_ENGINE_BACKEND=netlink`) and is
**unverified in this sandbox** (no `CAP_NET_ADMIN`, no KVM): it
compiles and issues real kernel requests, but no M4 run here has
exercised a successful kernel mutation.

What is real in M1+M4:

- The gRPC contract (`NetworkEngine` service, all M1+M4 methods).
- Request validation (required `lab_id` / `tenant_id` / `request_id` /
  `idempotency_key`, DNS-label name rules, CIDR parsing).
- CIDR overlap detection, IPAM allocation/release/utilisation.
- The in-memory topology store (networks, namespaces, bridges,
  allocations, subnets, veth pairs, routes, firewall policies, NAT
  rules, active faults, per lab), idempotency replay, and full lab
  cleanup.
- Pure rule evaluation: firewall (priority order, deny-before-allow
  ties, default-deny), routing (longest-prefix match, default route,
  conflict detection), fault validation/registry/effect model, and
  real in-memory telemetry counters.
- Structured errors with stable codes (`CIDR_OVERLAP`,
  `IPAM_EXHAUSTED`, `ADDRESS_ALREADY_ALLOCATED`, `ROUTE_CONFLICT`,
  `SUBNET_EXISTS`, `VETH_EXISTS`, `FIREWALL_POLICY_EXISTS`,
  `NAT_EXISTS`, `FAULT_EXISTS`, `BACKEND_UNAVAILABLE`, …).
- Graceful shutdown and structured logs.

What is explicitly **not** real yet (simulated backend):

- No netlink / nftables calls are made. `SimulatedBackend` (see
  `crates/engine-core/src/backend.rs`) implements the `RealNetlink`
  trait by recording intent and returning success. The sandbox has no
  `CAP_NET_ADMIN` and no KVM, and the engine must not pretend
  otherwise.
- Telemetry counters are real but count engine operations only; no
  traffic is generated or measured, so a fresh lab honestly reports
  zero counters.

Never claim a simulated network is a real Linux namespace/bridge. The
`Health` response carries `backend=SIMULATED` (or `backend=NETLINK`
when the real backend is selected) so any client can check.

## Milestone 4 — what landed

New gRPC operations (all additive to the M1 contract; no existing
field was renumbered, removed or retyped):

| RPC | What it does |
| --- | --- |
| `CreateSubnet` / `DeleteSubnet` | Carve a subnet out of a network's CIDR (containment + sibling-overlap checks). |
| `CreateVethPair` / `AttachInterface` | Create a veth pair; attach one peer to a bridge / namespace. |
| `AddRoute` / `DeleteRoute` | Static routes; same destination + different next hop is `ROUTE_CONFLICT`; overlapping prefixes coexist and resolve by longest-prefix match. |
| `CreateFirewallPolicy` / `ApplyFirewallPolicy` | Security-group policies (direction, protocol, port range, CIDR, action, priority). Evaluation: priority order, deny-before-allow ties, default-deny. |
| `CreateNAT` / `DeleteNAT` | SNAT / DNAT rules. |
| `InjectFault` | Latency / loss / partition / DNS / route-blackhole faults; magnitude ranges per kind, duration bounded to 1–3600s, faults expire. |
| `CollectTelemetry` | Real in-memory counters per lab (`operations_total{op}`, `subnets_created`, `routes_added`, `faults_injected`, `active_faults`, …); a fresh lab reports zeros. |

New pure crates (std + serde only, no syscalls — unit-testable
anywhere): `firewall`, `routing`, `fault-injection`, `telemetry`.

### Backend selection

```sh
# Default: simulated (no kernel mutation, no privileges needed).
NETWORK_ENGINE_ADDR=0.0.0.0:50051 RUST_LOG=info cargo run -p grpc-server --bin network-engine

# Real kernel backend (needs CAP_NET_ADMIN + the `nft` binary):
NETWORK_ENGINE_BACKEND=netlink NETWORK_ENGINE_ADDR=0.0.0.0:50051 cargo run -p grpc-server --bin network-engine
```

The real backend (`crates/engine-core/src/real.rs`,
`RealNetlinkBackend`, rtnetlink 0.14 + nftables 0.5) implements:
links/bridges/veth/routes/namespaces via rtnetlink, firewall/NAT via
nftables rulesets (`table ip cloudforge`, base chains `cf-input` /
`cf-output`, policy drop; `table ip cloudforge-nat` for NAT).
Per-operation fidelity notes live in that file's header. Two honest
limits: fault injection returns `BACKEND_UNAVAILABLE` (tc/netem is not
wired in M4 — faults are recorded and modelled, not enforced on real
packets), and applying a firewall policy / deleting a NAT rule replaces
the shared nftables table (single-policy semantics for now).

The M4 Docker image is unchanged from M1 (non-root, no capabilities):
the real backend is not the default, and a real-backend image still
needs a separate, reviewed change (install `nftables`, add
`CAP_NET_ADMIN`, never `--privileged` or host networking).

## Layout

```text
rust-network-engine/
  Cargo.toml                  # virtual workspace (resolver 2, edition 2021)
  Cargo.lock                  # pinned dependencies
  Dockerfile                  # unprivileged image (no CAP_NET_ADMIN)
  README.md                   # this file
  crates/
    ipam/                     # pure IPv4 CIDR/IPAM logic (std only, no syscalls)
    firewall/                 # M4: pure SG rule evaluation (std + serde)
    routing/                  # M4: pure route-table logic, LPM (std + serde)
    fault-injection/          # M4: fault validation, registry, effect model (std + serde)
    telemetry/                # M4: in-memory counters/series per lab (std + serde)
    engine-core/              # types, validation, topology store, idempotency, RealNetlink trait + real backend
    grpc-server/              # `network-engine` binary + library (tonic 0.12 / prost 0.13)
  tests/
    README.md                 # where the integration tests live
```

The protobuf contracts are **not** copied here: `crates/grpc-server/build.rs`
compiles `../../proto/cloudforge/v1/{common,network}.proto` from the repo
root, so Go and Rust can never drift apart silently.

## Build

Requires Rust stable (tested with 1.100.0) and `protoc`:

```sh
# Debian/Ubuntu:
sudo apt-get install -y protobuf-compiler
# or rely on the build.rs fallback, which uses a vendored protoc
# (protoc-bin-vendored) when no system protoc is on PATH.

cargo build --workspace
```

## Test

```sh
cargo test --workspace
```

Coverage:

- `ipam`: CIDR parsing/overlap, exhaustion (`/30`), release and
  re-allocation, duplicate-hint rejection, `/31` and `/32` edge cases,
  utilisation accounting.
- `engine-core`: name validation, topology store (overlap within a lab,
  same CIDR allowed across isolated labs, duplicate ids, namespace /
  bridge lifecycle, cleanup counts), idempotency store (first outcome
  wins, per-lab purge); M4 store tests (subnet containment/overlap,
  veth attach, route conflict + LPM lookup, firewall policy + NAT,
  fault inject + cleanup counting all M4 resources); real-backend
  tests (nftables ruleset serialises to valid `nft` JSON, fault
  injection is `BACKEND_UNAVAILABLE` on the real backend, simulated
  backend methods succeed).
- `engine-core/tests/integration.rs`: store/IPAM exhaustion agreement,
  cleanup purging idempotency keys.
- `firewall`: default-deny, allow/deny precedence (lower priority
  wins, deny-before-allow ties), wildcard protocol/port/CIDR, port
  ranges, ICMP ignoring ports, validation, serde round-trip.
- `routing`: longest-prefix match, default route, overlapping
  prefixes coexisting, same-destination conflict, idempotent re-add,
  remove, validation, serde round-trip.
- `fault-injection`: per-kind magnitude ranges (incl. binary 0/1 for
  partition/blackhole), duration bounds (1–3600s), registry
  expiry/pruning, traffic-effect model (latency adds, loss compounds,
  partition makes a target unreachable), serde round-trip.
- `telemetry`: counters start at zero, label partitioning, snapshot
  (zero counters for a fresh lab), bounded series ring, per-lab purge.
- `grpc-server/tests/grpc_smoke.rs`: real server on an ephemeral port +
  real tonic client — health, missing-meta rejection, create, idempotent
  replay (same operation id), structured `CIDR_OVERLAP` failure,
  allocate with hint, topology inspection, validation, release, cleanup
  and post-cleanup `NotFound`.
- `grpc-server/tests/grpc_m4.rs`: subnet create/overlap (+ replay,
  containment, delete), veth create+attach (incl. bad peer / missing
  bridge), route add + `ROUTE_CONFLICT`, firewall policy create/apply
  (+ bad rule, missing policy), NAT create/delete (+ duplicate),
  fault inject + telemetry (fresh-lab zeros, ghost-lab `NotFound`,
  invalid magnitude/duration, real counters), cleanup removing all
  M4 state (topology `NotFound` + telemetry back to zero afterwards).

## Lint / format

```sh
cargo fmt --all -- --check
cargo clippy --workspace --all-targets -- -D warnings
```

## Run

```sh
# Default listen address 0.0.0.0:50051; override with NETWORK_ENGINE_ADDR.
NETWORK_ENGINE_ADDR=0.0.0.0:50051 RUST_LOG=info cargo run -p grpc-server --bin network-engine
```

Graceful shutdown on `SIGTERM` / `SIGINT` / `Ctrl-C`. Structured logs
go to stdout.

Health is gRPC-only (no HTTP endpoint):

```text
service NetworkEngine / Health(HealthRequest) -> HealthResponse
```

`HealthResponse.detail` reports `backend=SIMULATED` by default, or
`backend=NETLINK` when the real backend is selected.

## API notes

Every mutating RPC requires `RequestMeta` (`lab_id`, `tenant_id`,
`request_id`, `idempotency_key` — all non-empty). Missing or empty
fields return `InvalidArgument`. Replaying the same
`(lab_id, idempotency_key)` returns the stored `OperationResult`
(including the original `operation_id`) without re-executing.
`CleanupLab` is the exception: it purges the lab's idempotency keys,
so it is safe to retry but is not itself replayed from the store.

Error mapping (`CoreError` → tonic `Status`, message prefixed with the
stable code):

| Condition | gRPC code | `error_code` |
| --- | --- | --- |
| Missing/invalid field, bad CIDR/name | `InvalidArgument` | `INVALID_ARGUMENT` / `INVALID_CIDR` |
| Unknown network/namespace/address | `NotFound` | `NOT_FOUND` / `ADDRESS_NOT_ALLOCATED` |
| Duplicate network/namespace/bridge/address | `AlreadyExists` | `NETWORK_EXISTS` / `NAMESPACE_EXISTS` / `BRIDGE_EXISTS` / `ADDRESS_ALREADY_ALLOCATED` |
| Duplicate subnet / veth / policy / NAT / fault | `AlreadyExists` | `SUBNET_EXISTS` / `VETH_EXISTS` / `FIREWALL_POLICY_EXISTS` / `NAT_EXISTS` / `FAULT_EXISTS` |
| CIDR overlap | `FailedPrecondition` | `CIDR_OVERLAP` |
| Route conflict (same destination, different next hop) | `FailedPrecondition` | `ROUTE_CONFLICT` |
| IPAM exhausted | `ResourceExhausted` | `IPAM_EXHAUSTED` |
| Backend failure | `Internal` | `BACKEND_ERROR` |
| Operation unavailable on the selected backend (e.g. fault injection on the real backend) | `Unavailable` | `BACKEND_UNAVAILABLE` |

Overlap is checked **within a lab only**: labs are isolated
namespaces, so two labs may both legitimately use `10.0.0.0/16`.

`ValidateNetwork` is a read-only query: an overlapping CIDR returns
`valid=false` with a `problems` list rather than an RPC error; only a
malformed CIDR returns `InvalidArgument`.

## Capabilities needed for the real backend (Milestone 4)

When the real rtnetlink/nftables backend runs, the engine must run
with the minimum capabilities necessary, isolated from the API:

- `CAP_NET_ADMIN` — create/delete namespaces, bridges, veth pairs,
  routes, nftables rules.
- `CAP_SYS_ADMIN` — only if `setns(2)`/namespace manipulation requires
  it for the chosen implementation; prefer `CAP_NET_ADMIN` alone and
  re-evaluate at M4.
- No `CAP_SYS_ADMIN`-wide or `--privileged` container, no host network
  namespace, no Docker socket access.

The M4 image deliberately still runs unprivileged (see `Dockerfile`:
non-root user, no capabilities) because the simulated backend — the
default — needs none. A real-backend deployment additionally needs the
`nft` binary on `PATH` (the `nftables` package) and is a separate,
reviewed image change, not part of M4.

## What is not in M4 (by design)

`CreateSubnet`, `CreateVethPair`, `AddRoute`, `CreateFirewallPolicy`,
`CreateNAT`, `InjectFault`, `CollectTelemetry` and the other operations
named in the full contract are **implemented in M4** (see the table
above). What remains for later milestones: the Go gRPC client that
replaces the M2 stub (a separate step — the proto contract is additive
so the Go side regenerates without breaking changes), real traffic
generation/measurement, tc/netem fault enforcement on the real backend,
per-lab nftables table isolation, and multi-policy nftables handling.
