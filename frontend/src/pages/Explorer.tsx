import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { deleteLabResource, destroyLab, fetchExplorerResources } from '../services/api.ts'
import { getStoredRegion } from '../services/catalog.ts'
import type { ExplorerResource } from '../types/index.ts'

/**
 * Resource Explorer (Phase 1) — central explorer over REAL backend
 * resources only (labs + lab resources + courses + users where
 * permitted). Filter/search/sort/inspect/delete per permissions.
 * Region scoping is display-only in Phase 1: every resource lives in
 * cf-local-1, and selecting another region returns an empty list with
 * an honest note, never invented rows.
 */
const TYPE_OPTIONS = ['', 'lab', 'vpc', 'subnet', 'instance', 'gateway', 'loadbalancer', 'route-table', 'security-group', 'course', 'user']
const PAGE_SIZE = 20

export function Explorer() {
  const queryClient = useQueryClient()
  const [q, setQ] = useState('')
  const [type, setType] = useState('')
  const [status, setStatus] = useState('')
  const [sort, setSort] = useState('name')
  const [offset, setOffset] = useState(0)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [notice, setNotice] = useState('')
  const region = getStoredRegion()

  const explorer = useQuery({
    queryKey: ['explorer', { q, type, status, sort, offset, region }],
    queryFn: () =>
      fetchExplorerResources({
        q: q === '' ? undefined : q,
        type: type === '' ? undefined : type,
        status: status === '' ? undefined : status,
        region,
        sort,
        limit: PAGE_SIZE,
        offset,
      }),
    retry: 1,
  })
  const del = useMutation({
    mutationFn: (r: ExplorerResource) => {
      if (r.type === 'lab') return destroyLab(r.id).then(() => undefined)
      if (r.lab_id) return deleteLabResource(r.lab_id, r.id)
      throw new Error(r.delete_hint ?? 'This resource cannot be deleted from the explorer.')
    },
    onSuccess: () => {
      setNotice('Delete requested.')
      setSelectedId(null)
      void queryClient.invalidateQueries({ queryKey: ['explorer'] })
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })

  const rows = (explorer.data?.resources ?? []).map((r) => ({ ...r, id: r.id }))
  const selected = rows.find((r) => r.id === selectedId) ?? null
  const total = explorer.data?.total ?? 0

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Resource Explorer' }]}
        title="Resource Explorer"
        description="Search across every real resource in this deployment — labs, lab resources, courses, and (for admins) users. Nothing here is synthesised."
        actions={<FidelityBadge fidelity="REAL" footnote="backend data only" />}
      />
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">
          {notice}
        </p>
      )}
      {explorer.data?.region_note && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm text-aws-muted">{explorer.data.region_note}</p>
      )}
      <AwsCard title="Filters">
        <div className="flex flex-wrap items-end gap-3">
          <label className="text-sm">
            Search
            <input
              type="search"
              value={q}
              onChange={(e) => { setQ(e.target.value); setOffset(0) }}
              placeholder="Name, ID, tag…"
              className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
            />
          </label>
          <label className="text-sm">
            Type
            <select value={type} onChange={(e) => { setType(e.target.value); setOffset(0) }} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              {TYPE_OPTIONS.map((t) => (
                <option key={t} value={t}>{t === '' ? 'All types' : t}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Status
            <input
              value={status}
              onChange={(e) => { setStatus(e.target.value); setOffset(0) }}
              placeholder="READY, available…"
              className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
            />
          </label>
          <label className="text-sm">
            Sort by
            <select value={sort} onChange={(e) => setSort(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="name">Name</option>
              <option value="created_at">Created date</option>
              <option value="status">Status</option>
            </select>
          </label>
          <span className="text-sm text-aws-muted">Region: {region} (scoping is display-only — all resources live in cf-local-1)</span>
        </div>
      </AwsCard>
      {explorer.isPending && <Loading label="Loading resources" />}
      {explorer.isError && <ErrorBox message={`Resource Explorer could not load (${explorer.error.message}). Sign in to see your resources.`} />}
      {explorer.data && (
        <AwsCard title={`Resources (${total})`}>
          <AwsTable<ExplorerResource & { id: string }>
            caption="Resources"
            emptyMessage="No resources match these filters."
            rows={rows}
            totalCount={total}
            columns={[
              { header: 'Type', render: (r) => r.type },
              { header: 'Name', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(r.id)}>{r.name}</button> },
              { header: 'ID', render: (r) => <span className="break-all">{r.id}</span> },
              { header: 'Region', render: (r) => r.region },
              { header: 'Status', render: (r) => r.status || '—' },
              { header: 'Created', render: (r) => (r.created_at ? new Date(r.created_at).toLocaleDateString() : '—') },
              { header: 'Provider', render: (r) => r.provider },
              { header: 'Fidelity', render: (r) => <FidelityBadge fidelity={r.fidelity} footnote="" /> },
            ]}
          />
          <div className="mt-3 flex items-center gap-2">
            <AwsButton disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>
              Previous
            </AwsButton>
            <AwsButton disabled={offset + PAGE_SIZE >= total} onClick={() => setOffset(offset + PAGE_SIZE)}>
              Next
            </AwsButton>
            <span className="text-xs text-aws-muted">
              Showing {total === 0 ? 0 : offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
            </span>
          </div>
          {selected && (
            <div className="mt-4 border-t border-aws-border pt-3">
              <h3 className="mb-2 text-sm font-semibold">Inspect — {selected.name}</h3>
              <dl className="space-y-1 text-sm">
                <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">ID</dt><dd className="break-all">{selected.id}</dd></div>
                <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Type</dt><dd>{selected.type}</dd></div>
                <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Status</dt><dd>{selected.status || '—'}</dd></div>
                <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Service</dt><dd>{selected.service_id}</dd></div>
                <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Lab</dt><dd>{selected.lab_id ?? '—'}</dd></div>
                {selected.tags && Object.keys(selected.tags).length > 0 && (
                  <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Tags</dt><dd>{Object.entries(selected.tags).map(([k, v]) => `${k}=${v}`).join(', ')}</dd></div>
                )}
              </dl>
              <div className="mt-3">
                <AwsButton
                  disabled={!selected.can_delete}
                  title={selected.can_delete ? 'Delete this resource' : (selected.delete_hint ?? 'Cannot be deleted from the explorer')}
                  onClick={() => del.mutate(selected)}
                >
                  Delete
                </AwsButton>
                {!selected.can_delete && selected.delete_hint && (
                  <p className="mt-1 text-xs text-aws-muted">{selected.delete_hint}</p>
                )}
              </div>
            </div>
          )}
        </AwsCard>
      )}
    </div>
  )
}
