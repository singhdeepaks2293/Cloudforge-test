//! Phase 7 engine-auth test (risk R1).
//!
//! Starts a real server with a token (`serve_with_token`) and drives
//! it with a real tonic client: without the token every RPC —
//! including Health — is rejected `Unauthenticated`; with the
//! `x-cloudforge-engine-token` metadata the same RPC succeeds.
//! Run with: `cargo test -p grpc-server --test grpc_auth`.

use grpc_server::proto::{network_engine_client::NetworkEngineClient, HealthRequest};
use grpc_server::{serve_with_token, EngineState, ENGINE_TOKEN_METADATA};
use std::net::TcpListener;
use std::time::Duration;
use tokio::time::sleep;
use tonic::{Code, Request};

const TOKEN: &str = "test-engine-token-long-enough";

async fn start_server() -> String {
    let port = TcpListener::bind("127.0.0.1:0")
        .expect("bind ephemeral port")
        .local_addr()
        .expect("local addr")
        .port();
    let addr = format!("127.0.0.1:{port}").parse().expect("parse addr");
    tokio::spawn(async move {
        serve_with_token(addr, EngineState::new(), Some(TOKEN.to_string()))
            .await
            .expect("server exited with error");
    });
    let endpoint = format!("http://127.0.0.1:{port}");
    for _ in 0..50 {
        if TcpListener::bind(format!("127.0.0.1:{port}")).is_err() {
            return endpoint;
        }
        sleep(Duration::from_millis(50)).await;
    }
    panic!("server did not start on {endpoint}");
}

#[tokio::test]
async fn grpc_auth_rejects_unauthenticated_and_accepts_token() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect client");

    // No token: Health is rejected Unauthenticated.
    let err = client
        .health(Request::new(HealthRequest {}))
        .await
        .expect_err("health without a token must be rejected");
    assert_eq!(err.code(), Code::Unauthenticated, "got {err:?}");
    assert!(
        err.message().contains("ENGINE_UNAUTHENTICATED"),
        "error must carry the stable code: {err:?}"
    );

    // Wrong token: rejected too.
    let mut req = Request::new(HealthRequest {});
    req.metadata_mut()
        .insert(ENGINE_TOKEN_METADATA, "wrong-token".parse().unwrap());
    let err = client
        .health(req)
        .await
        .expect_err("health with a wrong token must be rejected");
    assert_eq!(err.code(), Code::Unauthenticated);

    // Right token: Health succeeds.
    let mut req = Request::new(HealthRequest {});
    req.metadata_mut()
        .insert(ENGINE_TOKEN_METADATA, TOKEN.parse().unwrap());
    let resp = client.health(req).await.expect("health with token");
    assert!(resp.into_inner().ok, "engine health must report ok");
}
