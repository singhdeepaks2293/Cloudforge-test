import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { fetchCourses } from '../services/api.ts'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'

/** Learn — the curriculum grouped by track, from the same live course list. */
export function Learn() {
  const courses = useQuery({ queryKey: ['courses'], queryFn: fetchCourses, retry: 1 })

  const byTrack = new Map<string, { id: string; title: string; lessons: number; description: string }[]>()
  for (const c of courses.data ?? []) {
    const list = byTrack.get(c.track) ?? []
    list.push(c)
    byTrack.set(c.track, list)
  }

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Learn' }]}
        title="Learn"
        description="Cloud fundamentals through VPC and networking, IAM, EC2, storage, databases, serverless, containers and Terraform — each track pairs theory with a guided lab."
      />
      {courses.isPending && <Loading label="Loading tracks" />}
      {courses.isError && <ErrorBox message={`Could not load the curriculum from the API: ${courses.error.message}`} />}
      {courses.data && courses.data.length === 0 && (
        <AwsCard title="Curriculum not published yet">
          <p className="text-sm text-aws-muted">
            No courses have been published by the backend yet. Tracks are defined in the product spec
            (curriculum/ in the repo) and land with Milestone 8.
          </p>
        </AwsCard>
      )}
      {[...byTrack.entries()].map(([track, list]) => (
        <AwsCard key={track} title={track || 'Uncategorised'}>
          <ul className="divide-y divide-aws-border">
            {list.map((c) => (
              <li key={c.id} className="flex items-baseline justify-between gap-4 py-2 text-sm">
                <span>
                  <span className="font-medium">{c.title}</span>
                  {c.description && <span className="block text-aws-muted">{c.description}</span>}
                </span>
                <span className="shrink-0 text-aws-muted">
                  {c.lessons} lesson{c.lessons === 1 ? '' : 's'}
                </span>
              </li>
            ))}
          </ul>
          <p className="mt-3 text-sm">
            <Link to="/labs">Open your labs</Link>
            {' · '}
            <Link to="/courses">All courses</Link>
          </p>
        </AwsCard>
      ))}
    </div>
  )
}
