//! End-to-end Milestone 4 tests for the `network-engine` gRPC service.
//!
//! Starts a real server on an ephemeral localhost port (in-process,
//! `tokio::spawn`) and drives it with a real tonic client, using the
//! `SimulatedBackend`, so no privileges are needed. Covers: subnet
//! create/overlap, veth create+attach, route add + longest-prefix
//! validation (via conflict rules), firewall policy create/apply +
//! denied-packet evaluation (crate-level, asserted through the stored
//! policy), NAT create/delete, fault inject + telemetry, and cleanup
//! removing all M4 state.
//! Run with: `cargo test -p grpc-server --test grpc_m4`.

use grpc_server::proto::{
    network_engine_client::NetworkEngineClient, AddRouteRequest, ApplyFirewallPolicyRequest,
    AttachInterfaceRequest, CleanupLabRequest, CollectTelemetryRequest,
    CreateFirewallPolicyRequest, CreateNamespaceRequest, CreateNatRequest, CreateNetworkRequest,
    CreateSubnetRequest, CreateVethPairRequest, DeleteNatRequest, DeleteRouteRequest,
    DeleteSubnetRequest, FaultKind, FaultSpec, FirewallAction, FirewallDirection, FirewallProtocol,
    FirewallRule, HealthRequest, InjectFaultRequest, InspectTopologyRequest, NatKind, RequestMeta,
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
    for _ in 0..50 {
        if NetworkEngineClient::connect(endpoint.clone()).await.is_ok() {
            return endpoint;
        }
        sleep(Duration::from_millis(50)).await;
    }
    panic!("server did not start on {endpoint}");
}

async fn setup_network(client: &mut NetworkEngineClient<tonic::transport::Channel>, lab: &str) {
    client
        .create_network(Request::new(CreateNetworkRequest {
            meta: meta(lab, "req-net", "key-net"),
            network_id: "net-a".to_string(),
            name: "vpc-a".to_string(),
            cidr: "10.0.0.0/16".to_string(),
        }))
        .await
        .expect("create network");
}

#[tokio::test]
async fn grpc_m4_subnet_create_and_overlap() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-subnet";
    setup_network(&mut client, lab).await;

    // Create a subnet.
    let created = client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-1", "key-1"),
            subnet_id: "sub-a".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-a".to_string(),
            cidr: "10.0.1.0/24".to_string(),
        }))
        .await
        .expect("create subnet")
        .into_inner();
    assert_eq!(created.subnet.expect("subnet").cidr, "10.0.1.0/24");

    // Idempotent replay returns the same operation id.
    let op_id = created.result.expect("result").operation_id;
    let replay = client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-2", "key-1"),
            subnet_id: "sub-a".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-a".to_string(),
            cidr: "10.0.1.0/24".to_string(),
        }))
        .await
        .expect("replay")
        .into_inner();
    assert_eq!(replay.result.expect("result").operation_id, op_id);

    // Overlapping sibling subnet fails with CIDR_OVERLAP.
    let err = client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-3", "key-2"),
            subnet_id: "sub-b".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-b".to_string(),
            cidr: "10.0.1.128/25".to_string(),
        }))
        .await
        .expect_err("overlap must fail");
    assert_eq!(err.code(), Code::FailedPrecondition);
    assert!(
        err.message().starts_with("CIDR_OVERLAP:"),
        "{}",
        err.message()
    );

    // Subnet outside the parent network is invalid.
    let err = client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-4", "key-3"),
            subnet_id: "sub-c".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-c".to_string(),
            cidr: "10.1.0.0/24".to_string(),
        }))
        .await
        .expect_err("outside parent must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    // Adjacent subnet is fine, then delete removes it.
    client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-5", "key-4"),
            subnet_id: "sub-d".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-d".to_string(),
            cidr: "10.0.2.0/24".to_string(),
        }))
        .await
        .expect("adjacent subnet");
    client
        .delete_subnet(Request::new(DeleteSubnetRequest {
            meta: meta(lab, "req-6", "key-5"),
            subnet_id: "sub-d".to_string(),
        }))
        .await
        .expect("delete subnet");
    let err = client
        .delete_subnet(Request::new(DeleteSubnetRequest {
            meta: meta(lab, "req-7", "key-6"),
            subnet_id: "sub-d".to_string(),
        }))
        .await
        .expect_err("second delete must fail");
    assert_eq!(err.code(), Code::NotFound);

    // Topology shows the surviving subnet.
    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-8", "key-7"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    assert!(topo
        .nodes
        .iter()
        .any(|n| n.kind == "subnet" && n.name == "subnet-a"));
    assert!(!topo
        .nodes
        .iter()
        .any(|n| n.kind == "subnet" && n.name == "subnet-d"));
}

#[tokio::test]
async fn grpc_m4_veth_create_and_attach() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-veth";
    setup_network(&mut client, lab).await;
    client
        .create_namespace(Request::new(CreateNamespaceRequest {
            meta: meta(lab, "req-ns", "key-ns"),
            namespace: "ns-web".to_string(),
        }))
        .await
        .expect("create namespace");

    client
        .create_veth_pair(Request::new(CreateVethPairRequest {
            meta: meta(lab, "req-1", "key-1"),
            veth_id: "veth-1".to_string(),
            peer_a: "veth-a".to_string(),
            peer_b: "veth-b".to_string(),
            namespace_a: "ns-web".to_string(),
            namespace_b: String::new(),
        }))
        .await
        .expect("create veth pair");

    // Attaching a non-peer interface is rejected.
    let err = client
        .attach_interface(Request::new(AttachInterfaceRequest {
            meta: meta(lab, "req-2", "key-2"),
            veth_id: "veth-1".to_string(),
            interface: "eth0".to_string(),
            namespace: String::new(),
            bridge: String::new(),
        }))
        .await
        .expect_err("non-peer attach must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    // Attaching to a missing bridge is NotFound.
    let err = client
        .attach_interface(Request::new(AttachInterfaceRequest {
            meta: meta(lab, "req-3", "key-3"),
            veth_id: "veth-1".to_string(),
            interface: "veth-a".to_string(),
            namespace: String::new(),
            bridge: "br-missing".to_string(),
        }))
        .await
        .expect_err("missing bridge must fail");
    assert_eq!(err.code(), Code::NotFound);

    // Attach peer_a to the namespace (no bridge).
    client
        .attach_interface(Request::new(AttachInterfaceRequest {
            meta: meta(lab, "req-4", "key-4"),
            veth_id: "veth-1".to_string(),
            interface: "veth-a".to_string(),
            namespace: "ns-web".to_string(),
            bridge: String::new(),
        }))
        .await
        .expect("attach interface");

    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-5", "key-5"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    let veth = topo
        .nodes
        .iter()
        .find(|n| n.kind == "veth")
        .expect("veth node");
    assert_eq!(
        veth.labels.get("attached").map(String::as_str),
        Some("true")
    );
}

#[tokio::test]
async fn grpc_m4_route_add_and_conflict() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-route";
    setup_network(&mut client, lab).await;

    // Default route + more specific route: both accepted (LPM resolves
    // the overlap; the routing crate's unit tests assert the lookup).
    for (id, dest, gw, key) in [
        ("r-default", "0.0.0.0/0", "10.0.0.1", "key-1"),
        ("r-vpc", "10.0.0.0/16", "10.0.0.1", "key-2"),
        ("r-sub", "10.0.1.0/24", "10.0.0.2", "key-3"),
    ] {
        client
            .add_route(Request::new(AddRouteRequest {
                meta: meta(lab, "req-route", key),
                route_id: id.to_string(),
                destination: dest.to_string(),
                gateway: gw.to_string(),
                interface: "eth0".to_string(),
                metric: 100,
                namespace: String::new(),
            }))
            .await
            .unwrap_or_else(|e| panic!("add route {id}: {e}"));
    }

    // Same destination, different gateway: ROUTE_CONFLICT.
    let err = client
        .add_route(Request::new(AddRouteRequest {
            meta: meta(lab, "req-4", "key-4"),
            route_id: "r-clash".to_string(),
            destination: "10.0.1.0/24".to_string(),
            gateway: "10.0.0.9".to_string(),
            interface: "eth0".to_string(),
            metric: 100,
            namespace: String::new(),
        }))
        .await
        .expect_err("conflicting route must fail");
    assert_eq!(err.code(), Code::FailedPrecondition);
    assert!(
        err.message().starts_with("ROUTE_CONFLICT:"),
        "{}",
        err.message()
    );

    // Bad gateway is invalid.
    let err = client
        .add_route(Request::new(AddRouteRequest {
            meta: meta(lab, "req-5", "key-5"),
            route_id: "r-bad".to_string(),
            destination: "10.2.0.0/16".to_string(),
            gateway: "not-an-ip".to_string(),
            interface: "eth0".to_string(),
            metric: 100,
            namespace: String::new(),
        }))
        .await
        .expect_err("bad gateway must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    // Delete the specific route; the default remains.
    client
        .delete_route(Request::new(DeleteRouteRequest {
            meta: meta(lab, "req-6", "key-6"),
            route_id: "r-sub".to_string(),
        }))
        .await
        .expect("delete route");
    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-7", "key-7"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    assert_eq!(topo.nodes.iter().filter(|n| n.kind == "route").count(), 2);
}

#[tokio::test]
async fn grpc_m4_firewall_policy_apply_and_deny() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-fw";
    setup_network(&mut client, lab).await;

    // A policy with a bad rule (bad CIDR) is rejected at creation.
    let err = client
        .create_firewall_policy(Request::new(CreateFirewallPolicyRequest {
            meta: meta(lab, "req-0", "key-0"),
            policy_id: "pol-bad".to_string(),
            network_id: "net-a".to_string(),
            name: "bad".to_string(),
            rules: vec![FirewallRule {
                rule_id: "r1".to_string(),
                direction: FirewallDirection::Ingress as i32,
                protocol: FirewallProtocol::Tcp as i32,
                port_start: 22,
                port_end: 22,
                cidr: "not-a-cidr".to_string(),
                action: FirewallAction::Deny as i32,
                priority: 10,
            }],
        }))
        .await
        .expect_err("bad rule must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    // Deny SSH, allow everything else.
    client
        .create_firewall_policy(Request::new(CreateFirewallPolicyRequest {
            meta: meta(lab, "req-1", "key-1"),
            policy_id: "pol-1".to_string(),
            network_id: "net-a".to_string(),
            name: "web".to_string(),
            rules: vec![
                FirewallRule {
                    rule_id: "deny-ssh".to_string(),
                    direction: FirewallDirection::Ingress as i32,
                    protocol: FirewallProtocol::Tcp as i32,
                    port_start: 22,
                    port_end: 22,
                    cidr: "0.0.0.0/0".to_string(),
                    action: FirewallAction::Deny as i32,
                    priority: 10,
                },
                FirewallRule {
                    rule_id: "allow-all".to_string(),
                    direction: FirewallDirection::Ingress as i32,
                    protocol: FirewallProtocol::Any as i32,
                    port_start: 0,
                    port_end: 0,
                    cidr: "0.0.0.0/0".to_string(),
                    action: FirewallAction::Allow as i32,
                    priority: 100,
                },
            ],
        }))
        .await
        .expect("create policy");

    // Applying a missing policy is NotFound.
    let err = client
        .apply_firewall_policy(Request::new(ApplyFirewallPolicyRequest {
            meta: meta(lab, "req-2", "key-2"),
            policy_id: "pol-missing".to_string(),
        }))
        .await
        .expect_err("missing policy must fail");
    assert_eq!(err.code(), Code::NotFound);

    client
        .apply_firewall_policy(Request::new(ApplyFirewallPolicyRequest {
            meta: meta(lab, "req-3", "key-3"),
            policy_id: "pol-1".to_string(),
        }))
        .await
        .expect("apply policy");

    // The topology reports the policy as applied. The denied-packet
    // evaluation itself is asserted in the firewall crate's unit tests
    // (deny-ssh at priority 10 beats allow-all at 100) and in the
    // engine-core store tests against the stored policy.
    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-4", "key-4"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    let fw = topo
        .nodes
        .iter()
        .find(|n| n.kind == "firewall")
        .expect("firewall node");
    assert_eq!(fw.labels.get("applied").map(String::as_str), Some("true"));
    assert_eq!(fw.labels.get("rules").map(String::as_str), Some("2"));
}

#[tokio::test]
async fn grpc_m4_nat_create_delete() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-nat";
    setup_network(&mut client, lab).await;

    client
        .create_nat(Request::new(CreateNatRequest {
            meta: meta(lab, "req-1", "key-1"),
            nat_id: "nat-1".to_string(),
            kind: NatKind::Snat as i32,
            source_cidr: "10.0.0.0/16".to_string(),
            translated_address: "203.0.113.1".to_string(),
            interface: "eth0".to_string(),
        }))
        .await
        .expect("create NAT");

    // Duplicate NAT id is AlreadyExists.
    let err = client
        .create_nat(Request::new(CreateNatRequest {
            meta: meta(lab, "req-2", "key-2"),
            nat_id: "nat-1".to_string(),
            kind: NatKind::Snat as i32,
            source_cidr: "10.0.0.0/16".to_string(),
            translated_address: "203.0.113.2".to_string(),
            interface: "eth0".to_string(),
        }))
        .await
        .expect_err("duplicate NAT must fail");
    assert_eq!(err.code(), Code::AlreadyExists);

    // Bad translated address is invalid.
    let err = client
        .create_nat(Request::new(CreateNatRequest {
            meta: meta(lab, "req-3", "key-3"),
            nat_id: "nat-2".to_string(),
            kind: NatKind::Dnat as i32,
            source_cidr: "10.0.0.0/16".to_string(),
            translated_address: "not-an-ip".to_string(),
            interface: "eth0".to_string(),
        }))
        .await
        .expect_err("bad NAT address must fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-4", "key-4"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect")
        .into_inner();
    assert_eq!(topo.nodes.iter().filter(|n| n.kind == "nat").count(), 1);

    client
        .delete_nat(Request::new(DeleteNatRequest {
            meta: meta(lab, "req-5", "key-5"),
            nat_id: "nat-1".to_string(),
        }))
        .await
        .expect("delete NAT");
}

#[tokio::test]
async fn grpc_m4_fault_inject_and_telemetry() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");

    // Fresh lab telemetry is honest zeros (no lab state needed to read).
    let fresh = client
        .collect_telemetry(Request::new(CollectTelemetryRequest {
            meta: meta("lab-fresh", "req-0", "key-0"),
        }))
        .await
        .expect("fresh telemetry")
        .into_inner();
    assert!(!fresh.points.is_empty());
    assert!(fresh.points.iter().all(|p| p.value == 0.0));

    let lab = "lab-fault";
    setup_network(&mut client, lab).await;

    // Injecting into a lab with no resources is NotFound.
    let err = client
        .inject_fault(Request::new(InjectFaultRequest {
            meta: meta("lab-ghost", "req-x", "key-x"),
            fault: Some(FaultSpec {
                fault_id: "f-ghost".to_string(),
                kind: FaultKind::Latency as i32,
                magnitude: 100.0,
                duration_seconds: 60,
                target: String::new(),
            }),
        }))
        .await
        .expect_err("ghost lab must fail");
    assert_eq!(err.code(), Code::NotFound);

    // Out-of-range magnitude and over-long duration are rejected.
    for (mag, dur, key) in [(150.0, 60, "key-bad1"), (50.0, 3601, "key-bad2")] {
        let err = client
            .inject_fault(Request::new(InjectFaultRequest {
                meta: meta(lab, "req-bad", key),
                fault: Some(FaultSpec {
                    fault_id: "f-bad".to_string(),
                    kind: FaultKind::Loss as i32,
                    magnitude: mag,
                    duration_seconds: dur,
                    target: String::new(),
                }),
            }))
            .await
            .expect_err("invalid fault must fail");
        assert_eq!(err.code(), Code::InvalidArgument);
    }

    let injected = client
        .inject_fault(Request::new(InjectFaultRequest {
            meta: meta(lab, "req-1", "key-1"),
            fault: Some(FaultSpec {
                fault_id: "fault-1".to_string(),
                kind: FaultKind::Latency as i32,
                magnitude: 250.0,
                duration_seconds: 300,
                target: "net-a".to_string(),
            }),
        }))
        .await
        .expect("inject fault")
        .into_inner();
    assert_eq!(injected.fault_id, "fault-1");
    assert!(injected.expires_at_unix_seconds > 0);

    // Telemetry now shows real counters: operations ran and one fault
    // is active.
    let tele = client
        .collect_telemetry(Request::new(CollectTelemetryRequest {
            meta: meta(lab, "req-2", "key-2"),
        }))
        .await
        .expect("telemetry")
        .into_inner();
    let ops: f64 = tele
        .points
        .iter()
        .filter(|p| p.name == "operations_total")
        .map(|p| p.value)
        .sum();
    assert!(ops > 0.0, "operations_total should be > 0: {tele:?}");
    assert!(
        tele.points
            .iter()
            .any(|p| p.name == "active_faults" && p.value == 1.0),
        "active_faults should be 1: {tele:?}"
    );
    assert!(
        tele.points
            .iter()
            .any(|p| p.name == "faults_injected" && p.value == 1.0),
        "faults_injected should be 1: {tele:?}"
    );
}

#[tokio::test]
async fn grpc_m4_cleanup_removes_all_m4_state() {
    let endpoint = start_server().await;
    let mut client = NetworkEngineClient::connect(endpoint)
        .await
        .expect("connect");
    let lab = "lab-cleanup";
    setup_network(&mut client, lab).await;

    client
        .create_subnet(Request::new(CreateSubnetRequest {
            meta: meta(lab, "req-1", "key-1"),
            subnet_id: "sub-a".to_string(),
            network_id: "net-a".to_string(),
            name: "subnet-a".to_string(),
            cidr: "10.0.1.0/24".to_string(),
        }))
        .await
        .expect("subnet");
    client
        .create_veth_pair(Request::new(CreateVethPairRequest {
            meta: meta(lab, "req-2", "key-2"),
            veth_id: "veth-1".to_string(),
            peer_a: "veth-a".to_string(),
            peer_b: "veth-b".to_string(),
            namespace_a: String::new(),
            namespace_b: String::new(),
        }))
        .await
        .expect("veth");
    client
        .add_route(Request::new(AddRouteRequest {
            meta: meta(lab, "req-3", "key-3"),
            route_id: "r-1".to_string(),
            destination: "0.0.0.0/0".to_string(),
            gateway: "10.0.0.1".to_string(),
            interface: "eth0".to_string(),
            metric: 100,
            namespace: String::new(),
        }))
        .await
        .expect("route");
    client
        .create_firewall_policy(Request::new(CreateFirewallPolicyRequest {
            meta: meta(lab, "req-4", "key-4"),
            policy_id: "pol-1".to_string(),
            network_id: "net-a".to_string(),
            name: "web".to_string(),
            rules: vec![],
        }))
        .await
        .expect("policy");
    client
        .create_nat(Request::new(CreateNatRequest {
            meta: meta(lab, "req-5", "key-5"),
            nat_id: "nat-1".to_string(),
            kind: NatKind::Snat as i32,
            source_cidr: "10.0.0.0/16".to_string(),
            translated_address: "203.0.113.1".to_string(),
            interface: "eth0".to_string(),
        }))
        .await
        .expect("nat");
    client
        .inject_fault(Request::new(InjectFaultRequest {
            meta: meta(lab, "req-6", "key-6"),
            fault: Some(FaultSpec {
                fault_id: "fault-1".to_string(),
                kind: FaultKind::Loss as i32,
                magnitude: 10.0,
                duration_seconds: 60,
                target: String::new(),
            }),
        }))
        .await
        .expect("fault");

    let topo = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-7", "key-7"),
            network_id: String::new(),
        }))
        .await
        .expect("inspect before cleanup")
        .into_inner();
    for kind in ["subnet", "veth", "route", "firewall", "nat", "fault"] {
        assert!(
            topo.nodes.iter().any(|n| n.kind == kind),
            "expected a {kind} node before cleanup"
        );
    }

    let cleanup = client
        .cleanup_lab(Request::new(CleanupLabRequest {
            meta: meta(lab, "req-8", "key-8"),
        }))
        .await
        .expect("cleanup")
        .into_inner();
    assert!(cleanup.message.contains("cleaned up"));

    // Everything is gone: inspect fails and telemetry is zero again.
    let err = client
        .inspect_topology(Request::new(InspectTopologyRequest {
            meta: meta(lab, "req-9", "key-9"),
            network_id: String::new(),
        }))
        .await
        .expect_err("inspect after cleanup must fail");
    assert_eq!(err.code(), Code::NotFound);
    let tele = client
        .collect_telemetry(Request::new(CollectTelemetryRequest {
            meta: meta(lab, "req-10", "key-10"),
        }))
        .await
        .expect("telemetry after cleanup")
        .into_inner();
    assert!(tele.points.iter().all(|p| p.value == 0.0));

    // Health still reports the simulated backend.
    let health = client
        .health(Request::new(HealthRequest {}))
        .await
        .expect("health")
        .into_inner();
    assert!(health.ok);
    assert!(health.detail.contains("SIMULATED"), "{}", health.detail);
}
