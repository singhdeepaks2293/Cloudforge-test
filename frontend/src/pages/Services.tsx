import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Search } from 'lucide-react'
import { PageHeader } from '../components/AwsCard.tsx'
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
  const shown = results?.length ?? services.length

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'All Services' }]}
        title="All Services"
        description="Every CloudForge service, from the backend service registry. Availability and fidelity are honest — a service with no working operations is labelled, never shown as working."
      />

      <div className="flex flex-wrap items-center gap-3 border border-aws-border bg-white px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center overflow-hidden rounded-sm border border-aws-border-dark bg-white">
          <Search className="ml-2 h-4 w-4 shrink-0 text-aws-muted" aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search services, operations, resource types, learning"
            aria-label="Search services"
            className="min-w-0 flex-1 px-2 py-1.5 text-sm focus:outline-none"
          />
        </div>
        <p className="text-sm text-aws-muted">
          {servicesQuery.data ? `${shown} of ${services.length} services` : 'Loading registry…'}
        </p>
      </div>

      {servicesQuery.isPending && <Loading label="Loading services" />}
      {servicesQuery.isError && <ErrorBox message={`Service registry could not be loaded (${servicesQuery.error.message}).`} />}

      {results !== null && (
        <section className="border border-aws-border bg-white">
          <div className="border-b border-aws-border px-4 py-2">
            <h2 className="text-sm font-semibold">
              {results.length} matching service{results.length === 1 ? '' : 's'}
            </h2>
          </div>
          {results.length === 0 ? (
            <p className="px-4 py-6 text-sm text-aws-muted">No service matches &ldquo;{query}&rdquo;.</p>
          ) : (
            <ServiceGrid services={results} />
          )}
        </section>
      )}

      {results === null &&
        groups.map((g) => (
          <section key={g.category} className="border border-aws-border bg-white">
            <div className="flex items-baseline justify-between gap-2 border-b border-aws-border px-4 py-2">
              <h2 className="text-sm font-semibold">{g.category}</h2>
              <span className="text-xs text-aws-muted">
                {g.services.length} service{g.services.length === 1 ? '' : 's'}
              </span>
            </div>
            <ServiceGrid services={g.services} />
          </section>
        ))}

      {results === null && servicesQuery.data && services.length === 0 && (
        <p className="text-sm text-aws-muted">The registry returned no services.</p>
      )}
    </div>
  )
}

function ServiceGrid({ services }: { services: RegistryService[] }) {
  return (
    <ul className="grid grid-cols-1 bg-white sm:grid-cols-2 xl:grid-cols-3">
      {services.map((s) => (
        <li key={s.id} className="border-b border-aws-border sm:border-r">
          <div className="flex items-start gap-2 p-3 hover:bg-[#f9f9f9]">
            <Link to={s.route} className="flex min-w-0 flex-1 items-start gap-3 hover:no-underline">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-aws-navy text-aws-orange">
                <ServiceIcon icon={s.icon} className="h-5 w-5" />
              </span>
              <span className="min-w-0">
                <span className="flex flex-wrap items-center gap-1.5">
                  <span className="font-medium text-aws-link hover:underline">{s.name}</span>
                  <FidelityBadge fidelity={s.fidelity} footnote="" />
                </span>
                <span className="mt-0.5 line-clamp-2 block text-xs text-aws-muted">{s.description}</span>
                <span className="mt-0.5 block text-[11px] text-aws-muted">
                  {s.availability}
                  {s.operations.length > 0 ? ` · ${s.operations.length} operations` : ''}
                </span>
              </span>
            </Link>
            <FavoriteStar serviceId={s.id} serviceName={s.name} />
          </div>
        </li>
      ))}
    </ul>
  )
}
