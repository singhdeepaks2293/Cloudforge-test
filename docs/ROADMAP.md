# CloudForge Roadmap

What comes after CloudForge v2 (Phases 0–7, complete 2026-11-22).
Ordered by value; nothing here is promised on a date.

## Security hardening (Phase 7 follow-ups)

1. **Engine mTLS.** Replace/augment the shared-secret token with a
   generated CA + client certificates issued at deploy time
   (server verifies client cert, clients verify server cert).
   Token auth stays as the fallback for dev mode.
2. **Short-lived access tokens + refresh.** 12 h JWTs become
   short-lived (e.g. 15 min) with a rotating refresh token stored
   server-side; the revocation denylist (`jti`, migration 013)
   already supports this.
3. **Header auth becomes API-client-only.** Stop issuing the token
   to browser clients; the cookie becomes the only browser session.
   (Phase 7 kept the header path working during transition.)
4. **ECR registry auth.** Put auth in front of `registry:2` (or a
   token endpoint backed by CloudForge IAM) so the registry is not
   local-only-by-convention.
5. **SSRF grep gate in `make verify`** (audit R14): fail on new
   `http.Get`/`http.Post` to user-supplied URLs outside the guard.
6. **Postgres-required startup in compose** (audit R13): fail
   startup when Postgres is configured but unreachable, instead of
   silently falling back to in-memory stores.
7. **Per-user limiter state on the admin dashboard** (audit R8
   follow-up) — surface limiter state, not just enforcement.

## Platform

- **Real-host verification of the Docker paths:** `docker compose
  build/up/config`, TLS compose override against nginx, Grafana
  dashboards against live Prometheus, full-mode resource limits
  under load. (Unverifiable in the sandbox; the step the Phase 7
  report flags.)
- **Real netlink backend verification** on a KVM-capable host
  (rtnetlink/nftables, `CAP_NET_ADMIN` + `nft`).
- **Terraform container execution on a real host** (the success
  path is implemented and stub-tested, unverified live).
- **MinIO-backed S3 flows** once a MinIO binary is obtainable
  (dl.min.io returned 410 in the sandbox).
- **EKS decision revisit:** local k3s/kind, or keep UNSUPPORTED
  with the Kubernetes learning path.

## Product

- More curriculum tracks and break-and-fix scenarios; instructor
  dashboards; certification-style tracks.
- Learning Center guided labs wired to more lab definitions.
- Cost Explorer: more services priced from real usage.
- Config: more rule types (evaluated from live engine state, not
  just lab state).

## Explicitly not planned

- Real AWS execution (the point of CloudForge is learning without
  an AWS account). The AWS provider learning mode works only with
  a local emulator; real AWS credentials are refused.
- Terraform Cloud / HCP Terraform remote backend.
- Hosting CloudForge as a public SaaS (the threat model is a
  trusted local network).
