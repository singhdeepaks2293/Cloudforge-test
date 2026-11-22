# CloudFormation — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `cloudformation` |
| Name | CloudFormation |
| Category | Additional |
| Registry route | `/services/cloudformation` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/CloudFormationConsole.tsx, route `/services/cloudformation` |
| Resource types | stack |
| Learning tracks linked | terraform |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

CloudFormation is the infrastructure-as-code service that turns a JSON/YAML template into a stack of resources: declare resources, parameters, mappings and outputs, preview the change set, then create, update or delete the whole stack as one unit with rollback on failure. A learner meets it as the declarative counterpart to Terraform — the same idea (templates, plans, state) with a different engine — and as the provisioning engine behind Service Catalog products.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Stacks, StackSets, Change sets (under a stack), Designer.
**Home — Stacks list:** columns Stack name, Status (CREATE_COMPLETE / UPDATE_COMPLETE / CREATE_FAILED / ROLLBACK_COMPLETE / DELETE_IN_PROGRESS / etc.), Created time, Description. Filters: status, search by name. Deleted stacks are shown greyed for 90 days.
**Stack detail tabs:** Overview (description, stack ID, status reason, capabilities, tags), Outputs (output key/value/description/export name), Resources (logical ID, physical ID, type, status), Events (timestamp, logical ID, status, reason — the deployment timeline), Parameters (key/value), Template (view template JSON/YAML), Change sets (list + create), Stack policy, Drift detection.
**Wizard — Create stack:** 1. Prerequisite — Prepare template (template is ready / use a sample / create template in Designer; template source: S3 URL / upload file / sample). 2. Specify stack details (stack name, parameters with defaults and validation). 3. Configure stack options (tags, permissions/IAM role, stack policy, rollback configuration, notification options, stack creation options). 4. Review and create (capabilities acknowledgement for IAM resources).
**Change set flow:** Create change set → review the diff table (Action Add/Modify/Remove/Replace, Logical ID, Type, Replacement) → Execute change set (or delete it).
**Actions:** Create stack, Update stack, Delete stack, Create change set, Execute change set, Detect drift, View drift results, Rollback (via update), Create StackSet.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Stacks and templates — own template engine with change sets and rollback._
- **What the current console shows (registry mirrors note):** Stacks list; Stack detail (Resources / Events / Template); Create-stack wizard; change-set preview
- **How it differs / what is missing (registry differs note):** A separate template engine from Terraform (JSON/YAML, own stack state, change-set diff, rollback on failure). Provisions SQS, SNS, DynamoDB (subset), Lambda, S3 (MinIO) and RDS only; other resource types are UNSUPPORTED in the change set. Rollback deletes resources created in the failed execution; resources deleted by an update are NOT restored.
- **Console today:** Yes — frontend/src/pages/CloudFormationConsole.tsx, route `/services/cloudformation`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend the resource types, do not rebuild the engine.** Current: own JSON/YAML template engine with change-set diff, rollback on failure, and 6 provisioned types (SQS, SNS, DynamoDB subset, Lambda inline code, S3 via MinIO, RDS) (backend/internal/phase4/phase4.go `ParseCFNTemplate`/`PlanCFNChanges`, httpapi/phase4.go `p4_cfn_*`). To build later, in priority order:
1. **More resource types:** IAM Role/Policy (stored identities — reuse the IAM CRUD), VPC/Subnet/SecurityGroup (lab resources), ElastiCache (namespaced Redis), ElastiCache, EC2 instance (lab instance), KMS key, SecretsManager secret, CloudWatch alarm (threshold rule), SQS/SNS already done. Each new type needs a create/update/delete path in the engine plus a `PlanCFNChanges` entry; unsupported types stay UNSUPPORTED in the change set, never silently skipped.
2. **Nested stacks and `Fn::ImportValue` / cross-stack references:** resolve outputs of other CloudForge stacks.
3. **Drift detection for real:** compare each stack resource's recorded state against the live backend state (SQS queue exists? S3 bucket policy matches?) and report drifted resources; currently drift is not implemented.
4. **StackSets:** one template, many "accounts" — in CloudForge, many labs; a StackSet deploys the same stack into each selected lab. Medium scope, after single-stack types are solid.
5. **Designer:** a visual template editor is out of scope; the Template tab stays a text view with validation.
**Data model (exists, extend):** `p4_cfn_stacks` + `p4_cfn_events` + `p4_cfn_changesets`; add `pX_cfn_drift` (id, stack_id, resource_logical_id, drift_status, checked_at).
**Labs / lessons:** add two lessons to the `terraform` track (CloudFormation vs Terraform comparison): deploy a three-tier stack from a template, update it via change set, watch rollback on a deliberately bad update. Reuse the existing Terraform lessons' structure.
**Lite feasibility:** Yes — the engine is pure Go + Postgres; new resource types are only allowed if their underlying service works in lite (SQS/SNS/IAM/KMS do; anything needing Docker/KVM is refused honestly in lite).

## 5. Lite feasibility and scope

- **Scope: medium.** More provisioned resource types + real drift detection; StackSets after that.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `cloudformation`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
