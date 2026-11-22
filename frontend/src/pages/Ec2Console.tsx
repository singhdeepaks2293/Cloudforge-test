import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge, StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import {
  createLab,
  destroyLab,
  fetchHealth,
  fetchLabDefinitions,
  fetchLabEvents,
  fetchLabResources,
  fetchLabs,
  fetchMonitoring,
  rebootLab,
  setResourceTags,
  startLab,
  stopLab,
} from '../services/api.ts'
import { getStoredRegion as storedRegion } from '../services/catalog.ts'
import type { Lab, LabResource } from '../types/index.ts'

/**
 * EC2 console (Phase 2) — mirrors the AWS EC2 Instances console:
 * Instances table (Instance ID / Name / State / Type / VPC / Subnet /
 * AZ / Launch time), detail tabs Details / Security / Networking /
 * Storage / Monitoring / Status Checks / Logs / Tags / Events, Launch
 * instance wizard (image, instance type, network, security group,
 * storage — from the real lab definitions and the lab's resources),
 * Start/Stop/Reboot/Terminate actions. Instances here are lab
 * sessions: a lab IS the instance, and its instance-kind lab
 * resources are the VMs inside it.
 *
 * Honest limits: Start re-provisions a STOPPED lab (202
 * PROVISIONING; READY only when provisioning actually succeeds).
 * Reboot is a real host-agent OpRebootVM and is an honest 503 when
 * no host agent or no VM is recorded. Storage shows that EBS is
 * UNSUPPORTED. Logs/Events are the lab's real audit history.
 */
interface InstanceRow extends Lab {
  vpcName?: string
  subnetName?: string
  instanceCount: number
  resources: LabResource[]
}

async function loadInstances(): Promise<InstanceRow[]> {
  const labs = await fetchLabs()
  const rows: InstanceRow[] = []
  for (const lab of labs) {
    const resources = await fetchLabResources(lab.id).catch(() => [] as LabResource[])
    const vpc = resources.find((r) => r.kind === 'vpc')
    const subnet = resources.find((r) => r.kind === 'subnet')
    rows.push({
      ...lab,
      vpcName: vpc?.name,
      subnetName: subnet?.name,
      instanceCount: resources.filter((r) => r.kind === 'instance').length,
      resources,
    })
  }
  return rows
}

export function Ec2Console() {
  const queryClient = useQueryClient()
  const instances = useQuery({ queryKey: ['ec2-instances'], queryFn: loadInstances, retry: 1 })
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const definitions = useQuery({ queryKey: ['lab-definitions'], queryFn: fetchLabDefinitions, retry: 1 })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [tab, setTab] = useState<string>('details')
  const [notice, setNotice] = useState('')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardDef, setWizardDef] = useState('ec2-basics')
  const [wizardName, setWizardName] = useState('')

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['ec2-instances'] })
    void queryClient.invalidateQueries({ queryKey: ['labs'] })
  }
  const launch = useMutation({
    mutationFn: () => createLab(wizardDef, wizardName.trim() === '' ? undefined : wizardName.trim()),
    onSuccess: (lab) => {
      setNotice(`Instance ${lab.id} launching — state becomes READY only when provisioning succeeds.`)
      setWizardOpen(false)
      setWizardName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Launch failed: ${e.message}`),
  })
  const start = useMutation({
    mutationFn: (id: string) => startLab(id),
    onSuccess: (lab) => {
      setNotice(`Instance ${lab.id} is re-provisioning (PROVISIONING) — it reports READY only when that succeeds.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Start failed: ${e.message}`),
  })
  const reboot = useMutation({
    mutationFn: (id: string) => rebootLab(id),
    onSuccess: (lab) => {
      setNotice(`Reboot requested for instance ${lab.id}.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Reboot failed: ${e.message}`),
  })
  const stop = useMutation({
    mutationFn: (id: string) => stopLab(id),
    onSuccess: (lab) => {
      setNotice(`Instance ${lab.id} is stopping.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Stop failed: ${e.message}`),
  })
  const terminate = useMutation({
    mutationFn: (id: string) => destroyLab(id),
    onSuccess: () => {
      setNotice('Instance termination requested — the worker destroys it and marks resources CLEANED.')
      setSelectedId(null)
      refresh()
    },
    onError: (e: Error) => setNotice(`Terminate failed: ${e.message}`),
  })

  const rows = instances.data ?? []
  const selected = rows.find((r) => r.id === selectedId) ?? null
  const region = storedRegion()

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[
          { label: 'CloudForge', to: '/' },
          { label: 'Compute', to: '/services' },
          { label: 'EC2' },
        ]}
        title="EC2 — Instances"
        description="Lab sessions as instances, in the local region. Launching creates a lab; the worker provisions it and the state only becomes READY when provisioning actually succeeded."
        actions={
          <div className="flex items-center gap-2">
            <FidelityBadge fidelity="PARTIAL" footnote="labs SIMULATED · no real AWS hardware" />
            <AwsButton onClick={() => setWizardOpen(true)}>Launch instance</AwsButton>
          </div>
        }
      />
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">
          {notice}
        </p>
      )}
      {wizardOpen && (
        <AwsCard title="Launch instance">
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              launch.mutate()
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor="ec2-wizard-def" className="text-sm font-medium">Image / lab definition</label>
              <select
                id="ec2-wizard-def"
                value={wizardDef}
                onChange={(e) => setWizardDef(e.target.value)}
                className="rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm"
              >
                {(definitions.data ?? []).map((d) => (
                  <option key={d.id} value={d.id} disabled={health.data?.lite === true && d.duration_minutes > 30}>
                    {d.title} — {d.duration_minutes} min{health.data?.lite === true && d.duration_minutes > 30 ? ' (not available in lite mode)' : ''}
                  </option>
                ))}
              </select>
              <label htmlFor="ec2-wizard-name" className="text-sm font-medium">Name</label>
              <input
                id="ec2-wizard-name"
                value={wizardName}
                onChange={(e) => setWizardName(e.target.value)}
                placeholder="Optional — defaults to the lab name"
                className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
              />
            </div>
            <p className="text-sm text-aws-muted">
              Network, security group and storage come from the lab definition's resources (VPC/subnets/security
              groups are created by the lab and managed on the VPC console). Instance types are the lab profiles
              (Micro in lite mode). Launching creates a lab; the worker provisions it and the state only becomes
              READY when provisioning actually succeeded.
            </p>
            {definitions.isError && <ErrorBox message={`Lab definitions could not be loaded (${definitions.error.message}).`} />}
            <div className="flex gap-2">
              <AwsButton type="submit">Launch instance</AwsButton>
              <AwsButton onClick={() => setWizardOpen(false)}>Cancel</AwsButton>
              <Link to="/labs" className="rounded-sm border border-aws-border-dark bg-white px-3 py-1.5 text-sm hover:bg-[#f2f3f3]">
                Open Labs launcher
              </Link>
            </div>
          </form>
        </AwsCard>
      )}
      {instances.isPending && <Loading label="Loading instances" />}
      {instances.isError && <ErrorBox message={`Instances could not be loaded (${instances.error.message}).`} />}
      {instances.data && (
        <AwsCard title={`Instances (${rows.length}) — region ${region}`}>
          <AwsTable<InstanceRow & { id: string }>
            caption="EC2 instances"
            emptyMessage="No instances yet — launch one from the Labs page."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Instance ID', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelectedId(r.id); setTab('details') }}>{r.id}</button> },
              { header: 'Name', render: (r) => r.name },
              { header: 'Instance state', render: (r) => <StateBadge state={r.status} /> },
              { header: 'Instance type', render: (r) => r.definition_id ?? '—' },
              { header: 'VPC', render: (r) => r.vpcName ?? '—' },
              { header: 'Subnet', render: (r) => r.subnetName ?? '—' },
              { header: 'Availability Zone', render: () => 'cf-local-1a' },
              { header: 'Launch time', render: (r) => (r.created_at ? new Date(r.created_at).toLocaleString() : '—') },
              { header: 'VMs', render: (r) => String(r.instanceCount) },
            ]}
          />
          <div className="mt-3 flex flex-wrap gap-2">
            <AwsButton
              disabled={selected === null || !['READY', 'RUNNING'].includes(selected.status)}
              title={selected === null ? 'Select an instance first' : 'Stop the selected instance'}
              onClick={() => selected && stop.mutate(selected.id)}
            >
              Stop instance
            </AwsButton>
            <AwsButton
              disabled={selected === null || selected.status !== 'STOPPED'}
              title={selected === null ? 'Select an instance first' : selected.status === 'STOPPED' ? 'Re-provision the stopped instance (202 PROVISIONING)' : 'Start is only valid for a STOPPED instance'}
              onClick={() => selected && start.mutate(selected.id)}
            >
              Start instance
            </AwsButton>
            <AwsButton
              disabled={selected === null || !['READY', 'RUNNING'].includes(selected.status)}
              title={selected === null ? 'Select an instance first' : 'Reboot via the host agent (real OpRebootVM; honest 503 when no host agent or no VM is recorded)'}
              onClick={() => selected && reboot.mutate(selected.id)}
            >
              Reboot instance
            </AwsButton>
            <AwsButton
              disabled={selected === null || ['DESTROYING', 'DESTROYED'].includes(selected.status)}
              title={selected === null ? 'Select an instance first' : 'Terminate the selected instance'}
              onClick={() => selected && terminate.mutate(selected.id)}
            >
              Terminate instance
            </AwsButton>
            <Link to="/labs" className="rounded-sm border border-aws-border-dark bg-white px-3 py-1.5 text-sm hover:bg-[#f2f3f3]">
              Open Labs launcher
            </Link>
          </div>
        </AwsCard>
      )}
      {selected && (
        <AwsCard title={`Instance ${selected.id}`}>
          <div className="mb-3 flex flex-wrap gap-1 border-b border-aws-border" role="tablist" aria-label="Instance detail tabs">
            {([
              ['details', 'Details'],
              ['security', 'Security'],
              ['networking', 'Networking'],
              ['storage', 'Storage'],
              ['monitoring', 'Monitoring'],
              ['status', 'Status Checks'],
              ['logs', 'Logs'],
              ['tags', 'Tags'],
              ['events', 'Events'],
            ] as const).map(([id, label]) => (
              <button
                key={id}
                type="button"
                role="tab"
                aria-selected={tab === id}
                onClick={() => setTab(id)}
                className={`border-b-2 px-3 py-2 text-sm font-medium ${tab === id ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted hover:text-aws-text'}`}
              >
                {label}
              </button>
            ))}
          </div>
          {tab === 'details' && (
            <dl className="space-y-2 text-sm">
              <DetailRow k="Instance ID" v={selected.id} />
              <DetailRow k="Name" v={selected.name} />
              <DetailRow k="State" v={selected.status} />
              <DetailRow k="Instance type" v={selected.definition_id ?? '—'} />
              <DetailRow k="Region / AZ" v={`${region} / cf-local-1a`} />
              <DetailRow k="Launch time" v={selected.created_at ? new Date(selected.created_at).toLocaleString() : '—'} />
              <DetailRow k="Lab resources" v={selected.resources.map((r) => `${r.kind}:${r.name}`).join(', ') || 'none recorded'} />
              {selected.error && <DetailRow k="Provisioning error" v={selected.error} />}
            </dl>
          )}
          {tab === 'security' && (
            <ResourceList resources={selected.resources.filter((r) => r.kind === 'security-group')} empty="No security groups recorded for this lab yet." label="Security groups" />
          )}
          {tab === 'networking' && (
            <ResourceList resources={selected.resources.filter((r) => ['vpc', 'subnet', 'gateway', 'route-table'].includes(r.kind))} empty="No networking resources recorded for this lab yet." label="Networking" />
          )}
          {tab === 'storage' && (
            <p className="text-sm text-aws-muted">
              No EBS volumes: EBS is <strong>UNSUPPORTED</strong> in CloudForge — there is no volume API and no
              attach/detach operation. Lab instances carry their own disks; see the volume note on the{' '}
              <Link to="/services/ebs" className="text-aws-link hover:underline">EBS service page</Link>.
            </p>
          )}
          {tab === 'monitoring' && <InstanceMonitoring labId={selected.id} status={selected.status} />}
          {tab === 'status' && (
            <p className="text-sm text-aws-muted">
              System status check: {selected.status === 'READY' || selected.status === 'RUNNING' ? 'passed — the lab provisioned successfully' : `not passed — instance is ${selected.status}`}.
              Instance status check: {health.data?.network_engine_reachable ? 'engine reachable' : 'engine unreachable — provisioning would fail, never fake READY'}.
            </p>
          )}
          {tab === 'logs' && <InstanceEvents labId={selected.id} />}
          {tab === 'tags' && <InstanceTags lab={selected} onChanged={refresh} />}
          {tab === 'events' && <InstanceEvents labId={selected.id} />}
        </AwsCard>
      )}
    </div>
  )
}

function InstanceMonitoring({ labId, status }: { labId: string; status: string }) {
  const monitoring = useQuery({ queryKey: ['monitoring'], queryFn: fetchMonitoring, retry: 1 })
  const engine = monitoring.data?.metrics.find((m) => m.name === 'cloudforge_network_engine_up')
  return (
    <div className="space-y-2 text-sm">
      <p>
        Instance state: <strong>{status}</strong>. Local control-plane health (live from /api/v1/monitoring):
      </p>
      {monitoring.isPending && <Loading label="Loading monitoring data" />}
      {monitoring.isError && <ErrorBox message={`Monitoring could not be loaded (${monitoring.error.message}).`} />}
      {monitoring.data && (
        <dl className="space-y-1">
          {Object.entries(monitoring.data.components).map(([name, c]) => (
            <div key={name} className="flex gap-2">
              <dt className="w-28 shrink-0 font-medium">{name}</dt>
              <dd>{c.status}{c.error ? ` — ${c.error}` : ''}{c.latency_ms !== undefined ? ` (${c.latency_ms} ms)` : ''}</dd>
            </div>
          ))}
          <div className="flex gap-2">
            <dt className="w-28 shrink-0 font-medium">Network engine</dt>
            <dd>{engine ? `up = ${engine.total}` : 'series not observed yet (never zero-filled)'}</dd>
          </div>
          <div className="flex gap-2">
            <dt className="w-28 shrink-0 font-medium">Uptime</dt>
            <dd>{monitoring.data.uptime_seconds}s · API {monitoring.data.version}</dd>
          </div>
        </dl>
      )}
      <p className="text-aws-muted">
        No per-instance CloudWatch-style metrics pipeline exists — these are the control-plane metrics the
        CloudWatch console reports. Lab id for correlation: <span className="font-mono text-xs">{labId}</span>
      </p>
    </div>
  )
}

function InstanceEvents({ labId }: { labId: string }) {
  const events = useQuery({ queryKey: ['lab-events', labId], queryFn: () => fetchLabEvents(labId, 50), retry: 1 })
  if (events.isPending) return <Loading label="Loading lab events" />
  if (events.isError) return <ErrorBox message={`Lab events could not be loaded (${events.error.message}).`} />
  if (events.data.events.length === 0) return <p className="text-sm text-aws-muted">No events recorded for this lab yet.</p>
  return (
    <AwsTable<{ id: string; action: string; resource_type: string; resource_id: string; created_at: string; actor_id: string }>
      caption="Lab events (real audit history)"
      emptyMessage="No events."
      rows={events.data.events}
      totalCount={events.data.total}
      columns={[
        { header: 'Time', render: (e) => (e.created_at ? new Date(e.created_at).toLocaleString() : '—') },
        { header: 'Action', render: (e) => e.action },
        { header: 'Resource', render: (e) => `${e.resource_type} ${e.resource_id}` },
        { header: 'Actor', render: (e) => e.actor_id },
      ]}
    />
  )
}

function InstanceTags({ lab, onChanged }: { lab: InstanceRow; onChanged: () => void }) {
  const [resourceId, setResourceId] = useState(lab.resources[0]?.id ?? '')
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [notice, setNotice] = useState('')
  const resource = lab.resources.find((r) => r.id === resourceId) ?? lab.resources[0]
  const tags = resource?.labels ?? {}
  const save = useMutation({
    mutationFn: (next: Record<string, string>) => setResourceTags(lab.id, resource!.id, next),
    onSuccess: () => {
      setKey('')
      setValue('')
      onChanged()
    },
    onError: (e: Error) => setNotice(`Save tags failed: ${e.message}`),
  })
  if (lab.resources.length === 0) return <p className="text-sm text-aws-muted">No resources recorded for this instance yet — tags live on its resources.</p>
  return (
    <div className="space-y-2 text-sm">
      {notice !== '' && <p role="status">{notice}</p>}
      <label>
        Resource
        <select value={resource?.id ?? ''} onChange={(e) => setResourceId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
          {lab.resources.map((r) => (
            <option key={r.id} value={r.id}>{r.kind}:{r.name}</option>
          ))}
        </select>
      </label>
      {Object.keys(tags).length === 0 ? (
        <p className="text-aws-muted">No tags on {resource?.name} yet.</p>
      ) : (
        <ul className="space-y-1">
          {Object.entries(tags).map(([k, v]) => (
            <li key={k} className="flex items-center gap-2">
              <span><strong>{k}</strong> = {v}</span>
              <button
                type="button"
                className="text-aws-link hover:underline"
                onClick={() => {
                  const next = { ...tags }
                  delete next[k]
                  save.mutate(next)
                }}
              >
                Remove
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (key.trim() !== '' && resource) save.mutate({ ...tags, [key.trim()]: value })
        }}
      >
        <input value={key} onChange={(e) => setKey(e.target.value)} placeholder="Key" aria-label="Tag key" className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="Value" aria-label="Tag value" className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        <AwsButton type="submit" disabled={key.trim() === ''}>Add / update tag</AwsButton>
        <span className="text-aws-muted">Keys starting with aws: are reserved and rejected.</span>
      </form>
    </div>
  )
}

function DetailRow({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex gap-2">
      <dt className="w-36 shrink-0 font-medium">{k}</dt>
      <dd className="break-all">{v}</dd>
    </div>
  )
}

function ResourceList({ resources, empty, label }: { resources: LabResource[]; empty: string; label: string }) {
  if (resources.length === 0) return <p className="text-sm text-aws-muted">{empty}</p>
  return (
    <ul className="space-y-1 text-sm">
      {resources.map((r) => (
        <li key={r.id}>
          <strong>{label}:</strong> {r.name} ({r.id}) — {r.state ?? 'unknown state'}
        </li>
      ))}
    </ul>
  )
}

