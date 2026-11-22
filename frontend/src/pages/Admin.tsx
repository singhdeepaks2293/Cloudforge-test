import { useQuery } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { fetchAudit, fetchQuotas, getToken } from '../services/api.ts'

/**
 * Administration (Phase 1): per-role quotas and the audit log, both
 * real admin-only APIs (GET /api/v1/admin/quotas, GET /api/v1/audit).
 * Non-admins get an honest "admin only" state, not an empty table.
 * Audit retention is 90 days, delete-only (see docs/security/model.md).
 */
export function Admin() {
  const authed = getToken() !== null
  const quotas = useQuery({ queryKey: ['admin-quotas'], queryFn: fetchQuotas, retry: 0, enabled: authed })
  const audit = useQuery({ queryKey: ['admin-audit'], queryFn: () => fetchAudit(50), retry: 0, enabled: authed })

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Administration' }]}
        title="Administration"
        description="Per-role quotas and the platform audit log. Both are admin-only."
      />
      {!authed && <ErrorBox message="Sign in as an admin to use Administration." />}
      {authed && quotas.isPending && <Loading label="Loading quotas" />}
      {authed && quotas.isError && (
        <ErrorBox message="Quotas are admin-only. If you are signed in as a learner or instructor, this page is unavailable to you — that is the permission model working, not an empty result." />
      )}
      {quotas.data && (
        <AwsCard title="Per-role quotas">
          <AwsTable<{ id: string; role: string; max_labs: number; max_cpu: number; max_memory_mb: number; max_disk_gb: number; max_lab_hours: number; max_lab_minutes: number }>
            caption="Per-role quotas"
            emptyMessage="No quota sets configured."
            rows={quotas.data.map((q) => ({ ...q, id: q.role }))}
            totalCount={quotas.data.length}
            columns={[
              { header: 'Role', render: (q) => q.role },
              { header: 'Max labs', render: (q) => String(q.max_labs) },
              { header: 'Max CPU', render: (q) => String(q.max_cpu) },
              { header: 'Max memory (MiB)', render: (q) => String(q.max_memory_mb) },
              { header: 'Max disk (GiB)', render: (q) => String(q.max_disk_gb) },
              { header: 'Max lab hours', render: (q) => String(q.max_lab_hours) },
              { header: 'Max lab minutes', render: (q) => String(q.max_lab_minutes) },
            ]}
          />
        </AwsCard>
      )}
      {authed && audit.isPending && <Loading label="Loading audit log" />}
      {audit.data && (
        <AwsCard title={`Audit log (newest first, ${audit.data.total} total — 90-day retention, delete-only)`}>
          <AwsTable<{ id: string; actor_id: string; action: string; resource_type: string; resource_id: string; created_at: string }>
            caption="Audit log"
            emptyMessage="No audit entries yet."
            rows={audit.data.entries}
            totalCount={audit.data.entries.length}
            columns={[
              { header: 'Time', render: (a) => (a.created_at ? new Date(a.created_at).toLocaleString() : '—') },
              { header: 'Actor', render: (a) => <span className="break-all">{a.actor_id}</span> },
              { header: 'Action', render: (a) => a.action },
              { header: 'Resource type', render: (a) => a.resource_type },
              { header: 'Resource ID', render: (a) => <span className="break-all">{a.resource_id}</span> },
            ]}
          />
        </AwsCard>
      )}
    </div>
  )
}
