import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { fetchLabs, getToken } from '../services/api.ts'
import type { Lab } from '../types/index.ts'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'

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
  const labs = useQuery({
    queryKey: ['labs'],
    queryFn: fetchLabs,
    retry: 1,
    enabled: authed,
  })

  const rows = useMemo(() => (labs.data ?? []).map(labToRow), [labs.data])
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((r) =>
      [r.id, r.name, r.state, r.privateIp, r.publicIp, r.definitionId ?? ''].join(' ').toLowerCase().includes(q),
    )
  }, [rows, filter])

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'EC2' }, { label: 'Instances' }]}
        title="Instances"
        description="Your lab sessions, shown as EC2-style instances with their real lifecycle state."
        actions={
          <Link to="/learn">
            <AwsButton variant="primary">Launch lab</AwsButton>
          </Link>
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

      {authed && labs.isPending && <Loading label="Loading instances" />}
      {authed && labs.isError && <ErrorBox message={`Could not load labs: ${labs.error.message}`} />}
      {authed && labs.data && (
        <AwsCard title="Instances">
          <AwsTable
            columns={[
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
                  <span className="flex gap-3 whitespace-nowrap">
                    <Link to={`/labs/${r.id}/workstation`}>Open workstation</Link>
                    <Link to={`/labs/${r.id}/topology`}>Topology</Link>
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
                ? 'You have no lab sessions. Lab provisioning (start / stop / destroy) arrives in Milestones 2–3; this table will list sessions once the backend exposes them.'
                : 'No instances match the current filter.'
            }
            caption="Lab sessions as EC2-style instances"
          />
        </AwsCard>
      )}
    </div>
  )
}
