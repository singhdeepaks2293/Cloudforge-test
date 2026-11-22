import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
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

  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Application Integration', to: '/services' }, { label: 'EventBridge' }]}
      title="EventBridge — Rules"
      description="Durable event rules with pattern matching to SQS, SNS and Lambda targets. Events arrive through PutEvents — AWS service events are not generated."
      fidelity="PARTIAL"
      fidelityFootnote="durable Postgres rules · subset pattern language"
      homeLabel="EventBridge dashboard"
      listLabel="Rules"
      initialSection={initialSection}
      home={(goToList) => (
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
              "rules.isError ? 'Rules could not be loaded.' : `${rows.length} rule(s) listed.`",
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
    >
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
          <AwsTable<Rec>
            caption="EventBridge rules"
            emptyMessage="No rules yet. Create one above — the target id must exist (validated at creation)."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Rule name', render: (r) => String(r.name) },
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
    </ConsoleWithHome>
  )
}
