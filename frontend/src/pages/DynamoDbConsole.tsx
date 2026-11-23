import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const itemsOf = (v: unknown): Rec[] => {
  const arr = Array.isArray((v as Rec)?.items) ? ((v as Rec).items as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : `item-${i}` }))
}

/**
 * DynamoDB console (Phase 3) — mirrors the AWS DynamoDB console's
 * Tables list; selecting a table shows a single Items card (Put item
 * / Query / Scan) and creation is an inline form, not a wizard.
 * There is no Overview tab and no Create-table wizard. The backend
 * names its backend on every response: DynamoDB Local (API_EMULATED,
 * real DynamoDB JSON API) when it answers, otherwise the
 * Postgres-backed subset (PARTIAL, hash key only, Scan capped at
 * 100). Never full DynamoDB.
 *
 * v4 survey (2026-11-22) left nav: Dashboard, Tables, Explore items,
 * PartiQL editor, Backups, Exports to S3, Imports from S3, Bulk put
 * items (New), Integrations, Reserved capacity, Settings; Caches, DAX.
 * Tables columns in the survey's exact order (12): Name, Status,
 * Partition key, Sort key, Indexes, Replication Regions, Deletion
 * protection, Favorite, Read capacity mode, Write capacity mode,
 * Total size, Table class. Only Name, Status and Partition key have
 * real values — the subset has no sort key or secondary indexes and
 * no capacity/size/deletion-protection data, so those render '—'.
 * Only Tables (and Explore items via the selected table's Items card)
 * have a backend; the rest are honest UNSUPPORTED sections.
 */
export function DynamoDbConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [keyAttr, setKeyAttr] = useState('id')
  const [selected, setSelected] = useState('')
  const [itemJson, setItemJson] = useState('{"id": "1", "name": "ada"}')
  const [queryKey, setQueryKey] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [section, setSection] = useState('tables')

  const tables = useQuery({ queryKey: ['dynamodb-tables'], queryFn: () => request<Rec>('/api/v1/aws/dynamodb/tables'), retry: 1 })
  const rawTables = tables.data?.tables
  const tableList: Rec[] = (Array.isArray(rawTables) ? (rawTables as Record<string, unknown>[]) : []).map((t) => ({ ...t, id: typeof t.name === 'string' ? t.name : 'table' })).filter(
    (t: Rec) => filter.trim() === '' || String(t.name ?? '').toLowerCase().includes(filter.toLowerCase()) || String(t.id).toLowerCase().includes(filter.toLowerCase()),
  )
  const scan = useQuery({
    queryKey: ['dynamodb-scan', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/dynamodb/tables/${encodeURIComponent(selected)}/items`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['dynamodb-tables'] })
    void queryClient.invalidateQueries({ queryKey: ['dynamodb-scan', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/dynamodb/tables', { method: 'POST', body: { name: name.trim(), key_attribute: keyAttr.trim() } }),
    onSuccess: (d) => {
      setNotice(`Table ${String(d.name)} created on backend ${String(d.backend)} (${String(d.fidelity ?? 'PARTIAL')}).`)
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create table failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (table: string) => request(`/api/v1/aws/dynamodb/tables/${encodeURIComponent(table)}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Table deleted.')
      setSelected('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete table failed: ${e.message}`),
  })
  const put = useMutation({
    mutationFn: () => {
      let item: unknown
      try {
        item = JSON.parse(itemJson) as unknown
      } catch {
        throw new Error('Item is not valid JSON')
      }
      return request<Rec>(`/api/v1/aws/dynamodb/tables/${encodeURIComponent(selected)}/items`, { method: 'POST', body: { item } })
    },
    onSuccess: () => {
      setNotice('Item written.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Put item failed: ${e.message}`),
  })
  const query = useMutation({
    mutationFn: () => {
      const tbl = tableList.find((t) => String(t.name) === selected)
      const ka = String(tbl?.key_attribute ?? keyAttr)
      return request<Rec>(`/api/v1/aws/dynamodb/tables/${encodeURIComponent(selected)}/query?key=${encodeURIComponent(queryKey)}&key_attribute=${encodeURIComponent(ka)}`)
    },
    onSuccess: (d) => setNotice(`Query returned ${String((d as Rec).count ?? 0)} item(s).`),
    onError: (e: Error) => setNotice(`Query failed: ${e.message}`),
  })

  return (
    <ServiceConsole
      icon="database"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Databases', to: '/services' }, { label: 'DynamoDB' }]}
      title="DynamoDB — Tables"
      description="Key-value tables. DynamoDB Local when it answers (real DynamoDB JSON API); otherwise the Postgres-backed subset — hash key only, no sort key or secondary indexes."
      fidelity="PARTIAL"
      fidelityFootnote="API_EMULATED via DynamoDB Local, else Postgres subset"
      subNav={[
        { id: 'home', label: 'Dashboard' },
        { id: 'tables', label: 'Tables' },
        { id: 'explore', label: 'Explore items' },
        { id: 'partiql', label: 'PartiQL editor' },
        { id: 'backups', label: 'Backups' },
        { id: 'exports', label: 'Exports to S3' },
        { id: 'imports', label: 'Imports from S3' },
        { id: 'bulk-put', label: 'Bulk put items', badge: 'New' },
        { id: 'integrations', label: 'Integrations' },
        { id: 'reserved', label: 'Reserved capacity' },
        { id: 'settings', label: 'Settings' },
        { id: 'caches', label: 'Caches', group: 'Caches' },
        { id: 'dax', label: 'DAX', group: 'Caches' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName="DynamoDB"
          banner="Key-value tables. DynamoDB Local when it answers (real DynamoDB JSON API); otherwise the Postgres-backed subset — hash key only, no sort key or secondary indexes."
          actions={[
            { label: 'Create table', primary: true, onClick: () => setSection('tables') },
            { label: 'Explore items', onClick: () => setSection('explore') },
          ]}
          cards={[
            { label: 'Tables', count: tableList.length, onSelect: () => setSection('tables'), note: 'Hash key only in the Postgres subset' },
            { label: 'Explore items', count: selected !== '' ? itemsOf(scan.data ?? {}).length : '—', onSelect: () => setSection('explore'), note: 'Put item / Query / Scan on a selected table' },
            { label: 'Backups', count: 0, onSelect: () => setSection('backups'), note: 'UNSUPPORTED — no backup API' },
          ]}
          onRefresh={refresh}
          refreshing={tables.isFetching}
          health={[
            tables.isError ? 'Tables could not be loaded.' : `${tableList.length} table(s) listed.`,
            `Backend: ${String(tables.data?.backend ?? 'unknown')}.`,
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[{ label: 'Learning Center — databases track', to: '/learn' }]}
        />
      )}
      {section === 'tables' && (
        <>
      {tables.isPending && <Loading label="Loading tables" />}
      {tables.isPending && <Loading label="Loading tables" />}
      {tables.isError && <ErrorBox message={`Tables could not be loaded (${tables.error.message}).`} />}
      {tables.data && (
        <>
          <p className="text-sm text-aws-muted">
            Backend: <strong>{String(tables.data.backend ?? 'unknown')}</strong>
            {typeof tables.data.note === 'string' ? ` — ${tables.data.note}` : ''}
          </p>
          <AwsCard title={`Tables (${tableList.length})`}>
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (name.trim() !== '') create.mutate()
              }}
            >
              <label htmlFor="ddb-name" className="text-sm font-medium">Table name</label>
              <input id="ddb-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="users" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <label htmlFor="ddb-key" className="text-sm font-medium">Partition key</label>
              <input id="ddb-key" value={keyAttr} onChange={(e) => setKeyAttr(e.target.value)} placeholder="id" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton type="submit" disabled={create.isPending}>Create table</AwsButton>
            </form>
            <AwsTable<Rec>
              caption="DynamoDB tables"
              emptyMessage="No tables yet. Create one above."
              rows={tableList}
              totalCount={(Array.isArray(rawTables) ? rawTables.length : 0)}
              filter={filter}
              onFilterChange={setFilter}
              filterPlaceholder="Filter tables by name"
              columns={[
                // Tables columns in the v4 survey's exact order (12).
                // Real values: Name, Status, Partition key. The subset
                // has no sort key or secondary indexes and no
                // replication, deletion-protection, capacity, size or
                // table-class data, so those render '—'. The backend is
                // shown in the paragraph above the table, not as a
                // column. Actions (Delete) is CloudForge-only.
                { header: 'Name', render: (t) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(t.name))}>{String(t.name)}</button> },
                { header: 'Status', render: (t) => String(t.status ?? '—') },
                { header: 'Partition key', render: (t) => String(t.key_attribute ?? '—') },
                { header: 'Sort key', render: () => '—' },
                { header: 'Indexes', render: () => '—' },
                { header: 'Replication Regions', render: () => '—' },
                { header: 'Deletion protection', render: () => '—' },
                { header: 'Favorite', render: () => '—' },
                { header: 'Read capacity mode', render: () => '—' },
                { header: 'Write capacity mode', render: () => '—' },
                { header: 'Total size', render: () => '—' },
                { header: 'Table class', render: () => '—' },
                {
                  header: 'Actions',
                  render: (t) => (
                    <AwsButton variant="secondary" disabled={remove.isPending} onClick={() => remove.mutate(String(t.name))}>
                      Delete
                    </AwsButton>
                  ),
                },
              ]}
            />
          </AwsCard>
          {selected !== '' && (
            <AwsCard title={`Items in ${selected} — Explore items`}>
              <div className="mb-3 flex flex-wrap items-end gap-2">
                <label className="text-sm">
                  Item JSON
                  <textarea value={itemJson} onChange={(e) => setItemJson(e.target.value)} rows={2} className="ml-2 w-80 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
                </label>
                <AwsButton disabled={put.isPending} onClick={() => put.mutate()}>Put item</AwsButton>
                <label className="text-sm">
                  Query key
                  <input value={queryKey} onChange={(e) => setQueryKey(e.target.value)} placeholder="1" className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
                </label>
                <AwsButton variant="secondary" disabled={query.isPending} onClick={() => query.mutate()}>Query</AwsButton>
                <AwsButton variant="secondary" onClick={refresh}>Scan / refresh</AwsButton>
              </div>
              {scan.isPending && <Loading label="Scanning items" />}
              {scan.data && (
                <AwsTable<Rec>
                  caption={`Items in ${selected}`}
                  emptyMessage="No items in this table."
                  rows={itemsOf(scan.data)}
                  totalCount={Number(scan.data.count ?? itemsOf(scan.data).length)}
                  columns={[
                    { header: 'Item (JSON)', render: (it) => <code className="text-xs">{JSON.stringify(it)}</code> },
                    {
                      header: 'Actions',
                      render: (it) => {
                        const tbl = tableList.find((t) => String(t.name) === selected)
                        const ka = String(tbl?.key_attribute ?? 'id')
                        const kv = String((it as Rec)[ka] ?? '')
                        return (
                          <AwsButton
                            variant="secondary"
                            disabled={kv === ''}
                            onClick={() => {
                              void request(`/api/v1/aws/dynamodb/tables/${encodeURIComponent(selected)}/items/${encodeURIComponent(kv)}?key_attribute=${encodeURIComponent(ka)}`, { method: 'DELETE' }).then(() => {
                                setNotice('Item deleted.')
                                refresh()
                              }).catch((e: Error) => setNotice(`Delete item failed: ${e.message}`))
                            }}
                          >
                            Delete
                          </AwsButton>
                        )
                      },
                    },
                  ]}
                />
              )}
              <p className="mt-2 text-sm text-aws-muted">Indexes and Monitoring tabs are not built: the subset has no secondary indexes, and table metrics are not collected.</p>
            </AwsCard>
          )}
        </>
      )}
        </>
      )}
      {section === 'explore' && (
        <AwsCard title="Explore items">
          <p className="text-sm text-aws-muted">
            Pick a table on the Tables page to explore its items — Put item, Query and Scan live on the
            selected table&apos;s Items card there. No table is selected right now.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setSection('tables')}>Open Tables</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'partiql' && (
        <UnsupportedSection
          title="PartiQL editor"
          reason="The PartiQL editor is UNSUPPORTED in CloudForge — there is no PartiQL endpoint. Query and Scan run from the selected table's Items card on the Tables page."
          actionLabel="Run PartiQL statement"
        />
      )}
      {section === 'backups' && (
        <UnsupportedSection
          title="Backups"
          reason="Backups are UNSUPPORTED in CloudForge — there is no DynamoDB backup API; tables live in DynamoDB Local or the Postgres subset with no snapshot mechanism."
          actionLabel="Create backup"
        />
      )}
      {section === 'exports' && (
        <UnsupportedSection
          title="Exports to S3"
          reason="Exports to S3 are UNSUPPORTED in CloudForge — there is no table-export API."
          actionLabel="Export table to S3"
        />
      )}
      {section === 'imports' && (
        <UnsupportedSection
          title="Imports from S3"
          reason="Imports from S3 are UNSUPPORTED in CloudForge — there is no table-import API; items are added one at a time with Put item."
          actionLabel="Import from S3"
        />
      )}
      {section === 'bulk-put' && (
        <UnsupportedSection
          title="Bulk put items"
          reason="Bulk put items is UNSUPPORTED in CloudForge — there is no bulk-put API; items are written one at a time with Put item on the selected table's Items card."
          actionLabel="Bulk put items"
        />
      )}
      {section === 'integrations' && (
        <UnsupportedSection
          title="Integrations"
          reason="Integrations are UNSUPPORTED in CloudForge — there is no DynamoDB integration API and no integration target."
          actionLabel="Create integration"
        />
      )}
      {section === 'reserved' && (
        <UnsupportedSection
          title="Reserved capacity"
          reason="Reserved capacity is UNSUPPORTED in CloudForge — there is no reserved-capacity API; tables are on-demand only."
          actionLabel="Purchase reserved capacity"
        />
      )}
      {section === 'settings' && (
        <UnsupportedSection
          title="Settings"
          reason="DynamoDB settings are UNSUPPORTED in CloudForge — no DynamoDB console settings are stored."
          actionLabel="Save settings"
        />
      )}
      {section === 'caches' && (
        <UnsupportedSection
          title="Caches"
          reason="Caches are UNSUPPORTED in CloudForge — there is no DynamoDB cache API; see ElastiCache for in-memory caches."
          actionLabel="Create cache"
        />
      )}
      {section === 'dax' && (
        <UnsupportedSection
          title="DAX"
          reason="DAX is UNSUPPORTED in CloudForge — there is no DAX cluster API and no in-memory acceleration for DynamoDB tables."
          actionLabel="Create DAX cluster"
        />
      )}
    </ServiceConsole>
  )
}
