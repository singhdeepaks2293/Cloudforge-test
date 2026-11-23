import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { AwsActionsMenu, AwsPreferencesButton, AwsRefreshButton, AwsToolbar } from '../components/AwsShell.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * API Gateway console (Phase 3) — mirrors the AWS API Gateway
 * console's APIs / Resources / Stages / Deploy flow, plus an
 * invoke tester and real request logs. Routing is REAL: methods
 * route to Lambda functions or to SSRF-guarded HTTP targets, and
 * every invocation is logged. A deployment snapshots the current
 * resources into a stage. REST/HTTP only (no WebSocket APIs);
 * invoke requires platform auth (no API keys / usage plans).
 */
export function ApiGatewayConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [selected, setSelected] = useState('')
  const [resPath, setResPath] = useState('/echo')
  const [method, setMethod] = useState('POST')
  const [integrationType, setIntegrationType] = useState('lambda')
  const [integrationTarget, setIntegrationTarget] = useState('')
  const [stageName, setStageName] = useState('prod')
  const [invokePath, setInvokePath] = useState('/echo')
  const [invokeMethod, setInvokeMethod] = useState('POST')
  const [invokeBody, setInvokeBody] = useState('{"ping": "via-gateway"}')
  const [invokeResult, setInvokeResult] = useState('')
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'apis')
  const goToList = () => setSection('apis')
  const [filter, setFilter] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [apiType, setApiType] = useState<'REST' | 'HTTP' | 'WEBSOCKET'>('REST')
  const [detailTab, setDetailTab] = useState<'resources' | 'stages' | 'invoke' | 'logs'>('resources')

  const apis = useQuery({ queryKey: ['apigw-apis'], queryFn: () => request<Rec>('/api/v1/aws/apigateway/apis'), retry: 1 })
  const rows = listOf(apis.data, 'apis')
  const detail = useQuery({
    queryKey: ['apigw-api', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/apigateway/apis/${selected}`),
    enabled: selected !== '',
    retry: 1,
  })
  const logs = useQuery({
    queryKey: ['apigw-logs', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/apigateway/apis/${selected}/logs`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['apigw-apis'] })
    void queryClient.invalidateQueries({ queryKey: ['apigw-api', selected] })
    void queryClient.invalidateQueries({ queryKey: ['apigw-logs', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/apigateway/apis', { method: 'POST', body: { name: name.trim(), protocol: 'REST' } }),
    onSuccess: () => {
      setNotice('API created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create API failed: ${e.message}`),
  })
  const addResource = useMutation({
    mutationFn: () => {
      const integration: Record<string, unknown> = { type: integrationType }
      if (integrationType === 'lambda') integration.function_id = integrationTarget.trim()
      else integration.url = integrationTarget.trim()
      return request<Rec>(`/api/v1/aws/apigateway/apis/${selected}/resources`, {
        method: 'POST',
        body: { path: resPath.trim(), methods: [{ method, integration }] },
      })
    },
    onSuccess: () => {
      setNotice(`Resource ${resPath.trim()} ${method} added (deploy a stage to publish it).`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Add resource failed: ${e.message}`),
  })
  const deploy = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/apigateway/apis/${selected}/deploy`, { method: 'POST', body: { stage_name: stageName.trim() } }),
    onSuccess: () => {
      setNotice(`Stage ${stageName.trim()} deployed (snapshot of current resources).`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Deploy failed: ${e.message}`),
  })

  const sel = rows.find((a) => String(a.id) === selected)
  const resources = listOf(detail.data, 'resources')
  const stages = listOf(detail.data, 'stages')
  const checkedApi = rows.find((a) => String(a.id) === checked[0])
  const filteredRows = rows.filter(
    (a) =>
      filter.trim() === '' ||
      String(a.name ?? '').toLowerCase().includes(filter.toLowerCase()) ||
      String(a.id).toLowerCase().includes(filter.toLowerCase()),
  )

  return (
    <ServiceConsole
      icon="zap"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Serverless', to: '/services' }, { label: 'API Gateway' }]}
      title="API Gateway — APIs"
      description="REST/HTTP APIs routing to Lambda functions and SSRF-guarded HTTP targets, with real request logging."
      fidelity="PARTIAL"
      fidelityFootnote="real routing · real logs · no WebSocket APIs"
      subNav={[
        { id: 'home', label: 'API Gateway dashboard' },
        { id: 'apis', label: 'APIs' },
        { id: 'custom-domains', label: 'Custom domain names' },
        { id: 'domain-access', label: 'Domain name access associations' },
        { id: 'vpc-links', label: 'VPC links' },
        { id: 'agentcore', label: 'AgentCore targets' },
        { id: 'usage-plans', label: 'Usage plans' },
        { id: 'api-keys', label: 'API keys' },
        { id: 'client-certs', label: 'Client certificates' },
        { id: 'settings', label: 'Settings' },
        { id: 'portal-inventory', label: 'API inventory', group: 'API Portal' },
        { id: 'portal-products', label: 'Products', group: 'API Portal' },
        { id: 'portal-docs', label: 'Documentation portals', group: 'API Portal' },
        { id: 'portal-org', label: 'Organization settings', group: 'API Portal' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="API Gateway"
            banner="Real routing to Lambda functions and SSRF-guarded HTTP targets with real request logging; a deployment snapshots resources into a stage. REST/HTTP only (no WebSocket APIs)."
            actions={[
              { label: 'Create API', primary: true, onClick: goToList },
            { label: 'View Lambda functions', to: '/services/lambda' },
            ]}
            cards={[
              { label: 'APIs', count: rows.length, onSelect: goToList },
            { label: 'Resources', count: selected !== '' ? resources.length : '—', onSelect: goToList },
            { label: 'Stages', count: selected !== '' ? stages.length : '—', onSelect: goToList, note: 'A deployment snapshots resources into a stage' },
            { label: 'WebSocket APIs', count: 0, disabledReason: 'REST/HTTP only — no WebSocket APIs.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={apis.isFetching}
            health={[
              apis.isError ? 'APIs could not be loaded.' : `${rows.length} API(s) listed.`,
            "Invoke requires platform auth (no API keys / usage plans).",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — serverless track', to: '/learn' },
            { label: 'Lambda (integration target)', to: '/services/lambda' },
            ]}
          />
      )}
      {section === 'custom-domains' && (
        <UnsupportedSection
          title="Custom domain names"
          reason="Custom domain names are UNSUPPORTED in CloudForge — there is no custom-domain API and no TLS certificate surface."
          actionLabel="Create custom domain name"
        />
      )}
      {section === 'vpc-links' && (
        <UnsupportedSection
          title="VPC links"
          reason="VPC links are UNSUPPORTED in CloudForge — there is no VPC-link API."
          actionLabel="Create VPC link"
        />
      )}
      {section === 'usage-plans' && (
        <UnsupportedSection
          title="Usage plans"
          reason="Usage plans are UNSUPPORTED in CloudForge — invoke requires platform auth and there are no API keys or usage plans."
          actionLabel="Create usage plan"
        />
      )}
      {section === 'api-keys' && (
        <UnsupportedSection
          title="API keys"
          reason="API keys are UNSUPPORTED in CloudForge — invoke requires platform auth and there is no API-key API."
          actionLabel="Create API key"
        />
      )}
      {section === 'apis' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {invokeResult !== '' && <pre className="overflow-auto border border-aws-border bg-white px-4 py-2 font-mono text-sm" role="status">{invokeResult}</pre>}
      {apis.isPending && <Loading label="Loading APIs" />}
      {apis.isError && <ErrorBox message={`APIs could not be loaded (${apis.error.message}).`} />}
      <ConsoleTabs
        tabs={[
          { id: 'REST', label: 'REST APIs' },
          { id: 'HTTP', label: 'HTTP APIs' },
          { id: 'WEBSOCKET', label: 'WebSocket APIs' },
        ]}
        active={apiType}
        onChange={(id) => setApiType(id as typeof apiType)}
      />
      {apiType === 'HTTP' && (
        <UnsupportedSection
          title="HTTP APIs"
          reason="HTTP APIs are UNSUPPORTED in CloudForge — the create form only creates REST APIs; no HTTP API type can be created despite the console description saying REST/HTTP."
          actionLabel="Create HTTP API"
        />
      )}
      {apiType === 'WEBSOCKET' && (
        <UnsupportedSection
          title="WebSocket APIs"
          reason="WebSocket APIs are UNSUPPORTED in CloudForge — there is no WebSocket API backend."
          actionLabel="Create WebSocket API"
        />
      )}
      {apiType === 'REST' && apis.data && (
        <AwsCard title={`APIs (${rows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="apigw-name" className="text-sm font-medium">API name</label>
            <input id="apigw-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="echo-api" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={create.isPending}>Create API</AwsButton>
          </form>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={apis.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          >
            <AwsActionsMenu
              items={[
                {
                  label: 'Delete',
                  disabledReason: checkedApi === undefined ? 'Select an API first.' : undefined,
                  onClick: () => {
                    void request(`/api/v1/aws/apigateway/apis/${String(checkedApi?.id)}`, { method: 'DELETE' }).then(() => {
                      setNotice('API deleted.')
                      setSelected('')
                      setChecked([])
                      refresh()
                    }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                  },
                },
              ]}
            />
          </AwsToolbar>
          <AwsTable<Rec>
            caption="APIs"
            emptyMessage="No APIs yet. Create one above."
            rows={filteredRows}
            totalCount={rows.length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter APIs by name"
            selectable
            selectedIds={checked}
            onToggleRow={(id) => {
              setSelected(id)
              setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))
            }}
            onToggleAll={(all) => setChecked(all ? filteredRows.map((r) => r.id) : [])}
            columns={[
              // APIs columns in the v4 survey's exact order (8).
              // Description, Endpoint type and Security policy have
              // no backend value and render '—'; ID is the API id.
              { header: 'Name', render: (a) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(a.id))}>{String(a.name)}</button> },
              { header: 'Description', render: () => '—' },
              { header: 'ID', render: (a) => <code className="text-xs">{String(a.id)}</code> },
              { header: 'Protocol', render: (a) => String(a.protocol ?? '—') },
              { header: 'Endpoint type', render: () => '—' },
              { header: 'Created', render: (a) => (typeof a.created_at === 'string' ? new Date(a.created_at as string).toLocaleString() : '—') },
              { header: 'Security policy', render: () => '—' },
              { header: 'API status', render: (a) => String(a.status ?? '—') },
              {
                header: 'Actions',
                render: (a) => (
                  <AwsButton
                    variant="secondary"
                    onClick={() => {
                      void request(`/api/v1/aws/apigateway/apis/${String(a.id)}`, { method: 'DELETE' }).then(() => {
                        setNotice('API deleted.')
                        setSelected('')
                        refresh()
                      }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                    }}
                  >
                    Delete
                  </AwsButton>
                ),
              },
            ]}
          />
        </AwsCard>
      )}
      {sel && (
        <AwsCard title={`API detail — ${String(sel.name)}`}>
          <ConsoleTabs
            tabs={[
              { id: 'resources', label: 'Resources' },
              { id: 'stages', label: 'Stages' },
              { id: 'invoke', label: 'Invoke' },
              { id: 'logs', label: 'Logs' },
            ]}
            active={detailTab}
            onChange={(id) => setDetailTab(id as typeof detailTab)}
          />
          <div className="mt-3">
          {detailTab === 'resources' && (
          <>
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (resPath.trim() !== '' && integrationTarget.trim() !== '') addResource.mutate()
              }}
            >
              <label htmlFor="apigw-path" className="text-sm font-medium">Path</label>
              <input id="apigw-path" value={resPath} onChange={(e) => setResPath(e.target.value)} className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <label className="text-sm">
                Method
                <select value={method} onChange={(e) => setMethod(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="GET">GET</option>
                  <option value="POST">POST</option>
                  <option value="PUT">PUT</option>
                  <option value="DELETE">DELETE</option>
                  <option value="PATCH">PATCH</option>
                </select>
              </label>
              <label className="text-sm">
                Integration
                <select value={integrationType} onChange={(e) => setIntegrationType(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="lambda">Lambda</option>
                  <option value="http">HTTP</option>
                </select>
              </label>
              <label htmlFor="apigw-target" className="text-sm font-medium">Function id / URL</label>
              <input id="apigw-target" value={integrationTarget} onChange={(e) => setIntegrationTarget(e.target.value)} placeholder={integrationType === 'lambda' ? 'function id' : 'http://localhost:8081/api/v1/health'} className="w-80 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
              <AwsButton type="submit" disabled={addResource.isPending}>Add resource</AwsButton>
            </form>
            <AwsTable<Rec>
              caption="Resources"
              emptyMessage="No resources yet. Add one above, then deploy a stage."
              rows={resources}
              totalCount={resources.length}
              columns={[
                { header: 'Path', render: (r) => String(r.path) },
                {
                  header: 'Methods',
                  render: (r) => (Array.isArray(r.methods) ? (r.methods as Rec[]).map((m) => `${String(m.method)} → ${String((m.integration as Rec)?.type ?? '')}`).join(', ') : '—'),
                },
              ]}
            />
          </>
          )}
          {detailTab === 'stages' && (
          <>
            <p className="mb-3 text-sm text-aws-muted">A deployment snapshots the current resources into a stage.</p>
            {stages.length === 0 ? (
              <p className="mb-3 text-sm text-aws-muted">No stages deployed yet.</p>
            ) : (
              <ul className="mb-3 space-y-1 text-sm">
                {stages.map((s) => (
                  <li key={String(s.id)}>{String(s.name)} (deployed {String(s.deployed_at ?? '').slice(0, 19)})</li>
                ))}
              </ul>
            )}
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor="apigw-stage" className="text-sm font-medium">Deploy stage</label>
              <input id="apigw-stage" value={stageName} onChange={(e) => setStageName(e.target.value)} className="w-24 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton disabled={deploy.isPending} onClick={() => deploy.mutate()}>Deploy</AwsButton>
            </div>
          </>
          )}
          {detailTab === 'invoke' && (
          <>
            <form
              className="mb-3 flex flex-wrap items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                let body: unknown
                try {
                  body = invokeBody.trim() === '' ? undefined : (JSON.parse(invokeBody) as unknown)
                } catch {
                  setInvokeResult('Body is not valid JSON.')
                  return
                }
                const url = `/api/v1/gateway/${selected}/${encodeURIComponent(stageName)}${invokePath.startsWith('/') ? invokePath : `/${invokePath}`}`
                void request<Rec>(url, { method: invokeMethod, body }).then((d) => {
                  setInvokeResult(JSON.stringify(d, null, 2))
                  refresh()
                }).catch((err: Error) => setInvokeResult(`Invoke failed: ${err.message}`))
              }}
            >
              <label className="text-sm">
                Stage
                <input value={stageName} onChange={(e) => setStageName(e.target.value)} className="ml-2 w-24 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
              </label>
              <label className="text-sm">
                Method
                <select value={invokeMethod} onChange={(e) => setInvokeMethod(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="GET">GET</option>
                  <option value="POST">POST</option>
                  <option value="PUT">PUT</option>
                  <option value="DELETE">DELETE</option>
                </select>
              </label>
              <label className="text-sm">
                Path
                <input value={invokePath} onChange={(e) => setInvokePath(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
              </label>
              <label className="text-sm">
                Body JSON
                <textarea value={invokeBody} onChange={(e) => setInvokeBody(e.target.value)} rows={2} className="ml-2 w-72 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
              </label>
              <AwsButton type="submit">Invoke</AwsButton>
            </form>
            <p className="text-sm text-aws-muted">Invoke requires platform auth (the console sends your token). Undeployed stages and unmapped methods return 404 — never a fake success.</p>
          </>
          )}
          {detailTab === 'logs' && (
          <>
            {logs.isPending && <Loading label="Loading logs" />}
            {logs.data && (
              <AwsTable<Rec>
                caption="Gateway request logs"
                emptyMessage="No invocations logged yet."
                rows={listOf(logs.data, 'logs')}
                totalCount={listOf(logs.data, 'logs').length}
                columns={[
                  { header: 'Time', render: (l) => String(l.created_at ?? '—') },
                  { header: 'Stage', render: (l) => String(l.stage ?? '—') },
                  { header: 'Method', render: (l) => String(l.method ?? '—') },
                  { header: 'Path', render: (l) => String(l.path ?? '—') },
                  { header: 'Status', render: (l) => String(l.status ?? '—') },
                  { header: 'Duration (ms)', render: (l) => String(l.duration_ms ?? '—') },
                ]}
              />
            )}
            <p className="mt-2 text-sm text-aws-muted">Request logs (latest 100).</p>
          </>
          )}
          </div>
        </AwsCard>
      )}
        </>
      )}
      {section === 'domain-access' && (
        <UnsupportedSection
          title="Domain name access associations"
          reason="Domain name access associations are UNSUPPORTED in CloudForge — there is no domain-name API."
          actionLabel="Create association"
        />
      )}
      {section === 'agentcore' && (
        <UnsupportedSection
          title="AgentCore targets"
          reason="AgentCore targets are UNSUPPORTED in CloudForge — there is no AgentCore API and no agent runtime."
          actionLabel="Create AgentCore target"
        />
      )}
      {section === 'client-certs' && (
        <UnsupportedSection
          title="Client certificates"
          reason="Client certificates are UNSUPPORTED in CloudForge — there is no client-certificate API; invoke requires platform auth instead."
          actionLabel="Generate client certificate"
        />
      )}
      {section === 'settings' && (
        <UnsupportedSection
          title="Settings"
          reason="API Gateway settings are UNSUPPORTED in CloudForge — no settings are stored."
          actionLabel="Save settings"
        />
      )}
      {section === 'portal-inventory' && (
        <UnsupportedSection
          title="API inventory — API Portal"
          reason="The API Portal inventory is UNSUPPORTED in CloudForge — there is no portal API."
          actionLabel="Open API Portal"
        />
      )}
      {section === 'portal-products' && (
        <UnsupportedSection
          title="Products — API Portal"
          reason="API Portal products are UNSUPPORTED in CloudForge — there is no portal API."
          actionLabel="Create product"
        />
      )}
      {section === 'portal-docs' && (
        <UnsupportedSection
          title="Documentation portals — API Portal"
          reason="API Portal documentation portals are UNSUPPORTED in CloudForge — there is no portal API."
          actionLabel="Create documentation portal"
        />
      )}
      {section === 'portal-org' && (
        <UnsupportedSection
          title="Organization settings — API Portal"
          reason="API Portal organization settings are UNSUPPORTED in CloudForge — there is no portal API."
          actionLabel="Save organization settings"
        />
      )}
    </ServiceConsole>
  )
}
