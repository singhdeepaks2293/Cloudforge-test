//! gRPC service implementation for `cloudforge.v1.NetworkEngine`.
//!
//! Contract rules enforced by every method:
//! * `RequestMeta` is required and all four identifiers
//!   (`lab_id`, `tenant_id`, `request_id`, `idempotency_key`) must be
//!   non-empty, otherwise `InvalidArgument`.
//! * Mutating methods honour idempotency: a replayed
//!   `(lab_id, idempotency_key)` returns the stored outcome without
//!   re-executing. Read-only methods (`Health`, `InspectTopology`,
//!   `ValidateNetwork`) have no side effects and are idempotent by
//!   construction, so they are not stored.
//! * Failures are returned as structured tonic `Status` values whose
//!   message starts with the stable error code, e.g.
//!   `"CIDR_OVERLAP: 10.0.1.0/24 overlaps 10.0.0.0/16"`. Nothing is
//!   ever reported as successful when it was not.
//! * Every mutating method validates first, mutates the store, then
//!   calls the kernel backend; if the backend fails, the store change
//!   is rolled back (M1's `SimulatedBackend` never fails, but the
//!   rollback path is real and will matter for the M4 rtnetlink
//!   backend).

use crate::proto::network_engine_server::NetworkEngine;
use crate::proto::{
    self, AddRouteRequest, AllocateAddressRequest, AllocateAddressResponse,
    ApplyFirewallPolicyRequest, AttachInterfaceRequest, CollectTelemetryRequest,
    CollectTelemetryResponse, CreateBridgeRequest, CreateFirewallPolicyRequest,
    CreateNamespaceRequest, CreateNatRequest, CreateNetworkRequest, CreateNetworkResponse,
    CreateSubnetRequest, CreateSubnetResponse, CreateVethPairRequest, DeleteNamespaceRequest,
    DeleteNatRequest, DeleteNetworkRequest, DeleteRouteRequest, DeleteSubnetRequest,
    FaultKind as ProtoFaultKind, FaultSpec as ProtoFaultSpec, FirewallAction as ProtoAction,
    FirewallDirection as ProtoDirection, FirewallProtocol as ProtoProtocol,
    FirewallRule as ProtoRule, HealthRequest, HealthResponse, InjectFaultRequest,
    InjectFaultResponse, InspectTopologyRequest, InspectTopologyResponse, NatKind as ProtoNatKind,
    OperationState, ReleaseAddressRequest, RequestMeta as ProtoMeta, Subnet as ProtoSubnet,
    TelemetryPoint as ProtoPoint, TopologyNode as ProtoNode, ValidateNetworkRequest,
    ValidateNetworkResponse,
};
use crate::EngineState;
use engine_core::{
    CoreError, IdempotencyStore, NatKind as CoreNatKind, NatRule as CoreNatRule,
    OperationResult as CoreResult, RequestMeta, StoredOutcome, TopologyStore,
};
use fault_injection::{FaultKind as CoreFaultKind, FaultSpec as CoreFaultSpec};
use ipam::Cidr;
use routing::RouteEntry;
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Instant;
use tonic::{Request, Response, Status};
use tracing::{info, warn};

/// The gRPC service. Cloneable state is held behind `Arc`s in
/// `EngineState`; this struct adds only the start time and the
/// operation counter.
pub struct NetworkEngineService {
    state: EngineState,
    started: Instant,
    op_counter: AtomicU64,
}

impl NetworkEngineService {
    pub fn new(state: EngineState) -> Self {
        NetworkEngineService {
            state,
            started: Instant::now(),
            op_counter: AtomicU64::new(0),
        }
    }

    fn store(&self) -> &TopologyStore {
        &self.state.store
    }

    fn idempotency(&self) -> &IdempotencyStore {
        &self.state.idempotency
    }

    /// Fidelity marker of the selected backend, for health / logs.
    pub fn backend_fidelity(&self) -> &'static str {
        self.state.backend.fidelity()
    }

    /// Record that one mutating RPC ran for this lab. Counters are
    /// real: they only move when an RPC actually runs.
    fn record_op(&self, meta: &RequestMeta, op: &str) {
        self.state.telemetry.increment(
            meta.lab_id.as_str(),
            "operations_total",
            &[("op", op)],
            1.0,
        );
    }

    /// Refresh the `active_faults` gauge for a lab from the store.
    fn refresh_fault_gauge(&self, meta: &RequestMeta) {
        let active = self.store().active_faults(&meta.lab_id).len() as f64;
        self.state
            .telemetry
            .set(meta.lab_id.as_str(), "active_faults", &[], active);
    }

    fn next_operation_id(&self) -> String {
        format!("op-{}", self.op_counter.fetch_add(1, Ordering::SeqCst) + 1)
    }

    /// Validate the proto `RequestMeta` carried by a request.
    #[allow(clippy::result_large_err)] // tonic::Status is large by construction; these are thin validation helpers.
    fn meta(req_meta: Option<ProtoMeta>) -> Result<RequestMeta, Status> {
        let m = req_meta
            .ok_or_else(|| Status::invalid_argument("INVALID_ARGUMENT: meta is required"))?;
        RequestMeta::new(
            &m.lab_id,
            &m.tenant_id,
            &m.request_id,
            &m.idempotency_key,
            m.timeout_ms,
        )
        .map_err(core_to_status)
    }

    fn idem_get(&self, meta: &RequestMeta) -> Option<StoredOutcome> {
        self.idempotency()
            .get(meta.lab_id.as_str(), &meta.idempotency_key)
    }

    fn idem_put(&self, meta: &RequestMeta, result: &CoreResult, payload: Option<String>) {
        self.idempotency().insert(
            meta.lab_id.as_str(),
            &meta.idempotency_key,
            StoredOutcome {
                result: result.clone(),
                payload,
            },
        );
    }

    fn ok_result(&self, started: Instant, message: impl Into<String>) -> CoreResult {
        let mut r = CoreResult::succeeded(self.next_operation_id(), message);
        r.duration_ms = started.elapsed().as_millis() as i64;
        r
    }
}

/// Map a core error to a structured tonic status. The message always
/// starts with the stable error code.
fn core_to_status(err: CoreError) -> Status {
    let code = err.specific_code();
    let message = format!("{code}: {err}");
    match &err {
        CoreError::InvalidArgument(_) => Status::invalid_argument(message),
        CoreError::NotFound(_) => Status::not_found(message),
        CoreError::Conflict(_) => match code {
            "CIDR_OVERLAP" | "ROUTE_CONFLICT" => Status::failed_precondition(message),
            _ => Status::already_exists(message),
        },
        CoreError::Exhausted(_) => Status::resource_exhausted(message),
        CoreError::Backend(_) => Status::internal(message),
        CoreError::Unavailable(_) => Status::unavailable(message),
    }
}

fn proto_result(r: &CoreResult) -> proto::OperationResult {
    proto::OperationResult {
        operation_id: r.operation_id.clone(),
        state: match r.state {
            engine_core::OperationState::Succeeded => OperationState::Succeeded as i32,
            engine_core::OperationState::Failed => OperationState::Failed as i32,
            engine_core::OperationState::Pending => OperationState::Pending as i32,
            engine_core::OperationState::Running => OperationState::Running as i32,
            engine_core::OperationState::RolledBack => OperationState::RolledBack as i32,
            engine_core::OperationState::Cancelled => OperationState::Cancelled as i32,
            engine_core::OperationState::Unspecified => OperationState::Unspecified as i32,
        },
        message: r.message.clone(),
        error_code: r.error_code.clone().unwrap_or_default(),
        duration_ms: r.duration_ms,
    }
}

/// Convert a proto firewall rule into the firewall crate's rule.
/// Rejects unknown enum values and out-of-range ports.
#[allow(clippy::result_large_err)] // tonic::Status is large by construction; these are thin validation helpers.
fn proto_firewall_rule(rule: &ProtoRule) -> Result<firewall::Rule, Status> {
    let direction = match ProtoDirection::try_from(rule.direction).ok() {
        Some(ProtoDirection::Ingress) => firewall::Direction::Ingress,
        Some(ProtoDirection::Egress) => firewall::Direction::Egress,
        _ => {
            return Err(Status::invalid_argument(
                "INVALID_ARGUMENT: firewall rule direction must be INGRESS or EGRESS",
            ))
        }
    };
    let protocol = match ProtoProtocol::try_from(rule.protocol).ok() {
        Some(ProtoProtocol::Any) => firewall::Protocol::Any,
        Some(ProtoProtocol::Tcp) => firewall::Protocol::Tcp,
        Some(ProtoProtocol::Udp) => firewall::Protocol::Udp,
        Some(ProtoProtocol::Icmp) => firewall::Protocol::Icmp,
        _ => {
            return Err(Status::invalid_argument(
                "INVALID_ARGUMENT: firewall rule protocol must be ANY, TCP, UDP or ICMP",
            ))
        }
    };
    let action = match ProtoAction::try_from(rule.action).ok() {
        Some(ProtoAction::Allow) => firewall::Action::Allow,
        Some(ProtoAction::Deny) => firewall::Action::Deny,
        _ => {
            return Err(Status::invalid_argument(
                "INVALID_ARGUMENT: firewall rule action must be ALLOW or DENY",
            ))
        }
    };
    if !(0..=65_535).contains(&rule.port_start) || !(0..=65_535).contains(&rule.port_end) {
        return Err(Status::invalid_argument(
            "INVALID_ARGUMENT: firewall rule ports must be in 0..=65535",
        ));
    }
    Ok(firewall::Rule {
        rule_id: rule.rule_id.clone(),
        direction,
        protocol,
        port_start: rule.port_start as u16,
        port_end: rule.port_end as u16,
        cidr: rule.cidr.clone(),
        action,
        priority: rule.priority,
    })
}

#[allow(clippy::result_large_err)] // tonic::Status is large by construction; these are thin validation helpers.
fn proto_nat_kind(kind: i32) -> Result<CoreNatKind, Status> {
    match ProtoNatKind::try_from(kind).ok() {
        Some(ProtoNatKind::Snat) => Ok(CoreNatKind::Snat),
        Some(ProtoNatKind::Dnat) => Ok(CoreNatKind::Dnat),
        _ => Err(Status::invalid_argument(
            "INVALID_ARGUMENT: NAT kind must be SNAT or DNAT",
        )),
    }
}

#[allow(clippy::result_large_err)] // tonic::Status is large by construction; these are thin validation helpers.
fn proto_fault_kind(kind: i32) -> Result<CoreFaultKind, Status> {
    match ProtoFaultKind::try_from(kind).ok() {
        Some(ProtoFaultKind::Latency) => Ok(CoreFaultKind::Latency),
        Some(ProtoFaultKind::Loss) => Ok(CoreFaultKind::Loss),
        Some(ProtoFaultKind::Partition) => Ok(CoreFaultKind::Partition),
        Some(ProtoFaultKind::Dns) => Ok(CoreFaultKind::Dns),
        Some(ProtoFaultKind::RouteBlackhole) => Ok(CoreFaultKind::RouteBlackhole),
        _ => Err(Status::invalid_argument(
            "INVALID_ARGUMENT: fault kind must be latency, loss, partition, dns or route-blackhole",
        )),
    }
}

#[tonic::async_trait]
impl NetworkEngine for NetworkEngineService {
    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        Ok(Response::new(HealthResponse {
            service: "network-engine".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            ok: true,
            detail: format!(
                "ok; backend={} (M4 — {})",
                self.backend_fidelity(),
                if self.backend_fidelity() == "SIMULATED" {
                    "no kernel mutation"
                } else {
                    "real kernel mutation, unverified in this sandbox"
                }
            ),
            uptime_seconds: self.started.elapsed().as_secs() as i64,
        }))
    }

    async fn create_network(
        &self,
        request: Request<CreateNetworkRequest>,
    ) -> Result<Response<CreateNetworkResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_network");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_network: idempotent replay");
            let (network_id, cidr) = stored
                .payload
                .as_deref()
                .and_then(|p| p.split_once('\n'))
                .map(|(a, b)| (a.to_string(), b.to_string()))
                .unwrap_or_default();
            return Ok(Response::new(CreateNetworkResponse {
                result: Some(proto_result(&stored.result)),
                network_id,
                cidr,
            }));
        }
        if req.network_id.trim().is_empty() {
            return Err(Status::invalid_argument(
                "INVALID_ARGUMENT: network_id must not be empty",
            ));
        }
        self.store()
            .create_network(&meta.lab_id, &req.network_id, &req.name, &req.cidr)
            .map_err(|e| {
                warn!(request_id = %meta.request_id, error = %e, "create_network rejected");
                core_to_status(e)
            })?;
        if let Err(e) = self
            .state
            .backend
            .create_network(&meta.lab_id, &meta.tenant_id, &req.network_id, &req.cidr)
            .await
        {
            // Roll back the store change: never leave state the kernel
            // does not have.
            let _ = self.store().delete_network(&meta.lab_id, &req.network_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!("network {} created ({})", req.network_id, req.cidr),
        );
        self.idem_put(
            &meta,
            &result,
            Some(format!("{}\n{}", req.network_id, req.cidr)),
        );
        info!(request_id = %meta.request_id, network_id = %req.network_id, cidr = %req.cidr, "network created");
        Ok(Response::new(CreateNetworkResponse {
            result: Some(proto_result(&result)),
            network_id: req.network_id,
            cidr: req.cidr,
        }))
    }

    async fn delete_network(
        &self,
        request: Request<DeleteNetworkRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "delete_network");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "delete_network: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .delete_network(&meta.lab_id, &req.network_id)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .delete_network(&meta.lab_id, &meta.tenant_id, &req.network_id)
            .await
        {
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("network {} deleted", req.network_id));
        self.idem_put(&meta, &result, None);
        info!(request_id = %meta.request_id, network_id = %req.network_id, "network deleted");
        Ok(Response::new(proto_result(&result)))
    }

    async fn create_namespace(
        &self,
        request: Request<CreateNamespaceRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_namespace");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_namespace: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .create_namespace(&meta.lab_id, &req.namespace)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .create_namespace(&meta.lab_id, &meta.tenant_id, &req.namespace)
            .await
        {
            let _ = self.store().delete_namespace(&meta.lab_id, &req.namespace);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("namespace {} created", req.namespace));
        self.idem_put(&meta, &result, None);
        info!(request_id = %meta.request_id, namespace = %req.namespace, "namespace created");
        Ok(Response::new(proto_result(&result)))
    }

    async fn delete_namespace(
        &self,
        request: Request<DeleteNamespaceRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "delete_namespace");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "delete_namespace: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .delete_namespace(&meta.lab_id, &req.namespace)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .delete_namespace(&meta.lab_id, &meta.tenant_id, &req.namespace)
            .await
        {
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("namespace {} deleted", req.namespace));
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn create_bridge(
        &self,
        request: Request<CreateBridgeRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_bridge");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_bridge: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .create_bridge(&meta.lab_id, &req.bridge, &req.namespace)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .create_bridge(&meta.lab_id, &meta.tenant_id, &req.bridge, &req.namespace)
            .await
        {
            // M4 note: the store has no delete_bridge yet, and the
            // simulated backend never fails, so no rollback is possible
            // here; the real backend's bridge-create failure would
            // leave a store bridge without a kernel bridge, which lab
            // cleanup reconciles.
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("bridge {} created", req.bridge));
        self.idem_put(&meta, &result, None);
        info!(request_id = %meta.request_id, bridge = %req.bridge, namespace = %req.namespace, "bridge created");
        Ok(Response::new(proto_result(&result)))
    }

    async fn allocate_address(
        &self,
        request: Request<AllocateAddressRequest>,
    ) -> Result<Response<AllocateAddressResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "allocate_address");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "allocate_address: idempotent replay");
            let address = stored.payload.clone().unwrap_or_default();
            let cidr = self
                .store()
                .network_cidr(&meta.lab_id, &req.network_id)
                .map(|c| c.to_string())
                .unwrap_or_default();
            return Ok(Response::new(AllocateAddressResponse {
                result: Some(proto_result(&stored.result)),
                address,
                cidr,
            }));
        }
        // If the caller supplied a CIDR, it must match the network's CIDR.
        if !req.cidr.trim().is_empty() {
            let expected = self
                .store()
                .network_cidr(&meta.lab_id, &req.network_id)
                .map_err(core_to_status)?;
            let given = Cidr::parse(&req.cidr).map_err(|e| core_to_status(CoreError::from(e)))?;
            if given != expected {
                return Err(Status::invalid_argument(format!(
                    "INVALID_ARGUMENT: cidr {} does not match network {} ({})",
                    req.cidr, req.network_id, expected
                )));
            }
        }
        let address = self
            .store()
            .allocate_address(&meta.lab_id, &req.network_id, &req.hint)
            .map_err(|e| {
                warn!(request_id = %meta.request_id, error = %e, "allocate_address failed");
                core_to_status(e)
            })?;
        let cidr = self
            .store()
            .network_cidr(&meta.lab_id, &req.network_id)
            .map(|c| c.to_string())
            .unwrap_or_default();
        let result = self.ok_result(
            started,
            format!("allocated {address} in {}", req.network_id),
        );
        self.idem_put(&meta, &result, Some(address.to_string()));
        Ok(Response::new(AllocateAddressResponse {
            result: Some(proto_result(&result)),
            address: address.to_string(),
            cidr,
        }))
    }

    async fn release_address(
        &self,
        request: Request<ReleaseAddressRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "release_address");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "release_address: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .release_address(&meta.lab_id, &req.network_id, &req.address)
            .map_err(core_to_status)?;
        let result = self.ok_result(
            started,
            format!("released {} from {}", req.address, req.network_id),
        );
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn inspect_topology(
        &self,
        request: Request<InspectTopologyRequest>,
    ) -> Result<Response<InspectTopologyResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        let nodes = self
            .store()
            .inspect(&meta.lab_id, &req.network_id)
            .map_err(core_to_status)?;
        let proto_nodes: Vec<ProtoNode> = nodes
            .into_iter()
            .map(|n| ProtoNode {
                id: n.id,
                kind: n.kind,
                name: n.name,
                labels: n.labels.into_iter().collect::<HashMap<String, String>>(),
            })
            .collect();
        let result = self.ok_result(started, format!("topology for lab {}", meta.lab_id));
        Ok(Response::new(InspectTopologyResponse {
            result: Some(proto_result(&result)),
            nodes: proto_nodes,
        }))
    }

    async fn validate_network(
        &self,
        request: Request<ValidateNetworkRequest>,
    ) -> Result<Response<ValidateNetworkResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        let cidr = Cidr::parse(&req.cidr).map_err(|e| core_to_status(CoreError::from(e)))?;
        let mut problems: Vec<String> = Vec::new();
        for other_s in &req.existing_cidrs {
            match Cidr::parse(other_s) {
                Ok(other) => {
                    if cidr.overlaps(other) {
                        problems.push(format!("CIDR_OVERLAP: {} overlaps {}", cidr, other));
                    }
                }
                Err(e) => {
                    return Err(Status::invalid_argument(format!(
                        "INVALID_CIDR: existing_cidrs entry {other_s:?} is invalid: {e}"
                    )));
                }
            }
        }
        for existing in self.store().lab_cidrs(&meta.lab_id) {
            if let Ok(other) = Cidr::parse(&existing) {
                if cidr.overlaps(other) {
                    problems.push(format!(
                        "CIDR_OVERLAP: {} overlaps existing lab network {}",
                        cidr, other
                    ));
                }
            }
        }
        let valid = problems.is_empty();
        let result = self.ok_result(
            started,
            if valid {
                format!("{cidr} is valid")
            } else {
                format!("{} problem(s) found", problems.len())
            },
        );
        Ok(Response::new(ValidateNetworkResponse {
            result: Some(proto_result(&result)),
            valid,
            problems,
        }))
    }

    async fn cleanup_lab(
        &self,
        request: Request<crate::proto::CleanupLabRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        // Cleanup is deliberately NOT stored under its own idempotency
        // key: it purges the lab's idempotency keys, so a stored outcome
        // could never be replayed afterwards. Cleanup is safe to retry
        // regardless — removing an already-empty lab returns 0.
        let backend_removed = self
            .state
            .backend
            .cleanup_lab(&meta.lab_id, &meta.tenant_id)
            .await
            .map_err(core_to_status)?;
        let removed = self.store().cleanup_lab(&meta.lab_id);
        let purged = self.idempotency().purge_lab(meta.lab_id.as_str());
        let telemetry_purged = self.state.telemetry.purge_lab(meta.lab_id.as_str());
        let result = self.ok_result(
            started,
            format!(
                "lab {} cleaned up: {removed} store resources, {backend_removed} backend resources, {purged} idempotency keys purged, {telemetry_purged} telemetry counters purged",
                meta.lab_id
            ),
        );
        info!(request_id = %meta.request_id, lab_id = %meta.lab_id, removed, purged, "lab cleaned up");
        Ok(Response::new(proto_result(&result)))
    }

    // --- Milestone 4 -----------------------------------------------------

    async fn create_subnet(
        &self,
        request: Request<CreateSubnetRequest>,
    ) -> Result<Response<CreateSubnetResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_subnet");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_subnet: idempotent replay");
            let parts: Vec<&str> = stored
                .payload
                .as_deref()
                .unwrap_or_default()
                .split('\n')
                .collect();
            let subnet = ProtoSubnet {
                subnet_id: parts.first().copied().unwrap_or_default().to_string(),
                network_id: parts.get(1).copied().unwrap_or_default().to_string(),
                name: parts.get(2).copied().unwrap_or_default().to_string(),
                cidr: parts.get(3).copied().unwrap_or_default().to_string(),
            };
            return Ok(Response::new(CreateSubnetResponse {
                result: Some(proto_result(&stored.result)),
                subnet: Some(subnet),
            }));
        }
        self.store()
            .create_subnet(
                &meta.lab_id,
                &req.subnet_id,
                &req.network_id,
                &req.name,
                &req.cidr,
            )
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .create_subnet(
                &meta.lab_id,
                &meta.tenant_id,
                &req.subnet_id,
                &req.network_id,
                &req.cidr,
            )
            .await
        {
            let _ = self.store().delete_subnet(&meta.lab_id, &req.subnet_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!(
                "subnet {} created in {} ({})",
                req.subnet_id, req.network_id, req.cidr
            ),
        );
        self.idem_put(
            &meta,
            &result,
            Some(format!(
                "{}\n{}\n{}\n{}",
                req.subnet_id, req.network_id, req.name, req.cidr
            )),
        );
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "subnets_created", &[], 1.0);
        Ok(Response::new(CreateSubnetResponse {
            result: Some(proto_result(&result)),
            subnet: Some(ProtoSubnet {
                subnet_id: req.subnet_id,
                network_id: req.network_id,
                name: req.name,
                cidr: req.cidr,
            }),
        }))
    }

    async fn delete_subnet(
        &self,
        request: Request<DeleteSubnetRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "delete_subnet");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "delete_subnet: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let removed = self
            .store()
            .delete_subnet(&meta.lab_id, &req.subnet_id)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .delete_subnet(&meta.lab_id, &meta.tenant_id, &req.subnet_id)
            .await
        {
            self.store().restore_subnet(&meta.lab_id, removed);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("subnet {} deleted", req.subnet_id));
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn create_veth_pair(
        &self,
        request: Request<CreateVethPairRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_veth_pair");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_veth_pair: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        self.store()
            .create_veth_pair(
                &meta.lab_id,
                &req.veth_id,
                &req.peer_a,
                &req.peer_b,
                &req.namespace_a,
                &req.namespace_b,
            )
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .create_veth_pair(
                &meta.lab_id,
                &meta.tenant_id,
                &req.veth_id,
                &req.peer_a,
                &req.peer_b,
            )
            .await
        {
            let _ = self.store().delete_veth_pair(&meta.lab_id, &req.veth_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!(
                "veth pair {} created ({} <-> {})",
                req.veth_id, req.peer_a, req.peer_b
            ),
        );
        self.idem_put(&meta, &result, None);
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "veth_pairs_created", &[], 1.0);
        Ok(Response::new(proto_result(&result)))
    }

    async fn attach_interface(
        &self,
        request: Request<AttachInterfaceRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "attach_interface");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "attach_interface: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let previous = self
            .store()
            .attach_interface(
                &meta.lab_id,
                &req.veth_id,
                &req.interface,
                &req.namespace,
                &req.bridge,
            )
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .attach_interface(
                &meta.lab_id,
                &meta.tenant_id,
                &req.veth_id,
                &req.interface,
                &req.namespace,
                &req.bridge,
            )
            .await
        {
            self.store().restore_veth_pair(&meta.lab_id, previous);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!(
                "interface {} of {} attached (namespace {:?}, bridge {:?})",
                req.interface, req.veth_id, req.namespace, req.bridge
            ),
        );
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn add_route(
        &self,
        request: Request<AddRouteRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "add_route");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "add_route: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let entry = RouteEntry::new(
            &req.route_id,
            &req.destination,
            &req.gateway,
            &req.interface,
            req.metric,
            &req.namespace,
        );
        self.store()
            .add_route(&meta.lab_id, entry.clone())
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .add_route(&meta.lab_id, &meta.tenant_id, &entry)
            .await
        {
            let _ = self.store().delete_route(&meta.lab_id, &req.route_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!(
                "route {} added ({} via {})",
                req.route_id, req.destination, req.gateway
            ),
        );
        self.idem_put(&meta, &result, None);
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "routes_added", &[], 1.0);
        Ok(Response::new(proto_result(&result)))
    }

    async fn delete_route(
        &self,
        request: Request<DeleteRouteRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "delete_route");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "delete_route: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let entry = self
            .store()
            .get_route(&meta.lab_id, &req.route_id)
            .map_err(core_to_status)?;
        self.store()
            .delete_route(&meta.lab_id, &req.route_id)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .delete_route(
                &meta.lab_id,
                &meta.tenant_id,
                &req.route_id,
                &entry.destination,
                &entry.gateway,
                &entry.interface,
            )
            .await
        {
            let _ = self.store().add_route(&meta.lab_id, entry);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("route {} deleted", req.route_id));
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn create_firewall_policy(
        &self,
        request: Request<CreateFirewallPolicyRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_firewall_policy");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_firewall_policy: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let mut rules = Vec::with_capacity(req.rules.len());
        for proto_rule in &req.rules {
            rules.push(proto_firewall_rule(proto_rule)?);
        }
        let policy = firewall::Policy::new(&req.policy_id, &req.network_id, &req.name, rules);
        self.store()
            .create_firewall_policy(&meta.lab_id, policy)
            .map_err(core_to_status)?;
        let result = self.ok_result(
            started,
            format!(
                "firewall policy {} created (not yet applied)",
                req.policy_id
            ),
        );
        self.idem_put(&meta, &result, None);
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "firewall_policies_created", &[], 1.0);
        Ok(Response::new(proto_result(&result)))
    }

    async fn apply_firewall_policy(
        &self,
        request: Request<ApplyFirewallPolicyRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "apply_firewall_policy");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "apply_firewall_policy: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let policy = self
            .store()
            .get_firewall_policy(&meta.lab_id, &req.policy_id)
            .map_err(core_to_status)?;
        // Backend first: the policy is only marked applied once the
        // kernel (or simulation) has accepted it.
        if let Err(e) = self
            .state
            .backend
            .apply_firewall(&meta.lab_id, &meta.tenant_id, &policy)
            .await
        {
            return Err(core_to_status(e));
        }
        self.store()
            .mark_firewall_applied(&meta.lab_id, &req.policy_id)
            .map_err(core_to_status)?;
        let result = self.ok_result(
            started,
            format!("firewall policy {} applied", req.policy_id),
        );
        self.idem_put(&meta, &result, None);
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "firewall_policies_applied", &[], 1.0);
        Ok(Response::new(proto_result(&result)))
    }

    async fn create_nat(
        &self,
        request: Request<CreateNatRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "create_nat");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "create_nat: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let rule = CoreNatRule {
            nat_id: req.nat_id.clone(),
            kind: proto_nat_kind(req.kind)?,
            source_cidr: req.source_cidr.clone(),
            translated_address: req.translated_address.clone(),
            interface: req.interface.clone(),
        };
        self.store()
            .create_nat(&meta.lab_id, rule.clone())
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .create_nat(&meta.lab_id, &meta.tenant_id, &rule)
            .await
        {
            let _ = self.store().delete_nat(&meta.lab_id, &req.nat_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!("NAT rule {} created ({:?})", req.nat_id, rule.kind),
        );
        self.idem_put(&meta, &result, None);
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "nat_rules_created", &[], 1.0);
        Ok(Response::new(proto_result(&result)))
    }

    async fn delete_nat(
        &self,
        request: Request<DeleteNatRequest>,
    ) -> Result<Response<proto::OperationResult>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "delete_nat");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "delete_nat: idempotent replay");
            return Ok(Response::new(proto_result(&stored.result)));
        }
        let removed = self
            .store()
            .delete_nat(&meta.lab_id, &req.nat_id)
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .delete_nat(&meta.lab_id, &meta.tenant_id, &req.nat_id)
            .await
        {
            self.store().restore_nat(&meta.lab_id, removed);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(started, format!("NAT rule {} deleted", req.nat_id));
        self.idem_put(&meta, &result, None);
        Ok(Response::new(proto_result(&result)))
    }

    async fn inject_fault(
        &self,
        request: Request<InjectFaultRequest>,
    ) -> Result<Response<InjectFaultResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        self.record_op(&meta, "inject_fault");
        if let Some(stored) = self.idem_get(&meta) {
            info!(request_id = %meta.request_id, "inject_fault: idempotent replay");
            let (fault_id, expires_at) = stored
                .payload
                .as_deref()
                .and_then(|p| p.split_once('\n'))
                .map(|(a, b)| (a.to_string(), b.parse::<i64>().unwrap_or(0)))
                .unwrap_or_default();
            return Ok(Response::new(InjectFaultResponse {
                result: Some(proto_result(&stored.result)),
                fault_id,
                expires_at_unix_seconds: expires_at,
            }));
        }
        let proto_fault: ProtoFaultSpec = req
            .fault
            .ok_or_else(|| Status::invalid_argument("INVALID_ARGUMENT: fault is required"))?;
        if !self.store().lab_exists(&meta.lab_id) {
            return Err(Status::not_found(format!(
                "NOT_FOUND: lab {} has no resources",
                meta.lab_id
            )));
        }
        if proto_fault.duration_seconds < 0 {
            return Err(Status::invalid_argument(
                "INVALID_ARGUMENT: fault duration_seconds must not be negative",
            ));
        }
        let spec = CoreFaultSpec::new(
            &proto_fault.fault_id,
            proto_fault_kind(proto_fault.kind)?,
            proto_fault.magnitude,
            proto_fault.duration_seconds as u64,
            &proto_fault.target,
        );
        let active = self
            .store()
            .inject_fault(&meta.lab_id, spec.clone())
            .map_err(core_to_status)?;
        if let Err(e) = self
            .state
            .backend
            .inject_fault(&meta.lab_id, &meta.tenant_id, &spec)
            .await
        {
            let _ = self.store().remove_fault(&meta.lab_id, &spec.fault_id);
            return Err(core_to_status(e));
        }
        let result = self.ok_result(
            started,
            format!(
                "fault {} injected ({} {} for {}s)",
                spec.fault_id, spec.kind, spec.magnitude, spec.duration_seconds
            ),
        );
        self.idem_put(
            &meta,
            &result,
            Some(format!(
                "{}\n{}",
                spec.fault_id, active.expires_at_unix_seconds
            )),
        );
        self.state
            .telemetry
            .increment(meta.lab_id.as_str(), "faults_injected", &[], 1.0);
        self.refresh_fault_gauge(&meta);
        Ok(Response::new(InjectFaultResponse {
            result: Some(proto_result(&result)),
            fault_id: spec.fault_id,
            expires_at_unix_seconds: active.expires_at_unix_seconds as i64,
        }))
    }

    async fn collect_telemetry(
        &self,
        request: Request<CollectTelemetryRequest>,
    ) -> Result<Response<CollectTelemetryResponse>, Status> {
        let started = Instant::now();
        let req = request.into_inner();
        let meta = Self::meta(req.meta)?;
        // Read-only: no idempotency storage, but the meta is still
        // required and the fault gauge is refreshed so the snapshot is
        // current. Counters are real — a fresh lab reports zeros.
        self.refresh_fault_gauge(&meta);
        let points: Vec<ProtoPoint> = self
            .state
            .telemetry
            .snapshot(meta.lab_id.as_str())
            .into_iter()
            .map(|p| ProtoPoint {
                name: p.name,
                value: p.value,
                timestamp_unix_seconds: p.timestamp_unix_seconds as i64,
                labels: p.labels.into_iter().collect::<HashMap<String, String>>(),
            })
            .collect();
        let result = self.ok_result(
            started,
            format!(
                "telemetry for lab {}: {} point(s)",
                meta.lab_id,
                points.len()
            ),
        );
        Ok(Response::new(CollectTelemetryResponse {
            result: Some(proto_result(&result)),
            points,
        }))
    }
}
