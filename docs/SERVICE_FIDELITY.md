# CloudForge v2 — Service Fidelity

> **Audit date:** 2026-11-22 IST · Companion to `docs/SERVICE_CATALOG.md`
> and `docs/IMPLEMENTATION_STATUS.md` (Phase 0 audit).
> Fidelity is assigned from what the code actually does, never from what a
> label, a doc, or a registry entry claims.

## 1. Fidelity definitions

| Fidelity | Meaning | When to use it |
|---|---|---|
| `REAL` | The operation runs against real local infrastructure and its result is the real result. No AWS is involved; "real" means "real locally". | KVM VM actually started; Docker container actually running; Terraform binary actually executed a plan; Postgres row actually written; a file actually restored by `pg_restore`. |
| `API_EMULATED` | A real local service speaks the AWS API (or a compatible subset of it) and persists real state through that API. The API contract is honoured; the backend is local, not AWS. | MinIO speaking S3 (`CreateBucket` really creates a bucket in MinIO); DynamoDB Local speaking the DynamoDB JSON API. |
| `SIMULATED` | An educational implementation that mimics the behaviour in-process or against local stand-ins, with known gaps. State may be in-memory and lost on restart. | In-memory SQS/SNS/KMS/STS simulators; the Rust engine's simulated backend (validates inputs, holds topology in memory, touches no kernel); the simulated terminal shell. |
| `PARTIAL` | Some operations of the service are implemented (at any fidelity above), others are not; the implemented subset is real within its stated limits. | IAM policy evaluation (a real subset of the policy language, nothing else); S3 (buckets over HTTP, objects adapter-only). |
| `UNSUPPORTED` | Not implemented. No operation exists, or the registry explicitly marks it unsupported. | EKS `CreateCluster`; everything in §6 K–N not yet started. |

**Rules**

1. **Never present simulation as real.** A `SIMULATED` operation is labelled
   `SIMULATED` in the UI (badge), in the API response where applicable, and
   in docs. A learner must never leave a lab believing they used real AWS.
2. **No resource is shown `READY` until the operation actually succeeded.**
   Lab state transitions to `READY` only after provisioning completes
   (`backend/internal/labs/labs.go:245-279` `provision()`; worker path
   `backend/internal/jobs/jobs.go:137-175`). Engine/host-agent failure is a
   `FAILED` lab or a 502/503 — never a fake `READY`. The same rule applies
   per service in Phases 1–6: create returns 202 (accepted) while work is
   async; the resource row shows its real state (`CREATED`/`PROVISIONING`/
   `READY`/`FAILED`/`DESTROYED`), and the detail page shows the underlying
   error when provisioning failed.
3. **Health is never assumed ok.** Capabilities start `unknown`
   (`backend/internal/providers/providers.go`); probes set `ok`/`degraded`/
   `unavailable` with a checked-at timestamp. `/readyz` live-checks
   Postgres/Redis/engine on every call.
4. **A service is not "complete" on UI alone, backend alone, or tests alone.**
   Complete = backend + frontend console + tests + a live end-to-end walk
   (create → list → detail tabs → action → delete) in a running stack
   (see `IMPLEMENTATION_STATUS.md` §8).
5. **A registry entry is not an implementation.** `providers.go` `seeded`
   capabilities with no HTTP route are `registry-only` in the catalog and
   must be wired or downgraded in Phase 1.

## 2. Per-service fidelity table (with evidence)

| Service / operation | Fidelity | Evidence (file / function) |
|---|---|---|
| Labs — launch / provision / destroy | SIMULATED (default) / REAL (when KVM/Docker backends run) | `backend/internal/labs/labs.go:provision()`; `backend/internal/jobs/jobs.go:provisionViaHostAgent()`; `backend/internal/hostagent/` (simulated / `docker.go` / `libvirt.go`). Default `HOST_AGENT_MODE=simulated`; real KVM unverified in this sandbox. |
| Network engine — CreateNetwork/Subnet/Route/Firewall/NAT, InspectTopology, fault injection, telemetry | SIMULATED (default backend) | `rust-network-engine/crates/engine-core/src/backend.rs:27-51` (`BackendKind::Simulated` default); `crates/grpc-server/src/lib.rs:56-68`. Real backend: `RealNetlinkBackend` (`crates/engine-core/src/real.rs`), compiles, unverified here, needs `CAP_NET_ADMIN` + `nft`. Telemetry counters are real in-memory counters; "no traffic is fabricated" (`proto/cloudforge/v1/network.proto`). |
| S3 — CreateBucket / ListBuckets | API_EMULATED | `backend/internal/providers/s3.go` (`S3Adapter`, minio-go v7 against local MinIO); HTTP routes `backend/internal/httpapi/aws.go:78-106`. Limitations: local buckets only, no global namespace (`providers.go` s3 entries). |
| S3 — PutObject / GetObject | API_EMULATED (HTTP route since Phase 2) | `providers/s3.go:160-200`; HTTP routes `backend/internal/httpapi/s3.go` (objects list/upload/download/delete, metadata, versioning, policy). Upload through the backend (base64 JSON, 10 MiB cap); no direct browser→MinIO credentials. MinIO down → honest 503. Object ACLs NOT exposed by minio-go. No replication/lifecycle unless MinIO is configured for them. |
| DynamoDB — CreateTable / PutItem / GetItem / Query / Scan / DeleteItem / DeleteTable | API_EMULATED (via DynamoDB Local) / PARTIAL (Postgres subset fallback) | `backend/internal/providers/dynamodb.go` (DynamoDB JSON API to DynamoDB Local, default `http://localhost:8000`, dummy local-only SigV4 credential — Local rejects unsigned requests and rejects non-alphanumeric access keys); HTTP routes + console since Phase 3 (`backend/internal/httpapi/phase3.go`, `frontend/src/pages/DynamoDbConsole.tsx`). Probe cached 60 s: Local answering → `dynamodb-local` / `API_EMULATED`; otherwise the durable Postgres subset (`p3_dynamodb_tables` / `p3_dynamodb_items`, hash key only, no sort key/indexes, Scan capped at 100). Duplicate table → 409 (`ErrDynamoTableExists`). |
| IAM — EvaluatePolicy | PARTIAL | `backend/internal/providers/iam.go:82` (`Evaluate`); `providers.go` iam entry: "Real evaluation of a subset … No ARNs, principals, policy variables, or other condition operators." HTTP: `POST /api/v1/aws/iam/evaluate` (`httpapi/iam.go`). Phase 2: extended to stored identities (user/group/role/policy ids) and inline policies, returns the evaluation trail (`EvaluateWithTrail`). |
| IAM — Users / Groups / Roles / Policies CRUD | API_EMULATED (since Phase 2) | `backend/internal/httpapi/iam.go`; `backend/internal/store/iam.go` + `postgres_iam.go`; migration `008_iam.sql`. Admin-only CRUD; access keys secret-once, SHA-256 hash at rest; AssumeRole short-lived sessions with expiry enforced. Every stored policy document is probe-evaluated before storage (`ValidatePolicyDocument`). Trust policies recorded, not evaluated. Registry capabilities are `API_EMULATED`, never `REAL`. |
| STS — AssumeRole / GetCallerIdentity | SIMULATED | `backend/internal/providers/simulators.go:80-109` — locally generated credentials, "grant no AWS access", in-memory only. No HTTP route. |
| KMS — CreateKey / Encrypt / Decrypt / Rotate | PARTIAL (durable local envelope encryption, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_kms_keys`); `backend/internal/phase6/phase6.go` (envelope crypto: AES-256-GCM, random nonce, base64 nonce‖ciphertext; master key from `CLOUDFORGE_KMS_MASTER_KEY` or the JWT secret). Keys survive restarts; wrapped keys never leave the server; rotate keeps previous versions so old ciphertext still decrypts. Replaces the in-memory simulator (`providers/simulators.go`); the registry entry is now `cloudforge-kms`/PARTIAL. NOT AWS KMS, no HSM. |
| Secrets Manager — CreateSecret / GetSecret / PutSecretValue | PARTIAL (encrypted at rest, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_secrets`). Values are envelope-encrypted with a fresh data key wrapped by a KMS key; list responses never include the value or ciphertext; versions increment on put. No automatic rotation. |
| Athena — StartQueryExecution | PARTIAL (PostgreSQL SQL, not Presto/Athena, since Phase 6) | `backend/internal/httpapi/phase6.go`; `backend/internal/phase6/phase6.go` (`ValidateAthenaSQL`: single read-only SELECT/WITH only). Runs against platform Postgres or an RDS instance DB; executions recorded. No workgroups, no CTAS. |
| Glue — StartCrawler | PARTIAL (real crawls, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_glue_crawlers`, `p6_glue_tables`). Postgres `information_schema`, DynamoDB key attributes, S3 via MinIO; per-source status, run PARTIAL if any source fails. Catalog only — no ETL jobs. |
| Kinesis — PutRecord / GetRecords | API_EMULATED (durable Postgres streams, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_kinesis_streams`, `p6_kinesis_records`); `backend/internal/phase6/phase6.go` (`KinesisShard`: SHA-256 partition-key hash). Shard_count 1–8, 1 MiB cap, monotonic sequence numbers, sequence-order reads. Streams are Postgres rows, not a streaming broker. |
| Firehose — PutRecord | PARTIAL (real delivery records, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_firehose_streams`, `p6_firehose_deliveries`). Destination S3 (MinIO) or a Postgres table; a real delivery record per put; FAILED delivery returns 502; counters only on DELIVERED. No buffering, no Lambda transforms. |
| QuickSight — CreateDashboard | PARTIAL (charts from live query results, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_quicksight_dashboards`). A dashboard is a saved Athena query + bar/line chart rendered from the live query results; `chart_error` when the query is missing or fails. No sample data, no SPICE. |
| SageMaker — notebooks / training jobs / models / endpoints | PARTIAL (real OLS training + real endpoints, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_sagemaker_*`); `backend/internal/phase6/phase6.go` (`TrainLinear`: OLS via normal equations; builtin `housing`/`study` datasets, 10 rows each). Notebook cells run in the sandboxed Lambda runner with real output. Training returns real MSE/RMSE/R² and fails honestly on degenerate data. Endpoints serve the stored model artifact from disk. No scikit-learn in this sandbox — pure-Go OLS, labelled as such; never a fake accuracy. |
| Bedrock — InvokeModel | SIMULATED (concept playground, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_bedrock_invocations`); `backend/internal/phase6/phase6.go` (`BedrockTemplateResponse`). 4 concept models, all `supported: false`; the response is a labelled template that echoes the prompt and states no model ran. No real model, nothing sent to any provider, no paid third-party AI API. |
| DMS — CreateReplicationTask | PARTIAL (real row-by-row copy, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_dms_tasks`, `p6_dms_logs`). Platform Postgres ↔ RDS instances; target table built from source `information_schema` column types; real row counts + per-table logs; ≤5000 rows/table. Full-load only, no CDC. |
| DataSync — CreateTask | PARTIAL (real copy + SHA-256 verification, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_datasync_tasks`, `p6_datasync_logs`). Locations `s3:<bucket>[/prefix]` or `local:<path>` (local paths confined to the workdir root, `..` rejected); SHA-256 verification per file. No scheduling, no NFS/SMB agents. |
| Transfer Family | UNSUPPORTED (explicit Phase 6 decision) | `backend/internal/httpapi/phase6.go` (`GET /api/v1/aws/transfer/info`); `frontend/src/pages/MigrationConsoles.tsx` (`TransferConsole`). No `sshd`/`sftp-server` binary on the host, so no SFTP server can run; the info endpoint returns the decision, the reason, the binaries checked, and DataSync as the alternative. |
| Migration Hub | PARTIAL (aggregation only, since Phase 6) | `backend/internal/httpapi/phase6.go` (`GET /api/v1/aws/migrationhub/tasks`). Aggregates the real DMS + DataSync tasks with a status summary; creates nothing. |
| GuardDuty — ListFindings / scan | PARTIAL (real audit-log evidence only, since Phase 6) | `backend/internal/httpapi/phase6.go` (scan reads `auth.login_failed` from the real audit log, last 24 h, grouped by client IP; ≥5 failures → brute-force MEDIUM, ≥5 + ≥3 distinct emails → credential stuffing HIGH; dedupes open findings; 0 findings without evidence). Failed logins are audited with an empty actor (stored as NULL) because `audit_logs.user_id` is a UUID column — the literal `"anonymous"` silently dropped every record (found live 2026-11-22). Only failed-login evidence is analysed. |
| Security Hub — GetFindings | PARTIAL (aggregation only, since Phase 6) | `backend/internal/httpapi/phase6.go`. Aggregates the real findings in the shared `findings` table (GuardDuty/Inspector/Config/Advisor); creates none of its own. |
| Inspector — StartAssessmentRun | PARTIAL (real resource checks, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_inspector_assessments`). Checks real state: open SGs, public S3 bucket policies, unencrypted lab instances; S3 check skipped + recorded when MinIO is down. No agent-based CVE scanning. |
| Config — PutConfigRule / evaluate | PARTIAL (real evaluation, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_config_rules`, `p6_config_results`, `p6_config_items`). 3 rule types, evaluated against real lab resources/S3; one result row + one finding per non-compliant resource; snapshots record current resources. |
| WAF — CreateWebACL / evaluate | SIMULATED (rules really evaluated; evaluation only, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_waf_acls`); `backend/internal/phase6/phase6.go` (`EvaluateWAF`: first match wins, default allow). The ACL does not block live API traffic. |
| VPC Peering / Transit Gateway | SIMULATED (real route entries in lab state, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_vpc_peerings`, `p6_transit_gateways`). Peering/TGW attachments are real route-table entries in the VPCs' lab state (same-VPC and overlapping-CIDR peerings refused). No engine peering/TGW RPC — not kernel peering. |
| VPC Endpoints | SIMULATED (endpoint records, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_vpc_endpoints`). Services s3/dynamodb/sqs/sns; DNS name `<service>.cf-local-1.vpce.local` is a teaching name, not registered. |
| Network Firewall | SIMULATED (rules really evaluated; engine sync best-effort, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_netfirewalls`); `backend/internal/phase6/phase6.go` (`EvaluateFirewall`: priority order, default deny). Policy is sent to the engine's `CreateFirewallPolicy` when a lab is attached; `engine_policy_applied` is true only on success. Does not filter live lab traffic. |
| VPC Flow Logs | SIMULATED (engine-telemetry snapshots only, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_flowlog_groups`, `p6_flowlog_records`). `/capture` calls engine `CollectTelemetry` with the required `RequestMeta` and writes one record per real telemetry point; engine down → honest 503, zero records. No per-packet flows are synthesised. |
| Systems Manager — inventory / RunCommand | PARTIAL (needs a host agent, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_ssm_commands`); `backend/internal/hostagent/client.go`. Inventory and Run Command go through the real host agent (allowlisted ops, real output); without one both return an honest 503. Instances list works without an agent (real labs + instance resources). |
| Trusted Advisor | PARTIAL (5 real checks, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_advisor_results`). Checks real state: idle labs, unattached SGs, open SGs, quota usage, S3 buckets without versioning; one finding per match. |
| Cost Explorer / Budgets | SIMULATED (Cost Explorer) / PARTIAL (Budgets) (educational estimates, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_cost_budgets`); `backend/internal/phase6/phase6.go` (`ComputeCost`, `DefaultPricing`). Costs computed from real usage (lab/RDS/cache hours, S3 bytes, SQS messages, Lambda invocations) at published teaching rates; labelled "USD (educational estimates — not an AWS bill)". Budget `alert`/`usage_percent` evaluated on every read; zero/negative limit refused. |
| Organizations | PARTIAL (real users, since Phase 6) | `backend/internal/httpapi/phase6.go` (`p6_org_units`). Units over the real platform user store; members must exist (duplicate → 409); delete refused while child units exist. No SCPs, no AWS accounts. |
| SQS — CreateQueue / SendMessage / ReceiveMessage / DeleteMessage | API_EMULATED (durable Postgres queues, since Phase 3) | `backend/internal/httpapi/phase3.go`; `backend/internal/store/phase3.go` (`p3_sqs_queues` / `p3_sqs_messages`); `backend/internal/phase3/phase3.go` (`SQSMessageVisible`, `SQSShouldDLQ`). Receive hides until the visibility timeout (does not delete); at `max_receives` the message moves to the DLQ instead of being returned; redrive re-enqueues (new id). No FIFO, no long polling. The old in-memory simulator (`providers/simulators.go`) is superseded for these operations. |
| SNS — CreateTopic / Publish / Subscribe | API_EMULATED (durable Postgres topics, real fan-out, since Phase 3) | `backend/internal/httpapi/phase3.go` (`p3_sns_topics` / `p3_sns_subscriptions` / `p3_sns_deliveries`). Fan-out to SQS queues (durable enqueue), HTTP(S) endpoints (real POST, SSRF-guarded to loopback), Lambda functions (real invocation); every delivery recorded. Email/SMS not built (400). The old in-memory simulator is superseded for these operations. |
| EventBridge — PutRule / PutEvents | API_EMULATED (durable Postgres rules, since Phase 3) | `backend/internal/httpapi/phase3.go` (`p3_eventbridge_rules`); `backend/internal/phase3/phase3.go` (`PatternMatches`: source / detail-type arrays + nested detail subset). Targets SQS/SNS/Lambda, validated at rule creation. No schedule expressions, no input transformers, no AWS service events (PutEvents only). The old in-memory simulator is superseded for these operations. |
| VPC — CreateVpc (registry) | SIMULATED | `providers.go` vpc entry ("simulation on Linux network namespaces, bridges and nftables — not AWS VPC"). The real path is lab resources + engine, not this registry entry. Phase 2: full lifecycle via `backend/internal/httpapi/vpc.go` + `resources.go` (VPC/subnet CRUD with CIDR validation, routes, SG rules, IGW attach/detach, NAT, tags); engine firewall policy is a one-shot snapshot (no update/delete RPC). NACLs: API_EMULATED since v3 (`backend/internal/httpapi/exam.go`, real first-match evaluation) — still no engine RPC, so no live traffic is filtered. |
| EC2 — RunInstances / DescribeInstances (registry) | API_EMULATED **(claim disputed — see note)** | `providers.go` ec2 entries claim `aws-emulator` backend requiring `AWS_EMULATOR_ENDPOINT` (`http://localhost:4566`); no emulator service in compose, no EC2 HTTP handler. Real instance provisioning is labs/host-agent, not this entry. Until the emulator is deployed or the entry is corrected, treat EC2-as-an-API as **not started**. Phase 2 (real path): Start/Reboot wired (`backend/internal/httpapi/instances.go`); instance creation now starts the VM (was left `"defined"` — fixed 2026-11-22). |
| Lambda — CreateFunction / InvokeFunction | PARTIAL (subprocess sandbox, since Phase 3 — the Docker-invoker claim is corrected, not built) | `backend/internal/phase3/phase3.go` (`RunLambda`: python3/node, timeout, output cap, prlimit memory cap, temp workdir, uid 65534 when root, no host shell); `backend/internal/httpapi/phase3.go` (versions/aliases, SQS/EventBridge triggers, invocations list). NOT container isolation, NOT AWS Lambda: no VPC attachment, managed scaling, layers, or execution-role model. Timeout → honest 504 with `timed_out: true`, invocation recorded. |
| API Gateway — CreateRestApi / CreateResource / CreateDeployment | PARTIAL (real routing + real request logs, since Phase 3) | `backend/internal/httpapi/phase3.go` (`p3_apigw_apis` / `p3_apigw_resources` / `p3_apigw_stages` / `p3_apigw_logs`). Integrations `lambda` (function id/name resolved) or `http` (SSRF-guarded URL). Deploy snapshots resources into a stage; invoke via `/api/v1/gateway/{apiId}/{stage}/*` (platform auth required); every invocation logged. REST/HTTP only (no WebSocket APIs); Lambda error → 502, timeout → 504. |
| RDS — CreateDBInstance / DeleteDBInstance / CreateDBSnapshot / RestoreDBInstanceFromDBSnapshot | PARTIAL (real Postgres DB + role on the platform Postgres, since Phase 3) | `backend/internal/phase3/phase3.go` (`RDSCreateDatabase` / `RDSDropDatabase` / `RDSSnapshot` / `RDSRestore`); `backend/internal/httpapi/phase3.go`. Engine postgres 14/15/16 only; master password generated, shown once, SHA-256 hash at rest; snapshot is a real `pg_dump -Fc` file; restore creates a new instance via `pg_restore`. Start/stop per instance UNSUPPORTED (409). Platform Postgres user needs `CREATEDB` + `CREATEROLE`. No containerised per-instance Postgres (not built). |
| ElastiCache — CreateCacheCluster / DeleteCacheCluster | PARTIAL (real namespaced Redis, lite mode, since Phase 3) | `backend/internal/httpapi/phase3.go`, `phase3_redis.go` (key prefix `cf-cache:<cluster-id>:` + best-effort ACL user on the platform Redis). Engine redis only; full mode (dedicated container) honestly 503. Keys browser is a read-only SCAN of the prefix (max 100); flush deletes only that cluster's prefix. |
| ELB — CreateLoadBalancer / CreateListener / CreateTargetGroup / RegisterTargets | SIMULATED (in-process round-robin + health checks, since Phase 3) | `backend/internal/httpapi/phase3.go`; `backend/internal/phase3/phase3.go` (`RoundRobin`, `ProbeTarget`: HTTP GET to `http://host:port<health_check_path>` when set, else TCP dial, 2 s timeout). Only targets whose last probe said `healthy` are eligible; `/route` returns the target a request would go to — no traffic is actually proxied; DNS name is a local teaching name, not registered. |
| Route 53 — CreateHostedZone / ChangeResourceRecordSets | SIMULATED (durable records; no DNS server, since Phase 3) | `backend/internal/httpapi/phase3.go` (`p3_dns_zones` / `p3_dns_records`); `backend/internal/phase3/phase3.go` (`ValidateDNSRecord`, `ResolveDNS` incl. `*.` wildcards). A/CNAME/TXT only; one record set per (name, type); zone cannot be deleted while it holds records. No DNS server serves the records to the lab network. |
| EKS — CreateCluster | UNSUPPORTED (explicit Phase 4 decision) | `providers.go` eks entry; `backend/internal/phase4/phase4.go` (`EKSDecision`, `K8sLearningPath`); `GET /api/v1/aws/eks/info` (`httpapi/phase4.go`); `frontend/src/pages/EksConsole.tsx`. Reasons: no Docker/Kubernetes in the sandbox to verify a cluster against; a managed-Kubernetes control plane would dominate lite classroom-laptop mode; the concepts map onto the ECS runtime, so they are taught for real instead of against a diagram. The console shows the decision + reason + 6-concept learning path (Pod, Deployment, Service, ConfigMap/Secret, Namespace, Job/CronJob) with ECS equivalents; Create cluster is disabled. k3s-based EKS remains a possible future phase on a real Docker/KVM host, gated behind a capability check. |
| ECR — CreateRepository / DescribeImages / PutImage | PARTIAL (local registry, since Phase 4) | `backend/internal/phase4/ecr.go` (`LocalDiskBackend`: blobs `<workdir>/ecr/blobs/sha256/<hex>`, manifests on disk, digest verified on store, manifest stored only after every referenced blob exists; `RemoteRegistryBackend` proxies to `registry:2` when `ECR_REGISTRY_URL` is set); `backend/internal/httpapi/phase4.go` (`p4_ecr_*`); `frontend/src/pages/EcrConsole.tsx`. Push via API accepts manifest+blobs (client digests) or raw `layers` base64 (manifest synthesised server-side); `/v2/` serves the Docker Registry HTTP API subset (catalog, tags/list, manifests, blobs, monolithic upload). Scan is an integrity scan (every layer re-read + digest-verified), explicitly NOT a vulnerability (CVE) scan. No Docker Hub compatibility, no chunked/resumable uploads, no OCI index, no auth (local-only, Phase 7 item). Full-mode `registry:2` service is wired in `deployments/compose/docker-compose.yml` (`optional` profile, `ECR_REGISTRY_URL=http://registry:5000`) but UNVERIFIED here (no Docker in this sandbox). |
| ECS — CreateCluster / RegisterTaskDefinition / CreateService / RunTask | PARTIAL (subprocess runtime, since Phase 4) | `backend/internal/phase4/phase4.go` (`TaskRunner`: argv subprocess, timeout, output cap, `prlimit` memory cap, temp workdir, uid 65534 when root); `backend/internal/httpapi/phase4.go` (`p4_ecs_*`); `frontend/src/pages/EcsConsole.tsx`. Task status comes from the real process (PENDING → RUNNING → STOPPED with a real exit code); services reconcile desired count by starting/stopping real tasks; rolling deployments stop old tasks and start new ones. **Reconcile circuit breaker** (`p4ReconcileService`): if 5+ tasks for a service were created in the last 60 s, restarts pause and the service reports `reconcile_throttled` with a reason; a service update clears it. Without this, an instantly-exiting command under a service restarts forever (seen live: 3,200 task rows in ~2 min). No Docker: no container is started, no image is pulled — the image is recorded and matched against the ECR catalogue. No VPC networking, service discovery, or load balancer integration; desired count max 10; task timeout 3600 s. |
| Fargate — RunTask | SIMULATED (same subprocess runtime, no cluster, since Phase 4) | Same `TaskRunner` as ECS with `launch_type: FARGATE`; no cluster required or used (`httpapi/phase4.go`); `EcsConsole.tsx` (Fargate section). Not AWS Fargate: no managed capacity, no container — honestly labelled SIMULATED. |
| CodeBuild — CreateProject / StartBuild | PARTIAL (real local builds, since Phase 4) | `backend/internal/phase4/phase4.go` (`ParseBuildspec`, `RunShell`); `backend/internal/httpapi/phase4.go` (`p4_codebuild_*`); `frontend/src/pages/CodeBuildConsole.tsx`. Git clone (local repos only, `-c safe.directory`, cloned as the API user then chowned to 65534 so build steps can write) or directory copy (symlinks skipped); buildspec phases install/pre_build/build/post_build in a sandboxed subprocess with timeouts; real logs + artifacts on disk. Local sources only (no http/ssh remotes); no Docker build environment. |
| CodePipeline — CreatePipeline / StartPipelineExecution | PARTIAL (real local pipelines, since Phase 4) | `backend/internal/httpapi/phase4.go` (`p4_codepipeline_*`); `frontend/src/pages/CodePipelineConsole.tsx`. Stages run in order: source (local git clone/copy) → build (real CodeBuild run against the pipeline's source checkout) → deploy (ECS service update / Lambda update / Terraform). Build stages resolve by `project_id` or `build_project_name`; deploy stages need `target` + `service_id`/`task_definition_id` (ECS) or `function_id`/`code_file` (Lambda). Terraform deploy honestly fails without Docker (`terraform_unavailable`). A failed stage stops the execution; later stages never run. |
| CodeDeploy — CreateApplication / CreateDeployment | PARTIAL (real deployments + rollback, since Phase 4) | `backend/internal/httpapi/phase4.go` (`p4_codedeploy_*`); `frontend/src/pages/CodeDeployConsole.tsx`. ECS rolling deployment (updates the service, stops old tasks, reconciles, waits ≤60 s for desired count RUNNING on the new revision) or Lambda code update (version+1, stored code verified == deployed code). Rollback creates a new deployment restoring the previous revision; only a SUCCEEDED deployment can be rolled back. ECS + Lambda targets only. |
| CloudFormation — CreateStack / CreateChangeSet | PARTIAL (own template engine, 6 types, since Phase 4) | `backend/internal/phase4/phase4.go` (`ParseCFNTemplate` JSON-first then YAML, `ValidateCFNTemplate`, `PlanCFNChanges` with CREATE/UPDATE/DELETE/NO_CHANGE/UNSUPPORTED, `CFNResolveValue` Ref / Fn::Join / Fn::Sub); `backend/internal/httpapi/phase4.go` (`p4_cfn_*`); `frontend/src/pages/CloudFormationConsole.tsx`. Separate template engine with its own stack state — NOT Terraform. 6 types provision for real: SQS, SNS, DynamoDB (Postgres subset), Lambda (inline `Code.ZipFile`), S3 (MinIO), RDS (platform Postgres). Other types are UNSUPPORTED in the change set, never silently skipped; a change set with only UNSUPPORTED changes is not executable (409 on execute). Failed creates roll back the resources they made; resources deleted by an update are NOT restored (event log says so). S3 stack fails CREATE_FAILED when MinIO is down. |
| Terraform — init / validate / plan / approve / apply / destroy | REAL (execution) | `backend/internal/terraform/terraform.go` (`ContainerRunner`, pinned `hashicorp/terraform:1.9.8`, Docker Engine API v1.41); `backend/internal/jobs/terraform.go`. Execution is real; the infrastructure it manages is the local emulator/simulated stack. Container execution unverified in this sandbox (no Docker; honest 503). |
| Terraform Learning — dashboard / courses / editor / plan preview / state / provider | PARTIAL (real local checks + real container execution when Docker is present, since Phase 5) | `backend/internal/tflearn/` (`lessons.go`: L1 15 + L2 20 + L3 20 lessons, L4 7 projects; `hcl.go`: project-wide HCL validation, formatting, static plan diff, drift; `provider.go`: 9 cloudforge_* types with API mappings); `backend/internal/httpapi/tflearn.go` (25 routes; migration `011_tflearn.sql`, tables `p5_tflearn_*`); `frontend/src/pages/TerraformLearning.tsx` (`/terraform-learning`, `/services/terraform-learning`). Dashboard/progress/projects/runs/achievements are real backend data. Local validate/format/plan-preview are real local checks labelled "local" — the preview is a static diff (source `cloudforge-local-diff`), not a terraform binary plan, and approving it is a learning record, not a container apply gate. Container execution delegates to the M7 runner through a linked lab (apply: approved M7 plan + confirm; destroy: confirm); without Docker it is an honest 503 `terraform_unavailable` with a FAILED run recorded. State is append-only versioned snapshots refreshed from the linked lab's latest succeeded container plan; restores create a new version; locks are 10-minute with holder/admin force-unlock. No Terraform Cloud / remote backend / module registry; multi-file projects are concatenated into one document for container execution. |
| Terminal shell | SIMULATED (simulated mode) / REAL exec path (Docker/libvirt) | `backend/internal/terminal/terminal.go:211` (simulated allowlist, output labelled simulated); `backend/internal/hostagent/ops.go:237` (allowlist, no arbitrary shell). |
| Curriculum / lessons / assessments / achievements | REAL (content + progress) | `curriculum/*.yaml`; `backend/internal/curriculum/`; Postgres tables `005_curriculum.sql`. XP/achievements are real platform state, not AWS state. |
| Monitoring — `/metrics`, `/readyz`, Grafana | REAL (local metrics) / PARTIAL (as "CloudWatch") | `backend/internal/metrics/metrics.go`; `infrastructure/monitoring/`. Metrics are real control-plane measurements; there is no AWS CloudWatch. Phase 2: `GET /api/v1/monitoring` (live Prometheus series + readiness as JSON) and in-app CloudWatch console; alarms are threshold rules evaluated live, definitions in server memory (lost on restart). Phase 7: `/metrics` is token-authenticated (`X-Metrics-Token` or Bearer) / loopback-only in dev; nginx no longer proxies it. |
| Audit log | REAL (local log) / PARTIAL (as "CloudTrail") | `backend/internal/store/postgres.go` (audit rows, 90-day `audit_cleanup`); admin-only `GET /api/v1/audit`. Real platform audit, not AWS CloudTrail. Phase 2: filtered list + CSV/JSON export (admin-only, max 1000 events), in-app CloudTrail console, `GET /api/v1/labs/{id}/events` per-lab history. Phase 7: full export jobs (cap 10,000 events, retention newest 7) + worker archive-before-delete (`audit-archive-<date>.csv`). |
| Backup / restore | REAL | `scripts/backup.sh` / `restore.sh` (pg_dump/pg_restore; round-trip verified on a scratch DB). |
| Frontend service catalog (17 entries) | — (navigation aid, not a service) | `frontend/src/services/catalog.ts` — entries redirect to `/labs`, `/learn`, `/console`, `/courses`, `/`; they are not service consoles and must not be mistaken for implemented services. |

## 3. The READY rule (binding for Phases 1–6)

> **No resource is shown `READY` until the operation actually succeeded.**

Concretely, for every service console built in Phases 1–6:

- **Create is async until proven otherwise.** If provisioning goes through
  the worker/jobs table, the API returns **202** and the resource state is
  `CREATED`/`PROVISIONING` — never `READY` on the create response.
  (Precedent: `handleCreateLab`, `httpapi.go:1057-1060`.)
- **State comes from the backend that did the work.** A subnet is `READY`
  only after the engine's `CreateSubnet` returned success; an instance only
  after the host agent returned success; a bucket only after MinIO
  acknowledged it; a Terraform-managed resource only after `apply`
  succeeded and the worker synced it into `lab_resources`.
- **Failure is visible, not silent.** Engine/host-agent/backend failure
  surfaces as `FAILED` with the error (lab precedent:
  `labs.go:272`, `jobs.go:175`), or as an honest 502/503 from the API —
  never as a `READY` row, never as an empty success.
- **Simulated success is labelled.** When the simulated backend "creates"
  something, the row/badge says `SIMULATED` (and the detail view says what
  was actually done: "validated and recorded in memory; no kernel objects
  created"). Simulated success is still success — it is just never
  presented as real infrastructure.
- **The live walk checks this.** The Phase 1–6 verification walk
  (create → list → detail tabs → action → delete) must include a failure
  case: stop the backend the resource depends on (or point at a dead
  endpoint) and confirm the UI shows failure/pending, not `READY`.
  (Precedent: engine-down → lab `FAILED`; Redis-down → health reports
  `redis_connected:false`, courses still served.)

## 4. Fidelity badges (UI contract)

Every resource row, detail header, and console home in Phases 1–6 carries
a fidelity badge (`AwsBadge.tsx` is the existing component to extend):

| Badge | When shown |
|---|---|
| `REAL` | Operation ran against real local infrastructure (KVM/Docker/Terraform binary/Postgres). |
| `API_EMULATED` | Operation ran through a real local API-compatible backend (MinIO/DynamoDB Local). |
| `SIMULATED` | Educational in-memory/local-stand-in implementation. |
| `PARTIAL` | Service-level badge when only a subset of operations exists; each operation row shows its own badge. |
| `UNSUPPORTED` | The operation/service is not built. Shown **disabled** with a reason ("Not built yet — Phase N"), never as an active button. |

Badge text must name the backend where it matters, e.g.
`SIMULATED · in-memory, lost on restart` (KMS/SQS) or
`API_EMULATED · local MinIO, not AWS S3`. The catalog's per-service
"Differs" column (`SERVICE_CATALOG.md`) is the source of the badge
footnotes.

## 5. v3 — AWS-reference console UI (lite exam set, 2026-11-22)

Every service in the lite (SAA-C03 / SAP-C02) set has a console in the
pattern captured from the real VPC console (ap-southeast-2):

- **Home dashboard** (default landing): notification banner, action
  links ("Create X", "Launch…"), "Resources by Region" — a two-column
  grid of resource cards, NOT a table. Every card: linked resource-type
  heading, region label, linked live count, collapsed "Regions"
  expander. Then Service Health, Settings (link list) and Additional
  Information (link list). No data table, columns, pagination or detail
  tabs on Home; those live on the list/detail pages.
- **Left nav**: grouped like the reference (VPC: Virtual private cloud
  / Security / PrivateLink and Lattice / Network Firewall / VPN /
  Transit Gateways / Traffic Mirroring, plus standalone items). Items
  that are separate consoles link to them; items with no backend are
  shown disabled with the reason.
- **List pages**: `AwsTable` with the AWS console's columns, filters
  and Actions menu; **detail tabs** via `ConsoleTabs` (Details /
  Configuration / Monitoring / Tags / … per service).
- **Wiring**: every action calls the real backend endpoint where one
  exists (see §2). Where a capability is SIMULATED / PARTIAL /
  UNSUPPORTED the button is disabled with the reason and the Home card
  says so — never a no-op pretending success, never simulated data
  presented as real AWS.
- **Services with no backend** (Auto Scaling, Aurora, Backup, Macie,
  Audit Manager, Artifact, CloudFront, EBS, EFS, FSx, Shield, Control
  Tower, IAM Identity Center, RAM, VPN, Direct Connect, Global
  Accelerator, MGN, Compute Optimizer, Savings Plans) get the same
  Home + operations-list + detail-tab treatment in
  `frontend/src/pages/LiteServiceConsole.tsx`, driven by the live
  registry (`GET /api/v1/services`) and the Resource Explorer counts
  (0 recorded, never invented), with a "What you can do instead" link
  to the available service teaching the same concept. Backup notes the
  host-side `scripts/backup.sh` / `scripts/restore.sh` model.
- **Branding**: CloudForge's own (dark navy header, orange primary
  buttons, CloudForge name). No AWS logo, brand colours, trademarks,
  and no claim this is AWS.
- Non-lite services keep their v2 consoles unchanged in v3.

### 5.1 Survey reconciliation (Workstream 1b, 2026-11-22)

The consoles above were reconciled against the read-only survey in
`docs/reference/aws-console-survey-2026-11-22.md` (45 of 46 services
checked; DataSync unchecked). List tables use the survey's exact
columns; a column with no backend data renders "Not available" with
the reason, never an invented value. Columns that were CloudForge-only
inventions were dropped from the tables (their data, where real, moved
to the detail panel — e.g. Lambda version/timeout/memory, IAM role
ARN/max session). Services the survey found to be sections of another
console are sections, with the old route redirecting to the section:

| Service | Survey outcome applied |
|---|---|
| S3 | Buckets table: Name, Region, IAM Access Analyzer, Creation date. Lands on the Buckets list, not a dashboard. |
| IAM | Lands on Roles. Roles table: Role name, Trusted entities (read from the stored trust policy), Last activity (Not available — no backend source). |
| Lambda | Functions table: Function name, Description (Not available), Runtime, Last modified (updated_at). |
| ELB | Section of the EC2 console (Load Balancing group); `/services/elb` redirects to `/services/ec2?section=load-balancers`. Load Balancers table: Name, State, Type, Scheme, IP address type, VPC ID, Availability Zones, Security groups, DNS name, ARN, Date created. |
| Auto Scaling | Section of the EC2 console; `/services/autoscaling` redirects to `/services/ec2?section=autoscaling`. Empty-state marketing intro with a disabled Create button. |
| EBS | Section of the EC2 console (Elastic Block Store group); `/services/ebs` redirects to `/services/ec2?section=volumes`. Empty Volumes table with the survey's exact columns. |
| Transit Gateway | Section of the VPC console; `/services/transitgateway` redirects to `/services/vpc?section=tgw`. Attachments table: Name, Transit gateway attachment ID, Transit gateway ID, State (+ VPC). |
| VPN | Section of the VPC console; `/services/vpn` redirects to `/services/vpc?section=vpn`. Empty VPN Connections table with the survey's exact columns. |
| Cost Explorer | Breakdown table: Service, Total cost, % of total cost (grouped from the real priced usage lines, kept in a details block). |
| Budgets | Table: Name, Thresholds, Budget, Amount used, Forecasted amount (Not available — no forecasting), Current vs. budgeted, Forecasted vs. budgeted. |
| CloudFront | Distributions list with the survey's exact columns, empty (UNSUPPORTED). |
| EKS | Clusters list with the survey's exact columns, empty (UNSUPPORTED). |
| CloudFormation | Stacks table: Stack name, Status, Created time, Description (Not available). |
| CodeBuild | Projects table: Name, Source provider, Repository, Latest build status, Description (Not available), Last Modified. |
| CodePipeline | Pipelines table: Name, Latest execution status, Latest source revisions (Not available), Latest execution started, Most recent executions. |
| GuardDuty, Security Hub, Organizations, Redshift, Kinesis, DMS | RESTRICTED / not accessible in the reference account — no layout was captured beyond the restriction dialog. The consoles' layouts follow public console documentation, not an observed layout; each console's Home banner says so. The restriction is never presented as the design. Redshift has no CloudForge console at all. |
| DataSync | NOT YET CHECKED in the survey (browser unavailable) — layout is from public console documentation and is marked UNVERIFIED in the console's Home banner and in the survey file. |
| Wizards | No create/edit wizard was opened in the survey (read-only), so no AWS wizard steps were observed. CloudForge wizard steps (EC2 Launch instance, S3 Create bucket, Lambda Create function) are CloudForge's own design, inferred from the list-page actions, and each wizard carries a footnote saying so. |
| Batch | No CloudForge console exists; the survey entry is backup reference only — nothing to reconcile. |
| All other surveyed services (VPC, EC2, RDS, Route 53, DynamoDB, ElastiCache, SQS, SNS, EventBridge, API Gateway, ECS, CloudWatch, CloudTrail, KMS, Secrets Manager, WAF, Backup, EFS, Cognito, SES, Athena, Glue, SageMaker) | Already matched the survey pattern (Home dashboard with resource cards, no table on Home; list page with the service's own columns) — no change. |

## 6. v3 — exam services in lite (SAA-C03 / SAP-C02, 2026-11-22)

The 24 services listed in §5 as "with no backend" now have a real
backend: `backend/internal/httpapi/exam.go` (generic CRUD +
custom actions, routes `/api/v1/aws/<service>/...`), the providers
registry (`backend/internal/providers/providers.go`), the service
catalog (`backend/internal/httpapi/services.go`, 109 `svc()` rows
in total per the Workstream 4 audit), and one shared Postgres
table `p7_exam_services` (migration `014_exam_services.sql`).
§5's "no backend" list is superseded for these services — the
frontend wiring (Workstream 1) can now show real counts and call
real endpoints instead of the "What you can do instead" fallback.

| Service | Fidelity | What is real, what is not |
|---|---|---|
| Auto Scaling | API_EMULATED | Groups are durable records (min/desired/max, scale action, bounds enforced, max 10). Scaling sets the recorded desired capacity; no instances are launched — real instances come from the lab itself. |
| Global Accelerator | API_EMULATED | Accelerators + endpoint groups are durable records; failover switches the recorded active region and appends history. Static IPs are teaching addresses in `192.0.2.0/24` (TEST-NET-1); no traffic is routed. |
| Direct Connect | API_EMULATED | Connections are durable records; confirm moves REQUESTED → AVAILABLE. No carrier, LOA-CFA or BGP session exists. |
| Control Tower | API_EMULATED | Landing zones + guardrails are durable records. A guardrail is a recorded setting, not an enforced SCP. |
| IAM Identity Center | API_EMULATED | Permission sets + assignments are durable records. No login or federation is created. |
| RAM | API_EMULATED | Resource shares + principals are durable records. No second account exists, so sharing changes no access. |
| Storage Gateway | API_EMULATED | Gateways + file shares are durable records; activation is a teaching state change (OFFLINE → ONLINE). No appliance is deployed, no data is cached. |
| Snowball | API_EMULATED | Jobs are durable records advanced along a teaching lifecycle (created → preparing → shipped → delivered → completed); forward-only, no device ships. |
| Lake Formation | API_EMULATED | Permission grants are durable records, not enforced — no data lake exists here. |
| Macie | API_EMULATED | Classification jobs scan S3 bucket names, DynamoDB table names and job-scope names for sensitive-data patterns and write real findings to the shared `findings` table. Names only — object contents are never read. |
| Compute Optimizer | API_EMULATED | Recommendations are computed from real lab state (profile, stopped/running) and say so: LOW confidence, except a stopped lab (terminate, MEDIUM). There is no utilization pipeline — the basis string states this. |
| Savings Plans | API_EMULATED | Plans are durable records; utilization divides the real hourly lab burn (teaching rates) by the commitment. No billing data exists. |
| Audit Manager | API_EMULATED | Assessments are durable records; collect-evidence counts real entries from this deployment's audit log since the assessment was created. |
| Artifact | API_EMULATED | A catalogue of 8 real AWS Artifact report names plus agreement records. No documents are served or redistributed. |
| Aurora | API_EMULATED | Clusters are durable records with writer/reader instances and teaching endpoints; failover swaps the recorded writer/reader roles. No database runs. |
| Backup | REAL (runs) / API_EMULATED (plans, vaults) | A run is a real `pg_dump -Fc` of the platform Postgres into the API workdir, with size + SHA-256 recorded. No database or no `pg_dump` → a FAILED job with the reason, never a fake success. The registry entry for `StartBackupJob` is PARTIAL (the codebase rule `TestNoCapabilityClaimsReal` forbids any registry operation claiming REAL); the catalog row stays REAL, as in v2. |
| MGN | API_EMULATED | Source servers are durable records; the replication lifecycle (NOT_STARTED → REPLICATING → TEST_LAUNCHED → CUTOVER) is a teaching state machine — wrong order is refused. No servers are replicated. |
| FSx | API_EMULATED | File systems + mount targets are durable records. No file system is provisioned. |
| Shield | API_EMULATED | Protections are durable records. No traffic is inspected or mitigated. |
| EBS | API_EMULATED | Volumes are durable records; attach validates the lab really exists (404 otherwise) and marks the volume in-use; delete is refused while in-use. No disk is attached — the lab's disk is unchanged. |
| EFS | API_EMULATED | File systems + mount targets are durable records. No file system is mounted. |
| CloudFront | API_EMULATED | Distributions are durable records with teaching domain names; invalidations are recorded COMPLETED. Nothing is cached or served. |
| VPN | API_EMULATED | Customer/VPN gateways + connections are durable records; a connection validates its gateways exist; tunnel activate/deactivate is a teaching field. No IPsec tunnel exists. |
| NACLs | API_EMULATED | ACLs + numbered rules are durable records; **evaluate is a real evaluation** (lowest number first, stateless, default deny, protocol/port/CIDR matching). Rules do not filter live traffic — no engine RPC. |

**Design.** All 24 services share one collection/table because
their record semantics are identical (CRUD + a few state
transitions); a service that later grows real behaviour gets its
own collection then. Name uniqueness is per (service, kind, owner),
enforced in the handler — not a DB unique constraint — because
different services/kinds may legitimately share a name. Ownership
follows the Phase 3 rule: non-admin users see only their own
records (403 on another user's id).

**Lite cap.** `quota.LiteExamResourceMax`: learner 10, instructor
25, admin 50 records per service per user; full mode uncapped.
Enforced in the create handler (429 `quota_exceeded`). Lab caps
(1 lab / Micro / 30 min) and compose limits are unchanged.

**Still honest gaps in lite (see also the cert map).** S3 object
operations return 503 in lite (MinIO is not in the lite compose
file). Transit Gateway attachments remain route-table entries in
lab state. EKS, Elastic Beanstalk, Batch, Step Functions, Redshift,
Cognito and ACM remain UNSUPPORTED by prior decision or because
they are outside the assigned lite set.
