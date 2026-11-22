//! `network-engine` binary.
//!
//! Listens on `NETWORK_ENGINE_ADDR` (default `0.0.0.0:50051`).
//! Backend selection: `NETWORK_ENGINE_BACKEND=simulated|netlink`
//! (default `simulated`; `netlink` is the real rtnetlink/nftables
//! backend and needs `CAP_NET_ADMIN` plus the `nft` binary — it is
//! unverified in the sandbox).
//! Structured logs go to stdout; set `RUST_LOG` to tune verbosity,
//! e.g. `RUST_LOG=network_engine=debug,grpc_server=debug`.

use grpc_server::EngineState;
use std::net::SocketAddr;
use tracing::info;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "network_engine=info,grpc_server=info".into()),
        )
        .init();

    let addr: SocketAddr = std::env::var("NETWORK_ENGINE_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:50051".to_string())
        .parse()
        .map_err(|e| format!("invalid NETWORK_ENGINE_ADDR: {e}"))?;

    // Phase 7 (R1): fail closed. The engine never starts
    // unauthenticated in production: a token must be configured
    // (CLOUDFORGE_ENGINE_TOKEN) or dev mode explicitly opted into
    // (CLOUDFORGE_DEV=1, which uses the well-known dev token).
    let token = grpc_server::engine_token_from_env();
    if token.is_none() {
        return Err("refusing to start: set CLOUDFORGE_ENGINE_TOKEN to a long random value, or CLOUDFORGE_DEV=1 for local development only".into());
    }

    let state = EngineState::from_env();
    info!(
        backend = state.backend.fidelity(),
        "starting network-engine"
    );
    grpc_server::serve_with_token(addr, state, token).await?;
    info!("network-engine shut down cleanly");
    Ok(())
}
