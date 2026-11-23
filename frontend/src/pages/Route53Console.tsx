import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * Route 53 console (Phase 3) — mirrors the AWS Route 53 console's
 * Hosted Zones / Records views with inline create-zone and
 * create-record forms (not a wizard), plus a Resolve tester.
 * SIMULATED: zones and records are durable, but no DNS server serves
 * them to the lab network — resolution happens against the stored
 * records via the resolve endpoint. A, CNAME and TXT only; other
 * types are rejected by the API, not faked.
 *
 * v4 survey (2026-11-22) left nav, restructured: Dashboard, Hosted
 * zones, Health checks, Profiles, Global Resolver (Global resolvers,
 * Shared DNS views), VPC Resolver (VPCs, Inbound endpoints, Outbound
 * endpoints, Rules, Query logging, Outposts), Domains (Registered
 * domains, Requests), IP-based routing (CIDR collections), Traffic
 * flow (Traffic policies, Policy records), DNS Firewall, Application
 * Recovery Controller. Only Dashboard and Hosted zones have a
 * backend; the rest are honest UNSUPPORTED sections. Hosted-zone
 * columns are NOT OBSERVABLE in the survey, so the existing columns
 * are kept, not invented.
 */
export function Route53Console({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'zones')
  const [zoneName, setZoneName] = useState('')
  const [selected, setSelected] = useState('')
  const [recName, setRecName] = useState('')
  const [recType, setRecType] = useState('A')
  const [recValues, setRecValues] = useState('10.0.0.5')
  const [resolveName, setResolveName] = useState('')
  const [resolveType, setResolveType] = useState('A')
  const [resolveResult, setResolveResult] = useState('')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')

  const zones = useQuery({ queryKey: ['dns-zones'], queryFn: () => request<Rec>('/api/v1/aws/route53/zones'), retry: 1 })
  const zoneRows = listOf(zones.data, 'zones').filter(
    (z) => filter.trim() === '' || String(z.name ?? '').toLowerCase().includes(filter.toLowerCase()) || String(z.id).toLowerCase().includes(filter.toLowerCase()),
  )
  const records = useQuery({
    queryKey: ['dns-records', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/route53/zones/${selected}/records`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['dns-zones'] })
    void queryClient.invalidateQueries({ queryKey: ['dns-records', selected] })
  }
  const createZone = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/route53/zones', { method: 'POST', body: { name: zoneName.trim() } }),
    onSuccess: (d) => {
      setNotice(`Hosted zone ${String(d.name)} created.`)
      setZoneName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create zone failed: ${e.message}`),
  })
  const createRecord = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/route53/zones/${selected}/records`, {
      method: 'POST',
      body: { name: recName.trim(), type: recType, ttl: 300, values: recValues.split(',').map((v) => v.trim()).filter((v) => v !== '') },
    }),
    onSuccess: () => {
      setNotice('Record created.')
      setRecName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create record failed: ${e.message}`),
  })

  const selZone = zoneRows.find((z) => String(z.id) === selected)

  return (
    <ServiceConsole
      icon="network"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Route 53' }]}
      title="Route 53 — Hosted Zones"
      description="DNS zones and records. Durable, resolvable through the resolve endpoint — no DNS server serves these records to the lab network."
      fidelity="SIMULATED"
      fidelityFootnote="stored records · resolve endpoint only"
      subNav={[
        { id: 'home', label: 'Dashboard' },
        { id: 'zones', label: 'Hosted zones' },
        { id: 'health-checks', label: 'Health checks' },
        { id: 'profiles', label: 'Profiles' },
        { id: 'global-resolvers', label: 'Global resolvers', group: 'Global Resolver' },
        { id: 'shared-dns-views', label: 'Shared DNS views', group: 'Global Resolver' },
        { id: 'resolver-vpcs', label: 'VPCs', group: 'VPC Resolver' },
        { id: 'inbound-endpoints', label: 'Inbound endpoints', group: 'VPC Resolver' },
        { id: 'outbound-endpoints', label: 'Outbound endpoints', group: 'VPC Resolver' },
        { id: 'resolver-rules', label: 'Rules', group: 'VPC Resolver' },
        { id: 'query-logging', label: 'Query logging', group: 'VPC Resolver' },
        { id: 'outposts', label: 'Outposts', group: 'VPC Resolver' },
        { id: 'registered-domains', label: 'Registered domains', group: 'Domains' },
        { id: 'domain-requests', label: 'Requests', group: 'Domains' },
        { id: 'cidr-collections', label: 'CIDR collections', group: 'IP-based routing' },
        { id: 'traffic-policies', label: 'Traffic policies', group: 'Traffic flow' },
        { id: 'policy-records', label: 'Policy records', group: 'Traffic flow' },
        { id: 'dns-firewall', label: 'DNS Firewall' },
        { id: 'arc', label: 'Application Recovery Controller' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="Route 53"
            banner="Hosted zones and DNS records — durable, resolvable via the resolve endpoint. No DNS server serves these records to the lab network."
            actions={[
              { label: 'Create hosted zone', primary: true, onClick: () => setSection('zones') },
            { label: 'Learning Center — networking track', to: '/learn' },
            ]}
            cards={[
              { label: 'Hosted zones', count: zoneRows.length, onSelect: () => setSection('zones') },
            { label: 'Records', count: selected !== '' ? listOf(records.data, 'records').length : '—', onSelect: () => setSection('zones'), note: 'A, CNAME and TXT only' },
            { label: 'Health checks', count: 0, onSelect: () => setSection('health-checks'), note: 'UNSUPPORTED — no health-check API.' },
            { label: 'Route 53 Resolver', count: 0, onSelect: () => setSection('resolver-vpcs'), note: 'UNSUPPORTED — no resolver API.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={zones.isFetching}
            health={[
              zones.isError ? 'Hosted zones could not be loaded.' : `${zoneRows.length} hosted zone(s) listed.`,
            "Resolution is against the stored records via the resolve endpoint.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — networking track', to: '/learn' },
            { label: 'Global Accelerator (unsupported — exam concept)', to: '/services/globalaccelerator' },
            ]}
          />
      )}
      {section === 'zones' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {resolveResult !== '' && <p className="border border-aws-border bg-white px-4 py-2 font-mono text-sm" role="status">{resolveResult}</p>}
      {zones.isPending && <Loading label="Loading hosted zones" />}
      {zones.isError && <ErrorBox message={`Hosted zones could not be loaded (${zones.error.message}).`} />}
      {zones.data && (
        <AwsCard title={`Hosted Zones (${zoneRows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (zoneName.trim() !== '') createZone.mutate()
            }}
          >
            <label htmlFor="dns-zone" className="text-sm font-medium">Zone name</label>
            <input id="dns-zone" value={zoneName} onChange={(e) => setZoneName(e.target.value)} placeholder="app.local" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={createZone.isPending}>Create hosted zone</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="Hosted zones"
            emptyMessage="No hosted zones yet. Create one above."
            rows={zoneRows}
            totalCount={listOf(zones.data, 'zones').length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter hosted zones by name"
            columns={[
              { header: 'Zone name', render: (z) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(z.id))}>{String(z.name)}</button> },
              { header: 'Created', render: (z) => String(z.created_at ?? '—') },
              {
                header: 'Actions',
                render: (z) => (
                  <AwsButton
                    variant="secondary"
                    onClick={() => {
                      void request(`/api/v1/aws/route53/zones/${String(z.id)}`, { method: 'DELETE' }).then(() => {
                        setNotice('Zone deleted.')
                        setSelected('')
                        refresh()
                      }).catch((e: Error) => setNotice(`Delete zone failed: ${e.message}`))
                    }}
                  >
                    Delete
                  </AwsButton>
                ),
              },
            ]}
          />
          <p className="mt-2 text-sm text-aws-muted">A zone cannot be deleted while it holds records — delete the records first (same rule as Route 53).</p>
        </AwsCard>
      )}
      {selZone && (
        <AwsCard title={`Records — ${String(selZone.name)}`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (recName.trim() !== '') createRecord.mutate()
            }}
          >
            <label htmlFor="dns-rec-name" className="text-sm font-medium">Record name</label>
            <input id="dns-rec-name" value={recName} onChange={(e) => setRecName(e.target.value)} placeholder="api.app.local" className="w-56 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Type
              <select value={recType} onChange={(e) => setRecType(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="A">A</option>
                <option value="CNAME">CNAME</option>
                <option value="TXT">TXT</option>
              </select>
            </label>
            <label htmlFor="dns-rec-values" className="text-sm font-medium">Values (comma-separated)</label>
            <input id="dns-rec-values" value={recValues} onChange={(e) => setRecValues(e.target.value)} placeholder="10.0.0.5" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={createRecord.isPending}>Create record</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="Record sets"
            emptyMessage="No records in this zone yet."
            rows={listOf(records.data, 'records')}
            totalCount={listOf(records.data, 'records').length}
            columns={[
              { header: 'Record name', render: (r) => String(r.name) },
              { header: 'Type', render: (r) => String(r.type ?? '—') },
              { header: 'TTL', render: (r) => String(r.ttl ?? '—') },
              { header: 'Values', render: (r) => (Array.isArray(r.values) ? (r.values as unknown[]).join(', ') : '—') },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton
                    variant="secondary"
                    onClick={() => {
                      void request(`/api/v1/aws/route53/zones/${selected}/records/${String(r.id)}`, { method: 'DELETE' }).then(() => {
                        setNotice('Record deleted.')
                        refresh()
                      }).catch((e: Error) => setNotice(`Delete record failed: ${e.message}`))
                    }}
                  >
                    Delete
                  </AwsButton>
                ),
              },
            ]}
          />
          <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-aws-border pt-3">
            <span className="text-sm font-medium">Resolve (against stored records):</span>
            <input value={resolveName} onChange={(e) => setResolveName(e.target.value)} placeholder="api.app.local" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <select value={resolveType} onChange={(e) => setResolveType(e.target.value)} className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="A">A</option>
              <option value="CNAME">CNAME</option>
              <option value="TXT">TXT</option>
            </select>
            <AwsButton
              variant="secondary"
              onClick={() => {
                void request<Rec>(`/api/v1/aws/route53/zones/${selected}/resolve?name=${encodeURIComponent(resolveName)}&type=${resolveType}`).then((d) => {
                  setResolveResult(`${String(d.name)} ${String(d.type)}: ${Array.isArray(d.answers) && (d.answers as unknown[]).length > 0 ? (d.answers as unknown[]).join(', ') : '(no answer)'}`)
                }).catch((e: Error) => setResolveResult(`Resolve failed: ${e.message}`))
              }}
            >
              Resolve
            </AwsButton>
            <span className="text-sm text-aws-muted">Wildcard (*.app.local) records answer matching names.</span>
          </div>
        </AwsCard>
      )}
        </>
      )}
      {section === 'health-checks' && (
        <UnsupportedSection
          title="Health checks"
          reason="Health checks are UNSUPPORTED in CloudForge — there is no health-check API; the resolve endpoint answers from stored records only."
          actionLabel="Create health check"
        />
      )}
      {section === 'profiles' && (
        <UnsupportedSection
          title="Profiles"
          reason="Profiles are UNSUPPORTED in CloudForge — there is no Route 53 profile API."
          actionLabel="Create profile"
        />
      )}
      {section === 'global-resolvers' && (
        <UnsupportedSection
          title="Global resolvers"
          reason="Global resolvers are UNSUPPORTED in CloudForge — there is no global-resolver API; resolution is against stored records via the resolve endpoint."
          actionLabel="Create global resolver"
        />
      )}
      {section === 'shared-dns-views' && (
        <UnsupportedSection
          title="Shared DNS views"
          reason="Shared DNS views are UNSUPPORTED in CloudForge — there is no shared-DNS-view API."
          actionLabel="Create shared DNS view"
        />
      )}
      {section === 'resolver-vpcs' && (
        <UnsupportedSection
          title="VPC Resolver — VPCs"
          reason="VPC Resolver is UNSUPPORTED in CloudForge — there is no resolver API; zones are not attached to lab VPCs and private zones are taught in the SAP track, not built here."
          actionLabel="Associate VPC"
        />
      )}
      {section === 'inbound-endpoints' && (
        <UnsupportedSection
          title="Inbound endpoints"
          reason="Inbound endpoints are UNSUPPORTED in CloudForge — there is no resolver-endpoint API."
          actionLabel="Create inbound endpoint"
        />
      )}
      {section === 'outbound-endpoints' && (
        <UnsupportedSection
          title="Outbound endpoints"
          reason="Outbound endpoints are UNSUPPORTED in CloudForge — there is no resolver-endpoint API."
          actionLabel="Create outbound endpoint"
        />
      )}
      {section === 'resolver-rules' && (
        <UnsupportedSection
          title="Resolver rules"
          reason="Resolver rules are UNSUPPORTED in CloudForge — there is no resolver-rule API."
          actionLabel="Create rule"
        />
      )}
      {section === 'query-logging' && (
        <UnsupportedSection
          title="Query logging"
          reason="Query logging is UNSUPPORTED in CloudForge — there is no query-logging API; the resolve endpoint answers from stored records without logging queries."
          actionLabel="Configure query logging"
        />
      )}
      {section === 'outposts' && (
        <UnsupportedSection
          title="Outposts"
          reason="Outposts are UNSUPPORTED in CloudForge — there is no Outposts resolver API."
          actionLabel="Create Outpost resolver"
        />
      )}
      {section === 'registered-domains' && (
        <UnsupportedSection
          title="Registered domains"
          reason="Registered domains are UNSUPPORTED in CloudForge — there is no domain-registration API; zones use local names like app.local."
          actionLabel="Register domain"
        />
      )}
      {section === 'domain-requests' && (
        <UnsupportedSection
          title="Requests"
          reason="Domain requests are UNSUPPORTED in CloudForge — there is no domain-registration API, so there are no registration requests."
          actionLabel="View requests"
        />
      )}
      {section === 'cidr-collections' && (
        <UnsupportedSection
          title="CIDR collections"
          reason="CIDR collections are UNSUPPORTED in CloudForge — there is no CIDR-collection API for IP-based routing."
          actionLabel="Create CIDR collection"
        />
      )}
      {section === 'traffic-policies' && (
        <UnsupportedSection
          title="Traffic policies"
          reason="Traffic policies are UNSUPPORTED in CloudForge — there is no traffic-policy API; the resolve endpoint answers from stored records only."
          actionLabel="Create traffic policy"
        />
      )}
      {section === 'policy-records' && (
        <UnsupportedSection
          title="Policy records"
          reason="Policy records are UNSUPPORTED in CloudForge — there is no traffic-policy API, so no policy records exist."
          actionLabel="Create policy record"
        />
      )}
      {section === 'dns-firewall' && (
        <UnsupportedSection
          title="DNS Firewall"
          reason="DNS Firewall is UNSUPPORTED in CloudForge — there is no DNS Firewall API and no DNS filtering; see also the DNS Firewall section on the VPC console."
          actionLabel="Create rule group"
        />
      )}
      {section === 'arc' && (
        <UnsupportedSection
          title="Application Recovery Controller"
          reason="Application Recovery Controller is UNSUPPORTED in CloudForge — there is no recovery-controller API."
          actionLabel="Create recovery control"
        />
      )}
    </ServiceConsole>
  )
}
