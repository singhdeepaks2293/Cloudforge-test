package security

import (
	"log/slog"
	"strings"
	"testing"
)

func TestValidateEndpointURL(t *testing.T) {
	for _, good := range []string{
		"http://localhost:4566", "http://localhost:9000",
		"http://127.0.0.1:4566", "http://127.0.0.9:8000", "http://[::1]:4566",
		"https://localhost:4566",
	} {
		if err := ValidateEndpointURL(good); err != nil {
			t.Fatalf("endpoint %q should be accepted: %v", good, err)
		}
	}
	for _, bad := range []string{
		"", "http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5:4566", "http://192.168.1.1:4566",
		"http://ec2.amazonaws.com/", "https://s3.amazonaws.com/",
		"ftp://localhost:21", "http://user:pass@localhost:4566",
		"http://localhost.localdomain:4566", "http://example.com/",
		"http://127.0.0.1.evil.com/", "http://0.0.0.0:4566",
	} {
		if err := ValidateEndpointURL(bad); err == nil {
			t.Fatalf("endpoint %q should be rejected (SSRF guard)", bad)
		}
	}
	// An explicitly allowed emulator host (Docker service name) passes,
	// but only when the caller opts in.
	if err := ValidateEndpointURL("http://minio:9000", "minio"); err != nil {
		t.Fatalf("allowed host minio should be accepted: %v", err)
	}
	if err := ValidateEndpointURL("http://minio:9000"); err == nil {
		t.Fatal("minio must be rejected without an explicit allow-list entry")
	}
}

func TestValidateEndpointHost(t *testing.T) {
	for _, good := range []string{"localhost:9000", "127.0.0.1:9000", "localhost", "[::1]:9000"} {
		if err := ValidateEndpointHost(good); err != nil {
			t.Fatalf("host %q should be accepted: %v", good, err)
		}
	}
	for _, bad := range []string{"", "example.com:9000", "10.0.0.5:9000", "user:pass@localhost:9000", "localhost:9000/path"} {
		if err := ValidateEndpointHost(bad); err == nil {
			t.Fatalf("host %q should be rejected", bad)
		}
	}
	if err := ValidateEndpointHost("minio:9000", "minio"); err != nil {
		t.Fatalf("allowed host minio:9000 should be accepted: %v", err)
	}
}

func TestRedactString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"postgres://cf:secret123@db:5432/cf", "postgres://cf:<redacted>@db:5432/cf"},
		{"Authorization: Bearer abc.def.ghi", "Authorization: Bearer <redacted>"},
		{`password="hunter2"`, `password="<redacted>"`},
		{"token=abc123&other=x", "token=<redacted>&other=x"},
		{"plain text with no secrets", "plain text with no secrets"},
		{"api_key: sk-live-123", "api_key: <redacted>"},
	}
	for _, c := range cases {
		if got := RedactString(c.in); got != c.want {
			t.Fatalf("RedactString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestScrubMap(t *testing.T) {
	in := map[string]any{
		"user": "ada", "token": "secret-token", "password": "hunter2",
		"detail": "login failed for postgres://cf:pw@db/cf",
		"nested": map[string]any{"api_key": "sk-1", "ok": "fine"},
		"list":   []any{"Bearer abc", "plain"},
	}
	out := ScrubMap(in)
	if out["token"] != "<redacted>" || out["password"] != "<redacted>" {
		t.Fatalf("sensitive keys must be redacted: %v", out)
	}
	if out["user"] != "ada" {
		t.Fatalf("non-sensitive keys must pass through: %v", out)
	}
	if d, _ := out["detail"].(string); !strings.Contains(d, "<redacted>") || strings.Contains(d, "pw@db") {
		t.Fatalf("credential URL inside a value must be redacted: %q", d)
	}
	nested, _ := out["nested"].(map[string]any)
	if nested["api_key"] != "<redacted>" || nested["ok"] != "fine" {
		t.Fatalf("nested maps must be scrubbed: %v", nested)
	}
	lst, _ := out["list"].([]any)
	if lst[0] != "Bearer <redacted>" || lst[1] != "plain" {
		t.Fatalf("lists must be scrubbed element-wise: %v", lst)
	}
	// Input must not be modified.
	if in["token"] != "secret-token" {
		t.Fatal("ScrubMap must not modify its input")
	}
	if ScrubMap(nil) != nil {
		t.Fatal("ScrubMap(nil) must be nil")
	}
}

func TestSecretLogValuer(t *testing.T) {
	v := Secret("hunter2").LogValue()
	if v.Kind() != slog.KindString || v.String() != "<redacted>" {
		t.Fatalf("Secret must render as <redacted>, got %v", v)
	}
}
