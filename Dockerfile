# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

# This file builds solobill's two images as named final stages, "api"
# (cmd/solobill, published as solobill-api) and "frontend" (cmd/frontend
# with the Vue build, published as solobill-frontend), rather than two
# Dockerfiles: both binaries share go.mod, go.sum and internal/, so one file
# keeps every pinned base image digest in one place. "docker build --target
# <name>" (see Taskfile.yaml, .github/workflows/checks.yaml and
# publish.yaml) builds only the stages that target depends on: BuildKit
# never evaluates a stage that is not an ancestor of the requested target,
# so "--target api" never runs the Node stage and "--target frontend" never
# fetches typst. internal/envsync pins both. A build with no "--target"
# resolves to the last stage, "frontend".

# Node stage: builds the Vue app into web/dist, the same path "task
# web:build" writes to locally. Only the "frontend" stage copies it. Pinned
# to the exact node version mise.toml resolves node="26" to locally and to
# the exact pnpm patch mise.toml pins (PNPM_VERSION below), so this image
# builds with the same toolchain task web:build does, not merely the same
# majors.
#
# Digest for node:26.13.1-bookworm-slim is the multi-platform image index
# (linux/amd64, linux/arm64, linux/ppc64le), resolved against Docker Hub's
# API; re-resolve with
# "docker buildx imagetools inspect node:26.13.1-bookworm-slim" if this
# patch version ever moves. That command reports the index digest below,
# the same one a plain "docker pull" resolves against on either platform.
FROM node:26.13.1-bookworm-slim@sha256:0042e17d232a87a2cd485be1da7900fca4d10a4ad11b29e9285da1d2e54798c9 AS web

WORKDIR /src/web

# corepack no longer ships with Node. nodejs/node PR 57617 dropped it,
# starting with Node.js 25. This image's Node 26 reflects that change.
# npm still ships, so npm installs corepack first. corepack then
# installs the pinned pnpm version below, the exact one mise.toml pins,
# so the lockfile this stage reads was written by the same pnpm.
# Renovate updates PNPM_VERSION through the regex manager in
# renovate.json, in the same pull request as the mise.toml pin (group
# "pnpm"); internal/envsync fails when the two differ.
# renovate: datasource=npm depName=pnpm
ARG PNPM_VERSION=12.23.0
RUN npm install -g corepack && corepack enable && corepack prepare "pnpm@${PNPM_VERSION}" --activate

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# Shared Go stage: downloads the module graph once, for both build stages
# below, so the cache layer and the base image digest exist in one place.
#
# Digest for golang:1.27.4-bookworm is the multi-platform image index
# (linux/amd64, linux/arm64, linux/386, linux/arm/v7, linux/ppc64le),
# matching mise.toml's pinned Go patch exactly; resolved against Docker
# Hub's API. Re-resolve with
# "docker buildx imagetools inspect golang:1.27.4-bookworm" if this patch
# version ever moves; that command reports the index digest below.
FROM golang:1.27.4-bookworm@sha256:ce59b41415e2524ae99926fe1f37ce48ba8c194dcbd2a56a65436cdcefa2bd34 AS go-deps

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Build stage for cmd/solobill, the API: a fully static binary.
# CGO_ENABLED=0 is safe here, not just fast: pgx/v5, the only database
# driver this binary links, is pure Go top to bottom (internal/pgio,
# pgconn, pgproto3, pgtype, pgxpool, ... - verified by "go list -deps"
# against this repository's go.mod, none of them import "C"), so there is
# no libc dependency this build is dropping by going static. It embeds
# nothing and never references the Node stage.
FROM go-deps AS build-api

COPY cmd/solobill/ cmd/solobill/
COPY internal/ internal/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/solobill ./cmd/solobill

# Build stage for cmd/frontend: a fully static binary that embeds nothing
# either. It reads the Vue build from FRONTEND_BUILD_DIR at startup, which
# the "frontend" stage below fills from the Node stage.
FROM go-deps AS build-frontend

COPY cmd/frontend/ cmd/frontend/
COPY internal/ internal/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/frontend ./cmd/frontend

# Typst stage: fetches the typst binary internal/pdf runs as "typst
# compile" (docs/adr/0001-typst-pdf-engine.md). Only the "api" stage copies
# it. It reuses the Go stage's pinned base image instead of pinning another
# one: it only needs curl, sha256sum and tar, and nothing of it reaches the
# runtime image except the binary and its licence files.
#
# The upstream archives are statically linked musl builds, so the binary
# runs on the distroless static runtime below without a libc.
#
# TYPST_VERSION must equal the typst pin in mise.toml:
# internal/envsync's TestDockerfileTypstMatchesMise fails when they
# differ. Renovate moves TYPST_VERSION through the regex manager in
# renovate.json, grouped with the mise.toml pin. Renovate cannot move the
# two checksums: on every version bump they are a manual step. Copy the
# "sha256:" digest GitHub shows for typst-x86_64-unknown-linux-musl.tar.xz
# and typst-aarch64-unknown-linux-musl.tar.xz on the release page of the
# new version. Never compute them from a download. Until both are
# replaced, the old checksums fail the build below, by design.
#
# The 0.14.2 checksums below are the asset digests of GitHub's release
# API for tag v0.14.2.
FROM golang:1.27.4-bookworm@sha256:ce59b41415e2524ae99926fe1f37ce48ba8c194dcbd2a56a65436cdcefa2bd34 AS typst

# renovate: datasource=github-releases depName=typst/typst
ARG TYPST_VERSION=0.14.2
ARG TYPST_SHA256_AMD64=a6044cbad2a954deb921167e257e120ac0a16b20339ec01121194ff9d394996d
ARG TYPST_SHA256_ARM64=491b101aa40a3a7ea82a3f8a6232cabb4e6a7e233810082e5ac812d43fdcd47a
ARG TARGETARCH

# The archives are xz compressed, and GNU tar calls the external xz for
# them. Installed explicitly, because whether this base image already
# ships xz-utils is not verified. It only unpacks an archive whose
# checksum was checked before, so its version does not matter.
RUN apt-get update \
    && apt-get install -y --no-install-recommends xz-utils \
    && rm -rf /var/lib/apt/lists/*

# Fails closed: an unknown architecture, an empty checksum or a checksum
# mismatch stops the build before anything is unpacked.
RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) triple=x86_64-unknown-linux-musl; sum="${TYPST_SHA256_AMD64}" ;; \
      arm64) triple=aarch64-unknown-linux-musl; sum="${TYPST_SHA256_ARM64}" ;; \
      *) echo "typst: no pinned archive for TARGETARCH '${TARGETARCH}'" >&2; exit 1 ;; \
    esac; \
    if [ -z "${sum}" ]; then echo "typst: no SHA-256 for ${TARGETARCH}" >&2; exit 1; fi; \
    curl -fsSL -o /tmp/typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-${triple}.tar.xz"; \
    echo "${sum}  /tmp/typst.tar.xz" | sha256sum -c -; \
    mkdir /out; \
    tar -xJf /tmp/typst.tar.xz -C /out --strip-components=1 \
      "typst-${triple}/typst" "typst-${triple}/LICENSE" "typst-${triple}/NOTICE"; \
    rm /tmp/typst.tar.xz; \
    /out/typst --version

# Runtime stage of the API, published as solobill-api:
# gcr.io/distroless/static-debian12 carries a minimal root filesystem,
# ca-certificates and tzdata, and nothing else - no shell, no package
# manager, no busybox. That trust store is the reason this is not
# "scratch": cmd/solobill makes outbound TLS connections of its own
# whenever OIDC_ISSUER_URL is set (to the identity provider) and whenever
# DATABASE_URL asks for sslmode=require (a managed Postgres, unlike the
# compose file's own local, same-network, sslmode=disable instance) - both
# need a CA trust store to verify a certificate, which scratch has none of.
# This mirrors this organisation's cve-notifier: same base image, same
# ":nonroot" tag, same reasoning in its own Dockerfile comment.
#
# Digest for gcr.io/distroless/static-debian12:nonroot is the multi-platform
# image index (linux/amd64, linux/arm64, linux/arm/v7, linux/s390x,
# linux/ppc64le), resolved against gcr.io's registry API; re-resolve with
# "docker buildx imagetools inspect gcr.io/distroless/static-debian12:nonroot"
# if this needs refreshing; that command reports the index digest below.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS api

COPY --from=build-api /out/solobill /usr/local/bin/solobill

# internal/pdf runs "typst" from PATH by default (pdf.DefaultBinary), and
# the chart sets PDF_TYPST_BINARY to this same fixed path. Typst is
# Apache-2.0, so its LICENSE and NOTICE ship next to it.
COPY --from=typst /out/typst /usr/local/bin/typst
COPY --from=typst /out/LICENSE /out/NOTICE /usr/local/share/doc/typst/

# Matches LISTEN_ADDR's default (":8080", internal/config.Load) and
# ADMIN_LISTEN_ADDR's default (":8081", the same file): the public
# listener the Ingress routes /api to, and the administrative one it does
# not, see internal/httpapi's package doc for why the two are split.
EXPOSE 8080
EXPOSE 8081

# Numeric, not "nonroot:nonroot": the same uid/gid the base image's own
# nonroot user already resolves to, kept explicit, matching cve-notifier.
USER 65532:65532

# Proves on every image build, for the platform being built, that the
# final image runs typst as the non root user. Exec form, because this
# image has no shell. The build fails when the binary cannot run.
RUN ["/usr/local/bin/typst", "--version"]

# No HEALTHCHECK: this image has no shell and no curl/wget to exec one
# with. The probes of the chart and the compose file's ordering cover it.
ENTRYPOINT ["/usr/local/bin/solobill"]

# Runtime stage of the frontend, published as solobill-frontend: the same
# distroless base. It serves the Vue build through govue's frontend package
# in app mode and makes no outbound connection at all. No typst here: only
# the API renders PDFs. The last stage, so a build with no "--target"
# builds it.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS frontend

COPY --from=build-frontend /out/frontend /usr/local/bin/frontend

# FRONTEND_BUILD_DIR's default (internal/frontendconfig.DefaultBuildDir).
# Owned by root and read only for the nonroot user: the server reads every
# file once at startup and never writes.
COPY --from=web /src/web/dist/ /usr/share/solobill/web/

# Matches LISTEN_ADDR's and ADMIN_LISTEN_ADDR's defaults (":8080" and
# ":8081", internal/frontendconfig): the public listener the Ingress routes
# every path but /api to, and govue's admin listener (health, readiness,
# metrics).
EXPOSE 8080
EXPOSE 8081

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/frontend"]
