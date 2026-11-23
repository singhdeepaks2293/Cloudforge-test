import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import { AwsActionsMenu, AwsPreferencesButton, AwsRefreshButton, AwsToolbar } from '../components/AwsShell.tsx'
import { request } from '../services/api.ts'

type Rec = Record<string, unknown> & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * EventBridge console (Phase 3) — mirrors the AWS EventBridge
 * console's Rules list, Event buses and Put-events flow. Rules are
 * DURABLE (Postgres-persisted). Pattern matching is source /
 * detail-type arrays plus nested detail subset matching — not the
 * full AWS event-pattern language. Targets are SQS queues, SNS
 * topics and Lambda functions (validated at rule creation). AWS
 * service events are NOT generated; events arrive through PutEvents.
 *
 * v4 survey (2026-11-22) left nav: Dashboard, Event buses (New),
 * Pipes, Scheduler (Schedules, Schedule groups, Scheduled rules
 * (legacy)), Integration (Partner event sources, API destinations,
 * Connections), Schema registry (Schemas), Classic event routing
 * (Global endpoints), Developer resources (Learn, Sandbox, Quick
 * starts). Rules redirect to Event buses in AWS, so the rules list
 * lives on the Event buses page (the default section) — the bus
 * table has the survey's exact columns: Name, Type, Status, ARN,
 * Schema discovery. Only the default bus exists (Type Default,
 * Status Active, Schema discovery Disabled). Event streams and
 * Settings were dropped from the new survey and are removed.
 */
export function EventBridgeConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [source, setSource] = useState('my.app')
  const [detailType, setDetailType] = useState('OrderPlaced')
  const [targetType, setTargetType] = useState('sqs')
  const [targetId, setTargetId] = useState('')
  const [eventJson, setEventJson] = useState('{"source": "my.app", "detail-type": "OrderPlaced", "detail": {"id": 1}}')
  const [putResult, setPutResult] = useState('')
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'buses')
  const goToList = () => setSection('buses')
  const [filter, setFilter] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [selected, setSelected] = useState('')
  const [detailTab, setDetailTab] = useState<'overview' | 'events'>('overview')

  const rules = useQuery({ queryKey: ['eb-rules'], queryFn: () => request<Rec>('/api/v1/aws/events/rules'), retry: 1 })
  const rows = listOf(rules.data, 'rules')
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['eb-rules'] })
  const create = useMutation({
    mutationFn: () => {
      const target: Record<string, unknown> = { type: targetType }
      if (targetType === 'sqs') target.queue_id = targetId.trim()
      if (targetType === 'sns') target.topic_id = targetId.trim()
      if (targetType === 'lambda') target.function_id = targetId.trim()
      return request<Rec>('/api/v1/aws/events/rules', {
        method: 'POST',
        body: { name: name.trim(), source: source.trim(), detail_type: detailType.trim(), targets: [target] },
      })
    },
    onSuccess: () => {
      setNotice('Rule created.')
      setName('')
      setTargetId('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create rule failed: ${e.message}`),
  })

  const sel = rows.find((r) => String(r.id) === selected)
  const checkedRule = rows.find((r) => String(r.id) === checked[0])
  const filteredRows = rows.filter(
    (r) =>
      filter.trim() === '' ||
      String(r.name ?? '').toLowerCase().includes(filter.toLowerCase()) ||
      String(r.id).toLowerCase().includes(filter.toLowerCase()),
  )

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Application Integration', to: '/services' }, { label: 'EventBridge' }]}
      title="EventBridge — Rules"
      description="Durable event rules with pattern matching to SQS, SNS and Lambda targets. Events arrive through PutEvents — AWS service events are not generated."
      fidelity="PARTIAL"
      fidelityFootnote="durable Postgres rules · subset pattern language"
      subNav={[
        { id: 'home', label: 'EventBridge dashboard' },
        { id: 'buses', label: 'Event buses', badge: 'New' },
        { id: 'pipes', label: 'Pipes' },
        { id: 'schedules', label: 'Schedules', group: 'Scheduler' },
        { id: 'schedule-groups', label: 'Schedule groups', group: 'Scheduler' },
        { id: 'scheduled-rules', label: 'Scheduled rules (legacy)', group: 'Scheduler' },
        { id: 'partner-sources', label: 'Partner event sources', group: 'Integration' },
        { id: 'api-destinations', label: 'API destinations', group: 'Integration' },
        { id: 'connections', label: 'Connections', group: 'Integration' },
        { id: 'schemas', label: 'Schemas', group: 'Schema registry' },
        { id: 'global-endpoints', label: 'Global endpoints', group: 'Classic event routing' },
        { id: 'learn', label: 'Learn', group: 'Developer resources' },
        { id: 'sandbox', label: 'Sandbox', group: 'Developer resources' },
        { id: 'quick-starts', label: 'Quick starts', group: 'Developer resources' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="EventBridge"
            banner="Durable Postgres rules; pattern matching to SQS, SNS and Lambda targets. AWS service events are not generated — events arrive through PutEvents."
            actions={[
              { label: 'Create rule', primary: true, onClick: goToList },
            { label: 'View SQS queues (target)', to: '/services/sqs' },
            ]}
            cards={[
              { label: 'Rules', count: rows.length, onSelect: goToList },
            { label: 'Event buses', count: '—', onSelect: goToList, note: 'Default bus only' },
            { label: 'Archives', count: 0, disabledReason: 'Not modelled.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={rules.isFetching}
            health={[
              rules.isError ? 'Rules could not be loaded.' : `${rows.length} rule(s) listed.`,
            "Pattern = source / detail-type arrays + nested detail subset matching (not the full AWS event-pattern language).",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — messaging track', to: '/learn' },
            { label: 'Lambda (target)', to: '/services/lambda' },
            ]}
          />
      )}
      {section === 'buses' && (
        <div className="space-y-4">
          <AwsCard title="Event buses">
            <AwsTable<Rec>
              caption="Event buses"
              emptyMessage="No event bus."
              rows={[{ id: 'default', name: 'default' }]}
              totalCount={1}
              columns={[
                // Event buses columns in the v4 survey's exact order (5).
                { header: 'Name', render: (b) => String(b.name) },
                { header: 'Type', render: () => 'Default' },
                { header: 'Status', render: () => 'Active' },
                { header: 'ARN', render: () => <code className="text-xs">arn:cloudforge:events:local:event-bus/default</code> },
                { header: 'Schema discovery', render: () => 'Disabled' },
              ]}
            />
            <p className="mt-2 text-sm text-aws-muted">
              Only the default bus exists ({rows.length} rule(s)). There is no custom-bus API; rules always
              attach to the default bus. In AWS, Rules redirect to Event buses — the rules list is below.
            </p>
          </AwsCard>
          <AwsCard title="Permissions — default event bus">
            <p className="text-sm text-aws-muted">No resource-based policy.</p>
            <p className="mt-1 text-xs text-aws-muted">
              Per the 2026-11-22 policy-editor survey, the default bus Permissions tab is JSON-only and read-only
              in AWS; CloudForge has no resource-based policy to show.
            </p>
          </AwsCard>
        </div>
      )}
      {section === 'schedules' && (
        <UnsupportedSection
          title="Schedules"
          reason="EventBridge Scheduler is UNSUPPORTED in CloudForge — there is no schedule API and no schedule expressions."
          actionLabel="Create schedule"
        />
      )}
      {section === 'schemas' && (
        <UnsupportedSection
          title="Schema registry"
          reason="The schema registry is UNSUPPORTED in CloudForge — there is no schema API and no event schemas are stored."
          actionLabel="Create schema"
        />
      )}
      {section === 'pipes' && (
        <UnsupportedSection
          title="Pipes"
          reason="EventBridge Pipes are UNSUPPORTED in CloudForge — there is no pipe API."
          actionLabel="Create pipe"
        />
      )}
      {section === 'schedule-groups' && (
        <UnsupportedSection
          title="Schedule groups"
          reason="Schedule groups are UNSUPPORTED in CloudForge — there is no Scheduler API and no schedule groups are stored."
          actionLabel="Create schedule group"
        />
      )}
      {section === 'scheduled-rules' && (
        <UnsupportedSection
          title="Scheduled rules (legacy)"
          reason="Scheduled rules (legacy) are UNSUPPORTED in CloudForge — there is no schedule API. Scheduled rules were replaced by EventBridge Scheduler, which is also not built."
          actionLabel="Create scheduled rule"
        />
      )}
      {section === 'partner-sources' && (
        <UnsupportedSection
          title="Partner event sources"
          reason="Partner event sources are UNSUPPORTED in CloudForge — there is no partner-event API."
          actionLabel="Create partner event source"
        />
      )}
      {section === 'api-destinations' && (
        <UnsupportedSection
          title="API destinations"
          reason="API destinations are UNSUPPORTED in CloudForge — there is no API-destination API."
          actionLabel="Create API destination"
        />
      )}
      {section === 'connections' && (
        <UnsupportedSection
          title="Connections"
          reason="Connections are UNSUPPORTED in CloudForge — there is no connection API."
          actionLabel="Create connection"
        />
      )}
      {section === 'global-endpoints' && (
        <UnsupportedSection
          title="Global endpoints"
          reason="Global endpoints are UNSUPPORTED in CloudForge — there is no global-endpoint API."
          actionLabel="Create global endpoint"
        />
      )}
      {section === 'learn' && (
        <AwsCard title="Learn — EventBridge">
          <p className="text-sm text-aws-muted">
            EventBridge learning material is in the Learning Center — the messaging track covers rules, patterns
            and targets.
          </p>
          <div className="mt-3">
            <AwsButton variant="secondary" onClick={() => { window.location.href = '/learn' }}>Open Learning Center</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'sandbox' && (
        <UnsupportedSection
          title="Sandbox"
          reason="The EventBridge sandbox is UNSUPPORTED in CloudForge — there is no sandbox API. Test events with the Put events form on the Event buses page."
          actionLabel="Open sandbox"
        />
      )}
      {section === 'quick-starts' && (
        <AwsCard title="Quick starts — EventBridge">
          <p className="text-sm text-aws-muted">
            Quick starts are in the Learning Center — the messaging track has guided exercises for rules and targets.
          </p>
          <div className="mt-3">
            <AwsButton variant="secondary" onClick={() => { window.location.href = '/learn' }}>Open Learning Center</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'buses' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {putResult !== '' && <p className="border border-aws-border bg-white px-4 py-2 font-mono text-sm" role="status">{putResult}</p>}
      {rules.isPending && <Loading label="Loading rules" />}
      {rules.isError && <ErrorBox message={`Rules could not be loaded (${rules.error.message}).`} />}
      {rules.data && (
        <AwsCard title={`Rules (${rows.length}) — default event bus`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '' && targetId.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="eb-name" className="text-sm font-medium">Rule name</label>
            <input id="eb-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="orders-rule" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="eb-source" className="text-sm font-medium">Source</label>
            <input id="eb-source" value={source} onChange={(e) => setSource(e.target.value)} className="w-32 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="eb-detail-type" className="text-sm font-medium">Detail type</label>
            <input id="eb-detail-type" value={detailType} onChange={(e) => setDetailType(e.target.value)} className="w-36 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Target
              <select value={targetType} onChange={(e) => setTargetType(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="sqs">SQS queue</option>
                <option value="sns">SNS topic</option>
                <option value="lambda">Lambda function</option>
              </select>
            </label>
            <label htmlFor="eb-target" className="text-sm font-medium">Target id</label>
            <input id="eb-target" value={targetId} onChange={(e) => setTargetId(e.target.value)} placeholder="queue / topic / function id" className="w-64 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={create.isPending}>Create rule</AwsButton>
          </form>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={rules.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          >
            <AwsActionsMenu
              items={[
                {
                  label: checkedRule !== undefined && String(checkedRule.state) === 'ENABLED' ? 'Disable' : 'Enable',
                  disabledReason: checkedRule === undefined ? 'Select a rule first.' : undefined,
                  onClick: () => {
                    const next = String(checkedRule?.state) === 'ENABLED' ? 'DISABLED' : 'ENABLED'
                    void request(`/api/v1/aws/events/rules/${String(checkedRule?.id)}/state`, { method: 'POST', body: { state: next } }).then(() => {
                      setNotice(`Rule ${next === 'ENABLED' ? 'enabled' : 'disabled'}.`)
                      refresh()
                    }).catch((e: Error) => setNotice(`Change state failed: ${e.message}`))
                  },
                },
                {
                  label: 'Delete',
                  disabledReason: checkedRule === undefined ? 'Select a rule first.' : undefined,
                  onClick: () => {
                    void request(`/api/v1/aws/events/rules/${String(checkedRule?.id)}`, { method: 'DELETE' }).then(() => {
                      setNotice('Rule deleted.')
                      setSelected('')
                      setChecked([])
                      refresh()
                    }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                  },
                },
              ]}
            />
          </AwsToolbar>
          <AwsTable<Rec>
            caption="EventBridge rules"
            emptyMessage="No rules yet. Create one above — the target id must exist (validated at creation)."
            rows={filteredRows}
            totalCount={rows.length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter rules by name"
            selectable
            selectedIds={checked}
            onToggleRow={(id) => {
              setSelected(id)
              setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))
            }}
            onToggleAll={(all) => setChecked(all ? filteredRows.map((r) => r.id) : [])}
            columns={[
              { header: 'Rule name', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(r.id))}>{String(r.name)}</button> },
              { header: 'Pattern', render: (r) => <code className="text-xs">{JSON.stringify(r.pattern ?? {})}</code> },
              { header: 'State', render: (r) => String(r.state ?? '—') },
              { header: 'Matched events', render: (r) => String(r.matched_events ?? 0) },
              { header: 'Deliveries', render: (r) => String(r.deliveries ?? 0) },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        const next = String(r.state) === 'ENABLED' ? 'DISABLED' : 'ENABLED'
                        void request(`/api/v1/aws/events/rules/${String(r.id)}/state`, { method: 'POST', body: { state: next } }).then(() => {
                          setNotice(`Rule ${next === 'ENABLED' ? 'enabled' : 'disabled'}.`)
                          refresh()
                        }).catch((e: Error) => setNotice(`Change state failed: ${e.message}`))
                      }}
                    >
                      {String(r.state) === 'ENABLED' ? 'Disable' : 'Enable'}
                    </AwsButton>
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/events/rules/${String(r.id)}`, { method: 'DELETE' }).then(() => {
                          setNotice('Rule deleted.')
                          refresh()
                        }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                      }}
                    >
                      Delete
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
          <p className="mt-2 text-sm text-aws-muted">No schedule expressions and no input transformers. Event buses: only the default bus exists.</p>
        </AwsCard>
      )}
      {sel && (
        <AwsCard title={`Rule detail — ${String(sel.name)}`}>
          <ConsoleTabs
            tabs={[
              { id: 'overview', label: 'Overview' },
              { id: 'events', label: 'Events' },
            ]}
            active={detailTab}
            onChange={(id) => setDetailTab(id as typeof detailTab)}
          />
          <div className="mt-3">
          {detailTab === 'overview' && (
            <dl className="grid grid-cols-2 gap-2 text-sm">
              <dt className="font-medium">Rule name</dt><dd>{String(sel.name)}</dd>
              <dt className="font-medium">Rule id</dt><dd><code className="text-xs">{String(sel.id)}</code></dd>
              <dt className="font-medium">State</dt><dd>{String(sel.state ?? '—')}</dd>
              <dt className="font-medium">Pattern</dt><dd><code className="text-xs">{JSON.stringify(sel.pattern ?? {})}</code></dd>
              <dt className="font-medium">Targets</dt><dd><code className="text-xs">{JSON.stringify(sel.targets ?? [])}</code></dd>
              <dt className="font-medium">Matched events</dt><dd>{String(sel.matched_events ?? 0)}</dd>
              <dt className="font-medium">Deliveries</dt><dd>{String(sel.deliveries ?? 0)}</dd>
            </dl>
          )}
          {detailTab === 'events' && (
            <div className="space-y-2 text-sm">
              <p>
                Matched events: <strong>{String(sel.matched_events ?? 0)}</strong> · Deliveries:{' '}
                <strong>{String(sel.deliveries ?? 0)}</strong>
              </p>
              <p className="text-aws-muted">
                CloudForge records no per-event list for a rule — only the matched-event and delivery counts above.
                Use Put events below to send a test event and watch the counts move.
              </p>
            </div>
          )}
          </div>
        </AwsCard>
      )}
      <AwsCard title="Put events (test)">
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            let entry: unknown
            try {
              entry = JSON.parse(eventJson) as unknown
            } catch {
              setPutResult('Event is not valid JSON.')
              return
            }
            void request<Rec>('/api/v1/aws/events/put-events', { method: 'POST', body: { entries: [entry] } }).then((d) => {
              const results = Array.isArray(d.results) ? (d.results as Rec[]) : []
              const first = results[0] ?? {}
              setPutResult(`Matched rules: ${Array.isArray(first.matched_rules) ? (first.matched_rules as unknown[]).join(', ') || '(none)' : '(none)'} · deliveries: ${String(first.deliveries ?? 0)}`)
              refresh()
            }).catch((err: Error) => setPutResult(`Put events failed: ${err.message}`))
          }}
        >
          <label className="text-sm">
            Event JSON (needs source + detail-type)
            <textarea value={eventJson} onChange={(e) => setEventJson(e.target.value)} rows={3} className="ml-2 w-96 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
          </label>
          <AwsButton type="submit">Put event</AwsButton>
        </form>
      </AwsCard>
        </>
      )}
    </ServiceConsole>
  )
}
