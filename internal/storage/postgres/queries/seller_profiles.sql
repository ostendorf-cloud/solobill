-- Tenant isolation is two layers, as in customers.sql: every query names
-- the tenant from the transaction's app.tenant_id setting in its own
-- WHERE clause, and row level security enforces the same scope below.
-- The tenant is never a parameter a caller chooses, except the INSERT's
-- tenant_id, which row level security's WITH CHECK pins to the same value.

-- name: GetSellerProfile :one
SELECT tenant_id, legal_name, street, address_line2, postal_code, city, country_code,
       tax_number, vat_id, kleinunternehmer_id, default_vat_regime, default_language,
       bank_name, iban, bic, email, phone, website, footer_text, number_formats,
       created_at, updated_at
FROM seller_profiles
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- PutSellerProfile creates or replaces the profile as a whole. It names
-- its columns and never writes a logo column, so saving the profile never
-- drops the logo issue #75 adds.
--
-- name: PutSellerProfile :one
INSERT INTO seller_profiles (
    tenant_id, legal_name, street, address_line2, postal_code, city, country_code,
    tax_number, vat_id, kleinunternehmer_id, default_vat_regime, default_language,
    bank_name, iban, bic, email, phone, website, footer_text, number_formats
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
ON CONFLICT (tenant_id) DO UPDATE
SET legal_name = EXCLUDED.legal_name,
    street = EXCLUDED.street,
    address_line2 = EXCLUDED.address_line2,
    postal_code = EXCLUDED.postal_code,
    city = EXCLUDED.city,
    country_code = EXCLUDED.country_code,
    tax_number = EXCLUDED.tax_number,
    vat_id = EXCLUDED.vat_id,
    kleinunternehmer_id = EXCLUDED.kleinunternehmer_id,
    default_vat_regime = EXCLUDED.default_vat_regime,
    default_language = EXCLUDED.default_language,
    bank_name = EXCLUDED.bank_name,
    iban = EXCLUDED.iban,
    bic = EXCLUDED.bic,
    email = EXCLUDED.email,
    phone = EXCLUDED.phone,
    website = EXCLUDED.website,
    footer_text = EXCLUDED.footer_text,
    number_formats = EXCLUDED.number_formats,
    updated_at = now()
RETURNING tenant_id, legal_name, street, address_line2, postal_code, city, country_code,
          tax_number, vat_id, kleinunternehmer_id, default_vat_regime, default_language,
          bank_name, iban, bic, email, phone, website, footer_text, number_formats,
          created_at, updated_at;
