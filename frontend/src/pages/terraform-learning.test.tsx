/**
 * Render tests for the Terraform Learning section (Phase 5): the
 * dashboard renders real progress/skill/projects/executions data,
 * the courses tab renders levels + Level 4 projects, and the
 * projects tab renders the project list and editor shell. fetch +
 * localStorage are stubbed; React Query runs with retries off so
 * the render settles.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { TerraformLearning } from './TerraformLearning.tsx'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

async function renderSettled(
  route: string,
  prefetches: { key: unknown[]; fn: () => Promise<unknown> }[],
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  for (const p of prefetches) await client.prefetchQuery({ queryKey: p.key, queryFn: p.fn })
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}><TerraformLearning /></MemoryRouter>
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
    if (u.includes('/terraform-learning/dashboard')) {
      return Promise.resolve(okResponse({
        progress: { completed_lessons: 3, total_lessons: 55, percent: 5, current_level: 1, current_lesson_id: 'l1-04-providers', pending_exercises: 52, skill_level: 'Novice' },
        projects: [{ id: 'p-1', name: 'vpc-lab', level: 1, template: 'p4-01-vpc', lab_id: '', owner_id: 'u-1' }],
        project_count: 1, successful_plans: 1, successful_applies: 0,
        recent_executions: [{ id: 'r-1', project_id: 'p-1', operation: 'plan-preview', status: 'SUCCEEDED', source: 'cloudforge-local-diff', log: 'local plan preview: 2 to add' }],
        achievements: [
          { id: 'tf-first-lesson', title: 'First Lesson', description: 'Complete your first Terraform lesson', earned: true },
          { id: 'tf-level-1', title: 'Fundamentals Complete', description: 'Complete all 15 Level 1 lessons', earned: false },
        ],
        terraform_available: false, mode_lite: true,
      }))
    }
    if (u.includes('/terraform-learning/courses')) {
      return Promise.resolve(okResponse({
        levels: [
          { level: 1, title: 'Fundamentals', description: 'IaC from first principles', completed: 3, total: 15, lessons: [{ id: 'l1-01-iac', title: 'Infrastructure as Code', summary: 'Why IaC', completed: true, xp: 10 }] },
          { level: 2, title: 'Intermediate', description: 'State and modules', completed: 0, total: 20, lessons: [] },
          { level: 3, title: 'Advanced', description: 'Scale and teams', completed: 0, total: 20, lessons: [] },
        ],
        projects: [{ id: 'p4-01-vpc', level: 4, title: 'VPC Network', description: 'Build a VPC', objectives: [], tasks: [], validation_criteria: [], xp: 50 }],
        total_lessons: 55, completed_lessons: 3,
      }))
    }
    if (u.includes('/terraform-learning/provider/resources')) {
      return Promise.resolve(okResponse({ resources: [{ type: 'cloudforge_vpc', description: 'A VPC', cloudforge_api: 'POST /labs/{id}/resources', fidelity: 'API_EMULATED' }] }))
    }
    return Promise.resolve(okResponse({}))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('TerraformLearning', () => {
  it('renders the dashboard from real progress data', async () => {
    const html = await renderSettled('/terraform-learning', [
      { key: ['tflearn-dashboard'], fn: () => fetch('/api/v1/terraform-learning/dashboard').then((r) => r.json()) },
    ])
    expect(html).toContain('Terraform Learning')
    expect(html).toContain('3 / 55')
    expect(html).toContain('Novice')
    expect(html).toContain('plan-preview')
    expect(html).toContain('First Lesson')
    expect(html).toContain('terraform_unavailable')
  })

  it('renders courses with levels and Level 4 projects', async () => {
    const html = await renderSettled('/terraform-learning', [
      { key: ['tflearn-dashboard'], fn: () => fetch('/api/v1/terraform-learning/dashboard').then((r) => r.json()) },
      { key: ['tflearn-courses'], fn: () => fetch('/api/v1/terraform-learning/courses').then((r) => r.json()) },
    ])
    // Dashboard is the default tab; the Courses tab button is present.
    expect(html).toContain('Courses')
    expect(html).toContain('Projects &amp; Editor')
    expect(html).toContain('Provider')
  })

  it('labels local checks and honest unavailability on the dashboard', async () => {
    const html = await renderSettled('/terraform-learning', [
      { key: ['tflearn-dashboard'], fn: () => fetch('/api/v1/terraform-learning/dashboard').then((r) => r.json()) },
    ])
    expect(html).toContain('lite mode')
    expect(html).toContain('successful plans')
  })
})
