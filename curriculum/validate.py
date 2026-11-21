#!/usr/bin/env python3
"""Validate CloudForge curriculum content.

Usage: python3 curriculum/validate.py [curriculum_dir]

Checks, per track YAML:
  - parses as YAML and has the required top-level fields
  - difficulty is beginner|intermediate|advanced
  - at least 3 lessons, each with >= 150 words of theory and >= 1 key point
  - every lesson id is unique within the track
  - guided lab and challenge each have >= 1 validation rule
  - assessment has >= 5 questions, each with exactly 4 options and exactly
    one correct answer (correct_answer in 0..3) plus an explanation
  - prerequisites reference tracks that exist
  - every validation rule name is in the known vocabulary (see README.md)
  - mermaid diagram present on lessons where one is declared relevant
    (warn, do not fail, if a lesson lacks one)
Break-and-fix scenarios:
  - required fields, injected_fault.kind in the Rust engine fault model
    (route-blackhole | latency | loss | dns), >= 2 diagnosis hints,
    >= 1 fix step, >= 1 validation rule, xp_reward > 0
Achievements:
  - at least 12 entries, unique ids, known trigger type, xp > 0

Requires PyYAML. Exits non-zero on any error; warnings are printed
but do not fail the run.
"""
import os
import sys

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML is required (import yaml failed). "
          "Install it with: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

BASE = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))

KNOWN_RULES = {
    "lab_exists", "vpc_exists", "subnet_count", "subnet_exists",
    "subnet_az_spread", "route_to_igw", "route_to_nat",
    "no_route_to_internet", "nat_gateway_exists",
    "security_group_allows", "security_group_allows_ssh",
    "security_group_denies", "instance_running", "instance_reachable",
    "volume_attached", "bucket_exists", "bucket_versioning_enabled",
    "bucket_encryption_enabled", "bucket_not_public", "object_exists",
    "cdn_serves_site", "database_exists", "database_private",
    "database_backup_enabled", "table_exists",
    "dynamodb_partition_key", "lambda_exists", "lambda_trigger",
    "lambda_invoked", "lambda_schedule", "queue_exists",
    "queue_dlq_configured", "message_delivered", "dlq_has_message",
    "topic_exists", "topic_subscribers", "dashboard_exists",
    "metric_alarm_exists", "alarm_fired", "alarm_cleared",
    "metric_below", "container_image_exists", "container_running",
    "load_balancer_healthy_targets", "container_az_spread",
    "terraform_state_exists", "terraform_no_drift",
    "pipeline_exists", "pipeline_stage_passed", "deployment_strategy",
    "canary_healthy", "canary_rolled_back", "architecture_doc_exists",
    "adr_count", "cost_estimate_exists",
    "iam_role_exists", "iam_policy_allows", "iam_policy_denies",
    "iam_access_key_absent", "dns_resolves",
}

KNOWN_FAULTS = {"route-blackhole", "latency", "loss", "dns"}

KNOWN_TRIGGERS = {
    "lessons_completed", "labs_completed", "track_completed",
    "break_fix_solved", "challenges_completed", "assessment_score",
    "xp_total", "streak_days", "tracks_completed",
}

TRACK_FIELDS = {"id", "title", "description", "difficulty",
                "estimated_hours", "learning_objectives", "modules",
                "guided_lab", "challenge", "assessment", "xp_reward",
                "prerequisites"}
DIFFICULTIES = {"beginner", "intermediate", "advanced"}

errors, warnings = [], []


def err(msg):
    errors.append(msg)


def warn(msg):
    warnings.append(msg)


def load(path):
    with open(path) as f:
        return yaml.safe_load(f)


def check_rules(rules, where):
    if not isinstance(rules, list) or not rules:
        err(f"{where}: needs >= 1 validation rule")
        return
    for r in rules:
        if not isinstance(r, dict) or "rule" not in r:
            err(f"{where}: malformed validation rule {r!r}")
            continue
        if r["rule"] not in KNOWN_RULES:
            err(f"{where}: unknown validation rule '{r['rule']}'")
        if not r.get("description"):
            err(f"{where}: rule '{r['rule']}' needs a description")


def check_track(path, known_ids):
    t = load(path)
    name = os.path.basename(path)
    for field in TRACK_FIELDS:
        if field not in t:
            err(f"{name}: missing field '{field}'")
    if t.get("difficulty") not in DIFFICULTIES:
        err(f"{name}: bad difficulty {t.get('difficulty')!r}")
    if not isinstance(t.get("estimated_hours"), (int, float)) or t.get("estimated_hours", 0) <= 0:
        err(f"{name}: estimated_hours must be a positive number")
    if not isinstance(t.get("learning_objectives"), list) or not t["learning_objectives"]:
        err(f"{name}: learning_objectives must be a non-empty list")
    if not isinstance(t.get("xp_reward"), int) or t.get("xp_reward", 0) <= 0:
        err(f"{name}: xp_reward must be a positive integer")

    lesson_ids, lesson_count = set(), 0
    for mod in t.get("modules") or []:
        if not mod.get("id") or not mod.get("title"):
            err(f"{name}: module missing id/title: {mod!r}")
        for les in mod.get("lessons") or []:
            lesson_count += 1
            lid = les.get("id")
            if not lid:
                err(f"{name}: lesson missing id: {les.get('title')!r}")
            elif lid in lesson_ids:
                err(f"{name}: duplicate lesson id '{lid}'")
            else:
                lesson_ids.add(lid)
            words = len((les.get("theory") or "").split())
            if words < 150:
                err(f"{name}/{lid}: theory is {words} words, needs >= 150")
            if not isinstance(les.get("key_points"), list) or not les["key_points"]:
                err(f"{name}/{lid}: needs >= 1 key point")
            if not les.get("architecture_diagram_mermaid"):
                warn(f"{name}/{lid}: no mermaid diagram")
    if lesson_count < 3:
        err(f"{name}: only {lesson_count} lessons, needs >= 3")

    for section in ("guided_lab", "challenge"):
        sec = t.get(section) or {}
        if not sec.get("lab_definition_id") and section == "guided_lab":
            err(f"{name}: guided_lab needs lab_definition_id")
        if not sec.get("title"):
            err(f"{name}: {section} needs a title")
        check_rules(sec.get("validation_rules"), f"{name}/{section}")
    if not (t.get("challenge") or {}).get("task"):
        err(f"{name}: challenge needs a task description")

    assess = t.get("assessment") or []
    if len(assess) < 5:
        err(f"{name}: assessment has {len(assess)} questions, needs >= 5")
    for i, q in enumerate(assess):
        opts = q.get("options") or []
        if len(opts) != 4:
            err(f"{name}: question {i} has {len(opts)} options, needs exactly 4")
        ca = q.get("correct_answer")
        if not isinstance(ca, int) or not 0 <= ca < 4:
            err(f"{name}: question {i} has invalid correct_answer {ca!r}")
        if not q.get("question") or not q.get("explanation"):
            err(f"{name}: question {i} needs question text and explanation")

    for pre in t.get("prerequisites") or []:
        if pre not in known_ids:
            err(f"{name}: prerequisite '{pre}' is not a known track")
        if pre == t.get("id"):
            err(f"{name}: track lists itself as a prerequisite")
    return t


def main():
    track_files = []
    for entry in sorted(os.listdir(BASE)):
        p = os.path.join(BASE, entry)
        if os.path.isdir(p) and entry not in ("break-and-fix",):
            for fn in sorted(os.listdir(p)):
                if fn.endswith(".yaml"):
                    track_files.append(os.path.join(p, fn))
    known_ids = {os.path.splitext(os.path.basename(p))[0] for p in track_files}
    total_lessons = total_questions = 0
    for p in track_files:
        t = check_track(p, known_ids)
        total_lessons += sum(len(m.get("lessons") or []) for m in t.get("modules") or [])
        total_questions += len(t.get("assessment") or [])

    baf_dir = os.path.join(BASE, "break-and-fix")
    baf_files = sorted(f for f in os.listdir(baf_dir) if f.endswith(".yaml")) \
        if os.path.isdir(baf_dir) else []
    if len(baf_files) < 5:
        err(f"break-and-fix: only {len(baf_files)} scenarios, needs >= 5")
    for fn in baf_files:
        s = load(os.path.join(baf_dir, fn))
        where = f"break-and-fix/{fn}"
        for field in ("id", "title", "symptom", "injected_fault",
                      "diagnosis_hints", "fix_steps", "validation_rules",
                      "xp_reward"):
            if field not in s:
                err(f"{where}: missing field '{field}'")
        fault = s.get("injected_fault") or {}
        if fault.get("kind") not in KNOWN_FAULTS:
            err(f"{where}: injected_fault.kind {fault.get('kind')!r} not in "
                f"Rust engine fault model {sorted(KNOWN_FAULTS)}")
        if not fault.get("target"):
            err(f"{where}: injected_fault needs a target")
        if len(s.get("diagnosis_hints") or []) < 2:
            err(f"{where}: needs >= 2 diagnosis hints")
        if len(s.get("fix_steps") or []) < 1:
            err(f"{where}: needs >= 1 fix step")
        check_rules(s.get("validation_rules"), where)
        if not isinstance(s.get("xp_reward"), int) or s.get("xp_reward", 0) <= 0:
            err(f"{where}: xp_reward must be a positive integer")

    ach_path = os.path.join(BASE, "achievements.yaml")
    if not os.path.exists(ach_path):
        err("achievements.yaml missing")
    else:
        ach = load(ach_path).get("achievements") or []
        if len(ach) < 12:
            err(f"achievements: only {len(ach)}, needs >= 12")
        seen = set()
        for a in ach:
            if a.get("id") in seen:
                err(f"achievements: duplicate id '{a.get('id')}'")
            seen.add(a.get("id"))
            if (a.get("trigger") or {}).get("type") not in KNOWN_TRIGGERS:
                err(f"achievements/{a.get('id')}: unknown trigger "
                    f"{(a.get('trigger') or {}).get('type')!r}")
            if (a.get("trigger") or {}).get("track") and \
                    a["trigger"]["track"] not in known_ids:
                err(f"achievements/{a.get('id')}: trigger track "
                    f"'{a['trigger']['track']}' is not a known track")
            if not isinstance(a.get("xp"), int) or a.get("xp", 0) <= 0:
                err(f"achievements/{a.get('id')}: xp must be a positive integer")

    for w in warnings:
        print(f"WARNING: {w}")
    if errors:
        print(f"\nFAILED: {len(errors)} error(s)")
        for e in errors:
            print(f"  ERROR: {e}")
        sys.exit(1)
    print(f"OK: {len(track_files)} tracks, {total_lessons} lessons, "
          f"{total_questions} assessment questions, {len(baf_files)} "
          f"break-and-fix scenarios, {len(seen) if os.path.exists(ach_path) else 0} "
          f"achievements, {len(warnings)} warning(s)")


if __name__ == "__main__":
    main()
