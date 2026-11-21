//! Structured errors for the engine core.
//!
//! Each error carries a stable machine-readable `error_code()` that the
//! gRPC layer copies into the tonic `Status` message and the proto
//! `OperationResult.error_code` field, so clients never have to parse
//! human-readable text to decide what went wrong.

use std::fmt;

/// Errors that can arise from validation, the topology store, or IPAM.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CoreError {
    /// A required field is missing or malformed.
    InvalidArgument(String),
    /// The referenced resource does not exist.
    NotFound(String),
    /// The request conflicts with existing state (duplicate name/id,
    /// overlapping CIDR, already-allocated address, ...).
    Conflict(String),
    /// No resource remains (IPAM exhaustion).
    Exhausted(String),
    /// The (simulated) kernel backend failed.
    Backend(String),
    /// The requested operation is not available from the selected
    /// backend (e.g. tc/netem fault injection in the M4 real backend).
    Unavailable(String),
}

impl CoreError {
    /// Stable machine-readable code.
    pub fn error_code(&self) -> &'static str {
        match self {
            CoreError::InvalidArgument(_) => "INVALID_ARGUMENT",
            CoreError::NotFound(_) => "NOT_FOUND",
            CoreError::Conflict(_) => "CONFLICT",
            CoreError::Exhausted(_) => "IPAM_EXHAUSTED",
            CoreError::Backend(_) => "BACKEND_ERROR",
            CoreError::Unavailable(_) => "BACKEND_UNAVAILABLE",
        }
    }

    /// More specific code for well-known conflicts. The message is
    /// inspected for a marker set by the store (e.g. `CIDR_OVERLAP:`);
    /// anything else stays a generic `CONFLICT`.
    pub fn specific_code(&self) -> &'static str {
        match self {
            CoreError::Conflict(msg) if msg.starts_with("CIDR_OVERLAP:") => "CIDR_OVERLAP",
            CoreError::Conflict(msg) if msg.starts_with("NETWORK_EXISTS:") => "NETWORK_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("NAMESPACE_EXISTS:") => "NAMESPACE_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("BRIDGE_EXISTS:") => "BRIDGE_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("ADDRESS_ALREADY_ALLOCATED:") => {
                "ADDRESS_ALREADY_ALLOCATED"
            }
            CoreError::Conflict(msg) if msg.starts_with("SUBNET_EXISTS:") => "SUBNET_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("VETH_EXISTS:") => "VETH_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("ROUTE_CONFLICT:") => "ROUTE_CONFLICT",
            CoreError::Conflict(msg) if msg.starts_with("FIREWALL_POLICY_EXISTS:") => {
                "FIREWALL_POLICY_EXISTS"
            }
            CoreError::Conflict(msg) if msg.starts_with("NAT_EXISTS:") => "NAT_EXISTS",
            CoreError::Conflict(msg) if msg.starts_with("FAULT_EXISTS:") => "FAULT_EXISTS",
            other => other.error_code(),
        }
    }
}

impl fmt::Display for CoreError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            CoreError::InvalidArgument(m)
            | CoreError::NotFound(m)
            | CoreError::Conflict(m)
            | CoreError::Exhausted(m)
            | CoreError::Backend(m)
            | CoreError::Unavailable(m) => f.write_str(m),
        }
    }
}

impl std::error::Error for CoreError {}

impl From<ipam::IpamError> for CoreError {
    fn from(err: ipam::IpamError) -> Self {
        use ipam::IpamError as E;
        match err {
            E::InvalidCidr(m) | E::InvalidAddress(m) => CoreError::InvalidArgument(m),
            E::OutOfRange { .. } => CoreError::InvalidArgument(err.to_string()),
            E::AlreadyAllocated(m) => {
                CoreError::Conflict(format!("ADDRESS_ALREADY_ALLOCATED: {m}"))
            }
            E::NotAllocated(m) => CoreError::NotFound(m),
            E::Exhausted { cidr } => {
                CoreError::Exhausted(format!("no usable addresses remain in {cidr}"))
            }
            E::Overlap { a, b } => CoreError::Conflict(format!("CIDR_OVERLAP: {a} overlaps {b}")),
        }
    }
}
