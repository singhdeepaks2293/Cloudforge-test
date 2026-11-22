# How Advanced grading works

## The two grading layers

1. **The curriculum validator** (`curriculum/validate.py`) checks content shape: every track has
   ≥ 3 lessons with ≥ 150 words of theory, a guided lab and a challenge each with ≥ 1 validation
   rule, and an assessment with ≥ 5 questions of exactly 4 options and one correct answer.
   Break-and-fix scenarios need a fault kind in the engine model, ≥ 2 diagnosis hints, ≥ 1 fix
   step, ≥ 1 validation rule and positive XP. Achievements need known trigger types and positive XP.
2. **The validation engine** (`backend/internal/validation`) evaluates a track's or scenario's
   rules against real lab state when the learner asks for validation. A rule that passes is real
   evidence; a rule that is `unknown` fails the whole validation — the engine never fakes a pass.

## Rules the engine implements (the only ones Advanced uses)

`lab_exists`, `vpc_exists`, `subnet_count`, `subnet_exists`, `route_to_igw`, `route_to_nat`,
`nat_gateway_exists` (alias `nat_exists`), `security_group_allows`, `security_group_allows_ssh`,
`security_group_denies`, `no_route_to_internet`, `instance_running`, `instance_reachable`,
`load_balancer_healthy_targets`, `dns_resolves`, `metric_below`.

Every Advanced guided lab, challenge and scenario uses only these rules — verified by the test in
`backend/internal/advanced/advanced_test.go` (which mirrors this list) and by a scan of the YAML.
That is a deliberate difference from some older tracks, which use rules the engine does not
implement and can therefore never pass.

## Rules in the validator vocabulary that the engine does NOT implement

These exist in `curriculum/validate.py`'s `KNOWN_RULES` but return `unknown` from the engine, so
using one as a grading rule makes the lab unpassable:

- Pipeline / deployment: `pipeline_exists`, `pipeline_stage_passed`, `deployment_strategy`,
  `canary_healthy`, `canary_rolled_back`
- Alerting: `metric_alarm_exists`, `alarm_fired`, `alarm_cleared`
- Data: `database_exists`, `database_private`, `database_backup_enabled`, `table_exists`,
  `dynamodb_partition_key`
- Storage / objects: `bucket_exists`, `bucket_versioning_enabled`, `bucket_encryption_enabled`,
  `bucket_not_public`, `object_exists`, `cdn_serves_site`, `volume_attached`
- Serverless / messaging: `lambda_exists`, `lambda_trigger`, `lambda_invoked`, `lambda_schedule`,
  `queue_exists`, `queue_dlq_configured`, `message_delivered`, `dlq_has_message`, `topic_exists`,
  `topic_subscribers`
- Containers / networking extras: `container_image_exists`, `container_running`,
  `container_az_spread`, `subnet_az_spread`
- Terraform / docs / IAM: `terraform_state_exists`, `terraform_no_drift`, `architecture_doc_exists`,
  `adr_count`, `cost_estimate_exists`, `iam_role_exists`, `iam_policy_allows`, `iam_policy_denies`,
  `iam_access_key_absent`, `dashboard_exists`

**Follow-up for the coordinator (not this workstream's ownership):** implementing these in
`backend/internal/validation` would let the E2E tracks grade pipeline stages, canary health and
rollback directly instead of through proxies. Until then the proxies below are the grading.

## How pipeline / canary / rollback / alerting are graded today

The E2E tracks grade the *observable production outcome* of a pipeline or rollout, which the
engine can check:

- **Canary healthy** → `load_balancer_healthy_targets >= 2` (both versions behind the balancer and
  healthy) plus `metric_below` on the recorded error rate.
- **Rollback done** → `instance_running >= 1` and `load_balancer_healthy_targets >= 2` after the
  rollback, plus `metric_below` (error_rate < 1) proving the error rate returned to baseline.
- **Alerting** → the alert threshold is the `metric_below` threshold itself; the alerting rules and
  runbook are assessed by the track's assessment questions, not by a rule the engine cannot run.
- **Pipeline stages** → assessed by the track's assessment questions and by the guided lab's
  deployment outcome (staging + production instances running behind the balancer). There is no
  engine rule that inspects a pipeline, so no pipeline rule is claimed as grading.

## How `metric_below` works (and what the learner must do)

`metric_below` reads `config.metrics.<name>` from a lab resource — there is no live metrics
pipeline in the control plane. A `metric_below` grading rule therefore requires the learner to
record the measured value as a metric on the relevant resource (for example, the error rate on the
load balancer resource, or CPU utilisation on the instance resource). Every Advanced task that
uses `metric_below` says so in its task text, and the corresponding fix steps say so too. If no
metric is recorded, the rule grades `unknown` and the validation fails — honestly, not silently.

## Known grading quirks to know about

- `no_route_to_internet` passes only when **no** route table in the lab routes 0.0.0.0/0 to an
  internet or NAT gateway. It cannot grade the isolation of one subnet in a lab that also has
  public subnets. The incident-response track therefore grades containment with
  `security_group_denies` (per-group, checkable) and treats route removal as an ungraded action.
- `dns_resolves` for an internal name (e.g. `example.internal`) is checked against VPC DNS state;
  for an external name it falls back to the control-plane resolver. The Advanced scenarios use the
  same pair of names as the existing `dns-failure` scenario.
- `instance_reachable` passes only for a running instance with a recorded address (or
  `config.reachable: true`). A running instance with no address fails rather than passing on a guess.
- `load_balancer_healthy_targets` reads `healthy_targets` from the load balancer resource's config;
  the learner (or the provisioning flow) must record it.
