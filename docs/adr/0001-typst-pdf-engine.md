# 0001 Typst renders the document PDFs

- Status: proposed
- Date: 2026-11-25
- Issues: #74 (spike), #139 (image)

## Context

- An issued document is rendered from its frozen payload and must give the same PDF on every
  render.
- The PDF must be PDF/A-3b, the base of a later ZUGFeRD file with attachments.
- The runtime image is `gcr.io/distroless/static-debian12:nonroot`: no shell, no libc, non root.
- Every published image targets `linux/amd64` and `linux/arm64`.
- A downloaded binary is pinned by version and by checksum per architecture.

## Decision

- `internal/pdf` renders through `typst compile` from Typst 0.14.2, in the image and in the tests.
- The spike in #74 kept Typst: the renderer, the template `document-v1` and the golden tests
  merged with it.
- Typst enforces PDF/A-3b (`pdf.PDFStandard`) and reads only the bundled fonts.
- The image carries the upstream static musl build at `/usr/local/bin/typst` for both
  architectures. The build verifies the SHA-256 of the release archive per architecture.
- The image build runs `typst --version` as the non root user in the final stage.
- The Dockerfile's `TYPST_VERSION` equals the pin in `mise.toml`, enforced by `internal/envsync`.

## Consequences

- Licence: Apache-2.0, read from the `LICENSE` of the local 0.14.2 install. `LICENSE` and
  `NOTICE` ship under `/usr/local/share/doc/typst/`.
- Image size added: not verified. No image was built for this record.
- Both architectures run in CI only. Neither was built or run locally for this record.
- The worst case payload renders to about 18 KB a page with 0.14.2 (`TestWorstCaseRender`).
- Renovate moves the version but not the checksums. Every bump needs the two release asset
  digests copied by hand, and the image build fails until they are.
- A Typst bump can change the layout of issued documents. The golden tests catch it before merge.

## Alternatives

- A pure Go PDF library: no PDF/A, which the ZUGFeRD goal needs. It stays the fallback.
- Building Typst from source with cargo: a Rust toolchain stage and a much longer image build.
- Typst only in `mise.toml`, not in the image: the image could not render a PDF.

## Related

- Concept: `docs/concepts/solobill.md`, section "Decisions of the owner".
