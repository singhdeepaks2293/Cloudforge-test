import { AwsCard } from './AwsCard.tsx'
import { AwsButton } from './AwsButton.tsx'

/**
 * Honest placeholder for a survey-recorded console section that has no
 * backend in CloudForge. The section is shown (the 2026-11-22 survey
 * records it in the AWS console) with its action disabled and the reason
 * stated, instead of pretending the capability exists or hiding the gap.
 */
export function UnsupportedSection({
  title,
  reason,
  actionLabel,
}: {
  title: string
  reason: string
  actionLabel: string
}) {
  return (
    <AwsCard title={title}>
      <p className="mb-3 text-sm text-aws-muted">{reason}</p>
      <AwsButton disabled title={reason}>
        {actionLabel}
      </AwsButton>
      <p className="mt-3 text-xs text-aws-muted">
        Disabled: no backend serves this in CloudForge. The tab is shown because the 2026-11-22 survey records it
        in the AWS console — the layout is the reference, the capability is honestly absent.
      </p>
    </AwsCard>
  )
}
