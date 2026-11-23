/**
 * v4.1 "Sign in as root": the login page offers a root checkbox
 * (unchecked by default), and login() sends the choice as `as_root`
 * in the login request body.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { Login } from './Auth.tsx'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('Login offers "Sign in as root"', () => {
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
    fetchMock.mockResolvedValue(
      okResponse({ token: 'tok', expires_at: new Date().toISOString(), user: { id: '1', email: 'a@example.com', role: 'learner' } }),
    )
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('shows the root checkbox unchecked by default, with the learner note', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const html = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/login']}>
          <Login />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(html).toContain('Sign in as root (admin)')
    expect(html).toContain('Root sign-in needs an admin account')
    expect(html).toContain('you sign in as a learner')
    // Unchecked by default: no `checked` attribute on the checkbox.
    expect(html).not.toMatch(/<input[^>]*type="checkbox"[^>]*checked/)
  })

  it('login() sends as_root=true when root is requested, false by default', async () => {
    const { login } = await import('../services/api.ts')
    const bodies: unknown[] = []
    fetchMock.mockImplementation((_url: string, opts: { body?: string }) => {
      bodies.push(opts?.body !== undefined ? JSON.parse(opts.body) : null)
      return Promise.resolve(
        okResponse({ token: 'tok', expires_at: new Date().toISOString(), user: { id: '1', email: 'a@example.com', role: 'learner' } }),
      )
    })
    await login('admin@example.com', 'correct-horse', true)
    await login('learner@example.com', 'correct-horse')
    expect(bodies).toHaveLength(2)
    expect(bodies[0]).toMatchObject({ email: 'admin@example.com', as_root: true })
    expect(bodies[1]).toMatchObject({ email: 'learner@example.com', as_root: false })
  })
})
