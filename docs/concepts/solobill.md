# solobill

Accounting for a sole trader: offers, invoices, expenses, EÜR and the numbers for Elster.

## Problem

- Commercial accounting tools for sole traders start around 20 EUR per month, more than this
  trade can carry at its current turnover.
- Offers and invoices are produced by hand today, so numbering, VAT treatment and the running
  totals depend on care rather than on the tool.
- The year-end EÜR means reconstructing income and expenses from documents and a bank statement,
  with no record of how a figure was derived.
- Receiving structured e-invoices has been mandatory for domestic B2B since 2025, and nothing in
  the current process can read or archive one.
- A GoBD audit would currently rest on files in folders, with no immutability and no audit trail.

## Goals

- Every outgoing document is produced by the tool: offer, invoice, correction, dunning notice.
- A discount can be given per line and on the whole document, as a percentage or an amount, and
  it appears on the document rather than being folded into the unit price.
- Invoice numbers are gapless per year and per document type, and never reused.
- An issued invoice is never modified; a correction is its own document referencing the original.
- Both VAT regimes work: Kleinunternehmer under section 19 UStG, and standard taxation.
- EÜR and USt-Voranmeldung are produced as reports naming the Elster form field for each figure.
- Outgoing invoices are ZUGFeRD 2.x, a PDF carrying the logo with the structured XML embedded.
- XRechnung is available as an explicit export for public-sector customers.
- An uploaded incoming invoice in either format prefills an expense without retyping.
- Every figure in a report can be traced back to the documents it came from.
- An AI agent can create and query documents through an MCP server, under a scoped token.
- Documents from 2023 onwards are imported, keeping the numbers they were issued under.
- The first release is correct for tax year 2026.
- Customers are owned here, and a connected system reads them rather than keeping its own.
- A second tenant can be added without a data migration.

## Non goals

- Direct transmission to Elster. ERiC stays out; the reports are filled into the official form.
- Double-entry bookkeeping, balance sheets, and anything beyond what an EÜR filer needs.
- Payroll, asset-heavy depreciation planning, and multi-currency.
- Automatic bank import in the first version. The interface is designed for it, nothing more.
- Default interest on a dunning notice. Fixed fees only, with the fields modelled for it.
- Correctness for a tax year before 2026. Earlier years are imported as records, not recomputed.
- Tax advice. The tool computes and cites; it does not decide treatment for the user.

## Approach

- Go API and Vue SPA on govue, Postgres, one Helm chart, images to ghcr. Public, behind login.
- Auth through govue's OIDC package, with a local account path behind the same interface, so a
  later SaaS self-signup does not need a second auth model.
- Every table carries `tenant_id`, every query is scoped by it, and Postgres row-level security
  enforces it below the application so a missed `WHERE` cannot leak across tenants.
- The core is an append-only document ledger. A document has a state, and a state transition is a
  new row; the document body of an issued invoice is frozen and hashed.
- Numbering is a per-tenant, per-type, per-year counter allocated inside the issuing transaction,
  so a gap cannot open under concurrency.
- An imported historical document carries the number it was issued under and is marked imported,
  outside that counter, so an import can never shift or reuse a number a customer already holds.
- Customers live here and are exposed through the API, so a connected system references them by
  id instead of holding a second copy that drifts.
- Rendering is separated from the ledger: a document's stored, frozen payload plus a template
  version produces the same PDF forever, which is what makes reproduction after years possible.
- The e-invoice layer is one port with two adapters, ZUGFeRD and XRechnung, in both directions.
- The tax engine takes ledger entries and a period and returns labelled figures. Elster field
  mapping is data, versioned per tax year, not code.
- Document storage is S3-compatible object storage, write-once, with the hash in the ledger row.
  PostgreSQL is the authority, the store holds the bytes: see "Receipt storage and GoBD".
- The MCP server is a second binary in the same repository, talking to the same API with a
  scoped token, so an agent can never exceed what that token grants.
- Mail goes out through the existing SMTP service, with send attempts recorded on the document.

## Rounding

- Every persisted amount is an integer minor unit (cents): catalogue price, line price, VAT,
  document totals. No column is ever a float.
- A line's net amount is computed at full precision before any rounding: quantity times unit
  price. A fractional quantity (hours, days) means this multiplication is not done in integer
  cents directly; it is done at sub-cent precision (a scaled integer or a rational, never a
  float) and only rounded to the cent at the point the figure is persisted or displayed.
- A percentage discount, per line or per document, is applied at that same sub-cent precision,
  rounded to the nearest cent exactly once, using commercial rounding (half away from zero,
  kaufmännisches Runden), at the point the figure is persisted or displayed. It is never rounded
  twice.
- VAT rounds once per rate, on that rate's net total after discount, matching issue #15's own
  prior decision, never once per line.
- A document-level percentage discount is allocated across lines by the largest-remainder
  method: compute each line's exact, unrounded share of the discount first, round every share
  down to the cent, then distribute the leftover cents (the document-level discount amount minus
  the sum of the rounded-down shares) one cent at a time to the lines with the largest
  fractional remainder, breaking ties by line order. The sum of the per-line shares then always
  equals the document-level discount exactly: no cent is lost or invented. Each line's exact
  share is that line's own net *after* any line-level discount, never the raw, pre-line-discount
  net: a document-level discount is the final adjustment on top of whatever the customer is
  already being charged per line, so it is proportional to what that line is actually worth at
  that point, not to a figure the line-level discount has already superseded.
- A document-level amount discount, not a percentage, needs no allocation: it is already its own
  figure and is shown as such, never divided across lines.
- Rejected: carrying the whole rounding residual on one named line. It makes that line's shown
  figure depend on how many other lines exist, which the reader cannot see, rather than on that
  line's own weight.

### Worked example

A document with two lines and a 15% document-level discount, VAT at 19%:

- Line 1: net after its own line-level discount, EUR 120.04.
- Line 2: no line-level discount, net EUR 70.04.
- Document net total: EUR 190.08.

Each line's exact, unrounded share of the 15% document discount:

- Line 1: 120.04 × 0.15 = EUR 18.006.
- Line 2: 70.04 × 0.15 = EUR 10.506.
- Exact total: EUR 28.512, which rounds half away from zero to EUR 28.51. This rounded total,
  not the floored shares below, is the figure the per-line shares must add up to.

Largest-remainder allocation: floor every share to the cent first (Line 1 EUR 18.00, Line 2
EUR 10.50; floored sum EUR 28.50), then hand out the single leftover cent (EUR 28.51 minus
EUR 28.50) to the line with the largest fractional remainder. Both lines carry the same
remainder here (EUR 0.006), so the tie breaks by line order and Line 1 receives it:

- Line 1 discount: EUR 18.01.
- Line 2 discount: EUR 10.50.
- Sum: EUR 28.51, exactly the document-level discount.

Rounding each line's exact share independently instead of applying the largest-remainder step
looks equally correct line by line (EUR 18.006 rounds to EUR 18.01, EUR 10.506 rounds to
EUR 10.51), but sums to EUR 28.52, one cent more than the document actually discounts. That
invented cent, invisible unless the two numbers are added back up, is exactly what per-document
rounding (round the total once, then allocate) is for, against per-line rounding (round every
line first, then add).

Carrying the example to the document total: net after discount is EUR 102.03 for Line 1 and
EUR 59.54 for Line 2, summing to EUR 161.57 (190.08 minus 28.51). VAT rounds once on that total,
not per line: 161.57 × 0.19 = 30.6983, rounding half away from zero to EUR 30.70. Document
total: EUR 161.57 + EUR 30.70 = EUR 192.27.

## Year end milestone 2026

The owner uses solobill for the 2026 year end. Issue #67 is the contract every other issue of
the milestone (#68 to #82) builds against. Only #67 edits this document in the milestone.

### Decisions of the owner

- Receipts, imported originals and the logo go to the S3 compatible store of the cluster
  (SeaweedFS). SHA-256, size and metadata live in PostgreSQL.
- The PDF engine is Typst, behind a 4 hour spike in issue #74. The spike result is an ADR.
- 2026 documents already issued are imported with their original numbers.
- The import format is decided later. Only `POST /api/v1/imports` and `original_number` are
  reserved now.
- Depreciation: linear, GWG and Sammelposten only. No degressive method.
- The owner is a Kleinunternehmer today. Both VAT regimes are built and tested.
- Documents are rendered in German and in English.
- Nothing is cut from the scope.
- The EÜR mapping is data per tax year. 2025 is verified, 2026 is a copy marked unverified.
- A correction replaces the invoice. It carries the full corrected lines and the original's
  number with a suffix. See "Corrections".
- The display number format is configurable per document type by a template. The owner's
  numbers look like `RE-20260009`. See "Number format".

### Payments

- A `payments` table, append-only. A trigger blocks UPDATE and DELETE.
- A payment belongs to an invoice chain: `document_id` is always the original invoice, the root
  every correction of the chain references. Partial payments are allowed.
- A recording mistake is undone by a reversal row: a negative amount naming the reversed payment
  in `reverses_payment_id`.
- Money paid back to the customer is a refund: a negative amount without
  `reverses_payment_id`, dated the day the money left.
- `received_on` is the Zufluss or Abfluss date section 11 EStG counts by. The EÜR uses payments
  only, never document totals or invoice dates.
- The open amount is the effective document's grand total minus the sum of the chain's
  payments. It is derived, never stored. A negative open amount means a refund is due.
- An issued invoice cannot carry a paid date, because `documents_block_issued_mutation` refuses
  every change. This is why payments are a table of their own.
- Under the standard regime the VAT share of a partial payment is split pro rata. This is an
  assumption the owner confirms in issue #71.
- Which document sets that ratio: the chain's effective document on the payment's
  `received_on`, that is the newest correction issued on or before that day, or the original.
- A later correction does not recompute the VAT share of payments received before it. The EÜR
  counts each payment's amount with the ratio of its own day. A difference the correction
  causes shows up only through the refund or the additional payment that follows it.
- The legal side of both rules (section 13 and 17 UStG on corrections and partial payments) is
  not verified. The owner confirms it with a Steuerberater.

### Offers

- `offer` is a third document type with its own gapless counter per year.
- Issue #70 widens both `documents_type_check` and `document_numbers_type_check` by a new
  migration. The old migrations stay untouched.
- An offer has a validity date and no service period. It is never paid and never in the EÜR.
- `POST /api/v1/offers/{id}/to-invoice` copies the lines into an invoice draft and sets
  `source_offer_id` (composite foreign key, same tenant).

### Corrections

The owner's decision: a correction replaces the invoice.

- A correction is its own document of type `correction`. It is issued and frozen like an
  invoice and never changes the original.
- It carries the full corrected lines, never a difference and never negative quantities.
  `internal/money` is unchanged.
- `corrects_document_id` always names the original invoice, the root of the chain, also for a
  correction of a correction.
- Its number is the original's display number plus `-1`, `-2` and so on.
  `documents.correction_seq` counts per original and is set in the issuing transaction, under a
  lock on the root row. It never comes from `document_numbers`.
- The numeric `number` and `number_year` of a correction repeat the original's. Issue #70
  therefore restricts `documents_number_year_number_unique` to the other types and adds a unique
  index on `(tenant_id, corrects_document_id, correction_seq)`.
- The newest issued correction of a chain is the effective document. Every older document of
  the chain is shown as superseded. "Superseded" is derived when reading, never written onto an
  issued row.
- Payments belong to the chain and the open amount is computed against the effective document.
  A lower corrected total leaves a negative open amount, settled by a refund payment.
- The EÜR counts payments only, so a correction changes the EÜR only through the payments and
  refunds that follow it.
- A new correction copies the lines of the current effective document as its draft.
- The draft records its base: `correction_base_id` and `correction_base_seq`, the effective
  document it copied from. At issue, under the lock on the root row, issue #70 compares the base
  with the chain's effective document and refuses with `409 correction_base_changed` when they
  differ. Example: draft A is created while correction 1 is effective, correction 2 is issued
  meanwhile, A is refused, because it would replace lines it never saw. The owner discards A
  and starts a new correction from correction 2.
- The rules are `internal/docchain`: effective, superseded, next sequence, base check. Pure and
  tested, called by #70.
- An imported historic correction keeps the number it was issued under. The suffix rule does
  not apply to it, and its original may be unknown to solobill.

Not verified, and not claimed:

- Whether a number with a suffix satisfies section 14 Abs. 4 Satz 1 Nr. 4 UStG and section 31
  UStDV for a correction. The owner or a Steuerberater confirms it before the first correction
  is issued.
- Whether the GoBD prescribe anything about the form of a correction. The GoBD letter was not
  read in full. This model follows the owner's decision, not a known requirement of the GoBD
  and not the behaviour of another product.

### Number format

The owner's decision: the display number format is a template per document type.

- The profile holds `number_formats`, one template for `invoice` and one for `offer`.
- Tokens: `{year}` is the four digit number year, `{yy}` its last two digits, `{seq}` the
  number without padding, `{seq:N}` the number padded with zeros to at least N digits (N from 1
  to 9).
- `{{` and `}}` are a literal `{` and `}`. A single `}` and an unknown token are errors at save.
- Every other character must be an ASCII letter, a digit, `-`, `_`, `.` or `/`. No space, no
  control character, no quote, no backslash, nothing outside ASCII.
- A template must contain exactly one `{seq}` or `{seq:N}` and exactly one `{year}` or `{yy}`,
  so a number is unique per year, and it must start with a literal prefix.
- A rendered number is at most 36 characters, checked at save against a nine digit counter. A
  correction suffix of up to four characters keeps every display number within 40.
- The owner's format is `RE-{year}{seq:4}`: number 9 of 2026 renders as `RE-20260009`.
- Defaults when no template is saved: `RE-{year}{seq:4}` for an invoice, `AN-{year}{seq:4}` for
  an offer. They differ in their prefix.
- On save, the templates of all types are checked together: no type's literal prefix may be a
  prefix of another type's. Two numbers with such prefixes differ at the latest at the end of
  the shorter prefix, so an invoice and an offer can never render the same string.
- At issue the rendered number is stored in `documents.display_number`, unique per tenant (a
  unique index). A collision, for example with an imported number, refuses the issue with `409
  conflict`. Every list and read returns the stored value. A template change never rewrites an
  issued number.
- A correction has no template: it is the original's number plus `-1`, `-2` and so on.
- An imported document stores its original number in `display_number` as well, so the unique
  index also catches a collision between an imported and a new number.
- The numeric counter in `document_numbers` stays gapless per tenant, type and year. The
  template only renders it.
- Where a number leaves solobill it needs no escaping beyond one rule, because the character
  set excludes quotes, backslashes, spaces and control characters:
  - File name (`Content-Disposition`): `numbering.FileName`, `/` becomes `_`, plus `.pdf`.
  - Typst: the number is passed as data in the JSON input, never spliced into markup.
  - CSV: every field is quoted by the CSV writer as usual; the number needs nothing more.
- `internal/numbering` holds the parser, the renderer, the set check, the defaults and the file
  name rule, with tests. Issue #68 calls it on save, issue #70 at issue.

### Imported documents and original numbers

- `documents.original_number text` keeps the number exactly as the customer holds it, for
  example `RE-2024-017`. Display uses it when set.
- `documents.number` stays the numeric key of `documents_number_year_number_unique`. When the
  original number matches the type's template, the import reads the number back out of it
  (`RE-20260009` under `RE-{year}{seq:4}` is 9 of 2026). Otherwise it applies the rule
  `docs/import.md` states. `original_number` keeps the original unchanged in both cases.
- An imported row is `imported = true` and never passes through `document_numbers`.
- The import enters an issued document by the one controlled path
  `storage.ImportedDocumentRepository`: it creates a draft with its lines, then moves that draft
  to issued in one UPDATE. The trigger refuses only a change of a row that is already issued,
  so it stays as strict as today. No trigger is relaxed and no second table is added.
- An imported document still has a frozen payload: a `FrozenPayloadV1` with `imported` set, its
  totals from the import row and `original` naming the original PDF by file id and SHA-256.
- Order of the import: first every original PDF is uploaded through `POST /api/v1/files` with
  purpose `import_original`. The import rows then reference those file ids. The dry run checks
  that each file exists. The commit freezes the payload with the file id and its hash.
- That link lives only inside the frozen payload, which no trigger on `files` can see. The
  grant protects it: the application role cannot update or delete a `files` row.
- For 2026 the counter of each type is raised to the highest imported number in the commit
  transaction, so the next issued number continues the sequence.
- Payments of imported 2026 invoices are imported too (`payments.imported`), because the EÜR
  counts by the day of receipt.
- Imported 2023 to 2025 documents are archive only and excluded from every report.

### VAT regime per period

- The period table is the source. A table `vat_regime_periods(tenant_id, valid_from, regime,
  note, supersedes_id)` holds the history. A switch inside a year is one more row.
- The table is append-only. A wrong row is corrected by a new row naming it in `supersedes_id`.
  The new row carries its own `valid_from`. A row is superseded at most once.
- The effective periods are the rows no other row supersedes. The regime on a date is the
  regime of the latest effective period starting on or before that date.
- A new draft's form is prefilled with the regime of the period that applies today. Only when
  no period exists yet does the profile's `default_vat_regime` prefill it. The default never
  decides a tax treatment.
- At issue the regime is evaluated again against the issue date. When the draft's regime
  differs, the issue is refused with `409 vat_regime_mismatch`. The owner changes the draft.
- An issued document keeps the regime frozen in its payload. A later period row never changes
  it.
- An expense and other income use the period of their payment date.
- No period covering a date refuses issuing, booking and the report with
  `400 vat_regime_missing`.

### Seller data validation

- Issue #68 exports the validation, issue #70 calls it at issue time. The signature is part of
  this contract:

```go
// Package sellerprofile, file internal/sellerprofile/validate.go, issue #68.
// The result maps a JSON path under "seller." to a reason. Empty means complete.
func Validate(p storage.SellerProfile, regime storage.VATRegime) map[string]string
```

- Standard regime: legal name, full address, and a Steuernummer or USt-IdNr.
- Kleinunternehmer: legal name, full address, and one of Steuernummer, USt-IdNr or
  Kleinunternehmer-Identifikationsnummer.
- Issue #70 merges this map with the one `docpayload.Validate` returns and answers both as the
  error's `details`, unchanged.
- One field path scheme for every validation: JSON names joined by `.`, an array index as
  `[i]`. Examples: `seller.tax_number`, `lines[1].description`. The path is a key directly in
  `details`, the same place govue's JSON decoder puts a wrongly typed field.

### Frozen payload

- `internal/docpayload.FrozenPayloadV1` is the shape of `documents.frozen_payload`.
- It carries a `version` field. A later shape is a new type, a stored payload is never migrated.
- Version 1 is fixed forever. A golden JSON test pins every field name.
- It holds every field of section 14 Abs. 4 Satz 1 Nr. 1 to 8 UStG and of section 34a UStDV,
  a seller snapshot, a customer snapshot, the lines and the totals from `internal/money`.
- It also holds, so no version 2 is needed for them: `currency` (only `EUR`), a second address
  line, the customer number, the customer's own reference, and for a correction `corrects` and
  `correction_seq`.
- `Validate` runs `internal/money` again over the payload's own lines and discounts and compares
  every frozen line figure, every bucket and every total with the result. A payload can never
  carry figures its lines do not produce.
- An imported document has no recomputable lines. For it `Validate` checks the arithmetic
  only: net minus discount, the bucket sums, each bucket's VAT rounded once, the grand total,
  and a VAT total of 0 under the Kleinunternehmer regime.
- A correction's `corrects` needs id, number and a valid issue date; only a correction may
  carry it. An imported correction may lack the id and is not checked against the suffix rule.
- The payload holds the printed tax note as text in the document language, so a later wording
  change never alters an issued document.
- The renderer formats and never computes.
- Nr. 9 (retention note of section 14b) and Nr. 10 ("Gutschrift") are out of this milestone.

### Receipt storage and GoBD

The rules: section 146 Abs. 4 AO (no change may hide the original), section 147 Abs. 2 and 3 AO
(readable and complete for the retention period), section 14b UStG (invoices eight years).

- PostgreSQL is the authority. `files` holds object key, version id, SHA-256, size, the sniffed
  content type, the original filename as data only, and the upload time.
- The SHA-256 is computed while streaming the upload. A download recomputes it and aborts on a
  mismatch.
- The primary control on the metadata is the grant: the application role gets `SELECT` and
  `INSERT` on `files`, never `UPDATE` or `DELETE`. No row can change once written.
- A trigger that refuses a change of a file linked to a booked expense or booked other income is
  optional defence in depth. It cannot see a link that lives only in a frozen payload (an
  imported original, the logo hash); the grant protects those.
- Booked expenses, payments and issued documents are immutable by trigger. A booked row
  references the file by id, so the hash it was booked with cannot drift.
- The application has no delete path for an object. `storage.ObjectStore` has no Delete.
- The object key is generated (`<tenant id>/<file id>`). The filename is never a path or a key.
- Retention: section 147 Abs. 3 AO gives ten years for books and eight for Buchungsbelege. The
  default is ten years for everything, counted from the end of the calendar year.
- The PostgreSQL backup of the cluster covers the metadata. The bucket needs its own backup.

What the hash does not do:

- The hash detects a changed object only when it is read: on a download or in the integrity
  scan. It does not prevent the change.
- Without versioning or object lock, an object overwritten or deleted in the store with the
  bucket credentials or an admin key is gone. It cannot be recovered. That breaks the retention
  duty of section 147 AO.

So the store side is a production requirement:

- Bucket versioning on, so an overwrite or delete keeps the earlier version.
- Object lock in compliance mode, with a retention date of the end of the year plus ten years,
  requested per object when `S3_OBJECT_LOCK` is on. A locked object cannot be deleted or
  overwritten before that date, even with admin credentials.
- Both are set up by the owner in the infrastructure repository: a new bucket created with
  object lock enabled (SeaweedFS allows it only at bucket creation), versioning on, and a bucket
  policy that adds the versioning, retention and legal hold actions.
The owner's decision on what happens while that protection is missing: only the file routes
are gated.

- solobill checks the bucket at startup and then periodically (every minute): is it
  reachable, is versioning on, is object lock configured.
- When the bucket is unreachable, or reports no versioning while `S3_REQUIRE_VERSIONING=true`
  (the default), the file routes answer `503 object_store_unavailable`: upload, download, the
  logo, and the attachment of an import original. Every other route keeps working: invoices,
  customers, payments, expenses without a new receipt, the report.
- `/readyz` stays about the database only. The pod keeps taking traffic.
- Every check that finds the protection missing logs a warning at level `WARN` with the reason.
- `GET /api/v1/status` reports `object_store.reachable`, `versioning`, `object_lock` and
  `gobd_protection_active`. The UI shows a banner while `gobd_protection_active` is false.
- Local development sets `S3_REQUIRE_VERSIONING=false`: the file routes work against the
  unversioned compose bucket, the warning and the banner stay.
- An integrity scan reads every object, recomputes its SHA-256 and compares it with the
  database. It runs as `solobill files verify`, a subcommand registered in
  `cmd/solobill/cmd_files.go`, scheduled as a Kubernetes CronJob (weekly by default) by the
  chart. A mismatch or a missing object exits non-zero and is logged with the file id. Issue
  #75 builds the checks, the status endpoint, the subcommand and the CronJob.

What remains unverified:

- Whether SeaweedFS 4.47 supports versioning and object lock. The SeaweedFS wiki says it does,
  but names no version.
- Whether the AWS provider's `object_lock_enabled` works against SeaweedFS, and whether its IAM
  mapper accepts the versioning and retention action names.
- Whether this design satisfies an auditor. The GoBD letter (BMF, 28 November 2019, amended
  2024 and 2025) was not read in full. Nothing here claims GoBD conformity.
- Whether the cluster backup of the bucket exists and covers ten years.

### Object store client and configuration

- Client: `github.com/minio/minio-go/v7`, licence Apache-2.0, compatible with distribution under
  the Elastic License 2.0. It is the client library only, not the MinIO server.
- Why not an own SigV4 client: request signing is security relevant code and a small client
  still needs checksums, retention headers and versioning calls.
- Why not the AWS SDK v2: more modules for the same calls against a non AWS store.
- `AGENTS.md` names no dependency policy. The size of minio-go's dependency tree is not verified
  and is checked in issue #75 before it is added.
- Configuration, loaded by `internal/config` in issue #75, no value in any file:

| Variable | Meaning |
| --- | --- |
| `S3_ENDPOINT` | `http://s3.seaweedfs.svc:8333` in the cluster, the compose service locally |
| `S3_BUCKET` | Bucket name |
| `S3_REGION` | Region string the store expects |
| `S3_USE_PATH_STYLE` | `true` for SeaweedFS |
| `S3_ACCESS_KEY_ID`, `S3_ACCESS_KEY_ID_FILE` | Key id, or a file holding it |
| `S3_SECRET_ACCESS_KEY`, `S3_SECRET_ACCESS_KEY_FILE` | Secret, or a file holding it |
| `S3_OBJECT_LOCK` | `true` requests retention per object and fails startup without a lock |
| `S3_REQUIRE_VERSIONING` | `true` (default): the file routes answer 503 while the bucket has no versioning |

- In the cluster the bucket, the user and the key come from the infrastructure repository and
  OpenBao. The chart takes a secret reference only.
- `docker-compose.yaml` runs SeaweedFS (Apache-2.0) as a single `weed server` with the S3
  gateway, reachable only on the compose network.

### Depreciation scope

- Linear by month: the acquisition month counts in full (section 7 Abs. 1 Satz 4 EStG).
- GWG up to 800 EUR, full deduction in the acquisition year (section 6 Abs. 2 EStG).
- Sammelposten above 250 EUR up to 1,000 EUR, one fifth per year over five years (section 6
  Abs. 2a EStG). A disposal does not reduce the pool.
- Items up to 250 EUR are expensed at once.
- No degressive AfA, although section 7 Abs. 2 EStG allows it for 2025 to 2027 acquisitions.
- The pure computation lives in `internal/afa` (issue #78): no clock, no database.
- An expense with `is_asset` set goes to the register and is not an expense in the EÜR. Only
  its depreciation is.
- A Kleinunternehmer has no Vorsteuer deduction, so the thresholds apply to the gross price.
  This reading of section 6 Abs. 2 and section 9b EStG is not verified with a Steuerberater.
- The useful life is entered per asset. The AfA tables were not read.
- Only the owner edits an asset, and only while its first schedule year is open: the calendar
  year of `acquired_on` (or the opening year of an imported asset) has not ended. Afterwards a
  depreciation figure may already be in a filed EÜR, so the asset is refused with `409`. A
  later change is a disposal.
- The EÜR report warns for every booked expense with `is_asset` set and no asset row.

### EÜR mapping as data per tax year

- The report (issue #80) maps line keys to the form line and Kennzahl of one tax year, as
  embedded data per year, not code.
- 2025 is verified against the BMF letter of 29 August 2025 (Anlage EÜR 2025), read as a copy
  on steuerschroeder.de, not on the BMF host.
- 2026 ships as a copy of 2025 marked `unverified`. The report shows a banner until the owner
  compares every line with the BMF letter of 1 September 2026 or the ELSTER form.
- Zufluss by `payments.received_on`, Abfluss by `expenses.paid_on`. The 10 day reading of
  section 11 EStG is from secondary sources only.
- Entries from 22 December to 10 January are listed for the owner to assign to a tax year.
  The decision is a row in `euer_assignments(tenant_id, entry_kind, entry_id, tax_year, note,
  supersedes_id, created_by, created_at)`, append-only. A changed decision is a new row naming
  the old one. Without a row an entry counts in the year of its payment date.
- Issue #80 creates the table and `GET` and `POST /api/v1/euer-assignments`.

### EÜR line keys

- A key names a line of the Anlage EÜR 2025, never a form number and never a Kennzahl.
- A key is stable forever. Booked rows reference it.
- "nd" and "d" split a line into its non deductible and deductible Kennzahl.
- Kind: `income` and `expense` are chosen by the owner, `asset` comes from the register,
  `derived` is computed by the report.
- The Kennzahlen in the key tables are the 2025 form as read for the plan. They are data of
  issue #80.
- The keys are registered per issue, one file pair each, so no issue edits this document:

| Issue | Code | Table | Content |
| --- | --- | --- | --- |
| #67 | `internal/category/keys_67.go` | `docs/concepts/euer-keys/67.md` | the verified 2025 lines |
| #76 | `internal/category/keys_76.go` | `docs/concepts/euer-keys/76.md` | the car cost lines 68 to 73, empty until read |
| #80 | `internal/category/keys_80.go` | `docs/concepts/euer-keys/80.md` | Zeile 65 (Kz 162 or 172 open), Zeile 13, Kz 111 on Zeile 10 |

- `internal/category`'s test reads every table and checks in both directions: each key has a
  row in its own issue's table, with the same kind and Zeile, and each row is a key.
- The open points of the set are listed in the table of the issue that resolves them.

### Migration timestamp reservation

- Every issue owns one range of dbmate versions. A migration file outside its issue's range is
  a review finding.
- The ranges follow the dependency order, so a table only references tables of a lower range.
- A merged migration is never edited. A fix is a new file in the same range.
- dbmate applies a pending version that is lower than one already applied, unless run with
  `--strict`. Not verified against dbmate 2.38.
- Every new table also needs its grants: one file `docker/postgres/grants/<range start>_<name>.sql`
  per issue, applied by compose and by the integration tests. See "Conflict zones".

| Issue | Range | Planned migrations |
| --- | --- | --- |
| #67 contract | none | ships no table |
| #68 seller profile | `20261121100000` to `20261121100099` | `seller_profiles` with `number_formats`, `vat_regime_periods` with `supersedes_id` |
| #69 customers, catalogue | `20261121110000` to `20261121110099` | `customers.document_language`, `customers.number`, `customers.address_line2` |
| #70 documents | `20261121120000` to `20261121120099` | widen `documents_type_check` and `document_numbers_type_check` for offers; `original_number`, `source_offer_id`, `language`, `valid_until`, `service_date_to`, `payment_terms`, `notes`, `customer_reference`, `correction_seq`, `correction_base_id`, `correction_base_seq`, `display_number` with a unique index per tenant; number uniqueness without corrections plus a unique `(tenant_id, corrects_document_id, correction_seq)` |
| #71 payments | `20261121130000` to `20261121130099` | `payments` with trigger, root invoice reference |
| #72 Vue shell | none | Vue only |
| #73 Vue documents | none | Vue only |
| #74 PDF | `20261121140000` to `20261121140099` | reserved, none planned |
| #75 files on S3 | `20261121150000` to `20261121150099` | `files` (grant SELECT and INSERT only), `seller_profiles.logo_file_id` |
| #76 expenses | `20261121160000` to `20261121160099` | `expenses` with `is_asset`, `other_income`, both with trigger |
| #77 Vue expenses | none | Vue only |
| #78 assets | `20261121170000` to `20261121170099` | `assets` |
| #79 Vue assets | none | Vue only |
| #80 EÜR report | `20261121180000` to `20261121180099` | `euer_assignments`, append-only; the mapping is embedded data |
| #81 import | `20261121190000` to `20261121190099` | `import_batches`, batch references on imported rows |
| #82 hardening | `20261121200000` to `20261121200099` | reserved for fixes |

### Conflict zones

Every file that several issues would otherwise edit, and the rule that keeps them apart:

- `internal/httpapi`: one `routes_<resource>.go` per resource with its `registerX`. `NewHandler`
  and `routes.go` are not edited again. `Repositories` already has every field.
- `internal/httpapi/reserved_<resource>_test.go`: one file per routes file lists its 501 stubs.
  The issue that implements the routes deletes its own file. A test fails when a stub is listed
  under another file than the one that registers it.
- `internal/httpapi` test fakes: `buildTestApp` and `fake_store_test.go` are not edited again.
  A feature adds its fakes in its own `fake_<resource>_test.go`, whose `init` appends a hook to
  `repositoryHooks` (`testhooks_test.go`). Its tests read the fake back through
  `testApp.repos`.
- `internal/storage`: one domain file and one interface file per resource.
- `internal/storage/postgres`: every `func (s *Store) X()` accessor lives in its own
  `store_<resource>.go`. `store.go` is not edited again.
- `internal/storage/postgres/sqlcgen/models.go`: regenerated after a rebase, never hand merged.
- `cmd/solobill`: a feature wires itself in its own `wire_<feature>.go`, whose `init` appends a
  `wireFunc` to `wirers`. A subcommand lives in its own `cmd_<name>.go`, whose `init` calls
  `registerCommand` (`commands.go`); `solobill files verify` is `cmd_files.go` of #75. `main.go`,
  `wire.go` and `commands.go` are not edited again.
- `internal/config`: a feature loads its variables in its own `config_<feature>.go`. `Config`
  already carries `ObjectStore` (#75) and `PDF` (#74), and `Load` already calls both loaders.
- `charts/solobill`, two owners with disjoint files and blocks:
  - #75 owns `templates/_objectstore.tpl` (already included by `deployment.yaml` and
    `networkpolicy.yaml`), the `objectStore` block at the end of `values.yaml`, the
    `objectStore` property in `values.schema.json`, a new CronJob template for `files verify`
    and new test files.
  - #82 owns `templates/_requestlimits.tpl` (already merged into the annotations of
    `ingress.yaml` and `httproute.yaml`), the `requestLimits` block at the top of `values.yaml`,
    the `requestLimits` property at the top of `values.schema.json`, and the chart README notes.
  - The two blocks sit apart in both files, so the two branches never touch neighbouring lines.
    No other issue edits the chart.
- `internal/category` and its key tables: one `keys_<issue>.go` and one
  `docs/concepts/euer-keys/<issue>.md` per issue (#67, #76, #80). See "EÜR line keys".
- Grants: one SQL file per issue in `docker/postgres/grants/`, named after the issue's range
  start. `docker-compose.yaml`, `integration_test.go` and `charts/solobill/README.md` all read
  or point to that directory and are not edited for a new table.
- Row level security: every new table carries `tenant_id` and enables and forces RLS with a
  policy in its own migration. `TestEveryTenantScopedTableEnforcesRowLevelSecurity` finds those
  tables itself. `TestEveryTableIsTenantScopedOrAllowlisted` fails for a table without
  `tenant_id` unless it is in `tablesWithoutTenantID` with a reason; only #67 and #82 edit that
  list. `TestAppRoleCannotChangeFiles` asserts the files grant once #75 creates the table.
- `go.mod` and `go.sum`: only #74 (if Typst needs a Go module) and #75 (minio-go) add
  dependencies. The second to merge rebases onto `main`, runs `go mod tidy`, and never hand
  merges `go.sum`.
- `web/src/router/routes/<feature>.ts` and `web/src/i18n/locales/{de,en}/<feature>.json`: one
  per feature. The two index files are not edited again. A test fails when a file there is not
  imported.
- Navigation: derived from route meta. A feature adds `meta.nav` (`labelKey`, `order`,
  optional `roles`) to its own route. `navigationEntries` in `web/src/router/meta.ts` lists
  them, the navigation component of #72 renders them, and no feature edits that component.
- `docs/concepts/solobill.md` and `docs/api.md`: only #67 edits them in this milestone. A
  needed change goes back to the owner as a contract change. The key tables under
  `docs/concepts/euer-keys/` are separate files with their own owners.
- `AGENTS.md` layout: only issue #82 edits it. Until then the layout section is stale on
  purpose.

### Unverified tax points

- The Anlage EÜR 2026 line numbers and Kennzahlen.
- Kz 162 and 172 order in Zeile 65, and Kz 111 on Zeile 10 or 12.
- The 10 day rule for regularly recurring payments, and how USt-Vorauszahlungen fall under it.
- The gross GWG and Sammelposten thresholds for a Kleinunternehmer.
- A one year useful life for computer hardware and software (BMF letter of 22 February 2022),
  and whether the acquisition year is pro rata.
- The pro rata VAT split of partial payments under the standard regime.
- Whether a correction numbered as the original plus a suffix satisfies section 14 Abs. 4 Satz
  1 Nr. 4 UStG and section 31 UStDV.
- Every figure is a computation over the owner's own entries, not tax advice.

## Alternatives

- Extending an existing open-source tool such as Kivitendo or InvoicePlane: both are PHP, carry
  double-entry models far beyond an EÜR filer, and would be slower to bend than to replace.
- Buying lexoffice or Papierkram: rejected on cost, which is the reason this project exists.
- Storing documents as rows and re-rendering on demand: rejected because a template change would
  silently alter an already-issued invoice, which GoBD does not permit.
- Mutable invoices with an audit log beside them: rejected because immutability has to be
  structural, not a convention the next feature can break.
- Multi-tenancy by schema per tenant: rejected for a single-user start, where migration and
  connection handling cost more than row-level security does.
- Building the MCP server into the API binary: rejected because the agent surface and the human
  surface should not share a process or a credential.
- Receipts as `bytea` in PostgreSQL: simpler and inside the database backup, but rejected by the
  owner for the S3 store the concept already named. The database keeps the authority anyway.
- Receipts on a PersistentVolumeClaim: blocks a second replica and needs its own backup.
- A pure Go PDF library instead of Typst: no PDF/A, which the later ZUGFeRD goal needs. It stays
  the fallback if the Typst spike fails.

## Open questions

- Whether a Verfahrensdokumentation is written as part of this repository, and by whom.
- The import format (issue #81). The endpoint and `original_number` are reserved, the format is
  the owner's decision before that issue's step 2.
- Which bank interface a later import would use: FinTS directly, or an aggregator.
- Whether offers created by Eventory's webshop arrive through the API or through a shared table.
