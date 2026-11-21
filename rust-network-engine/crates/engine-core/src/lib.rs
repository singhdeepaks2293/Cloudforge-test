//! Shared core types for the CloudForge Rust network engine.
//!
//! This crate owns everything that is independent of the transport
//! (gRPC) and of the kernel backend:
//! * validated identifiers (`LabId`, `TenantId`, network names),
//! * the operation result / error model,
//! * the in-memory topology store (networks, namespaces, bridges and
//!   IPAM allocations, per lab),
//! * the idempotency store,
//! * the `RealNetlink` backend trait and its M1 `SimulatedBackend`.
//!
//! M1 fidelity note: the topology store, validation, IPAM and cleanup
//! bookkeeping in this crate are **real**; kernel mutation (netlink /
//! nftables) is **simulated** by default — `SimulatedBackend` records
//! the intent and returns success without touching the host. The real
//! rtnetlink backend landed in Milestone 4 (`crate::real`) and is
//! selected with `NETWORK_ENGINE_BACKEND=netlink`; it is unverified in
//! the sandbox (no `CAP_NET_ADMIN`).

pub mod backend;
pub mod errors;
pub mod idempotency;
pub mod real;
pub mod store;
pub mod types;
pub mod validation;

pub use backend::{BackendKind, RealNetlink, SimulatedBackend};
pub use errors::CoreError;
pub use idempotency::{IdempotencyStore, StoredOutcome};
pub use real::RealNetlinkBackend;
pub use store::{
    BridgeRecord, LabState, NatRecord, NetworkRecord, SubnetRecord, TopologyNode, TopologyStore,
    VethRecord,
};
pub use types::{LabId, NatKind, NatRule, OperationResult, OperationState, RequestMeta, TenantId};
pub use validation::validate_name;
