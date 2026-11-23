import { useRef, useState } from 'react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
import {
  AwsActionsMenu,
  AwsCopy,
  AwsFlash,
  AwsPreferencesButton,
  AwsRefreshButton,
  AwsStat,
  AwsToolbar,
  AwsWizard,
} from '../components/AwsShell.tsx'
import {
  createS3Bucket,
  deleteS3Bucket,
  deleteS3Object,
  downloadS3Object,
  fetchS3BucketPolicy,
  fetchS3Buckets,
  fetchS3ObjectMetadata,
  fetchS3Objects,
  fetchS3Versioning,
  setS3BucketPolicy,
  setS3Versioning,
  uploadS3Object,
} from '../services/api.ts'
import type { S3Bucket, S3Object } from '../types/index.ts'

/**
 * S3 console — mirrors the AWS S3 Buckets console: service sub-nav,
 * a toolbar with Create bucket / Actions / Refresh, a buckets table
 * with checkboxes, bucket detail tabs Objects / Properties /
 * Permissions / Management / Metrics, and a multi-step Create bucket
 * wizard.
 *
 * Buckets are real local MinIO buckets (API_EMULATED); objects
 * list/upload/download/delete through the backend (no direct
 * browser→MinIO credentials). Versioning is a real toggle where the
 * adapter supports it; object ACLs are NOT exposed by minio-go and
 * the tab says so. MinIO down is an honest 503.
 *
 * Second half of the AWS S3 left nav (2026-11-22 survey): Files,
 * Access Grants, IAM Access Analyzer, Storage Lens and Storage
 * management and insights are sections of their own. The survey
 * records each nav item only — no table columns and no detail tabs
 * were recorded for any of them, so no AWS columns are claimed here.
 * Files / Access Grants / Analyzer / Storage Lens are honest
 * UNSUPPORTED empty states (no backend API exists for any of them);
 * Storage management and insights is a PARTIAL real table of per-bucket
 * management state (versioning is real; replication/lifecycle/
 * inventory honestly have no API). Access Points, Object Lambda
 * access points, Batch operations and Replication have no backend
 * operation either (see backend/internal/httpapi/httpapi.go — the S3
 * routes are buckets, objects, versioning and policy only), so they
 * stay disabled in the nav with the reason shown under the label.
 *
 * v4 survey (2026-11-22) notes: the Buckets table header is now
 * "AWS Region"; bucket-type tabs (General purpose / Directory) sit
 * above the table — only general-purpose buckets exist here. The
 * IAM Access Analyzer column is KEPT even though the new survey
 * shows no Analyzer column: the old survey recorded one and the two
 * surveys disagree, so the column stays as-is (always "Not
 * available") rather than being deleted on one pass's evidence.
 * Storage Lens now has its survey tabs (dashboards / groups) and an
 * empty table with the survey's columns (Dashboard name, Status).
 * Access Points for FSx is an honest UNSUPPORTED section.
 */
const CREATE_STEPS = ['General configuration', 'Ownership', 'Block Public Access', 'Versioning', 'Summary']

export function S3Console({ initialSection }: { initialSection?: string }) {
  const queryClient = useQueryClient()
  const buckets = useQuery({ queryKey: ['s3-buckets'], queryFn: fetchS3Buckets, retry: 1 })
  const [activeSub, setActiveSub] = useState(initialSection ?? 'buckets')
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [checked, setChecked] = useState<string[]>([])
  const [tab, setTab] = useState('objects')
  const [newName, setNewName] = useState('')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [bucketFilter, setBucketFilter] = useState('')
  // Bucket-type tabs above the Buckets table (v4 survey): General
  // purpose buckets / Directory buckets. Only general-purpose buckets
  // exist in CloudForge (local MinIO has no directory-bucket type).
  const [bucketType, setBucketType] = useState<'general' | 'directory'>('general')
  // Storage Lens tabs (v4 survey): Storage Lens dashboards / Storage
  // Lens groups. Both are empty — no dashboard exists.
  const [lensTab, setLensTab] = useState<'dashboards' | 'groups'>('dashboards')
  const [notice, setNotice] = useState('')
  const [noticeTone, setNoticeTone] = useState<'success' | 'info' | 'error'>('info')

  const flash = (message: string, tone: 'success' | 'info' | 'error' = 'info') => {
    setNotice(message)
    setNoticeTone(tone)
  }
  const create = useMutation({
    mutationFn: (name: string) => createS3Bucket(name),
    onSuccess: (b) => {
      flash(`Bucket "${b.name}" created in local MinIO.`, 'success')
      setNewName('')
      setWizardOpen(false)
      setWizardStep(0)
      void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })
    },
    onError: (e: Error) => flash(`Create bucket failed: ${e.message}`, 'error'),
  })
  const del = useMutation({
    mutationFn: (name: string) => deleteS3Bucket(name),
    onSuccess: () => {
      flash('Bucket deleted.', 'success')
      setSelectedName(null)
      void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })
    },
    onError: (e: Error) => flash(`Delete bucket failed: ${e.message}`, 'error'),
  })

  const rows: (S3Bucket & { id: string })[] = (buckets.data ?? [])
    .map((b) => ({ ...b, id: b.name }))
    .filter((b) => bucketFilter.trim() === '' || b.name.toLowerCase().includes(bucketFilter.toLowerCase()))
  const selected = rows.find((b) => b.name === selectedName) ?? null
  const minioDown = buckets.isError
  const nameValid = /^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(newName.trim())

  return (
    <ServiceConsole
      icon="hard-drive"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Storage', to: '/services' }, { label: 'S3' }]}
      title="S3 — Buckets"
      description="Object storage buckets, backed by local MinIO (S3-compatible API, not AWS S3). Bucket names are local to this deployment."
      fidelity="PARTIAL"
      fidelityFootnote="buckets + objects over HTTP · MinIO-backed"
      subNav={[
        { id: 'home', label: 'S3 dashboard' },
        { id: 'buckets', label: 'Buckets' },
        { id: 'files', label: 'Files' },
        { id: 'access-points', label: 'Access points', group: 'Access management and security', disabledReason: 'Not supported by the local adapter' },
        { id: 'access-points-fsx', label: 'Access Points for FSx', group: 'Access management and security' },
        { id: 'access-grants', label: 'Access Grants', group: 'Access management and security' },
        { id: 'analyzer', label: 'IAM Access Analyzer', group: 'Access management and security' },
        { id: 'storage-management', label: 'Storage management and insights' },
        { id: 'storage-lens', label: 'Storage Lens' },
        { id: 'object-lambda', label: 'Object Lambda access points', disabledReason: 'Not modelled' },
        { id: 'batch', label: 'Batch operations', disabledReason: 'Not modelled' },
        { id: 'bpa', label: 'Block Public Access settings' },
        { id: 'replication', label: 'Replication', disabledReason: 'No replication API' },
      ]}
      activeSub={activeSub}
      onSubChange={setActiveSub}
      actions={
        <AwsButton
          variant="primary"
          disabled={minioDown}
          title={minioDown ? 'MinIO is unreachable — create would fail' : undefined}
          onClick={() => {
            setWizardOpen(true)
            setWizardStep(0)
            setActiveSub('buckets')
          }}
        >
          Create bucket
        </AwsButton>
      }
    >
      {notice !== '' && <AwsFlash tone={noticeTone}>{notice}</AwsFlash>}
      {minioDown && (
        <ErrorBox
          message={`MinIO is unreachable (${buckets.error?.message ?? 'backend unavailable'}). Bucket operations return an honest 503 until MinIO is running — no buckets are shown as a fake empty list.`}
        />
      )}

      {activeSub === 'home' && (
        <ServiceHomeDashboard
          serviceName="S3"
          banner={
            minioDown
              ? 'MinIO is unreachable — bucket operations return an honest 503 until MinIO is running; no buckets are shown as a fake empty list.'
              : 'Buckets are local to this deployment (no global S3 namespace), backed by local MinIO — object storage, not AWS S3.'
          }
          actions={[
            {
              label: 'Create bucket',
              primary: true,
              onClick: () => {
                setWizardOpen(true)
                setWizardStep(0)
                setActiveSub('buckets')
              },
              disabledReason: minioDown ? 'MinIO is unreachable — create would fail.' : undefined,
            },
            { label: 'Learning Center — storage track', to: '/learn' },
          ]}
          cards={[
            { label: 'Buckets', count: rows.length, onSelect: () => setActiveSub('buckets'), note: 'Local MinIO buckets' },
            { label: 'Objects', count: '—', onSelect: () => setActiveSub('buckets'), note: 'Counted per bucket on the Objects tab' },
            { label: 'Access Points', count: 0, disabledReason: 'Not supported by the local adapter.' },
            { label: 'Block Public Access', count: '—', onSelect: () => setActiveSub('bpa'), note: 'Account-level settings' },
          ]}
          onRefresh={() => void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })}
          refreshing={buckets.isFetching}
          health={[
            minioDown ? 'MinIO: unreachable.' : `MinIO: reachable — ${rows.length} bucket(s) listed.`,
            'Objects upload/download/delete go through the backend (10 MiB cap); object ACLs are not exposed by the MinIO adapter.',
          ]}
          settings={[
            { label: 'Resource Explorer', to: '/explorer' },
            { label: 'All services', to: '/services' },
          ]}
          info={[
            { label: 'Learning Center — storage track', to: '/learn' },
            { label: 'Backup (host-side scripts)', to: '/services/backup' },
          ]}
        />
      )}

      {activeSub === 'buckets' && (
        <div className="space-y-4">
          {wizardOpen && (
            <AwsWizard
              title="Create bucket"
              steps={CREATE_STEPS}
              current={wizardStep}
              onStep={setWizardStep}
              onCancel={() => setWizardOpen(false)}
              onSubmit={() => create.mutate(newName.trim())}
              submitLabel="Create bucket"
              busy={create.isPending}
            >
              {wizardStep === 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold">General configuration</h3>
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">Bucket name</span>
                    <input
                      value={newName}
                      onChange={(e) => setNewName(e.target.value)}
                      placeholder="my-bucket"
                      className="w-full max-w-md rounded-sm border border-aws-border-dark px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
                    />
                  </label>
                  <p className="text-sm text-aws-muted">
                    Bucket names must be 3–63 characters, lowercase letters, numbers, dots and hyphens, and start and
                    end with a letter or number. This is the local MinIO namespace, not the global AWS namespace.
                  </p>
                  {newName.trim() !== '' && !nameValid && (
                    <p className="text-sm text-aws-error" role="alert">That name does not meet the local naming rules.</p>
                  )}
                  <label className="block text-sm">
                    <span className="mb-1 block font-medium">AWS Region</span>
                    <select disabled className="rounded-sm border border-aws-border-dark bg-[#f2f3f3] px-2 py-1 text-sm">
                      <option>cf-local-1 (Local Region 1) — local only</option>
                    </select>
                  </label>
                </div>
              )}
              {wizardStep === 1 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Object Ownership</h3>
                  <p className="text-aws-muted">
                    Object ownership and ACLs are <strong>not exposed</strong> by the minio-go adapter. This step is
                    informational; it does not change a bucket setting.
                  </p>
                </div>
              )}
              {wizardStep === 2 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Block Public Access settings for this bucket</h3>
                  <p className="text-aws-muted">
                    CloudForge does not implement S3 public-access blocks. Buckets are reachable only through the
                    authenticated backend; there is no public endpoint and no ACL surface.
                  </p>
                </div>
              )}
              {wizardStep === 3 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Bucket Versioning</h3>
                  <p className="text-aws-muted">
                    Versioning can be enabled after creation on the bucket's Properties tab, where it is a real
                    MinIO-backed toggle. It is not set during this wizard.
                  </p>
                </div>
              )}
              {wizardStep === 4 && (
                <div className="space-y-2 text-sm">
                  <h3 className="text-sm font-semibold">Summary</h3>
                  <dl className="divide-y divide-aws-border border-y border-aws-border">
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Bucket name</dt><dd>{newName.trim() || '—'}</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Region</dt><dd>cf-local-1 (local only)</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Backend</dt><dd>Local MinIO — API_EMULATED</dd></div>
                    <div className="flex gap-2 px-1 py-2"><dt className="w-48 shrink-0 font-medium">Versioning</dt><dd>Off (enable later on Properties)</dd></div>
                  </dl>
                  <p className="text-aws-muted">Creating a bucket is a real MinIO operation through the backend.</p>
                </div>
              )}
            </AwsWizard>
          )}
          {wizardOpen && (
            <p className="text-xs text-aws-muted">
              Wizard steps inferred from the console&apos;s Create bucket action, not observed — the 2026-11-22
              survey opened no create wizard, so no AWS wizard steps were recorded.
            </p>
          )}

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <AwsStat label="Buckets" value={rows.length} hint="Local MinIO" />
            <AwsStat label="Selected" value={checked.length} />
            <AwsStat label="Backend" value="MinIO" hint="API_EMULATED" />
            <AwsStat label="Region" value="cf-local-1" hint="Local only" />
          </div>

          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={() => void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })} busy={buckets.isFetching} />
                <AwsPreferencesButton disabledReason="Table preferences are not persisted in CloudForge." />
              </>
            }
          >
            <AwsActionsMenu
              label="Actions"
              items={[
                {
                  label: 'Delete',
                  danger: true,
                  onClick: () => selectedName && del.mutate(selectedName),
                  disabledReason: selectedName === null ? 'Select a bucket first' : undefined,
                },
                { label: 'Empty bucket', disabledReason: 'No bulk-empty operation — delete objects on the Objects tab' },
                { label: 'Copy bucket name', onClick: () => selectedName && void navigator.clipboard?.writeText(selectedName), disabledReason: selectedName === null ? 'Select a bucket first' : undefined },
                { label: 'Create access point', disabledReason: 'Not supported by the local adapter' },
              ]}
            />
          </AwsToolbar>

          <ConsoleTabs
            tabs={[
              { id: 'general', label: 'General purpose buckets' },
              { id: 'directory', label: 'Directory buckets' },
            ]}
            active={bucketType}
            onChange={(id) => setBucketType(id as 'general' | 'directory')}
          />
          {bucketType === 'directory' && (
            <AwsCard title="Directory buckets">
              <p className="text-sm text-aws-muted">
                Directory buckets are <strong>UNSUPPORTED</strong> in CloudForge — local MinIO has no
                directory-bucket type, so no directory bucket can exist or be created. All CloudForge buckets are
                general purpose buckets, listed on the General purpose buckets tab.
              </p>
            </AwsCard>
          )}
          {buckets.isPending && bucketType === 'general' && <Loading label="Loading buckets" />}
          {buckets.data && bucketType === 'general' && (
            <AwsCard>
              <AwsTable<S3Bucket & { id: string }>
                caption="S3 buckets"
                emptyMessage="No buckets yet — choose Create bucket to make one."
                rows={rows}
                totalCount={(buckets.data ?? []).length}
                filter={bucketFilter}
                onFilterChange={setBucketFilter}
                filterPlaceholder="Filter buckets by name"
                selectable
                selectedIds={checked}
                onToggleRow={(id) => {
                  setSelectedName(id)
                  setChecked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
                }}
                onToggleAll={(all) => setChecked(all ? rows.map((r) => r.id) : [])}
                columns={[
                  {
                    header: 'Name',
                    render: (b) => (
                      <button
                        type="button"
                        className="text-aws-link hover:underline"
                        onClick={() => {
                          setSelectedName(b.name)
                          setTab('objects')
                        }}
                      >
                        {b.name}
                      </button>
                    ),
                  },
                  { header: 'AWS Region', render: () => 'cf-local-1' },
                  { header: 'IAM Access Analyzer', render: () => <span title="No Access Analyzer data exists in CloudForge">Not available</span> },
                  { header: 'Creation date', render: (b) => (b.created_at ? new Date(b.created_at).toLocaleString() : '—') },
                  {
                    header: 'Actions',
                    render: (b) => (
                      <span className="flex gap-2">
                        <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelectedName(b.name); setTab('objects') }}>Open</button>
                        <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(b.name)}>Delete</button>
                      </span>
                    ),
                  },
                ]}
              />
              <p className="mt-2 text-sm text-aws-muted">
                Deleting a bucket only works when it is empty (MinIO refuses non-empty buckets) — delete its objects
                first.
              </p>
            </AwsCard>
          )}

          {selected && bucketType === 'general' && (
            <AwsCard>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h2 className="text-base font-semibold">Bucket {selected.name}</h2>
                  <p className="text-xs text-aws-muted">
                    Local MinIO bucket · not AWS S3 · <AwsCopy value={selected.name} label="Copy bucket name" />
                  </p>
                </div>
                <button type="button" className="text-sm text-aws-link hover:underline" onClick={() => setSelectedName(null)}>
                  Close
                </button>
              </div>
              <ConsoleTabs
                tabs={[
                  { id: 'objects', label: 'Objects' },
                  { id: 'properties', label: 'Properties' },
                  { id: 'permissions', label: 'Permissions' },
                  { id: 'management', label: 'Management' },
                  { id: 'metrics', label: 'Metrics' },
                ]}
                active={tab}
                onChange={setTab}
              />
              <div className="pt-3">
                {tab === 'objects' && <ObjectsTab bucket={selected.name} />}
                {tab === 'properties' && <PropertiesTab bucket={selected} />}
                {tab === 'permissions' && <PermissionsTab bucket={selected.name} />}
                {tab === 'management' && <ManagementTab bucket={selected.name} />}
                {tab === 'metrics' && (
                  <p className="text-sm text-aws-muted">
                    No per-bucket metrics pipeline exists yet. Control-plane metrics are on the{' '}
                    <Link to="/services/cloudwatch" className="text-aws-link hover:underline">CloudWatch console</Link>.
                  </p>
                )}
              </div>
            </AwsCard>
          )}
        </div>
      )}

      {activeSub === 'files' && (
        <div className="space-y-4">
          <AwsCard title="File systems">
            <p className="text-sm text-aws-muted">
              Files is <strong>UNSUPPORTED</strong> in CloudForge — the local MinIO adapter exposes buckets and
              objects only. There is no S3 file-system endpoint: no file system exists, nothing can be mounted, and
              the backend implements no file-system API. The 2026-11-22 survey records &ldquo;Files (File
              systems)&rdquo; as an S3 left-nav item only — no table columns were recorded, so no table is shown
              here and no file system is invented.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="Files is UNSUPPORTED — no file-system API.">
                Create file system
              </AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="What you can do instead">
            <ul className="space-y-1 text-sm">
              <li>
                <Link to="/services/efs" className="text-aws-link hover:underline">EFS</Link>
                <span className="text-aws-muted"> — file systems recorded as teaching records (no real NFS mount).</span>
              </li>
              <li>
                <Link to="/services/fsx" className="text-aws-link hover:underline">FSx</Link>
                <span className="text-aws-muted"> — file systems recorded as teaching records (no real volume exists).</span>
              </li>
              <li>
                <button type="button" className="text-aws-link hover:underline" onClick={() => setActiveSub('buckets')}>
                  Buckets
                </button>
                <span className="text-aws-muted"> — objects in local MinIO buckets are the real storage in this deployment.</span>
              </li>
            </ul>
          </AwsCard>
        </div>
      )}

      {activeSub === 'access-points-fsx' && (
        <div className="space-y-4">
          <AwsCard title="Access Points for FSx">
            <p className="text-sm text-aws-muted">
              Access Points for FSx are <strong>UNSUPPORTED</strong> in CloudForge — there is no FSx access-point
              API and no FSx file system for an access point to attach to. The v4 survey records &ldquo;Access
              Points for FSx&rdquo; as an S3 left-nav item only — no table was recorded, so no access point is
              shown here and none is invented.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="Access Points for FSx are UNSUPPORTED — no FSx access-point API.">
                Create access point
              </AwsButton>
            </div>
          </AwsCard>
        </div>
      )}

      {activeSub === 'access-grants' && (
        <div className="space-y-4">
          <AwsCard title="Access Grants">
            <p className="text-sm text-aws-muted">
              Access Grants is <strong>UNSUPPORTED</strong> in CloudForge — the backend has no access-grants API.
              No grant exists, no grant can be created, and grants do not change access to any bucket. The
              2026-11-22 survey records &ldquo;Access Grants&rdquo; as an S3 left-nav item only — no table columns
              were recorded, so no grant list is shown here and no grant is invented. The v4 survey adds the
              prerequisite for the AWS getting-started flow: an IAM Identity Center instance in this Region must
              exist first — CloudForge has no Identity Center either.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="Access Grants is UNSUPPORTED — no access-grants API.">
                Create access grant
              </AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="How bucket access works here instead">
            <ul className="list-disc space-y-1 pl-5 text-sm text-aws-muted">
              <li>Bucket policies are per-bucket, on each bucket&apos;s Permissions tab (real MinIO operation).</li>
              <li>The backend enforces authentication on every object request; there is no public bucket endpoint.</li>
              <li>
                Identities are managed in the{' '}
                <Link to="/services/iam" className="text-aws-link hover:underline">IAM console</Link>.
              </li>
            </ul>
          </AwsCard>
        </div>
      )}

      {activeSub === 'analyzer' && (
        <div className="space-y-4">
          <AwsCard title="IAM Access Analyzer">
            <p className="text-sm text-aws-muted">
              IAM Access Analyzer is <strong>UNSUPPORTED</strong> in CloudForge — no analyzer runs in this
              deployment and no findings exist. The Buckets table&apos;s IAM Access Analyzer column is always
              &ldquo;Not available&rdquo; for the same reason. The 2026-11-22 survey records &ldquo;IAM Access
              Analyzer&rdquo; as an S3 left-nav item only — no findings table was recorded, so no findings are
              shown here and none are invented.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="IAM Access Analyzer is UNSUPPORTED — no analyzer runs.">
                Create analyzer
              </AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="What you can check instead">
            <ul className="list-disc space-y-1 pl-5 text-sm text-aws-muted">
              <li>
                Each bucket&apos;s policy is visible on its{' '}
                <button type="button" className="text-aws-link hover:underline" onClick={() => setActiveSub('buckets')}>
                  Buckets
                </button>{' '}
                detail Permissions tab — review it for public access.
              </li>
              <li>
                The{' '}
                <Link to="/services/securityhub" className="text-aws-link hover:underline">Security Hub console</Link>{' '}
                lists findings from real checks, including public S3 bucket policies.
              </li>
            </ul>
          </AwsCard>
        </div>
      )}

      {activeSub === 'storage-management' && <StorageManagementSection />}

      {activeSub === 'storage-lens' && (
        <div className="space-y-4">
          <AwsCard title="Storage Lens">
            <p className="text-sm text-aws-muted">
              Storage Lens is <strong>UNSUPPORTED</strong> in CloudForge — there is no metrics pipeline for bucket
              storage in this deployment, so no Storage Lens dashboard can be built and no storage metrics are
              collected. The v4 survey records 0 dashboards and the tabs Storage Lens dashboards / Storage Lens
              groups with the columns Dashboard name and Status — the empty table below uses exactly those
              columns; no metrics are invented.
            </p>
            <div className="mt-3">
              <AwsButton disabled title="Storage Lens is UNSUPPORTED — no storage-metrics pipeline.">
                Create dashboard
              </AwsButton>
            </div>
          </AwsCard>
          <AwsCard title="Storage Lens dashboards">
            <ConsoleTabs
              tabs={[
                { id: 'dashboards', label: 'Storage Lens dashboards' },
                { id: 'groups', label: 'Storage Lens groups' },
              ]}
              active={lensTab}
              onChange={(id) => setLensTab(id as 'dashboards' | 'groups')}
            />
            <div className="pt-3">
              {lensTab === 'dashboards' ? (
                <AwsTable<{ id: string }>
                  caption="Storage Lens dashboards"
                  emptyMessage="No Storage Lens dashboards — create one above (disabled: no storage-metrics pipeline)."
                  rows={[]}
                  totalCount={0}
                  columns={[
                    { header: 'Dashboard name', render: () => '—' },
                    { header: 'Status', render: () => '—' },
                  ]}
                />
              ) : (
                <p className="text-sm text-aws-muted">
                  No Storage Lens groups — groups are created from dashboards, and no dashboard exists.
                </p>
              )}
            </div>
          </AwsCard>
          <AwsCard title="What you can check instead">
            <ul className="list-disc space-y-1 pl-5 text-sm text-aws-muted">
              <li>
                Per-bucket object counts and sizes are on each bucket&apos;s{' '}
                <button type="button" className="text-aws-link hover:underline" onClick={() => setActiveSub('buckets')}>
                  Buckets
                </button>{' '}
                detail Objects tab.
              </li>
              <li>
                Control-plane metrics are on the{' '}
                <Link to="/services/cloudwatch" className="text-aws-link hover:underline">CloudWatch console</Link>.
              </li>
            </ul>
          </AwsCard>
        </div>
      )}

      {activeSub === 'bpa' && (
        <div className="space-y-3">
          <AwsFlash tone="info">
            CloudForge has no S3 Block Public Access surface. Buckets are reachable only through the authenticated
            backend; there is no public bucket endpoint to block, and this page does not fake account settings.
          </AwsFlash>
          <AwsCard title="What this means">
            <ul className="list-disc space-y-1 pl-5 text-sm text-aws-muted">
              <li>No bucket ACLs are exposed by the minio-go adapter.</li>
              <li>Bucket policies are per-bucket and live on each bucket's Permissions tab.</li>
              <li>The backend enforces auth on every object request.</li>
            </ul>
          </AwsCard>
        </div>
      )}
    </ServiceConsole>
  )
}

function ObjectsTab({ bucket }: { bucket: string }) {
  const queryClient = useQueryClient()
  const [prefix, setPrefix] = useState('')
  const [notice, setNotice] = useState('')
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const objects = useQuery({
    queryKey: ['s3-objects', bucket, prefix],
    queryFn: () => fetchS3Objects(bucket, prefix),
    retry: 1,
  })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['s3-objects', bucket] })
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const bytes = await file.arrayBuffer()
      return uploadS3Object(bucket, prefix + file.name, bytes, file.type === '' ? undefined : file.type)
    },
    onSuccess: (o) => {
      setNotice(`Uploaded "${o.key}" (${o.size} bytes).`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Upload failed: ${e.message}`),
  })
  const remove = useMutation({
    mutationFn: (key: string) => deleteS3Object(bucket, key),
    onSuccess: () => {
      setNotice('Object deleted.')
      setSelectedKey(null)
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete failed: ${e.message}`),
  })
  const download = useMutation({
    mutationFn: async (key: string) => {
      const blob = await downloadS3Object(bucket, key)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = key.split('/').pop() ?? key
      a.click()
      URL.revokeObjectURL(url)
    },
    onError: (e: Error) => setNotice(`Download failed: ${e.message}`),
  })
  const rows: (S3Object & { id: string })[] = (objects.data ?? []).map((o) => ({ ...o, id: o.key }))
  const crumbs = prefix === '' ? [] : prefix.split('/').filter((p) => p !== '')
  const selectedObject = rows.find((o) => o.key === selectedKey) ?? null
  return (
    <div className="space-y-3">
      {notice !== '' && <AwsFlash tone={notice.startsWith('Uploaded') || notice === 'Object deleted.' ? 'success' : 'error'}>{notice}</AwsFlash>}
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <button type="button" className="text-aws-link hover:underline" onClick={() => setPrefix('')}>{bucket}</button>
        {crumbs.map((c, i) => (
          <span key={`${c}-${i}`}>
            {' / '}
            <button
              type="button"
              className="text-aws-link hover:underline"
              onClick={() => setPrefix(`${crumbs.slice(0, i + 1).join('/')}/`)}
            >
              {c}
            </button>
          </span>
        ))}
      </div>
      <AwsToolbar
        right={<AwsRefreshButton onClick={refresh} busy={objects.isFetching} />}
      >
        <AwsActionsMenu
          label="Actions"
          items={[
            { label: 'Download', onClick: () => selectedKey && download.mutate(selectedKey), disabledReason: selectedKey === null ? 'Select an object first' : undefined },
            { label: 'Copy S3 URI', onClick: () => selectedKey && void navigator.clipboard?.writeText(`s3://${bucket}/${selectedKey}`), disabledReason: selectedKey === null ? 'Select an object first' : undefined },
            { label: 'Delete', danger: true, onClick: () => selectedKey && remove.mutate(selectedKey), disabledReason: selectedKey === null ? 'Select an object first' : undefined },
            { label: 'Open', onClick: () => selectedKey && setSelectedKey(selectedKey), disabledReason: selectedKey === null ? 'Select an object first' : undefined },
          ]}
        />
        <input
          ref={fileRef}
          type="file"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) upload.mutate(f)
            e.target.value = ''
          }}
        />
        <AwsButton onClick={() => fileRef.current?.click()}>Upload</AwsButton>
      </AwsToolbar>
      <p className="text-sm text-aws-muted">
        Uploads go through the backend (base64 JSON, 10 MiB cap) — the browser never holds MinIO credentials.
      </p>
      {objects.isPending && <Loading label="Loading objects" />}
      {objects.isError && <ErrorBox message={`Objects could not be loaded (${objects.error.message}).`} />}
      {objects.data && (
        <AwsTable<S3Object & { id: string }>
          caption="Objects"
          emptyMessage={prefix === '' ? 'No objects in this bucket yet — upload one above.' : 'No objects under this prefix.'}
          rows={rows}
          totalCount={rows.length}
          selectable
          selectedIds={selectedKey ? [selectedKey] : []}
          onToggleRow={(id) => setSelectedKey((prev) => (prev === id ? null : id))}
          onToggleAll={() => setSelectedKey(null)}
          columns={[
            {
              header: 'Key',
              render: (o) =>
                o.is_prefix ? (
                  <button type="button" className="text-aws-link hover:underline" onClick={() => setPrefix(o.key)}>{o.key} (folder)</button>
                ) : (
                  <button type="button" className="text-aws-link hover:underline" onClick={() => setSelectedKey(o.key)}>{o.key}</button>
                ),
            },
            { header: 'Size', render: (o) => (o.is_prefix ? '—' : `${o.size} B`) },
            { header: 'Last modified', render: (o) => (o.last_modified ? new Date(o.last_modified).toLocaleString() : '—') },
            { header: 'Type', render: (o) => o.content_type ?? '—' },
            {
              header: 'Actions',
              render: (o) =>
                o.is_prefix ? (
                  '—'
                ) : (
                  <span className="flex gap-2">
                    <button type="button" className="text-aws-link hover:underline" onClick={() => download.mutate(o.key)}>Download</button>
                    <button type="button" className="text-aws-link hover:underline" onClick={() => remove.mutate(o.key)}>Delete</button>
                  </span>
                ),
            },
          ]}
        />
      )}
      {selectedObject && (
        <div className="flex items-center gap-3 border border-aws-border bg-[#f7f8f8] px-3 py-2 text-sm">
          <span className="font-medium">{selectedObject.key}</span>
          <AwsCopy value={`s3://${bucket}/${selectedObject.key}`} label="Copy S3 URI" />
        </div>
      )}
      {selectedKey && <ObjectDetail bucket={bucket} objectKey={selectedKey} onClose={() => setSelectedKey(null)} />}
    </div>
  )
}

function ObjectDetail({ bucket, objectKey, onClose }: { bucket: string; objectKey: string; onClose: () => void }) {
  const meta = useQuery({ queryKey: ['s3-meta', bucket, objectKey], queryFn: () => fetchS3ObjectMetadata(bucket, objectKey), retry: 1 })
  return (
    <div className="border-t border-aws-border pt-3">
      <h3 className="text-sm font-semibold">Object {objectKey}</h3>
      {meta.isPending && <Loading label="Loading object metadata" />}
      {meta.isError && <ErrorBox message={`Metadata could not be loaded (${meta.error.message}).`} />}
      {meta.data && (
        <dl className="mt-2 space-y-1 text-sm">
          <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Key</dt><dd className="break-all">{meta.data.key}</dd></div>
          <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Size</dt><dd>{meta.data.size} bytes</dd></div>
          <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Content type</dt><dd>{meta.data.content_type ?? '—'}</dd></div>
          <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">ETag</dt><dd className="break-all font-mono text-xs">{meta.data.etag ?? '—'}</dd></div>
          <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Last modified</dt><dd>{meta.data.last_modified ? new Date(meta.data.last_modified).toLocaleString() : '—'}</dd></div>
          {meta.data.user_metadata && Object.keys(meta.data.user_metadata).length > 0 && (
            <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">User metadata</dt><dd>{Object.entries(meta.data.user_metadata).map(([k, v]) => `${k}=${v}`).join(', ')}</dd></div>
          )}
        </dl>
      )}
      <div className="mt-2">
        <AwsButton onClick={onClose}>Close</AwsButton>
      </div>
    </div>
  )
}

function PropertiesTab({ bucket }: { bucket: S3Bucket }) {
  const queryClient = useQueryClient()
  const [notice, setNotice] = useState('')
  const versioning = useQuery({ queryKey: ['s3-versioning', bucket.name], queryFn: () => fetchS3Versioning(bucket.name), retry: 1 })
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => setS3Versioning(bucket.name, enabled),
    onSuccess: (enabled) => {
      setNotice(`Versioning ${enabled ? 'enabled' : 'suspended'}.`)
      void queryClient.invalidateQueries({ queryKey: ['s3-versioning', bucket.name] })
    },
    onError: (e: Error) => setNotice(`Versioning change failed: ${e.message}`),
  })
  return (
    <div className="space-y-3">
      {notice !== '' && <AwsFlash tone={notice.includes('failed') ? 'error' : 'success'}>{notice}</AwsFlash>}
      <dl className="space-y-2 text-sm">
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Bucket name</dt><dd>{bucket.name}</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Created</dt><dd>{bucket.created_at ? new Date(bucket.created_at).toLocaleString() : '—'}</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Backend</dt><dd>Local MinIO — API_EMULATED, not AWS S3</dd></div>
        <div className="flex gap-2"><dt className="w-32 shrink-0 font-medium">Region</dt><dd>cf-local-1</dd></div>
      </dl>
      <div>
        <h3 className="text-sm font-semibold">Bucket Versioning</h3>
        {versioning.isPending && <Loading label="Loading versioning state" />}
        {versioning.isError && <ErrorBox message={`Versioning state could not be loaded (${versioning.error.message}).`} />}
        {versioning.data !== undefined && (
          <p className="text-sm">
            Versioning is <strong>{versioning.data ? 'Enabled' : 'Suspended'}</strong>.{' '}
            <AwsButton onClick={() => toggle.mutate(!versioning.data)}>{versioning.data ? 'Suspend versioning' : 'Enable versioning'}</AwsButton>
          </p>
        )}
      </div>
    </div>
  )
}

function PermissionsTab({ bucket }: { bucket: string }) {
  const queryClient = useQueryClient()
  const [edited, setEdited] = useState('')
  const [touched, setTouched] = useState(false)
  const [notice, setNotice] = useState('')
  const policy = useQuery({
    queryKey: ['s3-policy', bucket],
    queryFn: () => fetchS3BucketPolicy(bucket),
    retry: 1,
  })
  const policyText = touched ? edited : (policy.data ?? '')
  const save = useMutation({
    mutationFn: () => setS3BucketPolicy(bucket, policyText),
    onSuccess: () => {
      setNotice('Bucket policy saved.')
      void queryClient.invalidateQueries({ queryKey: ['s3-policy', bucket] })
    },
    onError: (e: Error) => setNotice(`Save failed: ${e.message}`),
  })
  return (
    <div className="space-y-3">
      <p className="text-sm text-aws-muted">
        Bucket policy (JSON) — edited where the adapter supports it. Object ACLs are <strong>not available</strong>:
        the minio-go adapter exposes no Get/SetBucketACL surface, so ACLs are not shown or editable here.
      </p>
      {notice !== '' && <AwsFlash tone={notice.includes('failed') ? 'error' : 'success'}>{notice}</AwsFlash>}
      {policy.isPending && <Loading label="Loading bucket policy" />}
      {policy.isError && <ErrorBox message={`Bucket policy could not be loaded (${policy.error.message}).`} />}
      {policy.data !== undefined && (
        <form
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <label htmlFor="s3-policy" className="block text-sm font-medium">Bucket policy (JSON)</label>
          <textarea
            id="s3-policy"
            value={policyText}
            onChange={(e) => {
              setEdited(e.target.value)
              setTouched(true)
            }}
            rows={10}
            placeholder='{"Version":"2012-10-17","Statement":[]}'
            className="w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 font-mono text-xs focus:border-aws-link focus:outline-none"
          />
          <AwsButton type="submit">Save policy</AwsButton>
        </form>
      )}
    </div>
  )
}

function ManagementTab({ bucket }: { bucket: string }) {
  const versioning = useQuery({ queryKey: ['s3-versioning', bucket], queryFn: () => fetchS3Versioning(bucket), retry: 1 })
  return (
    <div className="space-y-2 text-sm">
      <p>
        Versioning: <strong>{versioning.data === undefined ? 'unknown' : versioning.data ? 'Enabled' : 'Suspended'}</strong>{' '}
        (toggle on the Properties tab).
      </p>
      <p className="text-aws-muted">
        No replication or lifecycle rules: MinIO defaults apply and there is no lifecycle API in this deployment.
      </p>
    </div>
  )
}

/**
 * Storage management and insights — PARTIAL. A real per-bucket table:
 * versioning is a real backend read per bucket; replication, lifecycle
 * and inventory honestly have no API, so those columns say so instead
 * of inventing rules. The survey records this nav item only — no AWS
 * columns were recorded, so these columns are CloudForge's own and are
 * not claimed to match AWS.
 */
function StorageManagementSection() {
  const queryClient = useQueryClient()
  const buckets = useQuery({ queryKey: ['s3-buckets'], queryFn: fetchS3Buckets, retry: 1 })
  const rows = (buckets.data ?? []).map((b) => ({ ...b, id: b.name }))
  const versioning = useQueries({
    queries: rows.map((b) => ({
      queryKey: ['s3-versioning', b.name],
      queryFn: () => fetchS3Versioning(b.name),
      retry: 1,
    })),
  })
  const versioningByBucket = new Map(rows.map((b, i) => [b.name, versioning[i]]))
  return (
    <div className="space-y-4">
      <AwsCard title="Storage management and insights">
        <p className="mb-3 text-sm text-aws-muted">
          Management state per bucket. Versioning is real (read from the backend per bucket; toggle it on the
          bucket&apos;s Properties tab). Replication, lifecycle rules and inventory have no API in this deployment,
          so those columns say so — no rules are invented.
        </p>
        <AwsToolbar
          right={<AwsRefreshButton onClick={() => void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })} busy={buckets.isFetching} />}
        >
          <span className="text-sm text-aws-muted">PARTIAL — versioning real; replication / lifecycle / inventory not available.</span>
        </AwsToolbar>
        {buckets.isPending && <Loading label="Loading buckets" />}
        {buckets.isError && (
          <ErrorBox message={`Bucket management state could not be loaded (${buckets.error.message}).`} />
        )}
        {buckets.data && (
          <AwsTable<S3Bucket & { id: string }>
            caption="Bucket storage management"
            emptyMessage="No buckets yet — management state is listed per bucket once one exists."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Bucket', render: (b) => b.name },
              {
                header: 'Versioning',
                render: (b) => {
                  const v = versioningByBucket.get(b.name)
                  if (v?.isPending) return 'Loading…'
                  if (v?.isError) return 'Not available'
                  return v?.data ? 'Enabled' : 'Suspended'
                },
              },
              { header: 'Replication rules', render: () => 'None — no replication API' },
              { header: 'Lifecycle rules', render: () => 'None — no lifecycle API' },
              { header: 'Inventory', render: () => 'Not available — no inventory' },
            ]}
          />
        )}
      </AwsCard>
    </div>
  )
}
