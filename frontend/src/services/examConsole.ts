/**
 * Shared types + helpers for the dedicated exam-service consoles
 * (audit gap #3). No JSX here, so this module can export hooks and
 * helpers freely; the components live in ExamServiceConsole.tsx.
 */
import type { ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { AwsColumn } from '../components/AwsTable.tsx'
import type { ServiceHomeCard } from '../components/ServiceHome.tsx'
import { request } from './api.ts'

export type Rec = Record<string, unknown> & { id: string }

/** Extract the named list key from an API-shaped response. */
export const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

export const str = (r: Rec, k: string, fallback = '—'): string => (typeof r[k] === 'string' && r[k] !== '' ? (r[k] as string) : fallback)
export const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)
export const arrOf = (r: Rec, k: string): unknown[] => (Array.isArray(r[k]) ? (r[k] as unknown[]) : [])
export const fmtTime = (v: unknown): string => (typeof v === 'string' && v !== '' ? new Date(v).toLocaleString() : '—')
export const fmtUsd = (v: unknown): string => (typeof v === 'number' ? `$${v.toFixed(2)}` : '—')

export const inputCls =
  'rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none'

export interface ExamCtx {
  refresh: () => void
  flash: (message: string, tone?: 'success' | 'info' | 'error') => void
  /** POST /api/v1/aws/<path>; flashes the backend note on success. */
  runAction: (path: string, body?: Record<string, unknown>) => void
  actionBusy: boolean
  selectedId: string
  setSelectedId: (id: string) => void
  setSection: (id: string) => void
}

export interface ExamDetailCtx extends ExamCtx {
  sel: Rec
}

export interface ExamCreateField {
  key: string
  label: string
  /** 'list' is a textarea of comma-separated values, sent as string[]. */
  type: 'text' | 'number' | 'select' | 'list'
  options?: string[]
  required?: boolean
  placeholder?: string
  defaultValue?: string | number
  hint?: string
}

export interface ExamDetailTab {
  id: string
  label: string
  disabledReason?: string
  render: (ctx: ExamDetailCtx) => ReactNode
}

export interface ExamExtraTab {
  id: string
  label: string
  group?: string
  /** No backend for this tab: render the honest UNSUPPORTED state. */
  unsupported?: string
  render?: (ctx: ExamCtx) => ReactNode
}

export interface ExamServiceConfig {
  id: string
  name: string
  category: string
  icon: string
  description: string
  fidelity: string
  fidelityFootnote: string
  banner: string
  bannerTone?: 'info' | 'success' | 'error'
  /** Section shown first; defaults to the list section. */
  defaultSection?: string
  listSection: string
  listLabel: string
  /** Path under /api/v1/aws/, e.g. 'aurora/clusters'. */
  listPath: string
  listKey: string
  kindLabel: string
  createLabel: string
  /** When set, there is no create API — the button is disabled with this reason. */
  createDisabledReason?: string
  columns: AwsColumn<Rec>[]
  /** Honesty note under the list table (e.g. columns are CloudForge's own). */
  columnsNote?: string
  createFields: ExamCreateField[]
  createNote?: string
  /** Extra detail tabs beyond the auto-rendered Details tab. */
  detailTabs: ExamDetailTab[]
  /** Extra top-level tabs beyond Home / list / Operations / Learning. */
  extraTabs?: ExamExtraTab[]
  /** Buttons shown in the list toolbar (e.g. Refresh recommendations). */
  listActions?: (ctx: ExamCtx) => ReactNode
  homeCards: (rows: Rec[], goTo: (section: string) => void) => ServiceHomeCard[]
  homeExtra?: ReactNode
  /** Health lines on Home; defaults to a count + fidelity line. */
  health?: (rows: Rec[]) => string[]
}

export function useExamList(listPath: string, listKey: string) {
  const queryClient = useQueryClient()
  const q = useQuery({ queryKey: ['exam-list', listPath], queryFn: () => request<Rec>(`/api/v1/aws/${listPath}`), retry: 1 })
  const rows = listOf(q.data, listKey)
  return {
    rows,
    data: q.data,
    isPending: q.isPending,
    isError: q.isError,
    errorMessage: q.error?.message ?? 'could not be loaded',
    isFetching: q.isFetching,
    refresh: () => void queryClient.invalidateQueries({ queryKey: ['exam-list', listPath] }),
  }
}

