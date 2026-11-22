# CodeBuild — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `codebuild` |
| Name | CodeBuild |
| Category | Developer Tools |
| Registry route | `/services/codebuild` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/CodeBuildConsole.tsx, route `/services/codebuild` |
| Resource types | build-project, build |
| Learning tracks linked | devops |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

CodeBuild is the fully managed build service: give it source code and a buildspec, and it provisions a build environment, runs the phases (install, pre-build, build, post-build), captures logs and artifacts, and reports success or failure. A learner meets it as the 'compile and test my code in the cloud' step of a pipeline, and as the build stage inside CodePipeline.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Build projects, Build history, Report groups, Settings.
**Home — Build projects list:** columns Name, Status (last build status), Source (repository + branch), Created. Filters: search, status.
**Project detail tabs:** Build history (table: Build ID, Status, Duration, Started, Source version), Build details (per build: Phases table with phase name/status/duration, Logs tab with live tail, Artifacts, Environment).
**Build history list (global):** columns Build ID, Project, Status (SUCCEEDED / FAILED / IN_PROGRESS / STOPPED), Duration, Started.
**Wizard — Create build project:** 1. Project configuration (project name, description). 2. Source (source provider GitHub/CodeCommit/S3/CodePipeline, repository, branch). 3. Environment (managed image, operating system, runtime, image, compute type, service role, privileged mode). 4. Buildspec (use a buildspec file / insert build commands; editor with phases install/pre_build/build/post_build). 5. Artifacts (type S3/none, location, name, packaging). 6. Logs (CloudWatch logs group, S3 logs). 7. Review.
**Actions:** Start build (with optional source version override), Stop build, Retry build, Delete build project, Create report group.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Build projects — real local builds from a local git repo + buildspec._
- **What the current console shows (registry mirrors note):** Build projects; Build history with real logs; build detail (phases, artifacts)
- **How it differs / what is missing (registry differs note):** Real builds: git checkout from a local path under CLOUDFORGE_BUILD_SOURCE_ROOT (no remote URLs), buildspec phases run as real shell commands in a subprocess with timeouts, real logs and artifacts on disk. No Docker build environment; no managed build fleet.
- **Console today:** Yes — frontend/src/pages/CodeBuildConsole.tsx, route `/services/codebuild`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend sources and artifacts, do not rebuild the runner.** Current: real local builds from a local git repo + buildspec, phases run as real shell commands in a sandboxed subprocess with timeouts, real logs + artifacts on disk (backend/internal/phase4/phase4.go `ParseBuildspec`/`RunShell`, httpapi/phase4.go `p4_codebuild_*`). Console: CodeBuildConsole.tsx. To build later, in order:
1. **Source providers:** keep local-path-only (the security constraint: no remote URLs). Add S3 source (a zip in a MinIO bucket — real download + extract) and CodePipeline source (already the pipeline path). GitHub/CodeCommit remain disabled with the reason (no remote fetch locally).
2. **Build environments:** add a runtime/image matrix that is honest about what runs where — the sandboxed subprocess has python3/node available; record the requested image and refuse images that cannot run locally with a clear error instead of pretending. Docker build environments stay out of scope (no Docker in lite; full mode could run them in a container later — gate behind a capability check).
3. **Artifacts to S3:** upload build artifacts to a MinIO bucket (real PutObject) in addition to local disk; record the artifact location on the build.
4. **Report groups / test reports:** parse JUnit XML test output from the build log directory into a test report (passed/failed/skipped counts) — small, high teaching value.
5. **Build caching:** cache the source checkout + dependency directory per project (keyed by source version) to speed repeat builds; show cache hits in the build detail.
**Data model (exists, extend):** `p4_codebuild_*`; add `pX_codebuild_reports` (id, build_id, test_suite, passed, failed, skipped, created_at) and artifact_location on builds.
**Labs / lessons:** add one lesson to the `devops` track: build a Python project with tests, read the test report, fix a failing test, rebuild. Reuse the existing CodeBuild lesson.
**Lite feasibility:** Yes — subprocess builds already work in lite; S3 artifacts need MinIO, which is full-mode only, so in lite artifacts stay on local disk and the S3 option is disabled with the reason.

## 5. Lite feasibility and scope

- **Scope: small.** S3 sources/artifacts + JUnit test reports + build caching on the existing runner.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `codebuild`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
