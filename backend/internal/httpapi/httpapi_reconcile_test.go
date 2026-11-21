package httpapi

// Milestone 9, Part 3: POST /api/v1/admin/cleanup-reconcile.
import (
	"net/http"
	"testing"
)

func TestCleanupReconcileEndpoint(t *testing.T) {
	_, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	learnerToken := registerAs(t, ts, "learner@example.com")

	// Learner is forbidden.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/admin/cleanup-reconcile", learnerToken, map[string]any{}); status != http.StatusForbidden {
		t.Fatalf("learner reconcile: expected 403, got %d", status)
	}
	// Unauthenticated is forbidden/unauthorized.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/admin/cleanup-reconcile", "", map[string]any{}); status != http.StatusUnauthorized {
		t.Fatalf("anonymous reconcile: expected 401, got %d", status)
	}
	// Admin, no labs: dry-run defaults to true, empty report list.
	status, body := do(t, "POST", ts.URL+"/api/v1/admin/cleanup-reconcile", adminToken, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("admin reconcile (no labs): %d %v", status, body)
	}
	if dry, _ := body["dry_run"].(bool); !dry {
		t.Fatalf("dry_run must default to true, body=%v", body)
	}
	reports, _ := body["reports"].([]any)
	if len(reports) != 0 {
		t.Fatalf("expected no reports with no labs, got %v", reports)
	}
	// Unknown lab id: 500 (reconcile_failed), not a 200 with a
	// fabricated report.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/admin/cleanup-reconcile", adminToken, map[string]any{"lab_id": "does-not-exist"}); status != http.StatusInternalServerError {
		t.Fatalf("unknown lab reconcile: expected 500, got %d", status)
	}
}

func TestCleanupReconcileEndpointPerLab(t *testing.T) {
	_, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	labID, _ := createLab(t, ts.URL, adminToken, "vpc-fundamentals")

	// The test engine (127.0.0.1:1) is unreachable, so the report
	// must say so in errors — and must still return 200 with the
	// report, reconciling whatever it can check (nothing here).
	status, body := do(t, "POST", ts.URL+"/api/v1/admin/cleanup-reconcile", adminToken, map[string]any{"lab_id": labID, "dry_run": true})
	if status != http.StatusOK {
		t.Fatalf("per-lab reconcile: %d %v", status, body)
	}
	reports, _ := body["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %v", body)
	}
	rep, _ := reports[0].(map[string]any)
	if rep["lab_id"] != labID {
		t.Fatalf("report lab mismatch: %v", rep)
	}
	if errs, _ := rep["errors"].([]any); len(errs) == 0 {
		t.Fatalf("unreachable engine must be reported in errors: %v", rep)
	}
	if removed, _ := rep["removed_db"].(float64); removed != 0 {
		t.Fatalf("dry-run must not remove anything: %v", rep)
	}
}
