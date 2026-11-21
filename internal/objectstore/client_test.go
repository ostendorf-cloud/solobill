package objectstore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeS3 is the handful of S3 calls Client makes, path style, with one
// bucket "receipts". It records the headers of the last PUT.
type fakeS3 struct {
	mu         sync.Mutex
	objects    map[string][]byte
	putHeaders http.Header
	versioning string
	lock       string // "", "Enabled"; "" answers ObjectLockConfigurationNotFoundError
	// versions holds stored versions under "<key>@<version id>".
	versions       map[string][]byte
	retentionFails bool
}

const testBucket = "receipts"

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != testBucket {
		writeS3NotFound(w, "NoSuchBucket")

		return
	}

	query := r.URL.Query()
	switch {
	case key == "" && query.Has("versioning"):
		_, _ = io.WriteString(w, `<VersioningConfiguration><Status>`+f.versioning+`</Status></VersioningConfiguration>`)
	case key == "" && query.Has("object-lock"):
		if f.lock == "" {
			writeS3NotFound(w, "ObjectLockConfigurationNotFoundError")

			return
		}
		_, _ = io.WriteString(w, `<ObjectLockConfiguration><ObjectLockEnabled>`+f.lock+`</ObjectLockEnabled></ObjectLockConfiguration>`)
	case key != "" && query.Has("retention"):
		if f.retentionFails {
			writeS3NotFound(w, "NoSuchObjectLockConfiguration")

			return
		}
		_, _ = io.WriteString(w, `<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>2037-01-01T00:00:00Z</RetainUntilDate></Retention>`)
	case key != "" && query.Has("versionId") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		version := query.Get("versionId")
		body, ok := f.versions[key+"@"+version]
		if !ok {
			writeS3NotFound(w, "NoSuchVersion")

			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("X-Amz-Version-Id", version)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case key != "" && r.Method == http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		f.objects[key] = body
		f.putHeaders = r.Header.Clone()
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("X-Amz-Version-Id", "v1")
	case key != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		body, ok := f.objects[key]
		if !ok {
			writeS3NotFound(w, "NoSuchKey")

			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("X-Amz-Version-Id", "v1")
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

// writeS3NotFound answers a 404 S3 error with code, the only error status
// the fake needs.
func writeS3NotFound(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `<Error><Code>`+code+`</Code><Message>test</Message></Error>`)
}

// testSecretAccessKey is an obviously fake key, used to prove it never
// reaches a log line.
const testSecretAccessKey = "test-secret-not-real"

func newTestClient(t *testing.T, s3 *fakeS3, objectLock bool) *objectstore.Client {
	t.Helper()

	return newLoggedTestClient(t, s3, objectLock, nil)
}

func newLoggedTestClient(t *testing.T, s3 *fakeS3, objectLock bool, logger *slog.Logger) *objectstore.Client {
	t.Helper()

	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)

	client, err := objectstore.New(objectstore.Config{
		Endpoint:        srv.URL,
		Bucket:          testBucket,
		Region:          "us-east-1",
		UsePathStyle:    true,
		AccessKeyID:     "test-access-key-id",
		SecretAccessKey: testSecretAccessKey,
		ObjectLock:      objectLock,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return client
}

func TestClientPutWarnsWhenTheRetentionCannotBeReadBack(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	s3 := &fakeS3{objects: map[string][]byte{}, retentionFails: true}
	client := newLoggedTestClient(t, s3, true, slog.New(slog.NewTextHandler(&logs, nil)))

	body := []byte("%PDF-1.7")
	retain := time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)
	info, err := client.Put(context.Background(), storage.PutObject{
		Key: "t/f", Body: bytes.NewReader(body), SizeBytes: int64(len(body)),
		ContentType: "application/pdf", SHA256: sha256Hex(body), RetainUntil: &retain,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if info.RetainUntil != nil {
		t.Errorf("RetainUntil = %v, want nil when the store does not confirm it", info.RetainUntil)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "key=t/f") {
		t.Errorf("log = %q, want a WARN line naming the key", out)
	}
	if strings.Contains(out, testSecretAccessKey) {
		t.Error("the warning carries the secret access key")
	}
}

func TestClientGetVersionReadsTheRecordedVersion(t *testing.T) {
	t.Parallel()

	original := []byte("%PDF-1.7 original")
	s3 := &fakeS3{
		objects:  map[string][]byte{"t/f": []byte("%PDF-1.7 overwritten")},
		versions: map[string][]byte{"t/f@v1": original},
	}
	client := newTestClient(t, s3, false)
	ctx := context.Background()

	rc, _, err := client.GetVersion(ctx, "t/f", "v1")
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(got, original) {
		t.Errorf("GetVersion() = %q, %v, want the recorded version %q", got, err, original)
	}

	if _, _, err := client.GetVersion(ctx, "t/f", "v2"); err == nil {
		t.Error("GetVersion() of an unknown version error = nil, want an error")
	}
	if _, _, err := client.GetVersion(ctx, "t/f", ""); err == nil {
		t.Error("GetVersion() with an empty version error = nil, want an error")
	}
}

func TestClientPutSendsTheChecksumAndRetention(t *testing.T) {
	t.Parallel()

	s3 := &fakeS3{objects: map[string][]byte{}}
	client := newTestClient(t, s3, true)

	body := []byte("%PDF-1.7 a receipt")
	sum := sha256.Sum256(body)
	retain := time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)

	info, err := client.Put(context.Background(), storage.PutObject{
		Key:         "t/f",
		Body:        bytes.NewReader(body),
		SizeBytes:   int64(len(body)),
		ContentType: "application/pdf",
		SHA256:      sha256Hex(body),
		RetainUntil: &retain,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if got := s3.putHeaders.Get("X-Amz-Checksum-Sha256"); got != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Errorf("x-amz-checksum-sha256 = %q, want the base64 of the sum", got)
	}
	if got := s3.putHeaders.Get("X-Amz-Object-Lock-Mode"); got != "COMPLIANCE" {
		t.Errorf("x-amz-object-lock-mode = %q, want COMPLIANCE", got)
	}
	if got := s3.putHeaders.Get("X-Amz-Object-Lock-Retain-Until-Date"); got != "2037-01-01T00:00:00Z" {
		t.Errorf("x-amz-object-lock-retain-until-date = %q, want 2037-01-01T00:00:00Z", got)
	}
	// Over plain HTTP minio-go signs the body in aws-chunked framing, which
	// a real store decodes and this fake does not.
	if !bytes.Contains(s3.objects["t/f"], body) {
		t.Errorf("stored body = %q, want it to carry %q", s3.objects["t/f"], body)
	}
	if info.VersionID != "v1" {
		t.Errorf("VersionID = %q, want v1", info.VersionID)
	}
	if info.RetainUntil == nil || !info.RetainUntil.Equal(retain) {
		t.Errorf("RetainUntil = %v, want %v", info.RetainUntil, retain)
	}
}

func TestClientPutWithoutObjectLockRequestsNoRetention(t *testing.T) {
	t.Parallel()

	s3 := &fakeS3{objects: map[string][]byte{}}
	client := newTestClient(t, s3, false)

	body := []byte("%PDF-1.7")
	retain := time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)
	info, err := client.Put(context.Background(), storage.PutObject{
		Key: "t/f", Body: bytes.NewReader(body), SizeBytes: int64(len(body)),
		ContentType: "application/pdf", SHA256: sha256Hex(body), RetainUntil: &retain,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := s3.putHeaders.Get("X-Amz-Object-Lock-Mode"); got != "" {
		t.Errorf("x-amz-object-lock-mode = %q, want none", got)
	}
	if info.RetainUntil != nil {
		t.Errorf("RetainUntil = %v, want nil", info.RetainUntil)
	}
}

func TestClientPutRefusesABadHash(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, &fakeS3{objects: map[string][]byte{}}, false)
	_, err := client.Put(context.Background(), storage.PutObject{
		Key: "t/f", Body: strings.NewReader("x"), SizeBytes: 1, SHA256: "not-hex",
	})
	if err == nil {
		t.Fatal("Put() error = nil, want an error for a malformed sha256")
	}
}

func TestClientGetAndStat(t *testing.T) {
	t.Parallel()

	s3 := &fakeS3{objects: map[string][]byte{"t/f": []byte("%PDF-1.7")}}
	client := newTestClient(t, s3, false)
	ctx := context.Background()

	rc, info, err := client.Get(ctx, "t/f")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(got) != "%PDF-1.7" || info.SizeBytes != 8 {
		t.Errorf("Get() = %q, %+v, %v", got, info, err)
	}

	if _, _, err := client.Get(ctx, "t/missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := client.Stat(ctx, "t/missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat(missing) error = %v, want ErrNotFound", err)
	}
	if info, err := client.Stat(ctx, "t/f"); err != nil || info.SizeBytes != 8 {
		t.Errorf("Stat() = %+v, %v", info, err)
	}
}

func TestClientBucketChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		versioning     string
		lock           string
		wantVersioning bool
		wantLock       bool
	}{
		{name: "both", versioning: "Enabled", lock: "Enabled", wantVersioning: true, wantLock: true},
		{name: "suspended, no lock configuration", versioning: "Suspended", lock: ""},
		{name: "never versioned", versioning: "", lock: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, &fakeS3{objects: map[string][]byte{}, versioning: tt.versioning, lock: tt.lock}, false)
			ctx := context.Background()

			versioning, err := client.BucketVersioning(ctx)
			if err != nil || versioning != tt.wantVersioning {
				t.Errorf("BucketVersioning() = %v, %v, want %v", versioning, err, tt.wantVersioning)
			}
			lock, err := client.BucketObjectLock(ctx)
			if err != nil || lock != tt.wantLock {
				t.Errorf("BucketObjectLock() = %v, %v, want %v", lock, err, tt.wantLock)
			}
		})
	}
}

func TestNewRefusesBadConfig(t *testing.T) {
	t.Parallel()

	valid := objectstore.Config{Endpoint: "http://s3.example:8333", Bucket: "b"}
	tests := []struct {
		name   string
		mutate func(c *objectstore.Config)
	}{
		{"ftp scheme", func(c *objectstore.Config) { c.Endpoint = "ftp://s3.example" }},
		{"path", func(c *objectstore.Config) { c.Endpoint = "http://s3.example/bucket" }},
		{"user info", func(c *objectstore.Config) { c.Endpoint = "http://key:secret@s3.example" }},
		{"no bucket", func(c *objectstore.Config) { c.Bucket = "" }},
		{"key without secret", func(c *objectstore.Config) { c.AccessKeyID = "id" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)
			_, err := objectstore.New(cfg)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if strings.Contains(err.Error(), "secret") && strings.Contains(cfg.Endpoint, "secret") {
				t.Errorf("New() error %q repeats the endpoint's user info", err)
			}
		})
	}
	if _, err := objectstore.New(valid); err != nil {
		t.Errorf("New(valid) error = %v", err)
	}
}
