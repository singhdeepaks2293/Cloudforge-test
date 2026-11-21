# CloudForge curriculum content (Milestone 8)

Structured, machine-readable course content. This directory holds **content
only** — no API code, no seed code, no frontend code. A later step maps this
content into the database and serves it through the Go control plane.

## Layout

```
curriculum/
  README.md                     this file
  validate.py                   content validator (run: python3 validate.py)
  achievements.yaml             15 achievements (id, title, description, trigger, xp)
  break-and-fix/                6 scenarios, one YAML file each
  fundamentals/fundamentals.yaml
  global-infrastructure/global-infrastructure.yaml
  iam/iam.yaml
  ec2/ec2.yaml
  networking/networking.yaml
  storage/storage.yaml
  databases/databases.yaml
  serverless/serverless.yaml
  messaging/messaging.yaml
  monitoring/monitoring.yaml
  containers/containers.yaml
  terraform/terraform.yaml
  devops/devops.yaml
  architecture/architecture.yaml
```

## Track file schema

Each `<track>/<track>.yaml` is one track:

| Field | Meaning |
|---|---|
| `id` | Track slug; must match the directory and file name |
| `title`, `description` | Display text |
| `difficulty` | `beginner` \| `intermediate` \| `advanced` |
| `estimated_hours` | Positive number |
| `learning_objectives` | List of outcome statements |
| `modules` | List of `{id, title, lessons}` |
| `modules[].lessons[]` | `{id, title, theory, key_points, architecture_diagram_mermaid?}` — `theory` is Markdown prose, **>= 150 words**, written for a learner, not placeholder text |
| `guided_lab` | `{lab_definition_id, title, tasks, validation_rules}` — tasks are human-readable steps; rules are machine-checkable (see vocabulary below) |
| `challenge` | `{title, task, validation_rules}` — independent work, same rule vocabulary |
| `assessment` | >= 5 multiple-choice questions, each `{question, options[4], correct_answer (0-3), explanation}` |
| `xp_reward` | XP granted on track completion |
| `prerequisites` | List of track ids that must be completed first |

## Consistency with the Milestone 1 seeded catalogue

The M1 in-memory catalogue (`backend/internal/courses/courses.go`) seeds three
courses. This content extends them without contradicting them:

- `vpc-fundamentals` (6 lessons) → `networking` track: same six lesson
  topics, in the same order, plus the same concepts taught in depth.
- `iam-security` (5 lessons) → `iam` track: same five lesson topics, in the
  same order, plus an additional module on credentials and troubleshooting.
- `ec2-basics` (5 lessons) → `ec2` track: same five lesson topics, in the
  same order.

The other eleven tracks are new. When the M2+ seed step lands, the seeded
courses should be replaced by (or mapped onto) these tracks; the M1 lesson
titles are preserved as lesson titles in the corresponding tracks.

## Validation-rule vocabulary

Validation rules are objects `{rule, params?, description}`. The `rule`
name is the machine-checkable predicate; `params` carry its arguments;
`description` is shown to the learner. Rules inspect **real infrastructure
state** in the lab (VPCs, routes, security groups, instances, buckets,
queues, functions, metrics), never the text of the learner's answer.

| Rule | Params | Checks |
|---|---|---|
| `lab_exists` | `lab_definition_id` | A lab session exists for this definition |
| `vpc_exists` | `cidr?` | A VPC exists (optionally with this CIDR) |
| `subnet_count` | `operator`, `value` | Subnet count satisfies the comparison (`>=`, `==`, …) |
| `subnet_exists` | `cidr?`, `az?` | A matching subnet exists |
| `subnet_az_spread` | `min_zones` | Subnets span at least this many AZs |
| `route_to_igw` | `subnet_type` | Subnets of this type route `0.0.0.0/0` to the internet gateway |
| `route_to_nat` | `subnet_type` | Subnets of this type route `0.0.0.0/0` to a NAT gateway |
| `no_route_to_internet` | `subnet_type` | Subnets of this type have no internet route |
| `nat_gateway_exists` | `min_count` | At least this many NAT gateways exist and are available |
| `security_group_allows` | `port`, `source` | A security group allows this port from this source |
| `security_group_allows_ssh` | `port`, `source` | SSH (port 22) allowed from this source |
| `security_group_denies` | `port`, `source` | This port is denied from this source |
| `instance_running` | `count`, `operator` | Instances in the running state satisfy the comparison |
| `instance_reachable` | `protocol`, `port` | An instance answers on this protocol/port (ssh/http) |
| `volume_attached` | `size_gib`, `operator` | A data volume of at least this size is attached |
| `bucket_exists` | `name` | An object-storage bucket exists |
| `bucket_versioning_enabled` | `bucket` | Versioning is on for this bucket |
| `bucket_encryption_enabled` | `bucket` | Encryption is on for this bucket |
| `bucket_not_public` | `bucket` | The bucket is not publicly accessible |
| `object_exists` | `bucket`, `key` | An object with this key exists in this bucket |
| `cdn_serves_site` | `distribution` | The distribution serves the site over HTTPS |
| `database_exists` | `engine` | A database instance of this engine exists |
| `database_private` | — | The database has no public endpoint |
| `database_backup_enabled` | `retention_days` | Automated backups on with this retention |
| `table_exists` | `database`, `table` | A table exists in this database |
| `dynamodb_partition_key` | `table`, `key` | The table's partition key is this attribute |
| `lambda_exists` | `function_name` | The function exists |
| `lambda_trigger` | `function_name`, `source`, `bucket?` | The function is triggered by this source |
| `lambda_invoked` | `function_name`, `min_count` | The function ran at least this many times |
| `lambda_schedule` | `function_name`, `schedule` | The function is on this EventBridge schedule |
| `queue_exists` | `name` | The queue exists |
| `queue_dlq_configured` | `queue`, `max_receives` | A dead-letter queue is configured after this many receives |
| `message_delivered` | `queue`, `min_count` | At least this many messages were delivered |
| `dlq_has_message` | `queue`, `min_count` | The dead-letter queue received messages |
| `topic_exists` | `name` | The SNS topic exists |
| `topic_subscribers` | `topic`, `min_count` | The topic has at least this many subscribers |
| `dashboard_exists` | `name` | The monitoring dashboard exists |
| `metric_alarm_exists` | `metric`, `threshold` | An alarm on this metric/threshold exists |
| `alarm_fired` | `alarm` | The alarm fired during the scenario |
| `alarm_cleared` | `alarm` | The alarm cleared after the fix |
| `metric_below` | `metric`, `threshold` | The metric is currently below the threshold |
| `container_image_exists` | `image`, `tag` | The image:tag exists in the registry |
| `container_running` | `service`, `min_count` | At least this many containers of the service run |
| `load_balancer_healthy_targets` | `service`, `min_count` | The load balancer has this many healthy targets |
| `container_az_spread` | `service`, `min_zones` | Service tasks span at least this many AZs |
| `terraform_state_exists` | `workspace` | Terraform state exists for this workspace |
| `terraform_no_drift` | `workspace` | A fresh plan shows no drift from state |
| `pipeline_exists` | `name` | The CI/CD pipeline exists |
| `pipeline_stage_passed` | `pipeline`, `stage` | The named pipeline stage passed |
| `deployment_strategy` | `service`, `strategy` | The service deploys with this strategy |
| `canary_healthy` | `service` | Canary metrics stayed healthy during rollout |
| `canary_rolled_back` | `service` | The bad canary was rolled back |
| `architecture_doc_exists` | `kind` | A document of this kind exists |
| `adr_count` | `operator`, `value` | ADR count satisfies the comparison |
| `cost_estimate_exists` | — | A monthly cost estimate exists |
| `iam_role_exists` | `role_name` | The IAM role exists |
| `iam_policy_allows` | `role`/`user`, `action`, `resource` | The policy allows this action on this resource |
| `iam_policy_denies` | `role`/`user`, `action` | The policy does not allow this action |
| `iam_access_key_absent` | `user` | No long-lived access key remains for this user |
| `dns_resolves` | `name` | The name resolves from inside the VPC |

The validator (`validate.py`) rejects any rule name outside this table, so
the vocabulary is the contract between content authors and the future
validation engine (Milestone 6).

## Break-and-fix scenarios

`break-and-fix/<id>.yaml` — one file per scenario:

| Field | Meaning |
|---|---|
| `id`, `title`, `symptom` | What the learner sees (the incident as reported) |
| `injected_fault` | `{kind, target, parameters}` — `kind` must be one of the Rust engine fault model kinds: `route-blackhole`, `latency`, `loss`, `dns`. `target` is the resource the fault is injected into (e.g. `route_table`, `nat_gateway`, `security_group`, `vpc_dns_resolver`, `subnet`); `parameters` carry the fault details |
| `diagnosis_hints` | Ordered, escalating hints — early ones are vague, later ones point at the cause |
| `fix_steps` | The repair procedure, in order |
| `validation_rules` | Same vocabulary as labs; the scenario is solved when all pass |
| `xp_reward` | XP granted on solving |

## Achievements

`achievements.yaml` is `{achievements: [...]}` with `{id, title,
description, trigger, xp}`. Trigger types:

| Trigger type | Extra fields | Meaning |
|---|---|---|
| `lessons_completed` | `operator`, `value` | Lesson count reaches value |
| `labs_completed` | `operator`, `value` | Lab count reaches value |
| `challenges_completed` | `operator`, `value` | Challenge count reaches value |
| `break_fix_solved` | `operator`, `value` | Scenarios solved reaches value |
| `track_completed` | `track` | This track completed |
| `tracks_completed` | `difficulty`, `operator`, `value` | Tracks of this difficulty completed reaches value |
| `assessment_score` | `operator`, `value` | An assessment score reaches value (percent) |
| `xp_total` | `operator`, `value` | Total XP reaches value |
| `streak_days` | `operator`, `value` | Consecutive learning days reaches value |

## How a track maps to the database seed + API (later step)

The later seeding step (not this milestone) should:

1. Create one `courses` row per track (using `id` as the stable key, so the
   M1 seeded courses map onto `networking`, `iam`, `ec2` by their existing
   ids or by an explicit alias table).
2. Create one `modules` row per module and one `lessons` row per lesson,
   preserving `id` ordering within the parent.
3. Create one `labs` definition per `guided_lab.lab_definition_id` and one
   per challenge (challenge lab definitions get a `challenge-` prefix), with
   `validation_rules` stored as JSONB.
4. Create one `assessments` row per track question set, with `options` and
   `correct_answer` stored as JSONB.
5. Create one `achievements` row per entry in `achievements.yaml`, with the
   `trigger` stored as JSONB.
6. Create one `break_fix_scenarios` row per file in `break-and-fix/`, with
   `injected_fault` stored as JSONB and passed to the Rust engine's
   `InjectFault` RPC when the scenario starts.

The API then serves tracks/lessons/assessments from the database; the YAML
files remain the source of truth for content edits, and `validate.py` is the
gate that runs in CI before any seed import.

## Validation

```bash
python3 curriculum/validate.py          # from the repo root, or:
python3 validate.py                     # from inside curriculum/
```

Requires PyYAML (`pip install pyyaml`). It checks: every YAML file parses;
track schema and difficulty values; >= 3 lessons per track; theory >= 150
words per lesson; unique lesson ids; lab and challenge each have >= 1
validation rule from the known vocabulary; assessment has >= 5 questions
with exactly 4 options and exactly one correct answer; prerequisites
reference existing tracks; break-and-fix fault kinds are in the Rust engine
fault model; >= 12 achievements with known trigger types. Exits non-zero
on any error.
