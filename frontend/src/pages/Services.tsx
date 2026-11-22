import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { FavoriteStar } from '../components/FavoriteStar.tsx'
import { ServiceIcon } from '../components/AwsNav.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { fetchServices } from '../services/registry.ts'
import { groupServicesByCategory, searchRegistryServices } from '../services/registry.ts'
import type { RegistryService } from '../types/index.ts'

const EMPTY_SERVICES: RegistryService[] = []

/**
 * All Services — the AWS console's services index, rendered from the
 * backend registry (GET /api/v1/services). Search covers names,
 * descriptions, categories, API operations, resource types and
 * learning exercises; every entry shows its category, availability
 * and fidelity, and navigates to the service's real page.
 */
export function Services() {
  const servicesQuery = useQuery({ queryKey: ['services'], queryFn: fetchServices, retry: 1 })
  const [query, setQuery] = useState('')
  // Stable empty array: `servicesQuery.data ?? []` would be a new
  // array every render and defeat the memos below (oxlint warning).
  const services = servicesQuery.data ?? EMPTY_SERVICES
  const results = useMemo(
    () => (query.trim() === '' ? null : searchRegistryServices(query, services)),
    [query, services],
  )
  const groups = useMemo(() => groupServicesByCategory(services), [services])

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'All Services' }]}
        title="All Services"
        description="Every CloudForge service, from the backend service registry. Availability and fidelity are honest — a service with no working operations is labelled, never shown as working."
      />
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search services, operations, resource types, learning"
        aria-label="Search services"
        className="w-full max-w-xl rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
      />
      {servicesQuery.isPending && <Loading label="Loading services" />}
      {servicesQuery.isError && <ErrorBox message={`Service registry could not be loaded (${servicesQuery.error.message}).`} />}
      {results !== null && (
        <AwsCard title={`${results.length} matching service${results.length === 1 ? '' : 's'}`}>
          {results.length === 0 ? (
            <p className="text-sm text-aws-muted">No service matches &ldquo;{query}&rdquo;.</p>
          ) : (
            <ServiceList services={results} />
          )}
        </AwsCard>
      )}
      {results === null &&
        groups.map((g) => (
          <AwsCard key={g.category} title={g.category}>
            <ServiceList services={g.services} />
          </AwsCard>
        ))}
    </div>
  )
}

function ServiceList({ services }: { services: RegistryService[] }) {
  return (
    <ul className="divide-y divide-aws-border">
      {services.map((s) => (
        <li key={s.id} className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
          <ServiceIcon icon={s.icon} className="mt-0.5 h-5 w-5 shrink-0 text-aws-muted" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <Link to={s.route} className="font-medium text-aws-link hover:underline">
                {s.name}
              </Link>
              <FidelityBadge fidelity={s.fidelity} />
              <span className="text-xs text-aws-muted">{s.availability}</span>
            </div>
            <p className="mt-0.5 text-sm text-aws-muted">{s.description}</p>
            {s.operations.length > 0 && (
              <p className="mt-0.5 text-xs text-aws-muted">
                Operations: {s.operations.map((o) => o.operation).join(', ')}
              </p>
            )}
          </div>
          <FavoriteStar serviceId={s.id} serviceName={s.name} />
        </li>
      ))}
    </ul>
  )
}
