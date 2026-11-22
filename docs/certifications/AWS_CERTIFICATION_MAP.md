# AWS Certification Coverage Map

CloudForge now ships two exam-oriented tracks:

| Track | Exam | Level | Guided lab |
| --- | --- | --- | --- |
| `aws-saa` | AWS Certified Solutions Architect - Associate (SAA-C03) | Intermediate | `saa-ha-web` |
| `aws-sap` | AWS Certified Solutions Architect - Professional (SAP-C02) | Advanced | `sap-hybrid-network` |

The tracks teach the exam domains with real production incidents. This page
maps each domain to the services it needs and records what CloudForge can
actually demonstrate. Fidelity labels follow
[`docs/SERVICE_FIDELITY.md`](../SERVICE_FIDELITY.md): a simulated or partial
service is never presented as the real AWS service.

**v3 (2026-11-22):** every service below that was `Unsupported` is now
API-emulated in lite mode (real API endpoints, durable records, wired
into the registry), with a 3-lesson "Exam Services in Practice"
module in each track and two new 30-minute labs
(`saa-exam-services`, `sap-exam-services`). Backup runs are real
`pg_dump`. What is still missing is listed under Honest gaps.

## Coverage legend

| Label | Meaning |
| --- | --- |
| **Real** | The control plane stores real records and the operation has a working backend. |
| **Partial** | A working subset exists; documented gaps remain. |
| **Simulated** | The teaching model is real enough for the exercise, but no AWS runtime is involved. |
| **Unsupported** | Catalogued for exam vocabulary only; no console or runtime. |

## SAA-C03 domains

### 1. Design resilient architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| VPC, subnets, route tables, IGW, NAT | Simulated | `saa-ha-web`, `vpc-fundamentals` |
| Security groups, load balancing | Simulated | `saa-ha-web`, break-and-fix scenarios |
| EC2 and instance health | Partial | `ec2-basics`, `saa-ha-web` |
| RDS / Aurora | Partial (RDS), API-emulated (Aurora) | `databases` track; Aurora: `saa-storage-edge` lesson, `saa-exam-services` lab |
| SQS, SNS, EventBridge | Partial | `messaging` track |
| Route 53 | Simulated | `networking` track, `hybrid-dns-outage` |
| Backup | Real (runs are pg_dump) | `storage` track; `saa-storage-edge` lesson, `saa-exam-services` lab |
| Auto Scaling | API-emulated | `saa-autoscaling` lesson, `saa-exam-services` lab |

### 2. Design secure architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| IAM users, roles, policy evaluation | Partial | `iam` track, `iam-security` lab |
| KMS, Secrets Manager | Partial | `iam` track |
| Security groups, network ACLs | Simulated / API-emulated (NACLs, real evaluation) | `networking` track; NACLs: `saa-cost-detection` lesson, `saa-exam-services` lab |
| VPC endpoints | Simulated | SAA network-segmentation lesson |
| WAF, Shield | Simulated / API-emulated (Shield) | `waf` console; Shield: `saa-cost-detection` lesson |
| GuardDuty, Security Hub, Inspector | Partial | `monitoring` track |
| CloudTrail, Config | Partial | `monitoring` track |
| Macie, Audit Manager, Artifact | API-emulated | Macie: `saa-cost-detection`; Audit Manager/Artifact: `sap-landing-zone-services` lesson, `sap-exam-services` lab |

### 3. Design high-performing architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| CloudFront | API-emulated | `saa-storage-edge` lesson, `saa-exam-services` lab |
| ElastiCache | Partial | `databases` track |
| S3 storage classes and lifecycle | Partial (MinIO) | `storage` track |
| EBS, EFS, FSx | API-emulated | `saa-storage-edge` lesson, `saa-exam-services` lab |
| Lambda | Partial (sandboxed subprocess) | `serverless` track |
| ECS / Fargate | Partial / simulated | `containers` track |
| API Gateway | Partial | `serverless` track |
| Kinesis, Firehose | API-emulated / partial | `messaging` track |

### 4. Design cost-optimized architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| Cost Explorer, Budgets, Trusted Advisor | Partial | SAA cost-governance lesson |
| Compute Optimizer, Savings Plans | API-emulated | `saa-cost-detection` lesson, `saa-exam-services` lab |
| Tagging and showback | Real (resource tags) | `terraform` and SAA tracks |
| S3 lifecycle and idle-resource cleanup | Partial | `storage` track |

## SAP-C02 domains

### 1. Design solutions for organizational complexity

| Service | CloudForge | Practice |
| --- | --- | --- |
| Organizations, OUs, SCPs | Partial | `sap-landing-zone` |
| IAM Identity Center | API-emulated | `sap-landing-zone-services` lesson, `sap-exam-services` lab |
| Control Tower | API-emulated | `sap-landing-zone-services` lesson, `sap-exam-services` lab |
| Resource Access Manager | API-emulated | `sap-landing-zone-services` lesson, `sap-exam-services` lab |
| CloudTrail, Config, Security Hub (centralized) | Partial | `sap-guardrails-operations` |

### 2. Design for new solutions

| Service | CloudForge | Practice |
| --- | --- | --- |
| VPC, Transit Gateway, VPN | Simulated / API-emulated (VPN) | `sap-transit-gateway`, `sap-hybrid-network`; VPN: `sap-hybrid-connectivity-services` lesson, `sap-exam-services` lab |
| Route 53 Resolver and private zones | Simulated | `sap-hybrid-dns`, `hybrid-dns-outage` |
| Global Accelerator, Direct Connect | API-emulated | `sap-hybrid-connectivity-services` lesson, `sap-exam-services` lab |
| Network Firewall, flow logs | Simulated | `sap-network-security-scale` |
| Migration services (DMS, DataSync, MGN) | Partial / API-emulated (MGN) | `sap-migration-strategies`; MGN: `sap-data-migration-services` lesson, `sap-exam-services` lab |
| Disaster recovery patterns | Teaching model | `sap-dr-patterns` |

### 3. Continuous improvement and cost

| Topic | CloudForge | Practice |
| --- | --- | --- |
| Cost trade-offs and commitments | Partial | `sap-cost-resilience-tradeoffs` |
| Shared-services consolidation | Teaching model | `sap-cost-resilience-tradeoffs` |
| Resilience testing and game days | Teaching model | `sap-dr-patterns` |

## Industry break-and-fix incidents

The following scenarios are seeded alongside the original six and use only
rules the validation engine can actually evaluate:

| Scenario | Fault | What learners practise |
| --- | --- | --- |
| `checkout-sg-regression` | Security-group loss | Blast-radius reasoning, SG review, health-check blind spots |
| `nat-egress-collapse` | NAT gateway loss | Private versus public path tracing |
| `alb-target-latency` | Load-balancer latency | Client versus server latency, timeout alignment |
| `hybrid-dns-outage` | VPC resolver NXDOMAIN | DNS versus routing, private zones, hybrid forwarding |

## Honest gaps

The following remain honest gaps in lite mode, even after v3:

- **S3 objects in lite**: MinIO is not in the lite compose file, so S3
  object operations return an honest 503 in lite (buckets/records
  work in full mode). Object flows are unit-tested, not live-verified
  in lite.
- **Teaching state machines, not real infrastructure**: Storage
  Gateway (no appliance, no data cached), Snowball (no device
  ships), Lake Formation (grants recorded, not enforced), MGN
  (no servers replicated), VPN (no IPsec tunnel), Global
  Accelerator (teaching IPs, no traffic routed), Control Tower
  guardrails (recorded, not enforced SCPs), RAM (no second
  account), Identity Center (no login/federation). Each is
  labelled API-emulated with the limit in the registry and the
  fidelity table, and each lesson says so.
- **No utilization data**: Compute Optimizer recommendations are
  computed from lab state, not CloudWatch utilization; Savings
  Plan utilization is lab burn against the commitment, not a bill.
- **Transit Gateway attachments** remain route-table entries in
  lab state, not kernel peering.
- **Still UNSUPPORTED** (by prior decision or outside the lite
  set): EKS, Elastic Beanstalk, Batch, Step Functions, Redshift,
  Cognito, ACM, SES, S3 Storage Lens, Access Analyzer,
  DocumentDB, Neptune.

The validation engine currently grades network, instance, security-group,
route, DNS, load-balancer and metric rules. Graded exercises for S3, IAM,
Lambda, SQS and RDS need provider state to be wired into the validation
state before they can be added honestly. The new exam-services lessons
exercise the services through the API (curl steps in the lesson text)
rather than graded validation rules, for the same reason.
