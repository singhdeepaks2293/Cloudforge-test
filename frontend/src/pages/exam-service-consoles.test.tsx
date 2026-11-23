/**
 * Render tests for the 17 dedicated exam-service consoles (audit gap
 * #3): one render test per console over the shared ExamServiceConsole
 * builder, plus targeted tests for the survey-recorded tabs (Backup
 * nav, CloudFront nav + Distributions columns, EFS Access points)
 * and the secondary lists (Backup vaults/jobs, Artifact reports,
 * Macie findings, CloudFront invalidations). fetch + localStorage
 * are stubbed; React Query runs with retries off so the render
 * settles. Rows come from stubbed API-shaped data — nothing is
 * invented by the components under test.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { request } from '../services/api.ts'
import {
  ArtifactConsole,
  AuditManagerConsole,
  AuroraConsole,
  BackupConsole,
  CloudFrontConsole,
  ComputeOptimizerConsole,
  ControlTowerConsole,
  DirectConnectConsole,
  EfsConsole,
  FsxConsole,
  GlobalAcceleratorConsole,
  IdentityCenterConsole,
  MacieConsole,
  MgnConsole,
  RamConsole,
  SavingsPlansConsole,
  ShieldConsole,
} from './ExamServiceConsoles.tsx'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

async function renderSettled(
  el: React.ReactElement,
  prefetches: { key: unknown[]; fn: () => Promise<unknown> }[],
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  for (const p of prefetches) await client.prefetchQuery({ queryKey: p.key, queryFn: p.fn })
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/']}>{el}</MemoryRouter>
    </QueryClientProvider>,
  )
}

const pf = (key: unknown[], url: string) => ({ key, fn: () => request(url) })
const ts = '2026-11-22T00:00:00Z'

const serviceBody = (id: string, name: string, fidelity: string, kind: string) => ({
  id,
  name,
  category: 'Databases',
  description: `${name} — exam service.`,
  icon: 'database',
  route: `/services/${id}`,
  availability: 'available',
  fidelity,
  operations: [],
  resource_types: [kind],
  docs_url: '/learn',
  learning_exercises: ['aws-saa', 'aws-sap'],
})

const LISTS: Record<string, unknown> = {
  '/api/v1/aws/aurora/clusters': {
    clusters: [
      {
        id: 'cl-1', name: 'app-cluster', engine: 'aurora-postgresql', status: 'available', instance_count: 2,
        instances: [
          { id: 'app-cluster-1', role: 'writer', status: 'available' },
          { id: 'app-cluster-2', role: 'reader', status: 'available' },
        ],
        writer_endpoint: 'app-cluster.cluster-cf-local-1.cf-local',
        reader_endpoint: 'app-cluster.cluster-ro-cf-local-1.cf-local',
        created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/backup/plans': {
    plans: [{ id: 'bp-1', name: 'nightly', schedule: '0 2 * * *', retention_days: 30, resources: [], status: 'ACTIVE', created_at: ts, updated_at: ts }],
    count: 1,
  },
  '/api/v1/aws/backup/vaults': {
    vaults: [{ id: 'bv-1', name: 'default-vault', status: 'ACTIVE', created_at: ts, updated_at: ts }],
    count: 1,
  },
  '/api/v1/aws/backup/jobs': {
    jobs: [
      {
        id: 'bj-1', name: 'backup-nightly-abc12345', plan_id: 'bp-1', plan_name: 'nightly', status: 'COMPLETED',
        size_bytes: 123456, sha256: 'aabbccddeeff00112233', started_at: ts, completed_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/macie/jobs': {
    jobs: [
      {
        id: 'mj-1', name: 'weekly-scan', status: 'COMPLETED', findings_created: 0,
        last_run: { completed_at: ts, scanned: { s3_buckets: 0, dynamodb_tables: 2, named_scope: 0 }, findings_created: 0 },
        created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/macie/findings': {
    findings: [
      {
        id: 'mf-1', title: 'Sensitive-data pattern in DynamoDB table pii-users', severity: 'MEDIUM',
        resource_type: 'dynamodb-table', resource_id: 'pii-users', created_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/auditmanager/assessments': {
    assessments: [
      {
        id: 'am-1', name: 'q4-audit', framework: 'CIS AWS Foundations Benchmark', status: 'ACTIVE',
        evidence: [{ collected_at: ts, entries_count: 42, note: '' }], created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/artifact/agreements': {
    agreements: [{ id: 'aa-1', name: 'soc2-agreement', report: 'SOC 2 Type II', status: 'PENDING', created_at: ts, updated_at: ts }],
    count: 1,
  },
  '/api/v1/aws/artifact/reports': {
    reports: [
      { name: 'SOC 2 Type II', category: 'Security', source: 'AWS Artifact — download from the AWS Management Console' },
      { name: 'ISO 27001', category: 'Security', source: 'AWS Artifact — download from the AWS Management Console' },
    ],
    count: 2,
    note: 'Catalogue only: CloudForge does not serve AWS compliance documents. Download the report itself from AWS Artifact.',
  },
  '/api/v1/aws/cloudfront/distributions': {
    distributions: [
      {
        id: 'd-1', name: 'web-dist', status: 'Deployed', origin: 'app-bucket.s3.cf-local',
        domain_name: 'web-dist.cloudfront.cf-local', description: 'Web distribution', created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/cloudfront/distributions/d-1/invalidations': {
    invalidations: [{ id: 'I12345678', paths: ['/*'], status: 'COMPLETED', created_at: ts, completed_at: ts }],
    count: 1,
  },
  '/api/v1/aws/efs/file-systems': {
    'file-systems': [
      {
        id: 'fs-1', name: 'shared-fs', performance_mode: 'generalPurpose', throughput_mode: 'bursting',
        encrypted: true, mount_targets: [], status: 'available', created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/fsx/file-systems': {
    'file-systems': [
      {
        id: 'fx-1', name: 'lustre-fs', file_system_type: 'LUSTRE', storage_capacity_gb: 1200,
        throughput_mbps: 128, mount_targets: [], status: 'AVAILABLE', created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/shield/protections': {
    protections: [
      { id: 'sp-1', name: 'web-protection', resource_type: 'cloudfront-distribution', resource_id: 'web-dist', status: 'ACTIVE', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/controltower/landing-zones': {
    'landing-zones': [
      { id: 'lz-1', name: 'main-lz', home_region: 'cf-local-1', version: '3.3', guardrails: [], status: 'ACTIVE', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/identitycenter/permission-sets': {
    'permission-sets': [
      { id: 'ps-1', name: 'ReadOnly', session_duration_seconds: 3600, managed_policies: [], assignments: [], status: 'ACTIVE', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/ram/resource-shares': {
    'resource-shares': [
      { id: 'rs-1', name: 'shared-subnet', resource_type: 'subnet', principals: [], allow_external_principals: false, status: 'ACTIVE', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/directconnect/connections': {
    connections: [
      { id: 'dx-1', name: 'dx-main', location: 'EqDC2 (teaching)', bandwidth: '1Gbps', status: 'REQUESTED', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/globalaccelerator/accelerators': {
    accelerators: [
      {
        id: 'ga-1', name: 'ga-web', static_ips: ['192.0.2.10', '192.0.2.20'], active_region: 'cf-local-1',
        endpoint_groups: [{ region: 'cf-local-1', endpoints: ['elb-1'] }], ip_address_type: 'IPv4', status: 'ACTIVE',
        created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/mgn/source-servers': {
    'source-servers': [
      { id: 'ss-1', name: 'web-01', source: '10.0.1.15', os: 'linux', replication_state: 'NOT_STARTED', status: 'NOT_STARTED', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
  '/api/v1/aws/computeoptimizer/recommendations': {
    recommendations: [
      {
        id: 'rec-1', name: 'rec-lab-1-downsize-profile',
        title: 'Consider the Micro profile for lab lab-1 (currently Small)', recommendation_type: 'downsize-profile',
        current_profile: 'Small', recommended_profile: 'Micro', estimated_monthly_savings_usd: 7.3, confidence: 'LOW',
        basis: 'Lab state only (profile and state) — no CloudWatch utilization metrics exist in this deployment, so this is not measured rightsizing.',
        status: 'ACTIVE', created_at: ts, updated_at: ts,
      },
    ],
    count: 1,
  },
  '/api/v1/aws/savingsplans/plans': {
    plans: [
      { id: 'sp-1', name: 'compute-1yr', plan_type: 'COMPUTE', term: 'ONE_YEAR', payment: 'NO_UPFRONT', commitment_usd_per_hour: 0.05, status: 'ACTIVE', created_at: ts, updated_at: ts },
    ],
    count: 1,
  },
}

const SERVICES: Record<string, unknown> = {
  aurora: serviceBody('aurora', 'Aurora', 'API_EMULATED', 'cluster'),
  backup: serviceBody('backup', 'Backup', 'REAL', 'backup-plan'),
  macie: serviceBody('macie', 'Macie', 'API_EMULATED', 'classification-job'),
  auditmanager: serviceBody('auditmanager', 'Audit Manager', 'API_EMULATED', 'assessment'),
  artifact: serviceBody('artifact', 'Artifact', 'API_EMULATED', 'agreement'),
  cloudfront: serviceBody('cloudfront', 'CloudFront', 'API_EMULATED', 'distribution'),
  efs: serviceBody('efs', 'EFS', 'API_EMULATED', 'file-system'),
  fsx: serviceBody('fsx', 'FSx', 'API_EMULATED', 'file-system'),
  shield: serviceBody('shield', 'Shield', 'API_EMULATED', 'protection'),
  controltower: serviceBody('controltower', 'Control Tower', 'API_EMULATED', 'landing-zone'),
  identitycenter: serviceBody('identitycenter', 'IAM Identity Center', 'API_EMULATED', 'permission-set'),
  ram: serviceBody('ram', 'Resource Access Manager', 'API_EMULATED', 'resource-share'),
  directconnect: serviceBody('directconnect', 'Direct Connect', 'API_EMULATED', 'connection'),
  globalaccelerator: serviceBody('globalaccelerator', 'Global Accelerator', 'API_EMULATED', 'accelerator'),
  mgn: serviceBody('mgn', 'Application Migration', 'API_EMULATED', 'source-server'),
  computeoptimizer: serviceBody('computeoptimizer', 'Compute Optimizer', 'API_EMULATED', 'recommendation'),
  savingsplans: serviceBody('savingsplans', 'Savings Plans', 'API_EMULATED', 'savings-plan'),
}

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
    if (u.includes('/api/v1/aws/cloudfront/distributions/d-1/invalidations')) {
      return Promise.resolve(okResponse(LISTS['/api/v1/aws/cloudfront/distributions/d-1/invalidations']))
    }
    for (const [path, body] of Object.entries(LISTS)) {
      if (u.includes(path)) return Promise.resolve(okResponse(body))
    }
    const svc = u.match(/\/api\/v1\/services\/([a-z]+)/)?.[1]
    if (svc !== undefined && SERVICES[svc] !== undefined) return Promise.resolve(okResponse(SERVICES[svc]))
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('Dedicated exam-service consoles — one render test per console', () => {
  const cases: {
    id: string
    listPath: string
    el: React.ReactElement
    row: string
    column: string
    createLabel: string
    listLabel: string
  }[] = [
    { id: 'aurora', listPath: 'aurora/clusters', el: <AuroraConsole />, row: 'app-cluster', column: 'Writer endpoint', createLabel: 'Create cluster', listLabel: 'Clusters' },
    { id: 'backup', listPath: 'backup/plans', el: <BackupConsole initialSection="plans" />, row: 'nightly', column: 'Retention (days)', createLabel: 'Create backup plan', listLabel: 'Backup plans' },
    { id: 'macie', listPath: 'macie/jobs', el: <MacieConsole />, row: 'weekly-scan', column: 'Findings created', createLabel: 'Create classification job', listLabel: 'Classification jobs' },
    { id: 'auditmanager', listPath: 'auditmanager/assessments', el: <AuditManagerConsole />, row: 'q4-audit', column: 'Evidence entries', createLabel: 'Create assessment', listLabel: 'Assessments' },
    { id: 'artifact', listPath: 'artifact/agreements', el: <ArtifactConsole />, row: 'soc2-agreement', column: 'Report', createLabel: 'Create agreement', listLabel: 'Agreements' },
    { id: 'cloudfront', listPath: 'cloudfront/distributions', el: <CloudFrontConsole />, row: 'web-dist', column: 'Alternate domain names', createLabel: 'Create distribution', listLabel: 'Distributions' },
    { id: 'efs', listPath: 'efs/file-systems', el: <EfsConsole initialSection="file-systems" />, row: 'shared-fs', column: 'Encrypted', createLabel: 'Create file system', listLabel: 'File systems' },
    { id: 'fsx', listPath: 'fsx/file-systems', el: <FsxConsole />, row: 'lustre-fs', column: 'Throughput (Mbps)', createLabel: 'Create file system', listLabel: 'File systems' },
    { id: 'shield', listPath: 'shield/protections', el: <ShieldConsole />, row: 'web-protection', column: 'Resource type', createLabel: 'Create protection', listLabel: 'Protections' },
    { id: 'controltower', listPath: 'controltower/landing-zones', el: <ControlTowerConsole />, row: 'main-lz', column: 'Guardrails', createLabel: 'Create landing zone', listLabel: 'Landing zones' },
    { id: 'identitycenter', listPath: 'identitycenter/permission-sets', el: <IdentityCenterConsole />, row: 'ReadOnly', column: 'Session duration', createLabel: 'Create permission set', listLabel: 'Permission sets' },
    { id: 'ram', listPath: 'ram/resource-shares', el: <RamConsole />, row: 'shared-subnet', column: 'Allow external principals', createLabel: 'Create resource share', listLabel: 'Resource shares' },
    { id: 'directconnect', listPath: 'directconnect/connections', el: <DirectConnectConsole />, row: 'dx-main', column: 'Bandwidth', createLabel: 'Create connection', listLabel: 'Connections' },
    { id: 'globalaccelerator', listPath: 'globalaccelerator/accelerators', el: <GlobalAcceleratorConsole />, row: 'ga-web', column: 'Static IPs', createLabel: 'Create accelerator', listLabel: 'Accelerators' },
    { id: 'mgn', listPath: 'mgn/source-servers', el: <MgnConsole />, row: 'web-01', column: 'Replication state', createLabel: 'Register source server', listLabel: 'Source servers' },
    { id: 'computeoptimizer', listPath: 'computeoptimizer/recommendations', el: <ComputeOptimizerConsole />, row: 'Consider the Micro profile for lab lab-1', column: 'Est. monthly savings', createLabel: 'Create recommendation', listLabel: 'Recommendations' },
    { id: 'savingsplans', listPath: 'savingsplans/plans', el: <SavingsPlansConsole />, row: 'compute-1yr', column: 'Commitment ($/hr)', createLabel: 'Create savings plan', listLabel: 'Savings plans' },
  ]
  it.each(cases)('$id renders its real list ($listLabel) with the row and create action', async (c) => {
    const html = await renderSettled(c.el, [
      pf(['exam-list', c.listPath], `/api/v1/aws/${c.listPath}`),
      pf(['service', c.id], `/api/v1/services/${c.id}`),
    ])
    expect(html).toContain(c.listLabel)
    expect(html).toContain(c.row)
    expect(html).toContain(c.column)
    expect(html).toContain(c.createLabel)
    expect(html).toContain('Operations')
    expect(html).toContain('Learning')
  })
})

describe('Home dashboards', () => {
  it('Aurora Home shows Resources by Region with a live cluster count and an RDS alternative', async () => {
    const html = await renderSettled(<AuroraConsole initialSection="home" />, [
      pf(['exam-list', 'aurora/clusters'], '/api/v1/aws/aurora/clusters'),
      pf(['service', 'aurora'], '/api/v1/services/aurora'),
    ])
    expect(html).toContain('Resources by Region')
    expect(html).toContain('Clusters')
    expect(html).toContain('RDS — DB instances')
    expect(html).toContain('Service Health')
  })

  it('Backup Home names the host scripts and the real pg_dump run', async () => {
    const html = await renderSettled(<BackupConsole />, [
      pf(['exam-list', 'backup/plans'], '/api/v1/aws/backup/plans'),
      pf(['service', 'backup'], '/api/v1/services/backup'),
    ])
    expect(html).toContain('Backup scripts (host-side)')
    expect(html).toContain('scripts/backup.sh')
    expect(html).toContain('scripts/restore.sh')
    expect(html).toContain('real pg_dump')
  })
})

describe('Survey-recorded tabs', () => {
  it('CloudFront shows all 10 survey nav items and the exact Distributions columns', async () => {
    const html = await renderSettled(<CloudFrontConsole />, [
      pf(['exam-list', 'cloudfront/distributions'], '/api/v1/aws/cloudfront/distributions'),
      pf(['service', 'cloudfront'], '/api/v1/services/cloudfront'),
    ])
    for (const label of [
      'Distributions', 'Cache statistics', 'Origin access', 'Functions', 'Key groups', 'Public keys',
      'Field-level encryption', 'Invalidations', 'Reports &amp; analytics', 'What&#x27;s new',
    ]) {
      expect(html).toContain(label)
    }
    for (const col of ['ID', 'Status', 'Description', 'Type', 'Domain name', 'Alternate domain names', 'Origins', 'Pricing plan', 'Last modified']) {
      expect(html).toContain(col)
    }
    expect(html).toContain('web-dist.cloudfront.cf-local')
  })

  it('CloudFront Cache statistics tab is UNSUPPORTED with a disabled button and a reason', async () => {
    const html = await renderSettled(<CloudFrontConsole initialSection="cache-statistics" />, [
      pf(['exam-list', 'cloudfront/distributions'], '/api/v1/aws/cloudfront/distributions'),
      pf(['service', 'cloudfront'], '/api/v1/services/cloudfront'),
    ])
    expect(html).toContain('Cache statistics are not built')
    expect(html).toContain('no edge location caches anything')
    expect(html).toContain('disabled')
  })

  it('Backup shows the survey nav groups and vault locks as UNSUPPORTED', async () => {
    const html = await renderSettled(<BackupConsole initialSection="vault-locks" />, [
      pf(['exam-list', 'backup/plans'], '/api/v1/aws/backup/plans'),
      pf(['service', 'backup'], '/api/v1/services/backup'),
    ])
    expect(html).toContain('Backup vault locks are not built')
    expect(html).toContain('My account')
    expect(html).toContain('External resources')
    expect(html).toContain('Backup Audit Manager')
    expect(html).toContain('Jobs dashboard')
  })

  it('EFS shows the survey Access points tab as UNSUPPORTED', async () => {
    const html = await renderSettled(<EfsConsole initialSection="access-points" />, [
      pf(['exam-list', 'efs/file-systems'], '/api/v1/aws/efs/file-systems'),
      pf(['service', 'efs'], '/api/v1/services/efs'),
    ])
    expect(html).toContain('EFS access points are not built')
    expect(html).toContain('File systems')
  })
})

describe('Secondary lists', () => {
  it('Backup Vaults tab lists real vaults', async () => {
    const html = await renderSettled(<BackupConsole initialSection="vaults" />, [
      pf(['exam-list', 'backup/plans'], '/api/v1/aws/backup/plans'),
      pf(['exam-list', 'backup/vaults'], '/api/v1/aws/backup/vaults'),
      pf(['service', 'backup'], '/api/v1/services/backup'),
    ])
    expect(html).toContain('default-vault')
    expect(html).toContain('Create vault')
  })

  it('Backup Jobs tab lists real pg_dump jobs', async () => {
    const html = await renderSettled(<BackupConsole initialSection="jobs" />, [
      pf(['exam-list', 'backup/plans'], '/api/v1/aws/backup/plans'),
      pf(['backup-jobs'], '/api/v1/aws/backup/jobs'),
      pf(['service', 'backup'], '/api/v1/services/backup'),
    ])
    expect(html).toContain('backup-nightly-abc12345')
    expect(html).toContain('COMPLETED')
    expect(html).toContain('aabbccddeeff0011')
  })

  it('Artifact Reports tab shows the catalogue, not redistributed documents', async () => {
    const html = await renderSettled(<ArtifactConsole initialSection="reports" />, [
      pf(['exam-list', 'artifact/agreements'], '/api/v1/aws/artifact/agreements'),
      pf(['artifact-reports'], '/api/v1/aws/artifact/reports'),
      pf(['service', 'artifact'], '/api/v1/services/artifact'),
    ])
    expect(html).toContain('SOC 2 Type II')
    expect(html).toContain('ISO 27001')
    expect(html).toContain('does not redistribute')
  })

  it('Macie Findings tab lists real findings from classification runs', async () => {
    const html = await renderSettled(<MacieConsole initialSection="findings" />, [
      pf(['exam-list', 'macie/jobs'], '/api/v1/aws/macie/jobs'),
      pf(['macie-findings'], '/api/v1/aws/macie/findings'),
      pf(['service', 'macie'], '/api/v1/services/macie'),
    ])
    expect(html).toContain('Sensitive-data pattern in DynamoDB table pii-users')
    expect(html).toContain('MEDIUM')
  })

  it('CloudFront Invalidations tab lists invalidations for the selected distribution', async () => {
    const html = await renderSettled(<CloudFrontConsole initialSection="invalidations" />, [
      pf(['exam-list', 'cloudfront/distributions'], '/api/v1/aws/cloudfront/distributions'),
      pf(['cf-invalidations', 'd-1'], '/api/v1/aws/cloudfront/distributions/d-1/invalidations'),
      pf(['service', 'cloudfront'], '/api/v1/services/cloudfront'),
    ])
    expect(html).toContain('I12345678')
    expect(html).toContain('COMPLETED')
    expect(html).toContain('Create invalidation')
  })

  it('Compute Optimizer has no create API — the button is disabled and Refresh is offered', async () => {
    const html = await renderSettled(<ComputeOptimizerConsole />, [
      pf(['exam-list', 'computeoptimizer/recommendations'], '/api/v1/aws/computeoptimizer/recommendations'),
      pf(['service', 'computeoptimizer'], '/api/v1/services/computeoptimizer'),
    ])
    expect(html).toContain('Refresh recommendations')
    expect(html).toContain('no create API')
    expect(html).toContain('$7.30')
  })
})
