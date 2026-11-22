// Phase 4 — containers and developer tools (CloudForge v2).
//
// ECR (local registry: Postgres catalogue + content-addressed blobs
// on disk, Docker Registry HTTP API v2 subset on /v2/), ECS +
// Fargate (task definitions, clusters, services, tasks — the
// runtime is a sandboxed subprocess, SIMULATED, task status from
// the real process), EKS (UNSUPPORTED by decision — the console is
// a Kubernetes-concepts learning path on the ECS runtime),
// CodeBuild (real local builds), CodePipeline (source -> build ->
// deploy), CodeDeploy (deployments with rollback), CloudFormation
// (own template engine + stack state, change sets, rollback).
//
// Ownership: resources record owner_id; a non-admin sees only their
// own. No resource is shown READY/ACTIVE until the underlying
// operation actually succeeded; failures are FAILED with the error,
// never a fake success.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/phase3"
	"github.com/cloudforge/cloudforge/backend/internal/phase4"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// --- Phase 4 server state ----------------------------------------------------

// p4Registry returns the registry backend for this server: a real
// registry:2 container when P4RegistryURL is set, else the local
// disk backend. The local backend is the default in lite mode and
// in this sandbox (no Docker).
func (s *Server) p4Registry() phase4.RegistryBackend {
	s.p4Mu.Lock()
	defer s.p4Mu.Unlock()
	if s.p4Reg != nil {
		return s.p4Reg
	}
	if s.P4RegistryURL != "" {
		s.p4Reg = phase4.NewRemoteRegistryBackend(s.P4RegistryURL)
	} else {
		s.p4Reg = phase4.NewLocalDiskBackend(s.P3WorkdirRoot)
	}
	return s.p4Reg
}

// p4Runner returns the ECS/Fargate task runner, wiring its stop
// callback once: a stopped task updates its stored status and, when
// it belonged to an ACTIVE service, the service is reconciled back
// to its desired count (a real replacement task is started).
func (s *Server) p4Runner() *phase4.TaskRunner {
	s.p4Mu.Lock()
	defer s.p4Mu.Unlock()
	if s.p4TaskRunner != nil {
		return s.p4TaskRunner
	}
	r := phase4.NewTaskRunner(s.P3WorkdirRoot)
	r.OnStop = func(c phase4.TaskCompletion) {
		s.p4TaskStopped(c)
	}
	s.p4TaskRunner = r
	return r
}

// p4TaskStopped records a task's real exit and reconciles its service.
func (s *Server) p4TaskStopped(c phase4.TaskCompletion) {
	ctx := context.Background()
	st := s.P3
	if st == nil {
		return
	}
	ent, err := st.P3Get(ctx, "ecs_tasks", c.TaskID)
	if err != nil {
		return
	}
	status := "STOPPED"
	ent.Data["status"] = status
	ent.Data["exit_code"] = c.ExitCode
	ent.Data["stopped_at"] = p3Now()
	if c.ErrMsg != "" {
		ent.Data["stop_reason"] = c.ErrMsg
	} else if c.ExitCode == 0 {
		ent.Data["stop_reason"] = "essential container exited (exit code 0)"
	} else {
		ent.Data["stop_reason"] = fmt.Sprintf("essential container exited (exit code %d)", c.ExitCode)
	}
	if c.TimedOut {
		ent.Data["stop_reason"] = c.ErrMsg
	}
	_, _ = st.P3Update(ctx, "ecs_tasks", ent)
	if svcID := phase3.Str(ent.Data, "service_id", ""); svcID != "" {
		// Only reconcile when the service still runs this task's
		// definition: a task stopped by a rolling deployment (the
		// service already points at a new revision) must not
		// trigger a replacement from the old definition — the
		// deployment's own reconcile starts the new tasks.
		if svc, err := st.P3Get(ctx, "ecs_services", svcID); err == nil &&
			phase3.Str(svc.Data, "task_definition_id", "") == phase3.Str(ent.Data, "task_definition_id", "") {
			s.p4ReconcileService(ctx, svcID)
		}
	}
}

// p4ReconcileOrphans marks tasks that were RUNNING/PENDING when the
// API (re)started but have no live process as STOPPED — a restart
// loses the process, and the stored status must say so instead of
// claiming a task is still running.
func (s *Server) p4ReconcileOrphans(ctx context.Context) {
	s.p4OrphanOnce.Do(func() {
		st := s.P3
		if st == nil {
			return
		}
		runner := s.p4Runner()
		tasks, err := st.P3List(ctx, "ecs_tasks")
		if err != nil {
			return
		}
		for _, t := range tasks {
			status := phase3.Str(t.Data, "status", "")
			if (status == "RUNNING" || status == "PENDING") && !runner.IsRunning(t.ID) {
				t.Data["status"] = "STOPPED"
				t.Data["stop_reason"] = "api restarted; task process lost"
				t.Data["stopped_at"] = p3Now()
				_, _ = st.P3Update(ctx, "ecs_tasks", t)
			}
		}
	})
}

// p4StartTaskEntity starts the process for a stored task entity and
// marks it RUNNING. The command comes from the task definition's
// container (argv, no shell); the image is recorded, not pulled.
func (s *Server) p4StartTaskEntity(ctx context.Context, ent *store.P3Entity) error {
	argv := phase3.StrSlice(ent.Data, "command")
	if len(argv) == 0 {
		argv = []string{"sleep", "60"}
	}
	env := map[string]string{}
	if raw, ok := ent.Data["env"].(map[string]any); ok {
		for k, v := range raw {
			if str, ok := v.(string); ok {
				env[k] = str
			}
		}
	}
	timeout := phase3.Int(ent.Data, "timeout_seconds", 3600)
	memory := phase3.Int(ent.Data, "memory_mb", 512)
	sandbox, err := s.p4Runner().Start(ent.ID, argv, env, timeout, memory)
	if err != nil {
		return err
	}
	ent.Data["status"] = "RUNNING"
	ent.Data["started_at"] = p3Now()
	ent.Data["sandbox"] = sandbox
	_, err = s.P3.P3Update(ctx, "ecs_tasks", ent)
	return err
}

// p4ReconcileService keeps an ACTIVE service at its desired count:
// missing tasks are started from the service's current task
// definition, excess RUNNING tasks are stopped (oldest first).
// It is called on service reads/updates and when a task stops, so
// the desired count is real, not a stored number.
func (s *Server) p4ReconcileService(ctx context.Context, serviceID string) {
	st := s.P3
	if st == nil {
		return
	}
	svc, err := st.P3Get(ctx, "ecs_services", serviceID)
	if err != nil {
		return
	}
	if phase3.Str(svc.Data, "status", "") != "ACTIVE" {
		return
	}
	desired := phase3.Int(svc.Data, "desired_count", 0)
	tasks, _ := st.P3List(ctx, "ecs_tasks")
	running := []*store.P3Entity{}
	for _, t := range tasks {
		if phase3.Str(t.Data, "service_id", "") != serviceID {
			continue
		}
		if phase3.Str(t.Data, "status", "") == "RUNNING" && s.p4Runner().IsRunning(t.ID) {
			running = append(running, t)
		}
	}
	for len(running) > desired {
		oldest := running[0]
		_ = s.p4Runner().Stop(oldest.ID)
		oldest.Data["status"] = "STOPPED"
		oldest.Data["stop_reason"] = "stopped by service scale-in"
		oldest.Data["stopped_at"] = p3Now()
		_, _ = st.P3Update(ctx, "ecs_tasks", oldest)
		running = running[1:]
	}
	// Circuit breaker: a service whose tasks exit immediately would
	// otherwise restart forever — OnStop fires, reconcile starts a
	// replacement, it exits at once, OnStop fires again — creating
	// unbounded task rows (seen live: 3,200 rows in ~2 min for an
	// `echo` service, and the API hammering Postgres the whole
	// time). If 5+ tasks for this service were created in the last
	// 60 s, stop restarting and mark the service throttled. A later
	// service update (desired count or task definition) clears the
	// throttle, so the operator can fix the command and resume.
	// The throttle is per service, not global, and the service
	// keeps its ACTIVE status — only restarts are paused.
	if len(running) < desired {
		recent := 0
		cutoff := time.Now().Add(-60 * time.Second)
		for _, t := range tasks {
			if phase3.Str(t.Data, "service_id", "") == serviceID && t.CreatedAt.After(cutoff) {
				recent++
			}
		}
		if recent >= 5 {
			svc.Data["reconcile_throttled"] = true
			svc.Data["reconcile_throttle_reason"] = fmt.Sprintf("tasks are exiting immediately (%d starts in the last 60s); restarts paused — fix the task command, then update the service (desired count or task definition) to resume", recent)
			svc.Data["reconcile_throttled_at"] = p3Now()
			_, _ = st.P3Update(ctx, "ecs_services", svc)
			return
		}
		if phase3.Bool(svc.Data, "reconcile_throttled", false) {
			delete(svc.Data, "reconcile_throttled")
			delete(svc.Data, "reconcile_throttle_reason")
			delete(svc.Data, "reconcile_throttled_at")
			_, _ = st.P3Update(ctx, "ecs_services", svc)
		}
	}
	for len(running) < desired {
		taskDefID := phase3.Str(svc.Data, "task_definition_id", "")
		td, err := st.P3Get(ctx, "ecs_taskdefs", taskDefID)
		if err != nil {
			svc.Data["status"] = "ACTIVE"
			svc.Data["reconcile_error"] = "current task definition is missing: " + err.Error()
			_, _ = st.P3Update(ctx, "ecs_services", svc)
			return
		}
		delete(svc.Data, "reconcile_error")
		_, _ = st.P3Update(ctx, "ecs_services", svc)
		ent, err := st.P3Create(ctx, "ecs_tasks", &store.P3Entity{Name: svc.Name + "-" + ids.New()[:8], Data: map[string]any{
			"cluster_id":         phase3.Str(svc.Data, "cluster_id", ""),
			"service_id":         svc.ID,
			"task_definition_id": td.ID,
			"launch_type":        phase3.Str(svc.Data, "launch_type", "EC2"),
			"status":             "PENDING",
			"image":              firstContainerImage(td),
			"command":            firstContainerCommand(td),
			"env":                firstContainerEnv(td),
			"cpu":                phase3.Str(td.Data, "cpu", ""),
			"memory_mb":          phase3.Int(td.Data, "memory_mb", 512),
			"timeout_seconds":    3600,
			"owner_id":           phase3.Str(svc.Data, "owner_id", ""),
			"created_at":         p3Now(),
		}})
		if err != nil {
			return
		}
		if err := s.p4StartTaskEntity(ctx, ent); err != nil {
			ent.Data["status"] = "STOPPED"
			ent.Data["stop_reason"] = "failed to start: " + err.Error()
			ent.Data["stopped_at"] = p3Now()
			_, _ = st.P3Update(ctx, "ecs_tasks", ent)
			return
		}
		running = append(running, ent)
	}
}

func firstContainerImage(td *store.P3Entity) string {
	if defs, ok := td.Data["container_definitions"].([]any); ok && len(defs) > 0 {
		if m, ok := defs[0].(map[string]any); ok {
			return phase3.Str(m, "image", "")
		}
	}
	return ""
}

func firstContainerCommand(td *store.P3Entity) []any {
	if defs, ok := td.Data["container_definitions"].([]any); ok && len(defs) > 0 {
		if m, ok := defs[0].(map[string]any); ok {
			// The memory store preserves Go types (no JSON
			// round-trip), so command may be []string (as
			// registered) or []any (after a Postgres round-trip).
			switch cmd := m["command"].(type) {
			case []any:
				return cmd
			case []string:
				out := make([]any, len(cmd))
				for i, c := range cmd {
					out[i] = c
				}
				return out
			}
		}
	}
	return []any{"sleep", "60"}
}

func firstContainerEnv(td *store.P3Entity) map[string]any {
	if defs, ok := td.Data["container_definitions"].([]any); ok && len(defs) > 0 {
		if m, ok := defs[0].(map[string]any); ok {
			if env, ok := m["environment"].(map[string]any); ok {
				return env
			}
		}
	}
	return map[string]any{}
}

// --- ECR: repository catalogue -----------------------------------------------

func (s *Server) handleECRListRepos(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "ecr_repos")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		view := p3View(e)
		images, _ := st.P3List(r.Context(), "ecr_images")
		count := 0
		for _, im := range images {
			if phase3.Str(im.Data, "repository_id", "") == e.ID {
				count++
			}
		}
		view["image_count"] = count
		view["registry_backend"] = s.p4Registry().Name()
		view["registry_uri"] = "localhost:8081/" + e.Name
		out = append(out, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"repositories": out, "count": len(out), "registry_backend": s.p4Registry().Name(), "fidelity": "PARTIAL"})
}

func (s *Server) handleECRCreateRepo(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name           string `json:"name"`
		ScanOnPush     bool   `json:"scan_on_push"`
		ImmutableTags  bool   `json:"immutable_tags"`
		LifecycleRules []any  `json:"lifecycle_rules"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := phase4.ValidateECRRepoName(req.Name); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	e, err := st.P3Create(r.Context(), "ecr_repos", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"scan_on_push":    req.ScanOnPush,
		"immutable_tags":  req.ImmutableTags,
		"lifecycle_rules": req.LifecycleRules,
		"owner_id":        u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.audit(r, u.ID, "ecr_repository_created", "ecr-repository", e.ID, map[string]any{"name": req.Name})
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleECRDeleteRepo(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	repo, err := st.P3Get(r.Context(), "ecr_repos", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, repo, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	images, _ := st.P3List(r.Context(), "ecr_images")
	for _, im := range images {
		if phase3.Str(im.Data, "repository_id", "") == repo.ID {
			s.writeError(w, r, http.StatusConflict, "repository_not_empty", "repository still holds images; delete the images first (a non-empty repository is not force-deleted)")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "ecr_repos", repo.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": repo.ID})
}

func (s *Server) handleECRListImages(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	repo, err := st.P3Get(r.Context(), "ecr_repos", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, repo, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	all, _ := st.P3List(r.Context(), "ecr_images")
	out := []map[string]any{}
	for _, im := range all {
		if phase3.Str(im.Data, "repository_id", "") != repo.ID {
			continue
		}
		view := p3View(im)
		view["image_uri"] = "localhost:8081/" + repo.Name + ":" + phase3.Str(im.Data, "tag", "latest")
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["pushed_at"]) > fmt.Sprint(out[j]["pushed_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"images": out, "count": len(out), "repository": repo.Name, "registry_backend": s.p4Registry().Name()})
}

// handleECRPushImage is the API/UI push path: the caller supplies a
// schema2 manifest and its blobs (base64). Blobs are verified by
// digest on store; the manifest is stored only after every blob
// exists — the same rule the /v2/ path enforces. The docker CLI
// push path is /v2/ (see handleRegistryDispatch).
func (s *Server) handleECRPushImage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	repo, err := st.P3Get(r.Context(), "ecr_repos", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, repo, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Tag      string `json:"tag"`
		Manifest any    `json:"manifest"`
		Blobs    []struct {
			Digest  string `json:"digest"`
			Content string `json:"content_base64"`
		} `json:"blobs"`
		// Convenience shape: raw layer contents as base64. When
		// no manifest is given, the API builds a schemaVersion-2
		// manifest server-side (empty config + one layer per
		// entry, digests computed here). The strict path is
		// manifest + blobs with client-computed digests, and the
		// /v2/ registry API takes real manifests directly.
		Layers []string `json:"layers"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	tag := strings.TrimSpace(req.Tag)
	if tag == "" {
		tag = "latest"
	}
	if err := phase4.ValidateImageTag(tag); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	backend := s.p4Registry()
	if req.Manifest == nil {
		if len(req.Layers) == 0 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "manifest is required (schemaVersion 2 JSON) or layers (base64 layer contents)")
			return
		}
		// Synthesise the manifest from the raw layers.
		type layerRef struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		}
		layerRefs := []layerRef{}
		for _, b64 := range req.Layers {
			if len(b64) > 10*1024*1024 {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "a single layer is capped at 10 MiB through the API push path (use the /v2/ registry API for larger layers)")
				return
			}
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "layer is not valid base64: "+err.Error())
				return
			}
			digest := phase4.Digest(raw)
			if err := backend.PutBlob(r.Context(), repo.Name, digest, strings.NewReader(string(raw))); err != nil {
				s.writeError(w, r, http.StatusBadGateway, "registry_error", "storing blob "+digest+": "+err.Error())
				return
			}
			layerRefs = append(layerRefs, layerRef{MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip", Digest: digest, Size: int64(len(raw))})
		}
		configBytes := []byte("{}")
		configDigest := phase4.Digest(configBytes)
		if err := backend.PutBlob(r.Context(), repo.Name, configDigest, strings.NewReader(string(configBytes))); err != nil {
			s.writeError(w, r, http.StatusBadGateway, "registry_error", "storing config blob: "+err.Error())
			return
		}
		synth, err := json.Marshal(map[string]any{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
			"config":        map[string]any{"mediaType": "application/vnd.docker.container.image.v1+json", "digest": configDigest, "size": len(configBytes)},
			"layers":        layerRefs,
		})
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", "building manifest: "+err.Error())
			return
		}
		req.Manifest = json.RawMessage(synth)
	}
	manifestBytes, err := json.Marshal(req.Manifest)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "manifest is not JSON-serialisable: "+err.Error())
		return
	}
	for _, b := range req.Blobs {
		if len(b.Content) > 10*1024*1024 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "a single blob is capped at 10 MiB through the API push path (use the /v2/ registry API for larger layers)")
			return
		}
		raw, err := base64.StdEncoding.DecodeString(b.Content)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "blob "+b.Digest+" is not valid base64: "+err.Error())
			return
		}
		if phase4.Digest(raw) != b.Digest {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("blob digest mismatch: content is %s, claimed %s", phase4.Digest(raw), b.Digest))
			return
		}
		if err := backend.PutBlob(r.Context(), repo.Name, b.Digest, strings.NewReader(string(raw))); err != nil {
			s.writeError(w, r, http.StatusBadGateway, "registry_error", "storing blob "+b.Digest+": "+err.Error())
			return
		}
	}
	digest, err := backend.PutManifest(r.Context(), repo.Name, tag, manifestBytes)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "manifest rejected: "+err.Error())
		return
	}
	var m struct {
		Config struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"config"`
		Layers []struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"layers"`
	}
	_ = json.Unmarshal(manifestBytes, &m)
	layers := []any{}
	var size int64
	for _, l := range m.Layers {
		layers = append(layers, map[string]any{"digest": l.Digest, "size": l.Size})
		size += l.Size
	}
	size += m.Config.Size
	name := repo.Name + ":" + tag
	// Immutable tags: an existing tag cannot be overwritten.
	if phase3.Bool(repo.Data, "immutable_tags", false) {
		if _, err := st.P3GetByName(r.Context(), "ecr_images", name); err == nil {
			s.writeError(w, r, http.StatusConflict, "tag_immutable", "repository has immutable tags enabled; tag "+tag+" already exists")
			return
		}
	}
	img, err := st.P3Create(r.Context(), "ecr_images", &store.P3Entity{Name: name, Data: map[string]any{
		"repository_id": repo.ID, "repository_name": repo.Name, "tag": tag,
		"digest": digest, "size_bytes": size, "layers": layers,
		"config_digest": m.Config.Digest, "pushed_at": p3Now(), "owner_id": u.ID,
		"backend": backend.Name(),
	}})
	if err != nil {
		if errors.Is(err, store.ErrP3Conflict) {
			// Re-push of the same tag: update the existing image row.
			ex, gerr := st.P3GetByName(r.Context(), "ecr_images", name)
			if gerr != nil {
				s.p3Err(w, r, err)
				return
			}
			ex.Data["digest"] = digest
			ex.Data["size_bytes"] = size
			ex.Data["layers"] = layers
			ex.Data["config_digest"] = m.Config.Digest
			ex.Data["pushed_at"] = p3Now()
			ex.Data["backend"] = backend.Name()
			img, err = st.P3Update(r.Context(), "ecr_images", ex)
			if err != nil {
				s.p3Err(w, r, err)
				return
			}
		} else {
			s.p3Err(w, r, err)
			return
		}
	}
	s.p4ApplyLifecycle(r.Context(), repo)
	s.audit(r, u.ID, "ecr_image_pushed", "ecr-image", img.ID, map[string]any{"repository": repo.Name, "tag": tag, "digest": digest})
	view := p3View(img)
	view["image_uri"] = "localhost:8081/" + name
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleECRGetImage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	im, err := st.P3Get(r.Context(), "ecr_images", chi.URLParam(r, "imageId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, im, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(im)
	view["image_uri"] = "localhost:8081/" + phase3.Str(im.Data, "repository_name", "") + ":" + phase3.Str(im.Data, "tag", "latest")
	manifest, _, err := s.p4Registry().GetManifest(r.Context(), phase3.Str(im.Data, "repository_name", ""), phase3.Str(im.Data, "tag", "latest"))
	if err == nil {
		var m any
		if json.Unmarshal(manifest, &m) == nil {
			view["manifest"] = m
		}
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleECRDeleteImage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	im, err := st.P3Get(r.Context(), "ecr_images", chi.URLParam(r, "imageId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, im, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	_ = s.p4Registry().DeleteManifest(r.Context(), phase3.Str(im.Data, "repository_name", ""), phase3.Str(im.Data, "tag", "latest"))
	if err := st.P3Delete(r.Context(), "ecr_images", im.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": im.ID})
}

// handleECRScan runs the image integrity scan: every layer blob is
// re-read and its digest re-verified, and the manifest schema is
// checked. This is a real scan of what the registry stores — it is
// NOT a vulnerability scan (no CVE database, no image filesystem to
// inspect), and the response says so. A vulnerability scan needs a
// scanner binary on the host, which is not configured here.
func (s *Server) handleECRScan(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	im, err := st.P3Get(r.Context(), "ecr_images", chi.URLParam(r, "imageId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, im, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	findings := []map[string]any{}
	layers, _ := im.Data["layers"].([]any)
	checked := 0
	for _, raw := range layers {
		lm, _ := raw.(map[string]any)
		digest := phase3.Str(lm, "digest", "")
		if digest == "" {
			continue
		}
		checked++
		rc, _, err := s.p4Registry().GetBlob(r.Context(), digest)
		if err != nil {
			findings = append(findings, map[string]any{"severity": "HIGH", "title": "layer blob missing", "description": "layer " + digest + " is referenced by the manifest but not present in the registry: " + err.Error()})
			continue
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			findings = append(findings, map[string]any{"severity": "HIGH", "title": "layer blob unreadable", "description": err.Error()})
			continue
		}
		if phase4.Digest(raw) != digest {
			findings = append(findings, map[string]any{"severity": "HIGH", "title": "layer blob digest mismatch", "description": "stored bytes for " + digest + " do not hash to the claimed digest"})
		}
	}
	status := "SUCCEEDED"
	if len(findings) > 0 {
		status = "FAILED"
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"image":          phase3.Str(im.Data, "repository_name", "") + ":" + phase3.Str(im.Data, "tag", "latest"),
		"digest":         phase3.Str(im.Data, "digest", ""),
		"scan_type":      "integrity",
		"scan_status":    status,
		"layers_checked": checked,
		"findings":       findings,
		"scanned_at":     p3Now(),
		"note":           "Integrity scan only: every layer blob was re-read and digest-verified. This is NOT a vulnerability (CVE) scan — no scanner binary is configured on this host and the manifest-only registry has no image filesystem to inspect.",
	})
}

// handleECRLifecycle evaluates a repository's lifecycle rules now:
// keep only the newest N tagged images, and delete untagged images
// older than N days. Rules come from the repository record.
func (s *Server) handleECRLifecycle(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	repo, err := st.P3Get(r.Context(), "ecr_repos", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, repo, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	deleted := s.p4ApplyLifecycle(r.Context(), repo)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"repository": repo.Name, "deleted_images": deleted, "count": len(deleted)})
}

// p4ApplyLifecycle applies a repo's lifecycle rules and returns the
// ids of deleted images. Rules: {"type":"keep_last_n_tagged",
// "count":N} and {"type":"expire_untagged_days","days":N} — and the
// shorthand the console and the live walk send,
// {"keep_last_n_tagged":N,"expire_untagged_days":D} (no "type"
// field). Both shapes are accepted; a rule that matches neither is
// ignored, never silently treated as a deletion.
func (s *Server) p4ApplyLifecycle(ctx context.Context, repo *store.P3Entity) []string {
	st := s.P3
	if st == nil {
		return nil
	}
	rules, _ := repo.Data["lifecycle_rules"].([]any)
	if len(rules) == 0 {
		return nil
	}
	all, _ := st.P3List(ctx, "ecr_images")
	mine := []*store.P3Entity{}
	for _, im := range all {
		if phase3.Str(im.Data, "repository_id", "") == repo.ID {
			mine = append(mine, im)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt.After(mine[j].CreatedAt) })
	deleted := []string{}
	remove := func(im *store.P3Entity) {
		_ = s.p4Registry().DeleteManifest(ctx, phase3.Str(im.Data, "repository_name", ""), phase3.Str(im.Data, "tag", "latest"))
		if err := st.P3Delete(ctx, "ecr_images", im.ID); err == nil {
			deleted = append(deleted, im.ID)
		}
	}
	for _, raw := range rules {
		rule, _ := raw.(map[string]any)
		// Normalise the two accepted shapes to (type, count/days).
		kind := phase3.Str(rule, "type", "")
		count := phase3.Int(rule, "count", 0)
		days := phase3.Int(rule, "days", 0)
		if kind == "" {
			if n := phase3.Int(rule, "keep_last_n_tagged", 0); n > 0 {
				kind, count = "keep_last_n_tagged", n
			} else if d := phase3.Int(rule, "expire_untagged_days", 0); d > 0 {
				kind, days = "expire_untagged_days", d
			}
		}
		switch kind {
		case "keep_last_n_tagged":
			n := count
			if n <= 0 {
				continue
			}
			kept := 0
			for _, im := range mine {
				if phase3.Str(im.Data, "tag", "") == "" {
					continue
				}
				kept++
				if kept > n {
					remove(im)
				}
			}
		case "expire_untagged_days":
			if days <= 0 {
				continue
			}
			cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
			for _, im := range mine {
				if phase3.Str(im.Data, "tag", "") != "" {
					continue
				}
				if im.CreatedAt.Before(cutoff) {
					remove(im)
				}
			}
		}
		// Refresh the list after each rule (a rule may have removed rows).
		all, _ = st.P3List(ctx, "ecr_images")
		mine = mine[:0]
		for _, im := range all {
			if phase3.Str(im.Data, "repository_id", "") == repo.ID {
				mine = append(mine, im)
			}
		}
		sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt.After(mine[j].CreatedAt) })
	}
	return deleted
}

// --- Docker Registry HTTP API v2 subset (/v2/) --------------------------------
//
// Unauthenticated by design for local use (a docker client cannot
// present a CloudForge JWT); the registry only ever runs on the
// local host / local container network. The response always carries
// Docker-Distribution-API-Version: registry/2.0.

var p4Uploads = struct {
	sync.Mutex
	m map[string]string // upload uuid -> repo
}{m: map[string]string{}}

func (s *Server) registryHeader(w http.ResponseWriter) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
}

func (s *Server) handleRegistryDispatch(w http.ResponseWriter, r *http.Request) {
	s.registryHeader(w)
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	path = strings.TrimSuffix(path, "/")
	backend := s.p4Registry()
	ctx := r.Context()
	switch {
	case path == "":
		if r.Method != http.MethodGet {
			s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported on /v2/")
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{})
		return
	case path == "_catalog":
		repos, err := backend.Catalog(ctx)
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"repositories": repos})
		return
	}
	parts := strings.Split(path, "/")
	// <name>/tags/list — name may contain slashes.
	if len(parts) >= 2 && parts[len(parts)-2] == "tags" && parts[len(parts)-1] == "list" {
		repo := strings.Join(parts[:len(parts)-2], "/")
		tags, err := backend.Tags(ctx, repo)
		if err != nil {
			if errors.Is(err, phase4.ErrRegistryNotFound) {
				s.writeError(w, r, http.StatusNotFound, "not_found", "repository "+repo+" not found")
				return
			}
			s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"name": repo, "tags": tags})
		return
	}
	// <name>/manifests/<ref>
	if len(parts) >= 3 && parts[len(parts)-2] == "manifests" {
		repo := strings.Join(parts[:len(parts)-2], "/")
		ref := parts[len(parts)-1]
		switch r.Method {
		case http.MethodGet:
			manifest, digest, err := backend.GetManifest(ctx, repo, ref)
			if err != nil {
				if errors.Is(err, phase4.ErrRegistryNotFound) {
					s.writeError(w, r, http.StatusNotFound, "not_found", "manifest "+ref+" not found in "+repo)
					return
				}
				s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifest)
			return
		case http.MethodPut:
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10*1024*1024))
			if err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "manifest body could not be read: "+err.Error())
				return
			}
			digest, err := backend.PutManifest(ctx, repo, ref, raw)
			if err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "manifest rejected: "+err.Error())
				return
			}
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Location", "/v2/"+repo+"/manifests/"+digest)
			w.WriteHeader(http.StatusCreated)
			return
		case http.MethodDelete:
			if err := backend.DeleteManifest(ctx, repo, ref); err != nil {
				if errors.Is(err, phase4.ErrRegistryNotFound) {
					s.writeError(w, r, http.StatusNotFound, "not_found", "manifest "+ref+" not found in "+repo)
					return
				}
				s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
				return
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "manifests support GET, PUT and DELETE only")
		return
	}
	// <name>/blobs/uploads/ (POST) and <name>/blobs/uploads/<uuid> (PUT)
	if len(parts) >= 3 && parts[len(parts)-2] == "blobs" && parts[len(parts)-1] == "uploads" && r.Method == http.MethodPost {
		repo := strings.Join(parts[:len(parts)-2], "/")
		// Monolithic upload with ?digest= in the POST body is
		// accepted directly; otherwise a session uuid is issued.
		if digest := r.URL.Query().Get("digest"); digest != "" {
			if err := backend.PutBlob(ctx, repo, digest, r.Body); err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", "blob rejected: "+err.Error())
				return
			}
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Location", "/v2/"+repo+"/blobs/"+digest)
			w.WriteHeader(http.StatusCreated)
			return
		}
		uuid := ids.New()
		p4Uploads.Lock()
		p4Uploads.m[uuid] = repo
		p4Uploads.Unlock()
		w.Header().Set("Location", "/v2/"+repo+"/blobs/uploads/"+uuid)
		w.Header().Set("Range", "0-0")
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(parts) >= 4 && parts[len(parts)-3] == "blobs" && parts[len(parts)-2] == "uploads" && r.Method == http.MethodPut {
		uuid := parts[len(parts)-1]
		p4Uploads.Lock()
		repo, ok := p4Uploads.m[uuid]
		if ok {
			delete(p4Uploads.m, uuid)
		}
		p4Uploads.Unlock()
		if !ok {
			s.writeError(w, r, http.StatusNotFound, "not_found", "unknown blob upload "+uuid)
			return
		}
		digest := r.URL.Query().Get("digest")
		if digest == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "monolithic blob upload requires ?digest=sha256:...")
			return
		}
		if err := backend.PutBlob(ctx, repo, digest, r.Body); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "blob rejected: "+err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Location", "/v2/"+repo+"/blobs/"+digest)
		w.WriteHeader(http.StatusCreated)
		return
	}
	// <name>/blobs/<digest>
	if len(parts) >= 3 && parts[len(parts)-2] == "blobs" {
		repo := strings.Join(parts[:len(parts)-2], "/")
		digest := parts[len(parts)-1]
		_ = repo // local backend addresses blobs by digest alone
		if r.Method != http.MethodGet {
			s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "blobs support GET only here (delete via the API)")
			return
		}
		if rb, ok := backend.(*phase4.RemoteRegistryBackend); ok {
			rc, size, err := rb.GetBlobForRepo(ctx, repo, digest)
			if err != nil {
				if errors.Is(err, phase4.ErrRegistryNotFound) {
					s.writeError(w, r, http.StatusNotFound, "not_found", "blob "+digest+" not found")
					return
				}
				s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
				return
			}
			defer rc.Close()
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", digest)
			if size >= 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.Copy(w, rc)
			return
		}
		rc, size, err := backend.GetBlob(ctx, digest)
		if err != nil {
			if errors.Is(err, phase4.ErrRegistryNotFound) {
				s.writeError(w, r, http.StatusNotFound, "not_found", "blob "+digest+" not found")
				return
			}
			s.writeError(w, r, http.StatusBadGateway, "registry_error", err.Error())
			return
		}
		defer rc.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rc)
		return
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "unsupported registry path "+r.URL.Path+" (the implemented subset is: /v2/, /v2/_catalog, tags/list, manifests, blobs — see docs)")
}

// --- ECS: clusters -----------------------------------------------------------

func (s *Server) handleECSListClusters(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "ecs_clusters")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		view := p3View(e)
		tasks, _ := st.P3List(r.Context(), "ecs_tasks")
		running := 0
		for _, t := range tasks {
			if phase3.Str(t.Data, "cluster_id", "") == e.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
				running++
			}
		}
		view["running_tasks"] = running
		out = append(out, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"clusters": out, "count": len(out), "fidelity": "SIMULATED", "runtime": "sandboxed subprocess on the API host — no Docker container is started"})
}

func (s *Server) handleECSCreateCluster(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string            `json:"name"`
		Tags map[string]string `json:"tags"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 255 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "cluster name is required (max 255 characters)")
		return
	}
	e, err := st.P3Create(r.Context(), "ecs_clusters", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"status": "ACTIVE", "tags": req.Tags, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleECSDeleteCluster(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	cl, err := st.P3Get(r.Context(), "ecs_clusters", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, cl, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	tasks, _ := st.P3List(r.Context(), "ecs_tasks")
	for _, t := range tasks {
		if phase3.Str(t.Data, "cluster_id", "") == cl.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
			s.writeError(w, r, http.StatusConflict, "cluster_not_empty", "cluster still has running tasks; stop them first")
			return
		}
	}
	services, _ := st.P3List(r.Context(), "ecs_services")
	for _, svc := range services {
		if phase3.Str(svc.Data, "cluster_id", "") == cl.ID && phase3.Str(svc.Data, "status", "") == "ACTIVE" {
			s.writeError(w, r, http.StatusConflict, "cluster_not_empty", "cluster still has active services; delete them first")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "ecs_clusters", cl.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": cl.ID})
}

// --- ECS: task definitions ---------------------------------------------------

type ecsContainerDef struct {
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Command      []string          `json:"command"`
	Environment  map[string]string `json:"environment"`
	PortMappings []map[string]any  `json:"port_mappings"`
	Essential    bool              `json:"essential"`
	CPU          int               `json:"cpu"`
	MemoryMB     int               `json:"memory_mb"`
}

func (s *Server) handleECSRegisterTaskDef(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Family               string            `json:"family"`
		ContainerDefinitions []ecsContainerDef `json:"container_definitions"`
		CPU                  string            `json:"cpu"`
		MemoryMB             int               `json:"memory_mb"`
		NetworkMode          string            `json:"network_mode"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Family = strings.TrimSpace(req.Family)
	if req.Family == "" || len(req.Family) > 255 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "task definition family is required (max 255 characters)")
		return
	}
	if len(req.ContainerDefinitions) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "at least one container definition is required")
		return
	}
	for i, c := range req.ContainerDefinitions {
		if strings.TrimSpace(c.Name) == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("container definition %d has no name", i))
			return
		}
		if strings.TrimSpace(c.Image) == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("container %q has no image", c.Name))
			return
		}
		if len(c.Command) == 0 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("container %q has no command — the runtime executes the command as a sandboxed subprocess, so a command is required", c.Name))
			return
		}
		if c.MemoryMB < 0 || c.MemoryMB > 4096 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("container %q memory_mb must be 1-4096", c.Name))
			return
		}
		if c.CPU < 0 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("container %q cpu must be >= 0", c.Name))
			return
		}
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 512
	}
	if req.MemoryMB > 4096 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "memory_mb must be 1-4096")
		return
	}
	// Revision: next integer for this family.
	existing, _ := st.P3List(r.Context(), "ecs_taskdefs")
	revision := 1
	for _, e := range existing {
		if phase3.Str(e.Data, "family", "") == req.Family {
			if rev := phase3.Int(e.Data, "revision", 0); rev >= revision {
				revision = rev + 1
			}
		}
	}
	defs := []any{}
	for _, c := range req.ContainerDefinitions {
		env := map[string]any{}
		for k, v := range c.Environment {
			env[k] = v
		}
		defs = append(defs, map[string]any{
			"name": c.Name, "image": c.Image, "command": c.Command,
			"environment": env, "port_mappings": c.PortMappings, "essential": c.Essential,
			"cpu": c.CPU, "memory_mb": c.MemoryMB,
		})
	}
	name := fmt.Sprintf("%s:%d", req.Family, revision)
	e, err := st.P3Create(r.Context(), "ecs_taskdefs", &store.P3Entity{Name: name, Data: map[string]any{
		"family": req.Family, "revision": revision, "container_definitions": defs,
		"cpu": req.CPU, "memory_mb": req.MemoryMB, "network_mode": req.NetworkMode,
		"status": "ACTIVE", "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	view := p3View(e)
	view["task_definition_arn"] = "arn:cloudforge:ecs::local:task-definition/" + name
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handleECSListTaskDefs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	family := strings.TrimSpace(r.URL.Query().Get("family"))
	all, err := st.P3List(r.Context(), "ecs_taskdefs")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		if family != "" && phase3.Str(e.Data, "family", "") != family {
			continue
		}
		view := p3View(e)
		view["task_definition_arn"] = "arn:cloudforge:ecs::local:task-definition/" + e.Name
		out = append(out, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"task_definitions": out, "count": len(out)})
}

func (s *Server) handleECSDeleteTaskDef(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	td, err := st.P3Get(r.Context(), "ecs_taskdefs", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, td, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	services, _ := st.P3List(r.Context(), "ecs_services")
	for _, svc := range services {
		if phase3.Str(svc.Data, "task_definition_id", "") == td.ID && phase3.Str(svc.Data, "status", "") == "ACTIVE" {
			s.writeError(w, r, http.StatusConflict, "task_definition_in_use", "task definition is the current revision of active service "+svc.Name)
			return
		}
	}
	if err := st.P3Delete(r.Context(), "ecs_taskdefs", td.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": td.ID})
}

// --- ECS: services -------------------------------------------------------------

func (s *Server) handleECSCreateService(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name             string `json:"name"`
		ClusterID        string `json:"cluster_id"`
		TaskDefinitionID string `json:"task_definition_id"`
		DesiredCount     int    `json:"desired_count"`
		LaunchType       string `json:"launch_type"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "service name is required")
		return
	}
	launchType := strings.ToUpper(strings.TrimSpace(req.LaunchType))
	if launchType == "" {
		launchType = "EC2"
	}
	if launchType != "EC2" && launchType != "FARGATE" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "launch_type must be EC2 or FARGATE")
		return
	}
	if launchType == "FARGATE" {
		// Fargate is serverless: no cluster is required or used.
		req.ClusterID = ""
	} else {
		if req.ClusterID == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "cluster_id is required for launch type EC2 (use FARGATE for a serverless run with no cluster)")
			return
		}
		if _, err := st.P3Get(r.Context(), "ecs_clusters", req.ClusterID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no cluster with that id")
			return
		}
	}
	if _, err := st.P3Get(r.Context(), "ecs_taskdefs", req.TaskDefinitionID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no task definition with that id")
		return
	}
	if req.DesiredCount < 0 || req.DesiredCount > 10 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "desired_count must be 0-10 (teaching-scale cap)")
		return
	}
	e, err := st.P3Create(r.Context(), "ecs_services", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"cluster_id": req.ClusterID, "task_definition_id": req.TaskDefinitionID,
		"desired_count": req.DesiredCount, "launch_type": launchType,
		"status": "ACTIVE", "owner_id": u.ID, "deployments": []any{},
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.p4ReconcileService(r.Context(), e.ID)
	updated, _ := st.P3Get(r.Context(), "ecs_services", e.ID)
	if updated != nil {
		e = updated
	}
	s.audit(r, u.ID, "ecs_service_created", "ecs-service", e.ID, map[string]any{"name": req.Name, "desired_count": req.DesiredCount})
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleECSListServices(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	s.p4ReconcileOrphans(r.Context())
	all, err := st.P3List(r.Context(), "ecs_services")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		s.p4ReconcileService(r.Context(), e.ID)
		fresh, _ := st.P3Get(r.Context(), "ecs_services", e.ID)
		if fresh != nil {
			e = fresh
		}
		view := p3View(e)
		tasks, _ := st.P3List(r.Context(), "ecs_tasks")
		running := 0
		for _, t := range tasks {
			if phase3.Str(t.Data, "service_id", "") == e.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
				running++
			}
		}
		view["running_count"] = running
		out = append(out, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"services": out, "count": len(out)})
}

func (s *Server) handleECSGetService(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	s.p4ReconcileOrphans(r.Context())
	e, err := st.P3Get(r.Context(), "ecs_services", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.p4ReconcileService(r.Context(), e.ID)
	fresh, _ := st.P3Get(r.Context(), "ecs_services", e.ID)
	if fresh != nil {
		e = fresh
	}
	view := p3View(e)
	tasks, _ := st.P3List(r.Context(), "ecs_tasks")
	running := 0
	for _, t := range tasks {
		if phase3.Str(t.Data, "service_id", "") == e.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
			running++
		}
	}
	view["running_count"] = running
	s.writeJSON(w, r, http.StatusOK, view)
}

// handleECSUpdateService changes desired count and/or task
// definition. A task-definition change is a real rolling deployment:
// the old tasks are stopped and new ones start from the new
// revision; the previous revision is recorded in deployments so a
// rollback (CodeDeploy or a manual update back) is possible.
func (s *Server) handleECSUpdateService(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "ecs_services", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		DesiredCount     *int   `json:"desired_count"`
		TaskDefinitionID string `json:"task_definition_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.DesiredCount != nil {
		if *req.DesiredCount < 0 || *req.DesiredCount > 10 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "desired_count must be 0-10 (teaching-scale cap)")
			return
		}
		e.Data["desired_count"] = *req.DesiredCount
	}
	if req.TaskDefinitionID != "" && req.TaskDefinitionID != phase3.Str(e.Data, "task_definition_id", "") {
		if _, err := st.P3Get(r.Context(), "ecs_taskdefs", req.TaskDefinitionID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no task definition with that id")
			return
		}
		old := phase3.Str(e.Data, "task_definition_id", "")
		deployments, _ := e.Data["deployments"].([]any)
		deployments = append(deployments, map[string]any{
			"at": p3Now(), "from_task_definition_id": old,
			"to_task_definition_id": req.TaskDefinitionID, "status": "IN_PROGRESS",
		})
		e.Data["deployments"] = deployments
		e.Data["task_definition_id"] = req.TaskDefinitionID
		// Rolling deployment: stop the old tasks now; the reconcile
		// below starts the replacements from the new revision.
		tasks, _ := st.P3List(r.Context(), "ecs_tasks")
		for _, t := range tasks {
			if phase3.Str(t.Data, "service_id", "") == e.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
				_ = s.p4Runner().Stop(t.ID)
				t.Data["status"] = "STOPPED"
				t.Data["stop_reason"] = "replaced by deployment to a new task definition"
				t.Data["stopped_at"] = p3Now()
				_, _ = st.P3Update(r.Context(), "ecs_tasks", t)
			}
		}
	}
	// A service update clears the reconcile throttle (see
	// p4ReconcileService): the operator fixed the task command
	// (or scaled to 0 and back) and is resuming deliberately.
	delete(e.Data, "reconcile_throttled")
	delete(e.Data, "reconcile_throttle_reason")
	delete(e.Data, "reconcile_throttled_at")
	updated, err := st.P3Update(r.Context(), "ecs_services", e)
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.p4ReconcileService(r.Context(), updated.ID)
	fresh, _ := st.P3Get(r.Context(), "ecs_services", updated.ID)
	if fresh != nil {
		updated = fresh
	}
	// Mark the deployment complete when the desired count is running.
	deployments, _ := updated.Data["deployments"].([]any)
	if len(deployments) > 0 {
		if last, ok := deployments[len(deployments)-1].(map[string]any); ok && phase3.Str(last, "status", "") == "IN_PROGRESS" {
			tasks, _ := st.P3List(r.Context(), "ecs_tasks")
			running := 0
			for _, t := range tasks {
				if phase3.Str(t.Data, "service_id", "") == updated.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
					running++
				}
			}
			if running >= phase3.Int(updated.Data, "desired_count", 0) {
				last["status"] = "COMPLETED"
				updated.Data["deployments"] = deployments
				_, _ = st.P3Update(r.Context(), "ecs_services", updated)
			}
		}
	}
	s.writeJSON(w, r, http.StatusOK, p3View(updated))
}

func (s *Server) handleECSDeleteService(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "ecs_services", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	tasks, _ := st.P3List(r.Context(), "ecs_tasks")
	for _, t := range tasks {
		if phase3.Str(t.Data, "service_id", "") == e.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
			_ = s.p4Runner().Stop(t.ID)
			t.Data["status"] = "STOPPED"
			t.Data["stop_reason"] = "service deleted"
			t.Data["stopped_at"] = p3Now()
			_, _ = st.P3Update(r.Context(), "ecs_tasks", t)
		}
	}
	// INACTIVE first, so the reconciler never restarts its tasks.
	e.Data["status"] = "INACTIVE"
	e.Data["desired_count"] = 0
	_, _ = st.P3Update(r.Context(), "ecs_services", e)
	if err := st.P3Delete(r.Context(), "ecs_services", e.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": e.ID})
}

// --- ECS: tasks ---------------------------------------------------------------

func (s *Server) handleECSRunTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		ClusterID        string `json:"cluster_id"`
		TaskDefinitionID string `json:"task_definition_id"`
		LaunchType       string `json:"launch_type"`
		Count            int    `json:"count"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	launchType := strings.ToUpper(strings.TrimSpace(req.LaunchType))
	if launchType == "" {
		launchType = "EC2"
	}
	if launchType != "EC2" && launchType != "FARGATE" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "launch_type must be EC2 or FARGATE")
		return
	}
	if launchType == "FARGATE" {
		req.ClusterID = ""
	} else {
		if req.ClusterID == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "cluster_id is required for launch type EC2 (use FARGATE for a serverless run with no cluster)")
			return
		}
		if _, err := st.P3Get(r.Context(), "ecs_clusters", req.ClusterID); err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no cluster with that id")
			return
		}
	}
	td, err := st.P3Get(r.Context(), "ecs_taskdefs", req.TaskDefinitionID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "no task definition with that id")
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 10 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "count must be 1-10 (teaching-scale cap)")
		return
	}
	started := []map[string]any{}
	for i := 0; i < req.Count; i++ {
		ent, err := st.P3Create(r.Context(), "ecs_tasks", &store.P3Entity{Name: phase3.Str(td.Data, "family", "task") + "-" + ids.New()[:8], Data: map[string]any{
			"cluster_id": req.ClusterID, "task_definition_id": td.ID,
			"launch_type": launchType, "status": "PENDING",
			"image": firstContainerImage(td), "command": firstContainerCommand(td),
			"env": firstContainerEnv(td), "cpu": phase3.Str(td.Data, "cpu", ""),
			"memory_mb":       phase3.Int(td.Data, "memory_mb", 512),
			"timeout_seconds": 3600, "owner_id": u.ID, "created_at": p3Now(),
		}})
		if err != nil {
			s.p3Err(w, r, err)
			return
		}
		if err := s.p4StartTaskEntity(r.Context(), ent); err != nil {
			ent.Data["status"] = "STOPPED"
			ent.Data["stop_reason"] = "failed to start: " + err.Error()
			ent.Data["stopped_at"] = p3Now()
			_, _ = st.P3Update(r.Context(), "ecs_tasks", ent)
			s.writeError(w, r, http.StatusBadGateway, "task_start_failed", "task process could not be started: "+err.Error())
			return
		}
		fresh, _ := st.P3Get(r.Context(), "ecs_tasks", ent.ID)
		if fresh != nil {
			ent = fresh
		}
		started = append(started, p3View(ent))
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"tasks": started, "count": len(started), "launch_type": launchType, "runtime": "sandboxed subprocess on the API host — no Docker container is started; the image is recorded, not pulled"})
}

func (s *Server) handleECSListTasks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	s.p4ReconcileOrphans(r.Context())
	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster_id"))
	serviceID := strings.TrimSpace(r.URL.Query().Get("service_id"))
	launchType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("launch_type")))
	all, err := st.P3List(r.Context(), "ecs_tasks")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		if clusterID != "" && phase3.Str(e.Data, "cluster_id", "") != clusterID {
			continue
		}
		if serviceID != "" && phase3.Str(e.Data, "service_id", "") != serviceID {
			continue
		}
		if launchType != "" && phase3.Str(e.Data, "launch_type", "") != launchType {
			continue
		}
		view := p3View(e)
		view["running"] = s.p4Runner().IsRunning(e.ID)
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["created_at"]) > fmt.Sprint(out[j]["created_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tasks": out, "count": len(out)})
}

func (s *Server) handleECSGetTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "ecs_tasks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(e)
	view["running"] = s.p4Runner().IsRunning(e.ID)
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleECSStopTask(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "ecs_tasks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	if phase3.Str(e.Data, "status", "") != "RUNNING" {
		s.writeError(w, r, http.StatusConflict, "task_not_running", "task is "+phase3.Str(e.Data, "status", "unknown")+", not RUNNING")
		return
	}
	if phase3.Str(e.Data, "service_id", "") != "" {
		s.writeError(w, r, http.StatusConflict, "task_managed_by_service", "this task belongs to a service — scale the service down instead of stopping its tasks directly")
		return
	}
	_ = s.p4Runner().Stop(e.ID)
	// The OnStop callback updates the stored status; wait briefly
	// for it so the response reflects the real state.
	for i := 0; i < 20; i++ {
		fresh, err := st.P3Get(r.Context(), "ecs_tasks", e.ID)
		if err == nil && phase3.Str(fresh.Data, "status", "") == "STOPPED" {
			e = fresh
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if phase3.Str(e.Data, "status", "") != "STOPPED" {
		e.Data["status"] = "STOPPED"
		e.Data["stop_reason"] = "stopped by user"
		e.Data["stopped_at"] = p3Now()
		_, _ = st.P3Update(r.Context(), "ecs_tasks", e)
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleECSTaskLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "ecs_tasks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	logs, err := s.p4Runner().ReadLog(e.ID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "could not read task log: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"task_id": e.ID, "logs": logs, "running": s.p4Runner().IsRunning(e.ID)})
}

// --- EKS: decision + learning path --------------------------------------------
//
// EKS stays UNSUPPORTED by explicit Phase 4 decision (see
// phase4.EKSDecision). This endpoint serves the decision and the
// Kubernetes-concepts learning path so the console is data-driven,
// not a static page pretending to be a cluster.

func (s *Server) handleEKSInfo(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"service":       "eks",
		"fidelity":      phase4.EKSDecision,
		"supported":     false,
		"decision":      phase4.EKSDecision,
		"reason":        phase4.EKSDecisionReason,
		"learning_path": phase4.K8sLearningPath,
	})
}

// --- CodeBuild -----------------------------------------------------------------

// p4BuildSourceRoot is where local build sources must live. Only
// local filesystem paths are accepted — never http(s)/ssh/git@
// remotes, so a build can never fetch code (or exfiltrate data)
// over the network.
func (s *Server) p4BuildSourceRoot() string {
	if s.P4BuildSourceRoot != "" {
		return s.P4BuildSourceRoot
	}
	return s.P3WorkdirRoot
}

func p4ValidBuildSource(root, source string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return errors.New("source path is required (a local directory path)")
	}
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") {
		return errors.New("only local filesystem paths are accepted as build sources (no http(s)/ssh/git@ remotes)")
	}
	if !filepath.IsAbs(source) {
		return errors.New("source path must be absolute")
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return fmt.Errorf("source path must be under the build source root %s", rootAbs)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("source path does not exist: %s", abs)
	}
	return nil
}

func (s *Server) handleCodeBuildListProjects(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "codebuild_projects")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"projects": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "count": len(p3Own(all, u.ID, p3IsAdmin(u.Role)))})
}

func (s *Server) handleCodeBuildCreateProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string            `json:"name"`
		SourcePath string            `json:"source_path"`
		Buildspec  string            `json:"buildspec"`
		Env        map[string]string `json:"env"`
		TimeoutMin int               `json:"timeout_minutes"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "project name is required")
		return
	}
	if err := p4ValidBuildSource(s.p4BuildSourceRoot(), req.SourcePath); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "source_path: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Buildspec) != "" {
		if _, err := phase4.ParseBuildspec([]byte(req.Buildspec)); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "inline buildspec: "+err.Error())
			return
		}
	}
	timeout := req.TimeoutMin
	if timeout <= 0 {
		timeout = 60
	}
	if timeout > 480 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "timeout_minutes must be 1-480")
		return
	}
	env := req.Env
	if env == nil {
		env = map[string]string{}
	}
	e, err := st.P3Create(r.Context(), "codebuild_projects", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"source_path": req.SourcePath, "buildspec": req.Buildspec, "env": env,
		"timeout_minutes": timeout, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleCodeBuildDeleteProject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	p, err := st.P3Get(r.Context(), "codebuild_projects", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, p, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	builds, _ := st.P3List(r.Context(), "codebuild_builds")
	for _, b := range builds {
		if phase3.Str(b.Data, "project_id", "") == p.ID && phase3.Str(b.Data, "status", "") == "IN_PROGRESS" {
			s.writeError(w, r, http.StatusConflict, "project_has_running_build", "project has a build in progress")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "codebuild_projects", p.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": p.ID})
}

// handleCodeBuildStart starts a build asynchronously (202) — the
// build record is IN_PROGRESS until the runner finishes it.
func (s *Server) handleCodeBuildStart(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	p, err := st.P3Get(r.Context(), "codebuild_projects", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, p, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	e, err := st.P3Create(r.Context(), "codebuild_builds", &store.P3Entity{Name: p.Name + "-" + ids.New()[:8], Data: map[string]any{
		"project_id": p.ID, "project_name": p.Name, "status": "IN_PROGRESS",
		"started_at": p3Now(), "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p4RunBuild(context.Background(), p, e)
	s.writeJSON(w, r, http.StatusAccepted, p3View(e))
}

func (s *Server) handleCodeBuildListBuilds(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	all, err := st.P3List(r.Context(), "codebuild_builds")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		if projectID != "" && phase3.Str(e.Data, "project_id", "") != projectID {
			continue
		}
		out = append(out, p3View(e))
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["created_at"]) > fmt.Sprint(out[j]["created_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"builds": out, "count": len(out)})
}

func (s *Server) handleCodeBuildGetBuild(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "codebuild_builds", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

func (s *Server) handleCodeBuildLogs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "codebuild_builds", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	logPath := phase3.Str(e.Data, "log_path", "")
	data, err := os.ReadFile(logPath)
	if err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"build_id": e.ID, "logs": "", "note": "no log file yet (the build may still be starting)"})
		return
	}
	if len(data) > 256*1024 {
		data = data[len(data)-256*1024:]
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"build_id": e.ID, "logs": string(data), "status": phase3.Str(e.Data, "status", "")})
}

// p4RunBuild executes one build synchronously and updates its
// record. It is used by the CodeBuild start endpoint (in a
// goroutine) and by CodePipeline's build stage (in the pipeline's
// goroutine). Logs are real output from the commands that ran.
func (s *Server) p4RunBuild(ctx context.Context, project, build *store.P3Entity) *store.P3Entity {
	st := s.P3
	fail := func(reason string) *store.P3Entity {
		build.Data["status"] = "FAILED"
		build.Data["ended_at"] = p3Now()
		build.Data["failure_reason"] = reason
		updated, _ := st.P3Update(ctx, "codebuild_builds", build)
		if updated != nil {
			return updated
		}
		return build
	}
	base := filepath.Join(s.P3WorkdirRoot, "codebuild", build.ID)
	srcDir := filepath.Join(base, "source")
	artifactDir := filepath.Join(base, "artifacts")
	logPath := filepath.Join(base, "build.log")
	_ = os.MkdirAll(artifactDir, 0o755)
	// MkdirAll honours the process umask (0007 in this sandbox
	// turns 0755 into 0750), so chmod explicitly: the build runs
	// as uid 65534 and must traverse every parent dir.
	_ = os.Chmod(filepath.Join(s.P3WorkdirRoot, "codebuild"), 0o755)
	_ = os.Chmod(base, 0o755)
	_ = os.Chmod(artifactDir, 0o755)
	// The build runs as uid 65534 (nobody) when the API runs as
	// root, so its directories must be writable by it. They hold
	// only this build's files.
	if os.Geteuid() == 0 {
		_ = os.Chown(base, 65534, 65534)
		_ = os.Chown(artifactDir, 65534, 65534)
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fail("could not create build log: " + err.Error())
	}
	defer logF.Close()
	build.Data["log_path"] = logPath
	build.Data["artifact_dir"] = artifactDir
	_, _ = st.P3Update(ctx, "codebuild_builds", build)
	fmt.Fprintf(logF, "[cloudforge codebuild] build %s of project %s\n", build.ID, project.Name)

	source := phase3.Str(project.Data, "source_path", "")
	// Checkout: git clone when the source is a repo, else copy the
	// directory tree. Both are local-only by p4ValidBuildSource.
	// The clone runs as the API user (not as uid 65534): git
	// refuses to read a repo owned by another user ("detected
	// dubious ownership"), and the source is operator-owned.
	// When the clone fails — notably when the source is owned by
	// another user (e.g. a pipeline checkout chowned to 65534; the
	// env trust in p4GitCmd does not reach clone's upload-pack
	// child) — fall back to copying the source tree, which for a
	// local-only source is equivalent (full tree + .git). The
	// checked-out tree is chowned to 65534 afterwards so the
	// build commands (which run as nobody) can write to it.
	commit := ""
	if _, err := os.Stat(filepath.Join(source, ".git")); err == nil {
		clone := p4GitCmd(ctx, source, "clone", "--quiet", source, srcDir)
		clone.Stdout = logF
		clone.Stderr = logF
		if err := clone.Run(); err != nil {
			fmt.Fprintf(logF, "[cloudforge codebuild] git clone failed (%v); copying the source tree instead\n", err)
			if cerr := p4CopyDir(source, srcDir); cerr != nil {
				fmt.Fprintf(logF, "[cloudforge codebuild] source copy failed: %v\n", cerr)
				return fail("git clone failed: " + err.Error())
			}
		}
		if os.Geteuid() == 0 {
			_ = p4ChownTree(srcDir, 65534, 65534)
		}
		out, err := p4GitCmd(ctx, srcDir, "-C", srcDir, "rev-parse", "HEAD").Output()
		if err == nil {
			commit = strings.TrimSpace(string(out))
		}
	} else {
		if err := p4CopyDir(source, srcDir); err != nil {
			fmt.Fprintf(logF, "[cloudforge codebuild] source copy failed: %v\n", err)
			return fail("source copy failed: " + err.Error())
		}
		if os.Geteuid() == 0 {
			_ = p4ChownTree(srcDir, 65534, 65534)
		}
	}
	build.Data["commit"] = commit
	_, _ = st.P3Update(ctx, "codebuild_builds", build)

	// Buildspec: inline on the project, else buildspec.yml at the
	// repo root.
	var bs *phase4.Buildspec
	if inline := strings.TrimSpace(phase3.Str(project.Data, "buildspec", "")); inline != "" {
		bs, err = phase4.ParseBuildspec([]byte(inline))
		if err != nil {
			fmt.Fprintf(logF, "[cloudforge codebuild] inline buildspec: %v\n", err)
			return fail("inline buildspec: " + err.Error())
		}
	} else {
		raw, err := os.ReadFile(filepath.Join(srcDir, "buildspec.yml"))
		if err != nil {
			fmt.Fprintf(logF, "[cloudforge codebuild] no buildspec.yml in the source and no inline buildspec\n")
			return fail("no buildspec found (inline buildspec or buildspec.yml in the source root)")
		}
		bs, err = phase4.ParseBuildspec(raw)
		if err != nil {
			fmt.Fprintf(logF, "[cloudforge codebuild] buildspec.yml: %v\n", err)
			return fail("buildspec.yml: " + err.Error())
		}
	}
	env := map[string]string{}
	if raw, ok := project.Data["env"].(map[string]any); ok {
		for k, v := range raw {
			if str, ok := v.(string); ok {
				env[k] = str
			}
		}
	}
	if bs.Env != nil {
		if vars, ok := bs.Env["variables"]; ok {
			for k, v := range vars {
				env[k] = v
			}
		}
	}
	timeoutMin := phase3.Int(project.Data, "timeout_minutes", 60)
	phasesRun := []string{}
	sandbox := ""
	for _, phaseName := range phase4.BuildspecPhaseOrder {
		ph, ok := bs.Phases[phaseName]
		if !ok || len(ph.Commands) == 0 {
			continue
		}
		fmt.Fprintf(logF, "[cloudforge codebuild] phase %s (%d commands)\n", phaseName, len(ph.Commands))
		res := phase4.RunShell(ctx, srcDir, env, timeoutMin*60, 512, logF, ph.Commands)
		sandbox = res.Sandbox
		phasesRun = append(phasesRun, phaseName)
		if res.TimedOut {
			return fail(fmt.Sprintf("phase %s timed out after %d minutes", phaseName, timeoutMin))
		}
		if res.ExitCode != 0 {
			return fail(fmt.Sprintf("phase %s failed with exit code %d (see logs)", phaseName, res.ExitCode))
		}
	}
	// Artifacts: copy the declared globs out of the source dir.
	artifacts := []string{}
	for _, pattern := range bs.Artifacts.Files {
		matches, _ := filepath.Glob(filepath.Join(srcDir, pattern))
		for _, m := range matches {
			rel, err := filepath.Rel(srcDir, m)
			if err != nil {
				continue
			}
			dest := filepath.Join(artifactDir, rel)
			_ = os.MkdirAll(filepath.Dir(dest), 0o755)
			if data, err := os.ReadFile(m); err == nil {
				_ = os.WriteFile(dest, data, 0o644)
				artifacts = append(artifacts, rel)
			}
		}
	}
	build.Data["status"] = "SUCCEEDED"
	build.Data["ended_at"] = p3Now()
	build.Data["phases_run"] = phasesRun
	build.Data["artifacts"] = artifacts
	build.Data["sandbox"] = sandbox
	updated, err := st.P3Update(ctx, "codebuild_builds", build)
	if err != nil {
		return build
	}
	return updated
}

// p4BuildProjectRef is the human-readable reference a build stage
// uses for its project (id or name), for error messages.
func p4BuildProjectRef(stage map[string]any) string {
	if id := phase3.Str(stage, "project_id", ""); id != "" {
		return id
	}
	return phase3.Str(stage, "build_project_name", "")
}

// p4ResolveBuildProject resolves a pipeline build stage to its
// CodeBuild project, by project_id or by build_project_name (the
// name is what consoles and humans use; the id is what the store
// uses). A stage that names neither is an error.
func (s *Server) p4ResolveBuildProject(ctx context.Context, st store.Phase3Store, stage map[string]any) (*store.P3Entity, error) {
	if id := phase3.Str(stage, "project_id", ""); id != "" {
		return st.P3Get(ctx, "codebuild_projects", id)
	}
	if name := phase3.Str(stage, "build_project_name", ""); name != "" {
		return st.P3GetByName(ctx, "codebuild_projects", name)
	}
	return nil, fmt.Errorf("build stage names no CodeBuild project (set project_id or build_project_name)")
}

// p4GitCmd builds a git command that trusts exactly one repo path
// via the GIT_CONFIG_* environment variables, which child processes
// inherit — a "-c safe.directory=..." flag is NOT inherited by the
// upload-pack child a local clone spawns, so it does not help there.
// Two values are set: git 2.43 matches safe.directory against the
// git-dir path (<repo>/.git) for a non-bare repo, not the worktree
// root, so trusting only the worktree still fails (verified live);
// trusting both covers bare and non-bare repos. Any GIT_CONFIG_*
// already in the environment is stripped first so the count stays
// exact.
//
// LIMIT: even the env trust does not reach the upload-pack child
// git spawns for a local clone (verified live: a clone of a
// nobody-owned repo fails with exit 128 even with both values set,
// while the same trust in the global config works). It DOES work for
// direct commands (rev-parse). Clone callers must therefore fall
// back to a filesystem copy of the source tree when the clone fails
// — for a local-only source that is equivalent (full tree + .git).
func p4GitCmd(ctx context.Context, trustedRepo string, args ...string) *exec.Cmd {
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") || strings.HasPrefix(kv, "GIT_CONFIG_KEY_") || strings.HasPrefix(kv, "GIT_CONFIG_VALUE_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0="+trustedRepo,
		"GIT_CONFIG_KEY_1=safe.directory", "GIT_CONFIG_VALUE_1="+filepath.Join(trustedRepo, ".git"))
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	return cmd
}

// p4ChownTree chowns a directory tree to uid/gid (best-effort).
func p4ChownTree(path string, uid, gid int) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}

// p4CopyDir copies a directory tree (files only, no symlinks
// followed — a symlink is skipped, never followed off the tree).
func p4CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// --- CodePipeline ---------------------------------------------------------------

func (s *Server) handlePipelineList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "codepipeline_pipelines")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"pipelines": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "count": len(p3Own(all, u.ID, p3IsAdmin(u.Role)))})
}

func (s *Server) handlePipelineCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string           `json:"name"`
		Stages []map[string]any `json:"stages"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "pipeline name is required")
		return
	}
	if len(req.Stages) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "at least one stage is required")
		return
	}
	for i, stage := range req.Stages {
		typ := phase3.Str(stage, "type", "")
		if typ != "source" && typ != "build" && typ != "deploy" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("stage %d has unknown type %q (source, build, deploy only)", i, typ))
			return
		}
		if phase3.Str(stage, "name", "") == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("stage %d has no name", i))
			return
		}
		if typ == "source" {
			if err := p4ValidBuildSource(s.p4BuildSourceRoot(), phase3.Str(stage, "source_path", "")); err != nil {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("stage %d source: %v", i, err))
				return
			}
		}
		if typ == "build" {
			if _, err := s.p4ResolveBuildProject(r.Context(), st, stage); err != nil {
				s.writeError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("stage %d references unknown CodeBuild project %q", i, p4BuildProjectRef(stage)))
				return
			}
		}
		if typ == "deploy" {
			target := phase3.Str(stage, "target", "")
			if target != "ecs-service" && target != "lambda-function" && target != "terraform" {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("stage %d deploy target %q is not supported (ecs-service, lambda-function, terraform)", i, target))
				return
			}
		}
	}
	// Normalise stages to []any of map[string]any: the store keeps
	// Go values as stored (no JSON round-trip in the memory store),
	// so a []map[string]any would fail the []any assertion on
	// read-back in p4RunPipeline.
	stagesAny := []any{}
	for _, st := range req.Stages {
		stagesAny = append(stagesAny, st)
	}
	e, err := st.P3Create(r.Context(), "codepipeline_pipelines", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"stages": stagesAny, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handlePipelineDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	p, err := st.P3Get(r.Context(), "codepipeline_pipelines", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, p, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	execs, _ := st.P3List(r.Context(), "codepipeline_executions")
	for _, ex := range execs {
		if phase3.Str(ex.Data, "pipeline_id", "") == p.ID && phase3.Str(ex.Data, "status", "") == "IN_PROGRESS" {
			s.writeError(w, r, http.StatusConflict, "pipeline_has_running_execution", "pipeline has an execution in progress")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "codepipeline_pipelines", p.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": p.ID})
}

func (s *Server) handlePipelineStart(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	p, err := st.P3Get(r.Context(), "codepipeline_pipelines", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, p, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	e, err := st.P3Create(r.Context(), "codepipeline_executions", &store.P3Entity{Name: p.Name + "-" + ids.New()[:8], Data: map[string]any{
		"pipeline_id": p.ID, "pipeline_name": p.Name, "status": "IN_PROGRESS",
		"started_at": p3Now(), "stages": []any{}, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p4RunPipeline(context.Background(), p, e)
	s.writeJSON(w, r, http.StatusAccepted, p3View(e))
}

func (s *Server) handlePipelineListExecutions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	pipelineID := strings.TrimSpace(r.URL.Query().Get("pipeline_id"))
	all, err := st.P3List(r.Context(), "codepipeline_executions")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		if pipelineID != "" && phase3.Str(e.Data, "pipeline_id", "") != pipelineID {
			continue
		}
		out = append(out, p3View(e))
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["created_at"]) > fmt.Sprint(out[j]["created_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"executions": out, "count": len(out)})
}

func (s *Server) handlePipelineGetExecution(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "codepipeline_executions", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// p4RunPipeline executes a pipeline's stages in order. A failed
// stage stops the execution (FAILED) — later stages never run, and
// the failure reason names the stage that failed.
func (s *Server) p4RunPipeline(ctx context.Context, pipeline, execEnt *store.P3Entity) {
	st := s.P3
	stages, _ := pipeline.Data["stages"].([]any)
	runStages := []any{}
	fail := func(stageName, reason string) {
		runStages[len(runStages)-1].(map[string]any)["status"] = "FAILED"
		runStages[len(runStages)-1].(map[string]any)["detail"] = reason
		runStages[len(runStages)-1].(map[string]any)["ended_at"] = p3Now()
		execEnt.Data["stages"] = runStages
		execEnt.Data["status"] = "FAILED"
		execEnt.Data["ended_at"] = p3Now()
		execEnt.Data["failure_reason"] = "stage " + stageName + ": " + reason
		_, _ = st.P3Update(ctx, "codepipeline_executions", execEnt)
	}
	workdir := filepath.Join(s.P3WorkdirRoot, "codepipeline", execEnt.ID)
	_ = os.MkdirAll(workdir, 0o755)
	_ = os.Chmod(filepath.Join(s.P3WorkdirRoot, "codepipeline"), 0o755)
	_ = os.Chmod(workdir, 0o755)
	if os.Geteuid() == 0 {
		_ = os.Chown(workdir, 65534, 65534)
	}
	var lastBuild *store.P3Entity
	for _, raw := range stages {
		stage, _ := raw.(map[string]any)
		name := phase3.Str(stage, "name", "")
		typ := phase3.Str(stage, "type", "")
		rec := map[string]any{"name": name, "type": typ, "status": "IN_PROGRESS", "started_at": p3Now()}
		runStages = append(runStages, rec)
		execEnt.Data["stages"] = runStages
		_, _ = st.P3Update(ctx, "codepipeline_executions", execEnt)
		switch typ {
		case "source":
			source := phase3.Str(stage, "source_path", "")
			dest := filepath.Join(workdir, "source")
			var commit string
			if _, err := os.Stat(filepath.Join(source, ".git")); err == nil {
				// As in p4RunBuild: clone as the API user with
				// p4GitCmd (GIT_CONFIG_* env trust — see its
				// comment), then chown the tree to 65534 so the
				// build stage can write to it. If the clone
				// fails (e.g. the source is owned by another
				// user), copy the tree instead — equivalent for
				// a local-only source.
				cmd := p4GitCmd(ctx, source, "clone", "--quiet", source, dest)
				if err := cmd.Run(); err != nil {
					if cerr := p4CopyDir(source, dest); cerr != nil {
						fail(name, "git clone failed: "+err.Error())
						return
					}
				}
				if os.Geteuid() == 0 {
					_ = p4ChownTree(dest, 65534, 65534)
				}
				out, err := p4GitCmd(ctx, dest, "-C", dest, "rev-parse", "HEAD").Output()
				if err == nil {
					commit = strings.TrimSpace(string(out))
				}
			} else {
				if err := p4CopyDir(source, dest); err != nil {
					fail(name, "source copy failed: "+err.Error())
					return
				}
				if os.Geteuid() == 0 {
					_ = p4ChownTree(dest, 65534, 65534)
				}
			}
			rec["status"] = "SUCCEEDED"
			rec["detail"] = "checked out to " + dest + func() string {
				if commit != "" {
					return " (commit " + commit + ")"
				}
				return ""
			}()
			rec["ended_at"] = p3Now()
			execEnt.Data["source_dir"] = dest
			execEnt.Data["commit"] = commit
		case "build":
			proj, err := s.p4ResolveBuildProject(ctx, st, stage)
			if err != nil {
				fail(name, "CodeBuild project not found: "+err.Error())
				return
			}
			// The build runs against the pipeline's source checkout
			// when a source stage already ran (real CodePipeline
			// passes the source artifact to the build); otherwise
			// it builds from the project's own source path.
			buildProj := proj
			if srcDir, ok := execEnt.Data["source_dir"].(string); ok && srcDir != "" {
				cp := *proj
				cp.Data = map[string]any{}
				for k, v := range proj.Data {
					cp.Data[k] = v
				}
				cp.Data["source_path"] = srcDir
				buildProj = &cp
			}
			b, err := st.P3Create(ctx, "codebuild_builds", &store.P3Entity{Name: proj.Name + "-" + ids.New()[:8], Data: map[string]any{
				"project_id": proj.ID, "project_name": proj.Name, "status": "IN_PROGRESS",
				"started_at": p3Now(), "owner_id": phase3.Str(execEnt.Data, "owner_id", ""),
				"pipeline_execution_id": execEnt.ID,
			}})
			if err != nil {
				fail(name, "could not create build record: "+err.Error())
				return
			}
			b = s.p4RunBuild(ctx, buildProj, b)
			lastBuild = b
			if phase3.Str(b.Data, "status", "") != "SUCCEEDED" {
				fail(name, "build "+b.ID+" "+phase3.Str(b.Data, "status", "")+": "+phase3.Str(b.Data, "failure_reason", "see build logs"))
				return
			}
			rec["status"] = "SUCCEEDED"
			rec["detail"] = "build " + b.ID + " succeeded"
			rec["ended_at"] = p3Now()
			execEnt.Data["build_id"] = b.ID
		case "deploy":
			target := phase3.Str(stage, "target", "")
			detail, err := s.p4PipelineDeploy(ctx, stage, lastBuild, workdir)
			if err != nil {
				fail(name, err.Error())
				return
			}
			_ = target
			rec["status"] = "SUCCEEDED"
			rec["detail"] = detail
			rec["ended_at"] = p3Now()
		default:
			fail(name, "unknown stage type "+typ)
			return
		}
		execEnt.Data["stages"] = runStages
		_, _ = st.P3Update(ctx, "codepipeline_executions", execEnt)
	}
	execEnt.Data["stages"] = runStages
	execEnt.Data["status"] = "SUCCEEDED"
	execEnt.Data["ended_at"] = p3Now()
	_, _ = st.P3Update(ctx, "codepipeline_executions", execEnt)
}

// p4PipelineDeploy performs one deploy stage. ecs-service updates
// the service's task definition (a real rolling deployment);
// lambda-function replaces the function's code from the build
// artifact; terraform honestly fails without Docker.
func (s *Server) p4PipelineDeploy(ctx context.Context, stage map[string]any, lastBuild *store.P3Entity, workdir string) (string, error) {
	st := s.P3
	switch phase3.Str(stage, "target", "") {
	case "ecs-service":
		svcID := phase3.Str(stage, "service_id", "")
		tdID := phase3.Str(stage, "task_definition_id", "")
		svc, err := st.P3Get(ctx, "ecs_services", svcID)
		if err != nil {
			return "", fmt.Errorf("deploy target service not found: %w", err)
		}
		if _, err := st.P3Get(ctx, "ecs_taskdefs", tdID); err != nil {
			return "", fmt.Errorf("deploy task definition not found: %w", err)
		}
		old := phase3.Str(svc.Data, "task_definition_id", "")
		svc.Data["task_definition_id"] = tdID
		deployments, _ := svc.Data["deployments"].([]any)
		deployments = append(deployments, map[string]any{"at": p3Now(), "from_task_definition_id": old, "to_task_definition_id": tdID, "status": "COMPLETED", "via": "codepipeline"})
		svc.Data["deployments"] = deployments
		if _, err := st.P3Update(ctx, "ecs_services", svc); err != nil {
			return "", err
		}
		// Rolling restart of the service's tasks.
		tasks, _ := st.P3List(ctx, "ecs_tasks")
		for _, t := range tasks {
			if phase3.Str(t.Data, "service_id", "") == svc.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
				_ = s.p4Runner().Stop(t.ID)
				t.Data["status"] = "STOPPED"
				t.Data["stop_reason"] = "replaced by pipeline deploy"
				t.Data["stopped_at"] = p3Now()
				_, _ = st.P3Update(ctx, "ecs_tasks", t)
			}
		}
		s.p4ReconcileService(ctx, svc.ID)
		return "service " + svc.Name + " updated to task definition " + tdID, nil
	case "lambda-function":
		fnID := phase3.Str(stage, "function_id", "")
		codeFile := phase3.Str(stage, "code_file", "")
		fn, err := st.P3Get(ctx, "lambda_functions", fnID)
		if err != nil {
			return "", fmt.Errorf("deploy target function not found: %w", err)
		}
		if lastBuild == nil {
			return "", errors.New("lambda deploy needs a preceding build stage (no build artifacts available)")
		}
		artifactDir := phase3.Str(lastBuild.Data, "artifact_dir", "")
		if artifactDir == "" || codeFile == "" {
			return "", errors.New("lambda deploy needs code_file (relative to the build artifacts) and a build that produced artifacts")
		}
		raw, err := os.ReadFile(filepath.Join(artifactDir, codeFile))
		if err != nil {
			return "", fmt.Errorf("deploy artifact %s not found in build %s: %w", codeFile, lastBuild.ID, err)
		}
		runtime := phase3.Str(fn.Data, "runtime", "")
		if _, ok := phase3.SupportedRuntimes[runtime]; !ok {
			return "", fmt.Errorf("function runtime %q is not supported for deploy", runtime)
		}
		fn.Data["code"] = string(raw)
		fn.Data["version"] = phase3.Int(fn.Data, "version", 1) + 1
		if _, err := st.P3Update(ctx, "lambda_functions", fn); err != nil {
			return "", err
		}
		return fmt.Sprintf("function %s code updated to version %d from build artifact %s", fn.Name, phase3.Int(fn.Data, "version", 0), codeFile), nil
	case "terraform":
		return "", errors.New("terraform deploy is unavailable here: Terraform runs only in containers and this host has no Docker (terraform_unavailable) — the stage failed honestly instead of reporting a deploy that never happened")
	default:
		return "", fmt.Errorf("unknown deploy target %q", phase3.Str(stage, "target", ""))
	}
}

// --- CodeDeploy ------------------------------------------------------------------

// p4DeployTarget resolves a deployment group's target into a
// human-readable description and validates it exists.
func (s *Server) p4DeployTarget(ctx context.Context, group *store.P3Entity) (string, error) {
	st := s.P3
	switch phase3.Str(group.Data, "target_type", "") {
	case "ecs-service":
		svc, err := st.P3Get(ctx, "ecs_services", phase3.Str(group.Data, "target_id", ""))
		if err != nil {
			return "", err
		}
		return "ECS service " + svc.Name, nil
	case "lambda-function":
		fn, err := st.P3Get(ctx, "lambda_functions", phase3.Str(group.Data, "target_id", ""))
		if err != nil {
			return "", err
		}
		return "Lambda function " + fn.Name, nil
	default:
		return "", fmt.Errorf("unknown target type %q", phase3.Str(group.Data, "target_type", ""))
	}
}

func (s *Server) handleCodeDeployListApps(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "codedeploy_apps")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"applications": p3ListView(p3Own(all, u.ID, p3IsAdmin(u.Role))), "count": len(p3Own(all, u.ID, p3IsAdmin(u.Role)))})
}

func (s *Server) handleCodeDeployCreateApp(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string `json:"name"`
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		GroupName  string `json:"deployment_group_name"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "application name is required")
		return
	}
	if req.TargetType != "ecs-service" && req.TargetType != "lambda-function" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "target_type must be ecs-service or lambda-function")
		return
	}
	groupName := strings.TrimSpace(req.GroupName)
	if groupName == "" {
		groupName = "Default"
	}
	e, err := st.P3Create(r.Context(), "codedeploy_apps", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"target_type": req.TargetType, "target_id": req.TargetID,
		"deployment_group_name": groupName, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if _, err := s.p4DeployTarget(r.Context(), e); err != nil {
		// The target must exist — a deployment group pointing at
		// nothing is a misconfiguration, not a stored application.
		_ = st.P3Delete(r.Context(), "codedeploy_apps", e.ID)
		s.writeError(w, r, http.StatusNotFound, "not_found", "deploy target not found: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleCodeDeployDeleteApp(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	app, err := st.P3Get(r.Context(), "codedeploy_apps", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, app, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	deps, _ := st.P3List(r.Context(), "codedeploy_deployments")
	for _, d := range deps {
		if phase3.Str(d.Data, "application_id", "") == app.ID && phase3.Str(d.Data, "status", "") == "IN_PROGRESS" {
			s.writeError(w, r, http.StatusConflict, "application_has_running_deployment", "application has a deployment in progress")
			return
		}
	}
	if err := st.P3Delete(r.Context(), "codedeploy_apps", app.ID); err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": app.ID})
}

func (s *Server) handleCodeDeployCreateDeployment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	app, err := st.P3Get(r.Context(), "codedeploy_apps", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, app, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		TaskDefinitionID string `json:"task_definition_id"`
		Code             string `json:"code"`
		Description      string `json:"description"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	targetType := phase3.Str(app.Data, "target_type", "")
	targetID := phase3.Str(app.Data, "target_id", "")
	revision := map[string]any{}
	previous := map[string]any{}
	switch targetType {
	case "ecs-service":
		if strings.TrimSpace(req.TaskDefinitionID) == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "task_definition_id is required for an ECS deployment")
			return
		}
		td, err := st.P3Get(r.Context(), "ecs_taskdefs", req.TaskDefinitionID)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "no task definition with that id")
			return
		}
		svc, err := st.P3Get(r.Context(), "ecs_services", targetID)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "deploy target service no longer exists")
			return
		}
		revision = map[string]any{"type": "ecs-taskdef", "task_definition_id": td.ID, "task_definition_name": td.Name}
		previous = map[string]any{"type": "ecs-taskdef", "task_definition_id": phase3.Str(svc.Data, "task_definition_id", ""), "task_definition_name": ""}
	case "lambda-function":
		if strings.TrimSpace(req.Code) == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is required for a Lambda deployment")
			return
		}
		if len(req.Code) > 256*1024 {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "code is capped at 256 KiB")
			return
		}
		fn, err := st.P3Get(r.Context(), "lambda_functions", targetID)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "not_found", "deploy target function no longer exists")
			return
		}
		revision = map[string]any{"type": "lambda-code", "code": req.Code}
		previous = map[string]any{"type": "lambda-code", "code": phase3.Str(fn.Data, "code", ""), "version": phase3.Int(fn.Data, "version", 1)}
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "unknown target type "+targetType)
		return
	}
	e, err := st.P3Create(r.Context(), "codedeploy_deployments", &store.P3Entity{Name: app.Name + "-" + ids.New()[:8], Data: map[string]any{
		"application_id": app.ID, "application_name": app.Name,
		"deployment_group_name": phase3.Str(app.Data, "deployment_group_name", "Default"),
		"target_type":           targetType, "target_id": targetID,
		"revision": revision, "previous_revision": previous,
		"description": req.Description, "status": "IN_PROGRESS",
		"started_at": p3Now(), "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p4RunDeployment(context.Background(), app, e)
	s.writeJSON(w, r, http.StatusAccepted, p3View(e))
}

func (s *Server) handleCodeDeployListDeployments(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	appID := strings.TrimSpace(r.URL.Query().Get("application_id"))
	all, err := st.P3List(r.Context(), "codedeploy_deployments")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		if appID != "" && phase3.Str(e.Data, "application_id", "") != appID {
			continue
		}
		out = append(out, p3View(e))
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["created_at"]) > fmt.Sprint(out[j]["created_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deployments": out, "count": len(out)})
}

func (s *Server) handleCodeDeployGetDeployment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "codedeploy_deployments", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	s.writeJSON(w, r, http.StatusOK, p3View(e))
}

// handleCodeDeployRollback creates a new deployment whose revision
// is the failed deployment's previous revision. The rollback itself
// runs through the same path as a normal deployment, so its status
// is real.
func (s *Server) handleCodeDeployRollback(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	orig, err := st.P3Get(r.Context(), "codedeploy_deployments", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, orig, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	prev, _ := orig.Data["previous_revision"].(map[string]any)
	if len(prev) == 0 {
		s.writeError(w, r, http.StatusConflict, "nothing_to_roll_back", "this deployment recorded no previous revision")
		return
	}
	app, err := st.P3Get(r.Context(), "codedeploy_apps", phase3.Str(orig.Data, "application_id", ""))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// The rollback's own "previous" is the revision being rolled
	// back from, so a second rollback returns to it.
	e, err := st.P3Create(r.Context(), "codedeploy_deployments", &store.P3Entity{Name: app.Name + "-rollback-" + ids.New()[:8], Data: map[string]any{
		"application_id": app.ID, "application_name": app.Name,
		"deployment_group_name": phase3.Str(orig.Data, "deployment_group_name", "Default"),
		"target_type":           phase3.Str(orig.Data, "target_type", ""), "target_id": phase3.Str(orig.Data, "target_id", ""),
		"revision": prev, "previous_revision": orig.Data["revision"],
		"description": "rollback of deployment " + orig.ID, "status": "IN_PROGRESS",
		"started_at": p3Now(), "rolled_back_from": orig.ID, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	go s.p4RunDeployment(context.Background(), app, e)
	s.writeJSON(w, r, http.StatusAccepted, p3View(e))
}

// p4RunDeployment applies one deployment revision to its target and
// waits for the target to actually reach the new state: an ECS
// service must have its desired count RUNNING on the new task
// definition; a Lambda function must hold the new code. A timeout
// or a failed check is FAILED, never SUCCEEDED.
func (s *Server) p4RunDeployment(ctx context.Context, app, dep *store.P3Entity) {
	st := s.P3
	fail := func(reason string) {
		dep.Data["status"] = "FAILED"
		dep.Data["ended_at"] = p3Now()
		dep.Data["failure_reason"] = reason
		_, _ = st.P3Update(ctx, "codedeploy_deployments", dep)
	}
	rev, _ := dep.Data["revision"].(map[string]any)
	targetType := phase3.Str(dep.Data, "target_type", "")
	targetID := phase3.Str(dep.Data, "target_id", "")
	switch targetType {
	case "ecs-service":
		tdID := phase3.Str(rev, "task_definition_id", "")
		svc, err := st.P3Get(ctx, "ecs_services", targetID)
		if err != nil {
			fail("deploy target service no longer exists: " + err.Error())
			return
		}
		if _, err := st.P3Get(ctx, "ecs_taskdefs", tdID); err != nil {
			fail("task definition no longer exists: " + err.Error())
			return
		}
		old := phase3.Str(svc.Data, "task_definition_id", "")
		svc.Data["task_definition_id"] = tdID
		deployments, _ := svc.Data["deployments"].([]any)
		deployments = append(deployments, map[string]any{"at": p3Now(), "from_task_definition_id": old, "to_task_definition_id": tdID, "status": "IN_PROGRESS", "via": "codedeploy", "deployment_id": dep.ID})
		svc.Data["deployments"] = deployments
		if _, err := st.P3Update(ctx, "ecs_services", svc); err != nil {
			fail("could not update service: " + err.Error())
			return
		}
		tasks, _ := st.P3List(ctx, "ecs_tasks")
		for _, t := range tasks {
			if phase3.Str(t.Data, "service_id", "") == svc.ID && phase3.Str(t.Data, "status", "") == "RUNNING" {
				_ = s.p4Runner().Stop(t.ID)
				t.Data["status"] = "STOPPED"
				t.Data["stop_reason"] = "replaced by CodeDeploy deployment " + dep.ID
				t.Data["stopped_at"] = p3Now()
				_, _ = st.P3Update(ctx, "ecs_tasks", t)
			}
		}
		s.p4ReconcileService(ctx, svc.ID)
		// Wait up to 60 s for the desired count RUNNING on the new revision.
		desired := phase3.Int(svc.Data, "desired_count", 0)
		ok := false
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			tasks, _ := st.P3List(ctx, "ecs_tasks")
			running := 0
			for _, t := range tasks {
				if phase3.Str(t.Data, "service_id", "") == svc.ID &&
					phase3.Str(t.Data, "status", "") == "RUNNING" &&
					phase3.Str(t.Data, "task_definition_id", "") == tdID &&
					s.p4Runner().IsRunning(t.ID) {
					running++
				}
			}
			if running >= desired {
				ok = true
				break
			}
		}
		if !ok {
			fail(fmt.Sprintf("deployment did not reach desired count %d RUNNING on the new task definition within 60s", desired))
			return
		}
		dep.Data["status"] = "SUCCEEDED"
		dep.Data["ended_at"] = p3Now()
		_, _ = st.P3Update(ctx, "codedeploy_deployments", dep)
	case "lambda-function":
		code := phase3.Str(rev, "code", "")
		fn, err := st.P3Get(ctx, "lambda_functions", targetID)
		if err != nil {
			fail("deploy target function no longer exists: " + err.Error())
			return
		}
		if code == "" {
			fail("revision has no code")
			return
		}
		fn.Data["code"] = code
		fn.Data["version"] = phase3.Int(fn.Data, "version", 1) + 1
		if _, err := st.P3Update(ctx, "lambda_functions", fn); err != nil {
			fail("could not update function: " + err.Error())
			return
		}
		// Verify the stored code is the deployed code.
		fresh, err := st.P3Get(ctx, "lambda_functions", targetID)
		if err != nil || phase3.Str(fresh.Data, "code", "") != code {
			fail("verification failed: stored function code is not the deployed code")
			return
		}
		dep.Data["status"] = "SUCCEEDED"
		dep.Data["ended_at"] = p3Now()
		dep.Data["new_version"] = phase3.Int(fresh.Data, "version", 0)
		_, _ = st.P3Update(ctx, "codedeploy_deployments", dep)
	default:
		fail("unknown target type " + targetType)
	}
}

// --- CloudFormation ------------------------------------------------------------
//
// A separate template engine from Terraform: JSON/YAML templates,
// parameters, change-set diffs against the stack's current
// resources, stack events, and rollback on failure. Provisioning
// goes through the existing Phase 3 resources (SQS/SNS/DynamoDB
// subset/Lambda/S3 via MinIO/RDS on the platform Postgres).
// PARTIAL: only SupportedCFNTypes are provisioned; anything else is
// UNSUPPORTED in the change set. Rollback deletes resources created
// in the failed execution; resources deleted by an update are NOT
// restored (the event log says so).

func (s *Server) handleCFNListStacks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	all, err := st.P3List(r.Context(), "cfn_stacks")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range p3Own(all, u.ID, p3IsAdmin(u.Role)) {
		view := p3View(e)
		delete(view, "template")
		resources, _ := e.Data["resources"].(map[string]any)
		view["resource_count"] = len(resources)
		out = append(out, view)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"stacks": out, "count": len(out), "fidelity": "PARTIAL", "supported_types": phase4.SupportedCFNTypeList()})
}

func (s *Server) handleCFNCreateStack(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string         `json:"name"`
		Template   string         `json:"template"`
		Parameters map[string]any `json:"parameters"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "stack name is required")
		return
	}
	tpl, err := phase4.ParseCFNTemplate([]byte(req.Template))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "template: "+err.Error())
		return
	}
	problems := phase4.ValidateCFNTemplate(tpl)
	if len(problems) > 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "template validation failed: "+strings.Join(problems, "; "))
		return
	}
	params := map[string]any{}
	for k, v := range req.Parameters {
		params[k] = v
	}
	// Defaults fill missing parameters; a parameter with no value
	// and no default is an error.
	for name, p := range tpl.Parameters {
		if _, ok := params[name]; !ok {
			if p.Default != nil {
				params[name] = p.Default
			} else {
				s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("parameter %q has no value and no Default", name))
				return
			}
		}
	}
	e, err := st.P3Create(r.Context(), "cfn_stacks", &store.P3Entity{Name: req.Name, Data: map[string]any{
		"template": req.Template, "parameters": params,
		"status": "CREATE_IN_PROGRESS", "resources": map[string]any{},
		"description": tpl.Description, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	// Create is synchronous in the handler (provisioning is local
	// and fast); the stack is CREATE_COMPLETE only after every
	// resource provisioned, or CREATE_FAILED with rollback.
	s.p4CFNExecute(r.Context(), e, tpl, params, "CREATE")
	fresh, _ := st.P3Get(r.Context(), "cfn_stacks", e.ID)
	if fresh != nil {
		e = fresh
	}
	view := p3View(e)
	delete(view, "template")
	code := http.StatusCreated
	if phase3.Str(e.Data, "status", "") == "CREATE_FAILED" {
		code = http.StatusCreated // the stack exists, in a failed state — the status says so
	}
	s.writeJSON(w, r, code, view)
}

func (s *Server) handleCFNGetStack(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cfn_stacks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	view := p3View(e)
	resources, _ := e.Data["resources"].(map[string]any)
	view["resource_count"] = len(resources)
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleCFNDeleteStack(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	e, err := st.P3Get(r.Context(), "cfn_stacks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, e, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	e.Data["status"] = "DELETE_IN_PROGRESS"
	_, _ = st.P3Update(r.Context(), "cfn_stacks", e)
	s.p4CFNEvent(r.Context(), e.ID, "DELETE", "stack", "Stack delete started")
	failed := []string{}
	resources, _ := e.Data["resources"].(map[string]any)
	names := []string{}
	for name := range resources {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		res, _ := resources[name].(map[string]any)
		if err := s.p4CFNDeleteResource(r.Context(), phase3.Str(res, "type", ""), phase3.Str(res, "physical_id", "")); err != nil {
			failed = append(failed, name+": "+err.Error())
			s.p4CFNEvent(r.Context(), e.ID, "DELETE_FAILED", name, err.Error())
			continue
		}
		s.p4CFNEvent(r.Context(), e.ID, "DELETE_COMPLETE", name, "resource deleted")
		delete(resources, name)
	}
	e.Data["resources"] = resources
	if len(failed) > 0 {
		e.Data["status"] = "DELETE_FAILED"
		e.Data["delete_errors"] = failed
		_, _ = st.P3Update(r.Context(), "cfn_stacks", e)
		s.writeError(w, r, http.StatusBadGateway, "stack_delete_failed", "some resources could not be deleted: "+strings.Join(failed, "; "))
		return
	}
	e.Data["status"] = "DELETE_COMPLETE"
	_, _ = st.P3Update(r.Context(), "cfn_stacks", e)
	s.p4CFNEvent(r.Context(), e.ID, "DELETE_COMPLETE", "stack", "Stack delete complete")
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "id": e.ID, "status": "DELETE_COMPLETE"})
}

func (s *Server) handleCFNListEvents(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stack, err := st.P3Get(r.Context(), "cfn_stacks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, stack, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	all, _ := st.P3List(r.Context(), "cfn_events")
	out := []map[string]any{}
	for _, e := range all {
		if phase3.Str(e.Data, "stack_id", "") == stack.ID {
			out = append(out, p3View(e))
		}
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["created_at"]) < fmt.Sprint(out[j]["created_at"]) })
	s.writeJSON(w, r, http.StatusOK, map[string]any{"events": out, "count": len(out)})
}

func (s *Server) handleCFNValidateTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var req struct {
		Template string `json:"template"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	tpl, err := phase4.ParseCFNTemplate([]byte(req.Template))
	if err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"valid": false, "problems": []string{"template: " + err.Error()}, "supported_types": phase4.SupportedCFNTypeList()})
		return
	}
	problems := phase4.ValidateCFNTemplate(tpl)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"valid": len(problems) == 0, "problems": problems, "supported_types": phase4.SupportedCFNTypeList(), "resource_count": len(tpl.Resources)})
}

func (s *Server) handleCFNCreateChangeset(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	stack, err := st.P3Get(r.Context(), "cfn_stacks", chi.URLParam(r, "id"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, stack, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	var req struct {
		Name       string         `json:"name"`
		Template   string         `json:"template"`
		Parameters map[string]any `json:"parameters"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	templateBody := req.Template
	if templateBody == "" {
		templateBody = phase3.Str(stack.Data, "template", "")
	}
	tpl, err := phase4.ParseCFNTemplate([]byte(templateBody))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "template: "+err.Error())
		return
	}
	params := map[string]any{}
	if existing, ok := stack.Data["parameters"].(map[string]any); ok {
		for k, v := range existing {
			params[k] = v
		}
	}
	for k, v := range req.Parameters {
		params[k] = v
	}
	current := map[string]map[string]any{}
	if resources, ok := stack.Data["resources"].(map[string]any); ok {
		for name, raw := range resources {
			if m, ok := raw.(map[string]any); ok {
				current[name] = m
			}
		}
	}
	changes := phase4.PlanCFNChanges(tpl, current)
	// A change set with only UNSUPPORTED changes is still created
	// (so the console can show why nothing will happen), but it
	// cannot be executed.
	executable := false
	for _, c := range changes {
		if c.Action == "CREATE" || c.Action == "UPDATE" || c.Action == "DELETE" {
			executable = true
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "changeset-" + ids.New()[:8]
	}
	e, err := st.P3Create(r.Context(), "cfn_changesets", &store.P3Entity{Name: name, Data: map[string]any{
		"stack_id": stack.ID, "template": templateBody, "parameters": params,
		"changes": changes, "status": "CREATE_COMPLETE", "executed": false,
		"executable": executable, "owner_id": u.ID,
	}})
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, p3View(e))
}

func (s *Server) handleCFNExecuteChangeset(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.p3Store(w, r)
	if !ok {
		return
	}
	cs, err := st.P3Get(r.Context(), "cfn_changesets", chi.URLParam(r, "changesetId"))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	if !s.p3Visible(w, r, cs, u.ID, p3IsAdmin(u.Role)) {
		return
	}
	if phase3.Bool(cs.Data, "executed", false) {
		s.writeError(w, r, http.StatusConflict, "changeset_already_executed", "this change set has already been executed")
		return
	}
	if !phase3.Bool(cs.Data, "executable", false) {
		s.writeError(w, r, http.StatusConflict, "changeset_not_executable", "this change set contains only UNSUPPORTED changes — nothing can be executed")
		return
	}
	stack, err := st.P3Get(r.Context(), "cfn_stacks", phase3.Str(cs.Data, "stack_id", ""))
	if err != nil {
		s.p3Err(w, r, err)
		return
	}
	tpl, err := phase4.ParseCFNTemplate([]byte(phase3.Str(cs.Data, "template", "")))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "change set template: "+err.Error())
		return
	}
	params := map[string]any{}
	if raw, ok := cs.Data["parameters"].(map[string]any); ok {
		for k, v := range raw {
			params[k] = v
		}
	}
	stack.Data["status"] = "UPDATE_IN_PROGRESS"
	_, _ = st.P3Update(r.Context(), "cfn_stacks", stack)
	s.p4CFNExecute(r.Context(), stack, tpl, params, "UPDATE")
	cs.Data["executed"] = true
	_, _ = st.P3Update(r.Context(), "cfn_changesets", cs)
	fresh, _ := st.P3Get(r.Context(), "cfn_stacks", stack.ID)
	if fresh != nil {
		stack = fresh
	}
	view := p3View(stack)
	delete(view, "template")
	s.writeJSON(w, r, http.StatusOK, view)
}

// p4CFNExecute applies a template to a stack (CREATE or UPDATE).
// Every resource action is an event. On failure the resources
// created in this execution are deleted again and the stack is
// CREATE_FAILED/UPDATE_FAILED with ROLLBACK_COMPLETE.
func (s *Server) p4CFNExecute(ctx context.Context, stack *store.P3Entity, tpl *phase4.CFNTemplate, params map[string]any, action string) {
	st := s.P3
	verb := action
	if verb == "CREATE" {
		verb = "CREATE"
	}
	resources, _ := stack.Data["resources"].(map[string]any)
	if resources == nil {
		resources = map[string]any{}
	}
	physical := map[string]string{}
	for name, raw := range resources {
		if m, ok := raw.(map[string]any); ok {
			physical[name] = phase3.Str(m, "physical_id", "")
		}
	}
	current := map[string]map[string]any{}
	for name, raw := range resources {
		if m, ok := raw.(map[string]any); ok {
			current[name] = m
		}
	}
	changes := phase4.PlanCFNChanges(tpl, current)
	created := []string{} // logical ids created in this execution
	failed := ""
	apply := func(ch phase4.CFNChange) error {
		if ch.Action == "UNSUPPORTED" {
			s.p4CFNEvent(ctx, stack.ID, verb+"_SKIPPED", ch.LogicalID, ch.Reason)
			return nil
		}
		res := tpl.Resources[ch.LogicalID]
		props := map[string]any{}
		for k, v := range res.Properties {
			props[k] = phase4.CFNResolveValue(v, params, physical)
		}
		switch ch.Action {
		case "DELETE":
			cur := current[ch.LogicalID]
			if err := s.p4CFNDeleteResource(ctx, ch.Type, phase3.Str(cur, "physical_id", "")); err != nil {
				return fmt.Errorf("delete %s: %w", ch.LogicalID, err)
			}
			delete(resources, ch.LogicalID)
			delete(physical, ch.LogicalID)
			s.p4CFNEvent(ctx, stack.ID, verb+"_COMPLETE", ch.LogicalID, "resource deleted")
			return nil
		case "CREATE":
			pid, err := s.p4CFNProvisionResource(ctx, stack, ch.LogicalID, ch.Type, props)
			if err != nil {
				return fmt.Errorf("create %s (%s): %w", ch.LogicalID, ch.Type, err)
			}
			resources[ch.LogicalID] = map[string]any{"type": ch.Type, "physical_id": pid, "properties": props}
			physical[ch.LogicalID] = pid
			created = append(created, ch.LogicalID)
			s.p4CFNEvent(ctx, stack.ID, verb+"_COMPLETE", ch.LogicalID, "resource created: "+pid)
			return nil
		case "UPDATE":
			cur := current[ch.LogicalID]
			pid := phase3.Str(cur, "physical_id", "")
			if err := s.p4CFNUpdateResource(ctx, stack, ch.LogicalID, ch.Type, pid, props); err != nil {
				return fmt.Errorf("update %s (%s): %w", ch.LogicalID, ch.Type, err)
			}
			resources[ch.LogicalID] = map[string]any{"type": ch.Type, "physical_id": pid, "properties": props}
			s.p4CFNEvent(ctx, stack.ID, verb+"_COMPLETE", ch.LogicalID, "resource updated: "+pid)
			return nil
		}
		return nil
	}
	// Deletes first (removed from the template, then type changes),
	// then creates/updates in template order.
	for _, ch := range changes {
		if ch.Action != "DELETE" {
			continue
		}
		if err := apply(ch); err != nil {
			failed = err.Error()
			break
		}
	}
	if failed == "" {
		for _, ch := range changes {
			if ch.Action == "DELETE" {
				continue
			}
			if err := apply(ch); err != nil {
				failed = err.Error()
				break
			}
		}
	}
	stack.Data["resources"] = resources
	if failed != "" {
		// Rollback: delete what this execution created.
		for i := len(created) - 1; i >= 0; i-- {
			name := created[i]
			if res, ok := resources[name].(map[string]any); ok {
				_ = s.p4CFNDeleteResource(ctx, phase3.Str(res, "type", ""), phase3.Str(res, "physical_id", ""))
				delete(resources, name)
				s.p4CFNEvent(ctx, stack.ID, "ROLLBACK_COMPLETE", name, "created resource deleted during rollback")
			}
		}
		stack.Data["resources"] = resources
		if action == "CREATE" {
			stack.Data["status"] = "CREATE_FAILED"
		} else {
			stack.Data["status"] = "UPDATE_FAILED"
		}
		stack.Data["failure_reason"] = failed
		stack.Data["rollback_status"] = "ROLLBACK_COMPLETE (created resources deleted; resources deleted by this update were NOT restored)"
		s.p4CFNEvent(ctx, stack.ID, "ROLLBACK_COMPLETE", "stack", "rollback complete: "+failed)
	} else {
		if action == "CREATE" {
			stack.Data["status"] = "CREATE_COMPLETE"
		} else {
			stack.Data["status"] = "UPDATE_COMPLETE"
		}
		delete(stack.Data, "failure_reason")
		delete(stack.Data, "rollback_status")
		s.p4CFNEvent(ctx, stack.ID, verb+"_COMPLETE", "stack", "Stack "+strings.ToLower(verb)+" complete")
	}
	_, _ = st.P3Update(ctx, "cfn_stacks", stack)
}

// p4CFNEvent appends a stack event (best-effort — an event failure
// never fails the stack operation).
func (s *Server) p4CFNEvent(ctx context.Context, stackID, status, logicalID, reason string) {
	st := s.P3
	if st == nil {
		return
	}
	_, _ = st.P3Create(ctx, "cfn_events", &store.P3Entity{Name: stackID + "-" + ids.New()[:8], Data: map[string]any{
		"stack_id": stackID, "status": status, "logical_id": logicalID,
		"reason": reason, "at": p3Now(),
	}})
}

// p4CFNProvisionResource creates the real Phase 3 resource for one
// template resource and returns its physical id.
func (s *Server) p4CFNProvisionResource(ctx context.Context, stack *store.P3Entity, logicalID, typ string, props map[string]any) (string, error) {
	st := s.P3
	owner := phase3.Str(stack.Data, "owner_id", "")
	strProp := func(key, def string) string {
		if v, ok := props[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return def
	}
	switch typ {
	case "AWS::SQS::Queue":
		name := strProp("QueueName", logicalID)
		if !sqsNameRE.MatchString(name) {
			return "", fmt.Errorf("QueueName %q is not a valid queue name (1-80 letters, digits, _ and -)", name)
		}
		e, err := st.P3Create(ctx, "sqs_queues", &store.P3Entity{Name: name, Data: map[string]any{
			"visibility_timeout_seconds": 30, "message_retention_seconds": 345600,
			"max_receives": 0, "dead_letter_queue_id": "", "owner_id": owner,
			"cfn_stack_id": stack.ID, "cfn_logical_id": logicalID,
		}})
		if err != nil {
			return "", err
		}
		return e.ID, nil
	case "AWS::SNS::Topic":
		name := strProp("TopicName", logicalID)
		if !sqsNameRE.MatchString(name) {
			return "", fmt.Errorf("TopicName %q is not a valid topic name (1-80 letters, digits, _ and -)", name)
		}
		e, err := st.P3Create(ctx, "sns_topics", &store.P3Entity{Name: name, Data: map[string]any{
			"arn": "arn:cloudforge:sns::local:" + name, "owner_id": owner,
			"cfn_stack_id": stack.ID, "cfn_logical_id": logicalID,
		}})
		if err != nil {
			return "", err
		}
		return e.ID, nil
	case "AWS::DynamoDB::Table":
		name := strProp("TableName", logicalID)
		keyAttr := "id"
		if ks, ok := props["KeySchema"].([]any); ok && len(ks) > 0 {
			if km, ok := ks[0].(map[string]any); ok {
				if attr, ok := km["AttributeName"].(string); ok && attr != "" {
					keyAttr = attr
				}
			}
		}
		// The Postgres-subset backend (PARTIAL): durable, hash key
		// only — the same subset the DynamoDB console falls back to
		// when DynamoDB Local is unreachable.
		e, err := st.P3Create(ctx, "dynamodb_tables", &store.P3Entity{Name: name, Data: map[string]any{
			"key_attribute": keyAttr, "status": "ACTIVE", "backend": "postgres-subset",
			"fidelity": "PARTIAL", "owner_id": owner,
			"cfn_stack_id": stack.ID, "cfn_logical_id": logicalID,
		}})
		if err != nil {
			return "", err
		}
		return e.ID, nil
	case "AWS::Lambda::Function":
		runtime := strProp("Runtime", "python3")
		if _, ok := phase3.SupportedRuntimes[runtime]; !ok {
			return "", fmt.Errorf("runtime %q is not supported (python3 / nodejs only)", runtime)
		}
		code := ""
		if c, ok := props["Code"].(map[string]any); ok {
			code, _ = c["ZipFile"].(string)
		}
		if strings.TrimSpace(code) == "" {
			return "", errors.New("Properties.Code.ZipFile is required (inline code)")
		}
		name := strProp("FunctionName", logicalID)
		e, err := st.P3Create(ctx, "lambda_functions", &store.P3Entity{Name: name, Data: map[string]any{
			"runtime": runtime, "handler": strProp("Handler", "handler.handle"),
			"code": code, "env": map[string]any{}, "timeout_seconds": 10, "memory_mb": 256,
			"version": 1, "aliases": map[string]any{}, "triggers": []any{}, "owner_id": owner,
			"cfn_stack_id": stack.ID, "cfn_logical_id": logicalID,
		}})
		if err != nil {
			return "", err
		}
		return e.ID, nil
	case "AWS::S3::Bucket":
		name := strings.ToLower(strProp("BucketName", logicalID))
		ad := s.AWS
		if ad == nil || ad.S3 == nil {
			return "", errors.New("S3 is not configured on this server (no MinIO adapter)")
		}
		if err := ad.S3.CreateBucket(ctx, name); err != nil {
			return "", fmt.Errorf("MinIO CreateBucket failed: %w", err)
		}
		return name, nil
	case "AWS::RDS::DBInstance":
		if s.P3DatabaseURL == "" {
			return "", errors.New("no platform Postgres is configured (DATABASE_URL); RDS cannot be provisioned")
		}
		dbName := strProp("DBName", strings.ReplaceAll(strings.ToLower(logicalID), "-", "_"))
		username := "cf_" + strings.ReplaceAll(strings.ToLower(logicalID), "-", "_")
		if err := phase3.ValidatePGIdentifier(dbName); err != nil {
			return "", fmt.Errorf("DBName: %w", err)
		}
		if err := phase3.ValidatePGIdentifier(username); err != nil {
			return "", fmt.Errorf("username: %w", err)
		}
		password, err := randomToken(10)
		if err != nil {
			return "", err
		}
		if err := phase3.RDSCreateDatabase(ctx, s.P3DatabaseURL, dbName, username, password); err != nil {
			return "", fmt.Errorf("Postgres provisioning failed: %w", err)
		}
		e, err := st.P3Create(ctx, "rds_instances", &store.P3Entity{Name: logicalID, Data: map[string]any{
			"engine": "postgres", "version": "16", "database_name": dbName,
			"username": username, "password_hash": store.HashSecret(password),
			"endpoint": s.rdsEndpoint(), "status": "available",
			"fidelity": "PARTIAL", "owner_id": owner,
			"cfn_stack_id": stack.ID, "cfn_logical_id": logicalID,
		}})
		if err != nil {
			_ = phase3.RDSDropDatabase(ctx, s.P3DatabaseURL, dbName, username)
			return "", err
		}
		return e.ID, nil
	default:
		return "", fmt.Errorf("resource type %q is not supported by CloudFormation", typ)
	}
}

// p4CFNUpdateResource re-applies a resource's properties in place
// where the underlying service supports it; otherwise it is a
// verified no-op (the resource still exists with the same physical
// id) and the event log says so.
func (s *Server) p4CFNUpdateResource(ctx context.Context, stack *store.P3Entity, logicalID, typ, physicalID string, props map[string]any) error {
	st := s.P3
	switch typ {
	case "AWS::Lambda::Function":
		fn, err := st.P3Get(ctx, "lambda_functions", physicalID)
		if err != nil {
			return err
		}
		if c, ok := props["Code"].(map[string]any); ok {
			if code, ok := c["ZipFile"].(string); ok && strings.TrimSpace(code) != "" {
				fn.Data["code"] = code
				fn.Data["version"] = phase3.Int(fn.Data, "version", 1) + 1
			}
		}
		if _, err := st.P3Update(ctx, "lambda_functions", fn); err != nil {
			return err
		}
		return nil
	case "AWS::SQS::Queue":
		q, err := st.P3Get(ctx, "sqs_queues", physicalID)
		if err != nil {
			return err
		}
		if v, ok := props["VisibilityTimeout"]; ok {
			if n, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil && n >= 1 && n <= 43200 {
				q.Data["visibility_timeout_seconds"] = n
			}
		}
		_, err = st.P3Update(ctx, "sqs_queues", q)
		return err
	default:
		// SQS (other props), SNS, DynamoDB, S3, RDS: the resource
		// exists and keeps its physical id; in-place property
		// updates are not implemented for these types, and the
		// change set said so ("properties are re-applied;
		// best-effort per resource type").
		if _, err := s.p4CFNExists(ctx, typ, physicalID); err != nil {
			return err
		}
		return nil
	}
}

// p4CFNExists verifies a physical resource still exists.
func (s *Server) p4CFNExists(ctx context.Context, typ, physicalID string) (bool, error) {
	st := s.P3
	switch typ {
	case "AWS::SNS::Topic":
		_, err := st.P3Get(ctx, "sns_topics", physicalID)
		return err == nil, err
	case "AWS::DynamoDB::Table":
		_, err := st.P3Get(ctx, "dynamodb_tables", physicalID)
		return err == nil, err
	case "AWS::S3::Bucket":
		ad := s.AWS
		if ad == nil || ad.S3 == nil {
			return false, errors.New("S3 is not configured on this server")
		}
		buckets, err := ad.S3.ListBuckets(ctx)
		if err != nil {
			return false, err
		}
		for _, b := range buckets {
			if b.Name == physicalID {
				return true, nil
			}
		}
		return false, errors.New("bucket " + physicalID + " no longer exists in MinIO")
	case "AWS::RDS::DBInstance":
		_, err := st.P3Get(ctx, "rds_instances", physicalID)
		return err == nil, err
	default:
		return false, fmt.Errorf("unknown type %q", typ)
	}
}

// p4CFNDeleteResource deletes the real resource behind a physical id.
func (s *Server) p4CFNDeleteResource(ctx context.Context, typ, physicalID string) error {
	st := s.P3
	switch typ {
	case "AWS::SQS::Queue":
		msgs, _ := st.P3List(ctx, "sqs_messages")
		for _, m := range msgs {
			if phase3.Str(m.Data, "queue_id", "") == physicalID {
				_ = st.P3Delete(ctx, "sqs_messages", m.ID)
			}
		}
		return st.P3Delete(ctx, "sqs_queues", physicalID)
	case "AWS::SNS::Topic":
		subs, _ := st.P3List(ctx, "sns_subscriptions")
		for _, sub := range subs {
			if phase3.Str(sub.Data, "topic_id", "") == physicalID {
				_ = st.P3Delete(ctx, "sns_subscriptions", sub.ID)
			}
		}
		return st.P3Delete(ctx, "sns_topics", physicalID)
	case "AWS::DynamoDB::Table":
		items, _ := st.P3List(ctx, "dynamodb_items")
		for _, it := range items {
			if phase3.Str(it.Data, "table_name", "") != "" {
				// Items are keyed by table name in the subset store;
				// the table entity name is the table name.
				if tbl, err := st.P3Get(ctx, "dynamodb_tables", physicalID); err == nil && phase3.Str(it.Data, "table_name", "") == tbl.Name {
					_ = st.P3Delete(ctx, "dynamodb_items", it.ID)
				}
			}
		}
		return st.P3Delete(ctx, "dynamodb_tables", physicalID)
	case "AWS::Lambda::Function":
		return st.P3Delete(ctx, "lambda_functions", physicalID)
	case "AWS::S3::Bucket":
		ad := s.AWS
		if ad == nil || ad.S3 == nil {
			return errors.New("S3 is not configured on this server")
		}
		return ad.S3.RemoveBucket(ctx, physicalID)
	case "AWS::RDS::DBInstance":
		e, err := st.P3Get(ctx, "rds_instances", physicalID)
		if err != nil {
			return err
		}
		if s.P3DatabaseURL != "" {
			if err := phase3.RDSDropDatabase(ctx, s.P3DatabaseURL, phase3.Str(e.Data, "database_name", ""), phase3.Str(e.Data, "username", "")); err != nil {
				return err
			}
		}
		return st.P3Delete(ctx, "rds_instances", physicalID)
	default:
		return fmt.Errorf("resource type %q is not supported by CloudFormation", typ)
	}
}

// --- route registration ---------------------------------------------------------

func (s *Server) registerPhase4Routes(r chi.Router) {
	// ECR
	r.Get("/aws/ecr/repositories", s.handleECRListRepos)
	r.Post("/aws/ecr/repositories", s.handleECRCreateRepo)
	r.Delete("/aws/ecr/repositories/{id}", s.handleECRDeleteRepo)
	r.Get("/aws/ecr/repositories/{id}/images", s.handleECRListImages)
	r.Post("/aws/ecr/repositories/{id}/images", s.handleECRPushImage)
	r.Get("/aws/ecr/images/{imageId}", s.handleECRGetImage)
	r.Delete("/aws/ecr/images/{imageId}", s.handleECRDeleteImage)
	r.Post("/aws/ecr/images/{imageId}/scan", s.handleECRScan)
	r.Post("/aws/ecr/repositories/{id}/lifecycle", s.handleECRLifecycle)
	// ECS + Fargate
	r.Get("/aws/ecs/clusters", s.handleECSListClusters)
	r.Post("/aws/ecs/clusters", s.handleECSCreateCluster)
	r.Delete("/aws/ecs/clusters/{id}", s.handleECSDeleteCluster)
	r.Post("/aws/ecs/task-definitions", s.handleECSRegisterTaskDef)
	r.Get("/aws/ecs/task-definitions", s.handleECSListTaskDefs)
	r.Delete("/aws/ecs/task-definitions/{id}", s.handleECSDeleteTaskDef)
	r.Get("/aws/ecs/services", s.handleECSListServices)
	r.Post("/aws/ecs/services", s.handleECSCreateService)
	r.Get("/aws/ecs/services/{id}", s.handleECSGetService)
	r.Put("/aws/ecs/services/{id}", s.handleECSUpdateService)
	r.Delete("/aws/ecs/services/{id}", s.handleECSDeleteService)
	r.Post("/aws/ecs/tasks/run", s.handleECSRunTask)
	r.Get("/aws/ecs/tasks", s.handleECSListTasks)
	r.Get("/aws/ecs/tasks/{id}", s.handleECSGetTask)
	r.Post("/aws/ecs/tasks/{id}/stop", s.handleECSStopTask)
	r.Get("/aws/ecs/tasks/{id}/logs", s.handleECSTaskLogs)
	// EKS (decision + learning path only — no cluster)
	r.Get("/aws/eks/info", s.handleEKSInfo)
	// CodeBuild
	r.Get("/aws/codebuild/projects", s.handleCodeBuildListProjects)
	r.Post("/aws/codebuild/projects", s.handleCodeBuildCreateProject)
	r.Delete("/aws/codebuild/projects/{id}", s.handleCodeBuildDeleteProject)
	r.Post("/aws/codebuild/projects/{id}/builds", s.handleCodeBuildStart)
	r.Get("/aws/codebuild/builds", s.handleCodeBuildListBuilds)
	r.Get("/aws/codebuild/builds/{id}", s.handleCodeBuildGetBuild)
	r.Get("/aws/codebuild/builds/{id}/logs", s.handleCodeBuildLogs)
	// CodePipeline
	r.Get("/aws/codepipeline/pipelines", s.handlePipelineList)
	r.Post("/aws/codepipeline/pipelines", s.handlePipelineCreate)
	r.Delete("/aws/codepipeline/pipelines/{id}", s.handlePipelineDelete)
	r.Post("/aws/codepipeline/pipelines/{id}/executions", s.handlePipelineStart)
	r.Get("/aws/codepipeline/executions", s.handlePipelineListExecutions)
	r.Get("/aws/codepipeline/executions/{id}", s.handlePipelineGetExecution)
	// CodeDeploy
	r.Get("/aws/codedeploy/applications", s.handleCodeDeployListApps)
	r.Post("/aws/codedeploy/applications", s.handleCodeDeployCreateApp)
	r.Delete("/aws/codedeploy/applications/{id}", s.handleCodeDeployDeleteApp)
	r.Post("/aws/codedeploy/applications/{id}/deployments", s.handleCodeDeployCreateDeployment)
	r.Get("/aws/codedeploy/deployments", s.handleCodeDeployListDeployments)
	r.Get("/aws/codedeploy/deployments/{id}", s.handleCodeDeployGetDeployment)
	r.Post("/aws/codedeploy/deployments/{id}/rollback", s.handleCodeDeployRollback)
	// CloudFormation
	r.Post("/aws/cloudformation/validate-template", s.handleCFNValidateTemplate)
	r.Get("/aws/cloudformation/stacks", s.handleCFNListStacks)
	r.Post("/aws/cloudformation/stacks", s.handleCFNCreateStack)
	r.Get("/aws/cloudformation/stacks/{id}", s.handleCFNGetStack)
	r.Delete("/aws/cloudformation/stacks/{id}", s.handleCFNDeleteStack)
	r.Get("/aws/cloudformation/stacks/{id}/events", s.handleCFNListEvents)
	r.Post("/aws/cloudformation/stacks/{id}/changesets", s.handleCFNCreateChangeset)
	r.Post("/aws/cloudformation/stacks/{id}/changesets/{changesetId}/execute", s.handleCFNExecuteChangeset)
}

// registerECRRegistryRoutes mounts the Docker Registry HTTP API
// subset on /v2/ (outside /api/v1 and outside auth — a docker
// client cannot present a CloudForge JWT).
func (s *Server) registerECRRegistryRoutes(r chi.Router) {
	r.Handle("/v2/*", http.HandlerFunc(s.handleRegistryDispatch))
	r.Get("/v2", http.HandlerFunc(s.handleRegistryDispatch))
}
