# CloudForge Known Limitations

Honest, consolidated limitations. Nothing here is a placeholder for
work that was claimed done — each item says what is missing, why,
and what to do instead. Phase-specific detail lives in
`docs/IMPLEMENTATION_STATUS.md` §10–§16 and in `MILESTONE_LOG.md`.

## Sandbox (this environment, not the product)

- No Docker, KVM, `/dev/kvm`, libvirt, or nginx binary. Unverified
  here: `docker compose build/up/config`, real KVM/libvirt VM
  provisioning, the Rust real-netlink/nftables backend (compiles;
  default is SIMULATED), Terraform container execution (honest 503
  here), nginx config syntax, Grafana dashboards against live
  Prometheus, and the TLS compose/nginx path (cert generation
  itself was verified with openssl). Verified here instead: Go/Rust/
  frontend tests, E2E, live lite lab provisioning, curriculum
  seeding, launch/destroy flows.
- No MinIO binary obtainable (dl.min.io returns 410) — S3 object
  flows unit-test + live-503 only; Firehose S3 delivery and the
  Inspector S3 check fail/skip honestly here.
- No sklearn — SageMaker training is pure-Go OLS linear regression
  on tiny built-in datasets, labelled as such.
- No sshd/sftp-server — Transfer Family UNSUPPORTED by decision.
- Frontend `:8080` is verified with Vite preview of the production
  build, not the Docker nginx image (no Docker here).

## Security (Phase 7 residuals)

- **mTLS for the engine is not built.** Token auth is in place;
  mTLS (generated CA + client certs at deploy time) is the next step.
- **ECR `registry:2` has no auth** (local-only). Do not expose it.
- **Terraform bridge mode only approximates isolation.** Use
  `CLOUDFORGE_TF_NETWORK_MODE=isolated` (`--network none`) for
  untrusted HCL; in isolated mode the emulator is unreachable and
  plans needing it fail honestly.
- **The API port must not be published to the open internet.** The
  trusted-proxy rule assumes only nginx (or loopback) sets
  `X-Forwarded-For`.
- **Pre-Phase-7 tokens cannot be individually revoked** (no `jti`).
  They expire within 12 h; rotating `CLOUDFORGE_JWT_SECRET`
  invalidates all of them at once.
- **The `Authorization: Bearer` header path still works** for API
  clients/tests during the cookie transition; the planned follow-up
  is to make it API-client-only.
- **Audit cleanup without an export directory still deletes.**
  If `AuditExportDir` is unset, the worker logs a warning and
  deletes 90-day-old rows without archiving (documented; in the
  normal worker startup the directory is set).
- Full-mode compose resource limits were added in Phase 7 but have
  never been run (no Docker here) — sane on paper, watch
  `docker stats` on first real deployment.

## Fidelity (by design, not bugs)

- The Rust engine's real rtnetlink/nftables backend compiles but is
  unverified; the default backend is SIMULATED.
- No engine peering/Transit Gateway RPC — peering/TGW are SIMULATED
  with real route-table entries in lab state, not kernel peering.
- DMS is full-load only (≤5000 rows/table), no CDC; DataSync local
  paths are confined to the workdir; WAF rules are evaluated only
  via `/evaluate`, not inline in the API request path; Flow Logs are
  engine-telemetry snapshots, not per-packet flows; KMS/Secrets are
  durable local envelope encryption, not AWS KMS/HSM; Cost Explorer
  is educational estimates, never an AWS bill; Bedrock is a SIMULATED
  template playground, no real model, no paid third-party API.
- EKS is UNSUPPORTED by decision (with a Kubernetes learning path).
- Training jobs, GuardDuty detectors, and Inspector assessments have
  no delete route (jobs/assessments are immutable history); training
  job names are globally unique.
- Config evaluates 3 rule types only; SSM needs a host agent
  (honest 503 without one); Trusted Advisor runs 5 checks only.
- Region scoping is display-only (all resources are `cf-local-1`).

## Test quirks

- Hostagent disk-capacity check enforces 16 GiB against
  `/var/lib/cloudforge/labs` when it exists; on a small overlay
  this fails two hostagent tests in `make verify`. Remove the
  directory first (the walk cleanup does this). On a real host
  with ≥16 GiB free this does not trigger.
- `-seed-curriculum` is fatal when the curriculum directory cannot
  be resolved: run from the repo root or set `CURRICULUM_DIR`.
- The Rust engine's `NETWORK_ENGINE_ADDR` must be an IP literal
  (`0.0.0.0:50051`), not a hostname — `SocketAddr` parsing rejects
  `localhost:50051`. (The Go clients accept hostnames.)
