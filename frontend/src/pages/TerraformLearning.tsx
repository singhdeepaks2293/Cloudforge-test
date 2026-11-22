import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? (r.id as string) : typeof r.name === 'string' ? (r.name as string) : `${key}-${i}` }))
}
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')
const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)
const boolOf = (r: Rec, k: string): boolean => r[k] === true

/** Minimal HCL syntax highlighting (no external dependency): comments,
 * strings, keywords, numbers. Rendered as spans, never as raw HTML. */
const HCL_KEYWORDS = new Set([
  'resource', 'variable', 'output', 'module', 'provider', 'terraform', 'data', 'locals', 'moved', 'import', 'dynamic', 'for_each', 'count', 'true', 'false', 'null',
])
function highlightHcl(code: string): React.ReactNode[] {
  return code.split('\n').map((line, li) => {
    const nodes: React.ReactNode[] = []
    let rest = line
    let key = 0
    const push = (text: string, style?: React.CSSProperties) => {
      if (text) nodes.push(<span key={`${li}-${key++}`} style={style}>{text}</span>)
    }
    // Comment: # or // to end of line (outside strings — good enough for the editor preview).
    const commentAt = (() => {
      const hash = rest.indexOf('#')
      const slash = rest.indexOf('//')
      if (hash === -1) return slash
      if (slash === -1) return hash
      return Math.min(hash, slash)
    })()
    let codePart = rest
    let commentPart = ''
    if (commentAt !== -1) {
      codePart = rest.slice(0, commentAt)
      commentPart = rest.slice(commentAt)
    }
    // Tokenise the code part: strings, then words/numbers.
    const re = /("(?:[^"\\]|\\.)*")|([A-Za-z_][A-Za-z0-9_-]*)|(\d+(?:\.\d+)?)/g
    let last = 0
    let m: RegExpExecArray | null
    while ((m = re.exec(codePart)) !== null) {
      if (m.index > last) push(codePart.slice(last, m.index))
      if (m[1] !== undefined) push(m[1], { color: '#7a3b00' })
      else if (m[2] !== undefined) push(m[2], HCL_KEYWORDS.has(m[2]) ? { color: '#0073bb', fontWeight: 600 } : undefined)
      else if (m[3] !== undefined) push(m[3], { color: '#7a3b00' })
      last = m.index + m[0].length
    }
    if (last < codePart.length) push(codePart.slice(last))
    if (commentPart) push(commentPart, { color: '#687078', fontStyle: 'italic' })
    return (
      <div key={li}>
        {nodes.length > 0 ? nodes : '\u00a0'}
      </div>
    )
  })
}

/**
 * Terraform Learning (Phase 5, master prompt §9) — a first-class
 * main-navigation section: dashboard, course structure (Levels 1–4),
 * code editor, execution workflow, state, provider docs.
 *
 * Fidelity is shown, not assumed: local validate/format/plan-preview
 * are labelled "local"; container execution is the M7 runner and is
 * honestly unavailable without Docker (503 terraform_unavailable);
 * apply/destroy need an approved plan + explicit confirmation.
 */
export function TerraformLearning() {
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<'dashboard' | 'courses' | 'projects' | 'provider'>('dashboard')
  const [notice, setNotice] = useState('')
  const [lessonId, setLessonId] = useState('')
  const [lessonLevel, setLessonLevel] = useState(1)
  const [answer, setAnswer] = useState<number | null>(null)
  const [projectId, setProjectId] = useState('')
  const [newName, setNewName] = useState('')
  const [newTemplate, setNewTemplate] = useState('')
  const [filePath, setFilePath] = useState('main.tf')
  const [editor, setEditor] = useState('')
  const [editorDirty, setEditorDirty] = useState(false)
  const [showPreview, setShowPreview] = useState(false)
  const [labId, setLabId] = useState('')
  const [operation, setOperation] = useState('plan')
  const [planId, setPlanId] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [restoreVersion, setRestoreVersion] = useState('')

  const dashboard = useQuery({ queryKey: ['tflearn-dashboard'], queryFn: () => request<Rec>('/api/v1/terraform-learning/dashboard'), retry: 1 })
  const courses = useQuery({ queryKey: ['tflearn-courses'], queryFn: () => request<Rec>('/api/v1/terraform-learning/courses'), retry: 1 })
  const provider = useQuery({ queryKey: ['tflearn-provider'], queryFn: () => request<Rec>('/api/v1/terraform-learning/provider/resources'), retry: 1, enabled: tab === 'provider' })
  const lesson = useQuery({
    queryKey: ['tflearn-lesson', lessonLevel, lessonId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/courses/${lessonLevel}/${lessonId}`),
    enabled: lessonId !== '',
    retry: 1,
  })
  const project = useQuery({
    queryKey: ['tflearn-project', projectId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}`),
    enabled: projectId !== '',
    retry: 1,
  })
  const executions = useQuery({
    queryKey: ['tflearn-executions', projectId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/executions`),
    enabled: projectId !== '',
    retry: 1,
    refetchInterval: projectId !== '' ? 5000 : false,
  })
  const state = useQuery({
    queryKey: ['tflearn-state', projectId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state`),
    enabled: projectId !== '',
    retry: 1,
  })
  const stateHistory = useQuery({
    queryKey: ['tflearn-state-history', projectId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/history`),
    enabled: projectId !== '',
    retry: 1,
  })
  const drift = useQuery({
    queryKey: ['tflearn-drift', projectId],
    queryFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/drift`),
    enabled: projectId !== '',
    retry: 1,
  })

  const refreshAll = () => {
    void queryClient.invalidateQueries({ queryKey: ['tflearn-dashboard'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-courses'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-project'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-executions'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-state'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-state-history'] })
    void queryClient.invalidateQueries({ queryKey: ['tflearn-drift'] })
  }

  const completeLesson = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/courses/${lessonLevel}/${lessonId}/complete`, { method: 'POST', body: { answer_index: answer } }),
    onSuccess: (data) => {
      setNotice(str(data as Rec, 'message') || 'Answer recorded.')
      setAnswer(null)
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Knowledge check failed: ${e.message}`),
  })
  const createProject = useMutation({
    mutationFn: () => request<Rec>('/api/v1/terraform-learning/projects', { method: 'POST', body: { name: newName.trim(), template: newTemplate || undefined } }),
    onSuccess: (data) => {
      const p = (data as Rec)['project'] as Rec | undefined
      setNotice('Project created.')
      setNewName('')
      setNewTemplate('')
      if (p) setProjectId(str(p, 'id'))
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Create project failed: ${e.message}`),
  })
  const saveFile = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/files`, { method: 'POST', body: { path: filePath, content: editor } }),
    onSuccess: () => {
      setNotice(`Saved ${filePath}.`)
      setEditorDirty(false)
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Save failed: ${e.message}`),
  })
  const validateProject = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/validate`, { method: 'POST', body: {} }),
    onSuccess: (data) => {
      const d = data as Rec
      setNotice(d['valid'] === true ? 'Local validation passed — no HCL errors.' : `Local validation found errors (see below). Valid: ${String(d['valid'])}`)
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Validate failed: ${e.message}`),
  })
  const formatProject = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/format`, { method: 'POST', body: {} }),
    onSuccess: (data) => {
      const files = listOf(data as Rec, 'formatted_files')
      setNotice(files.length > 0 ? `Formatted: ${files.map((f) => str(f, 'id')).join(', ')}` : 'Already formatted — nothing changed.')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Format failed: ${e.message}`),
  })
  const planPreview = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/plan-preview`, { method: 'POST', body: {} }),
    onSuccess: (data) => {
      const d = data as Rec
      const counts = (d['counts'] as Record<string, number>) ?? {}
      setNotice(`Plan preview: ${counts['create'] ?? 0} to add, ${counts['update'] ?? 0} to change, ${counts['delete'] ?? 0} to destroy (local static diff).`)
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Plan preview failed: ${e.message}`),
  })
  const execute = useMutation({
    mutationFn: () =>
      request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/executions`, {
        method: 'POST',
        body: { operation, plan_id: planId || undefined, confirm },
      }),
    onSuccess: (data) => {
      setNotice(`Execution queued: ${str(data as Rec, 'operation')} (run ${str(data as Rec, 'run_id')}).`)
      setConfirm(false)
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Execution failed: ${e.message}`),
  })
  const approvePreview = useMutation({
    mutationFn: (pid: string) => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/plans/${pid}/approve`, { method: 'POST', body: {} }),
    onSuccess: () => {
      setNotice('Plan preview approved (learning record — not a container apply gate).')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Approve failed: ${e.message}`),
  })
  const lockState = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/lock`, { method: 'POST', body: {} }),
    onSuccess: () => {
      setNotice('State locked.')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Lock failed: ${e.message}`),
  })
  const unlockState = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/unlock`, { method: 'POST', body: {} }),
    onSuccess: () => {
      setNotice('State unlocked.')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Unlock failed: ${e.message}`),
  })
  const backupState = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/backup`, { method: 'POST', body: {} }),
    onSuccess: () => {
      setNotice('State backed up as a new version.')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Backup failed: ${e.message}`),
  })
  const restoreState = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}/state/restore`, { method: 'POST', body: { version: Number(restoreVersion) } }),
    onSuccess: () => {
      setNotice('State restored as a new version (history not rewritten).')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Restore failed: ${e.message}`),
  })
  const linkLab = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/terraform-learning/projects/${projectId}`, { method: 'PUT', body: { lab_id: labId } }),
    onSuccess: () => {
      setNotice('Lab linked — container execution is now available for this project.')
      refreshAll()
    },
    onError: (e: Error) => setNotice(`Link lab failed: ${e.message}`),
  })

  const progress = (dashboard.data?.['progress'] as Rec) ?? {}
  const projects = listOf(dashboard.data, 'projects')
  const levels = listOf(courses.data, 'levels')
  const level4 = listOf(courses.data, 'projects')
  const providerResources = listOf(provider.data, 'resources')
  const projectFiles = useMemo(() => {
    const p = project.data?.['project'] as Rec | undefined
    return p ? listOf(p, 'files') : []
  }, [project.data])
  const execRows = listOf(executions.data, 'executions')
  const stateView = (state.data?.['state'] as Rec) ?? {}
  const stateVersions = listOf(stateHistory.data, 'versions')
  const driftRows = listOf(drift.data, 'drift')
  const achievements = listOf(dashboard.data, 'achievements')
  const recent = listOf(dashboard.data, 'recent_executions')
  const lessonData = (lesson.data?.['lesson'] as Rec) ?? {}
  const latestPreview = execRows.find((e) => str(e, 'operation') === 'plan-preview')

  const openFile = (path: string) => {
    const f = projectFiles.find((x) => str(x, 'path') === path)
    setFilePath(path)
    setEditor(f ? str(f, 'content') : '')
    setEditorDirty(false)
    setShowPreview(false)
  }

  const tabs: { id: typeof tab; label: string }[] = [
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'courses', label: 'Courses' },
    { id: 'projects', label: 'Projects & Editor' },
    { id: 'provider', label: 'Provider' },
  ]

  return (
    <div>
      <PageHeader
        title="Terraform Learning"
        breadcrumb={[{ label: 'Home', to: '/' }, { label: 'Terraform Learning' }]}
        description="Learn Terraform by doing: courses, a real editor, plan review, and state — with execution through the container runner. Nothing here reaches real AWS."
      />
      {notice !== '' && <div style={{ marginBottom: 12 }}><ErrorBox message={notice} /></div>}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
        {tabs.map((t) => (
          <AwsButton key={t.id} variant={tab === t.id ? 'primary' : 'secondary'} onClick={() => setTab(t.id)}>{t.label}</AwsButton>
        ))}
      </div>

      {tab === 'dashboard' && (
        <div>
          {dashboard.isPending ? <Loading label="Loading" /> : dashboard.isError ? <ErrorBox message="Could not load the Terraform Learning dashboard." /> : (
            <div>
              <AwsCard title="Learning progress">
                <p>Completed lessons: <strong>{num(progress, 'completed_lessons')} / {num(progress, 'total_lessons')}</strong> ({num(progress, 'percent')}%) — pending exercises: <strong>{num(progress, 'pending_exercises')}</strong></p>
                <p>Current course: Level {num(progress, 'current_level')}{str(progress, 'current_lesson_id') !== '' ? ` — ${str(progress, 'current_lesson_id')}` : ''} — skill level: <strong>{str(progress, 'skill_level')}</strong></p>
                <p>Terraform projects: <strong>{num(dashboard.data ?? {}, 'project_count')}</strong> — successful plans: <strong>{num(dashboard.data ?? {}, 'successful_plans')}</strong> — successful applies: <strong>{num(dashboard.data ?? {}, 'successful_applies')}</strong></p>
                <p>Container execution available: <strong>{(dashboard.data?.['terraform_available'] as boolean) ? 'yes (Docker reachable)' : 'no — execution honestly returns 503 terraform_unavailable here'}</strong>{(dashboard.data?.['mode_lite'] as boolean) ? ' — lite mode: Terraform execution needs Docker, which lite mode does not provide.' : ''}</p>
              </AwsCard>
              <AwsCard title={`Achievements (${achievements.filter((a) => boolOf(a, 'earned')).length} / ${achievements.length})`}>
                <ul>{achievements.map((a) => <li key={str(a, 'id')}>{boolOf(a, 'earned') ? '✓' : '○'} <strong>{str(a, 'title')}</strong> — {str(a, 'description')}</li>)}</ul>
              </AwsCard>
              <AwsCard title="Recent executions">
                {recent.length === 0 ? <p>No executions yet. Create a project and run a plan preview.</p> : (
                  <AwsTable<Rec>
                    columns={[
                      { header: 'Operation', render: (r) => str(r, 'operation') },
                      { header: 'Status', render: (r) => str(r, 'status') },
                      { header: 'Source', render: (r) => str(r, 'source') },
                      { header: 'Error', render: (r) => str(r, 'error') },
                    ]}
                    rows={recent}
                    emptyMessage="No executions yet."
                  />
                )}
              </AwsCard>
            </div>
          )}
        </div>
      )}

      {tab === 'courses' && (
        <div>
          {courses.isPending ? <Loading label="Loading" /> : courses.isError ? <ErrorBox message="Could not load courses." /> : (
            <div>
              {levels.map((lvl) => (
                <AwsCard key={str(lvl, 'level')} title={`Level ${num(lvl, 'level')} — ${str(lvl, 'title')} (${num(lvl, 'completed')}/${num(lvl, 'total')} complete)`}>
                  <p>{str(lvl, 'description')}</p>
                  <AwsTable<Rec>
                    columns={[
                      { header: 'Lesson', render: (r) => str(r, 'title') },
                      { header: 'Summary', render: (r) => str(r, 'summary') },
                      { header: 'Done', render: (r) => (boolOf(r, 'completed') ? '✓' : '—') },
                      {
                        header: 'Action',
                        render: (r) => <AwsButton variant="secondary" onClick={() => { setLessonLevel(num(lvl, 'level')); setLessonId(str(r, 'id')); setAnswer(null) }}>Open</AwsButton>,
                      },
                    ]}
                    rows={listOf(lvl, 'lessons')}
                    emptyMessage="No lessons in this level."
                  />
                </AwsCard>
              ))}
              <AwsCard title="Level 4 — AWS-style Infrastructure Projects">
                <AwsTable<Rec>
                  columns={[
                    { header: 'Project', render: (r) => str(r, 'title') },
                    { header: 'Description', render: (r) => str(r, 'description') },
                    { header: 'XP', render: (r) => String(num(r, 'xp')) },
                    {
                      header: 'Action',
                      render: (r) => <AwsButton variant="secondary" onClick={() => { setNewName(str(r, 'title').toLowerCase().replace(/[^a-z0-9]+/g, '-')); setNewTemplate(str(r, 'id')); setTab('projects') }}>Use template</AwsButton>,
                    },
                  ]}
                  rows={level4}
                  emptyMessage="No projects."
                />
              </AwsCard>
              {lessonId !== '' && (
                <AwsCard title={str(lessonData, 'title')}>
                  {lesson.isPending ? <Loading label="Loading" /> : lesson.isError ? <ErrorBox message="Could not load the lesson." /> : (
                    <div>
                      <p><strong>Summary:</strong> {str(lessonData, 'summary')}</p>
                      <p>{str(lessonData, 'explanation')}</p>
                      <p><strong>Real-world use case:</strong> {str(lessonData, 'use_case')}</p>
                      <p><strong>Code example:</strong></p>
                      <pre style={{ background: '#f7f7f7', padding: 12, overflowX: 'auto', fontSize: 13 }}>{str(lessonData, 'code_example')}</pre>
                      <p><strong>Guided exercise:</strong> {str(lessonData, 'exercise')}</p>
                      <p><strong>Expected output:</strong> {str(lessonData, 'expected_output')}</p>
                      <p><strong>Common mistakes:</strong></p>
                      <ul>{((lessonData['common_mistakes'] as string[]) ?? []).map((m, i) => <li key={i}>{m}</li>)}</ul>
                      {str(lessonData, 'curriculum_lesson') !== '' && <p><em>Reuses the curriculum Terraform track lesson “{str(lessonData, 'curriculum_lesson')}” — content lives in curriculum/terraform/terraform.yaml, not duplicated here.</em></p>}
                      <p><strong>Knowledge check:</strong> {str((lessonData['knowledge_check'] as Rec) ?? {}, 'question')}</p>
                      {((lessonData['knowledge_check'] as Rec)?.['options'] as string[] ?? []).map((opt, i) => (
                        <label key={i} style={{ display: 'block', margin: '4px 0' }}>
                          <input type="radio" name="tflearn-answer" checked={answer === i} onChange={() => setAnswer(i)} /> {opt}
                        </label>
                      ))}
                      <div style={{ marginTop: 8 }}>
                        <AwsButton disabled={answer === null || completeLesson.isPending} onClick={() => completeLesson.mutate()}>
                          {boolOf(lessonData, 'completed') ? 'Re-submit knowledge check' : 'Submit answer & complete lesson'}
                        </AwsButton>
                        {boolOf(lessonData, 'completed') && <span style={{ marginLeft: 8 }}>✓ Completed</span>}
                      </div>
                    </div>
                  )}
                </AwsCard>
              )}
            </div>
          )}
        </div>
      )}

      {tab === 'projects' && (
        <div>
          <AwsCard title="Terraform projects">
            <form
              onSubmit={(e) => {
                e.preventDefault()
                createProject.mutate()
              }}
            >
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="Project name" style={{ padding: 6 }} />
                <select value={newTemplate} onChange={(e) => setNewTemplate(e.target.value)} style={{ padding: 6 }}>
                  <option value="">Blank project</option>
                  {level4.map((p) => <option key={str(p, 'id')} value={str(p, 'id')}>{str(p, 'title')}</option>)}
                </select>
                <AwsButton type="submit" disabled={newName.trim() === '' || createProject.isPending}>Create project</AwsButton>
              </div>
            </form>
            <div style={{ marginTop: 12 }}>
              <AwsTable<Rec>
                columns={[
                  { header: 'Name', render: (r) => str(r, 'name') },
                  { header: 'Level', render: (r) => String(num(r, 'level')) },
                  { header: 'Template', render: (r) => str(r, 'template') },
                  { header: 'Lab', render: (r) => str(r, 'lab_id') || '— (local only)' },
                  {
                    header: 'Action',
                    render: (r) => <AwsButton variant="secondary" onClick={() => { setProjectId(str(r, 'id')); const f = projectFiles[0]; if (f) openFile(str(f, 'path')) }}>Open editor</AwsButton>,
                  },
                ]}
                rows={projects}
                emptyMessage="No projects yet — create one above."
              />
            </div>
          </AwsCard>

          {projectId !== '' && (
            <div>
              <AwsCard title={`Editor — ${str((project.data?.['project'] as Rec) ?? {}, 'name')}`}>
                {project.isPending ? <Loading label="Loading" /> : project.isError ? <ErrorBox message="Could not load the project." /> : (
                  <div>
                    <p>Linked lab: <strong>{str((project.data?.['project'] as Rec) ?? {}, 'lab_id') || 'none — link a lab to run container execution'}</strong></p>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginBottom: 8 }}>
                      <input value={labId} onChange={(e) => setLabId(e.target.value)} placeholder="Lab ID to link" style={{ padding: 6 }} />
                      <AwsButton variant="secondary" disabled={labId.trim() === '' || linkLab.isPending} onClick={() => linkLab.mutate()}>Link lab</AwsButton>
                    </div>
                    <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
                      <div style={{ minWidth: 180 }}>
                        <strong>Files</strong>
                        <ul style={{ listStyle: 'none', padding: 0 }}>
                          {projectFiles.map((f) => (
                            <li key={str(f, 'path')}>
                              <button type="button" onClick={() => openFile(str(f, 'path'))} style={{ background: 'none', border: 'none', color: '#0073bb', cursor: 'pointer', textDecoration: filePath === str(f, 'path') ? 'underline' : 'none', padding: '2px 0' }}>
                                {str(f, 'path')}
                              </button>
                            </li>
                          ))}
                        </ul>
                        <input value={filePath} onChange={(e) => setFilePath(e.target.value)} placeholder="new file path (.tf)" style={{ padding: 6, width: '100%' }} />
                        <p style={{ fontSize: 12, color: '#687078' }}>Files are saved per project in the backend. Save validates the whole project (cross-file references allowed).</p>
                      </div>
                      <div style={{ flex: 1, minWidth: 300 }}>
                        <textarea
                          value={editor}
                          onChange={(e) => { setEditor(e.target.value); setEditorDirty(true) }}
                          rows={14}
                          spellCheck={false}
                          style={{ width: '100%', fontFamily: 'monospace', fontSize: 13, padding: 8 }}
                          placeholder="# Write Terraform configuration here…"
                        />
                        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 8 }}>
                          <AwsButton disabled={!editorDirty || saveFile.isPending} onClick={() => saveFile.mutate()}>Save {filePath}</AwsButton>
                          <AwsButton variant="secondary" onClick={() => setShowPreview((v) => !v)}>{showPreview ? 'Hide highlighting' : 'Show highlighting'}</AwsButton>
                          <AwsButton variant="secondary" disabled={validateProject.isPending} onClick={() => validateProject.mutate()}>Validate (local)</AwsButton>
                          <AwsButton variant="secondary" disabled={formatProject.isPending} onClick={() => formatProject.mutate()}>Format (local)</AwsButton>
                          <AwsButton variant="secondary" disabled={planPreview.isPending} onClick={() => planPreview.mutate()}>Plan preview (local diff)</AwsButton>
                        </div>
                        {showPreview && (
                          <pre style={{ background: '#f7f7f7', padding: 12, overflowX: 'auto', fontSize: 13, fontFamily: 'monospace', marginTop: 8 }}>{highlightHcl(editor)}</pre>
                        )}
                        <p style={{ fontSize: 12, color: '#687078' }}>Validate / format / plan preview are local checks — they do not run the Terraform binary and do not need Docker. Container execution is below.</p>
                      </div>
                    </div>
                  </div>
                )}
              </AwsCard>

              <AwsCard title="Execution workflow — container runner (Milestone 7)">
                <p>Execution runs the real Terraform binary (hashicorp/terraform:1.9.8) in a container against the linked lab. Without Docker it honestly returns 503 terraform_unavailable — never a faked plan. Apply needs an approved, unexpired plan plus confirmation; destroy needs confirmation.</p>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                  <select value={operation} onChange={(e) => setOperation(e.target.value)} style={{ padding: 6 }}>
                    <option value="init">init</option>
                    <option value="validate">validate</option>
                    <option value="plan">plan</option>
                    <option value="apply">apply</option>
                    <option value="destroy">destroy</option>
                  </select>
                  <input value={planId} onChange={(e) => setPlanId(e.target.value)} placeholder="Plan ID (apply only)" style={{ padding: 6 }} />
                  <label>
                    <input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} /> I confirm this {operation} (required for apply/destroy)
                  </label>
                  <AwsButton disabled={execute.isPending} onClick={() => execute.mutate()}>Run {operation}</AwsButton>
                  {latestPreview && (
                    <AwsButton variant="secondary" disabled={approvePreview.isPending} onClick={() => approvePreview.mutate(str(latestPreview, 'id'))}>
                      Approve latest plan preview ({str(latestPreview, 'id').slice(0, 8)}…)
                    </AwsButton>
                  )}
                </div>
                <div style={{ marginTop: 12 }}>
                  <AwsTable<Rec>
                    columns={[
                      { header: 'Operation', render: (r) => str(r, 'operation') },
                      { header: 'Status', render: (r) => str(r, 'status') },
                      { header: 'Source', render: (r) => str(r, 'source') },
                      { header: 'Error', render: (r) => str(r, 'error') },
                      { header: 'Log', render: (r) => str(r, 'log').slice(0, 120) },
                    ]}
                    rows={execRows}
                    emptyMessage="No executions yet."
                  />
                </div>
                <p style={{ fontSize: 12, color: '#687078' }}>
                  Fidelity: init / validate / plan / apply / destroy — REAL (container) when Docker is present, honestly 503 without it. Plan preview — LOCAL static diff, labelled as such. Unsupported operations are not shown as buttons.
                </p>
              </AwsCard>

              <AwsCard title="State — versioned snapshots, locking, backup, drift">
                {state.isPending ? <Loading label="Loading" /> : state.isError ? <ErrorBox message="No state recorded yet." /> : (
                  <div>
                    <p>
                      Current version: <strong>v{num(stateView, 'version')}</strong> (source: {str(stateView, 'source')}) —{' '}
                      {boolOf(stateView, 'locked') ? <strong>locked by {str(stateView, 'locked_by')} until {str(stateView, 'lock_expires_at')}</strong> : 'unlocked'} —{' '}
                      {((stateView['resources'] as Rec[]) ?? []).length} recorded resources
                    </p>
                    <ul>{((stateView['resources'] as Rec[]) ?? []).map((r, i) => <li key={i}>{str(r, 'address')} ({str(r, 'type')})</li>)}</ul>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <AwsButton variant="secondary" disabled={lockState.isPending} onClick={() => lockState.mutate()}>Lock state</AwsButton>
                      <AwsButton variant="secondary" disabled={unlockState.isPending} onClick={() => unlockState.mutate()}>Unlock state</AwsButton>
                      <AwsButton variant="secondary" disabled={backupState.isPending} onClick={() => backupState.mutate()}>Backup (new version)</AwsButton>
                      <input value={restoreVersion} onChange={(e) => setRestoreVersion(e.target.value)} placeholder="Version to restore" style={{ padding: 6, width: 140 }} />
                      <AwsButton variant="secondary" disabled={restoreVersion.trim() === '' || restoreState.isPending} onClick={() => restoreState.mutate()}>Restore as new version</AwsButton>
                    </div>
                    <p style={{ fontSize: 12, color: '#687078' }}>State is append-only: restores create a new version copied from the restored one — history is never rewritten and state is never edited by string replacement.</p>
                    <p><strong>Version history:</strong> {stateVersions.map((v) => `v${num(v, 'version')} (${str(v, 'source')})`).join(', ') || '—'}</p>
                    <p><strong>Drift:</strong> {driftRows.length === 0 ? 'no drift detected' : `${driftRows.length} finding(s)`}</p>
                    {driftRows.length > 0 && (
                      <AwsTable<Rec>
                        columns={[
                          { header: 'Address', render: (r) => str(r, 'address') },
                          { header: 'Kind', render: (r) => str(r, 'kind') },
                          { header: 'Detail', render: (r) => str(r, 'detail') },
                        ]}
                        rows={driftRows}
                        emptyMessage="No drift."
                      />
                    )}
                    <p style={{ fontSize: 12, color: '#687078' }}>Drift compares declared configuration, recorded state, and live lab resources (when a lab is linked).</p>
                  </div>
                )}
              </AwsCard>
            </div>
          )}
        </div>
      )}

      {tab === 'provider' && (
        <div>
          <AwsCard title="CloudForge Terraform provider">
            <p>The CloudForge provider maps cloudforge_* resources onto real CloudForge APIs. It never needs AWS credentials and never reaches real AWS. aws_* resources execute through the container runner against the local emulator.</p>
            <p><FidelityBadge fidelity="PARTIAL" /> Local validate/format/plan-preview are real; container plan/apply needs Docker and a linked lab.</p>
            {provider.isPending ? <Loading label="Loading" /> : provider.isError ? <ErrorBox message="Could not load provider resources." /> : (
              <AwsTable<Rec>
                columns={[
                  { header: 'Resource type', render: (r) => str(r, 'type') },
                  { header: 'Description', render: (r) => str(r, 'description') },
                  { header: 'CloudForge API', render: (r) => str(r, 'cloudforge_api') },
                  { header: 'Fidelity', render: (r) => str(r, 'fidelity') },
                ]}
                rows={providerResources}
                emptyMessage="No provider resources."
              />
            )}
            <p style={{ fontSize: 12, color: '#687078' }}>
              Installation &amp; versioning: the provider is a learning surface served by this API (GET /api/v1/terraform-learning/provider). There is no registry download; the provider version tracks the platform version. AWS-provider learning mode works only with a local emulator configured — real AWS credentials are refused.
            </p>
          </AwsCard>
        </div>
      )}
    </div>
  )
}
