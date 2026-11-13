#!/bin/sh
set -eu

# Creates the restricted application role that cmd/solobill actually
# connects as in docker-compose.yaml. See the "two Postgres roles"
# comment on that file's postgres service for the full reasoning; this
# script only carries out the role creation and grants it describes.
#
# The official postgres image runs every script under
# /docker-entrypoint-initdb.d/ exactly once, against a freshly
# initialized, empty data directory, as the bootstrap superuser
# (POSTGRES_USER). That is also why this script has no "IF NOT EXISTS"
# guard around CREATE ROLE: a data directory that already exists never
# reaches this script again, so the role can never already be there.
#
# APP_DB_PASSWORD is deliberately not POSTGRES_PASSWORD: that is the
# bootstrap superuser's own secret, and this script must not need it or
# be able to leak it into the restricted role. The two passwords travel
# as two different environment variables for exactly that reason.
: "${APP_DB_PASSWORD:?APP_DB_PASSWORD must be set (see .env.example)}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
	CREATE ROLE solobill_app LOGIN PASSWORD '${APP_DB_PASSWORD}' NOSUPERUSER NOBYPASSRLS;
	GRANT USAGE ON SCHEMA public TO solobill_app;
	GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, accounts, invites TO solobill_app;
SQL
