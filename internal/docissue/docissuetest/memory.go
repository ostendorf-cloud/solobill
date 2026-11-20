// Package docissuetest is an in memory storage.DocumentRepository and
// storage.DocumentNumberRepository for the tests of internal/docissue and
// internal/httpapi. It mirrors what internal/storage/postgres and the
// migrations guarantee: tenant scoping, an issued document refusing
// every write, the unique display number per tenant, the unique number
// per type and year outside corrections, the unique correction sequence
// per chain and a counter per tenant, type and year. It has no
// transactions: a test of the rollback runs against PostgreSQL.
package docissuetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// ErrWrongTenant is a write of a row of another tenant than the scope's,
// which row level security's WITH CHECK refuses.
var ErrWrongTenant = errors.New("docissuetest: row of another tenant")

type counterKey struct {
	tenant uuid.UUID
	typ    storage.DocumentType
	year   int
}

// Store is the in memory store. The zero value is not usable; call New.
type Store struct {
	mu       sync.Mutex
	tenant   func(ctx context.Context) (uuid.UUID, bool)
	profile  func(ctx context.Context) (storage.SellerProfile, error)
	docs     map[uuid.UUID]storage.Document
	counters map[counterKey]int64
	created  int
	locks    []uuid.UUID
}

// New returns an empty store. tenant reads the scope's tenant from a
// context; a context without one fails with storage.ErrNoTenantScope, as
// the real repositories do. profile answers SellerProfileForIssue.
func New(tenant func(ctx context.Context) (uuid.UUID, bool), profile func(ctx context.Context) (storage.SellerProfile, error)) *Store {
	return &Store{
		tenant:   tenant,
		profile:  profile,
		docs:     map[uuid.UUID]storage.Document{},
		counters: map[counterKey]int64{},
	}
}

func (s *Store) scope(ctx context.Context) (uuid.UUID, error) {
	t, ok := s.tenant(ctx)
	if !ok {
		return uuid.Nil, storage.ErrNoTenantScope
	}

	return t, nil
}

func cloneDoc(d storage.Document, withLines bool) storage.Document {
	if withLines {
		d.Lines = append([]storage.DocumentLine(nil), d.Lines...)
	} else {
		d.Lines = nil
	}
	d.FrozenPayload = append([]byte(nil), d.FrozenPayload...)
	if len(d.FrozenPayload) == 0 {
		d.FrozenPayload = nil
	}

	return d
}

func withLines(tenant, docID uuid.UUID, lines []storage.DocumentLine, at time.Time) []storage.DocumentLine {
	out := make([]storage.DocumentLine, len(lines))
	for i, l := range lines {
		l.ID = uuid.New()
		l.TenantID = tenant
		l.DocumentID = docID
		l.Position = int32(i) //nolint:gosec // a test slice index
		l.CreatedAt, l.UpdatedAt = at, at
		out[i] = l
	}

	return out
}

func (s *Store) stamp() time.Time {
	s.created++

	return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Add(time.Duration(s.created) * time.Second)
}

// Put stores d as it is, issued or not, with its lines, for a test that
// needs a row the API cannot produce. It returns d with an id.
func (s *Store) Put(d storage.Document) storage.Document {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	at := s.stamp()
	if d.CreatedAt.IsZero() {
		d.CreatedAt, d.UpdatedAt = at, at
	}
	d.Lines = withLines(d.TenantID, d.ID, d.Lines, at)
	s.docs[d.ID] = cloneDoc(d, true)

	return d
}

// Get returns a stored row whatever its tenant, for assertions.
func (s *Store) Get(id uuid.UUID) (storage.Document, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.docs[id]

	return cloneDoc(d, true), ok
}

// Counter returns a counter whatever the scope, for assertions.
func (s *Store) Counter(tenant uuid.UUID, docType storage.DocumentType, year int) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counters[counterKey{tenant, docType, year}]
}

// Locked returns every id Lock was called with, in order.
func (s *Store) Locked() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]uuid.UUID(nil), s.locks...)
}

// Create implements storage.DocumentRepository.
func (s *Store) Create(ctx context.Context, d storage.Document, lines []storage.DocumentLine) (storage.Document, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if d.TenantID != tenant {
		return storage.Document{}, ErrWrongTenant
	}
	if err := s.checkReferences(d); err != nil {
		return storage.Document{}, err
	}
	d.ID = uuid.New()
	d.State = storage.DocumentStateDraft
	if d.Language == "" {
		d.Language = storage.DocumentLanguageGerman
	}
	at := s.stamp()
	d.CreatedAt, d.UpdatedAt = at, at
	d.Lines = withLines(tenant, d.ID, lines, at)
	s.docs[d.ID] = cloneDoc(d, true)

	return cloneDoc(d, true), nil
}

// checkReferences mirrors the composite foreign keys and the
// documents_check_references trigger.
func (s *Store) checkReferences(d storage.Document) error {
	ref := func(id *uuid.UUID) (storage.Document, bool) {
		if id == nil {
			return storage.Document{}, true
		}
		r, ok := s.docs[*id]
		if !ok || r.TenantID != d.TenantID {
			return storage.Document{}, false
		}

		return r, true
	}
	root, ok := ref(d.CorrectsDocumentID)
	if !ok || (d.CorrectsDocumentID != nil && (root.Type != storage.DocumentTypeInvoice || root.State != storage.DocumentStateIssued)) {
		return storage.ErrConflict
	}
	offer, ok := ref(d.SourceOfferID)
	if !ok || (d.SourceOfferID != nil && offer.Type != storage.DocumentTypeOffer) {
		return storage.ErrConflict
	}
	if _, ok := ref(d.CorrectionBaseID); !ok {
		return storage.ErrConflict
	}

	return nil
}

func (s *Store) find(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	d, ok := s.docs[id]
	if !ok || d.TenantID != tenant {
		return storage.Document{}, storage.ErrNotFound
	}

	return d, nil
}

// FindByID implements storage.DocumentRepository.
func (s *Store) FindByID(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.find(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}

	return cloneDoc(d, true), nil
}

// List implements storage.DocumentRepository: newest first, no lines.
func (s *Store) List(ctx context.Context) ([]storage.Document, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []storage.Document
	for _, d := range s.docs {
		if d.TenantID == tenant {
			out = append(out, cloneDoc(d, false))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })

	return out, nil
}

// Update implements storage.DocumentRepository. It writes the header
// fields UpdateDocument names and replaces the lines; an issued row is
// refused as the trigger refuses it.
func (s *Store) Update(ctx context.Context, d storage.Document, lines []storage.DocumentLine) (storage.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.find(ctx, d.ID)
	if err != nil {
		return storage.Document{}, err
	}
	if existing.State == storage.DocumentStateIssued {
		return storage.Document{}, storage.ErrConflict
	}

	existing.CustomerID = d.CustomerID
	existing.VATRegime = d.VATRegime
	existing.ServiceDate = d.ServiceDate
	existing.DueDate = d.DueDate
	existing.DiscountPercentPermyriad = d.DiscountPercentPermyriad
	existing.DiscountAmountCents = d.DiscountAmountCents
	existing.Language = d.Language
	if existing.Language == "" {
		existing.Language = storage.DocumentLanguageGerman
	}
	existing.ValidUntil = d.ValidUntil
	existing.ServiceDateTo = d.ServiceDateTo
	existing.PaymentTerms = d.PaymentTerms
	existing.Notes = d.Notes
	existing.CustomerReference = d.CustomerReference
	at := s.stamp()
	existing.UpdatedAt = at
	existing.Lines = withLines(existing.TenantID, existing.ID, lines, at)
	s.docs[existing.ID] = cloneDoc(existing, true)

	return cloneDoc(existing, true), nil
}

// DeleteDraft implements storage.DocumentRepository. A row another
// document references is refused, as the foreign keys refuse it.
func (s *Store) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if existing.State == storage.DocumentStateIssued {
		return storage.ErrConflict
	}
	for _, other := range s.docs {
		for _, ref := range []*uuid.UUID{other.SourceOfferID, other.CorrectsDocumentID, other.CorrectionBaseID} {
			if ref != nil && *ref == id {
				return storage.ErrConflict
			}
		}
	}
	delete(s.docs, id)

	return nil
}

// Lock implements storage.DocumentRepository. It records the id; the
// memory store has no row locks.
func (s *Store) Lock(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.find(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	s.locks = append(s.locks, id)

	return cloneDoc(d, true), nil
}

// Chain implements storage.DocumentRepository.
func (s *Store) Chain(ctx context.Context, rootID uuid.UUID) ([]storage.Document, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []storage.Document
	for _, d := range s.docs {
		if d.TenantID != tenant || d.State != storage.DocumentStateIssued {
			continue
		}
		if d.ID == rootID || (d.CorrectsDocumentID != nil && *d.CorrectsDocumentID == rootID) {
			out = append(out, cloneDoc(d, false))
		}
	}
	seq := func(d storage.Document) int32 {
		if d.CorrectionSeq == nil {
			return 0
		}

		return *d.CorrectionSeq
	}
	sort.Slice(out, func(i, j int) bool { return seq(out[i]) < seq(out[j]) })

	return out, nil
}

// SellerProfileForIssue implements storage.DocumentRepository.
func (s *Store) SellerProfileForIssue(ctx context.Context) (storage.SellerProfile, error) {
	if _, err := s.scope(ctx); err != nil {
		return storage.SellerProfile{}, err
	}

	return s.profile(ctx)
}

// Issue implements storage.DocumentRepository with the guards of
// IssueDocument, the CHECK constraints and the unique indexes.
func (s *Store) Issue(ctx context.Context, id uuid.UUID, issue storage.DocumentIssue) (storage.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.find(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Document{}, storage.ErrConflict
	}
	if err != nil {
		return storage.Document{}, err
	}
	if d.State != storage.DocumentStateDraft || d.Type != issue.Type {
		return storage.Document{}, storage.ErrConflict
	}
	isCorrection := issue.Type == storage.DocumentTypeCorrection
	if !isCorrection && issue.IssuedAt.Year() != issue.NumberYear {
		return storage.Document{}, errors.New("docissuetest: year does not match issuedAt")
	}
	if isCorrection != (issue.CorrectionSeq != nil) {
		return storage.Document{}, errors.New("docissuetest: correction sequence on the wrong type")
	}
	sum := sha256.Sum256(issue.Payload)
	if hex.EncodeToString(sum[:]) != issue.PayloadSHA256 {
		return storage.Document{}, errors.New("docissuetest: payload hash mismatch")
	}
	for _, other := range s.docs {
		if other.TenantID != d.TenantID || other.State != storage.DocumentStateIssued {
			continue
		}
		if other.DisplayNumber != nil && *other.DisplayNumber == issue.DisplayNumber {
			return storage.Document{}, storage.ErrConflict
		}
		if !isCorrection && other.Type == issue.Type && other.Number != nil && *other.Number == issue.Number &&
			other.NumberYear != nil && int(*other.NumberYear) == issue.NumberYear {
			return storage.Document{}, storage.ErrConflict
		}
		if isCorrection && other.CorrectionSeq != nil && *other.CorrectionSeq == *issue.CorrectionSeq &&
			other.CorrectsDocumentID != nil && d.CorrectsDocumentID != nil && *other.CorrectsDocumentID == *d.CorrectsDocumentID {
			return storage.Document{}, storage.ErrConflict
		}
	}

	number := issue.Number
	year := int32(issue.NumberYear) //nolint:gosec // a calendar year
	display := issue.DisplayNumber
	hash := issue.PayloadSHA256
	issuedAt := issue.IssuedAt
	d.State = storage.DocumentStateIssued
	d.Number = &number
	d.NumberYear = &year
	d.DisplayNumber = &display
	d.CorrectionSeq = issue.CorrectionSeq
	d.FrozenPayload = append([]byte(nil), issue.Payload...)
	d.FrozenPayloadSHA256 = &hash
	d.IssuedAt = &issuedAt
	d.UpdatedAt = s.stamp()
	s.docs[id] = cloneDoc(d, true)

	return cloneDoc(d, true), nil
}

// AllocateNext implements storage.DocumentNumberRepository.
func (s *Store) AllocateNext(ctx context.Context, docType storage.DocumentType, year int) (int64, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := counterKey{tenant, docType, year}
	s.counters[key]++

	return s.counters[key], nil
}

// Current implements storage.DocumentNumberRepository.
func (s *Store) Current(ctx context.Context, docType storage.DocumentType, year int) (int64, error) {
	tenant, err := s.scope(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counters[counterKey{tenant, docType, year}], nil
}
