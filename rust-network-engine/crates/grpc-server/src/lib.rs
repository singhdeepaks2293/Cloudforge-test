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

/// The gRPC metadata key carrying the shared-secret engine token
/// (Phase 7, risk R1). The Go control plane sends it on every RPC;
/// the server rejects any RPC without a matching token.
pub const ENGINE_TOKEN_METADATA: &str = "x-cloudforge-engine-token";

/// The well-known dev default engine token. Usable only when
/// `CLOUDFORGE_DEV=1` explicitly opts in; the binary refuses to
/// start without a real token otherwise (see main.rs).
pub const DEV_ENGINE_TOKEN: &str = "dev-only-change-me-engine";

/// Resolve the engine auth token from the environment:
/// `CLOUDFORGE_ENGINE_TOKEN` when set, else the dev default when
/// `CLOUDFORGE_DEV=1`, else `None` (unauthenticated — only the
/// in-process integration tests run this way; the binary entrypoint
/// in main.rs refuses to start with `None`).
pub fn engine_token_from_env() -> Option<String> {
    match std::env::var("CLOUDFORGE_ENGINE_TOKEN") {
        Ok(t) if !t.is_empty() => Some(t),
        _ => {
            if std::env::var("CLOUDFORGE_DEV")
                .map(|v| v == "1")
                .unwrap_or(false)
            {
                Some(DEV_ENGINE_TOKEN.to_string())
            } else {
                None
            }
        }
    }
}

/// Constant-time string equality for the token comparison, so the
/// check does not leak the token length/prefix through timing.
fn timing_safe_eq(a: &str, b: &str) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.bytes().zip(b.bytes()) {
        diff |= x ^ y;
    }
    diff == 0
}

/// tonic interceptor enforcing the engine token on every RPC
/// (including Health). `expected: None` means unauthenticated —
/// used only by the in-process integration tests, never by the
/// deployed binary (main.rs refuses to start that way).
#[derive(Clone)]
pub struct EngineAuthInterceptor {
    expected: Option<std::sync::Arc<String>>,
}

impl tonic::service::Interceptor for EngineAuthInterceptor {
    fn call(&mut self, req: tonic::Request<()>) -> Result<tonic::Request<()>, tonic::Status> {
        let ok = match &self.expected {
            None => true,
            Some(expected) => req
                .metadata()
                .get(ENGINE_TOKEN_METADATA)
                .and_then(|v| v.to_str().ok())
                .map(|got| timing_safe_eq(got, expected))
                .unwrap_or(false),
        };
        if ok {
            Ok(req)
        } else {
            Err(tonic::Status::unauthenticated(
                "ENGINE_UNAUTHENTICATED: a valid x-cloudforge-engine-token is required",
            ))
        }
    }
}

/// The router type: the generated server wrapped in the auth
/// interceptor.
pub type AuthenticatedRouter = tonic::service::interceptor::InterceptedService<
    NetworkEngineServer<NetworkEngineService>,
    EngineAuthInterceptor,
>;

/// Build the tonic router for the given state, authenticating every
/// RPC (including Health) against `token` when `Some`. With `None`
/// the router is unauthenticated — used only by the in-process
/// integration tests, never by the deployed binary.
pub fn router_with_auth(state: EngineState, token: Option<String>) -> AuthenticatedRouter {
    NetworkEngineServer::with_interceptor(
        NetworkEngineService::new(state),
        EngineAuthInterceptor {
            expected: token.map(std::sync::Arc::new),
        },
    )
}

/// Build the tonic router for the given state (unauthenticated;
/// integration tests only — see `router_with_auth`).
pub fn router(state: EngineState) -> AuthenticatedRouter {
    router_with_auth(state, None)
}

/// Serve forever on `addr` until SIGTERM/SIGINT, then shut down
/// gracefully (in-flight RPCs are allowed to finish; tonic drains the
/// listener on shutdown). The engine token comes from the
/// environment (see `engine_token_from_env`); when it is `None` the
/// server logs a loud warning and serves unauthenticated — the
/// binary in main.rs refuses that configuration, so a deployed
/// engine is never unauthenticated.
pub async fn serve(addr: SocketAddr, state: EngineState) -> Result<(), tonic::transport::Error> {
    serve_with_token(addr, state, engine_token_from_env()).await
}

/// Serve with an explicit token (`None` = unauthenticated).
pub async fn serve_with_token(
    addr: SocketAddr,
    state: EngineState,
    token: Option<String>,
) -> Result<(), tonic::transport::Error> {
    let fidelity = state.backend.fidelity();
    let authenticated = token.is_some();
    let svc = router_with_auth(state, token);
    if authenticated {
        info!(
            fidelity,
            "network-engine listening (gRPC auth: token required)"
        );
    } else {
        tracing::warn!("network-engine listening WITHOUT gRPC auth (no CLOUDFORGE_ENGINE_TOKEN and CLOUDFORGE_DEV != 1) — test-only configuration");
    }
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
