/**
 * Shared catalogue data for the Advanced section.
 *
 * The curriculum YAML (curriculum/advanced/) is the source of truth
 * for lessons, tasks and grading rules. This module is only the
 * launch index the pages render: which lab each project launches,
 * whether it works in lite mode, and the interview / checklist
 * content used as a fallback until the /api/v1/advanced routes are
 * wired (see docs/advanced/ROUTES.md). The API copies live in
 * curriculum/advanced/interviews.json and skills-checklist.json;
 * keep the ids and titles in sync when either side changes.
 */

export interface AdvancedProject {
  id: string
  title: string
  kind: 'industry' | 'end-to-end'
  summary: string
  labDefinitionId: string
  durationMinutes: number
  liteLaunchable: boolean
  xpReward: number
  gradingRules: string[]
}

export const ADVANCED_PROJECTS: AdvancedProject[] = [
  {
    id: 'advanced-multi-tier',
    title: 'Multi-Tier Production Deployment',
    kind: 'industry',
    summary:
      'Public web tier, private application tier and isolated data tier across two Availability Zones, deployed with Terraform and validated against real state.',
    labDefinitionId: 'adv-multi-tier',
    durationMinutes: 120,
    liteLaunchable: false,
    xpReward: 300,
    gradingRules: ['vpc_exists', 'subnet_count', 'route_to_igw', 'route_to_nat', 'nat_gateway_exists', 'instance_running', 'load_balancer_healthy_targets', 'security_group_denies'],
  },
  {
    id: 'advanced-ha-dr',
    title: 'High Availability & Disaster Recovery',
    kind: 'industry',
    summary:
      'Multi-AZ design, failover that actually happens, backups that restore, and a disaster recovery plan you have rehearsed — not just written.',
    labDefinitionId: 'adv-ha-dr',
    durationMinutes: 60,
    liteLaunchable: false,
    xpReward: 300,
    gradingRules: ['vpc_exists', 'subnet_count', 'route_to_igw', 'route_to_nat', 'nat_gateway_exists', 'instance_running', 'load_balancer_healthy_targets'],
  },
  {
    id: 'advanced-migration',
    title: 'Migration: On-Prem to Cloud',
    kind: 'industry',
    summary:
      'Discover an on-prem estate, choose a strategy per workload, cut over without a big-bang outage, and prove the new home works.',
    labDefinitionId: 'adv-migration',
    durationMinutes: 30,
    liteLaunchable: true,
    xpReward: 300,
    gradingRules: ['vpc_exists', 'subnet_count', 'instance_running', 'instance_reachable', 'security_group_allows', 'dns_resolves'],
  },
  {
    id: 'advanced-cost-optimisation',
    title: 'Cost Optimisation in Production',
    kind: 'industry',
    summary:
      'Measure the bill, right-size from evidence, commit only to steady usage, and keep the savings with guardrails that stop the waste coming back.',
    labDefinitionId: 'adv-cost',
    durationMinutes: 30,
    liteLaunchable: true,
    xpReward: 250,
    gradingRules: ['vpc_exists', 'subnet_count', 'instance_running', 'instance_reachable', 'security_group_denies', 'metric_below'],
  },
  {
    id: 'advanced-incident-response',
    title: 'Security Incident Response',
    kind: 'industry',
    summary:
      'Detect a real incident, contain it without destroying evidence, eradicate the cause, recover safely, and write the review that stops it happening again.',
    labDefinitionId: 'adv-incident',
    durationMinutes: 30,
    liteLaunchable: true,
    xpReward: 300,
    gradingRules: ['vpc_exists', 'subnet_count', 'instance_running', 'instance_reachable', 'security_group_denies'],
  },
  {
    id: 'advanced-cicd-pipeline',
    title: 'End-to-End CI/CD: Staging to Production',
    kind: 'end-to-end',
    summary:
      'One immutable artifact through staging to production: tested, approved, promoted — and rolled back when the release is bad.',
    labDefinitionId: 'adv-cicd',
    durationMinutes: 30,
    liteLaunchable: true,
    xpReward: 350,
    gradingRules: ['vpc_exists', 'subnet_count', 'instance_running', 'load_balancer_healthy_targets', 'instance_reachable', 'security_group_allows', 'metric_below'],
  },
  {
    id: 'advanced-production-rollout',
    title: 'Production Rollout: Canary, Rollback & Alerting',
    kind: 'end-to-end',
    summary:
      'Canary rollout with numeric health gates, a rollback that runs in minutes, and alerting that tells you before your users do.',
    labDefinitionId: 'adv-rollout',
    durationMinutes: 30,
    liteLaunchable: true,
    xpReward: 350,
    gradingRules: ['vpc_exists', 'subnet_count', 'instance_running', 'load_balancer_healthy_targets', 'instance_reachable', 'security_group_allows', 'metric_below'],
  },
]

export interface AdvancedScenario {
  id: string
  title: string
  kind: 'route-blackhole' | 'latency' | 'loss' | 'dns'
  target: string
  symptom: string
  xpReward: number
  gradingRules: string[]
}

export const ADVANCED_SCENARIOS: AdvancedScenario[] = [
  {
    id: 'adv-cascade-checkout-outage',
    title: 'Outage: Checkout Cascades and the Logs Disagree',
    kind: 'latency',
    target: 'load_balancer',
    symptom:
      'Checkout p99 latency jumps from 300 ms to over 5 seconds, users retry and traffic doubles, and every log blames a different layer.',
    xpReward: 250,
    gradingRules: ['load_balancer_healthy_targets', 'instance_reachable', 'metric_below'],
  },
  {
    id: 'adv-multi-az-route-blackhole',
    title: 'Outage: One Availability Zone Goes Dark',
    kind: 'route-blackhole',
    target: 'route_table',
    symptom:
      'Half the fleet stops responding at once: everything in one zone fails while the other zone is fine, and nothing was deployed in the last hour.',
    xpReward: 250,
    gradingRules: ['route_to_igw', 'subnet_count', 'instance_reachable'],
  },
  {
    id: 'adv-dns-stale-records',
    title: "Incident: DNS Answers with Yesterday's Addresses",
    kind: 'dns',
    target: 'vpc_dns_resolver',
    symptom:
      'After a migration the same hostname resolves to the old address from one subnet and the new address from another, and the failures move as caches expire.',
    xpReward: 220,
    gradingRules: ['dns_resolves', 'instance_reachable'],
  },
  {
    id: 'adv-intermittent-packet-loss',
    title: 'Incident: The Flaky Egress Nobody Can Reproduce',
    kind: 'loss',
    target: 'nat_gateway',
    symptom:
      'Deployments fail one time in four, across unrelated endpoints, and the problem vanishes for hours at a time before coming back.',
    xpReward: 240,
    gradingRules: ['nat_gateway_exists', 'route_to_nat', 'instance_reachable'],
  },
  {
    id: 'adv-db-replica-lag',
    title: "Incident: Reads Return Yesterday's Data",
    kind: 'latency',
    target: 'database',
    symptom:
      'Writes succeed but reads served from the replica lag further behind, and the lag grows under load while the primary looks healthy.',
    xpReward: 240,
    gradingRules: ['instance_running', 'instance_reachable', 'metric_below'],
  },
  {
    id: 'adv-hybrid-dns-split-brain',
    title: 'Incident: Hybrid DNS Answers Differently on Each Side',
    kind: 'dns',
    target: 'route53_resolver',
    symptom:
      'The same hostname resolves to the cloud address from the office and the on-prem address from the VPC, and cross-link calls fail half the time.',
    xpReward: 250,
    gradingRules: ['dns_resolves', 'instance_reachable'],
  },
]

export interface InterviewFallback {
  id: string
  title: string
  roleFocus: string
  problem: string
  expectedApproach: string[]
  rubric: { criterion: string; goodLooksLike: string }[]
  pitfalls: string[]
  relatedProjects: string[]
}

export const INTERVIEW_FALLBACK: InterviewFallback[] = [
  {
    id: 'interview-design-ha-web',
    title: 'Design a highly available web application',
    roleFocus: 'cloud engineer',
    problem:
      'Design a web application that serves 10,000 concurrent users, must stay up if an Availability Zone fails, and stores user uploads and a relational dataset. Walk through the architecture, the failure modes, and how you would prove it works.',
    expectedApproach: [
      'Start from requirements: traffic shape, data durability, recovery objectives — ask clarifying questions before drawing boxes.',
      'Place a load balancer in front of stateless web instances across at least two Availability Zones.',
      'Put the application tier in private subnets and the database in isolated subnets, reachable only from the application tier.',
      'Store uploads in object storage and sessions in a shared store so instances stay stateless.',
      'Name the failure modes: zone loss, database primary loss, load balancer target failure — and the mitigation for each.',
      'Describe how you would prove it: health checks, a zone-failure game day, and validation against real state.',
    ],
    rubric: [
      { criterion: 'Clarifies requirements before designing', goodLooksLike: 'Asks about traffic, data, RTO/RPO and budget instead of drawing immediately.' },
      { criterion: 'Uses zones, tiers and trust boundaries correctly', goodLooksLike: 'Public web tier, private app tier, isolated data tier; at least two zones.' },
      { criterion: 'Keeps instances stateless', goodLooksLike: 'Sessions and uploads live in shared stores, not on instance disks.' },
      { criterion: 'Names failure modes and mitigations', goodLooksLike: 'Zone loss, primary loss and target failure each have a concrete answer.' },
      { criterion: 'Describes verification, not just the design', goodLooksLike: 'Health checks, game days and validation against real state.' },
    ],
    pitfalls: [
      'Drawing boxes before asking a single clarifying question.',
      'One Availability Zone with a promise to add a second later.',
      'State on the instance disk, which makes scaling and replacement impossible.',
    ],
    relatedProjects: ['advanced-multi-tier', 'advanced-ha-dr'],
  },
  {
    id: 'interview-debug-outage',
    title: 'Debug a production outage with conflicting logs',
    roleFocus: 'devops engineer',
    problem:
      'Checkout latency jumps from 300 ms to 5 seconds. The application logs blame the database, the database logs blame the network, and the load balancer reports timeouts. Users are retrying, which doubles the traffic. Walk through how you find the cause.',
    expectedApproach: [
      'Stabilise first: stop the retry storm from making it worse, and communicate status.',
      'Separate client latency from server latency: compare load-balancer latency with application request duration.',
      "Check one layer at a time — load balancer targets, application, database — with that layer's own metrics, not another layer's logs.",
      'Look for the single slow component: one target group, one zone, one replica.',
      'Form a hypothesis, test it with a targeted check, and only then act.',
      'After the fix, verify with the same metrics and record the timeline.',
    ],
    rubric: [
      { criterion: 'Stabilises and communicates before deep debugging', goodLooksLike: 'Mentions mitigating the retry storm and telling users what is happening.' },
      { criterion: "Uses each layer's own metrics", goodLooksLike: "Does not take one layer's logs as proof about another layer." },
      { criterion: 'Narrows by splitting the system', goodLooksLike: 'Per-zone, per-target-group, per-tier comparisons.' },
      { criterion: 'Hypothesis then test, not guessing', goodLooksLike: 'States what would confirm or refute each hypothesis.' },
      { criterion: 'Verifies the fix and records the timeline', goodLooksLike: 'Same metrics after the fix, plus a timeline for the review.' },
    ],
    pitfalls: [
      'Believing the first log that names a culprit.',
      'Restarting everything and calling it fixed without knowing the cause.',
      'Debugging for an hour without telling anyone the service is down.',
    ],
    relatedProjects: ['advanced-production-rollout', 'advanced-incident-response'],
  },
  {
    id: 'interview-migration-plan',
    title: 'Plan an on-prem to cloud migration',
    roleFocus: 'solutions architect',
    problem:
      'A company has 40 on-prem servers running a mix of web applications, a reporting database and file shares. They want to move to the cloud in six months. Walk through how you plan the migration.',
    expectedApproach: [
      'Start with discovery: inventory every server, application, dependency and data flow before choosing anything.',
      'Score workloads on cost, criticality and coupling, and find the ones to retire.',
      'Choose a strategy per workload — rehost, replatform, repurchase, refactor, retire, retain — and say why for each.',
      'Plan in waves ordered by dependency, easiest first, so the team learns the process on low-risk moves.',
      'Design the cutover: parallel running, continuous replication, gradual traffic shift, tested rollback.',
      'Cover data verification, decommissioning, and the landing zone the workloads move into.',
    ],
    rubric: [
      { criterion: 'Discovers before deciding', goodLooksLike: 'Inventory and dependency mapping come before any strategy choice.' },
      { criterion: 'Chooses per workload with reasons', goodLooksLike: 'Different strategies for different workloads, each justified.' },
      { criterion: 'Plans in dependency-ordered waves', goodLooksLike: 'Easiest, lowest-risk waves first; dependent workloads move together.' },
      { criterion: 'Designs a safe cutover', goodLooksLike: 'Parallel running, replication, gradual shift, tested rollback.' },
      { criterion: 'Covers data and decommissioning', goodLooksLike: 'Verification of data, old-estate retention and decommissioning plan.' },
    ],
    pitfalls: [
      'Choosing one strategy for all 40 servers.',
      'A big-bang cutover weekend with no rollback.',
      'Forgetting the reporting job that reads the production database directly.',
    ],
    relatedProjects: ['advanced-migration'],
  },
  {
    id: 'interview-cost-doubled',
    title: 'The cloud bill doubled overnight',
    roleFocus: 'cloud engineer',
    problem:
      'The monthly cloud bill doubled. The finance team wants answers and the engineering team says nothing changed. Walk through how you find the cause and stop it happening again.',
    expectedApproach: [
      'Attribute first: split the bill by tag, service and team to find which line actually grew.',
      'Check the trend, not just the month: when did the growth start, and what changed then?',
      'Look for the usual causes: an oversized instance, a forgotten test environment, a runaway job, data transfer, orphaned storage.',
      'Verify with utilisation evidence before changing anything — do not delete on suspicion.',
      'Fix the cause, then add the guardrail: budget alerts, tagging policy, scheduled shutdowns, a monthly review.',
      'Communicate what happened, what it cost, and what changed, in plain language.',
    ],
    rubric: [
      { criterion: 'Attributes before acting', goodLooksLike: 'Tags and per-service split locate the growing line first.' },
      { criterion: 'Uses trend and change correlation', goodLooksLike: 'Finds when growth started and what changed at that time.' },
      { criterion: 'Verifies with evidence', goodLooksLike: 'Utilisation data before any deletion or resizing.' },
      { criterion: 'Adds a guardrail, not just a fix', goodLooksLike: 'Budget alerts, policies or shutdowns that stop recurrence.' },
      { criterion: 'Communicates clearly', goodLooksLike: 'Plain-language summary of cause, cost and prevention.' },
    ],
    pitfalls: [
      'Deleting resources on suspicion without evidence.',
      'Optimising the most visible line instead of the biggest one.',
      'Fixing the bill and adding no guardrail, so it happens again.',
    ],
    relatedProjects: ['advanced-cost-optimisation'],
  },
  {
    id: 'interview-cicd-design',
    title: 'Design a CI/CD pipeline for a regulated team',
    roleFocus: 'devops engineer',
    problem:
      'A team ships a customer-facing application and must prove every production change was tested and approved. Design their CI/CD pipeline: stages, environments, approvals, rollback and audit trail.',
    expectedApproach: [
      'Define the stages: checkout, build, lint, unit tests, integration tests against real services, packaging into one immutable artifact.',
      'Promote the same artifact through staging to production; never rebuild per environment.',
      'Put an approval gate between staging and production with the staging evidence attached.',
      'Design rollback before the first deploy: keep the previous artifact, make the traffic shift reversible, keep database changes expand-then-contract.',
      'Record the audit trail: who approved, what version, when, and the test evidence for that version.',
      'Add deployment monitoring: error rate and latency gates, alerts linked to the rollback runbook.',
    ],
    rubric: [
      { criterion: 'Covers the full pipeline with evidence at each stage', goodLooksLike: 'Build, test, package stages each record what ran and what came out.' },
      { criterion: 'Promotes one immutable artifact', goodLooksLike: 'Same artifact in staging and production; config injected per environment.' },
      { criterion: 'Approval gate with evidence', goodLooksLike: 'A recorded human decision with test results and plan attached.' },
      { criterion: 'Rollback designed up front', goodLooksLike: 'Previous artifact kept, reversible traffic shift, compatible database changes.' },
      { criterion: 'Audit trail and deployment monitoring', goodLooksLike: 'Who approved what and when; error-rate gates and alerting.' },
    ],
    pitfalls: [
      'Rebuilding the artifact for production.',
      'An approval gate with no evidence attached to approve against.',
      'A rollback plan invented during the first bad deploy.',
    ],
    relatedProjects: ['advanced-cicd-pipeline', 'advanced-production-rollout'],
  },
  {
    id: 'interview-security-incident',
    title: 'Respond to a compromised cloud instance',
    roleFocus: 'cloud engineer',
    problem:
      'An instance is making outbound connections at 3 a.m. to an unknown address, and an access key has been used from two countries at once. Walk through the incident response.',
    expectedApproach: [
      'Treat it as an incident: correlate the signals, confirm with the instance’s own logs and metrics.',
      'Contain first: isolate the instance by cutting its network path, revoke the exposed key, snapshot the disk before touching anything.',
      'Investigate from the preserved evidence: timeline, entry point, what was accessed.',
      'Eradicate the cause: rotate credentials, close the hole, rebuild from known-good images — do not clean in place.',
      'Recover in stages and verify with the same signals that detected the incident.',
      'Write a blameless review with actions, owners and dates.',
    ],
    rubric: [
      { criterion: 'Correlates signals before acting', goodLooksLike: 'Odd egress plus key reuse plus timing form the picture.' },
      { criterion: 'Contains without destroying evidence', goodLooksLike: 'Isolate, revoke, snapshot — in that order.' },
      { criterion: 'Eradicates the cause, not the symptom', goodLooksLike: 'Credential rotation, hole closed, rebuild from image.' },
      { criterion: 'Recovers in stages and verifies', goodLooksLike: 'Restricted network first; same detection signals checked quiet.' },
      { criterion: 'Closes with a blameless review', goodLooksLike: 'Timeline, root cause, actions with owners and dates.' },
    ],
    pitfalls: [
      'Deleting the instance immediately and destroying the evidence.',
      'Deleting the suspicious process while the stolen key still works.',
      'Cleaning a compromised system in place instead of rebuilding it.',
    ],
    relatedProjects: ['advanced-incident-response'],
  },
  {
    id: 'interview-scale-traffic-spike',
    title: 'Handle a 10x traffic spike',
    roleFocus: 'solutions architect',
    problem:
      'A marketing campaign sends ten times the normal traffic to the application with two hours’ notice. Walk through how you keep the service up, and what you change afterwards so the next spike is boring.',
    expectedApproach: [
      'Check the current headroom and the scaling limits: instance caps, database connections, load balancer targets.',
      'Scale the stateless tiers first — they scale horizontally — and pre-warm what cannot scale fast.',
      'Protect the database: connection limits, read replicas for reads, caching in front of hot queries.',
      'Shed load gracefully if needed: degrade non-critical features rather than failing all requests.',
      'Watch the right metrics during the spike and keep a rollback for any change made in a hurry.',
      'Afterwards: make scaling automatic, load-test to the new peak, and record the runbook.',
    ],
    rubric: [
      { criterion: 'Checks headroom and limits first', goodLooksLike: 'Instance caps, connection limits and target health before scaling.' },
      { criterion: 'Scales stateless tiers, protects the database', goodLooksLike: 'Horizontal scaling for web/app; connection limits and caching for the database.' },
      { criterion: 'Has a graceful degradation plan', goodLooksLike: 'Non-critical features degrade instead of all requests failing.' },
      { criterion: 'Watches metrics and keeps changes reversible', goodLooksLike: 'Monitors during the spike; hurried changes can be rolled back.' },
      { criterion: 'Makes the next spike boring', goodLooksLike: 'Autoscaling, load tests, runbook updates.' },
    ],
    pitfalls: [
      'Scaling the database the way you scale the web tier.',
      'Raising limits without checking what breaks first.',
      'Making hurried changes with no way back.',
    ],
    relatedProjects: ['advanced-multi-tier', 'advanced-ha-dr'],
  },
  {
    id: 'interview-dr-plan',
    title: 'Defend a disaster recovery plan',
    roleFocus: 'solutions architect',
    problem:
      'You propose a disaster recovery plan to the business. They ask: how long will we be down, how much data will we lose, what does it cost, and how do you know it works? Walk through your answer.',
    expectedApproach: [
      'State RTO and RPO as numbers, and tie them to the cost of downtime the business already accepts.',
      'Present the strategy that fits those numbers — backup/restore, pilot light, warm standby or multi-site — with its cost.',
      'Show the tradeoff explicitly: what a faster recovery costs, and what a slower one risks.',
      'Explain how failover is triggered and who can declare a disaster, with named people and thresholds.',
      'Prove it works: game days, rehearsal results, measured recovery times — not a document that was never run.',
      'Cover the return to normal: failback, verification, and the post-incident review.',
    ],
    rubric: [
      { criterion: 'States RTO and RPO as numbers tied to business cost', goodLooksLike: 'Numbers, not adjectives, linked to what downtime costs.' },
      { criterion: 'Matches strategy to the numbers', goodLooksLike: 'The chosen strategy is justified by the RTO/RPO, not by preference.' },
      { criterion: 'Shows the cost tradeoff honestly', goodLooksLike: 'Faster recovery costs more; the tradeoff is explicit.' },
      { criterion: 'Names who declares and how failover triggers', goodLooksLike: 'Named people, thresholds, automatic where possible.' },
      { criterion: 'Proves it with rehearsal evidence', goodLooksLike: 'Game days and measured recovery times, not an untested document.' },
    ],
    pitfalls: [
      'Quoting RTO/RPO as adjectives like fast or minimal.',
      'A DR plan that has never been rehearsed.',
      'Hiding the cost of the standby from the business.',
    ],
    relatedProjects: ['advanced-ha-dr', 'advanced-cicd-pipeline'],
  },
]

export interface ArchetypeFallback {
  id: string
  title: string
  summary: string
  skills: { name: string; required: boolean; advancedProjects: string[] }[]
}

export const ARCHETYPE_FALLBACK: ArchetypeFallback[] = [
  {
    id: 'cloud-engineer',
    title: 'Cloud Engineer',
    summary:
      'Builds and operates cloud infrastructure: networks, compute, storage and databases, with reliability and cost under control.',
    skills: [
      { name: 'Design multi-tier VPC architectures across Availability Zones', required: true, advancedProjects: ['advanced-multi-tier', 'advanced-ha-dr'] },
      { name: 'Plan and test backup, failover and disaster recovery', required: true, advancedProjects: ['advanced-ha-dr', 'advanced-migration'] },
      { name: 'Migrate workloads and right-size resources from measured utilisation', required: true, advancedProjects: ['advanced-migration', 'advanced-cost-optimisation'] },
      { name: 'Read cloud bills, attribute spend and control cost with guardrails', required: true, advancedProjects: ['advanced-cost-optimisation'] },
      { name: 'Respond to security incidents: contain, eradicate, recover, review', required: true, advancedProjects: ['advanced-incident-response'] },
      { name: 'Operate load balancers, DNS and private networking in production', required: false, advancedProjects: ['advanced-multi-tier', 'advanced-ha-dr'] },
    ],
  },
  {
    id: 'devops-engineer',
    title: 'DevOps Engineer',
    summary:
      'Owns the path from commit to production: pipelines, deployments, rollback and the observability that keeps releases safe.',
    skills: [
      { name: 'Build CI/CD pipelines that test, approve and promote one immutable artifact', required: true, advancedProjects: ['advanced-cicd-pipeline'] },
      { name: 'Run staging-to-production promotions with approval gates', required: true, advancedProjects: ['advanced-cicd-pipeline', 'advanced-production-rollout'] },
      { name: 'Ship with canary, blue-green and rolling strategies and health gates', required: true, advancedProjects: ['advanced-production-rollout'] },
      { name: 'Design rollback that runs in minutes and verify it afterwards', required: true, advancedProjects: ['advanced-production-rollout', 'advanced-cicd-pipeline'] },
      { name: 'Wire monitoring, alerting and runbooks into every deployment', required: true, advancedProjects: ['advanced-cicd-pipeline', 'advanced-production-rollout'] },
      { name: 'Debug production incidents from logs and metrics, and write the review', required: false, advancedProjects: ['advanced-incident-response'] },
    ],
  },
  {
    id: 'solutions-architect',
    title: 'Solutions Architect',
    summary:
      'Designs systems and guides teams: architecture, migration and operations, with tradeoffs made explicit and costed.',
    skills: [
      { name: 'Design highly available systems and defend the tradeoffs', required: true, advancedProjects: ['advanced-multi-tier', 'advanced-ha-dr'] },
      { name: 'Define RTO and RPO as numbers and design disaster recovery to meet them', required: true, advancedProjects: ['advanced-ha-dr'] },
      { name: 'Plan migrations: discovery, per-workload strategy, waves and cutover', required: true, advancedProjects: ['advanced-migration'] },
      { name: 'Reason about cost, reliability and performance tradeoffs in the open', required: true, advancedProjects: ['advanced-cost-optimisation', 'advanced-ha-dr'] },
      { name: 'Design deployment and operational models a team can run safely', required: true, advancedProjects: ['advanced-cicd-pipeline', 'advanced-production-rollout'] },
      { name: 'Design for security: containment, least privilege and incident response', required: false, advancedProjects: ['advanced-incident-response'] },
    ],
  },
]
