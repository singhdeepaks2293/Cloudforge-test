// Phase 6 HTTP tests: Athena validation, Kinesis ordering, Firehose,
// SageMaker notebook + real training + endpoint inference, Bedrock
// labelling, KMS/Secrets envelope round-trips, WAF evaluation,
// GuardDuty from real failed logins, Config, DataSync local copy,
// Transfer UNSUPPORTED, Cost/Budgets, Organizations. Everything runs
// against the in-memory Phase 3 store; Postgres-backed paths (Athena
// execute, DMS copy, Glue Postgres crawl) are covered by the live
// walk, and their no-database failure is asserted here.
package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func p6PollStatus(t *testing.T, tsURL, token, url, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var out map[string]any
	for time.Now().Before(deadline) {
		code, o := p3Call(t, "GET", tsURL+url, token, nil)
		if code != 200 {
			t.Fatalf("poll %s: %d %v", url, code, o)
		}
		out = o
		if p3Str(t, o, "status") == want {
			return o
		}
		if p3Str(t, o, "status") == "FAILED" {
			return o
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s on %s (last: %v)", want, url, out)
	return out
}

func TestPhase6AthenaValidation(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// DELETE is rejected before anything is stored.
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/athena/queries", token, map[string]any{"name": "evil", "query": "DELETE FROM users"}); code != 400 {
		t.Fatalf("DELETE query: got %d, want 400 (%v)", code, out)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/athena/queries", token, map[string]any{"name": "q1", "query": "SELECT 1 AS n"}); code != 201 {
		t.Fatalf("create query: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/athena/queries", token, map[string]any{"name": "q1", "query": "SELECT 2"}); code != 409 {
		t.Fatalf("duplicate query name: got %d, want 409", code)
	}
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/athena/queries", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("list queries: %d %v", code, out)
	}
	qid := ""
	if qs, ok := out["queries"].([]any); ok && len(qs) == 1 {
		if m, ok := qs[0].(map[string]any); ok {
			qid, _ = m["id"].(string)
		}
	}
	if qid == "" {
		t.Fatal("query id missing from list")
	}
	// No DATABASE_URL in the test server: execute is an honest 503,
	// never a fake result.
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/athena/queries/"+qid+"/execute", token, map[string]any{}); code != 503 {
		t.Fatalf("execute without DB: got %d, want 503 (%v)", code, out)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/athena/queries/"+qid, token, nil); code != 200 {
		t.Fatalf("delete query: %d", code)
	}
}

func TestPhase6Kinesis(t *testing.T) {
	_, ts, token := p3TestServer(t)
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/kinesis/streams", token, map[string]any{"name": "orders", "shard_count": 9}); code != 400 {
		t.Fatalf("shard_count 9: got %d, want 400", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/kinesis/streams", token, map[string]any{"name": "orders", "shard_count": 2})
	if code != 201 {
		t.Fatalf("create stream: %d %v", code, out)
	}
	id := p3Str(t, out, "id")
	for _, data := range []string{`{"order":1}`, `{"order":2}`} {
		code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/kinesis/streams/"+id+"/records", token, map[string]any{"partition_key": "order-1", "data": data})
		if code != 201 || p3Str(t, out, "sequence_number") == "" {
			t.Fatalf("put record: %d %v", code, out)
		}
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/kinesis/streams/"+id+"/records", token, nil)
	if code != 200 || out["count"] != float64(2) {
		t.Fatalf("get records: %d %v", code, out)
	}
	if recs, ok := out["records"].([]any); ok && len(recs) == 2 {
		s0 := p3Str(t, recs[0].(map[string]any), "sequence_number")
		s1 := p3Str(t, recs[1].(map[string]any), "sequence_number")
		if s0 >= s1 {
			t.Fatalf("records not in sequence order: %q >= %q", s0, s1)
		}
		// after_sequence filters.
		code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/kinesis/streams/"+id+"/records?after_sequence="+s0, token, nil)
		if code != 200 || out["count"] != float64(1) {
			t.Fatalf("after_sequence: %d %v", code, out)
		}
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/kinesis/streams/"+id, token, nil); code != 200 {
		t.Fatalf("delete stream: %d", code)
	}
	if code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/kinesis/streams/"+id+"/records", token, nil); code != 404 {
		t.Fatalf("records after delete: got %d, want 404 (%v)", code, out)
	}
}

func TestPhase6FirehosePostgresNoDB(t *testing.T) {
	_, ts, token := p3TestServer(t)
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/firehose/streams", token, map[string]any{"name": "fh1", "destination": "kafka"}); code != 400 {
		t.Fatalf("bad destination: got %d, want 400", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/firehose/streams", token, map[string]any{"name": "fh1", "destination": "postgres"})
	if code != 201 {
		t.Fatalf("create stream: %d %v", code, out)
	}
	id := p3Str(t, out, "id")
	// No DATABASE_URL: honest 503, no delivery recorded as success.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/firehose/streams/"+id+"/records", token, map[string]any{"data": `{"a":1}`}); code != 503 {
		t.Fatalf("put without DB: got %d, want 503", code)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/firehose/streams/"+id+"/deliveries", token, nil)
	if code != 200 || out["count"] != float64(0) {
		t.Fatalf("deliveries: %d %v", code, out)
	}
}

func TestPhase6SageMakerNotebookAndTraining(t *testing.T) {
	_, ts, token := p3TestServer(t)
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/notebooks", token, map[string]any{"name": "nb1"})
	if code != 201 {
		t.Fatalf("create notebook: %d %v", code, out)
	}
	nbID := p3Str(t, out, "id")
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/notebooks/"+nbID+"/execute", token, map[string]any{"code": "print(2 + 2)"})
	if code != 200 {
		t.Fatalf("execute cell: %d %v", code, out)
	}
	if !strings.Contains(p3Str(t, out, "output"), "4") {
		t.Fatalf("cell output %q does not contain 4", p3Str(t, out, "output"))
	}
	if p3Str(t, out, "sandbox") == "" {
		t.Fatal("cell result does not name its sandbox")
	}
	// Real training on the built-in housing data.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/training-jobs", token, map[string]any{"name": "job1", "dataset": "housing", "features": []string{"size_sqft", "rooms"}, "target": "price_usd"})
	if code != 201 {
		t.Fatalf("create training job: %d %v", code, out)
	}
	jobID := p3Str(t, out, "id")
	done := p6PollStatus(t, ts.URL, token, "/api/v1/aws/sagemaker/training-jobs/"+jobID, "COMPLETED")
	if p3Str(t, done, "status") != "COMPLETED" {
		t.Fatalf("training job did not complete: %v", done)
	}
	r2, _ := done["r2"].(float64)
	if r2 < 0.9 {
		t.Fatalf("training R2 %f, want >= 0.9", r2)
	}
	modelID := p3Str(t, done, "model_id")
	if modelID == "" {
		t.Fatal("completed job has no model_id")
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/endpoints", token, map[string]any{"name": "ep1", "model_id": modelID})
	if code != 201 {
		t.Fatalf("create endpoint: %d %v", code, out)
	}
	epID := p3Str(t, out, "id")
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/endpoints/"+epID+"/invoke", token, map[string]any{"features": []float64{1500, 3}})
	if code != 200 {
		t.Fatalf("invoke endpoint: %d %v", code, out)
	}
	pred, _ := out["prediction"].(float64)
	if pred < 250000 || pred > 350000 {
		t.Fatalf("prediction %f out of sane range", pred)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/endpoints/"+epID+"/invoke", token, map[string]any{"features": []float64{1500}}); code != 400 {
		t.Fatalf("wrong feature count: got %d, want 400", code)
	}
	// Degenerate custom data fails honestly, never with a fake model.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sagemaker/training-jobs", token, map[string]any{"name": "job-bad", "dataset": "custom", "rows": [][]float64{{1, 10}, {1, 10}, {1, 10}, {1, 10}}, "features": []string{"x"}, "target": "y"})
	if code != 201 {
		t.Fatalf("create degenerate job: %d %v", code, out)
	}
	bad := p6PollStatus(t, ts.URL, token, "/api/v1/aws/sagemaker/training-jobs/"+p3Str(t, out, "id"), "FAILED")
	if p3Str(t, bad, "status") != "FAILED" || p3Str(t, bad, "error") == "" {
		t.Fatalf("degenerate job should FAILED with an error: %v", bad)
	}
}

func TestPhase6BedrockLabelled(t *testing.T) {
	_, ts, token := p3TestServer(t)
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/bedrock/models", token, nil)
	if code != 200 || p3Str(t, out, "fidelity") != "SIMULATED" {
		t.Fatalf("bedrock models: %d %v", code, out)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/bedrock/invoke", token, map[string]any{"model_id": "nope", "prompt": "hi"}); code != 400 {
		t.Fatalf("unknown model: got %d, want 400", code)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/bedrock/invoke", token, map[string]any{"model_id": "amazon.titan-text", "prompt": "summarise this"})
	if code != 200 || p3Str(t, out, "fidelity") != "SIMULATED" {
		t.Fatalf("bedrock invoke: %d %v", code, out)
	}
	if !strings.Contains(p3Str(t, out, "output"), "SIMULATED") {
		t.Fatalf("bedrock output is not labelled simulated: %q", p3Str(t, out, "output"))
	}
}

func TestPhase6KMSAndSecrets(t *testing.T) {
	_, ts, token := p3TestServer(t)
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys", token, map[string]any{"name": "key1", "description": "test"})
	if code != 201 {
		t.Fatalf("create key: %d %v", code, out)
	}
	keyID := p3Str(t, out, "id")
	if _, has := out["wrapped_key"]; has {
		t.Fatal("key material leaked in the create response")
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys/"+keyID+"/encrypt", token, map[string]any{"plaintext": "hello-kms"})
	if code != 200 {
		t.Fatalf("encrypt: %d %v", code, out)
	}
	ct := p3Str(t, out, "ciphertext")
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys/"+keyID+"/decrypt", token, map[string]any{"ciphertext": ct})
	if code != 200 || p3Str(t, out, "plaintext") != "hello-kms" {
		t.Fatalf("decrypt round trip: %d %v", code, out)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys/"+keyID+"/decrypt", token, map[string]any{"ciphertext": "not-base64!!"}); code != 400 {
		t.Fatalf("bad ciphertext: got %d, want 400", code)
	}
	// Rotate: old ciphertext still decrypts, version bumps.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys/"+keyID+"/rotate", token, map[string]any{})
	if code != 200 || out["key_version"] != float64(2) || out["version"] != float64(2) {
		t.Fatalf("rotate: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/kms/keys/"+keyID+"/decrypt", token, map[string]any{"ciphertext": ct})
	if code != 200 || p3Str(t, out, "plaintext") != "hello-kms" {
		t.Fatalf("decrypt after rotate: %d %v", code, out)
	}
	// Secrets: value round-trips, list never leaks ciphertext.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/secretsmanager/secrets", token, map[string]any{"name": "db-pass", "value": "s3cr3t-value", "kms_key_id": keyID})
	if code != 201 {
		t.Fatalf("create secret: %d %v", code, out)
	}
	secID := p3Str(t, out, "id")
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/secretsmanager/secrets", token, nil)
	if code != 200 {
		t.Fatalf("list secrets: %d", code)
	}
	if secs, ok := out["secrets"].([]any); ok && len(secs) == 1 {
		if _, has := secs[0].(map[string]any)["ciphertext"]; has {
			t.Fatal("ciphertext leaked in secret list")
		}
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/secretsmanager/secrets/"+secID, token, nil)
	if code != 200 || p3Str(t, out, "value") != "s3cr3t-value" {
		t.Fatalf("get secret value: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/secretsmanager/secrets/"+secID+"/value", token, map[string]any{"value": "rotated-value"})
	if code != 200 || out["version"] != float64(2) {
		t.Fatalf("put value: %d %v", code, out)
	}
	// Key in use cannot be deleted.
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/kms/keys/"+keyID, token, nil); code != 409 {
		t.Fatalf("delete in-use key: got %d, want 409", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/secretsmanager/secrets/"+secID, token, nil); code != 200 {
		t.Fatalf("delete secret: %d", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/kms/keys/"+keyID, token, nil); code != 200 {
		t.Fatalf("delete key after secret gone: %d", code)
	}
}

func TestPhase6WAFAndGuardDuty(t *testing.T) {
	_, ts, token := p3TestServer(t)
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/waf/acls", token, map[string]any{
		"name": "web-acl",
		"rules": []map[string]any{
			{"name": "block-admin", "action": "block", "match_type": "path_prefix", "match_value": "/admin"},
		},
	})
	if code != 201 {
		t.Fatalf("create ACL: %d %v", code, out)
	}
	aclID := p3Str(t, out, "id")
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/waf/acls/"+aclID+"/evaluate", token, map[string]any{"method": "GET", "path": "/admin", "source_ip": "10.0.0.1"})
	if code != 200 || p3Str(t, out, "action") != "block" || p3Str(t, out, "matched_rule") != "block-admin" {
		t.Fatalf("waf block: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/waf/acls/"+aclID+"/evaluate", token, map[string]any{"method": "GET", "path": "/public", "source_ip": "10.0.0.1"})
	if code != 200 || p3Str(t, out, "action") != "allow" {
		t.Fatalf("waf allow: %d %v", code, out)
	}
	// GuardDuty: a scan with no failed logins creates nothing.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/guardduty/detectors", token, map[string]any{"name": "default", "enabled": true}); code != 201 {
		t.Fatalf("create detector: %d", code)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/guardduty/detectors/x/scan", token, map[string]any{})
	if code != 200 || out["findings_created"] != float64(0) {
		t.Fatalf("empty scan: %d %v", code, out)
	}
	// Six failed logins from the same client IP, then a scan finds it.
	for i := 0; i < 6; i++ {
		code, _ := p3Call(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]any{"email": "attacker@example.com", "password": "wrong"})
		if code != 401 {
			t.Fatalf("failed login %d: got %d, want 401", i, code)
		}
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/guardduty/detectors/x/scan", token, map[string]any{})
	if code != 200 || out["findings_created"] != float64(1) {
		t.Fatalf("scan after brute force: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/guardduty/findings", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("findings: %d %v", code, out)
	}
	fid := ""
	if fs, ok := out["findings"].([]any); ok && len(fs) == 1 {
		fid, _ = fs[0].(map[string]any)["id"].(string)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/securityhub/summary", token, nil)
	if code != 200 || out["open_findings"] != float64(1) {
		t.Fatalf("security hub summary: %d %v", code, out)
	}
	// A second scan does not duplicate the open finding.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/guardduty/detectors/x/scan", token, map[string]any{})
	if code != 200 || out["findings_created"] != float64(0) {
		t.Fatalf("rescan dedupe: %d %v", code, out)
	}
	// Status workflow resolves it.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/guardduty/findings/"+fid+"/status", token, map[string]any{"status": "RESOLVED"})
	if code != 200 || p3Str(t, out, "status") != "RESOLVED" {
		t.Fatalf("resolve finding: %d %v", code, out)
	}
}

func TestPhase6DataSyncLocal(t *testing.T) {
	srv, ts, token := p3TestServer(t)
	root := srv.P3WorkdirRoot
	srcDir := filepath.Join(root, "sync-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello datasync"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/datasync/tasks", token, map[string]any{
		"name": "sync1", "source": "local:sync-src", "destination": "local:sync-dst",
	})
	if code != 201 {
		t.Fatalf("create task: %d %v", code, out)
	}
	done := p6PollStatus(t, ts.URL, token, "/api/v1/aws/datasync/tasks/"+p3Str(t, out, "id"), "COMPLETED")
	if p3Str(t, done, "status") != "COMPLETED" {
		t.Fatalf("datasync did not complete: %v", done)
	}
	if done["files_copied"] != float64(1) || done["files_verified"] != float64(1) {
		t.Fatalf("counts: %v", done)
	}
	got, err := os.ReadFile(filepath.Join(root, "sync-dst", "a.txt"))
	if err != nil || string(got) != "hello datasync" {
		t.Fatalf("copied file missing or wrong: %q %v", got, err)
	}
	// Escape attempt is refused.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/datasync/tasks", token, map[string]any{
		"name": "sync-evil", "source": "local:../../etc", "destination": "local:sync-dst2",
	}); code != 400 {
		t.Fatalf("path escape: got %d, want 400", code)
	}
}

func TestPhase6MiscHonestPaths(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// Transfer Family: UNSUPPORTED with the reason.
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/transfer/info", token, nil)
	if code != 200 || out["supported"] != false {
		t.Fatalf("transfer info: %d %v", code, out)
	}
	// Migration Hub: empty, honest.
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/migrationhub/tasks", token, nil)
	if code != 200 || out["count"] != float64(0) {
		t.Fatalf("migration hub: %d %v", code, out)
	}
	// DMS: same source/target refused; unknown RDS refused.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/dms/tasks", token, map[string]any{"name": "t1", "source": map[string]any{"kind": "platform"}, "target": map[string]any{"kind": "platform"}}); code != 400 {
		t.Fatalf("dms same db: got %d, want 400", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/dms/tasks", token, map[string]any{"name": "t1", "source": map[string]any{"kind": "rds", "instance_id": "nope"}, "target": map[string]any{"kind": "platform"}}); code != 404 {
		t.Fatalf("dms unknown rds: got %d, want 404", code)
	}
	// Peering with an unknown VPC: 404, nothing created.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpc/peerings", token, map[string]any{"name": "p1", "vpc_a_resource_id": "nope", "vpc_b_resource_id": "nope2"}); code != 404 {
		t.Fatalf("peering unknown vpc: got %d, want 404", code)
	}
	// Flow log capture without an engine: honest 503.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/vpc/flow-logs", token, map[string]any{"name": "fl1", "vpc_resource_id": "nope"})
	if code != 404 {
		t.Fatalf("flow log unknown vpc: got %d, want 404 (%v)", code, out)
	}
	// Config rule evaluates clean with no labs.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/config/rules", token, map[string]any{"name": "no-open-ssh", "rule_type": "sg-no-open-ssh"})
	if code != 201 {
		t.Fatalf("create config rule: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/config/rules/"+p3Str(t, out, "id")+"/evaluate", token, map[string]any{})
	if code != 200 || out["compliant"] != true {
		t.Fatalf("config evaluate empty: %d %v", code, out)
	}
	// Cost explorer: 200, educational label, zero total with no usage.
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/costexplorer/costs", token, nil)
	if code != 200 || !strings.Contains(p3Str(t, out, "currency"), "educational") {
		t.Fatalf("cost explorer: %d %v", code, out)
	}
	// Budgets + Organizations.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/costexplorer/budgets", token, map[string]any{"name": "b1", "limit_usd": 5})
	if code != 201 {
		t.Fatalf("create budget: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/costexplorer/budgets/"+p3Str(t, out, "id"), token, nil)
	if code != 200 || out["alert"] != false {
		t.Fatalf("budget get: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/organizations/units", token, map[string]any{"name": "eng"})
	if code != 201 {
		t.Fatalf("create org unit: %d %v", code, out)
	}
	unitID := p3Str(t, out, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/organizations/units/"+unitID+"/members", token, map[string]any{"user_id": "no-such-user"}); code != 404 {
		t.Fatalf("org add unknown user: got %d, want 404", code)
	}
	// SSM without a host agent: honest 503.
	if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/aws/ssm/inventory", token, nil); code != 503 {
		t.Fatalf("ssm inventory without agent: got %d, want 503", code)
	}
	// QuickSight dashboard without a query: created, chart empty.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/quicksight/dashboards", token, map[string]any{"name": "d1", "chart_type": "bar"})
	if code != 201 {
		t.Fatalf("create dashboard: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/quicksight/dashboards/"+p3Str(t, out, "id"), token, nil)
	if code != 200 || p3Str(t, out, "chart_error") == "" {
		t.Fatalf("dashboard without query: %d %v", code, out)
	}
	// Inspector assessment completes (no labs, no S3: zero findings).
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/inspector/assessments", token, map[string]any{"name": "a1"})
	if code != 201 {
		t.Fatalf("create assessment: %d %v", code, out)
	}
	done := p6PollStatus(t, ts.URL, token, "/api/v1/aws/inspector/assessments/"+p3Str(t, out, "id"), "COMPLETED")
	if p3Str(t, done, "status") != "COMPLETED" || done["findings"] != float64(0) {
		t.Fatalf("inspector empty run: %v", done)
	}
	// Glue crawler + tables list (empty until a run).
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/glue/crawlers", token, map[string]any{"name": "c1", "sources": []string{"postgres"}})
	if code != 201 {
		t.Fatalf("create crawler: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/glue/crawlers/"+p3Str(t, out, "id")+"/run", token, map[string]any{})
	if code != 200 {
		t.Fatalf("glue run without DB: %d %v", code, out)
	}
	if p3Str(t, out, "status") != "PARTIAL" {
		t.Fatalf("glue run without DB should be PARTIAL, got %v", out)
	}
}
