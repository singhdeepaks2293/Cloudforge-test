import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import {
  AwsActionsMenu,
  AwsFlash,
  AwsPreferencesButton,
  AwsRefreshButton,
  AwsStat,
  AwsToolbar,
  AwsWizard,
} from '../components/AwsShell.tsx'
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
 * EC2 console — mirrors the AWS EC2 Instances console: service
 * sub-navigation, a toolbar with Instance state / Actions / Refresh /
 * Launch instances, an instances table with checkboxes and state
 * filters, detail tabs Details / Security / Networking / Storage /
 * Monitoring / Status Checks / Logs / Tags / Events, and a multi-step
 * Launch instance wizard.
 *
 * Instances here are lab sessions: a lab IS the instance, and its
 * instance-kind lab resources are the VMs inside it. Nothing is faked:
 * Start re-provisions a STOPPED lab, Reboot is a real host-agent
 * OpRebootVM (honest 503), EBS is UNSUPPORTED.
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

const WIZARD_STEPS = ['Name and tags', 'Application and OS Images', 'Instance type', 'Network settings', 'Summary']

export function Ec2Console() {
  const queryClient = useQueryClient()
  const instances = useQuery({ queryKey: ['ec2-instances'], queryFn: loadInstances, retry: 1 })
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const definitions = useQuery({ queryKey: ['lab-definitions'], queryFn: fetchLabDefinitions, retry: 1 })
  const [activeSub, setActiveSub] = useState('instances')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [tab, setTab] = useState<string>('details')
  const [notice, setNotice] = useState('')
  const [noticeTone, setNoticeTone] = useState<'success' | 'info' | 'error'>('info')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [wizardDef, setWizardDef] = useState('ec2-basics')
  const [wizardName, setWizardName] = useState('')
  const [filter, setFilter] = useState('')
  const [stateFilter, setStateFilter] = useState('all')

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['ec2-instances'] })
    void queryClient.invalidateQueries({ queryKey: ['labs'] })
  }
  const flash = (message: string, tone: 'success' | 'info' | 'error' = 'info') => {
    setNotice(message)
    setNoticeTone(tone)
  }
  const launch = useMutation({
    mutationFn: () => createLab(wizardDef, wizardName.trim() === '' ? undefined : wizardName.trim()),
    onSuccess: (lab) => {
      flash(`Instance ${lab.id} launching — state becomes READY only when provisioning succeeds.`, 'success')
      setWizardOpen(false)
      setWizardStep(0)
      setWizardName('')
      refresh()
    },
    onError: (e: Error) => flash(`Launch failed: ${e.message}`, 'error'),
  })
  const start = useMutation({
    mutationFn: (id: string) => startLab(id),
    onSuccess: (lab) => {
      flash(`Instance ${lab.id} is re-provisioning (PROVISIONING) — it reports READY only when that succeeds.`)
      refresh()
    },
    onError: (e: Error) => flash(`Start failed: ${e.message}`, 'error'),
  })
  const reboot = useMutation({
    mutationFn: (id: string) => rebootLab(id),
    onSuccess: (lab) => {
      flash(`Reboot requested for instance ${lab.id}.`)
      refresh()
    },
    onError: (e: Error) => flash(`Reboot failed: ${e.message}`, 'error'),
  })
  const stop = useMutation({
    mutationFn: (id: string) => stopLab(id),
    onSuccess: (lab) => {
      flash(`Instance ${lab.id} is stopping.`)
      refresh()
    },
    onError: (e: Error) => flash(`Stop failed: ${e.message}`, 'error'),
  })
  const terminate = useMutation({
    mutationFn: (id: string) => destroyLab(id),
    onSuccess: () => {
      flash('Instance termination requested — the worker destroys it and marks resources CLEANED.', 'success')
      setSelectedId(null)
      setCheckedIds([])
      refresh()
    },
    onError: (e: Error) => flash(`Terminate failed: ${e.message}`, 'error'),
  })

  const allRows = instances.data ?? []
  const rows = allRows.filter((r) => {
    if (stateFilter !== 'all' && r.status !== stateFilter) return false
    const q = filter.trim().toLowerCase()
    if (q === '') return true
    return [r.id, r.name, r.definition_id ?? '', r.status, r.vpcName ?? '', r.subnetName ?? '']
      .some((v) => v.toLowerCase().includes(q))
  })
  const selected = allRows.find((r) => r.id === selectedId) ?? null
  const region = storedRegion()
  const canRun = selected !== null && ['READY', 'RUNNING'].includes(selected.status)
  const canStart = selected !== null && selected.status === 'STOPPED'
  const canTerminate = selected !== null && !['DESTROYING', 'DESTROYED'].includes(selected.status)
  const definition = (definitions.data ?? []).find((d) => d.id === wizardDef)
  const lite = health.data?.lite === true

  const launchButton = (
    <AwsButton
      variant="primary"
      onClick={() => {
        setWizardOpen(true)
        setWizardStep(0)
        setActiveSub('instances')
      }}
    >
      Launch instances
    </AwsButton>
  )

  return (
    <ServiceConsole
      icon="server"
      breadcrumb={[
        { label: 'CloudForge', to: '/' },
        { label: 'Compute', to: '/services' },
        { label: 'EC2' },
      ]}
      title="EC2 — Instances"
      description="Lab sessions as instances, in the local region. Launching creates a lab; the worker provisions it and the state only becomes READY when provisioning actually succeeded."
      fidelity="PARTIAL"
      fidelityFootnote="labs SIMULATED · no real AWS hardware"
      subNav={[
        { id: 'instances', label: 'Instances' },
        { id: 'amis', label: 'AMIs' },
        { id: 'instance-types', label: 'Instance types' },
        { id: 'volumes', label: 'Volumes', disabledReason: 'EBS unsupported — no volume API' },
        { id: 'security-groups', label: 'Security groups' },
        { id: 'elastic-ips', label: 'Elastic IPs', disabledReason: 'No Elastic IP operation' },
        { id: 'launch-templates', label: 'Launch templates', disabledReason: 'Not modelled' },
      ]}
      activeSub={activeSub}
      onSubChange={setActiveSub}
      actions={launchButton}
    >
      {notice !== '' && <AwsFlash tone={noticeTone}>{notice}</AwsFlash>}

      {activeSub === 'instances' && (
        <div className="space-y-4">
          {wizardOpen && (
            <AwsWizard
              title="Launch an instance"
              steps={WIZARD_STEPS}
              current={wizardStep}
              onStep={setWizardStep}
              onCancel={() => setWizardOpen(false)}
              onSubmit={() => launch.mutate()}
              submitLabel="Launch instance"
              busy={launch.isPending}
            >
              {wizardStep === 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold">Name and tags</h3>
                  <p className="text-sm text-aws-muted">
                    Names are CloudForge lab names. Tags are set on the lab's resources after launch, on the Tags tab.
                  </p>
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">Name</span>
                    <input
                      value={wizardName}
                      onChange={(e) => setWizardName(e.target.value)}
                      placeholder="Optional — defaults to the lab name"
                      className="w-full max-w-md rounded-sm border border-aws-border-dark px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
                    />
                  </label>
                </div>
              )}
              {wizardStep === 1 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Application and OS Images (Amazon Machine Image)</h3>
                  <p className="text-sm text-aws-muted">
                    These are CloudForge lab definitions, not real AWS AMIs. Choosing one selects the topology and
                    software the lab provisions.
                  </p>
                  {definitions.isPending && <Loading label="Loading images" />}
                  {definitions.isError && <ErrorBox message={`Lab definitions could not be loaded (${definitions.error.message}).`} />}
                  <div className="divide-y divide-aws-border border border-aws-border">
                    {(definitions.data ?? []).map((d) => {
                      const unavailable = lite && d.duration_minutes > 30
                      return (
                        <label
                          key={d.id}
                          className={`flex cursor-pointer items-start gap-2 px-3 py-2 text-sm hover:bg-[#f9f9f9] ${unavailable ? 'cursor-not-allowed opacity-50' : ''}`}
                        >
                          <input
                            type="radio"
                            name="ec2-image"
                            className="mt-1"
                            value={d.id}
                            checked={wizardDef === d.id}
                            disabled={unavailable}
                            onChange={() => setWizardDef(d.id)}
                          />
                          <span>
                            <span className="font-medium">{d.title}</span>{' '}
                            <span className="text-aws-muted">({d.id}) · {d.duration_minutes} min</span>
                            {unavailable && <span className="block text-xs">Not available in lite mode.</span>}
                          </span>
                        </label>
                      )
                    })}
                  </div>
                </div>
              )}
              {wizardStep === 2 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Instance type</h3>
                  <p className="text-sm text-aws-muted">
                    CloudForge labs use lab profiles, not AWS instance types. vCPU/memory specs are not modelled, so
                    none are shown here.
                  </p>
                  <div className="border border-aws-border px-3 py-2 text-sm">
                    <p className="font-medium">{definition?.title ?? wizardDef}</p>
                    <p className="text-aws-muted">Profile id: {wizardDef} · {definition?.duration_minutes ?? '—'} min</p>
                    <p className="mt-1 text-xs text-aws-muted">
                      {lite ? 'Lite mode: only profiles up to 30 minutes are available.' : 'Full mode: all lab profiles are available.'}
                    </p>
                  </div>
                </div>
              )}
              {wizardStep === 3 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Network settings</h3>
                  <p className="text-sm text-aws-muted">
                    The lab definition creates its own VPC, subnets, route tables and security groups. Those are real
                    local resources managed on the VPC console — they are not chosen here, and no AWS network is
                    touched.
                  </p>
                  <Link to="/services/vpc" className="text-sm text-aws-link hover:underline">
                    Open the VPC console
                  </Link>
                </div>
              )}
              {wizardStep === 4 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Summary</h3>
                  <dl className="divide-y divide-aws-border border-y border-aws-border">
                    <SummaryRow k="Name" v={wizardName.trim() === '' ? '(default lab name)' : wizardName.trim()} />
                    <SummaryRow k="Image / lab definition" v={`${definition?.title ?? wizardDef} (${wizardDef})`} />
                    <SummaryRow k="Instance type" v={wizardDef} />
                    <SummaryRow k="Network" v="From the lab definition (VPC console)" />
                    <SummaryRow k="Region" v={region} />
                  </dl>
                  <p className="text-aws-muted">
                    Launching creates a lab. The worker provisions it; the instance reports READY only when
                    provisioning actually succeeded, and failures keep their real error.
                  </p>
                </div>
              )}
            </AwsWizard>
          )}

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <AwsStat label="Instances" value={allRows.length} hint="Lab sessions" />
            <AwsStat label="Running / ready" value={allRows.filter((r) => ['READY', 'RUNNING'].includes(r.status)).length} />
            <AwsStat label="Stopped" value={allRows.filter((r) => r.status === 'STOPPED').length} />
            <AwsStat label="Region" value={region} hint="Local only" />
          </div>

          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={instances.isFetching} />
                <AwsPreferencesButton disabledReason="Table preferences are not persisted in CloudForge." />
              </>
            }
          >
            <AwsActionsMenu
              label="Instance state"
              items={[
                {
                  label: 'Start instance',
                  onClick: () => selected && start.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canStart ? 'Only STOPPED instances can be started' : undefined,
                },
                {
                  label: 'Stop instance',
                  onClick: () => selected && stop.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canRun ? 'Only READY/RUNNING instances can be stopped' : undefined,
                },
                {
                  label: 'Reboot instance',
                  onClick: () => selected && reboot.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canRun ? 'Only READY/RUNNING instances can be rebooted' : 'Real host-agent OpRebootVM; honest 503 when unavailable',
                },
                {
                  label: 'Terminate instance',
                  onClick: () => selected && terminate.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canTerminate ? 'Already terminating' : undefined,
                  danger: true,
                },
              ]}
            />
            <AwsActionsMenu
              label="Actions"
              items={[
                { label: 'Connect', disabledReason: 'No EC2 Instance Connect / SSH bridge in CloudForge — use the lab workstation terminal' },
                { label: 'Monitor and troubleshoot', onClick: () => setTab('monitoring'), disabledReason: selected === null ? 'Select an instance first' : undefined },
                { label: 'Manage tags', onClick: () => setTab('tags'), disabledReason: selected === null ? 'Select an instance first' : undefined },
                { label: 'Create image', disabledReason: 'Creating AMIs is not supported' },
                { label: 'Attach IAM role', disabledReason: 'No instance-profile operation' },
              ]}
            />
          </AwsToolbar>

          <div className="flex flex-wrap items-center gap-2">
            <input
              type="search"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter instances by ID, name, type, VPC…"
              aria-label="Filter instances"
              className="w-80 max-w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
            />
            <label className="flex items-center gap-1 text-sm">
              <span className="text-aws-muted">Instance state</span>
              <select
                value={stateFilter}
                onChange={(e) => setStateFilter(e.target.value)}
                className="rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm"
              >
                <option value="all">All states</option>
                <option value="READY">ready</option>
                <option value="RUNNING">running</option>
                <option value="STOPPED">stopped</option>
                <option value="FAILED">failed</option>
              </select>
            </label>
            <span className="text-xs text-aws-muted">
              {rows.length} of {allRows.length} shown
            </span>
          </div>

          {instances.isPending && <Loading label="Loading instances" />}
          {instances.isError && <ErrorBox message={`Instances could not be loaded (${instances.error.message}).`} />}
          {instances.data && (
            <AwsCard>
              <AwsTable<InstanceRow & { id: string }>
                caption="EC2 instances"
                emptyMessage="No instances match this filter — launch one, or clear the filter."
                rows={rows}
                totalCount={allRows.length}
                selectable
                selectedIds={checkedIds}
                onToggleRow={(id) => {
                  setSelectedId(id)
                  setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
                }}
                onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
                columns={[
                  {
                    header: 'Instance ID',
                    render: (r) => (
                      <button
                        type="button"
                        className="text-aws-link hover:underline"
                        onClick={() => {
                          setSelectedId(r.id)
                          setTab('details')
                        }}
                      >
                        {r.id}
                      </button>
                    ),
                  },
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
                  ['status', 'Status checks'],
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
      )}

      {activeSub === 'amis' && (
        <div className="space-y-3">
          <AwsFlash tone="info">
            These are CloudForge lab definitions exposed as "images". They are not Amazon Machine Images and no AWS
            image is queried.
          </AwsFlash>
          {definitions.isPending && <Loading label="Loading images" />}
          {definitions.isError && <ErrorBox message={`Lab definitions could not be loaded (${definitions.error.message}).`} />}
          {definitions.data && (
            <AwsCard title={`Lab images (${definitions.data.length})`}>
              <AwsTable
                caption="Lab definitions as images"
                emptyMessage="No lab definitions registered."
                rows={definitions.data}
                totalCount={definitions.data.length}
                columns={[
                  { header: 'Image ID', render: (d) => d.id },
                  { header: 'Name', render: (d) => d.title },
                  { header: 'Duration', render: (d) => `${d.duration_minutes} min` },
                  { header: 'Lite mode', render: (d) => (lite && d.duration_minutes > 30 ? 'Unavailable' : 'Available') },
                  { header: 'Description', render: (d) => d.description ?? '—' },
                ]}
              />
            </AwsCard>
          )}
        </div>
      )}

      {activeSub === 'instance-types' && (
        <div className="space-y-3">
          <AwsFlash tone="info">
            CloudForge has no real instance-type catalogue. Each lab definition is its own profile; vCPU and memory
            specs are not modelled, so this page does not invent them.
          </AwsFlash>
          {definitions.data && (
            <AwsCard title="Lab profiles">
              <AwsTable
                caption="Lab profiles"
                emptyMessage="No profiles registered."
                rows={definitions.data}
                totalCount={definitions.data.length}
                columns={[
                  { header: 'Profile', render: (d) => d.id },
                  { header: 'Name', render: (d) => d.title },
                  { header: 'Max duration', render: (d) => `${d.duration_minutes} min` },
                  { header: 'Specs', render: () => 'Not modelled' },
                ]}
              />
            </AwsCard>
          )}
        </div>
      )}

      {activeSub === 'security-groups' && (
        <div className="space-y-3">
          <AwsFlash tone="info">
            Security groups are created by labs and managed on the VPC console. This view lists only resources of the
            instances already loaded here.
          </AwsFlash>
          <AwsCard title="Security groups from your instances">
            {allRows.every((r) => r.resources.every((x) => x.kind !== 'security-group')) ? (
              <p className="text-sm text-aws-muted">No security groups recorded yet. Launch an instance to create one.</p>
            ) : (
              <ul className="space-y-1 text-sm">
                {allRows.flatMap((r) =>
                  r.resources
                    .filter((x) => x.kind === 'security-group')
                    .map((x) => (
                      <li key={`${r.id}-${x.id}`}>
                        <strong>{x.name}</strong> ({x.id}) — {x.state ?? 'unknown state'} · instance {r.id}
                      </li>
                    )),
                )}
              </ul>
            )}
            <Link to="/services/vpc" className="mt-3 inline-block text-sm text-aws-link hover:underline">
              Open the VPC console to manage security groups
            </Link>
          </AwsCard>
        </div>
      )}
    </ServiceConsole>
  )
}

function SummaryRow({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex gap-2 px-1 py-2">
      <dt className="w-48 shrink-0 font-medium">{k}</dt>
      <dd className="break-all">{v}</dd>
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
