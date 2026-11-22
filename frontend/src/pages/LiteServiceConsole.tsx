import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ServiceHomeDashboard, type ServiceHomeCard } from '../components/ServiceHome.tsx'
import { AwsFlash, AwsKeyValue } from '../components/AwsShell.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { fetchExplorerResources } from '../services/api.ts'
import { useService } from '../services/registry.ts'
import { getStoredRegion } from '../services/catalog.ts'
import type { RegistryOperation } from '../types/index.ts'
import { LITE_GENERIC_SERVICES } from '../services/liteServices.ts'

/**
 * v3 — generic console for the lite (SAA-C03 / SAP-C02) exam services
 * that have no dedicated console and no backend runtime. Every one of
 * them gets the full AWS-reference treatment, honestly:
 *
 * - Home dashboard (VPC pattern): banner, action links, "Resources by
 *   Region" cards per resource type (counts from the live Resource
 *   Explorer — 0 recorded, never invented), Service Health, Settings,
 *   Additional Information.
 * - List page(s): the service's registered operations as an AwsTable
 *   with text + fidelity filters — the real list surface for these
 *   services, since there are no resources to list.
 * - Detail tabs: selecting an operation opens Details / Limitations
 *   tabs; the action button is disabled with the reason when there is
 *   no HTTP route. Nothing pretends to work.
 * - "What you can do instead": links to the available CloudForge
 *   service that teaches the same concept.
 *
 * Services covered (registry ids): autoscaling, aurora, backup,
 * macie, auditmanager, artifact, cloudfront, ebs, efs, fsx, shield,
 * controltower, identitycenter, ram, vpn, directconnect,
 * globalaccelerator, mgn. Non-lite services keep their v2 consoles
 * and must not be routed here.
 */
export function LiteServiceConsole({ initialSection }: { initialSection?: string }) {
  const { id = '' } = useParams()
  const serviceQuery = useService(id)
  const [section, setSection] = useState(initialSection ?? 'home')
  const [filter, setFilter] = useState('')
  const [fidelityFilter, setFidelityFilter] = useState('all')
  const [selectedOp, setSelectedOp] = useState<string | null>(null)
  const [detailTab, setDetailTab] = useState<'details' | 'limitations'>('details')
  const region = getStoredRegion()

  const svc = serviceQuery.data
  const generic = LITE_GENERIC_SERVICES[id]
  const resourceTypes = svc?.resource_types ?? []
  const firstType = resourceTypes[0] ?? ''
  const explorer = useQuery({
    queryKey: ['lite-explorer', id, firstType],
    queryFn: () => fetchExplorerResources({ type: firstType }),
    retry: 1,
    enabled: svc !== undefined && firstType !== '',
  })

  const operations = useMemo(() => {
    const ops = svc?.operations ?? []
    const q = filter.trim().toLowerCase()
    return ops.filter((op) => {
      if (fidelityFilter !== 'all' && op.fidelity !== fidelityFilter) return false
      if (q !== '' && !op.operation.toLowerCase().includes(q)) return false
      return true
    })
  }, [svc, filter, fidelityFilter])

  const selected = (svc?.operations ?? []).find((o) => o.operation === selectedOp) ?? null

  if (generic === undefined) {
    return <ErrorBox message={`"${id}" is not a lite generic service — it has its own console or is not in the lite exam set.`} />
  }
  if (serviceQuery.isPending) return <Loading label="Loading service" />
  if (serviceQuery.isError || !svc) {
    return <ErrorBox message={`Service "${id}" could not be loaded (${serviceQuery.error?.message ?? 'not found'}).`} />
  }

  const explorerCount: number | string = explorer.data !== undefined ? explorer.data.total : '—'
  const cards: ServiceHomeCard[] = resourceTypes.map((rt) => ({
    label: titleCase(rt),
    count: resourceTypes.length === 1 ? explorerCount : '—',
    onSelect: () => setSection(`type:${rt}`),
    note:
      resourceTypes.length === 1
        ? explorer.isError
          ? 'Explorer could not be loaded — count unavailable, not zero.'
          : 'Recorded in the Resource Explorer'
        : 'No list API for this resource type.',
    disabledReason: svc.fidelity === 'UNSUPPORTED' ? 'UNSUPPORTED — no backend for this service.' : undefined,
  }))
  if (cards.length === 0) {
    cards.push({ label: 'Resources', count: 0, disabledReason: 'No resource types are registered for this service.' })
  }

  const subNav = [
    { id: 'home', label: `${svc.name} dashboard` },
    ...resourceTypes.map((rt) => ({ id: `type:${rt}`, label: titleCase(rt) })),
    // Survey 2026-11-22: CloudFront lands on Distributions, with the
    // survey's exact columns. CloudFront is UNSUPPORTED, so the table
    // is empty — but the columns are the observed ones, not invented.
    ...(id === 'cloudfront' ? [{ id: 'distributions', label: 'Distributions' }] : []),
    { id: 'operations', label: 'Operations' },
    { id: 'learning', label: 'Learning' },
  ]

  return (
    <ServiceConsole
      icon={svc.icon}
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: svc.category, to: '/services' }, { label: svc.name }]}
      title={svc.name}
      description={svc.description}
      fidelity={svc.fidelity}
      fidelityFootnote={svc.differs ?? generic.note}
      subNav={subNav}
      activeSub={section}
      onSubChange={(next) => {
        setSection(next)
        setSelectedOp(null)
      }}
    >
      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName={svc.name}
          banner={
            svc.fidelity === 'UNSUPPORTED'
              ? `${svc.name} is UNSUPPORTED in CloudForge — catalogued for SAA-C03 / SAP-C02 exam vocabulary only. ${generic.note ?? svc.differs ?? ''} Nothing here is faked: there are no resources to list and no working actions.`
              : `${svc.name} — ${svc.description} ${generic.note ?? ''}`
          }
          bannerTone={svc.fidelity === 'UNSUPPORTED' ? 'error' : 'info'}
          actions={[
            { label: `Create ${resourceTypes[0] ? titleCase(resourceTypes[0]).replace(/s$/, '') : 'resource'}`, primary: true, disabledReason: `${svc.name} has no create API in CloudForge — ${generic.note ?? svc.differs ?? 'unsupported.'}` },
            { label: 'Learning Center', to: '/learn' },
          ]}
          cards={cards}
          onRefresh={() => void explorer.refetch()}
          refreshing={explorer.isFetching}
          health={[
            `Region: ${region} (region scoping is display-only; all resources live in cf-local-1).`,
            `Registry fidelity: ${svc.fidelity}; availability: ${svc.availability}.`,
            explorer.isError
              ? 'Resource Explorer could not be loaded.'
              : `${explorerCount} resource(s) of type "${firstType}" recorded in the explorer.`,
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — SAA-C03 / SAP-C02 tracks', to: '/learn' },
            { label: 'Certification coverage map', to: '/learn' },
          ]}
        >
          <AwsCard title="What you can do instead">
            <ul className="space-y-2 text-sm">
              {generic.alternatives.map((a) => (
                <li key={a.label}>
                  <Link to={a.to} className="text-aws-link hover:underline">
                    {a.label}
                  </Link>
                  <span className="text-aws-muted"> — {a.note}</span>
                </li>
              ))}
            </ul>
          </AwsCard>
          {id === 'backup' && (
            <AwsCard title="Backup scripts (host-side)">
              <p className="text-sm text-aws-muted">
                Run on the CloudForge host, not from this console: <code>scripts/backup.sh</code> writes a pg_dump
                -Fc file (default <code>./backups</code>, keeps 7), and <code>scripts/restore.sh</code> restores it
                with pg_restore. The backup/restore round-trip was verified in Milestone 9.
              </p>
            </AwsCard>
          )}
        </ServiceHomeDashboard>
      )}
      {section.startsWith('type:') && (
        <AwsCard title={`${titleCase(section.slice(5))} — ${region}`}>
          <p className="text-sm text-aws-muted">
            No {titleCase(section.slice(5)).toLowerCase()} resources are recorded in CloudForge — {svc.name} is{' '}
            {svc.fidelity === 'UNSUPPORTED' ? 'UNSUPPORTED' : svc.fidelity.toLowerCase()} and has no list API for
            this resource type. The explorer count on the dashboard is the live count (0 recorded), not a placeholder.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setSection('operations')}>View registered operations</AwsButton>{' '}
            <AwsButton onClick={() => setSection('home')}>Back to {svc.name} dashboard</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'distributions' && id === 'cloudfront' && (
        <AwsCard title="Distributions (0)">
          <p className="mb-3 text-sm text-aws-muted">
            CloudFront is <strong>UNSUPPORTED</strong> in CloudForge — no distributions exist. The table below uses
            the survey&apos;s exact Distributions columns; every column is empty because there is nothing to list.
            Create distribution is disabled: there is no distribution API.
          </p>
          <AwsTable<{ id: string }>
            caption="CloudFront distributions"
            emptyMessage="No distributions — CloudFront is UNSUPPORTED in CloudForge."
            rows={[]}
            totalCount={0}
            columns={[
              { header: 'ID', render: () => '—' },
              { header: 'Status', render: () => '—' },
              { header: 'Description', render: () => '—' },
              { header: 'Type', render: () => '—' },
              { header: 'Domain name', render: () => '—' },
              { header: 'Alternate domain names', render: () => '—' },
              { header: 'Origins', render: () => '—' },
              { header: 'Pricing plan', render: () => '—' },
              { header: 'Last modified', render: () => '—' },
            ]}
          />
        </AwsCard>
      )}
      {section === 'operations' && (
        <div className="space-y-4">
          <AwsCard title={`Operations (${operations.length} of ${(svc.operations ?? []).length}) — region ${region}`}>
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <label className="text-sm">
                Filter
                <input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter by operation name…"
                  className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
                />
              </label>
              <label className="text-sm">
                Fidelity
                <select value={fidelityFilter} onChange={(e) => setFidelityFilter(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="all">All</option>
                  <option value="REAL">REAL</option>
                  <option value="API_EMULATED">API_EMULATED</option>
                  <option value="PARTIAL">PARTIAL</option>
                  <option value="SIMULATED">SIMULATED</option>
                  <option value="UNSUPPORTED">UNSUPPORTED</option>
                </select>
              </label>
            </div>
            <AwsTable<RegistryOperation & { id: string }>
              caption={`${svc.name} operations`}
              emptyMessage={
                svc.operations.length === 0
                  ? `No operations are registered for ${svc.name} — it is catalogued ${svc.fidelity} with no API behind it.`
                  : 'No operations match this filter.'
              }
              rows={operations.map((op) => ({ ...op, id: op.operation }))}
              totalCount={operations.length}
              columns={[
                { header: 'Operation', render: (op) => <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelectedOp(op.operation); setDetailTab('details') }}>{op.operation}</button> },
                { header: 'Fidelity', render: (op) => <FidelityBadge fidelity={op.fidelity} footnote="" /> },
                { header: 'Supported', render: (op) => (op.supported ? 'Yes' : 'No') },
                { header: 'HTTP route', render: (op) => (op.http_route ? <code>{op.http_route}</code> : <span className="text-aws-muted">No HTTP route — disabled</span>) },
                { header: 'Limitations', render: (op) => <span className="text-aws-muted">{(op.limitations ?? []).join(' ') || '—'}</span> },
              ]}
            />
          </AwsCard>
          {selected !== null && (
            <AwsCard title={`Operation: ${selected.operation}`}>
              <ConsoleTabs
                tabs={[
                  { id: 'details', label: 'Details' },
                  { id: 'limitations', label: 'Limitations' },
                ]}
                active={detailTab}
                onChange={(next) => setDetailTab(next as 'details' | 'limitations')}
              />
              {detailTab === 'details' && (
                <div className="mt-3 space-y-3">
                  <AwsKeyValue
                    rows={[
                      { label: 'Operation', value: selected.operation },
                      { label: 'Fidelity', value: <FidelityBadge fidelity={selected.fidelity} footnote="" /> },
                      { label: 'Supported', value: selected.supported ? 'Yes' : 'No' },
                      { label: 'HTTP route', value: selected.http_route ?? 'None — no backend route serves this operation.' },
                      { label: 'Service', value: `${svc.name} (${svc.fidelity})` },
                    ]}
                  />
                  <AwsButton
                    variant="primary"
                    disabled
                  >
                    Run {selected.operation}
                  </AwsButton>
                  <p className="text-sm text-aws-muted">
                    Disabled: {selected.http_route ? 'this operation has a route but no generic runner in this console.' : 'no HTTP route serves this operation in CloudForge.'}
                  </p>
                </div>
              )}
              {detailTab === 'limitations' && (
                <div className="mt-3">
                  {(selected.limitations ?? []).length === 0 ? (
                    <p className="text-sm text-aws-muted">No limitations are recorded for this operation.</p>
                  ) : (
                    <ul className="list-disc space-y-1 pl-5 text-sm">
                      {(selected.limitations ?? []).map((l) => (
                        <li key={l}>{l}</li>
                      ))}
                    </ul>
                  )}
                  {svc.differs !== undefined && svc.differs !== '' && (
                    <p className="mt-2 text-sm text-aws-muted">Differs from AWS: {svc.differs}</p>
                  )}
                </div>
              )}
            </AwsCard>
          )}
        </div>
      )}
      {section === 'learning' && (
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
            {svc.name} is taught in the SAA-C03 / SAP-C02 tracks — see the certification coverage map in the{' '}
            <Link to="/learn" className="text-aws-link hover:underline">
              Learning Center
            </Link>
            .
          </p>
          {generic.note !== undefined && (
            <AwsFlash tone="info">{generic.note}</AwsFlash>
          )}
        </AwsCard>
      )}
    </ServiceConsole>
  )
}

function titleCase(s: string): string {
  return s
    .split(/[-_ ]+/)
    .map((w) => (w.length === 0 ? w : w[0].toUpperCase() + w.slice(1)))
    .join(' ')
}
