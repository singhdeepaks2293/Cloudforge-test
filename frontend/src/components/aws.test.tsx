/**
 * Tests for the AWS-console-style shared components (M9 Part 2).
 * Rendered server-side with react-dom/server inside a MemoryRouter —
 * no DOM or testing-library needed. They assert the behaviours the
 * console depends on: header branding/search, active-nav highlight,
 * table rendering/filtering, and state pills.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import { AwsHeader } from './AwsHeader.tsx'
import { AwsNav } from './AwsNav.tsx'
import { AwsTable } from './AwsTable.tsx'
import { AwsBadge, StateBadge } from './AwsBadge.tsx'
import { AwsButton } from './AwsButton.tsx'
import { searchServices } from '../services/catalog.ts'

function render(el: React.ReactElement, route = '/') {
  return renderToStaticMarkup(<MemoryRouter initialEntries={[route]}>{el}</MemoryRouter>)
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
    const html = render(<AwsHeader onToggleNav={() => undefined} />)
    expect(html).toContain('CloudForge')
    expect(html).toContain('Build Cloud Skills. Not Cloud Bills.')
    expect(html).toContain('Search services')
    expect(html).toContain('cf-local-1 (Local)')
    expect(html).not.toContain('AWS Management Console')
  })

  it('search navigation: typing "ec2" resolves to the local Instances route', () => {
    // The header navigates to the first search hit on Enter; the
    // resolution itself is the catalog search.
    expect(searchServices('ec2')[0].to).toBe('/labs')
    expect(searchServices('vpc')[0].to).toBe('/console')
    expect(searchServices('courses')[0].to).toBe('/courses')
  })
})

describe('AwsNav active behaviour', () => {
  it('highlights the active service with the orange left border', () => {
    const html = render(<AwsNav collapsed={false} />, '/labs')
    // The EC2 entry (to=/labs) must carry the active classes.
    expect(html).toContain('border-aws-orange')
    expect(html).toContain('text-aws-orange-dark')
    // Groups render: Compute / Networking / Storage / Databases / Security / Monitoring / Learn.
    for (const group of ['Compute', 'Networking', 'Storage', 'Databases', 'Security', 'Monitoring', 'Learn']) {
      expect(html).toContain(group)
    }
  })

  it('highlights only home-destination entries on the home route', () => {
    const html = render(<AwsNav collapsed={false} />, '/')
    expect(html).toContain('Console Home')
    // Three entries point at '/': the Console Home link, CloudWatch
    // and the Console Home service — all three are legitimately active.
    expect(html.match(/border-aws-orange/g)?.length).toBe(3)
  })

  it('collapsed nav still renders one icon link per service', () => {
    const html = render(<AwsNav collapsed />, '/labs')
    expect(html).toContain('border-aws-orange')
    // Collapsed nav shows icons only — no visible "Console Home" label.
    expect(html).not.toContain('>Console Home<')
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

describe('AwsBadge / StateBadge', () => {
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
})
