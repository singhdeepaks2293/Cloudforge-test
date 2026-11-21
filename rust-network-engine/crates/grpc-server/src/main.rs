//! `network-engine` binary.
//!
//! Listens on `NETWORK_ENGINE_ADDR` (default `0.0.0.0:50051`).
//! Backend selection: `NETWORK_ENGINE_BACKEND=simulated|netlink`
//! (default `simulated`; `netlink` is the real rtnetlink/nftables
//! backend and needs `CAP_NET_ADMIN` plus the `nft` binary — it is
//! unverified in the sandbox).
//! Structured logs go to stdout; set `RUST_LOG` to tune verbosity,
//! e.g. `RUST_LOG=network_engine=debug,grpc_server=debug`.

use grpc_server::{serve, EngineState};
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

    let state = EngineState::from_env();
    info!(
        backend = state.backend.fidelity(),
        "starting network-engine"
    );
    serve(addr, state).await?;
    info!("network-engine shut down cleanly");
    Ok(())
}
