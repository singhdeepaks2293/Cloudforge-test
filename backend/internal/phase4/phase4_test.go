// Phase 4 pure-logic tests: ECR naming/digests, the local-disk
// registry backend, the ECS task runner, buildspec parsing, and the
// CloudFormation template engine. HTTP handlers are covered in
// internal/httpapi/phase4_test.go.
package phase4

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestECRNaming(t *testing.T) {
	if err := ValidateECRRepoName("my-app/api"); err != nil {
		t.Errorf("valid repo name rejected: %v", err)
	}
	if err := ValidateECRRepoName("MyApp"); err == nil {
		t.Error("uppercase repo name accepted")
	}
	if err := ValidateECRRepoName(""); err == nil {
		t.Error("empty repo name accepted")
	}
	if err := ValidateImageTag("v1.2.3"); err != nil {
		t.Errorf("valid tag rejected: %v", err)
	}
	if err := ValidateImageTag(""); err == nil {
		t.Error("empty tag accepted")
	}
	repo, tag, digest := ParseImageRef("my-app/api:1.0")
	if repo != "my-app/api" || tag != "1.0" || digest != "" {
		t.Errorf("ParseImageRef = %q %q %q", repo, tag, digest)
	}
	repo, tag, digest = ParseImageRef("my-app/api@sha256:abc")
	if repo != "my-app/api" || digest != "sha256:abc" {
		t.Errorf("ParseImageRef digest form = %q %q %q", repo, tag, digest)
	}
	if got := Digest([]byte("hello")); !strings.HasPrefix(got, "sha256:") || len(got) != 7+64 {
		t.Errorf("Digest = %q", got)
	}
	if _, err := DigestHex("sha256:zz"); err == nil {
		t.Error("bad digest hex accepted")
	}
}

func TestLocalDiskRegistryRoundTrip(t *testing.T) {
	ctx := context.Background()
	workdir := t.TempDir()
	b := NewLocalDiskBackend(workdir)
	repo := "demo/app"
	// Blob before manifest: manifest-first is rejected.
	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config":        map[string]any{"digest": "sha256:" + strings.Repeat("a", 64), "size": 5},
		"layers":        []any{map[string]any{"digest": "sha256:" + strings.Repeat("b", 64), "size": 5}},
	}
	raw, _ := json.Marshal(manifest)
	if _, err := b.PutManifest(ctx, repo, "v1", raw); err == nil {
		t.Fatal("manifest with missing blobs was accepted")
	}
	// Push the blobs (content must hash to the claimed digest).
	configBytes := []byte("cfg!!")
	layerBytes := []byte("layer")
	configDigest := Digest(configBytes)
	layerDigest := Digest(layerBytes)
	manifest["config"] = map[string]any{"digest": configDigest, "size": len(configBytes)}
	manifest["layers"] = []any{map[string]any{"digest": layerDigest, "size": len(layerBytes)}}
	raw, _ = json.Marshal(manifest)
	if err := b.PutBlob(ctx, repo, configDigest, strings.NewReader(string(configBytes))); err != nil {
		t.Fatalf("put config blob: %v", err)
	}
	if err := b.PutBlob(ctx, repo, layerDigest, strings.NewReader(string(layerBytes))); err != nil {
		t.Fatalf("put layer blob: %v", err)
	}
	// Digest mismatch is rejected.
	if err := b.PutBlob(ctx, repo, layerDigest, strings.NewReader("different")); err == nil {
		t.Error("blob with wrong content for the claimed digest was accepted")
	}
	digest, err := b.PutManifest(ctx, repo, "v1", raw)
	if err != nil {
		t.Fatalf("put manifest: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("manifest digest = %q", digest)
	}
	got, gotDigest, err := b.GetManifest(ctx, repo, "v1")
	if err != nil || gotDigest != digest || string(got) != string(raw) {
		t.Fatalf("get manifest: %v %q", err, gotDigest)
	}
	catalog, err := b.Catalog(ctx)
	if err != nil || len(catalog) != 1 || catalog[0] != repo {
		t.Fatalf("catalog = %v %v", catalog, err)
	}
	tags, err := b.Tags(ctx, repo)
	if err != nil || len(tags) != 1 || tags[0] != "v1" {
		t.Fatalf("tags = %v %v", tags, err)
	}
	rc, size, err := b.GetBlob(ctx, layerDigest)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	defer rc.Close()
	buf := make([]byte, 64)
	n, _ := rc.Read(buf)
	if size != int64(len(layerBytes)) || string(buf[:n]) != string(layerBytes) {
		t.Fatalf("blob bytes wrong: size %d, %q", size, buf[:n])
	}
	if _, _, err := b.GetManifest(ctx, repo, "nope"); err == nil {
		t.Error("missing manifest not reported")
	}
	if err := b.DeleteManifest(ctx, repo, "v1"); err != nil {
		t.Fatalf("delete manifest: %v", err)
	}
	if _, _, err := b.GetManifest(ctx, repo, "v1"); err == nil {
		t.Error("deleted manifest still readable")
	}
}

// p4Workdir returns a fresh workdir that a uid-65534 child can
// traverse: os.MkdirTemp under /tmp (1777) + chmod 0755. t.TempDir
// nests under go-test's own 0700 parent dirs, which the nobody
// child cannot traverse — the same reason Phase 3 Lambda chmods
// its temp dir. Production workdirs are 0755 by the operator.
func p4Workdir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cf-phase4-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTaskRunnerEchoAndSleep(t *testing.T) {
	r := NewTaskRunner(p4Workdir(t))
	done := make(chan TaskCompletion, 4)
	r.OnStop = func(c TaskCompletion) { done <- c }

	// Echo task: runs to completion with exit 0.
	if _, err := r.Start("echo-1", []string{"echo", "hello-task"}, nil, 30, 128); err != nil {
		t.Fatalf("start echo task: %v", err)
	}
	select {
	case c := <-done:
		if c.TaskID != "echo-1" || c.ExitCode != 0 {
			t.Fatalf("echo completion = %+v", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("echo task did not complete")
	}
	logs, err := r.ReadLog("echo-1")
	if err != nil || !strings.Contains(logs, "hello-task") {
		t.Fatalf("task log = %q, err %v", logs, err)
	}
	if r.IsRunning("echo-1") {
		t.Error("finished task still reports running")
	}

	// Sleep task: running, then stopped by Stop().
	if _, err := r.Start("sleep-1", []string{"sleep", "60"}, nil, 60, 128); err != nil {
		t.Fatalf("start sleep task: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if !r.IsRunning("sleep-1") {
		t.Fatal("sleep task does not report running")
	}
	if err := r.Stop("sleep-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case c := <-done:
		if c.TaskID != "sleep-1" {
			t.Fatalf("unexpected completion %+v", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stopped task did not report completion")
	}

	// Failing task: exit code is real.
	if _, err := r.Start("fail-1", []string{"sh", "-c", "exit 3"}, nil, 30, 128); err != nil {
		t.Fatalf("start fail task: %v", err)
	}
	select {
	case c := <-done:
		if c.ExitCode != 3 {
			t.Fatalf("fail completion = %+v, want exit 3", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("failing task did not complete")
	}
}

func TestBuildspecParse(t *testing.T) {
	raw := []byte("version: 0.2\nphases:\n  build:\n    commands:\n      - echo hi\n      - make test\nartifacts:\n  files:\n    - app.jar\n")
	bs, err := ParseBuildspec(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(bs.Phases["build"].Commands) != 2 || bs.Artifacts.Files[0] != "app.jar" {
		t.Fatalf("parsed buildspec = %+v", bs)
	}
	if _, err := ParseBuildspec([]byte("version: 0.2\n")); err == nil {
		t.Error("buildspec with no phases accepted")
	}
	if _, err := ParseBuildspec([]byte("phases:\n  deploy:\n    commands: [x]\n")); err == nil {
		t.Error("buildspec with unknown phase accepted")
	}
	if _, err := ParseBuildspec([]byte("phases: [unclosed")); err == nil {
		t.Error("invalid YAML accepted")
	}
}

func TestCFNTemplate(t *testing.T) {
	jsonTpl := `{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Description": "demo",
		"Parameters": {"QueueName": {"Type": "String", "Default": "orders"}},
		"Resources": {
			"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": {"Ref": "QueueName"}}},
			"Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "events"}},
			"Box": {"Type": "AWS::EC2::Instance", "Properties": {}}
		}
	}`
	tpl, err := ParseCFNTemplate([]byte(jsonTpl))
	if err != nil {
		t.Fatalf("parse JSON template: %v", err)
	}
	if len(tpl.Resources) != 3 || tpl.Parameters["QueueName"].Default != "orders" {
		t.Fatalf("parsed template = %+v", tpl)
	}
	problems := ValidateCFNTemplate(tpl)
	if len(problems) != 1 || !strings.Contains(problems[0], "AWS::EC2::Instance") {
		t.Fatalf("validate problems = %v, want exactly the EC2 type", problems)
	}
	yamlTpl := "Resources:\n  Q:\n    Type: AWS::SQS::Queue\n    Properties:\n      QueueName: q1\n"
	tpl2, err := ParseCFNTemplate([]byte(yamlTpl))
	if err != nil || len(tpl2.Resources) != 1 {
		t.Fatalf("parse YAML template: %v %+v", err, tpl2)
	}
	if _, err := ParseCFNTemplate([]byte("")); err == nil {
		t.Error("empty template accepted")
	}
	if _, err := ParseCFNTemplate([]byte("{not json")); err == nil {
		t.Error("garbage template accepted")
	}

	// Planning: fresh stack creates the supported resources and
	// reports the unsupported one, never silently.
	changes := PlanCFNChanges(tpl, map[string]map[string]any{})
	byID := map[string]CFNChange{}
	for _, c := range changes {
		byID[c.LogicalID] = c
	}
	if byID["Queue"].Action != "CREATE" || byID["Topic"].Action != "CREATE" {
		t.Fatalf("plan = %+v", changes)
	}
	if byID["Box"].Action != "UNSUPPORTED" {
		t.Fatalf("unsupported resource plan = %+v", byID["Box"])
	}
	// Update: existing resource -> UPDATE; removed resource -> DELETE.
	current := map[string]map[string]any{
		"Queue": {"type": "AWS::SQS::Queue", "physical_id": "q-1"},
		"Gone":  {"type": "AWS::SNS::Topic", "physical_id": "t-1"},
	}
	changes = PlanCFNChanges(tpl, current)
	byID = map[string]CFNChange{}
	for _, c := range changes {
		byID[c.LogicalID] = c
	}
	if byID["Queue"].Action != "UPDATE" {
		t.Fatalf("update plan = %+v", byID["Queue"])
	}
	if byID["Gone"].Action != "DELETE" {
		t.Fatalf("delete plan = %+v", byID["Gone"])
	}

	// Ref / Join / Sub resolution.
	physical := map[string]string{"Queue": "q-123"}
	params := map[string]any{"Env": "prod"}
	v := CFNResolveValue(map[string]any{"Ref": "Queue"}, params, physical)
	if v != "q-123" {
		t.Errorf("Ref resolve = %v", v)
	}
	v = CFNResolveValue(map[string]any{"Fn::Join": []any{"-", []any{"a", map[string]any{"Ref": "Env"}}}}, params, physical)
	if v != "a-prod" {
		t.Errorf("Join resolve = %v", v)
	}
	v = CFNResolveValue(map[string]any{"Fn::Sub": "queue-${Queue}-done"}, params, physical)
	if v != "queue-q-123-done" {
		t.Errorf("Sub resolve = %v", v)
	}
}

func TestRunShellRealCommands(t *testing.T) {
	dir := p4Workdir(t)
	// Builds write into their workdir, so it must be writable by
	// the uid-65534 child (same chown the build runner does).
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, 65534, 65534); err != nil {
			t.Fatal(err)
		}
	}
	var sb strings.Builder
	res := RunShell(context.Background(), dir, map[string]string{"WHO": "world"}, 30, 256, &sb, []string{"echo hello $WHO", "echo second > out.txt"})
	if res.ExitCode != 0 {
		t.Fatalf("RunShell = %+v, log %q", res, sb.String())
	}
	if !strings.Contains(sb.String(), "hello world") {
		t.Fatalf("log = %q", sb.String())
	}
	if data, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || strings.TrimSpace(string(data)) != "second" {
		t.Fatalf("command side effect missing: %v %q", err, data)
	}
	res = RunShell(context.Background(), dir, nil, 30, 256, &sb, []string{"false"})
	if res.ExitCode == 0 {
		t.Fatalf("failing command reported success: %+v", res)
	}
}

func TestEKSDecision(t *testing.T) {
	if EKSDecision != "UNSUPPORTED" {
		t.Fatalf("EKS decision = %q, want UNSUPPORTED", EKSDecision)
	}
	if len(K8sLearningPath) == 0 {
		t.Fatal("empty Kubernetes learning path")
	}
	for _, c := range K8sLearningPath {
		if c.Concept == "" || c.CloudForge == "" || c.Route == "" || c.TryIt == "" {
			t.Fatalf("incomplete learning-path row: %+v", c)
		}
	}
}
