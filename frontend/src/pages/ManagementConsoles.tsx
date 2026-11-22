import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
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

/**
 * Systems Manager console (Phase 6) — mirrors the AWS SSM fleet
 * manager / Run Command screens. Inventory and Run Command go
 * through the real host agent; without one the API returns an
 * honest 503 and the console says so.
 */
export function SsmConsole() {
  const queryClient = useQueryClient()
  const [labId, setLabId] = useState('')
  const [command, setCommand] = useState('echo hello')
  const [notice, setNotice] = useState('')
  const inventory = useQuery({ queryKey: ['ssm-inventory'], queryFn: () => request<Rec>('/api/v1/aws/ssm/inventory'), retry: 1 })
  const instances = useQuery({ queryKey: ['ssm-instances'], queryFn: () => request<Rec>('/api/v1/aws/ssm/instances'), retry: 1 })
  const instanceRows = listOf(instances.data, 'instances')
  const commands = useQuery({ queryKey: ['ssm-commands'], queryFn: () => request<Rec>('/api/v1/aws/ssm/commands'), retry: 1 })
  const commandRows = listOf(commands.data, 'commands')
  const labs = useQuery({ queryKey: ['labs-list'], queryFn: () => request<Rec>('/api/v1/labs'), retry: 1 })
  const labRows = listOf(labs.data, 'labs')
  const inv = (inventory.data?.inventory ?? {}) as Record<string, unknown>
  const run = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ssm/commands', {
        method: 'POST',
        body: { lab_id: labId, command: command.split(/\s+/).filter(Boolean) },
      }),
    onSuccess: (d) => { setNotice(`Command ${str(d, 'status')}: ${str(d, 'output').slice(0, 200)}`); void queryClient.invalidateQueries({ queryKey: ['ssm-commands'] }) },
    onError: (e: Error) => setNotice(`Run failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Systems Manager' }]}
        title="Systems Manager — Fleet & Run Command"
        description="Real inventory from the host agent and real command output from allowlisted host-agent ops."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="needs a host agent · honest 503 without one" />}
      />
      <Notice text={notice} />
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
          rows={instanceRows}
          emptyMessage="No managed instances — launch a lab first."
        />
      </AwsCard>
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
          <AwsButton variant="primary" onClick={() => labId !== '' && run.mutate()}>Run command</AwsButton>
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
          rows={commandRows}
          emptyMessage="No commands run yet."
        />
      </AwsCard>
    </div>
  )
}

/**
 * Trusted Advisor console (Phase 6) — mirrors the AWS Trusted
 * Advisor checks screens. Five checks run against real resource
 * state; every finding names the real resource it came from.
 */
export function TrustedAdvisorConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [notice, setNotice] = useState('')
  const results = useQuery({ queryKey: ['advisor-results'], queryFn: () => request<Rec>('/api/v1/aws/trustedadvisor/results'), retry: 1 })
  const resultRows = listOf(results.data, 'results')
  const run = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/trustedadvisor/run', { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Advisor run complete: ${String(d.findings_created ?? d.findings ?? 0)} finding(s) created.`); void queryClient.invalidateQueries({ queryKey: ['advisor-results'] }) },
    onError: (e: Error) => setNotice(`Run failed: ${e.message}`),
  })
  return (
    <ConsoleWithHome
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Trusted Advisor' }]}
      title="Trusted Advisor — Checks"
      description="Real checks against real resource state: idle labs, unattached security groups, open SGs, quota usage, unversioned buckets."
      fidelity="PARTIAL"
      fidelityFootnote="5 checks · real state only"
      homeLabel="Trusted Advisor dashboard"
      listLabel="Checks"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="Trusted Advisor"
            banner="Five checks run against real state: idle labs, unattached/open security groups, quota usage, S3 buckets without versioning. A check with no matching resource creates no finding."
            actions={[
              { label: 'Run checks', primary: true, onClick: goToList },
            { label: 'Open Cost Explorer', to: '/services/costexplorer' },
            ]}
            cards={[
              { label: 'Check results', count: resultRows.length, onSelect: goToList },
            { label: 'Check categories', count: '—', onSelect: goToList, note: 'Cost, security, fault tolerance, performance, service limits' },
            { label: 'Compute Optimizer', count: 0, disabledReason: 'Compute Optimizer is UNSUPPORTED — see its console.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={results.isFetching}
            health={[
              "results.isError ? 'Check results could not be loaded.' : `${resultRows.length} result(s) listed.`",
            "One finding per match, recorded in the shared findings table.",
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
    >
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
          ]}
          rows={resultRows}
          emptyMessage="No advisor results yet — run the checks."
        />
      </AwsCard>
    </ConsoleWithHome>
  )
}

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
 */
export function CostExplorerConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [budgetName, setBudgetName] = useState('')
  const [budgetLimit, setBudgetLimit] = useState(10)
  const [notice, setNotice] = useState('')
  const costs = useQuery({ queryKey: ['costs'], queryFn: () => request<Rec>('/api/v1/aws/costexplorer/costs'), retry: 1 })
  const budgets = useQuery({ queryKey: ['cost-budgets'], queryFn: () => request<Rec>('/api/v1/aws/costexplorer/budgets'), retry: 1 })
  const budgetRows = listOf(budgets.data, 'budgets')
  const lines = Array.isArray(costs.data?.lines) ? (costs.data?.lines as Record<string, unknown>[]) : []
  const pricing = (costs.data?.pricing ?? {}) as Record<string, unknown>
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['costs'] })
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
  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Cost Explorer' }]}
      title="Cost Explorer — Costs & budgets"
      description="Educational cost estimates computed from real usage. This is not an AWS bill."
      fidelity="SIMULATED"
      fidelityFootnote="educational estimates · not an AWS bill"
      homeLabel="Cost Explorer dashboard"
      listLabel="Costs & budgets"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="Cost Explorer"
            banner="Costs are computed from real usage (lab hours, S3 bytes, SQS messages, Lambda invocations, RDS/cache hours) at published teaching rates — educational estimates, never an AWS bill."
            actions={[
              { label: 'Create budget', primary: true, onClick: goToList },
            { label: 'Open Trusted Advisor', to: '/services/trustedadvisor' },
            ]}
            cards={[
              { label: 'Cost line items', count: lines.length, onSelect: goToList, note: 'USD (educational estimates)' },
            { label: 'Budgets', count: budgetRows.length, onSelect: goToList, note: 'Evaluated against the real cost model on every read' },
            { label: 'Savings Plans', count: 0, disabledReason: 'Savings Plans is UNSUPPORTED — see its console.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={costs.isFetching}
            health={[
              costs.isError ? 'Costs could not be loaded.' : `Total $${Number(costs.data?.total_usd ?? 0).toFixed(4)} (educational estimate).`,
            "Budget alert / usage_percent is evaluated on every read; zero/negative limits are refused.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Budgets (this console)', to: '/services/costexplorer' },
            { label: 'Savings Plans (unsupported — exam concept)', to: '/services/savingsplans' },
            ]}
          />
        )}
    >
      <Notice text={notice} />
      {costs.isPending && <Loading label="Loading costs" />}
      {costs.isError && <ErrorBox message={`Costs could not be loaded (${costs.error.message}).`} />}
      {costs.data && (
        <AwsCard title={`Current costs — total $${Number(costs.data.total_usd ?? 0).toFixed(4)} ${str(costs.data, 'currency')}`}>
          <AwsTable
            columns={[
              { header: 'Service', render: (r) => str(r as Rec, 'service') },
              { header: 'Total cost', render: (r) => `$${Number((r as Rec).cost_usd ?? 0).toFixed(4)}` },
              {
                header: '% of total cost',
                render: (r) => {
                  const total = Number(costs.data?.total_usd ?? 0)
                  return total > 0 ? `${((Number((r as Rec).cost_usd ?? 0) / total) * 100).toFixed(1)}%` : '—'
                },
              },
            ]}
            rows={costBreakdown(lines)}
            emptyMessage="No billable usage yet."
          />
          <p className="mt-2 text-xs text-aws-muted">
            Cost and usage breakdown, grouped by service from the real priced usage lines below (survey columns:
            Service, Total cost, % of total cost). Educational estimates at teaching rates — not an AWS bill.
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
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Thresholds', render: (r) => (r.alert === true ? 'ALERT — cost has reached 100% of budget' : 'Alert at 100% of budget') },
            { header: 'Budget', render: (r) => `$${Number(r.limit_usd ?? 0).toFixed(2)}` },
            { header: 'Amount used', render: (r) => `$${Number(r.current_cost_usd ?? 0).toFixed(4)}` },
            { header: 'Forecasted amount', render: () => <span title="CloudForge does not forecast costs">Not available</span> },
            { header: 'Current vs. budgeted', render: (r) => `${Number(r.usage_percent ?? 0).toFixed(1)}% of budget` },
            { header: 'Forecasted vs. budgeted', render: () => <span title="CloudForge does not forecast costs">Not available</span> },
            {
              header: 'Actions',
              render: (r) => <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>,
            },
          ]}
          rows={budgetRows}
          emptyMessage="No budgets yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Alert state is evaluated on every read against the current real cost.</p>
      </AwsCard>
    </ConsoleWithHome>
  )
}

/**
 * Organizations console (Phase 6) — mirrors the AWS Organizations
 * screens. Organizational units with real member users (resolved
 * to their emails); children block deletion.
 */
export function OrganizationsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  const [memberUserId, setMemberUserId] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const units = useQuery({ queryKey: ['org-units'], queryFn: () => request<Rec>('/api/v1/aws/organizations/units'), retry: 1 })
  const unitRows = listOf(units.data, 'units')
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
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'Organizations' }]}
      title="Organizations — Organizational units"
      description="Multi-account/workspace structure reusing the platform's real users and roles."
      fidelity="PARTIAL"
      fidelityFootnote="real users · no AWS accounts"
      homeLabel="Organizations dashboard"
      listLabel="Organizational units"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="Organizations"
            banner="Units and members are real platform users (learner/instructor/admin), not AWS accounts; no service control policies are enforced. Survey note: in the 2026-11-22 reference account this console was RESTRICTED (paid plan / not supported) — no layout was captured beyond the restriction dialog, so this console's layout follows public console documentation, not an observed layout. The restriction is not the design."
            actions={[
              { label: 'Create unit', primary: true, onClick: goToList },
            { label: 'Open Control Tower (unsupported)', to: '/services/controltower' },
            ]}
            cards={[
              { label: 'Organizational units', count: unitRows.length, onSelect: goToList },
            { label: 'Accounts (users)', count: userRows.length, onSelect: goToList, note: 'Real platform users' },
            { label: 'Service control policies', count: 0, disabledReason: 'SCPs are not enforced in CloudForge.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={units.isFetching}
            health={[
              "units.isError ? 'Units could not be loaded.' : `${unitRows.length} unit(s) listed.`",
            "Children block deletion of a unit.",
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
    >
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
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Manage members</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={unitRows}
          emptyMessage="No organizational units yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Delete is refused (409) while a unit has child units.</p>
      </AwsCard>
      {sel && (
        <AwsCard title={`Members: ${str(sel, 'name')}`}>
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
        </AwsCard>
      )}
    </ConsoleWithHome>
  )
}
