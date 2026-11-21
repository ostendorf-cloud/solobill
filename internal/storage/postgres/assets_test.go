package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// recordingTx is a pgx.Tx that records every statement and answers each
// with no row. Only QueryRow is implemented: it is all the asset
// repository's :one queries use. Any other method panics on the nil
// embedded interface, which fails the test loudly.
type recordingTx struct {
	pgx.Tx

	mu         sync.Mutex
	statements []string
	// existing makes GetAsset find a row, so a refusal is a conflict and
	// not "not found".
	existing bool
}

type noRow struct{ found bool }

func (r noRow) Scan(...any) error {
	if r.found {
		return nil
	}

	return pgx.ErrNoRows
}

func (tx *recordingTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	tx.statements = append(tx.statements, sql)
	// sqlc prefixes every statement with its "-- name: X" comment.
	if strings.Contains(sql, "name: GetAsset ") {
		return noRow{found: tx.existing}
	}

	return noRow{}
}

func (tx *recordingTx) sentUpdate() bool {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	for _, s := range tx.statements {
		if strings.Contains(s, "UPDATE assets SET") {
			return true
		}
	}

	return false
}

// TestAssetUpdateChecksTheReplacementYearBeforeSending pins the Go side of
// the PUT rule, without a database: a replacement whose first schedule
// year has ended in Germany is refused before any UPDATE is sent. The
// UPDATE's own WHERE only sees the stored row, so without this check a
// replacement moved into a closed year would be written.
func TestAssetUpdateChecksTheReplacementYearBeforeSending(t *testing.T) {
	t.Parallel()

	day := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}

		return d
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	opening := int32(2026)

	tests := []struct {
		name       string
		acquired   string
		opening    *int32
		now        time.Time
		wantUpdate bool
	}{
		{"acquired in an ended year", "2025-12-31", nil, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), false},
		{"acquired in the current year", "2026-01-05", nil, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), true},
		// 23:30 UTC on 31 December is 00:30 on 1 January in Berlin: 2026
		// has ended there.
		{"31 December 23:30 UTC is 2027 in Berlin", "2026-12-01", nil, time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC), false},
		{"1 January 00:30 Berlin", "2026-12-01", nil, time.Date(2027, 1, 1, 0, 30, 0, 0, berlin), false},
		{"31 December 22:30 UTC is still 2026 in Berlin", "2026-12-01", nil, time.Date(2026, 12, 31, 22, 30, 0, 0, time.UTC), true},
		{"an imported asset's opening year counts", "2024-07-01", &opening, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &recordingTx{existing: true}
			ctx := context.WithValue(context.Background(), txContextKey{}, pgx.Tx(tx))
			life := int32(36)

			_, err := (&assetRepository{}).Update(ctx, storage.Asset{
				ID: uuid.New(), Name: "Laptop", AcquiredOn: day(tt.acquired), CostBasisCents: 149900,
				Method: storage.DepreciationLinear, Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: &life,
				OpeningBookValueYear: tt.opening,
			}, tt.now)

			if got := tx.sentUpdate(); got != tt.wantUpdate {
				t.Errorf("UPDATE sent = %v, want %v (statements %d)", got, tt.wantUpdate, len(tx.statements))
			}
			// Both paths end in a conflict here: the fake answers the
			// UPDATE with no row and GetAsset with one.
			if !errors.Is(err, storage.ErrConflict) {
				t.Errorf("Update() error = %v, want ErrConflict", err)
			}
		})
	}
}
