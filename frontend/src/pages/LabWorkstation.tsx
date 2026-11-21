/**
 * Lab workstation — /labs/:id/workstation (Milestone 6 frontend,
 * M9 Part 2 AWS-console-style restyle, M9 Part 3 live feeds).
 *
 * Multi-pane layout: instructions (real course/lab data), resource
 * explorer (live GET /api/v1/labs/{id}/resources), validation panel
 * (live POST /api/v1/labs/{id}/validate), hints, topology and
 * terminal. Panel sizes are adjustable via draggable dividers
 * (plain pointer events, no extra dependency).
 *
 * Honesty rules, verified against backend/internal/httpapi/httpapi.go:
 * - Validation calls POST /api/v1/labs/{id}/validate and lists the
 *   real check results; a failed call (e.g. engine down: 503) is
 *   shown as an error, never as passed checks.
 * - The terminal's live mode needs WS /api/v1/labs/{id}/terminal;
 *   it requests a short-lived token first (M9 Part 2 fix) and reports
 *   honestly when the backend is unavailable. An echo test mode is
 *   offered, labelled as a UI test only.
 * - The lab definition's validation rules are not exposed by the
 *   backend (GET /api/v1/labs/{id} returns no rules), so none are
 *   listed as if they were.
 */
import { useCallback, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import {
  fetchCourseDetail,
  fetchLab,
  fetchLabResources,
  fetchLabTopology,
  getToken,
  validateLab,
} from '../services/api.ts'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { StateBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { Terminal, type TerminalMode } from '../components/Terminal.tsx'
import { TopologyCanvas } from '../components/TopologyCanvas.tsx'

/** Draggable vertical divider between two flex panes. */
function Divider({ onDrag }: { onDrag: (dx: number) => void }) {
  const startX = useRef(0)
  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    startX.current = e.clientX
    const target = e.currentTarget
    target.setPointerCapture(e.pointerId)
    const onMove = (ev: PointerEvent) => {
      onDrag(ev.clientX - startX.current)
      startX.current = ev.clientX
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      onPointerDown={onPointerDown}
      className="w-1.5 shrink-0 cursor-col-resize rounded bg-aws-border hover:bg-aws-border-dark"
      title="Drag to resize panels"
    />
  )
}

export function LabWorkstation() {
  const { id = '' } = useParams()
  const authed = getToken() !== null
  const queryClient = useQueryClient()
  const [leftWidth, setLeftWidth] = useState(300)
  const [rightWidth, setRightWidth] = useState(320)
  const [terminalMode, setTerminalMode] = useState<TerminalMode>('live')
  const [terminalHeight, setTerminalHeight] = useState(360)
  const [tab, setTab] = useState<'topology' | 'terminal'>('topology')

  const lab = useQuery({ queryKey: ['lab', id], queryFn: () => fetchLab(id), retry: 1, enabled: authed && id !== '' })
  const definitionId = lab.data?.definition_id
  const course = useQuery({
    queryKey: ['course-detail', definitionId],
    queryFn: () => fetchCourseDetail(definitionId as string),
    retry: 1,
    enabled: authed && definitionId !== undefined && definitionId !== '',
  })
  const topo = useQuery({
    queryKey: ['lab-topology', id],
    queryFn: () => fetchLabTopology(id),
    retry: false,
    enabled: authed && id !== '',
  })
  // Live resource list (GET /api/v1/labs/{id}/resources). The lab
  // payload itself carries no resources field.
  const resourcesQuery = useQuery({
    queryKey: ['lab-resources', id],
    queryFn: () => fetchLabResources(id),
    retry: 1,
    enabled: authed && id !== '',
  })
  const validation = useMutation({
    mutationFn: () => validateLab(id),
  })
  const refresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['lab-topology', id] })
    void queryClient.invalidateQueries({ queryKey: ['lab-resources', id] })
    void queryClient.invalidateQueries({ queryKey: ['lab', id] })
  }, [queryClient, id])

  const clamp = useCallback((v: number, min: number, max: number) => Math.min(max, Math.max(min, v)), [])

  const resources = resourcesQuery.data ?? []

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[
          { label: 'CloudForge', to: '/' },
          { label: 'EC2', to: '/labs' },
          { label: 'Instances', to: '/labs' },
          { label: 'Workstation' },
        ]}
        title="Lab workstation"
        description="Instructions, topology, terminal, resources and validation for one lab session."
        actions={lab.data ? <StateBadge state={lab.data.status} /> : undefined}
      />

      {!authed && (
        <AwsCard title="Sign in first">
          <p className="text-sm text-aws-muted">
            The workstation needs a lab session. Please <Link to="/login">log in</Link> first.
          </p>
        </AwsCard>
      )}
      {authed && lab.isPending && <Loading label="Loading lab" />}
      {authed && lab.isError && <ErrorBox message={`Could not load lab: ${lab.error.message}`} />}

      {authed && lab.data && (
        <div className="flex items-stretch gap-0">
          {/* Instructions panel */}
          <section
            style={{ width: leftWidth }}
            className="flex shrink-0 flex-col gap-4 overflow-auto border border-aws-border bg-white p-4"
          >
            <div>
              <h2 className="text-base font-semibold">Instructions</h2>
              <p className="mt-1 text-sm font-medium">{lab.data.name}</p>
              {lab.data.definition_id && (
                <p className="text-xs text-aws-muted">Lab definition: {lab.data.definition_id}</p>
              )}
              {lab.data.error && <p className="mt-1 text-sm text-aws-error">{lab.data.error}</p>}
            </div>
            {course.isPending && definitionId && <Loading label="Loading course instructions" />}
            {course.data && (
              <div className="text-sm">
                <p className="font-medium">{course.data.title}</p>
                <p className="mt-1 text-aws-muted">{course.data.description}</p>
                {course.data.lesson_titles.length > 0 && (
                  <ol className="mt-2 list-decimal space-y-1 pl-5 text-aws-text">
                    {course.data.lesson_titles.map((t) => (
                      <li key={t}>{t}</li>
                    ))}
                  </ol>
                )}
              </div>
            )}
            {course.isError && definitionId && (
              <p className="text-sm text-aws-muted">
                No course instructions for definition <code className="font-mono">{definitionId}</code> — the
                backend has no course with that id ({course.error.message}). Follow the lab goal in the
                topology and terminal panes instead.
              </p>
            )}
            {!definitionId && (
              <p className="text-sm text-aws-muted">This lab has no definition id, so no course instructions apply.</p>
            )}
            <div>
              <h3 className="text-sm font-semibold">Hints</h3>
              {course.data && course.data.lesson_titles.length > 0 ? (
                <ul className="mt-1 list-disc space-y-1 pl-5 text-sm text-aws-muted">
                  {course.data.lesson_titles.map((t) => (
                    <li key={t}>Work through: {t}</li>
                  ))}
                </ul>
              ) : (
                <p className="mt-1 text-sm text-aws-muted">
                  Hints come from the course lessons for this lab&apos;s definition; none are available until a
                  matching course is published.
                </p>
              )}
            </div>
          </section>

          <Divider onDrag={(dx) => setLeftWidth((w) => clamp(w + dx, 200, 560))} />

          {/* Centre: topology + terminal, AWS-style tabs */}
          <section className="min-w-0 flex-1 space-y-4 px-4">
            <div className="border border-aws-border bg-white">
              <div className="flex gap-1 border-b border-aws-border" role="tablist" aria-label="Workstation views">
                {(
                  [
                    ['topology', 'Topology'],
                    ['terminal', 'Terminal'],
                  ] as const
                ).map(([t, label]) => (
                  <button
                    key={t}
                    type="button"
                    role="tab"
                    aria-selected={tab === t}
                    onClick={() => setTab(t)}
                    className={`border-b-2 px-4 py-2 text-sm font-medium ${
                      tab === t
                        ? 'border-aws-orange text-aws-orange-dark'
                        : 'border-transparent text-aws-muted hover:text-aws-text'
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <div className="p-4">
                {tab === 'topology' && (
                  <TopologyCanvas
                    lab={lab.data}
                    topology={topo.data ?? null}
                    topologyError={topo.isError ? topo.error.message : null}
                    onChanged={refresh}
                    height={380}
                  />
                )}
                {tab === 'terminal' && (
                  <>
                    <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
                      <AwsButton
                        variant={terminalMode === 'live' ? 'primary' : 'secondary'}
                        onClick={() => setTerminalMode('live')}
                      >
                        Live shell
                      </AwsButton>
                      <AwsButton
                        variant={terminalMode === 'echo' ? 'primary' : 'secondary'}
                        onClick={() => setTerminalMode('echo')}
                      >
                        Echo test (UI only)
                      </AwsButton>
                      <label className="ml-2 text-aws-muted">
                        Height{' '}
                        <input
                          type="range"
                          min={200}
                          max={600}
                          step={20}
                          value={terminalHeight}
                          onChange={(e) => setTerminalHeight(Number(e.target.value))}
                          className="align-middle"
                        />{' '}
                        {terminalHeight}px
                      </label>
                    </div>
                    <Terminal labId={id} mode={terminalMode} height={terminalHeight} />
                  </>
                )}
              </div>
            </div>
          </section>

          <Divider onDrag={(dx) => setRightWidth((w) => clamp(w - dx, 220, 560))} />

          {/* Right: resources + validation */}
          <section
            style={{ width: rightWidth }}
            className="flex shrink-0 flex-col gap-4 overflow-auto border border-aws-border bg-white p-4"
          >
            <div>
              <h2 className="text-base font-semibold">Resource explorer</h2>
              {resourcesQuery.isPending && <Loading label="Loading resources" />}
              {resourcesQuery.isError && (
                <p className="mt-1 text-sm text-aws-error">
                  Could not load resources: {resourcesQuery.error.message}
                </p>
              )}
              {resourcesQuery.isSuccess && resources.length === 0 && (
                <p className="mt-1 text-sm text-aws-muted">
                  No resources recorded for this lab yet — create one from the topology tab.
                </p>
              )}
              {resources.length > 0 && (
                <ul className="mt-2 divide-y divide-aws-border text-sm">
                  {resources.map((r) => (
                    <li key={r.id} className="py-2">
                      <span className="font-medium">{r.name}</span>{' '}
                      <span className="rounded-full bg-aws-bg px-2 py-0.5 font-mono text-xs">{r.kind}</span>
                      {r.state && <span className="ml-2 text-aws-muted">{r.state}</span>}
                      {r.private_ip && <p className="font-mono text-xs text-aws-muted">{r.private_ip}</p>}
                      {r.public_ip && <p className="font-mono text-xs text-aws-muted">{r.public_ip}</p>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <h2 className="text-base font-semibold">Validation</h2>
              <div className="mt-2">
                <AwsButton
                  variant="primary"
                  onClick={() => validation.mutate()}
                  disabled={validation.isPending}
                >
                  {validation.isPending ? 'Running validation…' : 'Run validation'}
                </AwsButton>
              </div>
              {validation.isError && (
                <div className="mt-2 border border-[#f5c6a8] bg-aws-warning-bg px-3 py-2 text-sm text-aws-warning">
                  Validation failed: {validation.error.message} — the lab state is unchanged; fix the
                  backend/engine issue and run it again.
                </div>
              )}
              {validation.data && (
                <div className="mt-2 space-y-2 text-sm">
                  <p
                    className={`font-semibold ${validation.data.passed ? 'text-emerald-700' : 'text-aws-error'}`}
                    role="status"
                  >
                    {validation.data.passed ? 'Validation passed' : 'Validation failed'} —{' '}
                    {validation.data.checks.filter((c) => c.passed).length}/{validation.data.checks.length}{' '}
                    checks passed.
                  </p>
                  <ul className="space-y-1">
                    {validation.data.checks.map((c) => (
                      <li key={c.rule_id} className="flex items-start gap-2">
                        <span aria-hidden>{c.passed ? '✓' : '✗'}</span>
                        <span>
                          <code className="font-mono text-xs">{c.rule_id}</code>
                          {c.message && <span className="text-aws-muted"> — {c.message}</span>}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {!validation.data && !validation.isError && !validation.isPending && (
                <p className="mt-2 text-sm text-aws-muted">
                  No checks have run yet. Validation runs the lab definition&apos;s rules against the
                  lab&apos;s real resources, engine topology and host-agent VMs.
                </p>
              )}
              <p className="mt-2 text-sm text-aws-muted">
                Lab state: <span className="font-medium text-aws-text">{lab.data.status}</span>.
              </p>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
