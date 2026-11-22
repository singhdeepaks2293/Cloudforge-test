# Cloud9-style Terminal — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `terminal` |
| Name | Cloud9-style Terminal |
| Category | Developer Tools |
| Registry route | `/labs` |
| Availability today | partial |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes (partial) — Lab workstation terminal in frontend/src/pages/LabWorkstation.tsx — opened from a lab, not a standalone service console |
| Resource types | terminal-session |
| Learning tracks linked | fundamentals |
| Build-later scope | **small** |
| Lite feasibility | see §5 |

## 1. Purpose

The terminal is CloudForge's own Cloud9-style surface (not an AWS service): an in-browser shell attached to a lab, for running the commands a lesson asks for — checking a VPC, curling an endpoint, inspecting a file — without leaving the console. A learner meets it inside every lab workstation, and an instructor meets it as the place where 'try it yourself' actually happens.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** none — the terminal lives inside the lab workstation page (`/labs/:id/workstation`), opened per lab.
**Home — Workstation page:** left: lab details + resources; centre: terminal pane (xterm.js: command input, scrollback, prompt showing the lab name); top bar: connection status, session timer (5-minute token), Clear, Reconnect buttons. Below/above: an allowlist notice ("Only allowlisted commands run here; the simulated shell is labelled simulated").
**Actions:** Run command (Enter), Clear screen, Reconnect (new short-lived token), Open full workstation (larger terminal).

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: partial). Registry description: _Lab terminal — allowlisted commands over a short-lived token._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Allowlisted commands only; simulated shell is labelled simulated. Opened from a lab's workstation page.
- **Console today:** Yes (partial) — Lab workstation terminal in frontend/src/pages/LabWorkstation.tsx — opened from a lab, not a standalone service console. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — harden and extend the command set, do not rebuild the shell.** Current: WebSocket terminal, 5-minute single-use token (SHA-256 hash persisted), allowlisted commands only, simulated shell labelled as simulated (backend/internal/httpapi/terminal.go, backend/internal/terminal/terminal.go:211 allowlist, backend/internal/hostagent/ops.go:237). Console: LabWorkstation.tsx. Fidelity PARTIAL. To build later, in order:
1. **More allowlisted commands, carefully:** add read-only inspection commands that map to real CloudForge state (`cf labs list`, `cf services list`, `cf vpc describe`, `cf logs tail`) implemented as thin wrappers over the existing APIs — each new command needs an allowlist entry + a test that it cannot escape the allowlist (no pipes to arbitrary binaries, no `;`/`&&` chaining into unlisted commands — the existing allowlist design already forbids this; keep it).
2. **Real execution path:** when a host agent is present, commands run on the lab VM/container through the host agent (the real exec path already exists in hostagent/ops.go); when absent, the simulated shell answers from real platform state (never invented output) and says so. The mode badge (Simulated / Real) must be visible in the terminal header at all times.
3. **Session recording:** record the transcript (commands + output, capped) per lab for the instructor to review — Postgres `pX_terminal_sessions` (id, lab_id, user_id, transcript_path, started_at, ended_at). Transcripts are a teaching artifact, not surveillance: tell the learner on the page that the session is recorded for the class.
4. **File editor alongside:** a minimal editor for the lab workdir files (read/edit/save, same allowlist philosophy) — the Cloud9 parallel. Small scope, high teaching value for the Terraform lessons.
**Data model (add):** `pX_terminal_sessions` only; the allowlist stays in code (terminal.go).
**Labs / lessons:** the terminal is used by existing lessons; add one lesson in the `fundamentals` track: "your first terminal session" (list resources, inspect a lab, read a log). No new content format needed.
**Lite feasibility:** Yes — the simulated shell is the lite offering; real execution needs the host agent (full mode).

## 5. Lite feasibility and scope

- **Scope: small.** More allowlisted inspection commands + session transcripts + workdir file editor.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `terminal`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
