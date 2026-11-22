import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
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
  setResourceTags,
} from '../services/api.ts'
import { getStoredRegion } from '../services/catalog.ts'
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
 * NACLs are UNSUPPORTED — the engine has no NACL RPC — and say so.
 */
type Section = 'vpcs' | 'subnets' | 'routetables' | 'gateways' | 'nat' | 'securitygroups' | 'nacls'

const SECTION_KIND: Record<Exclude<Section, 'gateways' | 'nat' | 'nacls'>, string> = {
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

function cfgStr(config: Record<string, unknown> | undefined, key: string): string {
  if (!config) return ''
  const v = config[key]
  return typeof v === 'string' ? v : ''
}

export function VpcConsole() {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<Section>('vpcs')
  const [labId, setLabId] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [notice, setNotice] = useState('')
  const region = getStoredRegion()

  const labs = useQuery({ queryKey: ['labs'], queryFn: fetchLabs, retry: 1 })
  const activeLabId = labId !== '' ? labId : (labs.data?.[0]?.id ?? '')
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

  return (
    <ServiceConsole
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'VPC' }]}
      title="VPC"
      description="Virtual private clouds from your labs — real lab resources on the Rust network engine (SIMULATED backend by default). CIDRs are local (10.x); there are no AWS availability zones. Pick a lab, then create and manage its networking."
      fidelity="PARTIAL"
      fidelityFootnote="lab VPCs · engine default SIMULATED · NACLs UNSUPPORTED"
      subNav={[
        { id: 'vpcs', label: 'Your VPCs' },
        { id: 'subnets', label: 'Subnets' },
        { id: 'routetables', label: 'Route Tables' },
        { id: 'gateways', label: 'Internet Gateways' },
        { id: 'nat', label: 'NAT Gateways' },
        { id: 'securitygroups', label: 'Security Groups' },
        { id: 'nacls', label: 'Network ACLs' },
      ]}
      activeSub={section}
      onSubChange={(id) => {
        setSection(id as Section)
        setSelectedId(null)
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
          className="rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm"
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
        <p className="mb-3 border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>
      )}
      {labs.isPending && <Loading label="Loading labs" />}
      {labs.data && labs.data.length === 0 && (
        <ErrorBox message="No labs yet — launch a lab first; VPCs live inside labs." />
      )}
      {resources.isPending && activeLabId !== '' && <Loading label="Loading VPC resources" />}
      {resources.isError && <ErrorBox message={`VPC resources could not be loaded (${resources.error.message}).`} />}
      {section === 'nacls' && (
        <AwsCard title="Network ACLs">
          <p className="text-sm text-aws-muted">
            Network ACLs are <strong>UNSUPPORTED</strong> — the Rust network engine exposes no NACL RPC, so there is
            no API to build on and nothing is faked. Use security groups (stateful, rule-based) for lab firewalling.
          </p>
        </AwsCard>
      )}
      {section !== 'nacls' && activeLabId !== '' && (
        <>
          <CreateForm
            section={section}
            labId={activeLabId}
            vpcs={vpcs}
            gateways={all.filter((r) => r.kind === 'gateway')}
            onCreated={refresh}
            onNotice={setNotice}
          />
          <AwsCard title={`${sectionLabel(section)} (${rows.length}) — region ${region}`}>
            <AwsTable<LabResource & { id: string }>
              caption={sectionLabel(section)}
              emptyMessage={`No ${sectionLabel(section).toLowerCase()} recorded in this lab yet — create one above.`}
              rows={rows}
              totalCount={rows.length}
              columns={[
                { header: 'ID', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(r.id)}>{r.id}</button> },
                { header: 'Name', render: (r) => r.name },
                { header: 'CIDR', render: (r) => r.cidr ?? '—' },
                { header: 'State', render: (r) => r.state ?? '—' },
                { header: 'Parent', render: (r) => r.parent_id ?? '—' },
                { header: 'Fidelity', render: () => <FidelityBadge fidelity="PARTIAL" footnote="" /> },
              ]}
            />
            {selected && (
              <ResourceDetail
                resource={selected}
                labId={activeLabId}
                all={all}
                onChanged={refresh}
                onDelete={() => del.mutate(selected)}
                onNotice={setNotice}
              />
            )}
          </AwsCard>
        </>
      )}
    </ServiceConsole>
  )
}

function sectionLabel(section: Section): string {
  switch (section) {
    case 'vpcs': return 'Your VPCs'
    case 'subnets': return 'Subnets'
    case 'routetables': return 'Route Tables'
    case 'gateways': return 'Internet Gateways'
    case 'nat': return 'NAT Gateways'
    case 'securitygroups': return 'Security Groups'
    case 'nacls': return 'Network ACLs'
  }
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
          throw new Error('Network ACLs are UNSUPPORTED.')
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

function ResourceDetail({ resource, labId, all, onChanged, onDelete, onNotice }: {
  resource: LabResource
  labId: string
  all: LabResource[]
  onChanged: () => void
  onDelete: () => void
  onNotice: (s: string) => void
}) {
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
        <p className="mt-2 text-sm text-aws-muted">NAT gateways are engine-managed; delete the resource to remove it (the engine DeleteNAT runs on delete).</p>
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
