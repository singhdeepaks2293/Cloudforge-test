//! Real kernel backend: rtnetlink (links, routes, namespaces) +
//! nftables (firewall, NAT).
//!
//! **Fidelity: REAL kernel calls, UNVERIFIED in this sandbox.**
//! This module compiles and issues genuine netlink / `nft` requests,
//! but the sandbox has no `CAP_NET_ADMIN` and no KVM, so no M4 run
//! here has exercised a successful kernel mutation — every mutating
//! call is expected to fail here with `EPERM` (surfaced as
//! `BACKEND_ERROR`). Verification needs a real host with
//! `CAP_NET_ADMIN` and the `nft` binary on `PATH`. Never claim a
//! simulated run proved the real backend.
//!
//! Per-operation fidelity:
//! * Networks / bridges / veth pairs / attach / routes — real
//!   rtnetlink requests (`ip link add … type bridge/veth`, `ip link
//!   set … master …`, `ip route add/del` equivalents).
//! * Namespaces — real `rtnetlink::NetworkNamespace::add/del`
//!   (`ip netns add/del` equivalent, via fork + unshare).
//! * Subnets — no kernel object: a subnet is a logical carve-out of a
//!   network's CIDR, enforced by the store/IPAM. No-op here.
//! * Firewall — real nftables ruleset (`table ip cloudforge`,
//!   base chains `cf-input` / `cf-output`, policy `drop`, one rule
//!   per policy rule, ordered by priority with deny-before-allow
//!   ties). Applying a policy **replaces** the table, so the most
//!   recently applied policy is the enforced one.
//! * NAT — real nftables ruleset (`table ip cloudforge-nat`,
//!   `postrouting` SNAT / `prerouting` DNAT chains). `delete_nat`
//!   removes the whole NAT table.
//! * Fault injection — **not wired**: returns `BACKEND_UNAVAILABLE`.
//!   Dropping/delaying real packets needs tc/netem qdiscs, which
//!   rtnetlink 0.14 does not expose for this use; the fault *model*
//!   (validation, registry, traffic effects) lives in the
//!   `fault-injection` crate and is what the simulated path uses.
//! * Cleanup — deletes both nftables tables (best effort); link
//!   deletion is driven by the store's per-resource deletes.

use crate::backend::RealNetlink;
use crate::errors::CoreError;
use crate::types::{LabId, NatKind, NatRule, TenantId};
use async_trait::async_trait;
use futures::TryStreamExt;
use ipam::Cidr;
use nftables::helper::apply_ruleset;
use nftables::schema::{Chain, NfCmd, NfListObject, Nftables, Rule, Table};
use nftables::stmt::{Match, Operator, Statement, NAT};
use nftables::types::{NfChainPolicy, NfChainType, NfFamily, NfHook};
use nftables::{
    expr::Expression, expr::NamedExpression, expr::Payload, expr::PayloadField, expr::Prefix,
};
use rtnetlink::Handle;
use std::net::Ipv4Addr;
use std::os::unix::io::AsRawFd;

/// Table / chain names used by the real backend. Names are fixed per
/// host (not per lab): applying a policy replaces the shared table.
/// Multi-lab isolation on one host is future work (M9 hardening).
pub const FIREWALL_TABLE: &str = "cloudforge";
pub const NAT_TABLE: &str = "cloudforge-nat";

/// Real kernel backend. Constructing it performs no I/O; every method
/// opens a fresh netlink connection.
#[derive(Debug, Default, Clone, Copy)]
pub struct RealNetlinkBackend;

impl RealNetlinkBackend {
    pub fn new() -> Self {
        Self
    }

    pub fn fidelity() -> &'static str {
        "NETLINK"
    }

    /// Open a netlink connection and spawn its driver task.
    fn connect() -> Result<Handle, CoreError> {
        let (connection, handle, _messages) = rtnetlink::new_connection().map_err(|e| {
            CoreError::Backend(format!("BACKEND_ERROR: rtnetlink connect failed: {e}"))
        })?;
        tokio::spawn(connection);
        Ok(handle)
    }
}

/// Linux interface names are at most 15 bytes (`IFNAMSIZ - 1`).
fn check_ifname(name: &str) -> Result<(), CoreError> {
    if name.is_empty() {
        return Err(CoreError::InvalidArgument(
            "interface name must not be empty".to_string(),
        ));
    }
    if name.len() > 15 {
        return Err(CoreError::InvalidArgument(format!(
            "interface name {name:?} exceeds 15 characters (Linux IFNAMSIZ)"
        )));
    }
    Ok(())
}

async fn link_index(handle: &Handle, name: &str) -> Result<u32, CoreError> {
    let mut stream = handle.link().get().match_name(name.to_string()).execute();
    match stream.try_next().await {
        Ok(Some(msg)) => Ok(msg.header.index),
        Ok(None) => Err(CoreError::NotFound(format!(
            "link {name} not found (rtnetlink)"
        ))),
        Err(e) => Err(CoreError::Backend(format!(
            "BACKEND_ERROR: rtnetlink link lookup for {name} failed: {e}"
        ))),
    }
}

fn rt_err(e: rtnetlink::Error) -> CoreError {
    CoreError::Backend(format!("BACKEND_ERROR: rtnetlink request failed: {e}"))
}

/// Build the nftables ruleset for one firewall policy: table + two
/// base chains (input/output, policy drop) + one rule per policy rule,
/// ordered by priority (lower first, deny-before-allow on ties — the
/// same ordering as `firewall::Policy::evaluate`).
fn firewall_ruleset(policy: &firewall::Policy) -> Nftables {
    let mut objects: Vec<nftables::schema::NfObject> = Vec::new();
    objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
        NfListObject::Table(Table {
            family: NfFamily::IP,
            name: FIREWALL_TABLE.to_string(),
            handle: None,
        }),
    )));
    for (chain_name, hook) in [("cf-input", NfHook::Input), ("cf-output", NfHook::Output)] {
        objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
            NfListObject::Chain(Chain {
                family: NfFamily::IP,
                table: FIREWALL_TABLE.to_string(),
                name: chain_name.to_string(),
                newname: None,
                handle: None,
                _type: Some(NfChainType::Filter),
                hook: Some(hook),
                prio: Some(0),
                dev: None,
                policy: Some(NfChainPolicy::Drop),
            }),
        )));
    }

    let mut rules: Vec<&firewall::Rule> = policy.rules.iter().collect();
    rules.sort_by(|a, b| {
        a.priority
            .cmp(&b.priority)
            .then_with(|| match (a.action, b.action) {
                (firewall::Action::Deny, firewall::Action::Allow) => std::cmp::Ordering::Less,
                (firewall::Action::Allow, firewall::Action::Deny) => std::cmp::Ordering::Greater,
                _ => std::cmp::Ordering::Equal,
            })
    });

    for rule in rules {
        let (chain, addr_field) = match rule.direction {
            firewall::Direction::Ingress => ("cf-input", "saddr"),
            firewall::Direction::Egress => ("cf-output", "daddr"),
        };
        let mut expr: Vec<Statement> = Vec::new();
        // Remote CIDR match (skip for the any-address wildcard).
        if rule.cidr != "0.0.0.0/0" {
            if let Ok(cidr) = Cidr::parse(&rule.cidr) {
                expr.push(Statement::Match(Match {
                    left: Expression::Named(NamedExpression::Payload(Payload::PayloadField(
                        PayloadField {
                            protocol: "ip".to_string(),
                            field: addr_field.to_string(),
                        },
                    ))),
                    right: Expression::Named(NamedExpression::Prefix(Prefix {
                        addr: Box::new(Expression::String(cidr.network_address().to_string())),
                        len: cidr.prefix() as u32,
                    })),
                    op: Operator::EQ,
                }));
            }
        }
        // Protocol match.
        let proto_num = match rule.protocol {
            firewall::Protocol::Tcp => Some(6u32),
            firewall::Protocol::Udp => Some(17u32),
            firewall::Protocol::Icmp => Some(1u32),
            firewall::Protocol::Any => None,
        };
        if let Some(num) = proto_num {
            expr.push(Statement::Match(Match {
                left: Expression::Named(NamedExpression::Payload(Payload::PayloadField(
                    PayloadField {
                        protocol: "ip".to_string(),
                        field: "protocol".to_string(),
                    },
                ))),
                right: Expression::Number(num),
                op: Operator::EQ,
            }));
        }
        // Port match (TCP/UDP only; a range becomes two comparisons).
        if rule.protocol != firewall::Protocol::Icmp
            && rule.protocol != firewall::Protocol::Any
            && !(rule.port_start == 0 && rule.port_end == 0)
        {
            let proto_name = match rule.protocol {
                firewall::Protocol::Tcp => "tcp",
                firewall::Protocol::Udp => "udp",
                _ => "tcp",
            };
            if rule.port_start == rule.port_end {
                expr.push(Statement::Match(Match {
                    left: Expression::Named(NamedExpression::Payload(Payload::PayloadField(
                        PayloadField {
                            protocol: proto_name.to_string(),
                            field: "dport".to_string(),
                        },
                    ))),
                    right: Expression::Number(rule.port_start as u32),
                    op: Operator::EQ,
                }));
            } else {
                for (op, port) in [
                    (Operator::GEQ, rule.port_start),
                    (Operator::LEQ, rule.port_end),
                ] {
                    expr.push(Statement::Match(Match {
                        left: Expression::Named(NamedExpression::Payload(Payload::PayloadField(
                            PayloadField {
                                protocol: proto_name.to_string(),
                                field: "dport".to_string(),
                            },
                        ))),
                        right: Expression::Number(port as u32),
                        op,
                    }));
                }
            }
        }
        expr.push(match rule.action {
            firewall::Action::Allow => Statement::Accept(None),
            firewall::Action::Deny => Statement::Drop(None),
        });
        objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
            NfListObject::Rule(Rule {
                family: NfFamily::IP,
                table: FIREWALL_TABLE.to_string(),
                chain: chain.to_string(),
                expr,
                handle: None,
                index: None,
                comment: Some(format!("cloudforge rule {}", rule.rule_id)),
            }),
        )));
    }
    Nftables { objects }
}

fn delete_table_ruleset(table: &str) -> Nftables {
    Nftables {
        objects: vec![nftables::schema::NfObject::CmdObject(NfCmd::Delete(
            NfListObject::Table(Table {
                family: NfFamily::IP,
                name: table.to_string(),
                handle: None,
            }),
        ))],
    }
}

/// Apply an nftables ruleset by shelling out to the `nft` binary
/// (via the `nftables` crate helper). Blocking I/O, so run on the
/// blocking pool.
async fn apply_nft(ruleset: Nftables) -> Result<(), CoreError> {
    tokio::task::spawn_blocking(move || apply_ruleset(&ruleset, None, None))
        .await
        .map_err(|e| CoreError::Backend(format!("BACKEND_ERROR: nft apply task failed: {e}")))?
        .map_err(|e| CoreError::Backend(format!("BACKEND_ERROR: nft apply failed: {e}")))
}

#[async_trait]
impl RealNetlink for RealNetlinkBackend {
    fn fidelity(&self) -> &'static str {
        Self::fidelity()
    }

    async fn create_network(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        network_id: &str,
        _cidr: &str,
    ) -> Result<(), CoreError> {
        check_ifname(network_id)?;
        let handle = Self::connect()?;
        handle
            .link()
            .add()
            .bridge(network_id.to_string())
            .execute()
            .await
            .map_err(rt_err)?;
        let index = link_index(&handle, network_id).await?;
        handle
            .link()
            .set(index)
            .up()
            .execute()
            .await
            .map_err(rt_err)?;
        Ok(())
    }

    async fn delete_network(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        network_id: &str,
    ) -> Result<(), CoreError> {
        let handle = Self::connect()?;
        let index = link_index(&handle, network_id).await?;
        handle.link().del(index).execute().await.map_err(rt_err)?;
        Ok(())
    }

    async fn create_namespace(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        namespace: &str,
    ) -> Result<(), CoreError> {
        rtnetlink::NetworkNamespace::add(namespace.to_string())
            .await
            .map_err(rt_err)
    }

    async fn delete_namespace(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        namespace: &str,
    ) -> Result<(), CoreError> {
        rtnetlink::NetworkNamespace::del(namespace.to_string())
            .await
            .map_err(rt_err)
    }

    async fn create_bridge(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        bridge: &str,
        _namespace: &str,
    ) -> Result<(), CoreError> {
        check_ifname(bridge)?;
        let handle = Self::connect()?;
        handle
            .link()
            .add()
            .bridge(bridge.to_string())
            .execute()
            .await
            .map_err(rt_err)?;
        let index = link_index(&handle, bridge).await?;
        handle
            .link()
            .set(index)
            .up()
            .execute()
            .await
            .map_err(rt_err)?;
        Ok(())
    }

    async fn cleanup_lab(&self, _lab: &LabId, _tenant: &TenantId) -> Result<usize, CoreError> {
        // Best effort: drop the engine's nftables tables. Link
        // deletion is driven by the store's per-resource deletes.
        let _ = apply_nft(delete_table_ruleset(FIREWALL_TABLE)).await;
        let _ = apply_nft(delete_table_ruleset(NAT_TABLE)).await;
        Ok(0)
    }

    async fn create_subnet(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _subnet_id: &str,
        _network_id: &str,
        _cidr: &str,
    ) -> Result<(), CoreError> {
        // A subnet is a logical carve-out of a network's CIDR; it has
        // no separate kernel object. No-op by design.
        Ok(())
    }

    async fn delete_subnet(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _subnet_id: &str,
    ) -> Result<(), CoreError> {
        Ok(())
    }

    async fn create_veth_pair(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _veth_id: &str,
        peer_a: &str,
        peer_b: &str,
    ) -> Result<(), CoreError> {
        check_ifname(peer_a)?;
        check_ifname(peer_b)?;
        let handle = Self::connect()?;
        handle
            .link()
            .add()
            .veth(peer_a.to_string(), peer_b.to_string())
            .execute()
            .await
            .map_err(rt_err)?;
        for peer in [peer_a, peer_b] {
            let index = link_index(&handle, peer).await?;
            handle
                .link()
                .set(index)
                .up()
                .execute()
                .await
                .map_err(rt_err)?;
        }
        Ok(())
    }

    async fn attach_interface(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _veth_id: &str,
        interface: &str,
        namespace: &str,
        bridge: &str,
    ) -> Result<(), CoreError> {
        let handle = Self::connect()?;
        let index = link_index(&handle, interface).await?;
        if !namespace.is_empty() {
            // Move the link into the named netns: open its /run/netns
            // file (created by `NetworkNamespace::add`) and pass the fd.
            let path = format!("/run/netns/{namespace}");
            let file = std::fs::File::open(&path)
                .map_err(|e| CoreError::NotFound(format!("netns file {path} not found: {e}")))?;
            handle
                .link()
                .set(index)
                .setns_by_fd(file.as_raw_fd())
                .execute()
                .await
                .map_err(rt_err)?;
            return Ok(());
        }
        if !bridge.is_empty() {
            let bridge_index = link_index(&handle, bridge).await?;
            handle
                .link()
                .set(index)
                .controller(bridge_index)
                .execute()
                .await
                .map_err(rt_err)?;
        }
        handle
            .link()
            .set(index)
            .up()
            .execute()
            .await
            .map_err(rt_err)?;
        Ok(())
    }

    async fn add_route(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        entry: &routing::RouteEntry,
    ) -> Result<(), CoreError> {
        let cidr = Cidr::parse(&entry.destination).map_err(CoreError::from)?;
        let gw: Ipv4Addr = entry.gateway.parse().map_err(|_| {
            CoreError::InvalidArgument(format!("invalid gateway IPv4: {}", entry.gateway))
        })?;
        let handle = Self::connect()?;
        let ifindex = link_index(&handle, &entry.interface).await?;
        handle
            .route()
            .add()
            .v4()
            .destination_prefix(cidr.network_address(), cidr.prefix())
            .gateway(gw)
            .output_interface(ifindex)
            .priority(entry.metric.max(0) as u32)
            .execute()
            .await
            .map_err(rt_err)?;
        Ok(())
    }

    async fn delete_route(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _route_id: &str,
        destination: &str,
        gateway: &str,
        interface: &str,
    ) -> Result<(), CoreError> {
        let cidr = Cidr::parse(destination).map_err(CoreError::from)?;
        let gw: Ipv4Addr = gateway
            .parse()
            .map_err(|_| CoreError::InvalidArgument(format!("invalid gateway IPv4: {gateway}")))?;
        let handle = Self::connect()?;
        let ifindex = link_index(&handle, interface).await?;
        let mut req = handle
            .route()
            .add()
            .v4()
            .destination_prefix(cidr.network_address(), cidr.prefix())
            .gateway(gw)
            .output_interface(ifindex);
        let message = req.message_mut().clone();
        handle
            .route()
            .del(message)
            .execute()
            .await
            .map_err(rt_err)?;
        Ok(())
    }

    async fn apply_firewall(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        policy: &firewall::Policy,
    ) -> Result<(), CoreError> {
        // Replace the shared table so the applied policy is the
        // enforced one; a missing table on delete is fine.
        let _ = apply_nft(delete_table_ruleset(FIREWALL_TABLE)).await;
        apply_nft(firewall_ruleset(policy)).await
    }

    async fn create_nat(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        rule: &NatRule,
    ) -> Result<(), CoreError> {
        let translated: Ipv4Addr = rule.translated_address.parse().map_err(|_| {
            CoreError::InvalidArgument(format!(
                "invalid translated IPv4: {}",
                rule.translated_address
            ))
        })?;
        let mut objects: Vec<nftables::schema::NfObject> =
            vec![nftables::schema::NfObject::CmdObject(NfCmd::Add(
                NfListObject::Table(Table {
                    family: NfFamily::IP,
                    name: NAT_TABLE.to_string(),
                    handle: None,
                }),
            ))];
        match rule.kind {
            NatKind::Snat => {
                objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
                    NfListObject::Chain(Chain {
                        family: NfFamily::IP,
                        table: NAT_TABLE.to_string(),
                        name: "cf-postrouting".to_string(),
                        newname: None,
                        handle: None,
                        _type: Some(NfChainType::NAT),
                        hook: Some(NfHook::Postrouting),
                        prio: Some(100),
                        dev: None,
                        policy: Some(NfChainPolicy::Accept),
                    }),
                )));
                let mut expr: Vec<Statement> = Vec::new();
                if rule.source_cidr != "0.0.0.0/0" {
                    if let Ok(cidr) = Cidr::parse(&rule.source_cidr) {
                        expr.push(Statement::Match(Match {
                            left: Expression::Named(NamedExpression::Payload(
                                Payload::PayloadField(PayloadField {
                                    protocol: "ip".to_string(),
                                    field: "saddr".to_string(),
                                }),
                            )),
                            right: Expression::Named(NamedExpression::Prefix(Prefix {
                                addr: Box::new(Expression::String(
                                    cidr.network_address().to_string(),
                                )),
                                len: cidr.prefix() as u32,
                            })),
                            op: Operator::EQ,
                        }));
                    }
                }
                expr.push(Statement::SNAT(Some(NAT {
                    addr: Some(Expression::String(translated.to_string())),
                    family: Some(nftables::stmt::NATFamily::IP),
                    port: None,
                    flags: None,
                })));
                objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
                    NfListObject::Rule(Rule {
                        family: NfFamily::IP,
                        table: NAT_TABLE.to_string(),
                        chain: "cf-postrouting".to_string(),
                        expr,
                        handle: None,
                        index: None,
                        comment: Some(format!("cloudforge nat {}", rule.nat_id)),
                    }),
                )));
            }
            NatKind::Dnat => {
                objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
                    NfListObject::Chain(Chain {
                        family: NfFamily::IP,
                        table: NAT_TABLE.to_string(),
                        name: "cf-prerouting".to_string(),
                        newname: None,
                        handle: None,
                        _type: Some(NfChainType::NAT),
                        hook: Some(NfHook::Prerouting),
                        prio: Some(-100),
                        dev: None,
                        policy: Some(NfChainPolicy::Accept),
                    }),
                )));
                let dest = Cidr::parse(&rule.source_cidr)
                    .map(|c| c.network_address().to_string())
                    .unwrap_or_else(|_| rule.source_cidr.clone());
                let expr: Vec<Statement> = vec![
                    Statement::Match(Match {
                        left: Expression::Named(NamedExpression::Payload(Payload::PayloadField(
                            PayloadField {
                                protocol: "ip".to_string(),
                                field: "daddr".to_string(),
                            },
                        ))),
                        right: Expression::String(translated.to_string()),
                        op: Operator::EQ,
                    }),
                    Statement::DNAT(Some(NAT {
                        addr: Some(Expression::String(dest)),
                        family: Some(nftables::stmt::NATFamily::IP),
                        port: None,
                        flags: None,
                    })),
                ];
                objects.push(nftables::schema::NfObject::CmdObject(NfCmd::Add(
                    NfListObject::Rule(Rule {
                        family: NfFamily::IP,
                        table: NAT_TABLE.to_string(),
                        chain: "cf-prerouting".to_string(),
                        expr,
                        handle: None,
                        index: None,
                        comment: Some(format!("cloudforge nat {}", rule.nat_id)),
                    }),
                )));
            }
        }
        apply_nft(Nftables { objects }).await
    }

    async fn delete_nat(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _nat_id: &str,
    ) -> Result<(), CoreError> {
        // NAT rules share one table; deleting any rule drops the table
        // (documented M4 limitation — per-rule nftables handles land
        // with multi-policy support).
        apply_nft(delete_table_ruleset(NAT_TABLE)).await
    }

    async fn inject_fault(
        &self,
        _lab: &LabId,
        _tenant: &TenantId,
        _fault: &fault_injection::FaultSpec,
    ) -> Result<(), CoreError> {
        Err(CoreError::Unavailable(
            "BACKEND_UNAVAILABLE: tc/netem fault injection is not wired into the real backend in M4; faults are recorded and modelled by the fault-injection crate (simulated path) only".to_string(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fidelity_marker() {
        assert_eq!(RealNetlinkBackend::fidelity(), "NETLINK");
    }

    #[test]
    fn firewall_ruleset_serializes_to_nft_json() {
        // The ruleset must serialize to the JSON the `nft` binary
        // accepts; this is the closest check possible without
        // CAP_NET_ADMIN or an `nft` binary in the sandbox.
        let policy = firewall::Policy::new(
            "p1",
            "net-a",
            "web",
            vec![
                firewall::Rule {
                    rule_id: "deny-ssh".to_string(),
                    direction: firewall::Direction::Ingress,
                    protocol: firewall::Protocol::Tcp,
                    port_start: 22,
                    port_end: 22,
                    cidr: "0.0.0.0/0".to_string(),
                    action: firewall::Action::Deny,
                    priority: 10,
                },
                firewall::Rule {
                    rule_id: "allow-web".to_string(),
                    direction: firewall::Direction::Ingress,
                    protocol: firewall::Protocol::Tcp,
                    port_start: 8000,
                    port_end: 8010,
                    cidr: "10.0.0.0/8".to_string(),
                    action: firewall::Action::Allow,
                    priority: 100,
                },
            ],
        );
        let json = serde_json::to_string(&firewall_ruleset(&policy)).unwrap();
        assert!(json.contains("cloudforge"));
        assert!(json.contains("cf-input"));
        assert!(json.contains("deny-ssh"));
        // Rules are emitted in priority order: deny-ssh before allow-web.
        assert!(json.find("deny-ssh").unwrap() < json.find("allow-web").unwrap());
        // Round-trip through the nftables schema types.
        let back: Nftables = serde_json::from_str(&json).unwrap();
        assert!(!back.objects.is_empty());
    }

    #[tokio::test]
    async fn inject_fault_is_unavailable_in_real_backend() {
        let backend = RealNetlinkBackend::new();
        let lab = LabId::new("lab-1").unwrap();
        let tenant = TenantId::new("tenant-1").unwrap();
        let fault = fault_injection::FaultSpec::new(
            "f1",
            fault_injection::FaultKind::Latency,
            100.0,
            60,
            "",
        );
        let err = backend
            .inject_fault(&lab, &tenant, &fault)
            .await
            .unwrap_err();
        assert_eq!(err.error_code(), "BACKEND_UNAVAILABLE");
    }

    #[tokio::test]
    async fn simulated_backend_new_methods_succeed() {
        use crate::backend::SimulatedBackend;
        let backend = SimulatedBackend::new();
        let lab = LabId::new("lab-1").unwrap();
        let tenant = TenantId::new("tenant-1").unwrap();
        assert!(backend
            .create_subnet(&lab, &tenant, "s1", "net-a", "10.0.1.0/24")
            .await
            .is_ok());
        assert!(backend
            .create_veth_pair(&lab, &tenant, "v1", "veth-a", "veth-b")
            .await
            .is_ok());
        assert!(backend
            .attach_interface(&lab, &tenant, "v1", "veth-a", "", "br-0")
            .await
            .is_ok());
        let entry = routing::RouteEntry::new("r1", "0.0.0.0/0", "10.0.0.1", "eth0", 100, "");
        assert!(backend.add_route(&lab, &tenant, &entry).await.is_ok());
        assert!(backend
            .delete_route(&lab, &tenant, "r1", "0.0.0.0/0", "10.0.0.1", "eth0")
            .await
            .is_ok());
        let policy = firewall::Policy::new("p1", "net-a", "web", vec![]);
        assert!(backend.apply_firewall(&lab, &tenant, &policy).await.is_ok());
        let rule = NatRule {
            nat_id: "n1".to_string(),
            kind: NatKind::Snat,
            source_cidr: "10.0.0.0/16".to_string(),
            translated_address: "203.0.113.1".to_string(),
            interface: "eth0".to_string(),
        };
        assert!(backend.create_nat(&lab, &tenant, &rule).await.is_ok());
        assert!(backend.delete_nat(&lab, &tenant, "n1").await.is_ok());
        let fault =
            fault_injection::FaultSpec::new("f1", fault_injection::FaultKind::Loss, 10.0, 60, "");
        assert!(backend.inject_fault(&lab, &tenant, &fault).await.is_ok());
    }
}
