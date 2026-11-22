import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { FidelityBadge } from '../components/AwsBadge.tsx'
import { ConsoleTabs } from '../components/ServiceConsole.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'
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
 * S3 console (Phase 2) — mirrors the AWS S3 Buckets console: Buckets
 * list, bucket detail tabs Objects / Properties / Permissions /
 * Management / Metrics, Create-bucket wizard. Buckets are real local
 * MinIO buckets (API_EMULATED); objects list/upload/download/delete
 * through the backend (no direct browser→MinIO credentials — uploads
 * are base64 JSON, capped at 10 MiB by the backend). Versioning is a
 * real toggle where the adapter supports it; Permissions shows and
 * edits the bucket policy — object ACLs are NOT exposed by the
 * minio-go adapter, and the tab says so. MinIO down is an honest
 * 503, never a fake empty list.
 */
export function S3Console() {
  const queryClient = useQueryClient()
  const buckets = useQuery({ queryKey: ['s3-buckets'], queryFn: fetchS3Buckets, retry: 1 })
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [tab, setTab] = useState('objects')
  const [newName, setNewName] = useState('')
  const [notice, setNotice] = useState('')

  const create = useMutation({
    mutationFn: (name: string) => createS3Bucket(name),
    onSuccess: (b) => {
      setNotice(`Bucket "${b.name}" created in local MinIO.`)
      setNewName('')
      void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })
    },
    onError: (e: Error) => setNotice(`Create bucket failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (name: string) => deleteS3Bucket(name),
    onSuccess: () => {
      setNotice('Bucket deleted.')
      setSelectedName(null)
      void queryClient.invalidateQueries({ queryKey: ['s3-buckets'] })
    },
    onError: (e: Error) => setNotice(`Delete bucket failed: ${e.message}`),
  })

  const rows: (S3Bucket & { id: string })[] = (buckets.data ?? []).map((b) => ({ ...b, id: b.name }))
  const selected = rows.find((b) => b.name === selectedName) ?? null
  const minioDown = buckets.isError

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Storage', to: '/services' }, { label: 'S3' }]}
        title="S3 — Buckets"
        description="Object storage buckets, backed by local MinIO (S3-compatible API, not AWS S3). Bucket names are local to this deployment."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="buckets + objects over HTTP · MinIO-backed" />}
      />
      {notice !== '' && (
        <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">
          {notice}
        </p>
      )}
      {minioDown && (
        <ErrorBox
          message={`MinIO is unreachable (${buckets.error?.message ?? 'backend unavailable'}). Bucket operations return an honest 503 until MinIO is running — no buckets are shown as a fake empty list.`}
        />
      )}
      <AwsCard
        title="Create bucket"
        actions={<FidelityBadge fidelity="API_EMULATED" footnote="local MinIO" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (newName.trim() !== '') create.mutate(newName.trim())
          }}
          className="flex flex-wrap items-center gap-2"
        >
          <label htmlFor="bucket-name" className="text-sm font-medium">
            Bucket name
          </label>
          <input
            id="bucket-name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="my-bucket"
            disabled={minioDown}
            className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none disabled:opacity-50"
          />
          <AwsButton type="submit" disabled={minioDown || newName.trim() === ''}>
            Create bucket
          </AwsButton>
        </form>
      </AwsCard>
      {buckets.isPending && <Loading label="Loading buckets" />}
      {buckets.data && (
        <AwsCard title={`Buckets (${rows.length})`}>
          <AwsTable<S3Bucket & { id: string }>
            caption="S3 buckets"
            emptyMessage="No buckets yet — create one above."
            rows={rows}
            totalCount={rows.length}
            columns={[
              { header: 'Bucket name', render: (b) => <button type="button" className="text-aws-link hover:underline" onClick={() => { setSelectedName(b.name); setTab('objects') }}>{b.name}</button> },
              { header: 'Created', render: (b) => (b.created_at ? new Date(b.created_at).toLocaleString() : '—') },
              { header: 'Region', render: () => 'cf-local-1' },
              { header: 'Fidelity', render: () => <FidelityBadge fidelity="API_EMULATED" footnote="" /> },
              {
                header: 'Actions',
                render: (b) => (
                  <button type="button" className="text-aws-link hover:underline" onClick={() => del.mutate(b.name)}>
                    Delete
                  </button>
                ),
              },
            ]}
          />
          <p className="mt-2 text-sm text-aws-muted">Deleting a bucket only works when it is empty (MinIO refuses non-empty buckets) — delete its objects first.</p>
        </AwsCard>
      )}
      {selected && (
        <AwsCard title={`Bucket ${selected.name}`}>
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
  return (
    <div className="space-y-3">
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <button type="button" className="text-aws-link hover:underline" onClick={() => setPrefix('')}>{bucket}</button>
        {crumbs.map((c, i) => (
          <span key={c}>
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
      <div className="flex flex-wrap items-center gap-2">
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
        <AwsButton onClick={() => fileRef.current?.click()}>Upload object</AwsButton>
        <span className="text-sm text-aws-muted">Uploads go through the backend (base64 JSON, 10 MiB cap) — the browser never holds MinIO credentials.</span>
      </div>
      {objects.isPending && <Loading label="Loading objects" />}
      {objects.isError && <ErrorBox message={`Objects could not be loaded (${objects.error.message}).`} />}
      {objects.data && (
        <AwsTable<S3Object & { id: string }>
          caption="Objects"
          emptyMessage={prefix === '' ? 'No objects in this bucket yet — upload one above.' : 'No objects under this prefix.'}
          rows={rows}
          totalCount={rows.length}
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
      {notice !== '' && <p className="text-sm" role="status">{notice}</p>}
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
  // The textarea shows the server value until the user edits it
  // (derived during render — no effect copying query data into state).
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
      {notice !== '' && <p className="text-sm" role="status">{notice}</p>}
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
