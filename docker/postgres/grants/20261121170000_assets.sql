-- Table grants of the application role for
-- 20261121170000_create_assets.sql (issue #78). See
-- 20261113160002_base.sql for how these files are applied.
--
-- The full list of privileges on assets: SELECT, INSERT and UPDATE. An
-- asset is created, read, edited while its first schedule year is open,
-- and given a disposal date once. It is never deleted: a figure of a past
-- year may already be in a filed EÜR. No DELETE, TRUNCATE, REFERENCES or
-- TRIGGER.
GRANT SELECT, INSERT, UPDATE ON assets TO solobill_app;
