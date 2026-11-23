import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { AwsActionsMenu, AwsPreferencesButton, AwsRefreshButton, AwsToolbar, AwsWizard } from '../components/AwsShell.tsx'
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
 *
 * Create-stack wizard steps follow the recorded AWS backup
 * (docs/reference/aws-console-backup/cloudformation.md): 1. Prepare
 * template, 2. Specify stack details, 3. Configure stack options,
 * 4. Review and create. Validation runs on step 1. Stack options
 * (tags, IAM role, stack policy, rollback configuration,
 * notifications) are NOT stored — the teaching API takes only a
 * name and a template. Stack detail has tabs: Resources, Events,
 * Template, Change sets. The Template tab fetches the single-stack
 * endpoint (the list view strips the template) and shows the
 * template only if the endpoint returns one.
 */
export function CloudFormationConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [template, setTemplate] = useState(DEFAULT_TEMPLATE)
  const [selected, setSelected] = useState('')
  const [csName, setCsName] = useState('')
  const [csTemplate, setCsTemplate] = useState('')
  const [validateResult, setValidateResult] = useState<Rec | null>(null)
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'stacks')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [stackFilter, setStackFilter] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [detailTab, setDetailTab] = useState<'resources' | 'events' | 'template' | 'changesets'>('resources')

  const stacks = useQuery({ queryKey: ['cfn-stacks'], queryFn: () => request<Rec>('/api/v1/aws/cloudformation/stacks'), retry: 1 })
  const events = useQuery({
    queryKey: ['cfn-events', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/cloudformation/stacks/${selected}/events`),
    enabled: selected !== '',
    retry: 1,
  })
  const stackRows = listOf(stacks.data, 'stacks')
  const eventRows = listOf(events.data, 'events')
  const stackDetail = useQuery({
    queryKey: ['cfn-stack', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/cloudformation/stacks/${selected}`),
    enabled: selected !== '',
    retry: 1,
  })
  const filteredStacks = stackRows.filter(
    (r) => stackFilter.trim() === '' || str(r, 'name').toLowerCase().includes(stackFilter.toLowerCase()),
  )
  const checkedStack = stackRows.find((r) => String(r.id) === checked[0])
  const detailTemplate = typeof stackDetail.data?.template === 'string' ? (stackDetail.data.template as string) : ''
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
      setWizardOpen(false)
      setWizardStep(0)
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

  const wizard = wizardOpen ? (
    <AwsWizard
      title="Create stack"
      steps={['Prepare template', 'Stack details', 'Configure options', 'Review']}
      current={wizardStep}
      onStep={setWizardStep}
      onCancel={() => { setWizardOpen(false); setWizardStep(0) }}
      onSubmit={() => {
        if (name.trim() === '' || template.trim() === '') {
          setNotice('Enter a stack name and a template first.')
          setWizardStep(name.trim() === '' ? 1 : 0)
          return
        }
        create.mutate()
      }}
      submitLabel="Create stack"
      busy={create.isPending}
    >
      {wizardStep === 0 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cfn-wiz-template" className="block font-medium">Template (JSON or YAML)</label>
          <textarea id="cfn-wiz-template" value={template} onChange={(e) => setTemplate(e.target.value)} rows={10} className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          <div className="flex items-center gap-2">
            <AwsButton variant="secondary" disabled={validate.isPending} onClick={() => validate.mutate()}>Validate template</AwsButton>
            {validateResult && (
              <p role="status">
                Template {validateResult.valid === true ? 'is valid' : 'is NOT valid'} — {String(validateResult.resource_count ?? 0)} resource(s).
                {Array.isArray(validateResult.problems) && (validateResult.problems as unknown[]).length > 0 && (
                  <span className="text-aws-error"> {(validateResult.problems as unknown[]).map((p) => String(p)).join('; ')}</span>
                )}
                {Array.isArray(validateResult.supported_types) && (
                  <span className="text-aws-muted"> Supported: {(validateResult.supported_types as string[]).join(', ')}</span>
                )}
              </p>
            )}
          </div>
          <p className="text-aws-muted">Paste a template, or use the sample (SQS queue + SNS topic). No S3 URL, file upload or Designer — the template is pasted as text.</p>
        </div>
      )}
      {wizardStep === 1 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="cfn-wiz-name" className="block font-medium">Stack name</label>
          <input id="cfn-wiz-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="demo" className="w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <p className="text-aws-muted">No parameters are collected — the template is applied as written. Six resource types provision for real (SQS, SNS, DynamoDB, Lambda, S3, RDS); any other type is reported UNSUPPORTED in the change set.</p>
        </div>
      )}
      {wizardStep === 2 && (
        <div className="space-y-3 text-sm">
          <p>No stack options are stored in CloudForge — tags, IAM role, stack policy, rollback configuration and notification options are not modelled. The stack is created with the template as written, and failed creates roll back the resources they made.</p>
        </div>
      )}
      {wizardStep === 3 && (
        <div className="space-y-3 text-sm">
          <dl className="grid grid-cols-2 gap-2">
            <dt className="font-medium">Stack name</dt><dd>{name.trim() === '' ? '— (enter a name on step 2)' : name.trim()}</dd>
            <dt className="font-medium">Template</dt><dd>{template.trim().length} characters{validateResult ? ` — ${validateResult.valid === true ? 'valid' : 'NOT valid'}` : ' (not validated yet)'}</dd>
            <dt className="font-medium">Stack options</dt><dd>None stored</dd>
          </dl>
          <p className="text-aws-muted">This is a separate template engine with its own stack state — not Terraform.</p>
        </div>
      )}
    </AwsWizard>
  ) : null

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Management & Governance', to: '/services' }, { label: 'CloudFormation' }]}
      title="CloudFormation — Stacks"
      description="Infrastructure as code with its own template engine and stack state — not Terraform. Six resource types provision for real; anything else is UNSUPPORTED in the change set."
      fidelity="PARTIAL"
      fidelityFootnote="6 resource types · rollback on failure"
      subNav={[
        { id: 'home', label: 'CloudFormation dashboard' },
        { id: 'stacks', label: 'Stacks' },
        { id: 'stack-refactors', label: 'Stack refactors' },
        { id: 'stacksets', label: 'StackSets' },
        { id: 'exports', label: 'Exports' },
        { id: 'iac-generator', label: 'IaC generator' },
        { id: 'template-studio', label: 'Template studio' },
        { id: 'hooks-overview', label: 'Hooks overview' },
        { id: 'invocation-summary', label: 'Invocation summary' },
        { id: 'hooks', label: 'Hooks' },
        { id: 'registry-public', label: 'Public extensions', group: 'Registry' },
        { id: 'registry-activated', label: 'Activated extensions', group: 'Registry' },
        { id: 'registry-publisher', label: 'Publisher', group: 'Registry' },
        { id: 'registry-spotlight', label: 'Spotlight', group: 'Registry' },
        { id: 'feedback', label: 'Feedback' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="CloudFormation"
            banner="Infrastructure as code with its own template engine and stack state — not Terraform. Six resource types provision for real; anything else is UNSUPPORTED in the change set."
            actions={[
              { label: 'Create stack', primary: true, onClick: () => { setSection('stacks'); setWizardOpen(true); setWizardStep(0) } },
            { label: 'View stacks', onClick: () => setSection('stacks') },
            ]}
            cards={[
              { label: 'Stacks', count: stackRows.length, onSelect: () => setSection('stacks') },
            { label: 'StackSets', count: 0, disabledReason: 'StackSets are not built in CloudForge.' },
            { label: 'Template studio', count: 0, disabledReason: 'No visual template editor — the Template tab is a text view.' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={stacks.isFetching}
            health={[
              stacks.isError ? 'Stacks could not be loaded.' : `${stackRows.length} stack(s) listed.`,
            "Failed creates roll back the resources they made; resources deleted by an update are not restored.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — terraform track', to: '/learn' },
            { label: 'Terraform Learning (a different engine)', to: '/learn/terraform' },
            ]}
          />
      )}
      {section === 'stacks' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {wizard}
      <AwsCard title={`Stacks (${stackRows.length})`}>
        <div className="mb-3">
          <AwsButton variant="primary" onClick={() => { setWizardOpen(true); setWizardStep(0) }}>Create stack</AwsButton>
        </div>

        {stacks.isPending && <Loading label="Loading stacks" />}
        {stacks.isError && <ErrorBox message={`Stacks could not be loaded (${stacks.error.message}).`} />}
        {stacks.data && (
          <>
          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={stacks.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          >
            <AwsActionsMenu
              items={[
                { label: 'Update stack', disabledReason: checkedStack === undefined ? 'Select a stack first.' : 'No update-stack API exists — create a change set from the stack detail instead.' },
                {
                  label: 'Delete stack',
                  disabledReason: checkedStack === undefined ? 'Select a stack first.' : undefined,
                  onClick: () => { if (checkedStack) del.mutate(String(checkedStack.id)) },
                },
                { label: 'Detect drift', disabledReason: checkedStack === undefined ? 'Select a stack first.' : 'Drift detection is not implemented — there is no drift API.' },
              ]}
            />
          </AwsToolbar>
          <AwsTable<Rec>
            caption="CloudFormation stacks"
            emptyMessage="No stacks yet. Create one above."
            rows={filteredStacks}
            totalCount={stackRows.length}
            filter={stackFilter}
            onFilterChange={setStackFilter}
            filterPlaceholder="Filter stacks by name"
            selectable
            selectedIds={checked}
            onToggleRow={(id) => {
              setSelected(id)
              setDetailTab('resources')
              setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [id]))
            }}
            onToggleAll={(all) => setChecked(all ? filteredStacks.map((r) => r.id) : [])}
            columns={[
              { header: 'Stack name', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelected(String(r.id)); setDetailTab('resources') }}><span className="font-mono">{str(r, 'name')}</span></button> },
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
          </>
        )}
      </AwsCard>

      {selected !== '' && selStack && (
        <AwsCard title={`Stack detail — ${str(selStack, 'name')} (${str(selStack, 'status')})`}>
          <ConsoleTabs
            tabs={[
              { id: 'resources', label: 'Resources' },
              { id: 'events', label: 'Events' },
              { id: 'template', label: 'Template' },
              { id: 'changesets', label: 'Change sets' },
            ]}
            active={detailTab}
            onChange={(id) => setDetailTab(id as typeof detailTab)}
          />
          <div className="mt-3">
          {detailTab === 'resources' && (
          <>
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
          </>
          )}
          {detailTab === 'events' && (
          <>
          <h3 className="mb-2 text-sm font-semibold">Events ({eventRows.length})</h3>
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
          </>
          )}
          {detailTab === 'template' && (
            <>
              {stackDetail.isPending && <Loading label="Loading stack template" />}
              {stackDetail.data && detailTemplate !== '' ? (
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-sm border border-aws-border bg-[#f7f7f7] p-3 font-mono text-xs">{detailTemplate}</pre>
              ) : (
                <p className="text-sm text-aws-muted">
                  No template returned for this stack — the single-stack endpoint did not include one. The
                  template used to create the stack is not stored separately.
                </p>
              )}
            </>
          )}
          {detailTab === 'changesets' && (
          <>
          <p className="mb-3 text-sm text-aws-muted">
            A change set previews the diff before an update. There is no change-set list endpoint — create
            one below; execution is via the change-set execute endpoint.
          </p>
          <h3 className="mb-2 text-sm font-semibold">Create change set</h3>
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
          </>
          )}
          </div>
        </AwsCard>
      )}
        </>
      )}
      {section === 'stack-refactors' && (
        <UnsupportedSection
          title="Stack refactors"
          reason="Stack refactors are UNSUPPORTED in CloudForge — there is no refactor API."
          actionLabel="Create stack refactor"
        />
      )}
      {section === 'stacksets' && (
        <UnsupportedSection
          title="StackSets"
          reason="StackSets are UNSUPPORTED in CloudForge — there is no StackSet API; a stack deploys into one lab only."
          actionLabel="Create StackSet"
        />
      )}
      {section === 'exports' && (
        <UnsupportedSection
          title="Exports"
          reason="Exports are UNSUPPORTED in CloudForge — there is no export API and no cross-stack references."
          actionLabel="Create export"
        />
      )}
      {section === 'iac-generator' && (
        <UnsupportedSection
          title="IaC generator"
          reason="The IaC generator is UNSUPPORTED in CloudForge — there is no generator API."
          actionLabel="Generate IaC"
        />
      )}
      {section === 'template-studio' && (
        <UnsupportedSection
          title="Template studio"
          reason="Template studio is UNSUPPORTED in CloudForge — there is no visual template editor; the Template tab is a text view with validation."
          actionLabel="Open Template studio"
        />
      )}
      {section === 'hooks-overview' && (
        <UnsupportedSection
          title="Hooks overview"
          reason="Hooks are UNSUPPORTED in CloudForge — there is no hook API."
          actionLabel="View hooks overview"
        />
      )}
      {section === 'invocation-summary' && (
        <UnsupportedSection
          title="Invocation summary"
          reason="Invocation summaries are UNSUPPORTED in CloudForge — there is no hook API."
          actionLabel="View invocation summary"
        />
      )}
      {section === 'hooks' && (
        <UnsupportedSection
          title="Hooks"
          reason="Hooks are UNSUPPORTED in CloudForge — there is no hook API."
          actionLabel="Create hook"
        />
      )}
      {section === 'registry-public' && (
        <UnsupportedSection
          title="Public extensions — Registry"
          reason="The public extensions registry is UNSUPPORTED in CloudForge — there is no extension registry."
          actionLabel="Browse public extensions"
        />
      )}
      {section === 'registry-activated' && (
        <UnsupportedSection
          title="Activated extensions — Registry"
          reason="Activated extensions are UNSUPPORTED in CloudForge — there is no extension registry."
          actionLabel="Activate extension"
        />
      )}
      {section === 'registry-publisher' && (
        <UnsupportedSection
          title="Publisher — Registry"
          reason="The extension publisher is UNSUPPORTED in CloudForge — there is no extension registry."
          actionLabel="Register as publisher"
        />
      )}
      {section === 'registry-spotlight' && (
        <UnsupportedSection
          title="Spotlight — Registry"
          reason="Spotlight is UNSUPPORTED in CloudForge — there is no extension registry."
          actionLabel="View Spotlight"
        />
      )}
      {section === 'feedback' && (
        <UnsupportedSection
          title="Feedback"
          reason="Feedback is UNSUPPORTED in CloudForge — there is no feedback channel in the console."
          actionLabel="Send feedback"
        />
      )}
    </ServiceConsole>
  )
}
