/**
 * Typed fetch client for the CloudForge Go API.
 *
 * - Base URL from VITE_API_URL; when it is unset or empty the client
 *   uses same-origin relative URLs (""), so the browser goes through
 *   the nginx reverse proxy in the Docker deployment (M9) or the Vite
 *   dev-server proxy under `npm run dev`. Set VITE_API_URL only for a
 *   split deployment where the API lives on another origin.
 * - Every request carries an X-Request-ID header (one UUID per request)
 *   — the header the Go API reads and echoes — plus the same value as
 *   X-Correlation-Id, so a request can be traced through the API,
 *   worker and Rust network engine logs.
 * - Auth token (when present) is sent as `Authorization: Bearer <token>`
 *   and is stored in localStorage under TOKEN_KEY.
 * - Responses are validated before use: a malformed health/courses/labs
 *   payload is a parse error, never silently treated as empty data.
 */

import type {
  AuthResponse,
  Course,
  CourseDetail,
  CreateLabResourceInput,
  Health,
  Lab,
  LabResource,
  LabResourceKind,
  LabStatus,
  TerminalToken,
  Topology,
  TopologyNode,
  ValidationResult,
} from '../types/index.ts'

export const TOKEN_KEY = 'cloudforge_token'

/**
 * API base URL with no trailing slash. Empty string means
 * same-origin: requests are relative URLs and go through the nginx
 * proxy (Docker) or the Vite dev-server proxy (`npm run dev`).
 */
export function apiBaseUrl(): string {
  const raw = import.meta.env.VITE_API_URL as string | undefined
  if (!raw || raw.trim() === '') return ''
  return raw.trim().replace(/\/+$/, '')
}

/** Absolute URL for an API path such as "/api/v1/health". */
export function buildUrl(path: string): string {
  const p = path.startsWith('/') ? path : `/${path}`
  return `${apiBaseUrl()}${p}`
}

export function newCorrelationId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `cf-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

interface RequestOptions {
  method?: string
  body?: unknown
  /** Attach the stored Bearer token. Defaults to true when a token exists. */
  auth?: boolean
}

async function parseJsonBody(res: Response): Promise<unknown> {
  const text = await res.text()
  if (text.trim() === '') return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    throw new ApiError(res.status, `Invalid JSON response from ${res.url || 'API'}`)
  }
}

function errorMessage(body: unknown, fallback: string): string {
  if (body && typeof body === 'object') {
    const rec = body as Record<string, unknown>
    // Backend error envelope: { error: { code, message, request_id } }
    if (isRecord(rec.error) && typeof rec.error.message === 'string' && rec.error.message !== '') {
      return rec.error.message
    }
    if (typeof rec.error === 'string' && rec.error !== '') return rec.error
    if (typeof rec.message === 'string' && rec.message !== '') return rec.message
  }
  return fallback
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    // The Go API reads X-Request-ID (and echoes it back); X-Correlation-Id
    // carries the same value for tracing through the worker/engine logs.
    'X-Request-ID': newCorrelationId(),
  }
  headers['X-Correlation-Id'] = headers['X-Request-ID']
  const useAuth = opts.auth !== false
  const token = useAuth ? getToken() : null
  if (token) headers.Authorization = `Bearer ${token}`

  const res = await fetch(buildUrl(path), {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })
  const body = await parseJsonBody(res)
  if (!res.ok) {
    throw new ApiError(res.status, errorMessage(body, `Request failed (${res.status})`))
  }
  return body as T
}

// ---------------------------------------------------------------------------
// Parsers: validate untrusted JSON into the typed shapes in src/types.
// ---------------------------------------------------------------------------

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function asString(v: unknown, field: string): string {
  if (typeof v !== 'string') throw new ApiError(0, `Invalid response: ${field} must be a string`)
  return v
}

export function parseHealth(data: unknown): Health {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: health must be an object')
  return {
    service: asString(data.service, 'service'),
    version: asString(data.version, 'version'),
    ok: typeof data.ok === 'boolean' ? data.ok : false,
    uptime_seconds: typeof data.uptime_seconds === 'number' ? data.uptime_seconds : 0,
    // Absent on older/partial backends: treat as unreachable, never as reachable.
    network_engine_reachable: data.network_engine_reachable === true,
    // Backend field is `network_engine_detail`; accept plain `detail` too.
    detail:
      typeof data.network_engine_detail === 'string'
        ? data.network_engine_detail
        : typeof data.detail === 'string'
          ? data.detail
          : undefined,
  }
}

function parseCourse(data: unknown): Course {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: course must be an object')
  // The Go list view omits `lessons` and sends `lesson_count`; the detail
  // view sends `lessons` as a title list. Normalise all shapes to a count.
  const lessonsRaw = data.lessons
  const lessons =
    typeof data.lesson_count === 'number'
      ? data.lesson_count
      : Array.isArray(lessonsRaw)
        ? lessonsRaw.length
        : typeof lessonsRaw === 'number'
          ? lessonsRaw
          : 0
  return {
    id: asString(data.id, 'course.id'),
    title: asString(data.title, 'course.title'),
    track: typeof data.track === 'string' ? data.track : '',
    description: typeof data.description === 'string' ? data.description : '',
    lessons,
  }
}

export function parseCourseList(data: unknown): Course[] {
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.courses)
      ? (data.courses as unknown[])
      : (() => {
          throw new ApiError(0, 'Invalid response: courses must be an array or { courses: [...] }')
        })()
  return items.map(parseCourse)
}

const LAB_STATUSES: LabStatus[] = [
  'CREATED', 'PROVISIONING', 'READY', 'RUNNING', 'VALIDATING', 'COMPLETED',
  'STOPPING', 'STOPPED', 'FAILED', 'EXPIRED', 'DESTROYING', 'DESTROYED',
]

function parseLab(data: unknown): Lab {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: lab must be an object')
  // Backend Lab JSON uses `state` (not `status`) and `definition_id`.
  const rawStatus = typeof data.state === 'string' ? data.state : (data.status as string | undefined)
  const status: LabStatus =
    rawStatus !== undefined && (LAB_STATUSES as string[]).includes(rawStatus)
      ? (rawStatus as LabStatus)
      : 'CREATED'
  const id = asString(data.id, 'lab.id')
  // `resources` is NOT returned by the landed backend yet (see Lab in
  // src/types). Parse it when present so the UI lights up when the M6
  // backend step adds it; absent means "unknown", never "empty by fiat".
  const resourcesRaw = data.resources
  const resources = Array.isArray(resourcesRaw)
    ? resourcesRaw.map((r) => parseLabResource(r)).filter((r) => r !== null)
    : undefined
  return {
    id,
    name: typeof data.name === 'string' ? data.name : (typeof data.title === 'string' ? data.title : id),
    status,
    definition_id: typeof data.definition_id === 'string' ? data.definition_id : undefined,
    error: typeof data.error === 'string' && data.error !== '' ? data.error : undefined,
    created_at: typeof data.created_at === 'string' ? data.created_at : undefined,
    resources,
  }
}

const RESOURCE_KINDS: LabResourceKind[] = ['vpc', 'subnet', 'instance', 'gateway', 'loadbalancer']

/** Parse one lab resource; returns null (skipped) when it has no id. */
export function parseLabResource(data: unknown): LabResource | null {
  if (!isRecord(data)) return null
  const id = typeof data.id === 'string' ? data.id : ''
  if (id === '') return null
  const rawKind = typeof data.kind === 'string' ? data.kind : (typeof data.type === 'string' ? data.type : '')
  const kind: LabResourceKind = (RESOURCE_KINDS as string[]).includes(rawKind)
    ? (rawKind as LabResourceKind)
    : 'unknown'
  return {
    id,
    kind,
    name: typeof data.name === 'string' && data.name !== '' ? data.name : id,
    state: typeof data.state === 'string' ? data.state : (typeof data.status === 'string' ? data.status : undefined),
    private_ip: typeof data.private_ip === 'string' ? data.private_ip : undefined,
    public_ip: typeof data.public_ip === 'string' ? data.public_ip : undefined,
    parent_id: typeof data.parent_id === 'string' ? data.parent_id : undefined,
    labels: isRecord(data.labels) ? (data.labels as Record<string, string>) : undefined,
  }
}

export function parseLabResourceList(data: unknown): LabResource[] {
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.resources)
      ? (data.resources as unknown[])
      : []
  return items.map(parseLabResource).filter((r): r is LabResource => r !== null)
}

export function parseCourseDetail(data: unknown): CourseDetail {
  const base = parseCourse(data)
  const titles = isRecord(data) && Array.isArray(data.lessons)
    ? (data.lessons as unknown[]).filter((t): t is string => typeof t === 'string')
    : []
  return { ...base, lesson_titles: titles }
}

function parseValidationResult(data: unknown): ValidationResult {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: validation result must be an object')
  const checksRaw = Array.isArray(data.checks) ? data.checks : []
  return {
    lab_id: typeof data.lab_id === 'string' ? data.lab_id : '',
    passed: data.passed === true,
    checks: checksRaw.filter(isRecord).map((c) => ({
      rule_id: typeof c.rule_id === 'string' ? c.rule_id : '',
      passed: c.passed === true,
      message: typeof c.message === 'string' ? c.message : undefined,
    })),
  }
}

function parseTerminalToken(data: unknown): TerminalToken {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: terminal token must be an object')
  const token = typeof data.token === 'string' ? data.token : ''
  if (token === '') throw new ApiError(0, 'Invalid response: terminal token is empty')
  return { token, expires_at: typeof data.expires_at === 'string' ? data.expires_at : undefined }
}

/**
 * True when an ApiError means "this endpoint is not routed yet"
 * (Go ServeMux returns 404 for unknown routes; a future backend may
 * use 501). Kept for genuinely unrouted paths: the M6/M7 lab
 * endpoints are live since M9 Part 3, so their failures surface as
 * real errors (e.g. 503 engine down), not as "pending".
 */
export function isPendingEndpointError(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 404 || err.status === 501)
}

export function parseLabList(data: unknown): Lab[] {
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.labs)
      ? (data.labs as unknown[])
      : (() => {
          throw new ApiError(0, 'Invalid response: labs must be an array or { labs: [...] }')
        })()
  return items.map(parseLab)
}

export function parseTopology(data: unknown): Topology {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: topology must be an object')
  const raw = Array.isArray(data.nodes) ? data.nodes : []
  const nodes: TopologyNode[] = raw
    .filter(isRecord)
    .map((n) => ({
      id: typeof n.id === 'string' ? n.id : '',
      kind: typeof n.kind === 'string' ? n.kind : 'unknown',
      name: typeof n.name === 'string' ? n.name : '',
      labels: isRecord(n.labels) ? (n.labels as Record<string, string>) : undefined,
    }))
    .filter((n) => n.id !== '')
  return { nodes }
}

// ---------------------------------------------------------------------------
// API calls
// ---------------------------------------------------------------------------

export async function fetchHealth(): Promise<Health> {
  const data = await request<unknown>('/api/v1/health')
  return parseHealth(data)
}

export async function fetchCourses(): Promise<Course[]> {
  const data = await request<unknown>('/api/v1/courses')
  return parseCourseList(data)
}

export async function fetchLabs(): Promise<Lab[]> {
  const data = await request<unknown>('/api/v1/labs')
  return parseLabList(data)
}

/** GET /api/v1/labs/{id} — the lab itself (no resources field yet). */
export async function fetchLab(id: string): Promise<Lab> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(id)}`)
  return parseLab(data)
}

/** GET /api/v1/courses/{id} — detail view with lesson titles. */
export async function fetchCourseDetail(id: string): Promise<CourseDetail> {
  const data = await request<unknown>(`/api/v1/courses/${encodeURIComponent(id)}`)
  return parseCourseDetail(data)
}

// ---------------------------------------------------------------------------
// Milestone 6/7 lab endpoints — LIVE since M6/M7 (backend
// internal/httpapi/{topology,resources}.go). The M9 Part 2 UI still
// carried "pending backend endpoint" wording for these; M9 Part 3
// wired the live feeds, so callers treat their responses as real:
// a 503 means the network engine / host agent is down (an honest
// error state to display), not a missing endpoint. A 404/501 still
// means "not routed" (see isPendingEndpointError) and is kept for
// genuinely unrouted paths.
// ---------------------------------------------------------------------------

/** GET /api/v1/labs/{id}/topology — live topology from the engine + DB resources. */
export async function fetchLabTopology(labId: string): Promise<Topology> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/topology`)
  return parseTopology(data)
}

/** GET /api/v1/labs/{id}/resources — live lab resource list. */
export async function fetchLabResources(labId: string): Promise<LabResource[]> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources`)
  return parseLabResourceList(data)
}

/** POST /api/v1/labs/{id}/resources — create a resource (engine/host-agent backed). */
export async function createLabResource(labId: string, input: CreateLabResourceInput): Promise<LabResource> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources`, {
    method: 'POST',
    body: input,
  })
  const parsed = parseLabResource(data)
  if (parsed === null) throw new ApiError(0, 'Invalid response: created resource must be an object with an id')
  return parsed
}

/** DELETE /api/v1/labs/{id}/resources/{resourceId} — idempotent delete. */
export async function deleteLabResource(labId: string, resourceId: string): Promise<void> {
  await request<unknown>(
    `/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(resourceId)}`,
    { method: 'DELETE' },
  )
}

/** POST /api/v1/labs/{id}/validate — run the lab's validation rules. */
export async function validateLab(labId: string): Promise<ValidationResult> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/validate`, { method: 'POST' })
  return parseValidationResult(data)
}

/** POST /api/v1/labs/{id}/terminal/token — live since the M6/M7 backend step.
 * Returns a short-lived, single-use token for the terminal WebSocket.
 * Terminal.tsx calls this before every (re)connect; the long-lived
 * Bearer <redacted> is never used as the WS token. */
export async function requestTerminalToken(labId: string): Promise<TerminalToken> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/terminal/token`, {
    method: 'POST',
  })
  return parseTerminalToken(data)
}

/**
 * WebSocket URL for the lab terminal: /api/v1/labs/{id}/terminal.
 * `token` MUST be the short-lived token from requestTerminalToken()
 * (M9 Part 2) — the backend rejects the long-lived Bearer <redacted> here.
 * The scheme follows the API base URL (ws:// for http://, wss:// for
 * https://). With a same-origin base (VITE_API_URL unset) the URL is
 * relative, which the browser resolves against the page origin.
 */
export function buildTerminalWsUrl(labId: string, token: string): string {
  const base = apiBaseUrl()
  if (base === '') {
    return `/api/v1/labs/${encodeURIComponent(labId)}/terminal?token=${encodeURIComponent(token)}`
  }
  const wsBase = base.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:')
  return `${wsBase}/api/v1/labs/${encodeURIComponent(labId)}/terminal?token=${encodeURIComponent(token)}`
}

/**
 * InspectTopology data for the Cloud Console page (M1).
 * The endpoint is best-effort in M1: a 404/501 means the backend does
 * not expose topology yet and the caller shows the M1 placeholder.
 */
export async function fetchTopology(): Promise<Topology> {
  const data = await request<unknown>('/api/v1/network/topology')
  return parseTopology(data)
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/api/v1/auth/login', {
    method: 'POST',
    body: { email, password },
    auth: false,
  })
  if (!data || typeof data.token !== 'string' || data.token === '') {
    throw new ApiError(0, 'Login succeeded but the server returned no token')
  }
  setToken(data.token)
  return data
}

export async function register(name: string, email: string, password: string): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/api/v1/auth/register', {
    method: 'POST',
    body: { name, email, password },
    auth: false,
  })
  if (!data || typeof data.token !== 'string' || data.token === '') {
    throw new ApiError(0, 'Registration succeeded but the server returned no token')
  }
  setToken(data.token)
  return data
}

export function logout(): void {
  clearToken()
}
