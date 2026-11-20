-- Table grants of the application role for the tables up to
-- 20261113160002_create_document_numbers.sql.
--
-- One file per issue, named after the first version of its reserved
-- migration range (docs/concepts/solobill.md, "Migration timestamp
-- reservation"), so two branches never edit the same lines. Applied in
-- file name order by docker-compose.yaml's "grant-app-role" service, and
-- by internal/storage/postgres/integration_test.go's setUpTestAppRole,
-- which replaces the role name solobill_app with its own test role.
-- charts/solobill/README.md points a real deployment at these files.
--
-- Every file grants every privilege its tables need, SELECT and INSERT
-- included. The ALTER DEFAULT PRIVILEGES rule in docker-compose.yaml
-- does not cover a fresh database, where every migration runs before it.
-- Grant only what the code needs: files gets SELECT and INSERT and never
-- UPDATE or DELETE (docs/concepts/solobill.md, "Receipt storage and
-- GoBD").

GRANT SELECT, INSERT ON tenants TO solobill_app;
GRANT SELECT, INSERT, UPDATE ON accounts, invites TO solobill_app;

-- customers and catalogue_items only ever soft delete (an UPDATE setting
-- deleted_at), never a real DELETE.
GRANT SELECT, INSERT, UPDATE ON customers, catalogue_items TO solobill_app;

-- documents is the one table a real DELETE reaches (DeleteDraftDocument
-- on a still mutable draft). document_lines needs DELETE (the lines of a
-- draft are deleted and recreated on update, and ON DELETE CASCADE) but
-- never UPDATE: a raw UPDATE against document_lines is refused by this
-- grant itself (SQLSTATE 42501), before any trigger runs. Neither grant
-- lets the role change an issued document: the triggers
-- documents_block_issued_mutation and
-- document_lines_block_issued_document_mutation refuse that.
GRANT SELECT, INSERT, UPDATE, DELETE ON documents TO solobill_app;
GRANT SELECT, INSERT, DELETE ON document_lines TO solobill_app;

-- document_numbers is only ever inserted, read and incremented.
GRANT SELECT, INSERT, UPDATE ON document_numbers TO solobill_app;
