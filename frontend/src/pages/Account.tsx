import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { logout, fetchHealth, fetchMe, getToken } from '../services/api.ts'
import { DEFAULT_REGION, getFavoriteServiceIds, getStoredRegion, LOCAL_REGIONS, setStoredRegion } from '../services/catalog.ts'
import { useServices } from '../services/registry.ts'

/**
 * Account Settings (Phase 1): profile and role (from /auth/me), the
 * local region preference, the mode the control plane reports (lite /
 * full), Favorites management, and sign out.
 */
export function Account() {
  const navigate = useNavigate()
  const authed = getToken() !== null
  const me = useQuery({ queryKey: ['me'], queryFn: fetchMe, retry: 0, enabled: authed })
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const services = useServices()
  const [region, setRegion] = useState<string>(() => getStoredRegion())
  const favorites = getFavoriteServiceIds()
  const byId = new Map((services.data ?? []).map((s) => [s.id, s]))

  if (!authed) {
    return (
      <div className="space-y-4">
        <PageHeader
          breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Account Settings' }]}
          title="Account Settings"
          description="Sign in to see your profile, role and preferences."
        />
        <ErrorBox message="You are not signed in." />
        <Link to="/login" className="text-sm text-aws-link hover:underline">Sign in</Link>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Account Settings' }]}
        title="Account Settings"
        description="Your CloudForge profile, role, region and favourites."
      />
      {me.isPending && <Loading label="Loading profile" />}
      {me.isError && <ErrorBox message={`Profile could not be loaded (${me.error.message}).`} />}
      {me.data && (
        <AwsCard title="Profile">
          <dl className="space-y-2 text-sm">
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Name</dt><dd>{me.data.name ?? '—'}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Email</dt><dd>{me.data.email}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Role</dt><dd>{me.data.role ?? 'learner'}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">User ID</dt><dd className="break-all">{me.data.id}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Created</dt><dd>{me.data.created_at ? new Date(me.data.created_at).toLocaleString() : '—'}</dd></div>
          </dl>
        </AwsCard>
      )}
      <AwsCard title="Region">
        <p className="mb-2 text-sm text-aws-muted">
          Local regions only — CloudForge runs locally, not on AWS. Region scoping is display-only in Phase 1: all
          resources live in cf-local-1.
        </p>
        <div className="flex flex-wrap gap-2">
          {LOCAL_REGIONS.map((r) => (
            <button
              key={r.id}
              type="button"
              onClick={() => {
                setRegion(r.id)
                setStoredRegion(r.id)
              }}
              className={`rounded-sm border px-3 py-1.5 text-sm ${r.id === region ? 'border-aws-orange bg-aws-orange-light font-medium text-aws-orange-dark' : 'border-aws-border-dark bg-white'}`}
            >
              {r.label}
            </button>
          ))}
        </div>
        <p className="mt-2 text-xs text-aws-muted">Selected: {LOCAL_REGIONS.find((r) => r.id === region)?.label ?? DEFAULT_REGION}</p>
      </AwsCard>
      <AwsCard title="Deployment">
        <p className="text-sm text-aws-muted">
          Control plane: {health.data?.ok ? 'healthy' : health.isError ? 'unreachable' : 'loading…'} · Mode:{' '}
          {health.data?.lite === true ? 'lite (classroom laptop — 1 lab, Micro, 30 min)' : 'full'} · Network engine:{' '}
          {health.data?.network_engine_reachable ? 'reachable' : 'unreachable'}
        </p>
      </AwsCard>
      <AwsCard title="Favorites">
        {favorites.length === 0 ? (
          <p className="text-sm text-aws-muted">
            No favourites yet — star a service on the <Link to="/services" className="text-aws-link hover:underline">All Services</Link> page.
            Favourites are stored per user in this browser.
          </p>
        ) : (
          <ul className="space-y-1 text-sm">
            {favorites.map((id) => (
              <li key={id}>
                <Link to={byId.get(id)?.route ?? '/services'} className="text-aws-link hover:underline">
                  {byId.get(id)?.name ?? id}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </AwsCard>
      <AwsButton
        onClick={() => {
          void logout().finally(() => navigate('/login'))
        }}
      >
        Sign out
      </AwsButton>
    </div>
  )
}
