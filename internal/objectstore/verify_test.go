package objectstore_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

func TestVerify(t *testing.T) {
	t.Parallel()

	original := []byte("%PDF-1.7 receipt")
	want := sha256Hex(original)

	tests := []struct {
		name    string
		stored  []byte
		wantErr bool
	}{
		{name: "unchanged", stored: original},
		{name: "same size, one byte changed", stored: []byte("%PDF-1.7 receipT"), wantErr: true},
		{name: "truncated", stored: original[:len(original)-1], wantErr: true},
		{name: "appended", stored: append(bytes.Clone(original), 'x'), wantErr: true},
		{name: "empty", stored: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := objectstore.Verify(bytes.NewReader(tt.stored), want, int64(len(original)))
			if tt.wantErr {
				if !errors.Is(err, objectstore.ErrIntegrity) {
					t.Fatalf("Verify() error = %v, want ErrIntegrity", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("Verify() error = %v, want nil", err)
			}
		})
	}
}

func TestKey(t *testing.T) {
	t.Parallel()

	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	file := uuid.MustParse("22222222-2222-4222-8222-222222222222")

	if got, want := objectstore.Key(tenant, file), "11111111-1111-4111-8111-111111111111/22222222-2222-4222-8222-222222222222"; got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestRetainUntil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		uploaded time.Time
		want     time.Time
	}{
		{time.Date(2026, time.November, 20, 10, 0, 0, 0, time.UTC), time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC), time.Date(2037, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		if got := objectstore.RetainUntil(tt.uploaded); !got.Equal(tt.want) {
			t.Errorf("RetainUntil(%v) = %v, want %v", tt.uploaded, got, tt.want)
		}
	}
}
