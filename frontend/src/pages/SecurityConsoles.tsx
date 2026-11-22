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

function FindingsTable({ rows, onStatus }: { rows: Rec[]; onStatus?: (id: string, status: string) => void }) {
  return (
    <AwsTable
      columns={[
        { header: 'Title', render: (r) => str(r, 'title') },
        { header: 'Severity', render: (r) => str(r, 'severity') },
        { header: 'Source', render: (r) => str(r, 'source') },
        { header: 'Status', render: (r) => str(r, 'status') },
        { header: 'Detail', render: (r) => str(r, 'detail').slice(0, 120) },
        {
          header: 'Actions',
          render: (r) =>
            onStatus ? (
              <span className="flex gap-2">
                <AwsButton onClick={() => onStatus(str(r, 'id'), 'ACKNOWLEDGED')}>Acknowledge</AwsButton>
                <AwsButton onClick={() => onStatus(str(r, 'id'), 'RESOLVED')}>Resolve</AwsButton>
                <AwsButton onClick={() => onStatus(str(r, 'id'), 'SUPPRESSED')}>Suppress</AwsButton>
              </span>
            ) : (
              <span>—</span>
            ),
        },
      ]}
      rows={rows}
      emptyMessage="No findings."
    />
  )
}

/**
 * GuardDuty console (Phase 6) — mirrors the AWS GuardDuty findings
 * screens. Findings come only from real evidence: failed logins in
 * the platform's own audit log. A scan with no evidence creates
 * zero findings.
 */
export function GuardDutyConsole() {
  const queryClient = useQueryClient()
  const [detName, setDetName] = useState('')
  const [notice, setNotice] = useState('')
  const detectors = useQuery({ queryKey: ['gd-detectors'], queryFn: () => request<Rec>('/api/v1/aws/guardduty/detectors'), retry: 1 })
  const detRows = listOf(detectors.data, 'detectors')
  const findings = useQuery({ queryKey: ['gd-findings'], queryFn: () => request<Rec>('/api/v1/aws/guardduty/findings'), retry: 1 })
  const findingRows = listOf(findings.data, 'findings')
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
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'GuardDuty' }]}
        title="GuardDuty — Threat detection"
        description="Findings are generated only from real evidence in the platform's own audit log (failed logins, last 24 h). Never invented."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="real audit-log evidence only" />}
      />
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
      <AwsCard title={`Findings (${findingRows.length})`}>
        <FindingsTable rows={findingRows} onStatus={(id, status) => setStatus.mutate({ id, status })} />
      </AwsCard>
    </div>
  )
}

/**
 * Security Hub console (Phase 6) — mirrors the AWS Security Hub
 * findings screens. It aggregates the real findings in the shared
 * findings table; it creates none of its own.
 */
export function SecurityHubConsole() {
  const queryClient = useQueryClient()
  const [notice, setNotice] = useState('')
  const summary = useQuery({ queryKey: ['sechub-summary'], queryFn: () => request<Rec>('/api/v1/aws/securityhub/summary'), retry: 1 })
  const findings = useQuery({ queryKey: ['sechub-findings'], queryFn: () => request<Rec>('/api/v1/aws/securityhub/findings'), retry: 1 })
  const findingRows = listOf(findings.data, 'findings')
  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => request<Rec>(`/api/v1/aws/guardduty/findings/${id}/status`, { method: 'POST', body: { status } }),
    onSuccess: () => { setNotice('Finding status updated.'); void queryClient.invalidateQueries({ queryKey: ['sechub-findings'] }); void queryClient.invalidateQueries({ queryKey: ['sechub-summary'] }) },
    onError: (e: Error) => setNotice(`Status update failed: ${e.message}`),
  })
  const bySeverity = (summary.data?.by_severity ?? {}) as Record<string, number>
  const bySource = (summary.data?.by_source ?? {}) as Record<string, number>
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Security Hub' }]}
        title="Security Hub — Findings"
        description="Central aggregation of findings from GuardDuty, Inspector, Config and Trusted Advisor."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="aggregation only — no findings of its own" />}
      />
      <Notice text={notice} />
      {summary.isPending && <Loading label="Loading summary" />}
      {summary.isError && <ErrorBox message={`Summary could not be loaded (${summary.error.message}).`} />}
      <AwsCard title="Summary">
        <p className="text-sm">Open findings: {String(num((summary.data ?? {}) as Rec, 'open'))}</p>
        <p className="mt-1 text-sm">By severity: {Object.entries(bySeverity).map(([k, v]) => `${k}: ${v}`).join(' · ') || '—'}</p>
        <p className="mt-1 text-sm">By source: {Object.entries(bySource).map(([k, v]) => `${k}: ${v}`).join(' · ') || '—'}</p>
      </AwsCard>
      <AwsCard title={`Findings (${findingRows.length})`}>
        <FindingsTable rows={findingRows} onStatus={(id, status) => setStatus.mutate({ id, status })} />
      </AwsCard>
    </div>
  )
}

/**
 * Inspector console (Phase 6) — mirrors the AWS Inspector
 * assessment screens. Checks run against real resource state:
 * open security groups, public S3 bucket policies, unencrypted
 * lab instances.
 */
export function InspectorConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [labId, setLabId] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const assessments = useQuery({ queryKey: ['inspector-assessments'], queryFn: () => request<Rec>('/api/v1/aws/inspector/assessments'), retry: 1 })
  const assessRows = listOf(assessments.data, 'assessments')
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
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Inspector' }]}
        title="Inspector — Assessments"
        description="Assessments against real lab resources: open security groups, public buckets, unencrypted instances."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="resource checks only · no CVE scanning" />}
      />
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
              render: (r) => <AwsButton onClick={() => setSelected(str(r, 'id'))}>View findings</AwsButton>,
            },
          ]}
          rows={assessRows}
          emptyMessage="No assessments yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Assessment: ${str(sel, 'name')}`}>
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
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Config console (Phase 6) — mirrors the AWS Config rules /
 * resources screens: compliance rules evaluated against real
 * resources, a configuration-history snapshot, and per-resource
 * results.
 */
export function ConfigConsole() {
  const queryClient = useQueryClient()
  const [ruleName, setRuleName] = useState('')
  const [ruleType, setRuleType] = useState('sg-no-open-ssh')
  const [notice, setNotice] = useState('')
  const rules = useQuery({ queryKey: ['config-rules'], queryFn: () => request<Rec>('/api/v1/aws/config/rules'), retry: 1 })
  const ruleRows = listOf(rules.data, 'rules')
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
    onSuccess: () => { setNotice('Rule deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const snapshot = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/config/snapshots', { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Snapshot recorded: ${String(d.items_recorded ?? 0)} resource(s).`); refresh() },
    onError: (e: Error) => setNotice(`Snapshot failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Config' }]}
        title="Config — Rules & configuration history"
        description="Compliance rules evaluated against real resources, with non-compliant resources listed."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="3 rule types · real evaluation" />}
      />
      <Notice text={notice} />
      {rules.isPending && <Loading label="Loading rules" />}
      {rules.isError && <ErrorBox message={`Rules could not be loaded (${rules.error.message}).`} />}
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
                  <AwsButton onClick={() => evaluate.mutate(str(r, 'id'))}>Evaluate</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={ruleRows}
          emptyMessage="No rules yet."
        />
      </AwsCard>
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
    </div>
  )
}

/**
 * WAF console (Phase 6) — mirrors the AWS WAF web ACLs screens.
 * Rules are really evaluated against a sample request the user
 * supplies; the ACL does not block live API traffic (labelled).
 */
export function WafConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [ruleName, setRuleName] = useState('block-admin')
  const [ruleAction, setRuleAction] = useState('block')
  const [matchType, setMatchType] = useState('path_prefix')
  const [matchValue, setMatchValue] = useState('/admin')
  const [selected, setSelected] = useState('')
  const [path, setPath] = useState('/admin')
  const [sourceIp, setSourceIp] = useState('203.0.113.10')
  const [evalResult, setEvalResult] = useState('')
  const [notice, setNotice] = useState('')
  const acls = useQuery({ queryKey: ['waf-acls'], queryFn: () => request<Rec>('/api/v1/aws/waf/acls'), retry: 1 })
  const aclRows = listOf(acls.data, 'acls')
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['waf-acls'] }) }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/waf/acls', {
        method: 'POST',
        body: { name: name.trim(), rules: [{ name: ruleName.trim(), action: ruleAction, match_type: matchType, match_value: matchValue }] },
      }),
    onSuccess: () => { setNotice('Web ACL created.'); setName(''); refresh() },
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
    onSuccess: () => { setNotice('Web ACL deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = aclRows.find((a) => a.id === selected)
  const rules = Array.isArray(sel?.rules) ? (sel?.rules as Record<string, unknown>[]) : []
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'WAF' }]}
        title="WAF — Web ACLs"
        description="Rules evaluated for real against sample requests you supply. This ACL does not block live API traffic."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="evaluation only · does not block live traffic" />}
      />
      <Notice text={notice} />
      {acls.isPending && <Loading label="Loading web ACLs" />}
      {acls.isError && <ErrorBox message={`Web ACLs could not be loaded (${acls.error.message}).`} />}
      <AwsCard title="Web ACLs">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="ACL name" className={inputCls} />
          <input value={ruleName} onChange={(e) => setRuleName(e.target.value)} placeholder="Rule name" className={inputCls} />
          <select value={ruleAction} onChange={(e) => setRuleAction(e.target.value)} className={inputCls} aria-label="Rule action">
            <option value="block">block</option>
            <option value="allow">allow</option>
          </select>
          <select value={matchType} onChange={(e) => setMatchType(e.target.value)} className={inputCls} aria-label="Match type">
            <option value="path_prefix">path_prefix</option>
            <option value="path_contains">path_contains</option>
            <option value="query_contains">query_contains</option>
            <option value="body_contains">body_contains</option>
            <option value="ip_prefix">ip_prefix</option>
            <option value="header_equals">header_equals</option>
          </select>
          <input value={matchValue} onChange={(e) => setMatchValue(e.target.value)} placeholder="Match value" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create web ACL</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Rules', render: (r) => String(Array.isArray(r.rules) ? (r.rules as unknown[]).length : 0) },
            { header: 'Evaluations', render: (r) => String(num(r, 'evaluations')) },
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
          rows={aclRows}
          emptyMessage="No web ACLs yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Evaluate: ${str(sel, 'name')}`}>
          <AwsTable
            columns={[
              { header: 'Rule', render: (r) => str(r as Rec, 'name') },
              { header: 'Action', render: (r) => str(r as Rec, 'action') },
              { header: 'Match', render: (r) => `${str(r as Rec, 'match_type')}: ${str(r as Rec, 'match_value')}` },
            ]}
            rows={rules.map((r, i) => ({ ...r, id: `rule-${i}` }))}
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
    </div>
  )
}

/**
 * KMS console (Phase 6) — mirrors the AWS KMS keys screens.
 * Keys are real envelope-encryption keys held in the platform store
 * (master key from the server environment); wrapped keys are never
 * returned. Rotate keeps previous versions so old ciphertext still
 * decrypts.
 */
export function KmsConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [selected, setSelected] = useState('')
  const [plain, setPlain] = useState('hello cloudforge')
  const [cipher, setCipher] = useState('')
  const [decrypted, setDecrypted] = useState('')
  const [notice, setNotice] = useState('')
  const keys = useQuery({ queryKey: ['kms-keys'], queryFn: () => request<Rec>('/api/v1/aws/kms/keys'), retry: 1 })
  const keyRows = listOf(keys.data, 'keys')
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
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'KMS' }]}
        title="KMS — Customer managed keys"
        description="Real envelope encryption (AES-256-GCM) with keys stored in the platform store. Not AWS KMS or an HSM."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="durable local envelope encryption" />}
      />
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
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Description', render: (r) => str(r, 'description') },
            { header: 'Version', render: (r) => String(num(r, 'version')) },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Use</AwsButton>
                  <AwsButton onClick={() => rotate.mutate(str(r, 'id'))}>Rotate</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={keyRows}
          emptyMessage="No keys yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Encrypt / decrypt with: ${str(sel, 'name')} (v${String(num(sel, 'version'))})`}>
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
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Secrets Manager console (Phase 6) — mirrors the AWS Secrets
 * Manager screens. Values are envelope-encrypted with a KMS key
 * and stored encrypted at rest; list responses never include the
 * value.
 */
export function SecretsConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [kmsKey, setKmsKey] = useState('')
  const [selected, setSelected] = useState('')
  const [newValue, setNewValue] = useState('')
  const [revealed, setRevealed] = useState('')
  const [notice, setNotice] = useState('')
  const secrets = useQuery({ queryKey: ['secrets'], queryFn: () => request<Rec>('/api/v1/aws/secretsmanager/secrets'), retry: 1 })
  const secretRows = listOf(secrets.data, 'secrets')
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
    onSuccess: (d) => { setRevealed(str(d, 'value')); setSelected(str(d, 'id')); },
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
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security', to: '/services' }, { label: 'Secrets Manager' }]}
        title="Secrets Manager — Secrets"
        description="Secrets are envelope-encrypted with a KMS key and stored encrypted at rest. No automatic rotation."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="encrypted at rest · no auto-rotation" />}
      />
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
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Version', render: (r) => String(num(r, 'version')) },
            { header: 'KMS key', render: (r) => str(r, 'kms_key_id') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => reveal.mutate(str(r, 'id'))}>Show value</AwsButton>
                  <AwsButton onClick={() => { setSelected(str(r, 'id')); setRevealed('') }}>Update value</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={secretRows}
          emptyMessage="No secrets yet."
        />
      </AwsCard>
      {selected !== '' && (
        <AwsCard title="Secret value">
          {revealed !== '' && <p className="text-sm" role="status">Value: <span className="font-mono">{revealed}</span></p>}
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <input value={newValue} onChange={(e) => setNewValue(e.target.value)} placeholder="New value" className={inputCls} type="password" />
            <AwsButton onClick={() => newValue !== '' && putValue.mutate()}>Store new version</AwsButton>
          </div>
        </AwsCard>
      )}
    </div>
  )
}
