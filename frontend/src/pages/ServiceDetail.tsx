import { useState } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { useService } from '../services/registry.ts'

/**
 * Generic service console (Phase 1). Every registry service without a
 * dedicated console (EC2, S3, VPC, IAM have their own pages) lands
 * here: overview, the operations table with fidelity badges, and the
 * linked learning exercises. Operations with no HTTP route are shown
 * disabled with the reason — never as working buttons. A service that
 * gains a dedicated console redirects to it.
 */
const DEDICATED: Record<string, string> = {
  ec2: '/services/ec2',
  s3: '/services/s3',
  vpc: '/services/vpc',
  iam: '/services/iam',
  subnet: '/services/vpc',
  routetable: '/services/vpc',
  igw: '/services/vpc',
  nat: '/services/vpc',
  sg: '/services/vpc',
}

export function ServiceDetail() {
  const { id = '' } = useParams()
  const serviceQuery = useService(id)
  const [tab, setTab] = useState<'overview' | 'operations' | 'learning'>('overview')

  if (DEDICATED[id] !== undefined) {
    return <Navigate to={DEDICATED[id]} replace />
  }

  const svc = serviceQuery.data
  if (serviceQuery.isPending) return <Loading label="Loading service" />
  if (serviceQuery.isError || !svc) {
    return <ErrorBox message={`Service "${id}" could not be loaded (${serviceQuery.error?.message ?? 'not found'}).`} />
  }

  const subNav = [
    { id: 'overview', label: 'Overview' },
    { id: 'operations', label: 'Operations' },
    { id: 'learning', label: 'Learning' },
  ]

  return (
    <ServiceConsole
      breadcrumb={[
        { label: 'CloudForge', to: '/' },
        { label: 'All Services', to: '/services' },
        { label: svc.name },
      ]}
      title={svc.name}
      description={svc.description}
      fidelity={svc.fidelity}
      fidelityFootnote={svc.differs ?? undefined}
      subNav={subNav}
      activeSub={tab}
      onSubChange={(next) => setTab(next as 'overview' | 'operations' | 'learning')}
      actions={
        <Link to={svc.route} className="rounded-sm bg-aws-orange px-3 py-1.5 text-sm font-medium text-white hover:bg-aws-orange-dark">
          Open {svc.name}
        </Link>
      }
    >
      <ConsoleTabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'operations', label: 'Operations' },
          { id: 'learning', label: 'Learning' },
        ]}
        active={tab}
        onChange={(next) => setTab(next as 'overview' | 'operations' | 'learning')}
      />
      {tab === 'overview' && (
        <AwsCard title="About this service">
          <dl className="space-y-2 text-sm">
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Category</dt><dd>{svc.category}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Availability</dt><dd>{svc.availability}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Fidelity</dt><dd><FidelityBadge fidelity={svc.fidelity} /></dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Resource types</dt><dd>{svc.resource_types.length > 0 ? svc.resource_types.join(', ') : '—'}</dd></div>
            {svc.mirrors && (
              <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Mirrors (AWS console)</dt><dd>{svc.mirrors}</dd></div>
            )}
            {svc.differs && (
              <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Differs</dt><dd>{svc.differs}</dd></div>
            )}
          </dl>
        </AwsCard>
      )}
      {tab === 'operations' && (
        <AwsCard title="Operations">
          {svc.operations.length === 0 ? (
            <p className="text-sm text-aws-muted">
              No operations are registered for this service yet. It is labelled {svc.fidelity} and its actions are
              unavailable — see the registry entry for the reason.
            </p>
          ) : (
            <table className="w-full border-collapse text-left text-xs">
              <thead>
                <tr className="bg-[#f2f3f3]">
                  <th className="border border-aws-border px-2 py-1.5">Operation</th>
                  <th className="border border-aws-border px-2 py-1.5">Fidelity</th>
                  <th className="border border-aws-border px-2 py-1.5">HTTP route</th>
                  <th className="border border-aws-border px-2 py-1.5">Limitations</th>
                </tr>
              </thead>
              <tbody>
                {svc.operations.map((op) => (
                  <tr key={op.operation}>
                    <td className="border border-aws-border px-2 py-1.5 font-medium">{op.operation}</td>
                    <td className="border border-aws-border px-2 py-1.5">
                      <FidelityBadge fidelity={op.fidelity} footnote="" />
                    </td>
                    <td className="border border-aws-border px-2 py-1.5">
                      {op.http_route ? <code>{op.http_route}</code> : <span className="text-aws-muted">No HTTP route — disabled</span>}
                    </td>
                    <td className="border border-aws-border px-2 py-1.5 text-aws-muted">
                      {(op.limitations ?? []).join(' ')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </AwsCard>
      )}
      {tab === 'learning' && (
        <AwsCard title="Learning exercises">
          {svc.learning_exercises.length === 0 ? (
            <p className="text-sm text-aws-muted">No learning track is linked to this service yet.</p>
          ) : (
            <ul className="list-disc space-y-1 pl-5 text-sm">
              {svc.learning_exercises.map((track) => (
                <li key={track}>
                  <Link to="/learn" className="text-aws-link hover:underline">
                    {track} track
                  </Link>
                </li>
              ))}
            </ul>
          )}
          <p className="mt-3 text-sm text-aws-muted">
            Documentation for this service lives in the <Link to="/learn" className="text-aws-link hover:underline">Learning Center</Link>.
          </p>
        </AwsCard>
      )}
    </ServiceConsole>
  )
}
