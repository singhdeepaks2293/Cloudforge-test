import { useState } from 'react'
import type { ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ElbListContent } from './ElbConsole.tsx'
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
import {
  fmtTime,
  num,
  str,
  useAutoScalingGroups,
  useEbsVolumes,
  useExamAction,
  useExamCreate,
  useExamDelete,
  volumeAttachments,
  type Rec,
} from '../services/examResources.ts'
import type { Lab, LabResource } from '../types/index.ts'

/**
 * EC2 console — mirrors the AWS EC2 Instances console: service
 * sub-navigation, a toolbar with Instance state / Actions / Refresh /
 * Launch instances, an instances table with checkboxes and state
 * filters, detail tabs Details / Security / Networking / Storage /
 * Monitoring / Status Checks / Logs / Tags / Events, and a multi-step
 * Launch instance wizard.
 *
 * The long tail of the AWS EC2 left nav (2026-11-22 survey) is covered
 * too: Spot Requests, Reserved Instances, Dedicated Hosts, Capacity
 * Reservations, AMI Catalog, Public Images, Placement Groups, Key
 * Pairs and Network Interfaces are honest UNSUPPORTED empty states —
 * the survey records those pages' names but no list columns, so no
 * table with invented columns is shown. Launch Templates, Snapshots,
 * Lifecycle Manager, Elastic IPs and Trust Stores render real sections
 * of the same kind instead of label-only disabled nav items. Savings
 * Plans, Systems Manager and Account settings are links to their real
 * pages, not tabs.
 *
 * v4 survey (2026-11-22): the Instances table uses the survey's exact
 * 17 columns in order (Name, Instance ID, Instance state, Instance
 * type, Status check, Availability Zone, Public IPv4 DNS/address,
 * Elastic IP, IPv6 IPs, Monitoring, Security group name, Key name,
 * Launch time, Platform details, Managed, Operator); only Name, ID,
 * state, type, AZ, security-group name and launch time have real
 * values, the rest render '—'. The Actions menu is the survey's full
 * menu (flat here — AwsActionsMenu has no submenus, so the survey's
 * Security / Networking / Instance settings submenus render as
 * prefixed items), wired where the backend supports it. The Launch
 * wizard is the survey's observed 8 steps (Name and tags; Application
 * and OS Images; Instance type; Key pair; Network settings; Configure
 * storage; Advanced details; Summary). New nav items: AWS Global View,
 * Events, UltraServers (New) and Capacity Manager (New) — honest
 * UNSUPPORTED sections. EBS Snapshots, Public Images, Systems Manager
 * and Account settings are kept even though the new survey drops them:
 * the old survey recorded them and the reconciliation tests pin them.
 *
 * Instances here are lab sessions: a lab IS the instance, and its
 * instance-kind lab resources are the VMs inside it. Nothing is faked:
 * Start re-provisions a STOPPED lab, Reboot is a real host-agent
 * OpRebootVM (honest 503). EBS volumes and Auto Scaling groups are
 * API-EMULATED teaching records (backend/internal/httpapi/exam.go):
 * volumes support create/list/attach/detach/delete and groups support
 * create/list/scale/delete, but attach is recorded only (no disk is
 * really attached) and scale records capacity only (no instance is
 * really launched). EBS snapshots and lifecycle policies have no
 * backend and stay UNSUPPORTED.
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

const WIZARD_STEPS = ['Name and tags', 'Application and OS Images', 'Instance type', 'Key pair', 'Network settings', 'Configure storage', 'Advanced details', 'Summary']

export function Ec2Console({ initialSection }: { initialSection?: string }) {
  const queryClient = useQueryClient()
  const instances = useQuery({ queryKey: ['ec2-instances'], queryFn: loadInstances, retry: 1 })
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const definitions = useQuery({ queryKey: ['lab-definitions'], queryFn: fetchLabDefinitions, retry: 1 })
  // EBS volumes and Auto Scaling groups are real API-EMULATED backends
  // (exam.go); the home cards and the instance Storage tab read the same
  // queries the Volumes / Auto Scaling sections use below.
  const ebs = useEbsVolumes()
  const asgs = useAutoScalingGroups()
  const [searchParams] = useSearchParams()
  const [activeSub, setActiveSub] = useState(searchParams.get('section') ?? initialSection ?? 'instances')
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
        { id: 'home', label: 'EC2 dashboard' },
        { id: 'global-view', label: 'AWS Global View', disabledReason: 'No global-view API in CloudForge — there is one local region only.' },
        { id: 'events', label: 'Events', disabledReason: 'EC2 scheduled events are not modelled in CloudForge — per-instance audit events are on the instance Events tab.' },
        { id: 'instances', label: 'Instances' },
        { id: 'instance-types', label: 'Instance types' },
        { id: 'launch-templates', label: 'Launch templates' },
        { id: 'spot-requests', label: 'Spot Requests' },
        { id: 'savings-plans', label: 'Savings Plans', to: '/services/savingsplans' },
        { id: 'reserved-instances', label: 'Reserved Instances' },
        { id: 'dedicated-hosts', label: 'Dedicated Hosts' },
        { id: 'capacity-reservations', label: 'Capacity Reservations' },
        { id: 'ultraservers', label: 'UltraServers', badge: 'New', disabledReason: 'No UltraServers in CloudForge — no ultra-scale cluster hardware.' },
        { id: 'capacity-manager', label: 'Capacity Manager', badge: 'New', disabledReason: 'No Capacity Manager API in CloudForge.' },
        { id: 'amis', label: 'AMIs', group: 'Images' },
        { id: 'ami-catalog', label: 'AMI Catalog', group: 'Images' },
        { id: 'ebs-snapshots', label: 'EBS Snapshots', group: 'Images' },
        { id: 'public-images', label: 'Public Images', group: 'Images' },
        { id: 'volumes', label: 'Volumes', group: 'Elastic Block Store' },
        { id: 'snapshots', label: 'Snapshots', group: 'Elastic Block Store' },
        { id: 'lifecycle', label: 'Lifecycle Manager', group: 'Elastic Block Store' },
        { id: 'security-groups', label: 'Security groups', group: 'Network & Security' },
        { id: 'elastic-ips', label: 'Elastic IPs', group: 'Network & Security' },
        { id: 'placement-groups', label: 'Placement Groups', group: 'Network & Security' },
        { id: 'key-pairs', label: 'Key Pairs', group: 'Network & Security' },
        { id: 'network-interfaces', label: 'Network Interfaces', group: 'Network & Security' },
        { id: 'load-balancers', label: 'Load Balancers', group: 'Load Balancing' },
        { id: 'target-groups', label: 'Target Groups', group: 'Load Balancing' },
        { id: 'trust-stores', label: 'Trust Stores', group: 'Load Balancing' },
        { id: 'autoscaling', label: 'Auto Scaling Groups', group: 'Auto Scaling' },
        { id: 'systems-manager', label: 'Systems Manager', to: '/services/ssm' },
        { id: 'account-settings', label: 'Account settings', to: '/account' },
      ]}
      activeSub={activeSub}
      onSubChange={setActiveSub}
      actions={launchButton}
    >
      {notice !== '' && <AwsFlash tone={noticeTone}>{notice}</AwsFlash>}

      {activeSub === 'home' && (
        <ServiceHomeDashboard
          serviceName="EC2"
          banner="Lab sessions as instances, in the local region. Launching creates a lab; the worker provisions it and the state only becomes READY when provisioning actually succeeded."
          actions={[
            {
              label: 'Launch instance',
              primary: true,
              onClick: () => {
                setWizardOpen(true)
                setWizardStep(0)
                setActiveSub('instances')
              },
            },
            { label: 'Open VPC console', to: '/services/vpc' },
          ]}
          cards={[
            { label: 'Instances', count: instances.data?.length ?? '—', onSelect: () => setActiveSub('instances'), note: 'Lab sessions as instances' },
            { label: 'AMIs', count: definitions.data?.length ?? '—', onSelect: () => setActiveSub('amis'), note: 'CloudForge lab definitions, not real AWS AMIs' },
            { label: 'Volumes', count: ebs.isPending ? '—' : ebs.rows.length, onSelect: () => setActiveSub('volumes'), note: 'API-EMULATED — durable records; attach/detach is recorded only, no disk is really attached.' },
            { label: 'Load Balancers', count: '—', onSelect: () => setActiveSub('load-balancers'), note: 'SIMULATED round-robin with real health probes' },
            { label: 'Auto Scaling Groups', count: asgs.isPending ? '—' : asgs.rows.length, onSelect: () => setActiveSub('autoscaling'), note: 'API-EMULATED — groups with min/desired/max and a scale action; nothing is really launched.' },
            { label: 'Elastic IPs', count: 0, onSelect: () => setActiveSub('elastic-ips'), note: 'No Elastic IP operation — see the Elastic IPs section.' },
            { label: 'Security Groups', count: '—', to: '/services/vpc', note: 'Managed on the VPC console' },
            { label: 'Launch Templates', count: 0, onSelect: () => setActiveSub('launch-templates'), note: 'Launch templates are UNSUPPORTED — not modelled.' },
          ]}
          onRefresh={refresh}
          refreshing={instances.isFetching}
          health={[
            health.data !== undefined
              ? `Control plane: ${health.data.ok ? 'healthy' : 'degraded'}; network engine ${health.data.network_engine_reachable ? 'reachable' : 'unreachable'}.`
              : 'Control-plane health loading…',
            instances.isError ? 'Instances could not be loaded.' : `${instances.data?.length ?? 0} instance(s) listed.`,
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — EC2 track', to: '/learn' },
            { label: 'Auto Scaling groups (API-EMULATED)', to: '/services/ec2?section=autoscaling' },
          ]}
        />
      )}

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
                  <p className="text-sm text-aws-muted">
                    Image source: Quick Start only — CloudForge offers its own lab definitions and no My AMIs,
                    Marketplace, Community or Shared images.
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
                  <p className="text-sm text-aws-muted">
                    Compare and Get advice are not available — there are no AWS instance types to compare; the
                    profile is the lab definition.
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
                  <h3 className="text-sm font-semibold">Key pair (login)</h3>
                  <p className="text-sm text-aws-muted">
                    CloudForge has no key pairs and no SSH key injection into lab VMs — labs are reached through
                    the lab workstation terminal, not SSH. There is no key pair to select and no new key pair can
                    be created here.
                  </p>
                  <AwsButton disabled title="No key-pair API in CloudForge — no SSH key injection into lab VMs.">
                    Create new key pair
                  </AwsButton>
                </div>
              )}
              {wizardStep === 4 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Network settings</h3>
                  <p className="text-sm text-aws-muted">
                    The lab definition creates its own VPC, subnets, route tables and security groups. Those are real
                    local resources managed on the VPC console — they are not chosen here, and no AWS network is
                    touched.
                  </p>
                  <p className="text-sm text-aws-muted">
                    Firewall / security groups come from the lab definition — there is no Create security group
                    step and no protocol-rule editor (Type / Protocol / Port / Source / Description) here.
                    Security groups are managed on the VPC console after launch.
                  </p>
                  <Link to="/services/vpc" className="text-sm text-aws-link hover:underline">
                    Open the VPC console
                  </Link>
                </div>
              )}
              {wizardStep === 5 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Configure storage</h3>
                  <p className="text-sm text-aws-muted">
                    Lab instances carry their own disks — there is no real EBS volume behind a launch. The root
                    volume below is the lab disk, not an AWS volume.
                  </p>
                  <div className="border border-aws-border px-3 py-2 text-sm">
                    <p className="font-medium">Volume 1 — AMI Root · 8 GiB · gp3</p>
                    <p className="text-aws-muted">Lab disk (8 GiB, gp3-equivalent); size and type are fixed by the lab definition.</p>
                  </div>
                  <p className="text-sm text-aws-muted">
                    Additional volumes are API-EMULATED teaching records, created on the Volumes section after
                    launch — they are recorded only and no disk is really attached.
                  </p>
                  <Link to="/services/ec2?section=volumes" className="text-sm text-aws-link hover:underline">
                    Open the Volumes section
                  </Link>
                </div>
              )}
              {wizardStep === 6 && (
                <div className="space-y-2">
                  <h3 className="text-sm font-semibold">Advanced details</h3>
                  <p className="text-sm text-aws-muted">
                    Advanced details are not modelled in CloudForge — there is no user data, IAM role, instance
                    metadata option or shutdown-behaviour setting to choose. The lab definition controls
                    provisioning; the instance launches with the lab&apos;s defaults.
                  </p>
                  <dl className="divide-y divide-aws-border border-y border-aws-border text-sm">
                    <SummaryRow k="IAM role" v="None — no instance-profile operation in CloudForge" />
                    <SummaryRow k="Shutdown behavior" v="Stop — fixed; no shutdown-behavior API" />
                    <SummaryRow k="User data" v="None — not modelled" />
                  </dl>
                </div>
              )}
              {wizardStep === 7 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Summary</h3>
                  <dl className="divide-y divide-aws-border border-y border-aws-border">
                    <SummaryRow k="Name" v={wizardName.trim() === '' ? '(default lab name)' : wizardName.trim()} />
                    <SummaryRow k="Image / lab definition" v={`${definition?.title ?? wizardDef} (${wizardDef})`} />
                    <SummaryRow k="Instance type" v={wizardDef} />
                    <SummaryRow k="Key pair" v="None — no SSH keys in CloudForge labs" />
                    <SummaryRow k="Network" v="From the lab definition (VPC console)" />
                    <SummaryRow k="Storage" v="1 volume, 8 GiB gp3 (lab disk)" />
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
          {wizardOpen && (
            <p className="text-xs text-aws-muted">
              Wizard steps follow the v4 survey (2026-11-22), which opened the Launch instances wizard and
              left via Cancel: Name and tags; Application and OS Images; Instance type; Key pair; Network
              settings; Configure storage; Advanced details; Summary. Key pair, storage and advanced-details
              choices are CloudForge's own honest equivalents — no AWS key, volume or metadata is ever created.
            </p>
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
                { label: 'Create image', disabledReason: 'Creating AMIs is not supported in CloudForge' },
                { label: 'Create launch template', disabledReason: 'Launch templates are UNSUPPORTED in CloudForge — no template API' },
                { label: 'Manage image', disabledReason: 'No AMI management API in CloudForge — lab definitions are not AMIs' },
                { label: 'Security: Modify IAM role', disabledReason: 'No instance-profile operation in CloudForge' },
                { label: 'Security: Modify Identity Center identity', disabledReason: 'No Identity Center in CloudForge' },
                { label: 'Security: Get Windows password', disabledReason: 'No Windows instances in CloudForge labs' },
                { label: 'Security: Modify instance metadata', disabledReason: 'No instance-metadata API in CloudForge' },
                { label: 'Security: Manage instance protection', disabledReason: 'No termination-protection API in CloudForge' },
                { label: 'Networking: Associate Elastic IP', disabledReason: 'No Elastic IP operation in CloudForge' },
                { label: 'Networking: Disassociate Elastic IP', disabledReason: 'No Elastic IP operation in CloudForge' },
                { label: 'Networking: Attach network interface', disabledReason: 'No network-interface (ENI) API in CloudForge' },
                { label: 'Networking: Edit instance IP addresses', disabledReason: 'No instance IP-address API in CloudForge' },
                { label: 'Networking: Change source/destination check', disabledReason: 'No source/destination-check API in CloudForge' },
                { label: 'Networking: Edit ENIs', disabledReason: 'No network-interface (ENI) API in CloudForge' },
                {
                  label: 'Instance settings: View details',
                  onClick: () => setTab('details'),
                  disabledReason: selected === null ? 'Select an instance first' : undefined,
                },
                { label: 'Instance settings: Edit instance type', disabledReason: 'The instance type is the lab definition and cannot change after launch' },
                {
                  label: 'Instance settings: Start instance',
                  onClick: () => selected && start.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canStart ? 'Only STOPPED instances can be started' : undefined,
                },
                {
                  label: 'Instance settings: Stop instance',
                  onClick: () => selected && stop.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canRun ? 'Only READY/RUNNING instances can be stopped' : undefined,
                },
                {
                  label: 'Instance settings: Reboot instance',
                  onClick: () => selected && reboot.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canRun ? 'Only READY/RUNNING instances can be rebooted' : undefined,
                },
                { label: 'Instance settings: Hibernate instance', disabledReason: 'No hibernation in CloudForge labs' },
                {
                  label: 'Instance settings: Terminate instance',
                  onClick: () => selected && terminate.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canTerminate ? 'Already terminating' : undefined,
                  danger: true,
                },
                { label: 'Instance settings: Change shutdown behavior', disabledReason: 'No shutdown-behavior API in CloudForge' },
                { label: 'Instance settings: Edit status check alarm', disabledReason: 'No status-check alarms in CloudForge' },
                { label: 'Instance settings: Modify capacity reservation', disabledReason: 'No capacity-reservation API in CloudForge' },
                {
                  label: 'Monitoring',
                  onClick: () => setTab('monitoring'),
                  disabledReason: selected === null ? 'Select an instance first' : undefined,
                },
                { label: 'Image and templates', disabledReason: 'No AMI or template management in CloudForge' },
                {
                  label: 'Add/edit tags',
                  onClick: () => setTab('tags'),
                  disabledReason: selected === null ? 'Select an instance first' : undefined,
                },
                {
                  label: 'Delete',
                  onClick: () => selected && terminate.mutate(selected.id),
                  disabledReason: selected === null ? 'Select an instance first' : !canTerminate ? 'Already terminating' : undefined,
                  danger: true,
                },
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
                  { header: 'Name', render: (r) => r.name },
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
                  { header: 'Instance state', render: (r) => <StateBadge state={r.status} /> },
                  { header: 'Instance type', render: (r) => r.definition_id ?? '—' },
                  { header: 'Status check', render: () => '—' },
                  { header: 'Availability Zone', render: () => 'cf-local-1a' },
                  { header: 'Public IPv4 DNS', render: () => '—' },
                  { header: 'Public IPv4 address', render: () => '—' },
                  { header: 'Elastic IP', render: () => '—' },
                  { header: 'IPv6 IPs', render: () => '—' },
                  { header: 'Monitoring', render: () => '—' },
                  {
                    header: 'Security group name',
                    render: (r) =>
                      r.resources.filter((x) => x.kind === 'security-group').map((x) => x.name).join(', ') || '—',
                  },
                  { header: 'Key name', render: () => '—' },
                  { header: 'Launch time', render: (r) => (r.created_at ? new Date(r.created_at).toLocaleString() : '—') },
                  { header: 'Platform details', render: () => '—' },
                  { header: 'Managed', render: () => '—' },
                  { header: 'Operator', render: () => '—' },
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
                <div className="space-y-2 text-sm">
                  {(() => {
                    const attached = ebs.rows.filter((v) =>
                      volumeAttachments(v).some((a) => a.labId === selected.id),
                    )
                    return attached.length === 0 ? (
                      <p className="text-aws-muted">
                        No EBS volumes attached to this instance. Lab instances carry their own disks; volumes are
                        API-EMULATED teaching records and attaching one is recorded only — no disk is really
                        attached. Attach a volume on the{' '}
                        <Link to="/services/ec2?section=volumes" className="text-aws-link hover:underline">Volumes section</Link>.
                      </p>
                    ) : (
                      <ul className="space-y-1">
                        {attached.map((v) => (
                          <li key={v.id}>
                            <strong>{str(v, 'name')}</strong> ({v.id}) — {str(v, 'volume_type')} {num(v, 'size_gb')} GiB,{' '}
                            {str(v, 'status')} · attached as{' '}
                            {volumeAttachments(v).filter((a) => a.labId === selected.id).map((a) => a.device).join(', ')}
                          </li>
                        ))}
                      </ul>
                    )
                  })()}
                  <Link to="/services/ec2?section=volumes" className="inline-block text-aws-link hover:underline">
                    Manage volumes
                  </Link>
                </div>
              )}
              {tab === 'monitoring' && <InstanceMonitoring labId={selected.id} status={selected.status} />}
              {tab === 'status' && (
                <p className="text-sm text-aws-muted">
                  System status check: {selected.status === 'READY' || selected.status === 'RUNNING' ? 'passed — the lab provisioned successfully' : `not passed — instance is ${selected.status}`}.
                  Instance status check: {health.data?.network_engine_reachable ? 'engine reachable' : 'engine unreachable — provisioning would fail, never fake READY'}.
                </p>
              )}
              {tab === 'logs' && (
                <div className="space-y-2 text-sm">
                  <p className="text-aws-muted">
                    There is no per-instance log stream in CloudForge — no console-output API exists and no log
                    lines are recorded for a lab. Provisioning activity is in the audit events on the Events tab.
                  </p>
                  <AwsButton onClick={() => setTab('events')}>Show audit events</AwsButton>
                </div>
              )}
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

      {(activeSub === 'load-balancers' || activeSub === 'target-groups') && (
        <div className="space-y-4">
          <AwsFlash tone="info">
            Elastic Load Balancing is the EC2 console&apos;s Load Balancing section — it has no standalone console
            (2026-11-22 survey). The old /services/elb bookmark redirects here.
          </AwsFlash>
          <ElbListContent />
        </div>
      )}

      {activeSub === 'autoscaling' && <AutoScalingSection flash={flash} />}

      {activeSub === 'volumes' && <EbsVolumesSection flash={flash} />}

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

      {activeSub === 'launch-templates' && (
        <EmptySection title="Launch Templates" createLabel="Create launch template">
          <p className="text-sm text-aws-muted">
            Launch templates are <strong>UNSUPPORTED</strong> in CloudForge — they are not modelled and there is no
            template API. Launching is done directly from the Instances page, where the lab definition plays the
            template&apos;s role (image + instance type + network).
          </p>
        </EmptySection>
      )}

      {activeSub === 'spot-requests' && (
        <EmptySection title="Spot Requests" createLabel="Request Spot Instances">
          <p className="text-sm text-aws-muted">
            Spot Requests are <strong>UNSUPPORTED</strong> in CloudForge — there is no spot market, no spot pricing
            and no bidding API. Labs are fixed lab sessions launched on demand; nothing here can be interrupted or
            reclaimed the way Spot capacity can.
          </p>
        </EmptySection>
      )}

      {activeSub === 'reserved-instances' && (
        <EmptySection title="Reserved Instances" createLabel="Purchase Reserved Instances">
          <p className="text-sm text-aws-muted">
            Reserved Instances are <strong>UNSUPPORTED</strong> in CloudForge — there is no reservation or billing
            API and no usage billing to reserve against. Cost is reported only as teaching-rate lines on the Cost
            Explorer console.
          </p>
        </EmptySection>
      )}

      {activeSub === 'dedicated-hosts' && (
        <EmptySection title="Dedicated Hosts" createLabel="Allocate host">
          <p className="text-sm text-aws-muted">
            Dedicated Hosts are <strong>UNSUPPORTED</strong> in CloudForge — there is no host-allocation API. Labs
            share the local host (or the simulated engine); no host can be reserved for one tenant.
          </p>
        </EmptySection>
      )}

      {activeSub === 'capacity-reservations' && (
        <EmptySection title="Capacity Reservations" createLabel="Create Capacity Reservation">
          <p className="text-sm text-aws-muted">
            Capacity Reservations are <strong>UNSUPPORTED</strong> in CloudForge — there is no capacity-reservation
            API. Capacity is the local host and, in lite mode, the one-lab quota; it cannot be reserved ahead of
            time.
          </p>
        </EmptySection>
      )}

      {activeSub === 'ami-catalog' && (
        <EmptySection
          title="AMI Catalog"
          instead={[{ label: 'AMIs — the lab definitions CloudForge can launch', onSelect: () => setActiveSub('amis'), note: 'the only launchable images' }]}
        >
          <p className="text-sm text-aws-muted">
            The AMI Catalog is <strong>UNSUPPORTED</strong> in CloudForge — there is no public catalog of
            AWS-published images and no AWS image is ever queried. The only images that exist here are the lab
            definitions listed on the AMIs page.
          </p>
        </EmptySection>
      )}

      {(activeSub === 'snapshots' || activeSub === 'ebs-snapshots') && (
        <EmptySection title="Snapshots">
          <p className="text-sm text-aws-muted">
            Snapshots are <strong>UNSUPPORTED</strong> in CloudForge — there is no snapshot API and no snapshot can
            exist. Volumes exist on the Volumes section, but they cannot be snapshotted. The survey lists this list
            twice (EBS Snapshots under Images, Snapshots under Elastic Block Store); it is one list, shown here
            from either nav item.
          </p>
        </EmptySection>
      )}

      {activeSub === 'public-images' && (
        <EmptySection
          title="Public Images"
          instead={[{ label: 'AMIs — the lab definitions CloudForge can launch', onSelect: () => setActiveSub('amis'), note: 'no public or shared images exist' }]}
        >
          <p className="text-sm text-aws-muted">
            Public Images are <strong>UNSUPPORTED</strong> in CloudForge — there are no public or shared images.
            Only the lab definitions on the AMIs page can be launched.
          </p>
        </EmptySection>
      )}

      {activeSub === 'lifecycle' && (
        <EmptySection title="Lifecycle Manager">
          <p className="text-sm text-aws-muted">
            Lifecycle Manager is <strong>UNSUPPORTED</strong> in CloudForge — there is no lifecycle-policy API.
            There are no EBS snapshots for a policy to manage, so no policy can exist. Volumes are managed on the
            Volumes section.
          </p>
        </EmptySection>
      )}

      {activeSub === 'elastic-ips' && (
        <EmptySection
          title="Elastic IPs"
          instead={[{ label: 'VPC console', to: '/services/vpc', note: 'Elastic IPs are not modelled there either — see its disabled nav item' }]}
        >
          <p className="text-sm text-aws-muted">
            Elastic IPs are <strong>UNSUPPORTED</strong> in CloudForge — there is no Elastic IP operation: no
            allocate, associate, or release API, and labs get their addresses from the lab definition&apos;s own
            networking. No address can exist to list.
          </p>
        </EmptySection>
      )}

      {activeSub === 'placement-groups' && (
        <EmptySection title="Placement Groups" createLabel="Create placement group">
          <p className="text-sm text-aws-muted">
            Placement Groups are <strong>UNSUPPORTED</strong> in CloudForge — there is no placement-group API and
            no placement strategy (cluster / spread / partition) is modelled. The engine places a lab&apos;s VMs;
            that placement is not configurable.
          </p>
        </EmptySection>
      )}

      {activeSub === 'key-pairs' && (
        <EmptySection title="Key Pairs" createLabel="Create key pair">
          <p className="text-sm text-aws-muted">
            Key Pairs are <strong>UNSUPPORTED</strong> in CloudForge — there is no key-pair API and no SSH key
            injection into lab VMs. Connect to a lab through its workstation terminal instead of SSH.
          </p>
        </EmptySection>
      )}

      {activeSub === 'network-interfaces' && (
        <EmptySection
          title="Network Interfaces"
          instead={[{ label: 'Instances — open an instance, then its Networking tab', onSelect: () => setActiveSub('instances'), note: 'a lab\u2019s networking resources are listed there' }]}
        >
          <p className="text-sm text-aws-muted">
            Network Interfaces are <strong>UNSUPPORTED</strong> in CloudForge — there is no network-interface (ENI)
            API and no ENI resource is modelled. A lab&apos;s networking exists only as its VPC, subnets and
            gateways, listed on the instance&apos;s Networking tab.
          </p>
        </EmptySection>
      )}

      {activeSub === 'trust-stores' && (
        <EmptySection
          title="Trust Stores"
          instead={[{ label: 'Load Balancers', onSelect: () => setActiveSub('load-balancers'), note: 'SIMULATED round-robin with real health probes' }]}
        >
          <p className="text-sm text-aws-muted">
            Trust Stores are <strong>UNSUPPORTED</strong> in CloudForge — they are not modelled and there is no
            trust-store API. The load balancer&apos;s listeners are simulated in-process, so there is no TLS trust
            material to store.
          </p>
        </EmptySection>
      )}

      {activeSub === 'global-view' && (
        <EmptySection title="AWS Global View">
          <p className="text-sm text-aws-muted">
            AWS Global View is <strong>UNSUPPORTED</strong> in CloudForge — there is one local region only and no
            multi-region inventory to view. The Resource Explorer lists everything in the local region.
          </p>
        </EmptySection>
      )}

      {activeSub === 'events' && (
        <EmptySection
          title="Events"
          instead={[{ label: 'Instances — open an instance, then its Events tab', onSelect: () => setActiveSub('instances'), note: 'per-instance audit events are listed there' }]}
        >
          <p className="text-sm text-aws-muted">
            EC2 Events are <strong>UNSUPPORTED</strong> in CloudForge — scheduled EC2 events (maintenance,
            retirements) are not modelled. Per-instance audit events are on the instance&apos;s Events tab.
          </p>
        </EmptySection>
      )}

      {activeSub === 'ultraservers' && (
        <EmptySection title="UltraServers">
          <p className="text-sm text-aws-muted">
            UltraServers are <strong>UNSUPPORTED</strong> in CloudForge — there is no ultra-scale cluster hardware
            and no UltraServer API. Labs run on the local host (or the simulated engine).
          </p>
        </EmptySection>
      )}

      {activeSub === 'capacity-manager' && (
        <EmptySection title="Capacity Manager">
          <p className="text-sm text-aws-muted">
            Capacity Manager is <strong>UNSUPPORTED</strong> in CloudForge — there is no capacity-manager API.
            Capacity is the local host and, in lite mode, the one-lab quota; it cannot be managed ahead of time.
          </p>
        </EmptySection>
      )}
    </ServiceConsole>
  )
}

/**
 * Honest empty state for an EC2 nav page CloudForge does not build.
 * The 2026-11-22 survey records these pages' names in the EC2 left nav
 * but no list columns for any of them, so no table is rendered — a
 * table with invented columns would be a fake list. The Create button,
 * where AWS has one, is disabled with the reason.
 */
function EmptySection({
  title,
  createLabel,
  instead,
  children,
}: {
  title: string
  createLabel?: string
  instead?: { label: string; to?: string; onSelect?: () => void; note?: string }[]
  children: ReactNode
}) {
  return (
    <div className="space-y-4">
      <AwsCard title={`${title} (0)`}>
        {children}
        <p className="mt-2 text-xs text-aws-muted">
          The 2026-11-22 survey records this page&apos;s name in the EC2 left nav but no list columns, so no table
          is shown here — an empty table with invented columns would be a fake list.
        </p>
        {createLabel !== undefined && (
          <div className="mt-3">
            <AwsButton disabled title={`${title} is UNSUPPORTED in CloudForge — no API.`}>
              {createLabel}
            </AwsButton>
          </div>
        )}
      </AwsCard>
      {instead !== undefined && instead.length > 0 && (
        <AwsCard title="What you can do instead">
          <ul className="space-y-1 text-sm">
            {instead.map((l) => (
              <li key={l.label}>
                {l.to !== undefined ? (
                  <Link to={l.to} className="text-aws-link hover:underline">
                    {l.label}
                  </Link>
                ) : (
                  <button type="button" onClick={l.onSelect} className="text-aws-link hover:underline">
                    {l.label}
                  </button>
                )}
                {l.note !== undefined && <span className="text-aws-muted"> — {l.note}</span>}
              </li>
            ))}
          </ul>
        </AwsCard>
      )}
    </div>
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

/**
 * EBS Volumes — wired to the real API-EMULATED backend
 * (POST/GET/DELETE /api/v1/aws/ebs/volumes, POST .../attach|detach).
 * The table keeps the survey's exact 23 Volumes columns; columns the
 * backend has no value for render '—' instead of an invented value.
 * Attach is recorded only: the lab's disk is unchanged and no device
 * is attached to any VM. In-use volumes cannot be deleted (409).
 */
function EbsVolumesSection({ flash }: { flash: (message: string, tone?: 'success' | 'info' | 'error') => void }) {
  const volumes = useEbsVolumes()
  const create = useExamCreate('ebs/volumes')
  const act = useExamAction('ebs/volumes')
  const remove = useExamDelete('ebs/volumes')
  const labs = useQuery({ queryKey: ['labs'], queryFn: fetchLabs, retry: 1 })
  const [filter, setFilter] = useState('')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [sizeGb, setSizeGb] = useState(10)
  const [volumeType, setVolumeType] = useState('gp3')
  const [attachOpen, setAttachOpen] = useState(false)
  const [attachLabId, setAttachLabId] = useState('')
  const selected = volumes.rows.find((r) => r.id === checkedIds[0]) ?? null
  const rows = volumes.rows.filter(
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
        EBS is the EC2 console&apos;s Elastic Block Store section — it has no standalone console (2026-11-22
        survey). The old /services/ebs bookmark redirects here.
      </AwsFlash>
      <AwsCard title={`Volumes (${volumes.rows.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          EBS volumes are <strong>API-EMULATED</strong> in CloudForge — durable records with create, attach,
          detach and delete. Attach is recorded only: the lab&apos;s disk is unchanged and no device is attached to
          any VM. Lab instances carry their own disks. Snapshots and lifecycle policies have no backend and stay
          UNSUPPORTED (see the Snapshots and Lifecycle Manager sections).
        </p>
        {volumes.isError && <ErrorBox message={`Volumes could not be loaded (${volumes.errorMessage}).`} />}
        <AwsToolbar
          right={
            <>
              <AwsButton disabled title="Deleted volumes are not kept — delete is immediate, there is no recycle bin.">
                Recycle Bin
              </AwsButton>
              <AwsRefreshButton onClick={volumes.refresh} busy={volumes.isFetching} />
              <AwsPreferencesButton />
            </>
          }
        >
          <AwsActionsMenu
            items={[
              {
                label: 'Attach to lab…',
                disabledReason:
                  selected === null
                    ? 'Select a volume first.'
                    : str(selected, 'status') !== 'available'
                      ? `Only an available volume can be attached (current: ${str(selected, 'status')}).`
                      : undefined,
                onClick: () => setAttachOpen(true),
              },
              {
                label: 'Detach',
                disabledReason:
                  selected === null
                    ? 'Select a volume first.'
                    : str(selected, 'status') !== 'in-use'
                      ? `Only an in-use volume can be detached (current: ${str(selected, 'status')}).`
                      : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  act.mutate(
                    { id: sel.id, action: 'detach' },
                    {
                      onSuccess: () => flash(`Volume ${sel.id} detached.`, 'success'),
                      onError: (e: Error) => flash(`Detach failed: ${e.message}`, 'error'),
                    },
                  )
                },
              },
              {
                label: 'Delete',
                disabledReason: selected === null ? 'Select a volume first.' : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  remove.mutate(sel.id, {
                    onSuccess: () => {
                      flash(`Volume ${sel.id} deleted.`, 'success')
                      setCheckedIds([])
                    },
                    onError: (e: Error) => flash(`Delete failed: ${e.message}`, 'error'),
                  })
                },
              },
            ]}
          />
          <AwsButton
            variant="primary"
            onClick={() => setShowCreate((v) => !v)}
          >
            Create volume
          </AwsButton>
        </AwsToolbar>
        {showCreate && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Create volume</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. data-vol" className={`w-full max-w-md ${inputCls}`} />
            </label>
            <div className="flex flex-wrap gap-3">
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Size (GiB)</span>
                <input
                  type="number"
                  min={1}
                  max={16384}
                  value={sizeGb}
                  onChange={(e) => setSizeGb(Number(e.target.value))}
                  className={`w-32 ${inputCls}`}
                />
              </label>
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Type</span>
                <select value={volumeType} onChange={(e) => setVolumeType(e.target.value)} className={inputCls}>
                  {['gp2', 'gp3', 'io1', 'io2', 'st1', 'sc1'].map((t) => (
                    <option key={t} value={t}>{t}</option>
                  ))}
                </select>
              </label>
            </div>
            <p className="text-xs text-aws-muted">
              Size must be 1–16384 GiB. Type defaults to gp3 and volumes are encrypted by default; those defaults
              come from the backend.
            </p>
            <div>
              <AwsButton
                variant="primary"
                disabled={name.trim() === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { name: name.trim(), size_gb: sizeGb, volume_type: volumeType },
                    {
                      onSuccess: (v) => {
                        flash(`Volume ${v.id} created.`, 'success')
                        setName('')
                        setShowCreate(false)
                      },
                      onError: (e: Error) => flash(`Create failed: ${e.message}`, 'error'),
                    },
                  )
                }
              >
                Create volume
              </AwsButton>
            </div>
          </div>
        )}
        {attachOpen && selected !== null && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Attach {str(selected, 'name')} ({selected.id})</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Lab (instance)</span>
              <select
                value={attachLabId}
                onChange={(e) => setAttachLabId(e.target.value)}
                className={`w-full max-w-md ${inputCls}`}
              >
                <option value="">Select a lab…</option>
                {(labs.data ?? []).map((l) => (
                  <option key={l.id} value={l.id}>{l.name} ({l.id}) — {l.status}</option>
                ))}
              </select>
            </label>
            <p className="text-xs text-aws-muted">
              The lab must exist — the backend returns 404 otherwise. The attachment is recorded as /dev/sdf;
              nothing is really attached.
            </p>
            <div className="flex gap-2">
              <AwsButton
                variant="primary"
                disabled={attachLabId === '' || act.isPending}
                onClick={() =>
                  act.mutate(
                    { id: selected.id, action: 'attach', body: { lab_id: attachLabId } },
                    {
                      onSuccess: () => {
                        flash(`Volume ${selected.id} attached to lab ${attachLabId} (recorded only).`, 'success')
                        setAttachOpen(false)
                        setAttachLabId('')
                      },
                      onError: (e: Error) => flash(`Attach failed: ${e.message}`, 'error'),
                    },
                  )
                }
              >
                Attach
              </AwsButton>
              <AwsButton onClick={() => setAttachOpen(false)}>Cancel</AwsButton>
            </div>
          </div>
        )}
        <AwsTable<Rec>
          caption="EBS volumes"
          emptyMessage="No volumes yet — create one to practise attach and detach."
          rows={rows}
          totalCount={volumes.rows.length}
          filter={filter}
          onFilterChange={setFilter}
          filterPlaceholder="Filter volumes by name or ID"
          selectable
          selectedIds={checkedIds}
          onToggleRow={(id) => setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))}
          onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Volume ID', render: (r) => <span className="font-mono">{r.id}</span> },
            { header: 'Type', render: (r) => str(r, 'volume_type') },
            { header: 'Size', render: (r) => `${num(r, 'size_gb')} GiB` },
            { header: 'IOPS', render: () => '—' },
            { header: 'Throughput', render: () => '—' },
            { header: 'Owner ID', render: () => '—' },
            { header: 'Snapshot ID', render: () => '—' },
            { header: 'Source volume ID', render: () => '—' },
            { header: 'Created', render: (r) => fmtTime(r.created_at) },
            { header: 'Availability Zone', render: () => '—' },
            { header: 'Volume state', render: (r) => <StateBadge state={str(r, 'status')} /> },
            { header: 'Alarm status', render: () => '—' },
            {
              header: 'Attached resources',
              render: (r) => {
                const atts = volumeAttachments(r)
                return atts.length === 0 ? '—' : atts.map((a) => `${a.labId} (${a.device})`).join(', ')
              },
            },
            { header: 'Status check', render: () => '—' },
            { header: 'Initialization state', render: () => '—' },
            { header: 'Encryption', render: (r) => (r.encrypted === true ? 'Encrypted' : 'Not encrypted') },
            { header: 'KMS key ID', render: () => '—' },
            { header: 'KMS key alias', render: () => '—' },
            { header: 'Fast snapshot restored', render: () => '—' },
            { header: 'Multi-Attach enabled', render: () => '—' },
            { header: 'Managed', render: () => '—' },
            { header: 'Operator', render: () => '—' },
          ]}
        />
        {selected !== null && (
          <dl className="mt-3 space-y-1 text-sm">
            <DetailRow k="Volume ID" v={selected.id} />
            <DetailRow k="Created" v={fmtTime(selected.created_at)} />
            <DetailRow k="Updated" v={fmtTime(selected.updated_at)} />
            <DetailRow
              k="Attachments"
              v={
                volumeAttachments(selected).length === 0
                  ? 'none — attach is recorded only, the lab disk is unchanged'
                  : volumeAttachments(selected).map((a) => `${a.labId} as ${a.device} at ${a.attachedAt}`).join('; ')
              }
            />
          </dl>
        )}
      </AwsCard>
    </div>
  )
}

/**
 * Auto Scaling groups — wired to the real API-EMULATED backend
 * (POST/GET/DELETE /api/v1/aws/autoscaling/groups, POST .../scale).
 * The survey's ASG page is a marketing empty state with no list
 * columns, so the table uses the record's own fields rather than
 * invented AWS columns. Scaling updates the recorded desired/current
 * capacity only — no instance is launched or terminated.
 */
function AutoScalingSection({ flash }: { flash: (message: string, tone?: 'success' | 'info' | 'error') => void }) {
  const groups = useAutoScalingGroups()
  const create = useExamCreate('autoscaling/groups')
  const act = useExamAction('autoscaling/groups')
  const remove = useExamDelete('autoscaling/groups')
  const [filter, setFilter] = useState('')
  const [checkedIds, setCheckedIds] = useState<string[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [minSize, setMinSize] = useState(1)
  const [maxSize, setMaxSize] = useState(2)
  const [desired, setDesired] = useState(1)
  const [scaleDesired, setScaleDesired] = useState(1)
  const selected = groups.rows.find((r) => r.id === checkedIds[0]) ?? null
  const rows = groups.rows.filter(
    (r) =>
      filter.trim() === '' ||
      str(r, 'name').toLowerCase().includes(filter.toLowerCase()) ||
      r.id.toLowerCase().includes(filter.toLowerCase()),
  )
  const inputCls =
    'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'
  return (
    <div className="space-y-4">
      <AwsCard title="Auto Scaling Groups">
        <p className="mb-3 text-sm text-aws-muted">
          Auto Scaling is <strong>API-EMULATED</strong> in CloudForge — groups are durable teaching records with
          min/desired/max capacity and a scale action. Scaling updates the recorded capacity only; no instance is
          launched or terminated. Scale real capacity by launching or stopping instances on the Instances page.
        </p>
        {groups.isError && <ErrorBox message={`Auto Scaling groups could not be loaded (${groups.errorMessage}).`} />}
        <AwsToolbar
          right={
            <>
              <AwsRefreshButton onClick={groups.refresh} busy={groups.isFetching} />
              <AwsPreferencesButton />
            </>
          }
        >
          <AwsActionsMenu
            items={[
              {
                label: 'Delete',
                disabledReason: selected === null ? 'Select a group first.' : undefined,
                onClick: () => {
                  const sel = selected
                  if (sel === null) return
                  remove.mutate(sel.id, {
                    onSuccess: () => {
                      flash(`Auto Scaling group ${sel.id} deleted.`, 'success')
                      setCheckedIds([])
                    },
                    onError: (e: Error) => flash(`Delete failed: ${e.message}`, 'error'),
                  })
                },
              },
            ]}
          />
          <AwsButton
            variant="primary"
            onClick={() => setShowCreate((v) => !v)}
          >
            Create Auto Scaling group
          </AwsButton>
        </AwsToolbar>
        {showCreate && (
          <div className="mb-3 space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <h3 className="text-sm font-semibold">Create Auto Scaling group</h3>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. web-asg" className={`w-full max-w-md ${inputCls}`} />
            </label>
            <div className="flex flex-wrap gap-3">
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Min size</span>
                <input type="number" min={0} max={10} value={minSize} onChange={(e) => setMinSize(Number(e.target.value))} className={`w-24 ${inputCls}`} />
              </label>
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Max size</span>
                <input type="number" min={0} max={10} value={maxSize} onChange={(e) => setMaxSize(Number(e.target.value))} className={`w-24 ${inputCls}`} />
              </label>
              <label className="block text-sm">
                <span className="mb-1 block font-medium">Desired capacity</span>
                <input type="number" min={0} max={10} value={desired} onChange={(e) => setDesired(Number(e.target.value))} className={`w-24 ${inputCls}`} />
              </label>
            </div>
            <p className="text-xs text-aws-muted">
              Max size is capped at 10 instances (teaching cap) and desired capacity must be between min and max;
              the backend returns 400 otherwise. Launch profile defaults to Micro, health check type to EC2.
            </p>
            <div>
              <AwsButton
                variant="primary"
                disabled={name.trim() === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { name: name.trim(), min_size: minSize, max_size: maxSize, desired_capacity: desired },
                    {
                      onSuccess: (g) => {
                        flash(`Auto Scaling group ${g.id} created.`, 'success')
                        setName('')
                        setShowCreate(false)
                      },
                      onError: (e: Error) => flash(`Create failed: ${e.message}`, 'error'),
                    },
                  )
                }
              >
                Create Auto Scaling group
              </AwsButton>
            </div>
          </div>
        )}
        {selected !== null && (
          <div className="mb-3 flex flex-wrap items-end gap-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Scale {str(selected, 'name')} to desired capacity</span>
              <input
                type="number"
                min={num(selected, 'min_size')}
                max={num(selected, 'max_size')}
                value={scaleDesired}
                onChange={(e) => setScaleDesired(Number(e.target.value))}
                className={`w-24 ${inputCls}`}
              />
            </label>
            <AwsButton
              disabled={act.isPending}
              onClick={() =>
                act.mutate(
                  { id: selected.id, action: 'scale', body: { desired_capacity: scaleDesired } },
                  {
                    onSuccess: () => flash(`Group ${selected.id} scaled to desired capacity ${scaleDesired} (recorded only).`, 'success'),
                    onError: (e: Error) => flash(`Scale failed: ${e.message}`, 'error'),
                  },
                )
              }
            >
              Scale
            </AwsButton>
            <p className="text-xs text-aws-muted">
              Desired must be between min ({num(selected, 'min_size')}) and max ({num(selected, 'max_size')}).
            </p>
          </div>
        )}
        <AwsTable<Rec>
          caption="Auto Scaling groups"
          emptyMessage="No Auto Scaling groups yet — create one to practise desired capacity."
          rows={rows}
          totalCount={groups.rows.length}
          filter={filter}
          onFilterChange={setFilter}
          filterPlaceholder="Filter groups by name or ID"
          selectable
          selectedIds={checkedIds}
          onToggleRow={(id) => setCheckedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))}
          onToggleAll={(checked) => setCheckedIds(checked ? rows.map((r) => r.id) : [])}
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Group ID', render: (r) => <span className="font-mono">{r.id}</span> },
            { header: 'Min size', render: (r) => String(num(r, 'min_size')) },
            { header: 'Max size', render: (r) => String(num(r, 'max_size')) },
            { header: 'Desired capacity', render: (r) => String(num(r, 'desired_capacity')) },
            { header: 'Current capacity', render: (r) => String(num(r, 'current_capacity')) },
            { header: 'Launch profile', render: (r) => str(r, 'launch_profile') },
            { header: 'Health check type', render: (r) => str(r, 'health_check_type') },
            { header: 'Status', render: (r) => <StateBadge state={str(r, 'status')} /> },
            { header: 'Created', render: (r) => fmtTime(r.created_at) },
          ]}
        />
      </AwsCard>
      <AwsCard title="How it works">
        <p className="text-sm text-aws-muted">
          In AWS, an Auto Scaling group keeps a desired number of instances running, replacing unhealthy ones
          and scaling on demand or schedule. In CloudForge, a group records that intent — min, desired and max —
          and scaling changes the recorded numbers. The fixed-count lab is the practice surface for real
          capacity: launch or stop instances on the Instances page.
        </p>
      </AwsCard>
      <AwsCard title="Related services">
        <ul className="space-y-1 text-sm">
          <li>
            <Link to="/services/ec2" className="text-aws-link hover:underline">EC2 — Instances</Link>
            <span className="text-aws-muted"> — launch or stop instances to change capacity.</span>
          </li>
          <li>
            <Link to="/services/ec2?section=load-balancers" className="text-aws-link hover:underline">EC2 — Load Balancers</Link>
            <span className="text-aws-muted"> — distribute traffic across the instances you launched.</span>
          </li>
          <li>
            <Link to="/learn" className="text-aws-link hover:underline">Learning Center — SAA track</Link>
            <span className="text-aws-muted"> — Auto Scaling lessons.</span>
          </li>
        </ul>
      </AwsCard>
    </div>
  )
}
