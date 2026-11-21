// Milestone 9, Part 3: admin quota endpoints + enforcement.
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// Lightweight mode: a server built with Lite=true must enforce the
// lite limits even though its quota store is seeded with the
// full-mode defaults (3 labs / 4 CPU / 8 GiB / 50 GB / 8 h).
func TestLiteModeCapsQuotas(t *testing.T) {
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServerWithDeps(Deps{
		Users: users, Labs: store.NewMemoryLabStore(labStore),
		Courses: store.NewMemoryCourseStore(), Audit: store.NewMemoryAuditStore(),
		Cache: &cache.Cache{}, Registry: providers.NewRegistry(),
		Network: networkclient.New("127.0.0.1:1"), Logger: nil,
		AdminEmail: "admin@example.com", Lite: true,
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	// The stored row is the full default; roleLimits must cap it.
	l := srv.roleLimits(context.Background(), "learner")
	if l.MaxLabs != 1 || l.MaxCPU != 1 || l.MaxMemoryMB != 1024 ||
		l.MaxDiskGB != 8 || l.DurationMinutes() != quota.LiteMaxLabMinutes {
		t.Fatalf("lite roleLimits must cap full defaults at lite limits: %+v", l)
	}

	// /api/v1/health reports lite mode honestly.
	if status, body := do(t, "GET", ts.URL+"/api/v1/health", "", nil); status != http.StatusOK {
		t.Fatalf("health: got %d", status)
	} else if body["lite"] != true {
		t.Fatalf("health must report lite=true in lite mode: %v", body)
	}

	learnerToken := registerAs(t, ts, "learner@example.com")

	// Duration: vpc-fundamentals is a 60 min lab, over the 30 min
	// lite cap — refused before anything is provisioned.
	if status, body := do(t, "POST", ts.URL+"/api/v1/labs", learnerToken, map[string]any{
		"definition_id": "vpc-fundamentals", "name": "too-long",
	}); status != http.StatusTooManyRequests {
		t.Fatalf("60 min lab over the lite 30 min cap: got %d, want 429: %v", status, body)
	}
	// ec2-basics is a 30 min lab: allowed, and it fills the single
	// lite lab slot.
	if _, body := createLab(t, ts.URL, learnerToken, "ec2-basics"); body == nil {
		t.Fatal("30 min lab must be allowed in lite mode")
	}
	// Count: a second lab is refused even though it is also 30 min.
	if status, body := do(t, "POST", ts.URL+"/api/v1/labs", learnerToken, map[string]any{
		"definition_id": "iam-security", "name": "second",
	}); status != http.StatusTooManyRequests {
		t.Fatalf("second lab over the lite 1-lab cap: got %d, want 429: %v", status, body)
	} else if body["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatalf("quota error must carry code quota_exceeded: %v", body)
	}
}

func TestAdminQuotas(t *testing.T) {
	srv, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	learnerToken := registerAs(t, ts, "learner@example.com")

	// Learners cannot read or write quotas.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/admin/quotas", learnerToken, nil); status != http.StatusForbidden {
		t.Fatalf("learner GET quotas: got %d, want 403", status)
	}
	if status, _ := do(t, "PUT", ts.URL+"/api/v1/admin/quotas", learnerToken, map[string]any{"role": "learner", "max_labs": 1}); status != http.StatusForbidden {
		t.Fatalf("learner PUT quotas: got %d, want 403", status)
	}

	// Admin reads the seeded defaults.
	status, body := do(t, "GET", ts.URL+"/api/v1/admin/quotas", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin GET quotas: %d %v", status, body)
	}
	quotas, _ := body["quotas"].([]any)
	if len(quotas) != 3 {
		t.Fatalf("expected 3 seeded quota rows, got %v", body)
	}
	byRole := map[string]map[string]any{}
	for _, q := range quotas {
		m := q.(map[string]any)
		byRole[m["role"].(string)] = m
	}
	if byRole["learner"]["max_labs"] != float64(3) || byRole["admin"]["max_labs"] != float64(10) {
		t.Fatalf("seeded defaults wrong: %v", byRole)
	}

	// Invalid role / negative / over-cap values are rejected.
	for _, bad := range []map[string]any{
		{"role": "superuser", "max_labs": 5},
		{"role": "learner", "max_labs": -1},
		{"role": "learner", "max_labs": 101},
		{"role": "learner", "max_cpu": 65},
	} {
		if status, _ := do(t, "PUT", ts.URL+"/api/v1/admin/quotas", adminToken, bad); status != http.StatusBadRequest {
			t.Fatalf("PUT %v: got %d, want 400", bad, status)
		}
	}

	// Partial update: only max_labs changes, the rest keep defaults.
	status, body = do(t, "PUT", ts.URL+"/api/v1/admin/quotas", adminToken, map[string]any{"role": "learner", "max_labs": 1})
	if status != http.StatusOK {
		t.Fatalf("PUT quotas: %d %v", status, body)
	}
	q, _ := body["quota"].(map[string]any)
	if q["max_labs"] != float64(1) || q["max_cpu"] != float64(4) || q["max_lab_hours"] != float64(8) {
		t.Fatalf("partial update must keep other fields at defaults: %v", q)
	}

	// Enforcement: the learner can now create only 1 lab.
	labID, _ := createLab(t, ts.URL, learnerToken, "vpc-fundamentals")
	if status, body := do(t, "POST", ts.URL+"/api/v1/labs", learnerToken, map[string]any{
		"definition_id": "vpc-fundamentals", "name": "second",
	}); status != http.StatusTooManyRequests {
		t.Fatalf("second lab over the learner limit: got %d, want 429: %v", status, body)
	} else if body["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatalf("quota error must carry code quota_exceeded: %v", body)
	}
	// Destroying the lab frees the slot again.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+labID+"/destroy", learnerToken, nil); status != http.StatusOK {
		t.Fatalf("destroy lab: got %d", status)
	}
	if _, err := srv.Labs.Get(context.Background(), labID); err != nil {
		t.Fatalf("lab should still exist after destroy: %v", err)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/labs", learnerToken, map[string]any{
		"definition_id": "vpc-fundamentals", "name": "after-destroy",
	})
	if status != http.StatusCreated {
		t.Fatalf("lab after destroy freed the quota slot: got %d %v", status, body)
	}

	// Raising the admin limit raises the store ceiling too (the
	// backstop must not veto the new limit).
	if status, _ := do(t, "PUT", ts.URL+"/api/v1/admin/quotas", adminToken, map[string]any{"role": "admin", "max_labs": 12}); status != http.StatusOK {
		t.Fatalf("PUT admin max_labs 12: got %d", status)
	}
	if status, body := do(t, "GET", ts.URL+"/api/v1/admin/quotas", adminToken, nil); status != http.StatusOK {
		t.Fatalf("GET after update: %d %v", status, body)
	}
	// Duration cap: with the learner capped at 1 h, the 120-minute
	// three-tier-app is refused while the 60-minute vpc-fundamentals
	// lab is still allowed.
	if status, _ := do(t, "PUT", ts.URL+"/api/v1/admin/quotas", adminToken, map[string]any{"role": "learner", "max_labs": 3, "max_lab_hours": 1}); status != http.StatusOK {
		t.Fatalf("PUT learner 1 h cap: got %d", status)
	}
	if status, body := do(t, "POST", ts.URL+"/api/v1/labs", learnerToken, map[string]any{
		"definition_id": "three-tier-app", "name": "too-long",
	}); status != http.StatusTooManyRequests {
		t.Fatalf("120 min lab over the 1 h cap: got %d, want 429: %v", status, body)
	}
	// Restore the defaults so later tests in this package are unaffected.
	if status, _ := do(t, "PUT", ts.URL+"/api/v1/admin/quotas", adminToken, map[string]any{"role": "learner", "max_labs": 3, "max_lab_hours": 8}); status != http.StatusOK {
		t.Fatalf("restore learner defaults: got %d", status)
	}
}

func TestMemoryQuotaStore(t *testing.T) {
	ctx := context.Background()
	qs := store.NewMemoryQuotaStore()
	got, err := qs.GetQuotas(ctx)
	if err != nil || len(got) != 3 {
		t.Fatalf("seeded memory quotas: %v %v", got, err)
	}
	if _, err := qs.GetQuota(ctx, "nobody"); err == nil {
		t.Fatal("unknown role must return an error")
	}
	if err := qs.UpsertQuota(ctx, &store.QuotaSet{Role: "learner", MaxLabs: 2, MaxCPU: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	one, err := qs.GetQuota(ctx, "learner")
	if err != nil || one.MaxLabs != 2 || one.MaxCPU != 2 {
		t.Fatalf("upserted row: %+v %v", one, err)
	}
}
