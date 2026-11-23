import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
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

/** Small definition list for row-detail tabs (real backend fields only). */
function Dl({ entries }: { entries: [string, string][] }) {
  return (
    <dl className="space-y-1 text-sm">
      {entries.map(([k, v]) => (
        <div key={k} className="flex gap-2">
          <dt className="w-36 shrink-0 font-medium">{k}</dt>
          <dd className="break-all">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

/**
 * Systems Manager console (Phase 6) — mirrors the AWS SSM fleet
 * manager / Run Command screens. Inventory and Run Command go
 * through the real host agent; without one the API returns an
 * honest 503 and the console says so.
 *
 * Left nav follows the public-docs reference layout (the survey
 * records SSM only as one EC2 nav item, so every layout verdict is
 * unobserved): Fleet Manager, Run Command, Session Manager, Patch
 * Manager, State Manager, Documents, Inventory, Parameter Store,
 * Automation. Only Fleet Manager, Run Command and Inventory have a
 * backend; the rest are honest UNSUPPORTED tabs. Parameter Store is
 * the next build, not a fake list — there is no parameter backend.
 */
export function SsmConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'fleet')
  const [labId, setLabId] = useState('')
  const [command, setCommand] = useState('echo hello')
  const [notice, setNotice] = useState('')
  const [instanceFilter, setInstanceFilter] = useState('')
  const [commandFilter, setCommandFilter] = useState('')
  const inventory = useQuery({ queryKey: ['ssm-inventory'], queryFn: () => request<Rec>('/api/v1/aws/ssm/inventory'), retry: 1 })
  const instances = useQuery({ queryKey: ['ssm-instances'], queryFn: () => request<Rec>('/api/v1/aws/ssm/instances'), retry: 1 })
  const instanceRows = listOf(instances.data, 'instances')
  const filteredInstances = useMemo(() => {
    const q = instanceFilter.trim().toLowerCase()
    if (q === '') return instanceRows
    return instanceRows.filter((r) => `${str(r, 'name')} ${str(r, 'lab_id')} ${str(r, 'state')} ${str(r, 'type')}`.toLowerCase().includes(q))
  }, [instanceRows, instanceFilter])
  const commands = useQuery({ queryKey: ['ssm-commands'], queryFn: () => request<Rec>('/api/v1/aws/ssm/commands'), retry: 1 })
  const commandRows = listOf(commands.data, 'commands')
  const filteredCommands = useMemo(() => {
    const q = commandFilter.trim().toLowerCase()
    if (q === '') return commandRows
    return commandRows.filter((r) => `${Array.isArray(r.command) ? (r.command as string[]).join(' ') : str(r, 'command')} ${str(r, 'lab_id')} ${str(r, 'status')} ${str(r, 'output')}`.toLowerCase().includes(q))
  }, [commandRows, commandFilter])
  const labs = useQuery({ queryKey: ['labs-list'], queryFn: () => request<Rec>('/api/v1/labs'), retry: 1 })
  const labRows = listOf(labs.data, 'labs')
  const inv = (inventory.data?.inventory ?? {}) as Record<string, unknown>
  const invKeys = Object.keys(inv)
  const run = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ssm/commands', {
        method: 'POST',
        body: { lab_id: labId, command: command.split(/\s+/).filter(Boolean) },
      }),
    onSuccess: (d) => { setNotice(`Command ${str(d, 'status')}: ${str(d, 'output').slice(0, 200)}`); void queryClient.invalidateQueries({ queryKey: ['ssm-commands'] }) },
    onError: (e: Error) => setNotice(`Run failed: ${e.message}`),
  })
  const inventoryCard = (
    <AwsCard title="Inventory (host agent)">
      {inventory.isPending && <Loading label="Loading inventory" />}
      {inventory.isError && <ErrorBox message={`Inventory unavailable: ${inventory.error.message} — start a host agent to enable SSM.`} />}
      {inventory.data && (
        <dl className="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
          {Object.entries(inv).map(([k, v]) => (
            <div key={k}>
              <dt className="text-aws-muted">{k}</dt>
              <dd className="font-mono text-xs">{String(v)}</dd>
            </div>
          ))}
        </dl>
      )}
    </AwsCard>
  )
  const instancesCard = (
    <AwsCard title={`Managed instances (${instanceRows.length})`}>
      {instances.isPending && <Loading label="Loading instances" />}
      {instances.isError && <ErrorBox message={`Instances could not be loaded (${instances.error.message}).`} />}
      <AwsTable
        columns={[
          { header: 'Instance', render: (r) => str(r, 'name') || str(r, 'id') },
          { header: 'Lab', render: (r) => str(r, 'lab_id') },
          { header: 'State', render: (r) => str(r, 'state') },
          { header: 'Type', render: (r) => str(r, 'type') },
        ]}
        rows={filteredInstances}
        emptyMessage="No managed instances — launch a lab first."
        filter={instanceFilter}
        onFilterChange={setInstanceFilter}
        filterPlaceholder="Filter instances"
        totalCount={instanceRows.length}
      />
    </AwsCard>
  )
  const runCommandCard = (
    <AwsCard title="Run Command">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <label className="text-sm">
          Lab
          <select value={labId} onChange={(e) => setLabId(e.target.value)} className={`ml-2 ${inputCls}`}>
            <option value="">(choose a lab)</option>
            {labRows.map((l) => (
              <option key={l.id} value={l.id}>{str(l, 'name') || l.id}</option>
            ))}
          </select>
        </label>
        <input value={command} onChange={(e) => setCommand(e.target.value)} placeholder="Command (space-separated)" className={`${inputCls} w-80`} aria-label="Command" />
        <AwsButton variant="primary" disabled={labId === ''} title={labId === '' ? 'Choose a lab first.' : 'Run command'} onClick={() => labId !== '' && run.mutate()}>Run command</AwsButton>
      </div>
      <p className="text-xs text-aws-muted">Only allowlisted host-agent operations run. Without a host agent the API returns 503 host_agent_unavailable.</p>
      <AwsTable
        columns={[
          { header: 'Command', render: (r) => (Array.isArray(r.command) ? (r.command as string[]).join(' ') : str(r, 'command')) },
          { header: 'Lab', render: (r) => str(r, 'lab_id') },
          { header: 'Status', render: (r) => str(r, 'status') },
          { header: 'Output', render: (r) => str(r, 'output').slice(0, 160) },
          { header: 'Error', render: (r) => str(r, 'error') },
        ]}
        rows={filteredCommands}
        emptyMessage="No commands run yet."
        filter={commandFilter}
        onFilterChange={setCommandFilter}
        filterPlaceholder="Filter commands"
        totalCount={commandRows.length}
      />
    </AwsCard>
  )
  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Systems Manager' }]}
      title="Systems Manager — Fleet & Run Command"
      description="Real inventory from the host agent and real command output from allowlisted host-agent ops."
      fidelity="PARTIAL"
      fidelityFootnote="needs a host agent · honest 503 without one"
      subNav={[
        { id: 'home', label: 'Systems Manager dashboard' },
        { id: 'fleet', label: 'Fleet Manager' },
        { id: 'run-command', label: 'Run Command' },
        { id: 'session-manager', label: 'Session Manager' },
        { id: 'patch-manager', label: 'Patch Manager' },
        { id: 'state-manager', label: 'State Manager' },
        { id: 'documents', label: 'Documents' },
        { id: 'inventory', label: 'Inventory' },
        { id: 'parameter-store', label: 'Parameter Store' },
        { id: 'automation', label: 'Automation' },
        { id: 'opscenter', label: 'OpsCenter', disabledReason: 'UNSUPPORTED — no backend in CloudForge.' },
        { id: 'explorer', label: 'Explorer', disabledReason: 'UNSUPPORTED — use Resource Explorer instead.' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Systems Manager"
          banner="Fleet Manager and Run Command run against the real host agent. Without a host agent the API returns an honest 503 and the console says so — nothing is simulated."
          actions={[
            { label: 'Run a command', primary: true, onClick: () => setSection('run-command') },
            { label: 'Open Fleet Manager', onClick: () => setSection('fleet') },
          ]}
          cards={[
            { label: 'Managed instances', count: instanceRows.length, onSelect: () => setSection('fleet'), note: 'Labs with a host agent' },
            { label: 'Commands run', count: commandRows.length, onSelect: () => setSection('run-command') },
            { label: 'Inventory keys', count: inventory.isError ? '—' : invKeys.length, onSelect: () => setSection('inventory'), note: inventory.isError ? 'Host agent unavailable (honest 503).' : 'From the host agent' },
            { label: 'Parameters', count: 0, disabledReason: 'Parameter Store is UNSUPPORTED — no parameter backend yet.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={instances.isFetching}
          health={[
            inventory.isError ? 'Inventory unavailable — start a host agent to enable SSM.' : `${invKeys.length} inventory key(s) from the host agent.`,
            'Only allowlisted host-agent operations run.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'EC2 (labs are the managed instances)', to: '/services/ec2' },
          ]}
        />
      )}
      {section === 'fleet' && (
        <>
          <Notice text={notice} />
          {inventoryCard}
          {instancesCard}
          {runCommandCard}
        </>
      )}
      {section === 'run-command' && (
        <>
          <Notice text={notice} />
          {runCommandCard}
        </>
      )}
      {section === 'inventory' && (
        <>
          <Notice text={notice} />
          {inventoryCard}
        </>
      )}
      {section === 'session-manager' && (
        <UnsupportedSection
          title="Session Manager"
          reason="Interactive shell sessions to managed instances have no backend in CloudForge — the host agent exposes inventory and allowlisted Run Command operations only, no interactive session channel."
          actionLabel="Start session"
        />
      )}
      {section === 'patch-manager' && (
        <UnsupportedSection
          title="Patch Manager"
          reason="Patch baselines and patching operations have no backend in CloudForge — there is no OS package inventory or patch schedule behind the host agent."
          actionLabel="Create patch baseline"
        />
      )}
      {section === 'state-manager' && (
        <UnsupportedSection
          title="State Manager"
          reason="State Manager associations (desired-state documents applied on a schedule) have no backend in CloudForge."
          actionLabel="Create association"
        />
      )}
      {section === 'documents' && (
        <UnsupportedSection
          title="Documents"
          reason="SSM Documents (runbooks / command documents) have no backend in CloudForge — Run Command takes a raw allowlisted command instead of a document."
          actionLabel="Create document"
        />
      )}
      {section === 'parameter-store' && (
        <UnsupportedSection
          title="Parameter Store"
          reason="Parameter Store has no backend in CloudForge yet — there is no parameter list, create or get API, so this tab lists nothing rather than showing invented parameters. It is the next SSM build (parameters would be stored encrypted like Secrets Manager values)."
          actionLabel="Create parameter"
        />
      )}
      {section === 'automation' && (
        <UnsupportedSection
          title="Automation"
          reason="Automation runbooks have no backend in CloudForge — there is no multi-step automation engine behind Run Command."
          actionLabel="Create automation"
        />
      )}
    </ServiceConsole>
  )
}

/** One-line description of each Trusted Advisor check, from the check list in the run card. */
const advisorCheckDescriptions: [RegExp, string][] = [
  [/idle/i, 'Idle labs: labs READY for more than 24 h with no resources attached. An idle lab is capacity you pay teaching cost for and do not use.'],
  [/unattached/i, 'Unattached security groups: security groups not attached to any instance or resource.'],
  [/open/i, 'Open security groups: security groups with ingress open to the world (0.0.0.0/0), e.g. open SSH.'],
  [/quota/i, 'Quota usage: active labs compared with the role limit.'],
  [/versioning|bucket/i, 'S3 buckets without versioning: buckets where versioning is not enabled.'],
]

/**
 * Trusted Advisor console (Phase 6) — mirrors the AWS Trusted
 * Advisor checks screens. Five checks run against real resource
 * state; every finding names the real resource it came from.
 *
 * The survey does not record Trusted Advisor at all, so the nav
 * and the result detail tabs below are the backend's own structure,
 * not an AWS layout.
 */
export function TrustedAdvisorConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'checks')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [detailTab, setDetailTab] = useState('details')
  const results = useQuery({ queryKey: ['advisor-results'], queryFn: () => request<Rec>('/api/v1/aws/trustedadvisor/results'), retry: 1 })
  const resultRows = listOf(results.data, 'results')
  const filteredResults = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return resultRows
    return resultRows.filter((r) => `${str(r, 'check')} ${str(r, 'severity')} ${str(r, 'status')} ${str(r, 'resource_id')} ${str(r, 'detail')}`.toLowerCase().includes(q))
  }, [resultRows, filter])
  const run = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/trustedadvisor/run', { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Advisor run complete: ${String(d.findings_created ?? d.findings ?? 0)} finding(s) created.`); void queryClient.invalidateQueries({ queryKey: ['advisor-results'] }) },
    onError: (e: Error) => setNotice(`Run failed: ${e.message}`),
  })
  const sel = resultRows.find((r) => r.id === selectedId) ?? null
  const selCheckDesc = sel
    ? advisorCheckDescriptions.find(([re]) => re.test(str(sel, 'check')))?.[1] ?? 'No description recorded for this check.'
    : ''
  return (
    <ServiceConsole
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Trusted Advisor' }]}
      title="Trusted Advisor — Checks"
      description="Real checks against real resource state: idle labs, unattached security groups, open SGs, quota usage, unversioned buckets."
      fidelity="PARTIAL"
      fidelityFootnote="5 checks · real state only"
      subNav={[
        { id: 'home', label: 'Trusted Advisor dashboard' },
        { id: 'checks', label: 'Checks' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Trusted Advisor"
          banner="Five checks run against real state: idle labs, unattached/open security groups, quota usage, S3 buckets without versioning. A check with no matching resource creates no finding."
          actions={[
            { label: 'Run checks', primary: true, onClick: () => setSection('checks') },
            { label: 'Open Cost Explorer', to: '/services/costexplorer' },
          ]}
          cards={[
            { label: 'Check results', count: resultRows.length, onSelect: () => setSection('checks') },
            { label: 'Check categories', count: '—', onSelect: () => setSection('checks'), note: 'Cost, security, fault tolerance, performance, service limits' },
            { label: 'Compute Optimizer', count: 0, disabledReason: 'Compute Optimizer is UNSUPPORTED — see its console.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={results.isFetching}
          health={[
            results.isError ? 'Check results could not be loaded.' : `${resultRows.length} result(s) listed.`,
            'One finding per match, recorded in the shared findings table.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Compute Optimizer (unsupported — exam concept)', to: '/services/computeoptimizer' },
          ]}
        />
      )}
      {section === 'checks' && (
        <>
          <Notice text={notice} />
          <AwsCard title="Checks">
            <AwsButton variant="primary" onClick={() => run.mutate()}>Run all checks</AwsButton>
            <p className="mt-2 text-xs text-aws-muted">Checks: idle labs (READY &gt;24 h, no resources), unattached security groups, open security groups, quota usage (active labs vs role limit), S3 buckets without versioning.</p>
          </AwsCard>
          {results.isPending && <Loading label="Loading results" />}
          {results.isError && <ErrorBox message={`Results could not be loaded (${results.error.message}).`} />}
          <AwsCard title={`Results (${resultRows.length})`}>
            <AwsTable
              columns={[
                { header: 'Check', render: (r) => str(r, 'check') },
                { header: 'Severity', render: (r) => str(r, 'severity') },
                { header: 'Status', render: (r) => str(r, 'status') },
                { header: 'Resource', render: (r) => str(r, 'resource_id') },
                { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 140) },
                {
                  header: 'Actions',
                  render: (r) => <AwsButton onClick={() => { setSelectedId(str(r, 'id')); setDetailTab('details') }}>Details</AwsButton>,
                },
              ]}
              rows={filteredResults}
              emptyMessage="No advisor results yet — run the checks."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter results"
              totalCount={resultRows.length}
            />
            {sel && (
              <div className="mt-3 border-t border-aws-border pt-3">
                <div className="mb-3 flex items-center justify-between gap-2">
                  <ConsoleTabs
                    tabs={[
                      { id: 'details', label: 'Details' },
                      { id: 'check', label: 'Check' },
                    ]}
                    active={detailTab}
                    onChange={setDetailTab}
                  />
                  <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelectedId('')}>
                    Close
                  </button>
                </div>
                {detailTab === 'details' && (
                  <Dl
                    entries={[
                      ['Check', str(sel, 'check')],
                      ['Severity', str(sel, 'severity')],
                      ['Status', str(sel, 'status')],
                      ['Resource', str(sel, 'resource_id')],
                      ['Detail', str(sel, 'detail')],
                      ['ID', sel.id],
                    ]}
                  />
                )}
                {detailTab === 'check' && <p className="text-sm">{selCheckDesc}</p>}
              </div>
            )}
          </AwsCard>
        </>
      )}
    </ServiceConsole>
  )
}

const BUDGET_PAGE_SIZE = 10

/**
 * Group priced usage lines by service for the Cost Explorer breakdown
 * table (survey columns: Service, Total cost, % of total cost).
 * A line with no service label falls back to its line-item name.
 */
function costBreakdown(lines: Record<string, unknown>[]): (Record<string, unknown> & { id: string })[] {
  const byService = new Map<string, number>()
  for (const l of lines) {
    const service = typeof l.service === 'string' && l.service !== '' ? l.service : typeof l.name === 'string' ? l.name : 'Other'
    byService.set(service, (byService.get(service) ?? 0) + (typeof l.cost_usd === 'number' ? l.cost_usd : 0))
  }
  return [...byService.entries()].map(([service, cost_usd]) => ({ id: `service-${service}`, service, cost_usd }))
}

/**
 * Cost Explorer console (Phase 6) — mirrors the AWS Cost Explorer
 * / Budgets screens. Costs are computed from real usage at
 * published teaching rates and are labelled educational estimates,
 * never an AWS bill.
 *
 * The Budgets table lives here and is shared with the standalone
 * Budgets console (BudgetsConsole below, route /services/budgets):
 * one table, one component, so the two URLs never drift apart.
 * Survey columns: Name, Thresholds, Budget, Amount used, Forecasted
 * amount ("Not available" — no forecasting), Current vs. budgeted,
 * Forecasted vs. budgeted.
 *
 * Left nav follows the fresh v4 survey (2026-11-22): Cost Management
 * (Cost Explorer, Free Tier, Budgets) only. Cost analysis, Cost
 * anomaly detection and Cost allocation are NOT in the new survey's
 * nav and are not kept. Cost Explorer breakdown follows the new
 * survey shape — Total plus monthly columns May–Oct 2026, Granularity
 * Monthly, Group by Service — with a Report parameters panel (Time,
 * Group by Dimension (Service), Filters, Advanced: Unblended costs,
 * Show forecasted values). CloudForge keeps current usage only, with
 * no monthly history and no forecasting, so the monthly columns read
 * "Not available" and the Total column is the real per-service total;
 * the survey's 2026-05-01–2026-10-31 range is the reference account's
 * range, not CloudForge's, and is not claimed as ours.
 *
 * Fidelity: the registry says PARTIAL and the console used to say
 * SIMULATED. PARTIAL is what is actually true — the usage lines are
 * real (lab hours, S3 bytes, messages, invocations) and only the
 * rates are teaching rates — so the console now agrees with the
 * registry.
 */
export function BudgetsListContent() {
  const queryClient = useQueryClient()
  const [budgetName, setBudgetName] = useState('')
  const [budgetLimit, setBudgetLimit] = useState(10)
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [page, setPage] = useState(0)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [detailTab, setDetailTab] = useState('details')
  const budgets = useQuery({ queryKey: ['cost-budgets'], queryFn: () => request<Rec>('/api/v1/aws/costexplorer/budgets'), retry: 1 })
  const budgetRows = listOf(budgets.data, 'budgets')
  const filteredBudgets = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return budgetRows
    return budgetRows.filter((r) => str(r, 'name').toLowerCase().includes(q))
  }, [budgetRows, filter])
  const pageCount = Math.max(1, Math.ceil(filteredBudgets.length / BUDGET_PAGE_SIZE))
  const safePage = Math.min(page, pageCount - 1)
  const pageRows = filteredBudgets.slice(safePage * BUDGET_PAGE_SIZE, (safePage + 1) * BUDGET_PAGE_SIZE)
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['cost-budgets'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/costexplorer/budgets', { method: 'POST', body: { name: budgetName.trim(), limit_usd: budgetLimit } }),
    onSuccess: () => { setNotice('Budget created.'); setBudgetName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/costexplorer/budgets/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Budget deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = budgetRows.find((b) => b.id === selectedId) ?? null
  return (
    <>
      <Notice text={notice} />
      {budgets.isPending && <Loading label="Loading budgets" />}
      {budgets.isError && <ErrorBox message={`Budgets could not be loaded (${budgets.error.message}).`} />}
      <AwsCard title="Budgets">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (budgetName.trim() !== '') create.mutate() }}
        >
          <input value={budgetName} onChange={(e) => setBudgetName(e.target.value)} placeholder="Budget name" className={inputCls} />
          <label className="text-sm">
            Limit (USD)
            <input type="number" min={0} step={0.01} value={budgetLimit} onChange={(e) => setBudgetLimit(Number(e.target.value))} className={`ml-2 w-24 ${inputCls}`} />
          </label>
          <AwsButton variant="primary" type="submit">Create budget</AwsButton>
        </form>
        {selectedIds.length > 0 && (
          <div className="mb-3 flex items-center gap-2">
            <span className="text-sm">{selectedIds.length} selected</span>
            <AwsButton
              onClick={() => {
                const ids = selectedIds
                setSelectedIds([])
                ids.forEach((id) => del.mutate(id))
              }}
            >
              Delete selected
            </AwsButton>
            <AwsButton onClick={() => setSelectedIds([])}>Clear selection</AwsButton>
          </div>
        )}
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            // The threshold is a real configured value: alert at 100% of budget. The old text presented the
            // alert *state* ("ALERT — cost has reached…") as if it were the threshold; the state now lives
            // in the row itself (suffix) and in "Current vs. budgeted".
            { header: 'Thresholds', render: (r) => (r.alert === true ? '100% of budget — reached' : '100% of budget') },
            { header: 'Budget', render: (r) => `$${Number(r.limit_usd ?? 0).toFixed(2)}` },
            { header: 'Amount used', render: (r) => `$${Number(r.current_cost_usd ?? 0).toFixed(4)}` },
            { header: 'Forecasted amount', render: () => <span title="CloudForge does not forecast costs">Not available</span> },
            { header: 'Current vs. budgeted', render: (r) => `${Number(r.usage_percent ?? 0).toFixed(1)}% of budget` },
            { header: 'Forecasted vs. budgeted', render: () => <span title="CloudForge does not forecast costs">Not available</span> },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => { setSelectedId(str(r, 'id')); setDetailTab('details') }}>Details</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={pageRows}
          emptyMessage="No budgets yet."
          filter={filter}
          onFilterChange={(v) => { setFilter(v); setPage(0) }}
          filterPlaceholder="Find a budget"
          totalCount={filteredBudgets.length}
          selectable
          selectedIds={selectedIds}
          onToggleRow={(id) => setSelectedIds((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))}
          onToggleAll={(checked) => setSelectedIds(checked ? pageRows.map((r) => r.id) : [])}
        />
        {filteredBudgets.length > BUDGET_PAGE_SIZE && (
          <div className="mt-2 flex items-center gap-2 text-sm">
            <AwsButton disabled={safePage === 0} onClick={() => setPage(safePage - 1)}>Previous</AwsButton>
            <span className="text-aws-muted">Page {safePage + 1} of {pageCount}</span>
            <AwsButton disabled={safePage >= pageCount - 1} onClick={() => setPage(safePage + 1)}>Next</AwsButton>
          </div>
        )}
        <p className="mt-2 text-xs text-aws-muted">Alert state is evaluated on every read against the current real cost. Thresholds are the alert point (100% of budget); no forecasting, no email alerts.</p>
        {sel && (
          <div className="mt-3 border-t border-aws-border pt-3">
            <div className="mb-3 flex items-center justify-between gap-2">
              <ConsoleTabs
                tabs={[
                  { id: 'details', label: 'Details' },
                  { id: 'usage', label: 'Usage' },
                ]}
                active={detailTab}
                onChange={setDetailTab}
              />
              <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelectedId('')}>
                Close
              </button>
            </div>
            {detailTab === 'details' && (
              <Dl
                entries={[
                  ['Name', str(sel, 'name')],
                  ['Budget', `$${Number(sel.limit_usd ?? 0).toFixed(2)}`],
                  ['Threshold', 'Alert at 100% of budget'],
                  ['ID', sel.id],
                ]}
              />
            )}
            {detailTab === 'usage' && (
              <Dl
                entries={[
                  ['Amount used', `$${Number(sel.current_cost_usd ?? 0).toFixed(4)}`],
                  ['Current vs. budgeted', `${Number(sel.usage_percent ?? 0).toFixed(1)}% of budget`],
                  ['Alert', sel.alert === true ? 'Reached — cost is at or over 100% of budget.' : 'Not reached.'],
                  ['Forecasted amount', 'Not available — CloudForge does not forecast costs.'],
                  ['Forecasted vs. budgeted', 'Not available — CloudForge does not forecast costs.'],
                ]}
              />
            )}
          </div>
        )}
      </AwsCard>
    </>
  )
}

/**
 * Budgets console — the Billing console's Budgets tab as its own
 * route (/services/budgets, 2026-11-22 survey: Budgets / Reports
 * tabs, Create budget button, the 8-column table). It renders the
 * same BudgetsListContent as Cost Explorer, so /services/budgets
 * and Cost Explorer show the same table, not two different pages.
 * The Reports tab has no backend (no report generation), so it is
 * an honest UNSUPPORTED tab, not a fake report list.
 */
export function BudgetsConsole() {
  const [section, setSection] = useState<string>('budgets')
  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Budgets' }]}
      title="Budgets"
      description="Budgets evaluated against the real cost model on every read. This is the Billing console's Budgets table — the same table shown inside Cost Explorer."
      fidelity="PARTIAL"
      fidelityFootnote="evaluated on every read · no forecasting · no email alerts"
      subNav={[
        { id: 'budgets', label: 'Budgets' },
        { id: 'reports', label: 'Reports' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'budgets' && <BudgetsListContent />}
      {section === 'reports' && (
        <UnsupportedSection
          title="Reports"
          reason="Budget reports (scheduled emailed / saved budget reports) have no backend in CloudForge — there is no report generation, scheduling or email delivery. The Budgets table itself is the live report."
          actionLabel="Create report"
        />
      )}
    </ServiceConsole>
  )
}

export function CostExplorerConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'costs')
  const costs = useQuery({ queryKey: ['costs'], queryFn: () => request<Rec>('/api/v1/aws/costexplorer/costs'), retry: 1 })
  const budgets = useQuery({ queryKey: ['cost-budgets'], queryFn: () => request<Rec>('/api/v1/aws/costexplorer/budgets'), retry: 1 })
  const budgetRows = listOf(budgets.data, 'budgets')
  const lines = Array.isArray(costs.data?.lines) ? (costs.data?.lines as Record<string, unknown>[]) : []
  const pricing = (costs.data?.pricing ?? {}) as Record<string, unknown>
  const breakdown = costBreakdown(lines)
  const total = Number(costs.data?.total_usd ?? 0)
  const maxCost = breakdown.reduce((m, r) => Math.max(m, Number(r.cost_usd ?? 0)), 0)
  const goToList = () => setSection('costs')
  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Cost Explorer' }]}
      title="Cost Explorer — Costs & budgets"
      description="Educational cost estimates computed from real usage. This is not an AWS bill."
      fidelity="PARTIAL"
      fidelityFootnote="educational estimates from real usage · not an AWS bill"
      subNav={[
        { id: 'home', label: 'Cost Explorer dashboard' },
        { id: 'costs', label: 'Cost Explorer', group: 'Cost Management' },
        { id: 'free-tier', label: 'Free Tier', group: 'Cost Management' },
        { id: 'budgets', label: 'Budgets', group: 'Cost Management' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Cost Explorer"
          banner="Costs are computed from real usage (lab hours, S3 bytes, SQS messages, Lambda invocations, RDS/cache hours) at published teaching rates — educational estimates, never an AWS bill."
          actions={[
            { label: 'Create budget', primary: true, onClick: goToList },
            { label: 'Open Trusted Advisor', to: '/services/trustedadvisor' },
          ]}
          cards={[
            { label: 'Cost line items', count: lines.length, onSelect: goToList, note: 'USD (educational estimates)' },
            { label: 'Budgets', count: budgetRows.length, onSelect: () => setSection('budgets'), note: 'Evaluated against the real cost model on every read' },
            { label: 'Savings Plans', count: 0, disabledReason: 'Savings Plans is UNSUPPORTED — see its console.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={costs.isFetching}
          health={[
            costs.isError ? 'Costs could not be loaded.' : `Total $${total.toFixed(4)} (educational estimate).`,
            'Budget alert / usage_percent is evaluated on every read; zero/negative limits are refused.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Budgets', to: '/services/budgets' },
            { label: 'Savings Plans (unsupported — exam concept)', to: '/services/savingsplans' },
          ]}
        />
      )}
      {section === 'costs' && (
        <>
          {costs.isPending && <Loading label="Loading costs" />}
          {costs.isError && <ErrorBox message={`Costs could not be loaded (${costs.error.message}).`} />}
          {costs.data && (
            <AwsCard title="Cost and usage">
              <div className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="border border-aws-border bg-white px-4 py-3">
                  <p className="text-xs text-aws-muted">Total cost</p>
                  <p className="mt-1 text-lg font-semibold">${total.toFixed(4)} {str(costs.data, 'currency')}</p>
                  <p className="mt-1 text-xs text-aws-muted">Educational estimate — not an AWS bill.</p>
                </div>
                <div className="border border-aws-border bg-white px-4 py-3">
                  <p className="text-xs text-aws-muted">Granularity</p>
                  <p className="mt-1 text-lg font-semibold">Monthly</p>
                  <p className="mt-1 text-xs text-aws-muted">Current usage only — no monthly history is stored.</p>
                </div>
                <div className="border border-aws-border bg-white px-4 py-3">
                  <p className="text-xs text-aws-muted">Group by</p>
                  <p className="mt-1 text-lg font-semibold">Service</p>
                  <p className="mt-1 text-xs text-aws-muted">{breakdown.length} service(s) with priced usage in this breakdown.</p>
                </div>
              </div>
              {breakdown.length > 0 && (
                <div className="mb-4">
                  <h3 className="mb-2 text-sm font-semibold">Cost by service</h3>
                  <div className="space-y-2">
                    {breakdown.map((r) => {
                      const cost = Number(r.cost_usd ?? 0)
                      const pct = total > 0 ? (cost / total) * 100 : 0
                      return (
                        <div key={r.id} className="flex items-center gap-2 text-sm">
                          <span className="w-32 shrink-0 truncate">{str(r as Rec, 'service')}</span>
                          <div className="h-4 flex-1 bg-[#f2f3f3]">
                            <div className="h-4 bg-aws-orange" style={{ width: `${maxCost > 0 ? (cost / maxCost) * 100 : 0}%` }} />
                          </div>
                          <span className="w-24 shrink-0 text-right font-mono text-xs">${cost.toFixed(4)}</span>
                          <span className="w-14 shrink-0 text-right text-xs text-aws-muted">{pct.toFixed(1)}%</span>
                        </div>
                      )
                    })}
                  </div>
                  <p className="mt-2 text-xs text-aws-muted">Bars are proportional to cost within this breakdown, grouped by service from the real priced usage lines below. No fake series — a service with no priced usage does not appear.</p>
                </div>
              )}
              <AwsTable
                columns={[
                  { header: 'Service', render: (r) => str(r as Rec, 'service') },
                  { header: 'Total', render: (r) => `$${Number((r as Rec).cost_usd ?? 0).toFixed(4)}` },
                  { header: 'May 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                  { header: 'Jun 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                  { header: 'Jul 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                  { header: 'Aug 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                  { header: 'Sep 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                  { header: 'Oct 2026', render: () => <span title="CloudForge keeps current usage only — no monthly history">Not available</span> },
                ]}
                rows={breakdown}
                emptyMessage="No billable usage yet."
              />
              <p className="mt-2 text-xs text-aws-muted">
                Cost breakdown, grouped by service (survey columns: Total, May–Oct 2026; Granularity Monthly).
                Only Total is real — it is the current per-service total from the priced usage lines below.
                The monthly columns read Not available: CloudForge keeps current usage only and stores no
                monthly history, so no month can be filled in. The survey&apos;s 2026-05-01–2026-10-31 range is
                the reference account&apos;s range, not CloudForge&apos;s. Educational estimates at teaching rates
                — not an AWS bill.
              </p>
              <details className="mt-2 text-xs">
                <summary className="cursor-pointer text-aws-link">Priced usage lines (what each cost is computed from)</summary>
                <div className="mt-2">
                  <AwsTable
                    columns={[
                      { header: 'Line item', render: (r) => str(r as Rec, 'name') },
                      { header: 'Usage', render: (r) => String(num(r as Rec, 'usage')) },
                      { header: 'Unit', render: (r) => str(r as Rec, 'unit') },
                      { header: 'Cost (USD)', render: (r) => `$${Number((r as Rec).cost_usd ?? 0).toFixed(4)}` },
                    ]}
                    rows={lines.map((l, i) => ({ ...l, id: `line-${i}` }))}
                    emptyMessage="No billable usage yet."
                  />
                </div>
              </details>
              <p className="mt-2 text-xs text-aws-muted">{str(costs.data, 'note')}</p>
              {Object.keys(pricing).length > 0 && (
                <details className="mt-2 text-xs">
                  <summary className="cursor-pointer text-aws-link">Teaching rates</summary>
                  <ul className="mt-1 list-disc pl-5">
                    {Object.entries(pricing).map(([k, v]) => (
                      <li key={k}>{k}: {String(v)}</li>
                    ))}
                  </ul>
                </details>
              )}
            </AwsCard>
          )}
          <AwsCard title="Report parameters">
            <Dl
              entries={[
                ['Time', 'Current usage period only — CloudForge keeps current usage, no stored history, so no date range can be selected.'],
                ['Group by Dimension', 'Service'],
                ['Filters', 'Service, Linked account, Region, Instance type, Usage type, Usage type group, Resource, Cost category, Tag — the survey’s filter dimensions. Only the Service grouping is supported: the breakdown above is the only cost view the cost model supports, so the other filters cannot be applied.'],
                ['Advanced — Unblended costs', 'Shown — the costs above are unblended teaching-rate estimates.'],
                ['Advanced — Show forecasted values', 'Not available — CloudForge does not forecast costs.'],
              ]}
            />
            <p className="mt-2 text-xs text-aws-muted">
              Report parameters follow the fresh v4 survey (Time, Group by Dimension (Service), Filters, Advanced:
              Unblended costs, Show forecasted values). The parameters are shown as they are, with the honest
              limits stated — nothing here is a working control for a capability that does not exist.
            </p>
          </AwsCard>
          <BudgetsListContent />
        </>
      )}
      {section === 'free-tier' && (
        <UnsupportedSection
          title="Free Tier"
          reason="Free Tier usage tracking has no backend in CloudForge — there are no free-tier allowances or limits to track against, and costs here are educational estimates from real usage at teaching rates, not an AWS bill."
          actionLabel="View Free Tier usage"
        />
      )}
      {section === 'budgets' && <BudgetsListContent />}
    </ServiceConsole>
  )
}

/**
 * Organizations console (Phase 6) — mirrors the AWS Organizations
 * screens. Organizational units with real member users (resolved
 * to their emails); children block deletion.
 *
 * The fresh v4 survey (2026-11-22) records Organizations nav behind the
 * restriction dialog — Dashboard, Invitations, Policy management — so
 * Invitations and Policy management are honest UNSUPPORTED tabs below.
 * No columns or detail tabs were recorded behind the dialog, so none
 * are invented. The unit detail tabs below are the backend's own
 * structure (Members / Details on a selected unit), not an AWS layout.
 * Service control policies are not enforced and have no editor.
 */
export function OrganizationsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'units')
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  const [memberUserId, setMemberUserId] = useState('')
  const [selected, setSelected] = useState('')
  const [detailTab, setDetailTab] = useState('members')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const units = useQuery({ queryKey: ['org-units'], queryFn: () => request<Rec>('/api/v1/aws/organizations/units'), retry: 1 })
  const unitRows = listOf(units.data, 'units')
  const filteredUnits = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return unitRows
    return unitRows.filter((r) => str(r, 'name').toLowerCase().includes(q))
  }, [unitRows, filter])
  const users = useQuery({ queryKey: ['admin-users'], queryFn: () => request<Rec>('/api/v1/admin/users'), retry: 1 })
  const userRows = listOf(users.data, 'users')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['org-units'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/organizations/units', { method: 'POST', body: { name: name.trim(), parent_id: parentId || undefined } }),
    onSuccess: () => { setNotice('Organizational unit created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const addMember = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/organizations/units/${selected}/members`, { method: 'POST', body: { user_id: memberUserId } }),
    onSuccess: () => { setNotice('Member added.'); refresh() },
    onError: (e: Error) => setNotice(`Add member failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/organizations/units/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Unit deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = unitRows.find((u) => u.id === selected)
  const members = Array.isArray(sel?.members) ? (sel?.members as string[]) : []
  const memberEmails = Array.isArray(sel?.member_emails) ? (sel?.member_emails as string[]) : []
  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Organizations' }]}
      title="Organizations — Organizational units"
      description="Multi-account/workspace structure reusing the platform's real users and roles."
      fidelity="PARTIAL"
      fidelityFootnote="real users · no AWS accounts"
      subNav={[
        { id: 'home', label: 'Organizations dashboard' },
        { id: 'units', label: 'Organizational units' },
        { id: 'invitations', label: 'Invitations' },
        { id: 'policy-management', label: 'Policy management' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Organizations"
          banner="Units and members are real platform users (learner/instructor/admin), not AWS accounts; no service control policies are enforced. Survey note: in the 2026-11-22 reference account this console was RESTRICTED (paid plan / not supported) — no layout was captured beyond the restriction dialog, so this console's layout follows public console documentation, not an observed layout. The restriction is not the design."
          actions={[
            { label: 'Create unit', primary: true, onClick: () => setSection('units') },
            { label: 'Open Control Tower (unsupported)', to: '/services/controltower' },
          ]}
          cards={[
            { label: 'Organizational units', count: unitRows.length, onSelect: () => setSection('units') },
            { label: 'Accounts (users)', count: userRows.length, onSelect: () => setSection('units'), note: 'Real platform users' },
            { label: 'Service control policies', count: 0, disabledReason: 'SCPs are not enforced in CloudForge.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={units.isFetching}
          health={[
            units.isError ? 'Units could not be loaded.' : `${unitRows.length} unit(s) listed.`,
            'Children block deletion of a unit.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — IAM track', to: '/learn' },
            { label: 'Control Tower (unsupported — exam concept)', to: '/services/controltower' },
          ]}
        />
      )}
      {section === 'units' && (
        <>
          <Notice text={notice} />
          {units.isPending && <Loading label="Loading organizational units" />}
          {units.isError && <ErrorBox message={`Units could not be loaded (${units.error.message}).`} />}
          <AwsCard title="Organizational units">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
            >
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Unit name" className={inputCls} />
              <label className="text-sm">
                Parent unit
                <select value={parentId} onChange={(e) => setParentId(e.target.value)} className={`ml-2 ${inputCls}`}>
                  <option value="">(root)</option>
                  {unitRows.map((u) => (
                    <option key={u.id} value={u.id}>{str(u, 'name')}</option>
                  ))}
                </select>
              </label>
              <AwsButton variant="primary" type="submit">Create unit</AwsButton>
            </form>
            <AwsTable
              columns={[
                { header: 'Name', render: (r) => str(r, 'name') },
                { header: 'Parent', render: (r) => str(r, 'parent_id') || '(root)' },
                { header: 'Members', render: (r) => String(Array.isArray(r.members) ? (r.members as unknown[]).length : 0) },
                {
                  header: 'Actions',
                  render: (r) => (
                    <span className="flex gap-2">
                      <AwsButton onClick={() => { setSelected(str(r, 'id')); setDetailTab('members') }}>Manage members</AwsButton>
                      <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                    </span>
                  ),
                },
              ]}
              rows={filteredUnits}
              emptyMessage="No organizational units yet."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter units"
              totalCount={unitRows.length}
            />
            <p className="mt-2 text-xs text-aws-muted">Delete is refused (409) while a unit has child units.</p>
          </AwsCard>
          {sel && (
            <AwsCard title={`Unit: ${str(sel, 'name')}`}>
              <div className="mb-3 flex items-center justify-between gap-2">
                <ConsoleTabs
                  tabs={[
                    { id: 'members', label: 'Members' },
                    { id: 'details', label: 'Details' },
                  ]}
                  active={detailTab}
                  onChange={setDetailTab}
                />
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelected('')}>
                  Close
                </button>
              </div>
              {detailTab === 'members' && (
                <div>
                  <div className="mb-3 flex flex-wrap items-center gap-2">
                    <label className="text-sm">
                      User
                      <select value={memberUserId} onChange={(e) => setMemberUserId(e.target.value)} className={`ml-2 ${inputCls}`}>
                        <option value="">(choose a user)</option>
                        {userRows.map((u) => (
                          <option key={u.id} value={u.id}>{str(u, 'email') || u.id}</option>
                        ))}
                      </select>
                    </label>
                    <AwsButton variant="primary" onClick={() => memberUserId !== '' && addMember.mutate()}>Add member</AwsButton>
                  </div>
                  <ul className="list-disc pl-5 text-sm">
                    {members.map((m, i) => (
                      <li key={m}>{memberEmails[i] ?? m}</li>
                    ))}
                    {members.length === 0 && <li className="text-aws-muted">No members yet.</li>}
                  </ul>
                </div>
              )}
              {detailTab === 'details' && (
                <Dl
                  entries={[
                    ['Name', str(sel, 'name')],
                    ['Parent', str(sel, 'parent_id') || '(root)'],
                    ['Members', String(members.length)],
                    ['ID', sel.id],
                  ]}
                />
              )}
            </AwsCard>
          )}
        </>
      )}
      {section === 'invitations' && (
        <UnsupportedSection
          title="Invitations"
          reason="Invitations have no backend in CloudForge — there are no AWS accounts to invite into an organization; members here are real platform users added directly to a unit."
          actionLabel="Invite account"
        />
      )}
      {section === 'policy-management' && (
        <UnsupportedSection
          title="Policy management"
          reason="Policy management (service control policies, backup policies, tag policies) has no backend in CloudForge — no policies of any kind are enforced on organizational units."
          actionLabel="Create policy"
        />
      )}
    </ServiceConsole>
  )
}
