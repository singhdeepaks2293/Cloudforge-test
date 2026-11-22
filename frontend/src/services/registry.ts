/**
 * Backend-driven service registry client (Phase 1).
 *
 * The console nav, service search and All Services page render from
 * GET /api/v1/services — never from a hardcoded frontend list. The
 * backend merges console metadata with the live providers registry,
 * so operations and fidelity always reflect what the backend actually
 * serves. This module only parses and searches that response.
 */
import { useQuery } from '@tanstack/react-query'
import { ApiError, request } from './api.ts'
import type { RegistryOperation, RegistryService } from '../types/index.ts'

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

export function parseRegistryService(data: unknown): RegistryService {
  if (!isRecord(data)) throw new ApiError(0, 'Invalid response: service must be an object')
  const id = typeof data.id === 'string' ? data.id : ''
  if (id === '') throw new ApiError(0, 'Invalid response: service has no id')
  const opsRaw = Array.isArray(data.operations) ? data.operations : []
  const operations: RegistryOperation[] = opsRaw.filter(isRecord).map((o) => ({
    operation: typeof o.operation === 'string' ? o.operation : '',
    fidelity: typeof o.fidelity === 'string' ? o.fidelity : 'UNSUPPORTED',
    supported: o.supported === true,
    http_route: typeof o.http_route === 'string' ? o.http_route : undefined,
    limitations: Array.isArray(o.limitations)
      ? (o.limitations as unknown[]).filter((l): l is string => typeof l === 'string')
      : undefined,
  }))
  const availability = data.availability as RegistryService['availability']
  return {
    id,
    name: typeof data.name === 'string' ? data.name : id,
    category: typeof data.category === 'string' ? data.category : 'Additional',
    description: typeof data.description === 'string' ? data.description : '',
    icon: typeof data.icon === 'string' ? data.icon : 'server',
    route: typeof data.route === 'string' ? data.route : `/services/${id}`,
    availability:
      availability === 'available' || availability === 'partial' || availability === 'registry-only' || availability === 'unavailable'
        ? availability
        : 'unavailable',
    fidelity: typeof data.fidelity === 'string' ? data.fidelity : 'UNSUPPORTED',
    operations,
    resource_types: Array.isArray(data.resource_types)
      ? (data.resource_types as unknown[]).filter((t): t is string => typeof t === 'string')
      : [],
    docs_url: typeof data.docs_url === 'string' ? data.docs_url : '/learn',
    learning_exercises: Array.isArray(data.learning_exercises)
      ? (data.learning_exercises as unknown[]).filter((t): t is string => typeof t === 'string')
      : [],
    mirrors: typeof data.mirrors === 'string' ? data.mirrors : undefined,
    differs: typeof data.differs === 'string' ? data.differs : undefined,
  }
}

export function parseServices(data: unknown): RegistryService[] {
  const items: unknown[] = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.services)
      ? (data.services as unknown[])
      : (() => {
          throw new ApiError(0, 'Invalid response: services must be an array or { services: [...] }')
        })()
  return items.map(parseRegistryService)
}

/** GET /api/v1/services — the registry. Public; no auth required. */
export async function fetchServices(): Promise<RegistryService[]> {
  const data = await request<unknown>('/api/v1/services', { auth: false })
  return parseServices(data)
}

/** GET /api/v1/services/{id} — one service, or ApiError 404. */
export async function fetchService(id: string): Promise<RegistryService> {
  const data = await request<unknown>(`/api/v1/services/${encodeURIComponent(id)}`, { auth: false })
  return parseRegistryService(data)
}

/** React Query hook for the registry (shared by Layout/header/nav). */
export function useServices() {
  return useQuery({ queryKey: ['services'], queryFn: fetchServices, retry: 1, staleTime: 60_000 })
}

export function useService(id: string) {
  return useQuery({ queryKey: ['service', id], queryFn: () => fetchService(id), retry: 1, staleTime: 60_000 })
}

/**
 * Search over the registry: service names, descriptions, categories,
 * API operations, resource types and learning exercises. Results show
 * the category and navigate to the service's real route.
 */
export function searchRegistryServices(query: string, services: RegistryService[]): RegistryService[] {
  const q = query.trim().toLowerCase()
  if (q === '') return []
  const seen = new Set<string>()
  return services
    .filter((s) => {
      const hay = [
        s.name,
        s.description,
        s.category,
        ...s.operations.map((o) => o.operation),
        ...s.resource_types,
        ...s.learning_exercises,
      ]
        .join(' ')
        .toLowerCase()
      if (!hay.includes(q)) return false
      if (seen.has(s.id)) return false
      seen.add(s.id)
      return true
    })
    .slice(0, 8)
}

/** §4 main-navigation area order for the left nav. */
export const NAV_AREA_ORDER = [
  'Compute',
  'Storage',
  'Networking',
  'Databases',
  'Security and Identity',
  'Application Integration',
  'Containers',
  'Serverless',
  'Developer Tools',
  'Management and Governance',
  'Monitoring',
  'Migration and Transfer',
  'Analytics',
  'Machine Learning and AI',
  'Terraform Learning',
  'Learning Center',
  'Additional',
] as const

export function groupServicesByCategory(services: RegistryService[]): { category: string; services: RegistryService[] }[] {
  const byCat = new Map<string, RegistryService[]>()
  for (const s of services) {
    const list = byCat.get(s.category) ?? []
    list.push(s)
    byCat.set(s.category, list)
  }
  const order = new Map<string, number>(NAV_AREA_ORDER.map((c, i) => [c, i]))
  return [...byCat.entries()]
    .map(([category, list]) => ({ category, services: [...list].sort((a, b) => a.name.localeCompare(b.name)) }))
    .sort((a, b) => (order.get(a.category) ?? 99) - (order.get(b.category) ?? 99))
}
