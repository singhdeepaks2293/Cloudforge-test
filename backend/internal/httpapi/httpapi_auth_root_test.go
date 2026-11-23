package httpapi

import (
	"net/http"
	"testing"
)

// v4.1 "Sign in as root": the login page offers a root option. With
// it checked, only an admin account signs in — and the session acts
// as admin. Without it, the session is learner-scoped even for an
// admin account, so admin-only endpoints stay closed until the user
// explicitly signs in as root. Tokens without a role claim (issued
// before v4.1, or at registration) fall back to the DB role.
func loginAs(t *testing.T, tsURL, email string, asRoot bool) (int, map[string]any) {
	t.Helper()
	status, body := do(t, "POST", tsURL+"/api/v1/auth/login", "", map[string]any{
		"email": email, "password": "correct-horse", "as_root": asRoot,
	})
	return status, body
}

func TestRootSignIn(t *testing.T) {
	_, ts := m2Server(t)
	adminRegisterToken := registerAs(t, ts, "admin@example.com")
	learnerRegisterToken := registerAs(t, ts, "learner@example.com")

	// 1. Admin + root checked: signs in as root, admin endpoints open.
	status, body := loginAs(t, ts.URL, "admin@example.com", true)
	if status != http.StatusOK {
		t.Fatalf("admin root login: %d %v", status, body)
	}
	rootToken, _ := body["token"].(string)
	if rootToken == "" {
		t.Fatalf("admin root login returned no token: %v", body)
	}
	if user, _ := body["user"].(map[string]any); user["role"] != "admin" {
		t.Fatalf("admin root login user.role: %v, want admin", body["user"])
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", rootToken, nil); status != http.StatusOK {
		t.Fatalf("root session list users: got %d, want 200", status)
	}
	if status, body := do(t, "GET", ts.URL+"/api/v1/auth/me", rootToken, nil); status != http.StatusOK || body["role"] != "admin" {
		t.Fatalf("root session /me: %d %v, want role admin", status, body)
	}

	// 2. Admin + root unchecked: signs in as learner, admin endpoints closed.
	status, body = loginAs(t, ts.URL, "admin@example.com", false)
	if status != http.StatusOK {
		t.Fatalf("admin learner login: %d %v", status, body)
	}
	learnerScopedToken, _ := body["token"].(string)
	if user, _ := body["user"].(map[string]any); user["role"] != "learner" {
		t.Fatalf("admin learner login user.role: %v, want learner", body["user"])
	}
	if status, body := do(t, "GET", ts.URL+"/api/v1/auth/me", learnerScopedToken, nil); status != http.StatusOK || body["role"] != "learner" {
		t.Fatalf("learner-scoped session /me: %d %v, want role learner", status, body)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", learnerScopedToken, nil); status != http.StatusForbidden {
		t.Fatalf("learner-scoped admin session list users: got %d, want 403", status)
	}

	// 3. Learner + root checked: rejected, never widened.
	status, body = loginAs(t, ts.URL, "learner@example.com", true)
	if status != http.StatusForbidden {
		t.Fatalf("learner root login: got %d, want 403: %v", status, body)
	}
	if errObj, _ := body["error"].(map[string]any); errObj["message"] != "admin role required" {
		t.Fatalf("learner root login message: %v, want 'admin role required'", body)
	}

	// 4. Learner + root unchecked: signs in as learner.
	status, body = loginAs(t, ts.URL, "learner@example.com", false)
	if status != http.StatusOK {
		t.Fatalf("learner login: %d %v", status, body)
	}
	learnerToken, _ := body["token"].(string)
	if user, _ := body["user"].(map[string]any); user["role"] != "learner" {
		t.Fatalf("learner login user.role: %v, want learner", body["user"])
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", learnerToken, nil); status != http.StatusForbidden {
		t.Fatalf("learner session list users: got %d, want 403", status)
	}

	// 5. Wrong password with root checked is still a 401, not a 403:
	// credentials are checked before the role.
	status, _ = do(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]any{
		"email": "learner@example.com", "password": "wrong-password", "as_root": true,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("bad-password root login: got %d, want 401", status)
	}

	// 6. Tokens without a role claim (the registration tokens) keep
	// working and fall back to the account's DB role.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", adminRegisterToken, nil); status != http.StatusOK {
		t.Fatalf("claim-less admin token list users: got %d, want 200", status)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/users", learnerRegisterToken, nil); status != http.StatusForbidden {
		t.Fatalf("claim-less learner token list users: got %d, want 403", status)
	}
}
