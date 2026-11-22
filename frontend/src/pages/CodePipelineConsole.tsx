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

/**
 * Latest execution for a pipeline (by created_at), matched on the
 * execution's pipeline name or id. The pipeline's stage list is shown
 * on the execution's stage-detail view, not in this table.
 */
function latestExecution(executions: Rec[], pipeline: Rec): Rec | undefined {
  const mine = executions.filter((e) => str(e, 'pipeline_name') === str(pipeline, 'name') || str(e, 'pipeline_id') === str(pipeline, 'id'))
  if (mine.length === 0) return undefined
  mine.sort((a, b) => String(b.created_at ?? '').localeCompare(String(a.created_at ?? '')))
  return mine[0]
}

/**
 * CodePipeline console (Phase 4) — mirrors the AWS CodePipeline
 * console's Pipelines list and pipeline detail with stage
 * visualisation. Stages run for real, in order: source (local git
 * checkout or directory copy) → build (a real CodeBuild run against
 * that checkout) → deploy (ECS service update, Lambda update, or
 * Terraform apply). A failed stage stops the execution and later
 * stages never run. Terraform deploy honestly fails without Docker.
 */
export function CodePipelineConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sourcePath, setSourcePath] = useState('')
  const [buildProject, setBuildProject] = useState('')
  const [deployKind, setDeployKind] = useState('ecs-service')
  const [deployTarget, setDeployTarget] = useState('')
  const [deployTaskDef, setDeployTaskDef] = useState('')
  const [deployCodeFile, setDeployCodeFile] = useState('built.txt')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')

  const pipelines = useQuery({ queryKey: ['codepipeline-pipelines'], queryFn: () => request<Rec>('/api/v1/aws/codepipeline/pipelines'), retry: 1 })
  const executions = useQuery({ queryKey: ['codepipeline-executions'], queryFn: () => request<Rec>('/api/v1/aws/codepipeline/executions'), retry: 1, refetchInterval: 5000 })
  const pipelineRows = listOf(pipelines.data, 'pipelines')
  const execRows = listOf(executions.data, 'executions')
  const selExec = execRows.find((e) => String(e.id) === selected)
  const selStages = Array.isArray(selExec?.stages) ? (selExec?.stages as Record<string, unknown>[]) : []

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['codepipeline-pipelines'] })
    void queryClient.invalidateQueries({ queryKey: ['codepipeline-executions'] })
  }
  const create = useMutation({
    mutationFn: () => {
      const stages: Record<string, unknown>[] = [
        { name: 'Source', type: 'source', source_path: sourcePath.trim() },
      ]
      if (buildProject.trim() !== '') stages.push({ name: 'Build', type: 'build', build_project_name: buildProject.trim() })
      // Deploy stage contract (backend): target + service_id +
      // task_definition_id for ECS, function_id + code_file for
      // Lambda, target only for Terraform.
      if (deployKind === 'ecs-service' && deployTarget.trim() !== '') {
        stages.push({ name: 'Deploy', type: 'deploy', target: 'ecs-service', service_id: deployTarget.trim(), task_definition_id: deployTaskDef.trim() })
      } else if (deployKind === 'lambda-function' && deployTarget.trim() !== '') {
        stages.push({ name: 'Deploy', type: 'deploy', target: 'lambda-function', function_id: deployTarget.trim(), code_file: deployCodeFile.trim() })
      } else if (deployKind === 'terraform') {
        stages.push({ name: 'Deploy', type: 'deploy', target: 'terraform' })
      }
      return request<Rec>('/api/v1/aws/codepipeline/pipelines', { method: 'POST', body: { name: name.trim(), stages } })
    },
    onSuccess: () => {
      setNotice('Pipeline created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create pipeline failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codepipeline/pipelines/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Pipeline deleted.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete pipeline failed: ${e.message}`),
  })
  const start = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codepipeline/pipelines/${id}/executions`, { method: 'POST' }),
    onSuccess: () => {
      setNotice('Pipeline execution started.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Start execution failed: ${e.message}`),
  })

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodePipeline' }]}
        title="CodePipeline — Pipelines"
        description="Real local pipelines: source → build → deploy stages run in order against real services. A failed stage stops the execution."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="local sources · Terraform deploy needs Docker" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}

      <AwsCard title={`Pipelines (${pipelineRows.length})`}>
        <form
          className="mb-3 space-y-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '' && sourcePath.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="cp-name" className="text-sm font-medium">Pipeline name</label>
            <input id="cp-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-pipeline" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="cp-source" className="text-sm font-medium">Source path (absolute, under the build source root)</label>
            <input id="cp-source" value={sourcePath} onChange={(e) => setSourcePath(e.target.value)} placeholder="/var/lib/cloudforge/labs/…/repo" className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="text-sm">
              Build project (optional — omit for source-only)
              <input value={buildProject} onChange={(e) => setBuildProject(e.target.value)} placeholder="web-build" className="ml-2 w-48 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            </label>
            <label className="text-sm">
              Deploy target
              <select value={deployKind} onChange={(e) => setDeployKind(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
                <option value="ecs-service">ECS service</option>
                <option value="lambda-function">Lambda function</option>
                <option value="terraform">Terraform (needs Docker)</option>
              </select>
            </label>
            <label className="text-sm">
              Target id (optional — omit for source/build-only)
              <input value={deployTarget} onChange={(e) => setDeployTarget(e.target.value)} placeholder="service or function id" className="ml-2 w-64 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
            </label>
            {deployKind === 'ecs-service' && (
              <label className="text-sm">
                Task definition id (ECS deploy)
                <input value={deployTaskDef} onChange={(e) => setDeployTaskDef(e.target.value)} placeholder="task definition id" className="ml-2 w-64 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
              </label>
            )}
            {deployKind === 'lambda-function' && (
              <label className="text-sm">
                Code file (relative to build artifacts)
                <input value={deployCodeFile} onChange={(e) => setDeployCodeFile(e.target.value)} placeholder="built.txt" className="ml-2 w-48 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
              </label>
            )}
          </div>
          <AwsButton type="submit" disabled={create.isPending}>Create pipeline</AwsButton>
        </form>
        {pipelines.isPending && <Loading label="Loading pipelines" />}
        {pipelines.isError && <ErrorBox message={`Pipelines could not be loaded (${pipelines.error.message}).`} />}
        {pipelines.data && (
          <AwsTable<Rec>
            caption="CodePipeline pipelines"
            emptyMessage="No pipelines yet. Create one above."
            rows={pipelineRows}
            columns={[
              { header: 'Name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Latest execution status', render: (r) => { const e = latestExecution(execRows, r); return e ? str(e, 'status') || '—' : '—' } },
              { header: 'Latest source revisions', render: () => <span title="CloudForge does not record source revisions">Not available</span> },
              { header: 'Latest execution started', render: (r) => { const e = latestExecution(execRows, r); return <span className="text-aws-muted">{e ? String(e.created_at ?? '').slice(0, 19).replace('T', ' ') || '—' : '—'}</span> } },
              { header: 'Most recent executions', render: (r) => String(execRows.filter((e) => str(e, 'pipeline_name') === str(r, 'name') || str(e, 'pipeline_id') === str(r, 'id')).length) },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" disabled={start.isPending} onClick={() => start.mutate(String(r.id))}>Start execution</AwsButton>
                    <AwsButton variant="secondary" onClick={() => del.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </AwsCard>

      <AwsCard title={`Executions (${execRows.length})`}>
        {executions.isPending && <Loading label="Loading executions" />}
        {executions.isError && <ErrorBox message={`Executions could not be loaded (${executions.error.message}).`} />}
        {executions.data && (
          <AwsTable<Rec>
            caption="CodePipeline executions"
            emptyMessage="No executions yet. Start one from a pipeline above."
            rows={execRows}
            columns={[
              { header: 'Execution', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
              { header: 'Pipeline', render: (r) => <span className="font-mono">{str(r, 'pipeline_name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton variant="secondary" onClick={() => setSelected(String(r.id))}>Stage detail</AwsButton>
                ),
              },
            ]}
          />
        )}
        {selExec && (
          <div className="mt-4 border-t border-aws-border pt-3">
            <h3 className="mb-2 text-sm font-semibold">Execution {String(selExec.id).slice(0, 8)} — stage visualisation</h3>
            <ol className="space-y-2">
              {selStages.map((s, i) => (
                <li key={i} className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2 text-sm">
                  <span className="font-mono font-semibold">{String(s.name ?? s.type ?? `stage-${i}`)}</span>
                  <span className="text-aws-muted">{String(s.type ?? '')}</span>
                  <span className="font-medium">{String(s.status ?? 'PENDING')}</span>
                  {typeof s.detail === 'string' && s.detail !== '' && <span className="text-xs text-aws-muted">{s.detail as string}</span>}
                </li>
              ))}
              {selStages.length === 0 && <li className="text-sm text-aws-muted">No stage records yet — the execution may still be starting.</li>}
            </ol>
          </div>
        )}
      </AwsCard>
    </div>
  )
}
