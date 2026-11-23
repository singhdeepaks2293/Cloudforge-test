import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
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

/**
 * ElastiCache console (Phase 3) — mirrors the AWS ElastiCache
 * console's Clusters list, cluster detail (node info, keys browser,
 * flush, metrics). Lite mode is a REAL namespaced Redis database on
 * the platform Redis (key prefix + best-effort ACL user). Full mode
 * (a dedicated Redis container) is NOT built and its create returns
 * an honest 503 — the button is offered so the failure is visible,
 * never faked.
 *
 * v4 survey (2026-11-22) left nav: Dashboard; Resources (Valkey
 * caches, Memcached caches, Redis OSS caches, Global datastores,
 * Reserved nodes, Backups); Configurations (Subnet groups, Parameter
 * groups, User management, User group management); Migrations, Events,
 * Service updates, ElastiCache cluster client, Documentation, Amazon
 * MemoryDB. Valkey caches columns (6): Cache name, Status,
 * Description, Engine version, Configuration, Created date. Only
 * Cache name, Status, Configuration (the lite/full mode) and Created
 * date have real values; Description and Engine version are not
 * stored, so they render '—'. The single clusters list is the Valkey
 * list — there is no separate Memcached or Redis OSS list.
 */
export function ElastiCacheConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'valkey')
  const [name, setName] = useState('')
  const [mode, setMode] = useState('lite')
  const [selected, setSelected] = useState('')
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')

  const clusters = useQuery({ queryKey: ['cache-clusters'], queryFn: () => request<Rec>('/api/v1/aws/elasticache/clusters'), retry: 1 })
  const rows = listOf(clusters.data, 'clusters').filter(
    (r) => filter.trim() === '' || String(r.name ?? '').toLowerCase().includes(filter.toLowerCase()) || String(r.id).toLowerCase().includes(filter.toLowerCase()),
  )
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
    <ServiceConsole
      icon="database"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Databases', to: '/services' }, { label: 'ElastiCache' }]}
      title="ElastiCache — Clusters"
      description="In-memory caches. Lite mode is a real namespaced Redis database on the platform Redis. Engine redis only."
      fidelity="PARTIAL"
      fidelityFootnote="real Redis, namespaced by key prefix"
      subNav={[
        { id: 'home', label: 'Dashboard' },
        { id: 'valkey', label: 'Valkey caches', group: 'Resources' },
        { id: 'memcached', label: 'Memcached caches', group: 'Resources' },
        { id: 'redis', label: 'Redis OSS caches', group: 'Resources' },
        { id: 'global-datastores', label: 'Global datastores', group: 'Resources' },
        { id: 'reserved-nodes', label: 'Reserved nodes', group: 'Resources' },
        { id: 'backups', label: 'Backups', group: 'Resources' },
        { id: 'subnet-groups', label: 'Subnet groups', group: 'Configurations' },
        { id: 'param-groups', label: 'Parameter groups', group: 'Configurations' },
        { id: 'user-management', label: 'User management', group: 'Configurations' },
        { id: 'user-group-management', label: 'User group management', group: 'Configurations' },
        { id: 'migrations', label: 'Migrations' },
        { id: 'events', label: 'Events' },
        { id: 'service-updates', label: 'Service updates' },
        { id: 'cluster-client', label: 'ElastiCache cluster client' },
        { id: 'documentation', label: 'Documentation' },
        { id: 'memorydb', label: 'Amazon MemoryDB' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="ElastiCache"
            banner="Lite mode is a real namespaced Redis database on the platform Redis. A dedicated Redis container (full mode) is NOT built — full-mode creates return an honest 503."
            actions={[
              { label: 'Create cluster', primary: true, onClick: () => setSection('valkey') },
            { label: 'Learning Center — databases track', to: '/learn' },
            ]}
            cards={[
              { label: 'Clusters', count: rows.length, onSelect: () => setSection('valkey') },
            { label: 'Keys (selected cluster)', count: selected !== '' ? listOf(keys.data, 'keys').length : '—', onSelect: () => setSection('valkey'), note: 'Keys browser with TTL' },
            { label: 'Replication groups', count: 0, disabledReason: 'Not modelled.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={clusters.isFetching}
            health={[
              clusters.isError ? 'Clusters could not be loaded.' : `${rows.length} cluster(s) listed.`,
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
      {section === 'valkey' && (
        <>
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
            totalCount={listOf(clusters.data, 'clusters').length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter clusters by name"
            columns={[
              // Valkey caches columns in the v4 survey's exact order
              // (6). Real values: Cache name, Status, Configuration
              // (the lite/full mode) and Created date. Description and
              // Engine version are not stored for a cluster, so they
              // render '—'. Actions (Delete) is CloudForge-only.
              { header: 'Cache name', render: (c) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(c.id))}>{String(c.name)}</button> },
              { header: 'Status', render: (c) => String(c.status ?? '—') },
              { header: 'Description', render: () => '—' },
              { header: 'Engine version', render: () => '—' },
              { header: 'Configuration', render: (c) => String(c.mode ?? '—') },
              { header: 'Created date', render: (c) => String(c.created_at ?? '—') },
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
          <AwsCard title="Keys browser (set key, flush, listing max 100)">
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
        </>
      )}
      {section === 'memcached' && (
        <AwsCard title="Memcached caches">
          <p className="text-sm text-aws-muted">
            Memcached caches are <strong>UNSUPPORTED</strong> in CloudForge — the backend supports engine redis
            only (Memcached is rejected by the API, not faked). All CloudForge clusters are listed under Valkey
            caches.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setSection('valkey')}>Open Valkey caches</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'redis' && (
        <AwsCard title="Redis OSS caches">
          <p className="text-sm text-aws-muted">
            CloudForge clusters are Redis-compatible and are listed under Valkey caches — there is no separate
            Redis OSS list. Valkey is the Redis OSS fork CloudForge&apos;s list follows.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setSection('valkey')}>Open Valkey caches</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'global-datastores' && (
        <UnsupportedSection
          title="Global datastores"
          reason="Global datastores are UNSUPPORTED in CloudForge — there is no global-datastore API; clusters are local to this deployment."
          actionLabel="Create global datastore"
        />
      )}
      {section === 'reserved-nodes' && (
        <UnsupportedSection
          title="Reserved nodes"
          reason="Reserved nodes are UNSUPPORTED in CloudForge — there is no reservation API and no usage billing to reserve against."
          actionLabel="Purchase reserved node"
        />
      )}
      {section === 'backups' && (
        <UnsupportedSection
          title="Backups"
          reason="Backups are UNSUPPORTED in CloudForge — there is no cache-backup API; keys live in the platform Redis and are removed with the cluster."
          actionLabel="Create backup"
        />
      )}
      {section === 'subnet-groups' && (
        <UnsupportedSection
          title="Subnet groups"
          reason="Subnet groups are UNSUPPORTED in CloudForge — there is no subnet-group API; clusters run on the shared platform Redis, not in a lab VPC."
          actionLabel="Create subnet group"
        />
      )}
      {section === 'param-groups' && (
        <UnsupportedSection
          title="Parameter groups"
          reason="Parameter groups are UNSUPPORTED in CloudForge — there is no parameter-group API; the platform Redis runs with its host configuration."
          actionLabel="Create parameter group"
        />
      )}
      {section === 'user-management' && (
        <UnsupportedSection
          title="User management"
          reason="User management is UNSUPPORTED in CloudForge — there is no cache-user API; lite clusters get a best-effort ACL user created by the backend, not a managed user."
          actionLabel="Create user"
        />
      )}
      {section === 'user-group-management' && (
        <UnsupportedSection
          title="User group management"
          reason="User group management is UNSUPPORTED in CloudForge — there is no user-group API."
          actionLabel="Create user group"
        />
      )}
      {section === 'migrations' && (
        <UnsupportedSection
          title="Migrations"
          reason="Migrations are UNSUPPORTED in CloudForge — there is no cache-migration API."
          actionLabel="Start migration"
        />
      )}
      {section === 'events' && (
        <UnsupportedSection
          title="Events"
          reason="ElastiCache Events are UNSUPPORTED in CloudForge — there is no cache event feed; audit events for clusters are in CloudTrail."
          actionLabel="View events"
        />
      )}
      {section === 'service-updates' && (
        <UnsupportedSection
          title="Service updates"
          reason="Service updates are UNSUPPORTED in CloudForge — there is no service-update API; the platform Redis is managed by the host."
          actionLabel="View service updates"
        />
      )}
      {section === 'cluster-client' && (
        <UnsupportedSection
          title="ElastiCache cluster client"
          reason="The ElastiCache cluster client is UNSUPPORTED in CloudForge — there is no cluster-client download; connect with a normal Redis client to the cluster endpoint shown on the Valkey caches list."
          actionLabel="Download cluster client"
        />
      )}
      {section === 'documentation' && (
        <AwsCard title="Documentation">
          <p className="text-sm text-aws-muted">
            CloudForge has no in-console ElastiCache documentation page. The Learning Center covers caches as part
            of the databases track.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => window.location.assign('/learn')}>Open Learning Center</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'memorydb' && (
        <UnsupportedSection
          title="Amazon MemoryDB"
          reason="Amazon MemoryDB is UNSUPPORTED in CloudForge — there is no MemoryDB API; ElastiCache clusters are the only in-memory stores here."
          actionLabel="Create MemoryDB cluster"
        />
      )}
    </ServiceConsole>
  )
}
