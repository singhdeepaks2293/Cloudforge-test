import { Link } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { ADVANCED_PROJECTS, ADVANCED_SCENARIOS, INTERVIEW_FALLBACK } from './AdvancedData.ts'

/**
 * Advanced — the hub for the job-ready, industry and end-to-end
 * section. Every item here is a real working lab or scenario, not a
 * text-only page: projects launch labs, scenarios inject real faults,
 * and the job-ready material is graded against rubrics.
 */
export function Advanced() {
  const industry = ADVANCED_PROJECTS.filter((p) => p.kind === 'industry')
  const endToEnd = ADVANCED_PROJECTS.filter((p) => p.kind === 'end-to-end')
  const liteCount = ADVANCED_PROJECTS.filter((p) => p.liteLaunchable).length

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Advanced' }]}
        title="Advanced"
        description="Job-ready, industry and end-to-end work for engineers who want to operate in production: real projects, tough outages, full deployment pipelines — each one launchable, gradable, and proven against real state."
      />
      <AwsCard title="What is in the Advanced section">
        <ul className="list-disc space-y-1 pl-5 text-sm">
          <li>
            <strong>{industry.length} industry projects</strong> — multi-tier production, high availability and
            disaster recovery, migration, cost optimisation and incident response.
          </li>
          <li>
            <strong>{endToEnd.length} end-to-end deployment projects</strong> — CI/CD from staging to production,
            and canary rollouts with rollback and alerting.
          </li>
          <li>
            <strong>{ADVANCED_SCENARIOS.length} tough troubleshooting scenarios</strong> — multi-fault, ambiguous-log
            production outages beyond the existing break-and-fix set.
          </li>
          <li>
            <strong>{INTERVIEW_FALLBACK.length} interview walkthroughs</strong> with expected approaches and grading
            rubrics, plus a skills checklist mapped to real job descriptions.
          </li>
          <li>
            {liteCount} of {ADVANCED_PROJECTS.length} projects launch in lite mode (30-minute labs); the other two
            need the full mode because their labs run 60 and 120 minutes.
          </li>
        </ul>
      </AwsCard>
      <div className="grid gap-4 md:grid-cols-3">
        <AwsCard title="Industry projects">
          <p className="mb-3 text-sm text-aws-muted">
            End-to-end projects modelled on real production work: architecture, Terraform, deployment steps and
            automated validation.
          </p>
          <Link to="/advanced/projects" className="text-sm text-aws-link hover:underline">
            Browse the {industry.length} industry projects →
          </Link>
        </AwsCard>
        <AwsCard title="Tough troubleshooting">
          <p className="mb-3 text-sm text-aws-muted">
            Production outages with ambiguous logs and more than one thing wrong. Diagnose from logs and metrics,
            fix it, and let the validator grade the result.
          </p>
          <Link to="/advanced/troubleshooting" className="text-sm text-aws-link hover:underline">
            Browse the {ADVANCED_SCENARIOS.length} scenarios →
          </Link>
        </AwsCard>
        <AwsCard title="Job-ready">
          <p className="mb-3 text-sm text-aws-muted">
            Interview walkthroughs with rubrics, a skills checklist mapped to cloud, DevOps and architect roles,
            and a portfolio artefact generated from each project you complete.
          </p>
          <Link to="/advanced/job-ready" className="text-sm text-aws-link hover:underline">
            Open the job-ready material →
          </Link>
        </AwsCard>
      </div>
      <AwsCard title="End-to-end deployment">
        <p className="mb-3 text-sm text-aws-muted">
          CI/CD pipeline with staging-to-production promotion and rollback, and a canary rollout with health gates
          and alerting. Both are launchable, gradable projects — not walkthroughs.
        </p>
        <Link to="/advanced/projects" className="text-sm text-aws-link hover:underline">
          Browse the {endToEnd.length} end-to-end projects →
        </Link>
      </AwsCard>
    </div>
  )
}
