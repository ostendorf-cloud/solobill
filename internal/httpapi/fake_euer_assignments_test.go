package httpapi_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeEuerAssignments is the in memory EuerAssignmentRepository of the
// report tests. It mirrors the postgres one: it filters by the tenant the
// handler opened the scope for (httpapi.ScopedTenant), the way row level
// security does, and answers the bare storage.ErrConflict when
// SupersedesID does not name the entry's effective row. It has no Update
// and no Delete, like the table.
type fakeEuerAssignments struct {
	mu    sync.Mutex
	rows  []storage.EuerAssignment
	clock int
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.EuerAssignments = &fakeEuerAssignments{}
	})
}

// assignmentRepo reads the fake back out of the app.
func assignmentRepo(t *testing.T, app testApp) *fakeEuerAssignments {
	t.Helper()

	repo, ok := app.repos.EuerAssignments.(*fakeEuerAssignments)
	if !ok {
		t.Fatalf("EuerAssignments = %T, want the fake", app.repos.EuerAssignments)
	}

	return repo
}

// all returns every stored row of every tenant, for assertions.
func (f *fakeEuerAssignments) all() []storage.EuerAssignment {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]storage.EuerAssignment(nil), f.rows...)
}

func (f *fakeEuerAssignments) superseded(id uuid.UUID) bool {
	for _, r := range f.rows {
		if r.SupersedesID != nil && *r.SupersedesID == id {
			return true
		}
	}

	return false
}

func (f *fakeEuerAssignments) Create(ctx context.Context, a storage.EuerAssignment) (storage.EuerAssignment, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.EuerAssignment{}, storage.ErrNoTenantScope
	}
	if a.TenantID != tenant {
		return storage.EuerAssignment{}, storage.ErrConflict
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var effective *storage.EuerAssignment
	for i, r := range f.rows {
		if r.TenantID == tenant && r.EntryKind == a.EntryKind && r.EntryID == a.EntryID && !f.superseded(r.ID) {
			effective = &f.rows[i]
		}
	}
	switch {
	case effective == nil && a.SupersedesID != nil,
		effective != nil && (a.SupersedesID == nil || *a.SupersedesID != effective.ID):
		return storage.EuerAssignment{}, storage.ErrConflict
	}

	f.clock++
	a.ID = uuid.New()
	a.CreatedAt = time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(f.clock) * time.Second)
	f.rows = append(f.rows, a)

	return a, nil
}

func (f *fakeEuerAssignments) ListEffective(ctx context.Context) ([]storage.EuerAssignment, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var out []storage.EuerAssignment
	for _, r := range f.rows {
		if r.TenantID == tenant && !f.superseded(r.ID) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })

	return out, nil
}
