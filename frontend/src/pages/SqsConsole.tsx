import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleWithHome, ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
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
 */
export function SqsConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [vt, setVt] = useState(30)
  const [maxRecv, setMaxRecv] = useState(0)
  const [dlqId, setDlqId] = useState('')
  const [selected, setSelected] = useState('')
  const [body, setBody] = useState('{"order": 1}')
  const [notice, setNotice] = useState('')

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
    void queryClient.invalidateQueries({ queryKey: ['sqs-messages', selected] })
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

  return (
    <ConsoleWithHome
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Application Integration', to: '/services' }, { label: 'SQS' }]}
      title="SQS — Queues"
      description="Durable message queues (Postgres-persisted). Receive hides messages until the visibility timeout; at max receives they move to the dead-letter queue."
      fidelity="PARTIAL"
      fidelityFootnote="durable Postgres queues · no FIFO"
      homeLabel="SQS dashboard"
      listLabel="Queues"
      initialSection={initialSection}
      home={(goToList) => (
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
            { label: 'Dead-letter queues', count: '—', onSelect: goToList, note: 'DLQ view on the queue detail' },
            { label: 'FIFO queues', count: 0, disabledReason: 'No FIFO queues in CloudForge.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={queues.isFetching}
            health={[
              "queues.isError ? 'Queues could not be loaded.' : `${rows.length} queue(s) listed.`",
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
    >
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
          <AwsTable<Rec>
            caption="SQS queues"
            emptyMessage="No queues yet. Create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Queue name', render: (q) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(q.id))}>{String(q.name)}</button> },
              { header: 'Visible messages', render: (q) => String(q.approximate_visible_messages ?? '—') },
              { header: 'Invisible messages', render: (q) => String(q.approximate_invisible_messages ?? '—') },
              { header: 'Visibility timeout', render: (q) => `${String(q.visibility_timeout_seconds ?? '—')}s` },
              { header: 'Max receives', render: (q) => String(q.max_receives ?? 0) },
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
        </AwsCard>
      )}
      {sel && (
        <AwsCard title={`Queue detail — ${String(sel.name)}`}>
          <p className="mb-3 text-sm text-aws-muted">
            Dead-letter queue: {selDlqId !== '' ? `${String(dlqName ?? selDlqId)} (select it in the table to inspect its messages)` : 'none configured'} ·
            Retention: {String(sel.message_retention_seconds ?? '—')}s
          </p>
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
        </AwsCard>
      )}
    </ConsoleWithHome>
  )
}
