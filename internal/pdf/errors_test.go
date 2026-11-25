package pdf

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrorsCarryTheAPIMarkers pins the methods internal/httpapi reads
// to classify a render failure without importing this package: a
// missing Typst is 503, a payload the template refuses names its fields.
func TestErrorsCarryTheAPIMarkers(t *testing.T) {
	t.Parallel()

	var unavailable interface{ RendererUnavailable() bool }
	if err := fmt.Errorf("%w: exec: not found", ErrUnavailable); !errors.As(err, &unavailable) || !unavailable.RendererUnavailable() {
		t.Error("a wrapped ErrUnavailable does not mark itself unavailable")
	}
	if !errors.Is(fmt.Errorf("%w: x", ErrUnavailable), ErrUnavailable) {
		t.Error("errors.Is does not find ErrUnavailable")
	}
	if errors.As(fmt.Errorf("%w: x", ErrCompile), &unavailable) {
		t.Error("a compile failure marks itself unavailable")
	}

	var fields interface{ FieldDetails() map[string]string }
	invalid := &InvalidPayloadError{Details: map[string]string{"customer.name": "too long"}}
	if err := fmt.Errorf("render: %w", invalid); !errors.As(err, &fields) || fields.FieldDetails()["customer.name"] != "too long" {
		t.Error("a wrapped InvalidPayloadError does not name its fields")
	}
}
