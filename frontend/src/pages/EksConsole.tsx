import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { AwsPreferencesButton, AwsRefreshButton, AwsToolbar } from '../components/AwsShell.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')

/**
 * EKS console (Phase 4) — EKS is UNSUPPORTED by an explicit Phase 4
 * decision, and this console says so instead of presenting a cluster
 * that does not exist. Reasons: no Docker/Kubernetes in the sandbox
 * to verify a cluster against; a managed-Kubernetes control plane
 * would dominate lite classroom-laptop mode; and the Kubernetes
 * concepts users need first — Pod, Deployment, Service, ConfigMap,
 * Namespace, Job — map directly onto the ECS runtime, so the learning
 * path below teaches them for real against ECS instead of against a
 * diagram. A k3s-based EKS remains a possible future phase on a real
 * Docker/KVM host, gated behind a capability check.
 *
 * The Clusters section shows the whole page (decision, empty
 * clusters table with the survey's exact columns, learning path)
 * so the existing render test still sees every element at once.
 * There is no create wizard — Create cluster is deliberately
 * disabled by the Phase 4 decision.
 */
export function EksConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const info = useQuery({ queryKey: ['eks-info'], queryFn: () => request<Rec>('/api/v1/aws/eks/info'), retry: 1 })
  const d: Rec = info.data ?? { id: '' }
  const path = Array.isArray(d.learning_path) ? (d.learning_path as Record<string, unknown>[]) : []
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'clusters')
  const [clusterFilter, setClusterFilter] = useState('')

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Containers', to: '/services' }, { label: 'EKS' }]}
      title="EKS — Kubernetes"
      description="EKS is not built in CloudForge. This page records the decision and the Kubernetes-concepts learning path that uses the ECS runtime instead."
      fidelity="UNSUPPORTED"
      fidelityFootnote="no cluster exists here"
      subNav={[
        { id: 'home', label: 'EKS dashboard' },
        { id: 'clusters', label: 'Clusters' },
        { id: 'settings', label: 'Settings' },
        { id: 'eks-anywhere', label: 'EKS Anywhere' },
        { id: 'related', label: 'Related services' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="EKS"
            banner="EKS is not built in CloudForge. Kubernetes concepts — Pod, Deployment, Service, ConfigMap, Namespace, Job — map directly onto the ECS runtime, so the learning path below teaches them for real against ECS instead of against a diagram."
            actions={[
              { label: 'Open ECS (learn Kubernetes concepts)', to: '/services/ecs' },
            ]}
            cards={[
              { label: 'Clusters', count: 0, onSelect: () => setSection('clusters'), note: 'EKS is UNSUPPORTED — no cluster exists' },
            { label: 'Kubernetes concepts', count: path.length, onSelect: () => setSection('clusters'), note: 'Taught on the ECS runtime' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={info.isFetching}
            health={[
              info.isError ? 'EKS information could not be loaded.' : 'EKS is UNSUPPORTED — no cluster exists.',
            "A k3s-based EKS remains a possible future phase on a real Docker/KVM host, gated behind a capability check.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — containers track', to: '/learn' },
            { label: 'ECS (the Kubernetes-concepts runtime)', to: '/services/ecs' },
            ]}
          />
      )}
      {section === 'clusters' && (
        <>
      {info.isPending && <Loading label="Loading EKS information" />}
      {info.isError && <ErrorBox message={`EKS information could not be loaded (${info.error.message}).`} />}
      {info.data && (
        <>
          <AwsCard title="Decision">
            <p className="text-sm">
              <strong>Decision: {str(d as Rec, 'decision') || 'UNSUPPORTED'}.</strong> {str(d as Rec, 'reason')}
            </p>
            <p className="mt-2 text-sm text-aws-muted">
              No cluster-info endpoint is offered because there is no cluster. Creating a cluster is disabled — the button below is
              deliberately not wired to anything.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="EKS is UNSUPPORTED — there is no cluster to create.">
                Create cluster
              </AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="Clusters (0)">
            <p className="mb-3 text-sm text-aws-muted">
              No clusters — EKS is <strong>UNSUPPORTED</strong> in CloudForge and no cluster exists. The table below
              uses the survey&apos;s exact Clusters columns; every column is empty because there is nothing to list.
            </p>
            <AwsToolbar
              right={
                <>
                  <AwsRefreshButton onClick={() => void queryClient.invalidateQueries({ queryKey: ['eks-info'] })} busy={info.isFetching} />
                  <AwsPreferencesButton />
                </>
              }
            />
            <AwsTable<{ id: string }>
              caption="EKS clusters"
              emptyMessage="No clusters — EKS is UNSUPPORTED in CloudForge."
              rows={[]}
              totalCount={0}
              filter={clusterFilter}
              onFilterChange={setClusterFilter}
              filterPlaceholder="Filter clusters by name"
              columns={[
                { header: 'Cluster name', render: () => '—' },
                { header: 'Status', render: () => '—' },
                { header: 'Kubernetes version', render: () => '—' },
                { header: 'Support period', render: () => '—' },
                { header: 'Upgrade policy', render: () => '—' },
                { header: 'Created', render: () => '—' },
                { header: 'Certificate authority', render: () => '—' },
                { header: 'Provider', render: () => '—' },
                { header: 'Actions', render: () => '—' },
              ]}
            />
          </AwsCard>
          <AwsCard title="Kubernetes concepts — learn them on the ECS runtime">
            <p className="mb-3 text-sm text-aws-muted">
              Each Kubernetes concept below has a real CloudForge equivalent you can run today. Follow the route, then try it.
            </p>
            <AwsTable<Rec>
              caption="Kubernetes learning path"
              emptyMessage="No learning path returned."
              rows={path.map((p, i) => ({ ...p, id: typeof p.concept === 'string' ? (p.concept as string) : `concept-${i}` }))}
              columns={[
                { header: 'Kubernetes concept', render: (r) => <span className="font-semibold">{str(r, 'concept')}</span> },
                { header: 'What it is', render: (r) => str(r, 'what_it_is') },
                { header: 'CloudForge equivalent', render: (r) => str(r, 'cloudforge_equivalent') },
                {
                  header: 'Where',
                  render: (r) => (
                    <a href={str(r, 'route')} className="text-aws-link hover:underline">
                      {str(r, 'route')}
                    </a>
                  ),
                },
                { header: 'Try it', render: (r) => str(r, 'try_it') },
              ]}
            />
          </AwsCard>
        </>
      )}
        </>
      )}
      {section === 'settings' && (
        <UnsupportedSection
          title="Settings"
          reason="EKS settings are UNSUPPORTED in CloudForge — there is no cluster and no settings are stored."
          actionLabel="Save settings"
        />
      )}
      {section === 'eks-anywhere' && (
        <UnsupportedSection
          title="EKS Anywhere"
          reason="EKS Anywhere is UNSUPPORTED in CloudForge — there is no on-premises Kubernetes to manage."
          actionLabel="Create EKS Anywhere cluster"
        />
      )}
      {section === 'related' && (
        <AwsCard title="Related services">
          <p className="text-sm text-aws-muted">
            Kubernetes concepts map onto the ECS runtime — run them for real there instead of against a diagram.
          </p>
          <div className="mt-3 flex gap-2">
            <AwsButton variant="secondary" onClick={() => { window.location.href = '/services/ecs' }}>Open ECS</AwsButton>
            <AwsButton variant="secondary" onClick={() => { window.location.href = '/services/fargate' }}>Open Fargate</AwsButton>
          </div>
        </AwsCard>
      )}
    </ServiceConsole>
  )
}
