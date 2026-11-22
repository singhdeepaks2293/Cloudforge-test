package httpapi

import (
	"net/http"
	"testing"
)

var validFidelities = map[string]bool{
	"REAL": true, "API_EMULATED": true, "SIMULATED": true, "PARTIAL": true, "UNSUPPORTED": true,
}

func TestServicesRegistry(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/services", "", nil)
	if status != http.StatusOK {
		t.Fatalf("services: %d %v", status, body)
	}
	list, _ := body["services"].([]any)
	if len(list) < 40 {
		t.Fatalf("services: got %d, want >= 40 catalog entries", len(list))
	}
	byID := map[string]map[string]any{}
	for _, raw := range list {
		svc, _ := raw.(map[string]any)
		id, _ := svc["id"].(string)
		if id == "" {
			t.Fatal("service entry with no id")
		}
		if _, dup := byID[id]; dup {
			t.Fatalf("duplicate service id %q", id)
		}
		byID[id] = svc
		route, _ := svc["route"].(string)
		if route == "" || route[0] != '/' {
			t.Fatalf("service %q has no usable route: %q", id, route)
		}
		if !validFidelities[svc["fidelity"].(string)] {
			t.Fatalf("service %q has invalid fidelity %v", id, svc["fidelity"])
		}
		if svc["category"] == "" || svc["description"] == "" || svc["icon"] == "" {
			t.Fatalf("service %q is missing category/description/icon", id)
		}
		if svc["operations"] == nil {
			t.Fatalf("service %q has no operations field (must be [] when empty)", id)
		}
	}
	// EC2: a real console, PARTIAL fidelity, operations from the
	// providers registry (not a second hardcoded registry).
	ec2 := byID["ec2"]
	if ec2["fidelity"] != "PARTIAL" || ec2["route"] != "/services/ec2" {
		t.Fatalf("ec2 entry: %v", ec2)
	}
	ops, _ := ec2["operations"].([]any)
	if len(ops) < 2 {
		t.Fatalf("ec2 operations: %v, want RunInstances + DescribeInstances from the registry", ops)
	}
	// S3 bucket ops carry real HTTP routes; Phase 2 added object
	// upload/download through the backend (no direct browser->MinIO
	// credentials), so PutObject/GetObject now claim routes too.
	s3 := byID["s3"]
	s3ops, _ := s3["operations"].([]any)
	routes := map[string]string{}
	for _, raw := range s3ops {
		op, _ := raw.(map[string]any)
		routes[op["operation"].(string)], _ = op["http_route"].(string)
	}
	if routes["CreateBucket"] == "" || routes["ListBuckets"] == "" {
		t.Fatalf("s3 bucket ops must carry http_route: %v", routes)
	}
	if routes["PutObject"] == "" || routes["GetObject"] == "" {
		t.Fatalf("s3 Put/GetObject must claim their Phase 2 HTTP routes: %v", routes)
	}
	// DynamoDB is a real Phase 3 implementation: available, with HTTP
	// routes for the table/item operations (CreateTable/PutItem/GetItem/
	// Query/Scan/DeleteItem/DeleteTable) via DynamoDB Local when it
	// answers, else the Postgres-backed subset.
	ddb := byID["dynamodb"]
	if ddb["availability"] != "available" {
		t.Fatalf("dynamodb availability: %v, want available", ddb["availability"])
	}
	ddbOps, _ := ddb["operations"].([]any)
	if len(ddbOps) == 0 {
		t.Fatal("dynamodb must list its adapter operations")
	}
	for k := range routes {
		delete(routes, k)
	}
	for _, raw := range ddbOps {
		op, _ := raw.(map[string]any)
		routes[op["operation"].(string)], _ = op["http_route"].(string)
	}
	for _, want := range []string{"CreateTable", "PutItem", "GetItem", "Query", "Scan", "DeleteItem", "DeleteTable"} {
		if routes[want] == "" {
			t.Fatalf("dynamodb op %s must claim its Phase 3 HTTP route: %v", want, routes)
		}
	}
	// EKS is explicitly UNSUPPORTED with no operations.
	if byID["eks"]["fidelity"] != "UNSUPPORTED" || byID["eks"]["availability"] != "unavailable" {
		t.Fatalf("eks entry: %v", byID["eks"])
	}
	// Single-service fetch + unknown id.
	status, body = do(t, "GET", ts.URL+"/api/v1/services/ec2", "", nil)
	if status != http.StatusOK || body["id"] != "ec2" {
		t.Fatalf("GET /services/ec2: %d %v", status, body)
	}
	if status, _ := do(t, "GET", ts.URL+"/api/v1/services/nosuch", "", nil); status != http.StatusNotFound {
		t.Fatalf("GET /services/nosuch: got %d, want 404", status)
	}
}

func TestExplorerResources(t *testing.T) {
	_, ts := testServer(t)
	// Unauthenticated -> 401; the explorer never leaks resources.
	if status, _ := do(t, "GET", ts.URL+"/api/v1/explorer/resources", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("explorer without token: got %d, want 401", status)
	}
	token := register(t, ts)
	status, body := do(t, "GET", ts.URL+"/api/v1/explorer/resources", token, nil)
	if status != http.StatusOK {
		t.Fatalf("explorer: %d %v", status, body)
	}
	if body["region"] != "cf-local-1" || body["region_scoped"] != false {
		t.Fatalf("explorer region fields: %v (region scoping is display-only in Phase 1)", body)
	}
	// No labs yet: the in-memory course store seeds the 3 M1 courses,
	// which are the only real resources present.
	if total, _ := body["total"].(float64); total != 3 {
		t.Fatalf("explorer total with no labs: %v, want 3 courses", body["total"])
	}
	// Type filter + search over real data only.
	status, body = do(t, "GET", ts.URL+"/api/v1/explorer/resources?type=course", token, nil)
	if status != http.StatusOK || body["total"] != float64(3) {
		t.Fatalf("explorer type=course: %d %v", status, body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/explorer/resources?q=vpc-fundamentals", token, nil)
	if status != http.StatusOK || body["total"] != float64(1) {
		t.Fatalf("explorer q=vpc-fundamentals: %d %v", status, body)
	}
	// Another region: empty, with an honest note — never faked rows.
	status, body = do(t, "GET", ts.URL+"/api/v1/explorer/resources?region=cf-local-2", token, nil)
	if status != http.StatusOK || body["total"] != float64(0) {
		t.Fatalf("explorer region=cf-local-2: %d %v", status, body)
	}
	if note, _ := body["region_note"].(string); note == "" {
		t.Fatalf("explorer other-region response must explain that scoping is display-only: %v", body)
	}
	// Create a lab: it must appear in the explorer as a real lab row.
	status, body = do(t, "POST", ts.URL+"/api/v1/labs", token, map[string]any{"definition_id": "ec2-basics"})
	if status != http.StatusCreated && status != http.StatusAccepted && status != http.StatusOK {
		t.Fatalf("create lab: %d %v", status, body)
	}
	status, body = do(t, "GET", ts.URL+"/api/v1/explorer/resources?type=lab", token, nil)
	if status != http.StatusOK || body["total"] != float64(1) {
		t.Fatalf("explorer type=lab after create: %d %v", status, body)
	}
	res, _ := body["resources"].([]any)
	if len(res) != 1 {
		t.Fatalf("explorer lab rows: %v", body["resources"])
	}
	row, _ := res[0].(map[string]any)
	if row["fidelity"] != "SIMULATED" || row["service_id"] != "ec2" || row["can_delete"] != true {
		t.Fatalf("explorer lab row: %v", row)
	}
}
