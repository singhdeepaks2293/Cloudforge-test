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
const num = (r: Rec, k: string): number => (typeof r[k] === 'number' ? (r[k] as number) : 0)

/**
 * ECR console (Phase 4) — mirrors the AWS ECR console's Repositories
 * list and repository detail (Images with tag/digest/size, image
 * detail with layers, scan results, lifecycle rules). The registry is
 * a real local registry: a Postgres-backed catalogue with
 * content-addressed blobs on local disk, speaking the Docker Registry
 * HTTP API subset (manifest + blob upload/download, tag listing) at
 * /v2/. The scan is an integrity scan (every layer re-read and
 * digest-verified) — NOT a vulnerability (CVE) scan. No Docker Hub
 * compatibility is claimed; chunked/resumable uploads and OCI indexes
 * are not implemented.
 */
export function EcrConsole() {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [keepN, setKeepN] = useState(10)
  const [expireDays, setExpireDays] = useState(0)
  const [selected, setSelected] = useState('')
  const [tag, setTag] = useState('latest')
  const [layerText, setLayerText] = useState('hello from CloudForge ECR')
  const [imageId, setImageId] = useState('')
  const [notice, setNotice] = useState('')

  const repos = useQuery({ queryKey: ['ecr-repos'], queryFn: () => request<Rec>('/api/v1/aws/ecr/repositories'), retry: 1 })
  const repoRows = listOf(repos.data, 'repositories')
  const images = useQuery({
    queryKey: ['ecr-images', selected],
    queryFn: () => request<Rec>(`/api/v1/aws/ecr/repositories/${selected}/images`),
    enabled: selected !== '',
    retry: 1,
  })
  const imageRows = listOf(images.data, 'images')
  const selRepo = repoRows.find((r) => String(r.id) === selected)
  const selImage = imageRows.find((r) => String(r.id) === imageId)

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['ecr-repos'] })
    void queryClient.invalidateQueries({ queryKey: ['ecr-images', selected] })
  }
  const create = useMutation({
    mutationFn: () =>
      request<Rec>('/api/v1/aws/ecr/repositories', {
        method: 'POST',
        body: {
          name: name.trim(),
          lifecycle_rules:
            keepN > 0 || expireDays > 0
              ? [{ keep_last_n_tagged: keepN, expire_untagged_days: expireDays }]
              : undefined,
        },
      }),
    onSuccess: () => {
      setNotice('Repository created.')
      setName('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Create repository failed: ${e.message}`),
  })
  const del = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecr/repositories/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Repository deleted.')
      if (selected !== '') setSelected('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete repository failed: ${e.message}`),
  })
  const push = useMutation({
    mutationFn: () => {
      const blob = btoa(unescape(encodeURIComponent(layerText)))
      return request<Rec>(`/api/v1/aws/ecr/repositories/${selected}/images`, {
        method: 'POST',
        body: { tag: tag.trim(), layers: [blob] },
      })
    },
    onSuccess: () => {
      setNotice('Image pushed (layer stored as a content-addressed blob).')
      refresh()
    },
    onError: (e: Error) => setNotice(`Push failed: ${e.message}`),
  })
  const scan = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecr/images/${id}/scan`, { method: 'POST' }),
    onSuccess: (d) => {
      setNotice(`Scan ${String(d.scan_status ?? '')}: ${String(d.scan_note ?? '')}`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Scan failed: ${e.message}`),
  })
  const lifecycle = useMutation({
    mutationFn: () => request<Rec>(`/api/v1/aws/ecr/repositories/${selected}/lifecycle`, { method: 'POST' }),
    onSuccess: (d) => {
      setNotice(`Lifecycle evaluated: ${String(d.deleted ?? 0)} image(s) deleted, ${String(d.kept ?? 0)} kept.`)
      refresh()
    },
    onError: (e: Error) => setNotice(`Lifecycle evaluation failed: ${e.message}`),
  })
  const delImage = useMutation({
    mutationFn: (id: string) => request<Rec>(`/api/v1/aws/ecr/images/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setNotice('Image deleted.')
      setImageId('')
      refresh()
    },
    onError: (e: Error) => setNotice(`Delete image failed: ${e.message}`),
  })

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Containers', to: '/services' }, { label: 'ECR' }]}
        title="ECR — Repositories"
        description="Local container registry: Postgres catalogue + content-addressed blobs on disk, speaking the Docker Registry HTTP API subset at /v2/. Scan is an integrity scan, not a CVE scan."
        actions={<FidelityBadge fidelity="PARTIAL" footnote="local registry · no Docker Hub · no chunked uploads" />}
      />
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {repos.isPending && <Loading label="Loading repositories" />}
      {repos.isError && <ErrorBox message={`Repositories could not be loaded (${repos.error.message}).`} />}
      {repos.data && (
        <AwsCard title={`Repositories (${repoRows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (name.trim() !== '') create.mutate()
            }}
          >
            <label htmlFor="ecr-name" className="text-sm font-medium">Repository name</label>
            <input id="ecr-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="demo/app" className="rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label className="text-sm">
              Keep last N tagged
              <input type="number" min={0} value={keepN} onChange={(e) => setKeepN(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
            <label className="text-sm">
              Expire untagged after (days, 0 = never)
              <input type="number" min={0} value={expireDays} onChange={(e) => setExpireDays(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
            <AwsButton type="submit" disabled={create.isPending}>Create repository</AwsButton>
          </form>
          <AwsTable<Rec>
            caption="ECR repositories"
            emptyMessage="No repositories yet. Create one above."
            rows={repoRows}
            columns={[
              { header: 'Repository name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              { header: 'URI', render: (r) => <span className="font-mono text-xs">{str(r, 'registry_uri') || '—'}</span> },
              { header: 'Images', render: (r) => String(num(r, 'image_count')) },
              {
                header: 'Created',
                render: (r) => <span className="text-aws-muted">{str(r, 'created_at').slice(0, 19).replace('T', ' ') || '—'}</span>,
              },
              {
                header: 'Actions',
                render: (r) => (
                  <span className="flex gap-2">
                    <AwsButton variant="secondary" onClick={() => { setSelected(String(r.id)); setImageId('') }}>
                      {selected === String(r.id) ? 'Selected' : 'View images'}
                    </AwsButton>
                    <AwsButton variant="secondary" disabled={num(r, 'image_count') > 0} title={num(r, 'image_count') > 0 ? 'Delete the images first — a non-empty repository cannot be deleted.' : undefined} onClick={() => del.mutate(String(r.id))}>
                      Delete
                    </AwsButton>
                  </span>
                ),
              },
            ]}
          />
        </AwsCard>
      )}
      {selected !== '' && selRepo && (
        <AwsCard title={`Images in ${str(selRepo, 'name')} (${imageRows.length})`}>
          <form
            className="mb-3 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (tag.trim() !== '') push.mutate()
            }}
          >
            <label htmlFor="ecr-tag" className="text-sm font-medium">Tag</label>
            <input id="ecr-tag" value={tag} onChange={(e) => setTag(e.target.value)} placeholder="latest" className="w-32 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <label htmlFor="ecr-layer" className="text-sm font-medium">Layer content (stored as a blob)</label>
            <input id="ecr-layer" value={layerText} onChange={(e) => setLayerText(e.target.value)} className="w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
            <AwsButton type="submit" disabled={push.isPending}>Push image</AwsButton>
            <AwsButton variant="secondary" disabled={lifecycle.isPending} onClick={() => lifecycle.mutate()}>
              Evaluate lifecycle rules
            </AwsButton>
          </form>
          {images.isPending && <Loading label="Loading images" />}
          {images.data && (
            <AwsTable<Rec>
              caption="Repository images"
              emptyMessage="No images yet. Push one above."
              rows={imageRows}
              columns={[
                { header: 'Tag', render: (r) => <span className="font-mono">{str(r, 'tag')}</span> },
                { header: 'Digest', render: (r) => <span className="font-mono text-xs">{str(r, 'digest').slice(0, 19) || '—'}</span> },
                { header: 'Size (bytes)', render: (r) => String(num(r, 'size_bytes')) },
                { header: 'Scan', render: (r) => str(r, 'scan_status') || 'NOT_SCANNED' },
                {
                  header: 'Actions',
                  render: (r) => (
                    <span className="flex gap-2">
                      <AwsButton variant="secondary" onClick={() => setImageId(String(r.id))}>Detail</AwsButton>
                      <AwsButton variant="secondary" disabled={scan.isPending} onClick={() => scan.mutate(String(r.id))}>Scan</AwsButton>
                      <AwsButton variant="secondary" onClick={() => delImage.mutate(String(r.id))}>Delete</AwsButton>
                    </span>
                  ),
                },
              ]}
            />
          )}
          {selImage && (
            <div className="mt-4 border-t border-aws-border pt-3">
              <h3 className="mb-2 text-sm font-semibold">Image detail — {str(selImage, 'tag')} <span className="font-mono text-xs font-normal text-aws-muted">{str(selImage, 'digest')}</span></h3>
              <p className="text-xs text-aws-muted">Size {num(selImage, 'size_bytes')} bytes · pushed {str(selImage, 'pushed_at').slice(0, 19).replace('T', ' ') || '—'} · scan {str(selImage, 'scan_status') || 'NOT_SCANNED'}</p>
              {typeof selImage.scan_note === 'string' && selImage.scan_note !== '' && <p className="mt-1 text-xs">{selImage.scan_note as string}</p>}
              <p className="mt-1 text-xs text-aws-muted">Layers: digests are verified against the stored blobs on every scan. Layer contents are not shown here (they are binary blobs on disk).</p>
              <p className="mt-1 text-xs text-aws-muted">Registry API: GET /v2/{str(selRepo, 'name')}/tags/list · GET /v2/{str(selRepo, 'name')}/manifests/{str(selImage, 'tag')}</p>
            </div>
          )}
        </AwsCard>
      )}
    </div>
  )
}
