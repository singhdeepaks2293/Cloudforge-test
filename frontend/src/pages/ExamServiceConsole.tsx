/**
 * Shared builder for the v3 exam-service dedicated consoles (audit gap
 * #3 — the 17 generic-console services). Each console is a config
 * object in ExamServiceConsoles.tsx rendered by ExamServiceConsole:
 *
 * - Home dashboard (VPC pattern): banner, action links, "Resources by
 *   Region" cards from the live list query, Service Health, Settings,
 *   Additional Information, "What you can do instead".
 * - List tab(s) wired to the real API-emulated backend (migration
 *   014, /api/v1/aws/…): list / create / delete where the backend
 *   supports it, plus the service's own action (failover, confirm,
 *   run, …) via runAction. A tab with no backend stays UNSUPPORTED
 *   with its button disabled and the reason shown — nothing is
 *   invented.
 * - Detail tabs on a selected row: Details (auto-rendered from the
 *   record) plus the service's own tabs from the config.
 * - Operations tab (registry operations, honest fidelity) and a
 *   Learning tab, carried over from the generic console.
 *
 * Tabs/columns the 2026-11-22 AWS survey records are used verbatim
 * (Backup nav, CloudFront nav + Distributions columns, EFS nav).
 * Where the survey records nothing for a service, the nav and columns
 * are CloudForge's own structure and the console says so — never
 * presented as observed AWS layout. CloudForge branding only.
 */
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueries, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsFlash, AwsPreferencesButton, AwsRefreshButton, AwsToolbar } from '../components/AwsShell.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { request } from '../services/api.ts'
import { useService } from '../services/registry.ts'
import { LITE_GENERIC_SERVICES } from '../services/liteServices.ts'
import {
  fmtTime,
  inputCls,
  listOf,
  useExamList,
  type ExamCtx,
  type ExamDetailCtx,
  type ExamServiceConfig,
  type Rec,
} from '../services/examConsole.ts'
import type { RegistryOperation } from '../types/index.ts'

function FieldValue({ value }: { value: unknown }) {
  if (value === null || value === undefined || value === '') return <span className="text-aws-muted">—</span>
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return <span>{String(value)}</span>
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-aws-muted">[] (empty)</span>
    if (value.every((v) => typeof v === 'string' || typeof v === 'number')) return <span>{value.join(', ')}</span>
    return (
      <span>
        {value.length} item(s)
        <code className="ml-2 text-xs">{JSON.stringify(value).slice(0, 300)}</code>
      </span>
    )
  }
  return <code className="text-xs">{JSON.stringify(value).slice(0, 300)}</code>
}

/** Auto-rendered Details tab: every stored field of the record. */
function DetailsTab({ sel }: { sel: Rec }) {
  const hidden = new Set(['id', 'name', 'owner_id', 'service', 'kind'])
  const entries = Object.entries(sel).filter(([k]) => !hidden.has(k))
  return (
    <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-[220px_1fr]">
      <dt className="font-medium">ID</dt>
      <dd className="font-mono text-xs break-all">{sel.id}</dd>
      <dt className="font-medium">Name</dt>
      <dd>{String(sel.name ?? '—')}</dd>
      {entries.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="font-medium">{k.replace(/_/g, ' ')}</dt>
          <dd className="break-words">
            {k.endsWith('_at') || k === 'completed_at' || k === 'started_at' ? fmtTime(v) : <FieldValue value={v} />}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export function UnsupportedTab({ title, reason, actionLabel }: { title: string; reason: string; actionLabel: string }) {
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

export function ExamServiceConsole({ config, initialSection }: { config: ExamServiceConfig; initialSection?: string }) {
  const queryClient = useQueryClient()
  const [section, setSection] = useState(initialSection ?? config.defaultSection ?? config.listSection)
  const [selectedId, setSelectedId] = useState('')
  const [detailTab, setDetailTab] = useState('details')
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<Record<string, string>>({})
  const [notice, setNotice] = useState('')
  const [noticeTone, setNoticeTone] = useState<'success' | 'info' | 'error'>('info')
  const [selectedOp, setSelectedOp] = useState<string | null>(null)
  const [opTab, setOpTab] = useState<'details' | 'limitations'>('details')

  const list = useExamList(config.listPath, config.listKey)
  const rows = list.rows
  // Extra live counts for the Home cards (config.homeCountSources),
  // fetched with useQueries so the list is fixed-length and stable.
  const extraCountQueries = useQueries({
    queries: (config.homeCountSources ?? []).map((s) => ({
      queryKey: ['exam-list', s.path],
      queryFn: () => request<Rec>(`/api/v1/aws/${s.path}`),
      retry: 1,
    })),
  })
  const extraCounts: Record<string, number> = {}
  ;(config.homeCountSources ?? []).forEach((s, i) => {
    const data = extraCountQueries[i]?.data
    // Only publish a count once the list has loaded — while it is
    // loading (or errored) the card keeps its '—' fallback.
    if (data !== undefined) extraCounts[s.path] = listOf(data, s.listKey).length
  })
  const serviceQuery = useService(config.id)
  const generic = LITE_GENERIC_SERVICES[config.id]
  const sel = rows.find((r) => r.id === selectedId) ?? null

  const flash = (message: string, tone: 'success' | 'info' | 'error' = 'info') => {
    setNotice(message)
    setNoticeTone(tone)
  }
  const refresh = () => {
    list.refresh()
    void queryClient.invalidateQueries({ queryKey: ['service', config.id] })
  }

  const create = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = {}
      for (const f of config.createFields) {
        const raw = form[f.key] ?? ''
        if (raw.trim() === '') continue
        if (f.type === 'number') body[f.key] = Number(raw)
        else if (f.type === 'list') body[f.key] = raw.split(/[\n,]+/).map((s) => s.trim()).filter((s) => s !== '')
        else body[f.key] = raw.trim()
      }
      return request<Rec>(`/api/v1/aws/${config.listPath}`, { method: 'POST', body })
    },
    onSuccess: (d) => {
      flash(`${config.kindLabel} ${String(d.name ?? '')} created.`, 'success')
      setForm({})
      setCreateOpen(false)
      if (typeof d.id === 'string') {
        setSelectedId(d.id)
        setDetailTab('details')
      }
      refresh()
    },
    onError: (e: Error) => flash(`Create failed: ${e.message}`, 'error'),
  })
  const del = useMutation({
    mutationFn: (id: string) => request(`/api/v1/aws/${config.listPath}/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => {
      flash(`${config.kindLabel} deleted.`, 'success')
      setSelectedId('')
      refresh()
    },
    onError: (e: Error) => flash(`Delete failed: ${e.message}`, 'error'),
  })
  const action = useMutation({
    mutationFn: ({ path, body }: { path: string; body?: Record<string, unknown> }) =>
      request<Rec>(`/api/v1/aws/${path}`, { method: 'POST', body: body ?? {} }),
    onSuccess: (d) => {
      flash(typeof d.note === 'string' && d.note !== '' ? d.note : 'Done.', 'success')
      refresh()
    },
    onError: (e: Error) => flash(`Action failed: ${e.message}`, 'error'),
  })
  const runAction = (path: string, body?: Record<string, unknown>) => action.mutate({ path, body })

  const ctx: ExamCtx = {
    refresh,
    flash,
    runAction,
    actionBusy: action.isPending,
    selectedId,
    setSelectedId: (id: string) => {
      setSelectedId(id)
      setDetailTab('details')
    },
    setSection,
  }
  const detailCtx: ExamDetailCtx | null = sel === null ? null : { ...ctx, sel }

  const openCreate = () => {
    const initial: Record<string, string> = {}
    for (const f of config.createFields) {
      if (f.defaultValue !== undefined) initial[f.key] = String(f.defaultValue)
    }
    setForm(initial)
    setCreateOpen(true)
    setSection(config.listSection)
  }

  const svc = serviceQuery.data
  const operations = useMemo(() => svc?.operations ?? [], [svc])
  const selectedOperation = operations.find((o) => o.operation === selectedOp) ?? null

  const subNav = [
    { id: 'home', label: `${config.name} dashboard` },
    { id: config.listSection, label: config.listLabel },
    ...(config.extraTabs ?? []).map((t) => ({ id: t.id, label: t.label, group: t.group })),
    { id: 'operations', label: 'Operations' },
    { id: 'learning', label: 'Learning' },
  ]
  const extraTab = (config.extraTabs ?? []).find((t) => t.id === section)

  const health =
    config.health?.(rows) ??
    [
      list.isError ? `${config.listLabel} could not be loaded.` : `${rows.length} ${config.kindLabel}(s) listed.`,
      'Region: cf-local-1 (region scoping is display-only; all resources live in cf-local-1).',
      `Registry fidelity: ${config.fidelity}.`,
    ]

  return (
    <ServiceConsole
      icon={config.icon}
      breadcrumb={[
        { label: 'CloudForge', to: '/' },
        { label: config.category, to: '/services' },
        { label: config.name },
      ]}
      title={`${config.name} — ${config.listLabel}`}
      description={config.description}
      fidelity={config.fidelity}
      fidelityFootnote={config.fidelityFootnote}
      subNav={subNav}
      activeSub={section}
      onSubChange={(next) => {
        setSection(next)
        setSelectedId('')
        setSelectedOp(null)
      }}
      actions={
        config.createDisabledReason === undefined ? (
          <AwsButton variant="primary" onClick={openCreate}>
            {config.createLabel}
          </AwsButton>
        ) : (
          <AwsButton variant="primary" disabled title={config.createDisabledReason}>
            {config.createLabel}
          </AwsButton>
        )
      }
    >
      {notice !== '' && <AwsFlash tone={noticeTone}>{notice}</AwsFlash>}

      {section === 'home' && (
        <ServiceHomeDashboard
          serviceName={config.name}
          banner={config.banner}
          bannerTone={config.bannerTone ?? 'info'}
          actions={[
            config.createDisabledReason === undefined
              ? { label: config.createLabel, primary: true, onClick: openCreate }
              : { label: config.createLabel, primary: true, disabledReason: config.createDisabledReason },
            { label: 'Learning Center', to: '/learn' },
          ]}
          cards={config.homeCards(rows, setSection, extraCounts)}
          onRefresh={refresh}
          refreshing={list.isFetching}
          health={health}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — SAA-C03 / SAP-C02 tracks', to: '/learn' },
            { label: 'Certification coverage map', to: '/learn' },
          ]}
        >
          {generic !== undefined && (
            <AwsCard title="What you can do instead">
              <ul className="space-y-2 text-sm">
                {generic.alternatives.map((a) => (
                  <li key={a.label}>
                    <Link to={a.to} className="text-aws-link hover:underline">
                      {a.label}
                    </Link>
                    <span className="text-aws-muted"> — {a.note}</span>
                  </li>
                ))}
              </ul>
            </AwsCard>
          )}
          {config.homeExtra}
        </ServiceHomeDashboard>
      )}

      {section === config.listSection && (
        <div className="space-y-4">
          {createOpen && config.createDisabledReason === undefined && (
            <AwsCard title={config.createLabel}>
              <div className="space-y-3">
                <label className="block text-sm">
                  <span className="mb-1 block font-medium">Name</span>
                  <input
                    value={form.name ?? ''}
                    onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                    placeholder={`${config.kindLabel}-1`}
                    className={`w-full max-w-md ${inputCls}`}
                  />
                </label>
                {config.createFields.map((f) => (
                  <label key={f.key} className="block text-sm">
                    <span className="mb-1 block font-medium">
                      {f.label}
                      {f.required === true ? ' (required)' : ''}
                    </span>
                    {f.type === 'select' ? (
                      <select
                        value={form[f.key] ?? ''}
                        onChange={(e) => setForm((prev) => ({ ...prev, [f.key]: e.target.value }))}
                        className={inputCls}
                      >
                        <option value="">— select —</option>
                        {(f.options ?? []).map((o) => (
                          <option key={o} value={o}>
                            {o}
                          </option>
                        ))}
                      </select>
                    ) : f.type === 'list' ? (
                      <textarea
                        value={form[f.key] ?? ''}
                        onChange={(e) => setForm((prev) => ({ ...prev, [f.key]: e.target.value }))}
                        placeholder={f.placeholder ?? 'one per line or comma-separated'}
                        rows={3}
                        className={`w-full max-w-md ${inputCls}`}
                      />
                    ) : (
                      <input
                        type={f.type === 'number' ? 'number' : 'text'}
                        value={form[f.key] ?? ''}
                        onChange={(e) => setForm((prev) => ({ ...prev, [f.key]: e.target.value }))}
                        placeholder={f.placeholder ?? ''}
                        className={`w-full max-w-md ${inputCls}`}
                      />
                    )}
                    {f.hint !== undefined && <span className="mt-1 block text-xs text-aws-muted">{f.hint}</span>}
                  </label>
                ))}
                <div className="flex flex-wrap gap-2">
                  <AwsButton variant="primary" onClick={() => (form.name ?? '').trim() !== '' && create.mutate()} disabled={create.isPending || (form.name ?? '').trim() === ''}>
                    {config.createLabel}
                  </AwsButton>
                  <AwsButton onClick={() => setCreateOpen(false)}>Cancel</AwsButton>
                </div>
                {config.createNote !== undefined && <p className="text-xs text-aws-muted">{config.createNote}</p>}
                <p className="text-xs text-aws-muted">
                  Create form is CloudForge&apos;s own — the 2026-11-22 survey opened no create wizard, so no AWS
                  wizard steps were recorded for this service.
                </p>
              </div>
            </AwsCard>
          )}

          <AwsToolbar
            right={
              <>
                {config.listActions?.(ctx)}
                <AwsRefreshButton onClick={refresh} busy={list.isFetching} />
                <AwsPreferencesButton disabledReason="Table preferences are not persisted in CloudForge." />
              </>
            }
          >
            <span className="text-sm text-aws-muted">
              {rows.length} {config.kindLabel}(s) · API-emulated records — nothing here provisions real AWS
              infrastructure.
            </span>
          </AwsToolbar>

          {list.isPending && <Loading label={`Loading ${config.listLabel.toLowerCase()}`} />}
          {list.isError && <ErrorBox message={`${config.listLabel} could not be loaded (${list.errorMessage}).`} />}
          {list.data !== undefined && (
            <AwsCard>
              <AwsTable<Rec>
                caption={`${config.name} ${config.listLabel.toLowerCase()}`}
                emptyMessage={`No ${config.kindLabel}s yet. Choose ${config.createLabel} to make one.`}
                rows={rows}
                totalCount={rows.length}
                columns={[
                  ...config.columns,
                  {
                    header: 'Actions',
                    render: (r) => (
                      <span className="flex gap-2">
                        <button
                          type="button"
                          className="text-aws-link hover:underline"
                          onClick={() => {
                            setSelectedId(r.id)
                            setDetailTab('details')
                          }}
                        >
                          Open
                        </button>
                        <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(r.id)}>
                          Delete
                        </button>
                      </span>
                    ),
                  },
                ]}
              />
              {config.columnsNote !== undefined && <p className="mt-2 text-xs text-aws-muted">{config.columnsNote}</p>}
            </AwsCard>
          )}

          {detailCtx !== null && (
            <AwsCard>
              <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
                <div>
                  <h2 className="text-base font-semibold">{String(sel?.name ?? '')}</h2>
                  <p className="text-xs text-aws-muted">
                    {config.kindLabel} · {sel?.id} · status {String(sel?.status ?? '—')}
                  </p>
                </div>
                <div className="flex gap-2">
                  <AwsButton onClick={() => sel !== null && del.mutate(sel.id)}>Delete</AwsButton>
                  <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelectedId('')}>
                    Close
                  </button>
                </div>
              </div>
              <ConsoleTabs
                tabs={[
                  { id: 'details', label: 'Details' },
                  ...config.detailTabs.map((t) => ({ id: t.id, label: t.label, disabledReason: t.disabledReason })),
                ]}
                active={detailTab}
                onChange={setDetailTab}
              />
              <div className="pt-3">
                {detailTab === 'details' && <DetailsTab sel={detailCtx.sel} />}
                {config.detailTabs.map(
                  (t) => detailTab === t.id && <div key={t.id}>{t.render(detailCtx)}</div>,
                )}
              </div>
            </AwsCard>
          )}
        </div>
      )}

      {extraTab !== undefined && (
        <div className="space-y-4">
          {extraTab.unsupported !== undefined && extraTab.render === undefined ? (
            <UnsupportedTab title={`${config.name} — ${extraTab.label}`} reason={extraTab.unsupported} actionLabel={extraTab.label} />
          ) : (
            extraTab.render?.(ctx)
          )}
        </div>
      )}

      {section === 'operations' && (
        <div className="space-y-4">
          {serviceQuery.isPending && <Loading label="Loading operations" />}
          {serviceQuery.isError && <ErrorBox message={`Operations could not be loaded (${serviceQuery.error?.message ?? 'not found'}).`} />}
          {svc !== undefined && (
            <>
              <AwsCard title={`Operations (${operations.length}) — region cf-local-1`}>
                <p className="mb-3 text-sm text-aws-muted">
                  The registered operations for {config.name}. An operation with an HTTP route is served by the
                  API-emulated backend; one without a route has no runner in this console.
                </p>
                <AwsTable<RegistryOperation & { id: string }>
                  caption={`${config.name} operations`}
                  emptyMessage={`No operations are registered for ${config.name}.`}
                  rows={operations.map((op) => ({ ...op, id: op.operation }))}
                  totalCount={operations.length}
                  columns={[
                    {
                      header: 'Operation',
                      render: (op) => (
                        <button
                          type="button"
                          className="text-aws-link hover:underline"
                          onClick={() => {
                            setSelectedOp(op.operation)
                            setOpTab('details')
                          }}
                        >
                          {op.operation}
                        </button>
                      ),
                    },
                    { header: 'Fidelity', render: (op) => <FidelityBadge fidelity={op.fidelity} footnote="" /> },
                    { header: 'Supported', render: (op) => (op.supported ? 'Yes' : 'No') },
                    {
                      header: 'HTTP route',
                      render: (op) =>
                        op.http_route ? <code>{op.http_route}</code> : <span className="text-aws-muted">No HTTP route — disabled</span>,
                    },
                    { header: 'Limitations', render: (op) => <span className="text-aws-muted">{(op.limitations ?? []).join(' ') || '—'}</span> },
                  ]}
                />
              </AwsCard>
              {selectedOperation !== null && (
                <AwsCard title={`Operation: ${selectedOperation.operation}`}>
                  <ConsoleTabs
                    tabs={[
                      { id: 'details', label: 'Details' },
                      { id: 'limitations', label: 'Limitations' },
                    ]}
                    active={opTab}
                    onChange={(next) => setOpTab(next as 'details' | 'limitations')}
                  />
                  {opTab === 'details' && (
                    <div className="mt-3 space-y-3">
                      <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-[200px_1fr]">
                        <dt className="font-medium">Operation</dt>
                        <dd>{selectedOperation.operation}</dd>
                        <dt className="font-medium">Fidelity</dt>
                        <dd>
                          <FidelityBadge fidelity={selectedOperation.fidelity} footnote="" />
                        </dd>
                        <dt className="font-medium">Supported</dt>
                        <dd>{selectedOperation.supported ? 'Yes' : 'No'}</dd>
                        <dt className="font-medium">HTTP route</dt>
                        <dd>{selectedOperation.http_route ?? 'None — no backend route serves this operation.'}</dd>
                      </dl>
                      <AwsButton variant="primary" disabled>
                        Run {selectedOperation.operation}
                      </AwsButton>
                      <p className="text-sm text-aws-muted">
                        Disabled:{' '}
                        {selectedOperation.http_route
                          ? 'this operation has a route but no generic runner in this console — use the list and detail tabs.'
                          : 'no HTTP route serves this operation in CloudForge.'}
                      </p>
                    </div>
                  )}
                  {opTab === 'limitations' && (
                    <div className="mt-3">
                      {(selectedOperation.limitations ?? []).length === 0 ? (
                        <p className="text-sm text-aws-muted">No limitations are recorded for this operation.</p>
                      ) : (
                        <ul className="list-disc space-y-1 pl-5 text-sm">
                          {(selectedOperation.limitations ?? []).map((l) => (
                            <li key={l}>{l}</li>
                          ))}
                        </ul>
                      )}
                      {svc.differs !== undefined && svc.differs !== '' && (
                        <p className="mt-2 text-sm text-aws-muted">Differs from AWS: {svc.differs}</p>
                      )}
                    </div>
                  )}
                </AwsCard>
              )}
            </>
          )}
        </div>
      )}

      {section === 'learning' && (
        <AwsCard title="Learning exercises">
          {(svc?.learning_exercises ?? []).length === 0 ? (
            <p className="text-sm text-aws-muted">No learning track is linked to this service yet.</p>
          ) : (
            <ul className="list-disc space-y-1 pl-5 text-sm">
              {(svc?.learning_exercises ?? []).map((track) => (
                <li key={track}>
                  <Link to="/learn" className="text-aws-link hover:underline">
                    {track} track
                  </Link>
                </li>
              ))}
            </ul>
          )}
          <p className="mt-3 text-sm text-aws-muted">
            {config.name} is taught in the SAA-C03 / SAP-C02 tracks — see the certification coverage map in the{' '}
            <Link to="/learn" className="text-aws-link hover:underline">
              Learning Center
            </Link>
            .
          </p>
          {generic?.note !== undefined && <AwsFlash tone="info">{generic.note}</AwsFlash>}
        </AwsCard>
      )}
    </ServiceConsole>
  )
}
