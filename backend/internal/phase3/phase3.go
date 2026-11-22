// Package phase3 holds the service semantics for CloudForge v2
// Phase 3 (data, messaging, serverless). Handlers in httpapi own the
// HTTP surface and persistence (store.Phase3Store); this package owns
// the rules that must be the same wherever they are used: DynamoDB
// item key handling, SQS visibility/DLQ, EventBridge pattern
// matching, DNS record validation, the Lambda subprocess runner, and
// the RDS / ElastiCache helpers that talk to the real platform
// Postgres / Redis.
//
// Fidelity, stated once here so handlers, consoles and docs agree:
//   - DynamoDB: API_EMULATED when DynamoDB Local answers (the
//     adapter speaks the real DynamoDB JSON API); otherwise a
//     Postgres-backed subset (PARTIAL) of CreateTable/PutItem/
//     GetItem/Query/Scan/DeleteItem/DeleteTable. Never full DynamoDB.
//   - RDS: a real Postgres database + role on the platform Postgres
//     ("PostgreSQL-compatible, not Aurora"). Start/stop per instance
//     is UNSUPPORTED — a shared platform database cannot be stopped
//     per instance. Snapshots are real pg_dump files. A containerised
//     per-instance Postgres (full mode) is NOT built: there is no
//     Docker in this sandbox to verify it against, and building an
//     unverifiable Docker path would repeat the Lambda mistake the
//     Phase 0 audit flagged.
//   - ElastiCache: a real namespaced Redis database (key prefix +
//     best-effort ACL user) on the platform Redis in lite mode; a
//     dedicated Redis container in full mode is NOT built (same
//     reason as RDS containers).
//   - Lambda: real execution in a sandboxed subprocess (python3 /
//     node, timeout, output cap, memory cap via prlimit when the
//     binary exists, runs as uid 65534/nobody when the API runs as
//     root, temp workdir only, no host shell). That is NOT container
//     isolation and NOT AWS Lambda: code runs as an unprivileged
//     local user, not in a VM/container, and must not be given
//     untrusted code from strangers. The console and docs say so.
//   - ELB / Route 53: SIMULATED. The load balancer round-robins
//     in-process to targets it can probe (HTTP/TCP); DNS records are
//     durable and resolvable through the resolve endpoint, but no
//     DNS server serves them to the lab network.
package phase3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// --- generic data accessors (entity Data is map[string]any) -----------

func Str(d map[string]any, key, def string) string {
	if v, ok := d[key].(string); ok {
		return v
	}
	return def
}

func Float(d map[string]any, key string, def float64) float64 {
	switch v := d[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return def
}

func Int(d map[string]any, key string, def int) int { return int(Float(d, key, float64(def))) }

func Bool(d map[string]any, key string, def bool) bool {
	if v, ok := d[key].(bool); ok {
		return v
	}
	return def
}

func StrSlice(d map[string]any, key string) []string {
	out := []string{}
	switch v := d[key].(type) {
	case []string:
		return v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// --- DynamoDB item handling --------------------------------------------
//
// Items are stored either DynamoDB-typed ({"id": {"S": "1"}} — the
// shape the DynamoDB Local adapter speaks) or plain
// ({"id": "1"} — the shape the Postgres subset stores). Both are
// accepted on input; Plain converts typed to plain for display and
// for the Postgres subset's key lookup, Typed converts plain to
// typed for the adapter.

// DynamoPlain converts a DynamoDB-typed item to plain values.
func DynamoPlain(item map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range item {
		if m, ok := v.(map[string]any); ok && len(m) == 1 {
			for t, val := range m {
				switch t {
				case "S", "N", "BOOL":
					out[k] = val
				case "NULL":
					out[k] = nil
				default:
					out[k] = v
				}
			}
			continue
		}
		out[k] = v
	}
	return out
}

// DynamoTyped converts a plain item to DynamoDB-typed values
// (strings -> S, numbers -> N, bools -> BOOL, nil -> NULL).
func DynamoTyped(item map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range item {
		switch t := v.(type) {
		case string:
			out[k] = map[string]any{"S": t}
		case float64:
			out[k] = map[string]any{"N": strconv.FormatFloat(t, 'f', -1, 64)}
		case bool:
			out[k] = map[string]any{"BOOL": t}
		case nil:
			out[k] = map[string]any{"NULL": true}
		default:
			out[k] = map[string]any{"S": fmt.Sprintf("%v", v)}
		}
	}
	return out
}

// DynamoKeyValue extracts the partition-key value (plain string) from
// an item given the key attribute name. "" means the item has no key.
func DynamoKeyValue(item map[string]any, keyAttr string) string {
	plain := DynamoPlain(item)
	v, ok := plain[keyAttr]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// --- SQS semantics -------------------------------------------------------

// SQSMessageVisible reports whether a stored message may be received
// now (visible_at passed and not expired). Times are RFC3339 in data.
func SQSMessageVisible(d map[string]any, now time.Time) bool {
	if exp := Str(d, "expires_at", ""); exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil && now.After(t) {
			return false
		}
	}
	if vis := Str(d, "visible_at", ""); vis != "" {
		if t, err := time.Parse(time.RFC3339, vis); err == nil && now.Before(t) {
			return false
		}
	}
	return true
}

// SQSShouldDLQ reports whether a message has been received too many
// times for its queue (receive_count >= max_receives, max>0).
func SQSShouldDLQ(msgData, queueData map[string]any) bool {
	max := Int(queueData, "max_receives", 0)
	if max <= 0 {
		return false
	}
	return Int(msgData, "receive_count", 0) >= max
}

// --- EventBridge pattern matching ----------------------------------------
//
// A rule pattern is a subset of the AWS event-pattern language:
//   {"source": ["a","b"], "detail-type": ["x"], "detail": {"k": ["v"]}}
// Every key present in the pattern must match; keys absent from the
// pattern are ignored. Values are arrays (match if the event value
// is in the array) or nested objects (recursive subset match).

func PatternMatches(pattern, event map[string]any) bool {
	if len(pattern) == 0 {
		return true
	}
	for key, want := range pattern {
		got, ok := event[key]
		if !ok {
			return false
		}
		if !patternValueMatches(want, got) {
			return false
		}
	}
	return true
}

func patternValueMatches(want, got any) bool {
	switch w := want.(type) {
	case []any:
		for _, cand := range w {
			if fmt.Sprintf("%v", cand) == fmt.Sprintf("%v", got) {
				return true
			}
		}
		return false
	case map[string]any:
		gm, ok := got.(map[string]any)
		if !ok {
			return false
		}
		return PatternMatches(w, gm)
	default:
		return fmt.Sprintf("%v", want) == fmt.Sprintf("%v", got)
	}
}

// --- DNS validation ------------------------------------------------------

var dnsNameRE = regexp.MustCompile(`^(\*|[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)

// ValidateDNSRecord checks a Route 53-style record. A/CNAME/TXT are
// supported; anything else is rejected with a reason (honest
// UNSUPPORTED, not a silent accept).
func ValidateDNSRecord(name, rtype string, values []string) error {
	if !dnsNameRE.MatchString(name) || len(name) > 253 {
		return errors.New("record name is not a valid DNS name")
	}
	switch rtype {
	case "A":
		if len(values) == 0 {
			return errors.New("A record needs at least one IPv4 address")
		}
		for _, v := range values {
			ip := net.ParseIP(v)
			if ip == nil || ip.To4() == nil {
				return fmt.Errorf("A record value %q is not an IPv4 address", v)
			}
		}
	case "CNAME":
		if len(values) != 1 {
			return errors.New("CNAME record needs exactly one target")
		}
		if !dnsNameRE.MatchString(values[0]) {
			return fmt.Errorf("CNAME target %q is not a valid DNS name", values[0])
		}
	case "TXT":
		if len(values) == 0 {
			return errors.New("TXT record needs at least one value")
		}
	default:
		return fmt.Errorf("record type %q is not supported (only A, CNAME, TXT)", rtype)
	}
	return nil
}

// ResolveDNS finds the records answering (name, type) in a zone's
// record list, honouring a leading "*." wildcard record. It returns
// the matching record data maps (possibly empty).
func ResolveDNS(records []map[string]any, name, rtype string) []map[string]any {
	out := []map[string]any{}
	for _, rec := range records {
		if Str(rec, "type", "") != rtype {
			continue
		}
		rname := Str(rec, "name", "")
		if rname == name {
			out = append(out, rec)
			continue
		}
		if strings.HasPrefix(rname, "*.") && strings.HasSuffix(name, rname[1:]) {
			out = append(out, rec)
		}
	}
	return out
}

// --- ELB round-robin ------------------------------------------------------

// RoundRobin picks the next healthy target index. The counter is
// process-local (a restart resets the rotation — documented on the
// load balancer detail view); health, not the counter, decides
// whether a target receives traffic.
type RoundRobin struct{ n atomic.Uint64 }

func (r *RoundRobin) Next(count int) int {
	if count <= 0 {
		return -1
	}
	return int(r.n.Add(1)-1) % count
}

// ProbeTarget checks a target address: http(s) URLs get a GET with a
// 2 s timeout (2xx/3xx = healthy). A bare host is probed as an HTTP
// GET to http://host:port<healthCheckPath> when a health-check path
// is set, else a TCP dial of host:port. The port is joined here —
// callers must pass it, because dialing a bare host without a port
// fails ("missing port in address") and would mark every target
// unhealthy (seen live 2026-11-21: localhost:8081 reported
// unhealthy for exactly this reason).
func ProbeTarget(ctx context.Context, address string, port int, healthCheckPath string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return probeHTTP(ctx, address)
	}
	host := address
	if port > 0 {
		host = net.JoinHostPort(address, strconv.Itoa(port))
	}
	if healthCheckPath != "" {
		if !strings.HasPrefix(healthCheckPath, "/") {
			healthCheckPath = "/" + healthCheckPath
		}
		return probeHTTP(ctx, "http://"+host+healthCheckPath)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return false, err.Error()
	}
	conn.Close()
	return true, "tcp connect ok"
}

func probeHTTP(ctx context.Context, url string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		return true, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// --- Lambda runner ---------------------------------------------------------

// SupportedRuntimes maps the runtime names the console offers to the
// local binary that executes them. python3/node are the runtimes
// that actually execute here; anything else is rejected, not faked.
var SupportedRuntimes = map[string]struct {
	Binary string
	File   string
}{
	"python3.12": {"python3", "handler.py"},
	"python3.11": {"python3", "handler.py"},
	"python3":    {"python3", "handler.py"},
	"nodejs20":   {"node", "handler.js"},
	"nodejs22":   {"node", "handler.js"},
	"node":       {"node", "handler.js"},
}

// LambdaResult is one invocation outcome.
type LambdaResult struct {
	Output     string `json:"output"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
	TimedOut   bool   `json:"timed_out"`
	// Sandbox notes what isolation actually applied, so the console
	// can show it instead of claiming container isolation.
	Sandbox string `json:"sandbox"`
}

const lambdaMaxOutput = 64 * 1024 // stdout/stderr cap, bytes

// RunLambda executes function code as a sandboxed subprocess. The
// contract: code is a whole program; the event arrives as JSON on
// stdin; the result is whatever the program prints on stdout.
// No host shell is involved (exec.Command with argv, never sh -c),
// the workdir is a fresh temp dir, env is only the function's env
// plus PATH/HOME, the run is killed at timeout, stdout is capped,
// and when the API runs as root the child runs as uid 65534
// (nobody). Memory is capped with prlimit when that binary exists;
// when it does not, the result says so instead of claiming a cap.
func RunLambda(ctx context.Context, runtime, code string, env map[string]string, event any, timeoutSecs, memoryMB int) LambdaResult {
	start := time.Now()
	rt, ok := SupportedRuntimes[runtime]
	if !ok {
		return LambdaResult{Error: fmt.Sprintf("runtime %q is not supported (python3 / nodejs only)", runtime), DurationMs: time.Since(start).Milliseconds(), Sandbox: "rejected: unsupported runtime"}
	}
	if timeoutSecs <= 0 || timeoutSecs > 60 {
		timeoutSecs = 10
	}
	if memoryMB <= 0 || memoryMB > 1024 {
		memoryMB = 256
	}
	dir, err := os.MkdirTemp("", "cf-lambda-*")
	if err != nil {
		return LambdaResult{Error: "could not create invocation workdir: " + err.Error(), DurationMs: time.Since(start).Milliseconds(), Sandbox: "none"}
	}
	defer os.RemoveAll(dir)
	// The child may run as uid 65534 (nobody), so the workdir and
	// the code file must be readable by it. The dir holds only this
	// invocation's code — nothing secret lives there.
	_ = os.Chmod(dir, 0o755)
	if err := os.WriteFile(dir+"/"+rt.File, []byte(code), 0o644); err != nil {
		return LambdaResult{Error: "could not write function code: " + err.Error(), DurationMs: time.Since(start).Milliseconds(), Sandbox: "none"}
	}
	// Explicit chmod: WriteFile's perm is masked by the process
	// umask (0007 in this sandbox turns 0644 into 0640, which the
	// nobody child cannot read).
	_ = os.Chmod(dir+"/"+rt.File, 0o644)
	eventJSON, _ := json.Marshal(event)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	sandbox := "subprocess: timeout + output cap + temp workdir"
	if _, err := exec.LookPath("prlimit"); err == nil {
		// prlimit --as=<bytes> <binary> <file> — memory cap enforced
		// by the kernel for the child.
		cmd = exec.CommandContext(runCtx, "prlimit",
			"--as="+strconv.Itoa(memoryMB*1024*1024),
			rt.Binary, dir+"/"+rt.File)
		sandbox += fmt.Sprintf(" + prlimit memory cap %d MiB", memoryMB)
	} else {
		cmd = exec.CommandContext(runCtx, rt.Binary, dir+"/"+rt.File)
		sandbox += " (memory cap NOT enforced: prlimit binary absent)"
	}
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(eventJSON)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for k, v := range env {
		if k == "" || strings.Contains(k, "=") {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if os.Geteuid() == 0 {
		// Run unprivileged when the API itself is root (sandbox and
		// dev runs). On a non-root API this is a no-op: the child
		// simply runs as the API user.
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
		sandbox += " + uid 65534 (nobody)"
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &outBuf, limit: lambdaMaxOutput}
	cmd.Stderr = &limitedWriter{w: &errBuf, limit: lambdaMaxOutput}
	err = cmd.Run()
	dur := time.Since(start).Milliseconds()
	res := LambdaResult{Output: outBuf.String(), DurationMs: dur, Sandbox: sandbox}
	if errBuf.Len() > 0 {
		res.Output += "\n[stderr] " + errBuf.String()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Error = fmt.Sprintf("invocation timed out after %ds", timeoutSecs)
		return res
	}
	if err != nil {
		res.Error = "exit error: " + err.Error()
	}
	return res
}

type limitedWriter struct {
	w     *bytes.Buffer
	limit int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.w.Len() >= l.limit {
		return len(p), nil // drop the rest, keep the cap
	}
	room := l.limit - l.w.Len()
	if len(p) > room {
		p = p[:room]
	}
	return l.w.Write(p)
}

// --- RDS helpers (real platform Postgres) ----------------------------------

var pgIdentRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

// ValidatePGIdentifier rejects names Postgres would reject or that
// could smuggle SQL (identifiers are quoted, but the allowlist is
// the defence in depth).
func ValidatePGIdentifier(name string) error {
	if !pgIdentRE.MatchString(name) {
		return errors.New("name must start with a letter/underscore and contain only letters, digits and underscores (max 63 chars)")
	}
	return nil
}

// RDSCreateDatabase creates a role and a database owned by it on the
// platform Postgres (dsn = the API's DATABASE_URL). The role password
// is supplied by the caller (generated, shown once). It connects to
// the maintenance database (postgres) and issues CREATE ROLE /
// CREATE DATABASE; both are quoted identifiers.
func RDSCreateDatabase(ctx context.Context, dsn, dbName, role, password string) error {
	if err := ValidatePGIdentifier(dbName); err != nil {
		return fmt.Errorf("database name: %w", err)
	}
	if err := ValidatePGIdentifier(role); err != nil {
		return fmt.Errorf("role name: %w", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE ROLE %q LOGIN PASSWORD '%s'`, role, strings.ReplaceAll(password, "'", "''"))); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	// CREATE DATABASE ... OWNER <role> requires the provisioning
	// user to be a member of the new role (or a superuser). The
	// role creator has ADMIN OPTION on it, so grant membership to
	// the provisioning user from the DSN first; without this the
	// create fails with "must be able to SET ROLE" (seen live
	// 2026-11-21). Membership also lets the same user drop the
	// database later and dump it for snapshots.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %q`, role))
		return err
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`GRANT %q TO %q`, role, cfg.ConnConfig.User)); err != nil {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %q`, role))
		return fmt.Errorf("grant role: %w", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q OWNER %q`, dbName, role)); err != nil {
		// Roll back the role so a failed create leaves nothing behind.
		_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %q`, role))
		return fmt.Errorf("create database: %w", err)
	}
	return nil
}

// RDSDropDatabase drops the database and its role (idempotent-ish:
// missing objects are not errors, a live connection is).
func RDSDropDatabase(ctx context.Context, dsn, dbName, role string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	// Terminate other sessions on the database first, so the drop
	// does not fail on an open lab connection.
	_, _ = pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbName)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, dbName)); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	if role != "" {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %q`, role)); err != nil {
			return fmt.Errorf("drop role: %w", err)
		}
	}
	return nil
}

// RDSSnapshot runs pg_dump -Fc against the platform Postgres and
// writes filePath. pg_dump must exist on the API host (it does in
// the sandbox and in the postgres image; the distroless API image
// does NOT ship it — the handler returns an honest 503 there).
func RDSSnapshot(ctx context.Context, dsn, dbName, filePath string) (int64, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return 0, err
	}
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		return 0, errors.New("pg_dump binary is not available on the API host (snapshot unavailable)")
	}
	args := []string{"-Fc", "-f", filePath, "-h", cfg.ConnConfig.Host, "-p", strconv.Itoa(int(cfg.ConnConfig.Port)), "-U", cfg.ConnConfig.User, "-d", dbName}
	cmd := exec.CommandContext(ctx, pgDump, args...)
	cmd.Env = []string{"PGPASSWORD=" + cfg.ConnConfig.Password, "PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("pg_dump failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	fi, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// RDSRestore restores a pg_dump -Fc snapshot into dbName with
// pg_restore (clean: drop objects first). The target database must
// exist; callers create it (empty) before restoring.
func RDSRestore(ctx context.Context, dsn, dbName, filePath string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return err
	}
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		return errors.New("pg_restore binary is not available on the API host (restore unavailable)")
	}
	args := []string{"-c", "-d", dbName, "-h", cfg.ConnConfig.Host, "-p", strconv.Itoa(int(cfg.ConnConfig.Port)), "-U", cfg.ConnConfig.User, filePath}
	cmd := exec.CommandContext(ctx, pgRestore, args...)
	cmd.Env = []string{"PGPASSWORD=" + cfg.ConnConfig.Password, "PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// --- ElastiCache helpers (real platform Redis) -----------------------------

// NewRedisClient builds a go-redis client for the platform Redis.
// The caller pings it; a failed ping means the cluster is not READY.
func NewRedisClient(addr string) *redis.Client {
	if addr == "" {
		addr = "localhost:6379"
	}
	return redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
}

// CacheKeyPrefix is the namespace for one ElastiCache cluster's keys
// on the shared platform Redis (lite mode). Keys never collide
// between clusters, and the keys browser only ever SCANs its own
// prefix.
func CacheKeyPrefix(clusterID string) string { return "cf-cache:" + clusterID + ":" }
