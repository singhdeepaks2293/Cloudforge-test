/**
 * Render tests for the AWS-console-style pages (M9 Part 2): the
 * Dashboard (Console Home) and Labs (EC2-style Instances table) render
 * real API-shaped data — live health, real courses, real labs — with
 * no invented numbers. fetch + localStorage are stubbed; React Query
 * runs with retries off so the render settles.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { Dashboard } from './Dashboard.tsx'
import { Labs } from './Labs.tsx'

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

describe('Console Home renders live data', () => {
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
      if (String(url).includes('/health')) {
        return Promise.resolve(
          okResponse({ service: 'cloudforge-api', version: '4.1.0', ok: true, uptime_seconds: 3600, network_engine_reachable: false }),
        )
      }
      if (String(url).includes('/courses')) {
        return Promise.resolve(
          okResponse({ courses: [{ id: 'vpc-101', title: 'VPC Fundamentals', track: 'networking', description: 'd', lesson_count: 4 }] }),
        )
      }
      return Promise.resolve(okResponse({ labs: [] }))
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('shows the CloudForge welcome hero and the service health widget', async () => {
    const { fetchHealth, fetchCourses } = await import('../services/api.ts')
    const html = await renderSettled(<Dashboard />, '/', [
      { key: ['health'], fn: fetchHealth },
      { key: ['courses'], fn: fetchCourses },
    ])
    expect(html).toContain('Welcome to CloudForge')
    expect(html).toContain('cloudforge-api')
    expect(html).toContain('4.1.0')
    expect(html).toContain('VPC Fundamentals')
    expect(html).toContain('Recently visited services')
    expect(html).toContain('Build a solution')
  })
})

describe('Labs renders an EC2-style Instances table', () => {
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
    fetchMock.mockResolvedValue(
      okResponse({
        labs: [
          {
            id: 'lab-1',
            name: 'm9p2-check',
            state: 'RUNNING',
            definition_id: 'vpc-fundamentals',
            created_at: '2026-11-21T17:40:36Z',
            resources: [{ id: 'i-1', kind: 'instance', name: 'web-01', state: 'running', private_ip: '10.0.0.5' }],
          },
        ],
      }),
    )
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('lists the lab as an instance row with state pill and IPs', async () => {
    const { fetchLabs } = await import('../services/api.ts')
    const html = await renderSettled(<Labs />, '/labs', [{ key: ['labs'], fn: fetchLabs }])
    expect(html).toContain('Instances')
    expect(html).toContain('Instance ID')
    expect(html).toContain('lab-1')
    expect(html).toContain('m9p2-check')
    expect(html).toContain('running')
    expect(html).toContain('10.0.0.5')
    expect(html).toContain('Open workstation')
  })
})
