-- Table grants of the application role for
-- 20261121150000_create_files.sql (issue #75). See
-- 20261113160002_base.sql for how these files are applied.
--
-- files is written once: SELECT and INSERT only, never UPDATE or DELETE,
-- on the table and on any column. This is the primary control of
-- docs/concepts/solobill.md, "Receipt storage and GoBD", asserted by
-- TestAppRoleCannotChangeFiles. No TRUNCATE, REFERENCES or TRIGGER either.
GRANT SELECT, INSERT ON files TO solobill_app;
