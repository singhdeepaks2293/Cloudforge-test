// Package terminal implements the Milestone 6 lab terminal.
//
//   - Tokens: POST /terminal/token issues a short-lived (5 minute),
//     single-use token bound to one lab + user. It is stored in Redis
//     (via cache.Cache) when Redis is available and in an in-memory
//     map otherwise, with the same expiry — a Redis outage never
//     blocks the terminal, and the fallback is recorded on the token.
//     Only the SHA-256 hash is ever persisted (terminal_sessions).
//   - WebSocket: auth is the token query param ONLY — never the
//     long-lived Bearer token in a URL. Expiry is enforced at connect
//     and the token is consumed on first use; a second use is 401.
//   - Shell: the shell runs ONLY via host-agent ExecInLab with the
//     M3 allowlisted commands. In simulated mode (or when no host
//     agent is configured) the shell is a clearly-labelled simulated
//     shell that handles the allowlisted commands locally and never
//     runs a host shell. When a real container/VM exec is available
//     via the host agent, input lines are routed through it and its
//     (exit-code) result is returned.
//   - Messages: JSON {type:"resize",cols,rows} and {type:"ping"} are
//     handled; anything else is shell input (raw keystrokes, lines
//     executed on \r / \n). {type:"pong"} is sent for pings.
//
// The WebSocket transport is golang.org/x/net/websocket — already in
// the module graph as an indirect dependency of grpc, Go 1.25
// compatible, so no new dependency (and no new toolchain) is pulled
// in. nhooyr.io/websocket was considered and not used for exactly
// that reason.
package terminal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/cache"
	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
)

// TokenTTL is the terminal-token lifetime (5 minutes, single-use).
const TokenTTL = 5 * time.Minute

// ErrTokenInvalid is returned for unknown, expired or already-used tokens.
var ErrTokenInvalid = errors.New("terminal token is invalid, expired or already used")

// TokenInfo is the validated identity behind a terminal token.
type TokenInfo struct {
	LabID     string
	UserID    string
	SessionID string // terminal_sessions row id ("" when sessions are not recorded)
	ExpiresAt time.Time
	// RedisBacked reports whether the token lived in Redis (false =
	// in-memory fallback because Redis was down at issue time).
	RedisBacked bool
}

// tokenRecord is the stored token payload.
type tokenRecord struct {
	LabID     string    `json:"lab_id"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Service issues and consumes terminal tokens.
type Service struct {
	Cache *cache.Cache // nil = memory only

	mu     sync.Mutex
	memory map[string]tokenRecord // fallback when Redis is unavailable
}

// NewService builds a token service. A nil cache means memory-only.
func NewService(c *cache.Cache) *Service {
	return &Service{Cache: c, memory: make(map[string]tokenRecord)}
}

// HashToken returns the SHA-256 hex of a token (what is persisted).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Issue creates a single-use token bound to lab+user, expiring in
// TokenTTL. SessionID is recorded on the token so the WebSocket close
// can end the right terminal_sessions row.
func (s *Service) Issue(ctx context.Context, labID, userID, sessionID string) (string, time.Time, bool, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, false, err
	}
	exp := time.Now().UTC().Add(TokenTTL)
	rec := tokenRecord{LabID: labID, UserID: userID, SessionID: sessionID, ExpiresAt: exp}
	raw, _ := json.Marshal(rec)
	if s.Cache != nil && s.Cache.Available() {
		if s.Cache.SetWithTTL(ctx, "cloudforge:terminal:"+token, string(raw), TokenTTL) {
			return token, exp, true, nil
		}
	}
	// Redis down (or unset): in-memory fallback with the same expiry.
	s.mu.Lock()
	s.memory[token] = rec
	s.mu.Unlock()
	return token, exp, false, nil
}

// BindSession attaches a terminal_sessions row id to an already
// issued token, so the WebSocket close can end the right row. The
// token's remaining TTL is preserved.
func (s *Service) BindSession(ctx context.Context, token, sessionID string) {
	if s.Cache != nil && s.Cache.Available() {
		if raw, ok := s.Cache.Get(ctx, "cloudforge:terminal:"+token); ok {
			var rec tokenRecord
			if json.Unmarshal([]byte(raw), &rec) == nil {
				rec.SessionID = sessionID
				if data, err := json.Marshal(rec); err == nil {
					ttl := time.Until(rec.ExpiresAt)
					if ttl > 0 {
						s.Cache.SetWithTTL(ctx, "cloudforge:terminal:"+token, string(data), ttl)
						return
					}
				}
			}
		}
	}
	s.mu.Lock()
	if rec, ok := s.memory[token]; ok {
		rec.SessionID = sessionID
		s.memory[token] = rec
	}
	s.mu.Unlock()
}

// Consume validates and single-use consumes a token.
func (s *Service) Consume(ctx context.Context, token string) (TokenInfo, error) {
	if token == "" {
		return TokenInfo{}, ErrTokenInvalid
	}
	if s.Cache != nil && s.Cache.Available() {
		if raw, ok := s.Cache.GetDel(ctx, "cloudforge:terminal:"+token); ok {
			var rec tokenRecord
			if err := json.Unmarshal([]byte(raw), &rec); err != nil {
				return TokenInfo{}, ErrTokenInvalid
			}
			if time.Now().UTC().After(rec.ExpiresAt) {
				return TokenInfo{}, ErrTokenInvalid
			}
			return TokenInfo{LabID: rec.LabID, UserID: rec.UserID, SessionID: rec.SessionID, ExpiresAt: rec.ExpiresAt, RedisBacked: true}, nil
		}
	}
	s.mu.Lock()
	rec, ok := s.memory[token]
	if ok {
		delete(s.memory, token)
	}
	s.mu.Unlock()
	if !ok {
		return TokenInfo{}, ErrTokenInvalid
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		return TokenInfo{}, ErrTokenInvalid
	}
	return TokenInfo{LabID: rec.LabID, UserID: rec.UserID, SessionID: rec.SessionID, ExpiresAt: rec.ExpiresAt}, nil
}

// --- shell ---------------------------------------------------------------

// Shell runs one line of terminal input and returns the output text
// (without the prompt; the caller appends "\r\n$ ").
type Shell interface {
	// Banner is written once on connect.
	Banner(labID string) string
	// Execute runs one input line; "exit" requests disconnect.
	Execute(ctx context.Context, line string) (output string, exit bool)
}

// ShellConfig wires a shell: when Provisioner is nil or Mode is
// "simulated", the simulated shell is used; otherwise lines are
// routed through the host agent's ExecInLab.
type ShellConfig struct {
	LabID       string
	UserID      string
	Mode        string                // host-agent mode: simulated | docker | libvirt
	Provisioner hostagent.Provisioner // nil = no host agent configured
}

// NewShell builds the shell for cfg.
func NewShell(cfg ShellConfig) Shell {
	if cfg.Provisioner != nil && cfg.Mode != "" && cfg.Mode != "simulated" {
		return &realShell{cfg: cfg}
	}
	return &simulatedShell{labID: cfg.LabID}
}

// --- simulated shell -----------------------------------------------------

var simulatedAllowlist = []string{"aws", "terraform", "tofu", "docker", "kubectl", "ping", "curl", "ls", "cat", "help", "pwd", "echo", "exit", "clear"}

type simulatedShell struct{ labID string }

func (s *simulatedShell) Banner(labID string) string {
	return "CloudForge lab terminal — SIMULATED SHELL (host-agent mode: simulated).\r\n" +
		"Commands run only via the allowlisted host-agent ExecInLab operations;\r\n" +
		"no host shell is available and nothing here touches the host.\r\n" +
		"Allowlisted: aws, terraform, tofu, docker (read-only), kubectl (read-only),\r\n" +
		"ping, curl (lab-internal targets only), ls, cat. Type 'help' for more.\r\n"
}

func (s *simulatedShell) Execute(ctx context.Context, line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	fields := strings.Fields(trimmed)
	cmd := fields[0]
	switch cmd {
	case "exit", "logout":
		return "Closing simulated terminal session.\r\n", true
	case "clear":
		return "\x1b[2J\x1b[H", false
	case "help":
		return "Simulated lab shell. Allowlisted commands: " + strings.Join(simulatedAllowlist, ", ") + ".\r\n" +
			"Output is simulated: no command is executed on any host or VM.\r\n", false
	case "pwd":
		return "/home/cloudforge\r\n", false
	case "echo":
		return strings.Join(fields[1:], " ") + "\r\n", false
	case "ls":
		return "simulated: ls " + strings.Join(fields[1:], " ") + " — lab workdir is empty (simulated; no host directory was read).\r\n", false
	case "cat":
		return "simulated: cat " + strings.Join(fields[1:], " ") + " — no file was read (simulated).\r\n", false
	case "ping", "curl":
		// Validate the target exactly like the host agent does.
		if err := hostagent.Validate(hostagent.OpExecInLab, hostagent.OpRequest{
			Op: hostagent.OpExecInLab, LabID: s.labID, Command: fields,
		}); err != nil {
			return "rejected: " + err.Error() + "\r\n", false
		}
		return fmt.Sprintf("simulated: %s %s — no network request was made (simulated).\r\n", cmd, strings.Join(fields[1:], " ")), false
	case "aws", "terraform", "tofu", "docker", "kubectl":
		if err := hostagent.Validate(hostagent.OpExecInLab, hostagent.OpRequest{
			Op: hostagent.OpExecInLab, LabID: s.labID, Command: fields,
		}); err != nil {
			return "rejected: " + err.Error() + "\r\n", false
		}
		return fmt.Sprintf("simulated execution (no command was run): %s\r\n", strings.Join(fields, " ")), false
	default:
		return fmt.Sprintf("command not found: %q is not on the allowlist (aws, terraform, tofu, docker, kubectl, ping, curl, ls, cat, help, pwd, echo, exit).\r\n", cmd), false
	}
}

// --- real shell (via host agent) ------------------------------------------

type realShell struct{ cfg ShellConfig }

func (s *realShell) Banner(labID string) string {
	return fmt.Sprintf("CloudForge lab terminal — connected to lab %s via host agent (mode: %s).\r\n"+
		"Commands run only via the allowlisted host-agent ExecInLab operations.\r\n", labID, s.cfg.Mode)
}

func (s *realShell) Execute(ctx context.Context, line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if fields[0] == "exit" || fields[0] == "logout" {
		return "Closing terminal session.\r\n", true
	}
	if fields[0] == "help" {
		return "Lab shell via host-agent ExecInLab. Allowlisted: aws, terraform, tofu, docker (read-only), kubectl (read-only), ping, curl (lab-internal), ls, cat.\r\n", false
	}
	res, err := s.cfg.Provisioner.Do(ctx, hostagent.OpRequest{
		Op: hostagent.OpExecInLab, LabID: s.cfg.LabID, OwnerID: s.cfg.UserID, Command: fields,
	})
	if err != nil {
		if hostagent.IsUnavailable(err) {
			return "host agent unavailable: " + err.Error() + "\r\n", false
		}
		return "rejected: " + err.Error() + "\r\n", false
	}
	msg := res.Message
	if msg == "" {
		msg = fmt.Sprintf("exec %q completed (state %s)", strings.Join(fields, " "), res.State)
	}
	return msg + "\r\n", false
}

// --- wire protocol ---------------------------------------------------------

// ClientMessage is one JSON message from the terminal client.
type ClientMessage struct {
	Type string `json:"type"` // resize | ping | input
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Data string `json:"data,omitempty"`
}

// ServerMessage is one JSON message to the terminal client.
type ServerMessage struct {
	Type string `json:"type"` // pong | resize-ack | error
	Data string `json:"data,omitempty"`
}

// ParseClientMessage parses a wire message; ok=false means the payload
// is raw keystroke input, not JSON.
func ParseClientMessage(raw string) (ClientMessage, bool) {
	var m ClientMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m.Type == "" {
		return ClientMessage{}, false
	}
	switch m.Type {
	case "resize", "ping", "input":
		return m, true
	default:
		return ClientMessage{}, false
	}
}
