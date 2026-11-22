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
 * DMS console (Phase 6) — mirrors the AWS Database Migration
 * Service screens: replication tasks between the platform Postgres
 * and RDS instances, with real row counts and a migration log.
 * Full-load only, ≤5000 rows/table, no CDC.
 */
export function DmsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sourceKind, setSourceKind] = useState('platform')
  const [sourceInst, setSourceInst] = useState('')
  const [targetKind, setTargetKind] = useState('rds')
  const [targetInst, setTargetInst] = useState('')
  const [tables, setTables] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const tasks = useQuery({ queryKey: ['dms-tasks'], queryFn: () => request<Rec>('/api/v1/aws/dms/tasks'), retry: 1 })
  const taskRows = listOf(tasks.data, 'tasks')
  const logs = useQuery({
    queryKey: ['dms-logs', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/dms/tasks/${selected}/logs`),
    enabled: selected !== '',
    retry: 1,
  })
  const logRows = listOf(logs.data, 'logs')
  const rds = useQuery({ queryKey: ['rds-instances'], queryFn: () => request<Rec>('/api/v1/aws/rds/instances'), retry: 1 })
  const rdsRows = listOf(rds.data, 'instances')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['dms-tasks'] })
    void queryClient.invalidateQueries({ queryKey: ['dms-logs', selected] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/dms/tasks', {
        method: 'POST',
        body: {
          name: name.trim(),
          source: { kind: sourceKind, instance_id: sourceKind === 'rds' ? sourceInst : undefined },
          target: { kind: targetKind, instance_id: targetKind === 'rds' ? targetInst : undefined },
          tables: tables.split(',').map((t) => t.trim()).filter(Boolean),
        },
      }),
    onSuccess: () => { setNotice('Migration task started — it copies rows async; refresh to see progress.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/dms/tasks/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Task deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = taskRows.find((t) => t.id === selected)
  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Migration', to: '/services' }, { label: 'Database Migration Service' }]}
      title="Database Migration Service — Replication tasks"
      description="Real schema/data migration between the platform Postgres and RDS instances, with real row counts and a migration log."
      fidelity="PARTIAL"
      fidelityFootnote="full-load only · ≤5000 rows/table · no CDC"
      homeLabel="DMS dashboard"
      listLabel="Replication tasks"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="DMS"
            banner="Full-load row copies between the platform Postgres and RDS instance databases (up to 5000 rows per table) with real row counts and a migration log. No CDC, no ongoing replication. Survey note: in the 2026-11-22 reference account DMS was NOT ACCESSIBLE (search card not supported, no layout captured) — this console's layout follows public console documentation, not an observed layout."
            actions={[
              { label: 'Create replication task', primary: true, onClick: goToList },
            { label: 'Open DataSync', to: '/services/datasync' },
            ]}
            cards={[
              { label: 'Replication tasks', count: taskRows.length, onSelect: goToList },
            { label: 'Endpoints', count: '—', onSelect: goToList, note: 'Source/target are platform Postgres or RDS instances' },
            { label: 'Migration logs', count: selected !== '' ? logRows.length : '—', onSelect: goToList },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={tasks.isFetching}
            health={[
              "tasks.isError ? 'Tasks could not be loaded.' : `${taskRows.length} replication task(s) listed.`",
            "Target table is built from source information_schema column types.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — databases track', to: '/learn' },
            { label: 'Migration Hub (task tracking)', to: '/services/migrationhub' },
            ]}
          />
        )}
    >
      <Notice text={notice} />
      {tasks.isPending && <Loading label="Loading migration tasks" />}
      {tasks.isError && <ErrorBox message={`Tasks could not be loaded (${tasks.error.message}).`} />}
      <AwsCard title="Replication tasks">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Task name" className={inputCls} />
          <label className="text-sm">
            Source
            <select value={sourceKind} onChange={(e) => setSourceKind(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="platform">platform Postgres</option>
              <option value="rds">RDS instance</option>
            </select>
          </label>
          {sourceKind === 'rds' && (
            <select value={sourceInst} onChange={(e) => setSourceInst(e.target.value)} className={inputCls} aria-label="Source RDS instance">
              <option value="">(choose instance)</option>
              {rdsRows.map((r) => (
                <option key={r.id} value={r.id}>{str(r, 'name') || r.id}</option>
              ))}
            </select>
          )}
          <label className="text-sm">
            Target
            <select value={targetKind} onChange={(e) => setTargetKind(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="platform">platform Postgres</option>
              <option value="rds">RDS instance</option>
            </select>
          </label>
          {targetKind === 'rds' && (
            <select value={targetInst} onChange={(e) => setTargetInst(e.target.value)} className={inputCls} aria-label="Target RDS instance">
              <option value="">(choose instance)</option>
              {rdsRows.map((r) => (
                <option key={r.id} value={r.id}>{str(r, 'name') || r.id}</option>
              ))}
            </select>
          )}
          <input value={tables} onChange={(e) => setTables(e.target.value)} placeholder="Tables, comma-separated (empty = all)" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create replication task</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Source', render: (r) => `${str(r, 'source_kind')}${str(r, 'source_instance') ? `:${str(r, 'source_instance')}` : ''}` },
            { header: 'Target', render: (r) => `${str(r, 'target_kind')}${str(r, 'target_instance') ? `:${str(r, 'target_instance')}` : ''}` },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Rows copied', render: (r) => String(num(r, 'rows_copied')) },
            { header: 'Error', render: (r) => str(r, 'error') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Logs</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={taskRows}
          emptyMessage="No migration tasks yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Migration log: ${str(sel, 'name')}`}>
          <AwsTable
            columns={[
              { header: 'Table', render: (r) => str(r, 'table') },
              { header: 'Rows', render: (r) => String(num(r, 'rows')) },
              { header: 'Status', render: (r) => str(r, 'status') },
              { header: 'Message', render: (r) => str(r, 'message') },
            ]}
            rows={logRows}
            emptyMessage="No log entries yet."
          />
        </AwsCard>
      )}
    </ConsoleWithHome>
  )
}

/**
 * DataSync console (Phase 6) — mirrors the AWS DataSync tasks
 * screens: real file/object copy between S3 buckets and local paths
 * under the workdir, with SHA-256 verification per file.
 */
export function DataSyncConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [source, setSource] = useState('local:')
  const [dest, setDest] = useState('local:')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const tasks = useQuery({ queryKey: ['datasync-tasks'], queryFn: () => request<Rec>('/api/v1/aws/datasync/tasks'), retry: 1 })
  const taskRows = listOf(tasks.data, 'tasks')
  const logs = useQuery({
    queryKey: ['datasync-logs', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/datasync/tasks/${selected}/logs`),
    enabled: selected !== '',
    retry: 1,
  })
  const logRows = listOf(logs.data, 'logs')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['datasync-tasks'] })
    void queryClient.invalidateQueries({ queryKey: ['datasync-logs', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/datasync/tasks', { method: 'POST', body: { name: name.trim(), source: source.trim(), destination: dest.trim() } }),
    onSuccess: () => { setNotice('Transfer task started — files are copied async with SHA-256 verification.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/datasync/tasks/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Task deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = taskRows.find((t) => t.id === selected)
  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Migration', to: '/services' }, { label: 'DataSync' }]}
      title="DataSync — Transfer tasks"
      description="Real file/object copy between S3 buckets and local paths, with SHA-256 verification per file."
      fidelity="PARTIAL"
      fidelityFootnote="local paths confined to the workdir"
      homeLabel="DataSync dashboard"
      listLabel="Transfer tasks"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="DataSync"
            banner="Real copies between local workdir paths and S3 buckets with SHA-256 verification per file. Local paths are confined to the workdir root; S3 needs MinIO. No DataSync agents. Survey note: DataSync was NOT YET CHECKED in the 2026-11-22 survey (the browser became unavailable before its page was reached) — this layout is from public console documentation and is UNVERIFIED against the observed console. Do not treat it as an observed layout."
            actions={[
              { label: 'Create task', primary: true, onClick: goToList },
            { label: 'Open DMS', to: '/services/dms' },
            ]}
            cards={[
              { label: 'Transfer tasks', count: taskRows.length, onSelect: goToList },
            { label: 'Agents', count: 0, disabledReason: 'No DataSync agents.' },
            { label: 'Locations', count: '—', onSelect: goToList, note: 's3:<bucket>[/prefix] or local:<path>' },
            { label: 'Transfer logs', count: selected !== '' ? logRows.length : '—', onSelect: goToList },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={tasks.isFetching}
            health={[
              "tasks.isError ? 'Tasks could not be loaded.' : `${taskRows.length} task(s) listed.`",
            "SHA-256 verification per file; local paths confined to the workdir root, .. rejected.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — databases track', to: '/learn' },
            { label: 'Migration Hub (task tracking)', to: '/services/migrationhub' },
            ]}
          />
        )}
    >
      <Notice text={notice} />
      {tasks.isPending && <Loading label="Loading transfer tasks" />}
      {tasks.isError && <ErrorBox message={`Tasks could not be loaded (${tasks.error.message}).`} />}
      <AwsCard title="Transfer tasks">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Task name" className={inputCls} />
          <input value={source} onChange={(e) => setSource(e.target.value)} placeholder="Source s3:<bucket>[/prefix] or local:<path>" className={`${inputCls} w-72`} aria-label="Source location" />
          <input value={dest} onChange={(e) => setDest(e.target.value)} placeholder="Destination s3:<bucket>[/prefix] or local:<path>" className={`${inputCls} w-72`} aria-label="Destination location" />
          <AwsButton variant="primary" type="submit">Create task</AwsButton>
        </form>
        <p className="mb-2 text-xs text-aws-muted">Local paths are confined to the workdir root; “..” segments are rejected. S3 locations need MinIO.</p>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Source', render: (r) => str(r, 'source') },
            { header: 'Destination', render: (r) => str(r, 'destination') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Files', render: (r) => String(num(r, 'files_copied')) },
            { header: 'Bytes', render: (r) => String(num(r, 'bytes_copied')) },
            { header: 'Verified', render: (r) => String(num(r, 'files_verified')) },
            { header: 'Error', render: (r) => str(r, 'error') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Logs</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={taskRows}
          emptyMessage="No transfer tasks yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Transfer log: ${str(sel, 'name')}`}>
          <AwsTable
            columns={[
              { header: 'File', render: (r) => str(r, 'file') },
              { header: 'Bytes', render: (r) => String(num(r, 'bytes')) },
              { header: 'SHA-256 verified', render: (r) => String(r.verified === true ? 'yes' : 'no') },
              { header: 'Status', render: (r) => str(r, 'status') },
              { header: 'Message', render: (r) => str(r, 'message') },
            ]}
            rows={logRows}
            emptyMessage="No log entries yet."
          />
        </AwsCard>
      )}
    </ConsoleWithHome>
  )
}

/**
 * Transfer Family console (Phase 6) — an honest UNSUPPORTED
 * surface: the page explains why, shows the binaries that were
 * checked, and points at DataSync instead. Nothing is shown as
 * working.
 */
export function TransferConsole() {
  const info = useQuery({ queryKey: ['transfer-info'], queryFn: () => request<Rec>('/api/v1/aws/transfer/info'), retry: 1 })
  const checked = (info.data?.checked_binaries ?? {}) as Record<string, boolean>
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Migration', to: '/services' }, { label: 'Transfer Family' }]}
        title="Transfer Family — SFTP servers"
        description="Managed file transfer over SFTP."
        actions={<FidelityBadge fidelity="UNSUPPORTED" footnote="no local SFTP server" />}
      />
      {info.isPending && <Loading label="Checking Transfer Family availability" />}
      {info.isError && <ErrorBox message={`Could not check availability (${info.error.message}).`} />}
      {info.data && (
        <AwsCard title="Not available in this deployment">
          <p className="text-sm">{str(info.data, 'decision')}</p>
          <p className="mt-2 text-sm text-aws-muted">{str(info.data, 'reason')}</p>
          <ul className="mt-3 list-disc pl-5 text-sm">
            {Object.entries(checked).map(([bin, present]) => (
              <li key={bin}>
                <span className="font-mono">{bin}</span>: {present ? 'found' : 'not found on PATH'}
              </li>
            ))}
          </ul>
          <p className="mt-3 text-sm">Alternative: {str(info.data, 'alternative')}</p>
          <p className="mt-3">
            <AwsButton variant="primary" onClick={() => { window.location.href = '/services/datasync' }}>
              Open DataSync
            </AwsButton>
          </p>
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Migration Hub console (Phase 6) — aggregates the real DMS and
 * DataSync tasks with a status summary. It creates nothing.
 */
export function MigrationHubConsole() {
  const hub = useQuery({ queryKey: ['migrationhub-tasks'], queryFn: () => request<Rec>('/api/v1/aws/migrationhub/tasks'), retry: 1 })
  const taskRows = listOf(hub.data, 'tasks')
  const summary = (hub.data?.summary ?? {}) as Record<string, number>
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Migration', to: '/services' }, { label: 'Migration Hub' }]}
        title="Migration Hub — Migration tasks"
        description="A single view of the real DMS and DataSync tasks."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="tracks DMS + DataSync tasks" />}
      />
      {hub.isPending && <Loading label="Loading migration tasks" />}
      {hub.isError && <ErrorBox message={`Tasks could not be loaded (${hub.error.message}).`} />}
      <AwsCard title="Summary">
        <p className="text-sm">
          {Object.entries(summary).map(([k, v]) => `${k}: ${v}`).join(' · ') || 'No migration tasks yet.'}
        </p>
      </AwsCard>
      <AwsCard title={`Migration tasks (${taskRows.length})`}>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Kind', render: (r) => str(r, 'kind') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Detail', render: (r) => str(r, 'detail') },
          ]}
          rows={taskRows}
          emptyMessage="No DMS or DataSync tasks yet."
        />
      </AwsCard>
    </div>
  )
}
