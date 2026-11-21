// Typed HTTP client for the host-agent API (cmd/host-agent). The API
// and worker talk to the host agent ONLY through this client.
package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClientTimeout bounds a single host-agent request.
const ClientTimeout = 10 * time.Second

// Client is a typed host-agent client.
type Client struct {
	Addr  string // e.g. "localhost:8082" or "http://localhost:8082"
	Token string
	HTTP  *http.Client
}

// NewClient builds a client. A leading ":" in addr is treated as
// localhost (so HOST_AGENT_ADDR=":8082" works for both listening and
// dialing).
func NewClient(addr, token string) *Client {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	if addr != "" && !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return &Client{Addr: addr, Token: token, HTTP: &http.Client{Timeout: ClientTimeout}}
}

func (c *Client) baseURL() string {
	if c.Addr == "" {
		return "http://localhost:8082"
	}
	return strings.TrimSuffix(c.Addr, "/")
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal host-agent request: %w", err)
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, rdr)
	if err != nil {
		return fmt.Errorf("build host-agent request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if id := ctx.Value(requestIDCtxKey{}); id != nil {
		if s, ok := id.(string); ok && s != "" {
			req.Header.Set("X-Request-ID", s)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// A host agent that cannot be reached at all is "unavailable",
		// so callers fall back instead of treating it as a hard failure.
		return &UnavailableError{Backend: "host-agent", Reason: fmt.Sprintf("host agent at %s unreachable: %v", c.baseURL(), err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read host-agent response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("host agent: forbidden (403): token is not authorized for this lab")
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		var env errorEnvelope
		if json.Unmarshal(data, &env) == nil && env.Error.Message != "" {
			return &UnavailableError{Backend: "host-agent", Reason: env.Error.Message}
		}
		return &UnavailableError{Backend: "host-agent", Reason: "host agent reported unavailable (503)"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env errorEnvelope
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &env) == nil && env.Error.Message != "" {
			msg = env.Error.Code + ": " + env.Error.Message
		}
		return fmt.Errorf("host agent %s %s returned %d: %s", method, path, resp.StatusCode, msg)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode host-agent response: %w", err)
		}
	}
	return nil
}

// requestIDCtxKey carries a request id into the client (optional).
type requestIDCtxKey struct{}

// WithRequestID returns a context carrying a request id for the client.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

// Do executes one allowlisted operation on the host agent.
func (c *Client) Do(ctx context.Context, req OpRequest) (OpResult, error) {
	if err := Validate(req.Op, req); err != nil {
		return OpResult{}, err
	}
	var res OpResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/ops", req, &res); err != nil {
		return OpResult{}, err
	}
	return res, nil
}

// Inventory fetches host inventory from the host agent.
func (c *Client) Inventory(ctx context.Context) (Inventory, error) {
	var res struct {
		Inventory Inventory `json:"inventory"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/inventory", nil, &res); err != nil {
		return Inventory{}, err
	}
	return res.Inventory, nil
}

// Health checks the host agent's /healthz (unauthenticated).
func (c *Client) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/healthz", nil, nil)
}

// CleanupLab lists the lab's containers and VMs through the typed API
// and removes/deletes each one. A host agent that is unavailable makes
// CleanupLab return Unavailable (callers log and continue with their
// own resource cleanup); individual not-found results are ignored
// because removal is idempotent.
func (c *Client) CleanupLab(ctx context.Context, labID, ownerID string) error {
	if labID == "" {
		return errors.New("host agent CleanupLab requires a lab_id")
	}
	lists, err := c.Do(ctx, OpRequest{Op: OpListContainers, LabID: labID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	for _, item := range lists.Items {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if _, err := c.Do(ctx, OpRequest{Op: OpRemoveContainer, LabID: labID, OwnerID: ownerID, ID: id}); err != nil {
			return fmt.Errorf("cleanup container %s: %w", id, err)
		}
	}
	vms, err := c.Do(ctx, OpRequest{Op: OpListVMs, LabID: labID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	for _, item := range vms.Items {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if _, err := c.Do(ctx, OpRequest{Op: OpDeleteVM, LabID: labID, OwnerID: ownerID, ID: id}); err != nil {
			return fmt.Errorf("cleanup VM %s: %w", id, err)
		}
	}
	return nil
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
