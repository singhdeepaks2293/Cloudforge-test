import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * Route 53 console (Phase 3) — mirrors the AWS Route 53 console's
 * Hosted Zones / Records views and the Create-record wizard, plus a
 * Resolve tester. SIMULATED: zones and records are durable, but no
 * DNS server serves them to the lab network — resolution happens
 * against the stored records via the resolve endpoint. A, CNAME and
 * TXT only; other types are rejected by the API, not faked.
 */
export function Route53Console() {
  const queryClient = useQueryClient()
  const [zoneName, setZoneName] = useState('')
  const [selected, setSelected] = useState('')
  const [recName, setRecName] = useState('')
  const [recType, setRecType] = useState('A')
  const [recValues, setRecValues] = useState('10.0.0.5')
  const [resolveName, setResolveName] = useState('')
  const [resolveType, setResolveType] = useState('A')
  const [resolveResult, setResolveResult] = useState('')
  const [notice, setNotice] = useState('')

  const zones = useQuery({ queryKey: ['dns-zones'], queryFn: () => request<Rec>('/api/v1/aws/route53/zones'), retry: 1 })
  const zoneRows = listOf(zones.data, 'zones')
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
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Route 53' }]}
        title="Route 53 — Hosted Zones"
        description="DNS zones and records. Durable, resolvable through the resolve endpoint — no DNS server serves these records to the lab network."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="stored records · resolve endpoint only" />}
      />
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
            totalCount={zoneRows.length}
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
    </div>
  )
}
