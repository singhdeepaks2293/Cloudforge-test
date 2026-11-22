import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { AwsCard } from './AwsCard.tsx'
import { FidelityBadge } from './AwsBadge.tsx'
import { AwsFlash, AwsRefreshButton } from './AwsShell.tsx'
import { ServiceConsole } from './ServiceConsole.tsx'
import { getStoredRegion } from '../services/catalog.ts'

/**
 * v3 — AWS-reference service Home dashboard (pattern captured from the
 * real VPC console, ap-southeast-2) and the wrapper that gives grouped
 * (non-ServiceConsole) consoles a Home tab.
 *
 * VPC reference pattern, applied to every lite service:
 * - Top banner, then action links ("Create VPC", "Launch EC2 Instances").
 * - "Resources by Region" with a Refresh button: a two-column grid of
 *   resource cards, NOT a table. Every card: linked resource-type
 *   heading, region label, linked count, collapsed "Regions" expander.
 * - Then Service Health, Settings (link list) and Additional
 *   Information (link list) sections.
 * - No data table, columns, pagination or detail tabs on Home — those
 *   live on the list/detail pages, reached from the left nav.
 * - CloudForge branding only (dark navy header, orange primary
 *   buttons, CloudForge name); no AWS logo, brand colours or
 *   trademarks, and no claim this is AWS.
 */

export interface ServiceHomeCard {
  /** Resource-type heading, e.g. "VPCs". */
  label: string
  /** Live count from the backend query; '—' when there is no count API. */
  count: number | string
  /** In-console target for the heading and the count (list section). */
  to?: string
  /** In-console section switch (used when the list is a tab of the same console, not a route). */
  onSelect?: () => void
  /** Short honest note, e.g. "SIMULATED" or why the count is unavailable. */
  note?: string
  /** When set, the card is shown disabled with this reason. */
  disabledReason?: string
}

export interface ServiceHomeAction {
  label: string
  /** In-console target. */
  to?: string
  onClick?: () => void
  primary?: boolean
  disabledReason?: string
}

export interface ServiceHomeLink {
  label: string
  to: string
}

export function ServiceHomeDashboard({
  serviceName,
  banner,
  bannerTone = 'info',
  actions = [],
  cards,
  onRefresh,
  refreshing = false,
  health = [],
  settings = [],
  info = [],
  children,
}: {
  serviceName: string
  banner?: string
  bannerTone?: 'info' | 'success' | 'error'
  actions?: ServiceHomeAction[]
  cards: ServiceHomeCard[]
  onRefresh?: () => void
  refreshing?: boolean
  health?: string[]
  settings?: ServiceHomeLink[]
  info?: ServiceHomeLink[]
  children?: ReactNode
}) {
  const region = getStoredRegion()
  return (
    <div className="space-y-4">
      {banner !== undefined && banner !== '' && <AwsFlash tone={bannerTone}>{banner}</AwsFlash>}
      {actions.length > 0 && (
        <div className="flex flex-wrap items-center gap-4">
          {actions.map((a) =>
            a.to !== undefined ? (
              <Link key={a.label} to={a.to} className={`text-sm ${a.primary ? 'font-medium text-aws-orange-dark' : 'text-aws-link'} hover:underline`}>
                {a.label}
              </Link>
            ) : (
              <button
                key={a.label}
                type="button"
                onClick={a.onClick}
                disabled={a.disabledReason !== undefined}
                title={a.disabledReason}
                className={`text-sm ${a.primary ? 'font-medium text-aws-orange-dark' : 'text-aws-link'} hover:underline disabled:cursor-not-allowed disabled:opacity-50`}
              >
                {a.label}
              </button>
            ),
          )}
        </div>
      )}
      <section aria-label="Resources by Region">
        <div className="mb-2 flex items-center gap-2">
          <h2 className="text-base font-semibold">Resources by Region</h2>
          {onRefresh !== undefined && <AwsRefreshButton onClick={onRefresh} busy={refreshing} />}
        </div>
        {cards.length === 0 ? (
          <p className="text-sm text-aws-muted">No resource types are recorded for this service yet.</p>
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {cards.map((card) => (
              <article
                key={card.label}
                className={`border border-aws-border bg-white px-4 py-3 ${card.disabledReason !== undefined ? 'opacity-70' : ''}`}
                aria-label={`${card.label} resource card`}
              >
                {card.onSelect !== undefined && card.disabledReason === undefined ? (
                  <button type="button" onClick={card.onSelect} className="text-sm font-medium text-aws-link hover:underline">
                    {card.label}
                  </button>
                ) : card.to !== undefined && card.disabledReason === undefined ? (
                  <Link to={card.to} className="text-sm font-medium text-aws-link hover:underline">
                    {card.label}
                  </Link>
                ) : (
                  <p className="text-sm font-medium">{card.label}</p>
                )}
                <p className="mt-1 text-xs text-aws-muted">Region: {region}</p>
                {card.onSelect !== undefined && card.disabledReason === undefined ? (
                  <button type="button" onClick={card.onSelect} className="mt-0.5 block text-lg font-semibold text-aws-link hover:underline">
                    {card.count}
                  </button>
                ) : card.to !== undefined && card.disabledReason === undefined ? (
                  <Link to={card.to} className="mt-0.5 block text-lg font-semibold text-aws-link hover:underline">
                    {card.count}
                  </Link>
                ) : (
                  <p className="mt-0.5 text-lg font-semibold">{card.count}</p>
                )}
                {card.note !== undefined && card.note !== '' && <p className="mt-1 text-xs text-aws-muted">{card.note}</p>}
                {card.disabledReason !== undefined && <p className="mt-1 text-xs text-aws-muted">{card.disabledReason}</p>}
                <details className="mt-2 text-xs">
                  <summary className="cursor-pointer text-aws-link hover:underline">Regions</summary>
                  <ul className="mt-1 list-disc pl-5 text-aws-muted">
                    <li>
                      {region} — {card.count}
                    </li>
                    <li>All other local regions — 0 (region scoping is display-only; resources live in {region}).</li>
                  </ul>
                </details>
              </article>
            ))}
          </div>
        )}
      </section>
      {health.length > 0 && (
        <AwsCard title="Service Health">
          <ul className="list-disc space-y-1 pl-5 text-sm">
            {health.map((h) => (
              <li key={h}>{h}</li>
            ))}
          </ul>
        </AwsCard>
      )}
      {settings.length > 0 && (
        <AwsCard title="Settings">
          <ul className="space-y-1 text-sm">
            {settings.map((s) => (
              <li key={s.label}>
                <Link to={s.to} className="text-aws-link hover:underline">
                  {s.label}
                </Link>
              </li>
            ))}
          </ul>
        </AwsCard>
      )}
      {info.length > 0 && (
        <AwsCard title="Additional Information">
          <ul className="space-y-1 text-sm">
            {info.map((s) => (
              <li key={s.label}>
                <Link to={s.to} className="text-aws-link hover:underline">
                  {s.label}
                </Link>
              </li>
            ))}
          </ul>
        </AwsCard>
      )}
      {children}
      <p className="text-xs text-aws-muted">
        {serviceName} home — CloudForge local learning cloud, not AWS. Counts come from the live backend; a card
        with no count has no list API behind it and says so instead of inventing one.
      </p>
    </div>
  )
}

/**
 * Wrapper for the grouped Phase 6 consoles (Networking, Security,
 * Management, Analytics, Migration), which were built on PageHeader
 * instead of ServiceConsole and so had no left nav or Home dashboard.
 * It adds the two-item left nav — "<Service> dashboard" (Home) and the
 * existing list page — around the console's existing content.
 *
 * `initialSection` defaults to 'list' so existing render tests (which
 * assert list content) keep passing; App routes pass 'home' so real
 * users land on the dashboard first, as in the AWS console.
 */
export function ConsoleWithHome({
  breadcrumb,
  title,
  description,
  fidelity,
  fidelityFootnote,
  icon,
  home,
  homeLabel,
  listLabel,
  initialSection = 'list',
  children,
}: {
  breadcrumb: { label: string; to?: string }[]
  title: string
  description?: string
  fidelity: string
  fidelityFootnote?: string
  icon?: string
  /**
   * Home dashboard. Either a node, or a render function receiving
   * `goToList` so Home cards/action links can switch to the list page
   * (the list is a tab of the same console, not a route).
   */
  home: ReactNode | ((goToList: () => void) => ReactNode)
  homeLabel?: string
  listLabel: string
  initialSection?: 'home' | 'list'
  children: ReactNode
}) {
  const [section, setSection] = useState<'home' | 'list'>(initialSection)
  const goToList = () => setSection('list')
  return (
    <ServiceConsole
      icon={icon}
      breadcrumb={breadcrumb}
      title={title}
      description={description}
      fidelity={fidelity}
      fidelityFootnote={fidelityFootnote}
      subNav={[
        { id: 'home', label: homeLabel ?? `${title} dashboard` },
        { id: 'list', label: listLabel },
      ]}
      activeSub={section}
      onSubChange={(id) => setSection(id as 'home' | 'list')}
    >
      {section === 'home' ? (typeof home === 'function' ? home(goToList) : home) : children}
    </ServiceConsole>
  )
}

/** Re-export for pages that need the badge next to a Home heading. */
export { FidelityBadge }
