import { useCreateLab, useLiteMode } from '../hooks/useLaunchLab.ts'
import type { Lab, LabDefinition } from '../types/index.ts'
import {
  LAB_DEFINITIONS,
  LITE_MAX_LAB_MINUTES,
  findLabDefinition,
  isDefinitionDisabledInLite,
} from '../types/index.ts'
import { AwsButton } from './AwsButton.tsx'
import { AwsCard } from './AwsCard.tsx'

/**
 * Shared "Launch lab" flow (Launch-lab fix, 2026-11-22).
 *
 * Before this fix the Labs page's "Launch lab" button was only a
 * `<Link to="/learn">` and no page ever called POST /api/v1/labs,
 * so clicking Launch did nothing (confirmed in the live API logs:
 * register + GET /labs + GET /courses, but no POST /labs ever).
 * These components call createLab() and surface the backend's
 * error instead of failing silently:
 * - 429 quota_exceeded — e.g. lite mode's 1-lab / 30-minute caps
 * - 400 unknown_definition — the definition id does not exist
 * - 401 — the session expired; the user must log in again
 *
 * Lite mode is read from GET /api/v1/health ("lite": true), the
 * same flag the backend reports — never guessed from the URL.
 * Definitions longer than LITE_MAX_LAB_MINUTES are disabled in
 * lite mode with the reason shown, because the backend would
 * refuse them with 429 anyway.
 *
 * The mutation/error helpers live in hooks/useLaunchLab.ts so this
 * file only exports components (oxlint only-export-components).
 */

/** One definition row in the picker: title, duration, Launch button. */
function DefinitionRow({
  def,
  lite,
  launching,
  onLaunch,
}: {
  def: LabDefinition
  lite: boolean
  launching: boolean
  onLaunch: (id: string) => void
}) {
  const disabledInLite = isDefinitionDisabledInLite(def, lite)
  return (
    <li className="flex items-baseline justify-between gap-4 py-2 text-sm">
      <span>
        <span className="font-medium">{def.title}</span>
        <span className="block text-aws-muted">{def.description}</span>
        <span className="block text-aws-muted">
          {def.duration_minutes} minutes
          {disabledInLite && (
            <span className="text-aws-error">
              {' — '}
              Not available in lite mode (lite allows {LITE_MAX_LAB_MINUTES}-minute labs only)
            </span>
          )}
        </span>
      </span>
      <AwsButton
        variant="primary"
        disabled={disabledInLite || launching}
        onClick={() => onLaunch(def.id)}
      >
        {launching ? 'Launching…' : 'Launch lab'}
      </AwsButton>
    </li>
  )
}

/** Full definition picker, used on the Labs page. */
export function LabDefinitionPicker({ onLaunched }: { onLaunched: (lab: Lab) => void }) {
  const lite = useLiteMode()
  const { launch, launching, error } = useCreateLab(onLaunched)
  return (
    <AwsCard title="Launch a lab">
      {lite && (
        <p className="mb-2 text-sm text-aws-muted">
          Lite mode: 1 lab at a time, {LITE_MAX_LAB_MINUTES}-minute labs only. Longer labs need full mode.
        </p>
      )}
      <ul className="divide-y divide-aws-border">
        {LAB_DEFINITIONS.map((def) => (
          <DefinitionRow key={def.id} def={def} lite={lite} launching={launching} onLaunch={launch} />
        ))}
      </ul>
      {error && (
        <p role="alert" className="mt-3 text-sm text-aws-error">
          {error}
        </p>
      )}
    </AwsCard>
  )
}

/**
 * Single-definition Launch button, used on Learn / Courses rows
 * where the course id is a lab definition id. Renders nothing when
 * the id is not a lab definition; the caller falls back to a link.
 */
export function LaunchLabButton({
  definitionId,
  onLaunched,
}: {
  definitionId: string
  onLaunched: (lab: Lab) => void
}) {
  const def = findLabDefinition(definitionId)
  const lite = useLiteMode()
  const { launch, launching, error } = useCreateLab(onLaunched)
  if (!def) return null
  const disabledInLite = isDefinitionDisabledInLite(def, lite)
  return (
    <span className="inline-flex flex-col items-start gap-1">
      <AwsButton
        variant="primary"
        disabled={disabledInLite || launching}
        title={
          disabledInLite
            ? `Not available in lite mode (lite allows ${LITE_MAX_LAB_MINUTES}-minute labs only)`
            : undefined
        }
        onClick={() => launch(def.id)}
      >
        {launching ? 'Launching…' : 'Launch lab'}
      </AwsButton>
      {disabledInLite && (
        <span className="text-xs text-aws-muted">
          {def.duration_minutes} min — lite allows {LITE_MAX_LAB_MINUTES} min only
        </span>
      )}
      {error && (
        <span role="alert" className="text-xs text-aws-error">
          {error}
        </span>
      )}
    </span>
  )
}
