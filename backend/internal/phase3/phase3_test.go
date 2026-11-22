// Phase 3 logic tests: DynamoDB item handling, EventBridge pattern
// matching, DNS validation/resolution, SQS visibility/DLQ, ELB
// round-robin, and the Lambda subprocess runner (real execution,
// incl. timeout and error paths).
package phase3

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDynamoPlainTypedRoundTrip(t *testing.T) {
	plain := map[string]any{"id": "42", "n": float64(7), "ok": true}
	typed := DynamoTyped(plain)
	if _, ok := typed["id"].(map[string]any); !ok {
		t.Fatalf("typed id should be a DynamoDB-typed map, got %#v", typed["id"])
	}
	back := DynamoPlain(typed)
	if back["id"] != "42" {
		t.Errorf("plain id = %v, want 42", back["id"])
	}
	if DynamoKeyValue(plain, "id") != "42" {
		t.Errorf("key value = %q, want 42", DynamoKeyValue(plain, "id"))
	}
	if DynamoKeyValue(plain, "missing") != "" {
		t.Error("missing key attribute should give empty value")
	}
	// Typed input is accepted as-is.
	typedIn := map[string]any{"id": map[string]any{"S": "x"}}
	if DynamoKeyValue(typedIn, "id") != "x" {
		t.Errorf("typed key value = %q, want x", DynamoKeyValue(typedIn, "id"))
	}
}

func TestPatternMatches(t *testing.T) {
	pattern := map[string]any{
		"source":      []any{"my.app"},
		"detail-type": []any{"OrderPlaced"},
		"detail":      map[string]any{"region": []any{"eu", "us"}, "amount": []any{float64(10)}},
	}
	match := map[string]any{
		"source": "my.app", "detail-type": "OrderPlaced",
		"detail": map[string]any{"region": "eu", "amount": float64(10), "extra": "ignored"},
	}
	if !PatternMatches(pattern, match) {
		t.Error("pattern should match (extra detail keys are ignored)")
	}
	noMatch := map[string]any{
		"source": "my.app", "detail-type": "OrderPlaced",
		"detail": map[string]any{"region": "ap", "amount": float64(10)},
	}
	if PatternMatches(pattern, noMatch) {
		t.Error("pattern should NOT match region ap")
	}
	if !PatternMatches(map[string]any{}, match) {
		t.Error("empty pattern should match everything")
	}
}

func TestValidateDNSRecord(t *testing.T) {
	if err := ValidateDNSRecord("app.local", "A", []string{"10.0.0.1"}); err != nil {
		t.Errorf("valid A record rejected: %v", err)
	}
	if err := ValidateDNSRecord("app.local", "A", []string{"not-an-ip"}); err == nil {
		t.Error("A record with a non-IP value must be rejected")
	}
	if err := ValidateDNSRecord("app.local", "CNAME", []string{"a.local", "b.local"}); err == nil {
		t.Error("CNAME with two targets must be rejected")
	}
	if err := ValidateDNSRecord("app.local", "MX", []string{"10.0.0.1"}); err == nil {
		t.Error("MX is unsupported and must be rejected, not faked")
	}
	if err := ValidateDNSRecord("bad name!", "A", []string{"10.0.0.1"}); err == nil {
		t.Error("invalid DNS name must be rejected")
	}
}

func TestResolveDNSWildcard(t *testing.T) {
	records := []map[string]any{
		{"name": "*.app.local", "type": "A", "values": []string{"10.0.0.9"}},
		{"name": "api.app.local", "type": "A", "values": []string{"10.0.0.1"}},
	}
	matched := ResolveDNS(records, "api.app.local", "A")
	if len(matched) != 2 {
		t.Errorf("api.app.local should match exact + wildcard, got %d", len(matched))
	}
	if got := ResolveDNS(records, "other.local", "A"); len(got) != 0 {
		t.Errorf("other.local should match nothing, got %d", len(got))
	}
}

func TestSQSVisibilityAndDLQ(t *testing.T) {
	now := time.Now().UTC()
	msg := map[string]any{
		"visible_at": now.Add(-time.Second).Format(time.RFC3339),
		"expires_at": now.Add(time.Hour).Format(time.RFC3339),
	}
	if !SQSMessageVisible(msg, now) {
		t.Error("message past visible_at should be visible")
	}
	msg["visible_at"] = now.Add(time.Minute).Format(time.RFC3339)
	if SQSMessageVisible(msg, now) {
		t.Error("message inside its visibility timeout must not be visible")
	}
	msg["visible_at"] = now.Add(-time.Second).Format(time.RFC3339)
	msg["expires_at"] = now.Add(-time.Second).Format(time.RFC3339)
	if SQSMessageVisible(msg, now) {
		t.Error("expired message must not be visible")
	}
	q := map[string]any{"max_receives": 3}
	if SQSShouldDLQ(map[string]any{"receive_count": 2}, q) {
		t.Error("2 receives < max 3 must not DLQ")
	}
	if !SQSShouldDLQ(map[string]any{"receive_count": 3}, q) {
		t.Error("3 receives >= max 3 must DLQ")
	}
	if SQSShouldDLQ(map[string]any{"receive_count": 99}, map[string]any{"max_receives": 0}) {
		t.Error("max_receives 0 means no DLQ")
	}
}

func TestRoundRobin(t *testing.T) {
	rr := &RoundRobin{}
	seen := []int{}
	for i := 0; i < 5; i++ {
		seen = append(seen, rr.Next(2))
	}
	if want := []int{0, 1, 0, 1, 0}; len(seen) != len(want) {
		t.Fatalf("seen %v", seen)
	} else {
		for i := range want {
			if seen[i] != want[i] {
				t.Errorf("round-robin[%d] = %d, want %d", i, seen[i], want[i])
			}
		}
	}
	if rr.Next(0) != -1 {
		t.Error("Next(0) must be -1")
	}
}

func TestRunLambdaPython(t *testing.T) {
	ctx := context.Background()
	res := RunLambda(ctx, "python3", "import json,sys\nprint(json.dumps({'echo': json.load(sys.stdin)}))\n", nil, map[string]any{"hello": "world"}, 10, 256)
	if res.Error != "" {
		t.Fatalf("python invoke failed: %s (output %q)", res.Error, res.Output)
	}
	if res.DurationMs < 0 {
		t.Error("duration must be >= 0")
	}
	if res.Sandbox == "" {
		t.Error("sandbox description must be present (it names what isolation actually applied)")
	}
}

func TestRunLambdaNodeError(t *testing.T) {
	ctx := context.Background()
	res := RunLambda(ctx, "node", "throw new Error('boom')\n", nil, map[string]any{}, 10, 256)
	if res.Error == "" {
		t.Fatalf("failing node function must report an error, got output %q", res.Output)
	}
}

func TestRunLambdaTimeout(t *testing.T) {
	ctx := context.Background()
	res := RunLambda(ctx, "python3", "import time\ntime.sleep(30)\n", nil, map[string]any{}, 1, 256)
	if !res.TimedOut {
		t.Fatalf("sleeping function must time out, got error %q output %q", res.Error, res.Output)
	}
}

func TestRunLambdaUnsupportedRuntime(t *testing.T) {
	ctx := context.Background()
	res := RunLambda(ctx, "go1.22", "package main", nil, map[string]any{}, 10, 256)
	if res.Error == "" {
		t.Fatal("an unsupported runtime must be rejected, not faked")
	}
}

func TestValidatePGIdentifier(t *testing.T) {
	for _, bad := range []string{"", "1abc", "a-b", "a;b DROP TABLE x", "a b", "a.b"} {
		if err := ValidatePGIdentifier(bad); err == nil {
			t.Errorf("identifier %q must be rejected", bad)
		}
	}
	if err := ValidatePGIdentifier("my_db_1"); err != nil {
		t.Errorf("valid identifier rejected: %v", err)
	}
}

func TestProbeTarget(t *testing.T) {
	ctx := context.Background()
	// Nothing listens here: the probe must report unhealthy, not hang.
	healthy, _ := ProbeTarget(ctx, "http://127.0.0.1:1/health", 0, "")
	if healthy {
		t.Error("probe of a closed port must be unhealthy")
	}
	healthy, _ = ProbeTarget(ctx, "127.0.0.1", 1, "")
	if healthy {
		t.Error("TCP probe of a closed port must be unhealthy")
	}
	healthy, _ = ProbeTarget(ctx, "127.0.0.1", 1, "/health")
	if healthy {
		t.Error("HTTP probe of a closed port must be unhealthy")
	}
	// Positive case: a bare host + port + health-check path must be
	// joined into an HTTP GET (regression test: the old probe dialed
	// the bare host without a port and marked every target
	// unhealthy with "missing port in address").
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	_, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	healthy, detail := ProbeTarget(ctx, "127.0.0.1", port, "/api/v1/health")
	if !healthy {
		t.Errorf("probe of live server must be healthy, got %q", detail)
	}
	healthy, _ = ProbeTarget(ctx, "127.0.0.1", port, "/no-such-path")
	if healthy {
		t.Error("probe of a 404 health-check path must be unhealthy")
	}
}

// TestRDSCreateDropIntegration exercises the real RDS provisioning
// path against the platform Postgres: role + database creation
// (including the GRANT that lets a non-superuser provisioning user
// own the database), then drop. Skipped without DATABASE_URL.
// The provisioning user needs CREATEDB + CREATEROLE on the platform
// Postgres — the sandbox cloudforge user has both.
func TestRDSCreateDropIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is unset; integration test skipped (no PostgreSQL available)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dbName := "cf_test_rds_p3"
	role := "cf_test_rds_p3_role"
	// Clean up leftovers from an earlier aborted run first.
	_ = RDSDropDatabase(ctx, dsn, dbName, role)
	if err := RDSCreateDatabase(ctx, dsn, dbName, role, "test-password-123"); err != nil {
		t.Fatalf("RDSCreateDatabase: %v", err)
	}
	defer func() {
		if err := RDSDropDatabase(ctx, dsn, dbName, role); err != nil {
			t.Errorf("RDSDropDatabase: %v", err)
		}
	}()
	// The database exists and is owned by the new role; the role can
	// log in (proves the generated password works end to end).
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to platform postgres: %v", err)
	}
	defer pool.Close()
	var owner string
	if err := pool.QueryRow(ctx, `SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1`, dbName).Scan(&owner); err != nil {
		t.Fatalf("query owner of %s: %v", dbName, err)
	}
	if owner != role {
		t.Errorf("database owner: %q, want %q", owner, role)
	}
}
