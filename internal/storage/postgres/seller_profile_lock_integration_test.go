//go:build integration

// A profile save against a concurrent first issue, through the restricted,
// NOBYPASSRLS application role. Same reasons as integration_test.go's own
// build tag comment.
package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/numbering"
	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

const (
	// lockTestOldInvoice is the template newIssueEnv saves.
	lockTestOldInvoice = "RE-{year}{seq:4}"
	// lockTestNewInvoice changes the tokens, which the prefix rule refuses
	// once an invoice was issued.
	lockTestNewInvoice = "RE-{seq:6}-{year}"
)

// listHook is a DocumentRepository whose List runs afterList once it has
// read, so a test can stop a profile save between the issued types check
// and the write.
type listHook struct {
	storage.DocumentRepository
	afterList func()
}

func (h listHook) List(ctx context.Context) ([]storage.Document, error) {
	documents, err := h.DocumentRepository.List(ctx)
	if h.afterList != nil {
		h.afterList()
	}

	return documents, err
}

// saveResult is what a profile save answered: the problems that refused
// it, or the error that broke it.
type saveResult struct {
	problems map[string]string
	err      error
}

// saveInvoiceTemplate is the in transaction part of PUT /api/v1/profile
// (internal/httpapi's handlePutProfile) for a change of the invoice
// template: lock the profile row, read the issued types, check the
// templates, write. It runs in a scope of its own. The handler's own
// order is pinned by TestProfileSaveLocksBeforeReadingIssuedTypes.
func (e issueEnv) saveInvoiceTemplate(documents storage.DocumentRepository, template string) saveResult {
	var problems map[string]string
	err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		old, err := e.store.SellerProfiles().GetForUpdate(ctx)
		if err != nil {
			return err
		}

		listed, err := documents.List(ctx)
		if err != nil {
			return err
		}
		issued := map[storage.DocumentType]bool{}
		for _, d := range listed {
			if d.State == storage.DocumentStateIssued {
				issued[d.Type] = true
			}
		}

		next := sellerprofile.WithDefaultFormats(map[storage.DocumentType]string{
			storage.DocumentTypeInvoice: template,
			storage.DocumentTypeOffer:   old.NumberFormats[storage.DocumentTypeOffer],
		})
		problems = sellerprofile.CheckNumberFormats(next, old.NumberFormats, issued)
		if len(problems) > 0 {
			return nil
		}

		old.NumberFormats = next
		_, err = e.store.SellerProfiles().Put(ctx, old)

		return err
	})

	return saveResult{problems: problems, err: err}
}

// storedInvoiceTemplate reads the saved invoice template in a scope of
// its own.
func (e issueEnv) storedInvoiceTemplate(t *testing.T) string {
	t.Helper()

	var template string
	if err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		p, err := e.store.SellerProfiles().Get(ctx)
		template = p.NumberFormats[storage.DocumentTypeInvoice]

		return err
	}); err != nil {
		t.Fatalf("read the profile: %v", err)
	}

	return template
}

// waitForProfileLockWait blocks until a backend waits for a lock on the
// seller profile row. The poll is a wait on that condition, not on time.
func (e issueEnv) waitForProfileLockWait(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := e.adminPool.QueryRow(e.ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%seller_profiles%'`).Scan(&waiting)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no backend waited for the seller profile lock within 10s")
}

// renderedInvoice is the display number the first invoice of 2026 gets
// under template.
func renderedInvoice(t *testing.T, template string) string {
	t.Helper()

	tpl, err := numbering.Parse(template)
	if err != nil {
		t.Fatalf("parse %q: %v", template, err)
	}
	display, err := tpl.Render(issueTestNow.Year(), 1)
	if err != nil {
		t.Fatalf("render %q: %v", template, err)
	}

	return display
}

// TestProfileSave_RefusedAfterAConcurrentFirstIssue is the order issue
// first: an issue holds its transaction open after it wrote the document,
// the template change starts and waits, and once the issue commits the
// change finds the invoice issued and is refused. Without the row lock
// the change reads the issued types before the issue commits, passes the
// check and writes the new template after it.
func TestProfileSave_RefusedAfterAConcurrentFirstIssue(t *testing.T) {
	e := newIssueEnv(t, 6)
	draft := e.draft(t, storage.Document{})

	issueWritten := make(chan struct{})
	releaseIssue := make(chan struct{})
	issueDone := make(chan error, 1)
	var issued storage.Document
	go func() {
		issueDone <- e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
			var err error
			issued, err = e.issuer().Issue(ctx, draft.ID, issueTestNow)
			if err != nil {
				return err
			}
			close(issueWritten)
			<-releaseIssue

			return nil
		})
	}()
	select {
	case <-issueWritten:
	case err := <-issueDone:
		t.Fatalf("issue: %v", err)
	}

	saveDone := make(chan saveResult, 1)
	go func() { saveDone <- e.saveInvoiceTemplate(e.store.Documents(), lockTestNewInvoice) }()
	e.waitForProfileLockWait(t)

	close(releaseIssue)
	if err := <-issueDone; err != nil {
		t.Fatalf("issue transaction: %v", err)
	}
	res := <-saveDone
	if res.err != nil {
		t.Fatalf("profile save: %v", res.err)
	}

	if _, ok := res.problems["number_formats.invoice"]; !ok {
		t.Errorf("the template change after the first issue: problems = %v, want number_formats.invoice", res.problems)
	}
	if got := e.storedInvoiceTemplate(t); got != lockTestOldInvoice {
		t.Errorf("stored invoice template = %q, want %q", got, lockTestOldInvoice)
	}
	if want := renderedInvoice(t, lockTestOldInvoice); issued.DisplayNumber == nil || *issued.DisplayNumber != want {
		t.Errorf("display number = %v, want %q", issued.DisplayNumber, want)
	}
}

// TestProfileSave_FirstIssueSeesTheNewTemplate is the order save first:
// the template change has read the issued types, found none and stops
// before it writes. The first issue starts and waits for the profile
// lock, and once the change commits the issue numbers under the new
// template.
func TestProfileSave_FirstIssueSeesTheNewTemplate(t *testing.T) {
	e := newIssueEnv(t, 6)
	draft := e.draft(t, storage.Document{})

	listed := make(chan struct{})
	proceed := make(chan struct{})
	var proceedOnce sync.Once
	release := func() { proceedOnce.Do(func() { close(proceed) }) }
	t.Cleanup(release)

	hooked := listHook{DocumentRepository: e.store.Documents(), afterList: func() {
		close(listed)
		<-proceed
	}}
	saveDone := make(chan saveResult, 1)
	go func() { saveDone <- e.saveInvoiceTemplate(hooked, lockTestNewInvoice) }()
	select {
	case <-listed:
	case res := <-saveDone:
		t.Fatalf("the save ended before it read the issued types: %+v", res)
	}

	var issued storage.Document
	issueDone := make(chan error, 1)
	go func() {
		var err error
		issued, err = e.issue(draft.ID, e.issuer(), issueTestNow)
		issueDone <- err
	}()
	e.waitForProfileLockWait(t)

	release()
	res := <-saveDone
	if res.err != nil {
		t.Fatalf("profile save: %v", res.err)
	}
	if err := <-issueDone; err != nil {
		t.Fatalf("issue: %v", err)
	}

	if len(res.problems) > 0 {
		t.Errorf("the template change before the first issue: problems = %v, want none", res.problems)
	}
	if got := e.storedInvoiceTemplate(t); got != lockTestNewInvoice {
		t.Errorf("stored invoice template = %q, want %q", got, lockTestNewInvoice)
	}
	if want := renderedInvoice(t, lockTestNewInvoice); issued.DisplayNumber == nil || *issued.DisplayNumber != want {
		t.Errorf("display number = %v, want %q", issued.DisplayNumber, want)
	}
}
