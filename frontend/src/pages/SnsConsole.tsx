import { useState } from 'react'
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
  return arr.map((r, i) => ({ ...r, id: typeof r.id === 'string' ? r.id : typeof r.name === 'string' ? r.name : `${key}-${i}` }))
}

/**
 * SNS console (Phase 3) — mirrors the AWS SNS console's Topics list,
 * Subscriptions and Publish flow, plus delivery records. Topics are
 * DURABLE (Postgres-persisted). Fan-out is real: SQS queues (durable
 * enqueue), HTTP(S) endpoints (loopback only, SSRF-guarded) and
 * Lambda functions. Every delivery is recorded, success or failure.
 * Email/SMS delivery is not built.
 */
export function SnsConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [selected, setSelected] = useState('')
  const [proto, setProto] = useState('sqs')
  const [endpoint, setEndpoint] = useState('')
  const [message, setMessage] = useState('hello from SNS')
  const [subject, setSubject] = useState('')
  const [notice, setNotice] = useState('')

  const topics = useQuery({ queryKey: ['sns-topics'], queryFn: () => request<Rec>('/api/v1/aws/sns/topics'), retry: 1 })
  const rows = listOf(topics.data, 'topics')
  const subs = useQuery({
    queryKey: ['sns-subs', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/sns/topics/${selected}/subscriptions`),
    enabled: selected !== '',
    retry: 1,
  })
  const deliveries = useQuery({
    queryKey: ['sns-deliveries', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/sns/topics/${selected}/deliveries`),
    enabled: selected !== '',
    retry: 1,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['sns-topics'] })
    void queryClient.invalidateQueries({ queryKey: ['sns-subs', selected] })
    void queryClient.invalidateQueries({ queryKey: ['sns-deliveries', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/sns/topics', { method: 'POST', body: { name: name.trim() } }),
    onSuccess: () => {
      setNotice('Topic created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create topic failed: ${e.message}`),
  })
  const subscribe = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/sns/topics/${selected}/subscribe`, { method: 'POST', body: { protocol: proto, endpoint: endpoint.trim() } }),
    onSuccess: () => {
      setNotice('Subscription created.')
      setEndpoint('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Subscribe failed: ${e.message}`),
  })
  const publish = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/sns/topics/${selected}/publish`, { method: 'POST', body: { message, subject: subject === '' ? undefined : subject } }),
    onSuccess: (d) => {
      const dels = Array.isArray(d.deliveries) ? (d.deliveries as Rec[]) : []
      const okCount = dels.filter((x) => x.status === 'delivered').length
      setNotice(`Published (message ${String(d.message_id)}): ${okCount}/${dels.length} deliveries succeeded.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Publish failed: ${e.message}`),
  })

  const sel = rows.find((t) => String(t.id) === selected)

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Application Integration', to: '/services' }, { label: 'SNS' }]}
        title="SNS — Topics"
        description="Durable topics with real fan-out to SQS queues, HTTP(S) endpoints and Lambda functions. Every delivery is recorded."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="durable Postgres topics · no email/SMS" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {topics.isPending && <Loading label="Loading topics" />}
      {topics.isError && <ErrorBox message={`Topics could not be loaded (${topics.error.message}).`} />}
      {topics.data && (
        <AwsCard title={`Topics (${rows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="sns-name" className="text-sm font-medium">Topic name</label>
            <input id="sns-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="alerts" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={create.isPending}>Create topic</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="SNS topics"
            emptyMessage="No topics yet. Create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Topic name', render: (t) => <button type="button" className="text-aws-link underline" onClick={() => setSelected(String(t.id))}>{String(t.name)}</button> },
              { header: 'ARN', render: (t) => <code className="text-xs">{String(t.arn ?? '—')}</code> },
              {
                header: 'Actions',
                render: (t) => (
                  <AwsButton
                    variant="secondary"
                    onClick={() => {
                      void request(`/api/v1/aws/sns/topics/${String(t.id)}`, { method: 'DELETE' }).then(() => {
                        setNotice('Topic deleted.')
                        setSelected('')
                        refresh()
                      }).catch((e: Error) => setNotice(`Delete failed: ${e.message}`))
                    }}
                  >
                    Delete
                  </AwsButton>
                ),
              },
            ]}
          />
        </AwsCard>
      )}
      {sel && (
        <>
          <AwsCard title={`Subscriptions — ${String(sel.name)}`}>
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (endpoint.trim() !== '') subscribe.mutate()
              }}
            >
              <label className="text-sm">
                Protocol
                <select value={proto} onChange={(e) => setProto(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="sqs">SQS queue</option>
                  <option value="http">HTTP endpoint</option>
                  <option value="https">HTTPS endpoint</option>
                  <option value="lambda">Lambda function</option>
                </select>
              </label>
              <label htmlFor="sns-endpoint" className="text-sm font-medium">Endpoint</label>
              <input
                id="sns-endpoint"
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
                placeholder={proto === 'sqs' ? 'queue id or name' : proto === 'lambda' ? 'function id or name' : 'http://localhost:8081/hook'}
                className="w-80 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
              />
              <AwsButton type="submit" disabled={subscribe.isPending}>Subscribe</AwsButton>
              <span className="text-sm text-aws-muted">HTTP endpoints must pass the platform SSRF guard (loopback hosts only). Email/SMS are not built.</span>
            </form>
            <AwsTable<Rec>
              caption="Subscriptions"
              emptyMessage="No subscriptions yet."
              rows={listOf(subs.data, 'subscriptions')}
              totalCount={listOf(subs.data, 'subscriptions').length}
              columns={[
                { header: 'Protocol', render: (s) => String(s.protocol ?? '—') },
                { header: 'Endpoint', render: (s) => <code className="text-xs">{String(s.endpoint ?? '—')}</code> },
                {
                  header: 'Actions',
                  render: (s) => (
                    <AwsButton
                      variant="secondary"
                      onClick={() => {
                        void request(`/api/v1/aws/sns/topics/${selected}/subscriptions/${String(s.id)}`, { method: 'DELETE' }).then(() => {
                          setNotice('Subscription removed.')
                          refresh()
                        }).catch((e: Error) => setNotice(`Unsubscribe failed: ${e.message}`))
                      }}
                    >
                      Unsubscribe
                    </AwsButton>
                  ),
                },
              ]}
            />
          </AwsCard>
          <AwsCard title="Publish">
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (message.trim() !== '') publish.mutate()
              }}
            >
              <label htmlFor="sns-message" className="text-sm font-medium">Message</label>
              <input id="sns-message" value={message} onChange={(e) => setMessage(e.target.value)} className="w-96 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <label htmlFor="sns-subject" className="text-sm font-medium">Subject</label>
              <input id="sns-subject" value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="optional" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <AwsButton type="submit" disabled={publish.isPending}>Publish</AwsButton>
            </form>
            <AwsTable<Rec>
              caption="Delivery records"
              emptyMessage="No deliveries yet. Publish a message to see fan-out results."
              rows={listOf(deliveries.data, 'deliveries')}
              totalCount={listOf(deliveries.data, 'deliveries').length}
              columns={[
                { header: 'Message', render: (d) => <code className="text-xs">{String(d.message_id ?? '').slice(0, 13)}</code> },
                { header: 'Protocol', render: (d) => String(d.protocol ?? '—') },
                { header: 'Status', render: (d) => String(d.status ?? '—') },
                { header: 'Detail', render: (d) => String(d.detail ?? '—') },
              ]}
            />
          </AwsCard>
        </>
      )}
    </div>
  )
}
