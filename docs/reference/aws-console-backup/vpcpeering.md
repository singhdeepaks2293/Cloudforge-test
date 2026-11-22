# VPC Peering — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `vpcpeering` |
| Name | VPC Peering |
| Category | Networking |
| Registry route | `/services/vpcpeering` |
| Availability today | available |
| Fidelity today | **SIMULATED** |
| CloudForge console today | Yes — frontend/src/pages/NetworkingConsoles.tsx `VpcPeeringConsole`, route `/services/vpcpeering` |
| Resource types | peering-connection |
| Learning tracks linked | networking |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

VPC peering is the networking connection between two VPCs: a peering connection lets instances in one VPC talk to instances in another (same account, cross-account, or cross-region) as if they were on the same network, using private IP addresses and route-table entries — with no gateway, no VPN, and no single point of failure. A learner meets it as the first VPC-to-VPC connectivity option, before Transit Gateway (many VPCs) and VPN/Direct Connect (on-premises).

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Peering connections (under the VPC console's left nav: Your VPCs / Subnets / Route Tables / Internet Gateways / NAT Gateways / Peering Connections / Security Groups / Network ACLs).
**Home — Peering connections list:** columns Peering connection ID (`pcx-…`), Status (Initiating request / Pending acceptance / Active / Rejected / Failed / Deleted / Expired), Accepter VPC, Requester VPC, Accepter owner, Created. Filters: status, search.
**Detail tabs:** Details (peering ID, status, requester/accepter VPC + owner + region, expiry), DNS settings (DNS resolution over the peering, toggle), Tags.
**Wizard — Create peering connection:** 1. Peering connection name tag. 2. Select a local VPC to peer with (requester VPC dropdown). 3. Select another VPC to peer with (accepter: My VPC / Another account / Another region; VPC ID, account ID, region). 4. Review and create. After creation: the accepter must Accept (action on the connection) before it becomes Active; then both sides add routes (destination CIDR → `pcx-…`) in their route tables.
**Actions:** Create peering connection, Accept request, Reject request, Delete peering connection, Edit DNS settings, Add route (deep-link to the route table).

## 3. CloudForge fidelity today

- **Fidelity: SIMULATED** (availability: available). Registry description: _Peering connections between lab VPCs — real route-table entries._
- **What the current console shows (registry mirrors note):** Peering connections list; Create-peering wizard (CIDR overlap rejected); Delete removes routes
- **How it differs / what is missing (registry differs note):** SIMULATED: no engine peering RPC exists — a peering is real route-table entries in both VPCs' lab state, not kernel peering. CIDRs must not overlap.
- **Console today:** Yes — frontend/src/pages/NetworkingConsoles.tsx `VpcPeeringConsole`, route `/services/vpcpeering`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — move peering into the engine, do not rebuild the record layer.** Current: peering is real route-table entries in both VPCs' lab state (same-VPC and overlapping-CIDR peerings refused); no engine peering RPC exists — it is not kernel peering (backend/internal/httpapi/phase6.go `p6_vpc_peerings`). Fidelity SIMULATED. Console: NetworkingConsoles.tsx `VpcPeeringConsole`. To build later, in order:
1. **Engine RPC:** add `CreateVpcPeering`/`DeleteVpcPeering` to the Rust network engine (proto/cloudforge/v1/network.proto) so a peering creates real cross-namespace routing (veth pair + routes in both namespaces) instead of only lab-state route entries; the API then calls the engine and records `engine_peering_applied: true` only on success (same honesty pattern as the Network Firewall engine sync). The simulated backend keeps the current behaviour for lite.
2. **Cross-region / cross-account:** out of scope — CloudForge has one region with real resources (`cf-local-1`) and one account model; the wizard's "another account/region" options stay disabled with the reason. Same-VPC and overlapping-CIDR refusals stay.
3. **DNS resolution over peering:** when enabled, the Route 53 resolve endpoint should resolve names in the peer VPC — implement as a flag on the peering record that the resolver honours.
4. **Route validation:** creating a peering should offer (not force) the route-table entries in both VPCs with one click ("Add routes"), and the validator should warn when a peering is Active but no route points at it.
**Data model (exists, extend):** `p6_vpc_peerings`; add `engine_peering_applied bool` and `dns_resolution bool` columns.
**Labs / lessons:** add one lesson to the `networking` track: peer two lab VPCs, add routes, prove connectivity with the terminal (ping/curl between instances), then delete the peering and watch connectivity break. Reuse the VPC lessons.
**Lite feasibility:** Yes for the lab-state version (already works); the engine RPC is full-mode (real netlink) and falls back to lab-state entries in lite.

## 5. Lite feasibility and scope

- **Scope: small.** Engine CreateVpcPeering RPC for real cross-namespace routing; lab-state entries stay the lite path.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `vpcpeering`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
