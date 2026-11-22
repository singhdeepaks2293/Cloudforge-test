import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')
const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)

/**
 * ECS console (Phase 4) — mirrors the AWS ECS console's Clusters,
 * Task Definitions, Services and Tasks views, plus the Fargate
 * serverless run (same runtime, no cluster). Tasks run for real in a
 * sandboxed subprocess with resource limits (no Docker here, no
 * container is started, no image is pulled — the image is recorded
 * and matched against the ECR catalogue). Task status comes from the
 * real process: PENDING → RUNNING → STOPPED with a real exit code.
 * No Docker, no VPC networking, no service discovery, no load
 * balancer integration.
 */
export function EcsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [clusterName, setClusterName] = useState('')
  const [family, setFamily] = useState('')
  const [image, setImage] = useState('demo/app:latest')
  const [command, setCommand] = useState('echo hello-task')
  const [svcName, setSvcName] = useState('')
  const [svcCluster, setSvcCluster] = useState('')
  const [svcTaskDef, setSvcTaskDef] = useState('')
  const [svcDesired, setSvcDesired] = useState(1)
  const [svcLaunch, setSvcLaunch] = useState('EC2')
  const [runTaskDef, setRunTaskDef] = useState('')
  const [runCluster, setRunCluster] = useState('')
  const [runLaunch, setRunLaunch] = useState('EC2')
  const [runCount, setRunCount] = useState(1)
  const [logTaskId, setLogTaskId] = useState('')
  const [notice, setNotice] = useState('')

  const clusters = useQuery({ queryKey: ['ecs-clusters'], queryFn: () => request<Rec>('/api/v1/aws/ecs/clusters'), retry: 1 })
  const taskdefs = useQuery({ queryKey: ['ecs-taskdefs'], queryFn: () => request<Rec>('/api/v1/aws/ecs/task-definitions'), retry: 1 })
  const services = useQuery({ queryKey: ['ecs-services'], queryFn: () => request<Rec>('/api/v1/aws/ecs/services'), retry: 1 })
  const tasks = useQuery({ queryKey: ['ecs-tasks'], queryFn: () => request<Rec>('/api/v1/aws/ecs/tasks'), retry: 1, refetchInterval: 5000 })
  const logs = useQuery({
    queryKey: ['ecs-task-logs', logTaskId],
    queryFn: () => request<Rec>(`/api/v1/aws/ecs/tasks/${logTaskId}/logs`),
    enabled: logTaskId !== '',
    retry: 1,
  })
  const clusterRows = listOf(clusters.data, 'clusters')
  const taskdefRows = listOf(taskdefs.data, 'task_definitions')
  const serviceRows = listOf(services.data, 'services')
  const taskRows = listOf(tasks.data, 'tasks')

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['ecs-clusters'] })
    void queryClient.invalidateQueries({ queryKey: ['ecs-taskdefs'] })
    void queryClient.invalidateQueries({ queryKey: ['ecs-services'] })
    void queryClient.invalidateQueries({ queryKey: ['ecs-tasks'] })
  }
  const createCluster = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/ecs/clusters', { method: 'POST', body: { name: clusterName.trim() } }),
    onSuccess: () => {
      setNotice('Cluster created.')
      setClusterName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create cluster failed: ${e.message}`),
  })
  const deleteCluster = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecs/clusters/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Cluster deleted.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete cluster failed: ${e.message}`),
  })
  const registerTaskDef = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ecs/task-definitions', {
        method: 'POST',
        body: {
          family: family.trim(),
          container_definitions: [
            { name: family.trim(), image: image.trim(), command: command.trim().split(/\s+/), cpu: 256, memory_mb: 128 },
          ],
        },
      }),
    onSuccess: () => {
      setNotice('Task definition registered.')
      setFamily('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Register task definition failed: ${e.message}`),
  })
  const deleteTaskDef = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecs/task-definitions/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Task definition deleted.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete task definition failed: ${e.message}`),
  })
  const createService = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ecs/services', {
        method: 'POST',
        body: {
          name: svcName.trim(),
          cluster_id: svcLaunch === 'FARGATE' ? undefined : svcCluster,
          task_definition_id: svcTaskDef,
          desired_count: svcDesired,
          launch_type: svcLaunch,
        },
      }),
    onSuccess: () => {
      setNotice('Service created and reconciled (desired tasks started).')
      setSvcName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create service failed: ${e.message}`),
  })
  const updateService = useMutation({
    mutationFn: (v: { id: string; desired: number }) =>
      request<Rec>(`/api/v1/aws/ecs/services/${v.id}`, { method: 'PUT', body: { desired_count: v.desired } }),
    onSuccess: () => {
      setNotice('Service updated — rolling deployment started.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Update service failed: ${e.message}`),
  })
  const deleteService = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecs/services/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Service deleted (its tasks were stopped).')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete service failed: ${e.message}`),
  })
  const runTask = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ecs/tasks/run', {
        method: 'POST',
        body: {
          task_definition_id: runTaskDef,
          cluster_id: runLaunch === 'FARGATE' ? undefined : runCluster,
          launch_type: runLaunch,
          count: runCount,
        },
      }),
    onSuccess: () => {
      setNotice(`Task run started (${runLaunch}).`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Run task failed: ${e.message}`),
  })
  const stopTask = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecs/tasks/${id}`, { method: 'POST' }),
    onSuccess: () => {
      setNotice('Task stop requested.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Stop task failed: ${e.message}`),
  })

  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Containers', to: '/services' }, { label: 'ECS' }]}
      title="ECS — Clusters, Task Definitions, Services, Tasks"
      description="Container orchestration on a real sandboxed subprocess runtime: no Docker here, so no container is started and no image is pulled — the task command runs with resource limits and its status comes from the real process."
      fidelity="PARTIAL"
      fidelityFootnote="subprocess runtime · no Docker · no VPC networking"
      homeLabel="ECS dashboard"
      listLabel="Clusters"
      initialSection={initialSection}
      home={(goToList) => (
          <ServiceHomeDashboard
            serviceName="ECS"
            banner="Clusters, task definitions, services and tasks are real records; the runtime is SIMULATED — a task command runs as a sandboxed subprocess on the API host, no Docker container is started and no image is pulled."
            actions={[
              { label: 'Create cluster', primary: true, onClick: goToList },
            { label: 'Open Fargate (serverless runs)', to: '/services/fargate' },
            ]}
            cards={[
              { label: 'Clusters', count: clusterRows.length, onSelect: goToList },
            { label: 'Task definitions', count: taskdefRows.length, onSelect: goToList },
            { label: 'Services', count: serviceRows.length, onSelect: goToList },
            { label: 'Tasks', count: taskRows.length, onSelect: goToList, note: 'Status comes from the real process' },
            { label: 'Fargate', count: '—', to: '/services/fargate', note: 'Same runtime, launch type FARGATE, no cluster to manage' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={clusters.isFetching}
            health={[
              "clusters.isError ? 'Clusters could not be loaded.' : `${clusterRows.length} cluster(s), ${serviceRows.length} service(s), ${taskRows.length} task(s).`",
            "Desired count is reconciled on every read and when a task stops; logs are real process output.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — containers track', to: '/learn' },
            { label: 'EKS (unsupported — Kubernetes concepts)', to: '/services/eks' },
            ]}
          />
        )}
    >
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}

      <AwsCard title={`Clusters (${clusterRows.length})`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (clusterName.trim() !== '') createCluster.mutate()
          }}
        >
          <label htmlFor="ecs-cluster-name" className="text-sm font-medium">Cluster name</label>
          <input id="ecs-cluster-name" value={clusterName} onChange={(e) => setClusterName(e.target.value)} placeholder="web" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <AwsButton type="submit" disabled={createCluster.isPending}>Create cluster</AwsButton>
        </form>
        {clusters.isPending && <Loading label="Loading clusters" />}
        {clusters.isError && <ErrorBox message={`Clusters could not be loaded (${clusters.error.message}).`} />}
        {clusters.data && (
          <AwsTable<Rec>
            caption="ECS clusters"
            emptyMessage="No clusters yet. Create one above."
            rows={clusterRows}
            columns={[
              { header: 'Cluster name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Running tasks', render: (r) => String(num(r, 'running_tasks')) },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton
                    variant="secondary"
                    disabled={num(r, 'running_tasks') > 0}
                    title={num(r, 'running_tasks') > 0 ? 'Stop the running tasks (or delete the services) first.' : undefined}
                    onClick={() => deleteCluster.mutate(String(r.id))}
                  >
                    Delete
                  </AwsButton>
                ),
              },
            ]}
          />
        )}
      </AwsCard>

      <AwsCard title={`Task definitions (${taskdefRows.length})`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (family.trim() !== '') registerTaskDef.mutate()
          }}
        >
          <label htmlFor="ecs-family" className="text-sm font-medium">Family</label>
          <input id="ecs-family" value={family} onChange={(e) => setFamily(e.target.value)} placeholder="echoer" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <label className="text-sm">
            Image
            <input value={image} onChange={(e) => setImage(e.target.value)} className="ml-2 w-48 rounded-sm border border-aws-border-dark bg-white px-2 py-1 font-mono text-xs" />
          </label>
          <label className="text-sm">
            Command
            <input value={command} onChange={(e) => setCommand(e.target.value)} className="ml-2 w-48 rounded-sm border border-aws-border-dark bg-white px-2 py-1 font-mono text-xs" />
          </label>
          <AwsButton type="submit" disabled={registerTaskDef.isPending}>Register task definition</AwsButton>
        </form>
        <p className="mb-3 text-xs text-aws-muted">CPU 256 / memory 128 MiB per container by default. A container definition without a command is rejected — the runtime needs a real command to run.</p>
        {taskdefs.data && (
          <AwsTable<Rec>
            caption="ECS task definitions"
            emptyMessage="No task definitions yet. Register one above."
            rows={taskdefRows}
            columns={[
              { header: 'Family', render: (r) => <span className="font-mono">{str(r, 'family')}</span> },
              { header: 'Revision', render: (r) => String(num(r, 'revision')) },
              { header: 'Image', render: (r) => <span className="font-mono text-xs">{str(r, 'image') || '—'}</span> },
              { header: 'Command', render: (r) => <span className="font-mono text-xs">{Array.isArray(r.command) ? (r.command as string[]).join(' ') : '—'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton
                    variant="secondary"
                    title={num(r, 'in_use_by_services') > 0 ? 'In use by an active service.' : undefined}
                    disabled={num(r, 'in_use_by_services') > 0}
                    onClick={() => deleteTaskDef.mutate(String(r.id))}
                  >
                    Deregister
                  </AwsButton>
                ),
              },
            ]}
          />
        )}
      </AwsCard>

      <AwsCard title={`Services (${serviceRows.length})`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (svcName.trim() !== '' && svcTaskDef !== '') createService.mutate()
          }}
        >
          <label htmlFor="ecs-svc-name" className="text-sm font-medium">Service name</label>
          <input id="ecs-svc-name" value={svcName} onChange={(e) => setSvcName(e.target.value)} placeholder="web-svc" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <label className="text-sm">
            Launch type
            <select value={svcLaunch} onChange={(e) => setSvcLaunch(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
              <option value="EC2">EC2</option>
              <option value="FARGATE">Fargate</option>
            </select>
          </label>
          <label className="text-sm">
            Cluster
            <select value={svcCluster} onChange={(e) => setSvcCluster(e.target.value)} disabled={svcLaunch === 'FARGATE'} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm disabled:opacity-50">
              <option value="">— select —</option>
              {clusterRows.map((c) => (
                <option key={String(c.id)} value={String(c.id)}>{str(c, 'name')}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Task definition
            <select value={svcTaskDef} onChange={(e) => setSvcTaskDef(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
              <option value="">— select —</option>
              {taskdefRows.map((t) => (
                <option key={String(t.id)} value={String(t.id)}>{str(t, 'family')}:rev {num(t, 'revision')}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Desired count
            <input type="number" min={0} max={10} value={svcDesired} onChange={(e) => setSvcDesired(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <AwsButton type="submit" disabled={createService.isPending}>Create service</AwsButton>
        </form>
        {services.data && (
          <AwsTable<Rec>
            caption="ECS services"
            emptyMessage="No services yet. Create one above."
            rows={serviceRows}
            columns={[
              { header: 'Service name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Launch type', render: (r) => str(r, 'launch_type') || '—' },
              {
                header: 'Status',
                render: (r) => (
                  <span>
                    {str(r, 'status') || '—'}
                    {r.reconcile_throttled === true && (
                      <span className="block text-xs text-aws-warning">Throttled: {typeof r.reconcile_throttle_reason === 'string' ? (r.reconcile_throttle_reason as string) : 'restarts paused — update the service to resume.'}</span>
                    )}
                  </span>
                ),
              },
              { header: 'Desired', render: (r) => String(num(r, 'desired_count')) },
              { header: 'Running', render: (r) => String(num(r, 'running_count')) },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" disabled={num(r, 'desired_count') >= 10} onClick={() => updateService.mutate({ id: String(r.id), desired: num(r, 'desired_count') + 1 })}>Scale +1</AwsButton>
                    <AwsButton variant="secondary" disabled={num(r, 'desired_count') <= 0} onClick={() => updateService.mutate({ id: String(r.id), desired: num(r, 'desired_count') - 1 })}>Scale −1</AwsButton>
                    <AwsButton variant="secondary" onClick={() => deleteService.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </AwsCard>

      <AwsCard title={`Tasks (${taskRows.length}) — EC2 and Fargate runs`}>
        <form
          className="mb-3 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (runTaskDef !== '') runTask.mutate()
          }}
        >
          <label className="text-sm font-medium">
            Run task
            <select value={runTaskDef} onChange={(e) => setRunTaskDef(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
              <option value="">— task definition —</option>
              {taskdefRows.map((t) => (
                <option key={String(t.id)} value={String(t.id)}>{str(t, 'family')}:rev {num(t, 'revision')}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Launch type
            <select value={runLaunch} onChange={(e) => setRunLaunch(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
              <option value="EC2">EC2</option>
              <option value="FARGATE">Fargate (serverless — no cluster)</option>
            </select>
          </label>
          <label className="text-sm">
            Cluster
            <select value={runCluster} onChange={(e) => setRunCluster(e.target.value)} disabled={runLaunch === 'FARGATE'} className="ml-2 rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm disabled:opacity-50">
              <option value="">— select —</option>
              {clusterRows.map((c) => (
                <option key={String(c.id)} value={String(c.id)}>{str(c, 'name')}</option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            Count
            <input type="number" min={1} max={10} value={runCount} onChange={(e) => setRunCount(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <AwsButton type="submit" disabled={runTask.isPending}>Run task</AwsButton>
        </form>
        {tasks.data && (
          <AwsTable<Rec>
            caption="ECS tasks"
            emptyMessage="No tasks yet. Run one above, or create a service."
            rows={taskRows}
            columns={[
              { header: 'Task', render: (r) => <span className="font-mono text-xs">{str(r, 'name')}</span> },
              { header: 'Launch type', render: (r) => str(r, 'launch_type') || '—' },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Exit code', render: (r) => (typeof r.exit_code === 'number' ? String(r.exit_code as number) : '—') },
              { header: 'Command', render: (r) => <span className="font-mono text-xs">{Array.isArray(r.command) ? (r.command as string[]).join(' ') : '—'}</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" onClick={() => setLogTaskId(String(r.id))}>Logs</AwsButton>
                    <AwsButton
                      variant="secondary"
                      disabled={str(r, 'status') !== 'RUNNING'}
                      title={str(r, 'status') !== 'RUNNING' ? 'Only a RUNNING task can be stopped.' : str(r, 'service_id') !== '' ? 'Service-managed task — scale the service instead.' : undefined}
                      onClick={() => stopTask.mutate(String(r.id))}
                    >
                      Stop
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
        )}
        {logTaskId !== '' && (
          <div className="mt-4 border-t border-aws-border pt-3">
            <h3 className="mb-2 text-sm font-semibold">Task logs <span className="font-mono text-xs font-normal text-aws-muted">{logTaskId}</span></h3>
            {logs.isPending && <Loading label="Loading logs" />}
            {logs.data && (
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-sm border border-aws-border bg-[#f7f7f7] p-3 font-mono text-xs">
                {typeof logs.data.logs === 'string' ? (logs.data.logs as string) : typeof logs.data.note === 'string' ? (logs.data.note as string) : 'No log output.'}
              </pre>
            )}
          </div>
        )}
        <p className="mt-3 text-xs text-aws-muted">
          Runtime: subprocess sandbox — timeout + output cap + temp workdir + prlimit memory cap + uid 65534 (nobody). Fargate runs use the same runtime with no cluster to manage.
        </p>
      </AwsCard>
    </ConsoleWithHome>
  )
}
