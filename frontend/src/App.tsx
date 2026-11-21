import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout.tsx'
import { Console } from './pages/Console.tsx'
import { Courses } from './pages/Courses.tsx'
import { Dashboard } from './pages/Dashboard.tsx'
import { Labs } from './pages/Labs.tsx'
import { LabWorkstation } from './pages/LabWorkstation.tsx'
import { Learn } from './pages/Learn.tsx'
import { Login, Register } from './pages/Auth.tsx'
import { Topology } from './pages/Topology.tsx'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="learn" element={<Learn />} />
            <Route path="courses" element={<Courses />} />
            <Route path="labs" element={<Labs />} />
            <Route path="labs/:id/topology" element={<Topology />} />
            <Route path="labs/:id/workstation" element={<LabWorkstation />} />
            <Route path="console" element={<Console />} />
            <Route path="login" element={<Login />} />
            <Route path="register" element={<Register />} />
            <Route
              path="*"
              element={
                <div>
                  <h1 className="text-xl font-semibold">Page not found</h1>
                  <p className="mt-1 text-sm text-aws-muted">That route does not exist yet.</p>
                </div>
              }
            />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
