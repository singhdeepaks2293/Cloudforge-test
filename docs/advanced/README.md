# Advanced section

The Advanced section is the top-level console area for engineers who want a job or to work end-to-end
in production. It sits alongside Learn, Labs and Terraform Learning. Everything in it is a real working
lab or scenario — there are no text-only pages.

## Contents

| Area | Count | What it is |
| --- | --- | --- |
| Industry projects | 5 tracks | Multi-tier production, HA & DR, migration, cost optimisation, incident response |
| End-to-end deployment projects | 2 tracks | CI/CD staging→production, canary rollout with rollback & alerting |
| Tough troubleshooting | 6 scenarios | Multi-fault, ambiguous-log production outages (16 break-and-fix scenarios in total) |
| Interview walkthroughs | 8 | Problem, expected approach, grading rubric — no single correct answer |
| Skills checklist | 3 JD archetypes | Cloud engineer, DevOps engineer, solutions architect |
| Portfolio artefacts | 1 per project | Generated Markdown, only for completed projects |
| Achievements | 11 new | 7 per-project, plus Advanced Challenger, Advanced Specialist, Advanced Master, Job Ready |

## Files

- `curriculum/advanced/` — the 7 track YAMLs, `interviews.json`, `skills-checklist.json`.
- `curriculum/break-and-fix/adv-*.yaml` — the 6 new scenarios.
- `curriculum/achievements.yaml` — the 11 new achievements (and the `fixer` threshold, see below).
- `backend/internal/advanced/` — the `advanced` Go package: catalogue, interview/skills
  content, portfolio artefact generation, checklist state, HTTP handlers, tests.
- `frontend/src/pages/Advanced*.tsx` — hub, projects, troubleshooting, job-ready pages, plus
  `AdvancedData.ts`, the shared catalogue.
- `docs/advanced/` — this documentation.

## Counts (validator, 2026-11-22)

- 23 tracks (was 16), 116 lessons (was 88), 136 assessment questions (was 94)
- 16 break-and-fix scenarios (was 10), 30 achievements (was 19)
- `python3 curriculum/validate.py` passes with the same 17 pre-existing warnings, all in
  `aws-saa.yaml` / `aws-sap.yaml` (missing mermaid diagrams). The Advanced tracks add zero warnings.

## Lite launchability

5 of the 7 projects launch in lite mode (30-minute labs). The other two need full mode:

- Multi-Tier Production Deployment — `adv-multi-tier`, 120 minutes, full mode only.
- High Availability & Disaster Recovery — `adv-ha-dr`, 60 minutes, full mode only.

The frontend launch button disables itself in lite mode for those two, with the reason in its tooltip.
See `PROJECTS.md` for the per-project mapping.

## Honest limits

- **No fake certificates.** Portfolio artefacts record work CloudForge itself recorded; every artefact
  carries a footer saying it is not a certificate and makes no claim of AWS certification. The skills
  checklist archetypes are composite sketches of common postings, not copies of any single listing.
- **Pipeline / canary / alarm rules are not engine-gradable yet.** The validator vocabulary includes
  `pipeline_exists`, `canary_healthy`, `canary_rolled_back`, `metric_alarm_exists` and friends, but the
  validation engine (`backend/internal/validation`) does not implement them — an unimplemented rule
  grades `unknown` and can never pass. The Advanced tracks therefore grade only rules the engine
  implements, and grade pipeline/canary/rollback outcomes through observable state (healthy targets,
  recorded error rate). See `GRADING.md`.
- **Skills checklist state is in-memory per process** until a store migration lands. The API shape is
  stable so persistence can be added behind it. The frontend also keeps a local copy.
- **No in-console break-fix start button yet.** Scenarios are started via the existing API
  (`POST /api/v1/labs/{id}/break-fix/start`); the Troubleshooting page shows the exact call.
- **Routes are not wired yet.** Frontend routes/nav and the backend route registration are written out
  in `ROUTES.md` for the coordinator to apply. Until then the pages typecheck and lint clean but are
  unreachable, and the job-ready page falls back to its bundled catalogue.
- **Unverified here:** no Docker, KVM or Go toolchain in this sandbox, so the Go package compiles
  untested here (`go test` could not run) and no lab was launched.

## The `fixer` achievement

`fixer` ("Solve every break-and-fix scenario") was thresholded at 10, which was every scenario when
there were 10. There are now 16, so its threshold was raised to 16. The pre-existing
`incident-commander` achievement still says "every industry break-and-fix incident" at a threshold of
10; it was left untouched because its intended scope is ambiguous — the coordinator should decide
whether it means the original 10 or all 16.
