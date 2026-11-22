import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { createAuditExportJob, downloadAuditExport, downloadAuditExportJob, fetchAudit, fetchAuditExportJobs } from '../services/api.ts'
import type { AuditExportJob, AuditFilter } from '../services/api.ts'
import type { AuditEntry } from '../types/index.ts'

/**
 * CloudTrail console (Phase 2) — mirrors the AWS CloudTrail Event
 * history console: real audit events from the backend audit store
 * (Postgres in full mode, memory in dev), filterable by actor,
 * action, resource type/id, lab and date, with a detail view showing
 * the event metadata. Export is CSV or JSON (admin only, max 1000
 * events). No fake events — an empty result means nothing happened.
 */
export function CloudTrailConsole() {
  const [filter, setFilter] = useState<AuditFilter>({ limit: 50 })
  const [draft, setDraft] = useState({ actor_id: '', action: '', resource_type: '', resource_id: '', lab_id: '', q: '', from: '', to: '' })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [notice, setNotice] = useState('')

  const audit = useQuery({
    queryKey: ['audit', filter],
    queryFn: () => fetchAudit(filter),
    retry: 0,
  })
  const exportFile = useMutation({
    mutationFn: async (format: 'csv' | 'json') => {
      const blob = await downloadAuditExport(format)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `cloudforge-audit.${format}`
      a.click()
      URL.revokeObjectURL(url)
    },
    onSuccess: () => setNotice('Export downloaded.'),
    onError: (e: Error) => setNotice(`Export failed: ${e.message}`),
  })
  // Phase 7 (R10): durable export jobs — a full export archived
  // server-side with retention, downloadable later. The instant
  // export above streams at most 1000 events; a job holds up to
  // 10,000 and survives the session.
  const exportJobs = useQuery({
    queryKey: ['audit-export-jobs'],
    queryFn: () => fetchAuditExportJobs(),
    retry: 0,
  })
  const createJob = useMutation({
    mutationFn: async (format: 'csv' | 'json') => createAuditExportJob(format),
    onSuccess: () => {
      setNotice('Export job created.')
      void exportJobs.refetch()
    },
    onError: (e: Error) => setNotice(`Export job failed: ${e.message}`),
  })
  const downloadJob = useMutation({
    mutationFn: async (job: AuditExportJob) => {
      const blob = await downloadAuditExportJob(job.id)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = job.filename
      a.click()
      URL.revokeObjectURL(url)
    },
    onSuccess: () => setNotice('Export job downloaded.'),
    onError: (e: Error) => setNotice(`Export job download failed: ${e.message}`),
  })
  const selected = audit.data?.entries.find((e) => e.id === selectedId) ?? null
  const applyFilter = () => {
    const next: AuditFilter = { limit: 50 }
    if (draft.actor_id.trim() !== '') next.actor_id = draft.actor_id.trim()
    if (draft.action.trim() !== '') next.action = draft.action.trim()
    if (draft.resource_type.trim() !== '') next.resource_type = draft.resource_type.trim()
    if (draft.resource_id.trim() !== '') next.resource_id = draft.resource_id.trim()
    if (draft.lab_id.trim() !== '') next.lab_id = draft.lab_id.trim()
    if (draft.q.trim() !== '') next.q = draft.q.trim()
    if (draft.from !== '') next.from = new Date(draft.from).toISOString()
    if (draft.to !== '') next.to = new Date(draft.to).toISOString()
    setFilter(next)
  }

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management', to: '/services' }, { label: 'CloudTrail' }]}
        title="CloudTrail — Event history"
        description="Real audit events from the backend audit store. Admin only. Filter by actor, action, resource or date; export as CSV or JSON."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="real audit store · export max 1000 events" />}
      />
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>
      )}
      <AwsCard title="Filter events">
        <div className="flex flex-wrap items-end gap-2">
          <FilterField label="Actor ID" value={draft.actor_id} onChange={(v) => setDraft({ ...draft, actor_id: v })} />
          <FilterField label="Action" value={draft.action} onChange={(v) => setDraft({ ...draft, action: v })} placeholder="lab_created" />
          <FilterField label="Resource type" value={draft.resource_type} onChange={(v) => setDraft({ ...draft, resource_type: v })} placeholder="lab" />
          <FilterField label="Resource ID" value={draft.resource_id} onChange={(v) => setDraft({ ...draft, resource_id: v })} />
          <FilterField label="Lab ID" value={draft.lab_id} onChange={(v) => setDraft({ ...draft, lab_id: v })} />
          <FilterField label="Search" value={draft.q} onChange={(v) => setDraft({ ...draft, q: v })} placeholder="free text" />
          <label className="text-sm">
            From
            <input type="datetime-local" value={draft.from} onChange={(e) => setDraft({ ...draft, from: e.target.value })} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <label className="text-sm">
            To
            <input type="datetime-local" value={draft.to} onChange={(e) => setDraft({ ...draft, to: e.target.value })} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <AwsButton onClick={applyFilter}>Apply filters</AwsButton>
          <AwsButton
            onClick={() => {
              setDraft({ actor_id: '', action: '', resource_type: '', resource_id: '', lab_id: '', q: '', from: '', to: '' })
              setFilter({ limit: 50 })
            }}
          >
            Clear
          </AwsButton>
        </div>
        <div className="mt-3 flex gap-2">
          <AwsButton onClick={() => exportFile.mutate('csv')}>Export CSV</AwsButton>
          <AwsButton onClick={() => exportFile.mutate('json')}>Export JSON</AwsButton>
        </div>
      </AwsCard>
      <AwsCard title="Export jobs (archived, with retention)">
        <p className="text-sm text-aws-muted">
          A job archives up to 10,000 events server-side and is kept under the configured retention
          (newest {exportJobs.data?.retention ?? 7} jobs). The worker also archives audit rows older
          than 90 days to a file before deleting them.
        </p>
        <div className="mt-2 flex gap-2">
          <AwsButton onClick={() => createJob.mutate('csv')}>Create CSV job</AwsButton>
          <AwsButton onClick={() => createJob.mutate('json')}>Create JSON job</AwsButton>
        </div>
        {exportJobs.data && exportJobs.data.jobs.length > 0 && (
          <div className="mt-3">
            <AwsTable<AuditExportJob>
              columns={[
                { header: 'Created', render: (job) => new Date(job.created_at).toLocaleString() },
                { header: 'Format', render: (job) => job.format },
                { header: 'Events', render: (job) => String(job.events) },
                { header: 'Total', render: (job) => String(job.total) },
                { header: 'File', render: (job) => job.filename },
                {
                  header: 'Actions',
                  render: (job) => (
                    <AwsButton onClick={() => downloadJob.mutate(job)}>Download</AwsButton>
                  ),
                },
              ]}
              rows={exportJobs.data.jobs}
              emptyMessage="No export jobs yet."
            />
          </div>
        )}
        {exportJobs.data && exportJobs.data.jobs.length === 0 && (
          <p className="mt-2 text-sm text-aws-muted">No export jobs yet.</p>
        )}
      </AwsCard>
      {audit.isPending && <Loading label="Loading audit events" />}
      {audit.isError && (
        <ErrorBox message={`Audit events are admin-only and could not be loaded (${audit.error.message}). Sign in as an admin; other roles see this page as unavailable, not as an empty list.`} />
      )}
      {audit.data && (
        <AwsCard title={`Events (${audit.data.total})`}>
          <AwsTable<AuditEntry & { id: string }>
            caption="Audit events"
            emptyMessage="No events match these filters — nothing happened, not a missing feed."
            rows={audit.data.entries}
            totalCount={audit.data.total}
            columns={[
              { header: 'Time', render: (e) => (e.created_at ? new Date(e.created_at).toLocaleString() : '—') },
              { header: 'Actor', render: (e) => <span className="break-all font-mono text-xs">{e.actor_id}</span> },
              { header: 'Action', render: (e) => e.action },
              { header: 'Resource type', render: (e) => e.resource_type },
              { header: 'Resource', render: (e) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(e.id)}>{e.resource_id}</button> },
            ]}
          />
          {selected && <EventDetail entry={selected} onClose={() => setSelectedId(null)} />}
        </AwsCard>
      )}
    </div>
  )
}

function FilterField({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <label className="text-sm">
      {label}
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
      />
    </label>
  )
}

function EventDetail({ entry, onClose }: { entry: AuditEntry; onClose: () => void }) {
  return (
    <div className="mt-3 border-t border-aws-border pt-3">
      <h3 className="text-sm font-semibold">Event {entry.id}</h3>
      <dl className="mt-2 space-y-1 text-sm">
        <div className="flex gap-2"><dt className="w-28 shrink-0 font-medium">Time</dt><dd>{entry.created_at ? new Date(entry.created_at).toLocaleString() : '—'}</dd></div>
        <div className="flex gap-2"><dt className="w-28 shrink-0 font-medium">Actor</dt><dd className="break-all font-mono text-xs">{entry.actor_id}</dd></div>
        <div className="flex gap-2"><dt className="w-28 shrink-0 font-medium">Action</dt><dd>{entry.action}</dd></div>
        <div className="flex gap-2"><dt className="w-28 shrink-0 font-medium">Resource</dt><dd>{entry.resource_type} {entry.resource_id}</dd></div>
      </dl>
      <div className="mt-2">
        <AwsButton onClick={onClose}>Close</AwsButton>
      </div>
    </div>
  )
}
