import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
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
 * Elastic Load Balancing console (Phase 3) — mirrors the AWS ELB
 * console's Load Balancers list, Listeners and Target Groups tabs.
 * SIMULATED: in-process round-robin with real HTTP/TCP health
 * probes. No engine RPC, no nginx reconfiguration; the DNS name is
 * a local teaching name. Only targets whose last probe said healthy
 * are eligible for routing — unprobed targets are never assumed
 * healthy.
 */
/**
 * Load-balancer list content without the console shell, so the EC2
 * console can render it as its Load Balancing section (the survey
 * found ELB has no standalone console — it is the EC2 console's
 * Load Balancing section). ElbConsole below keeps the standalone
 * shell for the /services/elb bookmark, which now redirects to the
 * EC2 section.
 */
export function ElbListContent() {
  const queryClient = useQueryClient()
  const [lbName, setLbName] = useState('')
  const [tgName, setTgName] = useState('')
  const [targetAddr, setTargetAddr] = useState('')
  const [selectedLb, setSelectedLb] = useState('')
  const [selectedTg, setSelectedTg] = useState('')
  const [routeResult, setRouteResult] = useState('')
  const [notice, setNotice] = useState('')

  const lbs = useQuery({ queryKey: ['elb-lbs'], queryFn: () => request<Rec>('/api/v1/aws/elb/load-balancers'), retry: 1 })
  const tgs = useQuery({ queryKey: ['elb-tgs'], queryFn: () => request<Rec>('/api/v1/aws/elb/target-groups'), retry: 1 })
  const lbRows = listOf(lbs.data, 'load_balancers')
  const tgRows = listOf(tgs.data, 'target_groups')
  const tgDetail = useQuery({
    queryKey: ['elb-tg', selectedTg],
    queryFn: () => request<Rec>(`/api/v1/aws/elb/target-groups/${selectedTg}`),
    enabled: selectedTg !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['elb-lbs'] })
    void queryClient.invalidateQueries({ queryKey: ['elb-tgs'] })
    void queryClient.invalidateQueries({ queryKey: ['elb-tg', selectedTg] })
  }
  const createLb = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/elb/load-balancers', { method: 'POST', body: { name: lbName.trim(), scheme: 'internet-facing' } }),
    onSuccess: (d) => {
      setNotice(`Load balancer ${String(d.name)} created — DNS name ${String(d.dns_name)} (local teaching name, not registered).`)
      setLbName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create load balancer failed: ${e.message}`),
  })
  const createTg = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/elb/target-groups', { method: 'POST', body: { name: tgName.trim(), protocol: 'HTTP', port: 80, health_check_path: '/' } }),
    onSuccess: () => {
      setNotice('Target group created.')
      setTgName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create target group failed: ${e.message}`),
  })
  const addTarget = useMutation({
    mutationFn: () => request(`/api/v1/aws/elb/target-groups/${selectedTg}/targets`, { method: 'POST', body: { address: targetAddr.trim(), port: 80 } }),
    onSuccess: () => {
      setNotice(`Target ${targetAddr.trim()} registered (health unknown until probed).`)
      setTargetAddr('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Register target failed: ${e.message}`),
  })
  const health = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/elb/target-groups/${selectedTg}/health`, { method: 'POST', body: {} }),
    onSuccess: (d) => {
      setNotice(`Health check: ${String(d.healthy ?? 0)}/${String(d.total ?? 0)} healthy.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Health check failed: ${e.message}`),
  })

  return (
    <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {routeResult !== '' && <p className="border border-aws-border bg-white px-4 py-2 font-mono text-sm" role="status">{routeResult}</p>}
      {lbs.isPending && <Loading label="Loading load balancers" />}
      {lbs.isError && <ErrorBox message={`Load balancers could not be loaded (${lbs.error.message}).`} />}
      {lbs.data && (
        <AwsCard title={`Load Balancers (${lbRows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (lbName.trim() !== '') createLb.mutate()
            }}
          >
            <label htmlFor="elb-name" className="text-sm font-medium">Load balancer name</label>
            <input id="elb-name" value={lbName} onChange={(e) => setLbName(e.target.value)} placeholder="web-lb" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={createLb.isPending}>Create load balancer</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="Load balancers"
            emptyMessage="No load balancers yet. Create one above."
            rows={lbRows}
            totalCount={lbRows.length}
            columns={[
              { header: 'Name', render: (lb) => <button type="button" className="text-aws-link underline" onClick={() => setSelectedLb(String(lb.id))}>{String(lb.name)}</button> },
              { header: 'State', render: (lb) => String(lb.status ?? '—') },
              { header: 'Type', render: () => <span title="CloudForge does not model load balancer types (Application / Network / Gateway)">Not available</span> },
              { header: 'Scheme', render: (lb) => String(lb.scheme ?? '—') },
              { header: 'IP address type', render: () => <span title="CloudForge load balancers have no IP address type">Not available</span> },
              { header: 'VPC ID', render: () => <span title="CloudForge load balancers are not attached to a VPC">Not available</span> },
              { header: 'Availability Zones', render: () => <span title="No availability zones in the local region">Not available</span> },
              { header: 'Security groups', render: () => <span title="CloudForge load balancers have no security groups">Not available</span> },
              { header: 'DNS name', render: (lb) => String(lb.dns_name ?? '—') },
              { header: 'ARN', render: () => <span title="CloudForge load balancers have no ARN">Not available</span> },
              { header: 'Date created', render: (lb) => (typeof lb.created_at === 'string' ? new Date(lb.created_at as string).toLocaleString() : '—') },
              {
                header: 'Actions',
                render: (lb) => (
                  <span className="flex gap-2">
                    <AwsButton
                      variant="secondary"
                      disabled={selectedLb !== String(lb.id) && selectedLb !== ''}
                      onClick={() => {
                        const tg = tgRows[0]
                        if (!tg) {
                          setNotice('Create a target group first, then add a listener to this load balancer.')
                          return
                        }
                        void request(`/api/v1/aws/elb/load-balancers/${String(lb.id)}/listeners`, { method: 'POST', body: { port: 80, protocol: 'HTTP', target_group_id: String(tg.id) } }).then(() => {
                          setNotice(`Listener :80 added, forwarding to target group ${String(tg.name)}.`)
                          refresh()
                        }).catch((e: Error) => setNotice(`Add listener failed: ${e.message}`))
                      }}
                    >
                      Add :80 listener
                    </AwsButton>
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request<Rec>(`/api/v1/aws/elb/load-balancers/${String(lb.id)}/route`, { method: 'POST', body: {} }).then((d) => {
                          const t = d.target as Rec | undefined
                          setRouteResult(`Route for ${String(d.load_balancer)}: ${String(t?.address ?? '—')} (health ${String(t?.health ?? '—')})`)
                        }).catch((e: Error) => setRouteResult(`Route failed: ${e.message}`))
                      }}
                    >
                      Route (preview)
                    </AwsButton>
                    <AwsButton
                      variant="secondary"
                      disabled={selectedLb !== String(lb.id)}
                      onClick={() => {
                        void request(`/api/v1/aws/elb/load-balancers/${String(lb.id)}`, { method: 'DELETE' }).then(() => {
                          setNotice('Load balancer deleted.')
                          setSelectedLb('')
                          refresh()
                        }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                      }}
                    >
                      Delete
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
          <p className="mt-2 text-sm text-aws-muted">Select a load balancer row to enable Delete. &ldquo;Add :80 listener&rdquo; forwards to the first target group; &ldquo;Route (preview)&rdquo; shows the round-robin choice without proxying traffic.</p>
        </AwsCard>
      )}
      <AwsCard title={`Target Groups (${tgRows.length})`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (tgName.trim() !== '') createTg.mutate()
          }}
        >
          <label htmlFor="tg-name" className="text-sm font-medium">Target group name</label>
          <input id="tg-name" value={tgName} onChange={(e) => setTgName(e.target.value)} placeholder="web-tg" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <AwsButton type="submit" disabled={createTg.isPending}>Create target group</AwsButton>
        </form>
        <AwsTable<Rec>
          caption="Target groups"
          emptyMessage="No target groups yet."
          rows={tgRows}
          totalCount={tgRows.length}
          columns={[
            { header: 'Name', render: (tg) => <button type="button" className="text-aws-link underline" onClick={() => setSelectedTg(String(tg.id))}>{String(tg.name)}</button> },
            { header: 'Protocol', render: (tg) => String(tg.protocol ?? '—') },
            { header: 'Port', render: (tg) => String(tg.port ?? '—') },
            { header: 'Health check path', render: (tg) => String(tg.health_check_path ?? '—') },
          ]}
        />
        {selectedTg !== '' && (
          <div className="mt-3">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (targetAddr.trim() !== '') addTarget.mutate()
              }}
            >
              <label htmlFor="tg-target" className="text-sm font-medium">Target address</label>
              <input id="tg-target" value={targetAddr} onChange={(e) => setTargetAddr(e.target.value)} placeholder="http://127.0.0.1:8081/api/v1/health" className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton type="submit" disabled={addTarget.isPending}>Register target</AwsButton>
              <AwsButton variant="secondary" disabled={health.isPending} onClick={() => health.mutate()}>Run health check</AwsButton>
            </form>
            <AwsTable<Rec>
              caption="Targets"
              emptyMessage="No targets registered in this group."
              rows={listOf(tgDetail.data, 'targets')}
              totalCount={listOf(tgDetail.data, 'targets').length}
              columns={[
                { header: 'Target', render: (t) => String(t.address ?? '—') },
                { header: 'Health', render: (t) => String(t.health ?? 'unknown') },
                { header: 'Detail', render: (t) => String(t.health_detail ?? 'not probed yet') },
                {
                  header: 'Actions',
                  render: (t) => (
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/elb/target-groups/${selectedTg}/targets/${String(t.id)}`, { method: 'DELETE' }).then(() => refresh()).catch((e: Error) => setNotice(`Remove target failed: ${e.message}`))
                      }}
                    >
                      Deregister
                    </AwsButton>
                  ),
                },
              ]}
            />
            <p className="mt-2 text-sm text-aws-muted">Targets are http(s) URLs or host:port addresses. Health is probed on demand (HTTP GET / TCP dial, 2 s timeout); a target is eligible for routing only after a probe says healthy.</p>
          </div>
        )}
      </AwsCard>
    </>
  )
}

/**
 * Standalone ELB shell (kept for the /services/elb bookmark, which
 * App.tsx redirects to the EC2 console's Load Balancing section).
 * Home counts come from the same React Query keys as ElbListContent,
 * so the two share one cached fetch.
 */
export function ElbConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const lbs = useQuery({ queryKey: ['elb-lbs'], queryFn: () => request<Rec>('/api/v1/aws/elb/load-balancers'), retry: 1 })
  const tgs = useQuery({ queryKey: ['elb-tgs'], queryFn: () => request<Rec>('/api/v1/aws/elb/target-groups'), retry: 1 })
  const lbRows = listOf(lbs.data, 'load_balancers')
  const tgRows = listOf(tgs.data, 'target_groups')
  return (
    <ConsoleWithHome
      icon="scale"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Networking', to: '/services' }, { label: 'Elastic Load Balancing' }]}
      title="Elastic Load Balancing — Load Balancers"
      description="Application load balancers with listeners, target groups and health checks. In-process round-robin with real probes — no traffic is actually proxied. Per the 2026-11-22 console survey, ELB has no standalone console; it is the EC2 console's Load Balancing section — this page is kept as the old bookmark target."
      fidelity="SIMULATED"
      fidelityFootnote="in-process round-robin · real probes"
      homeLabel="ELB dashboard"
      listLabel="Load balancers"
      initialSection={initialSection}
      home={(goToList) => (
        <ServiceHomeDashboard
          serviceName="Elastic Load Balancing"
          banner="In-process round-robin with real HTTP/TCP health probes. No engine RPC, no nginx reconfiguration; the DNS name is a local teaching name."
          actions={[
            { label: 'Create load balancer', primary: true, onClick: goToList },
            { label: 'Open EC2 console — Load Balancing section', to: '/services/ec2?section=load-balancers' },
            { label: 'Open Auto Scaling (unsupported)', to: '/services/ec2?section=autoscaling' },
          ]}
          cards={[
            { label: 'Load balancers', count: lbRows.length, onSelect: goToList },
            { label: 'Target groups', count: tgRows.length, onSelect: goToList },
            { label: 'Listeners', count: '—', onSelect: goToList, note: 'Per load balancer, on the list page' },
            { label: 'Auto Scaling groups', count: 0, disabledReason: 'Auto Scaling is UNSUPPORTED — no scaling API.' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries()}
          refreshing={lbs.isFetching}
          health={[
            lbs.isError ? 'Load balancers could not be loaded.' : `${lbRows.length} load balancer(s), ${tgRows.length} target group(s).`,
            'SIMULATED: only targets whose last probe said healthy are eligible; unprobed targets are never assumed healthy.',
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
    >
      <ElbListContent />
    </ConsoleWithHome>
  )
}
