/**
 * Render tests for the Phase 4 consoles (ECR, ECS, EKS, CodeBuild,
 * CodePipeline, CodeDeploy, CloudFormation): each renders real
 * API-shaped data — repositories/images, clusters/task definitions/
 * services/tasks, the EKS UNSUPPORTED decision + learning path,
 * build projects/builds, pipelines/executions, applications/
 * deployments, stacks/events — with fidelity labels and honest
 * not-built notes. fetch + localStorage are stubbed; React Query
 * runs with retries off so the render settles.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { CloudFormationConsole } from './CloudFormationConsole.tsx'
import { CodeBuildConsole } from './CodeBuildConsole.tsx'
import { CodeDeployConsole } from './CodeDeployConsole.tsx'
import { CodePipelineConsole } from './CodePipelineConsole.tsx'
import { EcrConsole } from './EcrConsole.tsx'
import { EcsConsole } from './EcsConsole.tsx'
import { EksConsole } from './EksConsole.tsx'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

async function renderSettled(
  el: React.ReactElement,
  route: string,
  prefetches: { key: unknown[]; fn: () => Promise<unknown> }[],
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  for (const p of prefetches) await client.prefetchQuery({ queryKey: p.key, queryFn: p.fn })
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>{el}</MemoryRouter>
    </QueryClientProvider>,
  )
}

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  const store = new Map<string, string>([['cloudforge_token', 'test-token']])
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  })
  fetchMock.mockImplementation((url: string) => {
    const u = String(url)
    if (u.includes('/aws/ecr/repositories')) {
      return Promise.resolve(okResponse({ repositories: [{ id: 'r-1', name: 'demo/app', registry_uri: 'localhost:8081/demo/app', image_count: 1 }] }))
    }
    if (u.includes('/aws/ecs/clusters')) {
      return Promise.resolve(okResponse({ clusters: [{ id: 'c-1', name: 'web', status: 'ACTIVE', running_tasks: 1 }] }))
    }
    if (u.includes('/aws/ecs/task-definitions')) {
      return Promise.resolve(okResponse({ task_definitions: [{ id: 't-1', family: 'echoer', revision: 1, image: 'demo/app:latest', command: ['echo', 'hello-task'], in_use_by_services: 0 }] }))
    }
    if (u.includes('/aws/ecs/services')) {
      return Promise.resolve(okResponse({ services: [{ id: 's-1', name: 'web-svc', launch_type: 'EC2', status: 'ACTIVE', desired_count: 1, running_count: 1 }] }))
    }
    if (u.includes('/aws/ecs/tasks')) {
      return Promise.resolve(okResponse({ tasks: [{ id: 'task-1', name: 'echoer-abc', launch_type: 'EC2', status: 'RUNNING', exit_code: null, command: ['echo', 'hello-task'], service_id: '' }] }))
    }
    if (u.includes('/aws/eks/info')) {
      return Promise.resolve(
        okResponse({
          service: 'eks',
          fidelity: 'UNSUPPORTED',
          supported: false,
          decision: 'UNSUPPORTED',
          reason: 'No Docker or Kubernetes is available to verify a cluster against.',
          learning_path: [
            { concept: 'Pod', what_it_is: 'The smallest deployable unit.', cloudforge_equivalent: 'An ECS task', route: '/services/ecs', try_it: 'Run a task.' },
          ],
        }),
      )
    }
    if (u.includes('/aws/codebuild/projects')) {
      return Promise.resolve(okResponse({ projects: [{ id: 'p-1', name: 'web-build', source_path: '/tmp/repo' }] }))
    }
    if (u.includes('/aws/codebuild/builds')) {
      return Promise.resolve(okResponse({ builds: [{ id: 'b-1', project_name: 'web-build', status: 'SUCCEEDED', failure_reason: '', artifacts: ['built.txt'] }] }))
    }
    if (u.includes('/aws/codepipeline/pipelines')) {
      return Promise.resolve(okResponse({ pipelines: [{ id: 'pl-1', name: 'web-pipeline', stages: [{ name: 'Source', type: 'source' }, { name: 'Build', type: 'build' }] }] }))
    }
    if (u.includes('/aws/codepipeline/executions')) {
      return Promise.resolve(okResponse({ executions: [{ id: 'e-1', pipeline_name: 'web-pipeline', status: 'SUCCEEDED', failure_reason: '', stages: [{ name: 'Source', type: 'source', status: 'SUCCEEDED' }] }] }))
    }
    if (u.includes('/aws/codedeploy/applications')) {
      return Promise.resolve(okResponse({ applications: [{ id: 'a-1', name: 'web-app', target_type: 'ecs-service', target_id: 's-1' }] }))
    }
    if (u.includes('/aws/codedeploy/deployments')) {
      return Promise.resolve(okResponse({ deployments: [{ id: 'd-1', application_name: 'web-app', status: 'SUCCEEDED', failure_reason: '', revision: { task_definition_id: 't-1' }, previous_revision: {} }] }))
    }
    if (u.includes('/aws/cloudformation/stacks')) {
      return Promise.resolve(okResponse({ stacks: [{ id: 'st-1', name: 'demo', status: 'CREATE_COMPLETE', resource_count: 2, failure_reason: '', resources: { Queue: { type: 'AWS::SQS::Queue', physical_id: 'q-1' } } }] }))
    }
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('Phase 4 consoles render real data', () => {
  it('ECR lists repositories with URI and image count', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<EcrConsole />, '/services/ecr', [
      { key: ['ecr-repos'], fn: () => request('/api/v1/aws/ecr/repositories') },
    ])
    expect(html).toContain('ECR — Repositories')
    expect(html).toContain('demo/app')
    expect(html).toContain('localhost:8081/demo/app')
    expect(html).toContain('integrity scan')
    expect(html).toContain('PARTIAL')
  })

  it('ECS lists clusters, task definitions, services and tasks', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<EcsConsole />, '/services/ecs', [
      { key: ['ecs-clusters'], fn: () => request('/api/v1/aws/ecs/clusters') },
      { key: ['ecs-taskdefs'], fn: () => request('/api/v1/aws/ecs/task-definitions') },
      { key: ['ecs-services'], fn: () => request('/api/v1/aws/ecs/services') },
      { key: ['ecs-tasks'], fn: () => request('/api/v1/aws/ecs/tasks') },
    ])
    expect(html).toContain('ECS — Clusters')
    expect(html).toContain('web')
    expect(html).toContain('echoer')
    expect(html).toContain('web-svc')
    expect(html).toContain('echoer-abc')
    expect(html).toContain('Fargate')
    expect(html).toContain('no Docker here')
  })

  it('EKS shows the UNSUPPORTED decision and the learning path', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<EksConsole />, '/services/eks', [
      { key: ['eks-info'], fn: () => request('/api/v1/aws/eks/info') },
    ])
    expect(html).toContain('EKS — Kubernetes')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('Pod')
    expect(html).toContain('An ECS task')
    expect(html).toContain('Create cluster')
    expect(html).toContain('No cluster-info endpoint')
  })

  it('CodeBuild lists projects and build history with artifacts', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<CodeBuildConsole />, '/services/codebuild', [
      { key: ['codebuild-projects'], fn: () => request('/api/v1/aws/codebuild/projects') },
      { key: ['codebuild-builds'], fn: () => request('/api/v1/aws/codebuild/builds') },
    ])
    expect(html).toContain('CodeBuild — Build projects')
    expect(html).toContain('web-build')
    expect(html).toContain('SUCCEEDED')
    expect(html).toContain('built.txt')
    expect(html).toContain('no Docker build')
  })

  it('CodePipeline lists pipelines with stages and executions', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<CodePipelineConsole />, '/services/codepipeline', [
      { key: ['codepipeline-pipelines'], fn: () => request('/api/v1/aws/codepipeline/pipelines') },
      { key: ['codepipeline-executions'], fn: () => request('/api/v1/aws/codepipeline/executions') },
    ])
    expect(html).toContain('CodePipeline — Pipelines')
    expect(html).toContain('web-pipeline')
    expect(html).toContain('Source')
    expect(html).toContain('Build')
    expect(html).toContain('SUCCEEDED')
  })

  it('CodeDeploy lists applications and deployments with rollback', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<CodeDeployConsole />, '/services/codedeploy', [
      { key: ['codedeploy-apps'], fn: () => request('/api/v1/aws/codedeploy/applications') },
      { key: ['codedeploy-deployments'], fn: () => request('/api/v1/aws/codedeploy/deployments') },
    ])
    expect(html).toContain('CodeDeploy — Applications')
    expect(html).toContain('web-app')
    expect(html).toContain('ecs-service')
    expect(html).toContain('Rollback')
  })

  it('CloudFormation lists stacks with status and resources', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<CloudFormationConsole />, '/services/cloudformation', [
      { key: ['cfn-stacks'], fn: () => request('/api/v1/aws/cloudformation/stacks') },
    ])
    expect(html).toContain('CloudFormation — Stacks')
    expect(html).toContain('demo')
    expect(html).toContain('CREATE_COMPLETE')
    expect(html).toContain('not Terraform')
    expect(html).toContain('PARTIAL')
  })
})
