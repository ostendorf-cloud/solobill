-- migrate:up

-- seller_profiles holds the supplier data section 14 Abs. 4 Nr. 1 and 2
-- UStG and section 34a UStDV need on every invoice: one row per tenant
-- (issue #68). The tenant id is the primary key, so a second row for a
-- tenant cannot exist and the application upserts on it.
--
-- Saving an incomplete profile is allowed on purpose: completeness is
-- checked when a document is issued (internal/sellerprofile.Validate), not
-- here. So the five required address fields default to the empty string
-- instead of being forced to exist, while every optional field is NULL
-- when unknown, never an empty string, exactly as in customers.
--
-- The logo column does not exist yet. Issue #75 adds
-- seller_profiles.logo_file_id in its own migration, and the Put query
-- here names its columns, so saving the profile never writes it.
CREATE TABLE seller_profiles (
    tenant_id uuid PRIMARY KEY REFERENCES tenants (id),
    legal_name text NOT NULL DEFAULT '',
    street text NOT NULL DEFAULT '',
    address_line2 text,
    postal_code text NOT NULL DEFAULT '',
    city text NOT NULL DEFAULT '',
    -- ISO 3166-1 alpha-2, empty while unknown.
    country_code text NOT NULL DEFAULT '',
    -- Steuernummer, USt-IdNr and Kleinunternehmer-Identifikationsnummer.
    -- No format CHECK: the formats vary and this layer does not judge them.
    tax_number text,
    vat_id text,
    kleinunternehmer_id text,
    -- Only prefills a form while no vat_regime_periods row exists. The
    -- regime that applies on a date always comes from that table.
    default_vat_regime text NOT NULL,
    default_language text NOT NULL DEFAULT 'de',
    bank_name text,
    iban text,
    bic text,
    email text,
    phone text,
    website text,
    footer_text text,
    -- One template per document type, for example
    -- {"invoice": "RE-{year}{seq:4}", "offer": "AN-{year}{seq:4}"}. The
    -- grammar is checked by internal/numbering at save, not here: a CHECK
    -- would have to duplicate the parser. A missing key means the default.
    number_formats jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT seller_profiles_default_vat_regime_check CHECK (default_vat_regime IN ('standard', 'kleinunternehmer')),
    CONSTRAINT seller_profiles_default_language_check CHECK (default_language IN ('de', 'en')),
    CONSTRAINT seller_profiles_country_code_check CHECK (country_code = '' OR country_code ~ '^[A-Z]{2}$'),
    CONSTRAINT seller_profiles_number_formats_object CHECK (jsonb_typeof(number_formats) = 'object'),
    CONSTRAINT seller_profiles_address_line2_not_empty CHECK (address_line2 IS NULL OR address_line2 <> ''),
    CONSTRAINT seller_profiles_tax_number_not_empty CHECK (tax_number IS NULL OR tax_number <> ''),
    CONSTRAINT seller_profiles_vat_id_not_empty CHECK (vat_id IS NULL OR vat_id <> ''),
    CONSTRAINT seller_profiles_kleinunternehmer_id_not_empty CHECK (kleinunternehmer_id IS NULL OR kleinunternehmer_id <> ''),
    CONSTRAINT seller_profiles_bank_name_not_empty CHECK (bank_name IS NULL OR bank_name <> ''),
    CONSTRAINT seller_profiles_iban_not_empty CHECK (iban IS NULL OR iban <> ''),
    CONSTRAINT seller_profiles_bic_not_empty CHECK (bic IS NULL OR bic <> ''),
    CONSTRAINT seller_profiles_email_not_empty CHECK (email IS NULL OR email <> ''),
    CONSTRAINT seller_profiles_phone_not_empty CHECK (phone IS NULL OR phone <> ''),
    CONSTRAINT seller_profiles_website_not_empty CHECK (website IS NULL OR website <> ''),
    CONSTRAINT seller_profiles_footer_text_not_empty CHECK (footer_text IS NULL OR footer_text <> '')
);

-- Row level security is the actual multi-tenancy guarantee, with the same
-- shape as every other tenant scoped table: see
-- 20261113150832_create_customers.sql for why ENABLE and FORCE are both
-- needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE seller_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE seller_profiles FORCE ROW LEVEL SECURITY;

CREATE POLICY seller_profiles_tenant_isolation ON seller_profiles
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE seller_profiles;
