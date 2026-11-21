//! Workspace-level integration tests.
//!
//! The per-crate unit tests live next to the code they cover
//! (`crates/ipam`, `crates/engine-core`); the gRPC end-to-end smoke
//! test lives in `crates/grpc-server/tests/grpc_smoke.rs`. This file
//! pins cross-crate behaviour: the store and the IPAM pool must agree
//! about exhaustion, and idempotency must survive a full lab cleanup
//! cycle in the expected way.

use engine_core::{IdempotencyStore, OperationResult, StoredOutcome, TopologyStore};
use engine_core::{LabId, OperationState};

fn lab(id: &str) -> LabId {
    LabId::new(id).unwrap()
}

#[test]
fn store_and_ipam_agree_on_exhaustion() {
    let store = TopologyStore::new();
    let lab = lab("lab-x");
    store
        .create_network(&lab, "net-a", "vpc-a", "10.0.0.0/30")
        .unwrap();
    assert!(store.allocate_address(&lab, "net-a", "").is_ok());
    assert!(store.allocate_address(&lab, "net-a", "").is_ok());
    let err = store.allocate_address(&lab, "net-a", "").unwrap_err();
    assert_eq!(err.error_code(), "IPAM_EXHAUSTED");
}

#[test]
fn cleanup_purges_idempotency_keys_for_that_lab_only() {
    let store = TopologyStore::new();
    let idem = IdempotencyStore::new();
    let lab_a = lab("lab-a");
    let lab_b = lab("lab-b");
    store
        .create_network(&lab_a, "net-a", "vpc-a", "10.0.0.0/16")
        .unwrap();
    store
        .create_network(&lab_b, "net-b", "vpc-b", "10.0.0.0/16")
        .unwrap();

    let outcome = StoredOutcome {
        result: OperationResult {
            operation_id: "op-1".to_string(),
            state: OperationState::Succeeded,
            message: "ok".to_string(),
            error_code: None,
            duration_ms: 0,
        },
        payload: None,
    };
    idem.insert("lab-a", "k1", outcome.clone());
    idem.insert("lab-b", "k1", outcome);

    assert_eq!(store.cleanup_lab(&lab_a), 1);
    assert_eq!(idem.purge_lab("lab-a"), 1);
    assert!(idem.get("lab-a", "k1").is_none());
    assert!(idem.get("lab-b", "k1").is_some());
    assert!(store.lab_exists(&lab_b));
}
