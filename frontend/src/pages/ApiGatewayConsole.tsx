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
 * API Gateway console (Phase 3) — mirrors the AWS API Gateway
 * console's APIs / Resources / Stages / Deploy flow, plus an
 * invoke tester and real request logs. Routing is REAL: methods
 * route to Lambda functions or to SSRF-guarded HTTP targets, and
 * every invocation is logged. A deployment snapshots the current
 * resources into a stage. REST/HTTP only (no WebSocket APIs);
 * invoke requires platform auth (no API keys / usage plans).
 */
export function ApiGatewayConsole() {
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

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Serverless', to: '/services' }, { label: 'API Gateway' }]}
        title="API Gateway — APIs"
        description="REST/HTTP APIs routing to Lambda functions and SSRF-guarded HTTP targets, with real request logging."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="real routing · real logs · no WebSocket APIs" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {invokeResult !== '' && <pre className="overflow-auto border border-aws-border bg-white px-4 py-2 font-mono text-sm" role="status">{invokeResult}</pre>}
      {apis.isPending && <Loading label="Loading APIs" />}
      {apis.isError && <ErrorBox message={`APIs could not be loaded (${apis.error.message}).`} />}
      {apis.data && (
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
          <AwsTable<Rec>
            caption="APIs"
            emptyMessage="No APIs yet. Create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'API name', render: (a) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(a.id))}>{String(a.name)}</button> },
              { header: 'Protocol', render: (a) => String(a.protocol ?? '—') },
              { header: 'Status', render: (a) => String(a.status ?? '—') },
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
        <>
          <AwsCard title={`Resources — ${String(sel.name)}`}>
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
            <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-aws-border pt-3">
              <span className="text-sm font-medium">Stages:</span>
              {stages.length === 0 && <span className="text-sm text-aws-muted">none deployed yet</span>}
              {stages.map((s) => (
                <span key={String(s.id)} className="rounded-sm border border-aws-border bg-aws-bg px-2 py-1 text-sm">{String(s.name)} (deployed {String(s.deployed_at ?? '').slice(0, 19)})</span>
              ))}
              <label htmlFor="apigw-stage" className="text-sm font-medium">Deploy stage</label>
              <input id="apigw-stage" value={stageName} onChange={(e) => setStageName(e.target.value)} className="w-24 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton disabled={deploy.isPending} onClick={() => deploy.mutate()}>Deploy</AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="Invoke (test)">
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
          </AwsCard>
          <AwsCard title="Request logs (latest 100)">
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
          </AwsCard>
        </>
      )}
    </div>
  )
}
