/**
 * Console helpers that are NOT the service registry: icon resolution,
 * local regions, and Recently Visited / Favorites persistence.
 *
 * The service list itself lives in the backend (GET /api/v1/services,
 * see services/registry.ts) — nothing here hardcodes which services
 * exist. These helpers only key history by service id.
 */
import {
  Activity,
  BookOpen,
  Database,
  FlaskConical,
  HardDrive,
  Home,
  KeyRound,
  Layers,
  Lock,
  Network,
  Route,
  Scale,
  Search,
  Server,
  Shield,
  Table2,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { getToken } from './api.ts'

/** Resolve a registry icon key to a lucide component. */
export const SERVICE_ICONS: Record<string, LucideIcon> = {
  server: Server,
  zap: Zap,
  network: Network,
  layers: Layers,
  route: Route,
  shield: Shield,
  scale: Scale,
  database: Database,
  'hard-drive': HardDrive,
  table: Table2,
  key: KeyRound,
  lock: Lock,
  activity: Activity,
  book: BookOpen,
  flask: FlaskConical,
  home: Home,
  search: Search,
}

/** Regions available in this local deployment — no real AWS regions. */
export const LOCAL_REGIONS = [
  { id: 'cf-local-1', label: 'cf-local-1 (Local Region 1)' },
  { id: 'cf-local-2', label: 'cf-local-2 (Local Region 2)' },
  { id: 'cf-local-3', label: 'cf-local-3 (Local Region 3)' },
] as const

export const DEFAULT_REGION = LOCAL_REGIONS[0].id

const REGION_KEY = 'cloudforge_region'

export function getStoredRegion(): string {
  try {
    return localStorage.getItem(REGION_KEY) ?? DEFAULT_REGION
  } catch {
    return DEFAULT_REGION
  }
}

export function setStoredRegion(region: string): void {
  try {
    localStorage.setItem(REGION_KEY, region)
  } catch {
    // Region preference is best-effort.
  }
}

/** User id from the JWT `sub` claim (payload only, never the secret). */
export function getCurrentUserId(): string {
  try {
    const token = getToken()
    if (!token) return 'anonymous'
    const payload = token.split('.')[1]
    if (!payload) return 'anonymous'
    const decoded = JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/'))) as { sub?: string }
    return typeof decoded.sub === 'string' && decoded.sub !== '' ? decoded.sub : 'anonymous'
  } catch {
    return 'anonymous'
  }
}

function perUserKey(base: string): string {
  return `${base}:${getCurrentUserId()}`
}

function readIdList(key: string): string[] {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []
  } catch {
    return []
  }
}

function writeIdList(key: string, ids: string[]): void {
  try {
    localStorage.setItem(key, JSON.stringify(ids))
  } catch {
    // History is best-effort; never break navigation.
  }
}

/** Recently visited service ids, most recent first (max 5, per user). */
const RECENT_KEY = 'cloudforge_recent_services'

export function getRecentServiceIds(): string[] {
  return readIdList(perUserKey(RECENT_KEY))
}

export function recordRecentService(id: string): void {
  const current = getRecentServiceIds().filter((n) => n !== id)
  writeIdList(perUserKey(RECENT_KEY), [id, ...current].slice(0, 5))
}

/** Favourite service ids (per user). */
const FAVORITES_KEY = 'cloudforge_favorite_services'

export function getFavoriteServiceIds(): string[] {
  return readIdList(perUserKey(FAVORITES_KEY))
}

export function isFavoriteService(id: string): boolean {
  return getFavoriteServiceIds().includes(id)
}

export function toggleFavoriteService(id: string): boolean {
  const current = getFavoriteServiceIds()
  const next = current.includes(id) ? current.filter((n) => n !== id) : [...current, id]
  writeIdList(perUserKey(FAVORITES_KEY), next)
  return next.includes(id)
}
