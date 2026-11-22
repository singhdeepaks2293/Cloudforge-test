import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const itemsOf = (v: unknown): Rec[] => {
  const arr = Array.isArray((v as Rec)?.items) ? ((v as Rec).items as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : `item-${i}` }))
}

/**
 * DynamoDB console (Phase 3) — mirrors the AWS DynamoDB console's
 * Tables list, table detail (Overview / Items), Create-table wizard
 * and Explore-items flow. The backend names its backend on every
 * response: DynamoDB Local (API_EMULATED, real DynamoDB JSON API)
 * when it answers, otherwise the Postgres-backed subset (PARTIAL,
 * hash key only, Scan capped at 100). Never full DynamoDB.
 */
export function DynamoDbConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [keyAttr, setKeyAttr] = useState('id')
  const [selected, setSelected] = useState('')
  const [itemJson, setItemJson] = useState('{"id": "1", "name": "ada"}')
  const [queryKey, setQueryKey] = useState('')
  const [notice, setNotice] = useState('')

  const tables = useQuery({ queryKey: ['dynamodb-tables'], queryFn: () => request<Rec>('/api/v1/aws/dynamodb/tables'), retry: 1 })
  const rawTables = tables.data?.tables
  const tableList: Rec[] = (Array.isArray(rawTables) ? (rawTables as Record<string, unknown>[]) : []).map((t) => ({ ...t, id: typeof t.name === 'string' ? t.name : 'table' }))
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
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Databases', to: '/services' }, { label: 'DynamoDB' }]}
        title="DynamoDB — Tables"
        description="Key-value tables. DynamoDB Local when it answers (real DynamoDB JSON API); otherwise the Postgres-backed subset — hash key only, no sort key or secondary indexes."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="API_EMULATED via DynamoDB Local, else Postgres subset" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
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
              totalCount={tableList.length}
              columns={[
                { header: 'Table name', render: (t) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(t.name))}>{String(t.name)}</button> },
                { header: 'Partition key', render: (t) => String(t.key_attribute ?? '—') },
                { header: 'Status', render: (t) => String(t.status ?? '—') },
                { header: 'Backend', render: (t) => String(t.backend ?? tables.data?.backend ?? '—') },
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
    </div>
  )
}
