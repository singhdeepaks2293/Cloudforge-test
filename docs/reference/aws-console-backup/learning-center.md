# Learning Center — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `learning-center` |
| Name | Learning Center |
| Category | Learning Center |
| Registry route | `/learn` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/Learn.tsx + Courses.tsx, route `/learn` (Learning Center product pages) |
| Resource types | track, lesson, assessment |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

The Learning Center is CloudForge's own product surface (not an AWS service): the home for tracks, lessons, assessments, achievements and XP — the structured learning path that turns the console into a course. A learner meets it as the starting point ('what should I learn next?') and as the record of what they have completed, and an instructor meets it as the catalogue of what to assign.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav (CloudForge product, mirrors Skill Builder patterns):** Home, Learning paths, Tracks, Assessments, Achievements, My learning.
**Home:** recommended path banner, Continue learning (in-progress track with progress bar), Browse tracks by category (Compute, Storage, Networking, Databases, Security, Serverless, DevOps, Analytics, ML/AI, Migration, Terraform, Certification), Achievements strip (recently earned), Leaderboard (optional, per-class).
**Tracks list:** cards/table — columns Track title, Category, Lessons (count), Duration, Difficulty, Progress, Enrol button. Filters: category, difficulty, search.
**Track detail:** description, lesson list (title, duration, type: lesson/guided lab/challenge/assessment, completed checkmark), Enrol button, progress bar, prerequisites.
**Lesson player:** lesson content (Markdown), guided-lab panel (launch the linked lab), challenge panel (break-and-fix scenario), assessment (questions with immediate feedback), Mark complete + XP award.
**Assessments list:** columns Assessment, Track, Questions, Best score, Status.
**Achievements list:** columns Achievement, Description, XP, Earned (date) / Locked.
**Actions:** Enrol, Start lesson, Launch guided lab, Submit assessment, Claim achievement (automatic on completion).

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Tracks, lessons, assessments, achievements and XP._
- **What the current console shows (registry mirrors note):** Skill Builder-style home: paths, tracks, lesson player, assessments, achievements
- **How it differs / what is missing (registry differs note):** No instructor dashboard or SCORM/LTI export yet; 16 tracks include SAA-C03 and SAP-C02, and 6 lab definitions are launchable.
- **Console today:** Yes — frontend/src/pages/Learn.tsx + Courses.tsx, route `/learn` (Learning Center product pages). Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — close the guided-lab gap and add paths/instructor views, do not rebuild the curriculum engine.** Current: 16 tracks (including SAA-C03 and SAP-C02 cert tracks), 88 lessons, 94 questions, 19 achievements (after the 2026-11-22 GitHub sync), 10 break-and-fix scenarios; track/lesson/enrolment/progress/assessment/achievement/XP APIs + Learn/Courses pages (backend/internal/curriculum/, httpapi.go curriculum routes). Known gaps from the audit: most tracks' guided labs have no matching launchable lab definition (only 6 lab definitions are launchable), no Learning Center home/paths view, no instructor dashboard. To build later, in order:
1. **Guided-lab wiring:** every track's `guided_lab`/`challenge` block must resolve to a launchable lab definition; add lab definitions for the missing ones (or mark the guided lab as "read-only walkthrough" where a live lab is genuinely impossible). This is the highest-value fix — a guided lab that cannot launch is the worst kind of dead button.
2. **Learning paths:** a path = ordered tracks with prerequisites and a capstone; `curriculum/paths/*.yaml` + API + home page sections. Paths: Cloud Practitioner, Solutions Architect Associate (SAA-C03), Solutions Architect Professional (SAP-C02), DevOps, Data Analytics.
3. **Instructor dashboard:** per-class enrolment, progress table (learner × track × %), assessment scores, at-risk learners (no activity in 7 days). Admin/instructor-only.
4. **Skill assessments:** timed assessments with a question bank (randomised order, pass mark) and a results page; reuse the existing question format.
5. **Search and recommendations:** full-text search over lessons + "next lesson" recommendation from progress.
**Data model (exists, extend):** curriculum tables; add `pX_learning_paths` (id, slug, title, description, track_slugs text[], capstone_lab, created_at) + `pX_learning_enrolments` already exist as progress rows — reuse them.
**Labs / lessons:** the Learning Center IS the lessons layer; its build work is wiring, not new content. New content follows the existing curriculum YAML format (`curriculum/*/ *.yaml`, validated by `curriculum/validate.py`).
**Lite feasibility:** Yes — the whole Learning Center is content + Postgres; it is the primary lite-mode surface.

## 5. Lite feasibility and scope

- **Scope: medium.** Guided-lab wiring for every track + learning paths + instructor dashboard.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `learning-center`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
