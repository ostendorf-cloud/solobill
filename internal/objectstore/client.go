// Package objectstore is the S3 adapter behind storage.ObjectStore, built
// on github.com/minio/minio-go/v7 (the client library only, not the MinIO
// server). It works against any S3 compatible store; the deployment
// target is SeaweedFS. See docs/concepts/solobill.md, "Receipt storage and
// GoBD" and "Object store client and configuration".
//
// Three pieces:
//
//   - Client talks to the bucket: Put, Get, Stat and the two bucket checks.
//     It has no Delete, because the application has no delete path.
//   - Monitor checks the bucket at startup and then periodically, keeps the
//     last result and logs a warning while the protection is missing.
//   - Guarded combines both: it is the storage.ObjectStore the handlers
//     get, and it refuses every call with storage.ErrObjectStoreUnavailable
//     while the last check gated the file routes.
//
// PostgreSQL holds the authority. The hash the store keeps is a second
// line only: Verify recomputes the SHA-256 of what a read returns and
// compares it with the one stored in the files row.
package objectstore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Config is what Client needs. cmd/solobill fills it from
// config.ObjectStoreConfig. The credentials never leave this struct and
// the minio client: no method logs or wraps them into an error.
type Config struct {
	// Endpoint is the store's base URL, http or https, without a path,
	// for example "http://s3.seaweedfs.svc:8333".
	Endpoint string
	Bucket   string
	Region   string
	// UsePathStyle addresses the bucket in the path instead of the host
	// name, which SeaweedFS needs.
	UsePathStyle bool
	// AccessKeyID and SecretAccessKey are both empty for an anonymous
	// store, which only the local compose setup uses.
	AccessKeyID     string
	SecretAccessKey string
	// ObjectLock requests compliance mode retention per object (Put's
	// RetainUntil). Without it, RetainUntil is ignored.
	ObjectLock bool
	// Transport replaces the HTTP transport, for tests. Nil uses the
	// minio default.
	Transport http.RoundTripper
}

// Client is the minio-go adapter. It satisfies storage.ObjectStore and
// BucketChecker.
type Client struct {
	client     *minio.Client
	bucket     string
	objectLock bool
}

var _ storage.ObjectStore = (*Client)(nil)

// New builds a Client. It does not contact the store: the first contact is
// the Monitor's check, so the process starts even while the store is down.
func New(cfg Config) (*Client, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		// The parse error repeats the raw URL, which may carry user info.
		return nil, errors.New("objectstore: endpoint is not a valid URL")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("objectstore: endpoint scheme %q, want http or https", endpoint.Scheme)
	}
	if endpoint.Host == "" || strings.Trim(endpoint.Path, "/") != "" || endpoint.RawQuery != "" || endpoint.User != nil {
		return nil, errors.New("objectstore: endpoint must be scheme and host only, without a path, query or user info")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("objectstore: bucket is required")
	}
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		return nil, errors.New("objectstore: access key id and secret access key must both be set or both be empty")
	}

	lookup := minio.BucketLookupAuto
	if cfg.UsePathStyle {
		lookup = minio.BucketLookupPath
	}

	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       endpoint.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: lookup,
		Transport:    cfg.Transport,
		// Needed for the x-amz-checksum-sha256 header Put sends: minio-go
		// refuses a caller supplied checksum without it.
		TrailingHeaders: true,
	})
	if err != nil {
		return nil, fmt.Errorf("objectstore: build client: %w", err)
	}

	return &Client{client: client, bucket: cfg.Bucket, objectLock: cfg.ObjectLock}, nil
}

// Put implements storage.ObjectStore. It uploads in one request, never as
// a multipart upload, so the SHA-256 the caller computed is the checksum
// of the whole object and the store refuses a body that does not match
// it. The checksum header is standard S3; whether SeaweedFS 4.47 verifies
// it is not verified, which is why Verify on every read stays the control.
func (c *Client) Put(ctx context.Context, object storage.PutObject) (storage.ObjectInfo, error) {
	sum, err := hex.DecodeString(object.SHA256)
	if err != nil || len(sum) != 32 {
		return storage.ObjectInfo{}, errors.New("objectstore: put: sha256 must be 64 lowercase hex characters")
	}
	if object.SizeBytes <= 0 {
		return storage.ObjectInfo{}, errors.New("objectstore: put: size must be positive")
	}

	opts := minio.PutObjectOptions{
		ContentType:      object.ContentType,
		DisableMultipart: true,
		UserMetadata: map[string]string{
			"x-amz-checksum-sha256": base64.StdEncoding.EncodeToString(sum),
		},
	}
	lockRequested := c.objectLock && object.RetainUntil != nil
	if lockRequested {
		opts.Mode = minio.Compliance
		opts.RetainUntilDate = object.RetainUntil.UTC()
	}

	uploaded, err := c.client.PutObject(ctx, c.bucket, object.Key, object.Body, object.SizeBytes, opts)
	if err != nil {
		return storage.ObjectInfo{}, fmt.Errorf("objectstore: put %s: %w", object.Key, err)
	}

	info := storage.ObjectInfo{
		SizeBytes:   uploaded.Size,
		ContentType: object.ContentType,
		VersionID:   uploaded.VersionID,
	}
	if uploaded.Size != object.SizeBytes {
		return storage.ObjectInfo{}, fmt.Errorf("objectstore: put %s: store reports %d bytes, sent %d", object.Key, uploaded.Size, object.SizeBytes)
	}

	if lockRequested {
		// Read the retention back instead of trusting the request: a store
		// that ignores the lock headers must not end up with a retain date
		// in the files row that nothing enforces.
		_, until, err := c.client.GetObjectRetention(ctx, c.bucket, object.Key, uploaded.VersionID)
		if err == nil && until != nil {
			retain := until.UTC()
			info.RetainUntil = &retain
		}
	}

	return info, nil
}

// Get implements storage.ObjectStore. It reads the current version of the
// key. The caller verifies the bytes against the files row; see Verify.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	object, err := c.client.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, storage.ObjectInfo{}, mapError("get", key, err)
	}

	// GetObject is lazy: Stat sends the request and reports a missing key.
	stat, err := object.Stat()
	if err != nil {
		_ = object.Close()

		return nil, storage.ObjectInfo{}, mapError("get", key, err)
	}

	return object, infoFromStat(stat), nil
}

// Stat implements storage.ObjectStore.
func (c *Client) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	stat, err := c.client.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return storage.ObjectInfo{}, mapError("stat", key, err)
	}

	return infoFromStat(stat), nil
}

// BucketVersioning implements storage.ObjectStore and BucketChecker. An
// error means the bucket could not be asked, which the Monitor reports as
// unreachable.
func (c *Client) BucketVersioning(ctx context.Context) (bool, error) {
	cfg, err := c.client.GetBucketVersioning(ctx, c.bucket)
	if err != nil {
		return false, fmt.Errorf("objectstore: bucket versioning: %w", err)
	}

	return cfg.Enabled(), nil
}

// BucketObjectLock implements BucketChecker: true when the bucket has
// object lock enabled. A bucket without an object lock configuration is
// false, not an error.
func (c *Client) BucketObjectLock(ctx context.Context) (bool, error) {
	enabled, _, _, _, err := c.client.GetObjectLockConfig(ctx, c.bucket)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "ObjectLockConfigurationNotFoundError" || resp.StatusCode == http.StatusNotFound {
			return false, nil
		}

		return false, fmt.Errorf("objectstore: bucket object lock: %w", err)
	}

	return enabled == "Enabled", nil
}

// mapError turns a missing key into storage.ErrNotFound and wraps
// everything else with the operation and the key. The key is
// "<tenant id>/<file id>", no secret.
func mapError(op, key string, err error) error {
	resp := minio.ToErrorResponse(err)
	if resp.Code == minio.NoSuchKey || (resp.StatusCode == http.StatusNotFound && resp.Code != minio.NoSuchBucket) {
		return fmt.Errorf("objectstore: %s %s: %w", op, key, storage.ErrNotFound)
	}

	return fmt.Errorf("objectstore: %s %s: %w", op, key, err)
}

// lockRetainUntilHeader is where a HEAD or GET reports the retention of
// the object version it returned.
const lockRetainUntilHeader = "X-Amz-Object-Lock-Retain-Until-Date"

func infoFromStat(stat minio.ObjectInfo) storage.ObjectInfo {
	info := storage.ObjectInfo{
		SizeBytes:   stat.Size,
		ContentType: stat.ContentType,
		VersionID:   stat.VersionID,
	}
	if raw := stat.Metadata.Get(lockRetainUntilHeader); raw != "" {
		if until, err := time.Parse(time.RFC3339, raw); err == nil {
			until = until.UTC()
			info.RetainUntil = &until
		}
	}

	return info
}
