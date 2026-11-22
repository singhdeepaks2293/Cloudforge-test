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
const str = (r: Rec, k: string): string => (typeof r[k] === 'string' ? (r[k] as string) : '')

const DEFAULT_TEMPLATE = `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Description": "SQS queue + SNS topic",
  "Resources": {
    "Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders"}},
    "Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events"}}
  }
}`

/**
 * CloudFormation console (Phase 4) — mirrors the AWS CloudFormation
 * console's Stacks list, stack detail (events, resources) and change
 * sets. This is a separate template engine with its own stack state —
 * NOT Terraform. Six resource types are provisioned for real: SQS
 * queue, SNS topic, DynamoDB table (Postgres subset), Lambda function
 * (inline code), S3 bucket (MinIO), RDS instance (platform Postgres).
 * Any other type is reported UNSUPPORTED in the change set, never
 * silently skipped. Failed creates roll back the resources they made;
 * resources deleted by an update are not restored.
 */
export function CloudFormationConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [template, setTemplate] = useState(DEFAULT_TEMPLATE)
  const [selected, setSelected] = useState('')
  const [csName, setCsName] = useState('')
  const [csTemplate, setCsTemplate] = useState('')
  const [validateResult, setValidateResult] = useState<Rec | null>(null)
  const [notice, setNotice] = useState('')

  const stacks = useQuery({ queryKey: ['cfn-stacks'], queryFn: () => request<Rec>('/api/v1/aws/cloudformation/stacks'), retry: 1 })
  const events = useQuery({
    queryKey: ['cfn-events', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/cloudformation/stacks/${selected}/events`),
    enabled: selected !== '',
    retry: 1,
  })
  const stackRows = listOf(stacks.data, 'stacks')
  const eventRows = listOf(events.data, 'events')
  const selStack = stackRows.find((s) => String(s.id) === selected)
  const selResources = selStack && typeof selStack.resources === 'object' && selStack.resources !== null
    ? Object.entries(selStack.resources as Record<string, Record<string, unknown>>).map(([logical, r]) => ({
        id: logical,
        logical_id: logical,
        type: String(r.type ?? ''),
        physical_id: String(r.physical_id ?? ''),
      }))
    : []

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['cfn-stacks'] })
    void queryClient.invalidateQueries({ queryKey: ['cfn-events', selected] })
  }
  const create = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/cloudformation/stacks', { method: 'POST', body: { name: name.trim(), template } }),
    onSuccess: (d) => {
      setNotice(`Stack ${String(d.status ?? '')}.`)
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create stack failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/cloudformation/stacks/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Stack deleted (its resources were deleted too).')
      setSelected('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete stack failed: ${e.message}`),
  })
  const validate = useMutation({
    mutationFn: () => request<Rec>('/api/v1/aws/cloudformation/validate-template', { method: 'POST', body: { template } }),
    onSuccess: (d) => setValidateResult(d),
    onError: (e: Error) => setNotice(`Validate template failed: ${e.message}`),
  })
  const createChangeset = useMutation({
    mutationFn: () =>
      request<Rec>(`/api/v1/aws/cloudformation/stacks/${selected}/changesets`, {
        method: 'POST',
        body: { name: csName.trim(), template: csTemplate },
      }),
    onSuccess: (d) => {
      const changes = Array.isArray(d.changes) ? (d.changes as Record<string, unknown>[]) : []
      const unsupported = changes.filter((c) => c.action === 'UNSUPPORTED').length
      setNotice(`Change set created: ${changes.length} change(s), ${unsupported} unsupported, executable: ${String(d.executable ?? false)}.`)
      setCsName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create change set failed: ${e.message}`),
  })

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management & Governance', to: '/services' }, { label: 'CloudFormation' }]}
        title="CloudFormation — Stacks"
        description="Infrastructure as code with its own template engine and stack state — not Terraform. Six resource types provision for real; anything else is UNSUPPORTED in the change set."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="6 resource types · rollback on failure" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}

      <AwsCard title={`Stacks (${stackRows.length})`}>
        <form
          className="mb-3 space-y-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '' && template.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="cfn-name" className="text-sm font-medium">Stack name</label>
            <input id="cfn-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="demo" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={create.isPending}>Create stack</AwsButton>
            <AwsButton variant="secondary" disabled={validate.isPending} onClick={() => validate.mutate()}>Validate template</AwsButton>
          </div>
          <div>
            <label htmlFor="cfn-template" className="mb-1 block text-sm font-medium">Template (JSON or YAML)</label>
            <textarea
              id="cfn-template"
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              rows={10}
              className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
            />
          </div>
        </form>
        {validateResult && (
          <p className="mb-3 border border-aws-border bg-white px-3 py-2 text-sm" role="status">
            Template {validateResult.valid === true ? 'is valid' : 'is NOT valid'} — {String(validateResult.resource_count ?? 0)} resource(s).
            {Array.isArray(validateResult.problems) && (validateResult.problems as unknown[]).length > 0 && (
              <span className="text-aws-error"> {(validateResult.problems as unknown[]).map((p) => String(p)).join('; ')}</span>
            )}
            {Array.isArray(validateResult.supported_types) && (
              <span className="text-aws-muted"> Supported: {(validateResult.supported_types as string[]).join(', ')}</span>
            )}
          </p>
        )}
        {stacks.isPending && <Loading label="Loading stacks" />}
        {stacks.isError && <ErrorBox message={`Stacks could not be loaded (${stacks.error.message}).`} />}
        {stacks.data && (
          <AwsTable<Rec>
            caption="CloudFormation stacks"
            emptyMessage="No stacks yet. Create one above."
            rows={stackRows}
            columns={[
              { header: 'Stack name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'Status', render: (r) => str(r, 'status') || '—' },
              { header: 'Created time', render: (r) => <span className="text-aws-muted">{str(r, 'created_at').slice(0, 19).replace('T', ' ') || '—'}</span> },
              { header: 'Description', render: () => <span title="CloudForge does not store a stack description">Not available</span> },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" onClick={() => setSelected(String(r.id))}>{selected === String(r.id) ? 'Selected' : 'Detail'}</AwsButton>
                    <AwsButton variant="secondary" onClick={() => del.mutate(String(r.id))}>Delete</AwsButton>
                  </span>
                ),
              },
            ]}
          />
        )}
      </AwsCard>

      {selected !== '' && selStack && (
        <AwsCard title={`Stack detail — ${str(selStack, 'name')} (${str(selStack, 'status')})`}>
          <h3 className="mb-2 text-sm font-semibold">Resources ({selResources.length})</h3>
          <AwsTable<Rec>
            caption="Stack resources"
            emptyMessage="No resources recorded for this stack."
            rows={selResources}
            columns={[
              { header: 'Logical ID', render: (r) => <span className="font-mono">{str(r, 'logical_id')}</span> },
              { header: 'Type', render: (r) => <span className="font-mono text-xs">{str(r, 'type')}</span> },
              { header: 'Physical ID', render: (r) => <span className="font-mono text-xs">{str(r, 'physical_id')}</span> },
            ]}
          />
          <h3 className="mb-2 mt-4 text-sm font-semibold">Events ({eventRows.length})</h3>
          {events.isPending && <Loading label="Loading events" />}
          {events.data && (
            <AwsTable<Rec>
              caption="Stack events"
              emptyMessage="No events yet."
              rows={eventRows}
              columns={[
                { header: 'Time', render: (r) => <span className="text-aws-muted">{str(r, 'created_at').slice(0, 19).replace('T', ' ') || '—'}</span> },
                { header: 'Status', render: (r) => str(r, 'status') || '—' },
                { header: 'Resource', render: (r) => <span className="font-mono">{str(r, 'logical_id')}</span> },
                { header: 'Reason', render: (r) => <span className="text-xs">{str(r, 'reason') || '—'}</span> },
              ]}
            />
          )}
          <h3 className="mb-2 mt-4 text-sm font-semibold">Create change set</h3>
          <form
            className="space-y-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (csName.trim() !== '' && csTemplate.trim() !== '') createChangeset.mutate()
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor="cfn-cs-name" className="text-sm font-medium">Change set name</label>
              <input id="cfn-cs-name" value={csName} onChange={(e) => setCsName(e.target.value)} placeholder="add-queue" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
              <span className="text-xs text-aws-muted">Execute a change set from the stack detail API — the console shows the diff; execution is via the change-set execute endpoint.</span>
            </div>
            <textarea
              value={csTemplate}
              onChange={(e) => setCsTemplate(e.target.value)}
              rows={6}
              placeholder="Paste the updated template (JSON or YAML) here."
              className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
            />
            <AwsButton type="submit" disabled={createChangeset.isPending}>Create change set</AwsButton>
          </form>
        </AwsCard>
      )}
    </div>
  )
}
