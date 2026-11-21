/**
 * Topology editor — /labs/:id/topology (Milestone 6 frontend).
 *
 * Nodes/edges are built from real data only: the live topology feed
 * (GET /api/v1/labs/{id}/topology, wired in M9 Part 3) with the
 * lab's recorded resource list as fallback. A failed feed (e.g.
 * engine down: 503) is shown as an honest error, never as invented
 * nodes.
 */
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { fetchLab, fetchLabTopology, getToken } from '../services/api.ts'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { TopologyCanvas } from '../components/TopologyCanvas.tsx'

export function Topology() {
  const { id = '' } = useParams()
  const authed = getToken() !== null
  const queryClient = useQueryClient()

  const lab = useQuery({ queryKey: ['lab', id], queryFn: () => fetchLab(id), retry: 1, enabled: authed && id !== '' })
  const topo = useQuery({
    queryKey: ['lab-topology', id],
    queryFn: () => fetchLabTopology(id),
    retry: false,
    enabled: authed && id !== '',
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['lab-topology', id] })
    void queryClient.invalidateQueries({ queryKey: ['lab', id] })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[
          { label: 'CloudForge', to: '/' },
          { label: 'EC2', to: '/labs' },
          { label: 'Instances', to: '/labs' },
          { label: 'Topology' },
        ]}
        title="Topology editor"
        description="VPC topology for this lab — nodes and edges come only from the lab's real resources."
        actions={
          lab.data ? (
            <span className="flex items-center gap-3 text-sm">
              <span className="font-semibold">{lab.data.name}</span>
              <StateBadge state={lab.data.status} />
              {lab.data.definition_id && (
                <span className="text-aws-muted">Definition: {lab.data.definition_id}</span>
              )}
            </span>
          ) : undefined
        }
      />

      {!authed && (
        <AwsCard title="Sign in first">
          <p className="text-sm text-aws-muted">
            Topology editing needs a lab session. Please <Link to="/login">log in</Link> first.
          </p>
        </AwsCard>
      )}
      {authed && lab.isPending && <Loading label="Loading lab" />}
      {authed && lab.isError && <ErrorBox message={`Could not load lab: ${lab.error.message}`} />}
      {authed && lab.data && (
        <AwsCard title={`Topology — ${lab.data.name}`}>
          {topo.isPending && <Loading label="Checking live topology feed" />}
          {topo.isError && (
            <ErrorBox message={`Topology feed error: ${topo.error.message}`} />
          )}
          <p className="mb-3 text-sm">
            <Link to={`/labs/${id}/workstation`}>Open workstation</Link>
            {' · '}
            <Link to="/labs">Back to Instances</Link>
          </p>
          <TopologyCanvas
            lab={lab.data}
            topology={topo.data ?? null}
            topologyError={topo.isError ? topo.error.message : null}
            onChanged={refresh}
          />
        </AwsCard>
      )}
    </div>
  )
}
