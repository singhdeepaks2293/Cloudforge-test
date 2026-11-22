import { useNavigate } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsBadge } from '../components/AwsBadge.tsx'
import { LaunchLabButton } from '../components/LabLauncher.tsx'
import { ADVANCED_PROJECTS } from './AdvancedData.ts'

/**
 * AdvancedProjects — the industry and end-to-end project catalogue.
 * Each project launches its lab through the normal lab flow; the
 * button disables itself in lite mode when the lab exceeds the
 * 30-minute lite limit. Grading rules are the validator rules the
 * lab is checked against (see docs/advanced/GRADING.md).
 */
export function AdvancedProjects() {
  const navigate = useNavigate()
  const industry = ADVANCED_PROJECTS.filter((p) => p.kind === 'industry')
  const endToEnd = ADVANCED_PROJECTS.filter((p) => p.kind === 'end-to-end')

  const renderProject = (p: (typeof ADVANCED_PROJECTS)[number]) => (
    <li key={p.id} className="py-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium">{p.title}</span>
        <span className="flex shrink-0 items-center gap-2">
          <AwsBadge tone={p.liteLaunchable ? 'green' : 'amber'}>
            {p.liteLaunchable ? 'Lite' : 'Full mode only'}
          </AwsBadge>
          <span className="text-xs text-aws-muted">
            {p.durationMinutes} min · {p.xpReward} XP
          </span>
          <LaunchLabButton
            definitionId={p.labDefinitionId}
            onLaunched={(lab) => navigate(`/labs?launched=${lab.id}`)}
          />
        </span>
      </div>
      <p className="mt-1 text-sm text-aws-muted">{p.summary}</p>
      <p className="mt-1 text-xs text-aws-muted">
        Lab: <span className="font-mono">{p.labDefinitionId}</span>
        {' · '}Graded by:{' '}
        <span className="font-mono">{p.gradingRules.join(', ')}</span>
      </p>
    </li>
  )

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Advanced', to: '/advanced' }, { label: 'Projects' }]}
        title="Advanced projects"
        description="Real-world industry examples and end-to-end deployments. Each project is a track with lessons, a guided lab, a challenge and an assessment — launch the lab, do the work, and let the validator grade the result against real state."
      />
      <AwsCard title="Industry projects — real-world examples">
        <ul className="divide-y divide-aws-border">{industry.map(renderProject)}</ul>
      </AwsCard>
      <AwsCard title="End-to-end deployment">
        <p className="mb-2 text-sm text-aws-muted">
          CI/CD pipeline, staging-to-production promotion, rollback, monitoring and alerting, and runbooks — with a
          pipeline stage, a promotion, a rollback path and alerting checks in each project.
        </p>
        <ul className="divide-y divide-aws-border">{endToEnd.map(renderProject)}</ul>
      </AwsCard>
      <AwsCard title="How grading works">
        <p className="text-sm text-aws-muted">
          Every project is graded by the validation engine against real lab state — VPCs, subnets, routes, security
          groups, running instances, load balancer health and recorded metrics. Only rules the engine implements
          are used, so a project can actually pass: pipeline, canary and alarm rules that the engine does not
          implement yet are documented in docs/advanced/GRADING.md and are not used as grading rules. The
          portfolio artefact for a project is generated only after its track is complete.
        </p>
      </AwsCard>
    </div>
  )
}
