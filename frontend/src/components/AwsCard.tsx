import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

/** AWS-console-style white card: 1px border, no rounded excess, subtle shadow. */
export function AwsCard({
  title,
  actions,
  children,
  className = '',
}: {
  title?: string
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`border border-aws-border bg-white shadow-sm ${className}`}>
      {title !== undefined && (
        <div className="flex items-center justify-between gap-2 border-b border-aws-border px-4 py-3">
          <h2 className="text-base font-semibold">{title}</h2>
          {actions}
        </div>
      )}
      <div className="px-4 py-4">{children}</div>
    </section>
  )
}

/** Breadcrumb shown at the top of every console page. */
export function Breadcrumb({ items }: { items: { label: string; to?: string }[] }) {
  return (
    <nav aria-label="Breadcrumb" className="mb-2 text-xs text-aws-muted">
      {items.map((item, i) => (
        <span key={item.label}>
          {i > 0 && <span className="mx-1">/</span>}
          {item.to ? (
            <Link to={item.to} className="text-aws-link hover:underline">
              {item.label}
            </Link>
          ) : (
            <span className="text-aws-text">{item.label}</span>
          )}
        </span>
      ))}
    </nav>
  )
}

/** Page title block in AWS style: h1 + description line. */
export function PageHeader({
  breadcrumb,
  title,
  description,
  actions,
}: {
  breadcrumb: { label: string; to?: string }[]
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <header className="mb-4">
      <Breadcrumb items={breadcrumb} />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{title}</h1>
          {description && <p className="mt-1 text-sm text-aws-muted">{description}</p>}
        </div>
        {actions}
      </div>
    </header>
  )
}
