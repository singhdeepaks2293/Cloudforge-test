// S3 object API (Phase 2) — objects, metadata, versioning and bucket
// policy over the real MinIO adapter. Uploads go through the backend
// (the browser never sees MinIO credentials); MinIO down is an honest
// 503, exactly as for buckets. Object ACLs are NOT exposed by the
// MinIO adapter (minio-go has no Get/SetBucketACL surface), so the
// Permissions tab shows the bucket policy only and says so.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/providers"
)

// maxS3UploadBytes caps a single object upload through the backend
// (10 MiB of decoded bytes).
const maxS3UploadBytes = 10 << 20

func (s *Server) s3Adapter(w http.ResponseWriter, r *http.Request) (*providers.S3Adapter, bool) {
	if s.AWS == nil || s.AWS.S3 == nil {
		s.backendUnavailable(w, r, "s3", "ListObjects", "S3 providers are not configured on this server")
		return nil, false
	}
	return s.AWS.S3, true
}

func (s *Server) s3Err(w http.ResponseWriter, r *http.Request, service, operation string, err error) {
	if errors.Is(err, providers.ErrS3Unavailable) {
		s.backendUnavailable(w, r, service, operation, err.Error())
		return
	}
	s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
}

func (s *Server) handleS3ListObjects(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	objects, err := ad.ListObjects(r.Context(), bucket, r.URL.Query().Get("prefix"))
	if err != nil {
		s.s3Err(w, r, "s3", "ListObjects", err)
		return
	}
	if objects == nil {
		objects = []providers.S3Object{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"bucket": bucket, "prefix": r.URL.Query().Get("prefix"), "objects": objects,
	})
}

type s3UploadRequest struct {
	Key           string `json:"key"`
	ContentBase64 string `json:"content_base64"`
	ContentType   string `json:"content_type"`
}

func (s *Server) handleS3UploadObject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	var req s3UploadRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key is required")
		return
	}
	if strings.HasSuffix(req.Key, "/") {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "key must name an object, not a prefix (keys ending in / are folders)")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "content_base64 is not valid base64: "+err.Error())
		return
	}
	if len(data) > maxS3UploadBytes {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "object exceeds the 10 MiB upload limit")
		return
	}
	if err := ad.PutObject(r.Context(), bucket, req.Key, data); err != nil {
		s.s3Err(w, r, "s3", "PutObject", err)
		return
	}
	s.audit(r, u.ID, "aws_s3_object_uploaded", "s3-object", bucket+"/"+req.Key, map[string]any{"size": len(data)})
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"bucket": bucket, "key": req.Key, "size": len(data)})
}

func (s *Server) handleS3DownloadObject(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")
	if key == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "object key is required")
		return
	}
	meta, err := ad.StatObject(r.Context(), bucket, key)
	if err != nil {
		// A missing object is a 404, not a 400: StatObject's
		// NoSuchKey surfaces as a plain error from MinIO.
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "does not exist") {
			s.writeError(w, r, http.StatusNotFound, "object_not_found", "no object "+key+" in bucket "+bucket)
			return
		}
		s.s3Err(w, r, "s3", "GetObject", err)
		return
	}
	data, err := ad.GetObject(r.Context(), bucket, key)
	if err != nil {
		s.s3Err(w, r, "s3", "GetObject", err)
		return
	}
	ct := meta.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(key)+`"`)
	w.Header().Set("Content-Length", itoaLen(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleS3DeleteObject(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")
	if key == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "object key is required")
		return
	}
	if err := ad.DeleteObject(r.Context(), bucket, key); err != nil {
		s.s3Err(w, r, "s3", "DeleteObject", err)
		return
	}
	s.audit(r, u.ID, "aws_s3_object_deleted", "s3-object", bucket+"/"+key, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": key})
}

func (s *Server) handleS3ObjectMetadata(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")
	if key == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "object key is required")
		return
	}
	meta, err := ad.StatObject(r.Context(), bucket, key)
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "does not exist") {
			s.writeError(w, r, http.StatusNotFound, "object_not_found", "no object "+key+" in bucket "+bucket)
			return
		}
		s.s3Err(w, r, "s3", "StatObject", err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, meta)
}

func (s *Server) handleS3GetVersioning(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	enabled, err := ad.GetBucketVersioning(r.Context(), bucket)
	if err != nil {
		s.s3Err(w, r, "s3", "GetBucketVersioning", err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"bucket": bucket, "enabled": enabled})
}

type s3VersioningRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleS3SetVersioning(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	var req s3VersioningRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := ad.SetBucketVersioning(r.Context(), bucket, req.Enabled); err != nil {
		s.s3Err(w, r, "s3", "SetBucketVersioning", err)
		return
	}
	s.audit(r, u.ID, "aws_s3_versioning_set", "s3-bucket", bucket, map[string]any{"enabled": req.Enabled})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"bucket": bucket, "enabled": req.Enabled})
}

func (s *Server) handleS3GetPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	policy, err := ad.GetBucketPolicy(r.Context(), bucket)
	if err != nil {
		s.s3Err(w, r, "s3", "GetBucketPolicy", err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"bucket": bucket, "policy": policy})
}

type s3PolicyRequest struct {
	Policy string `json:"policy"`
}

func (s *Server) handleS3SetPolicy(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	var req s3PolicyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Policy != "" && !json.Valid([]byte(req.Policy)) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "policy is not valid JSON")
		return
	}
	if err := ad.SetBucketPolicy(r.Context(), bucket, req.Policy); err != nil {
		s.s3Err(w, r, "s3", "SetBucketPolicy", err)
		return
	}
	s.audit(r, u.ID, "aws_s3_policy_set", "s3-bucket", bucket, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"bucket": bucket, "policy": req.Policy})
}

func (s *Server) handleS3DeleteBucket(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	ad, ok := s.s3Adapter(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if err := ad.RemoveBucket(r.Context(), bucket); err != nil {
		s.s3Err(w, r, "s3", "DeleteBucket", err)
		return
	}
	s.audit(r, u.ID, "aws_s3_bucket_deleted", "s3-bucket", bucket, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": bucket})
}

func itoaLen(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
