/**
 * S3 second-half tests (audit gap #5, 2026-11-22): the AWS S3 left-nav
 * items the survey records after Buckets — Files, Access Grants, IAM
 * Access Analyzer, Storage Lens, Storage management and insights — each
 * render a real section, and Access Points / Object Lambda / Batch
 * operations / Replication stay honestly disabled with the reason shown
 * on the tab. fetch is stubbed; React Query runs with retries off so
 * the render settles. Counts come from stubbed API-shaped data —
 * nothing is invented by the components.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { fetchS3Buckets, fetchS3Versioning, request } from '../services/api.ts'
import { S3Console } from './S3Console.tsx'

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
  const store = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  })
  fetchMock.mockImplementation((url: string) => {
    const u = String(url)
    // Versioning check first: its URL also contains '/api/v1/aws/s3/buckets'.
    if (u.includes('/versioning')) return Promise.resolve(okResponse({ enabled: true }))
    if (u.includes('/api/v1/aws/s3/buckets')) return Promise.resolve(okResponse({ buckets: [{ name: 'app-data', created_at: '2026-11-22T00:00:00Z' }] }))
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

const bucketPrefetch = [{ key: ['s3-buckets'], fn: fetchS3Buckets }]
const managementPrefetch = [
  ...bucketPrefetch,
  { key: ['s3-versioning', 'app-data'], fn: () => fetchS3Versioning('app-data') },
]

describe('S3 second half — new nav sections', () => {
  it('nav lists the survey second half: Files, Access Grants, IAM Access Analyzer, Storage management and insights, Storage Lens', async () => {
    const html = await renderSettled(<S3Console initialSection="buckets" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('Files')
    expect(html).toContain('Access Grants')
    expect(html).toContain('IAM Access Analyzer')
    expect(html).toContain('Storage management and insights')
    expect(html).toContain('Storage Lens')
    expect(html).toContain('Access management and security')
  })

  it('Files renders an honest UNSUPPORTED empty state, no fake file system', async () => {
    const html = await renderSettled(<S3Console initialSection="files" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('File systems')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('no S3 file-system endpoint')
    expect(html).toContain('nothing can be mounted')
    expect(html).toContain('Create file system')
    expect(html).not.toContain('app-data')
  })

  it('Access Grants renders an honest UNSUPPORTED empty state, no fake grant', async () => {
    const html = await renderSettled(<S3Console initialSection="access-grants" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('Access Grants')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('no access-grants API')
    expect(html).toContain('Create access grant')
  })

  it('IAM Access Analyzer renders an honest UNSUPPORTED empty state, no fake findings', async () => {
    const html = await renderSettled(<S3Console initialSection="analyzer" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('IAM Access Analyzer')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('no analyzer runs')
    expect(html).toContain('Create analyzer')
  })

  it('Storage Lens renders an honest UNSUPPORTED empty state, no fake metrics', async () => {
    const html = await renderSettled(<S3Console initialSection="storage-lens" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('Storage Lens')
    expect(html).toContain('UNSUPPORTED')
    expect(html).toContain('no metrics pipeline')
    expect(html).toContain('Create dashboard')
  })

  it('Storage management and insights renders a real per-bucket table: versioning real, replication/lifecycle honestly absent', async () => {
    const html = await renderSettled(<S3Console initialSection="storage-management" />, '/services/s3', managementPrefetch)
    expect(html).toContain('Storage management and insights')
    expect(html).toContain('PARTIAL')
    expect(html).toContain('app-data')
    expect(html).toContain('Enabled')
    expect(html).toContain('no replication API')
    expect(html).toContain('no lifecycle API')
    expect(html).toContain('no inventory')
  })

  it('Access Points, Object Lambda, Batch operations and Replication stay disabled with the reason on the tab', async () => {
    const html = await renderSettled(<S3Console initialSection="buckets" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('Access points')
    expect(html).toContain('Not supported by the local adapter')
    expect(html).toContain('Object Lambda access points')
    expect(html).toContain('Batch operations')
    expect(html).toContain('Not modelled')
    expect(html).toContain('Replication')
    expect(html).toContain('No replication API')
  })

  it('Buckets list and create-bucket action are unchanged', async () => {
    const html = await renderSettled(<S3Console initialSection="buckets" />, '/services/s3', bucketPrefetch)
    expect(html).toContain('app-data')
    expect(html).toContain('Name')
    expect(html).toContain('Region')
    expect(html).toContain('Creation date')
    expect(html).toContain('Create bucket')
  })
})

describe('S3 second half — request helper', () => {
  it('request is used for the buckets prefetch', async () => {
    await request('/api/v1/aws/s3/buckets')
    expect(fetchMock).toHaveBeenCalled()
  })
})
