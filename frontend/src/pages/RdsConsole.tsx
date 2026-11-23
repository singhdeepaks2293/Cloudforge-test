import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * RDS console (Phase 3) — mirrors the AWS RDS console's Databases
 * list; selecting a database shows stacked detail cards
 * (Connectivity & security, Maintenance & backups) and creation is
 * an inline form, not a wizard. There are no Monitoring, Logs &
 * events or Configuration tabs. Each instance is a REAL Postgres
 * database + role on the platform Postgres ("PostgreSQL-compatible,
 * not Aurora"). The master password is shown exactly once at
 * creation. Start/Stop are UNSUPPORTED (a shared platform database
 * cannot be stopped per instance) and are rendered disabled with the
 * reason. Snapshots are real pg_dump -Fc files; restore creates a
 * new instance from a snapshot.
 *
 * v4 survey (2026-11-22) left nav: Dashboard, Databases, CloudWatch
 * Database Insights, Snapshots, Exports in Amazon S3, Automated
 * backups, Reserved instances, Capacity reservations (New), Proxies,
 * Subnet groups, Parameter groups, Option groups, Custom engine
 * versions, Zero-ETL integrations, Events, Event subscriptions,
 * Recommendations, Certificate update. Only Dashboard, Databases and
 * Snapshots (per-instance, on the database detail) have a backend;
 * the rest are honest UNSUPPORTED sections. Databases columns are NOT
 * OBSERVABLE in the survey, so the existing columns are kept, not
 * invented.
 */
export function RdsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'databases')
  const [name, setName] = useState('')
  const [version, setVersion] = useState('16')
  const [selected, setSelected] = useState('')
  const [created, setCreated] = useState<Rec | null>(null)
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')

  const instances = useQuery({ queryKey: ['rds-instances'], queryFn: () => request<Rec>('/api/v1/aws/rds/instances'), retry: 1 })
  const rows = listOf(instances.data, 'instances').filter(
    (r) => filter.trim() === '' || String(r.name ?? '').toLowerCase().includes(filter.toLowerCase()) || String(r.id).toLowerCase().includes(filter.toLowerCase()),
  )
  const detail = useQuery({
    queryKey: ['rds-instance', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/rds/instances/${selected}`),
    enabled: selected !== '',
    retry: 1,
  })
  const snapshots = useQuery({
    queryKey: ['rds-snapshots', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/rds/instances/${selected}/snapshots`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['rds-instances'] })
    void queryClient.invalidateQueries({ queryKey: ['rds-instance', selected] })
    void queryClient.invalidateQueries({ queryKey: ['rds-snapshots', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/rds/instances', { method: 'POST', body: { name: name.trim(), engine: 'postgres', version } }),
    onSuccess: (d) => {
      setCreated(d)
      setNotice(`Instance ${String(d.name)} is available. Save the master password now — it is shown once and never again.`)
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create instance failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (id: string) => request(`/api/v1/aws/rds/instances/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Instance deleted (database + role dropped).')
      setSelected('')
      setCreated(null)
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const snapshot = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/rds/instances/${selected}/snapshots`, { method: 'POST', body: {} }),
    onSuccess: (d) => {
      setNotice(`Snapshot ${String(d.name)} created (${String(d.size_bytes ?? 0)} bytes, real pg_dump file).`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Snapshot failed: ${e.message}`),
  })

  const sel = rows.find((r) => String(r.id) === selected) ?? detail.data

  return (
    <ServiceConsole
      icon="database"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Databases', to: '/services' }, { label: 'RDS' }]}
      title="RDS — Databases"
      description="Relational databases. Each instance is a real Postgres database + role on the platform Postgres — PostgreSQL-compatible, not Aurora. Engine postgres only."
      fidelity="PARTIAL"
      fidelityFootnote="real Postgres database + role · not Aurora"
      subNav={[
        { id: 'home', label: 'Dashboard' },
        { id: 'databases', label: 'Databases' },
        { id: 'insights', label: 'CloudWatch Database Insights' },
        { id: 'snapshots', label: 'Snapshots' },
        { id: 'exports', label: 'Exports in Amazon S3' },
        { id: 'backups', label: 'Automated backups' },
        { id: 'reserved', label: 'Reserved instances' },
        { id: 'capacity', label: 'Capacity reservations', badge: 'New' },
        { id: 'proxies', label: 'Proxies' },
        { id: 'subnet-groups', label: 'Subnet groups' },
        { id: 'param-groups', label: 'Parameter groups' },
        { id: 'option-groups', label: 'Option groups' },
        { id: 'engine-versions', label: 'Custom engine versions' },
        { id: 'zero-etl', label: 'Zero-ETL integrations' },
        { id: 'events', label: 'Events' },
        { id: 'event-subs', label: 'Event subscriptions' },
        { id: 'recommendations', label: 'Recommendations' },
        { id: 'cert-update', label: 'Certificate update' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="RDS"
            banner="Each instance is a real Postgres database + role on the platform Postgres — PostgreSQL-compatible, not Aurora. Engine postgres only."
            actions={[
              { label: 'Create database', primary: true, onClick: () => setSection('databases') },
            { label: 'Open Backup console', to: '/services/backup' },
            ]}
            cards={[
              { label: 'DB instances', count: rows.length, onSelect: () => setSection('databases'), note: 'Real Postgres databases + roles' },
            { label: 'Snapshots', count: selected !== '' ? listOf(snapshots.data, 'snapshots').length : '—', onSelect: () => setSection('databases'), note: 'Real pg_dump -Fc files' },
            { label: 'Parameter groups', count: 0, onSelect: () => setSection('param-groups'), note: 'UNSUPPORTED — no parameter-group API.' },
            { label: 'Aurora clusters', count: 0, disabledReason: 'Aurora is UNSUPPORTED — see the Aurora console.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={instances.isFetching}
            health={[
              instances.isError ? 'Databases could not be loaded.' : `${rows.length} DB instance(s) listed.`,
            "Start/Stop is UNSUPPORTED — a shared platform database cannot be stopped per instance.",
            "No Multi-AZ; no containerised per-instance Postgres.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — databases track', to: '/learn' },
            { label: 'Aurora (unsupported — exam concept)', to: '/services/aurora' },
            ]}
          />
      )}
      {section === 'databases' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {created && typeof created.password === 'string' && (
        <AwsCard title="Master password — shown once">
          <p className="text-sm">
            Instance <strong>{String(created.name)}</strong> · username <code>{String(created.username)}</code> · endpoint <code>{String(created.endpoint)}</code>
          </p>
          <p className="mt-1 font-mono text-sm">Password: {String(created.password)}</p>
          <p className="mt-1 text-sm">Connection: <code>{String(created.connection)}</code></p>
          <p className="mt-1 text-sm text-aws-muted">Only the SHA-256 hash is stored. Save this password now — it cannot be shown again.</p>
          <AwsButton variant="secondary" onClick={() => setCreated(null)}>Hide</AwsButton>
        </AwsCard>
      )}
      {instances.isPending && <Loading label="Loading databases" />}
      {instances.isError && <ErrorBox message={`Databases could not be loaded (${instances.error.message}).`} />}
      {instances.data && (
        <AwsCard title={`Databases (${rows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="rds-name" className="text-sm font-medium">DB instance identifier</label>
            <input id="rds-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="appdb" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Engine version
              <select value={version} onChange={(e) => setVersion(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="16">PostgreSQL 16</option>
                <option value="15">PostgreSQL 15</option>
                <option value="14">PostgreSQL 14</option>
              </select>
            </label>
            <AwsButton type="submit" disabled={create.isPending}>Create database</AwsButton>
            <span className="text-sm text-aws-muted">MySQL / MariaDB are rejected by the API, not faked.</span>
          </form>
          <AwsTable<Rec>
            caption="RDS databases"
            emptyMessage="No DB instances yet. Create one above."
            rows={rows}
            totalCount={listOf(instances.data, 'instances').length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter databases by name"
            columns={[
              { header: 'DB identifier', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(r.id))}>{String(r.name)}</button> },
              { header: 'Engine', render: (r) => `PostgreSQL ${String(r.version ?? '')}` },
              { header: 'Status', render: (r) => <StateBadge state={String(r.status ?? '').toUpperCase() === 'AVAILABLE' ? 'READY' : String(r.status ?? '')} /> },
              { header: 'Endpoint', render: (r) => String(r.endpoint ?? '—') },
              { header: 'Database', render: (r) => String(r.database_name ?? '—') },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" disabled title="Start/stop is unsupported: this instance is a database on the shared platform Postgres, which cannot be stopped per instance.">Start</AwsButton>
                    <AwsButton variant="secondary" disabled title="Start/stop is unsupported: this instance is a database on the shared platform Postgres, which cannot be stopped per instance.">Stop</AwsButton>
                    <AwsButton variant="secondary" disabled={remove.isPending} onClick={() => remove.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
          <p className="mt-2 text-sm text-aws-muted">No Multi-AZ. No containerised per-instance Postgres (not built — no Docker in this sandbox to verify it against).</p>
        </AwsCard>
      )}
      {sel && (
        <>
          <AwsCard title={`Connectivity & security — ${String(sel.name)}`}>
            <dl className="grid grid-cols-2 gap-2 text-sm">
              <dt className="font-medium">Endpoint</dt><dd><code>{String(sel.endpoint ?? '—')}</code></dd>
              <dt className="font-medium">Database name</dt><dd>{String(sel.database_name ?? '—')}</dd>
              <dt className="font-medium">Master username</dt><dd>{String(sel.username ?? '—')}</dd>
              <dt className="font-medium">Engine / version</dt><dd>PostgreSQL {String(sel.version ?? '')}</dd>
              <dt className="font-medium">Status</dt><dd>{String(sel.status ?? '—')}</dd>
            </dl>
            <p className="mt-2 text-sm text-aws-muted">Connect with psql using the endpoint, database name and master username above and the password shown once at creation. There is no per-instance security-group model: access is governed by the platform Postgres.</p>
          </AwsCard>
          <AwsCard title="Maintenance & backups — Snapshots">
            <div className="mb-3 flex items-center gap-2">
              <AwsButton disabled={snapshot.isPending} onClick={() => snapshot.mutate()}>Take snapshot</AwsButton>
              <span className="text-sm text-aws-muted">Snapshots are real pg_dump -Fc files on the API host. Restore creates a new instance from a snapshot.</span>
            </div>
            <AwsTable<Rec>
              caption="Snapshots"
              emptyMessage="No snapshots yet."
              rows={listOf(snapshots.data, 'snapshots')}
              totalCount={listOf(snapshots.data, 'snapshots').length}
              columns={[
                { header: 'Snapshot', render: (s) => String(s.name) },
                { header: 'Size', render: (s) => `${String(s.size_bytes ?? 0)} bytes` },
                { header: 'Status', render: (s) => String(s.status ?? '—') },
                { header: 'Created', render: (s) => String(s.created_at ?? '—') },
                {
                  header: 'Actions',
                  render: (s) => (
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        const target = `${String(sel.name)}-restored`
                        void request<Rec>(`/api/v1/aws/rds/snapshots/${String(s.id)}/restore`, { method: 'POST', body: { name: target } }).then((d) => {
                          setCreated(d)
                          setNotice(`Restored as new instance ${target}. Save its password — shown once.`)
                          refresh()
                        }).catch((e: Error) => setNotice(`Restore failed: ${e.message}`))
                      }}
                    >
                      Restore as new instance
                    </AwsButton>
                  ),
                },
              ]}
            />
          </AwsCard>
          <AwsCard title="Monitoring / Logs & events">
            <p className="text-sm text-aws-muted">
              Per-instance CloudWatch-style metrics are not built. Per-instance log streaming is not built either: this database runs on the
              shared platform Postgres, whose logs live in the platform Postgres log on the host. Audit events for this instance are in CloudTrail.
            </p>
          </AwsCard>
        </>
      )}
        </>
      )}
      {section === 'snapshots' && (
        <AwsCard title="Snapshots">
          <p className="text-sm text-aws-muted">
            Snapshots are listed per database — there is no global snapshot list endpoint, so open a database on
            the Databases page to see its snapshots. Snapshots are real pg_dump -Fc files on the API host, and
            restoring one creates a new instance.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setSection('databases')}>Open Databases</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'insights' && (
        <UnsupportedSection
          title="CloudWatch Database Insights"
          reason="CloudWatch Database Insights is UNSUPPORTED in CloudForge — there is no Database Insights API and no per-instance performance data is collected."
          actionLabel="Enable Database Insights"
        />
      )}
      {section === 'exports' && (
        <UnsupportedSection
          title="Exports in Amazon S3"
          reason="Exports in Amazon S3 are UNSUPPORTED in CloudForge — there is no snapshot-export API; snapshots are pg_dump files on the API host, not S3 objects."
          actionLabel="Export snapshot to S3"
        />
      )}
      {section === 'backups' && (
        <UnsupportedSection
          title="Automated backups"
          reason="Automated backups are UNSUPPORTED in CloudForge — there is no backup window or retention API; backups are manual snapshots taken per database."
          actionLabel="Create automated backup"
        />
      )}
      {section === 'reserved' && (
        <UnsupportedSection
          title="Reserved instances"
          reason="Reserved instances are UNSUPPORTED in CloudForge — there is no reservation or billing API and no usage billing to reserve against."
          actionLabel="Purchase Reserved Instance"
        />
      )}
      {section === 'capacity' && (
        <UnsupportedSection
          title="Capacity reservations"
          reason="Capacity reservations are UNSUPPORTED in CloudForge — there is no capacity-reservation API; capacity is the shared platform Postgres."
          actionLabel="Create capacity reservation"
        />
      )}
      {section === 'proxies' && (
        <UnsupportedSection
          title="Proxies"
          reason="RDS Proxies are UNSUPPORTED in CloudForge — there is no proxy API; connections go directly to the platform Postgres endpoint."
          actionLabel="Create proxy"
        />
      )}
      {section === 'subnet-groups' && (
        <UnsupportedSection
          title="Subnet groups"
          reason="Subnet groups are UNSUPPORTED in CloudForge — there is no subnet-group API; databases run on the shared platform Postgres, not in a lab VPC."
          actionLabel="Create DB subnet group"
        />
      )}
      {section === 'param-groups' && (
        <UnsupportedSection
          title="Parameter groups"
          reason="Parameter groups are UNSUPPORTED in CloudForge — there is no parameter-group API; the platform Postgres runs with its host configuration."
          actionLabel="Create parameter group"
        />
      )}
      {section === 'option-groups' && (
        <UnsupportedSection
          title="Option groups"
          reason="Option groups are UNSUPPORTED in CloudForge — there is no option-group API; the engine is PostgreSQL only."
          actionLabel="Create option group"
        />
      )}
      {section === 'engine-versions' && (
        <UnsupportedSection
          title="Custom engine versions"
          reason="Custom engine versions are UNSUPPORTED in CloudForge — there is no custom-engine API; PostgreSQL 14/15/16 are the only engines."
          actionLabel="Create custom engine version"
        />
      )}
      {section === 'zero-etl' && (
        <UnsupportedSection
          title="Zero-ETL integrations"
          reason="Zero-ETL integrations are UNSUPPORTED in CloudForge — there is no Zero-ETL API and no integration target."
          actionLabel="Create Zero-ETL integration"
        />
      )}
      {section === 'events' && (
        <UnsupportedSection
          title="Events"
          reason="RDS Events are UNSUPPORTED in CloudForge — there is no RDS event feed; audit events for databases are in CloudTrail."
          actionLabel="View events"
        />
      )}
      {section === 'event-subs' && (
        <UnsupportedSection
          title="Event subscriptions"
          reason="Event subscriptions are UNSUPPORTED in CloudForge — there is no event-subscription API."
          actionLabel="Create event subscription"
        />
      )}
      {section === 'recommendations' && (
        <UnsupportedSection
          title="Recommendations"
          reason="Recommendations are UNSUPPORTED in CloudForge — there is no recommendation engine; the survey records 0 recommendations."
          actionLabel="View recommendations"
        />
      )}
      {section === 'cert-update' && (
        <UnsupportedSection
          title="Certificate update"
          reason="Certificate update is UNSUPPORTED in CloudForge — there is no certificate API; the platform Postgres uses its host TLS configuration."
          actionLabel="Update certificate"
        />
      )}
    </ServiceConsole>
  )
}
