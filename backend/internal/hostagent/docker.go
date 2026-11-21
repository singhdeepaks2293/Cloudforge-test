// Docker host agent: real containers via a minimal typed HTTP client
// over the Docker daemon socket (/var/run/docker.sock).
//
// Why not github.com/docker/docker? It was tried first (v28.3.3): it
// fetched, but it does not build with Go 1.25 — `go mod tidy` pulled a
// go1.26 toolchain, ~25 transitive dependencies, and the build still
// failed (`undefined: sockets.DialPipe` in client/client.go, an API
// mismatch between the +incompatible v28.3.3 module and the resolved
// go-connections version). The Docker Engine API is a small, stable
// HTTP API, so this file implements the six calls CloudForge needs
// (list/create/start/stop/remove/inspect, plus exec and stats) as typed
// structs over the Unix socket, with no third-party dependency. The
// go.mod/go.sum were restored to their pre-Docker state after the
// failed attempt.
//
// When Docker is absent (no socket, or the daemon does not answer
// /_ping) every container operation returns a structured Unavailable
// error — never fake success. VM operations are not a Docker concern
// and return Unavailable with that reason.
package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	labelLabID  = "cloudforge.lab-id"
	labelOwner  = "cloudforge.owner"
	labelName   = "cloudforge.name"
	labelPrefix = "cloudforge."
)

// dockerAPIVersion pins the Engine API version used on the socket.
const dockerAPIVersion = "v1.41"

// DockerHostAgent manages containers through the Docker daemon.
type DockerHostAgent struct {
	socketPath    string
	http          *http.Client
	LabVolumePath string
}

// NewDockerHostAgent builds the agent. Construction never dials the
// daemon: availability is checked per operation so a host-agent process
// can start (and report honestly) on a host without Docker.
func NewDockerHostAgent(socketPath string) *DockerHostAgent {
	if socketPath == "" {
		socketPath = DefaultDockerSocket
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &DockerHostAgent{
		socketPath:    socketPath,
		http:          &http.Client{Transport: transport, Timeout: 30 * time.Second},
		LabVolumePath: LabRoots[0],
	}
}

func (a *DockerHostAgent) Mode() string { return "docker" }

// --- typed Docker API structs --------------------------------------------

// dockerError is the Engine API error body ({"message": "..."}).
type dockerError struct {
	Message string `json:"message"`
}

type dockerContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

type dockerHostConfig struct {
	NanoCPUs int64 `json:"NanoCpus,omitempty"`
	Memory   int64 `json:"Memory,omitempty"`
}

type dockerCreateRequest struct {
	Image      string            `json:"Image"`
	Hostname   string            `json:"Hostname,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	HostConfig dockerHostConfig  `json:"HostConfig,omitempty"`
}

type dockerCreateResponse struct {
	ID string `json:"Id"`
}

type dockerInspectResponse struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		NanoCPUs int64 `json:"NanoCpus"`
		Memory   int64 `json:"Memory"`
	} `json:"HostConfig"`
}

type dockerExecCreateRequest struct {
	Cmd          []string `json:"Cmd"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
}

type dockerExecCreateResponse struct {
	ID string `json:"Id"`
}

type dockerExecStartRequest struct {
	Detach bool `json:"Detach"`
	Tty    bool `json:"Tty"`
}

type dockerExecInspectResponse struct {
	ExitCode int  `json:"ExitCode"`
	Running  bool `json:"Running"`
}

// dockerStats is the minimal shape of the Docker stats JSON we need.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
}

// --- socket HTTP plumbing -------------------------------------------------

// call performs one Engine API request. The host in the URL is a dummy:
// the transport dials the Unix socket regardless of it.
func (a *DockerHostAgent) call(ctx context.Context, method, path string, body any, out any) (status int, err error) {
	var rdr io.Reader
	if body != nil {
		data, merr := json.Marshal(body)
		if merr != nil {
			return 0, fmt.Errorf("marshal docker request: %w", merr)
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost/"+dockerAPIVersion+path, rdr)
	if err != nil {
		return 0, fmt.Errorf("build docker request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, &UnavailableError{Backend: "docker", Reason: fmt.Sprintf("Docker daemon unreachable on %s: %v", a.socketPath, err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read docker response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var de dockerError
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &de) == nil && de.Message != "" {
			msg = de.Message
		}
		return resp.StatusCode, fmt.Errorf("docker API %s %s returned %d: %s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode docker response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// available checks the socket exists and the daemon answers /_ping.
func (a *DockerHostAgent) available(ctx context.Context) error {
	if _, err := os.Stat(a.socketPath); err != nil {
		return &UnavailableError{Backend: "docker", Reason: fmt.Sprintf("Docker socket %s not present (%v); install docker.io / docker-ce and start the daemon", a.socketPath, err)}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := a.call(pingCtx, http.MethodGet, "/_ping", nil, nil); err != nil {
		if IsUnavailable(err) {
			return err
		}
		return &UnavailableError{Backend: "docker", Reason: fmt.Sprintf("Docker daemon did not answer /_ping on %s: %v", a.socketPath, err)}
	}
	return nil
}

func (a *DockerHostAgent) Inventory(ctx context.Context) (Inventory, error) {
	return CollectInventory(a.LabVolumePath)
}

func (a *DockerHostAgent) RunOp(ctx context.Context, req OpRequest) (OpResult, error) {
	if err := Validate(req.Op, req); err != nil {
		return OpResult{}, err
	}
	switch req.Op {
	case OpHostInventory:
		inv, err := a.Inventory(ctx)
		if err != nil {
			return OpResult{}, err
		}
		return OpResult{Op: req.Op, Inventory: &inv}, nil
	case OpListContainers, OpCreateContainer, OpStartContainer, OpStopContainer, OpRemoveContainer, OpExecInLab:
		if err := a.available(ctx); err != nil {
			return OpResult{}, err
		}
	case OpListVMs, OpCreateVM, OpStartVM, OpStopVM, OpRebootVM, OpDeleteVM:
		return OpResult{}, &UnavailableError{Backend: "docker", Reason: "the Docker agent manages containers only; VMs need HOST_AGENT_MODE=libvirt"}
	default:
		return OpResult{}, &ValidationError{Op: req.Op, Reason: "unknown operation"}
	}
	switch req.Op {
	case OpListContainers:
		return a.listContainers(ctx, req)
	case OpCreateContainer:
		return a.createContainer(ctx, req)
	case OpStartContainer:
		return a.startContainer(ctx, req)
	case OpStopContainer:
		return a.stopContainer(ctx, req)
	case OpRemoveContainer:
		return a.removeContainer(ctx, req)
	case OpExecInLab:
		return a.execInLab(ctx, req)
	}
	return OpResult{}, &ValidationError{Op: req.Op, Reason: "unknown operation"}
}

func labFilterJSON(labID string) string {
	f, _ := json.Marshal(map[string]map[string]bool{
		"label": {labelLabID + "=" + labID: true},
	})
	return string(f)
}

func (a *DockerHostAgent) listContainers(ctx context.Context, req OpRequest) (OpResult, error) {
	path := "/containers/json?all=1&filters=" + url.QueryEscape(labFilterJSON(req.LabID))
	var summaries []dockerContainerSummary
	if _, err := a.call(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return OpResult{}, fmt.Errorf("docker list containers: %w", err)
	}
	items := make([]map[string]any, 0, len(summaries))
	for _, s := range summaries {
		name := strings.TrimPrefix(firstOr(s.Names, ""), "/")
		items = append(items, map[string]any{
			"id": s.ID, "name": name, "image": s.Image, "lab_id": req.LabID,
			"state": s.State,
		})
	}
	return OpResult{Op: req.Op, State: "ok",
		Message: fmt.Sprintf("%d container(s) in lab %s", len(items), req.LabID), Items: items}, nil
}

func firstOr(v []string, def string) string {
	if len(v) > 0 {
		return v[0]
	}
	return def
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (a *DockerHostAgent) createContainer(ctx context.Context, req OpRequest) (OpResult, error) {
	cpu := req.CPU
	if cpu == 0 {
		cpu = 1
	}
	mem := req.MemoryMB
	if mem == 0 {
		mem = 512
	}
	var created dockerCreateResponse
	path := "/containers/create?name=" + url.QueryEscape(req.Name)
	_, err := a.call(ctx, http.MethodPost, path, dockerCreateRequest{
		Image:    req.Image,
		Hostname: req.Name,
		Labels: map[string]string{
			labelLabID: req.LabID,
			labelOwner: req.OwnerID,
			labelName:  req.Name,
		},
		HostConfig: dockerHostConfig{
			NanoCPUs: int64(cpu) * 1_000_000_000,
			Memory:   int64(mem) * 1024 * 1024,
		},
	}, &created)
	if err != nil {
		return OpResult{}, fmt.Errorf("docker create container %q: %w", req.Name, err)
	}
	return OpResult{Op: req.Op, ID: created.ID, Name: req.Name, State: "created",
		Message: fmt.Sprintf("container %s created (id %s)", req.Name, shortID(created.ID))}, nil
}

// resolve finds a container id by id or by lab-scoped name label.
func (a *DockerHostAgent) resolve(ctx context.Context, req OpRequest) (string, error) {
	if req.ID != "" {
		var insp dockerInspectResponse
		if _, err := a.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(req.ID)+"/json", nil, &insp); err != nil {
			return "", fmt.Errorf("docker inspect container %q: %w", req.ID, err)
		}
		return insp.ID, nil
	}
	path := "/containers/json?all=1&filters=" + url.QueryEscape(labFilterJSON(req.LabID))
	var summaries []dockerContainerSummary
	if _, err := a.call(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return "", fmt.Errorf("docker list containers: %w", err)
	}
	for _, s := range summaries {
		if s.Labels[labelName] == req.Name {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("container %q not found in lab %q", req.Name, req.LabID)
}

func (a *DockerHostAgent) startContainer(ctx context.Context, req OpRequest) (OpResult, error) {
	id, err := a.resolve(ctx, req)
	if err != nil {
		return OpResult{}, err
	}
	if _, err := a.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil, nil); err != nil {
		return OpResult{}, fmt.Errorf("docker start container %q: %w", shortID(id), err)
	}
	return OpResult{Op: req.Op, ID: id, Name: req.Name, State: "running", Message: "container started"}, nil
}

func (a *DockerHostAgent) stopContainer(ctx context.Context, req OpRequest) (OpResult, error) {
	id, err := a.resolve(ctx, req)
	if err != nil {
		return OpResult{}, err
	}
	if _, err := a.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop", nil, nil); err != nil {
		return OpResult{}, fmt.Errorf("docker stop container %q: %w", shortID(id), err)
	}
	return OpResult{Op: req.Op, ID: id, Name: req.Name, State: "stopped", Message: "container stopped"}, nil
}

func (a *DockerHostAgent) removeContainer(ctx context.Context, req OpRequest) (OpResult, error) {
	// Double-remove is idempotent: resolving a missing container is a
	// success, not an error.
	id, err := a.resolve(ctx, req)
	if err != nil {
		return OpResult{Op: req.Op, State: "removed", Message: "container already removed"}, nil
	}
	if _, err := a.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id)+"?force=1", nil, nil); err != nil {
		return OpResult{}, fmt.Errorf("docker remove container %q: %w", shortID(id), err)
	}
	return OpResult{Op: req.Op, ID: id, Name: req.Name, State: "removed", Message: "container removed"}, nil
}

// execInLab runs an allowlisted command inside the named container via
// the Docker exec API. Output is discarded; the exit code is reported.
// The command was already validated by Validate (allowlist, no shell
// metacharacters).
func (a *DockerHostAgent) execInLab(ctx context.Context, req OpRequest) (OpResult, error) {
	id, err := a.resolve(ctx, req)
	if err != nil {
		return OpResult{}, err
	}
	var created dockerExecCreateResponse
	if _, err := a.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/exec",
		dockerExecCreateRequest{Cmd: req.Command, AttachStdout: true, AttachStderr: true}, &created); err != nil {
		return OpResult{}, fmt.Errorf("docker exec create: %w", err)
	}
	if _, err := a.call(ctx, http.MethodPost, "/exec/"+url.PathEscape(created.ID)+"/start",
		dockerExecStartRequest{Detach: false, Tty: false}, nil); err != nil {
		return OpResult{}, fmt.Errorf("docker exec start: %w", err)
	}
	var insp dockerExecInspectResponse
	if _, err := a.call(ctx, http.MethodGet, "/exec/"+url.PathEscape(created.ID)+"/json", nil, &insp); err != nil {
		return OpResult{}, fmt.Errorf("docker exec inspect: %w", err)
	}
	return OpResult{Op: req.Op, ID: id, State: "ok",
		Message: fmt.Sprintf("exec %q in container %s exited %d", strings.Join(req.Command, " "), shortID(id), insp.ExitCode)}, nil
}

func (a *DockerHostAgent) containerUsage(ctx context.Context, id string) (cpuPercent float64, memUsedMB int) {
	var st dockerStats
	if _, err := a.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/stats?stream=0", nil, &st); err != nil {
		return 0, 0
	}
	cpuDelta := float64(st.CPUStats.CPUUsage.TotalUsage - st.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(st.CPUStats.SystemCPUUsage - st.PreCPUStats.SystemCPUUsage)
	if sysDelta > 0 && cpuDelta >= 0 {
		cpus := float64(st.CPUStats.OnlineCPUs)
		if cpus <= 0 {
			cpus = 1
		}
		cpuPercent = cpuDelta / sysDelta * cpus * 100
	}
	return cpuPercent, int(st.MemoryStats.Usage / 1024 / 1024)
}

// CleanupLab removes every container labelled with the lab id.
func (a *DockerHostAgent) CleanupLab(ctx context.Context, labID string) error {
	if err := a.available(ctx); err != nil {
		return err
	}
	path := "/containers/json?all=1&filters=" + url.QueryEscape(labFilterJSON(labID))
	var summaries []dockerContainerSummary
	if _, err := a.call(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return fmt.Errorf("docker list containers for cleanup: %w", err)
	}
	for _, s := range summaries {
		if _, err := a.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(s.ID)+"?force=1", nil, nil); err != nil {
			return fmt.Errorf("docker remove container %q during cleanup: %w", shortID(s.ID), err)
		}
	}
	return nil
}

// Usage returns per-container cpu/mem usage from Docker stats.
func (a *DockerHostAgent) Usage(ctx context.Context, labID string) ([]ResourceUsage, error) {
	if err := a.available(ctx); err != nil {
		return nil, err
	}
	path := "/containers/json?all=1&filters=" + url.QueryEscape(labFilterJSON(labID))
	var summaries []dockerContainerSummary
	if _, err := a.call(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return nil, fmt.Errorf("docker list containers: %w", err)
	}
	out := make([]ResourceUsage, 0, len(summaries))
	for _, s := range summaries {
		cpu, mem := a.containerUsage(ctx, s.ID)
		out = append(out, ResourceUsage{
			Kind: "container", ID: s.ID, Name: strings.TrimPrefix(firstOr(s.Names, ""), "/"),
			LabID: labID, State: s.State, CPUPercent: cpu, MemoryUsed: mem,
		})
	}
	return out, nil
}
