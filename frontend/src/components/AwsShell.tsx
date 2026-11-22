import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clipboard,
  Info,
  Loader2,
  RefreshCw,
  Settings2,
} from 'lucide-react'
import { AwsButton } from './AwsButton.tsx'

/**
 * AWS-console-style shell primitives used across the top-five consoles:
 * page toolbars with an Actions dropdown, flash messages, multi-step
 * create wizards, copy-to-clipboard, empty states and summary tiles.
 * Purely presentational; pages keep all data and mutations.
 */

export function AwsToolbar({ children, right }: { children?: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <div className="flex flex-wrap items-center gap-2">{children}</div>
      <div className="ml-auto flex flex-wrap items-center gap-2">{right}</div>
    </div>
  )
}

export interface AwsActionItem {
  label: string
  onClick?: () => void
  disabledReason?: string
  danger?: boolean
}

/** AWS "Actions" dropdown. Disabled entries stay visible with a reason. */
export function AwsActionsMenu({ label = 'Actions', items }: { label?: string; items: AwsActionItem[] }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])
  return (
    <div ref={ref} className="relative">
      <AwsButton aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {label}
        <ChevronDown className="h-4 w-4" aria-hidden="true" />
      </AwsButton>
      {open && (
        <div
          role="menu"
          className="absolute top-9 left-0 z-30 min-w-56 border border-aws-border bg-white py-1 text-sm shadow-lg"
        >
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              disabled={item.disabledReason !== undefined}
              title={item.disabledReason}
              onClick={() => {
                setOpen(false)
                item.onClick?.()
              }}
              className={`block w-full px-4 py-1.5 text-left hover:bg-[#f2f3f3] disabled:cursor-not-allowed disabled:opacity-50 ${
                item.danger ? 'text-aws-error' : 'text-aws-text'
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

export function AwsRefreshButton({ onClick, busy = false }: { onClick: () => void; busy?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title="Refresh"
      aria-label="Refresh"
      className="rounded-sm border border-transparent p-1.5 text-aws-muted hover:border-aws-border-dark hover:bg-[#f2f3f3] hover:text-aws-text"
    >
      <RefreshCw className={`h-4 w-4 ${busy ? 'animate-spin' : ''}`} aria-hidden="true" />
    </button>
  )
}

export function AwsPreferencesButton({ disabledReason = 'Table preferences are not stored in CloudForge.' }: { disabledReason?: string }) {
  return (
    <button
      type="button"
      disabled
      title={disabledReason}
      className="flex items-center gap-1.5 rounded-sm border border-transparent px-2 py-1.5 text-sm text-aws-muted disabled:cursor-not-allowed disabled:opacity-60"
    >
      <Settings2 className="h-4 w-4" aria-hidden="true" />
      Preferences
    </button>
  )
}

type FlashTone = 'success' | 'info' | 'error'
const FLASH: Record<FlashTone, { cls: string; icon: ReactNode }> = {
  success: {
    cls: 'border-[#9bd39b] bg-[#e6f4ea] text-[#1e4d1e]',
    icon: <CheckCircle2 className="h-4 w-4 text-aws-success" aria-hidden="true" />,
  },
  info: {
    cls: 'border-[#a9c7e8] bg-[#e9f5fe] text-[#0b3d5c]',
    icon: <Info className="h-4 w-4 text-aws-link" aria-hidden="true" />,
  },
  error: {
    cls: 'border-[#f0b7ba] bg-aws-error-bg text-[#7a1a1e]',
    icon: <AlertCircle className="h-4 w-4 text-aws-error" aria-hidden="true" />,
  },
}

/** AWS-style flash banner (the console's inline alert strip). */
export function AwsFlash({ tone = 'info', children }: { tone?: FlashTone; children: ReactNode }) {
  const { cls, icon } = FLASH[tone]
  return (
    <div role="status" className={`mb-3 flex items-start gap-2 border px-3 py-2 text-sm ${cls}`}>
      <span className="mt-0.5 shrink-0">{icon}</span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

/** Copy-to-clipboard control used for ARNs, keys, URIs and IDs. */
export function AwsCopy({ value, label = 'Copy' }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard?.writeText(value).then(() => {
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1500)
        })
      }}
      className="inline-flex items-center gap-1 text-xs text-aws-link hover:underline"
      aria-label={`${label} ${value}`}
    >
      {copied ? <Check className="h-3.5 w-3.5" aria-hidden="true" /> : <Clipboard className="h-3.5 w-3.5" aria-hidden="true" />}
      {copied ? 'Copied' : label}
    </button>
  )
}

/** AWS-style multi-step create wizard with the numbered step rail. */
export function AwsWizard({
  title,
  steps,
  current,
  onStep,
  onCancel,
  onSubmit,
  submitLabel = 'Create',
  busy = false,
  children,
}: {
  title: string
  steps: string[]
  current: number
  onStep: (index: number) => void
  onCancel: () => void
  onSubmit: () => void
  submitLabel?: string
  busy?: boolean
  children: ReactNode
}) {
  const last = current === steps.length - 1
  return (
    <section className="border border-aws-border bg-white shadow-sm">
      <div className="flex items-center justify-between gap-2 border-b border-aws-border px-4 py-3">
        <h2 className="text-base font-semibold">{title}</h2>
        <button type="button" onClick={onCancel} className="text-sm text-aws-link hover:underline">
          Cancel
        </button>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-[220px_1fr]">
        <ol className="border-b border-aws-border bg-[#f7f8f8] p-4 text-sm md:border-r md:border-b-0">
          {steps.map((step, i) => (
            <li key={step}>
              <button
                type="button"
                onClick={() => onStep(i)}
                className={`flex w-full items-center gap-2 px-2 py-2 text-left ${
                  i === current ? 'font-semibold text-aws-orange-dark' : i < current ? 'text-aws-text' : 'text-aws-muted'
                }`}
              >
                <span
                  className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs ${
                    i < current
                      ? 'border-aws-success bg-aws-success text-white'
                      : i === current
                        ? 'border-aws-orange bg-aws-orange text-aws-navy'
                        : 'border-aws-border-dark bg-white'
                  }`}
                >
                  {i < current ? <Check className="h-3.5 w-3.5" aria-hidden="true" /> : i + 1}
                </span>
                {step}
              </button>
            </li>
          ))}
        </ol>
        <div className="min-w-0 p-4">
          {children}
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-aws-border pt-3">
            <AwsButton onClick={onCancel}>Cancel</AwsButton>
            <div className="flex items-center gap-2">
              {current > 0 && (
                <AwsButton variant="link" onClick={() => onStep(current - 1)}>
                  Previous
                </AwsButton>
              )}
              {last ? (
                <AwsButton variant="primary" onClick={onSubmit} disabled={busy}>
                  {busy ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : null}
                  {submitLabel}
                </AwsButton>
              ) : (
                <AwsButton variant="primary" onClick={() => onStep(current + 1)}>
                  Next
                </AwsButton>
              )}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

/** Empty state with an optional primary action (AWS console "no resources"). */
export function AwsEmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="border border-aws-border bg-white px-6 py-10 text-center">
      <p className="text-base font-semibold">{title}</p>
      {description && <p className="mx-auto mt-1 max-w-xl text-sm text-aws-muted">{description}</p>}
      {action && <div className="mt-4 flex justify-center">{action}</div>}
    </div>
  )
}

/** Small summary tile used on service dashboards. */
export function AwsStat({
  label,
  value,
  hint,
  to,
}: {
  label: string
  value: string | number
  hint?: string
  to?: string
}) {
  const body = (
    <>
      <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-aws-text">{value}</p>
      {hint && <p className="mt-1 text-xs text-aws-muted">{hint}</p>}
    </>
  )
  return to ? (
    <Link to={to} className="block border border-aws-border bg-white px-4 py-3 hover:border-aws-orange hover:no-underline">
      {body}
    </Link>
  ) : (
    <div className="border border-aws-border bg-white px-4 py-3">{body}</div>
  )
}

/** Definition list with AWS detail-panel styling. */
export function AwsKeyValue({ rows }: { rows: { label: string; value: ReactNode }[] }) {
  return (
    <dl className="divide-y divide-aws-border border-y border-aws-border">
      {rows.map((row) => (
        <div key={row.label} className="grid grid-cols-1 gap-1 px-1 py-2 text-sm sm:grid-cols-[220px_1fr]">
          <dt className="font-medium text-aws-text">{row.label}</dt>
          <dd className="min-w-0 break-all text-aws-text">{row.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** "Back to list" style link used above detail panels. */
export function AwsBackLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to} className="inline-flex items-center gap-1 text-sm">
      <ChevronRight className="h-4 w-4 rotate-180" aria-hidden="true" />
      {children}
    </Link>
  )
}
