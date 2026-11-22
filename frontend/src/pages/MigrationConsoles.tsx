import { useMemo, useState } from 'react'
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

/** "Not available" cell with the reason the backend has no value for it. */
function NotAvailable({ reason }: { reason: string }) {
  return <span title={reason}>Not available</span>
}

/** Duration between two RFC 3339 timestamps, or null when either is missing/invalid. */
function durationMs(start: string, end: string): number | null {
  if (start === '' || end === '') return null
  const ms = Date.parse(end) - Date.parse(start)
  return Number.isFinite(ms) && ms >= 0 ? ms : null
}

function fmtDuration(ms: number | null): string {
  if (ms === null) return 'Not available'
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  return `${(s / 60).toFixed(1)}m`
}

/** Throughput from a copied count over a run duration, or "Not available" when the run has no duration yet. */
function fmtThroughput(count: number, ms: number | null, unit: string): string {
  if (ms === null || ms <= 0) return 'Not available'
  const perSec = count / (ms / 1000)
  return `${perSec >= 100 ? Math.round(perSec).toString() : perSec.toFixed(2)} ${unit}/s`
}

/**
 * Split a DataSync location string ("s3:<bucket>[/prefix]" or
 * "local:<path>") into its surveyed Location columns. A location has
 * no backend id and a local path has no host — both are honestly
 * "Not available" in the table.
 */
function parseSyncLocation(raw: string): { type: string; host: string; path: string } {
  if (raw.startsWith('s3:')) {
    const rest = raw.slice(3)
    const slash = rest.indexOf('/')
    return { type: 'S3', host: slash === -1 ? rest : rest.slice(0, slash), path: slash === -1 ? '/' : rest.slice(slash) }
  }
  if (raw.startsWith('local:')) return { type: 'Local', host: 'Not available', path: raw.slice(6) === '' ? '/' : raw.slice(6) }
  return { type: 'Not available', host: 'Not available', path: raw }
}

/**
 * DataSync console (Phase 6) — mirrors the AWS DataSync tasks
 * screens: real file/object copy between S3 buckets and local paths
 * under the workdir, with SHA-256 verification per file.
 *
 * Survey columns (2026-11-22, fresh session — DataSync was checked):
 * Tasks: Task ID, Name, Status, Mode, Schedule, Source, Destination,
 * Created. Task executions: Execution ID, Start time, Duration,
 * Status, Data throughput, File throughput, Task, Mode. Locations:
 * Location ID, Host, Path, Type, Created, Tasks, Agents. Agents:
 * Agent ID, Name, Service endpoint, Status, Created, Tasks.
 * The backend has no Mode, Schedule, executions, locations, or
 * agents: Mode/Schedule are "Not available", executions and
 * locations are derived from each task's own run (started/completed
 * timestamps, copied counts, source/destination strings), and the
 * Agents table is honestly empty. Files/Bytes/Verified/Error stay
 * on the task detail view, not as list columns.
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
  // Task executions: the backend has no executions API — each task
  // runs once, so one execution row is derived from each task's own
  // run (started/completed timestamps, copied counts). Execution ID
  // and Mode have no backend value, so they are "Not available".
  const executions = useMemo(
    () =>
      taskRows.map((t) => {
        const started = str(t, 'started_at') || str(t, 'created_at')
        const ms = durationMs(started, str(t, 'completed_at'))
        return { ...t, started_at: started, duration_ms: ms }
      }),
    [taskRows],
  )
  // Locations: the backend has no locations API — the distinct
  // source/destination strings of the tasks are the locations.
  // Location ID and Agents have no backend value.
  const locations = useMemo(() => {
    const byLoc = new Map<string, Rec>()
    for (const t of taskRows) {
      for (const raw of [str(t, 'source'), str(t, 'destination')]) {
        if (raw === '') continue
        const p = parseSyncLocation(raw)
        const created = str(t, 'created_at')
        const prev = byLoc.get(raw)
        if (!prev) {
          byLoc.set(raw, { id: raw, location: raw, host: p.host, path: p.path, type: p.type, created_at: created, task_count: 1 })
        } else {
          prev.task_count = (typeof prev.task_count === 'number' ? prev.task_count : 0) + 1
          if (created !== '' && (typeof prev.created_at !== 'string' || prev.created_at === '' || created < (prev.created_at as string))) {
            prev.created_at = created
          }
        }
      }
    }
    return [...byLoc.values()]
  }, [taskRows])
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
      listLabel="Tasks"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="DataSync"
            banner="Real copies between local workdir paths and S3 buckets with SHA-256 verification per file. Local paths are confined to the workdir root; S3 needs MinIO. No DataSync agents. Survey note: the 2026-11-22 survey (fresh session) records the DataSync left nav as Dashboard, Task executions, Tasks, Locations, Agents, with the columns used on each table below — executions and locations are derived from tasks, as noted on each table."
            actions={[
              { label: 'Create task', primary: true, onClick: goToList },
            { label: 'Open DMS', to: '/services/dms' },
            ]}
            cards={[
              { label: 'Transfer tasks', count: taskRows.length, onSelect: goToList },
            { label: 'Task executions', count: taskRows.length, onSelect: goToList, note: 'One execution per task run (derived — no executions API)' },
            { label: 'Locations', count: locations.length, onSelect: goToList, note: 'Distinct task source/destination locations (derived — no locations API)' },
            { label: 'Agents', count: 0, disabledReason: 'No DataSync agents.' },
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
            { header: 'Task ID', render: (r) => <span className="font-mono text-xs">{r.id}</span> },
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Mode', render: () => <NotAvailable reason="The backend stores no transfer mode for a task." /> },
            { header: 'Schedule', render: () => <NotAvailable reason="The backend stores no schedule for a task — tasks run once on creation." /> },
            { header: 'Source', render: (r) => str(r, 'source') },
            { header: 'Destination', render: (r) => str(r, 'destination') },
            { header: 'Created', render: (r) => str(r, 'created_at') || '—' },
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
        <p className="mt-2 text-xs text-aws-muted">
          Survey columns: Task ID, Name, Status, Mode, Schedule, Source, Destination, Created. Mode and Schedule are
          not modelled by the backend, so they are Not available. Files, bytes, verified count and error are on the
          task detail view below, not list columns.
        </p>
      </AwsCard>
      <AwsCard title={`Task executions (${executions.length})`}>
        <AwsTable
          columns={[
            { header: 'Execution ID', render: () => <NotAvailable reason="The backend has no executions API — each task runs once, so the execution is the task's own run." /> },
            { header: 'Start time', render: (r) => str(r, 'started_at') || '—' },
            {
              header: 'Duration',
              render: (r) => fmtDuration(typeof r.duration_ms === 'number' ? (r.duration_ms as number) : null),
            },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Data throughput',
              render: (r) => fmtThroughput(num(r, 'bytes_copied'), typeof r.duration_ms === 'number' ? (r.duration_ms as number) : null, 'B'),
            },
            {
              header: 'File throughput',
              render: (r) => fmtThroughput(num(r, 'files_copied'), typeof r.duration_ms === 'number' ? (r.duration_ms as number) : null, 'files'),
            },
            { header: 'Task', render: (r) => str(r, 'name') },
            { header: 'Mode', render: () => <NotAvailable reason="The backend stores no transfer mode for a task." /> },
          ]}
          rows={executions}
          emptyMessage="No task executions yet — create a task first."
        />
        <p className="mt-2 text-xs text-aws-muted">
          Survey columns: Execution ID, Start time, Duration, Status, Data throughput, File throughput, Task, Mode.
          CloudForge has no separate executions API: one execution row is derived from each task&apos;s run, with
          duration and throughput computed from its started/completed timestamps and copied counts (Not available
          while a run has no completion timestamp). Execution ID and Mode have no backend value.
        </p>
      </AwsCard>
      <AwsCard title={`Locations (${locations.length})`}>
        <AwsTable
          columns={[
            { header: 'Location ID', render: () => <NotAvailable reason="The backend has no locations API — locations are derived from task source/destination strings, which carry no id." /> },
            { header: 'Host', render: (r) => str(r, 'host') },
            { header: 'Path', render: (r) => <span className="font-mono text-xs">{str(r, 'path')}</span> },
            { header: 'Type', render: (r) => str(r, 'type') },
            { header: 'Created', render: (r) => str(r, 'created_at') || '—' },
            { header: 'Tasks', render: (r) => String(num(r, 'task_count')) },
            { header: 'Agents', render: () => <NotAvailable reason="CloudForge has no DataSync agents." /> },
          ]}
          rows={locations}
          emptyMessage="No locations yet — create a task first."
        />
        <p className="mt-2 text-xs text-aws-muted">
          Survey columns: Location ID, Host, Path, Type, Created, Tasks, Agents. Locations are the distinct
          source/destination strings of the tasks (s3:&lt;bucket&gt;[/prefix] or local:&lt;path&gt;); Created is the
          earliest task using the location, Tasks is how many tasks use it. Location ID and Agents have no backend
          value.
        </p>
      </AwsCard>
      <AwsCard title="Agents (0)">
        <AwsTable
          columns={[
            { header: 'Agent ID', render: () => '—' },
            { header: 'Name', render: () => '—' },
            { header: 'Service endpoint', render: () => '—' },
            { header: 'Status', render: () => '—' },
            { header: 'Created', render: () => '—' },
            { header: 'Tasks', render: () => '—' },
          ]}
          rows={[]}
          emptyMessage="No DataSync agents — CloudForge has no agent API."
        />
        <p className="mt-2 text-xs text-aws-muted">
          Survey columns: Agent ID, Name, Service endpoint, Status, Created, Tasks. The table is empty by design:
          CloudForge copies files directly and ships no DataSync agent.
        </p>
      </AwsCard>
      {sel && (
        <AwsCard title={`Transfer log: ${str(sel, 'name')}`}>
          <dl className="mb-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
            <div><dt className="text-aws-muted">Files copied</dt><dd>{String(num(sel, 'files_copied'))}</dd></div>
            <div><dt className="text-aws-muted">Bytes copied</dt><dd>{String(num(sel, 'bytes_copied'))}</dd></div>
            <div><dt className="text-aws-muted">Files verified (SHA-256)</dt><dd>{String(num(sel, 'files_verified'))}</dd></div>
            <div><dt className="text-aws-muted">Error</dt><dd>{str(sel, 'error') || '—'}</dd></div>
          </dl>
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
