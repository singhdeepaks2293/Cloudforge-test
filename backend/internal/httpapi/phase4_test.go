// Phase 4 HTTP tests: ECR (catalogue + /v2/ registry API + scan +
// lifecycle), ECS/Fargate (clusters, task definitions, services
// with desired-count reconciliation and rolling deployments, tasks
// with real process status and logs), CodeBuild (real builds from a
// local source dir), CodePipeline (source -> build -> deploy, plus
// the honest Terraform-deploy failure), CodeDeploy (deploy +
// rollback), CloudFormation (stacks, change sets, rollback on
// failure), and the EKS decision endpoint. Everything runs against
// the in-memory Phase 3 store with a traversable temp workdir (see
// p4TestWorkdir: the sandboxed children run as uid 65534).
package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/phase4"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func p4TestWorkdir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cf-phase4-api-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func p4TestServer(t *testing.T) (*Server, *httptest.Server, string, string) {
	t.Helper()
	workdir := p4TestWorkdir(t)
	srv := NewServerWithDeps(Deps{
		P3:                store.NewMemoryPhase3Store(),
		P3DatabaseURL:     "",
		P3RedisAddr:       "localhost:1",
		P3DynamoEndpoint:  "http://localhost:1",
		P3WorkdirRoot:     workdir,
		P4BuildSourceRoot: workdir,
	})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts, adminToken(t, srv, ts), workdir
}

// p4Wait polls a GET until data[field] == want (or the deadline).
func p4Wait(t *testing.T, ts *httptest.Server, token, url, field, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		code, out := p3Call(t, "GET", url, token, nil)
		if code != 200 {
			t.Fatalf("poll %s: %d %v", url, code, out)
		}
		last = out
		if v, _ := out[field].(string); v == want {
			return out
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s == %q at %s; last = %v", field, want, url, last)
	return last
}

func p4Str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func p4List(m map[string]any, key string) []any {
	arr, _ := m[key].([]any)
	return arr
}

// --- ECR ----------------------------------------------------------------------

func p4Manifest(t *testing.T, configDigest, layerDigest string) map[string]any {
	t.Helper()
	return map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config":        map[string]any{"mediaType": "application/vnd.docker.container.image.v1+json", "digest": configDigest, "size": 5},
		"layers":        []any{map[string]any{"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip", "digest": layerDigest, "size": 5}},
	}
}

func p4PushImage(t *testing.T, ts *httptest.Server, token, repoID, tag string, manifest map[string]any, blobs map[string]string) (int, map[string]any) {
	t.Helper()
	blobsArr := []any{}
	for digest, content := range blobs {
		blobsArr = append(blobsArr, map[string]any{"digest": digest, "content_base64": base64.StdEncoding.EncodeToString([]byte(content))})
	}
	return p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID+"/images", token, map[string]any{
		"tag": tag, "manifest": manifest, "blobs": blobsArr,
	})
}

func TestPhase4ECR(t *testing.T) {
	_, ts, token, _ := p4TestServer(t)
	// Bad name -> 400, nothing stored.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories", token, map[string]any{"name": "Bad_Name"}); code != 400 {
		t.Fatalf("bad repo name: got %d, want 400", code)
	}
	code, repo := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories", token, map[string]any{"name": "demo/app"})
	if code != 201 {
		t.Fatalf("create repo: %d %v", code, repo)
	}
	repoID := p4Str(repo, "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories", token, map[string]any{"name": "demo/app"}); code != 409 {
		t.Fatalf("duplicate repo: got %d, want 409", code)
	}
	// Manifest before blobs -> 400 (BLOB_UNKNOWN).
	cfg, layer := "cfg!!", "layer"
	cfgDigest, layerDigest := phase4.Digest([]byte(cfg)), phase4.Digest([]byte(layer))
	if code, out := p4PushImage(t, ts, token, repoID, "v1", p4Manifest(t, cfgDigest, layerDigest), nil); code != 400 {
		t.Fatalf("manifest-first push: got %d %v, want 400", code, out)
	}
	// Real push with blobs.
	code, img := p4PushImage(t, ts, token, repoID, "v1", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: cfg, layerDigest: layer})
	if code != 201 {
		t.Fatalf("push image: %d %v", code, img)
	}
	imgID := p4Str(img, "id")
	if p4Str(img, "digest") == "" || p4Str(img, "image_uri") != "localhost:8081/demo/app:v1" {
		t.Fatalf("pushed image = %v", img)
	}
	// Digest mismatch in a blob -> 400.
	if code, _ := p4PushImage(t, ts, token, repoID, "v2", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: "tampered"}); code != 400 {
		t.Fatalf("tampered blob: got %d, want 400", code)
	}
	// Convenience push shape: raw layers as base64, no manifest.
	// (This is the shape the console and the live walk use; the
	// handler synthesises the manifest server-side. Regression
	// test: the live walk caught this returning 400 "unknown field
	// layers" because the handler only accepted manifest+blobs.)
	code, imgLayers := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID+"/images", token, map[string]any{
		"tag": "layers", "layers": []any{base64.StdEncoding.EncodeToString([]byte("layer-one"))},
	})
	if code != 201 || p4Str(imgLayers, "digest") == "" {
		t.Fatalf("push via layers shape: %d %v", code, imgLayers)
	}
	if code, bad := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID+"/images", token, map[string]any{
		"tag": "badb64", "layers": []any{"!!!not-base64!!!"},
	}); code != 400 {
		t.Fatalf("push with invalid base64 layer: got %d %v, want 400", code, bad)
	}
	if code, bad := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID+"/images", token, map[string]any{
		"tag": "nomanifest",
	}); code != 400 {
		t.Fatalf("push with neither manifest nor layers: got %d %v, want 400", code, bad)
	}
	// List + get (v1 from the manifest push, "layers" from the
	// convenience push).
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID+"/images", token, nil)
	if code != 200 || len(p4List(out, "images")) != 2 {
		t.Fatalf("list images: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/images/"+imgID, token, nil)
	if code != 200 || p4Str(out, "tag") != "v1" {
		t.Fatalf("get image: %d %v", code, out)
	}
	// Integrity scan: every layer re-read and digest-verified.
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/images/"+imgID+"/scan", token, nil)
	if code != 200 || p4Str(out, "scan_status") != "SUCCEEDED" || out["layers_checked"] != float64(1) {
		t.Fatalf("scan: %d %v", code, out)
	}
	if !strings.Contains(p4Str(out, "note"), "NOT a vulnerability") {
		t.Fatalf("scan must say it is not a vulnerability scan: %v", out)
	}
	// The /v2/ registry API subset: catalog, tags, manifest, blob.
	raw := func(method, url string, body io.Reader) (int, []byte, http.Header) {
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data, resp.Header
	}
	code2, data, hdr := raw("GET", ts.URL+"/v2/", nil)
	if code2 != 200 || hdr.Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Fatalf("v2 root: %d %s headers %v", code2, data, hdr)
	}
	code2, data, _ = raw("GET", ts.URL+"/v2/_catalog", nil)
	if code2 != 200 || !strings.Contains(string(data), "demo/app") {
		t.Fatalf("catalog: %d %s", code2, data)
	}
	code2, data, _ = raw("GET", ts.URL+"/v2/demo/app/tags/list", nil)
	if code2 != 200 || !strings.Contains(string(data), `"v1"`) {
		t.Fatalf("tags: %d %s", code2, data)
	}
	code2, data, hdr = raw("GET", ts.URL+"/v2/demo/app/manifests/v1", nil)
	if code2 != 200 || !strings.HasPrefix(hdr.Get("Docker-Content-Digest"), "sha256:") {
		t.Fatalf("manifest get: %d %s", code2, data)
	}
	code2, data, _ = raw("GET", ts.URL+"/v2/demo/app/blobs/"+layerDigest, nil)
	if code2 != 200 || string(data) != layer {
		t.Fatalf("blob get: %d %q", code2, data)
	}
	code2, _, _ = raw("GET", ts.URL+"/v2/demo/app/manifests/nope", nil)
	if code2 != 404 {
		t.Fatalf("missing manifest: got %d, want 404", code2)
	}
	// Monolithic blob upload via /v2/, then a manifest PUT for v2.
	blob2 := "second-layer"
	blob2Digest := phase4.Digest([]byte(blob2))
	req, _ := http.NewRequest("POST", ts.URL+"/v2/demo/app/blobs/uploads/?digest="+blob2Digest, strings.NewReader(blob2))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("v2 monolithic blob upload: %d", resp.StatusCode)
	}
	manifestBytes, _ := json.Marshal(p4Manifest(t, cfgDigest, blob2Digest))
	req, _ = http.NewRequest("PUT", ts.URL+"/v2/demo/app/manifests/v2", bytes.NewReader(manifestBytes))
	req.Header.Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("v2 manifest put: %d", resp.StatusCode)
	}
	// Lifecycle: keep only the newest 1 tagged image.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories", token, map[string]any{
		"name": "demo/lifecycle", "lifecycle_rules": []any{map[string]any{"type": "keep_last_n_tagged", "count": 1}},
	}); code != 201 {
		t.Fatalf("lifecycle repo: %d", code)
	}
	_, lc := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/repositories", token, nil)
	var lcID string
	for _, raw := range p4List(lc, "repositories") {
		if m, _ := raw.(map[string]any); p4Str(m, "name") == "demo/lifecycle" {
			lcID = p4Str(m, "id")
		}
	}
	if code, out := p4PushImage(t, ts, token, lcID, "a", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: cfg, layerDigest: layer}); code != 201 {
		t.Fatalf("push a: %d %v", code, out)
	}
	// Pushing "b" applies the lifecycle rule immediately: "a" is
	// expired on push, so only "b" remains.
	if code, out := p4PushImage(t, ts, token, lcID, "b", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: cfg, layerDigest: layer}); code != 201 {
		t.Fatalf("push b: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/repositories/"+lcID+"/images", token, nil)
	if code != 200 || len(p4List(out, "images")) != 1 || p4Str(p4List(out, "images")[0].(map[string]any), "tag") != "b" {
		t.Fatalf("lifecycle on push: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories/"+lcID+"/lifecycle", token, nil)
	if code != 200 || out["count"] != float64(0) {
		t.Fatalf("lifecycle evaluate (nothing left to expire): %d %v", code, out)
	}
	// Shorthand rule shape {"keep_last_n_tagged": N} — the shape the
	// console and the live walk send. Regression test: the live walk
	// caught that p4ApplyLifecycle only read {"type": ...,
	// "count": ...}, so the shorthand rule was silently ignored and
	// the old image was never deleted (the repo then refused to
	// delete with 409 because it was still non-empty).
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecr/repositories", token, map[string]any{
		"name": "demo/lifecycle-shorthand", "lifecycle_rules": []any{map[string]any{"keep_last_n_tagged": 1}},
	}); code != 201 {
		t.Fatalf("shorthand lifecycle repo: %d", code)
	}
	_, lc2 := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/repositories", token, nil)
	var lc2ID string
	for _, raw := range p4List(lc2, "repositories") {
		if m, _ := raw.(map[string]any); p4Str(m, "name") == "demo/lifecycle-shorthand" {
			lc2ID = p4Str(m, "id")
		}
	}
	if code, out := p4PushImage(t, ts, token, lc2ID, "x", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: cfg, layerDigest: layer}); code != 201 {
		t.Fatalf("push x: %d %v", code, out)
	}
	if code, out := p4PushImage(t, ts, token, lc2ID, "y", p4Manifest(t, cfgDigest, layerDigest), map[string]string{cfgDigest: cfg, layerDigest: layer}); code != 201 {
		t.Fatalf("push y: %d %v", code, out)
	}
	code, out = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecr/repositories/"+lc2ID+"/images", token, nil)
	if code != 200 || len(p4List(out, "images")) != 1 || p4Str(p4List(out, "images")[0].(map[string]any), "tag") != "y" {
		t.Fatalf("shorthand lifecycle on push: %d %v", code, out)
	}
	// Delete images, then repo (non-empty repo -> 409 first).
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID, token, nil); code != 409 {
		t.Fatalf("delete non-empty repo: got %d, want 409", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecr/images/"+imgID, token, nil); code != 200 {
		t.Fatalf("delete image: %d", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecr/images/"+p4Str(imgLayers, "id"), token, nil); code != 200 {
		t.Fatalf("delete layers image: %d", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecr/repositories/"+repoID, token, nil); code != 200 {
		t.Fatalf("delete repo: %d", code)
	}
}

func p4TaskDef(t *testing.T, ts *httptest.Server, token, family string, command []string) map[string]any {
	t.Helper()
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/task-definitions", token, map[string]any{
		"family": family, "cpu": "256", "memory_mb": 128,
		"container_definitions": []any{map[string]any{
			// Per-container cpu/memory as numbers — the shape the
			// console and the live walk send. Regression test: the
			// live walk caught "unknown field cpu" here because the
			// container struct had no cpu/memory fields.
			"name": "app", "image": "demo/app:v1", "command": command, "cpu": 256, "memory_mb": 128,
			"environment": map[string]any{"GREETING": "hi"}, "essential": true,
		}},
	})
	if code != 201 {
		t.Fatalf("register task def %s: %d %v", family, code, out)
	}
	return out
}

func TestPhase4ECS(t *testing.T) {
	_, ts, token, _ := p4TestServer(t)
	code, cluster := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/clusters", token, map[string]any{"name": "dev"})
	if code != 201 {
		t.Fatalf("create cluster: %d %v", code, cluster)
	}
	clusterID := p4Str(cluster, "id")
	// Task def without a command -> 400 (the runtime needs argv).
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/task-definitions", token, map[string]any{
		"family": "bad", "container_definitions": []any{map[string]any{"name": "app", "image": "x:y"}},
	}); code != 400 {
		t.Fatalf("task def without command: got %d, want 400", code)
	}
	td := p4TaskDef(t, ts, token, "echoer", []string{"echo", "hello-task"})
	tdID := p4Str(td, "id")
	// Run task: real process, exit 0, real logs.
	code, run := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/tasks/run", token, map[string]any{
		"cluster_id": clusterID, "task_definition_id": tdID, "launch_type": "EC2", "count": 1,
	})
	if code != 201 {
		t.Fatalf("run task: %d %v", code, run)
	}
	task := p4List(run, "tasks")[0].(map[string]any)
	taskID := p4Str(task, "id")
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/ecs/tasks/"+taskID, "status", "STOPPED")
	code, got := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks/"+taskID, token, nil)
	if code != 200 || got["exit_code"] != float64(0) {
		t.Fatalf("task result: %d %v", code, got)
	}
	code, logs := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks/"+taskID+"/logs", token, nil)
	if code != 200 || !strings.Contains(p4Str(logs, "logs"), "hello-task") {
		t.Fatalf("task logs: %d %v", code, logs)
	}
	// Failing task: exit code is real, not faked to 0.
	tdFail := p4TaskDef(t, ts, token, "failer", []string{"sh", "-c", "exit 3"})
	code, run = p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/tasks/run", token, map[string]any{
		"cluster_id": clusterID, "task_definition_id": p4Str(tdFail, "id"), "launch_type": "EC2",
	})
	if code != 201 {
		t.Fatalf("run failing task: %d %v", code, run)
	}
	failTask := p4List(run, "tasks")[0].(map[string]any)
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/ecs/tasks/"+p4Str(failTask, "id"), "status", "STOPPED")
	code, got = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks/"+p4Str(failTask, "id"), token, nil)
	if code != 200 || got["exit_code"] != float64(3) {
		t.Fatalf("failing task exit: %d %v", code, got)
	}
	// Fargate run: no cluster, launch type FARGATE.
	code, run = p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/tasks/run", token, map[string]any{
		"task_definition_id": tdID, "launch_type": "FARGATE",
	})
	if code != 201 {
		t.Fatalf("fargate run: %d %v", code, run)
	}
	if p4Str(p4List(run, "tasks")[0].(map[string]any), "launch_type") != "FARGATE" {
		t.Fatalf("fargate task = %v", run)
	}
	// Service with desired count 1: a real task is RUNNING.
	tdSleep := p4TaskDef(t, ts, token, "sleeper", []string{"sleep", "60"})
	code, svc := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/services", token, map[string]any{
		"name": "web", "cluster_id": clusterID, "task_definition_id": p4Str(tdSleep, "id"),
		"desired_count": 1, "launch_type": "EC2",
	})
	if code != 201 {
		t.Fatalf("create service: %d %v", code, svc)
	}
	svcID := p4Str(svc, "id")
	deadline := time.Now().Add(15 * time.Second)
	for {
		code, got = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
		if code == 200 && got["running_count"] == float64(1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("service never reached running_count 1: %d %v", code, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Stopping a service-managed task directly -> 409.
	code, tasks := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks?service_id="+svcID, token, nil)
	if code != 200 || len(p4List(tasks, "tasks")) != 1 {
		t.Fatalf("service tasks: %d %v", code, tasks)
	}
	svcTaskID := p4Str(p4List(tasks, "tasks")[0].(map[string]any), "id")
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/tasks/"+svcTaskID+"/stop", token, nil); code != 409 {
		t.Fatalf("stop service task: got %d, want 409", code)
	}
	// Rolling update to a new task definition: the service ends up
	// RUNNING on the new revision.
	tdSleep2 := p4TaskDef(t, ts, token, "sleeper", []string{"sleep", "60"})
	code, upd := p3Call(t, "PUT", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, map[string]any{
		"task_definition_id": p4Str(tdSleep2, "id"),
	})
	if code != 200 {
		t.Fatalf("service update: %d %v", code, upd)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		code, tasks = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks?service_id="+svcID, token, nil)
		running := 0
		for _, raw := range p4List(tasks, "tasks") {
			m, _ := raw.(map[string]any)
			if p4Str(m, "status") == "RUNNING" && p4Str(m, "task_definition_id") == p4Str(tdSleep2, "id") {
				running++
			}
		}
		if running == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rolling update never reached 1 RUNNING on new revision: %d %v", code, tasks)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Scale to 0: the task stops for real.
	code, upd = p3Call(t, "PUT", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, map[string]any{"desired_count": 0})
	if code != 200 {
		t.Fatalf("scale to 0: %d %v", code, upd)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		code, got = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
		if code == 200 && got["running_count"] == float64(0) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scale to 0 never reached 0 running: %d %v", code, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Cluster with an active service -> 409; delete service, then cluster.
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecs/clusters/"+clusterID, token, nil); code != 409 {
		t.Fatalf("delete cluster with active service: got %d, want 409", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil); code != 200 {
		t.Fatalf("delete service: %d", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecs/clusters/"+clusterID, token, nil); code != 200 {
		t.Fatalf("delete cluster: %d", code)
	}

	// Circuit breaker: a service whose tasks exit immediately must
	// not restart forever. Regression test for a live failure: an
	// `echo` service created 3,200 task rows in ~2 min (OnStop ->
	// reconcile -> start -> exit -> OnStop -> ...), hammering
	// Postgres the whole time. Now the reconcile throttles after 5
	// starts in 60 s and the service reports it.
	code, cluster2 := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/clusters", token, map[string]any{"name": "breaker"})
	if code != 201 {
		t.Fatalf("create breaker cluster: %d %v", code, cluster2)
	}
	code, svc2 := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/services", token, map[string]any{
		"name": "echo-loop", "cluster_id": p4Str(cluster2, "id"), "task_definition_id": tdID,
		"desired_count": 1, "launch_type": "EC2",
	})
	if code != 201 {
		t.Fatalf("create echo service: %d %v", code, svc2)
	}
	svc2ID := p4Str(svc2, "id")
	time.Sleep(4 * time.Second)
	code, tasks2 := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/tasks?service_id="+svc2ID, token, nil)
	if code != 200 {
		t.Fatalf("list breaker tasks: %d", code)
	}
	if n := len(p4List(tasks2, "tasks")); n > 8 {
		t.Fatalf("circuit breaker failed: %d tasks created for an instantly-exiting service in 4s (want <= 8)", n)
	}
	code, got2 := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svc2ID, token, nil)
	if code != 200 || got2["reconcile_throttled"] != true {
		t.Fatalf("service must report reconcile_throttled: %d %v", code, got2)
	}
	// Updating the service clears the throttle (operator resumes).
	if code, _ := p3Call(t, "PUT", ts.URL+"/api/v1/aws/ecs/services/"+svc2ID, token, map[string]any{"desired_count": 0}); code != 200 {
		t.Fatalf("scale breaker service to 0: %d", code)
	}
	code, got2 = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svc2ID, token, nil)
	if code != 200 || got2["reconcile_throttled"] == true {
		t.Fatalf("service update must clear the throttle: %d %v", code, got2)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecs/services/"+svc2ID, token, nil); code != 200 {
		t.Fatalf("delete breaker service: %d", code)
	}
	if code, _ := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/ecs/clusters/"+p4Str(cluster2, "id"), token, nil); code != 200 {
		t.Fatalf("delete breaker cluster: %d", code)
	}
}

// --- CodeBuild ------------------------------------------------------------------

func p4WriteSource(t *testing.T, workdir, name, buildspec string) string {
	t.Helper()
	src := filepath.Join(workdir, name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "buildspec.yml"), []byte(buildspec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "app.txt"), []byte("hello app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// p4WriteGitSource is p4WriteSource plus a real git repo (one commit),
// so tests can exercise the git-clone checkout paths.
func p4WriteGitSource(t *testing.T, workdir, name, buildspec string) string {
	t.Helper()
	src := p4WriteSource(t, workdir, name, buildspec)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("add", "-A")
	run("-c", "user.email=cf-test@local", "-c", "user.name=cf-test", "commit", "--quiet", "-m", "init")
	return src
}

func TestPhase4CodeBuild(t *testing.T) {
	_, ts, token, workdir := p4TestServer(t)
	// Remote sources are rejected; sources outside the root are rejected.
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "remote", "source_path": "https://github.com/example/repo",
	}); code != 400 {
		t.Fatalf("remote source: got %d, want 400", code)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "outside", "source_path": "/etc",
	}); code != 400 {
		t.Fatalf("outside source: got %d, want 400", code)
	}
	// A real build: buildspec.yml in the source dir, real commands,
	// real logs, real artifacts.
	src := p4WriteSource(t, workdir, "src-good", "version: 0.2\nphases:\n  build:\n    commands:\n      - echo build-ok\n      - cp app.txt built.txt\nartifacts:\n  files:\n    - built.txt\n")
	code, proj := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "good", "source_path": src,
	})
	if code != 201 {
		t.Fatalf("create project: %d %v", code, proj)
	}
	projID := p4Str(proj, "id")
	code, build := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects/"+projID+"/builds", token, nil)
	if code != 202 {
		t.Fatalf("start build: %d %v", code, build)
	}
	buildID := p4Str(build, "id")
	if p4Str(build, "status") != "IN_PROGRESS" {
		t.Fatalf("new build must be IN_PROGRESS, got %v", build)
	}
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codebuild/builds/"+buildID, "status", "SUCCEEDED")
	code, logs := p3Call(t, "GET", ts.URL+"/api/v1/aws/codebuild/builds/"+buildID+"/logs", token, nil)
	if code != 200 || !strings.Contains(p4Str(logs, "logs"), "build-ok") {
		t.Fatalf("build logs: %d %v", code, logs)
	}
	code, got := p3Call(t, "GET", ts.URL+"/api/v1/aws/codebuild/builds/"+buildID, token, nil)
	if code != 200 {
		t.Fatalf("get build: %d", code)
	}
	artifacts, _ := got["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0] != "built.txt" {
		t.Fatalf("build artifacts = %v", got["artifacts"])
	}
	// A failing build is FAILED with the phase named, never SUCCEEDED.
	srcBad := p4WriteSource(t, workdir, "src-bad", "version: 0.2\nphases:\n  build:\n    commands:\n      - echo before-fail\n      - false\n")
	code, projBad := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "bad", "source_path": srcBad,
	})
	if code != 201 {
		t.Fatalf("create bad project: %d %v", code, projBad)
	}
	code, buildBad := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects/"+p4Str(projBad, "id")+"/builds", token, nil)
	if code != 202 {
		t.Fatalf("start bad build: %d", code)
	}
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codebuild/builds/"+p4Str(buildBad, "id"), "status", "FAILED")
	code, got = p3Call(t, "GET", ts.URL+"/api/v1/aws/codebuild/builds/"+p4Str(buildBad, "id"), token, nil)
	if !strings.Contains(p4Str(got, "failure_reason"), "build") {
		t.Fatalf("failure reason = %v", got)
	}
	code, logs = p3Call(t, "GET", ts.URL+"/api/v1/aws/codebuild/builds/"+p4Str(buildBad, "id")+"/logs", token, nil)
	if code != 200 || !strings.Contains(p4Str(logs, "logs"), "before-fail") {
		t.Fatalf("failing build logs: %d %v", code, logs)
	}
}

// --- CodePipeline + CodeDeploy -----------------------------------------------------

func TestPhase4PipelineAndDeploy(t *testing.T) {
	_, ts, token, workdir := p4TestServer(t)
	// ECS service to deploy to.
	code, cluster := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/clusters", token, map[string]any{"name": "pipe"})
	if code != 201 {
		t.Fatalf("cluster: %d", code)
	}
	td1 := p4TaskDef(t, ts, token, "pipeapp", []string{"sleep", "60"})
	code, svc := p3Call(t, "POST", ts.URL+"/api/v1/aws/ecs/services", token, map[string]any{
		"name": "pipe-web", "cluster_id": p4Str(cluster, "id"),
		"task_definition_id": p4Str(td1, "id"), "desired_count": 1, "launch_type": "EC2",
	})
	if code != 201 {
		t.Fatalf("service: %d %v", code, svc)
	}
	svcID := p4Str(svc, "id")
	td2 := p4TaskDef(t, ts, token, "pipeapp", []string{"sleep", "60"})
	// Build project whose source is the pipeline's checkout dir.
	src := p4WriteSource(t, workdir, "src-pipe", "version: 0.2\nphases:\n  build:\n    commands:\n      - echo pipe-build-ok\n")
	code, proj := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "pipe-build", "source_path": src,
	})
	if code != 201 {
		t.Fatalf("project: %d %v", code, proj)
	}
	// Pipeline: source -> build -> deploy (ecs-service). The build
	// stage references the project by NAME here (project_id is
	// also accepted) — the shape the console and the live walk
	// use. Regression test: the live walk caught "references
	// unknown CodeBuild project \"\"" because the handler only
	// read project_id.
	code, pipe := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines", token, map[string]any{
		"name": "pipe1",
		"stages": []any{
			map[string]any{"name": "Source", "type": "source", "source_path": src},
			map[string]any{"name": "Build", "type": "build", "build_project_name": "pipe-build"},
			map[string]any{"name": "Deploy", "type": "deploy", "target": "ecs-service", "service_id": svcID, "task_definition_id": p4Str(td2, "id")},
		},
	})
	if code != 201 {
		t.Fatalf("create pipeline: %d %v", code, pipe)
	}
	pipeID := p4Str(pipe, "id")
	code, execEnt := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines/"+pipeID+"/executions", token, nil)
	if code != 202 {
		t.Fatalf("start execution: %d %v", code, execEnt)
	}
	execID := p4Str(execEnt, "id")
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codepipeline/executions/"+execID, "status", "SUCCEEDED")
	code, got := p3Call(t, "GET", ts.URL+"/api/v1/aws/codepipeline/executions/"+execID, token, nil)
	if code != 200 {
		t.Fatalf("get execution: %d", code)
	}
	stages, _ := got["stages"].([]any)
	if len(stages) != 3 {
		t.Fatalf("execution stages = %v", got["stages"])
	}
	for _, raw := range stages {
		st, _ := raw.(map[string]any)
		if p4Str(st, "status") != "SUCCEEDED" {
			t.Fatalf("stage %v not SUCCEEDED: %v", st["name"], st)
		}
	}
	// The deploy really updated the service's task definition.
	code, svcGot := p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
	if code != 200 || p4Str(svcGot, "task_definition_id") != p4Str(td2, "id") {
		t.Fatalf("service after pipeline deploy: %d %v", code, svcGot)
	}
	// Pipeline whose source is a git repo. The source stage clones
	// it, chowns the checkout to uid 65534, and then the build stage
	// clones FROM that checkout — the clone that failed live with
	// "detected dubious ownership" because a "-c safe.directory"
	// flag is not inherited by the upload-pack child git spawns for
	// a local clone. Regression test: the live walk caught it.
	gitSrc := p4WriteGitSource(t, workdir, "src-git", "version: 0.2\nphases:\n  build:\n    commands:\n      - echo git-pipe-ok\n")
	code, gitProj := p3Call(t, "POST", ts.URL+"/api/v1/aws/codebuild/projects", token, map[string]any{
		"name": "git-build", "source_path": gitSrc,
	})
	if code != 201 {
		t.Fatalf("git project: %d %v", code, gitProj)
	}
	tdGit := p4TaskDef(t, ts, token, "pipeapp", []string{"sleep", "60"})
	code, gitPipe := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines", token, map[string]any{
		"name": "pipe-git",
		"stages": []any{
			map[string]any{"name": "Source", "type": "source", "source_path": gitSrc},
			map[string]any{"name": "Build", "type": "build", "build_project_name": "git-build"},
			map[string]any{"name": "Deploy", "type": "deploy", "target": "ecs-service", "service_id": svcID, "task_definition_id": p4Str(tdGit, "id")},
		},
	})
	if code != 201 {
		t.Fatalf("create git pipeline: %d %v", code, gitPipe)
	}
	code, gitExec := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines/"+p4Str(gitPipe, "id")+"/executions", token, nil)
	if code != 202 {
		t.Fatalf("start git execution: %d %v", code, gitExec)
	}
	gitExecID := p4Str(gitExec, "id")
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codepipeline/executions/"+gitExecID, "status", "SUCCEEDED")
	code, gitGot := p3Call(t, "GET", ts.URL+"/api/v1/aws/codepipeline/executions/"+gitExecID, token, nil)
	if code != 200 {
		t.Fatalf("get git execution: %d", code)
	}
	if c := p4Str(gitGot, "commit"); c == "" {
		t.Fatalf("git execution recorded no commit: %v", gitGot)
	}
	code, svcGot = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
	if code != 200 || p4Str(svcGot, "task_definition_id") != p4Str(tdGit, "id") {
		t.Fatalf("service after git pipeline deploy: %d %v", code, svcGot)
	}
	// Terraform deploy stage honestly fails (no Docker here).
	code, pipeTF := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines", token, map[string]any{
		"name": "pipe-tf",
		"stages": []any{
			map[string]any{"name": "Source", "type": "source", "source_path": src},
			map[string]any{"name": "Deploy", "type": "deploy", "target": "terraform"},
		},
	})
	if code != 201 {
		t.Fatalf("create tf pipeline: %d %v", code, pipeTF)
	}
	code, execTF := p3Call(t, "POST", ts.URL+"/api/v1/aws/codepipeline/pipelines/"+p4Str(pipeTF, "id")+"/executions", token, nil)
	if code != 202 {
		t.Fatalf("start tf execution: %d", code)
	}
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codepipeline/executions/"+p4Str(execTF, "id"), "status", "FAILED")
	code, got = p3Call(t, "GET", ts.URL+"/api/v1/aws/codepipeline/executions/"+p4Str(execTF, "id"), token, nil)
	if !strings.Contains(p4Str(got, "failure_reason"), "terraform") {
		t.Fatalf("tf failure reason = %v", got)
	}
	// CodeDeploy: deploy a third revision, then roll back to the second.
	td3 := p4TaskDef(t, ts, token, "pipeapp", []string{"sleep", "60"})
	code, app := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/applications", token, map[string]any{
		"name": "pipe-app", "target_type": "ecs-service", "target_id": svcID,
	})
	if code != 201 {
		t.Fatalf("create app: %d %v", code, app)
	}
	appID := p4Str(app, "id")
	code, dep := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/applications/"+appID+"/deployments", token, map[string]any{
		"task_definition_id": p4Str(td3, "id"),
	})
	if code != 202 {
		t.Fatalf("create deployment: %d %v", code, dep)
	}
	depID := p4Str(dep, "id")
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codedeploy/deployments/"+depID, "status", "SUCCEEDED")
	code, svcGot = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
	if p4Str(svcGot, "task_definition_id") != p4Str(td3, "id") {
		t.Fatalf("service after deploy: %v", svcGot)
	}
	code, rb := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/deployments/"+depID+"/rollback", token, nil)
	if code != 202 {
		t.Fatalf("rollback: %d %v", code, rb)
	}
	rbID := p4Str(rb, "id")
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codedeploy/deployments/"+rbID, "status", "SUCCEEDED")
	code, svcGot = p3Call(t, "GET", ts.URL+"/api/v1/aws/ecs/services/"+svcID, token, nil)
	// The revision before the CodeDeploy deploy was tdGit (the git
	// pipeline deployed it), so the rollback restores tdGit — not
	// td2, which was the pre-deploy revision before the git
	// pipeline step was added.
	if p4Str(svcGot, "task_definition_id") != p4Str(tdGit, "id") {
		t.Fatalf("service after rollback: %v (want task def %s)", svcGot, p4Str(tdGit, "id"))
	}
	// Lambda deploy + rollback: version moves, code is restored.
	code, fn := p3Call(t, "POST", ts.URL+"/api/v1/aws/lambda/functions", token, map[string]any{
		"name": "deploy-fn", "runtime": "python3", "code": "import sys,json;print(json.dumps({'v':1}))",
	})
	if code != 201 {
		t.Fatalf("create lambda: %d %v", code, fn)
	}
	fnID := p4Str(fn, "id")
	code, appFn := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/applications", token, map[string]any{
		"name": "fn-app", "target_type": "lambda-function", "target_id": fnID,
	})
	if code != 201 {
		t.Fatalf("create lambda app: %d %v", code, appFn)
	}
	code, depFn := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/applications/"+p4Str(appFn, "id")+"/deployments", token, map[string]any{
		"code": "import sys,json;print(json.dumps({'v':2}))",
	})
	if code != 202 {
		t.Fatalf("create lambda deployment: %d %v", code, depFn)
	}
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codedeploy/deployments/"+p4Str(depFn, "id"), "status", "SUCCEEDED")
	code, fnGot := p3Call(t, "GET", ts.URL+"/api/v1/aws/lambda/functions/"+fnID, token, nil)
	if code != 200 || fnGot["version"] != float64(2) || !strings.Contains(p4Str(fnGot, "code"), "'v':2") {
		t.Fatalf("lambda after deploy: %d %v", code, fnGot)
	}
	code, rbFn := p3Call(t, "POST", ts.URL+"/api/v1/aws/codedeploy/deployments/"+p4Str(depFn, "id")+"/rollback", token, nil)
	if code != 202 {
		t.Fatalf("lambda rollback: %d", code)
	}
	p4Wait(t, ts, token, ts.URL+"/api/v1/aws/codedeploy/deployments/"+p4Str(rbFn, "id"), "status", "SUCCEEDED")
	code, fnGot = p3Call(t, "GET", ts.URL+"/api/v1/aws/lambda/functions/"+fnID, token, nil)
	if code != 200 || !strings.Contains(p4Str(fnGot, "code"), "'v':1") {
		t.Fatalf("lambda after rollback: %d %v", code, fnGot)
	}
}

// --- CloudFormation --------------------------------------------------------------

const p4CFNTemplate = `{
	"AWSTemplateFormatVersion": "2010-09-09",
	"Description": "phase4 test stack",
	"Resources": {
		"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders"}},
		"Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events"}},
		"Fn": {"Type": "AWS::Lambda::Function", "Properties": {
			"FunctionName": "cfn-fn", "Runtime": "python3",
			"Code": {"ZipFile": "import sys,json;print(json.dumps({'ok':True}))"}
		}}
	}
}`

func TestPhase4CloudFormation(t *testing.T) {
	_, ts, token, _ := p4TestServer(t)
	// Template validation: good template valid, bad type listed.
	code, out := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/validate-template", token, map[string]any{"template": p4CFNTemplate})
	if code != 200 || out["valid"] != true {
		t.Fatalf("validate good template: %d %v", code, out)
	}
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/validate-template", token, map[string]any{
		"template": `{"Resources": {"Box": {"Type": "AWS::EC2::Instance", "Properties": {}}}}`,
	})
	if code != 200 || out["valid"] != false {
		t.Fatalf("validate bad template: %d %v", code, out)
	}
	// Create with an unsupported type -> 400 (nothing stored).
	code, out = p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks", token, map[string]any{
		"name": "bad-stack", "template": `{"Resources": {"Box": {"Type": "AWS::EC2::Instance", "Properties": {}}}}`,
	})
	if code != 400 {
		t.Fatalf("create stack with unsupported type: got %d %v, want 400", code, out)
	}
	// Create a real stack: SQS + SNS + Lambda provision for real.
	code, stack := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks", token, map[string]any{
		"name": "demo", "template": p4CFNTemplate,
	})
	if code != 201 {
		t.Fatalf("create stack: %d %v", code, stack)
	}
	stackID := p4Str(stack, "id")
	if p4Str(stack, "status") != "CREATE_COMPLETE" {
		t.Fatalf("stack status = %v", stack)
	}
	code, queues := p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues", token, nil)
	if code != 200 || len(p4List(queues, "queues")) != 1 {
		t.Fatalf("stack queue: %d %v", code, queues)
	}
	code, events := p3Call(t, "GET", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/events", token, nil)
	if code != 200 || len(p4List(events, "events")) < 3 {
		t.Fatalf("stack events: %d %v", code, events)
	}
	// Change set adding an unsupported type: UNSUPPORTED, not executable
	// on its own. (The template keeps the current resources so the
	// change set does not delete them.)
	code, cs := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets", token, map[string]any{
		"name": "add-box",
		"template": `{"Resources": {
			"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders"}},
			"Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events"}},
			"Fn": {"Type": "AWS::Lambda::Function", "Properties": {
				"FunctionName": "cfn-fn", "Runtime": "python3",
				"Code": {"ZipFile": "import sys,json;print(json.dumps({'ok':True}))"}
			}},
			"Queue2": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders2"}},
			"Box": {"Type": "AWS::EC2::Instance", "Properties": {}}
		}}`,
	})
	if code != 201 {
		t.Fatalf("create changeset: %d %v", code, cs)
	}
	changes, _ := cs["changes"].([]any)
	unsupported := false
	for _, raw := range changes {
		if m, _ := raw.(map[string]any); p4Str(m, "action") == "UNSUPPORTED" {
			unsupported = true
		}
	}
	if !unsupported {
		t.Fatalf("changeset must flag the EC2 type UNSUPPORTED: %v", cs["changes"])
	}
	// The change set also contains a real CREATE (Queue2), so it is
	// executable: the create applies and the unsupported change is
	// skipped with its reason recorded.
	if code, stack3 := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets/"+p4Str(cs, "id")+"/execute", token, nil); code != 200 || p4Str(stack3, "status") != "UPDATE_COMPLETE" {
		t.Fatalf("execute mixed changeset: got %d %v, want 200 UPDATE_COMPLETE", code, stack3)
	}
	// A change set with ONLY an unsupported change is not executable.
	// (The template keeps the current resources, so there are no
	// CREATE/UPDATE/DELETEs.)
	code, csOnly := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets", token, map[string]any{
		"name": "only-box",
		"template": `{"Resources": {
			"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders"}},
			"Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events"}},
			"Fn": {"Type": "AWS::Lambda::Function", "Properties": {
				"FunctionName": "cfn-fn", "Runtime": "python3",
				"Code": {"ZipFile": "import sys,json;print(json.dumps({'ok':True}))"}
			}},
			"Queue2": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders2"}},
			"Box": {"Type": "AWS::EC2::Instance", "Properties": {}}
		}}`,
	})
	if code != 201 {
		t.Fatalf("create only-unsupported changeset: %d %v", code, csOnly)
	}
	if csOnly["executable"] != false {
		t.Fatalf("only-unsupported changeset must be non-executable: %v", csOnly)
	}
	if code, _ := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets/"+p4Str(csOnly, "id")+"/execute", token, nil); code != 409 {
		t.Fatalf("execute unsupported-only changeset: got %d, want 409", code)
	}
	// Change set adding a third queue: executes to UPDATE_COMPLETE.
	code, cs2 := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets", token, map[string]any{
		"name": "add-queue3",
		"template": `{"Resources": {
			"Queue": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders"}},
			"Topic": {"Type": "AWS::SNS::Topic", "Properties": {"TopicName": "cfn-events"}},
			"Fn": {"Type": "AWS::Lambda::Function", "Properties": {
				"FunctionName": "cfn-fn", "Runtime": "python3",
				"Code": {"ZipFile": "import sys,json;print(json.dumps({'ok':True}))"}
			}},
			"Queue2": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders2"}},
			"Queue3": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "cfn-orders3"}}
		}}`,
	})
	if code != 201 {
		t.Fatalf("create changeset 2: %d %v", code, cs2)
	}
	code, stack2 := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID+"/changesets/"+p4Str(cs2, "id")+"/execute", token, nil)
	if code != 200 || p4Str(stack2, "status") != "UPDATE_COMPLETE" {
		t.Fatalf("execute changeset 2: %d %v", code, stack2)
	}
	code, queues = p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues", token, nil)
	if code != 200 || len(p4List(queues, "queues")) != 3 {
		t.Fatalf("queues after update: %d %v", code, queues)
	}
	// Delete the stack: the real resources go away too.
	code, del := p3Call(t, "DELETE", ts.URL+"/api/v1/aws/cloudformation/stacks/"+stackID, token, nil)
	if code != 200 {
		t.Fatalf("delete stack: %d %v", code, del)
	}
	code, queues = p3Call(t, "GET", ts.URL+"/api/v1/aws/sqs/queues", token, nil)
	if code != 200 || len(p4List(queues, "queues")) != 0 {
		t.Fatalf("queues after stack delete: %d %v", code, queues)
	}
	// A stack whose S3 bucket cannot be created (no MinIO here)
	// fails CREATE_FAILED with a rollback, not CREATE_COMPLETE.
	code, stack3 := p3Call(t, "POST", ts.URL+"/api/v1/aws/cloudformation/stacks", token, map[string]any{
		"name": "s3fail", "template": `{"Resources": {"Bucket": {"Type": "AWS::S3::Bucket", "Properties": {"BucketName": "cfn-bucket"}}}}`,
	})
	if code != 201 {
		t.Fatalf("create s3 stack: %d %v", code, stack3)
	}
	if p4Str(stack3, "status") != "CREATE_FAILED" {
		t.Fatalf("s3 stack status = %v, want CREATE_FAILED", stack3)
	}
	if p4Str(stack3, "failure_reason") == "" || p4Str(stack3, "rollback_status") == "" {
		t.Fatalf("failed stack must carry failure_reason + rollback_status: %v", stack3)
	}
}

// --- EKS decision ------------------------------------------------------------------

func TestPhase4EKSInfo(t *testing.T) {
	_, ts, token, _ := p4TestServer(t)
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/aws/eks/info", token, nil)
	if code != 200 {
		t.Fatalf("eks info: %d %v", code, out)
	}
	if out["fidelity"] != "UNSUPPORTED" || out["supported"] != false {
		t.Fatalf("eks info = %v", out)
	}
	path, _ := out["learning_path"].([]any)
	if len(path) != 6 {
		t.Fatalf("learning path has %d rows, want 6", len(path))
	}
	for _, raw := range path {
		m, _ := raw.(map[string]any)
		if p4Str(m, "concept") == "" || p4Str(m, "route") == "" || p4Str(m, "try_it") == "" {
			t.Fatalf("incomplete learning row: %v", m)
		}
	}
}

// --- registry sanity in the service catalog -----------------------------------------

func TestPhase4ServicesRegistry(t *testing.T) {
	_, ts, token, _ := p4TestServer(t)
	code, out := p3Call(t, "GET", ts.URL+"/api/v1/services", token, nil)
	if code != 200 {
		t.Fatalf("services: %d", code)
	}
	byID := map[string]map[string]any{}
	for _, raw := range p4List(out, "services") {
		m, _ := raw.(map[string]any)
		byID[p4Str(m, "id")] = m
	}
	for _, id := range []string{"ecr", "ecs", "fargate", "codebuild", "codepipeline", "codedeploy", "cloudformation"} {
		svc, ok := byID[id]
		if !ok {
			t.Fatalf("service %q missing from catalog", id)
		}
		if svc["availability"] != "available" {
			t.Fatalf("service %q availability = %v, want available", id, svc["availability"])
		}
		ops, _ := svc["operations"].([]any)
		if len(ops) == 0 {
			t.Fatalf("service %q has no operations", id)
		}
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			if p4Str(op, "http_route") == "" {
				t.Fatalf("service %q operation %q has no http_route", id, op["operation"])
			}
		}
	}
	eks := byID["eks"]
	if eks["fidelity"] != "UNSUPPORTED" || eks["availability"] != "unavailable" {
		t.Fatalf("eks catalog entry = %v", eks)
	}
}
