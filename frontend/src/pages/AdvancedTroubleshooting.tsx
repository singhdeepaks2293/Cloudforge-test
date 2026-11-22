import { useNavigate } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsBadge } from '../components/AwsBadge.tsx'
import { LaunchLabButton } from '../components/LabLauncher.tsx'
import { ADVANCED_SCENARIOS } from './AdvancedData.ts'

/**
 * AdvancedTroubleshooting — the tough break-and-fix scenarios.
 *
 * These scenarios go beyond the existing set: multi-fault symptoms,
 * ambiguous logs, and production outages where every log points
 * somewhere else. Each scenario injects one fault kind the network
 * engine can evaluate (route-blackhole, latency, loss, dns) and is
 * graded by the validation engine against real lab state.
 *
 * Starting a scenario: launch a lab below, then start the scenario
 * on that lab with POST /api/v1/labs/{labId}/break-fix/start and
 * {"scenario_id": "<id>"}. There is no in-console start button yet —
 * the API is the honest path today, and the scenario list here shows
 * exactly what to start.
 */
export function AdvancedTroubleshooting() {
  const navigate = useNavigate()
  const toneFor = (kind: string): 'green' | 'amber' | 'red' | 'grey' | 'blue' =>
    kind === 'latency' ? 'amber' : kind === 'loss' ? 'red' : kind === 'dns' ? 'blue' : 'grey'

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Advanced', to: '/advanced' }, { label: 'Troubleshooting' }]}
        title="Tough troubleshooting"
        description="Six production outages beyond the existing break-and-fix set. Ambiguous logs, more than one thing wrong, and a fix the validator can actually grade — each scenario injects a real fault through the network engine."
      />
      <AwsCard title="How to start a scenario">
        <ol className="list-decimal space-y-1 pl-5 text-sm">
          <li>Launch a lab below (any 30-minute lab works in lite mode).</li>
          <li>
            Start the scenario on that lab: <span className="font-mono">POST /api/v1/labs/{'{labId}'}/break-fix/start</span>{' '}
            with <span className="font-mono">{'{"scenario_id": "<id>"}'}</span>.
          </li>
          <li>Read the logs and metrics, diagnose, apply the fix steps, then solve it from the lab — the validator grades every rule and awards XP only when all of them pass.</li>
        </ol>
        <div className="mt-3">
          <LaunchLabButton definitionId="ec2-basics" onLaunched={(lab) => navigate(`/labs?launched=${lab.id}`)} />
        </div>
      </AwsCard>
      <AwsCard title="Advanced scenarios">
        <ul className="divide-y divide-aws-border">
          {ADVANCED_SCENARIOS.map((s) => (
            <li key={s.id} className="py-3">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <span className="font-medium">{s.title}</span>
                <span className="flex shrink-0 items-center gap-2">
                  <AwsBadge tone={toneFor(s.kind)}>{s.kind}</AwsBadge>
                  <span className="text-xs text-aws-muted">
                    target <span className="font-mono">{s.target}</span> · {s.xpReward} XP
                  </span>
                </span>
              </div>
              <p className="mt-1 text-sm text-aws-muted">{s.symptom}</p>
              <p className="mt-1 text-xs text-aws-muted">
                Scenario id: <span className="font-mono">{s.id}</span>
                {' · '}Graded by: <span className="font-mono">{s.gradingRules.join(', ')}</span>
              </p>
            </li>
          ))}
        </ul>
      </AwsCard>
    </div>
  )
}
