/**
 * Pure topology builder: lab resources -> React Flow nodes + edges.
 *
 * No network, no fake data. Given the lab's real resource list (from
 * GET /api/v1/labs/{id}, which today returns no `resources` field —
 * see src/types/index.ts), it lays resources out in kind columns
 * (VPC -> subnet -> gateway / load balancer -> instance) and draws an
 * edge for every resource that names a `parent_id`. Resources without
 * a parent get no edge rather than an invented one. Unknown kinds
 * render in their own column as `unknown`.
 *
 * Also maps the engine InspectTopology kinds (network / namespace /
 * bridge / address) onto the same columns, so the Console topology
 * payload can reuse this layout when that REST surface lands.
 */

import type { Edge, Node } from '@xyflow/react'
import type { Lab, LabResource, LabResourceKind, TopologyNode } from '../types/index.ts'

export interface FlowNodeData extends Record<string, unknown> {
  label: string
  kind: LabResourceKind
  state?: string
  ips: string[]
  draft: boolean
}

export type FlowNode = Node<FlowNodeData>
export type FlowEdge = Edge

const KIND_ORDER: LabResourceKind[] = ['vpc', 'subnet', 'gateway', 'loadbalancer', 'instance', 'unknown']
const COLUMN_X = 280
const ROW_Y = 120

const KIND_LABELS: Record<LabResourceKind, string> = {
  vpc: 'VPC',
  subnet: 'Subnet',
  instance: 'Instance',
  gateway: 'Gateway',
  loadbalancer: 'Load Balancer',
  'route-table': 'Route Table',
  'security-group': 'Security Group',
  unknown: 'Resource',
}

/** Normalise any backend/engine kind string to a flow column kind. */
export function normaliseKind(raw: string): LabResourceKind {
  const k = raw.trim().toLowerCase()
  if (k === 'vpc' || k === 'network') return 'vpc'
  if (k === 'subnet' || k === 'namespace') return 'subnet'
  if (k === 'instance' || k === 'ec2' || k === 'vm' || k === 'address') return 'instance'
  if (k === 'gateway' || k === 'igw' || k === 'nat' || k === 'bridge') return 'gateway'
  if (k === 'loadbalancer' || k === 'lb' || k === 'alb' || k === 'elb') return 'loadbalancer'
  return 'unknown'
}

export function kindLabel(kind: LabResourceKind): string {
  return KIND_LABELS[kind]
}

function ipsOf(r: LabResource): string[] {
  const ips: string[] = []
  if (r.private_ip) ips.push(r.private_ip)
  if (r.public_ip && r.public_ip !== r.private_ip) ips.push(r.public_ip)
  return ips
}

export function buildTopologyFromResources(resources: LabResource[]): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const byId = new Map(resources.map((r) => [r.id, r]))
  const rows = new Map<LabResourceKind, number>()
  const nodes: FlowNode[] = resources.map((r) => {
    const kind = normaliseKind(r.kind)
    const row = rows.get(kind) ?? 0
    rows.set(kind, row + 1)
    return {
      id: r.id,
      type: 'cfNode',
      position: { x: KIND_ORDER.indexOf(kind) * COLUMN_X, y: row * ROW_Y },
      data: {
        label: r.name || r.id,
        kind,
        state: r.state,
        ips: ipsOf(r),
        draft: false,
      },
    }
  })
  // Edges only where the resource names a real parent that exists.
  const edges: FlowEdge[] = resources
    .filter((r) => r.parent_id !== undefined && byId.has(r.parent_id))
    .map((r) => ({
      id: `e-${r.parent_id as string}-${r.id}`,
      source: r.parent_id as string,
      target: r.id,
      animated: false,
    }))
  return { nodes, edges }
}

/** Build a flow graph from a lab's own (possibly absent) resource list. */
export function buildTopologyFromLab(lab: Lab): { nodes: FlowNode[]; edges: FlowEdge[] } {
  return buildTopologyFromResources(lab.resources ?? [])
}

/** Build a flow graph from InspectTopology nodes (engine kinds). */
export function buildTopologyFromTopologyNodes(nodes: TopologyNode[]): { nodes: FlowNode[]; edges: FlowEdge[] } {
  return buildTopologyFromResources(
    nodes.map((n) => ({
      id: n.id,
      kind: normaliseKind(n.kind),
      name: n.name || n.id,
      labels: n.labels,
    })),
  )
}

/** A local draft node (not yet saved). The canvas no longer creates
 * drafts — POST /api/v1/labs/{id}/resources is live since M9 Part 3
 * — but the builder is kept for tests and offline sketching. */
export function makeDraftNode(kind: LabResourceKind, name: string): FlowNode {
  const id = `draft-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
  return {
    id,
    type: 'cfNode',
    position: { x: KIND_ORDER.indexOf(kind) * COLUMN_X, y: 0 },
    data: { label: name.trim() === '' ? `New ${kindLabel(kind)}` : name.trim(), kind, ips: [], draft: true },
  }
}
