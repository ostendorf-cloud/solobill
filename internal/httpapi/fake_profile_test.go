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

// fakeSellerProfiles is the in memory SellerProfileRepository of the
// profile tests. The fake store's WithTenant does not carry the tenant
// into the context, so the fake keeps one profile and records the tenant
// id every Put received: the profile handler sets it from the session
// only, and the tests assert exactly that.
type fakeSellerProfiles struct {
	mu         sync.Mutex
	profile    *storage.SellerProfile
	putTenants []uuid.UUID
}

func (f *fakeSellerProfiles) Get(context.Context) (storage.SellerProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.profile == nil {
		return storage.SellerProfile{}, storage.ErrNotFound
	}

	return *f.profile, nil
}

func (f *fakeSellerProfiles) Put(_ context.Context, p storage.SellerProfile) (storage.SellerProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if f.profile != nil {
		p.CreatedAt = f.profile.CreatedAt
	} else {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	f.profile = &p
	f.putTenants = append(f.putTenants, p.TenantID)

	return p, nil
}

// fakeSellerLogos is the SellerProfileLogoRepository the tests flip to
// see has_logo change.
type fakeSellerLogos struct {
	mu     sync.Mutex
	fileID *uuid.UUID
}

func (f *fakeSellerLogos) Logo(context.Context) (*uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.fileID, nil
}

func (f *fakeSellerLogos) SetLogo(_ context.Context, id *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileID = id

	return nil
}

// fakeIssuedDocuments answers only List, which the profile handler reads
// to learn whether a document type already has an issued document.
type fakeIssuedDocuments struct {
	storage.DocumentRepository

	mu        sync.Mutex
	documents []storage.Document
}

func (f *fakeIssuedDocuments) List(context.Context) ([]storage.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]storage.Document(nil), f.documents...), nil
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		if repos.SellerProfiles == nil {
			repos.SellerProfiles = &fakeSellerProfiles{}
		}
		if repos.SellerProfileLogos == nil {
			repos.SellerProfileLogos = &fakeSellerLogos{}
		}
		if repos.Documents == nil {
			repos.Documents = &fakeIssuedDocuments{}
		}
	})
}
