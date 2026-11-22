// Phase 3 HTTP tests: DynamoDB (Postgres subset — DynamoDB Local
// is pointed at a dead port), SQS/SNS/EventBridge durability
// semantics, Lambda subprocess execution, API Gateway routing, ELB
// health + round-robin failure paths, Route 53, and the honest
// failure paths for RDS (no DATABASE_URL) and ElastiCache (dead
// Redis). Everything runs against the in-memory Phase 3 store.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func p3TestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	srv := NewServerWithDeps(Deps{
		P3:               store.NewMemoryPhase3Store(),
		P3DatabaseURL:    "",
		P3RedisAddr:      "localhost:1",
		P3DynamoEndpoint: "http://localhost:1",
		P3WorkdirRoot:    t.TempDir(),
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts, adminToken(t, srv, ts)
}

func p3Call(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	if body == nil {
		code, out := doRawBody(t, method, url, token, "")
		return code, out
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return doRawBody(t, method, url, token, string(raw))
}

func p3Str(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, _ := m[key].(string)
	return v
}

func TestPhase3DynamoDBSubset(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// Create table, then duplicate -> 409.
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/dynamodb/tables", token, map[string]any{"name": "users", "key_attribute": "id"})
	if code != 201 {
		t.Fatalf("create table: %d %v", code, out)
	}
	if p3Str(t, out, "backend") != "postgres-subset" {
		t.Errorf("backend = %q, want postgres-subset (DynamoDB Local is pointed at a dead port)", p3Str(t, out, "backend"))
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/dynamodb/tables", token, map[string]any{"name": "users", "key_attribute": "id"}); code != 409 {
		t.Errorf("duplicate table: got %d, want 409", code)
	}
	// Put without the key -> 400 (nothing stored).
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/dynamodb/tables/users/items", token, map[string]any{"item": map[string]any{"name": "noid"}}); code != 400 {
		t.Errorf("put without key: got %d, want 400", code)
	}
	// Put, get, query, scan, delete.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/dynamodb/tables/users/items", token, map[string]any{"item": map[string]any{"id": "1", "name": "ada"}}); code != 201 {
		t.Fatalf("put item: %d", code)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/dynamodb/tables/users/items/1?key_attribute=id", token, nil)
	if code != 200 {
		t.Fatalf("get item: %d %v", code, out)
	}
	if item, _ := out["item"].(map[string]any); item["name"] != "ada" {
		t.Errorf("get item returned %#v", out["item"])
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/dynamodb/tables/users/query?key=1&key_attribute=id", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Errorf("query: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/dynamodb/tables/users/items", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Errorf("scan: %d %v", code, out)
	}
	if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/aws/dynamodb/tables/users/items/99?key_attribute=id", token, nil); code != 404 {
		t.Errorf("get missing item: got %d, want 404", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/dynamodb/tables/users/items/1?key_attribute=id", token, nil); code != 200 {
		t.Errorf("delete item: got %d, want 200", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/dynamodb/tables/users", token, nil); code != 200 {
		t.Errorf("delete table: got %d, want 200", code)
	}
}

func TestPhase3SQSVisibilityDLQRedrive(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// DLQ first, then the source queue pointing at it (max 2 receives).
	_, dlq := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{"name": "orders-dlq"})
	dlqID := p3Str(t, dlq, "id")
	_, src := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{
		"name": "orders", "visibility_timeout_seconds": 1, "max_receives": 2, "dead_letter_queue_id": dlqID,
	})
	srcID := p3Str(t, src, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{"name": "orders"}); code != 409 {
		t.Errorf("duplicate queue: got %d, want 409", code)
	}
	_, msg := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/messages", token, map[string]any{"body": "order-1"})
	msgID := p3Str(t, msg, "message_id")
	// Receive 1: visible, count 1, then invisible for 1s.
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/receive", token, map[string]any{"max_messages": 1})
	if code != 200 {
		t.Fatalf("receive 1: %d %v", code, out)
	}
	if out["count"] != float64(1) {
		t.Fatalf("receive 1 count = %v, want 1", out["count"])
	}
	// Immediate second receive: nothing visible (visibility timeout).
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/receive", token, map[string]any{"max_messages": 1})
	if code != 200 || out["count"] != float64(0) {
		t.Errorf("receive during visibility timeout: %d count %v, want 200/0", code, out["count"])
	}
	time.Sleep(1100 * time.Millisecond)
	// Receive 2: count hits max -> moves to DLQ, not returned.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/receive", token, map[string]any{"max_messages": 1})
	if code != 200 || out["count"] != float64(0) || out["moved_to_dead_letter_queue"] != float64(1) {
		t.Errorf("receive 2 (DLQ move): %d %v", code, out)
	}
	// The DLQ now holds the message; the source is empty.
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues/"+dlqID+"/messages", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Errorf("dlq messages: %d %v", code, out)
	}
	// Redrive moves it back; then delete it by receipt handle.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/redrive", token, nil)
	if code != 200 || out["messages_moved"] != float64(1) {
		t.Errorf("redrive: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/receive", token, map[string]any{"max_messages": 1, "visibility_timeout_seconds": 30})
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("receive after redrive: %d %v", code, out)
	}
	msgs, _ := out["messages"].([]any)
	receipt, _ := msgs[0].(map[string]any)["receipt_handle"].(string)
	if receipt == "" {
		t.Fatal("receive must return a receipt handle")
	}
	if receipt != msgID {
		t.Logf("note: redriven message got a new id %s (original %s) — redrive re-enqueues", receipt, msgID)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/messages/"+receipt, token, nil); code != 200 {
		t.Errorf("delete message: got %d, want 200", code)
	}
	// Send with an empty body -> 400.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+srcID+"/messages", token, map[string]any{"body": "  "}); code != 400 {
		t.Errorf("empty send: got %d, want 400", code)
	}
}

func TestPhase3SNSFanoutToSQS(t *testing.T) {
	_, ts, token := p3TestServer(t)
	_, q := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{"name": "notify-q"})
	qID := p3Str(t, q, "id")
	_, topic := p3Call(t, "POST", ts.URL+"/api/v1/aws/sns/topics", token, map[string]any{"name": "alerts"})
	topicID := p3Str(t, topic, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sns/topics/"+topicID+"/subscribe", token, map[string]any{"protocol": "sqs", "endpoint": qID}); code != 201 {
		t.Fatalf("subscribe sqs: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sns/topics/"+topicID+"/subscribe", token, map[string]any{"protocol": "email", "endpoint": "a@b.c"}); code != 400 {
		t.Errorf("email subscribe: got %d, want 400 (not built)", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sns/topics/"+topicID+"/subscribe", token, map[string]any{"protocol": "http", "endpoint": "http://example.com/hook"}); code != 400 {
		t.Errorf("non-loopback http subscribe: got %d, want 400 (SSRF guard)", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/sns/topics/"+topicID+"/publish", token, map[string]any{"message": "hello", "subject": "test"})
	if code != 200 {
		t.Fatalf("publish: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues/"+qID+"/messages", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Errorf("fan-out message in queue: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/sns/topics/"+topicID+"/deliveries", token, nil)
	if code != 200 {
		t.Fatalf("deliveries: %d", code)
	}
	if deliveries, _ := out["deliveries"].([]any); len(deliveries) != 1 || deliveries[0].(map[string]any)["status"] != "delivered" {
		t.Errorf("deliveries = %v, want one delivered", out["deliveries"])
	}
}

func TestPhase3EventBridgeToSQS(t *testing.T) {
	_, ts, token := p3TestServer(t)
	_, q := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{"name": "eb-q"})
	qID := p3Str(t, q, "id")
	code, rule := p3Call(t, "POST", ts.URL+"/api/v1/aws/events/rules", token, map[string]any{
		"name": "orders-rule", "source": "my.app", "detail_type": "OrderPlaced",
		"targets": []any{map[string]any{"type": "sqs", "queue_id": qID}},
	})
	if code != 201 {
		t.Fatalf("create rule: %d %v", code, rule)
	}
	ruleID := p3Str(t, rule, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/events/rules", token, map[string]any{
		"name": "bad-rule", "source": "x", "detail_type": "y",
		"targets": []any{map[string]any{"type": "sqs", "queue_id": "no-such-queue"}},
	}); code != 404 {
		t.Errorf("rule with a missing target: got %d, want 404", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/events/put-events", token, map[string]any{
		"entries": []any{
			map[string]any{"source": "my.app", "detail-type": "OrderPlaced", "detail": map[string]any{"id": 1}},
			map[string]any{"source": "other", "detail-type": "OrderPlaced", "detail": map[string]any{"id": 2}},
		},
	})
	if code != 200 {
		t.Fatalf("put events: %d %v", code, out)
	}
	results, _ := out["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v", out["results"])
	}
	if len(results[0].(map[string]any)["matched_rules"].([]any)) != 1 || len(results[1].(map[string]any)["matched_rules"].([]any)) != 0 {
		t.Errorf("matching wrong: %#v", results)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues/"+qID+"/messages", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Errorf("event delivery to queue: %d %v", code, out)
	}
	// Disable the rule: the next matching event delivers nothing.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/events/rules/"+ruleID+"/state", token, map[string]any{"state": "DISABLED"}); code != 200 {
		t.Errorf("disable rule: got %d, want 200", code)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/events/put-events", token, map[string]any{
		"entries": []any{map[string]any{"source": "my.app", "detail-type": "OrderPlaced", "detail": map[string]any{"id": 3}}},
	})
	results, _ = out["results"].([]any)
	if len(results[0].(map[string]any)["matched_rules"].([]any)) != 0 {
		t.Error("disabled rule must not match")
	}
}

func TestPhase3LambdaAndAPIGateway(t *testing.T) {
	_, ts, token := p3TestServer(t)
	pyCode := "import json,sys\nevent=json.load(sys.stdin)\nval=event.get('ping') or (event.get('bodyJson') or {}).get('ping')\nprint(json.dumps({'got': val}))\n"
	code, fn := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions", token, map[string]any{
		"name": "echoer", "runtime": "python3", "handler": "handler.handle", "code": pyCode,
		"timeout_seconds": 10, "memory_mb": 256,
	})
	if code != 201 {
		t.Fatalf("create function: %d %v", code, fn)
	}
	fnID := p3Str(t, fn, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions", token, map[string]any{
		"name": "bad", "runtime": "go1.22", "code": "package main",
	}); code != 400 {
		t.Errorf("unsupported runtime: got %d, want 400", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/invoke", token, map[string]any{"event": map[string]any{"ping": "pong"}})
	if code != 200 {
		t.Fatalf("invoke: %d %v", code, out)
	}
	if !strings.Contains(p3Str(t, out, "output"), "pong") {
		t.Errorf("invoke output = %q, want pong", p3Str(t, out, "output"))
	}
	if p3Str(t, out, "sandbox") == "" {
		t.Error("invoke must name the sandbox that applied")
	}
	// A failing function reports its error as data (200), not a 500.
	if code, _ := p3Call(t, "PUT", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/code", token, map[string]any{"code": "raise RuntimeError('kaboom')\n"}); code != 200 {
		t.Fatalf("update code: %d", code)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/invoke", token, map[string]any{"event": map[string]any{}})
	if code != 200 || p3Str(t, out, "error") == "" {
		t.Errorf("failing invoke: %d %v (want 200 with an error field)", code, out)
	}
	// Alias to version 2, then invoke by alias.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/aliases", token, map[string]any{"name": "live", "version": 2}); code != 201 {
		t.Errorf("create alias: got %d, want 201", code)
	}
	// API Gateway: API -> resource (GET -> lambda) -> deploy -> invoke.
	_, api := p3Call(t, "POST", ts.URL+"/api/v1/aws/apigateway/apis", token, map[string]any{"name": "echo-api", "protocol": "REST"})
	apiID := p3Str(t, api, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/apigateway/apis/"+apiID+"/resources", token, map[string]any{
		"path":    "/echo",
		"methods": []any{map[string]any{"method": "POST", "integration": map[string]any{"type": "lambda", "function_id": fnID}}},
	}); code != 201 {
		t.Fatalf("create resource: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/apigateway/apis/"+apiID+"/deploy", token, map[string]any{"stage_name": "prod"}); code != 201 {
		t.Fatalf("deploy: %d", code)
	}
	// The function currently raises; fix it, redeploy, invoke via gateway.
	if code, _ := p3Call(t, "PUT", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/code", token, map[string]any{"code": pyCode}); code != 200 {
		t.Fatalf("restore code: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/apigateway/apis/"+apiID+"/deploy", token, map[string]any{"stage_name": "prod"}); code != 200 {
		t.Fatalf("redeploy: %d", code)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/gateway/"+apiID+"/prod/echo", token, map[string]any{"ping": "via-gateway"})
	if code != 200 {
		t.Fatalf("gateway invoke: %d %v", code, out)
	}
	if !strings.Contains(fmt.Sprintf("%v", out), "via-gateway") {
		t.Errorf("gateway invoke output = %v, want via-gateway", out)
	}
	// Undeployed stage -> 404, never a fake success.
	if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/gateway/"+apiID+"/dev/echo", token, nil); code != 404 {
		t.Errorf("undeployed stage: got %d, want 404", code)
	}
	// Logs were recorded.
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/apigateway/apis/"+apiID+"/logs", token, nil)
	if code != 200 {
		t.Fatalf("gateway logs: %d", code)
	}
	if logs, _ := out["logs"].([]any); len(logs) == 0 {
		t.Error("gateway logs must contain the invocations")
	}
	// SQS trigger: send -> function invoked asynchronously.
	_, q := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues", token, map[string]any{"name": "trigger-q"})
	qID := p3Str(t, q, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/triggers", token, map[string]any{"type": "sqs", "queue_id": qID}); code != 201 {
		t.Fatalf("add trigger: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/sqs/queues/"+qID+"/messages", token, map[string]any{"body": "trigger-me"}); code != 201 {
		t.Fatalf("send trigger message: %d", code)
	}
	// Poll briefly for the async invocation record.
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, invs := p3Call(t, "GET", ts.URL+"/api/v1/aws/lambda/functions/"+fnID+"/invocations", token, nil)
		if list, _ := invs["invocations"].([]any); len(list) >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SQS trigger did not invoke the function within 10s")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestPhase3ELBRoute53(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// A healthy target: a local test server returning 200.
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer healthy.Close()
	_, tg := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/target-groups", token, map[string]any{"name": "web-tg", "protocol": "HTTP", "port": 80, "health_check_path": "/"})
	tgID := p3Str(t, tg, "id")
	_, tgt := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/target-groups/"+tgID+"/targets", token, map[string]any{"address": healthy.URL, "port": 80})
	_ = p3Str(t, tgt, "id")
	// A dead target too (closed port).
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/target-groups/"+tgID+"/targets", token, map[string]any{"address": "http://127.0.0.1:1/", "port": 80}); code != 201 {
		t.Fatalf("add dead target: %d", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/target-groups/"+tgID+"/health", token, nil)
	if code != 200 || out["healthy"] != float64(1) || out["total"] != float64(2) {
		t.Errorf("health check: %d %v (want 1/2 healthy)", code, out)
	}
	_, lb := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/load-balancers", token, map[string]any{"name": "web-lb", "scheme": "internet-facing"})
	lbID := p3Str(t, lb, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/load-balancers/"+lbID+"/listeners", token, map[string]any{"port": 80, "protocol": "HTTP", "target_group_id": tgID}); code != 201 {
		t.Fatalf("create listener: %d", code)
	}
	// Route twice: both must return the healthy target (round-robin
	// over healthy targets only — the dead one never wins).
	for i := 0; i < 2; i++ {
		code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/elb/load-balancers/"+lbID+"/route", token, nil)
		if code != 200 {
			t.Fatalf("route %d: %d %v", i, code, out)
		}
		target, _ := out["target"].(map[string]any)
		if target["address"] != healthy.URL {
			t.Errorf("route chose %v, want the healthy target", target["address"])
		}
	}
	// Route 53: zone, records, resolve, failure cases.
	_, zone := p3Call(t, "POST", ts.URL+"/api/v1/aws/route53/zones", token, map[string]any{"name": "app.local"})
	zoneID := p3Str(t, zone, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/route53/zones/"+zoneID+"/records", token, map[string]any{"name": "api.app.local", "type": "A", "ttl": 300, "values": []string{"10.0.0.5"}}); code != 201 {
		t.Fatalf("create A record: %d", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/route53/zones/"+zoneID+"/records", token, map[string]any{"name": "api.app.local", "type": "A", "ttl": 300, "values": []string{"10.0.0.6"}}); code != 409 {
		t.Errorf("duplicate record set: got %d, want 409", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/route53/zones/"+zoneID+"/records", token, map[string]any{"name": "mail.app.local", "type": "MX", "ttl": 300, "values": []string{"10.0.0.1"}}); code != 400 {
		t.Errorf("MX record: got %d, want 400 (unsupported type)", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/route53/zones/"+zoneID, token, nil); code != 409 {
		t.Errorf("delete non-empty zone: got %d, want 409", code)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/route53/zones/"+zoneID+"/resolve?name=api.app.local&type=A", token, nil)
	if code != 200 {
		t.Fatalf("resolve: %d %v", code, out)
	}
	answers, _ := out["answers"].([]any)
	if len(answers) != 1 || answers[0] != "10.0.0.5" {
		t.Errorf("resolve answers = %v, want [10.0.0.5]", out["answers"])
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/route53/zones/"+zoneID+"/resolve?name=missing.app.local&type=A", token, nil)
	if code != 200 || out["matched"] != false {
		t.Errorf("resolve missing: %d %v (want matched=false)", code, out)
	}
}

func TestPhase3RDSAndCacheFailurePaths(t *testing.T) {
	_, ts, token := p3TestServer(t)
	// No DATABASE_URL in this test server: RDS create must 503
	// honestly, and mysql must 400 before any provisioning.
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/rds/instances", token, map[string]any{"name": "appdb", "engine": "mysql"}); code != 400 {
		t.Errorf("mysql engine: got %d, want 400 — %v", code, out)
	}
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/rds/instances", token, map[string]any{"name": "appdb"}); code != 503 {
		t.Errorf("rds without DATABASE_URL: got %d, want 503 — %v", code, out)
	}
	// Start/stop is UNSUPPORTED even for a (hypothetical) instance.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/rds/instances/whatever/stop", token, nil); code != 409 {
		t.Errorf("rds stop: got %d, want 409 unsupported", code)
	}
	// Dead Redis: ElastiCache lite create must 503 honestly.
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/elasticache/clusters", token, map[string]any{"name": "sess", "mode": "lite"}); code != 503 {
		t.Errorf("elasticache with dead Redis: got %d, want 503 — %v", code, out)
	}
	// Full mode (dedicated container) is not built: 503 with the reason.
	if code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/elasticache/clusters", token, map[string]any{"name": "sess", "mode": "full"}); code != 503 {
		t.Errorf("elasticache full mode: got %d, want 503 — %v", code, out)
	} else if !strings.Contains(p3Str(t, out, "error")+fmt.Sprint(out), "container") && !strings.Contains(fmt.Sprint(out), "container") {
		t.Errorf("full-mode 503 must name the missing container path: %v", out)
	}
	// Unauthenticated access is rejected on a Phase 3 route.
	if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues", "", nil); code != 401 {
		t.Errorf("unauthenticated list: got %d, want 401", code)
	}
}

func TestPhase3ServicesRegistry(t *testing.T) {
	_, ts, token := p3TestServer(t)
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/services/dynamodb", token, nil)
	if code != 200 {
		t.Fatalf("get dynamodb service: %d", code)
	}
	ops, _ := out["operations"].([]any)
	routes := map[string]bool{}
	for _, o := range ops {
		if m, ok := o.(map[string]any); ok {
			if r, _ := m["http_route"].(string); r != "" {
				routes[m["operation"].(string)] = true
			}
		}
	}
	for _, want := range []string{"CreateTable", "PutItem", "GetItem", "Query", "Scan", "DeleteItem", "DeleteTable"} {
		if !routes[want] {
			t.Errorf("dynamodb operation %s has no HTTP route in the registry", want)
		}
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/services/lambda", token, nil)
	if code != 200 {
		t.Fatalf("get lambda service: %d", code)
	}
	ops, _ = out["operations"].([]any)
	found := false
	for _, o := range ops {
		if m, ok := o.(map[string]any); ok && m["operation"] == "InvokeFunction" {
			found = true
			if m["fidelity"] == "SIMULATED" {
				t.Error("lambda InvokeFunction must no longer claim the unverified SIMULATED/Docker fidelity")
			}
		}
	}
	if !found {
		t.Error("lambda InvokeFunction missing from the registry")
	}
}
