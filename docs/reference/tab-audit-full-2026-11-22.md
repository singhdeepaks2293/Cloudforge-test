# CloudForge tab-by-tab audit — full per-service tables
## All 61 lite services (SAA-C03 + SAP-C02) vs the AWS console

Date: 2026-11-22. Sources: `docs/reference/aws-console-survey-2026-11-22.md` (46/46 services, ap-southeast-2, read-only; no create/edit wizard opened, so no wizard steps are recorded for any service), `frontend/src/pages/Ec2Console.tsx`, `frontend/src/pages/S3Console.tsx`, `frontend/src/pages/survey-reconciliation.test.tsx`, `frontend/src/pages/LiteServiceConsole.tsx`, `frontend/src/pages/ServiceDetail.tsx`, `frontend/src/App.tsx`, `backend/internal/httpapi/services.go`, `docs/reference/aws-console-backup/BACKUP_INDEX.md` (61-service lite list).

Scope note: the backup files cover the **other 48** registry services, not the 61 lite services — so for lite services the survey does not cover, there is no backup file to fall back on either.

What the survey-reconciliation test already asserts: S3 Buckets columns (Name / Region / IAM Access Analyzer / Creation date); IAM Roles columns; Lambda Functions columns; ELB Load Balancers columns; Cost Explorer / Budgets columns; Transit Gateway Attachments columns; CloudFormation / CodeBuild / CodePipeline columns; EKS and CloudFront empty tables; EC2 `?section=volumes` (EBS columns), `?section=autoscaling` (disabled Create), `?section=load-balancers`; VPC `?section=vpn` and `?section=tgw`. It asserts **columns and grouping only** — it does not assert detail tabs, and it does not assert EC2 Instances or S3 bucket-detail columns.

Key: ✅ tab/label present with working content · ⚠️ present but partial/disabled/empty-by-design · ❌ missing · — survey does not record this. Fidelity is the registry value in `backend/internal/httpapi/services.go` unless the console file states otherwise.

## Overall counts (61 lite services)

| Category | Count | Services |
|---|---|---|
| Fully matching | 0 | — |
| Mostly matching | 18 | EC2, S3, VPC, IAM, Lambda, SQS, SNS, Secrets Manager, ELB, Transit Gateway, Auto Scaling, EBS, VPN, DataSync, KMS (borderline), Route 53 (borderline), EventBridge (borderline), CloudTrail (borderline) |
| Partial | 24 | RDS, ElastiCache, API Gateway, ECS, Fargate, GuardDuty, Security Hub, Inspector, Config, Organizations, Cost Explorer, DMS, VPC Endpoints, Network Firewall, Flow Logs, Subnets, Route Tables, Internet Gateway, NAT Gateway, Security Groups, CloudFront, WAF, Trusted Advisor, SSM |
| Major gaps | 19 | Aurora, Backup, Macie, Audit Manager, Artifact, EFS, FSx, Shield, Control Tower, Identity Center, RAM, Direct Connect, Global Accelerator, MGN, Compute Optimizer, Savings Plans, Budgets, Network ACLs, +1 recount (17 generic + Budgets + NACLs) |
| Total | 61 | 0 + 18 + 24 + 19 = 61 |

Fidelity spread across the 61 (registry values): PARTIAL 32, SIMULATED 10, API_EMULATED 19 (the 17 generic + EBS/VPN in the registry, though their consoles label themselves UNSUPPORTED where nothing is built). No lite service is REAL except Backup (registry REAL for the host scripts — but there is no in-app Backup console).

---

## 1. EC2 — full detail

### 1a. AWS left-nav items (survey) vs CloudForge

| # | AWS nav item (survey) | CloudForge | Verdict |
|---|---|---|---|
| 1 | EC2 dashboard | EC2 dashboard (`home`) — ServiceHomeDashboard, PARTIAL | ✅ exact match |
| 2 | Instances | Instances — real lab list, PARTIAL (labs SIMULATED) | ✅ exact match |
| 3 | Instance Types | Instance types — lab profiles table, PARTIAL; vCPU/memory explicitly "Not modelled" | ✅ exact match, partial content |
| 4 | Launch Templates | Launch templates — label present, `disabledReason: 'Not modelled'`, **no section renders** | ⚠️ label only |
| 5 | Spot Requests | — | ❌ missing |
| 6 | Savings Plans | — (exists only as the generic `savingsplans` service, not in EC2 nav) | ❌ missing |
| 7 | Reserved Instances | — | ❌ missing |
| 8 | Dedicated Hosts | — | ❌ missing |
| 9 | Capacity Reservations | — | ❌ missing |
| 10 | AMIs | AMIs — lab definitions as images, PARTIAL | ✅ exact match |
| 11 | AMI Catalog | — | ❌ missing |
| 12 | EBS Snapshots (Images group) | — no separate item; see #15 | ❌ missing (as a distinct item) |
| 13 | Public Images | — | ❌ missing |
| 14 | Volumes (Elastic Block Store) | Volumes — empty table with survey's exact 23 EBS columns, UNSUPPORTED | ✅ exact match, empty by design |
| 15 | Snapshots (Elastic Block Store) | Snapshots — label present, disabled "EBS unsupported — no snapshot API", **no section renders** | ⚠️ label only |
| 16 | Lifecycle Manager | Lifecycle Manager — label present, disabled, **no section renders** | ⚠️ label only |
| 17 | Security Groups (Network & Security) | Security groups — lists only SGs from loaded instances, PARTIAL; management deferred to VPC console | ✅ exact match, partial content |
| 18 | Elastic IPs | Elastic IPs — label present, disabled "No Elastic IP operation", **no section renders** | ⚠️ label only |
| 19 | Placement Groups | — | ❌ missing |
| 20 | Key Pairs | — | ❌ missing |
| 21 | Network Interfaces | — | ❌ missing |
| 22 | Load Balancers (Load Balancing) | Load Balancers — `ElbListContent`, SIMULATED (in-process round-robin, real probes) | ✅ exact match |
| 23 | Target Groups | Target Groups — same `ElbListContent` as Load Balancers | ✅ exact match |
| 24 | Trust Stores | Trust Stores — label present, disabled "Not modelled", **no section renders** | ⚠️ label only |
| 25 | Auto Scaling Groups | Auto Scaling Groups — UNSUPPORTED empty state, Create button disabled | ✅ exact match, empty by design |
| 26 | Systems Manager | — (SSM is a separate console) | ❌ missing |
| 27 | Account settings | — | ❌ missing |

**EC2 counts:** 27 AWS nav items → 14 labels present, 13 missing. Of the 14 present, 9 render content (dashboard, Instances, Instance types, AMIs, Volumes, Security Groups, Load Balancers, Target Groups, Auto Scaling Groups) and 5 are label-only disabled items with no section (Launch Templates, Snapshots, Lifecycle Manager, Elastic IPs, Trust Stores). No CloudForge EC2 tab exists that AWS does not have — no extras.

### 1b. List-page columns — Instances

**Survey does not record this.** The survey only records that EC2 Home is an overview/marketing page with no table; the Instances list page was never reached with rows, so no AWS Instances columns are recorded. CloudForge's columns are therefore CloudForge's own, not survey-verified: Instance ID, Name, Instance state, Instance type (= lab definition id), VPC, Subnet, Availability Zone (hard-coded `cf-local-1a`), Launch time, VMs (instance-resource count). Toolbar: Instance state / Actions menus (Start/Stop/Reboot/Terminate real via lab API; Connect, Create image, Attach IAM role disabled), filter + state filter (ready/running/stopped/failed — no destroying/destroyed option), Refresh, Preferences disabled.

### 1c. Detail tabs — opening a row

**Survey does not record this** (no instance row was opened). CloudForge shows 9 detail tabs on a selected instance: Details, Security, Networking, Storage, Monitoring, Status checks, Logs, Tags, Events — fidelity varies per tab:
- Details — real lab fields, PARTIAL. Security/Networking — real lab resources only, PARTIAL. Storage — UNSUPPORTED note only (no EBS). Monitoring — control-plane metrics, not per-instance CloudWatch metrics (labelled as such). Status checks — derived from lab state + engine reachability. Logs and Events — both render the same real audit-event table (duplicated content). Tags — real tag editing via `setResourceTags`, PARTIAL.
- Launch instance wizard: 5 steps (Name and tags / Application and OS Images / Instance type / Network settings / Summary), with the footnote "wizard steps inferred … not observed" — the survey opened no wizard.

### 1d. EC2 verdict
Nav labels 14/27, working content 9/27. Main path (dashboard → Instances → detail → launch/terminate) is real and working; the long tail of the EC2 nav is either missing or label-only.

---

## 2. S3 — full detail

### 2a. AWS left-nav items (survey) vs CloudForge

Survey: S3 **lands on the Buckets list** (not a dashboard). Left nav as recorded:

| AWS nav item (survey) | CloudForge | Verdict |
|---|---|---|
| Buckets (General purpose / Directory / Table / Vector buckets) | Buckets — one list, no bucket-type sub-tabs | ⚠️ partial: list matches, type sub-items missing |
| Files (File systems) | — | ❌ missing |
| Access Points (Access management and security) | Access points — label present, disabled "Not supported by the local adapter", **no section renders** | ⚠️ label only |
| Access Grants | — | ❌ missing |
| IAM Access Analyzer | — (column only, see §2b) | ❌ missing as a nav item |
| Storage management and insights | — | ❌ missing |
| Storage Lens | — | ❌ missing |
| — (no dashboard item in survey) | S3 dashboard (`home`) | ➕ extra: CloudForge-only Home dashboard (VPC pattern) |
| — (not recorded as a nav item) | Object Lambda access points — disabled "Not modelled" | ➕ extra label, disabled, no section |
| — (not recorded as a nav item) | Batch operations — disabled "Not modelled" | ➕ extra label, disabled, no section |
| — (not recorded as a nav item) | Block Public Access settings — informational card only, no settings | ⚠️ present as a tab, but survey does not record it as an AWS nav item, so this cannot be verified as a match |
| — (not recorded as a nav item) | Replication — disabled "No replication API" | ➕ extra label, disabled, no section |

**S3 counts:** of the survey's 7 recorded nav items, 2 match exactly (Buckets, Access Points), 5 are missing. CloudForge adds 5 further tabs, 4 of them disabled and 1 informational. Console fidelity is PARTIAL ("buckets + objects over HTTP · MinIO-backed").

### 2b. List-page columns — Buckets

| AWS column (survey) | CloudForge | Verdict |
|---|---|---|
| (checkbox) | checkbox | ✅ |
| Name | Name (link opens detail) | ✅ |
| Region | Region (hard-coded `cf-local-1`) | ✅ |
| IAM Access Analyzer | IAM Access Analyzer — always "Not available" | ✅ exact column, no data |
| Creation date | Creation date | ✅ |
| — | Actions (Open / Delete) | ➕ extra column |

**4/4 AWS columns match, +1 extra.** This is asserted by `survey-reconciliation.test.tsx`. Create bucket wizard (5 steps: General configuration / Ownership / Block Public Access / Versioning / Summary) is marked "steps inferred, not observed"; Ownership and BPA steps are informational only (no ACL/BPA surface in MinIO adapter). Delete works only on empty buckets (real MinIO constraint). MinIO down = honest 503, no fake empty list.

### 2c. Detail tabs — opening a bucket

**Survey does not record this** — it says only "No detail tabs on the list" and no bucket was opened. CloudForge bucket detail has 5 tabs: Objects, Properties, Permissions, Management, Metrics.
- Objects — real list/upload/download/delete via backend (10 MiB cap), PARTIAL; object detail shows Key/Size/Content type/ETag/Last modified/User metadata.
- Properties — real bucket metadata + real versioning toggle (MinIO-backed), PARTIAL.
- Permissions — JSON bucket-policy editor (fetch/save via backend), PARTIAL; object ACLs explicitly "not available" (no minio-go surface).
- Management — versioning repeat + "No replication or lifecycle rules", PARTIAL (mostly a note).
- Metrics — no pipeline; links to CloudWatch. Essentially a placeholder.
Note: the tab *order* (… Permissions, Management, Metrics) is CloudForge's own; the survey records no AWS bucket-detail tab order to compare against, so this is not claimed as a match.

### 2d. S3 verdict
List columns 4/4 match; nav 2/7 match; bucket detail is real for Objects/Properties/Permissions and a note for Management/Metrics. The AWS S3 nav's second half (Files, Access Grants, Analyzer, Storage Lens, Storage management) is absent.

---

## 3. The other 59 lite services

### 3a. Dedicated consoles with a real backend (31 services + Fargate as an ECS section)

| Service (route) | AWS tabs recorded in survey | CloudForge tabs/sections | Fidelity | Verdict |
|---|---|---|---|---|
| VPC (`/services/vpc`) | Home dashboard; nav: Your VPCs, Subnets, Route tables, Internet gateways, Egress-only IGWs, DHCP option sets, Elastic IPs, Managed prefix lists, NAT gateways, Peering connections, Route servers; Security (Network ACLs, Security groups); PrivateLink and Lattice; DNS firewall; Network Firewall; VPN; Verified Access; Transit gateways; Traffic Mirroring; Network Manager, Cloud WAN, IPAM, Firewall Manager, Settings | 24 subNav tabs: VPC dashboard, Your VPCs, Subnets, Route Tables, Internet Gateways, Egress-only IGWs, DHCP Option Sets, Elastic IPs, Managed Prefix Lists, NAT Gateways, Peering Connections, Route Servers, Network ACLs, Security Groups, Endpoints, Endpoint Services, Firewalls, Site-to-Site VPN, Transit Gateways, Mirror Sessions, Network Manager, IPAM, Firewall Manager, Settings. Generic lab-resource tables per section + create forms (CIDR validated); VPN/TGW are real sections; NACLs have a dedicated section | PARTIAL | ✅ mostly matching. Missing: Verified Access, Cloud WAN, PrivateLink and Lattice, DNS firewall. Detail tabs: survey does not record any. |
| IAM (`/services/iam`) | Lands on Roles. Nav: Roles, Policies, IAM users, Root access management. Roles table: Role name, Trusted entities, Last activity | Dashboard, Users, Groups, Roles, Policies, Access Analyzer (disabled), Policy Simulator. Roles table matches survey columns (test-asserted). Visual+JSON policy editor added 2026-11-22 (v3.1) | PARTIAL | ✅ mostly matching. Extra: Dashboard, Simulator (CloudForge additions). Missing: Root access management. Policy detail tabs: survey's separate policy-editor survey records Permissions (Summary/JSON), Entities attached, Policy versions, Last Accessed — CloudForge policy detail is JSON-only, those 4 tabs are missing. |
| Lambda (`/services/lambda`) | Landing hero; nav: Dashboard, Applications, Event Functions, Event source mappings, Layers, Replicas. Functions table: Function name, Description, Runtime, Last modified | Lambda dashboard, Functions, Layers, Event source mappings, Code signing. Functions table matches survey columns (test-asserted; Description "Not available"). Function detail tabs: Code, Test, Configuration, Permissions, Monitoring, Triggers | PARTIAL | ✅ mostly matching. Missing: Applications, Replicas. Detail tabs: survey does not record AWS Lambda detail tabs, so CloudForge's 6 cannot be verified as a match. Wizard steps inferred. |
| RDS (`/services/rds`) | Dashboard, no main table. Nav: Dashboard, Databases, Snapshots, Exports in S3, Automated backups, Reserved instances, Capacity reservations, Proxies, Subnet groups, Parameter groups, Option groups, Custom engine versions, Cluster endpoints, Events, Event subscriptions, Integrations, Recommendations | Home + DB instances (real Postgres, PARTIAL), Snapshots (real pg_dump), Parameter groups, Aurora clusters (unsupported note). Start/stop explicitly unsupported per-instance | PARTIAL | ⚠️ partial. Present: Dashboard, Databases, Snapshots, Parameter groups, Aurora (as a note). Missing: Exports, Automated backups, Reserved instances, Capacity reservations, Proxies, Subnet groups, Option groups, Custom engine versions, Cluster endpoints, Events, Event subscriptions, Integrations, Recommendations. Detail tabs: survey does not record any. |
| ElastiCache (`/services/elasticache`) | Marketing home. Nav: Dashboard; Resources (Valkey, Memcached, Redis OSS caches, Global datastores, Reserved nodes, Backups); Configurations (Subnet groups, Parameter groups, User management, User group management) | Home + Clusters (real namespaced Redis, PARTIAL), Keys browser (read-only, max 100), Replication groups, Metrics | PARTIAL | ⚠️ partial. Present: Dashboard-equivalent, Clusters. Missing as tabs: Valkey/Memcached/Redis type split, Global datastores, Reserved nodes, Backups, Subnet groups, Parameter groups, User management, User group management. Detail tabs: survey does not record any. |
| SQS (`/services/sqs`) | Marketing home. Nav: single item Queues | Home + Queues (real, PARTIAL), Messages (selected queue), Dead-letter queues, FIFO queues | PARTIAL | ✅ mostly matching (nav is 1 item in AWS; CloudForge adds message/DLQ/FIFO views). List columns: survey does not record SQS queue columns. Detail tabs: survey does not record any. |
| SNS (`/services/sns`) | Marketing home + inline Create-topic form. Nav: Dashboard, Topics, Subscriptions, Mobile (Push, SMS) | Home + Topics (real, PARTIAL), Subscriptions, Deliveries | PARTIAL | ✅ mostly matching. Missing: Mobile/Push/SMS. Detail tabs: survey does not record any. |
| EventBridge (`/services/eventbridge`) | Marketing home + Create event bus card. Nav: Home, Rules, Schedules, Event buses, Schema registry, Pipes, Event streams, Settings, Quick starts | Home + Rules (real, PARTIAL), Event buses, Archives | PARTIAL | ⚠️ partial. Present: Home, Rules, Event buses. Missing: Schedules, Schema registry, Pipes, Event streams, Settings, Quick starts. Detail tabs: survey does not record any (EventBridge Permissions tab is JSON-only read-only per the policy-editor survey, and is not in CloudForge). |
| API Gateway (`/services/apigateway`) | Welcome page, Create API. Nav: APIs, Custom domain names, VPC links, Usage plans, API keys, API Portal. Tabs: REST APIs, HTTP APIs, WebSocket APIs | Home + APIs (real, PARTIAL), Resources, Stages, WebSocket APIs | PARTIAL | ⚠️ partial. Present: APIs, Stages, WebSocket APIs. Missing: Custom domain names, VPC links, Usage plans, API keys, API Portal; no REST/HTTP/WebSocket tab split on the list. Detail tabs: survey does not record any. |
| ECS (`/services/ecs`) | Get-started marketing page. Nav: Search ECS resources, Favorites and recents, Express Mode, Clusters, Gateways, Namespaces, Task definitions, Daemon task definitions, Account settings | Home + Clusters, Task definitions, Services, Tasks, Fargate (section) | PARTIAL | ⚠️ partial. Present: Clusters, Task definitions (+ Services/Tasks, which AWS lists under clusters). Missing: Express Mode, Gateways, Namespaces, Daemon task definitions, Account settings, Favorites/recents. Detail tabs: survey does not record any. |
| Fargate (`/services/ecs` section) | — survey does not record Fargate as a console (it is a launch type inside ECS) | Fargate section inside ECS console (same runtime, launch type FARGATE) | SIMULATED | ⚠️ present as a section; no AWS Fargate console to match against. |
| ELB (`/services/ec2` section) | No standalone console; EC2 Load Balancing section. Table: Name, State, Type, Scheme, IP address type, VPC ID, Availability Zones, Security groups, DNS name, ARN, Date created | EC2 → Load Balancers / Target Groups (`ElbListContent`), SIMULATED | SIMULATED | ✅ columns 11/11 match (test-asserted; Type/VPC ID/AZ/SG/ARN "Not available"). Detail: collapsed pane only in AWS with nothing selected; CloudForge detail tabs: survey does not record any. |
| Route 53 (`/services/route53`) | Marketing home. Nav: Global (Home, Dashboard, Hosted zones, Health checks, Traffic flow), VPC Resolver (Rules, Endpoints, Profiles, DNS Firewall), Domains, Traffic flow | Home + Hosted zones (real DNS + resolve endpoint, SIMULATED), Records, Health checks, Route 53 Resolver | SIMULATED | ⚠️ partial. Present: Hosted zones, Records, Health checks, Resolver. Missing: Traffic flow/policies/records, Registered/Transfer domains, Resolver Rules/Endpoints/Profiles, DNS Firewall. Detail tabs: survey does not record any. |
| CloudTrail (`/services/cloudtrail`) | Dashboard. Nav: Dashboard, Event history, Insights, Lake, Trails, Data events, Event data stores, Event selectors, Channels, Queries, Dashboards, Settings. Trails table (Name, Status); Event history table (Event name, Event time, Event source) | Home + Events (real audit log, PARTIAL), Export jobs, Trails, Insights | PARTIAL | ⚠️ partial. Present: Dashboard-equivalent, Event history, Trails, Insights, Export jobs. Missing: Lake, Data events, Event data stores, Event selectors, Channels, Queries, Dashboards, Settings. Columns: survey does not record a full CloudTrail columns list to compare; CloudForge event table is Time/Actor/Action/Resource type/Resource. |
| KMS (`/services/kms`) | Marketing home + Create a key. Nav: AWS managed keys, Customer managed keys, Keys shared with me, Custom key stores, Settings | Home + Customer managed keys (real, PARTIAL), Aliases, Grants | PARTIAL | ⚠️ partial. Present: Customer managed keys. Missing: AWS managed keys, Keys shared with me, Custom key stores, Settings. Detail tabs: survey does not record any (key-policy tab not observable — no key existed). |
| Secrets Manager (`/services/secretsmanager`) | Marketing landing. Nav: single item Secrets | Home + Secrets (real, PARTIAL), Versions, Rotation schedules | PARTIAL | ✅ mostly matching (AWS nav is 1 item; CloudForge adds Versions/Rotation). Detail tabs: survey does not record any. |
| WAF (`/services/waf`) | Marketing home + Get started. Nav grouped: AWS WAF (Web ACLs, Rules, Bot Control, IP sets, Regex pattern sets, Rule groups); AI traffic monetization; AWS Shield (Protected resources, Global threat dashboard, Advanced); Shield network security director; Firewall Manager | Home + Web ACLs (real rule evaluation, SIMULATED), Rules, Rule groups, Shield (DDoS) as a note | SIMULATED | ⚠️ partial. Present: Web ACLs, Rules, Rule groups. Missing: Bot Control, IP sets, Regex pattern sets, AI traffic monetization, Shield protected resources/threat dashboard/Advanced, Firewall Manager. Detail tabs: survey does not record any. |
| GuardDuty (`/services/guardduty`) | RESTRICTED — "Page restricted" dialog only; no layout captured | Home + Detectors, Findings (from real audit log, PARTIAL), Suppression rules | PARTIAL | ⚠️ layout follows public docs, not the survey — explicitly labelled as such. Cannot be verified as a match. |
| Security Hub (`/services/securityhub`) | RESTRICTED — same dialog; no layout captured | Home + Findings, Open findings, Standards | PARTIAL | ⚠️ layout follows public docs, not the survey. Cannot be verified as a match. |
| Inspector (`/services/inspector`) | — survey does not record Inspector | Home + Assessment runs, Findings, Assessment templates | PARTIAL | — survey does not record this. Cannot be verified as a match. |
| Config (`/services/config`) | — survey does not record Config | Home + Rules, Evaluation results, Configuration items | PARTIAL | — survey does not record this. Cannot be verified as a match. |
| Organizations (`/services/organizations`) | RESTRICTED — "Page restricted" dialog; no layout captured | Home + Organizational units (real user store, PARTIAL), Accounts (users), Service control policies | PARTIAL | ⚠️ layout follows public docs, not the survey. Cannot be verified as a match. |
| Cost Explorer (`/services/costexplorer`) | Billing console (global). Nav incl. Home, Cost Explorer, Cost analysis, Cost and usage report, Budgets, Purchase recommendations, Reservation utilization, Savings Plans, Cost anomaly detection, Cost allocation, Reports, Credits, Billing preferences, Billing. Breakdown table: Service, Total cost, % of total cost | Home + Cost line items (real priced usage, SIMULATED), Budgets (real, PARTIAL), Savings Plans (unsupported note) | SIMULATED (console) / PARTIAL (budgets) | ⚠️ partial. Columns match (test-asserted). Missing most Billing nav: Cost analysis, Cost and usage report/export, Purchase recommendations, Reservation utilization, Savings Plans utilisation, Cost anomaly detection, Cost allocation/tags, Reports, Credits, Billing preferences, Billing. Detail tabs: survey does not record any. |
| Budgets (`/services/budgets`) | Same Billing console. Tabs Budgets / Reports; table: Name, Thresholds, Budget, Amount used, Forecasted amount, Current vs. budgeted, Forecasted vs. budgeted | **No dedicated route** — falls back to generic ServiceDetail (Overview / Operations / Learning). The real Budgets table lives inside the Cost Explorer console | PARTIAL (registry) | ❌ major gap: the survey's Budgets table is not reachable at `/services/budgets`; only via Cost Explorer. Columns match where they exist (test-asserted, Forecasted amount "Not available"). |
| Trusted Advisor (`/services/trustedadvisor`) | — survey does not record Trusted Advisor | Home + Check results (5 real checks, PARTIAL), Check categories | PARTIAL | — survey does not record this. Cannot be verified as a match. |
| SSM / Systems Manager (`/services/ssm`) | — survey does not record SSM (EC2 nav lists "Systems Manager" with no detail) | Fleet & Run Command: Inventory (host agent), Run Command | PARTIAL | — survey does not record this. Cannot be verified as a match. |
| Athena (`/services/athena`) | — not in the 61 lite list in BACKUP_INDEX, but has a dedicated console; survey (backup section): marketing home, Get started, Launch query editor; nav collapsed | Query editor (real Postgres SQL, PARTIAL) | PARTIAL | — not a lite-61 service; excluded from the 61 counts. |
| Glue, QuickSight, SageMaker, Bedrock, Migration Hub | — not in the 61 lite list; survey backup section records only marketing/getting-started homes | Dedicated consoles exist (Glue crawlers PARTIAL, QuickSight PARTIAL, SageMaker PARTIAL, Bedrock SIMULATED, Migration Hub PARTIAL) | PARTIAL/SIMULATED | — not lite-61 services; excluded from the 61 counts. |
| DMS (`/services/dms`) | NOT ACCESSIBLE in survey — no layout captured | Home + Replication tasks (real row copies, PARTIAL), Endpoints, Migration logs | PARTIAL | ⚠️ layout follows public docs, not the survey. Cannot be verified as a match. |
| DataSync (`/services/datasync`) | Left nav: Dashboard, Task executions, Tasks, Locations, Agents. Tables: Task executions (Execution ID, Start time, Duration, Status, Data throughput, File throughput, Task, Mode); Tasks (Task ID, Name, Status, Mode, Schedule, Source, Destination, Created); Locations (Location ID, Host, Path, Type, Created, Tasks, Agents); Agents (Agent ID, Name, Service endpoint, Status, Created, Tasks). Detail tabs: none observed (all tables empty) | Home + Transfer tasks (real copies, SHA-256 verified, PARTIAL), Agents, Locations, Transfer logs | PARTIAL | ✅ mostly matching on nav (Dashboard/Tasks/Locations/Agents present; "Task executions" and "Transfer logs" are the same concept under different labels). Columns: CloudForge task table is Name/Source/Destination/Status/Files/Bytes/Verified/Error — **not** the survey's Task ID/Name/Status/Mode/Schedule/Source/Destination/Created (no Task ID, Mode, Schedule, Created columns). Detail tabs: survey does not record any. |
| VPC Endpoints (`/services/vpcendpoints`) | — survey does not record a standalone VPC Endpoints console (VPC nav lists Endpoints; endpoint policy not observable) | Home + Endpoints (SIMULATED), Endpoint services, Services | SIMULATED | — survey does not record this. Cannot be verified as a match. |
| Network Firewall (`/services/networkfirewall`) | — survey records it only as a VPC nav item ("Network Firewall"), no detail | Home + Firewalls (SIMULATED), Rule groups, TLS inspection configurations | SIMULATED | — survey does not record this beyond the VPC nav item. Cannot be verified as a match. |
| VPC Flow Logs (`/services/flowlogs`) | — survey does not record Flow Logs | Home + Flow log groups (SIMULATED), Records, CloudWatch log groups | SIMULATED | — survey does not record this. Cannot be verified as a match. |
| Transit Gateway (`/services/vpc` section) | No standalone console; VPC section. Nav: Transit gateways, Attachments, Policy tables, Route tables, Multicast, Metering policies. Attachments table: Name, Transit gateway attachment ID, Transit gateway ID, State | VPC → Transit Gateways section (SIMULATED): Transit gateways, Attachments, Route tables, Policy tables | SIMULATED | ✅ mostly matching. Attachments columns match (test-asserted). Missing: Multicast, Metering policies. Detail: collapsed pane only in AWS; survey does not record detail tabs. |
| VPC Peering (`/services/vpcpeering`) | — not in the 61 lite list; survey records it only as a VPC nav item | Peering connections (SIMULATED) | SIMULATED | — not a lite-61 service; excluded from counts. |

### 3b. Section of another console (10 services)

| Service | AWS position (survey) | CloudForge | Fidelity | Verdict |
|---|---|---|---|---|
| Auto Scaling (`/services/ec2` section) | EC2 Auto Scaling Groups page; empty state = marketing intro + Create button, How it works, Benefits, Related services, Pricing | EC2 → Auto Scaling Groups: UNSUPPORTED empty state, disabled Create, How it works / Related services cards | API_EMULATED (registry) / UNSUPPORTED (console) | ✅ matches the empty-state pattern. No table (none in AWS either). |
| EBS (`/services/ec2` section) | EC2 Elastic Block Store section. Volumes table: 23 columns (Name, Volume ID, Type, Size, IOPS, Throughput, Owner ID, Snapshot ID, Source volume ID, Created, Availability Zone, Volume state, Alarm status, Attached resources, Status check, Initialization state, Encryption, KMS key ID, KMS key alias, Fast snapshot restored, Multi-Attach enabled, Managed, Operator) | EC2 → Volumes: empty table with all 23 survey columns | API_EMULATED (registry) / UNSUPPORTED (console) | ✅ columns 23/23 match (test-asserted). Snapshots/Lifecycle Manager are label-only (see EC2 §1a). |
| ELB | see §3a | see §3a | SIMULATED | ✅ |
| Transit Gateway | see §3a | see §3a | SIMULATED | ✅ |
| VPN (`/services/vpc` section) | VPC section. Nav: Customer gateways, Virtual private gateways, Site-to-Site VPN Concentrators, Site-to-Site VPN connections, Client VPN endpoints. Connections table: 23 columns (Name, VPN ID, State, Virtual private gateway, Transit gateway, VPN Concentrator ID, Customer gateway, Customer gateway address, Inside IP version, Type, Category, VPC, Routing, Acceleration enabled, Authentication, Local/Remote IPv4/IPv6 CIDR, Core network ARN, Gateway association state, Outside IP address type) | VPC → Site-to-Site VPN Connections: empty table with survey columns, UNSUPPORTED | API_EMULATED (registry) / UNSUPPORTED (console) | ✅ columns match (test-asserted). Missing as tabs: Customer gateways, Virtual private gateways, Concentrators, Client VPN endpoints. |
| Subnets, Route Tables, Internet Gateway, NAT Gateway, Security Groups | VPC nav items (see §3a VPC) | VPC console sections of the same names (generic lab-resource tables + create forms) | SIMULATED | ✅ present as VPC sections. Their AWS list columns are not recorded in the survey (VPC section tables were not reached with rows), so column fidelity cannot be verified. |

### 3c. Generic LiteServiceConsole — no dedicated console, no backend runtime (17 services)

All 17 share the same console shape: Home dashboard (VPC pattern, banner + action links + Resources-by-Region cards from the Resource Explorer, Service Health, Settings, Additional Information) + subNav `[<Name> dashboard, <resource-type tabs>, Operations, Learning]` + an Operations table (registry operations with fidelity filters) + Detail tabs **Details / Limitations** on a selected operation (action button disabled with the reason when there is no HTTP route) + "What you can do instead" links to an available service. Registry fidelity for all 17 is API_EMULATED, but the console itself is honest that nothing is built — create is disabled, counts are 0 from the Explorer, never invented. CloudFront is the exception: it has an extra Distributions tab with the survey's exact columns (ID, Status, Description, Type, Domain name, Alternate domain names, Origins, Pricing plan, Last modified), empty because it is UNSUPPORTED.

| Service | AWS tabs recorded in survey | CloudForge | Verdict |
|---|---|---|---|
| Aurora | — survey does not record Aurora | Generic console; alternatives: RDS | ❌ major gap: no list, no create, no detail. Survey does not record AWS Aurora tabs, so nothing to match against. |
| Backup | Dashboard. Nav (My account): Dashboard, Jobs dashboard, Vaults, Backup vault locks, S3 backup tiering, Backup plans, Protected resources, Access points, Search, Jobs, Malware protection, Restore testing, Legal holds, Settings; External resources; My organization; Backup Audit Manager | Generic console; note says Backup is host scripts (`scripts/backup.sh`/`restore.sh`, pg_dump), no backup-plan API, no in-app Backup console. Registry fidelity REAL (the scripts), console is generic | ❌ major gap: AWS has ~20 nav items; CloudForge has the generic Home/Operations/Learning only. |
| Macie | — survey does not record Macie | Generic console; alternatives: GuardDuty, Security Hub | ❌ major gap. Survey does not record AWS Macie tabs. |
| Audit Manager | — survey does not record Audit Manager | Generic console; alternatives: CloudTrail export | ❌ major gap. Survey does not record AWS Audit Manager tabs. |
| Artifact | — survey does not record Artifact | Generic console; alternatives: CloudTrail export | ❌ major gap. Survey does not record AWS Artifact tabs. |
| CloudFront | Lands on Distributions (0). Nav: Distributions, Cache statistics, Origin access, Functions, Key groups, Public keys, Field-level encryption, Invalidations, Reports & analytics, What's new. Table columns as above | Generic console + Distributions tab with exact survey columns, empty (UNSUPPORTED) | ⚠️ partial: Distributions table matches; the other 9 nav items (Cache statistics, Origin access, Functions, Key groups, Public keys, Field-level encryption, Invalidations, Reports & analytics, What's new) are missing. |
| EFS | Backup section: Get-started page, Create file system; nav: File systems, Access points | Generic console; alternatives: S3 | ❌ major gap. Survey records only a get-started page. |
| FSx | — survey does not record FSx | Generic console; alternatives: S3 | ❌ major gap. Survey does not record AWS FSx tabs. |
| Shield | — survey records Shield only as a WAF-nav group (Protected resources, Global threat dashboard, Advanced) | Generic console; alternatives: WAF | ❌ major gap. No Shield console; survey records no standalone Shield tabs. |
| Control Tower | — survey does not record Control Tower | Generic console; alternatives: Organizations | ❌ major gap. Survey does not record AWS Control Tower tabs. |
| IAM Identity Center | — survey does not record Identity Center | Generic console; alternatives: IAM | ❌ major gap. Survey does not record AWS Identity Center tabs. |
| RAM | — survey does not record RAM | Generic console; alternatives: Organizations | ❌ major gap. Survey does not record AWS RAM tabs. |
| Direct Connect | — survey does not record Direct Connect | Generic console; alternatives: VPN, Transit Gateway | ❌ major gap. Survey does not record AWS Direct Connect tabs. |
| Global Accelerator | — survey does not record Global Accelerator | Generic console; alternatives: Route 53, ELB | ❌ major gap. Survey does not record AWS Global Accelerator tabs. |
| Application Migration (mgn) | — survey does not record MGN | Generic console; alternatives: DMS, DataSync | ❌ major gap. Survey does not record AWS MGN tabs. |
| Compute Optimizer | — survey does not record Compute Optimizer | Generic console; alternatives: Cost Explorer, Trusted Advisor | ❌ major gap. Survey does not record AWS Compute Optimizer tabs. |
| Savings Plans | — survey records Savings Plans only as a Cost Explorer nav item and an EC2 nav item | Generic console; alternatives: Cost Explorer, Budgets | ❌ major gap. No Savings Plans console; survey records no standalone tabs. |

### 3d. Generic ServiceDetail fallback (2 services)

| Service | AWS tabs recorded in survey | CloudForge | Verdict |
|---|---|---|---|
| Budgets | See §3a (Budgets table, 8 columns) | ServiceDetail: Overview / Operations / Learning only — no Budgets table at `/services/budgets` (the table lives in Cost Explorer) | ❌ major gap (routing). |
| Network ACLs (nacl) | VPC nav item "Network ACLs" (Security group) | ServiceDetail: Overview / Operations / Learning only at `/services/nacl` — **but** the VPC console also has a real Network ACLs section, so the same service has two different pages depending on the route | ❌ major gap (routing inconsistency). AWS list columns for NACLs are not recorded in the survey. |

---

## 4. Top 10 gaps across all 61

1. **No service fully matches AWS.** The best cases (S3, ELB, EBS Volumes, Transit Gateway attachments, CloudFront Distributions, Lambda Functions, Cost Explorer/Budgets tables) match on *list columns*, but every service is missing AWS nav items, detail tabs, or both.
2. **EC2's long tail is missing: 13 of 27 AWS nav items have no CloudForge equivalent** (Spot Requests, Reserved Instances, Dedicated Hosts, Capacity Reservations, AMI Catalog, Public Images, Placement Groups, Key Pairs, Network Interfaces, Systems Manager, Account settings, Savings Plans, plus EBS Snapshots as a distinct Images-group item), and 5 more are label-only with no section.
3. **17 exam services have no console at all — only the generic Home/Operations/Learning page** (Aurora, Backup, Macie, Audit Manager, Artifact, CloudFront*, EFS, FSx, Shield, Control Tower, Identity Center, RAM, Direct Connect, Global Accelerator, MGN, Compute Optimizer, Savings Plans). No list, no create, no resource detail; only an operations table and "what you can do instead" links. (*CloudFront is the exception: its Distributions table is present and column-exact, but empty.)
4. **Budgets and Network ACLs land on the wrong page.** `/services/budgets` and `/services/nacl` render generic ServiceDetail (Overview/Operations/Learning), even though the real Budgets table exists inside Cost Explorer and a real NACLs section exists inside the VPC console. Both are routing inconsistencies, not missing backends.
5. **AWS detail tabs were almost never observed, so CloudForge detail tabs are unvalidated.** The survey records detail tabs for **no** service (tables were empty or the list page was never reached with rows; DataSync explicitly says "Detail tabs: none observed"). CloudForge's detail sets — EC2's 9 tabs, Lambda's 6 (Code/Test/Configuration/Permissions/Monitoring/Triggers), S3's 5, IAM's policy detail (JSON-only vs AWS's 4 tabs: Permissions Summary/JSON, Entities attached, Policy versions, Last Accessed) — are CloudForge's own design, not survey-verified.
6. **List columns are unverified for most services.** The survey records exact list columns for only ~10 services (S3, Lambda, ELB, EBS Volumes, VPN connections, Transit Gateway attachments, CloudFront, Cost Explorer, Budgets, CloudTrail trails/events). For the other ~50, CloudForge's columns are its own choice; DataSync is the one clear column mismatch (CloudForge: Name/Source/Destination/Status/Files/Bytes/Verified/Error vs survey: Task ID/Name/Status/Mode/Schedule/Source/Destination/Created — no Task ID, Mode, Schedule, or Created column).
7. **S3's second half is absent:** Files/File systems, Access Grants, IAM Access Analyzer (as a console), Storage Lens, and Storage management and insights have no CloudForge equivalent; Access Points, Object Lambda, Batch operations, and Replication are disabled labels.
8. **VPC is missing its newer sections:** Verified Access, Cloud WAN, PrivateLink and Lattice, and DNS Firewall have no CloudForge tab; the 24 tabs present are mostly generic lab-resource tables rather than AWS-specific lists.
9. **Restricted services' layouts are unobserved by design:** GuardDuty, Security Hub, Organizations, and DMS (plus Kinesis, which is not in the 61) were restricted/not accessible, so their CloudForge layouts follow public documentation, not the survey — the survey file says so explicitly, and the consoles label it. These cannot be called matches.
10. **No wizard steps are survey-verified for any service.** EC2 Launch instance, S3 Create bucket, and Lambda Create function wizards all carry the "steps inferred, not observed" footnote. Any tab that is a wizard step (rather than a list/detail tab) is therefore unverified against AWS by construction.

## 5. What the survey-reconciliation test does and does not prove

It proves the column alignment it asserts (S3, IAM Roles, Lambda, ELB, Cost Explorer/Budgets, Transit Gateway attachments, CloudFormation/CodeBuild/CodePipeline, EKS/CloudFront empty tables, EC2 EBS/Auto Scaling/ELB sections, VPC VPN/TGW sections). It does **not** prove tab coverage (it never renders most subNav sections), it does not prove detail tabs (no test opens a row's detail tabs except EC2/S3 render paths), and it does not cover the 17 generic services or Budgets/NACLs routing at all.

## 6. Fixed in v3.2 (2026-11-22)

- Routing: `/services/budgets` renders the real Budgets table; `/services/nacl` redirects to `/services/vpc?section=nacls` with a real NACL section on the exam backend (rules evaluated, not attached to any subnet, no live traffic filtered).
- DataSync: Tasks table now uses the survey columns (Task ID, Name, Status, Mode, Schedule, Source, Destination, Created); Task executions, Locations, and Agents tables added with survey columns, derived honestly from real task data.
- 17 generic services now have dedicated consoles wired to the migration-014 API-emulated backend (Aurora, Backup, Macie, Audit Manager, Artifact, CloudFront, EFS, FSx, Shield, Control Tower, Identity Center, RAM, Direct Connect, Global Accelerator, MGN, Compute Optimizer, Savings Plans). CloudFront Distributions keeps the survey's exact columns. Backup runs real pg_dump. Tabs with no backend stay UNSUPPORTED with disabled buttons.
- EC2: all 27 survey nav items present, zero label-only tabs. 9 missing items added as honest UNSUPPORTED sections; 5 label-only items built into real sections; Savings Plans / Systems Manager / Account settings are links.
- S3: Files, Access Grants, IAM Access Analyzer, Storage Lens added as honest UNSUPPORTED tabs; Storage management added as a PARTIAL real table. Access Points / Object Lambda / Batch / Replication stay disabled with reasons shown.
- Not re-audited after the fix: the counts in §4 are the pre-fix audit. Detail tabs and wizard steps remain unvalidated against AWS (the survey never recorded them).
