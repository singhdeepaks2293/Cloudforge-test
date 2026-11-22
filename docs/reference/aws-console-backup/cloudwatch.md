# CloudWatch — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `cloudwatch` |
| Name | CloudWatch |
| Category | Monitoring |
| Registry route | `/services/cloudwatch` |
| Availability today | partial |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/CloudWatchConsole.tsx, route `/services/cloudwatch` |
| Resource types | metric, alarm, dashboard |
| Learning tracks linked | monitoring |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

CloudWatch is the monitoring and observability service: metrics (time series), alarms (threshold rules with actions), logs (log groups, streams, Insights queries), dashboards, and events. A learner meets it whenever something needs watching — a lab's CPU, a queue's depth, an error rate — and it is the monitoring half of the 'observe, alert, act' loop that CloudTrail's audit history completes.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Dashboards, Metrics (All metrics, Explorer), Alarms, Logs (Log groups, Log Insights, Live tail), Events (EventBridge rules), Application Signals (Services, SLOs), Synthetics (Canaries), Network monitoring, Settings.
**Home — Dashboards:** dashboard grid (widgets: line graphs, numbers, text); Create dashboard wizard (name, widgets).
**Metrics — All metrics:** left tree (namespace → metric name → dimensions), graph composer (statistic Average/Sum/Maximum, period 1m/5m/1h, graphed metrics table with label/colour/stat/period), Add to dashboard, Create alarm from a graphed metric.
**Alarms list:** columns Alarm name, State (OK / ALARM / INSUFFICIENT_DATA), Threshold (e.g. `CPUUtilization > 80 for 1 datapoints within 5 minutes`), Actions (SNS/ EC2 / Auto Scaling), Last state change. Filters: state, search.
**Alarm detail tabs:** Details (metric, threshold, actions, history graph), History (state transitions with timestamps and reasons).
**Wizard — Create alarm:** 1. Select metric (namespace/metric/dimensions, statistic, period). 2. Conditions (threshold type static/anomaly, operator, value, datapoints to alarm, missing-data treatment). 3. Configure actions (notification SNS topic, Auto Scaling action, EC2 action). 4. Add name and description. 5. Preview and create.
**Logs — Log groups list:** columns Log group, Stored bytes, Retention, Created. **Log Insights:** query editor (query language: `fields @timestamp, @message | filter ... | stats count() by ...`), Run query, results table + visualisation.
**Actions:** Create dashboard, Create alarm, Put metric data, Create log group, Run Insights query, Create composite alarm.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: partial). Registry description: _Metrics, alarms and logs over the local control plane._
- **What the current console shows (registry mirrors note):** Metrics / Alarms / Logs / Dashboards left nav; metric graphs; Create-alarm wizard
- **How it differs / what is missing (registry differs note):** Metrics are local control-plane metrics (the API's real Prometheus series + live /readyz component health), not AWS CloudWatch; alarms are real threshold rules evaluated live against current values, but alarm definitions are stored in memory and lost on restart; there is no Logs Insights-style log query yet.
- **Console today:** Yes — frontend/src/pages/CloudWatchConsole.tsx, route `/services/cloudwatch`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — persist alarms and add logs, do not rebuild metrics.** Current: `GET /api/v1/monitoring` serves live Prometheus series + readiness as JSON; alarms are real threshold rules evaluated live but stored in memory and lost on restart; there is no Logs Insights-style query (docs/SERVICE_FIDELITY.md). Console: CloudWatchConsole.tsx. To build later, in order:
1. **Persist alarms:** Postgres `pX_cloudwatch_alarms` (id, name, metric_namespace, metric_name, dimensions jsonb, statistic, period_seconds, threshold, comparison_operator, datapoints_to_alarm, evaluation_periods, missing_data_treatment, actions jsonb, state, state_reason, state_updated_at, created_by, created_at) + history table `pX_cloudwatch_alarm_history` (state transitions). Evaluate on a worker tick (every 60 s) against the stored metric series instead of only on read; state transitions fire the configured action (SNS publish via the real SNS fan-out; Auto Scaling/EC2 actions are disabled with reasons until those lite services can act).
2. **Metric history:** store sampled series in Postgres (`pX_cloudwatch_metrics`, timestamped rows per metric+dimensions, 24 h retention in lite) so graphs show history, not just the live value. Keep the Prometheus `/metrics` feed as the source of the control-plane series.
3. **Logs:** log groups/streams backed by Postgres (`pX_cloudwatch_log_groups`, `pX_cloudwatch_log_events` with ingestion timestamp + message); ingest from the API's own structured logs and from Lambda/ECS task output (they already produce real output — wire it in). Log Insights = a small query parser supporting `fields | filter | sort | limit | stats count()/avg()/sum() by` over the stored events; anything else returns a clear "unsupported query" error, not a wrong answer.
4. **Dashboards:** persist dashboard widget layouts in Postgres (`pX_cloudwatch_dashboards`) instead of only in the frontend; widgets: line graph (metric), number (latest value), text.
**Data model:** tables above; metric ingestion reuses the existing Prometheus series where possible.
**Labs / lessons:** add one lesson to the `monitoring` track: create an alarm on lab CPU, trigger it, watch the SNS notification; run a Log Insights query over Lambda invocation logs. Reuse the existing CloudWatch lesson structure.
**Lite feasibility:** Yes — Postgres-backed alarms/logs need no Docker/KVM. Keep retention short in lite (24 h metrics, 7 days logs) and cap ingestion rate.

## 5. Lite feasibility and scope

- **Scope: medium.** Persisted alarms + metric history + Postgres log groups with a small Insights query language.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `cloudwatch`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
