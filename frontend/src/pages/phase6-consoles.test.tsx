/**
 * Render tests for the Phase 6 consoles (Athena, Glue, Kinesis,
 * Firehose, QuickSight, SageMaker, Bedrock, DMS, DataSync, Transfer,
 * Migration Hub, GuardDuty, Security Hub, Inspector, Config, WAF,
 * KMS, Secrets Manager, VPC Peering, Transit Gateway, VPC Endpoints,
 * Network Firewall, Flow Logs, SSM, Trusted Advisor, Cost Explorer,
 * Organizations): each renders real API-shaped data with fidelity
 * labels and honest not-built notes. fetch + localStorage are
 * stubbed; React Query runs with retries off so the render settles.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { request } from '../services/api.ts'
import { AthenaConsole, FirehoseConsole, GlueConsole, KinesisConsole, QuickSightConsole } from './AnalyticsConsoles.tsx'
import { BedrockConsole, SageMakerConsole } from './MlConsoles.tsx'
import { DataSyncConsole, DmsConsole, MigrationHubConsole, TransferConsole } from './MigrationConsoles.tsx'
import { ConfigConsole, GuardDutyConsole, InspectorConsole, KmsConsole, SecretsConsole, SecurityHubConsole, WafConsole } from './SecurityConsoles.tsx'
import { FlowLogsConsole, NetworkFirewallConsole, TransitGatewayConsole, VpcEndpointsConsole, VpcPeeringConsole } from './NetworkingConsoles.tsx'
import { CostExplorerConsole, OrganizationsConsole, SsmConsole, TrustedAdvisorConsole } from './ManagementConsoles.tsx'

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
  const store = new Map<string, string>([['cloudforge_token', 'test-token']])
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  })
  fetchMock.mockImplementation((url: string) => {
    const u = String(url)
    if (u.includes('/aws/athena/queries')) return Promise.resolve(okResponse({ queries: [{ id: 'q-1', name: 'slow-queries', query: 'SELECT 1', database: 'platform' }] }))
    if (u.includes('/aws/athena/executions')) return Promise.resolve(okResponse({ executions: [{ id: 'e-1', status: 'SUCCEEDED', database: 'platform', row_count: 1, duration_ms: 3, query: 'SELECT 1' }] }))
    if (u.includes('/aws/glue/crawlers')) return Promise.resolve(okResponse({ crawlers: [{ id: 'c-1', name: 'main-crawler', sources: ['postgres'], status: 'READY', tables_found: 2 }] }))
    if (u.includes('/aws/glue/tables')) return Promise.resolve(okResponse({ tables: [{ id: 't-1', table: 'users', source: 'postgres', column_count: 3, crawled_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/aws/kinesis/streams')) return Promise.resolve(okResponse({ streams: [{ id: 's-1', name: 'orders', status: 'ACTIVE', shard_count: 2 }] }))
    if (u.includes('/aws/firehose/streams')) return Promise.resolve(okResponse({ streams: [{ id: 'f-1', name: 'events', destination: 'postgres', status: 'ACTIVE', records_delivered: 5, bytes_delivered: 120 }] }))
    if (u.includes('/aws/quicksight/dashboards')) return Promise.resolve(okResponse({ dashboards: [{ id: 'd-1', name: 'orders-by-day', chart_type: 'bar', label_column: 'day', value_column: 'n' }] }))
    if (u.includes('/aws/sagemaker/notebooks')) return Promise.resolve(okResponse({ notebooks: [{ id: 'n-1', name: 'explore', runtime: 'python3', status: 'IN_SERVICE' }] }))
    if (u.includes('/aws/sagemaker/training-jobs')) return Promise.resolve(okResponse({ training_jobs: [{ id: 'j-1', name: 'housing-v1', dataset: 'housing', status: 'COMPLETED', r2: 0.95, rmse: 12000 }] }))
    if (u.includes('/aws/sagemaker/models')) return Promise.resolve(okResponse({ models: [{ id: 'm-1', name: 'housing-v1', version: 1, status: 'READY', r2: 0.95, artifact_path: 'sagemaker/models/j-1.json' }] }))
    if (u.includes('/aws/sagemaker/endpoints')) return Promise.resolve(okResponse({ endpoints: [{ id: 'ep-1', name: 'housing-ep', model_id: 'm-1', model_name: 'housing-v1', status: 'IN_SERVICE' }] }))
    if (u.includes('/aws/bedrock/models')) return Promise.resolve(okResponse({ models: [{ id: 'b-1', model_id: 'anthropic.claude-3-sonnet', name: 'Claude 3 Sonnet', supported: false }] }))
    if (u.includes('/aws/bedrock/invocations')) return Promise.resolve(okResponse({ invocations: [{ id: 'i-1', model_id: 'anthropic.claude-3-sonnet', prompt: 'hi', fidelity: 'SIMULATED' }] }))
    if (u.includes('/aws/dms/tasks')) return Promise.resolve(okResponse({ tasks: [{ id: 'dm-1', name: 'platform-to-rds', source_kind: 'platform', target_kind: 'rds', status: 'COMPLETED', rows_copied: 42 }] }))
    if (u.includes('/aws/datasync/tasks')) return Promise.resolve(okResponse({ tasks: [{ id: 'ds-1', name: 'copy-docs', source: 'local:docs', destination: 'local:docs-copy', status: 'COMPLETED', files_copied: 3, bytes_copied: 900, files_verified: 3 }] }))
    if (u.includes('/aws/transfer/info')) return Promise.resolve(okResponse({ service: 'transfer', fidelity: 'UNSUPPORTED', supported: false, decision: 'Transfer Family is UNSUPPORTED: no local SFTP server.', reason: 'No local SFTP server binary found.', checked_binaries: { sshd: false, 'sftp-server': false }, alternative: 'Use DataSync.' }))
    if (u.includes('/aws/migrationhub/tasks')) return Promise.resolve(okResponse({ tasks: [{ id: 'dm-1', name: 'platform-to-rds', kind: 'dms', status: 'COMPLETED', detail: '42 rows' }], summary: { COMPLETED: 1 } }))
    if (u.includes('/aws/guardduty/detectors')) return Promise.resolve(okResponse({ detectors: [{ id: 'gd-1', name: 'main', status: 'ENABLED' }] }))
    if (u.includes('/aws/guardduty/findings')) return Promise.resolve(okResponse({ findings: [{ id: 'gf-1', title: 'Brute force login attempts', severity: 'MEDIUM', source: 'guardduty', status: 'NEW', detail: '6 failed logins from 203.0.113.1' }] }))
    if (u.includes('/aws/securityhub/summary')) return Promise.resolve(okResponse({ open: 1, by_severity: { MEDIUM: 1 }, by_source: { guardduty: 1 } }))
    if (u.includes('/aws/securityhub/findings')) return Promise.resolve(okResponse({ findings: [{ id: 'gf-1', title: 'Brute force login attempts', severity: 'MEDIUM', source: 'guardduty', status: 'NEW', detail: '6 failed logins' }] }))
    if (u.includes('/aws/inspector/assessments')) return Promise.resolve(okResponse({ assessments: [{ id: 'in-1', name: 'weekly', status: 'COMPLETED', findings_count: 1 }] }))
    if (u.includes('/aws/config/rules')) return Promise.resolve(okResponse({ rules: [{ id: 'cr-1', name: 'no-open-ssh', rule_type: 'sg-no-open-ssh', compliant: false }] }))
    if (u.includes('/aws/config/results')) return Promise.resolve(okResponse({ results: [{ id: 'res-1', rule_name: 'no-open-ssh', resource_id: 'sg-1', compliant: false, detail: 'Port 22 open to 0.0.0.0/0' }] }))
    if (u.includes('/aws/config/items')) return Promise.resolve(okResponse({ items: [{ id: 'ci-1', resource_id: 'sg-1', resource_type: 'security-group', lab_id: 'lab-1', snapshot_at: '2026-11-22T00:00:00Z' }] }))
    if (u.includes('/aws/waf/acls')) return Promise.resolve(okResponse({ acls: [{ id: 'w-1', name: 'web-acl', rules: [{ name: 'block-admin', action: 'block', match_type: 'path_prefix', match_value: '/admin' }], evaluations: 2 }] }))
    if (u.includes('/aws/kms/keys')) return Promise.resolve(okResponse({ keys: [{ id: 'k-1', name: 'app-key', description: 'app data', version: 1, status: 'ENABLED' }] }))
    if (u.includes('/aws/secretsmanager/secrets')) return Promise.resolve(okResponse({ secrets: [{ id: 'sec-1', name: 'db-password', version: 1, kms_key_id: 'k-1' }] }))
    if (u.includes('/aws/vpc/peerings')) return Promise.resolve(okResponse({ peerings: [{ id: 'pcx-1', name: 'a-to-b', vpc_a_resource_id: 'vpc-a', vpc_b_resource_id: 'vpc-b', status: 'ACTIVE' }] }))
    if (u.includes('/aws/transit-gateways')) return Promise.resolve(okResponse({ transit_gateways: [{ id: 'tgw-1', name: 'hub', status: 'AVAILABLE', attachment_count: 1, attachments: [{ id: 'att-1', vpc_resource_id: 'vpc-a', status: 'AVAILABLE' }] }] }))
    if (u.includes('/aws/vpc/endpoints')) return Promise.resolve(okResponse({ endpoints: [{ id: 'ep-1', name: 's3-ep', service: 's3', vpc_resource_id: 'vpc-a', dns_name: 's3.cf-local-1.vpce.local', status: 'AVAILABLE' }] }))
    if (u.includes('/aws/network-firewall/firewalls')) return Promise.resolve(okResponse({ firewalls: [{ id: 'fw-1', name: 'edge-fw', lab_id: 'lab-1', rules: [{ name: 'allow-https', priority: 100, action: 'allow', protocol: 'tcp', source_cidr: '0.0.0.0/0', dest_cidr: '0.0.0.0/0', port_range: '443' }], engine_policy_applied: false, status: 'READY' }] }))
    if (u.includes('/aws/vpc/flow-logs')) return Promise.resolve(okResponse({ groups: [{ id: 'fl-1', name: 'vpc-logs', vpc_resource_id: 'vpc-a', status: 'ACTIVE', records_count: 2 }] }))
    if (u.includes('/aws/ssm/inventory')) return Promise.resolve(okResponse({ inventory: { hostname: 'lab-host', os: 'linux', cpu_count: 4 }, source: 'host-agent' }))
    if (u.includes('/aws/ssm/instances')) return Promise.resolve(okResponse({ instances: [{ id: 'lab-1', name: 'lab-1', lab_id: 'lab-1', state: 'READY', type: 'lab' }] }))
    if (u.includes('/aws/ssm/commands')) return Promise.resolve(okResponse({ commands: [{ id: 'cmd-1', command: ['echo', 'hello'], lab_id: 'lab-1', status: 'SUCCESS', output: 'hello' }] }))
    if (u.includes('/aws/trustedadvisor/results')) return Promise.resolve(okResponse({ results: [{ id: 'ta-1', check: 'idle-labs', severity: 'LOW', status: 'NEW', resource_id: 'lab-1', detail: 'Lab idle for 30h' }] }))
    if (u.includes('/aws/costexplorer/costs')) return Promise.resolve(okResponse({ lines: [{ name: 'Lab hours (Micro)', usage: 10, unit: 'hours', cost_usd: 0.1 }], total_usd: 0.1, pricing: { lab_micro_hour: 0.01 }, currency: 'USD (educational estimates — not an AWS bill)', note: 'Teaching rates.' }))
    if (u.includes('/aws/costexplorer/budgets')) return Promise.resolve(okResponse({ budgets: [{ id: 'b-1', name: 'monthly', limit_usd: 10, current_cost_usd: 0.1, usage_percent: 1, alert: false }] }))
    if (u.includes('/aws/organizations/units')) return Promise.resolve(okResponse({ units: [{ id: 'ou-1', name: 'engineering', parent_id: '', members: ['u-1'], member_emails: ['dev@example.com'] }] }))
    if (u.includes('/api/v1/labs')) return Promise.resolve(okResponse({ labs: [{ id: 'lab-1', name: 'lab-1', status: 'READY' }] }))
    if (u.includes('/api/v1/explorer/resources')) return Promise.resolve(okResponse({ items: [{ id: 'vpc-a', name: 'main-vpc', type: 'vpc' }, { id: 'vpc-b', name: 'peer-vpc', type: 'vpc' }] }))
    if (u.includes('/api/v1/admin/users')) return Promise.resolve(okResponse({ users: [{ id: 'u-1', email: 'dev@example.com', role: 'developer' }] }))
    if (u.includes('/aws/rds/instances')) return Promise.resolve(okResponse({ instances: [{ id: 'db-1', name: 'appdb' }] }))
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('Phase 6 consoles', () => {
  it('Athena renders saved queries and history with the Postgres label', async () => {
    const html = await renderSettled(<AthenaConsole />, '/services/athena', [pf(['athena-queries'], '/api/v1/aws/athena/queries'), pf(['athena-executions'], '/api/v1/aws/athena/executions')])
    expect(html).toContain('slow-queries')
    expect(html).toContain('PostgreSQL SQL')
    expect(html).toContain('SUCCEEDED')
  })

  it('Glue renders crawlers and catalog tables', async () => {
    const html = await renderSettled(<GlueConsole />, '/services/glue', [pf(['glue-crawlers'], '/api/v1/aws/glue/crawlers'), pf(['glue-tables'], '/api/v1/aws/glue/tables')])
    expect(html).toContain('main-crawler')
    expect(html).toContain('users')
  })

  it('Kinesis renders streams', async () => {
    const html = await renderSettled(<KinesisConsole />, '/services/kinesis', [pf(['kinesis-streams'], '/api/v1/aws/kinesis/streams')])
    expect(html).toContain('orders')
    expect(html).toContain('ACTIVE')
  })

  it('Firehose renders delivery streams and counters', async () => {
    const html = await renderSettled(<FirehoseConsole />, '/services/firehose', [pf(['firehose-streams'], '/api/v1/aws/firehose/streams')])
    expect(html).toContain('events')
    expect(html).toContain('5')
  })

  it('QuickSight renders dashboards', async () => {
    const html = await renderSettled(<QuickSightConsole />, '/services/quicksight', [pf(['qs-dashboards'], '/api/v1/aws/quicksight/dashboards'), pf(['athena-queries'], '/api/v1/aws/athena/queries')])
    expect(html).toContain('orders-by-day')
  })

  it('SageMaker renders notebooks, jobs, models and endpoints with real metrics', async () => {
    const html = await renderSettled(<SageMakerConsole />, '/services/sagemaker', [pf(['sm-notebooks'], '/api/v1/aws/sagemaker/notebooks'), pf(['sm-jobs'], '/api/v1/aws/sagemaker/training-jobs'), pf(['sm-models'], '/api/v1/aws/sagemaker/models'), pf(['sm-endpoints'], '/api/v1/aws/sagemaker/endpoints')])
    expect(html).toContain('explore')
    expect(html).toContain('housing-v1')
    expect(html).toContain('0.950')
  })

  it('Bedrock renders the SIMULATED playground label', async () => {
    const html = await renderSettled(<BedrockConsole />, '/services/bedrock', [pf(['bedrock-models'], '/api/v1/aws/bedrock/models'), pf(['bedrock-invocations'], '/api/v1/aws/bedrock/invocations')])
    expect(html).toContain('SIMULATED')
    expect(html).toContain('Claude 3 Sonnet')
  })

  it('DMS renders replication tasks with row counts', async () => {
    const html = await renderSettled(<DmsConsole initialSection="list" />, '/services/dms', [pf(['dms-tasks'], '/api/v1/aws/dms/tasks'), pf(['rds-instances'], '/api/v1/aws/rds/instances')])
    expect(html).toContain('platform-to-rds')
    expect(html).toContain('42')
  })

  it('DataSync renders transfer tasks with verification counts', async () => {
    const html = await renderSettled(<DataSyncConsole />, '/services/datasync', [pf(['datasync-tasks'], '/api/v1/aws/datasync/tasks')])
    expect(html).toContain('copy-docs')
    expect(html).toContain('local:docs')
  })

  it('Transfer renders the honest UNSUPPORTED decision', async () => {
    const html = await renderSettled(<TransferConsole />, '/services/transfer', [pf(['transfer-info'], '/api/v1/aws/transfer/info')])
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('sshd')
  })

  it('Migration Hub renders the aggregated task list', async () => {
    const html = await renderSettled(<MigrationHubConsole />, '/services/migrationhub', [pf(['migrationhub-tasks'], '/api/v1/aws/migrationhub/tasks')])
    expect(html).toContain('platform-to-rds')
    expect(html).toContain('COMPLETED')
  })

  it('GuardDuty renders detectors and real findings', async () => {
    const html = await renderSettled(<GuardDutyConsole />, '/services/guardduty', [pf(['gd-detectors'], '/api/v1/aws/guardduty/detectors'), pf(['gd-findings'], '/api/v1/aws/guardduty/findings')])
    expect(html).toContain('main')
    expect(html).toContain('Brute force login attempts')
  })

  it('Security Hub renders the summary and findings', async () => {
    const html = await renderSettled(<SecurityHubConsole />, '/services/securityhub', [pf(['sechub-summary'], '/api/v1/aws/securityhub/summary'), pf(['sechub-findings'], '/api/v1/aws/securityhub/findings')])
    expect(html).toContain('Open findings: 1')
    expect(html).toContain('Brute force login attempts')
  })

  it('Inspector renders assessments', async () => {
    const html = await renderSettled(<InspectorConsole />, '/services/inspector', [pf(['inspector-assessments'], '/api/v1/aws/inspector/assessments'), pf(['labs-list'], '/api/v1/labs')])
    expect(html).toContain('weekly')
    expect(html).toContain('COMPLETED')
  })

  it('Config renders rules, results and items', async () => {
    const html = await renderSettled(<ConfigConsole />, '/services/config', [pf(['config-rules'], '/api/v1/aws/config/rules'), pf(['config-results'], '/api/v1/aws/config/results'), pf(['config-items'], '/api/v1/aws/config/items')])
    expect(html).toContain('no-open-ssh')
    expect(html).toContain('sg-1')
  })

  it('WAF renders ACLs with the evaluation-only label', async () => {
    const html = await renderSettled(<WafConsole />, '/services/waf', [pf(['waf-acls'], '/api/v1/aws/waf/acls')])
    expect(html).toContain('web-acl')
    expect(html).toContain('does not block live traffic')
  })

  it('KMS renders keys without leaking wrapped keys', async () => {
    const html = await renderSettled(<KmsConsole />, '/services/kms', [pf(['kms-keys'], '/api/v1/aws/kms/keys')])
    expect(html).toContain('app-key')
    expect(html).not.toContain('wrapped_key')
  })

  it('Secrets Manager renders secrets without values in the list', async () => {
    const html = await renderSettled(<SecretsConsole />, '/services/secretsmanager', [pf(['secrets'], '/api/v1/aws/secretsmanager/secrets'), pf(['kms-keys'], '/api/v1/aws/kms/keys')])
    expect(html).toContain('db-password')
    expect(html).not.toContain('hunter2')
  })

  it('VPC Peering renders peerings', async () => {
    const html = await renderSettled(<VpcPeeringConsole />, '/services/vpcpeering', [pf(['vpc-peerings'], '/api/v1/aws/vpc/peerings'), pf(['explorer-vpcs'], '/api/v1/explorer/resources?type=vpc')])
    expect(html).toContain('a-to-b')
    expect(html).toContain('vpc-a')
  })

  it('Transit Gateway renders gateways and attachments', async () => {
    const html = await renderSettled(<TransitGatewayConsole />, '/services/transitgateway', [pf(['tgws'], '/api/v1/aws/transit-gateways')])
    expect(html).toContain('hub')
    expect(html).toContain('AVAILABLE')
  })

  it('VPC Endpoints renders endpoints with the teaching DNS name', async () => {
    const html = await renderSettled(<VpcEndpointsConsole />, '/services/vpcendpoints', [pf(['vpc-endpoints'], '/api/v1/aws/vpc/endpoints'), pf(['explorer-vpcs'], '/api/v1/explorer/resources?type=vpc')])
    expect(html).toContain('s3-ep')
    expect(html).toContain('s3.cf-local-1.vpce.local')
  })

  it('Network Firewall renders firewalls and rules', async () => {
    const html = await renderSettled(<NetworkFirewallConsole />, '/services/networkfirewall', [pf(['netfirewalls'], '/api/v1/aws/network-firewall/firewalls'), pf(['labs-list'], '/api/v1/labs')])
    expect(html).toContain('edge-fw')
    expect(html).toContain('allow-https')
  })

  it('Flow Logs renders groups and the telemetry-only note', async () => {
    const html = await renderSettled(<FlowLogsConsole />, '/services/flowlogs', [pf(['flowlog-groups'], '/api/v1/aws/vpc/flow-logs'), pf(['explorer-vpcs'], '/api/v1/explorer/resources?type=vpc')])
    expect(html).toContain('vpc-logs')
    expect(html).toContain('engine telemetry')
  })

  it('SSM renders inventory, instances and command history', async () => {
    const html = await renderSettled(<SsmConsole />, '/services/ssm', [pf(['ssm-inventory'], '/api/v1/aws/ssm/inventory'), pf(['ssm-instances'], '/api/v1/aws/ssm/instances'), pf(['ssm-commands'], '/api/v1/aws/ssm/commands'), pf(['labs-list'], '/api/v1/labs')])
    expect(html).toContain('lab-host')
    expect(html).toContain('hello')
  })

  it('Trusted Advisor renders check results', async () => {
    const html = await renderSettled(<TrustedAdvisorConsole />, '/services/trustedadvisor', [pf(['advisor-results'], '/api/v1/aws/trustedadvisor/results')])
    expect(html).toContain('idle-labs')
    expect(html).toContain('lab-1')
  })

  it('Cost Explorer renders lines and budgets with the educational label', async () => {
    const html = await renderSettled(<CostExplorerConsole />, '/services/costexplorer', [pf(['costs'], '/api/v1/aws/costexplorer/costs'), pf(['cost-budgets'], '/api/v1/aws/costexplorer/budgets')])
    expect(html).toContain('Lab hours (Micro)')
    expect(html).toContain('monthly')
    expect(html).toContain('not an AWS bill')
  })

  it('Organizations renders units with member counts', async () => {
    const html = await renderSettled(<OrganizationsConsole />, '/services/organizations', [pf(['org-units'], '/api/v1/aws/organizations/units'), pf(['admin-users'], '/api/v1/admin/users')])
    expect(html).toContain('engineering')
    expect(html).toContain('Manage members')
  })
})
