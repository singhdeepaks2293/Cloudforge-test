package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeDynamo is a test double for DynamoDB Local: it answers the
// JSON API (ListTables / CreateTable / PutItem / GetItem) from
// memory. It is NOT a real DynamoDB.
type fakeDynamo struct {
	mu     sync.Mutex
	tables map[string]map[string]map[string]any // table -> key JSON -> item
}

func (f *fakeDynamo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	write := func(v any) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.Header.Get("X-Amz-Target") {
	case "DynamoDB_20120810.ListTables":
		var names []string
		for name := range f.tables {
			names = append(names, name)
		}
		if names == nil {
			names = []string{}
		}
		write(map[string]any{"TableNames": names})
	case "DynamoDB_20120810.CreateTable":
		name, _ := req["TableName"].(string)
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"message": "table name required"})
			return
		}
		if _, ok := f.tables[name]; ok {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"__type": "ResourceInUseException", "message": "table exists"})
			return
		}
		f.tables[name] = map[string]map[string]any{}
		write(map[string]any{"TableDescription": map[string]any{"TableName": name, "TableStatus": "ACTIVE"}})
	case "DynamoDB_20120810.PutItem":
		name, _ := req["TableName"].(string)
		item, _ := req["Item"].(map[string]any)
		if _, ok := f.tables[name]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"__type": "ResourceNotFoundException", "message": "table not found"})
			return
		}
		key, _ := json.Marshal(item["id"])
		f.tables[name][string(key)] = item
		write(map[string]any{})
	case "DynamoDB_20120810.GetItem":
		name, _ := req["TableName"].(string)
		key, _ := req["Key"].(map[string]any)
		if _, ok := f.tables[name]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"__type": "ResourceNotFoundException", "message": "table not found"})
			return
		}
		kb, _ := json.Marshal(key["id"])
		// Match by the id attribute value.
		for _, item := range f.tables[name] {
			if itemID, _ := json.Marshal(item["id"]); string(itemID) == string(kb) {
				write(map[string]any{"Item": item})
				return
			}
		}
		write(map[string]any{})
	default:
		w.WriteHeader(http.StatusBadRequest)
		write(map[string]any{"message": "unknown target"})
	}
}

func TestDynamoDBAdapterAgainstFakeServer(t *testing.T) {
	fake := &fakeDynamo{tables: map[string]map[string]map[string]any{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := NewDynamoDBAdapter(srv.URL)
	ctx := context.Background()
	if st, _ := a.Health(ctx); st != HealthOK {
		t.Fatalf("health = %q, want ok", st)
	}
	if err := a.CreateTable(ctx, "users", "id"); err != nil {
		t.Fatal(err)
	}
	item := map[string]any{"id": map[string]any{"S": "u-1"}, "name": map[string]any{"S": "Alice"}}
	if err := a.PutItem(ctx, "users", item); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetItem(ctx, "users", map[string]any{"id": map[string]any{"S": "u-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetItem returned no item")
	}
	if name, _ := got["name"].(map[string]any)["S"].(string); name != "Alice" {
		t.Fatalf("GetItem = %+v", got)
	}
	tables, err := a.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0] != "users" {
		t.Fatalf("ListTables = %v", tables)
	}
	// Duplicate table is a caller error, not unavailability.
	if err := a.CreateTable(ctx, "users", "id"); err == nil || errors.Is(err, ErrDynamoUnavailable) {
		t.Fatalf("duplicate table err = %v, want a caller error", err)
	}
}

func TestDynamoDBAdapterUnreachable(t *testing.T) {
	a := NewDynamoDBAdapter("http://127.0.0.1:1")
	ctx := context.Background()
	if st, detail := a.Health(ctx); st != HealthUnavailable || detail == "" {
		t.Fatalf("health = %q %q, want unavailable with detail", st, detail)
	}
	if _, err := a.ListTables(ctx); !errors.Is(err, ErrDynamoUnavailable) {
		t.Fatalf("ListTables against a dead endpoint must be ErrDynamoUnavailable, got %v", err)
	}
}
