import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge, StateBadge } from '../components/AwsBadge.tsx'
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
 */
export function CloudWatchConsole() {
  const queryClient = useQueryClient()
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

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'CloudWatch' }]}
        title="CloudWatch — Metrics & Alarms"
        description="Live metrics from the API's Prometheus registry and live component health from /readyz. A series that has never been observed is absent, not zero."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="real Prometheus series · alarms in-memory" />}
      />
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>
      )}
      {monitoring.isPending && <Loading label="Loading monitoring data" />}
      {monitoring.isError && <ErrorBox message={`Monitoring could not be loaded (${monitoring.error.message}).`} />}
      {monitoring.data && (
        <>
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
          </AwsCard>
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
          <AwsCard title={`Metrics (${metrics.length} of ${monitoring.data.metrics.length} families)`}>
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
          </AwsCard>
        </>
      )}
    </div>
  )
}
