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

const DEFAULT_BUILDspec = `version: 0.2
phases:
  install:
    commands:
      - echo installing
  build:
    commands:
      - echo build-ok
      - cp app.txt built.txt
artifacts:
  files:
    - built.txt
`

/**
 * Latest build status for a project: the most recent build by
 * created_at (survey column). '—' when the project has never built.
 */
function latestBuildStatus(builds: Rec[], projectName: string): string {
  const mine = builds.filter((b) => str(b, 'project_name') === projectName)
  if (mine.length === 0) return '—'
  mine.sort((a, b) => String(b.created_at ?? '').localeCompare(String(a.created_at ?? '')))
  return str(mine[0], 'status') || '—'
}

/**
 * CodeBuild console (Phase 4) — mirrors the AWS CodeBuild console's
 * Build projects and Build history with real logs. A build checks out
 * a LOCAL git repo or copies a local directory (sources must be
 * absolute paths under CLOUDFORGE_BUILD_SOURCE_ROOT; no http/ssh
 * remotes), runs the buildspec phases in a sandboxed subprocess, and
 * stores real logs and artifacts on disk. No Docker build
 * environment is offered — that button does not exist here.
 *
 * Create-project wizard steps follow the recorded AWS backup
 * (docs/reference/aws-console-backup/codebuild.md): 1. Project
 * configuration, 2. Source, 3. Environment, 4. Buildspec, 5.
 * Artifacts, 6. Logs, 7. Review. The wizard sends the real
 * buildspec field (the old inline form sent buildspec_inline,
 * which the backend ignores — console-created projects silently
 * dropped their inline buildspec), plus the real env map and
 * timeout_minutes. The description on step 1 is NOT stored — the
 * teaching CodeBuild API has no description field. Project detail
 * has tabs: Overview, Builds.
 */
export function CodeBuildConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sourcePath, setSourcePath] = useState('')
  const [buildspec, setBuildspec] = useState(DEFAULT_BUILDspec)
  const [logBuildId, setLogBuildId] = useState('')
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'projects')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [description, setDescription] = useState('')
  const [envText, setEnvText] = useState('')
  const [timeout, setTimeout] = useState(60)
  const [projectFilter, setProjectFilter] = useState('')
  const [buildFilter, setBuildFilter] = useState('')
  const [selectedProject, setSelectedProject] = useState('')
  const [detailTab, setDetailTab] = useState<'overview' | 'builds'>('overview')

  const projects = useQuery({ queryKey: ['codebuild-projects'], queryFn: () => request<Rec>('/api/v1/aws/codebuild/projects'), retry: 1 })
  const builds = useQuery({ queryKey: ['codebuild-builds'], queryFn: () => request<Rec>('/api/v1/aws/codebuild/builds'), retry: 1, refetchInterval: 5000 })
  const logs = useQuery({
    queryKey: ['codebuild-logs', logBuildId],
    queryFn: () => request<Rec>(`/api/v1/aws/codebuild/builds/${logBuildId}/logs`),
    enabled: logBuildId !== '',
    retry: 1,
  })
  const projectRows = listOf(projects.data, 'projects')
  const buildRows = listOf(builds.data, 'builds')
  const filteredProjects = projectRows.filter(
    (r) => projectFilter.trim() === '' || str(r, 'name').toLowerCase().includes(projectFilter.toLowerCase()),
  )
  const filteredBuilds = buildRows.filter(
    (b) =>
      buildFilter.trim() === '' ||
      str(b, 'project_name').toLowerCase().includes(buildFilter.toLowerCase()) ||
      String(b.id).toLowerCase().includes(buildFilter.toLowerCase()),
  )
  const selProject = projectRows.find((r) => String(r.id) === selectedProject)
  const selProjectBuilds = selProject ? buildRows.filter((b) => str(b, 'project_name') === str(selProject, 'name')) : []
  const parseEnv = (text: string): Record<string, string> => {
    const out: Record<string, string> = {}
    for (const line of text.split('\n')) {
      const eq = line.indexOf('=')
      if (eq <= 0) continue
      const k = line.slice(0, eq).trim()
      if (k !== '') out[k] = line.slice(eq + 1).trim()
    }
    return out
  }

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['codebuild-projects'] })
    void queryClient.invalidateQueries({ queryKey: ['codebuild-builds'] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/codebuild/projects', {
        method: 'POST',
        body: {
          name: name.trim(),
          source_path: sourcePath.trim(),
          buildspec,
          env: parseEnv(envText),
          timeout_minutes: timeout,
        },
      }),
    onSuccess: () => {
      setNotice('Build project created.')
      setName('')
      setWizardOpen(false)
      setWizardStep(0)
      refresh()
    },
    onError: (e: Error) => setNotice(`Create project failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codebuild/projects/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Build project deleted.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete project failed: ${e.message}`),
  })
  const start = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codebuild/projects/${id}/builds`, { method: 'POST' }),
    onSuccess: () => {
      setNotice('Build started.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Start build failed: ${e.message}`),
  })

  const wizard = wizardOpen ? (
    <AwsWizard
      title="Create build project"
      steps={['Project configuration', 'Source', 'Environment', 'Buildspec', 'Artifacts', 'Logs', 'Review']}
      current={wizardStep}
      onStep={setWizardStep}
      onCancel={() => { setWizardOpen(false); setWizardStep(0) }}
      onSubmit={() => {
        if (name.trim() === '' || sourcePath.trim() === '') {
          setNotice('Enter a project name and a source path first.')
          setWizardStep(name.trim() === '' ? 0 : 1)
          return
        }
        create.mutate()
      }}
      submitLabel="Create build project"
      busy={create.isPending}
    >
      {wizardStep === 0 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cb-wiz-name" className="block font-medium">Project name</label>
          <input id="cb-wiz-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-build" className="w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <label htmlFor="cb-wiz-desc" className="block font-medium">Description (optional)</label>
          <input id="cb-wiz-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Builds the web app" className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <p className="text-aws-muted">The description is NOT stored — the teaching CodeBuild API has no description field.</p>
        </div>
      )}
      {wizardStep === 1 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cb-wiz-source" className="block font-medium">Source path (absolute, under the build source root)</label>
          <input id="cb-wiz-source" value={sourcePath} onChange={(e) => setSourcePath(e.target.value)} placeholder="/var/lib/cloudforge/labs/…/repo" className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          <p className="text-aws-muted">A build checks out a LOCAL git repo or copies a local directory. No http/ssh remotes.</p>
        </div>
      )}
      {wizardStep === 2 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cb-wiz-env" className="block font-medium">Environment variables (one KEY=VALUE per line)</label>
          <textarea id="cb-wiz-env" value={envText} onChange={(e) => setEnvText(e.target.value)} rows={4} placeholder="NODE_ENV=production" className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          <label className="block">
            Timeout (minutes, 1–480)
            <input type="number" min={1} max={480} value={timeout} onChange={(e) => setTimeout(Number(e.target.value))} className="ml-2 w-20 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <p className="text-aws-muted">The build runs in a sandboxed subprocess — no Docker build environment is offered.</p>
        </div>
      )}
      {wizardStep === 3 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cb-wiz-buildspec" className="block font-medium">Buildspec (YAML)</label>
          <textarea id="cb-wiz-buildspec" value={buildspec} onChange={(e) => setBuildspec(e.target.value)} rows={10} className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
        </div>
      )}
      {wizardStep === 4 && (
        <div className="space-y-3 text-sm">
          <p>Artifacts come from the buildspec&apos;s <code className="font-mono text-xs">artifacts</code> section — there is no separate artifact configuration to store. The build stores the listed files on disk and the Build history table shows them.</p>
        </div>
      )}
      {wizardStep === 5 && (
        <div className="space-y-3 text-sm">
          <p>Logs are the real output of the build run, stored on disk and shown on the Build history page. There is no CloudWatch Logs integration — no log group is created.</p>
        </div>
      )}
      {wizardStep === 6 && (
        <div className="space-y-3 text-sm">
          <dl className="grid grid-cols-2 gap-2">
            <dt className="font-medium">Project name</dt><dd>{name.trim() === '' ? '— (enter a name on step 1)' : name.trim()}</dd>
            <dt className="font-medium">Source</dt><dd className="font-mono text-xs">{sourcePath.trim() === '' ? '— (enter a source path on step 2)' : sourcePath.trim()}</dd>
            <dt className="font-medium">Environment variables</dt><dd>{Object.keys(parseEnv(envText)).length} set</dd>
            <dt className="font-medium">Timeout</dt><dd>{timeout} minutes</dd>
            <dt className="font-medium">Buildspec</dt><dd>{buildspec.trim().length} characters</dd>
            <dt className="font-medium">Description</dt><dd>{description.trim() === '' ? '— (not stored)' : `${description.trim()} (not stored)`}</dd>
          </dl>
        </div>
      )}
    </AwsWizard>
  ) : null

  const projectDetail = selProject ? (
    <AwsCard title={`Project detail — ${str(selProject, 'name')}`}>
      <ConsoleTabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'builds', label: 'Builds' },
        ]}
        active={detailTab}
        onChange={(id) => setDetailTab(id as typeof detailTab)}
      />
      <div className="mt-3">
        {detailTab === 'overview' && (
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <dt className="font-medium">Project name</dt><dd>{str(selProject, 'name')}</dd>
            <dt className="font-medium">Source path</dt><dd className="font-mono text-xs">{str(selProject, 'source_path')}</dd>
            <dt className="font-medium">Latest build status</dt><dd>{latestBuildStatus(buildRows, str(selProject, 'name'))}</dd>
            <dt className="font-medium">Created</dt><dd>{str(selProject, 'created_at').slice(0, 19).replace('T', ' ') || '—'}</dd>
          </dl>
        )}
        {detailTab === 'builds' && (
          <>
            <AwsTable<Rec>
              caption="Builds for this project"
              emptyMessage="No builds for this project yet — start one from the project row."
              rows={selProjectBuilds}
              totalCount={selProjectBuilds.length}
              columns={[
                { header: 'Build', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
                { header: 'Status', render: (r) => str(r, 'status') || '—' },
                { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
                { header: 'Artifacts', render: (r) => (Array.isArray(r.artifacts) ? (r.artifacts as string[]).join(', ') : '—') },
                {
                  header: 'Actions',
                  render: (r) => (
                    <AwsButton variant="secondary" onClick={() => setLogBuildId(String(r.id))}>Logs</AwsButton>
                  ),
                },
              ]}
            />
            {logBuildId !== '' && (
              <div className="mt-4 border-t border-aws-border pt-3">
                <h3 className="mb-2 text-sm font-semibold">Build logs <span className="font-mono text-xs font-normal text-aws-muted">{logBuildId}</span></h3>
                {logs.isPending && <Loading label="Loading logs" />}
                {logs.data && (
                  <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-sm border border-aws-border bg-[#f7f7f7] p-3 font-mono text-xs">
                    {typeof logs.data.logs === 'string' ? (logs.data.logs as string) : typeof logs.data.note === 'string' ? (logs.data.note as string) : 'No log output yet.'}
                  </pre>
                )}
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
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodeBuild' }]}
      title="CodeBuild — Build projects"
      description="Real local builds: checkout from a local git repo or directory, run buildspec steps in a sandboxed subprocess with timeouts, real logs and artifacts on disk."
      fidelity="PARTIAL"
      fidelityFootnote="local sources only · no Docker build env"
      subNav={[
        { id: 'home', label: 'CodeBuild dashboard' },
        { id: 'getting-started', label: 'Getting started' },
        { id: 'projects', label: 'Build projects' },
        { id: 'history', label: 'Build history' },
        { id: 'report-groups', label: 'Report groups' },
        { id: 'report-history', label: 'Report history' },
        { id: 'compute-fleets', label: 'Compute fleets' },
        { id: 'account-metrics', label: 'Account metrics' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="CodeBuild"
            banner="Real local builds: checkout from a local git repo or directory, run buildspec steps in a sandboxed subprocess with timeouts, real logs and artifacts on disk."
            actions={[
              { label: 'Create build project', primary: true, onClick: () => { setSection('projects'); setWizardOpen(true); setWizardStep(0) } },
            { label: 'View build history', onClick: () => setSection('history') },
            ]}
            cards={[
              { label: 'Build projects', count: projectRows.length, onSelect: () => setSection('projects') },
            { label: 'Builds', count: buildRows.length, onSelect: () => setSection('history') },
            { label: 'Report groups', count: 0, disabledReason: 'Report groups are not built in CloudForge.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={projects.isFetching}
            health={[
              projects.isError ? 'Build projects could not be loaded.' : `${projectRows.length} project(s), ${buildRows.length} build(s).`,
            "No Docker build environment is offered.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — developer tools track', to: '/learn' },
            { label: 'CodePipeline (run builds in a pipeline)', to: '/services/codepipeline' },
            ]}
          />
      )}
      {section === 'getting-started' && (
        <AwsCard title="Getting started — CodeBuild">
          <ol className="list-decimal space-y-1 pl-5 text-sm">
            <li>Create a build project with a local source path and a buildspec.</li>
            <li>Start a build from the project row — the buildspec phases run in a sandboxed subprocess.</li>
            <li>Read the real logs and artifacts on the Build history page.</li>
          </ol>
          <div className="mt-3">
            <AwsButton variant="primary" onClick={() => { setSection('projects'); setWizardOpen(true); setWizardStep(0) }}>Create build project</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'projects' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {wizard}
      <AwsCard title={`Build projects (${projectRows.length})`}>
        <div className="mb-3">
          <AwsButton variant="primary" onClick={() => { setWizardOpen(true); setWizardStep(0) }}>Create build project</AwsButton>
        </div>

        {projects.isPending && <Loading label="Loading build projects" />}
        {projects.isError && <ErrorBox message={`Build projects could not be loaded (${projects.error.message}).`} />}
        {projects.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={projects.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeBuild projects"
            emptyMessage="No build projects yet. Create one above."
            rows={filteredProjects}
            totalCount={projectRows.length}
            filter={projectFilter}
            onFilterChange={setProjectFilter}
            filterPlaceholder="Filter projects by name"
            columns={[
              { header: 'Name', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelectedProject(String(r.id)); setDetailTab('overview') }}><span className="font-mono">{str(r, 'name')}</span></button> },
              { header: 'Source provider', render: () => <span title="Sources are local paths, not a hosted provider">Local path — no source provider</span> },
              { header: 'Repository', render: (r) => <span className="font-mono text-xs">{str(r, 'source_path')}</span> },
              { header: 'Latest build status', render: (r) => latestBuildStatus(buildRows, str(r, 'name')) },
              { header: 'Description', render: () => <span title="CloudForge does not store a project description">Not available</span> },
              { header: 'Last Modified', render: (r) => <span className="text-aws-muted">{str(r, 'updated_at').slice(0, 19).replace('T', ' ') || 'Not available'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" disabled={start.isPending} onClick={() => start.mutate(String(r.id))}>Start build</AwsButton>
                    <AwsButton variant="secondary" onClick={() => del.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
          </>
        )}
      </AwsCard>

      {projectDetail}
      <AwsCard title={`Build history (${buildRows.length})`}>
        {builds.isPending && <Loading label="Loading builds" />}
        {builds.isError && <ErrorBox message={`Builds could not be loaded (${builds.error.message}).`} />}
        {builds.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={builds.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeBuild builds"
            emptyMessage="No builds yet. Start one from a project above."
            rows={filteredBuilds}
            totalCount={buildRows.length}
            filter={buildFilter}
            onFilterChange={setBuildFilter}
            filterPlaceholder="Filter builds by project or id"
            columns={[
              { header: 'Build', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
              { header: 'Project', render: (r) => <span className="font-mono">{str(r, 'project_name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
              { header: 'Artifacts', render: (r) => (Array.isArray(r.artifacts) ? (r.artifacts as string[]).join(', ') : '—') },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton variant="secondary" onClick={() => setLogBuildId(String(r.id))}>Logs</AwsButton>
                ),
              },
            ]}
          />
          </>
        )}
        {logBuildId !== '' && (
          <div className="mt-4 border-t border-aws-border pt-3">
            <h3 className="mb-2 text-sm font-semibold">Build logs <span className="font-mono text-xs font-normal text-aws-muted">{logBuildId}</span></h3>
            {logs.isPending && <Loading label="Loading logs" />}
            {logs.data && (
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-sm border border-aws-border bg-[#f7f7f7] p-3 font-mono text-xs">
                {typeof logs.data.logs === 'string' ? (logs.data.logs as string) : typeof logs.data.note === 'string' ? (logs.data.note as string) : 'No log output yet.'}
              </pre>
            )}
            <p className="mt-1 text-xs text-aws-muted">Logs are the real output of the build run, stored on disk — not canned text.</p>
          </div>
        )}
      </AwsCard>
        </>
      )}
      {section === 'history' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title={`Build history (${buildRows.length})`}>
        {builds.isPending && <Loading label="Loading builds" />}
        {builds.isError && <ErrorBox message={`Builds could not be loaded (${builds.error.message}).`} />}
        {builds.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={builds.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeBuild builds"
            emptyMessage="No builds yet. Start one from a project above."
            rows={filteredBuilds}
            totalCount={buildRows.length}
            filter={buildFilter}
            onFilterChange={setBuildFilter}
            filterPlaceholder="Filter builds by project or id"
            columns={[
              { header: 'Build', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
              { header: 'Project', render: (r) => <span className="font-mono">{str(r, 'project_name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
              { header: 'Artifacts', render: (r) => (Array.isArray(r.artifacts) ? (r.artifacts as string[]).join(', ') : '—') },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton variant="secondary" onClick={() => setLogBuildId(String(r.id))}>Logs</AwsButton>
                ),
              },
            ]}
          />
          </>
        )}
        {logBuildId !== '' && (
          <div className="mt-4 border-t border-aws-border pt-3">
            <h3 className="mb-2 text-sm font-semibold">Build logs <span className="font-mono text-xs font-normal text-aws-muted">{logBuildId}</span></h3>
            {logs.isPending && <Loading label="Loading logs" />}
            {logs.data && (
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-sm border border-aws-border bg-[#f7f7f7] p-3 font-mono text-xs">
                {typeof logs.data.logs === 'string' ? (logs.data.logs as string) : typeof logs.data.note === 'string' ? (logs.data.note as string) : 'No log output yet.'}
              </pre>
            )}
            <p className="mt-1 text-xs text-aws-muted">Logs are the real output of the build run, stored on disk — not canned text.</p>
          </div>
        )}
      </AwsCard>
        </>
      )}
      {section === 'report-groups' && (
        <UnsupportedSection
          title="Report groups"
          reason="Report groups are UNSUPPORTED in CloudForge — there is no test-report API and builds store no test reports."
          actionLabel="Create report group"
        />
      )}
      {section === 'report-history' && (
        <UnsupportedSection
          title="Report history"
          reason="Report history is UNSUPPORTED in CloudForge — there are no test reports to list."
          actionLabel="View report history"
        />
      )}
      {section === 'compute-fleets' && (
        <UnsupportedSection
          title="Compute fleets"
          reason="Compute fleets are UNSUPPORTED in CloudForge — builds run in a sandboxed subprocess on the API host; there is no fleet to manage."
          actionLabel="Create compute fleet"
        />
      )}
      {section === 'account-metrics' && (
        <UnsupportedSection
          title="Account metrics"
          reason="Account metrics are UNSUPPORTED in CloudForge — there is no build-metrics API. Build counts are on the dashboard."
          actionLabel="View account metrics"
        />
      )}
    </ServiceConsole>
  )
}
