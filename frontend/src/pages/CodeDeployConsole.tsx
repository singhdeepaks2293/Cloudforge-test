import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { AwsPreferencesButton, AwsRefreshButton, AwsToolbar } from '../components/AwsShell.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')

/**
 * CodeDeploy console (Phase 4) — mirrors the AWS CodeDeploy console's
 * Applications and Deployments views. A deployment updates a real
 * target (ECS service rolling deployment, or Lambda code update with
 * a version bump) and tracks real status; rollback creates a new
 * deployment that restores the previous revision. The Terraform
 * target is not a deploy target here — CodeDeploy targets are
 * ecs-service and lambda-function only.
 *
 * Deployment groups are NOT separately managed in CloudForge —
 * there is no deployment-group CRUD; a group is a name recorded
 * on an application/deployment (default 'Default'), so the
 * Deployment groups section is a derived table over the real
 * applications and deployments. The create-application form sends
 * the real deployment_group_name field. Deployment detail has
 * tabs: Overview, Timeline, Rollback. The timeline is derived
 * from started_at/ended_at and status only — no lifecycle-event
 * timeline is recorded, never the AWS 7-step lifecycle.
 */
export function CodeDeployConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [targetKind, setTargetKind] = useState('ecs-service')
  const [targetId, setTargetId] = useState('')
  const [revision, setRevision] = useState('{"task_definition_id": ""}')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'applications')
  const [groupName, setGroupName] = useState('Default')
  const [appFilter, setAppFilter] = useState('')
  const [depFilter, setDepFilter] = useState('')
  const [detailTab, setDetailTab] = useState<'overview' | 'timeline' | 'rollback'>('overview')

  const apps = useQuery({ queryKey: ['codedeploy-apps'], queryFn: () => request<Rec>('/api/v1/aws/codedeploy/applications'), retry: 1 })
  const deployments = useQuery({ queryKey: ['codedeploy-deployments'], queryFn: () => request<Rec>('/api/v1/aws/codedeploy/deployments'), retry: 1, refetchInterval: 5000 })
  const appRows = listOf(apps.data, 'applications')
  const depRows = listOf(deployments.data, 'deployments')
  const selDep = depRows.find((d) => String(d.id) === selected)
  const filteredApps = appRows.filter(
    (r) => appFilter.trim() === '' || str(r, 'name').toLowerCase().includes(appFilter.toLowerCase()),
  )
  const filteredDeps = depRows.filter(
    (d) =>
      depFilter.trim() === '' ||
      str(d, 'application_name').toLowerCase().includes(depFilter.toLowerCase()) ||
      String(d.id).toLowerCase().includes(depFilter.toLowerCase()),
  )
  const groupOf = (r: Rec): string => str(r, 'deployment_group_name') || 'Default'
  const groupNames = Array.from(new Set([...appRows.map(groupOf), ...depRows.map(groupOf)]))
  const groupRows: Rec[] = groupNames.map((g) => {
    const groupDeps = depRows.filter((d) => groupOf(d) === g).sort((a, b) => String(b.created_at ?? '').localeCompare(String(a.created_at ?? '')))
    return {
      id: g,
      name: g,
      applications: appRows.filter((a) => groupOf(a) === g).map((a) => str(a, 'name')).join(', ') || '—',
      deployments: groupDeps.length,
      latestStatus: groupDeps.length > 0 ? str(groupDeps[0], 'status') || '—' : '—',
    }
  })

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['codedeploy-apps'] })
    void queryClient.invalidateQueries({ queryKey: ['codedeploy-deployments'] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/codedeploy/applications', {
        method: 'POST',
        body: { name: name.trim(), target_type: targetKind, target_id: targetId.trim(), deployment_group_name: groupName.trim() === '' ? 'Default' : groupName.trim() },
      }),
    onSuccess: () => {
      setNotice('Application created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create application failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codedeploy/applications/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Application deleted.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete application failed: ${e.message}`),
  })
  const deploy = useMutation({
    mutationFn: (appId: string) => {
      let rev: Record<string, unknown> = {}
      try {
        rev = JSON.parse(revision) as Record<string, unknown>
      } catch {
        throw new Error('Revision is not valid JSON.')
      }
      // The API takes the revision fields flat (task_definition_id
      // for ECS, code for Lambda) — not nested under "revision".
      return request<Rec>(`/api/v1/aws/codedeploy/applications/${appId}/deployments`, { method: 'POST', body: rev })
    },
    onSuccess: () => {
      setNotice('Deployment started.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create deployment failed: ${e.message}`),
  })
  const rollback = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/codedeploy/deployments/${id}/rollback`, { method: 'POST' }),
    onSuccess: () => {
      setNotice('Rollback deployment started (restores the previous revision).')
      refresh()
    },
    onError: (e: Error) => setNotice(`Rollback failed: ${e.message}`),
  })

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodeDeploy' }]}
      title="CodeDeploy — Applications"
      description="Deployments with real status and rollback: ECS service rolling deployments and Lambda code updates. The revision is the target's new state — for ECS, the task definition id; for Lambda, the new code."
      fidelity="PARTIAL"
      fidelityFootnote="ECS + Lambda targets only"
      subNav={[
        { id: 'home', label: 'CodeDeploy dashboard' },
        { id: 'applications', label: 'Applications' },
        { id: 'deployments', label: 'Deployments' },
        { id: 'deployment-groups', label: 'Deployment groups' },
        { id: 'deployment-configurations', label: 'Deployment configurations' },
        { id: 'on-premises', label: 'On-premises instances' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="CodeDeploy"
            banner="Deployments with real status and rollback: ECS service rolling deployments and Lambda code updates."
            actions={[
              { label: 'Create application', primary: true, onClick: () => setSection('applications') },
            { label: 'View deployments', onClick: () => setSection('deployments') },
            ]}
            cards={[
              { label: 'Applications', count: appRows.length, onSelect: () => setSection('applications') },
            { label: 'Deployments', count: depRows.length, onSelect: () => setSection('deployments') },
            { label: 'Deployment groups', count: groupRows.length, onSelect: () => setSection('deployment-groups'), note: 'Derived — no group CRUD' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={apps.isFetching}
            health={[
              apps.isError ? 'Applications could not be loaded.' : `${appRows.length} application(s), ${depRows.length} deployment(s).`,
            "The Terraform target is not a deploy target here — CodeDeploy targets are ecs-service and lambda-function only.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — developer tools track', to: '/learn' },
            { label: 'CodePipeline (deploy stage)', to: '/services/codepipeline' },
            ]}
          />
      )}
      {section === 'applications' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title={`Applications (${appRows.length})`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '' && targetId.trim() !== '') create.mutate()
          }}
        >
          <label htmlFor="cd-name" className="text-sm font-medium">Application name</label>
          <input id="cd-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="web-app" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <label className="text-sm">
            Target type
            <select value={targetKind} onChange={(e) => {
              setTargetKind(e.target.value)
              setRevision(e.target.value === 'ecs-service' ? '{"task_definition_id": ""}' : '{"code": "print(\'hello\')", "runtime": "python3"}')
            }} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
              <option value="ecs-service">ECS service</option>
              <option value="lambda-function">Lambda function</option>
            </select>
          </label>
          <label htmlFor="cd-target" className="text-sm font-medium">Target id</label>
          <input id="cd-target" value={targetId} onChange={(e) => setTargetId(e.target.value)} placeholder="service or function id" className="w-64 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          <label htmlFor="cd-group" className="text-sm font-medium">Deployment group name</label>
          <input id="cd-group" value={groupName} onChange={(e) => setGroupName(e.target.value)} placeholder="Default" className="w-40 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <AwsButton type="submit" disabled={create.isPending}>Create application</AwsButton>
        </form>
        {apps.isPending && <Loading label="Loading applications" />}
        {apps.isError && <ErrorBox message={`Applications could not be loaded (${apps.error.message}).`} />}
        {apps.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={apps.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeDeploy applications"
            emptyMessage="No applications yet. Create one above."
            rows={filteredApps}
            totalCount={appRows.length}
            filter={appFilter}
            onFilterChange={setAppFilter}
            filterPlaceholder="Filter applications by name"
            columns={[
              { header: 'Application name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Target type', render: (r) => str(r, 'target_type') || '—' },
              { header: 'Target id', render: (r) => <span className="font-mono text-xs">{str(r, 'target_id')}</span> },
              { header: 'Deployment group', render: (r) => groupOf(r) },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" disabled={deploy.isPending} onClick={() => deploy.mutate(String(r.id))}>Create deployment</AwsButton>
                    <AwsButton variant="secondary" onClick={() => del.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
          </>
        )}
        <div className="mt-3">
          <label htmlFor="cd-revision" className="mb-1 block text-sm font-medium">Revision JSON for the next deployment (flat fields: task_definition_id for ECS, code for Lambda)</label>
          <textarea
            id="cd-revision"
            value={revision}
            onChange={(e) => setRevision(e.target.value)}
            rows={2}
            className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
          />
        </div>
      </AwsCard>

      <AwsCard title={`Deployments (${depRows.length})`}>
        {deployments.isPending && <Loading label="Loading deployments" />}
        {deployments.isError && <ErrorBox message={`Deployments could not be loaded (${deployments.error.message}).`} />}
        {deployments.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={deployments.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeDeploy deployments"
            emptyMessage="No deployments yet. Create one from an application above."
            rows={filteredDeps}
            totalCount={depRows.length}
            filter={depFilter}
            onFilterChange={setDepFilter}
            filterPlaceholder="Filter deployments by application or id"
            columns={[
              { header: 'Deployment', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelected(String(r.id)); setDetailTab('overview') }}><span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span></button> },
              { header: 'Application', render: (r) => <span className="font-mono">{str(r, 'application_name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" onClick={() => setSelected(String(r.id))}>Detail</AwsButton>
                    <AwsButton variant="secondary" disabled={str(r, 'status') !== 'SUCCEEDED'} title={str(r, 'status') !== 'SUCCEEDED' ? 'Only a succeeded deployment can be rolled back.' : undefined} onClick={() => rollback.mutate(String(r.id))}>
                      Rollback
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
          </>
        )}
        {selDep && (
          <AwsCard title={`Deployment detail — ${String(selDep.id).slice(0, 8)}`}>
            <ConsoleTabs
              tabs={[
                { id: 'overview', label: 'Overview' },
                { id: 'timeline', label: 'Timeline' },
                { id: 'rollback', label: 'Rollback' },
              ]}
              active={detailTab}
              onChange={(id) => setDetailTab(id as typeof detailTab)}
            />
            <div className="mt-3 text-sm">
              {detailTab === 'overview' && (
                <>
                  <p>Status: {str(selDep, 'status')}</p>
                  <p className="mt-1">Application: <span className="font-mono">{str(selDep, 'application_name')}</span></p>
                  <p className="mt-1">Deployment group: {groupOf(selDep)}</p>
                  <p className="mt-1 text-xs text-aws-muted">Revision: <span className="font-mono">{JSON.stringify(selDep.revision ?? {})}</span></p>
                  <p className="mt-1 text-xs text-aws-muted">Previous revision: <span className="font-mono">{JSON.stringify(selDep.previous_revision ?? {})}</span></p>
                  {str(selDep, 'failure_reason') !== '' && <p className="mt-1 text-xs text-aws-error">{str(selDep, 'failure_reason')}</p>}
                </>
              )}
              {detailTab === 'timeline' && (
                <>
                  <ol className="space-y-2">
                    <li className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2">
                      <span className="font-mono text-xs">{str(selDep, 'started_at') || str(selDep, 'created_at') || '—'}</span>
                      <span>Deployment started</span>
                    </li>
                    <li className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2">
                      <span className="font-mono text-xs">{str(selDep, 'ended_at') || '—'}</span>
                      <span>Deployment {str(selDep, 'status') || '—'}</span>
                    </li>
                  </ol>
                  <p className="mt-2 text-xs text-aws-muted">
                    CloudForge records no lifecycle-event timeline — the timeline above is derived from
                    started_at/ended_at and status only. There is no 7-step AWS lifecycle
                    (ApplicationStop, DownloadBundle, BeforeInstall, Install, AfterInstall,
                    ApplicationStart, ValidateService) to show.
                  </p>
                </>
              )}
              {detailTab === 'rollback' && (
                <>
                  <p>
                    Rollback creates a new deployment that restores the previous revision
                    (<span className="font-mono text-xs">{JSON.stringify(selDep.previous_revision ?? {})}</span>).
                  </p>
                  <div className="mt-3">
                    <AwsButton
                      variant="secondary"
                      disabled={str(selDep, 'status') !== 'SUCCEEDED'}
                      title={str(selDep, 'status') !== 'SUCCEEDED' ? 'Only a succeeded deployment can be rolled back.' : undefined}
                      onClick={() => rollback.mutate(String(selDep.id))}
                    >
                      Rollback
                    </AwsButton>
                  </div>
                </>
              )}
            </div>
          </AwsCard>
        )}
      </AwsCard>
        </>
      )}
      {section === 'deployments' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title={`Deployments (${depRows.length})`}>
        {deployments.isPending && <Loading label="Loading deployments" />}
        {deployments.isError && <ErrorBox message={`Deployments could not be loaded (${deployments.error.message}).`} />}
        {deployments.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={deployments.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="CodeDeploy deployments"
            emptyMessage="No deployments yet. Create one from an application above."
            rows={filteredDeps}
            totalCount={depRows.length}
            filter={depFilter}
            onFilterChange={setDepFilter}
            filterPlaceholder="Filter deployments by application or id"
            columns={[
              { header: 'Deployment', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelected(String(r.id)); setDetailTab('overview') }}><span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span></button> },
              { header: 'Application', render: (r) => <span className="font-mono">{str(r, 'application_name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Failure reason', render: (r) => <span className="text-xs">{str(r, 'failure_reason') || '—'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" onClick={() => setSelected(String(r.id))}>Detail</AwsButton>
                    <AwsButton variant="secondary" disabled={str(r, 'status') !== 'SUCCEEDED'} title={str(r, 'status') !== 'SUCCEEDED' ? 'Only a succeeded deployment can be rolled back.' : undefined} onClick={() => rollback.mutate(String(r.id))}>
                      Rollback
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
          </>
        )}
        {selDep && (
          <AwsCard title={`Deployment detail — ${String(selDep.id).slice(0, 8)}`}>
            <ConsoleTabs
              tabs={[
                { id: 'overview', label: 'Overview' },
                { id: 'timeline', label: 'Timeline' },
                { id: 'rollback', label: 'Rollback' },
              ]}
              active={detailTab}
              onChange={(id) => setDetailTab(id as typeof detailTab)}
            />
            <div className="mt-3 text-sm">
              {detailTab === 'overview' && (
                <>
                  <p>Status: {str(selDep, 'status')}</p>
                  <p className="mt-1">Application: <span className="font-mono">{str(selDep, 'application_name')}</span></p>
                  <p className="mt-1">Deployment group: {groupOf(selDep)}</p>
                  <p className="mt-1 text-xs text-aws-muted">Revision: <span className="font-mono">{JSON.stringify(selDep.revision ?? {})}</span></p>
                  <p className="mt-1 text-xs text-aws-muted">Previous revision: <span className="font-mono">{JSON.stringify(selDep.previous_revision ?? {})}</span></p>
                  {str(selDep, 'failure_reason') !== '' && <p className="mt-1 text-xs text-aws-error">{str(selDep, 'failure_reason')}</p>}
                </>
              )}
              {detailTab === 'timeline' && (
                <>
                  <ol className="space-y-2">
                    <li className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2">
                      <span className="font-mono text-xs">{str(selDep, 'started_at') || str(selDep, 'created_at') || '—'}</span>
                      <span>Deployment started</span>
                    </li>
                    <li className="flex items-center gap-3 border border-aws-border bg-white px-3 py-2">
                      <span className="font-mono text-xs">{str(selDep, 'ended_at') || '—'}</span>
                      <span>Deployment {str(selDep, 'status') || '—'}</span>
                    </li>
                  </ol>
                  <p className="mt-2 text-xs text-aws-muted">
                    CloudForge records no lifecycle-event timeline — the timeline above is derived from
                    started_at/ended_at and status only. There is no 7-step AWS lifecycle
                    (ApplicationStop, DownloadBundle, BeforeInstall, Install, AfterInstall,
                    ApplicationStart, ValidateService) to show.
                  </p>
                </>
              )}
              {detailTab === 'rollback' && (
                <>
                  <p>
                    Rollback creates a new deployment that restores the previous revision
                    (<span className="font-mono text-xs">{JSON.stringify(selDep.previous_revision ?? {})}</span>).
                  </p>
                  <div className="mt-3">
                    <AwsButton
                      variant="secondary"
                      disabled={str(selDep, 'status') !== 'SUCCEEDED'}
                      title={str(selDep, 'status') !== 'SUCCEEDED' ? 'Only a succeeded deployment can be rolled back.' : undefined}
                      onClick={() => rollback.mutate(String(selDep.id))}
                    >
                      Rollback
                    </AwsButton>
                  </div>
                </>
              )}
            </div>
          </AwsCard>
        )}
      </AwsCard>
        </>
      )}
      {section === 'deployment-groups' && (
        <AwsCard title={`Deployment groups (${groupRows.length})`}>
          <p className="mb-3 text-sm text-aws-muted">
            Deployment groups are not separately managed in CloudForge — there is no deployment-group CRUD. A
            group is a name recorded on an application or deployment (default &lsquo;Default&rsquo;); this table
            is derived from the real applications and deployments.
          </p>
          <AwsTable<Rec>
            caption="Deployment groups"
            emptyMessage="No applications or deployments yet — create an application to get the Default group."
            rows={groupRows}
            totalCount={groupRows.length}
            columns={[
              { header: 'Deployment group name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Applications', render: (r) => str(r, 'applications') },
              { header: 'Deployments', render: (r) => String(Number(r.deployments ?? 0)) },
              { header: 'Latest status', render: (r) => str(r, 'latestStatus') },
            ]}
          />
        </AwsCard>
      )}
      {section === 'deployment-configurations' && (
        <UnsupportedSection
          title="Deployment configurations"
          reason="Deployment configurations are UNSUPPORTED in CloudForge — there is no deployment-configuration API. Deployments use the backend's fixed rolling behaviour; no Canary/AllAtOnce presets are stored."
          actionLabel="Create deployment configuration"
        />
      )}
      {section === 'on-premises' && (
        <UnsupportedSection
          title="On-premises instances"
          reason="On-premises instances are UNSUPPORTED in CloudForge — there are no on-premises instances; CodeDeploy targets are ECS services and Lambda functions only."
          actionLabel="Register on-premises instance"
        />
      )}
    </ServiceConsole>
  )
}
