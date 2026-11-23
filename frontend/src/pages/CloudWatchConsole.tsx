import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { createAlarm, deleteAlarm, fetchMonitoring } from '../services/api.ts'
import type { Alarm, MetricFamily } from '../types/index.ts'

/**
 * CloudWatch console (Phase 2) — mirrors the AWS CloudWatch console's
 * Metrics / Alarms views, fed by the API's real Prometheus series
 * (GET /api/v1/monitoring, which gathers the live default registry)
 * and the same readiness checks /readyz runs. A series absent from
 * the list has never been observed — it is never zero-filled. Alarms
 * are threshold rules evaluated live against current values; alarm
 * definitions live in memory on the server and are lost on restart,
 * and creating one on a never-observed metric is rejected with the
 * known-metrics list. No fake metrics.
 *
 * Left nav follows the fresh v4 survey (2026-11-22): Ingestion,
 * Dashboards, Alarms, AI Operations, GenAI Observability, Application
 * Signals (APM), Infrastructure Monitoring, Omni (New), Observability
 * Workshops; collapsed Logs, Metrics, Network Monitoring, Setup
 * groups. Only Metrics and Alarms have a backend. Logs have none —
 * the API exposes no log endpoint, so Log groups / Log Insights /
 * Live tail are honest UNSUPPORTED tabs, not a fake log list. There
 * are no metric graphs: /monitoring returns current values only, with
 * no stored time series history to graph. The old-survey nav items
 * (RUM, Synthetics Canaries, Internet Monitor, Network Observability,
 * Events, X-Ray traces, Application Insights, Resource Health,
 * Settings) are NOT in the new survey's nav and are not kept.
 */
export function CloudWatchConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<string>(initialSection === 'home' ? 'home' : 'metrics')
  const [filter, setFilter] = useState('')
  const [name, setName] = useState('')
  const [metric, setMetric] = useState('cloudforge_network_engine_up')
  const [threshold, setThreshold] = useState(0.5)
  const [comparison, setComparison] = useState<'gt' | 'lt'>('gt')
  const [notice, setNotice] = useState('')

  const monitoring = useQuery({
    queryKey: ['monitoring'],
    queryFn: fetchMonitoring,
    retry: 1,
    refetchInterval: 15_000,
  })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['monitoring'] })
  const create = useMutation({
    mutationFn: () => createAlarm(name.trim(), metric, threshold, comparison),
    onSuccess: () => {
      setNotice('Alarm created (in-memory only — lost on API restart).')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create alarm failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (id: string) => deleteAlarm(id),
    onSuccess: refresh,
    onError: (e: Error) => setNotice(`Delete alarm failed: ${e.message}`),
  })

  const metrics = useMemo(() => {
    const list = monitoring.data?.metrics ?? []
    if (filter.trim() === '') return list
    const q = filter.trim().toLowerCase()
    return list.filter((m) => m.name.toLowerCase().includes(q))
  }, [monitoring.data, filter])
  const metricNames = useMemo(() => (monitoring.data?.metrics ?? []).map((m) => m.name), [monitoring.data])
  const components = monitoring.data?.components ?? {}
  const alarms = monitoring.data?.alarms ?? []
  const healthyCount = Object.values(components).filter((c) => c.status === 'ok').length
  const componentCount = Object.keys(components).length

  const metricsCard = (
    <AwsCard title={`Metrics (${metrics.length} of ${monitoring.data?.metrics.length ?? 0} families)`}>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <label htmlFor="cw-filter" className="text-sm font-medium">Filter metrics</label>
        <input
          id="cw-filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="cloudforge_…"
          className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
        />
        <span className="text-sm text-aws-muted">Refreshed every 15s. Totals are the live summed values (counters/histograms report sample sums).</span>
      </div>
      <AwsTable<MetricFamily & { id: string }>
        caption="Metric families"
        emptyMessage="No metric families match this filter."
        rows={metrics.map((m) => ({ ...m, id: m.name }))}
        totalCount={metrics.length}
        columns={[
          { header: 'Metric', render: (m) => <span className="font-mono text-xs">{m.name}</span> },
          { header: 'Type', render: (m) => m.type },
          { header: 'Total', render: (m) => String(m.total) },
          { header: 'Samples', render: (m) => String(m.samples.length) },
          { header: 'Help', render: (m) => m.help ?? '—' },
        ]}
      />
      <p className="mt-2 text-xs text-aws-muted">
        No metric graphs: the /monitoring API returns current values only, with no stored time series history to
        graph. A series that has never been observed is absent, not zero-filled.
      </p>
    </AwsCard>
  )
  const alarmsCard = (
    <AwsCard title={`Alarms (${alarms.length})`}>
      <form
        className="mb-3 flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim() !== '') create.mutate()
        }}
      >
        <label htmlFor="cw-alarm-name" className="text-sm font-medium">Alarm name</label>
        <input id="cw-alarm-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="engine-down" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
        <label className="text-sm">
          Metric
          <select value={metric} onChange={(e) => setMetric(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            {metricNames.map((m) => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>
        </label>
        <label className="text-sm">
          Comparison
          <select value={comparison} onChange={(e) => setComparison(e.target.value as 'gt' | 'lt')} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
            <option value="gt">&gt; (greater than)</option>
            <option value="lt">&lt; (less than)</option>
          </select>
        </label>
        <label className="text-sm">
          Threshold
          <input type="number" step="any" value={threshold} onChange={(e) => setThreshold(Number(e.target.value))} className="ml-2 w-24 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        </label>
        <AwsButton type="submit" disabled={name.trim() === '' || metric === ''}>Create alarm</AwsButton>
      </form>
      <p className="mb-2 text-sm text-aws-muted">
        Alarms evaluate live against current values. Definitions are stored in memory on the server and are
        lost on restart. Creating an alarm on a metric that was never observed is rejected.
      </p>
      <AwsTable<Alarm & { id: string }>
        caption="Alarms"
        emptyMessage="No alarms yet — create one above."
        rows={alarms}
        totalCount={alarms.length}
        columns={[
          { header: 'Alarm', render: (a) => a.name },
          { header: 'Metric', render: (a) => <span className="font-mono text-xs">{a.metric}</span> },
          { header: 'Rule', render: (a) => `${a.comparison === 'gt' ? '>' : '<'} ${a.threshold}` },
          { header: 'Current value', render: (a) => (a.current_value !== undefined ? String(a.current_value) : 'no data') },
          { header: 'State', render: (a) => <StateBadge state={a.state === 'ok' ? 'READY' : a.state === 'alarm' ? 'FAILED' : 'STOPPED'} /> },
          { header: 'Actions', render: (a) => <button type="button" className="text-aws-link hover:underline" onClick={() => remove.mutate(a.id)}>Delete</button> },
        ]}
      />
    </AwsCard>
  )
  const healthCard = monitoring.data ? (
    <AwsCard title={`Component health — ${monitoring.data.status} (checked ${new Date(monitoring.data.checked_at).toLocaleString()})`}>
      <AwsTable<{ id: string; name: string; status: string; latency_ms?: number; error?: string }>
        caption="Component health"
        emptyMessage="No components reported."
        rows={Object.entries(components).map(([name, c]) => ({ id: name, name, ...c }))}
        totalCount={Object.keys(components).length}
        columns={[
          { header: 'Component', render: (c) => c.name },
          { header: 'Status', render: (c) => <StateBadge state={c.status === 'ok' ? 'READY' : c.status === 'down' ? 'FAILED' : 'STOPPED'} /> },
          { header: 'Latency', render: (c) => (c.latency_ms !== undefined ? `${c.latency_ms} ms` : '—') },
          { header: 'Detail', render: (c) => c.error ?? '—' },
        ]}
      />
      <p className="mt-2 text-sm text-aws-muted">
        API uptime {monitoring.data.uptime_seconds}s · version {monitoring.data.version} · mode{' '}
        {monitoring.data.lite ? 'lite' : 'full'} · {monitoring.data.note ?? ''}
      </p>
      <p className="mt-1 text-xs text-aws-muted">
        CloudForge component health (live /readyz checks) — not AWS Resource Health, which has no counterpart here.
      </p>
    </AwsCard>
  ) : null

  return (
    <ServiceConsole
      icon="activity"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'CloudWatch' }]}
      title="CloudWatch — Metrics & Alarms"
      description="Live metrics from the API's Prometheus registry and live component health from /readyz. A series that has never been observed is absent, not zero."
      fidelity="PARTIAL"
      fidelityFootnote="real Prometheus series · alarms in-memory"
      subNav={[
        { id: 'home', label: 'Overview' },
        { id: 'ingestion', label: 'Ingestion' },
        { id: 'dashboards', label: 'Dashboards' },
        { id: 'alarms', label: 'Alarms' },
        { id: 'ai-operations', label: 'AI Operations' },
        { id: 'genai-observability', label: 'GenAI Observability' },
        { id: 'app-signals', label: 'Application Signals' },
        { id: 'infrastructure-monitoring', label: 'Infrastructure Monitoring' },
        { id: 'omni', label: 'Omni (New)' },
        { id: 'observability-workshops', label: 'Observability Workshops' },
        { id: 'log-groups', label: 'Log groups', group: 'Logs' },
        { id: 'log-insights', label: 'Log Insights', group: 'Logs' },
        { id: 'contributor-insights', label: 'Contributor Insights', group: 'Logs', disabledReason: 'UNSUPPORTED — no log store in CloudForge.' },
        { id: 'live-tail', label: 'Live tail', group: 'Logs' },
        { id: 'metrics', label: 'Metrics', group: 'Metrics' },
        { id: 'net-monitor', label: 'Network Monitor', group: 'Network Monitoring' },
        { id: 'setup', label: 'Setup', group: 'Setup' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>
      )}
      {monitoring.isPending && <Loading label="Loading monitoring data" />}
      {monitoring.isError && <ErrorBox message={`Monitoring could not be loaded (${monitoring.error.message}).`} />}
      {section === 'home' && monitoring.data && (
        <ServiceHomeDashboard
          serviceName="CloudWatch"
          banner="Live metrics from the API's Prometheus registry and live component health from /readyz. No metric graphs: the API returns current values only, with no stored time series history to graph."
          actions={[
            { label: 'View metrics', primary: true, onClick: () => setSection('metrics') },
            { label: 'View alarms', onClick: () => setSection('alarms') },
          ]}
          cards={[
            { label: 'Alarms', count: alarms.length, onSelect: () => setSection('alarms'), note: 'In-memory — lost on API restart' },
            { label: 'Metric families', count: metrics.length, onSelect: () => setSection('metrics'), note: 'Only series observed at least once' },
            { label: 'Component health', count: `${healthyCount} of ${componentCount} healthy`, note: 'Live /readyz checks' },
          ]}
          onRefresh={refresh}
          refreshing={monitoring.isFetching}
          health={[
            `Monitoring status: ${monitoring.data.status} — ${metrics.length} metric familie(s), ${alarms.length} alarm(s).`,
            'A series that has never been observed is absent, not zero-filled.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — monitoring track', to: '/learn' },
            { label: 'CloudTrail (event history)', to: '/services/cloudtrail' },
          ]}
        >
          {healthCard}
        </ServiceHomeDashboard>
      )}
      {section === 'ingestion' && (
        <UnsupportedSection
          title="Ingestion"
          reason="Ingestion (metric / log ingestion configuration) has no backend in CloudForge — metrics come from the API's own Prometheus registry and there is no ingestion pipeline to configure."
          actionLabel="Configure ingestion"
        />
      )}
      {section === 'dashboards' && (
        <UnsupportedSection
          title="Dashboards"
          reason="Saved CloudWatch dashboards have no backend in CloudForge — the /monitoring API returns current values only, with no dashboard persistence and no stored time series to put on a dashboard."
          actionLabel="Create dashboard"
        />
      )}
      {section === 'ai-operations' && (
        <UnsupportedSection
          title="AI Operations"
          reason="AI Operations has no backend in CloudForge — there is no AI-assisted operations analysis of metrics, alarms or logs."
          actionLabel="Open AI Operations"
        />
      )}
      {section === 'genai-observability' && (
        <UnsupportedSection
          title="GenAI Observability"
          reason="GenAI Observability has no backend in CloudForge — there are no generative-AI workloads or model-invocation telemetry to observe."
          actionLabel="Open GenAI Observability"
        />
      )}
      {section === 'app-signals' && (
        <UnsupportedSection
          title="Application Signals"
          reason="Application Signals (APM) has no backend in CloudForge — requests are not traced and there is no application-performance data to show."
          actionLabel="Enable Application Signals"
        />
      )}
      {section === 'infrastructure-monitoring' && (
        <UnsupportedSection
          title="Infrastructure Monitoring"
          reason="Infrastructure Monitoring has no backend in CloudForge beyond the live component health on the Overview — there is no per-resource infrastructure monitoring view."
          actionLabel="Open Infrastructure Monitoring"
        />
      )}
      {section === 'omni' && (
        <UnsupportedSection
          title="Omni"
          reason="Omni has no backend in CloudForge — there is no unified observability view beyond Metrics, Alarms and the component health on the Overview."
          actionLabel="Open Omni"
        />
      )}
      {section === 'observability-workshops' && (
        <UnsupportedSection
          title="Observability Workshops"
          reason="Observability Workshops have no backend in CloudForge — workshops are guided learning content, not a console capability; see the Learning Center instead."
          actionLabel="Open Observability Workshops"
        />
      )}
      {section === 'metrics' && monitoring.data && metricsCard}
      {section === 'alarms' && monitoring.data && alarmsCard}
      {section === 'log-groups' && (
        <UnsupportedSection
          title="Log groups"
          reason="Log groups have no backend in CloudForge — the API exposes no log endpoint and stores no logs, so there is no log group to list. API logs go to the server's stdout on the host; they are not queryable from this console."
          actionLabel="Create log group"
        />
      )}
      {section === 'log-insights' && (
        <UnsupportedSection
          title="Log Insights"
          reason="Log Insights (querying logs) has no backend in CloudForge — there is no log store to query."
          actionLabel="Run query"
        />
      )}
      {section === 'live-tail' && (
        <UnsupportedSection
          title="Live tail"
          reason="Live tail has no backend in CloudForge — there is no log stream to tail; the API exposes no log endpoint."
          actionLabel="Start live tail"
        />
      )}
      {section === 'net-monitor' && (
        <UnsupportedSection
          title="Network Monitor"
          reason="Network Monitor has no backend in CloudForge — there is no network monitoring beyond the live component health on the Overview."
          actionLabel="Open Network Monitor"
        />
      )}
      {section === 'setup' && (
        <UnsupportedSection
          title="Setup"
          reason="CloudWatch setup has no backend in CloudForge — there is no CloudWatch settings store to configure."
          actionLabel="Save setup"
        />
      )}
    </ServiceConsole>
  )
}
