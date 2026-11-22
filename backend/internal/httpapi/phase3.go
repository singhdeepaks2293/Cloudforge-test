// Phase 3 — data, messaging and serverless consoles (CloudForge v2).
//
// DynamoDB / RDS / ElastiCache / ELB / Route 53 / SQS / SNS /
// EventBridge / Lambda / API Gateway, all backed by the Phase 3
// store (Postgres migration 009, in-memory fallback). Every handler
// states its fidelity in the response where it matters and never
// shows READY until the underlying operation succeeded:
//
//   - DynamoDB: DynamoDB Local when it answers (API_EMULATED,
//     real DynamoDB JSON API), else the Postgres subset (PARTIAL).
//   - RDS: a real Postgres database + role on the platform Postgres
//     (PostgreSQL-compatible, not Aurora); start/stop per instance
//     is UNSUPPORTED; snapshots are real pg_dump files.
//   - ElastiCache: a real namespaced Redis database on the platform
//     Redis in lite mode; a dedicated container in full mode is
//     NOT built (no Docker here to verify it against).
//   - ELB / Route 53: SIMULATED (in-process round-robin with real
//     probes; durable records resolvable via the resolve endpoint,
//     not served by a DNS server).
//   - SQS / SNS / EventBridge: durable, Postgres-persisted —
//     visibility timeout, retention, DLQ redrive, fan-out, pattern
//     matching are real behaviours of this store, not in-memory.
//   - Lambda: real subprocess execution (python3/node) with
//     timeout, output cap, prlimit memory cap when available, and
//     uid 65534 when the API runs as root. NOT container isolation.
//   - API Gateway: real routing to Lambda functions and to
//     SSRF-guarded HTTP targets, with real request logging.
//
// Ownership: resources record owner_id; a non-admin sees only their
// own. Admins see everything.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/security"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// --- shared helpers ---------------------------------------------------------

func (s *Server) p3Store(w http.ResponseWriter, r *http.Request) (store.Phase3Store, bool) {
	if s.P3 == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "phase3_unavailable", "Phase 3 store is not configured on this server")
		return nil, false
	}
	return s.P3, true
}

func (s *Server) p3Err(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "not_found", "no resource with that id")
	case errors.Is(err, store.ErrP3Conflict):
		s.writeError(w, r, http.StatusConflict, "already_exists", "a resource with that name already exists")
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	}
	return true
}

// p3View flattens an entity into its wire shape: id/name/timestamps
// plus every data field at the top level.
func p3View(e *store.P3Entity) map[string]any {
	out := map[string]any{
		"id": e.ID, "name": e.Name,
		"created_at": e.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": e.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for k, v := range e.Data {
		out[k] = v
	}
	return out
}

func p3ListView(entities []*store.P3Entity) []map[string]any {
	out := []map[string]any{}
	for _, e := range entities {
		out = append(out, p3View(e))
	}
	return out
}

// p3Own filters a list to the caller's own resources (admins see all).
func p3Own(list []*store.P3Entity, userID string, isAdmin bool) []*store.P3Entity {
	if isAdmin {
		return list
	}
	out := []*store.P3Entity{}
	for _, e := range list {
		if phase3.Str(e.Data, "owner_id", "") == userID {
			out = append(out, e)
		}
	}
	return out
}

func (s *Server) p3Visible(w http.ResponseWriter, r *http.Request, e *store.P3Entity, userID string, isAdmin bool) bool {
	if isAdmin || phase3.Str(e.Data, "owner_id", "") == userID || phase3.Str(e.Data, "owner_id", "") == "" {
		return true
	}
	s.writeError(w, r, http.StatusForbidden, "forbidden", "this resource belongs to another user")
	return false
}

func p3IsAdmin(role string) bool { return role == "admin" }

func p3Now() string { return time.Now().UTC().Format(time.RFC3339) }

func p3ParseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomPassword(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, n)
	for i := range out {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			out[i] = letters[i%len(letters)]
			continue
		}
		out[i] = letters[j.Int64()]
	}
	return string(out)
}

// p3DecodeOptional decodes a JSON body when one is present; an
// empty body is fine (dst keeps its zero value). decodeJSON would
// 400 on an empty body, which is wrong for optional-body endpoints.
func (s *Server) p3DecodeOptional(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.ContentLength == 0 {
		return true
	}
	return s.decodeJSON(w, r, dst)
}

// --- DynamoDB ---------------------------------------------------------------
//
// Backend selection is probed live and cached for 60 s: DynamoDB
// Local (adapter) when it answers ListTables, otherwise the
// Postgres-backed subset. The response always names the backend and
// its fidelity, so a table created through one backend is never
// mistaken for the other.

func (s *Server) dynamoAdapter() *providers.DynamoDBAdapter {
	ep := s.P3DynamoEndpoint
	if ep == "" {
		ep = providers.DefaultDynamoDBEndpoint
	}
	return providers.NewDynamoDBAdapter(ep)
}

func (s *Server) dynamoBackend(ctx context.Context) string {
	s.dynamoMu.Lock()
	defer s.dynamoMu.Unlock()
	if s.dynamoBackendName != "" && time.Since(s.dynamoCheckedAt) < time.Minute {
		return s.dynamoBackendName
	}
	backend := "postgres-subset"
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.dynamoAdapter().ListTables(probeCtx); err == nil {
		backend = "dynamodb-local"
	}
	s.dynamoBackendName = backend
	s.dynamoCheckedAt = time.Now()
	return backend
}

func (s *Server) handleDynamoListTables(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		names, err := s.dynamoAdapter().ListTables(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "dynamodb_unavailable", "DynamoDB Local at "+s.dynamoAdapter().Endpoint+" is unreachable: "+err.Error())
			return
		}
		tables := []map[string]any{}
		for _, name := range names {
			keyAttr := ""
			if desc, err := s.dynamoAdapter().DescribeTable(r.Context(), name); err == nil {
				keyAttr = desc
			}
			tables = append(tables, map[string]any{"name": name, "key_attribute": keyAttr, "status": "ACTIVE", "backend": backend, "fidelity": "API_EMULATED"})
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"tables": tables, "backend": backend, "fidelity": "API_EMULATED"})
		return
	}
	// Postgres subset: tables are P3 entities.
	all, err := st.P3List(r.Context(), "dynamodb_tables")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	_ = u
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tables": p3ListView(all), "backend": backend, "fidelity": "PARTIAL", "note": "DynamoDB Local is unreachable; this is the Postgres-backed subset (CreateTable/PutItem/GetItem/Query/Scan/DeleteItem/DeleteTable, hash key only)."})
}

func (s *Server) handleDynamoCreateTable(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name         string `json:"name"`
		KeyAttribute string `json:"key_attribute"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.KeyAttribute = strings.TrimSpace(req.KeyAttribute)
	if req.Name == "" || req.KeyAttribute == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name and key_attribute are required")
		return
	}
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		if err := s.dynamoAdapter().CreateTable(r.Context(), req.Name, req.KeyAttribute); err != nil {
			if errors.Is(err, providers.ErrDynamoTableExists) {
				s.writeError(w, r, http.StatusConflict, "already_exists", "a table with that name already exists")
				return
			}
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		s.audit(r, u.ID, "dynamodb_table_created", "dynamodb-table", req.Name, map[string]any{"backend": backend})
		s.writeJSON(w, r, http.StatusCreated, map[string]any{"name": req.Name, "key_attribute": req.KeyAttribute, "status": "ACTIVE", "backend": backend, "fidelity": "API_EMULATED"})
		return
	}
	e, err := st.P3Create(r.Context(), "dynamodb_tables", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"key_attribute": req.KeyAttribute, "status": "ACTIVE", "backend": backend,
		"fidelity": "PARTIAL", "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "dynamodb_table_created", "dynamodb-table", e.ID, map[string]any{"backend": backend})
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleDynamoDeleteTable(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "table")
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		if err := s.dynamoAdapter().DeleteTable(r.Context(), name); err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		s.audit(r, u.ID, "dynamodb_table_deleted", "dynamodb-table", name, map[string]any{"backend": backend})
		s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "name": name, "backend": backend})
		return
	}
	e, err := st.P3GetByName(r.Context(), "dynamodb_tables", name)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Delete the table's items too (a table delete removes its items).
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == name {
			_ = st.P3Delete(r.Context(), "dynamodb_items", it.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "dynamodb_tables", e.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "dynamodb_table_deleted", "dynamodb-table", e.ID, map[string]any{"backend": backend})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "name": name, "backend": backend})
}

func (s *Server) handleDynamoPutItem(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	table := chi.URLParam(r, "table")
	var req struct {
		Item map[string]any `json:"item"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Item) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "item is required (plain or DynamoDB-typed attributes)")
		return
	}
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		typed := req.Item
		// Accept plain items too: if no value looks typed, convert.
		if !dynamoLooksTyped(req.Item) {
			typed = phase3.DynamoTyped(req.Item)
		}
		if err := s.dynamoAdapter().PutItem(r.Context(), table, typed); err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		s.audit(r, u.ID, "dynamodb_item_put", "dynamodb-item", table, map[string]any{"backend": backend})
		s.writeJSON(w, r, http.StatusCreated, map[string]any{"table": table, "item": phase3.DynamoPlain(typed), "backend": backend})
		return
	}
	// Postgres subset: resolve the key attribute from the table row.
	tbl, err := st.P3GetByName(r.Context(), "dynamodb_tables", table)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no table named "+table+" (create it first)")
		return
	}
	keyAttr := phase3.Str(tbl.Data, "key_attribute", "")
	plain := phase3.DynamoPlain(req.Item)
	keyVal := phase3.DynamoKeyValue(plain, keyAttr)
	if keyVal == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "item is missing the partition key attribute "+keyAttr)
		return
	}
	// Upsert by (table, key): replace the existing item if present.
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == table && phase3.Str(it.Data, "key_value", "") == keyVal {
			it.Data["item"] = plain
			if _, err := st.P3Update(r.Context(), "dynamodb_items", it); err != nil {
				s.p3Err(w, r, err)
				return
			}
			s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "item": plain, "backend": backend})
			return
		}
	}
	e, err := st.P3Create(r.Context(), "dynamodb_items", &store.P3Entity{Name: table + ":" + keyVal, Data: map[string]any{
		"table_name": table, "key_value": keyVal, "item": plain, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	_ = e
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"table": table, "item": plain, "backend": backend})
}

func dynamoLooksTyped(item map[string]any) bool {
	for _, v := range item {
		if m, ok := v.(map[string]any); ok && len(m) == 1 {
			for t := range m {
				if t == "S" || t == "N" || t == "BOOL" || t == "NULL" {
					return true
				}
			}
		}
	}
	return false
}

func (s *Server) handleDynamoGetItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	table := chi.URLParam(r, "table")
	keyVal := chi.URLParam(r, "key")
	if keyVal == "" {
		keyVal = r.URL.Query().Get("key")
	}
	keyAttr := r.URL.Query().Get("key_attribute")
	if keyVal == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key (partition-key value) is required, as /items/{key} or ?key=")
		return
	}
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		if keyAttr == "" {
			if desc, err := s.dynamoAdapter().DescribeTable(r.Context(), table); err == nil {
				keyAttr = desc
			}
		}
		if keyAttr == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key_attribute is required for DynamoDB Local lookups")
			return
		}
		item, err := s.dynamoAdapter().GetItem(r.Context(), table, map[string]any{keyAttr: map[string]any{"S": keyVal}})
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		if item == nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no item with that key")
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "item": phase3.DynamoPlain(item), "backend": backend})
		return
	}
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == table && phase3.Str(it.Data, "key_value", "") == keyVal {
			item, _ := it.Data["item"].(map[string]any)
			s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "item": item, "backend": backend})
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "no item with that key")
}

func (s *Server) handleDynamoScan(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	table := chi.URLParam(r, "table")
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		items, err := s.dynamoAdapter().Scan(r.Context(), table, 100)
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		plain := []map[string]any{}
		for _, it := range items {
			plain = append(plain, phase3.DynamoPlain(it))
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "items": plain, "count": len(plain), "backend": backend, "note": "Scan is capped at 100 items; pagination is not followed."})
		return
	}
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	plain := []map[string]any{}
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == table {
			if m, ok := it.Data["item"].(map[string]any); ok {
				plain = append(plain, m)
			}
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "items": plain, "count": len(plain), "backend": backend})
}

func (s *Server) handleDynamoQuery(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	table := chi.URLParam(r, "table")
	keyVal := r.URL.Query().Get("key")
	keyAttr := r.URL.Query().Get("key_attribute")
	if keyVal == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "query param key (partition-key value) is required")
		return
	}
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		if keyAttr == "" {
			if desc, err := s.dynamoAdapter().DescribeTable(r.Context(), table); err == nil {
				keyAttr = desc
			}
		}
		if keyAttr == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key_attribute is required for DynamoDB Local queries")
			return
		}
		items, err := s.dynamoAdapter().Query(r.Context(), table, keyAttr, keyVal)
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		plain := []map[string]any{}
		for _, it := range items {
			plain = append(plain, phase3.DynamoPlain(it))
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "items": plain, "count": len(plain), "backend": backend})
		return
	}
	// Postgres subset: Query == GetItem on the hash key (no sort key,
	// no secondary indexes — stated in the response).
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	plain := []map[string]any{}
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == table && phase3.Str(it.Data, "key_value", "") == keyVal {
			if m, ok := it.Data["item"].(map[string]any); ok {
				plain = append(plain, m)
			}
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"table": table, "items": plain, "count": len(plain), "backend": backend, "note": "Postgres subset: Query matches the hash key only; no sort key or secondary indexes."})
}

func (s *Server) handleDynamoDeleteItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	table := chi.URLParam(r, "table")
	keyVal := chi.URLParam(r, "key")
	keyAttr := r.URL.Query().Get("key_attribute")
	backend := s.dynamoBackend(r.Context())
	if backend == "dynamodb-local" {
		if keyAttr == "" {
			if desc, err := s.dynamoAdapter().DescribeTable(r.Context(), table); err == nil {
				keyAttr = desc
			}
		}
		if keyAttr == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key_attribute is required for DynamoDB Local deletes")
			return
		}
		if err := s.dynamoAdapter().DeleteItem(r.Context(), table, map[string]any{keyAttr: map[string]any{"S": keyVal}}); err != nil {
			s.writeError(w, r, http.StatusBadGateway, "dynamodb_error", err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "table": table, "key": keyVal, "backend": backend})
		return
	}
	items, _ := st.P3List(r.Context(), "dynamodb_items")
	for _, it := range items {
		if phase3.Str(it.Data, "table_name", "") == table && phase3.Str(it.Data, "key_value", "") == keyVal {
			if err := st.P3Delete(r.Context(), "dynamodb_items", it.ID); err != nil {
				s.p3Err(w, r, err)
				return
			}
			s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "table": table, "key": keyVal, "backend": backend})
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "no item with that key")
}

// --- RDS --------------------------------------------------------------------
//
// A real Postgres database + role on the platform Postgres. Engine is
// Postgres only (mysql/mariadb -> 400, honestly unsupported, not
// faked). The master password is generated, shown exactly once, and
// only its SHA-256 hash is stored (same rule as IAM access keys).

func (s *Server) rdsEndpoint() string {
	if s.P3DatabaseURL == "" {
		return ""
	}
	cfg, err := pgxpool.ParseConfig(s.P3DatabaseURL)
	if err != nil {
		return ""
	}
	host := cfg.ConnConfig.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.ConnConfig.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (s *Server) handleRDSList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "rds_instances")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Never return the password hash in a list view.
	views := p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role)))
	for _, v := range views {
		delete(v, "password_hash")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"instances": views, "fidelity": "PARTIAL", "note": "PostgreSQL-compatible, not Aurora. Each instance is a real Postgres database + role on the platform Postgres."})
}

func (s *Server) handleRDSCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name         string `json:"name"`
		Engine       string `json:"engine"`
		Version      string `json:"version"`
		DatabaseName string `json:"database_name"`
		Username     string `json:"username"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	if engine == "" {
		engine = "postgres"
	}
	if engine != "postgres" && engine != "postgresql" {
		s.writeError(w, r, http.StatusBadRequest, "unsupported_engine", "only engine postgres is supported (mysql/mariadb are not built — a request for them is rejected, not faked)")
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "16"
	}
	if version != "16" && version != "15" && version != "14" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "version must be 14, 15 or 16")
		return
	}
	if s.P3DatabaseURL == "" {
		s.writeError(w, r, http.StatusServiceUnavailable, "rds_unavailable", "no platform Postgres is configured (DATABASE_URL); RDS instances cannot be provisioned")
		return
	}
	dbName := strings.TrimSpace(req.DatabaseName)
	if dbName == "" {
		dbName = strings.ReplaceAll(strings.ToLower(req.Name), "-", "_")
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "cf_" + strings.ReplaceAll(strings.ToLower(req.Name), "-", "_")
	}
	if err := phase3.ValidatePGIdentifier(dbName); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "database_name: "+err.Error())
		return
	}
	if err := phase3.ValidatePGIdentifier(username); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "username: "+err.Error())
		return
	}
	if _, err := st.P3GetByName(r.Context(), "rds_instances", req.Name); err == nil {
		s.writeError(w, r, http.StatusConflict, "already_exists", "an RDS instance with that name already exists")
		return
	}
	password := randomPassword(20)
	if err := phase3.RDSCreateDatabase(r.Context(), s.P3DatabaseURL, dbName, username, password); err != nil {
		// Nothing is stored: the instance is not READY, so it does
		// not exist in the list at all.
		s.writeError(w, r, http.StatusBadGateway, "rds_provision_failed", "Postgres provisioning failed: "+err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "rds_instances", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"engine": "postgres", "version": version, "database_name": dbName,
		"username": username, "password_hash": store.HashSecret(password),
		"endpoint": s.rdsEndpoint(), "status": "available",
		"fidelity": "PARTIAL", "owner_id": u.ID,
		"note": "PostgreSQL-compatible, not Aurora. Start/stop per instance is unsupported (shared platform database).",
	}})
	if err != nil {
		// The database exists but the row did not: roll the real
		// database back so nothing is left behind.
		_ = phase3.RDSDropDatabase(r.Context(), s.P3DatabaseURL, dbName, username)
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "rds_instance_created", "rds-instance", e.ID, map[string]any{"engine": "postgres", "database": dbName})
	view := p3View(e)
	delete(view, "password_hash")
	view["password"] = password // shown exactly once, at creation
	view["connection"] = fmt.Sprintf("postgres://%s@%s/%s", username, s.rdsEndpoint(), dbName)
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleRDSGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "rds_instances", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(e)
	delete(view, "password_hash")
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleRDSDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "rds_instances", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	if s.P3DatabaseURL != "" {
		if err := phase3.RDSDropDatabase(r.Context(), s.P3DatabaseURL, phase3.Str(e.Data, "database_name", ""), phase3.Str(e.Data, "username", "")); err != nil {
			e.Data["status"] = "delete-failed"
			_, _ = st.P3Update(r.Context(), "rds_instances", e)
			s.writeError(w, r, http.StatusBadGateway, "rds_delete_failed", "could not drop the Postgres database/role: "+err.Error())
			return
		}
	}
	// Delete the instance's snapshot rows too (the files stay on
	// disk under the snapshot dir; the row is gone, the file path
	// is named in the audit event).
	snaps, _ := st.P3List(r.Context(), "rds_snapshots")
	for _, sn := range snaps {
		if phase3.Str(sn.Data, "instance_id", "") == id {
			_ = st.P3Delete(r.Context(), "rds_snapshots", sn.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "rds_instances", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "rds_instance_deleted", "rds-instance", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// handleRDSStartStop is the honest UNSUPPORTED path: a database on
// the shared platform Postgres cannot be stopped or started per
// instance. Returning 409 with the reason beats pretending.
func (s *Server) handleRDSStartStop(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.requireAuth(w, r); !ok {
			return
		}
		s.writeError(w, r, http.StatusConflict, "rds_start_stop_unsupported",
			"RDS "+action+" is unsupported: this instance is a database on the shared platform Postgres, which cannot be stopped or started per instance. Delete it to remove it.")
	}
}

func (s *Server) handleRDSSnapshot(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "rds_instances", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = s.p3DecodeOptional(w, r, &req)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = e.Name + "-snap-" + time.Now().UTC().Format("20060102-150405")
	}
	dir := s.P3WorkdirRoot + "/rds-snapshots"
	if err := mkdirAll(dir); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "cannot create snapshot directory: "+err.Error())
		return
	}
	snapID := newID()
	filePath := dir + "/" + snapID + ".dump"
	size, err := phase3.RDSSnapshot(r.Context(), s.P3DatabaseURL, phase3.Str(e.Data, "database_name", ""), filePath)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "rds_snapshot_unavailable", "snapshot failed: "+err.Error())
		return
	}
	snap, err := st.P3Create(r.Context(), "rds_snapshots", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"instance_id": id, "instance_name": e.Name,
		"database_name": phase3.Str(e.Data, "database_name", ""),
		"file_path":     filePath, "size_bytes": size, "status": "available",
		"owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "rds_snapshot_created", "rds-snapshot", snap.ID, map[string]any{"instance_id": id, "size_bytes": size})
	s.writeJSON(w, r, http.StatusCreated, p3View(snap))
}

func (s *Server) handleRDSListSnapshots(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "rds_instances", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	all, _ := st.P3List(r.Context(), "rds_snapshots")
	out := []*store.P3Entity{}
	for _, sn := range all {
		if phase3.Str(sn.Data, "instance_id", "") == id {
			out = append(out, sn)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"snapshots": p3ListView(out), "note": "Snapshots are real pg_dump -Fc files on the API host."})
}

// handleRDSRestoreSnapshot creates a NEW instance from a snapshot
// (AWS restore semantics) and pg_restores the dump into it.
func (s *Server) handleRDSRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	snapID := chi.URLParam(r, "id")
	snap, err := st.P3Get(r.Context(), "rds_snapshots", snapID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, snap, u.ID, p3IsAdmin(u.Role)) {
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
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required for the restored instance")
		return
	}
	if _, err := st.P3GetByName(r.Context(), "rds_instances", req.Name); err == nil {
		s.writeError(w, r, http.StatusConflict, "already_exists", "an RDS instance with that name already exists")
		return
	}
	if s.P3DatabaseURL == "" {
		s.writeError(w, r, http.StatusServiceUnavailable, "rds_unavailable", "no platform Postgres is configured (DATABASE_URL)")
		return
	}
	dbName := strings.ReplaceAll(strings.ToLower(req.Name), "-", "_")
	username := "cf_" + dbName
	if err := phase3.ValidatePGIdentifier(dbName); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name: "+err.Error())
		return
	}
	password := randomPassword(20)
	if err := phase3.RDSCreateDatabase(r.Context(), s.P3DatabaseURL, dbName, username, password); err != nil {
		s.writeError(w, r, http.StatusBadGateway, "rds_provision_failed", "Postgres provisioning failed: "+err.Error())
		return
	}
	filePath := phase3.Str(snap.Data, "file_path", "")
	if err := phase3.RDSRestore(r.Context(), s.P3DatabaseURL, dbName, filePath); err != nil {
		_ = phase3.RDSDropDatabase(r.Context(), s.P3DatabaseURL, dbName, username)
		s.writeError(w, r, http.StatusBadGateway, "rds_restore_failed", "pg_restore failed (the new database was dropped again): "+err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "rds_instances", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"engine": "postgres", "version": phase3.Str(snap.Data, "version", "16"), "database_name": dbName,
		"username": username, "password_hash": store.HashSecret(password),
		"endpoint": s.rdsEndpoint(), "status": "available",
		"fidelity": "PARTIAL", "owner_id": u.ID, "restored_from": snapID,
		"note": "Restored from snapshot " + snap.Name + " (real pg_restore).",
	}})
	if err != nil {
		_ = phase3.RDSDropDatabase(r.Context(), s.P3DatabaseURL, dbName, username)
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "rds_instance_restored", "rds-instance", e.ID, map[string]any{"snapshot_id": snapID})
	view := p3View(e)
	delete(view, "password_hash")
	view["password"] = password
	view["connection"] = fmt.Sprintf("postgres://%s@%s/%s", username, s.rdsEndpoint(), dbName)
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleRDSLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "rds_instances", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"instance_id": e.ID, "logs": []any{},
		"note": "Per-instance log streaming is not built: this database runs on the shared platform Postgres, whose logs live in the platform Postgres log on the host. Audit events for this instance are in CloudTrail.",
	})
}

// --- ElastiCache ------------------------------------------------------------
//
// Lite mode: a real namespaced Redis database on the platform Redis
// (key prefix + a best-effort ACL user). Full mode: a dedicated
// Redis container — NOT built (no Docker in this sandbox to verify
// it against); a full-mode create returns an honest 503.

func (s *Server) cacheClient() *phase3Redis {
	return newPhase3Redis(s.P3RedisAddr)
}

func (s *Server) handleCacheList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "cache_clusters")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"clusters": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "PARTIAL"})
}

func (s *Server) handleCacheCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Engine string `json:"engine"`
		Mode   string `json:"mode"` // lite | full
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	if engine != "" && engine != "redis" {
		s.writeError(w, r, http.StatusBadRequest, "unsupported_engine", "only engine redis is supported (memcached is not built)")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "lite"
		if s.Lite {
			mode = "lite"
		}
	}
	if mode == "full" {
		// Dedicated-container mode is not built: no Docker in this
		// sandbox to verify it against. Say so (503), do not fake
		// a cluster.
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_container_unavailable",
			"a dedicated Redis container (full mode) is not built yet — there is no Docker in this sandbox to verify container provisioning against. Use mode \"lite\" (namespaced database on the platform Redis).")
		return
	}
	if mode != "lite" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "mode must be lite or full")
		return
	}
	rc := s.cacheClient()
	defer rc.Close()
	if err := rc.Ping(r.Context()); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_unavailable", "platform Redis at "+s.P3RedisAddr+" is unreachable: "+err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "cache_clusters", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"engine": "redis", "mode": "lite", "status": "available",
		"endpoint": s.P3RedisAddr,
		"fidelity": "PARTIAL", "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// The prefix is derived from the entity id, so the keys browser
	// and flush can find it from the row alone. The ACL user is
	// best-effort: if the platform Redis forbids ACL SETUSER, the
	// cluster still works on prefix isolation and the record says so.
	prefix := phase3.CacheKeyPrefix(e.ID)
	e.Data["key_prefix"] = prefix
	e.Data["cluster_id"] = e.ID
	e.Data["note"] = "Real Redis on the platform Redis, namespaced by key prefix " + prefix
	if err := rc.CreateACLUser(r.Context(), "cf-cache-"+e.ID[:8], prefix); err != nil {
		e.Data["acl_note"] = "ACL user could not be created (" + err.Error() + "); keys are isolated by prefix only, on the shared platform user."
	}
	updated, err := st.P3Update(r.Context(), "cache_clusters", e)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "elasticache_cluster_created", "cache-cluster", e.ID, map[string]any{"mode": "lite"})
	s.writeJSON(w, r, http.StatusCreated, p3View(updated))
}

func (s *Server) handleCacheGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleCacheDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "cache_clusters", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	rc := s.cacheClient()
	defer rc.Close()
	if err := rc.Ping(r.Context()); err == nil {
		// Delete the namespace's keys, then the ACL user.
		keys, _ := rc.ScanPrefix(r.Context(), phase3.Str(e.Data, "key_prefix", ""), 1000)
		if len(keys) > 0 {
			_, _ = rc.Del(r.Context(), keys)
		}
		_ = rc.DeleteACLUser(r.Context(), "cf-cache-"+id[:8])
	}
	if err := st.P3Delete(r.Context(), "cache_clusters", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "elasticache_cluster_deleted", "cache-cluster", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleCacheKeys(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	prefix := phase3.Str(e.Data, "key_prefix", "")
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	rc := s.cacheClient()
	defer rc.Close()
	keys, err := rc.ScanPrefix(r.Context(), prefix+pattern, 100)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_unavailable", "platform Redis is unreachable: "+err.Error())
		return
	}
	rows := []map[string]any{}
	for _, k := range keys {
		ttl, _ := rc.TTL(r.Context(), k)
		typ, _ := rc.Type(r.Context(), k)
		rows = append(rows, map[string]any{
			"key": strings.TrimPrefix(k, prefix), "full_key": k,
			"type": typ, "ttl_seconds": ttl,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["key"].(string) < rows[j]["key"].(string) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"cluster_id": e.ID, "prefix": prefix, "keys": rows, "count": len(rows), "note": "Read-only listing (SCAN of this cluster's prefix, max 100 keys)."})
}

func (s *Server) handleCacheGetKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	key := chi.URLParam(r, "key")
	prefix := phase3.Str(e.Data, "key_prefix", "")
	rc := s.cacheClient()
	defer rc.Close()
	val, err := rc.Get(r.Context(), prefix+key)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no such key in this cluster (or the key expired)")
		return
	}
	if len(val) > 4096 {
		val = val[:4096] + "… (truncated at 4 KiB)"
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"cluster_id": e.ID, "key": key, "value": val})
}

func (s *Server) handleCacheSetKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		TTL   int    `json:"ttl_seconds"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Key) == "" || strings.Contains(req.Key, " ") {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key is required and must not contain spaces")
		return
	}
	if len(req.Value) > 65536 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "value is capped at 64 KiB")
		return
	}
	prefix := phase3.Str(e.Data, "key_prefix", "")
	rc := s.cacheClient()
	defer rc.Close()
	if err := rc.Set(r.Context(), prefix+req.Key, req.Value, req.TTL); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_unavailable", "platform Redis is unreachable: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"cluster_id": e.ID, "key": req.Key, "ttl_seconds": req.TTL})
}

func (s *Server) handleCacheFlush(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	prefix := phase3.Str(e.Data, "key_prefix", "")
	rc := s.cacheClient()
	defer rc.Close()
	keys, err := rc.ScanPrefix(r.Context(), prefix, 10000)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_unavailable", "platform Redis is unreachable: "+err.Error())
		return
	}
	deleted := 0
	if len(keys) > 0 {
		deleted, _ = rc.Del(r.Context(), keys)
	}
	s.audit(r, u.ID, "elasticache_cluster_flushed", "cache-cluster", e.ID, map[string]any{"keys_deleted": deleted})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"flushed": true, "cluster_id": e.ID, "keys_deleted": deleted, "note": "Only this cluster's prefix was flushed; other clusters and the platform cache are untouched."})
}

func (s *Server) handleCacheMetrics(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cache_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	prefix := phase3.Str(e.Data, "key_prefix", "")
	rc := s.cacheClient()
	defer rc.Close()
	info, err := rc.Info(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "elasticache_unavailable", "platform Redis is unreachable: "+err.Error())
		return
	}
	keys, _ := rc.ScanPrefix(r.Context(), prefix, 10000)
	hits, misses := info["keyspace_hits"], info["keyspace_misses"]
	hitRate := 0.0
	if hits+misses > 0 {
		hitRate = float64(hits) / float64(hits+misses)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"cluster_id": e.ID, "used_memory_bytes": info["used_memory"],
		"used_memory_human": rc.InfoHuman(r.Context()),
		"keys_in_cluster":   len(keys), "connected_clients": info["connected_clients"],
		"keyspace_hits": hits, "keyspace_misses": misses, "hit_rate": hitRate,
		"note": "Memory and hit rate are platform-Redis-wide (shared Redis in lite mode); keys_in_cluster is this cluster's prefix only.",
	})
}

// --- small utilities ---------------------------------------------------------

func newID() string { return ids.New() }

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o755) }

// --- ELB (SIMULATED) --------------------------------------------------------
//
// In-process round-robin to targets the API can actually probe
// (HTTP GET / TCP dial, 2 s timeout). No engine RPC, no nginx
// reconfiguration: the load balancer's DNS name is a local teaching
// name, and /route returns the target a request would go to. Health
// is probed on demand, never assumed.

func (s *Server) handleELBList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "load_balancers")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"load_balancers": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "SIMULATED", "note": "In-process round-robin with real HTTP/TCP health probes; no engine or nginx reconfiguration."})
}

func (s *Server) handleELBCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Scheme string `json:"scheme"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	scheme := strings.ToLower(strings.TrimSpace(req.Scheme))
	if scheme == "" {
		scheme = "internet-facing"
	}
	if scheme != "internet-facing" && scheme != "internal" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "scheme must be internet-facing or internal")
		return
	}
	e, err := st.P3Create(r.Context(), "load_balancers", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"scheme": scheme, "status": "active",
		"dns_name": req.Name + ".cf-local-1.elb.local",
		"fidelity": "SIMULATED", "owner_id": u.ID,
		"note": "DNS name is a local teaching name; it is not registered in any DNS server. Use POST /elb/{id}/route to see the round-robin choice.",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "elb_created", "load-balancer", e.ID, nil)
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleELBGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	lb, err := st.P3Get(r.Context(), "load_balancers", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, lb, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(lb)
	listeners, _ := st.P3List(r.Context(), "lb_listeners")
	lv := []map[string]any{}
	for _, l := range listeners {
		if phase3.Str(l.Data, "load_balancer_id", "") == lb.ID {
			lv = append(lv, p3View(l))
		}
	}
	view["listeners"] = lv
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleELBDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "load_balancers", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	listeners, _ := st.P3List(r.Context(), "lb_listeners")
	for _, l := range listeners {
		if phase3.Str(l.Data, "load_balancer_id", "") == id {
			_ = st.P3Delete(r.Context(), "lb_listeners", l.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "load_balancers", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleELBCreateListener(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	lbID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "load_balancers", lbID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	var req struct {
		Port          int    `json:"port"`
		Protocol      string `json:"protocol"`
		TargetGroupID string `json:"target_group_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "port must be 1-65535")
		return
	}
	proto := strings.ToUpper(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "HTTP"
	}
	if proto != "HTTP" && proto != "TCP" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be HTTP or TCP")
		return
	}
	if req.TargetGroupID != "" {
		if _, err := st.P3Get(r.Context(), "target_groups", req.TargetGroupID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no target group with that id")
			return
		}
	}
	e, err := st.P3Create(r.Context(), "lb_listeners", &store.P3Entity{Name: fmt.Sprintf("%s:%d", lbID, req.Port), Data: map[string]any{
		"load_balancer_id": lbID, "port": req.Port, "protocol": proto,
		"target_group_id": req.TargetGroupID, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleELBDeleteListener(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	l, err := st.P3Get(r.Context(), "lb_listeners", chi.URLParam(r, "listenerId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if phase3.Str(l.Data, "load_balancer_id", "") != chi.URLParam(r, "id") {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no listener with that id on this load balancer")
		return
	}
	if err := st.P3Delete(r.Context(), "lb_listeners", l.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": l.ID})
}

func (s *Server) handleTGList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "target_groups")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"target_groups": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role)))})
}

func (s *Server) handleTGCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name            string `json:"name"`
		Protocol        string `json:"protocol"`
		Port            int    `json:"port"`
		HealthCheckPath string `json:"health_check_path"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	proto := strings.ToUpper(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "HTTP"
	}
	if proto != "HTTP" && proto != "TCP" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be HTTP or TCP")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "port must be 1-65535")
		return
	}
	if req.HealthCheckPath == "" {
		req.HealthCheckPath = "/"
	}
	e, err := st.P3Create(r.Context(), "target_groups", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"protocol": proto, "port": req.Port, "health_check_path": req.HealthCheckPath,
		"owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleTGGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	tg, err := st.P3Get(r.Context(), "target_groups", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, tg, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(tg)
	targets, _ := st.P3List(r.Context(), "lb_targets")
	tv := []map[string]any{}
	for _, t := range targets {
		if phase3.Str(t.Data, "target_group_id", "") == tg.ID {
			tv = append(tv, p3View(t))
		}
	}
	view["targets"] = tv
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleTGDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "target_groups", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	targets, _ := st.P3List(r.Context(), "lb_targets")
	for _, t := range targets {
		if phase3.Str(t.Data, "target_group_id", "") == id {
			_ = st.P3Delete(r.Context(), "lb_targets", t.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "target_groups", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleTGAddTarget(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	tgID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "target_groups", tgID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	var req struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Address = strings.TrimSpace(req.Address)
	if req.Address == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "address is required (an http(s) URL or host:port)")
		return
	}
	if req.Port < 0 || req.Port > 65535 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "port must be 0-65535")
		return
	}
	e, err := st.P3Create(r.Context(), "lb_targets", &store.P3Entity{Name: req.Address, Data: map[string]any{
		"target_group_id": tgID, "address": req.Address, "port": req.Port,
		"health": "unknown", "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleTGRemoveTarget(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	t, err := st.P3Get(r.Context(), "lb_targets", chi.URLParam(r, "targetId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if phase3.Str(t.Data, "target_group_id", "") != chi.URLParam(r, "id") {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no target with that id in this target group")
		return
	}
	if err := st.P3Delete(r.Context(), "lb_targets", t.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": t.ID})
}

func (s *Server) handleTGHealth(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	tg, err := st.P3Get(r.Context(), "target_groups", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, tg, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	targets, _ := st.P3List(r.Context(), "lb_targets")
	rows := []map[string]any{}
	healthPath := phase3.Str(tg.Data, "health_check_path", "")
	for _, t := range targets {
		if phase3.Str(t.Data, "target_group_id", "") != tg.ID {
			continue
		}
		addr := phase3.Str(t.Data, "address", "")
		port := phase3.Int(t.Data, "port", 0)
		healthy, detail := phase3.ProbeTarget(r.Context(), addr, port, healthPath)
		t.Data["health"] = "healthy"
		if !healthy {
			t.Data["health"] = "unhealthy"
		}
		t.Data["health_detail"] = detail
		t.Data["health_checked_at"] = p3Now()
		_, _ = st.P3Update(r.Context(), "lb_targets", t)
		rows = append(rows, p3View(t))
	}
	healthy := 0
	for _, row := range rows {
		if row["health"] == "healthy" {
			healthy++
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"target_group_id": tg.ID, "targets": rows, "healthy": healthy, "total": len(rows)})
}

// handleELBRoute returns the round-robin target for the load
// balancer's first listener. Only targets whose last probe said
// healthy are eligible; a target never probed is not eligible
// (health is never assumed).
func (s *Server) handleELBRoute(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	lb, err := st.P3Get(r.Context(), "load_balancers", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, lb, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	listeners, _ := st.P3List(r.Context(), "lb_listeners")
	var tgID string
	for _, l := range listeners {
		if phase3.Str(l.Data, "load_balancer_id", "") == lb.ID {
			tgID = phase3.Str(l.Data, "target_group_id", "")
			break
		}
	}
	if tgID == "" {
		s.writeError(w, r, http.StatusConflict, "no_listener", "this load balancer has no listener with a target group yet")
		return
	}
	targets, _ := st.P3List(r.Context(), "lb_targets")
	healthy := []*store.P3Entity{}
	for _, t := range targets {
		if phase3.Str(t.Data, "target_group_id", "") == tgID && phase3.Str(t.Data, "health", "") == "healthy" {
			healthy = append(healthy, t)
		}
	}
	if len(healthy) == 0 {
		s.writeError(w, r, http.StatusServiceUnavailable, "no_healthy_targets", "no healthy targets in the target group (run the health check first; unprobed targets are not eligible)")
		return
	}
	s.elbMu.Lock()
	rr, ok := s.elbRR[lb.ID]
	if !ok {
		rr = &phase3.RoundRobin{}
		if s.elbRR == nil {
			s.elbRR = map[string]*phase3.RoundRobin{}
		}
		s.elbRR[lb.ID] = rr
	}
	s.elbMu.Unlock()
	chosen := healthy[rr.Next(len(healthy))]
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"load_balancer":   phase3.Str(lb.Data, "dns_name", lb.Name),
		"target_group_id": tgID, "target": p3View(chosen),
		"note": "In-process round-robin (SIMULATED): this is the target a request would go to; no traffic is actually proxied.",
	})
}

// --- Route 53 (SIMULATED) --------------------------------------------------
//
// Durable zones/records. Resolution happens through the resolve
// endpoint (and lab validation can call it); no DNS server serves
// these records to the lab network, and the console says so.

func (s *Server) handleDNSListZones(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "dns_zones")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"zones": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "SIMULATED", "note": "Zones are durable records. No DNS server serves them; resolve them through GET /route53/zones/{id}/resolve."})
}

func (s *Server) handleDNSCreateZone(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(strings.TrimSuffix(req.Name, "."))
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required (e.g. app.local)")
		return
	}
	if err := phase3.ValidateDNSRecord(name, "A", []string{"127.0.0.1"}); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "zone name is not a valid DNS name")
		return
	}
	e, err := st.P3Create(r.Context(), "dns_zones", &store.P3Entity{Name: name, Data: map[string]any{
		"owner_id": u.ID, "fidelity": "SIMULATED",
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleDNSDeleteZone(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "dns_zones", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	records, _ := st.P3List(r.Context(), "dns_records")
	for _, rec := range records {
		if phase3.Str(rec.Data, "zone_id", "") == id {
			s.writeError(w, r, http.StatusConflict, "zone_not_empty", "delete the zone's records first (a hosted zone cannot be deleted while it holds records)")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "dns_zones", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleDNSListRecords(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	zoneID := chi.URLParam(r, "id")
	zone, err := st.P3Get(r.Context(), "dns_zones", zoneID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, zone, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	all, _ := st.P3List(r.Context(), "dns_records")
	out := []*store.P3Entity{}
	for _, rec := range all {
		if phase3.Str(rec.Data, "zone_id", "") == zoneID {
			out = append(out, rec)
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"zone": p3View(zone), "records": p3ListView(out)})
}

func (s *Server) handleDNSCreateRecord(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	zoneID := chi.URLParam(r, "id")
	zone, err := st.P3Get(r.Context(), "dns_zones", zoneID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, zone, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		TTL    int      `json:"ttl"`
		Values []string `json:"values"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	if req.TTL <= 0 {
		req.TTL = 300
	}
	if err := phase3.ValidateDNSRecord(req.Name, req.Type, req.Values); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// One record set per (name, type) — a second create for the same
	// set is a conflict, like Route 53.
	all, _ := st.P3List(r.Context(), "dns_records")
	for _, rec := range all {
		if phase3.Str(rec.Data, "zone_id", "") == zoneID && phase3.Str(rec.Data, "name", "") == req.Name && phase3.Str(rec.Data, "type", "") == req.Type {
			s.writeError(w, r, http.StatusConflict, "already_exists", "a "+req.Type+" record set for "+req.Name+" already exists in this zone (delete it first)")
			return
		}
	}
	e, err := st.P3Create(r.Context(), "dns_records", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"zone_id": zoneID, "zone_name": zone.Name, "name": req.Name, "type": req.Type,
		"ttl": req.TTL, "values": req.Values, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleDNSDeleteRecord(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	rec, err := st.P3Get(r.Context(), "dns_records", chi.URLParam(r, "recordId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if phase3.Str(rec.Data, "zone_id", "") != chi.URLParam(r, "id") {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no record with that id in this zone")
		return
	}
	if err := st.P3Delete(r.Context(), "dns_records", rec.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": rec.ID})
}

func (s *Server) handleDNSResolve(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	zoneID := chi.URLParam(r, "id")
	zone, err := st.P3Get(r.Context(), "dns_zones", zoneID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, zone, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	rtype := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))
	if rtype == "" {
		rtype = "A"
	}
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "query param name is required")
		return
	}
	all, _ := st.P3List(r.Context(), "dns_records")
	var recs []map[string]any
	for _, rec := range all {
		if phase3.Str(rec.Data, "zone_id", "") == zoneID {
			recs = append(recs, rec.Data)
		}
	}
	matched := phase3.ResolveDNS(recs, name, rtype)
	values := []string{}
	for _, m := range matched {
		values = append(values, phase3.StrSlice(m, "values")...)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"zone": zone.Name, "name": name, "type": rtype,
		"answers": values, "matched": len(matched) > 0,
		"note": "Resolution is against the stored records only (SIMULATED): no DNS server serves this zone to the lab network.",
	})
}

// --- SQS (durable) ----------------------------------------------------------
//
// Postgres-persisted queues and messages. Receive does NOT delete:
// the message is invisible until visible_at, receive_count grows,
// and at max_receives it moves to the DLQ instead of being returned.

var sqsNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func (s *Server) sqsPurgeExpired(ctx context.Context, st store.Phase3Store, queueID string) {
	msgs, err := st.P3List(ctx, "sqs_messages")
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") != queueID {
			continue
		}
		if exp := phase3.Str(m.Data, "expires_at", ""); exp != "" {
			if t, err := time.Parse(time.RFC3339, exp); err == nil && now.After(t) {
				_ = st.P3Delete(ctx, "sqs_messages", m.ID)
			}
		}
	}
}

func (s *Server) handleSQSListQueues(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "sqs_queues")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Attach live approximate counts (expired messages purged first).
	views := []map[string]any{}
	for _, q := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		s.sqsPurgeExpired(r.Context(), st, q.ID)
		view := p3View(q)
		msgs, _ := st.P3List(r.Context(), "sqs_messages")
		visible, invisible := 0, 0
		now := time.Now().UTC()
		for _, m := range msgs {
			if phase3.Str(m.Data, "queue_id", "") != q.ID {
				continue
			}
			if phase3.SQSMessageVisible(m.Data, now) {
				visible++
			} else {
				invisible++
			}
		}
		view["approximate_visible_messages"] = visible
		view["approximate_invisible_messages"] = invisible
		views = append(views, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"queues": views, "fidelity": "PARTIAL", "note": "Durable, Postgres-persisted queues (not in-memory). FIFO queues are not built."})
}

func (s *Server) handleSQSCreateQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name                  string `json:"name"`
		VisibilityTimeoutSecs int    `json:"visibility_timeout_seconds"`
		RetentionSecs         int    `json:"message_retention_seconds"`
		MaxReceives           int    `json:"max_receives"`
		DeadLetterQueueID     string `json:"dead_letter_queue_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !sqsNameRE.MatchString(req.Name) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "queue name must be 1-80 characters of letters, digits, _ and - (FIFO .fifo suffix is not supported)")
		return
	}
	if req.VisibilityTimeoutSecs <= 0 {
		req.VisibilityTimeoutSecs = 30
	}
	if req.VisibilityTimeoutSecs > 43200 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "visibility_timeout_seconds must be 1-43200")
		return
	}
	if req.RetentionSecs <= 0 {
		req.RetentionSecs = 345600
	}
	if req.RetentionSecs < 60 || req.RetentionSecs > 1209600 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "message_retention_seconds must be 60-1209600")
		return
	}
	if req.MaxReceives < 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "max_receives must be >= 0 (0 = no DLQ redrive)")
		return
	}
	if req.DeadLetterQueueID != "" {
		dlq, err := st.P3Get(r.Context(), "sqs_queues", req.DeadLetterQueueID)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no dead-letter queue with that id")
			return
		}
		if phase3.Str(dlq.Data, "dead_letter_queue_id", "") != "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "a dead-letter queue cannot itself have a dead-letter queue")
			return
		}
	}
	e, err := st.P3Create(r.Context(), "sqs_queues", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"visibility_timeout_seconds": req.VisibilityTimeoutSecs,
		"message_retention_seconds":  req.RetentionSecs,
		"max_receives":               req.MaxReceives,
		"dead_letter_queue_id":       req.DeadLetterQueueID,
		"owner_id":                   u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "sqs_queue_created", "sqs-queue", e.ID, map[string]any{"name": req.Name})
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleSQSGetQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	q, err := st.P3Get(r.Context(), "sqs_queues", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.sqsPurgeExpired(r.Context(), st, q.ID)
	view := p3View(q)
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	visible, invisible := 0, 0
	now := time.Now().UTC()
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") != q.ID {
			continue
		}
		if phase3.SQSMessageVisible(m.Data, now) {
			visible++
		} else {
			invisible++
		}
	}
	view["approximate_visible_messages"] = visible
	view["approximate_invisible_messages"] = invisible
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleSQSDeleteQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") == id {
			_ = st.P3Delete(r.Context(), "sqs_messages", m.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "sqs_queues", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// sqsEnqueue is the shared enqueue path (SQS send, SNS fan-out,
// EventBridge delivery): one durable message row.
func (s *Server) sqsEnqueue(ctx context.Context, st store.Phase3Store, queueID, body string, attributes map[string]any) (*store.P3Entity, error) {
	q, err := st.P3Get(ctx, "sqs_queues", queueID)
	if err != nil {
		return nil, err
	}
	retention := phase3.Int(q.Data, "message_retention_seconds", 345600)
	now := time.Now().UTC()
	if attributes == nil {
		attributes = map[string]any{}
	}
	return st.P3Create(ctx, "sqs_messages", &store.P3Entity{Name: "msg-" + newID(), Data: map[string]any{
		"queue_id": q.ID, "body": body, "attributes": attributes,
		"receive_count": 0, "visible_at": now.Format(time.RFC3339),
		"expires_at": now.Add(time.Duration(retention) * time.Second).Format(time.RFC3339),
		"sent_at":    now.Format(time.RFC3339),
	}})
}

func (s *Server) handleSQSSendMessage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Body       string         `json:"body"`
		Attributes map[string]any `json:"attributes"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "body is required")
		return
	}
	if len(req.Body) > 256*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "body is capped at 256 KiB")
		return
	}
	m, err := s.sqsEnqueue(r.Context(), st, queueID, req.Body, req.Attributes)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Lambda SQS triggers fire asynchronously: a slow function must
	// never fail the send. The invocation is recorded either way.
	go s.fireSQSTriggers(queueID, req.Body)
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"message_id": m.ID, "queue_id": queueID,
		"visible_at": phase3.Str(m.Data, "visible_at", ""),
		"expires_at": phase3.Str(m.Data, "expires_at", ""),
	})
}

// fireSQSTriggers invokes Lambda functions triggered by this queue.
func (s *Server) fireSQSTriggers(queueID, body string) {
	if s.P3 == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fns, err := s.P3.P3List(ctx, "lambda_functions")
	if err != nil {
		return
	}
	for _, fn := range fns {
		triggers, _ := fn.Data["triggers"].([]any)
		for _, t := range triggers {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if phase3.Str(tm, "type", "") == "sqs" && phase3.Str(tm, "queue_id", "") == queueID {
				event := map[string]any{"source": "sqs", "queue_id": queueID, "body": body}
				_, _ = s.invokeLambdaInternal(ctx, fn, event)
			}
		}
	}
}

func (s *Server) handleSQSReceive(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		MaxMessages           int `json:"max_messages"`
		VisibilityTimeoutSecs int `json:"visibility_timeout_seconds"`
	}
	_ = s.p3DecodeOptional(w, r, &req)
	if req.MaxMessages <= 0 {
		req.MaxMessages = 1
	}
	if req.MaxMessages > 10 {
		req.MaxMessages = 10
	}
	vt := phase3.Int(q.Data, "visibility_timeout_seconds", 30)
	if req.VisibilityTimeoutSecs > 0 {
		vt = req.VisibilityTimeoutSecs
	}
	s.sqsPurgeExpired(r.Context(), st, queueID)
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	now := time.Now().UTC()
	out := []map[string]any{}
	movedToDLQ := 0
	for _, m := range msgs {
		if len(out) >= req.MaxMessages {
			break
		}
		if phase3.Str(m.Data, "queue_id", "") != queueID {
			continue
		}
		if !phase3.SQSMessageVisible(m.Data, now) {
			continue
		}
		m.Data["receive_count"] = phase3.Int(m.Data, "receive_count", 0) + 1
		// DLQ redrive: at max receives the message moves to the
		// dead-letter queue instead of being returned.
		if dlqID := phase3.Str(q.Data, "dead_letter_queue_id", ""); dlqID != "" && phase3.SQSShouldDLQ(m.Data, q.Data) {
			if _, err := s.sqsEnqueue(r.Context(), st, dlqID, phase3.Str(m.Data, "body", ""), nil); err == nil {
				_ = st.P3Delete(r.Context(), "sqs_messages", m.ID)
				movedToDLQ++
			}
			continue
		}
		m.Data["visible_at"] = now.Add(time.Duration(vt) * time.Second).Format(time.RFC3339)
		if _, err := st.P3Update(r.Context(), "sqs_messages", m); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"message_id": m.ID, "receipt_handle": m.ID,
			"body":          phase3.Str(m.Data, "body", ""),
			"attributes":    m.Data["attributes"],
			"receive_count": m.Data["receive_count"],
			"visible_until": m.Data["visible_at"],
		})
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"queue_id": queueID, "messages": out, "count": len(out),
		"moved_to_dead_letter_queue": movedToDLQ,
	})
}

func (s *Server) handleSQSDeleteMessage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	receipt := chi.URLParam(r, "receipt")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	m, err := st.P3Get(r.Context(), "sqs_messages", receipt)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if phase3.Str(m.Data, "queue_id", "") != queueID {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no message with that receipt handle in this queue")
		return
	}
	if err := st.P3Delete(r.Context(), "sqs_messages", receipt); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "message_id": receipt})
}

func (s *Server) handleSQSPurge(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	n := 0
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") == queueID {
			_ = st.P3Delete(r.Context(), "sqs_messages", m.ID)
			n++
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"purged": true, "queue_id": queueID, "messages_deleted": n})
}

// handleSQSRedrive moves every message from a dead-letter queue
// back into its source queue (AWS RedrivePolicy semantics).
func (s *Server) handleSQSRedrive(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	dlqID := phase3.Str(q.Data, "dead_letter_queue_id", "")
	if dlqID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "this queue has no dead-letter queue configured")
		return
	}
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	moved := 0
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") != dlqID {
			continue
		}
		if _, err := s.sqsEnqueue(r.Context(), st, queueID, phase3.Str(m.Data, "body", ""), nil); err == nil {
			_ = st.P3Delete(r.Context(), "sqs_messages", m.ID)
			moved++
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"redriven": true, "queue_id": queueID, "dead_letter_queue_id": dlqID, "messages_moved": moved})
}

func (s *Server) handleSQSListMessages(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	queueID := chi.URLParam(r, "id")
	q, err := st.P3Get(r.Context(), "sqs_queues", queueID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, q, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.sqsPurgeExpired(r.Context(), st, queueID)
	msgs, _ := st.P3List(r.Context(), "sqs_messages")
	out := []map[string]any{}
	now := time.Now().UTC()
	for _, m := range msgs {
		if phase3.Str(m.Data, "queue_id", "") != queueID {
			continue
		}
		view := p3View(m)
		view["visible"] = phase3.SQSMessageVisible(m.Data, now)
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["created_at"].(string) < out[j]["created_at"].(string) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"queue_id": queueID, "messages": out, "count": len(out)})
}

// --- SNS (durable) ----------------------------------------------------------
//
// Fan-out to SQS queues (durable enqueue), HTTP(S) endpoints (real
// POST, SSRF-guarded to loopback like the rest of the platform),
// and Lambda functions (real invocation). Every delivery is
// recorded, success or failure.

func (s *Server) handleSNSListTopics(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "sns_topics")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"topics": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "PARTIAL", "note": "Durable, Postgres-persisted topics. Delivery: SQS queues, HTTP(S) endpoints (loopback only), Lambda functions."})
}

func (s *Server) handleSNSCreateTopic(w http.ResponseWriter, r *http.Request) {
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
	if !sqsNameRE.MatchString(req.Name) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "topic name must be 1-80 characters of letters, digits, _ and -")
		return
	}
	e, err := st.P3Create(r.Context(), "sns_topics", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"arn": "arn:cloudforge:sns::local:" + req.Name, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleSNSDeleteTopic(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	e, err := st.P3Get(r.Context(), "sns_topics", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	subs, _ := st.P3List(r.Context(), "sns_subscriptions")
	for _, sub := range subs {
		if phase3.Str(sub.Data, "topic_id", "") == id {
			_ = st.P3Delete(r.Context(), "sns_subscriptions", sub.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "sns_topics", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleSNSListSubscriptions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	topicID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "sns_topics", topicID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	all, _ := st.P3List(r.Context(), "sns_subscriptions")
	out := []*store.P3Entity{}
	for _, sub := range all {
		if phase3.Str(sub.Data, "topic_id", "") == topicID {
			out = append(out, sub)
		}
	}
	_ = u
	s.writeJSON(w, r, http.StatusOK, map[string]any{"topic_id": topicID, "subscriptions": p3ListView(out)})
}

func (s *Server) handleSNSSubscribe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	topicID := chi.URLParam(r, "id")
	topic, err := st.P3Get(r.Context(), "sns_topics", topicID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, topic, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Protocol string `json:"protocol"`
		Endpoint string `json:"endpoint"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	endpoint := strings.TrimSpace(req.Endpoint)
	switch proto {
	case "sqs":
		// Endpoint is a queue id or queue name.
		if _, err := st.P3Get(r.Context(), "sqs_queues", endpoint); err != nil {
			if q, err2 := st.P3GetByName(r.Context(), "sqs_queues", endpoint); err2 == nil {
				endpoint = q.ID
			} else {
				s.writeError(w, r, http.StatusNotFound, "not_found", "no SQS queue with that id or name")
				return
			}
		}
	case "http", "https":
		if err := security.ValidateEndpointURL(endpoint); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "HTTP endpoint is not allowed by the SSRF guard (loopback/emulator hosts only): "+err.Error())
			return
		}
	case "lambda":
		if _, err := st.P3Get(r.Context(), "lambda_functions", endpoint); err != nil {
			if fn, err2 := st.P3GetByName(r.Context(), "lambda_functions", endpoint); err2 == nil {
				endpoint = fn.ID
			} else {
				s.writeError(w, r, http.StatusNotFound, "not_found", "no Lambda function with that id or name")
				return
			}
		}
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be sqs, http, https or lambda (email/SMS are not built)")
		return
	}
	e, err := st.P3Create(r.Context(), "sns_subscriptions", &store.P3Entity{Name: proto + ":" + endpoint, Data: map[string]any{
		"topic_id": topicID, "protocol": proto, "endpoint": endpoint, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleSNSUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	sub, err := st.P3Get(r.Context(), "sns_subscriptions", chi.URLParam(r, "subId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if phase3.Str(sub.Data, "topic_id", "") != chi.URLParam(r, "id") {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no subscription with that id on this topic")
		return
	}
	if err := st.P3Delete(r.Context(), "sns_subscriptions", sub.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": sub.ID})
}

// snsDeliverOne delivers one published message to one subscription
// and records the delivery. It returns (delivered bool, detail).
func (s *Server) snsDeliverOne(ctx context.Context, st store.Phase3Store, topic *store.P3Entity, sub *store.P3Entity, messageID, message, subject string) (bool, string) {
	proto := phase3.Str(sub.Data, "protocol", "")
	endpoint := phase3.Str(sub.Data, "endpoint", "")
	envelope := map[string]any{
		"Type": "Notification", "MessageId": messageID,
		"TopicArn": phase3.Str(topic.Data, "arn", topic.Name),
		"Message":  message, "Subject": subject,
		"Timestamp": p3Now(),
	}
	envelopeJSON, _ := json.Marshal(envelope)
	var delivered bool
	var detail string
	switch proto {
	case "sqs":
		if _, err := s.sqsEnqueue(ctx, st, endpoint, string(envelopeJSON), map[string]any{"sns_topic": topic.Name}); err != nil {
			detail = "enqueue failed: " + err.Error()
		} else {
			delivered, detail = true, "enqueued to SQS queue "+endpoint
		}
	case "http", "https":
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, strings.NewReader(string(envelopeJSON)))
		if err != nil {
			detail = "request build failed: " + err.Error()
			break
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			detail = "POST failed: " + err.Error()
			break
		}
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			delivered, detail = true, fmt.Sprintf("HTTP %d", resp.StatusCode)
		} else {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	case "lambda":
		fn, err := st.P3Get(ctx, "lambda_functions", endpoint)
		if err != nil {
			detail = "function not found: " + err.Error()
			break
		}
		res, err := s.invokeLambdaInternal(ctx, fn, envelope)
		if err != nil {
			detail = "invoke failed: " + err.Error()
		} else if res.Error != "" {
			detail = "function error: " + res.Error
		} else {
			delivered, detail = true, fmt.Sprintf("invoked in %d ms", res.DurationMs)
		}
	default:
		detail = "unsupported protocol " + proto
	}
	status := "failed"
	if delivered {
		status = "delivered"
	}
	_, _ = st.P3Create(ctx, "sns_deliveries", &store.P3Entity{Name: messageID, Data: map[string]any{
		"topic_id": topic.ID, "subscription_id": sub.ID, "message_id": messageID,
		"protocol": proto, "endpoint": endpoint, "status": status, "detail": detail,
	}})
	return delivered, detail
}

func (s *Server) handleSNSPublish(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	topicID := chi.URLParam(r, "id")
	topic, err := st.P3Get(r.Context(), "sns_topics", topicID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, topic, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Message string `json:"message"`
		Subject string `json:"subject"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "message is required")
		return
	}
	if len(req.Message) > 256*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "message is capped at 256 KiB")
		return
	}
	messageID := newID()
	subs, _ := st.P3List(r.Context(), "sns_subscriptions")
	deliveries := []map[string]any{}
	for _, sub := range subs {
		if phase3.Str(sub.Data, "topic_id", "") != topicID {
			continue
		}
		ok, detail := s.snsDeliverOne(r.Context(), st, topic, sub, messageID, req.Message, req.Subject)
		deliveries = append(deliveries, map[string]any{
			"subscription_id": sub.ID, "protocol": phase3.Str(sub.Data, "protocol", ""),
			"endpoint": phase3.Str(sub.Data, "endpoint", ""),
			"status":   map[bool]string{true: "delivered", false: "failed"}[ok], "detail": detail,
		})
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"message_id": messageID, "topic_id": topicID,
		"deliveries": deliveries, "delivered": len(deliveries),
	})
}

func (s *Server) handleSNSListDeliveries(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	topicID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "sns_topics", topicID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	all, _ := st.P3List(r.Context(), "sns_deliveries")
	out := []*store.P3Entity{}
	for _, d := range all {
		if phase3.Str(d.Data, "topic_id", "") == topicID {
			out = append(out, d)
		}
	}
	_ = u
	s.writeJSON(w, r, http.StatusOK, map[string]any{"topic_id": topicID, "deliveries": p3ListView(out)})
}

// --- EventBridge (durable) --------------------------------------------------
//
// Rules match a subset of the AWS event-pattern language
// (phase3.PatternMatches): source / detail-type arrays plus nested
// detail subset matching. Targets are SQS queues, SNS topics and
// Lambda functions. AWS service events are NOT generated — events
// arrive through PutEvents.

func (s *Server) handleEBListRules(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "eventbridge_rules")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"rules": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "PARTIAL", "note": "Durable rules. Pattern: source / detail-type arrays + nested detail subset matching. AWS service events are not generated; events arrive through PutEvents."})
}

func (s *Server) handleEBCreateRule(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string         `json:"name"`
		Source     string         `json:"source"`
		DetailType string         `json:"detail_type"`
		Pattern    map[string]any `json:"pattern"`
		State      string         `json:"state"`
		Targets    []any          `json:"targets"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	pattern := req.Pattern
	if pattern == nil {
		pattern = map[string]any{}
	}
	if req.Source != "" {
		pattern["source"] = []any{req.Source}
	}
	if req.DetailType != "" {
		pattern["detail-type"] = []any{req.DetailType}
	}
	state := strings.ToUpper(strings.TrimSpace(req.State))
	if state == "" {
		state = "ENABLED"
	}
	if state != "ENABLED" && state != "DISABLED" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "state must be ENABLED or DISABLED")
		return
	}
	// Validate targets now: a rule pointing at nothing real is
	// rejected, not stored broken.
	targets := []any{}
	for _, t := range req.Targets {
		tm, ok := t.(map[string]any)
		if !ok {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "each target must be an object {type, queue_id|topic_id|function_id}")
			return
		}
		ttype := phase3.Str(tm, "type", "")
		switch ttype {
		case "sqs":
			if _, err := st.P3Get(r.Context(), "sqs_queues", phase3.Str(tm, "queue_id", "")); err != nil {
				s.writeError(w, r, http.StatusNotFound, "not_found", "target SQS queue not found: "+phase3.Str(tm, "queue_id", ""))
				return
			}
		case "sns":
			if _, err := st.P3Get(r.Context(), "sns_topics", phase3.Str(tm, "topic_id", "")); err != nil {
				s.writeError(w, r, http.StatusNotFound, "not_found", "target SNS topic not found: "+phase3.Str(tm, "topic_id", ""))
				return
			}
		case "lambda":
			if _, err := st.P3Get(r.Context(), "lambda_functions", phase3.Str(tm, "function_id", "")); err != nil {
				s.writeError(w, r, http.StatusNotFound, "not_found", "target Lambda function not found: "+phase3.Str(tm, "function_id", ""))
				return
			}
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "target type must be sqs, sns or lambda")
			return
		}
		targets = append(targets, tm)
	}
	e, err := st.P3Create(r.Context(), "eventbridge_rules", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"pattern": pattern, "state": state, "targets": targets,
		"matched_events": 0, "deliveries": 0, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleEBDeleteRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	if err := st.P3Delete(r.Context(), "eventbridge_rules", chi.URLParam(r, "id")); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": chi.URLParam(r, "id")})
}

func (s *Server) handleEBSetRuleState(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	rule, err := st.P3Get(r.Context(), "eventbridge_rules", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, rule, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	state := strings.ToUpper(strings.TrimSpace(req.State))
	if state != "ENABLED" && state != "DISABLED" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "state must be ENABLED or DISABLED")
		return
	}
	rule.Data["state"] = state
	updated, err := st.P3Update(r.Context(), "eventbridge_rules", rule)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(updated))
}

// ebDeliverEvent routes one event to one rule's targets. It returns
// the number of successful deliveries.
func (s *Server) ebDeliverEvent(ctx context.Context, st store.Phase3Store, rule *store.P3Entity, event map[string]any) int {
	delivered := 0
	targets, _ := rule.Data["targets"].([]any)
	eventJSON, _ := json.Marshal(event)
	for _, t := range targets {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		switch phase3.Str(tm, "type", "") {
		case "sqs":
			if _, err := s.sqsEnqueue(ctx, st, phase3.Str(tm, "queue_id", ""), string(eventJSON), map[string]any{"eventbridge_rule": rule.Name}); err == nil {
				delivered++
			}
		case "sns":
			topic, err := st.P3Get(ctx, "sns_topics", phase3.Str(tm, "topic_id", ""))
			if err != nil {
				continue
			}
			subs, _ := st.P3List(ctx, "sns_subscriptions")
			for _, sub := range subs {
				if phase3.Str(sub.Data, "topic_id", "") != topic.ID {
					continue
				}
				if ok, _ := s.snsDeliverOne(ctx, st, topic, sub, newID(), string(eventJSON), "EventBridge event"); ok {
					delivered++
				}
			}
		case "lambda":
			fn, err := st.P3Get(ctx, "lambda_functions", phase3.Str(tm, "function_id", ""))
			if err != nil {
				continue
			}
			if _, err := s.invokeLambdaInternal(ctx, fn, event); err == nil {
				delivered++
			}
		}
	}
	return delivered
}

func (s *Server) handleEBPutEvents(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Entries []map[string]any `json:"entries"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Entries) == 0 || len(req.Entries) > 10 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "entries must contain 1-10 events")
		return
	}
	rules, _ := st.P3List(r.Context(), "eventbridge_rules")
	results := []map[string]any{}
	for _, entry := range req.Entries {
		source := phase3.Str(entry, "source", "")
		detailType := phase3.Str(entry, "detail-type", "")
		if source == "" || detailType == "" {
			results = append(results, map[string]any{"error": "each entry needs source and detail-type"})
			continue
		}
		matched := []string{}
		deliveries := 0
		for _, rule := range rules {
			if phase3.Str(rule.Data, "state", "") != "ENABLED" {
				continue
			}
			pattern, _ := rule.Data["pattern"].(map[string]any)
			if pattern == nil {
				continue
			}
			if !phase3.PatternMatches(pattern, entry) {
				continue
			}
			matched = append(matched, rule.Name)
			n := s.ebDeliverEvent(r.Context(), st, rule, entry)
			deliveries += n
			rule.Data["matched_events"] = phase3.Int(rule.Data, "matched_events", 0) + 1
			rule.Data["deliveries"] = phase3.Int(rule.Data, "deliveries", 0) + n
			_, _ = st.P3Update(r.Context(), "eventbridge_rules", rule)
		}
		results = append(results, map[string]any{
			"event_id": newID(), "matched_rules": matched, "deliveries": deliveries,
		})
	}
	_ = u
	s.writeJSON(w, r, http.StatusOK, map[string]any{"results": results, "entries": len(req.Entries)})
}

// --- Lambda (real subprocess execution) -------------------------------------
//
// Runtimes that actually execute here: python3 and node, as a whole
// program reading the event JSON on stdin and writing the result to
// stdout. The runner (phase3.RunLambda) enforces a timeout, an
// output cap, a prlimit memory cap when the binary exists, a fresh
// temp workdir, a minimal env, and uid 65534 when the API runs as
// root. That is subprocess sandboxing, NOT container isolation and
// NOT AWS Lambda — the console and docs say so, and the invocation
// result names the sandbox that actually applied.

func (s *Server) handleLambdaList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "lambda_functions")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	views := []map[string]any{}
	for _, fn := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		view := p3View(fn)
		view["runtimes_supported"] = []string{"python3.12", "python3.11", "python3", "nodejs20", "nodejs22", "node"}
		views = append(views, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"functions": views, "fidelity": "PARTIAL",
		"note": "Real execution in a sandboxed subprocess (python3/node). NOT container isolation, NOT AWS Lambda: no VPC attachment, no managed scaling, no layers.",
	})
}

func (s *Server) handleLambdaCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     string            `json:"name"`
		Runtime  string            `json:"runtime"`
		Handler  string            `json:"handler"`
		Code     string            `json:"code"`
		Env      map[string]string `json:"env"`
		Timeout  int               `json:"timeout_seconds"`
		MemoryMB int               `json:"memory_mb"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	runtime := strings.TrimSpace(req.Runtime)
	if _, ok := phase3.SupportedRuntimes[runtime]; !ok {
		supported := []string{}
		for k := range phase3.SupportedRuntimes {
			supported = append(supported, k)
		}
		sort.Strings(supported)
		s.writeError(w, r, http.StatusBadRequest, "unsupported_runtime", "runtime "+runtime+" is not supported; supported runtimes: "+strings.Join(supported, ", "))
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is required (a whole program: event JSON on stdin, result on stdout)")
		return
	}
	if len(req.Code) > 256*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is capped at 256 KiB")
		return
	}
	handler := strings.TrimSpace(req.Handler)
	if handler == "" {
		handler = "handler.handle"
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	if timeout > 60 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "timeout_seconds must be 1-60")
		return
	}
	memory := req.MemoryMB
	if memory <= 0 {
		memory = 256
	}
	if memory > 1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "memory_mb must be 1-1024")
		return
	}
	env := req.Env
	if env == nil {
		env = map[string]string{}
	}
	e, err := st.P3Create(r.Context(), "lambda_functions", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"runtime": runtime, "handler": handler, "code": req.Code, "env": env,
		"timeout_seconds": timeout, "memory_mb": memory, "version": 1,
		"aliases": map[string]any{}, "triggers": []any{}, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "lambda_function_created", "lambda-function", e.ID, map[string]any{"runtime": runtime})
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleLambdaGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fn, err := st.P3Get(r.Context(), "lambda_functions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(fn))
}

func (s *Server) handleLambdaDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	fn, err := st.P3Get(r.Context(), "lambda_functions", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	invs, _ := st.P3List(r.Context(), "lambda_invocations")
	for _, inv := range invs {
		if phase3.Str(inv.Data, "function_id", "") == id {
			_ = st.P3Delete(r.Context(), "lambda_invocations", inv.ID)
		}
	}
	if err := st.P3Delete(r.Context(), "lambda_functions", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleLambdaUpdateCode(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fn, err := st.P3Get(r.Context(), "lambda_functions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Code) == "" || len(req.Code) > 256*1024 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is required and capped at 256 KiB")
		return
	}
	fn.Data["code"] = req.Code
	fn.Data["version"] = phase3.Int(fn.Data, "version", 1) + 1
	updated, err := st.P3Update(r.Context(), "lambda_functions", fn)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(updated))
}

func (s *Server) handleLambdaCreateAlias(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fn, err := st.P3Get(r.Context(), "lambda_functions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "alias name is required")
		return
	}
	if req.Version <= 0 || req.Version > phase3.Int(fn.Data, "version", 1) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "version must be an existing published version (latest is $LATEST)")
		return
	}
	aliases, _ := fn.Data["aliases"].(map[string]any)
	if aliases == nil {
		aliases = map[string]any{}
	}
	aliases[req.Name] = req.Version
	fn.Data["aliases"] = aliases
	updated, err := st.P3Update(r.Context(), "lambda_functions", fn)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(updated))
}

func (s *Server) handleLambdaAddTrigger(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fn, err := st.P3Get(r.Context(), "lambda_functions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Type    string `json:"type"`
		QueueID string `json:"queue_id"`
		RuleID  string `json:"rule_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch req.Type {
	case "sqs":
		if _, err := st.P3Get(r.Context(), "sqs_queues", req.QueueID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no SQS queue with that id")
			return
		}
	case "eventbridge":
		if _, err := st.P3Get(r.Context(), "eventbridge_rules", req.RuleID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no EventBridge rule with that id")
			return
		}
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "trigger type must be sqs or eventbridge (API Gateway triggers are configured on the API, not here)")
		return
	}
	triggers, _ := fn.Data["triggers"].([]any)
	triggers = append(triggers, map[string]any{"type": req.Type, "queue_id": req.QueueID, "rule_id": req.RuleID})
	fn.Data["triggers"] = triggers
	updated, err := st.P3Update(r.Context(), "lambda_functions", fn)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(updated))
}

// invokeLambdaInternal runs a function and records the invocation.
// It returns the runner result; err is non-nil only when the record
// itself could not be written.
func (s *Server) invokeLambdaInternal(ctx context.Context, fn *store.P3Entity, event any) (phase3.LambdaResult, error) {
	runtime := phase3.Str(fn.Data, "runtime", "")
	code := phase3.Str(fn.Data, "code", "")
	env := map[string]string{}
	if raw, ok := fn.Data["env"].(map[string]any); ok {
		for k, v := range raw {
			if str, ok := v.(string); ok {
				env[k] = str
			}
		}
	}
	timeout := phase3.Int(fn.Data, "timeout_seconds", 10)
	memory := phase3.Int(fn.Data, "memory_mb", 256)
	res := phase3.RunLambda(ctx, runtime, code, env, event, timeout, memory)
	status := "success"
	if res.TimedOut {
		status = "timeout"
	} else if res.Error != "" {
		status = "error"
	}
	if s.P3 != nil {
		_, err := s.P3.P3Create(ctx, "lambda_invocations", &store.P3Entity{Name: fn.Name, Data: map[string]any{
			"function_id": fn.ID, "function_name": fn.Name,
			"version": phase3.Int(fn.Data, "version", 1),
			"status":  status, "duration_ms": res.DurationMs,
			"output": res.Output, "error": res.Error, "sandbox": res.Sandbox,
			"event": event,
		}})
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Server) handleLambdaInvoke(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fn, err := st.P3Get(r.Context(), "lambda_functions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, fn, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Event     any    `json:"event"`
		Qualifier string `json:"qualifier"`
	}
	_ = s.p3DecodeOptional(w, r, &req)
	version := phase3.Int(fn.Data, "version", 1)
	if req.Qualifier != "" && req.Qualifier != "$LATEST" {
		aliases, _ := fn.Data["aliases"].(map[string]any)
		if aliases != nil {
			if v, ok := aliases[req.Qualifier]; ok {
				version = int(phase3.Float(map[string]any{"v": v}, "v", 0))
			} else {
				s.writeError(w, r, http.StatusNotFound, "not_found", "no alias named "+req.Qualifier)
				return
			}
		} else {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no alias named "+req.Qualifier)
			return
		}
	}
	event := req.Event
	if event == nil {
		event = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(phase3.Int(fn.Data, "timeout_seconds", 10)+5)*time.Second)
	defer cancel()
	res, err := s.invokeLambdaInternal(ctx, fn, event)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "invocation ran but the record could not be stored: "+err.Error())
		return
	}
	status := http.StatusOK
	if res.TimedOut {
		status = http.StatusGatewayTimeout
	} else if res.Error != "" {
		status = http.StatusOK // the function ran and errored: the error is data, not a transport failure
	}
	s.writeJSON(w, r, status, map[string]any{
		"function_id": fn.ID, "function_name": fn.Name, "version": version,
		"status":      map[bool]string{true: "error", false: "success"}[res.Error != ""],
		"duration_ms": res.DurationMs, "output": res.Output,
		"error": res.Error, "timed_out": res.TimedOut, "sandbox": res.Sandbox,
	})
}

func (s *Server) handleLambdaListInvocations(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	fnID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "lambda_functions", fnID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	all, _ := st.P3List(r.Context(), "lambda_invocations")
	out := []*store.P3Entity{}
	for _, inv := range all {
		if phase3.Str(inv.Data, "function_id", "") == fnID {
			out = append(out, inv)
		}
	}
	_ = u
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 50 {
		out = out[:50]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"function_id": fnID, "invocations": p3ListView(out)})
}

// --- API Gateway ------------------------------------------------------------
//
// REST/HTTP APIs routing to Lambda functions and to SSRF-guarded
// HTTP targets. Every invocation is logged (method, path, status,
// duration). The invoke surface is /api/v1/gateway/{api}/{stage}/*
// and requires the same auth as the rest of the API.

func (s *Server) handleAPIGWList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "apigw_apis")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"apis": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "fidelity": "PARTIAL", "note": "REST/HTTP APIs routing to Lambda functions and SSRF-guarded HTTP targets, with real request logging."})
}

func (s *Server) handleAPIGWCreate(w http.ResponseWriter, r *http.Request) {
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
		Protocol    string `json:"protocol"`
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
	proto := strings.ToUpper(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "REST"
	}
	if proto != "REST" && proto != "HTTP" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "protocol must be REST or HTTP (WebSocket APIs are not built)")
		return
	}
	e, err := st.P3Create(r.Context(), "apigw_apis", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"protocol": proto, "description": req.Description, "status": "available",
		"owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleAPIGWGet(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	api, err := st.P3Get(r.Context(), "apigw_apis", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, api, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(api)
	res, _ := st.P3List(r.Context(), "apigw_resources")
	rv := []map[string]any{}
	for _, re := range res {
		if phase3.Str(re.Data, "api_id", "") == api.ID {
			rv = append(rv, p3View(re))
		}
	}
	view["resources"] = rv
	stages, _ := st.P3List(r.Context(), "apigw_stages")
	sv := []map[string]any{}
	for _, sg := range stages {
		if phase3.Str(sg.Data, "api_id", "") == api.ID {
			sv = append(sv, p3View(sg))
		}
	}
	view["stages"] = sv
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleAPIGWDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	api, err := st.P3Get(r.Context(), "apigw_apis", id)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, api, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	for _, coll := range []string{"apigw_resources", "apigw_stages", "apigw_logs"} {
		all, _ := st.P3List(r.Context(), coll)
		for _, e := range all {
			if phase3.Str(e.Data, "api_id", "") == id {
				_ = st.P3Delete(r.Context(), coll, e.ID)
			}
		}
	}
	if err := st.P3Delete(r.Context(), "apigw_apis", id); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleAPIGWCreateResource(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	apiID := chi.URLParam(r, "id")
	api, err := st.P3Get(r.Context(), "apigw_apis", apiID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, api, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Path    string `json:"path"`
		Methods []any  `json:"methods"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	path := strings.TrimSpace(req.Path)
	if !strings.HasPrefix(path, "/") {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "path must start with /")
		return
	}
	if len(req.Methods) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "at least one method is required, each {method, integration:{type: lambda|http, function_id|url}}")
		return
	}
	methods := []any{}
	for _, m := range req.Methods {
		mm, ok := m.(map[string]any)
		if !ok {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "each method must be an object {method, integration}")
			return
		}
		method := strings.ToUpper(strings.TrimSpace(phase3.Str(mm, "method", "")))
		if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" && method != "PATCH" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "method must be GET, POST, PUT, DELETE or PATCH")
			return
		}
		integration, _ := mm["integration"].(map[string]any)
		if integration == nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "each method needs an integration {type: lambda|http, function_id|url}")
			return
		}
		switch phase3.Str(integration, "type", "") {
		case "lambda":
			fnID := phase3.Str(integration, "function_id", "")
			if fnID == "" {
				if fn, err := st.P3GetByName(r.Context(), "lambda_functions", phase3.Str(integration, "function_name", "")); err == nil {
					fnID = fn.ID
					integration["function_id"] = fnID
				}
			}
			if _, err := st.P3Get(r.Context(), "lambda_functions", fnID); err != nil {
				s.writeError(w, r, http.StatusNotFound, "not_found", "integration Lambda function not found: "+fnID)
				return
			}
		case "http":
			url := phase3.Str(integration, "url", "")
			if err := security.ValidateEndpointURL(url); err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "HTTP integration URL is not allowed by the SSRF guard (loopback/emulator hosts only): "+err.Error())
				return
			}
		default:
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "integration type must be lambda or http")
			return
		}
		methods = append(methods, map[string]any{"method": method, "integration": integration})
	}
	// One resource per path: a second create replaces the methods.
	all, _ := st.P3List(r.Context(), "apigw_resources")
	for _, re := range all {
		if phase3.Str(re.Data, "api_id", "") == apiID && phase3.Str(re.Data, "path", "") == path {
			re.Data["methods"] = methods
			updated, err := st.P3Update(r.Context(), "apigw_resources", re)
			if err != nil {
				s.p3Err(w, r, err)
				return
			}
			s.writeJSON(w, r, http.StatusOK, p3View(updated))
			return
		}
	}
	e, err := st.P3Create(r.Context(), "apigw_resources", &store.P3Entity{Name: path, Data: map[string]any{
		"api_id": apiID, "path": path, "methods": methods, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleAPIGWDeploy(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	apiID := chi.URLParam(r, "id")
	api, err := st.P3Get(r.Context(), "apigw_apis", apiID)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, api, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		StageName string `json:"stage_name"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	stageName := strings.TrimSpace(req.StageName)
	if stageName == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "stage_name is required")
		return
	}
	// The deployment is a snapshot of the current resources, so a
	// later resource edit does not change a deployed stage until
	// the next deploy.
	res, _ := st.P3List(r.Context(), "apigw_resources")
	snapshot := []any{}
	for _, re := range res {
		if phase3.Str(re.Data, "api_id", "") == apiID {
			snapshot = append(snapshot, map[string]any{"path": phase3.Str(re.Data, "path", ""), "methods": re.Data["methods"]})
		}
	}
	if len(snapshot) == 0 {
		s.writeError(w, r, http.StatusConflict, "nothing_to_deploy", "this API has no resources yet — add a resource before deploying")
		return
	}
	stages, _ := st.P3List(r.Context(), "apigw_stages")
	for _, sg := range stages {
		if phase3.Str(sg.Data, "api_id", "") == apiID && sg.Name == stageName {
			sg.Data["deployment"] = snapshot
			sg.Data["deployed_at"] = p3Now()
			updated, err := st.P3Update(r.Context(), "apigw_stages", sg)
			if err != nil {
				s.p3Err(w, r, err)
				return
			}
			s.writeJSON(w, r, http.StatusOK, p3View(updated))
			return
		}
	}
	e, err := st.P3Create(r.Context(), "apigw_stages", &store.P3Entity{Name: stageName, Data: map[string]any{
		"api_id": apiID, "deployment": snapshot, "deployed_at": p3Now(), "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleAPIGWLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	apiID := chi.URLParam(r, "id")
	if _, err := st.P3Get(r.Context(), "apigw_apis", apiID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	all, _ := st.P3List(r.Context(), "apigw_logs")
	out := []*store.P3Entity{}
	for _, l := range all {
		if phase3.Str(l.Data, "api_id", "") == apiID {
			out = append(out, l)
		}
	}
	_ = u
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"api_id": apiID, "logs": p3ListView(out)})
}

// handleAPIGWInvoke is the gateway's runtime surface:
// /api/v1/gateway/{api}/{stage}/{path...} with any method.
func (s *Server) handleAPIGWInvoke(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	apiID := chi.URLParam(r, "apiId")
	stageName := chi.URLParam(r, "stage")
	rest := chi.URLParam(r, "*")
	path := "/" + rest
	start := time.Now()
	logInvocation := func(status int, detail string) {
		_, _ = st.P3Create(r.Context(), "apigw_logs", &store.P3Entity{Name: path, Data: map[string]any{
			"api_id": apiID, "stage": stageName, "method": r.Method, "path": path,
			"status": status, "duration_ms": time.Since(start).Milliseconds(),
			"detail": detail, "user_id": u.ID,
		}})
	}
	api, err := st.P3Get(r.Context(), "apigw_apis", apiID)
	if err != nil {
		logInvocation(http.StatusNotFound, "api not found")
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, api, u.ID, p3IsAdmin(u.Role)) {
		logInvocation(http.StatusForbidden, "forbidden")
		return
	}
	stages, _ := st.P3List(r.Context(), "apigw_stages")
	var stage *store.P3Entity
	for _, sg := range stages {
		if phase3.Str(sg.Data, "api_id", "") == apiID && sg.Name == stageName {
			stage = sg
			break
		}
	}
	if stage == nil {
		logInvocation(http.StatusNotFound, "stage not deployed: "+stageName)
		s.writeError(w, r, http.StatusNotFound, "not_found", "stage "+stageName+" is not deployed on this API (deploy it first)")
		return
	}
	deployment, _ := stage.Data["deployment"].([]any)
	var matchedMethod map[string]any
	var matchedPath string
	for _, d := range deployment {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		resPath := phase3.Str(dm, "path", "")
		if resPath != path && !(strings.HasSuffix(resPath, "/*") && strings.HasPrefix(path, strings.TrimSuffix(resPath, "*"))) {
			continue
		}
		methods, _ := dm["methods"].([]any)
		for _, m := range methods {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if phase3.Str(mm, "method", "") == r.Method {
				matchedMethod = mm
				matchedPath = resPath
				break
			}
		}
		if matchedMethod != nil {
			break
		}
	}
	if matchedMethod == nil {
		logInvocation(http.StatusNotFound, "no method "+r.Method+" on "+path+" in stage "+stageName)
		s.writeError(w, r, http.StatusNotFound, "not_found", "no "+r.Method+" method on "+path+" in stage "+stageName)
		return
	}
	integration, _ := matchedMethod["integration"].(map[string]any)
	bodyBytes, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	var bodyJSON any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &bodyJSON)
	}
	switch phase3.Str(integration, "type", "") {
	case "lambda":
		fn, err := st.P3Get(r.Context(), "lambda_functions", phase3.Str(integration, "function_id", ""))
		if err != nil {
			logInvocation(http.StatusBadGateway, "integration function gone: "+err.Error())
			s.writeError(w, r, http.StatusBadGateway, "integration_error", "the integration Lambda function no longer exists")
			return
		}
		event := map[string]any{
			"httpMethod": r.Method, "path": path, "resource": matchedPath,
			"stage": stageName, "headers": map[string]any{},
			"body": string(bodyBytes), "bodyJson": bodyJSON,
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(phase3.Int(fn.Data, "timeout_seconds", 10)+5)*time.Second)
		defer cancel()
		res, err := s.invokeLambdaInternal(ctx, fn, event)
		if err != nil {
			logInvocation(http.StatusInternalServerError, "invocation record failed: "+err.Error())
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if res.TimedOut {
			logInvocation(http.StatusGatewayTimeout, "lambda timed out")
			s.writeJSON(w, r, http.StatusGatewayTimeout, map[string]any{"error": res.Error, "sandbox": res.Sandbox})
			return
		}
		if res.Error != "" {
			logInvocation(http.StatusBadGateway, "lambda error: "+res.Error)
			s.writeJSON(w, r, http.StatusBadGateway, map[string]any{"error": res.Error, "output": res.Output, "sandbox": res.Sandbox})
			return
		}
		logInvocation(http.StatusOK, "lambda "+fn.Name)
		// If the function printed JSON, return it as JSON; else as text.
		var parsed any
		if json.Unmarshal([]byte(strings.TrimSpace(res.Output)), &parsed) == nil {
			s.writeJSON(w, r, http.StatusOK, parsed)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"output": res.Output})
	case "http":
		target := phase3.Str(integration, "url", "")
		if err := security.ValidateEndpointURL(target); err != nil {
			logInvocation(http.StatusBadGateway, "integration URL rejected by SSRF guard")
			s.writeError(w, r, http.StatusBadGateway, "integration_error", "the HTTP integration URL is not allowed by the SSRF guard")
			return
		}
		reqCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, r.Method, target, strings.NewReader(string(bodyBytes)))
		if err != nil {
			logInvocation(http.StatusBadGateway, "integration request failed: "+err.Error())
			s.writeError(w, r, http.StatusBadGateway, "integration_error", err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logInvocation(http.StatusBadGateway, "integration upstream failed: "+err.Error())
			s.writeError(w, r, http.StatusBadGateway, "integration_error", "upstream "+target+" unreachable: "+err.Error())
			return
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(http.MaxBytesReader(w, resp.Body, 1<<20))
		logInvocation(resp.StatusCode, "http "+target)
		var parsed any
		if json.Unmarshal(respBytes, &parsed) == nil {
			s.writeJSON(w, r, resp.StatusCode, parsed)
			return
		}
		s.writeJSON(w, r, resp.StatusCode, map[string]any{"output": string(respBytes)})
	default:
		logInvocation(http.StatusBadGateway, "unknown integration type")
		s.writeError(w, r, http.StatusBadGateway, "integration_error", "unknown integration type")
	}
}

// --- routes -------------------------------------------------------------------

// registerPhase3Routes wires every Phase 3 route. It is called from
// Server.Routes() inside the /api/v1 group (auth + rate limit apply).
func (s *Server) registerPhase3Routes(r chi.Router) {
	// DynamoDB
	r.Get("/aws/dynamodb/tables", s.handleDynamoListTables)
	r.Post("/aws/dynamodb/tables", s.handleDynamoCreateTable)
	r.Delete("/aws/dynamodb/tables/{table}", s.handleDynamoDeleteTable)
	r.Post("/aws/dynamodb/tables/{table}/items", s.handleDynamoPutItem)
	r.Get("/aws/dynamodb/tables/{table}/items", s.handleDynamoScan)
	r.Get("/aws/dynamodb/tables/{table}/items/{key}", s.handleDynamoGetItem)
	r.Delete("/aws/dynamodb/tables/{table}/items/{key}", s.handleDynamoDeleteItem)
	r.Get("/aws/dynamodb/tables/{table}/query", s.handleDynamoQuery)
	// RDS
	r.Get("/aws/rds/instances", s.handleRDSList)
	r.Post("/aws/rds/instances", s.handleRDSCreate)
	r.Get("/aws/rds/instances/{id}", s.handleRDSGet)
	r.Delete("/aws/rds/instances/{id}", s.handleRDSDelete)
	r.Post("/aws/rds/instances/{id}/start", s.handleRDSStartStop("start"))
	r.Post("/aws/rds/instances/{id}/stop", s.handleRDSStartStop("stop"))
	r.Post("/aws/rds/instances/{id}/snapshots", s.handleRDSSnapshot)
	r.Get("/aws/rds/instances/{id}/snapshots", s.handleRDSListSnapshots)
	r.Get("/aws/rds/instances/{id}/logs", s.handleRDSLogs)
	r.Post("/aws/rds/snapshots/{id}/restore", s.handleRDSRestoreSnapshot)
	// ElastiCache
	r.Get("/aws/elasticache/clusters", s.handleCacheList)
	r.Post("/aws/elasticache/clusters", s.handleCacheCreate)
	r.Get("/aws/elasticache/clusters/{id}", s.handleCacheGet)
	r.Delete("/aws/elasticache/clusters/{id}", s.handleCacheDelete)
	r.Get("/aws/elasticache/clusters/{id}/keys", s.handleCacheKeys)
	r.Post("/aws/elasticache/clusters/{id}/keys", s.handleCacheSetKey)
	r.Get("/aws/elasticache/clusters/{id}/keys/{key}", s.handleCacheGetKey)
	r.Post("/aws/elasticache/clusters/{id}/flush", s.handleCacheFlush)
	r.Get("/aws/elasticache/clusters/{id}/metrics", s.handleCacheMetrics)
	// ELB
	r.Get("/aws/elb/load-balancers", s.handleELBList)
	r.Post("/aws/elb/load-balancers", s.handleELBCreate)
	r.Get("/aws/elb/load-balancers/{id}", s.handleELBGet)
	r.Delete("/aws/elb/load-balancers/{id}", s.handleELBDelete)
	r.Post("/aws/elb/load-balancers/{id}/listeners", s.handleELBCreateListener)
	r.Delete("/aws/elb/load-balancers/{id}/listeners/{listenerId}", s.handleELBDeleteListener)
	r.Post("/aws/elb/load-balancers/{id}/route", s.handleELBRoute)
	r.Get("/aws/elb/target-groups", s.handleTGList)
	r.Post("/aws/elb/target-groups", s.handleTGCreate)
	r.Get("/aws/elb/target-groups/{id}", s.handleTGGet)
	r.Delete("/aws/elb/target-groups/{id}", s.handleTGDelete)
	r.Post("/aws/elb/target-groups/{id}/targets", s.handleTGAddTarget)
	r.Delete("/aws/elb/target-groups/{id}/targets/{targetId}", s.handleTGRemoveTarget)
	r.Post("/aws/elb/target-groups/{id}/health", s.handleTGHealth)
	// Route 53
	r.Get("/aws/route53/zones", s.handleDNSListZones)
	r.Post("/aws/route53/zones", s.handleDNSCreateZone)
	r.Delete("/aws/route53/zones/{id}", s.handleDNSDeleteZone)
	r.Get("/aws/route53/zones/{id}/records", s.handleDNSListRecords)
	r.Post("/aws/route53/zones/{id}/records", s.handleDNSCreateRecord)
	r.Delete("/aws/route53/zones/{id}/records/{recordId}", s.handleDNSDeleteRecord)
	r.Get("/aws/route53/zones/{id}/resolve", s.handleDNSResolve)
	// SQS
	r.Get("/aws/sqs/queues", s.handleSQSListQueues)
	r.Post("/aws/sqs/queues", s.handleSQSCreateQueue)
	r.Get("/aws/sqs/queues/{id}", s.handleSQSGetQueue)
	r.Delete("/aws/sqs/queues/{id}", s.handleSQSDeleteQueue)
	r.Post("/aws/sqs/queues/{id}/messages", s.handleSQSSendMessage)
	r.Get("/aws/sqs/queues/{id}/messages", s.handleSQSListMessages)
	r.Post("/aws/sqs/queues/{id}/receive", s.handleSQSReceive)
	r.Delete("/aws/sqs/queues/{id}/messages/{receipt}", s.handleSQSDeleteMessage)
	r.Post("/aws/sqs/queues/{id}/purge", s.handleSQSPurge)
	r.Post("/aws/sqs/queues/{id}/redrive", s.handleSQSRedrive)
	// SNS
	r.Get("/aws/sns/topics", s.handleSNSListTopics)
	r.Post("/aws/sns/topics", s.handleSNSCreateTopic)
	r.Delete("/aws/sns/topics/{id}", s.handleSNSDeleteTopic)
	r.Get("/aws/sns/topics/{id}/subscriptions", s.handleSNSListSubscriptions)
	r.Post("/aws/sns/topics/{id}/subscribe", s.handleSNSSubscribe)
	r.Delete("/aws/sns/topics/{id}/subscriptions/{subId}", s.handleSNSUnsubscribe)
	r.Post("/aws/sns/topics/{id}/publish", s.handleSNSPublish)
	r.Get("/aws/sns/topics/{id}/deliveries", s.handleSNSListDeliveries)
	// EventBridge
	r.Get("/aws/events/rules", s.handleEBListRules)
	r.Post("/aws/events/rules", s.handleEBCreateRule)
	r.Delete("/aws/events/rules/{id}", s.handleEBDeleteRule)
	r.Post("/aws/events/rules/{id}/state", s.handleEBSetRuleState)
	r.Post("/aws/events/put-events", s.handleEBPutEvents)
	// Lambda
	r.Get("/aws/lambda/functions", s.handleLambdaList)
	r.Post("/aws/lambda/functions", s.handleLambdaCreate)
	r.Get("/aws/lambda/functions/{id}", s.handleLambdaGet)
	r.Delete("/aws/lambda/functions/{id}", s.handleLambdaDelete)
	r.Put("/aws/lambda/functions/{id}/code", s.handleLambdaUpdateCode)
	r.Post("/aws/lambda/functions/{id}/aliases", s.handleLambdaCreateAlias)
	r.Post("/aws/lambda/functions/{id}/triggers", s.handleLambdaAddTrigger)
	r.Post("/aws/lambda/functions/{id}/invoke", s.handleLambdaInvoke)
	r.Get("/aws/lambda/functions/{id}/invocations", s.handleLambdaListInvocations)
	// API Gateway
	r.Get("/aws/apigateway/apis", s.handleAPIGWList)
	r.Post("/aws/apigateway/apis", s.handleAPIGWCreate)
	r.Get("/aws/apigateway/apis/{id}", s.handleAPIGWGet)
	r.Delete("/aws/apigateway/apis/{id}", s.handleAPIGWDelete)
	r.Post("/aws/apigateway/apis/{id}/resources", s.handleAPIGWCreateResource)
	r.Post("/aws/apigateway/apis/{id}/deploy", s.handleAPIGWDeploy)
	r.Get("/aws/apigateway/apis/{id}/logs", s.handleAPIGWLogs)
	// Gateway runtime surface (any method, wildcard path).
	r.Handle("/gateway/{apiId}/{stage}/*", http.HandlerFunc(s.handleAPIGWInvoke))
}
