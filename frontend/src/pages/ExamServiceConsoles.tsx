/**
 * Dedicated consoles for the 17 generic-console services (audit gap
 * #3). Each export is one service's console, built on the shared
 * ExamServiceConsole builder in ExamServiceConsole.tsx and wired to
 * the real API-emulated backend (migration 014, /api/v1/aws/…).
 *
 * Survey discipline (2026-11-22): Backup nav (~20 items), CloudFront
 * nav (10 items) + Distributions columns, and EFS nav (File systems,
 * Access points) are the survey's recorded layout, used verbatim.
 * Every other service's nav and columns are CloudForge's own
 * structure — the survey records no tabs or columns for them — and
 * each console says so. No AWS tabs/columns are invented; no tab
 * without a backend pretends to work (UNSUPPORTED, button disabled,
 * reason shown). CloudForge branding only.
 *
 * Route registration (coordinator wires App.tsx — this file only
 * exports the components):
 *   /services/aurora            -> AuroraConsole
 *   /services/backup            -> BackupConsole
 *   /services/macie             -> MacieConsole
 *   /services/auditmanager      -> AuditManagerConsole
 *   /services/artifact          -> ArtifactConsole
 *   /services/cloudfront        -> CloudFrontConsole
 *   /services/efs               -> EfsConsole
 *   /services/fsx               -> FsxConsole
 *   /services/shield            -> ShieldConsole
 *   /services/controltower      -> ControlTowerConsole
 *   /services/identitycenter    -> IdentityCenterConsole
 *   /services/ram               -> RamConsole
 *   /services/directconnect     -> DirectConnectConsole
 *   /services/globalaccelerator -> GlobalAcceleratorConsole
 *   /services/mgn               -> MgnConsole
 *   /services/computeoptimizer  -> ComputeOptimizerConsole
 *   /services/savingsplans      -> SavingsPlansConsole
 */
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'
import { ExamServiceConsole } from './ExamServiceConsole.tsx'
import {
  fmtTime,
  fmtUsd,
  inputCls,
  listOf,
  str,
  useExamList,
  type ExamCtx,
  type ExamDetailCtx,
  type ExamServiceConfig,
  type Rec,
} from '../services/examConsole.ts'

const ownColumnsNote = (service: string) =>
  `List columns are CloudForge's own — the 2026-11-22 survey records no ${service} list columns, so nothing here claims to match AWS.`

function singleCard(listLabel: string, listSection: string) {
  return (rows: Rec[], goTo: (section: string) => void) => [
    { label: listLabel, count: rows.length, onSelect: () => goTo(listSection) },
  ]
}

function SmallForm({
  label,
  value,
  onChange,
  placeholder,
  children,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  children?: ReactNode
}) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block font-medium">{label}</span>
      <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className={`w-full max-w-md ${inputCls}`} />
      {children}
    </label>
  )
}

/* --- Detail-tab panels (stateful; rendered by the config below) --- */

function AuroraInstancesPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const instances = Array.isArray(sel.instances) ? (sel.instances as Record<string, unknown>[]) : []
  const writer = instances.find((i) => i.role === 'writer')
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string; role?: string; status?: string }>
        caption="Cluster instances"
        emptyMessage="This cluster has no instances recorded."
        rows={instances.map((i, ix) => ({ id: String(i.id ?? `instance-${ix}`), role: String(i.role ?? ''), status: String(i.status ?? '') }))}
        totalCount={instances.length}
        columns={[
          { header: 'Instance', render: (r) => r.id },
          { header: 'Role', render: (r) => r.role ?? '—' },
          { header: 'Status', render: (r) => r.status ?? '—' },
        ]}
      />
      <AwsButton
        onClick={() => runAction(`aurora/clusters/${sel.id}/failover`)}
        disabled={actionBusy || instances.length < 2}
        title={instances.length < 2 ? 'Failover needs at least 2 instances' : undefined}
      >
        Failover to {writer !== undefined ? 'a reader' : 'reader'}
      </AwsButton>
      <p className="text-xs text-aws-muted">
        Failover swaps the recorded writer/reader roles only — no connections move and no data is replicated, because
        no Aurora storage engine runs here. Use RDS for a real Postgres database.
      </p>
    </div>
  )
}

function AuroraEndpointsPanel({ sel }: { sel: Rec }) {
  return (
    <div className="space-y-2 text-sm">
      <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[220px_1fr]">
        <dt className="font-medium">Writer endpoint</dt>
        <dd className="font-mono text-xs break-all">{str(sel, 'writer_endpoint')}</dd>
        <dt className="font-medium">Reader endpoint</dt>
        <dd className="font-mono text-xs break-all">{str(sel, 'reader_endpoint')}</dd>
      </dl>
      <p className="text-aws-muted">
        Both endpoints are teaching names ({str(sel, 'writer_endpoint', '—').split('.')[0] || 'cluster'}
        .cluster-cf-local-1.cf-local). Nothing resolves or listens on them — no Aurora engine runs here.
      </p>
    </div>
  )
}

function MountTargetsPanel({
  sel,
  basePath,
  runAction,
  actionBusy,
}: ExamDetailCtx & { basePath: string }) {
  const [subnet, setSubnet] = useState('')
  const targets = Array.isArray(sel.mount_targets) ? (sel.mount_targets as Record<string, unknown>[]) : []
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string; subnet_id?: string; status?: string; created_at?: string }>
        caption="Mount targets"
        emptyMessage="No mount targets recorded for this file system."
        rows={targets.map((t, i) => ({
          id: String(t.id ?? `mt-${i}`),
          subnet_id: String(t.subnet_id ?? ''),
          status: String(t.status ?? ''),
          created_at: String(t.created_at ?? ''),
        }))}
        totalCount={targets.length}
        columns={[
          { header: 'Mount target', render: (r) => r.id },
          { header: 'Subnet', render: (r) => r.subnet_id ?? '—' },
          { header: 'Status', render: (r) => r.status ?? '—' },
          { header: 'Created', render: (r) => fmtTime(r.created_at) },
        ]}
      />
      <div className="flex flex-wrap items-end gap-2">
        <SmallForm label="Subnet ID" value={subnet} onChange={setSubnet} placeholder="subnet-cf-local-1a" />
        <AwsButton
          variant="primary"
          onClick={() => {
            if (subnet.trim() === '') return
            runAction(`${basePath}/${sel.id}/mount-targets`, { subnet_id: subnet.trim() })
            setSubnet('')
          }}
          disabled={actionBusy || subnet.trim() === ''}
        >
          Add mount target
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Mount targets are recorded only: no NFS endpoint exists, no DNS name is registered, and nothing can be
        mounted. Subnet names are teaching labels.
      </p>
    </div>
  )
}

function GuardrailsPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState(true)
  const guardrails = Array.isArray(sel.guardrails) ? (sel.guardrails as Record<string, unknown>[]) : []
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string; guardrail?: string; enabled?: boolean; updated_at?: string }>
        caption="Guardrails"
        emptyMessage="No guardrails recorded for this landing zone."
        rows={guardrails.map((g, i) => ({
          id: String(g.guardrail ?? `guardrail-${i}`),
          guardrail: String(g.guardrail ?? ''),
          enabled: g.enabled === true,
          updated_at: String(g.updated_at ?? ''),
        }))}
        totalCount={guardrails.length}
        columns={[
          { header: 'Guardrail', render: (r) => r.guardrail ?? '—' },
          { header: 'Enabled', render: (r) => (r.enabled === true ? 'Yes' : 'No') },
          { header: 'Updated', render: (r) => fmtTime(r.updated_at) },
          {
            header: 'Actions',
            render: (r) => (
              <AwsButton
                onClick={() => runAction(`controltower/landing-zones/${sel.id}/guardrails`, { guardrail: r.guardrail, enabled: !(r.enabled === true) })}
                disabled={actionBusy}
              >
                {r.enabled === true ? 'Disable' : 'Enable'}
              </AwsButton>
            ),
          },
        ]}
      />
      <div className="flex flex-wrap items-end gap-2">
        <SmallForm label="Guardrail name" value={name} onChange={setName} placeholder="e.g. disallow-public-s3" />
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          Enabled
        </label>
        <AwsButton
          variant="primary"
          onClick={() => {
            if (name.trim() === '') return
            runAction(`controltower/landing-zones/${sel.id}/guardrails`, { guardrail: name.trim(), enabled })
            setName('')
          }}
          disabled={actionBusy || name.trim() === ''}
        >
          Set guardrail
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Guardrails are recorded settings only: enabling one installs no SCP or Config rule anywhere. Enforcement is
        modelled in the Organizations / SCP lessons.
      </p>
    </div>
  )
}

function AssignmentsPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const [principalType, setPrincipalType] = useState('user')
  const [principal, setPrincipal] = useState('')
  const [account, setAccount] = useState('')
  const assignments = Array.isArray(sel.assignments) ? (sel.assignments as Record<string, unknown>[]) : []
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string; principal_type?: string; principal?: string; account?: string; assigned_at?: string }>
        caption="Assignments"
        emptyMessage="No assignments recorded for this permission set."
        rows={assignments.map((a, i) => ({
          id: `assignment-${i}`,
          principal_type: String(a.principal_type ?? ''),
          principal: String(a.principal ?? ''),
          account: String(a.account ?? ''),
          assigned_at: String(a.assigned_at ?? ''),
        }))}
        totalCount={assignments.length}
        columns={[
          { header: 'Principal type', render: (r) => r.principal_type ?? '—' },
          { header: 'Principal', render: (r) => r.principal ?? '—' },
          { header: 'Account', render: (r) => r.account ?? '—' },
          { header: 'Assigned', render: (r) => fmtTime(r.assigned_at) },
        ]}
      />
      <div className="flex flex-wrap items-end gap-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Principal type</span>
          <select value={principalType} onChange={(e) => setPrincipalType(e.target.value)} className={inputCls}>
            <option value="user">user</option>
            <option value="group">group</option>
          </select>
        </label>
        <SmallForm label="Principal" value={principal} onChange={setPrincipal} placeholder="alice" />
        <SmallForm label="Account" value={account} onChange={setAccount} placeholder="cf-local-account-1" />
        <AwsButton
          variant="primary"
          onClick={() => {
            if (principal.trim() === '' || account.trim() === '') return
            runAction(`identitycenter/permission-sets/${sel.id}/assignments`, {
              principal_type: principalType,
              principal: principal.trim(),
              account: account.trim(),
            })
            setPrincipal('')
            setAccount('')
          }}
          disabled={actionBusy || principal.trim() === '' || account.trim() === ''}
        >
          Assign
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Assignments are recorded only: they create no login or role in any account, and no IdP is federated here.
      </p>
    </div>
  )
}

function PrincipalsPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const [principal, setPrincipal] = useState('')
  const principals = Array.isArray(sel.principals) ? (sel.principals as unknown[]) : []
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string }>
        caption="Associated principals"
        emptyMessage="No principals associated with this resource share."
        rows={principals.map((p, i) => ({ id: String(p ?? `principal-${i}`) }))}
        totalCount={principals.length}
        columns={[
          { header: 'Principal', render: (r) => r.id },
          {
            header: 'Actions',
            render: (r) => (
              <AwsButton onClick={() => runAction(`ram/resource-shares/${sel.id}/principals`, { principal: r.id, action: 'remove' })} disabled={actionBusy}>
                Remove
              </AwsButton>
            ),
          },
        ]}
      />
      <div className="flex flex-wrap items-end gap-2">
        <SmallForm label="Principal" value={principal} onChange={setPrincipal} placeholder="account name" />
        <AwsButton
          variant="primary"
          onClick={() => {
            if (principal.trim() === '') return
            runAction(`ram/resource-shares/${sel.id}/principals`, { principal: principal.trim(), action: 'add' })
            setPrincipal('')
          }}
          disabled={actionBusy || principal.trim() === ''}
        >
          Add principal
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Association is recorded only: it grants no access, because no second account exists. Principals are recorded
        names, not validated against any account.
      </p>
    </div>
  )
}

function FailoverPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const [region, setRegion] = useState('')
  const groups = Array.isArray(sel.endpoint_groups) ? (sel.endpoint_groups as Record<string, unknown>[]) : []
  const staticIps = Array.isArray(sel.static_ips) ? (sel.static_ips as unknown[]).map(String) : []
  return (
    <div className="space-y-3">
      <p className="text-sm">
        Static IPs: <code>{staticIps.join(', ') || '—'}</code>{' '}
        <span className="text-aws-muted">(teaching addresses in 192.0.2.0/24 — TEST-NET-1, routed nowhere)</span>
      </p>
      <AwsTable<{ id: string; region?: string; endpoints?: unknown; active?: boolean }>
        caption="Endpoint groups"
        emptyMessage="No endpoint groups recorded for this accelerator."
        rows={groups.map((g, i) => ({
          id: String(g.region ?? `group-${i}`),
          region: String(g.region ?? ''),
          endpoints: g.endpoints,
          active: sel.active_region === g.region,
        }))}
        totalCount={groups.length}
        columns={[
          { header: 'Region', render: (r) => r.region ?? '—' },
          {
            header: 'Endpoints',
            render: (r) => (Array.isArray(r.endpoints) ? (r.endpoints as unknown[]).join(', ') : '—'),
          },
          { header: 'Active', render: (r) => (r.active === true ? 'Yes — traffic would go here' : 'No') },
        ]}
      />
      <div className="flex flex-wrap items-end gap-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Fail over to region</span>
          <select value={region} onChange={(e) => setRegion(e.target.value)} className={inputCls}>
            <option value="">— select —</option>
            {groups.map((g) => (
              <option key={String(g.region)} value={String(g.region)}>
                {String(g.region)}
              </option>
            ))}
          </select>
        </label>
        <AwsButton
          variant="primary"
          onClick={() => region !== '' && runAction(`globalaccelerator/accelerators/${sel.id}/failover`, { region })}
          disabled={actionBusy || region === '' || region === String(sel.active_region ?? '')}
          title={region === String(sel.active_region ?? '') ? 'That region is already active' : undefined}
        >
          Fail over
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Failover switches the recorded active endpoint group only — no traffic moves, because no anycast addresses
        are routed anywhere. Failover history is kept on the record (Details tab).
      </p>
    </div>
  )
}

function MgnReplicationPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const state = String(sel.replication_state ?? sel.status ?? '')
  const step = (label: string, path: string, from: string) => (
    <AwsButton
      variant="primary"
      onClick={() => runAction(`mgn/source-servers/${sel.id}/${path}`)}
      disabled={actionBusy || state !== from}
      title={state !== from ? `Needs state ${from} (current: ${state})` : undefined}
    >
      {label}
    </AwsButton>
  )
  return (
    <div className="space-y-3">
      <p className="text-sm">
        Replication state: <strong>{state}</strong>
      </p>
      <ol className="list-decimal space-y-1 pl-5 text-sm text-aws-muted">
        <li>NOT_STARTED — source server registered, nothing replicating.</li>
        <li>REPLICATING — start replication.</li>
        <li>TEST_LAUNCHED — test launch on the replicated server.</li>
        <li>CUTOVER — cut over to the migrated server.</li>
      </ol>
      <div className="flex flex-wrap gap-2">
        {step('Start replication', 'start-replication', 'NOT_STARTED')}
        {step('Test launch', 'test-launch', 'REPLICATING')}
        {step('Cutover', 'cutover', 'TEST_LAUNCHED')}
      </div>
      <p className="text-xs text-aws-muted">
        Teaching state machine (SIMULATED): no agent is installed and no blocks are copied — the lifecycle exists so
        the cutover lesson can be practised end to end.
      </p>
    </div>
  )
}

function UtilizationPanel({ sel }: { sel: Rec }) {
  const q = useQuery({
    queryKey: ['sp-util', sel.id],
    queryFn: () => request<Rec>(`/api/v1/aws/savingsplans/plans/${encodeURIComponent(sel.id)}/utilization`),
    retry: 1,
  })
  if (q.isPending) return <Loading label="Loading utilization" />
  if (q.isError || q.data === undefined) return <ErrorBox message={`Utilization could not be loaded (${q.error?.message ?? 'unknown error'}).`} />
  const d = q.data
  return (
    <div className="space-y-3 text-sm">
      <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[260px_1fr]">
        <dt className="font-medium">Hourly commitment</dt>
        <dd>{fmtUsd(d.hourly_commitment_usd)}/hr</dd>
        <dt className="font-medium">Current hourly lab cost</dt>
        <dd>{fmtUsd(d.current_hourly_lab_cost_usd)}/hr</dd>
        <dt className="font-medium">Covered by this plan</dt>
        <dd>{fmtUsd(d.covered_hourly_usd)}/hr</dd>
        <dt className="font-medium">Utilization</dt>
        <dd>{typeof d.utilization_percent === 'number' ? `${(d.utilization_percent as number).toFixed(1)}%` : '—'}</dd>
        <dt className="font-medium">Monthly commitment</dt>
        <dd>{fmtUsd(d.monthly_commitment_usd)}</dd>
        <dt className="font-medium">Monthly covered</dt>
        <dd>{fmtUsd(d.monthly_covered_usd)}</dd>
      </dl>
      <p className="text-aws-muted">{typeof d.basis === 'string' ? d.basis : ''}</p>
      <p className="text-xs text-aws-muted">
        Plans are records, not real AWS commitments: no money is committed and no discount is applied.
      </p>
    </div>
  )
}

function EvidencePanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const evidence = Array.isArray(sel.evidence) ? (sel.evidence as Record<string, unknown>[]) : []
  return (
    <div className="space-y-3">
      <AwsTable<{ id: string; collected_at?: string; entries_count?: number; note?: string }>
        caption="Evidence collected"
        emptyMessage="No evidence collected yet for this assessment."
        rows={evidence.map((e, i) => ({
          id: `evidence-${i}`,
          collected_at: String(e.collected_at ?? ''),
          entries_count: typeof e.entries_count === 'number' ? e.entries_count : 0,
          note: String(e.note ?? ''),
        }))}
        totalCount={evidence.length}
        columns={[
          { header: 'Collected at', render: (r) => fmtTime(r.collected_at) },
          { header: 'Audit-log entries counted', render: (r) => String(r.entries_count ?? 0) },
          { header: 'Note', render: (r) => r.note ?? '—' },
        ]}
      />
      <AwsButton
        variant="primary"
        onClick={() => runAction(`auditmanager/assessments/${sel.id}/collect-evidence`)}
        disabled={actionBusy}
      >
        Collect evidence
      </AwsButton>
      <p className="text-xs text-aws-muted">
        Evidence is a count of real audit-log entries since the assessment started; the entries themselves stay in the
        audit log and nothing is fabricated. No AWS evidence is collected.
      </p>
    </div>
  )
}

function LastRunPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const last = (sel.last_run ?? null) as Record<string, unknown> | null
  const scanned = (last?.scanned ?? null) as Record<string, unknown> | null
  return (
    <div className="space-y-3">
      <AwsButton
        variant="primary"
        onClick={() => runAction(`macie/jobs/${sel.id}/run`)}
        disabled={actionBusy}
      >
        Run classification job
      </AwsButton>
      {last === null ? (
        <p className="text-sm text-aws-muted">This job has never run.</p>
      ) : (
        <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-[220px_1fr]">
          <dt className="font-medium">Completed at</dt>
          <dd>{fmtTime(last.completed_at)}</dd>
          <dt className="font-medium">S3 buckets scanned</dt>
          <dd>{String(scanned?.s3_buckets ?? 0)}</dd>
          <dt className="font-medium">DynamoDB tables scanned</dt>
          <dd>{String(scanned?.dynamodb_tables ?? 0)}</dd>
          <dt className="font-medium">Scoped names scanned</dt>
          <dd>{String(scanned?.named_scope ?? 0)}</dd>
          <dt className="font-medium">Findings created</dt>
          <dd>{String(last.findings_created ?? 0)}</dd>
          {typeof last.note === 'string' && last.note !== '' && (
            <>
              <dt className="font-medium">Note</dt>
              <dd>{last.note}</dd>
            </>
          )}
        </dl>
      )}
      <p className="text-xs text-aws-muted">
        A run scans S3 bucket names (when MinIO answers — it does not in lite mode) and DynamoDB table names for
        sensitive-data name patterns, and records findings in the shared findings table. Object and table contents
        are never read; no finding is invented.
      </p>
    </div>
  )
}

function BackupRunHistoryPanel({ sel, flash }: ExamDetailCtx) {
  const queryClient = useQueryClient()
  const jobs = useQuery({ queryKey: ['backup-jobs'], queryFn: () => request<Rec>('/api/v1/aws/backup/jobs'), retry: 1 })
  const mine = listOf(jobs.data, 'jobs').filter((j) => String(j.plan_id ?? '') === sel.id)
  const run = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/backup/plans/${encodeURIComponent(sel.id)}/run`, { method: 'POST', body: {} }),
    onSuccess: (d) => {
      flash(`Backup job ${String(d.name ?? '')} finished with status ${String(d.status ?? '')}.`, String(d.status ?? '') === 'COMPLETED' ? 'success' : 'error')
      void queryClient.invalidateQueries({ queryKey: ['backup-jobs'] })
    },
    onError: (e: Error) => flash(`Run failed: ${e.message}`, 'error'),
  })
  return (
    <div className="space-y-3">
      <AwsButton variant="primary" onClick={() => run.mutate()} disabled={run.isPending}>
        Run plan now (real pg_dump)
      </AwsButton>
      <p className="text-xs text-aws-muted">
        A run is a real pg_dump -Fc of the platform Postgres, with SHA-256 recorded. Without a platform database or
        pg_dump on the API host the job is FAILED with the reason — never a fake success.
      </p>
      {jobs.isPending && <Loading label="Loading jobs" />}
      {jobs.isError && <ErrorBox message={`Jobs could not be loaded (${jobs.error.message}).`} />}
      {jobs.data !== undefined && (
        <AwsTable<Rec>
          caption="Jobs for this plan"
          emptyMessage="This plan has never run."
          rows={mine}
          totalCount={mine.length}
          columns={[
            { header: 'Job', render: (r) => str(r, 'name') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Size', render: (r) => (typeof r.size_bytes === 'number' ? `${r.size_bytes} bytes` : '—') },
            { header: 'SHA-256', render: (r) => <code className="text-xs">{str(r, 'sha256').slice(0, 16) || '—'}</code> },
            { header: 'Started', render: (r) => fmtTime(r.started_at) },
            { header: 'Completed', render: (r) => fmtTime(r.completed_at) },
            { header: 'Error', render: (r) => str(r, 'error', '') },
          ]}
        />
      )}
    </div>
  )
}

function InvalidationsPanel({ distributionId, flash }: { distributionId: string; flash: ExamCtx['flash'] }) {
  const queryClient = useQueryClient()
  const [paths, setPaths] = useState('')
  const q = useQuery({
    queryKey: ['cf-invalidations', distributionId],
    queryFn: () => request<Rec>(`/api/v1/aws/cloudfront/distributions/${encodeURIComponent(distributionId)}/invalidations`),
    retry: 1,
  })
  const rows = listOf(q.data, 'invalidations')
  const create = useMutation({
    mutationFn: (list: string[]) =>
      request<Rec>(`/api/v1/aws/cloudfront/distributions/${encodeURIComponent(distributionId)}/invalidations`, {
        method: 'POST',
        body: { paths: list },
      }),
    onSuccess: (d) => {
      flash(typeof d.note === 'string' && d.note !== '' ? d.note : 'Invalidation recorded.', 'success')
      setPaths('')
      void queryClient.invalidateQueries({ queryKey: ['cf-invalidations', distributionId] })
      void queryClient.invalidateQueries({ queryKey: ['exam-list', 'cloudfront/distributions'] })
    },
    onError: (e: Error) => flash(`Create invalidation failed: ${e.message}`, 'error'),
  })
  const parsed = paths.split(/[\n,]+/).map((s) => s.trim()).filter((s) => s !== '')
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Paths (one per line or comma-separated)</span>
          <textarea value={paths} onChange={(e) => setPaths(e.target.value)} placeholder={'/images/logo.png\n/*'} rows={3} className={`w-full max-w-md ${inputCls}`} />
        </label>
        <AwsButton variant="primary" onClick={() => parsed.length > 0 && create.mutate(parsed)} disabled={create.isPending || parsed.length === 0}>
          Create invalidation
        </AwsButton>
      </div>
      <p className="text-xs text-aws-muted">
        Invalidations are teaching records, completed immediately: nothing is cached at any edge, so nothing is
        actually invalidated.
      </p>
      {q.isPending && <Loading label="Loading invalidations" />}
      {q.isError && <ErrorBox message={`Invalidations could not be loaded (${q.error.message}).`} />}
      {q.data !== undefined && (
        <AwsTable<Rec>
          caption="Invalidations"
          emptyMessage="No invalidations yet for this distribution."
          rows={rows}
          totalCount={rows.length}
          columns={[
            { header: 'Invalidation ID', render: (r) => str(r, 'id') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Paths', render: (r) => (Array.isArray(r.paths) ? (r.paths as unknown[]).join(', ') : '—') },
            { header: 'Created', render: (r) => fmtTime(r.created_at) },
            { header: 'Completed', render: (r) => fmtTime(r.completed_at) },
          ]}
        />
      )}
    </div>
  )
}

/* --- Extra top-level tab components --- */

function VaultsTab({ flash }: { flash: ExamCtx['flash'] }) {
  const [name, setName] = useState('')
  const vaults = useExamList('backup/vaults', 'vaults')
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/backup/vaults', { method: 'POST', body: { name: name.trim() } }),
    onSuccess: () => {
      flash('Backup vault created.', 'success')
      setName('')
      vaults.refresh()
    },
    onError: (e: Error) => flash(`Create vault failed: ${e.message}`, 'error'),
  })
  const del = useMutation({
    mutationFn: (id: string) => request(`/api/v1/aws/backup/vaults/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => {
      flash('Backup vault deleted.', 'success')
      vaults.refresh()
    },
    onError: (e: Error) => flash(`Delete vault failed: ${e.message}`, 'error'),
  })
  return (
    <div className="space-y-4">
      <AwsCard title="Create backup vault">
        <div className="flex flex-wrap items-end gap-2">
          <SmallForm label="Vault name" value={name} onChange={setName} placeholder="default-vault" />
          <AwsButton variant="primary" onClick={() => name.trim() !== '' && create.mutate()} disabled={create.isPending || name.trim() === ''}>
            Create vault
          </AwsButton>
        </div>
        <p className="mt-2 text-xs text-aws-muted">
          Vaults are durable records. No AWS Backup vault exists; a plan run writes a real pg_dump file and records
          its SHA-256 on the job, it does not copy into a vault.
        </p>
      </AwsCard>
      {vaults.isPending && <Loading label="Loading vaults" />}
      {vaults.isError && <ErrorBox message={`Vaults could not be loaded (${vaults.errorMessage}).`} />}
      {vaults.data !== undefined && (
        <AwsCard>
          <AwsTable<Rec>
            caption="Backup vaults"
            emptyMessage="No backup vaults yet. Choose Create vault to make one."
            rows={vaults.rows}
            totalCount={vaults.rows.length}
            columns={[
              { header: 'Vault name', render: (r) => str(r, 'name') },
              { header: 'Status', render: (r) => str(r, 'status') },
              { header: 'Created', render: (r) => fmtTime(r.created_at) },
              {
                header: 'Actions',
                render: (r) => (
                  <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(r.id)}>
                    Delete
                  </button>
                ),
              },
            ]}
          />
          <p className="mt-2 text-xs text-aws-muted">
            List columns are CloudForge&apos;s own — the 2026-11-22 survey records no Backup vault columns.
          </p>
        </AwsCard>
      )}
    </div>
  )
}

function JobsTab() {
  const jobs = useQuery({ queryKey: ['backup-jobs'], queryFn: () => request<Rec>('/api/v1/aws/backup/jobs'), retry: 1 })
  const rows = listOf(jobs.data, 'jobs')
  if (jobs.isPending) return <Loading label="Loading jobs" />
  if (jobs.isError || jobs.data === undefined) return <ErrorBox message={`Jobs could not be loaded (${jobs.error?.message ?? 'unknown error'}).`} />
  return (
    <AwsCard>
      <AwsTable<Rec>
        caption="Backup jobs"
        emptyMessage="No backup jobs yet. Run a backup plan to create one."
        rows={rows}
        totalCount={rows.length}
        columns={[
          { header: 'Job', render: (r) => str(r, 'name') },
          { header: 'Plan', render: (r) => str(r, 'plan_name') },
          { header: 'Status', render: (r) => str(r, 'status') },
          { header: 'Size', render: (r) => (typeof r.size_bytes === 'number' ? `${r.size_bytes} bytes` : '—') },
          { header: 'SHA-256', render: (r) => <code className="text-xs">{str(r, 'sha256').slice(0, 16) || '—'}</code> },
          { header: 'Started', render: (r) => fmtTime(r.started_at) },
          { header: 'Completed', render: (r) => fmtTime(r.completed_at) },
          { header: 'Error', render: (r) => str(r, 'error', '') },
        ]}
      />
      <p className="mt-2 text-xs text-aws-muted">
        Jobs are real pg_dump runs of the platform Postgres (or FAILED with the reason when no database or pg_dump
        is available). List columns are CloudForge&apos;s own — the survey records no Backup job columns.
      </p>
    </AwsCard>
  )
}

function JobsDashboardTab() {
  const jobs = useQuery({ queryKey: ['backup-jobs'], queryFn: () => request<Rec>('/api/v1/aws/backup/jobs'), retry: 1 })
  const rows = listOf(jobs.data, 'jobs')
  const count = (status: string) => rows.filter((r) => String(r.status ?? '') === status).length
  if (jobs.isPending) return <Loading label="Loading jobs" />
  if (jobs.isError) return <ErrorBox message={`Jobs could not be loaded (${jobs.error.message}).`} />
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <AwsCard title="Jobs">
          <p className="text-2xl font-semibold">{rows.length}</p>
        </AwsCard>
        <AwsCard title="Completed">
          <p className="text-2xl font-semibold">{count('COMPLETED')}</p>
        </AwsCard>
        <AwsCard title="Running">
          <p className="text-2xl font-semibold">{count('RUNNING')}</p>
        </AwsCard>
        <AwsCard title="Failed">
          <p className="text-2xl font-semibold">{count('FAILED')}</p>
        </AwsCard>
      </div>
      <p className="text-sm text-aws-muted">
        Jobs dashboard — counts from the real backup-job list. A run is a real pg_dump -Fc of the platform Postgres;
        restore is host-side only, via scripts/restore.sh (see the Backup dashboard).
      </p>
    </div>
  )
}

function ArtifactReportsTab() {
  const q = useQuery({ queryKey: ['artifact-reports'], queryFn: () => request<Rec>('/api/v1/aws/artifact/reports'), retry: 1 })
  const rows = listOf(q.data, 'reports')
  if (q.isPending) return <Loading label="Loading reports" />
  if (q.isError || q.data === undefined) return <ErrorBox message={`Reports could not be loaded (${q.error?.message ?? 'unknown error'}).`} />
  return (
    <AwsCard>
      <AwsTable<Rec>
        caption="Compliance reports catalogue"
        emptyMessage="No reports in the catalogue."
        rows={rows}
        totalCount={rows.length}
        columns={[
          { header: 'Report', render: (r) => str(r, 'name') },
          { header: 'Category', render: (r) => str(r, 'category') },
          { header: 'Source', render: (r) => str(r, 'source') },
        ]}
      />
      <p className="mt-2 text-xs text-aws-muted">
        Catalogue only: CloudForge does not redistribute AWS compliance documents. Download the report itself from
        AWS Artifact. {typeof q.data.note === 'string' ? q.data.note : ''}
      </p>
    </AwsCard>
  )
}

function MacieFindingsTab() {
  const q = useQuery({ queryKey: ['macie-findings'], queryFn: () => request<Rec>('/api/v1/aws/macie/findings'), retry: 1 })
  const rows = listOf(q.data, 'findings')
  if (q.isPending) return <Loading label="Loading findings" />
  if (q.isError || q.data === undefined) return <ErrorBox message={`Findings could not be loaded (${q.error?.message ?? 'unknown error'}).`} />
  return (
    <AwsCard>
      <AwsTable<Rec>
        caption="Macie findings"
        emptyMessage="No Macie findings. Run a classification job — a finding is created only when a scanned name matches a sensitive-data pattern."
        rows={rows}
        totalCount={rows.length}
        columns={[
          { header: 'Finding', render: (r) => str(r, 'title', str(r, 'name')) },
          { header: 'Severity', render: (r) => str(r, 'severity') },
          { header: 'Resource type', render: (r) => str(r, 'resource_type') },
          { header: 'Resource', render: (r) => str(r, 'resource_id', str(r, 'resource')) },
          { header: 'Created', render: (r) => fmtTime(r.created_at) },
        ]}
      />
      <p className="mt-2 text-xs text-aws-muted">
        Findings come only from real classification runs (names scanned, contents never read). List columns are
        CloudForge&apos;s own — the survey records no Macie columns.
      </p>
    </AwsCard>
  )
}

function CloudFrontInvalidationsTab({ flash }: { flash: ExamCtx['flash'] }) {
  const dists = useExamList('cloudfront/distributions', 'distributions')
  const [distId, setDistId] = useState('')
  const current = distId !== '' ? distId : (dists.rows[0]?.id ?? '')
  if (dists.isPending) return <Loading label="Loading distributions" />
  if (dists.isError) return <ErrorBox message={`Distributions could not be loaded (${dists.errorMessage}).`} />
  if (dists.rows.length === 0) {
    return (
      <AwsCard title="Invalidations">
        <p className="text-sm text-aws-muted">
          No distributions exist yet, so there is nothing to invalidate. Create a distribution first — invalidations
          are per-distribution.
        </p>
      </AwsCard>
    )
  }
  return (
    <AwsCard title="Invalidations">
      <label className="mb-3 block text-sm">
        <span className="mb-1 block font-medium">Distribution</span>
        <select value={current} onChange={(e) => setDistId(e.target.value)} className={inputCls}>
          {dists.rows.map((d) => (
            <option key={d.id} value={d.id}>
              {str(d, 'name')} ({str(d, 'domain_name')})
            </option>
          ))}
        </select>
      </label>
      {current !== '' && <InvalidationsPanel distributionId={current} flash={flash} />}
    </AwsCard>
  )
}

/* --- One more detail panel (Direct Connect confirm) --- */

function DxConfirmPanel({ sel, runAction, actionBusy }: ExamDetailCtx) {
  const status = String(sel.status ?? '')
  return (
    <div className="space-y-3">
      <p className="text-sm">
        Connection status: <strong>{status}</strong>
        {status === 'AVAILABLE' && typeof sel.confirmed_at === 'string' && (
          <span className="text-aws-muted"> · confirmed {fmtTime(sel.confirmed_at)}</span>
        )}
      </p>
      <AwsButton
        variant="primary"
        onClick={() => runAction(`directconnect/connections/${sel.id}/confirm`)}
        disabled={actionBusy || status !== 'REQUESTED'}
        title={status !== 'REQUESTED' ? `Only a REQUESTED connection can be confirmed (current: ${status})` : undefined}
      >
        Confirm connection
      </AwsButton>
      <p className="text-xs text-aws-muted">
        Confirmation is a teaching state change from REQUESTED to AVAILABLE: no physical cross-connect, LOA-CFA or
        BGP session exists.
      </p>
    </div>
  )
}

/* --- Service configs --- */

const AURORA_CONFIG: ExamServiceConfig = {
  id: 'aurora',
  name: 'Aurora',
  category: 'Databases',
  icon: 'database',
  description: 'Aurora clusters — durable records with writer/reader instances and failover.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Clusters are durable records; no Aurora engine runs — instances are teaching records and endpoints are teaching names. Failover swaps the recorded writer/reader roles.',
  banner:
    'Aurora clusters are durable records with writer/reader instances and failover. No Aurora storage engine runs and no data is replicated — endpoints are teaching names. Use RDS for a real Postgres database.',
  listSection: 'clusters',
  listLabel: 'Clusters',
  listPath: 'aurora/clusters',
  listKey: 'clusters',
  kindLabel: 'cluster',
  createLabel: 'Create cluster',
  columns: [
    { header: 'Cluster', render: (r) => str(r, 'name') },
    { header: 'Engine', render: (r) => str(r, 'engine') },
    { header: 'Status', render: (r) => str(r, 'status') },
    {
      header: 'Instances',
      render: (r) => {
        const instances = Array.isArray(r.instances) ? (r.instances as Record<string, unknown>[]) : []
        const writer = instances.find((i) => i.role === 'writer')
        return `${instances.length}${writer !== undefined ? ` · writer ${String(writer.id ?? '')}` : ''}`
      },
    },
    { header: 'Writer endpoint', render: (r) => <code className="text-xs">{str(r, 'writer_endpoint')}</code> },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Aurora'),
  createFields: [
    { key: 'engine', label: 'Engine', type: 'select', options: ['aurora-postgresql', 'aurora-mysql'], required: true },
    { key: 'instance_count', label: 'Instance count', type: 'number', defaultValue: 2, hint: '1–3 instances; the first is the writer.' },
  ],
  createNote: 'Creating a cluster records writer/reader instances and teaching endpoints. Nothing is provisioned.',
  detailTabs: [
    { id: 'instances', label: 'Instances', render: (ctx) => <AuroraInstancesPanel {...ctx} /> },
    { id: 'endpoints', label: 'Endpoints', render: (ctx) => <AuroraEndpointsPanel sel={ctx.sel} /> },
  ],
  homeCards: singleCard('Clusters', 'clusters'),
}

const BACKUP_CONFIG: ExamServiceConfig = {
  id: 'backup',
  name: 'Backup',
  category: 'Additional',
  icon: 'hard-drive',
  description: 'Backup plans, vaults and jobs — plans/vaults are records; a run is a real pg_dump.',
  fidelity: 'REAL',
  fidelityFootnote:
    'Plans and vaults are durable records; a plan run is a real pg_dump -Fc of the platform Postgres with SHA-256 recorded. Restore is host-side scripts/restore.sh — there is no restore API.',
  banner:
    'Backup plans and vaults are durable records. Running a plan is a real pg_dump -Fc of the platform Postgres, with SHA-256 recorded — never a fake success. Restore is host-side only, via scripts/restore.sh.',
  defaultSection: 'home',
  listSection: 'plans',
  listLabel: 'Backup plans',
  listPath: 'backup/plans',
  listKey: 'plans',
  kindLabel: 'backup-plan',
  createLabel: 'Create backup plan',
  columns: [
    { header: 'Plan', render: (r) => str(r, 'name') },
    { header: 'Schedule', render: (r) => str(r, 'schedule') },
    { header: 'Retention (days)', render: (r) => String(r.retention_days ?? '—') },
    {
      header: 'Resources',
      render: (r) => (Array.isArray(r.resources) ? String((r.resources as unknown[]).length) : '0'),
    },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Backup'),
  createFields: [
    { key: 'schedule', label: 'Schedule', type: 'text', defaultValue: '0 2 * * *', hint: 'Cron schedule — recorded only; no scheduler runs the plan by itself.' },
    { key: 'retention_days', label: 'Retention (days)', type: 'number', defaultValue: 30 },
  ],
  createNote: 'A plan is a durable record. Nothing runs until you run it — and a run is a real pg_dump.',
  detailTabs: [{ id: 'run-history', label: 'Run history', render: (ctx) => <BackupRunHistoryPanel {...ctx} /> }],
  extraTabs: [
    { id: 'jobs-dashboard', label: 'Jobs dashboard', group: 'My account', render: () => <JobsDashboardTab /> },
    { id: 'vaults', label: 'Vaults', group: 'My account', render: (ctx) => <VaultsTab flash={ctx.flash} /> },
    {
      id: 'vault-locks',
      label: 'Backup vault locks',
      group: 'My account',
      unsupported:
        'Backup vault locks are not built in CloudForge — there is no lock / retention-lock API and no vault is immutable. Vaults are plain durable records.',
    },
    {
      id: 's3-tiering',
      label: 'S3 backup tiering',
      group: 'My account',
      unsupported: 'S3 backup tiering is not built — there is no cold-storage tier for backup files in CloudForge.',
    },
    {
      id: 'protected-resources',
      label: 'Protected resources',
      group: 'My account',
      unsupported:
        'Protected resources are not built as a list — a plan records resource names, but nothing tracks which resources are protected or their compliance state.',
    },
    {
      id: 'access-points',
      label: 'Access points',
      group: 'My account',
      unsupported: 'Backup access points are not built in CloudForge.',
    },
    {
      id: 'search',
      label: 'Search',
      group: 'My account',
      unsupported: 'Backup search is not built — use the Jobs list and the Resource Explorer to find backup records.',
    },
    { id: 'jobs', label: 'Jobs', group: 'My account', render: () => <JobsTab /> },
    {
      id: 'malware-protection',
      label: 'Malware protection',
      group: 'My account',
      unsupported: 'Malware protection scanning is not built — backup files are not scanned.',
    },
    {
      id: 'restore-testing',
      label: 'Restore testing',
      group: 'My account',
      unsupported: 'Restore testing is not built — restore is a manual host-side run of scripts/restore.sh (see the dashboard).',
    },
    {
      id: 'legal-holds',
      label: 'Legal holds',
      group: 'My account',
      unsupported: 'Legal holds are not built in CloudForge.',
    },
    {
      id: 'settings',
      label: 'Settings',
      group: 'My account',
      unsupported: 'Backup settings are not built — there is no configurable backup service to tune. Retention is per-plan.',
    },
    {
      id: 'gateways',
      label: 'Gateways',
      group: 'External resources',
      unsupported: 'External gateways (Storage Gateway appliances) are not connected to Backup in CloudForge.',
    },
    {
      id: 'hypervisors',
      label: 'Hypervisors',
      group: 'External resources',
      unsupported: 'External hypervisors are not connected to Backup in CloudForge.',
    },
    {
      id: 'virtual-machines',
      label: 'Virtual machines',
      group: 'External resources',
      unsupported: 'External virtual machines are not connected to Backup in CloudForge.',
    },
    {
      id: 'cross-account-monitoring',
      label: 'Cross-account monitoring',
      group: 'My organization',
      unsupported: 'Cross-account monitoring is not built — there is no second account in CloudForge.',
    },
    {
      id: 'backup-policies',
      label: 'Backup policies',
      group: 'My organization',
      unsupported: 'Backup policies (organization-wide) are not built — plans are per-user records here.',
    },
    {
      id: 'frameworks',
      label: 'Frameworks',
      group: 'Backup Audit Manager',
      unsupported: 'Backup Audit Manager frameworks are not built — use Audit Manager assessments for audit evidence.',
    },
    {
      id: 'audit-reports',
      label: 'Reports',
      group: 'Backup Audit Manager',
      unsupported: 'Backup Audit Manager reports are not built — use CloudTrail export for audit evidence.',
    },
  ],
  homeCountSources: [
    { path: 'backup/vaults', listKey: 'vaults' },
    { path: 'backup/jobs', listKey: 'jobs' },
  ],
  homeCards: (rows, goTo, extra) => [
    { label: 'Backup plans', count: rows.length, onSelect: () => goTo('plans') },
    { label: 'Backup vaults', count: extra?.['backup/vaults'] ?? '—', onSelect: () => goTo('vaults') },
    { label: 'Backup jobs', count: extra?.['backup/jobs'] ?? '—', onSelect: () => goTo('jobs') },
  ],
  homeExtra: (
    <AwsCard title="Backup scripts (host-side)">
      <p className="text-sm text-aws-muted">
        Run on the CloudForge host, not from this console: <code>scripts/backup.sh</code> writes a pg_dump -Fc file
        (default <code>./backups</code>, keeps 7), and <code>scripts/restore.sh</code> restores it with pg_restore.
        The backup/restore round-trip was verified in Milestone 9. Restore is DESTRUCTIVE for the target
        database&apos;s CloudForge data: restore.sh refuses a non-empty database unless --force is given. There is
        no restore API — a plan run in this console only ever dumps, it never restores.
      </p>
    </AwsCard>
  ),
  health: (rows) => [
    `${rows.length} backup plan(s).`,
    'A plan run is a real pg_dump -Fc of the platform Postgres with SHA-256 recorded.',
    'Restore is host-side only: scripts/restore.sh (destructive without --force).',
    'Registry fidelity: REAL (the run); plans and vaults are durable records.',
  ],
}

const MACIE_CONFIG: ExamServiceConfig = {
  id: 'macie',
  name: 'Macie',
  category: 'Security',
  icon: 'shield',
  description: 'Sensitive-data discovery — classification jobs that scan real local metadata.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Classification jobs scan S3 bucket names and DynamoDB table names for sensitive-data patterns; contents are never read and no finding is invented.',
  banner:
    'Sensitive-data discovery: a run scans S3 bucket names (when MinIO answers — it does not in lite mode) and DynamoDB table names for sensitive-data name patterns. Object and table contents are never read.',
  listSection: 'jobs',
  listLabel: 'Classification jobs',
  listPath: 'macie/jobs',
  listKey: 'jobs',
  kindLabel: 'classification-job',
  createLabel: 'Create classification job',
  columns: [
    { header: 'Job', render: (r) => str(r, 'name') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Findings created', render: (r) => String(r.findings_created ?? 0) },
    {
      header: 'Last run',
      render: (r) => {
        const last = (r.last_run ?? null) as Record<string, unknown> | null
        return last !== null && typeof last.completed_at === 'string' ? fmtTime(last.completed_at) : 'Never'
      },
    },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Macie'),
  createFields: [
    {
      key: 'scope',
      label: 'Scope (names to scan)',
      type: 'list',
      hint: 'S3 bucket or DynamoDB table names, one per line or comma-separated. Contents are never read.',
    },
  ],
  detailTabs: [{ id: 'last-run', label: 'Last run', render: (ctx) => <LastRunPanel {...ctx} /> }],
  extraTabs: [{ id: 'findings', label: 'Findings', render: () => <MacieFindingsTab /> }],
  homeCards: (rows, goTo) => [
    { label: 'Classification jobs', count: rows.length, onSelect: () => goTo('jobs') },
    { label: 'Findings', count: '—', note: 'Open the Findings tab for the live count.', onSelect: () => goTo('findings') },
  ],
}

const AUDITMANAGER_CONFIG: ExamServiceConfig = {
  id: 'auditmanager',
  name: 'Audit Manager',
  category: 'Security',
  icon: 'clipboard',
  description: 'Audit assessments with evidence counted from the real audit log.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Evidence is a count of real audit-log entries since the assessment started; nothing is fabricated.',
  banner:
    'Audit assessments with evidence counted from the real audit log. Collecting evidence counts real audit-log entries since the assessment started — no AWS evidence is collected and no evidence set is fabricated.',
  listSection: 'assessments',
  listLabel: 'Assessments',
  listPath: 'auditmanager/assessments',
  listKey: 'assessments',
  kindLabel: 'assessment',
  createLabel: 'Create assessment',
  columns: [
    { header: 'Assessment', render: (r) => str(r, 'name') },
    { header: 'Framework', render: (r) => str(r, 'framework') },
    { header: 'Status', render: (r) => str(r, 'status') },
    {
      header: 'Evidence entries',
      render: (r) => {
        const ev = Array.isArray(r.evidence) ? (r.evidence as Record<string, unknown>[]) : []
        return String(ev.reduce((sum, e) => sum + (typeof e.entries_count === 'number' ? e.entries_count : 0), 0))
      },
    },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Audit Manager'),
  createFields: [
    { key: 'framework', label: 'Framework', type: 'text', required: true, placeholder: 'e.g. CIS AWS Foundations Benchmark' },
  ],
  detailTabs: [{ id: 'evidence', label: 'Evidence', render: (ctx) => <EvidencePanel {...ctx} /> }],
  homeCards: singleCard('Assessments', 'assessments'),
}

const ARTIFACT_CONFIG: ExamServiceConfig = {
  id: 'artifact',
  name: 'Artifact',
  category: 'Security',
  icon: 'file-text',
  description: "Compliance reports catalogue and the organization's own agreements.",
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'The report list is a catalogue of real AWS Artifact report names; CloudForge does not redistribute AWS compliance documents.',
  banner:
    'Compliance reports catalogue and the organization’s own agreements. The report list is a catalogue of real AWS Artifact report names — CloudForge does not serve the documents themselves; download them from AWS Artifact.',
  listSection: 'agreements',
  listLabel: 'Agreements',
  listPath: 'artifact/agreements',
  listKey: 'agreements',
  kindLabel: 'agreement',
  createLabel: 'Create agreement',
  columns: [
    { header: 'Agreement', render: (r) => str(r, 'name') },
    { header: 'Report', render: (r) => str(r, 'report') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Artifact'),
  createFields: [
    {
      key: 'report',
      label: 'Report',
      type: 'select',
      required: true,
      options: [
        'SOC 2 Type II',
        'SOC 3',
        'ISO 27001',
        'PCI DSS Attestation of Compliance',
        'CSA STAR',
        'FedRAMP',
        'HIPAA',
        'GDPR',
      ],
    },
  ],
  createNote: 'Accepting an agreement here records the organization’s own agreement — it accepts nothing with AWS.',
  detailTabs: [],
  extraTabs: [{ id: 'reports', label: 'Reports', render: () => <ArtifactReportsTab /> }],
  homeCards: (rows, goTo) => [
    { label: 'Agreements', count: rows.length, onSelect: () => goTo('agreements') },
    { label: 'Reports', count: '—', note: 'Catalogue of AWS Artifact report names — open the Reports tab.', onSelect: () => goTo('reports') },
  ],
}

const CLOUDFRONT_CONFIG: ExamServiceConfig = {
  id: 'cloudfront',
  name: 'CloudFront',
  category: 'Networking',
  icon: 'network',
  description: 'Content delivery distributions — durable records with invalidations.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Distributions are durable records with invalidations. No edge location caches anything and the domain name is a teaching name, not registered or served. Invalidations are recorded COMPLETED; nothing is actually invalidated at an edge.',
  banner:
    'Content delivery distributions — durable records with invalidations. No edge location caches anything; the domain name is a teaching name, not registered or served.',
  defaultSection: 'distributions',
  listSection: 'distributions',
  listLabel: 'Distributions',
  listPath: 'cloudfront/distributions',
  listKey: 'distributions',
  kindLabel: 'distribution',
  createLabel: 'Create distribution',
  columns: [
    { header: 'ID', render: (r) => <code className="text-xs">{str(r, 'id')}</code> },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Description', render: (r) => str(r, 'description', str(r, 'name')) },
    { header: 'Type', render: () => <span title="CloudForge does not store a distribution type">Not available</span> },
    { header: 'Domain name', render: (r) => <code className="text-xs">{str(r, 'domain_name')}</code> },
    { header: 'Alternate domain names', render: () => <span title="CloudForge does not store alternate domain names">Not available</span> },
    { header: 'Origins', render: (r) => str(r, 'origin') },
    { header: 'Pricing plan', render: () => <span title="CloudForge does not price distributions">Not available</span> },
    { header: 'Last modified', render: (r) => fmtTime(r.updated_at) },
  ],
  columnsNote:
    'Distributions columns are the survey’s exact columns (ID, Status, Description, Type, Domain name, Alternate domain names, Origins, Pricing plan, Last modified). “Not available” means CloudForge stores no value for that column.',
  createFields: [
    { key: 'origin', label: 'Origin', type: 'text', required: true, placeholder: 'e.g. app-bucket.s3.cf-local' },
    { key: 'description', label: 'Description', type: 'text', placeholder: 'What this distribution serves' },
  ],
  detailTabs: [
    { id: 'invalidations', label: 'Invalidations', render: (ctx) => <InvalidationsPanel distributionId={ctx.sel.id} flash={ctx.flash} /> },
  ],
  extraTabs: [
    {
      id: 'policies',
      label: 'Policies',
      unsupported:
        'Policies are not built — there is no CloudFront policy API in CloudForge. The 2026-11-22 v4 survey records Policies in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'functions',
      label: 'Functions',
      unsupported: 'CloudFront Functions are not built — there is no edge function runtime in CloudForge.',
    },
    {
      id: 'static-ips',
      label: 'Static IPs',
      unsupported:
        'Static IPs are not built — CloudFront distributions here have teaching domain names, not static anycast IPs. The 2026-11-22 v4 survey records Static IPs in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'vpc-origins',
      label: 'VPC origins',
      unsupported:
        'VPC origins are not built — there is no VPC-origin API in CloudForge. The 2026-11-22 v4 survey records VPC origins in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'multi-tenant-distributions',
      label: 'Multi-tenant distributions',
      group: 'SaaS',
      unsupported:
        'Multi-tenant distributions are not built — there is no SaaS distribution API in CloudForge. The 2026-11-22 v4 survey records Multi-tenant distributions under SaaS in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'distribution-tenants',
      label: 'Distribution tenants',
      group: 'SaaS',
      unsupported:
        'Distribution tenants are not built — there is no SaaS tenant API in CloudForge. The 2026-11-22 v4 survey records Distribution tenants under SaaS in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'monitoring',
      label: 'Monitoring',
      group: 'Telemetry',
      unsupported:
        'Monitoring is not built — no edge location caches anything in CloudForge, so there is no CloudFront telemetry to show. The 2026-11-22 v4 survey records Monitoring under Telemetry in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'alarms',
      label: 'Alarms',
      group: 'Telemetry',
      unsupported:
        'Alarms are not built — there is no CloudFront telemetry to alarm on in CloudForge. The 2026-11-22 v4 survey records Alarms under Telemetry in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'logs',
      label: 'Logs',
      group: 'Telemetry',
      unsupported:
        'Logs are not built — no edge location logs anything in CloudForge. The 2026-11-22 v4 survey records Logs under Telemetry in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'cache-statistics',
      label: 'Cache statistics',
      group: 'Reports & analytics',
      unsupported: 'Cache statistics are not built — no edge location caches anything in CloudForge, so there is no cache telemetry to show.',
    },
    {
      id: 'popular-objects',
      label: 'Popular objects',
      group: 'Reports & analytics',
      unsupported:
        'Popular objects are not built — no edge usage data exists in CloudForge. The 2026-11-22 v4 survey records Popular objects under Reports & analytics in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'top-referrers',
      label: 'Top referrers',
      group: 'Reports & analytics',
      unsupported:
        'Top referrers are not built — no edge usage data exists in CloudForge. The 2026-11-22 v4 survey records Top referrers under Reports & analytics in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'usage',
      label: 'Usage',
      group: 'Reports & analytics',
      unsupported:
        'Usage is not built — no edge usage data exists in CloudForge. The 2026-11-22 v4 survey records Usage under Reports & analytics in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'viewers',
      label: 'Viewers',
      group: 'Reports & analytics',
      unsupported:
        'Viewers are not built — no edge usage data exists in CloudForge. The 2026-11-22 v4 survey records Viewers under Reports & analytics in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'origin-access',
      label: 'Origin access',
      group: 'Security',
      unsupported: 'Origin access (OAC / OAI) is not built — there is no origin-access control API in CloudForge.',
    },
    {
      id: 'trust-stores',
      label: 'Trust stores',
      group: 'Security',
      unsupported:
        'Trust stores are not built — there is no trust-store API in CloudForge. The 2026-11-22 v4 survey records Trust stores under Security in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'field-level-encryption',
      label: 'Field-level encryption',
      group: 'Security',
      unsupported: 'Field-level encryption is not built in CloudForge.',
    },
    {
      id: 'public-keys',
      label: 'Public keys',
      group: 'Key management',
      unsupported: 'Public keys are not built — there is no key management for signed URLs / cookies in CloudForge.',
    },
    {
      id: 'key-groups',
      label: 'Key groups',
      group: 'Key management',
      unsupported: 'Key groups are not built — there is no key management for signed URLs / cookies in CloudForge.',
    },
    {
      id: 'savings-overview',
      label: 'Overview',
      group: 'Savings Bundle',
      unsupported:
        'Savings Bundle overview is not built — CloudFront distributions are not priced in CloudForge. The 2026-11-22 v4 survey records Overview under Savings Bundle in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'savings-inventory',
      label: 'Inventory',
      group: 'Savings Bundle',
      unsupported:
        'Savings Bundle inventory is not built — CloudFront distributions are not priced in CloudForge. The 2026-11-22 v4 survey records Inventory under Savings Bundle in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'savings-purchase',
      label: 'Purchase',
      group: 'Savings Bundle',
      unsupported:
        'Savings Bundle purchase is not built — CloudFront distributions are not priced in CloudForge. The 2026-11-22 v4 survey records Purchase under Savings Bundle in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'resource-sharing',
      label: 'Resource sharing',
      unsupported:
        'Resource sharing is not built — there is no CloudFront resource-sharing API in CloudForge. The 2026-11-22 v4 survey records Resource sharing in the CloudFront left nav; the tab is shown as reference only.',
    },
    {
      id: 'settings',
      label: 'Settings',
      unsupported:
        'Settings are not built — there is no CloudFront settings API in CloudForge. The 2026-11-22 v4 survey records Settings in the CloudFront left nav; the tab is shown as reference only.',
    },
    { id: 'invalidations', label: 'Invalidations', render: (ctx) => <CloudFrontInvalidationsTab flash={ctx.flash} /> },
    {
      id: 'whats-new',
      label: "What's new",
      render: () => (
        <AwsCard title="What's new">
          <p className="text-sm text-aws-muted">
            CloudForge has no feed of AWS CloudFront release notes, and this tab does not pretend to be one. For what
            is new in CloudForge itself, see the Learning Center and the release notes in the repository. The tab is
            shown because the 2026-11-22 survey records “What&apos;s new” in the CloudFront left nav.
          </p>
        </AwsCard>
      ),
    },
  ],
  homeCards: (rows, goTo) => [
    { label: 'Distributions', count: rows.length, onSelect: () => goTo('distributions') },
    { label: 'Invalidations', count: '—', note: 'Per-distribution — open a distribution.', onSelect: () => goTo('invalidations') },
  ],
}

const EFS_CONFIG: ExamServiceConfig = {
  id: 'efs',
  name: 'EFS',
  category: 'Storage',
  icon: 'hard-drive',
  description: 'Elastic file systems and mount targets — durable records.',
  fidelity: 'API_EMULATED',
  fidelityFootnote: 'No NFS endpoint exists and nothing can be mounted; mount targets are recorded against teaching subnet names.',
  banner:
    'Elastic file systems and mount targets — durable records. No NFS endpoint exists and nothing can be mounted; mount targets are recorded against teaching subnet names.',
  defaultSection: 'home',
  listSection: 'file-systems',
  listLabel: 'File systems',
  listPath: 'efs/file-systems',
  listKey: 'file-systems',
  kindLabel: 'file-system',
  createLabel: 'Create file system',
  columns: [
    { header: 'Name', render: (r) => str(r, 'name') },
    { header: 'File system ID', render: (r) => <code className="text-xs">{str(r, 'id')}</code> },
    { header: 'Encrypted', render: (r) => (r.encrypted === false ? 'No' : 'Yes') },
    {
      header: 'Total size',
      render: () => <span title="CloudForge stores no size accounting for file systems">Not available</span>,
    },
    {
      header: 'Size in Standard',
      render: () => <span title="CloudForge stores no size accounting for file systems">Not available</span>,
    },
    {
      header: 'Size in IA',
      render: () => <span title="CloudForge stores no size accounting for file systems">Not available</span>,
    },
    {
      header: 'Size in Archive',
      render: () => <span title="CloudForge stores no size accounting for file systems">Not available</span>,
    },
    {
      header: 'Provisioned Throughput (MiB/s)',
      render: () => <span title="CloudForge stores no provisioned throughput for file systems">Not available</span>,
    },
    { header: 'File system state', render: (r) => str(r, 'status') },
    { header: 'Creation time', render: (r) => fmtTime(r.created_at) },
    {
      header: 'Availability Zone',
      render: () => <span title="CloudForge file systems are not placed in an Availability Zone">Not available</span>,
    },
    {
      header: 'Replication overwrite protection',
      render: () => <span title="CloudForge has no EFS replication">Not available</span>,
    },
  ],
  columnsNote:
    'File systems columns are the 2026-11-22 v4 survey’s exact columns (Name, File system ID, Encrypted, Total size, Size in Standard, Size in IA, Size in Archive, Provisioned Throughput (MiB/s), File system state, Creation time, Availability Zone, Replication overwrite protection), in order. “Not available” means CloudForge stores no value for that column. Performance mode, throughput mode and mount targets are CloudForge’s own fields — they stay on the Details tab and the Mount targets tab, not as list columns.',
  createFields: [
    { key: 'performance_mode', label: 'Performance mode', type: 'select', options: ['generalPurpose', 'maxIO'], defaultValue: 'generalPurpose' },
    { key: 'throughput_mode', label: 'Throughput mode', type: 'select', options: ['bursting', 'provisioned', 'elastic'], defaultValue: 'bursting' },
  ],
  detailTabs: [
    { id: 'mount-targets', label: 'Mount targets', render: (ctx) => <MountTargetsPanel {...ctx} basePath="efs/file-systems" /> },
  ],
  extraTabs: [
    {
      id: 'access-points',
      label: 'Access points',
      unsupported:
        'EFS access points are not built — there is no access-point API in CloudForge. The survey records Access points in the EFS left nav; the tab is shown as reference only.',
    },
  ],
  homeCards: singleCard('File systems', 'file-systems'),
  homeExtra: (
    <AwsCard title="Get started with EFS">
      <p className="text-sm text-aws-muted">
        Create your first file system from the File systems tab — a durable record with a performance mode and a
        throughput mode. No NFS endpoint exists and nothing can be mounted; mount targets are recorded against
        teaching subnet names. The 2026-11-22 v4 survey records the file-systems table columns used on the File
        systems tab; the links below are CloudForge&apos;s own.
      </p>
      <ul className="mt-3 space-y-1 text-sm">
        <li>
          <Link to="/services/backup" className="text-aws-link hover:underline">
            Backup
          </Link>
          <span className="text-aws-muted"> — back up file-system contents with real pg_dump jobs.</span>
        </li>
        <li>
          <Link to="/services/datasync" className="text-aws-link hover:underline">
            DataSync
          </Link>
          <span className="text-aws-muted"> — copy files with SHA-256 verification.</span>
        </li>
        <li>
          <Link to="/services/transfer" className="text-aws-link hover:underline">
            Transfer Family
          </Link>
          <span className="text-aws-muted"> — UNSUPPORTED here: no local SFTP server; DataSync is the alternative.</span>
        </li>
        <li>
          <Link to="/learn" className="text-aws-link hover:underline">
            Documentation — Learning Center
          </Link>
          <span className="text-aws-muted"> — storage track and certification coverage map.</span>
        </li>
      </ul>
    </AwsCard>
  ),
}

const FSX_CONFIG: ExamServiceConfig = {
  id: 'fsx',
  name: 'FSx',
  category: 'Storage',
  icon: 'hard-drive',
  description: 'Managed file systems (Lustre, Windows, ONTAP, OpenZFS) — durable records.',
  fidelity: 'API_EMULATED',
  fidelityFootnote: 'No Lustre/Windows/ONTAP volume exists and nothing can be mounted; mount targets are recorded against teaching subnet names.',
  banner:
    'Managed file systems (Lustre, Windows, ONTAP, OpenZFS) — durable records. No Lustre/Windows/ONTAP volume exists and nothing can be mounted.',
  listSection: 'file-systems',
  listLabel: 'File systems',
  listPath: 'fsx/file-systems',
  listKey: 'file-systems',
  kindLabel: 'file-system',
  createLabel: 'Create file system',
  columns: [
    { header: 'File system', render: (r) => str(r, 'name') },
    { header: 'Type', render: (r) => str(r, 'file_system_type') },
    { header: 'Storage (GB)', render: (r) => String(r.storage_capacity_gb ?? '—') },
    { header: 'Throughput (Mbps)', render: (r) => String(r.throughput_mbps ?? '—') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('FSx'),
  createFields: [
    { key: 'file_system_type', label: 'File system type', type: 'select', options: ['LUSTRE', 'WINDOWS', 'ONTAP', 'OPENZFS'], required: true },
    { key: 'storage_capacity_gb', label: 'Storage capacity (GB)', type: 'number', defaultValue: 1200 },
    { key: 'throughput_mbps', label: 'Throughput (Mbps)', type: 'number', defaultValue: 128 },
  ],
  detailTabs: [
    { id: 'mount-targets', label: 'Mount targets', render: (ctx) => <MountTargetsPanel {...ctx} basePath="fsx/file-systems" /> },
  ],
  extraTabs: [
    {
      id: 'volumes',
      label: 'Volumes',
      unsupported:
        'Volumes are not built — there is no FSx volume API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'snapshots',
      label: 'Snapshots',
      unsupported:
        'Snapshots are not built — there is no FSx snapshot API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 's3-access-points',
      label: 'S3 Access Points',
      unsupported:
        'S3 Access Points are not built — there is no FSx S3 access-point API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'file-caches',
      label: 'File Caches',
      unsupported:
        'File Caches are not built — there is no FSx file-cache API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'backups',
      label: 'Backups',
      unsupported:
        'Backups are not built — there is no FSx backup API in CloudForge; use the Backup console for pg_dump jobs. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'storage-virtual-machines',
      label: 'Storage virtual machines',
      group: 'ONTAP',
      unsupported:
        'ONTAP storage virtual machines are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'cluster-peers',
      label: 'Cluster peers',
      group: 'ONTAP',
      unsupported:
        'ONTAP cluster peers are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'svm-peers',
      label: 'SVM peers',
      group: 'ONTAP',
      unsupported:
        'ONTAP SVM peers are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'snapmirror-relationships',
      label: 'SnapMirror relationships',
      group: 'ONTAP',
      unsupported:
        'SnapMirror relationships are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'snapmirror-policies',
      label: 'SnapMirror policies',
      group: 'ONTAP',
      unsupported:
        'SnapMirror policies are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'snapshot-policies',
      label: 'Snapshot policies',
      group: 'ONTAP',
      unsupported:
        'ONTAP snapshot policies are not built — there is no ONTAP API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'service-quotas',
      label: 'FSx on Service Quotas',
      unsupported:
        'FSx on Service Quotas is not built — there is no Service Quotas API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'settings',
      label: 'Settings',
      unsupported:
        'Settings are not built — there is no FSx settings API in CloudForge. In the 2026-11-22 v4 survey account FSx showed a paid-plan restriction dialog (“Service FSx unavailable — Upgrade to paid plan”), so only the nav label was recorded and no page structure was observed.',
    },
  ],
  homeCards: singleCard('File systems', 'file-systems'),
}

const SHIELD_CONFIG: ExamServiceConfig = {
  id: 'shield',
  name: 'Shield',
  category: 'Security and Identity',
  icon: 'shield',
  description: 'DDoS protection records — which resources are covered.',
  fidelity: 'API_EMULATED',
  fidelityFootnote: 'Protections are records only: no traffic is inspected, no attack is mitigated and no Shield Advanced subscription exists.',
  banner:
    'DDoS protection records — which resources are covered. Protections are records only: no traffic is inspected, no attack is mitigated, and no Shield Advanced subscription exists.',
  listSection: 'protections',
  listLabel: 'Protections',
  listPath: 'shield/protections',
  listKey: 'protections',
  kindLabel: 'protection',
  createLabel: 'Create protection',
  columns: [
    { header: 'Protection', render: (r) => str(r, 'name') },
    { header: 'Resource type', render: (r) => str(r, 'resource_type') },
    { header: 'Resource ID', render: (r) => str(r, 'resource_id') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Shield'),
  createFields: [
    { key: 'resource_type', label: 'Resource type', type: 'text', required: true, placeholder: 'e.g. cloudfront-distribution' },
    { key: 'resource_id', label: 'Resource ID', type: 'text', required: true, placeholder: 'e.g. my-distribution' },
  ],
  detailTabs: [],
  extraTabs: [
    {
      id: 'threat-dashboard',
      label: 'Global threat dashboard',
      unsupported:
        'The global threat dashboard is not built — no traffic is inspected in CloudForge, so there is no threat telemetry to show. The survey records it as part of the AWS Shield group inside the WAF console, not as a standalone Shield layout.',
    },
    {
      id: 'advanced',
      label: 'Advanced',
      unsupported:
        'Shield Advanced is not built — there is no Shield Advanced subscription, no 24/7 response team and no cost protection in CloudForge. The survey records it as part of the AWS Shield group inside the WAF console.',
    },
  ],
  homeCards: singleCard('Protections', 'protections'),
}

const CONTROLTOWER_CONFIG: ExamServiceConfig = {
  id: 'controltower',
  name: 'Control Tower',
  category: 'Management',
  icon: 'shield',
  description: 'Landing zones with guardrails — durable records.',
  fidelity: 'API_EMULATED',
  fidelityFootnote: 'No accounts are vended and no SCPs or Config rules are enforced; guardrails are recorded settings.',
  banner:
    'Landing zones with guardrails — durable records. No accounts are vended and no SCPs or Config rules are enforced; guardrails are recorded settings. Enforcement is modelled in the Organizations / SCP lessons.',
  listSection: 'landing-zones',
  listLabel: 'Landing zones',
  listPath: 'controltower/landing-zones',
  listKey: 'landing-zones',
  kindLabel: 'landing-zone',
  createLabel: 'Create landing zone',
  columns: [
    { header: 'Landing zone', render: (r) => str(r, 'name') },
    { header: 'Home region', render: (r) => str(r, 'home_region') },
    { header: 'Version', render: (r) => str(r, 'version') },
    {
      header: 'Guardrails',
      render: (r) => String(Array.isArray(r.guardrails) ? (r.guardrails as unknown[]).length : 0),
    },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Control Tower'),
  createFields: [{ key: 'home_region', label: 'Home region', type: 'text', defaultValue: 'cf-local-1' }],
  detailTabs: [{ id: 'guardrails', label: 'Guardrails', render: (ctx) => <GuardrailsPanel {...ctx} /> }],
  homeCards: singleCard('Landing zones', 'landing-zones'),
}

const IDENTITYCENTER_CONFIG: ExamServiceConfig = {
  id: 'identitycenter',
  name: 'IAM Identity Center',
  category: 'Security',
  icon: 'shield',
  description: 'Workforce SSO permission sets and account assignments — durable records.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'No IdP is federated and assignments grant no access to any account; they are recorded for teaching. The IAM evaluator is the enforcement model.',
  banner:
    'Workforce SSO permission sets and account assignments — durable records. No IdP is federated and assignments grant no access to any account; they are recorded for teaching.',
  listSection: 'permission-sets',
  listLabel: 'Permission sets',
  listPath: 'identitycenter/permission-sets',
  listKey: 'permission-sets',
  kindLabel: 'permission-set',
  createLabel: 'Create permission set',
  columns: [
    { header: 'Permission set', render: (r) => str(r, 'name') },
    { header: 'Session duration', render: (r) => (typeof r.session_duration_seconds === 'number' ? `${r.session_duration_seconds} s` : '—') },
    {
      header: 'Managed policies',
      render: (r) => String(Array.isArray(r.managed_policies) ? (r.managed_policies as unknown[]).length : 0),
    },
    {
      header: 'Assignments',
      render: (r) => String(Array.isArray(r.assignments) ? (r.assignments as unknown[]).length : 0),
    },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('IAM Identity Center'),
  createFields: [
    { key: 'session_duration_seconds', label: 'Session duration (seconds)', type: 'number', defaultValue: 3600, hint: 'Backend default 3600.' },
  ],
  detailTabs: [{ id: 'assignments', label: 'Assignments', render: (ctx) => <AssignmentsPanel {...ctx} /> }],
  homeCards: singleCard('Permission sets', 'permission-sets'),
}

const RAM_CONFIG: ExamServiceConfig = {
  id: 'ram',
  name: 'Resource Access Manager',
  category: 'Security',
  icon: 'share',
  description: 'Cross-account resource sharing — durable share records.',
  fidelity: 'API_EMULATED',
  fidelityFootnote: 'No second account exists, so sharing changes no access; principals are recorded names, not validated against any account.',
  banner:
    'Cross-account resource sharing — durable share records. No second account exists, so sharing changes no access; principals are recorded names, not validated against any account.',
  listSection: 'resource-shares',
  listLabel: 'Resource shares',
  listPath: 'ram/resource-shares',
  listKey: 'resource-shares',
  kindLabel: 'resource-share',
  createLabel: 'Create resource share',
  columns: [
    { header: 'Resource share', render: (r) => str(r, 'name') },
    { header: 'Resource type', render: (r) => str(r, 'resource_type') },
    {
      header: 'Principals',
      render: (r) => String(Array.isArray(r.principals) ? (r.principals as unknown[]).length : 0),
    },
    { header: 'Allow external principals', render: (r) => (r.allow_external_principals === true ? 'Yes' : 'No') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Resource Access Manager'),
  createFields: [
    {
      key: 'resource_type',
      label: 'Resource type',
      type: 'select',
      required: true,
      options: ['subnet', 'transit-gateway', 'vpc', 'prefix-list', 'route-table'],
    },
  ],
  detailTabs: [{ id: 'principals', label: 'Principals', render: (ctx) => <PrincipalsPanel {...ctx} /> }],
  homeCards: singleCard('Resource shares', 'resource-shares'),
}

const DIRECTCONNECT_CONFIG: ExamServiceConfig = {
  id: 'directconnect',
  name: 'Direct Connect',
  category: 'Networking',
  icon: 'network',
  description: 'Dedicated connections — durable records with a confirm step.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'No physical cross-connect, LOA-CFA or BGP session exists; confirming a connection is a teaching state change from REQUESTED to AVAILABLE.',
  banner:
    'Dedicated connections — durable records with a confirm step. No physical cross-connect, LOA-CFA or BGP session exists; confirming is a teaching state change.',
  listSection: 'connections',
  listLabel: 'Connections',
  listPath: 'directconnect/connections',
  listKey: 'connections',
  kindLabel: 'connection',
  createLabel: 'Create connection',
  columns: [
    { header: 'Connection', render: (r) => str(r, 'name') },
    { header: 'Location', render: (r) => str(r, 'location') },
    { header: 'Bandwidth', render: (r) => str(r, 'bandwidth') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Direct Connect'),
  createFields: [
    {
      key: 'bandwidth',
      label: 'Bandwidth',
      type: 'select',
      required: true,
      options: ['50Mbps', '100Mbps', '200Mbps', '300Mbps', '400Mbps', '500Mbps', '1Gbps', '2Gbps', '5Gbps', '10Gbps', '100Gbps'],
    },
    { key: 'location', label: 'Location', type: 'text', defaultValue: 'EqDC2 (teaching)' },
  ],
  detailTabs: [{ id: 'confirm', label: 'Confirm', render: (ctx) => <DxConfirmPanel {...ctx} /> }],
  extraTabs: [
    {
      id: 'resiliency-groups',
      label: 'Resiliency groups',
      unsupported:
        'Resiliency groups are not built — there is no Direct Connect resiliency-group API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'virtual-interfaces',
      label: 'Virtual interfaces',
      unsupported:
        'Virtual interfaces are not built — there is no Direct Connect virtual-interface API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'lags',
      label: 'LAGs',
      unsupported:
        'LAGs are not built — there is no Direct Connect LAG API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'direct-connect-gateways',
      label: 'Direct Connect gateways',
      unsupported:
        'Direct Connect gateways are not built — there is no Direct Connect gateway API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'virtual-private-gateways',
      label: 'Virtual private gateways',
      unsupported:
        'Virtual private gateways are not built in this console — they are managed in the VPC console. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'transit-gateways',
      label: 'Transit gateways',
      unsupported:
        'Transit gateways are not built in this console — they are managed in the VPC console’s Transit Gateways section. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'interconnect-multicloud',
      label: 'AWS Interconnect – multicloud',
      unsupported:
        'AWS Interconnect – multicloud is not built — there is no interconnect API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
    {
      id: 'interconnect-last-mile',
      label: 'AWS Interconnect – last mile',
      unsupported:
        'AWS Interconnect – last mile is not built — there is no interconnect API in CloudForge. In the 2026-11-22 v4 survey account Direct Connect showed an advanced-features restriction dialog, so only the nav label was recorded and no page structure was observed.',
    },
  ],
  homeCards: singleCard('Connections', 'connections'),
}

const GLOBALACCELERATOR_CONFIG: ExamServiceConfig = {
  id: 'globalaccelerator',
  name: 'Global Accelerator',
  category: 'Networking',
  icon: 'network',
  description: 'Anycast accelerators with endpoint groups and failover history.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Static IPs are teaching addresses in 192.0.2.0/24 (TEST-NET-1); no traffic is routed and no health checks run. Failover switches the recorded active endpoint group.',
  banner:
    'Anycast accelerators with endpoint groups and failover history. Static IPs are teaching addresses in 192.0.2.0/24 (TEST-NET-1); no traffic is routed and no health checks run.',
  listSection: 'accelerators',
  listLabel: 'Accelerators',
  listPath: 'globalaccelerator/accelerators',
  listKey: 'accelerators',
  kindLabel: 'accelerator',
  createLabel: 'Create accelerator',
  columns: [
    { header: 'Accelerator', render: (r) => str(r, 'name') },
    {
      header: 'Static IPs',
      render: (r) => (Array.isArray(r.static_ips) ? (r.static_ips as unknown[]).join(', ') : '—'),
    },
    { header: 'Active region', render: (r) => str(r, 'active_region') },
    { header: 'IP address type', render: (r) => str(r, 'ip_address_type') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Global Accelerator'),
  createFields: [
    { key: 'endpoint_group_region', label: 'Endpoint group region', type: 'text', required: true, placeholder: 'e.g. cf-local-1', hint: 'Required unless endpoint groups are supplied via the API.' },
    { key: 'ip_address_type', label: 'IP address type', type: 'select', options: ['IPv4', 'DUAL_STACK'], defaultValue: 'IPv4' },
  ],
  detailTabs: [{ id: 'endpoint-groups', label: 'Endpoint groups', render: (ctx) => <FailoverPanel {...ctx} /> }],
  homeCards: singleCard('Accelerators', 'accelerators'),
}

const MGN_CONFIG: ExamServiceConfig = {
  id: 'mgn',
  name: 'Application Migration',
  category: 'Migration and Transfer',
  icon: 'layers',
  description: 'Lift-and-shift source servers with a replication lifecycle — records plus a teaching state machine.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'No agent is installed and no blocks are copied: replication, test launch and cutover move the server along a teaching lifecycle (SIMULATED) so the cutover lesson can be practised end to end.',
  banner:
    'Lift-and-shift source servers with a replication lifecycle — records plus a teaching state machine. No agent is installed and no blocks are copied.',
  listSection: 'source-servers',
  listLabel: 'Source servers',
  listPath: 'mgn/source-servers',
  listKey: 'source-servers',
  kindLabel: 'source-server',
  createLabel: 'Register source server',
  columns: [
    { header: 'Source server', render: (r) => str(r, 'name') },
    { header: 'Source', render: (r) => str(r, 'source') },
    { header: 'OS', render: (r) => str(r, 'os') },
    { header: 'Replication state', render: (r) => str(r, 'replication_state') },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Application Migration'),
  createFields: [
    { key: 'source', label: 'Source', type: 'text', required: true, placeholder: 'e.g. 10.0.1.15 or web-01' },
    { key: 'os', label: 'OS', type: 'select', options: ['linux', 'windows'], defaultValue: 'linux' },
  ],
  detailTabs: [{ id: 'replication', label: 'Replication', render: (ctx) => <MgnReplicationPanel {...ctx} /> }],
  homeCards: singleCard('Source servers', 'source-servers'),
}

const COMPUTEOPTIMIZER_CONFIG: ExamServiceConfig = {
  id: 'computeoptimizer',
  name: 'Compute Optimizer',
  category: 'Management',
  icon: 'trending-up',
  description: 'Rightsizing recommendations computed from real lab state.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Recommendations come from lab profile and state only — no CloudWatch utilization exists here, so every recommendation says so and carries LOW confidence (MEDIUM for a stopped lab). Never presented as measured rightsizing.',
  banner:
    'Rightsizing recommendations computed from real lab state. There is no utilization pipeline here, so every recommendation says so and carries LOW confidence (MEDIUM for a stopped lab) — never presented as measured rightsizing.',
  listSection: 'recommendations',
  listLabel: 'Recommendations',
  listPath: 'computeoptimizer/recommendations',
  listKey: 'recommendations',
  kindLabel: 'recommendation',
  createLabel: 'Create recommendation',
  createDisabledReason:
    'Recommendations are computed from real lab state — there is no create API. Use Refresh recommendations.',
  columns: [
    { header: 'Recommendation', render: (r) => str(r, 'title', str(r, 'name')) },
    { header: 'Type', render: (r) => str(r, 'recommendation_type') },
    { header: 'Current profile', render: (r) => str(r, 'current_profile') },
    { header: 'Recommended profile', render: (r) => str(r, 'recommended_profile') },
    { header: 'Est. monthly savings', render: (r) => fmtUsd(r.estimated_monthly_savings_usd) },
    { header: 'Confidence', render: (r) => str(r, 'confidence') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Compute Optimizer'),
  createFields: [],
  detailTabs: [],
  listActions: (ctx) => (
    <AwsButton onClick={() => ctx.runAction('computeoptimizer/recommendations/refresh')} disabled={ctx.actionBusy}>
      Refresh recommendations
    </AwsButton>
  ),
  homeCards: singleCard('Recommendations', 'recommendations'),
}

const SAVINGSPLANS_CONFIG: ExamServiceConfig = {
  id: 'savingsplans',
  name: 'Savings Plans',
  category: 'Management',
  icon: 'trending-up',
  description: 'Commitment plans with utilization computed from real lab hours.',
  fidelity: 'API_EMULATED',
  fidelityFootnote:
    'Plans are records, not real AWS commitments: no money is committed and no discount is applied. Utilization is real arithmetic over lab hours at the teaching rates.',
  banner:
    'Commitment plans with utilization computed from real lab hours. Plans are records, not real AWS commitments: no money is committed and no discount is applied.',
  listSection: 'plans',
  listLabel: 'Savings plans',
  listPath: 'savingsplans/plans',
  listKey: 'plans',
  kindLabel: 'savings-plan',
  createLabel: 'Create savings plan',
  columns: [
    { header: 'Plan', render: (r) => str(r, 'name') },
    { header: 'Plan type', render: (r) => str(r, 'plan_type') },
    { header: 'Term', render: (r) => str(r, 'term') },
    { header: 'Payment', render: (r) => str(r, 'payment') },
    { header: 'Commitment ($/hr)', render: (r) => fmtUsd(r.commitment_usd_per_hour) },
    { header: 'Status', render: (r) => str(r, 'status') },
    { header: 'Created', render: (r) => fmtTime(r.created_at) },
  ],
  columnsNote: ownColumnsNote('Savings Plans'),
  createFields: [
    { key: 'plan_type', label: 'Plan type', type: 'select', required: true, options: ['COMPUTE', 'EC2'] },
    { key: 'commitment_usd_per_hour', label: 'Commitment ($/hour)', type: 'number', required: true, hint: '$0–100 per hour.' },
    { key: 'term', label: 'Term', type: 'select', options: ['ONE_YEAR', 'THREE_YEARS'], defaultValue: 'ONE_YEAR' },
    { key: 'payment', label: 'Payment', type: 'select', options: ['NO_UPFRONT', 'PARTIAL_UPFRONT', 'ALL_UPFRONT'], defaultValue: 'NO_UPFRONT' },
  ],
  detailTabs: [{ id: 'utilization', label: 'Utilization', render: (ctx) => <UtilizationPanel sel={ctx.sel} /> }],
  homeCards: singleCard('Savings plans', 'plans'),
}

/* --- Exported consoles (route registration is the coordinator's step) --- */

export function AuroraConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={AURORA_CONFIG} initialSection={initialSection} />
}
export function BackupConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={BACKUP_CONFIG} initialSection={initialSection} />
}
export function MacieConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={MACIE_CONFIG} initialSection={initialSection} />
}
export function AuditManagerConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={AUDITMANAGER_CONFIG} initialSection={initialSection} />
}
export function ArtifactConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={ARTIFACT_CONFIG} initialSection={initialSection} />
}
export function CloudFrontConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={CLOUDFRONT_CONFIG} initialSection={initialSection} />
}
export function EfsConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={EFS_CONFIG} initialSection={initialSection} />
}
export function FsxConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={FSX_CONFIG} initialSection={initialSection} />
}
export function ShieldConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={SHIELD_CONFIG} initialSection={initialSection} />
}
export function ControlTowerConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={CONTROLTOWER_CONFIG} initialSection={initialSection} />
}
export function IdentityCenterConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={IDENTITYCENTER_CONFIG} initialSection={initialSection} />
}
export function RamConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={RAM_CONFIG} initialSection={initialSection} />
}
export function DirectConnectConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={DIRECTCONNECT_CONFIG} initialSection={initialSection} />
}
export function GlobalAcceleratorConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={GLOBALACCELERATOR_CONFIG} initialSection={initialSection} />
}
export function MgnConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={MGN_CONFIG} initialSection={initialSection} />
}
export function ComputeOptimizerConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={COMPUTEOPTIMIZER_CONFIG} initialSection={initialSection} />
}
export function SavingsPlansConsole({ initialSection }: { initialSection?: string }) {
  return <ExamServiceConsole config={SAVINGSPLANS_CONFIG} initialSection={initialSection} />
}
