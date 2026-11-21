package hostagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func baseReq(op Op) OpRequest {
	return OpRequest{Op: op, LabID: "lab-1", OwnerID: "user-1", Name: "web-1"}
}

func TestValidateAcceptsValidOps(t *testing.T) {
	valid := []OpRequest{
		{Op: OpListContainers, LabID: "lab-1"},
		{Op: OpCreateContainer, LabID: "lab-1", Name: "web-1", Image: "nginx:1.27"},
		{Op: OpStartContainer, LabID: "lab-1", Name: "web-1"},
		{Op: OpCreateVM, LabID: "lab-1", Name: "vm-1", Profile: "small"},
		{Op: OpHostInventory},
		{Op: OpExecInLab, LabID: "lab-1", Command: []string{"ls", "workdir"}},
		{Op: OpExecInLab, LabID: "lab-1", Command: []string{"curl", "https://api.lab.internal/health"}},
		{Op: OpExecInLab, LabID: "lab-1", Command: []string{"ping", "10.42.0.5"}},
		{Op: OpExecInLab, LabID: "lab-1", Command: []string{"terraform", "plan"}},
		{Op: OpExecInLab, LabID: "lab-1", Command: []string{"cat", "/var/lib/cloudforge/labs/lab-1/output.txt"}},
	}
	for _, req := range valid {
		if err := Validate(req.Op, req); err != nil {
			t.Errorf("Validate(%v) should pass: %v", req.Op, err)
		}
	}
}

func TestValidateRejectsEachClass(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		req  OpRequest
	}{
		{"unknown op", Op("RunAnything"), OpRequest{Op: Op("RunAnything"), LabID: "lab-1"}},
		{"op mismatch", OpListVMs, OpRequest{Op: OpListContainers, LabID: "lab-1"}},
		{"missing lab", OpListContainers, OpRequest{Op: OpListContainers}},
		{"shell metachars in name", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "web; rm -rf /", Image: "nginx"}},
		{"shell metachars in image", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "web-1", Image: "nginx$(id)"}},
		{"backtick in name", OpCreateVM, OpRequest{Op: OpCreateVM, LabID: "lab-1", Name: "vm-`id`", Profile: "small"}},
		{"pipe in lab id", OpListVMs, OpRequest{Op: OpListVMs, LabID: "lab|1"}},
		{"host path escape via ..", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w", Image: "nginx", HostPath: "/var/lib/cloudforge/labs/lab-1/../../etc/passwd"}},
		{"absolute path outside lab dirs", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w", Image: "nginx", HostPath: "/etc/passwd"}},
		{"absolute path in another lab", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w", Image: "nginx", HostPath: "/var/lib/cloudforge/labs/lab-2/data"}},
		{"relative path escape", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w", Image: "nginx", Workdir: "../../etc"}},
		{"exec non-allowlisted rm", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"rm", "-rf", "/"}}},
		{"exec sh -c", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"sh", "-c", "id"}}},
		{"exec absolute path", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"/bin/ls"}}},
		{"exec metachars in args", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"ls", "a;id"}}},
		{"exec curl external host", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"curl", "https://example.com"}}},
		{"exec ping external host", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"ping", "8.8.8.8"}}},
		{"exec docker run", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"docker", "run", "nginx"}}},
		{"exec kubectl apply", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"kubectl", "apply", "-f", "x.yaml"}}},
		{"exec cat outside lab", OpExecInLab, OpRequest{Op: OpExecInLab, LabID: "lab-1", Command: []string{"cat", "/etc/passwd"}}},
		{"unknown profile", OpCreateVM, OpRequest{Op: OpCreateVM, LabID: "lab-1", Name: "vm-1", Profile: "xxlarge"}},
		{"bad ssh key", OpCreateVM, OpRequest{Op: OpCreateVM, LabID: "lab-1", Name: "vm-1", Profile: "small", SSHKey: "not-a-key"}},
		{"cpu out of bounds", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w", Image: "nginx", CPU: 99}},
		{"missing image", OpCreateContainer, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "w"}},
		{"missing name", OpCreateVM, OpRequest{Op: OpCreateVM, LabID: "lab-1", Profile: "small"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.op, tc.req); err == nil {
				t.Fatalf("Validate should reject %s (%+v)", tc.name, tc.req)
			}
		})
	}
}

func TestTemplates(t *testing.T) {
	want := map[string]Profile{
		"micro":             {VCPU: 1, MemoryMB: 1024, DiskGB: 8},
		"small":             {VCPU: 2, MemoryMB: 2048, DiskGB: 16},
		"medium":            {VCPU: 2, MemoryMB: 4096, DiskGB: 30},
		"database":          {VCPU: 2, MemoryMB: 4096, DiskGB: 30},
		"network-appliance": {VCPU: 1, MemoryMB: 1024, DiskGB: 8},
	}
	for name, w := range want {
		p, err := ValidateProfile(name)
		if err != nil {
			t.Fatalf("profile %s: %v", name, err)
		}
		if p.VCPU != w.VCPU || p.MemoryMB != w.MemoryMB || p.DiskGB != w.DiskGB {
			t.Fatalf("profile %s = %+v, want %+v", name, p, w)
		}
	}
	if _, err := ValidateProfile("huge"); err == nil {
		t.Fatal("unknown profile must be rejected")
	}
	if err := ValidateBounds(0, 512, 8); err == nil {
		t.Fatal("0 vCPU must be rejected")
	}
	inv := Inventory{CPUCount: 2, MemoryTotalMB: 2048, DiskFreeGB: 10}
	// small needs 16 GiB disk, only 10 free.
	if err := CheckCapacity(Profiles["small"], inv, 0, 0, 0); err == nil {
		t.Fatal("capacity check must reject when disk does not fit")
	}
	if err := CheckCapacity(Profiles["micro"], Inventory{CPUCount: 4, MemoryTotalMB: 8192, DiskFreeGB: 100}, 0, 0, 0); err != nil {
		t.Fatalf("micro should fit: %v", err)
	}
}

func TestCloudInit(t *testing.T) {
	yaml, err := GenerateUserData(CloudInit{
		Hostname: "web-1", Username: "cloudforge",
		SSHKey:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl user@example.com",
		Packages: []string{"nginx", "curl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yaml, `hostname: "web-1"`) || !strings.Contains(yaml, "ssh-ed25519") {
		t.Fatalf("unexpected user-data:\n%s", yaml)
	}
	// Injection attempts must fail, not render.
	bad := []CloudInit{
		{Hostname: "web-1\n  evil: true", Username: "cloudforge"},
		{Hostname: "web-1", Username: "root; rm -rf /"},
		{Hostname: "web-1", Username: "cloudforge", SSHKey: "ssh-rsa AAAAB3; id"},
		{Hostname: "web-1", Username: "cloudforge", Packages: []string{"nginx;id"}},
	}
	for i, ci := range bad {
		if _, err := GenerateUserData(ci); err == nil {
			t.Fatalf("cloud-init case %d must be rejected: %+v", i, ci)
		}
	}
}

func TestInventoryParsingFromFixtures(t *testing.T) {
	cpuinfo := "processor\t: 0\nmodel name\t: Intel(R) Xeon(R) CPU\nprocessor\t: 1\nmodel name\t: Intel(R) Xeon(R) CPU\n"
	n, model, err := ParseCPUInfo(strings.NewReader(cpuinfo))
	if err != nil || n != 2 || !strings.Contains(model, "Xeon") {
		t.Fatalf("cpuinfo: n=%d model=%q err=%v", n, model, err)
	}
	meminfo := "MemTotal:        8043212 kB\nMemFree:          123456 kB\nMemAvailable:    4096000 kB\n"
	total, avail, err := ParseMemInfo(strings.NewReader(meminfo))
	if err != nil || total != 8043212/1024 || avail != 4000 {
		t.Fatalf("meminfo: total=%d avail=%d err=%v", total, avail, err)
	}
	l1, l5, l15, err := ParseLoadAvg(strings.NewReader("0.42 0.31 0.25 1/123 4567\n"))
	if err != nil || l1 != 0.42 || l5 != 0.31 || l15 != 0.25 {
		t.Fatalf("loadavg: %v %v %v err=%v", l1, l5, l15, err)
	}
	if _, _, err := ParseCPUInfo(strings.NewReader("")); err == nil {
		t.Fatal("empty cpuinfo must fail")
	}
	if _, _, err := ParseMemInfo(strings.NewReader("MemFree: 1 kB\n")); err == nil {
		t.Fatal("meminfo without MemTotal must fail")
	}
	// DiskUsage on a real directory.
	if totalGB, freeGB, err := DiskUsage(t.TempDir()); err != nil || totalGB <= 0 || freeGB <= 0 {
		t.Fatalf("DiskUsage: total=%v free=%v err=%v", totalGB, freeGB, err)
	}
	// Full collection from fixture files.
	dir := t.TempDir()
	write := func(name, content string) string {
		p := dir + "/" + name
		if err := writeFile(p, content); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cpu := write("cpuinfo", cpuinfo)
	mem := write("meminfo", meminfo)
	load := write("loadavg", "0.10 0.20 0.30 1/2 3\n")
	inv, err := CollectInventoryFrom(cpu, mem, load, dir)
	if err != nil {
		t.Fatal(err)
	}
	if inv.CPUCount != 2 || inv.MemoryTotalMB != 8043212/1024 || inv.Load5 != 0.20 || inv.DiskFreeGB <= 0 {
		t.Fatalf("inventory from fixtures: %+v", inv)
	}
}

func TestSimulatedLifecycle(t *testing.T) {
	ctx := context.Background()
	a := NewSimulatedHostAgent()
	a.TotalCPU = 4
	a.TotalMemoryMB = 8192
	a.TotalDiskGB = 100

	// Container: create -> start -> stop -> remove; double-remove idempotent.
	res, err := a.RunOp(ctx, OpRequest{Op: OpCreateContainer, LabID: "lab-1", OwnerID: "u1", Name: "web-1", Image: "nginx:1.27"})
	if err != nil || res.State != "created" || res.ID == "" {
		t.Fatalf("create container: %+v err=%v", res, err)
	}
	cid := res.ID
	res, err = a.RunOp(ctx, OpRequest{Op: OpStartContainer, LabID: "lab-1", ID: cid})
	if err != nil || res.State != "running" {
		t.Fatalf("start container: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpStopContainer, LabID: "lab-1", ID: cid})
	if err != nil || res.State != "stopped" {
		t.Fatalf("stop container: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpRemoveContainer, LabID: "lab-1", ID: cid})
	if err != nil || res.State != "removed" {
		t.Fatalf("remove container: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpRemoveContainer, LabID: "lab-1", ID: cid})
	if err != nil || res.State != "removed" {
		t.Fatalf("double remove must be idempotent: %+v err=%v", res, err)
	}

	// VM: create -> start -> reboot -> stop -> delete; double-delete idempotent.
	res, err = a.RunOp(ctx, OpRequest{Op: OpCreateVM, LabID: "lab-1", OwnerID: "u1", Name: "vm-1", Profile: "small"})
	if err != nil || res.ID == "" {
		t.Fatalf("create VM: %+v err=%v", res, err)
	}
	vid := res.ID
	res, err = a.RunOp(ctx, OpRequest{Op: OpStartVM, LabID: "lab-1", ID: vid})
	if err != nil || res.State != "running" {
		t.Fatalf("start VM: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpRebootVM, LabID: "lab-1", ID: vid})
	if err != nil || res.State != "running" {
		t.Fatalf("reboot VM: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpStopVM, LabID: "lab-1", ID: vid})
	if err != nil || res.State != "stopped" {
		t.Fatalf("stop VM: %+v err=%v", res, err)
	}
	if _, err := a.RunOp(ctx, OpRequest{Op: OpRebootVM, LabID: "lab-1", ID: vid}); err == nil {
		t.Fatal("reboot of a stopped VM must fail")
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpDeleteVM, LabID: "lab-1", ID: vid})
	if err != nil || res.State != "deleted" {
		t.Fatalf("delete VM: %+v err=%v", res, err)
	}
	res, err = a.RunOp(ctx, OpRequest{Op: OpDeleteVM, LabID: "lab-1", ID: vid})
	if err != nil || res.State != "deleted" {
		t.Fatalf("double delete must be idempotent: %+v err=%v", res, err)
	}

	// Capacity: a tiny host rejects a second medium VM.
	a.TotalCPU = 2
	a.TotalMemoryMB = 4096
	if _, err := a.RunOp(ctx, OpRequest{Op: OpCreateVM, LabID: "lab-2", Name: "vm-a", Profile: "medium"}); err != nil {
		t.Fatalf("first medium VM should fit: %v", err)
	}
	if _, err := a.RunOp(ctx, OpRequest{Op: OpCreateVM, LabID: "lab-2", Name: "vm-b", Profile: "medium"}); err == nil {
		t.Fatal("second medium VM must exceed capacity")
	}
}

func TestSimulatedCleanupAndUsage(t *testing.T) {
	ctx := context.Background()
	a := NewSimulatedHostAgent()
	a.TotalCPU = 8
	a.TotalMemoryMB = 16384
	a.TotalDiskGB = 200
	for _, req := range []OpRequest{
		{Op: OpCreateContainer, LabID: "lab-9", Name: "c1", Image: "nginx"},
		{Op: OpCreateContainer, LabID: "lab-9", Name: "c2", Image: "redis"},
		{Op: OpCreateVM, LabID: "lab-9", Name: "v1", Profile: "micro"},
		{Op: OpCreateVM, LabID: "lab-other", Name: "v2", Profile: "micro"},
	} {
		if _, err := a.RunOp(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.RunOp(ctx, OpRequest{Op: OpStartContainer, LabID: "lab-9", Name: "c1"}); err != nil {
		t.Fatal(err)
	}
	usage, err := a.Usage(ctx, "lab-9")
	if err != nil || len(usage) != 3 {
		t.Fatalf("usage for lab-9: %v err=%v", usage, err)
	}
	running := 0
	for _, u := range usage {
		if u.State == "running" {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("expected 1 running resource, got %d", running)
	}
	if err := a.CleanupLab(ctx, "lab-9"); err != nil {
		t.Fatal(err)
	}
	usage, _ = a.Usage(ctx, "lab-9")
	if len(usage) != 0 {
		t.Fatalf("cleanup must remove all lab-9 resources, got %v", usage)
	}
	usage, _ = a.Usage(ctx, "lab-other")
	if len(usage) != 1 {
		t.Fatalf("cleanup must not touch other labs, got %v", usage)
	}
	// Cleanup is idempotent.
	if err := a.CleanupLab(ctx, "lab-9"); err != nil {
		t.Fatalf("second cleanup must succeed: %v", err)
	}
	// ExecInLab is validated but never runs a command.
	res, err := a.RunOp(ctx, OpRequest{Op: OpExecInLab, LabID: "lab-9", Command: []string{"ls", "workdir"}})
	if err != nil || !strings.Contains(res.Message, "simulated") {
		t.Fatalf("simulated exec: %+v err=%v", res, err)
	}
}

func TestInventoryEndpoint(t *testing.T) {
	ctx := context.Background()
	a := NewSimulatedHostAgent()
	inv, err := a.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if inv.CPUCount <= 0 || inv.MemoryTotalMB <= 0 {
		t.Fatalf("inventory looks empty: %+v", inv)
	}
	res, err := a.RunOp(ctx, OpRequest{Op: OpHostInventory})
	if err != nil || res.Inventory == nil || res.Inventory.CPUCount <= 0 {
		t.Fatalf("HostInventory op: %+v err=%v", res, err)
	}
}

func TestDockerUnavailableInSandbox(t *testing.T) {
	ctx := context.Background()
	a := NewDockerHostAgent("/nonexistent/docker.sock")
	if a.Mode() != "docker" {
		t.Fatalf("mode = %q", a.Mode())
	}
	_, err := a.RunOp(ctx, OpRequest{Op: OpListContainers, LabID: "lab-1"})
	if err == nil || !IsUnavailable(err) {
		t.Fatalf("docker without a socket must return Unavailable, got %v", err)
	}
	_, err = a.RunOp(ctx, OpRequest{Op: OpCreateVM, LabID: "lab-1", Name: "vm-1", Profile: "small"})
	if err == nil || !IsUnavailable(err) {
		t.Fatalf("docker VM op must return Unavailable, got %v", err)
	}
	// Host inventory does not need Docker and must still work.
	if _, err := a.Inventory(ctx); err != nil {
		t.Fatalf("docker-agent inventory must work without Docker: %v", err)
	}
}

func TestLibvirtUnavailableInSandbox(t *testing.T) {
	ctx := context.Background()
	a := NewLibvirtHostAgent("/nonexistent/libvirt-sock", "qemu:///system")
	if a.Mode() != "libvirt" {
		t.Fatalf("mode = %q", a.Mode())
	}
	_, err := a.RunOp(ctx, OpRequest{Op: OpCreateVM, LabID: "lab-1", Name: "vm-1", Profile: "small"})
	if err == nil || !IsUnavailable(err) {
		t.Fatalf("libvirt without a socket must return Unavailable, got %v", err)
	}
	// No record may be created when the hypervisor is unavailable.
	a.store.mu.Lock()
	n := len(a.store.vms)
	a.store.mu.Unlock()
	if n != 0 {
		t.Fatalf("failed libvirt create must not leave a VM record (got %d)", n)
	}
	_, err = a.RunOp(ctx, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "c1", Image: "nginx"})
	if err == nil || !IsUnavailable(err) {
		t.Fatalf("libvirt container op must return Unavailable, got %v", err)
	}
}

func TestLibvirtDomainXMLIsTyped(t *testing.T) {
	v := &VM{Name: "vm-1", CPU: 2, MemoryMB: 2048}
	out, err := renderDomainXML(v, "/var/lib/cloudforge/labs/lab-1/vm-1-disk.qcow2")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "<name>vm-1</name>") || !strings.Contains(s, "cloudforge-br0") || !strings.Contains(s, "vm-1-disk.qcow2") {
		t.Fatalf("unexpected domain XML:\n%s", s)
	}
	if !strings.Contains(s, "hvm") {
		t.Fatalf("domain XML should declare hvm:\n%s", s)
	}
}

func TestNewAgentModes(t *testing.T) {
	for _, mode := range []string{"simulated", "docker", "libvirt", "auto", ""} {
		a, err := NewAgent(mode)
		if err != nil || a == nil {
			t.Fatalf("NewAgent(%q): %v", mode, err)
		}
	}
	if _, err := NewAgent("bogus"); err == nil {
		t.Fatal("unknown mode must be rejected")
	}
}

// --- server + client ------------------------------------------------------

func testHostAgentServer(t *testing.T, token string) (*httptest.Server, *Client) {
	t.Helper()
	agent := NewSimulatedHostAgent()
	agent.TotalCPU = 8
	agent.TotalMemoryMB = 16384
	agent.TotalDiskGB = 200
	srv := NewServer(agent, token, nil)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, NewClient(ts.URL, token)
}

func TestClientAuthFailure(t *testing.T) {
	ctx := context.Background()
	ts, _ := testHostAgentServer(t, "secret-token")
	_ = ts
	bad := NewClient(ts.URL, "wrong-token")
	_, err := bad.Do(ctx, OpRequest{Op: OpListContainers, LabID: "lab-1"})
	if !isUnauthorized(err) {
		t.Fatalf("wrong token must be unauthorized, got %v", err)
	}
	if _, err := bad.Inventory(ctx); !isUnauthorized(err) {
		t.Fatalf("wrong token on inventory must be unauthorized, got %v", err)
	}
	// No token at all.
	none := NewClient(ts.URL, "")
	if _, err := none.Do(ctx, OpRequest{Op: OpListContainers, LabID: "lab-1"}); !isUnauthorized(err) {
		t.Fatalf("missing token must be unauthorized, got %v", err)
	}
}

func isUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestClientServerRoundTrip(t *testing.T) {
	ctx := context.Background()
	_, c := testHostAgentServer(t, "secret-token")
	res, err := c.Do(ctx, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "web-1", Image: "nginx:1.27", IdempotencyKey: "k1"})
	if err != nil || res.ID == "" {
		t.Fatalf("client create: %+v err=%v", res, err)
	}
	// Idempotency replay through the server returns the same id.
	res2, err := c.Do(ctx, OpRequest{Op: OpCreateContainer, LabID: "lab-1", Name: "web-1", Image: "nginx:1.27", IdempotencyKey: "k1"})
	if err != nil || res2.ID != res.ID || !res2.Replayed {
		t.Fatalf("idempotent replay: %+v err=%v (first id %s)", res2, err, res.ID)
	}
	inv, err := c.Inventory(ctx)
	if err != nil || inv.CPUCount <= 0 {
		t.Fatalf("client inventory: %+v err=%v", inv, err)
	}
	if err := c.Health(ctx); err != nil {
		t.Fatalf("client health: %v", err)
	}
	if err := c.CleanupLab(ctx, "lab-1", "u1"); err != nil {
		t.Fatalf("client cleanup: %v", err)
	}
	list, err := c.Do(ctx, OpRequest{Op: OpListContainers, LabID: "lab-1"})
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("after cleanup list must be empty: %+v err=%v", list, err)
	}
}

func TestScopedTokenEnforcesLabOwnership(t *testing.T) {
	ctx := context.Background()
	ts, _ := testHostAgentServer(t, "secret-token:lab:lab-1")
	scoped := NewClient(ts.URL, "secret-token:lab:lab-1")
	if _, err := scoped.Do(ctx, OpRequest{Op: OpListContainers, LabID: "lab-1"}); err != nil {
		t.Fatalf("scoped token on its own lab must work: %v", err)
	}
	_, err := scoped.Do(ctx, OpRequest{Op: OpListContainers, LabID: "lab-2"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("scoped token on another lab must be forbidden (403), got %v", err)
	}
}

func TestServerRejectsValidationBeforeExecution(t *testing.T) {
	ts, _ := testHostAgentServer(t, "secret-token")
	// Bypass the client (which validates locally) to prove the server
	// itself rejects an invalid op with 400 before executing anything.
	body := strings.NewReader(`{"op":"ExecInLab","lab_id":"lab-1","command":["rm","-rf","/"]}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/ops", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("server must reject invalid op with 400, got %d", resp.StatusCode)
	}
}

func TestHealthzUnauthenticated(t *testing.T) {
	ts, _ := testHostAgentServer(t, "secret-token")
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}
