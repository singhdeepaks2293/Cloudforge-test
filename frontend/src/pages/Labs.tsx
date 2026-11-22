import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { ApiError, destroyLab, fetchLab, fetchLabs, getToken } from '../services/api.ts'
import type { Lab } from '../types/index.ts'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { LabDefinitionPicker } from '../components/LabLauncher.tsx'
import {
  areAllSelected,
  bulkDestroySummary,
  destroyLabsSequentially,
  toggleSelectAll,
  toggleSelected,
  type BulkDestroyResult,
} from '../hooks/useBulkDestroy.ts'

/** Map a lab to the EC2-style Instances row: resources are the "instance" details. */
function labToRow(lab: Lab) {
  const instance = lab.resources?.find((r) => r.kind === 'instance')
  return {
    id: lab.id,
    name: lab.name,
    state: lab.status,
    type: instance ? 'lab.instance' : '—',
    privateIp: instance?.private_ip ?? '—',
    publicIp: instance?.public_ip ?? '—',
    launchTime: lab.created_at ? new Date(lab.created_at).toLocaleString() : '—',
    definitionId: lab.definition_id,
    error: lab.error,
  }
}

/** Labs — EC2-style "Instances" table. Real lab sessions only. */
export function Labs() {
  const authed = getToken() !== null
  const [filter, setFilter] = useState('')
  const [showPicker, setShowPicker] = useState(false)
  const [launchedId, setLaunchedId] = useState<string | null>(null)
  const [searchParams] = useSearchParams()
  const queryClient = useQueryClient()
  const labs = useQuery({
    queryKey: ['labs'],
    queryFn: fetchLabs,
    retry: 1,
    enabled: authed,
  })

  // A lab launched from this page — or from Learn / Courses, which
  // navigate here as /labs?launched=<id> — is polled until it leaves
  // CREATED / PROVISIONING, so the user sees it reach READY instead
  // of a row that never changes.
  const trackedId = launchedId ?? searchParams.get('launched')
  const tracked = useQuery({
    queryKey: ['lab', trackedId],
    queryFn: () => fetchLab(trackedId as string),
    enabled: authed && trackedId !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'CREATED' || status === 'PROVISIONING' ? 1000 : false
    },
    retry: 1,
  })

  const onLaunched = (lab: Lab) => {
    setLaunchedId(lab.id)
    setShowPicker(false)
    void queryClient.invalidateQueries({ queryKey: ['labs'] })
  }

  // Destroy (added with the Launch-lab fix — the page previously had
  // no destroy action at all). Two-click confirm: the first click
  // arms the row's confirm buttons; the second calls
  // POST /api/v1/labs/{id}/destroy. Success refreshes the table,
  // where the row moves to DESTROYING then DESTROYED; failures
  // (401 / 404 / 409) are shown instead of failing silently.
  const [confirmDestroyId, setConfirmDestroyId] = useState<string | null>(null)
  const [destroyError, setDestroyError] = useState<string | null>(null)
  const [destroyNotice, setDestroyNotice] = useState<string | null>(null)
  const destroyMutation = useMutation({
    mutationFn: (id: string) => destroyLab(id),
    onSuccess: (lab) => {
      setConfirmDestroyId(null)
      setDestroyError(null)
      setDestroyNotice(`Lab “${lab.name}” is being destroyed.`)
      setSelectedIds((prev) => {
        const next = new Set(prev)
        next.delete(lab.id)
        return next
      })
      void queryClient.invalidateQueries({ queryKey: ['labs'] })
    },
    onError: (err: unknown) => {
      setConfirmDestroyId(null)
      setDestroyError(
        err instanceof ApiError
          ? `Could not destroy this lab: ${err.message}`
          : 'Could not destroy this lab. Please try again.',
      )
    },
  })

  // Bulk destroy (added with the Launch-lab fix): checkbox selection
  // per row + select-all in the table header, "Destroy selected"
  // when 1+ are selected, and "Destroy all labs" for everything the
  // user owns. Both run destroyLab() one lab at a time (no bulk
  // backend endpoint) and report a per-lab result; one failure
  // never aborts the rest. The confirm step always names the count.
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set())
  const [bulkConfirm, setBulkConfirm] = useState<{ kind: 'selected' | 'all'; ids: string[] } | null>(null)
  const [bulkRunning, setBulkRunning] = useState(false)
  const [bulkProgress, setBulkProgress] = useState<{ done: number; total: number; currentId: string } | null>(null)
  const [bulkResults, setBulkResults] = useState<BulkDestroyResult[] | null>(null)

  const runBulkDestroy = async (ids: string[]) => {
    setBulkConfirm(null)
    setBulkResults(null)
    setBulkRunning(true)
    setBulkProgress({ done: 0, total: ids.length, currentId: ids[0] ?? '' })
    const results = await destroyLabsSequentially(ids, destroyLab, (done, total, currentId) =>
      setBulkProgress({ done, total, currentId }),
    )
    setBulkRunning(false)
    setBulkProgress(null)
    setBulkResults(results)
    setSelectedIds(new Set())
    void queryClient.invalidateQueries({ queryKey: ['labs'] })
  }

  const rows = useMemo(() => (labs.data ?? []).map(labToRow), [labs.data])
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((r) =>
      [r.id, r.name, r.state, r.privateIp, r.publicIp, r.definitionId ?? ''].join(' ').toLowerCase().includes(q),
    )
  }, [rows, filter])
  const rowNames = useMemo(() => new Map(rows.map((r) => [r.id, r.name])), [rows])
  const filteredIds = useMemo(() => filtered.map((r) => r.id), [filtered])
  const allFilteredSelected = areAllSelected(selectedIds, filteredIds)

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'EC2' }, { label: 'Instances' }]}
        title="Instances"
        description="Your lab sessions, shown as EC2-style instances with their real lifecycle state."
        actions={
          authed ? (
            <AwsButton variant="primary" onClick={() => setShowPicker((v) => !v)}>
              {showPicker ? 'Close launcher' : 'Launch lab'}
            </AwsButton>
          ) : undefined
        }
      />

      {!authed && (
        <AwsCard title="Sign in to see your instances">
          <p className="text-sm text-aws-muted">
            Lab sessions are per-user. Please <Link to="/login">log in</Link> or{' '}
            <Link to="/register">register</Link> first.
          </p>
        </AwsCard>
      )}

      {authed && showPicker && <LabDefinitionPicker onLaunched={onLaunched} />}

      {authed && trackedId && tracked.data && (
        <AwsCard title="Launching lab">
          <p className="flex flex-wrap items-center gap-3 text-sm">
            <span className="font-medium">{tracked.data.name}</span>
            <StateBadge state={tracked.data.status} />
            {tracked.data.error && <span className="text-aws-error">{tracked.data.error}</span>}
            {tracked.data.status === 'READY' && (
              <span className="flex gap-3">
                <Link to={`/labs/${tracked.data.id}/workstation`}>Open workstation</Link>
                <Link to={`/labs/${tracked.data.id}/topology`}>Topology</Link>
              </span>
            )}
            {(tracked.data.status === 'CREATED' || tracked.data.status === 'PROVISIONING') && (
              <span className="text-aws-muted">Provisioning… this page updates automatically.</span>
            )}
          </p>
        </AwsCard>
      )}

      {authed && destroyNotice && (
        <AwsCard title="Lab destroyed">
          <p className="text-sm text-aws-muted">{destroyNotice}</p>
        </AwsCard>
      )}
      {authed && destroyError && <ErrorBox message={destroyError} />}

      {authed && bulkConfirm && (
        <AwsCard
          title={
            bulkConfirm.kind === 'all'
              ? `Destroy all ${bulkConfirm.ids.length} labs?`
              : `Destroy ${bulkConfirm.ids.length} selected ${bulkConfirm.ids.length === 1 ? 'lab' : 'labs'}?`
          }
        >
          <p className="text-sm text-aws-muted">
            This destroys {bulkConfirm.ids.length} {bulkConfirm.ids.length === 1 ? 'lab' : 'labs'}, one by one.
            Each lab is destroyed with the normal destroy action; a lab that fails (for example one that
            is already destroyed) is reported below and does not stop the others.
          </p>
          <div className="mt-3 flex gap-2">
            <AwsButton variant="danger" onClick={() => void runBulkDestroy(bulkConfirm.ids)}>
              Confirm destroy ({bulkConfirm.ids.length})
            </AwsButton>
            <AwsButton variant="secondary" onClick={() => setBulkConfirm(null)}>
              Cancel
            </AwsButton>
          </div>
        </AwsCard>
      )}

      {authed && bulkRunning && bulkProgress && (
        <AwsCard title="Destroying labs">
          <p className="text-sm text-aws-muted">
            Destroying lab {Math.min(bulkProgress.done + 1, bulkProgress.total)} of {bulkProgress.total}…
          </p>
        </AwsCard>
      )}

      {authed && !bulkRunning && bulkResults && (
        <AwsCard title="Bulk destroy finished">
          <p className="text-sm">{bulkDestroySummary(bulkResults)}</p>
          {bulkResults.some((r) => !r.ok) && (
            <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-aws-error">
              {bulkResults
                .filter((r) => !r.ok)
                .map((r) => (
                  <li key={r.id}>
                    {rowNames.get(r.id) ?? r.id} ({r.id}): {r.error}
                  </li>
                ))}
            </ul>
          )}
        </AwsCard>
      )}

      {authed && labs.isPending && <Loading label="Loading instances" />}
      {authed && labs.isError && <ErrorBox message={`Could not load labs: ${labs.error.message}`} />}
      {authed && labs.data && (
        <AwsCard title="Instances">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            {selectedIds.size > 0 && (
              <AwsButton
                variant="danger"
                disabled={bulkRunning}
                onClick={() => setBulkConfirm({ kind: 'selected', ids: [...selectedIds] })}
              >
                Destroy selected ({selectedIds.size})
              </AwsButton>
            )}
            {rows.length > 0 && (
              <AwsButton
                variant="danger"
                disabled={bulkRunning}
                onClick={() => setBulkConfirm({ kind: 'all', ids: rows.map((r) => r.id) })}
              >
                Destroy all labs ({rows.length})
              </AwsButton>
            )}
            {selectedIds.size > 0 && (
              <span className="text-xs text-aws-muted">{selectedIds.size} selected</span>
            )}
          </div>
          <AwsTable
            columns={[
              {
                header: (
                  <input
                    type="checkbox"
                    aria-label="Select all labs"
                    checked={allFilteredSelected}
                    disabled={bulkRunning || filtered.length === 0}
                    onChange={() => setSelectedIds(toggleSelectAll(selectedIds, filteredIds))}
                  />
                ),
                key: 'select',
                render: (r) => (
                  <input
                    type="checkbox"
                    aria-label={`Select lab ${r.name}`}
                    checked={selectedIds.has(r.id)}
                    disabled={bulkRunning}
                    onChange={() => setSelectedIds(toggleSelected(selectedIds, r.id))}
                  />
                ),
              },
              {
                header: 'Instance ID',
                render: (r) => <span className="font-mono">{r.id}</span>,
              },
              {
                header: 'Name',
                render: (r) => (
                  <span>
                    <span className="font-medium">{r.name}</span>
                    {r.definitionId && (
                      <span className="block text-aws-muted">Definition: {r.definitionId}</span>
                    )}
                    {r.error && <span className="block text-aws-error">{r.error}</span>}
                  </span>
                ),
              },
              { header: 'Instance state', render: (r) => <StateBadge state={r.state} /> },
              { header: 'Instance type', render: (r) => <span className="font-mono">{r.type}</span> },
              { header: 'Private IP address', render: (r) => <span className="font-mono">{r.privateIp}</span> },
              { header: 'Public IP address', render: (r) => <span className="font-mono">{r.publicIp}</span> },
              { header: 'Launch time', render: (r) => r.launchTime },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex items-center gap-3 whitespace-nowrap">
                    <Link to={`/labs/${r.id}/workstation`}>Open workstation</Link>
                    <Link to={`/labs/${r.id}/topology`}>Topology</Link>
                    {r.state !== 'DESTROYING' && r.state !== 'DESTROYED' &&
                      (confirmDestroyId === r.id ? (
                        <>
                          <AwsButton
                            variant="danger"
                            disabled={destroyMutation.isPending}
                            onClick={() => destroyMutation.mutate(r.id)}
                          >
                            {destroyMutation.isPending ? 'Destroying…' : 'Confirm destroy'}
                          </AwsButton>
                          <AwsButton variant="secondary" onClick={() => setConfirmDestroyId(null)}>
                            Cancel
                          </AwsButton>
                        </>
                      ) : (
                        <AwsButton variant="danger" onClick={() => setConfirmDestroyId(r.id)}>
                          Destroy
                        </AwsButton>
                      ))}
                  </span>
                ),
              },
            ]}
            rows={filtered}
            totalCount={rows.length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter instances"
            emptyMessage={
              rows.length === 0
                ? 'You have no lab sessions yet. Use "Launch lab" above to start one.'
                : 'No instances match the current filter.'
            }
            caption="Lab sessions as EC2-style instances"
          />
        </AwsCard>
      )}
    </div>
  )
}
