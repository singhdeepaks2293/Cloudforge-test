import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge, StateBadge } from '../components/AwsBadge.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { TransitGatewayListContent } from './NetworkingConsoles.tsx'
import {
  AwsActionsMenu,
  AwsFlash,
  AwsPreferencesButton,
  AwsRefreshButton,
  AwsStat,
  AwsToolbar,
  AwsWizard,
} from '../components/AwsShell.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import {
  addSecurityGroupRule,
  addVpcRoute,
  attachGateway,
  attachSecurityGroupToInstance,
  createLabResource,
  deleteLabResource,
  deleteSecurityGroupRule,
  deleteVpcRoute,
  detachGateway,
  fetchLabResources,
  fetchLabs,
  request,
  setResourceTags,
} from '../services/api.ts'
import { getStoredRegion } from '../services/catalog.ts'
import {
  fmtTime,
  str,
  useCustomerGateways,
  useExamAction,
  useExamCreate,
  useExamDelete,
  useVpnConnections,
  useVpnGateways,
  type Rec,
} from '../services/examResources.ts'
import type { LabResource } from '../types/index.ts'

/**
 * VPC console (Phase 2) — mirrors the AWS VPC console's left nav:
 * Your VPCs / Subnets / Route Tables / Internet Gateways / NAT
 * Gateways / Security Groups / Network ACLs. Rows are REAL lab
 * resources (per-lab, picked above); creation goes through the real
 * POST /labs/{id}/resources API with backend CIDR validation (bad
 * CIDR -> 400, never a fake row). Route tables and security groups
 * have real detail panels: routes are engine-owned (a route the
 * engine rejects is never persisted — 502 when the engine is down),
 * SG rules are DB-authoritative and the UI says whether the engine
 * firewall snapshot applied. IGW attach/detach is recorded
 * (SIMULATED — the engine has no IGW RPC). NAT gateways go through
 * the engine (real netlink where it supports it, SIMULATED in lite).
 * NACLs are API_EMULATED exam resources — durable records with real
 * stateless rule evaluation (lowest rule number first, default deny);
 * the ACL is not attached to any subnet and filters no live traffic.
 * VPN is API-EMULATED too: customer gateways, VPN gateways and
 * connections are durable records with tunnel activate/deactivate;
 * the tunnel state is a teaching field — no IPsec tunnel exists and
 * no traffic flows. Site-to-Site VPN Concentrators and Client VPN
 * endpoints have no backend and stay disabled nav labels.
 * v4 survey (2026-11-22): Your VPCs uses the survey's exact 17
 * columns (Name, VPC ID, ARN, State, Encryption control ID/mode,
 * Block Public Access, Automated internet firewall coverage, IPv4/IPv6
 * CIDR, DHCP option set, Main route table, Main network ACL, Tenancy,
 * Default VPC, Owner ID) with VPCs / VPC encryption controls tabs
 * above the table; Subnets uses the survey's exact 22 columns;
 * a selected VPC opens the survey's 6 observed detail tabs
 * (Resource map, CIDRs, Flow logs, Tags, Related resources,
 * Integrations); Create VPC is the observed wizard (VPC only /
 * VPC and more, name tag, IPv4 CIDR manual or IPAM, IPv6, Tenancy,
 * VPC encryption control, Tags). Columns the lab record has no
 * value for render '—', never an invented value.
 */
type Section = 'dashboard' | 'vpcs' | 'subnets' | 'routetables' | 'gateways' | 'nat' | 'securitygroups' | 'nacls' | 'vpn' | 'customer-gateways' | 'vpn-gateways' | 'tgw' | 'verified-access' | 'cloud-wan' | 'lattice' | 'dns-firewall'

const SECTION_KIND: Record<Exclude<Section, 'gateways' | 'nat' | 'nacls' | 'dashboard' | 'vpn' | 'customer-gateways' | 'vpn-gateways' | 'tgw' | 'verified-access' | 'cloud-wan' | 'lattice' | 'dns-firewall'>, string> = {
  vpcs: 'vpc',
  subnets: 'subnet',
  routetables: 'route-table',
  securitygroups: 'security-group',
}

const EMPTY_RESOURCES: LabResource[] = []

function cfgList(config: Record<string, unknown> | undefined, key: string): Record<string, unknown>[] {
  if (!config) return []
  const v = config[key]
  if (!Array.isArray(v)) return []
  return v.filter((x): x is Record<string, unknown> => typeof x === 'object' && x !== null)
}

/**
 * Usable IPv4 addresses in a subnet CIDR, derived from the real CIDR
 * (AWS reserves 5 addresses: network, router, DNS, future use,
 * broadcast). Returns '—' when the CIDR is absent or unparseable.
 */
function availableIpv4(cidr: string | undefined): string {
  if (!cidr) return '—'
  const m = cidr.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)\/(\d+)$/)
  if (!m) return '—'
  const prefix = Number(m[5])
  if (prefix < 16 || prefix > 28) return '—'
  return String(Math.max(0, 2 ** (32 - prefix) - 5))
}

function cfgStr(config: Record<string, unknown> | undefined, key: string): string {
  if (!config) return ''
  const v = config[key]
  return typeof v === 'string' ? v : ''
}

export function VpcConsole() {
  const queryClient = useQueryClient()
  const [searchParams] = useSearchParams()
  const [section, setSection] = useState<Section>(() => {
    const fromUrl = searchParams.get('section')
    return fromUrl === 'vpn' || fromUrl === 'customer-gateways' || fromUrl === 'vpn-gateways' || fromUrl === 'tgw' || fromUrl === 'nacls' || fromUrl === 'verified-access' || fromUrl === 'cloud-wan' || fromUrl === 'lattice' || fromUrl === 'dns-firewall' ? (fromUrl as Section) : 'dashboard'
  })
  const [labId, setLabId] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [notice, setNotice] = useState('')
  // Tabs above the Your VPCs table (v4 survey): VPCs / VPC encryption controls.
  const [vpcTab, setVpcTab] = useState<'vpcs' | 'encryption-controls'>('vpcs')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const region = getStoredRegion()

  const labs = useQuery({ queryKey: ['labs'], queryFn: fetchLabs, retry: 1 })
  const activeLabId = labId !== '' ? labId : (labs.data?.[0]?.id ?? '')
  // VPN connections are real API-EMULATED records (exam.go); the
  // dashboard card reads the same query the VPN section uses.
  const vpnConnections = useVpnConnections()
  const resources = useQuery({
    queryKey: ['vpc-resources', activeLabId],
    queryFn: () => fetchLabResources(activeLabId),
    retry: 1,
    enabled: activeLabId !== '',
  })
  const all = resources.data ?? EMPTY_RESOURCES
  const vpcs = useMemo(() => all.filter((r) => r.kind === 'vpc'), [all])
  const rows: LabResource[] = (() => {
    switch (section) {
      case 'gateways':
        return all.filter((r) => r.kind === 'gateway' && cfgStr(r.config, 'gateway_type') !== 'nat')
      case 'nat':
        return all.filter((r) => r.kind === 'gateway' && cfgStr(r.config, 'gateway_type') === 'nat')
      case 'nacls':
        return []
      case 'vpn':
      case 'customer-gateways':
      case 'vpn-gateways':
      case 'verified-access':
      case 'cloud-wan':
      case 'lattice':
      case 'dns-firewall':
      case 'tgw':
        // Rendered by their own sections below, not the generic table.
        return []
      case 'dashboard':
        return []
      default:
        return all.filter((r) => r.kind === SECTION_KIND[section])
    }
  })()
  const selected = rows.find((r) => r.id === selectedId) ?? null
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['vpc-resources', activeLabId] })
  }
  const del = useMutation({
    mutationFn: (r: LabResource) => deleteLabResource(activeLabId, r.id),
    onSuccess: () => {
      setNotice('Resource deleted.')
      setSelectedId(null)
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })

  const idCell = (r: LabResource) => (
    <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(r.id)}>
      {r.id}
    </button>
  )
  // Your VPCs columns in the v4 survey's exact order (17). Only Name,
  // VPC ID, State and IPv4 CIDR have real lab values; every other
  // column renders '—' — CloudForge records no ARN, encryption
  // control, Block Public Access, DHCP option set, main route table /
  // network ACL, tenancy, default flag or owner id for a lab VPC.
  const vpcColumns = [
    { header: 'Name', render: (r: LabResource) => r.name },
    { header: 'VPC ID', render: (r: LabResource) => idCell(r) },
    { header: 'ARN', render: () => '—' },
    { header: 'State', render: (r: LabResource) => r.state ?? '—' },
    { header: 'Encryption control ID', render: () => '—' },
    { header: 'Encryption control mode', render: () => '—' },
    { header: 'Block Public Access', render: () => '—' },
    { header: 'Automated internet firewall coverage', render: () => '—' },
    { header: 'IPv4 CIDR', render: (r: LabResource) => r.cidr ?? '—' },
    { header: 'IPv6 CIDR', render: () => '—' },
    { header: 'DHCP option set', render: () => '—' },
    { header: 'Main route table', render: () => '—' },
    { header: 'Main network ACL', render: () => '—' },
    { header: 'Tenancy', render: () => '—' },
    { header: 'Default VPC', render: () => '—' },
    { header: 'Owner ID', render: () => '—' },
  ]
  // Subnets columns in the v4 survey's exact order (22). Real values:
  // Name, Subnet ID, State, VPC (parent), IPv4 CIDR, Available IPv4
  // addresses (derived from the CIDR). CloudForge labs have no
  // availability zones, IPv6, route-table or NACL attachment, so those
  // columns render '—'.
  const subnetColumns = [
    { header: 'Name', render: (r: LabResource) => r.name },
    { header: 'Subnet ID', render: (r: LabResource) => idCell(r) },
    { header: 'ARN', render: () => '—' },
    { header: 'State', render: (r: LabResource) => r.state ?? '—' },
    { header: 'VPC', render: (r: LabResource) => r.parent_id ?? '—' },
    { header: 'Block Public Access', render: () => '—' },
    { header: 'Automated internet firewall connectivity type', render: () => '—' },
    { header: 'IPv4 CIDR', render: (r: LabResource) => r.cidr ?? '—' },
    { header: 'IPv6 CIDR', render: () => '—' },
    { header: 'IPv6 CIDR association ID', render: () => '—' },
    { header: 'Available IPv4 addresses', render: (r: LabResource) => availableIpv4(r.cidr) },
    { header: 'Availability Zone', render: () => '—' },
    { header: 'Network border group', render: () => '—' },
    { header: 'Route table', render: () => '—' },
    { header: 'Internet access', render: () => '—' },
    { header: 'Network ACL', render: () => '—' },
    { header: 'Default subnet', render: () => '—' },
    { header: 'Auto-assign public IPv4', render: () => '—' },
    { header: 'Auto-assign customer-owned IPv4', render: () => '—' },
    { header: 'Customer-owned IPv4 pool', render: () => '—' },
    { header: 'Auto-assign IPv6', render: () => '—' },
    { header: 'Owner ID', render: () => '—' },
  ]
  const genericColumns = [
    { header: 'ID', render: (r: LabResource) => idCell(r) },
    { header: 'Name', render: (r: LabResource) => r.name },
    { header: 'CIDR', render: (r: LabResource) => r.cidr ?? '—' },
    { header: 'State', render: (r: LabResource) => r.state ?? '—' },
    { header: 'Parent', render: (r: LabResource) => r.parent_id ?? '—' },
    { header: 'Fidelity', render: () => <FidelityBadge fidelity="PARTIAL" footnote="" /> },
  ]
  const tableColumns = section === 'vpcs' ? vpcColumns : section === 'subnets' ? subnetColumns : genericColumns

  return (
    <ServiceConsole
      icon="network"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'VPC' }]}
      title="VPC"
      description="Virtual private clouds from your labs — real lab resources on the Rust network engine (SIMULATED backend by default). CIDRs are local (10.x); there are no AWS availability zones. Pick a lab, then create and manage its networking."
      fidelity="PARTIAL"
      fidelityFootnote="lab VPCs · engine default SIMULATED · NACLs API_EMULATED (real rule evaluation, no live filtering)"
      subNav={[
        { id: 'dashboard', label: 'VPC dashboard' },
        // Grouped left nav, mirroring the real VPC console's panel:
        // "Filter by VPC" is the lab picker above the content (VPCs
        // live inside labs); groups below match the reference order.
        { id: 'vpcs', label: 'Your VPCs', group: 'Virtual private cloud' },
        { id: 'subnets', label: 'Subnets', group: 'Virtual private cloud' },
        { id: 'routetables', label: 'Route Tables', group: 'Virtual private cloud' },
        { id: 'gateways', label: 'Internet Gateways', group: 'Virtual private cloud' },
        { id: 'egress-igw', label: 'Egress-only Internet Gateways', group: 'Virtual private cloud', disabledReason: 'No egress-only gateway API in CloudForge.' },
        { id: 'dhcp', label: 'DHCP Option Sets', group: 'Virtual private cloud', disabledReason: 'No DHCP option-set API in CloudForge.' },
        { id: 'eips', label: 'Elastic IPs', group: 'Virtual private cloud', disabledReason: 'No Elastic IP operation in CloudForge.' },
        { id: 'prefix-lists', label: 'Managed Prefix Lists', group: 'Virtual private cloud', disabledReason: 'No prefix-list API in CloudForge.' },
        { id: 'nat', label: 'NAT Gateways', group: 'Virtual private cloud' },
        { id: 'peering', label: 'Peering Connections', group: 'Virtual private cloud', to: '/services/vpcpeering' },
        { id: 'route-servers', label: 'Route Servers', group: 'Virtual private cloud', disabledReason: 'No route-server API in CloudForge.' },
        { id: 'nacls', label: 'Network ACLs', group: 'Security' },
        { id: 'dns-firewall-rules', label: 'Rule Groups', group: 'DNS Firewall', to: '/services/vpc?section=dns-firewall' },
        { id: 'dns-firewall-domains', label: 'Domain Lists', group: 'DNS Firewall', to: '/services/vpc?section=dns-firewall' },
        { id: 'securitygroups', label: 'Security Groups', group: 'Security' },
        { id: 'privatelink-start', label: 'Getting Started', group: 'PrivateLink and Lattice', disabledReason: 'No PrivateLink getting-started page in CloudForge.' },
        { id: 'endpoints', label: 'Endpoints', group: 'PrivateLink and Lattice', to: '/services/vpcendpoints' },
        { id: 'endpoint-services', label: 'Endpoint Services', group: 'PrivateLink and Lattice', disabledReason: 'No endpoint-service API in CloudForge.' },
        { id: 'lattice-service-networks', label: 'Service Networks', group: 'PrivateLink and Lattice', disabledReason: 'No service-network API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice-services', label: 'Lattice Services', group: 'PrivateLink and Lattice', disabledReason: 'No Lattice service API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice-resource-configs', label: 'Resource Configurations', group: 'PrivateLink and Lattice', disabledReason: 'No Lattice resource-configuration API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice-resource-gateways', label: 'Resource Gateways', group: 'PrivateLink and Lattice', disabledReason: 'No Lattice resource-gateway API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice-target-groups', label: 'Target Groups', group: 'PrivateLink and Lattice', disabledReason: 'No Lattice target-group API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice-domain-verifications', label: 'Domain Verifications', group: 'PrivateLink and Lattice', disabledReason: 'No Lattice domain-verification API in CloudForge — Lattice is UNSUPPORTED.' },
        { id: 'lattice', label: 'Lattice', group: 'PrivateLink and Lattice' },
        { id: 'firewalls', label: 'Firewalls', group: 'Network Firewall', to: '/services/networkfirewall' },
        { id: 'nfw-policies', label: 'Firewall Policies', group: 'Network Firewall', disabledReason: 'No Network Firewall policy API in CloudForge.' },
        { id: 'nfw-rule-groups', label: 'Network Firewall Rule Groups', group: 'Network Firewall', disabledReason: 'No Network Firewall rule-group API in CloudForge.' },
        { id: 'nfw-tls', label: 'TLS Inspection Configurations', group: 'Network Firewall', disabledReason: 'No TLS inspection API in CloudForge.' },
        { id: 'nfw-resource-groups', label: 'Network Firewall Resource Groups', group: 'Network Firewall', disabledReason: 'No Network Firewall resource-group API in CloudForge.' },
        { id: 'nfw-endpoint-assoc', label: 'VPC Endpoint Associations', group: 'Network Firewall', disabledReason: 'No Network Firewall endpoint-association API in CloudForge.' },
        { id: 'nfw-containers', label: 'Container Associations', group: 'Network Firewall', disabledReason: 'No Network Firewall container-association API in CloudForge.' },
        { id: 'vpn', label: 'Site-to-Site VPN Connections', group: 'Virtual private network (VPN)' },
        { id: 'customer-gateways', label: 'Customer Gateways', group: 'Virtual private network (VPN)' },
        { id: 'vpn-gateways', label: 'Virtual Private Gateways', group: 'Virtual private network (VPN)' },
        { id: 'concentrators', label: 'Site-to-Site VPN Concentrators', group: 'Virtual private network (VPN)', disabledReason: 'No concentrator API in CloudForge — concentrators are not modelled.' },
        { id: 'client-vpn', label: 'Client VPN Endpoints', group: 'Virtual private network (VPN)', disabledReason: 'No Client VPN API in CloudForge — client VPN endpoints are not modelled.' },
        { id: 'verified-access', label: 'Verified Access', group: 'Verified Access' },
        { id: 'tgw', label: 'Transit Gateways', group: 'Transit Gateways' },
        { id: 'tgw-attachments', label: 'Transit Gateway Attachments', group: 'Transit Gateways', to: '/services/vpc?section=tgw' },
        { id: 'tgw-policy-tables', label: 'Transit Gateway Policy Tables', group: 'Transit Gateways', to: '/services/vpc?section=tgw' },
        { id: 'tgw-route-tables', label: 'Transit Gateway Route Tables', group: 'Transit Gateways', to: '/services/vpc?section=tgw' },
        { id: 'tgw-multicast', label: 'Transit Gateway Multicast', group: 'Transit Gateways', disabledReason: 'No transit-gateway multicast API in CloudForge.' },
        { id: 'tgw-metering', label: 'Metering Policies', group: 'Transit Gateways', disabledReason: 'No transit-gateway metering API in CloudForge.' },
        { id: 'mirroring', label: 'Mirror Sessions', group: 'Traffic Mirroring', disabledReason: 'No traffic-mirroring API in CloudForge.' },
        { id: 'mirror-targets', label: 'Mirror Targets', group: 'Traffic Mirroring', disabledReason: 'No traffic-mirroring API in CloudForge.' },
        { id: 'mirror-filters', label: 'Mirror Filters', group: 'Traffic Mirroring', disabledReason: 'No traffic-mirroring API in CloudForge.' },
        { id: 'flex-cost', label: 'Networking Flexible Cost Allocation', disabledReason: 'No flexible cost-allocation API in CloudForge.' },
        { id: 'network-manager', label: 'Network Manager', to: '/explorer' },
        { id: 'cloud-wan', label: 'Cloud WAN', group: 'Cloud WAN' },
        { id: 'ipam', label: 'VPC IP Address Manager', disabledReason: 'No IPAM API in CloudForge.' },
        { id: 'firewall-manager', label: 'Firewall Manager', disabledReason: 'No Firewall Manager console in CloudForge.' },
        { id: 'settings', label: 'Settings', disabledReason: 'No VPC console settings are stored in CloudForge.' },
      ]}
      activeSub={section}
      onSubChange={(id) => {
        setSection(id as Section)
        setSelectedId(null)
        setCheckedIds([])
        setVpcTab('vpcs')
      }}
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <label htmlFor="vpc-lab" className="text-sm font-medium">Lab</label>
        <select
          id="vpc-lab"
          value={activeLabId}
          onChange={(e) => {
            setLabId(e.target.value)
            setSelectedId(null)
          }}
          className="min-w-[20rem] max-w-full rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm"
        >
          {(labs.data ?? []).map((l) => (
            <option key={l.id} value={l.id}>{l.name} ({l.id}) — {l.status}</option>
          ))}
        </select>
        {activeLabId !== '' && (
          <Link to={`/labs/${activeLabId}/topology`} className="text-sm text-aws-link hover:underline">
            View lab topology (real VPC/subnet/route state)
          </Link>
        )}
      </div>
      {notice !== '' && (
        <AwsFlash tone={notice.toLowerCase().includes('failed') ? 'error' : 'success'}>{notice}</AwsFlash>
      )}
      {labs.isPending && <Loading label="Loading labs" />}
      {labs.data && labs.data.length === 0 && (
        <ErrorBox message="No labs yet — launch a lab first; VPCs live inside labs." />
      )}
      {resources.isPending && activeLabId !== '' && <Loading label="Loading VPC resources" />}
      {resources.isError && <ErrorBox message={`VPC resources could not be loaded (${resources.error.message}).`} />}
      {section === 'nacls' && (
        <div className="space-y-4">
          <AwsFlash tone="info">
            Network ACLs is the VPC console&apos;s Security section — it has no standalone console (2026-11-22
            survey). The old /services/nacl bookmark redirects here.
          </AwsFlash>
          <NaclSectionContent />
        </div>
      )}
      {section === 'dashboard' && (
        <ServiceHomeDashboard
          serviceName="VPC"
          banner="VPCs live inside labs — pick a lab above (the Filter by VPC equivalent), then create and manage its networking. CIDRs are local (10.x); there are no AWS availability zones."
          actions={[
            { label: 'Create VPC', primary: true, onClick: () => setSection('vpcs') },
            { label: 'Launch EC2 Instances', to: '/services/ec2' },
          ]}
          cards={[
            { label: 'Your VPCs', count: vpcs.length, onSelect: () => setSection('vpcs'), note: 'Lab VPCs on the Rust network engine' },
            { label: 'Subnets', count: all.filter((r) => r.kind === 'subnet').length, onSelect: () => setSection('subnets') },
            { label: 'Route Tables', count: all.filter((r) => r.kind === 'route-table').length, onSelect: () => setSection('routetables') },
            { label: 'Internet Gateways', count: all.filter((r) => r.kind === 'gateway' && cfgStr(r.config, 'gateway_type') !== 'nat').length, onSelect: () => setSection('gateways'), note: 'SIMULATED attach/detach — no engine IGW RPC' },
            { label: 'NAT Gateways', count: all.filter((r) => r.kind === 'gateway' && cfgStr(r.config, 'gateway_type') === 'nat').length, onSelect: () => setSection('nat'), note: 'Engine-backed; SIMULATED in lite mode' },
            { label: 'Security Groups', count: all.filter((r) => r.kind === 'security-group').length, onSelect: () => setSection('securitygroups') },
            { label: 'Network ACLs', count: '—', onSelect: () => setSection('nacls'), note: 'Numbered rules, really evaluated (lowest number first, default deny); ACLs filter no live traffic' },
            { label: 'Elastic IPs', count: 0, disabledReason: 'No Elastic IP operation in CloudForge.' },
            { label: 'VPC Endpoints', count: '—', to: '/services/vpcendpoints', note: 'PrivateLink-style endpoints (SIMULATED)' },
            { label: 'Transit Gateways', count: '—', onSelect: () => setSection('tgw'), note: 'Attachments are real route-table entries (SIMULATED)' },
            { label: 'Site-to-Site VPN Connections', count: vpnConnections.isPending ? '—' : vpnConnections.rows.length, onSelect: () => setSection('vpn'), note: 'API-EMULATED — customer gateways, VPN gateways and connections; tunnel state is a teaching field, no real IPsec tunnel.' },
          ]}
          onRefresh={refresh}
          refreshing={resources.isFetching}
          health={[
            labs.isError
              ? 'Labs could not be loaded — VPC counts may be stale.'
              : `${labs.data?.length ?? 0} lab(s) in this account.`,
            resources.isError
              ? 'VPC resources could not be loaded.'
              : 'VPC resources loaded from the lab store.',
            'Network engine: SIMULATED backend by default; the real netlink backend is unverified in this sandbox.',
          ]}
          settings={[
            { label: 'VPC console settings', to: '/services/vpc' },
            { label: 'Resource Explorer', to: '/explorer' },
          ]}
          info={[
            { label: 'Learning Center — networking track', to: '/learn' },
            { label: 'All services', to: '/services' },
            { label: 'Lab topology', to: activeLabId !== '' ? `/labs/${activeLabId}/topology` : '/labs' },
          ]}
        />
      )}
      {section === 'vpn' && <VpnConnectionsSection onNotice={setNotice} />}

      {section === 'customer-gateways' && <CustomerGatewaysSection onNotice={setNotice} />}

      {section === 'vpn-gateways' && <VpnGatewaysSection onNotice={setNotice} />}

      {section === 'verified-access' && (
        <UnsupportedSection
          title="Verified Access"
          reason="Verified Access (trust providers, groups and endpoints) is UNSUPPORTED in CloudForge — there is no Verified Access API and no identity-aware access evaluation."
          actionLabel="Create Verified Access instance"
        />
      )}

      {section === 'cloud-wan' && (
        <UnsupportedSection
          title="Cloud WAN"
          reason="Cloud WAN (core networks, segments and attachments) is UNSUPPORTED in CloudForge — there is no Cloud WAN API and no global-network record."
          actionLabel="Create core network"
        />
      )}

      {section === 'lattice' && (
        <UnsupportedSection
          title="Lattice"
          reason="Lattice (service networks, services and listeners) is UNSUPPORTED in CloudForge — there is no service-network API."
          actionLabel="Create service network"
        />
      )}

      {section === 'dns-firewall' && (
        <UnsupportedSection
          title="DNS Firewall"
          reason="DNS Firewall (rule groups and domain lists) is UNSUPPORTED in CloudForge — there is no DNS Firewall API and no DNS filtering."
          actionLabel="Create rule group"
        />
      )}

      {section === 'tgw' && (
        <div className="space-y-4">
          <AwsFlash tone="info">
            Transit Gateway is the VPC console&apos;s Transit Gateways section — it has no standalone console
            (2026-11-22 survey). The old /services/transitgateway bookmark redirects here.
          </AwsFlash>
          <TransitGatewayListContent />
        </div>
      )}

      {section !== 'nacls' && section !== 'dashboard' && section !== 'vpn' && section !== 'customer-gateways' && section !== 'vpn-gateways' && section !== 'verified-access' && section !== 'cloud-wan' && section !== 'lattice' && section !== 'dns-firewall' && section !== 'tgw' && activeLabId !== '' && (
        <>
          {resources.data && (
            <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
              <AwsStat label="VPCs" value={vpcs.length} hint="This lab" />
              <AwsStat label="Subnets" value={all.filter((r) => r.kind === 'subnet').length} />
              <AwsStat label="Route tables" value={all.filter((r) => r.kind === 'route-table').length} />
              <AwsStat label="Security groups" value={all.filter((r) => r.kind === 'security-group').length} />
            </div>
          )}
          {section === 'vpcs' ? (
            <CreateVpcWizard labId={activeLabId} onCreated={refresh} onNotice={setNotice} />
          ) : (
            <CreateForm
              section={section}
              labId={activeLabId}
              vpcs={vpcs}
              gateways={all.filter((r) => r.kind === 'gateway')}
              onCreated={refresh}
              onNotice={setNotice}
            />
          )}
          {section === 'vpcs' && (
            <ConsoleTabs
              tabs={[
                { id: 'vpcs', label: 'VPCs' },
                { id: 'encryption-controls', label: 'VPC encryption controls' },
              ]}
              active={vpcTab}
              onChange={(id) => setVpcTab(id as 'vpcs' | 'encryption-controls')}
            />
          )}
          {section === 'vpcs' && vpcTab === 'encryption-controls' ? (
            <UnsupportedSection
              title="VPC encryption controls"
              reason="VPC encryption controls are UNSUPPORTED in CloudForge — there is no encryption-control API and no control is recorded for any VPC. The None / Monitor / Enforce choice in the Create VPC wizard is recorded as a VPC tag only; nothing is enforced."
              actionLabel="Create encryption control"
            />
          ) : (
          <AwsCard title={`${sectionLabel(section)} (${rows.length}) — region ${region}`}>
            <AwsToolbar
              right={
                <>
                  <AwsRefreshButton onClick={refresh} busy={resources.isFetching} />
                  <AwsPreferencesButton disabledReason="Table preferences are not persisted in CloudForge." />
                </>
              }
            >
              <AwsActionsMenu
                label="Actions"
                items={[
                  {
                    label: 'Delete selected',
                    danger: true,
                    onClick: () => selected && del.mutate(selected),
                    disabledReason: selected === null ? 'Select a resource first' : undefined,
                  },
                  {
                    label: 'View lab topology',
                    onClick: () => {
                      if (activeLabId !== '') window.location.assign(`/labs/${activeLabId}/topology`)
                    },
                    disabledReason: activeLabId === '' ? 'No lab selected' : undefined,
                  },
                ]}
              />
            </AwsToolbar>
            <AwsTable<LabResource & { id: string }>
              caption={sectionLabel(section)}
              emptyMessage={`No ${sectionLabel(section).toLowerCase()} recorded in this lab yet — create one above.`}
              rows={rows}
              totalCount={rows.length}
              selectable
              selectedIds={checkedIds}
              onToggleRow={(id) => {
                setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))
                setSelectedId(id)
              }}
              onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
              columns={tableColumns}
            />
            {selected && (
              <ResourceDetail
                key={selected.id}
                resource={selected}
                labId={activeLabId}
                all={all}
                onChanged={refresh}
                onDelete={() => del.mutate(selected)}
                onNotice={setNotice}
              />
            )}
          </AwsCard>
          )}
        </>
      )}
    </ServiceConsole>
  )
}

type NaclRec = Record<string, unknown> & { id: string }
const naclStr = (r: Record<string, unknown>, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')
const naclNum = (r: Record<string, unknown>, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)
const naclRowsOf = (v: unknown, key: string): NaclRec[] => {
  const arr = Array.isArray((v as NaclRec)?.[key]) ? ((v as NaclRec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}
const naclInputCls = 'rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm focus:border-aws-link focus:outline-none'

/**
 * Network ACLs section (VPC console, Security group) — the real
 * exam backend: GET/POST /api/v1/aws/nacl/acls, numbered rules per
 * direction, and POST .../evaluate, which really evaluates a packet
 * (lowest rule number first, stateless, default deny). The survey
 * records no NACL list columns (the VPC nav item only), so the ACL
 * table columns are CloudForge's own. The ACL is not attached to
 * any subnet and filters no live traffic — the UI says so.
 */
function NaclSectionContent() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState('')
  const [ruleNumber, setRuleNumber] = useState(100)
  const [ruleDirection, setRuleDirection] = useState<'ingress' | 'egress'>('ingress')
  const [ruleProtocol, setRuleProtocol] = useState('tcp')
  const [portFrom, setPortFrom] = useState(0)
  const [portTo, setPortTo] = useState(65535)
  const [ruleCidr, setRuleCidr] = useState('0.0.0.0/0')
  const [ruleAction, setRuleAction] = useState<'allow' | 'deny'>('allow')
  const [evDirection, setEvDirection] = useState<'ingress' | 'egress'>('ingress')
  const [evProtocol, setEvProtocol] = useState('tcp')
  const [evPort, setEvPort] = useState(443)
  const [evIp, setEvIp] = useState('10.0.0.5')
  const [evResult, setEvResult] = useState<Record<string, unknown> | null>(null)

  const acls = useQuery({ queryKey: ['nacl-acls'], queryFn: () => request<NaclRec>('/api/v1/aws/nacl/acls'), retry: 1 })
  const aclRows = naclRowsOf(acls.data, 'acls')
  const rules = useQuery({
    queryKey: ['nacl-rules', selectedId],
    queryFn: () => request<NaclRec>(`/api/v1/aws/nacl/acls/${selectedId}/rules`),
    enabled: selectedId !== '',
    retry: 1,
  })
  const ruleRows: NaclRec[] = Array.isArray(rules.data?.rules)
    ? (rules.data.rules as Record<string, unknown>[]).map((r) => ({
        ...r,
        id: `${naclStr(r, 'direction')}-${naclNum(r, 'rule_number')}`,
      }))
    : []
  const sel = aclRows.find((a) => a.id === selectedId) ?? null
  const refreshAcls = () => { void queryClient.invalidateQueries({ queryKey: ['nacl-acls'] }) }
  const refreshRules = () => { void queryClient.invalidateQueries({ queryKey: ['nacl-rules', selectedId] }) }
  const create = useMutation({
    mutationFn: () => request<NaclRec>('/api/v1/aws/nacl/acls', { method: 'POST', body: { name: name.trim() } }),
    onSuccess: () => { setNotice('Network ACL created.'); setName(''); refreshAcls() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Record<string, unknown>>(`/api/v1/aws/nacl/acls/${id}`, { method: 'DELETE' }),
    onSuccess: (_d, id) => { setNotice('Network ACL deleted.'); if (id === selectedId) setSelectedId(''); refreshAcls() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const addRule = useMutation({
    mutationFn: () =>
      request<NaclRec>(`/api/v1/aws/nacl/acls/${selectedId}/rules`, {
        method: 'POST',
        body: { rule_number: ruleNumber, direction: ruleDirection, protocol: ruleProtocol, port_from: portFrom, port_to: portTo, cidr: ruleCidr.trim(), action: ruleAction },
      }),
    onSuccess: () => { setNotice('Rule added.'); refreshRules(); refreshAcls() },
    onError: (e: Error) => setNotice(`Add rule failed: ${e.message}`),
  })
  const delRule = useMutation({
    mutationFn: (r: NaclRec) =>
      request<Record<string, unknown>>(`/api/v1/aws/nacl/acls/${selectedId}/rules/${naclNum(r, 'rule_number')}?direction=${naclStr(r, 'direction')}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Rule deleted.'); refreshRules(); refreshAcls() },
    onError: (e: Error) => setNotice(`Delete rule failed: ${e.message}`),
  })
  const evaluate = useMutation({
    mutationFn: () =>
      request<Record<string, unknown>>(`/api/v1/aws/nacl/acls/${selectedId}/evaluate`, {
        method: 'POST',
        body: { direction: evDirection, protocol: evProtocol, port: evPort, source_ip: evIp.trim() },
      }),
    onSuccess: (d) => { setEvResult(d); setNotice('') },
    onError: (e: Error) => setNotice(`Evaluate failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      {notice !== '' && (
        <AwsFlash tone={notice.toLowerCase().includes('failed') ? 'error' : 'success'}>{notice}</AwsFlash>
      )}
      <AwsCard title={`Network ACLs (${aclRows.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          Stateless ACLs with numbered rules, really evaluated by the backend (lowest rule number first match,
          ingress and egress evaluated independently, default deny). An ACL is not attached to any subnet and
          filters no live traffic — evaluation describes a packet you supply. Use security groups for lab
          firewalling that applies to instances.
        </p>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="ACL name" className={naclInputCls} aria-label="ACL name" />
          <AwsButton variant="primary" type="submit">Create network ACL</AwsButton>
        </form>
        {acls.isPending && <Loading label="Loading network ACLs" />}
        {acls.isError && <ErrorBox message={`Network ACLs could not be loaded (${acls.error.message}).`} />}
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => naclStr(r, 'name') },
            { header: 'ID', render: (r) => <span className="font-mono text-xs">{r.id}</span> },
            { header: 'Status', render: (r) => naclStr(r, 'status') || '—' },
            { header: 'Rules', render: (r) => String(Array.isArray(r.rules) ? (r.rules as unknown[]).length : 0) },
            { header: 'Created', render: (r) => naclStr(r, 'created_at') || '—' },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => { setSelectedId(r.id); setEvResult(null) }}>Rules</AwsButton>
                  <AwsButton onClick={() => del.mutate(r.id)}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={aclRows}
          emptyMessage="No network ACLs yet — create one above."
        />
        <p className="mt-2 text-xs text-aws-muted">
          The survey records no AWS Network ACL list columns, so these columns are CloudForge&apos;s own.
        </p>
      </AwsCard>
      {sel && (
        <>
          <AwsCard title={`Rules: ${naclStr(sel, 'name')}`}>
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); addRule.mutate() }}
            >
              <label className="text-sm">
                Rule number
                <input type="number" min={1} max={32766} value={ruleNumber} onChange={(e) => setRuleNumber(Number(e.target.value))} className={`ml-2 w-20 ${naclInputCls}`} />
              </label>
              <label className="text-sm">
                Direction
                <select value={ruleDirection} onChange={(e) => setRuleDirection(e.target.value as 'ingress' | 'egress')} className={`ml-2 ${naclInputCls}`}>
                  <option value="ingress">Ingress</option>
                  <option value="egress">Egress</option>
                </select>
              </label>
              <label className="text-sm">
                Protocol
                <select value={ruleProtocol} onChange={(e) => setRuleProtocol(e.target.value)} className={`ml-2 ${naclInputCls}`}>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="icmp">ICMP</option>
                  <option value="all">All</option>
                </select>
              </label>
              <label className="text-sm">
                Ports
                <input type="number" min={0} max={65535} value={portFrom} onChange={(e) => setPortFrom(Number(e.target.value))} className={`ml-2 w-20 ${naclInputCls}`} aria-label="Port from" />
                <span className="mx-1">–</span>
                <input type="number" min={0} max={65535} value={portTo} onChange={(e) => setPortTo(Number(e.target.value))} className={`w-20 ${naclInputCls}`} aria-label="Port to" />
              </label>
              <label className="text-sm">
                CIDR
                <input value={ruleCidr} onChange={(e) => setRuleCidr(e.target.value)} className={`ml-2 ${naclInputCls}`} />
              </label>
              <label className="text-sm">
                Action
                <select value={ruleAction} onChange={(e) => setRuleAction(e.target.value as 'allow' | 'deny')} className={`ml-2 ${naclInputCls}`}>
                  <option value="allow">Allow</option>
                  <option value="deny">Deny</option>
                </select>
              </label>
              <AwsButton variant="primary" type="submit">Add rule</AwsButton>
            </form>
            <p className="mb-2 text-xs text-aws-muted">Rule numbers are unique per direction (1–32766); the lowest number matching a packet wins.</p>
            {rules.isPending && <Loading label="Loading rules" />}
            {rules.isError && <ErrorBox message={`Rules could not be loaded (${rules.error.message}).`} />}
            <AwsTable
              columns={[
                { header: 'Rule number', render: (r) => String(naclNum(r, 'rule_number')) },
                { header: 'Direction', render: (r) => naclStr(r, 'direction') },
                { header: 'Protocol', render: (r) => naclStr(r, 'protocol') },
                { header: 'Ports', render: (r) => `${naclNum(r, 'port_from')}–${naclNum(r, 'port_to')}` },
                { header: 'CIDR', render: (r) => <span className="font-mono text-xs">{naclStr(r, 'cidr')}</span> },
                { header: 'Action', render: (r) => naclStr(r, 'action') },
                { header: 'Actions', render: (r) => <AwsButton onClick={() => delRule.mutate(r)}>Delete</AwsButton> },
              ]}
              rows={ruleRows}
              emptyMessage="No rules yet — add one above."
            />
          </AwsCard>
          <AwsCard title={`Evaluate packet: ${naclStr(sel, 'name')}`}>
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); evaluate.mutate() }}
            >
              <label className="text-sm">
                Direction
                <select value={evDirection} onChange={(e) => setEvDirection(e.target.value as 'ingress' | 'egress')} className={`ml-2 ${naclInputCls}`}>
                  <option value="ingress">Ingress</option>
                  <option value="egress">Egress</option>
                </select>
              </label>
              <label className="text-sm">
                Protocol
                <select value={evProtocol} onChange={(e) => setEvProtocol(e.target.value)} className={`ml-2 ${naclInputCls}`}>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="icmp">ICMP</option>
                </select>
              </label>
              <label className="text-sm">
                Port
                <input type="number" min={0} max={65535} value={evPort} onChange={(e) => setEvPort(Number(e.target.value))} className={`ml-2 w-20 ${naclInputCls}`} />
              </label>
              <label className="text-sm">
                Source IP
                <input value={evIp} onChange={(e) => setEvIp(e.target.value)} className={`ml-2 ${naclInputCls}`} />
              </label>
              <AwsButton variant="primary" type="submit">Evaluate</AwsButton>
            </form>
            {evResult && (
              <dl className="space-y-1 text-sm">
                <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">Decision</dt><dd><strong>{evResult.allowed === true ? 'ALLOW' : 'DENY'}</strong></dd></div>
                <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">Matched rule number</dt><dd>{typeof evResult.matched_rule_number === 'number' && (evResult.matched_rule_number as number) >= 0 ? String(evResult.matched_rule_number) : 'No rule matched — default deny'}</dd></div>
                <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">Action</dt><dd>{typeof evResult.action === 'string' ? (evResult.action as string) : '—'}</dd></div>
                {typeof evResult.note === 'string' && evResult.note !== '' && (
                  <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">Note</dt><dd className="text-aws-muted">{evResult.note as string}</dd></div>
                )}
              </dl>
            )}
            {!evResult && <p className="text-sm text-aws-muted">Describe a packet above — the backend evaluates it against this ACL&apos;s stored rules.</p>}
          </AwsCard>
        </>
      )}
    </div>
  )
}

function sectionLabel(section: Section): string {
  switch (section) {
    case 'dashboard': return 'VPC dashboard'
    case 'vpcs': return 'Your VPCs'
    case 'subnets': return 'Subnets'
    case 'routetables': return 'Route Tables'
    case 'gateways': return 'Internet Gateways'
    case 'nat': return 'NAT Gateways'
    case 'securitygroups': return 'Security Groups'
    case 'nacls': return 'Network ACLs'
    case 'vpn': return 'Site-to-Site VPN Connections'
    case 'customer-gateways': return 'Customer Gateways'
    case 'vpn-gateways': return 'Virtual Private Gateways'
    case 'verified-access': return 'Verified Access'
    case 'cloud-wan': return 'Cloud WAN'
    case 'lattice': return 'Lattice'
    case 'dns-firewall': return 'DNS Firewall'
    case 'tgw': return 'Transit Gateways'
  }
}

/**
 * Create VPC wizard — the observed AWS wizard steps (v4 survey,
 * 2026-11-22, all left via Cancel, nothing submitted): Resources to
 * create (VPC only / VPC and more), Name tag, IPv4 CIDR (manual or
 * IPAM), IPv6 options, Tenancy, VPC encryption control, Tags.
 * "VPC and more" uses the survey's observed defaults: name
 * "project", 10.0.0.0/16, 2 AZs, 2 public + 2 private subnets, NAT
 * None, S3 Gateway endpoint, DNS hostnames + resolution checked.
 * What it really creates (all through the real lab-resources API):
 * the VPC, plus for "VPC and more" on a /16 CIDR the 4 subnets, 1
 * internet gateway and 3 route tables. There is no IPAM, no IPv6,
 * no dedicated tenancy, no encryption-control API and no gateway
 * endpoint in CloudForge labs, so those choices are disabled or
 * recorded as VPC tags only — the wizard says so at each step, and
 * the success notice lists what was NOT created (S3 Gateway
 * endpoint, DNS hostnames/resolution — not modelled).
 */
const VPC_WIZARD_STEPS = [
  'Resources to create',
  'Name tag',
  'IPv4 CIDR',
  'IPv6 options',
  'Tenancy',
  'VPC encryption control',
  'Tags',
]

function CreateVpcWizard({ labId, onCreated, onNotice }: {
  labId: string
  onCreated: () => void
  onNotice: (s: string) => void
}) {
  const [step, setStep] = useState(0)
  const [mode, setMode] = useState<'only' | 'more'>('only')
  const [name, setName] = useState('')
  const [cidr, setCidr] = useState('10.0.0.0/16')
  const [cidrSource, setCidrSource] = useState<'manual' | 'ipam'>('manual')
  const [ipv6, setIpv6] = useState<'none' | 'ipam' | 'amazon' | 'owned'>('none')
  const [tenancy, setTenancy] = useState<'default' | 'dedicated'>('default')
  const [encControl, setEncControl] = useState<'none' | 'monitor' | 'enforce'>('none')
  const [azs, setAzs] = useState(2)
  const [publicSubnets, setPublicSubnets] = useState(2)
  const [privateSubnets, setPrivateSubnets] = useState(2)
  const [nat, setNat] = useState<'none' | 'one' | 'per-az'>('none')
  const [s3Endpoint, setS3Endpoint] = useState(true)
  const [dnsHostnames, setDnsHostnames] = useState(true)
  const [dnsResolution, setDnsResolution] = useState(true)
  const [tags, setTags] = useState<{ key: string; value: string }[]>([])
  const [busy, setBusy] = useState(false)
  const inputCls =
    'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'

  const reset = () => {
    setStep(0)
    setMode('only')
    setName('')
    setCidr('10.0.0.0/16')
    setCidrSource('manual')
    setIpv6('none')
    setTenancy('default')
    setEncControl('none')
    setAzs(2)
    setPublicSubnets(2)
    setPrivateSubnets(2)
    setNat('none')
    setS3Endpoint(true)
    setDnsHostnames(true)
    setDnsResolution(true)
    setTags([])
  }

  const submit = async () => {
    const vpcName = name.trim()
    if (vpcName === '') {
      onNotice('Create failed: enter a VPC name tag first.')
      setStep(1)
      return
    }
    if (cidr.trim() === '') {
      onNotice('Create failed: enter an IPv4 CIDR first.')
      setStep(2)
      return
    }
    setBusy(true)
    try {
      const vpc = await createLabResource(labId, { kind: 'vpc', name: vpcName, cidr: cidr.trim() })
      const created: string[] = [`VPC "${vpc.name}" (${vpc.cidr ?? cidr.trim()})`]
      const skipped: string[] = []
      if (mode === 'more') {
        const m = cidr.trim().match(/^(\d+)\.(\d+)\.0\.0\/16$/)
        if (m) {
          const base = `${m[1]}.${m[2]}`
          const subnetSpecs: { name: string; cidr: string }[] = []
          for (let i = 0; i < publicSubnets; i += 1) {
            subnetSpecs.push({ name: `${vpcName}-public-${i + 1}`, cidr: `${base}.${i + 1}.0/24` })
          }
          for (let i = 0; i < privateSubnets; i += 1) {
            subnetSpecs.push({ name: `${vpcName}-private-${i + 1}`, cidr: `${base}.${101 + i}.0/24` })
          }
          for (const spec of subnetSpecs) {
            try {
              await createLabResource(labId, { kind: 'subnet', name: spec.name, cidr: spec.cidr, parent_id: vpc.id })
              created.push(`subnet ${spec.name} (${spec.cidr})`)
            } catch (e) {
              skipped.push(`subnet ${spec.name} (${e instanceof Error ? e.message : 'create failed'})`)
            }
          }
          try {
            await createLabResource(labId, { kind: 'gateway', name: `${vpcName}-igw`, config: { gateway_type: 'igw' } })
            created.push(`internet gateway ${vpcName}-igw`)
          } catch (e) {
            skipped.push(`internet gateway (${e instanceof Error ? e.message : 'create failed'})`)
          }
          const rtNames = [`${vpcName}-public-rt`, `${vpcName}-private-rt-1`, `${vpcName}-private-rt-2`]
          for (const rtName of rtNames) {
            try {
              await createLabResource(labId, { kind: 'route-table', name: rtName, parent_id: vpc.id })
              created.push(`route table ${rtName}`)
            } catch (e) {
              skipped.push(`route table ${rtName} (${e instanceof Error ? e.message : 'create failed'})`)
            }
          }
        } else {
          skipped.push('subnets, internet gateway and route tables need a /16 CIDR (e.g. 10.0.0.0/16) — only the VPC was created')
        }
        if (nat !== 'none') skipped.push('NAT gateway — NAT gateways are created separately on the NAT Gateways page')
        if (s3Endpoint) skipped.push('S3 Gateway endpoint — no gateway-endpoint API in CloudForge labs')
        if (dnsHostnames || dnsResolution) skipped.push('DNS hostnames / DNS resolution — not modelled for lab VPCs')
      }
      const tagRecord: Record<string, string> = {}
      for (const t of tags) {
        if (t.key.trim() !== '') tagRecord[t.key.trim()] = t.value
      }
      if (encControl !== 'none') tagRecord['encryption-control'] = encControl
      if (Object.keys(tagRecord).length > 0) {
        try {
          await setResourceTags(labId, vpc.id, tagRecord)
        } catch (e) {
          skipped.push(`tags (${e instanceof Error ? e.message : 'tag save failed'})`)
        }
      }
      onNotice(
        `Created: ${created.join('; ')}.${skipped.length > 0 ? ` Not created: ${skipped.join('; ')}.` : ''}`,
      )
      onCreated()
      reset()
    } catch (e) {
      onNotice(`Create failed: ${e instanceof Error ? e.message : 'unknown error'}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AwsWizard
      title="Create VPC"
      steps={VPC_WIZARD_STEPS}
      current={step}
      onStep={setStep}
      onCancel={reset}
      onSubmit={() => void submit()}
      submitLabel="Create VPC"
      busy={busy}
    >
      {step === 0 && (
        <div className="space-y-3 text-sm">
          <p className="text-aws-muted">
            Choose what to create. &ldquo;VPC and more&rdquo; also creates subnets, an internet gateway and route
            tables from the defaults below — all real lab resources.
          </p>
          <label className="flex items-start gap-2">
            <input type="radio" name="vpc-mode" checked={mode === 'only'} onChange={() => setMode('only')} className="mt-1" />
            <span><strong>VPC only</strong> — just the VPC, nothing else.</span>
          </label>
          <label className="flex items-start gap-2">
            <input
              type="radio"
              name="vpc-mode"
              checked={mode === 'more'}
              onChange={() => {
                setMode('more')
                if (name.trim() === '') setName('project')
              }}
              className="mt-1"
            />
            <span><strong>VPC and more</strong> — VPC plus subnets, an internet gateway and route tables (survey defaults).</span>
          </label>
          {mode === 'more' && (
            <div className="space-y-2 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
              <div className="flex flex-wrap gap-3">
                <label>
                  Number of Availability Zones
                  <select value={azs} onChange={(e) => setAzs(Number(e.target.value))} className={`ml-2 ${inputCls}`}>
                    <option value={1}>1</option>
                    <option value={2}>2</option>
                  </select>
                </label>
                <label>
                  Public subnets
                  <select value={publicSubnets} onChange={(e) => setPublicSubnets(Number(e.target.value))} className={`ml-2 ${inputCls}`}>
                    <option value={0}>0</option>
                    <option value={1}>1</option>
                    <option value={2}>2</option>
                  </select>
                </label>
                <label>
                  Private subnets
                  <select value={privateSubnets} onChange={(e) => setPrivateSubnets(Number(e.target.value))} className={`ml-2 ${inputCls}`}>
                    <option value={0}>0</option>
                    <option value={1}>1</option>
                    <option value={2}>2</option>
                  </select>
                </label>
              </div>
              <p className="text-xs text-aws-muted">
                CloudForge labs have no availability zones — subnets are created in the lab without an AZ
                assignment. Preview: {publicSubnets + privateSubnets} subnets, 3 route tables, 2 connections
                (internet gateway, S3 gateway endpoint — the endpoint itself is not created, see below).
              </p>
              <label className="block">
                NAT gateways
                <select value={nat} onChange={(e) => setNat(e.target.value as 'none' | 'one' | 'per-az')} className={`ml-2 ${inputCls}`}>
                  <option value="none">None</option>
                  <option value="one">1 NAT gateway</option>
                  <option value="per-az">1 per AZ</option>
                </select>
                <span className="ml-2 text-xs text-aws-muted">NAT gateways are created separately on the NAT Gateways page.</span>
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={s3Endpoint} onChange={(e) => setS3Endpoint(e.target.checked)} />
                S3 Gateway endpoint (noted, not created — no gateway-endpoint API in CloudForge labs)
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={dnsHostnames} onChange={(e) => setDnsHostnames(e.target.checked)} />
                Enable DNS hostnames (not modelled for lab VPCs)
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={dnsResolution} onChange={(e) => setDnsResolution(e.target.checked)} />
                Enable DNS resolution (not modelled for lab VPCs)
              </label>
            </div>
          )}
        </div>
      )}
      {step === 1 && (
        <div className="space-y-3 text-sm">
          <label className="block">
            <span className="mb-1 block font-medium">Name tag</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="project" className={`w-full max-w-md ${inputCls}`} />
          </label>
          <p className="text-aws-muted">The VPC&apos;s Name tag. &ldquo;VPC and more&rdquo; defaults the name to <strong>project</strong>.</p>
        </div>
      )}
      {step === 2 && (
        <div className="space-y-3 text-sm">
          <label className="flex items-start gap-2">
            <input type="radio" name="cidr-source" checked={cidrSource === 'manual'} onChange={() => setCidrSource('manual')} className="mt-1" />
            <span>
              <strong>IPv4 CIDR block — manual input</strong>
              <input value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.0.0.0/16" className={`ml-2 font-mono ${inputCls}`} />
              <span className="ml-2 text-xs text-aws-muted">/16–/28. The backend validates the CIDR (400 otherwise).</span>
            </span>
          </label>
          <label className="flex items-start gap-2 opacity-60">
            <input type="radio" name="cidr-source" checked={cidrSource === 'ipam'} disabled className="mt-1" />
            <span><strong>IPv4 CIDR block — IPAM-allocated</strong> — no IPAM API in CloudForge, so IPAM allocation is unavailable.</span>
          </label>
        </div>
      )}
      {step === 3 && (
        <div className="space-y-3 text-sm">
          <p className="text-aws-muted">Lab VPCs are IPv4-only; IPv6 is not modelled in CloudForge.</p>
          <label className="flex items-start gap-2">
            <input type="radio" name="ipv6" checked={ipv6 === 'none'} onChange={() => setIpv6('none')} className="mt-1" />
            <span><strong>No IPv6 CIDR block</strong></span>
          </label>
          {(['ipam', 'amazon', 'owned'] as const).map((opt) => (
            <label key={opt} className="flex items-start gap-2 opacity-60">
              <input type="radio" name="ipv6" checked={ipv6 === opt} disabled className="mt-1" />
              <span>
                <strong>{opt === 'ipam' ? 'IPAM-allocated IPv6 CIDR block' : opt === 'amazon' ? 'Amazon-provided IPv6 CIDR block' : 'IPv6 CIDR owned by me'}</strong>
                {' — '}not available: no IPv6 in CloudForge labs.
              </span>
            </label>
          ))}
        </div>
      )}
      {step === 4 && (
        <div className="space-y-3 text-sm">
          <label className="flex items-start gap-2">
            <input type="radio" name="tenancy" checked={tenancy === 'default'} onChange={() => setTenancy('default')} className="mt-1" />
            <span><strong>Default</strong> — lab VPCs run on shared lab capacity.</span>
          </label>
          <label className="flex items-start gap-2 opacity-60">
            <input type="radio" name="tenancy" checked={tenancy === 'dedicated'} disabled className="mt-1" />
            <span><strong>Dedicated</strong> — no dedicated tenancy in CloudForge labs.</span>
          </label>
        </div>
      )}
      {step === 5 && (
        <div className="space-y-3 text-sm">
          <p className="text-aws-muted">
            CloudForge has no VPC encryption-control API. A Monitor or Enforce choice is recorded as a VPC tag
            only — nothing is enforced.
          </p>
          {(['none', 'monitor', 'enforce'] as const).map((opt) => (
            <label key={opt} className="flex items-start gap-2">
              <input type="radio" name="enc-control" checked={encControl === opt} onChange={() => setEncControl(opt)} className="mt-1" />
              <span>
                <strong>{opt === 'none' ? 'None' : opt === 'monitor' ? 'Monitor' : 'Enforce'}</strong>
                {opt === 'enforce' && ' — recorded as a tag only; enforcement does not exist in CloudForge.'}
                {opt === 'monitor' && ' — recorded as a tag only; no monitoring is performed.'}
              </span>
            </label>
          ))}
        </div>
      )}
      {step === 6 && (
        <div className="space-y-3 text-sm">
          <p className="text-aws-muted">Tags are saved on the VPC with the real tags API when it is created.</p>
          {tags.map((t, i) => (
            <div key={i} className="flex flex-wrap items-center gap-2">
              <input
                value={t.key}
                onChange={(e) => setTags((prev) => prev.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))}
                placeholder="Key"
                className={inputCls}
                aria-label="Tag key"
              />
              <input
                value={t.value}
                onChange={(e) => setTags((prev) => prev.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
                placeholder="Value"
                className={inputCls}
                aria-label="Tag value"
              />
              <AwsButton onClick={() => setTags((prev) => prev.filter((_, j) => j !== i))}>Remove</AwsButton>
            </div>
          ))}
          <div>
            <AwsButton onClick={() => setTags((prev) => [...prev, { key: '', value: '' }])}>Add tag</AwsButton>
          </div>
          <div className="rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="font-semibold">Summary</h3>
            <p>
              VPC <strong>{name.trim() === '' ? '(no name yet)' : name.trim()}</strong> — {cidr.trim() === '' ? '(no CIDR yet)' : cidr.trim()}
              {mode === 'more'
                ? `, plus ${publicSubnets} public + ${privateSubnets} private subnets, 1 internet gateway and 3 route tables (needs a /16 CIDR).`
                : ' — VPC only.'}
            </p>
            <p className="text-xs text-aws-muted">
              Not created: S3 Gateway endpoint and DNS hostnames/resolution (not modelled); NAT gateways are
              created separately. Encryption control {encControl === 'none' ? 'None' : encControl} is recorded as a
              tag only.
            </p>
          </div>
        </div>
      )}
    </AwsWizard>
  )
}

function CreateForm({ section, labId, vpcs, gateways, onCreated, onNotice }: {
  section: Section
  labId: string
  vpcs: LabResource[]
  gateways: LabResource[]
  onCreated: () => void
  onNotice: (s: string) => void
}) {
  const [name, setName] = useState('')
  const [cidr, setCidr] = useState('10.0.0.0/16')
  const [parentId, setParentId] = useState('')
  const [gatewayType, setGatewayType] = useState<'igw' | 'nat'>('igw')
  const create = useMutation({
    mutationFn: () => {
      switch (section) {
        case 'vpcs':
          return createLabResource(labId, { kind: 'vpc', name: name.trim(), cidr: cidr.trim() })
        case 'subnets':
          return createLabResource(labId, { kind: 'subnet', name: name.trim(), cidr: cidr.trim(), parent_id: parentId })
        case 'routetables':
          return createLabResource(labId, { kind: 'route-table', name: name.trim(), parent_id: parentId })
        case 'gateways':
        case 'nat':
          return createLabResource(labId, { kind: 'gateway', name: name.trim(), config: { gateway_type: section === 'nat' ? 'nat' : gatewayType } })
        case 'securitygroups':
          return createLabResource(labId, { kind: 'security-group', name: name.trim() })
        case 'nacls':
          throw new Error('Network ACLs are created in the Network ACLs section, not this form.')
        case 'customer-gateways':
        case 'vpn-gateways':
          throw new Error('Gateways are created in their own VPN section, not this form.')
        case 'verified-access':
        case 'cloud-wan':
        case 'lattice':
        case 'dns-firewall':
          throw new Error('This section has no create form — there is no backend for it in CloudForge.')
        case 'vpn':
          throw new Error('VPN connections are created in the VPN section, not this form.')
        case 'tgw':
          throw new Error('Transit gateways are created on the Transit Gateways section, not this form.')
        case 'dashboard':
          throw new Error('The dashboard has no create form.')
      }
    },
    onSuccess: (r) => {
      onNotice(`${sectionLabel(section).slice(0, -1)} "${r.name}" created.`)
      setName('')
      onCreated()
    },
    onError: (e: Error) => onNotice(`Create failed: ${e.message}`),
  })
  if (section === 'nacls') return null
  const needsParent = section === 'subnets' || section === 'routetables'
  return (
    <AwsCard title={`Create ${sectionLabel(section).slice(0, -1).toLowerCase()}`}>
      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim() !== '') create.mutate()
        }}
      >
        <label htmlFor="vpc-create-name" className="text-sm font-medium">Name</label>
        <input id="vpc-create-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="main" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
        {(section === 'vpcs' || section === 'subnets') && (
          <label className="text-sm">
            CIDR
            <input value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.0.0.0/16" className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm" />
          </label>
        )}
        {needsParent && (
          <label className="text-sm">
            VPC
            <select value={parentId} onChange={(e) => setParentId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="">Select a VPC…</option>
              {vpcs.map((v) => (<option key={v.id} value={v.id}>{v.name} ({v.cidr ?? ''})</option>))}
            </select>
          </label>
        )}
        {section === 'gateways' && (
          <label className="text-sm">
            Type
            <select value={gatewayType} onChange={(e) => setGatewayType(e.target.value as 'igw' | 'nat')} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="igw">Internet gateway</option>
              <option value="nat">NAT gateway (engine-backed)</option>
            </select>
          </label>
        )}
        <AwsButton type="submit" disabled={name.trim() === '' || (needsParent && parentId === '')}>
          Create {sectionLabel(section).slice(0, -1).toLowerCase()}
        </AwsButton>
        {section === 'subnets' && <span className="text-sm text-aws-muted">CIDR must sit inside the parent VPC and not overlap a sibling subnet — the backend validates it (400 otherwise).</span>}
        {section === 'nat' && <span className="text-sm text-aws-muted">NAT creation goes through the engine; in lite mode it is SIMULATED with a documentation-range address.</span>}
        {section === 'gateways' && gatewayType === 'nat' && <span className="text-sm text-aws-muted">NAT gateways are engine-backed; an engine failure is a 502 and nothing is created.</span>}
      </form>
      <p className="mt-2 text-sm text-aws-muted">Gateways in this lab: {gateways.length === 0 ? 'none' : gateways.map((g) => g.name).join(', ')}</p>
    </AwsCard>
  )
}

/**
 * VPC detail tabs — OBSERVED in the v4 survey for a real VPC
 * (Resource map, CIDRs, Flow logs, Tags, Related resources,
 * Integrations). Resource map / Flow logs / Integrations are honest
 * info panels (CloudForge has no per-VPC topology graph, no lab
 * flow-log API and no VPC integrations); CIDRs and Related resources
 * render real lab data; Tags reuses the real tags editor. Other
 * resource kinds keep the single detail panel below.
 */
function ResourceDetail({ resource, labId, all, onChanged, onDelete, onNotice }: {
  resource: LabResource
  labId: string
  all: LabResource[]
  onChanged: () => void
  onDelete: () => void
  onNotice: (s: string) => void
}) {
  const [vpcTab, setVpcTab] = useState('resource-map')
  if (resource.kind === 'vpc') {
    const subnets = all.filter((r) => r.kind === 'subnet' && r.parent_id === resource.id)
    const routeTables = all.filter((r) => r.kind === 'route-table' && r.parent_id === resource.id)
    const gateways = all.filter((r) => r.kind === 'gateway')
    return (
      <div className="mt-3 border-t border-aws-border pt-3">
        <dl className="mb-3 space-y-1 text-sm">
          <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">VPC ID</dt><dd className="break-all">{resource.id}</dd></div>
          <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Name</dt><dd>{resource.name}</dd></div>
          <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">State</dt><dd>{resource.state ?? '—'}</dd></div>
        </dl>
        <ConsoleTabs
          tabs={[
            { id: 'resource-map', label: 'Resource map' },
            { id: 'cidrs', label: 'CIDRs' },
            { id: 'flow-logs', label: 'Flow logs' },
            { id: 'tags', label: 'Tags' },
            { id: 'related', label: 'Related resources' },
            { id: 'integrations', label: 'Integrations' },
          ]}
          active={vpcTab}
          onChange={setVpcTab}
        />
        <div className="pt-3 text-sm">
          {vpcTab === 'resource-map' && (
            <p className="text-aws-muted">
              CloudForge has no per-VPC topology graph. The lab topology page shows this VPC&apos;s subnets,
              route tables and gateways from the same lab store:{' '}
              <Link to={`/labs/${labId}/topology`} className="text-aws-link hover:underline">
                View lab topology (real VPC/subnet/route state)
              </Link>
              .
            </p>
          )}
          {vpcTab === 'cidrs' && (
            <dl className="space-y-1">
              <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">IPv4 CIDR</dt><dd className="font-mono text-xs">{resource.cidr ?? '—'}</dd></div>
              <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">IPv6 CIDR</dt><dd>— (IPv6 is not modelled in CloudForge labs)</dd></div>
              <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium">CIDR block count</dt><dd>1 IPv4 CIDR; no additional CIDR associations — the lab API accepts one CIDR per VPC.</dd></div>
            </dl>
          )}
          {vpcTab === 'flow-logs' && (
            <p className="text-aws-muted">
              Flow logs are UNSUPPORTED for lab VPCs — CloudForge has no flow-log API for lab resources, so no
              flow log is recorded for this VPC. Flow log groups exist as a separate exam console:{' '}
              <Link to="/services/flowlogs" className="text-aws-link hover:underline">Flow Logs</Link>.
            </p>
          )}
          {vpcTab === 'tags' && (
            <TagsEditor resource={resource} labId={labId} onChanged={onChanged} onNotice={onNotice} />
          )}
          {vpcTab === 'related' && (
            <div className="space-y-2">
              <p>
                Subnets in this VPC: {subnets.length === 0 ? 'none' : subnets.map((r) => `${r.name} (${r.cidr ?? 'no CIDR'})`).join(', ')}
              </p>
              <p>
                Route tables in this VPC: {routeTables.length === 0 ? 'none' : routeTables.map((r) => r.name).join(', ')}
              </p>
              <p>
                Gateways in this lab: {gateways.length === 0 ? 'none' : gateways.map((r) => `${r.name} (${r.state ?? '—'})`).join(', ')}
              </p>
              <p className="text-xs text-aws-muted">
                Security groups and network ACLs are lab-wide records; they are not attached to a single VPC in
                CloudForge, so they are not listed here.
              </p>
            </div>
          )}
          {vpcTab === 'integrations' && (
            <p className="text-aws-muted">
              No integrations — CloudForge has no IPAM, Firewall Manager, flow-log delivery or other VPC
              integration for lab VPCs. The VPC encryption control choice in the Create VPC wizard is recorded
              as a tag only.
            </p>
          )}
        </div>
        <div className="mt-3">
          <AwsButton onClick={onDelete}>Delete resource</AwsButton>
        </div>
      </div>
    )
  }
  return (
    <div className="mt-3 border-t border-aws-border pt-3">
      <dl className="space-y-1 text-sm">
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">ID</dt><dd className="break-all">{resource.id}</dd></div>
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Name</dt><dd>{resource.name}</dd></div>
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Kind</dt><dd>{resource.kind}</dd></div>
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">State</dt><dd>{resource.state ?? '—'}</dd></div>
        {resource.cidr && <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">CIDR</dt><dd className="font-mono text-xs">{resource.cidr}</dd></div>}
        {resource.parent_id && <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Parent</dt><dd>{resource.parent_id}</dd></div>}
      </dl>
      {resource.kind === 'route-table' && (
        <RouteTableDetail resource={resource} labId={labId} all={all} onChanged={onChanged} onNotice={onNotice} />
      )}
      {resource.kind === 'gateway' && (
        <GatewayDetail resource={resource} labId={labId} all={all} onChanged={onChanged} onNotice={onNotice} />
      )}
      {resource.kind === 'security-group' && (
        <SecurityGroupDetail resource={resource} labId={labId} all={all} onChanged={onChanged} onNotice={onNotice} />
      )}
      <TagsEditor resource={resource} labId={labId} onChanged={onChanged} onNotice={onNotice} />
      <div className="mt-3">
        <AwsButton onClick={onDelete}>Delete resource</AwsButton>
      </div>
    </div>
  )
}

function RouteTableDetail({ resource, labId, all, onChanged, onNotice }: {
  resource: LabResource
  labId: string
  all: LabResource[]
  onChanged: () => void
  onNotice: (s: string) => void
}) {
  const [destination, setDestination] = useState('0.0.0.0/0')
  const [target, setTarget] = useState('local')
  const routes = cfgList(resource.config, 'routes')
  const gateways = all.filter((r) => r.kind === 'gateway')
  const add = useMutation({
    mutationFn: () => addVpcRoute(labId, resource.id, destination.trim(), target),
    onSuccess: () => {
      onNotice('Route added (engine applied it).')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Add route failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (routeId: string) => deleteVpcRoute(labId, resource.id, routeId),
    onSuccess: () => {
      onNotice('Route deleted.')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Delete route failed: ${e.message}`),
  })
  return (
    <div className="mt-3">
      <h3 className="text-sm font-semibold">Routes (engine-owned — a route the engine rejects is never persisted)</h3>
      {routes.length === 0 ? (
        <p className="text-sm text-aws-muted">No routes yet.</p>
      ) : (
        <AwsTable<Record<string, unknown> & { id: string }>
          caption="Routes"
          emptyMessage="No routes."
          rows={routes.map((r) => ({ ...r, id: typeof r.id === 'string' ? r.id : '' }))}
          totalCount={routes.length}
          columns={[
            { header: 'Destination', render: (r) => <span className="font-mono text-xs">{String(r.destination ?? '')}</span> },
            { header: 'Target', render: (r) => String(r.target ?? '') },
            { header: 'Actions', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(r.id)}>Delete</button> },
          ]}
        />
      )}
      <form
        className="mt-2 flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          add.mutate()
        }}
      >
        <label className="text-sm">
          Destination
          <input value={destination} onChange={(e) => setDestination(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm" />
        </label>
        <label className="text-sm">
          Target
          <select value={target} onChange={(e) => setTarget(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            <option value="local">local</option>
            {gateways.map((g) => (<option key={g.id} value={g.id}>{g.name} ({g.id})</option>))}
          </select>
        </label>
        <AwsButton type="submit">Add route</AwsButton>
      </form>
    </div>
  )
}

function GatewayDetail({ resource, labId, all, onChanged, onNotice }: {
  resource: LabResource
  labId: string
  all: LabResource[]
  onChanged: () => void
  onNotice: (s: string) => void
}) {
  const [vpcId, setVpcId] = useState('')
  const vpcs = all.filter((r) => r.kind === 'vpc')
  const type = cfgStr(resource.config, 'gateway_type') || 'igw'
  const attached = resource.state === 'attached'
  const attach = useMutation({
    mutationFn: () => attachGateway(labId, resource.id, vpcId),
    onSuccess: () => {
      onNotice('Gateway attached to the VPC (recorded; SIMULATED — the engine has no IGW RPC).')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Attach failed: ${e.message}`),
  })
  const detach = useMutation({
    mutationFn: () => detachGateway(labId, resource.id),
    onSuccess: () => {
      onNotice('Gateway detached.')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Detach failed: ${e.message}`),
  })
  return (
    <div className="mt-3">
      <h3 className="text-sm font-semibold">Gateway</h3>
      <p className="text-sm text-aws-muted">
        Type: <strong>{type === 'nat' ? 'NAT gateway' : 'Internet gateway'}</strong> · State: <strong>{resource.state ?? '—'}</strong>
        {type === 'nat' && cfgStr(resource.config, 'translated_address') !== '' && (
          <> · Translated address: <span className="font-mono text-xs">{cfgStr(resource.config, 'translated_address')}</span> (documentation range, not a real public IP)</>
        )}
      </p>
      {type === 'igw' && !attached && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <label className="text-sm">
            Attach to VPC
            <select value={vpcId} onChange={(e) => setVpcId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="">Select a VPC…</option>
              {vpcs.map((v) => (<option key={v.id} value={v.id}>{v.name}</option>))}
            </select>
          </label>
          <AwsButton disabled={vpcId === ''} onClick={() => attach.mutate()}>Attach</AwsButton>
        </div>
      )}
      {type === 'igw' && attached && (
        <div className="mt-2">
          <AwsButton onClick={() => detach.mutate()}>Detach</AwsButton>
        </div>
      )}
      {type === 'nat' && (
        <p className="mt-2 text-sm text-aws-muted">NAT gateways are engine-managed; delete the resource to remove it (the engine DeleteNAT runs on delete). No Elastic IP association: CloudForge has no Elastic IP API; NAT gateways are engine-managed with a documentation-range address.</p>
      )}
    </div>
  )
}

function SecurityGroupDetail({ resource, labId, all, onChanged, onNotice }: {
  resource: LabResource
  labId: string
  all: LabResource[]
  onChanged: () => void
  onNotice: (s: string) => void
}) {
  const [direction, setDirection] = useState<'ingress' | 'egress'>('ingress')
  const [protocol, setProtocol] = useState('tcp')
  const [fromPort, setFromPort] = useState(22)
  const [toPort, setToPort] = useState(22)
  const [cidr, setCidr] = useState('0.0.0.0/0')
  const [instanceId, setInstanceId] = useState('')
  const rules = cfgList(resource.config, 'rules')
  const applied = resource.config?.engine_policy_applied !== false
  const note = typeof resource.config?.engine_note === 'string' ? (resource.config.engine_note as string) : ''
  const instances = all.filter((r) => r.kind === 'instance')
  const add = useMutation({
    mutationFn: () => addSecurityGroupRule(labId, resource.id, { direction, protocol, from_port: fromPort, to_port: toPort, cidr: cidr.trim() }),
    onSuccess: () => {
      onNotice('Rule added (stored in the DB; see the engine-sync note below).')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Add rule failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (ruleId: string) => deleteSecurityGroupRule(labId, resource.id, ruleId),
    onSuccess: () => {
      onNotice('Rule deleted.')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Delete rule failed: ${e.message}`),
  })
  const attach = useMutation({
    mutationFn: () => attachSecurityGroupToInstance(labId, resource.id, instanceId),
    onSuccess: () => {
      onNotice('Security group attached to the instance.')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Attach failed: ${e.message}`),
  })
  return (
    <div className="mt-3">
      <h3 className="text-sm font-semibold">Rules (DB-authoritative)</h3>
      <p className="text-sm text-aws-muted">
        Engine firewall snapshot: {applied ? 'applied' : 'NOT applied — the engine has no update/delete RPC for firewall policies, so later edits are stored but not re-applied'}{note !== '' ? ` — ${note}` : ''}
      </p>
      {rules.length === 0 ? (
        <p className="text-sm text-aws-muted">No rules yet.</p>
      ) : (
        <AwsTable<Record<string, unknown> & { id: string }>
          caption="Security group rules"
          emptyMessage="No rules."
          rows={rules.map((r) => ({ ...r, id: typeof r.id === 'string' ? r.id : '' }))}
          totalCount={rules.length}
          columns={[
            { header: 'Direction', render: (r) => String(r.direction ?? '') },
            { header: 'Protocol', render: (r) => String(r.protocol ?? '') },
            { header: 'Ports', render: (r) => `${String(r.from_port ?? '')}–${String(r.to_port ?? '')}` },
            { header: 'Source', render: (r) => <span className="font-mono text-xs">{String(r.cidr ?? r.source ?? '')}</span> },
            { header: 'Actions', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(r.id)}>Delete</button> },
          ]}
        />
      )}
      <form
        className="mt-2 flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          add.mutate()
        }}
      >
        <label className="text-sm">
          Direction
          <select value={direction} onChange={(e) => setDirection(e.target.value as 'ingress' | 'egress')} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            <option value="ingress">Ingress</option>
            <option value="egress">Egress</option>
          </select>
        </label>
        <label className="text-sm">
          Protocol
          <select value={protocol} onChange={(e) => setProtocol(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
            <option value="icmp">ICMP</option>
            <option value="any">Any</option>
          </select>
        </label>
        <label className="text-sm">
          From port
          <input type="number" min={0} max={65535} value={fromPort} onChange={(e) => setFromPort(Number(e.target.value))} className="ml-2 w-20 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        </label>
        <label className="text-sm">
          To port
          <input type="number" min={0} max={65535} value={toPort} onChange={(e) => setToPort(Number(e.target.value))} className="ml-2 w-20 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        </label>
        <label className="text-sm">
          CIDR
          <input value={cidr} onChange={(e) => setCidr(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm" />
        </label>
        <AwsButton type="submit">Add rule</AwsButton>
      </form>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <label className="text-sm">
          Attach to instance
          <select value={instanceId} onChange={(e) => setInstanceId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            <option value="">Select an instance…</option>
            {instances.map((i) => (<option key={i.id} value={i.id}>{i.name} ({i.id})</option>))}
          </select>
        </label>
        <AwsButton disabled={instanceId === ''} onClick={() => attach.mutate()}>Attach to instance</AwsButton>
      </div>
    </div>
  )
}

function TagsEditor({ resource, labId, onChanged, onNotice }: {
  resource: LabResource
  labId: string
  onChanged: () => void
  onNotice: (s: string) => void
}) {
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const tags = resource.labels ?? {}
  const save = useMutation({
    mutationFn: (next: Record<string, string>) => setResourceTags(labId, resource.id, next),
    onSuccess: () => {
      setKey('')
      setValue('')
      onChanged()
    },
    onError: (e: Error) => onNotice(`Save tags failed: ${e.message}`),
  })
  const remove = (k: string) => {
    const next = { ...tags }
    delete next[k]
    save.mutate(next)
  }
  return (
    <div className="mt-3">
      <h3 className="text-sm font-semibold">Tags</h3>
      {Object.keys(tags).length === 0 ? (
        <p className="text-sm text-aws-muted">No tags yet.</p>
      ) : (
        <ul className="space-y-1 text-sm">
          {Object.entries(tags).map(([k, v]) => (
            <li key={k} className="flex items-center gap-2">
              <span><strong>{k}</strong> = {v}</span>
              <button type="button" className="text-aws-link hover:underline" onClick={() => remove(k)}>Remove</button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="mt-2 flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (key.trim() !== '') save.mutate({ ...tags, [key.trim()]: value })
        }}
      >
        <input value={key} onChange={(e) => setKey(e.target.value)} placeholder="Key" aria-label="Tag key" className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="Value" aria-label="Tag value" className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        <AwsButton type="submit" disabled={key.trim() === ''}>Add / update tag</AwsButton>
        <span className="text-sm text-aws-muted">Keys starting with aws: are reserved and rejected.</span>
      </form>
    </div>
  )
}

/**
 * Site-to-Site VPN connections — wired to the real API-EMULATED backend
 * (POST/GET/DELETE /api/v1/aws/vpn/connections, POST .../activate|deactivate).
 * The table keeps the survey's exact 19 VPN Connections columns; columns
 * the backend has no value for render '—' instead of an invented value.
 * Tunnel state is a teaching field: no IPsec tunnel exists and no
 * traffic flows. Connections validate their gateways — the backend
 * returns 400 when a gateway id does not exist, and the tunnel starts
 * DOWN. Concentrators and Client VPN endpoints have no backend and stay
 * disabled nav labels, not built features.
 */
function VpnConnectionsSection({ onNotice }: { onNotice: (s: string) => void }) {
  const connections = useVpnConnections()
  const customerGateways = useCustomerGateways()
  const vpnGateways = useVpnGateways()
  const create = useExamCreate('vpn/connections')
  const act = useExamAction('vpn/connections')
  const remove = useExamDelete('vpn/connections')
  const [filter, setFilter] = useState('')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [cgwId, setCgwId] = useState('')
  const [vgwId, setVgwId] = useState('')
  const selected = connections.rows.find((r) => r.id === checkedIds[0]) ?? null
  const rows = connections.rows.filter(
    (r) =>
      filter.trim() === '' ||
      str(r, 'name').toLowerCase().includes(filter.toLowerCase()) ||
      r.id.toLowerCase().includes(filter.toLowerCase()),
  )
  const cgwAddress = (id: string): string => {
    const gw = customerGateways.rows.find((g) => g.id === id)
    return gw === undefined ? '—' : str(gw, 'ip_address')
  }
  const inputCls =
    'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'
  const tunnelAction = (action: 'activate' | 'deactivate') => {
    if (selected === null) return
    act.mutate(
      { id: selected.id, action },
      {
        onSuccess: () =>
          onNotice(`VPN connection ${selected.id} tunnel ${action === 'activate' ? 'UP' : 'DOWN'} (teaching field — no real tunnel).`),
        onError: (e: Error) => onNotice(`${action === 'activate' ? 'Activate' : 'Deactivate'} failed: ${e.message}`),
      },
    )
  }
  return (
    <div className="space-y-4">
      <AwsFlash tone="info">
        VPN is the VPC console&apos;s Virtual private network section — it has no standalone console (2026-11-22
        survey). The old /services/vpn bookmark redirects here.
      </AwsFlash>
      <AwsCard title={`VPN connections (${connections.rows.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          VPN connections are <strong>API-EMULATED</strong> in CloudForge — durable records with tunnel
          activate/deactivate. The tunnel state is a teaching field: no IPsec tunnel exists and no traffic flows.
          Transit Gateway attachments are the available hybrid-networking practice for real route-table entries.
        </p>
        {connections.isError && (
          <ErrorBox message={`VPN connections could not be loaded (${connections.errorMessage}).`} />
        )}
        <AwsToolbar
          right={
            <>
              <AwsButton disabled title="No configuration-download API in CloudForge — tunnel parameters are teaching fields.">
                Download configuration
              </AwsButton>
              <AwsRefreshButton onClick={connections.refresh} busy={connections.isFetching} />
              <AwsPreferencesButton />
            </>
          }
        >
          <AwsActionsMenu
            items={[
              {
                label: 'Activate tunnel',
                disabledReason:
                  selected === null
                    ? 'Select a connection first.'
                    : str(selected, 'tunnel_status') === 'UP'
                      ? 'Tunnel is already UP.'
                      : undefined,
                onClick: () => tunnelAction('activate'),
              },
              {
                label: 'Deactivate tunnel',
                disabledReason:
                  selected === null
                    ? 'Select a connection first.'
                    : str(selected, 'tunnel_status') === 'DOWN'
                      ? 'Tunnel is already DOWN.'
                      : undefined,
                onClick: () => tunnelAction('deactivate'),
              },
              {
                label: 'Delete',
                disabledReason: selected === null ? 'Select a connection first.' : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  remove.mutate(sel.id, {
                    onSuccess: () => {
                      onNotice(`VPN connection ${sel.id} deleted.`)
                      setCheckedIds([])
                    },
                    onError: (e: Error) => onNotice(`Delete failed: ${e.message}`),
                  })
                },
              },
            ]}
          />
          <AwsButton variant="primary" onClick={() => setShowCreate((v) => !v)}>
            Create VPN connection
          </AwsButton>
        </AwsToolbar>
        {showCreate && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Create VPN connection</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. office-vpn" className={`w-full max-w-md ${inputCls}`} />
            </label>
            <div className="flex flex-wrap gap-3">
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Customer gateway</span>
                <select value={cgwId} onChange={(e) => setCgwId(e.target.value)} className={inputCls}>
                  <option value="">Select a customer gateway…</option>
                  {customerGateways.rows.map((g) => (
                    <option key={g.id} value={g.id}>{str(g, 'name')} ({g.id}) — {str(g, 'ip_address')}</option>
                  ))}
                </select>
              </label>
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Virtual private gateway</span>
                <select value={vgwId} onChange={(e) => setVgwId(e.target.value)} className={inputCls}>
                  <option value="">Select a virtual private gateway…</option>
                  {vpnGateways.rows.map((g) => (
                    <option key={g.id} value={g.id}>{str(g, 'name')} ({g.id})</option>
                  ))}
                </select>
              </label>
            </div>
            <p className="text-xs text-aws-muted">
              Create the gateways first on the Customer Gateways and Virtual Private Gateways pages. The backend
              returns 400 when a gateway id does not exist, and the tunnel starts DOWN.
            </p>
            <div>
              <AwsButton
                variant="primary"
                disabled={name.trim() === '' || cgwId === '' || vgwId === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { name: name.trim(), customer_gateway_id: cgwId, vpn_gateway_id: vgwId },
                    {
                      onSuccess: (c) => {
                        onNotice(`VPN connection ${c.id} created — tunnel DOWN.`)
                        setName('')
                        setShowCreate(false)
                      },
                      onError: (e: Error) => onNotice(`Create failed: ${e.message}`),
                    },
                  )
                }
              >
                Create VPN connection
              </AwsButton>
            </div>
          </div>
        )}
        <AwsTable<Rec>
          caption="VPN connections"
          emptyMessage="No VPN connections yet — create the gateways first, then a connection."
          rows={rows}
          totalCount={connections.rows.length}
          filter={filter}
          onFilterChange={setFilter}
          filterPlaceholder="Filter connections by name or ID"
          selectable
          selectedIds={checkedIds}
          onToggleRow={(id) => setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))}
          onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'VPN ID', render: (r) => <span className="font-mono">{r.id}</span> },
            { header: 'State', render: (r) => <StateBadge state={str(r, 'status')} /> },
            { header: 'Virtual private gateway', render: (r) => <span className="font-mono">{str(r, 'vpn_gateway_id')}</span> },
            { header: 'Transit gateway', render: () => '—' },
            { header: 'VPN Concentrator ID', render: () => '—' },
            { header: 'Customer gateway', render: (r) => <span className="font-mono">{str(r, 'customer_gateway_id')}</span> },
            { header: 'Customer gateway address', render: (r) => cgwAddress(str(r, 'customer_gateway_id')) },
            { header: 'Inside IP version', render: () => '—' },
            { header: 'Type', render: (r) => str(r, 'type') },
            { header: 'Category', render: () => '—' },
            { header: 'VPC', render: () => '—' },
            { header: 'Routing', render: () => '—' },
            { header: 'Acceleration enabled', render: () => '—' },
            { header: 'Authentication', render: () => '—' },
            { header: 'Local IPv4 network CIDR', render: () => '—' },
            { header: 'Remote IPv4 network CIDR', render: () => '—' },
            { header: 'Local IPv6 network CIDR', render: () => '—' },
            { header: 'Remote IPv6 network CIDR', render: () => '—' },
            { header: 'Core network ARN', render: () => '—' },
            { header: 'Core network attachment ARN', render: () => '—' },
            { header: 'Gateway association state', render: () => '—' },
            { header: 'Outside IP address type', render: () => '—' },
          ]}
        />
        {selected !== null && (
          <dl className="mt-3 space-y-1 text-sm">
            <dt className="font-medium">Tunnel status</dt>
            <dd><StateBadge state={str(selected, 'tunnel_status')} /> — teaching field, no real IPsec tunnel.</dd>
            <dt className="font-medium">Type</dt>
            <dd>{str(selected, 'type')}</dd>
            <dt className="font-medium">Created</dt>
            <dd>{fmtTime(selected.created_at)}</dd>
          </dl>
        )}
      </AwsCard>
    </div>
  )
}

/**
 * Customer gateways — wired to the real API-EMULATED backend
 * (POST/GET/DELETE /api/v1/aws/vpn/customer-gateways). The survey
 * records no Customer Gateways list columns, so the table uses the
 * record's own fields rather than invented AWS columns.
 */
function CustomerGatewaysSection({ onNotice }: { onNotice: (s: string) => void }) {
  const gateways = useCustomerGateways()
  const create = useExamCreate('vpn/customer-gateways')
  const remove = useExamDelete('vpn/customer-gateways')
  const [filter, setFilter] = useState('')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [ipAddress, setIpAddress] = useState('')
  const [bgpAsn, setBgpAsn] = useState(65000)
  const selected = gateways.rows.find((r) => r.id === checkedIds[0]) ?? null
  const rows = gateways.rows.filter(
    (r) =>
      filter.trim() === '' ||
      str(r, 'name').toLowerCase().includes(filter.toLowerCase()) ||
      r.id.toLowerCase().includes(filter.toLowerCase()),
  )
  const inputCls =
    'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'
  return (
    <div className="space-y-4">
      <AwsFlash tone="info">
        Customer gateways are the on-premises side of a Site-to-Site VPN connection — the customer gateway address
        is the public IP of your side of the tunnel. Connections on the Site-to-Site VPN Connections page
        reference a gateway from this list.
      </AwsFlash>
      <AwsCard title={`Customer gateways (${gateways.rows.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          Customer gateways are <strong>API-EMULATED</strong> in CloudForge — durable records. The IP address must
          be a valid IP (the backend returns 400 otherwise) and the BGP ASN defaults to 65000.
        </p>
        {gateways.isError && (
          <ErrorBox message={`Customer gateways could not be loaded (${gateways.errorMessage}).`} />
        )}
        <AwsToolbar
          right={
            <>
              <AwsRefreshButton onClick={gateways.refresh} busy={gateways.isFetching} />
              <AwsPreferencesButton />
            </>
          }
        >
          <AwsActionsMenu
            items={[
              {
                label: 'Delete',
                disabledReason: selected === null ? 'Select a gateway first.' : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  remove.mutate(sel.id, {
                    onSuccess: () => {
                      onNotice(`Customer gateway ${sel.id} deleted.`)
                      setCheckedIds([])
                    },
                    onError: (e: Error) => onNotice(`Delete failed: ${e.message}`),
                  })
                },
              },
            ]}
          />
          <AwsButton variant="primary" onClick={() => setShowCreate((v) => !v)}>
            Create customer gateway
          </AwsButton>
        </AwsToolbar>
        {showCreate && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Create customer gateway</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. office-gw" className={`w-full max-w-md ${inputCls}`} />
            </label>
            <div className="flex flex-wrap gap-3">
              <label className="block text-sm">
                <span className="mb-1 block font-medium">IP address</span>
                <input value={ipAddress} onChange={(e) => setIpAddress(e.target.value)} placeholder="e.g. 203.0.113.10" className={`w-full max-w-md font-mono ${inputCls}`} />
              </label>
              <label className="block text-sm">
                <span className="mb-1 block font-medium">BGP ASN</span>
                <input type="number" min={1} value={bgpAsn} onChange={(e) => setBgpAsn(Number(e.target.value))} className={`w-32 ${inputCls}`} />
              </label>
            </div>
            <div>
              <AwsButton
                variant="primary"
                disabled={name.trim() === '' || ipAddress.trim() === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { name: name.trim(), ip_address: ipAddress.trim(), bgp_asn: bgpAsn },
                    {
                      onSuccess: (g) => {
                        onNotice(`Customer gateway ${g.id} created.`)
                        setName('')
                        setIpAddress('')
                        setShowCreate(false)
                      },
                      onError: (e: Error) => onNotice(`Create failed: ${e.message}`),
                    },
                  )
                }
              >
                Create customer gateway
              </AwsButton>
            </div>
          </div>
        )}
        <AwsTable<Rec>
          caption="Customer gateways"
          emptyMessage="No customer gateways yet — create one to start a Site-to-Site VPN connection."
          rows={rows}
          totalCount={gateways.rows.length}
          filter={filter}
          onFilterChange={setFilter}
          filterPlaceholder="Filter gateways by name or ID"
          selectable
          selectedIds={checkedIds}
          onToggleRow={(id) => setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))}
          onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Customer gateway ID', render: (r) => <span className="font-mono">{r.id}</span> },
            { header: 'IP address', render: (r) => <span className="font-mono">{str(r, 'ip_address')}</span> },
            { header: 'BGP ASN', render: (r) => String(r.bgp_asn ?? '—') },
            { header: 'State', render: (r) => <StateBadge state={str(r, 'status')} /> },
            { header: 'Created', render: (r) => fmtTime(r.created_at) },
          ]}
        />
      </AwsCard>
    </div>
  )
}

/**
 * Virtual private gateways — wired to the real API-EMULATED backend
 * (POST/GET/DELETE /api/v1/aws/vpn/vpn-gateways). The survey records
 * no Virtual Private Gateways list columns, so the table uses the
 * record's own fields rather than invented AWS columns.
 */
function VpnGatewaysSection({ onNotice }: { onNotice: (s: string) => void }) {
  const gateways = useVpnGateways()
  const create = useExamCreate('vpn/vpn-gateways')
  const remove = useExamDelete('vpn/vpn-gateways')
  const [filter, setFilter] = useState('')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const selected = gateways.rows.find((r) => r.id === checkedIds[0]) ?? null
  const rows = gateways.rows.filter(
    (r) =>
      filter.trim() === '' ||
      str(r, 'name').toLowerCase().includes(filter.toLowerCase()) ||
      r.id.toLowerCase().includes(filter.toLowerCase()),
  )
  const inputCls =
    'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'
  return (
    <div className="space-y-4">
      <AwsFlash tone="info">
        Virtual private gateways are the CloudForge side of a Site-to-Site VPN connection. Connections on the
        Site-to-Site VPN Connections page reference a gateway from this list.
      </AwsFlash>
      <AwsCard title={`Virtual private gateways (${gateways.rows.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          Virtual private gateways are <strong>API-EMULATED</strong> in CloudForge — durable records. A gateway
          has no attachment in CloudForge (no VPC attachment API); it exists so connections have a real gateway
          id to reference.
        </p>
        {gateways.isError && (
          <ErrorBox message={`Virtual private gateways could not be loaded (${gateways.errorMessage}).`} />
        )}
        <AwsToolbar
          right={
            <>
              <AwsRefreshButton onClick={gateways.refresh} busy={gateways.isFetching} />
              <AwsPreferencesButton />
            </>
          }
        >
          <AwsActionsMenu
            items={[
              {
                label: 'Delete',
                disabledReason: selected === null ? 'Select a gateway first.' : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  remove.mutate(sel.id, {
                    onSuccess: () => {
                      onNotice(`Virtual private gateway ${sel.id} deleted.`)
                      setCheckedIds([])
                    },
                    onError: (e: Error) => onNotice(`Delete failed: ${e.message}`),
                  })
                },
              },
            ]}
          />
          <AwsButton variant="primary" onClick={() => setShowCreate((v) => !v)}>
            Create virtual private gateway
          </AwsButton>
        </AwsToolbar>
        {showCreate && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Create virtual private gateway</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. cf-vgw" className={`w-full max-w-md ${inputCls}`} />
            </label>
            <div>
              <AwsButton
                variant="primary"
                disabled={name.trim() === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { name: name.trim() },
                    {
                      onSuccess: (g) => {
                        onNotice(`Virtual private gateway ${g.id} created.`)
                        setName('')
                        setShowCreate(false)
                      },
                      onError: (e: Error) => onNotice(`Create failed: ${e.message}`),
                    },
                  )
                }
              >
                Create virtual private gateway
              </AwsButton>
            </div>
          </div>
        )}
        <AwsTable<Rec>
          caption="Virtual private gateways"
          emptyMessage="No virtual private gateways yet — create one to start a Site-to-Site VPN connection."
          rows={rows}
          totalCount={gateways.rows.length}
          filter={filter}
          onFilterChange={setFilter}
          filterPlaceholder="Filter gateways by name or ID"
          selectable
          selectedIds={checkedIds}
          onToggleRow={(id) => setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))}
          onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'VPN gateway ID', render: (r) => <span className="font-mono">{r.id}</span> },
            { header: 'State', render: (r) => <StateBadge state={str(r, 'status')} /> },
            { header: 'Created', render: (r) => fmtTime(r.created_at) },
          ]}
        />
      </AwsCard>
    </div>
  )
}
