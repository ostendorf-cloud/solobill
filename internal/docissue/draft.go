package docissue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// DraftPayload builds the payload a draft prints before it is issued:
// the draft, the given seller profile, the customer and the profile's
// logo as they are now, today's issue date in Germany and no number. A
// correction names its original. Nothing is locked, numbered, checked
// or stored: the renderer prints the result with its watermark and
// reports what is missing. Every call must run inside a context
// storage.TenantScope.WithTenant produced.
func (i Issuer) DraftPayload(ctx context.Context, draft storage.Document, profile storage.SellerProfile, now time.Time) (docpayload.FrozenPayloadV1, error) {
	if draft.State != storage.DocumentStateDraft {
		return docpayload.FrozenPayloadV1{}, ErrNotDraft
	}

	customer, err := i.Customers.FindByID(ctx, draft.CustomerID)
	if err != nil {
		return docpayload.FrozenPayloadV1{}, fmt.Errorf("docissue: customer: %w", err)
	}
	logo, err := i.logoSHA256(ctx)
	if err != nil {
		return docpayload.FrozenPayloadV1{}, err
	}

	in := PayloadInput{
		Document:   draft,
		Seller:     profile,
		LogoSHA256: logo,
		Customer:   customer,
		IssueDate:  IssueDate(now),
	}
	if draft.Type == storage.DocumentTypeCorrection {
		if draft.CorrectsDocumentID == nil {
			return docpayload.FrozenPayloadV1{}, errors.New("docissue: a correction without its original")
		}
		root, err := i.Documents.FindByID(ctx, *draft.CorrectsDocumentID)
		if err != nil {
			return docpayload.FrozenPayloadV1{}, fmt.Errorf("docissue: the correction's original: %w", err)
		}
		if root.DisplayNumber != nil {
			in.Corrects = &docpayload.DocumentReference{
				ID:        root.ID.String(),
				Number:    *root.DisplayNumber,
				IssueDate: issueDateOf(root),
			}
		}
	}

	return BuildPayload(in), nil
}
