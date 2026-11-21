import { useState, type ReactNode } from 'react'
import { Outlet } from 'react-router-dom'
import { AwsHeader } from './AwsHeader.tsx'
import { AwsNav } from './AwsNav.tsx'
import { AwsBadge, type BadgeTone } from './AwsBadge.tsx'
import { AwsCard } from './AwsCard.tsx'
import { getToken } from '../services/api.ts'

/**
 * Console shell — AWS-console style: dark navy header on every page,
 * collapsible left service nav, light-grey (#eaeded) content area.
 * Re-exports the legacy helpers (Card, ErrorBox, Loading, StatusBadge)
 * so existing pages keep compiling; new pages use the Aws* components.
 */
export function Layout() {
  const [navCollapsed, setNavCollapsed] = useState(false)
  return (
    <div className="flex min-h-screen flex-col bg-aws-bg text-aws-text">
      <AwsHeader onToggleNav={() => setNavCollapsed((v) => !v)} />
      <div className="flex min-h-0 flex-1">
        <aside
          className={`shrink-0 overflow-y-auto border-r border-aws-border bg-white ${
            navCollapsed ? 'w-14' : 'w-60'
          }`}
        >
          <AwsNav collapsed={navCollapsed} />
          {!navCollapsed && (
            <div className="border-t border-aws-border px-4 py-3">
              <p className="text-[11px] leading-snug text-aws-muted">
                CloudForge — local learning cloud.
                <br />
                AWS-console style, CloudForge branding. Not AWS.
              </p>
              <p className="mt-1 text-[11px] text-aws-muted">
                {getToken() !== null ? 'Signed in (local account)' : 'Not signed in'}
              </p>
            </div>
          )}
        </aside>
        <main className="min-w-0 flex-1 px-4 py-4 sm:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

export function StatusBadge({ ok, label }: { ok: boolean | null; label: string }) {
  const tone: BadgeTone = ok === null ? 'grey' : ok ? 'green' : 'red'
  const dot = ok === null ? 'bg-aws-muted' : ok ? 'bg-aws-success' : 'bg-aws-error'
  return (
    <AwsBadge tone={tone}>
      <span className={`h-2 w-2 rounded-full ${dot}`} />
      {label}
    </AwsBadge>
  )
}

export function Card({ title, children }: { title: string; children: ReactNode }) {
  return <AwsCard title={title}>{children}</AwsCard>
}

export function ErrorBox({ message }: { message: string }) {
  return (
    <div className="border border-[#f5c6cb] bg-aws-error-bg px-4 py-3 text-sm text-aws-error" role="alert">
      {message}
    </div>
  )
}

export function Loading({ label }: { label: string }) {
  return <p className="text-sm text-aws-muted">{label}…</p>
}
