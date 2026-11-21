package httpapi_test

import (
	"context"
	"errors"
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

// profileExtras are repositories this feature does not own. Only a test
// that needs one hands it to newProfileFixture, and the hook below sets it
// for that test's app alone: the hook installs nothing else outside this
// feature's own repositories.
type profileExtras struct {
	logos *fakeSellerLogos
	docs  *fakeIssuedDocuments
}

// profileExtrasByTest maps the *testing.T of a test to its extras.
var profileExtrasByTest sync.Map

// fakeSellerLogos is a SellerProfileLogoRepository (issue #75's
// interface) a test flips to see has_logo change.
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

// fakeIssuedDocuments is a DocumentRepository (issue #70's interface)
// whose List answers a set list. Every other method answers an error, so
// no unexpected call can panic or pass silently.
type fakeIssuedDocuments struct {
	mu        sync.Mutex
	documents []storage.Document
}

var errFakeDocumentsUnsupported = errors.New("fakeIssuedDocuments: method not supported")

func (f *fakeIssuedDocuments) List(context.Context) ([]storage.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]storage.Document(nil), f.documents...), nil
}

func (f *fakeIssuedDocuments) Create(context.Context, storage.Document, []storage.DocumentLine) (storage.Document, error) {
	return storage.Document{}, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) FindByID(context.Context, uuid.UUID) (storage.Document, error) {
	return storage.Document{}, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) Update(context.Context, storage.Document, []storage.DocumentLine) (storage.Document, error) {
	return storage.Document{}, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) DeleteDraft(context.Context, uuid.UUID) error {
	return errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) Lock(context.Context, uuid.UUID) (storage.Document, error) {
	return storage.Document{}, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) Chain(context.Context, uuid.UUID) ([]storage.Document, error) {
	return nil, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) SellerProfileForIssue(context.Context) (storage.SellerProfile, error) {
	return storage.SellerProfile{}, errFakeDocumentsUnsupported
}

func (f *fakeIssuedDocuments) Issue(context.Context, uuid.UUID, storage.DocumentIssue) (storage.Document, error) {
	return storage.Document{}, errFakeDocumentsUnsupported
}

func init() {
	repositoryHooks = append(repositoryHooks, func(t *testing.T, repos *httpapi.Repositories) {
		if repos.SellerProfiles == nil {
			repos.SellerProfiles = &fakeSellerProfiles{}
		}

		if v, ok := profileExtrasByTest.Load(t); ok {
			extras, _ := v.(profileExtras)
			if extras.logos != nil {
				repos.SellerProfileLogos = extras.logos
			}
			if extras.docs != nil {
				repos.Documents = extras.docs
			}
		}
	})
}
