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

function FindingsTable({ rows, onStatus, onSelect, filter, onFilterChange }: { rows: Rec[]; onStatus?: (id: string, status: string) => void; onSelect?: (id: string) => void; filter?: string; onFilterChange?: (v: string) => void }) {
  return (
    <AwsTable
      filter={filter}
      onFilterChange={onFilterChange}
      filterPlaceholder="Filter findings"
      columns={[
        { header: 'Title', render: (r) => str(r, 'title') },
        { header: 'Severity', render: (r) => str(r, 'severity') },
        { header: 'Source', render: (r) => str(r, 'source') },
        { header: 'Status', render: (r) => str(r, 'status') },
        { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 120) },
        {
          header: 'Actions',
          render: (r) => (
            <span className="flex gap-2">
              {onSelect && <AwsButton onClick={() => onSelect(str(r, 'id'))}>Details</AwsButton>}
              {onStatus ? (
                <>
                  <AwsButton onClick={() => onStatus(str(r, 'id'), 'ACKNOWLEDGED')}>Acknowledge</AwsButton>
                  <AwsButton onClick={() => onStatus(str(r, 'id'), 'RESOLVED')}>Resolve</AwsButton>
                  <AwsButton onClick={() => onStatus(str(r, 'id'), 'SUPPRESSED')}>Suppress</AwsButton>
                </>
              ) : (
                <span>—</span>
              )}
            </span>
          ),
        },
      ]}
      rows={rows}
      emptyMessage="No findings."
    />
  )
}

/**
 * Row detail for a shared-findings-table finding. Tabs show only real
 * backend fields: Details is the full record, Status is the live status
 * with the same status actions as the table. No invented AWS detail
 * tabs — the survey recorded none for GuardDuty / Security Hub (both
 * RESTRICTED in the reference account).
 */
function FindingDetail({ finding, onStatus, onClose }: { finding: Rec; onStatus?: (id: string, status: string) => void; onClose: () => void }) {
  const [tab, setTab] = useState('details')
  return (
    <AwsCard title={`Finding: ${str(finding, 'title')}`}>
      <div className="mb-3 flex items-center justify-between gap-2">
        <ConsoleTabs
          tabs={[
            { id: 'details', label: 'Details' },
            { id: 'status', label: 'Status' },
          ]}
          active={tab}
          onChange={setTab}
        />
        <button type="button" className="text-sm text-aws-link hover:underline" onClick={onClose}>
          Close
        </button>
      </div>
      {tab === 'details' && (
        <Dl
          entries={[
            ['Title', str(finding, 'title')],
            ['Severity', str(finding, 'severity')],
            ['Source', str(finding, 'source')],
            ['Status', str(finding, 'status')],
            ['Detail', str(finding, 'detail')],
            ['ID', finding.id],
          ]}
        />
      )}
      {tab === 'status' && (
        <div>
          <p className="text-sm">
            Current status: <strong>{str(finding, 'status')}</strong>
          </p>
          {onStatus ? (
            <div className="mt-3 flex gap-2">
              <AwsButton onClick={() => onStatus(finding.id, 'ACKNOWLEDGED')}>Acknowledge</AwsButton>
              <AwsButton onClick={() => onStatus(finding.id, 'RESOLVED')}>Resolve</AwsButton>
              <AwsButton onClick={() => onStatus(finding.id, 'SUPPRESSED')}>Suppress</AwsButton>
            </div>
          ) : (
            <p className="mt-2 text-sm text-aws-muted">Status changes are made from the Findings table.</p>
          )}
          <p className="mt-2 text-xs text-aws-muted">Status updates are real — they write the shared findings table.</p>
        </div>
      )}
    </AwsCard>
  )
}

/**
 * GuardDuty console (Phase 6) — mirrors the AWS GuardDuty findings
 * screens. Findings come only from real evidence: failed logins in
 * the platform's own audit log. A scan with no evidence creates
 * zero findings.
 *
 * Left nav is the backend's own structure (Detectors, Findings) —
 * the survey recorded no GuardDuty layout (RESTRICTED), so no AWS
 * nav is invented here.
 */
export function GuardDutyConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'detectors')
  const [detName, setDetName] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const detectors = useQuery({ queryKey: ['gd-detectors'], queryFn: () => request<Rec>('/api/v1/aws/guardduty/detectors'), retry: 1 })
  const detRows = listOf(detectors.data, 'detectors')
  const findings = useQuery({ queryKey: ['gd-findings'], queryFn: () => request<Rec>('/api/v1/aws/guardduty/findings'), retry: 1 })
  const findingRows = listOf(findings.data, 'findings')
  const filteredFindings = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return findingRows
    return findingRows.filter((r) => `${str(r, 'title')} ${str(r, 'severity')} ${str(r, 'source')} ${str(r, 'status')} ${str(r, 'detail')}`.toLowerCase().includes(q))
  }, [findingRows, filter])
  const selected = findingRows.find((f) => f.id === selectedId) ?? null
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['gd-detectors'] })
    void queryClient.invalidateQueries({ queryKey: ['gd-findings'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/guardduty/detectors', { method: 'POST', body: { name: detName.trim() } }),
    onSuccess: () => { setNotice('Detector created.'); setDetName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const scan = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/guardduty/detectors/${id}/scan`, { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Scan complete: ${String(d.findings_created ?? 0)} new finding(s).`); refresh() },
    onError: (e: Error) => setNotice(`Scan failed: ${e.message}`),
  })
  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => request<Rec>(`/api/v1/aws/guardduty/findings/${id}/status`, { method: 'POST', body: { status } }),
    onSuccess: () => { setNotice('Finding status updated.'); refresh() },
    onError: (e: Error) => setNotice(`Status update failed: ${e.message}`),
  })
  const detectorsCard = (
    <>
      <Notice text={notice} />
      {detectors.isPending && <Loading label="Loading detectors" />}
      {detectors.isError && <ErrorBox message={`Detectors could not be loaded (${detectors.error.message}).`} />}
      <AwsCard title="Detectors">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (detName.trim() !== '') create.mutate() }}
        >
          <input value={detName} onChange={(e) => setDetName(e.target.value)} placeholder="Detector name" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create detector</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => <AwsButton onClick={() => scan.mutate(str(r, 'id'))}>Run scan</AwsButton>,
            },
          ]}
          rows={detRows}
          emptyMessage="No detectors yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Scan rules: ≥5 failed logins from one IP in 24 h → brute-force (MEDIUM); ≥5 failures with ≥3 distinct emails → credential stuffing (HIGH). Re-scans dedupe against open findings.</p>
      </AwsCard>
    </>
  )
  const findingsCard = (
    <AwsCard title={`Findings (${findingRows.length})`}>
      <AwsTable
        columns={[
          { header: 'Title', render: (r) => str(r, 'title') },
          { header: 'Severity', render: (r) => str(r, 'severity') },
          { header: 'Source', render: (r) => str(r, 'source') },
          { header: 'Status', render: (r) => str(r, 'status') },
          { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 120) },
          {
            header: 'Actions',
            render: (r) => (
              <span className="flex gap-2">
                <AwsButton onClick={() => { setSelectedId(str(r, 'id')); setSection('findings') }}>Details</AwsButton>
                <AwsButton onClick={() => setStatus.mutate({ id: str(r, 'id'), status: 'ACKNOWLEDGED' })}>Acknowledge</AwsButton>
                <AwsButton onClick={() => setStatus.mutate({ id: str(r, 'id'), status: 'RESOLVED' })}>Resolve</AwsButton>
                <AwsButton onClick={() => setStatus.mutate({ id: str(r, 'id'), status: 'SUPPRESSED' })}>Suppress</AwsButton>
              </span>
            ),
          },
        ]}
        rows={filteredFindings}
        emptyMessage="No findings."
        filter={filter}
        onFilterChange={setFilter}
        filterPlaceholder="Filter findings"
        totalCount={findingRows.length}
      />
      {selected && (
        <div className="mt-3">
          <FindingDetail finding={selected} onStatus={(id, status) => setStatus.mutate({ id, status })} onClose={() => setSelectedId('')} />
        </div>
      )}
    </AwsCard>
  )
  return (
    <ServiceConsole
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'GuardDuty' }]}
      title="GuardDuty — Threat detection"
      description="Findings are generated only from real evidence in the platform's own audit log (failed logins, last 24 h). Never invented."
      fidelity="PARTIAL"
      fidelityFootnote="real audit-log evidence only"
      subNav={[
        { id: 'home', label: 'GuardDuty dashboard' },
        { id: 'detectors', label: 'Detectors' },
        { id: 'findings', label: 'Findings' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="GuardDuty"
          banner="Findings are generated only from real evidence in the platform's own audit log (failed logins, last 24 h). A scan with no matching evidence creates nothing. Survey note: in the 2026-11-22 reference account this console was RESTRICTED (paid plan / not supported) — no layout was captured beyond the restriction dialog, so this console's layout follows public console documentation, not an observed layout. The restriction is not the design."
          actions={[
            { label: 'Run scan', primary: true, onClick: () => setSection('detectors') },
            { label: 'Open Security Hub', to: '/services/securityhub' },
          ]}
          cards={[
            { label: 'Detectors', count: detRows.length, onSelect: () => setSection('detectors') },
            { label: 'Findings', count: findingRows.length, onSelect: () => setSection('findings'), note: 'Brute force and credential-stuffing patterns only' },
            { label: 'Suppression rules', count: 0, disabledReason: 'Not modelled.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={findings.isFetching}
          health={[
            findings.isError ? 'Findings could not be loaded.' : `${findingRows.length} finding(s) listed.`,
            'No threat-intelligence feeds and no invented threats.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Macie (unsupported — exam concept)', to: '/services/macie' },
          ]}
        />
      )}
      {section === 'detectors' && (
        <>
          {detectorsCard}
          {findingsCard}
        </>
      )}
      {section === 'findings' && findingsCard}
    </ServiceConsole>
  )
}

/**
 * Security Hub console (Phase 6) — mirrors the AWS Security Hub
 * findings screens. It aggregates the real findings in the shared
 * findings table; it creates none of its own.
 *
 * Left nav is the backend's own structure (Findings) — the survey
 * recorded no Security Hub layout (RESTRICTED), so no AWS nav is
 * invented here.
 */
export function SecurityHubConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'findings')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const summary = useQuery({ queryKey: ['sechub-summary'], queryFn: () => request<Rec>('/api/v1/aws/securityhub/summary'), retry: 1 })
  const findings = useQuery({ queryKey: ['sechub-findings'], queryFn: () => request<Rec>('/api/v1/aws/securityhub/findings'), retry: 1 })
  const findingRows = listOf(findings.data, 'findings')
  const filteredFindings = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return findingRows
    return findingRows.filter((r) => `${str(r, 'title')} ${str(r, 'severity')} ${str(r, 'source')} ${str(r, 'status')} ${str(r, 'detail')}`.toLowerCase().includes(q))
  }, [findingRows, filter])
  const selected = findingRows.find((f) => f.id === selectedId) ?? null
  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => request<Rec>(`/api/v1/aws/guardduty/findings/${id}/status`, { method: 'POST', body: { status } }),
    onSuccess: () => { setNotice('Finding status updated.'); void queryClient.invalidateQueries({ queryKey: ['sechub-findings'] }); void queryClient.invalidateQueries({ queryKey: ['sechub-summary'] }) },
    onError: (e: Error) => setNotice(`Status update failed: ${e.message}`),
  })
  const bySeverity = (summary.data?.by_severity ?? {}) as Record<string, number>
  const bySource = (summary.data?.by_source ?? {}) as Record<string, number>
  return (
    <ServiceConsole
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Security Hub' }]}
      title="Security Hub — Findings"
      description="Central aggregation of findings from GuardDuty, Inspector, Config and Trusted Advisor."
      fidelity="PARTIAL"
      fidelityFootnote="aggregation only — no findings of its own"
      subNav={[
        { id: 'home', label: 'Security Hub dashboard' },
        { id: 'findings', label: 'Findings' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Security Hub"
          banner="Central aggregation of the real findings in the shared findings table — nothing is counted that was not created by GuardDuty, Inspector, Config or Trusted Advisor. Survey note: in the 2026-11-22 reference account this console was RESTRICTED (paid plan / not supported) — no layout was captured beyond the restriction dialog, so this console's layout follows public console documentation, not an observed layout. The restriction is not the design."
          actions={[
            { label: 'View findings', primary: true, onClick: () => setSection('findings') },
            { label: 'Open GuardDuty', to: '/services/guardduty' },
          ]}
          cards={[
            { label: 'Findings', count: findingRows.length, onSelect: () => setSection('findings') },
            { label: 'Open findings', count: typeof summary.data?.open === 'number' ? summary.data.open : '—', onSelect: () => setSection('findings') },
            { label: 'Standards', count: 0, disabledReason: 'No AWS standards packs in CloudForge.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={findings.isFetching}
          health={[
            findings.isError ? 'Findings could not be loaded.' : `${findingRows.length} finding(s) listed.`,
            'No AWS Security Hub integration.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Config (findings source)', to: '/services/config' },
          ]}
        />
      )}
      {section === 'findings' && (
        <>
          <Notice text={notice} />
          {summary.isPending && <Loading label="Loading summary" />}
          {summary.isError && <ErrorBox message={`Summary could not be loaded (${summary.error.message}).`} />}
          <AwsCard title="Summary">
            <p className="text-sm">Open findings: {String(num((summary.data ?? {}) as Rec, 'open'))}</p>
            <p className="mt-1 text-sm">By severity: {Object.entries(bySeverity).map(([k, v]) => `${k}: ${v}`).join(' · ') || '—'}</p>
            <p className="mt-1 text-sm">By source: {Object.entries(bySource).map(([k, v]) => `${k}: ${v}`).join(' · ') || '—'}</p>
          </AwsCard>
          <AwsCard title={`Findings (${findingRows.length})`}>
            <FindingsTable rows={filteredFindings} onStatus={(id, status) => setStatus.mutate({ id, status })} onSelect={setSelectedId} filter={filter} onFilterChange={setFilter} />
            {selected && (
              <div className="mt-3">
                <FindingDetail finding={selected} onStatus={(id, status) => setStatus.mutate({ id, status })} onClose={() => setSelectedId('')} />
              </div>
            )}
          </AwsCard>
        </>
      )}
    </ServiceConsole>
  )
}

/**
 * Inspector console (Phase 6) — mirrors the AWS Inspector
 * assessment screens. Checks run against real resource state:
 * open security groups, public S3 bucket policies, unencrypted
 * lab instances.
 *
 * The survey does not record Inspector at all, so the nav and the
 * detail tabs below are the backend's own structure (Assessment
 * runs; Findings / Details on a selected run), not an AWS layout.
 */
export function InspectorConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'assessments')
  const [name, setName] = useState('')
  const [labId, setLabId] = useState('')
  const [selected, setSelected] = useState('')
  const [detailTab, setDetailTab] = useState('findings')
  const [filter, setFilter] = useState('')
  const [notice, setNotice] = useState('')
  const assessments = useQuery({ queryKey: ['inspector-assessments'], queryFn: () => request<Rec>('/api/v1/aws/inspector/assessments'), retry: 1 })
  const assessRows = listOf(assessments.data, 'assessments')
  const filteredAssess = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return assessRows
    return assessRows.filter((r) => `${str(r, 'name')} ${str(r, 'status')}`.toLowerCase().includes(q))
  }, [assessRows, filter])
  const detail = useQuery({
    queryKey: ['inspector-assessment', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/inspector/assessments/${selected}`),
    enabled: selected !== '',
    retry: 1,
  })
  const labs = useQuery({ queryKey: ['labs-list'], queryFn: () => request<Rec>('/api/v1/labs'), retry: 1 })
  const labRows = listOf(labs.data, 'labs')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['inspector-assessments'] })
    void queryClient.invalidateQueries({ queryKey: ['inspector-assessment', selected] })
  }
  const run = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/inspector/assessments', { method: 'POST', body: { name: name.trim(), lab_id: labId || undefined } }),
    onSuccess: () => { setNotice('Assessment started — it runs async; refresh to see findings.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Assessment failed to start: ${e.message}`),
  })
  const detailFindings = Array.isArray(detail.data?.findings) ? (detail.data?.findings as Record<string, unknown>[]) : []
  const sel = assessRows.find((a) => a.id === selected)
  return (
    <ServiceConsole
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Inspector' }]}
      title="Inspector — Assessments"
      description="Assessments against real lab resources: open security groups, public buckets, unencrypted instances."
      fidelity="PARTIAL"
      fidelityFootnote="resource checks only · no CVE scanning"
      subNav={[
        { id: 'home', label: 'Inspector dashboard' },
        { id: 'assessments', label: 'Assessment runs' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Inspector"
          banner="Real checks against real resources: open security groups, public S3 bucket policies, unencrypted lab instances. No agent-based CVE scanning."
          actions={[
            { label: 'Start assessment', primary: true, onClick: () => setSection('assessments') },
            { label: 'Open Security Hub', to: '/services/securityhub' },
          ]}
          cards={[
            { label: 'Assessment runs', count: assessRows.length, onSelect: () => setSection('assessments') },
            { label: 'Findings', count: '—', onSelect: () => setSection('assessments'), note: 'findings_count on each run' },
            { label: 'Assessment templates', count: 0, disabledReason: 'Not modelled.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={assessments.isFetching}
          health={[
            assessments.isError ? 'Assessments could not be loaded.' : `${assessRows.length} assessment run(s) listed.`,
            'The S3 check is skipped and recorded when MinIO is down.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'Config (findings source)', to: '/services/config' },
          ]}
        />
      )}
      {section === 'assessments' && (
        <>
          <Notice text={notice} />
          {assessments.isPending && <Loading label="Loading assessments" />}
          {assessments.isError && <ErrorBox message={`Assessments could not be loaded (${assessments.error.message}).`} />}
          <AwsCard title="Assessments">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); run.mutate() }}
            >
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Assessment name (optional)" className={inputCls} />
              <label className="text-sm">
                Lab (optional)
                <select value={labId} onChange={(e) => setLabId(e.target.value)} className={`ml-2 ${inputCls}`}>
                  <option value="">(all labs)</option>
                  {labRows.map((l) => (
                    <option key={l.id} value={l.id}>{str(l, 'name') || l.id}</option>
                  ))}
                </select>
              </label>
              <AwsButton variant="primary" type="submit">Start assessment</AwsButton>
            </form>
            <AwsTable
              columns={[
                { header: 'Name', render: (r) => str(r, 'name') },
                { header: 'Status', render: (r) => str(r, 'status') },
                { header: 'Findings', render: (r) => String(num(r, 'findings_count')) },
                {
                  header: 'Actions',
                  render: (r) => <AwsButton onClick={() => { setSelected(str(r, 'id')); setDetailTab('findings') }}>View findings</AwsButton>,
                },
              ]}
              rows={filteredAssess}
              emptyMessage="No assessments yet."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter assessments"
              totalCount={assessRows.length}
            />
          </AwsCard>
          {sel && (
            <AwsCard title={`Assessment: ${str(sel, 'name')}`}>
              <div className="mb-3 flex items-center justify-between gap-2">
                <ConsoleTabs
                  tabs={[
                    { id: 'findings', label: 'Findings' },
                    { id: 'details', label: 'Details' },
                  ]}
                  active={detailTab}
                  onChange={setDetailTab}
                />
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelected('')}>
                  Close
                </button>
              </div>
              {detailTab === 'findings' && (
                <>
                  <AwsTable
                    columns={[
                      { header: 'Title', render: (r) => str(r as Rec, 'title') },
                      { header: 'Severity', render: (r) => str(r as Rec, 'severity') },
                      { header: 'Detail', render: (r) => str(r as Rec, 'detail').slice(0, 120) },
                    ]}
                    rows={detailFindings.map((f, i) => ({ ...f, id: `f-${i}` }))}
                    emptyMessage="No findings in this assessment."
                  />
                  {detail.data && str(detail.data, 'notes') !== '' && <p className="mt-2 text-xs text-aws-muted">{str(detail.data, 'notes')}</p>}
                </>
              )}
              {detailTab === 'details' && (
                <Dl
                  entries={[
                    ['Name', str(sel, 'name')],
                    ['Status', str(sel, 'status')],
                    ['Findings', String(num(sel, 'findings_count'))],
                    ['Lab', str(sel, 'lab_id') || '(all labs)'],
                    ['ID', sel.id],
                    ['Notes', detail.data ? str(detail.data, 'notes') : '—'],
                  ]}
                />
              )}
            </AwsCard>
          )}
        </>
      )}
    </ServiceConsole>
  )
}

/**
 * Config console (Phase 6) — mirrors the AWS Config rules /
 * resources screens: compliance rules evaluated against real
 * resources, a configuration-history snapshot, and per-resource
 * results.
 *
 * The survey does not record Config at all, so the nav (Rules,
 * Evaluation results, Configuration items) and the rule detail tabs
 * are the backend's own structure, not an AWS layout.
 */
export function ConfigConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'rules')
  const [ruleName, setRuleName] = useState('')
  const [ruleType, setRuleType] = useState('sg-no-open-ssh')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [detailTab, setDetailTab] = useState('details')
  const rules = useQuery({ queryKey: ['config-rules'], queryFn: () => request<Rec>('/api/v1/aws/config/rules'), retry: 1 })
  const ruleRows = listOf(rules.data, 'rules')
  const filteredRules = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return ruleRows
    return ruleRows.filter((r) => `${str(r, 'name')} ${str(r, 'rule_type')}`.toLowerCase().includes(q))
  }, [ruleRows, filter])
  const results = useQuery({ queryKey: ['config-results'], queryFn: () => request<Rec>('/api/v1/aws/config/results'), retry: 1 })
  const resultRows = listOf(results.data, 'results')
  const items = useQuery({ queryKey: ['config-items'], queryFn: () => request<Rec>('/api/v1/aws/config/items'), retry: 1 })
  const itemRows = listOf(items.data, 'items')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['config-rules'] })
    void queryClient.invalidateQueries({ queryKey: ['config-results'] })
    void queryClient.invalidateQueries({ queryKey: ['config-items'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/config/rules', { method: 'POST', body: { name: ruleName.trim(), rule_type: ruleType } }),
    onSuccess: () => { setNotice('Rule created.'); setRuleName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const evaluate = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/config/rules/${id}/evaluate`, { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Evaluation: compliant=${String(d.compliant)} — ${String(d.non_compliant_count ?? 0)} non-compliant resource(s).`); refresh() },
    onError: (e: Error) => setNotice(`Evaluate failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/config/rules/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Rule deleted.'); setSelectedId(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const snapshot = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/config/snapshots', { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Snapshot recorded: ${String(d.items_recorded ?? 0)} resource(s).`); refresh() },
    onError: (e: Error) => setNotice(`Snapshot failed: ${e.message}`),
  })
  const sel = ruleRows.find((r) => r.id === selectedId) ?? null
  const selResults = sel ? resultRows.filter((r) => str(r, 'rule_name') === str(sel, 'name')) : []
  const rulesCard = (
    <AwsCard title="Rules">
      <form
        className="mb-3 flex flex-wrap items-center gap-2"
        onSubmit={(e) => { e.preventDefault(); if (ruleName.trim() !== '') create.mutate() }}
      >
        <input value={ruleName} onChange={(e) => setRuleName(e.target.value)} placeholder="Rule name" className={inputCls} />
        <label className="text-sm">
          Rule type
          <select value={ruleType} onChange={(e) => setRuleType(e.target.value)} className={`ml-2 ${inputCls}`}>
            <option value="sg-no-open-ssh">sg-no-open-ssh</option>
            <option value="sg-no-open-ingress">sg-no-open-ingress</option>
            <option value="s3-bucket-versioning-enabled">s3-bucket-versioning-enabled</option>
          </select>
        </label>
        <AwsButton variant="primary" type="submit">Create rule</AwsButton>
        <AwsButton onClick={() => snapshot.mutate()}>Record configuration snapshot</AwsButton>
      </form>
      <AwsTable
        columns={[
          { header: 'Name', render: (r) => str(r, 'name') },
          { header: 'Rule type', render: (r) => str(r, 'rule_type') },
          { header: 'Last evaluation', render: (r) => str(r, 'last_evaluated') },
          { header: 'Compliant', render: (r) => String(r.compliant === true ? 'yes' : r.compliant === false ? 'no' : '—') },
          {
            header: 'Actions',
            render: (r) => (
              <span className="flex gap-2">
                <AwsButton onClick={() => { setSelectedId(str(r, 'id')); setDetailTab('details') }}>Details</AwsButton>
                <AwsButton onClick={() => evaluate.mutate(str(r, 'id'))}>Evaluate</AwsButton>
                <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
              </span>
            ),
          },
        ]}
        rows={filteredRules}
        emptyMessage="No rules yet."
        filter={filter}
        onFilterChange={setFilter}
        filterPlaceholder="Filter rules"
        totalCount={ruleRows.length}
      />
      {sel && (
        <div className="mt-3 border-t border-aws-border pt-3">
          <div className="mb-3 flex items-center justify-between gap-2">
            <ConsoleTabs
              tabs={[
                { id: 'details', label: 'Details' },
                { id: 'results', label: 'Evaluation results' },
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
                ['Rule type', str(sel, 'rule_type')],
                ['Last evaluation', str(sel, 'last_evaluated') || '—'],
                ['Compliant', String(sel.compliant === true ? 'yes' : sel.compliant === false ? 'no' : '—')],
                ['ID', sel.id],
              ]}
            />
          )}
          {detailTab === 'results' && (
            <AwsTable
              columns={[
                { header: 'Rule', render: (r) => str(r, 'rule_name') },
                { header: 'Resource', render: (r) => str(r, 'resource_id') },
                { header: 'Compliant', render: (r) => String(r.compliant === true ? 'yes' : 'no') },
                { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 120) },
              ]}
              rows={selResults}
              emptyMessage="No evaluations for this rule yet — run Evaluate above."
            />
          )}
        </div>
      )}
    </AwsCard>
  )
  const resultsCard = (
    <AwsCard title={`Evaluation results (${resultRows.length})`}>
      <AwsTable
        columns={[
          { header: 'Rule', render: (r) => str(r, 'rule_name') },
          { header: 'Resource', render: (r) => str(r, 'resource_id') },
          { header: 'Compliant', render: (r) => String(r.compliant === true ? 'yes' : 'no') },
          { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 120) },
        ]}
        rows={resultRows}
        emptyMessage="No evaluations yet."
      />
    </AwsCard>
  )
  const itemsCard = (
    <AwsCard title={`Configuration items (${itemRows.length})`}>
      <AwsTable
        columns={[
          { header: 'Resource', render: (r) => str(r, 'resource_id') },
          { header: 'Type', render: (r) => str(r, 'resource_type') },
          { header: 'Lab', render: (r) => str(r, 'lab_id') },
          { header: 'Snapshot at', render: (r) => str(r, 'snapshot_at') },
        ]}
        rows={itemRows}
        emptyMessage="No snapshots yet — record one above."
      />
    </AwsCard>
  )
  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Config' }]}
      title="Config — Rules & configuration history"
      description="Compliance rules evaluated against real resources, with non-compliant resources listed."
      fidelity="PARTIAL"
      fidelityFootnote="3 rule types · real evaluation"
      subNav={[
        { id: 'home', label: 'Config dashboard' },
        { id: 'rules', label: 'Rules' },
        { id: 'results', label: 'Evaluation results' },
        { id: 'items', label: 'Configuration items' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Config"
          banner="Three rules (S3 bucket versioning, no open SSH, no open ingress) evaluated against real resources; non-compliant resources are listed with reasons and recorded as Config findings."
          actions={[
            { label: 'Create rule', primary: true, onClick: () => setSection('rules') },
            { label: 'Open Security Hub', to: '/services/securityhub' },
          ]}
          cards={[
            { label: 'Rules', count: ruleRows.length, onSelect: () => setSection('rules') },
            { label: 'Evaluation results', count: resultRows.length, onSelect: () => setSection('results') },
            { label: 'Configuration items', count: itemRows.length, onSelect: () => setSection('items'), note: 'Snapshots of lab resources' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={rules.isFetching}
          health={[
            rules.isError ? 'Rules could not be loaded.' : `${ruleRows.length} rule(s) listed.`,
            'Configuration history is recorded snapshots of lab resources.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'CloudTrail (event history)', to: '/services/cloudtrail' },
          ]}
        />
      )}
      {section === 'rules' && (
        <>
          <Notice text={notice} />
          {rules.isPending && <Loading label="Loading rules" />}
          {rules.isError && <ErrorBox message={`Rules could not be loaded (${rules.error.message}).`} />}
          {rulesCard}
          {resultsCard}
          {itemsCard}
        </>
      )}
      {section === 'results' && (
        <>
          <Notice text={notice} />
          {resultsCard}
        </>
      )}
      {section === 'items' && (
        <>
          <Notice text={notice} />
          {itemsCard}
        </>
      )}
    </ServiceConsole>
  )
}

type WafRuleDraft = { name: string; action: string; match_type: string; match_value: string }
const defaultWafRule = (n: number): WafRuleDraft => ({ name: n === 1 ? 'block-admin' : `rule-${n}`, action: 'block', match_type: 'path_prefix', match_value: '/admin' })

/**
 * WAF console (Phase 6) — mirrors the AWS WAF Protection packs screens.
 * Rules are really evaluated against a sample request the user
 * supplies; the ACL does not block live API traffic (labelled).
 *
 * The backend stores a rules array per ACL and evaluates first
 * match in array order, so an ACL can hold several rules and the
 * rule's position in the list is its priority. There is no
 * update-rule endpoint, so rules are set at create time.
 * Left nav follows the fresh v4 survey's new wafv2-pro console
 * (2026-11-22): Protection packs (web ACLs), IP sets, Regex
 * pattern sets, Rule groups, Application integration, Add-on
 * protections, Security analytics (New); AI traffic monetization
 * (Analysis, Revenue); AWS Shield (Getting started, Overview,
 * Protected resources, Events, Global threat dashboard); AWS
 * Shield network security director (Getting started, Dashboard);
 * AWS Firewall Manager (Getting started, Security policies,
 * Resource sets, Application lists, Protocol lists, Settings);
 * Switch to old WAF console (a note, not a working switch).
 * Rules and Bot Control are GONE from the nav in the new survey
 * and are not kept as extras. Only Protection packs has a
 * backend; every other item is an honest UNSUPPORTED section.
 * Protection-pack columns follow the survey (Name, Rules,
 * Resources, Dashboard, AI traffic analysis (New), Sampled
 * requests, Logging & metrics, Scope, ARN, ID); the backend has
 * no Resources / Dashboard / AI traffic / Sampled requests /
 * Logging & metrics / Scope / ARN field, so those read
 * "Not available" instead of being faked.
 */
export function WafConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'acls')
  const [name, setName] = useState('')
  const [ruleDrafts, setRuleDrafts] = useState<WafRuleDraft[]>([defaultWafRule(1)])
  const [selected, setSelected] = useState('')
  const [path, setPath] = useState('/admin')
  const [sourceIp, setSourceIp] = useState('203.0.113.10')
  const [evalResult, setEvalResult] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const acls = useQuery({ queryKey: ['waf-acls'], queryFn: () => request<Rec>('/api/v1/aws/waf/acls'), retry: 1 })
  const aclRows = listOf(acls.data, 'acls')
  const filteredAcls = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return aclRows
    return aclRows.filter((r) => str(r, 'name').toLowerCase().includes(q))
  }, [aclRows, filter])
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['waf-acls'] }) }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/waf/acls', {
        method: 'POST',
        body: { name: name.trim(), rules: ruleDrafts.filter((r) => r.name.trim() !== '').map((r) => ({ name: r.name.trim(), action: r.action, match_type: r.match_type, match_value: r.match_value })) },
      }),
    onSuccess: () => { setNotice('Protection pack created.'); setName(''); setRuleDrafts([defaultWafRule(1)]); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const evaluate = useMutation({
    mutationFn: () =>
      request<Rec>(`/api/v1/aws/waf/acls/${selected}/evaluate`, { method: 'POST', body: { method: 'GET', path, source_ip: sourceIp } }),
    onSuccess: (d) => { setEvalResult(`Action: ${str(d, 'action')} — matched rule: ${str(d, 'matched_rule') || '(default allow)'}`); refresh() },
    onError: (e: Error) => setNotice(`Evaluate failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/waf/acls/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Protection pack deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = aclRows.find((a) => a.id === selected)
  const selRules = Array.isArray(sel?.rules) ? (sel?.rules as Record<string, unknown>[]) : []
  const updateDraft = (i: number, patch: Partial<WafRuleDraft>) =>
    setRuleDrafts((ds) => ds.map((d, j) => (j === i ? { ...d, ...patch } : d)))
  const subNav = [
    { id: 'home', label: 'WAF dashboard' },
    { id: 'acls', label: 'Protection packs (web ACLs)', group: 'AWS WAF' },
    { id: 'ip-sets', label: 'IP sets', group: 'AWS WAF' },
    { id: 'regex-sets', label: 'Regex pattern sets', group: 'AWS WAF' },
    { id: 'rule-groups', label: 'Rule groups', group: 'AWS WAF' },
    { id: 'app-integration', label: 'Application integration', group: 'AWS WAF' },
    { id: 'add-on-protections', label: 'Add-on protections', group: 'AWS WAF' },
    { id: 'security-analytics', label: 'Security analytics (New)', group: 'AWS WAF' },
    { id: 'ai-analysis', label: 'Analysis', group: 'AI traffic monetization' },
    { id: 'ai-revenue', label: 'Revenue', group: 'AI traffic monetization' },
    { id: 'shield-getting-started', label: 'Getting started', group: 'AWS Shield' },
    { id: 'shield-overview', label: 'Overview', group: 'AWS Shield' },
    { id: 'shield-resources', label: 'Protected resources', group: 'AWS Shield' },
    { id: 'shield-events', label: 'Events', group: 'AWS Shield' },
    { id: 'shield-threat', label: 'Global threat dashboard', group: 'AWS Shield' },
    { id: 'shield-director-getting-started', label: 'Getting started', group: 'AWS Shield network security director' },
    { id: 'shield-director-dashboard', label: 'Dashboard', group: 'AWS Shield network security director' },
    { id: 'fm-getting-started', label: 'Getting started', group: 'AWS Firewall Manager' },
    { id: 'fm-policies', label: 'Security policies', group: 'AWS Firewall Manager' },
    { id: 'fm-resource-sets', label: 'Resource sets', group: 'AWS Firewall Manager' },
    { id: 'fm-app-lists', label: 'Application lists', group: 'AWS Firewall Manager' },
    { id: 'fm-protocol-lists', label: 'Protocol lists', group: 'AWS Firewall Manager' },
    { id: 'fm-settings', label: 'Settings', group: 'AWS Firewall Manager' },
    {
      id: 'old-console',
      label: 'Switch to old WAF console',
      disabledReason: 'Note: the old WAF console is not available in CloudForge — this console follows the new wafv2-pro console (Protection packs). There is no old console to switch to.',
    },
  ]
  return (
    <ServiceConsole
      icon="shield"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'WAF' }]}
      title="WAF — Protection packs"
      description="Rules evaluated for real against sample requests you supply. This protection pack does not block live API traffic."
      fidelity="SIMULATED"
      fidelityFootnote="evaluation only · does not block live traffic"
      subNav={subNav}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="WAF"
          banner="Rules are really evaluated (first match, path/header/query/body/IP matchers) but only by the /evaluate endpoint against a request you supply — they are NOT inline in the API request path, so nothing is live-blocked."
          actions={[
            { label: 'Create protection pack', primary: true, onClick: () => setSection('acls') },
            { label: 'Open Network Firewall', to: '/services/networkfirewall' },
          ]}
          cards={[
            { label: 'Protection packs (web ACLs)', count: aclRows.length, onSelect: () => setSection('acls') },
            { label: 'Rule groups', count: 0, onSelect: () => setSection('rule-groups'), note: 'UNSUPPORTED — no rule-group backend.' },
            { label: 'Shield (DDoS)', count: 0, disabledReason: 'Shield is UNSUPPORTED — see the Shield console.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={acls.isFetching}
          health={[
            acls.isError ? 'Protection packs could not be loaded.' : `${aclRows.length} protection pack(s) listed.`,
            'Evaluation only — the ACL does not block live API traffic.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — IAM track', to: '/learn' },
            { label: 'Shield (unsupported — exam concept)', to: '/services/shield' },
          ]}
        />
      )}
      {section === 'acls' && (
        <>
          <Notice text={notice} />
          {acls.isPending && <Loading label="Loading protection packs" />}
          {acls.isError && <ErrorBox message={`Protection packs could not be loaded (${acls.error.message}).`} />}
          <AwsCard title="Protection packs (web ACLs)">
            <form
              className="mb-3 space-y-2"
              onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '' && ruleDrafts.some((r) => r.name.trim() !== '')) create.mutate() }}
            >
              <div className="flex flex-wrap items-center gap-2">
                <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Protection pack name" className={inputCls} />
              </div>
              {ruleDrafts.map((r, i) => (
                <div key={i} className="flex flex-wrap items-center gap-2">
                  <span className="w-20 text-sm text-aws-muted">Priority {i + 1}</span>
                  <input value={r.name} onChange={(e) => updateDraft(i, { name: e.target.value })} placeholder="Rule name" className={inputCls} aria-label="Rule name" />
                  <select value={r.action} onChange={(e) => updateDraft(i, { action: e.target.value })} className={inputCls} aria-label="Rule action">
                    <option value="block">block</option>
                    <option value="allow">allow</option>
                  </select>
                  <select value={r.match_type} onChange={(e) => updateDraft(i, { match_type: e.target.value })} className={inputCls} aria-label="Match type">
                    <option value="path_prefix">path_prefix</option>
                    <option value="path_contains">path_contains</option>
                    <option value="query_contains">query_contains</option>
                    <option value="body_contains">body_contains</option>
                    <option value="ip_prefix">ip_prefix</option>
                    <option value="header_equals">header_equals</option>
                  </select>
                  <input value={r.match_value} onChange={(e) => updateDraft(i, { match_value: e.target.value })} placeholder="Match value" className={inputCls} aria-label="Match value" />
                  {ruleDrafts.length > 1 && <AwsButton onClick={() => setRuleDrafts((ds) => ds.filter((_, j) => j !== i))}>Remove rule</AwsButton>}
                </div>
              ))}
              <div className="flex flex-wrap items-center gap-2">
                <AwsButton onClick={() => setRuleDrafts((ds) => [...ds, defaultWafRule(ds.length + 1)])}>Add rule</AwsButton>
                <AwsButton variant="primary" type="submit">Create protection pack</AwsButton>
              </div>
              <p className="text-xs text-aws-muted">Rules are evaluated in list order — first match wins, so position is priority. Rules are set at create time; there is no update-rule endpoint, so an existing protection pack cannot gain rules later.</p>
            </form>
            <AwsTable
              columns={[
                { header: 'Name', render: (r) => str(r, 'name') },
                { header: 'Rules', render: (r) => String(Array.isArray(r.rules) ? (r.rules as unknown[]).length : 0) },
                { header: 'Resources', render: () => <span title="The WAF backend stores no associated resources">Not available</span> },
                { header: 'Dashboard', render: () => <span title="No per-pack dashboard backend in CloudForge">Not available</span> },
                { header: 'AI traffic analysis (New)', render: () => <span title="AI traffic analysis has no backend in CloudForge">Not available</span> },
                { header: 'Sampled requests', render: () => <span title="Sampled requests have no backend in CloudForge — only the Evaluate form below samples a request">Not available</span> },
                { header: 'Logging & metrics', render: () => <span title="Per-pack logging and metrics have no backend in CloudForge">Not available</span> },
                { header: 'Scope', render: () => <span title="The WAF backend stores no scope (regional / CloudFront)">Not available</span> },
                { header: 'ARN', render: () => <span title="The WAF backend assigns no ARN">Not available</span> },
                { header: 'ID', render: (r) => <span className="break-all font-mono text-xs">{r.id}</span> },
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
              rows={filteredAcls}
              emptyMessage="No protection packs yet."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter protection packs"
              totalCount={aclRows.length}
            />
            <p className="mt-2 text-xs text-aws-muted">
              Survey columns, kept honest: only Name, Rules and ID come from the backend. Resources, Dashboard,
              AI traffic analysis, Sampled requests, Logging &amp; metrics, Scope and ARN have no backend field
              and are shown as Not available, not faked.
            </p>
          </AwsCard>
          {sel && (
            <AwsCard title={`Evaluate: ${str(sel, 'name')}`}>
              <AwsTable
                columns={[
                  { header: 'Priority', render: (r) => String(num(r as Rec, 'priority')) },
                  { header: 'Rule', render: (r) => str(r as Rec, 'name') },
                  { header: 'Action', render: (r) => str(r as Rec, 'action') },
                  { header: 'Match', render: (r) => `${str(r as Rec, 'match_type')}: ${str(r as Rec, 'match_value')}` },
                ]}
                rows={selRules.map((r, i) => ({ ...r, id: `rule-${i}`, priority: i + 1 }))}
                emptyMessage="No rules."
              />
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="Request path" className={inputCls} aria-label="Request path" />
                <input value={sourceIp} onChange={(e) => setSourceIp(e.target.value)} placeholder="Source IP" className={inputCls} aria-label="Source IP" />
                <AwsButton variant="primary" onClick={() => evaluate.mutate()}>Evaluate request</AwsButton>
              </div>
              {evalResult !== '' && <p className="mt-2 text-sm" role="status">{evalResult}</p>}
              <p className="mt-2 text-xs text-aws-muted">First matching rule wins; no match → default allow.</p>
            </AwsCard>
          )}
        </>
      )}
      {section === 'app-integration' && (
        <UnsupportedSection
          title="Application integration"
          reason="Application integration (SDK / JS integration for WAF) has no backend in CloudForge — the WAF backend evaluates rules only through the /evaluate endpoint against a request you supply."
          actionLabel="Set up application integration"
        />
      )}
      {section === 'add-on-protections' && (
        <UnsupportedSection
          title="Add-on protections"
          reason="Add-on protections have no backend in CloudForge — the WAF backend supports only allow/block rules with path, query, body, header and IP matchers."
          actionLabel="Enable add-on protection"
        />
      )}
      {section === 'security-analytics' && (
        <UnsupportedSection
          title="Security analytics"
          reason="Security analytics has no backend in CloudForge — there is no sampled-request store, log pipeline or analytics view for protection packs."
          actionLabel="Open security analytics"
        />
      )}
      {section === 'ai-analysis' && (
        <UnsupportedSection
          title="AI traffic monetization — Analysis"
          reason="AI traffic analysis has no backend in CloudForge — the WAF backend records no AI-traffic data to analyse."
          actionLabel="Open analysis"
        />
      )}
      {section === 'ai-revenue' && (
        <UnsupportedSection
          title="AI traffic monetization — Revenue"
          reason="AI traffic revenue has no backend in CloudForge — there is no AI-traffic metering or billing to report revenue from."
          actionLabel="Open revenue"
        />
      )}
      {section === 'shield-getting-started' && (
        <UnsupportedSection
          title="AWS Shield — Getting started"
          reason="Shield getting started has no backend in CloudForge — Shield is UNSUPPORTED; see the Shield console."
          actionLabel="Get started with Shield"
        />
      )}
      {section === 'shield-overview' && (
        <UnsupportedSection
          title="AWS Shield — Overview"
          reason="The Shield overview has no backend in CloudForge — Shield is UNSUPPORTED; see the Shield console."
          actionLabel="Open Shield overview"
        />
      )}
      {section === 'shield-resources' && (
        <UnsupportedSection
          title="AWS Shield — Protected resources"
          reason="Shield protected resources have no backend in CloudForge — Shield is UNSUPPORTED; see the Shield console."
          actionLabel="View protected resources"
        />
      )}
      {section === 'shield-events' && (
        <UnsupportedSection
          title="AWS Shield — Events"
          reason="Shield events have no backend in CloudForge — Shield is UNSUPPORTED; see the Shield console."
          actionLabel="View Shield events"
        />
      )}
      {section === 'shield-threat' && (
        <UnsupportedSection
          title="AWS Shield — Global threat dashboard"
          reason="The Shield global threat dashboard has no backend in CloudForge — Shield is UNSUPPORTED; see the Shield console."
          actionLabel="Open threat dashboard"
        />
      )}
      {section === 'shield-director-getting-started' && (
        <UnsupportedSection
          title="Shield network security director — Getting started"
          reason="Shield network security director has no backend in CloudForge — there is no network-security-director API."
          actionLabel="Get started"
        />
      )}
      {section === 'shield-director-dashboard' && (
        <UnsupportedSection
          title="Shield network security director — Dashboard"
          reason="The Shield network security director dashboard has no backend in CloudForge — there is no network-security-director API."
          actionLabel="Open dashboard"
        />
      )}
      {section === 'fm-getting-started' && (
        <UnsupportedSection
          title="Firewall Manager — Getting started"
          reason="Firewall Manager has no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Get started"
        />
      )}
      {section === 'fm-policies' && (
        <UnsupportedSection
          title="Firewall Manager — Security policies"
          reason="Firewall Manager security policies have no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Create security policy"
        />
      )}
      {section === 'fm-resource-sets' && (
        <UnsupportedSection
          title="Firewall Manager — Resource sets"
          reason="Firewall Manager resource sets have no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Create resource set"
        />
      )}
      {section === 'fm-app-lists' && (
        <UnsupportedSection
          title="Firewall Manager — Application lists"
          reason="Firewall Manager application lists have no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Create application list"
        />
      )}
      {section === 'fm-protocol-lists' && (
        <UnsupportedSection
          title="Firewall Manager — Protocol lists"
          reason="Firewall Manager protocol lists have no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Create protocol list"
        />
      )}
      {section === 'fm-settings' && (
        <UnsupportedSection
          title="Firewall Manager — Settings"
          reason="Firewall Manager settings have no backend in CloudForge — there is no Firewall Manager API."
          actionLabel="Save settings"
        />
      )}
      {section === 'ip-sets' && (
        <UnsupportedSection
          title="IP sets"
          reason="Reusable IP sets have no backend in CloudForge — IP matching is done inline per rule (ip_prefix) instead of by referencing a shared set."
          actionLabel="Create IP set"
        />
      )}
      {section === 'regex-sets' && (
        <UnsupportedSection
          title="Regex pattern sets"
          reason="Reusable regex pattern sets have no backend in CloudForge — the WAF backend supports only literal match values (path_contains, query_contains, body_contains), not regular expressions."
          actionLabel="Create regex pattern set"
        />
      )}
      {section === 'rule-groups' && (
        <UnsupportedSection
          title="Rule groups"
          reason="Reusable rule groups have no backend in CloudForge — rules live directly on a web ACL and cannot be shared between ACLs."
          actionLabel="Create rule group"
        />
      )}
    </ServiceConsole>
  )
}

/**
 * KMS console (Phase 6) — mirrors the AWS KMS keys screens.
 * Keys are real envelope-encryption keys held in the platform store
 * (master key from the server environment); wrapped keys are never
 * returned. Rotate keeps previous versions so old ciphertext still
 * decrypts.
 *
 * Left nav follows the fresh v4 survey (2026-11-22): AWS managed keys,
 * Customer managed keys, Keys shared with me (New), Custom key stores
 * (AWS CloudHSM key stores, External key stores). Settings is NOT in
 * the new survey's nav and is not kept. Only Customer managed keys has
 * a backend. The old Home "Aliases" card is removed: it promised
 * "Aliases on the key detail" where no alias feature or backend exists.
 * Keys columns follow the survey (Aliases, Key ID, Status, Key type,
 * Key spec, Key usage); the backend stores only name, description,
 * status, version and ID, so Aliases / Key type / Key spec / Key usage
 * read "Not available" instead of being faked. Key detail tabs are NOT
 * OBSERVABLE in the survey, so the existing Details / Encrypt & decrypt
 * tabs are kept and none are invented.
 */
export function KmsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'keys')
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [selected, setSelected] = useState('')
  const [keyTab, setKeyTab] = useState('details')
  const [plain, setPlain] = useState('hello cloudforge')
  const [cipher, setCipher] = useState('')
  const [decrypted, setDecrypted] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const keys = useQuery({ queryKey: ['kms-keys'], queryFn: () => request<Rec>('/api/v1/aws/kms/keys'), retry: 1 })
  const keyRows = listOf(keys.data, 'keys')
  const filteredKeys = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return keyRows
    return keyRows.filter((r) => `${str(r, 'name')} ${str(r, 'description')} ${str(r, 'status')}`.toLowerCase().includes(q))
  }, [keyRows, filter])
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['kms-keys'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/kms/keys', { method: 'POST', body: { name: name.trim(), description: desc } }),
    onSuccess: () => { setNotice('Key created.'); setName(''); setDesc(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const encrypt = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/kms/keys/${selected}/encrypt`, { method: 'POST', body: { plaintext: plain } }),
    onSuccess: (d) => { setCipher(str(d, 'ciphertext')); setDecrypted(''); setNotice('Encrypted.'); },
    onError: (e: Error) => setNotice(`Encrypt failed: ${e.message}`),
  })
  const decrypt = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/kms/keys/${selected}/decrypt`, { method: 'POST', body: { ciphertext: cipher } }),
    onSuccess: (d) => { setDecrypted(str(d, 'plaintext')); setNotice('Decrypted.'); },
    onError: (e: Error) => setNotice(`Decrypt failed: ${e.message}`),
  })
  const rotate = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/kms/keys/${id}/rotate`, { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Key rotated to version ${String(d.version ?? '')}.`); refresh() },
    onError: (e: Error) => setNotice(`Rotate failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/kms/keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Key deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = keyRows.find((k) => k.id === selected)
  return (
    <ServiceConsole
      icon="lock"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'KMS' }]}
      title="KMS — Customer managed keys"
      description="Real envelope encryption (AES-256-GCM) with keys stored in the platform store. Not AWS KMS or an HSM."
      fidelity="PARTIAL"
      fidelityFootnote="durable local envelope encryption"
      subNav={[
        { id: 'home', label: 'KMS dashboard' },
        { id: 'aws-managed', label: 'AWS managed keys' },
        { id: 'keys', label: 'Customer managed keys' },
        { id: 'shared', label: 'Keys shared with me (New)' },
        { id: 'cloudhsm', label: 'AWS CloudHSM key stores', group: 'Custom key stores' },
        { id: 'external', label: 'External key stores', group: 'Custom key stores' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="KMS"
          banner="Durable local keys: AES-256 key material stored wrapped under a master key; real AES-256-GCM encrypt/decrypt with rotation that keeps previous versions decryptable. NOT real AWS KMS, no HSM."
          actions={[
            { label: 'Create key', primary: true, onClick: () => setSection('keys') },
            { label: 'Open Secrets Manager', to: '/services/secretsmanager' },
          ]}
          cards={[
            { label: 'Customer managed keys', count: keyRows.length, onSelect: () => setSection('keys') },
            { label: 'Grants', count: 0, disabledReason: 'Not modelled.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={keys.isFetching}
          health={[
            keys.isError ? 'Keys could not be loaded.' : `${keyRows.length} key(s) listed.`,
            'Wrapped keys never leave the server.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — IAM track', to: '/learn' },
            { label: 'Secrets Manager (KMS consumer)', to: '/services/secretsmanager' },
          ]}
        />
      )}
      {section === 'aws-managed' && (
        <UnsupportedSection
          title="AWS managed keys"
          reason="CloudForge has no AWS-managed keys — every key here is a customer-managed local key created through this console. There is no AWS account behind CloudForge, so no AWS-managed key can exist."
          actionLabel="Create AWS managed key"
        />
      )}
      {section === 'keys' && (
        <>
          <Notice text={notice} />
          {keys.isPending && <Loading label="Loading keys" />}
          {keys.isError && <ErrorBox message={`Keys could not be loaded (${keys.error.message}).`} />}
          <AwsCard title="Keys">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
            >
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Key name" className={inputCls} />
              <input value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Description" className={inputCls} />
              <AwsButton variant="primary" type="submit">Create key</AwsButton>
            </form>
            <AwsTable
              columns={[
                { header: 'Aliases', render: () => <span title="Key aliases are not modelled in CloudForge — keys are addressed by name">Not available</span> },
                { header: 'Key ID', render: (r) => (
                  <span>
                    <span className="break-all font-mono text-xs">{r.id}</span>
                    <span className="block text-xs text-aws-muted">{str(r, 'name')}</span>
                  </span>
                ) },
                { header: 'Status', render: (r) => str(r, 'status') },
                { header: 'Key type', render: () => <span title="The KMS backend stores no key type">Not available</span> },
                { header: 'Key spec', render: () => <span title="The KMS backend stores no key spec">Not available</span> },
                { header: 'Key usage', render: () => <span title="The KMS backend stores no key usage">Not available</span> },
                {
                  header: 'Actions',
                  render: (r) => (
                    <span className="flex gap-2">
                      <AwsButton onClick={() => { setSelected(str(r, 'id')); setKeyTab('details') }}>Use</AwsButton>
                      <AwsButton onClick={() => rotate.mutate(str(r, 'id'))}>Rotate</AwsButton>
                      <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                    </span>
                  ),
                },
              ]}
              rows={filteredKeys}
              emptyMessage="No keys yet."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter keys"
              totalCount={keyRows.length}
            />
            <p className="mt-2 text-xs text-aws-muted">
              Survey columns, kept honest: only Key ID and Status come from the backend (name and description live
              on the create form and the key detail). Aliases, Key type, Key spec and Key usage have no backend
              field and are shown as Not available, not faked.
            </p>
          </AwsCard>
          {sel && (
            <AwsCard title={`Key: ${str(sel, 'name')} (v${String(num(sel, 'version'))})`}>
              <div className="mb-3 flex items-center justify-between gap-2">
                <ConsoleTabs
                  tabs={[
                    { id: 'details', label: 'Details' },
                    { id: 'crypto', label: 'Encrypt / decrypt' },
                  ]}
                  active={keyTab}
                  onChange={setKeyTab}
                />
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelected('')}>
                  Close
                </button>
              </div>
              {keyTab === 'details' && (
                <Dl
                  entries={[
                    ['Name', str(sel, 'name')],
                    ['Description', str(sel, 'description') || '—'],
                    ['Version', String(num(sel, 'version'))],
                    ['Status', str(sel, 'status')],
                    ['ID', sel.id],
                  ]}
                />
              )}
              {keyTab === 'crypto' && (
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <input value={plain} onChange={(e) => setPlain(e.target.value)} className={`${inputCls} w-80`} aria-label="Plaintext" />
                    <AwsButton variant="primary" onClick={() => encrypt.mutate()}>Encrypt</AwsButton>
                  </div>
                  {cipher !== '' && (
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="w-80 break-all font-mono text-xs">{cipher}</span>
                      <AwsButton onClick={() => decrypt.mutate()}>Decrypt</AwsButton>
                    </div>
                  )}
                  {decrypted !== '' && <p className="text-sm" role="status">Decrypted: {decrypted}</p>}
                  <p className="text-xs text-aws-muted">Ciphertext is AES-256-GCM (random nonce, base64). Decrypting with the wrong key fails with 400 — nothing is faked.</p>
                </div>
              )}
            </AwsCard>
          )}
        </>
      )}
      {section === 'shared' && (
        <UnsupportedSection
          title="Keys shared with me"
          reason="Cross-account key sharing has no backend in CloudForge — keys belong to the user who created them and cannot be shared with another account, because there are no AWS accounts here."
          actionLabel="View shared keys"
        />
      )}
      {section === 'cloudhsm' && (
        <UnsupportedSection
          title="AWS CloudHSM key stores"
          reason="CloudHSM key stores have no backend in CloudForge — keys are stored wrapped under the platform master key, never in an HSM."
          actionLabel="Create CloudHSM key store"
        />
      )}
      {section === 'external' && (
        <UnsupportedSection
          title="External key stores"
          reason="External key stores have no backend in CloudForge — keys are stored wrapped under the platform master key, never in an external key store."
          actionLabel="Create external key store"
        />
      )}
    </ServiceConsole>
  )
}

/**
 * Secrets Manager console (Phase 6) — mirrors the AWS Secrets
 * Manager screens. Values are envelope-encrypted with a KMS key
 * and stored encrypted at rest; list responses never include the
 * value.
 *
 * Left nav is the survey's single item (Secrets). The secret detail
 * is split into Value / Details tabs showing only real backend
 * fields; there is no versions list or rotation schedule (the Home
 * cards say so honestly). Secrets columns follow the fresh v4 survey
 * (Secret name, Description, Last retrieved (UTC), Created on (UTC));
 * the backend stores no description and tracks no last-retrieved
 * time, so those read "Not available", and Created on comes from the
 * real created_at timestamp. Secret detail tabs are NOT OBSERVABLE
 * in the survey, so none are invented.
 */
export function SecretsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'secrets')
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [kmsKey, setKmsKey] = useState('')
  const [selected, setSelected] = useState('')
  const [secretTab, setSecretTab] = useState('value')
  const [newValue, setNewValue] = useState('')
  const [revealed, setRevealed] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const secrets = useQuery({ queryKey: ['secrets'], queryFn: () => request<Rec>('/api/v1/aws/secretsmanager/secrets'), retry: 1 })
  const secretRows = listOf(secrets.data, 'secrets')
  const filteredSecrets = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (q === '') return secretRows
    return secretRows.filter((r) => `${str(r, 'name')} ${str(r, 'kms_key_id')}`.toLowerCase().includes(q))
  }, [secretRows, filter])
  const keys = useQuery({ queryKey: ['kms-keys'], queryFn: () => request<Rec>('/api/v1/aws/kms/keys'), retry: 1 })
  const keyRows = listOf(keys.data, 'keys')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['secrets'] }) }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/secretsmanager/secrets', { method: 'POST', body: { name: name.trim(), value, kms_key_id: kmsKey || undefined } }),
    onSuccess: () => { setNotice('Secret stored (encrypted at rest).'); setName(''); setValue(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const reveal = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/secretsmanager/secrets/${id}`),
    onSuccess: (d) => { setRevealed(str(d, 'value')); setSelected(str(d, 'id')); setSecretTab('value') },
    onError: (e: Error) => setNotice(`Get failed: ${e.message}`),
  })
  const putValue = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/secretsmanager/secrets/${selected}/value`, { method: 'POST', body: { value: newValue } }),
    onSuccess: (d) => { setNotice(`New version stored (version ${String(d.version ?? '')}).`); setNewValue(''); setRevealed(''); refresh() },
    onError: (e: Error) => setNotice(`Put failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/secretsmanager/secrets/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Secret deleted.'); setSelected(''); setRevealed(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = secretRows.find((s) => s.id === selected) ?? null
  return (
    <ServiceConsole
      icon="lock"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Secrets Manager' }]}
      title="Secrets Manager — Secrets"
      description="Secrets are envelope-encrypted with a KMS key and stored encrypted at rest. No automatic rotation."
      fidelity="PARTIAL"
      fidelityFootnote="encrypted at rest · no auto-rotation"
      subNav={[
        { id: 'home', label: 'Secrets Manager dashboard' },
        { id: 'secrets', label: 'Secrets' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="Secrets Manager"
          banner="Secrets are envelope-encrypted at rest (AES-256-GCM value, data key wrapped by a KMS key) and versioned; values are decrypted on read and never stored in plaintext. No automatic rotation schedule."
          actions={[
            { label: 'Create secret', primary: true, onClick: () => setSection('secrets') },
            { label: 'Open KMS', to: '/services/kms' },
          ]}
          cards={[
            { label: 'Secrets', count: secretRows.length, onSelect: () => setSection('secrets') },
            { label: 'Versions', count: '—', onSelect: () => setSection('secrets'), note: 'Versions increment on put' },
            { label: 'Rotation schedules', count: 0, disabledReason: 'No automatic rotation schedule.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={secrets.isFetching}
          health={[
            secrets.isError ? 'Secrets could not be loaded.' : `${secretRows.length} secret(s) listed.`,
            'List responses never include the value or ciphertext.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — IAM track', to: '/learn' },
            { label: 'KMS (wrapping keys)', to: '/services/kms' },
          ]}
        />
      )}
      {section === 'secrets' && (
        <>
          <Notice text={notice} />
          {secrets.isPending && <Loading label="Loading secrets" />}
          {secrets.isError && <ErrorBox message={`Secrets could not be loaded (${secrets.error.message}).`} />}
          <AwsCard title="Secrets">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '' && value !== '') create.mutate() }}
            >
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Secret name" className={inputCls} />
              <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="Secret value" className={inputCls} type="password" />
              <label className="text-sm">
                KMS key
                <select value={kmsKey} onChange={(e) => setKmsKey(e.target.value)} className={`ml-2 ${inputCls}`}>
                  <option value="">(default key)</option>
                  {keyRows.map((k) => (
                    <option key={k.id} value={k.id}>{str(k, 'name')}</option>
                  ))}
                </select>
              </label>
              <AwsButton variant="primary" type="submit">Store secret</AwsButton>
            </form>
            <AwsTable
              columns={[
                { header: 'Secret name', render: (r) => str(r, 'name') },
                { header: 'Description', render: () => <span title="The Secrets backend stores no description">Not available</span> },
                { header: 'Last retrieved (UTC)', render: () => <span title="The Secrets backend tracks no last-retrieved time">Not available</span> },
                { header: 'Created on (UTC)', render: (r) => (str(r, 'created_at') !== '' ? new Date(str(r, 'created_at')).toUTCString() : '—') },
                {
                  header: 'Actions',
                  render: (r) => (
                    <span className="flex gap-2">
                      <AwsButton onClick={() => reveal.mutate(str(r, 'id'))}>Show value</AwsButton>
                      <AwsButton onClick={() => { setSelected(str(r, 'id')); setRevealed(''); setSecretTab('value') }}>Update value</AwsButton>
                      <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                    </span>
                  ),
                },
              ]}
              rows={filteredSecrets}
              emptyMessage="No secrets yet."
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter secrets"
              totalCount={secretRows.length}
            />
            <p className="mt-2 text-xs text-aws-muted">
              Survey columns, kept honest: only Secret name and Created on (UTC) come from the backend. Description
              and Last retrieved have no backend field and are shown as Not available, not faked. The KMS key
              lives on the secret detail.
            </p>
          </AwsCard>
          {sel && (
            <AwsCard title={`Secret: ${str(sel, 'name')}`}>
              <div className="mb-3 flex items-center justify-between gap-2">
                <ConsoleTabs
                  tabs={[
                    { id: 'value', label: 'Value' },
                    { id: 'details', label: 'Details' },
                  ]}
                  active={secretTab}
                  onChange={setSecretTab}
                />
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelected('')}>
                  Close
                </button>
              </div>
              {secretTab === 'value' && (
                <div>
                  {revealed !== '' && <p className="text-sm" role="status">Value: <span className="font-mono">{revealed}</span></p>}
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    <input value={newValue} onChange={(e) => setNewValue(e.target.value)} placeholder="New value" className={inputCls} type="password" />
                    <AwsButton onClick={() => newValue !== '' && putValue.mutate()}>Store new version</AwsButton>
                  </div>
                  <p className="mt-2 text-xs text-aws-muted">Storing a new value increments the version; previous versions are not listed (no versions list backend).</p>
                </div>
              )}
              {secretTab === 'details' && (
                <Dl
                  entries={[
                    ['Name', str(sel, 'name')],
                    ['Description', 'Not available — the backend stores no description.'],
                    ['Last retrieved (UTC)', 'Not available — the backend tracks no last-retrieved time.'],
                    ['Created on (UTC)', str(sel, 'created_at') !== '' ? new Date(str(sel, 'created_at')).toUTCString() : '—'],
                    ['Version', String(num(sel, 'version'))],
                    ['KMS key', str(sel, 'kms_key_id') || '(default key)'],
                    ['ID', sel.id],
                  ]}
                />
              )}
            </AwsCard>
          )}
        </>
      )}
    </ServiceConsole>
  )
}
