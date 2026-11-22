// Package store — Phase 3 durable store.
//
// Phase 3 (data, messaging, serverless) keeps every resource as a
// generic entity: an id, a name, and a JSON document in `data`.
// The memory implementation backs unit tests and the in-memory
// fallback; the Postgres implementation backs production (migration
// 009, one p3_* table per collection, data JSONB). Handlers own the
// service semantics (visibility timeouts, fan-out, pattern matching);
// this store only persists entities, so the two implementations stay
// the same shape and a resource created in memory survives the same
// handler code as one created in Postgres.
//
// Volumes are teaching-scale, so List returns every entity in the
// collection and handlers filter in Go; the indexed data->>'…'
// columns in migration 009 exist for operators inspecting the DB
// directly, not for the query path.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudforge/cloudforge/backend/internal/database"
	"github.com/cloudforge/cloudforge/backend/internal/ids"
)

// ErrP3Conflict is returned when a Phase 3 name already exists in a
// collection that requires unique names (queues, topics, tables…).
var ErrP3Conflict = errors.New("phase3 entity already exists")

// P3Entity is one Phase 3 resource. Data is the whole entity document;
// handlers define its keys per collection (see phase3.go helpers and
// the httpapi phase3 handlers for the per-collection shapes).
type P3Entity struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// P3Collections is the allowlist of collection names. Table names are
// derived from it only — never from caller input — so no SQL can be
// injected through a collection name.
var P3Collections = map[string]string{
	"dynamodb_tables":    "p3_dynamodb_tables",
	"dynamodb_items":     "p3_dynamodb_items",
	"rds_instances":      "p3_rds_instances",
	"rds_snapshots":      "p3_rds_snapshots",
	"cache_clusters":     "p3_cache_clusters",
	"load_balancers":     "p3_load_balancers",
	"lb_listeners":       "p3_lb_listeners",
	"target_groups":      "p3_target_groups",
	"lb_targets":         "p3_lb_targets",
	"dns_zones":          "p3_dns_zones",
	"dns_records":        "p3_dns_records",
	"sqs_queues":         "p3_sqs_queues",
	"sqs_messages":       "p3_sqs_messages",
	"sns_topics":         "p3_sns_topics",
	"sns_subscriptions":  "p3_sns_subscriptions",
	"sns_deliveries":     "p3_sns_deliveries",
	"eventbridge_rules":  "p3_eventbridge_rules",
	"lambda_functions":   "p3_lambda_functions",
	"lambda_invocations": "p3_lambda_invocations",
	"apigw_apis":         "p3_apigw_apis",
	"apigw_resources":    "p3_apigw_resources",
	"apigw_stages":       "p3_apigw_stages",
	"apigw_logs":         "p3_apigw_logs",
	// Phase 4 (containers + developer tools) — same generic store,
	// tables p4_* from migration 010_phase4.sql.
	"ecr_repos":               "p4_ecr_repos",
	"ecr_images":              "p4_ecr_images",
	"ecs_clusters":            "p4_ecs_clusters",
	"ecs_taskdefs":            "p4_ecs_taskdefs",
	"ecs_services":            "p4_ecs_services",
	"ecs_tasks":               "p4_ecs_tasks",
	"codebuild_projects":      "p4_codebuild_projects",
	"codebuild_builds":        "p4_codebuild_builds",
	"codepipeline_pipelines":  "p4_codepipeline_pipelines",
	"codepipeline_executions": "p4_codepipeline_executions",
	"codedeploy_apps":         "p4_codedeploy_apps",
	"codedeploy_deployments":  "p4_codedeploy_deployments",
	"cfn_stacks":              "p4_cfn_stacks",
	"cfn_changesets":          "p4_cfn_changesets",
	"cfn_events":              "p4_cfn_events",
	// Phase 5 (Terraform Learning) — same generic store, tables
	// p5_* from migration 011_tflearn.sql.
	"tflearn_projects": "p5_tflearn_projects",
	"tflearn_files":    "p5_tflearn_files",
	"tflearn_progress": "p5_tflearn_progress",
	"tflearn_runs":     "p5_tflearn_runs",
	"tflearn_states":   "p5_tflearn_states",
	"tflearn_locks":    "p5_tflearn_locks",
	// Phase 6 (advanced services) — same generic store, tables
	// p6_* from migration 012_phase6.sql.
	"athena_queries":        "p6_athena_queries",
	"athena_executions":     "p6_athena_executions",
	"glue_crawlers":         "p6_glue_crawlers",
	"glue_tables":           "p6_glue_tables",
	"kinesis_streams":       "p6_kinesis_streams",
	"kinesis_records":       "p6_kinesis_records",
	"firehose_streams":      "p6_firehose_streams",
	"firehose_deliveries":   "p6_firehose_deliveries",
	"quicksight_dashboards": "p6_quicksight_dashboards",
	"sagemaker_notebooks":   "p6_sagemaker_notebooks",
	"sagemaker_jobs":        "p6_sagemaker_jobs",
	"sagemaker_models":      "p6_sagemaker_models",
	"sagemaker_endpoints":   "p6_sagemaker_endpoints",
	"bedrock_invocations":   "p6_bedrock_invocations",
	"dms_tasks":             "p6_dms_tasks",
	"dms_logs":              "p6_dms_logs",
	"datasync_tasks":        "p6_datasync_tasks",
	"datasync_logs":         "p6_datasync_logs",
	"findings":              "p6_findings",
	"inspector_assessments": "p6_inspector_assessments",
	"config_items":          "p6_config_items",
	"config_rules":          "p6_config_rules",
	"config_results":        "p6_config_results",
	"waf_acls":              "p6_waf_acls",
	"kms_keys":              "p6_kms_keys",
	"secrets":               "p6_secrets",
	"vpc_peerings":          "p6_vpc_peerings",
	"transit_gateways":      "p6_transit_gateways",
	"tgw_attachments":       "p6_tgw_attachments",
	"vpc_endpoints":         "p6_vpc_endpoints",
	"netfirewalls":          "p6_netfirewalls",
	"flowlog_groups":        "p6_flowlog_groups",
	"flowlog_records":       "p6_flowlog_records",
	"ssm_commands":          "p6_ssm_commands",
	"advisor_results":       "p6_advisor_results",
	"cost_budgets":          "p6_cost_budgets",
	"org_units":             "p6_org_units",
}

// P3Table returns the Postgres table for a collection, or "".
func P3Table(collection string) string { return P3Collections[collection] }

// Phase3Store persists Phase 3 entities. UniqueName collections
// enforce name uniqueness in Create (the DB unique index is the
// backstop; the memory store enforces it in code).
type Phase3Store interface {
	P3Create(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error)
	P3Get(ctx context.Context, collection, id string) (*P3Entity, error)
	P3GetByName(ctx context.Context, collection, name string) (*P3Entity, error)
	P3List(ctx context.Context, collection string) ([]*P3Entity, error)
	P3Update(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error)
	P3Delete(ctx context.Context, collection, id string) error
}

// P3UniqueName marks the collections whose names must be unique.
// P3Create enforces it; P3Update keeps the name it was created with
// unless the caller changes it to a still-free name.
var P3UniqueName = map[string]bool{
	"dynamodb_tables": true, "rds_instances": true, "cache_clusters": true,
	"load_balancers": true, "target_groups": true, "dns_zones": true,
	"sqs_queues": true, "sns_topics": true, "eventbridge_rules": true,
	"lambda_functions": true, "apigw_apis": true,
	// Phase 4 unique-name collections.
	"ecr_repos": true, "ecs_clusters": true, "ecs_services": true,
	"codebuild_projects": true, "codepipeline_pipelines": true,
	"codedeploy_apps": true, "cfn_stacks": true,
	// Phase 6 unique-name collections.
	"athena_queries": true, "glue_crawlers": true, "kinesis_streams": true,
	"firehose_streams": true, "quicksight_dashboards": true,
	"sagemaker_notebooks": true, "sagemaker_jobs": true,
	"sagemaker_models": true, "sagemaker_endpoints": true,
	"dms_tasks": true, "datasync_tasks": true, "config_rules": true,
	"waf_acls": true, "kms_keys": true, "secrets": true,
	"vpc_peerings": true, "transit_gateways": true, "vpc_endpoints": true,
	"netfirewalls": true, "flowlog_groups": true, "cost_budgets": true,
	"org_units": true,
}

func p3Copy(e *P3Entity) *P3Entity {
	cp := *e
	cp.Data = map[string]any{}
	for k, v := range e.Data {
		cp.Data[k] = v
	}
	return &cp
}

func p3Validate(collection string, e *P3Entity) error {
	if P3Table(collection) == "" {
		return fmt.Errorf("unknown phase3 collection %q", collection)
	}
	if e.Name == "" {
		return errors.New("name is required")
	}
	if len(e.Name) > 128 {
		return errors.New("name must be at most 128 characters")
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	return nil
}

// --- Memory implementation -------------------------------------------------

type MemoryPhase3Store struct {
	mu   sync.Mutex
	data map[string]map[string]*P3Entity // collection -> id -> entity
}

func NewMemoryPhase3Store() *MemoryPhase3Store {
	return &MemoryPhase3Store{data: map[string]map[string]*P3Entity{}}
}

var _ Phase3Store = (*MemoryPhase3Store)(nil)

func (m *MemoryPhase3Store) coll(collection string) map[string]*P3Entity {
	if m.data[collection] == nil {
		m.data[collection] = map[string]*P3Entity{}
	}
	return m.data[collection]
}

func (m *MemoryPhase3Store) P3Create(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := p3Validate(collection, e); err != nil {
		return nil, err
	}
	c := m.coll(collection)
	if P3UniqueName[collection] {
		for _, ex := range c {
			if ex.Name == e.Name {
				return nil, ErrP3Conflict
			}
		}
	}
	cp := p3Copy(e)
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	now := time.Now().UTC()
	cp.CreatedAt = now
	cp.UpdatedAt = now
	c[cp.ID] = cp
	return p3Copy(cp), nil
}

func (m *MemoryPhase3Store) P3Get(ctx context.Context, collection, id string) (*P3Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if P3Table(collection) == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	e, ok := m.data[collection][id]
	if !ok {
		return nil, ErrNotFound
	}
	return p3Copy(e), nil
}

func (m *MemoryPhase3Store) P3GetByName(ctx context.Context, collection, name string) (*P3Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if P3Table(collection) == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	for _, e := range m.data[collection] {
		if e.Name == name {
			return p3Copy(e), nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryPhase3Store) P3List(ctx context.Context, collection string) ([]*P3Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if P3Table(collection) == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	out := []*P3Entity{}
	for _, e := range m.data[collection] {
		out = append(out, p3Copy(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryPhase3Store) P3Update(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := p3Validate(collection, e); err != nil {
		return nil, err
	}
	c := m.coll(collection)
	ex, ok := c[e.ID]
	if !ok {
		return nil, ErrNotFound
	}
	if P3UniqueName[collection] && e.Name != ex.Name {
		for _, other := range c {
			if other.ID != e.ID && other.Name == e.Name {
				return nil, ErrP3Conflict
			}
		}
	}
	cp := p3Copy(e)
	cp.CreatedAt = ex.CreatedAt
	cp.UpdatedAt = time.Now().UTC()
	c[cp.ID] = cp
	return p3Copy(cp), nil
}

func (m *MemoryPhase3Store) P3Delete(ctx context.Context, collection, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if P3Table(collection) == "" {
		return fmt.Errorf("unknown phase3 collection %q", collection)
	}
	if _, ok := m.data[collection][id]; !ok {
		return ErrNotFound
	}
	delete(m.data[collection], id)
	return nil
}

// --- Postgres implementation -------------------------------------------------

type PostgresPhase3Store struct{ db *database.DB }

func NewPostgresPhase3Store(db *database.DB) *PostgresPhase3Store {
	return &PostgresPhase3Store{db: db}
}

var _ Phase3Store = (*PostgresPhase3Store)(nil)

func p3IsUnique(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func (p *PostgresPhase3Store) P3Create(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error) {
	table := P3Table(collection)
	if err := p3Validate(collection, e); err != nil {
		return nil, err
	}
	cp := p3Copy(e)
	if cp.ID == "" {
		cp.ID = ids.New()
	}
	raw, err := json.Marshal(cp.Data)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = p.db.Pool.Exec(ctx,
		fmt.Sprintf("INSERT INTO %s (id, name, data, created_at, updated_at) VALUES ($1,$2,$3,$4,$5)", table),
		cp.ID, cp.Name, raw, now, now)
	if err != nil {
		if p3IsUnique(err) {
			return nil, ErrP3Conflict
		}
		return nil, err
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	return cp, nil
}

func (p *PostgresPhase3Store) p3Row(ctx context.Context, query string, args ...any) (*P3Entity, error) {
	row := p.db.Pool.QueryRow(ctx, query, args...)
	var e P3Entity
	var raw []byte
	if err := row.Scan(&e.ID, &e.Name, &raw, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrNotFound
	}
	if err := json.Unmarshal(raw, &e.Data); err != nil {
		return nil, err
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	return &e, nil
}

func (p *PostgresPhase3Store) P3Get(ctx context.Context, collection, id string) (*P3Entity, error) {
	table := P3Table(collection)
	if table == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	return p.p3Row(ctx,
		fmt.Sprintf("SELECT id, name, data, created_at, updated_at FROM %s WHERE id=$1", table), id)
}

func (p *PostgresPhase3Store) P3GetByName(ctx context.Context, collection, name string) (*P3Entity, error) {
	table := P3Table(collection)
	if table == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	return p.p3Row(ctx,
		fmt.Sprintf("SELECT id, name, data, created_at, updated_at FROM %s WHERE name=$1 LIMIT 1", table), name)
}

func (p *PostgresPhase3Store) P3List(ctx context.Context, collection string) ([]*P3Entity, error) {
	table := P3Table(collection)
	if table == "" {
		return nil, fmt.Errorf("unknown phase3 collection %q", collection)
	}
	rows, err := p.db.Pool.Query(ctx,
		fmt.Sprintf("SELECT id, name, data, created_at, updated_at FROM %s ORDER BY created_at ASC", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*P3Entity{}
	for rows.Next() {
		var e P3Entity
		var raw []byte
		if err := rows.Scan(&e.ID, &e.Name, &raw, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &e.Data); err != nil {
			return nil, err
		}
		if e.Data == nil {
			e.Data = map[string]any{}
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (p *PostgresPhase3Store) P3Update(ctx context.Context, collection string, e *P3Entity) (*P3Entity, error) {
	table := P3Table(collection)
	if err := p3Validate(collection, e); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(e.Data)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tag, err := p.db.Pool.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET name=$1, data=$2, updated_at=$3 WHERE id=$4", table),
		e.Name, raw, now, e.ID)
	if err != nil {
		if p3IsUnique(err) {
			return nil, ErrP3Conflict
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	cp := p3Copy(e)
	cp.UpdatedAt = now
	return cp, nil
}

func (p *PostgresPhase3Store) P3Delete(ctx context.Context, collection, id string) error {
	table := P3Table(collection)
	if table == "" {
		return fmt.Errorf("unknown phase3 collection %q", collection)
	}
	tag, err := p.db.Pool.Exec(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id=$1", table), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
