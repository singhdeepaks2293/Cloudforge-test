// v3 exam services (SAA-C03 / SAP-C02) in lite mode.
//
// Twenty-four exam-needed services that were catalogued UNSUPPORTED in
// v2 now have a real API-emulated backend path: real HTTP endpoints,
// durable Postgres state in the shared p7_exam_services table
// (collection "exam_services", migration 014), list/create/get/delete
// where the service model allows, plus one or two small actions each
// (scale, failover, confirm, …). They are wired into the providers
// registry (backend/internal/providers/providers.go) with honest
// fidelity (API_EMULATED, except MGN replication which is SIMULATED
// and Backup runs which are REAL pg_dump) and into the service
// catalog (services.go) with mirrors/differs notes.
//
// What "API-emulated" means here: the API contract is honoured and
// the records are real and persistent, but nothing provisions real
// AWS infrastructure. An Auto Scaling group records min/desired/max
// and scales the recorded number — it launches no instances. A
// Global Accelerator records endpoint groups and failover history —
// it routes no traffic. Each handler and each registry entry says
// exactly that. Nothing here is presented as real AWS.
//
// Lite mode: these services cost no container, VM or engine capacity
// (a few Postgres rows each), so they run in lite unchanged. The only
// lite guard is a per-service, per-user resource cap
// (quota.LiteExamResourceMax: 10 learner / 25 instructor / 50 admin);
// full mode is uncapped. See docs/DEPLOYMENT.md.
//
// Ownership: resources record owner_id; a non-admin sees only their
// own. No resource is shown ACTIVE/available until the operation
// actually succeeded; failures are 4xx/5xx with the reason, never a
// fake success. Actions that cannot do real work (MGN replication,
// VPN tunnel state, Snowball shipping) are teaching state machines
// and say so in the response.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/cloudforge/cloudforge/backend/internal/phase6"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// examCollection is the shared Phase 3 collection for all v3 exam
// services (table p7_exam_services, migration 014). The service
// discriminator is data["service"].
const examCollection = "exam_services"

// examDef describes one exam service's generic CRUD shape. Custom
// actions live in their own handlers below.
type examDef struct {
	id            string // registry service id, e.g. "autoscaling"
	path          string // URL path under /api/v1/aws/, e.g. "autoscaling/groups"
	kind          string // resource kind label for messages
	required      []string
	defaults      map[string]any
	initialStatus string
	validate      func(req map[string]any) error
}

// examDefs is every v3 exam service with generic CRUD. Services with
// no generic create (computeoptimizer recommendations are computed;
// artifact reports are a static catalogue; backup jobs are created
// by running a plan) are registered separately in registerExamRoutes.
var examDefs = []examDef{
	{id: "autoscaling", path: "autoscaling/groups", kind: "auto-scaling-group",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"min_size": 1, "max_size": 2, "desired_capacity": 1, "current_capacity": 1, "launch_profile": "Micro", "health_check_type": "EC2"},
		validate:      validateASG},
	{id: "globalaccelerator", path: "globalaccelerator/accelerators", kind: "accelerator",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"ip_address_type": "IPv4", "failover_history": []any{}},
		validate:      validateAccelerator},
	{id: "directconnect", path: "directconnect/connections", kind: "connection",
		initialStatus: "REQUESTED",
		defaults:      map[string]any{"location": "EqDC2 (teaching)"},
		validate:      validateDXConnection},
	{id: "controltower", path: "controltower/landing-zones", kind: "landing-zone",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"home_region": "cf-local-1", "version": "3.3", "guardrails": []any{}},
		validate:      nil},
	{id: "identitycenter", path: "identitycenter/permission-sets", kind: "permission-set",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"session_duration_seconds": 3600, "managed_policies": []any{}, "assignments": []any{}},
		validate:      nil},
	{id: "ram", path: "ram/resource-shares", kind: "resource-share",
		required:      []string{"resource_type"},
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"allow_external_principals": false, "principals": []any{}, "resource_ids": []any{}},
		validate:      validateRAMShare},
	{id: "storagegateway", path: "storagegateway/gateways", kind: "gateway",
		required:      []string{"gateway_type"},
		initialStatus: "OFFLINE",
		defaults:      map[string]any{"timezone": "GMT", "file_shares": []any{}},
		validate:      validateGateway},
	{id: "snowball", path: "snowball/jobs", kind: "job",
		required:      []string{"job_type"},
		initialStatus: "CREATED",
		defaults:      map[string]any{"device_type": "SNOWBALL_EDGE"},
		validate:      validateSnowballJob},
	{id: "lakeformation", path: "lakeformation/permissions", kind: "permission",
		required:      []string{"principal", "resource_type", "resource_name"},
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"permissions": []any{}},
		validate:      validateLakePermission},
	{id: "macie", path: "macie/jobs", kind: "classification-job",
		initialStatus: "CREATED",
		defaults:      map[string]any{"scope": []any{}, "findings_created": 0},
		validate:      nil},
	{id: "savingsplans", path: "savingsplans/plans", kind: "savings-plan",
		required:      []string{"plan_type", "commitment_usd_per_hour"},
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"term": "ONE_YEAR", "payment": "NO_UPFRONT"},
		validate:      validateSavingsPlan},
	{id: "auditmanager", path: "auditmanager/assessments", kind: "assessment",
		required:      []string{"framework"},
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"evidence": []any{}},
		validate:      nil},
	{id: "artifact", path: "artifact/agreements", kind: "agreement",
		required:      []string{"report"},
		initialStatus: "PENDING",
		defaults:      map[string]any{},
		validate:      nil},
	{id: "aurora", path: "aurora/clusters", kind: "db-cluster",
		required:      []string{"engine"},
		initialStatus: "available",
		defaults:      map[string]any{"instance_count": 2, "master_username": "cfadmin"},
		validate:      validateAuroraCluster},
	{id: "backup", path: "backup/plans", kind: "backup-plan",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"schedule": "0 2 * * *", "retention_days": 30, "resources": []any{}},
		validate:      nil},
	{id: "backup", path: "backup/vaults", kind: "backup-vault",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{},
		validate:      nil},
	{id: "mgn", path: "mgn/source-servers", kind: "source-server",
		required:      []string{"source"},
		initialStatus: "NOT_STARTED",
		defaults:      map[string]any{"os": "linux", "replication_state": "NOT_STARTED"},
		validate:      nil},
	{id: "fsx", path: "fsx/file-systems", kind: "file-system",
		required:      []string{"file_system_type"},
		initialStatus: "AVAILABLE",
		defaults:      map[string]any{"storage_capacity_gb": 1200, "throughput_mbps": 128, "mount_targets": []any{}},
		validate:      validateFSx},
	{id: "shield", path: "shield/protections", kind: "protection",
		required:      []string{"resource_type", "resource_id"},
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"mitigations": []any{}},
		validate:      nil},
	{id: "ebs", path: "ebs/volumes", kind: "volume",
		required:      []string{"size_gb"},
		initialStatus: "available",
		defaults:      map[string]any{"volume_type": "gp3", "encrypted": true, "attachments": []any{}},
		validate:      validateEBSVolume},
	{id: "efs", path: "efs/file-systems", kind: "file-system",
		initialStatus: "available",
		defaults:      map[string]any{"performance_mode": "generalPurpose", "throughput_mode": "bursting", "encrypted": true, "mount_targets": []any{}},
		validate:      validateEFS},
	{id: "cloudfront", path: "cloudfront/distributions", kind: "distribution",
		required:      []string{"origin"},
		initialStatus: "Deployed",
		defaults:      map[string]any{"enabled": true, "invalidations": []any{}},
		validate:      nil},
	{id: "vpn", path: "vpn/customer-gateways", kind: "customer-gateway",
		required:      []string{"ip_address"},
		initialStatus: "available",
		defaults:      map[string]any{"bgp_asn": 65000},
		validate:      validateCustomerGateway},
	{id: "vpn", path: "vpn/vpn-gateways", kind: "vpn-gateway",
		initialStatus: "available",
		defaults:      map[string]any{},
		validate:      nil},
	{id: "vpn", path: "vpn/connections", kind: "vpn-connection",
		required:      []string{"customer_gateway_id", "vpn_gateway_id"},
		initialStatus: "available",
		defaults:      map[string]any{"type": "ipsec.1", "tunnel_status": "DOWN"},
		validate:      nil},
	{id: "nacl", path: "nacl/acls", kind: "network-acl",
		initialStatus: "ACTIVE",
		defaults:      map[string]any{"rules": []any{}},
		validate:      nil},
}

// registerExamRoutes mounts every v3 exam-service route under /api/v1.
func (s *Server) registerExamRoutes(r chi.Router) {
	for _, d := range examDefs {
		def := d
		r.Post("/aws/"+def.path, s.examCreate(def))
		r.Get("/aws/"+def.path, s.examList(def))
		r.Get("/aws/"+def.path+"/{id}", s.examGet(def))
		r.Delete("/aws/"+def.path+"/{id}", s.examDelete(def))
	}
	// Service-specific actions.
	r.Post("/aws/autoscaling/groups/{id}/scale", s.handleASGScale)
	r.Post("/aws/globalaccelerator/accelerators/{id}/failover", s.handleGAFailover)
	r.Post("/aws/directconnect/connections/{id}/confirm", s.handleDXConfirm)
	r.Post("/aws/controltower/landing-zones/{id}/guardrails", s.handleCTGuardrail)
	r.Post("/aws/identitycenter/permission-sets/{id}/assignments", s.handleIDCAssign)
	r.Post("/aws/ram/resource-shares/{id}/principals", s.handleRAMPrincipal)
	r.Post("/aws/storagegateway/gateways/{id}/activate", s.handleSGWActivate)
	r.Post("/aws/storagegateway/gateways/{id}/file-shares", s.handleSGWFileShare)
	r.Post("/aws/snowball/jobs/{id}/advance", s.handleSnowballAdvance)
	r.Post("/aws/macie/jobs/{id}/run", s.handleMacieRun)
	r.Get("/aws/macie/findings", s.handleMacieFindings)
	r.Post("/aws/computeoptimizer/recommendations/refresh", s.handleComputeOptimizerRefresh)
	r.Get("/aws/computeoptimizer/recommendations", s.handleComputeOptimizerList)
	r.Get("/aws/computeoptimizer/recommendations/{id}", s.handleComputeOptimizerGet)
	r.Delete("/aws/computeoptimizer/recommendations/{id}", s.handleComputeOptimizerDelete)
	r.Get("/aws/savingsplans/plans/{id}/utilization", s.handleSavingsPlanUtilization)
	r.Post("/aws/auditmanager/assessments/{id}/collect-evidence", s.handleAuditManagerEvidence)
	r.Get("/aws/artifact/reports", s.handleArtifactReports)
	r.Post("/aws/aurora/clusters/{id}/failover", s.handleAuroraFailover)
	r.Post("/aws/backup/plans/{id}/run", s.handleBackupRun)
	r.Get("/aws/backup/jobs", s.handleBackupListJobs)
	r.Get("/aws/backup/jobs/{id}", s.handleBackupGetJob)
	r.Post("/aws/mgn/source-servers/{id}/start-replication", s.handleMGNStartReplication)
	r.Post("/aws/mgn/source-servers/{id}/test-launch", s.handleMGNTestLaunch)
	r.Post("/aws/mgn/source-servers/{id}/cutover", s.handleMGNCutover)
	r.Post("/aws/fsx/file-systems/{id}/mount-targets", s.handleFSxMountTarget)
	r.Post("/aws/ebs/volumes/{id}/attach", s.handleEBSAttach)
	r.Post("/aws/ebs/volumes/{id}/detach", s.handleEBSDetach)
	r.Post("/aws/efs/file-systems/{id}/mount-targets", s.handleEFSMountTarget)
	r.Post("/aws/cloudfront/distributions/{id}/invalidations", s.handleCFInvalidation)
	r.Get("/aws/cloudfront/distributions/{id}/invalidations", s.handleCFListInvalidations)
	r.Post("/aws/vpn/connections/{id}/activate", s.handleVPNActivate)
	r.Post("/aws/vpn/connections/{id}/deactivate", s.handleVPNDeactivate)
	r.Post("/aws/nacl/acls/{id}/rules", s.handleNACLAddRule)
	r.Get("/aws/nacl/acls/{id}/rules", s.handleNACLListRules)
	r.Delete("/aws/nacl/acls/{id}/rules/{ruleNumber}", s.handleNACLDeleteRule)
	r.Post("/aws/nacl/acls/{id}/evaluate", s.handleNACLEvaluate)
}

// --- generic CRUD ------------------------------------------------------------

// examInt reads an integer field from a decoded JSON body (float64,
// int, or a numeric string). def is returned when the key is absent.
func examInt(req map[string]any, key string, def int) (int, error) {
	v, ok := req[key]
	if !ok || v == nil {
		return def, nil
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func examFloat(req map[string]any, key string, def float64) (float64, error) {
	v, ok := req[key]
	if !ok || v == nil {
		return def, nil
	}
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", key)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("%s must be a number", key)
	}
}

func examStr(req map[string]any, key, def string) string {
	if v, ok := req[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func examStrList(req map[string]any, key string) []any {
	v, ok := req[key]
	if !ok {
		return []any{}
	}
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{}
}

// examListFor returns the caller's visible entities of one service
// and kind (kind disambiguates services with several resource
// kinds, e.g. backup plans vs vaults).
func (s *Server) examListFor(ctx context.Context, def examDef, u *auth.User) ([]*store.P3Entity, error) {
	all, err := s.P3.P3List(ctx, examCollection)
	if err != nil {
		return nil, err
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "service", "") != def.id {
			continue
		}
		if def.kind != "" && phase3.Str(e.Data, "kind", "") != def.kind {
			continue
		}
		out = append(out, e)
	}
	out = p3Own(out, u.ID, p3IsAdmin(u.Role))
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// examCountFor counts one user's resources in one service (lite cap).
// The cap is per service, across all its kinds (plans + vaults
// together count against the backup cap).
func (s *Server) examCountFor(ctx context.Context, service string, u *auth.User) (int, error) {
	all, err := s.P3.P3List(ctx, examCollection)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range all {
		if phase3.Str(e.Data, "service", "") != service {
			continue
		}
		if !p3IsAdmin(u.Role) && phase3.Str(e.Data, "owner_id", "") != u.ID {
			continue
		}
		n++
	}
	return n, nil
}

// examLoadOwned loads one entity and checks it belongs to this
// service and the caller may see it. 404 (not 403) when the id
// belongs to another service, so ids do not leak across services.
func (s *Server) examLoadOwned(w http.ResponseWriter, r *http.Request, def examDef, u *auth.User) (*store.P3Entity, bool) {
	e, err := s.P3.P3Get(r.Context(), examCollection, chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return nil, false
	}
	if phase3.Str(e.Data, "service", "") != def.id || (def.kind != "" && phase3.Str(e.Data, "kind", "") != def.kind) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no "+def.kind+" with that id")
		return nil, false
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return nil, false
	}
	return e, true
}

func (s *Server) examCreate(def examDef) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		st, ok := s.p3Store(w, r)
		if !ok {
			return
		}
		var req map[string]any
		if !s.decodeJSON(w, r, &req) {
			return
		}
		if req == nil {
			req = map[string]any{}
		}
		name := strings.TrimSpace(examStr(req, "name", ""))
		if name == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
			return
		}
		if len(name) > 128 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name must be at most 128 characters")
			return
		}
		for _, field := range def.required {
			if v, ok := req[field]; !ok || v == nil || (fmt.Sprintf("%v", v) == "") {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", field+" is required")
				return
			}
		}
		if def.validate != nil {
			if err := def.validate(req); err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
		}
		// Lite cap: per-service, per-user. Full mode is uncapped.
		if s.Lite {
			count, err := s.examCountFor(r.Context(), def.id, u)
			if err != nil {
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			if count >= quota.LiteExamResourceMax(u.Role) {
				s.writeError(w, r, http.StatusTooManyRequests, "quota_exceeded",
					fmt.Sprintf("lite mode allows %d %s resources per user (have %d)", quota.LiteExamResourceMax(u.Role), def.id, count))
				return
			}
		}
		// Name uniqueness is per (service, kind, owner): two kinds
		// may share a name, one user may not reuse a name in one kind.
		if existing, err := s.examListFor(r.Context(), def, u); err == nil {
			for _, e := range existing {
				if e.Name == name {
					s.writeError(w, r, http.StatusConflict, "already_exists", "a "+def.kind+" named "+name+" already exists")
					return
				}
			}
		}
		data := map[string]any{
			"service": def.id, "kind": def.kind, "owner_id": u.ID,
			"status": def.initialStatus, "fidelity": "API_EMULATED",
		}
		for k, v := range def.defaults {
			data[k] = v
		}
		for k, v := range req {
			if k == "name" {
				continue
			}
			data[k] = v
		}
		// Derived fields that must not come from the caller.
		switch def.id {
		case "globalaccelerator":
			data["static_ips"] = examTeachingIPs(name)
			if groups, ok := req["endpoint_groups"].([]any); ok && len(groups) > 0 {
				data["endpoint_groups"] = groups
				if g, ok := groups[0].(map[string]any); ok {
					data["active_region"] = examStr(g, "region", "cf-local-1")
				}
			} else {
				data["endpoint_groups"] = []any{map[string]any{
					"region":    examStr(req, "endpoint_group_region", "cf-local-1"),
					"endpoints": examStrList(req, "endpoints"),
				}}
				data["active_region"] = examStr(req, "endpoint_group_region", "cf-local-1")
			}
		case "vpn":
			// A connection must reference gateways that really exist.
			if def.kind == "vpn-connection" {
				for _, ref := range []struct{ field, kind string }{
					{"customer_gateway_id", "customer-gateway"},
					{"vpn_gateway_id", "vpn-gateway"},
				} {
					refID := examStr(req, ref.field, "")
					refEnt, err := st.P3Get(r.Context(), examCollection, refID)
					if err != nil || phase3.Str(refEnt.Data, "service", "") != "vpn" || phase3.Str(refEnt.Data, "kind", "") != ref.kind {
						s.writeError(w, r, http.StatusBadRequest, "invalid_request", "no "+ref.kind+" with id "+refID)
						return
					}
				}
			}
		case "aurora":
			n, _ := examInt(req, "instance_count", 2)
			data["instance_count"] = n
			instances := []any{}
			for i := 0; i < n; i++ {
				role := "reader"
				if i == 0 {
					role = "writer"
				}
				instances = append(instances, map[string]any{
					"id": fmt.Sprintf("%s-%d", name, i+1), "role": role, "status": "available",
				})
			}
			data["instances"] = instances
			data["writer_endpoint"] = name + ".cluster-cf-local-1.cf-local"
			data["reader_endpoint"] = name + ".cluster-ro-cf-local-1.cf-local"
		case "cloudfront":
			data["domain_name"] = name + ".cloudfront.cf-local"
			if _, ok := data["default_cache_behavior"]; !ok {
				data["default_cache_behavior"] = map[string]any{"viewer_protocol_policy": "redirect-to-https", "ttl_seconds": 86400}
			}
		}
		e, err := st.P3Create(r.Context(), examCollection, &store.P3Entity{Name: name, Data: data})
		if err != nil {
			s.p3Err(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusCreated, p3View(e))
	}
}

// examTeachingIPs derives two stable teaching addresses in the
// documentation range 192.0.2.0/24 from a name. They are not routed
// anywhere; the range is TEST-NET-1, reserved for documentation.
func examTeachingIPs(name string) []string {
	sum := sha256.Sum256([]byte(name))
	a := 1 + int(sum[0])%250
	b := 1 + int(sum[1])%250
	if b == a {
		b = a%250 + 1
	}
	return []string{fmt.Sprintf("192.0.2.%d", a), fmt.Sprintf("192.0.2.%d", b)}
}

func (s *Server) examList(def examDef) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		if _, ok := s.p3Store(w, r); !ok {
			return
		}
		list, err := s.examListFor(r.Context(), def, u)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		// The last path segment is already plural ("groups",
		// "accelerators", …) — it is the list key.
		key := def.path[strings.LastIndex(def.path, "/")+1:]
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			key: p3ListView(list), "count": len(list),
		})
	}
}

func (s *Server) examGet(def examDef) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		if _, ok := s.p3Store(w, r); !ok {
			return
		}
		e, ok := s.examLoadOwned(w, r, def, u)
		if !ok {
			return
		}
		s.writeJSON(w, r, http.StatusOK, p3View(e))
	}
}

func (s *Server) examDelete(def examDef) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		st, ok := s.p3Store(w, r)
		if !ok {
			return
		}
		e, ok := s.examLoadOwned(w, r, def, u)
		if !ok {
			return
		}
		// A volume in-use cannot be deleted while attached.
		if def.id == "ebs" && phase3.Str(e.Data, "status", "") == "in-use" {
			s.writeError(w, r, http.StatusConflict, "invalid_state", "volume is in-use; detach it first")
			return
		}
		if err := st.P3Delete(r.Context(), examCollection, e.ID); err != nil {
			s.p3Err(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": e.ID, "kind": def.kind})
	}
}

// examSave persists a mutated entity.
func (s *Server) examSave(ctx context.Context, e *store.P3Entity) (*store.P3Entity, error) {
	return s.P3.P3Update(ctx, examCollection, e)
}

// examAction loads the caller's entity for a custom action.
func (s *Server) examAction(w http.ResponseWriter, r *http.Request, def examDef) (*auth.User, *store.P3Entity, bool) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return nil, nil, false
	}
	if _, ok := s.p3Store(w, r); !ok {
		return nil, nil, false
	}
	e, ok := s.examLoadOwned(w, r, def, u)
	if !ok {
		return nil, nil, false
	}
	return u, e, true
}

// --- shared action helpers -------------------------------------------------

func examDataList(e *store.P3Entity, key string) []any {
	if list, ok := e.Data[key].([]any); ok {
		return list
	}
	return []any{}
}

func examDataStr(e *store.P3Entity, key, def string) string {
	return phase3.Str(e.Data, key, def)
}

// --- per-service validation -------------------------------------------------

func validateASG(req map[string]any) error {
	min, err := examInt(req, "min_size", 1)
	if err != nil {
		return err
	}
	max, err := examInt(req, "max_size", 2)
	if err != nil {
		return err
	}
	desired, err := examInt(req, "desired_capacity", 1)
	if err != nil {
		return err
	}
	if min < 0 {
		return fmt.Errorf("min_size must be >= 0")
	}
	if max < min {
		return fmt.Errorf("max_size must be >= min_size")
	}
	if max > 10 {
		return fmt.Errorf("max_size is capped at 10 instances (teaching cap)")
	}
	if desired < min || desired > max {
		return fmt.Errorf("desired_capacity must be between min_size and max_size")
	}
	return nil
}

func validateAccelerator(req map[string]any) error {
	if _, hasGroups := req["endpoint_groups"]; !hasGroups && examStr(req, "endpoint_group_region", "") == "" {
		return fmt.Errorf("endpoint_group_region is required")
	}
	switch examStr(req, "ip_address_type", "IPv4") {
	case "IPv4", "DUAL_STACK":
	default:
		return fmt.Errorf("ip_address_type must be IPv4 or DUAL_STACK")
	}
	return nil
}

var dxBandwidths = map[string]bool{
	"50Mbps": true, "100Mbps": true, "200Mbps": true, "300Mbps": true,
	"400Mbps": true, "500Mbps": true, "1Gbps": true, "2Gbps": true,
	"5Gbps": true, "10Gbps": true, "100Gbps": true,
}

func validateDXConnection(req map[string]any) error {
	bw := examStr(req, "bandwidth", "")
	if bw == "" {
		return fmt.Errorf("bandwidth is required")
	}
	if !dxBandwidths[bw] {
		return fmt.Errorf("bandwidth must be one of 50Mbps, 100Mbps, 200Mbps, 300Mbps, 400Mbps, 500Mbps, 1Gbps, 2Gbps, 5Gbps, 10Gbps, 100Gbps")
	}
	return nil
}

func validateRAMShare(req map[string]any) error {
	switch examStr(req, "resource_type", "") {
	case "subnet", "transit-gateway", "vpc", "prefix-list", "route-table":
		return nil
	default:
		return fmt.Errorf("resource_type must be subnet, transit-gateway, vpc, prefix-list or route-table")
	}
}

func validateGateway(req map[string]any) error {
	switch examStr(req, "gateway_type", "") {
	case "FILE_S3", "CACHED", "VTL", "STORED":
		return nil
	default:
		return fmt.Errorf("gateway_type must be FILE_S3, CACHED, VTL or STORED")
	}
}

func validateSnowballJob(req map[string]any) error {
	switch examStr(req, "job_type", "") {
	case "IMPORT", "EXPORT":
		return nil
	default:
		return fmt.Errorf("job_type must be IMPORT or EXPORT")
	}
}

var lakePermissions = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"ALTER": true, "DROP": true, "GRANT": true, "DESCRIBE": true,
}

func validateLakePermission(req map[string]any) error {
	switch examStr(req, "resource_type", "") {
	case "database", "table", "data-location":
	default:
		return fmt.Errorf("resource_type must be database, table or data-location")
	}
	for _, p := range examStrList(req, "permissions") {
		s, _ := p.(string)
		if !lakePermissions[s] {
			return fmt.Errorf("unknown permission %q (SELECT, INSERT, UPDATE, DELETE, ALTER, DROP, GRANT, DESCRIBE)", s)
		}
	}
	return nil
}

func validateSavingsPlan(req map[string]any) error {
	switch examStr(req, "plan_type", "") {
	case "COMPUTE", "EC2":
	default:
		return fmt.Errorf("plan_type must be COMPUTE or EC2")
	}
	c, err := examFloat(req, "commitment_usd_per_hour", 0)
	if err != nil {
		return err
	}
	if c <= 0 || c > 100 {
		return fmt.Errorf("commitment_usd_per_hour must be between 0 and 100")
	}
	switch examStr(req, "term", "ONE_YEAR") {
	case "ONE_YEAR", "THREE_YEARS":
	default:
		return fmt.Errorf("term must be ONE_YEAR or THREE_YEARS")
	}
	switch examStr(req, "payment", "NO_UPFRONT") {
	case "NO_UPFRONT", "PARTIAL_UPFRONT", "ALL_UPFRONT":
	default:
		return fmt.Errorf("payment must be NO_UPFRONT, PARTIAL_UPFRONT or ALL_UPFRONT")
	}
	return nil
}

func validateAuroraCluster(req map[string]any) error {
	switch examStr(req, "engine", "") {
	case "aurora-postgresql", "aurora-mysql":
	default:
		return fmt.Errorf("engine must be aurora-postgresql or aurora-mysql")
	}
	n, err := examInt(req, "instance_count", 2)
	if err != nil {
		return err
	}
	if n < 1 || n > 3 {
		return fmt.Errorf("instance_count must be between 1 and 3")
	}
	return nil
}

func validateFSx(req map[string]any) error {
	switch examStr(req, "file_system_type", "") {
	case "LUSTRE", "WINDOWS", "ONTAP", "OPENZFS":
	default:
		return fmt.Errorf("file_system_type must be LUSTRE, WINDOWS, ONTAP or OPENZFS")
	}
	capacity, err := examInt(req, "storage_capacity_gb", 1200)
	if err != nil {
		return err
	}
	if capacity <= 0 {
		return fmt.Errorf("storage_capacity_gb must be > 0")
	}
	return nil
}

func validateEBSVolume(req map[string]any) error {
	size, err := examInt(req, "size_gb", 0)
	if err != nil {
		return err
	}
	if size < 1 || size > 16384 {
		return fmt.Errorf("size_gb must be between 1 and 16384")
	}
	switch examStr(req, "volume_type", "gp3") {
	case "gp2", "gp3", "io1", "io2", "st1", "sc1":
	default:
		return fmt.Errorf("volume_type must be gp2, gp3, io1, io2, st1 or sc1")
	}
	return nil
}

func validateEFS(req map[string]any) error {
	switch examStr(req, "performance_mode", "generalPurpose") {
	case "generalPurpose", "maxIO":
	default:
		return fmt.Errorf("performance_mode must be generalPurpose or maxIO")
	}
	switch examStr(req, "throughput_mode", "bursting") {
	case "bursting", "provisioned", "elastic":
	default:
		return fmt.Errorf("throughput_mode must be bursting, provisioned or elastic")
	}
	return nil
}

func validateCustomerGateway(req map[string]any) error {
	ip := examStr(req, "ip_address", "")
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("ip_address must be a valid IP address")
	}
	return nil
}

// --- Auto Scaling -----------------------------------------------------------

func (s *Server) handleASGScale(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "autoscaling", kind: "auto-scaling-group"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	desired, err := examInt(req, "desired_capacity", -1)
	if err != nil || desired < 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "desired_capacity is required and must be >= 0")
		return
	}
	minF, _ := e.Data["min_size"].(float64)
	maxF, _ := e.Data["max_size"].(float64)
	if float64(desired) < minF || float64(desired) > maxF {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("desired_capacity must be between min_size (%d) and max_size (%d)", int(minF), int(maxF)))
		return
	}
	e.Data["desired_capacity"] = desired
	// current_capacity is the last scaled-to value: no instances are
	// launched or terminated by this call (see the registry entry).
	e.Data["current_capacity"] = desired
	e.Data["scaled_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// --- Global Accelerator -----------------------------------------------------

func (s *Server) handleGAFailover(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "globalaccelerator", kind: "accelerator"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	region := strings.TrimSpace(examStr(req, "region", ""))
	if region == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "region is required")
		return
	}
	known := false
	for _, g := range examDataList(e, "endpoint_groups") {
		if m, ok := g.(map[string]any); ok && m["region"] == region {
			known = true
		}
	}
	if !known {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "region "+region+" is not an endpoint group of this accelerator")
		return
	}
	active := examDataStr(e, "active_region", "")
	if active == region {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "region "+region+" is already active")
		return
	}
	history := examDataList(e, "failover_history")
	history = append(history, map[string]any{"from": active, "to": region, "at": p3Now()})
	e.Data["failover_history"] = history
	e.Data["active_region"] = region
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Failover is recorded only: no traffic moves, because no anycast addresses are routed anywhere."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Direct Connect ---------------------------------------------------------

func (s *Server) handleDXConfirm(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "directconnect", kind: "connection"})
	if !ok {
		return
	}
	if examDataStr(e, "status", "") != "REQUESTED" {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "only a REQUESTED connection can be confirmed (current: "+examDataStr(e, "status", "")+")")
		return
	}
	e.Data["status"] = "AVAILABLE"
	e.Data["confirmed_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Confirmation is a teaching state change: no physical cross-connect, LOA-CFA or BGP session exists."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Control Tower ----------------------------------------------------------

func (s *Server) handleCTGuardrail(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "controltower", kind: "landing-zone"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(examStr(req, "guardrail", ""))
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "guardrail is required")
		return
	}
	enabled, ok := req["enabled"].(bool)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "enabled (boolean) is required")
		return
	}
	guardrails := examDataList(e, "guardrails")
	found := false
	for i, g := range guardrails {
		if m, ok := g.(map[string]any); ok && m["guardrail"] == name {
			m["enabled"] = enabled
			m["updated_at"] = p3Now()
			guardrails[i] = m
			found = true
		}
	}
	if !found {
		guardrails = append(guardrails, map[string]any{"guardrail": name, "enabled": enabled, "updated_at": p3Now()})
	}
	e.Data["guardrails"] = guardrails
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Guardrails are recorded settings only: enabling one installs no SCP or Config rule anywhere."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- IAM Identity Center ----------------------------------------------------

func (s *Server) handleIDCAssign(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "identitycenter", kind: "permission-set"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	principalType := examStr(req, "principal_type", "")
	principal := strings.TrimSpace(examStr(req, "principal", ""))
	account := strings.TrimSpace(examStr(req, "account", ""))
	switch principalType {
	case "user", "group":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "principal_type must be user or group")
		return
	}
	if principal == "" || account == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "principal and account are required")
		return
	}
	assignments := examDataList(e, "assignments")
	assignments = append(assignments, map[string]any{
		"principal_type": principalType, "principal": principal, "account": account, "assigned_at": p3Now(),
	})
	e.Data["assignments"] = assignments
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Assignments are recorded only: they create no login or role in any account."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- RAM --------------------------------------------------------------------

func (s *Server) handleRAMPrincipal(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "ram", kind: "resource-share"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	principal := strings.TrimSpace(examStr(req, "principal", ""))
	action := examStr(req, "action", "add")
	if principal == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "principal is required")
		return
	}
	principals := examDataList(e, "principals")
	switch action {
	case "add":
		for _, p := range principals {
			if p == principal {
				s.writeError(w, r, http.StatusConflict, "already_exists", "principal "+principal+" is already associated")
				return
			}
		}
		principals = append(principals, principal)
	case "remove":
		kept := []any{}
		removed := false
		for _, p := range principals {
			if p == principal {
				removed = true
				continue
			}
			kept = append(kept, p)
		}
		if !removed {
			s.writeError(w, r, http.StatusNotFound, "not_found", "principal "+principal+" is not associated")
			return
		}
		principals = kept
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "action must be add or remove")
		return
	}
	e.Data["principals"] = principals
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Association is recorded only: it grants no access, because no second account exists."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Storage Gateway ----------------------------------------------------------

func (s *Server) handleSGWActivate(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "storagegateway", kind: "gateway"})
	if !ok {
		return
	}
	if examDataStr(e, "status", "") != "OFFLINE" {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "only an OFFLINE gateway can be activated (current: "+examDataStr(e, "status", "")+")")
		return
	}
	e.Data["status"] = "ONLINE"
	e.Data["activated_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Activation is a teaching state change: no appliance connects and no activation key is exchanged with AWS."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleSGWFileShare(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "storagegateway", kind: "gateway"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(examStr(req, "name", ""))
	location := strings.TrimSpace(examStr(req, "location", ""))
	if name == "" || location == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name and location are required")
		return
	}
	shares := examDataList(e, "file_shares")
	for _, sh := range shares {
		if m, ok := sh.(map[string]any); ok && m["name"] == name {
			s.writeError(w, r, http.StatusConflict, "already_exists", "a file share named "+name+" already exists")
			return
		}
	}
	shares = append(shares, map[string]any{"name": name, "location": location, "status": "AVAILABLE", "created_at": p3Now()})
	e.Data["file_shares"] = shares
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "File shares are records only: the location is not verified against MinIO in lite mode and no data is served."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Snowball -----------------------------------------------------------------

var snowballLifecycle = []string{"CREATED", "PREPARING", "SHIPPED", "DELIVERED", "COMPLETED"}

func (s *Server) handleSnowballAdvance(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "snowball", kind: "job"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	next := examStr(req, "status", "")
	current := examDataStr(e, "status", "")
	allowed := ""
	if next == "CANCELLED" && (current == "CREATED" || current == "PREPARING") {
		allowed = next
	} else {
		for i, st := range snowballLifecycle {
			if st == current && i+1 < len(snowballLifecycle) && snowballLifecycle[i+1] == next {
				allowed = next
			}
		}
	}
	if allowed == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("cannot move a %s job to %q (lifecycle: CREATED → PREPARING → SHIPPED → DELIVERED → COMPLETED; CANCELLED only from CREATED/PREPARING)", current, next))
		return
	}
	e.Data["status"] = next
	e.Data["updated_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "The lifecycle is a teaching state machine: advancing a job moves no physical device."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Macie --------------------------------------------------------------------

// macieSensitivePatterns are substrings in a resource NAME that
// suggest sensitive data. Only names are scanned — never object or
// table contents — because this deployment has no right to read
// user data for classification, and in lite mode MinIO (the object
// store) does not even run.
var macieSensitivePatterns = []string{
	"ssn", "social-security", "credit-card", "creditcard", "password",
	"secret", "bank", "phi", "pii", "passport", "api-key", "private-key",
}

func macieScanName(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, p := range macieSensitivePatterns {
		if strings.Contains(lower, p) {
			return p, true
		}
	}
	return "", false
}

func (s *Server) handleMacieRun(w http.ResponseWriter, r *http.Request) {
	u, e, ok := s.examAction(w, r, examDef{id: "macie", kind: "classification-job"})
	if !ok {
		return
	}
	scanned := map[string]int{"s3_buckets": 0, "dynamodb_tables": 0, "named_scope": 0}
	created := 0
	notes := []string{}
	// S3 bucket names, when MinIO answers (it does not in lite mode).
	if s.AWS != nil && s.AWS.S3 != nil {
		if buckets, err := s.AWS.S3.ListBuckets(r.Context()); err == nil {
			for _, b := range buckets {
				scanned["s3_buckets"]++
				if pattern, hit := macieScanName(b.Name); hit {
					if _, err := s.p6CreateFinding(r.Context(), u.ID, "macie",
						"Sensitive-data pattern in S3 bucket "+b.Name, "MEDIUM",
						"s3-bucket", b.Name,
						"Bucket name contains the sensitive-data pattern "+pattern+". Names only were scanned; object contents were not read."); err == nil {
						created++
					}
				}
			}
		} else {
			notes = append(notes, "S3 buckets could not be read (MinIO unreachable — expected in lite mode); 0 buckets scanned")
		}
	} else {
		notes = append(notes, "S3 adapter not configured; 0 buckets scanned")
	}
	// DynamoDB table names (durable subset — always readable).
	if tables, err := s.P3.P3List(r.Context(), "dynamodb_tables"); err == nil {
		for _, t := range tables {
			if phase3.Str(t.Data, "owner_id", "") != u.ID && !p3IsAdmin(u.Role) {
				continue
			}
			scanned["dynamodb_tables"]++
			if pattern, hit := macieScanName(t.Name); hit {
				if _, err := s.p6CreateFinding(r.Context(), u.ID, "macie",
					"Sensitive-data pattern in DynamoDB table "+t.Name, "MEDIUM",
					"dynamodb-table", t.Name,
					"Table name contains the sensitive-data pattern "+pattern+". Names only were scanned."); err == nil {
					created++
				}
			}
		}
	}
	// Names the job itself lists in its scope.
	for _, item := range examDataList(e, "scope") {
		name, _ := item.(string)
		if name == "" {
			continue
		}
		scanned["named_scope"]++
		if pattern, hit := macieScanName(name); hit {
			if _, err := s.p6CreateFinding(r.Context(), u.ID, "macie",
				"Sensitive-data pattern in "+name, "MEDIUM",
				"named-scope", name,
				"Scoped name contains the sensitive-data pattern "+pattern+"."); err == nil {
				created++
			}
		}
	}
	e.Data["status"] = "COMPLETED"
	e.Data["findings_created"] = created
	e.Data["last_run"] = map[string]any{
		"completed_at": p3Now(), "scanned": scanned, "findings_created": created,
		"note": strings.Join(notes, "; "),
	}
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleMacieFindings(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	all, err := s.P3.P3List(r.Context(), "findings")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	mine := []*store.P3Entity{}
	for _, f := range all {
		if phase3.Str(f.Data, "source", "") != "macie" {
			continue
		}
		mine = append(mine, f)
	}
	mine = p3Own(mine, u.ID, p3IsAdmin(u.Role))
	s.writeJSON(w, r, http.StatusOK, map[string]any{"findings": p3ListView(mine), "count": len(mine)})
}

// --- Compute Optimizer --------------------------------------------------------

// handleComputeOptimizerRefresh computes recommendations from real
// lab state and stores them. There is no utilization pipeline in
// this deployment, so every recommendation says so and carries LOW
// confidence — except a STOPPED lab, where the advice (terminate the
// idle lab) needs no metrics. Nothing here is measured rightsizing.
func (s *Server) handleComputeOptimizerRefresh(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	if s.Labs == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "labs_unavailable", "lab store is not configured on this server")
		return
	}
	labs, err := s.Labs.ListByOwner(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Refresh replaces the user's previous recommendations.
	if old, err := s.examListFor(r.Context(), examDef{id: "computeoptimizer", kind: "recommendation"}, u); err == nil {
		for _, e := range old {
			_ = st.P3Delete(r.Context(), examCollection, e.ID)
		}
	}
	pricing := phase6.DefaultPricing
	recs := []*store.P3Entity{}
	for _, lab := range labs {
		if lab.State == "DESTROYED" || lab.State == "FAILED" {
			continue
		}
		profile := labDefinitionProfile(lab.DefinitionID)
		rate := pricing.LabHourMicro
		switch profile {
		case "Small":
			rate = pricing.LabHourSmall
		case "Medium":
			rate = pricing.LabHourMedium
		}
		recType, title, confidence, savings := "", "", "LOW", 0.0
		switch {
		case lab.State == "STOPPED":
			recType = "terminate-idle-lab"
			title = "Terminate the stopped lab " + lab.Name + " to stop accruing cost"
			confidence = "MEDIUM"
			savings = rate * 730
		case profile != "Micro":
			recType = "downsize-profile"
			title = "Consider the Micro profile for lab " + lab.Name + " (currently " + profile + ")"
			savings = (rate - pricing.LabHourMicro) * 730
		default:
			recType = "right-sized"
			title = "Lab " + lab.Name + " is already on the smallest (Micro) profile; no action"
		}
		e, err := st.P3Create(r.Context(), examCollection, &store.P3Entity{
			Name: "rec-" + lab.ID + "-" + recType,
			Data: map[string]any{
				"service": "computeoptimizer", "kind": "recommendation", "owner_id": u.ID,
				"status": "ACTIVE", "fidelity": "API_EMULATED",
				"recommendation_type": recType, "title": title,
				"lab_id": lab.ID, "lab_definition_id": lab.DefinitionID,
				"current_profile": profile, "recommended_profile": "Micro",
				"estimated_monthly_savings_usd": savings,
				"confidence":                    confidence,
				"basis":                         "Lab state only (profile and state) — no CloudWatch utilization metrics exist in this deployment, so this is not measured rightsizing.",
			},
		})
		if err != nil {
			continue
		}
		recs = append(recs, e)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"recommendations": p3ListView(recs), "count": len(recs),
		"note": "Recommendations are computed from real lab state (profile, state) at the teaching rates; no utilization data was used.",
	})
}

var defComputeOptimizer = examDef{id: "computeoptimizer", kind: "recommendation"}

func (s *Server) handleComputeOptimizerList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.examListFor(r.Context(), defComputeOptimizer, u)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"recommendations": p3ListView(list), "count": len(list)})
}

func (s *Server) handleComputeOptimizerGet(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defComputeOptimizer)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleComputeOptimizerDelete(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defComputeOptimizer)
	if !ok {
		return
	}
	if err := s.P3.P3Delete(r.Context(), examCollection, e.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": e.ID, "kind": "recommendation"})
}

// --- Savings Plans ------------------------------------------------------------

// handleSavingsPlanUtilization computes utilization from real lab
// usage: the owner's current hourly lab burn at the teaching rates
// against the plan's hourly commitment. An educational estimate,
// never an AWS bill.
func (s *Server) handleSavingsPlanUtilization(w http.ResponseWriter, r *http.Request) {
	u, e, ok := s.examAction(w, r, examDef{id: "savingsplans", kind: "savings-plan"})
	if !ok {
		return
	}
	commitment, err := examFloat(e.Data, "commitment_usd_per_hour", 0)
	if err != nil || commitment <= 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "plan has no valid hourly commitment")
		return
	}
	hourlyBurn := 0.0
	if s.Labs != nil {
		if labs, err := s.Labs.ListByOwner(r.Context(), u.ID); err == nil {
			pricing := phase6.DefaultPricing
			for _, lab := range labs {
				if lab.State == "DESTROYED" || lab.State == "FAILED" {
					continue
				}
				switch labDefinitionProfile(lab.DefinitionID) {
				case "Small":
					hourlyBurn += pricing.LabHourSmall
				case "Medium":
					hourlyBurn += pricing.LabHourMedium
				default:
					hourlyBurn += pricing.LabHourMicro
				}
			}
		}
	}
	covered := hourlyBurn
	if covered > commitment {
		covered = commitment
	}
	pct := 0.0
	if commitment > 0 {
		pct = covered / commitment * 100
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"plan_id": e.ID, "plan_name": e.Name,
		"hourly_commitment_usd":       commitment,
		"current_hourly_lab_cost_usd": hourlyBurn,
		"covered_hourly_usd":          covered,
		"utilization_percent":         pct,
		"monthly_commitment_usd":      commitment * 730,
		"monthly_covered_usd":         covered * 730,
		"basis":                       "Real lab hours at the teaching rates (Micro $0.01/hr, Small $0.02/hr, Medium $0.04/hr); educational estimate, not an AWS bill.",
	})
}

// --- Audit Manager ------------------------------------------------------------

// handleAuditManagerEvidence counts real audit-log entries since the
// assessment was created and stores the count as evidence. The
// entries themselves stay in the audit log; nothing is fabricated.
func (s *Server) handleAuditManagerEvidence(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "auditmanager", kind: "assessment"})
	if !ok {
		return
	}
	count := 0
	note := ""
	if s.Audit == nil {
		note = "audit store not configured; 0 entries counted"
	} else {
		_, total, err := s.Audit.ListFiltered(r.Context(), store.AuditFilter{From: e.CreatedAt, Limit: 1000})
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		count = total
	}
	evidence := examDataList(e, "evidence")
	evidence = append(evidence, map[string]any{
		"collected_at": p3Now(), "entries_count": count, "note": note,
	})
	e.Data["evidence"] = evidence
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// --- Artifact -------------------------------------------------------------------

// artifactReports is the catalogue of real AWS Artifact report names.
// CloudForge does not redistribute AWS compliance documents; each
// entry points at AWS Artifact as the source.
var artifactReports = []map[string]any{
	{"name": "SOC 2 Type II", "category": "Security", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "SOC 3", "category": "Security", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "ISO 27001", "category": "Security", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "PCI DSS Attestation of Compliance", "category": "Compliance", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "CSA STAR", "category": "Security", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "FedRAMP", "category": "Compliance", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "HIPAA", "category": "Compliance", "source": "AWS Artifact — download from the AWS Management Console"},
	{"name": "GDPR", "category": "Privacy", "source": "AWS Artifact — download from the AWS Management Console"},
}

func (s *Server) handleArtifactReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"reports": artifactReports, "count": len(artifactReports),
		"note": "Catalogue only: CloudForge does not serve AWS compliance documents. Download the report itself from AWS Artifact.",
	})
}

// --- Aurora ---------------------------------------------------------------------

func (s *Server) handleAuroraFailover(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "aurora", kind: "db-cluster"})
	if !ok {
		return
	}
	instances := examDataList(e, "instances")
	if len(instances) < 2 {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "failover needs at least 2 instances (this cluster has "+strconv.Itoa(len(instances))+")")
		return
	}
	// Swap the writer and the first reader.
	writerIdx, readerIdx := -1, -1
	for i, inst := range instances {
		if m, ok := inst.(map[string]any); ok {
			if m["role"] == "writer" && writerIdx < 0 {
				writerIdx = i
			}
			if m["role"] == "reader" && readerIdx < 0 {
				readerIdx = i
			}
		}
	}
	if writerIdx < 0 || readerIdx < 0 {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "cluster has no writer/reader pair to swap")
		return
	}
	wm := instances[writerIdx].(map[string]any)
	rm := instances[readerIdx].(map[string]any)
	wm["role"], rm["role"] = "reader", "writer"
	instances[writerIdx], instances[readerIdx] = wm, rm
	e.Data["instances"] = instances
	e.Data["failover_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Failover swaps the recorded writer/reader roles only: no connections move and no data is replicated (there is no Aurora engine here)."
	s.writeJSON(w, r, http.StatusOK, out)
}

// --- Backup ---------------------------------------------------------------------

// handleBackupRun runs a backup plan for real: a pg_dump -Fc of the
// platform Postgres into the API workdir, with SHA-256 recorded.
// No database configured or no pg_dump binary is a FAILED job with
// the reason — never a fake success.
func (s *Server) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	u, plan, ok := s.examAction(w, r, examDef{id: "backup", kind: "backup-plan"})
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	jobName := "backup-" + plan.Name + "-" + ids.New()[:8]
	job, err := st.P3Create(r.Context(), examCollection, &store.P3Entity{
		Name: jobName,
		Data: map[string]any{
			"service": "backup", "kind": "backup-job", "owner_id": u.ID,
			"status": "RUNNING", "fidelity": "REAL",
			"plan_id": plan.ID, "plan_name": plan.Name,
			"vault_id": "", "started_at": p3Now(),
		},
	})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	fail := func(reason string) {
		job.Data["status"] = "FAILED"
		job.Data["error"] = reason
		job.Data["completed_at"] = p3Now()
		_, _ = s.examSave(r.Context(), job)
	}
	if s.P3DatabaseURL == "" {
		fail("no platform Postgres configured (P3DatabaseURL is empty)")
		s.writeJSON(w, r, http.StatusCreated, p3View(job))
		return
	}
	cfg, err := pgxpool.ParseConfig(s.P3DatabaseURL)
	if err != nil {
		fail("cannot parse platform database URL: " + err.Error())
		s.writeJSON(w, r, http.StatusCreated, p3View(job))
		return
	}
	workdir := s.P3WorkdirRoot
	if workdir == "" {
		workdir = os.TempDir()
	}
	dir := filepath.Join(workdir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fail("cannot create backup directory: " + err.Error())
		s.writeJSON(w, r, http.StatusCreated, p3View(job))
		return
	}
	filePath := filepath.Join(dir, job.ID+".dump")
	size, err := phase3.RDSSnapshot(r.Context(), s.P3DatabaseURL, cfg.ConnConfig.Database, filePath)
	if err != nil {
		fail(err.Error())
		s.writeJSON(w, r, http.StatusCreated, p3View(job))
		return
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		fail("backup file could not be read back: " + err.Error())
		s.writeJSON(w, r, http.StatusCreated, p3View(job))
		return
	}
	sum := sha256.Sum256(raw)
	job.Data["status"] = "COMPLETED"
	job.Data["size_bytes"] = size
	job.Data["sha256"] = hex.EncodeToString(sum[:])
	job.Data["file"] = filePath
	job.Data["completed_at"] = p3Now()
	if _, err := s.examSave(r.Context(), job); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(job))
}

var defBackupJob = examDef{id: "backup", kind: "backup-job"}

func (s *Server) handleBackupListJobs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.examListFor(r.Context(), defBackupJob, u)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"jobs": p3ListView(list), "count": len(list)})
}

func (s *Server) handleBackupGetJob(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defBackupJob)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// --- MGN ------------------------------------------------------------------------

// MGN lifecycle actions are a teaching state machine (SIMULATED):
// NOT_STARTED → REPLICATING → TEST_LAUNCHED → CUTOVER. No agent is
// installed and no blocks are copied; the state machine exists so
// the cutover lesson can be practised end to end.
func (s *Server) handleMGNTransition(w http.ResponseWriter, r *http.Request, from, to, verb string) {
	_, e, ok := s.examAction(w, r, examDef{id: "mgn", kind: "source-server"})
	if !ok {
		return
	}
	current := examDataStr(e, "replication_state", "")
	if current != from {
		s.writeError(w, r, http.StatusConflict, "invalid_state",
			fmt.Sprintf("cannot %s a server in state %s (needs %s)", verb, current, from))
		return
	}
	e.Data["replication_state"] = to
	e.Data["status"] = to
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Teaching state machine (SIMULATED): no data moves; the lifecycle exists so the cutover lesson can be practised."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleMGNStartReplication(w http.ResponseWriter, r *http.Request) {
	s.handleMGNTransition(w, r, "NOT_STARTED", "REPLICATING", "start replication on")
}

func (s *Server) handleMGNTestLaunch(w http.ResponseWriter, r *http.Request) {
	s.handleMGNTransition(w, r, "REPLICATING", "TEST_LAUNCHED", "test-launch")
}

func (s *Server) handleMGNCutover(w http.ResponseWriter, r *http.Request) {
	s.handleMGNTransition(w, r, "TEST_LAUNCHED", "CUTOVER", "cut over")
}

// --- FSx / EFS mount targets -----------------------------------------------------

func (s *Server) examAddMountTarget(w http.ResponseWriter, r *http.Request, def examDef) {
	_, e, ok := s.examAction(w, r, def)
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	subnet := strings.TrimSpace(examStr(req, "subnet_id", ""))
	if subnet == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "subnet_id is required")
		return
	}
	targets := examDataList(e, "mount_targets")
	for _, t := range targets {
		if m, ok := t.(map[string]any); ok && m["subnet_id"] == subnet {
			s.writeError(w, r, http.StatusConflict, "already_exists", "a mount target already exists in subnet "+subnet)
			return
		}
	}
	targets = append(targets, map[string]any{
		"id": "mt-" + ids.New()[:8], "subnet_id": subnet, "status": "available", "created_at": p3Now(),
	})
	e.Data["mount_targets"] = targets
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Mount targets are recorded only: no DNS name is registered and no mount succeeds against one."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleFSxMountTarget(w http.ResponseWriter, r *http.Request) {
	s.examAddMountTarget(w, r, examDef{id: "fsx", kind: "file-system"})
}

func (s *Server) handleEFSMountTarget(w http.ResponseWriter, r *http.Request) {
	s.examAddMountTarget(w, r, examDef{id: "efs", kind: "file-system"})
}

// --- EBS --------------------------------------------------------------------------

func (s *Server) handleEBSAttach(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "ebs", kind: "volume"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	labID := strings.TrimSpace(examStr(req, "lab_id", ""))
	if labID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "lab_id is required")
		return
	}
	if examDataStr(e, "status", "") != "available" {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "only an available volume can be attached (current: "+examDataStr(e, "status", "")+")")
		return
	}
	// The lab must really exist; the disk itself is unchanged.
	if s.Labs == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "labs_unavailable", "lab store is not configured on this server")
		return
	}
	if _, err := s.Labs.Get(r.Context(), labID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no lab with id "+labID)
		return
	}
	attachments := examDataList(e, "attachments")
	attachments = append(attachments, map[string]any{
		"lab_id": labID, "device": "/dev/sdf", "attached_at": p3Now(),
	})
	e.Data["attachments"] = attachments
	e.Data["status"] = "in-use"
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Attachment is recorded only: the lab's disk is unchanged and no device is attached to any VM."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleEBSDetach(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "ebs", kind: "volume"})
	if !ok {
		return
	}
	if examDataStr(e, "status", "") != "in-use" {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "only an in-use volume can be detached (current: "+examDataStr(e, "status", "")+")")
		return
	}
	e.Data["attachments"] = []any{}
	e.Data["status"] = "available"
	e.Data["last_detached_at"] = p3Now()
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// --- CloudFront ---------------------------------------------------------------------

func (s *Server) handleCFInvalidation(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "cloudfront", kind: "distribution"})
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	paths := examStrList(req, "paths")
	if len(paths) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "paths (non-empty list) is required")
		return
	}
	invalidations := examDataList(e, "invalidations")
	invalidations = append(invalidations, map[string]any{
		"id": "I" + ids.New()[:8], "paths": paths, "status": "COMPLETED",
		"created_at": p3Now(), "completed_at": p3Now(),
	})
	e.Data["invalidations"] = invalidations
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Invalidations are teaching records: nothing is cached at any edge, so nothing is actually invalidated."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleCFListInvalidations(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, examDef{id: "cloudfront", kind: "distribution"})
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"invalidations": examDataList(e, "invalidations"),
		"count":         len(examDataList(e, "invalidations")),
	})
}

// --- VPN ------------------------------------------------------------------------------

func (s *Server) handleVPNTunnel(w http.ResponseWriter, r *http.Request, up bool) {
	_, e, ok := s.examAction(w, r, examDef{id: "vpn", kind: "vpn-connection"})
	if !ok {
		return
	}
	want := "DOWN"
	if up {
		want = "UP"
	}
	if examDataStr(e, "tunnel_status", "") == want {
		s.writeError(w, r, http.StatusConflict, "invalid_state", "tunnel is already "+want)
		return
	}
	e.Data["tunnel_status"] = want
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := p3View(e)
	out["note"] = "Tunnel state is a teaching field: no IPsec tunnel exists and no traffic flows."
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleVPNActivate(w http.ResponseWriter, r *http.Request) {
	s.handleVPNTunnel(w, r, true)
}

func (s *Server) handleVPNDeactivate(w http.ResponseWriter, r *http.Request) {
	s.handleVPNTunnel(w, r, false)
}

// --- NACLs ----------------------------------------------------------------------------

// naclRule is one parsed ACL rule.
type naclRule struct {
	Number    int
	Direction string
	Protocol  string
	PortFrom  int
	PortTo    int
	CIDR      string
	Action    string
}

func parseNACLRule(raw any) (naclRule, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return naclRule{}, false
	}
	num, _ := examInt(m, "rule_number", 0)
	from, _ := examInt(m, "port_from", 0)
	to, _ := examInt(m, "port_to", 0)
	return naclRule{
		Number:    int(num),
		Direction: phase3.Str(m, "direction", ""),
		Protocol:  strings.ToLower(phase3.Str(m, "protocol", "")),
		PortFrom:  int(from),
		PortTo:    int(to),
		CIDR:      phase3.Str(m, "cidr", ""),
		Action:    strings.ToLower(phase3.Str(m, "action", "")),
	}, true
}

var defNACL = examDef{id: "nacl", kind: "network-acl"}

func (s *Server) handleNACLAddRule(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defNACL)
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	num, err := examInt(req, "rule_number", 0)
	if err != nil || num < 1 || num > 32766 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rule_number must be between 1 and 32766")
		return
	}
	direction := examStr(req, "direction", "")
	if direction != "ingress" && direction != "egress" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "direction must be ingress or egress")
		return
	}
	protocol := strings.ToLower(examStr(req, "protocol", ""))
	switch protocol {
	case "tcp", "udp", "icmp", "all", "-1":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be tcp, udp, icmp, all or -1")
		return
	}
	action := strings.ToLower(examStr(req, "action", ""))
	if action != "allow" && action != "deny" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "action must be allow or deny")
		return
	}
	cidr := examStr(req, "cidr", "")
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "cidr must be a valid CIDR block")
		return
	}
	from, err := examInt(req, "port_from", 0)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	to, err := examInt(req, "port_to", 65535)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if protocol == "tcp" || protocol == "udp" {
		if from < 0 || from > 65535 || to < 0 || to > 65535 || from > to {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "port_from/port_to must be 0-65535 with port_from <= port_to")
			return
		}
	}
	rules := examDataList(e, "rules")
	for _, raw := range rules {
		if rule, ok := parseNACLRule(raw); ok && rule.Number == num && rule.Direction == direction {
			s.writeError(w, r, http.StatusConflict, "already_exists",
				fmt.Sprintf("rule %d already exists for %s (rule numbers are unique per direction)", num, direction))
			return
		}
	}
	rules = append(rules, map[string]any{
		"rule_number": num, "direction": direction, "protocol": protocol,
		"port_from": from, "port_to": to, "cidr": cidr, "action": action,
	})
	e.Data["rules"] = rules
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleNACLListRules(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defNACL)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"rules": examDataList(e, "rules"), "count": len(examDataList(e, "rules")),
	})
}

func (s *Server) handleNACLDeleteRule(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defNACL)
	if !ok {
		return
	}
	num, err := strconv.Atoi(chi.URLParam(r, "ruleNumber"))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "ruleNumber must be an integer")
		return
	}
	direction := strings.TrimSpace(r.URL.Query().Get("direction"))
	if direction != "ingress" && direction != "egress" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "query param direction=ingress|egress is required")
		return
	}
	rules := examDataList(e, "rules")
	kept := []any{}
	removed := false
	for _, raw := range rules {
		if rule, ok := parseNACLRule(raw); ok && rule.Number == num && rule.Direction == direction {
			removed = true
			continue
		}
		kept = append(kept, raw)
	}
	if !removed {
		s.writeError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("no %s rule %d", direction, num))
		return
	}
	e.Data["rules"] = kept
	if _, err := s.examSave(r.Context(), e); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// handleNACLEvaluate really evaluates the stored rules: lowest rule
// number first, first match wins, stateless (ingress and egress are
// evaluated independently), default deny. It evaluates a packet you
// describe; it does not inspect live traffic.
func (s *Server) handleNACLEvaluate(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.examAction(w, r, defNACL)
	if !ok {
		return
	}
	var req map[string]any
	if !s.decodeJSON(w, r, &req) {
		return
	}
	direction := examStr(req, "direction", "")
	if direction != "ingress" && direction != "egress" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "direction must be ingress or egress")
		return
	}
	protocol := strings.ToLower(examStr(req, "protocol", ""))
	switch protocol {
	case "tcp", "udp", "icmp":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be tcp, udp or icmp")
		return
	}
	port, err := examInt(req, "port", -1)
	if err != nil || port < 0 || port > 65535 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "port must be 0-65535")
		return
	}
	ip := net.ParseIP(examStr(req, "source_ip", ""))
	if ip == nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "source_ip must be a valid IP address")
		return
	}
	var rules []naclRule
	for _, raw := range examDataList(e, "rules") {
		if rule, ok := parseNACLRule(raw); ok && rule.Direction == direction {
			rules = append(rules, rule)
		}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Number < rules[j].Number })
	matched := -1
	allowed := false
	for _, rule := range rules {
		if rule.Protocol != "all" && rule.Protocol != "-1" && rule.Protocol != protocol {
			continue
		}
		if (protocol == "tcp" || protocol == "udp") && (port < rule.PortFrom || port > rule.PortTo) {
			continue
		}
		_, network, err := net.ParseCIDR(rule.CIDR)
		if err != nil || !network.Contains(ip) {
			continue
		}
		matched = rule.Number
		allowed = rule.Action == "allow"
		break
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"acl_id": e.ID, "direction": direction, "protocol": protocol,
		"port": port, "source_ip": ip.String(),
		"allowed": allowed, "matched_rule_number": matched,
		"action":       map[bool]string{true: "allow", false: "deny"}[allowed],
		"evaluated_at": p3Now(),
		"note":         "Stateless first-match evaluation over the stored rules (default deny). This ACL filters no live traffic.",
	})
}
