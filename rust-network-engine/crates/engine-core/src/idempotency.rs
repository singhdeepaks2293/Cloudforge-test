//! Idempotency store.
//!
//! Infrastructure operations carry an idempotency key. A retry that
//! replays the same `(lab_id, idempotency_key)` must return the stored
//! outcome instead of executing the operation a second time —
//! re-creating a network or re-allocating an address would leak
//! resources.
//!
//! The store is deliberately transport-agnostic: it keeps the outcome
//! plus an optional opaque payload string (e.g. the allocated address
//! or the created network id) so each gRPC handler can rebuild its own
//! response type from the stored entry. Entries are scoped per lab; a
//! lab cleanup purges that lab's keys.

use crate::types::OperationResult;
use std::collections::HashMap;
use std::sync::Mutex;

/// What a previous execution of an idempotent operation returned.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StoredOutcome {
    pub result: OperationResult,
    /// Handler-specific payload, e.g. allocated address / network id.
    pub payload: Option<String>,
}

#[derive(Debug, Default)]
pub struct IdempotencyStore {
    inner: Mutex<HashMap<(String, String), StoredOutcome>>,
}

impl IdempotencyStore {
    pub fn new() -> Self {
        Self::default()
    }

    /// Look up a previous outcome for `(lab_id, idempotency_key)`.
    pub fn get(&self, lab_id: &str, key: &str) -> Option<StoredOutcome> {
        self.inner
            .lock()
            .expect("idempotency store lock poisoned")
            .get(&(lab_id.to_string(), key.to_string()))
            .cloned()
    }

    /// Record an outcome. Overwriting the same key with a different
    /// outcome is a caller bug; the first outcome wins.
    pub fn insert(&self, lab_id: &str, key: &str, outcome: StoredOutcome) {
        self.inner
            .lock()
            .expect("idempotency store lock poisoned")
            .entry((lab_id.to_string(), key.to_string()))
            .or_insert(outcome);
    }

    /// Forget every key belonging to a lab (called by lab cleanup).
    /// Returns the number of entries removed.
    pub fn purge_lab(&self, lab_id: &str) -> usize {
        let mut inner = self.inner.lock().expect("idempotency store lock poisoned");
        let before = inner.len();
        inner.retain(|(lab, _), _| lab != lab_id);
        before - inner.len()
    }

    pub fn len(&self) -> usize {
        self.inner
            .lock()
            .expect("idempotency store lock poisoned")
            .len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::OperationState;

    fn outcome(id: &str) -> StoredOutcome {
        StoredOutcome {
            result: OperationResult {
                operation_id: id.to_string(),
                state: OperationState::Succeeded,
                message: "ok".to_string(),
                error_code: None,
                duration_ms: 1,
            },
            payload: Some("10.0.0.5".to_string()),
        }
    }

    #[test]
    fn duplicate_key_returns_stored_result_and_first_wins() {
        let store = IdempotencyStore::new();
        assert!(store.get("lab-1", "key-1").is_none());
        store.insert("lab-1", "key-1", outcome("op-1"));
        assert_eq!(store.get("lab-1", "key-1").unwrap(), outcome("op-1"));
        // A second insert under the same key must not overwrite.
        store.insert("lab-1", "key-1", outcome("op-2"));
        assert_eq!(
            store.get("lab-1", "key-1").unwrap().result.operation_id,
            "op-1"
        );
        // Same key in a different lab is independent.
        assert!(store.get("lab-2", "key-1").is_none());
    }

    #[test]
    fn purge_lab_removes_only_that_lab() {
        let store = IdempotencyStore::new();
        store.insert("lab-1", "k1", outcome("op-1"));
        store.insert("lab-1", "k2", outcome("op-2"));
        store.insert("lab-2", "k1", outcome("op-3"));
        assert_eq!(store.purge_lab("lab-1"), 2);
        assert!(store.get("lab-1", "k1").is_none());
        assert!(store.get("lab-2", "k1").is_some());
        assert_eq!(store.len(), 1);
    }
}
