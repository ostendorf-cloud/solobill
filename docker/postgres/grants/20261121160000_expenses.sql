-- Table grants of the application role for
-- 20261121160000_create_expenses.sql and
-- 20261121160001_create_other_income.sql (issue #76). See
-- 20261113160002_base.sql for how these files are applied.
--
-- A draft is created, changed and deleted, so both tables need SELECT,
-- INSERT, UPDATE and DELETE. A booked row is protected by its table's
-- trigger, not by the grant: UPDATE and DELETE are refused once the row is
-- booked, for any role. No TRUNCATE, REFERENCES or TRIGGER.
GRANT SELECT, INSERT, UPDATE, DELETE ON expenses TO solobill_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON other_income TO solobill_app;
