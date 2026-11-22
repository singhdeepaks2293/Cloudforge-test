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
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')
const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)
const inputCls = 'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'

function Notice({ text }: { text: string }) {
  return text !== '' ? (
    <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">
      {text}
    </p>
  ) : null
}

/**
 * SageMaker console (Phase 6) — mirrors the AWS SageMaker screens:
 * notebooks (real sandboxed Python execution), training jobs (real
 * OLS linear regression on a tiny built-in dataset — no fake
 * accuracy), a model registry of real versioned artifacts on disk,
 * and inference endpoints that serve the stored model.
 */
export function SageMakerConsole() {
  const queryClient = useQueryClient()
  const [nbName, setNbName] = useState('')
  const [nbRuntime, setNbRuntime] = useState('python3')
  const [selectedNb, setSelectedNb] = useState('')
  const [cell, setCell] = useState('print(2 + 2)')
  const [jobName, setJobName] = useState('')
  const [dataset, setDataset] = useState('housing')
  const [customRows, setCustomRows] = useState('1500,3,250000\n1800,4,310000\n1200,2,190000')
  const [epName, setEpName] = useState('')
  const [epModel, setEpModel] = useState('')
  const [selectedEp, setSelectedEp] = useState('')
  const [features, setFeatures] = useState('1500,3')
  const [prediction, setPrediction] = useState('')
  const [notice, setNotice] = useState('')

  const notebooks = useQuery({ queryKey: ['sm-notebooks'], queryFn: () => request<Rec>('/api/v1/aws/sagemaker/notebooks'), retry: 1 })
  const nbRows = listOf(notebooks.data, 'notebooks')
  const jobs = useQuery({ queryKey: ['sm-jobs'], queryFn: () => request<Rec>('/api/v1/aws/sagemaker/training-jobs'), retry: 1 })
  const jobRows = listOf(jobs.data, 'training_jobs')
  const models = useQuery({ queryKey: ['sm-models'], queryFn: () => request<Rec>('/api/v1/aws/sagemaker/models'), retry: 1 })
  const modelRows = listOf(models.data, 'models')
  const endpoints = useQuery({ queryKey: ['sm-endpoints'], queryFn: () => request<Rec>('/api/v1/aws/sagemaker/endpoints'), retry: 1 })
  const epRows = listOf(endpoints.data, 'endpoints')
  const nbDetail = useQuery({
    queryKey: ['sm-notebook', selectedNb],
    queryFn: () => request<Rec>(`/api/v1/aws/sagemaker/notebooks/${selectedNb}`),
    enabled: selectedNb !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['sm-notebooks'] })
    void queryClient.invalidateQueries({ queryKey: ['sm-jobs'] })
    void queryClient.invalidateQueries({ queryKey: ['sm-models'] })
    void queryClient.invalidateQueries({ queryKey: ['sm-endpoints'] })
    void queryClient.invalidateQueries({ queryKey: ['sm-notebook', selectedNb] })
  }
  const createNb = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/sagemaker/notebooks', { method: 'POST', body: { name: nbName.trim(), runtime: nbRuntime } }),
    onSuccess: () => { setNotice('Notebook instance created.'); setNbName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const runCell = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/sagemaker/notebooks/${selectedNb}/execute`, { method: 'POST', body: { code: cell } }),
    onSuccess: () => { setNotice('Cell executed — see the run history below.'); refresh() },
    onError: (e: Error) => setNotice(`Cell failed: ${e.message}`),
  })
  const delNb = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/sagemaker/notebooks/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Notebook deleted.'); setSelectedNb(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const createJob = useMutation({
    mutationFn: () => {
      const rows =
        dataset === 'custom'
          ? customRows
              .split('\n')
              .map((l) => l.split(',').map((x) => Number(x.trim())))
              .filter((r) => r.length > 1 && r.every((v) => Number.isFinite(v)))
          : undefined
      return request<Rec>('/api/v1/aws/sagemaker/training-jobs', {
        method: 'POST',
        body: { name: jobName.trim(), dataset, rows, features: dataset === 'housing' ? ['size_sqft', 'rooms'] : dataset === 'study' ? ['study_hours', 'sleep_hours'] : [], target: dataset === 'housing' ? 'price_usd' : dataset === 'study' ? 'exam_score' : 'target' },
      })
    },
    onSuccess: () => { setNotice('Training job started — it runs async; refresh to see the result.'); setJobName(''); refresh() },
    onError: (e: Error) => setNotice(`Training job failed to start: ${e.message}`),
  })
  const delModel = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/sagemaker/models/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Model deleted.'); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const createEp = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/sagemaker/endpoints', { method: 'POST', body: { name: epName.trim(), model_id: epModel } }),
    onSuccess: () => { setNotice('Endpoint created.'); setEpName(''); refresh() },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const invoke = useMutation({
    mutationFn: () =>
      request<Rec>(`/api/v1/aws/sagemaker/endpoints/${selectedEp}/invoke`, {
        method: 'POST',
        body: { features: features.split(',').map((x) => Number(x.trim())) },
      }),
    onSuccess: (d) => { setPrediction(`Prediction: ${String(d.prediction ?? d.result ?? JSON.stringify(d))}`); refresh() },
    onError: (e: Error) => { setPrediction(''); setNotice(`Invoke failed: ${e.message}`) },
  })
  const delEp = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/sagemaker/endpoints/${id}`, { method: 'DELETE' }),
    onSuccess: () => { setNotice('Endpoint deleted.'); setSelectedEp(''); refresh() },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const executions = Array.isArray(nbDetail.data?.executions) ? (nbDetail.data?.executions as Record<string, unknown>[]) : []
  const selNb = nbRows.find((n) => n.id === selectedNb)
  const selEp = epRows.find((e) => e.id === selectedEp)

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'ML/AI', to: '/services' }, { label: 'SageMaker' }]}
        title="SageMaker — Notebooks, training, models & endpoints"
        description="Real notebook execution (sandboxed subprocess), real linear-regression training on tiny built-in datasets, and real endpoints serving stored models."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="OLS linear regression · no scikit-learn in this sandbox" />}
      />
      <Notice text={notice} />
      {(notebooks.isPending || jobs.isPending || models.isPending || endpoints.isPending) && <Loading label="Loading SageMaker resources" />}
      {notebooks.isError && <ErrorBox message={`Notebooks could not be loaded (${notebooks.error.message}).`} />}
      <AwsCard title="Notebook instances">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (nbName.trim() !== '') createNb.mutate() }}
        >
          <input value={nbName} onChange={(e) => setNbName(e.target.value)} placeholder="Notebook name" className={inputCls} />
          <label className="text-sm">
            Runtime
            <select value={nbRuntime} onChange={(e) => setNbRuntime(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="python3">python3</option>
              <option value="nodejs">nodejs</option>
            </select>
          </label>
          <AwsButton variant="primary" type="submit">Create notebook instance</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Runtime', render: (r) => str(r, 'runtime') },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => setSelectedNb(str(r, 'id'))}>Open</AwsButton>
                  <AwsButton onClick={() => delNb.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={nbRows}
          emptyMessage="No notebook instances yet."
        />
      </AwsCard>
      {selNb && (
        <AwsCard title={`Notebook: ${str(selNb, 'name')} — execute a cell`}>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <input value={cell} onChange={(e) => setCell(e.target.value)} className={`${inputCls} w-96`} aria-label="Cell code" />
            <AwsButton variant="primary" onClick={() => runCell.mutate()}>Run cell</AwsButton>
            <span className="text-xs text-aws-muted">Sandboxed subprocess, 30 s timeout, 256 MB.</span>
          </div>
          <AwsTable
            columns={[
              { header: 'Status', render: (r) => str(r as Rec, 'status') },
              { header: 'Code', render: (r) => <span className="font-mono text-xs">{str(r as Rec, 'code')}</span> },
              { header: 'Output', render: (r) => <span className="font-mono text-xs">{str(r as Rec, 'output').slice(0, 120)}</span> },
              { header: 'Error', render: (r) => str(r as Rec, 'error') },
              { header: 'Duration (ms)', render: (r) => String(num(r as Rec, 'duration_ms')) },
            ]}
            rows={executions.map((e, i) => ({ ...e, id: `exec-${i}` }))}
            emptyMessage="No cells run yet."
          />
        </AwsCard>
      )}
      <AwsCard title="Training jobs">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (jobName.trim() !== '') createJob.mutate() }}
        >
          <input value={jobName} onChange={(e) => setJobName(e.target.value)} placeholder="Job name" className={inputCls} />
          <label className="text-sm">
            Dataset
            <select value={dataset} onChange={(e) => setDataset(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="housing">housing (size_sqft, rooms → price_usd)</option>
              <option value="study">study (study_hours, sleep_hours → exam_score)</option>
              <option value="custom">custom rows (features…, target)</option>
            </select>
          </label>
          {dataset === 'custom' && (
            <textarea value={customRows} onChange={(e) => setCustomRows(e.target.value)} rows={3} aria-label="Custom dataset rows" className={`${inputCls} font-mono`} />
          )}
          <AwsButton variant="primary" type="submit">Create training job</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Dataset', render: (r) => str(r, 'dataset') },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'R²', render: (r) => (typeof r.r2 === 'number' ? (r.r2 as number).toFixed(3) : '—') },
            { header: 'RMSE', render: (r) => (typeof r.rmse === 'number' ? (r.rmse as number).toFixed(2) : '—') },
            { header: 'Error', render: (r) => str(r, 'error') },
          ]}
          rows={jobRows}
          emptyMessage="No training jobs yet."
        />
        <p className="mt-2 text-xs text-aws-muted">Training is ordinary least squares via normal equations on the dataset shown. Degenerate data (e.g. a constant column) fails the job with the real error — no accuracy is invented.</p>
      </AwsCard>
      <AwsCard title="Models — Model registry">
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Version', render: (r) => String(num(r, 'version')) },
            { header: 'Status', render: (r) => str(r, 'status') },
            { header: 'R²', render: (r) => (typeof r.r2 === 'number' ? (r.r2 as number).toFixed(3) : '—') },
            { header: 'Artifact', render: (r) => <span className="font-mono text-xs">{str(r, 'artifact_path')}</span> },
            {
              header: 'Actions',
              render: (r) => <AwsButton onClick={() => delModel.mutate(str(r, 'id'))}>Delete</AwsButton>,
            },
          ]}
          rows={modelRows}
          emptyMessage="No models yet — run a training job."
        />
      </AwsCard>
      <AwsCard title="Endpoints">
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => { e.preventDefault(); if (epName.trim() !== '' && epModel !== '') createEp.mutate() }}
        >
          <input value={epName} onChange={(e) => setEpName(e.target.value)} placeholder="Endpoint name" className={inputCls} />
          <label className="text-sm">
            Model
            <select value={epModel} onChange={(e) => setEpModel(e.target.value)} className={`ml-2 ${inputCls}`}>
              <option value="">(choose a model)</option>
              {modelRows.map((m) => (
                <option key={m.id} value={m.id}>{str(m, 'name')} v{String(num(m, 'version'))}</option>
              ))}
            </select>
          </label>
          <AwsButton variant="primary" type="submit">Create endpoint</AwsButton>
        </form>
        <AwsTable
          columns={[
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Model', render: (r) => str(r, 'model_name') || str(r, 'model_id') },
            { header: 'Status', render: (r) => str(r, 'status') },
            {
              header: 'Actions',
              render: (r) => (
                <span className="flex gap-2">
                  <AwsButton onClick={() => { setSelectedEp(str(r, 'id')); setEpModel(str(r, 'model_id')) }}>Invoke</AwsButton>
                  <AwsButton onClick={() => delEp.mutate(str(r, 'id'))}>Delete</AwsButton>
                </span>
              ),
            },
          ]}
          rows={epRows}
          emptyMessage="No endpoints yet."
        />
      </AwsCard>
      {selEp && (
        <AwsCard title={`Invoke endpoint: ${str(selEp, 'name')}`}>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <input value={features} onChange={(e) => setFeatures(e.target.value)} placeholder="Features, comma-separated" className={inputCls} aria-label="Feature values" />
            <AwsButton variant="primary" onClick={() => invoke.mutate()}>Invoke</AwsButton>
          </div>
          {prediction !== '' && <p className="text-sm" role="status">{prediction}</p>}
          <p className="mt-2 text-xs text-aws-muted">The endpoint reads the stored model artifact from disk and returns a real prediction from the trained linear model. Wrong feature count returns 400.</p>
        </AwsCard>
      )}
    </div>
  )
}

/**
 * Bedrock console (Phase 6) — a clearly labelled SIMULATED concept
 * playground. No real model runs, nothing is sent to any provider,
 * and the response text says so.
 */
export function BedrockConsole() {
  const queryClient = useQueryClient()
  const [modelId, setModelId] = useState('anthropic.claude-3-sonnet')
  const [prompt, setPrompt] = useState('Explain what a VPC is in one sentence.')
  const [notice, setNotice] = useState('')
  const [response, setResponse] = useState('')
  const models = useQuery({ queryKey: ['bedrock-models'], queryFn: () => request<Rec>('/api/v1/aws/bedrock/models'), retry: 1 })
  const modelRows = listOf(models.data, 'models')
  const invocations = useQuery({ queryKey: ['bedrock-invocations'], queryFn: () => request<Rec>('/api/v1/aws/bedrock/invocations'), retry: 1 })
  const invRows = listOf(invocations.data, 'invocations')
  const invoke = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/bedrock/invoke', { method: 'POST', body: { model_id: modelId, prompt } }),
    onSuccess: (d) => {
      setResponse(str(d, 'response'))
      setNotice(`Invocation recorded — fidelity: ${str(d, 'fidelity') || 'SIMULATED'}.`)
      void queryClient.invalidateQueries({ queryKey: ['bedrock-invocations'] })
    },
    onError: (e: Error) => setNotice(`Invoke failed: ${e.message}`),
  })
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'ML/AI', to: '/services' }, { label: 'Bedrock' }]}
        title="Bedrock — Model playground (SIMULATED)"
        description="Concept playground only. No real model runs here and nothing is sent to any provider."
        actions={<FidelityBadge fidelity="SIMULATED" footnote="template responses only" />}
      />
      <Notice text={notice} />
      {models.isPending && <Loading label="Loading models" />}
      {models.isError && <ErrorBox message={`Models could not be loaded (${models.error.message}).`} />}
      <AwsCard title="Models">
        <AwsTable
          columns={[
            { header: 'Model ID', render: (r) => <span className="font-mono text-xs">{str(r, 'model_id')}</span> },
            { header: 'Name', render: (r) => str(r, 'name') },
            { header: 'Supported', render: (r) => String(r.supported === true ? 'yes' : 'no — simulated only') },
          ]}
          rows={modelRows}
          emptyMessage="No concept models."
        />
      </AwsCard>
      <AwsCard title="Playground">
        <div className="space-y-2">
          <label className="block text-sm">
            Model
            <select value={modelId} onChange={(e) => setModelId(e.target.value)} className={`ml-2 ${inputCls}`}>
              {modelRows.map((m) => (
                <option key={m.id} value={str(m, 'model_id')}>{str(m, 'name')}</option>
              ))}
            </select>
          </label>
          <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} aria-label="Prompt" className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <AwsButton variant="primary" onClick={() => invoke.mutate()} disabled={invoke.isPending}>
            Invoke (simulated)
          </AwsButton>
          {response !== '' && (
            <div className="border border-aws-border bg-aws-light px-4 py-3 text-sm">
              <p className="mb-1 font-medium">SIMULATED response — no model ran:</p>
              <p>{response}</p>
            </div>
          )}
        </div>
      </AwsCard>
      <AwsCard title={`Invocation history (${invRows.length})`}>
        <AwsTable
          columns={[
            { header: 'Model', render: (r) => <span className="font-mono text-xs">{str(r, 'model_id')}</span> },
            { header: 'Prompt', render: (r) => str(r, 'prompt').slice(0, 80) },
            { header: 'Fidelity', render: (r) => str(r, 'fidelity') },
          ]}
          rows={invRows}
          emptyMessage="No invocations yet."
        />
      </AwsCard>
    </div>
  )
}
