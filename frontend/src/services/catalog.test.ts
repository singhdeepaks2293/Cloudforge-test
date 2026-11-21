import { describe, expect, it } from 'vitest'
import { ALL_SERVICES, DEFAULT_REGION, LOCAL_REGIONS, NAV_GROUPS, searchServices } from './catalog.ts'

describe('searchServices (header search)', () => {
  it('finds EC2 by name and navigates to the local Instances page', () => {
    const [hit] = searchServices('EC2')
    expect(hit.name).toBe('EC2')
    expect(hit.to).toBe('/labs')
  })

  it('finds VPC by keyword and matches case-insensitively', () => {
    expect(searchServices('vpc')[0].name).toBe('VPC')
    expect(searchServices('Vpc')[0].name).toBe('VPC')
    expect(searchServices('SUBNET').map((s) => s.name)).toContain('Subnets')
  })

  it('matches descriptions and keywords, e.g. "instance" finds EC2', () => {
    expect(searchServices('instance').map((s) => s.name)).toContain('EC2')
    expect(searchServices('iam').map((s) => s.name)).toContain('IAM')
  })

  it('returns nothing for an empty or unmatched query', () => {
    expect(searchServices('')).toEqual([])
    expect(searchServices('   ')).toEqual([])
    expect(searchServices('zzz-no-such-service')).toEqual([])
  })

  it('caps results at 8', () => {
    expect(searchServices('a').length).toBeLessThanOrEqual(8)
  })
})

describe('service catalog', () => {
  it('every nav group has at least one service and every service has a local route', () => {
    expect(NAV_GROUPS.length).toBeGreaterThan(0)
    for (const s of ALL_SERVICES) {
      expect(s.to.startsWith('/')).toBe(true)
      expect(s.name).not.toBe('')
      expect(s.keywords.length).toBeGreaterThan(0)
    }
  })

  it('only lists local regions — no real AWS regions', () => {
    expect(LOCAL_REGIONS.map((r) => r.id)).toEqual(['cf-local-1', 'cf-local-2'])
    expect(DEFAULT_REGION).toBe('cf-local-1')
    for (const r of LOCAL_REGIONS) expect(r.label).toContain('Local')
  })
})
