-- migrate:up

-- Issue #70: offers, the replace model of corrections, the stored display
-- number and the printed free text fields. See docs/concepts/solobill.md,
-- "Offers", "Corrections", "Number format" and "Imported documents and
-- original numbers". The migrations of issue #15 stay untouched; every
-- change here is a new constraint, column, index or trigger.

-- Offers are a third document type with their own gapless counter per
-- year, so both type checks widen. 'correction' stays in
-- document_numbers_type_check only so the constraint keeps accepting what
-- it accepted before: from this issue on a correction never allocates
-- from the counter (its correction_seq is counted per chain, below).
ALTER TABLE documents DROP CONSTRAINT documents_type_check;
ALTER TABLE documents ADD CONSTRAINT documents_type_check
    CHECK (type IN ('invoice', 'correction', 'offer'));

ALTER TABLE document_numbers DROP CONSTRAINT document_numbers_type_check;
ALTER TABLE document_numbers ADD CONSTRAINT document_numbers_type_check
    CHECK (type IN ('invoice', 'correction', 'offer'));

ALTER TABLE documents
    -- The number an imported document was issued under, exactly as the
    -- customer holds it. Only the import (#81) writes it.
    ADD COLUMN original_number text,
    -- The offer an invoice was created from (POST /offers/{id}/to-invoice).
    -- Composite foreign key below, so it can never name a row of another
    -- tenant.
    ADD COLUMN source_offer_id uuid,
    -- The language the document is printed in.
    ADD COLUMN language text NOT NULL DEFAULT 'de',
    -- An offer's validity date.
    ADD COLUMN valid_until date,
    -- The end of a service period that starts at service_date.
    ADD COLUMN service_date_to date,
    -- Free text printed on the document. Their limits are the ones of the
    -- template document-v1 (internal/docpayload's MaxTextChars and
    -- MaxLineChars), so a stored draft can always be issued as far as
    -- its length goes.
    ADD COLUMN payment_terms text,
    ADD COLUMN notes text,
    ADD COLUMN customer_reference text,
    -- A correction's place in its chain: 1 for the first correction of an
    -- original invoice, 2 for the next. Set in the issuing transaction
    -- under a lock on the chain's root row, never from document_numbers.
    ADD COLUMN correction_seq integer,
    -- The number as printed, written once at issue: the rendered template,
    -- the original's number plus "-1", "-2" for a correction, or an
    -- imported document's original number. Every list and read returns
    -- it, so a later template change never rewrites an issued number.
    ADD COLUMN display_number text,
    -- On a correction draft: the chain's effective document it copied its
    -- lines from. Issuing refuses when the effective document has changed
    -- since (409 correction_base_changed).
    ADD COLUMN correction_base_id uuid,
    ADD COLUMN correction_base_seq integer;

ALTER TABLE documents
    ADD CONSTRAINT documents_language_check CHECK (language IN ('de', 'en')),
    ADD CONSTRAINT documents_source_offer_id_fkey
        FOREIGN KEY (tenant_id, source_offer_id) REFERENCES documents (tenant_id, id),
    ADD CONSTRAINT documents_correction_base_id_fkey
        FOREIGN KEY (tenant_id, correction_base_id) REFERENCES documents (tenant_id, id),
    -- Only an invoice comes from an offer.
    ADD CONSTRAINT documents_source_offer_only_invoice
        CHECK (source_offer_id IS NULL OR type = 'invoice'),
    -- Only an offer has a validity date, and an offer has no service
    -- period (docs/concepts/solobill.md, "Offers").
    ADD CONSTRAINT documents_valid_until_only_offer
        CHECK (valid_until IS NULL OR type = 'offer'),
    ADD CONSTRAINT documents_offer_has_no_service_period
        CHECK (type <> 'offer' OR (service_date IS NULL AND service_date_to IS NULL)),
    ADD CONSTRAINT documents_service_period_order
        CHECK (service_date_to IS NULL OR (service_date IS NOT NULL AND service_date_to >= service_date)),
    -- An issued correction always has its sequence, nothing else ever
    -- does, and a draft gets it only at issue.
    ADD CONSTRAINT documents_correction_seq_consistent
        CHECK ((type = 'correction' AND state = 'issued') = (correction_seq IS NOT NULL)),
    -- 999 is numbering.MaxCorrectionSeq: the suffix "-999" is the most a
    -- display number of 40 characters leaves room for.
    ADD CONSTRAINT documents_correction_seq_range
        CHECK (correction_seq IS NULL OR (correction_seq >= 1 AND correction_seq <= 999)),
    ADD CONSTRAINT documents_correction_base_consistent
        CHECK ((correction_base_id IS NULL) = (correction_base_seq IS NULL)),
    ADD CONSTRAINT documents_correction_base_only_correction
        CHECK (correction_base_id IS NULL OR type = 'correction'),
    ADD CONSTRAINT documents_correction_base_seq_range
        CHECK (correction_base_seq IS NULL OR correction_base_seq >= 0),
    -- Every issued document has its display number, no draft has one.
    ADD CONSTRAINT documents_display_number_consistent
        CHECK ((state = 'issued') = (display_number IS NOT NULL)),
    -- The character set of internal/numbering (ValidNumber, Correction):
    -- ASCII letters, digits, "-", "_", ".", "/", not starting with ".",
    -- "-" or "/", at most 40 characters (numbering.MaxDisplay). Defence in
    -- depth: every path that writes a display number already checks it.
    ADD CONSTRAINT documents_display_number_shape
        CHECK (display_number IS NULL OR (
            display_number ~ '^[A-Za-z0-9_][A-Za-z0-9_./-]*$'
            AND char_length(display_number) <= 40
        )),
    ADD CONSTRAINT documents_original_number_only_imported
        CHECK (original_number IS NULL OR imported),
    ADD CONSTRAINT documents_original_number_length
        CHECK (original_number IS NULL OR char_length(original_number) <= 36),
    ADD CONSTRAINT documents_payment_terms_length
        CHECK (payment_terms IS NULL OR char_length(payment_terms) <= 1000),
    ADD CONSTRAINT documents_notes_length
        CHECK (notes IS NULL OR char_length(notes) <= 1000),
    ADD CONSTRAINT documents_customer_reference_length
        CHECK (customer_reference IS NULL OR char_length(customer_reference) <= 200);

-- The display number is unique per tenant: a collision, for example of a
-- new number with an imported one, refuses the issue (409 conflict). It
-- is the last guard behind the template rules of internal/numbering.
-- Drafts carry NULL, and NULLs never collide.
CREATE UNIQUE INDEX documents_display_number_unique ON documents (tenant_id, display_number);

-- A correction repeats the number and number_year of its original, so the
-- numeric uniqueness of issue #15 holds for every other type only. A
-- constraint cannot carry a WHERE clause, so it becomes a partial unique
-- index of the same name.
ALTER TABLE documents DROP CONSTRAINT documents_number_year_number_unique;
CREATE UNIQUE INDEX documents_number_year_number_unique
    ON documents (tenant_id, type, number_year, number)
    WHERE type <> 'correction';

-- Two corrections of one chain never share a sequence, also when two
-- issues race past the lock on the root row through a bug.
CREATE UNIQUE INDEX documents_correction_seq_unique
    ON documents (tenant_id, corrects_document_id, correction_seq)
    WHERE correction_seq IS NOT NULL;

-- The references a foreign key cannot check, because they depend on the
-- referenced row's type and state:
--
--   - corrects_document_id names an issued invoice, the root of the chain,
--     also for a correction of a correction.
--   - source_offer_id names an offer.
--   - correction_base_id names an issued document of the same chain: the
--     root itself or one of its corrections.
--
-- Row level security applies to the lookups, so a row of another tenant
-- is never found. The trigger fires after documents_block_issued_mutation
-- (triggers of one event fire in name order), so an UPDATE of an issued
-- row is refused as immutable first.
CREATE FUNCTION documents_check_references() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    ref_type text;
    ref_state text;
    ref_corrects uuid;
BEGIN
    IF NEW.corrects_document_id IS NOT NULL THEN
        SELECT type, state INTO ref_type, ref_state
        FROM documents
        WHERE tenant_id = NEW.tenant_id AND id = NEW.corrects_document_id;

        IF ref_type IS DISTINCT FROM 'invoice' OR ref_state IS DISTINCT FROM 'issued' THEN
            RAISE EXCEPTION 'document % must correct an issued invoice, the root of its chain', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.source_offer_id IS NOT NULL THEN
        SELECT type INTO ref_type
        FROM documents
        WHERE tenant_id = NEW.tenant_id AND id = NEW.source_offer_id;

        IF ref_type IS DISTINCT FROM 'offer' THEN
            RAISE EXCEPTION 'document % must name an offer as its source', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.correction_base_id IS NOT NULL THEN
        SELECT state, corrects_document_id INTO ref_state, ref_corrects
        FROM documents
        WHERE tenant_id = NEW.tenant_id AND id = NEW.correction_base_id;

        IF ref_state IS DISTINCT FROM 'issued'
            OR NOT (NEW.correction_base_id = NEW.corrects_document_id
                OR ref_corrects IS NOT DISTINCT FROM NEW.corrects_document_id) THEN
            RAISE EXCEPTION 'document % must be based on an issued document of its own chain', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER documents_check_references
    BEFORE INSERT OR UPDATE ON documents
    FOR EACH ROW
    EXECUTE FUNCTION documents_check_references();

ALTER TABLE documents ENABLE ALWAYS TRIGGER documents_check_references;

-- An issued document is immutable: the row triggers of issue #15 refuse
-- UPDATE and DELETE of an issued document and its lines. A row trigger
-- does not fire on TRUNCATE, so each document table gets a statement
-- trigger, the same shape files_block_truncate has. The application role
-- has no TRUNCATE grant anyway; this refuses it for every role, the table
-- owner included, unless the owner drops the trigger on purpose. It also
-- refuses truncating drafts: nothing has a reason to truncate a ledger
-- table.
CREATE FUNCTION documents_block_truncate() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'table % is part of the document ledger and is never truncated', TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER documents_block_truncate
    BEFORE TRUNCATE ON documents
    FOR EACH STATEMENT EXECUTE FUNCTION documents_block_truncate();

CREATE TRIGGER document_lines_block_truncate
    BEFORE TRUNCATE ON document_lines
    FOR EACH STATEMENT EXECUTE FUNCTION documents_block_truncate();

CREATE TRIGGER document_numbers_block_truncate
    BEFORE TRUNCATE ON document_numbers
    FOR EACH STATEMENT EXECUTE FUNCTION documents_block_truncate();

ALTER TABLE documents ENABLE ALWAYS TRIGGER documents_block_truncate;
ALTER TABLE document_lines ENABLE ALWAYS TRIGGER document_lines_block_truncate;
ALTER TABLE document_numbers ENABLE ALWAYS TRIGGER document_numbers_block_truncate;

-- A counter only ever moves up, and a counter row is never deleted or
-- moved to another key: a lowered or deleted counter would allocate a
-- number again that a customer already holds. The unique index on the
-- numbers would then refuse the issue, so this is defence in depth that
-- names the actual mistake. The application role has no DELETE grant on
-- document_numbers; the trigger also binds the table owner.
CREATE FUNCTION document_numbers_only_increase() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'document number counters are never deleted'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
        OR NEW.type IS DISTINCT FROM OLD.type
        OR NEW.number_year IS DISTINCT FROM OLD.number_year THEN
        RAISE EXCEPTION 'a document number counter never moves to another key'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF NEW.last_number < OLD.last_number THEN
        RAISE EXCEPTION 'document number counter % % would go down from % to %',
            OLD.type, OLD.number_year, OLD.last_number, NEW.last_number
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER document_numbers_only_increase
    BEFORE UPDATE OR DELETE ON document_numbers
    FOR EACH ROW
    EXECUTE FUNCTION document_numbers_only_increase();

ALTER TABLE document_numbers ENABLE ALWAYS TRIGGER document_numbers_only_increase;

-- migrate:down

DROP TRIGGER document_numbers_only_increase ON document_numbers;
DROP FUNCTION document_numbers_only_increase();

DROP TRIGGER document_numbers_block_truncate ON document_numbers;
DROP TRIGGER document_lines_block_truncate ON document_lines;
DROP TRIGGER documents_block_truncate ON documents;
DROP FUNCTION documents_block_truncate();

DROP TRIGGER documents_check_references ON documents;
DROP FUNCTION documents_check_references();

DROP INDEX documents_correction_seq_unique;
DROP INDEX documents_number_year_number_unique;
ALTER TABLE documents ADD CONSTRAINT documents_number_year_number_unique
    UNIQUE (tenant_id, type, number_year, number);
DROP INDEX documents_display_number_unique;

ALTER TABLE documents
    DROP CONSTRAINT documents_customer_reference_length,
    DROP CONSTRAINT documents_notes_length,
    DROP CONSTRAINT documents_payment_terms_length,
    DROP CONSTRAINT documents_original_number_length,
    DROP CONSTRAINT documents_original_number_only_imported,
    DROP CONSTRAINT documents_display_number_shape,
    DROP CONSTRAINT documents_display_number_consistent,
    DROP CONSTRAINT documents_correction_base_seq_range,
    DROP CONSTRAINT documents_correction_base_only_correction,
    DROP CONSTRAINT documents_correction_base_consistent,
    DROP CONSTRAINT documents_correction_seq_range,
    DROP CONSTRAINT documents_correction_seq_consistent,
    DROP CONSTRAINT documents_service_period_order,
    DROP CONSTRAINT documents_offer_has_no_service_period,
    DROP CONSTRAINT documents_valid_until_only_offer,
    DROP CONSTRAINT documents_source_offer_only_invoice,
    DROP CONSTRAINT documents_correction_base_id_fkey,
    DROP CONSTRAINT documents_source_offer_id_fkey,
    DROP CONSTRAINT documents_language_check;

ALTER TABLE documents
    DROP COLUMN correction_base_seq,
    DROP COLUMN correction_base_id,
    DROP COLUMN display_number,
    DROP COLUMN correction_seq,
    DROP COLUMN customer_reference,
    DROP COLUMN notes,
    DROP COLUMN payment_terms,
    DROP COLUMN service_date_to,
    DROP COLUMN valid_until,
    DROP COLUMN language,
    DROP COLUMN source_offer_id,
    DROP COLUMN original_number;

ALTER TABLE document_numbers DROP CONSTRAINT document_numbers_type_check;
ALTER TABLE document_numbers ADD CONSTRAINT document_numbers_type_check
    CHECK (type IN ('invoice', 'correction'));

ALTER TABLE documents DROP CONSTRAINT documents_type_check;
ALTER TABLE documents ADD CONSTRAINT documents_type_check
    CHECK (type IN ('invoice', 'correction'));
