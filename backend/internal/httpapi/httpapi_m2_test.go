package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// m2Server builds a server with an admin bootstrap email so the first
// admin can be created through the public register endpoint.
func m2Server(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServerWithDeps(Deps{
		Users: users, Labs: store.NewMemoryLabStore(labStore),
		Courses: store.NewMemoryCourseStore(), Audit: store.NewMemoryAuditStore(),
		Cache: &cache.Cache{}, Registry: providers.NewRegistry(),
		Network: networkclient.New("127.0.0.1:1"), Logger: nil,
		AdminEmail: "admin@example.com",
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts
}

func registerAs(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	status, body := do(t, "POST", ts.URL+"/api/v1/auth/register", "", map[string]any{
		"email": email, "name": "Test", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register %s: %d %v", email, status, body)
	}
	token, _ := body["token"].(string)
	return token
}

func TestAdminBootstrapAndUserManagement(t *testing.T) {
	_, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	learnerToken := registerAs(t, ts, "learner@example.com")

	// Admin user got the admin role via the bootstrap email.
	if status, body := do(t, "GET", ts.URL+"/api/v1/users/me", adminToken, nil); status != http.StatusOK || body["role"] != "admin" {
		t.Fatalf("admin /users/me: %d %v", status, body)
	}
	if status, body := do(t, "GET", ts.URL+"/api/v1/users/me", learnerToken, nil); status != http.StatusOK || body["role"] != "learner" {
		t.Fatalf("learner /users/me: %d %v", status, body)
	}
	// Learners cannot list users; admins can.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", learnerToken, nil); status != http.StatusForbidden {
		t.Fatalf("learner list users: got %d, want 403", status)
	}
	status, body := do(t, "GET", ts.URL+"/api/v1/users", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin list users: %d %v", status, body)
	}
	users, _ := body["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %v", body)
	}
}

func TestCourseCRUDAndProgressXP(t *testing.T) {
	_, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	learnerToken := registerAs(t, ts, "learner@example.com")

	// Learner cannot create courses.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/courses", learnerToken, map[string]any{
		"id": "x", "title": "X", "track": "T",
	}); status != http.StatusForbidden {
		t.Fatalf("learner create course: got %d, want 403", status)
	}
	// Admin creates a course with a module + lessons.
	status, body := do(t, "POST", ts.URL+"/api/v1/courses", adminToken, map[string]any{
		"id": "test-101", "title": "Test 101", "track": "Testing", "description": "d",
		"modules": []any{map[string]any{"title": "Core", "lessons": []string{"L1", "L2"}}},
	})
	if status != http.StatusCreated {
		t.Fatalf("admin create course: %d %v", status, body)
	}
	// Update + delete round-trip.
	if status, _ := do(t, "PUT", ts.URL+"/api/v1/courses/test-101", adminToken, map[string]any{
		"title": "Test 101 v2", "track": "Testing", "description": "d2",
	}); status != http.StatusOK {
		t.Fatalf("update course: got %d, want 200", status)
	}
	if status, _ := do(t, "DELETE", ts.URL+"/api/v1/courses/test-101", adminToken, nil); status != http.StatusOK {
		t.Fatalf("delete course: got %d, want 200", status)
	}

	// Progress flow on a seeded course: modules -> enroll -> complete.
	status, body = do(t, "GET", ts.URL+"/api/v1/courses/vpc-fundamentals/modules", learnerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("modules: %d %v", status, body)
	}
	modules, _ := body["modules"].([]any)
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %v", body)
	}
	mod, _ := modules[0].(map[string]any)
	lessons, _ := mod["lessons"].([]any)
	if len(lessons) != 6 {
		t.Fatalf("expected 6 lessons, got %v", lessons)
	}
	lesson, _ := lessons[0].(map[string]any)
	lessonID, _ := lesson["id"].(string)

	// Complete before enrolling -> 409.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/courses/vpc-fundamentals/progress", learnerToken,
		map[string]any{"lesson_id": lessonID}); status != http.StatusConflict {
		t.Fatalf("complete before enroll: got %d, want 409", status)
	}
	if status, _ := do(t, "POST", ts.URL+"/api/v1/courses/vpc-fundamentals/enroll", learnerToken, nil); status != http.StatusOK {
		t.Fatalf("enroll: got %d, want 200", status)
	}
	status, body = do(t, "POST", ts.URL+"/api/v1/courses/vpc-fundamentals/progress", learnerToken,
		map[string]any{"lesson_id": lessonID})
	if status != http.StatusOK || body["xp_awarded"] != float64(20) {
		t.Fatalf("complete lesson: %d %v (want xp_awarded=20: 10 lesson + 10 first-lesson bonus)", status, body)
	}
	// Second completion awards nothing.
	status, body = do(t, "POST", ts.URL+"/api/v1/courses/vpc-fundamentals/progress", learnerToken,
		map[string]any{"lesson_id": lessonID})
	if status != http.StatusOK || body["xp_awarded"] != float64(0) {
		t.Fatalf("re-complete: %d %v (want xp_awarded=0)", status, body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/courses/vpc-fundamentals/progress", learnerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("progress: %d %v", status, body)
	}
	if body["completed_lessons"] != float64(1) || body["xp"] != float64(20) {
		t.Fatalf("progress values wrong: %v", body)
	}
}

func TestAuditEndpointIsAdminOnly(t *testing.T) {
	_, ts := m2Server(t)
	adminToken := registerAs(t, ts, "admin@example.com")
	learnerToken := registerAs(t, ts, "learner@example.com")

	if status, _ := do(t, "GET", ts.URL+"/api/v1/audit", learnerToken, nil); status != http.StatusForbidden {
		t.Fatalf("learner audit: got %d, want 403", status)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/audit", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous audit: got %d, want 401", status)
	}
	status, body := do(t, "GET", ts.URL+"/api/v1/audit?limit=10&offset=0", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin audit: %d %v", status, body)
	}
	// Register/login mutations were audited (at least the two registers).
	if total, _ := body["total"].(float64); total < 2 {
		t.Fatalf("audit total should include registrations, got %v", body)
	}
	// limit is capped at 100.
	status, body = do(t, "GET", ts.URL+"/api/v1/audit?limit=500", adminToken, nil)
	if status != http.StatusOK || body["limit"] != float64(100) {
		t.Fatalf("audit limit cap: %d %v", status, body)
	}
}

func TestHealthReportsDatabaseAndRedisModes(t *testing.T) {
	_, ts := m2Server(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/health", "", nil)
	if status != http.StatusOK {
		t.Fatalf("health: %d %v", status, body)
	}
	// In-memory test server: no Postgres, no Redis.
	if body["database_connected"] != false || body["redis_connected"] != false {
		t.Fatalf("health must report database_connected=false and redis_connected=false in memory mode: %v", body)
	}
}
