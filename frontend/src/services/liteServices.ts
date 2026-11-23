/**
 * v3 lite exam set — the "What you can do instead" map for the exam
 * services. Every service listed here now HAS a dedicated console and
 * a backend runtime (v3 migration 014): the 17 exam services have
 * dedicated consoles in ExamServiceConsoles.tsx, and autoscaling, ebs
 * and vpn have real API-emulated backends (durable records + actions)
 * whose EC2/VPC console sections are now wired to them (v4): the EC2
 * Volumes section creates/attaches/detaches volumes, the EC2 Auto
 * Scaling Groups section creates/scales groups, and the VPC console
 * VPN section creates customer gateways, VPN gateways and connections
 * with tunnel activate/deactivate.
 * "What you can do instead" card on every exam console, and
 * ServiceDetail reads it as the fallback for /services/:id routes no
 * dedicated route or redirect matched first. The `note` on each entry
 * is the honest one-liner the Learning tab shows for that service.
 * Kept in a services module (not a component file) so the map can be
 * shared without tripping fast-refresh. Each entry lists the available
 * CloudForge service that teaches the same concept.
 */
export const LITE_GENERIC_SERVICES: Record<string, { alternatives: { label: string; to: string; note: string }[]; note?: string }> = {
  autoscaling: {
    alternatives: [{ label: 'EC2 — Instances', to: '/services/ec2', note: 'Labs use a fixed instance count; scale by launching or stopping instances.' }],
    note: 'Auto Scaling groups have a real API-emulated backend (durable records with min/desired/max and a scale action), wired to the EC2 console Auto Scaling Groups section — create a group there to practise desired capacity. Scaling records capacity only; it launches no instances.',
  },
  aurora: {
    alternatives: [{ label: 'RDS — DB instances', to: '/services/rds', note: 'Real Postgres databases on the platform Postgres.' }],
    note: 'Dedicated Aurora console at /services/aurora — clusters, instances, endpoints and failover over the API-emulated backend.',
  },
  backup: {
    alternatives: [
      { label: 'RDS — Snapshots', to: '/services/rds', note: 'Real pg_dump snapshots with restore.' },
      { label: 'S3 — Buckets', to: '/services/s3', note: 'Object storage on local MinIO.' },
    ],
    note: 'Dedicated Backup console at /services/backup — plans, vaults and jobs; a plan run is a real pg_dump via the API. Restore stays host-side: scripts/backup.sh and scripts/restore.sh.',
  },
  macie: {
    alternatives: [
      { label: 'GuardDuty — Findings', to: '/services/guardduty', note: 'Findings from the real audit log.' },
      { label: 'Security Hub — Findings', to: '/services/securityhub', note: 'Aggregated findings.' },
    ],
    note: 'Dedicated Macie console at /services/macie — classification jobs, runs and findings over the API-emulated backend.',
  },
  auditmanager: {
    alternatives: [{ label: 'CloudTrail — Event history', to: '/services/cloudtrail', note: 'Audit-log export (CSV/JSON) is the available evidence surface.' }],
    note: 'Dedicated Audit Manager console at /services/auditmanager — assessments and evidence collection over the API-emulated backend.',
  },
  artifact: {
    alternatives: [{ label: 'CloudTrail — Event history', to: '/services/cloudtrail', note: 'Audit-log export for compliance evidence.' }],
    note: 'Dedicated Artifact console at /services/artifact — reports catalogue and agreements.',
  },
  cloudfront: {
    alternatives: [
      { label: 'S3 — Buckets', to: '/services/s3', note: 'Object storage; compare origin behaviour with edge caching in the SAA lesson.' },
      { label: 'ElastiCache — Clusters', to: '/services/elasticache', note: 'In-memory caching practice.' },
    ],
    note: 'Dedicated CloudFront console at /services/cloudfront — distributions (survey columns), invalidations and the other survey nav items.',
  },
  ebs: {
    alternatives: [{ label: 'EC2 — Instances', to: '/services/ec2', note: 'Lab instances carry their own disks; the Volumes section manages EBS volumes.' }],
    note: 'EBS volumes have a real API-emulated backend (durable records with attach/detach against real labs), wired to the EC2 console Volumes section — volumes can be created, attached and detached there. Attach is recorded only; the lab disk is unchanged.',
  },
  computeoptimizer: {
    alternatives: [
      { label: 'Cost Explorer — Costs & budgets', to: '/services/costexplorer', note: 'Educational cost estimates computed from real usage.' },
      { label: 'Trusted Advisor — Checks', to: '/services/trustedadvisor', note: 'Five real checks, including quota usage.' },
    ],
    note: 'Dedicated Compute Optimizer console at /services/computeoptimizer — recommendations computed from real lab state.',
  },
  efs: {
    alternatives: [{ label: 'S3 — Buckets', to: '/services/s3', note: 'Shared object storage on local MinIO.' }],
    note: 'Dedicated EFS console at /services/efs — file systems and mount targets over the API-emulated backend.',
  },
  fsx: {
    alternatives: [{ label: 'S3 — Buckets', to: '/services/s3', note: 'Shared object storage on local MinIO.' }],
    note: 'Dedicated FSx console at /services/fsx — file systems and mount targets over the API-emulated backend.',
  },
  shield: {
    alternatives: [{ label: 'WAF — Web ACLs', to: '/services/waf', note: 'Real rule evaluation (evaluation only, not inline).' }],
    note: 'Dedicated Shield console at /services/shield — protection records over the API-emulated backend.',
  },
  controltower: {
    alternatives: [{ label: 'Organizations — Units', to: '/services/organizations', note: 'Organizational units over the real user store.' }],
    note: 'Dedicated Control Tower console at /services/controltower — landing zones and guardrails.',
  },
  identitycenter: {
    alternatives: [{ label: 'IAM — Users, Roles, Policies', to: '/services/iam', note: 'Teaching IAM with a real policy evaluator (subset).' }],
    note: 'Dedicated IAM Identity Center console at /services/identitycenter — permission sets and assignments.',
  },
  ram: {
    alternatives: [{ label: 'Organizations — Units', to: '/services/organizations', note: 'Organizational units over the real user store.' }],
    note: 'Dedicated Resource Access Manager console at /services/ram — resource shares and principals.',
  },
  savingsplans: {
    alternatives: [
      { label: 'Cost Explorer — Costs & budgets', to: '/services/costexplorer', note: 'Educational cost estimates computed from real usage.' },
      { label: 'Budgets (Cost Explorer console)', to: '/services/costexplorer', note: 'Budgets evaluated against the real cost model on every read.' },
    ],
    note: 'Dedicated Savings Plans console at /services/savingsplans — plans with utilization computed from real lab hours.',
  },
  vpn: {
    alternatives: [
      { label: 'Transit Gateway', to: '/services/transitgateway', note: 'Hub attachments as real route-table entries (SIMULATED).' },
      { label: 'VPC', to: '/services/vpc', note: 'Lab VPCs, subnets and route tables.' },
    ],
    note: 'VPN has a real API-emulated backend (customer gateways, VPN gateways and connections with tunnel activate/deactivate), wired to the VPC console VPN section — connections can be created and managed there. Tunnel state is a teaching field; no real IPsec tunnel exists. Transit Gateway attachments are the available hybrid-networking practice.',
  },
  directconnect: {
    alternatives: [
      { label: 'VPN', to: '/services/vpc?section=vpn', note: 'API-EMULATED — see the VPC console VPN section.' },
      { label: 'Transit Gateway', to: '/services/transitgateway', note: 'Hub attachments as real route-table entries (SIMULATED).' },
    ],
    note: 'Dedicated Direct Connect console at /services/directconnect — connections with a confirm step.',
  },
  globalaccelerator: {
    alternatives: [
      { label: 'Route 53 — Hosted zones', to: '/services/route53', note: 'DNS records with a resolve endpoint.' },
      { label: 'ELB — Load balancers', to: '/services/elb', note: 'In-process round-robin with real health probes.' },
    ],
    note: 'Dedicated Global Accelerator console at /services/globalaccelerator — accelerators, endpoint groups and failover.',
  },
  mgn: {
    alternatives: [
      { label: 'DMS — Replication tasks', to: '/services/dms', note: 'Real row copies between Postgres databases.' },
      { label: 'DataSync — Tasks', to: '/services/datasync', note: 'Real copies with SHA-256 verification.' },
    ],
    note: 'Dedicated Application Migration console at /services/mgn — source servers with the replication lifecycle.',
  },
}

