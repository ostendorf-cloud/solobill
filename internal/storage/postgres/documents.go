package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// pgIntegrityConstraintViolationClass is the Postgres error class
// db/migrations' documents_block_issued_mutation and
// document_lines_block_issued_document_mutation trigger functions raise
// under (USING ERRCODE = 'integrity_constraint_violation'), distinct
// from pgUniqueViolation: a trigger's RAISE EXCEPTION reports as this
// class's own generic code, "23000", not as the specific "23505" a
// unique index violation carries. mapWriteError does not recognize this
// class on its own (it only ever maps pgUniqueViolation to
// storage.ErrConflict), so documentRepository's own write paths check
// for it explicitly; see mapDocumentWriteError.
const pgIntegrityConstraintViolationClass = "23000"

// documentRepository satisfies storage.DocumentRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
type documentRepository struct{}

// Create implements storage.DocumentRepository: inserts the document
// row, then every line, in order, each with its own Position, all
// inside the same transaction WithTenant already opened.
func (r *documentRepository) Create(ctx context.Context, document storage.Document, lines []storage.DocumentLine) (storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	row, err := sqlcgen.New(tx).CreateDocument(ctx, sqlcgen.CreateDocumentParams{
		TenantID:                 document.TenantID,
		Type:                     string(document.Type),
		CustomerID:               document.CustomerID,
		CorrectsDocumentID:       ptrToUUID(document.CorrectsDocumentID),
		VatRegime:                string(document.VATRegime),
		ServiceDate:              ptrToDate(document.ServiceDate),
		DueDate:                  ptrToDate(document.DueDate),
		DiscountPercentPermyriad: ptrToInt4(document.DiscountPercentPermyriad),
		DiscountAmountCents:      ptrToInt8(document.DiscountAmountCents),
		SourceOfferID:            ptrToUUID(document.SourceOfferID),
		Language:                 string(documentLanguage(document.Language)),
		ValidUntil:               ptrToDate(document.ValidUntil),
		ServiceDateTo:            ptrToDate(document.ServiceDateTo),
		PaymentTerms:             ptrToText(document.PaymentTerms),
		Notes:                    ptrToText(document.Notes),
		CustomerReference:        ptrToText(document.CustomerReference),
		CorrectionBaseID:         ptrToUUID(document.CorrectionBaseID),
		CorrectionBaseSeq:        ptrToInt4(document.CorrectionBaseSeq),
	})
	if err != nil {
		return storage.Document{}, mapDocumentWriteError(err)
	}

	createdLines, err := createDocumentLines(ctx, tx, row.TenantID, row.ID, lines)
	if err != nil {
		return storage.Document{}, err
	}

	result := documentFromRow(row)
	result.Lines = createdLines

	return result, nil
}

// FindByID implements storage.DocumentRepository. It does not filter on
// state; see storage.DocumentRepository.FindByID for why.
func (r *documentRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	row, err := sqlcgen.New(tx).GetDocumentByID(ctx, id)
	if err != nil {
		return storage.Document{}, mapReadError(err)
	}

	lineRows, err := sqlcgen.New(tx).ListDocumentLines(ctx, row.ID)
	if err != nil {
		return storage.Document{}, mapReadError(err)
	}

	result := documentFromRow(row)
	result.Lines = documentLinesFromRows(lineRows)

	return result, nil
}

// List implements storage.DocumentRepository. Lines is left nil on
// every returned storage.Document; see storage.DocumentRepository.List
// for why.
func (r *documentRepository) List(ctx context.Context) ([]storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListDocuments(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	documents := make([]storage.Document, len(rows))
	for i, row := range rows {
		documents[i] = documentFromRow(row)
	}

	return documents, nil
}

// Update implements storage.DocumentRepository: a full replace of the
// document's own header, then its lines (delete every existing line,
// recreate the caller's current set), all inside the same transaction.
// db/migrations' documents_block_issued_mutation and
// document_lines_block_issued_document_mutation triggers are what
// actually refuse this against an issued document; see
// mapDocumentWriteError for how that refusal is told apart from "no
// such row at all" and surfaced as storage.ErrConflict.
func (r *documentRepository) Update(ctx context.Context, document storage.Document, lines []storage.DocumentLine) (storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	row, err := sqlcgen.New(tx).UpdateDocument(ctx, sqlcgen.UpdateDocumentParams{
		ID:                       document.ID,
		CustomerID:               document.CustomerID,
		VatRegime:                string(document.VATRegime),
		ServiceDate:              ptrToDate(document.ServiceDate),
		DueDate:                  ptrToDate(document.DueDate),
		DiscountPercentPermyriad: ptrToInt4(document.DiscountPercentPermyriad),
		DiscountAmountCents:      ptrToInt8(document.DiscountAmountCents),
		Language:                 string(documentLanguage(document.Language)),
		ValidUntil:               ptrToDate(document.ValidUntil),
		ServiceDateTo:            ptrToDate(document.ServiceDateTo),
		PaymentTerms:             ptrToText(document.PaymentTerms),
		Notes:                    ptrToText(document.Notes),
		CustomerReference:        ptrToText(document.CustomerReference),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.Document{}, storage.ErrNotFound
		}

		return storage.Document{}, mapDocumentWriteError(err)
	}

	if _, err := sqlcgen.New(tx).DeleteDocumentLines(ctx, row.ID); err != nil {
		return storage.Document{}, mapDocumentWriteError(err)
	}

	createdLines, err := createDocumentLines(ctx, tx, row.TenantID, row.ID, lines)
	if err != nil {
		return storage.Document{}, err
	}

	result := documentFromRow(row)
	result.Lines = createdLines

	return result, nil
}

// DeleteDraft implements storage.DocumentRepository. document_lines'
// own ON DELETE CASCADE foreign key (see db/migrations) removes the
// document's lines; see mapDocumentWriteError for how an issued
// document's refusal (from documents_block_issued_mutation) is told
// apart from "no such row at all".
func (r *documentRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	affected, err := sqlcgen.New(tx).DeleteDraftDocument(ctx, id)
	if err != nil {
		return mapDocumentWriteError(err)
	}

	if affected == 0 {
		return storage.ErrNotFound
	}

	return nil
}

// Lock implements storage.DocumentRepository: queries/documents.sql's
// LockDocument, then the lines. It reports no state: the caller checks
// it before it allocates anything. See LockDocument for why issuing
// locks the draft and a correction's root before the counter.
func (r *documentRepository) Lock(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	row, err := sqlcgen.New(tx).LockDocument(ctx, id)
	if err != nil {
		return storage.Document{}, mapReadError(err)
	}

	lineRows, err := sqlcgen.New(tx).ListDocumentLines(ctx, row.ID)
	if err != nil {
		return storage.Document{}, mapReadError(err)
	}

	result := documentFromRow(row)
	result.Lines = documentLinesFromRows(lineRows)

	return result, nil
}

// Chain implements storage.DocumentRepository.
func (r *documentRepository) Chain(ctx context.Context, rootID uuid.UUID) ([]storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListChainDocuments(ctx, rootID)
	if err != nil {
		return nil, mapReadError(err)
	}

	documents := make([]storage.Document, len(rows))
	for i, row := range rows {
		documents[i] = documentFromRow(row)
	}

	return documents, nil
}

// SellerProfileForIssue implements storage.DocumentRepository through
// queries/documents.sql's LockSellerProfileForIssue, mapped by issue
// #68's own sellerProfileFromRow.
func (r *documentRepository) SellerProfileForIssue(ctx context.Context) (storage.SellerProfile, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.SellerProfile{}, err
	}

	row, err := sqlcgen.New(tx).LockSellerProfileForIssue(ctx)
	if err != nil {
		return storage.SellerProfile{}, mapReadError(err)
	}

	return sellerProfileFromRow(row)
}

// Issue implements storage.DocumentRepository: the one UPDATE
// queries/documents.sql's IssueDocument performs. It allocates nothing:
// the caller has locked the draft (Lock) and, for an invoice or an
// offer, allocated the number with DocumentNumberRepository.AllocateNext
// inside the same transaction, so a rolled-back issue gives its number
// back (see storage.DocumentNumberRepository.AllocateNext).
func (r *documentRepository) Issue(ctx context.Context, id uuid.UUID, issue storage.DocumentIssue) (storage.Document, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	// A counter-issued document's number_year and issuedAt must agree: a
	// caller passing a year that does not match issuedAt's own calendar
	// year would freeze a number whose year contradicts the invoice date
	// it is issued under. A correction repeats its original's number and
	// year, so its year may lie before its own issue date. An imported
	// document never comes here (storage.ImportedDocumentRepository).
	isCorrection := issue.Type == storage.DocumentTypeCorrection
	if !isCorrection && issue.IssuedAt.Year() != issue.NumberYear {
		return storage.Document{}, fmt.Errorf("issue: year %d does not match issuedAt's own year %d", issue.NumberYear, issue.IssuedAt.Year())
	}
	if isCorrection != (issue.CorrectionSeq != nil) {
		return storage.Document{}, errors.New("issue: a correction, and only a correction, carries a correction sequence")
	}

	row, err := sqlcgen.New(tx).IssueDocument(ctx, sqlcgen.IssueDocumentParams{
		ID:                  id,
		Number:              pgtype.Int8{Int64: issue.Number, Valid: true},
		NumberYear:          pgtype.Int4{Int32: int32(issue.NumberYear), Valid: true}, //nolint:gosec // a calendar year, far inside int32's range
		IssuedAt:            toTimestamptz(issue.IssuedAt),
		FrozenPayload:       issue.Payload,
		FrozenPayloadSha256: pgtype.Text{String: issue.PayloadSHA256, Valid: true},
		DisplayNumber:       pgtype.Text{String: issue.DisplayNumber, Valid: true},
		CorrectionSeq:       ptrToInt4(issue.CorrectionSeq),
		Type:                string(issue.Type),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either id does not exist in this tenant at all, or it does
			// but is not a draft of this type: queries/documents.sql's
			// IssueDocument's own doc comment is why this is reported as
			// a conflict, not a not-found, unlike every other :one query
			// in this package reaching pgx.ErrNoRows. ErrNotADraft tells
			// it apart from a unique index refusing the number.
			return storage.Document{}, fmt.Errorf("%w: %w", storage.ErrConflict, storage.ErrNotADraft)
		}

		return storage.Document{}, mapDocumentWriteError(err)
	}

	lineRows, err := sqlcgen.New(tx).ListDocumentLines(ctx, row.ID)
	if err != nil {
		return storage.Document{}, mapReadError(err)
	}

	result := documentFromRow(row)
	result.Lines = documentLinesFromRows(lineRows)

	return result, nil
}

// createDocumentLines inserts lines in order, each under tenantID and
// documentID, assigning Position 0, 1, 2, ... by the caller's own slice
// order: callers never choose Position directly, so two lines can never
// collide on document_lines_tenant_id_document_id_position_unique.
func createDocumentLines(ctx context.Context, tx pgx.Tx, tenantID, documentID uuid.UUID, lines []storage.DocumentLine) ([]storage.DocumentLine, error) {
	created := make([]storage.DocumentLine, len(lines))

	for i, line := range lines {
		row, err := sqlcgen.New(tx).CreateDocumentLine(ctx, sqlcgen.CreateDocumentLineParams{
			TenantID:                     tenantID,
			DocumentID:                   documentID,
			Position:                     int32(i), //nolint:gosec // i is a small slice index, far inside int32's range
			CatalogueItemID:              ptrToUUID(line.CatalogueItemID),
			Description:                  line.Description,
			QuantityThousandths:          line.QuantityThousandths,
			UnitPriceCents:               line.UnitPriceCents,
			Unit:                         line.Unit,
			VatRatePermyriad:             line.VATRatePermyriad,
			LineDiscountPercentPermyriad: ptrToInt4(line.LineDiscountPercentPermyriad),
			LineDiscountAmountCents:      ptrToInt8(line.LineDiscountAmountCents),
		})
		if err != nil {
			return nil, mapDocumentWriteError(err)
		}

		created[i] = documentLineFromRow(row)
	}

	return created, nil
}

// mapDocumentWriteError is mapWriteError (unique-violation detection)
// extended with pgIntegrityConstraintViolationClass recognition: a write
// documents_block_issued_mutation or
// document_lines_block_issued_document_mutation refuses raises that
// class, not pgUniqueViolation, and must still surface as
// storage.ErrConflict, since "this row exists but is not in a state this
// write can touch" is exactly what storage.ErrConflict already means
// everywhere else in this package (see storage.ErrConflict's own doc
// comment).
func mapDocumentWriteError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == pgIntegrityConstraintViolationClass || pgErr.Code == pgForeignKeyViolation) {
		return storage.ErrConflict
	}

	return mapWriteError(err)
}

// pgForeignKeyViolation is the code of a foreign key violation. On the
// document paths it means the row is still referenced (an offer draft an
// invoice was created from cannot be deleted) or a referenced row is
// gone: a write losing to the state of another row, storage.ErrConflict.
const pgForeignKeyViolation = "23503"

func documentFromRow(row sqlcgen.Document) storage.Document {
	return storage.Document{
		ID:                       row.ID,
		TenantID:                 row.TenantID,
		Type:                     storage.DocumentType(row.Type),
		State:                    storage.DocumentState(row.State),
		CustomerID:               row.CustomerID,
		CorrectsDocumentID:       uuidToPtr(row.CorrectsDocumentID),
		VATRegime:                storage.VATRegime(row.VatRegime),
		ServiceDate:              dateToPtr(row.ServiceDate),
		DueDate:                  dateToPtr(row.DueDate),
		DiscountPercentPermyriad: int4ToPtr(row.DiscountPercentPermyriad),
		DiscountAmountCents:      int8ToPtr(row.DiscountAmountCents),
		Number:                   int8ToPtr(row.Number),
		NumberYear:               int4ToPtr(row.NumberYear),
		Imported:                 row.Imported,
		FrozenPayload:            row.FrozenPayload,
		FrozenPayloadSHA256:      textToPtr(row.FrozenPayloadSha256),
		IssuedAt:                 timeToPtr(row.IssuedAt),
		CreatedAt:                timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:                timeFromTimestamptz(row.UpdatedAt),
		OriginalNumber:           textToPtr(row.OriginalNumber),
		SourceOfferID:            uuidToPtr(row.SourceOfferID),
		Language:                 storage.DocumentLanguage(row.Language),
		ValidUntil:               dateToPtr(row.ValidUntil),
		ServiceDateTo:            dateToPtr(row.ServiceDateTo),
		PaymentTerms:             textToPtr(row.PaymentTerms),
		Notes:                    textToPtr(row.Notes),
		CustomerReference:        textToPtr(row.CustomerReference),
		CorrectionSeq:            int4ToPtr(row.CorrectionSeq),
		DisplayNumber:            textToPtr(row.DisplayNumber),
		CorrectionBaseID:         uuidToPtr(row.CorrectionBaseID),
		CorrectionBaseSeq:        int4ToPtr(row.CorrectionBaseSeq),
	}
}

// documentLanguage is the language a document row is written with: an
// empty value is German, the column's own default.
func documentLanguage(l storage.DocumentLanguage) storage.DocumentLanguage {
	if l == "" {
		return storage.DocumentLanguageGerman
	}

	return l
}

func documentLinesFromRows(rows []sqlcgen.DocumentLine) []storage.DocumentLine {
	lines := make([]storage.DocumentLine, len(rows))
	for i, row := range rows {
		lines[i] = documentLineFromRow(row)
	}

	return lines
}

func documentLineFromRow(row sqlcgen.DocumentLine) storage.DocumentLine {
	return storage.DocumentLine{
		ID:                           row.ID,
		TenantID:                     row.TenantID,
		DocumentID:                   row.DocumentID,
		Position:                     row.Position,
		CatalogueItemID:              uuidToPtr(row.CatalogueItemID),
		Description:                  row.Description,
		QuantityThousandths:          row.QuantityThousandths,
		UnitPriceCents:               row.UnitPriceCents,
		Unit:                         row.Unit,
		VATRatePermyriad:             row.VatRatePermyriad,
		LineDiscountPercentPermyriad: int4ToPtr(row.LineDiscountPercentPermyriad),
		LineDiscountAmountCents:      int8ToPtr(row.LineDiscountAmountCents),
		CreatedAt:                    timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:                    timeFromTimestamptz(row.UpdatedAt),
	}
}
