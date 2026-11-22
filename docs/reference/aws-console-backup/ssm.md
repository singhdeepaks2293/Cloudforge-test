# Systems Manager — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `ssm` |
| Name | Systems Manager |
| Category | Management and Governance |
| Registry route | `/services/ssm` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/ManagementConsoles.tsx `SsmConsole`, route `/services/ssm` |
| Resource types | document, managed-instance |
| Learning tracks linked | monitoring |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Systems Manager is the operations hub for managed instances: Fleet Manager (instance inventory), Run Command (execute commands on instances), Session Manager (shell access without SSH), Patch Manager, State Manager (desired-state configuration), Parameter Store (hierarchical configuration), Documents (runbooks), and Automation (multi-step workflows). A learner meets it as the 'manage a fleet without SSH keys' service — the operational counterpart to the terminal.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Fleet Manager, Run Command, Session Manager, Patch Manager, State Manager, Documents, Inventory, Parameter Store, Automation, (OpsCenter, Explorer).
**Home — Fleet Manager / Managed instances list:** columns Node ID, Status (Online / Connection lost), Platform (Amazon Linux / Ubuntu / Windows), Agent version, Last ping, Region. Filters: platform, status, search.
**Run Command — Command history list:** columns Command ID, Document name, Status (Success / Failed / In progress / Timed out), Targets, Requested by, Requested time. Detail: per-target output (stdout/stderr, exit code), parameters.
**Run Command wizard:** 1. Document (AWS-RunShellScript / AWS-RunPowerShellScript / custom). 2. Command parameters (commands text area). 3. Targets (choose instances by tags/ids, rate control). 4. Output options (S3 bucket, CloudWatch log group). 5. Review and Run.
**Session Manager — Sessions list:** columns Session ID, Target, Status, Started, User. Start session wizard: target instance, shell.
**Parameter Store list:** columns Name (hierarchical `/app/prod/db-host`), Type (String / SecureString / StringList), Value (masked for SecureString), Version, Last modified. Create parameter wizard: name, description, type, value (SecureString → KMS key), tags.
**Documents list:** columns Name, Owner, Platform, Version, Created. Detail: document content (JSON/YAML), parameters, execution history.
**Inventory list:** applications, files, network config, Windows updates per instance.
**Patch Manager:** patch baselines, patch groups, maintenance windows, patch compliance (instances missing patches).
**Automation — Executions list:** columns Execution ID, Document, Status, Started. Runbook detail: steps with status.
**Actions:** Run command, Start session, Create parameter/document, Create maintenance window, Run automation, Send inventory.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Managed instances and run command via the host agent._
- **What the current console shows (registry mirrors note):** Managed instances list; Run-command flow with real output; command history
- **How it differs / what is missing (registry differs note):** Inventory is the real host-agent inventory; Run Command executes allowlisted commands in a lab via the host agent and records the real output and exit code. No host agent is an honest 503 — commands are never faked.
- **Console today:** Yes — frontend/src/pages/ManagementConsoles.tsx `SsmConsole`, route `/services/ssm`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add Parameter Store, Documents, Patch Manager and Automation, do not rebuild Run Command.** Current: inventory is the real host-agent inventory; Run Command executes allowlisted commands in a lab via the host agent and records the real output and exit code; without a host agent both return an honest 503 (backend/internal/httpapi/phase6.go `p6_ssm_commands`, hostagent/client.go). Console: ManagementConsoles.tsx `SsmConsole`. To build later, in order:
1. **Parameter Store:** `pX_ssm_parameters` (id, name, type String/SecureString/StringList, value, secure_value_encrypted, version, tags jsonb, created_by, updated_at) + CRUD endpoints + `GetParametersByPath` (hierarchical prefix listing). SecureString values are envelope-encrypted with a KMS key (same pattern as Secrets Manager) and masked in list responses; versions increment on put. This is the highest-value addition — every other service (Lambda env vars, CodeBuild, ECS tasks) can read parameters from here.
2. **Documents:** `pX_ssm_documents` (id, name, owner, platform, content jsonb, version, created_at) + CRUD; documents are JSON runbooks with steps; Run Command gains a document picker (today it is only the built-in shell document).
3. **Automation:** a document execution engine — run a document's steps in order (each step = a Run Command invocation, a Lambda invocation, or an approve step), recording per-step status; reuse the CodePipeline stage-execution pattern.
4. **Session Manager:** a WebSocket shell to a lab instance via the host agent — reuse the existing terminal WebSocket (backend/internal/httpapi/terminal.go) with the SSM session record wrapped around it; without a host agent it is an honest 503, same as Run Command.
5. **Patch Manager:** patch baselines (a list of approved package rules) + compliance computed from the host-agent inventory (installed packages vs baseline); maintenance windows are schedules for Run Command executions (cron via the worker poller).
**Data model (extend):** `p6_ssm_commands` exists; add the tables above.
**Labs / lessons:** add one lesson to the `monitoring` track: store a database password in Parameter Store (SecureString), read it from a Lambda function, rotate it, and watch the function pick up the new value; run a patch-compliance check. Reuse the Lambda/IAM lessons.
**Lite feasibility:** Yes — Parameter Store/Documents/Automation are pure Postgres; Run Command/Session Manager need the host agent (simulated mode works for the teaching path; real execution needs a real agent).

## 5. Lite feasibility and scope

- **Scope: medium.** Parameter Store + Documents + Automation on the existing Run Command; Session Manager reuses terminal WS.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `ssm`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
