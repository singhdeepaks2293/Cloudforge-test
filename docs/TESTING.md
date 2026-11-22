# CloudForge Testing

## The one command

```sh
DATABASE_URL="postgres://cloudforge:cloudforge@127.0.0.1:5432/cloudforge?sslmode=disable" \
REDIS_ADDR=localhost:6379 make verify
```

`make verify` runs: `gofmt` + `go vet` + `go test ./...` (backend,
23 packages, including Postgres integration tests when a server is
up — they skip otherwise), `cargo test --workspace` (Rust, 75
tests) + `cargo clippy`, and the frontend build + vitest (138 tests)
+ oxlint. It must exit 0. Phase 7 result (2026-11-22, sandbox):
exit 0.

## Suites

| Suite | Command | Phase 7 result |
|---|---|---|
| Go unit + integration | `cd backend && go test ./... -count=1` (with `DATABASE_URL` + `REDIS_ADDR` set) | All 23 packages pass (auth ~15 s, httpapi ~30 s, store incl. Postgres integration, providers, networkclient, ratelimit) |
| Rust | `cd rust-network-engine && cargo test --workspace` | 75 passed, 0 failed (74 + 1 new `grpc_auth` test); `cargo clippy --workspace --all-targets` clean except pre-existing workspace warnings |
| Frontend | `cd frontend && npm run build && npm test && npm run lint` | Build passes, vitest 138/138 (was 136/136), oxlint 0 warnings |
| E2E acceptance | `cd tests/e2e && go test -v . -count=1` | `TestEndToEnd` PASS (14.8 s) — exercises the token-authenticated fake engine path |
| Live walk | `/tmp/cf-live/p7/walk-phase7.sh` (recreate from the Phase 7 report if lost — `/tmp` is ephemeral) | FAIL=0: every hardening item + the Phase 1–6 regression flows, with real curl responses |

## What the tests cover (Phase 7 additions)

- `backend/internal/auth/revocation_test.go` — logout revokes one
  token, not the other; `RevokeUser` revokes all of one user only;
  expired revocations stop denying.
- `backend/internal/store/store_integration_test.go` —
  `TestPostgresRevocationIntegration` (Postgres denylist round-trip).
- `backend/internal/config/config_test.go` —
  `TestValidateSecretsFailsClosed` (refuses dev defaults without
  the opt-in).
- `backend/internal/httpapi/security_p7_test.go` — logout revokes,
  login sets the `HttpOnly` session cookie, cookie-only auth,
  admin session revoke, `/metrics` token + loopback rules, security
  headers (HSTS on with TLS, off without), trusted-proxy XFF
  (untrusted peer ignored, loopback/private honoured), per-user
  rate limit (limit 2, other user unaffected, unauthenticated
  skips), audit export job lifecycle (create/list/download/
  retention prune to 2, non-admin 403).
- `backend/internal/jobs/audit_export_test.go` —
  `TestExportAuditArchive` (old rows archived, fresh rows kept).
- `backend/internal/terraform/terraform_test.go` —
  `TestTFHostConfigIsolated` (`--network none`, no ExtraHosts).
- `rust-network-engine/crates/grpc-server/tests/grpc_auth.rs` —
  no token → `UNAUTHENTICATED`, wrong token → `UNAUTHENTICATED`,
  right token → Health ok.
- `frontend/src/services/api.test.ts` — logout POSTs
  `/auth/logout` and clears the token; clears it even when the
  server call fails.

## Live verification (sandbox)

The sandbox has no Docker, KVM, `/dev/kvm`, MinIO binary, sklearn,
or sshd, so live verification runs the lite stack directly from
source: engine `:50051` + API `:8081` + worker `:8083` + frontend
preview `:8080` against real Postgres 16 + Redis 7, with the
explicit dev opt-in (`CLOUDFORGE_DEV=1`) where Phase 7 introduced
it. The Phase 7 walk records real curl responses for: fail-closed
startup (API + engine), engine token auth (right token up /
wrong token down), `/metrics` 401/200, CSP/security headers,
session cookie + cookie-only auth, per-user rate limit 429,
spoofed-XFF auth limit 429, logout revocation 401, admin session
revoke, lab `ec2-basics` → READY → DESTROYED, one flow per phase
(IAM evaluate, SQS send/receive, ECR create, Terraform Learning
courses, Athena execute, GuardDuty detector, Cost Explorer), audit
export job (create/list/download, real rows), frontend pages, and
backup/restore round-trip. See `MILESTONE_LOG.md` (Phase 7 entry)
for the transcript.

## Known test quirks

- **Hostagent disk check:** if `/var/lib/cloudforge/labs` exists
  on a disk with <16 GiB free, `make verify` fails two hostagent
  tests. Remove the directory before running `make verify` (the
  walk cleanup does this). Pre-existing, not a Phase 7 change.
- **Go toolchain:** `go.mod` targets Go 1.26. If `go` is missing
  after a VM replacement, use the module-cache toolchain
  (`~/go/pkg/mod/golang.org/toolchain@*/bin/go`, `GOTOOLCHAIN=local`).
- **Rust formatting:** run `cargo fmt --all` — `cargo fmt --check`
  flags even one-line diffs (Phase 7 caught one in
  `grpc-server/src/lib.rs`).
- **Secret redaction:** tool output redacts secret-looking values
  as `<redacted>`; when editing secret-adjacent lines, anchor on
  comment lines or verify with Python `repr` checks rather than
  trusting displayed values.
