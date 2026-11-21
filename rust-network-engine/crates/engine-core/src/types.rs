//! Validated identifiers and operation metadata.
//!
//! Identifiers are newtypes, not bare strings, so a lab id can never be
//! silently passed where a tenant id is expected.

use crate::errors::CoreError;
use std::fmt;

/// A lab (tenant-scoped session) identifier. Must be non-empty.
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct LabId(String);

/// A tenant (user / organisation) identifier. Must be non-empty.
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct TenantId(String);

macro_rules! impl_id {
    ($name:ident, $what:literal) => {
        impl $name {
            pub fn new(s: impl Into<String>) -> Result<Self, CoreError> {
                let s = s.into();
                if s.trim().is_empty() {
                    return Err(CoreError::InvalidArgument(format!(
                        "{} must not be empty",
                        $what
                    )));
                }
                Ok(Self(s))
            }

            /// Borrow the inner value without re-validating.
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }

        impl fmt::Display for $name {
            fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str(&self.0)
            }
        }

        impl From<$name> for String {
            fn from(id: $name) -> String {
                id.0
            }
        }
    };
}

impl_id!(LabId, "lab_id");
impl_id!(TenantId, "tenant_id");

/// Metadata every infrastructure operation must carry. Mirrors
/// `cloudforge.v1.RequestMeta` in `proto/cloudforge/v1/common.proto`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestMeta {
    pub lab_id: LabId,
    pub tenant_id: TenantId,
    pub request_id: String,
    pub idempotency_key: String,
    pub timeout_ms: i64,
}

impl RequestMeta {
    /// Validate raw string fields; all four identifiers are required.
    pub fn new(
        lab_id: &str,
        tenant_id: &str,
        request_id: &str,
        idempotency_key: &str,
        timeout_ms: i64,
    ) -> Result<Self, CoreError> {
        let lab_id = LabId::new(lab_id)?;
        let tenant_id = TenantId::new(tenant_id)?;
        if request_id.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "request_id must not be empty".to_string(),
            ));
        }
        if idempotency_key.trim().is_empty() {
            return Err(CoreError::InvalidArgument(
                "idempotency_key must not be empty".to_string(),
            ));
        }
        Ok(RequestMeta {
            lab_id,
            tenant_id,
            request_id: request_id.to_string(),
            idempotency_key: idempotency_key.to_string(),
            timeout_ms,
        })
    }
}

/// Outcome of one infrastructure operation. Mirrors
/// `cloudforge.v1.OperationResult`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OperationResult {
    pub operation_id: String,
    pub state: OperationState,
    pub message: String,
    /// Machine-readable code, e.g. `CIDR_OVERLAP`; `None` on success.
    pub error_code: Option<String>,
    pub duration_ms: i64,
}

impl OperationResult {
    pub fn succeeded(operation_id: impl Into<String>, message: impl Into<String>) -> Self {
        OperationResult {
            operation_id: operation_id.into(),
            state: OperationState::Succeeded,
            message: message.into(),
            error_code: None,
            duration_ms: 0,
        }
    }

    pub fn failed(
        operation_id: impl Into<String>,
        error_code: impl Into<String>,
        message: impl Into<String>,
    ) -> Self {
        OperationResult {
            operation_id: operation_id.into(),
            state: OperationState::Failed,
            message: message.into(),
            error_code: Some(error_code.into()),
            duration_ms: 0,
        }
    }
}

/// NAT direction, mirroring `cloudforge.v1.NatKind`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum NatKind {
    Snat,
    Dnat,
}

impl fmt::Display for NatKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            NatKind::Snat => f.write_str("SNAT"),
            NatKind::Dnat => f.write_str("DNAT"),
        }
    }
}

/// A NAT rule, as passed to the kernel backend. `source_cidr` is the
/// traffic selector; `translated_address` is the rewrite target
/// (SNAT: the external address; DNAT: the internal destination).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NatRule {
    pub nat_id: String,
    pub kind: NatKind,
    pub source_cidr: String,
    pub translated_address: String,
    pub interface: String,
}

/// Explicit operation states; mirrors `cloudforge.v1.OperationState`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OperationState {
    Unspecified,
    Pending,
    Running,
    Succeeded,
    Failed,
    RolledBack,
    Cancelled,
}

impl fmt::Display for OperationState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            OperationState::Unspecified => "UNSPECIFIED",
            OperationState::Pending => "PENDING",
            OperationState::Running => "RUNNING",
            OperationState::Succeeded => "SUCCEEDED",
            OperationState::Failed => "FAILED",
            OperationState::RolledBack => "ROLLED_BACK",
            OperationState::Cancelled => "CANCELLED",
        };
        f.write_str(s)
    }
}
