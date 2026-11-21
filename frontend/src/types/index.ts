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
  labels?: Record<string, string>
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
}

export interface AuthResponse {
  token: string
  user?: AuthUser
}
