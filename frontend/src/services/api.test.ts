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
  arrayBufferToBase64,
  assumeIamRole,
  buildTerminalWsUrl,
  buildUrl,
  clearToken,
  createIamUser,
  createLab,
  createLabResource,
  deleteS3Object,
  destroyLab,
  fetchCourseDetail,
  fetchIamPolicies,
  fetchIamUsers,
  fetchLabDefinitions,
  fetchLabEvents,
  fetchMonitoring,
  fetchS3Objects,
  fetchS3Versioning,
  parseAuditEntry,
  parseIamTrail,
  parseMonitoring,
  parseS3Object,
  startLab,
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
  logout,
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
    vi.stubEnv('VITE_API_URL', 'http://localhost:8081')
    try {
      expect(buildUrl('/api/v1/health')).toBe('http://localhost:8081/api/v1/health')
      expect(buildUrl('/api/v1/health')).not.toContain('8081//')
    } finally {
      vi.unstubAllEnvs()
    }
  })

  it('strips a trailing slash from an explicit VITE_API_URL', () => {
    vi.stubEnv('VITE_API_URL', 'http://localhost:8081/')
    try {
      expect(buildUrl('/api/v1/health')).toBe('http://localhost:8081/api/v1/health')
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
      lite: false,
    })
  })

  it('parses the lite flag from /api/v1/health (Launch-lab fix)', () => {
    expect(parseHealth({ service: 's', version: 'v', ok: true, uptime_seconds: 1, lite: true }).lite).toBe(true)
    expect(parseHealth({ service: 's', version: 'v', ok: true, uptime_seconds: 1 }).lite).toBe(false)
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

  it('logout POSTs to /auth/logout and clears the local token', async () => {
    setToken('tok-123')
    fetchMock.mockResolvedValue(okResponse({ logged_out: true, token_revoked: true }))
    await logout()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/auth/logout')
    expect(fetchMock.mock.calls[0][1].method).toBe('POST')
    expect(getToken()).toBeNull()
  })

  it('logout still clears the local token when the server call fails', async () => {
    setToken('tok-123')
    fetchMock.mockImplementation(() =>
      Promise.resolve(okResponse({ error: { code: 'unauthorized', message: 'gone' } }, 401)),
    )
    await logout()
    expect(getToken()).toBeNull()
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

describe('createLab (Launch-lab fix)', () => {
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
    setToken('tok-123')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  const labEnvelope = (state = 'PROVISIONING') => ({
    lab: { id: 'lab-1', name: 'Launch a Linux web server', state, definition_id: 'ec2-basics', created_at: '2026-11-21T00:00:00Z' },
    replayed: false,
  })

  it('POSTs { definition_id } to /api/v1/labs with the Bearer token and parses the { lab } envelope', async () => {
    fetchMock.mockResolvedValue(okResponse(labEnvelope(), 202))
    const lab = await createLab('ec2-basics')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs')
    expect(fetchMock.mock.calls[0][1].method).toBe('POST')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body as string)).toEqual({ definition_id: 'ec2-basics' })
    expect((fetchMock.mock.calls[0][1].headers as Record<string, string>).Authorization).toBe('Bearer tok-123')
    expect(lab.id).toBe('lab-1')
    expect(lab.status).toBe('PROVISIONING')
    expect(lab.definition_id).toBe('ec2-basics')
  })

  it('sends an optional name through', async () => {
    fetchMock.mockResolvedValue(okResponse(labEnvelope(), 201))
    await createLab('iam-security', 'My IAM lab')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body as string)).toEqual({
      definition_id: 'iam-security',
      name: 'My IAM lab',
    })
  })

  it('surfaces a 429 quota_exceeded as ApiError status 429 with the backend message', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        okResponse({ error: { code: 'quota_exceeded', message: 'lab duration 60 min exceeds the 30 min role limit' } }, 429),
      ),
    )
    let err: unknown
    try {
      await createLab('vpc-fundamentals')
    } catch (e) {
      err = e
    }
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(429)
    expect((err as Error).message).toContain('30 min')
  })

  it('surfaces a 400 unknown_definition as ApiError status 400', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(okResponse({ error: { code: 'unknown_definition', message: 'unknown lab definition_id' } }, 400)),
    )
    await expect(createLab('no-such-lab')).rejects.toMatchObject({ status: 400 })
  })

  it('surfaces a 401 as ApiError status 401', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        okResponse({ error: { code: 'unauthorized', message: 'a valid Bearer token is required' } }, 401),
      ),
    )
    await expect(createLab('ec2-basics')).rejects.toMatchObject({ status: 401 })
  })
})

describe('destroyLab (Destroy action)', () => {
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
    setToken('tok-123')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('POSTs to /api/v1/labs/{id}/destroy with the Bearer token and parses the bare lab', async () => {
    fetchMock.mockResolvedValue(
      okResponse({ id: 'lab-1', name: 'Launch a Linux web server', state: 'DESTROYING', definition_id: 'ec2-basics' }),
    )
    const lab = await destroyLab('lab-1')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs/lab-1/destroy')
    expect(fetchMock.mock.calls[0][1].method).toBe('POST')
    expect((fetchMock.mock.calls[0][1].headers as Record<string, string>).Authorization).toBe('Bearer tok-123')
    expect(lab.id).toBe('lab-1')
    expect(lab.status).toBe('DESTROYING')
  })

  it('surfaces a 404 lab_not_found as ApiError status 404', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(okResponse({ error: { code: 'lab_not_found', message: 'no lab with that id' } }, 404)),
    )
    await expect(destroyLab('missing')).rejects.toMatchObject({ status: 404 })
  })

  it('surfaces a 409 invalid_transition as ApiError status 409', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        okResponse({ error: { code: 'invalid_transition', message: 'invalid lab state transition: DESTROYED -> DESTROYING' } }, 409),
      ),
    )
    await expect(destroyLab('lab-1')).rejects.toMatchObject({ status: 409 })
  })

  it('surfaces a 401 as ApiError status 401', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        okResponse({ error: { code: 'unauthorized', message: 'a valid Bearer token is required' } }, 401),
      ),
    )
    await expect(destroyLab('lab-1')).rejects.toMatchObject({ status: 401 })
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
    vi.stubEnv('VITE_API_URL', 'http://localhost:8081')
    try {
      expect(buildTerminalWsUrl('lab-1', 'tok abc')).toBe(
        'ws://localhost:8081/api/v1/labs/lab-1/terminal?token=tok%20abc',
      )
    } finally {
      vi.unstubAllEnvs()
    }
  })
})

describe('Phase 2 parsers and endpoints', () => {
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
    clearToken()
  })

  afterEach(() => {
    clearToken()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it('parseLabResource keeps cidr and config (VPC routes/SG rules ride in config)', () => {
    const r = parseLabResource({
      id: 'rt-1',
      kind: 'route-table',
      name: 'rt',
      state: 'available',
      cidr: '10.0.0.0/16',
      config: { routes: [{ id: 'r1', destination: '0.0.0.0/0', target: 'local' }], engine_policy_applied: false },
    })
    expect(r?.cidr).toBe('10.0.0.0/16')
    if (r === null) throw new Error('expected a parsed resource')
    const routes: unknown = r.config?.routes
    expect(Array.isArray(routes)).toBe(true)
    expect((routes as unknown[]).length).toBe(1)
    expect(r.config?.engine_policy_applied).toBe(false)
  })

  it('parseIamTrail keeps the deciding statement', () => {
    const trail = parseIamTrail([
      { policy_index: 0, statement_index: 0, effect: 'Allow', matched: true, reason: 'action matched', decides: true },
      { policy_index: 0, statement_index: 1, effect: 'Deny', matched: false, reason: 'action did not match', decides: false },
    ])
    expect(trail?.length).toBe(2)
    expect(trail?.find((t) => t.decides)?.statement_index).toBe(0)
    expect(parseIamTrail('not-an-array')).toBeUndefined()
  })

  it('parseS3Object keeps prefix entries and metadata', () => {
    expect(parseS3Object({ key: 'photos/', is_prefix: true, size: 0 })).toMatchObject({ key: 'photos/', is_prefix: true })
    expect(parseS3Object({ key: 'a.txt', size: 12, content_type: 'text/plain', etag: 'abc' })).toMatchObject({
      key: 'a.txt',
      size: 12,
      content_type: 'text/plain',
    })
    expect(parseS3Object({ size: 1 })).toBeNull()
  })

  it('parseMonitoring keeps components, families and alarm states', () => {
    const m = parseMonitoring({
      status: 'ok',
      components: { postgres: { status: 'ok', latency_ms: 2 }, engine: { status: 'down', error: 'dial failed' } },
      metrics: [{ name: 'cloudforge_network_engine_up', type: 'gauge', total: 0, samples: [{ labels: {}, value: 0 }] }],
      alarms: [{ id: 'a1', name: 'engine-down', metric: 'cloudforge_network_engine_up', threshold: 0.5, comparison: 'gt', created_by: 'u1', created_at: '2026-11-22T00:00:00Z', state: 'ok' }],
      lite: true,
      uptime_seconds: 42,
      version: '0.5.0',
      checked_at: '2026-11-22T00:00:00Z',
    })
    expect(m.status).toBe('ok')
    expect(m.components.engine.status).toBe('down')
    expect(m.metrics[0].name).toBe('cloudforge_network_engine_up')
    expect(m.alarms[0].state).toBe('ok')
    expect(m.lite).toBe(true)
    expect(() => parseMonitoring(null)).toThrow(ApiError)
  })

  it('parseAuditEntry keeps metadata for lab events', () => {
    const e = parseAuditEntry({ id: 'e1', actor_id: 'u1', action: 'lab_created', resource_type: 'lab', resource_id: 'lab-1', created_at: '2026-11-22T00:00:00Z', metadata: { lab_id: 'lab-1' } })
    expect(e?.metadata).toEqual({ lab_id: 'lab-1' })
    expect(parseAuditEntry({ action: 'x' })).toBeNull()
  })

  it('arrayBufferToBase64 round-trips bytes', () => {
    const bytes = new TextEncoder().encode('hello')
    expect(arrayBufferToBase64(bytes.buffer as ArrayBuffer)).toBe('aGVsbG8=')
  })

  it('IAM CRUD helpers hit the exact Phase 2 paths', async () => {
    fetchMock.mockResolvedValue(okResponse({ users: [{ id: 'u1', name: 'alice', path: '/', arn: 'arn:cloudforge:iam::local:user/alice', created_at: '2026-11-22T00:00:00Z' }] }))
    const users = await fetchIamUsers()
    expect(users[0].name).toBe('alice')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/aws/iam/users')

    fetchMock.mockResolvedValue(okResponse({ id: 'u2', name: 'bob', path: '/', arn: 'arn:x', created_at: '' }))
    await createIamUser('bob')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/aws/iam/users')
    expect(JSON.parse(fetchMock.mock.calls[1][1].body as string)).toEqual({ name: 'bob' })

    fetchMock.mockResolvedValue(okResponse({ policies: [] }))
    await fetchIamPolicies()
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/aws/iam/policies')
  })

  it('assumeIamRole requires the one-time token in the response', async () => {
    fetchMock.mockResolvedValue(okResponse({ session_id: 's1', role_id: 'r1', role_arn: 'arn:x', assumed_by: 'u1', token: 'tok', expires_at: '2026-11-22T01:00:00Z' }))
    const s = await assumeIamRole('r1', 900)
    expect(s.token).toBe('tok')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/aws/iam/roles/r1/assume')
    fetchMock.mockResolvedValue(okResponse({ session_id: 's1' }))
    await expect(assumeIamRole('r1')).rejects.toThrow(ApiError)
  })

  it('S3 object helpers hit the exact Phase 2 paths', async () => {
    fetchMock.mockResolvedValue(okResponse({ bucket: 'b1', prefix: '', objects: [{ key: 'a.txt', size: 3 }] }))
    const objects = await fetchS3Objects('b1')
    expect(objects[0].key).toBe('a.txt')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/aws/s3/buckets/b1/objects')

    fetchMock.mockResolvedValue(okResponse({ bucket: 'b1', enabled: true }))
    expect(await fetchS3Versioning('b1')).toBe(true)
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/aws/s3/buckets/b1/versioning')

    fetchMock.mockResolvedValue(okResponse({ deleted: 'a.txt' }))
    await deleteS3Object('b1', 'a.txt')
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/aws/s3/buckets/b1/objects/a.txt')
  })

  it('fetchLabDefinitions and fetchLabEvents hit the Phase 2 paths', async () => {
    fetchMock.mockResolvedValue(okResponse({ definitions: [{ id: 'ec2-basics', title: 'Launch a Linux web server', description: 'd', duration_minutes: 30 }] }))
    const defs = await fetchLabDefinitions()
    expect(defs[0].id).toBe('ec2-basics')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs/definitions')

    fetchMock.mockResolvedValue(okResponse({ events: [{ id: 'e1', actor_id: 'u1', action: 'lab_created', resource_type: 'lab', resource_id: 'lab-1', created_at: '2026-11-22T00:00:00Z' }], total: 1 }))
    const { events, total } = await fetchLabEvents('lab-1')
    expect(total).toBe(1)
    expect(events[0].action).toBe('lab_created')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/labs/lab-1/events?limit=50')
  })

  it('startLab posts to the Phase 2 start route and parses the lab', async () => {
    fetchMock.mockResolvedValue(okResponse({ id: 'lab-1', name: 'x', state: 'PROVISIONING', definition_id: 'ec2-basics' }))
    const lab = await startLab('lab-1')
    expect(lab.status).toBe('PROVISIONING')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/labs/lab-1/start')
  })

  it('fetchMonitoring parses the live monitoring payload', async () => {
    fetchMock.mockResolvedValue(okResponse({ status: 'ok', components: {}, metrics: [], alarms: [], lite: false, uptime_seconds: 1, version: 'v', checked_at: '2026-11-22T00:00:00Z' }))
    const m = await fetchMonitoring()
    expect(m.status).toBe('ok')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/monitoring')
  })
})
