/**
 * v3 lite exam set — generic services with no dedicated console and
 * no backend runtime. Kept in a services module (not a component
 * file) so the map can be shared without tripping fast-refresh.
 * Each entry lists the available CloudForge service that teaches
 * the same concept, plus the honest reason the service is not built.
 */
export const LITE_GENERIC_SERVICES: Record<string, { alternatives: { label: string; to: string; note: string }[]; note?: string }> = {
  autoscaling: {
    alternatives: [{ label: 'EC2 — Instances', to: '/services/ec2', note: 'Labs use a fixed instance count; scale by launching or stopping instances.' }],
    note: 'Auto Scaling groups are not modelled: labs use a fixed instance count. Reason about desired capacity in the SAA lessons.',
  },
  aurora: {
    alternatives: [{ label: 'RDS — DB instances', to: '/services/rds', note: 'Real Postgres databases on the platform Postgres.' }],
    note: 'Aurora clusters are not built. RDS is the available managed-database practice.',
  },
  backup: {
    alternatives: [
      { label: 'RDS — Snapshots', to: '/services/rds', note: 'Real pg_dump snapshots with restore.' },
      { label: 'S3 — Buckets', to: '/services/s3', note: 'Object storage on local MinIO.' },
    ],
    note: 'Backup in CloudForge is scripts/backup.sh and scripts/restore.sh (pg_dump -Fc / pg_restore) run on the host — there is no backup-plan API and no in-app Backup console, so no plan is created here.',
  },
  macie: {
    alternatives: [
      { label: 'GuardDuty — Findings', to: '/services/guardduty', note: 'Findings from the real audit log.' },
      { label: 'Security Hub — Findings', to: '/services/securityhub', note: 'Aggregated findings.' },
    ],
    note: 'Macie sensitive-data discovery is not built. GuardDuty and Security Hub findings are the available detection practice.',
  },
  auditmanager: {
    alternatives: [{ label: 'CloudTrail — Event history', to: '/services/cloudtrail', note: 'Audit-log export (CSV/JSON) is the available evidence surface.' }],
    note: 'Audit Manager evidence collection is not built. Use the CloudTrail audit export for audit evidence.',
  },
  artifact: {
    alternatives: [{ label: 'CloudTrail — Event history', to: '/services/cloudtrail', note: 'Audit-log export for compliance evidence.' }],
    note: 'Artifact compliance reports and agreements are not built.',
  },
  cloudfront: {
    alternatives: [
      { label: 'S3 — Buckets', to: '/services/s3', note: 'Object storage; compare origin behaviour with edge caching in the SAA lesson.' },
      { label: 'ElastiCache — Clusters', to: '/services/elasticache', note: 'In-memory caching practice.' },
    ],
    note: 'CloudFront distributions are not built. The SAA caching lesson compares edge caching against these local services.',
  },
  ebs: {
    alternatives: [{ label: 'EC2 — Instances', to: '/services/ec2', note: 'Lab instances carry their own disks; the Volumes tab states EBS is unsupported.' }],
    note: 'EBS volumes are not built — there is no volume API; lab instances carry their own disks.',
  },
  computeoptimizer: {
    alternatives: [
      { label: 'Cost Explorer — Costs & budgets', to: '/services/costexplorer', note: 'Educational cost estimates computed from real usage.' },
      { label: 'Trusted Advisor — Checks', to: '/services/trustedadvisor', note: 'Five real checks, including quota usage.' },
    ],
    note: 'Compute Optimizer rightsizing recommendations are not built. Cost Explorer and Budgets are the available cost practice.',
  },
  efs: {
    alternatives: [{ label: 'S3 — Buckets', to: '/services/s3', note: 'Shared object storage on local MinIO.' }],
    note: 'EFS file systems are not built.',
  },
  fsx: {
    alternatives: [{ label: 'S3 — Buckets', to: '/services/s3', note: 'Shared object storage on local MinIO.' }],
    note: 'FSx file systems are not built.',
  },
  shield: {
    alternatives: [{ label: 'WAF — Web ACLs', to: '/services/waf', note: 'Real rule evaluation (evaluation only, not inline).' }],
    note: 'Shield DDoS protection is not built. WAF is the available web-protection practice.',
  },
  controltower: {
    alternatives: [{ label: 'Organizations — Units', to: '/services/organizations', note: 'Organizational units over the real user store.' }],
    note: 'Control Tower landing zones are not built. Organizations and SCP concepts are taught in the SAP track.',
  },
  identitycenter: {
    alternatives: [{ label: 'IAM — Users, Roles, Policies', to: '/services/iam', note: 'Teaching IAM with a real policy evaluator (subset).' }],
    note: 'IAM Identity Center (workforce SSO, permission sets) is not built. IAM users, roles and policy evaluation are available.',
  },
  ram: {
    alternatives: [{ label: 'Organizations — Units', to: '/services/organizations', note: 'Organizational units over the real user store.' }],
    note: 'Resource Access Manager cross-account sharing is not built.',
  },
  savingsplans: {
    alternatives: [
      { label: 'Cost Explorer — Costs & budgets', to: '/services/costexplorer', note: 'Educational cost estimates computed from real usage.' },
      { label: 'Budgets (Cost Explorer console)', to: '/services/costexplorer', note: 'Budgets evaluated against the real cost model on every read.' },
    ],
    note: 'Savings Plans commitment-based compute discounts are not built. Cost concepts are taught in the SAA track.',
  },
  vpn: {
    alternatives: [
      { label: 'Transit Gateway', to: '/services/transitgateway', note: 'Hub attachments as real route-table entries (SIMULATED).' },
      { label: 'VPC', to: '/services/vpc', note: 'Lab VPCs, subnets and route tables.' },
    ],
    note: 'VPN connections are not built. Transit Gateway attachments are the available hybrid-networking practice.',
  },
  directconnect: {
    alternatives: [
      { label: 'VPN', to: '/services/vpn', note: 'Also unsupported — see the hybrid-networking alternatives there.' },
      { label: 'Transit Gateway', to: '/services/transitgateway', note: 'Hub attachments as real route-table entries (SIMULATED).' },
    ],
    note: 'Direct Connect dedicated links are not built. VPN and Transit Gateway concepts are simulated in the VPC console.',
  },
  globalaccelerator: {
    alternatives: [
      { label: 'Route 53 — Hosted zones', to: '/services/route53', note: 'DNS records with a resolve endpoint.' },
      { label: 'ELB — Load balancers', to: '/services/elb', note: 'In-process round-robin with real health probes.' },
    ],
    note: 'Global Accelerator is not built. Practise failover with Route 53-style DNS and load-balancer health rules.',
  },
  mgn: {
    alternatives: [
      { label: 'DMS — Replication tasks', to: '/services/dms', note: 'Real row copies between Postgres databases.' },
      { label: 'DataSync — Tasks', to: '/services/datasync', note: 'Real copies with SHA-256 verification.' },
    ],
    note: 'Application Migration Service (lift-and-shift) is not built. DMS and DataSync are the available migration practice.',
  },
}

