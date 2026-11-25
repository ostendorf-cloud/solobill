# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

# Frontend build stage: produces the dist/ bundle cmd/solobill's
# go:embed directive needs populated before the Go stage below can even
# compile, let alone serve anything (see AGENTS.md's "A build needs the
# frontend first"). Pinned to the exact node version mise.toml resolves
# node="26" to locally (node 26.13.0) and to the exact pnpm patch
# mise.toml pins (PNPM_VERSION below), so this image builds with the same
# toolchain task web:build does, not merely the same majors.
#
# web/vite.config.ts's own outDir is "../cmd/solobill/dist", relative to
# web/, so this stage keeps the same sibling layout the repository itself
# has (cmd/ next to web/) instead of building web/ in isolation.
#
# Digest for node:26.13.1-bookworm-slim is the multi-platform image index
# (linux/amd64, linux/arm64, linux/ppc64le), resolved against Docker Hub's
# API; re-resolve with
# "docker buildx imagetools inspect node:26.13.1-bookworm-slim" if this
# patch version ever moves. That command reports the index digest below,
# the same one a plain "docker pull" resolves against on either platform.
FROM node:26.13.1-bookworm-slim@sha256:0042e17d232a87a2cd485be1da7900fca4d10a4ad11b29e9285da1d2e54798c9 AS web

WORKDIR /src

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
# 1.27.4-bookworm, the exact patch mise.toml pins go to.
#
# Digest for golang:1.27.4-bookworm is the multi-platform image index
# (linux/amd64, linux/arm64, linux/386, linux/arm/v7, linux/ppc64le),
# matching mise.toml's pinned Go patch exactly; resolved against Docker
# Hub's API. Re-resolve with
# "docker buildx imagetools inspect golang:1.27.4-bookworm" if this patch
# version ever moves; that command reports the index digest below.
FROM golang:1.27.4-bookworm@sha256:ce59b41415e2524ae99926fe1f37ce48ba8c194dcbd2a56a65436cdcefa2bd34 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/cmd/solobill/dist/ cmd/solobill/dist/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/solobill ./cmd/solobill

# Typst stage: fetches the typst binary internal/pdf runs as "typst
# compile" (docs/adr/0001-typst-pdf-engine.md). It reuses the Go stage's
# pinned base image instead of pinning another one: it only needs curl,
# sha256sum and tar, and nothing of it reaches the runtime image except
# the binary and its licence files.
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
# Digest for gcr.io/distroless/static-debian12:nonroot is the multi-platform
# image index (linux/amd64, linux/arm64, linux/arm/v7, linux/s390x,
# linux/ppc64le), resolved against gcr.io's registry API; re-resolve with
# "docker buildx imagetools inspect gcr.io/distroless/static-debian12:nonroot"
# if this needs refreshing; that command reports the index digest below.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/solobill /usr/local/bin/solobill

# internal/pdf runs "typst" from PATH by default (pdf.DefaultBinary).
# /usr/local/bin is the fixed path. Typst is Apache-2.0, so its LICENSE
# and NOTICE ship next to it.
COPY --from=typst /out/typst /usr/local/bin/typst
COPY --from=typst /out/LICENSE /out/NOTICE /usr/local/share/doc/typst/

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

# Proves on every image build, for the platform being built, that the
# final image runs typst as the non root user. Exec form, because this
# image has no shell. The build fails when the binary cannot run.
RUN ["/usr/local/bin/typst", "--version"]

# No HEALTHCHECK: this image has no shell and no curl/wget to exec one
# with, and unlike cve-notifier's binary, cmd/solobill has no built-in
# "--healthcheck" mode of its own (out of this step's scope). Nothing in
# docker-compose.yaml depends on this image's own health as a result; the
# migrate service's completion is what sequences this container's start.
ENTRYPOINT ["/usr/local/bin/solobill"]
