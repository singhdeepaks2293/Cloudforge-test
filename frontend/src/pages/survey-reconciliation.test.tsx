/**
 * Survey-reconciliation tests (Workstream 1b, 2026-11-22): the lite
 * consoles match the AWS console survey
 * (docs/reference/aws-console-survey-2026-11-22.md) — exact list-table
 * columns, ELB/Auto Scaling/EBS as EC2 sections, Transit Gateway/VPN
 * as VPC sections, and survey landings. fetch is stubbed; React Query
 * runs with retries off so the render settles. Counts come from
 * stubbed API-shaped data — nothing is invented by the components.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { fetchIamGroups, fetchIamPolicies, fetchIamRoles, fetchIamUsers, fetchS3Buckets, request } from '../services/api.ts'
import { S3Console } from './S3Console.tsx'
import { IamConsole } from './IamConsole.tsx'
import { LambdaConsole } from './LambdaConsole.tsx'
import { ElbListContent } from './ElbConsole.tsx'
import { Ec2Console } from './Ec2Console.tsx'
import { VpcConsole } from './VpcConsole.tsx'
import { TransitGatewayListContent } from './NetworkingConsoles.tsx'
import { CostExplorerConsole } from './ManagementConsoles.tsx'
import { CloudFormationConsole } from './CloudFormationConsole.tsx'
import { CodeBuildConsole } from './CodeBuildConsole.tsx'
import { CodePipelineConsole } from './CodePipelineConsole.tsx'
import { EksConsole } from './EksConsole.tsx'
import { LiteServiceConsole } from './LiteServiceConsole.tsx'

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

const pf = (key: unknown[], url: string) => ({ key, fn: () => request(url) })

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  const store = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  })
  fetchMock.mockImplementation((url: string) => {
    const u = String(url)
    if (u.includes('/api/v1/aws/s3/buckets')) return Promise.resolve(okResponse({ buckets: [{ name: 'app-data', created_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/api/v1/aws/iam/roles')) return Promise.resolve(okResponse({ roles: [{ id: 'r1', name: 'reader', arn: 'arn:cloudforge:iam::local:role/reader', trust_policy: { Version: '2012-10-17', Statement: [{ Effect: 'Allow', Principal: { Service: 'ec2.amazonaws.com' }, Action: 'sts:AssumeRole' }] }, max_session_duration_seconds: 3600, policies: [], created_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/api/v1/aws/iam/users')) return Promise.resolve(okResponse({ users: [] }))
    if (u.includes('/api/v1/aws/iam/groups')) return Promise.resolve(okResponse({ groups: [] }))
    if (u.includes('/api/v1/aws/iam/policies')) return Promise.resolve(okResponse({ policies: [] }))
    if (u.includes('/api/v1/aws/lambda/functions')) return Promise.resolve(okResponse({ functions: [{ id: 'f1', name: 'echoer', runtime: 'python3', created_at: '2026-11-22T00:00:00Z', updated_at: '2026-11-22T01:00:00Z' }] }))
    if (u.includes('/api/v1/aws/elb/load-balancers')) return Promise.resolve(okResponse({ load_balancers: [{ id: 'lb1', name: 'web-lb', status: 'active', scheme: 'internet-facing', dns_name: 'web-lb.cf-local-1.elb.local', created_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/api/v1/aws/elb/target-groups')) return Promise.resolve(okResponse({ target_groups: [] }))
    if (u.includes('/api/v1/aws/costexplorer/costs')) return Promise.resolve(okResponse({ lines: [{ service: 'EC2', name: 'labs (Micro)', usage: 10, unit: 'lab-hour', cost_usd: 0.1 }, { service: 'S3', name: 'buckets', usage: 5, unit: 'GB-month', cost_usd: 0.115 }], total_usd: 0.215, currency: 'USD', note: 'Teaching rates apply.' }))
    if (u.includes('/api/v1/aws/costexplorer/budgets')) return Promise.resolve(okResponse({ budgets: [{ id: 'b1', name: 'monthly', limit_usd: 10, current_cost_usd: 0.215, usage_percent: 2.15, alert: false }] }))
    if (u.includes('/api/v1/aws/transit-gateways')) return Promise.resolve(okResponse({ transit_gateways: [{ id: 'tgw-1', name: 'hub', status: 'AVAILABLE', attachment_count: 1, attachments: [{ id: 'att-1', vpc_resource_id: 'vpc-a', status: 'AVAILABLE' }] }] }))
    if (u.includes('/api/v1/services/cloudfront')) return Promise.resolve(okResponse({ id: 'cloudfront', name: 'CloudFront', category: 'Networking', description: 'Content delivery.', icon: 'globe', route: '/services/cloudfront', availability: 'unavailable', fidelity: 'UNSUPPORTED', operations: [], resource_types: ['distribution'], docs_url: '/learn', learning_exercises: ['aws-saa'] }))
    if (u.includes('/api/v1/explorer/resources')) return Promise.resolve(okResponse({ resources: [], total: 0, limit: 50, offset: 0, region: 'cf-local-1', region_scoped: false }))
    if (u.includes('/api/v1/aws/eks/info')) return Promise.resolve(okResponse({ decision: 'UNSUPPORTED', reason: 'No cluster exists.', learning_path: [] }))
    if (u.includes('/api/v1/aws/cloudformation/stacks')) return Promise.resolve(okResponse({ stacks: [{ id: 's1', name: 'web', status: 'CREATE_COMPLETE', created_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/api/v1/aws/codebuild/projects')) return Promise.resolve(okResponse({ projects: [{ id: 'p1', name: 'web-build', source_path: '/repo', created_at: '2026-11-22T00:00:00Z', updated_at: '2026-11-22T01:00:00Z' }] }))
    if (u.includes('/api/v1/aws/codebuild/builds')) return Promise.resolve(okResponse({ builds: [{ id: 'b1', project_name: 'web-build', status: 'SUCCEEDED', created_at: '2026-11-22T02:00:00Z' }] }))
    if (u.includes('/api/v1/aws/codepipeline/pipelines')) return Promise.resolve(okResponse({ pipelines: [{ id: 'pl1', name: 'web-pipe', stages: [{ name: 'Source', type: 'source' }], created_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/api/v1/aws/codepipeline/executions')) return Promise.resolve(okResponse({ executions: [{ id: 'e1', pipeline_name: 'web-pipe', status: 'SUCCEEDED', created_at: '2026-11-22T03:00:00Z' }] }))
    if (u.includes('/api/v1/labs/lab-1/resources')) return Promise.resolve(okResponse({ resources: [] }))
    if (u.includes('/api/v1/labs')) return Promise.resolve(okResponse({ labs: [{ id: 'lab-1', name: 'lab-1', status: 'READY' }] }))
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('Survey table columns', () => {
  it('S3 Buckets uses Name / Region / IAM Access Analyzer / Creation date', async () => {
    const html = await renderSettled(<S3Console initialSection="buckets" />, '/services/s3', [
      { key: ['s3-buckets'], fn: fetchS3Buckets },
    ])
    expect(html).toContain('app-data')
    expect(html).toContain('IAM Access Analyzer')
    expect(html).toContain('Creation date')
    expect(html).toContain('Not available')
  })

  it('IAM Roles uses Role name / Trusted entities (from the trust policy) / Last activity', async () => {
    const html = await renderSettled(<IamConsole initialSection="roles" />, '/services/iam', [
      { key: ['iam-users'], fn: fetchIamUsers },
      { key: ['iam-groups'], fn: fetchIamGroups },
      { key: ['iam-roles'], fn: fetchIamRoles },
      { key: ['iam-policies'], fn: fetchIamPolicies },
    ])
    expect(html).toContain('reader')
    expect(html).toContain('Trusted entities')
    expect(html).toContain('ec2.amazonaws.com')
    expect(html).toContain('Last activity')
  })

  it('Lambda Functions uses Function name / Description / Runtime / Last modified', async () => {
    const html = await renderSettled(<LambdaConsole initialSection="functions" />, '/services/lambda', [
      pf(['lambda-functions'], '/api/v1/aws/lambda/functions'),
    ])
    expect(html).toContain('echoer')
    expect(html).toContain('Description')
    expect(html).toContain('Runtime')
    expect(html).toContain('Last modified')
  })

  it('Load Balancers uses the survey columns incl. Type, VPC ID, Availability Zones, ARN, Date created', async () => {
    const html = await renderSettled(<ElbListContent />, '/services/ec2', [
      pf(['elb-lbs'], '/api/v1/aws/elb/load-balancers'),
      pf(['elb-tgs'], '/api/v1/aws/elb/target-groups'),
    ])
    expect(html).toContain('web-lb')
    expect(html).toContain('IP address type')
    expect(html).toContain('VPC ID')
    expect(html).toContain('Availability Zones')
    expect(html).toContain('Security groups')
    expect(html).toContain('Date created')
  })

  it('Cost Explorer breakdown groups by Service with Total cost and % of total cost; Budgets uses the survey columns', async () => {
    const html = await renderSettled(<CostExplorerConsole initialSection="list" />, '/services/costexplorer', [
      pf(['costs'], '/api/v1/aws/costexplorer/costs'),
      pf(['cost-budgets'], '/api/v1/aws/costexplorer/budgets'),
    ])
    expect(html).toContain('Total cost')
    expect(html).toContain('% of total cost')
    expect(html).toContain('monthly')
    expect(html).toContain('Thresholds')
    expect(html).toContain('Amount used')
    expect(html).toContain('Forecasted amount')
    expect(html).toContain('Current vs. budgeted')
    expect(html).toContain('Forecasted vs. budgeted')
  })

  it('Transit Gateway Attachments uses Name / attachment ID / transit gateway ID / State', async () => {
    const html = await renderSettled(<TransitGatewayListContent />, '/services/vpc', [
      pf(['tgws'], '/api/v1/aws/transit-gateways'),
    ])
    expect(html).toContain('Transit gateway attachment ID')
    expect(html).toContain('Transit gateway ID')
    expect(html).toContain('att-1')
  })

  it('CloudFormation Stacks uses Stack name / Status / Created time / Description', async () => {
    const html = await renderSettled(<CloudFormationConsole />, '/services/cloudformation', [
      pf(['cfn-stacks'], '/api/v1/aws/cloudformation/stacks'),
    ])
    expect(html).toContain('web')
    expect(html).toContain('Created time')
    expect(html).toContain('Description')
  })

  it('CodeBuild Projects uses Name / Source provider / Repository / Latest build status / Description / Last Modified', async () => {
    const html = await renderSettled(<CodeBuildConsole />, '/services/codebuild', [
      pf(['codebuild-projects'], '/api/v1/aws/codebuild/projects'),
      pf(['codebuild-builds'], '/api/v1/aws/codebuild/builds'),
    ])
    expect(html).toContain('web-build')
    expect(html).toContain('Source provider')
    expect(html).toContain('Repository')
    expect(html).toContain('Latest build status')
    expect(html).toContain('SUCCEEDED')
    expect(html).toContain('Last Modified')
  })

  it('CodePipeline Pipelines uses Name / Latest execution status / Latest source revisions / Latest execution started / Most recent executions', async () => {
    const html = await renderSettled(<CodePipelineConsole />, '/services/codepipeline', [
      pf(['codepipeline-pipelines'], '/api/v1/aws/codepipeline/pipelines'),
      pf(['codepipeline-executions'], '/api/v1/aws/codepipeline/executions'),
    ])
    expect(html).toContain('web-pipe')
    expect(html).toContain('Latest execution status')
    expect(html).toContain('Latest source revisions')
    expect(html).toContain('Latest execution started')
    expect(html).toContain('Most recent executions')
    expect(html).toContain('SUCCEEDED')
  })

  it('EKS shows the survey Clusters table, empty because EKS is UNSUPPORTED', async () => {
    const html = await renderSettled(<EksConsole />, '/services/eks', [
      pf(['eks-info'], '/api/v1/aws/eks/info'),
    ])
    expect(html).toContain('Clusters (0)')
    expect(html).toContain('Cluster name')
    expect(html).toContain('Kubernetes version')
    expect(html).toContain('Support period')
    expect(html).toContain('No clusters — EKS is UNSUPPORTED')
  })

  it('CloudFront shows the survey Distributions table, empty because CloudFront is UNSUPPORTED', async () => {
    const html = await renderSettled(
      <Routes>
        <Route path="/services/:id" element={<LiteServiceConsole initialSection="distributions" />} />
      </Routes>,
      '/services/cloudfront',
      [pf(['service', 'cloudfront'], '/api/v1/services/cloudfront')],
    )
    expect(html).toContain('Distributions')
    expect(html).toContain('Domain name')
    expect(html).toContain('Pricing plan')
    expect(html).toContain('No distributions — CloudFront is UNSUPPORTED')
  })
})

describe('Survey grouping (sections, not standalone consoles)', () => {
  it('EC2 ?section=volumes renders the EBS Volumes section with the survey columns', async () => {
    const html = await renderSettled(<Ec2Console initialSection="home" />, '/services/ec2?section=volumes', [])
    expect(html).toContain('Volumes (0)')
    expect(html).toContain('Volume ID')
    expect(html).toContain('Availability Zone')
    expect(html).toContain('No volumes — EBS is UNSUPPORTED')
  })

  it('EC2 ?section=autoscaling renders the Auto Scaling empty state with a disabled Create button', async () => {
    const html = await renderSettled(<Ec2Console initialSection="home" />, '/services/ec2?section=autoscaling', [])
    expect(html).toContain('Auto Scaling Groups')
    expect(html).toContain('Create Auto Scaling group')
    expect(html).toContain('Auto Scaling is')
    expect(html).toContain('UNSUPPORTED')
  })

  it('EC2 ?section=load-balancers renders the ELB list inside EC2', async () => {
    const html = await renderSettled(<Ec2Console initialSection="home" />, '/services/ec2?section=load-balancers', [
      pf(['elb-lbs'], '/api/v1/aws/elb/load-balancers'),
      pf(['elb-tgs'], '/api/v1/aws/elb/target-groups'),
    ])
    expect(html).toContain('Load Balancing section')
    expect(html).toContain('web-lb')
  })

  it('VPC ?section=vpn renders the VPN connections section with the survey columns', async () => {
    const html = await renderSettled(<VpcConsole />, '/services/vpc?section=vpn', [])
    expect(html).toContain('VPN connections (0)')
    expect(html).toContain('VPN ID')
    expect(html).toContain('Customer gateway')
    expect(html).toContain('No VPN connections — VPN is UNSUPPORTED')
  })

  it('VPC ?section=tgw renders the Transit Gateway list inside VPC', async () => {
    const html = await renderSettled(<VpcConsole />, '/services/vpc?section=tgw', [
      pf(['tgws'], '/api/v1/aws/transit-gateways'),
    ])
    expect(html).toContain('Transit Gateways section')
    expect(html).toContain('hub')
    expect(html).toContain('Transit gateway attachment ID')
  })
})
