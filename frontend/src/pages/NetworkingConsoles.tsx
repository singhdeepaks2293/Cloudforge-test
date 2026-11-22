import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')
const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)
const inputCls = 'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'

function Notice({ text }: { text: string }) {
  return text !== '' ? (
    <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">
      {text}
    </p>
  ) : null
}

/** VPC resource picker: lists the caller's VPC lab resources from the explorer; falls back to a free-text resource ID. */
function VpcSelect({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  const vpcs = useQuery({
    queryKey: ['explorer-vpcs'],
    queryFn: () => request<Rec>('/api/v1/explorer/resources?type=vpc'),
    retry: 1,
  })
  const items = listOf(vpcs.data, 'items')
  const options = items.length > 0 ? items : listOf(vpcs.data, 'resources')
  return (
    <label className="text-sm">
      {label}
      {options.length > 0 ? (
        <select value={value} onChange={(e) => onChange(e.target.value)} className={`ml-2 ${inputCls}`} aria-label={label}>
          <option value="">(choose a VPC)</option>
          {options.map((v) => (
            <option key={v.id} value={v.id}>{str(v, 'name') || v.id}</option>
          ))}
        </select>
      ) : (
        <input value={value} onChange={(e) => onChange(e.target.value)} placeholder="VPC resource id" className={`ml-2 ${inputCls}`} aria-label={label} />
      )}
    </label>
  )
}

function useLabs() {
  const labs = useQuery({ queryKey: ['labs-list'], queryFn: () => request<Rec>('/api/v1/labs'), retry: 1 })
  return listOf(labs.data, 'labs')
}

/**
 * VPC Peering console (Phase 6) — mirrors the AWS VPC peering
 * connections screens. A peering writes real route-table entries
 * into both VPCs' lab state; there is no engine peering RPC, so
 * it is labelled SIMULATED and overlapping CIDRs are refused.
 */
export function VpcPeeringConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [vpcA, setVpcA] = useState('')
  const [vpcB, setVpcB] = useState('')
  const [notice, setNotice] = useState('')
  const peerings = useQuery({ queryKey: ['vpc-peerings'], queryFn: () => request<Rec>('/api/v1/aws/vpc/peerings'), retry: 1 })
  const rows = listOf(peerings.data, 'peerings')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['vpc-peerings'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/vpc/peerings', { method: 'POST', body: { name: name.trim(), vpc_a_resource_id: vpcA, vpc_b_resource_id: vpcB } }),
    onSuccess: () => { setNotice('Peering created — route entries written to both VPCs.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/vpc/peerings/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Peering deleted — routes removed.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'VPC Peering' }]}
        title="VPC Peering — Peering connections"
        description="Peer two lab VPCs. The peering is real route-table entries in both VPCs' lab state, not kernel peering."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="real route entries · no engine peering RPC" />}
      />
      <Notice text={notice} />
      {peerings.isPending && <Loading label="Loading peerings" />}
      {peerings.isError && <ErrorBox message={`Peerings could not be loaded (${peerings.error.message}).`} />}
      <AwsCard title="Peering connections">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '' && vpcA !== '' && vpcB !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Peering name" className={inputCls} />
          <VpcSelect value={vpcA} onChange={setVpcA} label="VPC A" />
          <VpcSelect value={vpcB} onChange={setVpcB} label="VPC B" />
          <AwsButton variant="primary" type="submit">Create peering connection</AwsButton>
        </form>
        <p className="mb-2 text-xs text-aws-muted">The two VPCs must be different and their CIDRs must not overlap — both are refused with 400.</p>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'VPC A', render: (r) => str(r, 'vpc_a_resource_id') },
            { header: 'VPC B', render: (r) => str(r, 'vpc_b_resource_id') },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>,
            },
          ]}
          rows={rows}
          emptyMessage="No peering connections yet — create two lab VPCs first."
        />
      </AwsCard>
    </div>
  )
}

/**
 * Transit Gateway console (Phase 6) — mirrors the AWS Transit
 * Gateway screens. Attachments are real route-table entries in the
 * attached VPCs' lab state.
 */
export function TransitGatewayConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [attachVpc, setAttachVpc] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const tgws = useQuery({ queryKey: ['tgws'], queryFn: () => request<Rec>('/api/v1/aws/transit-gateways'), retry: 1 })
  const rows = listOf(tgws.data, 'transit_gateways')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['tgws'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/transit-gateways', { method: 'POST', body: { name: name.trim() } }),
    onSuccess: () => { setNotice('Transit gateway created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const attach = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/transit-gateways/${selected}/attachments`, { method: 'POST', body: { vpc_resource_id: attachVpc } }),
    onSuccess: () => { setNotice('VPC attached — 0.0.0.0/0 route written to the VPC.'); refresh() },
    onError: (e: Error) => setNotice(`Attach failed: ${e.message}`),
  })
  const detach = useMutation({
    mutationFn: (attId: string) => request<Rec>(`/api/v1/aws/transit-gateways/${selected}/attachments/${attId}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Attachment removed — route removed.'); refresh() },
    onError: (e: Error) => setNotice(`Detach failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/transit-gateways/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Transit gateway deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = rows.find((t) => t.id === selected)
  const attachments = Array.isArray(sel?.attachments) ? (sel?.attachments as Record<string, unknown>[]) : []
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Transit Gateway' }]}
        title="Transit Gateway — Transit gateways"
        description="Central hub for lab VPCs. Attachments are real route-table entries in the attached VPCs' lab state."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="real route entries · no engine TGW RPC" />}
      />
      <Notice text={notice} />
      {tgws.isPending && <Loading label="Loading transit gateways" />}
      {tgws.isError && <ErrorBox message={`Transit gateways could not be loaded (${tgws.error.message}).`} />}
      <AwsCard title="Transit gateways">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Transit gateway name" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create transit gateway</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Attachments', render: (r) => String(num(r, 'attachment_count')) },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Manage attachments</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={rows}
          emptyMessage="No transit gateways yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Delete is refused (409) while attachments exist.</p>
      </AwsCard>
      {sel && (
        <AwsCard title={`Attachments: ${str(sel, 'name')}`}>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <VpcSelect value={attachVpc} onChange={setAttachVpc} label="VPC to attach" />
            <AwsButton variant="primary" onClick={() => attachVpc !== '' && attach.mutate()}>Attach VPC</AwsButton>
          </div>
          <AwsTable
            columns={[
              { header: 'Attachment', render: (r) => str(r as Rec, 'id') },
              { header: 'VPC', render: (r) => str(r as Rec, 'vpc_resource_id') },
              { header: 'Status', render: (r) => str(r as Rec, 'status') },
              {
                header: 'Actions',
                render: (r) => <AwsButton onClick={() => detach.mutate(str(r as Rec, 'id'))}>Detach</AwsButton>,
              },
            ]}
            rows={attachments.map((a, i) => ({ ...a, id: typeof a.id === 'string' ? a.id : `att-${i}` }))}
            emptyMessage="No attachments yet."
          />
        </AwsCard>
      )}
    </div>
  )
}

/**
 * VPC Endpoints console (Phase 6) — mirrors the AWS VPC endpoints
 * screens. Endpoint records route lab traffic to platform services;
 * the DNS name is a local teaching name, not registered.
 */
export function VpcEndpointsConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [vpc, setVpc] = useState('')
  const [service, setService] = useState('s3')
  const [notice, setNotice] = useState('')
  const endpoints = useQuery({ queryKey: ['vpc-endpoints'], queryFn: () => request<Rec>('/api/v1/aws/vpc/endpoints'), retry: 1 })
  const rows = listOf(endpoints.data, 'endpoints')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['vpc-endpoints'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/vpc/endpoints', { method: 'POST', body: { name: name.trim(), vpc_resource_id: vpc, service } }),
    onSuccess: () => { setNotice('Endpoint created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/vpc/endpoints/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Endpoint deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'VPC Endpoints' }]}
        title="VPC Endpoints — Endpoints"
        description="PrivateLink-style endpoints from a lab VPC to platform services."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="endpoint records · DNS name is a teaching name" />}
      />
      <Notice text={notice} />
      {endpoints.isPending && <Loading label="Loading endpoints" />}
      {endpoints.isError && <ErrorBox message={`Endpoints could not be loaded (${endpoints.error.message}).`} />}
      <AwsCard title="Endpoints">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '' && vpc !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Endpoint name" className={inputCls} />
          <VpcSelect value={vpc} onChange={setVpc} label="VPC" />
          <label className="text-sm">
            Service
            <select value={service} onChange={(e) => setService(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="s3">s3</option>
              <option value="dynamodb">dynamodb</option>
              <option value="sqs">sqs</option>
              <option value="sns">sns</option>
            </select>
          </label>
          <AwsButton variant="primary" type="submit">Create endpoint</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Service', render: (r) => str(r, 'service') },
            { header: 'VPC', render: (r) => str(r, 'vpc_resource_id') },
            { header: 'DNS name', render: (r) => <span className="font-mono text-xs">{str(r, 'dns_name')}</span> },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>,
            },
          ]}
          rows={rows}
          emptyMessage="No endpoints yet."
        />
      </AwsCard>
    </div>
  )
}

/**
 * Network Firewall console (Phase 6) — mirrors the AWS Network
 * Firewall screens. Rules are really evaluated (5-tuple, priority
 * order, default deny) and the policy is sent to the engine's
 * firewall RPC when a lab is attached — sync failure is recorded,
 * never claimed.
 */
export function NetworkFirewallConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [labId, setLabId] = useState('')
  const [ruleName, setRuleName] = useState('allow-https')
  const [priority, setPriority] = useState(100)
  const [action, setAction] = useState('allow')
  const [protocol, setProtocol] = useState('tcp')
  const [srcCidr, setSrcCidr] = useState('0.0.0.0/0')
  const [dstCidr, setDstCidr] = useState('0.0.0.0/0')
  const [portRange, setPortRange] = useState('443')
  const [selected, setSelected] = useState('')
  const [srcIp, setSrcIp] = useState('203.0.113.10')
  const [dstIp, setDstIp] = useState('10.0.0.5')
  const [dstPort, setDstPort] = useState(443)
  const [evalResult, setEvalResult] = useState('')
  const [notice, setNotice] = useState('')
  const labs = useLabs()
  const firewalls = useQuery({ queryKey: ['netfirewalls'], queryFn: () => request<Rec>('/api/v1/aws/network-firewall/firewalls'), retry: 1 })
  const rows = listOf(firewalls.data, 'firewalls')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['netfirewalls'] }) }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/network-firewall/firewalls', {
        method: 'POST',
        body: {
          name: name.trim(),
          lab_id: labId || undefined,
          rules: [{ name: ruleName.trim(), priority, action, protocol, source_cidr: srcCidr, dest_cidr: dstCidr, port_range: portRange }],
        },
      }),
    onSuccess: (d) => { setNotice(`Firewall created. Engine policy applied: ${String(d.engine_policy_applied === true ? 'yes' : 'no')}${d.engine_policy_applied === true ? '' : ` (${str(d, 'engine_sync_error') || 'no lab attached / engine unavailable'})`}.`); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const evaluate = useMutation({
    mutationFn: () =>
      request<Rec>(`/api/v1/aws/network-firewall/firewalls/${selected}/evaluate`, {
        method: 'POST',
        body: { source_ip: srcIp, dest_ip: dstIp, dest_port: dstPort, protocol, direction: 'ingress' },
      }),
    onSuccess: (d) => { setEvalResult(`Action: ${str(d, 'action')} — matched rule: ${str(d, 'matched_rule') || '(default deny)'}`); },
    onError: (e: Error) => setNotice(`Evaluate failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/network-firewall/firewalls/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Firewall deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = rows.find((f) => f.id === selected)
  const rules = Array.isArray(sel?.rules) ? (sel?.rules as Record<string, unknown>[]) : []
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Network Firewall' }]}
        title="Network Firewall — Firewalls"
        description="Stateful firewall policies with real 5-tuple rule evaluation."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="rules really evaluated · engine sync best-effort" />}
      />
      <Notice text={notice} />
      {firewalls.isPending && <Loading label="Loading firewalls" />}
      {firewalls.isError && <ErrorBox message={`Firewalls could not be loaded (${firewalls.error.message}).`} />}
      <AwsCard title="Firewalls">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Firewall name" className={inputCls} />
          <label className="text-sm">
            Lab (optional — engine sync)
            <select value={labId} onChange={(e) => setLabId(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="">(none)</option>
              {labs.map((l) => (
                <option key={l.id} value={l.id}>{str(l, 'name') || l.id}</option>
              ))}
            </select>
          </label>
          <input value={ruleName} onChange={(e) => setRuleName(e.target.value)} placeholder="Rule name" className={inputCls} />
          <input type="number" value={priority} onChange={(e) => setPriority(Number(e.target.value))} className={`w-20 ${inputCls}`} aria-label="Priority" />
          <select value={action} onChange={(e) => setAction(e.target.value)} className={inputCls} aria-label="Action">
            <option value="allow">allow</option>
            <option value="deny">deny</option>
          </select>
          <select value={protocol} onChange={(e) => setProtocol(e.target.value)} className={inputCls} aria-label="Protocol">
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
            <option value="icmp">icmp</option>
            <option value="any">any</option>
          </select>
          <input value={srcCidr} onChange={(e) => setSrcCidr(e.target.value)} placeholder="Source CIDR" className={inputCls} />
          <input value={dstCidr} onChange={(e) => setDstCidr(e.target.value)} placeholder="Dest CIDR" className={inputCls} />
          <input value={portRange} onChange={(e) => setPortRange(e.target.value)} placeholder="Port range (e.g. 443 or 8000-9000)" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create firewall</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Lab', render: (r) => str(r, 'lab_id') },
            { header: 'Rules', render: (r) => String(Array.isArray(r.rules) ? (r.rules as unknown[]).length : 0) },
            { header: 'Engine policy applied', render: (r) => String(r.engine_policy_applied === true ? 'yes' : 'no') },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Evaluate</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={rows}
          emptyMessage="No firewalls yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Evaluate packet: ${str(sel, 'name')}`}>
          <AwsTable
            columns={[
              { header: 'Rule', render: (r) => str(r as Rec, 'name') },
              { header: 'Priority', render: (r) => String(num(r as Rec, 'priority')) },
              { header: 'Action', render: (r) => str(r as Rec, 'action') },
              { header: 'Protocol', render: (r) => str(r as Rec, 'protocol') },
              { header: 'Source', render: (r) => str(r as Rec, 'source_cidr') },
              { header: 'Dest', render: (r) => str(r as Rec, 'dest_cidr') },
              { header: 'Ports', render: (r) => str(r as Rec, 'port_range') },
            ]}
            rows={rules.map((r, i) => ({ ...r, id: `rule-${i}` }))}
            emptyMessage="No rules."
          />
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <input value={srcIp} onChange={(e) => setSrcIp(e.target.value)} placeholder="Source IP" className={inputCls} aria-label="Source IP" />
            <input value={dstIp} onChange={(e) => setDstIp(e.target.value)} placeholder="Dest IP" className={inputCls} aria-label="Dest IP" />
            <input type="number" value={dstPort} onChange={(e) => setDstPort(Number(e.target.value))} className={`w-24 ${inputCls}`} aria-label="Dest port" />
            <AwsButton variant="primary" onClick={() => evaluate.mutate()}>Evaluate packet</AwsButton>
          </div>
          {evalResult !== '' && <p className="mt-2 text-sm" role="status">{evalResult}</p>}
          <p className="mt-2 text-xs text-aws-muted">Rules are evaluated in priority order; anything unmatched is denied.</p>
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Flow Logs console (Phase 6) — mirrors the AWS VPC flow logs
 * screens. Records are built ONLY from real engine telemetry
 * snapshots taken by /capture — no per-packet flows are invented.
 */
export function FlowLogsConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [vpc, setVpc] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const groups = useQuery({ queryKey: ['flowlog-groups'], queryFn: () => request<Rec>('/api/v1/aws/vpc/flow-logs'), retry: 1 })
  const rows = listOf(groups.data, 'groups')
  const records = useQuery({
    queryKey: ['flowlog-records', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/vpc/flow-logs/${selected}/records`),
    enabled: selected !== '',
    retry: 1,
  })
  const recordRows = listOf(records.data, 'records')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['flowlog-groups'] })
    void queryClient.invalidateQueries({ queryKey: ['flowlog-records', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/vpc/flow-logs', { method: 'POST', body: { name: name.trim(), vpc_resource_id: vpc } }),
    onSuccess: () => { setNotice('Flow log group created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const capture = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/vpc/flow-logs/${id}/capture`, { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Captured ${String(d.records_written ?? 0)} record(s) from engine telemetry.`); refresh() },
    onError: (e: Error) => setNotice(`Capture failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/vpc/flow-logs/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Flow log group deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = rows.find((g) => g.id === selected)
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Flow Logs' }]}
        title="VPC Flow Logs — Flow log groups"
        description="Flow-log records built only from real engine telemetry snapshots. Never invented flows."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="engine-telemetry snapshots only" />}
      />
      <Notice text={notice} />
      {groups.isPending && <Loading label="Loading flow log groups" />}
      {groups.isError && <ErrorBox message={`Flow log groups could not be loaded (${groups.error.message}).`} />}
      <AwsCard title="Flow log groups">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '' && vpc !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Group name" className={inputCls} />
          <VpcSelect value={vpc} onChange={setVpc} label="VPC" />
          <AwsButton variant="primary" type="submit">Create flow log group</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'VPC', render: (r) => str(r, 'vpc_resource_id') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Records', render: (r) => String(num(r, 'records_count')) },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>View records</AwsButton>
                  <AwsButton onClick={() => capture.mutate(str(r, 'id'))}>Capture now</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={rows}
          emptyMessage="No flow log groups yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Capture calls the engine's CollectTelemetry and stores one record per real telemetry point. If the engine is down, capture returns an honest 503 and writes zero records.</p>
      </AwsCard>
      {sel && (
        <AwsCard title={`Records: ${str(sel, 'name')} (${recordRows.length})`}>
          <AwsTable
            columns={[
              { header: 'Name', render: (r) => str(r, 'name') },
              { header: 'Value', render: (r) => String(num(r, 'value')) },
              { header: 'Labels', render: (r) => str(r, 'labels') },
              { header: 'Source', render: (r) => str(r, 'source') },
              { header: 'Timestamp', render: (r) => str(r, 'timestamp') },
            ]}
            rows={recordRows}
            emptyMessage="No records yet — capture from the engine."
          />
        </AwsCard>
      )}
    </div>
  )
}
