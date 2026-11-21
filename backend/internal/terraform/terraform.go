// Package terraform runs Terraform (or OpenTofu) for a lab, isolated
// in a Docker container, and parses `terraform show -json` plan output.
//
// Isolation rules (Milestone 7):
//   - Terraform NEVER runs on the host. The only executor is
//     ContainerRunner, which talks to the Docker Engine API over the
//     Unix socket — the same mechanism as the host-agent Docker client
//     (internal/hostagent/docker.go) — and runs the pinned image
//     hashicorp/terraform:1.9.8 (OpenTofu opentofu/opentofu:1.9.2 is
//     the documented alternative; see DefaultImage / AlternativeImage).
//   - The container gets no host mounts except the lab workdir, no
//     network except what the provider endpoint needs (bridge network
//     with host.docker.internal pointing at the emulator on the host),
//     and resource limits (1 vCPU, 512 MiB).
//   - Provider config points only at the configured emulator/MinIO
//     endpoints. ValidateEndpoint SSRF-guards the endpoint URL:
//     only loopback hosts are accepted, and real AWS credentials are
//     refused (see ValidateProviderConfig).
//   - When Docker is absent (this sandbox) ContainerRunner returns a
//     structured *UnavailableError and the API answers 503 — never a
//     faked plan or apply.
//
// TestStubRunner is a test-only Runner (labelled as such in every
// result it produces). Production code never constructs it.
package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/security"
)

// DefaultImage is the pinned Terraform image ContainerRunner uses.
const DefaultImage = "hashicorp/terraform:1.9.8"

// AlternativeImage is the documented OpenTofu alternative; set it via
// TERRAFORM_IMAGE to use it instead of DefaultImage.
const AlternativeImage = "opentofu/opentofu:1.9.2"

// DefaultDockerSocket is the Docker daemon socket ContainerRunner dials.
const DefaultDockerSocket = "/var/run/docker.sock"

// PlanTTL bounds an approved plan: an apply must run within this long
// of the plan succeeding.
const PlanTTL = time.Hour

// UnavailableError means the Terraform backend (Docker) is not
// present on this host. It is never a faked success.
type UnavailableError struct {
	Backend string
	Reason  string
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("terraform backend %q unavailable: %s", e.Backend, e.Reason)
}

// IsUnavailable reports whether err is (or wraps) an UnavailableError.
func IsUnavailable(err error) bool {
	var ue *UnavailableError
	return errors.As(err, &ue)
}

// Operation is one terraform action.
type Operation string

const (
	OpInit     Operation = "init"
	OpValidate Operation = "validate"
	OpPlan     Operation = "plan"
	OpApply    Operation = "apply"
	OpDestroy  Operation = "destroy"
)

// RunRequest is one terraform execution inside a lab workdir.
type RunRequest struct {
	Operation Operation
	Workdir   string // absolute lab workdir (the only host mount)
	// Configuration is the lab's Terraform HCL (main.tf content).
	// Empty means "provider only": a real plan with zero resources.
	Configuration string
	// Endpoints are the provider endpoints (already validated).
	EmulatorEndpoint string
	MinIOEndpoint    string
	// Env holds extra environment for the container; AWS credentials
	// must never be placed here (see ValidateProviderConfig).
	Env map[string]string
}

// RunResult is the outcome of one terraform execution.
type RunResult struct {
	Operation Operation `json:"operation"`
	Success   bool      `json:"success"`
	Output    string    `json:"output"` // combined stdout/stderr, truncated to 64 KiB
	// Plan is set for a successful plan: the parsed `terraform show
	// -json` output (see ParsePlanJSON). PlanJSON is the raw parsed
	// document for storage.
	Plan     *Plan          `json:"plan,omitempty"`
	PlanJSON map[string]any `json:"plan_json,omitempty"`
	// Stub is true when the result came from TestStubRunner.
	Stub bool `json:"stub,omitempty"`
}

// Runner executes terraform operations.
type Runner interface {
	// Available reports whether the runner can execute right now
	// (Docker present and answering). The second return explains why
	// not; callers surface it in a 503.
	Available(ctx context.Context) (bool, string)
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// --- plan JSON ------------------------------------------------------------

// PlannedResource is one resource in a parsed plan.
type PlannedResource struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Name    string `json:"name"`
}

// Plan is the validated summary of a `terraform show -json` document.
type Plan struct {
	FormatVersion    string            `json:"format_version"`
	TerraformVersion string            `json:"terraform_version"`
	Resources        []PlannedResource `json:"resources"`
	// ResourceChanges counts actions: create/update/delete/no-op.
	ActionCounts map[string]int `json:"action_counts"`
}

// ParsePlanJSON parses and validates `terraform show -json` output.
// It requires format_version and planned_values; a document missing
// either is not a plan and is rejected rather than stored.
func ParsePlanJSON(data []byte) (*Plan, map[string]any, error) {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("plan JSON is not valid JSON: %w", err)
	}
	fv, _ := doc["format_version"].(string)
	if fv == "" {
		return nil, nil, errors.New("plan JSON has no format_version: not a `terraform show -json` document")
	}
	pv, ok := doc["planned_values"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("plan JSON has no planned_values object")
	}
	plan := &Plan{FormatVersion: fv, ActionCounts: map[string]int{}}
	if v, ok := doc["terraform_version"].(string); ok {
		plan.TerraformVersion = v
	}
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		if res, ok := m["resources"].([]any); ok {
			for _, item := range res {
				rm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				addr, _ := rm["address"].(string)
				typ, _ := rm["type"].(string)
				name, _ := rm["name"].(string)
				if addr == "" && typ == "" {
					continue
				}
				plan.Resources = append(plan.Resources, PlannedResource{Address: addr, Type: typ, Name: name})
			}
		}
		// planned_values nests resources under root_module.
		if rm, ok := m["root_module"].(map[string]any); ok {
			walk(rm)
		}
		if child, ok := m["child_modules"].([]any); ok {
			for _, c := range child {
				if cm, ok := c.(map[string]any); ok {
					walk(cm)
				}
			}
		}
	}
	walk(pv)
	if changes, ok := doc["resource_changes"].([]any); ok {
		for _, item := range changes {
			cm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			change, _ := cm["change"].(map[string]any)
			actions, _ := change["actions"].([]any)
			for _, a := range actions {
				if s, ok := a.(string); ok {
					plan.ActionCounts[s]++
				}
			}
		}
	}
	if plan.Resources == nil {
		plan.Resources = []PlannedResource{}
	}
	// Return a plain (non-UseNumber) copy for storage.
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, nil, fmt.Errorf("plan JSON re-parse failed: %w", err)
	}
	return plan, stored, nil
}

// --- endpoint / credential validation -------------------------------------

// ValidateEndpoint SSRF-guards a provider endpoint URL: only http(s)
// URLs whose host is loopback (localhost, 127.0.0.0/8, ::1) are
// accepted. Anything else — public hosts, cloud metadata addresses,
// non-http schemes, embedded credentials — is refused.
//
// It delegates to security.ValidateEndpointURL so every component
// enforces the same rule; the Terraform runner passes no extra
// allowed hosts, so its rule stays loopback-only.
func ValidateEndpoint(raw string) error {
	return security.ValidateEndpointURL(raw)
}

// ValidateProviderConfig checks the emulator/MinIO endpoints and
// refuses real AWS credentials: when AWS_ACCESS_KEY_ID /
// AWS_SECRET_ACCESS_KEY are set in the process environment to values
// other than the emulator dev defaults, the run is refused unless the
// endpoint is loopback (in which case the credentials can only reach
// the local emulator). A non-loopback endpoint is always refused.
func ValidateProviderConfig(emulatorEndpoint, minIOEndpoint string) error {
	if err := ValidateEndpoint(emulatorEndpoint); err != nil {
		return fmt.Errorf("emulator endpoint: %w", err)
	}
	if minIOEndpoint != "" {
		// MinIO is host:port, not a URL.
		if strings.Contains(minIOEndpoint, "://") {
			if err := ValidateEndpoint(minIOEndpoint); err != nil {
				return fmt.Errorf("minio endpoint: %w", err)
			}
		} else {
			host, _, err := net.SplitHostPort(minIOEndpoint)
			if err != nil {
				host = minIOEndpoint
			}
			if err := ValidateEndpoint("http://" + host); err != nil {
				return fmt.Errorf("minio endpoint: %w", err)
			}
		}
	}
	for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		if v := os.Getenv(key); v != "" && !isEmulatorDefaultCredential(v) {
			return fmt.Errorf("%s is set to a non-emulator value; refusing to run Terraform with credentials that could reach real AWS (unset it or use the local emulator defaults)", key)
		}
	}
	return nil
}

func isEmulatorDefaultCredential(v string) bool {
	switch v {
	case "minioadmin", "test", "localstack", "foobar":
		return true
	default:
		return false
	}
}

// ValidateConfiguration rejects HCL that tries to reach real AWS:
// literal amazonaws.com hosts or embedded access keys. Provider
// blocks are rewritten by WriteProviderConfig in any case.
func ValidateConfiguration(hcl string) error {
	if len(hcl) > 64*1024 {
		return errors.New("terraform configuration exceeds the 64 KiB limit")
	}
	lower := strings.ToLower(hcl)
	if strings.Contains(lower, "amazonaws.com") {
		return errors.New("terraform configuration references amazonaws.com; only the local emulator endpoints are allowed")
	}
	if strings.Contains(lower, "aws_access_key") || strings.Contains(lower, "aws_secret_key") {
		return errors.New("terraform configuration must not embed AWS credentials")
	}
	return nil
}

// WriteProviderConfig writes the per-lab provider configuration into
// workdir: an AWS provider pinned to the emulator endpoints with
// dummy local credentials and all real-AWS validation skipped. It
// never writes real credentials. The file is provider.tf; the lab's
// own configuration lives in main.tf (written by the caller).
func WriteProviderConfig(workdir, emulatorEndpoint, minIOEndpoint string) error {
	if err := ValidateProviderConfig(emulatorEndpoint, minIOEndpoint); err != nil {
		return err
	}
	if err := os.MkdirAll(workdir, 0o750); err != nil {
		return fmt.Errorf("create terraform workdir: %w", err)
	}
	s3Endpoint := minIOEndpoint
	if s3Endpoint != "" && !strings.HasPrefix(s3Endpoint, "http") {
		s3Endpoint = "http://" + s3Endpoint
	}
	content := fmt.Sprintf(`# Generated by CloudForge (Milestone 7). Do not edit: this file pins
# the AWS provider to the LOCAL emulator endpoints. Real AWS is never
# configured here; credentials below are the local emulator defaults.
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    ec2 = %q
    s3  = %q
  }

  s3_use_path_style = true
}
`, emulatorEndpoint, s3Endpoint)
	return os.WriteFile(filepath.Join(workdir, "provider.tf"), []byte(content), 0o640)
}

// --- ContainerRunner ------------------------------------------------------

// ContainerRunner runs terraform inside a Docker container via the
// Docker Engine API over the Unix socket.
type ContainerRunner struct {
	Image      string
	SocketPath string
	HTTP       *http.Client
}

// NewContainerRunner builds a runner for image (DefaultImage when
// empty) and socket (DefaultDockerSocket when empty). Construction
// performs no I/O; availability is checked per call.
func NewContainerRunner(image, socketPath string) *ContainerRunner {
	if image == "" {
		image = DefaultImage
	}
	if socketPath == "" {
		socketPath = DefaultDockerSocket
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &ContainerRunner{Image: image, SocketPath: socketPath,
		HTTP: &http.Client{Transport: transport, Timeout: 5 * time.Minute}}
}

// Available checks the socket exists and the daemon answers /_ping.
func (r *ContainerRunner) Available(ctx context.Context) (bool, string) {
	if _, err := os.Stat(r.SocketPath); err != nil {
		return false, fmt.Sprintf("Docker socket %s not present (%v); install Docker and start the daemon", r.SocketPath, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.call(pingCtx, http.MethodGet, "/_ping", nil, nil); err != nil {
		return false, fmt.Sprintf("Docker daemon did not answer /_ping on %s: %v", r.SocketPath, err)
	}
	return true, "Docker daemon reachable on " + r.SocketPath
}

type dockerError struct {
	Message string `json:"message"`
}

func (r *ContainerRunner) call(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal docker request: %w", err)
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost/v1.41"+path, rdr)
	if err != nil {
		return fmt.Errorf("build docker request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return &UnavailableError{Backend: "docker", Reason: fmt.Sprintf("Docker daemon unreachable on %s: %v", r.SocketPath, err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read docker response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var de dockerError
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &de) == nil && de.Message != "" {
			msg = de.Message
		}
		return fmt.Errorf("docker API %s %s returned %d: %s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode docker response: %w", err)
		}
	}
	return nil
}

// Run executes one terraform operation in a fresh container. The
// workdir must already exist with provider.tf (+ main.tf when the lab
// supplied configuration); the container mounts only that directory,
// runs with 1 vCPU / 512 MiB, and is removed afterwards. Plan output
// is captured via `terraform show -json` inside the same container
// run (the plan command writes plan.tfplan, show renders it).
func (r *ContainerRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if ok, reason := r.Available(ctx); !ok {
		return RunResult{}, &UnavailableError{Backend: "docker", Reason: reason}
	}
	if req.Workdir == "" {
		return RunResult{}, errors.New("terraform workdir is required")
	}
	if strings.Contains(req.Workdir, "..") {
		return RunResult{}, errors.New("terraform workdir must not contain ..")
	}
	cmd, err := commandFor(req.Operation)
	if err != nil {
		return RunResult{}, err
	}
	// The container runs: terraform <op> … ; for plan, also render
	// the JSON plan so the caller can parse and store it.
	shell := "set -e; cd /work; terraform " + strings.Join(cmd, " ")
	if req.Operation == OpPlan {
		shell += "; terraform show -json plan.tfplan > plan.json"
	}
	createBody := map[string]any{
		"Image":      r.Image,
		"Cmd":        []string{"sh", "-c", shell},
		"WorkingDir": "/work",
		"Env":        envList(req.Env),
		"HostConfig": map[string]any{
			"Binds":      []string{req.Workdir + ":/work"},
			"NanoCpus":   1_000_000_000,
			"Memory":     512 * 1024 * 1024,
			"AutoRemove": false,
			"ExtraHosts": []string{"host.docker.internal:host-gateway"},
		},
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := r.call(ctx, http.MethodPost, "/containers/create", createBody, &created); err != nil {
		return RunResult{}, fmt.Errorf("docker create terraform container: %w", err)
	}
	defer func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.call(ctx2, http.MethodDelete, "/containers/"+created.ID+"?force=1", nil, nil)
	}()
	if err := r.call(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		return RunResult{}, fmt.Errorf("docker start terraform container: %w", err)
	}
	// Wait for exit (bounded by the HTTP client timeout).
	var wait struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := r.call(ctx, http.MethodPost, "/containers/"+created.ID+"/wait?condition=next-exit", nil, &wait); err != nil {
		return RunResult{}, fmt.Errorf("docker wait terraform container: %w", err)
	}
	logs, err := r.logs(ctx, created.ID)
	if err != nil {
		return RunResult{}, err
	}
	res := RunResult{Operation: req.Operation, Success: wait.StatusCode == 0, Output: truncate(logs, 64*1024)}
	if !res.Success {
		return res, fmt.Errorf("terraform %s exited %d: %s", req.Operation, wait.StatusCode, truncate(logs, 1024))
	}
	if req.Operation == OpPlan {
		planRaw, err := r.readFile(ctx, created.ID, "/work/plan.json")
		if err != nil {
			return res, fmt.Errorf("read plan.json from terraform container: %w", err)
		}
		plan, stored, err := ParsePlanJSON(planRaw)
		if err != nil {
			return res, fmt.Errorf("terraform plan produced unparseable JSON: %w", err)
		}
		res.Plan = plan
		res.PlanJSON = stored
	}
	return res, nil
}

func (r *ContainerRunner) logs(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/v1.41/containers/"+id+"/logs?stdout=1&stderr=1", nil)
	if err != nil {
		return "", err
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return "", &UnavailableError{Backend: "docker", Reason: fmt.Sprintf("docker logs unreachable: %v", err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// Docker multiplexes stdout/stderr with an 8-byte header per
	// frame; strip it so the output is readable text.
	return demuxLogs(data), nil
}

func demuxLogs(data []byte) string {
	var out bytes.Buffer
	for len(data) >= 8 {
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		if size < 0 || 8+size > len(data) {
			break
		}
		out.Write(data[8 : 8+size])
		data = data[8+size:]
	}
	if out.Len() == 0 {
		return string(data)
	}
	return out.String()
}

// readFile copies one file out of a container (GET /archive).
func (r *ContainerRunner) readFile(ctx context.Context, id, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/v1.41/containers/"+id+"/archive?path="+url.QueryEscape(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker archive %s returned %d", path, resp.StatusCode)
	}
	// The archive is a tar stream; plan.json is small, so extract the
	// single file by scanning for its content after the tar header.
	// (A full tar reader would need archive/tar; this scan is enough
	// for a one-file JSON document and fails loudly otherwise.)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractTarFile(data, "plan.json")
}

func extractTarFile(tar []byte, name string) ([]byte, error) {
	// Minimal tar scan: 512-byte headers, name at offset 0, size
	// octal at offset 124.
	for off := 0; off+512 <= len(tar); {
		header := tar[off : off+512]
		if header[0] == 0 {
			break
		}
		fname := strings.TrimRight(string(header[:100]), "\x00")
		sizeStr := strings.TrimRight(string(header[124:136]), "\x00 ")
		var size int64
		_, _ = fmt.Sscanf(sizeStr, "%o", &size)
		dataStart := off + 512
		if dataStart+int(size) > len(tar) {
			return nil, fmt.Errorf("tar entry %q is truncated", fname)
		}
		if filepath.Base(fname) == name {
			return tar[dataStart : dataStart+int(size)], nil
		}
		off = dataStart + int((size+511)/512*512)
	}
	return nil, fmt.Errorf("file %q not found in container archive", name)
}

func commandFor(op Operation) ([]string, error) {
	switch op {
	case OpInit:
		return []string{"init", "-input=false", "-no-color"}, nil
	case OpValidate:
		return []string{"validate", "-no-color"}, nil
	case OpPlan:
		return []string{"plan", "-input=false", "-no-color", "-out=plan.tfplan"}, nil
	case OpApply:
		return []string{"apply", "-input=false", "-auto-approve", "-no-color", "plan.tfplan"}, nil
	case OpDestroy:
		return []string{"destroy", "-input=false", "-auto-approve", "-no-color"}, nil
	default:
		return nil, fmt.Errorf("unknown terraform operation %q", op)
	}
}

func envList(env map[string]string) []string {
	var out []string
	for k, v := range env {
		// Never pass AWS credentials into the container.
		if strings.HasPrefix(k, "AWS_") {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…[truncated]"
}

// --- TestStubRunner -------------------------------------------------------

// TestStubRunner is a TEST-ONLY Runner. Every result it returns is
// labelled Stub=true and its plan JSON is a fixed canned document.
// Production code must never construct it; httpapi tests and the
// jobs tests use it so the terraform API/worker paths are exercised
// without Docker.
type TestStubRunner struct {
	// Calls records every Run invocation (for assertions).
	Calls []RunRequest
	// FailOperation makes Run fail for that operation.
	FailOperation map[Operation]error
	// PlanJSON overrides the canned plan document.
	PlanJSON string
}

func (s *TestStubRunner) Available(ctx context.Context) (bool, string) {
	return true, "test stub runner (test only)"
}

func (s *TestStubRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	s.Calls = append(s.Calls, req)
	if err := s.FailOperation[req.Operation]; err != nil {
		return RunResult{Operation: req.Operation, Success: false, Output: err.Error(), Stub: true}, err
	}
	res := RunResult{Operation: req.Operation, Success: true, Stub: true,
		Output: "test stub runner: terraform " + string(req.Operation) + " succeeded (test only, nothing was executed)"}
	if req.Operation == OpPlan {
		doc := s.PlanJSON
		if doc == "" {
			doc = `{"format_version":"1.2","terraform_version":"1.9.8","planned_values":{"root_module":{"resources":[
				{"address":"aws_vpc.lab","type":"aws_vpc","name":"lab"},
				{"address":"aws_subnet.public","type":"aws_subnet","name":"public"},
				{"address":"aws_instance.web","type":"aws_instance","name":"web"}]}},
				"resource_changes":[
				{"address":"aws_vpc.lab","change":{"actions":["create"]}},
				{"address":"aws_subnet.public","change":{"actions":["create"]}},
				{"address":"aws_instance.web","change":{"actions":["create"]}}]}`
		}
		plan, stored, err := ParsePlanJSON([]byte(doc))
		if err != nil {
			return res, err
		}
		res.Plan = plan
		res.PlanJSON = stored
	}
	return res, nil
}
