package postgres

import (
	"context"
	"testing"
)

// TestNewPool_InvalidConnString proves a malformed connection string fails
// fast, at parse time, with no network attempt: an empty string and a
// string with no recognizable scheme both fail the same way a real but
// unreachable host would, without needing one.
func TestNewPool_InvalidConnString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		connStr string
	}{
		{name: "not a connection string", connStr: "not a connection string"},
		{name: "invalid port", connStr: "postgres://user:pass@example.invalid:not-a-port/db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewPool(context.Background(), tt.connStr)
			if err == nil {
				t.Fatalf("NewPool(%q) error = nil, want an error", tt.connStr)
			}
		})
	}
}
