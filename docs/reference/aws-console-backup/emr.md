# EMR — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `emr` |
| Name | EMR |
| Category | Analytics |
| Registry route | `/services/emr` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | cluster, step |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

EMR is the managed big-data service: launch a cluster of EC2 instances with Hadoop, Spark, Hive, Presto/Trino, HBase or Flink pre-installed, submit steps (Spark jobs, Hive queries), and terminate the cluster when the job is done. A learner meets it as the 'run a Spark job without building a Hadoop cluster' service, and as the batch-analytics counterpart to Athena (interactive SQL) and Glue (ETL).

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Clusters, Security configurations, Block public access, Events, Notebooks, Security (EMR Studio).
**Home — Clusters list:** columns Name / ID, Status (Starting / Bootstrapping / Running / Waiting / Terminating / Terminated / Terminated with errors), Created, Elapsed time, Applications (Spark, Hive, …). Filters: status, search.
**Cluster detail tabs:** Summary (cluster ID, status, applications, master/public DNS, log URI, tags), Steps (table: Step ID, Name, Status, Started, Elapsed; Add step), Configurations (JSON editor of classifications), Events (timeline), Hardware (instance groups: master/core/task, instance type, count, EBS), Monitoring (YARN/container metrics), Bootstrap actions (list + add), Applications (installed applications + versions).
**Wizard — Create cluster:** Step 1 Name and applications (name, release label emr-7.x, applications Spark/Hadoop/Hive/Presto). Step 2 Instances (instance groups vs instance fleets, master/core/task instance types + counts, EBS, EC2 key pair). Step 3 Steps (optional Spark/Hive steps, JAR location, args). Step 4 Bootstrap actions (scripts). Step 5 Security and access (service role, EC2 instance profile, security config, EC2 key pair). Step 6 Networking (VPC, subnets, security groups). Step 7 Review.
**Actions:** Create cluster, Add step, Terminate cluster, Clone cluster, Resize (instance groups), Add bootstrap action, Terminate with errors handling.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Big-data clusters._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started (Phase 5).
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** clusters CRUD `POST/GET/DELETE /api/v1/aws/emr/clusters`, steps `/api/v1/aws/emr/clusters/{id}/steps` (add, list, cancel), instance groups resize, bootstrap actions, security configurations CRUD.
**Runtime (honest scope — no Hadoop cluster locally):** EMR in CloudForge is a Spark-job runner, not a Hadoop cluster. A cluster is a record with applications; a step is a Spark job executed by a local Spark-compatible runner — the realistic local options are (a) a Python/PySpark script run through the sandboxed subprocess runner (PySpark needs a Spark install on the host — gate behind a capability check and refuse honestly when absent), or (b) a pure-Python "Spark-like" job API (map/filter/reduce over a local file or S3 objects) that teaches the Spark programming model without the engine. Option (b) is the lite-feasible default and must be labelled as a teaching subset, not Spark. Steps read input from S3 (MinIO) or local paths and write output the same way. Cluster status follows the step lifecycle; instance groups are recorded (instance type/count) and billed in the cost model, but no instances are provisioned — say so on the cluster page.
**Data model:** Postgres `pX_emr_clusters` (id, name, release_label, applications text[], status, instance_groups jsonb, log_uri, created_at, terminated_at) + `pX_emr_steps` (id, cluster_id, name, type, jar_or_script, args text[], status, log_path, started_at, finished_at) + `pX_emr_bootstrap` (id, cluster_id, script_location, args text[]).
**Labs / lessons:** one lesson in the `databases`/analytics area: run a word-count-style job over a text file in S3, then a Spark-SQL-style aggregation; compare with the Athena lesson on the same data.
**Lite feasibility:** Partial — the teaching-subset runner (option b) works in lite; a real PySpark install is full-mode only and capability-gated. Instance-group provisioning is never real in either mode.

## 5. Lite feasibility and scope

- **Scope: large.** Spark-job teaching runner (subset, labelled) + cluster/step model; real Spark is capability-gated.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `emr`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
