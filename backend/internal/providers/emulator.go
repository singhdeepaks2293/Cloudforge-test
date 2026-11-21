package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/security"
)

// DefaultEmulatorEndpoint is used when AWS_EMULATOR_ENDPOINT is unset.
const DefaultEmulatorEndpoint = "http://localhost:4566"

// EmulatorStatus is the result of probing an AWS emulator endpoint
// (LocalStack or similar). Reachable=false means nothing is listening
// there: every emulator-backed operation is then UNSUPPORTED /
// unavailable with this reason, never silently ok.
type EmulatorStatus struct {
	Endpoint  string            `json:"endpoint"`
	Reachable bool              `json:"reachable"`
	Services  map[string]string `json:"services,omitempty"`
	Version   string            `json:"version,omitempty"`
	Detail    string            `json:"detail"`
	CheckedAt time.Time         `json:"checked_at"`
}

// ProbeEmulator health-probes an AWS emulator endpoint. For
// LocalStack it reads GET {endpoint}/_localstack/health, which
// reports per-service availability ("available" / "running" /
// "disabled" ...). Any other 2xx response counts as "an HTTP server
// is listening but it did not identify itself as LocalStack".
// Connection failure, timeout or non-2xx is unreachable.
//
// The endpoint is SSRF-guarded first: a non-loopback endpoint is
// refused without any request being made, so a misconfigured or
// hostile endpoint can never make this probe fetch an arbitrary host.
func ProbeEmulator(ctx context.Context, endpoint string) EmulatorStatus {
	if endpoint == "" {
		endpoint = DefaultEmulatorEndpoint
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	st := EmulatorStatus{Endpoint: endpoint, CheckedAt: time.Now().UTC()}
	if err := security.ValidateEndpointURL(endpoint); err != nil {
		st.Detail = "emulator endpoint refused (SSRF guard): " + err.Error()
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	client := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/_localstack/health", nil)
	if err != nil {
		st.Detail = "invalid emulator endpoint: " + err.Error()
		return st
	}
	resp, err := client.Do(req)
	if err != nil {
		st.Detail = fmt.Sprintf("no AWS emulator reachable at %s: %v", endpoint, err)
		return st
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		st.Detail = fmt.Sprintf("emulator at %s answered HTTP %d (not a LocalStack health endpoint)", endpoint, resp.StatusCode)
		return st
	}
	st.Reachable = true
	var parsed struct {
		Services map[string]string `json:"services"`
		Version  string            `json:"version"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		st.Detail = fmt.Sprintf("an HTTP server answered at %s but its health payload is not LocalStack JSON", endpoint)
		return st
	}
	st.Services = parsed.Services
	st.Version = parsed.Version
	st.Detail = fmt.Sprintf("LocalStack reachable at %s (%d services reported)", endpoint, len(parsed.Services))
	return st
}

// EmulatorHealthProbe adapts ProbeEmulator to the Registry probe
// shape: "ok" when the emulator answers, "unavailable" otherwise.
// Per-service availability from the LocalStack health payload is
// included in the detail; the registry's ec2 entries are the ones
// this probe drives.
func EmulatorHealthProbe(endpoint string) HealthProbe {
	return func(ctx context.Context) (string, string) {
		st := ProbeEmulator(ctx, endpoint)
		if !st.Reachable {
			return HealthUnavailable, st.Detail
		}
		return HealthOK, st.Detail
	}
}

// EmulatorEndpointFromEnv returns AWS_EMULATOR_ENDPOINT or the
// default LocalStack address.
func EmulatorEndpointFromEnv() string {
	if v := os.Getenv("AWS_EMULATOR_ENDPOINT"); v != "" {
		return v
	}
	return DefaultEmulatorEndpoint
}
