# solobill

Accounting for a sole trader: offers, invoices, expenses, EÜR and the numbers for Elster.

## What it does

Commercial accounting tools start around 20 EUR per month, more than a sole trader's turnover
can carry. solobill is a Go API and Vue single page application on `govue`, backed by Postgres,
built to replace hand-written offers and invoices with gapless numbering, both VAT regimes, and
reports that cite the Elster form field for every figure. See `docs/concepts/solobill.md` for the
full design.

The foundation is in place: local and OIDC login, sessions, the invite flow, row level security,
and a Vue shell that proves login and the token contract work end to end. No accounting feature
exists yet.

Login runs on `govue`'s `account`, `session` and `passkey` packages: password and TOTP in one
step, passkeys next to them, an invite that sets the password and confirms the TOTP code
together, and OIDC bound to the same account. solobill keeps the tenancy rules on top. See the
section "Login on govue" in `docs/concepts/solobill.md` and `charts/solobill/README.md` for
the cookie, proxy, replica and passkey settings a deployment needs.

## Install

```sh
task setup
```

Installs the pinned toolchain (Go, Node, pnpm, and the lint and release tooling) through `mise`.

## Use

```sh
task build       # builds web/ and compiles the solobill binary into bin/solobill
bin/solobill
```

The binary serves the HTTP API and the built Vue shell behind it; see `internal/config`'s package
doc for the environment variables it reads on startup, `DATABASE_URL` and the session signing key
included.

## Run it locally with Docker

```sh
cp .env.example .env
# fill in .env: openssl rand -hex 24 for the two database passwords,
# openssl rand -hex 32 for SESSION_CODEC_KEY and CSRF_KEY
docker compose up --build
```

This builds `Dockerfile`'s multi-stage image (the Vue frontend, then the Go binary with it
embedded, then a distroless runtime) and starts three services: `postgres`, a one-shot `migrate`
service that applies `db/migrations` and exits, and `solobill` itself, which waits for `migrate`
to finish before it starts. The database comes up with a seeded `demo` tenant
(`SEED_TENANT_SLUG`/`SEED_TENANT_NAME` in `.env.example`), so there is a tenant to use right away.
Open `http://localhost:8080`, not `127.0.0.1`: compose configures passkeys for the relying party
`localhost`, the one host WebAuthn allows over plain HTTP.

`solobill` never connects to Postgres as the bootstrap superuser `migrate` uses to run the
migrations: see the comment on `docker-compose.yaml`'s `postgres` service for why there are two
separate database roles, and `AGENTS.md` for the one-line summary.

The published image `ghcr.io/ostendorf-cloud/solobill` is one manifest list for `linux/amd64` and `linux/arm64`.

`task dev:up` and `task dev:down` wrap `docker compose up --build -d` and `docker compose down`.

## Development

```sh
task setup
task check
```

`task check` runs every lint, build and test target one after another; `task --list` shows them
individually. `task web:build` has to run before `task build` or `task test:go` can compile
`cmd/solobill`, since it embeds the frontend's output; `task build` and `task test:go` already
declare that dependency, so running either one through Task is enough.

`task check` never needs a database: `task generate:check` parses `db/migrations` statically, with
no connection, to confirm the sqlc-generated query code in `internal/storage/postgres/sqlcgen` is
up to date. The row level security isolation test needs a real Postgres 18, so it runs separately:

```sh
dbmate up
task test:integration
```

`DATABASE_URL` points at that Postgres; `dbmate up` applies `db/migrations`, and
`task test:integration` runs against the same database.

Releases are driven by [release-me](https://github.com/ostendorf-cloud/release-me): Conventional
Commits decide the version, and merging the release pull request it maintains cuts the release.
`CHANGELOG.md` is generated, never hand edited.
