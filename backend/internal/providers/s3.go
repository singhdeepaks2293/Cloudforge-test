package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/security"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrS3Unavailable is returned when the S3 backend (MinIO) cannot be
// reached or rejects the request for a backend-level reason. Callers
// map it to HTTP 503 with the capability info — never to a faked
// success.
var ErrS3Unavailable = errors.New("s3 backend unavailable")

// S3Adapter is a real S3-compatible client backed by a local MinIO
// server (minio-go v7). It is NOT AWS S3: buckets and objects live
// only in the configured MinIO deployment.
type S3Adapter struct {
	Endpoint string
	client   *minio.Client
}

// S3Bucket is one bucket as reported by ListBuckets.
type S3Bucket struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// DefaultMinIOEndpoint is used when MINIO_ENDPOINT is unset.
const DefaultMinIOEndpoint = "localhost:9000"

// NewS3Adapter builds an adapter for endpoint (host:port, no scheme;
// useSSL selects https). Creating the client performs no network
// I/O; reachability is established by Health / the first operation.
//
// The endpoint is SSRF-guarded (security.ValidateEndpointHost):
// only loopback hosts or the Docker service name "minio" are
// accepted — a public or LAN address is refused here, at
// construction, instead of at first use.
func NewS3Adapter(endpoint, accessKey, secretKey string, useSSL bool) (*S3Adapter, error) {
	if endpoint == "" {
		endpoint = DefaultMinIOEndpoint
	}
	if err := security.ValidateEndpointHost(endpoint, "minio"); err != nil {
		return nil, fmt.Errorf("s3 endpoint refused (SSRF guard): %w", err)
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client for %s: %w", endpoint, err)
	}
	return &S3Adapter{Endpoint: endpoint, client: client}, nil
}

// NewS3AdapterFromEnv builds an adapter from MINIO_ENDPOINT
// (default localhost:9000), MINIO_ACCESS_KEY / MINIO_SECRET_KEY
// (default minioadmin/minioadmin — the MinIO dev defaults) and
// MINIO_USE_SSL (default false).
func NewS3AdapterFromEnv() (*S3Adapter, error) {
	useSSL := strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true")
	return NewS3Adapter(
		envOr("MINIO_ENDPOINT", DefaultMinIOEndpoint),
		envOr("MINIO_ACCESS_KEY", "minioadmin"),
		envOr("MINIO_SECRET_KEY", "minioadmin"),
		useSSL,
	)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// isUnreachable classifies minio errors: connection-level failures
// (and 503s from the server) mean the backend is down; other errors
// (bad bucket name, auth) are caller errors and are returned as-is.
func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.StatusCode == httpStatusServiceUnavailable || resp.StatusCode == httpStatusBadGateway
	}
	// Non-HTTP errors from minio-go are transport errors.
	return true
}

const (
	httpStatusServiceUnavailable = 503
	httpStatusBadGateway         = 502
)

// Health probes the backend with ListBuckets. It returns
// ("ok", detail) when MinIO answers, ("unavailable", detail) when
// it cannot be reached, and ("degraded", detail) for auth errors
// (the server is up but the configured credentials are rejected).
func (a *S3Adapter) Health(ctx context.Context) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := a.client.ListBuckets(ctx)
	if err == nil {
		return HealthOK, "MinIO ListBuckets succeeded at " + a.Endpoint
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.StatusCode == 401 || resp.StatusCode == 403) {
		return HealthDegraded, fmt.Sprintf("MinIO at %s rejected the configured credentials (%s)", a.Endpoint, resp.Code)
	}
	return HealthUnavailable, fmt.Sprintf("MinIO at %s unreachable: %v", a.Endpoint, err)
}

// CreateBucket creates a bucket. Unreachable backend -> ErrS3Unavailable.
func (a *S3Adapter) CreateBucket(ctx context.Context, name string) error {
	if err := validateBucketName(name); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	if err := a.client.MakeBucket(ctx, name, minio.MakeBucketOptions{}); err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// ListBuckets lists buckets on the MinIO server.
func (a *S3Adapter) ListBuckets(ctx context.Context) ([]S3Bucket, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	buckets, err := a.client.ListBuckets(ctx)
	if err != nil {
		if isUnreachable(err) {
			return nil, fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return nil, err
	}
	out := make([]S3Bucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, S3Bucket{Name: b.Name, CreatedAt: b.CreationDate})
	}
	return out, nil
}

// PutObject stores data as an object.
func (a *S3Adapter) PutObject(ctx context.Context, bucket, key string, data []byte) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if key == "" {
		return errors.New("object key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	_, err := a.client.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	if err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// GetObject fetches an object's bytes.
func (a *S3Adapter) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	if err := validateBucketName(bucket); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, errors.New("object key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	obj, err := a.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if isUnreachable(err) {
			return nil, fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		if isUnreachable(err) {
			return nil, fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return nil, err
	}
	return data, nil
}

// S3Object is one object as reported by ListObjects / StatObject.
// Prefixes ("folders") are reported as objects with IsPrefix=true
// and no size; they are a listing convenience, not stored objects.
type S3Object struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified time.Time         `json:"last_modified,omitempty"`
	ETag         string            `json:"etag,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	IsPrefix     bool              `json:"is_prefix,omitempty"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}

// ListObjects lists objects under prefix (recursive). A prefix
// ending in "/" lists that "folder". Common prefixes are returned as
// IsPrefix entries when the server reports them (non-recursive
// listing is not used; prefixes are derived client-side).
func (a *S3Adapter) ListObjects(ctx context.Context, bucket, prefix string) ([]S3Object, error) {
	if err := validateBucketName(bucket); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	out := []S3Object{}
	seenPrefix := map[string]bool{}
	for info := range a.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if info.Err != nil {
			if isUnreachable(info.Err) {
				return nil, fmt.Errorf("%w: %v", ErrS3Unavailable, info.Err)
			}
			return nil, info.Err
		}
		// Derive "folder" entries from keys below the prefix so the
		// UI can navigate prefixes like the AWS console does.
		rel := strings.TrimPrefix(info.Key, prefix)
		if idx := strings.Index(rel, "/"); idx >= 0 {
			dir := prefix + rel[:idx+1]
			if !seenPrefix[dir] {
				seenPrefix[dir] = true
				out = append(out, S3Object{Key: dir, IsPrefix: true})
			}
			continue
		}
		out = append(out, S3Object{
			Key: info.Key, Size: info.Size, LastModified: info.LastModified,
			ETag: strings.Trim(info.ETag, `"`), ContentType: info.ContentType,
			UserMetadata: info.UserMetadata,
		})
	}
	return out, nil
}

// DeleteObject removes an object. A missing object is a success
// (idempotent delete, like S3).
func (a *S3Adapter) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if key == "" {
		return errors.New("object key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	if err := a.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// StatObject returns an object's metadata (size, content type,
// last-modified, ETag, user metadata).
func (a *S3Adapter) StatObject(ctx context.Context, bucket, key string) (S3Object, error) {
	if err := validateBucketName(bucket); err != nil {
		return S3Object{}, err
	}
	if key == "" {
		return S3Object{}, errors.New("object key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	info, err := a.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isUnreachable(err) {
			return S3Object{}, fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return S3Object{}, err
	}
	return S3Object{
		Key: info.Key, Size: info.Size, LastModified: info.LastModified,
		ETag: strings.Trim(info.ETag, `"`), ContentType: info.ContentType,
		UserMetadata: info.UserMetadata,
	}, nil
}

// GetBucketVersioning reports whether versioning is enabled.
// MinIO returns an empty Status when versioning was never set.
func (a *S3Adapter) GetBucketVersioning(ctx context.Context, bucket string) (bool, error) {
	if err := validateBucketName(bucket); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	cfg, err := a.client.GetBucketVersioning(ctx, bucket)
	if err != nil {
		if isUnreachable(err) {
			return false, fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return false, err
	}
	return cfg.Status == "Enabled", nil
}

// SetBucketVersioning enables ("Enabled") or suspends ("Suspended")
// versioning on a bucket.
func (a *S3Adapter) SetBucketVersioning(ctx context.Context, bucket string, enabled bool) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	status := "Suspended"
	if enabled {
		status = "Enabled"
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	if err := a.client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: status}); err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// GetBucketPolicy returns the bucket policy JSON ("" when none is set).
// MinIO answers NoSuchBucketPolicy with an empty policy; that is
// reported as "" with no error.
func (a *S3Adapter) GetBucketPolicy(ctx context.Context, bucket string) (string, error) {
	if err := validateBucketName(bucket); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	policy, err := a.client.GetBucketPolicy(ctx, bucket)
	if err != nil {
		if isUnreachable(err) {
			return "", fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		var resp minio.ErrorResponse
		if errors.As(err, &resp) && resp.Code == "NoSuchBucketPolicy" {
			return "", nil
		}
		return "", err
	}
	return policy, nil
}

// SetBucketPolicy replaces the bucket policy. An empty policy removes it.
func (a *S3Adapter) SetBucketPolicy(ctx context.Context, bucket, policy string) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	if err := a.client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// RemoveBucket deletes an empty bucket. A non-empty bucket is a
// caller error from MinIO, not an unavailable backend.
func (a *S3Adapter) RemoveBucket(ctx context.Context, bucket string) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout*5)
	defer cancel()
	if err := a.client.RemoveBucket(ctx, bucket); err != nil {
		if isUnreachable(err) {
			return fmt.Errorf("%w: %v", ErrS3Unavailable, err)
		}
		return err
	}
	return nil
}

// validateBucketName enforces the S3 bucket naming rules we rely on
// (lowercase DNS labels, 3–63 chars) so a bad name is a 400, never a
// confusing backend error.
func validateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return errors.New("bucket name must be 3-63 characters")
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.'
		if !ok || (i == 0 && (r == '-' || r == '.')) || (i == len(name)-1 && (r == '-' || r == '.')) {
			return errors.New("bucket name must be lowercase letters, digits, '-' or '.' and must not start or end with '-' or '.'")
		}
	}
	return nil
}
