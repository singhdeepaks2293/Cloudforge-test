// Package hostagent is the CloudForge host agent (Milestone 3).
//
// It is the only privileged boundary on a host: the only way the API or
// worker can create containers/VMs or run commands inside a lab is
// through the typed, allowlisted operations in this package. There is no
// arbitrary shell execution anywhere — no os/exec with user-supplied
// strings and no `sh -c`. The one exception is the libvirt agent's
// clearly-marked hypervisor method, which invokes the `virsh` binary
// with fixed argv and a validated domain name only (see libvirt.go).
//
// Modes (HOST_AGENT_MODE, default "simulated"):
//   - simulated: in-memory containers/VMs with real lifecycle state
//     transitions and resource accounting. Used for tests and in this
//     sandbox, where no Docker or libvirt exists.
//   - docker: real containers via a minimal typed HTTP client over the
//     Docker socket (docker/docker was tried and does not build with
//     Go 1.25 — see docker.go). Returns a structured Unavailable error
//     when Docker is absent — never fake success.
//   - libvirt: real VMs via the libvirt socket/URI (virsh). Returns a
//     structured Unavailable error when libvirt is absent.
//   - auto: docker if its socket exists, else libvirt if its socket
//     exists, else simulated (logged).
package hostagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// Version is the host-agent API version.
const Version = "0.3.0-m3"

// DefaultSocketPath is the Docker daemon socket.
const DefaultDockerSocket = "/var/run/docker.sock"

// DefaultLibvirtSocket is the libvirt daemon socket.
const DefaultLibvirtSocket = "/var/run/libvirt/libvirt-sock"

// DefaultLibvirtURI is the libvirt connection URI for a local host.
const DefaultLibvirtURI = "qemu:///system"

// LabRoots are the only absolute host paths a lab operation may touch.
// Anything absolute outside these roots is rejected by Validate.
var LabRoots = []string{"/var/lib/cloudforge/labs", "/srv/cloudforge/labs"}

// --- Errors ---------------------------------------------------------------

// ValidationError is a rejected operation (unknown op, shell
// metacharacters, path escape, non-allowlisted command, bad profile…).
type ValidationError struct {
	Op     Op
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("host agent validation failed for op %q: %s", e.Op, e.Reason)
	}
	return "host agent validation failed: " + e.Reason
}

// UnavailableError means the requested backend (Docker / libvirt) is not
// present on this host. It is never a faked success: callers must fall
// back or fail honestly when they see it.
type UnavailableError struct {
	Backend string // "docker" or "libvirt"
	Reason  string
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("host agent backend %q unavailable: %s", e.Backend, e.Reason)
}

// IsUnavailable reports whether err is (or wraps) an UnavailableError.
func IsUnavailable(err error) bool {
	var ue *UnavailableError
	return errors.As(err, &ue)
}

// ErrUnauthorized is returned by the client when the host agent rejects
// the bearer token (HTTP 401/403).
var ErrUnauthorized = errors.New("host agent: unauthorized (bad or missing token)")

// --- Operations -----------------------------------------------------------

// Op is one allowlisted host operation. Anything not in this enum is
// rejected by Validate — there is no generic "run this" operation.
type Op string

const (
	OpListContainers  Op = "ListContainers"
	OpStartContainer  Op = "StartContainer"
	OpStopContainer   Op = "StopContainer"
	OpRemoveContainer Op = "RemoveContainer"
	OpCreateContainer Op = "CreateContainer"
	OpListVMs         Op = "ListVMs"
	OpStartVM         Op = "StartVM"
	OpStopVM          Op = "StopVM"
	OpRebootVM        Op = "RebootVM"
	OpDeleteVM        Op = "DeleteVM"
	OpCreateVM        Op = "CreateVM"
	OpHostInventory   Op = "HostInventory"
	OpExecInLab       Op = "ExecInLab"
)

// AllOps lists every allowlisted operation.
var AllOps = []Op{
	OpListContainers, OpStartContainer, OpStopContainer, OpRemoveContainer,
	OpCreateContainer, OpListVMs, OpStartVM, OpStopVM, OpRebootVM,
	OpDeleteVM, OpCreateVM, OpHostInventory, OpExecInLab,
}

func (o Op) valid() bool {
	for _, allowed := range AllOps {
		if o == allowed {
			return true
		}
	}
	return false
}

// labScoped reports whether the op acts on one lab's resources.
func (o Op) labScoped() bool { return o != OpHostInventory }

// OpRequest is one typed host operation. Params are fields, not a free
// map, so nothing unvalidated can smuggle a value through.
type OpRequest struct {
	Op             Op       `json:"op"`
	LabID          string   `json:"lab_id,omitempty"`
	OwnerID        string   `json:"owner_id,omitempty"`
	Name           string   `json:"name,omitempty"` // container/VM name
	ID             string   `json:"id,omitempty"`   // container/VM id
	Image          string   `json:"image,omitempty"`
	Profile        string   `json:"profile,omitempty"` // VM profile name
	HostPath       string   `json:"host_path,omitempty"`
	Workdir        string   `json:"workdir,omitempty"`
	Command        []string `json:"command,omitempty"` // ExecInLab argv, allowlisted
	Target         string   `json:"target,omitempty"`  // ping/curl target host (ExecInLab)
	CPU            int      `json:"cpu,omitempty"`
	MemoryMB       int      `json:"memory_mb,omitempty"`
	DiskGB         int      `json:"disk_gb,omitempty"`
	Hostname       string   `json:"hostname,omitempty"`
	Username       string   `json:"username,omitempty"`
	SSHKey         string   `json:"ssh_key,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// OpResult is the outcome of one operation.
type OpResult struct {
	Op        Op               `json:"op"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	State     string           `json:"state,omitempty"`
	Message   string           `json:"message,omitempty"`
	Replayed  bool             `json:"replayed,omitempty"`
	Items     []map[string]any `json:"items,omitempty"`
	Inventory *Inventory       `json:"inventory,omitempty"`
	Usage     []ResourceUsage  `json:"usage,omitempty"`
}

// --- Validation -----------------------------------------------------------

var (
	safeToken          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	safeIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	safeUser           = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	safeImage          = regexp.MustCompile(`^[a-z0-9]+([._/-][a-z0-9]+)*(:[A-Za-z0-9._-]+)?$`)
	safeSSHKey         = regexp.MustCompile(`^(ssh-rsa|ssh-ed25519|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ssh-dss) [A-Za-z0-9+/=]+( [A-Za-z0-9._@-]+)?$`)
)

// shellMeta are characters that must never appear in a validated string
// param: shell metacharacters, glob characters, quotes, newlines and
// control characters. SSH keys are validated by format instead (they
// legitimately contain spaces, +, / and =).
const shellMeta = ";|&`$()<>\\\n\r\t'\"*?~#!{}[]"

func containsShellMeta(s string) bool { return strings.ContainsAny(s, shellMeta) }

func checkToken(field, v string) error {
	if v == "" {
		return nil
	}
	if containsShellMeta(v) || !safeToken.MatchString(v) {
		return fmt.Errorf("field %s %q is not a safe token (letters, digits, . _ - only)", field, v)
	}
	return nil
}

func checkNoMeta(field, v string) error {
	if containsShellMeta(v) {
		return fmt.Errorf("field %s contains shell metacharacters", field)
	}
	return nil
}

// validateLabPath rejects absolute host paths outside the lab roots and
// any path that escapes its lab directory via "..". Relative paths are
// confined to the lab workdir by the same ".." rule.
func validateLabPath(field, p, labID string) error {
	if p == "" {
		return nil
	}
	if strings.ContainsRune(p, 0) || containsShellMeta(p) {
		return fmt.Errorf("field %s %q contains forbidden characters", field, p)
	}
	clean := path.Clean(p)
	if !path.IsAbs(clean) {
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("field %s %q escapes the lab workdir", field, p)
		}
		return nil
	}
	for _, root := range LabRoots {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			if labID == "" {
				return nil
			}
			rest := strings.TrimPrefix(clean, root+"/")
			if rest == labID || strings.HasPrefix(rest, labID+"/") {
				return nil
			}
			return fmt.Errorf("field %s %q is outside lab %q directory", field, p, labID)
		}
	}
	return fmt.Errorf("field %s %q is outside the lab directories (%s)", field, p, strings.Join(LabRoots, ", "))
}

// execAllowlist is the complete set of commands ExecInLab may run.
// The first argv element must be a bare name from this list.
var execAllowlist = map[string]bool{
	"aws": true, "terraform": true, "tofu": true, "docker": true,
	"kubectl": true, "ping": true, "curl": true, "ls": true, "cat": true,
}

var dockerSubcommands = map[string]bool{
	"ps": true, "images": true, "logs": true, "inspect": true,
	"version": true, "info": true, "stats": true,
}

var kubectlVerbs = map[string]bool{
	"get": true, "describe": true, "logs": true, "version": true, "config": true,
}

// isLabInternalHost reports whether host is a lab-internal destination:
// localhost, *.lab.internal / *.internal, or a private/loopback IP.
// Anything else (public DNS names, public IPs) is rejected for ping/curl.
func isLabInternalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	if strings.HasSuffix(host, ".lab.internal") || strings.HasSuffix(host, ".internal") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback()
	}
	return false
}

// validateExecTarget checks ping/curl targets and path args of ls/cat.
func validateExec(req OpRequest) error {
	if len(req.Command) == 0 {
		return errors.New("ExecInLab requires a non-empty command")
	}
	cmd := req.Command[0]
	if strings.Contains(cmd, "/") || strings.Contains(cmd, "\\") {
		return fmt.Errorf("ExecInLab command %q must be a bare name from the allowlist, not a path", cmd)
	}
	if !execAllowlist[cmd] {
		return fmt.Errorf("ExecInLab command %q is not on the allowlist", cmd)
	}
	for i, arg := range req.Command[1:] {
		if err := checkNoMeta(fmt.Sprintf("command arg %d", i+1), arg); err != nil {
			return err
		}
		// Any absolute path argument must stay inside the lab dirs.
		if strings.HasPrefix(arg, "/") {
			if err := validateLabPath("command arg", arg, req.LabID); err != nil {
				return err
			}
		}
	}
	args := req.Command[1:]
	switch cmd {
	case "ping", "curl":
		target := req.Target
		if target == "" {
			// Find the first non-flag argument (the host/URL).
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					continue
				}
				target = a
				break
			}
		}
		if target == "" {
			return fmt.Errorf("ExecInLab %s requires a target host", cmd)
		}
		host := target
		if strings.Contains(target, "://") {
			u, err := url.Parse(target)
			if err != nil || u.Hostname() == "" {
				return fmt.Errorf("ExecInLab %s target %q is not a valid URL", cmd, target)
			}
			host = u.Hostname()
		}
		if !isLabInternalHost(host) {
			return fmt.Errorf("ExecInLab %s target %q is not a lab-internal host", cmd, target)
		}
	case "ls", "cat":
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				continue
			}
			if err := validateLabPath("command arg", a, req.LabID); err != nil {
				return err
			}
		}
	case "docker":
		if len(args) == 0 || !dockerSubcommands[args[0]] {
			return fmt.Errorf("ExecInLab docker subcommand is not on the allowlist (read-only: ps, images, logs, inspect, version, info, stats)")
		}
	case "kubectl":
		if len(args) == 0 || !kubectlVerbs[args[0]] {
			return fmt.Errorf("ExecInLab kubectl verb is not on the allowlist (get, describe, logs, version, config)")
		}
	}
	return nil
}

// Validate rejects anything not on the allowlist: unknown ops, shell
// metacharacters in any string param, absolute host paths outside the
// lab directories, ExecInLab commands outside the command allowlist, and
// out-of-range resource requests. op must match req.Op when req.Op is set.
func Validate(op Op, req OpRequest) error {
	if !op.valid() {
		return &ValidationError{Op: op, Reason: "unknown operation (not on the allowlist)"}
	}
	if req.Op != "" && req.Op != op {
		return &ValidationError{Op: op, Reason: fmt.Sprintf("op mismatch: request carries %q", req.Op)}
	}
	if op.labScoped() && req.LabID == "" {
		return &ValidationError{Op: op, Reason: "lab_id is required"}
	}
	for field, v := range map[string]string{
		"lab_id": req.LabID, "owner_id": req.OwnerID, "name": req.Name,
		"id": req.ID, "profile": req.Profile, "hostname": req.Hostname,
	} {
		if err := checkToken(field, v); err != nil {
			return &ValidationError{Op: op, Reason: err.Error()}
		}
	}
	// Idempotency keys may contain ":" (e.g. "provision:<lab-id>") but
	// no shell metacharacters.
	if req.IdempotencyKey != "" {
		if containsShellMeta(req.IdempotencyKey) || !safeIdempotencyKey.MatchString(req.IdempotencyKey) {
			return &ValidationError{Op: op, Reason: fmt.Sprintf("field idempotency_key %q is not a safe key (letters, digits, . _ - : only)", req.IdempotencyKey)}
		}
	}
	if req.Image != "" {
		if containsShellMeta(req.Image) || !safeImage.MatchString(req.Image) {
			return &ValidationError{Op: op, Reason: fmt.Sprintf("image %q is not a safe image reference", req.Image)}
		}
	}
	if req.Username != "" {
		if containsShellMeta(req.Username) || !safeUser.MatchString(req.Username) {
			return &ValidationError{Op: op, Reason: fmt.Sprintf("username %q is invalid (lowercase, digits, _ - only)", req.Username)}
		}
	}
	if req.SSHKey != "" && !safeSSHKey.MatchString(req.SSHKey) {
		return &ValidationError{Op: op, Reason: "ssh_key is not a well-formed public key (ssh-rsa / ssh-ed25519 / ecdsa-sha2-*)"}
	}
	if req.Target != "" {
		if err := checkNoMeta("target", req.Target); err != nil {
			return &ValidationError{Op: op, Reason: err.Error()}
		}
	}
	if err := validateLabPath("host_path", req.HostPath, req.LabID); err != nil {
		return &ValidationError{Op: op, Reason: err.Error()}
	}
	if err := validateLabPath("workdir", req.Workdir, req.LabID); err != nil {
		return &ValidationError{Op: op, Reason: err.Error()}
	}
	// Resource bounds (0 = use the agent default).
	if req.CPU < 0 || req.CPU > 32 {
		return &ValidationError{Op: op, Reason: "cpu must be between 0 and 32 vCPUs"}
	}
	if req.MemoryMB < 0 || req.MemoryMB > 65536 {
		return &ValidationError{Op: op, Reason: "memory_mb must be between 0 and 65536"}
	}
	if req.DiskGB < 0 || req.DiskGB > 500 {
		return &ValidationError{Op: op, Reason: "disk_gb must be between 0 and 500"}
	}

	switch op {
	case OpCreateContainer:
		if req.Name == "" {
			return &ValidationError{Op: op, Reason: "name is required"}
		}
		if req.Image == "" {
			return &ValidationError{Op: op, Reason: "image is required"}
		}
	case OpStartContainer, OpStopContainer, OpRemoveContainer:
		if req.ID == "" && req.Name == "" {
			return &ValidationError{Op: op, Reason: "id or name is required"}
		}
	case OpCreateVM:
		if req.Name == "" {
			return &ValidationError{Op: op, Reason: "name is required"}
		}
		if req.Profile == "" {
			return &ValidationError{Op: op, Reason: "profile is required"}
		}
		if _, err := ValidateProfile(req.Profile); err != nil {
			return &ValidationError{Op: op, Reason: err.Error()}
		}
	case OpStartVM, OpStopVM, OpRebootVM, OpDeleteVM:
		if req.ID == "" && req.Name == "" {
			return &ValidationError{Op: op, Reason: "id or name is required"}
		}
	case OpExecInLab:
		if err := validateExec(req); err != nil {
			return &ValidationError{Op: op, Reason: err.Error()}
		}
	}
	return nil
}

// --- Agent interface ------------------------------------------------------

// Container is one lab container.
type Container struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Image      string    `json:"image"`
	LabID      string    `json:"lab_id"`
	OwnerID    string    `json:"owner_id,omitempty"`
	State      string    `json:"state"` // created | running | stopped
	CPU        int       `json:"cpu"`
	MemoryMB   int       `json:"memory_mb"`
	IP         string    `json:"ip,omitempty"`
	CPUPercent float64   `json:"cpu_percent,omitempty"`
	MemoryUsed int       `json:"memory_used_mb,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// VM is one lab virtual machine.
type VM struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	LabID         string    `json:"lab_id"`
	OwnerID       string    `json:"owner_id,omitempty"`
	Profile       string    `json:"profile"`
	State         string    `json:"state"` // defined | running | stopped
	CPU           int       `json:"cpu"`
	MemoryMB      int       `json:"memory_mb"`
	DiskGB        int       `json:"disk_gb"`
	Hostname      string    `json:"hostname"`
	Username      string    `json:"username"`
	IP            string    `json:"ip,omitempty"`
	DiskPlan      string    `json:"disk_plan,omitempty"`    // qcow2 backing-file plan
	NetworkPlan   string    `json:"network_plan,omitempty"` // network attachment plan
	CloudInitYAML string    `json:"-"`                      // generated user-data (not serialized)
	CPUPercent    float64   `json:"cpu_percent,omitempty"`
	MemoryUsed    int       `json:"memory_used_mb,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ResourceUsage is per-container/VM cpu/mem usage.
type ResourceUsage struct {
	Kind       string  `json:"kind"` // container | vm
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	LabID      string  `json:"lab_id"`
	State      string  `json:"state"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   int     `json:"memory_mb"`
	MemoryUsed int     `json:"memory_used_mb"`
}

// HostAgent is the typed host boundary. Implementations: simulated,
// Docker, libvirt (see NewAgent).
type HostAgent interface {
	// Mode reports "simulated", "docker" or "libvirt".
	Mode() string
	// RunOp validates (again, defence in depth) and executes one
	// allowlisted operation.
	RunOp(ctx context.Context, req OpRequest) (OpResult, error)
	// Inventory returns real host inventory (see inventory.go).
	Inventory(ctx context.Context) (Inventory, error)
	// CleanupLab removes every container and VM belonging to a lab.
	// It is idempotent: a lab with no resources succeeds.
	CleanupLab(ctx context.Context, labID string) error
	// Usage returns per-resource cpu/mem usage for a lab.
	Usage(ctx context.Context, labID string) ([]ResourceUsage, error)
}

// Provisioner is the subset of the host-agent client the worker needs.
// *Client implements it; tests may substitute a fake.
type Provisioner interface {
	Do(ctx context.Context, req OpRequest) (OpResult, error)
	CleanupLab(ctx context.Context, labID, ownerID string) error
}

// NewAgent builds the agent for mode ("simulated", "docker", "libvirt",
// "auto"). An unknown mode is an error; a docker/libvirt agent whose
// backend is absent still constructs successfully and reports
// Unavailable from RunOp — construction must never fake availability.
func NewAgent(mode string) (HostAgent, error) {
	switch strings.ToLower(mode) {
	case "", "simulated":
		return NewSimulatedHostAgent(), nil
	case "docker":
		return NewDockerHostAgent(DefaultDockerSocket), nil
	case "libvirt":
		return NewLibvirtHostAgent(DefaultLibvirtSocket, DefaultLibvirtURI), nil
	case "auto":
		if _, err := socketExists(DefaultDockerSocket); err == nil {
			return NewDockerHostAgent(DefaultDockerSocket), nil
		}
		if _, err := socketExists(DefaultLibvirtSocket); err == nil {
			return NewLibvirtHostAgent(DefaultLibvirtSocket, DefaultLibvirtURI), nil
		}
		return NewSimulatedHostAgent(), nil
	default:
		return nil, fmt.Errorf("unknown HOST_AGENT_MODE %q (want simulated, docker, libvirt or auto)", mode)
	}
}

func socketExists(p string) (bool, error) {
	// Small helper so NewAgent("auto") can probe without importing os
	// everywhere; implemented in inventory.go's file scope via os.Stat.
	return socketExistsOS(p)
}
