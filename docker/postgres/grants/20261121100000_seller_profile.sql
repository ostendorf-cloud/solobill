-- Table grants of the application role for
-- 20261121100000_create_seller_profiles.sql and
-- 20261121100001_create_vat_regime_periods.sql (issue #68). See
-- 20261113160002_base.sql for how these files are applied.
--
-- seller_profiles is created and replaced as a whole through an upsert
-- (INSERT, then UPDATE), never deleted.
GRANT SELECT, INSERT, UPDATE ON seller_profiles TO solobill_app;

-- vat_regime_periods is append-only: a wrong row is corrected by a new
-- row naming it in supersedes_id. No UPDATE and no DELETE.
GRANT SELECT, INSERT ON vat_regime_periods TO solobill_app;
