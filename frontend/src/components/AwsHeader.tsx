import { useEffect, useRef, useState } from 'react'
import { Bell, ChevronDown, Grid2x2, LogOut, Menu, Search, User } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { clearToken, getToken } from '../services/api.ts'
import { ALL_SERVICES, DEFAULT_REGION, LOCAL_REGIONS, recordRecentService, searchServices } from '../services/catalog.ts'
import { ServiceIcon } from './AwsNav.tsx'

/**
 * Top header bar, AWS-console style: dark navy (#232f3e), CloudForge
 * wordmark + services grid button on the left, a wide "Search services"
 * input in the centre, region selector, alerts bell and user menu on
 * the right. Present on every page via Layout.
 */
export function AwsHeader({ onToggleNav }: { onToggleNav: () => void }) {
  const navigate = useNavigate()
  const authed = getToken() !== null
  const [query, setQuery] = useState('')
  const [showResults, setShowResults] = useState(false)
  const [showServices, setShowServices] = useState(false)
  const [showRegion, setShowRegion] = useState(false)
  const [showUser, setShowUser] = useState(false)
  const [region, setRegion] = useState<string>(() => {
    try {
      return localStorage.getItem('cloudforge_region') ?? DEFAULT_REGION
    } catch {
      return DEFAULT_REGION
    }
  })
  const searchRef = useRef<HTMLDivElement | null>(null)

  const results = searchServices(query)

  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) setShowResults(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

  const goTo = (to: string, serviceName?: string) => {
    if (serviceName) recordRecentService(serviceName)
    setQuery('')
    setShowResults(false)
    setShowServices(false)
    navigate(to)
  }

  const regionLabel = LOCAL_REGIONS.find((r) => r.id === region)?.label ?? DEFAULT_REGION

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
            {query.trim() === '' ? (
              <ul>
                {ALL_SERVICES.map((s) => (
                  <li key={`${s.name}-${s.to}`}>
                    <button
                      type="button"
                      onClick={() => goTo(s.to, s.name)}
                      className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                    >
                      <ServiceIcon icon={s.icon} />
                      <span className="font-medium">{s.name}</span>
                      <span className="truncate text-xs text-aws-muted">{s.label}</span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <ul>
                {results.map((s) => (
                  <li key={`${s.name}-${s.to}`}>
                    <button
                      type="button"
                      onClick={() => goTo(s.to, s.name)}
                      className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
                    >
                      <ServiceIcon icon={s.icon} />
                      <span className="font-medium">{s.name}</span>
                      <span className="truncate text-xs text-aws-muted">{s.label}</span>
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
              if (e.key === 'Enter' && results.length > 0) goTo(results[0].to, results[0].name)
              if (e.key === 'Escape') setShowResults(false)
            }}
            placeholder="Search services"
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
                  <li key={`${s.name}-${s.to}`}>
                    <button
                      type="button"
                      onClick={() => goTo(s.to, s.name)}
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
          <div className="absolute top-11 right-0 w-64 border border-aws-border bg-white text-aws-text shadow-lg">
            <p className="border-b border-aws-border px-4 py-2 text-xs font-semibold tracking-wide text-aws-muted uppercase">
              Local regions
            </p>
            {LOCAL_REGIONS.map((r) => (
              <button
                key={r.id}
                type="button"
                onClick={() => {
                  setRegion(r.id)
                  try {
                    localStorage.setItem('cloudforge_region', r.id)
                  } catch {
                    // Region preference is best-effort.
                  }
                  setShowRegion(false)
                }}
                className={`block w-full px-4 py-2 text-left text-sm hover:bg-[#f2f3f3] ${r.id === region ? 'font-semibold text-aws-link' : ''}`}
              >
                {r.label}
              </button>
            ))}
            <p className="border-t border-aws-border px-4 py-2 text-xs text-aws-muted">
              CloudForge runs locally — these are local regions, not AWS regions.
            </p>
          </div>
        )}
      </div>

      <button
        type="button"
        aria-label="Alerts"
        title="No alerts — the control plane health is shown on Console Home."
        className="hidden rounded-sm p-1.5 hover:bg-aws-navy-light sm:block"
      >
        <Bell className="h-5 w-5" aria-hidden="true" />
      </button>

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
          <div className="absolute top-11 right-0 w-56 border border-aws-border bg-white text-aws-text shadow-lg">
            {authed ? (
              <button
                type="button"
                onClick={() => {
                  clearToken()
                  setShowUser(false)
                  navigate('/login')
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm hover:bg-[#f2f3f3]"
              >
                <LogOut className="h-4 w-4" aria-hidden="true" /> Sign out
              </button>
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
