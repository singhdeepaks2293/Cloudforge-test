import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bell, ChevronDown, Grid2x2, HelpCircle, LogOut, Menu, Search, User } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { logout, fetchHealth, fetchMe, getToken } from '../services/api.ts'
import { DEFAULT_REGION, getStoredRegion, LOCAL_REGIONS, recordRecentService, setStoredRegion } from '../services/catalog.ts'
import { groupServicesByCategory, searchRegistryServices } from '../services/registry.ts'
import type { RegistryService } from '../types/index.ts'
import { ServiceIcon } from './AwsNav.tsx'

/**
 * Top header bar, AWS-console style: dark navy (#232f3e), CloudForge
 * wordmark + services grid button on the left, a wide "Search services"
 * input in the centre (searching the backend service registry —
 * service names, descriptions, categories, API operations, resource
 * types and learning exercises), region selector, notifications,
 * help/docs and account menus on the right. Present on every page via
 * Layout. Services come from GET /api/v1/services (props), never from
 * a hardcoded list.
 */
export function AwsHeader({ onToggleNav, services = [] }: { onToggleNav: () => void; services?: RegistryService[] }) {
  const navigate = useNavigate()
  const authed = getToken() !== null
  const [query, setQuery] = useState('')
  const [showResults, setShowResults] = useState(false)
  const [showServices, setShowServices] = useState(false)
  const [showRegion, setShowRegion] = useState(false)
  const [showAlerts, setShowAlerts] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const [showUser, setShowUser] = useState(false)
  const [region, setRegion] = useState<string>(() => getStoredRegion())
  const searchRef = useRef<HTMLDivElement | null>(null)

  // Notifications are real control-plane health, not invented alerts.
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1, staleTime: 30_000 })
  const me = useQuery({ queryKey: ['me'], queryFn: fetchMe, retry: 0, enabled: authed })

  const results = searchRegistryServices(query, services)
  const groups = groupServicesByCategory(services)

  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) setShowResults(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

  const goTo = (to: string, serviceId?: string) => {
    if (serviceId) recordRecentService(serviceId)
    setQuery('')
    setShowResults(false)
    setShowServices(false)
    navigate(to)
  }

  const regionLabel = LOCAL_REGIONS.find((r) => r.id === region)?.label ?? DEFAULT_REGION
  const healthItems: { label: string; ok: boolean | null }[] = health.data
    ? [
        { label: `Control plane: ${health.data.ok ? 'healthy' : 'degraded'}`, ok: health.data.ok },
        { label: `Network engine: ${health.data.network_engine_reachable ? 'reachable' : 'unreachable'}`, ok: health.data.network_engine_reachable },
        { label: `Mode: ${health.data.lite === true ? 'lite (classroom)' : 'full'}`, ok: null },
      ]
    : health.isError
      ? [{ label: 'Control plane health could not be loaded.', ok: false }]
      : [{ label: 'Loading control-plane health…', ok: null }]

  return (
    <header className="sticky top-0 z-40 flex h-12 items-center gap-3 bg-aws-navy px-3 text-white">
      <button
        type="button"
        onClick={onToggleNav}
        aria-label="Toggle navigation"
        className="rounded-sm p-1.5 hover:bg-aws-navy-light"
      >
        <Menu className="h-5 w-5" aria-hidden="true" />
      </button>
      <Link to="/" className="flex shrink-0 items-baseline gap-2 text-white hover:no-underline">
        <span className="text-lg font-bold tracking-tight">CloudForge</span>
        <span className="hidden text-[11px] text-[#d5dbdb] lg:inline">Build Cloud Skills. Not Cloud Bills.</span>
      </Link>

      <div className="relative">
        <button
          type="button"
          onClick={() => setShowServices((v) => !v)}
          aria-label="All services"
          aria-expanded={showServices}
          className="flex items-center gap-1.5 rounded-sm px-2 py-1.5 text-sm hover:bg-aws-navy-light"
        >
          <Grid2x2 className="h-4 w-4" aria-hidden="true" />
          <span className="hidden md:inline">Services</span>
        </button>
        {showServices && (
          <div className="absolute top-11 left-0 max-h-[70vh] w-80 overflow-auto border border-aws-border bg-white text-aws-text shadow-lg">
            <p className="border-b border-aws-border px-4 py-2 text-xs font-semibold tracking-wide text-aws-muted uppercase">
              CloudForge services
            </p>
            {services.length === 0 ? (
              <p className="px-4 py-3 text-sm text-aws-muted">Service registry could not be loaded.</p>
            ) : query.trim() === '' ? (
              groups.map((g) => (
                <div key={g.category}>
                  <p className="bg-[#f2f3f3] px-4 py-1.5 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">
                    {g.category}
                  </p>
                  <ul>
                    {g.services.map((s) => (
                      <li key={s.id}>
                        <button
                          type="button"
                          onClick={() => goTo(s.route, s.id)}
                          className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                        >
                          <ServiceIcon icon={s.icon} />
                          <span className="font-medium">{s.name}</span>
                          <span className="truncate text-xs text-aws-muted">{s.description}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ))
            ) : (
              <ul>
                {results.map((s) => (
                  <li key={s.id}>
                    <button
                      type="button"
                      onClick={() => goTo(s.route, s.id)}
                      className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                    >
                      <ServiceIcon icon={s.icon} />
                      <span className="font-medium">{s.name}</span>
                      <span className="truncate text-xs text-aws-muted">{s.description}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>

      <div ref={searchRef} className="relative min-w-0 flex-1">
        <div className="flex items-center overflow-hidden rounded-sm bg-white">
          <Search className="ml-2 h-4 w-4 shrink-0 text-aws-muted" aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setShowResults(true)
            }}
            onFocus={() => setShowResults(true)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && results.length > 0) goTo(results[0].route, results[0].id)
              if (e.key === 'Escape') setShowResults(false)
            }}
            placeholder="Search services, resources, operations, docs, learning"
            aria-label="Search services"
            className="min-w-0 flex-1 px-2 py-1.5 text-sm text-aws-text focus:outline-none"
          />
        </div>
        {showResults && query.trim() !== '' && (
          <div className="absolute top-10 right-0 left-0 border border-aws-border bg-white text-aws-text shadow-lg">
            {results.length === 0 ? (
              <p className="px-4 py-3 text-sm text-aws-muted">
                No CloudForge service matches &ldquo;{query}&rdquo;.
              </p>
            ) : (
              <ul>
                {results.map((s) => (
                  <li key={s.id}>
                    <button
                      type="button"
                      onClick={() => goTo(s.route, s.id)}
                      className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                    >
                      <ServiceIcon icon={s.icon} />
                      <span className="font-medium">{s.name}</span>
                      <span className="truncate text-xs text-aws-muted">{s.description}</span>
                      <span className="ml-auto shrink-0 rounded-full bg-[#eaeded] px-2 py-0.5 text-[11px] text-aws-muted">
                        {s.category} · {s.fidelity}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>

      <div className="relative hidden sm:block">
        <button
          type="button"
          onClick={() => setShowRegion((v) => !v)}
          aria-expanded={showRegion}
          className="flex items-center gap-1 rounded-sm px-2 py-1.5 text-sm hover:bg-aws-navy-light"
        >
          {regionLabel}
          <ChevronDown className="h-4 w-4" aria-hidden="true" />
        </button>
        {showRegion && (
          <div className="absolute top-11 right-0 w-72 border border-aws-border bg-white text-aws-text shadow-lg">
            <p className="border-b border-aws-border px-4 py-2 text-xs font-semibold tracking-wide text-aws-muted uppercase">
              Local regions
            </p>
            {LOCAL_REGIONS.map((r) => (
              <button
                key={r.id}
                type="button"
                onClick={() => {
                  setRegion(r.id)
                  setStoredRegion(r.id)
                  setShowRegion(false)
                }}
                className={`block w-full px-4 py-2 text-left text-sm hover:bg-[#f2f3f3] ${r.id === region ? 'font-semibold text-aws-link' : ''}`}
              >
                {r.label}
              </button>
            ))}
            <p className="border-t border-aws-border px-4 py-2 text-xs text-aws-muted">
              CloudForge runs locally — these are local regions, not AWS regions. Region scoping is display-only in
              Phase 1: all resources live in cf-local-1.
            </p>
          </div>
        )}
      </div>

      <div className="relative hidden sm:block">
        <button
          type="button"
          onClick={() => setShowAlerts((v) => !v)}
          aria-label="Notifications"
          aria-expanded={showAlerts}
          title="Control-plane health"
          className="rounded-sm p-1.5 hover:bg-aws-navy-light"
        >
          <Bell className="h-5 w-5" aria-hidden="true" />
        </button>
        {showAlerts && (
          <div className="absolute top-11 right-0 w-72 border border-aws-border bg-white text-aws-text shadow-lg">
            <p className="border-b border-aws-border px-4 py-2 text-xs font-semibold tracking-wide text-aws-muted uppercase">
              Notifications — live control-plane health
            </p>
            <ul>
              {healthItems.map((item) => (
                <li key={item.label} className="flex items-center gap-2 px-4 py-2 text-sm">
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${item.ok === null ? 'bg-aws-muted' : item.ok ? 'bg-aws-success' : 'bg-aws-error'}`}
                  />
                  {item.label}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      <div className="relative hidden sm:block">
        <button
          type="button"
          onClick={() => setShowHelp((v) => !v)}
          aria-label="Help and documentation"
          aria-expanded={showHelp}
          className="rounded-sm p-1.5 hover:bg-aws-navy-light"
        >
          <HelpCircle className="h-5 w-5" aria-hidden="true" />
        </button>
        {showHelp && (
          <div className="absolute top-11 right-0 w-64 border border-aws-border bg-white text-aws-text shadow-lg">
            <p className="border-b border-aws-border px-4 py-2 text-xs font-semibold tracking-wide text-aws-muted uppercase">
              Help &amp; documentation
            </p>
            <Link to="/learn" onClick={() => setShowHelp(false)} className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline">
              Learning Center — tracks &amp; lessons
            </Link>
            <Link to="/courses" onClick={() => setShowHelp(false)} className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline">
              Courses
            </Link>
            <Link to="/services" onClick={() => setShowHelp(false)} className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline">
              All services
            </Link>
            <Link to="/explorer" onClick={() => setShowHelp(false)} className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline">
              Resource Explorer
            </Link>
            <p className="border-t border-aws-border px-4 py-2 text-xs text-aws-muted">
              CloudForge — local learning cloud. No AWS account required.
            </p>
          </div>
        )}
      </div>

      <div className="relative">
        <button
          type="button"
          onClick={() => setShowUser((v) => !v)}
          aria-expanded={showUser}
          aria-label="Account menu"
          className="flex items-center gap-1 rounded-sm px-2 py-1.5 text-sm hover:bg-aws-navy-light"
        >
          <User className="h-5 w-5" aria-hidden="true" />
          <ChevronDown className="hidden h-4 w-4 sm:block" aria-hidden="true" />
        </button>
        {showUser && (
          <div className="absolute top-11 right-0 w-64 border border-aws-border bg-white text-aws-text shadow-lg">
            {authed ? (
              <>
                {me.data && (
                  <div className="border-b border-aws-border px-4 py-2">
                    <p className="text-sm font-medium">{me.data.name ?? me.data.email}</p>
                    <p className="text-xs text-aws-muted">{me.data.email}</p>
                    <p className="text-xs text-aws-muted">Role: {me.data.role ?? 'learner'}</p>
                  </div>
                )}
                <Link
                  to="/account"
                  onClick={() => setShowUser(false)}
                  className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline"
                >
                  Account settings
                </Link>
                <button
                  type="button"
                  onClick={() => {
                    void logout().finally(() => {
                      setShowUser(false)
                      navigate('/login')
                    })
                  }}
                  className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                >
                  <LogOut className="h-4 w-4" aria-hidden="true" /> Sign out
                </button>
              </>
            ) : (
              <>
                <Link
                  to="/login"
                  onClick={() => setShowUser(false)}
                  className="block px-4 py-2 text-sm text-aws-text hover:bg-[#f2f3f3] hover:no-underline"
                >
                  Sign in
                </Link>
                <Link
                  to="/register"
                  onClick={() => setShowUser(false)}
                  className="block px-4 py-2 text-sm text-aws-text hover:bg-[#f2f3f3] hover:no-underline"
                >
                  Create account
                </Link>
              </>
            )}
            <p className="border-t border-aws-border px-4 py-2 text-xs text-aws-muted">
              CloudForge — local learning cloud. No AWS account required.
            </p>
          </div>
        )}
      </div>
    </header>
  )
}
