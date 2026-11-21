package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// paymentRepository satisfies storage.PaymentRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
//
// The table is append-only. db/migrations' payments_check_insert refuses
// a row whose document is not an issued invoice, and a reversal that does
// not mirror a payment of its own chain; payments_reversed_once refuses a
// second reversal. All of them end in storage.ErrConflict.
type paymentRepository struct{}

// Create implements storage.PaymentRepository. ID and CreatedAt of
// payment are ignored: the database sets them.
func (r *paymentRepository) Create(ctx context.Context, payment storage.Payment) (storage.Payment, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Payment{}, err
	}

	row, err := sqlcgen.New(tx).CreatePayment(ctx, sqlcgen.CreatePaymentParams{
		TenantID:          payment.TenantID,
		DocumentID:        payment.DocumentID,
		ReceivedOn:        pgtype.Date{Time: payment.ReceivedOn, Valid: true},
		AmountCents:       payment.AmountCents,
		Method:            string(payment.Method),
		Note:              ptrToText(payment.Note),
		ReversesPaymentID: ptrToUUID(payment.ReversesPaymentID),
		Imported:          payment.Imported,
	})
	if err != nil {
		return storage.Payment{}, mapDocumentWriteError(err)
	}

	return paymentFromRow(row), nil
}

// ListByChain implements storage.PaymentRepository.
func (r *paymentRepository) ListByChain(ctx context.Context, rootID uuid.UUID) ([]storage.Payment, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListPaymentsByChain(ctx, rootID)
	if err != nil {
		return nil, mapReadError(err)
	}

	return paymentsFromRows(rows), nil
}

// ListReceivedBetween implements storage.PaymentRepository. Only the
// calendar dates of from and to count.
func (r *paymentRepository) ListReceivedBetween(ctx context.Context, from, to time.Time) ([]storage.Payment, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListPaymentsReceivedBetween(ctx, sqlcgen.ListPaymentsReceivedBetweenParams{
		ReceivedFrom: pgtype.Date{Time: from, Valid: true},
		ReceivedTo:   pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return nil, mapReadError(err)
	}

	return paymentsFromRows(rows), nil
}

func paymentsFromRows(rows []sqlcgen.Payment) []storage.Payment {
	out := make([]storage.Payment, len(rows))
	for i, row := range rows {
		out[i] = paymentFromRow(row)
	}

	return out
}

func paymentFromRow(row sqlcgen.Payment) storage.Payment {
	return storage.Payment{
		ID:                row.ID,
		TenantID:          row.TenantID,
		DocumentID:        row.DocumentID,
		ReceivedOn:        row.ReceivedOn.Time,
		AmountCents:       row.AmountCents,
		Method:            storage.PaymentMethod(row.Method),
		Note:              textToPtr(row.Note),
		ReversesPaymentID: uuidToPtr(row.ReversesPaymentID),
		Imported:          row.Imported,
		CreatedAt:         timeFromTimestamptz(row.CreatedAt),
	}
}
