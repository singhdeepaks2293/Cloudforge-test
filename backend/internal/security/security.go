// Package security holds the shared guards every CloudForge
// component uses instead of re-implementing them inconsistently:
//
//   - SSRF protection for provider endpoint URLs
//     (ValidateEndpointURL / ValidateEndpointHost): only loopback
//     hosts are accepted, plus explicitly named emulator hosts
//     (e.g. the Docker service name "minio") when the caller opts
//     in. The Terraform runner's ValidateEndpoint delegates here,
//     so the same rule is enforced everywhere.
//   - Secrets redaction for logs and audit metadata
//     (RedactString / ScrubMap / Secret): values that look like
//     credentials are replaced with "<redacted>" before they can
//     reach a log line or an audit row.
//
// Nothing in this package imports a CloudForge package, so any
// component can use it without creating an import cycle.
package security

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// --- SSRF protection ------------------------------------------------------

// ValidateEndpointURL SSRF-guards a provider endpoint URL: only
// http(s) URLs whose host is loopback (localhost, 127.0.0.0/8, ::1)
// are accepted, plus any host named in allowedHosts (exact,
// case-insensitive match — intended for Docker service names such
// as "minio", which are operator-configured, not user input).
// Anything else — public hosts, cloud metadata addresses
// (169.254.169.254), private LAN addresses, non-http schemes,
// embedded credentials — is refused.
//
// "localhost.localdomain" and similar lookalikes are NOT loopback:
// only the exact name "localhost" and loopback IPs pass.
func ValidateEndpointURL(raw string, allowedHosts ...string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("endpoint is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("endpoint %q is not a valid URL: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint %q must use http or https", raw)
	}
	if u.User != nil {
		return fmt.Errorf("endpoint %q must not embed credentials", raw)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint %q has no host", raw)
	}
	if isLoopbackHost(host) {
		return nil
	}
	for _, h := range allowedHosts {
		if strings.EqualFold(host, h) {
			return nil
		}
	}
	return fmt.Errorf("endpoint host %q is not loopback (only localhost / 127.0.0.0/8 / ::1 are allowed)", host)
}

// ValidateEndpointHost SSRF-guards a bare host:port endpoint (the
// MinIO form, "localhost:9000" — no scheme). The same loopback rule
// applies, plus allowedHosts. A bare hostname without a port is
// accepted and checked the same way.
func ValidateEndpointHost(hostPort string, allowedHosts ...string) error {
	hostPort = strings.TrimSpace(hostPort)
	if hostPort == "" {
		return errors.New("endpoint is empty")
	}
	// A host:port value must not carry a path, query or credentials.
	if strings.ContainsAny(hostPort, "/?@") {
		return fmt.Errorf("endpoint %q must be a plain host:port", hostPort)
	}
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	if host == "" {
		return fmt.Errorf("endpoint %q has no host", hostPort)
	}
	// IPv6 literals need brackets in URL form.
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return ValidateEndpointURL("http://"+host, allowedHosts...)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	// 127.0.0.0/8 is the loopback range; IsLoopback covers
	// 127.0.0.1 only, so check the /8 explicitly.
	if v4 := ip.To4(); v4 != nil && v4[0] == 127 {
		return true
	}
	return false
}

// --- Secrets redaction ----------------------------------------------------

// sensitiveKeys are audit/log metadata keys whose values are never
// recorded in clear text.
var sensitiveKeys = map[string]bool{
	"token": true, "access_token": true, "refresh_token": true,
	"id_token": true, "secret": true, "secret_key": true,
	"access_key": true, "api_key": true, "apikey": true,
	"password": true, "passwd": true, "authorization": true,
	"auth": true, "jwt": true, "bearer": true,
	"private_key": true, "session": true, "cookie": true,
	"client_secret": true,
}

func isSensitiveKey(k string) bool {
	return sensitiveKeys[strings.ToLower(strings.TrimSpace(k))]
}

var (
	// postgres://user:password@host — mask the password only.
	credentialURL = regexp.MustCompile(`(?i)(://[^:/\s]+:)([^@\s]+)(@)`)
	// Bearer <token> / Basic <credentials> in a header value.
	bearerValue = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	// key=value / key: value / "key": "value" assignment forms.
	// The value stops at whitespace, quotes, ; , } or & (so a
	// token=... in a query string does not swallow the next param).
	credentialKV = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key|access[_-]?key|client[_-]?secret)(["'\s:=]+)([^\s"',;}&#]+)`)
)

// RedactString masks credential-looking material inside a free-form
// string: passwords in URLs, Bearer/Basic header values, and
// password=/token=/secret=/api_key= assignments. Text without
// credentials passes through unchanged.
func RedactString(s string) string {
	s = credentialURL.ReplaceAllString(s, `${1}<redacted>${3}`)
	s = bearerValue.ReplaceAllString(s, `${1} <redacted>`)
	s = credentialKV.ReplaceAllString(s, `${1}${2}<redacted>`)
	return s
}

// RedactURL removes the password from a URL for logging (keeps the
// username, host and path). An unparseable value is fully redacted
// rather than leaked.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable-url>"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// ScrubMap returns a copy of m with sensitive values redacted: any
// value under a sensitive key becomes "<redacted>", string values
// are passed through RedactString, and nested maps are scrubbed
// recursively. The input map is never modified.
func ScrubMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			out[k] = "<redacted>"
			continue
		}
		out[k] = scrubValue(v)
	}
	return out
}

func scrubValue(v any) any {
	switch t := v.(type) {
	case string:
		return RedactString(t)
	case map[string]any:
		return ScrubMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = scrubValue(e)
		}
		return out
	default:
		return v
	}
}

// Secret wraps a credential for slog: it always renders as
// "<redacted>", so a secret passed as a log attribute can never
// leak, even if a caller forgets to redact it first.
//
//	slog.Info("login", "user", u.Email, "token", security.Secret(tok))
func Secret(v string) slog.LogValuer {
	return secretValue(v)
}

type secretValue string

func (secretValue) LogValue() slog.Value { return slog.StringValue("<redacted>") }
