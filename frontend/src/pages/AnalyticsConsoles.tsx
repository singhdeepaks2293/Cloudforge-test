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

/**
 * Athena console (Phase 6) — mirrors the AWS Athena query editor:
 * saved queries, query editor, execution history and a results table.
 * Queries are real PostgreSQL SQL, not Presto/Athena — the console
 * says so on every run, and the backend rejects anything that is not
 * a single read-only SELECT.
 */
export function AthenaConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sql, setSql] = useState('SELECT 1 AS n')
  const [db, setDb] = useState('platform')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const [result, setResult] = useState<Rec | null>(null)

  const queries = useQuery({ queryKey: ['athena-queries'], queryFn: () => request<Rec>('/api/v1/aws/athena/queries'), retry: 1 })
  const rows = listOf(queries.data, 'queries')
  const executions = useQuery({ queryKey: ['athena-executions'], queryFn: () => request<Rec>('/api/v1/aws/athena/executions'), retry: 1 })
  const execRows = listOf(executions.data, 'executions')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['athena-queries'] })
    void queryClient.invalidateQueries({ queryKey: ['athena-executions'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/athena/queries', { method: 'POST', body: { name: name.trim(), query: sql, database: db } }),
    onSuccess: () => { setNotice('Query saved.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Save failed: ${e.message}`),
  })
  const execute = useMutation({
    mutationFn: (body: Record<string, unknown>) => request<Rec>('/api/v1/aws/athena/execute', { method: 'POST', body }),
    onSuccess: (d) => { setResult(d); setNotice(`Query ran: ${String(d.row_count ?? 0)} row(s) in ${String(d.duration_ms ?? 0)} ms.`); refresh() },
    onError: (e: Error) => { setResult(null); setNotice(`Query failed: ${e.message}`) },
  })
  const runSaved = (q: Rec) => {
    setSelected(str(q, 'id'))
    execute.mutate({ query: str(q, 'query'), database: str(q, 'database') || 'platform' })
  }
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/athena/queries/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Query deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const cols = Array.isArray(result?.columns) ? (result?.columns as string[]) : []
  const resultRows = Array.isArray(result?.rows) ? (result?.rows as Record<string, unknown>[]) : []
  const chartVals = resultRows.map((r) => Number(r[cols[1] ?? ''] ?? 0)).filter((v) => Number.isFinite(v))
  const chartMax = chartVals.length > 0 ? Math.max(...chartVals) : 0

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Analytics', to: '/services' }, { label: 'Athena' }]}
        title="Athena — Query editor"
        description="Interactive SQL queries against the platform Postgres (or an RDS instance database). Read-only SELECT only — the backend rejects writes."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="PostgreSQL SQL, not Presto/Athena" />}
      />
      <Notice text={notice} />
      {queries.isPending && <Loading label="Loading queries" />}
      {queries.isError && <ErrorBox message={`Queries could not be loaded (${queries.error.message}).`} />}
      <AwsCard title="Query editor">
        <div className="space-y-2">
          <textarea value={sql} onChange={(e) => setSql(e.target.value)} rows={4} aria-label="SQL query" className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-sm focus:border-aws-link focus:outline-none" />
          <div className="flex flex-wrap items-center gap-2">
            <label className="text-sm">
              Database
              <select value={db} onChange={(e) => setDb(e.target.value)} className={`ml-2 ${inputCls}`}>
                <option value="platform">platform (Postgres)</option>
              </select>
            </label>
            <AwsButton variant="primary" onClick={() => execute.mutate({ query: sql, database: db })} disabled={execute.isPending}>
              Run query
            </AwsButton>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Saved query name" className={inputCls} />
            <AwsButton onClick={() => name.trim() !== '' && create.mutate()}>Save query</AwsButton>
          </div>
          <p className="text-xs text-aws-muted">Only a single SELECT (or WITH … SELECT) statement is allowed. INSERT/UPDATE/DELETE/DROP and stacked statements are rejected with 400 and nothing is stored.</p>
        </div>
      </AwsCard>
      {result && (
        <AwsCard title={`Results (${String(result.row_count ?? 0)} rows)`}>
          {cols.length > 0 && (
            <AwsTable
              columns={cols.map((c) => ({ header: c, render: (row: Rec) => String((row as unknown as Record<string, unknown>)[c] ?? '') }))}
              rows={resultRows.map((r, i) => ({ ...r, id: `result-${i}` }))}
              emptyMessage="No rows returned."
            />
          )}
          {chartVals.length > 1 && chartMax > 0 && (
            <div className="mt-3">
              <p className="mb-2 text-sm font-medium">Chart — {cols[1]} by {cols[0]} (from these results)</p>
              <div className="flex items-end gap-2" role="img" aria-label="Bar chart of query results">
                {resultRows.slice(0, 12).map((r, i) => {
                  const v = Number(r[cols[1]] ?? 0)
                  return (
                    <div key={i} className="flex flex-col items-center gap-1" title={`${String(r[cols[0]])}: ${v}`}>
                      <div className="w-8 bg-aws-orange" style={{ height: `${Math.max(4, (v / chartMax) * 96)}px` }} />
                      <span className="w-12 truncate text-center text-[10px]">{String(r[cols[0]] ?? '')}</span>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </AwsCard>
      )}
      <AwsCard title={`Saved queries (${rows.length})`}>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Database', render: (r) => str(r, 'database') },
            { header: 'Query', render: (r) => <span className="font-mono text-xs">{str(r, 'query').slice(0, 80)}</span> },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => runSaved(r)}>Run</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={rows}
          emptyMessage="No saved queries yet."
        />
      </AwsCard>
      <AwsCard title={`Query history (${execRows.length})`}>
        <AwsTable
          columns={[
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Database', render: (r) => str(r, 'database') },
            { header: 'Rows', render: (r) => String(num(r, 'row_count')) },
            { header: 'Duration (ms)', render: (r) => String(num(r, 'duration_ms')) },
            { header: 'Query', render: (r) => <span className="font-mono text-xs">{str(r, 'query').slice(0, 60)}</span> },
            { header: 'Error', render: (r) => str(r, 'error') },
          ]}
          rows={execRows}
          emptyMessage="No executions yet."
        />
      </AwsCard>
      <p className="text-xs text-aws-muted">Selected query: {selected || '—'}</p>
    </div>
  )
}

/**
 * Glue console (Phase 6) — mirrors the AWS Glue crawlers / Data
 * Catalog screens. Crawlers read real sources (Postgres
 * information_schema, DynamoDB tables, S3 buckets); a source that
 * cannot be reached makes the run PARTIAL, never silently skipped.
 */
export function GlueConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sources, setSources] = useState('postgres')
  const [notice, setNotice] = useState('')
  const crawlers = useQuery({ queryKey: ['glue-crawlers'], queryFn: () => request<Rec>('/api/v1/aws/glue/crawlers'), retry: 1 })
  const crawlerRows = listOf(crawlers.data, 'crawlers')
  const tables = useQuery({ queryKey: ['glue-tables'], queryFn: () => request<Rec>('/api/v1/aws/glue/tables'), retry: 1 })
  const tableRows = listOf(tables.data, 'tables')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['glue-crawlers'] })
    void queryClient.invalidateQueries({ queryKey: ['glue-tables'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/glue/crawlers', { method: 'POST', body: { name: name.trim(), sources: sources.split(',').map((s) => s.trim()).filter(Boolean) } }),
    onSuccess: () => { setNotice('Crawler created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const run = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/glue/crawlers/${id}/run`, { method: 'POST', body: {} }),
    onSuccess: (d) => { setNotice(`Crawl ${String(d.status)}: ${String(d.tables_found ?? 0)} table(s) found.`); refresh() },
    onError: (e: Error) => setNotice(`Crawl failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/glue/crawlers/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Crawler deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Analytics', to: '/services' }, { label: 'Glue' }]}
        title="Glue — Crawlers & Data Catalog"
        description="Crawl real sources into a data catalog of tables and columns."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="real crawls only · no ETL jobs" />}
      />
      <Notice text={notice} />
      {crawlers.isPending && <Loading label="Loading crawlers" />}
      {crawlers.isError && <ErrorBox message={`Crawlers could not be loaded (${crawlers.error.message}).`} />}
      <AwsCard title="Crawlers">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Crawler name" className={inputCls} />
          <label className="text-sm">
            Sources (comma-separated: postgres, dynamodb, s3)
            <input value={sources} onChange={(e) => setSources(e.target.value)} className={`ml-2 ${inputCls}`} />
          </label>
          <AwsButton variant="primary" type="submit">Create crawler</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Sources', render: (r) => (Array.isArray(r.sources) ? (r.sources as string[]).join(', ') : '') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Tables found', render: (r) => String(num(r, 'tables_found')) },
            { header: 'Last run', render: (r) => str(r, 'last_run') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => run.mutate(str(r, 'id'))}>Run crawler</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={crawlerRows}
          emptyMessage="No crawlers yet."
        />
      </AwsCard>
      <AwsCard title={`Data Catalog — Tables (${tableRows.length})`}>
        <AwsTable
          columns={[
            { header: 'Table', render: (r) => str(r, 'table') },
            { header: 'Source', render: (r) => str(r, 'source') },
            { header: 'Columns', render: (r) => String(num(r, 'column_count')) },
            { header: 'Crawled at', render: (r) => str(r, 'crawled_at') },
          ]}
          rows={tableRows}
          emptyMessage="No catalog tables yet — run a crawler."
        />
        <p className="mt-2 text-xs text-aws-muted">Columns are the real columns crawled from the source (Postgres information_schema, DynamoDB key attributes, S3 object fields).</p>
      </AwsCard>
    </div>
  )
}

/**
 * Kinesis console (Phase 6) — mirrors the AWS Kinesis data streams
 * screens: streams, PutRecord with a partition key, and GetRecords
 * in sequence-number order.
 */
export function KinesisConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [shards, setShards] = useState(1)
  const [selected, setSelected] = useState('')
  const [partitionKey, setPartitionKey] = useState('order-1')
  const [data, setData] = useState('{"order": 1}')
  const [after, setAfter] = useState('')
  const [notice, setNotice] = useState('')
  const streams = useQuery({ queryKey: ['kinesis-streams'], queryFn: () => request<Rec>('/api/v1/aws/kinesis/streams'), retry: 1 })
  const streamRows = listOf(streams.data, 'streams')
  const records = useQuery({
    queryKey: ['kinesis-records', selected, after],
    queryFn: () => request<Rec>(`/api/v1/aws/kinesis/streams/${selected}/records${after ? `?after_sequence=${encodeURIComponent(after)}` : ''}`),
    enabled: selected !== '',
    retry: 1,
  })
  const recordRows = listOf(records.data, 'records')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['kinesis-streams'] })
    void queryClient.invalidateQueries({ queryKey: ['kinesis-records', selected, after] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/kinesis/streams', { method: 'POST', body: { name: name.trim(), shard_count: shards } }),
    onSuccess: () => { setNotice('Stream created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const put = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/kinesis/streams/${selected}/records`, { method: 'POST', body: { partition_key: partitionKey, data } }),
    onSuccess: (d) => { setNotice(`Record put on shard ${String(d.shard)} (sequence ${String(d.sequence_number)}).`); refresh() },
    onError: (e: Error) => setNotice(`Put failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/kinesis/streams/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Stream deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = streamRows.find((s) => s.id === selected)
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Analytics', to: '/services' }, { label: 'Kinesis' }]}
        title="Kinesis — Data streams"
        description="Durable streams backed by Postgres, with sequence numbers and shard assignment by partition-key hash."
        actions={<FidelityBadge fidelity="API_EMULATED" footnote="durable Postgres streams" />}
      />
      <Notice text={notice} />
      {streams.isPending && <Loading label="Loading streams" />}
      {streams.isError && <ErrorBox message={`Streams could not be loaded (${streams.error.message}).`} />}
      <AwsCard title="Data streams">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Stream name" className={inputCls} />
          <label className="text-sm">
            Shards (1–8)
            <input type="number" min={1} max={8} value={shards} onChange={(e) => setShards(Number(e.target.value))} className={`ml-2 w-20 ${inputCls}`} />
          </label>
          <AwsButton variant="primary" type="submit">Create stream</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Shards', render: (r) => String(num(r, 'shard_count')) },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => { setSelected(str(r, 'id')); setAfter('') }}>Open</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={streamRows}
          emptyMessage="No streams yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Stream: ${str(sel, 'name')}`}>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <input value={partitionKey} onChange={(e) => setPartitionKey(e.target.value)} placeholder="Partition key" className={inputCls} />
            <input value={data} onChange={(e) => setData(e.target.value)} placeholder='Record data (JSON string)' className={`${inputCls} w-64`} />
            <AwsButton variant="primary" onClick={() => put.mutate()}>Put record</AwsButton>
            <input value={after} onChange={(e) => setAfter(e.target.value)} placeholder="After sequence (optional)" className={inputCls} />
            <AwsButton onClick={() => refresh()}>Refresh records</AwsButton>
          </div>
          <AwsTable
            columns={[
              { header: 'Sequence number', render: (r) => <span className="font-mono text-xs">{str(r, 'sequence_number')}</span> },
              { header: 'Shard', render: (r) => String(num(r, 'shard')) },
              { header: 'Partition key', render: (r) => str(r, 'partition_key') },
              { header: 'Data', render: (r) => <span className="font-mono text-xs">{str(r, 'data').slice(0, 120)}</span> },
            ]}
            rows={recordRows}
            emptyMessage="No records yet — put one above."
          />
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Firehose console (Phase 6) — mirrors the AWS Firehose delivery
 * streams screens: streams, PutRecord, and real delivery records.
 */
export function FirehoseConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [dest, setDest] = useState('postgres')
  const [bucket, setBucket] = useState('')
  const [selected, setSelected] = useState('')
  const [data, setData] = useState('{"event": "click"}')
  const [notice, setNotice] = useState('')
  const streams = useQuery({ queryKey: ['firehose-streams'], queryFn: () => request<Rec>('/api/v1/aws/firehose/streams'), retry: 1 })
  const streamRows = listOf(streams.data, 'streams')
  const deliveries = useQuery({
    queryKey: ['firehose-deliveries', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/firehose/streams/${selected}/deliveries`),
    enabled: selected !== '',
    retry: 1,
  })
  const deliveryRows = listOf(deliveries.data, 'deliveries')
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['firehose-streams'] })
    void queryClient.invalidateQueries({ queryKey: ['firehose-deliveries', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/firehose/streams', { method: 'POST', body: { name: name.trim(), destination: dest, bucket: bucket.trim() || undefined } }),
    onSuccess: () => { setNotice('Delivery stream created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const put = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/firehose/streams/${selected}/records`, { method: 'POST', body: { data } }),
    onSuccess: (d) => { setNotice(`Delivered: ${String(d.detail ?? '')}`); refresh() },
    onError: (e: Error) => setNotice(`Delivery failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/firehose/streams/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Delivery stream deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const sel = streamRows.find((s) => s.id === selected)
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Analytics', to: '/services' }, { label: 'Firehose' }]}
        title="Firehose — Delivery streams"
        description="Deliver records to S3 (MinIO) or a Postgres table, with a real delivery record per put."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="S3 needs MinIO · Postgres destination is local" />}
      />
      <Notice text={notice} />
      {streams.isPending && <Loading label="Loading streams" />}
      {streams.isError && <ErrorBox message={`Streams could not be loaded (${streams.error.message}).`} />}
      <AwsCard title="Delivery streams">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Stream name" className={inputCls} />
          <label className="text-sm">
            Destination
            <select value={dest} onChange={(e) => setDest(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="postgres">Postgres table</option>
              <option value="s3">S3 bucket (MinIO)</option>
            </select>
          </label>
          {dest === 's3' && <input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="Bucket name" className={inputCls} />}
          <AwsButton variant="primary" type="submit">Create stream</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Destination', render: (r) => str(r, 'destination') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'Records delivered', render: (r) => String(num(r, 'records_delivered')) },
            { header: 'Bytes delivered', render: (r) => String(num(r, 'bytes_delivered')) },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Open</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={streamRows}
          emptyMessage="No delivery streams yet."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Stream: ${str(sel, 'name')} — deliveries`}>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <input value={data} onChange={(e) => setData(e.target.value)} placeholder='Record data (JSON string)' className={`${inputCls} w-80`} />
            <AwsButton variant="primary" onClick={() => put.mutate()}>Put record</AwsButton>
            <AwsButton onClick={() => refresh()}>Refresh</AwsButton>
          </div>
          <AwsTable
            columns={[
              { header: 'Status', render: (r) => str(r, 'status') },
              { header: 'Detail', render: (r) => str(r, 'detail') || str(r, 'error') },
              { header: 'Bytes', render: (r) => String(num(r, 'bytes')) },
              { header: 'Delivered at', render: (r) => str(r, 'delivered_at') },
            ]}
            rows={deliveryRows}
            emptyMessage="No deliveries yet."
          />
          {str(sel, 'destination') === 's3' && <p className="mt-2 text-xs text-aws-muted">S3 delivery needs MinIO — when MinIO is down the put returns an honest 503 and the delivery is recorded FAILED.</p>}
        </AwsCard>
      )}
    </div>
  )
}

/**
 * QuickSight console (Phase 6) — mirrors the AWS QuickSight
 * dashboards screens. A dashboard is a saved Athena query plus a
 * bar/line chart rendered from the live query results — no sample
 * data, ever.
 */
export function QuickSightConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [queryId, setQueryId] = useState('')
  const [chartType, setChartType] = useState('bar')
  const [labelCol, setLabelCol] = useState('')
  const [valueCol, setValueCol] = useState('')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const dashboards = useQuery({ queryKey: ['qs-dashboards'], queryFn: () => request<Rec>('/api/v1/aws/quicksight/dashboards'), retry: 1 })
  const dashRows = listOf(dashboards.data, 'dashboards')
  const queries = useQuery({ queryKey: ['athena-queries'], queryFn: () => request<Rec>('/api/v1/aws/athena/queries'), retry: 1 })
  const queryRows = listOf(queries.data, 'queries')
  const detail = useQuery({
    queryKey: ['qs-dashboard', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/quicksight/dashboards/${selected}`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['qs-dashboards'] })
    void queryClient.invalidateQueries({ queryKey: ['qs-dashboard', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/quicksight/dashboards', { method: 'POST', body: { name: name.trim(), query_id: queryId || undefined, chart_type: chartType, label_column: labelCol || undefined, value_column: valueCol || undefined } }),
    onSuccess: () => { setNotice('Dashboard created.'); setName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/quicksight/dashboards/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Dashboard deleted.'); setSelected(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const detailData = (detail.data ?? {}) as Rec
  const chartData = Array.isArray(detail.data?.chart_data) ? (detail.data?.chart_data as { label: string; value: number }[]) : []
  const chartMax = chartData.length > 0 ? Math.max(...chartData.map((p) => p.value)) : 0
  const sel = dashRows.find((d) => d.id === selected)
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Analytics', to: '/services' }, { label: 'QuickSight' }]}
        title="QuickSight — Dashboards"
        description="Charts rendered from live Athena query results — no sample data."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="charts from live query results" />}
      />
      <Notice text={notice} />
      {dashboards.isPending && <Loading label="Loading dashboards" />}
      {dashboards.isError && <ErrorBox message={`Dashboards could not be loaded (${dashboards.error.message}).`} />}
      <AwsCard title="Dashboards">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (name.trim() !== '') create.mutate() }}
        >
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Dashboard name" className={inputCls} />
          <label className="text-sm">
            Athena query
            <select value={queryId} onChange={(e) => setQueryId(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="">(none yet)</option>
              {queryRows.map((q) => (
                <option key={q.id} value={q.id}>{str(q, 'name')}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Chart
            <select value={chartType} onChange={(e) => setChartType(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="bar">Bar</option>
              <option value="line">Line</option>
            </select>
          </label>
          <input value={labelCol} onChange={(e) => setLabelCol(e.target.value)} placeholder="Label column" className={inputCls} />
          <input value={valueCol} onChange={(e) => setValueCol(e.target.value)} placeholder="Value column" className={inputCls} />
          <AwsButton variant="primary" type="submit">Create dashboard</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Chart', render: (r) => str(r, 'chart_type') },
            { header: 'Label column', render: (r) => str(r, 'label_column') },
            { header: 'Value column', render: (r) => str(r, 'value_column') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelected(str(r, 'id'))}>Open</AwsButton>
                  <AwsButton onClick={() => del.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={dashRows}
          emptyMessage="No dashboards yet — save an Athena query first, then chart it."
        />
      </AwsCard>
      {sel && (
        <AwsCard title={`Dashboard: ${str(sel, 'name')}`}>
          {detail.isPending && <Loading label="Loading chart data" />}
          {detail.data && str(detail.data, 'chart_error') !== '' && <ErrorBox message={`Chart could not be built: ${str(detail.data, 'chart_error')}`} />}
          {chartData.length > 0 && chartMax > 0 && (
            <div className="flex items-end gap-2" role="img" aria-label="Bar chart of live query results">
              {chartData.slice(0, 12).map((p, i) => (
                <div key={i} className="flex flex-col items-center gap-1" title={`${p.label}: ${p.value}`}>
                  <div className="w-8 bg-aws-orange" style={{ height: `${Math.max(4, (p.value / chartMax) * 120)}px` }} />
                  <span className="w-12 truncate text-center text-[10px]">{p.label}</span>
                </div>
              ))}
            </div>
          )}
          {chartData.length === 0 && !detail.isPending && str(detailData, 'chart_error') === '' && (
            <p className="text-sm text-aws-muted">The linked query returned no chartable rows.</p>
          )}
          <p className="mt-2 text-xs text-aws-muted">Label column: {str(detailData, 'label_column') || '—'} · Value column: {str(detailData, 'value_column') || '—'} · Chart type: {str(sel, 'chart_type')}</p>
        </AwsCard>
      )}
    </div>
  )
}
