# AWS Console Reference Survey — 2026-11-22

Read-only survey of the real AWS Management Console, region ap-southeast-2,
from the user's own Chrome tab. Nothing was created, edited, deleted, launched,
terminated, submitted, or applied. No account IDs, credentials, or private
resource IDs/names are recorded here. No create/edit wizard was opened, so
wizard steps are not described — only the entry button is noted where seen.

Purpose: layout reference for CloudForge service consoles, in CloudForge
branding only. No AWS logo, exact brand colours, trademarks, or copied assets.
Never claim CloudForge is AWS.

## Common pattern

Every console shares the same top header (Console Home, Services menu, global
Search, CloudShell, Notifications, Help & support, account menu), a left
navigation panel, and a main content area. Many services land on a
marketing-style home page with no table; the table, when present, is on the
service's list page. Several services are global and load on the us-east-1
console host despite the ap-southeast-2 region parameter.

## Lite services (29 checked)

### VPC (checked earlier, 2026-11-22)
Home dashboard, not a list page. Left nav grouped: Virtual private cloud
(Your VPCs, Subnets, Route tables, Internet gateways, Egress-only internet
gateways, DHCP option sets, Elastic IPs, Managed prefix lists, NAT gateways,
Peering connections, Route servers); Security (Network ACLs, Security groups);
PrivateLink and Lattice; DNS firewall; Network Firewall; VPN; Verified Access;
Transit gateways; Traffic Mirroring; plus Network Manager, Cloud WAN, VPC IP
Address Manager, Firewall Manager, Settings. Main: "Resources by Region" —
two-column grid of resource cards (linked heading, region label, linked count,
Regions expander), Refresh Resources button; then Service Health, Settings,
Additional Information, Network Manager, Site-to-Site VPN, Firewall Manager.
No table or detail tabs on Home.

### EC2
Home is an overview/marketing page, no table. Left nav: EC2 dashboard,
Instances, Instance Types, Launch Templates, Spot Requests, Savings Plans,
Reserved Instances, Dedicated Hosts, Capacity Reservations, Images (AMIs,
AMI Catalog, EBS Snapshots, Public Images), Elastic Block Store (Volumes,
Snapshots, Lifecycle Manager), Network & Security (Security Groups, Elastic
IPs, Placement Groups, Key Pairs, Network Interfaces), Load Balancing (Load
Balancers, Target Groups, Trust Stores), Auto Scaling (Auto Scaling Groups),
Systems Manager, Account settings. Main hero with Launch instance button and
five cards (Virtual machines, High performance computing, Machine learning
and AI, Serverless compute, Storage and data).

### S3
Lands on the Buckets list. Left nav: Buckets (General purpose, Directory,
Table, Vector buckets), Files (File systems), Access management and security
(Access Points, Access Grants, IAM Access Analyzer), Storage management and
insights, Storage Lens. Table "General purpose buckets": checkbox, Name,
Region, IAM Access Analyzer, Creation date. Controls: Create bucket, Delete,
search by name, pagination, Preferences gear. No detail tabs on the list.

### IAM
Lands on Roles (global, us-east-1 host). Left nav: Roles, Policies, IAM
users, Root access management. Main: Create role, Delete (disabled),
Refresh roles, search by role name or ARN. Table: checkbox, Role name,
Trusted entities, Last activity, pagination. No detail tabs on the list.

### RDS
Dashboard, no main resource table. Left nav: Dashboard, Databases, Snapshots,
Exports in Amazon S3, Automated backups, Reserved instances, Capacity
reservations, Proxies, Subnet groups, Parameter groups, Option groups, Custom
engine versions, Cluster endpoints, Events, Event subscriptions,
Integrations, Recommendations. Main: "Create with express" and "Create with
full configuration" cards, Restore from S3 card, Service overview cards,
Service health table (Current status, Details), Explore RDS section.

### Lambda
Landing page is a hero with language tabs and a code-editor panel; list view
is "Event Functions". Left nav: Dashboard, Applications, Event Functions,
Event source mappings, Layers, Replicas. Table: checkbox, Function name,
Description, Runtime, Last modified. Controls: search, Actions dropdown,
Create Event Function, pagination, Preferences gear.

### Elastic Load Balancing
No standalone console; it is the EC2 console's Load Balancing section.
Left nav: EC2 nav, Load Balancing group (Load Balancers, Target Groups,
Trust Stores). Table "Load balancers": checkbox, Name, State, Type, Scheme,
IP address type, VPC ID, Availability Zones, Security groups, DNS name, ARN,
Date created. Controls: Create load balancer, Options dropdown, filter
search, Actions, Refresh. Collapsed detail pane at bottom when nothing
selected; no detail tabs visible with nothing selected.

### Route 53
Home is a marketing page, no table. Left nav: Global (Home, Dashboard,
Hosted zones, Health checks, Traffic flow), VPC Resolver (Rules, Endpoints,
Profiles, DNS Firewall), Domains (Registered domains, Domain
registrations, Transfer domains), Traffic flow (Traffic policies, Policy
records).

### DynamoDB
Home is a marketing hero with Create table button, Benefits, Start building
links. Left nav collapsed on arrival. No table on home.

### ElastiCache
Home is a marketing page. Left nav: Dashboard; Resources (Valkey caches,
Memcached caches, Redis OSS caches, Global datastores, Reserved nodes,
Backups); Configurations (Subnet groups, Parameter groups, User management,
User group management). No table on home.

### SQS
Home is a marketing page. Left nav is a single item: Queues. No table
on home.

### SNS
Home is a marketing page with an inline "Create a topic" form (Topic name,
Next step). Left nav: Dashboard, Topics, Subscriptions, Mobile (Push
notifications, Text messaging SMS). No table on home.

### EventBridge
Home is a marketing page with a "Create event bus" card (radio: EventBridge
event bus, Pipe, Schedule, Schema registry). Left nav: Home, Rules,
Schedules, Event buses, Schema registry, Pipes, Event streams, Settings,
Quick starts, CloudWatch link. No table on home.

### API Gateway
Welcome page with Create API button. Left nav: APIs, Custom domain names,
VPC links, Usage plans, API keys, API Portal. Main tabs: REST APIs, HTTP
APIs, WebSocket APIs. No table on welcome page.

### ECS
Get-started marketing page. Left nav: Search ECS resources, Favorites and
recents, Express Mode, Clusters, Gateways, Namespaces, Task definitions,
Daemon task definitions, Account settings. No table on get-started page.

### CloudWatch
Overview dashboard. Long grouped left nav: Overview, Dashboards, Insights
and investigations (Application Signals, RUM, Synthetics Canaries, Internet
Monitor, Network Observability), Alarms, Logs (Log groups, Log Insights,
Contributor Insights, Live tail), Metrics, Events, X-Ray traces, Application
Insights, Network Monitor, Resource Health, Settings. Main: Select dashboard
dropdown, date-range selector, Filter by resource group, timezone, Refresh,
Actions, Cross service dashboard widgets. No table on Overview.

### CloudTrail
Dashboard. Left nav: Dashboard, Event history, Insights, Lake, Trails, Data
events, Event data stores, Event selectors, Channels, Queries, Dashboards,
Settings. Main: Create a trail, Create event data store, Start logging,
Welcome hero, Query results history, Trails table (Name, Status),
CloudTrail Insights, Event history table (Event name, Event time,
Event source), Pricing card.

### KMS
Home is a marketing page with Create a key button. Left nav: AWS managed
keys, Customer managed keys, Keys shared with me, Custom key stores,
Settings. No table on home.

### Secrets Manager
Landing marketing page with Store a new secret dropdown. Left nav is a
single item: Secrets. No table on landing page.

### WAF
Home is a marketing page with Get started card. Left nav grouped: AWS WAF
(Web ACLs, Rules, Bot Control, IP sets, Regex pattern sets, Rule groups);
AI traffic monetization; AWS Shield (Protected resources, Global threat
dashboard, Advanced); Shield network security director; Firewall Manager.
No table on home.

### GuardDuty — RESTRICTED
Not accessible in this environment: search card says access not supported,
console home shows a "Page restricted" dialog (paid plan). No layout
captured beyond the dialog. Use public console documentation as the
reference for its layout; do not claim the restricted state is the design.

### Security Hub — RESTRICTED
Same pattern as GuardDuty: search card not supported, console home shows
a "Page restricted" dialog (paid plan). No layout captured.

### Cost Explorer
Billing and Cost Management console (global, us-east-1 host). Left nav:
Home, Cost Explorer, Cost analysis, Cost and usage report, Cost and usage
report data export, Budgets, Purchase recommendations, Reservation
utilization, Savings Plans, Savings Plans utilization coverage, Cost anomaly
detection, Cost allocation, Cost allocation tags, Reports, Credits, Billing
preferences, Billing. Main: Save report as, Preferences, summary cards
(Total cost, Average monthly cost, Service count), Cost and usage chart
(Bar / Stacked bar / Line, Group by Service), Cost and usage breakdown
table (Service, Total cost, % of total cost), Report parameters panel.

### Budgets
Same Billing console. Main: Budgets / Reports tabs, Create budget button.
Table: checkbox, Name, Thresholds, Budget, Amount used, Forecasted amount,
Current vs. budgeted, Forecasted vs. budgeted. Search "Find a budget",
pagination, Preferences gear.

### Organizations — RESTRICTED
Search card says available with advanced features, no clickable console
link; console home shows "Page restricted" dialog (paid plan). No layout
captured.

### Transit Gateway
No standalone console; VPC console section. Left nav Transit gateways
group: Transit gateways, Transit gateway attachments, Transit gateway
policy tables, Transit gateway route tables, Transit gateway multicast,
Metering policies. Attachments table: checkbox, Name, Transit gateway
attachment ID, Transit gateway ID, State (more columns off-screen).
Collapsed detail pane at bottom; no detail tabs with nothing selected.

### VPN
VPC console section. Left nav VPN group: Customer gateways, Virtual
private gateways, Site-to-Site VPN Concentrators, Site-to-Site VPN
connections, Client VPN endpoints. VPN connections table: checkbox, Name,
VPN ID, State, Virtual private gateway, Transit gateway, VPN Concentrator
ID, Customer gateway, Customer gateway address, Inside IP version, Type,
Category, VPC, Routing, Acceleration enabled, Authentication, Local/ Remote
IPv4/IPv6 network CIDR, Core network ARN, Gateway association state,
Outside IP address type. Controls: Actions, Download configuration,
Create VPN connection, search, pagination, Preferences gear.

### CloudFront
Lands on Distributions (0). Left nav: Distributions, Cache statistics,
Origin access, Functions, Key groups, Public keys, Field-level encryption,
Invalidations, Reports & analytics, What's new. Table: checkbox, ID,
Status, Description, Type, Domain name, Alternate domain names, Origins,
Pricing plan, Last modified. Controls: Refresh, Enable/Disable/Delete
(disabled with nothing selected), Create distribution, search, Filter type,
pagination, Preferences gear.

### Auto Scaling
EC2 Auto Scaling groups page; no standalone console. Left nav is the EC2
nav, Auto Scaling group (Auto Scaling Groups). Empty state is a marketing
intro with Create Auto Scaling group button, How it works, Benefits,
Related services, Pricing. No table on the empty-state page. Note: a
separate "AWS Auto Scaling" service (Scaling plans) also appears in search.

### Backup
Dashboard. Left nav grouped: My account (Dashboard, Jobs dashboard,
Vaults, Backup vault locks, S3 backup tiering, Backup plans, Protected
resources, Access points, Search, Jobs, Malware protection, Restore
testing, Legal holds, Settings); External resources (Gateways, Hypervisors,
Virtual machines); My organization (Cross-account monitoring, Backup
policies); Backup Audit Manager (Frameworks, Reports). Main: Create backup
vault, Create on-demand backup, Backup jobs tile, Backup summary tile,
Protected resources, Restore testing results, Compliance status. No table
on dashboard.

## Backup reference services (16 checked, for later use)

### EKS
Home is a marketing page. Left nav: Dashboard, Clusters; Settings
(Dashboard settings, Console settings); EKS Anywhere; Related services.
Clusters list table: Cluster name, Status, Kubernetes version, Support
period, Upgrade policy, Created, Certificate authority, Provider, Actions.
Controls: Refresh, Actions, Delete (disabled), Create cluster, Filter
clusters, pagination, Preferences gear.

### Elastic Beanstalk
Welcome page with Create application button. Left nav only: Applications,
Environments, Change history. No table on welcome page.

### Batch
Dashboard. Left nav: Dashboard, Jobs, Job definitions, Job queues,
Environments, Consumable resources, Scheduling policies, Wizard; Console
settings. Main: Batch resources / Batch metrics tabs, Auto-refresh toggle,
Reset to default layout, Add widgets, Jobs overview widget, Job queue
overview table (Job queue, Total capacity utilization snapshot, Priority,
Submitted, Pending, Runnable, Starting, Running, Succeeded, Failed),
Compute environment overview table (Name, Type, Provisioning model,
Instance types, Status, State, Minimum/Desired/Maximum vCPUs).

### EBS
No standalone console; EC2 console Elastic Block Store section. Left nav:
Volumes, Snapshots, Lifecycle Manager. Volumes table: checkbox, Name,
Volume ID, Type, Size, IOPS, Throughput, Owner ID, Snapshot ID, Source
volume ID, Created, Availability Zone, Volume state, Alarm status,
Attached resources, Status check, Initialization state, Encryption, KMS
key ID, KMS key alias, Fast snapshot restored, Multi-Attach enabled,
Managed, Operator. Controls: Recycle Bin, Actions, Create volume, Saved
filter sets, search, pagination, Preferences gear.

### EFS
Get-started page with Create file system button. Left nav: File systems,
Access points, plus links to Backup, DataSync, Transfer, Documentation.
No table on get-started page.

### Redshift — RESTRICTED
Search card says available on a paid plan; landing loads behind a "Page
restricted" dialog. Marketing landing visible behind it (Try Redshift
Serverless, How it works, Benefits, Use cases, Related services, Create
cluster / Go to provisioned clusters). No left nav visible beyond an
Open navigation pane button.

### Cognito
Home is a marketing page with Get started buttons for User pools and
Identity pools. Left nav collapsed on arrival. No table on home.

### SES
Home is a marketing page with Get started button. Left nav collapsed on
arrival. No table on home.

### CloudFormation
Lands on Stacks (0). Left nav: Stacks, Stack refactors, StackSets,
Exports, IaC generator, Template studio, Hooks overview, Invocation
summary, Hooks, Registry (Public extensions, Activated extensions,
Publisher, Spotlight), Feedback. Table: checkbox, Stack name, Status,
Created time, Description. Controls: Refresh, Delete stack (disabled),
Update stack, Stack actions, Create stack, Filter status, View nested,
search by stack name, pagination, Preferences gear.

### CodeBuild
Build projects list. Left nav (Developer Tools): Source (CodeCommit),
Artifacts (CodeArtifact), Build (CodeBuild: Getting started, Build
projects, Build history, Report groups, Report history, Compute fleets,
Account metrics), Related integrations (Jenkins, GitHub Actions). Table:
checkbox, Name, Source provider, Repository, Latest build status,
Description, Last Modified. Controls: Refresh, Actions, Create trigger,
View details, Debug build, Start build, Create project, search,
pagination, Preferences gear.

### CodePipeline
Pipelines list. Left nav (Developer Tools): Pipeline (CodePipeline:
Pipelines, Pipeline executions, Settings), plus Source/Artifacts/Build/
Deploy groups. Table: checkbox, Name, Latest execution status, Latest
source revisions, Latest execution started, Most recent executions.
Controls: Refresh, View history, Release change, Delete pipeline
(disabled), Create pipeline, Filter results, pagination, Preferences gear.

### Athena
Home is a marketing page with a Get started choice (Query your data in
Athena console / in SageMaker Unified Studio) and Launch query editor
button. Left nav collapsed on arrival. No table on home.

### Glue
Getting Started page with Get started and Set up ETL jobs cards. Left
nav: Getting Started, Data Catalog (collapsed), Data Integration and ETL
(collapsed), Settings. No table on getting-started page.

### Kinesis — RESTRICTED
Search card says available on a paid plan; console home shows "Page
restricted" dialog. No layout captured.

### SageMaker
Lands on the SageMaker (DataZone) home: Get started button, four cards
(Unified Studio, AI & ML, Governance, Lakehouse), Resources links,
Contact us. Left nav collapsed on arrival. No table on home.

### DMS — NOT ACCESSIBLE
Search card says access not supported, not clickable. A direct attempt at
its console home path returned Page Not Found (that path was a guess, not
an observed link) and was not pursued. No layout captured. Use public
console documentation as the reference when building it later.

## DataSync — CHECKED 2026-11-22 (retry, fresh browser session)

DataSync loaded normally and was not restricted or unsupported.
Left nav, in order: Dashboard (with a "New" badge), Task executions,
Tasks, Locations, Agents. Panel headed "DataSync".

Landing page (#/getStarted): "Welcome to AWS DataSync", action buttons
Create task / Create location / Create agent, "Your DataSync tasks"
(No tasks found), three cards (Transfer data between storage systems,
Copy data between AWS Regions, Transfer data using object tags), How it
works (Create locations, Create tasks, Execute tasks), Additional
resources. No table on landing page.

Dashboard (#/dashboard): Create task button; Service overview tiles
(Tasks 0, Locations 0, Agents 0, Data throughput 0 B/s); Task executions
table; Total transferred by task table (Task, Data transferred, Files
transferred); Task execution status chart (No data available).

Task executions table: checkbox, Execution ID, Start time, Duration,
Status, Data throughput, File throughput, Task, Mode. Buttons Edit /
Stop (disabled with nothing selected), Find task executions search,
Preferences, pagination.

Tasks table: checkbox, Task ID, Name, Status, Mode, Schedule, Source,
Destination, Created. Buttons Actions (disabled), Create task.

Locations table: checkbox, Location ID, Host, Path, Type, Created,
Tasks, Agents. Buttons Delete (disabled), Create location.

Agents table: checkbox, Agent ID, Name, Service endpoint, Status,
Created, Tasks. Buttons Edit / Delete (disabled), Create agent; status
filter dropdown (Any status).

Detail tabs: none observed — all four tables were empty (0 rows), so no
row could be opened and no detail-tab set was visible. Do not infer
detail-tab names from this survey.

## Coverage

46 of 46 services checked (all 29 lite services, all 17 backup
services, counting restricted / not-accessible ones as checked with the
outcome above). Nothing remains unchecked.

## Survey applied (Workstream 1b, 2026-11-22)

Reconciliation pass against the consoles in `frontend/src/pages/*`
and `frontend/src/components/*`. No AWS re-check was done; every
change below is measured against this file only.

### Tables aligned to the survey's exact columns

- S3 Buckets: Name, Region, IAM Access Analyzer, Creation date
  (IAM Access Analyzer is "Not available" — no analyzer data exists).
  S3 now lands on the Buckets list.
- IAM Roles: Role name, Trusted entities (read from the stored trust
  policy), Last activity ("Not available" — no backend source). IAM
  now lands on Roles.
- Lambda Functions: Function name, Description ("Not available"),
  Runtime, Last modified.
- Load Balancers: Name, State, Type, Scheme, IP address type, VPC ID,
  Availability Zones, Security groups, DNS name, ARN, Date created
  (Type / IP address type / VPC ID / Availability Zones / Security
  groups / ARN are "Not available" — CloudForge models none of them).
- Cost Explorer breakdown: Service, Total cost, % of total cost
  (grouped from the real priced usage lines); Budgets: Name,
  Thresholds, Budget, Amount used, Forecasted amount ("Not
  available" — no forecasting), Current vs. budgeted, Forecasted vs.
  budgeted.
- Transit Gateway Attachments: Name, Transit gateway attachment ID,
  Transit gateway ID, State.
- CloudFront Distributions, EKS Clusters: survey columns, empty
  tables (both UNSUPPORTED).
- CloudFormation Stacks: Stack name, Status, Created time,
  Description ("Not available"). CodeBuild Projects: Name, Source
  provider, Repository, Latest build status, Description ("Not
  available"), Last Modified. CodePipeline Pipelines: Name, Latest
  execution status, Latest source revisions ("Not available"), Latest
  execution started, Most recent executions.

### Grouping (sections, not standalone consoles)

- ELB, Auto Scaling and EBS are now sections of the EC2 console
  (Load Balancing / Auto Scaling / Elastic Block Store groups).
  `/services/elb` → `/services/ec2?section=load-balancers`,
  `/services/autoscaling` → `/services/ec2?section=autoscaling`,
  `/services/ebs` → `/services/ec2?section=volumes`.
- Transit Gateway and VPN are now sections of the VPC console.
  `/services/transitgateway` → `/services/vpc?section=tgw`,
  `/services/vpn` → `/services/vpc?section=vpn`.
- The old standalone shells (ElbConsole, TransitGatewayConsole) are
  kept as thin wrappers around the extracted list content
  (ElbListContent, TransitGatewayListContent) for any other in-app
  link; the routes themselves redirect, so bookmarks land on the
  section. The VPC nav items that used to link out now switch
  sections in place.

### Restricted / unchecked / inferred — how each was handled

- GuardDuty, Security Hub, Organizations, Redshift, Kinesis, DMS:
  RESTRICTED / not accessible in the reference account, so no layout
  was captured. Their CloudForge consoles' layouts follow public
  console documentation, and each console's Home banner now says so
  explicitly (GuardDuty, Security Hub, Organizations, Kinesis, DMS).
  The restriction is never presented as the design. Redshift has no
  CloudForge console at all, so nothing was added for it.
- DataSync: NOT YET CHECKED — this survey was the last remaining
  service when the browser became unavailable, and no DataSync page
  was reached. The DataSync console's layout is from public console
  documentation and its Home banner now marks it UNVERIFIED against
  the observed console. No observed layout was invented.
- Wizards: no create/edit wizard was opened (read-only survey), so no
  wizard steps are recorded in this file. The EC2 Launch instance, S3
  Create bucket and Lambda Create function wizards keep CloudForge's
  own steps, each with a footnote: "wizard steps inferred from the
  console's action, not observed".

### Already matched — no change

VPC, EC2, RDS, Route 53, DynamoDB, ElastiCache, SQS, SNS,
EventBridge, API Gateway, ECS, CloudWatch, CloudTrail, KMS, Secrets
Manager, WAF, Backup, EFS, Cognito, SES, Athena, Glue, SageMaker:
Home dashboard with resource cards and no table on Home, list page
with the service's own columns — as surveyed. Batch has no CloudForge
console; its survey entry stays backup reference only.
