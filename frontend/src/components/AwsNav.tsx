import { Server, Star } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import { SERVICE_ICONS, getFavoriteServiceIds, getRecentServiceIds } from '../services/catalog.ts'
import { groupServicesByCategory } from '../services/registry.ts'
import type { RegistryService } from '../types/index.ts'

export function ServiceIcon({ icon, className = 'h-4 w-4' }: { icon: string; className?: string }) {
  const Icon = SERVICE_ICONS[icon] ?? Server
  return <Icon className={className} aria-hidden="true" />
}

/**
 * Collapsible left navigation, AWS-console style, rendered from the
 * backend service registry (GET /api/v1/services). §4 main-navigation
 * areas: Home, All Services, Recently Visited, Favorites, Resource
 * Explorer, the registry categories (Compute, Storage, Networking, …),
 * Learning Center, Terraform Learning, Account Settings, Administration.
 * The active item gets the orange left border and orange text.
 * Recently Visited and Favorites are per-user localStorage (see
 * services/catalog.ts); everything else is the live registry.
 */
export function AwsNav({
  collapsed,
  onNavigate,
  services = [],
  registryError = false,
}: {
  collapsed: boolean
  onNavigate?: () => void
  services?: RegistryService[]
  registryError?: boolean
}) {
  const groups = groupServicesByCategory(services)
  const byId = new Map(services.map((s) => [s.id, s]))
  const recent = getRecentServiceIds().map((id) => byId.get(id)).filter((s): s is RegistryService => s !== undefined)
  const favorites = getFavoriteServiceIds().map((id) => byId.get(id)).filter((s): s is RegistryService => s !== undefined)

  const linkCls = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-2 border-l-2 px-3 py-1.5 text-sm ${
      isActive
        ? 'border-aws-orange bg-aws-orange-light font-medium text-aws-orange-dark'
        : 'border-transparent text-aws-text hover:bg-[#f2f3f3]'
    }`

  if (collapsed) {
    const flat: { to: string; label: string; icon: string }[] = [
      { to: '/', label: 'Console Home', icon: 'home' },
      { to: '/services', label: 'All Services', icon: 'layers' },
      { to: '/explorer', label: 'Resource Explorer', icon: 'search' },
      ...groups.flatMap((g) => g.services.map((s) => ({ to: s.route, label: s.name, icon: s.icon }))),
      { to: '/learn', label: 'Learning Center', icon: 'book' },
      { to: '/account', label: 'Account Settings', icon: 'key' },
      { to: '/admin', label: 'Administration', icon: 'shield' },
    ]
    return (
      <nav aria-label="Services" className="flex flex-col items-center gap-1 py-3">
        {flat.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            title={item.label}
            onClick={onNavigate}
            className={({ isActive }) =>
              `flex h-9 w-9 items-center justify-center rounded-sm border-l-2 ${
                isActive
                  ? 'border-aws-orange bg-aws-orange-light text-aws-orange-dark'
                  : 'border-transparent text-aws-muted hover:bg-[#f2f3f3] hover:text-aws-text'
              }`
            }
          >
            <ServiceIcon icon={item.icon} />
          </NavLink>
        ))}
      </nav>
    )
  }

  return (
    <nav aria-label="Services" className="flex flex-col gap-4 px-2 py-3">
      <NavLink to="/" end onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="home" />
        Console Home
      </NavLink>
      <NavLink to="/services" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="layers" />
        All Services
      </NavLink>

      {recent.length > 0 && (
        <div>
          <p className="px-3 pb-1 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">Recently Visited</p>
          <ul className="space-y-0.5">
            {recent.map((s) => (
              <li key={s.id}>
                <NavLink to={s.route} onClick={onNavigate} className={linkCls}>
                  <ServiceIcon icon={s.icon} />
                  {s.name}
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      )}

      {favorites.length > 0 && (
        <div>
          <p className="px-3 pb-1 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">Favorites</p>
          <ul className="space-y-0.5">
            {favorites.map((s) => (
              <li key={s.id}>
                <NavLink to={s.route} onClick={onNavigate} className={linkCls}>
                  <Star className="h-4 w-4 text-aws-orange" aria-hidden="true" />
                  {s.name}
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      )}

      <NavLink to="/explorer" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="search" />
        Resource Explorer
      </NavLink>

      {registryError && services.length === 0 && (
        <p className="px-3 text-xs text-aws-error">Service registry could not be loaded — showing static navigation only.</p>
      )}
      {groups.map((g) => (
        <div key={g.category}>
          <p className="px-3 pb-1 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">{g.category}</p>
          <ul className="space-y-0.5">
            {g.services.map((s) => (
              <li key={s.id}>
                <NavLink to={s.route} onClick={onNavigate} className={linkCls}>
                  <ServiceIcon icon={s.icon} />
                  {s.name}
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      ))}

      <NavLink to="/learn" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="book" />
        Learning Center
      </NavLink>
      <NavLink to="/services/terraform-learning" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="layers" />
        Terraform Learning
      </NavLink>
      <NavLink to="/account" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="key" />
        Account Settings
      </NavLink>
      <NavLink to="/admin" onClick={onNavigate} className={linkCls}>
        <ServiceIcon icon="shield" />
        Administration
      </NavLink>
    </nav>
  )
}
