package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a test double for a MinIO server: it speaks just enough
// of the S3 HTTP API (ListBuckets XML, MakeBucket, Put/GetObject) for
// minio-go to exercise the adapter. It is NOT a real S3 server and
// must never be mistaken for one.
type fakeS3 struct {
	mu      sync.Mutex
	buckets map[string]map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
			return
		}
		if r.URL.Path == "/" {
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>test</ID><DisplayName>test</DisplayName></Owner><Buckets>`)
			for name := range f.buckets {
				b.WriteString("<Bucket><Name>" + name + "</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>")
			}
			b.WriteString(`</Buckets></ListAllMyBucketsResult>`)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(b.String()))
			return
		}
		if len(parts) == 2 {
			if objs, ok := f.buckets[parts[0]]; ok {
				if data, ok := objs[parts[1]]; ok {
					w.Header().Set("Content-Length", itoa(len(data)))
					w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
					w.Header().Set("ETag", `"fake-etag"`)
					_, _ = w.Write(data)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code></Error>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case http.MethodPut:
		if len(parts) == 1 && parts[0] != "" {
			if _, ok := f.buckets[parts[0]]; ok {
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.buckets[parts[0]] = map[string][]byte{}
			w.WriteHeader(http.StatusOK)
			return
		}
		if len(parts) == 2 {
			if _, ok := f.buckets[parts[0]]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			data, _ := io.ReadAll(r.Body)
			// minio-go signs streaming uploads with
			// Content-Encoding: aws-chunked (signalled by
			// X-Amz-Content-Sha256: STREAMING-...); decode the
			// framing so the stored object is the real payload.
			if r.Header.Get("Content-Encoding") == "aws-chunked" ||
				strings.Contains(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING") {
				data = decodeAWSChunked(data)
			}
			f.buckets[parts[0]][parts[1]] = data
			w.Header().Set("ETag", `"fake-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// decodeAWSChunked decodes the aws-chunked framing minio-go uses
// for streaming-signed uploads: "<hex-len>[;chunk-signature=...]\r\n
// <bytes>\r\n" repeated, terminated by a zero-length chunk.
func decodeAWSChunked(data []byte) []byte {
	var out []byte
	for len(data) > 0 {
		i := strings.Index(string(data), "\r\n")
		if i < 0 {
			return out
		}
		header := string(data[:i])
		if j := strings.Index(header, ";"); j >= 0 {
			header = header[:j]
		}
		var n int
		if _, err := fmt.Sscanf(header, "%x", &n); err != nil || n == 0 {
			return out
		}
		data = data[i+2:]
		if len(data) < n {
			return out
		}
		out = append(out, data[:n]...)
		data = data[n:]
		if len(data) >= 2 && data[0] == '\r' && data[1] == '\n' {
			data = data[2:]
		}
	}
	return out
}

func itoa(n int) string {
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

func TestS3AdapterAgainstFakeServer(t *testing.T) {
	fake := &fakeS3{buckets: map[string]map[string][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	a, err := NewS3Adapter(endpoint, "test", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if st, _ := a.Health(ctx); st != HealthOK {
		t.Fatalf("health = %q, want ok", st)
	}
	if err := a.CreateBucket(ctx, "my-bucket"); err != nil {
		t.Fatal(err)
	}
	if err := a.PutObject(ctx, "my-bucket", "hello.txt", []byte("hello cloudforge")); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetObject(ctx, "my-bucket", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello cloudforge" {
		t.Fatalf("GetObject = %q", got)
	}
	buckets, err := a.ListBuckets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0].Name != "my-bucket" {
		t.Fatalf("ListBuckets = %+v", buckets)
	}
	// Invalid bucket name is a caller error, not a backend error.
	if err := a.CreateBucket(ctx, "BAD NAME"); !strings.Contains(err.Error(), "bucket name") {
		t.Fatalf("bad bucket name err = %v", err)
	}
}

func TestS3AdapterUnreachable(t *testing.T) {
	a, err := NewS3Adapter("127.0.0.1:1", "test", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if st, detail := a.Health(ctx); st != HealthUnavailable || detail == "" {
		t.Fatalf("health = %q %q, want unavailable with detail", st, detail)
	}
	if err := a.CreateBucket(ctx, "my-bucket"); !errors.Is(err, ErrS3Unavailable) {
		t.Fatalf("CreateBucket against a dead endpoint must be ErrS3Unavailable, got %v", err)
	}
}
