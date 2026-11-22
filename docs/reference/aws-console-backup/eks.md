# EKS — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `eks` |
| Name | EKS |
| Category | Compute |
| Registry route | `/services/eks` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | Yes — frontend/src/pages/EksConsole.tsx, route `/services/eks` (concepts learning path; cluster creation disabled) |
| Resource types | cluster, node-group |
| Learning tracks linked | none linked |
| Build-later scope | **large** |
| Lite feasibility | see §5 |

## 1. Purpose

EKS is the managed Kubernetes service: a managed control plane (API server, etcd) plus node groups (EC2) or Fargate profiles, add-ons (VPC CNI, CoreDNS, kube-proxy), and IAM Roles for Service Accounts. A learner meets it as the industry-standard container orchestrator — the service most real-world container jobs assume — and as the place where Kubernetes concepts (Pods, Deployments, Services, Ingresses, ConfigMaps) become hands-on.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Clusters, Node groups (per cluster), Add-ons (per cluster), Fargate profiles, Insights.
**Home — Clusters list:** columns Cluster name, Status (Active / Creating / Failed / Deleting), Kubernetes version, Platform version, Created. Filters: search, status.
**Cluster detail tabs:** Overview (endpoint, certificate authority data, OIDC provider, cluster IAM role, logging, networking: VPC/subnets/security groups), Compute (node groups table: name, status, instance type, desired/min/max, AMI type; Fargate profiles table), Networking (cluster IP family, service IPv4 range, endpoint access public/private), Add-ons (table: name, version, status, update available), Access (access entries: principal, access policies), Logs (control-plane log types enabled), Update history.
**Wizard — Create cluster:** 1. Cluster configuration (name, Kubernetes version, cluster service role). 2. Networking (VPC, subnets, security groups, cluster endpoint access public/private, IP family). 3. Logging (API/audit/authenticator/controllerManager/scheduler log types). 4. Add-ons (VPC CNI, CoreDNS, kube-proxy versions). 5. Review and create. Then: Add node group wizard (name, node IAM role, AMI type, instance type, disk size, desired/min/max size, subnets, labels, taints) and Add-on wizard.
**Actions:** Create cluster, Add node group, Add add-on, Update Kubernetes version, Update add-on, Delete cluster, Update cluster access, Create Fargate profile.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Managed Kubernetes clusters — UNSUPPORTED by Phase 4 decision; Kubernetes concepts are taught on the ECS runtime._
- **What the current console shows (registry mirrors note):** Cluster list / Node groups / Add-ons screens are NOT built — the console is a Kubernetes-concepts learning path with working ECS exercises
- **How it differs / what is missing (registry differs note):** Phase 4 decision: EKS stays UNSUPPORTED. No local k3s/kind cluster is built — it cannot be verified in this sandbox (no Docker/KVM), and a managed control plane would break lite mode. The console maps Pod/Deployment/Service/ConfigMap/Namespace/Job onto the ECS/ELB/CodeBuild runtimes that do exist, with working try-it actions.
- **Console today:** Yes — frontend/src/pages/EksConsole.tsx, route `/services/eks` (concepts learning path; cluster creation disabled). Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend: explicit UNSUPPORTED decision — do not build silently; the decision is recorded, build only as a gated phase.** Current: Phase 4 decision — EKS stays UNSUPPORTED; no local k3s/kind cluster is built because it cannot be verified in the sandbox (no Docker/KVM) and a managed control plane would break lite mode. The console (EksConsole.tsx) shows the decision + reason + a 6-concept learning path (Pod, Deployment, Service, ConfigMap/Secret, Namespace, Job/CronJob) with working ECS equivalents; Create cluster is disabled. `GET /api/v1/aws/eks/info` returns the decision (backend/internal/phase4/phase4.go `EKSDecision`, `K8sLearningPath`).
To build later (full mode only, gated behind a capability check):
1. **Runtime:** k3s (single binary, smallest footprint) or kind (needs Docker) on the host, started by the host agent; the API talks to the cluster through the Kubernetes API (client-go) with a service-account token stored server-side. The host agent must report k3s availability in its capability probe before the EKS console enables Create.
2. **Backend endpoints (new):** `POST /api/v1/aws/eks/clusters` (create cluster = start/configure k3s + record), `GET /api/v1/aws/eks/clusters`, `GET /api/v1/aws/eks/clusters/{id}`, `DELETE /api/v1/aws/eks/clusters/{id}`, node groups `POST/GET/DELETE /api/v1/aws/eks/clusters/{id}/nodegroups` (a node group = a set of k3s agent nodes or, honestly, a label on the single-node cluster), add-ons `POST/GET /api/v1/aws/eks/clusters/{id}/addons` (install CoreDNS/metrics-server via kubectl apply of pinned manifests), `GET /api/v1/aws/eks/clusters/{id}/workloads` (list Pods/Deployments/Services via the API).
3. **Kubernetes objects console:** Pods/Deployments/Services/ConfigMaps/Namespaces tables with the real kubectl-equivalent data; create a Deployment from an ECR image (image recorded, pulled by k3s from the local registry); expose a Service via a NodePort and show the URL.
4. **IAM mapping:** access entries map CloudForge roles to Kubernetes RBAC (ClusterRoleBindings) — admin-only.
**Data model:** Postgres `pX_eks_clusters` (id, name, k8s_version, status, endpoint, ca_data, node_count, created_at) + `pX_eks_nodegroups` (id, cluster_id, name, instance_type, desired, min, max, status) + `pX_eks_addons` (id, cluster_id, name, version, status).
**Labs / lessons:** extend the existing 6-concept learning path into real k3s exercises: deploy an nginx Deployment, expose it as a Service, scale it, roll it back. Keep the ECS equivalents as the lite-mode path.
**Lite feasibility:** No — k3s needs a real host with container runtime and memory headroom; EKS is full-mode only and stays disabled in lite with the Phase 4 reason. The concepts learning path is the lite offering.

## 5. Lite feasibility and scope

- **Scope: large.** k3s-backed clusters via the host agent (full mode only, capability-gated); ECS concepts path stays the lite offering.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `eks`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
