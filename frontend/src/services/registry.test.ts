import { describe, expect, it } from 'vitest'
import { groupServicesByCategory, parseServices, searchRegistryServices } from './registry.ts'
import type { RegistryService } from '../types/index.ts'

function svc(partial: Partial<RegistryService> & { id: string }): RegistryService {
  return {
    name: partial.id.toUpperCase(),
    category: 'Compute',
    description: '',
    icon: 'server',
    route: `/services/${partial.id}`,
    availability: 'available',
    fidelity: 'SIMULATED',
    operations: [],
    resource_types: [],
    docs_url: '/learn',
    learning_exercises: [],
    ...partial,
  }
}

const FIXTURE = [
  svc({ id: 'ec2', name: 'EC2', description: 'Lab sessions as instances', operations: [{ operation: 'RunInstances', fidelity: 'API_EMULATED', supported: true, http_route: '' }], resource_types: ['instance'], learning_exercises: ['ec2'] }),
  svc({ id: 'vpc', name: 'VPC', category: 'Networking', description: 'Virtual private clouds', resource_types: ['vpc', 'subnet'] }),
  svc({ id: 's3', name: 'S3', category: 'Storage', description: 'Object storage buckets', operations: [{ operation: 'CreateBucket', fidelity: 'API_EMULATED', supported: true, http_route: 'POST /api/v1/aws/s3/buckets' }] }),
  svc({ id: 'lambda', name: 'Lambda', description: 'Serverless functions', learning_exercises: ['serverless'] }),
]

describe('parseServices (GET /api/v1/services)', () => {
  it('parses the { services: [...] } envelope and defaults empty operations', () => {
    const parsed = parseServices({ services: FIXTURE, count: 4 })
    expect(parsed).toHaveLength(4)
    expect(parsed[0].id).toBe('ec2')
    expect(parsed[1].operations).toEqual([])
    expect(parsed[2].route).toBe('/services/s3')
  })

  it('rejects a service with no id and a malformed envelope', () => {
    expect(() => parseServices({ services: [{ name: 'NoId' }] })).toThrow()
    expect(() => parseServices({ nope: true })).toThrow()
  })
})

describe('searchRegistryServices', () => {
  it('finds EC2 by name and navigates to the real service page', () => {
    const [hit] = searchRegistryServices('EC2', FIXTURE)
    expect(hit.id).toBe('ec2')
    expect(hit.route).toBe('/services/ec2')
  })

  it('matches descriptions, categories, operations, resource types and learning exercises', () => {
    expect(searchRegistryServices('vpc', FIXTURE)[0].id).toBe('vpc')
    expect(searchRegistryServices('RunInstances', FIXTURE)[0].id).toBe('ec2')
    expect(searchRegistryServices('subnet', FIXTURE)[0].id).toBe('vpc')
    expect(searchRegistryServices('serverless', FIXTURE).map((s) => s.id)).toContain('lambda')
    expect(searchRegistryServices('bucket', FIXTURE)[0].id).toBe('s3')
  })

  it('returns nothing for an empty or unmatched query and caps at 8', () => {
    expect(searchRegistryServices('', FIXTURE)).toEqual([])
    expect(searchRegistryServices('zzz-no-such-service', FIXTURE)).toEqual([])
    expect(searchRegistryServices('a', FIXTURE).length).toBeLessThanOrEqual(8)
  })
})

describe('groupServicesByCategory', () => {
  it('groups by category in §4 nav-area order', () => {
    const groups = groupServicesByCategory(FIXTURE)
    expect(groups.map((g) => g.category)).toEqual(['Compute', 'Storage', 'Networking'])
    expect(groups[0].services.map((s) => s.id)).toEqual(['ec2', 'lambda'])
  })
})
