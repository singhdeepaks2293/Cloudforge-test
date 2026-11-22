/**
 * Bulk destroy helpers (bulk-destroy addition to the Launch-lab fix,
 * 2026-11-22).
 *
 * There is deliberately NO bulk endpoint: bulk destroy calls the
 * existing POST /api/v1/labs/{id}/destroy one lab at a time, in
 * order, and records a per-lab result. A failure (404 already
 * destroyed, 409 invalid transition, 401) is recorded for that lab
 * and the run continues with the next one — one bad lab never
 * aborts the rest, and no failure is swallowed.
 *
 * The sequential runner and the selection helpers are pure
 * functions so they are unit-testable without React; the Labs page
 * wires them to destroyLab() and React Query invalidation.
 */

export interface BulkDestroyResult {
  id: string
  ok: boolean
  /** Backend error message when ok is false. */
  error?: string
}

/**
 * Destroy labs one by one, in the order given. Calls onProgress
 * before each attempt (done = labs already finished) and once more
 * at the end, so the UI can show "Destroying lab 2 of 3…".
 */
export async function destroyLabsSequentially(
  ids: string[],
  destroy: (id: string) => Promise<unknown>,
  onProgress?: (done: number, total: number, currentId: string) => void,
): Promise<BulkDestroyResult[]> {
  const results: BulkDestroyResult[] = []
  for (let i = 0; i < ids.length; i++) {
    onProgress?.(i, ids.length, ids[i])
    try {
      await destroy(ids[i])
      results.push({ id: ids[i], ok: true })
    } catch (err) {
      results.push({
        id: ids[i],
        ok: false,
        error: err instanceof Error ? err.message : 'Unknown error',
      })
    }
  }
  onProgress?.(ids.length, ids.length, '')
  return results
}

/** Toggle one id in a selection set (returns a new set). */
export function toggleSelected(selected: ReadonlySet<string>, id: string): Set<string> {
  const next = new Set(selected)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  return next
}

/** True when every id in `ids` is selected (and ids is non-empty). */
export function areAllSelected(selected: ReadonlySet<string>, ids: readonly string[]): boolean {
  return ids.length > 0 && ids.every((id) => selected.has(id))
}

/** Toggle select-all over `ids`: select all when not all selected, else clear those ids. */
export function toggleSelectAll(selected: ReadonlySet<string>, ids: readonly string[]): Set<string> {
  const next = new Set(selected)
  if (areAllSelected(selected, ids)) {
    for (const id of ids) next.delete(id)
  } else {
    for (const id of ids) next.add(id)
  }
  return next
}

/** One-line summary of a finished bulk run, e.g. "Destroyed 2 of 3 labs." */
export function bulkDestroySummary(results: BulkDestroyResult[]): string {
  const ok = results.filter((r) => r.ok).length
  const total = results.length
  if (total === 0) return 'No labs were destroyed.'
  if (ok === total) return `Destroyed ${ok} of ${total} ${total === 1 ? 'lab' : 'labs'}.`
  return `Destroyed ${ok} of ${total} ${total === 1 ? 'lab' : 'labs'} — ${total - ok} failed.`
}
