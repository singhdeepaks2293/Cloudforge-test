// v3 exam services (SAA-C03 / SAP-C02) in lite mode: generic CRUD for
// all 24 services, every custom action, ownership isolation, the
// registry/catalog wiring, and the lite per-service cap. Everything
// runs against the in-memory Phase 3 store (p3TestServer); the
// Postgres path is the same store behind migration 014, applied by
// database.Migrate on startup (unverified here — no Postgres in the
// sandbox test run; see the store integration tests).
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// examCRUDCase is one service's minimal valid create body.
type examCRUDCase struct {
	service string
	path    string
	body    map[string]any
	// badBody is rejected with 400 when non-nil.
	badBody map[string]any
}

var examCRUDCases = []examCRUDCase{
	{"autoscaling", "autoscaling/groups", map[string]any{"name": "web-asg", "min_size": 1, "max_size": 3, "desired_capacity": 2},
		map[string]any{"name": "bad-asg", "min_size": 1, "max_size": 3, "desired_capacity": 9}},
	{"globalaccelerator", "globalaccelerator/accelerators", map[string]any{"name": "ga1", "endpoint_group_region": "cf-local-1"}, nil},
	{"directconnect", "directconnect/connections", map[string]any{"name": "dx1", "bandwidth": "1Gbps"},
		map[string]any{"name": "bad-dx", "bandwidth": "7Gbps"}},
	{"controltower", "controltower/landing-zones", map[string]any{"name": "lz1"}, nil},
	{"identitycenter", "identitycenter/permission-sets", map[string]any{"name": "ps1"}, nil},
	{"ram", "ram/resource-shares", map[string]any{"name": "share1", "resource_type": "subnet"},
		map[string]any{"name": "bad-ram", "resource_type": "planet"}},
	{"storagegateway", "storagegateway/gateways", map[string]any{"name": "gw1", "gateway_type": "FILE_S3"},
		map[string]any{"name": "bad-gw", "gateway_type": "MAGIC"}},
	{"snowball", "snowball/jobs", map[string]any{"name": "job1", "job_type": "IMPORT"},
		map[string]any{"name": "bad-job", "job_type": "TELEPORT"}},
	{"lakeformation", "lakeformation/permissions", map[string]any{"name": "perm1", "principal": "analyst", "resource_type": "table", "resource_name": "orders"},
		map[string]any{"name": "bad-perm", "principal": "analyst", "resource_type": "table", "resource_name": "orders", "permissions": []any{"FLY"}}},
	{"macie", "macie/jobs", map[string]any{"name": "macie1"}, nil},
	{"savingsplans", "savingsplans/plans", map[string]any{"name": "sp1", "plan_type": "COMPUTE", "commitment_usd_per_hour": 0.05},
		map[string]any{"name": "bad-sp", "plan_type": "COMPUTE", "commitment_usd_per_hour": 0}},
	{"auditmanager", "auditmanager/assessments", map[string]any{"name": "am1", "framework": "CIS AWS Foundations"}, nil},
	{"artifact", "artifact/agreements", map[string]any{"name": "agr1", "report": "SOC 2 Type II"}, nil},
	{"aurora", "aurora/clusters", map[string]any{"name": "aurora1", "engine": "aurora-postgresql"},
		map[string]any{"name": "bad-aurora", "engine": "aurora-magic"}},
	{"backup", "backup/plans", map[string]any{"name": "plan1"}, nil},
	{"backup", "backup/vaults", map[string]any{"name": "vault1"}, nil},
	{"mgn", "mgn/source-servers", map[string]any{"name": "srv1", "source": "10.0.0.5"}, nil},
	{"fsx", "fsx/file-systems", map[string]any{"name": "fsx1", "file_system_type": "LUSTRE"},
		map[string]any{"name": "bad-fsx", "file_system_type": "HAMSTER"}},
	{"shield", "shield/protections", map[string]any{"name": "prot1", "resource_type": "load-balancer", "resource_id": "lb-1"}, nil},
	{"ebs", "ebs/volumes", map[string]any{"name": "vol1", "size_gb": 10},
		map[string]any{"name": "bad-vol", "size_gb": 0}},
	{"efs", "efs/file-systems", map[string]any{"name": "efs1"}, nil},
	{"cloudfront", "cloudfront/distributions", map[string]any{"name": "dist1", "origin": "example.cf-local"}, nil},
	{"vpn", "vpn/customer-gateways", map[string]any{"name": "cgw1", "ip_address": "203.0.113.1"},
		map[string]any{"name": "bad-cgw", "ip_address": "not-an-ip"}},
	{"vpn", "vpn/vpn-gateways", map[string]any{"name": "vgw1"}, nil},
	{"nacl", "nacl/acls", map[string]any{"name": "acl1"}, nil},
}

func TestExamServicesCRUD(t *testing.T) {
	_, ts, token := p3TestServer(t)
	for _, tc := range examCRUDCases {
		base := ts.URL + "/api/v1/aws/" + tc.path
		// Invalid body first (nothing stored).
		if tc.badBody != nil {
			if code, out := p3Call(t, "POST", base, token, tc.badBody); code != 400 {
				t.Fatalf("%s invalid body: got %d, want 400 (%v)", tc.service, code, out)
			}
		}
		// Missing name -> 400.
		noName := map[string]any{}
		for k, v := range tc.body {
			if k != "name" {
				noName[k] = v
			}
		}
		if code, _ := p3Call(t, "POST", base, token, noName); code != 400 {
			t.Fatalf("%s missing name: got %d, want 400", tc.service, code)
		}
		// Create.
		code, out := p3Call(t, "POST", base, token, tc.body)
		if code != 201 {
			t.Fatalf("%s create: got %d %v", tc.service, code, out)
		}
		id := p3Str(t, out, "id")
		if id == "" || p3Str(t, out, "fidelity") != "API_EMULATED" {
			t.Fatalf("%s create response must carry id + API_EMULATED fidelity: %v", tc.service, out)
		}
		// Duplicate name in the same service -> 409.
		if code, _ := p3Call(t, "POST", base, token, tc.body); code != 409 {
			t.Fatalf("%s duplicate name: got %d, want 409", tc.service, code)
		}
		// List shows exactly one.
		code, out = p3Call(t, "GET", base, token, nil)
		if code != 200 || out["count"] != float64(1) {
			t.Fatalf("%s list: %d %v", tc.service, code, out)
		}
		// Get.
		if code, out := p3Call(t, "GET", base+"/"+id, token, nil); code != 200 || p3Str(t, out, "id") != id {
			t.Fatalf("%s get: %d %v", tc.service, code, out)
		}
		// The id must not resolve under another service (no cross-service leak).
		if tc.service == "autoscaling" {
			if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/aws/ebs/volumes/"+id, token, nil); code != 404 {
				t.Fatalf("autoscaling id under ebs: got %d, want 404", code)
			}
		}
		// Delete, then get -> 404.
		if code, _ := p3Call(t, "DELETE", base+"/"+id, token, nil); code != 200 {
			t.Fatalf("%s delete: got %d", tc.service, code)
		}
		if code, _ := p3Call(t, "GET", base+"/"+id, token, nil); code != 404 {
			t.Fatalf("%s get after delete: got %d, want 404", tc.service, code)
		}
	}
	// VPN connections validate their gateways.
	_, cgw := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/customer-gateways", token, map[string]any{"name": "cgw2", "ip_address": "203.0.113.2"})
	_, vgw := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/vpn-gateways", token, map[string]any{"name": "vgw2"})
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/connections", token, map[string]any{
		"name": "bad-conn", "customer_gateway_id": "no-such", "vpn_gateway_id": p3Str(t, vgw, "id"),
	}); code != 400 {
		t.Fatalf("vpn connection with missing customer gateway: got %d, want 400", code)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/connections", token, map[string]any{
		"name": "conn1", "customer_gateway_id": p3Str(t, cgw, "id"), "vpn_gateway_id": p3Str(t, vgw, "id"),
	})
	if code != 201 {
		t.Fatalf("vpn connection create: got %d %v", code, out)
	}
	if p3Str(t, out, "tunnel_status") != "DOWN" {
		t.Fatalf("vpn connection starts DOWN: %v", out)
	}
}

func TestExamServiceActions(t *testing.T) {
	_, ts, token := p3TestServer(t)
	must := func(method, url string, body any, want int) map[string]any {
		t.Helper()
		code, out := p3Call(t, method, url, token, body)
		if code != want {
			t.Fatalf("%s %s: got %d, want %d (%v)", method, url, code, want, out)
		}
		return out
	}
	// Auto Scaling: scale within bounds, refuse outside.
	_, asg := p3Call(t, "POST", ts.URL+"/api/v1/aws/autoscaling/groups", token, map[string]any{"name": "asg1", "min_size": 1, "max_size": 3, "desired_capacity": 1})
	asgID := p3Str(t, asg, "id")
	out := must("POST", ts.URL+"/api/v1/aws/autoscaling/groups/"+asgID+"/scale", map[string]any{"desired_capacity": 3}, 200)
	if out["desired_capacity"] != float64(3) || out["current_capacity"] != float64(3) {
		t.Fatalf("scale did not set desired+current to 3: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/autoscaling/groups/"+asgID+"/scale", map[string]any{"desired_capacity": 4}, 400)
	// Global Accelerator: two-region failover.
	_, ga := p3Call(t, "POST", ts.URL+"/api/v1/aws/globalaccelerator/accelerators", token, map[string]any{
		"name": "ga1", "endpoint_groups": []any{
			map[string]any{"region": "cf-local-1", "endpoints": []any{"10.0.0.1"}},
			map[string]any{"region": "cf-local-2", "endpoints": []any{"10.0.0.2"}},
		},
	})
	gaID := p3Str(t, ga, "id")
	if p3Str(t, ga, "active_region") != "cf-local-1" {
		t.Fatalf("accelerator active_region must follow the first endpoint group: %v", ga)
	}
	if ips, ok := ga["static_ips"].([]any); !ok || len(ips) != 2 || !strings.HasPrefix(ips[0].(string), "192.0.2.") {
		t.Fatalf("accelerator static IPs must be teaching addresses: %v", ga)
	}
	out = must("POST", ts.URL+"/api/v1/aws/globalaccelerator/accelerators/"+gaID+"/failover", map[string]any{"region": "cf-local-2"}, 200)
	if p3Str(t, out, "active_region") != "cf-local-2" {
		t.Fatalf("failover did not switch active_region: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/globalaccelerator/accelerators/"+gaID+"/failover", map[string]any{"region": "cf-local-3"}, 400)
	// Direct Connect: confirm once, refuse twice.
	_, dx := p3Call(t, "POST", ts.URL+"/api/v1/aws/directconnect/connections", token, map[string]any{"name": "dx1", "bandwidth": "10Gbps"})
	dxID := p3Str(t, dx, "id")
	if p3Str(t, dx, "status") != "REQUESTED" {
		t.Fatalf("connection starts REQUESTED: %v", dx)
	}
	out = must("POST", ts.URL+"/api/v1/aws/directconnect/connections/"+dxID+"/confirm", nil, 200)
	if p3Str(t, out, "status") != "AVAILABLE" {
		t.Fatalf("confirm did not make the connection AVAILABLE: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/directconnect/connections/"+dxID+"/confirm", nil, 409)
	// Control Tower guardrails.
	_, lz := p3Call(t, "POST", ts.URL+"/api/v1/aws/controltower/landing-zones", token, map[string]any{"name": "lz1"})
	lzID := p3Str(t, lz, "id")
	out = must("POST", ts.URL+"/api/v1/aws/controltower/landing-zones/"+lzID+"/guardrails", map[string]any{"guardrail": "CT.SECURITY.1", "enabled": true}, 200)
	if gs, ok := out["guardrails"].([]any); !ok || len(gs) != 1 {
		t.Fatalf("guardrail not recorded: %v", out)
	}
	// IAM Identity Center assignments.
	_, ps := p3Call(t, "POST", ts.URL+"/api/v1/aws/identitycenter/permission-sets", token, map[string]any{"name": "ps1"})
	psID := p3Str(t, ps, "id")
	out = must("POST", ts.URL+"/api/v1/aws/identitycenter/permission-sets/"+psID+"/assignments", map[string]any{"principal_type": "group", "principal": "ops", "account": "111122223333"}, 200)
	if as, ok := out["assignments"].([]any); !ok || len(as) != 1 {
		t.Fatalf("assignment not recorded: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/identitycenter/permission-sets/"+psID+"/assignments", map[string]any{"principal_type": "robot", "principal": "x", "account": "1"}, 400)
	// RAM principals add/remove.
	_, sh := p3Call(t, "POST", ts.URL+"/api/v1/aws/ram/resource-shares", token, map[string]any{"name": "sh1", "resource_type": "subnet"})
	shID := p3Str(t, sh, "id")
	must("POST", ts.URL+"/api/v1/aws/ram/resource-shares/"+shID+"/principals", map[string]any{"principal": "222233334444", "action": "add"}, 200)
	must("POST", ts.URL+"/api/v1/aws/ram/resource-shares/"+shID+"/principals", map[string]any{"principal": "222233334444", "action": "add"}, 409)
	out = must("POST", ts.URL+"/api/v1/aws/ram/resource-shares/"+shID+"/principals", map[string]any{"principal": "222233334444", "action": "remove"}, 200)
	if ps, ok := out["principals"].([]any); !ok || len(ps) != 0 {
		t.Fatalf("principal not removed: %v", out)
	}
	// Storage Gateway: activate + file share.
	_, gw := p3Call(t, "POST", ts.URL+"/api/v1/aws/storagegateway/gateways", token, map[string]any{"name": "gw1", "gateway_type": "FILE_S3"})
	gwID := p3Str(t, gw, "id")
	out = must("POST", ts.URL+"/api/v1/aws/storagegateway/gateways/"+gwID+"/activate", nil, 200)
	if p3Str(t, out, "status") != "ONLINE" {
		t.Fatalf("gateway did not come ONLINE: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/storagegateway/gateways/"+gwID+"/activate", nil, 409)
	out = must("POST", ts.URL+"/api/v1/aws/storagegateway/gateways/"+gwID+"/file-shares", map[string]any{"name": "share1", "location": "s3://bucket1"}, 200)
	if fs, ok := out["file_shares"].([]any); !ok || len(fs) != 1 {
		t.Fatalf("file share not recorded: %v", out)
	}
	// Snowball lifecycle: forward only.
	_, job := p3Call(t, "POST", ts.URL+"/api/v1/aws/snowball/jobs", token, map[string]any{"name": "job1", "job_type": "EXPORT"})
	jobID := p3Str(t, job, "id")
	must("POST", ts.URL+"/api/v1/aws/snowball/jobs/"+jobID+"/advance", map[string]any{"status": "COMPLETED"}, 400)
	out = must("POST", ts.URL+"/api/v1/aws/snowball/jobs/"+jobID+"/advance", map[string]any{"status": "PREPARING"}, 200)
	if p3Str(t, out, "status") != "PREPARING" {
		t.Fatalf("snowball did not advance: %v", out)
	}
	// Macie: a run over a sensitive name creates exactly one finding.
	_, mj := p3Call(t, "POST", ts.URL+"/api/v1/aws/macie/jobs", token, map[string]any{"name": "macie1", "scope": []any{"customer-ssn-records"}})
	mjID := p3Str(t, mj, "id")
	out = must("POST", ts.URL+"/api/v1/aws/macie/jobs/"+mjID+"/run", nil, 200)
	if p3Str(t, out, "status") != "COMPLETED" || out["findings_created"] != float64(1) {
		t.Fatalf("macie run must complete with 1 finding: %v", out)
	}
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/macie/findings", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("macie findings: %d %v", code, out)
	}
	// Compute Optimizer: no labs -> no recommendations, honestly.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/computeoptimizer/recommendations/refresh", token, nil)
	if code != 200 || out["count"] != float64(0) {
		t.Fatalf("compute optimizer refresh with no labs: %d %v", code, out)
	}
	// Savings Plans: utilization is 0% with no labs.
	_, sp := p3Call(t, "POST", ts.URL+"/api/v1/aws/savingsplans/plans", token, map[string]any{"name": "sp1", "plan_type": "COMPUTE", "commitment_usd_per_hour": 1})
	spID := p3Str(t, sp, "id")
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/savingsplans/plans/"+spID+"/utilization", token, nil)
	if code != 200 || out["utilization_percent"] != float64(0) {
		t.Fatalf("savings utilization with no labs: %d %v", code, out)
	}
	// Audit Manager: evidence counts real audit entries (possibly 0).
	_, am := p3Call(t, "POST", ts.URL+"/api/v1/aws/auditmanager/assessments", token, map[string]any{"name": "am1", "framework": "CIS AWS Foundations"})
	amID := p3Str(t, am, "id")
	out = must("POST", ts.URL+"/api/v1/aws/auditmanager/assessments/"+amID+"/collect-evidence", nil, 200)
	if ev, ok := out["evidence"].([]any); !ok || len(ev) != 1 {
		t.Fatalf("audit evidence not recorded: %v", out)
	}
	// Artifact: catalogue + agreements.
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/artifact/reports", token, nil)
	if code != 200 || out["count"] != float64(8) {
		t.Fatalf("artifact reports: %d %v", code, out)
	}
	// Aurora: failover swaps writer/reader.
	_, au := p3Call(t, "POST", ts.URL+"/api/v1/aws/aurora/clusters", token, map[string]any{"name": "au1", "engine": "aurora-postgresql", "instance_count": 2})
	auID := p3Str(t, au, "id")
	out = must("POST", ts.URL+"/api/v1/aws/aurora/clusters/"+auID+"/failover", nil, 200)
	insts, _ := out["instances"].([]any)
	if len(insts) != 2 || insts[0].(map[string]any)["role"] != "reader" || insts[1].(map[string]any)["role"] != "writer" {
		t.Fatalf("aurora failover did not swap roles: %v", out)
	}
	// Backup: a run without a platform database is a FAILED job, not a fake success.
	_, plan := p3Call(t, "POST", ts.URL+"/api/v1/aws/backup/plans", token, map[string]any{"name": "plan1"})
	planID := p3Str(t, plan, "id")
	out = must("POST", ts.URL+"/api/v1/aws/backup/plans/"+planID+"/run", nil, 201)
	if p3Str(t, out, "status") != "FAILED" || p3Str(t, out, "error") == "" {
		t.Fatalf("backup run without a database must be a FAILED job with a reason: %v", out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/backup/jobs", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("backup jobs list: %d %v", code, out)
	}
	// MGN lifecycle: wrong order is refused.
	_, srv := p3Call(t, "POST", ts.URL+"/api/v1/aws/mgn/source-servers", token, map[string]any{"name": "srv1", "source": "10.0.0.5"})
	srvID := p3Str(t, srv, "id")
	must("POST", ts.URL+"/api/v1/aws/mgn/source-servers/"+srvID+"/cutover", nil, 409)
	must("POST", ts.URL+"/api/v1/aws/mgn/source-servers/"+srvID+"/start-replication", nil, 200)
	must("POST", ts.URL+"/api/v1/aws/mgn/source-servers/"+srvID+"/test-launch", nil, 200)
	out = must("POST", ts.URL+"/api/v1/aws/mgn/source-servers/"+srvID+"/cutover", nil, 200)
	if p3Str(t, out, "replication_state") != "CUTOVER" {
		t.Fatalf("mgn did not reach CUTOVER: %v", out)
	}
	// FSx + EFS mount targets.
	_, fsx := p3Call(t, "POST", ts.URL+"/api/v1/aws/fsx/file-systems", token, map[string]any{"name": "fsx1", "file_system_type": "LUSTRE"})
	fsxID := p3Str(t, fsx, "id")
	must("POST", ts.URL+"/api/v1/aws/fsx/file-systems/"+fsxID+"/mount-targets", map[string]any{"subnet_id": "subnet-1"}, 200)
	must("POST", ts.URL+"/api/v1/aws/fsx/file-systems/"+fsxID+"/mount-targets", map[string]any{"subnet_id": "subnet-1"}, 409)
	_, efs := p3Call(t, "POST", ts.URL+"/api/v1/aws/efs/file-systems", token, map[string]any{"name": "efs1"})
	must("POST", ts.URL+"/api/v1/aws/efs/file-systems/"+p3Str(t, efs, "id")+"/mount-targets", map[string]any{"subnet_id": "subnet-1"}, 200)
	// EBS: attach to a missing lab is 404; attach to a real lab works; delete in-use is 409.
	_, vol := p3Call(t, "POST", ts.URL+"/api/v1/aws/ebs/volumes", token, map[string]any{"name": "vol1", "size_gb": 10})
	volID := p3Str(t, vol, "id")
	must("POST", ts.URL+"/api/v1/aws/ebs/volumes/"+volID+"/attach", map[string]any{"lab_id": "no-such-lab"}, 404)
	labID, labBody := createLab(t, ts.URL, token, "ec2-basics")
	if labID == "" {
		t.Fatalf("could not create a lab for the EBS attach test: %v", labBody)
	}
	out = must("POST", ts.URL+"/api/v1/aws/ebs/volumes/"+volID+"/attach", map[string]any{"lab_id": labID}, 200)
	if p3Str(t, out, "status") != "in-use" {
		t.Fatalf("volume did not become in-use: %v", out)
	}
	must("DELETE", ts.URL+"/api/v1/aws/ebs/volumes/"+volID, nil, 409)
	must("POST", ts.URL+"/api/v1/aws/ebs/volumes/"+volID+"/detach", nil, 200)
	must("DELETE", ts.URL+"/api/v1/aws/ebs/volumes/"+volID, nil, 200)
	// CloudFront invalidations.
	_, dist := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudfront/distributions", token, map[string]any{"name": "dist1", "origin": "example.cf-local"})
	distID := p3Str(t, dist, "id")
	out = must("POST", ts.URL+"/api/v1/aws/cloudfront/distributions/"+distID+"/invalidations", map[string]any{"paths": []any{"/index.html"}}, 200)
	if invs, ok := out["invalidations"].([]any); !ok || len(invs) != 1 || invs[0].(map[string]any)["status"] != "COMPLETED" {
		t.Fatalf("invalidation not recorded COMPLETED: %v", out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/cloudfront/distributions/"+distID+"/invalidations", token, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("invalidations list: %d %v", code, out)
	}
	// VPN tunnel activate/deactivate.
	_, cgw := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/customer-gateways", token, map[string]any{"name": "cgw1", "ip_address": "203.0.113.1"})
	_, vgw := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/vpn-gateways", token, map[string]any{"name": "vgw1"})
	_, conn := p3Call(t, "POST", ts.URL+"/api/v1/aws/vpn/connections", token, map[string]any{
		"name": "conn1", "customer_gateway_id": p3Str(t, cgw, "id"), "vpn_gateway_id": p3Str(t, vgw, "id"),
	})
	connID := p3Str(t, conn, "id")
	out = must("POST", ts.URL+"/api/v1/aws/vpn/connections/"+connID+"/activate", nil, 200)
	if p3Str(t, out, "tunnel_status") != "UP" {
		t.Fatalf("tunnel did not come UP: %v", out)
	}
	must("POST", ts.URL+"/api/v1/aws/vpn/connections/"+connID+"/activate", nil, 409)
	out = must("POST", ts.URL+"/api/v1/aws/vpn/connections/"+connID+"/deactivate", nil, 200)
	if p3Str(t, out, "tunnel_status") != "DOWN" {
		t.Fatalf("tunnel did not go DOWN: %v", out)
	}
	// NACL: real first-match evaluation.
	_, acl := p3Call(t, "POST", ts.URL+"/api/v1/aws/nacl/acls", token, map[string]any{"name": "acl1"})
	aclID := p3Str(t, acl, "id")
	must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/rules", map[string]any{"rule_number": 100, "direction": "ingress", "protocol": "tcp", "port_from": 443, "port_to": 443, "cidr": "0.0.0.0/0", "action": "allow"}, 201)
	must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/rules", map[string]any{"rule_number": 100, "direction": "ingress", "protocol": "tcp", "port_from": 80, "port_to": 80, "cidr": "0.0.0.0/0", "action": "allow"}, 409)
	must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/rules", map[string]any{"rule_number": 50, "direction": "ingress", "protocol": "tcp", "port_from": 22, "port_to": 22, "cidr": "0.0.0.0/0", "action": "deny"}, 201)
	out = must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/evaluate", map[string]any{"direction": "ingress", "protocol": "tcp", "port": 443, "source_ip": "1.2.3.4"}, 200)
	if out["allowed"] != true || out["matched_rule_number"] != float64(100) {
		t.Fatalf("443 must be allowed by rule 100: %v", out)
	}
	out = must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/evaluate", map[string]any{"direction": "ingress", "protocol": "tcp", "port": 22, "source_ip": "1.2.3.4"}, 200)
	if out["allowed"] != false || out["matched_rule_number"] != float64(50) {
		t.Fatalf("22 must be denied by rule 50: %v", out)
	}
	out = must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/evaluate", map[string]any{"direction": "ingress", "protocol": "tcp", "port": 8080, "source_ip": "1.2.3.4"}, 200)
	if out["allowed"] != false || out["matched_rule_number"] != float64(-1) {
		t.Fatalf("8080 must hit the default deny: %v", out)
	}
	must("DELETE", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/rules/50?direction=ingress", nil, 200)
	out = must("POST", ts.URL+"/api/v1/aws/nacl/acls/"+aclID+"/evaluate", map[string]any{"direction": "ingress", "protocol": "tcp", "port": 22, "source_ip": "1.2.3.4"}, 200)
	if out["allowed"] != false || out["matched_rule_number"] != float64(-1) {
		t.Fatalf("22 after rule delete must hit the default deny: %v", out)
	}
	// Lake Formation: grant then revoke (delete).
	_, perm := p3Call(t, "POST", ts.URL+"/api/v1/aws/lakeformation/permissions", token, map[string]any{"name": "perm1", "principal": "analyst", "resource_type": "table", "resource_name": "orders", "permissions": []any{"SELECT"}})
	must("DELETE", ts.URL+"/api/v1/aws/lakeformation/permissions/"+p3Str(t, perm, "id"), nil, 200)
}

// TestExamServicesRegistry checks the catalog wiring: every v3 exam
// service is available with its registry operations carrying real
// HTTP routes, and the catalog total matches the registry (109 rows
// per the Workstream 4 audit).
func TestExamServicesRegistry(t *testing.T) {
	_, ts := testServer(t)
	status, body := do(t, "GET", ts.URL+"/api/v1/services", "", nil)
	if status != http.StatusOK {
		t.Fatalf("services: %d %v", status, body)
	}
	list, _ := body["services"].([]any)
	if len(list) < 109 {
		t.Fatalf("services: got %d, want >= 109 catalog entries", len(list))
	}
	byID := map[string]map[string]any{}
	for _, raw := range list {
		svc, _ := raw.(map[string]any)
		byID[svc["id"].(string)] = svc
	}
	want := map[string]string{
		"autoscaling": "API_EMULATED", "globalaccelerator": "API_EMULATED",
		"directconnect": "API_EMULATED", "controltower": "API_EMULATED",
		"identitycenter": "API_EMULATED", "ram": "API_EMULATED",
		"storagegateway": "API_EMULATED", "snowball": "API_EMULATED",
		"lakeformation": "API_EMULATED", "macie": "API_EMULATED",
		"computeoptimizer": "API_EMULATED", "savingsplans": "API_EMULATED",
		"auditmanager": "API_EMULATED", "artifact": "API_EMULATED",
		"aurora": "API_EMULATED", "backup": "REAL", "mgn": "API_EMULATED",
		"fsx": "API_EMULATED", "shield": "API_EMULATED", "ebs": "API_EMULATED",
		"efs": "API_EMULATED", "cloudfront": "API_EMULATED", "vpn": "API_EMULATED",
		"nacl": "API_EMULATED",
	}
	for id, fidelity := range want {
		svc, ok := byID[id]
		if !ok {
			t.Fatalf("service %q missing from the catalog", id)
		}
		if svc["availability"] != "available" {
			t.Fatalf("service %q availability: %v, want available", id, svc["availability"])
		}
		if svc["fidelity"] != fidelity {
			t.Fatalf("service %q fidelity: %v, want %s", id, svc["fidelity"], fidelity)
		}
		ops, _ := svc["operations"].([]any)
		if len(ops) == 0 {
			t.Fatalf("service %q must list registry operations", id)
		}
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			if op["http_route"] == "" {
				t.Fatalf("service %q op %v must carry an http_route", id, op["operation"])
			}
			if op["supported"] != true {
				t.Fatalf("service %q op %v must be supported", id, op["operation"])
			}
		}
	}
}

// TestExamServicesOwnership: a learner sees none of the admin's exam
// resources and cannot fetch one by id.
func TestExamServicesOwnership(t *testing.T) {
	_, ts, adminToken := p3TestServer(t)
	learnerToken := registerAs(t, ts, "learner@example.com")
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/autoscaling/groups", adminToken, map[string]any{"name": "admin-asg", "min_size": 1, "max_size": 2, "desired_capacity": 1})
	if code != 201 {
		t.Fatalf("admin create: %d %v", code, out)
	}
	id := p3Str(t, out, "id")
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/autoscaling/groups", learnerToken, nil)
	if code != 200 || out["count"] != float64(0) {
		t.Fatalf("learner list must be empty: %d %v", code, out)
	}
	if code, _ := p3Call(t, "GET", ts.URL+"/api/v1/aws/autoscaling/groups/"+id, learnerToken, nil); code != 403 {
		t.Fatalf("learner get of admin resource: got %d, want 403", code)
	}
}

// TestComputeOptimizerWithLab: a real lab produces a real
// recommendation (right-sized Micro lab) with a LOW confidence and
// an honest basis note.
func TestComputeOptimizerWithLab(t *testing.T) {
	labStore := labs.NewStore()
	labStore.ProvisionDelay = 5 * time.Millisecond
	labStore.CheckNetwork = func() bool { return true }
	srv := NewServerWithDeps(Deps{
		Users: auth.NewStore("test-secret"), Labs: store.NewMemoryLabStore(labStore),
		Courses: store.NewMemoryCourseStore(), Audit: store.NewMemoryAuditStore(),
		Cache: &cache.Cache{}, Registry: providers.NewRegistry(),
		Network: networkclient.New("127.0.0.1:1"), Logger: nil,
		AdminEmail: "admin@example.com",
		P3:         store.NewMemoryPhase3Store(), P3WorkdirRoot: t.TempDir(),
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	adminToken := registerAs(t, ts, "admin@example.com")
	labID, labBody := createLab(t, ts.URL, adminToken, "ec2-basics")
	if labID == "" {
		t.Fatalf("lab create failed: %v", labBody)
	}
	// Wait for READY (async provisioning).
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body := do(t, "GET", ts.URL+"/api/v1/labs/"+labID, adminToken, nil)
		if body["state"] == "READY" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lab did not reach READY: %v", body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/computeoptimizer/recommendations/refresh", adminToken, nil)
	if code != 200 || out["count"] != float64(1) {
		t.Fatalf("refresh with one Micro lab: %d %v", code, out)
	}
	recs, _ := out["recommendations"].([]any)
	rec := recs[0].(map[string]any)
	if rec["recommendation_type"] != "right-sized" || rec["confidence"] != "LOW" {
		t.Fatalf("Micro lab recommendation: %v", rec)
	}
	if !strings.Contains(rec["basis"].(string), "no CloudWatch utilization metrics") {
		t.Fatalf("recommendation must state the missing utilization data: %v", rec)
	}
	// A Savings Plan now sees the lab's hourly burn: Micro $0.01/hr
	// against a $0.005 commitment is 100% utilized (capped).
	_, sp := p3Call(t, "POST", ts.URL+"/api/v1/aws/savingsplans/plans", adminToken, map[string]any{"name": "sp1", "plan_type": "COMPUTE", "commitment_usd_per_hour": 0.005})
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/savingsplans/plans/"+p3Str(t, sp, "id")+"/utilization", adminToken, nil)
	if code != 200 || out["utilization_percent"] != float64(100) {
		t.Fatalf("savings utilization with one Micro lab: %d %v", code, out)
	}
	if out["current_hourly_lab_cost_usd"] != float64(0.01) {
		t.Fatalf("hourly lab cost must be Micro $0.01: %v", out)
	}
}
