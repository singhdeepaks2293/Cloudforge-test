# AWS console survey — v4 (fresh, 2026-11-22, 21:57–22:20 IST)
Region ap-southeast-2 (IAM/Route 53/CloudFront/WAF/Organizations/Billing global, us-east-1). Account 867109003096 "Ready Set Build", user deepak singh. READ-ONLY: nothing created/changed; only wizards opened were Create VPC, Launch instances, Lambda Create Event Function — all left via Cancel, nothing submitted.

Account is nearly empty — only resources: default VPC vpc-0612fead9ab13370f, 3 default subnets, 1 route table, 1 IGW, 1 DHCP set, 1 NACL, 1 SG, 12 IAM roles. So detail tabs could only be observed for VPC and IAM roles/policies; every other service's detail tabs are recorded as NOT OBSERVABLE (empty tables), never invented.

## VPC
Dashboard counts: VPCs 1, Subnets 3, Route Tables 1, IGWs 1, Egress-only IGWs 0, DHCP 1, Elastic IPs 0, Endpoints 0, Endpoint Services 0, NAT 0, Peering 0, NACLs 1, SGs 1, Customer gateways 0, VPGs 0, VPN connections 0, Running Instances 0.
Left nav in order: Virtual private cloud: Your VPCs, Subnets, Route tables, Internet gateways, Egress-only internet gateways, DHCP option sets, Elastic IPs, Managed prefix lists, NAT gateways, Peering connections, Route servers; Security: Network ACLs, Security groups; PrivateLink and Lattice: Getting started, Endpoints, Endpoint services, Service networks, Lattice services, Resource configurations, Resource gateways, Target groups, Domain verifications; DNS firewall: Rule groups, Domain lists; Network Firewall: Firewalls, Firewall policies, Network Firewall rule groups, TLS inspection configurations, Network Firewall resource groups, VPC endpoint associations, Container associations; VPN: Customer gateways, Virtual private gateways, Site-to-Site VPN Concentrators, Site-to-Site VPN connections, Client VPN endpoints; Verified Access: Verified Access instances, Verified Access trust providers, Verified Access groups, Verified Access endpoints; Transit gateways: Transit gateways, Transit gateway attachments, Transit gateway policy tables, Transit gateway route tables, Transit gateway multicast, Metering policies; Traffic Mirroring: Mirror sessions, Mirror targets, Mirror filters; then Networking flexible cost allocation, Network Manager, Cloud WAN, VPC IP Address Manager, Firewall Manager, Settings.
Your VPCs columns (exact order): checkbox, Name, VPC ID, ARN, State, Encryption control ID, Encryption control mode, Block Public Access, Automated internet firewall coverage, IPv4 CIDR, IPv6 CIDR, DHCP option set, Main route table, Main network ACL, Tenancy, Default VPC, Owner ID. Tabs above table: VPCs, VPC encryption controls.
VPC detail tabs (vpc-0612fead9ab13370f, OBSERVED): Resource map, CIDRs, Flow logs, Tags, Related resources, Integrations.
Subnets columns: Name, Subnet ID, ARN, State, VPC, Block Public Access, Automated internet firewall connectivity type, IPv4 CIDR, IPv6 CIDR, IPv6 CIDR association ID, Available IPv4 addresses, Availability Zone, Network border group, Route table, Internet access, Network ACL, Default subnet, Auto-assign public IPv4, Auto-assign customer-owned IPv4, Customer-owned IPv4 pool, Auto-assign IPv6, Owner ID.
Create VPC wizard (observed, Cancel): Resources to create VPC only / VPC and more; Name tag; IPv4 CIDR manual or IPAM, /16–/28; IPv6: none / IPAM / Amazon-provided / owned; Tenancy Default; VPC encryption control None / Monitor / Enforce ($); Tags Add tag. VPC and more defaults: name "project", 10.0.0.0/16, 2 AZs, 2 public + 2 private subnets, NAT None, S3 Gateway endpoint, DNS hostnames+resolution checked; preview 4 subnets, 3 route tables, 2 connections (igw, vpce-s3).

## EC2
Left nav: Dashboard, AWS Global View, Events; Instances: Instances, Instance Types, Launch Templates, Spot Requests, Savings Plans, Reserved Instances, Dedicated Hosts, Capacity Reservations, UltraServers (New), Capacity Manager (New); Images: AMIs, AMI Catalog; Elastic Block Store: Volumes, Snapshots, Lifecycle Manager; Network & Security: Security Groups, Elastic IPs, Placement Groups, Key Pairs, Network Interfaces; Load Balancing: Load Balancers, Target Groups, Trust Stores; Auto Scaling: Auto Scaling Groups; Settings.
Instances columns (0 instances): Name, Instance ID, Instance state, Instance type, Status check, Availability Zone, Public IPv4 DNS, Public IPv4 address, Elastic IP, IPv6 IPs, Monitoring, Security group name, Key name, Launch time, Platform details, Managed, Operator. Actions menu: Create image, Create launch template, Manage image, Security (Modify IAM role, Modify Identity Center identity, Get Windows password, Modify instance metadata, Manage instance protection), Networking (Associate/Disassociate Elastic IP, Attach network interface, Edit instance IP addresses, Change source/destination check, Edit ENIs), Instance settings (View details, Edit instance type, Stop/Start/Reboot/Hibernate, Terminate, Change shutdown behavior, Edit status check alarm, Modify capacity reservation), Monitoring, Image and templates, Add/edit tags, Delete.
Instance DETAIL TABS NOT OBSERVABLE — no instances.
Launch instances wizard (observed, Cancel): Name and tags; Application and OS Images (Quick Start / My AMIs / Marketplace / Community / Shared); Instance type (Compare, Get advice); Key pair (Select / Create new); Network settings (VPC, Subnet, Auto-assign public IP, Firewall/SGs with Create security group, protocol rules Type/Protocol/Port/Source/Description); Configure storage (Volume 1 AMI Root 8 GiB gp3, Add new volume); Advanced details; Summary (1 instance, Amazon Linux 2023, t3.micro, 1 volume 8 GiB, Free tier eligible).

## S3
Buckets columns: Name, AWS Region, Creation date. Tabs: General purpose buckets / Directory buckets. Side nav: Buckets (General purpose, Directory, Table, Vector buckets); Files (File systems); Access management and security (Access Points, Access Points for FSx, Access Grants, IAM Access Analyzer); Storage management and insights (Storage Lens, Batch Operations); Account and organization settings; AWS Marketplace for S3. Storage Lens: 0 dashboards; tabs Storage Lens dashboards / Storage Lens groups; columns Dashboard name, Status. Access Grants: no instance — getting-started only; prerequisite: create an IAM Identity Center instance in this Region first.
BUCKET DETAIL TABS NOT OBSERVABLE — no buckets.

## IAM (global)
Side nav (new simplified console, only 4): Roles, Policies, IAM users, Root access management.
Roles (12) columns: Role name, Trusted entities, Last activity. Role detail (AccountFullAccessRole) tabs (OBSERVED): Permissions, Trust relationships, Tags, Last Accessed, Revoke sessions. Permissions tab table: Policy name, Type, Attached entities.
Policies (1622, 82 pages) columns: Policy name, Type, Used as, Description. Policy detail (AdministratorAccess) tabs (OBSERVED): Permissions, Entities attached, Policy versions (1), Last Accessed. Permissions tab: Summary / JSON toggle; "Allow (484 of 484 services)"; table Service, Access level, Resource, Request condition.
IAM users (0) columns: User name, Path, Last activity, MFA, Access key ID, Active key age, Access key last used, ARN, Creation time.

## RDS
Left nav: Dashboard, Databases, CloudWatch Database Insights, Snapshots, Exports in Amazon S3, Automated backups, Reserved instances, Capacity reservations (New), Proxies, Subnet groups, Parameter groups, Option groups, Custom engine versions, Zero-ETL integrations, Events, Event subscriptions, Recommendations (0), Certificate update. Databases table columns NOT OBSERVABLE (empty table, no headers). DB DETAIL TABS NOT OBSERVABLE.

## Lambda
Side nav: Lambda dashboard, Event Functions, Web Functions, Layers, Applications, Test events, Event source mappings, Code signing configurations, Settings. Event Functions columns: Function name, Description, Package type, Runtime, Type, Last modified. Create Event Function wizard (observed, Cancel): Author from scratch / Use a blueprint / Container image; Function name (1–64 chars); Runtime Node.js 26.x default; toggles Durable execution, EC2 capacity provider; Additional settings (collapsed): architecture, execution role, HTTP(S) endpoints, tenant isolation, VPC, code signing, KMS, tags. FUNCTION DETAIL TABS NOT OBSERVABLE.

## Route 53 (global)
Left nav: Dashboard, Hosted zones, Health checks, Profiles, Global Resolver (Global resolvers, Shared DNS views), VPC Resolver (VPCs, Inbound endpoints, Outbound endpoints, Rules, Query logging, Outposts), Domains (Registered domains, Requests), IP-based routing (CIDR collections), Traffic flow (Traffic policies, Policy records), DNS Firewall, Application Recovery Controller. Dashboard showed "couldn't update the page — unknown error". Hosted zones columns truncated in view — NOT OBSERVABLE. HOSTED ZONE DETAIL TABS NOT OBSERVABLE.

## DynamoDB
Tables columns: Name, Status, Partition key, Sort key, Indexes, Replication Regions, Deletion protection, Favorite, Read capacity mode, Write capacity mode, Total size, Table class. Left nav: Dashboard, Tables, Explore items, PartiQL editor, Backups, Exports to S3, Imports from S3, Bulk put items (New), Integrations, Reserved capacity, Settings; Caches, DAX. TABLE DETAIL TABS NOT OBSERVABLE.

## ElastiCache
Left nav: Dashboard, Resources (Valkey caches, Memcached caches, Redis OSS caches, Global datastores, Reserved nodes, Backups), Configurations (Subnet groups, Parameter groups, User management, User group management), Migrations, Events, Service updates, ElastiCache cluster client, Documentation, Amazon MemoryDB. Valkey caches columns: Cache name, Status, Description, Engine version, Configuration, Created date.

## SQS
Queues columns: Name, Type, Created, Messages available, Messages in flight, Encryption, Content-based deduplication. Left nav: Queues, Queues with errors, DLQs. QUEUE DETAIL TABS NOT OBSERVABLE.

## SNS
Topics columns: Name, Type, ARN. Left nav: Dashboard, Topics, Subscriptions, Mobile (Push notifications, Text messaging (SMS)). TOPIC DETAIL TABS NOT OBSERVABLE.

## EventBridge
Rules redirect to Event buses. Event buses columns: Name, Type, Status, ARN, Schema discovery (default bus: Name, Type, ARN, Schema discovery). Left nav: Dashboard, Event buses (New), Pipes, Scheduler (Schedules, Schedule groups, Scheduled rules (legacy)), Integration (Partner event sources, API destinations, Connections), Schema registry (Schemas), Classic event routing (Global endpoints), Developer resources (Learn, Sandbox, Quick starts). RULE DETAIL TABS NOT OBSERVABLE.

## API Gateway
APIs columns: Name, Description, ID, Protocol, Endpoint type, Created, Security policy, API status. Left nav: APIs, Custom domain names, Domain name access associations, VPC links, AgentCore targets, Usage plans, API keys, Client certificates, Settings; API Portal: API inventory, Products, Documentation portals, Organization settings. API DETAIL TABS NOT OBSERVABLE.

## ECS
Clusters columns: Cluster, Services, Tasks, Container instances, CloudWatch monitoring, Capacity provider strategy. Left nav: Search ECS resources, Favorites and recents, Express Mode, Clusters, Gateways (New), Namespaces, Task definitions, Daemon task definitions, Account settings. CLUSTER DETAIL TABS NOT OBSERVABLE.

## ELB (via EC2)
Load Balancers columns: Name, State, Type, Scheme, IP address type, VPC ID, Availability Zones, Security groups, DNS name, ARN, Date created. Actions (disabled, nothing selected): Edit IP address type, Edit subnets, Manage instances, Edit health check settings, Manage listeners, Edit security groups, Edit load balancer attributes, Manage tags, Delete load balancer. LB DETAIL TABS NOT OBSERVABLE.

## CloudTrail
Left nav: Dashboard, Data event coverage (New), Event history, Insights, Trails; Lake: Dashboards, Query, Event data stores, Integrations; Settings. Trails table: Name, Status. Event history: Event name, Event time, Event source. Banner: "CloudTrail Lake is no longer open to new customers starting May 31, 2026". TRAIL DETAIL TABS NOT OBSERVABLE — no trail exists.

## KMS
Customer managed keys columns: Aliases, Key ID, Status, Key type, Key spec, Key usage. Left nav: AWS managed keys, Customer managed keys, Keys shared with me (New), Custom key stores (AWS CloudHSM key stores, External key stores). KEY DETAIL TABS NOT OBSERVABLE.

## Secrets Manager
Secrets columns: Secret name, Description, Last retrieved (UTC), Created on (UTC). Left nav: only Secrets. SECRET DETAIL TABS NOT OBSERVABLE.

## WAF (new wafv2-pro console)
Left nav: AWS WAF (Protection packs (web ACLs), IP sets, Regex pattern sets, Rule groups, Application integration, Add-on protections, Security analytics (New)), AI traffic monetization (Analysis, Revenue), AWS Shield (Getting started, Overview, Protected resources, Events, Global threat dashboard), AWS Shield network security director (Getting started, Dashboard), AWS Firewall Manager (Getting started, Security policies, Resource sets, Application lists, Protocol lists, Settings), Switch to old WAF console. Protection packs columns: Name, Rules, Resources, Dashboard, AI traffic analysis (New), Sampled requests, Logging & metrics, Scope, ARN, ID. WEB ACL DETAIL TABS NOT OBSERVABLE.

## Restricted (paid-plan / advanced-features dialogs, nothing clicked)
GuardDuty: "Service GuardDuty unavailable — Upgrade to paid plan". Security Hub: same pattern. Organizations: "not available to your project… upgrade… activate advanced features" — nav behind dialog: Dashboard, Invitations, Policy management. FSx: "Service FSx unavailable — Upgrade to paid plan" — nav behind dialog: File systems, Volumes, Snapshots, S3 Access Points, File Caches, Backups, ONTAP (Storage virtual machines, Cluster peers, SVM peers, SnapMirror relationships, SnapMirror policies, Snapshot policies), FSx on Service Quotas, Settings. DMS: "Service Database Migration Service unavailable — Upgrade to paid plan" — dashboard behind: Active 0, Error 0, Failed 0, Load complete 0; nav: Convert or move to managed, Migrate or replicate, Monitor; Certificates, Subnet groups, Support lifecycle policy. Direct Connect: "not available to your project… upgrade… activate advanced features" — nav behind dialog: Connections, Resiliency groups, Virtual interfaces, LAGs, Direct Connect gateways, Virtual private gateways, Transit gateways, AWS Interconnect – multicloud, AWS Interconnect – last mile.

## Cost Explorer / Budgets (Billing, global)
Cost Explorer: Total cost $0.00, date range 2026-05-01 to 2026-10-31, Granularity Monthly, Group by Service; breakdown columns Total, May–Oct 2026. Report parameters: Time, Group by Dimension (Service), Filters (Service, Linked account, Region, Instance type, Usage type, Usage type group, Resource, Cost category, Tag), Advanced (Unblended costs, Show forecasted values checked). Budgets columns: Name, Thresholds, Budget, Amount used, Forecasted amount, Current vs. budgeted, Forecasted vs. budgeted. Left nav: Cost Management (Cost Explorer, Free Tier, Budgets).

## CloudFront (global)
Distributions columns: ID, Status, Description, Type, Domain name (standard), Alternate domain names, Origins, Pricing plan, Last modified. Left nav: Distributions, Policies, Functions, Static IPs, VPC origins; SaaS (Multi-tenant distributions, Distribution tenants); Telemetry (Monitoring, Alarms, Logs); Reports & analytics (Cache statistics, Popular objects, Top referrers, Usage, Viewers); Security (Origin access, Trust stores, Field-level encryption); Key management (Public keys, Key groups); Savings Bundle (Overview, Inventory, Purchase); Resource sharing, Settings. DISTRIBUTION DETAIL TABS NOT OBSERVABLE.

## EFS
File systems columns: Name, File system ID, Encrypted, Total size, Size in Standard, Size in IA, Size in Archive, Provisioned Throughput (MiB/s), File system state, Creation time, Availability Zone, Replication overwrite protection. Left nav: File systems, Access points. FILE SYSTEM DETAIL TABS NOT OBSERVABLE.

## DataSync
Tasks columns: Task ID, Name, Status, Mode, Schedule, Source, Destination, Created. Left nav: Dashboard (New), Task executions, Tasks, Locations, Agents. TASK DETAIL TABS NOT OBSERVABLE — no tasks.

## Transit Gateway (in VPC)
Transit gateways columns: Name, Transit gateway ID, Owner ID, State. DETAIL TABS NOT OBSERVABLE.

## VPN (in VPC)
Site-to-Site VPN connections columns: Name, VPN ID, State, Virtual private gateway, Transit gateway, VPN Concentrator ID, Customer gateway, Customer gateway address, Inside IP version, Type, Category, VPC, Routing, Acceleration enabled, Authentication, Local IPv4 network CIDR, Remote IPv4 network CIDR, Local IPv6 network CIDR, Remote IPv6 network CIDR, Core network ARN, Core network attachment ARN, Gateway association state, Outside IP address type. DETAIL TABS NOT OBSERVABLE.

## Other SAP services reached
CloudWatch: Overview "no alarms, metrics or default dashboard"; left nav Ingestion, Dashboards, Alarms, AI Operations, GenAI Observability, Application Signals (APM), Infrastructure Monitoring, Omni (New), Observability Workshops; collapsed Logs, Metrics, Network Monitoring, Setup.
AWS Config: NOT SET UP — "Set up AWS Config", Get started / 1-click setup (not clicked); no nav items rendered.
Systems Manager: left nav Home, Explore managed instances, Node Management (Fleet Manager, Run Command, Inventory, Session Manager, Patch Manager, State Manager, Compliance, Distributor), Operations Management (OpsCenter, Incident Manager, Explorer, OpsItems, Trusted Advisor, CloudWatch dashboard/alarms/Logs, EventBridge, CloudTrail), Application Management (Application Manager, AppConfig, Parameter Store, Change Manager, Change Calendar, Maintenance Windows, Automation, Quick Setup, Documents, Hybrid Activations, Shared Resources, Insights, Settings, Get Started).
AWS Backup: left nav Home, Dashboard, My backups, Protected resources, Backup plans, Backup vaults, Copy jobs, Restore jobs, Reports, Legal holds, Settings; Central management (Organizations, Cross-account, Cross-Region management, Cross-Region backup); Additional (AWS Organizations, Backup Audit Manager, Resource assignment, Documentation, Getting started).
ECR: Private repositories columns Repository name, URI, Created at, Tag immutability, Encryption type. REPOSITORY DETAIL TABS NOT OBSERVABLE.
Step Functions: State machines columns Name, Type, Creation date, Status. STATE MACHINE DETAIL TABS NOT OBSERVABLE.
CloudFormation: Stacks columns Stack name, Status, Created time, Description. Left nav: Stacks, Stack refactors (New), StackSets, Exports, IaC generator, Template studio, Hooks overview, Invocation summary, Hooks, Registry (Public extensions, Activated extensions, Publisher), Spotlight. STACK DETAIL TABS NOT OBSERVABLE.

## Limits
Services NOT reached in this pass (no findings claimed): Kinesis, MSK, Redshift, Neptune, EKS, CodePipeline/CodeBuild, Glue, Athena, EMR, SES, Global Accelerator, Route 53 Resolver endpoints, Cloud WAN, Network Firewall, Verified Access, PrivateLink endpoints, Lattice.
