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
 */
export function CodeBuildConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [sourcePath, setSourcePath] = useState('')
  const [buildspec, setBuildspec] = useState(DEFAULT_BUILDspec)
  const [logBuildId, setLogBuildId] = useState('')
  const [notice, setNotice] = useState('')

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

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['codebuild-projects'] })
    void queryClient.invalidateQueries({ queryKey: ['codebuild-builds'] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/codebuild/projects', {
        method: 'POST',
        body: { name: name.trim(), source_path: sourcePath.trim(), buildspec_inline: buildspec },
      }),
    onSuccess: () => {
      setNotice('Build project created.')
      setName('')
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

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodeBuild' }]}
        title="CodeBuild — Build projects"
        description="Real local builds: checkout from a local git repo or directory, run buildspec steps in a sandboxed subprocess with timeouts, real logs and artifacts on disk."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="local sources only · no Docker build env" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}

      <AwsCard title={`Build projects (${projectRows.length})`}>
        <form
          className="mb-3 space-y-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '' && sourcePath.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="cb-name" className="text-sm font-medium">Project name</label>
            <input id="cb-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-build" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="cb-source" className="text-sm font-medium">Source path (absolute, under the build source root)</label>
            <input id="cb-source" value={sourcePath} onChange={(e) => setSourcePath(e.target.value)} placeholder="/var/lib/cloudforge/labs/…/repo" className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          </div>
          <div>
            <label htmlFor="cb-buildspec" className="mb-1 block text-sm font-medium">Buildspec (YAML)</label>
            <textarea
              id="cb-buildspec"
              value={buildspec}
              onChange={(e) => setBuildspec(e.target.value)}
              rows={9}
              className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
            />
          </div>
          <AwsButton type="submit" disabled={create.isPending}>Create build project</AwsButton>
        </form>
        {projects.isPending && <Loading label="Loading build projects" />}
        {projects.isError && <ErrorBox message={`Build projects could not be loaded (${projects.error.message}).`} />}
        {projects.data && (
          <AwsTable<Rec>
            caption="CodeBuild projects"
            emptyMessage="No build projects yet. Create one above."
            rows={projectRows}
            columns={[
              { header: 'Name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
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
        )}
      </AwsCard>

      <AwsCard title={`Build history (${buildRows.length})`}>
        {builds.isPending && <Loading label="Loading builds" />}
        {builds.isError && <ErrorBox message={`Builds could not be loaded (${builds.error.message}).`} />}
        {builds.data && (
          <AwsTable<Rec>
            caption="CodeBuild builds"
            emptyMessage="No builds yet. Start one from a project above."
            rows={buildRows}
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
    </div>
  )
}
