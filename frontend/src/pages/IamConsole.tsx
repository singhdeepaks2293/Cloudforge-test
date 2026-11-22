import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge, StateBadge } from '../components/AwsBadge.tsx'
import { ServiceConsole } from '../components/ServiceConsole.tsx'
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
  POLICY_SERVICES,
  SAMPLE_VISUAL_STATEMENTS,
  blankStatement,
  documentToVisual,
  serviceById,
  visualToDocument,
  visualToJson,
  visualValidationErrors,
} from '../services/iamPolicyEditor.ts'
import type { VisualStatement } from '../services/iamPolicyEditor.ts'

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
              { header: 'User name', render: (u) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(u.id)}>{u.name}</button> },
              { header: 'ARN', render: (u) => <span className="break-all font-mono text-xs">{u.arn}</span> },
              { header: 'Groups', render: (u) => String(u.groups?.length ?? 0) },
              { header: 'Policies', render: (u) => String(u.policies?.length ?? 0) },
              { header: 'Created', render: (u) => (u.created_at ? new Date(u.created_at).toLocaleString() : '—') },
            ]}
          />
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
  const { roles, policies } = useIamData()
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
        <RoleDetail role={selected} policies={policies.data ?? []} onDelete={() => del.mutate(selected.id)} />
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

function RoleDetail({ role, policies, onDelete }: { role: IamRole; policies: IamPolicy[]; onDelete: () => void }) {
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
  return (
    <AwsCard title={`Role ${role.name}`}>
      <dl className="space-y-1 text-sm">
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">ARN</dt><dd className="break-all font-mono text-xs">{role.arn}</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Max session</dt><dd>{role.max_session_duration_seconds}s</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Trusted entities</dt><dd className="break-all font-mono text-xs">{trustedEntities(role.trust_policy)}</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Trust policy</dt><dd className="break-all font-mono text-xs">{JSON.stringify(role.trust_policy)}</dd></div>
      </dl>
      <h3 className="mt-4 text-sm font-semibold">Attached policies</h3>
      <AttachedPolicies kind="role" id={role.id} policyIds={role.policies} policies={policies} />
      <PolicyAttach kind="role" id={role.id} policies={policies} />
      <h3 className="mt-4 text-sm font-semibold">Assume role</h3>
      <p className="text-sm text-aws-muted">Returns a short-lived session token (shown once). The backend enforces expiry — an expired session is a 410, never silently extended.</p>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <label className="text-sm">
          Duration (seconds, max {role.max_session_duration_seconds})
          <input type="number" min={60} max={role.max_session_duration_seconds} value={duration} onChange={(e) => setDuration(Number(e.target.value))} className="ml-2 w-24 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
        </label>
        <AwsButton onClick={() => assume.mutate()}>Assume role</AwsButton>
      </div>
      {notice !== '' && <p className="mt-2 text-sm" role="status">{notice}</p>}
      {session && (
        <div className="mt-3 border border-aws-orange bg-[#fff8f0] px-3 py-2 text-sm" role="alert">
          <p className="font-semibold">Session created — copy the token now.</p>
          <p>Session ID: <code className="font-mono text-xs">{session.session_id}</code></p>
          <p>Token: <code className="font-mono text-xs">{session.token}</code></p>
          <p>Expires: {new Date(session.expires_at).toLocaleString()}</p>
          <p className="text-aws-muted">{session.note}</p>
        </div>
      )}
      <div className="mt-3">
        <AwsButton onClick={onDelete}>Delete role</AwsButton>
      </div>
    </AwsCard>
  )
}

// --- Policies ----------------------------------------------------------------

function PoliciesSection() {
  const queryClient = useQueryClient()
  const { policies } = useIamData()
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
      setNotice('Policy created — the backend validated and probe-evaluated it before storing.')
      setName('')
      setDescription('')
      const parsed = documentToVisual(SAMPLE_POLICY)
      setStatements(parsed.statements ?? SAMPLE_VISUAL_STATEMENTS)
      setJsonText(SAMPLE_POLICY)
      setJsonError(null)
      setVisualUnsupported([])
      setTab('visual')
      void queryClient.invalidateQueries({ queryKey: ['iam-policies'] })
    },
    onError: (e: Error) => setNotice(`Create failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => deleteIamPolicy(id),
    onSuccess: () => {
      setNotice('Policy deleted.')
      setSelectedId(null)
      void queryClient.invalidateQueries({ queryKey: ['iam-policies'] })
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const selected = policies.data?.find((p) => p.id === selectedId) ?? null
  return (
    <div className="space-y-4">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <AwsCard title="Create policy">
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim() !== '') create.mutate()
          }}
        >
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="iam-policy-name" className="text-sm font-medium">Policy name</label>
            <input id="iam-policy-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="list-buckets" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="iam-policy-desc" className="text-sm font-medium">Description</label>
            <input id="iam-policy-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          </div>
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
                const extraActions = st.actions.filter((a) => !known.some((k) => k.toLowerCase() === a.toLowerCase()))
                const allActions = st.customAction.trim() !== '' && st.customAction.trim().toLowerCase() === `${st.service.toLowerCase()}:*`
                return (
                  <div key={i} className="space-y-3 rounded-sm border border-aws-border bg-white p-3">
                    <div className="flex items-center justify-between">
                      <strong className="text-sm">Statement {i + 1}</strong>
                      <AwsButton disabled={statements.length <= 1} onClick={() => setStatements((prev) => prev.filter((_, j) => j !== i))}>Remove statement</AwsButton>
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
                      <span className="text-sm font-medium">Actions</span>
                      <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
                        <label className="text-sm">
                          <input
                            type="checkbox"
                            checked={allActions}
                            onChange={(e) => updateStatement(i, { customAction: e.target.checked ? `${st.service}:*` : '', actions: e.target.checked ? [] : st.actions })}
                            className="mr-1"
                          />
                          All actions ({st.service}:*)
                        </label>
                        {known.map((action) => (
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
                      <label className="text-sm font-medium" htmlFor={`iam-policy-resource-${i}`}>Resource</label>
                      <input id={`iam-policy-resource-${i}`} value={st.resources} onChange={(e) => updateStatement(i, { resources: e.target.value })} placeholder="* — comma-separated for several resources" className="ml-2 w-full max-w-md rounded-sm border border-aws-border-dark px-2 py-1 font-mono text-sm sm:w-auto" />
                      {(service?.resourcePatterns ?? ['*']).length > 1 && (
                        <span className="ml-2 text-sm text-aws-muted">
                          Try:{' '}
                          {service?.resourcePatterns.filter((p) => p !== '*').map((p) => (
                            <button key={p} type="button" onClick={() => updateStatement(i, { resources: p })} className="mr-1 font-mono text-xs text-aws-link hover:underline">{p}</button>
                          ))}
                        </span>
                      )}
                    </div>
                    <div>
                      <label className="text-sm font-medium" htmlFor={`iam-policy-username-${i}`}>Condition — username (optional)</label>
                      <input id={`iam-policy-username-${i}`} value={st.username} onChange={(e) => updateStatement(i, { username: e.target.value })} placeholder="ada" className="ml-2 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
                      <p className="mt-1 text-sm text-aws-muted">
                        Only StringEquals on aws:username is evaluated by the backend — no other condition
                        operators or keys.
                      </p>
                    </div>
                  </div>
                )
              })}
              <AwsButton onClick={() => setStatements((prev) => [...prev, blankStatement()])}>Add statement</AwsButton>
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
          <AwsButton type="submit" disabled={name.trim() === '' || (tab === 'visual' && visualErrors.length > 0) || (tab === 'json' && jsonError !== null)}>Create policy</AwsButton>
        </form>
      </AwsCard>
      {policies.isPending && <Loading label="Loading IAM policies" />}
      <AdminOnly error={policies.isError ? policies.error : null} />
      {policies.data && (
        <AwsCard title={`Policies (${policies.data.length})`}>
          <AwsTable<IamPolicy & { id: string }>
            caption="IAM policies"
            emptyMessage="No IAM policies yet — create one above."
            rows={policies.data}
            totalCount={policies.data.length}
            columns={[
              { header: 'Policy name', render: (p) => <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedId(p.id)}>{p.name}</button> },
              { header: 'Description', render: (p) => p.description ?? '—' },
              { header: 'Created', render: (p) => (p.created_at ? new Date(p.created_at).toLocaleString() : '—') },
            ]}
          />
        </AwsCard>
      )}
      {selected && (
        <AwsCard title={`Policy ${selected.name}`}>
          <pre className="overflow-x-auto rounded-sm bg-[#f2f3f3] p-3 font-mono text-xs">{JSON.stringify(selected.document, null, 2)}</pre>
          <div className="mt-3">
            <AwsButton onClick={() => del.mutate(selected.id)}>Delete policy</AwsButton>
          </div>
        </AwsCard>
      )}
    </div>
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
