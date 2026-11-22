/**
 * Shared types for the CloudForge frontend.
 *
 * These mirror the Go control-plane REST contract, verified against
 * backend/internal/httpapi/httpapi.go, backend/internal/courses/courses.go
 * and backend/internal/labs/labs.go (landed 2026-11-21):
 * - health: {service, version, ok, uptime_seconds,
 *   network_engine_reachable, network_engine_detail}
 * - courses list: {courses: [{id, title, track, description,
 *   lesson_count}]} — list views omit `lessons`; detail views send
 *   `lessons` as a title list plus `lesson_count`.
 * - labs list: {labs: [{id, name, state, definition_id, error?,
 *   created_at}]} — the field is `state`, not `status`.
 * - errors: {error: {code, message, request_id}}.
 * - correlation: the API reads/echoes `X-Request-ID`.
 * If the backend contract changes, update these types and the
 * parsers in src/services/api.ts together.
 */

/** GET /api/v1/health */
export interface Health {
  service: string
  version: string
  ok: boolean
  /** Seconds since the API process started. */
  uptime_seconds: number
  /** True when the Go API could reach the Rust network engine (gRPC Health). */
  network_engine_reachable: boolean
  /** Optional human-readable detail from the backend. */
  detail?: string
  /** True when the API runs in lightweight (classroom) mode: 1 lab, Micro, 30 min. */
  lite?: boolean
}

/**
 * A launchable lab definition. Mirrors backend/internal/labs/labs.go
 * `Definitions` — the backend is the source of truth for durations;
 * this copy exists only so the picker can render titles/descriptions
 * without a dedicated definitions endpoint. If the backend adds a
 * definition, add it here too.
 */
export interface LabDefinition {
  id: string
  title: string
  description: string
  /** Duration in minutes; lite mode only allows definitions of 30 or less. */
  duration_minutes: number
}

export const LAB_DEFINITIONS: LabDefinition[] = [
  {
    id: 'ec2-basics',
    title: 'Launch a Linux web server',
    description: 'Launch a KVM-backed Linux instance and serve a web page.',
    duration_minutes: 30,
  },
  {
    id: 'iam-security',
    title: 'Fix a broken IAM policy',
    description: 'Diagnose an access-denied error and apply least privilege.',
    duration_minutes: 30,
  },
  {
    id: 'vpc-fundamentals',
    title: 'Build a VPC with public and private subnets',
    description: 'Create an isolated VPC, two subnets, route tables and a NAT gateway.',
    duration_minutes: 60,
  },
  {
    id: 'three-tier-app',
    title: 'Build a three-tier application',
    description: 'Web, application and database tiers across public and private subnets.',
    duration_minutes: 120,
  },
]

/** The longest lab duration lite mode allows (minutes). */
export const LITE_MAX_LAB_MINUTES = 30

export function findLabDefinition(id: string): LabDefinition | undefined {
  return LAB_DEFINITIONS.find((d) => d.id === id)
}

/** True when a definition cannot be launched in lite mode. */
export function isDefinitionDisabledInLite(def: LabDefinition, lite: boolean): boolean {
  return lite && def.duration_minutes > LITE_MAX_LAB_MINUTES
}

/** A course in the CloudForge curriculum. */
export interface Course {
  id: string
  title: string
  /** Curriculum track, e.g. "networking", "iam", "ec2". */
  track: string
  description: string
  /** Number of lessons in the course. */
  lessons: number
}

/** Lab lifecycle states (see docs/architecture/overview.md). */
export type LabStatus =
  | 'CREATED'
  | 'PROVISIONING'
  | 'READY'
  | 'RUNNING'
  | 'VALIDATING'
  | 'COMPLETED'
  | 'STOPPING'
  | 'STOPPED'
  | 'FAILED'
  | 'EXPIRED'
  | 'DESTROYING'
  | 'DESTROYED'

/** A lab session owned by the signed-in user. GET /api/v1/labs.
 *  Mirrors backend/internal/labs.Lab: `state` (not `status`),
 *  `definition_id`, optional `error` (set when provisioning FAILED). */
export interface Lab {
  id: string
  name: string
  status: LabStatus
  definition_id?: string
  error?: string
  created_at?: string
  /**
   * Resources provisioned for the lab. The landed backend (M1/M2) does
   * NOT return this field — GET /api/v1/labs/{id} returns only the Lab
   * scalar fields above. It is parsed when present so the topology /
   * workstation UI lights up the moment the M6 backend step adds it,
   * and is otherwise `undefined` (never synthesised).
   */
  resources?: LabResource[]
}

/**
 * One provisioned resource inside a lab (VPC, subnet, instance, ...).
 * Kind strings are normalised by buildTopologyFromResources() in
 * src/services/topology.ts; unknown kinds render as `unknown`.
 */
export type LabResourceKind =
  | 'vpc'
  | 'subnet'
  | 'instance'
  | 'gateway'
  | 'loadbalancer'
  | 'route-table'
  | 'security-group'
  | 'unknown'

export interface LabResource {
  id: string
  kind: LabResourceKind
  name: string
  /** Lifecycle state of the resource, e.g. "available", "running". */
  state?: string
  private_ip?: string
  public_ip?: string
  /** Parent resource id (e.g. an instance's subnet, a subnet's VPC). */
  parent_id?: string
  /** CIDR block for vpc/subnet resources (from the resource view). */
  cidr?: string
  labels?: Record<string, string>
  /** Backend resource config: routes, SG rules, gateway type, etc. */
  config?: Record<string, unknown>
}

/** Course detail view: GET /api/v1/courses/{id} adds lesson titles. */
export interface CourseDetail extends Course {
  lesson_titles: string[]
}

/** A validation rule from a lab definition (M6 backend step). */
export interface ValidationRule {
  id: string
  description: string
}

/** Result of POST /api/v1/labs/{id}/validate (M6 backend step). */
export interface ValidationResult {
  lab_id: string
  passed: boolean
  checks: { rule_id: string; passed: boolean; message?: string }[]
}

/** Short-lived terminal token (M6 backend step). */
export interface TerminalToken {
  token: string
  expires_at?: string
}

/** Input for POST /api/v1/labs/{id}/resources (live since M6). */
export interface CreateLabResourceInput {
  kind: LabResourceKind
  name: string
  parent_id?: string
  /** CIDR for vpc/subnet kinds (required for subnets). */
  cidr?: string
  /** Kind-specific config (e.g. { gateway_type: 'igw' | 'nat' } for gateways). */
  config?: Record<string, unknown>
}

/** A node returned by InspectTopology (proto TopologyNode). */
export interface TopologyNode {
  id: string
  /** network | namespace | bridge | address */
  kind: string
  name: string
  labels?: Record<string, string>
}

export interface Topology {
  nodes: TopologyNode[]
}

export interface AuthUser {
  id: string
  email: string
  name?: string
  role?: string
  created_at?: string
}

/** One operation of a registry service (Phase 1: GET /api/v1/services). */
export interface RegistryOperation {
  operation: string
  fidelity: string
  supported: boolean
  http_route?: string
  limitations?: string[]
}

/** One service from the backend service registry (Phase 1). */
export interface RegistryService {
  id: string
  name: string
  category: string
  description: string
  icon: string
  route: string
  availability: 'available' | 'partial' | 'registry-only' | 'unavailable'
  fidelity: string
  operations: RegistryOperation[]
  resource_types: string[]
  docs_url: string
  learning_exercises: string[]
  mirrors?: string
  differs?: string
}

/** One row of GET /api/v1/explorer/resources (Phase 1, real data only). */
export interface ExplorerResource {
  id: string
  type: string
  name: string
  region: string
  status: string
  created_at?: string
  tags?: Record<string, string>
  provider: string
  fidelity: string
  lab_id?: string
  service_id: string
  detail_url: string
  can_delete: boolean
  delete_hint?: string
}

export interface ExplorerResult {
  resources: ExplorerResource[]
  total: number
  limit: number
  offset: number
  region: string
  region_scoped: boolean
  region_note?: string
}

/** An S3 bucket from GET /api/v1/aws/s3/buckets. */
export interface S3Bucket {
  name: string
  created_at?: string
}

/** IAM policy evaluation result (POST /api/v1/aws/iam/evaluate). */
export interface IamEvaluation {
  decision: string
  reason?: string
  matched_statement?: unknown
  /** Per-statement evaluation trail (Phase 2): which statement decided, and why each matched or not. */
  trail?: IamTrailEntry[]
  /** Stored policies the backend resolved for this evaluation. */
  policies_evaluated?: string[]
}

/** One entry of the IAM evaluation trail. */
export interface IamTrailEntry {
  policy_index: number
  statement_index: number
  effect: string
  matched: boolean
  reason: string
  decides: boolean
}

/** A CloudForge teaching IAM identity (Phase 2, /api/v1/aws/iam/*).
 *  These are NOT AWS IAM and NOT platform users: they are identities
 *  stored in Postgres (migration 008) for learning IAM concepts. */
export interface IamUser {
  id: string
  name: string
  path: string
  arn: string
  groups?: string[]
  policies?: string[]
  created_at: string
}

export interface IamGroup {
  id: string
  name: string
  path: string
  arn: string
  members?: string[]
  policies?: string[]
  created_at: string
}

export interface IamRole {
  id: string
  name: string
  path: string
  arn: string
  trust_policy: unknown
  max_session_duration_seconds: number
  policies?: string[]
  created_at: string
}

export interface IamPolicy {
  id: string
  name: string
  description?: string
  document: unknown
  created_at: string
}

export interface IamAccessKey {
  id: string
  user_id: string
  status: 'Active' | 'Inactive'
  created_at: string
  last_used_at?: string
}

/** The one-time secret returned at access-key creation (never stored). */
export interface IamAccessKeyCreated {
  access_key: IamAccessKey
  secret: string
  note?: string
}

/** A short-lived assumed-role session (Phase 2). */
export interface IamSession {
  id: string
  role_id: string
  user_id: string
  expires_at: string
  created_at: string
}

export interface IamSessionCreated {
  session_id: string
  role_id: string
  role_arn: string
  assumed_by: string
  token: string
  expires_at: string
  note?: string
}

/** One S3 object from GET /api/v1/aws/s3/buckets/{bucket}/objects. */
export interface S3Object {
  key: string
  size: number
  last_modified?: string
  etag?: string
  content_type?: string
  /** True for a "folder" prefix entry (derived from the key listing). */
  is_prefix?: boolean
  user_metadata?: Record<string, string>
}

/** One lab audit event (GET /api/v1/labs/{id}/events). */
export interface LabEvent {
  id: string
  actor_id: string
  action: string
  resource_type: string
  resource_id: string
  created_at: string
  metadata?: Record<string, unknown>
}

/** One component health entry from GET /api/v1/monitoring. */
export interface MonitoringComponent {
  status: string
  latency_ms?: number
  error?: string
}

export interface MetricSample {
  labels?: Record<string, string>
  value: number
}

export interface MetricFamily {
  name: string
  help?: string
  type: string
  total: number
  samples: MetricSample[]
}

/** A threshold alarm over a Prometheus metric (Phase 2).
 *  Definitions live in memory on the server and are lost on restart. */
export interface Alarm {
  id: string
  name: string
  metric: string
  threshold: number
  comparison: 'gt' | 'lt'
  created_by: string
  created_at: string
  /** Evaluated live on read: ok | alarm | no_data. */
  state: string
  current_value?: number
}

/** GET /api/v1/monitoring — live Prometheus series + readiness. */
export interface MonitoringData {
  status: string
  components: Record<string, MonitoringComponent>
  metrics: MetricFamily[]
  alarms: Alarm[]
  lite: boolean
  uptime_seconds: number
  version: string
  checked_at: string
  note?: string
}

/** Per-role quota set (GET /api/v1/admin/quotas, admin only). */
export interface QuotaSet {
  role: string
  max_labs: number
  max_cpu: number
  max_memory_mb: number
  max_disk_gb: number
  max_lab_hours: number
  max_lab_minutes: number
}

/** One audit entry (GET /api/v1/audit, admin only). */
export interface AuditEntry {
  id: string
  actor_id: string
  action: string
  resource_type: string
  resource_id: string
  created_at: string
}

export interface AuthResponse {
  token: string
  user?: AuthUser
}
