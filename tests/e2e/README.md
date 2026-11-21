# CloudForge end-to-end acceptance test (Milestone 9, Part 3)

Black-box, stdlib-only Go test. It builds the real `api` binary and
the `e2e-fakes` binary (fake gRPC network engine + simulated host
agent), starts them as processes, and drives the real HTTP API —
register → login → open the VPC Fundamentals course → start a lab →
create a VPC → two subnets → routes/NAT/security group → launch a
Linux VM (simulated host agent) → terminal token → topology → break
a route (fault injection) → diagnose → repair → pass validation →
XP awarded → destroy the lab → verify all resources are cleaned up
(including the leftover engine network, via cleanup reconciliation).

## Run

```sh
cd tests/e2e
go test -run TestEndToEnd -v -count=1 -timeout 5m .
```

The test needs:

- A Go toolchain on `PATH` (the sandbox toolchain under
  `~/go/pkg/mod/golang.org/toolchain@.../bin` is found automatically).
- A reachable Redis on `REDIS_ADDR` (default `localhost:6379`):
  the API's auth rate limiter is fail-closed without one.
- Free loopback ports (chosen at runtime).

It does **not** need Docker, KVM, or the real Rust engine: the
fake engine speaks real gRPC on a real TCP listener, and the
simulated host agent is the real host-agent HTTP server.

## Hermeticity

The API is started with `DATABASE_URL` pointed at a dead port
(`127.0.0.1:1`), so it falls back to in-memory stores — the test
leaves nothing behind in the dev Postgres. (The config's default
DSN is the live dev Postgres; without the override, lab
provisioning would wait on a worker that isn't running.) The test
asserts the API log contains "in-memory stores" before proceeding.

## Skipping

The test SKIPS (does not fail) when:

- `CLOUDFORGE_E2E_SKIP=1` is set, or
- the Go toolchain is unavailable, or a binary fails to build, or
  the fake engine / host agent cannot be started.

Those are environment problems, not product failures. A failure
after setup (any HTTP step asserting real state) is a real failure,
and the API log is dumped into the test output.
