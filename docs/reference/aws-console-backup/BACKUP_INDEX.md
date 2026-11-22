# AWS console backup — index (Workstream 4, v3 backup)

> **Reference notes only — not built consoles.** Created 2026-11-22 by Workstream 4.
> The backup set is every service in the registry (`backend/internal/httpapi/services.go`)
> that is **not** in the v3 lite set. The lite set (61 services) is being built in lite
> by other workstreams and is listed separately below — it is not duplicated here.
> CloudForge branding only: no AWS logos, no AWS trademarks, no copied assets or
> screenshots, and no claim this is AWS.

## Counts (verified against the registry at generation time)

- Registry services counted: **109** (109 — the task brief said 100, which is stale; the registry grew with the SAA/SAP cert-track services after the GitHub sync of remote `213c33c`)
- Lite-set services: **61** (all 61 are present in the registry; none missing, no duplicates)
- Backup files: **48** (one per remaining registry service; 109 − 61 = 48)
- Registry services that could not be classified: **none** — every registry id mapped to exactly one of the lite set or the backup set.

## How to use this backup

1. Pick a service file below and read it end to end before writing any code — it is meant to be sufficient on its own: purpose, the real console's layout (left nav, home/dashboard sections, list-page columns and filters, detail tabs, wizard steps, action names), CloudForge fidelity today (with the registry mirrors/differs notes), and what would be needed to build it later (backend endpoints, data model, labs/lessons, lite feasibility, scope).
2. Re-check the live equivalent service's console before building — console layouts change, and these notes are a snapshot from 2026-11-22.
3. Build in this order: backend endpoints + data model first, then the console, then tests, then a live walk (create → list → detail tabs → action → delete, including a failure case). A service is not complete on UI alone, backend alone, or tests alone.
4. Follow the fidelity rules: label every operation REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED, disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (`docs/SERVICE_FIDELITY.md` §3).
5. Keep CloudForge branding only. Mirror interaction patterns and workflows, never the brand.
6. When a service is built, move its file out of this backup (or mark it built at the top of the file) and update this index — do not leave a stale backup file describing a service that now exists.

## Backup services

| Service id | Name | Category | Fidelity today | Console today | Build-later scope |
|---|---|---|---|---|---|
| [acm](./acm.md) | Certificate Manager | Security and Identity | UNSUPPORTED | No | **medium** — Local self-signed certificate issuance + envelope-encrypted key storage + validation flow. |
| [apprunner](./apprunner.md) | App Runner | Containers | UNSUPPORTED | No | **medium** — App Runner service/deployment model on the ECS subprocess runtime + ELB routing. |
| [appsync](./appsync.md) | AppSync | Serverless | UNSUPPORTED | No | **large** — Small GraphQL executor + schema/datasource/resolver model; subscriptions out of scope. |
| [athena](./athena.md) | Athena | Analytics | PARTIAL | Yes | **medium** — Workgroups + CTAS + Glue-catalog browsing on the existing Athena executor. |
| [batch](./batch.md) | Batch | Compute | UNSUPPORTED | No | **medium** — Job queue/definitions/environments model + scheduler over the ECS subprocess runner. |
| [beanstalk](./beanstalk.md) | Elastic Beanstalk | Compute | UNSUPPORTED | No | **large** — Environment orchestration composing ELB + instances + RDS + CodeBuild; platform start-command table. |
| [bedrock](./bedrock.md) | Bedrock | Machine Learning and AI | SIMULATED | Yes | **medium** — Knowledge Bases (local chunk retrieval) + Agents (Lambda action groups) + Guardrails; real models stay out of scope. |
| [cloudformation](./cloudformation.md) | CloudFormation | Additional | PARTIAL | Yes | **medium** — More provisioned resource types + real drift detection; StackSets after that. |
| [cloudwatch](./cloudwatch.md) | CloudWatch | Monitoring | PARTIAL | Yes | **medium** — Persisted alarms + metric history + Postgres log groups with a small Insights query language. |
| [codebuild](./codebuild.md) | CodeBuild | Developer Tools | PARTIAL | Yes | **small** — S3 sources/artifacts + JUnit test reports + build caching on the existing runner. |
| [codedeploy](./codedeploy.md) | CodeDeploy | Developer Tools | PARTIAL | Yes | **medium** — Lambda traffic shifting + ECS blue/green (needs ELB target-group weights) + AppSpec hooks. |
| [codepipeline](./codepipeline.md) | CodePipeline | Developer Tools | PARTIAL | Yes | **small** — Manual approval actions + S3/CloudFormation deploy targets on the existing engine. |
| [cognito](./cognito.md) | Cognito | Security and Identity | UNSUPPORTED | No | **large** — User pools/users/groups/app clients on the existing Argon2id/JWT auth; TOTP MFA; hosted UI. |
| [comprehend](./comprehend.md) | Comprehend | Machine Learning and AI | UNSUPPORTED | No | **medium** — Local lexicon/rule-based NLP + naive-Bayes custom classifiers, honestly labelled as approximations. |
| [documentdb](./documentdb.md) | DocumentDB | Databases | UNSUPPORTED | No | **medium** — JSONB document store + MongoDB-like query subset over Postgres; snapshots reuse RDS pattern. |
| [dynamodb](./dynamodb.md) | DynamoDB | Databases | PARTIAL | Yes | **medium** — Sort keys + GSIs + PartiQL + Streams on the existing DynamoDB subset. |
| [ecr](./ecr.md) | ECR | Containers | PARTIAL | Yes | **medium** — Token auth on /v2/ + chunked uploads + lifecycle enforcement on the existing registry. |
| [eks](./eks.md) | EKS | Compute | UNSUPPORTED | Yes | **large** — k3s-backed clusters via the host agent (full mode only, capability-gated); ECS concepts path stays the lite offering. |
| [emr](./emr.md) | EMR | Analytics | UNSUPPORTED | No | **large** — Spark-job teaching runner (subset, labelled) + cluster/step model; real Spark is capability-gated. |
| [glacier](./glacier.md) | S3 Glacier | Storage | UNSUPPORTED | No | **small** — Vault/archive/job model over MinIO (or local disk in lite) with honest retrieval-tier delays. |
| [glue](./glue.md) | Glue | Analytics | PARTIAL | Yes | **medium** — Python-shell ETL jobs + triggers/workflows + connections on the existing crawler/catalog. |
| [lakeformation](./lakeformation.md) | Lake Formation | Analytics | UNSUPPORTED | No | **medium** — Permission/tag model + enforcement hook in the Athena executor and Glue crawler. |
| [learning-center](./learning-center.md) | Learning Center | Learning Center | PARTIAL | Yes | **medium** — Guided-lab wiring for every track + learning paths + instructor dashboard. |
| [lex](./lex.md) | Lex | Machine Learning and AI | UNSUPPORTED | No | **medium** — Bot/intent/slot model + similarity-based NLU + Lambda fulfilment, labelled as teaching NLU. |
| [licensemanager](./licensemanager.md) | License Manager | Additional | UNSUPPORTED | No | **small** — License/configuration records + usage computed from real lab instances + hard-limit launch hook. |
| [migrationhub](./migrationhub.md) | Migration Hub | Migration and Transfer | PARTIAL | Yes | **small** — Servers/applications records + progress roll-up on the existing aggregation endpoint. |
| [mq](./mq.md) | MQ | Application Integration | UNSUPPORTED | No | **medium** — Broker/user/configuration model + durable queues (Stage 1); real RabbitMQ/ActiveMQ container is Stage 2, full-mode only. |
| [neptune](./neptune.md) | Neptune | Databases | UNSUPPORTED | No | **medium** — Vertex/edge store + Gremlin/openCypher query subset over Postgres; snapshots reuse RDS pattern. |
| [opensearch](./opensearch.md) | OpenSearch | Analytics | UNSUPPORTED | No | **medium** — Postgres full-text search backend with a query-DSL subset (Stage 1); real OpenSearch container is Stage 2. |
| [polly](./polly.md) | Polly | Machine Learning and AI | UNSUPPORTED | No | **small** — Local TTS binary (espeak-ng) via the sandboxed runner + lexicon application; 503 when absent. |
| [quicksight](./quicksight.md) | QuickSight | Analytics | PARTIAL | Yes | **medium** — More visual types + RDS/S3/file datasets + sharing on the existing dashboard model. |
| [redshift](./redshift.md) | Redshift | Databases | UNSUPPORTED | No | **medium** — Cluster/workgroup model on the RDS Postgres pattern + Redshift-flavoured query path (COPY/UNLOAD). |
| [rekognition](./rekognition.md) | Rekognition | Machine Learning and AI | UNSUPPORTED | No | **large** — Collections + perceptual-hash face search + tesseract OCR; label/moderation stay disabled without a model. |
| [resourceexplorer](./resourceexplorer.md) | Resource Explorer | Management and Governance | REAL | Yes | **small** — Index Phase 2–6 service resources + saved views + tag search on the existing endpoint. |
| [sagemaker](./sagemaker.md) | SageMaker | Machine Learning and AI | PARTIAL | Yes | **medium** — Logistic regression + k-means + tuning jobs + model registry on the existing OLS core. |
| [servicecatalog](./servicecatalog.md) | Service Catalog | Additional | UNSUPPORTED | No | **medium** — Portfolio/product/provisioned-product model provisioning through the CloudFormation + Terraform engines. |
| [ses](./ses.md) | SES | Application Integration | UNSUPPORTED | No | **medium** — Identities + local mail sink (stored mail, optional SMTP relay) + reputation/suppression. |
| [snowball](./snowball.md) | Snowball | Storage | UNSUPPORTED | No | **small** — Job/address tracker + transfer-time calculator; no device emulation. |
| [ssm](./ssm.md) | Systems Manager | Management and Governance | PARTIAL | Yes | **medium** — Parameter Store + Documents + Automation on the existing Run Command; Session Manager reuses terminal WS. |
| [stepfunctions](./stepfunctions.md) | Step Functions | Serverless | UNSUPPORTED | No | **large** — ASL-subset interpreter (Task/Choice/Pass/Wait/Parallel/Map/Succeed/Fail + Retry/Catch) + event history. |
| [storagegateway](./storagegateway.md) | Storage Gateway | Storage | UNSUPPORTED | No | **medium** — File gateway sync (local dir <-> S3/MinIO) + volume/tape records; no NFS/SMB/iSCSI serving. |
| [sts](./sts.md) | STS | Security and Identity | SIMULATED | No | **small** — Durable sessions + session-token auth in the API middleware + trust-policy evaluation. |
| [terminal](./terminal.md) | Cloud9-style Terminal | Developer Tools | PARTIAL | Yes (partial) | **small** — More allowlisted inspection commands + session transcripts + workdir file editor. |
| [terraform](./terraform.md) | Terraform | Developer Tools | PARTIAL | No | **medium** — Service console (runs/state/variables) over the existing runner API; execution stays Docker-gated. |
| [terraform-learning](./terraform-learning.md) | Terraform Learning | Terraform Learning | PARTIAL | Yes | **medium** — Module registry + policy checker + drift UI + multi-file editor on the existing learning engine. |
| [transfer](./transfer.md) | Transfer Family | Migration and Transfer | UNSUPPORTED | Yes | **large** — Pure-Go SFTP server (portable path) + server/user model over MinIO home directories; SFTP only in v1. |
| [vpcpeering](./vpcpeering.md) | VPC Peering | Networking | SIMULATED | Yes | **small** — Engine CreateVpcPeering RPC for real cross-namespace routing; lab-state entries stay the lite path. |
| [xray](./xray.md) | X-Ray | Developer Tools | UNSUPPORTED | No | **medium** — In-process tracer (API middleware + Lambda/ECS runners) + trace store + computed service map. |

## Lite set — not in this backup (built in lite, v3)

These 61 services are being built in lite by other workstreams. They are listed here only so a reader can see the full registry split; do not create backup files for them.

| Service id | Name |
|---|---|
| `apigateway` | API Gateway |
| `artifact` | Artifact |
| `auditmanager` | Audit Manager |
| `aurora` | Aurora |
| `autoscaling` | EC2 Auto Scaling |
| `backup` | Backup |
| `budgets` | Budgets |
| `cloudfront` | CloudFront |
| `cloudtrail` | CloudTrail |
| `computeoptimizer` | Compute Optimizer |
| `config` | Config |
| `controltower` | Control Tower |
| `costexplorer` | Cost Explorer |
| `datasync` | DataSync |
| `directconnect` | Direct Connect |
| `dms` | DMS |
| `ebs` | EBS |
| `ec2` | EC2 |
| `ecs` | ECS |
| `efs` | EFS |
| `elasticache` | ElastiCache |
| `elb` | Elastic Load Balancing |
| `eventbridge` | EventBridge |
| `fargate` | Fargate |
| `firehose` | Kinesis Data Firehose |
| `flowlogs` | VPC Flow Logs |
| `fsx` | FSx |
| `globalaccelerator` | Global Accelerator |
| `guardduty` | GuardDuty |
| `iam` | IAM |
| `identitycenter` | IAM Identity Center |
| `igw` | Internet Gateway |
| `inspector` | Inspector |
| `kinesis` | Kinesis |
| `kms` | KMS |
| `lambda` | Lambda |
| `macie` | Macie |
| `mgn` | Application Migration |
| `nacl` | Network ACLs |
| `nat` | NAT Gateway |
| `networkfirewall` | Network Firewall |
| `organizations` | Organizations |
| `ram` | Resource Access Manager |
| `rds` | RDS |
| `route53` | Route 53 |
| `routetable` | Route Tables |
| `s3` | S3 |
| `savingsplans` | Savings Plans |
| `secretsmanager` | Secrets Manager |
| `securityhub` | Security Hub |
| `sg` | Security Groups |
| `shield` | Shield |
| `sns` | SNS |
| `sqs` | SQS |
| `subnet` | Subnets |
| `transitgateway` | Transit Gateway |
| `trustedadvisor` | Trusted Advisor |
| `vpc` | VPC |
| `vpcendpoints` | VPC Endpoints |
| `vpn` | VPN / Direct Connect |
| `waf` | WAF |

## Sources

- `backend/internal/httpapi/services.go` — the registry (109 services at generation time)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- `frontend/src/App.tsx` and `frontend/src/pages/*` — which backup services already have a dedicated console
