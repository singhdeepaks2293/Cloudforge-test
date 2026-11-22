# Advanced projects and scenarios

Every item below is a launchable, gradable unit. Track files live in `curriculum/advanced/`;
scenarios in `curriculum/break-and-fix/adv-*.yaml`. Each track has 4 lessons (each ≥ 150 words of
theory with a mermaid diagram), a guided lab, a challenge and a 6-question assessment.

## 1. Industry projects

### advanced-multi-tier — Multi-Tier Production Deployment (300 XP)

- Prerequisites: `networking`, `ec2`, `databases`.
- Guided lab: `adv-multi-tier` (120 min) — **full mode only**; lite refuses 120-minute labs.
- Challenge: one Availability Zone fails during peak traffic; keep ≥ 2 healthy load balancer targets,
  ≥ 2 running instances, and the database port denied from the internet.
- Grading rules: `vpc_exists`, `subnet_count >= 4`, `route_to_igw`, `route_to_nat`,
  `nat_gateway_exists`, `instance_running >= 2`, `load_balancer_healthy_targets >= 2`,
  `security_group_denies` (3306 from 0.0.0.0/0).
- Teaches: trust boundaries, stateless tiers, data-tier isolation, Terraform deployment, validation
  against real state.

### advanced-ha-dr — High Availability & Disaster Recovery (300 XP)

- Prerequisites: `networking`, `architecture`.
- Guided lab: `adv-ha-dr` (60 min) — **full mode only**; lite refuses 60-minute labs.
- Challenge: an Availability Zone is gone; prove the system still serves traffic with ≥ 2 healthy
  targets, ≥ 2 running instances and a surviving NAT gateway.
- Grading rules: `vpc_exists`, `subnet_count >= 4`, `route_to_igw`, `route_to_nat`,
  `nat_gateway_exists >= 2`, `instance_running >= 2`, `load_balancer_healthy_targets >= 2`.
- Teaches: single points of failure, RTO/RPO as numbers, pilot light / warm standby / multi-site,
  game days, DR runbooks.

### advanced-migration — Migration: On-Prem to Cloud (300 XP)

- Prerequisites: `fundamentals`, `storage`, `databases`.
- Guided lab: `adv-migration` (30 min) — **lite-launchable**.
- Challenge: the maintenance window was cut in half; prove the landing zone is ready anyway.
- Grading rules: `vpc_exists`, `subnet_count >= 2`, `instance_running >= 1`, `instance_reachable`,
  `security_group_allows` (443), `dns_resolves` (archive.ubuntu.com).
- Teaches: discovery, the six strategies (rehost / replatform / repurchase / refactor / retire /
  retain), wave planning, cutover without a big bang, data verification, decommissioning.

### advanced-cost-optimisation — Cost Optimisation in Production (250 XP)

- Prerequisites: `ec2`, `monitoring`.
- Guided lab: `adv-cost` (30 min) — **lite-launchable**.
- Challenge: the bill doubled overnight; prove the fleet still runs, SSH is not open to the internet,
  and the recorded CPU utilisation is below 70%.
- Grading rules: `vpc_exists`, `subnet_count >= 2`, `instance_running >= 1`, `instance_reachable`,
  `security_group_denies` (22 from 0.0.0.0/0), `metric_below` (cpu_utilization < 70).
- Teaches: bill attribution with tags, right-sizing from measured utilisation, reservations covering
  the floor not the peak, budget alerts and guardrails.

### advanced-incident-response — Security Incident Response (300 XP)

- Prerequisites: `iam`, `monitoring`.
- Guided lab: `adv-incident` (30 min) — **lite-launchable**.
- Challenge: the attacker is still active; prove containment with SSH and RDP denied from the
  internet and a known-good instance still running and reachable.
- Grading rules: `vpc_exists`, `subnet_count >= 2`, `instance_running >= 1`, `instance_reachable`,
  `security_group_denies` (22 and 3389 from 0.0.0.0/0).
- Teaches: detection by correlation, isolate-first containment without destroying evidence,
  eradication of the cause, staged recovery, blameless post-incident review.
- Note: the lab task also says to remove the compromised subnet's internet route, but that is not a
  grading rule — `no_route_to_internet` checks *all* route tables, so it cannot grade one subnet's
  isolation in a lab that also has public subnets. See `GRADING.md`.

## 2. End-to-end deployment projects

### advanced-cicd-pipeline — End-to-End CI/CD: Staging to Production (350 XP)

- Prerequisites: `devops`, `containers`.
- Guided lab: `adv-cicd` (30 min) — **lite-launchable**.
- Pipeline stages: checkout → build → lint → unit tests → integration tests → one immutable artifact
  → staging → approval gate → production.
- Challenge: a bad release reached staging; roll it back, prove ≥ 2 healthy load balancer targets
  and ≥ 1 running instance, and record the error rate after rollback below 1%.
- Grading rules: `vpc_exists`, `subnet_count >= 2`, `instance_running >= 2`,
  `load_balancer_healthy_targets >= 2`, `instance_reachable`, `security_group_allows` (443),
  `metric_below` (error_rate < 1).
- Teaches: immutable artifacts, staging that mirrors production, approval with evidence, rollback
  planned before the deploy, expand-then-contract database changes, deployment monitoring.

### advanced-production-rollout — Production Rollout: Canary, Rollback & Alerting (350 XP)

- Prerequisites: `devops`, `monitoring`.
- Guided lab: `adv-rollout` (30 min) — **lite-launchable**.
- Rollout: canary traffic steps (1% → 10% → 50% → 100%) gated on numeric health gates; a failed
  gate stops the rollout automatically and returns traffic.
- Challenge: the canary breached its health gate; roll it back and prove the error rate is below 1%
  afterwards, with ≥ 2 healthy targets.
- Grading rules: `vpc_exists`, `subnet_count >= 2`, `instance_running >= 2`,
  `load_balancer_healthy_targets >= 2`, `instance_reachable`, `security_group_allows` (443),
  `metric_below` (error_rate < 1).
- Teaches: canary vs blue-green vs rolling, health gates, alerting that compares the canary against
  the old version, rollout runbooks.

## 3. Tough troubleshooting — 6 new scenarios

All six use only fault kinds the Rust engine can inject (`route-blackhole`, `latency`, `loss`,
`dns`) and only validation rules the engine implements. Start each one on a launched lab with
`POST /api/v1/labs/{labId}/break-fix/start` and `{"scenario_id": "<id>"}`.

| Scenario | Fault | Target | Grading rules | XP |
| --- | --- | --- | --- | --- |
| `adv-cascade-checkout-outage` — checkout cascades, logs disagree | latency 5000 ms | load_balancer | `load_balancer_healthy_targets >= 2`, `instance_reachable`, `metric_below` (error_rate < 1) | 250 |
| `adv-multi-az-route-blackhole` — one zone goes dark | route-blackhole | route_table | `route_to_igw`, `subnet_count >= 4`, `instance_reachable` | 250 |
| `adv-dns-stale-records` — DNS answers with yesterday's addresses | dns (stale_records) | vpc_dns_resolver | `dns_resolves` × 2, `instance_reachable` | 220 |
| `adv-intermittent-packet-loss` — flaky egress, one failure in four | loss 25% egress | nat_gateway | `nat_gateway_exists`, `route_to_nat`, `instance_reachable` | 240 |
| `adv-db-replica-lag` — reads return yesterday's data | latency 8000 ms | database | `instance_running >= 1`, `instance_reachable`, `metric_below` (replication_lag_seconds < 5) | 240 |
| `adv-hybrid-dns-split-brain` — hybrid DNS answers differently per side | dns (split_brain) | route53_resolver | `dns_resolves` × 2, `instance_reachable` | 250 |

## 4. Job-ready material

### Interview walkthroughs (8) — `curriculum/advanced/interviews.json`

Each has a problem, an expected approach, a grading rubric (criterion + what good looks like),
common pitfalls, and related Advanced projects. There is no single correct answer.

| ID | Title | Role focus |
| --- | --- | --- |
| `interview-design-ha-web` | Design a highly available web application | cloud engineer |
| `interview-debug-outage` | Debug a production outage with conflicting logs | devops engineer |
| `interview-migration-plan` | Plan an on-prem to cloud migration | solutions architect |
| `interview-cost-doubled` | The cloud bill doubled overnight | cloud engineer |
| `interview-cicd-design` | Design a CI/CD pipeline for a regulated team | devops engineer |
| `interview-security-incident` | Respond to a compromised cloud instance | cloud engineer |
| `interview-scale-traffic-spike` | Handle a 10x traffic spike | solutions architect |
| `interview-dr-plan` | Defend a disaster recovery plan | solutions architect |

### Skills checklist — `curriculum/advanced/skills-checklist.json`

Three JD archetypes (composite sketches of common postings, not copies of any single listing),
each with 6 skills mapped to the Advanced projects that teach them:

- **Cloud Engineer** — multi-tier VPC design, backup/failover/DR, migration & right-sizing, bill
  attribution & guardrails, incident response, production networking.
- **DevOps Engineer** — CI/CD pipelines, staging→production promotion, canary/blue-green/rolling,
  rollback, deployment monitoring & runbooks, incident debugging.
- **Solutions Architect** — HA design & tradeoffs, RTO/RPO & DR, migration planning, cost/reliability
  tradeoffs, deployment & operational models, security design.

### Portfolio artefacts

One Markdown artefact per completed Advanced project, generated by
`POST /api/v1/advanced/portfolio/{project}` (see `ROUTES.md`). It contains the architecture summary
(modules, lessons, objectives, lab used), deployment evidence CloudForge recorded (labs, challenges,
break-and-fix solves, best assessment score, total XP, grading rules), and a footer stating it is a
portfolio record, not a certificate, with no claim of AWS certification. A project that is not
completed returns 409 and no artefact.
