package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestFilesCommandUsage(t *testing.T) {
	t.Parallel()

	verified := func(context.Context) error { return nil }
	tests := []struct {
		args     []string
		wantCode int
		wantRun  bool
	}{
		{args: nil, wantCode: 2},
		{args: []string{"-h"}, wantCode: 0},
		{args: []string{"--help"}, wantCode: 0},
		{args: []string{"verify", "--help"}, wantCode: 0},
		{args: []string{"--all"}, wantCode: 2},
		{args: []string{"verify", "--all"}, wantCode: 2},
		{args: []string{"verify", "extra"}, wantCode: 2},
		{args: []string{"repair"}, wantCode: 2},
		{args: []string{"verify"}, wantCode: 0, wantRun: true},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			t.Parallel()

			ran := false
			var out bytes.Buffer
			err := filesCommand(context.Background(), tt.args, &out, func(ctx context.Context) error {
				ran = true

				return verified(ctx)
			})
			code, _ := exitWith(err)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
			if ran != tt.wantRun {
				t.Errorf("verify ran = %v, want %v", ran, tt.wantRun)
			}
		})
	}
}

func TestFilesIsRegistered(t *testing.T) {
	if _, ok := commands["files"]; !ok {
		t.Fatal(`subcommand "files" is not registered`)
	}
}

// fakeCatalog holds the files of each tenant.
type fakeCatalog struct {
	byTenant map[uuid.UUID][]storage.File
	current  uuid.UUID
}

func (c *fakeCatalog) WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	c.current = tenantID

	return fn(ctx)
}

func (c *fakeCatalog) TenantIDs(context.Context) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(c.byTenant))
	for id := range c.byTenant {
		ids = append(ids, id)
	}

	return ids, nil
}

func (c *fakeCatalog) ListFiles(context.Context) ([]storage.File, error) {
	return c.byTenant[c.current], nil
}

type fakeReader map[string][]byte

func (r fakeReader) Get(_ context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	data, ok := r[key]
	if !ok {
		return nil, storage.ObjectInfo{}, storage.ErrNotFound
	}

	return io.NopCloser(bytes.NewReader(data)), storage.ObjectInfo{SizeBytes: int64(len(data))}, nil
}

func storedFile(tenantID uuid.UUID, data []byte) storage.File {
	id := uuid.New()
	sum := sha256.Sum256(data)

	return storage.File{
		ID: id, TenantID: tenantID, ObjectKey: objectstore.Key(tenantID, id),
		SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)),
	}
}

func TestVerifyFiles(t *testing.T) {
	t.Parallel()

	tenantA, tenantB := uuid.New(), uuid.New()
	good := storedFile(tenantA, []byte("%PDF-1.7 good"))
	tampered := storedFile(tenantA, []byte("%PDF-1.7 original"))
	missing := storedFile(tenantB, []byte("%PDF-1.7 gone"))
	foreignKey := storedFile(tenantB, []byte("%PDF-1.7 foreign"))
	foreignKey.ObjectKey = objectstore.Key(tenantA, foreignKey.ID)

	t.Run("all good", func(t *testing.T) {
		t.Parallel()

		catalog := &fakeCatalog{byTenant: map[uuid.UUID][]storage.File{tenantA: {good}}}
		reader := fakeReader{good.ObjectKey: []byte("%PDF-1.7 good")}
		if err := verifyFiles(context.Background(), catalog, reader, slog.New(slog.DiscardHandler)); err != nil {
			t.Fatalf("verifyFiles() error = %v, want nil", err)
		}
	})

	t.Run("a mismatch, a missing object and a foreign key fail with each file id", func(t *testing.T) {
		t.Parallel()

		catalog := &fakeCatalog{byTenant: map[uuid.UUID][]storage.File{
			tenantA: {good, tampered},
			tenantB: {missing, foreignKey},
		}}
		reader := fakeReader{
			good.ObjectKey:       []byte("%PDF-1.7 good"),
			tampered.ObjectKey:   []byte("%PDF-1.7 originaL"),
			foreignKey.ObjectKey: []byte("%PDF-1.7 foreign"),
		}

		var logs bytes.Buffer
		err := verifyFiles(context.Background(), catalog, reader, slog.New(slog.NewTextHandler(&logs, nil)))
		code, msg := exitWith(err)
		if code != 1 || !strings.Contains(msg, "3 of 4") {
			t.Fatalf("exit = %d, %q, want 1 and 3 of 4 failed", code, msg)
		}
		for _, f := range []storage.File{tampered, missing, foreignKey} {
			if !strings.Contains(logs.String(), "file_id="+f.ID.String()) {
				t.Errorf("log does not name file %s:\n%s", f.ID, logs.String())
			}
		}
		if strings.Contains(logs.String(), "file_id="+good.ID.String()) {
			t.Error("log names the good file as failed")
		}
	})

	t.Run("a listing failure is an error", func(t *testing.T) {
		t.Parallel()

		err := verifyFiles(context.Background(), errCatalog{}, fakeReader{}, slog.New(slog.DiscardHandler))
		if err == nil {
			t.Fatal("verifyFiles() error = nil, want the listing error")
		}
	})
}

type errCatalog struct{}

func (errCatalog) WithTenant(ctx context.Context, _ uuid.UUID, fn func(context.Context) error) error {
	return fn(ctx)
}

func (errCatalog) TenantIDs(context.Context) ([]uuid.UUID, error) {
	return nil, errors.New("database down")
}

func (errCatalog) ListFiles(context.Context) ([]storage.File, error) { return nil, nil }
