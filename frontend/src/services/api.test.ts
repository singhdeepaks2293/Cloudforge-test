/**
 * Tests for the API client (src/services/api.ts).
 *
 * These run in Node under vitest — no DOM, no network. fetch is stubbed
 * per test. They cover URL building, the correlation header, token
 * handling, and strict parsing of the health/courses payloads, so a
 * backend contract drift fails here instead of silently rendering
 * wrong data in the dashboard.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  buildTerminalWsUrl,
  buildUrl,
  clearToken,
  createLabResource,
  fetchCourseDetail,
  fetchCourses,
  fetchHealth,
  fetchLab,
  fetchLabResources,
  fetchLabTopology,
  getToken,
  isPendingEndpointError,
  parseCourseDetail,
  parseCourseList,
  parseHealth,
  parseLabResource,
  parseLabResourceList,
  request,
  requestTerminalToken,
  setToken,
  validateLab,
} from './api.ts'

const okResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

describe('buildUrl', () => {
  it('defaults to same-origin relative URLs when VITE_API_URL is unset (M9 nginx/Vite proxy)', () => {
    expect(buildUrl('/api/v1/health')).toBe('/api/v1/health')
  })

  it('adds a leading slash when the path has none', () => {
    expect(buildUrl('api/v1/courses')).toBe('/api/v1/courses')
  })

  it('honours an explicit VITE_API_URL (split deployment)', () => {
    vi.stubEnv('VITE_API_URL', 'http://localhost:8080')
    try {
      expect(buildUrl('/api/v1/health')).toBe('http://localhost:8080/api/v1/health')
      expect(buildUrl('/api/v1/health')).not.toContain('8080//')
    } finally {
      vi.unstubAllEnvs()
    }
  })

  it('strips a trailing slash from an explicit VITE_API_URL', () => {
    vi.stubEnv('VITE_API_URL', 'http://localhost:8080/')
    try {
      expect(buildUrl('/api/v1/health')).toBe('http://localhost:8080/api/v1/health')
    } finally {
      vi.unstubAllEnvs()
    }
  })
})

describe('parseHealth', () => {
  it('parses a complete M1 health payload', () => {
    expect(
      parseHealth({
        service: 'cloudforge-api',
        version: '0.1.0-m1',
        ok: true,
        uptime_seconds: 42,
        network_engine_reachable: true,
        network_engine_detail: 'ok',
      }),
    ).toEqual({
      service: 'cloudforge-api',
      version: '0.1.0-m1',
      ok: true,
      uptime_seconds: 42,
      network_engine_reachable: true,
      detail: 'ok',
    })
  })

  it('treats a missing network_engine_reachable as unreachable, never reachable', () => {
    const h = parseHealth({ service: 'cloudforge-api', version: '0.1.0', ok: true, uptime_seconds: 1 })
    expect(h.network_engine_reachable).toBe(false)
  })

  it('rejects a non-object payload instead of inventing health data', () => {
    expect(() => parseHealth(null)).toThrow(ApiError)
    expect(() => parseHealth('ok')).toThrow(ApiError)
    expect(() => parseHealth({ service: 1, version: 'x', ok: true, uptime_seconds: 0 })).toThrow(ApiError)
  })
})

describe('parseCourseList', () => {
  it('accepts a bare array and a { courses } envelope', () => {
    const course = { id: 'vpc-101', title: 'VPC Fundamentals', track: 'networking', description: 'd', lessons: 4 }
    expect(parseCourseList([course])).toHaveLength(1)
    expect(parseCourseList({ courses: [course] })).toHaveLength(1)
  })

  it('normalises a lessons array to a lesson count', () => {
    const [c] = parseCourseList([
      { id: 'x', title: 'T', track: 'iam', description: '', lessons: [{}, {}, {}] },
    ])
    expect(c.lessons).toBe(3)
  })

  it('uses lesson_count from the Go list view, where lessons is omitted', () => {
    // Real backend list shape: lessons omitted, lesson_count present.
    const [c] = parseCourseList({
      courses: [{ id: 'vpc-fundamentals', title: 'VPC Fundamentals', track: 'Networking', description: 'd', lesson_count: 6 }],
    })
    expect(c.lessons).toBe(6)
  })

  it('rejects malformed payloads', () => {
    expect(() => parseCourseList({})).toThrow(ApiError)
    expect(() => parseCourseList([{ id: 1 }])).toThrow(ApiError)
  })
})

describe('request', () => {
  const fetchMock = vi.fn()
  // Node (vitest default environment) has no localStorage; stub a
  // minimal in-memory one so token handling is testable here.
  const store = new Map<string, string>()
  const localStorageStub = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  }

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('localStorage', localStorageStub)
    store.clear()
    clearToken()
  })

  afterEach(() => {
    clearToken()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('sends an X-Request-ID correlation header on every request', async () => {
    fetchMock.mockResolvedValue(okResponse({ service: 'cloudforge-api', version: '0.1.0-m1', ok: true, uptime_seconds: 0 }))
    await fetchHealth()
    const headers = fetchMock.mock.calls[0][1].headers as Record<string, string>
    expect(typeof headers['X-Request-ID']).toBe('string')
    expect(headers['X-Request-ID'].length).toBeGreaterThan(0)
    expect(headers['X-Correlation-Id']).toBe(headers['X-Request-ID'])
  })

  it('sends the stored Bearer token when present', async () => {
    setToken('tok-123')
    expect(getToken()).toBe('tok-123')
    fetchMock.mockResolvedValue(okResponse([]))
    await fetchCourses()
    const headers = fetchMock.mock.calls[0][1].headers as Record<string, string>
    expect(headers.Authorization).toBe('Bearer tok-123')
  })

  it('throws ApiError with the server message on a non-2xx response', async () => {
    // Fresh Response per call: a Response body can only be read once.
    // Backend envelope: { error: { code, message, request_id } }.
    fetchMock.mockImplementation(() =>
      Promise.resolve(okResponse({ error: { code: 'unauthorized', message: 'a valid Bearer token is required' } }, 401)),
    )
    await expect(fetchCourses()).rejects.toThrow(ApiError)
    await expect(fetchCourses()).rejects.toThrow('a valid Bearer token is required')
  })

  it('throws ApiError on invalid JSON instead of returning garbage', async () => {
    fetchMock.mockResolvedValue(new Response('not json', { status: 200 }))
    await expect(request('/api/v1/health')).rejects.toThrow(ApiError)
  })
})

describe('M6 lab client (live endpoints)', () => {
  const fetchMock = vi.fn()
  const store = new Map<string, string>()
  const localStorageStub = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  }
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('localStorage', localStorageStub)
    store.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('fetchLab parses the real GET /api/v1/labs/{id} shape (state, definition_id)', async () => {
    fetchMock.mockResolvedValue(
      okResponse({ id: 'lab-1', name: 'VPC lab', state: 'READY', definition_id: 'vpc-101', created_at: '2026-11-21T00:00:00Z' }),
    )
    const lab = await fetchLab('lab-1')
    expect(lab.status).toBe('READY')
    expect(lab.definition_id).toBe('vpc-101')
    expect(lab.resources).toBeUndefined() // backend returns no resources yet
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs/lab-1')
  })

  it('fetchLab parses resources when the backend starts returning them', async () => {
    fetchMock.mockResolvedValue(
      okResponse({
        id: 'lab-1', name: 'VPC lab', state: 'RUNNING',
        resources: [{ id: 'vpc-1', kind: 'vpc', name: 'main-vpc', state: 'available' }],
      }),
    )
    const lab = await fetchLab('lab-1')
    expect(lab.resources).toHaveLength(1)
    expect(lab.resources?.[0].kind).toBe('vpc')
  })

  it('parseLabResource normalises kind/type and skips resourceless entries', () => {
    expect(parseLabResource({ id: 'i-1', type: 'instance', name: 'web', private_ip: '10.0.0.5' })).toMatchObject({
      kind: 'instance',
      private_ip: '10.0.0.5',
    })
    expect(parseLabResource({ id: 'x', kind: 'warp-drive', name: '?' })?.kind).toBe('unknown')
    expect(parseLabResource({ kind: 'vpc' })).toBeNull()
    expect(parseLabResourceList({ resources: [{ id: 's-1', kind: 'subnet', name: 'sub' }, { kind: 'vpc' }] })).toHaveLength(1)
  })

  it('fetchCourseDetail keeps lesson titles from the detail view', async () => {
    fetchMock.mockResolvedValue(
      okResponse({ id: 'vpc-101', title: 'VPC', track: 'networking', description: 'd', lessons: ['Intro', 'Subnets'], lesson_count: 2 }),
    )
    const c = await fetchCourseDetail('vpc-101')
    expect(c.lesson_titles).toEqual(['Intro', 'Subnets'])
    expect(c.lessons).toBe(2)
  })

  it('parseCourseDetail tolerates a list-view payload (no titles)', () => {
    expect(parseCourseDetail({ id: 'x', title: 'T', track: 't', description: '', lesson_count: 3 }).lesson_titles).toEqual([])
  })
})

describe('M6 live lab endpoints', () => {
  const fetchMock = vi.fn()
  const store = new Map<string, string>()
  const localStorageStub = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  }
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('localStorage', localStorageStub)
    store.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('a 404 from a not-yet-routed endpoint is recognised as pending, not a generic failure', async () => {
    // The M6 lab endpoints are live (M9 Part 3); this helper is kept
    // for genuinely unrouted paths, which still surface as 404/501.
    // Fresh Response per call: a Response body can only be read once.
    fetchMock.mockImplementation(() =>
      Promise.resolve(okResponse({ error: { code: 'not_found', message: '404 page not found' } }, 404)),
    )
    await expect(fetchLabTopology('lab-1')).rejects.toThrow(ApiError)
    let err: unknown
    try {
      await fetchLabTopology('lab-1')
    } catch (e) {
      err = e
    }
    expect(isPendingEndpointError(err)).toBe(true)
    expect(isPendingEndpointError(new ApiError(500, 'boom'))).toBe(false)
    expect(isPendingEndpointError(new Error('boom'))).toBe(false)
  })

  it('validateLab / createLabResource / fetchLabResources / requestTerminalToken hit the exact M6 paths', async () => {
    fetchMock.mockResolvedValue(okResponse({ lab_id: 'lab-1', passed: true, checks: [] }))
    await validateLab('lab-1')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs/lab-1/validate')
    fetchMock.mockResolvedValue(okResponse({ id: 'i-1', kind: 'instance', name: 'web' }))
    await createLabResource('lab-1', { kind: 'instance', name: 'web' })
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/labs/lab-1/resources')
    expect(JSON.parse(fetchMock.mock.calls[1][1].body as string)).toEqual({ kind: 'instance', name: 'web' })
    fetchMock.mockResolvedValue(okResponse({ resources: [] }))
    await fetchLabResources('lab-1')
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/labs/lab-1/resources')
    fetchMock.mockResolvedValue(okResponse({ token: 'short-lived', expires_at: '2026-11-21T21:00:00Z' }))
    const tok = await requestTerminalToken('lab-1')
    expect(tok.token).toBe('short-lived')
    expect(fetchMock.mock.calls[3][0]).toBe('/api/v1/labs/lab-1/terminal/token')
  })

  it('buildTerminalWsUrl is a relative same-origin URL by default', () => {
    expect(buildTerminalWsUrl('lab-1', 'tok abc')).toBe(
      '/api/v1/labs/lab-1/terminal?token=tok%20abc',
    )
  })

  it('buildTerminalWsUrl derives ws/wss from an explicit API base and encodes the token', () => {
    vi.stubEnv('VITE_API_URL', 'http://localhost:8080')
    try {
      expect(buildTerminalWsUrl('lab-1', 'tok abc')).toBe(
        'ws://localhost:8080/api/v1/labs/lab-1/terminal?token=tok%20abc',
      )
    } finally {
      vi.unstubAllEnvs()
    }
  })
})
