import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { AwsPreferencesButton, AwsRefreshButton, AwsToolbar, AwsWizard } from '../components/AwsShell.tsx'
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
 * stage stops the execution and later
 * stages never run. Terraform deploy honestly fails without Docker.
 *
 * Create-pipeline wizard steps follow the recorded AWS backup
 * (docs/reference/aws-console-backup/codepipeline.md): 1. Pipeline
 * settings, 2. Source, 3. Build, 4. Deploy, 5. Review. Pipeline
 * detail has tabs: Visual (the real stage list from the pipeline
 * record), History (executions for this pipeline with the stage
 * visualisation of a selected execution).
 */
export function CodePipelineConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
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
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'pipelines')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [pipelineFilter, setPipelineFilter] = useState('')
  const [execFilter, setExecFilter] = useState('')
  const [selectedPipeline, setSelectedPipeline] = useState('')
  const [detailTab, setDetailTab] = useState<'visual' | 'history'>('visual')

  const pipelines = useQuery({ queryKey: ['codepipeline-pipelines'], queryFn: () => request<Rec>('/api/v1/aws/codepipeline/pipelines'), retry: 1 })
  const executions = useQuery({ queryKey: ['codepipeline-executions'], queryFn: () => request<Rec>('/api/v1/aws/codepipeline/executions'), retry: 1, refetchInterval: 5000 })
  const pipelineRows = listOf(pipelines.data, 'pipelines')
  const execRows = listOf(executions.data, 'executions')
  const filteredPipelines = pipelineRows.filter(
    (r) => pipelineFilter.trim() === '' || str(r, 'name').toLowerCase().includes(pipelineFilter.toLowerCase()),
  )
  const filteredExecs = execRows.filter(
    (e) =>
      execFilter.trim() === '' ||
      str(e, 'pipeline_name').toLowerCase().includes(execFilter.toLowerCase()) ||
      String(e.id).toLowerCase().includes(execFilter.toLowerCase()),
  )
  const selPipeline = pipelineRows.find((r) => String(r.id) === selectedPipeline)
  const selPipelineStages = selPipeline && Array.isArray(selPipeline.stages) ? (selPipeline.stages as Record<string, unknown>[]) : []
  const selPipelineExecs = selPipeline
    ? execRows.filter((e) => str(e, 'pipeline_name') === str(selPipeline, 'name') || str(e, 'pipeline_id') === str(selPipeline, 'id'))
    : []
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
      setWizardOpen(false)
      setWizardStep(0)
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

  const wizard = wizardOpen ? (
    <AwsWizard
      title="Create pipeline"
      steps={['Pipeline settings', 'Source', 'Build', 'Deploy', 'Review']}
      current={wizardStep}
      onStep={setWizardStep}
      onCancel={() => { setWizardOpen(false); setWizardStep(0) }}
      onSubmit={() => {
        if (name.trim() === '' || sourcePath.trim() === '') {
          setNotice('Enter a pipeline name and a source path first.')
          setWizardStep(name.trim() === '' ? 0 : 1)
          return
        }
        create.mutate()
      }}
      submitLabel="Create pipeline"
      busy={create.isPending}
    >
      {wizardStep === 0 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cp-wiz-name" className="block font-medium">Pipeline name</label>
          <input id="cp-wiz-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-pipeline" className="w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
        </div>
      )}
      {wizardStep === 1 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cp-wiz-source" className="block font-medium">Source path (absolute, under the build source root)</label>
          <input id="cp-wiz-source" value={sourcePath} onChange={(e) => setSourcePath(e.target.value)} placeholder="/var/lib/cloudforge/labs/…/repo" className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          <p className="text-aws-muted">Source is a local git checkout or directory copy. No http/ssh remotes.</p>
        </div>
      )}
      {wizardStep === 2 && (
        <div className="space-y-3 text-sm">
          <label className="block font-medium">
            Build project (optional — omit for source-only)
            <input value={buildProject} onChange={(e) => setBuildProject(e.target.value)} placeholder="web-build" className="mt-1 block w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          </label>
          <p className="text-aws-muted">The Build stage runs a real CodeBuild run against the source checkout. Leave empty for a source-only pipeline.</p>
        </div>
      )}
      {wizardStep === 3 && (
        <div className="space-y-3 text-sm">
          <label className="block font-medium">
            Deploy target
            <select value={deployKind} onChange={(e) => setDeployKind(e.target.value)} className="mt-1 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
              <option value="ecs-service">ECS service</option>
              <option value="lambda-function">Lambda function</option>
              <option value="terraform">Terraform (needs Docker)</option>
            </select>
          </label>
          <label className="block">
            Target id (optional — omit for source/build-only)
            <input value={deployTarget} onChange={(e) => setDeployTarget(e.target.value)} placeholder="service or function id" className="mt-1 block w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          </label>
          {deployKind === 'ecs-service' && (
            <label className="block">
              Task definition id (ECS deploy)
              <input value={deployTaskDef} onChange={(e) => setDeployTaskDef(e.target.value)} placeholder="task definition id" className="mt-1 block w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
            </label>
          )}
          {deployKind === 'lambda-function' && (
            <label className="block">
              Code file (relative to build artifacts)
              <input value={deployCodeFile} onChange={(e) => setDeployCodeFile(e.target.value)} placeholder="built.txt" className="mt-1 block w-48 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
            </label>
          )}
          {deployKind === 'terraform' && (
            <p className="text-aws-muted">Terraform deploy honestly fails without Docker — the pipeline still runs its Source (and Build) stages first.</p>
          )}
        </div>
      )}
      {wizardStep === 4 && (
        <div className="space-y-3 text-sm">
          <dl className="grid grid-cols-2 gap-2">
            <dt className="font-medium">Pipeline name</dt><dd>{name.trim() === '' ? '— (enter a name on step 1)' : name.trim()}</dd>
            <dt className="font-medium">Source</dt><dd className="font-mono text-xs">{sourcePath.trim() === '' ? '— (enter a source path on step 2)' : sourcePath.trim()}</dd>
            <dt className="font-medium">Build</dt><dd>{buildProject.trim() === '' ? 'none (source-only)' : buildProject.trim()}</dd>
            <dt className="font-medium">Deploy</dt><dd>{deployKind}{deployTarget.trim() !== '' ? ` → ${deployTarget.trim()}` : ' (no target — source/build-only)'}</dd>
          </dl>
          <p className="text-aws-muted">Stages run in order: Source → Build → Deploy. A failed stage stops the execution and later stages never run.</p>
        </div>
      )}
    </AwsWizard>
  ) : null

  const pipelineDetail = selPipeline ? (
    <AwsCard title={`Pipeline detail — ${str(selPipeline, 'name')}`}>
      <ConsoleTabs
        tabs={[
          { id: 'visual', label: 'Visual' },
          { id: 'history', label: 'History' },
        ]}
        active={detailTab}
        onChange={(id) => setDetailTab(id as typeof detailTab)}
      />
      <div className="mt-3">
        {detailTab === 'visual' && (
          <>
            <ol className="space-y-2">
              {selPipelineStages.map((st, i) => (
                <li key={i} className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2 text-sm">
                  <span className="font-mono font-semibold">{String(st.name ?? st.type ?? `stage-${i}`)}</span>
                  <span className="text-aws-muted">{String(st.type ?? '')}</span>
                  {typeof st.source_path === 'string' && <span className="font-mono text-xs">{st.source_path as string}</span>}
                  {typeof st.build_project_name === 'string' && <span className="font-mono text-xs">{st.build_project_name as string}</span>}
                  {typeof st.target === 'string' && <span className="font-mono text-xs">{st.target as string}</span>}
                </li>
              ))}
              {selPipelineStages.length === 0 && <li className="text-sm text-aws-muted">No stages recorded for this pipeline.</li>}
            </ol>
            <p className="mt-2 text-xs text-aws-muted">The stage list is the pipeline&apos;s real stored stages.</p>
          </>
        )}
        {detailTab === 'history' && (
          <>
            <AwsTable<Rec>
              caption="Executions for this pipeline"
              emptyMessage="No executions for this pipeline yet — start one from the pipeline row."
              rows={selPipelineExecs}
              totalCount={selPipelineExecs.length}
              columns={[
                { header: 'Execution', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
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
            {selExec && (
              <div className="mt-4 border-t border-aws-border pt-3">
                <h3 className="mb-2 text-sm font-semibold">Execution {String(selExec.id).slice(0, 8)} — stage visualisation</h3>
                <ol className="space-y-2">
                  {selStages.map((st, i) => (
                    <li key={i} className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2 text-sm">
                      <span className="font-mono font-semibold">{String(st.name ?? st.type ?? `stage-${i}`)}</span>
                      <span className="text-aws-muted">{String(st.type ?? '')}</span>
                      <span className="font-medium">{String(st.status ?? 'PENDING')}</span>
                      {typeof st.detail === 'string' && st.detail !== '' && <span className="text-xs text-aws-muted">{st.detail as string}</span>}
                    </li>
                  ))}
                  {selStages.length === 0 && <li className="text-sm text-aws-muted">No stage records yet — the execution may still be starting.</li>}
                </ol>
              </div>
            )}
          </>
        )}
      </div>
    </AwsCard>
  ) : null

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodePipeline' }]}
      title="CodePipeline — Pipelines"
      description="Real local pipelines: source → build → deploy stages run in order against real services. A failed stage stops the execution."
      fidelity="PARTIAL"
      fidelityFootnote="local sources · Terraform deploy needs Docker"
      subNav={[
        { id: 'home', label: 'CodePipeline dashboard' },
        { id: 'pipelines', label: 'Pipelines' },
        { id: 'executions', label: 'Executions' },
        { id: 'settings', label: 'Settings' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="CodePipeline"
            banner="Real local pipelines: source → build → deploy stages run in order against real services. A failed stage stops the execution."
            actions={[
              { label: 'Create pipeline', primary: true, onClick: () => { setSection('pipelines'); setWizardOpen(true); setWizardStep(0) } },
            { label: 'View executions', onClick: () => setSection('executions') },
            ]}
            cards={[
              { label: 'Pipelines', count: pipelineRows.length, onSelect: () => setSection('pipelines') },
            { label: 'Executions', count: execRows.length, onSelect: () => setSection('executions') },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={pipelines.isFetching}
            health={[
              pipelines.isError ? 'Pipelines could not be loaded.' : `${pipelineRows.length} pipeline(s), ${execRows.length} execution(s).`,
            "Terraform deploy honestly fails without Docker.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — developer tools track', to: '/learn' },
            { label: 'CodeBuild (build stage)', to: '/services/codebuild' },
            ]}
          />
      )}
      {section === 'pipelines' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {wizard}
      <AwsCard title={`Pipelines (${pipelineRows.length})`}>
        <div className="mb-3">
          <AwsButton variant="primary" onClick={() => { setWizardOpen(true); setWizardStep(0) }}>Create pipeline</AwsButton>
        </div>

        {pipelines.isPending && <Loading label="Loading pipelines" />}
        {pipelines.isError && <ErrorBox message={`Pipelines could not be loaded (${pipelines.error.message}).`} />}
        {pipelines.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={pipelines.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodePipeline pipelines"
            emptyMessage="No pipelines yet. Create one above."
            rows={filteredPipelines}
            totalCount={pipelineRows.length}
            filter={pipelineFilter}
            onFilterChange={setPipelineFilter}
            filterPlaceholder="Filter pipelines by name"
            columns={[
              { header: 'Name', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelectedPipeline(String(r.id)); setDetailTab('visual') }}><span className="font-mono">{str(r, 'name')}</span></button> },
              {
                header: 'Stages',
                render: (r) => {
                  const stages = Array.isArray(r.stages) ? (r.stages as Record<string, unknown>[]) : []
                  return stages.length === 0 ? '—' : stages.map((st) => String(st.name ?? st.type ?? '')).join(', ')
                },
              },
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
          </>
        )}
      </AwsCard>

      {pipelineDetail}
      <AwsCard title={`Executions (${execRows.length})`}>
        {executions.isPending && <Loading label="Loading executions" />}
        {executions.isError && <ErrorBox message={`Executions could not be loaded (${executions.error.message}).`} />}
        {executions.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={executions.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodePipeline executions"
            emptyMessage="No executions yet. Start one from a pipeline above."
            rows={filteredExecs}
            totalCount={execRows.length}
            filter={execFilter}
            onFilterChange={setExecFilter}
            filterPlaceholder="Filter executions by pipeline or id"
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
          </>
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
        </>
      )}
      {section === 'executions' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title={`Executions (${execRows.length})`}>
        {executions.isPending && <Loading label="Loading executions" />}
        {executions.isError && <ErrorBox message={`Executions could not be loaded (${executions.error.message}).`} />}
        {executions.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={executions.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodePipeline executions"
            emptyMessage="No executions yet. Start one from a pipeline above."
            rows={filteredExecs}
            totalCount={execRows.length}
            filter={execFilter}
            onFilterChange={setExecFilter}
            filterPlaceholder="Filter executions by pipeline or id"
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
          </>
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
        </>
      )}
      {section === 'settings' && (
        <UnsupportedSection
          title="Settings"
          reason="CodePipeline settings are UNSUPPORTED in CloudForge — no settings are stored."
          actionLabel="Save settings"
        />
      )}
    </ServiceConsole>
  )
}
