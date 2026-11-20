package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// testS3Secret is an obviously fake value, used to prove it never reaches
// an error message.
const testS3Secret = "test-only-s3-secret-not-real"

func TestLoadObjectStore(t *testing.T) {
	t.Parallel()

	t.Run("no endpoint leaves the store off and requires versioning", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(validEnv(t, nil))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ObjectStore.Enabled {
			t.Error("Enabled = true without S3_ENDPOINT, want false")
		}
		if !cfg.ObjectStore.RequireVersioning {
			t.Error("RequireVersioning = false, want the default true")
		}
		if cfg.ObjectStore.ObjectLock {
			t.Error("ObjectLock = true, want the default false")
		}
	})

	t.Run("a full configuration", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		secretFile := filepath.Join(dir, "secret")
		writeFile(t, secretFile, testS3Secret+"\n")

		cfg, err := Load(validEnv(t, map[string]string{
			"S3_ENDPOINT":               "http://s3.seaweedfs.svc:8333/",
			"S3_BUCKET":                 "solobill",
			"S3_REGION":                 "eu-central-1",
			"S3_USE_PATH_STYLE":         "true",
			"S3_ACCESS_KEY_ID":          "test-access-key-id",
			"S3_SECRET_ACCESS_KEY_FILE": secretFile,
			"S3_OBJECT_LOCK":            "true",
			"S3_REQUIRE_VERSIONING":     "false",
		}))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		s := cfg.ObjectStore
		if !s.Enabled || s.Endpoint != "http://s3.seaweedfs.svc:8333" || s.Bucket != "solobill" || s.Region != "eu-central-1" {
			t.Errorf("ObjectStore = %+v", s)
		}
		if !s.UsePathStyle || !s.ObjectLock || s.RequireVersioning {
			t.Errorf("flags = path style %v, lock %v, versioning %v", s.UsePathStyle, s.ObjectLock, s.RequireVersioning)
		}
		if s.AccessKeyID != "test-access-key-id" || s.SecretAccessKey != testS3Secret {
			t.Error("credentials were not read from the variable and the file")
		}
	})

	refusals := []struct {
		name string
		env  map[string]string
	}{
		{"endpoint without bucket", map[string]string{"S3_ENDPOINT": "http://s3.example:8333"}},
		{"endpoint with a path", map[string]string{"S3_ENDPOINT": "http://s3.example:8333/solobill", "S3_BUCKET": "b"}},
		{"endpoint with credentials", map[string]string{"S3_ENDPOINT": "http://id:" + testS3Secret + "@s3.example", "S3_BUCKET": "b"}},
		{"ftp endpoint", map[string]string{"S3_ENDPOINT": "ftp://s3.example", "S3_BUCKET": "b"}},
		{"key id without secret", map[string]string{"S3_ENDPOINT": "http://s3.example", "S3_BUCKET": "b", "S3_ACCESS_KEY_ID": "id"}},
		{"secret without key id", map[string]string{"S3_ENDPOINT": "http://s3.example", "S3_BUCKET": "b", "S3_SECRET_ACCESS_KEY": testS3Secret}},
		{"secret and its file", map[string]string{
			"S3_ENDPOINT": "http://s3.example", "S3_BUCKET": "b", "S3_ACCESS_KEY_ID": "id",
			"S3_SECRET_ACCESS_KEY": testS3Secret, "S3_SECRET_ACCESS_KEY_FILE": "/nonexistent",
		}},
		{"lock flag typo", map[string]string{"S3_OBJECT_LOCK": "yes"}},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(validEnv(t, tt.env))
			if err == nil {
				t.Fatal("Load() error = nil, want a refusal")
			}
			if strings.Contains(err.Error(), testS3Secret) {
				t.Errorf("Load() error repeats the secret: %v", err)
			}
		})
	}
}

func TestLoadFilesVerify(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"DATABASE_URL":         "postgres://localhost:5432/solobill?sslmode=disable",
		"DATABASE_USER":        "solobill",
		"DATABASE_PASSWORD":    "a-password",
		"S3_ENDPOINT":          "http://s3.example:8333",
		"S3_BUCKET":            "solobill",
		"S3_ACCESS_KEY_ID":     "id",
		"S3_SECRET_ACCESS_KEY": testS3Secret,
	}
	getenv := func(overrides map[string]string) func(string) string {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range overrides {
			if v == "" {
				delete(env, k)

				continue
			}
			env[k] = v
		}

		return func(key string) string { return env[key] }
	}

	cfg, err := LoadFilesVerify(getenv(nil))
	if err != nil {
		t.Fatalf("LoadFilesVerify() error = %v", err)
	}
	if !cfg.ObjectStore.Enabled || cfg.DatabaseUser != "solobill" {
		t.Errorf("cfg = %+v", cfg.ObjectStore)
	}

	// Neither the session nor the CSRF key is needed.
	if _, err := LoadFilesVerify(getenv(map[string]string{"S3_ENDPOINT": ""})); err == nil {
		t.Error("LoadFilesVerify() without S3_ENDPOINT error = nil, want a refusal")
	}
	if _, err := LoadFilesVerify(getenv(map[string]string{"DATABASE_PASSWORD": ""})); err == nil {
		t.Error("LoadFilesVerify() without DATABASE_PASSWORD error = nil, want a refusal")
	}
}
