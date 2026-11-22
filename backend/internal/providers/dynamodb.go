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

// ErrDynamoTableExists is returned by CreateTable when the table
// already exists. Callers map it to HTTP 409.
var ErrDynamoTableExists = errors.New("dynamodb table already exists")

// DynamoError is a DynamoDB API error (HTTP >= 400): Target is the
// operation, Type is the DynamoDB __type (e.g.
// ResourceInUseException), Msg is the message.
type DynamoError struct {
	Target string
	Type   string
	Msg    string
}

func (e *DynamoError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("dynamodb %s failed: %s: %s", e.Target, e.Type, e.Msg)
	}
	return fmt.Sprintf("dynamodb %s failed: %s", e.Target, e.Msg)
}

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
	// DynamoDB Local refuses unsigned requests
	// (MissingAuthenticationToken) but never validates the
	// signature: a dummy local-only credential is enough (verified
	// live 2026-11-21: any Credential value returns TableNames).
	// This header is only ever sent to the configured local
	// endpoint, never to AWS. Note the credential must be
	// alphanumeric: Local rejects keys containing '-' with
	// UnrecognizedClientException ("cloudforge-local" failed live;
	// "cloudforgelocal" works).
	now := time.Now().UTC()
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=cloudforgelocal/"+now.Format("20060102")+"/us-east-1/dynamodb/aws4_request, SignedHeaders=content-type;host;x-amz-date;x-amz-target, Signature=0000000000000000000000000000000000000000000000000000000000000000")
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
			// DynamoDB errors use capital-M "Message".
			msg, _ = out["Message"].(string)
		}
		if msg == "" {
			msg = resp.Status
		}
		typ, _ := out["__type"].(string)
		return nil, &DynamoError{Target: target, Type: typ, Msg: msg}
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
	if err != nil {
		var de *DynamoError
		// Local returns the fully-qualified type
		// "com.amazonaws.dynamodb.v20120810#ResourceInUseException";
		// the fake server in tests returns the bare name.
		if errors.As(err, &de) && (de.Type == "ResourceInUseException" || strings.HasSuffix(de.Type, "#ResourceInUseException")) {
			return fmt.Errorf("%w: %s", ErrDynamoTableExists, de.Msg)
		}
		return err
	}
	return nil
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

// DescribeTable returns the table's partition-key attribute name
// (KeySchema HASH entry), "" when the table has none / is absent.
func (a *DynamoDBAdapter) DescribeTable(ctx context.Context, table string) (string, error) {
	if table == "" {
		return "", errors.New("table is required")
	}
	out, err := a.do(ctx, "DescribeTable", map[string]any{"TableName": table})
	if err != nil {
		return "", err
	}
	tbl, _ := out["Table"].(map[string]any)
	schema, _ := tbl["KeySchema"].([]any)
	for _, k := range schema {
		if m, ok := k.(map[string]any); ok && m["KeyType"] == "HASH" {
			if name, ok := m["AttributeName"].(string); ok {
				return name, nil
			}
		}
	}
	return "", nil
}

// DeleteTable deletes a table. DynamoDB Local returns the table
// description; absence is an error from the backend.
func (a *DynamoDBAdapter) DeleteTable(ctx context.Context, table string) error {
	if table == "" {
		return errors.New("table is required")
	}
	_, err := a.do(ctx, "DeleteTable", map[string]any{"TableName": table})
	return err
}

// DeleteItem deletes an item by key (DynamoDB-typed).
func (a *DynamoDBAdapter) DeleteItem(ctx context.Context, table string, key map[string]any) error {
	if table == "" || len(key) == 0 {
		return errors.New("table and a non-empty key are required")
	}
	_, err := a.do(ctx, "DeleteItem", map[string]any{"TableName": table, "Key": key})
	return err
}

// Scan returns every item in a table (DynamoDB-typed), up to limit
// (0 = backend default). Pagination (LastEvaluatedKey) is not
// followed — teaching-scale tables only; the handler says so.
func (a *DynamoDBAdapter) Scan(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	if table == "" {
		return nil, errors.New("table is required")
	}
	body := map[string]any{"TableName": table}
	if limit > 0 {
		body["Limit"] = limit
	}
	out, err := a.do(ctx, "Scan", body)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	if raw, ok := out["Items"].([]any); ok {
		for _, it := range raw {
			if m, ok := it.(map[string]any); ok {
				items = append(items, m)
			}
		}
	}
	return items, nil
}

// Query returns items whose partition key equals keyValue (plain
// string; the caller supplies the key attribute name). This is the
// hash-key-only subset DynamoDB Local supports without an index.
func (a *DynamoDBAdapter) Query(ctx context.Context, table, keyAttr, keyValue string) ([]map[string]any, error) {
	if table == "" || keyAttr == "" {
		return nil, errors.New("table and key attribute are required")
	}
	out, err := a.do(ctx, "Query", map[string]any{
		"TableName":                 table,
		"KeyConditionExpression":    "#k = :v",
		"ExpressionAttributeNames":  map[string]any{"#k": keyAttr},
		"ExpressionAttributeValues": map[string]any{":v": map[string]any{"S": keyValue}},
	})
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	if raw, ok := out["Items"].([]any); ok {
		for _, it := range raw {
			if m, ok := it.(map[string]any); ok {
				items = append(items, m)
			}
		}
	}
	return items, nil
}
