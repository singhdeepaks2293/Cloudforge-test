import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  DEFAULT_REGION,
  getFavoriteServiceIds,
  getRecentServiceIds,
  getStoredRegion,
  LOCAL_REGIONS,
  recordRecentService,
  setStoredRegion,
  toggleFavoriteService,
} from './catalog.ts'

describe('local regions (Phase 1)', () => {
  it('lists three local regions — no real AWS regions', () => {
    expect(LOCAL_REGIONS.map((r) => r.id)).toEqual(['cf-local-1', 'cf-local-2', 'cf-local-3'])
    expect(DEFAULT_REGION).toBe('cf-local-1')
    for (const r of LOCAL_REGIONS) expect(r.label).toContain('Local')
  })

  it('persists the region preference in localStorage', () => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
    expect(getStoredRegion()).toBe('cf-local-1')
    setStoredRegion('cf-local-3')
    expect(getStoredRegion()).toBe('cf-local-3')
    vi.unstubAllGlobals()
  })
})

describe('recently visited + favorites (per-user localStorage)', () => {
  beforeEach(() => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    })
  })

  it('records recent services most-recent-first, max 5, deduped', () => {
    for (const id of ['ec2', 's3', 'vpc', 'iam', 'ec2', 'rds', 'lambda']) recordRecentService(id)
    expect(getRecentServiceIds()).toEqual(['lambda', 'rds', 'ec2', 'iam', 'vpc'])
  })

  it('toggles favorites', () => {
    expect(getFavoriteServiceIds()).toEqual([])
    expect(toggleFavoriteService('ec2')).toBe(true)
    expect(toggleFavoriteService('s3')).toBe(true)
    expect(getFavoriteServiceIds()).toEqual(['ec2', 's3'])
    expect(toggleFavoriteService('ec2')).toBe(false)
    expect(getFavoriteServiceIds()).toEqual(['s3'])
  })
})
