//! End-to-end smoke test for the `network-engine` gRPC service.
//!
//! Starts a real server on an ephemeral localhost port (in-process,
//! `tokio::spawn`) and drives it with a real tonic client. The server
//! still uses the M1 `SimulatedBackend`, so no privileges are needed.
//! Run with: `cargo test -p grpc-server --test grpc_smoke`.

use grpc_server::proto::{
    network_engine_client::NetworkEngineClient, AllocateAddressRequest, CleanupLabRequest,
    CreateNamespaceRequest, CreateNetworkRequest, HealthRequest, InspectTopologyRequest,
    ReleaseAddressRequest, RequestMeta, ValidateNetworkRequest,
};
use grpc_server::{serve, EngineState};
use std::net::TcpListener;
use std::time::Duration;
use tokio::time::sleep;
use tonic::{Code, Request};

fn meta(lab: &str, req: &str, key: &str) -> Option<RequestMeta> {
    Some(RequestMeta {
        lab_id: lab.to_string(),
        tenant_id: "tenant-1".to_string(),
        request_id: req.to_string(),
        idempotency_key: key.to_string(),
        timeout_ms: 0,
    })
}

async fn start_server() -> String {
    // Ask the OS for a free port, then serve on it.
    let port = TcpListener::bind("127.0.0.1:0")
        .expect("bind ephemeral port")
        .local_addr()
        .expect("local addr")
        .port();
    let addr = format!("127.0.0.1:{port}").parse().expect("parse addr");
    tokio::spawn(async move {
        serve(addr, EngineState::new())
            .await
            .expect("server exited with error");
    });
    let endpoint = format!("http://127.0.0.1:{port}");
    // Wait until the server accepts connections.
    for _ in 0..50 {
        if NetworkEngineClient::connect(endpoint.clone()).await.is_ok() {
            return endpoint;
        }
        sleep(Duration::from_millis(50)).await;
    }
    panic!("server did not start on {endpoint}");
}

#[tokio::test]
async fn grpc_smoke_end_to_end() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect client");

    // Health: real uptime, service name, version. Retried because
    // tonic connect() is lazy — the server may still be binding.
    let mut health = None;
    for _ in 0..50 {
        match client.health(Request::new(HealthRequest {})).await {
            Ok(resp) => {
                health = Some(resp.into_inner());
                break;
            }
            Err(_) => sleep(Duration::from_millis(50)).await,
        }
    }
    let health = health.expect("health");
    assert!(health.ok);
    assert_eq!(health.service, "network-engine");
    assert!(!health.version.is_empty());
    assert!(health.detail.contains("SIMULATED"));

    // Missing meta is rejected.
    let err = client
        .create_network(Request::new(CreateNetworkRequest {
            meta: None,
            network_id: "net-a".to_string(),
            name: "vpc-a".to_string(),
            cidr: "10.0.0.0/16".to_string(),
        }))
        .await
        .expect_err("missing meta must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    // Create a network.
    let created = client
        .create_network(Request::new(CreateNetworkRequest {
            meta: meta("lab-1", "req-1", "key-1"),
            network_id: "net-a".to_string(),
            name: "vpc-a".to_string(),
            cidr: "10.0.0.0/16".to_string(),
        }))
        .await
        .expect("create network")
        .into_inner();
    let op_id = created.result.expect("result").operation_id;
    assert_eq!(created.network_id, "net-a");
    assert_eq!(created.cidr, "10.0.0.0/16");

    // Idempotent replay: same key returns the SAME operation id and does
    // not create a second network.
    let replay = client
        .create_network(Request::new(CreateNetworkRequest {
            meta: meta("lab-1", "req-2", "key-1"),
            network_id: "net-a".to_string(),
            name: "vpc-a".to_string(),
            cidr: "10.0.0.0/16".to_string(),
        }))
        .await
        .expect("replay")
        .into_inner();
    assert_eq!(
        replay.result.expect("result").operation_id,
        op_id,
        "replay must return the stored operation id"
    );

    // Overlapping CIDR with a fresh key is a structured failure.
    let err = client
        .create_network(Request::new(CreateNetworkRequest {
            meta: meta("lab-1", "req-3", "key-2"),
            network_id: "net-b".to_string(),
            name: "vpc-b".to_string(),
            cidr: "10.0.1.0/24".to_string(),
        }))
        .await
        .expect_err("overlap must fail");
    assert_eq!(err.code(), Code::FailedPrecondition);
    assert!(
        err.message().starts_with("CIDR_OVERLAP:"),
        "{}",
        err.message()
    );

    // Namespace + allocation + inspection.
    client
        .create_namespace(Request::new(CreateNamespaceRequest {
            meta: meta("lab-1", "req-4", "key-3"),
            namespace: "ns-web".to_string(),
        }))
        .await
        .expect("create namespace");

    let alloc = client
        .allocate_address(Request::new(AllocateAddressRequest {
            meta: meta("lab-1", "req-5", "key-4"),
            network_id: "net-a".to_string(),
            cidr: "10.0.0.0/16".to_string(),
            hint: "10.0.0.42".to_string(),
        }))
        .await
        .expect("allocate with hint")
        .into_inner();
    assert_eq!(alloc.address, "10.0.0.42");

    let alloc2 = client
        .allocate_address(Request::new(AllocateAddressRequest {
            meta: meta("lab-1", "req-6", "key-5"),
            network_id: "net-a".to_string(),
            cidr: String::new(),
            hint: String::new(),
        }))
        .await
        .expect("allocate sequential")
        .into_inner();
    assert_eq!(alloc2.address, "10.0.0.1");

    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta("lab-1", "req-7", "key-6"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    assert!(topo
        .nodes
        .iter()
        .any(|n| n.kind == "network" && n.name == "vpc-a"));
    assert!(topo
        .nodes
        .iter()
        .any(|n| n.kind == "namespace" && n.name == "ns-web"));
    assert_eq!(topo.nodes.iter().filter(|n| n.kind == "address").count(), 2);

    // Validation: overlapping CIDR is invalid, with a named problem.
    let validation = client
        .validate_network(Request::new(ValidateNetworkRequest {
            meta: meta("lab-1", "req-8", "key-7"),
            cidr: "10.0.5.0/24".to_string(),
            existing_cidrs: vec!["10.0.0.0/16".to_string()],
        }))
        .await
        .expect("validate")
        .into_inner();
    assert!(!validation.valid);
    assert!(validation
        .problems
        .iter()
        .any(|p| p.starts_with("CIDR_OVERLAP")));

    // Release then cleanup removes everything.
    client
        .release_address(Request::new(ReleaseAddressRequest {
            meta: meta("lab-1", "req-9", "key-8"),
            network_id: "net-a".to_string(),
            address: "10.0.0.42".to_string(),
        }))
        .await
        .expect("release");

    let cleanup = client
        .cleanup_lab(Request::new(CleanupLabRequest {
            meta: meta("lab-1", "req-10", "key-9"),
        }))
        .await
        .expect("cleanup")
        .into_inner();
    assert!(cleanup.message.contains("cleaned up"));

    let err = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta("lab-1", "req-11", "key-10"),
            network_id: String::new(),
        }))
        .await
        .expect_err("inspect after cleanup must fail");
    assert_eq!(err.code(), Code::NotFound);
}
