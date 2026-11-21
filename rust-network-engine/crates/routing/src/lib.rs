//! Pure route-table logic.
//!
//! Std + serde only: no syscalls, no network access. A [`RouteTable`]
//! holds static routes and answers longest-prefix-match lookups.
//!
//! Conflict rules:
//! * Adding a route whose destination prefix already exists with a
//!   **different** gateway or interface is a conflict
//!   ([`RouteError::Conflict`]) — the same destination must not point
//!   two ways.
//! * Adding the identical route twice is a no-op success (idempotent).
//! * Overlapping but non-identical prefixes (e.g. `10.0.0.0/16` and
//!   `10.0.1.0/24`) are **not** conflicts: they coexist and the lookup
//!   resolves them by longest prefix, then lowest metric.
//! * The default route is `0.0.0.0/0` and matches everything, last.

use ipam::Cidr;
use serde::{Deserialize, Serialize};
use std::fmt;
use std::net::Ipv4Addr;

/// One static route.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RouteEntry {
    pub route_id: String,
    pub destination: String, // CIDR, e.g. "10.0.1.0/24" or "0.0.0.0/0"
    pub gateway: String,     // IPv4 address
    pub interface: String,
    pub metric: i32,
    pub namespace: String,
}

impl RouteEntry {
    pub fn new(
        route_id: &str,
        destination: &str,
        gateway: &str,
        interface: &str,
        metric: i32,
        namespace: &str,
    ) -> Self {
        RouteEntry {
            route_id: route_id.to_string(),
            destination: destination.to_string(),
            gateway: gateway.to_string(),
            interface: interface.to_string(),
            metric,
            namespace: namespace.to_string(),
        }
    }

    /// Parsed destination CIDR.
    pub fn destination_cidr(&self) -> Result<Cidr, RouteError> {
        Cidr::parse(&self.destination).map_err(|e| RouteError::Invalid(e.to_string()))
    }

    /// Parsed gateway address.
    pub fn gateway_addr(&self) -> Result<Ipv4Addr, RouteError> {
        self.gateway
            .parse()
            .map_err(|_| RouteError::Invalid(format!("invalid gateway IPv4: {}", self.gateway)))
    }

    /// Validate the entry: non-empty ids, parseable destination CIDR
    /// and gateway, non-negative metric.
    pub fn validate(&self) -> Result<(), RouteError> {
        if self.route_id.trim().is_empty() {
            return Err(RouteError::Invalid(
                "route_id must not be empty".to_string(),
            ));
        }
        if self.interface.trim().is_empty() {
            return Err(RouteError::Invalid(
                "interface must not be empty".to_string(),
            ));
        }
        if self.metric < 0 {
            return Err(RouteError::Invalid(format!(
                "metric {} must not be negative",
                self.metric
            )));
        }
        self.destination_cidr()?;
        self.gateway_addr()?;
        Ok(())
    }
}

/// Errors from route-table operations.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RouteError {
    Invalid(String),
    Conflict(String),
    NotFound(String),
}

impl RouteError {
    /// Stable machine-readable code.
    pub fn error_code(&self) -> &'static str {
        match self {
            RouteError::Invalid(_) => "INVALID_ARGUMENT",
            RouteError::Conflict(_) => "ROUTE_CONFLICT",
            RouteError::NotFound(_) => "NOT_FOUND",
        }
    }
}

impl fmt::Display for RouteError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            RouteError::Invalid(m) | RouteError::Conflict(m) | RouteError::NotFound(m) => {
                f.write_str(m)
            }
        }
    }
}

impl std::error::Error for RouteError {}

/// An in-memory static route table.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct RouteTable {
    routes: Vec<RouteEntry>,
}

impl RouteTable {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn len(&self) -> usize {
        self.routes.len()
    }

    pub fn is_empty(&self) -> bool {
        self.routes.is_empty()
    }

    pub fn routes(&self) -> &[RouteEntry] {
        &self.routes
    }

    /// Add a route. Identical re-add is a no-op; same destination with
    /// a different next hop is a conflict.
    pub fn add(&mut self, entry: RouteEntry) -> Result<(), RouteError> {
        entry.validate()?;
        let dest = entry.destination_cidr()?;
        for existing in &self.routes {
            if existing.destination_cidr()? == dest {
                if existing.gateway == entry.gateway
                    && existing.interface == entry.interface
                    && existing.metric == entry.metric
                {
                    return Ok(()); // Idempotent re-add.
                }
                return Err(RouteError::Conflict(format!(
                    "ROUTE_CONFLICT: destination {} already routes via {} ({})",
                    entry.destination, existing.gateway, existing.interface
                )));
            }
            if existing.route_id == entry.route_id {
                return Err(RouteError::Conflict(format!(
                    "ROUTE_CONFLICT: route_id {} already exists",
                    entry.route_id
                )));
            }
        }
        self.routes.push(entry);
        Ok(())
    }

    /// Remove a route by id.
    pub fn remove(&mut self, route_id: &str) -> Result<RouteEntry, RouteError> {
        let pos = self
            .routes
            .iter()
            .position(|r| r.route_id == route_id)
            .ok_or_else(|| RouteError::NotFound(format!("route {route_id} not found")))?;
        Ok(self.routes.remove(pos))
    }

    pub fn get(&self, route_id: &str) -> Option<&RouteEntry> {
        self.routes.iter().find(|r| r.route_id == route_id)
    }

    /// Longest-prefix match for `addr`: among routes whose destination
    /// contains the address, pick the longest prefix; break ties by
    /// lowest metric, then by route id for determinism. Returns `None`
    /// when no route (not even a default route) matches.
    pub fn lookup(&self, addr: Ipv4Addr) -> Option<&RouteEntry> {
        self.routes
            .iter()
            .filter_map(|r| {
                let cidr = r.destination_cidr().ok()?;
                if cidr.contains_addr(addr) {
                    Some((cidr.prefix(), r.metric, r))
                } else {
                    None
                }
            })
            .min_by(|a, b| {
                b.0.cmp(&a.0) // longest prefix first
                    .then_with(|| a.1.cmp(&b.1)) // then lowest metric
                    .then_with(|| a.2.route_id.cmp(&b.2.route_id))
            })
            .map(|(_, _, r)| r)
    }

    /// Does adding `entry` conflict with the table (without mutating)?
    pub fn would_conflict(&self, entry: &RouteEntry) -> Result<(), RouteError> {
        let mut probe = self.clone();
        probe.add(entry.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn route(id: &str, dest: &str, gw: &str, iface: &str, metric: i32) -> RouteEntry {
        RouteEntry::new(id, dest, gw, iface, metric, "")
    }

    #[test]
    fn longest_prefix_match_prefers_most_specific() {
        let mut table = RouteTable::new();
        table
            .add(route("r-default", "0.0.0.0/0", "10.0.0.1", "eth0", 100))
            .unwrap();
        table
            .add(route("r-vpc", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        table
            .add(route("r-sub", "10.0.1.0/24", "10.0.0.2", "eth1", 100))
            .unwrap();
        assert_eq!(
            table.lookup("10.0.1.55".parse().unwrap()).unwrap().route_id,
            "r-sub"
        );
        assert_eq!(
            table.lookup("10.0.2.55".parse().unwrap()).unwrap().route_id,
            "r-vpc"
        );
        assert_eq!(
            table.lookup("8.8.8.8".parse().unwrap()).unwrap().route_id,
            "r-default"
        );
    }

    #[test]
    fn default_route_matches_everything() {
        let mut table = RouteTable::new();
        table
            .add(route("r-default", "0.0.0.0/0", "10.0.0.1", "eth0", 10))
            .unwrap();
        assert!(table.lookup("192.0.2.1".parse().unwrap()).is_some());
        assert!(table.lookup("10.255.255.255".parse().unwrap()).is_some());
    }

    #[test]
    fn no_route_without_default() {
        let mut table = RouteTable::new();
        table
            .add(route("r-vpc", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        assert!(table.lookup("8.8.8.8".parse().unwrap()).is_none());
        assert!(table.lookup("10.0.0.9".parse().unwrap()).is_some());
    }

    #[test]
    fn overlapping_prefixes_coexist() {
        let mut table = RouteTable::new();
        table
            .add(route("r-big", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        // A more specific overlapping prefix via another gateway is fine.
        table
            .add(route("r-small", "10.0.1.0/24", "10.0.0.2", "eth1", 100))
            .unwrap();
        assert_eq!(table.len(), 2);
    }

    #[test]
    fn same_destination_different_gateway_is_conflict() {
        let mut table = RouteTable::new();
        table
            .add(route("r1", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        let err = table
            .add(route("r2", "10.0.0.0/16", "10.0.0.9", "eth0", 100))
            .unwrap_err();
        assert_eq!(err.error_code(), "ROUTE_CONFLICT");
        assert_eq!(table.len(), 1);
    }

    #[test]
    fn identical_readd_is_idempotent() {
        let mut table = RouteTable::new();
        table
            .add(route("r1", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        table
            .add(route("r1", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        assert_eq!(table.len(), 1);
        // Same route id, different destination: also a conflict.
        assert!(table
            .add(route("r1", "10.1.0.0/16", "10.0.0.1", "eth0", 100))
            .is_err());
    }

    #[test]
    fn metric_breaks_prefix_ties() {
        // Two routes cannot share a destination (conflict), so equal
        // prefix length can only arise from distinct prefixes of the
        // same length that both contain the address — impossible for
        // distinct CIDRs. Instead verify metric ordering is stable
        // when prefixes differ: longer prefix wins regardless of metric.
        let mut table = RouteTable::new();
        table
            .add(route("r-big-cheap", "10.0.0.0/16", "10.0.0.1", "eth0", 1))
            .unwrap();
        table
            .add(route(
                "r-small-pricey",
                "10.0.1.0/24",
                "10.0.0.2",
                "eth1",
                500,
            ))
            .unwrap();
        assert_eq!(
            table.lookup("10.0.1.9".parse().unwrap()).unwrap().route_id,
            "r-small-pricey"
        );
    }

    #[test]
    fn remove_and_lookup_after_remove() {
        let mut table = RouteTable::new();
        table
            .add(route("r1", "10.0.0.0/16", "10.0.0.1", "eth0", 100))
            .unwrap();
        assert_eq!(table.remove("r1").unwrap().route_id, "r1");
        assert!(table.lookup("10.0.0.9".parse().unwrap()).is_none());
        assert_eq!(table.remove("r1").unwrap_err().error_code(), "NOT_FOUND");
    }

    #[test]
    fn validation_rejects_bad_routes() {
        let mut table = RouteTable::new();
        assert!(table
            .add(route("r1", "10.0.0.1/16", "10.0.0.1", "eth0", 0))
            .is_err()); // host bits
        assert!(table
            .add(route("r1", "10.0.0.0/16", "not-an-ip", "eth0", 0))
            .is_err());
        assert!(table
            .add(route("r1", "10.0.0.0/16", "10.0.0.1", "eth0", -1))
            .is_err());
        assert!(table
            .add(route("", "10.0.0.0/16", "10.0.0.1", "eth0", 0))
            .is_err());
        assert_eq!(table.len(), 0);
    }

    #[test]
    fn table_serializes_with_serde() {
        let mut table = RouteTable::new();
        table
            .add(route("r1", "0.0.0.0/0", "10.0.0.1", "eth0", 10))
            .unwrap();
        let json = serde_json::to_string(&table).unwrap();
        let back: RouteTable = serde_json::from_str(&json).unwrap();
        assert_eq!(back.len(), 1);
        assert_eq!(
            back.lookup("8.8.8.8".parse().unwrap()).unwrap().route_id,
            "r1"
        );
    }
}
