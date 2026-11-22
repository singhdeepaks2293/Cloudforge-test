package phase6

import (
	"strings"
	"testing"
)

func TestValidateAthenaSQL(t *testing.T) {
	if err := ValidateAthenaSQL("SELECT id, name FROM users WHERE id = 1"); err != nil {
		t.Fatalf("valid SELECT rejected: %v", err)
	}
	if err := ValidateAthenaSQL("WITH x AS (SELECT 1 AS n) SELECT * FROM x"); err != nil {
		t.Fatalf("valid WITH rejected: %v", err)
	}
	for _, bad := range []string{
		"DELETE FROM users",
		"SELECT * FROM users; DROP TABLE users",
		"INSERT INTO users VALUES (1)",
		"UPDATE users SET name='x'",
		"CREATE TABLE x (id INT)",
		"",
		"SELECT * FROM users WHERE name = 'x'; SELECT 1",
	} {
		if err := ValidateAthenaSQL(bad); err == nil {
			t.Fatalf("query %q was accepted, want rejection", bad)
		}
	}
}

func TestTrainLinearHousing(t *testing.T) {
	rows := BuiltinDatasets["housing"]
	m, err := TrainLinear(rows, []string{"size_sqft", "rooms"}, "price_usd", "housing")
	if err != nil {
		t.Fatalf("TrainLinear: %v", err)
	}
	if m.Rows != len(rows) {
		t.Fatalf("rows %d, want %d", m.Rows, len(rows))
	}
	// R² must be high for this near-linear teaching data, and the
	// prediction for a mid-range house must be in a sane band — both
	// are computed from the data, not asserted from a magic constant
	// beyond a broad sanity range.
	if m.R2 < 0.9 {
		t.Fatalf("R2 %f, want >= 0.9 for near-linear data", m.R2)
	}
	pred := m.Predict([]float64{1500, 3})
	if pred < 250000 || pred > 350000 {
		t.Fatalf("prediction for 1500sqft/3br = %f, want roughly 250k-350k", pred)
	}
	if m.RMSE <= 0 || m.RMSE > 30000 {
		t.Fatalf("RMSE %f out of sane range", m.RMSE)
	}
}

func TestTrainLinearDegenerate(t *testing.T) {
	// Constant feature column -> singular matrix -> honest error,
	// never a fabricated model.
	rows := [][]float64{{1, 10}, {1, 10}, {1, 10}, {1, 10}}
	if _, err := TrainLinear(rows, []string{"x"}, "y", "degenerate"); err == nil {
		t.Fatal("degenerate data trained without error")
	}
	if _, err := TrainLinear([][]float64{{1, 2}}, []string{"x"}, "y", "tiny"); err == nil {
		t.Fatal("2-row dataset trained without error")
	}
}

func TestEvaluateWAF(t *testing.T) {
	rules := []WAFRule{
		{Name: "block-admin", Action: "block", MatchType: "path_prefix", MatchValue: "/admin"},
		{Name: "block-bad-ip", Action: "block", MatchType: "ip_prefix", MatchValue: "203.0.113.0/24"},
	}
	act, name, matched := EvaluateWAF(rules, WAFRequest{Method: "GET", Path: "/admin/panel", SourceIP: "10.0.0.1"})
	if act != "block" || name != "block-admin" || !matched {
		t.Fatalf("admin path: got %s/%s/%v", act, name, matched)
	}
	act, _, _ = EvaluateWAF(rules, WAFRequest{Method: "GET", Path: "/ok", SourceIP: "203.0.113.5"})
	if act != "block" {
		t.Fatalf("bad IP: got %s, want block", act)
	}
	act, _, matched = EvaluateWAF(rules, WAFRequest{Method: "GET", Path: "/ok", SourceIP: "10.0.0.1"})
	if act != "allow" || matched {
		t.Fatalf("clean request: got %s matched=%v, want allow/unmatched", act, matched)
	}
}

func TestEvaluateFirewall(t *testing.T) {
	rules := []FirewallRule{
		{Name: "allow-ssh-office", Priority: 100, Action: "allow", Protocol: "tcp", SourceCIDR: "10.0.0.0/8", DestCIDR: "0.0.0.0/0", PortRange: "22"},
		{Name: "deny-all", Priority: 9999, Action: "deny", Protocol: "any", SourceCIDR: "0.0.0.0/0", DestCIDR: "0.0.0.0/0", PortRange: ""},
	}
	act, name := EvaluateFirewall(rules, FirewallPacket{SourceIP: "10.1.2.3", DestIP: "10.0.0.9", DestPort: 22, Protocol: "tcp"})
	if act != "allow" || name != "allow-ssh-office" {
		t.Fatalf("ssh from office: got %s/%s", act, name)
	}
	act, _ = EvaluateFirewall(rules, FirewallPacket{SourceIP: "203.0.113.1", DestIP: "10.0.0.9", DestPort: 22, Protocol: "tcp"})
	if act != "deny" {
		t.Fatalf("ssh from outside: got %s, want deny", act)
	}
	// Default deny with no rules at all.
	act, name = EvaluateFirewall(nil, FirewallPacket{SourceIP: "10.0.0.1", DestIP: "10.0.0.2", DestPort: 80, Protocol: "tcp"})
	if act != "deny" || name != "" {
		t.Fatalf("no rules: got %s/%s, want deny/empty", act, name)
	}
}

func TestEnvelopeCryptoRoundTrip(t *testing.T) {
	master := MasterKey("test-master-secret")
	raw := []byte("0123456789abcdef0123456789abcdef")
	wrapped, err := WrapKey(master, raw)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	if strings.Contains(wrapped, string(raw)) {
		t.Fatal("wrapped key leaks plaintext")
	}
	back, err := UnwrapKey(master, wrapped)
	if err != nil {
		t.Fatalf("UnwrapKey: %v", err)
	}
	if string(back) != string(raw) {
		t.Fatal("round trip mismatch")
	}
	sealed, err := EncryptValue(raw, "top-secret-value")
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}
	plain, err := DecryptValue(raw, sealed)
	if err != nil || plain != "top-secret-value" {
		t.Fatalf("DecryptValue: %q, %v", plain, err)
	}
	if _, err := DecryptValue([]byte("11111111111111111111111111111111"), sealed); err == nil {
		t.Fatal("decrypt with wrong key succeeded")
	}
}

func TestComputeCost(t *testing.T) {
	lines := ComputeCost(DefaultPricing, map[string]float64{"Micro": 10}, 2, 5000, 2000, 5, 3)
	total := CostTotal(lines)
	// 10*0.01 + 2*0.023 + 5*0.0004 + 2*0.0002 + 5*0.015 + 3*0.01
	want := 0.10 + 0.046 + 0.002 + 0.0004 + 0.075 + 0.03
	if total < want-1e-9 || total > want+1e-9 {
		t.Fatalf("total %f, want %f", total, want)
	}
	if got := len(ComputeCost(DefaultPricing, map[string]float64{}, 0, 0, 0, 0, 0)); got != 0 {
		t.Fatalf("zero usage produced %d lines, want 0", got)
	}
}

func TestKinesisShardStable(t *testing.T) {
	if KinesisShard("order-1", 4) != KinesisShard("order-1", 4) {
		t.Fatal("shard assignment not stable")
	}
	if s := KinesisShard("x", 4); s < 0 || s >= 4 {
		t.Fatalf("shard %d out of range", s)
	}
}

func TestBedrockTemplateLabelled(t *testing.T) {
	out := BedrockTemplateResponse("anthropic.claude-3", "hello world")
	if !strings.Contains(out, "SIMULATED") || !strings.Contains(out, "no model ran") {
		t.Fatalf("template response is not honestly labelled: %q", out)
	}
	if strings.Contains(out, "hello world") == false {
		t.Fatalf("template should echo the prompt: %q", out)
	}
}
