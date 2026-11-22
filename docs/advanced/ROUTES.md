# Advanced routes, navigation and backend wiring

The Advanced pages and the `advanced` Go package are written but **not wired**: the coordinator
applies the snippets below. Nothing in `frontend/src/App.tsx`, `AwsNav.tsx`, `AwsHeader.tsx` or
`backend/internal/httpapi/` was edited by this workstream.

## Frontend routes — `frontend/src/App.tsx`

Add the imports next to the existing page imports (after the `TerraformLearning` import):

```tsx
import { Advanced } from './pages/Advanced.tsx'
import { AdvancedJobReady } from './pages/AdvancedJobReady.tsx'
import { AdvancedProjects } from './pages/AdvancedProjects.tsx'
import { AdvancedTroubleshooting } from './pages/AdvancedTroubleshooting.tsx'
```

Add the routes next to the existing `learn` / `terraform-learning` routes (near
`<Route path="learn" element={<Learn />} />`):

```tsx
<Route path="advanced" element={<Advanced />} />
<Route path="advanced/projects" element={<AdvancedProjects />} />
<Route path="advanced/troubleshooting" element={<AdvancedTroubleshooting />} />
<Route path="advanced/job-ready" element={<AdvancedJobReady />} />
```

## Frontend navigation — `frontend/src/components/AwsNav.tsx`

In the expanded nav, after the Terraform Learning link (the block starting
`<NavLink to="/services/terraform-learning"`), add:

```tsx
<NavLink to="/advanced" onClick={onNavigate} className={linkCls}>
  <ServiceIcon icon="book" />
  Advanced
</NavLink>
```

In the collapsed nav's `flat` array (after `{ to: '/learn', label: 'Learning Center', icon: 'book' },`),
add:

```tsx
{ to: '/advanced', label: 'Advanced', icon: 'book' },
```

In the header help menu (`frontend/src/components/AwsHeader.tsx`, after the Learning Center link),
add:

```tsx
<Link to="/advanced" onClick={() => setOpenMenu(null)} className="block px-4 py-2 text-sm hover:bg-[#f2f3f3] hover:no-underline">
  Advanced — job-ready, industry &amp; end-to-end
</Link>
```

## Backend routes — `backend/internal/httpapi/httpapi.go`

The handlers live in `backend/internal/advanced/` and take the authenticated user id as an
argument; the router owns auth, exactly as the existing curriculum handlers do.

1. Add the import:

```go
"github.com/cloudforge/cloudforge/backend/internal/advanced"
```

2. Add a field to the `Server` struct (near `Curriculum` / `CurriculumDir`):

```go
Advanced *advanced.Service
```

3. In `NewServerWithDeps`, after the block that defaults `d.Curriculum` when nil (the
`NewMemoryCurriculumStore` fallback), construct the service from the same inputs:

```go
if d.Advanced == nil {
    d.Advanced = advanced.NewService(d.CurriculumDir, d.Curriculum)
}
```

and add `Advanced: d.Advanced,` to the `Server` struct literal (near
`Curriculum: d.Curriculum, CurriculumDir: d.CurriculumDir,`).

4. In the `r.Route("/api/v1", ...)` block, after the Milestone 8 curriculum routes
(after `r.Post("/labs/{id}/break-fix/solve", s.handleBreakFixSolve)`), add:

```go
// Advanced section (Workstream 3): catalogue, interviews, skills
// checklist, checklist state and portfolio artefacts.
r.Route("/advanced", func(r chi.Router) {
    r.Get("/projects", func(w http.ResponseWriter, req *http.Request) {
        if _, ok := s.requireAuth(w, req); !ok {
            return
        }
        s.Advanced.HandleProjects(w, req)
    })
    r.Get("/interviews", func(w http.ResponseWriter, req *http.Request) {
        if _, ok := s.requireAuth(w, req); !ok {
            return
        }
        s.Advanced.HandleInterviews(w, req)
    })
    r.Get("/skills-checklist", func(w http.ResponseWriter, req *http.Request) {
        if _, ok := s.requireAuth(w, req); !ok {
            return
        }
        s.Advanced.HandleSkillsChecklist(w, req)
    })
    r.Get("/checklist-state", func(w http.ResponseWriter, req *http.Request) {
        u, ok := s.requireAuth(w, req)
        if !ok {
            return
        }
        s.Advanced.HandleChecklistState(w, req, u.ID)
    })
    r.Put("/checklist-state", func(w http.ResponseWriter, req *http.Request) {
        u, ok := s.requireAuth(w, req)
        if !ok {
            return
        }
        s.Advanced.HandleChecklistState(w, req, u.ID)
    })
    r.Post("/portfolio/{project}", func(w http.ResponseWriter, req *http.Request) {
        u, ok := s.requireAuth(w, req)
        if !ok {
            return
        }
        s.Advanced.HandlePortfolio(w, req, u.ID)
    })
})
```

Notes on the wiring:

- `HandlePortfolio` parses the project id from `r.URL.Path` with the prefix
  `/api/v1/advanced/portfolio/`; the chi `{project}` segment is only routing, the handler does
  its own parsing and rejects ids containing `/`.
- All five routes sit inside the existing `/api/v1` group, so the general and per-user rate
  limiters already apply.
- `requireAuth` returns `(*auth.User, bool)`; the `u.ID` passed to the handlers is the
  authenticated user's id. If the auth helper's signature differs on the branch this lands on,
  adapt the adapter closures — the `advanced` package itself does not change.
- The frontend `AdvancedJobReady` page already calls these exact paths; before wiring it falls
  back to its bundled catalogue and shows the API error instead of inventing data.

## What is intentionally NOT wired here

- No in-console break-fix start button. Scenarios are started via the existing
  `POST /api/v1/labs/{id}/break-fix/start` endpoint; adding a button is follow-up UI work, not a
  route.
- No new lab definitions were added to `backend/internal/labs/labs.go` (not this workstream's
  ownership). Advanced projects launch the closest existing definitions; see `PROJECTS.md`.
