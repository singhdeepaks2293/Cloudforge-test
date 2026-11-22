/**
 * Render test for the tabbed IAM policy editor (IamConsole, Policies
 * section). Render is static (as in console-home.test.tsx): the
 * create-policy card renders regardless of the policies query, so
 * both tabs, the visual statement form and the JSON preview are
 * visible in the markup without clicking.
 */
import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { IamConsole } from './IamConsole.tsx'

function renderPolicies(): string {
  vi.stubGlobal('fetch', vi.fn())
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <IamConsole initialSection="policies" />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  vi.unstubAllGlobals()
  return html
}

describe('IAM policy editor tabs', () => {
  it('renders Visual and JSON tabs with the visual form and live JSON preview', () => {
    const html = renderPolicies()
    expect(html).toContain('Visual')
    expect(html).toContain('JSON')
    // Visual tab is the default: statement form + preview of the sample.
    expect(html).toContain('Statement 1')
    expect(html).toContain('Statement 2')
    expect(html).toContain('Add statement')
    expect(html).toContain('JSON preview')
    expect(html).toContain('s3:ListBuckets')
    expect(html).toContain('s3:DeleteBucket')
    expect(html).toContain('Create policy')
  })

  it('offers only backend-supported options in the visual form', () => {
    const html = renderPolicies()
    expect(html).toContain('StringEquals on aws:username')
    expect(html).toContain('s3:ListBuckets')
    expect(html).toContain('DynamoDB')
    expect(html).not.toContain('NotAction')
    expect(html).not.toContain('Principal')
  })
})
