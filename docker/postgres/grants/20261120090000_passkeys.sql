-- Table grants of the application role for
-- 20261120090000_create_passkey_credentials.sql (issue #86). See
-- 20261113160002_base.sql for how these files are applied.
--
-- A passkey is deleted for real (passkey.Store.DeleteCredential), and the
-- user handle goes with the last passkey of an account, so both tables
-- need DELETE. passkey_user_handles is never updated in place: a new
-- handle is a new row after the old one is gone.
GRANT SELECT, INSERT, UPDATE, DELETE ON passkey_credentials TO solobill_app;
GRANT SELECT, INSERT, DELETE ON passkey_user_handles TO solobill_app;
