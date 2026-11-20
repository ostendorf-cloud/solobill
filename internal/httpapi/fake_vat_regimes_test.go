package httpapi_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeVATRegimes is the in memory VATRegimePeriodRepository of the VAT
// regime tests. It follows the interface's own contract for ErrConflict
// and ErrNotFound. Like fakeSellerProfiles it holds one tenant's rows.
type fakeVATRegimes struct {
	mu      sync.Mutex
	periods []storage.VATRegimePeriod
	created int
}

func (f *fakeVATRegimes) superseded(id uuid.UUID) bool {
	for _, p := range f.periods {
		if p.SupersedesID != nil && *p.SupersedesID == id {
			return true
		}
	}

	return false
}

func (f *fakeVATRegimes) List(context.Context) ([]storage.VATRegimePeriod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]storage.VATRegimePeriod(nil), f.periods...), nil
}

func (f *fakeVATRegimes) Create(_ context.Context, p storage.VATRegimePeriod) (storage.VATRegimePeriod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if p.SupersedesID != nil {
		found := false
		for _, existing := range f.periods {
			found = found || existing.ID == *p.SupersedesID
		}
		if !found {
			return storage.VATRegimePeriod{}, storage.ErrNotFound
		}
		if f.superseded(*p.SupersedesID) {
			return storage.VATRegimePeriod{}, storage.ErrConflict
		}
	}
	for _, existing := range f.periods {
		isSuperseded := p.SupersedesID != nil && existing.ID == *p.SupersedesID
		if existing.ValidFrom.Equal(p.ValidFrom) && !f.superseded(existing.ID) && !isSuperseded {
			return storage.VATRegimePeriod{}, storage.ErrConflict
		}
	}

	f.created++
	p.ID = uuid.New()
	p.CreatedAt = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Add(time.Duration(f.created) * time.Second)
	f.periods = append(f.periods, p)

	return p, nil
}

func (f *fakeVATRegimes) RegimeOn(_ context.Context, date time.Time) (storage.VATRegimePeriod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var best *storage.VATRegimePeriod
	for i, p := range f.periods {
		if p.ValidFrom.After(date) || f.superseded(p.ID) {
			continue
		}
		if best == nil || !p.ValidFrom.Before(best.ValidFrom) {
			best = &f.periods[i]
		}
	}
	if best == nil {
		return storage.VATRegimePeriod{}, storage.ErrNotFound
	}

	return *best, nil
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		if repos.VATRegimePeriods == nil {
			repos.VATRegimePeriods = &fakeVATRegimes{}
		}
	})
}
