// Milestone 6 terminal handlers: short-lived token issue and the
// WebSocket shell.
//
// Auth is the terminal token query param ONLY — the long-lived Bearer
// token is never accepted in a URL. The token endpoint itself needs
// the Bearer token (header), as usual.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terminal"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleTerminalToken(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	lab, ok := s.ownLab(w, r, u)
	if !ok {
		return
	}
	mode := s.HostAgentMode
	if mode == "" {
		mode = "simulated"
	}
	// Issue the token, then record the session row (token hash only)
	// and bind the row id back onto the token so the WebSocket close
	// can end the right session. A session-store failure is logged
	// but must not block the token (the token itself is the auth;
	// the row is the audit trail).
	rawToken, exp, redisBacked, err := s.Terminal.Issue(r.Context(), lab.ID, u.ID, "")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "could not issue terminal token: "+err.Error())
		return
	}
	if sess, serr := s.TerminalSessions.CreateSession(r.Context(), &store.TerminalSession{
		UserID: u.ID, LabID: lab.ID, TokenHash: terminal.HashToken(rawToken),
		ExpiresAt: exp, Mode: mode,
	}); serr == nil {
		s.Terminal.BindSession(r.Context(), rawToken, sess.ID)
	} else {
		s.Logger.Warn("terminal session row not recorded", "lab_id", lab.ID, "error", serr)
	}
	s.audit(r, u.ID, "terminal_token_issued", "lab", lab.ID, map[string]any{
		"redis_backed": redisBacked, "expires_at": exp,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"token": rawToken, "expires_at": exp.Format(time.RFC3339),
		"redis_backed": redisBacked,
	})
}

// terminalWSHandler returns the WebSocket handler. Token validation
// happens BEFORE the upgrade: an invalid/expired/used token gets a
// plain 401 JSON response, never an upgraded socket. On connect the
// session row (by token hash) is marked used; on close it is ended.
func (s *Server) terminalWSHandler() http.Handler {
	wsHandler := websocket.Handler(func(conn *websocket.Conn) {
		req := conn.Request()
		info, ok := req.Context().Value(terminalInfoKey{}).(terminal.TokenInfo)
		if !ok {
			_ = conn.Close()
			return
		}
		s.serveTerminal(conn, req, info)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		labID := chi.URLParam(r, "id")
		token := r.URL.Query().Get("token")
		info, err := s.Terminal.Consume(r.Context(), token)
		if err != nil {
			if errors.Is(err, terminal.ErrTokenInvalid) {
				s.writeError(w, r, http.StatusUnauthorized, "invalid_terminal_token",
					"terminal token is invalid, expired or already used; request a new one via POST /terminal/token")
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if info.LabID != labID {
			s.writeError(w, r, http.StatusForbidden, "forbidden", "terminal token is bound to a different lab")
			return
		}
		// Mark the session row used (best effort).
		if info.SessionID != "" {
			_ = s.TerminalSessions.MarkSessionUsed(r.Context(), info.SessionID)
		}
		ctx := context.WithValue(r.Context(), terminalInfoKey{}, info)
		wsHandler.ServeHTTP(w, r.WithContext(ctx))
	})
}

type terminalInfoKey struct{}

// serveTerminal runs one WebSocket terminal session.
func (s *Server) serveTerminal(conn *websocket.Conn, r *http.Request, info terminal.TokenInfo) {
	defer conn.Close()
	labID := info.LabID
	mode := s.HostAgentMode
	if mode == "" {
		mode = "simulated"
	}
	var provisioner hostagent.Provisioner
	if s.HostAgent != nil {
		provisioner = s.HostAgent
	}
	shell := terminal.NewShell(terminal.ShellConfig{
		LabID: labID, UserID: info.UserID, Mode: mode, Provisioner: provisioner,
	})
	// Session end is recorded on close (best effort).
	defer func() {
		if info.SessionID != "" {
			_ = s.TerminalSessions.EndSession(context.Background(), info.SessionID, time.Now().UTC())
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Minute))
	if _, err := conn.Write([]byte(shell.Banner(labID) + "\r\n$ ")); err != nil {
		return
	}
	var line strings.Builder
	for {
		var msg string
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			return // client closed / error: end the session
		}
		_ = conn.SetDeadline(time.Now().Add(30 * time.Minute))
		if cm, ok := terminal.ParseClientMessage(msg); ok {
			switch cm.Type {
			case "ping":
				resp, _ := json.Marshal(terminal.ServerMessage{Type: "pong"})
				_, _ = conn.Write(append(resp, '\n'))
			case "resize":
				// Terminal size is noted; the simulated shell has no
				// pty to resize, and the host-agent exec path has no
				// resize operation either. Acknowledge honestly.
				resp, _ := json.Marshal(terminal.ServerMessage{Type: "resize-ack", Data: "noted"})
				_, _ = conn.Write(append(resp, '\n'))
			case "input":
				msg = cm.Data
			default:
				continue
			}
			if cm.Type != "input" {
				continue
			}
		}
		// Raw keystrokes: accumulate until end-of-line, then execute.
		for _, ch := range msg {
			switch ch {
			case '\r', '\n':
				out, exit := shell.Execute(r.Context(), line.String())
				line.Reset()
				if _, err := conn.Write([]byte(out + "\r\n$ ")); err != nil {
					return
				}
				if exit {
					return
				}
			case '\x7f':
				// Backspace: drop one char from the line buffer.
				cur := line.String()
				line.Reset()
				if len(cur) > 0 {
					line.WriteString(cur[:len(cur)-1])
				}
				_, _ = conn.Write([]byte("\b \b"))
			default:
				line.WriteRune(ch)
				// Echo the keystroke so the terminal feels live.
				if _, err := conn.Write([]byte(string(ch))); err != nil {
					return
				}
			}
		}
	}
}
