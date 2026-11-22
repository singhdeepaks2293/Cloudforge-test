import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * ElastiCache console (Phase 3) — mirrors the AWS ElastiCache
 * console's Clusters list, cluster detail (node info, keys browser,
 * flush, metrics). Lite mode is a REAL namespaced Redis database on
 * the platform Redis (key prefix + best-effort ACL user). Full mode
 * (a dedicated Redis container) is NOT built and its create returns
 * an honest 503 — the button is offered so the failure is visible,
 * never faked.
 */
export function ElastiCacheConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [mode, setMode] = useState('lite')
  const [selected, setSelected] = useState('')
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [notice, setNotice] = useState('')

  const clusters = useQuery({ queryKey: ['cache-clusters'], queryFn: () => request<Rec>('/api/v1/aws/elasticache/clusters'), retry: 1 })
  const rows = listOf(clusters.data, 'clusters')
  const keys = useQuery({
    queryKey: ['cache-keys', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/elasticache/clusters/${selected}/keys`),
    enabled: selected !== '',
    retry: 1,
  })
  const metrics = useQuery({
    queryKey: ['cache-metrics', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/elasticache/clusters/${selected}/metrics`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['cache-clusters'] })
    void queryClient.invalidateQueries({ queryKey: ['cache-keys', selected] })
    void queryClient.invalidateQueries({ queryKey: ['cache-metrics', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/elasticache/clusters', { method: 'POST', body: { name: name.trim(), engine: 'redis', mode } }),
    onSuccess: (d) => {
      setNotice(`Cluster ${String(d.name)} is available (prefix ${String(d.key_prefix)}).${typeof d.acl_note === 'string' && d.acl_note !== '' ? ` Note: ${d.acl_note}` : ''}`)
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create cluster failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (id: string) => request(`/api/v1/aws/elasticache/clusters/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Cluster deleted (its prefixed keys and ACL user were removed).')
      setSelected('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const setKeyMut = useMutation({
    mutationFn: () => request(`/api/v1/aws/elasticache/clusters/${selected}/keys`, { method: 'POST', body: { key: key.trim(), value } }),
    onSuccess: () => {
      setNotice(`Key ${key.trim()} set.`)
      setKey('')
      setValue('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Set key failed: ${e.message}`),
  })
  const flush = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/elasticache/clusters/${selected}/flush`, { method: 'POST', body: {} }),
    onSuccess: (d) => {
      setNotice(`Flushed ${String(d.keys_deleted ?? 0)} key(s) in this cluster's prefix only.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Flush failed: ${e.message}`),
  })

  const sel = rows.find((r) => String(r.id) === selected)

  return (
    <ConsoleWithHome
      icon="database"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Databases', to: '/services' }, { label: 'ElastiCache' }]}
      title="ElastiCache — Clusters"
      description="In-memory caches. Lite mode is a real namespaced Redis database on the platform Redis. Engine redis only."
      fidelity="PARTIAL"
      fidelityFootnote="real Redis, namespaced by key prefix"
      homeLabel="ElastiCache dashboard"
      listLabel="Clusters"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="ElastiCache"
            banner="Lite mode is a real namespaced Redis database on the platform Redis. A dedicated Redis container (full mode) is NOT built — full-mode creates return an honest 503."
            actions={[
              { label: 'Create cluster', primary: true, onClick: goToList },
            { label: 'Learning Center — databases track', to: '/learn' },
            ]}
            cards={[
              { label: 'Clusters', count: rows.length, onSelect: goToList },
            { label: 'Keys (selected cluster)', count: selected !== '' ? listOf(keys.data, 'keys').length : '—', onSelect: goToList, note: 'Keys browser with TTL' },
            { label: 'Replication groups', count: 0, disabledReason: 'Not modelled.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={clusters.isFetching}
            health={[
              "clusters.isError ? 'Clusters could not be loaded.' : `${rows.length} cluster(s) listed.`",
            "Memory/hit-rate metrics are platform-Redis-wide; keys-in-cluster is prefix-only.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — databases track', to: '/learn' },
            { label: 'CloudFront (unsupported — exam concept)', to: '/services/cloudfront' },
            ]}
          />
        )}
    >
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {clusters.isPending && <Loading label="Loading clusters" />}
      {clusters.isError && <ErrorBox message={`Clusters could not be loaded (${clusters.error.message}).`} />}
      {clusters.data && (
        <AwsCard title={`Clusters (${rows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="cache-name" className="text-sm font-medium">Cluster name</label>
            <input id="cache-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="sessions" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Mode
              <select value={mode} onChange={(e) => setMode(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="lite">Lite — namespaced platform Redis</option>
                <option value="full">Full — dedicated container (not built)</option>
              </select>
            </label>
            <AwsButton type="submit" disabled={create.isPending}>Create cluster</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="ElastiCache clusters"
            emptyMessage="No clusters yet. Create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Cluster', render: (c) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(c.id))}>{String(c.name)}</button> },
              { header: 'Engine', render: (c) => String(c.engine ?? 'redis') },
              { header: 'Mode', render: (c) => String(c.mode ?? '—') },
              { header: 'Status', render: (c) => String(c.status ?? '—') },
              { header: 'Endpoint', render: (c) => String(c.endpoint ?? '—') },
              {
                header: 'Actions',
                render: (c) => <AwsButton variant="secondary" disabled={remove.isPending} onClick={() => remove.mutate(String(c.id))}>Delete</AwsButton>,
              },
            ]}
          />
          {mode === 'full' && <p className="mt-2 text-sm text-aws-muted">Full mode is not built: a dedicated Redis container cannot be verified without Docker in this sandbox, so the API returns an honest 503 instead of a fake cluster.</p>}
        </AwsCard>
      )}
      {sel && (
        <>
          <AwsCard title={`Node info — ${String(sel.name)}`}>
            <dl className="grid grid-cols-2 gap-2 text-sm">
              <dt className="font-medium">Endpoint</dt><dd><code>{String(sel.endpoint ?? '—')}</code></dd>
              <dt className="font-medium">Key prefix</dt><dd><code>{String(sel.key_prefix ?? '—')}</code></dd>
              <dt className="font-medium">Mode</dt><dd>{String(sel.mode ?? '—')}</dd>
              <dt className="font-medium">Status</dt><dd>{String(sel.status ?? '—')}</dd>
            </dl>
            {typeof sel.acl_note === 'string' && sel.acl_note !== '' && <p className="mt-2 text-sm text-aws-muted">{String(sel.acl_note)}</p>}
            {typeof sel.note === 'string' && <p className="mt-1 text-sm text-aws-muted">{String(sel.note)}</p>}
          </AwsCard>
          <AwsCard title="Keys browser (read-only listing, max 100)">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (key.trim() !== '') setKeyMut.mutate()
              }}
            >
              <label htmlFor="cache-key" className="text-sm font-medium">Key</label>
              <input id="cache-key" value={key} onChange={(e) => setKey(e.target.value)} placeholder="user:1" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <label htmlFor="cache-value" className="text-sm font-medium">Value</label>
              <input id="cache-value" value={value} onChange={(e) => setValue(e.target.value)} placeholder="ada" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton type="submit" disabled={setKeyMut.isPending}>Set key</AwsButton>
              <AwsButton variant="secondary" disabled={flush.isPending} onClick={() => flush.mutate()}>Flush cluster</AwsButton>
            </form>
            {keys.isPending && <Loading label="Loading keys" />}
            {keys.data && (
              <AwsTable<Rec>
                caption="Keys"
                emptyMessage="No keys in this cluster's prefix yet."
                rows={listOf(keys.data, 'keys')}
                totalCount={Number(keys.data.count ?? 0)}
                columns={[
                  { header: 'Key', render: (k) => String(k.key) },
                  { header: 'Type', render: (k) => String(k.type ?? '—') },
                  { header: 'TTL (s)', render: (k) => (k.ttl_seconds === -1 ? 'no expiry' : String(k.ttl_seconds ?? '—')) },
                  {
                    header: 'Value',
                    render: (k) => (
                      <AwsButton
                        variant="secondary"
                        onClick={() => {
                          void request<Rec>(`/api/v1/aws/elasticache/clusters/${selected}/keys/${encodeURIComponent(String(k.key))}`).then((d) => setNotice(`Value of ${String(k.key)}: ${String(d.value ?? '')}`)).catch((e: Error) => setNotice(`Get key failed: ${e.message}`))
                        }}
                      >
                        Show value
                      </AwsButton>
                    ),
                  },
                ]}
              />
            )}
          </AwsCard>
          <AwsCard title="Metrics">
            {metrics.isPending && <Loading label="Loading metrics" />}
            {metrics.data && (
              <dl className="grid grid-cols-2 gap-2 text-sm">
                <dt className="font-medium">Used memory</dt><dd>{String(metrics.data.used_memory_human ?? metrics.data.used_memory_bytes ?? '—')}</dd>
                <dt className="font-medium">Keys in this cluster</dt><dd>{String(metrics.data.keys_in_cluster ?? '—')}</dd>
                <dt className="font-medium">Hit rate</dt><dd>{typeof metrics.data.hit_rate === 'number' ? `${(Number(metrics.data.hit_rate) * 100).toFixed(1)}%` : '—'}</dd>
                <dt className="font-medium">Keyspace hits / misses</dt><dd>{String(metrics.data.keyspace_hits ?? '—')} / {String(metrics.data.keyspace_misses ?? '—')}</dd>
              </dl>
            )}
            <p className="mt-2 text-sm text-aws-muted">Memory and hit rate are platform-Redis-wide (shared Redis in lite mode); keys-in-cluster is this cluster&apos;s prefix only.</p>
          </AwsCard>
        </>
      )}
    </ConsoleWithHome>
  )
}
