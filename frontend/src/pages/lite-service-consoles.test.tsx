/**
 * Render tests for the v3 lite-service consoles: the shared
 * ServiceHome dashboard (VPC reference pattern), ConsoleWithHome,
 * the generic LiteServiceConsole (UNSUPPORTED exam services), the
 * VPC dashboard, and a dedicated console's Home tab. fetch +
 * localStorage are stubbed; React Query runs with retries off so the
 * render settles. Counts come from stubbed API-shaped data — nothing
 * is invented by the components under test.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { fetchAudit, fetchAuditExportJobs, fetchLabResources, fetchLabs, request } from '../services/api.ts'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { LiteServiceConsole } from './LiteServiceConsole.tsx'
import { VpcConsole } from './VpcConsole.tsx'
import { RdsConsole } from './RdsConsole.tsx'
import { CloudTrailConsole } from './CloudTrailConsole.tsx'

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

const serviceBody = (id: string, name: string, resourceTypes: string[]) => ({
  id,
  name,
  category: 'Compute',
  description: `${name} — exam vocabulary only.`,
  icon: 'server',
  route: `/services/${id}`,
  availability: 'unavailable',
  fidelity: 'UNSUPPORTED',
  operations: [],
  resource_types: resourceTypes,
  docs_url: '/learn',
  learning_exercises: ['aws-saa', 'aws-sap'],
})

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
    if (u.includes('/api/v1/services/autoscaling')) return Promise.resolve(okResponse(serviceBody('autoscaling', 'EC2 Auto Scaling', ['auto-scaling-group'])))
    if (u.includes('/api/v1/services/backup')) return Promise.resolve(okResponse(serviceBody('backup', 'Backup', ['backup-plan', 'vault'])))
    if (u.includes('/api/v1/explorer/resources')) return Promise.resolve(okResponse({ resources: [], total: 0, limit: 50, offset: 0, region: 'cf-local-1', region_scoped: false }))
    if (u.includes('/api/v1/labs/lab-1/resources')) return Promise.resolve(okResponse({ resources: [] }))
    if (u.includes('/api/v1/labs')) return Promise.resolve(okResponse({ labs: [{ id: 'lab-1', name: 'lab-1', status: 'READY' }] }))
    if (u.includes('/api/v1/aws/rds/instances')) return Promise.resolve(okResponse({ instances: [{ id: 'db-1', name: 'appdb', status: 'AVAILABLE', version: '16', endpoint: 'db-1.cf-local-1.local', database_name: 'appdb', username: 'cf_admin' }] }))
    if (u.includes('/audit/export/jobs')) return Promise.resolve(okResponse({ jobs: [], retention: 7 }))
    if (u.includes('/audit')) return Promise.resolve(okResponse({ entries: [{ id: 'ev-1', actor_id: 'admin-1', action: 'lab_created', resource_type: 'lab', resource_id: 'lab-1', created_at: '2026-11-22T00:00:00Z' }], total: 1 }))
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('ServiceHomeDashboard (VPC reference pattern)', () => {
  it('renders resource cards with counts, region label, Regions expander and action links — no table', async () => {
    const html = await renderSettled(
      <ServiceHomeDashboard
        serviceName="Test"
        banner="Test banner."
        actions={[{ label: 'Create thing', primary: true, onClick: () => {} }]}
        cards={[
          { label: 'Things', count: 2, note: 'Live count' },
          { label: 'Widgets', count: 0, disabledReason: 'UNSUPPORTED — no API.' },
        ]}
        health={['Control plane: healthy.']}
        settings={[{ label: 'Resource Explorer', to: '/explorer' }]}
        info={[{ label: 'Learning Center', to: '/learn' }]}
      />,
      '/',
      [],
    )
    expect(html).toContain('Resources by Region')
    expect(html).toContain('Things')
    expect(html).toContain('Regions')
    expect(html).toContain('Create thing')
    expect(html).toContain('Service Health')
    expect(html).toContain('Additional Information')
    expect(html).toContain('UNSUPPORTED — no API.')
    expect(html).not.toContain('<table')
  })

  it('ConsoleWithHome defaults to the list and shows Home with initialSection="home"', async () => {
    const home = <p>HOME DASHBOARD</p>
    const listHtml = await renderSettled(
      <ConsoleWithHome
        breadcrumb={[{ label: 'CloudForge', to: '/' }]}
        title="Test"
        description="Test console."
        fidelity="PARTIAL"
        home={home}
        listLabel="Things"
      >
        <p>LIST PAGE</p>
      </ConsoleWithHome>,
      '/',
      [],
    )
    expect(listHtml).toContain('LIST PAGE')
    expect(listHtml).not.toContain('HOME DASHBOARD')
    const homeHtml = await renderSettled(
      <ConsoleWithHome
        breadcrumb={[{ label: 'CloudForge', to: '/' }]}
        title="Test"
        description="Test console."
        fidelity="PARTIAL"
        home={home}
        listLabel="Things"
        initialSection="home"
      >
        <p>LIST PAGE</p>
      </ConsoleWithHome>,
      '/',
      [],
    )
    expect(homeHtml).toContain('HOME DASHBOARD')
    expect(homeHtml).not.toContain('LIST PAGE')
  })
})

describe('LiteServiceConsole (generic lite exam services)', () => {
  it('Auto Scaling renders an honest UNSUPPORTED dashboard with 0 recorded and an EC2 alternative', async () => {
    const html = await renderSettled(
      <Routes>
        <Route path="/services/:id" element={<LiteServiceConsole />} />
      </Routes>,
      '/services/autoscaling',
      [pf(['service', 'autoscaling'], '/api/v1/services/autoscaling')],
    )
    expect(html).toContain('EC2 Auto Scaling')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('Resources by Region')
    expect(html).toContain('Auto Scaling Group')
    expect(html).toContain('EC2 — Instances')
    expect(html).toContain('no create API')
  })

  it('Backup states the scripts model and names backup.sh / restore.sh', async () => {
    const html = await renderSettled(
      <Routes>
        <Route path="/services/:id" element={<LiteServiceConsole />} />
      </Routes>,
      '/services/backup',
      [pf(['service', 'backup'], '/api/v1/services/backup')],
    )
    expect(html).toContain('Backup')
    expect(html).toContain('scripts/backup.sh')
    expect(html).toContain('scripts/restore.sh')
    expect(html).toContain('RDS — Snapshots')
  })
})

describe('VPC dashboard (reference service)', () => {
  it('renders the Home dashboard with resource cards and grouped nav', async () => {
    const html = await renderSettled(<VpcConsole />, '/services/vpc', [
      { key: ['labs'], fn: fetchLabs },
      { key: ['vpc-resources', 'lab-1'], fn: () => fetchLabResources('lab-1') },
    ])
    expect(html).toContain('Resources by Region')
    expect(html).toContain('Your VPCs')
    expect(html).toContain('Create VPC')
    expect(html).toContain('Launch EC2 Instances')
    expect(html).toContain('Virtual private cloud')
    expect(html).toContain('Network ACLs')
    expect(html).toContain('Service Health')
  })
})

describe('CloudTrail Home tab', () => {
  it('Home shows live event counts and the Trails gap; the default view stays the list', async () => {
    const homeHtml = await renderSettled(<CloudTrailConsole initialSection="home" />, '/services/cloudtrail', [
      { key: ['audit', { limit: 50 }], fn: () => fetchAudit({ limit: 50 }) },
      { key: ['audit-export-jobs'], fn: fetchAuditExportJobs },
    ])
    expect(homeHtml).toContain('Resources by Region')
    expect(homeHtml).toContain('Export jobs')
    expect(homeHtml).toContain('Trails')
    expect(homeHtml).toContain('not AWS CloudTrail')
    const listHtml = await renderSettled(<CloudTrailConsole />, '/services/cloudtrail', [
      { key: ['audit', { limit: 50 }], fn: () => fetchAudit({ limit: 50 }) },
      { key: ['audit-export-jobs'], fn: fetchAuditExportJobs },
    ])
    expect(listHtml).toContain('lab_created')
    expect(listHtml).not.toContain('Resources by Region')
  })
})

describe('Dedicated console Home tab', () => {
  it('RDS Home shows live DB-instance counts; the default view stays the list', async () => {
    const homeHtml = await renderSettled(<RdsConsole initialSection="home" />, '/services/rds', [
      pf(['rds-instances'], '/api/v1/aws/rds/instances'),
    ])
    expect(homeHtml).toContain('Resources by Region')
    expect(homeHtml).toContain('DB instances')
    expect(homeHtml).toContain('Aurora clusters')
    const listHtml = await renderSettled(<RdsConsole />, '/services/rds', [
      pf(['rds-instances'], '/api/v1/aws/rds/instances'),
    ])
    expect(listHtml).toContain('appdb')
    expect(listHtml).not.toContain('Resources by Region')
  })
})
