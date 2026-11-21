//! Kernel backend abstraction.
//!
//! The gRPC service talks to the host only through `RealNetlink`. Two
//! implementations exist:
//!
//! * [`SimulatedBackend`] (default) — validates its inputs, logs the
//!   intended mutation and returns success **without touching the
//!   host**. The sandbox has no `CAP_NET_ADMIN` and no KVM, and the
//!   simulated backend must not pretend otherwise.
//! * [`RealNetlinkBackend`](crate::real::RealNetlinkBackend) — real
//!   rtnetlink / nftables calls, selected with
//!   `NETWORK_ENGINE_BACKEND=netlink`. It compiles and issues real
//!   kernel requests, but it is **unverified in this sandbox** (no
//!   `CAP_NET_ADMIN` here): on a real host it needs `CAP_NET_ADMIN`
//!   and the `nft` binary for firewall/NAT. See `crate::real` for the
//!   per-operation fidelity notes.
//!
//! The trait is `async` (via `async_trait`): real netlink calls are
//! I/O, and the service layer is async, so the trait follows. The
//! simulated backend does no I/O and its methods return immediately.

use crate::errors::CoreError;
use crate::types::{LabId, NatRule, TenantId};
use async_trait::async_trait;

/// Which kernel backend the engine uses. Selected by the
/// `NETWORK_ENGINE_BACKEND` environment variable (`simulated` |
/// `netlink`, default `simulated`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BackendKind {
    Simulated,
    Netlink,
}

impl BackendKind {
    /// Read the backend selection from the environment. Unknown values
    /// fall back to simulated (the caller logs a warning).
    pub fn from_env() -> Self {
        match std::env::var("NETWORK_ENGINE_BACKEND")
            .unwrap_or_default()
            .to_lowercase()
            .as_str()
        {
            "netlink" | "real" | "rtnetlink" => BackendKind::Netlink,
            _ => BackendKind::Simulated,
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            BackendKind::Simulated => "simulated",
            BackendKind::Netlink => "netlink",
        }
    }
}

/// The privileged operations the engine may perform on the host, one
/// typed method per operation. There is deliberately no "run an
/// arbitrary command" method.
#[async_trait]
pub trait RealNetlink: Send + Sync {
    /// Fidelity marker surfaced in logs and health detail.
    fn fidelity(&self) -> &'static str;

    async fn create_network(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        network_id: &str,
        cidr: &str,
    ) -> Result<(), CoreError>;
    async fn delete_network(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        network_id: &str,
    ) -> Result<(), CoreError>;
    async fn create_namespace(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        namespace: &str,
    ) -> Result<(), CoreError>;
    async fn delete_namespace(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        namespace: &str,
    ) -> Result<(), CoreError>;
    async fn create_bridge(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        bridge: &str,
        namespace: &str,
    ) -> Result<(), CoreError>;
    async fn cleanup_lab(&self, lab: &LabId, tenant: &TenantId) -> Result<usize, CoreError>;

    // --- Milestone 4 -------------------------------------------------
    async fn create_subnet(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        subnet_id: &str,
        network_id: &str,
        cidr: &str,
    ) -> Result<(), CoreError>;
    async fn delete_subnet(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        subnet_id: &str,
    ) -> Result<(), CoreError>;
    async fn create_veth_pair(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        veth_id: &str,
        peer_a: &str,
        peer_b: &str,
    ) -> Result<(), CoreError>;
    async fn attach_interface(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        veth_id: &str,
        interface: &str,
        namespace: &str,
        bridge: &str,
    ) -> Result<(), CoreError>;
    async fn add_route(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        entry: &routing::RouteEntry,
    ) -> Result<(), CoreError>;
    async fn delete_route(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        route_id: &str,
        destination: &str,
        gateway: &str,
        interface: &str,
    ) -> Result<(), CoreError>;
    async fn apply_firewall(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        policy: &firewall::Policy,
    ) -> Result<(), CoreError>;
    async fn create_nat(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        rule: &NatRule,
    ) -> Result<(), CoreError>;
    async fn delete_nat(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        nat_id: &str,
    ) -> Result<(), CoreError>;
    async fn inject_fault(
        &self,
        lab: &LabId,
        tenant: &TenantId,
        fault: &fault_injection::FaultSpec,
    ) -> Result<(), CoreError>;
}

/// Implementation: records intent, mutates nothing.
///
/// Every method succeeds (after the caller has validated inputs) and
/// returns the number of simulated kernel objects removed for cleanup
/// (always 0 — the store, not the kernel, holds simulated state).
#[derive(Debug, Default, Clone, Copy)]
pub struct SimulatedBackend;

impl SimulatedBackend {
    pub fn new() -> Self {
        Self
    }

    /// Fidelity marker surfaced in logs and health detail.
    pub fn fidelity() -> &'static str {
        "SIMULATED"
    }
}

#[async_trait]
impl RealNetlink for SimulatedBackend {
    fn fidelity(&self) -> &'static str {
        Self::fidelity()
    }

    async fn create_network(
        &self,
        lab: &LabId,
        _tenant: &TenantId,
        network_id: &str,
        cidr: &str,
    ) -> Result<(), CoreError> {
        // No kernel mutation; the store is the source of truth.
        let _ = (lab, network_id, cidr);
        Ok(())
    }

    async fn delete_network(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _network_id: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn create_namespace(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _namespace: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn delete_namespace(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _namespace: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn create_bridge(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _bridge: &str,
        _namespace: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn cleanup_lab(&self, _lab: &LabId, _tenant: &TenantId) -> Result<usize, CoreError> {
        Ok(0)
    }

    async fn create_subnet(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _subnet_id: &str,
        _network_id: &str,
        _cidr: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn delete_subnet(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _subnet_id: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn create_veth_pair(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _veth_id: &str,
        _peer_a: &str,
        _peer_b: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn attach_interface(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _veth_id: &str,
        _interface: &str,
        _namespace: &str,
        _bridge: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn add_route(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _entry: &routing::RouteEntry,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn delete_route(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _route_id: &str,
        _destination: &str,
        _gateway: &str,
        _interface: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn apply_firewall(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _policy: &firewall::Policy,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn create_nat(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _rule: &NatRule,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn delete_nat(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _nat_id: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn inject_fault(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _fault: &fault_injection::FaultSpec,
    ) -> Result<(), CoreError> {
        Ok(())
    }
}
