/**
 * Tests for the bulk-destroy helpers (useBulkDestroy.ts).
 *
 * The sequential runner is what the Labs page's "Destroy selected"
 * / "Destroy all labs" actions call: one POST /api/v1/labs/{id}/destroy
 * per lab, in order, with a per-lab result. These tests prove the
 * contract without React: order is preserved, one failure never
 * aborts the rest, and every failure carries its backend message.
 */
import { describe, expect, it, vi } from 'vitest'
import {
  areAllSelected,
  bulkDestroySummary,
  destroyLabsSequentially,
  toggleSelectAll,
  toggleSelected,
} from './useBulkDestroy.ts'

describe('destroyLabsSequentially', () => {
  it('destroys labs one by one, in order, and reports every success', async () => {
    const order: string[] = []
    const destroy = vi.fn(async (id: string) => {
      order.push(id)
      return { id }
    })
    const progress: [number, number, string][] = []
    const results = await destroyLabsSequentially(['lab-1', 'lab-2', 'lab-3'], destroy, (d, t, c) =>
      progress.push([d, t, c]),
    )
    expect(order).toEqual(['lab-1', 'lab-2', 'lab-3'])
    expect(results).toEqual([
      { id: 'lab-1', ok: true },
      { id: 'lab-2', ok: true },
      { id: 'lab-3', ok: true },
    ])
    expect(destroy).toHaveBeenCalledTimes(3)
    expect(progress[0]).toEqual([0, 3, 'lab-1'])
    expect(progress[progress.length - 1]).toEqual([3, 3, ''])
  })

  it('continues after a failure and records the backend message per lab', async () => {
    const destroy = vi.fn(async (id: string) => {
      if (id === 'lab-2') throw new Error('no lab with that id')
      if (id === 'lab-3') throw new Error('invalid lab state transition: DESTROYED -> DESTROYING')
      return { id }
    })
    const results = await destroyLabsSequentially(['lab-1', 'lab-2', 'lab-3'], destroy)
    expect(results).toEqual([
      { id: 'lab-1', ok: true },
      { id: 'lab-2', ok: false, error: 'no lab with that id' },
      { id: 'lab-3', ok: false, error: 'invalid lab state transition: DESTROYED -> DESTROYING' },
    ])
    expect(destroy).toHaveBeenCalledTimes(3)
    expect(bulkDestroySummary(results)).toBe('Destroyed 1 of 3 labs — 2 failed.')
  })

  it('summarises a clean run and an empty run', () => {
    expect(bulkDestroySummary([{ id: 'a', ok: true }])).toBe('Destroyed 1 of 1 lab.')
    expect(bulkDestroySummary([])).toBe('No labs were destroyed.')
  })
})

describe('selection helpers', () => {
  it('toggleSelected adds then removes an id without mutating the input', () => {
    const start = new Set(['lab-1'])
    const added = toggleSelected(start, 'lab-2')
    expect([...added].sort()).toEqual(['lab-1', 'lab-2'])
    expect(start.has('lab-2')).toBe(false)
    expect([...toggleSelected(added, 'lab-1')]).toEqual(['lab-2'])
  })

  it('areAllSelected / toggleSelectAll select and clear a whole page', () => {
    const ids = ['lab-1', 'lab-2']
    expect(areAllSelected(new Set(), ids)).toBe(false)
    expect(areAllSelected(new Set(), [])).toBe(false)
    const all = toggleSelectAll(new Set(['lab-1']), ids)
    expect(areAllSelected(all, ids)).toBe(true)
    expect([...toggleSelectAll(all, ids)]).toEqual([])
  })
})
