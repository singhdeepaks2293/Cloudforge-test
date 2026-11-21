//! In-memory topology store.
//!
//! The store is the source of truth for what a lab contains: networks
//! (with their IPAM pools), namespaces, bridges and allocated
//! addresses, plus the Milestone 4 resources — subnets, veth pairs,
//! routes, firewall policies, NAT rules and active faults — keyed by
//! lab. All access goes through one `Mutex`, which keeps the
//! check-then-act sequences (overlap check + insert, hint check +
//! allocate) atomic without a database.
//!
//! Ownership: every record belongs to exactly one lab; callers must
//! pass the lab id of the authenticated tenant, and the gRPC layer
//! rejects requests whose lab/tenant do not match the session. (The
//! store itself is per-lab keyed, so a lookup can never leak another
//! lab's resources.)

use crate::errors::CoreError;
use crate::types::{LabId, NatKind, NatRule};
use crate::validation::validate_name;
use fault_injection::{ActiveFault, FaultRegistry, FaultSpec};
use firewall::Policy as FirewallPolicy;
use ipam::{Cidr, IpamPool};
use routing::{RouteEntry, RouteTable};
use std::collections::{HashMap, HashSet};
use std::net::Ipv4Addr;
use std::sync::Mutex;

/// A network (VPC-equivalent) inside a lab.
#[derive(Debug)]
pub struct NetworkRecord {
    pub network_id: String,
    pub name: String,
    pub cidr: Cidr,
    pub pool: IpamPool,
}

/// A Linux bridge inside a lab.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BridgeRecord {
    pub bridge: String,
    /// "" means the host namespace.
    pub namespace: String,
}

/// A subnet carved out of a parent network's CIDR.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SubnetRecord {
    pub subnet_id: String,
    pub network_id: String,
    pub name: String,
    pub cidr: Cidr,
}

/// A veth pair inside a lab.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VethRecord {
    pub veth_id: String,
    pub peer_a: String,
    pub peer_b: String,
    /// "" means the host namespace.
    pub namespace_a: String,
    /// "" means the host namespace.
    pub namespace_b: String,
    /// Bridge one peer is attached to; "" = none.
    pub bridge: String,
    pub attached: bool,
}

/// A NAT rule inside a lab.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NatRecord {
    pub nat_id: String,
    pub kind: NatKind,
    pub source_cidr: Cidr,
    pub translated_address: Ipv4Addr,
    pub interface: String,
}

impl NatRecord {
    pub fn to_rule(&self) -> NatRule {
        NatRule {
            nat_id: self.nat_id.clone(),
            kind: self.kind,
            source_cidr: self.source_cidr.to_string(),
            translated_address: self.translated_address.to_string(),
            interface: self.interface.clone(),
        }
    }
}

/// One node of the topology graph returned by `InspectTopology`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TopologyNode {
    pub id: String,
    /// `network` | `namespace` | `bridge` | `address`.
    pub kind: String,
    pub name: String,
    pub labels: Vec<(String, String)>,
}

/// All state belonging to a single lab.
#[derive(Debug, Default)]
pub struct LabState {
    pub networks: HashMap<String, NetworkRecord>,
    pub namespaces: HashSet<String>,
    pub bridges: HashMap<String, BridgeRecord>,
    pub subnets: HashMap<String, SubnetRecord>,
    pub veth_pairs: HashMap<String, VethRecord>,
    pub routes: RouteTable,
    pub firewall_policies: HashMap<String, FirewallPolicy>,
    pub nat_rules: HashMap<String, NatRecord>,
    pub faults: FaultRegistry,
}

impl LabState {
    /// Number of tracked resources (networks + namespaces + bridges +
    /// allocated addresses + subnets + veth pairs + routes + firewall
    /// policies + NAT rules + active faults). Used by cleanup reporting.
    pub fn resource_count(&self) -> usize {
        self.networks.len()
            + self.namespaces.len()
            + self.bridges.len()
            + self
                .networks
                .values()
                .map(|n| n.pool.allocated_count())
                .sum::<usize>()
            + self.subnets.len()
            + self.veth_pairs.len()
            + self.routes.len()
            + self.firewall_policies.len()
            + self.nat_rules.len()
            + self.faults.active_count()
    }
}

#[derive(Debug, Default)]
pub struct TopologyStore {
    inner: Mutex<HashMap<LabId, LabState>>,
}

impl TopologyStore {
    pub fn new() -> Self {
        Self::default()
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, HashMap<LabId, LabState>> {
        self.inner.lock().expect("topology store lock poisoned")
    }

    /// Create a network. Fails on an invalid name/CIDR, a duplicate
    /// network id, or a CIDR overlapping any other network **in the
    /// same lab**. Overlap across labs is allowed: labs are isolated
    /// namespaces, so two labs may legitimately both use `10.0.0.0/16`.
    pub fn create_network(
        &self,
        lab: &LabId,
        network_id: &str,
        name: &str,
        cidr_s: &str,
    ) -> Result<(), CoreError> {
        validate_name(name)?;
        if network_id.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "network_id must not be empty".to_string(),
            ));
        }
        let cidr = Cidr::parse(cidr_s)?;
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if state.networks.contains_key(network_id) {
            return Err(CoreError::Conflict(format!(
                "NETWORK_EXISTS: network {network_id} already exists in lab {lab}"
            )));
        }
        for other in state.networks.values() {
            if cidr.overlaps(other.cidr) {
                return Err(CoreError::Conflict(format!(
                    "CIDR_OVERLAP: {cidr} overlaps {} ({})",
                    other.cidr, other.network_id
                )));
            }
        }
        state.networks.insert(
            network_id.to_string(),
            NetworkRecord {
                network_id: network_id.to_string(),
                name: name.to_string(),
                cidr,
                pool: IpamPool::new(cidr),
            },
        );
        Ok(())
    }

    /// Delete a network and release all of its allocations. Subnets
    /// carved from the network and firewall policies bound to it go
    /// with it; veth pairs, routes and NAT rules are lab-scoped and
    /// survive (they are removed by lab cleanup).
    pub fn delete_network(&self, lab: &LabId, network_id: &str) -> Result<(), CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state.networks.remove(network_id).ok_or_else(|| {
            CoreError::NotFound(format!("network {network_id} not found in lab {lab}"))
        })?;
        state.subnets.retain(|_, s| s.network_id != network_id);
        state
            .firewall_policies
            .retain(|_, p| p.network_id != network_id);
        Ok(())
    }

    pub fn network_cidr(&self, lab: &LabId, network_id: &str) -> Result<Cidr, CoreError> {
        let inner = self.lock();
        inner
            .get(lab)
            .and_then(|s| s.networks.get(network_id))
            .map(|n| n.cidr)
            .ok_or_else(|| {
                CoreError::NotFound(format!("network {network_id} not found in lab {lab}"))
            })
    }

    pub fn create_namespace(&self, lab: &LabId, namespace: &str) -> Result<(), CoreError> {
        validate_name(namespace)?;
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if !state.namespaces.insert(namespace.to_string()) {
            return Err(CoreError::Conflict(format!(
                "NAMESPACE_EXISTS: namespace {namespace} already exists in lab {lab}"
            )));
        }
        Ok(())
    }

    pub fn delete_namespace(&self, lab: &LabId, namespace: &str) -> Result<(), CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        if !state.namespaces.remove(namespace) {
            return Err(CoreError::NotFound(format!(
                "namespace {namespace} not found in lab {lab}"
            )));
        }
        // Bridges attached to the deleted namespace go with it.
        state.bridges.retain(|_, b| b.namespace != namespace);
        // Veth peers living in the deleted namespace lose that
        // attachment (and the bridge attachment if the bridge is gone).
        for veth in state.veth_pairs.values_mut() {
            if veth.namespace_a == namespace {
                veth.namespace_a.clear();
            }
            if veth.namespace_b == namespace {
                veth.namespace_b.clear();
            }
            if !veth.bridge.is_empty() && !state.bridges.contains_key(&veth.bridge) {
                veth.bridge.clear();
                veth.attached = false;
            }
        }
        Ok(())
    }

    pub fn create_bridge(
        &self,
        lab: &LabId,
        bridge: &str,
        namespace: &str,
    ) -> Result<(), CoreError> {
        validate_name(bridge)?;
        if !namespace.is_empty() {
            validate_name(namespace)?;
        }
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if state.bridges.contains_key(bridge) {
            return Err(CoreError::Conflict(format!(
                "BRIDGE_EXISTS: bridge {bridge} already exists in lab {lab}"
            )));
        }
        if !namespace.is_empty() && !state.namespaces.contains(namespace) {
            return Err(CoreError::NotFound(format!(
                "namespace {namespace} not found in lab {lab}"
            )));
        }
        state.bridges.insert(
            bridge.to_string(),
            BridgeRecord {
                bridge: bridge.to_string(),
                namespace: namespace.to_string(),
            },
        );
        Ok(())
    }

    /// Allocate an address from a network's pool; `hint` may be empty.
    pub fn allocate_address(
        &self,
        lab: &LabId,
        network_id: &str,
        hint: &str,
    ) -> Result<Ipv4Addr, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        let net = state.networks.get_mut(network_id).ok_or_else(|| {
            CoreError::NotFound(format!("network {network_id} not found in lab {lab}"))
        })?;
        if hint.trim().is_empty() {
            net.pool.allocate().map_err(CoreError::from)
        } else {
            net.pool.allocate_hint(hint).map_err(CoreError::from)
        }
    }

    pub fn release_address(
        &self,
        lab: &LabId,
        network_id: &str,
        address: &str,
    ) -> Result<(), CoreError> {
        let addr = address
            .parse::<Ipv4Addr>()
            .map_err(|_| CoreError::InvalidArgument(format!("invalid IPv4 address: {address}")))?;
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        let net = state.networks.get_mut(network_id).ok_or_else(|| {
            CoreError::NotFound(format!("network {network_id} not found in lab {lab}"))
        })?;
        net.pool.release(addr).map_err(CoreError::from)
    }

    // --- Milestone 4: subnets -------------------------------------------

    /// Create a subnet inside a parent network. The subnet CIDR must
    /// be contained in the parent network's CIDR and must not overlap
    /// a sibling subnet of the same network.
    pub fn create_subnet(
        &self,
        lab: &LabId,
        subnet_id: &str,
        network_id: &str,
        name: &str,
        cidr_s: &str,
    ) -> Result<(), CoreError> {
        validate_name(name)?;
        if subnet_id.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "subnet_id must not be empty".to_string(),
            ));
        }
        let cidr = Cidr::parse(cidr_s)?;
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        let parent = state.networks.get(network_id).ok_or_else(|| {
            CoreError::NotFound(format!("network {network_id} not found in lab {lab}"))
        })?;
        let parent_cidr = parent.cidr;
        if state.subnets.contains_key(subnet_id) {
            return Err(CoreError::Conflict(format!(
                "SUBNET_EXISTS: subnet {subnet_id} already exists in lab {lab}"
            )));
        }
        if !parent_cidr.contains_addr(cidr.network_address())
            || !parent_cidr.contains_addr(cidr.broadcast_address())
        {
            return Err(CoreError::InvalidArgument(format!(
                "subnet {cidr} is not contained in parent network {parent_cidr}"
            )));
        }
        for other in state.subnets.values() {
            if other.network_id == network_id && cidr.overlaps(other.cidr) {
                return Err(CoreError::Conflict(format!(
                    "CIDR_OVERLAP: {cidr} overlaps subnet {} ({})",
                    other.cidr, other.subnet_id
                )));
            }
        }
        state.subnets.insert(
            subnet_id.to_string(),
            SubnetRecord {
                subnet_id: subnet_id.to_string(),
                network_id: network_id.to_string(),
                name: name.to_string(),
                cidr,
            },
        );
        Ok(())
    }

    /// Delete a subnet, returning the removed record (for backend
    /// rollback).
    pub fn delete_subnet(&self, lab: &LabId, subnet_id: &str) -> Result<SubnetRecord, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state.subnets.remove(subnet_id).ok_or_else(|| {
            CoreError::NotFound(format!("subnet {subnet_id} not found in lab {lab}"))
        })
    }

    /// Re-insert a previously deleted subnet (backend rollback).
    pub fn restore_subnet(&self, lab: &LabId, record: SubnetRecord) {
        let mut inner = self.lock();
        inner
            .entry(lab.clone())
            .or_default()
            .subnets
            .insert(record.subnet_id.clone(), record);
    }

    // --- Milestone 4: veth pairs ----------------------------------------

    pub fn create_veth_pair(
        &self,
        lab: &LabId,
        veth_id: &str,
        peer_a: &str,
        peer_b: &str,
        namespace_a: &str,
        namespace_b: &str,
    ) -> Result<(), CoreError> {
        validate_name(peer_a)?;
        validate_name(peer_b)?;
        if veth_id.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "veth_id must not be empty".to_string(),
            ));
        }
        if peer_a == peer_b {
            return Err(CoreError::InvalidArgument(
                "veth peers must have different names".to_string(),
            ));
        }
        if !namespace_a.is_empty() {
            validate_name(namespace_a)?;
        }
        if !namespace_b.is_empty() {
            validate_name(namespace_b)?;
        }
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if state.veth_pairs.contains_key(veth_id) {
            return Err(CoreError::Conflict(format!(
                "VETH_EXISTS: veth pair {veth_id} already exists in lab {lab}"
            )));
        }
        for other in state.veth_pairs.values() {
            if other.peer_a == peer_a
                || other.peer_b == peer_a
                || other.peer_a == peer_b
                || other.peer_b == peer_b
            {
                return Err(CoreError::Conflict(format!(
                    "VETH_EXISTS: interface {peer_a} or {peer_b} is already used by veth pair {}",
                    other.veth_id
                )));
            }
        }
        for ns in [namespace_a, namespace_b] {
            if !ns.is_empty() && !state.namespaces.contains(ns) {
                return Err(CoreError::NotFound(format!(
                    "namespace {ns} not found in lab {lab}"
                )));
            }
        }
        state.veth_pairs.insert(
            veth_id.to_string(),
            VethRecord {
                veth_id: veth_id.to_string(),
                peer_a: peer_a.to_string(),
                peer_b: peer_b.to_string(),
                namespace_a: namespace_a.to_string(),
                namespace_b: namespace_b.to_string(),
                bridge: String::new(),
                attached: false,
            },
        );
        Ok(())
    }

    pub fn delete_veth_pair(&self, lab: &LabId, veth_id: &str) -> Result<VethRecord, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state.veth_pairs.remove(veth_id).ok_or_else(|| {
            CoreError::NotFound(format!("veth pair {veth_id} not found in lab {lab}"))
        })
    }

    /// Attach one peer of a veth pair to a bridge and/or namespace.
    /// Returns the previous record (for backend rollback).
    pub fn attach_interface(
        &self,
        lab: &LabId,
        veth_id: &str,
        interface: &str,
        namespace: &str,
        bridge: &str,
    ) -> Result<VethRecord, CoreError> {
        if !namespace.is_empty() {
            validate_name(namespace)?;
        }
        if !bridge.is_empty() {
            validate_name(bridge)?;
        }
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        if !namespace.is_empty() && !state.namespaces.contains(namespace) {
            return Err(CoreError::NotFound(format!(
                "namespace {namespace} not found in lab {lab}"
            )));
        }
        if !bridge.is_empty() && !state.bridges.contains_key(bridge) {
            return Err(CoreError::NotFound(format!(
                "bridge {bridge} not found in lab {lab}"
            )));
        }
        let veth = state.veth_pairs.get_mut(veth_id).ok_or_else(|| {
            CoreError::NotFound(format!("veth pair {veth_id} not found in lab {lab}"))
        })?;
        let previous = veth.clone();
        if interface == veth.peer_a {
            veth.namespace_a = namespace.to_string();
        } else if interface == veth.peer_b {
            veth.namespace_b = namespace.to_string();
        } else {
            return Err(CoreError::InvalidArgument(format!(
                "interface {interface} is not a peer of veth pair {veth_id}"
            )));
        }
        veth.bridge = bridge.to_string();
        veth.attached = true;
        Ok(previous)
    }

    /// Restore a veth record (backend rollback).
    pub fn restore_veth_pair(&self, lab: &LabId, record: VethRecord) {
        let mut inner = self.lock();
        inner
            .entry(lab.clone())
            .or_default()
            .veth_pairs
            .insert(record.veth_id.clone(), record);
    }

    // --- Milestone 4: routes --------------------------------------------

    /// Add a static route. Conflict detection (same destination, a
    /// different next hop) comes from the routing crate; overlapping
    /// non-identical prefixes are allowed and resolved by LPM.
    pub fn add_route(&self, lab: &LabId, entry: RouteEntry) -> Result<(), CoreError> {
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        state.routes.add(entry).map_err(|e| match e {
            routing::RouteError::Invalid(m) => CoreError::InvalidArgument(m),
            routing::RouteError::Conflict(m) => CoreError::Conflict(m),
            routing::RouteError::NotFound(m) => CoreError::NotFound(m),
        })
    }

    pub fn delete_route(&self, lab: &LabId, route_id: &str) -> Result<RouteEntry, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state.routes.remove(route_id).map_err(|e| match e {
            routing::RouteError::Invalid(m) => CoreError::InvalidArgument(m),
            routing::RouteError::Conflict(m) => CoreError::Conflict(m),
            routing::RouteError::NotFound(m) => CoreError::NotFound(m),
        })
    }

    pub fn get_route(&self, lab: &LabId, route_id: &str) -> Result<RouteEntry, CoreError> {
        let inner = self.lock();
        inner
            .get(lab)
            .and_then(|s| s.routes.get(route_id))
            .cloned()
            .ok_or_else(|| CoreError::NotFound(format!("route {route_id} not found in lab {lab}")))
    }

    /// Longest-prefix-match lookup for an address in a lab's table.
    pub fn lookup_route(&self, lab: &LabId, addr: Ipv4Addr) -> Option<RouteEntry> {
        let inner = self.lock();
        inner.get(lab).and_then(|s| s.routes.lookup(addr)).cloned()
    }

    // --- Milestone 4: firewall ------------------------------------------

    pub fn create_firewall_policy(
        &self,
        lab: &LabId,
        policy: FirewallPolicy,
    ) -> Result<(), CoreError> {
        validate_name(&policy.name).map_err(|_| {
            CoreError::InvalidArgument(format!(
                "firewall policy name {:?} is not a valid DNS-label name",
                policy.name
            ))
        })?;
        policy.validate().map_err(CoreError::InvalidArgument)?;
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if !state.networks.contains_key(&policy.network_id) {
            return Err(CoreError::NotFound(format!(
                "network {} not found in lab {lab}",
                policy.network_id
            )));
        }
        if state.firewall_policies.contains_key(&policy.policy_id) {
            return Err(CoreError::Conflict(format!(
                "FIREWALL_POLICY_EXISTS: policy {} already exists in lab {lab}",
                policy.policy_id
            )));
        }
        state
            .firewall_policies
            .insert(policy.policy_id.clone(), policy);
        Ok(())
    }

    /// Mark a policy applied and return a clone of it (for the backend).
    pub fn mark_firewall_applied(
        &self,
        lab: &LabId,
        policy_id: &str,
    ) -> Result<FirewallPolicy, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        let policy = state.firewall_policies.get_mut(policy_id).ok_or_else(|| {
            CoreError::NotFound(format!(
                "firewall policy {policy_id} not found in lab {lab}"
            ))
        })?;
        policy.applied = true;
        Ok(policy.clone())
    }

    pub fn get_firewall_policy(
        &self,
        lab: &LabId,
        policy_id: &str,
    ) -> Result<FirewallPolicy, CoreError> {
        let inner = self.lock();
        inner
            .get(lab)
            .and_then(|s| s.firewall_policies.get(policy_id))
            .cloned()
            .ok_or_else(|| {
                CoreError::NotFound(format!(
                    "firewall policy {policy_id} not found in lab {lab}"
                ))
            })
    }

    // --- Milestone 4: NAT ------------------------------------------------

    pub fn create_nat(&self, lab: &LabId, rule: NatRule) -> Result<(), CoreError> {
        if rule.nat_id.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "nat_id must not be empty".to_string(),
            ));
        }
        let source_cidr = Cidr::parse(&rule.source_cidr).map_err(CoreError::from)?;
        let translated = rule.translated_address.parse::<Ipv4Addr>().map_err(|_| {
            CoreError::InvalidArgument(format!(
                "invalid translated IPv4: {}",
                rule.translated_address
            ))
        })?;
        let mut inner = self.lock();
        let state = inner.entry(lab.clone()).or_default();
        if state.nat_rules.contains_key(&rule.nat_id) {
            return Err(CoreError::Conflict(format!(
                "NAT_EXISTS: NAT rule {} already exists in lab {lab}",
                rule.nat_id
            )));
        }
        state.nat_rules.insert(
            rule.nat_id.clone(),
            NatRecord {
                nat_id: rule.nat_id.clone(),
                kind: rule.kind,
                source_cidr,
                translated_address: translated,
                interface: rule.interface.clone(),
            },
        );
        Ok(())
    }

    pub fn delete_nat(&self, lab: &LabId, nat_id: &str) -> Result<NatRecord, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state
            .nat_rules
            .remove(nat_id)
            .ok_or_else(|| CoreError::NotFound(format!("NAT rule {nat_id} not found in lab {lab}")))
    }

    /// Re-insert a previously deleted NAT record (backend rollback).
    pub fn restore_nat(&self, lab: &LabId, record: NatRecord) {
        let mut inner = self.lock();
        inner
            .entry(lab.clone())
            .or_default()
            .nat_rules
            .insert(record.nat_id.clone(), record);
    }

    // --- Milestone 4: faults ---------------------------------------------

    /// Inject a fault. Validation (kind, magnitude, duration) comes
    /// from the fault-injection crate; duplicates are conflicts.
    pub fn inject_fault(&self, lab: &LabId, spec: FaultSpec) -> Result<ActiveFault, CoreError> {
        let mut inner = self.lock();
        let state = inner
            .get_mut(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        state.faults.inject(spec).map_err(|m| {
            if m.contains("already exists") {
                CoreError::Conflict(format!("FAULT_EXISTS: {m}"))
            } else {
                CoreError::InvalidArgument(format!("FAULT_INVALID: {m}"))
            }
        })
    }

    /// Remove a fault (backend rollback / manual heal).
    pub fn remove_fault(&self, lab: &LabId, fault_id: &str) -> Option<ActiveFault> {
        let mut inner = self.lock();
        inner.get_mut(lab).and_then(|s| s.faults.remove(fault_id))
    }

    /// Currently active (unexpired) faults for a lab.
    pub fn active_faults(&self, lab: &LabId) -> Vec<ActiveFault> {
        let mut inner = self.lock();
        inner
            .get_mut(lab)
            .map(|s| s.faults.active())
            .unwrap_or_default()
    }

    /// Combined traffic effect of the lab's active faults on `target`.
    pub fn fault_effect_for(&self, lab: &LabId, target: &str) -> fault_injection::TrafficEffect {
        let mut inner = self.lock();
        inner
            .get_mut(lab)
            .map(|s| s.faults.effect_for(target))
            .unwrap_or_default()
    }

    /// Topology nodes for a lab, optionally restricted to one network.
    pub fn inspect(&self, lab: &LabId, network_id: &str) -> Result<Vec<TopologyNode>, CoreError> {
        let inner = self.lock();
        let state = inner
            .get(lab)
            .ok_or_else(|| CoreError::NotFound(format!("lab {lab} has no resources")))?;
        if !network_id.is_empty() && !state.networks.contains_key(network_id) {
            return Err(CoreError::NotFound(format!(
                "network {network_id} not found in lab {lab}"
            )));
        }
        let mut nodes = Vec::new();
        for net in state.networks.values() {
            if !network_id.is_empty() && net.network_id != network_id {
                continue;
            }
            nodes.push(TopologyNode {
                id: net.network_id.clone(),
                kind: "network".to_string(),
                name: net.name.clone(),
                labels: vec![
                    ("cidr".to_string(), net.cidr.to_string()),
                    (
                        "allocated".to_string(),
                        net.pool.allocated_count().to_string(),
                    ),
                    ("usable".to_string(), net.pool.usable_total().to_string()),
                ],
            });
            for addr in net.pool.allocated_addresses() {
                nodes.push(TopologyNode {
                    id: format!("{}:{}", net.network_id, addr),
                    kind: "address".to_string(),
                    name: addr.to_string(),
                    labels: vec![("network_id".to_string(), net.network_id.clone())],
                });
            }
        }
        for subnet in state.subnets.values() {
            if !network_id.is_empty() && subnet.network_id != network_id {
                continue;
            }
            nodes.push(TopologyNode {
                id: subnet.subnet_id.clone(),
                kind: "subnet".to_string(),
                name: subnet.name.clone(),
                labels: vec![
                    ("network_id".to_string(), subnet.network_id.clone()),
                    ("cidr".to_string(), subnet.cidr.to_string()),
                ],
            });
        }
        if network_id.is_empty() {
            for ns in &state.namespaces {
                nodes.push(TopologyNode {
                    id: ns.clone(),
                    kind: "namespace".to_string(),
                    name: ns.clone(),
                    labels: vec![],
                });
            }
            for br in state.bridges.values() {
                nodes.push(TopologyNode {
                    id: br.bridge.clone(),
                    kind: "bridge".to_string(),
                    name: br.bridge.clone(),
                    labels: vec![("namespace".to_string(), br.namespace.clone())],
                });
            }
            for veth in state.veth_pairs.values() {
                nodes.push(TopologyNode {
                    id: veth.veth_id.clone(),
                    kind: "veth".to_string(),
                    name: format!("{}<->{}", veth.peer_a, veth.peer_b),
                    labels: vec![
                        ("peer_a".to_string(), veth.peer_a.clone()),
                        ("peer_b".to_string(), veth.peer_b.clone()),
                        ("bridge".to_string(), veth.bridge.clone()),
                        ("attached".to_string(), veth.attached.to_string()),
                    ],
                });
            }
            for route in state.routes.routes() {
                nodes.push(TopologyNode {
                    id: route.route_id.clone(),
                    kind: "route".to_string(),
                    name: route.destination.clone(),
                    labels: vec![
                        ("gateway".to_string(), route.gateway.clone()),
                        ("interface".to_string(), route.interface.clone()),
                        ("metric".to_string(), route.metric.to_string()),
                    ],
                });
            }
            for policy in state.firewall_policies.values() {
                nodes.push(TopologyNode {
                    id: policy.policy_id.clone(),
                    kind: "firewall".to_string(),
                    name: policy.name.clone(),
                    labels: vec![
                        ("network_id".to_string(), policy.network_id.clone()),
                        ("rules".to_string(), policy.rules.len().to_string()),
                        ("applied".to_string(), policy.applied.to_string()),
                    ],
                });
            }
            for nat in state.nat_rules.values() {
                nodes.push(TopologyNode {
                    id: nat.nat_id.clone(),
                    kind: "nat".to_string(),
                    name: format!("{:?}", nat.kind),
                    labels: vec![
                        ("source_cidr".to_string(), nat.source_cidr.to_string()),
                        (
                            "translated_address".to_string(),
                            nat.translated_address.to_string(),
                        ),
                        ("interface".to_string(), nat.interface.clone()),
                    ],
                });
            }
            for fault in state.faults.active_snapshot() {
                nodes.push(TopologyNode {
                    id: fault.spec.fault_id.clone(),
                    kind: "fault".to_string(),
                    name: fault.spec.kind.to_string(),
                    labels: vec![
                        ("magnitude".to_string(), fault.spec.magnitude.to_string()),
                        ("target".to_string(), fault.spec.target.clone()),
                        (
                            "expires_at".to_string(),
                            fault.expires_at_unix_seconds.to_string(),
                        ),
                    ],
                });
            }
        }
        Ok(nodes)
    }

    /// All CIDRs currently in use by a lab (for overlap checks).
    pub fn lab_cidrs(&self, lab: &LabId) -> Vec<String> {
        let inner = self.lock();
        inner
            .get(lab)
            .map(|s| s.networks.values().map(|n| n.cidr.to_string()).collect())
            .unwrap_or_default()
    }

    /// Remove **all** state for a lab. Returns the number of resources
    /// removed (networks + namespaces + bridges + allocated addresses).
    /// Removing a non-existent lab is a no-op returning 0.
    pub fn cleanup_lab(&self, lab: &LabId) -> usize {
        let mut inner = self.lock();
        inner
            .remove(lab)
            .map(|state| state.resource_count())
            .unwrap_or(0)
    }

    pub fn lab_exists(&self, lab: &LabId) -> bool {
        self.lock().contains_key(lab)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn lab() -> LabId {
        LabId::new("lab-1").unwrap()
    }

    #[test]
    fn create_network_rejects_overlap_within_lab() {
        let store = TopologyStore::new();
        store
            .create_network(&lab(), "net-a", "vpc-a", "10.0.0.0/16")
            .unwrap();
        let err = store
            .create_network(&lab(), "net-b", "vpc-b", "10.0.1.0/24")
            .unwrap_err();
        assert_eq!(err.specific_code(), "CIDR_OVERLAP");
        // Disjoint CIDR in the same lab is fine.
        store
            .create_network(&lab(), "net-c", "vpc-c", "10.1.0.0/16")
            .unwrap();
        // Same CIDR in a *different* lab is fine (lab isolation).
        let other = LabId::new("lab-2").unwrap();
        store
            .create_network(&other, "net-a", "vpc-a", "10.0.0.0/16")
            .unwrap();
    }

    #[test]
    fn duplicate_network_id_is_conflict() {
        let store = TopologyStore::new();
        store
            .create_network(&lab(), "net-a", "vpc-a", "10.0.0.0/16")
            .unwrap();
        let err = store
            .create_network(&lab(), "net-a", "vpc-a2", "10.1.0.0/16")
            .unwrap_err();
        assert_eq!(err.specific_code(), "NETWORK_EXISTS");
    }

    #[test]
    fn invalid_name_and_cidr_rejected() {
        let store = TopologyStore::new();
        assert!(store
            .create_network(&lab(), "net-a", "Bad Name", "10.0.0.0/16")
            .is_err());
        assert!(store
            .create_network(&lab(), "net-a", "vpc-a", "10.0.0.1/16")
            .is_err());
        assert!(store
            .create_network(&lab(), "net-a", "vpc-a", "not-a-cidr")
            .is_err());
    }

    #[test]
    fn namespace_and_bridge_lifecycle() {
        let store = TopologyStore::new();
        store.create_namespace(&lab(), "ns-web").unwrap();
        assert_eq!(
            store
                .create_namespace(&lab(), "ns-web")
                .unwrap_err()
                .specific_code(),
            "NAMESPACE_EXISTS"
        );
        // Bridge into a missing namespace is NotFound.
        assert!(store.create_bridge(&lab(), "br-0", "ns-missing").is_err());
        store.create_bridge(&lab(), "br-0", "ns-web").unwrap();
        store.create_bridge(&lab(), "br-host", "").unwrap();
        // Deleting the namespace removes its bridges, not host bridges.
        store.delete_namespace(&lab(), "ns-web").unwrap();
        let nodes = store.inspect(&lab(), "").unwrap();
        assert!(nodes
            .iter()
            .any(|n| n.kind == "bridge" && n.name == "br-host"));
        assert!(!nodes.iter().any(|n| n.kind == "bridge" && n.name == "br-0"));
    }

    #[test]
    fn allocate_release_and_inspect() {
        let store = TopologyStore::new();
        store
            .create_network(&lab(), "net-a", "vpc-a", "10.0.0.0/30")
            .unwrap();
        let a = store.allocate_address(&lab(), "net-a", "").unwrap();
        assert_eq!(a.to_string(), "10.0.0.1");
        let b = store.allocate_address(&lab(), "net-a", "10.0.0.2").unwrap();
        assert_eq!(b.to_string(), "10.0.0.2");
        assert!(store.allocate_address(&lab(), "net-a", "").is_err()); // exhausted
        let nodes = store.inspect(&lab(), "net-a").unwrap();
        assert_eq!(nodes.iter().filter(|n| n.kind == "address").count(), 2);
        store.release_address(&lab(), "net-a", "10.0.0.1").unwrap();
        let again = store.allocate_address(&lab(), "net-a", "").unwrap();
        assert_eq!(again.to_string(), "10.0.0.1");
    }

    #[test]
    fn delete_network_releases_allocations_and_cleanup_removes_all() {
        let store = TopologyStore::new();
        store
            .create_network(&lab(), "net-a", "vpc-a", "10.0.0.0/24")
            .unwrap();
        store.create_namespace(&lab(), "ns-1").unwrap();
        store.create_bridge(&lab(), "br-0", "").unwrap();
        store.allocate_address(&lab(), "net-a", "").unwrap();
        store.allocate_address(&lab(), "net-a", "").unwrap();
        // 1 network + 1 namespace + 1 bridge + 2 addresses = 5.
        assert_eq!(store.cleanup_lab(&lab()), 5);
        assert!(!store.lab_exists(&lab()));
        // Cleanup is idempotent: second call removes nothing.
        assert_eq!(store.cleanup_lab(&lab()), 0);
        assert!(store.inspect(&lab(), "").is_err());
    }

    #[test]
    fn delete_missing_network_is_not_found() {
        let store = TopologyStore::new();
        assert!(matches!(
            store.delete_network(&lab(), "nope"),
            Err(CoreError::NotFound(_))
        ));
    }

    // --- Milestone 4 -----------------------------------------------------

    fn lab_with_network(store: &TopologyStore) -> LabId {
        let lab = lab();
        store
            .create_network(&lab, "net-a", "vpc-a", "10.0.0.0/16")
            .unwrap();
        lab
    }

    #[test]
    fn subnet_create_overlap_and_containment() {
        let store = TopologyStore::new();
        let lab = lab_with_network(&store);
        store
            .create_subnet(&lab, "sub-a", "net-a", "subnet-a", "10.0.1.0/24")
            .unwrap();
        // Overlapping sibling subnet is rejected.
        let err = store
            .create_subnet(&lab, "sub-b", "net-a", "subnet-b", "10.0.1.128/25")
            .unwrap_err();
        assert_eq!(err.specific_code(), "CIDR_OVERLAP");
        // Adjacent sibling is fine.
        store
            .create_subnet(&lab, "sub-c", "net-a", "subnet-c", "10.0.2.0/24")
            .unwrap();
        // Outside the parent network is rejected.
        assert!(store
            .create_subnet(&lab, "sub-d", "net-a", "subnet-d", "10.1.0.0/24")
            .is_err());
        // Missing parent network is NotFound.
        assert!(matches!(
            store.create_subnet(&lab, "sub-e", "net-missing", "subnet-e", "10.0.3.0/24"),
            Err(CoreError::NotFound(_))
        ));
        // Duplicate subnet id is a conflict.
        assert_eq!(
            store
                .create_subnet(&lab, "sub-a", "net-a", "subnet-a2", "10.0.3.0/24")
                .unwrap_err()
                .specific_code(),
            "SUBNET_EXISTS"
        );
        // Deleting the network removes its subnets.
        store.delete_network(&lab, "net-a").unwrap();
        assert!(store.delete_subnet(&lab, "sub-a").is_err());
    }

    #[test]
    fn veth_create_attach_and_namespace_cleanup() {
        let store = TopologyStore::new();
        let lab = lab_with_network(&store);
        store.create_namespace(&lab, "ns-web").unwrap();
        store.create_bridge(&lab, "br-0", "").unwrap();
        store
            .create_veth_pair(&lab, "veth-1", "veth-a", "veth-b", "ns-web", "")
            .unwrap();
        // Peer names are unique across pairs.
        assert!(store
            .create_veth_pair(&lab, "veth-2", "veth-a", "veth-c", "", "")
            .is_err());
        // Attach to a missing bridge is NotFound.
        assert!(store
            .attach_interface(&lab, "veth-1", "veth-a", "", "br-missing")
            .is_err());
        // Attach a non-peer interface is invalid.
        assert!(store
            .attach_interface(&lab, "veth-1", "eth0", "", "br-0")
            .is_err());
        let prev = store
            .attach_interface(&lab, "veth-1", "veth-a", "ns-web", "br-0")
            .unwrap();
        assert!(!prev.attached);
        let nodes = store.inspect(&lab, "").unwrap();
        let veth = nodes.iter().find(|n| n.kind == "veth").unwrap();
        assert_eq!(
            veth.labels.iter().find(|(k, _)| k == "attached").unwrap().1,
            "true"
        );
        // Deleting the namespace clears the veth's namespace field.
        store.delete_namespace(&lab, "ns-web").unwrap();
        let nodes = store.inspect(&lab, "").unwrap();
        assert!(!nodes.iter().any(|n| n.kind == "namespace"));
    }

    #[test]
    fn route_add_conflict_and_longest_prefix_lookup() {
        let store = TopologyStore::new();
        let lab = lab();
        store
            .add_route(
                &lab,
                RouteEntry::new("r-default", "0.0.0.0/0", "10.0.0.1", "eth0", 100, ""),
            )
            .unwrap();
        store
            .add_route(
                &lab,
                RouteEntry::new("r-sub", "10.0.1.0/24", "10.0.0.2", "eth1", 100, ""),
            )
            .unwrap();
        // Longest-prefix match via the store.
        assert_eq!(
            store
                .lookup_route(&lab, "10.0.1.55".parse().unwrap())
                .unwrap()
                .route_id,
            "r-sub"
        );
        assert_eq!(
            store
                .lookup_route(&lab, "8.8.8.8".parse().unwrap())
                .unwrap()
                .route_id,
            "r-default"
        );
        // Same destination, different gateway: conflict.
        assert_eq!(
            store
                .add_route(
                    &lab,
                    RouteEntry::new("r-x", "10.0.1.0/24", "10.0.0.9", "eth1", 100, ""),
                )
                .unwrap_err()
                .specific_code(),
            "ROUTE_CONFLICT"
        );
        // Delete then lookup falls back to the default route.
        store.delete_route(&lab, "r-sub").unwrap();
        assert_eq!(
            store
                .lookup_route(&lab, "10.0.1.55".parse().unwrap())
                .unwrap()
                .route_id,
            "r-default"
        );
    }

    #[test]
    fn firewall_policy_create_apply_and_nat() {
        let store = TopologyStore::new();
        let lab = lab_with_network(&store);
        let policy = FirewallPolicy::new(
            "pol-1",
            "net-a",
            "web",
            vec![firewall::Rule {
                rule_id: "deny-ssh".to_string(),
                direction: firewall::Direction::Ingress,
                protocol: firewall::Protocol::Tcp,
                port_start: 22,
                port_end: 22,
                cidr: "0.0.0.0/0".to_string(),
                action: firewall::Action::Deny,
                priority: 10,
            }],
        );
        store.create_firewall_policy(&lab, policy).unwrap();
        assert_eq!(
            store
                .create_firewall_policy(&lab, FirewallPolicy::new("pol-1", "net-a", "web2", vec![]))
                .unwrap_err()
                .specific_code(),
            "FIREWALL_POLICY_EXISTS"
        );
        let applied = store.mark_firewall_applied(&lab, "pol-1").unwrap();
        assert!(applied.applied);
        // Denied packet evaluation through the stored policy.
        let pkt = firewall::Packet::new(
            firewall::Direction::Ingress,
            firewall::Protocol::Tcp,
            "203.0.113.5".parse().unwrap(),
            22,
        );
        assert_eq!(
            store
                .get_firewall_policy(&lab, "pol-1")
                .unwrap()
                .evaluate(&pkt),
            firewall::Decision::Deny
        );
        // NAT create / duplicate / delete.
        store
            .create_nat(
                &lab,
                NatRule {
                    nat_id: "nat-1".to_string(),
                    kind: NatKind::Snat,
                    source_cidr: "10.0.0.0/16".to_string(),
                    translated_address: "203.0.113.1".to_string(),
                    interface: "eth0".to_string(),
                },
            )
            .unwrap();
        assert_eq!(
            store
                .create_nat(
                    &lab,
                    NatRule {
                        nat_id: "nat-1".to_string(),
                        kind: NatKind::Snat,
                        source_cidr: "10.0.0.0/16".to_string(),
                        translated_address: "203.0.113.2".to_string(),
                        interface: "eth0".to_string(),
                    },
                )
                .unwrap_err()
                .specific_code(),
            "NAT_EXISTS"
        );
        store.delete_nat(&lab, "nat-1").unwrap();
        assert!(store.delete_nat(&lab, "nat-1").is_err());
    }

    #[test]
    fn fault_inject_expires_and_cleanup_removes_m4_state() {
        let store = TopologyStore::new();
        let lab = lab_with_network(&store);
        store.create_namespace(&lab, "ns-1").unwrap();
        store
            .create_subnet(&lab, "sub-a", "net-a", "subnet-a", "10.0.1.0/24")
            .unwrap();
        store
            .create_veth_pair(&lab, "veth-1", "veth-a", "veth-b", "", "")
            .unwrap();
        store
            .add_route(
                &lab,
                RouteEntry::new("r1", "0.0.0.0/0", "10.0.0.1", "eth0", 100, ""),
            )
            .unwrap();
        store
            .create_firewall_policy(&lab, FirewallPolicy::new("pol-1", "net-a", "web", vec![]))
            .unwrap();
        store
            .create_nat(
                &lab,
                NatRule {
                    nat_id: "nat-1".to_string(),
                    kind: NatKind::Snat,
                    source_cidr: "10.0.0.0/16".to_string(),
                    translated_address: "203.0.113.1".to_string(),
                    interface: "eth0".to_string(),
                },
            )
            .unwrap();
        let fault = store
            .inject_fault(
                &lab,
                FaultSpec::new(
                    "fault-1",
                    fault_injection::FaultKind::Latency,
                    100.0,
                    60,
                    "",
                ),
            )
            .unwrap();
        assert!(fault.expires_at_unix_seconds > 0);
        assert_eq!(store.active_faults(&lab).len(), 1);
        // Invalid fault is rejected.
        assert!(store
            .inject_fault(
                &lab,
                FaultSpec::new("fault-2", fault_injection::FaultKind::Loss, 150.0, 60, ""),
            )
            .is_err());
        // Cleanup counts everything: 1 network + 1 namespace + 1 subnet
        // + 1 veth + 1 route + 1 policy + 1 NAT + 1 fault = 8.
        assert_eq!(store.cleanup_lab(&lab), 8);
        assert!(!store.lab_exists(&lab));
    }
}
