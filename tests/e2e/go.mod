module github.com/cloudforge/cloudforge/tests/e2e

go 1.26.0

// Stdlib only: the test is black-box — it builds the real api binary
// and the e2e-fakes binary (fake gRPC engine + simulated host agent),
// starts them as processes, and drives the real HTTP API. It never
// imports backend/internal packages (Go's internal rule forbids it
// from outside the backend module), which is exactly what makes
// this a true end-to-end test.
