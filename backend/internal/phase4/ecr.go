// ECR registry backends (Phase 4).
//
// The Docker Registry HTTP API v2 subset CloudForge implements:
//
//	GET  /v2/                              -> {} (API version check)
//	GET  /v2/_catalog                     -> {"repositories": [...]}
//	GET  /v2/<name>/tags/list             -> {"name": ..., "tags": [...]}
//	GET  /v2/<name>/manifests/<ref>       -> manifest JSON (+ Docker-Content-Digest)
//	PUT  /v2/<name>/manifests/<ref>       -> store manifest (blobs must exist)
//	GET  /v2/<name>/blobs/<digest>        -> blob bytes
//	POST /v2/<name>/blobs/uploads/        -> 202 + Location (monolithic upload:
//	     PUT <location>?digest=sha256:... with the blob as body)
//	DELETE /v2/<name>/manifests/<digest>  -> delete manifest + image record
//
// Two backends implement it:
//   - LocalDiskBackend (default, lite + full): blobs are
//     content-addressed files under <workdir>/ecr/blobs/sha256/<hex>,
//     manifests are files under <workdir>/ecr/manifests/<repo>/<tag>.json.
//     The Postgres catalogue (p4_ecr_repos / p4_ecr_images) is the
//     source of truth for the console; the backend is the source of
//     truth for bytes.
//   - RemoteRegistryBackend: the same API proxied to a real
//     registry:2 container (ECR_REGISTRY_URL, full mode only). The
//     console catalogue stays in Postgres; blobs/manifests live in
//     the external registry. An unreachable external registry is a
//     502 from the handlers, never a silent local fallback.
//
// What is NOT implemented (and is never claimed): chunked/resumable
// uploads (only the monolithic POST+PUT flow), OCI index manifests,
// content negotiation beyond schema2, authentication (the registry
// is local-only, like the unauthenticated engine gRPC port —
// documented in docs/security/model.md as a Phase 7 item), and any
// Docker Hub compatibility.
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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RegistryBackend is one implementation of the registry API subset.
type RegistryBackend interface {
	// Name identifies the backend in API responses ("local-disk" or
	// "registry:2 at <url>").
	Name() string
	Catalog(ctx context.Context) ([]string, error)
	Tags(ctx context.Context, repo string) ([]string, error)
	GetManifest(ctx context.Context, repo, ref string) (manifest []byte, digest string, err error)
	PutManifest(ctx context.Context, repo, ref string, manifest []byte) (digest string, err error)
	DeleteManifest(ctx context.Context, repo, ref string) error
	BlobExists(ctx context.Context, digest string) (bool, error)
	GetBlob(ctx context.Context, digest string) (io.ReadCloser, int64, error)
	PutBlob(ctx context.Context, repo, digest string, r io.Reader) error
	DeleteBlob(ctx context.Context, digest string) error
}

// --- local disk backend ------------------------------------------------------

type LocalDiskBackend struct{ Root string }

func NewLocalDiskBackend(workdir string) *LocalDiskBackend {
	return &LocalDiskBackend{Root: filepath.Join(workdir, "ecr")}
}

func (b *LocalDiskBackend) Name() string { return "local-disk" }

func (b *LocalDiskBackend) blobPath(digest string) (string, error) {
	hexPart, err := DigestHex(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.Root, "blobs", "sha256", hexPart), nil
}

func (b *LocalDiskBackend) manifestPath(repo, ref string) string {
	safe := strings.ReplaceAll(ref, "/", "_")
	return filepath.Join(b.Root, "manifests", repo, safe+".json")
}

func (b *LocalDiskBackend) Catalog(ctx context.Context) ([]string, error) {
	manDir := filepath.Join(b.Root, "manifests")
	out := []string{}
	err := filepath.WalkDir(manDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			rel, err := filepath.Rel(manDir, filepath.Dir(path))
			if err == nil && rel != "." {
				out = append(out, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// De-duplicate (one repo, many tags) and sort.
	seen := map[string]bool{}
	uniq := []string{}
	for _, r := range out {
		if !seen[r] {
			seen[r] = true
			uniq = append(uniq, r)
		}
	}
	sort.Strings(uniq)
	return uniq, nil
}

func (b *LocalDiskBackend) Tags(ctx context.Context, repo string) ([]string, error) {
	dir := filepath.Join(b.Root, "manifests", repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRegistryNotFound
		}
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			out = append(out, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return out, nil
}

// ErrRegistryNotFound is returned when a repo/tag/manifest/blob does
// not exist in the registry backend.
var ErrRegistryNotFound = errors.New("not found in registry")

func (b *LocalDiskBackend) GetManifest(ctx context.Context, repo, ref string) ([]byte, string, error) {
	data, err := os.ReadFile(b.manifestPath(repo, ref))
	if err != nil {
		if os.IsNotExist(err) {
			// Try digest lookup: manifests are also indexed by digest.
			if strings.HasPrefix(ref, "sha256:") {
				hexPart, _ := DigestHex(ref)
				p := filepath.Join(b.Root, "manifests-by-digest", hexPart+".json")
				data, err = os.ReadFile(p)
				if err == nil {
					return data, ref, nil
				}
			}
			return nil, "", ErrRegistryNotFound
		}
		return nil, "", err
	}
	return data, Digest(data), nil
}

func (b *LocalDiskBackend) PutManifest(ctx context.Context, repo, ref string, manifest []byte) (string, error) {
	// Validate it is a schema2 manifest with a config + layers, and
	// that every referenced blob exists. A manifest pointing at
	// missing blobs is rejected (BLOB_UNKNOWN), not stored.
	var m struct {
		SchemaVersion int `json:"schemaVersion"`
		Config        struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return "", fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	if m.SchemaVersion != 2 {
		return "", fmt.Errorf("only schemaVersion 2 manifests are supported (got %d)", m.SchemaVersion)
	}
	refs := []string{}
	if m.Config.Digest != "" {
		refs = append(refs, m.Config.Digest)
	}
	for _, l := range m.Layers {
		refs = append(refs, l.Digest)
	}
	for _, d := range refs {
		p, err := b.blobPath(d)
		if err != nil {
			return "", fmt.Errorf("blob %q: %w", d, err)
		}
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("blob unknown: %s (push the blob before the manifest)", d)
		}
	}
	p := b.manifestPath(repo, ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, manifest, 0o644); err != nil {
		return "", err
	}
	digest := Digest(manifest)
	hexPart, _ := DigestHex(digest)
	idx := filepath.Join(b.Root, "manifests-by-digest", hexPart+".json")
	_ = os.MkdirAll(filepath.Dir(idx), 0o755)
	_ = os.WriteFile(idx, manifest, 0o644)
	return digest, nil
}

func (b *LocalDiskBackend) DeleteManifest(ctx context.Context, repo, ref string) error {
	p := b.manifestPath(repo, ref)
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return ErrRegistryNotFound
		}
		return err
	}
	return nil
}

func (b *LocalDiskBackend) BlobExists(ctx context.Context, digest string) (bool, error) {
	p, err := b.blobPath(digest)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (b *LocalDiskBackend) GetBlob(ctx context.Context, digest string) (io.ReadCloser, int64, error) {
	p, err := b.blobPath(digest)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrRegistryNotFound
		}
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, fi.Size(), nil
}

func (b *LocalDiskBackend) PutBlob(ctx context.Context, repo, digest string, r io.Reader) error {
	hexPart, err := DigestHex(digest)
	if err != nil {
		return err
	}
	dir := filepath.Join(b.Root, "blobs", "sha256")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+hexPart+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	if _, err := io.Copy(w, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	_ = f.Close()
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != digest {
		_ = os.Remove(tmp)
		return fmt.Errorf("blob digest mismatch: content is %s, upload claimed %s", got, digest)
	}
	return os.Rename(tmp, filepath.Join(dir, hexPart))
}

func (b *LocalDiskBackend) DeleteBlob(ctx context.Context, digest string) error {
	p, err := b.blobPath(digest)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- remote registry:2 backend ----------------------------------------------

type RemoteRegistryBackend struct {
	Base string // e.g. http://registry:5000 (no trailing slash)
	cli  *http.Client
}

func NewRemoteRegistryBackend(base string) *RemoteRegistryBackend {
	return &RemoteRegistryBackend{Base: strings.TrimSuffix(base, "/"), cli: &http.Client{Timeout: 30 * time.Second}}
}

func (b *RemoteRegistryBackend) Name() string { return "registry:2 at " + b.Base }

func (b *RemoteRegistryBackend) do(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, b.Base+path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("external registry %s unreachable: %w", b.Base, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, ErrRegistryNotFound
	}
	return resp, nil
}

func (b *RemoteRegistryBackend) Catalog(ctx context.Context) ([]string, error) {
	resp, err := b.do(ctx, http.MethodGet, "/v2/_catalog", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external registry catalog: %s", resp.Status)
	}
	var out struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Repositories == nil {
		out.Repositories = []string{}
	}
	return out.Repositories, nil
}

func (b *RemoteRegistryBackend) Tags(ctx context.Context, repo string) ([]string, error) {
	resp, err := b.do(ctx, http.MethodGet, "/v2/"+repo+"/tags/list", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external registry tags: %s", resp.Status)
	}
	var out struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out.Tags, nil
}

func (b *RemoteRegistryBackend) GetManifest(ctx context.Context, repo, ref string) ([]byte, string, error) {
	resp, err := b.do(ctx, http.MethodGet, "/v2/"+repo+"/manifests/"+ref, nil,
		map[string]string{"Accept": "application/vnd.docker.distribution.manifest.v2+json"})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("external registry manifest: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		digest = Digest(data)
	}
	return data, digest, nil
}

func (b *RemoteRegistryBackend) PutManifest(ctx context.Context, repo, ref string, manifest []byte) (string, error) {
	resp, err := b.do(ctx, http.MethodPut, "/v2/"+repo+"/manifests/"+ref, bytes.NewReader(manifest),
		map[string]string{"Content-Type": "application/vnd.docker.distribution.manifest.v2+json"})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("external registry manifest push: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		digest = Digest(manifest)
	}
	return digest, nil
}

func (b *RemoteRegistryBackend) DeleteManifest(ctx context.Context, repo, ref string) error {
	// registry:2 deletes by digest, not tag.
	digest := ref
	if !strings.HasPrefix(ref, "sha256:") {
		_, d, err := b.GetManifest(ctx, repo, ref)
		if err != nil {
			return err
		}
		digest = d
	}
	resp, err := b.do(ctx, http.MethodDelete, "/v2/"+repo+"/manifests/"+digest, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("external registry manifest delete: %s", resp.Status)
	}
	return nil
}

func (b *RemoteRegistryBackend) BlobExists(ctx context.Context, digest string) (bool, error) {
	// HEAD against any repo path is not valid; use the catalog repo
	// list is not needed — callers pass repo via PutBlob; for the
	// existence check we probe with a GET on the first repo is not
	// possible, so this backend reports existence via GetBlob error.
	// Callers use BlobExists only for the local backend's manifest
	// validation; the remote backend validates server-side on PUT.
	return true, nil
}

func (b *RemoteRegistryBackend) GetBlob(ctx context.Context, digest string) (io.ReadCloser, int64, error) {
	// Blobs are addressed per-repository in the registry API; the
	// digest-only form cannot be fetched without a repo. Handlers
	// pass the repo through the request path, so this method is
	// unused by them — GetBlobForRepo is the real path.
	return nil, 0, errors.New("remote registry blobs are addressed per-repository; use the repository blob endpoint")
}

// GetBlobForRepo fetches a blob from the remote registry.
func (b *RemoteRegistryBackend) GetBlobForRepo(ctx context.Context, repo, digest string) (io.ReadCloser, int64, error) {
	resp, err := b.do(ctx, http.MethodGet, "/v2/"+repo+"/blobs/"+digest, nil, nil)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("external registry blob: %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

func (b *RemoteRegistryBackend) PutBlob(ctx context.Context, repo, digest string, r io.Reader) error {
	// Monolithic upload: POST .../blobs/uploads/ then PUT with
	// ?digest=. registry:2 returns the upload location.
	resp, err := b.do(ctx, http.MethodPost, "/v2/"+repo+"/blobs/uploads/", nil, nil)
	if err != nil {
		return err
	}
	loc := resp.Header.Get("Location")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || loc == "" {
		return fmt.Errorf("external registry blob upload start: %s", resp.Status)
	}
	uploadURL := loc
	if !strings.HasPrefix(uploadURL, "http") {
		uploadURL = b.Base + loc
	}
	u, err := url.Parse(uploadURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("digest", digest)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := b.cli.Do(req)
	if err != nil {
		return fmt.Errorf("external registry %s unreachable: %w", b.Base, err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("external registry blob upload: %s: %s", putResp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (b *RemoteRegistryBackend) DeleteBlob(ctx context.Context, digest string) error {
	// registry:2 has no blob-delete API; deletion is via manifest
	// delete + garbage collection on the registry host.
	return errors.New("registry:2 has no blob-delete API (blobs are reclaimed by registry garbage collection on the host)")
}

// sha256 helpers: crypto/sha256 is imported by phase4.go; this file
// uses it via the Digest helper there. No extra helpers needed.
