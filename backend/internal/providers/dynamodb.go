package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrDynamoUnavailable is returned when the DynamoDB endpoint cannot
// be reached. Callers map it to HTTP 503 with the capability info.
var ErrDynamoUnavailable = errors.New("dynamodb backend unavailable")

// DefaultDynamoDBEndpoint is used when DYNAMODB_ENDPOINT is unset.
const DefaultDynamoDBEndpoint = "http://localhost:8000"

// DynamoDBAdapter is a minimal HTTP adapter for the DynamoDB JSON
// API as spoken by DynamoDB Local (and LocalStack's DynamoDB). It
// uses no AWS SDK: requests are POST / with an X-Amz-Target header.
// It is NOT AWS DynamoDB: tables and items live only in the
// configured local endpoint.
type DynamoDBAdapter struct {
	Endpoint   string
	HTTPClient *http.Client
}

// NewDynamoDBAdapter builds an adapter for endpoint (a URL; a bare
// host:port is treated as http://host:port).
func NewDynamoDBAdapter(endpoint string) *DynamoDBAdapter {
	if endpoint == "" {
		endpoint = DefaultDynamoDBEndpoint
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	return &DynamoDBAdapter{
		Endpoint:   strings.TrimSuffix(endpoint, "/"),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewDynamoDBAdapterFromEnv builds an adapter from DYNAMODB_ENDPOINT
// (default http://localhost:8000).
func NewDynamoDBAdapterFromEnv() *DynamoDBAdapter {
	return NewDynamoDBAdapter(os.Getenv("DYNAMODB_ENDPOINT"))
}

func (a *DynamoDBAdapter) do(ctx context.Context, target string, body any) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint+"/", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+target)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDynamoUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("dynamodb %s: unparsable response: %w", target, err)
		}
	}
	if resp.StatusCode >= 400 {
		msg, _ := out["message"].(string)
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("dynamodb %s failed: %s", target, msg)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// Health probes the backend with ListTables.
func (a *DynamoDBAdapter) Health(ctx context.Context) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if _, err := a.do(ctx, "ListTables", map[string]any{}); err != nil {
		if errors.Is(err, ErrDynamoUnavailable) {
			return HealthUnavailable, fmt.Sprintf("DynamoDB at %s unreachable: %v", a.Endpoint, err)
		}
		return HealthDegraded, fmt.Sprintf("DynamoDB at %s answered but errored: %v", a.Endpoint, err)
	}
	return HealthOK, "DynamoDB ListTables succeeded at " + a.Endpoint
}

// ListTables lists table names.
func (a *DynamoDBAdapter) ListTables(ctx context.Context) ([]string, error) {
	out, err := a.do(ctx, "ListTables", map[string]any{})
	if err != nil {
		return nil, err
	}
	var tables []string
	if names, ok := out["TableNames"].([]any); ok {
		for _, n := range names {
			if s, ok := n.(string); ok {
				tables = append(tables, s)
			}
		}
	}
	if tables == nil {
		tables = []string{}
	}
	return tables, nil
}

// CreateTable creates a table with a single hash key (PAY_PER_REQUEST
// billing). keyName is the partition-key attribute name.
func (a *DynamoDBAdapter) CreateTable(ctx context.Context, table, keyName string) error {
	if table == "" || keyName == "" {
		return errors.New("table and key attribute name are required")
	}
	_, err := a.do(ctx, "CreateTable", map[string]any{
		"TableName":            table,
		"KeySchema":            []any{map[string]any{"AttributeName": keyName, "KeyType": "HASH"}},
		"AttributeDefinitions": []any{map[string]any{"AttributeName": keyName, "AttributeType": "S"}},
		"BillingMode":          "PAY_PER_REQUEST",
	})
	return err
}

// PutItem writes an item. Values must already be DynamoDB-typed,
// e.g. {"id": {"S": "1"}, "n": {"N": "42"}}.
func (a *DynamoDBAdapter) PutItem(ctx context.Context, table string, item map[string]any) error {
	if table == "" || len(item) == 0 {
		return errors.New("table and a non-empty item are required")
	}
	_, err := a.do(ctx, "PutItem", map[string]any{"TableName": table, "Item": item})
	return err
}

// GetItem fetches an item by key (DynamoDB-typed, e.g.
// {"id": {"S": "1"}}). It returns nil, nil when the item is absent.
func (a *DynamoDBAdapter) GetItem(ctx context.Context, table string, key map[string]any) (map[string]any, error) {
	if table == "" || len(key) == 0 {
		return nil, errors.New("table and a non-empty key are required")
	}
	out, err := a.do(ctx, "GetItem", map[string]any{"TableName": table, "Key": key})
	if err != nil {
		return nil, err
	}
	item, _ := out["Item"].(map[string]any)
	return item, nil
}
