/**
 * Tests for the AWS-console-style shared components (M9 Part 2, Phase 1).
 * Rendered server-side with react-dom/server inside a MemoryRouter —
 * no DOM or testing-library needed. They assert the behaviours the
 * console depends on: header branding/search, active-nav highlight,
 * table rendering/filtering, and state pills. Phase 1: header and nav
 * render from the backend service registry (passed as props), not from
 * a hardcoded frontend list.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { AwsHeader } from './AwsHeader.tsx'
import { AwsNav } from './AwsNav.tsx'
import { AwsTable } from './AwsTable.tsx'
import { AwsBadge, FidelityBadge, StateBadge } from './AwsBadge.tsx'
import { AwsButton } from './AwsButton.tsx'
import { searchRegistryServices } from '../services/registry.ts'
import type { RegistryService } from '../types/index.ts'

const FIXTURE: RegistryService[] = [
  { id: 'ec2', name: 'EC2', category: 'Compute', description: 'Lab sessions as instances', icon: 'server', route: '/services/ec2', availability: 'available', fidelity: 'PARTIAL', operations: [], resource_types: ['instance'], docs_url: '/learn', learning_exercises: ['ec2'] },
  { id: 's3', name: 'S3', category: 'Storage', description: 'Object storage buckets', icon: 'database', route: '/services/s3', availability: 'available', fidelity: 'PARTIAL', operations: [], resource_types: ['bucket'], docs_url: '/learn', learning_exercises: ['storage'] },
  { id: 'vpc', name: 'VPC', category: 'Networking', description: 'Virtual private clouds', icon: 'network', route: '/services/vpc', availability: 'available', fidelity: 'PARTIAL', operations: [], resource_types: ['vpc'], docs_url: '/learn', learning_exercises: ['networking'] },
  { id: 'iam', name: 'IAM', category: 'Security and Identity', description: 'Identity and access management', icon: 'key', route: '/services/iam', availability: 'available', fidelity: 'PARTIAL', operations: [], resource_types: ['user'], docs_url: '/learn', learning_exercises: ['iam'] },
]

function render(el: React.ReactElement, route = '/') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>{el}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AwsHeader', () => {
  // Vitest runs in Node: no localStorage. The header reads the token
  // and the stored region from it, so stub it like api.test.ts does.
  beforeEach(() => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
  })

  it('brands the console as CloudForge and offers service search + a local region', () => {
    const html = render(<AwsHeader onToggleNav={() => undefined} services={FIXTURE} />)
    expect(html).toContain('CloudForge')
    expect(html).toContain('Build Cloud Skills. Not Cloud Bills.')
    expect(html).toContain('Search services')
    expect(html).toContain('cf-local-1 (Local Region 1)')
    expect(html).not.toContain('AWS Management Console')
  })

  it('search navigation: typing "ec2" resolves to the real EC2 console route', () => {
    // The header navigates to the first search hit on Enter; the
    // resolution itself is the registry search.
    expect(searchRegistryServices('ec2', FIXTURE)[0].route).toBe('/services/ec2')
    expect(searchRegistryServices('vpc', FIXTURE)[0].route).toBe('/services/vpc')
    expect(searchRegistryServices('s3', FIXTURE)[0].route).toBe('/services/s3')
  })
})

describe('AwsNav active behaviour', () => {
  beforeEach(() => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
  })

  it('highlights the active service with the orange left border', () => {
    const html = render(<AwsNav collapsed={false} services={FIXTURE} />, '/services/ec2')
    // The EC2 entry (route=/services/ec2) must carry the active classes.
    expect(html).toContain('border-aws-orange')
    expect(html).toContain('text-aws-orange-dark')
    // Registry groups render: Compute / Storage / Networking / Security and Identity.
    for (const group of ['Compute', 'Storage', 'Networking', 'Security and Identity']) {
      expect(html).toContain(group)
    }
    // §4 main-navigation areas are present.
    for (const area of ['All Services', 'Resource Explorer', 'Learning Center', 'Account Settings', 'Administration']) {
      expect(html).toContain(area)
    }
  })

  it('highlights only the Console Home link on the home route', () => {
    const html = render(<AwsNav collapsed={false} services={FIXTURE} />, '/')
    expect(html).toContain('Console Home')
    expect(html.match(/border-aws-orange/g)?.length).toBe(1)
  })

  it('collapsed nav still renders one icon link per entry', () => {
    const html = render(<AwsNav collapsed services={FIXTURE} />, '/services/ec2')
    expect(html).toContain('border-aws-orange')
    // Collapsed nav shows icons only — no visible "Console Home" label.
    expect(html).not.toContain('>Console Home<')
  })

  it('renders the static areas when the registry is empty', () => {
    const html = render(<AwsNav collapsed={false} services={[]} registryError />, '/')
    expect(html).toContain('All Services')
    expect(html).toContain('Resource Explorer')
    expect(html).toContain('Service registry could not be loaded')
  })
})

describe('AwsTable', () => {
  const rows = [
    { id: 'i-1', name: 'web-01', state: 'RUNNING' },
    { id: 'i-2', name: 'db-01', state: 'STOPPED' },
  ]
  const columns = [
    { header: 'Instance ID', render: (r: (typeof rows)[number]) => r.id },
    { header: 'Name', render: (r: (typeof rows)[number]) => r.name },
    { header: 'State', render: (r: (typeof rows)[number]) => r.state },
  ]

  it('renders a grey header row, all rows, and a footer count', () => {
    const html = renderToStaticMarkup(
      <AwsTable columns={columns} rows={rows} emptyMessage="No instances" caption="Instances" />,
    )
    expect(html).toContain('bg-[#f2f3f3]')
    expect(html).toContain('i-1')
    expect(html).toContain('web-01')
    expect(html).toContain('2 items')
  })

  it('renders the empty message when there are no rows', () => {
    const html = renderToStaticMarkup(
      <AwsTable columns={columns} rows={[]} emptyMessage="No instances yet" caption="Instances" />,
    )
    expect(html).toContain('No instances yet')
  })

  it('shows the filter input and filtered count when a filter is wired up', () => {
    const html = renderToStaticMarkup(
      <AwsTable
        columns={columns}
        rows={rows.slice(0, 1)}
        totalCount={2}
        filter="web"
        onFilterChange={() => undefined}
        filterPlaceholder="Filter instances"
        emptyMessage="none"
        caption="Instances"
      />,
    )
    expect(html).toContain('Filter instances')
    expect(html).toContain('1 of 2 shown')
  })
})

describe('AwsBadge / StateBadge / FidelityBadge', () => {
  it('maps running to green, pending to amber, stopped/failed to red', () => {
    expect(renderToStaticMarkup(<StateBadge state="RUNNING" />)).toContain('bg-aws-success-bg')
    expect(renderToStaticMarkup(<StateBadge state="PROVISIONING" />)).toContain('bg-aws-warning-bg')
    expect(renderToStaticMarkup(<StateBadge state="STOPPED" />)).toContain('bg-aws-error-bg')
    expect(renderToStaticMarkup(<StateBadge state="FAILED" />)).toContain('bg-aws-error-bg')
    expect(renderToStaticMarkup(<StateBadge state="RUNNING" />)).toContain('running')
  })

  it('primary button is orange', () => {
    expect(renderToStaticMarkup(<AwsButton variant="primary">Launch</AwsButton>)).toContain('bg-aws-orange')
    expect(renderToStaticMarkup(<AwsBadge tone="blue">info</AwsBadge>)).toContain('text-aws-link')
  })

  it('fidelity badges label every fidelity honestly', () => {
    expect(renderToStaticMarkup(<FidelityBadge fidelity="REAL" />)).toContain('bg-aws-success-bg')
    expect(renderToStaticMarkup(<FidelityBadge fidelity="API_EMULATED" />)).toContain('API_EMULATED')
    expect(renderToStaticMarkup(<FidelityBadge fidelity="SIMULATED" />)).toContain('bg-aws-warning-bg')
    expect(renderToStaticMarkup(<FidelityBadge fidelity="UNSUPPORTED" />)).toContain('bg-aws-error-bg')
    expect(renderToStaticMarkup(<FidelityBadge fidelity="PARTIAL" />)).toContain('PARTIAL')
  })
})
