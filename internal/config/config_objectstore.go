package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/ostendorf-cloud/govue/envconf"
	"github.com/ostendorf-cloud/govue/logging"
)

// defaultS3Region is the region a SeaweedFS S3 gateway answers to unless
// it is configured otherwise.
const defaultS3Region = "us-east-1"

// ObjectStoreConfig is the S3 compatible store receipts, originals and
// the logo live in. The variable names are fixed by
// docs/concepts/solobill.md, "Object store client and configuration".
//
// No S3_ENDPOINT is a valid configuration: the process starts, the file
// routes answer 503 object_store_unavailable and GET /api/v1/status
// reports the bucket unreachable. The concept's rule "the process always
// starts" covers a missing store the same way as a store without
// versioning or object lock.
type ObjectStoreConfig struct {
	// Enabled is true exactly when S3_ENDPOINT is set.
	Enabled bool

	// Endpoint is the store's base URL, scheme and host only.
	Endpoint string
	Bucket   string
	Region   string
	// UsePathStyle addresses the bucket in the path (SeaweedFS needs it).
	UsePathStyle bool

	// AccessKeyID and SecretAccessKey come from S3_ACCESS_KEY_ID and
	// S3_SECRET_ACCESS_KEY or their _FILE twins. Both empty is an anonymous
	// store, which only the local compose setup uses.
	AccessKeyID     string
	SecretAccessKey string

	// ObjectLock requests compliance mode retention per object and gates
	// the file routes while the bucket has no object lock.
	ObjectLock bool
	// RequireVersioning gates the file routes while the bucket has no
	// versioning. Default true.
	RequireVersioning bool
}

// loadObjectStore reads the S3_* variables into cfg and reports every
// problem through fail. A secret value never reaches a message.
func loadObjectStore(l *envconf.Loader, cfg *ObjectStoreConfig, fail func(error)) {
	cfg.ObjectLock = l.Bool("S3_OBJECT_LOCK", false)
	cfg.RequireVersioning = l.Bool("S3_REQUIRE_VERSIONING", true)

	if l.String("S3_ENDPOINT", "") == "" {
		return
	}
	cfg.Enabled = true

	if endpoint := l.URL("S3_ENDPOINT", ""); endpoint != nil {
		if endpoint.Path != "" {
			fail(errors.New("S3_ENDPOINT: must be scheme and host only, without a path"))
		}
		cfg.Endpoint = endpoint.String()
	}
	cfg.Bucket = l.Required("S3_BUCKET")
	cfg.Region = l.String("S3_REGION", defaultS3Region)
	cfg.UsePathStyle = l.Bool("S3_USE_PATH_STYLE", true)

	cfg.AccessKeyID = l.SecretOrFile("S3_ACCESS_KEY_ID")
	cfg.SecretAccessKey = l.SecretOrFile("S3_SECRET_ACCESS_KEY")
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		fail(errors.New("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY (or their _FILE variants) must either both be set or both be empty"))
	}
}

// FilesVerifyConfig is what "solobill files verify" needs: the database
// and the object store, nothing of the HTTP server. The CronJob running it
// therefore needs neither the session nor the CSRF key.
type FilesVerifyConfig struct {
	LogLevel         slog.Level
	DatabaseURL      string
	DatabaseUser     string
	DatabasePassword string
	ObjectStore      ObjectStoreConfig
}

// LoadFilesVerify loads FilesVerifyConfig from getenv (os.Getenv when nil)
// and reports every problem at once, like Load. S3_ENDPOINT is required
// here: a scan without a store has nothing to compare.
func LoadFilesVerify(getenv func(string) string) (FilesVerifyConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	l := envconf.New(getenv)

	var problems []error
	fail := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}

	var cfg FilesVerifyConfig

	level, err := logging.Level(l.String("LOG_LEVEL", "info"))
	fail(err)
	cfg.LogLevel = level

	cfg.DatabaseURL = l.Required("DATABASE_URL")
	cfg.DatabaseUser = l.Required("DATABASE_USER")
	cfg.DatabasePassword = l.RequiredSecretOrFile("DATABASE_PASSWORD")

	loadObjectStore(l, &cfg.ObjectStore, fail)
	if !cfg.ObjectStore.Enabled {
		fail(errors.New("S3_ENDPOINT: is required for files verify"))
	}

	if err := errors.Join(append(problems, l.Err())...); err != nil {
		return FilesVerifyConfig{}, fmt.Errorf("config: load files verify: %w", err)
	}

	return cfg, nil
}
