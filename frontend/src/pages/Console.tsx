import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { fetchTopology } from '../services/api.ts'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'

/**
 * Cloud Console — network topology with AWS-style tabs (Overview /
 * Topology nodes). Renders InspectTopology data when the backend
 * exposes GET /api/v1/network/topology; otherwise says so honestly.
 */
export function Console() {
  const topo = useQuery({ queryKey: ['topology'], queryFn: fetchTopology, retry: 1 })
  const [tab, setTab] = useState<'overview' | 'nodes'>('overview')

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'VPC' }, { label: 'Cloud Console' }]}
        title="Cloud Console"
        description="Network topology, rendered from the Rust network engine via the Go API — when it is available."
      />

      <div className="flex gap-1 border-b border-aws-border" role="tablist" aria-label="Console views">
        {(
          [
            ['overview', 'Overview'],
            ['nodes', 'Topology nodes'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={`border-b-2 px-4 py-2 text-sm font-medium ${
              tab === id
                ? 'border-aws-orange text-aws-orange-dark'
                : 'border-transparent text-aws-muted hover:text-aws-text'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <AwsCard title="Overview">
          {topo.isPending && <Loading label="Loading topology" />}
          {topo.isError && (
            <ErrorBox
              message={`The backend does not expose topology data yet (${topo.error.message}). The network engine InspectTopology RPC exists in proto/cloudforge/v1/network.proto; the REST surface for it lands with the network engine work in Milestone 4.`}
            />
          )}
          {topo.data && (
            <p className="text-sm text-aws-muted">
              The engine reports {topo.data.nodes.length} topology node
              {topo.data.nodes.length === 1 ? '' : 's'}. Switch to the Topology nodes tab for the list, or open a
              lab&apos;s <Link to="/labs">topology editor</Link> to work with a lab network.
            </p>
          )}
          {!topo.data && !topo.isPending && !topo.isError && (
            <p className="text-sm text-aws-muted">
              Nothing is drawn here instead of a fake network: a topology diagram that does not reflect real
              infrastructure would teach the wrong thing.
            </p>
          )}
        </AwsCard>
      )}

      {tab === 'nodes' && (
        <>
          {topo.isPending && <Loading label="Loading topology" />}
          {topo.isError && (
            <AwsCard title="Topology not available yet">
              <ErrorBox
                message={`The backend does not expose topology data yet (${topo.error.message}).`}
              />
              <p className="mt-3 text-sm text-aws-muted">
                Nothing is drawn here instead of a fake network: a topology diagram that does not reflect real
                infrastructure would teach the wrong thing.
              </p>
            </AwsCard>
          )}
          {topo.data && (
            <AwsCard title="Topology nodes">
              <AwsTable
                columns={[
                  { header: 'Name', render: (n) => <span className="font-medium">{n.name || n.id}</span> },
                  { header: 'ID', render: (n) => <span className="font-mono">{n.id}</span> },
                  { header: 'Kind', render: (n) => <span className="font-mono">{n.kind}</span> },
                ]}
                rows={topo.data.nodes.map((n) => ({ id: n.id, name: n.name, kind: n.kind }))}
                emptyMessage="The engine reports no topology nodes."
                caption="Topology nodes from the network engine"
              />
            </AwsCard>
          )}
        </>
      )}
    </div>
  )
}
