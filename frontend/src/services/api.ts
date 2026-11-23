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
  Alarm,
  AuditEntry,
  AuthResponse,
  AuthUser,
  Course,
  CourseDetail,
  CreateLabResourceInput,
  ExplorerResource,
  ExplorerResult,
  Health,
  IamAccessKey,
  IamAccessKeyCreated,
  IamEvaluation,
  IamGroup,
  IamPolicy,
  IamRole,
  IamSession,
  IamSessionCreated,
  IamUser,
  Lab,
  LabDefinition,
  LabEvent,
  LabResource,
  LabResourceKind,
  LabStatus,
  MetricFamily,
  MonitoringData,
  QuotaSet,
  S3Bucket,
  S3Object,
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
    // /api/v1/health reports "lite": true in lightweight mode; absent
    // (older backends) means full mode, never lite by assumption.
    lite: data.lite === true,
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

const RESOURCE_KINDS: LabResourceKind[] = ['vpc', 'subnet', 'instance', 'gateway', 'loadbalancer', 'route-table', 'security-group']

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
    cidr: typeof data.cidr === 'string' ? data.cidr : undefined,
    labels: isRecord(data.labels) ? (data.labels as Record<string, string>) : undefined,
    config: isRecord(data.config) ? (data.config as Record<string, unknown>) : undefined,
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

/**
 * POST /api/v1/labs/{id}/destroy — destroy a lab session.
 * Backend (handleDestroyLab): 200 with the bare lab (state
 * DESTROYING, then DESTROYED by the worker); 401 unauthenticated,
 * 404 lab_not_found (also for other users' labs), 409
 * invalid_transition when the lab cannot be destroyed from its
 * current state. The Labs page had no destroy action at all before
 * the Launch-lab fix — destroying was only possible via curl.
 */
export async function destroyLab(id: string): Promise<Lab> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(id)}/destroy`, {
    method: 'POST',
  })
  if (isRecord(data) && 'lab' in data) return parseLab(data.lab)
  return parseLab(data)
}

/**
 * POST /api/v1/labs — launch a lab from a definition.
 * Backend (handleCreateLab): 201 in-memory, 202 when the worker
 * provisions asynchronously, 200 when an idempotency replay hits an
 * existing lab; 400 unknown_definition, 401 unauthenticated, 429
 * quota_exceeded. The response envelope is { lab, replayed }.
 * Errors surface as ApiError with the backend's message, so callers
 * can show the quota / unknown-definition reason instead of failing
 * silently.
 */
export async function createLab(definitionId: string, name?: string): Promise<Lab> {
  const body: Record<string, string> = { definition_id: definitionId }
  if (name !== undefined && name !== '') body.name = name
  const data = await request<unknown>('/api/v1/labs', { method: 'POST', body })
  if (isRecord(data) && 'lab' in data) return parseLab(data.lab)
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

/**
 * POST /api/v1/auth/login. `asRoot` is the "Sign in as root" option
 * on the login page: with it checked, only an admin account signs
 * in, and the session acts as admin; unchecked (the default) the
 * session is learner-scoped even for an admin account.
 */
export async function login(email: string, password: string, asRoot = false): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/api/v1/auth/login', {
    method: 'POST',
    body: { email, password, as_root: asRoot },
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

/**
 * Phase 7: logout is now server-side. POST /auth/logout revokes
 * the session token (it stops working immediately, not at its 12h
 * expiry) and clears the HttpOnly session cookie; the localStorage
 * token is cleared too. A failed logout still clears locally —
 * the user asked to sign out, and a stale local token must never
 * survive it.
 */
export async function logout(): Promise<void> {
  try {
    await request('/api/v1/auth/logout', { method: 'POST' })
  } catch {
    // Best-effort: the local sign-out below still happens.
  }
  clearToken()
}

/** Phase 7: admin revokes every session issued to a user. */
export async function revokeUserSessions(userId: string): Promise<{ user_id: string; sessions_revoked: number }> {
  return request(`/api/v1/admin/users/${encodeURIComponent(userId)}/sessions/revoke`, { method: 'POST' })
}

export interface AuditExportJob {
  id: string
  format: string
  events: number
  total: number
  bytes: number
  created_at: string
  created_by: string
  filename: string
}

/** Phase 7: create a durable audit export job (admin; CSV or JSON). */
export async function createAuditExportJob(format: 'csv' | 'json'): Promise<AuditExportJob> {
  return request<AuditExportJob>(`/api/v1/audit/export/jobs?format=${format}`, { method: 'POST' })
}

/** Phase 7: list audit export jobs, newest first (admin). */
export async function fetchAuditExportJobs(): Promise<{ jobs: AuditExportJob[]; retention: number }> {
  const data = await request<unknown>('/api/v1/audit/export/jobs')
  if (!isRecord(data) || !Array.isArray(data.jobs)) throw new ApiError(0, 'Invalid response: jobs must be an array')
  return data as unknown as { jobs: AuditExportJob[]; retention: number }
}

/** Phase 7: download one audit export job file (admin). */
export async function downloadAuditExportJob(id: string): Promise<Blob> {
  const headers: Record<string, string> = { 'X-Request-ID': newCorrelationId() }
  headers['X-Correlation-Id'] = headers['X-Request-ID']
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(buildUrl(`/api/v1/audit/export/jobs/${encodeURIComponent(id)}`), {
    headers,
  })
  if (!res.ok) throw new ApiError(res.status, `Audit export download failed (${res.status})`)
  return res.blob()
}

// ---------------------------------------------------------------------------
// Phase 1 — console foundation: profile, explorer, S3, IAM, admin.
// ---------------------------------------------------------------------------

function parseAuthUser(data: unknown): AuthUser {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: user must be an object')
  return {
    id: asString(data.id, 'user.id'),
    email: typeof data.email === 'string' ? data.email : '',
    name: typeof data.name === 'string' ? data.name : undefined,
    role: typeof data.role === 'string' ? data.role : undefined,
    created_at: typeof data.created_at === 'string' ? data.created_at : undefined,
  }
}

/** GET /api/v1/auth/me — the signed-in user's profile + role. */
export async function fetchMe(): Promise<AuthUser> {
  const data = await request<unknown>('/api/v1/auth/me')
  return parseAuthUser(data)
}

/** GET /api/v1/users — admin only. */
export async function fetchUsers(): Promise<AuthUser[]> {
  const data = await request<unknown>('/api/v1/users')
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.users)
      ? (data.users as unknown[])
      : []
  return items.map(parseAuthUser)
}

/** GET /api/v1/admin/quotas — admin only. */
export async function fetchQuotas(): Promise<QuotaSet[]> {
  const data = await request<unknown>('/api/v1/admin/quotas')
  const items: unknown[] = isRecord(data) && Array.isArray(data.quotas) ? (data.quotas as unknown[]) : []
  return items.filter(isRecord).map((q) => ({
    role: typeof q.role === 'string' ? q.role : '',
    max_labs: typeof q.max_labs === 'number' ? q.max_labs : 0,
    max_cpu: typeof q.max_cpu === 'number' ? q.max_cpu : 0,
    max_memory_mb: typeof q.max_memory_mb === 'number' ? q.max_memory_mb : 0,
    max_disk_gb: typeof q.max_disk_gb === 'number' ? q.max_disk_gb : 0,
    max_lab_hours: typeof q.max_lab_hours === 'number' ? q.max_lab_hours : 0,
    max_lab_minutes: typeof q.max_lab_minutes === 'number' ? q.max_lab_minutes : 0,
  }))
}

/** GET /api/v1/audit — admin only. Accepts a plain limit or a filter object (Phase 2). */
export interface AuditFilter {
  limit?: number
  offset?: number
  actor_id?: string
  action?: string
  resource_type?: string
  resource_id?: string
  lab_id?: string
  q?: string
  from?: string
  to?: string
}

export async function fetchAudit(limitOrFilter: number | AuditFilter = 50): Promise<{ entries: AuditEntry[]; total: number }> {
  const f: AuditFilter = typeof limitOrFilter === 'number' ? { limit: limitOrFilter } : limitOrFilter
  const params = new URLSearchParams()
  params.set('limit', String(f.limit ?? 50))
  if (f.offset) params.set('offset', String(f.offset))
  if (f.actor_id) params.set('actor_id', f.actor_id)
  if (f.action) params.set('action', f.action)
  if (f.resource_type) params.set('resource_type', f.resource_type)
  if (f.resource_id) params.set('resource_id', f.resource_id)
  if (f.lab_id) params.set('lab_id', f.lab_id)
  if (f.q) params.set('q', f.q)
  if (f.from) params.set('from', f.from)
  if (f.to) params.set('to', f.to)
  const data = await request<unknown>(`/api/v1/audit?${params.toString()}`)
  const items: unknown[] = isRecord(data) && Array.isArray(data.audit) ? (data.audit as unknown[]) : []
  return {
    entries: items.filter(isRecord).map((a) => ({
      id: typeof a.id === 'string' ? a.id : '',
      actor_id: typeof a.actor_id === 'string' ? a.actor_id : '',
      action: typeof a.action === 'string' ? a.action : '',
      resource_type: typeof a.resource_type === 'string' ? a.resource_type : '',
      resource_id: typeof a.resource_id === 'string' ? a.resource_id : '',
      created_at: typeof a.created_at === 'string' ? a.created_at : '',
    })),
    total: typeof data === 'object' && data !== null && typeof (data as Record<string, unknown>).total === 'number'
      ? ((data as Record<string, unknown>).total as number)
      : items.length,
  }
}

/** POST /api/v1/labs/{id}/stop — READY/RUNNING → STOPPED. */
export async function stopLab(id: string): Promise<Lab> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(id)}/stop`, { method: 'POST' })
  if (isRecord(data) && 'lab' in data) return parseLab(data.lab)
  return parseLab(data)
}

export interface ExplorerQuery {
  q?: string
  type?: string
  status?: string
  region?: string
  sort?: string
  limit?: number
  offset?: number
}

function parseExplorerResource(data: unknown): ExplorerResource | null {
  if (!isRecord(data)) return null
  const id = typeof data.id === 'string' ? data.id : ''
  if (id === '') return null
  return {
    id,
    type: typeof data.type === 'string' ? data.type : 'unknown',
    name: typeof data.name === 'string' && data.name !== '' ? data.name : id,
    region: typeof data.region === 'string' ? data.region : 'cf-local-1',
    status: typeof data.status === 'string' ? data.status : '',
    created_at: typeof data.created_at === 'string' ? data.created_at : undefined,
    tags: isRecord(data.tags) ? (data.tags as Record<string, string>) : undefined,
    provider: typeof data.provider === 'string' ? data.provider : 'cloudforge',
    fidelity: typeof data.fidelity === 'string' ? data.fidelity : 'UNSUPPORTED',
    lab_id: typeof data.lab_id === 'string' ? data.lab_id : undefined,
    service_id: typeof data.service_id === 'string' ? data.service_id : '',
    detail_url: typeof data.detail_url === 'string' ? data.detail_url : '',
    can_delete: data.can_delete === true,
    delete_hint: typeof data.delete_hint === 'string' ? data.delete_hint : undefined,
  }
}

/** GET /api/v1/explorer/resources — real backend resources only. */
export async function fetchExplorerResources(query: ExplorerQuery = {}): Promise<ExplorerResult> {
  const params = new URLSearchParams()
  if (query.q) params.set('q', query.q)
  if (query.type) params.set('type', query.type)
  if (query.status) params.set('status', query.status)
  if (query.region) params.set('region', query.region)
  if (query.sort) params.set('sort', query.sort)
  if (query.limit) params.set('limit', String(query.limit))
  if (query.offset) params.set('offset', String(query.offset))
  const suffix = params.toString() === '' ? '' : `?${params.toString()}`
  const data = await request<unknown>(`/api/v1/explorer/resources${suffix}`)
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: explorer result must be an object')
  const items: unknown[] = Array.isArray(data.resources) ? data.resources : []
  return {
    resources: items.map(parseExplorerResource).filter((r): r is ExplorerResource => r !== null),
    total: typeof data.total === 'number' ? data.total : 0,
    limit: typeof data.limit === 'number' ? data.limit : 50,
    offset: typeof data.offset === 'number' ? data.offset : 0,
    region: typeof data.region === 'string' ? data.region : 'cf-local-1',
    region_scoped: data.region_scoped === true,
    region_note: typeof data.region_note === 'string' && data.region_note !== '' ? data.region_note : undefined,
  }
}

/** GET /api/v1/aws/s3/buckets — 503 with capability info when MinIO is down. */
export async function fetchS3Buckets(): Promise<S3Bucket[]> {
  const data = await request<unknown>('/api/v1/aws/s3/buckets')
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.buckets)
      ? (data.buckets as unknown[])
      : []
  return items
    .filter(isRecord)
    .map((b) => ({
      name: typeof b.name === 'string' ? b.name : '',
      created_at: typeof b.created_at === 'string' ? b.created_at : undefined,
    }))
    .filter((b) => b.name !== '')
}

/** POST /api/v1/aws/s3/buckets — create a bucket in local MinIO. */
export async function createS3Bucket(name: string): Promise<S3Bucket> {
  const data = await request<unknown>('/api/v1/aws/s3/buckets', { method: 'POST', body: { name } })
  // The backend answers {bucket: "<name>"} (201).
  if (isRecord(data) && typeof data.bucket === 'string') {
    return { name: data.bucket }
  }
  if (isRecord(data) && typeof data.name === 'string') {
    return { name: data.name, created_at: typeof data.created_at === 'string' ? data.created_at : undefined }
  }
  return { name }
}

/** POST /api/v1/aws/iam/evaluate — real evaluation of the IAM subset (Phase 2: resolves stored identities, returns the trail). */
export async function evaluateIamPolicy(input: {
  policies?: unknown[]
  user_id?: string
  group_ids?: string[]
  role_ids?: string[]
  policy_ids?: string[]
  action: string
  resource: string
  username?: string
}): Promise<IamEvaluation> {
  const data = await request<unknown>('/api/v1/aws/iam/evaluate', { method: 'POST', body: input })
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: IAM evaluation must be an object')
  return {
    decision: typeof data.decision === 'string' ? data.decision : 'Deny',
    reason: typeof data.reason === 'string' ? data.reason : undefined,
    matched_statement: data.matched_statement,
    trail: parseIamTrail(data.trail),
    policies_evaluated: Array.isArray(data.policies_evaluated)
      ? (data.policies_evaluated as unknown[]).filter((p): p is string => typeof p === 'string')
      : undefined,
  }
}

export function parseIamTrail(data: unknown): IamEvaluation['trail'] {
  if (!Array.isArray(data)) return undefined
  return data
    .filter(isRecord)
    .map((t) => ({
      policy_index: typeof t.policy_index === 'number' ? t.policy_index : 0,
      statement_index: typeof t.statement_index === 'number' ? t.statement_index : 0,
      effect: typeof t.effect === 'string' ? t.effect : '',
      matched: t.matched === true,
      reason: typeof t.reason === 'string' ? t.reason : '',
      decides: t.decides === true,
    }))
}

// ---------------------------------------------------------------------------
// Phase 2 — IAM, S3 objects, VPC detail, EC2 start/reboot, monitoring, audit
// ---------------------------------------------------------------------------

function parseIamUser(data: unknown): IamUser | null {
  if (!isRecord(data) || typeof data.id !== 'string' || typeof data.name !== 'string') return null
  return {
    id: data.id,
    name: data.name,
    path: typeof data.path === 'string' ? data.path : '/',
    arn: typeof data.arn === 'string' ? data.arn : '',
    groups: strList(data.groups),
    policies: strList(data.policies),
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
  }
}

function parseIamGroup(data: unknown): IamGroup | null {
  if (!isRecord(data) || typeof data.id !== 'string' || typeof data.name !== 'string') return null
  return {
    id: data.id,
    name: data.name,
    path: typeof data.path === 'string' ? data.path : '/',
    arn: typeof data.arn === 'string' ? data.arn : '',
    members: strList(data.members),
    policies: strList(data.policies),
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
  }
}

function parseIamRole(data: unknown): IamRole | null {
  if (!isRecord(data) || typeof data.id !== 'string' || typeof data.name !== 'string') return null
  return {
    id: data.id,
    name: data.name,
    path: typeof data.path === 'string' ? data.path : '/',
    arn: typeof data.arn === 'string' ? data.arn : '',
    trust_policy: data.trust_policy ?? null,
    max_session_duration_seconds: typeof data.max_session_duration_seconds === 'number' ? data.max_session_duration_seconds : 3600,
    policies: strList(data.policies),
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
  }
}

function parseIamPolicy(data: unknown): IamPolicy | null {
  if (!isRecord(data) || typeof data.id !== 'string' || typeof data.name !== 'string') return null
  return {
    id: data.id,
    name: data.name,
    description: typeof data.description === 'string' ? data.description : undefined,
    document: data.document ?? null,
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
  }
}

function parseIamAccessKey(data: unknown): IamAccessKey | null {
  if (!isRecord(data) || typeof data.id !== 'string') return null
  return {
    id: data.id,
    user_id: typeof data.user_id === 'string' ? data.user_id : '',
    status: data.status === 'Inactive' ? 'Inactive' : 'Active',
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
    last_used_at: typeof data.last_used_at === 'string' ? data.last_used_at : undefined,
  }
}

function strList(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  return v.filter((x): x is string => typeof x === 'string')
}

function listField<T>(data: unknown, field: string, parse: (v: unknown) => T | null): T[] {
  if (!isRecord(data) || !Array.isArray(data[field])) return []
  return (data[field] as unknown[]).map(parse).filter((x): x is T => x !== null)
}

const iamBase = '/api/v1/aws/iam'

/** GET /api/v1/aws/iam/users — admin only. */
export async function fetchIamUsers(): Promise<IamUser[]> {
  return listField(await request<unknown>(`${iamBase}/users`), 'users', parseIamUser)
}

/** POST /api/v1/aws/iam/users — admin only. */
export async function createIamUser(name: string): Promise<IamUser> {
  const created = parseIamUser(await request<unknown>(`${iamBase}/users`, { method: 'POST', body: { name } }))
  if (!created) throw new ApiError(0, 'Invalid response: IAM user must be an object')
  return created
}

/** DELETE /api/v1/aws/iam/users/{id} — admin only. */
export async function deleteIamUser(id: string): Promise<void> {
  await request<unknown>(`${iamBase}/users/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** GET /api/v1/aws/iam/groups — admin only. */
export async function fetchIamGroups(): Promise<IamGroup[]> {
  return listField(await request<unknown>(`${iamBase}/groups`), 'groups', parseIamGroup)
}

export async function createIamGroup(name: string): Promise<IamGroup> {
  const created = parseIamGroup(await request<unknown>(`${iamBase}/groups`, { method: 'POST', body: { name } }))
  if (!created) throw new ApiError(0, 'Invalid response: IAM group must be an object')
  return created
}

export async function deleteIamGroup(id: string): Promise<void> {
  await request<unknown>(`${iamBase}/groups/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function addIamGroupMember(groupId: string, userId: string): Promise<void> {
  await request<unknown>(`${iamBase}/groups/${encodeURIComponent(groupId)}/members`, {
    method: 'POST',
    body: { user_id: userId },
  })
}

export async function removeIamGroupMember(groupId: string, userId: string): Promise<void> {
  await request<unknown>(`${iamBase}/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(userId)}`, {
    method: 'DELETE',
  })
}

/** GET /api/v1/aws/iam/roles — admin only. */
export async function fetchIamRoles(): Promise<IamRole[]> {
  return listField(await request<unknown>(`${iamBase}/roles`), 'roles', parseIamRole)
}

export async function createIamRole(name: string, trustPolicy: unknown, maxSessionSeconds?: number): Promise<IamRole> {
  const created = parseIamRole(
    await request<unknown>(`${iamBase}/roles`, {
      method: 'POST',
      body: { name, trust_policy: trustPolicy, max_session_duration_seconds: maxSessionSeconds },
    }),
  )
  if (!created) throw new ApiError(0, 'Invalid response: IAM role must be an object')
  return created
}

export async function deleteIamRole(id: string): Promise<void> {
  await request<unknown>(`${iamBase}/roles/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** GET /api/v1/aws/iam/policies — admin only. */
export async function fetchIamPolicies(): Promise<IamPolicy[]> {
  return listField(await request<unknown>(`${iamBase}/policies`), 'policies', parseIamPolicy)
}

export async function createIamPolicy(name: string, document: unknown, description?: string): Promise<IamPolicy> {
  const created = parseIamPolicy(
    await request<unknown>(`${iamBase}/policies`, {
      method: 'POST',
      body: { name, document, description },
    }),
  )
  if (!created) throw new ApiError(0, 'Invalid response: IAM policy must be an object')
  return created
}

export async function deleteIamPolicy(id: string): Promise<void> {
  await request<unknown>(`${iamBase}/policies/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** POST /api/v1/aws/iam/{kind}/{id}/policies — attach a stored policy (admin only). */
export async function attachIamPolicy(kind: 'user' | 'group' | 'role', id: string, policyId: string): Promise<void> {
  const plural = kind === 'user' ? 'users' : kind === 'group' ? 'groups' : 'roles'
  await request<unknown>(`${iamBase}/${plural}/${encodeURIComponent(id)}/policies`, {
    method: 'POST',
    body: { policy_id: policyId },
  })
}

export async function detachIamPolicy(kind: 'user' | 'group' | 'role', id: string, policyId: string): Promise<void> {
  const plural = kind === 'user' ? 'users' : kind === 'group' ? 'groups' : 'roles'
  await request<unknown>(`${iamBase}/${plural}/${encodeURIComponent(id)}/policies/${encodeURIComponent(policyId)}`, {
    method: 'DELETE',
  })
}

/** GET /api/v1/aws/iam/users/{id}/access-keys — admin only. Never returns a secret. */
export async function fetchIamAccessKeys(userId: string): Promise<IamAccessKey[]> {
  return listField(
    await request<unknown>(`${iamBase}/users/${encodeURIComponent(userId)}/access-keys`),
    'access_keys',
    parseIamAccessKey,
  )
}

/** POST .../access-keys — the secret is returned exactly once, here. */
export async function createIamAccessKey(userId: string): Promise<IamAccessKeyCreated> {
  const data = await request<unknown>(`${iamBase}/users/${encodeURIComponent(userId)}/access-keys`, { method: 'POST' })
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: access key creation must be an object')
  const key = parseIamAccessKey(data.access_key)
  if (!key || typeof data.secret !== 'string') throw new ApiError(0, 'Invalid response: access key creation must include the key and its one-time secret')
  return { access_key: key, secret: data.secret, note: typeof data.note === 'string' ? data.note : undefined }
}

export async function setIamAccessKeyStatus(userId: string, keyId: string, status: 'Active' | 'Inactive'): Promise<IamAccessKey> {
  const updated = parseIamAccessKey(
    await request<unknown>(`${iamBase}/users/${encodeURIComponent(userId)}/access-keys/${encodeURIComponent(keyId)}/status`, {
      method: 'POST',
      body: { status },
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: access key must be an object')
  return updated
}

export async function deleteIamAccessKey(userId: string, keyId: string): Promise<void> {
  await request<unknown>(`${iamBase}/users/${encodeURIComponent(userId)}/access-keys/${encodeURIComponent(keyId)}`, {
    method: 'DELETE',
  })
}

/** POST /api/v1/aws/iam/roles/{id}/assume — any authenticated user; the token is returned exactly once. */
export async function assumeIamRole(roleId: string, durationSeconds?: number): Promise<IamSessionCreated> {
  const data = await request<unknown>(`${iamBase}/roles/${encodeURIComponent(roleId)}/assume`, {
    method: 'POST',
    body: { duration_seconds: durationSeconds },
  })
  if (!isRecord(data) || typeof data.token !== 'string' || typeof data.session_id !== 'string') {
    throw new ApiError(0, 'Invalid response: assume-role must include a session id and one-time token')
  }
  return {
    session_id: data.session_id,
    role_id: typeof data.role_id === 'string' ? data.role_id : roleId,
    role_arn: typeof data.role_arn === 'string' ? data.role_arn : '',
    assumed_by: typeof data.assumed_by === 'string' ? data.assumed_by : '',
    token: data.token,
    expires_at: typeof data.expires_at === 'string' ? data.expires_at : '',
    note: typeof data.note === 'string' ? data.note : undefined,
  }
}

/** GET /api/v1/aws/iam/sessions/{id} — 410 when the session expired. */
export async function fetchIamSession(sessionId: string): Promise<{ session: IamSession; valid: boolean; expires_in_seconds: number }> {
  const data = await request<unknown>(`${iamBase}/sessions/${encodeURIComponent(sessionId)}`)
  if (!isRecord(data) || !isRecord(data.session)) throw new ApiError(0, 'Invalid response: session must be an object')
  const s = data.session
  return {
    session: {
      id: typeof s.id === 'string' ? s.id : sessionId,
      role_id: typeof s.role_id === 'string' ? s.role_id : '',
      user_id: typeof s.user_id === 'string' ? s.user_id : '',
      expires_at: typeof s.expires_at === 'string' ? s.expires_at : '',
      created_at: typeof s.created_at === 'string' ? s.created_at : '',
    },
    valid: data.valid === true,
    expires_in_seconds: typeof data.expires_in_seconds === 'number' ? data.expires_in_seconds : 0,
  }
}

// --- S3 objects (Phase 2) ----------------------------------------------------

export function parseS3Object(data: unknown): S3Object | null {
  if (!isRecord(data) || typeof data.key !== 'string') return null
  return {
    key: data.key,
    size: typeof data.size === 'number' ? data.size : 0,
    last_modified: typeof data.last_modified === 'string' ? data.last_modified : undefined,
    etag: typeof data.etag === 'string' ? data.etag : undefined,
    content_type: typeof data.content_type === 'string' ? data.content_type : undefined,
    is_prefix: data.is_prefix === true,
    user_metadata: isRecord(data.user_metadata) ? (data.user_metadata as Record<string, string>) : undefined,
  }
}

/** GET /api/v1/aws/s3/buckets/{bucket}/objects?prefix= — 503 when MinIO is down. */
export async function fetchS3Objects(bucket: string, prefix = ''): Promise<S3Object[]> {
  const suffix = prefix === '' ? '' : `?prefix=${encodeURIComponent(prefix)}`
  return listField(
    await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/objects${suffix}`),
    'objects',
    parseS3Object,
  )
}

/** POST .../objects — upload through the backend (base64, max 10 MiB). No browser→MinIO credentials. */
export async function uploadS3Object(bucket: string, key: string, bytes: ArrayBuffer, contentType?: string): Promise<{ key: string; size: number }> {
  const data = await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/objects`, {
    method: 'POST',
    body: { key, content_base64: arrayBufferToBase64(bytes), content_type: contentType ?? 'application/octet-stream' },
  })
  if (!isRecord(data) || typeof data.key !== 'string') throw new ApiError(0, 'Invalid response: upload must return the object key')
  return { key: data.key, size: typeof data.size === 'number' ? data.size : 0 }
}

export function arrayBufferToBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

/** GET .../objects/{key} — downloads the raw bytes (authenticated, through the backend). */
export async function downloadS3Object(bucket: string, key: string): Promise<Blob> {
  const headers: Record<string, string> = { 'X-Request-ID': newCorrelationId() }
  headers['X-Correlation-Id'] = headers['X-Request-ID']
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(buildUrl(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/objects/${key.split('/').map(encodeURIComponent).join('/')}`), { headers })
  if (!res.ok) {
    const body = await parseJsonBody(res).catch(() => null)
    throw new ApiError(res.status, errorMessage(body, `Download failed (${res.status})`))
  }
  return res.blob()
}

/** DELETE .../objects/{key} — idempotent on the adapter. */
export async function deleteS3Object(bucket: string, key: string): Promise<void> {
  await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/objects/${key.split('/').map(encodeURIComponent).join('/')}`, {
    method: 'DELETE',
  })
}

/** GET .../metadata/{key} — StatObject. */
export async function fetchS3ObjectMetadata(bucket: string, key: string): Promise<S3Object> {
  const meta = parseS3Object(
    await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/metadata/${key.split('/').map(encodeURIComponent).join('/')}`),
  )
  if (!meta) throw new ApiError(0, 'Invalid response: object metadata must be an object')
  return meta
}

/** GET/PUT .../versioning — the adapter supports it; the toggle is real. */
export async function fetchS3Versioning(bucket: string): Promise<boolean> {
  const data = await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/versioning`)
  if (!isRecord(data) || typeof data.enabled !== 'boolean') throw new ApiError(0, 'Invalid response: versioning must carry enabled')
  return data.enabled
}

export async function setS3Versioning(bucket: string, enabled: boolean): Promise<boolean> {
  const data = await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/versioning`, {
    method: 'PUT',
    body: { enabled },
  })
  if (!isRecord(data) || typeof data.enabled !== 'boolean') throw new ApiError(0, 'Invalid response: versioning must carry enabled')
  return data.enabled
}

/** GET/PUT .../policy — bucket policy JSON. Object ACLs are not exposed by the MinIO adapter. */
export async function fetchS3BucketPolicy(bucket: string): Promise<string> {
  const data = await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/policy`)
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: bucket policy must be an object')
  return typeof data.policy === 'string' ? data.policy : ''
}

export async function setS3BucketPolicy(bucket: string, policy: string): Promise<void> {
  await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}/policy`, {
    method: 'PUT',
    body: { policy },
  })
}

/** DELETE /api/v1/aws/s3/buckets/{bucket} — only when empty (MinIO refuses non-empty). */
export async function deleteS3Bucket(bucket: string): Promise<void> {
  await request<unknown>(`/api/v1/aws/s3/buckets/${encodeURIComponent(bucket)}`, { method: 'DELETE' })
}

// --- VPC detail (Phase 2) ----------------------------------------------------

/** POST /labs/{id}/resources/{resourceId}/routes — engine-owned; 502 when the engine is down, nothing persisted. */
export async function addVpcRoute(labId: string, routeTableId: string, destination: string, target: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(routeTableId)}/routes`, {
      method: 'POST',
      body: { destination, target },
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: route table must be a resource')
  return updated
}

export async function deleteVpcRoute(labId: string, routeTableId: string, routeId: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(routeTableId)}/routes/${encodeURIComponent(routeId)}`, {
      method: 'DELETE',
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: route table must be a resource')
  return updated
}

/** POST .../rules — SG rules are DB-authoritative; the response says whether the engine snapshot applied. */
export async function addSecurityGroupRule(
  labId: string,
  sgId: string,
  rule: { direction: 'ingress' | 'egress'; protocol: string; from_port: number; to_port: number; cidr?: string; source?: string },
): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(sgId)}/rules`, {
      method: 'POST',
      body: rule,
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: security group must be a resource')
  return updated
}

export async function deleteSecurityGroupRule(labId: string, sgId: string, ruleId: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(sgId)}/rules/${encodeURIComponent(ruleId)}`, {
      method: 'DELETE',
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: security group must be a resource')
  return updated
}

export async function attachSecurityGroupToInstance(labId: string, sgId: string, instanceId: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(sgId)}/attach-instance`, {
      method: 'POST',
      body: { instance_id: instanceId },
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: security group must be a resource')
  return updated
}

/** POST .../attach | .../detach — IGW attach/detach is recorded (SIMULATED); NAT goes through the engine. */
export async function attachGateway(labId: string, gatewayId: string, vpcId: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(gatewayId)}/attach`, {
      method: 'POST',
      body: { vpc_id: vpcId },
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: gateway must be a resource')
  return updated
}

export async function detachGateway(labId: string, gatewayId: string): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(gatewayId)}/detach`, {
      method: 'POST',
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: gateway must be a resource')
  return updated
}

/** PUT .../tags — replaces the tag set; keys starting with aws: are rejected by the backend. */
export async function setResourceTags(labId: string, resourceId: string, tags: Record<string, string>): Promise<LabResource> {
  const updated = parseLabResource(
    await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/resources/${encodeURIComponent(resourceId)}/tags`, {
      method: 'PUT',
      body: { tags },
    }),
  )
  if (!updated) throw new ApiError(0, 'Invalid response: resource must be a resource')
  return updated
}

// --- EC2 start/reboot, definitions, lab events (Phase 2) ---------------------

/** POST /api/v1/labs/{id}/start — 202 PROVISIONING; READY only when re-provisioning succeeds. */
export async function startLab(id: string): Promise<Lab> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(id)}/start`, { method: 'POST' })
  if (isRecord(data) && 'lab' in data) return parseLab(data.lab)
  return parseLab(data)
}

/** POST /api/v1/labs/{id}/reboot — real host-agent OpRebootVM; 503 when no host agent or no VM. */
export async function rebootLab(id: string): Promise<Lab> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(id)}/reboot`, { method: 'POST' })
  if (isRecord(data) && 'lab' in data) return parseLab(data.lab)
  return parseLab(data)
}

/** GET /api/v1/labs/definitions — the backend is the source of truth (replaces the hardcoded list for the wizard). */
export async function fetchLabDefinitions(): Promise<LabDefinition[]> {
  const data = await request<unknown>('/api/v1/labs/definitions')
  if (!isRecord(data) || !Array.isArray(data.definitions)) throw new ApiError(0, 'Invalid response: definitions must be an array')
  return (data.definitions as unknown[])
    .filter(isRecord)
    .map((d) => ({
      id: typeof d.id === 'string' ? d.id : '',
      title: typeof d.title === 'string' ? d.title : '',
      description: typeof d.description === 'string' ? d.description : '',
      duration_minutes: typeof d.duration_minutes === 'number' ? d.duration_minutes : 0,
    }))
    .filter((d) => d.id !== '')
}

export function parseAuditEntry(data: unknown): LabEvent | null {
  if (!isRecord(data) || typeof data.id !== 'string') return null
  return {
    id: data.id,
    actor_id: typeof data.actor_id === 'string' ? data.actor_id : '',
    action: typeof data.action === 'string' ? data.action : '',
    resource_type: typeof data.resource_type === 'string' ? data.resource_type : '',
    resource_id: typeof data.resource_id === 'string' ? data.resource_id : '',
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
    metadata: isRecord(data.metadata) ? (data.metadata as Record<string, unknown>) : undefined,
  }
}

/** GET /api/v1/labs/{id}/events — the lab's real audit history. */
export async function fetchLabEvents(labId: string, limit = 50): Promise<{ events: LabEvent[]; total: number }> {
  const data = await request<unknown>(`/api/v1/labs/${encodeURIComponent(labId)}/events?limit=${limit}`)
  const events = listField(data, 'events', parseAuditEntry)
  return { events, total: isRecord(data) && typeof data.total === 'number' ? data.total : events.length }
}

// --- Monitoring + audit export (Phase 2) --------------------------------------

export function parseMonitoring(data: unknown): MonitoringData {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: monitoring must be an object')
  const components: MonitoringData['components'] = {}
  if (isRecord(data.components)) {
    for (const [name, raw] of Object.entries(data.components)) {
      if (!isRecord(raw)) continue
      components[name] = {
        status: typeof raw.status === 'string' ? raw.status : 'unknown',
        latency_ms: typeof raw.latency_ms === 'number' ? raw.latency_ms : undefined,
        error: typeof raw.error === 'string' ? raw.error : undefined,
      }
    }
  }
  const metrics: MetricFamily[] = Array.isArray(data.metrics)
    ? (data.metrics as unknown[])
        .filter(isRecord)
        .map((m) => ({
          name: typeof m.name === 'string' ? m.name : '',
          help: typeof m.help === 'string' ? m.help : undefined,
          type: typeof m.type === 'string' ? m.type : '',
          total: typeof m.total === 'number' ? m.total : 0,
          samples: Array.isArray(m.samples)
            ? (m.samples as unknown[])
                .filter(isRecord)
                .map((s) => ({
                  labels: isRecord(s.labels) ? (s.labels as Record<string, string>) : undefined,
                  value: typeof s.value === 'number' ? s.value : 0,
                }))
            : [],
        }))
        .filter((m) => m.name !== '')
    : []
  const alarms: Alarm[] = Array.isArray(data.alarms)
    ? (data.alarms as unknown[])
        .filter(isRecord)
        .map((a) => ({
          id: typeof a.id === 'string' ? a.id : '',
          name: typeof a.name === 'string' ? a.name : '',
          metric: typeof a.metric === 'string' ? a.metric : '',
          threshold: typeof a.threshold === 'number' ? a.threshold : 0,
          comparison: (a.comparison === 'lt' ? 'lt' : 'gt') as 'gt' | 'lt',
          created_by: typeof a.created_by === 'string' ? a.created_by : '',
          created_at: typeof a.created_at === 'string' ? a.created_at : '',
          state: typeof a.state === 'string' ? a.state : 'no_data',
          current_value: typeof a.current_value === 'number' ? a.current_value : undefined,
        }))
        .filter((a) => a.id !== '')
    : []
  return {
    status: typeof data.status === 'string' ? data.status : 'unknown',
    components,
    metrics,
    alarms,
    lite: data.lite === true,
    uptime_seconds: typeof data.uptime_seconds === 'number' ? data.uptime_seconds : 0,
    version: typeof data.version === 'string' ? data.version : '',
    checked_at: typeof data.checked_at === 'string' ? data.checked_at : '',
    note: typeof data.note === 'string' ? data.note : undefined,
  }
}

/** GET /api/v1/monitoring — live Prometheus series + readiness, never zero-filled. */
export async function fetchMonitoring(): Promise<MonitoringData> {
  return parseMonitoring(await request<unknown>('/api/v1/monitoring'))
}

/** POST /api/v1/monitoring/alarms — rejected when the metric was never observed. */
export async function createAlarm(name: string, metric: string, threshold: number, comparison: 'gt' | 'lt'): Promise<Alarm> {
  const data = await request<unknown>('/api/v1/monitoring/alarms', {
    method: 'POST',
    body: { name, metric, threshold, comparison },
  })
  if (!isRecord(data) || typeof data.id !== 'string') throw new ApiError(0, 'Invalid response: alarm must be an object')
  return {
    id: data.id,
    name: typeof data.name === 'string' ? data.name : name,
    metric: typeof data.metric === 'string' ? data.metric : metric,
    threshold: typeof data.threshold === 'number' ? data.threshold : threshold,
    comparison: data.comparison === 'lt' ? 'lt' : 'gt',
    created_by: typeof data.created_by === 'string' ? data.created_by : '',
    created_at: typeof data.created_at === 'string' ? data.created_at : '',
    state: typeof data.state === 'string' ? data.state : 'no_data',
    current_value: typeof data.current_value === 'number' ? data.current_value : undefined,
  }
}

export async function deleteAlarm(id: string): Promise<void> {
  await request<unknown>(`/api/v1/monitoring/alarms/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** GET /api/v1/audit/export?format=csv|json — admin only; returns the raw file body. */
export async function downloadAuditExport(format: 'csv' | 'json'): Promise<Blob> {
  const headers: Record<string, string> = { 'X-Request-ID': newCorrelationId() }
  headers['X-Correlation-Id'] = headers['X-Request-ID']
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(buildUrl(`/api/v1/audit/export?format=${format}`), { headers })
  if (!res.ok) {
    const body = await parseJsonBody(res).catch(() => null)
    throw new ApiError(res.status, errorMessage(body, `Export failed (${res.status})`))
  }
  return res.blob()
}
