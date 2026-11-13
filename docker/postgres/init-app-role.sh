#!/bin/sh
set -eu

# Creates the restricted application role that cmd/solobill actually
# connects as in docker-compose.yaml. See the "two Postgres roles"
# comment on that file's postgres service for the full reasoning; this
# script only carries out the role creation and schema-level grant it
# describes. The table-level grants (SELECT/INSERT/UPDATE on tenants,
# accounts and invites) are NOT done here: see docker-compose.yaml's
# own "grant-app-role" service for why those have to wait until after
# "migrate" has created those tables.
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

# The heredoc delimiter is quoted ('SQL', not SQL) so the shell never
# interpolates anything inside the heredoc body itself; the password is
# passed through psql's own -v/:'var' substitution instead, which quotes
# and escapes it as a SQL string literal. An unquoted heredoc would splice
# APP_DB_PASSWORD straight into "PASSWORD '...'" before psql ever parses
# it, so a password containing a single quote would either break the
# statement or run arbitrary SQL as the bootstrap superuser.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
	-v app_password="$APP_DB_PASSWORD" <<-'SQL'
	CREATE ROLE solobill_app LOGIN PASSWORD :'app_password' NOSUPERUSER NOBYPASSRLS;
	GRANT USAGE ON SCHEMA public TO solobill_app;
SQL
