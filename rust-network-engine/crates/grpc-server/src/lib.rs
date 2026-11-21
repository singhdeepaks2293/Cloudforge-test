//! `network-engine` library: generated protobuf bindings, the gRPC
//! service implementation, and the server entrypoint used by both the
//! binary and the integration tests.

#[allow(clippy::result_large_err)]
pub mod proto {
    //! Generated from `proto/cloudforge/v1/{common,network}.proto`.
    //! The package is `cloudforge.v1`; the files are shared with the Go
    //! control plane and must not be redefined here.
    //!
    //! `result_large_err` is allowed here: the generated tonic client
    //! methods return `Result<Response<_>, tonic::Status>`, and
    //! `Status` is large by construction. That is generated code, not
    //! a design choice of this crate.
    tonic::include_proto!("cloudforge.v1");
}

pub mod service;

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use engine_core::{
    BackendKind, IdempotencyStore, RealNetlink, RealNetlinkBackend, SimulatedBackend, TopologyStore,
};
use proto::network_engine_server::NetworkEngineServer;
use service::NetworkEngineService;
use telemetry::TelemetryStore;
use tokio::signal;
use tonic::transport::Server;
use tracing::info;

/// Shared server state, constructed once and cloned into the service.
#[derive(Clone)]
pub struct EngineState {
    pub store: Arc<TopologyStore>,
    pub idempotency: Arc<IdempotencyStore>,
    pub backend: Arc<dyn RealNetlink>,
    pub telemetry: Arc<TelemetryStore>,
}

impl Default for EngineState {
    fn default() -> Self {
        Self::new()
    }
}

impl EngineState {
    /// Default wiring: in-memory store + simulated (non-privileged)
    /// backend + in-memory telemetry. Used by the integration tests.
    pub fn new() -> Self {
        Self::with_backend(Arc::new(SimulatedBackend::new()))
    }

    /// Wiring selected by `NETWORK_ENGINE_BACKEND` (`simulated` |
    /// `netlink`, default `simulated`). Used by the binary.
    pub fn from_env() -> Self {
        match BackendKind::from_env() {
            BackendKind::Simulated => {
                info!("network-engine backend: simulated (no kernel mutation)");
                Self::new()
            }
            BackendKind::Netlink => {
                info!(
                    "network-engine backend: netlink (REAL kernel mutation; needs CAP_NET_ADMIN and the nft binary — unverified in this sandbox)"
                );
                Self::with_backend(Arc::new(RealNetlinkBackend::new()))
            }
        }
    }

    pub fn with_backend(backend: Arc<dyn RealNetlink>) -> Self {
        EngineState {
            store: Arc::new(TopologyStore::new()),
            idempotency: Arc::new(IdempotencyStore::new()),
            backend,
            telemetry: Arc::new(TelemetryStore::new()),
        }
    }
}

/// Build the tonic router for the given state.
pub fn router(state: EngineState) -> NetworkEngineServer<NetworkEngineService> {
    NetworkEngineServer::new(NetworkEngineService::new(state))
}

/// Serve forever on `addr` until SIGTERM/SIGINT, then shut down
/// gracefully (in-flight RPCs are allowed to finish; tonic drains the
/// listener on shutdown).
pub async fn serve(addr: SocketAddr, state: EngineState) -> Result<(), tonic::transport::Error> {
    let fidelity = state.backend.fidelity();
    let svc = router(state);
    info!(fidelity, "network-engine listening");
    Server::builder()
        .timeout(Duration::from_secs(30))
        .add_service(svc)
        .serve_with_shutdown(addr, shutdown_signal())
        .await
}

async fn shutdown_signal() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{signal, SignalKind};
        let mut term = signal(SignalKind::terminate()).expect("SIGTERM handler");
        let mut int = signal(SignalKind::interrupt()).expect("SIGINT handler");
        tokio::select! {
            _ = term.recv() => info!("received SIGTERM, shutting down"),
            _ = int.recv() => info!("received SIGINT, shutting down"),
            _ = signal::ctrl_c() => info!("received Ctrl-C, shutting down"),
        }
    }
    #[cfg(not(unix))]
    {
        let _ = signal::ctrl_c().await;
        info!("received Ctrl-C, shutting down");
    }
}
