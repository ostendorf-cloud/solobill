# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# Frontend build stage: produces the dist/ bundle cmd/solobill's
# go:embed directive needs populated before the Go stage below can even
# compile, let alone serve anything (see AGENTS.md's "A build needs the
# frontend first"). Pinned to the exact node and pnpm versions mise.toml
# resolves node="26" and pnpm="12" to locally (node 26.13.0, pnpm
# 12.19.0), so this image builds with the same toolchain task web:build
# does, not merely the same majors.
#
# web/vite.config.ts's own outDir is "../cmd/solobill/dist", relative to
# web/, so this stage keeps the same sibling layout the repository itself
# has (cmd/ next to web/) instead of building web/ in isolation.
FROM node:26.13.1-bookworm-slim@sha256:5df79989ed685e09f0a98019db4cebfb22fb9df1a53c0161664a011f7ff6b0f1 AS web

WORKDIR /src

# corepack no longer ships with Node. nodejs/node PR 57617 dropped it,
# starting with Node.js 25. This image's Node 26 reflects that change.
# npm still ships, so npm installs corepack first. corepack then
# installs the pnpm version mise.toml resolves to, matching the comment
# above.
RUN npm install -g corepack && corepack enable && corepack prepare pnpm@12.19.0 --activate

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/
RUN cd web && pnpm install --frozen-lockfile

COPY web/ web/
RUN mkdir -p cmd/solobill && cd web && pnpm run build

# Go build stage: compiles a fully static binary. CGO_ENABLED=0 is safe
# here, not just fast: pgx/v5, the only database driver this binary
# links, is pure Go top to bottom (internal/pgio, pgconn, pgproto3,
# pgtype, pgxpool, ... - verified by "go list -deps" against this
# repository's go.mod, none of them import "C"), so there is no libc
# dependency this build is dropping by going static. Pinned to
# 1.27.0-bookworm, the exact patch mise.toml pins go to (not a newer
# 1.27.x): see mise.toml's own comment for why golangci-lint 2.15.0
# specifically needs that exact patch, a constraint this image is kept
# aligned with even though golangci-lint itself never runs here.
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/cmd/solobill/dist/ cmd/solobill/dist/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/solobill ./cmd/solobill

# Runtime stage: gcr.io/distroless/static-debian12 carries a minimal root
# filesystem, ca-certificates and tzdata, and nothing else - no shell, no
# package manager, no busybox. That trust store is the reason this is
# not "scratch": cmd/solobill makes outbound TLS connections of its own
# whenever OIDC_ISSUER_URL is set (to the identity provider) and whenever
# DATABASE_URL asks for sslmode=require (a managed Postgres, unlike this
# compose file's own local, same-network, sslmode=disable instance,
# documented in docker-compose.yaml) - both need a CA trust store to
# verify a certificate, which scratch has none of and would need built up
# by hand. This mirrors this organisation's cve-notifier, the only other
# repository here with a Dockerfile: same base image, same ":nonroot" tag,
# same reasoning in its own Dockerfile comment.
#
# Pinned by tag, not digest: resolving and recording a digest needs a
# registry pull, which this sandbox could not make (no network egress to
# a container registry; see the implementation report). ":nonroot" is
# itself the distroless project's own maintained, documented tag, not a
# floating "latest" - acceptable as the alternative to a digest, and the
# same choice cve-notifier already made.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/solobill /usr/local/bin/solobill

# Matches LISTEN_ADDR's default (":8080", internal/config.Load) and
# ADMIN_LISTEN_ADDR's default (":8081", the same file): the public
# listener Ingress routes to, and the administrative one it does not, see
# internal/httpapi's package doc for why the two are split. Override
# LISTEN_ADDR, ADMIN_LISTEN_ADDR and both EXPOSE lines together if a
# deployment needs other ports.
EXPOSE 8080
EXPOSE 8081

# Numeric, not "nonroot:nonroot": the same uid/gid the base image's own
# nonroot user already resolves to, and so redundant with the image's own
# default - kept explicit anyway, matching cve-notifier's Dockerfile.
USER 65532:65532

# No HEALTHCHECK: this image has no shell and no curl/wget to exec one
# with, and unlike cve-notifier's binary, cmd/solobill has no built-in
# "--healthcheck" mode of its own (out of this step's scope). Nothing in
# docker-compose.yaml depends on this image's own health as a result; the
# migrate service's completion is what sequences this container's start.
ENTRYPOINT ["/usr/local/bin/solobill"]
