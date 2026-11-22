// Package phase4 holds the service semantics for CloudForge v2
// Phase 4 (containers and developer tools). Handlers in httpapi own
// the HTTP surface and persistence (the generic store.Phase3Store,
// extended with the p4_* collections in migration 010); this package
// owns the rules that must be the same wherever they are used.
//
// Fidelity, stated once here so handlers, consoles and docs agree:
//   - ECR: a real local registry — durable Postgres catalogue +
//     content-addressed blobs on local disk, speaking the Docker
//     Registry HTTP API v2 subset (catalog, tags, manifest and blob
//     upload/download). In full mode the same API can be backed by
//     a real registry:2 container via ECR_REGISTRY_URL. No Docker
//     Hub compatibility is claimed: only the subset in ecr.go.
//   - ECS / Fargate: task definitions, clusters, services and tasks
//     are real records; the runtime is SIMULATED — a task's command
//     runs as a sandboxed subprocess on the API host (timeout,
//     output cap, prlimit memory cap when available, uid 65534 when
//     the API runs as root), like Phase 3 Lambda. No Docker
//     container is started and no image is pulled: the image field
//     is recorded and matched against the ECR catalogue, but the
//     command runs locally. Task status comes from the real process
//     (PENDING -> RUNNING -> STOPPED with a real exit code), never
//     invented. Fargate is the same runtime with no cluster to
//     manage, labelled serverless.
//   - EKS: UNSUPPORTED by explicit Phase 4 decision — no local
//     k3s/kind cluster is built (see EKSDecision). The EKS console
//     is a Kubernetes-concepts learning path that runs its exercises
//     on the ECS runtime above. No static diagram is presented as
//     a cluster.
//   - CodeBuild: real local builds — checkout from a local git repo
//     path (local paths only, under CLOUDFORGE_BUILD_SOURCE_ROOT),
//     buildspec phases run as real shell commands in a subprocess
//     with timeouts, real logs, artifacts copied to disk.
//   - CodePipeline: real stage execution (source -> build ->
//     deploy); deploy targets are the ECS service update and the
//     Lambda code update that actually exist; Terraform deploy
//     honestly fails without Docker.
//   - CodeDeploy: real deployments with real status and rollback
//     to the previous revision (previous task definition / previous
//     Lambda code, captured before the deploy).
//   - CloudFormation: a separate template engine (JSON/YAML, own
//     stack state, change-set diff, rollback on failure) that
//     provisions through the existing Phase 3 resources. PARTIAL:
//     only the resource types in SupportedCFNTypes are provisioned;
//     the rest are reported UNSUPPORTED in the change set, never
//     silently skipped. It is NOT Terraform.
package phase4

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// --- ECR naming ------------------------------------------------------------

var ecrRepoRE = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
var ecrTagRE = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)

// ValidateECRRepoName rejects names the registry cannot store.
// Repository names are lowercase, slash-separated components.
func ValidateECRRepoName(name string) error {
	if name == "" || len(name) > 255 {
		return errors.New("repository name is required (max 255 characters)")
	}
	for _, part := range strings.Split(name, "/") {
		if !ecrRepoRE.MatchString(part) {
			return fmt.Errorf("repository name component %q must be lowercase letters, digits, ., _ or -", part)
		}
	}
	return nil
}

// ValidateImageTag rejects tags the registry cannot store.
func ValidateImageTag(tag string) error {
	if tag == "" || !ecrTagRE.MatchString(tag) {
		return errors.New("tag must be 1-128 characters of letters, digits, _, . or -")
	}
	return nil
}

// ParseImageRef splits "repo:tag" (default tag "latest") or
// "repo@sha256:..." into repo, tag, digest.
func ParseImageRef(ref string) (repo, tag, digest string) {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[:i], "", ref[i+1:]
	}
	if i := strings.LastIndex(ref, ":"); i >= 0 && !strings.Contains(ref[i:], "/") {
		return ref[:i], ref[i+1:], ""
	}
	return ref, "latest", ""
}

// Digest returns the sha256 digest string for blob bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DigestHex is the bare hex part of a sha256: digest.
func DigestHex(digest string) (string, error) {
	digest = strings.TrimSpace(digest)
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("digest %q must start with sha256:", digest)
	}
	hexPart := strings.TrimPrefix(digest, "sha256:")
	if len(hexPart) != 64 {
		return "", fmt.Errorf("digest %q is not a 64-hex sha256", digest)
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return "", fmt.Errorf("digest %q is not valid hex: %w", digest, err)
	}
	return hexPart, nil
}

// --- sandboxed command execution -------------------------------------------

// CommandResult is one sandboxed command run.
type CommandResult struct {
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	TimedOut   bool   `json:"timed_out"`
	DurationMs int64  `json:"duration_ms"`
	// Sandbox names the isolation that actually applied.
	Sandbox string `json:"sandbox"`
}

const maxCommandOutput = 256 * 1024 // per-command stdout+stderr cap

// RunShell runs shell commands (sh -c, one per entry) in dir with a
// timeout, writing combined output to logW. Buildspec commands are
// shell by nature, so sh -c is the honest runner here (unlike
// Lambda/ECS argv execution). The child runs as uid 65534 when the
// API runs as root, with a prlimit memory cap when available, and
// only the supplied env plus PATH/HOME.
func RunShell(ctx context.Context, dir string, env map[string]string, timeoutSecs, memoryMB int, logW io.Writer, commands []string) CommandResult {
	start := time.Now()
	sandbox := "subprocess: timeout + output cap + temp workdir"
	if timeoutSecs <= 0 || timeoutSecs > 3600 {
		timeoutSecs = 300
	}
	if memoryMB <= 0 || memoryMB > 4096 {
		memoryMB = 512
	}
	if logW == nil {
		logW = io.Discard
	}
	for _, cmdStr := range commands {
		fmt.Fprintf(logW, "$ %s\n", cmdStr)
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
		var cmd *exec.Cmd
		if _, err := exec.LookPath("prlimit"); err == nil {
			cmd = exec.CommandContext(runCtx, "prlimit",
				"--as="+strconv.Itoa(memoryMB*1024*1024), "sh", "-c", cmdStr)
			if !strings.Contains(sandbox, "prlimit memory cap") {
				sandbox += fmt.Sprintf(" + prlimit memory cap %d MiB", memoryMB)
			}
		} else {
			cmd = exec.CommandContext(runCtx, "sh", "-c", cmdStr)
			sandbox += " (memory cap NOT enforced: prlimit binary absent)"
		}
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
		for k, v := range env {
			if k == "" || strings.Contains(k, "=") {
				continue
			}
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if os.Geteuid() == 0 {
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
			if !strings.Contains(sandbox, "uid 65534") {
				sandbox += " + uid 65534 (nobody)"
			}
		}
		var out bytes.Buffer
		lw := &limitedWriter{w: &out, limit: maxCommandOutput}
		cmd.Stdout = io.MultiWriter(logW, lw)
		cmd.Stderr = io.MultiWriter(logW, lw)
		err := cmd.Run()
		cancel()
		if runCtx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(logW, "[command timed out after %ds]\n", timeoutSecs)
			return CommandResult{ExitCode: -1, Output: out.String(), TimedOut: true, DurationMs: time.Since(start).Milliseconds(), Sandbox: sandbox}
		}
		if err != nil {
			code := -1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			}
			fmt.Fprintf(logW, "[command failed with exit code %d: %v]\n", code, err)
			return CommandResult{ExitCode: code, Output: out.String(), DurationMs: time.Since(start).Milliseconds(), Sandbox: sandbox}
		}
	}
	return CommandResult{ExitCode: 0, Output: "", DurationMs: time.Since(start).Milliseconds(), Sandbox: sandbox}
}

type limitedWriter struct {
	w     *bytes.Buffer
	limit int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.w.Len() >= l.limit {
		return len(p), nil
	}
	room := l.limit - l.w.Len()
	if len(p) > room {
		p = p[:room]
	}
	return l.w.Write(p)
}

// RunArgv runs one argv command (no shell) with a timeout, for ECS
// tasks. It returns the started *exec.Cmd, its context (for timeout
// detection), a cancel func, and the sandbox description. Output goes
// to logW (capped).
func RunArgv(ctx context.Context, dir string, env map[string]string, argv []string, timeoutSecs, memoryMB int, logW io.Writer) (*exec.Cmd, context.Context, context.CancelFunc, string) {
	sandbox := "subprocess: timeout + output cap + temp workdir"
	if timeoutSecs <= 0 || timeoutSecs > 3600 {
		timeoutSecs = 3600
	}
	if memoryMB <= 0 || memoryMB > 4096 {
		memoryMB = 512
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	var cmd *exec.Cmd
	if _, err := exec.LookPath("prlimit"); err == nil {
		args := append([]string{"--as=" + strconv.Itoa(memoryMB*1024*1024)}, argv...)
		cmd = exec.CommandContext(runCtx, "prlimit", args...)
		sandbox += fmt.Sprintf(" + prlimit memory cap %d MiB", memoryMB)
	} else {
		cmd = exec.CommandContext(runCtx, argv[0], argv[1:]...)
		sandbox += " (memory cap NOT enforced: prlimit binary absent)"
	}
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for k, v := range env {
		if k == "" || strings.Contains(k, "=") {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if os.Geteuid() == 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}, Setpgid: true}
		sandbox += " + uid 65534 (nobody)"
	}
	if logW != nil {
		cmd.Stdout = logW
		cmd.Stderr = logW
	}
	return cmd, runCtx, cancel, sandbox
}

// --- ECS task runner ---------------------------------------------------------

// TaskCompletion is delivered when a runner-managed task stops.
type TaskCompletion struct {
	TaskID   string
	ExitCode int
	ErrMsg   string
	TimedOut bool
}

// TaskRunner runs ECS/Fargate tasks as sandboxed subprocesses and
// tracks them by task id. Status is read from the real process:
// a task is RUNNING only while its process lives.
type TaskRunner struct {
	Workdir string
	// OnStop is called (in its own goroutine) when a task stops.
	OnStop func(c TaskCompletion)

	mu      sync.Mutex
	running map[string]*runningTask
}

type runningTask struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	log    *os.File
}

func NewTaskRunner(workdir string) *TaskRunner {
	return &TaskRunner{Workdir: workdir, running: map[string]*runningTask{}}
}

// LogPath is the on-disk log for a task.
func (r *TaskRunner) LogPath(taskID string) string {
	return filepath.Join(r.Workdir, "ecs-tasks", taskID+".log")
}

// Start launches argv as taskID's process. The task's workdir is a
// fresh temp dir under Workdir/ecs-tasks/<id>.work; logs go to
// LogPath(taskID). An already-running task id is an error.
func (r *TaskRunner) Start(taskID string, argv []string, env map[string]string, timeoutSecs, memoryMB int) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("task command is empty")
	}
	r.mu.Lock()
	if _, ok := r.running[taskID]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("task %s is already running", taskID)
	}
	r.mu.Unlock()
	base := filepath.Join(r.Workdir, "ecs-tasks")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	// MkdirAll honours the process umask (0007 in this sandbox
	// turns 0755 into 0750), so chmod explicitly — the child may
	// run as uid 65534 (nobody) and must traverse the workdir.
	// The workdir root itself must also be traversable; in
	// production it is created 0755 by the operator/compose
	// volume, and tests chmod it.
	_ = os.Chmod(base, 0o755)
	workdir := filepath.Join(base, taskID+".work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return "", err
	}
	_ = os.Chmod(workdir, 0o755)
	// The task runs as uid 65534 and must be able to write its
	// workdir (builds write files); chown it to nobody when the
	// API runs as root. The dir holds only this task's files.
	if os.Geteuid() == 0 {
		_ = os.Chown(workdir, 65534, 65534)
	}
	logF, err := os.OpenFile(r.LogPath(taskID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(logF, "[cloudforge ecs] starting: %s (sandbox: subprocess, not a container)\n", strings.Join(argv, " "))
	cmd, runCtx, cancel, sandbox := RunArgv(context.Background(), workdir, env, argv, timeoutSecs, memoryMB, logF)
	fmt.Fprintf(logF, "[cloudforge ecs] sandbox: %s\n", sandbox)
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logF.Close()
		return "", fmt.Errorf("could not start task process: %w", err)
	}
	r.mu.Lock()
	r.running[taskID] = &runningTask{cmd: cmd, cancel: cancel, log: logF}
	r.mu.Unlock()
	go func() {
		err := cmd.Wait()
		exitCode := 0
		errMsg := ""
		timedOut := false
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
			errMsg = err.Error()
			if runCtx.Err() == context.DeadlineExceeded {
				timedOut = true
				errMsg = fmt.Sprintf("task timed out after %ds", timeoutSecs)
			}
		}
		fmt.Fprintf(logF, "[cloudforge ecs] stopped: exit code %d%s\n", exitCode, func() string {
			if errMsg != "" {
				return " (" + errMsg + ")"
			}
			return ""
		}())
		_ = logF.Close()
		r.mu.Lock()
		delete(r.running, taskID)
		r.mu.Unlock()
		cancel()
		if r.OnStop != nil {
			r.OnStop(TaskCompletion{TaskID: taskID, ExitCode: exitCode, ErrMsg: errMsg, TimedOut: timedOut})
		}
	}()
	return sandbox, nil
}

// IsRunning reports whether the task's process is still alive.
func (r *TaskRunner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[taskID]
	return ok
}

// Stop kills a running task (whole process group when setpgid
// applied, else the process). A task that already stopped is not
// an error — the caller checks the stored status.
func (r *TaskRunner) Stop(taskID string) error {
	r.mu.Lock()
	rt, ok := r.running[taskID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if rt.cmd.Process != nil {
		// Kill the process group when we started one; fall back to
		// the single process.
		if pgid, err := syscall.Getpgid(rt.cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		_ = rt.cmd.Process.Kill()
	}
	rt.cancel()
	return nil
}

// ReadLog returns the task log (tail, max 64 KiB), or "" when the
// task has not produced output yet.
func (r *TaskRunner) ReadLog(taskID string) (string, error) {
	data, err := os.ReadFile(r.LogPath(taskID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(data) > 64*1024 {
		data = data[len(data)-64*1024:]
	}
	return string(data), nil
}

// --- CodeBuild buildspec -----------------------------------------------------

type buildspecPhase struct {
	Commands []string `yaml:"commands"`
}

type buildspecArtifacts struct {
	Files []string `yaml:"files"`
}

// Buildspec is the subset of buildspec.yml CloudForge executes:
// version + phases (install / pre_build / build / post_build) +
// artifacts.files globs. Anything else in the file is ignored, not
// executed.
type Buildspec struct {
	Version   string                       `yaml:"version"`
	Phases    map[string]buildspecPhase    `yaml:"phases"`
	Artifacts buildspecArtifacts           `yaml:"artifacts"`
	Env       map[string]map[string]string `yaml:"env"`
}

// ParseBuildspec parses buildspec YAML. An empty document is an
// error (a build with no steps is a misconfiguration, not a
// success); a buildspec with no phases is also an error.
func ParseBuildspec(raw []byte) (*Buildspec, error) {
	var bs Buildspec
	if err := yaml.Unmarshal(raw, &bs); err != nil {
		return nil, fmt.Errorf("buildspec is not valid YAML: %w", err)
	}
	if len(bs.Phases) == 0 {
		return nil, errors.New("buildspec defines no phases (need at least a build phase with commands)")
	}
	for name, ph := range bs.Phases {
		if name != "install" && name != "pre_build" && name != "build" && name != "post_build" {
			return nil, fmt.Errorf("buildspec phase %q is not supported (install, pre_build, build, post_build only)", name)
		}
		_ = ph
	}
	return &bs, nil
}

// BuildspecPhaseOrder is the order phases execute in.
var BuildspecPhaseOrder = []string{"install", "pre_build", "build", "post_build"}

// --- CloudFormation template engine ------------------------------------------

// CFNResource is one resource in a template.
type CFNResource struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

// CFNTemplate is a parsed CloudFormation template (JSON or YAML).
type CFNTemplate struct {
	AWSTemplateFormatVersion string                  `json:"aws_template_format_version" yaml:"AWSTemplateFormatVersion"`
	Description              string                  `json:"description" yaml:"Description"`
	Parameters               map[string]CFNParameter `json:"parameters" yaml:"Parameters"`
	Resources                map[string]CFNResource  `json:"resources" yaml:"Resources"`
}

// CFNParameter is one template parameter.
type CFNParameter struct {
	Type    string `json:"type" yaml:"Type"`
	Default any    `json:"default" yaml:"Default"`
}

// SupportedCFNTypes is the set of resource types CloudFormation in
// CloudForge actually provisions. Anything else is reported
// UNSUPPORTED in the change set — never silently skipped.
var SupportedCFNTypes = map[string]bool{
	"AWS::SQS::Queue":       true,
	"AWS::SNS::Topic":       true,
	"AWS::DynamoDB::Table":  true,
	"AWS::Lambda::Function": true,
	"AWS::S3::Bucket":       true,
	"AWS::RDS::DBInstance":  true,
}

// SupportedCFNTypeList is the sorted list, for error messages.
func SupportedCFNTypeList() []string {
	out := []string{}
	for t := range SupportedCFNTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ParseCFNTemplate parses a JSON or YAML template. JSON is tried
// first (a JSON document is valid YAML too, but the JSON path gives
// better errors for JSON input); YAML keys are the AWS names
// (AWSTemplateFormatVersion / Description / Parameters / Resources).
func ParseCFNTemplate(raw []byte) (*CFNTemplate, error) {
	if len(raw) == 0 {
		return nil, errors.New("template is empty")
	}
	if len(raw) > 1024*1024 {
		return nil, errors.New("template is capped at 1 MiB")
	}
	// Try JSON first: strict field names.
	var jt struct {
		AWSTemplateFormatVersion string                  `json:"AWSTemplateFormatVersion"`
		Description              string                  `json:"Description"`
		Parameters               map[string]CFNParameter `json:"Parameters"`
		Resources                map[string]struct {
			Type       string         `json:"Type"`
			Properties map[string]any `json:"Properties"`
		} `json:"Resources"`
	}
	if err := json.Unmarshal(raw, &jt); err == nil && jt.Resources != nil {
		tpl := &CFNTemplate{
			AWSTemplateFormatVersion: jt.AWSTemplateFormatVersion,
			Description:              jt.Description,
			Parameters:               jt.Parameters,
			Resources:                map[string]CFNResource{},
		}
		for name, r := range jt.Resources {
			tpl.Resources[name] = CFNResource{Type: r.Type, Properties: r.Properties}
		}
		return tpl, nil
	}
	// YAML path.
	var yt struct {
		AWSTemplateFormatVersion string `yaml:"AWSTemplateFormatVersion"`
		Description              string `yaml:"Description"`
		Parameters               map[string]struct {
			Type    string `yaml:"Type"`
			Default any    `yaml:"Default"`
		} `yaml:"Parameters"`
		Resources map[string]struct {
			Type       string         `yaml:"Type"`
			Properties map[string]any `yaml:"Properties"`
		} `yaml:"Resources"`
	}
	if err := yaml.Unmarshal(raw, &yt); err != nil {
		return nil, fmt.Errorf("template is neither valid JSON nor valid YAML: %w", err)
	}
	if yt.Resources == nil {
		return nil, errors.New("template defines no Resources")
	}
	tpl := &CFNTemplate{
		AWSTemplateFormatVersion: yt.AWSTemplateFormatVersion,
		Description:              yt.Description,
		Parameters:               map[string]CFNParameter{},
		Resources:                map[string]CFNResource{},
	}
	for name, p := range yt.Parameters {
		tpl.Parameters[name] = CFNParameter{Type: p.Type, Default: p.Default}
	}
	for name, r := range yt.Resources {
		tpl.Resources[name] = CFNResource{Type: r.Type, Properties: r.Properties}
	}
	return tpl, nil
}

// ValidateCFNTemplate checks a parsed template and returns the list
// of problems (empty = valid). Unknown resource types and missing
// required properties are errors, not warnings.
func ValidateCFNTemplate(tpl *CFNTemplate) []string {
	problems := []string{}
	if len(tpl.Resources) == 0 {
		problems = append(problems, "template defines no Resources")
	}
	names := []string{}
	for name := range tpl.Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		res := tpl.Resources[name]
		if res.Type == "" {
			problems = append(problems, fmt.Sprintf("resource %q has no Type", name))
			continue
		}
		if !SupportedCFNTypes[res.Type] {
			problems = append(problems, fmt.Sprintf("resource %q type %q is not supported (supported: %s)", name, res.Type, strings.Join(SupportedCFNTypeList(), ", ")))
		}
		switch res.Type {
		case "AWS::Lambda::Function":
			if code, _ := res.Properties["Code"].(map[string]any); code == nil {
				problems = append(problems, fmt.Sprintf("resource %q (Lambda) needs Properties.Code.ZipFile", name))
			}
		case "AWS::RDS::DBInstance":
			if _, ok := res.Properties["DBName"]; !ok {
				problems = append(problems, fmt.Sprintf("resource %q (RDS) needs Properties.DBName", name))
			}
		}
	}
	return problems
}

// CFNChange is one entry of a change set.
type CFNChange struct {
	LogicalID string `json:"logical_id"`
	Type      string `json:"type"`
	Action    string `json:"action"` // CREATE | UPDATE | DELETE | NO_CHANGE | UNSUPPORTED
	Reason    string `json:"reason,omitempty"`
}

// PlanCFNChanges diffs the desired template against the stack's
// current resources (logical id -> type + physical id). A resource
// whose type changed is DELETE + CREATE. Unsupported types are
// reported as UNSUPPORTED, never as CREATE.
func PlanCFNChanges(tpl *CFNTemplate, current map[string]map[string]any) []CFNChange {
	changes := []CFNChange{}
	names := []string{}
	for name := range tpl.Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		res := tpl.Resources[name]
		if !SupportedCFNTypes[res.Type] {
			changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "UNSUPPORTED",
				Reason: fmt.Sprintf("type %q is not provisioned by CloudFormation (supported: %s)", res.Type, strings.Join(SupportedCFNTypeList(), ", "))})
			continue
		}
		cur, exists := current[name]
		if !exists {
			changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "CREATE"})
			continue
		}
		if curType, _ := cur["type"].(string); curType != res.Type {
			changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "DELETE", Reason: "type changed from " + curType})
			changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "CREATE"})
			continue
		}
		// Same type: NO_CHANGE when the desired properties match
		// the properties stored at provision time (best-effort:
		// templates using Ref in properties compare raw against
		// resolved values and may report UPDATE spuriously — that
		// only means the change set re-applies them, never that a
		// change is invented).
		if stored, ok := cur["properties"].(map[string]any); ok {
			want, _ := json.Marshal(res.Properties)
			have, _ := json.Marshal(stored)
			if string(want) == string(have) {
				changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "NO_CHANGE", Reason: "properties are unchanged"})
				continue
			}
		}
		changes = append(changes, CFNChange{LogicalID: name, Type: res.Type, Action: "UPDATE", Reason: "properties are re-applied; in-place update is best-effort per resource type"})
	}
	curNames := []string{}
	for name := range current {
		curNames = append(curNames, name)
	}
	sort.Strings(curNames)
	for _, name := range curNames {
		if _, ok := tpl.Resources[name]; !ok {
			typ, _ := current[name]["type"].(string)
			changes = append(changes, CFNChange{LogicalID: name, Type: typ, Action: "DELETE", Reason: "removed from template"})
		}
	}
	return changes
}

// CFNResolveValue resolves Ref / Fn::Join / Fn::Sub in a template
// value. params are the stack's parameter values; physical maps a
// logical id to its provisioned physical id. Unresolvable values
// are returned as-is (the provisioner validates required fields).
func CFNResolveValue(v any, params map[string]any, physical map[string]string) any {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 1 {
			if ref, ok := t["Ref"].(string); ok {
				if pv, ok := params[ref]; ok {
					return pv
				}
				if pid, ok := physical[ref]; ok {
					return pid
				}
				return v
			}
			if join, ok := t["Fn::Join"].([]any); ok && len(join) == 2 {
				sep, _ := join[0].(string)
				parts, _ := join[1].([]any)
				resolved := []string{}
				for _, p := range parts {
					resolved = append(resolved, fmt.Sprintf("%v", CFNResolveValue(p, params, physical)))
				}
				return strings.Join(resolved, sep)
			}
			if sub, ok := t["Fn::Sub"].(string); ok {
				out := sub
				for k, pv := range params {
					out = strings.ReplaceAll(out, "${"+k+"}", fmt.Sprintf("%v", pv))
				}
				for k, pid := range physical {
					out = strings.ReplaceAll(out, "${"+k+"}", pid)
				}
				return out
			}
		}
		out := map[string]any{}
		for k, val := range t {
			out[k] = CFNResolveValue(val, params, physical)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = CFNResolveValue(val, params, physical)
		}
		return out
	default:
		return v
	}
}

// --- EKS decision ------------------------------------------------------------

// EKSDecision is the Phase 4 decision on EKS, recorded in code so
// the console, the registry and the docs cannot drift apart.
//
// Decision (b): EKS stays UNSUPPORTED. A real local k3s/kind cluster
// was not built because (1) there is no Docker or Kubernetes in
// this sandbox to verify a cluster against, and building an
// unverifiable cluster path would repeat the Lambda mistake the
// Phase 0 audit flagged (a registry claim with no working backend);
// (2) a managed-Kubernetes control plane is the heaviest component
// in this catalogue and would dominate the lite classroom-laptop
// mode the user asked to keep working; (3) the Kubernetes concepts
// a learner needs first — Pod, Deployment, Service, ConfigMap —
// map directly onto the ECS runtime that Phase 4 does build, so
// the learning path below teaches them for real instead of against
// a diagram. A k3s-based EKS remains a possible future phase on a
// real Docker/KVM host, gated behind a capability check.
const EKSDecision = "UNSUPPORTED"

// EKSDecisionReason is the one-line reason shown in the console.
const EKSDecisionReason = "No local Kubernetes cluster is built: k3s/kind cannot be verified in this sandbox (no Docker/KVM), and a managed control plane would break lite mode. Kubernetes concepts are taught hands-on through the ECS runtime instead — see the learning path below."

// K8sConcept maps one Kubernetes concept onto the CloudForge
// runtime that teaches it for real.
type K8sConcept struct {
	Concept    string `json:"concept"`
	WhatItIs   string `json:"what_it_is"`
	CloudForge string `json:"cloudforge_equivalent"`
	Route      string `json:"route"`
	TryIt      string `json:"try_it"`
}

// K8sLearningPath is the Kubernetes-concepts learning path served
// by GET /api/v1/aws/eks/learning-path and rendered by the EKS
// console. Every row has a working "try it" action on a real
// runtime — none of it is a static diagram presented as a cluster.
var K8sLearningPath = []K8sConcept{
	{Concept: "Pod", WhatItIs: "The smallest deployable unit: one or more containers sharing a network namespace.",
		CloudForge: "An ECS task — one task definition's containers run together as one sandboxed process group.", Route: "/services/ecs", TryIt: "Run a task from the ECS console and watch its live logs."},
	{Concept: "Deployment", WhatItIs: "Declares a desired replica count; the controller keeps that many Pods running and rolls updates out.",
		CloudForge: "An ECS service — desired count is reconciled on every read, and updating the task definition rolls the tasks.", Route: "/services/ecs", TryIt: "Create a service with desired count 2, then update its task definition."},
	{Concept: "Service", WhatItIs: "A stable network endpoint in front of a set of Pods, with load balancing.",
		CloudForge: "An ELB target group — targets are probed and the load balancer round-robins only to healthy ones.", Route: "/services/elb", TryIt: "Register two targets and run a health check."},
	{Concept: "ConfigMap / Secret", WhatItIs: "Configuration and sensitive values injected into containers as env vars or files.",
		CloudForge: "Task-definition environment variables and Lambda function env vars (never put real secrets in either).", Route: "/services/ecs", TryIt: "Set an env var on a task definition and print it from the task."},
	{Concept: "Namespace", WhatItIs: "A scope that isolates groups of resources inside one cluster.",
		CloudForge: "An ECS cluster — tasks and services live in a named cluster, and Fargate runs without one.", Route: "/services/ecs", TryIt: "Create a cluster and run a task inside it."},
	{Concept: "Job / CronJob", WhatItIs: "Run-to-completion work, once or on a schedule.",
		CloudForge: "A CodeBuild build — a real buildspec run with logs and artifacts — or a one-shot ECS task.", Route: "/services/codebuild", TryIt: "Start a build from a local git repo and read its real logs."},
}
