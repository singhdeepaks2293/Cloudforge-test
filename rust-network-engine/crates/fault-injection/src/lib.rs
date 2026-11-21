//! Fault-spec validation, active-fault registry with expiry, and a
//! traffic-effect model.
//!
//! Std + serde only: no syscalls. This crate models what a fault *does*
//! to traffic; actually dropping/delaying packets on the wire is the
//! kernel backend's job (tc/netem in the real backend — not wired in
//! M4, see `engine-core::real`). The simulated path applies the effect
//! model to synthetic traffic accounting instead of pretending to
//! touch packets.
//!
//! Fault kinds and magnitude ranges:
//! * `Latency` — added latency in milliseconds, `0..=10000`.
//! * `Loss` — packet-loss percent, `0..=100`.
//! * `Partition` — `0` (healed) or `1` (partitioned).
//! * `Dns` — DNS failure percent, `0..=100`.
//! * `RouteBlackhole` — `0` (healed) or `1` (blackholed).
//!
//! Duration is bounded to `1..=3600` seconds (see `MAX_DURATION_SECS`).
//! Faults expire: [`FaultRegistry::active`] prunes expired entries, so
//! a fault can never linger past its declared duration.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

/// Maximum fault duration: one hour.
pub const MAX_DURATION_SECS: u64 = 3600;

/// Fault kinds, mirroring `cloudforge.v1.FaultKind`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum FaultKind {
    Latency,
    Loss,
    Partition,
    Dns,
    RouteBlackhole,
}

impl fmt::Display for FaultKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            FaultKind::Latency => "latency",
            FaultKind::Loss => "loss",
            FaultKind::Partition => "partition",
            FaultKind::Dns => "dns",
            FaultKind::RouteBlackhole => "route-blackhole",
        };
        f.write_str(s)
    }
}

impl FaultKind {
    /// Magnitude range for this kind (inclusive).
    pub fn magnitude_range(&self) -> (f64, f64) {
        match self {
            FaultKind::Latency => (0.0, 10_000.0),
            FaultKind::Loss => (0.0, 100.0),
            FaultKind::Partition => (0.0, 1.0),
            FaultKind::Dns => (0.0, 100.0),
            FaultKind::RouteBlackhole => (0.0, 1.0),
        }
    }

    /// Human-readable magnitude unit, for messages.
    pub fn magnitude_unit(&self) -> &'static str {
        match self {
            FaultKind::Latency => "ms",
            FaultKind::Loss | FaultKind::Dns => "%",
            FaultKind::Partition | FaultKind::RouteBlackhole => "0/1",
        }
    }
}

/// A validated fault specification.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FaultSpec {
    pub fault_id: String,
    pub kind: FaultKind,
    pub magnitude: f64,
    pub duration_seconds: u64,
    /// Affected resource (network / namespace / interface); "" = whole lab.
    pub target: String,
}

impl FaultSpec {
    pub fn new(
        fault_id: &str,
        kind: FaultKind,
        magnitude: f64,
        duration_seconds: u64,
        target: &str,
    ) -> Self {
        FaultSpec {
            fault_id: fault_id.to_string(),
            kind,
            magnitude,
            duration_seconds,
            target: target.to_string(),
        }
    }

    /// Validate id, magnitude range for the kind, and duration bounds.
    pub fn validate(&self) -> Result<(), String> {
        if self.fault_id.trim().is_empty() {
            return Err("fault_id must not be empty".to_string());
        }
        let (lo, hi) = self.kind.magnitude_range();
        if !(lo..=hi).contains(&self.magnitude) {
            return Err(format!(
                "fault {}: magnitude {} {} out of range [{}, {}] for kind {}",
                self.fault_id,
                self.magnitude,
                self.kind.magnitude_unit(),
                lo,
                hi,
                self.kind
            ));
        }
        if !self.magnitude.is_finite() {
            return Err(format!("fault {}: magnitude must be finite", self.fault_id));
        }
        // Partition / blackhole are binary: 0 (off) or 1 (on).
        if matches!(self.kind, FaultKind::Partition | FaultKind::RouteBlackhole)
            && self.magnitude != 0.0
            && self.magnitude != 1.0
        {
            return Err(format!(
                "fault {}: magnitude for kind {} must be 0 or 1, got {}",
                self.fault_id, self.kind, self.magnitude
            ));
        }
        if self.duration_seconds == 0 || self.duration_seconds > MAX_DURATION_SECS {
            return Err(format!(
                "fault {}: duration_seconds {} out of range [1, {}]",
                self.fault_id, self.duration_seconds, MAX_DURATION_SECS
            ));
        }
        Ok(())
    }
}

/// What a set of active faults does to traffic aimed at a target.
///
/// The model is deliberately simple and honest: latency faults add a
/// fixed delay, loss faults drop a fraction, partition/blackhole
/// faults drop everything for the affected target.
#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
pub struct TrafficEffect {
    /// Added one-way latency in milliseconds.
    pub added_latency_ms: f64,
    /// Fraction of packets lost, `0.0..=1.0`.
    pub loss_fraction: f64,
    /// Fraction of DNS queries that fail, `0.0..=1.0`.
    pub dns_failure_fraction: f64,
    /// True when the target is fully unreachable.
    pub unreachable: bool,
}

impl Default for TrafficEffect {
    fn default() -> Self {
        TrafficEffect {
            added_latency_ms: 0.0,
            loss_fraction: 0.0,
            dns_failure_fraction: 0.0,
            unreachable: false,
        }
    }
}

impl TrafficEffect {
    /// Combine two effects (e.g. two faults on the same target):
    /// latencies add, loss compounds (`1 - (1-a)(1-b)`), unreachability
    /// dominates.
    pub fn combine(self, other: TrafficEffect) -> TrafficEffect {
        TrafficEffect {
            added_latency_ms: self.added_latency_ms + other.added_latency_ms,
            loss_fraction: 1.0 - (1.0 - self.loss_fraction) * (1.0 - other.loss_fraction),
            dns_failure_fraction: 1.0
                - (1.0 - self.dns_failure_fraction) * (1.0 - other.dns_failure_fraction),
            unreachable: self.unreachable || other.unreachable,
        }
    }
}

/// One active fault, with its expiry.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ActiveFault {
    pub spec: FaultSpec,
    /// Wall-clock expiry, unix seconds (for API responses).
    pub expires_at_unix_seconds: u64,
    #[serde(skip)]
    pub expires_at: Option<Instant>,
}

impl ActiveFault {
    pub fn is_expired(&self) -> bool {
        match self.expires_at {
            Some(t) => Instant::now() >= t,
            None => false,
        }
    }
}

/// Registry of active faults. Expiry is enforced on every read, so
/// callers never observe an expired fault.
#[derive(Debug, Default)]
pub struct FaultRegistry {
    faults: HashMap<String, ActiveFault>,
}

impl FaultRegistry {
    pub fn new() -> Self {
        Self::default()
    }

    /// Insert a validated fault. Returns the stored entry (with expiry)
    /// or a validation / duplicate error.
    pub fn inject(&mut self, spec: FaultSpec) -> Result<ActiveFault, String> {
        spec.validate()?;
        if self.faults.contains_key(&spec.fault_id) {
            return Err(format!("fault {} already exists", spec.fault_id));
        }
        let now_unix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);
        let active = ActiveFault {
            expires_at_unix_seconds: now_unix + spec.duration_seconds,
            expires_at: Some(Instant::now() + Duration::from_secs(spec.duration_seconds)),
            spec,
        };
        self.faults
            .insert(active.spec.fault_id.clone(), active.clone());
        Ok(active)
    }

    /// Remove expired faults. Returns how many were pruned.
    pub fn prune_expired(&mut self) -> usize {
        let before = self.faults.len();
        self.faults.retain(|_, f| !f.is_expired());
        before - self.faults.len()
    }

    /// All currently active (unexpired) faults.
    pub fn active(&mut self) -> Vec<ActiveFault> {
        self.prune_expired();
        let mut v: Vec<ActiveFault> = self.faults.values().cloned().collect();
        v.sort_by(|a, b| a.spec.fault_id.cmp(&b.spec.fault_id));
        v
    }

    /// Active faults without pruning (usable from `&self`, e.g. for
    /// topology inspection; expired entries are filtered out).
    pub fn active_snapshot(&self) -> Vec<ActiveFault> {
        let mut v: Vec<ActiveFault> = self
            .faults
            .values()
            .filter(|f| !f.is_expired())
            .cloned()
            .collect();
        v.sort_by(|a, b| a.spec.fault_id.cmp(&b.spec.fault_id));
        v
    }

    pub fn get(&mut self, fault_id: &str) -> Option<ActiveFault> {
        self.prune_expired();
        self.faults.get(fault_id).cloned()
    }

    pub fn remove(&mut self, fault_id: &str) -> Option<ActiveFault> {
        self.faults.remove(fault_id)
    }

    pub fn len(&mut self) -> usize {
        self.prune_expired();
        self.faults.len()
    }

    /// Number of unexpired faults, without pruning (usable from `&self`).
    pub fn active_count(&self) -> usize {
        self.faults.values().filter(|f| !f.is_expired()).count()
    }

    pub fn is_empty(&mut self) -> bool {
        self.len() == 0
    }

    /// Combined traffic effect for `target`. A fault applies when its
    /// target is "" (whole lab) or exactly matches `target`.
    pub fn effect_for(&mut self, target: &str) -> TrafficEffect {
        let mut effect = TrafficEffect::default();
        for fault in self.active() {
            if !fault.spec.target.is_empty() && fault.spec.target != target {
                continue;
            }
            let one = match fault.spec.kind {
                FaultKind::Latency => TrafficEffect {
                    added_latency_ms: fault.spec.magnitude,
                    ..Default::default()
                },
                FaultKind::Loss => TrafficEffect {
                    loss_fraction: fault.spec.magnitude / 100.0,
                    ..Default::default()
                },
                FaultKind::Partition | FaultKind::RouteBlackhole => {
                    if fault.spec.magnitude >= 1.0 {
                        TrafficEffect {
                            unreachable: true,
                            ..Default::default()
                        }
                    } else {
                        TrafficEffect::default()
                    }
                }
                FaultKind::Dns => TrafficEffect {
                    dns_failure_fraction: fault.spec.magnitude / 100.0,
                    ..Default::default()
                },
            };
            effect = effect.combine(one);
        }
        effect
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    fn spec(id: &str, kind: FaultKind, mag: f64, dur: u64) -> FaultSpec {
        FaultSpec::new(id, kind, mag, dur, "")
    }

    #[test]
    fn validation_accepts_every_kind_in_range() {
        for (kind, mag) in [
            (FaultKind::Latency, 250.0),
            (FaultKind::Loss, 50.0),
            (FaultKind::Partition, 1.0),
            (FaultKind::Dns, 100.0),
            (FaultKind::RouteBlackhole, 0.0),
        ] {
            assert!(spec("f1", kind, mag, 60).validate().is_ok(), "{kind:?}");
        }
    }

    #[test]
    fn validation_rejects_out_of_range_magnitude_and_duration() {
        assert!(spec("f1", FaultKind::Latency, 10_001.0, 60)
            .validate()
            .is_err());
        assert!(spec("f1", FaultKind::Loss, 101.0, 60).validate().is_err());
        assert!(spec("f1", FaultKind::Loss, -1.0, 60).validate().is_err());
        assert!(spec("f1", FaultKind::Partition, 0.5, 60)
            .validate()
            .is_err());
        assert!(spec("f1", FaultKind::Latency, 10.0, 0).validate().is_err());
        assert!(spec("f1", FaultKind::Latency, 10.0, MAX_DURATION_SECS + 1)
            .validate()
            .is_err());
        assert!(spec("f1", FaultKind::Latency, 10.0, MAX_DURATION_SECS)
            .validate()
            .is_ok());
        assert!(spec("", FaultKind::Latency, 10.0, 60).validate().is_err());
        assert!(spec("f1", FaultKind::Latency, f64::NAN, 60)
            .validate()
            .is_err());
    }

    #[test]
    fn registry_inject_and_expiry() {
        let mut reg = FaultRegistry::new();
        let active = reg.inject(spec("f1", FaultKind::Loss, 25.0, 3600)).unwrap();
        assert!(active.expires_at_unix_seconds > 0);
        assert_eq!(reg.len(), 1);
        // Duplicate id is rejected.
        assert!(reg.inject(spec("f1", FaultKind::Loss, 25.0, 60)).is_err());
        assert_eq!(reg.len(), 1);
    }

    #[test]
    fn expired_faults_are_pruned() {
        let mut reg = FaultRegistry::new();
        // Inject with a 1s duration, then fake expiry by removing and
        // re-inserting an already-expired entry via the registry's own
        // expiry path: sleep is avoided by testing `is_expired`
        // indirectly — inject, then check prune with a 1s fault after
        // a short real wait is flaky, so instead verify that a fault
        // with duration 1 is active immediately and that `prune_expired`
        // on a fresh registry prunes nothing.
        reg.inject(spec("f1", FaultKind::Latency, 5.0, 1)).unwrap();
        assert_eq!(reg.prune_expired(), 0);
        assert_eq!(reg.active().len(), 1);
        std::thread::sleep(Duration::from_millis(1100));
        assert_eq!(reg.prune_expired(), 1);
        assert!(reg.active().is_empty());
    }

    #[test]
    fn traffic_effect_model() {
        let mut reg = FaultRegistry::new();
        reg.inject(spec("lat", FaultKind::Latency, 100.0, 60))
            .unwrap();
        reg.inject(spec("loss", FaultKind::Loss, 50.0, 60)).unwrap();
        let effect = reg.effect_for("");
        assert_eq!(effect.added_latency_ms, 100.0);
        assert!((effect.loss_fraction - 0.5).abs() < f64::EPSILON);
        assert!(!effect.unreachable);

        // Two loss faults compound: 1 - 0.5*0.5 = 0.75.
        reg.inject(spec("loss2", FaultKind::Loss, 50.0, 60))
            .unwrap();
        assert!((reg.effect_for("").loss_fraction - 0.75).abs() < f64::EPSILON);

        // Partition makes the target unreachable.
        reg.inject(FaultSpec::new(
            "part",
            FaultKind::Partition,
            1.0,
            60,
            "net-a",
        ))
        .unwrap();
        assert!(reg.effect_for("net-a").unreachable);
        assert!(!reg.effect_for("net-b").unreachable);
        // A lab-wide fault applies to every target.
        assert!(reg.effect_for("anything").loss_fraction > 0.0);
    }

    #[test]
    fn dns_fault_only_affects_dns() {
        let mut reg = FaultRegistry::new();
        reg.inject(spec("dns", FaultKind::Dns, 100.0, 60)).unwrap();
        let effect = reg.effect_for("");
        assert_eq!(effect.dns_failure_fraction, 1.0);
        assert_eq!(effect.loss_fraction, 0.0);
        assert!(!effect.unreachable);
    }

    #[test]
    fn spec_serializes_with_serde() {
        let s = spec("f1", FaultKind::Latency, 42.5, 300);
        let json = serde_json::to_string(&s).unwrap();
        let back: FaultSpec = serde_json::from_str(&json).unwrap();
        assert_eq!(back, s);
    }
}
