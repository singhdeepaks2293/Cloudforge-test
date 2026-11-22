# CloudForge frontend (Milestones 1 + 6 frontend, M9 Part 2 console redesign)

React 19 + TypeScript + Vite 8 + Tailwind CSS v4, TanStack Query for data
fetching, react-router-dom for routing.

## Stack (verified installed versions)

| Dependency | Version |
| --- | --- |
| react / react-dom | 19.3.1 |
| vite | 8.3.6 |
| typescript | ~7.0.2 |
| tailwindcss / @tailwindcss/vite | 4.3.5 (v4, `@import "tailwindcss"` + Vite plugin — no v3 config file) |
| @tanstack/react-query | 5.108.2 |
| react-router-dom | 7.18.6 |
| vitest | 5.1.1 (dev) |
| oxlint | 1.90.0 (dev, `npm run lint`) |
| @xyflow/react | 13.0.2 (M6 topology editor) |
| @xterm/xterm | 6.0.0 (M6 terminal) |
| @xterm/addon-fit | 0.11.0 (M6 terminal resize) |
| lucide-react | ^1.77.0 (M9 Part 2 service icons — no emoji as UI icons) |

> Version note (M6): the unscoped `xterm` 5.3.0 / `xterm-addon-fit`
> packages are deprecated upstream in favour of `@xterm/xterm` /
> `@xterm/addon-fit`, so M6 installs the scoped packages. All three
> install cleanly alongside React 19; no other new dependencies were
> added.

## Run

```bash
npm install
npm run dev        # http://localhost:5173
npm run build      # tsc + vite build -> dist/
npm run test       # vitest (API client tests)
npm run lint       # oxlint
```

## Configuration

Copy `.env.example` to `.env` and set `VITE_API_URL` (default
`http://localhost:8081`) — the Go API base URL. It is baked in at build
time for the Docker image (see `Dockerfile`, `VITE_API_URL` build arg).

## Pages

- `/` Dashboard — live `GET /api/v1/health` + course list from the API.
  Shows loading/error states honestly; **no hardcoded statistics**.
- `/learn`, `/courses` — curriculum from `GET /api/v1/courses`.
- `/labs` — `GET /api/v1/labs`, requires a login token.
- `/console` — M1 topology placeholder with AWS-style tabs; renders InspectTopology data when
  the backend exposes `GET /api/v1/network/topology`, otherwise says so.
- `/labs` — EC2-style **Instances** table (Instance ID, Name, State, Type, Private/Public IP, Launch time).
- `/` — Console Home: welcome hero, recently visited services, Build-a-solution cards, live health / lab / course widgets (real data only).
- `/login`, `/register` — `POST /api/v1/auth/*`; token in localStorage.

## Milestone 6 — Lab workstation (frontend only)

New pages/components, all in `frontend/` (no backend changes):

- `/labs/:id/topology` — `src/pages/Topology.tsx` + `src/components/TopologyCanvas.tsx`.
  React Flow canvas with node types VPC / subnet / instance / gateway / load
  balancer and edges for attachments (`parent_id`). Node selection opens an
  inspector (id, state, IPs). Adding a node creates a **local draft only**
  (labelled DRAFT); deleting is possible for draft nodes only.
- `/labs/:id/workstation` — `src/pages/LabWorkstation.tsx`. Multi-pane
  layout: instructions + hints (from the real course detail when the lab's
  `definition_id` matches a course), resource explorer, validation panel,
  topology pane and terminal pane. Panel widths are adjustable with
  draggable dividers (plain pointer events, no extra dependency); the
  terminal height has a slider.
- `src/components/Terminal.tsx` — xterm.js terminal. Live mode first calls
  `POST /api/v1/labs/:id/terminal/token` for a short-lived token (M9 Part 2 fix — the long-lived Bearer token is never sent to the WebSocket; the backend rejects it) and opens the WebSocket with that token,
  reconnects with exponential backoff (1s/2s/4s/8s, max 5 attempts), fits
  on resize, and reports session expiry. Echo mode is a clearly labelled
  **UI test only** (local echo, never a lab shell).
- `src/services/topology.ts` — pure builder: lab resources → React Flow
  nodes/edges (kind columns, one edge per real `parent_id`, none invented).
  Unit-tested in `src/services/topology.test.ts` from resource fixtures.

### What is live vs pending (verified against `backend/internal/httpapi/httpapi.go`, 2026-11-21)

**Live against the real API** — `GET /api/v1/labs/{id}` (lab state,
`definition_id`, `error`), `GET /api/v1/courses/{id}` (instructions/hints
from lesson titles), `GET /api/v1/labs`, `GET /api/v1/courses`. The
topology/resource/validation UI parses a `resources` array on the lab
payload when present, so it lights up automatically if the backend adds
one — today the backend returns none, so the canvas/explorer render an
honest empty state instead of invented infrastructure.

**Pending on backend endpoints (Milestone 6 backend step)** — the UI
renders an explicit "backend endpoint not yet available" state for each
and never fakes data or a success:

| Endpoint | Used by |
| --- | --- |
| `GET /api/v1/labs/{id}/topology` | Topology page live feed (falls back to the lab's resource list) |
| `GET /api/v1/labs/{id}/resources` | Resource explorer list |
| `POST /api/v1/labs/{id}/resources` | Add node (currently a local draft with a "will call backend when endpoint lands" note) |
| `DELETE /api/v1/labs/{id}/resources/{resourceId}` | Delete real node (disabled until it exists) |
| `POST /api/v1/labs/{id}/validate` | Validation panel (no checks run or shown) |
| `POST /api/v1/labs/{id}/terminal/token` | Short-lived terminal token |
| `WS /api/v1/labs/{id}/terminal` | Live terminal shell |

Typed calls for all of the above already exist in `src/services/api.ts`
(`fetchLabTopology`, `fetchLabResources`, `createLabResource`,
`deleteLabResource`, `validateLab`, `requestTerminalToken`,
`buildTerminalWsUrl`) so the UI can switch them on when the backend
routes them; `isPendingEndpointError()` (404/501) is how the UI tells
"not routed yet" apart from a real failure. `GET /api/v1/network/topology`
(used by `/console`) is likewise not routed by the backend yet.

### Verification (M6)

```bash
npm run build   # tsc + vite build — passes (chunk-size warning only:
                # React Flow + xterm make the bundle ~914 kB / 271 kB gzip)
npm run test    # vitest — 30 tests pass (22 API client + 8 topology builder)
npm run lint    # oxlint — 0 warnings, 0 errors
```

## Milestone 9 Part 2 — AWS-console-style redesign

**AWS-console-STYLE in CloudForge's own branding, not AWS itself.**
The header says CloudForge, the tagline is "Build Cloud Skills. Not
Cloud Bills.", no AWS trademarks, logos or brand assets are used, and
nothing claims this is AWS. See `MILESTONE_LOG.md` (M9 Part 2) for the
full before/after and verification.

- Theme tokens in `src/index.css` (Tailwind v4 `@theme`):
  `--color-aws-navy #232f3e`, `--color-aws-orange #ff9900`,
  `--color-aws-link #0073bb`, `--color-aws-bg #eaeded`, etc.
- Shared components in `src/components/`: `AwsHeader` (navy header,
  service search, `cf-local-1 (Local)` region selector, user menu),
  `AwsNav` (collapsible service groups, orange active highlight),
  `AwsTable` (grey header row, filter, footer count), `AwsCard`
  (+ breadcrumb / page header), `AwsButton` (orange primary),
  `AwsBadge` (green/amber/red state pills). Pages reuse these —
  no ad-hoc per-page restyling.
- Service catalog in `src/services/catalog.ts`: the local services
  (EC2, VPC, S3, IAM, Lambda, RDS, …) mapped to real local routes;
  the header search filters this list and navigates to the matching
  local page. Regions are local only (`cf-local-1`, `cf-local-2`).
- Terminal fix (the M6 gap): `Terminal.tsx` now requests a fresh
  short-lived token via `POST /api/v1/labs/:id/terminal/token`
  before every (re)connect — verified live against the running API
  (WS `101` + simulated-shell banner with the short-lived token;
  `401` with the old Bearer-as-WS-token behaviour).
- `frontend/nginx.conf` and `vite.config.ts` (dev proxy for `/api`,
  `/healthz`, `/readyz`, `/metrics` to :8081) and the same-origin
  default in `src/services/api.ts` are unchanged from M9 Part 1.

### Verification (M9 Part 2)

```bash
npm run build   # tsc + vite build — passes
npm run test    # vitest — 51 tests pass (32 before this milestone)
npm run lint    # oxlint — 0 warnings, 0 errors
```

New tests: `src/services/catalog.test.ts` (header-search navigation,
active-nav behaviour), `src/components/aws.test.tsx` (header, nav,
table, badges/buttons), `src/pages/console-home.test.tsx` (Console
Home renders live health + real courses; Instances table renders a
real lab row). Live check: dev server + Go API on :5173/:8081, all
SPA routes → 200, proxy served real health/courses; terminal token →
WS flow verified end to end (see `MILESTONE_LOG.md`).

Known limitations: no live-browser visual check was possible in this
sandbox — the console likeness still needs a human look at
`npm run dev`. The topology/resources/validate UI still shows the M6
"pending backend endpoint" wording even though those backend routes
have since landed; wiring those live feeds is a follow-up.

Known limitations: the production bundle is not code-split (topology and
terminal ship in the main chunk); the terminal live mode cannot be
exercised end-to-end until the WS endpoint lands; the workstation's
instructions/hints only resolve when a lab's `definition_id` matches a
published course id.

## Types / API client

- `src/types/index.ts` — `Health`, `Course`, `Lab`, `Topology`, verified
  against the landed Go backend (`backend/internal/httpapi/httpapi.go`,
  `courses/courses.go`, `labs/labs.go`): health carries
  `network_engine_detail`, course lists carry `lesson_count`, labs use
  `state`/`definition_id`, errors arrive as
  `{error: {code, message, request_id}}`, correlation header is
  `X-Request-ID`.
- `src/services/api.ts` — typed fetch client, `X-Correlation-Id` per
  request, Bearer token, strict response parsing (malformed payloads are
  errors, never silently treated as empty data).
