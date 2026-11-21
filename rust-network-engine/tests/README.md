# Integration tests — where they live

Cargo only runs `tests/` directories that belong to a package, and the
workspace root is a virtual workspace (no package of its own), so the
integration tests live with the crates they exercise:

- `crates/ipam/src/lib.rs` — unit tests: CIDR parsing, overlap,
  exhaustion, release/re-allocate, duplicate hints, /31 and /32.
- `crates/engine-core/src/` — unit tests: name validation, topology
  store (overlap, duplicates, lifecycle), idempotency store.
- `crates/engine-core/tests/integration.rs` — cross-crate integration
  tests: store/IPAM exhaustion agreement, cleanup purging idempotency
  keys per lab.
- `crates/grpc-server/tests/grpc_smoke.rs` — end-to-end gRPC smoke
  test: starts a real server on an ephemeral localhost port and drives
  it with a real tonic client (health, create/replay, overlap,
  allocate, inspect, validate, release, cleanup).

Run everything with `cargo test --workspace` from this directory.
