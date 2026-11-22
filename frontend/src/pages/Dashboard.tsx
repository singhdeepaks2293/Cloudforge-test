import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  ArrowRight,
  BookOpen,
  CircleDollarSign,
  FlaskConical,
  GraduationCap,
  HeartPulse,
  LayoutGrid,
  Server,
  Star,
} from 'lucide-react'
import { fetchCourses, fetchHealth, fetchLabs, getToken } from '../services/api.ts'
import { AwsBadge } from '../components/AwsBadge.tsx'
import { AwsStat } from '../components/AwsShell.tsx'
import { ServiceIcon } from '../components/AwsNav.tsx'
import { ErrorBox, Loading, StatusBadge } from '../components/Layout.tsx'
import { getFavoriteServiceIds, getRecentServiceIds } from '../services/catalog.ts'
import { useServices } from '../services/registry.ts'

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

function WidgetTitle({ icon, title, action }: { icon: ReactNode; title: string; action?: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 border-b border-aws-border px-4 py-3">
      <div className="flex items-center gap-2">
        <span className="text-aws-orange-dark">{icon}</span>
        <h2 className="text-base font-semibold">{title}</h2>
      </div>
      {action}
    </div>
  )
}

/**
 * Console Home — AWS Console Home layout with CloudForge branding and
 * real local data only: live control-plane health, real lab count,
 * real courses, recently visited services and favorites from local
 * history. No fake AWS numbers, no fake costs.
 */
export function Dashboard() {
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const courses = useQuery({ queryKey: ['courses'], queryFn: fetchCourses, retry: 1 })
  const authed = getToken() !== null
  const labs = useQuery({ queryKey: ['labs'], queryFn: fetchLabs, retry: 1, enabled: authed })
  const servicesQuery = useServices()
  const services = servicesQuery.data ?? []
  const byId = new Map(services.map((s) => [s.id, s]))
  const recent = getRecentServiceIds()
    .map((id) => byId.get(id))
    .filter((s): s is NonNullable<typeof s> => s !== undefined)
  const favorites = getFavoriteServiceIds()
    .map((id) => byId.get(id))
    .filter((s): s is NonNullable<typeof s> => s !== undefined)

  return (
    <div className="space-y-4">
      <section className="border border-aws-border bg-white">
        <div className="flex flex-wrap items-start justify-between gap-4 px-5 py-5">
          <div className="max-w-3xl">
            <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">CloudForge Console</p>
            <h1 className="mt-1 text-2xl font-semibold">Welcome to CloudForge</h1>
            <p className="mt-2 text-sm text-aws-muted">
              Your local learning cloud — build cloud skills, not cloud bills. Everything below comes from the live
              local API; nothing is invented. This is a CloudForge learning environment, not the AWS Management
              Console.
            </p>
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <Link
                to="/courses"
                className="rounded-sm border border-[#d97706] bg-aws-orange px-3 py-1.5 text-sm font-medium text-aws-navy hover:bg-aws-orange-dark hover:text-white hover:no-underline"
              >
                Browse courses
              </Link>
              <Link
                to="/labs"
                className="rounded-sm border border-aws-border-dark bg-white px-3 py-1.5 text-sm font-medium text-aws-text hover:bg-[#f2f3f3] hover:no-underline"
              >
                Open your labs
              </Link>
              <Link to="/services" className="px-1 py-1.5 text-sm font-medium text-aws-link hover:underline">
                View all services
              </Link>
            </div>
          </div>
          <div className="flex items-center gap-2 border border-aws-border bg-[#f7f8f8] px-3 py-2 text-xs text-aws-muted">
            <span className="h-2 w-2 rounded-full bg-aws-orange" />
            Local region: {health.data ? `${health.data.service}` : 'connecting…'}
          </div>
        </div>
      </section>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <div className="space-y-4 xl:col-span-2">
          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<LayoutGrid className="h-4 w-4" aria-hidden="true" />}
              title="Recently visited services"
              action={
                <Link to="/services" className="text-sm text-aws-link hover:underline">
                  View all services
                </Link>
              }
            />
            <div className="px-4 py-4">
              {recent.length === 0 ? (
                <p className="text-sm text-aws-muted">
                  No services visited yet in this browser. Use the search box or the left nav — visited services
                  will appear here.
                </p>
              ) : (
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                  {recent.slice(0, 8).map((s) => (
                    <Link
                      key={s.id}
                      to={s.route}
                      className="flex flex-col gap-1 border border-aws-border px-3 py-3 text-aws-text hover:border-aws-orange hover:no-underline"
                    >
                      <ServiceIcon icon={s.icon} className="h-5 w-5 text-aws-orange-dark" />
                      <span className="text-sm font-semibold">{s.name}</span>
                      <span className="line-clamp-2 text-xs text-aws-muted">{s.description}</span>
                    </Link>
                  ))}
                </div>
              )}
            </div>
          </section>

          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<HeartPulse className="h-4 w-4" aria-hidden="true" />}
              title="Service health"
              action={
                health.data ? (
                  <StatusBadge ok={health.data.ok} label={health.data.ok ? 'Healthy' : 'Unhealthy'} />
                ) : undefined
              }
            />
            <div className="px-4 py-4">
              {health.isPending && <Loading label="Checking API health" />}
              {health.isError && (
                <ErrorBox
                  message={`API unreachable. Start the Go API (backend/cmd/api) — no status is shown instead of a fake one. Error: ${health.error.message}`}
                />
              )}
              {health.data && (
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                  <div>
                    <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">Service</p>
                    <p className="mt-1 font-mono text-sm">{health.data.service}</p>
                  </div>
                  <div>
                    <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">Version</p>
                    <p className="mt-1 font-mono text-sm">{health.data.version}</p>
                  </div>
                  <div>
                    <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">Uptime</p>
                    <p className="mt-1 font-mono text-sm">{formatUptime(health.data.uptime_seconds)}</p>
                  </div>
                  <div>
                    <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">Network engine</p>
                    <p className="mt-1">
                      <StatusBadge
                        ok={health.data.network_engine_reachable}
                        label={health.data.network_engine_reachable ? 'Reachable' : 'Unreachable'}
                      />
                    </p>
                  </div>
                  {health.data.detail && (
                    <div className="col-span-2 sm:col-span-4">
                      <p className="text-xs font-medium tracking-wide text-aws-muted uppercase">Detail</p>
                      <p className="mt-1 text-sm">{health.data.detail}</p>
                    </div>
                  )}
                </div>
              )}
            </div>
          </section>

          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<CircleDollarSign className="h-4 w-4" aria-hidden="true" />}
              title="Cost and usage"
              action={<AwsBadge tone="grey">Local only</AwsBadge>}
            />
            <div className="px-4 py-4">
              <p className="text-sm">
                CloudForge runs entirely on your machine — there is <strong>no AWS billing, subscription or
                credit card</strong>. This widget deliberately shows no invented spend.
              </p>
              <div className="mt-3 grid grid-cols-3 gap-3">
                <AwsStat label="AWS spend" value="$0.00" hint="Nothing is billed" />
                <AwsStat label="Labs running" value={authed ? (labs.data ?? []).filter((l) => l.status === 'RUNNING' || l.status === 'READY').length : '—'} hint={authed ? 'From /api/v1/labs' : 'Sign in to see'} />
                <AwsStat label="Services" value={servicesQuery.isError ? '—' : services.length} hint={servicesQuery.isError ? 'Registry unavailable' : 'Live backend registry'} />
              </div>
            </div>
          </section>
        </div>

        <div className="space-y-4">
          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<Server className="h-4 w-4" aria-hidden="true" />}
              title="Your labs"
              action={
                <Link to="/labs" className="text-sm text-aws-link hover:underline">
                  View all
                </Link>
              }
            />
            <div className="px-4 py-4">
              {!authed && (
                <p className="text-sm text-aws-muted">
                  <Link to="/login">Sign in</Link> to see your lab sessions.
                </p>
              )}
              {authed && labs.isPending && <Loading label="Loading labs" />}
              {authed && labs.isError && <ErrorBox message={`Could not load labs: ${labs.error.message}`} />}
              {authed && labs.data && (
                <div>
                  <p className="text-3xl font-semibold">{labs.data.length}</p>
                  <p className="text-sm text-aws-muted">
                    lab session{labs.data.length === 1 ? '' : 's'} (
                    {labs.data.filter((l) => l.status === 'RUNNING' || l.status === 'READY').length} running / ready)
                  </p>
                  {labs.data.length > 0 && (
                    <ul className="mt-3 divide-y divide-aws-border text-sm">
                      {labs.data.slice(0, 4).map((l) => (
                        <li key={l.id} className="flex items-center justify-between gap-2 py-2">
                          <span className="truncate">{l.name}</span>
                          <AwsBadge tone={l.status === 'READY' || l.status === 'RUNNING' ? 'green' : 'grey'}>
                            {l.status.toLowerCase()}
                          </AwsBadge>
                        </li>
                      ))}
                    </ul>
                  )}
                  <Link to="/labs" className="mt-3 inline-flex items-center gap-1 text-sm font-medium">
                    Open Instances <ArrowRight className="h-4 w-4" aria-hidden="true" />
                  </Link>
                </div>
              )}
            </div>
          </section>

          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<GraduationCap className="h-4 w-4" aria-hidden="true" />}
              title="Courses"
              action={
                <Link to="/courses" className="text-sm text-aws-link hover:underline">
                  Browse
                </Link>
              }
            />
            <div className="px-4 py-4">
              {courses.isPending && <Loading label="Loading courses" />}
              {courses.isError && <ErrorBox message={`Could not load courses from the API: ${courses.error.message}`} />}
              {courses.data && courses.data.length === 0 && (
                <p className="text-sm text-aws-muted">The course catalogue is empty — no courses published yet.</p>
              )}
              {courses.data && courses.data.length > 0 && (
                <div>
                  <p className="text-3xl font-semibold">{courses.data.length}</p>
                  <p className="text-sm text-aws-muted">
                    course{courses.data.length === 1 ? '' : 's'} in the catalogue
                  </p>
                  <ul className="mt-3 divide-y divide-aws-border">
                    {courses.data.slice(0, 4).map((c) => (
                      <li key={c.id} className="py-2 text-sm">
                        <span className="font-medium">{c.title}</span>{' '}
                        <span className="text-aws-muted">
                          · {c.track} · {c.lessons} lesson{c.lessons === 1 ? '' : 's'}
                        </span>
                      </li>
                    ))}
                  </ul>
                  <Link to="/courses" className="mt-2 inline-block text-sm font-medium">
                    View all courses
                  </Link>
                </div>
              )}
            </div>
          </section>

          <section className="border border-aws-border bg-white">
            <WidgetTitle
              icon={<Star className="h-4 w-4" aria-hidden="true" />}
              title="Favorites"
              action={
                <Link to="/services" className="text-sm text-aws-link hover:underline">
                  Add
                </Link>
              }
            />
            <div className="px-4 py-4">
              {favorites.length === 0 ? (
                <p className="text-sm text-aws-muted">
                  No favorites yet. Open a service and add it to Favorites from the service page.
                </p>
              ) : (
                <ul className="divide-y divide-aws-border text-sm">
                  {favorites.map((s) => (
                    <li key={s.id} className="py-2">
                      <Link to={s.route} className="flex items-center gap-2 font-medium">
                        <ServiceIcon icon={s.icon} /> {s.name}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>
        </div>
      </div>

      <section className="border border-aws-border bg-white">
        <WidgetTitle
          icon={<FlaskConical className="h-4 w-4" aria-hidden="true" />}
          title="Build a solution"
        />
        <div className="grid grid-cols-1 gap-3 px-4 py-4 md:grid-cols-3">
          {[
            {
              icon: <Server className="h-5 w-5 text-aws-orange-dark" aria-hidden="true" />,
              title: 'Launch a lab instance',
              text: 'Start from the EC2-style Instances table and open a lab workstation with topology and terminal.',
              to: '/labs',
              cta: 'Open Instances',
            },
            {
              icon: <BookOpen className="h-5 w-5 text-aws-orange-dark" aria-hidden="true" />,
              title: 'Learn VPC networking',
              text: 'Follow the networking track: VPCs, subnets, route tables and security groups, with a guided lab.',
              to: '/learn',
              cta: 'Open Learn',
            },
            {
              icon: <FlaskConical className="h-5 w-5 text-aws-orange-dark" aria-hidden="true" />,
              title: 'Try a break-and-fix challenge',
              text: 'Diagnose a broken local network from the curriculum — the same skills as a real incident.',
              to: '/courses',
              cta: 'Browse courses',
            },
          ].map((card) => (
            <div key={card.title} className="border border-aws-border p-4">
              {card.icon}
              <h3 className="mt-2 text-sm font-semibold">{card.title}</h3>
              <p className="mt-1 text-sm text-aws-muted">{card.text}</p>
              <Link to={card.to} className="mt-3 inline-flex items-center gap-1 text-sm font-medium">
                {card.cta} <ArrowRight className="h-4 w-4" aria-hidden="true" />
              </Link>
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}
