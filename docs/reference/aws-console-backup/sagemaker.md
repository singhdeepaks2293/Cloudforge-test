# SageMaker — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `sagemaker` |
| Name | SageMaker |
| Category | Machine Learning and AI |
| Registry route | `/services/sagemaker` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/MlConsoles.tsx `SageMakerConsole`, route `/services/sagemaker` |
| Resource types | notebook, model, endpoint |
| Learning tracks linked | databases |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

SageMaker is the managed machine-learning platform: notebook instances for exploration, training jobs that run your code on managed compute, a model registry, endpoints that serve predictions, hyperparameter tuning, processing jobs, and Ground Truth labelling. A learner meets it as the 'train and deploy a model without managing servers' service — the full ML lifecycle in one place — and as the contrast to Bedrock, where you use someone else's model instead of training your own.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Studio, Canvas, Notebook instances, Training jobs, Models, Endpoints, Hyperparameter tuning jobs, Processing jobs, Ground Truth, Feature Store.
**Home — Training jobs list:** columns Name, Status (Completed / InProgress / Failed / Stopped), Algorithm (built-in or custom), Created, Duration. Filters: status, search.
**Notebook instances list:** columns Name, Status (InService / Pending / Stopped / Failed), Instance type, URL (Open Jupyter link), Created. Detail: lifecycle config, permissions.
**Models list:** columns Name, Status, Created. Model detail: containers (image + model data URL), execution role.
**Endpoints list:** columns Name, Status (InService / Creating / Failed), Instance type, Model, Created. Detail: variants (model + traffic weight), invocation (test input → output), monitoring.
**Hyperparameter tuning jobs list:** columns Name, Status, Objective metric, Best training job, Created.
**Wizard — Create notebook instance:** name, instance type (ml.t3.medium etc.), IAM role, lifecycle configuration, git repos, tags.
**Wizard — Create training job:** job name, algorithm (built-in: Linear Learner, XGBoost, k-means; or custom container), input data (S3 channel), output (S3), instance type/count, hyperparameters (key/value table), stopping condition, tags.
**Wizard — Create model / endpoint:** model name, container image, model data URL, execution role; endpoint name, endpoint configuration (production variants: model + instance type + initial instance count + traffic weight).
**Actions:** Start/stop notebook, Create training job, Stop training job, Create model, Create endpoint, Invoke endpoint (test), Delete endpoint/model/job.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _ML notebooks, real training jobs, model registry and inference._
- **What the current console shows (registry mirrors note):** Notebook instances; Training jobs; Models; Endpoints; Invoke action
- **How it differs / what is missing (registry differs note):** Notebooks run cells in the sandboxed Python/Node subprocess (same as Lambda). Training is a real OLS linear regression in Go on a built-in or user-supplied tiny dataset (no scikit-learn here); metrics are computed from the data. Models are versioned JSON artifacts on disk; endpoints serve real predictions from stored weights. Linear models only; no GPU.
- **Console today:** Yes — frontend/src/pages/MlConsoles.tsx `SageMakerConsole`, route `/services/sagemaker`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — extend algorithms and endpoints, do not rebuild the training core.** Current: notebooks run cells in the sandboxed Python/Node subprocess (same as Lambda); training is a real OLS linear regression in Go on built-in or user-supplied tiny datasets (no scikit-learn); metrics are real MSE/RMSE/R²; models are versioned JSON artifacts on disk; endpoints serve real predictions from stored weights; linear models only, no GPU (backend/internal/httpapi/phase6.go `p6_sagemaker_*`, phase6.go `TrainLinear`). Console: MlConsoles.tsx `SageMakerConsole`. To build later, in order:
1. **More algorithms (real, small):** logistic regression (classification, same OLS-style normal-equations approach or gradient descent in Go), k-means clustering (Go implementation, k configurable), and a decision-tree stump — each with real metrics (accuracy/precision/recall for classification, inertia for k-means) computed from the data. Keep datasets tiny (≤1,000 rows) and the algorithms pure Go; never report a metric that was not computed.
2. **Hyperparameter tuning:** a tuning job = N training jobs over a grid of hyperparameters (e.g. learning rate × iterations), each recorded, with the best (by objective metric) highlighted; runs sequentially via the worker jobs table.
3. **Processing jobs:** a data-preparation job (Python script via the sandboxed runner, input S3/local → output S3/local) — the same runner as Glue ETL jobs; share the implementation.
4. **Endpoint variants + A/B traffic:** an endpoint configuration with two variants (model A 90% / model B 10%); Invoke routes by weight (record which variant answered). Reuse the CodeDeploy traffic-shifting pattern.
5. **Model registry:** model packages with approval status (PendingManualApproval / Approved / Rejected) and versioning; deployment only from Approved models (enforce in the endpoint creation path).
**Data model (exists, extend):** `p6_sagemaker_*`; add `pX_sagemaker_tuning` (id, name, objective_metric, status, best_job_id, created_at) + `pX_sagemaker_model_packages` (id, model_id, version, approval_status, created_at) + endpoint variants on the existing endpoint table.
**Labs / lessons:** add one lesson to the `databases`/ML area: train a logistic-regression classifier, tune it, register the model, deploy it behind an endpoint, and invoke it from a Lambda function. Reuse the existing SageMaker lesson.
**Lite feasibility:** Yes — pure Go + subprocess; datasets stay tiny; no GPU, no Docker.

## 5. Lite feasibility and scope

- **Scope: medium.** Logistic regression + k-means + tuning jobs + model registry on the existing OLS core.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `sagemaker`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
