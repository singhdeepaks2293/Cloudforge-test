// Milestone 9, Part 3: security review tests — audit metadata
// scrubbing and provider endpoint SSRF guards.
package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditMetadataIsScrubbed(t *testing.T) {
	srv, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	// Record an audit entry whose metadata accidentally carries a
	// token and a credential URL, then read it back via the admin
	// audit listing: neither secret may be persisted in clear text.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/audit", nil)
	_ = req
	srv.audit(req, "tester", "test_action", "test", "x", map[string]any{
		"token":  "secret-jwt-value",
		"detail": "called postgres://cf:hunter2@db:5432/cf",
		"public": "visible",
		"nested": map[string]any{"password": "hunter2"},
	})
	status, body := do(t, "GET", ts.URL+"/api/v1/audit?limit=5", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("audit list: %d %v", status, body)
	}
	entries, _ := body["audit"].([]any)
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	found := false
	for _, e := range entries {
		m := e.(map[string]any)
		if m["action"] != "test_action" {
			continue
		}
		found = true
		meta, _ := m["metadata"].(map[string]any)
		if meta["token"] != "<redacted>" {
			t.Fatalf("audit token must be redacted, got %v", meta["token"])
		}
		if d, _ := meta["detail"].(string); d != "called postgres://cf:<redacted>@db:5432/cf" {
			t.Fatalf("audit credential URL must be redacted, got %q", d)
		}
		if meta["public"] != "visible" {
			t.Fatalf("non-secret metadata must pass through: %v", meta)
		}
		nested, _ := meta["nested"].(map[string]any)
		if nested["password"] != "<redacted>" {
			t.Fatalf("nested password must be redacted: %v", nested)
		}
	}
	if !found {
		t.Fatal("test audit entry not found in listing")
	}
}
