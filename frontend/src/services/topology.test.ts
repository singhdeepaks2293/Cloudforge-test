/**
 * Tests for the pure topology builder (src/services/topology.ts).
 *
 * Fixtures are lab-resource shapes as the backend will return them
 * (see LabResource in src/types/index.ts). No network, no fake
 * backend: the builder only lays out and links what it is given.
 */
import { describe, expect, it } from 'vitest'
import {
  buildTopologyFromLab,
  buildTopologyFromResources,
  buildTopologyFromTopologyNodes,
  makeDraftNode,
  normaliseKind,
} from './topology.ts'
import type { LabResource } from '../types/index.ts'

const fixtures: LabResource[] = [
  { id: 'vpc-1', kind: 'vpc', name: 'main-vpc', state: 'available' },
  { id: 'sub-1', kind: 'subnet', name: 'public-a', state: 'available', parent_id: 'vpc-1' },
  { id: 'sub-2', kind: 'subnet', name: 'private-a', state: 'available', parent_id: 'vpc-1' },
  { id: 'igw-1', kind: 'gateway', name: 'igw', parent_id: 'vpc-1' },
  { id: 'i-1', kind: 'instance', name: 'web-01', state: 'running', private_ip: '10.0.1.10', public_ip: '203.0.113.5', parent_id: 'sub-1' },
  { id: 'i-2', kind: 'instance', name: 'db-01', state: 'running', private_ip: '10.0.2.10', parent_id: 'sub-2' },
]

describe('buildTopologyFromResources', () => {
  it('creates one node per resource, in kind columns, with IPs in node data', () => {
    const { nodes } = buildTopologyFromResources(fixtures)
    expect(nodes).toHaveLength(6)
    const web = nodes.find((n) => n.id === 'i-1')
    expect(web?.data.label).toBe('web-01')
    expect(web?.data.kind).toBe('instance')
    expect(web?.data.state).toBe('running')
    expect(web?.data.ips).toEqual(['10.0.1.10', '203.0.113.5'])
    expect(web?.data.draft).toBe(false)
    // Kind columns: VPC left of subnet, subnet left of instance.
    const vpc = nodes.find((n) => n.id === 'vpc-1')
    const sub = nodes.find((n) => n.id === 'sub-1')
    expect(vpc?.position.x).toBeLessThan(sub?.position.x as number)
    expect(sub?.position.x).toBeLessThan(web?.position.x as number)
    // Same-kind nodes stack vertically instead of overlapping.
    const sub2 = nodes.find((n) => n.id === 'sub-2')
    expect(sub?.position.y).not.toBe(sub2?.position.y)
  })

  it('draws an edge only where a resource names a real parent', () => {
    const { edges } = buildTopologyFromResources(fixtures)
    expect(edges.map((e) => `${e.source}->${e.target}`).sort()).toEqual([
      'sub-1->i-1',
      'sub-2->i-2',
      'vpc-1->igw-1',
      'vpc-1->sub-1',
      'vpc-1->sub-2',
    ])
  })

  it('draws no edges for dangling or absent parents — never invents links', () => {
    const { edges } = buildTopologyFromResources([
      { id: 'i-9', kind: 'instance', name: 'orphan', parent_id: 'no-such-subnet' },
      { id: 'vpc-9', kind: 'vpc', name: 'lonely' },
    ])
    expect(edges).toEqual([])
  })

  it('returns an empty graph for an empty resource list (today’s backend)', () => {
    expect(buildTopologyFromResources([])).toEqual({ nodes: [], edges: [] })
    expect(buildTopologyFromLab({ id: 'lab-1', name: 'L', status: 'READY' }).nodes).toEqual([])
  })

  it('maps unknown kinds to the unknown column instead of dropping them', () => {
    const { nodes } = buildTopologyFromResources([{ id: 'z-1', kind: 'unknown', name: 'mystery' }])
    expect(nodes).toHaveLength(1)
    expect(nodes[0].data.kind).toBe('unknown')
  })
})

describe('normaliseKind', () => {
  it('maps engine kinds (network/namespace/bridge/address) onto flow kinds', () => {
    expect(normaliseKind('network')).toBe('vpc')
    expect(normaliseKind('namespace')).toBe('subnet')
    expect(normaliseKind('bridge')).toBe('gateway')
    expect(normaliseKind('address')).toBe('instance')
    expect(normaliseKind('IGW')).toBe('gateway')
    expect(normaliseKind('warp-drive')).toBe('unknown')
  })

  it('buildTopologyFromTopologyNodes reuses the same layout', () => {
    const { nodes } = buildTopologyFromTopologyNodes([
      { id: 'n-1', kind: 'network', name: 'net' },
      { id: 'n-2', kind: 'address', name: 'addr' },
    ])
    expect(nodes.map((n) => n.data.kind)).toEqual(['vpc', 'instance'])
  })
})

describe('makeDraftNode', () => {
  it('creates a draft node that is clearly flagged as unsaved', () => {
    const node = makeDraftNode('instance', 'web-02')
    expect(node.data.draft).toBe(true)
    expect(node.data.label).toBe('web-02')
    expect(node.id.startsWith('draft-')).toBe(true)
  })
})
