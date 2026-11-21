-- migrate:up

-- payments records when money arrived for an invoice chain, or left it
-- again (issue #71; docs/concepts/solobill.md, "Payments"). received_on is
-- the Zufluss or Abfluss date section 11 EStG counts by: the EÜR uses
-- payments only, never document totals or invoice dates.
--
-- An issued invoice cannot carry a paid date, because
-- documents_block_issued_mutation refuses every change of it. So payments
-- are rows of their own, and the table is append-only: payments_block_mutation
-- refuses every UPDATE and DELETE, for any role, and the grant gives the
-- application role SELECT and INSERT only.
--
-- A payment belongs to the chain, not to one document of it: document_id
-- is always the original invoice, the root every correction references.
-- The open amount (the effective document's grand total minus the sum of
-- the chain's payments) is derived when reading and never stored here.
--
-- Two kinds of negative rows exist:
--   - A reversal undoes a recording mistake. It names the reversed row in
--     reverses_payment_id and carries its negated amount.
--   - A refund is money paid back to the customer, for example after a
--     correction lowered the total. It has no reverses_payment_id and is
--     dated the day the money left.
--
-- Amounts are gross integer cents, never a float. The VAT share of a
-- payment is not stored either: it is derived from the chain's effective
-- document on received_on (internal/paymentvat).
CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    -- The chain root: an issued invoice. payments_check_insert checks the
    -- type and the state, the foreign key below only the existence in the
    -- same tenant.
    document_id uuid NOT NULL,
    received_on date NOT NULL,
    amount_cents bigint NOT NULL,
    method text NOT NULL,
    note text,
    reverses_payment_id uuid,
    -- A payment of an imported 2026 invoice, brought in by issue #81.
    imported boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payments_method_check
        CHECK (method IN ('bank_transfer', 'cash', 'card', 'paypal', 'other')),
    -- A payment of zero moves no money and has no place in the EÜR.
    CONSTRAINT payments_amount_not_zero CHECK (amount_cents <> 0),
    -- 1,000,000,000.00 EUR, the same bound as expenses: far above any
    -- payment of a sole trader, and far below where a sum of rows could
    -- overflow a bigint.
    CONSTRAINT payments_amount_bound CHECK (abs(amount_cents) <= 100000000000),
    -- A reversal always takes money back out of the chain.
    CONSTRAINT payments_reversal_negative CHECK (reverses_payment_id IS NULL OR amount_cents < 0),
    CONSTRAINT payments_note_length CHECK (note IS NULL OR char_length(note) <= 1000),
    CONSTRAINT payments_not_self_reversal CHECK (reverses_payment_id IS NULL OR reverses_payment_id <> id),
    -- Composite foreign key target, see 20261113150832_create_customers.sql
    -- for why a single column reference would be a cross tenant existence
    -- oracle.
    CONSTRAINT payments_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT payments_document_fkey FOREIGN KEY (tenant_id, document_id)
        REFERENCES documents (tenant_id, id),
    CONSTRAINT payments_reverses_fkey FOREIGN KEY (tenant_id, reverses_payment_id)
        REFERENCES payments (tenant_id, id)
);

-- A payment is reversed at most once, also under concurrency.
CREATE UNIQUE INDEX payments_reversed_once
    ON payments (tenant_id, reverses_payment_id)
    WHERE reverses_payment_id IS NOT NULL;

-- GET /api/v1/documents/{id}/payments lists one chain in this order.
CREATE INDEX payments_chain_idx ON payments (tenant_id, document_id, received_on, created_at, id);

-- The EÜR report (issue #80) reads one date range of received_on.
CREATE INDEX payments_received_on_idx ON payments (tenant_id, received_on, created_at, id);

-- Every UPDATE and DELETE is refused, for any role: a mistake is undone by
-- a reversal row (section 146 Abs. 4 AO: no change may hide the
-- original). The error class integrity_constraint_violation is what
-- internal/storage/postgres maps to storage.ErrConflict, the same as
-- expenses_block_booked_mutation. search_path is pinned for the same
-- reason as there.
CREATE FUNCTION payments_block_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'payment % is append-only and immutable; record a reversal instead', OLD.id
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER payments_block_mutation
    BEFORE UPDATE OR DELETE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION payments_block_mutation();

-- A row trigger does not fire on TRUNCATE. The application role holds no
-- TRUNCATE privilege; this is defence in depth for every other role, the
-- same shape as expenses_block_truncate.
CREATE FUNCTION payments_block_truncate() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'payments are never truncated'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER payments_block_truncate
    BEFORE TRUNCATE ON payments
    FOR EACH STATEMENT
    EXECUTE FUNCTION payments_block_truncate();

-- payments_check_insert keeps a direct INSERT from storing what the
-- application never would:
--   - document_id must be an issued invoice: the root of its chain. Never
--     a draft, an offer or a correction.
--   - A reversal must name a payment of the same chain that is not a
--     reversal itself, carry exactly its negated amount, and not be dated
--     before it. payments_reversed_once refuses a second reversal.
--
-- Whether a refund exceeds what is due is a rule over the whole chain and
-- the effective document's total, which lives in a frozen payload: the
-- application checks it, under a lock on the root document row.
--
-- The lookups run under the inserting session's row level security, so
-- they only ever see the tenant's own rows. A row of another tenant is
-- simply not found.
CREATE FUNCTION payments_check_insert() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    doc_type text;
    doc_state text;
    original payments%ROWTYPE;
BEGIN
    SELECT type, state INTO doc_type, doc_state
    FROM documents
    WHERE tenant_id = NEW.tenant_id AND id = NEW.document_id;

    IF doc_type IS DISTINCT FROM 'invoice' OR doc_state IS DISTINCT FROM 'issued' THEN
        RAISE EXCEPTION 'payment % must reference an issued invoice, the root of its chain', NEW.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.reverses_payment_id IS NOT NULL THEN
        SELECT * INTO original
        FROM payments
        WHERE tenant_id = NEW.tenant_id AND id = NEW.reverses_payment_id;

        IF NOT FOUND
            OR original.document_id <> NEW.document_id
            OR original.reverses_payment_id IS NOT NULL
            OR NEW.amount_cents <> -original.amount_cents
            OR NEW.received_on < original.received_on
        THEN
            RAISE EXCEPTION 'payment % does not reverse a payment of its own chain', NEW.id
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER payments_check_insert
    BEFORE INSERT ON payments
    FOR EACH ROW
    EXECUTE FUNCTION payments_check_insert();

-- ENABLE ALWAYS, see 20261113160000_create_documents.sql: a plain ENABLE
-- trigger is skipped under session_replication_role = replica.
ALTER TABLE payments ENABLE ALWAYS TRIGGER payments_block_mutation;
ALTER TABLE payments ENABLE ALWAYS TRIGGER payments_block_truncate;
ALTER TABLE payments ENABLE ALWAYS TRIGGER payments_check_insert;

-- Row level security, the same policy shape as every other tenant scoped
-- table: see 20261112233249_create_accounts.sql for why ENABLE and FORCE
-- are both needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE payments FORCE ROW LEVEL SECURITY;

CREATE POLICY payments_tenant_isolation ON payments
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE payments;
DROP FUNCTION payments_check_insert();
DROP FUNCTION payments_block_truncate();
DROP FUNCTION payments_block_mutation();
