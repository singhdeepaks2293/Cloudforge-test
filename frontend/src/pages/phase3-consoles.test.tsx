/**
 * Render tests for the Phase 3 consoles (DynamoDB, RDS, ElastiCache,
 * ELB, Route 53, SQS, SNS, EventBridge, Lambda, API Gateway): each
 * renders real API-shaped data — tables, instances, clusters, load
 * balancers, zones, queues, topics, rules, functions, APIs — with
 * fidelity labels and honest not-built notes. fetch + localStorage
 * are stubbed; React Query runs with retries off so the render
 * settles.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { ApiGatewayConsole } from './ApiGatewayConsole.tsx'
import { DynamoDbConsole } from './DynamoDbConsole.tsx'
import { ElastiCacheConsole } from './ElastiCacheConsole.tsx'
import { ElbConsole } from './ElbConsole.tsx'
import { EventBridgeConsole } from './EventBridgeConsole.tsx'
import { LambdaConsole } from './LambdaConsole.tsx'
import { RdsConsole } from './RdsConsole.tsx'
import { Route53Console } from './Route53Console.tsx'
import { SnsConsole } from './SnsConsole.tsx'
import { SqsConsole } from './SqsConsole.tsx'

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
    if (u.includes('/aws/dynamodb/tables')) {
      return Promise.resolve(okResponse({ tables: [{ name: 'users', key_attribute: 'id', item_count: 2, backend: 'API_EMULATED' }] }))
    }
    if (u.includes('/aws/rds/instances')) {
      return Promise.resolve(okResponse({ instances: [{ id: 'db-1', name: 'appdb', engine: 'postgres', version: '16', status: 'available', endpoint: 'localhost:5432', db_name: 'appdb', username: 'appdb_user' }] }))
    }
    if (u.includes('/aws/elasticache/clusters')) {
      return Promise.resolve(okResponse({ clusters: [{ id: 'c-1', name: 'sess', engine: 'redis', status: 'available', mode: 'lite' }] }))
    }
    if (u.includes('/aws/elb/load-balancers')) {
      return Promise.resolve(okResponse({ load_balancers: [{ id: 'lb-1', name: 'web-lb', scheme: 'internet-facing', dns_name: 'web-lb.cf-local-1.elb.local', status: 'active' }] }))
    }
    if (u.includes('/aws/elb/target-groups')) {
      return Promise.resolve(okResponse({ target_groups: [{ id: 'tg-1', name: 'web-tg', protocol: 'HTTP', port: 80, healthy: 1, total: 1 }] }))
    }
    if (u.includes('/aws/route53/zones')) {
      return Promise.resolve(okResponse({ zones: [{ id: 'z-1', name: 'example.local', record_count: 2 }] }))
    }
    if (u.includes('/aws/sqs/queues')) {
      return Promise.resolve(okResponse({ queues: [{ id: 'q-1', name: 'orders', approximate_visible_messages: 3, approximate_invisible_messages: 0, visibility_timeout_seconds: 30, max_receives: 0 }] }))
    }
    if (u.includes('/aws/sns/topics')) {
      return Promise.resolve(okResponse({ topics: [{ id: 't-1', name: 'alerts', arn: 'arn:cloudforge:sns:local:alerts' }] }))
    }
    if (u.includes('/aws/events/rules')) {
      return Promise.resolve(okResponse({ rules: [{ id: 'r-1', name: 'orders-rule', state: 'ENABLED', matched_events: 2, deliveries: 2, pattern: { source: ['my.app'] } }] }))
    }
    if (u.includes('/aws/lambda/functions')) {
      return Promise.resolve(okResponse({ functions: [{ id: 'f-1', name: 'echoer', runtime: 'python3', version: 1, timeout_seconds: 10, memory_mb: 256 }] }))
    }
    if (u.includes('/aws/apigateway/apis')) {
      return Promise.resolve(okResponse({ apis: [{ id: 'a-1', name: 'echo-api', protocol: 'REST', status: 'available' }] }))
    }
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('Phase 3 consoles render real data', () => {
  it('DynamoDB lists tables with backend and key attribute', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<DynamoDbConsole />, '/services/dynamodb', [
      { key: ['dynamodb-tables'], fn: () => request('/api/v1/aws/dynamodb/tables') },
    ])
    expect(html).toContain('DynamoDB — Tables')
    expect(html).toContain('users')
    expect(html).toContain('id')
    expect(html).toContain('API_EMULATED')
  })

  it('RDS lists instances and disables Start/Stop as unsupported', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<RdsConsole />, '/services/rds', [
      { key: ['rds-instances'], fn: () => request('/api/v1/aws/rds/instances') },
    ])
    expect(html).toContain('RDS — Databases')
    expect(html).toContain('appdb')
    expect(html).toContain('postgres')
    expect(html).toContain('Start')
    expect(html).toContain('Stop')
    expect(html).toContain('unsupported')
  })

  it('ElastiCache lists clusters with engine and mode', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<ElastiCacheConsole />, '/services/elasticache', [
      { key: ['cache-clusters'], fn: () => request('/api/v1/aws/elasticache/clusters') },
    ])
    expect(html).toContain('ElastiCache — Clusters')
    expect(html).toContain('sess')
    expect(html).toContain('redis')
    expect(html).toContain('lite')
  })

  it('ELB lists load balancers and target groups', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<ElbConsole />, '/services/elb', [
      { key: ['elb-lbs'], fn: () => request('/api/v1/aws/elb/load-balancers') },
      { key: ['elb-tgs'], fn: () => request('/api/v1/aws/elb/target-groups') },
    ])
    expect(html).toContain('web-lb')
    expect(html).toContain('web-lb.cf-local-1.elb.local')
    expect(html).toContain('web-tg')
  })

  it('Route 53 lists hosted zones', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<Route53Console />, '/services/route53', [
      { key: ['dns-zones'], fn: () => request('/api/v1/aws/route53/zones') },
    ])
    expect(html).toContain('example.local')
  })

  it('SQS lists queues with visible message counts', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<SqsConsole />, '/services/sqs', [
      { key: ['sqs-queues'], fn: () => request('/api/v1/aws/sqs/queues') },
    ])
    expect(html).toContain('SQS — Queues')
    expect(html).toContain('orders')
    expect(html).toContain('Visible messages')
    expect(html).toContain('no FIFO')
  })

  it('SNS lists topics with ARNs', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<SnsConsole />, '/services/sns', [
      { key: ['sns-topics'], fn: () => request('/api/v1/aws/sns/topics') },
    ])
    expect(html).toContain('SNS — Topics')
    expect(html).toContain('alerts')
    expect(html).toContain('arn:cloudforge:sns:local:alerts')
  })

  it('EventBridge lists rules with state and matched counts', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<EventBridgeConsole />, '/services/eventbridge', [
      { key: ['eb-rules'], fn: () => request('/api/v1/aws/events/rules') },
    ])
    expect(html).toContain('EventBridge — Rules')
    expect(html).toContain('orders-rule')
    expect(html).toContain('ENABLED')
  })

  it('Lambda lists functions with runtime and states the subprocess sandbox', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<LambdaConsole />, '/services/lambda', [
      { key: ['lambda-functions'], fn: () => request('/api/v1/aws/lambda/functions') },
    ])
    expect(html).toContain('Lambda — Functions')
    expect(html).toContain('echoer')
    expect(html).toContain('python3')
    expect(html).toContain('subprocess sandbox')
  })

  it('API Gateway lists APIs with protocol', async () => {
    const { request } = await import('../services/api.ts')
    const html = await renderSettled(<ApiGatewayConsole />, '/services/apigateway', [
      { key: ['apigw-apis'], fn: () => request('/api/v1/aws/apigateway/apis') },
    ])
    expect(html).toContain('API Gateway — APIs')
    expect(html).toContain('echo-api')
    expect(html).toContain('REST')
    expect(html).toContain('no WebSocket APIs')
  })
})
