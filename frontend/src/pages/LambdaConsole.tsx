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

const DEFAULT_CODE = "import json, sys\n\nevent = json.load(sys.stdin)\nprint(json.dumps({'got': event}))\n"

/**
 * Lambda console (Phase 3) — mirrors the AWS Lambda console's
 * Functions list and detail tabs (Code / Configuration / Triggers /
 * Permissions / Monitoring). Functions REALLY execute here, in a
 * sandboxed subprocess (python3/node): timeout, output cap, prlimit
 * memory cap when available, temp workdir, uid 65534 when the API
 * runs as root. That is NOT container isolation and NOT AWS Lambda —
 * no VPC attachment, managed scaling, layers, or Docker invoker.
 * The console shows the sandbox that actually applied on every
 * invocation. Do not run untrusted code from strangers.
 */
export function LambdaConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [runtime, setRuntime] = useState('python3')
  const [code, setCode] = useState(DEFAULT_CODE)
  const [selected, setSelected] = useState('')
  const [eventJson, setEventJson] = useState('{"ping": "pong"}')
  const [invokeResult, setInvokeResult] = useState<Rec | null>(null)
  const [notice, setNotice] = useState('')

  const functions = useQuery({ queryKey: ['lambda-functions'], queryFn: () => request<Rec>('/api/v1/aws/lambda/functions'), retry: 1 })
  const rows = listOf(functions.data, 'functions')
  const invocations = useQuery({
    queryKey: ['lambda-invocations', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/lambda/functions/${selected}/invocations`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['lambda-functions'] })
    void queryClient.invalidateQueries({ queryKey: ['lambda-invocations', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/lambda/functions', {
      method: 'POST',
      body: { name: name.trim(), runtime, handler: 'handler.handle', code, timeout_seconds: 10, memory_mb: 256 },
    }),
    onSuccess: (d) => {
      setNotice(`Function ${String(d.name)} created (version 1, runtime ${String(d.runtime)}).`)
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create function failed: ${e.message}`),
  })
  const invoke = useMutation({
    mutationFn: () => {
      let event: unknown = {}
      try {
        event = JSON.parse(eventJson) as unknown
      } catch {
        throw new Error('Event is not valid JSON')
      }
      return request<Rec>(`/api/v1/aws/lambda/functions/${selected}/invoke`, { method: 'POST', body: { event } })
    },
    onSuccess: (d) => {
      setInvokeResult(d)
      refresh()
    },
    onError: (e: Error) => setNotice(`Invoke failed: ${e.message}`),
  })

  const sel = rows.find((f) => String(f.id) === selected)

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Compute', to: '/services' }, { label: 'Lambda' }]}
        title="Lambda — Functions"
        description="Serverless functions that really execute here, in a sandboxed subprocess (python3/node). Code is a whole program: event JSON on stdin, result on stdout."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="subprocess sandbox · not containers · not AWS Lambda" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {functions.isPending && <Loading label="Loading functions" />}
      {functions.isError && <ErrorBox message={`Functions could not be loaded (${functions.error.message}).`} />}
      {functions.data && (
        <AwsCard title={`Functions (${rows.length})`}>
          <form
            className="mb-3 flex flex-col gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor="lambda-name" className="text-sm font-medium">Function name</label>
              <input id="lambda-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="echoer" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <label className="text-sm">
                Runtime
                <select value={runtime} onChange={(e) => setRuntime(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="python3">Python 3</option>
                  <option value="python3.12">Python 3.12</option>
                  <option value="python3.11">Python 3.11</option>
                  <option value="nodejs20">Node.js 20</option>
                  <option value="nodejs22">Node.js 22</option>
                </select>
              </label>
              <AwsButton type="submit" disabled={create.isPending}>Create function</AwsButton>
            </div>
            <label className="text-sm">
              Code (whole program — reads the event as JSON on stdin, prints the result on stdout)
              <textarea value={code} onChange={(e) => setCode(e.target.value)} rows={5} className="mt-1 w-full rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
            </label>
          </form>
          <AwsTable<Rec>
            caption="Lambda functions"
            emptyMessage="No functions yet. Create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Function name', render: (f) => <button type="button" className="text-aws-link underline" onClick={() => { setSelected(String(f.id)); setInvokeResult(null) }}>{String(f.name)}</button> },
              { header: 'Runtime', render: (f) => String(f.runtime ?? '—') },
              { header: 'Version', render: (f) => String(f.version ?? '—') },
              { header: 'Timeout', render: (f) => `${String(f.timeout_seconds ?? '—')}s` },
              { header: 'Memory', render: (f) => `${String(f.memory_mb ?? '—')} MB` },
              {
                header: 'Actions',
                render: (f) => (
                  <AwsButton
                    variant="secondary"
                    onClick={() => {
                      void request(`/api/v1/aws/lambda/functions/${String(f.id)}`, { method: 'DELETE' }).then(() => {
                        setNotice('Function deleted.')
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
          <p className="mt-2 text-sm text-aws-muted">Supported runtimes: python3 / nodejs only. A Docker invoker does not exist — the old registry claim has been corrected. No VPC attachment, managed scaling, or layers.</p>
        </AwsCard>
      )}
      {sel && (
        <>
          <AwsCard title={`Code & Configuration — ${String(sel.name)}`}>
            <dl className="mb-3 grid grid-cols-2 gap-2 text-sm">
              <dt className="font-medium">Runtime</dt><dd>{String(sel.runtime ?? '—')}</dd>
              <dt className="font-medium">Handler</dt><dd>{String(sel.handler ?? '—')}</dd>
              <dt className="font-medium">Version</dt><dd>{String(sel.version ?? '—')}</dd>
              <dt className="font-medium">Timeout / Memory</dt><dd>{String(sel.timeout_seconds ?? '—')}s / {String(sel.memory_mb ?? '—')} MB</dd>
              <dt className="font-medium">Aliases</dt><dd>{Object.keys((sel.aliases as Rec | undefined) ?? {}).join(', ') || 'none'}</dd>
              <dt className="font-medium">Triggers</dt><dd>{Array.isArray(sel.triggers) ? `${(sel.triggers as unknown[]).length} configured` : 'none'}</dd>
            </dl>
            <pre className="overflow-auto rounded-sm border border-aws-border bg-aws-bg p-3 font-mono text-xs">{String(sel.code ?? '')}</pre>
            <p className="mt-2 text-sm text-aws-muted">
              Permissions tab: functions run with no IAM role and no AWS credentials — there is no execution role model here.
              Triggers: add an SQS or EventBridge trigger from the function detail via the API
              (<code>POST /aws/lambda/functions/{String(sel.id)}/triggers</code>); API Gateway integrations are configured on the API.
            </p>
          </AwsCard>
          <AwsCard title="Test / Invoke">
            <form
              className="mb-3 flex flex-wrap items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                invoke.mutate()
              }}
            >
              <label className="text-sm">
                Event JSON
                <textarea value={eventJson} onChange={(e) => setEventJson(e.target.value)} rows={2} className="ml-2 w-96 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
              </label>
              <AwsButton type="submit" disabled={invoke.isPending}>Invoke</AwsButton>
            </form>
            {invokeResult && (
              <div className="text-sm">
                <p>Status: <strong>{String(invokeResult.status ?? '—')}</strong> · Duration: {String(invokeResult.duration_ms ?? '—')} ms{invokeResult.timed_out === true ? ' · TIMED OUT' : ''}</p>
                <p className="mt-1">Sandbox: <code>{String(invokeResult.sandbox ?? '')}</code></p>
                {typeof invokeResult.error === 'string' && invokeResult.error !== '' && <p className="mt-1 text-red-700">Error: {String(invokeResult.error)}</p>}
                <pre className="mt-2 overflow-auto rounded-sm border border-aws-border bg-aws-bg p-3 font-mono text-xs">{String(invokeResult.output ?? '')}</pre>
              </div>
            )}
          </AwsCard>
          <AwsCard title="Monitoring — Invocations (latest 50)">
            {invocations.isPending && <Loading label="Loading invocations" />}
            {invocations.data && (
              <AwsTable<Rec>
                caption="Invocations"
                emptyMessage="No invocations yet."
                rows={listOf(invocations.data, 'invocations')}
                totalCount={listOf(invocations.data, 'invocations').length}
                columns={[
                  { header: 'Time', render: (i) => String(i.created_at ?? '—') },
                  { header: 'Status', render: (i) => String(i.status ?? '—') },
                  { header: 'Duration (ms)', render: (i) => String(i.duration_ms ?? '—') },
                  { header: 'Output', render: (i) => <code className="text-xs">{String(i.output ?? '').slice(0, 120)}</code> },
                  { header: 'Error', render: (i) => String(i.error ?? '') },
                ]}
              />
            )}
          </AwsCard>
        </>
      )}
    </div>
  )
}
