// HTTP server for the host agent (cmd/host-agent).
//
// Only three typed endpoints exist:
//
//	POST /v1/ops       {op, params, lab_id, idempotency_key}
//	GET  /v1/inventory
//	GET  /healthz      (unauthenticated liveness)
//
// Authentication is a shared HMAC bearer token (HOST_AGENT_TOKEN).
// The token may carry a lab scope as a ":lab:<lab-id>" suffix, e.g.
// "SECRET:lab:abc123" — a scoped token may only operate on that lab.
// A token without a scope is the control-plane token (API/worker) and
// may operate on any lab. M9 upgrades this to mTLS with per-lab
// certificates; the scope check is the interim ownership enforcement
// and is documented as such.
//
// Every request gets a request id (echoed when supplied) and structured
// slog logging. Idempotent operations are deduplicated by
// (lab_id, op, idempotency_key).
package hostagent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DevToken is the insecure dev default used only in simulated mode when
// HOST_AGENT_TOKEN is unset. Startup logs a warning when it is used.
const DevToken = "dev-only-change-me-host-agent"

// Server is the host-agent HTTP server.
type Server struct {
	Agent  HostAgent
	Token  string // configured token (secret part compared constant-time)
	Logger *slog.Logger

	mu   sync.Mutex
	idem map[string]OpResult // lab|op|idempotency_key -> result
}

// NewServer builds a Server. A nil logger uses slog.Default().
func NewServer(agent HostAgent, token string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Agent: agent, Token: token, Logger: logger, idem: map[string]OpResult{}}
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// GenerateToken returns a random 32-byte hex token for operators to put
// in HOST_AGENT_TOKEN.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// splitScope splits a token into its secret and lab scope. A scoped
// token looks like "SECRET:lab:<lab-id>".
func splitScope(token string) (secret, labScope string) {
	if i := strings.LastIndex(token, ":lab:"); i >= 0 {
		return token[:i], token[i+len(":lab:"):]
	}
	return token, ""
}

// authorize checks the Bearer token and the lab scope. It returns the
// scope lab id ("" = control-plane token, any lab) or an error.
func (s *Server) authorize(r *http.Request, labID string) (string, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", errors.New("missing Bearer token")
	}
	got := strings.TrimPrefix(h, "Bearer ")
	wantSecret, wantScope := splitScope(s.Token)
	gotSecret, gotScope := splitScope(got)
	// Compare the secret part in constant time; the scope must match
	// the configured scope exactly when one is configured.
	if len(gotSecret) != len(wantSecret) ||
		subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
		return "", errors.New("invalid token")
	}
	scope := wantScope
	if gotScope != "" {
		scope = gotScope
	}
	if scope != "" && labID != "" && labID != scope {
		return "", fmt.Errorf("token is scoped to lab %q, not %q", scope, labID)
	}
	return scope, nil
}

type ctxKey string

const serverRequestIDKey ctxKey = "request_id"

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + time.Now().UTC().Format("20060102150405")
	}
	return hex.EncodeToString(b)
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	reqID, _ := r.Context().Value(serverRequestIDKey).(string)
	s.writeJSON(w, r, status, map[string]any{
		"error": map[string]any{"code": code, "message": message, "request_id": reqID},
	})
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), serverRequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		reqID, _ := r.Context().Value(serverRequestIDKey).(string)
		s.log().Info("host_agent_request", "request_id", reqID,
			"method", r.Method, "path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	mode := ""
	if s.Agent != nil {
		mode = s.Agent.Mode()
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": "ok", "service": "cloudforge-host-agent",
		"version": Version, "mode": mode,
	})
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if _, err := s.authorize(r, ""); err != nil {
		s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "a valid host-agent Bearer token is required")
		return
	}
	inv, err := s.Agent.Inventory(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"inventory": inv})
}

func (s *Server) handleOps(w http.ResponseWriter, r *http.Request) {
	var req OpRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return
	}
	// Auth + per-lab ownership check before any validation or execution.
	if _, err := s.authorize(r, req.LabID); err != nil {
		if strings.Contains(err.Error(), "scoped to lab") {
			s.writeError(w, r, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "a valid host-agent Bearer token is required")
		return
	}
	if err := Validate(req.Op, req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	// Idempotency replay (server level; agents dedupe too).
	if req.IdempotencyKey != "" {
		key := req.LabID + "|" + string(req.Op) + "|" + req.IdempotencyKey
		s.mu.Lock()
		if res, ok := s.idem[key]; ok {
			s.mu.Unlock()
			res.Replayed = true
			s.writeJSON(w, r, http.StatusOK, res)
			return
		}
		s.mu.Unlock()
	}
	res, err := s.Agent.RunOp(r.Context(), req)
	if err != nil {
		var ve *ValidationError
		var ue *UnavailableError
		switch {
		case errors.As(err, &ve):
			s.writeError(w, r, http.StatusBadRequest, "validation_failed", err.Error())
		case errors.As(err, &ue):
			s.writeError(w, r, http.StatusServiceUnavailable, "host_agent_unavailable", err.Error())
		case strings.Contains(err.Error(), "not found"):
			s.writeError(w, r, http.StatusNotFound, "not_found", err.Error())
		case strings.Contains(err.Error(), "insufficient host") || strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "cannot reboot"):
			s.writeError(w, r, http.StatusConflict, "conflict", err.Error())
		default:
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	if req.IdempotencyKey != "" {
		key := req.LabID + "|" + string(req.Op) + "|" + req.IdempotencyKey
		s.mu.Lock()
		s.idem[key] = res
		s.mu.Unlock()
	}
	s.writeJSON(w, r, http.StatusOK, res)
}

// Routes returns the host-agent HTTP handler tree.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/inventory", s.handleInventory)
	mux.HandleFunc("POST /v1/ops", s.handleOps)
	return s.withRequestID(s.withLogging(mux))
}
