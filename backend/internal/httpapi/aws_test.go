package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
)

func TestAWSCapabilitiesEndpoint(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/aws/s3/capabilities", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["service"] != "s3" {
		t.Fatalf("service = %v", body["service"])
	}
	caps, _ := body["capabilities"].([]any)
	if len(caps) < 4 {
		t.Fatalf("s3 capabilities = %d, want >= 4 (CreateBucket/ListBuckets/PutObject/GetObject)", len(caps))
	}
	// Health is a real probe result, never assumed ok: with no
	// MinIO running the s3 service must not report ok.
	if body["health"] == "ok" {
		t.Fatalf("s3 health = ok with no MinIO running: %v", body)
	}
	// Unknown service -> 404.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/aws/nosuch/capabilities", "", nil); status != http.StatusNotFound {
		t.Fatalf("unknown service status = %d, want 404", status)
	}
	// EKS stays explicitly unsupported.
	status, body = do(t, "GET", ts.URL+"/api/v1/aws/eks/capabilities", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	caps, _ = body["capabilities"].([]any)
	if len(caps) != 1 {
		t.Fatalf("eks capabilities = %v", body)
	}
	if caps[0].(map[string]any)["fidelity"] != "UNSUPPORTED" {
		t.Fatalf("eks capability = %v, want UNSUPPORTED", caps[0])
	}
}

func TestProvidersCapabilitiesStillWorks(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/providers/capabilities", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	caps, _ := body["capabilities"].([]any)
	found := false
	for _, c := range caps {
		m := c.(map[string]any)
		if m["service"] == "s3" && m["operation"] == "PutObject" {
			found = true
		}
		if m["fidelity"] == "REAL" {
			t.Fatalf("capability claims REAL: %v", m)
		}
	}
	if !found {
		t.Fatal("providers/capabilities must include the new s3 PutObject entry")
	}
}

func TestS3EndpointsReturn503WithoutBackend(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)

	// No AWS wiring on the test server (AWS=nil) -> 503 with
	// capability info, never a faked success.
	status, body := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets", token, map[string]any{"name": "my-bucket"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status %d, body %v, want 503", status, body)
	}
	if _, ok := body["capability"]; !ok {
		t.Fatalf("503 must include capability info: %v", body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/aws/s3/buckets", "", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("list status %d, want 503", status)
	}

	// Mutating endpoint requires auth even before the backend check.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets", "", map[string]any{"name": "x"}); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", status)
	}
}

func TestS3EndpointsReturn503WhenMinIOUnreachable(t *testing.T) {
	// Server wired with a real AWS struct pointing at the default
	// (unreachable) MinIO endpoint: the 503 must carry the s3
	// capability and name the backend.
	aws, err := providers.NewAWS(providers.AWSConfig{}, providers.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore("test-secret")
	srv := NewServerWithDeps(Deps{
		Users:    users,
		Registry: aws.Registry,
		AWS:      aws,
		Network:  networkclient.New("127.0.0.1:1"),
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	token := register(t, ts)
	status, body := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets", token, map[string]any{"name": "my-bucket"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status %d, body %v, want 503 (no MinIO in this sandbox)", status, body)
	}
	cap, _ := body["capability"].(map[string]any)
	if cap["service"] != "s3" || cap["operation"] != "CreateBucket" {
		t.Fatalf("capability = %v", body)
	}
	if cap["fidelity"] != "API_EMULATED" {
		t.Fatalf("fidelity = %v, want API_EMULATED", cap["fidelity"])
	}
}

func TestIAMEvaluateEndpoint(t *testing.T) {
	_, ts := testServer(t)
	token := register(t, ts)

	policy := map[string]any{"Version": "2012-10-17", "Statement": []any{
		map[string]any{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::my-bucket/*"},
	}}
	// Unauthenticated -> 401.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", "", map[string]any{
		"policies": []any{policy}, "action": "s3:GetObject", "resource": "arn:aws:s3:::my-bucket/k",
	}); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated evaluate status = %d, want 401", status)
	}
	// Allow.
	status, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", token, map[string]any{
		"policies": []any{policy}, "action": "s3:GetObject", "resource": "arn:aws:s3:::my-bucket/k",
	})
	if status != http.StatusOK || body["decision"] != "Allow" {
		t.Fatalf("allow: status %d, body %v", status, body)
	}
	// Deny (no matching statement).
	status, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", token, map[string]any{
		"policies": []any{policy}, "action": "s3:DeleteObject", "resource": "arn:aws:s3:::my-bucket/k",
	})
	if status != http.StatusOK || body["decision"] != "Deny" {
		t.Fatalf("deny: status %d, body %v", status, body)
	}
	// Missing action -> 400.
	if status, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", token, map[string]any{
		"policies": []any{policy}, "resource": "x",
	}); status != http.StatusBadRequest {
		t.Fatalf("missing action status = %d, want 400", status)
	}
}
