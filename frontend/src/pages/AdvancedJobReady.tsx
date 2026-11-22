import { useCallback, useEffect, useMemo, useState } from 'react'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { buildUrl, getToken, request } from '../services/api.ts'
import { ADVANCED_PROJECTS, ARCHETYPE_FALLBACK, INTERVIEW_FALLBACK } from './AdvancedData.ts'
import type { InterviewFallback } from './AdvancedData.ts'

/** The API returns rubric keys in snake_case; normalise to the fallback shape. */
type ApiInterview = Record<string, unknown> & {
  id?: unknown
  title?: unknown
  role_focus?: unknown
  problem?: unknown
  expected_approach?: unknown
  rubric?: unknown
  pitfalls?: unknown
  related_projects?: unknown
}
const asStrings = (v: unknown): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
const normaliseInterview = (v: ApiInterview): InterviewFallback | null => {
  if (typeof v.id !== 'string' || typeof v.title !== 'string' || typeof v.problem !== 'string') return null
  const rubric = Array.isArray(v.rubric)
    ? v.rubric
        .map((r) => {
          const rec = r as Record<string, unknown>
          if (typeof rec.criterion !== 'string') return null
          const good =
            typeof rec.good_looks_like === 'string'
              ? rec.good_looks_like
              : typeof rec.goodLooksLike === 'string'
                ? rec.goodLooksLike
                : ''
          return { criterion: rec.criterion, goodLooksLike: good }
        })
        .filter((r): r is { criterion: string; goodLooksLike: string } => r !== null)
    : []
  return {
    id: v.id,
    title: v.title,
    roleFocus: typeof v.role_focus === 'string' ? v.role_focus : '',
    problem: v.problem,
    expectedApproach: asStrings(v.expected_approach),
    rubric,
    pitfalls: asStrings(v.pitfalls),
    relatedProjects: asStrings(v.related_projects),
  }
}

const CHECKLIST_KEY = 'cloudforge-advanced-checklist'

/**
 * AdvancedJobReady — interview walkthroughs, skills checklist and
 * portfolio artefacts.
 *
 * Interviews and archetypes are fetched from /api/v1/advanced when
 * the routes are wired, and fall back to the bundled catalogue when
 * they are not (the fallback is labelled on the page). Checklist
 * state is saved to the API when it answers and to local storage
 * otherwise. Portfolio artefacts are generated server-side only for
 * projects the user has completed; a 409 means "not completed yet"
 * and a 404 means the route is not wired yet — neither is faked.
 */
export function AdvancedJobReady() {
  const [interviews, setInterviews] = useState<InterviewFallback[]>(INTERVIEW_FALLBACK)
  const [interviewsSource, setInterviewsSource] = useState<'api' | 'bundled'>('bundled')
  const [openId, setOpenId] = useState<string | null>(null)
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [checklistSynced, setChecklistSynced] = useState(false)
  const [portfolioError, setPortfolioError] = useState<string | null>(null)
  const [portfolioBusy, setPortfolioBusy] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const data = await request<{ interviews: ApiInterview[] }>('/api/v1/advanced/interviews')
        if (cancelled) return
        const list = (data.interviews ?? []).map(normaliseInterview).filter((x): x is InterviewFallback => x !== null)
        if (list.length > 0) {
          setInterviews(list)
          setInterviewsSource('api')
        }
      } catch (err) {
        // Routes not wired yet: keep the bundled catalogue.
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err))
      }
      try {
        const state = await request<{ checked: Record<string, boolean> }>('/api/v1/advanced/checklist-state')
        if (!cancelled) {
          setChecked(state.checked ?? {})
          setChecklistSynced(true)
        }
      } catch {
        try {
          const raw = localStorage.getItem(CHECKLIST_KEY)
          if (!cancelled && raw) setChecked(JSON.parse(raw) as Record<string, boolean>)
        } catch {
          /* ignore corrupt local state */
        }
      }
      if (!cancelled) setLoading(false)
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const toggleSkill = useCallback(
    (skillId: string) => {
      setChecked((prev) => {
        const next = { ...prev, [skillId]: !prev[skillId] }
        if (!next[skillId]) delete next[skillId]
        localStorage.setItem(CHECKLIST_KEY, JSON.stringify(next))
        if (checklistSynced) {
          void request('/api/v1/advanced/checklist-state', { method: 'PUT', body: { checked: next } }).catch(() => {
            setChecklistSynced(false)
          })
        }
        return next
      })
    },
    [checklistSynced],
  )

  const downloadPortfolio = useCallback(async (projectId: string) => {
    setPortfolioError(null)
    setPortfolioBusy(projectId)
    try {
      const token = getToken()
      const res = await fetch(buildUrl(`/api/v1/advanced/portfolio/${projectId}`), {
        method: 'POST',
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!res.ok) {
        const text = await res.text()
        let message = `Request failed (${res.status})`
        try {
          const body = JSON.parse(text) as { error?: { message?: string } }
          if (body.error?.message) message = body.error.message
        } catch {
          /* keep the status message */
        }
        if (res.status === 404) message += ' — the portfolio route is not wired yet (see docs/advanced/ROUTES.md).'
        setPortfolioError(message)
        return
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `cloudforge-portfolio-${projectId}.md`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      setPortfolioError(err instanceof Error ? err.message : String(err))
    } finally {
      setPortfolioBusy(null)
    }
  }, [])

  const checkedCount = useMemo(() => Object.keys(checked).length, [checked])

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Advanced', to: '/advanced' }, { label: 'Job-ready' }]}
        title="Job-ready"
        description="Interview walkthroughs with grading rubrics, a skills checklist mapped to real job descriptions, and a portfolio artefact generated from each Advanced project you complete. No fake certificates, no claim of AWS certification."
      />
      {loading && <Loading label="Loading job-ready material" />}
      {loadError && interviewsSource === 'bundled' && (
        <ErrorBox message={`Advanced API is not reachable (${loadError}). Showing the bundled catalogue instead.`} />
      )}

      <AwsCard title={`Interview walkthroughs (${interviews.length})`}>
        <p className="mb-3 text-sm text-aws-muted">
          Each walkthrough gives a problem, an expected approach and a rubric — what good looks like. There is no
          single correct answer: the rubric grades reasoning, tradeoffs named, and the verification step.
          {interviewsSource === 'bundled' && ' (Bundled catalogue — the API copy loads once the routes are wired.)'}
        </p>
        <ul className="divide-y divide-aws-border">
          {interviews.map((w) => {
            const open = openId === w.id
            return (
              <li key={w.id} className="py-3">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span>
                    <span className="font-medium">{w.title}</span>
                    <span className="ml-2 text-xs text-aws-muted">{w.roleFocus}</span>
                  </span>
                  <AwsButton variant="link" onClick={() => setOpenId(open ? null : w.id)}>
                    {open ? 'Hide walkthrough' : 'Show walkthrough'}
                  </AwsButton>
                </div>
                <p className="mt-1 text-sm text-aws-muted">{w.problem}</p>
                {open && (
                  <div className="mt-3 space-y-3 text-sm">
                    <div>
                      <h3 className="font-medium">Expected approach</h3>
                      <ol className="list-decimal space-y-1 pl-5 text-aws-muted">
                        {w.expectedApproach.map((s) => (
                          <li key={s}>{s}</li>
                        ))}
                      </ol>
                    </div>
                    <div>
                      <h3 className="font-medium">Grading rubric</h3>
                      <ul className="space-y-1 pl-1 text-aws-muted">
                        {w.rubric.map((r) => (
                          <li key={r.criterion}>
                            <strong className="text-aws-text">{r.criterion}:</strong> {r.goodLooksLike}
                          </li>
                        ))}
                      </ul>
                    </div>
                    <div>
                      <h3 className="font-medium">Common pitfalls</h3>
                      <ul className="list-disc space-y-1 pl-5 text-aws-muted">
                        {w.pitfalls.map((p) => (
                          <li key={p}>{p}</li>
                        ))}
                      </ul>
                    </div>
                    {w.relatedProjects.length > 0 && (
                      <p className="text-xs text-aws-muted">
                        Practise it in: <span className="font-mono">{w.relatedProjects.join(', ')}</span>
                      </p>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </AwsCard>

      <AwsCard
        title={`Skills checklist — mapped to real job descriptions (${checkedCount} checked)`}
        actions={
          <span className="text-xs text-aws-muted">
            {checklistSynced ? 'Synced to your account' : 'Saved locally on this browser'}
          </span>
        }
      >
        <p className="mb-3 text-sm text-aws-muted">
          Composite sketches of common postings, not copies of any single listing. Each skill maps to the Advanced
          projects that teach it — check a skill off when you can demonstrate it, not when you have read about it.
        </p>
        {ARCHETYPE_FALLBACK.map((a) => (
          <div key={a.id} className="mb-4">
            <h3 className="font-medium">{a.title}</h3>
            <p className="mb-2 text-sm text-aws-muted">{a.summary}</p>
            <ul className="space-y-1">
              {a.skills.map((s, i) => {
                const skillId = `${a.id}:${i}`
                const isChecked = checked[skillId] === true
                return (
                  <li key={skillId} className="flex items-start gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={isChecked}
                      onChange={() => toggleSkill(skillId)}
                      aria-label={s.name}
                      className="mt-0.5"
                    />
                    <span className={isChecked ? 'text-aws-muted line-through' : ''}>
                      {s.name}
                      {s.required ? '' : ' (nice to have)'}
                      <span className="block text-xs text-aws-muted">
                        Taught by: <span className="font-mono">{s.advancedProjects.join(', ')}</span>
                      </span>
                    </span>
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </AwsCard>

      <AwsCard title="Portfolio artefacts">
        <p className="mb-3 text-sm text-aws-muted">
          Completing an Advanced project generates a portfolio artefact: an architecture summary plus deployment
          evidence recorded by CloudForge. It is a record of work you did — not a certificate, and it makes no claim
          of AWS certification. Artefacts are generated only for projects you have completed.
        </p>
        {portfolioError && <ErrorBox message={portfolioError} />}
        <ul className="divide-y divide-aws-border">
          {ADVANCED_PROJECTS.map((p) => (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-2 py-2 text-sm">
              <span>
                <span className="font-medium">{p.title}</span>
                <span className="ml-2 text-xs text-aws-muted">{p.xpReward} XP</span>
              </span>
              <AwsButton
                variant="secondary"
                disabled={portfolioBusy === p.id}
                onClick={() => void downloadPortfolio(p.id)}
              >
                {portfolioBusy === p.id ? 'Generating…' : 'Download artefact'}
              </AwsButton>
            </li>
          ))}
        </ul>
      </AwsCard>
    </div>
  )
}
