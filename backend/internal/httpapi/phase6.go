// Phase 6 — advanced services (CloudForge v2).
//
// Analytics (Athena SQL over real Postgres, Glue catalog crawled from
// real sources, durable Kinesis streams, Firehose delivery with real
// delivery records, QuickSight charts from live query results),
// Machine Learning (SageMaker notebooks via the sandboxed Python
// runner, real OLS training jobs, versioned model artifacts on disk,
// inference endpoints serving stored weights, Bedrock as a labelled
// SIMULATED playground), Migration (DMS row copies between real
// Postgres databases, DataSync copies with SHA-256 verification,
// Transfer Family UNSUPPORTED, Migration Hub aggregation), Security
// (GuardDuty findings from the real audit log, Security Hub
// aggregation, Inspector checks against real resources, Config rules
// against real state, WAF rule evaluation, durable KMS + Secrets
// Manager with envelope encryption), advanced networking (VPC
// peering / Transit Gateway / VPC endpoints as real route-table
// entries in lab state, Network Firewall with real rule evaluation
// and an engine sync attempt, Flow Logs built only from real engine
// telemetry), and management (Systems Manager via the host-agent
// allowlist, Trusted Advisor checks on real state, Cost Explorer +
// Budgets over the real educational cost model, Organizations over
// the real user store).
//
// Ownership: resources record owner_id; a non-admin sees only their
// own. No resource is shown READY/ACTIVE until the underlying
// operation actually succeeded; failures are FAILED with the error,
// never a fake success. No finding, metric, flow, or model result is
// invented: every number comes from a real store, log, or computation.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/networkclient"
	gen "github.com/cloudforge/cloudforge/backend/internal/networkclient/gen/cloudforge/v1"
	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/cloudforge/cloudforge/backend/internal/phase6"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// registerPhase6Routes mounts every Phase 6 route under /api/v1.
// Called from Routes() after the Phase 3/4 routes.
func (s *Server) registerPhase6Routes(r chi.Router) {
	// Analytics.
	r.Post("/aws/athena/queries", s.handleAthenaCreateQuery)
	r.Get("/aws/athena/queries", s.handleAthenaListQueries)
	r.Get("/aws/athena/queries/{id}", s.handleAthenaGetQuery)
	r.Post("/aws/athena/queries/{id}/execute", s.handleAthenaExecute)
	r.Post("/aws/athena/execute", s.handleAthenaExecute) // ad-hoc: body {query, database}
	r.Delete("/aws/athena/queries/{id}", s.handleAthenaDeleteQuery)
	r.Get("/aws/athena/executions", s.handleAthenaListExecutions)
	r.Post("/aws/glue/crawlers", s.handleGlueCreateCrawler)
	r.Get("/aws/glue/crawlers", s.handleGlueListCrawlers)
	r.Post("/aws/glue/crawlers/{id}/run", s.handleGlueRunCrawler)
	r.Get("/aws/glue/tables", s.handleGlueListTables)
	r.Delete("/aws/glue/crawlers/{id}", s.handleGlueDeleteCrawler)
	r.Post("/aws/kinesis/streams", s.handleKinesisCreateStream)
	r.Get("/aws/kinesis/streams", s.handleKinesisListStreams)
	r.Get("/aws/kinesis/streams/{id}", s.handleKinesisGetStream)
	r.Post("/aws/kinesis/streams/{id}/records", s.handleKinesisPutRecord)
	r.Get("/aws/kinesis/streams/{id}/records", s.handleKinesisGetRecords)
	r.Delete("/aws/kinesis/streams/{id}", s.handleKinesisDeleteStream)
	r.Post("/aws/firehose/streams", s.handleFirehoseCreateStream)
	r.Get("/aws/firehose/streams", s.handleFirehoseListStreams)
	r.Post("/aws/firehose/streams/{id}/records", s.handleFirehosePutRecord)
	r.Get("/aws/firehose/streams/{id}/deliveries", s.handleFirehoseListDeliveries)
	r.Delete("/aws/firehose/streams/{id}", s.handleFirehoseDeleteStream)
	r.Post("/aws/quicksight/dashboards", s.handleQSCreateDashboard)
	r.Get("/aws/quicksight/dashboards", s.handleQSListDashboards)
	r.Get("/aws/quicksight/dashboards/{id}", s.handleQSGetDashboard)
	r.Delete("/aws/quicksight/dashboards/{id}", s.handleQSDeleteDashboard)
	// Machine learning / AI.
	r.Post("/aws/sagemaker/notebooks", s.handleSMCreateNotebook)
	r.Get("/aws/sagemaker/notebooks", s.handleSMListNotebooks)
	r.Get("/aws/sagemaker/notebooks/{id}", s.handleSMGetNotebook)
	r.Post("/aws/sagemaker/notebooks/{id}/execute", s.handleSMExecuteCell)
	r.Delete("/aws/sagemaker/notebooks/{id}", s.handleSMDeleteNotebook)
	r.Post("/aws/sagemaker/training-jobs", s.handleSMCreateTrainingJob)
	r.Get("/aws/sagemaker/training-jobs", s.handleSMListTrainingJobs)
	r.Get("/aws/sagemaker/training-jobs/{id}", s.handleSMGetTrainingJob)
	r.Get("/aws/sagemaker/models", s.handleSMListModels)
	r.Get("/aws/sagemaker/models/{id}", s.handleSMGetModel)
	r.Delete("/aws/sagemaker/models/{id}", s.handleSMDeleteModel)
	r.Post("/aws/sagemaker/endpoints", s.handleSMCreateEndpoint)
	r.Get("/aws/sagemaker/endpoints", s.handleSMListEndpoints)
	r.Post("/aws/sagemaker/endpoints/{id}/invoke", s.handleSMInvokeEndpoint)
	r.Delete("/aws/sagemaker/endpoints/{id}", s.handleSMDeleteEndpoint)
	r.Get("/aws/bedrock/models", s.handleBedrockListModels)
	r.Post("/aws/bedrock/invoke", s.handleBedrockInvoke)
	r.Get("/aws/bedrock/invocations", s.handleBedrockListInvocations)
	// Migration and transfer.
	r.Post("/aws/dms/tasks", s.handleDMSCreateTask)
	r.Get("/aws/dms/tasks", s.handleDMSListTasks)
	r.Get("/aws/dms/tasks/{id}", s.handleDMSGetTask)
	r.Get("/aws/dms/tasks/{id}/logs", s.handleDMSListLogs)
	r.Delete("/aws/dms/tasks/{id}", s.handleDMSDeleteTask)
	r.Post("/aws/datasync/tasks", s.handleSyncCreateTask)
	r.Get("/aws/datasync/tasks", s.handleSyncListTasks)
	r.Get("/aws/datasync/tasks/{id}", s.handleSyncGetTask)
	r.Get("/aws/datasync/tasks/{id}/logs", s.handleSyncListLogs)
	r.Delete("/aws/datasync/tasks/{id}", s.handleSyncDeleteTask)
	r.Get("/aws/transfer/info", s.handleTransferInfo)
	r.Get("/aws/migrationhub/tasks", s.handleMigrationHubTasks)
	// Security.
	r.Post("/aws/guardduty/detectors", s.handleGDCreateDetector)
	r.Get("/aws/guardduty/detectors", s.handleGDListDetectors)
	r.Post("/aws/guardduty/detectors/{id}/scan", s.handleGDScan)
	r.Get("/aws/guardduty/findings", s.handleGDListFindings)
	r.Post("/aws/guardduty/findings/{id}/status", s.handleFindingStatus)
	r.Get("/aws/securityhub/summary", s.handleSecHubSummary)
	r.Get("/aws/securityhub/findings", s.handleSecHubFindings)
	r.Post("/aws/inspector/assessments", s.handleInspectorRunAssessment)
	r.Get("/aws/inspector/assessments", s.handleInspectorListAssessments)
	r.Get("/aws/inspector/assessments/{id}", s.handleInspectorGetAssessment)
	r.Post("/aws/config/rules", s.handleConfigCreateRule)
	r.Get("/aws/config/rules", s.handleConfigListRules)
	r.Post("/aws/config/rules/{id}/evaluate", s.handleConfigEvaluateRule)
	r.Get("/aws/config/results", s.handleConfigListResults)
	r.Post("/aws/config/snapshots", s.handleConfigSnapshot)
	r.Get("/aws/config/items", s.handleConfigListItems)
	r.Delete("/aws/config/rules/{id}", s.handleConfigDeleteRule)
	r.Post("/aws/waf/acls", s.handleWAFCreateACL)
	r.Get("/aws/waf/acls", s.handleWAFListACLs)
	r.Get("/aws/waf/acls/{id}", s.handleWAFGetACL)
	r.Post("/aws/waf/acls/{id}/evaluate", s.handleWAFEvaluate)
	r.Delete("/aws/waf/acls/{id}", s.handleWAFDeleteACL)
	r.Post("/aws/kms/keys", s.handleKMSCreateKey)
	r.Get("/aws/kms/keys", s.handleKMSListKeys)
	r.Post("/aws/kms/keys/{id}/encrypt", s.handleKMSEncrypt)
	r.Post("/aws/kms/keys/{id}/decrypt", s.handleKMSDecrypt)
	r.Post("/aws/kms/keys/{id}/rotate", s.handleKMSRotate)
	r.Delete("/aws/kms/keys/{id}", s.handleKMSDeleteKey)
	r.Post("/aws/secretsmanager/secrets", s.handleSecretsCreate)
	r.Get("/aws/secretsmanager/secrets", s.handleSecretsList)
	r.Get("/aws/secretsmanager/secrets/{id}", s.handleSecretsGet)
	r.Post("/aws/secretsmanager/secrets/{id}/value", s.handleSecretsPutValue)
	r.Delete("/aws/secretsmanager/secrets/{id}", s.handleSecretsDelete)
	// Advanced networking.
	r.Post("/aws/vpc/peerings", s.handlePeeringCreate)
	r.Get("/aws/vpc/peerings", s.handlePeeringList)
	r.Delete("/aws/vpc/peerings/{id}", s.handlePeeringDelete)
	r.Post("/aws/transit-gateways", s.handleTGWCreate)
	r.Get("/aws/transit-gateways", s.handleTGWList)
	r.Post("/aws/transit-gateways/{id}/attachments", s.handleTGWAttach)
	r.Delete("/aws/transit-gateways/{id}/attachments/{attId}", s.handleTGWDetach)
	r.Delete("/aws/transit-gateways/{id}", s.handleTGWDelete)
	r.Post("/aws/vpc/endpoints", s.handleVPCEndpointCreate)
	r.Get("/aws/vpc/endpoints", s.handleVPCEndpointList)
	r.Delete("/aws/vpc/endpoints/{id}", s.handleVPCEndpointDelete)
	r.Post("/aws/network-firewall/firewalls", s.handleNFWCreate)
	r.Get("/aws/network-firewall/firewalls", s.handleNFWList)
	r.Post("/aws/network-firewall/firewalls/{id}/evaluate", s.handleNFWEvaluate)
	r.Delete("/aws/network-firewall/firewalls/{id}", s.handleNFWDelete)
	r.Post("/aws/vpc/flow-logs", s.handleFlowLogCreateGroup)
	r.Get("/aws/vpc/flow-logs", s.handleFlowLogListGroups)
	r.Post("/aws/vpc/flow-logs/{id}/capture", s.handleFlowLogCapture)
	r.Get("/aws/vpc/flow-logs/{id}/records", s.handleFlowLogListRecords)
	r.Delete("/aws/vpc/flow-logs/{id}", s.handleFlowLogDeleteGroup)
	// Management and governance.
	r.Get("/aws/ssm/inventory", s.handleSSMInventory)
	r.Get("/aws/ssm/instances", s.handleSSMInstances)
	r.Post("/aws/ssm/commands", s.handleSSMRunCommand)
	r.Get("/aws/ssm/commands", s.handleSSMListCommands)
	r.Post("/aws/trustedadvisor/run", s.handleAdvisorRun)
	r.Get("/aws/trustedadvisor/results", s.handleAdvisorListResults)
	r.Get("/aws/costexplorer/costs", s.handleCostExplorer)
	r.Post("/aws/costexplorer/budgets", s.handleBudgetCreate)
	r.Get("/aws/costexplorer/budgets", s.handleBudgetList)
	r.Get("/aws/costexplorer/budgets/{id}", s.handleBudgetGet)
	r.Delete("/aws/costexplorer/budgets/{id}", s.handleBudgetDelete)
	r.Post("/aws/organizations/units", s.handleOrgCreateUnit)
	r.Get("/aws/organizations/units", s.handleOrgListUnits)
	r.Post("/aws/organizations/units/{id}/members", s.handleOrgAddMember)
	r.Delete("/aws/organizations/units/{id}", s.handleOrgDeleteUnit)
}

// --- Phase 6 shared helpers -------------------------------------------------

// p6Master returns the KMS wrapping key for this server. The master
// secret is CLOUDFORGE_KMS_MASTER_KEY when set, else the JWT secret
// (wired in main.go); tests set P6MasterKey explicitly. The second
// return reports whether the dev default is in use.
func (s *Server) p6Master() ([]byte, string) {
	secret := s.P6MasterKey
	source := "CLOUDFORGE_KMS_MASTER_KEY"
	if secret == "" {
		secret = "dev-only-change-me-kms-master"
		source = "dev-default (set CLOUDFORGE_KMS_MASTER_KEY in production)"
	}
	return phase6.MasterKey(secret), source
}

// p6List returns the caller's visible entities in a collection.
func (s *Server) p6List(ctx context.Context, collection string, u *auth.User) ([]*store.P3Entity, error) {
	all, err := s.P3.P3List(ctx, collection)
	if err != nil {
		return nil, err
	}
	return p3Own(all, u.ID, p3IsAdmin(u.Role)), nil
}

func (s *Server) p6GetOwned(w http.ResponseWriter, r *http.Request, collection, id string, u *auth.User) (*store.P3Entity, bool) {
	e, err := s.P3.P3Get(r.Context(), collection, id)
	if err != nil {
		s.p3Err(w, r, err)
		return nil, false
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return nil, false
	}
	return e, true
}

func (s *Server) p6DeleteOwned(w http.ResponseWriter, r *http.Request, collection, id string, u *auth.User) bool {
	if _, ok := s.p6GetOwned(w, r, collection, id, u); !ok {
		return false
	}
	if err := s.P3.P3Delete(r.Context(), collection, id); err != nil {
		s.p3Err(w, r, err)
		return false
	}
	return true
}

// p6PG opens a pgx pool to the platform Postgres, optionally with
// the database replaced (RDS instance targets). The caller closes it.
func (s *Server) p6PG(ctx context.Context, database string) (*pgxpool.Pool, error) {
	if s.P3DatabaseURL == "" {
		return nil, errors.New("no platform Postgres configured (P3DatabaseURL is empty)")
	}
	cfg, err := pgxpool.ParseConfig(s.P3DatabaseURL)
	if err != nil {
		return nil, err
	}
	if database != "" {
		if err := phase6.ValidateIdentifier(database); err != nil {
			return nil, err
		}
		cfg.ConnConfig.Database = database
	}
	cfg.MaxConns = 2
	return pgxpool.NewWithConfig(ctx, cfg)
}

// p6ResolveTargetDB resolves an Athena/DMS database target: "platform"
// (the platform Postgres) or {"kind": "rds", "instance_id": ...}.
// It returns the database name to connect to.
func (s *Server) p6ResolveTargetDB(ctx context.Context, kind, instanceID string) (string, string, error) {
	switch kind {
	case "", "platform":
		cfg, err := pgxpool.ParseConfig(s.P3DatabaseURL)
		if err != nil {
			return "", "", err
		}
		return cfg.ConnConfig.Database, "platform", nil
	case "rds":
		e, err := s.P3.P3Get(ctx, "rds_instances", instanceID)
		if err != nil {
			return "", "", fmt.Errorf("no RDS instance with id %q", instanceID)
		}
		return phase3.Str(e.Data, "database_name", ""), "rds:" + e.Name, nil
	default:
		return "", "", fmt.Errorf("unknown database kind %q (platform or rds)", kind)
	}
}

var p6SeqCounter atomic.Uint64

// p6Sequence returns a monotonically increasing sequence string for
// Kinesis records (zero-padded unix nanos + a process counter so two
// records in the same nanosecond still order deterministically).
func p6Sequence() string {
	return fmt.Sprintf("%020d-%06d", time.Now().UnixNano(), p6SeqCounter.Add(1)%1000000)
}

func p6SHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func p6RandBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// p6CreateFinding stores one finding in the shared findings table.
// Source is guardduty | inspector | config | advisor. Severity is
// LOW | MEDIUM | HIGH | CRITICAL. Status starts NEW.
func (s *Server) p6CreateFinding(ctx context.Context, ownerID, source, title, severity, resourceType, resourceID, detail string) (*store.P3Entity, error) {
	return s.P3.P3Create(ctx, "findings", &store.P3Entity{Name: title, Data: map[string]any{
		"source": source, "title": title, "severity": severity,
		"status": "NEW", "resource_type": resourceType, "resource_id": resourceID,
		"detail": detail, "owner_id": ownerID, "created_at": p3Now(),
	}})
}

// --- Athena -----------------------------------------------------------------

func (s *Server) handleAthenaCreateQuery(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Query    string `json:"query"`
		Database string `json:"database"` // "platform" (default) or "rds:<instance-id>"
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 80 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required (max 80 characters)")
		return
	}
	if err := phase6.ValidateAthenaSQL(req.Query); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	db := strings.TrimSpace(req.Database)
	if db == "" {
		db = "platform"
	}
	e, err := st.P3Create(r.Context(), "athena_queries", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"query": req.Query, "database": db, "owner_id": u.ID,
		"fidelity": "PARTIAL", "engine_note": "PostgreSQL SQL, not Presto/Athena",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleAthenaListQueries(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	list, err := s.p6List(r.Context(), "athena_queries", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	_ = st
	s.writeJSON(w, r, http.StatusOK, map[string]any{"queries": p3ListView(list), "count": len(list),
		"engine_note": "PostgreSQL SQL, not Presto/Athena"})
}

func (s *Server) handleAthenaGetQuery(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "athena_queries", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleAthenaDeleteQuery(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "athena_queries", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleAthenaListExecutions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	qid := r.URL.Query().Get("query_id")
	list, err := s.p6List(r.Context(), "athena_executions", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range list {
		if qid == "" || phase3.Str(e.Data, "query_id", "") == qid {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"executions": p3ListView(out), "count": len(out)})
}

// handleAthenaExecute runs a saved query (or an ad-hoc query body)
// read-only against the platform Postgres or an RDS instance DB.
// The result rows are real rows from that database; errors are
// honest failures, never empty success.
func (s *Server) handleAthenaExecute(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Query    string `json:"query"`    // ad-hoc; empty = run the saved query
		Database string `json:"database"` // platform | rds:<instance-id>
		Limit    int    `json:"limit"`    // max rows returned (default 100, max 1000)
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	qid := chi.URLParam(r, "id")
	query := strings.TrimSpace(req.Query)
	dbSpec := strings.TrimSpace(req.Database)
	if qid != "" {
		saved, ok := s.p6GetOwned(w, r, "athena_queries", qid, u)
		if !ok {
			return
		}
		if query == "" {
			query = phase3.Str(saved.Data, "query", "")
		}
		if dbSpec == "" {
			dbSpec = phase3.Str(saved.Data, "database", "platform")
		}
	}
	if err := phase6.ValidateAthenaSQL(query); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if dbSpec == "" {
		dbSpec = "platform"
	}
	kind, instID := "platform", ""
	if strings.HasPrefix(dbSpec, "rds:") {
		kind, instID = "rds", strings.TrimPrefix(dbSpec, "rds:")
	} else if dbSpec != "platform" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "database must be \"platform\" or \"rds:<instance-id>\"")
		return
	}
	dbName, dbLabel, err := s.p6ResolveTargetDB(r.Context(), kind, instID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", err.Error())
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	start := time.Now()
	pool, err := s.p6PG(r.Context(), dbName)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", err.Error())
		return
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		s.recordAthenaExecution(r.Context(), st, qid, u.ID, query, dbLabel, 0, time.Since(start), err.Error())
		s.writeError(w, r, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	defer rows.Close()
	cols := []string{}
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, fd.Name)
	}
	outRows := []map[string]any{}
	for rows.Next() {
		if len(outRows) >= limit {
			break
		}
		vals, err := rows.Values()
		if err != nil {
			break
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = p6PGValue(vals[i])
		}
		outRows = append(outRows, row)
	}
	if err := rows.Err(); err != nil {
		s.recordAthenaExecution(r.Context(), st, qid, u.ID, query, dbLabel, 0, time.Since(start), err.Error())
		s.writeError(w, r, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	dur := time.Since(start)
	s.recordAthenaExecution(r.Context(), st, qid, u.ID, query, dbLabel, len(outRows), dur, "")
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"columns": cols, "rows": outRows, "row_count": len(outRows),
		"database": dbLabel, "duration_ms": dur.Milliseconds(),
		"engine_note": "PostgreSQL SQL, not Presto/Athena",
	})
}

func (s *Server) recordAthenaExecution(ctx context.Context, st store.Phase3Store, queryID, ownerID, query, database string, rowCount int, dur time.Duration, errMsg string) {
	status := "SUCCEEDED"
	if errMsg != "" {
		status = "FAILED"
	}
	_, _ = st.P3Create(ctx, "athena_executions", &store.P3Entity{Name: "exec-" + p6Sequence(), Data: map[string]any{
		"query_id": queryID, "query": query, "database": database,
		"row_count": rowCount, "duration_ms": dur.Milliseconds(),
		"status": status, "error": errMsg, "owner_id": ownerID,
	}})
}

// p6PGValue converts a pgx driver value into something JSON can carry.
func p6PGValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case nil:
		return nil
	default:
		return v
	}
}

// --- Glue -------------------------------------------------------------------

func (s *Server) handleGlueCreateCrawler(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string   `json:"name"`
		Sources []string `json:"sources"` // subset of: postgres, dynamodb, s3
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if len(req.Sources) == 0 {
		req.Sources = []string{"postgres"}
	}
	for _, src := range req.Sources {
		switch src {
		case "postgres", "dynamodb", "s3":
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("unknown crawler source %q (postgres, dynamodb or s3)", src))
			return
		}
	}
	e, err := st.P3Create(r.Context(), "glue_crawlers", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"sources": req.Sources, "status": "READY", "owner_id": u.ID,
		"last_run": nil, "tables_found": 0,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleGlueListCrawlers(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "glue_crawlers", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"crawlers": p3ListView(list), "count": len(list)})
}

func (s *Server) handleGlueDeleteCrawler(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "glue_crawlers", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleGlueRunCrawler crawls the crawler's sources for real and
// upserts glue_tables rows (table + columns). A source that cannot
// be reached is reported per-source and the run is PARTIAL — never
// silently skipped, never faked.
func (s *Server) handleGlueRunCrawler(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	crawler, ok := s.p6GetOwned(w, r, "glue_crawlers", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	sources := phase3.StrSlice(crawler.Data, "sources")
	results := map[string]any{}
	total := 0
	anyFailed := false
	for _, src := range sources {
		switch src {
		case "postgres":
			n, err := s.glueCrawlPostgres(r.Context(), st, crawler.ID, u.ID)
			if err != nil {
				anyFailed = true
				results["postgres"] = map[string]any{"status": "FAILED", "error": err.Error(), "tables": 0}
			} else {
				total += n
				results["postgres"] = map[string]any{"status": "SUCCEEDED", "tables": n}
			}
		case "dynamodb":
			n, err := s.glueCrawlDynamoDB(r.Context(), st, crawler.ID, u.ID)
			if err != nil {
				anyFailed = true
				results["dynamodb"] = map[string]any{"status": "FAILED", "error": err.Error(), "tables": 0}
			} else {
				total += n
				results["dynamodb"] = map[string]any{"status": "SUCCEEDED", "tables": n}
			}
		case "s3":
			n, err := s.glueCrawlS3(r.Context(), st, crawler.ID, u.ID)
			if err != nil {
				anyFailed = true
				results["s3"] = map[string]any{"status": "FAILED", "error": err.Error(), "tables": 0}
			} else {
				total += n
				results["s3"] = map[string]any{"status": "SUCCEEDED", "tables": n}
			}
		}
	}
	status := "SUCCEEDED"
	if anyFailed {
		status = "PARTIAL"
	}
	crawler.Data["status"] = status
	crawler.Data["last_run"] = p3Now()
	crawler.Data["tables_found"] = total
	crawler.Data["last_run_results"] = results
	if _, err := st.P3Update(r.Context(), "glue_crawlers", crawler); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"crawler": p3View(crawler), "status": status,
		"tables_found": total, "sources": results,
		"note": "Catalog rows are real tables/columns crawled from live sources; a failed source is reported, not skipped.",
	})
}

// glueCrawlPostgres records every user table in the platform Postgres
// (public schema) with its real columns.
func (s *Server) glueCrawlPostgres(ctx context.Context, st store.Phase3Store, crawlerID, ownerID string) (int, error) {
	pool, err := s.p6PG(ctx, "")
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	rows, err := pool.Query(ctx, `
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name NOT LIKE 'pg_%'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	tables := map[string][]map[string]any{}
	for rows.Next() {
		var table, col, typ string
		if err := rows.Scan(&table, &col, &typ); err != nil {
			return 0, err
		}
		tables[table] = append(tables[table], map[string]any{"name": col, "type": typ})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.glueUpsertTable(ctx, st, crawlerID, ownerID, "postgres", name, tables[name]); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}

func (s *Server) glueUpsertTable(ctx context.Context, st store.Phase3Store, crawlerID, ownerID, source, table string, columns []map[string]any) error {
	name := source + "." + table
	existing, _ := st.P3GetByName(ctx, "glue_tables", name)
	cols := []any{}
	for _, c := range columns {
		cols = append(cols, c)
	}
	data := map[string]any{
		"source": source, "table": table, "columns": cols,
		"column_count": len(columns), "crawler_id": crawlerID, "owner_id": ownerID,
		"crawled_at": p3Now(),
	}
	if existing != nil {
		existing.Data = data
		_, err := st.P3Update(ctx, "glue_tables", existing)
		return err
	}
	_, err := st.P3Create(ctx, "glue_tables", &store.P3Entity{Name: name, Data: data})
	return err
}

// glueCrawlDynamoDB records every DynamoDB table the Phase 3 store
// knows, with the key attribute as its (single) known column.
func (s *Server) glueCrawlDynamoDB(ctx context.Context, st store.Phase3Store, crawlerID, ownerID string) (int, error) {
	tables, err := st.P3List(ctx, "dynamodb_tables")
	if err != nil {
		return 0, err
	}
	for _, t := range tables {
		key := phase3.Str(t.Data, "key_attribute", "id")
		cols := []map[string]any{{"name": key, "type": "hash_key"}}
		if err := s.glueUpsertTable(ctx, st, crawlerID, ownerID, "dynamodb", t.Name, cols); err != nil {
			return 0, err
		}
	}
	return len(tables), nil
}

// glueCrawlS3 records every bucket as a catalog table (one row per
// bucket, columns = object key/size from a real listing). MinIO down
// is an honest error, not an empty catalog.
func (s *Server) glueCrawlS3(ctx context.Context, st store.Phase3Store, crawlerID, ownerID string) (int, error) {
	if s.AWS == nil || s.AWS.S3 == nil {
		return 0, errors.New("S3 adapter is not configured (no MinIO endpoint)")
	}
	buckets, err := s.AWS.S3.ListBuckets(ctx)
	if err != nil {
		return 0, fmt.Errorf("S3 list failed: %w", err)
	}
	for _, b := range buckets {
		cols := []map[string]any{{"name": "key", "type": "string"}, {"name": "size_bytes", "type": "bigint"}}
		if err := s.glueUpsertTable(ctx, st, crawlerID, ownerID, "s3", b.Name, cols); err != nil {
			return 0, err
		}
	}
	return len(buckets), nil
}

func (s *Server) handleGlueListTables(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	src := r.URL.Query().Get("source")
	list, err := s.p6List(r.Context(), "glue_tables", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range list {
		if src == "" || phase3.Str(e.Data, "source", "") == src {
			out = append(out, e)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tables": p3ListView(out), "count": len(out)})
}

// --- Kinesis ----------------------------------------------------------------

func (s *Server) handleKinesisCreateStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string `json:"name"`
		ShardCount int    `json:"shard_count"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.ShardCount <= 0 {
		req.ShardCount = 1
	}
	if req.ShardCount > 8 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "shard_count must be 1-8")
		return
	}
	e, err := st.P3Create(r.Context(), "kinesis_streams", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"shard_count": req.ShardCount, "status": "ACTIVE", "owner_id": u.ID,
		"record_count": 0,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleKinesisListStreams(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "kinesis_streams", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"streams": p3ListView(list), "count": len(list)})
}

func (s *Server) handleKinesisGetStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "kinesis_streams", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	// Real record count, recomputed from the records table.
	recs, _ := st.P3List(r.Context(), "kinesis_records")
	n := 0
	for _, rec := range recs {
		if phase3.Str(rec.Data, "stream_id", "") == e.ID {
			n++
		}
	}
	view := p3View(e)
	view["record_count"] = n
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleKinesisDeleteStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := s.p6GetOwned(w, r, "kinesis_streams", id, u); !ok {
		return
	}
	recs, _ := st.P3List(r.Context(), "kinesis_records")
	for _, rec := range recs {
		if phase3.Str(rec.Data, "stream_id", "") == id {
			_ = st.P3Delete(r.Context(), "kinesis_records", rec.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "kinesis_streams", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleKinesisPutRecord(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stream, ok := s.p6GetOwned(w, r, "kinesis_streams", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		PartitionKey string `json:"partition_key"`
		Data         string `json:"data"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.PartitionKey) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "partition_key is required")
		return
	}
	if len(req.Data) > 1024*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "data is capped at 1 MiB per record")
		return
	}
	shards := int(phase3.Int(stream.Data, "shard_count", 1))
	shard := phase6.KinesisShard(req.PartitionKey, shards)
	seq := p6Sequence()
	e, err := st.P3Create(r.Context(), "kinesis_records", &store.P3Entity{Name: seq, Data: map[string]any{
		"stream_id": stream.ID, "partition_key": req.PartitionKey,
		"data": req.Data, "shard": shard, "sequence_number": seq,
		"owner_id": u.ID, "created_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"sequence_number": seq, "shard": shard, "record_id": e.ID,
	})
}

func (s *Server) handleKinesisGetRecords(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stream, ok := s.p6GetOwned(w, r, "kinesis_streams", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	after := r.URL.Query().Get("after_sequence")
	shardQ := r.URL.Query().Get("shard")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	all, err := st.P3List(r.Context(), "kinesis_records")
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, rec := range all {
		if phase3.Str(rec.Data, "stream_id", "") != stream.ID {
			continue
		}
		if shardQ != "" && fmt.Sprintf("%v", rec.Data["shard"]) != shardQ {
			continue
		}
		if after != "" && phase3.Str(rec.Data, "sequence_number", "") <= after {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return phase3.Str(out[i].Data, "sequence_number", "") < phase3.Str(out[j].Data, "sequence_number", "")
	})
	if len(out) > limit {
		out = out[:limit]
	}
	next := ""
	if len(out) > 0 {
		next = phase3.Str(out[len(out)-1].Data, "sequence_number", "")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"records": p3ListView(out), "count": len(out),
		"next_sequence": next,
	})
}

// --- Firehose ---------------------------------------------------------------

func (s *Server) handleFirehoseCreateStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Destination string `json:"destination"` // s3 | postgres
		Bucket      string `json:"bucket"`      // s3 destination
		Table       string `json:"table"`       // postgres destination (default firehose_<name>)
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Destination != "s3" && req.Destination != "postgres" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "destination must be s3 or postgres")
		return
	}
	if req.Destination == "s3" && strings.TrimSpace(req.Bucket) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "bucket is required for the s3 destination")
		return
	}
	table := strings.TrimSpace(req.Table)
	if req.Destination == "postgres" {
		if table == "" {
			table = "firehose_" + strings.ReplaceAll(req.Name, "-", "_")
		}
		if err := phase6.ValidateIdentifier(table); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	e, err := st.P3Create(r.Context(), "firehose_streams", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"destination": req.Destination, "bucket": req.Bucket, "table": table,
		"status": "ACTIVE", "owner_id": u.ID,
		"records_delivered": 0, "bytes_delivered": 0,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleFirehoseListStreams(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "firehose_streams", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"streams": p3ListView(list), "count": len(list)})
}

func (s *Server) handleFirehoseDeleteStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "firehose_streams", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleFirehosePutRecord delivers one record to the stream's
// destination right away and stores a real delivery record: to S3
// via the MinIO adapter (honest 503 when MinIO is down), or to a
// Postgres table as a JSONB row.
func (s *Server) handleFirehosePutRecord(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stream, ok := s.p6GetOwned(w, r, "firehose_streams", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Data) > 1024*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "data is capped at 1 MiB per record")
		return
	}
	dest := phase3.Str(stream.Data, "destination", "")
	deliveredAt := p3Now()
	status, detail := "DELIVERED", ""
	switch dest {
	case "s3":
		if s.AWS == nil || s.AWS.S3 == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "s3_unavailable", "S3 adapter is not configured (no MinIO endpoint)")
			return
		}
		key := fmt.Sprintf("firehose/%s/%s.json", stream.Name, p6Sequence())
		if err := s.AWS.S3.PutObject(r.Context(), phase3.Str(stream.Data, "bucket", ""), key, []byte(req.Data)); err != nil {
			status, detail = "FAILED", err.Error()
		} else {
			detail = "s3://" + phase3.Str(stream.Data, "bucket", "") + "/" + key
		}
	case "postgres":
		table := phase3.Str(stream.Data, "table", "")
		pool, err := s.p6PG(r.Context(), "")
		if err != nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", err.Error())
			return
		}
		defer pool.Close()
		if _, err := pool.Exec(r.Context(), fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (id TEXT PRIMARY KEY, payload JSONB NOT NULL, received_at TIMESTAMPTZ NOT NULL DEFAULT now())`, table)); err != nil {
			status, detail = "FAILED", err.Error()
			break
		}
		var payload any
		if err := json.Unmarshal([]byte(req.Data), &payload); err != nil {
			payload = map[string]any{"raw": req.Data}
		}
		payloadJSON, _ := json.Marshal(payload)
		if _, err := pool.Exec(r.Context(), fmt.Sprintf(`INSERT INTO %q (id, payload) VALUES ($1, $2::jsonb)`, table), p6Sequence(), string(payloadJSON)); err != nil {
			status, detail = "FAILED", err.Error()
		} else {
			detail = "postgres table " + table
		}
	}
	rec, err := st.P3Create(r.Context(), "firehose_deliveries", &store.P3Entity{Name: p6Sequence(), Data: map[string]any{
		"stream_id": stream.ID, "status": status, "detail": detail,
		"bytes": len(req.Data), "delivered_at": deliveredAt, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if status == "DELIVERED" {
		stream.Data["records_delivered"] = phase3.Float(stream.Data, "records_delivered", 0) + 1
		stream.Data["bytes_delivered"] = phase3.Float(stream.Data, "bytes_delivered", 0) + float64(len(req.Data))
		_, _ = st.P3Update(r.Context(), "firehose_streams", stream)
		s.writeJSON(w, r, http.StatusCreated, map[string]any{"delivery_id": rec.ID, "status": status, "detail": detail})
		return
	}
	s.writeJSON(w, r, http.StatusBadGateway, map[string]any{"delivery_id": rec.ID, "status": status, "error": detail})
}

func (s *Server) handleFirehoseListDeliveries(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stream, ok := s.p6GetOwned(w, r, "firehose_streams", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "firehose_deliveries")
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "stream_id", "") == stream.ID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deliveries": p3ListView(out), "count": len(out)})
}

// --- QuickSight ---------------------------------------------------------------

func (s *Server) handleQSCreateDashboard(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name      string `json:"name"`
		QueryID   string `json:"query_id"`
		ChartType string `json:"chart_type"` // bar | line
		LabelCol  string `json:"label_column"`
		ValueCol  string `json:"value_column"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.ChartType != "bar" && req.ChartType != "line" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "chart_type must be bar or line")
		return
	}
	if req.QueryID != "" {
		if _, err := st.P3Get(r.Context(), "athena_queries", req.QueryID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no saved Athena query with that id")
			return
		}
	}
	e, err := st.P3Create(r.Context(), "quicksight_dashboards", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"query_id": req.QueryID, "chart_type": req.ChartType,
		"label_column": req.LabelCol, "value_column": req.ValueCol,
		"owner_id": u.ID, "fidelity": "PARTIAL",
		"note": "Charts render from live Athena query results — no sample data.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleQSListDashboards(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "quicksight_dashboards", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"dashboards": p3ListView(list), "count": len(list)})
}

// handleQSGetDashboard returns the dashboard plus the live chart
// data: the saved Athena query is executed for real and the label /
// value columns are extracted from the actual result rows.
func (s *Server) handleQSGetDashboard(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	d, ok := s.p6GetOwned(w, r, "quicksight_dashboards", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	view := p3View(d)
	qid := phase3.Str(d.Data, "query_id", "")
	if qid == "" {
		view["chart_data"] = []any{}
		view["chart_error"] = "no Athena query linked yet"
		s.writeJSON(w, r, http.StatusOK, view)
		return
	}
	saved, err := s.P3.P3Get(r.Context(), "athena_queries", qid)
	if err != nil {
		view["chart_data"] = []any{}
		view["chart_error"] = "linked Athena query no longer exists"
		s.writeJSON(w, r, http.StatusOK, view)
		return
	}
	query := phase3.Str(saved.Data, "query", "")
	dbSpec := phase3.Str(saved.Data, "database", "platform")
	kind, instID := "platform", ""
	if strings.HasPrefix(dbSpec, "rds:") {
		kind, instID = "rds", strings.TrimPrefix(dbSpec, "rds:")
	}
	dbName, _, err := s.p6ResolveTargetDB(r.Context(), kind, instID)
	if err != nil {
		view["chart_data"] = []any{}
		view["chart_error"] = err.Error()
		s.writeJSON(w, r, http.StatusOK, view)
		return
	}
	pool, err := s.p6PG(r.Context(), dbName)
	if err != nil {
		view["chart_data"] = []any{}
		view["chart_error"] = err.Error()
		s.writeJSON(w, r, http.StatusOK, view)
		return
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		view["chart_data"] = []any{}
		view["chart_error"] = err.Error()
		s.writeJSON(w, r, http.StatusOK, view)
		return
	}
	defer rows.Close()
	cols := []string{}
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, fd.Name)
	}
	labelCol := phase3.Str(d.Data, "label_column", "")
	valueCol := phase3.Str(d.Data, "value_column", "")
	if labelCol == "" && len(cols) > 0 {
		labelCol = cols[0]
	}
	if valueCol == "" && len(cols) > 1 {
		valueCol = cols[1]
	}
	points := []map[string]any{}
	for rows.Next() && len(points) < 100 {
		vals, err := rows.Values()
		if err != nil {
			break
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = p6PGValue(vals[i])
		}
		label := fmt.Sprintf("%v", row[labelCol])
		val, _ := row[valueCol].(float64)
		if f, ok := row[valueCol].(int64); ok {
			val = float64(f)
		}
		if f, ok := row[valueCol].(int32); ok {
			val = float64(f)
		}
		points = append(points, map[string]any{"label": label, "value": val})
	}
	view["chart_data"] = points
	view["label_column"] = labelCol
	view["value_column"] = valueCol
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleQSDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "quicksight_dashboards", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- SageMaker ---------------------------------------------------------------

func (s *Server) handleSMCreateNotebook(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Runtime string `json:"runtime"` // python3 (default) | nodejs
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	rt := req.Runtime
	if rt == "" {
		rt = "python3"
	}
	if _, ok := phase3.SupportedRuntimes[rt]; !ok {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "runtime must be python3 or nodejs")
		return
	}
	e, err := st.P3Create(r.Context(), "sagemaker_notebooks", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"runtime": rt, "status": "READY", "owner_id": u.ID,
		"executions":   []any{},
		"sandbox_note": "Cells run in the same sandboxed subprocess as Lambda (timeout, output cap, memory cap, uid 65534) — not a Jupyter server.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleSMListNotebooks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "sagemaker_notebooks", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"notebooks": p3ListView(list), "count": len(list)})
}

func (s *Server) handleSMGetNotebook(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "sagemaker_notebooks", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleSMDeleteNotebook(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "sagemaker_notebooks", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleSMExecuteCell runs one notebook cell as a real sandboxed
// Python/Node program (the Lambda runner) and records the real
// output, exit status and duration. Nothing is canned.
func (s *Server) handleSMExecuteCell(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	nb, ok := s.p6GetOwned(w, r, "sagemaker_notebooks", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}
	rt := phase3.Str(nb.Data, "runtime", "python3")
	res := phase3.RunLambda(r.Context(), rt, req.Code, nil, map[string]any{}, 30, 256)
	execRec := map[string]any{
		"code": req.Code, "output": res.Output, "error": res.Error,
		"status":    map[bool]string{true: "error", false: "ok"}[res.Error != "" || res.TimedOut],
		"timed_out": res.TimedOut, "duration_ms": res.DurationMs,
		"sandbox": res.Sandbox, "executed_at": p3Now(),
	}
	execs, _ := nb.Data["executions"].([]any)
	execs = append([]any{execRec}, execs...)
	if len(execs) > 20 {
		execs = execs[:20]
	}
	nb.Data["executions"] = execs
	_, _ = st.P3Update(r.Context(), "sagemaker_notebooks", nb)
	s.writeJSON(w, r, http.StatusOK, execRec)
}

func (s *Server) handleSMCreateTrainingJob(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string      `json:"name"`
		Dataset  string      `json:"dataset"` // builtin key or "custom"
		Rows     [][]float64 `json:"rows"`    // custom dataset: features..., target
		Features []string    `json:"features"`
		Target   string      `json:"target"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	var rows [][]float64
	dataset := req.Dataset
	if dataset == "" {
		dataset = "housing"
	}
	if dataset == "custom" {
		rows = req.Rows
	} else {
		var ok bool
		rows, ok = phase6.BuiltinDatasets[dataset]
		if !ok {
			keys := []string{}
			for k := range phase6.BuiltinDatasets {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("unknown dataset %q (available: %s, or \"custom\" with rows)", dataset, strings.Join(keys, ", ")))
			return
		}
	}
	features := req.Features
	if len(features) == 0 {
		features = []string{}
		for i := 0; i < len(rows[0])-1; i++ {
			features = append(features, fmt.Sprintf("feature_%d", i+1))
		}
	}
	target := req.Target
	if target == "" {
		target = "target"
	}
	e, err := st.P3Create(r.Context(), "sagemaker_jobs", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"dataset": dataset, "features": features, "target": target,
		"status": "TRAINING", "owner_id": u.ID,
		"algorithm":  "linear-regression (ordinary least squares, pure Go — no scikit-learn in this environment)",
		"started_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Training runs in the background; the job row flips to
	// COMPLETED with real metrics (or FAILED with the real error).
	go s.p6RunTrainingJob(e.ID, rows, features, target, dataset)
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) p6RunTrainingJob(jobID string, rows [][]float64, features []string, target, dataset string) {
	ctx := context.Background()
	model, err := phase6.TrainLinear(rows, features, target, dataset)
	job, gerr := s.P3.P3Get(ctx, "sagemaker_jobs", jobID)
	if gerr != nil {
		return
	}
	if err != nil {
		job.Data["status"] = "FAILED"
		job.Data["error"] = err.Error()
		job.Data["completed_at"] = p3Now()
		_, _ = s.P3.P3Update(ctx, "sagemaker_jobs", job)
		return
	}
	job.Data["status"] = "COMPLETED"
	job.Data["mse"] = model.MSE
	job.Data["rmse"] = model.RMSE
	job.Data["r2"] = model.R2
	job.Data["rows_trained"] = model.Rows
	job.Data["weights"] = model.Weights
	job.Data["intercept"] = model.Intercept
	job.Data["completed_at"] = p3Now()
	// Register a versioned model and write the artifact to disk so
	// the endpoint serves the stored weights, not a copy in memory.
	modelName := job.Name + "-model"
	artifactDir := filepath.Join(s.P3WorkdirRoot, "sagemaker", "models")
	_ = os.MkdirAll(artifactDir, 0o755)
	_ = os.Chmod(artifactDir, 0o755)
	artifactPath := filepath.Join(artifactDir, jobID+".json")
	raw, _ := json.Marshal(model)
	_ = os.WriteFile(artifactPath, raw, 0o644)
	me, merr := s.P3.P3Create(ctx, "sagemaker_models", &store.P3Entity{Name: modelName, Data: map[string]any{
		"job_id": jobID, "version": 1, "dataset": dataset,
		"features": features, "target": target,
		"weights": model.Weights, "intercept": model.Intercept,
		"mse": model.MSE, "rmse": model.RMSE, "r2": model.R2,
		"rows_trained": model.Rows, "artifact_path": artifactPath,
		"status": "READY", "owner_id": phase3.Str(job.Data, "owner_id", ""),
	}})
	if merr == nil {
		job.Data["model_id"] = me.ID
	} else {
		job.Data["model_error"] = merr.Error()
	}
	_, _ = s.P3.P3Update(ctx, "sagemaker_jobs", job)
}

func (s *Server) handleSMListTrainingJobs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "sagemaker_jobs", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"training_jobs": p3ListView(list), "count": len(list)})
}

func (s *Server) handleSMGetTrainingJob(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "sagemaker_jobs", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleSMListModels(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "sagemaker_models", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"models": p3ListView(list), "count": len(list)})
}

func (s *Server) handleSMGetModel(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "sagemaker_models", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleSMDeleteModel(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	m, ok := s.p6GetOwned(w, r, "sagemaker_models", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	// Refuse while an endpoint serves this model version.
	all, _ := s.P3.P3List(r.Context(), "sagemaker_endpoints")
	for _, ep := range all {
		if phase3.Str(ep.Data, "model_id", "") == m.ID && phase3.Str(ep.Data, "status", "") == "IN_SERVICE" {
			s.writeError(w, r, http.StatusConflict, "in_use", "an IN_SERVICE endpoint serves this model; delete the endpoint first")
			return
		}
	}
	if path := phase3.Str(m.Data, "artifact_path", ""); path != "" {
		_ = os.Remove(path)
	}
	if err := s.P3.P3Delete(r.Context(), "sagemaker_models", m.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleSMCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		ModelID string `json:"model_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.ModelID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name and model_id are required")
		return
	}
	model, err := st.P3Get(r.Context(), "sagemaker_models", req.ModelID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no model with that id")
		return
	}
	if phase3.Str(model.Data, "status", "") != "READY" {
		s.writeError(w, r, http.StatusConflict, "model_not_ready", "model is not READY")
		return
	}
	// The endpoint is IN_SERVICE only if the artifact file the
	// model points at actually exists on disk.
	if path := phase3.Str(model.Data, "artifact_path", ""); path == "" {
		s.writeError(w, r, http.StatusConflict, "model_not_ready", "model has no artifact file")
		return
	} else if _, err := os.Stat(path); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "artifact_missing", "model artifact file is missing from disk: "+err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "sagemaker_endpoints", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"model_id": model.ID, "model_name": model.Name,
		"status": "IN_SERVICE", "owner_id": u.ID,
		"invocations": 0, "created_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleSMListEndpoints(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "sagemaker_endpoints", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"endpoints": p3ListView(list), "count": len(list)})
}

// handleSMInvokeEndpoint serves a real prediction from the stored
// model artifact read off disk. The response names the model version
// that produced it; a missing artifact is a 503, never a guess.
func (s *Server) handleSMInvokeEndpoint(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	ep, ok := s.p6GetOwned(w, r, "sagemaker_endpoints", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	if phase3.Str(ep.Data, "status", "") != "IN_SERVICE" {
		s.writeError(w, r, http.StatusConflict, "endpoint_not_in_service", "endpoint is not IN_SERVICE")
		return
	}
	var req struct {
		Features []float64 `json:"features"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	model, err := st.P3Get(r.Context(), "sagemaker_models", phase3.Str(ep.Data, "model_id", ""))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "the endpoint's model no longer exists")
		return
	}
	path := phase3.Str(model.Data, "artifact_path", "")
	raw, err := os.ReadFile(path)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "artifact_missing", "model artifact cannot be read from disk: "+err.Error())
		return
	}
	var m phase6.LinearModel
	if err := json.Unmarshal(raw, &m); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "artifact_corrupt", "model artifact cannot be parsed: "+err.Error())
		return
	}
	if len(req.Features) != len(m.Weights) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("model expects %d features, got %d", len(m.Weights), len(req.Features)))
		return
	}
	pred := m.Predict(req.Features)
	ep.Data["invocations"] = phase3.Float(ep.Data, "invocations", 0) + 1
	_, _ = st.P3Update(r.Context(), "sagemaker_endpoints", ep)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"prediction": pred, "model_id": model.ID, "model_name": model.Name,
		"features": req.Features, "target": m.Target, "r2": m.R2,
		"note": "Real prediction from the stored linear model trained by a SageMaker training job — not a simulated result.",
	})
}

func (s *Server) handleSMDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "sagemaker_endpoints", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- Bedrock (SIMULATED playground) ------------------------------------------

var bedrockConceptModels = []map[string]any{
	{"model_id": "anthropic.claude-3-sonnet", "name": "Claude 3 Sonnet (concept)", "provider": "Anthropic", "supported": false, "note": "Concept entry only — no model runs and nothing is sent to any provider."},
	{"model_id": "amazon.titan-text", "name": "Titan Text (concept)", "provider": "Amazon", "supported": false, "note": "Concept entry only — no model runs and nothing is sent to any provider."},
	{"model_id": "meta.llama3-8b", "name": "Llama 3 8B (concept)", "provider": "Meta", "supported": false, "note": "Concept entry only — no model runs and nothing is sent to any provider."},
	{"model_id": "stability.stable-diffusion-xl", "name": "Stable Diffusion XL (concept)", "provider": "Stability AI", "supported": false, "note": "Concept entry only — no model runs and nothing is sent to any provider."},
}

func (s *Server) handleBedrockListModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"models": bedrockConceptModels, "count": len(bedrockConceptModels),
		"fidelity": "SIMULATED",
		"note":     "Concept playground only. No foundation model is hosted, no paid third-party AI API is called, and no response below is a real model output.",
	})
}

func (s *Server) handleBedrockInvoke(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		ModelID string `json:"model_id"`
		Prompt  string `json:"prompt"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	known := false
	for _, m := range bedrockConceptModels {
		if m["model_id"] == req.ModelID {
			known = true
		}
	}
	if !known {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "unknown concept model_id")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "prompt is required")
		return
	}
	out := phase6.BedrockTemplateResponse(req.ModelID, req.Prompt)
	_, err := st.P3Create(r.Context(), "bedrock_invocations", &store.P3Entity{Name: req.ModelID + "-" + p6Sequence(), Data: map[string]any{
		"model_id": req.ModelID, "prompt": req.Prompt, "output": out,
		"fidelity": "SIMULATED", "owner_id": u.ID, "created_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"model_id": req.ModelID, "output": out, "fidelity": "SIMULATED",
		"note": "Template response only — no model ran.",
	})
}

func (s *Server) handleBedrockListInvocations(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "bedrock_invocations", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > 100 {
		list = list[:100]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"invocations": p3ListView(list), "count": len(list), "fidelity": "SIMULATED"})
}

// --- KMS (durable, envelope encryption) ---------------------------------------

func (s *Server) p6KMSUnwrapKey(e *store.P3Entity) ([]byte, error) {
	master, _ := s.p6Master()
	wrapped, ok := e.Data["wrapped_key"].(string)
	if !ok || wrapped == "" {
		return nil, errors.New("key has no wrapped key material")
	}
	return phase6.UnwrapKey(master, wrapped)
}

func (s *Server) handleKMSCreateKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	master, source := s.p6Master()
	raw, err := p6RandBytes(32)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	wrapped, err := phase6.WrapKey(master, raw)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "kms_keys", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"description": req.Description, "status": "ENABLED",
		"wrapped_key": wrapped, "key_version": 1, "version": 1,
		"previous_wrapped_keys": []any{},
		"owner_id":              u.ID, "master_key_source": source,
		"fidelity": "PARTIAL",
		"note":     "Durable local key: AES-256-GCM key material is stored wrapped under a master key derived from CLOUDFORGE_KMS_MASTER_KEY (or the JWT secret). Not AWS KMS, no HSM.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(e)
	delete(view, "wrapped_key")
	delete(view, "previous_wrapped_keys")
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleKMSListKeys(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "kms_keys", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	views := p3ListView(list)
	for _, v := range views {
		delete(v, "wrapped_key")
		delete(v, "previous_wrapped_keys")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"keys": views, "count": len(views)})
}

func (s *Server) handleKMSEncrypt(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	key, ok := s.p6GetOwned(w, r, "kms_keys", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		Plaintext string `json:"plaintext"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Plaintext == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "plaintext is required")
		return
	}
	raw, err := s.p6KMSUnwrapKey(key)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "key_unavailable", err.Error())
		return
	}
	sealed, err := phase6.EncryptValue(raw, req.Plaintext)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"ciphertext": sealed, "key_id": key.ID, "key_version": phase3.Int(key.Data, "key_version", 1)})
}

func (s *Server) handleKMSDecrypt(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	key, ok := s.p6GetOwned(w, r, "kms_keys", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		Ciphertext string `json:"ciphertext"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	raw, err := s.p6KMSUnwrapKey(key)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "key_unavailable", err.Error())
		return
	}
	plain, err := phase6.DecryptValue(raw, req.Ciphertext)
	if err == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"plaintext": plain, "key_id": key.ID})
		return
	}
	// Try previous versions (rotation keeps old material for decrypt).
	master, _ := s.p6Master()
	prev, _ := key.Data["previous_wrapped_keys"].([]any)
	for _, p := range prev {
		ws, _ := p.(string)
		old, err2 := phase6.UnwrapKey(master, ws)
		if err2 != nil {
			continue
		}
		if plain, err2 := phase6.DecryptValue(old, req.Ciphertext); err2 == nil {
			s.writeJSON(w, r, http.StatusOK, map[string]any{"plaintext": plain, "key_id": key.ID, "decrypted_with_previous_version": true})
			return
		}
	}
	s.writeError(w, r, http.StatusBadRequest, "decrypt_failed", "ciphertext cannot be decrypted with this key (wrong key or corrupt ciphertext)")
}

func (s *Server) handleKMSRotate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	key, ok := s.p6GetOwned(w, r, "kms_keys", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	master, _ := s.p6Master()
	raw, err := p6RandBytes(32)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	wrapped, err := phase6.WrapKey(master, raw)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	prev, _ := key.Data["previous_wrapped_keys"].([]any)
	if cur, ok := key.Data["wrapped_key"].(string); ok && cur != "" {
		prev = append(prev, cur)
	}
	key.Data["previous_wrapped_keys"] = prev
	key.Data["wrapped_key"] = wrapped
	key.Data["key_version"] = phase3.Int(key.Data, "key_version", 1) + 1
	key.Data["version"] = key.Data["key_version"] // alias: views expose `version`
	key.Data["rotated_at"] = p3Now()
	if _, err := st.P3Update(r.Context(), "kms_keys", key); err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(key)
	delete(view, "wrapped_key")
	delete(view, "previous_wrapped_keys")
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleKMSDeleteKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := s.p6GetOwned(w, r, "kms_keys", id, u); !ok {
		return
	}
	// A key that still encrypts a secret cannot be deleted.
	all, _ := s.P3.P3List(r.Context(), "secrets")
	for _, sec := range all {
		if phase3.Str(sec.Data, "kms_key_id", "") == id {
			s.writeError(w, r, http.StatusConflict, "in_use", "a secret is encrypted with this key; delete the secret first")
			return
		}
	}
	if err := s.P3.P3Delete(r.Context(), "kms_keys", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- Secrets Manager (encrypted at rest) -------------------------------------

func (s *Server) handleSecretsCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		KMSKeyID string `json:"kms_key_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.Value == "" || req.KMSKeyID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name, value and kms_key_id are required")
		return
	}
	key, err := st.P3Get(r.Context(), "kms_keys", req.KMSKeyID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no KMS key with that id")
		return
	}
	if !s.p3Visible(w, r, key, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	raw, err := s.p6KMSUnwrapKey(key)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "key_unavailable", err.Error())
		return
	}
	// Envelope encryption: a fresh data key encrypts the value; the
	// data key itself is encrypted under the KMS key.
	dataKey, err := p6RandBytes(32)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	wrappedDK, err := phase6.WrapKey(raw, dataKey)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	sealed, err := phase6.EncryptValue(dataKey, req.Value)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "secrets", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"kms_key_id": req.KMSKeyID, "wrapped_data_key": wrappedDK,
		"ciphertext": sealed, "version": 1, "owner_id": u.ID,
		"versions": []any{map[string]any{"version": 1, "created_at": p3Now()}},
		"fidelity": "PARTIAL",
		"note":     "Stored envelope-encrypted at rest (AES-256-GCM value, data key wrapped by the KMS key). Rotation creates a new version; there is no automatic rotation schedule.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(e)
	delete(view, "wrapped_data_key")
	delete(view, "ciphertext")
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleSecretsList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "secrets", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	views := p3ListView(list)
	for _, v := range views {
		delete(v, "wrapped_data_key")
		delete(v, "ciphertext")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"secrets": views, "count": len(views)})
}

// handleSecretsGet decrypts and returns the current value. The value
// is never stored in plaintext anywhere.
func (s *Server) handleSecretsGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	sec, ok := s.p6GetOwned(w, r, "secrets", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	val, err := s.p6DecryptSecret(r.Context(), sec)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "decrypt_failed", err.Error())
		return
	}
	view := p3View(sec)
	delete(view, "wrapped_data_key")
	delete(view, "ciphertext")
	view["value"] = val
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) p6DecryptSecret(ctx context.Context, sec *store.P3Entity) (string, error) {
	key, err := s.P3.P3Get(ctx, "kms_keys", phase3.Str(sec.Data, "kms_key_id", ""))
	if err != nil {
		return "", fmt.Errorf("the secret's KMS key no longer exists")
	}
	raw, err := s.p6KMSUnwrapKey(key)
	if err != nil {
		return "", err
	}
	wrappedDK, _ := sec.Data["wrapped_data_key"].(string)
	dataKey, err := phase6.UnwrapKey(raw, wrappedDK)
	if err != nil {
		return "", errors.New("data key cannot be unwrapped (KMS key was rotated past the secret's version)")
	}
	sealed, _ := sec.Data["ciphertext"].(string)
	return phase6.DecryptValue(dataKey, sealed)
}

func (s *Server) handleSecretsPutValue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	sec, ok := s.p6GetOwned(w, r, "secrets", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Value == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "value is required")
		return
	}
	key, err := st.P3Get(r.Context(), "kms_keys", phase3.Str(sec.Data, "kms_key_id", ""))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "the secret's KMS key no longer exists")
		return
	}
	raw, err := s.p6KMSUnwrapKey(key)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "key_unavailable", err.Error())
		return
	}
	dataKey, err := p6RandBytes(32)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	wrappedDK, err := phase6.WrapKey(raw, dataKey)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	sealed, err := phase6.EncryptValue(dataKey, req.Value)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ver := phase3.Int(sec.Data, "version", 1) + 1
	sec.Data["wrapped_data_key"] = wrappedDK
	sec.Data["ciphertext"] = sealed
	sec.Data["version"] = ver
	versions, _ := sec.Data["versions"].([]any)
	versions = append(versions, map[string]any{"version": ver, "created_at": p3Now()})
	sec.Data["versions"] = versions
	if _, err := st.P3Update(r.Context(), "secrets", sec); err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(sec)
	delete(view, "wrapped_data_key")
	delete(view, "ciphertext")
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleSecretsDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "secrets", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- DMS ---------------------------------------------------------------------

// dmsEndpoint describes one side of a migration: platform (the
// platform Postgres) or an RDS instance database.
type dmsEndpoint struct {
	Kind       string `json:"kind"` // platform | rds
	InstanceID string `json:"instance_id,omitempty"`
}

func (s *Server) handleDMSCreateTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string      `json:"name"`
		Source dmsEndpoint `json:"source"`
		Target dmsEndpoint `json:"target"`
		Tables []string    `json:"tables"` // empty = all public tables
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Source.Kind == "" {
		req.Source.Kind = "platform"
	}
	if req.Target.Kind == "" {
		req.Target.Kind = "platform"
	}
	for _, ep := range []dmsEndpoint{req.Source, req.Target} {
		if ep.Kind != "platform" && ep.Kind != "rds" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "endpoint kind must be platform or rds")
			return
		}
		if ep.Kind == "rds" && ep.InstanceID == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rds endpoints need instance_id")
			return
		}
	}
	srcDB, _, err := s.p6ResolveTargetDB(r.Context(), req.Source.Kind, req.Source.InstanceID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "source: "+err.Error())
		return
	}
	tgtDB, _, err := s.p6ResolveTargetDB(r.Context(), req.Target.Kind, req.Target.InstanceID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "target: "+err.Error())
		return
	}
	if srcDB == tgtDB && req.Source.Kind == req.Target.Kind {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "source and target are the same database")
		return
	}
	for _, t := range req.Tables {
		if err := phase6.ValidateIdentifier(t); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "table "+err.Error())
			return
		}
	}
	srcJSON, _ := json.Marshal(req.Source)
	tgtJSON, _ := json.Marshal(req.Target)
	e, err := st.P3Create(r.Context(), "dms_tasks", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"source": json.RawMessage(srcJSON), "target": json.RawMessage(tgtJSON),
		"source_db": srcDB, "target_db": tgtDB,
		"tables": req.Tables, "status": "RUNNING", "owner_id": u.ID,
		"rows_copied": 0, "tables_copied": 0, "started_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p6RunDMSTask(e.ID, req.Source, req.Target, srcDB, tgtDB, req.Tables)
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) p6DMSLog(ctx context.Context, taskID, level, message string, rows int) {
	_, _ = s.P3.P3Create(ctx, "dms_logs", &store.P3Entity{Name: taskID + "-" + p6Sequence(), Data: map[string]any{
		"task_id": taskID, "level": level, "message": message,
		"rows": rows, "created_at": p3Now(),
	}})
}

// p6RunDMSTask copies tables row by row from source to target with
// real row counts. Tables are created in the target when missing
// (same columns, text-compatible types are NOT assumed — the target
// table is created with the source column types as reported by
// information_schema, quoted).
func (s *Server) p6RunDMSTask(taskID string, src, tgt dmsEndpoint, srcDB, tgtDB string, wantTables []string) {
	ctx := context.Background()
	task, err := s.P3.P3Get(ctx, "dms_tasks", taskID)
	if err != nil {
		return
	}
	fail := func(msg string) {
		task.Data["status"] = "FAILED"
		task.Data["error"] = msg
		task.Data["completed_at"] = p3Now()
		_, _ = s.P3.P3Update(ctx, "dms_tasks", task)
		s.p6DMSLog(ctx, taskID, "ERROR", msg, 0)
	}
	srcPool, err := s.p6PG(ctx, srcDB)
	if err != nil {
		fail("source connect failed: " + err.Error())
		return
	}
	defer srcPool.Close()
	tgtPool, err := s.p6PG(ctx, tgtDB)
	if err != nil {
		fail("target connect failed: " + err.Error())
		return
	}
	defer tgtPool.Close()
	tables := wantTables
	if len(tables) == 0 {
		rows, err := srcPool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name`)
		if err != nil {
			fail("list source tables failed: " + err.Error())
			return
		}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err == nil {
				tables = append(tables, t)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			fail("list source tables failed: " + err.Error())
			return
		}
	}
	totalRows := 0
	for _, table := range tables {
		cols, err := s.p6TableColumns(ctx, srcPool, table)
		if err != nil {
			s.p6DMSLog(ctx, taskID, "ERROR", "table "+table+": "+err.Error(), 0)
			fail("table " + table + ": " + err.Error())
			return
		}
		if len(cols) == 0 {
			s.p6DMSLog(ctx, taskID, "WARN", "table "+table+" has no columns, skipped", 0)
			continue
		}
		if err := s.p6EnsureTargetTable(ctx, tgtPool, table, cols); err != nil {
			s.p6DMSLog(ctx, taskID, "ERROR", "create target table "+table+": "+err.Error(), 0)
			fail("create target table " + table + ": " + err.Error())
			return
		}
		n, err := s.p6CopyTable(ctx, srcPool, tgtPool, table, cols)
		if err != nil {
			s.p6DMSLog(ctx, taskID, "ERROR", "copy table "+table+": "+err.Error(), n)
			fail("copy table " + table + ": " + err.Error())
			return
		}
		totalRows += n
		task.Data["tables_copied"] = phase3.Float(task.Data, "tables_copied", 0) + 1
		task.Data["rows_copied"] = totalRows
		_, _ = s.P3.P3Update(ctx, "dms_tasks", task)
		s.p6DMSLog(ctx, taskID, "INFO", fmt.Sprintf("table %s: %d rows copied", table, n), n)
	}
	task.Data["status"] = "COMPLETED"
	task.Data["rows_copied"] = totalRows
	task.Data["completed_at"] = p3Now()
	_, _ = s.P3.P3Update(ctx, "dms_tasks", task)
	s.p6DMSLog(ctx, taskID, "INFO", fmt.Sprintf("migration complete: %d tables, %d rows", len(tables), totalRows), totalRows)
}

func (s *Server) p6TableColumns(ctx context.Context, pool *pgxpool.Pool, table string) ([]map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT column_name, data_type, udt_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := []map[string]string{}
	for rows.Next() {
		var name, typ, udt string
		if err := rows.Scan(&name, &typ, &udt); err != nil {
			return nil, err
		}
		cols = append(cols, map[string]string{"name": name, "type": typ, "udt": udt})
	}
	return cols, rows.Err()
}

func (s *Server) p6EnsureTargetTable(ctx context.Context, pool *pgxpool.Pool, table string, cols []map[string]string) error {
	defs := []string{}
	for _, c := range cols {
		defs = append(defs, fmt.Sprintf(`%q %s`, c["name"], c["type"]))
	}
	_, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (%s)`, table, strings.Join(defs, ", ")))
	return err
}

// p6CopyTable copies up to 5000 rows per table in batches of 100.
func (s *Server) p6CopyTable(ctx context.Context, src, tgt *pgxpool.Pool, table string, cols []map[string]string) (int, error) {
	names := []string{}
	for _, c := range cols {
		names = append(names, fmt.Sprintf(`%q`, c["name"]))
	}
	rows, err := src.Query(ctx, fmt.Sprintf(`SELECT %s FROM %q LIMIT 5000`, strings.Join(names, ", "), table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0
	batch := [][]any{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		placeholders := []string{}
		args := []any{}
		for i, row := range batch {
			ph := []string{}
			for j := range row {
				args = append(args, row[j])
				ph = append(ph, fmt.Sprintf("$%d", len(args)))
			}
			placeholders = append(placeholders, "("+strings.Join(ph, ", ")+")")
			_ = i
		}
		_, err := tgt.Exec(ctx, fmt.Sprintf(`INSERT INTO %q (%s) VALUES %s`, table, strings.Join(names, ", "), strings.Join(placeholders, ", ")), args...)
		if err != nil {
			return err
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return total, err
		}
		batch = append(batch, vals)
		if len(batch) >= 100 {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

func (s *Server) handleDMSListTasks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "dms_tasks", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tasks": p3ListView(list), "count": len(list)})
}

func (s *Server) handleDMSGetTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "dms_tasks", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleDMSListLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	if _, ok := s.p6GetOwned(w, r, "dms_tasks", chi.URLParam(r, "id"), u); !ok {
		return
	}
	all, err := st.P3List(r.Context(), "dms_logs")
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "task_id", "") == chi.URLParam(r, "id") {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"logs": p3ListView(out), "count": len(out)})
}

func (s *Server) handleDMSDeleteTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "dms_tasks", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- DataSync ---------------------------------------------------------------

func (s *Server) handleSyncCreateTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Source      string `json:"source"`      // s3:<bucket>[/prefix] | local:<relative-path>
		Destination string `json:"destination"` // s3:<bucket>[/prefix] | local:<relative-path>
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	for _, loc := range []string{req.Source, req.Destination} {
		if !strings.HasPrefix(loc, "s3:") && !strings.HasPrefix(loc, "local:") {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "locations must be s3:<bucket>[/prefix] or local:<path-under-workdir>")
			return
		}
	}
	if req.Source == req.Destination {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "source and destination are the same location")
		return
	}
	if strings.HasPrefix(req.Source, "local:") || strings.HasPrefix(req.Destination, "local:") {
		for _, loc := range []string{req.Source, req.Destination} {
			if strings.HasPrefix(loc, "local:") {
				if _, err := s.p6LocalPath(strings.TrimPrefix(loc, "local:")); err != nil {
					s.writeError(w, r, http.StatusBadRequest, "invalid_request", "local path: "+err.Error())
					return
				}
			}
		}
	}
	e, err := st.P3Create(r.Context(), "datasync_tasks", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"source": req.Source, "destination": req.Destination,
		"status": "RUNNING", "owner_id": u.ID,
		"files_copied": 0, "bytes_copied": 0, "files_verified": 0, "started_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p6RunSyncTask(e.ID, req.Source, req.Destination)
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

// p6LocalPath resolves a DataSync local location under the workdir
// root and refuses escapes (no ".." out of the root, no absolute
// paths outside it).
func (s *Server) p6LocalPath(rel string) (string, error) {
	root := s.P3WorkdirRoot
	if root == "" {
		root = os.TempDir()
	}
	// Reject parent-directory segments outright: Clean() would
	// silently neutralise them, and a caller probing for an escape
	// should get a refusal, not a remapped path.
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q contains a parent-directory segment", rel)
		}
	}
	clean := filepath.Clean("/" + strings.TrimPrefix(rel, "/"))
	joined := filepath.Join(root, clean)
	if !strings.HasPrefix(joined, filepath.Clean(root)+string(os.PathSeparator)) && joined != filepath.Clean(root) {
		return "", fmt.Errorf("path %q escapes the workdir root", rel)
	}
	return joined, nil
}

func (s *Server) p6SyncLog(ctx context.Context, taskID, level, message string, files int) {
	_, _ = s.P3.P3Create(ctx, "datasync_logs", &store.P3Entity{Name: taskID + "-" + p6Sequence(), Data: map[string]any{
		"task_id": taskID, "level": level, "message": message,
		"files": files, "created_at": p3Now(),
	}})
}

func (s *Server) p6RunSyncTask(taskID, source, dest string) {
	ctx := context.Background()
	task, err := s.P3.P3Get(ctx, "datasync_tasks", taskID)
	if err != nil {
		return
	}
	fail := func(msg string) {
		task.Data["status"] = "FAILED"
		task.Data["error"] = msg
		task.Data["completed_at"] = p3Now()
		_, _ = s.P3.P3Update(ctx, "datasync_tasks", task)
		s.p6SyncLog(ctx, taskID, "ERROR", msg, 0)
	}
	files, bytes, verified, err := s.p6SyncCopy(ctx, source, dest)
	if err != nil {
		task.Data["files_copied"] = files
		task.Data["bytes_copied"] = bytes
		task.Data["files_verified"] = verified
		fail(err.Error())
		return
	}
	task.Data["status"] = "COMPLETED"
	task.Data["files_copied"] = files
	task.Data["bytes_copied"] = bytes
	task.Data["files_verified"] = verified
	task.Data["completed_at"] = p3Now()
	_, _ = s.P3.P3Update(ctx, "datasync_tasks", task)
	s.p6SyncLog(ctx, taskID, "INFO", fmt.Sprintf("transfer complete: %d files, %d bytes, %d SHA-256 verified", files, bytes, verified), files)
}

// p6SyncCopy performs the actual copy. Local<->local copies files
// with SHA-256 verification; S3<->S3 copies objects through the
// MinIO adapter (honest error when MinIO is down); mixed local/S3
// copies go through the adapter too.
func (s *Server) p6SyncCopy(ctx context.Context, source, dest string) (files, bytes, verified int, err error) {
	srcS3, srcBucket, srcPrefix := p6ParseS3Loc(source)
	dstS3, dstBucket, dstPrefix := p6ParseS3Loc(dest)
	if !srcS3 && !dstS3 {
		srcPath, err := s.p6LocalPath(strings.TrimPrefix(source, "local:"))
		if err != nil {
			return 0, 0, 0, err
		}
		dstPath, err := s.p6LocalPath(strings.TrimPrefix(dest, "local:"))
		if err != nil {
			return 0, 0, 0, err
		}
		return p6CopyLocalTree(srcPath, dstPath)
	}
	if s.AWS == nil || s.AWS.S3 == nil {
		return 0, 0, 0, errors.New("S3 adapter is not configured (no MinIO endpoint)")
	}
	if srcS3 && dstS3 {
		objs, err := s.AWS.S3.ListObjects(ctx, srcBucket, srcPrefix)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("list source bucket: %w", err)
		}
		for _, o := range objs {
			data, err := s.AWS.S3.GetObject(ctx, srcBucket, o.Key)
			if err != nil {
				return files, bytes, verified, fmt.Errorf("read %s: %w", o.Key, err)
			}
			rel := strings.TrimPrefix(o.Key, srcPrefix)
			dstKey := dstPrefix + rel
			if err := s.AWS.S3.PutObject(ctx, dstBucket, dstKey, data); err != nil {
				return files, bytes, verified, fmt.Errorf("write %s: %w", dstKey, err)
			}
			back, err := s.AWS.S3.GetObject(ctx, dstBucket, dstKey)
			if err != nil {
				return files, bytes, verified, fmt.Errorf("verify %s: %w", dstKey, err)
			}
			if p6SHA256(back) != p6SHA256(data) {
				return files, bytes, verified, fmt.Errorf("SHA-256 mismatch on %s", dstKey)
			}
			files++
			bytes += len(data)
			verified++
		}
		return files, bytes, verified, nil
	}
	// Mixed local <-> S3.
	if srcS3 {
		objs, err := s.AWS.S3.ListObjects(ctx, srcBucket, srcPrefix)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("list source bucket: %w", err)
		}
		dstPath, err := s.p6LocalPath(strings.TrimPrefix(dest, "local:"))
		if err != nil {
			return 0, 0, 0, err
		}
		for _, o := range objs {
			data, err := s.AWS.S3.GetObject(ctx, srcBucket, o.Key)
			if err != nil {
				return files, bytes, verified, err
			}
			rel := strings.TrimPrefix(o.Key, srcPrefix)
			target := filepath.Join(dstPath, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return files, bytes, verified, err
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return files, bytes, verified, err
			}
			back, err := os.ReadFile(target)
			if err != nil || p6SHA256(back) != p6SHA256(data) {
				return files, bytes, verified, fmt.Errorf("SHA-256 mismatch on %s", target)
			}
			files++
			bytes += len(data)
			verified++
		}
		return files, bytes, verified, nil
	}
	srcPath, err := s.p6LocalPath(strings.TrimPrefix(source, "local:"))
	if err != nil {
		return 0, 0, 0, err
	}
	entries, err := p6ListLocalFiles(srcPath)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, f := range entries {
		data, err := os.ReadFile(f)
		if err != nil {
			return files, bytes, verified, err
		}
		rel, _ := filepath.Rel(srcPath, f)
		dstKey := dstPrefix + filepath.ToSlash(rel)
		if err := s.AWS.S3.PutObject(ctx, dstBucket, dstKey, data); err != nil {
			return files, bytes, verified, err
		}
		back, err := s.AWS.S3.GetObject(ctx, dstBucket, dstKey)
		if err != nil {
			return files, bytes, verified, fmt.Errorf("verify %s: %w", dstKey, err)
		}
		if p6SHA256(back) != p6SHA256(data) {
			return files, bytes, verified, fmt.Errorf("SHA-256 mismatch on %s", dstKey)
		}
		files++
		bytes += len(data)
		verified++
	}
	return files, bytes, verified, nil
}

func p6ParseS3Loc(loc string) (isS3 bool, bucket, prefix string) {
	if !strings.HasPrefix(loc, "s3:") {
		return false, "", ""
	}
	rest := strings.TrimPrefix(loc, "s3:")
	parts := strings.SplitN(rest, "/", 2)
	bucket = parts[0]
	if len(parts) == 2 {
		prefix = parts[1]
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}
	return true, bucket, prefix
}

func p6ListLocalFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}

func p6CopyLocalTree(src, dst string) (files, bytes, verified int, err error) {
	info, err := os.Stat(src)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("source not found: %w", err)
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, 0, 0, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return 0, 0, 0, err
		}
		back, err := os.ReadFile(dst)
		if err != nil || p6SHA256(back) != p6SHA256(data) {
			return 0, 0, 0, errors.New("SHA-256 mismatch on copied file")
		}
		return 1, len(data), 1, nil
	}
	entries, err := p6ListLocalFiles(src)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, f := range entries {
		rel, _ := filepath.Rel(src, f)
		data, err := os.ReadFile(f)
		if err != nil {
			return files, bytes, verified, err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return files, bytes, verified, err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return files, bytes, verified, err
		}
		back, err := os.ReadFile(target)
		if err != nil || p6SHA256(back) != p6SHA256(data) {
			return files, bytes, verified, fmt.Errorf("SHA-256 mismatch on %s", target)
		}
		files++
		bytes += len(data)
		verified++
	}
	return files, bytes, verified, nil
}

func (s *Server) handleSyncListTasks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "datasync_tasks", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tasks": p3ListView(list), "count": len(list)})
}

func (s *Server) handleSyncGetTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "datasync_tasks", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleSyncListLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	if _, ok := s.p6GetOwned(w, r, "datasync_tasks", chi.URLParam(r, "id"), u); !ok {
		return
	}
	all, err := st.P3List(r.Context(), "datasync_logs")
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "task_id", "") == chi.URLParam(r, "id") {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"logs": p3ListView(out), "count": len(out)})
}

func (s *Server) handleSyncDeleteTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "datasync_tasks", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- Transfer Family (UNSUPPORTED, with the reason) --------------------------

func (s *Server) handleTransferInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	_, sshdErr := exec.LookPath("sshd")
	_, sftpErr := exec.LookPath("sftp-server")
	checked := map[string]any{
		"sshd":        sshdErr == nil,
		"sftp-server": sftpErr == nil,
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"service": "transfer", "fidelity": "UNSUPPORTED", "supported": false,
		"decision":         "Transfer Family is UNSUPPORTED: a managed SFTP endpoint needs a real local SFTP server, and this host has no sshd/sftp-server binary to run one against.",
		"reason":           "No local SFTP server binary found (checked sshd and sftp-server on PATH). Serving SFTP from the Go API itself would be a new protocol server, not a teaching surface, and is out of scope for Phase 6.",
		"checked_binaries": checked,
		"alternative":      "Use DataSync (local/S3 copies with SHA-256 verification) for file transfer in CloudForge.",
	})
}

// --- Migration Hub (aggregation, no own table) --------------------------------

func (s *Server) handleMigrationHubTasks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	out := []map[string]any{}
	dms, _ := s.p6List(r.Context(), "dms_tasks", u)
	for _, t := range dms {
		v := p3View(t)
		v["kind"] = "dms"
		out = append(out, v)
	}
	syncs, _ := s.p6List(r.Context(), "datasync_tasks", u)
	for _, t := range syncs {
		v := p3View(t)
		v["kind"] = "datasync"
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ci, _ := out[i]["created_at"].(string)
		cj, _ := out[j]["created_at"].(string)
		return ci > cj
	})
	summary := map[string]int{"total": len(out)}
	for _, v := range out {
		summary[fmt.Sprintf("%v", v["status"])]++
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"tasks": out, "count": len(out), "summary": summary,
		"note": "Migration Hub tracks the real DMS and DataSync tasks above — it creates nothing of its own.",
	})
}

// --- GuardDuty -----------------------------------------------------------------

func (s *Server) handleGDCreateDetector(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "default"
	}
	e, err := st.P3Create(r.Context(), "findings", &store.P3Entity{Name: "detector:" + req.Name, Data: map[string]any{
		"source": "guardduty-detector", "title": "detector:" + req.Name,
		"severity": "INFO", "status": "ACTIVE", "resource_type": "detector",
		"detail":  "GuardDuty-style detector over the platform audit log (failed logins).",
		"enabled": req.Enabled, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Detectors live in the findings table with source
	// guardduty-detector so Security Hub never counts them as
	// findings; the detector id is the entity id.
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleGDListDetectors(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	all, err := s.p6List(r.Context(), "findings", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "source", "") == "guardduty-detector" {
			out = append(out, e)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"detectors": p3ListView(out), "count": len(out)})
}

// handleGDScan runs the detectors over the real audit log (last 24h
// of auth.login_failed entries) and creates one finding per offending
// IP. A scan that finds nothing creates nothing — findings are never
// invented.
func (s *Server) handleGDScan(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if s.Audit == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "audit_unavailable", "audit store is not configured")
		return
	}
	entries, _, err := s.Audit.ListFiltered(r.Context(), store.AuditFilter{
		Action: "auth.login_failed", Limit: 1000,
		From: time.Now().UTC().Add(-24 * time.Hour),
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	byIP := map[string][]*store.AuditEntry{}
	for _, e := range entries {
		ip, _ := e.Metadata["client_ip"].(string)
		if ip == "" {
			continue
		}
		byIP[ip] = append(byIP[ip], e)
	}
	created := 0
	details := []map[string]any{}
	for ip, list := range byIP {
		emails := map[string]bool{}
		for _, e := range list {
			if em, ok := e.Metadata["email"].(string); ok && em != "" {
				emails[em] = true
			}
		}
		var title, severity, detail string
		switch {
		case len(list) >= 5 && len(emails) >= 3:
			title = "Credential stuffing from " + ip
			severity = "HIGH"
			detail = fmt.Sprintf("%d failed logins for %d distinct accounts from %s in the last 24h (audit log).", len(list), len(emails), ip)
		case len(list) >= 5:
			title = "Brute-force logins from " + ip
			severity = "MEDIUM"
			detail = fmt.Sprintf("%d failed logins from %s in the last 24h (audit log).", len(list), ip)
		default:
			continue
		}
		// Deduplicate: an open finding with the same title stays open.
		existing, _ := s.p6List(r.Context(), "findings", u)
		dup := false
		for _, f := range existing {
			if phase3.Str(f.Data, "source", "") == "guardduty" && phase3.Str(f.Data, "title", "") == title && phase3.Str(f.Data, "status", "") != "RESOLVED" && phase3.Str(f.Data, "status", "") != "SUPPRESSED" {
				dup = true
			}
		}
		if dup {
			details = append(details, map[string]any{"title": title, "status": "already_open"})
			continue
		}
		if _, err := s.p6CreateFinding(r.Context(), u.ID, "guardduty", title, severity, "ip", ip, detail); err == nil {
			created++
			details = append(details, map[string]any{"title": title, "severity": severity, "status": "created"})
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"findings_created": created, "failed_logins_scanned": len(entries),
		"details": details,
		"note":    "Findings come only from the real audit log (auth.login_failed entries). No finding is created without matching log evidence.",
	})
}

func (s *Server) handleGDListFindings(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "findings", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range list {
		if phase3.Str(e.Data, "source", "") == "guardduty" {
			out = append(out, e)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"findings": p3ListView(out), "count": len(out)})
}

// handleFindingStatus moves any finding (GuardDuty / Inspector /
// Config / Advisor) through NEW -> ACKNOWLEDGED -> RESOLVED, or to
// SUPPRESSED. Security Hub reads the same rows.
func (s *Server) handleFindingStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "NEW", "ACKNOWLEDGED", "RESOLVED", "SUPPRESSED":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "status must be NEW, ACKNOWLEDGED, RESOLVED or SUPPRESSED")
		return
	}
	f, ok := s.p6GetOwned(w, r, "findings", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	f.Data["status"] = req.Status
	f.Data["status_updated_at"] = p3Now()
	if _, err := st.P3Update(r.Context(), "findings", f); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(f))
}

// --- Security Hub (aggregation over the shared findings table) ----------------

func (s *Server) handleSecHubSummary(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "findings", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	bySeverity := map[string]int{}
	bySource := map[string]int{}
	byStatus := map[string]int{}
	open := 0
	for _, e := range list {
		src := phase3.Str(e.Data, "source", "")
		if src == "guardduty-detector" {
			continue
		}
		bySeverity[phase3.Str(e.Data, "severity", "LOW")]++
		bySource[src]++
		st := phase3.Str(e.Data, "status", "NEW")
		byStatus[st]++
		if st == "NEW" || st == "ACKNOWLEDGED" {
			open++
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"open_findings": open, "by_severity": bySeverity,
		"by_source": bySource, "by_status": byStatus,
		"note": "Aggregation of the real GuardDuty / Inspector / Config / Trusted Advisor findings — nothing is counted that was not created by one of those services.",
	})
}

func (s *Server) handleSecHubFindings(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "findings", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range list {
		if phase3.Str(e.Data, "source", "") != "guardduty-detector" {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"findings": p3ListView(out), "count": len(out)})
}

// --- Inspector ---------------------------------------------------------------

func (s *Server) handleInspectorRunAssessment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  string `json:"name"`
		LabID string `json:"lab_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "assessment-" + p6Sequence()
	}
	assessment, err := st.P3Create(r.Context(), "inspector_assessments", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"lab_id": req.LabID, "status": "RUNNING", "owner_id": u.ID,
		"findings": 0, "started_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p6RunInspector(assessment.ID, req.LabID, u.ID)
	s.writeJSON(w, r, http.StatusCreated, p3View(assessment))
}

// p6RunInspector runs three real checks against real resource state:
// open security groups, public S3 buckets, and unencrypted lab
// instances (no encryption attribute recorded). Every finding cites
// the resource it came from.
func (s *Server) p6RunInspector(assessmentID, labID, ownerID string) {
	ctx := context.Background()
	assess, err := s.P3.P3Get(ctx, "inspector_assessments", assessmentID)
	if err != nil {
		return
	}
	finish := func(n int) {
		assess.Data["status"] = "COMPLETED"
		assess.Data["findings"] = n
		assess.Data["completed_at"] = p3Now()
		_, _ = s.P3.P3Update(ctx, "inspector_assessments", assess)
	}
	found := 0
	// 1. Open security groups (lab resources, kind security-group).
	if s.Resources != nil {
		resources, err := s.Resources.ListResources(ctx, labID)
		if err == nil {
			for _, res := range resources {
				if res.Kind != "security-group" {
					continue
				}
				rules, _ := res.Config["rules"].([]any)
				// Config may decode rules as []map[string]any after JSON round-trip.
				for _, raw := range rules {
					rm, _ := raw.(map[string]any)
					if rm == nil {
						continue
					}
					cidr, _ := rm["cidr"].(string)
					fromPort := rm["from_port"]
					if cidr == "0.0.0.0/0" {
						port := fmt.Sprintf("%v", fromPort)
						sev := "MEDIUM"
						if port == "22" || port == "3389" {
							sev = "HIGH"
						}
						if _, err := s.p6CreateFinding(ctx, ownerID, "inspector",
							fmt.Sprintf("Security group %s allows %s from 0.0.0.0/0", res.Name, port),
							sev, "security-group", res.ID,
							fmt.Sprintf("Lab resource %s (%s) has an ingress rule from 0.0.0.0/0 on port %s.", res.Name, res.ID, port)); err == nil {
							found++
						}
					}
				}
			}
			// 3. Unencrypted lab instances.
			for _, res := range resources {
				if res.Kind != "instance" {
					continue
				}
				enc, _ := res.Config["encrypted"].(bool)
				if !enc {
					if _, err := s.p6CreateFinding(ctx, ownerID, "inspector",
						fmt.Sprintf("Instance %s has no recorded encryption", res.Name),
						"LOW", "instance", res.ID,
						"Lab resource "+res.Name+" ("+res.ID+") records no encryption attribute; CloudForge lab instances are not encrypted at rest."); err == nil {
						found++
					}
				}
			}
		}
	}
	// 2. Public S3 buckets (bucket policy granting *). MinIO down
	// means this check is skipped and recorded — not invented.
	if s.AWS != nil && s.AWS.S3 != nil {
		if buckets, err := s.AWS.S3.ListBuckets(ctx); err == nil {
			for _, b := range buckets {
				pol, err := s.AWS.S3.GetBucketPolicy(ctx, b.Name)
				if err != nil || pol == "" {
					continue
				}
				if strings.Contains(pol, `"*"`) && strings.Contains(pol, "Allow") {
					if _, err := s.p6CreateFinding(ctx, ownerID, "inspector",
						fmt.Sprintf("Bucket %s has a public policy", b.Name),
						"HIGH", "s3-bucket", b.Name,
						"Bucket policy for "+b.Name+" contains an Allow for principal \"*\"."); err == nil {
						found++
					}
				}
			}
		} else {
			assess.Data["s3_check"] = "skipped: " + err.Error()
		}
	} else {
		assess.Data["s3_check"] = "skipped: S3 adapter not configured"
	}
	finish(found)
}

func (s *Server) handleInspectorListAssessments(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "inspector_assessments", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"assessments": p3ListView(list), "count": len(list)})
}

func (s *Server) handleInspectorGetAssessment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "inspector_assessments", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	view := p3View(e)
	// Findings created by this assessment carry its id in the detail.
	all, _ := s.p6List(r.Context(), "findings", u)
	mine := []*store.P3Entity{}
	for _, f := range all {
		if phase3.Str(f.Data, "source", "") == "inspector" {
			mine = append(mine, f)
		}
	}
	view["findings_list"] = p3ListView(mine)
	s.writeJSON(w, r, http.StatusOK, view)
}

// --- Config -------------------------------------------------------------------

func (s *Server) handleConfigCreateRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		RuleType string `json:"rule_type"` // s3-bucket-versioning-enabled | sg-no-open-ssh | sg-no-open-ingress
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	switch req.RuleType {
	case "s3-bucket-versioning-enabled", "sg-no-open-ssh", "sg-no-open-ingress":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rule_type must be s3-bucket-versioning-enabled, sg-no-open-ssh or sg-no-open-ingress")
		return
	}
	if req.Name == "" {
		req.Name = req.RuleType
	}
	e, err := st.P3Create(r.Context(), "config_rules", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"rule_type": req.RuleType, "status": "ACTIVE", "owner_id": u.ID,
		"last_evaluation": nil, "compliant": nil,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleConfigListRules(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "config_rules", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"rules": p3ListView(list), "count": len(list)})
}

func (s *Server) handleConfigDeleteRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "config_rules", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleConfigEvaluateRule evaluates one rule against the real
// resource state and stores one result per non-compliant resource
// plus a Config finding for each.
func (s *Server) handleConfigEvaluateRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	rule, ok := s.p6GetOwned(w, r, "config_rules", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	ruleType := phase3.Str(rule.Data, "rule_type", "")
	nonCompliant := []map[string]any{}
	evaluated := 0
	switch ruleType {
	case "s3-bucket-versioning-enabled":
		if s.AWS == nil || s.AWS.S3 == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "s3_unavailable", "S3 adapter is not configured (no MinIO endpoint) — the rule cannot be evaluated")
			return
		}
		buckets, err := s.AWS.S3.ListBuckets(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "s3_error", err.Error())
			return
		}
		for _, b := range buckets {
			evaluated++
			enabled, err := s.AWS.S3.GetBucketVersioning(r.Context(), b.Name)
			if err != nil || !enabled {
				nonCompliant = append(nonCompliant, map[string]any{"resource_type": "s3-bucket", "resource_id": b.Name, "reason": "versioning is not enabled"})
			}
		}
	case "sg-no-open-ssh", "sg-no-open-ingress":
		if s.Labs == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "labs_unavailable", "lab store is not configured")
			return
		}
		labs, err := s.Labs.ListByOwner(r.Context(), u.ID)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		for _, lab := range labs {
			resources, err := s.Resources.ListResources(r.Context(), lab.ID)
			if err != nil {
				continue
			}
			for _, res := range resources {
				if res.Kind != "security-group" {
					continue
				}
				rules, _ := res.Config["rules"].([]any)
				for _, raw := range rules {
					rm, _ := raw.(map[string]any)
					if rm == nil {
						continue
					}
					cidr, _ := rm["cidr"].(string)
					port := fmt.Sprintf("%v", rm["from_port"])
					if cidr != "0.0.0.0/0" {
						continue
					}
					if ruleType == "sg-no-open-ssh" && port != "22" {
						continue
					}
					evaluated++
					nonCompliant = append(nonCompliant, map[string]any{"resource_type": "security-group", "resource_id": res.ID, "reason": fmt.Sprintf("ingress from 0.0.0.0/0 on port %s (lab %s)", port, lab.ID)})
				}
			}
		}
	}
	compliant := len(nonCompliant) == 0
	now := p3Now()
	for _, nc := range nonCompliant {
		_, _ = st.P3Create(r.Context(), "config_results", &store.P3Entity{Name: rule.Name + "-" + p6Sequence(), Data: map[string]any{
			"rule_id": rule.ID, "rule_name": rule.Name,
			"resource_type": nc["resource_type"], "resource_id": nc["resource_id"],
			"compliance": "NON_COMPLIANT", "reason": nc["reason"],
			"evaluated_at": now, "owner_id": u.ID,
		}})
		_, _ = s.p6CreateFinding(r.Context(), u.ID, "config",
			fmt.Sprintf("Config rule %s: %s is non-compliant", rule.Name, nc["resource_id"]),
			"MEDIUM", fmt.Sprintf("%v", nc["resource_type"]), fmt.Sprintf("%v", nc["resource_id"]),
			fmt.Sprintf("%v", nc["reason"]))
	}
	rule.Data["last_evaluation"] = now
	rule.Data["compliant"] = compliant
	rule.Data["resources_evaluated"] = evaluated
	rule.Data["non_compliant_count"] = len(nonCompliant)
	_, _ = st.P3Update(r.Context(), "config_rules", rule)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"rule": p3View(rule), "compliant": compliant,
		"resources_evaluated": evaluated,
		"non_compliant":       nonCompliant,
	})
}

func (s *Server) handleConfigListResults(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "config_results", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > 200 {
		list = list[:200]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"results": p3ListView(list), "count": len(list)})
}

// handleConfigSnapshot records the current configuration of one
// lab's resources (or all the caller's labs) as config items — the
// recorded history Config evaluates against later.
func (s *Server) handleConfigSnapshot(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		LabID string `json:"lab_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	labs := []string{}
	if req.LabID != "" {
		labs = []string{req.LabID}
	} else if s.Labs != nil {
		all, err := s.Labs.ListByOwner(r.Context(), u.ID)
		if err != nil {
			s.p3Err(w, r, err)
			return
		}
		for _, l := range all {
			labs = append(labs, l.ID)
		}
	}
	recorded := 0
	for _, labID := range labs {
		resources, err := s.Resources.ListResources(r.Context(), labID)
		if err != nil {
			continue
		}
		for _, res := range resources {
			cfgJSON, _ := json.Marshal(res.Config)
			_, err := st.P3Create(r.Context(), "config_items", &store.P3Entity{Name: res.ID + "-" + p6Sequence(), Data: map[string]any{
				"resource_id": res.ID, "resource_type": res.Kind, "lab_id": labID,
				"name": res.Name, "state": res.State,
				"configuration": string(cfgJSON), "owner_id": u.ID,
				"recorded_at": p3Now(),
			}})
			if err == nil {
				recorded++
			}
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"recorded": recorded, "labs": len(labs)})
}

func (s *Server) handleConfigListItems(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "config_items", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > 200 {
		list = list[:200]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"items": p3ListView(list), "count": len(list)})
}

// --- WAF ---------------------------------------------------------------------

func (s *Server) handleWAFCreateACL(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  string           `json:"name"`
		Rules []phase6.WAFRule `json:"rules"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	for _, ru := range req.Rules {
		if ru.Action != "allow" && ru.Action != "block" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "rule action must be allow or block")
			return
		}
		switch ru.MatchType {
		case "path_prefix", "path_contains", "header_equals", "query_contains", "body_contains", "ip_prefix":
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "unknown match_type "+ru.MatchType)
			return
		}
	}
	rulesJSON, _ := json.Marshal(req.Rules)
	e, err := st.P3Create(r.Context(), "waf_acls", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"rules": json.RawMessage(rulesJSON), "status": "ACTIVE",
		"evaluations": 0, "owner_id": u.ID,
		"fidelity": "SIMULATED",
		"note":     "Rules are evaluated by the /evaluate endpoint against a request you supply. They are NOT inline in the API request path — a blocked request here is a teaching evaluation, not a live block.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleWAFListACLs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "waf_acls", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"acls": p3ListView(list), "count": len(list)})
}

func (s *Server) handleWAFGetACL(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	e, ok := s.p6GetOwned(w, r, "waf_acls", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleWAFDeleteACL(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "waf_acls", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleWAFEvaluate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	acl, ok := s.p6GetOwned(w, r, "waf_acls", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req phase6.WAFRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		req.Path = "/"
	}
	rules := p6WAFRules(acl)
	action, matched, okMatched := phase6.EvaluateWAF(rules, req)
	acl.Data["evaluations"] = phase3.Float(acl.Data, "evaluations", 0) + 1
	_, _ = st.P3Update(r.Context(), "waf_acls", acl)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"action": action, "matched_rule": matched, "rule_matched": okMatched,
		"request":  map[string]any{"method": req.Method, "path": req.Path, "source_ip": req.SourceIP},
		"fidelity": "SIMULATED",
		"note":     "Evaluation only — this does not block live traffic.",
	})
}

func p6WAFRules(acl *store.P3Entity) []phase6.WAFRule {
	raw, _ := json.Marshal(acl.Data["rules"])
	var rules []phase6.WAFRule
	_ = json.Unmarshal(raw, &rules)
	return rules
}

// --- Advanced networking ------------------------------------------------------

// p6LabResource finds a lab resource the caller can see, by id.
// It scans the caller's labs (all labs for an admin) because the
// resource store is keyed by lab.
func (s *Server) p6LabResource(ctx context.Context, id, ownerID string, isAdmin bool) (*store.LabResource, error) {
	if s.Resources == nil || s.Labs == nil {
		return nil, errors.New("lab stores are not configured")
	}
	var labs []*labs.Lab
	var err error
	if isAdmin {
		labs, err = s.Labs.ListAll(ctx)
	} else {
		labs, err = s.Labs.ListByOwner(ctx, ownerID)
	}
	if err != nil {
		return nil, err
	}
	for _, lab := range labs {
		resources, err := s.Resources.ListResources(ctx, lab.ID)
		if err != nil {
			continue
		}
		for _, res := range resources {
			if res.ID == id {
				return res, nil
			}
		}
	}
	return nil, errors.New("no lab resource with that id")
}

// p6AddVPCRoute appends a route entry to a VPC lab resource's config
// routes and persists it. Peering/TGW/endpoints all record real
// routes in lab state; the engine has no peering/TGW RPC, so this
// state — not an engine call — is what makes the connection real
// inside CloudForge.
func (s *Server) p6AddVPCRoute(ctx context.Context, vpc *store.LabResource, destination, target string) error {
	routes, _ := vpc.Config["routes"].([]any)
	routes = append(routes, map[string]any{
		"destination": destination, "target": target,
		"status": "active", "created_at": p3Now(),
	})
	if vpc.Config == nil {
		vpc.Config = map[string]any{}
	}
	vpc.Config["routes"] = routes
	_, err := s.Resources.UpdateResource(ctx, vpc)
	return err
}

func (s *Server) p6RemoveVPCRoute(ctx context.Context, vpc *store.LabResource, target string) error {
	routes, _ := vpc.Config["routes"].([]any)
	kept := []any{}
	for _, raw := range routes {
		rm, _ := raw.(map[string]any)
		if rm != nil && rm["target"] == target {
			continue
		}
		kept = append(kept, raw)
	}
	if vpc.Config == nil {
		vpc.Config = map[string]any{}
	}
	vpc.Config["routes"] = kept
	_, err := s.Resources.UpdateResource(ctx, vpc)
	return err
}

func p6CIDROverlap(a, b string) bool {
	_, na, errA := net.ParseCIDR(a)
	_, nb, errB := net.ParseCIDR(b)
	if errA != nil || errB != nil {
		return true // unparseable: refuse rather than peer blindly
	}
	return na.Contains(nb.IP) || nb.Contains(na.IP)
}

func (s *Server) handlePeeringCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
		VPCA string `json:"vpc_a_resource_id"`
		VPCB string `json:"vpc_b_resource_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.VPCA == "" || req.VPCB == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name, vpc_a_resource_id and vpc_b_resource_id are required")
		return
	}
	if req.VPCA == req.VPCB {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "a VPC cannot peer with itself")
		return
	}
	vpcA, err := s.p6LabResource(r.Context(), req.VPCA, u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC A: "+err.Error())
		return
	}
	vpcB, err := s.p6LabResource(r.Context(), req.VPCB, u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC B: "+err.Error())
		return
	}
	if vpcA.Kind != "vpc" || vpcB.Kind != "vpc" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "both endpoints must be lab resources of kind vpc")
		return
	}
	if p6CIDROverlap(vpcA.CIDR, vpcB.CIDR) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("VPC CIDRs overlap (%s vs %s) — peering requires non-overlapping CIDRs", vpcA.CIDR, vpcB.CIDR))
		return
	}
	e, err := st.P3Create(r.Context(), "vpc_peerings", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"vpc_a_id": vpcA.ID, "vpc_a_name": vpcA.Name, "vpc_a_cidr": vpcA.CIDR,
		"vpc_b_id": vpcB.ID, "vpc_b_name": vpcB.Name, "vpc_b_cidr": vpcB.CIDR,
		"status": "ACTIVE", "owner_id": u.ID,
		"fidelity": "SIMULATED",
		"note":     "No engine peering RPC exists: the peering is real route-table entries in both VPCs' lab state, not kernel peering.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	target := "pcx-" + e.ID
	if err := s.p6AddVPCRoute(r.Context(), vpcA, vpcB.CIDR, target); err != nil {
		_ = st.P3Delete(r.Context(), "vpc_peerings", e.ID)
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "recording route in VPC A failed: "+err.Error())
		return
	}
	if err := s.p6AddVPCRoute(r.Context(), vpcB, vpcA.CIDR, target); err != nil {
		_ = s.p6RemoveVPCRoute(r.Context(), vpcA, target)
		_ = st.P3Delete(r.Context(), "vpc_peerings", e.ID)
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "recording route in VPC B failed: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handlePeeringList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "vpc_peerings", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"peerings": p3ListView(list), "count": len(list)})
}

func (s *Server) handlePeeringDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	peering, ok := s.p6GetOwned(w, r, "vpc_peerings", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	target := "pcx-" + peering.ID
	if vpcA, err := s.p6LabResource(r.Context(), phase3.Str(peering.Data, "vpc_a_id", ""), u.ID, p3IsAdmin(u.Role)); err == nil {
		_ = s.p6RemoveVPCRoute(r.Context(), vpcA, target)
	}
	if vpcB, err := s.p6LabResource(r.Context(), phase3.Str(peering.Data, "vpc_b_id", ""), u.ID, p3IsAdmin(u.Role)); err == nil {
		_ = s.p6RemoveVPCRoute(r.Context(), vpcB, target)
	}
	if err := s.P3.P3Delete(r.Context(), "vpc_peerings", peering.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleTGWCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	e, err := st.P3Create(r.Context(), "transit_gateways", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"status": "AVAILABLE", "owner_id": u.ID, "attachments": 0,
		"fidelity": "SIMULATED",
		"note":     "No engine transit-gateway RPC exists: attachments are real route-table entries in the attached VPCs' lab state.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleTGWList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "transit_gateways", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"transit_gateways": p3ListView(list), "count": len(list)})
}

func (s *Server) handleTGWAttach(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	tgw, ok := s.p6GetOwned(w, r, "transit_gateways", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		VPCResourceID string `json:"vpc_resource_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	vpc, err := s.p6LabResource(r.Context(), req.VPCResourceID, u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC: "+err.Error())
		return
	}
	if vpc.Kind != "vpc" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "attachment target must be a lab resource of kind vpc")
		return
	}
	att, err := st.P3Create(r.Context(), "tgw_attachments", &store.P3Entity{Name: tgw.Name + "-" + vpc.Name, Data: map[string]any{
		"transit_gateway_id": tgw.ID, "vpc_id": vpc.ID, "vpc_name": vpc.Name,
		"vpc_cidr": vpc.CIDR, "status": "AVAILABLE", "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	target := "tgw-" + tgw.ID
	if err := s.p6AddVPCRoute(r.Context(), vpc, "0.0.0.0/0", target); err != nil {
		_ = st.P3Delete(r.Context(), "tgw_attachments", att.ID)
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "recording route in the VPC failed: "+err.Error())
		return
	}
	tgw.Data["attachments"] = phase3.Float(tgw.Data, "attachments", 0) + 1
	_, _ = st.P3Update(r.Context(), "transit_gateways", tgw)
	s.writeJSON(w, r, http.StatusCreated, p3View(att))
}

func (s *Server) handleTGWDetach(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	att, err := st.P3Get(r.Context(), "tgw_attachments", chi.URLParam(r, "attId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, att, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	if phase3.Str(att.Data, "transit_gateway_id", "") != chi.URLParam(r, "id") {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no such attachment on this transit gateway")
		return
	}
	if vpc, err := s.p6LabResource(r.Context(), phase3.Str(att.Data, "vpc_id", ""), u.ID, p3IsAdmin(u.Role)); err == nil {
		_ = s.p6RemoveVPCRoute(r.Context(), vpc, "tgw-"+chi.URLParam(r, "id"))
	}
	if err := st.P3Delete(r.Context(), "tgw_attachments", att.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	if tgw, err := st.P3Get(r.Context(), "transit_gateways", chi.URLParam(r, "id")); err == nil {
		tgw.Data["attachments"] = maxFloat(0, phase3.Float(tgw.Data, "attachments", 0)-1)
		_, _ = st.P3Update(r.Context(), "transit_gateways", tgw)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (s *Server) handleTGWDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	tgw, ok := s.p6GetOwned(w, r, "transit_gateways", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	if phase3.Float(tgw.Data, "attachments", 0) > 0 {
		s.writeError(w, r, http.StatusConflict, "has_attachments", "detach all VPC attachments before deleting the transit gateway")
		return
	}
	if err := st.P3Delete(r.Context(), "transit_gateways", tgw.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

var p6VPCEndpointServices = map[string]bool{"s3": true, "dynamodb": true, "sqs": true, "sns": true}

func (s *Server) handleVPCEndpointCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name          string `json:"name"`
		VPCResourceID string `json:"vpc_resource_id"`
		Service       string `json:"service"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.VPCResourceID == "" || req.Service == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name, vpc_resource_id and service are required")
		return
	}
	if !p6VPCEndpointServices[req.Service] {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "service must be one of s3, dynamodb, sqs, sns (platform services with a real local endpoint)")
		return
	}
	vpc, err := s.p6LabResource(r.Context(), req.VPCResourceID, u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC: "+err.Error())
		return
	}
	if vpc.Kind != "vpc" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "endpoint target must be a lab resource of kind vpc")
		return
	}
	e, err := st.P3Create(r.Context(), "vpc_endpoints", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"vpc_id": vpc.ID, "vpc_name": vpc.Name, "service": req.Service,
		"status": "AVAILABLE", "owner_id": u.ID,
		"dns_name": req.Service + ".cf-local-1.vpce.local",
		"note":     "Endpoint record routing lab traffic to the platform " + req.Service + " service. The DNS name is a local teaching name, not registered.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if vpc.Config == nil {
		vpc.Config = map[string]any{}
	}
	eps, _ := vpc.Config["endpoints"].([]any)
	eps = append(eps, map[string]any{"endpoint_id": e.ID, "service": req.Service, "status": "AVAILABLE"})
	vpc.Config["endpoints"] = eps
	_, _ = s.Resources.UpdateResource(r.Context(), vpc)
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleVPCEndpointList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "vpc_endpoints", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"endpoints": p3ListView(list), "count": len(list)})
}

func (s *Server) handleVPCEndpointDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	ep, ok := s.p6GetOwned(w, r, "vpc_endpoints", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	if vpc, err := s.p6LabResource(r.Context(), phase3.Str(ep.Data, "vpc_id", ""), u.ID, p3IsAdmin(u.Role)); err == nil && vpc.Config != nil {
		eps, _ := vpc.Config["endpoints"].([]any)
		kept := []any{}
		for _, raw := range eps {
			rm, _ := raw.(map[string]any)
			if rm != nil && rm["endpoint_id"] == ep.ID {
				continue
			}
			kept = append(kept, raw)
		}
		vpc.Config["endpoints"] = kept
		_, _ = s.Resources.UpdateResource(r.Context(), vpc)
	}
	if err := s.P3.P3Delete(r.Context(), "vpc_endpoints", ep.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- Network Firewall ---------------------------------------------------------

func (s *Server) handleNFWCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  string                `json:"name"`
		LabID string                `json:"lab_id"`
		Rules []phase6.FirewallRule `json:"rules"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	for _, ru := range req.Rules {
		if ru.Action != "allow" && ru.Action != "deny" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "firewall rule action must be allow or deny")
			return
		}
	}
	rulesJSON, _ := json.Marshal(req.Rules)
	e, err := st.P3Create(r.Context(), "netfirewalls", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"lab_id": req.LabID, "rules": json.RawMessage(rulesJSON),
		"status": "READY", "owner_id": u.ID,
		"engine_policy_applied": false, "engine_note": "",
		"fidelity": "SIMULATED",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Best-effort engine sync: the engine has CreateFirewallPolicy
	// for a lab network. If the engine is down or the lab has no
	// network, the policy still exists with engine_policy_applied
	// false and the reason recorded — never claimed as applied.
	if req.LabID != "" && s.Network != nil {
		if err := s.p6SyncFirewallToEngine(r.Context(), e.ID, req.LabID, req.Rules); err != nil {
			e.Data["engine_note"] = "engine sync failed: " + err.Error()
			_, _ = st.P3Update(r.Context(), "netfirewalls", e)
		} else {
			e.Data["engine_policy_applied"] = true
			e.Data["engine_note"] = "policy created on the network engine for lab " + req.LabID
			_, _ = st.P3Update(r.Context(), "netfirewalls", e)
		}
	} else if req.LabID == "" {
		e.Data["engine_note"] = "no lab attached — policy is stored only, nothing was applied to the engine"
		_, _ = st.P3Update(r.Context(), "netfirewalls", e)
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

// p6SyncFirewallToEngine maps our rules onto the engine's firewall
// policy RPC. The engine's rule model has one CIDR per rule, so a
// rule with both source and destination CIDRs is sent as its source
// CIDR (documented in the policy note).
func (s *Server) p6SyncFirewallToEngine(ctx context.Context, policyID, labID string, rules []phase6.FirewallRule) error {
	protoRules := []*gen.FirewallRule{}
	for i, ru := range rules {
		pr := &gen.FirewallRule{
			RuleId:    fmt.Sprintf("%s-%d", policyID, i),
			Priority:  int32(ru.Priority),
			Cidr:      ru.SourceCIDR,
			PortStart: int32(p6PortRangeLo(ru.PortRange)),
			PortEnd:   int32(p6PortRangeHi(ru.PortRange)),
		}
		switch strings.ToLower(ru.Protocol) {
		case "tcp":
			pr.Protocol = gen.FirewallProtocol_FIREWALL_PROTOCOL_TCP
		case "udp":
			pr.Protocol = gen.FirewallProtocol_FIREWALL_PROTOCOL_UDP
		case "icmp":
			pr.Protocol = gen.FirewallProtocol_FIREWALL_PROTOCOL_ICMP
		default:
			pr.Protocol = gen.FirewallProtocol_FIREWALL_PROTOCOL_ANY
		}
		if ru.Action == "allow" {
			pr.Action = gen.FirewallAction_FIREWALL_ACTION_ALLOW
		} else {
			pr.Action = gen.FirewallAction_FIREWALL_ACTION_DENY
		}
		pr.Direction = gen.FirewallDirection_FIREWALL_DIRECTION_INGRESS
		protoRules = append(protoRules, pr)
	}
	_, err := s.Network.CreateFirewallPolicy(ctx, &gen.CreateFirewallPolicyRequest{
		PolicyId: policyID, NetworkId: labID, Name: policyID, Rules: protoRules,
	})
	return err
}

func p6PortRangeLo(spec string) int {
	if strings.Contains(spec, "-") {
		var lo, hi int
		fmt.Sscanf(spec, "%d-%d", &lo, &hi)
		return lo
	}
	var p int
	fmt.Sscanf(spec, "%d", &p)
	return p
}

func p6PortRangeHi(spec string) int {
	if strings.Contains(spec, "-") {
		var lo, hi int
		fmt.Sscanf(spec, "%d-%d", &lo, &hi)
		return hi
	}
	var p int
	fmt.Sscanf(spec, "%d", &p)
	return p
}

func (s *Server) handleNFWList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "netfirewalls", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"firewalls": p3ListView(list), "count": len(list)})
}

func (s *Server) handleNFWDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "netfirewalls", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleNFWEvaluate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	fw, ok := s.p6GetOwned(w, r, "netfirewalls", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var pkt phase6.FirewallPacket
	if !s.decodeJSON(w, r, &pkt) {
		return
	}
	raw, _ := json.Marshal(fw.Data["rules"])
	var rules []phase6.FirewallRule
	_ = json.Unmarshal(raw, &rules)
	action, matched := phase6.EvaluateFirewall(rules, pkt)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"action": action, "matched_rule": matched,
		"packet":  map[string]any{"source_ip": pkt.SourceIP, "dest_ip": pkt.DestIP, "dest_port": pkt.DestPort, "protocol": pkt.Protocol},
		"default": "deny (no matching rule)",
	})
}

// --- VPC Flow Logs ---------------------------------------------------------------

func (s *Server) handleFlowLogCreateGroup(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name          string `json:"name"`
		VPCResourceID string `json:"vpc_resource_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.VPCResourceID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name and vpc_resource_id are required")
		return
	}
	vpc, err := s.p6LabResource(r.Context(), req.VPCResourceID, u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC: "+err.Error())
		return
	}
	if vpc.Kind != "vpc" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "flow log target must be a lab resource of kind vpc")
		return
	}
	e, err := st.P3Create(r.Context(), "flowlog_groups", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"vpc_id": vpc.ID, "vpc_name": vpc.Name, "lab_id": vpc.LabID,
		"status": "ACTIVE", "owner_id": u.ID, "records": 0,
		"fidelity": "SIMULATED",
		"note":     "Records are built only from real engine telemetry snapshots taken by /capture — no flows are invented.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleFlowLogListGroups(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "flowlog_groups", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"groups": p3ListView(list), "count": len(list)})
}

func (s *Server) handleFlowLogDeleteGroup(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := s.p6GetOwned(w, r, "flowlog_groups", id, u); !ok {
		return
	}
	recs, _ := st.P3List(r.Context(), "flowlog_records")
	for _, rec := range recs {
		if phase3.Str(rec.Data, "group_id", "") == id {
			_ = st.P3Delete(r.Context(), "flowlog_records", rec.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "flowlog_groups", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleFlowLogCapture snapshots the engine's real telemetry for the
// group's lab and writes one flow-log record per telemetry point.
// Engine down is an honest 503 with zero records written.
func (s *Server) handleFlowLogCapture(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	group, ok := s.p6GetOwned(w, r, "flowlog_groups", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	if s.Network == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "engine_unavailable", "network engine client is not configured")
		return
	}
	// The engine requires RequestMeta on every RPC (lab scoping +
	// idempotency). The empty request this handler used to send was
	// rejected with INVALID_ARGUMENT "meta is required", so capture
	// always 503'd (found live 2026-11-22). The lab is resolved from
	// the group's VPC resource; the idempotency key is fresh per
	// capture so a repeat capture re-collects instead of replaying
	// the previous snapshot.
	vpcRes, err := s.p6LabResource(r.Context(), phase3.Str(group.Data, "vpc_id", ""), u.ID, p3IsAdmin(u.Role))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "VPC: no lab resource with that id")
		return
	}
	resp, err := s.Network.CollectTelemetry(r.Context(), &gen.CollectTelemetryRequest{
		Meta: networkclient.Meta(vpcRes.LabID, u.ID, RequestID(r.Context()), "flowlog-capture:"+group.ID+":"+p6Sequence()),
	})
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "engine_unavailable", "engine telemetry failed: "+err.Error())
		return
	}
	written := 0
	for _, pt := range resp.Points {
		labels := map[string]any{}
		for k, v := range pt.Labels {
			labels[k] = v
		}
		_, err := st.P3Create(r.Context(), "flowlog_records", &store.P3Entity{Name: group.Name + "-" + p6Sequence(), Data: map[string]any{
			"group_id": group.ID, "source": "engine-telemetry",
			"metric": pt.Name, "value": pt.Value,
			"timestamp_unix_seconds": pt.TimestampUnixSeconds,
			"labels":                 labels, "owner_id": u.ID,
			"captured_at": p3Now(),
		}})
		if err == nil {
			written++
		}
	}
	group.Data["records"] = phase3.Float(group.Data, "records", 0) + float64(written)
	group.Data["last_capture"] = p3Now()
	_, _ = st.P3Update(r.Context(), "flowlog_groups", group)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"group": p3View(group), "records_written": written,
		"telemetry_points": len(resp.Points),
		"note":             "Each record is one real engine telemetry point (name/value/labels/timestamp) — no per-packet flows are synthesised.",
	})
}

func (s *Server) handleFlowLogListRecords(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	group, ok := s.p6GetOwned(w, r, "flowlog_groups", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "flowlog_records")
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	out := []*store.P3Entity{}
	for _, e := range all {
		if phase3.Str(e.Data, "group_id", "") == group.ID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 200 {
		out = out[:200]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"records": p3ListView(out), "count": len(out)})
}

// --- Systems Manager ----------------------------------------------------------

func (s *Server) handleSSMInventory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if s.HostAgent == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "host_agent_unavailable", "no host agent is configured (HOST_AGENT_ADDR unset) — inventory needs a host agent")
		return
	}
	inv, err := s.HostAgent.Inventory(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, "host_agent_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"inventory": inv, "source": "host-agent"})
}

// handleSSMInstances lists the real lab instances SSM can target:
// labs plus their instance resources, with live state.
func (s *Server) handleSSMInstances(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if s.Labs == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "labs_unavailable", "lab store is not configured")
		return
	}
	labs, err := s.Labs.ListByOwner(r.Context(), u.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, lab := range labs {
		resources, _ := s.Resources.ListResources(r.Context(), lab.ID)
		instances := 0
		for _, res := range resources {
			if res.Kind == "instance" {
				instances++
				out = append(out, map[string]any{
					"instance_id": res.ID, "lab_id": lab.ID, "name": res.Name,
					"state": res.State, "private_ip": res.PrivateIP,
					"public_ip": res.PublicIP, "kind": "instance",
				})
			}
		}
		if instances == 0 {
			out = append(out, map[string]any{
				"instance_id": lab.ID, "lab_id": lab.ID, "name": lab.DefinitionID,
				"state": lab.State, "kind": "lab",
			})
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"instances": out, "count": len(out)})
}

// handleSSMRunCommand runs an allowlisted command in a lab via the
// host agent and records the real output and exit code. No host
// agent is an honest 503 — the command is never faked.
func (s *Server) handleSSMRunCommand(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		LabID   string   `json:"lab_id"`
		Command []string `json:"command"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.LabID == "" || len(req.Command) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "lab_id and a non-empty command are required")
		return
	}
	if s.HostAgent == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "host_agent_unavailable", "no host agent is configured (HOST_AGENT_ADDR unset) — run command needs a host agent")
		return
	}
	res, err := s.HostAgent.Do(r.Context(), hostagent.OpRequest{
		Op: hostagent.OpExecInLab, LabID: req.LabID, OwnerID: u.ID, Command: req.Command,
	})
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, "host_agent_error", err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "ssm_commands", &store.P3Entity{Name: req.Command[0] + "-" + p6Sequence(), Data: map[string]any{
		"lab_id": req.LabID, "command": req.Command,
		"output": res.Message, "status": res.State,
		"owner_id": u.ID, "executed_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(e)
	view["host_agent_result"] = map[string]any{"op": res.Op, "state": res.State, "message": res.Message}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleSSMListCommands(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "ssm_commands", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > 100 {
		list = list[:100]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"commands": p3ListView(list), "count": len(list)})
}

// --- Trusted Advisor -----------------------------------------------------------

func (s *Server) handleAdvisorRun(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	findings := []map[string]any{}
	// Idle labs: READY labs older than 24h with no recorded resources.
	if s.Labs != nil {
		labs, err := s.Labs.ListByOwner(r.Context(), u.ID)
		if err == nil {
			for _, lab := range labs {
				if lab.State != "READY" {
					continue
				}
				age := time.Since(lab.CreatedAt)
				resources, _ := s.Resources.ListResources(r.Context(), lab.ID)
				if age > 24*time.Hour && len(resources) == 0 {
					findings = append(findings, map[string]any{
						"check": "idle-lab", "severity": "LOW",
						"resource_type": "lab", "resource_id": lab.ID,
						"detail": fmt.Sprintf("Lab %s (%s) has been READY for %.0f hours with no recorded resources.", lab.DefinitionID, lab.ID, age.Hours()),
					})
				}
			}
		}
	}
	// Unattached / open security groups and quota usage, across the
	// caller's labs.
	if s.Labs != nil {
		labs, _ := s.Labs.ListByOwner(r.Context(), u.ID)
		sgAttached := map[string]bool{}
		for _, lab := range labs {
			resources, _ := s.Resources.ListResources(r.Context(), lab.ID)
			for _, res := range resources {
				if res.Kind == "instance" {
					if cfg, ok := res.Config["security_groups"].([]any); ok {
						for _, g := range cfg {
							if gs, ok := g.(string); ok {
								sgAttached[gs] = true
							}
						}
					}
				}
			}
		}
		for _, lab := range labs {
			resources, _ := s.Resources.ListResources(r.Context(), lab.ID)
			for _, res := range resources {
				if res.Kind != "security-group" {
					continue
				}
				if !sgAttached[res.ID] && !sgAttached[res.Name] {
					findings = append(findings, map[string]any{
						"check": "unattached-security-group", "severity": "LOW",
						"resource_type": "security-group", "resource_id": res.ID,
						"detail": fmt.Sprintf("Security group %s (%s) is not attached to any instance.", res.Name, res.ID),
					})
				}
				rules, _ := res.Config["rules"].([]any)
				for _, raw := range rules {
					rm, _ := raw.(map[string]any)
					if rm == nil {
						continue
					}
					if cidr, _ := rm["cidr"].(string); cidr == "0.0.0.0/0" {
						findings = append(findings, map[string]any{
							"check": "open-security-group", "severity": "HIGH",
							"resource_type": "security-group", "resource_id": res.ID,
							"detail": fmt.Sprintf("Security group %s (%s) allows ingress from 0.0.0.0/0 on port %v.", res.Name, res.ID, rm["from_port"]),
						})
					}
				}
			}
		}
		// Quota usage: active labs vs the caller's role limit.
		if s.Quotas != nil {
			limits := s.roleLimits(r.Context(), u.Role)
			active := 0
			for _, lab := range labs {
				if lab.State == "READY" || lab.State == "PROVISIONING" || lab.State == "CREATED" {
					active++
				}
			}
			if limits.MaxLabs > 0 && active >= limits.MaxLabs {
				findings = append(findings, map[string]any{
					"check": "quota-usage", "severity": "MEDIUM",
					"resource_type": "quota", "resource_id": u.Role,
					"detail": fmt.Sprintf("Using %d of %d labs allowed for role %s.", active, limits.MaxLabs, u.Role),
				})
			}
		}
	}
	// S3 buckets without versioning.
	if s.AWS != nil && s.AWS.S3 != nil {
		if buckets, err := s.AWS.S3.ListBuckets(r.Context()); err == nil {
			for _, b := range buckets {
				enabled, err := s.AWS.S3.GetBucketVersioning(r.Context(), b.Name)
				if err == nil && !enabled {
					findings = append(findings, map[string]any{
						"check": "s3-no-versioning", "severity": "LOW",
						"resource_type": "s3-bucket", "resource_id": b.Name,
						"detail": "Bucket " + b.Name + " has versioning disabled.",
					})
				}
			}
		}
	}
	created := 0
	for _, f := range findings {
		_, err := s.p6CreateFinding(r.Context(), u.ID, "advisor",
			fmt.Sprintf("Trusted Advisor: %s on %s", f["check"], f["resource_id"]),
			fmt.Sprintf("%v", f["severity"]), fmt.Sprintf("%v", f["resource_type"]),
			fmt.Sprintf("%v", f["resource_id"]), fmt.Sprintf("%v", f["detail"]))
		if err == nil {
			created++
		}
	}
	_, err := st.P3Create(r.Context(), "advisor_results", &store.P3Entity{Name: "advisor-run-" + p6Sequence(), Data: map[string]any{
		"findings": created, "checks_run": []string{"idle-lab", "unattached-security-group", "open-security-group", "quota-usage", "s3-no-versioning"},
		"owner_id": u.ID, "run_at": p3Now(),
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"findings_created": created, "checks": findings,
		"note": "Every check ran against real resource state (labs, security groups, quotas, S3). A check with no matching resource creates no finding.",
	})
}

func (s *Server) handleAdvisorListResults(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "advisor_results", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	findings, _ := s.p6List(r.Context(), "findings", u)
	advisorFindings := []*store.P3Entity{}
	for _, f := range findings {
		if phase3.Str(f.Data, "source", "") == "advisor" {
			advisorFindings = append(advisorFindings, f)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"runs": p3ListView(list), "findings": p3ListView(advisorFindings),
		"count": len(list),
	})
}

// --- Cost Explorer + Budgets -----------------------------------------------------

// p6CurrentCost computes the educational cost from real usage right
// now: lab hours from lab age, storage from S3 object sizes, SQS
// message and Lambda invocation counts, RDS/cache age in hours.
func (s *Server) p6CurrentCost(ctx context.Context, u *auth.User) ([]phase6.CostLine, string, error) {
	labHours := map[string]float64{}
	notes := []string{}
	if s.Labs != nil {
		labs, err := s.Labs.ListByOwner(ctx, u.ID)
		if err != nil {
			return nil, "", err
		}
		for _, lab := range labs {
			if lab.State == "DESTROYED" || lab.State == "FAILED" {
				continue
			}
			hours := time.Since(lab.CreatedAt).Hours()
			if hours > 720 {
				hours = 720
			}
			// Profile comes from the lab definition when known;
			// default Micro (the lite default) otherwise.
			profile := "Micro"
			if def := labDefinitionProfile(lab.DefinitionID); def != "" {
				profile = def
			}
			labHours[profile] += hours
		}
	}
	storageGB := 0.0
	if s.AWS != nil && s.AWS.S3 != nil {
		buckets, err := s.AWS.S3.ListBuckets(ctx)
		if err != nil {
			notes = append(notes, "S3 storage could not be read (MinIO unreachable) — storage priced at 0")
		} else {
			for _, b := range buckets {
				objs, err := s.AWS.S3.ListObjects(ctx, b.Name, "")
				if err != nil {
					continue
				}
				for _, o := range objs {
					storageGB += float64(o.Size) / (1024 * 1024 * 1024)
				}
			}
		}
	} else {
		notes = append(notes, "S3 adapter not configured — storage priced at 0")
	}
	sqsCount := 0
	if msgs, err := s.P3.P3List(ctx, "sqs_messages"); err == nil {
		sqsCount = len(msgs)
	}
	lambdaCount := 0
	if invs, err := s.P3.P3List(ctx, "lambda_invocations"); err == nil {
		lambdaCount = len(invs)
	}
	rdsHours := 0.0
	if insts, err := s.P3.P3List(ctx, "rds_instances"); err == nil {
		for _, inst := range insts {
			if phase3.Str(inst.Data, "owner_id", "") != u.ID && !p3IsAdmin(u.Role) {
				continue
			}
			rdsHours += time.Since(inst.CreatedAt).Hours()
			if rdsHours > 720 {
				rdsHours = 720
			}
		}
	}
	cacheHours := 0.0
	if clusters, err := s.P3.P3List(ctx, "cache_clusters"); err == nil {
		for _, c := range clusters {
			if phase3.Str(c.Data, "owner_id", "") != u.ID && !p3IsAdmin(u.Role) {
				continue
			}
			cacheHours += time.Since(c.CreatedAt).Hours()
			if cacheHours > 720 {
				cacheHours = 720
			}
		}
	}
	lines := phase6.ComputeCost(phase6.DefaultPricing, labHours, storageGB, sqsCount, lambdaCount, rdsHours, cacheHours)
	return lines, strings.Join(notes, "; "), nil
}

// labDefinitionProfile maps a lab definition id to its teaching
// profile. Definitions carry no profile field in the store, so the
// cost model uses Micro (the lite default) unless the definition is
// one of the known heavier ones.
func labDefinitionProfile(definitionID string) string {
	switch definitionID {
	case "three-tier-app":
		return "Small"
	default:
		return "Micro"
	}
}

func (s *Server) handleCostExplorer(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	lines, note, err := s.p6CurrentCost(r.Context(), u)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"lines": lines, "total_usd": phase6.CostTotal(lines),
		"pricing":  phase6.DefaultPricing,
		"currency": "USD (educational estimates — not an AWS bill)",
		"note":     "Costs are computed from real usage in this deployment (lab hours, S3 bytes, SQS messages, Lambda invocations, RDS/cache hours) at the published teaching rates. " + note,
	})
}

func (s *Server) handleBudgetCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string  `json:"name"`
		LimitUSD float64 `json:"limit_usd"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.LimitUSD <= 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name and a positive limit_usd are required")
		return
	}
	e, err := st.P3Create(r.Context(), "cost_budgets", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"limit_usd": req.LimitUSD, "owner_id": u.ID,
		"note": "Budget alerts are evaluated against the real educational cost model on every read.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleBudgetList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "cost_budgets", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	lines, _, err := s.p6CurrentCost(r.Context(), u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	total := phase6.CostTotal(lines)
	views := []map[string]any{}
	for _, b := range list {
		v := p3View(b)
		limit := phase3.Float(b.Data, "limit_usd", 0)
		v["current_cost_usd"] = total
		v["alert"] = total >= limit
		v["usage_percent"] = 0.0
		if limit > 0 {
			v["usage_percent"] = total / limit * 100
		}
		views = append(views, v)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"budgets": views, "count": len(views), "current_cost_usd": total})
}

func (s *Server) handleBudgetGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	b, ok := s.p6GetOwned(w, r, "cost_budgets", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	lines, _, err := s.p6CurrentCost(r.Context(), u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	total := phase6.CostTotal(lines)
	v := p3View(b)
	v["current_cost_usd"] = total
	v["alert"] = total >= phase3.Float(b.Data, "limit_usd", 0)
	v["lines"] = lines
	s.writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleBudgetDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	if !s.p6DeleteOwned(w, r, "cost_budgets", chi.URLParam(r, "id"), u) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// --- Organizations --------------------------------------------------------------

func (s *Server) handleOrgCreateUnit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.ParentID != "" {
		if _, err := st.P3Get(r.Context(), "org_units", req.ParentID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no parent unit with that id")
			return
		}
	}
	e, err := st.P3Create(r.Context(), "org_units", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"parent_id": req.ParentID, "members": []any{}, "owner_id": u.ID,
		"note": "Organizational units over the real CloudForge user store: members are platform users (learner/instructor/admin), not AWS accounts.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleOrgListUnits(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if _, ok := s.p3Store(w, r); !ok {
		return
	}
	list, err := s.p6List(r.Context(), "org_units", u)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Resolve member ids to real user emails for display.
	users, _ := s.Users.List()
	byID := map[string]string{}
	for _, usr := range users {
		byID[usr.ID] = usr.Email
	}
	views := []map[string]any{}
	for _, e := range list {
		v := p3View(e)
		members, _ := e.Data["members"].([]any)
		names := []string{}
		for _, m := range members {
			if id, ok := m.(string); ok {
				if email, ok := byID[id]; ok {
					names = append(names, email)
				} else {
					names = append(names, id+" (unknown user)")
				}
			}
		}
		v["member_emails"] = names
		views = append(views, v)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"units": views, "count": len(views)})
}

func (s *Server) handleOrgAddMember(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	unit, ok := s.p6GetOwned(w, r, "org_units", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if _, err := s.Users.GetByID(req.UserID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no platform user with that id")
		return
	}
	members, _ := unit.Data["members"].([]any)
	for _, m := range members {
		if m == req.UserID {
			s.writeError(w, r, http.StatusConflict, "already_exists", "user is already a member of this unit")
			return
		}
	}
	members = append(members, req.UserID)
	unit.Data["members"] = members
	if _, err := st.P3Update(r.Context(), "org_units", unit); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(unit))
}

func (s *Server) handleOrgDeleteUnit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	unit, ok := s.p6GetOwned(w, r, "org_units", chi.URLParam(r, "id"), u)
	if !ok {
		return
	}
	children, _ := s.p6List(r.Context(), "org_units", u)
	for _, c := range children {
		if phase3.Str(c.Data, "parent_id", "") == unit.ID {
			s.writeError(w, r, http.StatusConflict, "has_children", "delete the child units first")
			return
		}
	}
	_ = st
	if err := s.P3.P3Delete(r.Context(), "org_units", unit.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}
