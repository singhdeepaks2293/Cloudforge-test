import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AwsCard } from '../components/AwsCard.tsx'
import { ConsoleTabs, ServiceConsole } from '../components/ServiceConsole.tsx'
import { UnsupportedSection } from '../components/UnsupportedSection.tsx'
import { ServiceHomeDashboard } from '../components/ServiceHome.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { AwsPreferencesButton, AwsRefreshButton, AwsToolbar, AwsWizard } from '../components/AwsShell.tsx'
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
 *
 * Create-repository wizard steps follow the recorded AWS backup
 * (docs/reference/aws-console-backup/ecr.md): 1. repository name
 * and visibility, 2. tag immutability, 3. scan on push, 4.
 * encryption, 5. review. Visibility is private only (no public
 * repositories — the registry is local-only); encryption is AES-256
 * only (no KMS). The wizard sends the real scan_on_push and
 * immutable_tags fields to the backend. Repository detail has
 * tabs: Images, Lifecycle rules, Permissions, Tags.
 */
export function EcrConsole({ initialSection }: { initialSection?: 'home' | 'list' }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [keepN, setKeepN] = useState(10)
  const [expireDays, setExpireDays] = useState(0)
  const [selected, setSelected] = useState('')
  const [tag, setTag] = useState('latest')
  const [layerText, setLayerText] = useState('hello from CloudForge ECR')
  const [imageId, setImageId] = useState('')
  const [notice, setNotice] = useState('')
  const [section, setSection] = useState(initialSection === 'home' ? 'home' : 'repositories')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardStep, setWizardStep] = useState(0)
  const [immutable, setImmutable] = useState(false)
  const [scanOnPush, setScanOnPush] = useState(false)
  const [repoFilter, setRepoFilter] = useState('')
  const [detailTab, setDetailTab] = useState<'images' | 'lifecycle' | 'permissions' | 'tags'>('images')

  const repos = useQuery({ queryKey: ['ecr-repos'], queryFn: () => request<Rec>('/api/v1/aws/ecr/repositories'), retry: 1 })
  const repoRows = listOf(repos.data, 'repositories')
  const filteredRepos = repoRows.filter(
    (r) => repoFilter.trim() === '' || str(r, 'name').toLowerCase().includes(repoFilter.toLowerCase()),
  )
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
          scan_on_push: scanOnPush,
          immutable_tags: immutable,
          lifecycle_rules:
            keepN > 0 || expireDays > 0
              ? [{ keep_last_n_tagged: keepN, expire_untagged_days: expireDays }]
              : undefined,
        },
      }),
    onSuccess: () => {
      setNotice('Repository created.')
      setName('')
      setWizardOpen(false)
      setWizardStep(0)
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

  const wizard = wizardOpen ? (
    <AwsWizard
      title="Create repository"
      steps={['Repository name', 'Tag immutability', 'Scan on push', 'Encryption', 'Review']}
      current={wizardStep}
      onStep={setWizardStep}
      onCancel={() => { setWizardOpen(false); setWizardStep(0) }}
      onSubmit={() => {
        if (name.trim() === '') {
          setNotice('Enter a repository name first.')
          setWizardStep(0)
          return
        }
        create.mutate()
      }}
      submitLabel="Create repository"
      busy={create.isPending}
    >
      {wizardStep === 0 && (
        <div className="space-y-3 text-sm">
          <label htmlFor="ecr-wiz-name" className="block font-medium">Repository name</label>
          <input id="ecr-wiz-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="demo/app" className="w-72 rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none" />
          <p className="text-aws-muted">Lowercase letters, numbers, dots, underscores and slashes — e.g. demo/app.</p>
          <fieldset>
            <legend className="font-medium">Visibility</legend>
            <label className="mt-1 block"><input type="radio" checked readOnly className="mr-2" />Private — only platform-authenticated pulls</label>
            <label className="mt-1 block text-aws-muted"><input type="radio" disabled className="mr-2" />Public — not supported: the registry is local-only, there is no public pull endpoint.</label>
          </fieldset>
        </div>
      )}
      {wizardStep === 1 && (
        <div className="space-y-3 text-sm">
          <label className="flex items-center gap-2 font-medium">
            <input type="checkbox" checked={immutable} onChange={(e) => setImmutable(e.target.checked)} />
            Tag immutability — prevent image tags from being overwritten
          </label>
          <p className="text-aws-muted">When on, pushing an image with an existing tag is rejected instead of replacing it.</p>
        </div>
      )}
      {wizardStep === 2 && (
        <div className="space-y-3 text-sm">
          <label className="flex items-center gap-2 font-medium">
            <input type="checkbox" checked={scanOnPush} onChange={(e) => setScanOnPush(e.target.checked)} />
            Scan on push — scan each image when it is pushed
          </label>
          <p className="text-aws-muted">The scan is an integrity scan (every layer re-read and digest-verified) — NOT a vulnerability (CVE) scan.</p>
        </div>
      )}
      {wizardStep === 3 && (
        <div className="space-y-3 text-sm">
          <fieldset>
            <legend className="font-medium">Encryption</legend>
            <label className="mt-1 block"><input type="radio" checked readOnly className="mr-2" />AES-256 — the registry encrypts blobs at rest with AES-256</label>
            <label className="mt-1 block text-aws-muted"><input type="radio" disabled className="mr-2" />KMS — not supported: there is no KMS key API in CloudForge.</label>
          </fieldset>
        </div>
      )}
      {wizardStep === 4 && (
        <div className="space-y-3 text-sm">
          <dl className="grid grid-cols-2 gap-2">
            <dt className="font-medium">Repository name</dt><dd>{name.trim() === '' ? '— (enter a name on step 1)' : name.trim()}</dd>
            <dt className="font-medium">Visibility</dt><dd>Private</dd>
            <dt className="font-medium">Tag immutability</dt><dd>{immutable ? 'On' : 'Off'}</dd>
            <dt className="font-medium">Scan on push</dt><dd>{scanOnPush ? 'On (integrity scan)' : 'Off'}</dd>
            <dt className="font-medium">Encryption</dt><dd>AES-256</dd>
          </dl>
          <fieldset className="border-t border-aws-border pt-3">
            <legend className="font-medium">Lifecycle rules (optional, CloudForge only)</legend>
            <label className="mt-1 block">
              Keep last N tagged
              <input type="number" min={0} value={keepN} onChange={(e) => setKeepN(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
            <label className="mt-1 block">
              Expire untagged after (days, 0 = never)
              <input type="number" min={0} value={expireDays} onChange={(e) => setExpireDays(Number(e.target.value))} className="ml-2 w-16 rounded-sm border border-aws-border-dark px-2 py-1 text-sm" />
            </label>
          </fieldset>
        </div>
      )}
    </AwsWizard>
  ) : null

  return (
    <ServiceConsole
      icon="layers"
      breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Containers', to: '/services' }, { label: 'ECR' }]}
      title="ECR — Repositories"
      description="Local container registry: Postgres catalogue + content-addressed blobs on disk, speaking the Docker Registry HTTP API subset at /v2/. Scan is an integrity scan, not a CVE scan."
      fidelity="PARTIAL"
      fidelityFootnote="local registry · no Docker Hub · no chunked uploads"
      subNav={[
        { id: 'home', label: 'ECR dashboard' },
        { id: 'repositories', label: 'Repositories' },
        { id: 'images', label: 'Images' },
        { id: 'lifecycle', label: 'Lifecycle rules' },
        { id: 'replication', label: 'Replication' },
        { id: 'settings', label: 'Settings' },
      ]}
      activeSub={section}
      onSubChange={setSection}
    >
      {section === 'home' && (
          <ServiceHomeDashboard
            serviceName="ECR"
            banner="Local registry: Postgres catalogue + content-addressed blobs on disk, speaking the Docker Registry HTTP API subset at /v2/. Scan is an integrity scan, not a CVE scan."
            actions={[
              { label: 'Create repository', primary: true, onClick: () => { setSection('repositories'); setWizardOpen(true); setWizardStep(0) } },
            { label: 'Open ECS (run images)', to: '/services/ecs' },
            ]}
            cards={[
              { label: 'Repositories', count: repoRows.length, onSelect: () => setSection('repositories') },
            { label: 'Registry', count: '—', onSelect: () => setSection('repositories'), note: 'Local-only, no Docker Hub' },
            { label: 'Scan', count: '—', onSelect: () => setSection('repositories'), note: 'Integrity scan, not a CVE scan' },
            ]}
            onRefresh={() => void queryClient.invalidateQueries()}
            refreshing={repos.isFetching}
            health={[
              repos.isError ? 'Repositories could not be loaded.' : `${repoRows.length} repositor(y|ies) listed.`,
            "Chunked/resumable uploads and OCI indexes are not implemented.",
            ]}
            settings={[
              { label: 'Resource Explorer', to: '/explorer' },
              { label: 'All services', to: '/services' },
            ]}
            info={[
              { label: 'Learning Center — containers track', to: '/learn' },
            { label: 'ECS (run images)', to: '/services/ecs' },
            ]}
          />
      )}
      {section === 'repositories' && (
        <>
      {notice !== '' && <p className="border border-aws-border bg-white px-4 py-2 text-sm" role="status">{notice}</p>}
      {wizard}
      {repos.isPending && <Loading label="Loading repositories" />}
      {repos.isError && <ErrorBox message={`Repositories could not be loaded (${repos.error.message}).`} />}
      {repos.data && (
        <AwsCard title={`Repositories (${repoRows.length})`}>
          <div className="mb-3">
            <AwsButton variant="primary" onClick={() => { setWizardOpen(true); setWizardStep(0) }}>Create repository</AwsButton>
          </div>

          <AwsToolbar
            right={
              <>
                <AwsRefreshButton onClick={refresh} busy={repos.isFetching} />
                <AwsPreferencesButton />
              </>
            }
          />
          <AwsTable<Rec>
            caption="ECR repositories"
            emptyMessage="No repositories yet. Create one above."
            rows={filteredRepos}
            totalCount={repoRows.length}
            filter={repoFilter}
            onFilterChange={setRepoFilter}
            filterPlaceholder="Filter repositories by name"
            columns={[
              { header: 'Repository name', render: (r) => <button type="button" className="text-aws-link underline" onClick={() => { setSelected(String(r.id)); setImageId(''); setDetailTab('images') }}><span className="font-mono">{str(r, 'name')}</span></button> },
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
        <AwsCard title={`Repository detail — ${str(selRepo, 'name')}`}>
          <ConsoleTabs
            tabs={[
              { id: 'images', label: 'Images' },
              { id: 'lifecycle', label: 'Lifecycle rules' },
              { id: 'permissions', label: 'Permissions' },
              { id: 'tags', label: 'Tags' },
            ]}
            active={detailTab}
            onChange={(id) => setDetailTab(id as typeof detailTab)}
          />
          <div className="mt-3">
          {detailTab === 'images' && (
          <>
          <h3 className="mb-2 text-sm font-semibold">Images in {str(selRepo, 'name')} ({imageRows.length})</h3>
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
          </>
          )}
          {detailTab === 'lifecycle' && (
            <>
              <p className="mb-3 text-sm text-aws-muted">
                Lifecycle rules expire old images. Rules are set when the repository is created (Review step of
                the Create repository wizard) and evaluated on demand.
              </p>
              {Array.isArray(selRepo.lifecycle_rules) && (selRepo.lifecycle_rules as Record<string, unknown>[]).length > 0 ? (
                <AwsTable<Rec>
                  caption="Lifecycle rules"
                  emptyMessage="No lifecycle rules."
                  rows={(selRepo.lifecycle_rules as Record<string, unknown>[]).map((r, i) => ({ ...r, id: `rule-${i}` }))}
                  totalCount={(selRepo.lifecycle_rules as Record<string, unknown>[]).length}
                  columns={[
                    { header: 'Keep last N tagged', render: (r) => String(Number(r.keep_last_n_tagged ?? 0)) },
                    { header: 'Expire untagged after (days)', render: (r) => String(Number(r.expire_untagged_days ?? 0)) },
                  ]}
                />
              ) : (
                <p className="text-sm text-aws-muted">No lifecycle rules on this repository.</p>
              )}
              <div className="mt-3">
                <AwsButton variant="secondary" disabled={lifecycle.isPending} onClick={() => lifecycle.mutate()}>
                  Evaluate lifecycle rules
                </AwsButton>
              </div>
            </>
          )}
          {detailTab === 'permissions' && (
            <p className="text-sm text-aws-muted">
              No repository policy API exists in CloudForge — the local registry has no IAM-based repository
              permissions. Access is platform-auth only; the Docker Registry API at /v2/ is local-only.
            </p>
          )}
          {detailTab === 'tags' && (
            <p className="text-sm text-aws-muted">
              No repository tags are stored — the teaching ECR API has no tag field on repositories.
            </p>
          )}
          </div>
        </AwsCard>
      )}
        </>
      )}
      {section === 'images' && (
        <AwsCard title="Images">
          <p className="text-sm text-aws-muted">
            Images live on the repository detail — select a repository on the Repositories section to see its
            images, push a new one, scan it or view its layers.
          </p>
          <div className="mt-3">
            <AwsButton variant="secondary" onClick={() => setSection('repositories')}>Open Repositories</AwsButton>
          </div>
        </AwsCard>
      )}
      {section === 'lifecycle' && (
        <AwsCard title="Lifecycle rules">
          <p className="mb-3 text-sm text-aws-muted">
            Lifecycle rules are set per repository in the Create repository wizard (Review step) and evaluated
            on demand — expired images are deleted. Repositories without rules keep every image.
          </p>
          <AwsTable<Rec>
            caption="Lifecycle rules by repository"
            emptyMessage="No repositories yet."
            rows={repoRows}
            totalCount={repoRows.length}
            columns={[
              { header: 'Repository name', render: (r) => <span className="font-mono">{str(r, 'name')}</span> },
              {
                header: 'Rules',
                render: (r) => {
                  const rules = Array.isArray(r.lifecycle_rules) ? (r.lifecycle_rules as Record<string, unknown>[]) : []
                  return rules.length === 0 ? 'none' : rules.map((x) => `keep ${String(Number(x.keep_last_n_tagged ?? 0))} tagged, expire untagged after ${String(Number(x.expire_untagged_days ?? 0))}d`).join('; ')
                },
              },
              {
                header: 'Actions',
                render: (r) => (
                  <AwsButton
                    variant="secondary"
                    disabled={lifecycle.isPending}
                    onClick={() => {
                      setSelected(String(r.id))
                      lifecycle.mutate()
                    }}
                  >
                    Evaluate lifecycle rules
                  </AwsButton>
                ),
              },
            ]}
          />
        </AwsCard>
      )}
      {section === 'replication' && (
        <UnsupportedSection
          title="Replication"
          reason="Replication is UNSUPPORTED in CloudForge — the registry is local-only; there is no cross-region replication API and no remote registry to replicate to."
          actionLabel="Create replication configuration"
        />
      )}
      {section === 'settings' && (
        <UnsupportedSection
          title="Settings"
          reason="ECR settings are UNSUPPORTED in CloudForge — no settings are stored. Scan on push and tag immutability are set per repository in the Create repository wizard."
          actionLabel="Save settings"
        />
      )}
    </ServiceConsole>
  )
}
