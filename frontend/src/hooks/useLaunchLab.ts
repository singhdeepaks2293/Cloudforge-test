import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ApiError, createLab, fetchHealth } from '../services/api.ts'
import type { Lab } from '../types/index.ts'

/**
 * Shared "Launch lab" helpers (Launch-lab fix, 2026-11-22).
 *
 * These live in a hook file — not in LabLauncher.tsx — so the
 * component file only exports components (oxlint
 * react/only-export-components). See LabLauncher.tsx for the
 * background: before the fix no page ever called POST /api/v1/labs.
 */

/** Human-readable form of a createLab failure. */
export function launchErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 429) {
      return `Lab quota exceeded: ${err.message} In lite mode you can run 1 lab at a time, 30 minutes maximum.`
    }
    if (err.status === 401) {
      return 'You are not signed in (or your session expired). Please log in and try again.'
    }
    if (err.status === 400) {
      return `Could not launch this lab: ${err.message}`
    }
    return `Could not launch this lab: ${err.message}`
  }
  return 'Could not launch this lab. Please try again.'
}

/** Shared createLab mutation + error state for the launch buttons. */
export function useCreateLab(onLaunched: (lab: Lab) => void) {
  const [error, setError] = useState<string | null>(null)
  const mutation = useMutation({
    mutationFn: (definitionId: string) => createLab(definitionId),
    onSuccess: (lab) => {
      setError(null)
      onLaunched(lab)
    },
    onError: (err: unknown) => setError(launchErrorMessage(err)),
  })
  return {
    launch: (definitionId: string) => mutation.mutate(definitionId),
    launching: mutation.isPending,
    error,
    clearError: () => setError(null),
  }
}

/** True when the backend reports lite mode (health query shared with the picker). */
export function useLiteMode(): boolean {
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  return health.data?.lite === true
}
