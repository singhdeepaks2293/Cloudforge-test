import { Server } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import { NAV_GROUPS, SERVICE_ICONS } from '../services/catalog.ts'

export function ServiceIcon({ icon, className = 'h-4 w-4' }: { icon: string; className?: string }) {
  const Icon = SERVICE_ICONS[icon] ?? Server
  return <Icon className={className} aria-hidden="true" />
}

/**
 * Collapsible left navigation, AWS-console style: service groups with
 * an icon per service. The active item gets the orange left border
 * and orange text.
 */
export function AwsNav({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  if (collapsed) {
    return (
      <nav aria-label="Services" className="flex flex-col items-center gap-1 py-3">
        {NAV_GROUPS.flatMap((g) => g.services).map((s) => (
          <NavLink
            key={`${s.group}-${s.name}`}
            to={s.to}
            title={`${s.name} — ${s.label}`}
            onClick={onNavigate}
            className={({ isActive }) =>
              `flex h-9 w-9 items-center justify-center rounded-sm border-l-2 ${
                isActive
                  ? 'border-aws-orange bg-aws-orange-light text-aws-orange-dark'
                  : 'border-transparent text-aws-muted hover:bg-[#f2f3f3] hover:text-aws-text'
              }`
            }
          >
            <ServiceIcon icon={s.icon} />
          </NavLink>
        ))}
      </nav>
    )
  }
  return (
    <nav aria-label="Services" className="flex flex-col gap-4 px-2 py-3">
      <NavLink
        to="/"
        end
        onClick={onNavigate}
        className={({ isActive }) =>
          `flex items-center gap-2 border-l-2 px-3 py-2 text-sm font-medium ${
            isActive
              ? 'border-aws-orange bg-aws-orange-light text-aws-orange-dark'
              : 'border-transparent text-aws-text hover:bg-[#f2f3f3]'
          }`
        }
      >
        <ServiceIcon icon="home" />
        Console Home
      </NavLink>
      {NAV_GROUPS.map((g) => (
        <div key={g.group}>
          <p className="px-3 pb-1 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">{g.group}</p>
          <ul className="space-y-0.5">
            {g.services.map((s) => (
              <li key={`${s.group}-${s.name}`}>
                <NavLink
                  to={s.to}
                  end={s.to === '/'}
                  onClick={onNavigate}
                  className={({ isActive }) =>
                    `flex items-center gap-2 border-l-2 px-3 py-1.5 text-sm ${
                      isActive
                        ? 'border-aws-orange bg-aws-orange-light font-medium text-aws-orange-dark'
                        : 'border-transparent text-aws-text hover:bg-[#f2f3f3]'
                    }`
                  }
                >
                  <ServiceIcon icon={s.icon} />
                  {s.name}
                  <span className="truncate text-xs text-aws-muted">{s.label.replace(/^.*?\(|\)$/g, '')}</span>
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  )
}
