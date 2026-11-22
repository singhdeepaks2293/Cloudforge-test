// Phase 2 tests: IAM console API, VPC detail API, S3 object API,
// EC2 Start/Reboot, monitoring/alarms and audit filters/export.
// Everything runs against the in-memory stores with the network
// engine pointed at a closed port, so engine-dependent calls must
// fail honestly (502/503) and DB-authoritative calls must succeed.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// adminToken registers ada@example.com and promotes her to admin.
func adminToken(t *testing.T, srv *Server, ts *httptest.Server) string {
	t.Helper()
	token := registerAs(t, ts, "ada@example.com")
	id, err := srv.Users.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Users.SetRole(id, "admin"); err != nil {
		t.Fatal(err)
	}
	return token
}

// doRawBody posts a literal request body (for payloads that cannot
// be produced by encoding a Go value, e.g. malformed JSON).
func doRawBody(t *testing.T, method, url, token, raw string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: could not decode JSON body: %v", method, url, err)
	}
	return resp.StatusCode, out
}

func doRawText(t *testing.T, method, url, token string, body any) (int, string, http.Header) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func decodeBody(t *testing.T, s string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("could not decode JSON body %q: %v", s, err)
	}
	return out
}

const validPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBuckets","Resource":"*"}]}`
const denyPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"s3:DeleteBucket","Resource":"*"}]}`

func TestIAMUserPolicyCRUDAndEvaluateTrail(t *testing.T) {
	srv, ts := testServer(t)
	admin := adminToken(t, srv, ts)
	learner := registerAs(t, ts, "learner@example.com")

	// Non-admins cannot manage IAM identities.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/users", learner, map[string]any{"name": "bob"}); st != http.StatusForbidden {
		t.Fatalf("learner create IAM user: got %d, want 403", st)
	}

	// Create a user.
	st, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/users", admin, map[string]any{"name": "alice"})
	if st != http.StatusCreated {
		t.Fatalf("create user: %d %v", st, body)
	}
	userID, _ := body["id"].(string)
	if userID == "" {
		t.Fatalf("create user returned no id: %v", body)
	}
	// Duplicate name is a conflict, not a silent overwrite.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/users", admin, map[string]any{"name": "alice"}); st != http.StatusConflict {
		t.Fatalf("duplicate user: got %d, want 409", st)
	}
	// Bad name is rejected.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/users", admin, map[string]any{"name": "bad name!"}); st != http.StatusBadRequest {
		t.Fatalf("bad name: got %d, want 400", st)
	}

	// A policy with an unsupported condition operator is rejected
	// BEFORE storage: it must never be stored unevaluated.
	badPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*","Condition":{"StringLike":{"aws:username":"a*"}}}]}`

	if st, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, map[string]any{"name": "bad", "document": json.RawMessage(badPolicy)}); st != http.StatusBadRequest {
		t.Fatalf("unsupported condition: got %d %v, want 400", st, body)
	}
	// A policy with a variable is rejected too.
	varPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::${aws:username}/*"}]}`
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, map[string]any{"name": "var", "document": json.RawMessage(varPolicy)}); st != http.StatusBadRequest {
		t.Fatalf("policy variable: got %d, want 400", st)
	}
	// Malformed JSON is rejected (sent as a literal body: encoding
	// it through the test client would fail before it reached us).
	if st, _ := doRawBody(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, `{"name":"bad","document":{oops}}`); st != http.StatusBadRequest {
		t.Fatalf("malformed policy JSON: got %d, want 400", st)
	}
	// Nothing was stored: the list is still empty.
	st, body = do(t, "GET", ts.URL+"/api/v1/aws/iam/policies", admin, nil)
	if st != http.StatusOK {
		t.Fatalf("list policies: %d %v", st, body)
	}
	if list, _ := body["policies"].([]any); len(list) != 0 {
		t.Fatalf("rejected policies must not be stored, got %d", len(list))
	}

	// Valid allow + deny policies store fine.
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, map[string]any{"name": "list-buckets", "document": json.RawMessage(validPolicy)})
	if st != http.StatusCreated {
		t.Fatalf("create allow policy: %d %v", st, body)
	}
	allowID, _ := body["id"].(string)
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, map[string]any{"name": "no-delete", "document": json.RawMessage(denyPolicy)})
	if st != http.StatusCreated {
		t.Fatalf("create deny policy: %d %v", st, body)
	}
	denyID, _ := body["id"].(string)

	// Attach the allow policy to the user, then evaluate via user_id.
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/users/"+userID+"/policies", admin, map[string]any{"policy_id": allowID})
	if st != http.StatusOK {
		t.Fatalf("attach policy: %d %v", st, body)
	}
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", admin, map[string]any{
		"user_id": userID, "action": "s3:ListBuckets", "resource": "*",
	})
	if st != http.StatusOK || body["decision"] != "Allow" {
		t.Fatalf("evaluate via user: %d %v", st, body)
	}
	trail, _ := body["trail"].([]any)
	if len(trail) == 0 {
		t.Fatalf("evaluation must return a trail: %v", body)
	}
	decides := false
	for _, e := range trail {
		if m, ok := e.(map[string]any); ok && m["decides"] == true {
			decides = true
		}
	}
	if !decides {
		t.Fatalf("trail must name the deciding statement: %v", trail)
	}

	// Explicit deny wins over the allow when both are evaluated.
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", admin, map[string]any{
		"user_id": userID, "policy_ids": []string{denyID},
		"action": "s3:DeleteBucket", "resource": "*",
	})
	if st != http.StatusOK || body["decision"] != "Deny" {
		t.Fatalf("explicit deny: %d %v", st, body)
	}
	if reason, _ := body["reason"].(string); !strings.Contains(reason, "explicit deny") {
		t.Fatalf("deny reason must say explicit deny: %v", body)
	}
	// Nothing matches -> implicit (default) deny, with the trail
	// showing every statement as unmatched.
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", admin, map[string]any{
		"user_id": userID, "action": "ec2:RunInstances", "resource": "*",
	})
	if st != http.StatusOK || body["decision"] != "Deny" {
		t.Fatalf("implicit deny: %d %v", st, body)
	}
	if reason, _ := body["reason"].(string); !strings.Contains(reason, "implicit deny") {
		t.Fatalf("deny reason must say implicit deny: %v", body)
	}
	if ms, _ := body["matched_statement"].(float64); ms != 0 {
		t.Fatalf("implicit deny must match no statement, got %v", body)
	}
	// Detach + delete round-trip.
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/aws/iam/users/"+userID+"/policies/"+allowID, admin, nil); st != http.StatusOK {
		t.Fatalf("detach: got %d", st)
	}
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/aws/iam/users/"+userID, admin, nil); st != http.StatusOK {
		t.Fatalf("delete user: got %d", st)
	}
	if st, _ := do(t, "GET", ts.URL+"/api/v1/aws/iam/users/"+userID, admin, nil); st != http.StatusNotFound {
		t.Fatalf("deleted user must 404, got %d", st)
	}
}

func TestIAMAccessKeysShownOnceAndRevoked(t *testing.T) {
	srv, ts := testServer(t)
	admin := adminToken(t, srv, ts)
	_, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/users", admin, map[string]any{"name": "keyuser"})
	userID, _ := body["id"].(string)

	st, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/users/"+userID+"/access-keys", admin, nil)
	if st != http.StatusCreated {
		t.Fatalf("create access key: %d %v", st, body)
	}
	secret, _ := body["secret"].(string)
	if secret == "" {
		t.Fatalf("create must return the secret once: %v", body)
	}
	key, _ := body["access_key"].(map[string]any)
	keyID, _ := key["id"].(string)
	if !strings.HasPrefix(keyID, "AKIA") {
		t.Fatalf("key id must look like an access key id, got %q", keyID)
	}
	// The list never returns the secret (or its hash).
	st, body = do(t, "GET", ts.URL+"/api/v1/aws/iam/users/"+userID+"/access-keys", admin, nil)
	if st != http.StatusOK {
		t.Fatalf("list keys: %d %v", st, body)
	}
	keys, _ := body["access_keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %v", body)
	}
	if raw, _ := json.Marshal(body); strings.Contains(string(raw), secret) {
		t.Fatalf("the secret must never appear in a list response")
	}
	// Deactivate, then revoke.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/users/"+userID+"/access-keys/"+keyID+"/status", admin, map[string]any{"status": "Inactive"}); st != http.StatusOK {
		t.Fatalf("deactivate: got %d", st)
	}
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/aws/iam/users/"+userID+"/access-keys/"+keyID, admin, nil); st != http.StatusOK {
		t.Fatalf("revoke: got %d", st)
	}
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/aws/iam/users/"+userID+"/access-keys/"+keyID, admin, nil); st != http.StatusNotFound {
		t.Fatalf("revoking twice must 404, got %d", st)
	}
}

func TestIAMGroupsRolesAndAssumeRole(t *testing.T) {
	srv, ts := testServer(t)
	admin := adminToken(t, srv, ts)
	ctx := context.Background()

	// Group + membership.
	_, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/groups", admin, map[string]any{"name": "devs"})
	groupID, _ := body["id"].(string)
	_, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/users", admin, map[string]any{"name": "gmember"})
	userID, _ := body["id"].(string)
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/groups/"+groupID+"/members", admin, map[string]any{"user_id": userID}); st != http.StatusOK {
		t.Fatalf("add member: got %d", st)
	}
	// A policy attached to the group applies to the member in the simulator.
	_, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/policies", admin, map[string]any{"name": "gpol", "document": json.RawMessage(validPolicy)})
	polID, _ := body["id"].(string)
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/groups/"+groupID+"/policies", admin, map[string]any{"policy_id": polID}); st != http.StatusOK {
		t.Fatalf("attach group policy: got %d", st)
	}
	st, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", admin, map[string]any{
		"user_id": userID, "action": "s3:ListBuckets", "resource": "*",
	})
	if st != http.StatusOK || body["decision"] != "Allow" {
		t.Fatalf("group policy must apply to the member: %d %v", st, body)
	}

	// Role: a trust policy without sts:AssumeRole is rejected.
	badTrust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/roles", admin, map[string]any{"name": "bad", "trust_policy": json.RawMessage(badTrust)}); st != http.StatusBadRequest {
		t.Fatalf("trust without AssumeRole: got %d, want 400", st)
	}
	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:cloudforge:iam::local:user/alice"},"Action":"sts:AssumeRole","Resource":"*"}]}`
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/roles", admin, map[string]any{
		"name": "reader", "trust_policy": json.RawMessage(trust), "max_session_duration_seconds": 3600,
	})
	if st != http.StatusCreated {
		t.Fatalf("create role: %d %v", st, body)
	}
	roleID, _ := body["id"].(string)

	// AssumeRole: short-lived session, token shown once.
	st, body = do(t, "POST", ts.URL+"/api/v1/aws/iam/roles/"+roleID+"/assume", admin, map[string]any{"duration_seconds": 900})
	if st != http.StatusCreated {
		t.Fatalf("assume role: %d %v", st, body)
	}
	token, _ := body["token"].(string)
	sessionID, _ := body["session_id"].(string)
	if token == "" || sessionID == "" {
		t.Fatalf("assume must return a one-time token and session id: %v", body)
	}
	st, body = do(t, "GET", ts.URL+"/api/v1/aws/iam/sessions/"+sessionID, admin, nil)
	if st != http.StatusOK || body["valid"] != true {
		t.Fatalf("live session must validate: %d %v", st, body)
	}
	// A duration past the role max is refused.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/iam/roles/"+roleID+"/assume", admin, map[string]any{"duration_seconds": 99999}); st != http.StatusBadRequest {
		t.Fatalf("over-max duration: got %d, want 400", st)
	}
	// An expired session is a 410 (expiry enforced, never extended).
	expired, err := srv.IAM.CreateIAMSession(ctx, roleID, "someone", store.HashSecret("tok"), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := do(t, "GET", ts.URL+"/api/v1/aws/iam/sessions/"+expired.ID, admin, nil); st != http.StatusGone {
		t.Fatalf("expired session: got %d, want 410", st)
	}
}

func TestInstanceCreateStartsVMAndRebootWorks(t *testing.T) {
	// A real simulated host agent (in-process): the API must start
	// the VM it creates, or a launched instance would sit in
	// "defined" forever and reboot could never succeed.
	ha := httptest.NewServer(hostagent.NewServer(hostagent.NewSimulatedHostAgent(), "test-token", nil).Routes())
	defer ha.Close()
	users := auth.NewStore("test-secret")
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServerWithDeps(Deps{
		Users:     users,
		Labs:      store.NewMemoryLabStore(labStore),
		Registry:  providers.NewRegistry(),
		Network:   networkclient.New("127.0.0.1:1"),
		HostAgent: hostagent.NewClient(ha.URL, "test-token"),
	})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	token := registerAs(t, ts, "ada@example.com")
	lab := createReadyLab(t, ts, token)

	st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "instance", "name": "web", "config": map[string]any{"profile": "micro"},
	})
	if st != http.StatusCreated {
		t.Fatalf("create instance: %d %v", st, body)
	}
	if body["state"] != "running" {
		t.Fatalf("a launched instance must be running, got state %v: %v", body["state"], body)
	}
	if ip, _ := body["private_ip"].(string); ip == "" {
		t.Fatalf("a launched instance must record its private IP: %v", body)
	}
	instID, _ := body["id"].(string)

	// Reboot now works end-to-end (lab READY + VM running).
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/reboot", token, nil); st != http.StatusOK {
		t.Fatalf("reboot with a running VM: got %d, want 200", st)
	}
	// And a route via the running instance resolves its gateway
	// (the engine is down here, so the honest answer is 502 — the
	// point is it is NOT a 400 for a missing private IP).
	vpc, _ := srv.Resources.CreateResource(context.Background(), &store.LabResource{LabID: lab, Kind: "vpc", Name: "main", State: "available", CIDR: "10.0.0.0/16"})
	rt, _ := srv.Resources.CreateResource(context.Background(), &store.LabResource{LabID: lab, Kind: "route-table", Name: "rt", State: "available", ParentID: vpc.ID})
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+rt.ID+"/routes", token, map[string]any{"destination": "10.1.0.0/16", "target": instID}); st != http.StatusBadGateway {
		t.Fatalf("route via running instance with engine down: got %d, want 502", st)
	}
}

func TestFirstHostAddress(t *testing.T) {
	if got, err := firstHostAddress("10.0.0.0/16"); err != nil || got != "10.0.0.1" {
		t.Fatalf("10.0.0.0/16: %q %v", got, err)
	}
	if got, err := firstHostAddress("192.168.1.0/24"); err != nil || got != "192.168.1.1" {
		t.Fatalf("192.168.1.0/24: %q %v", got, err)
	}
	if _, err := firstHostAddress("not-a-cidr"); err == nil {
		t.Fatalf("bad CIDR must error")
	}
	if _, err := firstHostAddress("10.0.0.5/32"); err == nil {
		t.Fatalf("/32 has no usable host address")
	}
}

func TestResolveRouteNextHop(t *testing.T) {
	srv, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")
	lab := createReadyLab(t, ts, token)
	ctx := context.Background()
	vpc, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "vpc", Name: "main", State: "available", CIDR: "10.0.0.0/16"})
	rt, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "route-table", Name: "rt", State: "available", ParentID: vpc.ID})
	noIP, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "instance", Name: "noip", State: "running"})
	withIP, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "instance", Name: "web", State: "running", PrivateIP: "10.0.1.5"})
	nat, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "gateway", Name: "nat", State: "available", Config: map[string]any{"gateway_type": "nat", "translated_address": "203.0.113.10"}})

	// "local" routes via the VPC router (first address of the VPC CIDR)
	// on the lab's engine bridge (interface = lab id).
	gw, iface, err := resolveRouteNextHop(ctx, srv, lab, rt, "local")
	if err != nil || gw != "10.0.0.1" || iface != lab {
		t.Fatalf("local: gw=%q iface=%q err=%v", gw, iface, err)
	}
	// The VPC itself as a target is the same.
	if gw, _, err := resolveRouteNextHop(ctx, srv, lab, rt, vpc.ID); err != nil || gw != "10.0.0.1" {
		t.Fatalf("vpc target: gw=%q err=%v", gw, err)
	}
	// NAT routes via its translated address.
	if gw, _, err := resolveRouteNextHop(ctx, srv, lab, rt, nat.ID); err != nil || gw != "203.0.113.10" {
		t.Fatalf("nat target: gw=%q err=%v", gw, err)
	}
	// An instance routes via its private IP — but one without a
	// recorded IP cannot be a next hop.
	if gw, _, err := resolveRouteNextHop(ctx, srv, lab, rt, withIP.ID); err != nil || gw != "10.0.1.5" {
		t.Fatalf("instance with IP: gw=%q err=%v", gw, err)
	}
	if _, _, err := resolveRouteNextHop(ctx, srv, lab, rt, noIP.ID); err == nil {
		t.Fatalf("instance without a private IP must be rejected as a route target")
	}
	// Unknown targets and non-routable kinds are rejected.
	if _, _, err := resolveRouteNextHop(ctx, srv, lab, rt, "missing"); err == nil {
		t.Fatalf("unknown target must be rejected")
	}
	if _, _, err := resolveRouteNextHop(ctx, srv, lab, rt, rt.ID); err == nil {
		t.Fatalf("a route table cannot be a route target")
	}
	// The handler maps an instance without an IP to a 400 before
	// any engine call.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+rt.ID+"/routes", token, map[string]any{"destination": "10.1.0.0/16", "target": noIP.ID}); st != http.StatusBadRequest {
		t.Fatalf("route via IP-less instance: got %d, want 400", st)
	}
}

func TestVPCSubnetCIDRValidation(t *testing.T) {
	srv, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")
	// Seed a VPC + sibling subnet directly in the store so the
	// overlap check has something to compare against.
	lab := createReadyLab(t, ts, token)
	vpc, err := srv.Resources.CreateResource(context.Background(), &store.LabResource{
		LabID: lab, Kind: "vpc", Name: "main", State: "available", CIDR: "10.0.0.0/16",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Resources.CreateResource(context.Background(), &store.LabResource{
		LabID: lab, Kind: "subnet", Name: "a", State: "available", CIDR: "10.0.1.0/24", ParentID: vpc.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// Invalid CIDR -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "subnet", "name": "bad", "cidr": "not-a-cidr", "parent_id": vpc.ID,
	}); st != http.StatusBadRequest {
		t.Fatalf("invalid CIDR: got %d, want 400", st)
	}
	// Subnet outside the VPC -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "subnet", "name": "outside", "cidr": "192.168.1.0/24", "parent_id": vpc.ID,
	}); st != http.StatusBadRequest {
		t.Fatalf("subnet outside VPC: got %d, want 400", st)
	}
	// Overlapping a sibling -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "subnet", "name": "overlap", "cidr": "10.0.1.128/25", "parent_id": vpc.ID,
	}); st != http.StatusBadRequest {
		t.Fatalf("overlapping subnet: got %d, want 400", st)
	}
	// A valid, non-overlapping CIDR reaches the engine — which is
	// down in this test server — so it must be an honest 502 and
	// must NOT be persisted as a fake subnet.
	if st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "subnet", "name": "good", "cidr": "10.0.2.0/24", "parent_id": vpc.ID,
	}); st != http.StatusBadGateway {
		t.Fatalf("valid subnet with engine down: got %d %v, want 502", st, body)
	}
	res, err := srv.Resources.ListResources(context.Background(), lab)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Name == "good" {
			t.Fatalf("a subnet whose engine call failed must not be persisted")
		}
	}
	// VPC with a bad CIDR -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "vpc", "name": "badvpc", "cidr": "10.0.0.0/33",
	}); st != http.StatusBadRequest {
		t.Fatalf("bad VPC CIDR: got %d, want 400", st)
	}
}

func TestVPCRoutesSGRulesGatewaysAndTags(t *testing.T) {
	srv, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")
	lab := createReadyLab(t, ts, token)
	ctx := context.Background()
	vpc, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "vpc", Name: "main", State: "available", CIDR: "10.0.0.0/16"})
	rt, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "route-table", Name: "rt", State: "available", ParentID: vpc.ID})
	sg, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "security-group", Name: "web-sg", State: "available"})
	igw, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "gateway", Name: "igw", State: "available", Config: map[string]any{"gateway_type": "igw"}})
	inst, _ := srv.Resources.CreateResource(ctx, &store.LabResource{LabID: lab, Kind: "instance", Name: "web", State: "running"})

	// Routes: bad destination -> 400; unknown target -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+rt.ID+"/routes", token, map[string]any{"destination": "nope", "target": "local"}); st != http.StatusBadRequest {
		t.Fatalf("bad route destination: got %d, want 400", st)
	}
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+rt.ID+"/routes", token, map[string]any{"destination": "0.0.0.0/0", "target": "missing"}); st != http.StatusBadRequest {
		t.Fatalf("unknown route target: got %d, want 400", st)
	}
	// A valid route with the engine down -> 502, nothing persisted.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+rt.ID+"/routes", token, map[string]any{"destination": "0.0.0.0/0", "target": igw.ID}); st != http.StatusBadGateway {
		t.Fatalf("route with engine down: got %d, want 502", st)
	}
	got, _ := srv.Resources.GetResource(ctx, lab, rt.ID)
	if len(cfgList(got.Config, "routes")) != 0 {
		t.Fatalf("a route the engine rejected must not be persisted")
	}

	// SG rules: bad CIDR -> 400; bad port -> 400.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/rules", token, map[string]any{"direction": "ingress", "protocol": "tcp", "from_port": 22, "to_port": 22, "cidr": "bad"}); st != http.StatusBadRequest {
		t.Fatalf("bad SG CIDR: got %d, want 400", st)
	}
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/rules", token, map[string]any{"direction": "ingress", "protocol": "tcp", "from_port": 99999, "to_port": 99999, "cidr": "0.0.0.0/0"}); st != http.StatusBadRequest {
		t.Fatalf("bad SG port: got %d, want 400", st)
	}
	// A valid rule with the engine unreachable persists in the DB
	// (the DB is authoritative for validation) but records that the
	// engine snapshot was NOT applied.
	st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/rules", token, map[string]any{"direction": "ingress", "protocol": "tcp", "from_port": 22, "to_port": 22, "cidr": "0.0.0.0/0"})
	if st != http.StatusCreated {
		t.Fatalf("SG rule with engine down: got %d %v, want 201 with engine_sync note", st, body)
	}
	if cfg, ok := body["config"].(map[string]any); !ok || cfg["engine_policy_applied"] != false {
		t.Fatalf("SG rule must record engine_policy_applied=false when the engine is unreachable: %v", body)
	}
	got, _ = srv.Resources.GetResource(ctx, lab, sg.ID)
	rules := cfgList(got.Config, "rules")
	if len(rules) != 1 {
		t.Fatalf("SG rule must be persisted in the DB, got %d rules", len(rules))
	}
	ruleID, _ := rules[0]["id"].(string)
	// Attach the SG to the instance.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/attach-instance", token, map[string]any{"instance_id": inst.ID}); st != http.StatusOK {
		t.Fatalf("attach SG: got %d", st)
	}
	// Delete the rule again.
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/rules/"+ruleID, token, nil); st != http.StatusOK {
		t.Fatalf("delete SG rule: got %d", st)
	}
	// IGW attach/detach.
	if st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+igw.ID+"/attach", token, map[string]any{"vpc_id": vpc.ID}); st != http.StatusOK || body["state"] != "attached" {
		t.Fatalf("attach IGW: %d %v", st, body)
	}
	if st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources/"+igw.ID+"/detach", token, nil); st != http.StatusOK || body["state"] != "available" {
		t.Fatalf("detach IGW: %d %v", st, body)
	}
	// NAT gateway creation with the engine down -> 502, not persisted.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/resources", token, map[string]any{
		"kind": "gateway", "name": "nat", "config": map[string]any{"gateway_type": "nat"},
	}); st != http.StatusBadGateway {
		t.Fatalf("NAT with engine down: got %d, want 502", st)
	}
	// Tags: aws: prefix is rejected; valid tags persist.
	if st, _ := do(t, "PUT", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/tags", token, map[string]any{"tags": map[string]string{"aws:reserved": "x"}}); st != http.StatusBadRequest {
		t.Fatalf("reserved tag prefix: got %d, want 400", st)
	}
	if st, body := do(t, "PUT", ts.URL+"/api/v1/labs/"+lab+"/resources/"+sg.ID+"/tags", token, map[string]any{"tags": map[string]string{"env": "prod", "team": "net"}}); st != http.StatusOK {
		t.Fatalf("set tags: %d %v", st, body)
	}
}

func TestEC2StartRebootAndDefinitions(t *testing.T) {
	_, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")

	st, body := do(t, "GET", ts.URL+"/api/v1/labs/definitions", token, nil)
	if st != http.StatusOK {
		t.Fatalf("definitions: %d %v", st, body)
	}
	if defs, _ := body["definitions"].([]any); len(defs) != len(labs.Definitions) {
		t.Fatalf("definitions: got %d, want %d", len(defs), len(labs.Definitions))
	}

	lab := createReadyLab(t, ts, token)
	// Start on a READY lab is an invalid transition.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/start", token, nil); st != http.StatusConflict {
		t.Fatalf("start READY lab: got %d, want 409", st)
	}
	// Reboot with no host agent configured is an honest 503.
	if st, body := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/reboot", token, nil); st != http.StatusServiceUnavailable {
		t.Fatalf("reboot without host agent: got %d %v, want 503", st, body)
	}
	// Stop, wait for STOPPED, then start -> 202 PROVISIONING -> READY.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/stop", token, nil); st != http.StatusOK {
		t.Fatalf("stop: got %d", st)
	}
	waitLabState(t, ts, token, lab, labs.StateStopped)
	st, body = do(t, "POST", ts.URL+"/api/v1/labs/"+lab+"/start", token, nil)
	if st != http.StatusAccepted {
		t.Fatalf("start: got %d %v, want 202", st, body)
	}
	if body["state"] == labs.StateReady {
		t.Fatalf("start must not report READY before re-provisioning: %v", body)
	}
	waitLabState(t, ts, token, lab, labs.StateReady)
	// The lab's event history is real audit data.
	st, body = do(t, "GET", ts.URL+"/api/v1/labs/"+lab+"/events", token, nil)
	if st != http.StatusOK {
		t.Fatalf("lab events: %d %v", st, body)
	}
	if total, _ := body["total"].(float64); total < 1 {
		t.Fatalf("lab events must include the lifecycle events, got %v", body)
	}
}

// createReadyLab creates a lab and waits for it to reach READY
// (the test server provisions in-process with CheckNetwork=true).
func createReadyLab(t *testing.T, ts *httptest.Server, token string) string {
	t.Helper()
	st, body := do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{"definition_id": "ec2-basics", "name": "p2-test"})
	if st != http.StatusCreated && st != http.StatusAccepted {
		t.Fatalf("create lab: %d %v", st, body)
	}
	lab, _ := body["lab"].(map[string]any)
	id, _ := lab["id"].(string)
	if id == "" {
		t.Fatalf("create lab returned no id: %v", body)
	}
	waitLabState(t, ts, token, id, labs.StateReady)
	return id
}

func TestS3ObjectHandlersHonestWithoutMinIO(t *testing.T) {
	srv, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")
	// No AWS providers on the test server: every S3 call is a 503.
	if st, _ := do(t, "GET", ts.URL+"/api/v1/aws/s3/buckets", token, nil); st != http.StatusServiceUnavailable {
		t.Fatalf("buckets without providers: got %d, want 503", st)
	}
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/objects", token, map[string]any{"key": "a.txt", "content_base64": "aGk="}); st != http.StatusServiceUnavailable {
		t.Fatalf("upload without providers: got %d, want 503", st)
	}

	// Point the adapter at a dead MinIO endpoint: validation errors
	// are still 400s (they happen before any network call), while a
	// well-formed request is an honest 503.
	awsProviders, err := providers.NewAWS(providers.AWSConfig{
		MinIOEndpoint: "127.0.0.1:1", MinIOAccessKey: "minioadmin", MinIOSecretKey: "minioadmin",
	}, providers.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	srv.AWS = awsProviders
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/objects", token, map[string]any{"key": "a.txt", "content_base64": "!!!not-base64!!!"}); st != http.StatusBadRequest {
		t.Fatalf("bad base64 upload: got %d, want 400", st)
	}
	if st, _ := do(t, "POST", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/objects", token, map[string]any{"key": "a.txt", "content_base64": "aGk="}); st != http.StatusServiceUnavailable {
		t.Fatalf("upload with MinIO down: got %d, want 503", st)
	}
	if st, _ := do(t, "GET", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/objects", token, nil); st != http.StatusServiceUnavailable {
		t.Fatalf("list objects with MinIO down: got %d, want 503", st)
	}
	if st, _ := do(t, "GET", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/versioning", token, nil); st != http.StatusServiceUnavailable {
		t.Fatalf("versioning with MinIO down: got %d, want 503", st)
	}
	if st, _ := do(t, "PUT", ts.URL+"/api/v1/aws/s3/buckets/test-bucket/policy", token, map[string]any{"policy": "{bad json"}); st != http.StatusBadRequest {
		t.Fatalf("bad policy JSON: got %d, want 400", st)
	}
}

func TestMonitoringAlarmsAndAuditExport(t *testing.T) {
	srv, ts := testServer(t)
	admin := adminToken(t, srv, ts)
	token := admin // monitoring/alarms are any-authed-user; audit is admin-only
	learner := registerAs(t, ts, "learner@example.com")

	// Monitoring is live data: components + metric families.
	st, body := do(t, "GET", ts.URL+"/api/v1/monitoring", token, nil)
	if st != http.StatusOK {
		t.Fatalf("monitoring: %d %v", st, body)
	}
	if _, ok := body["components"]; !ok {
		t.Fatalf("monitoring must include live component health: %v", body)
	}
	metrics, _ := body["metrics"].([]any)
	if len(metrics) == 0 {
		t.Fatalf("monitoring must include real Prometheus series")
	}
	// An alarm on a never-observed metric is refused with the known list.
	if st, _ := do(t, "POST", ts.URL+"/api/v1/monitoring/alarms", token, map[string]any{
		"name": "nope", "metric": "cloudforge_no_such_metric", "threshold": 1, "comparison": "gt",
	}); st != http.StatusBadRequest {
		t.Fatalf("unknown metric alarm: got %d, want 400", st)
	}
	// A real alarm on a real gauge evaluates live (engine is down,
	// so engine_up == 0; gt 0.5 is ok, not alarm).
	st, body = do(t, "POST", ts.URL+"/api/v1/monitoring/alarms", token, map[string]any{
		"name": "engine-down", "metric": "cloudforge_network_engine_up", "threshold": 0.5, "comparison": "gt",
	})
	if st != http.StatusCreated {
		t.Fatalf("create alarm: %d %v", st, body)
	}
	alarmID, _ := body["id"].(string)
	st, body = do(t, "GET", ts.URL+"/api/v1/monitoring", token, nil)
	if st != http.StatusOK {
		t.Fatalf("monitoring after alarm: %d", st)
	}
	alarms, _ := body["alarms"].([]any)
	if len(alarms) != 1 {
		t.Fatalf("expected 1 alarm in monitoring, got %v", body["alarms"])
	}
	if m, ok := alarms[0].(map[string]any); !ok || m["state"] != "ok" {
		t.Fatalf("engine-down alarm with engine_up=0 and gt 0.5 must be ok, got %v", alarms[0])
	}
	if st, _ := do(t, "DELETE", ts.URL+"/api/v1/monitoring/alarms/"+alarmID, token, nil); st != http.StatusOK {
		t.Fatalf("delete alarm: got %d", st)
	}

	// Audit filters: only matching actions are returned.
	st, body = do(t, "GET", ts.URL+"/api/v1/audit?action=lab_created", admin, nil)
	if st != http.StatusOK {
		t.Fatalf("audit filter: %d %v", st, body)
	}
	entries, _ := body["audit"].([]any)
	for _, e := range entries {
		if m, ok := e.(map[string]any); !ok || m["action"] != "lab_created" {
			t.Fatalf("filtered audit returned a non-matching action: %v", e)
		}
	}
	// Export: CSV has a header + rows; JSON has events + total.
	st, raw, h := doRawText(t, "GET", ts.URL+"/api/v1/audit/export?format=csv", admin, nil)
	if st != http.StatusOK || !strings.Contains(h.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv export: %d %q", st, raw)
	}
	if !strings.HasPrefix(raw, "id,created_at,actor_id,action,resource_type,resource_id,request_id") {
		t.Fatalf("csv export must start with the header row, got %q", raw[:80])
	}
	st, raw, h = doRawText(t, "GET", ts.URL+"/api/v1/audit/export?format=json", admin, nil)
	if st != http.StatusOK || !strings.Contains(h.Get("Content-Type"), "application/json") {
		t.Fatalf("json export: %d", st)
	}
	exported := decodeBody(t, raw)
	if _, ok := exported["events"]; !ok {
		t.Fatalf("json export must carry events: %q", raw)
	}
	// Non-admins cannot export.
	if st, _, _ := doRawText(t, "GET", ts.URL+"/api/v1/audit/export?format=csv", learner, nil); st != http.StatusForbidden {
		t.Fatalf("learner export: got %d, want 403", st)
	}
}

func TestEvaluateTrailInlinePolicies(t *testing.T) {
	_, ts := testServer(t)
	token := registerAs(t, ts, "ada@example.com")
	// Inline allow + deny: the deny decides.
	st, body := do(t, "POST", ts.URL+"/api/v1/aws/iam/evaluate", token, map[string]any{
		"policies": []any{
			map[string]any{"Version": "2012-10-17", "Statement": []any{
				map[string]any{"Effect": "Allow", "Action": "s3:*", "Resource": "*"},
				map[string]any{"Effect": "Deny", "Action": "s3:DeleteBucket", "Resource": "*"},
			}},
		},
		"action": "s3:DeleteBucket", "resource": "*",
	})
	if st != http.StatusOK || body["decision"] != "Deny" {
		t.Fatalf("inline deny: %d %v", st, body)
	}
	if ms, _ := body["matched_statement"].(float64); ms != 2 {
		t.Fatalf("the deny statement (2nd) must decide, got matched_statement=%v", body["matched_statement"])
	}
	trail, _ := body["trail"].([]any)
	if len(trail) != 2 {
		t.Fatalf("trail must cover both statements, got %d", len(trail))
	}
}
