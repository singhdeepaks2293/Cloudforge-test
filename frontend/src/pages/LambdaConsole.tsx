import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import {
  AwsActionsMenu,
  AwsCopy,
  AwsFlash,
  AwsPreferencesButton,
  AwsRefreshButton,
  AwsStat,
  AwsToolbar,
  AwsWizard,
} from '../components/AwsShell.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

const DEFAULT_CODE = "import json, sys\n\nevent = json.load(sys.stdin)\nprint(json.dumps({'got': event}))\n"
const NODE_CODE = "const event = JSON.parse(require('fs').readFileSync(0, 'utf-8'));\nconsole.log(JSON.stringify({ got: event }));\n"
// Wizard steps from the v4 survey (2026-11-22), which opened the Create
// Event Function wizard and left via Cancel: author choice, function
// name + runtime, configuration (toggles + additional settings), summary.
const CREATE_STEPS = ['Author', 'Name and runtime', 'Configuration', 'Summary']

/**
 * Lambda console — mirrors the AWS Lambda console: service sub-nav,
 * Functions list with checkboxes and a toolbar, a multi-step Create
 * function wizard, and function detail tabs Code / Test /
 * Configuration / Permissions / Monitoring / Triggers.
 *
 * Functions REALLY execute here, in a sandboxed subprocess
 * (python3/node): timeout, output cap, prlimit memory cap when
 * available, temp workdir, uid 65534 when the API runs as root. That
 * is NOT container isolation and NOT AWS Lambda — no VPC attachment,
 * managed scaling, layers, or Docker invoker. The console shows the
 * sandbox that actually applied on every invocation.
 */
export function LambdaConsole({ initialSection }: { initialSection?: string }) {
  const queryClient = useQueryClient()
  const [activeSub, setActiveSub] = useState(initialSection ?? 'functions')
  const [name, setName] = useState('')
  // Runtime default is Node.js 22.x, the newest the backend supports
  // (SupportedRuntimes in backend/internal/phase3/phase3.go). Node.js
  // 26.x is the AWS console default in the v4 survey, but the backend
  // rejects it with 400 unsupported_runtime, so it is listed in the
  // wizard as a disabled option with that reason instead of a default
  // that would make every create fail.
  const [runtime, setRuntime] = useState('nodejs22')
  const [author, setAuthor] = useState<'scratch' | 'blueprint' | 'container'>('scratch')
  const [blueprint, setBlueprint] = useState<'echo' | 'hello'>('echo')
  const [durable] = useState(false)
  const [ec2Capacity] = useState(false)
  const [code, setCode] = useState(DEFAULT_CODE)
  const [selected, setSelected] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [tab, setTab] = useState('code')
  const [eventJson, setEventJson] = useState('{"ping": "pong"}')
  const [invokeResult, setInvokeResult] = useState<Rec | null>(null)
  const [notice, setNotice] = useState('')
  const [noticeTone, setNoticeTone] = useState<'success' | 'info' | 'error'>('info')
  const [filter, setFilter] = useState('')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)

  const functions = useQuery({ queryKey: ['lambda-functions'], queryFn: () => request<Rec>('/api/v1/aws/lambda/functions'), retry: 1 })
  const rows = listOf(functions.data, 'functions').filter(
    (r) => filter.trim() === '' || String(r.name ?? '').toLowerCase().includes(filter.toLowerCase()) || r.id.toLowerCase().includes(filter.toLowerCase()),
  )
  const invocations = useQuery({
    queryKey: ['lambda-invocations', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/lambda/functions/${selected}/invocations`),
    enabled: selected !== '',
    retry: 1,
  })
  const flash = (message: string, tone: 'success' | 'info' | 'error' = 'info') => {
    setNotice(message)
    setNoticeTone(tone)
  }
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
      flash(`Function ${String(d.name)} created (version 1, runtime ${String(d.runtime)}).`, 'success')
      setName('')
      setWizardOpen(false)
      setWizardStep(0)
      setSelected(String(d.id))
      setTab('code')
      refresh()
    },
    onError: (e: Error) => flash(`Create function failed: ${e.message}`, 'error'),
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
    onError: (e: Error) => flash(`Invoke failed: ${e.message}`, 'error'),
  })
  const del = useMutation({
    mutationFn: (id: string) => request(`/api/v1/aws/lambda/functions/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      flash('Function deleted.', 'success')
      setSelected('')
      setChecked([])
      refresh()
    },
    onError: (e: Error) => flash(`Delete failed: ${e.message}`, 'error'),
  })

  const sel = rows.find((f) => String(f.id) === selected)
  const functionArn = sel ? `arn:cloudforge:lambda:cf-local-1:local:function:${String(sel.name)}` : ''

  return (
    <ServiceConsole
      icon="zap"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Compute', to: '/services' }, { label: 'Lambda' }]}
      title="Lambda — Functions"
      description="Serverless functions that really execute here, in a sandboxed subprocess (python3/node). Code is a whole program: event JSON on stdin, result on stdout."
      fidelity="PARTIAL"
      fidelityFootnote="subprocess sandbox · not containers · not AWS Lambda"
      subNav={[
        { id: 'home', label: 'Lambda dashboard' },
        { id: 'functions', label: 'Event Functions' },
        { id: 'web-functions', label: 'Web Functions' },
        { id: 'layers', label: 'Layers', disabledReason: 'Not modelled' },
        { id: 'applications', label: 'Applications' },
        { id: 'test-events', label: 'Test events' },
        { id: 'event-sources', label: 'Event source mappings', disabledReason: 'Triggers are per-function via the API' },
        { id: 'code-signing', label: 'Code signing configurations' },
        { id: 'settings', label: 'Settings' },
      ]}
      activeSub={activeSub}
      onSubChange={setActiveSub}
      actions={
        <AwsButton
          variant="primary"
          onClick={() => {
            setWizardOpen(true)
            setWizardStep(0)
            setActiveSub('functions')
          }}
        >
          Create function
        </AwsButton>
      }
    >
      {notice !== '' && <AwsFlash tone={noticeTone}>{notice}</AwsFlash>}

      {activeSub === 'home' && (
        <ServiceHomeDashboard
          serviceName="Lambda"
          banner="Functions really execute here, in a sandboxed subprocess (python3/node) — not container isolation and not AWS Lambda. No VPC attachment, managed scaling, layers, or provisioned concurrency."
          actions={[
            {
              label: 'Create function',
              primary: true,
              onClick: () => {
                setWizardOpen(true)
                setWizardStep(0)
                setActiveSub('functions')
              },
            },
            { label: 'View SQS queues (trigger source)', to: '/services/sqs' },
          ]}
          cards={[
            { label: 'Event Functions', count: rows.length, onSelect: () => setActiveSub('functions') },
            { label: 'Layers', count: 0, disabledReason: 'Not modelled.' },
            { label: 'Event source mappings', count: '—', note: 'Triggers are per-function via the API' },
            {
              label: 'Invocations (selected function)',
              count: selected !== '' ? listOf(invocations.data, 'invocations').length : '—',
              onSelect: () => setActiveSub('functions'),
              note: 'Real subprocess output, with the sandbox that applied',
            },
          ]}
          onRefresh={refresh}
          refreshing={functions.isFetching}
          health={[
            functions.isError ? 'Functions could not be loaded.' : `${rows.length} function(s) listed.`,
            'Execution: sandboxed subprocess (timeout, output cap, prlimit memory cap when available, uid 65534 when the API runs as root).',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — serverless track', to: '/learn' },
            { label: 'API Gateway (HTTP trigger)', to: '/services/apigateway' },
          ]}
        />
      )}

      {activeSub === 'functions' && (
        <div className="space-y-4">
          {wizardOpen && (
            <AwsWizard
              title="Create function"
              steps={CREATE_STEPS}
              current={wizardStep}
              onStep={setWizardStep}
              onCancel={() => setWizardOpen(false)}
              onSubmit={() => {
                if (name.trim() === '' || name.trim().length > 64) {
                  flash('Function name must be 1–64 characters.', 'error')
                  setWizardStep(1)
                  return
                }
                create.mutate()
              }}
              submitLabel="Create function"
              busy={create.isPending}
            >
              {wizardStep === 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold">Author</h3>
                  <label className="flex items-start gap-2 text-sm">
                    <input type="radio" name="lambda-author" checked={author === 'scratch'} onChange={() => setAuthor('scratch')} className="mt-1" />
                    <span><strong>Author from scratch</strong> — write the code in the next step.</span>
                  </label>
                  <label className="flex items-start gap-2 text-sm">
                    <input
                      type="radio"
                      name="lambda-author"
                      checked={author === 'blueprint'}
                      onChange={() => {
                        setAuthor('blueprint')
                        if (blueprint === 'echo') { setCode(DEFAULT_CODE); setRuntime('python3') }
                        else { setCode(NODE_CODE); setRuntime('nodejs22') }
                      }}
                      className="mt-1"
                    />
                    <span><strong>Use a blueprint</strong> — a built-in starting template (CloudForge has no AWS blueprint library).</span>
                  </label>
                  {author === 'blueprint' && (
                    <label className="block text-sm">
                      <span className="mb-1 block font-medium">Blueprint</span>
                      <select
                        value={blueprint}
                        onChange={(e) => {
                          const next = e.target.value as 'echo' | 'hello'
                          setBlueprint(next)
                          if (next === 'echo') { setCode(DEFAULT_CODE); setRuntime('python3') }
                          else { setCode(NODE_CODE); setRuntime('nodejs22') }
                        }}
                        className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
                      >
                        <option value="echo">Echo event (Python) — reads the event JSON and prints it back</option>
                        <option value="hello">Hello world (Node.js) — reads the event JSON and prints it back</option>
                      </select>
                    </label>
                  )}
                  <label className="flex items-start gap-2 text-sm opacity-60">
                    <input type="radio" name="lambda-author" checked={author === 'container'} disabled className="mt-1" />
                    <span><strong>Container image</strong> — not supported: CloudForge has no Docker invoker and no container-image registry for functions.</span>
                  </label>
                </div>
              )}
              {wizardStep === 1 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold">Function name and runtime</h3>
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">Function name (1–64 characters)</span>
                    <input
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="echoer"
                      maxLength={64}
                      className="w-full max-w-md rounded-sm border border-aws-border-dark px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
                    />
                  </label>
                  {name.trim() !== '' && name.trim().length > 64 && (
                    <p className="text-sm text-aws-error" role="alert">Function names are at most 64 characters.</p>
                  )}
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">Runtime</span>
                    <select
                      value={runtime}
                      onChange={(e) => {
                        const next = e.target.value
                        setRuntime(next)
                        if (next.startsWith('node') && code === DEFAULT_CODE) setCode(NODE_CODE)
                        if (next.startsWith('python') && code === NODE_CODE) setCode(DEFAULT_CODE)
                      }}
                      className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
                    >
                      <option value="nodejs26" disabled>Node.js 26.x (AWS console default — not supported in CloudForge; the backend supports up to Node.js 22.x)</option>
                      <option value="nodejs22">Node.js 22.x</option>
                      <option value="nodejs20">Node.js 20.x</option>
                      <option value="node">Node.js (node)</option>
                      <option value="python3.12">Python 3.12</option>
                      <option value="python3.11">Python 3.11</option>
                      <option value="python3">Python 3</option>
                    </select>
                  </label>
                  <p className="text-sm text-aws-muted">
                    Node.js 26.x is the AWS console default in the v4 survey, but CloudForge&apos;s backend rejects
                    it (400 unsupported_runtime) — the newest supported Node.js runtime is 22.x, so that is the
                    default here. Python and Node.js run as sandboxed subprocesses, not containers.
                  </p>
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">Code (whole program — reads the event as JSON on stdin, prints the result on stdout)</span>
                    <textarea value={code} onChange={(e) => setCode(e.target.value)} rows={8} className="w-full rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
                  </label>
                </div>
              )}
              {wizardStep === 2 && (
                <div className="space-y-3 text-sm">
                  <h3 className="text-sm font-semibold">Configuration</h3>
                  <label className="flex items-start gap-2 opacity-60">
                    <input type="checkbox" checked={durable} disabled className="mt-1" />
                    <span><strong>Durable execution</strong> — not available in CloudForge: a function runs once in a subprocess and does not resume after a failure.</span>
                  </label>
                  <label className="flex items-start gap-2 opacity-60">
                    <input type="checkbox" checked={ec2Capacity} disabled className="mt-1" />
                    <span><strong>EC2 capacity provider</strong> — not available in CloudForge: there is no capacity-provider backend and no EC2 capacity to draw on.</span>
                  </label>
                  <details className="rounded-sm border border-aws-border p-3">
                    <summary className="cursor-pointer font-medium">Additional settings</summary>
                    <dl className="mt-2 space-y-1 text-aws-muted">
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">Architecture</dt><dd>x86_64 (fixed — no arm64 in the subprocess sandbox)</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">Execution role</dt><dd>None — functions run with no IAM role and no AWS credentials</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">HTTP(S) endpoints</dt><dd>None — invoke from this console or the API only</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">Tenant isolation</dt><dd>Not modelled</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">VPC</dt><dd>None — no VPC attachment in CloudForge</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">Code signing</dt><dd>None — functions run unsigned in the sandbox</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">KMS key</dt><dd>None — no encryption-key setting for function code</dd></div>
                      <div className="flex gap-2"><dt className="w-40 shrink-0 font-medium text-aws-text">Tags</dt><dd>Not stored — the teaching Lambda API has no tag field</dd></div>
                    </dl>
                  </details>
                </div>
              )}
              {wizardStep === 3 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Summary</h3>
                  <dl className="divide-y divide-aws-border border-y border-aws-border">
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Function name</dt><dd>{name.trim() || '—'}</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Author</dt><dd>{author === 'scratch' ? 'Author from scratch' : author === 'blueprint' ? `Blueprint — ${blueprint === 'echo' ? 'Echo event (Python)' : 'Hello world (Node.js)'}` : 'Container image'}</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Runtime</dt><dd>{runtime}</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Durable execution</dt><dd>Off (not available)</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">EC2 capacity provider</dt><dd>Off (not available)</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Timeout / memory</dt><dd>10s / 256 MB</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Sandbox</dt><dd>Subprocess with timeout, output cap and prlimit — not containers</dd></div>
                  </dl>
                  <p className="text-aws-muted">Creating a function stores the code and runs it only when you invoke it.</p>
                </div>
              )}
            </AwsWizard>
          )}
          <p className="text-xs text-aws-muted">
            Wizard steps follow the v4 survey (2026-11-22), which opened the Create Event Function wizard and left
            via Cancel: author from scratch / blueprint / container image, function name, runtime (Node.js 26.x
            default in AWS — CloudForge&apos;s newest supported runtime is Node.js 22.x), Durable execution and EC2
            capacity provider toggles, collapsed Additional settings.
          </p>

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <AwsStat label="Functions" value={rows.length} />
            <AwsStat label="Selected" value={checked.length} />
            <AwsStat label="Runtimes" value="python3 / node" hint="No Docker invoker" />
            <AwsStat label="Isolation" value="Subprocess" hint="Not containers" />
          </div>

          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={functions.isFetching} />
                <AwsPreferencesButton disabledReason="Table preferences are not persisted in CloudForge." />
              </>
            }
          >
            <AwsActionsMenu
              label="Actions"
              items={[
                {
                  label: 'Delete',
                  danger: true,
                  onClick: () => selected !== '' && del.mutate(selected),
                  disabledReason: selected === '' ? 'Select a function first' : undefined,
                },
                {
                  label: 'Invoke',
                  onClick: () => setTab('test'),
                  disabledReason: selected === '' ? 'Select a function first' : undefined,
                },
                {
                  label: 'Copy ARN',
                  onClick: () => functionArn !== '' && void navigator.clipboard?.writeText(functionArn),
                  disabledReason: selected === '' ? 'Select a function first' : undefined,
                },
                { label: 'Publish version', disabledReason: 'Versioning beyond v1 is not implemented' },
              ]}
            />
          </AwsToolbar>

          {functions.isPending && <Loading label="Loading functions" />}
          {functions.isError && <ErrorBox message={`Functions could not be loaded (${functions.error.message}).`} />}
          {functions.data && (
            <AwsCard>
              <AwsTable<Rec>
                caption="Lambda functions"
                emptyMessage="No functions yet. Choose Create function to make one."
                rows={rows}
                totalCount={listOf(functions.data, 'functions').length}
                filter={filter}
                onFilterChange={setFilter}
                filterPlaceholder="Filter functions by name"
                selectable
                selectedIds={checked}
                onToggleRow={(id) => {
                  setSelected(id)
                  setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
                }}
                onToggleAll={(all) => setChecked(all ? rows.map((r) => r.id) : [])}
                columns={[
                  {
                    header: 'Function name',
                    render: (f) => (
                      <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelected(String(f.id)); setInvokeResult(null); setTab('code') }}>
                        {String(f.name)}
                      </button>
                    ),
                  },
                  { header: 'Description', render: () => <span title="CloudForge does not store a function description">Not available</span> },
                  { header: 'Package type', render: () => 'Zip' },
                  { header: 'Runtime', render: (f) => String(f.runtime ?? '—') },
                  { header: 'Type', render: () => 'Event Function' },
                  { header: 'Last modified', render: (f) => (typeof f.updated_at === 'string' ? new Date(f.updated_at as string).toLocaleString() : '—') },
                  {
                    header: 'Actions',
                    render: (f) => (
                      <span className="flex gap-2">
                        <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelected(String(f.id)); setTab('test') }}>Test</button>
                        <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(String(f.id))}>Delete</button>
                      </span>
                    ),
                  },
                ]}
              />
              <p className="mt-2 text-sm text-aws-muted">
                Supported runtimes: python3 / nodejs only. A Docker invoker does not exist, so every function&apos;s
                package type is Zip — container images cannot be created. No VPC attachment, managed scaling, or
                layers. The sandbox that actually applied is reported on every invocation.
              </p>
            </AwsCard>
          )}

          {sel && (
            <AwsCard>
              <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
                <div>
                  <h2 className="text-base font-semibold">{String(sel.name)}</h2>
                  <p className="text-xs text-aws-muted">
                    {String(sel.runtime ?? '—')} · version {String(sel.version ?? '—')} ·{' '}
                    {functionArn !== '' && <AwsCopy value={functionArn} label="Copy ARN" />}
                  </p>
                </div>
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelected('')}>
                  Close
                </button>
              </div>
              <ConsoleTabs
                tabs={[
                  { id: 'code', label: 'Code' },
                  { id: 'test', label: 'Test' },
                  { id: 'configuration', label: 'Configuration' },
                  { id: 'permissions', label: 'Permissions' },
                  { id: 'monitoring', label: 'Monitoring' },
                  { id: 'triggers', label: 'Triggers' },
                ]}
                active={tab}
                onChange={setTab}
              />
              <div className="pt-3">
                {tab === 'code' && (
                  <div className="space-y-2">
                    <p className="text-sm text-aws-muted">Whole program: event JSON on stdin, result on stdout.</p>
                    <pre className="overflow-auto rounded-sm border border-aws-border bg-aws-bg p-3 font-mono text-xs">{String(sel.code ?? '')}</pre>
                  </div>
                )}
                {tab === 'test' && (
                  <div>
                    <form
                      className="mb-3 flex flex-wrap items-end gap-2"
                      onSubmit={(e) => {
                        e.preventDefault()
                        invoke.mutate()
                      }}
                    >
                      <label className="text-sm">
                        Event JSON
                        <textarea value={eventJson} onChange={(e) => setEventJson(e.target.value)} rows={3} className="mt-1 w-full max-w-xl rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
                      </label>
                      <AwsButton type="submit" variant="primary" disabled={invoke.isPending}>Test</AwsButton>
                    </form>
                    {invokeResult && (
                      <div className="text-sm">
                        <p>Status: <strong>{String(invokeResult.status ?? '—')}</strong> · Duration: {String(invokeResult.duration_ms ?? '—')} ms{invokeResult.timed_out === true ? ' · TIMED OUT' : ''}</p>
                        <p className="mt-1">Sandbox: <code>{String(invokeResult.sandbox ?? '')}</code></p>
                        {typeof invokeResult.error === 'string' && invokeResult.error !== '' && <p className="mt-1 text-aws-error">Error: {String(invokeResult.error)}</p>}
                        <pre className="mt-2 overflow-auto rounded-sm border border-aws-border bg-aws-bg p-3 font-mono text-xs">{String(invokeResult.output ?? '')}</pre>
                      </div>
                    )}
                  </div>
                )}
                {tab === 'configuration' && (
                  <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-[200px_1fr]">
                    <dt className="font-medium">Runtime</dt><dd>{String(sel.runtime ?? '—')}</dd>
                    <dt className="font-medium">Handler</dt><dd>{String(sel.handler ?? '—')}</dd>
                    <dt className="font-medium">Version</dt><dd>{String(sel.version ?? '—')}</dd>
                    <dt className="font-medium">Timeout / Memory</dt><dd>{String(sel.timeout_seconds ?? '—')}s / {String(sel.memory_mb ?? '—')} MB</dd>
                    <dt className="font-medium">Aliases</dt><dd>{Object.keys((sel.aliases as Rec | undefined) ?? {}).join(', ') || 'none'}</dd>
                    <dt className="font-medium">Triggers</dt><dd>{Array.isArray(sel.triggers) ? `${(sel.triggers as unknown[]).length} configured` : 'none'}</dd>
                  </dl>
                )}
                {tab === 'permissions' && (
                  <p className="text-sm text-aws-muted">
                    Functions run with <strong>no IAM role and no AWS credentials</strong> — there is no execution-role
                    model here. The CloudForge teaching IAM console is separate and does not grant this sandbox
                    anything.
                  </p>
                )}
                {tab === 'monitoring' && (
                  <div>
                    {invocations.isPending && <Loading label="Loading invocations" />}
                    {invocations.isError && <ErrorBox message={`Invocations could not be loaded (${invocations.error.message}).`} />}
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
                  </div>
                )}
                {tab === 'triggers' && (
                  <p className="text-sm text-aws-muted">
                    Add an SQS or EventBridge trigger via the API
                    (<code>POST /aws/lambda/functions/{String(sel.id)}/triggers</code>). API Gateway integrations are
                    configured on the API, not here.
                  </p>
                )}
              </div>
            </AwsCard>
          )}
        </div>
      )}

      {activeSub === 'web-functions' && (
        <AwsCard title="Web Functions">
          <p className="text-sm text-aws-muted">
            Web Functions are <strong>UNSUPPORTED</strong> in CloudForge — there is no web-function type and no
            function-URL hosting. Functions here are event functions only: they run when invoked with an event,
            not when a browser requests a URL.
          </p>
          <div className="mt-3">
            <AwsButton disabled title="Web Functions are UNSUPPORTED — no web-function type.">
              Create web function
            </AwsButton>
          </div>
        </AwsCard>
      )}

      {activeSub === 'applications' && (
        <AwsCard title="Applications">
          <p className="text-sm text-aws-muted">
            Applications are <strong>UNSUPPORTED</strong> in CloudForge — there is no SAM/template application
            model and no application registry. Functions are created one at a time on the Event Functions page.
          </p>
          <div className="mt-3">
            <AwsButton disabled title="Applications are UNSUPPORTED — no application model.">
              Create application
            </AwsButton>
          </div>
        </AwsCard>
      )}

      {activeSub === 'test-events' && (
        <AwsCard title="Test events">
          <p className="text-sm text-aws-muted">
            CloudForge keeps no saved test-event library — test events are entered per function on that
            function&apos;s Test tab and sent as the invoke payload. Open a function to test it.
          </p>
          <div className="mt-3">
            <AwsButton onClick={() => setActiveSub('functions')}>Open Event Functions</AwsButton>
          </div>
        </AwsCard>
      )}

      {activeSub === 'code-signing' && (
        <AwsCard title="Code signing configurations">
          <p className="text-sm text-aws-muted">
            Code signing configurations are <strong>UNSUPPORTED</strong> in CloudForge — there is no code-signing
            API and no signing profile. Functions run unsigned in the subprocess sandbox.
          </p>
          <div className="mt-3">
            <AwsButton disabled title="Code signing configurations are UNSUPPORTED — no code-signing API.">
              Create code signing configuration
            </AwsButton>
          </div>
        </AwsCard>
      )}

      {activeSub === 'settings' && (
        <AwsCard title="Settings">
          <p className="text-sm text-aws-muted">
            CloudForge stores no Lambda console settings — there is no default runtime, concurrency or
            execution-role setting to change. Supported runtimes are python3 / nodejs only; functions run as
            sandboxed subprocesses with a 10s timeout and 256 MB memory.
          </p>
        </AwsCard>
      )}
    </ServiceConsole>
  )
}
