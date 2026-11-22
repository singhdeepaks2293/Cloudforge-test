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
| RDS / Aurora | Partial (RDS), unsupported (Aurora) | `databases` track |
| SQS, SNS, EventBridge | Partial | `messaging` track |
| Route 53 | Simulated | `networking` track, `hybrid-dns-outage` |
| Backup | Real (policy records) | `storage` track |
| Auto Scaling | Unsupported | Reason about desired capacity in the SAA lessons |

### 2. Design secure architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| IAM users, roles, policy evaluation | Partial | `iam` track, `iam-security` lab |
| KMS, Secrets Manager | Partial | `iam` track |
| Security groups, network ACLs | Simulated / unsupported | `networking` track |
| VPC endpoints | Simulated | SAA network-segmentation lesson |
| WAF, Shield | Simulated / unsupported | `waf` console |
| GuardDuty, Security Hub, Inspector | Partial | `monitoring` track |
| CloudTrail, Config | Partial | `monitoring` track |
| Macie, Audit Manager, Artifact | Unsupported | Discussed in the SAP governance lessons |

### 3. Design high-performing architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| CloudFront | Unsupported | SAA caching lesson; compare with edge caching |
| ElastiCache | Partial | `databases` track |
| S3 storage classes and lifecycle | Partial (MinIO) | `storage` track |
| EBS, EFS, FSx | Unsupported | SAA compute-and-storage lesson |
| Lambda | Partial (sandboxed subprocess) | `serverless` track |
| ECS / Fargate | Partial / simulated | `containers` track |
| API Gateway | Partial | `serverless` track |
| Kinesis, Firehose | API-emulated / partial | `messaging` track |

### 4. Design cost-optimized architectures

| Service | CloudForge | Practice |
| --- | --- | --- |
| Cost Explorer, Budgets, Trusted Advisor | Partial | SAA cost-governance lesson |
| Compute Optimizer, Savings Plans | Unsupported | Discussed as exam concepts |
| Tagging and showback | Real (resource tags) | `terraform` and SAA tracks |
| S3 lifecycle and idle-resource cleanup | Partial | `storage` track |

## SAP-C02 domains

### 1. Design solutions for organizational complexity

| Service | CloudForge | Practice |
| --- | --- | --- |
| Organizations, OUs, SCPs | Partial | `sap-landing-zone` |
| IAM Identity Center | Unsupported | `sap-identity-federation` |
| Control Tower | Unsupported | `sap-landing-zone` |
| Resource Access Manager | Unsupported | `sap-identity-federation` |
| CloudTrail, Config, Security Hub (centralized) | Partial | `sap-guardrails-operations` |

### 2. Design for new solutions

| Service | CloudForge | Practice |
| --- | --- | --- |
| VPC, Transit Gateway, VPN | Simulated / unsupported | `sap-transit-gateway`, `sap-hybrid-network` |
| Route 53 Resolver and private zones | Simulated | `sap-hybrid-dns`, `hybrid-dns-outage` |
| Global Accelerator, Direct Connect | Unsupported | `sap-hybrid-dns`, `sap-transit-gateway` |
| Network Firewall, flow logs | Simulated | `sap-network-security-scale` |
| Migration services (DMS, DataSync, MGN) | Partial / unsupported | `sap-migration-strategies` |
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

The following SAA/SAP topics are taught but cannot be graded end-to-end yet:
Auto Scaling groups, CloudFront, Global Accelerator, Direct Connect,
Transit Gateway attachments, IAM Identity Center, Control Tower, Macie,
Audit Manager, Savings Plans and Compute Optimizer. They appear in the
service catalog with an explicit `UNSUPPORTED` label so learners can see the
gap rather than mistake a lesson for a working console.

The validation engine currently grades network, instance, security-group,
route, DNS, load-balancer and metric rules. Graded exercises for S3, IAM,
Lambda, SQS and RDS need provider state to be wired into the validation
state before they can be added honestly.
