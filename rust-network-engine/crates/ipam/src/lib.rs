//! Pure IPv4 IPAM (IP address management) logic.
//!
//! This crate deliberately uses only `std` and performs **no** syscalls and
//! no network access, so it can be unit-tested anywhere, including CI
//! runners without privileges. Kernel state (namespaces, bridges, routes)
//! is owned by the `RealNetlink` backend in `engine-core` / `grpc-server`,
//! not by this crate.
//!
//! Rules implemented:
//! * CIDR parsing and validation (`10.0.0.0/16`, host bits must be zero
//!   after masking — `10.0.0.1/16` is rejected as not a network address).
//! * CIDR overlap detection (including containment and identity).
//! * Sequential allocation of usable host addresses, skipping the network
//!   and broadcast addresses for prefixes <= /30, per RFC 3021 both
//!   addresses of a /31 are usable, and a /32 allocates its single address.
//! * Allocation hints: a requested address is honoured only if it lies
//!   inside the CIDR and is free; otherwise an error is returned — the
//!   caller is never silently given a different address.
//! * Release / re-allocation, duplicate-allocation prevention, utilisation
//!   accounting.

use std::collections::HashSet;
use std::fmt;
use std::net::Ipv4Addr;
use std::str::FromStr;

/// Errors returned by IPAM operations. `error_code()` gives the stable
/// machine-readable code surfaced to API clients.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IpamError {
    /// The CIDR string could not be parsed, or is not a network address.
    InvalidCidr(String),
    /// The address string could not be parsed as IPv4.
    InvalidAddress(String),
    /// The address is outside the pool's CIDR.
    OutOfRange { address: String, cidr: String },
    /// The address (or hint) is already allocated.
    AlreadyAllocated(String),
    /// The address is not currently allocated.
    NotAllocated(String),
    /// No usable address remains in the pool.
    Exhausted { cidr: String },
    /// The two CIDRs overlap.
    Overlap { a: String, b: String },
}

impl IpamError {
    /// Stable machine-readable code, e.g. `CIDR_OVERLAP`.
    pub fn error_code(&self) -> &'static str {
        match self {
            IpamError::InvalidCidr(_) => "INVALID_CIDR",
            IpamError::InvalidAddress(_) => "INVALID_ADDRESS",
            IpamError::OutOfRange { .. } => "ADDRESS_OUT_OF_RANGE",
            IpamError::AlreadyAllocated(_) => "ADDRESS_ALREADY_ALLOCATED",
            IpamError::NotAllocated(_) => "ADDRESS_NOT_ALLOCATED",
            IpamError::Exhausted { .. } => "IPAM_EXHAUSTED",
            IpamError::Overlap { .. } => "CIDR_OVERLAP",
        }
    }
}

impl fmt::Display for IpamError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IpamError::InvalidCidr(s) => write!(f, "invalid CIDR: {s}"),
            IpamError::InvalidAddress(s) => write!(f, "invalid IPv4 address: {s}"),
            IpamError::OutOfRange { address, cidr } => {
                write!(f, "address {address} is outside CIDR {cidr}")
            }
            IpamError::AlreadyAllocated(a) => write!(f, "address {a} is already allocated"),
            IpamError::NotAllocated(a) => write!(f, "address {a} is not allocated"),
            IpamError::Exhausted { cidr } => {
                write!(f, "no usable addresses remain in CIDR {cidr}")
            }
            IpamError::Overlap { a, b } => write!(f, "CIDR {a} overlaps CIDR {b}"),
        }
    }
}

impl std::error::Error for IpamError {}

pub type IpamResult<T> = Result<T, IpamError>;

/// An IPv4 network in CIDR notation, stored as a masked base address plus
/// prefix length.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Cidr {
    network: u32,
    prefix: u8,
}

impl Cidr {
    /// Parse `"10.0.0.0/16"`. Host bits must be zero: `10.0.0.1/16` is
    /// rejected so a pool can never be created from a host address.
    pub fn parse(s: &str) -> IpamResult<Self> {
        let (addr_s, prefix_s) = s
            .split_once('/')
            .ok_or_else(|| IpamError::InvalidCidr(format!("{s} is missing a /prefix")))?;
        let prefix: u8 = prefix_s.parse().map_err(|_| {
            IpamError::InvalidCidr(format!("{s} has a non-numeric prefix: {prefix_s}"))
        })?;
        if prefix > 32 {
            return Err(IpamError::InvalidCidr(format!("{s} has a prefix > 32")));
        }
        let addr = Ipv4Addr::from_str(addr_s)
            .map_err(|_| IpamError::InvalidCidr(format!("{s} has an invalid IPv4 address")))?;
        let raw: u32 = addr.into();
        let mask = prefix_mask(prefix);
        let network = raw & mask;
        if network != raw {
            return Err(IpamError::InvalidCidr(format!(
                "{s} is not a network address (host bits set); did you mean {}/{}?",
                Ipv4Addr::from(network),
                prefix
            )));
        }
        Ok(Cidr { network, prefix })
    }

    pub fn prefix(self) -> u8 {
        self.prefix
    }

    pub fn network_address(self) -> Ipv4Addr {
        Ipv4Addr::from(self.network)
    }

    pub fn broadcast_address(self) -> Ipv4Addr {
        Ipv4Addr::from(self.network | !prefix_mask(self.prefix))
    }

    /// Number of addresses in the block, including network/broadcast.
    pub fn total_addresses(self) -> u64 {
        1u64 << (32 - self.prefix)
    }

    /// Number of addresses that may be allocated.
    pub fn usable_addresses(self) -> u64 {
        match self.prefix {
            32 => 1,
            31 => 2,
            p if p <= 30 => self.total_addresses() - 2,
            _ => 0, // unreachable: prefix <= 32 by construction
        }
    }

    pub fn contains_addr(self, addr: Ipv4Addr) -> bool {
        let raw: u32 = addr.into();
        raw & prefix_mask(self.prefix) == self.network
    }

    /// True if `addr` may be handed out by this pool (in range and not a
    /// reserved network/broadcast address, except /31 and /32 per above).
    pub fn is_usable(self, addr: Ipv4Addr) -> bool {
        if !self.contains_addr(addr) {
            return false;
        }
        match self.prefix {
            32 | 31 => true,
            _ => addr != self.network_address() && addr != self.broadcast_address(),
        }
    }

    /// True if this CIDR and `other` share any address.
    pub fn overlaps(self, other: Cidr) -> bool {
        let (small, big) = if self.prefix >= other.prefix {
            (self, other)
        } else {
            (other, self)
        };
        // `small` is the more specific (or equal) block: overlap iff its
        // network address falls inside the larger block.
        big.contains_addr(small.network_address())
    }

    /// First usable address in the block.
    fn first_usable(self) -> u32 {
        match self.prefix {
            32 | 31 => self.network,
            _ => self.network + 1,
        }
    }

    /// Last usable address in the block.
    fn last_usable(self) -> u32 {
        match self.prefix {
            32 | 31 => self.network | !prefix_mask(self.prefix),
            _ => (self.network | !prefix_mask(self.prefix)) - 1,
        }
    }
}

impl fmt::Display for Cidr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}/{}", self.network_address(), self.prefix)
    }
}

fn prefix_mask(prefix: u8) -> u32 {
    if prefix == 0 {
        0
    } else {
        u32::MAX << (32 - prefix)
    }
}

/// Parse two CIDR strings and report whether they overlap.
pub fn cidrs_overlap(a: &str, b: &str) -> IpamResult<bool> {
    let ca = Cidr::parse(a)?;
    let cb = Cidr::parse(b)?;
    Ok(ca.overlaps(cb))
}

/// Parse a CIDR, returning `IpamError::Overlap` if it overlaps any entry
/// of `existing` (each entry is validated as a CIDR as well).
pub fn check_no_overlap(cidr: &str, existing: &[&str]) -> IpamResult<Cidr> {
    let parsed = Cidr::parse(cidr)?;
    for other_s in existing {
        let other = Cidr::parse(other_s)?;
        if parsed.overlaps(other) {
            return Err(IpamError::Overlap {
                a: parsed.to_string(),
                b: other.to_string(),
            });
        }
    }
    Ok(parsed)
}

/// An allocation pool for one CIDR block.
#[derive(Debug, Clone)]
pub struct IpamPool {
    cidr: Cidr,
    allocated: HashSet<u32>,
    /// Next candidate address for sequential allocation.
    next: u32,
}

impl IpamPool {
    pub fn new(cidr: Cidr) -> Self {
        let next = cidr.first_usable();
        IpamPool {
            cidr,
            allocated: HashSet::new(),
            next,
        }
    }

    pub fn cidr(&self) -> Cidr {
        self.cidr
    }

    pub fn allocated_count(&self) -> usize {
        self.allocated.len()
    }

    pub fn usable_total(&self) -> u64 {
        self.cidr.usable_addresses()
    }

    /// `(allocated, usable_total, utilisation_percent_0_100)`.
    pub fn utilisation(&self) -> (usize, u64, f64) {
        let total = self.usable_total();
        let pct = if total == 0 {
            0.0
        } else {
            self.allocated.len() as f64 / total as f64 * 100.0
        };
        (self.allocated.len(), total, pct)
    }

    pub fn is_allocated(&self, addr: Ipv4Addr) -> bool {
        self.allocated.contains(&u32::from(addr))
    }

    /// Allocate the next free usable address, scanning from the last
    /// allocation point and wrapping once.
    pub fn allocate(&mut self) -> IpamResult<Ipv4Addr> {
        let first = self.cidr.first_usable();
        let last = self.cidr.last_usable();
        let mut candidate = self.next;
        loop {
            if !self.allocated.contains(&candidate) {
                self.allocated.insert(candidate);
                self.next = if candidate == last {
                    first
                } else {
                    candidate + 1
                };
                return Ok(Ipv4Addr::from(candidate));
            }
            if candidate == last {
                candidate = first;
            } else {
                candidate += 1;
            }
            if candidate == self.next {
                return Err(IpamError::Exhausted {
                    cidr: self.cidr.to_string(),
                });
            }
        }
    }

    /// Allocate a specific address. Fails (never substitutes another
    /// address) when the hint is unparseable, out of range, reserved, or
    /// already allocated.
    pub fn allocate_hint(&mut self, hint: &str) -> IpamResult<Ipv4Addr> {
        let addr =
            Ipv4Addr::from_str(hint).map_err(|_| IpamError::InvalidAddress(hint.to_string()))?;
        if !self.cidr.contains_addr(addr) {
            return Err(IpamError::OutOfRange {
                address: hint.to_string(),
                cidr: self.cidr.to_string(),
            });
        }
        if !self.cidr.is_usable(addr) {
            return Err(IpamError::OutOfRange {
                address: format!("{hint} is a reserved network/broadcast address"),
                cidr: self.cidr.to_string(),
            });
        }
        let raw: u32 = addr.into();
        if self.allocated.contains(&raw) {
            return Err(IpamError::AlreadyAllocated(hint.to_string()));
        }
        self.allocated.insert(raw);
        Ok(addr)
    }

    /// Release a previously allocated address so it can be allocated again.
    pub fn release(&mut self, addr: Ipv4Addr) -> IpamResult<()> {
        let raw: u32 = addr.into();
        if !self.allocated.remove(&raw) {
            return Err(IpamError::NotAllocated(addr.to_string()));
        }
        Ok(())
    }

    /// Release by string; parse errors are reported as `InvalidAddress`.
    pub fn release_str(&mut self, addr: &str) -> IpamResult<()> {
        let parsed =
            Ipv4Addr::from_str(addr).map_err(|_| IpamError::InvalidAddress(addr.to_string()))?;
        self.release(parsed)
    }

    /// All currently allocated addresses, sorted.
    pub fn allocated_addresses(&self) -> Vec<Ipv4Addr> {
        let mut v: Vec<u32> = self.allocated.iter().copied().collect();
        v.sort_unstable();
        v.into_iter().map(Ipv4Addr::from).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_valid_cidr() {
        let c = Cidr::parse("10.10.0.0/16").unwrap();
        assert_eq!(c.network_address().to_string(), "10.10.0.0");
        assert_eq!(c.prefix(), 16);
        assert_eq!(c.total_addresses(), 65_536);
        assert_eq!(c.usable_addresses(), 65_534);
        assert_eq!(c.broadcast_address().to_string(), "10.10.255.255");
    }

    #[test]
    fn parse_rejects_host_bits_set() {
        assert!(matches!(
            Cidr::parse("10.10.0.1/16"),
            Err(IpamError::InvalidCidr(_))
        ));
    }

    #[test]
    fn parse_rejects_bad_inputs() {
        for bad in [
            "",
            "10.0.0.0",
            "10.0.0.0/",
            "10.0.0.0/33",
            "10.0.0.0/abc",
            "999.0.0.0/8",
            "10.0.0.0/-1",
        ] {
            assert!(Cidr::parse(bad).is_err(), "{bad} should be rejected");
        }
    }

    #[test]
    fn overlap_detection() {
        // Identical.
        assert!(cidrs_overlap("10.0.0.0/16", "10.0.0.0/16").unwrap());
        // Containment, either direction.
        assert!(cidrs_overlap("10.0.0.0/16", "10.0.1.0/24").unwrap());
        assert!(cidrs_overlap("10.0.1.0/24", "10.0.0.0/16").unwrap());
        // Partial overlap.
        assert!(cidrs_overlap("10.0.0.0/23", "10.0.1.0/24").unwrap());
        // Adjacent but disjoint.
        assert!(!cidrs_overlap("10.0.0.0/24", "10.0.1.0/24").unwrap());
        assert!(!cidrs_overlap("10.0.0.0/16", "10.1.0.0/16").unwrap());
        assert!(!cidrs_overlap("192.168.0.0/24", "10.0.0.0/8").unwrap());
        // /0 contains everything.
        assert!(cidrs_overlap("0.0.0.0/0", "10.0.0.0/8").unwrap());
    }

    #[test]
    fn check_no_overlap_reports_conflict() {
        let err = check_no_overlap("10.0.1.0/24", &["10.0.0.0/16"]).unwrap_err();
        assert_eq!(err.error_code(), "CIDR_OVERLAP");
        assert!(check_no_overlap("10.1.0.0/16", &["10.0.0.0/16"]).is_ok());
    }

    #[test]
    fn sequential_allocation_skips_reserved() {
        let mut pool = IpamPool::new(Cidr::parse("10.0.0.0/30").unwrap());
        assert_eq!(pool.allocate().unwrap().to_string(), "10.0.0.1");
        assert_eq!(pool.allocate().unwrap().to_string(), "10.0.0.2");
        assert_eq!(pool.allocate().unwrap_err().error_code(), "IPAM_EXHAUSTED");
    }

    #[test]
    fn exhaustion_and_utilisation() {
        let mut pool = IpamPool::new(Cidr::parse("192.168.1.0/30").unwrap());
        assert_eq!(pool.utilisation(), (0, 2, 0.0));
        pool.allocate().unwrap();
        let (used, total, pct) = pool.utilisation();
        assert_eq!((used, total), (1, 2));
        assert!((pct - 50.0).abs() < f64::EPSILON);
        pool.allocate().unwrap();
        assert_eq!(pool.utilisation().0, 2);
    }

    #[test]
    fn release_then_reallocate() {
        let mut pool = IpamPool::new(Cidr::parse("10.0.0.0/24").unwrap());
        let a = pool.allocate().unwrap();
        let b = pool.allocate().unwrap();
        assert_ne!(a, b);
        pool.release(a).unwrap();
        // Released address must be allocatable again, and releasing twice fails.
        assert!(pool.release(a).is_err());
        let c = pool.allocate_hint(&a.to_string()).unwrap();
        assert_eq!(c, a);
        // Hint for an already-allocated address is a duplicate error.
        assert_eq!(
            pool.allocate_hint(&b.to_string()).unwrap_err(),
            IpamError::AlreadyAllocated(b.to_string())
        );
    }

    #[test]
    fn hint_validation() {
        let mut pool = IpamPool::new(Cidr::parse("10.0.0.0/24").unwrap());
        // Outside the CIDR.
        assert_eq!(
            pool.allocate_hint("10.0.1.5").unwrap_err().error_code(),
            "ADDRESS_OUT_OF_RANGE"
        );
        // Network and broadcast addresses are reserved.
        assert!(pool.allocate_hint("10.0.0.0").is_err());
        assert!(pool.allocate_hint("10.0.0.255").is_err());
        // Unparseable.
        assert_eq!(
            pool.allocate_hint("not-an-ip").unwrap_err().error_code(),
            "INVALID_ADDRESS"
        );
        // Valid hint is honoured exactly.
        assert_eq!(
            pool.allocate_hint("10.0.0.100").unwrap().to_string(),
            "10.0.0.100"
        );
    }

    #[test]
    fn slash31_and_slash32() {
        let mut p31 = IpamPool::new(Cidr::parse("10.0.0.0/31").unwrap());
        assert_eq!(p31.usable_total(), 2);
        assert_eq!(p31.allocate().unwrap().to_string(), "10.0.0.0");
        assert_eq!(p31.allocate().unwrap().to_string(), "10.0.0.1");
        assert!(p31.allocate().is_err());

        let mut p32 = IpamPool::new(Cidr::parse("10.0.0.7/32").unwrap());
        assert_eq!(p32.usable_total(), 1);
        assert_eq!(p32.allocate().unwrap().to_string(), "10.0.0.7");
        assert!(p32.allocate().is_err());
    }

    #[test]
    fn release_unallocated_is_error() {
        let mut pool = IpamPool::new(Cidr::parse("10.0.0.0/24").unwrap());
        assert_eq!(
            pool.release_str("10.0.0.5").unwrap_err().error_code(),
            "ADDRESS_NOT_ALLOCATED"
        );
    }

    #[test]
    fn error_codes_are_stable() {
        assert_eq!(
            IpamError::InvalidCidr("x".into()).error_code(),
            "INVALID_CIDR"
        );
        assert_eq!(
            IpamError::Exhausted {
                cidr: "10.0.0.0/30".into()
            }
            .error_code(),
            "IPAM_EXHAUSTED"
        );
    }
}
