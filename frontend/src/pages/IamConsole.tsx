import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge, StateBadge } from '../components/AwsBadge.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { AwsCopy } from '../components/AwsShell.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import {
  addIamGroupMember,
  assumeIamRole,
  attachIamPolicy,
  createIamAccessKey,
  createIamGroup,
  createIamPolicy,
  createIamRole,
  createIamUser,
  deleteIamAccessKey,
  deleteIamGroup,
  deleteIamPolicy,
  deleteIamRole,
  deleteIamUser,
  detachIamPolicy,
  evaluateIamPolicy,
  fetchIamAccessKeys,
  fetchIamGroups,
  fetchIamPolicies,
  fetchIamRoles,
  fetchIamUsers,
  removeIamGroupMember,
  setIamAccessKeyStatus,
} from '../services/api.ts'
import type {
  IamAccessKeyCreated,
  IamGroup,
  IamPolicy,
  IamRole,
  IamSessionCreated,
  IamUser,
} from '../types/index.ts'
import {
  CLOUDFORGE_MANAGED_POLICIES,
  POLICY_SERVICES,
  SAMPLE_VISUAL_STATEMENTS,
  blankStatement,
  documentToVisual,
  groupServiceActions,
  lintPolicyDocument,
  serviceById,
  summarizePolicyDocument,
  visualToDocument,
  visualToJson,
  visualValidationErrors,
} from '../services/iamPolicyEditor.ts'
import type { ManagedPolicy, PolicySummaryRow, VisualStatement } from '../services/iamPolicyEditor.ts'

/**
 * IAM console (Phase 2) — mirrors the AWS IAM console's left nav:
 * Users / Groups / Roles / Policies / Access Analyzer / Policy
 * Simulator. Users, Groups, Roles and Policies are real CRUD against
 * the CloudForge teaching IAM store (Postgres, migration 008;
 * admin-only). Access keys show their secret exactly once (only a
 * SHA-256 hash is stored). Assume Role returns a short-lived session
 * whose expiry the backend enforces (410 when expired). The Policy
 * Simulator resolves stored identities and shows the full evaluation
 * trail: explicit deny / allow / default deny and which statement
 * decided it. Access Analyzer is still not built (UNSUPPORTED).
 *
 * CloudForge teaching IAM identities are NOT AWS IAM and NOT the
 * platform users (learner/instructor/admin) — the page says so.
 *
 * v4: the policy editor has access-level action groups with counts,
 * All/Specific resources with ARN entry, MFA/source-IP conditions
 * (labels only — the backend evaluates only StringEquals on
 * aws:username), a basic client-side linter, import from the built-in
 * CloudForge sample policies, and a Specify/Review two-step flow with
 * a permissions summary and tags (entered, never saved). Policies
 * have a detail page with Permissions (Summary/JSON), Entities
 * attached, Policy versions and Last Accessed tabs.
 */
const SAMPLE_POLICY = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "s3:ListBuckets",
      "Resource": "*"
    },
    {
      "Effect": "Deny",
      "Action": "s3:DeleteBucket",
      "Resource": "*"
    }
  ]
}`

const SAMPLE_TRUST = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "AWS": "arn:cloudforge:iam::local:user/alice" },
      "Action": "sts:AssumeRole",
      "Resource": "*"
    }
  ]
}`

type Section = 'dashboard' | 'users' | 'groups' | 'roles' | 'policies' | 'analyzer' | 'simulator'

export function IamConsole({ initialSection }: { initialSection?: Section }) {
  // Survey 2026-11-22: IAM lands on Roles, not a dashboard.
  const [section, setSection] = useState<Section>(initialSection ?? 'dashboard')
  return (
    <ServiceConsole
      icon="key"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Security and Identity', to: '/services' }, { label: 'IAM' }]}
      title="IAM"
      description="Teaching IAM identities (users, groups, roles, policies) stored in CloudForge — not AWS IAM, and not the platform's learner/instructor/admin roles. Policy documents are validated and probe-evaluated before storage; evaluation is a real subset: Allow/Deny, wildcards, explicit-deny-wins, StringEquals on aws:username — no ARNs, principals, policy variables, or other condition operators."
      fidelity="PARTIAL"
      fidelityFootnote="teaching IAM · API_EMULATED · analyzer UNSUPPORTED"
      subNav={[
        { id: 'dashboard', label: 'Dashboard' },
        { id: 'users', label: 'Users' },
        { id: 'groups', label: 'Groups' },
        { id: 'roles', label: 'Roles' },
        { id: 'policies', label: 'Policies' },
        { id: 'analyzer', label: 'Access Analyzer', disabledReason: 'Not built yet.' },
        { id: 'simulator', label: 'Policy Simulator' },
      ]}
      activeSub={section}
      onSubChange={(id) => setSection(id as Section)}
      actions={
        <AwsButton variant="primary" onClick={() => setSection('users')}>
          Add users
        </AwsButton>
      }
    >
      {section === 'dashboard' && <DashboardSection onNavigate={setSection} />}
      {section === 'users' && <UsersSection />}
      {section === 'groups' && <GroupsSection />}
      {section === 'roles' && <RolesSection />}
      {section === 'policies' && <PoliciesSection />}
      {section === 'analyzer' && (
        <AwsCard title="Access Analyzer">
          <p className="text-sm text-aws-muted">
            Access Analyzer is <strong>UNSUPPORTED</strong> — no analyzer API exists. It is shown as unavailable,
            never as working. Use the Policy Simulator to evaluate what a stored identity can do.
          </p>
        </AwsCard>
      )}
      {section === 'simulator' && <SimulatorSection />}
    </ServiceConsole>
  )
}

// --- shared bits -------------------------------------------------------------

function useIamData() {
  const users = useQuery({ queryKey: ['iam-users'], queryFn: fetchIamUsers, retry: 0 })
  const groups = useQuery({ queryKey: ['iam-groups'], queryFn: fetchIamGroups, retry: 0 })
  const roles = useQuery({ queryKey: ['iam-roles'], queryFn: fetchIamRoles, retry: 0 })
  const policies = useQuery({ queryKey: ['iam-policies'], queryFn: fetchIamPolicies, retry: 0 })
  return { users, groups, roles, policies }
}

function nameOf(list: { id: string; name: string }[] | undefined, id: string): string {
  return list?.find((x) => x.id === id)?.name ?? id
}

function AdminOnly({ error }: { error: Error | null }) {
  if (!error) return null
  return (
    <ErrorBox message={`IAM management is admin-only (${error.message}). Sign in as an admin; other roles see this page as unavailable, not as an empty list.`} />
  )
}

function DashboardSection({ onNavigate }: { onNavigate: (s: Section) => void }) {
  const { users, groups, roles, policies } = useIamData()
  const error = users.error ?? groups.error ?? roles.error ?? policies.error ?? null
  const count = (q: { data?: unknown[] }) => (q.data === undefined ? '—' : q.data.length)
  return (
    <div className="space-y-4">
      {error && <AdminOnly error={error} />}
      <ServiceHomeDashboard
        serviceName="IAM"
        banner="Teaching IAM identities (users, groups, roles, policies) stored in CloudForge — not AWS IAM, and not the platform's learner/instructor/admin roles."
        actions={[
          { label: 'Add users', primary: true, onClick: () => onNavigate('users') },
          { label: 'Open Policy Simulator', onClick: () => onNavigate('simulator') },
        ]}
        cards={[
          { label: 'Users', count: count(users), onSelect: () => onNavigate('users'), note: 'Teaching IAM identities' },
          { label: 'Groups', count: count(groups), onSelect: () => onNavigate('groups'), note: 'Policy bundles' },
          { label: 'Roles', count: count(roles), onSelect: () => onNavigate('roles'), note: 'Assumable locally' },
          { label: 'Policies', count: count(policies), onSelect: () => onNavigate('policies'), note: 'Validated documents' },
          { label: 'Access Analyzer', count: 0, disabledReason: 'UNSUPPORTED — no analyzer API exists.' },
        ]}
        health={[
          error !== null
            ? 'IAM management is admin-only; other roles see this page as unavailable, not as an empty list.'
            : 'Policy documents are validated and probe-evaluated before storage.',
          'Access keys show their secret once; only a SHA-256 hash is stored.',
          'Assume Role sessions are short-lived and expire with a real 410 from the backend.',
        ]}
        settings={[
          { label: 'Resource Explorer', to: '/explorer' },
          { label: 'All services', to: '/services' },
        ]}
        info={[
          { label: 'Learning Center — IAM track', to: '/learn' },
          { label: 'IAM Identity Center (unsupported — exam concept)', to: '/services/identitycenter' },
        ]}
      />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <AwsCard title="Security recommendations">
          <p className="mb-2 text-sm text-aws-muted">
            These are static notes for this teaching store, not an AWS security score. CloudForge never reports a
            fabricated "secure" status.
          </p>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            <li>Access Analyzer is <strong>UNSUPPORTED</strong> — no analyzer findings exist.</li>
            <li>Access keys show their secret once; only a SHA-256 hash is stored.</li>
            <li>Assume Role sessions are short-lived and expire with a real 410 from the backend.</li>
            <li>Policy evaluation is a documented subset: wildcards, explicit-deny-wins and StringEquals on aws:username only.</li>
          </ul>
        </AwsCard>
        <AwsCard title="Access Analyzer">
          <p className="text-sm text-aws-muted">
            Access Analyzer is not built. It is shown as unavailable rather than as a working feature. Use the Policy
            Simulator to evaluate what a stored identity can do.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <AwsButton onClick={() => onNavigate('simulator')}>Open Policy Simulator</AwsButton>
            <AwsButton onClick={() => onNavigate('analyzer')}>Open Access Analyzer</AwsButton>
          </div>
        </AwsCard>
      </div>
      <AwsCard title="IAM resources">
        <div className="flex flex-wrap gap-2">
          <AwsButton onClick={() => onNavigate('users')}>Users</AwsButton>
          <AwsButton onClick={() => onNavigate('groups')}>User groups</AwsButton>
          <AwsButton onClick={() => onNavigate('roles')}>Roles</AwsButton>
          <AwsButton onClick={() => onNavigate('policies')}>Policies</AwsButton>
        </div>
        <p className="mt-3 text-sm text-aws-muted">
          CloudForge teaching IAM identities are not AWS IAM and not the platform's learner/instructor/admin roles.
        </p>
      </AwsCard>
    </div>
  )
}

function PolicyAttach({ kind, id, policies }: { kind: 'user' | 'group' | 'role'; id: string; policies: IamPolicy[] }) {
  const queryClient = useQueryClient()
  const [policyId, setPolicyId] = useState('')
  const [notice, setNotice] = useState('')
  const attach = useMutation({
    mutationFn: () => attachIamPolicy(kind, id, policyId),
    onSuccess: () => {
      setNotice('Policy attached.')
      setPolicyId('')
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => setNotice(`Attach failed: ${e.message}`),
  })
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <select
        value={policyId}
        onChange={(e) => setPolicyId(e.target.value)}
        aria-label="Policy to attach"
        className="rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm"
      >
        <option value="">Attach a policy…</option>
        {policies.map((p) => (
          <option key={p.id} value={p.id}>{p.name}</option>
        ))}
      </select>
      <AwsButton disabled={policyId === ''} onClick={() => attach.mutate()}>Attach policy</AwsButton>
      {notice !== '' && <span className="text-sm" role="status">{notice}</span>}
    </div>
  )
}

function AttachedPolicies({ kind, id, policyIds, policies }: { kind: 'user' | 'group' | 'role'; id: string; policyIds?: string[]; policies: IamPolicy[] }) {
  const queryClient = useQueryClient()
  const [notice, setNotice] = useState('')
  const detach = useMutation({
    mutationFn: (policyId: string) => detachIamPolicy(kind, id, policyId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => setNotice(`Detach failed: ${e.message}`),
  })
  if (!policyIds || policyIds.length === 0) return <p className="text-sm text-aws-muted">No policies attached.</p>
  return (
    <ul className="space-y-1 text-sm">
      {policyIds.map((pid) => (
        <li key={pid} className="flex items-center gap-2">
          <span>{nameOf(policies, pid)}</span>
          <button type="button" className="text-aws-link hover:underline" onClick={() => detach.mutate(pid)}>Detach</button>
        </li>
      ))}
      {notice !== '' && <li role="status">{notice}</li>}
    </ul>
  )
}

// --- Users -------------------------------------------------------------------

function UsersSection() {
  const queryClient = useQueryClient()
  const { users, policies } = useIamData()
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: (n: string) => createIamUser(n),
    onSuccess: (u) => {
      setNotice(`User "${u.name}" created.`)
      setName('')
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
    },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => deleteIamUser(id),
    onSuccess: () => {
      setNotice('User deleted.')
      setSelectedId(null)
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const selected = users.data?.find((u) => u.id === selectedId) ?? null
  return (
    <div className="space-y-4">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title="Create user" actions={<FidelityBadge fidelity="API_EMULATED" footnote="teaching IAM, admin-only" />}>
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '') create.mutate(name.trim())
          }}
        >
          <label htmlFor="iam-user-name" className="text-sm font-medium">User name</label>
          <input
            id="iam-user-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="alice"
            className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
          />
          <AwsButton type="submit" disabled={name.trim() === ''}>Create user</AwsButton>
          <span className="text-sm text-aws-muted">Names: 1–64 chars, letters/digits and +=,.@-_</span>
        </form>
      </AwsCard>
      {users.isPending && <Loading label="Loading IAM users" />}
      <AdminOnly error={users.isError ? users.error : null} />
      {users.data && (
        <AwsCard title={`Users (${users.data.length})`}>
          <AwsTable<IamUser & { id: string }>
            caption="IAM users"
            emptyMessage="No IAM users yet — create one above."
            rows={users.data}
            totalCount={users.data.length}
            columns={[
              // IAM users columns in the v4 survey's exact order (9).
              // Real values: User name, Path, ARN, Creation time. Last
              // activity, MFA and the access-key columns have no backend
              // source on the list — keys are listed on the user detail
              // page, and CloudForge records no MFA enrolment or
              // last-activity data — so they render '—' / 'Not available',
              // never an invented value.
              { header: 'User name', render: (u) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(u.id)}>{u.name}</button> },
              { header: 'Path', render: (u) => u.path || '—' },
              { header: 'Last activity', render: () => <span title="CloudForge does not record user activity">Not available</span> },
              { header: 'MFA', render: () => '—' },
              { header: 'Access key ID', render: () => '—' },
              { header: 'Active key age', render: () => '—' },
              { header: 'Access key last used', render: () => '—' },
              { header: 'ARN', render: (u) => <span className="break-all font-mono text-xs">{u.arn}</span> },
              { header: 'Creation time', render: (u) => (u.created_at ? new Date(u.created_at).toLocaleString() : '—') },
            ]}
          />
          <p className="mt-2 text-xs text-aws-muted">
            Access key ID, Active key age and Access key last used show &lsquo;—&rsquo; on the list — access keys are
            listed on the user detail page. MFA shows &lsquo;—&rsquo;: CloudForge records no MFA enrolment.
          </p>
        </AwsCard>
      )}
      {selected && (
        <UserDetail
          user={selected}
          policies={policies.data ?? []}
          onDelete={() => del.mutate(selected.id)}
        />
      )}
    </div>
  )
}

function UserDetail({ user, policies, onDelete }: { user: IamUser; policies: IamPolicy[]; onDelete: () => void }) {
  const queryClient = useQueryClient()
  const [created, setCreated] = useState<IamAccessKeyCreated | null>(null)
  const [notice, setNotice] = useState('')
  const keys = useQuery({ queryKey: ['iam-keys', user.id], queryFn: () => fetchIamAccessKeys(user.id), retry: 0 })
  const createKey = useMutation({
    mutationFn: () => createIamAccessKey(user.id),
    onSuccess: (c) => {
      setCreated(c)
      void queryClient.invalidateQueries({ queryKey: ['iam-keys', user.id] })
    },
    onError: (e: Error) => setNotice(`Create key failed: ${e.message}`),
  })
  const revoke = useMutation({
    mutationFn: (keyId: string) => deleteIamAccessKey(user.id, keyId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['iam-keys', user.id] }),
    onError: (e: Error) => setNotice(`Revoke failed: ${e.message}`),
  })
  const toggle = useMutation({
    mutationFn: (k: { id: string; status: string }) => setIamAccessKeyStatus(user.id, k.id, k.status === 'Active' ? 'Inactive' : 'Active'),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['iam-keys', user.id] }),
    onError: (e: Error) => setNotice(`Status change failed: ${e.message}`),
  })
  return (
    <AwsCard title={`User ${user.name}`}>
      <dl className="space-y-1 text-sm">
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">ARN</dt><dd className="break-all font-mono text-xs">{user.arn}</dd></div>
        <div className="flex gap-2"><dt className="w-24 shrink-0 font-medium">Groups</dt><dd>{user.groups?.length ? user.groups.join(', ') : '—'}</dd></div>
      </dl>
      <h3 className="mt-4 text-sm font-semibold">Attached policies</h3>
      <AttachedPolicies kind="user" id={user.id} policyIds={user.policies} policies={policies} />
      <PolicyAttach kind="user" id={user.id} policies={policies} />
      <h3 className="mt-4 text-sm font-semibold">Access keys</h3>
      {notice !== '' && <p className="text-sm" role="status">{notice}</p>}
      {keys.isPending && <Loading label="Loading access keys" />}
      {keys.data && (
        <AwsTable<{ id: string; status: string; created_at: string; last_used_at?: string }>
          caption="Access keys"
          emptyMessage="No access keys yet."
          rows={keys.data}
          totalCount={keys.data.length}
          columns={[
            { header: 'Access key ID', render: (k) => <span className="font-mono text-xs">{k.id}</span> },
            { header: 'Status', render: (k) => <StateBadge state={k.status === 'Active' ? 'READY' : 'STOPPED'} /> },
            { header: 'Created', render: (k) => (k.created_at ? new Date(k.created_at).toLocaleString() : '—') },
            {
              header: 'Actions',
              render: (k) => (
                <span className="flex gap-2">
                  <button type="button" className="text-aws-link hover:underline" onClick={() => toggle.mutate(k)}>{k.status === 'Active' ? 'Deactivate' : 'Activate'}</button>
                  <button type="button" className="text-aws-link hover:underline" onClick={() => revoke.mutate(k.id)}>Revoke</button>
                </span>
              ),
            },
          ]}
        />
      )}
      <div className="mt-2 flex flex-wrap gap-2">
        <AwsButton onClick={() => createKey.mutate()}>Create access key</AwsButton>
        <AwsButton onClick={onDelete}>Delete user</AwsButton>
      </div>
      {created && (
        <div className="mt-3 border border-aws-orange bg-[#fff8f0] px-3 py-2 text-sm" role="alert">
          <p className="font-semibold">Access key created — copy the secret now.</p>
          <p>Access key ID: <code className="font-mono text-xs">{created.access_key.id}</code></p>
          <p>Secret access key: <code className="font-mono text-xs">{created.secret}</code></p>
          <p className="text-aws-muted">{created.note ?? 'The secret is shown only once; only its SHA-256 hash is stored.'}</p>
        </div>
      )}
    </AwsCard>
  )
}

// --- Groups ------------------------------------------------------------------

function GroupsSection() {
  const queryClient = useQueryClient()
  const { users, groups, policies } = useIamData()
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: (n: string) => createIamGroup(n),
    onSuccess: () => {
      setNotice('Group created.')
      setName('')
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
    },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => deleteIamGroup(id),
    onSuccess: () => {
      setNotice('Group deleted.')
      setSelectedId(null)
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const selected = groups.data?.find((g) => g.id === selectedId) ?? null
  return (
    <div className="space-y-4">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title="Create group">
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '') create.mutate(name.trim())
          }}
        >
          <label htmlFor="iam-group-name" className="text-sm font-medium">Group name</label>
          <input id="iam-group-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="devs" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <AwsButton type="submit" disabled={name.trim() === ''}>Create group</AwsButton>
        </form>
      </AwsCard>
      {groups.isPending && <Loading label="Loading IAM groups" />}
      <AdminOnly error={groups.isError ? groups.error : null} />
      {groups.data && (
        <AwsCard title={`Groups (${groups.data.length})`}>
          <AwsTable<IamGroup & { id: string }>
            caption="IAM groups"
            emptyMessage="No IAM groups yet — create one above."
            rows={groups.data}
            totalCount={groups.data.length}
            columns={[
              { header: 'Group name', render: (g) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(g.id)}>{g.name}</button> },
              { header: 'ARN', render: (g) => <span className="break-all font-mono text-xs">{g.arn}</span> },
              { header: 'Members', render: (g) => String(g.members?.length ?? 0) },
              { header: 'Policies', render: (g) => String(g.policies?.length ?? 0) },
            ]}
          />
        </AwsCard>
      )}
      {selected && (
        <GroupDetail
          group={selected}
          users={users.data ?? []}
          policies={policies.data ?? []}
          onDelete={() => del.mutate(selected.id)}
        />
      )}
    </div>
  )
}

function GroupDetail({ group, users, policies, onDelete }: { group: IamGroup; users: IamUser[]; policies: IamPolicy[]; onDelete: () => void }) {
  const queryClient = useQueryClient()
  const [memberId, setMemberId] = useState('')
  const [notice, setNotice] = useState('')
  const add = useMutation({
    mutationFn: () => addIamGroupMember(group.id, memberId),
    onSuccess: () => {
      setNotice('Member added.')
      setMemberId('')
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
    },
    onError: (e: Error) => setNotice(`Add member failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (userId: string) => removeIamGroupMember(group.id, userId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
    },
    onError: (e: Error) => setNotice(`Remove member failed: ${e.message}`),
  })
  return (
    <AwsCard title={`Group ${group.name}`}>
      <h3 className="text-sm font-semibold">Members</h3>
      {notice !== '' && <p className="text-sm" role="status">{notice}</p>}
      {group.members?.length ? (
        <ul className="space-y-1 text-sm">
          {group.members.map((uid) => (
            <li key={uid} className="flex items-center gap-2">
              <span>{nameOf(users, uid)}</span>
              <button type="button" className="text-aws-link hover:underline" onClick={() => remove.mutate(uid)}>Remove</button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-aws-muted">No members yet.</p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <select value={memberId} onChange={(e) => setMemberId(e.target.value)} aria-label="User to add" className="rounded-sm border border-aws-border-dark bg-white px-2 py-1 text-sm">
          <option value="">Add a user…</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>{u.name}</option>
          ))}
        </select>
        <AwsButton disabled={memberId === ''} onClick={() => add.mutate()}>Add member</AwsButton>
      </div>
      <h3 className="mt-4 text-sm font-semibold">Attached policies</h3>
      <AttachedPolicies kind="group" id={group.id} policyIds={group.policies} policies={policies} />
      <PolicyAttach kind="group" id={group.id} policies={policies} />
      <div className="mt-3">
        <AwsButton onClick={onDelete}>Delete group</AwsButton>
      </div>
    </AwsCard>
  )
}

// --- Roles -------------------------------------------------------------------

function RolesSection() {
  const queryClient = useQueryClient()
  const { users, groups, roles, policies } = useIamData()
  const [name, setName] = useState('')
  const [trust, setTrust] = useState(SAMPLE_TRUST)
  const [maxSecs, setMaxSecs] = useState(3600)
  const [notice, setNotice] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () => {
      let parsed: unknown
      try {
        parsed = JSON.parse(trust)
      } catch {
        throw new Error('Trust policy is not valid JSON.')
      }
      return createIamRole(name.trim(), parsed, maxSecs)
    },
    onSuccess: () => {
      setNotice('Role created.')
      setName('')
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => deleteIamRole(id),
    onSuccess: () => {
      setNotice('Role deleted.')
      setSelectedId(null)
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const selected = roles.data?.find((r) => r.id === selectedId) ?? null
  return (
    <div className="space-y-4">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title="Create role">
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="iam-role-name" className="text-sm font-medium">Role name</label>
            <input id="iam-role-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="reader" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Max session (seconds)
              <input type="number" min={60} max={43200} value={maxSecs} onChange={(e) => setMaxSecs(Number(e.target.value))} className="ml-2 w-24 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
          </div>
          <div>
            <label htmlFor="iam-trust" className="mb-1 block text-sm font-medium">
              Trust policy (JSON — must allow sts:AssumeRole; trust is recorded, not evaluated)
            </label>
            <textarea id="iam-trust" value={trust} onChange={(e) => setTrust(e.target.value)} rows={10} className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          </div>
          <AwsButton type="submit" disabled={name.trim() === ''}>Create role</AwsButton>
        </form>
      </AwsCard>
      {roles.isPending && <Loading label="Loading IAM roles" />}
      <AdminOnly error={roles.isError ? roles.error : null} />
      {roles.data && (
        <AwsCard title={`Roles (${roles.data.length})`}>
          <AwsTable<IamRole & { id: string }>
            caption="IAM roles"
            emptyMessage="No IAM roles yet — create one above."
            rows={roles.data}
            totalCount={roles.data.length}
            columns={[
              { header: 'Role name', render: (r) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(r.id)}>{r.name}</button> },
              { header: 'Trusted entities', render: (r) => trustedEntities(r.trust_policy) },
              { header: 'Last activity', render: () => <span title="CloudForge does not record role activity">Not available</span> },
            ]}
          />
        </AwsCard>
      )}
      {selected && (
        <RoleDetail role={selected} users={users.data ?? []} groups={groups.data ?? []} policies={policies.data ?? []} onDelete={() => del.mutate(selected.id)} />
      )}
    </div>
  )
}

/**
 * Trusted entities for a role, read from its stored trust policy
 * (survey 2026-11-22: the Roles table's Trusted entities column).
 * Trust policies are recorded, not evaluated; the principals named
 * in them are shown verbatim. Last activity has no backend source —
 * the table says "Not available" instead of inventing one.
 */
function trustedEntities(trustPolicy: unknown): string {
  try {
    const doc = typeof trustPolicy === 'string' ? JSON.parse(trustPolicy) : trustPolicy
    const statements = (doc as { Statement?: unknown })?.Statement
    const list = Array.isArray(statements) ? statements : [statements]
    const principals: string[] = []
    for (const st of list) {
      const principal = (st as { Principal?: unknown } | null)?.Principal
      if (typeof principal === 'string') principals.push(principal)
      else if (principal !== null && typeof principal === 'object') {
        for (const v of Object.values(principal as Record<string, unknown>)) {
          if (typeof v === 'string') principals.push(v)
          else if (Array.isArray(v)) for (const x of v) if (typeof x === 'string') principals.push(x)
        }
      }
    }
    return principals.length > 0 ? [...new Set(principals)].join(', ') : '—'
  } catch {
    return '—'
  }
}

/**
 * Role detail — the v4 survey OBSERVED these 5 tabs for a real role
 * (AccountFullAccessRole): Permissions, Trust relationships, Tags,
 * Last Accessed, Revoke sessions. Permissions shows the survey's
 * table (Policy name, Type, Attached entities) over the role's real
 * attached policies, with Detach and the existing attach control.
 * Trust relationships shows the stored trust policy (recorded, not
 * evaluated). Tags and Last Accessed have no backend source — the
 * tabs say so instead of inventing data. Revoke sessions holds the
 * Assume-role form: CloudForge has no session-revocation API, so
 * sessions cannot be revoked early — they are short-lived and the
 * backend returns a real 410 on expiry.
 */
function RoleDetail({ role, users, groups, policies, onDelete }: {
  role: IamRole
  users: IamUser[]
  groups: IamGroup[]
  policies: IamPolicy[]
  onDelete: () => void
}) {
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<'permissions' | 'trust' | 'tags' | 'accessed' | 'revoke'>('permissions')
  const [duration, setDuration] = useState(900)
  const [session, setSession] = useState<IamSessionCreated | null>(null)
  const [notice, setNotice] = useState('')
  const assume = useMutation({
    mutationFn: () => assumeIamRole(role.id, duration),
    onSuccess: (s) => {
      setSession(s)
      setNotice('')
    },
    onError: (e: Error) => setNotice(`Assume role failed: ${e.message}`),
  })
  const detach = useMutation({
    mutationFn: (policyId: string) => detachIamPolicy('role', role.id, policyId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => setNotice(`Detach failed: ${e.message}`),
  })
  const attachedEntities = (policyId: string): string => {
    const entities: string[] = [`role ${role.name}`]
    for (const u of users) if (u.policies?.includes(policyId)) entities.push(`user ${u.name}`)
    for (const g of groups) if (g.policies?.includes(policyId)) entities.push(`group ${g.name}`)
    return entities.join(', ')
  }
  const attached = (role.policies ?? [])
    .map((pid) => policies.find((p) => p.id === pid))
    .filter((p): p is IamPolicy => p !== undefined)
  return (
    <AwsCard title={`Role ${role.name}`}>
      <ConsoleTabs
        tabs={[
          { id: 'permissions', label: 'Permissions' },
          { id: 'trust', label: 'Trust relationships' },
          { id: 'tags', label: 'Tags' },
          { id: 'accessed', label: 'Last Accessed' },
          { id: 'revoke', label: 'Revoke sessions' },
        ]}
        active={tab}
        onChange={(id) => setTab(id as typeof tab)}
      />
      <div className="mt-3">
        {tab === 'permissions' && (
          <>
            <AwsTable<IamPolicy & { id: string }>
              caption="Attached policies"
              emptyMessage="No policies attached to this role."
              rows={attached}
              totalCount={attached.length}
              columns={[
                { header: 'Policy name', render: (p) => p.name },
                { header: 'Type', render: () => 'Customer managed' },
                { header: 'Attached entities', render: (p) => attachedEntities(p.id) },
                {
                  header: 'Actions',
                  render: (p) => (
                    <button type="button" className="text-aws-link hover:underline" onClick={() => detach.mutate(p.id)}>
                      Detach
                    </button>
                  ),
                },
              ]}
            />
            <PolicyAttach kind="role" id={role.id} policies={policies} />
          </>
        )}
        {tab === 'trust' && (
          <dl className="space-y-1 text-sm">
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">ARN</dt><dd className="break-all font-mono text-xs">{role.arn}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Max session</dt><dd>{role.max_session_duration_seconds}s</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Trusted entities</dt><dd className="break-all font-mono text-xs">{trustedEntities(role.trust_policy)}</dd></div>
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Trust policy</dt><dd className="break-all font-mono text-xs">{JSON.stringify(role.trust_policy)}</dd></div>
            <p className="pt-2 text-aws-muted">Trust is recorded, not evaluated — the principals named in the policy are shown verbatim.</p>
          </dl>
        )}
        {tab === 'tags' && (
          <p className="text-sm text-aws-muted">
            This role has no tags — CloudForge&apos;s teaching IAM API has no role-tag field, so no tag can be
            stored or listed here.
          </p>
        )}
        {tab === 'accessed' && (
          <p className="text-sm text-aws-muted">
            Last accessed is not recorded — CloudForge records no role usage, so there is no last-activity date
            to show for this role.
          </p>
        )}
        {tab === 'revoke' && (
          <div className="space-y-3 text-sm">
            <p className="text-aws-muted">
              CloudForge has no session-revocation API: a session issued by Assume role cannot be revoked early.
              Sessions are short-lived (up to the role&apos;s max session duration) and the backend enforces
              expiry — an expired session is a 410, never silently extended.
            </p>
            <h3 className="font-semibold">Assume role</h3>
            <p className="text-aws-muted">Returns a short-lived session token (shown once).</p>
            <div className="flex flex-wrap items-center gap-2">
              <label>
                Duration (seconds, max {role.max_session_duration_seconds})
                <input type="number" min={60} max={role.max_session_duration_seconds} value={duration} onChange={(e) => setDuration(Number(e.target.value))} className="ml-2 w-24 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
              </label>
              <AwsButton onClick={() => assume.mutate()}>Assume role</AwsButton>
            </div>
            {session && (
              <div className="border border-aws-orange bg-[#fff8f0] px-3 py-2" role="alert">
                <p className="font-semibold">Session created — copy the token now.</p>
                <p>Session ID: <code className="font-mono text-xs">{session.session_id}</code></p>
                <p>Token: <code className="font-mono text-xs">{session.token}</code></p>
                <p>Expires: {new Date(session.expires_at).toLocaleString()}</p>
                <p className="text-aws-muted">{session.note}</p>
              </div>
            )}
          </div>
        )}
      </div>
      {notice !== '' && <p className="mt-2 text-sm" role="status">{notice}</p>}
      <div className="mt-3">
        <AwsButton onClick={onDelete}>Delete role</AwsButton>
      </div>
    </AwsCard>
  )
}

// --- Policies ----------------------------------------------------------------

function PoliciesSection() {
  const queryClient = useQueryClient()
  const { users, groups, roles, policies } = useIamData()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [tab, setTab] = useState<'visual' | 'json'>('visual')
  const [statements, setStatements] = useState<VisualStatement[]>(() => {
    const parsed = documentToVisual(SAMPLE_POLICY)
    return parsed.statements ?? SAMPLE_VISUAL_STATEMENTS
  })
  const [jsonText, setJsonText] = useState(SAMPLE_POLICY)
  // Reasons the current JSON cannot be shown visually (empty = representable).
  const [visualUnsupported, setVisualUnsupported] = useState<string[]>([])
  const [jsonError, setJsonError] = useState<string | null>(null)
  const [notice, setNotice] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  // v4 — Step 1 Specify permissions / Step 2 Review and create (survey
  // 2026-11-22: AWS has the same two steps on Create policy).
  const [step, setStep] = useState<'specify' | 'review'>('specify')
  const [nextBlocked, setNextBlocked] = useState(false)
  // Review-step tags: entered here, NOT saved — the teaching IAM API
  // has no tag field, and the create notice says so.
  const [tags, setTags] = useState<{ key: string; value: string }[]>([])
  const [tagKey, setTagKey] = useState('')
  const [tagValue, setTagValue] = useState('')
  // Import dialog (built-in CloudForge managed policies only).
  const [importOpen, setImportOpen] = useState(false)
  const [importFilter, setImportFilter] = useState('')
  // MFA / source-IP request conditions, per statement. Labels only:
  // the backend evaluates only StringEquals on aws:username, so these
  // are never written into the policy document.
  const [mfaFlags, setMfaFlags] = useState<boolean[]>(() => SAMPLE_VISUAL_STATEMENTS.map(() => false))
  const [sourceIps, setSourceIps] = useState<string[]>(() => SAMPLE_VISUAL_STATEMENTS.map(() => ''))
  // Access-level group collapse state (shared across statements).
  const [collapsedLevels, setCollapsedLevels] = useState<string[]>([])
  const [linterTab, setLinterTab] = useState<'Security' | 'Errors' | 'Warnings' | 'Suggestions'>('Security')
  const [policyFilter, setPolicyFilter] = useState('')
  const visualErrors = visualValidationErrors(statements)
  const visualDisabled = jsonError !== null || visualUnsupported.length > 0

  const analyseJson = (text: string) => {
    const result = documentToVisual(text)
    setJsonError(result.jsonError)
    setVisualUnsupported(result.unsupported)
    return result
  }
  const switchToJson = () => {
    const generated = visualToJson(statements)
    setJsonText(generated)
    setJsonError(null)
    setVisualUnsupported([])
    setTab('json')
  }
  const switchToVisual = () => {
    const result = analyseJson(jsonText)
    if (result.statements) {
      setStatements(result.statements)
      setTab('visual')
    }
    // Otherwise the JSON stays exactly as typed and the Visual tab
    // stays disabled; the warning names the unsupported feature.
  }
  const updateStatement = (index: number, patch: Partial<VisualStatement>) => {
    setStatements((prev) => prev.map((st, i) => (i === index ? { ...st, ...patch } : st)))
  }
  const addStatement = () => {
    setStatements((prev) => [...prev, blankStatement()])
    setMfaFlags((prev) => [...prev, false])
    setSourceIps((prev) => [...prev, ''])
  }
  const removeStatement = (index: number) => {
    setStatements((prev) => prev.filter((_, j) => j !== index))
    setMfaFlags((prev) => prev.filter((_, j) => j !== index))
    setSourceIps((prev) => prev.filter((_, j) => j !== index))
  }
  // The document as currently edited, for the linter and the review step.
  const currentDoc: unknown = useMemo(() => {
    if (tab === 'visual') return visualToDocument(statements)
    try {
      return JSON.parse(jsonText) as unknown
    } catch {
      return null
    }
  }, [tab, statements, jsonText])
  const findings = useMemo(() => lintPolicyDocument(currentDoc), [currentDoc])
  const summary = useMemo(() => summarizePolicyDocument(currentDoc), [currentDoc])
  const goReview = () => {
    const blocked =
      tab === 'visual' ? visualErrors : jsonError !== null ? ['Policy document is not valid JSON.'] : []
    if (blocked.length > 0) {
      setNextBlocked(true)
      return
    }
    setNextBlocked(false)
    setStep('review')
  }
  const importPolicy = (mp: ManagedPolicy) => {
    const parsed = documentToVisual(mp.document)
    if (parsed.statements) {
      setStatements(parsed.statements)
      setJsonText(visualToJson(parsed.statements))
      setMfaFlags(parsed.statements.map(() => false))
      setSourceIps(parsed.statements.map(() => ''))
      setJsonError(null)
      setVisualUnsupported([])
      setTab('visual')
      setNotice(`Imported ${mp.name} into the editor — review it, then create the policy.`)
    } else {
      setNotice(`Could not import ${mp.name}: the document uses features the visual editor cannot show.`)
    }
    setImportOpen(false)
  }
  const create = useMutation({
    mutationFn: () => {
      let parsed: unknown
      if (tab === 'visual') {
        const errors = visualValidationErrors(statements)
        if (errors.length > 0) throw new Error(errors[0])
        parsed = visualToDocument(statements)
      } else {
        try {
          parsed = JSON.parse(jsonText)
        } catch {
          throw new Error('Policy document is not valid JSON.')
        }
      }
      return createIamPolicy(name.trim(), parsed, description.trim() === '' ? undefined : description.trim())
    },
    onSuccess: () => {
      setNotice(
        'Policy created — the backend validated and probe-evaluated it before storing.' +
          (tags.length > 0 ? ` ${tags.length} tag(s) entered on review were NOT saved: the teaching IAM API has no tag field.` : ''),
      )
      setName('')
      setDescription('')
      const parsed = documentToVisual(SAMPLE_POLICY)
      setStatements(parsed.statements ?? SAMPLE_VISUAL_STATEMENTS)
      setJsonText(SAMPLE_POLICY)
      setJsonError(null)
      setVisualUnsupported([])
      setTab('visual')
      setStep('specify')
      setNextBlocked(false)
      setTags([])
      setMfaFlags(SAMPLE_VISUAL_STATEMENTS.map(() => false))
      setSourceIps(SAMPLE_VISUAL_STATEMENTS.map(() => ''))
      void queryClient.invalidateQueries({ queryKey: ['iam-policies'] })
    },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const selected = policies.data?.find((p) => p.id === selectedId) ?? null
  const filteredPolicies = (policies.data ?? []).filter(
    (p) => policyFilter.trim() === '' || p.name.toLowerCase().includes(policyFilter.toLowerCase()),
  )
  const filteredImports = CLOUDFORGE_MANAGED_POLICIES.filter(
    (mp) =>
      importFilter.trim() === '' ||
      mp.name.toLowerCase().includes(importFilter.toLowerCase()) ||
      mp.description.toLowerCase().includes(importFilter.toLowerCase()),
  )
  const reviewBlocked =
    tab === 'visual' ? visualErrors : jsonError !== null ? ['Policy document is not valid JSON.'] : []
  return (
    <div className="space-y-4">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title="Create policy">
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (step === 'review' && name.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="iam-policy-name" className="text-sm font-medium">Policy name</label>
            <input id="iam-policy-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="list-buckets" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="iam-policy-desc" className="text-sm font-medium">Description</label>
            <input id="iam-policy-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          </div>
          <ol className="flex flex-wrap gap-4 text-sm">
            <li className={step === 'specify' ? 'font-semibold text-aws-orange-dark' : 'text-aws-muted'}>
              Step 1: Specify permissions
            </li>
            <li className={step === 'review' ? 'font-semibold text-aws-orange-dark' : 'text-aws-muted'}>
              Step 2: Review and create
            </li>
          </ol>
          {step === 'specify' && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <AwsButton type="button" onClick={() => setImportOpen((v) => !v)}>
                  {importOpen ? 'Close import' : 'Import policy'}
                </AwsButton>
                <span className="text-xs text-aws-muted">
                  Import offers built-in CloudForge sample policies — there is no AWS managed-policy catalogue in
                  CloudForge.
                </span>
              </div>
              {importOpen && (
                <div className="space-y-3 rounded-sm border border-aws-border bg-[#f9f9f9] p-3">
                  <p className="text-sm text-aws-muted">
                    Import a CloudForge sample policy into the editor. These are real documents in the grammar the
                    backend accepts, labelled as CloudForge samples — never as AWS managed policies.
                  </p>
                  <input
                    type="search"
                    value={importFilter}
                    onChange={(e) => setImportFilter(e.target.value)}
                    placeholder="Filter sample policies"
                    aria-label="Filter sample policies"
                    className="w-72 max-w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
                  />
                  <AwsTable<ManagedPolicy & { id: string }>
                    caption="CloudForge sample policies"
                    emptyMessage="No sample policy matches that filter."
                    rows={filteredImports.map((mp) => ({ ...mp, id: mp.name }))}
                    totalCount={CLOUDFORGE_MANAGED_POLICIES.length}
                    columns={[
                      { header: 'Policy name', render: (mp) => mp.name },
                      { header: 'Description', render: (mp) => mp.description },
                      {
                        header: 'Import',
                        render: (mp) => (
                          <AwsButton type="button" onClick={() => importPolicy(mp)}>
                            Import
                          </AwsButton>
                        ),
                      },
                    ]}
                  />
                </div>
              )}
              <div className="flex gap-2 text-sm" role="tablist" aria-label="Policy editor mode">
                <button type="button" role="tab" aria-selected={tab === 'visual'} disabled={visualDisabled && tab === 'json'} title={visualDisabled ? 'The current JSON uses features the visual editor cannot show — fix it on the JSON tab first.' : undefined} onClick={() => { if (tab === 'json') switchToVisual(); else setTab('visual') }} className={`border-b-2 px-2 py-1 font-medium ${tab === 'visual' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'} ${visualDisabled && tab === 'json' ? 'cursor-not-allowed opacity-50' : ''}`}>Visual</button>
                <button type="button" role="tab" aria-selected={tab === 'json'} onClick={switchToJson} className={`border-b-2 px-2 py-1 font-medium ${tab === 'json' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'}`}>JSON</button>
              </div>
              {tab === 'visual' && (
                <div className="space-y-3">
                  <p className="text-sm text-aws-muted">
                    Build the policy with pickers. Only what the backend accepts is offered: Allow/Deny, actions per
                    service, resources, and a StringEquals condition on aws:username. The JSON preview below is what
                    will be sent to the backend.
                  </p>
                  {statements.map((st, i) => {
                    const service = serviceById(st.service)
                    const known = service?.actions ?? []
                    const groups = service ? groupServiceActions(service) : []
                    const extraActions = st.actions.filter((a) => !known.some((k) => k.toLowerCase() === a.toLowerCase()))
                    const allActions = st.customAction.trim() !== '' && st.customAction.trim().toLowerCase() === `${st.service.toLowerCase()}:*`
                    return (
                      <div key={i} className="space-y-3 rounded-sm border border-aws-border bg-white p-3">
                        <div className="flex items-center justify-between">
                          <strong className="text-sm">Statement {i + 1}</strong>
                          <AwsButton type="button" disabled={statements.length <= 1} onClick={() => removeStatement(i)}>Remove statement</AwsButton>
                        </div>
                        <div className="flex flex-wrap gap-3">
                          <label className="text-sm">
                            Effect
                            <select value={st.effect} onChange={(e) => updateStatement(i, { effect: e.target.value as VisualStatement['effect'] })} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                              <option value="Allow">Allow</option>
                              <option value="Deny">Deny</option>
                            </select>
                          </label>
                          <label className="text-sm">
                            Service
                            <select
                              value={st.service}
                              onChange={(e) => updateStatement(i, { service: e.target.value, actions: [], customAction: '' })}
                              className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm"
                            >
                              {!service && <option value={st.service}>{st.service} (custom)</option>}
                              {POLICY_SERVICES.map((s) => (<option key={s.id} value={s.id}>{s.name}</option>))}
                            </select>
                          </label>
                        </div>
                        <div>
                          <div className="flex flex-wrap items-center gap-3">
                            <span className="text-sm font-medium">Actions</span>
                            {groups.length > 0 && (
                              <>
                                <button type="button" onClick={() => setCollapsedLevels([])} className="text-xs text-aws-link hover:underline">Expand all</button>
                                <button type="button" onClick={() => setCollapsedLevels(groups.map((g) => g.level))} className="text-xs text-aws-link hover:underline">Collapse all</button>
                              </>
                            )}
                          </div>
                          <label className="mt-1 block text-sm">
                            <input
                              type="checkbox"
                              checked={allActions}
                              onChange={(e) => updateStatement(i, { customAction: e.target.checked ? `${st.service}:*` : '', actions: e.target.checked ? [] : st.actions })}
                              className="mr-1"
                            />
                            All actions ({st.service}:*)
                          </label>
                          {groups.map((g) => {
                            const collapsed = collapsedLevels.includes(g.level)
                            const picked = g.actions.filter((a) => st.actions.some((x) => x.toLowerCase() === a.toLowerCase())).length
                            return (
                              <div key={g.level} className="mt-2 border border-aws-border">
                                <button
                                  type="button"
                                  onClick={() =>
                                    setCollapsedLevels((prev) =>
                                      collapsed ? prev.filter((l) => l !== g.level) : [...prev, g.level],
                                    )
                                  }
                                  className="flex w-full items-center justify-between bg-[#f2f3f3] px-2 py-1 text-sm font-medium"
                                >
                                  <span>{g.level} ({picked} of {g.actions.length} selected)</span>
                                  <span aria-hidden="true">{collapsed ? '+' : '−'}</span>
                                </button>
                                {!collapsed && (
                                  <div className="flex flex-wrap gap-x-4 gap-y-1 p-2">
                                    {g.actions.map((action) => (
                                      <label key={action} className="text-sm">
                                        <input
                                          type="checkbox"
                                          checked={st.actions.some((a) => a.toLowerCase() === action.toLowerCase())}
                                          disabled={allActions}
                                          onChange={(e) => updateStatement(i, { actions: e.target.checked ? [...st.actions, action] : st.actions.filter((a) => a.toLowerCase() !== action.toLowerCase()) })}
                                          className="mr-1"
                                        />
                                        {action}
                                      </label>
                                    ))}
                                  </div>
                                )}
                              </div>
                            )
                          })}
                          {extraActions.length > 0 && (
                            <p className="mt-1 text-sm">
                              Also included:{' '}
                              {extraActions.map((a) => (
                                <span key={a} className="mr-1 inline-flex items-center rounded-sm bg-[#f2f3f3] px-1.5 py-0.5 font-mono text-xs">
                                  {a}
                                  <button type="button" aria-label={`Remove ${a}`} onClick={() => updateStatement(i, { actions: st.actions.filter((x) => x !== a) })} className="ml-1 text-aws-link hover:underline">×</button>
                                </span>
                              ))}
                            </p>
                          )}
                          <label className="mt-2 block text-sm">
                            Custom action (e.g. {st.service}:* or {st.service}:GetBucketAcl)
                            <input value={st.customAction} onChange={(e) => updateStatement(i, { customAction: e.target.value })} placeholder={`${st.service}:*`} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm" />
                          </label>
                        </div>
                        <div>
                          <span className="text-sm font-medium">Resources</span>
                          <div className="mt-1 flex flex-wrap gap-4 text-sm">
                            <label>
                              <input
                                type="radio"
                                name={`iam-policy-resources-${i}`}
                                checked={st.resources.trim() === '*'}
                                onChange={() => updateStatement(i, { resources: '*' })}
                                className="mr-1"
                              />
                              All resources
                            </label>
                            <label>
                              <input
                                type="radio"
                                name={`iam-policy-resources-${i}`}
                                checked={st.resources.trim() !== '*'}
                                onChange={() =>
                                  updateStatement(i, { resources: st.resources.trim() === '*' ? '' : st.resources })
                                }
                                className="mr-1"
                              />
                              Specific
                            </label>
                          </div>
                          {st.resources.trim() !== '*' && (
                            <div className="mt-2">
                              <input
                                id={`iam-policy-resource-${i}`}
                                value={st.resources}
                                onChange={(e) => updateStatement(i, { resources: e.target.value })}
                                placeholder="arn:cloudforge:s3:::example-bucket/* — comma-separated for several resources"
                                className="w-full max-w-md rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm"
                              />
                              {(service?.resourcePatterns ?? ['*']).length > 1 && (
                                <span className="ml-2 text-sm text-aws-muted">
                                  Try:{' '}
                                  {service?.resourcePatterns.filter((p) => p !== '*').map((p) => (
                                    <button key={p} type="button" onClick={() => updateStatement(i, { resources: p })} className="mr-1 font-mono text-xs text-aws-link hover:underline">{p}</button>
                                  ))}
                                </span>
                              )}
                            </div>
                          )}
                          <p className="mt-1 text-xs text-aws-muted">
                            Enter ARNs for specific resources. The backend stores the string as typed; evaluation
                            matches it literally or by wildcard — no ARN is validated or resolved.
                          </p>
                        </div>
                        <div>
                          <label className="text-sm font-medium" htmlFor={`iam-policy-username-${i}`}>Condition — username (optional)</label>
                          <input id={`iam-policy-username-${i}`} value={st.username} onChange={(e) => updateStatement(i, { username: e.target.value })} placeholder="ada" className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
                          <p className="mt-1 text-sm text-aws-muted">
                            Only StringEquals on aws:username is evaluated by the backend — no other condition
                            operators or keys.
                          </p>
                        </div>
                        <div>
                          <span className="text-sm font-medium">Request conditions (labels only)</span>
                          <label className="mt-1 block text-sm">
                            <input
                              type="checkbox"
                              checked={mfaFlags[i] ?? false}
                              onChange={(e) => setMfaFlags((prev) => prev.map((v, j) => (j === i ? e.target.checked : v)))}
                              className="mr-1"
                            />
                            Require MFA
                          </label>
                          <label className="mt-1 block text-sm">
                            Source IP (optional)
                            <input
                              value={sourceIps[i] ?? ''}
                              onChange={(e) => setSourceIps((prev) => prev.map((v, j) => (j === i ? e.target.value : v)))}
                              placeholder="203.0.113.0/24"
                              className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm"
                            />
                          </label>
                          <p className="mt-1 text-xs text-aws-muted">
                            Labels only: the backend evaluates only StringEquals on aws:username, so MFA and
                            source-IP conditions are shown here for teaching and are never written into the policy
                            document.
                          </p>
                        </div>
                      </div>
                    )
                  })}
                  <AwsButton type="button" onClick={addStatement}>Add statement</AwsButton>
                  {visualErrors.length > 0 && (
                    <ul className="list-disc space-y-1 border border-aws-border bg-white px-4 py-2 text-sm text-red-800" role="alert">
                      {visualErrors.map((err) => (<li key={err}>{err}</li>))}
                    </ul>
                  )}
                  <div>
                    <span className="mb-1 block text-sm font-medium">JSON preview</span>
                    <pre className="overflow-x-auto rounded-sm bg-[#f2f3f3] p-3 font-mono text-xs">{visualToJson(statements)}</pre>
                  </div>
                </div>
              )}
              {tab === 'json' && (
                <div className="space-y-3">
                  <div>
                    <label htmlFor="iam-policy-doc" className="mb-1 block text-sm font-medium">
                      Policy document (JSON — validated in the browser, then by the backend before storage; unsupported features are rejected, never stored)
                    </label>
                    <textarea id="iam-policy-doc" value={jsonText} onChange={(e) => { setJsonText(e.target.value); analyseJson(e.target.value) }} rows={12} className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
                  </div>
                  {jsonError !== null && (
                    <p className="border border-aws-border bg-white px-4 py-2 text-sm text-red-800" role="alert">Not valid JSON: {jsonError}</p>
                  )}
                  {visualUnsupported.length > 0 && (
                    <div className="border border-aws-border bg-white px-4 py-2 text-sm" role="alert">
                      <p className="font-medium">This policy cannot be shown in the visual editor:</p>
                      <ul className="mt-1 list-disc pl-5">
                        {visualUnsupported.map((reason) => (<li key={reason}>{reason}</li>))}
                      </ul>
                      <p className="mt-1 text-aws-muted">
                        Your JSON is kept as-is — the Visual tab stays disabled until the JSON only uses features the
                        visual editor supports. You can still create the policy from JSON; the backend will reject it
                        if it uses features it does not support.
                      </p>
                    </div>
                  )}
                </div>
              )}
              <div className="border border-aws-border">
                <div className="flex flex-wrap items-center gap-2 border-b border-aws-border bg-[#f2f3f3] px-3 py-2">
                  <strong className="text-sm">Policy linter</strong>
                  <span className="text-xs text-aws-muted">
                    Basic client-side checks over the stored-document grammar — not an AWS lint result. The backend
                    still validates on create.
                  </span>
                  {(['Security', 'Errors', 'Warnings', 'Suggestions'] as const).map((cat) => (
                    <button
                      key={cat}
                      type="button"
                      onClick={() => setLinterTab(cat)}
                      className={`rounded-sm px-2 py-0.5 text-xs font-medium ${linterTab === cat ? 'bg-white text-aws-orange-dark shadow-sm' : 'text-aws-muted hover:text-aws-text'}`}
                    >
                      {cat} ({findings.filter((f) => f.category === cat).length})
                    </button>
                  ))}
                </div>
                <ul className="space-y-1 px-3 py-2 text-sm">
                  {findings.filter((f) => f.category === linterTab).length === 0 ? (
                    <li className="text-aws-muted">No {linterTab.toLowerCase()} findings.</li>
                  ) : (
                    findings
                      .filter((f) => f.category === linterTab)
                      .map((f, i) => <li key={i}>{f.message}</li>)
                  )}
                </ul>
              </div>
              {nextBlocked && (
                <div className="border border-red-300 bg-red-50 px-4 py-2 text-sm text-red-800" role="alert">
                  <p className="font-medium">Fix these before reviewing:</p>
                  <ul className="mt-1 list-disc pl-5">
                    {(tab === 'visual' ? visualErrors : ['Policy document is not valid JSON.']).map((err) => (
                      <li key={err}>{err}</li>
                    ))}
                  </ul>
                </div>
              )}
              <div>
                <AwsButton type="button" variant="primary" onClick={goReview}>
                  Next: Review and create
                </AwsButton>
              </div>
            </>
          )}
          {step === 'review' && (
            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-semibold">Review permissions</h3>
                {summary.representable ? (
                  <AwsTable<PolicySummaryRow & { id: string }>
                    caption="Permissions summary"
                    emptyMessage="No statements to review."
                    rows={summary.rows.map((r, i) => ({ ...r, id: String(i) }))}
                    totalCount={summary.rows.length}
                    columns={[
                      { header: 'Service', render: (r) => r.service },
                      { header: 'Access level', render: (r) => r.accessLevel },
                      { header: 'Resource', render: (r) => <span className="font-mono text-xs">{r.resource}</span> },
                      { header: 'Request condition', render: (r) => r.condition },
                    ]}
                  />
                ) : (
                  <p className="text-sm text-aws-muted">
                    This document cannot be summarised — it uses features the visual editor cannot show. The JSON
                    below is what will be sent.
                  </p>
                )}
                <div className="mt-3">
                  <span className="mb-1 block text-sm font-medium">Policy document (JSON)</span>
                  <pre className="overflow-x-auto rounded-sm bg-[#f2f3f3] p-3 font-mono text-xs">
                    {tab === 'visual' ? visualToJson(statements) : jsonText}
                  </pre>
                </div>
              </div>
              <div>
                <h3 className="text-sm font-semibold">Tags (optional — up to 50)</h3>
                <p className="text-xs text-aws-muted">
                  Tags are entered here for review only — they are NOT saved. The teaching IAM API has no tag field.
                </p>
                {tags.length > 0 && (
                  <ul className="mt-2 space-y-1 text-sm">
                    {tags.map((t, i) => (
                      <li key={`${t.key}-${i}`}>
                        <span className="font-mono text-xs">{t.key}</span>
                        {t.value !== '' && <> = <span className="font-mono text-xs">{t.value}</span></>}
                        <button type="button" aria-label={`Remove tag ${t.key}`} onClick={() => setTags((prev) => prev.filter((_, j) => j !== i))} className="ml-2 text-aws-link hover:underline">×</button>
                      </li>
                    ))}
                  </ul>
                )}
                {tags.length < 50 && (
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    <input value={tagKey} onChange={(e) => setTagKey(e.target.value)} placeholder="Key" aria-label="Tag key" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
                    <input value={tagValue} onChange={(e) => setTagValue(e.target.value)} placeholder="Value (optional)" aria-label="Tag value" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
                    <AwsButton
                      type="button"
                      disabled={tagKey.trim() === ''}
                      onClick={() => {
                        setTags((prev) => [...prev, { key: tagKey.trim(), value: tagValue.trim() }])
                        setTagKey('')
                        setTagValue('')
                      }}
                    >
                      Add tag
                    </AwsButton>
                  </div>
                )}
              </div>
              <div>
                <h3 className="text-sm font-semibold">Request conditions (labels only)</h3>
                {statements.every((_, i) => !mfaFlags[i] && (sourceIps[i] ?? '') === '') ? (
                  <p className="text-sm text-aws-muted">No MFA or source-IP labels set on any statement.</p>
                ) : (
                  <ul className="mt-1 list-disc space-y-1 pl-5 text-sm">
                    {statements.map((_, i) =>
                      mfaFlags[i] || (sourceIps[i] ?? '') !== '' ? (
                        <li key={i}>
                          Statement {i + 1}
                          {mfaFlags[i] ? ' — MFA required' : ''}
                          {(sourceIps[i] ?? '') !== '' ? ` — source IP ${sourceIps[i]}` : ''}{' '}
                          (label only — never written into the document)
                        </li>
                      ) : null,
                    )}
                  </ul>
                )}
              </div>
              {reviewBlocked.length > 0 && (
                <ul className="list-disc space-y-1 border border-aws-border bg-white px-4 py-2 text-sm text-red-800" role="alert">
                  {reviewBlocked.map((err) => (<li key={err}>{err}</li>))}
                </ul>
              )}
              <div className="flex flex-wrap gap-2">
                <AwsButton type="button" onClick={() => setStep('specify')}>
                  Previous
                </AwsButton>
                <AwsButton
                  type="submit"
                  variant="primary"
                  disabled={name.trim() === '' || reviewBlocked.length > 0}
                >
                  Create policy
                </AwsButton>
              </div>
            </div>
          )}
        </form>
      </AwsCard>
      {policies.isPending && <Loading label="Loading IAM policies" />}
      <AdminOnly error={policies.isError ? policies.error : null} />
      {policies.data && (
        <AwsCard title={`Policies (${policies.data.length})`}>
          <AwsTable<IamPolicy & { id: string }>
            caption="IAM policies"
            emptyMessage="No IAM policies yet — create one above."
            rows={filteredPolicies}
            totalCount={policies.data.length}
            filter={policyFilter}
            onFilterChange={setPolicyFilter}
            filterPlaceholder="Filter policies by name"
            columns={[
              // Policies columns in the v4 survey's order (Policy name,
              // Type, Used as, Description) plus Created, which the
              // list already showed. Type is 'Customer managed' for
              // every CloudForge policy — all of them are created here,
              // none is an AWS managed policy. Used as is 'Permissions
              // policy' — permissions boundaries are not modelled.
              { header: 'Policy name', render: (p) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(p.id)}>{p.name}</button> },
              { header: 'Type', render: () => 'Customer managed' },
              { header: 'Used as', render: () => 'Permissions policy' },
              { header: 'Description', render: (p) => p.description ?? '—' },
              { header: 'Created', render: (p) => (p.created_at ? new Date(p.created_at).toLocaleString() : '—') },
            ]}
          />
        </AwsCard>
      )}
      {selected && (
        <PolicyDetail
          policy={selected}
          users={users.data ?? []}
          groups={groups.data ?? []}
          roles={roles.data ?? []}
          onDeleted={() => {
            setSelectedId(null)
            void queryClient.invalidateQueries({ queryKey: ['iam-policies'] })
          }}
          onNotice={setNotice}
        />
      )}
    </div>
  )
}

// --- Policy detail (v4) ------------------------------------------------------
// The survey's policy detail tabs: Permissions (Summary / JSON),
// Entities attached, Policy versions, Last Accessed. Everything here is
// derived from stored data; CloudForge records no policy usage and
// keeps one version per policy, and the detail says so instead of
// inventing rows.

function PolicyDetail({
  policy,
  users,
  groups,
  roles,
  onDeleted,
  onNotice,
}: {
  policy: IamPolicy
  users: IamUser[]
  groups: IamGroup[]
  roles: IamRole[]
  onDeleted: () => void
  onNotice: (s: string) => void
}) {
  const queryClient = useQueryClient()
  const [detailTab, setDetailTab] = useState<'permissions' | 'entities' | 'versions' | 'accessed'>('permissions')
  const [permView, setPermView] = useState<'summary' | 'json'>('summary')
  const [attachKind, setAttachKind] = useState<'user' | 'group' | 'role'>('user')
  const [attachId, setAttachId] = useState('')
  const summary = useMemo(() => summarizePolicyDocument(policy.document), [policy.document])
  const attachedUsers = users.filter((u) => (u.policies ?? []).includes(policy.id))
  const attachedGroups = groups.filter((g) => (g.policies ?? []).includes(policy.id))
  const attachedRoles = roles.filter((r) => (r.policies ?? []).includes(policy.id))
  const attachTargets =
    attachKind === 'user' ? users : attachKind === 'group' ? groups : roles
  const del = useMutation({
    mutationFn: () => deleteIamPolicy(policy.id),
    onSuccess: () => {
      onNotice('Policy deleted.')
      onDeleted()
    },
    onError: (e: Error) => onNotice(`Delete failed: ${e.message}`),
  })
  const attach = useMutation({
    mutationFn: () => attachIamPolicy(attachKind, attachId, policy.id),
    onSuccess: () => {
      onNotice(`Policy attached to ${attachKind} ${nameOf(attachTargets, attachId)}.`)
      setAttachId('')
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => onNotice(`Attach failed: ${e.message}`),
  })
  const detach = useMutation({
    mutationFn: ({ kind, id }: { kind: 'user' | 'group' | 'role'; id: string }) =>
      detachIamPolicy(kind, id, policy.id),
    onSuccess: () => {
      onNotice('Policy detached.')
      void queryClient.invalidateQueries({ queryKey: ['iam-users'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['iam-roles'] })
    },
    onError: (e: Error) => onNotice(`Detach failed: ${e.message}`),
  })
  return (
    <AwsCard title={`Policy ${policy.name}`}>
      <ConsoleTabs
        tabs={[
          { id: 'permissions', label: 'Permissions' },
          { id: 'entities', label: 'Entities attached' },
          { id: 'versions', label: 'Policy versions' },
          { id: 'accessed', label: 'Last Accessed' },
        ]}
        active={detailTab}
        onChange={(id) => setDetailTab(id as typeof detailTab)}
      />
      <div className="mt-3">
        {detailTab === 'permissions' && (
          <div className="space-y-3">
            <div className="flex gap-2 text-sm" role="tablist" aria-label="Permissions view">
              <button type="button" role="tab" aria-selected={permView === 'summary'} onClick={() => setPermView('summary')} className={`border-b-2 px-2 py-1 font-medium ${permView === 'summary' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'}`}>Summary</button>
              <button type="button" role="tab" aria-selected={permView === 'json'} onClick={() => setPermView('json')} className={`border-b-2 px-2 py-1 font-medium ${permView === 'json' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'}`}>JSON</button>
            </div>
            {permView === 'summary' &&
              (summary.representable ? (
                <AwsTable<PolicySummaryRow & { id: string }>
                  caption="Permissions summary"
                  emptyMessage="No statements in this policy."
                  rows={summary.rows.map((r, i) => ({ ...r, id: String(i) }))}
                  totalCount={summary.rows.length}
                  columns={[
                    { header: 'Service', render: (r) => r.service },
                    { header: 'Access level', render: (r) => r.accessLevel },
                    { header: 'Resource', render: (r) => <span className="font-mono text-xs">{r.resource}</span> },
                    { header: 'Request condition', render: (r) => r.condition },
                  ]}
                />
              ) : (
                <p className="text-sm text-aws-muted">
                  This policy cannot be summarised — it uses features the visual editor cannot show. See the JSON
                  view for the stored document.
                </p>
              ))}
            {permView === 'json' && (
              <div>
                <div className="mb-2 flex items-center gap-2">
                  <span className="text-sm font-medium">Policy document</span>
                  <AwsCopy value={JSON.stringify(policy.document, null, 2)} label="Copy JSON" />
                </div>
                <pre className="overflow-x-auto rounded-sm bg-[#f2f3f3] p-3 font-mono text-xs">{JSON.stringify(policy.document, null, 2)}</pre>
              </div>
            )}
          </div>
        )}
        {detailTab === 'entities' && (
          <div className="space-y-4">
            <div>
              <h3 className="text-sm font-semibold">Attached as a permissions policy</h3>
              {attachedUsers.length === 0 && attachedGroups.length === 0 && attachedRoles.length === 0 ? (
                <p className="mt-1 text-sm text-aws-muted">This policy is not attached to any user, group or role.</p>
              ) : (
                <ul className="mt-1 space-y-1 text-sm">
                  {attachedUsers.map((u) => (
                    <li key={`user-${u.id}`}>
                      User <strong>{u.name}</strong>
                      <button type="button" onClick={() => detach.mutate({ kind: 'user', id: u.id })} className="ml-2 text-aws-link hover:underline">Detach</button>
                    </li>
                  ))}
                  {attachedGroups.map((g) => (
                    <li key={`group-${g.id}`}>
                      Group <strong>{g.name}</strong>
                      <button type="button" onClick={() => detach.mutate({ kind: 'group', id: g.id })} className="ml-2 text-aws-link hover:underline">Detach</button>
                    </li>
                  ))}
                  {attachedRoles.map((r) => (
                    <li key={`role-${r.id}`}>
                      Role <strong>{r.name}</strong>
                      <button type="button" onClick={() => detach.mutate({ kind: 'role', id: r.id })} className="ml-2 text-aws-link hover:underline">Detach</button>
                    </li>
                  ))}
                </ul>
              )}
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <label className="text-sm">
                  Attach to
                  <select value={attachKind} onChange={(e) => { setAttachKind(e.target.value as typeof attachKind); setAttachId('') }} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                    <option value="user">User</option>
                    <option value="group">Group</option>
                    <option value="role">Role</option>
                  </select>
                </label>
                <select value={attachId} onChange={(e) => setAttachId(e.target.value)} aria-label="Identity to attach to" className="rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                  <option value="">Select…</option>
                  {attachTargets.map((t) => (
                    <option key={t.id} value={t.id}>{t.name}</option>
                  ))}
                </select>
                <AwsButton disabled={attachId === ''} onClick={() => attach.mutate()}>
                  Attach policy
                </AwsButton>
              </div>
            </div>
            <div>
              <h3 className="text-sm font-semibold">Attached as a permissions boundary</h3>
              <p className="mt-1 text-sm text-aws-muted">
                No boundary attachments — permissions boundaries are not modelled in CloudForge teaching IAM.
              </p>
            </div>
          </div>
        )}
        {detailTab === 'versions' && (
          <div className="space-y-2">
            <AwsTable<{ id: string; version: string; created: string; isDefault: string }>
              caption="Policy versions"
              emptyMessage="No versions."
              rows={[{ id: 'v1', version: 'Version 1', created: policy.created_at ? new Date(policy.created_at).toLocaleString() : '—', isDefault: 'Default version' }]}
              totalCount={1}
              columns={[
                { header: 'Version', render: (r) => r.version },
                { header: 'Created', render: (r) => r.created },
                { header: 'Default', render: (r) => r.isDefault },
              ]}
            />
            <p className="text-sm text-aws-muted">
              CloudForge stores one version per policy — there is no version history and no new-version action.
            </p>
          </div>
        )}
        {detailTab === 'accessed' && (
          <div className="space-y-2">
            {summary.representable && summary.rows.length > 0 ? (
              <AwsTable<PolicySummaryRow & { id: string }>
                caption="Last accessed"
                emptyMessage="No services in this policy."
                rows={summary.rows.map((r, i) => ({ ...r, id: String(i) }))}
                totalCount={summary.rows.length}
                columns={[
                  { header: 'Service', render: (r) => r.service },
                  { header: 'Last accessed', render: () => 'Not recorded' },
                ]}
              />
            ) : (
              <p className="text-sm text-aws-muted">This policy has no summarisable services.</p>
            )}
            <p className="text-sm text-aws-muted">
              CloudForge records no policy usage, so last-accessed data is not available — the services above come
              from the policy document, not from usage.
            </p>
          </div>
        )}
      </div>
      <div className="mt-4">
        <AwsButton onClick={() => del.mutate()}>Delete policy</AwsButton>
      </div>
    </AwsCard>
  )
}

// --- Policy Simulator ----------------------------------------------------------

function SimulatorSection() {
  const { users, groups, roles, policies } = useIamData()
  const [mode, setMode] = useState<'inline' | 'stored'>('stored')
  const [policyText, setPolicyText] = useState(SAMPLE_POLICY)
  const [userId, setUserId] = useState('')
  const [groupId, setGroupId] = useState('')
  const [roleId, setRoleId] = useState('')
  const [policyId, setPolicyId] = useState('')
  const [action, setAction] = useState('s3:ListBuckets')
  const [resource, setResource] = useState('*')
  const [username, setUsername] = useState('')
  const [formError, setFormError] = useState('')
  const evaluate = useMutation({
    mutationFn: () => {
      if (mode === 'inline') {
        let parsed: unknown
        try {
          parsed = JSON.parse(policyText)
        } catch {
          throw new Error('Policy JSON is not valid JSON.')
        }
        const list: unknown[] = Array.isArray(parsed) ? parsed : [parsed]
        return evaluateIamPolicy({ policies: list, action, resource, username: username === '' ? undefined : username })
      }
      if (userId === '' && groupId === '' && roleId === '' && policyId === '') {
        throw new Error('Pick a stored user, group, role or policy to evaluate.')
      }
      return evaluateIamPolicy({
        user_id: userId === '' ? undefined : userId,
        group_ids: groupId === '' ? undefined : [groupId],
        role_ids: roleId === '' ? undefined : [roleId],
        policy_ids: policyId === '' ? undefined : [policyId],
        action,
        resource,
        username: username === '' ? undefined : username,
      })
    },
    onError: (e: Error) => setFormError(e.message),
    onSuccess: () => setFormError(''),
  })
  const deciding = useMemo(
    () => evaluate.data?.trail?.find((t) => t.decides),
    [evaluate.data],
  )
  return (
    <AwsCard title="Policy Simulator" actions={<FidelityBadge fidelity="PARTIAL" footnote="real subset evaluation" />}>
      <div className="mb-3 flex gap-2 text-sm" role="tablist" aria-label="Simulator mode">
        <button type="button" role="tab" aria-selected={mode === 'stored'} onClick={() => setMode('stored')} className={`border-b-2 px-2 py-1 font-medium ${mode === 'stored' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'}`}>Stored identities</button>
        <button type="button" role="tab" aria-selected={mode === 'inline'} onClick={() => setMode('inline')} className={`border-b-2 px-2 py-1 font-medium ${mode === 'inline' ? 'border-aws-orange text-aws-orange-dark' : 'border-transparent text-aws-muted'}`}>Inline policy JSON</button>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          evaluate.mutate()
        }}
        className="space-y-3"
      >
        {mode === 'stored' && (
          <div className="flex flex-wrap gap-3">
            <label className="text-sm">
              User
              <select value={userId} onChange={(e) => setUserId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="">—</option>
                {(users.data ?? []).map((u) => (<option key={u.id} value={u.id}>{u.name}</option>))}
              </select>
            </label>
            <label className="text-sm">
              Group
              <select value={groupId} onChange={(e) => setGroupId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="">—</option>
                {(groups.data ?? []).map((g) => (<option key={g.id} value={g.id}>{g.name}</option>))}
              </select>
            </label>
            <label className="text-sm">
              Role
              <select value={roleId} onChange={(e) => setRoleId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="">—</option>
                {(roles.data ?? []).map((r) => (<option key={r.id} value={r.id}>{r.name}</option>))}
              </select>
            </label>
            <label className="text-sm">
              Policy
              <select value={policyId} onChange={(e) => setPolicyId(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm">
                <option value="">—</option>
                {(policies.data ?? []).map((p) => (<option key={p.id} value={p.id}>{p.name}</option>))}
              </select>
            </label>
          </div>
        )}
        {mode === 'inline' && (
          <div>
            <label htmlFor="iam-sim-policy" className="mb-1 block text-sm font-medium">Policy (JSON — a policy document, or an array of them)</label>
            <textarea id="iam-sim-policy" value={policyText} onChange={(e) => setPolicyText(e.target.value)} rows={12} className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none" />
          </div>
        )}
        <div className="flex flex-wrap gap-3">
          <label className="text-sm">
            Action
            <input value={action} onChange={(e) => setAction(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <label className="text-sm">
            Resource
            <input value={resource} onChange={(e) => setResource(e.target.value)} className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
          <label className="text-sm">
            Username (optional)
            <input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="ada" className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
          </label>
        </div>
        {formError !== '' && <ErrorBox message={formError} />}
        <AwsButton type="submit">Evaluate policy</AwsButton>
      </form>
      {evaluate.isSuccess && (
        <div className="mt-4 space-y-3 border-t border-aws-border pt-3">
          <p className="text-sm">
            Decision: <StateBadge state={evaluate.data.decision === 'Allow' ? 'READY' : 'FAILED'} />{' '}
            <strong>{evaluate.data.decision}</strong>
          </p>
          {evaluate.data.reason && <p className="text-sm text-aws-muted">{evaluate.data.reason}</p>}
          {deciding && (
            <p className="text-sm">
              Decided by policy {deciding.policy_index + 1}, statement {deciding.statement_index + 1} ({deciding.effect}): {deciding.reason}
            </p>
          )}
          {evaluate.data.policies_evaluated && evaluate.data.policies_evaluated.length > 0 && (
            <p className="text-sm text-aws-muted">Policies evaluated: {evaluate.data.policies_evaluated.join(', ')}</p>
          )}
          {evaluate.data.trail && evaluate.data.trail.length > 0 && (
            <AwsTable<{ id: string; policy_index: number; statement_index: number; effect: string; matched: boolean; reason: string; decides: boolean }>
              caption="Evaluation trail"
              emptyMessage="No trail."
              rows={evaluate.data.trail.map((t, i) => ({ ...t, id: String(i) }))}
              totalCount={evaluate.data.trail.length}
              columns={[
                { header: 'Policy', render: (t) => String(t.policy_index + 1) },
                { header: 'Statement', render: (t) => String(t.statement_index + 1) },
                { header: 'Effect', render: (t) => t.effect },
                { header: 'Matched', render: (t) => (t.matched ? 'yes' : 'no') },
                { header: 'Decides', render: (t) => (t.decides ? 'yes — this statement decided' : 'no') },
                { header: 'Reason', render: (t) => t.reason },
              ]}
            />
          )}
        </div>
      )}
      {evaluate.isError && <div className="mt-3"><ErrorBox message={`Evaluation failed: ${evaluate.error.message}`} /></div>}
    </AwsCard>
  )
}
