//! Pure security-group rule evaluation.
//!
//! This crate has no syscalls and no network access: it answers one
//! question — "is this packet allowed?" — from a policy and a packet
//! description. The kernel (nftables) enforcement of the same policy
//! lives in the `RealNetlinkBackend`; the simulated backend enforces
//! nothing at the kernel level and relies on this evaluation for
//! validation and tests.
//!
//! Semantics (security-group style, default-deny):
//! * A packet matches a rule when direction, protocol, port and remote
//!   CIDR all match. `Any` protocol, port range `0/0` and CIDR
//!   `0.0.0.0/0` are wildcards.
//! * Rules are evaluated in priority order (lower `priority` first).
//!   The first matching rule decides.
//! * Ties on priority are broken **deny before allow**: an explicit
//!   deny at the same priority as an allow wins.
//! * No matching rule => [`Decision::Deny`] (default-deny).
//! * [`Policy::validate`] rejects malformed rules (bad CIDR, port
//!   range outside 0..=65535, `port_start > port_end` unless both are
//!   0, empty rule id, duplicate rule ids).

use ipam::Cidr;
use serde::{Deserialize, Serialize};
use std::fmt;
use std::net::Ipv4Addr;
use std::str::FromStr;

/// Packet direction relative to the protected resource.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Direction {
    Ingress,
    Egress,
}

/// L4 protocol. `Any` matches every protocol.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Protocol {
    Any,
    Tcp,
    Udp,
    Icmp,
}

/// Rule verdict.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Action {
    Allow,
    Deny,
}

/// Allow / deny outcome of evaluating one packet against a policy.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Decision {
    Allow,
    Deny,
}

impl fmt::Display for Decision {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Decision::Allow => f.write_str("allow"),
            Decision::Deny => f.write_str("deny"),
        }
    }
}

/// One packet, as seen by the firewall. `remote` is the peer address
/// (source for ingress, destination for egress); `port` is the
/// destination port for TCP/UDP and is ignored for ICMP / `Any`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Packet {
    pub direction: Direction,
    pub protocol: Protocol,
    pub remote: Ipv4Addr,
    pub port: u16,
}

impl Packet {
    pub fn new(direction: Direction, protocol: Protocol, remote: Ipv4Addr, port: u16) -> Self {
        Packet {
            direction,
            protocol,
            remote,
            port,
        }
    }
}

/// One firewall rule.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Rule {
    pub rule_id: String,
    pub direction: Direction,
    pub protocol: Protocol,
    /// Inclusive port range. `(0, 0)` means "any port".
    pub port_start: u16,
    pub port_end: u16,
    /// Remote CIDR the rule matches. `0.0.0.0/0` matches any address.
    pub cidr: String,
    pub action: Action,
    /// Lower wins; ties are broken deny-before-allow.
    pub priority: i32,
}

impl Rule {
    /// Validate one rule in isolation (CIDR, ports, id).
    pub fn validate(&self) -> Result<(), String> {
        if self.rule_id.trim().is_empty() {
            return Err("rule_id must not be empty".to_string());
        }
        if self.port_start > self.port_end && !(self.port_start == 0 && self.port_end == 0) {
            return Err(format!(
                "rule {}: port_start {} > port_end {}",
                self.rule_id, self.port_start, self.port_end
            ));
        }
        Cidr::parse(&self.cidr)
            .map_err(|e| format!("rule {}: invalid cidr {}: {e}", self.rule_id, self.cidr))?;
        Ok(())
    }

    /// Does this rule match the packet? Protocol `Any` and port `0/0`
    /// are wildcards; ICMP rules ignore the port.
    pub fn matches(&self, packet: &Packet) -> bool {
        if self.direction != packet.direction {
            return false;
        }
        if self.protocol != Protocol::Any && self.protocol != packet.protocol {
            return false;
        }
        if self.protocol != Protocol::Icmp
            && packet.protocol != Protocol::Icmp
            && !(self.port_start == 0 && self.port_end == 0)
            && (packet.port < self.port_start || packet.port > self.port_end)
        {
            return false;
        }
        match Cidr::parse(&self.cidr) {
            Ok(cidr) => cidr.contains_addr(packet.remote),
            Err(_) => false, // Unvalidated rule: never match silently.
        }
    }
}

/// A firewall policy: an ordered set of rules applied to a network.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Policy {
    pub policy_id: String,
    pub network_id: String,
    pub name: String,
    pub rules: Vec<Rule>,
    pub applied: bool,
}

impl Policy {
    pub fn new(policy_id: &str, network_id: &str, name: &str, rules: Vec<Rule>) -> Self {
        Policy {
            policy_id: policy_id.to_string(),
            network_id: network_id.to_string(),
            name: name.to_string(),
            rules,
            applied: false,
        }
    }

    /// Validate the policy: non-empty ids and every rule valid, with
    /// no duplicate rule ids.
    pub fn validate(&self) -> Result<(), String> {
        if self.policy_id.trim().is_empty() {
            return Err("policy_id must not be empty".to_string());
        }
        if self.network_id.trim().is_empty() {
            return Err("network_id must not be empty".to_string());
        }
        let mut seen = std::collections::HashSet::new();
        for rule in &self.rules {
            rule.validate()?;
            if !seen.insert(rule.rule_id.clone()) {
                return Err(format!("duplicate rule_id {}", rule.rule_id));
            }
        }
        Ok(())
    }

    /// Evaluate a packet. Rules are considered in priority order
    /// (lower first); at equal priority a deny is considered before an
    /// allow. The first matching rule decides; no match => deny.
    pub fn evaluate(&self, packet: &Packet) -> Decision {
        let mut ordered: Vec<&Rule> = self.rules.iter().collect();
        ordered.sort_by(|a, b| {
            a.priority
                .cmp(&b.priority)
                .then_with(|| match (a.action, b.action) {
                    (Action::Deny, Action::Allow) => std::cmp::Ordering::Less,
                    (Action::Allow, Action::Deny) => std::cmp::Ordering::Greater,
                    _ => std::cmp::Ordering::Equal,
                })
        });
        for rule in ordered {
            if rule.matches(packet) {
                return match rule.action {
                    Action::Allow => Decision::Allow,
                    Action::Deny => Decision::Deny,
                };
            }
        }
        Decision::Deny
    }

    /// The rule that decided a packet, if any (for diagnostics).
    pub fn deciding_rule(&self, packet: &Packet) -> Option<&Rule> {
        let mut ordered: Vec<&Rule> = self.rules.iter().collect();
        ordered.sort_by(|a, b| {
            a.priority
                .cmp(&b.priority)
                .then_with(|| match (a.action, b.action) {
                    (Action::Deny, Action::Allow) => std::cmp::Ordering::Less,
                    (Action::Allow, Action::Deny) => std::cmp::Ordering::Greater,
                    _ => std::cmp::Ordering::Equal,
                })
        });
        ordered.into_iter().find(|r| r.matches(packet))
    }
}

/// Parse a dotted IPv4 address, returning a readable error.
pub fn parse_ipv4(s: &str) -> Result<Ipv4Addr, String> {
    Ipv4Addr::from_str(s).map_err(|_| format!("invalid IPv4 address: {s}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(s: &str) -> Ipv4Addr {
        s.parse().unwrap()
    }

    fn allow_rule(id: &str, port: u16, priority: i32) -> Rule {
        Rule {
            rule_id: id.to_string(),
            direction: Direction::Ingress,
            protocol: Protocol::Tcp,
            port_start: port,
            port_end: port,
            cidr: "0.0.0.0/0".to_string(),
            action: Action::Allow,
            priority,
        }
    }

    #[test]
    fn default_deny_with_no_rules() {
        let policy = Policy::new("p1", "net-a", "empty", vec![]);
        let pkt = Packet::new(Direction::Ingress, Protocol::Tcp, addr("10.0.0.5"), 80);
        assert_eq!(policy.evaluate(&pkt), Decision::Deny);
    }

    #[test]
    fn allow_rule_permits_matching_packet() {
        let policy = Policy::new("p1", "net-a", "web", vec![allow_rule("r1", 80, 100)]);
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("1.2.3.4"),
                80
            )),
            Decision::Allow
        );
        // Wrong port, wrong direction, wrong protocol: all denied.
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("1.2.3.4"),
                22
            )),
            Decision::Deny
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Egress,
                Protocol::Tcp,
                addr("1.2.3.4"),
                80
            )),
            Decision::Deny
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Udp,
                addr("1.2.3.4"),
                80
            )),
            Decision::Deny
        );
    }

    #[test]
    fn lower_priority_wins_over_higher_priority() {
        // Allow-all at priority 200, deny-ssh at priority 100: ssh is
        // denied even though an allow also matches.
        let policy = Policy::new(
            "p1",
            "net-a",
            "prec",
            vec![
                Rule {
                    rule_id: "allow-all".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Any,
                    port_start: 0,
                    port_end: 0,
                    cidr: "0.0.0.0/0".to_string(),
                    action: Action::Allow,
                    priority: 200,
                },
                Rule {
                    rule_id: "deny-ssh".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Tcp,
                    port_start: 22,
                    port_end: 22,
                    cidr: "0.0.0.0/0".to_string(),
                    action: Action::Deny,
                    priority: 100,
                },
            ],
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("9.9.9.9"),
                22
            )),
            Decision::Deny
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("9.9.9.9"),
                80
            )),
            Decision::Allow
        );
        // Reversed insertion order must not change the outcome.
        let mut reversed = policy.clone();
        reversed.rules.reverse();
        assert_eq!(
            reversed.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("9.9.9.9"),
                22
            )),
            Decision::Deny
        );
    }

    #[test]
    fn same_priority_deny_beats_allow() {
        let policy = Policy::new(
            "p1",
            "net-a",
            "tie",
            vec![
                allow_rule("allow-80", 80, 100),
                Rule {
                    rule_id: "deny-80".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Tcp,
                    port_start: 80,
                    port_end: 80,
                    cidr: "0.0.0.0/0".to_string(),
                    action: Action::Deny,
                    priority: 100,
                },
            ],
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("1.1.1.1"),
                80
            )),
            Decision::Deny
        );
    }

    #[test]
    fn explicit_deny_overrides_wildcard_allow() {
        // Deny a single hostile CIDR; everything else is allowed.
        let policy = Policy::new(
            "p1",
            "net-a",
            "blocklist",
            vec![
                Rule {
                    rule_id: "deny-bad".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Any,
                    port_start: 0,
                    port_end: 0,
                    cidr: "203.0.113.0/24".to_string(),
                    action: Action::Deny,
                    priority: 10,
                },
                Rule {
                    rule_id: "allow-all".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Any,
                    port_start: 0,
                    port_end: 0,
                    cidr: "0.0.0.0/0".to_string(),
                    action: Action::Allow,
                    priority: 100,
                },
            ],
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Udp,
                addr("203.0.113.7"),
                53
            )),
            Decision::Deny
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Udp,
                addr("198.51.100.7"),
                53
            )),
            Decision::Allow
        );
    }

    #[test]
    fn wildcard_protocol_and_port_and_cidr() {
        let policy = Policy::new(
            "p1",
            "net-a",
            "wild",
            vec![Rule {
                rule_id: "any".to_string(),
                direction: Direction::Egress,
                protocol: Protocol::Any,
                port_start: 0,
                port_end: 0,
                cidr: "0.0.0.0/0".to_string(),
                action: Action::Allow,
                priority: 1,
            }],
        );
        for (proto, port) in [
            (Protocol::Tcp, 443),
            (Protocol::Udp, 53),
            (Protocol::Icmp, 0),
        ] {
            assert_eq!(
                policy.evaluate(&Packet::new(
                    Direction::Egress,
                    proto,
                    addr("8.8.8.8"),
                    port
                )),
                Decision::Allow,
                "{proto:?}/{port}"
            );
        }
    }

    #[test]
    fn port_range_and_icmp_ignore_port() {
        let policy = Policy::new(
            "p1",
            "net-a",
            "range",
            vec![
                Rule {
                    rule_id: "web-range".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Tcp,
                    port_start: 8000,
                    port_end: 8010,
                    cidr: "0.0.0.0/0".to_string(),
                    action: Action::Allow,
                    priority: 1,
                },
                Rule {
                    rule_id: "ping".to_string(),
                    direction: Direction::Ingress,
                    protocol: Protocol::Icmp,
                    port_start: 0,
                    port_end: 0,
                    cidr: "10.0.0.0/8".to_string(),
                    action: Action::Allow,
                    priority: 1,
                },
            ],
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("1.1.1.1"),
                8005
            )),
            Decision::Allow
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Tcp,
                addr("1.1.1.1"),
                8011
            )),
            Decision::Deny
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Icmp,
                addr("10.1.2.3"),
                0
            )),
            Decision::Allow
        );
        assert_eq!(
            policy.evaluate(&Packet::new(
                Direction::Ingress,
                Protocol::Icmp,
                addr("192.168.1.1"),
                0
            )),
            Decision::Deny
        );
    }

    #[test]
    fn validation_rejects_bad_rules() {
        let mut bad = allow_rule("r1", 80, 100);
        bad.cidr = "not-a-cidr".to_string();
        assert!(bad.validate().is_err());
        let mut bad = allow_rule("r1", 80, 100);
        bad.port_start = 90;
        bad.port_end = 80;
        assert!(bad.validate().is_err());
        let bad = allow_rule("", 80, 100);
        assert!(bad.validate().is_err());
        // Duplicate rule ids in one policy are rejected.
        let policy = Policy::new(
            "p1",
            "net-a",
            "dup",
            vec![allow_rule("r1", 80, 100), allow_rule("r1", 443, 101)],
        );
        assert!(policy.validate().is_err());
        // A well-formed policy validates.
        let policy = Policy::new("p1", "net-a", "ok", vec![allow_rule("r1", 80, 100)]);
        assert!(policy.validate().is_ok());
    }

    #[test]
    fn deciding_rule_reports_the_match() {
        let policy = Policy::new("p1", "net-a", "diag", vec![allow_rule("r-web", 80, 100)]);
        let pkt = Packet::new(Direction::Ingress, Protocol::Tcp, addr("1.2.3.4"), 80);
        assert_eq!(policy.deciding_rule(&pkt).unwrap().rule_id, "r-web");
        let miss = Packet::new(Direction::Ingress, Protocol::Tcp, addr("1.2.3.4"), 22);
        assert!(policy.deciding_rule(&miss).is_none());
    }

    #[test]
    fn policy_serializes_with_serde() {
        let policy = Policy::new("p1", "net-a", "web", vec![allow_rule("r1", 80, 100)]);
        let json = serde_json::to_string(&policy).unwrap();
        let back: Policy = serde_json::from_str(&json).unwrap();
        assert_eq!(back, policy);
    }
}
