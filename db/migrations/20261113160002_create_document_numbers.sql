-- migrate:up

-- document_numbers is the gapless counter docs/concepts/solobill.md's
-- Approach section describes: one row per tenant, per document type and
-- per year, holding the last number issued. internal/storage/postgres's
-- numbering code allocates the next number with a single
-- UPDATE ... SET last_number = last_number + 1 ... RETURNING, inside the
-- same transaction that writes the document's own issued state, so a
-- transaction that rolls back after allocating takes its number back
-- with it: nothing else ever observes a number that was allocated and
-- then abandoned. See internal/storage/postgres/queries/document_numbers.sql
-- for why that single UPDATE is already the serialising lock this
-- counter needs, with no separate SELECT ... FOR UPDATE beforehand.
--
-- Deliberately a separate table from documents, not a sequence or a
-- column on documents itself: a Postgres SEQUENCE never rolls back (by
-- design, so concurrent sessions never block each other on it), which is
-- exactly the gaplessness guarantee this counter must NOT have that
-- property for; see docs/concepts/solobill.md's own reasoning for why
-- the counter has to be allocated inside the issuing transaction in the
-- first place.
CREATE TABLE document_numbers (
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    -- Only the document types this counter actually numbers: the same
    -- set documents.type allows for a numbered document today (see that
    -- migration). 'offer', if and when it exists, will need its own
    -- decision about whether offers share this counter's notion of
    -- "type" or get their own numbering scheme entirely; this migration
    -- does not decide that ahead of time.
    type text NOT NULL,
    number_year integer NOT NULL,
    -- The last number actually issued for this (tenant, type,
    -- number_year); 0 means none yet. The next document issued under
    -- this key gets last_number + 1, which is also why a brand new key's
    -- row has to exist with last_number = 0 before the first allocation
    -- can lock and increment it; see the numbering code for how that row
    -- is created, idempotently, on first use.
    last_number bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT document_numbers_type_check CHECK (type IN ('invoice', 'correction')),
    CONSTRAINT document_numbers_number_year_range CHECK (number_year >= 2000),
    CONSTRAINT document_numbers_last_number_nonneg CHECK (last_number >= 0),
    -- The counter's own identity: one row per tenant, per type, per
    -- year. This is also exactly the row IncrementDocumentNumber's own
    -- UPDATE locks, which is what serialises issuing per tenant, per
    -- type and per year, and nothing coarser or finer than that: two
    -- different tenants, or two different years, or an invoice and a
    -- correction in the same year, never contend for the same lock.
    PRIMARY KEY (tenant_id, type, number_year)
);

-- Row level security is the actual multi-tenancy guarantee; see the
-- customers and catalogue_items migrations for the full reasoning, which
-- applies here unchanged. Enforcing it here matters just as much as on
-- any other tenant-scoped table: without it, a connection merely scoped
-- to the wrong tenant could still lock and increment another tenant's
-- counter.
ALTER TABLE document_numbers ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_numbers FORCE ROW LEVEL SECURITY;

CREATE POLICY document_numbers_tenant_isolation ON document_numbers
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE document_numbers;
