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
 * CodeDeploy console (Phase 4) — mirrors the AWS CodeDeploy console's
 * Applications and Deployments views. A deployment updates a real
 * target (ECS service rolling deployment, or Lambda code update with
 * a version bump) and tracks real status; rollback creates a new
 * deployment that restores the previous revision. The Terraform
 * target is not a deploy target here — CodeDeploy targets are
 * ecs-service and lambda-function only.
 */
export function CodeDeployConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [targetKind, setTargetKind] = useState('ecs-service')
  const [targetId, setTargetId] = useState('')
  const [revision, setRevision] = useState('{"task_definition_id": ""}')
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')

  const apps = useQuery({ queryKey: ['codedeploy-apps'], queryFn: () => request<Rec>('/api/v1/aws/codedeploy/applications'), retry: 1 })
  const deployments = useQuery({ queryKey: ['codedeploy-deployments'], queryFn: () => request<Rec>('/api/v1/aws/codedeploy/deployments'), retry: 1, refetchInterval: 5000 })
  const appRows = listOf(apps.data, 'applications')
  const depRows = listOf(deployments.data, 'deployments')
  const selDep = depRows.find((d) => String(d.id) === selected)

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['codedeploy-apps'] })
    void queryClient.invalidateQueries({ queryKey: ['codedeploy-deployments'] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/codedeploy/applications', {
        method: 'POST',
        body: { name: name.trim(), target_type: targetKind, target_id: targetId.trim() },
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
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Developer Tools', to: '/services' }, { label: 'CodeDeploy' }]}
        title="CodeDeploy — Applications"
        description="Deployments with real status and rollback: ECS service rolling deployments and Lambda code updates. The revision is the target's new state — for ECS, the task definition id; for Lambda, the new code."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="ECS + Lambda targets only" />}
      />
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
          <AwsButton type="submit" disabled={create.isPending}>Create application</AwsButton>
        </form>
        {apps.isPending && <Loading label="Loading applications" />}
        {apps.isError && <ErrorBox message={`Applications could not be loaded (${apps.error.message}).`} />}
        {apps.data && (
          <AwsTable<Rec>
            caption="CodeDeploy applications"
            emptyMessage="No applications yet. Create one above."
            rows={appRows}
            columns={[
              { header: 'Application name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Target type', render: (r) => str(r, 'target_type') || '—' },
              { header: 'Target id', render: (r) => <span className="font-mono text-xs">{str(r, 'target_id')}</span> },
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
          <AwsTable<Rec>
            caption="CodeDeploy deployments"
            emptyMessage="No deployments yet. Create one from an application above."
            rows={depRows}
            columns={[
              { header: 'Deployment', render: (r) => <span className="font-mono text-xs">{String(r.id).slice(0, 8)}</span> },
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
        )}
        {selDep && (
          <div className="mt-4 border-t border-aws-border pt-3 text-sm">
            <h3 className="mb-2 font-semibold">Deployment {String(selDep.id).slice(0, 8)}</h3>
            <p>Status: {str(selDep, 'status')}</p>
            <p className="mt-1 text-xs text-aws-muted">Revision: <span className="font-mono">{JSON.stringify(selDep.revision ?? {})}</span></p>
            <p className="mt-1 text-xs text-aws-muted">Previous revision: <span className="font-mono">{JSON.stringify(selDep.previous_revision ?? {})}</span></p>
            {str(selDep, 'failure_reason') !== '' && <p className="mt-1 text-xs text-aws-error">{str(selDep, 'failure_reason')}</p>}
          </div>
        )}
      </AwsCard>
    </div>
  )
}
