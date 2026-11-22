/** AWS-console-style status pill: green running, amber pending, red stopped/failed. */
export type BadgeTone = 'green' | 'amber' | 'red' | 'grey' | 'blue'

const TONE_CLS: Record<BadgeTone, string> = {
  green: 'bg-aws-success-bg text-aws-success',
  amber: 'bg-aws-warning-bg text-aws-warning',
  red: 'bg-aws-error-bg text-aws-error',
  grey: 'bg-[#eaeded] text-aws-muted',
  blue: 'bg-[#e9f5fe] text-aws-link',
}

export function AwsBadge({ tone = 'grey', children }: { tone?: BadgeTone; children: React.ReactNode }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${TONE_CLS[tone]}`}>
      {children}
    </span>
  )
}

/** Map a lab / resource state string to a pill tone + label, honestly. */
function stateTone(state: string): BadgeTone {
  const s = state.toUpperCase()
  if (['RUNNING', 'READY', 'COMPLETED', 'AVAILABLE', 'HEALTHY', 'ACTIVE'].includes(s)) return 'green'
  if (['CREATED', 'PROVISIONING', 'VALIDATING', 'STOPPING', 'DESTROYING', 'PENDING'].includes(s)) return 'amber'
  if (['FAILED', 'STOPPED', 'EXPIRED', 'DESTROYED', 'ERROR', 'UNHEALTHY'].includes(s)) return 'red'
  return 'grey'
}

export function StateBadge({ state }: { state: string }) {
  return <AwsBadge tone={stateTone(state)}>{state.toLowerCase()}</AwsBadge>
}

/**
 * Fidelity badge (Phase 1, docs/SERVICE_FIDELITY.md §4). Every service
 * and operation shows its fidelity; UNSUPPORTED operations are rendered
 * disabled next to it, never as working features.
 */
const FIDELITY_TONE: Record<string, BadgeTone> = {
  REAL: 'green',
  API_EMULATED: 'blue',
  SIMULATED: 'amber',
  PARTIAL: 'blue',
  UNSUPPORTED: 'red',
}

const FIDELITY_LABEL: Record<string, string> = {
  REAL: 'REAL · real local infrastructure',
  API_EMULATED: 'API_EMULATED · local API-compatible backend',
  SIMULATED: 'SIMULATED · educational implementation',
  PARTIAL: 'PARTIAL · only some operations exist',
  UNSUPPORTED: 'UNSUPPORTED · not built',
}

export function FidelityBadge({ fidelity, footnote }: { fidelity: string; footnote?: string }) {
  const tone = FIDELITY_TONE[fidelity] ?? 'grey'
  return (
    <span className="inline-flex items-center gap-2">
      <AwsBadge tone={tone}>{fidelity}</AwsBadge>
      <span className="text-xs text-aws-muted">{footnote ?? FIDELITY_LABEL[fidelity] ?? ''}</span>
    </span>
  )
}
