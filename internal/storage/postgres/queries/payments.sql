-- Every query names the tenant in its WHERE clause or as a column value,
-- and row level security enforces the same scope below it; see
-- customers.sql for why both layers.
--
-- payments is append-only: there is no UPDATE and no DELETE here, the
-- grant gives the application role neither, and payments_block_mutation
-- (see db/migrations) refuses both for every role.

-- name: CreatePayment :one
INSERT INTO payments (
    tenant_id, document_id, received_on, amount_cents, method, note, reverses_payment_id, imported
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, tenant_id, document_id, received_on, amount_cents, method, note, reverses_payment_id,
    imported, created_at;

-- ListPaymentsByChain is the payments of the chain rooted at document_id,
-- in a stable order: received_on, then creation, then id.
--
-- name: ListPaymentsByChain :many
SELECT id, tenant_id, document_id, received_on, amount_cents, method, note, reverses_payment_id,
    imported, created_at
FROM payments
WHERE document_id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY received_on, created_at, id;

-- ListPaymentsReceivedBetween is every payment with received_on in
-- [received_from, received_to], both inclusive, for the EÜR report.
--
-- name: ListPaymentsReceivedBetween :many
SELECT id, tenant_id, document_id, received_on, amount_cents, method, note, reverses_payment_id,
    imported, created_at
FROM payments
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND received_on >= sqlc.arg(received_from)::date
    AND received_on <= sqlc.arg(received_to)::date
ORDER BY received_on, created_at, id;
