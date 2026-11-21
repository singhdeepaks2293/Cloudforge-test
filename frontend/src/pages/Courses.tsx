import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { BookOpen } from 'lucide-react'
import { fetchCourses } from '../services/api.ts'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'
import { AwsTable } from '../components/AwsTable.tsx'
import { ErrorBox, Loading } from '../components/Layout.tsx'

export function Courses() {
  const courses = useQuery({ queryKey: ['courses'], queryFn: fetchCourses, retry: 1 })
  const [filter, setFilter] = useState('')
  const rows = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (courses.data ?? [])
      .filter((c) => !q || [c.id, c.title, c.track, c.description].join(' ').toLowerCase().includes(q))
      .map((c) => ({ id: c.id, title: c.title, track: c.track, description: c.description, lessons: c.lessons }))
  }, [courses.data, filter])

  return (
    <div className="space-y-4">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: 'Courses' }]}
        title="Courses"
        description="The CloudForge curriculum, served live from the Go API."
      />
      {courses.isPending && <Loading label="Loading courses" />}
      {courses.isError && <ErrorBox message={`Could not load courses from the API: ${courses.error.message}`} />}
      {courses.data && courses.data.length === 0 && (
        <AwsCard title="No courses yet">
          <p className="text-sm text-aws-muted">
            The catalogue is empty. Courses are seeded by the backend; this page renders whatever the API
            returns.
          </p>
        </AwsCard>
      )}
      {courses.data && courses.data.length > 0 && (
        <AwsCard title="Course catalogue">
          <AwsTable
            columns={[
              {
                header: 'Course',
                render: (r) => (
                  <span>
                    <BookOpen className="mr-1.5 inline h-4 w-4 text-aws-orange-dark" aria-hidden="true" />
                    <span className="font-medium">{r.title}</span>
                    <span className="block text-aws-muted">{r.description || 'No description yet.'}</span>
                  </span>
                ),
              },
              { header: 'Track', render: (r) => r.track || '—' },
              {
                header: 'Lessons',
                render: (r) => `${r.lessons} lesson${r.lessons === 1 ? '' : 's'}`,
              },
              {
                header: 'Actions',
                render: () => <Link to="/learn">View in Learn</Link>,
              },
            ]}
            rows={rows}
            totalCount={courses.data.length}
            filter={filter}
            onFilterChange={setFilter}
            filterPlaceholder="Filter courses"
            emptyMessage="No courses match the current filter."
            caption="CloudForge course catalogue"
          />
        </AwsCard>
      )}
    </div>
  )
}
