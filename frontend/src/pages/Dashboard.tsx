import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { BookOpen, FlaskConical, GraduationCap, Server } from 'lucide-react'
import { fetchCourses, fetchHealth, fetchLabs, getToken } from '../services/api.ts'
import { AwsBadge } from '../components/AwsBadge.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { ServiceIcon } from '../components/AwsNav.tsx'
import { ErrorBox, Loading, StatusBadge } from '../components/Layout.tsx'
import { getRecentServices } from '../services/catalog.ts'

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

/**
 * Console Home — AWS Console Home style, CloudForge branding, real
 * local data only: live control-plane health, real lab count, real
 * courses, recently visited services from local history. No fake AWS
 * numbers, no fake costs.
 */
export function Dashboard() {
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, retry: 1 })
  const courses = useQuery({ queryKey: ['courses'], queryFn: fetchCourses, retry: 1 })
  const authed = getToken() !== null
  const labs = useQuery({ queryKey: ['labs'], queryFn: fetchLabs, retry: 1, enabled: authed })
  const recent = getRecentServices()

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge' }, { label: 'Console Home' }]}
        title="Welcome to CloudForge"
        description="Your local learning cloud — build cloud skills, not cloud bills. Everything below comes from the live local API; nothing is invented."
      />

      <div className="border border-aws-border bg-aws-navy px-5 py-5 text-white">
        <h2 className="text-lg font-semibold">Console Home</h2>
        <p className="mt-1 text-sm text-[#d5dbdb]">
          Learn AWS-style cloud skills hands-on, on your own machine. No AWS account, subscription or credit
          card — the control plane, network engine and curriculum all run locally.
        </p>
        <div className="mt-4 flex flex-wrap gap-2">
          <Link to="/courses">
            <AwsButton variant="primary">Browse courses</AwsButton>
          </Link>
          <Link to="/labs">
            <AwsButton variant="secondary">Open your labs</AwsButton>
          </Link>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <AwsCard title="Service health">
          {health.isPending && <Loading label="Checking API health" />}
          {health.isError && (
            <ErrorBox
              message={`API unreachable. Start the Go API (backend/cmd/api) — no status is shown instead of a fake one. Error: ${health.error.message}`}
            />
          )}
          {health.data && (
            <dl className="grid grid-cols-2 gap-4">
              <div>
                <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Service</dt>
                <dd className="mt-1 font-mono text-sm">{health.data.service}</dd>
              </div>
              <div>
                <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Version</dt>
                <dd className="mt-1 font-mono text-sm">{health.data.version}</dd>
              </div>
              <div>
                <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Status</dt>
                <dd className="mt-1">
                  <StatusBadge ok={health.data.ok} label={health.data.ok ? 'Healthy' : 'Unhealthy'} />
                </dd>
              </div>
              <div>
                <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Uptime</dt>
                <dd className="mt-1 font-mono text-sm">{formatUptime(health.data.uptime_seconds)}</dd>
              </div>
              <div>
                <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Network engine</dt>
                <dd className="mt-1">
                  <StatusBadge
                    ok={health.data.network_engine_reachable}
                    label={health.data.network_engine_reachable ? 'Reachable (gRPC)' : 'Unreachable'}
                  />
                </dd>
              </div>
              {health.data.detail && (
                <div>
                  <dt className="text-xs font-medium tracking-wide text-aws-muted uppercase">Detail</dt>
                  <dd className="mt-1 text-sm">{health.data.detail}</dd>
                </div>
              )}
            </dl>
          )}
        </AwsCard>

        <AwsCard title="Your labs">
          {!authed && (
            <p className="text-sm text-aws-muted">
              <Link to="/login">Sign in</Link> to see your lab sessions.
            </p>
          )}
          {authed && labs.isPending && <Loading label="Loading labs" />}
          {authed && labs.isError && <ErrorBox message={`Could not load labs: ${labs.error.message}`} />}
          {authed && labs.data && (
            <div>
              <p className="text-2xl font-semibold">{labs.data.length}</p>
              <p className="text-sm text-aws-muted">
                lab session{labs.data.length === 1 ? '' : 's'} (
                {labs.data.filter((l) => l.status === 'RUNNING' || l.status === 'READY').length} running / ready)
              </p>
              <Link to="/labs" className="mt-3 inline-block text-sm font-medium">
                View instances
              </Link>
            </div>
          )}
        </AwsCard>

        <AwsCard title="Courses">
          {courses.isPending && <Loading label="Loading courses" />}
          {courses.isError && <ErrorBox message={`Could not load courses from the API: ${courses.error.message}`} />}
          {courses.data && courses.data.length === 0 && (
            <p className="text-sm text-aws-muted">The course catalogue is empty — no courses published yet.</p>
          )}
          {courses.data && courses.data.length > 0 && (
            <div>
              <p className="text-2xl font-semibold">{courses.data.length}</p>
              <p className="text-sm text-aws-muted">
                course{courses.data.length === 1 ? '' : 's'} in the catalogue
              </p>
              <ul className="mt-3 divide-y divide-aws-border">
                {courses.data.slice(0, 3).map((c) => (
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
        </AwsCard>
      </div>

      <AwsCard title="Recently visited services">
        {recent.length === 0 ? (
          <p className="text-sm text-aws-muted">
            No services visited yet in this browser. Use the search box or the left nav — visited services
            will appear here.
          </p>
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
            {recent.map((s) => (
              <Link
                key={s.name}
                to={s.to}
                className="flex flex-col gap-1 border border-aws-border px-3 py-3 text-aws-text hover:border-aws-orange hover:no-underline"
              >
                <ServiceIcon icon={s.icon} className="h-5 w-5 text-aws-orange-dark" />
                <span className="text-sm font-semibold">{s.name}</span>
                <span className="text-xs text-aws-muted">{s.label}</span>
              </Link>
            ))}
          </div>
        )}
      </AwsCard>

      <AwsCard title="Build a solution">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
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
              <Link to={card.to} className="mt-3 inline-block text-sm font-medium">
                {card.cta}
              </Link>
            </div>
          ))}
        </div>
      </AwsCard>

      <AwsCard title="Getting started">
        <ul className="space-y-2 text-sm">
          <li className="flex items-start gap-2">
            <GraduationCap className="mt-0.5 h-4 w-4 shrink-0 text-aws-orange-dark" aria-hidden="true" />
            <span>
              New here? Start with <Link to="/learn">Learn</Link> — pick a track and work through its lessons.
            </span>
          </li>
          <li className="flex items-start gap-2">
            <AwsBadge tone="blue">Local only</AwsBadge>
            <span>
              Active labs, VMs and resource counts appear only when the backend reports them — this page
              deliberately shows no placeholder numbers.
            </span>
          </li>
        </ul>
      </AwsCard>
    </div>
  )
}
