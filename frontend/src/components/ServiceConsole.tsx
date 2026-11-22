/**
 * Shared service-console layout (Phase 1): page header with breadcrumb,
 * title, description and a fidelity badge, a left sub-navigation (the
 * AWS console's per-service left nav), and the content area. Every
 * service console in Phases 1–6 is built on this shell so the layout,
 * tabs and tables behave the same way everywhere.
 */
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { PageHeader } from './AwsCard.tsx'
import { FidelityBadge } from './AwsBadge.tsx'
import { ServiceIcon } from './AwsNav.tsx'

export interface SubNavItem {
  id: string
  label: string
  /** When set, the item is disabled with this reason (no backend op). */
  disabledReason?: string
  /**
   * v3 — AWS-reference grouped left nav: items sharing a `group` render
   * under that group heading (e.g. VPC's "Virtual private cloud").
   * A heading renders the first time a group value appears.
   */
  group?: string
  /**
   * v3 — when set, the item navigates to another console instead of
   * switching this console's section (e.g. VPC nav linking to the
   * Transit Gateway console). Rendered as a Link, never as a button.
   */
  to?: string
}

export function ServiceConsole({
  breadcrumb,
  title,
  description,
  fidelity,
  fidelityFootnote,
  subNav,
  activeSub,
  onSubChange,
  actions,
  icon,
  children,
}: {
  breadcrumb: { label: string; to?: string }[]
  title: string
  description?: string
  fidelity: string
  fidelityFootnote?: string
  subNav: SubNavItem[]
  activeSub: string
  onSubChange: (id: string) => void
  actions?: ReactNode
  /** Registry icon key (see SERVICE_ICONS); renders the service tile. */
  icon?: string
  children: ReactNode
}) {
  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={breadcrumb}
        title={title}
        description={description}
        actions={actions}
        icon={
          icon ? (
            <span className="flex h-10 w-10 shrink-0 items-center justify-center bg-aws-navy text-aws-orange">
              <ServiceIcon icon={icon} className="h-5 w-5" />
            </span>
          ) : undefined
        }
      />
      <div className="flex items-center gap-3 border-y border-aws-border bg-white px-3 py-2">
        <FidelityBadge fidelity={fidelity} footnote={fidelityFootnote} />
        <span className="ml-auto text-xs text-aws-muted">CloudForge · local learning cloud — not AWS</span>
      </div>
      <div className="flex gap-4">
        <aside className="w-56 shrink-0">
          <nav aria-label={`${title} navigation`} className="sticky top-16 border border-aws-border bg-white">
            <p className="border-b border-aws-border px-3 py-2 text-[11px] font-semibold tracking-wide text-aws-muted uppercase">
              {title}
            </p>
            {subNav.map((item, index) => {
              const groupHeading =
                item.group !== undefined && (index === 0 || subNav[index - 1].group !== item.group) ? (
                  <p
                    key={`group-${item.group}`}
                    className="border-t border-aws-border bg-[#f7f8f8] px-3 py-1.5 text-[11px] font-semibold tracking-wide text-aws-muted uppercase"
                  >
                    {item.group}
                  </p>
                ) : null
              const cls = `block w-full border-l-2 px-3 py-2 text-left text-sm ${
                item.id === activeSub
                  ? 'border-aws-orange bg-aws-orange-light font-medium text-aws-orange-dark'
                  : 'border-transparent text-aws-text hover:bg-[#f2f3f3]'
              } ${item.disabledReason !== undefined ? 'cursor-not-allowed opacity-50' : ''}`
              const body = (
                <>
                  {item.label}
                  {item.disabledReason !== undefined && (
                    <span className="block text-[11px] font-normal text-aws-muted">{item.disabledReason}</span>
                  )}
                </>
              )
              const entry =
                item.to !== undefined ? (
                  <Link key={item.id} to={item.to} title={item.label} className={cls}>
                    {body}
                  </Link>
                ) : (
                  <button
                    key={item.id}
                    type="button"
                    disabled={item.disabledReason !== undefined}
                    title={item.disabledReason ?? item.label}
                    onClick={() => onSubChange(item.id)}
                    className={cls}
                  >
                    {body}
                  </button>
                )
              return (
                <span key={item.id} className="block">
                  {groupHeading}
                  {entry}
                </span>
              )
            })}
          </nav>
        </aside>
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  )
}

/** Tab strip used inside a console's content area (AWS detail tabs). */
export function ConsoleTabs({
  tabs,
  active,
  onChange,
}: {
  tabs: { id: string; label: string; disabledReason?: string }[]
  active: string
  onChange: (id: string) => void
}) {
  return (
    <div className="flex flex-wrap gap-1 border-b border-aws-border" role="tablist" aria-label="Detail tabs">
      {tabs.map((t) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          aria-selected={t.id === active}
          disabled={t.disabledReason !== undefined}
          title={t.disabledReason ?? t.label}
          onClick={() => onChange(t.id)}
          className={`border-b-2 px-4 py-2 text-sm font-medium ${
            t.id === active
              ? 'border-aws-orange text-aws-orange-dark'
              : 'border-transparent text-aws-muted hover:text-aws-text'
          } ${t.disabledReason !== undefined ? 'cursor-not-allowed opacity-50' : ''}`}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}
