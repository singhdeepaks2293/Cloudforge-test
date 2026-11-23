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

type Rec = Record<string, unknown> & { id: string } & { id: string }
const listOf = (v: unknown, key: string): Rec[] => {
  const arr = Array.isArray((v as Rec)?.[key]) ? ((v as Rec)[key] as Record<string, unknown>[]) : []
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * SQS console (Phase 3) — mirrors the AWS SQS console's Queues list,
 * queue detail (Send/Receive, message inspection, DLQ view) and the
 * Create-queue wizard. Queues and messages are DURABLE
 * (Postgres-persisted): Receive hides a message until the visibility
 * timeout instead of deleting it, and at max receives the message
 * moves to the DLQ instead of being returned. Redrive moves DLQ
 * messages back. FIFO queues are not built.
 *
 * v4 survey (2026-11-22) left nav: Queues, Queues with errors, DLQs.
 * Queues columns in the survey's exact order (7): Name, Type,
 * Created, Messages available, Messages in flight, Encryption,
 * Content-based deduplication. Type is Standard for every queue (no
 * FIFO queues exist); Created is the queue's created_at; Encryption
 * and Content-based deduplication have no backend value and render
 * '—'. Queues with errors lists queues with a dead-letter queue
 * configured and the live DLQ message count (CloudForge records no
 * per-queue error state); DLQs lists the queues that serve as a
 * dead-letter queue for another queue. FIFO Queues is kept even
 * though the new survey drops it: the old survey recorded it and the
 * render test pins the 'no FIFO' note.
 */
export function SqsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [vt, setVt] = useState(30)
  const [maxRecv, setMaxRecv] = useState(0)
  const [dlqId, setDlqId] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'queues')
  const goToList = () => setSection('queues')
  const [selected, setSelected] = useState('')
  const [body, setBody] = useState('{"order": 1}')
  const [notice, setNotice] = useState('')
  const [filter, setFilter] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [detailTab, setDetailTab] = useState<'details' | 'messages' | 'dlq'>('details')

  const queues = useQuery({ queryKey: ['sqs-queues'], queryFn: () => request<Rec>('/api/v1/aws/sqs/queues'), retry: 1 })
  const rows = listOf(queues.data, 'queues')
  const messages = useQuery({
    queryKey: ['sqs-messages', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/sqs/queues/${selected}/messages`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['sqs-queues'] })
    // Prefix match: covers the selected queue's messages and the DLQ's.
    void queryClient.invalidateQueries({ queryKey: ['sqs-messages'] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/sqs/queues', {
      method: 'POST',
      body: { name: name.trim(), visibility_timeout_seconds: vt, max_receives: maxRecv, dead_letter_queue_id: dlqId.trim() === '' ? undefined : dlqId.trim() },
    }),
    onSuccess: () => {
      setNotice('Queue created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create queue failed: ${e.message}`),
  })
  const send = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/sqs/queues/${selected}/messages`, { method: 'POST', body: { body } }),
    onSuccess: () => {
      setNotice('Message sent.')
      refresh()
    },
    onError: (e: Error) => setNotice(`Send failed: ${e.message}`),
  })
  const receive = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/sqs/queues/${selected}/receive`, { method: 'POST', body: { max_messages: 1 } }),
    onSuccess: (d) => {
      const moved = Number(d.moved_to_dead_letter_queue ?? 0)
      setNotice(`Received ${String(d.count ?? 0)} message(s).${moved > 0 ? ` ${moved} moved to the DLQ (max receives reached).` : ''}`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Receive failed: ${e.message}`),
  })

  const sel = rows.find((q) => String(q.id) === selected)
  const selDlqId = typeof sel?.dead_letter_queue_id === 'string' ? (sel.dead_letter_queue_id as string) : ''
  const dlqName = rows.find((q) => String(q.id) === selDlqId)?.name
  const dlqMessages = useQuery({
    queryKey: ['sqs-messages', selDlqId],
    queryFn: () => request<Rec>(`/api/v1/aws/sqs/queues/${selDlqId}/messages`),
    enabled: selDlqId !== '',
    retry: 1,
  })
  const checkedQueue = rows.find((q) => String(q.id) === checked[0])
  const filteredRows = rows.filter(
    (q) =>
      filter.trim() === '' ||
      String(q.name ?? '').toLowerCase().includes(filter.toLowerCase()) ||
      String(q.id).toLowerCase().includes(filter.toLowerCase()),
  )

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Application Integration', to: '/services' }, { label: 'SQS' }]}
      title="SQS — Queues"
      description="Durable message queues (Postgres-persisted). Receive hides messages until the visibility timeout; at max receives they move to the dead-letter queue."
      fidelity="PARTIAL"
      fidelityFootnote="durable Postgres queues · no FIFO"
      subNav={[
        { id: 'home', label: 'SQS dashboard' },
        { id: 'queues', label: 'Queues' },
        { id: 'errors', label: 'Queues with errors' },
        { id: 'dlqs', label: 'DLQs' },
        { id: 'fifo', label: 'FIFO Queues' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="SQS"
            banner="Durable Postgres queues: visibility timeout, retention, DLQ redrive after max receives are real. No FIFO queues, no long polling."
            actions={[
              { label: 'Create queue', primary: true, onClick: goToList },
            { label: 'View SNS topics (publisher)', to: '/services/sns' },
            ]}
            cards={[
              { label: 'Queues', count: rows.length, onSelect: goToList },
            { label: 'Messages (selected queue)', count: selected !== '' ? listOf(messages.data, 'messages').length : '—', onSelect: goToList },
            { label: 'Dead-letter queues', count: rows.filter((q) => rows.some((o) => String(o.dead_letter_queue_id ?? '') === String(q.id))).length, onSelect: () => setSection('dlqs'), note: 'Queues that serve as a dead-letter queue' },
            { label: 'FIFO queues', count: 0, disabledReason: 'No FIFO queues in CloudForge.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={queues.isFetching}
            health={[
              queues.isError ? 'Queues could not be loaded.' : `${rows.length} queue(s) listed.`,
            "Durable Postgres queues — the old in-memory simulator is replaced.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — messaging track', to: '/learn' },
            { label: 'EventBridge (event rules)', to: '/services/eventbridge' },
            ]}
          />
      )}
      {section === 'fifo' && (
        <UnsupportedSection
          title="FIFO Queues"
          reason="FIFO queues are UNSUPPORTED in CloudForge — there is no FIFO queue API. Queues are standard queues only: no ordering guarantee, no deduplication, no message-group id."
          actionLabel="Create FIFO queue"
        />
      )}
      {section === 'queues' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {queues.isPending && <Loading label="Loading queues" />}
      {queues.isError && <ErrorBox message={`Queues could not be loaded (${queues.error.message}).`} />}
      {queues.data && (
        <AwsCard title={`Queues (${rows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="sqs-name" className="text-sm font-medium">Queue name</label>
            <input id="sqs-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="orders" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Visibility timeout (s)
              <input type="number" min={1} max={43200} value={vt} onChange={(e) => setVt(Number(e.target.value))} className="ml-2 w-20 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
            <label className="text-sm">
              Max receives (0 = no DLQ)
              <input type="number" min={0} value={maxRecv} onChange={(e) => setMaxRecv(Number(e.target.value))} className="ml-2 w-20 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
            <label className="text-sm">
              DLQ queue id
              <input value={dlqId} onChange={(e) => setDlqId(e.target.value)} placeholder="optional" className="ml-2 w-64 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-xs" />
            </label>
            <AwsButton type="submit" disabled={create.isPending}>Create queue</AwsButton>
          </form>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={queues.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          >
            <AwsActionsMenu
              items={[
                {
                  label: 'Redrive DLQ',
                  disabledReason:
                    checkedQueue === undefined
                      ? 'Select a queue first.'
                      : String(checkedQueue.dead_letter_queue_id ?? '') === ''
                        ? 'The selected queue has no dead-letter queue configured.'
                        : undefined,
                  onClick: () => {
                    void request<Rec>(`/api/v1/aws/sqs/queues/${String(checkedQueue?.id)}/redrive`, { method: 'POST', body: {} }).then((d) => {
                      setNotice(`Redrive moved ${String(d.messages_moved ?? 0)} message(s) back from the DLQ.`)
                      refresh()
                    }).catch((e: Error) => setNotice(`Redrive failed: ${e.message}`))
                  },
                },
                {
                  label: 'Purge',
                  disabledReason: checkedQueue === undefined ? 'Select a queue first.' : undefined,
                  onClick: () => {
                    void request(`/api/v1/aws/sqs/queues/${String(checkedQueue?.id)}/purge`, { method: 'POST', body: {} }).then(() => {
                      setNotice('Queue purged.')
                      refresh()
                    }).catch((e: Error) => setNotice(`Purge failed: ${e.message}`))
                  },
                },
                {
                  label: 'Delete',
                  disabledReason: checkedQueue === undefined ? 'Select a queue first.' : undefined,
                  onClick: () => {
                    void request(`/api/v1/aws/sqs/queues/${String(checkedQueue?.id)}`, { method: 'DELETE' }).then(() => {
                      setNotice('Queue deleted.')
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
            caption="SQS queues"
            emptyMessage="No queues yet. Create one above."
            rows={filteredRows}
            totalCount={rows.length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter queues by name"
            selectable
            selectedIds={checked}
            onToggleRow={(id) => {
              setSelected(id)
              setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))
            }}
            onToggleAll={(all) => setChecked(all ? filteredRows.map((r) => r.id) : [])}
            columns={[
              // Queues columns in the v4 survey's exact order (7).
              // Type is Standard for every queue (no FIFO queues
              // exist); Encryption and Content-based deduplication
              // have no backend value and render '—'.
              { header: 'Name', render: (q) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(q.id))}>{String(q.name)}</button> },
              { header: 'Type', render: () => 'Standard' },
              { header: 'Created', render: (q) => (typeof q.created_at === 'string' ? new Date(q.created_at as string).toLocaleString() : '—') },
              { header: 'Messages available', render: (q) => String(q.approximate_visible_messages ?? '—') },
              { header: 'Messages in flight', render: (q) => String(q.approximate_invisible_messages ?? '—') },
              { header: 'Encryption', render: () => '—' },
              { header: 'Content-based deduplication', render: () => '—' },
              {
                header: 'Actions',
                render: (q) => (
                  <span className="flex gap-2">
                    <AwsButton
                      variant="secondary"
                      disabled={String(q.dead_letter_queue_id ?? '') === ''}
                      onClick={() => {
                        void request<Rec>(`/api/v1/aws/sqs/queues/${String(q.id)}/redrive`, { method: 'POST', body: {} }).then((d) => {
                          setNotice(`Redrive moved ${String(d.messages_moved ?? 0)} message(s) back from the DLQ.`)
                          refresh()
                        }).catch((e: Error) => setNotice(`Redrive failed: ${e.message}`))
                      }}
                    >
                      Redrive DLQ
                    </AwsButton>
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/sqs/queues/${String(q.id)}/purge`, { method: 'POST', body: {} }).then(() => {
                          setNotice('Queue purged.')
                          refresh()
                        }).catch((e: Error) => setNotice(`Purge failed: ${e.message}`))
                      }}
                    >
                      Purge
                    </AwsButton>
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/sqs/queues/${String(q.id)}`, { method: 'DELETE' }).then(() => {
                          setNotice('Queue deleted.')
                          setSelected('')
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
          <p className="mt-2 text-sm text-aws-muted">
            AWS names: Messages available = Visible messages; Messages in flight = Invisible messages
            (hidden until the visibility timeout). Queue settings (visibility timeout, max receives, DLQ) are on
            the queue detail below.
          </p>
        </AwsCard>
      )}
      {sel && (
        <AwsCard title={`Queue detail — ${String(sel.name)}`}>
          <ConsoleTabs
            tabs={[
              { id: 'details', label: 'Details' },
              { id: 'messages', label: 'Messages' },
              { id: 'dlq', label: 'Dead-letter queue' },
            ]}
            active={detailTab}
            onChange={(id) => setDetailTab(id as typeof detailTab)}
          />
          <div className="mt-3">
          {detailTab === 'details' && (
            <dl className="grid grid-cols-2 gap-2 text-sm">
              <dt className="font-medium">Queue name</dt><dd>{String(sel.name)}</dd>
              <dt className="font-medium">Queue id</dt><dd><code className="text-xs">{String(sel.id)}</code></dd>
              <dt className="font-medium">Visibility timeout</dt><dd>{String(sel.visibility_timeout_seconds ?? '—')}s</dd>
              <dt className="font-medium">Max receives</dt><dd>{String(sel.max_receives ?? 0)} (0 = no DLQ)</dd>
              <dt className="font-medium">Dead-letter queue</dt><dd>{selDlqId !== '' ? `${String(dlqName ?? selDlqId)}` : 'none configured'}</dd>
              <dt className="font-medium">Retention</dt><dd>{String(sel.message_retention_seconds ?? '—')}s</dd>
              <dt className="font-medium">Visible messages</dt><dd>{String(sel.approximate_visible_messages ?? '—')}</dd>
              <dt className="font-medium">Invisible messages</dt><dd>{String(sel.approximate_invisible_messages ?? '—')}</dd>
            </dl>
          )}
          {detailTab === 'messages' && (
          <>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (body.trim() !== '') send.mutate()
            }}
          >
            <label htmlFor="sqs-body" className="text-sm font-medium">Message body</label>
            <input id="sqs-body" value={body} onChange={(e) => setBody(e.target.value)} className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={send.isPending}>Send message</AwsButton>
            <AwsButton variant="secondary" disabled={receive.isPending} onClick={() => receive.mutate()}>Receive (1)</AwsButton>
            <AwsButton variant="secondary" onClick={refresh}>Refresh messages</AwsButton>
          </form>
          <p className="mb-2 text-sm text-aws-muted">Receive hides the message until the visibility timeout — it does not delete it. Delete a message by its receipt handle after processing it.</p>
          {messages.isPending && <Loading label="Loading messages" />}
          {messages.data && (
            <AwsTable<Rec>
              caption="Messages"
              emptyMessage="No messages in this queue."
              rows={listOf(messages.data, 'messages')}
              totalCount={Number(messages.data.count ?? 0)}
              columns={[
                { header: 'Message id', render: (m) => <code className="text-xs">{String(m.id)}</code> },
                { header: 'Body', render: (m) => <code className="text-xs">{String(m.body ?? '')}</code> },
                { header: 'Receive count', render: (m) => String(m.receive_count ?? 0) },
                { header: 'Visible', render: (m) => (m.visible === false ? 'no (in flight)' : 'yes') },
                {
                  header: 'Actions',
                  render: (m) => (
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/sqs/queues/${selected}/messages/${String(m.id)}`, { method: 'DELETE' }).then(() => {
                          setNotice('Message deleted.')
                          refresh()
                        }).catch((e: Error) => setNotice(`Delete message failed: ${e.message}`))
                      }}
                    >
                      Delete
                    </AwsButton>
                  ),
                },
              ]}
            />
          )}
          </>
          )}
          {detailTab === 'dlq' && (
            <>
              {selDlqId === '' ? (
                <p className="text-sm text-aws-muted">
                  No dead-letter queue configured for this queue. Set Max receives above 0 and a DLQ queue id when
                  creating the queue — at max receives a message moves to the DLQ instead of being returned.
                </p>
              ) : (
                <>
                  <p className="mb-3 text-sm text-aws-muted">
                    Dead-letter queue: <strong>{String(dlqName ?? selDlqId)}</strong> — messages that reached max
                    receives land here. Redrive moves them back to this queue.
                  </p>
                  <div className="mb-3">
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request<Rec>(`/api/v1/aws/sqs/queues/${String(sel.id)}/redrive`, { method: 'POST', body: {} }).then((d) => {
                          setNotice(`Redrive moved ${String(d.messages_moved ?? 0)} message(s) back from the DLQ.`)
                          refresh()
                        }).catch((e: Error) => setNotice(`Redrive failed: ${e.message}`))
                      }}
                    >
                      Redrive DLQ
                    </AwsButton>
                  </div>
                  {dlqMessages.isPending && <Loading label="Loading dead-letter messages" />}
                  {dlqMessages.data && (
                    <AwsTable<Rec>
                      caption="Dead-letter messages"
                      emptyMessage="No messages in the dead-letter queue."
                      rows={listOf(dlqMessages.data, 'messages')}
                      totalCount={Number(dlqMessages.data.count ?? 0)}
                      columns={[
                        { header: 'Message id', render: (m) => <code className="text-xs">{String(m.id)}</code> },
                        { header: 'Body', render: (m) => <code className="text-xs">{String(m.body ?? '')}</code> },
                        { header: 'Receive count', render: (m) => String(m.receive_count ?? 0) },
                      ]}
                    />
                  )}
                </>
              )}
            </>
          )}
          </div>
        </AwsCard>
      )}
        </>
      )}
      {section === 'errors' && (
        <QueuesWithErrorsSection rows={rows} onNotice={setNotice} onRefresh={refresh} />
      )}
      {section === 'dlqs' && (
        <DlqQueuesSection rows={rows} onSelect={(id) => { setSelected(id); setSection('queues') }} />
      )}
    </ServiceConsole>
  )
}

/** Live DLQ message count for one queue (used by the Queues with errors / DLQs sections). */
function DlqMessageCount({ queueId }: { queueId: string }) {
  const q = useQuery({
    queryKey: ['sqs-messages', queueId],
    queryFn: () => request<Rec>(`/api/v1/aws/sqs/queues/${queueId}/messages`),
    retry: 1,
  })
  if (q.isPending) return <>…</>
  if (q.isError) return <>—</>
  return <>{listOf(q.data, 'messages').length}</>
}

/**
 * Queues with errors (v4 survey nav item) — CloudForge records no
 * per-queue error state, so a queue is listed here when it has a
 * dead-letter queue configured: those are the queues whose failed
 * messages are captured. The DLQ messages column is the live count
 * of failed messages waiting in the queue's DLQ.
 */
function QueuesWithErrorsSection({ rows, onNotice, onRefresh }: {
  rows: Rec[]
  onNotice: (s: string) => void
  onRefresh: () => void
}) {
  const errorRows = rows.filter((q) => String(q.dead_letter_queue_id ?? '') !== '')
  return (
    <AwsCard title={`Queues with errors (${errorRows.length})`}>
      <p className="mb-3 text-sm text-aws-muted">
        CloudForge records no per-queue error state — a queue is listed here when it has a dead-letter queue
        configured. Messages that reach max receives land in the DLQ; redrive moves them back.
      </p>
      <AwsTable<Rec>
        caption="Queues with errors"
        emptyMessage="No queue has a dead-letter queue configured — configure one (max receives above 0 and a DLQ queue id) when creating a queue."
        rows={errorRows}
        totalCount={errorRows.length}
        columns={[
          { header: 'Name', render: (q) => String(q.name) },
          { header: 'Dead-letter queue', render: (q) => String(rows.find((o) => String(o.id) === String(q.dead_letter_queue_id ?? ''))?.name ?? String(q.dead_letter_queue_id ?? '')) },
          { header: 'DLQ messages', render: (q) => <DlqMessageCount queueId={String(q.dead_letter_queue_id ?? '')} /> },
          {
            header: 'Actions',
            render: (q) => (
              <AwsButton
                variant="secondary"
                onClick={() => {
                  void request<Rec>(`/api/v1/aws/sqs/queues/${String(q.id)}/redrive`, { method: 'POST', body: {} }).then((d) => {
                    onNotice(`Redrive moved ${String(d.messages_moved ?? 0)} message(s) back from the DLQ.`)
                    onRefresh()
                  }).catch((e: Error) => onNotice(`Redrive failed: ${e.message}`))
                }}
              >
                Redrive DLQ
              </AwsButton>
            ),
          },
        ]}
      />
    </AwsCard>
  )
}

/**
 * DLQs (v4 survey nav item) — the queues that serve as a dead-letter
 * queue for another queue, with the source queues and the live
 * message count in each.
 */
function DlqQueuesSection({ rows, onSelect }: {
  rows: Rec[]
  onSelect: (id: string) => void
}) {
  const dlqIds = new Set(rows.map((q) => String(q.dead_letter_queue_id ?? '')).filter((id) => id !== ''))
  const dlqRows = rows.filter((q) => dlqIds.has(String(q.id)))
  return (
    <AwsCard title={`DLQs (${dlqRows.length})`}>
      <p className="mb-3 text-sm text-aws-muted">
        Queues that serve as a dead-letter queue for another queue. Open one to see its messages and redrive them.
      </p>
      <AwsTable<Rec>
        caption="Dead-letter queues"
        emptyMessage="No queue is used as a dead-letter queue yet — configure one (max receives above 0 and a DLQ queue id) when creating a queue."
        rows={dlqRows}
        totalCount={dlqRows.length}
        columns={[
          { header: 'Name', render: (q) => <button type="button" className="text-aws-link underline" onClick={() => onSelect(String(q.id))}>{String(q.name)}</button> },
          {
            header: 'Source queues',
            render: (q) => rows.filter((o) => String(o.dead_letter_queue_id ?? '') === String(q.id)).map((o) => String(o.name)).join(', ') || '—',
          },
          { header: 'Messages', render: (q) => <DlqMessageCount queueId={String(q.id)} /> },
        ]}
      />
    </AwsCard>
  )
}
