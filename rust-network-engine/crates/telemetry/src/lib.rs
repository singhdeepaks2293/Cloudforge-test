//! In-memory counters and series per lab, with CollectTelemetry
//! aggregation.
//!
//! Std + serde only: no syscalls. Counters are real: the gRPC service
//! increments them when operations happen, and [`TelemetryStore::snapshot`]
//! returns exactly what was recorded — a fresh lab reports zero
//! counters, never fabricated traffic.
//!
//! A counter is keyed by `(lab, name, labels)`; [`TelemetryStore`] also
//! keeps a bounded ring of raw samples per `(lab, name)` for series
//! queries. Aggregation for `CollectTelemetry` is a snapshot of the
//! current counter values plus per-series latest values.

use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, HashMap, VecDeque};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

/// Maximum samples retained per (lab, name) series.
pub const MAX_SERIES_SAMPLES: usize = 1_000;

/// One telemetry sample.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct TelemetryPoint {
    pub name: String,
    pub value: f64,
    pub timestamp_unix_seconds: u64,
    pub labels: BTreeMap<String, String>,
}

impl TelemetryPoint {
    pub fn new(name: &str, value: f64, labels: BTreeMap<String, String>) -> Self {
        TelemetryPoint {
            name: name.to_string(),
            value,
            timestamp_unix_seconds: now_unix_seconds(),
            labels,
        }
    }
}

pub fn now_unix_seconds() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

/// Key for one counter: (lab, counter name, sorted label pairs).
type CounterKey = (String, String, Vec<(String, String)>);
/// Key for one series: (lab, series name).
type SeriesKey = (String, String);

#[derive(Debug, Default)]
struct Inner {
    counters: HashMap<CounterKey, f64>,
    series: HashMap<SeriesKey, VecDeque<TelemetryPoint>>,
}

/// Thread-safe per-lab telemetry store.
#[derive(Debug, Default)]
pub struct TelemetryStore {
    inner: Mutex<Inner>,
}

impl TelemetryStore {
    pub fn new() -> Self {
        Self::default()
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, Inner> {
        self.inner.lock().expect("telemetry store lock poisoned")
    }

    /// Increment a counter for a lab by `delta` (usually 1.0).
    pub fn increment(&self, lab: &str, name: &str, labels: &[(&str, &str)], delta: f64) {
        let mut inner = self.lock();
        let key_labels: Vec<(String, String)> = labels
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        *inner
            .counters
            .entry((lab.to_string(), name.to_string(), key_labels))
            .or_insert(0.0) += delta;
    }

    /// Set a gauge (latest-value) counter for a lab.
    pub fn set(&self, lab: &str, name: &str, labels: &[(&str, &str)], value: f64) {
        let mut inner = self.lock();
        let key_labels: Vec<(String, String)> = labels
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        inner
            .counters
            .insert((lab.to_string(), name.to_string(), key_labels), value);
    }

    /// Record a raw sample in the per-(lab, name) series (bounded ring).
    pub fn record(&self, lab: &str, point: TelemetryPoint) {
        let mut inner = self.lock();
        let series = inner
            .series
            .entry((lab.to_string(), point.name.clone()))
            .or_default();
        if series.len() >= MAX_SERIES_SAMPLES {
            series.pop_front();
        }
        series.push_back(point);
    }

    /// Current value of one counter, or 0.0 when never recorded.
    pub fn counter(&self, lab: &str, name: &str, labels: &[(&str, &str)]) -> f64 {
        let inner = self.lock();
        let key_labels: Vec<(String, String)> = labels
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        inner
            .counters
            .get(&(lab.to_string(), name.to_string(), key_labels))
            .copied()
            .unwrap_or(0.0)
    }

    /// Snapshot for `CollectTelemetry`: one point per recorded counter
    /// for the lab, plus a synthetic zero-valued `operations_total`
    /// point when the lab has recorded nothing yet, so a fresh lab
    /// honestly reports zero rather than an empty absence.
    pub fn snapshot(&self, lab: &str) -> Vec<TelemetryPoint> {
        let inner = self.lock();
        let mut points: Vec<TelemetryPoint> = inner
            .counters
            .iter()
            .filter(|((l, _, _), _)| l == lab)
            .map(|((_, name, labels), value)| TelemetryPoint {
                name: name.clone(),
                value: *value,
                timestamp_unix_seconds: now_unix_seconds(),
                labels: labels.iter().cloned().collect(),
            })
            .collect();
        if points.is_empty() {
            points.push(TelemetryPoint::new(
                "operations_total",
                0.0,
                BTreeMap::new(),
            ));
        }
        points.sort_by(|a, b| a.name.cmp(&b.name).then_with(|| a.labels.cmp(&b.labels)));
        points
    }

    /// Latest sample per series for a lab (empty for a fresh lab).
    pub fn latest_series(&self, lab: &str) -> Vec<TelemetryPoint> {
        let inner = self.lock();
        inner
            .series
            .iter()
            .filter(|((l, _), _)| l == lab)
            .filter_map(|(_, samples)| samples.back().cloned())
            .collect()
    }

    /// Drop all telemetry for a lab (called by lab cleanup). Returns
    /// the number of counter entries removed.
    pub fn purge_lab(&self, lab: &str) -> usize {
        let mut inner = self.lock();
        let before = inner.counters.len();
        inner.counters.retain(|(l, _, _), _| l != lab);
        inner.series.retain(|(l, _), _| l != lab);
        before - inner.counters.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counters_start_at_zero_and_increment() {
        let store = TelemetryStore::new();
        assert_eq!(store.counter("lab-1", "operations_total", &[]), 0.0);
        store.increment("lab-1", "operations_total", &[], 1.0);
        store.increment("lab-1", "operations_total", &[], 1.0);
        assert_eq!(store.counter("lab-1", "operations_total", &[]), 2.0);
        // Labels partition counters; another lab is independent.
        store.increment(
            "lab-1",
            "operations_total",
            &[("op", "create_network")],
            1.0,
        );
        assert_eq!(store.counter("lab-1", "operations_total", &[]), 2.0);
        assert_eq!(
            store.counter("lab-1", "operations_total", &[("op", "create_network")]),
            1.0
        );
        assert_eq!(store.counter("lab-2", "operations_total", &[]), 0.0);
    }

    #[test]
    fn snapshot_returns_recorded_counters_and_zero_for_fresh_lab() {
        let store = TelemetryStore::new();
        let fresh = store.snapshot("lab-fresh");
        assert_eq!(fresh.len(), 1);
        assert_eq!(fresh[0].name, "operations_total");
        assert_eq!(fresh[0].value, 0.0);

        store.increment("lab-1", "subnets_created", &[], 2.0);
        store.increment("lab-1", "operations_total", &[("op", "create_subnet")], 2.0);
        let snap = store.snapshot("lab-1");
        assert_eq!(snap.len(), 2);
        assert!(snap
            .iter()
            .any(|p| p.name == "subnets_created" && p.value == 2.0));
        // No cross-lab leakage.
        assert!(!snap.iter().any(|p| p.labels.contains_key("lab")));
        assert_eq!(store.snapshot("lab-2").len(), 1);
    }

    #[test]
    fn series_ring_is_bounded_and_latest_works() {
        let store = TelemetryStore::new();
        for i in 0..MAX_SERIES_SAMPLES + 10 {
            store.record(
                "lab-1",
                TelemetryPoint::new("latency_ms", i as f64, BTreeMap::new()),
            );
        }
        let latest = store.latest_series("lab-1");
        assert_eq!(latest.len(), 1);
        assert_eq!(latest[0].value, (MAX_SERIES_SAMPLES + 9) as f64);
    }

    #[test]
    fn purge_lab_removes_only_that_lab() {
        let store = TelemetryStore::new();
        store.increment("lab-1", "a", &[], 1.0);
        store.increment("lab-2", "a", &[], 1.0);
        store.record("lab-1", TelemetryPoint::new("s", 1.0, BTreeMap::new()));
        assert_eq!(store.purge_lab("lab-1"), 1);
        assert_eq!(store.counter("lab-1", "a", &[]), 0.0);
        assert_eq!(store.counter("lab-2", "a", &[]), 1.0);
        assert!(store.latest_series("lab-1").is_empty());
    }

    #[test]
    fn point_serializes_with_serde() {
        let p = TelemetryPoint::new("x", 3.5, BTreeMap::new());
        let json = serde_json::to_string(&p).unwrap();
        let back: TelemetryPoint = serde_json::from_str(&json).unwrap();
        assert_eq!(back, p);
    }
}
