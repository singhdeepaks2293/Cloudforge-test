package terminal

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTokenIssueConsumeSingleUse(t *testing.T) {
	svc := NewService(nil) // memory fallback (no Redis in unit tests)
	ctx := context.Background()
	token, exp, redisBacked, err := svc.Issue(ctx, "lab-1", "user-1", "sess-1")
	if err != nil || token == "" {
		t.Fatalf("issue: %v %q", err, token)
	}
	if redisBacked {
		t.Fatal("no cache configured: token must be memory-backed")
	}
	if time.Until(exp) > TokenTTL || time.Until(exp) <= 0 {
		t.Fatalf("expiry must be within TokenTTL: %v", exp)
	}
	info, err := svc.Consume(ctx, token)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if info.LabID != "lab-1" || info.UserID != "user-1" || info.SessionID != "sess-1" {
		t.Fatalf("token info mismatch: %+v", info)
	}
	// Single-use: the second consume must fail.
	if _, err := svc.Consume(ctx, token); err != ErrTokenInvalid {
		t.Fatalf("second consume: got %v, want ErrTokenInvalid", err)
	}
	if _, err := svc.Consume(ctx, "no-such-token"); err != ErrTokenInvalid {
		t.Fatalf("unknown token: got %v, want ErrTokenInvalid", err)
	}
}

func TestTokenHashIsStableAndNotTheToken(t *testing.T) {
	h1 := HashToken("abc")
	h2 := HashToken("abc")
	if h1 != h2 || h1 == "abc" || len(h1) != 64 {
		t.Fatalf("hash must be a stable 64-hex SHA-256, got %q", h1)
	}
	if HashToken("abd") == h1 {
		t.Fatal("different tokens must hash differently")
	}
}

func TestBindSession(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()
	token, _, _, err := svc.Issue(ctx, "lab-1", "user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	svc.BindSession(ctx, token, "sess-9")
	info, err := svc.Consume(ctx, token)
	if err != nil {
		t.Fatalf("consume after bind: %v", err)
	}
	if info.SessionID != "sess-9" {
		t.Fatalf("bound session id lost: %+v", info)
	}
}

func TestSimulatedShellBannerAndAllowlist(t *testing.T) {
	sh := NewShell(ShellConfig{LabID: "lab-1", Mode: "simulated"})
	banner := sh.Banner("lab-1")
	if !strings.Contains(strings.ToLower(banner), "simulated") {
		t.Fatalf("simulated shell banner must say simulated: %q", banner)
	}
	ctx := context.Background()
	out, exit := sh.Execute(ctx, "help")
	if exit || !strings.Contains(out, "aws") {
		t.Fatalf("help: exit=%v out=%q", exit, out)
	}
	out, _ = sh.Execute(ctx, "echo hello lab")
	if !strings.Contains(out, "hello lab") {
		t.Fatalf("echo: %q", out)
	}
	out, _ = sh.Execute(ctx, "rm -rf /")
	if !strings.Contains(out, "not on the allowlist") {
		t.Fatalf("non-allowlisted command must be rejected: %q", out)
	}
	out, _ = sh.Execute(ctx, "curl http://169.254.169.254/latest/")
	if !strings.Contains(out, "rejected") {
		t.Fatalf("curl to a non-lab host must be rejected: %q", out)
	}
	out, _ = sh.Execute(ctx, "terraform plan")
	if !strings.Contains(out, "simulated") {
		t.Fatalf("terraform in simulated mode must be labelled simulated: %q", out)
	}
	out, exit = sh.Execute(ctx, "exit")
	if !exit {
		t.Fatalf("exit must request disconnect: %q", out)
	}
}

func TestParseClientMessage(t *testing.T) {
	m, ok := ParseClientMessage(`{"type":"resize","cols":80,"rows":24}`)
	if !ok || m.Type != "resize" || m.Cols != 80 || m.Rows != 24 {
		t.Fatalf("resize parse: %+v %v", m, ok)
	}
	if _, ok := ParseClientMessage(`{"type":"ping"}`); !ok {
		t.Fatal("ping must parse")
	}
	if _, ok := ParseClientMessage("ls -la"); ok {
		t.Fatal("raw keystrokes must not parse as a JSON message")
	}
	if _, ok := ParseClientMessage(`{"type":"unknown"}`); ok {
		t.Fatal("unknown message types must not parse")
	}
}
