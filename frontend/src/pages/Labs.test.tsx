/**
 * Tests for the Launch-lab fix (2026-11-22): the Labs page's
 * definition picker disables labs longer than 30 minutes in lite
 * mode, with the reason shown — and leaves them enabled in full
 * mode. The picker reads lite mode from GET /api/v1/health, the
 * same flag the backend reports. Render is static (as in
 * console-home.test.tsx): the health query is prefetched, so the
 * disabled state is visible in the markup without clicking.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { LabDefinitionPicker } from '../components/LabLauncher.tsx'
import { Labs } from './Labs.tsx'
import { fetchHealth, fetchLabs } from '../services/api.ts'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

async function renderPicker(lite: boolean): Promise<string> {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockResolvedValue(
    okResponse({ service: 'cloudforge-api', version: '4.0.0', ok: true, uptime_seconds: 1, lite }),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await client.prefetchQuery({ queryKey: ['health'], queryFn: fetchHealth })
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <LabDefinitionPicker onLaunched={() => undefined} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  vi.unstubAllGlobals()
  fetchMock.mockReset()
  return html
}

describe('LabDefinitionPicker lite mode', () => {
  const store = new Map<string, string>()
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    store.clear()
  })

  it('lists all four definitions with durations', async () => {
    const html = await renderPicker(false)
    for (const title of [
      'Launch a Linux web server',
      'Fix a broken IAM policy',
      'Build a VPC with public and private subnets',
      'Build a three-tier application',
    ]) {
      expect(html).toContain(title)
    }
    expect(html).toContain('30 minutes')
    expect(html).toContain('60 minutes')
    expect(html).toContain('120 minutes')
  })

  it('disables the 60- and 120-minute labs in lite mode and explains why', async () => {
    const html = await renderPicker(true)
    expect(html).toContain('Lite mode: 1 lab at a time, 30-minute labs only')
    expect(html).toContain('Not available in lite mode (lite allows 30-minute labs only)')
    // Exactly the two long labs carry a disabled="" attribute (the
    // `disabled:opacity-50` Tailwind class on every button does not
    // count — match the attribute, not the class name).
    expect(html.match(/disabled=""/g)?.length).toBe(2)
  })

  it('leaves every lab enabled in full mode', async () => {
    const html = await renderPicker(false)
    expect(html).not.toContain('Not available in lite mode')
    expect(html.match(/disabled=""/g)).toBeNull()
  })
})

describe('Labs bulk destroy selection', () => {
  const twoLabs = {
    labs: [
      { id: 'lab-1', name: 'EC2 basics lab', state: 'READY', definition_id: 'ec2-basics', created_at: '2026-11-21T00:00:00Z' },
      { id: 'lab-2', name: 'IAM lab', state: 'READY', definition_id: 'iam-security', created_at: '2026-11-21T00:00:00Z' },
    ],
  }

  async function renderLabs(): Promise<string> {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockResolvedValue(okResponse(twoLabs))
    const store = new Map<string, string>([['cloudforge_token', 'test-token']])
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await client.prefetchQuery({ queryKey: ['labs'], queryFn: fetchLabs })
    const html = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/labs']}>
          <Labs />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    return html
  }

  it('renders a checkbox per row and a select-all checkbox in the header', async () => {
    const html = await renderLabs()
    expect(html).toContain('aria-label="Select all labs"')
    expect(html).toContain('aria-label="Select lab EC2 basics lab"')
    expect(html).toContain('aria-label="Select lab IAM lab"')
  })

  it('shows "Destroy all labs" with the total count, and no "Destroy selected" before anything is selected', async () => {
    const html = await renderLabs()
    expect(html).toContain('Destroy all labs (2)')
    expect(html).not.toContain('Destroy selected')
  })

  it('still shows a per-row Destroy action next to workstation/topology', async () => {
    const html = await renderLabs()
    expect(html).toContain('Destroy')
    expect(html).toContain('Open workstation')
    expect(html).toContain('Topology')
  })
})
