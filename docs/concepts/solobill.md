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

### Payments

- A `payments` table, append-only. A trigger blocks UPDATE and DELETE.
- A payment belongs to an issued invoice or correction. Partial payments are allowed.
- A mistake is undone by a reversal row: a negative amount naming the reversed payment.
- `received_on` is the Zufluss date section 11 Abs. 1 EStG counts by. The EÜR uses it, never
  the invoice date.
- The open amount is derived from the grand total and the payments, never stored.
- An issued invoice cannot carry a paid date, because `documents_block_issued_mutation` refuses
  every change. This is why payments are a table of their own.
- Under the standard regime the VAT share of a partial payment is split pro rata. This is an
  assumption the owner confirms in issue #71.

### Offers

- `offer` is a third document type with its own gapless counter per year.
- Issue #70 widens `documents_type_check` by a new migration. The old migration stays untouched.
- An offer has a validity date and no service period. It is never paid and never in the EÜR.
- `POST /api/v1/offers/{id}/to-invoice` copies the lines into an invoice draft and sets
  `source_offer_id` (composite foreign key, same tenant).

### Imported documents and original numbers

- `documents.original_number text` keeps the number exactly as the customer holds it, for
  example `RE-2024-017`. Display uses it when set.
- `documents.number` stays the numeric key of `documents_number_year_number_unique`. The import
  derives it from the original number by a rule `docs/import.md` states.
- An imported row is `imported = true` and never passes through `document_numbers`.
- The import enters an issued document by the one controlled path
  `storage.ImportedDocumentRepository`: it creates a draft with its lines, then moves that draft
  to issued in one UPDATE. The trigger refuses only a change of a row that is already issued,
  so it stays as strict as today. No trigger is relaxed and no second table is added.
- An imported document still has a frozen payload: a `FrozenPayloadV1` with `imported` set, its
  totals from the import row and `original` naming the original PDF by file id and SHA-256.
- For 2026 the counter of each type is raised to the highest imported number in the commit
  transaction, so the next issued number continues the sequence.
- Payments of imported 2026 invoices are imported too (`payments.imported`), because the EÜR
  counts by the day of receipt.
- Imported 2023 to 2025 documents are archive only and excluded from every report.

### VAT regime per period

- The regime is not one profile flag. A table `vat_regime_periods(tenant_id, valid_from, regime,
  note)` holds the history. A switch inside a year is one more row.
- The regime on a date is the regime of the latest period starting on or before that date.
- A document copies the regime into `documents.vat_regime` when drafted. That column, frozen
  into the payload at issue, is what the document is taxed under.
- An expense and other income use the period of their payment date.
- The profile's `default_vat_regime` only prefills a new draft.
- No period covering a date blocks issuing and the report with a `400`.

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
- Issue #70 merges this map with `docpayload.Validate` and answers both as `details.fields`.

### Frozen payload

- `internal/docpayload.FrozenPayloadV1` is the shape of `documents.frozen_payload`.
- It carries a `version` field. A later shape is a new type, a stored payload is never migrated.
- It holds every field of section 14 Abs. 4 Satz 1 Nr. 1 to 8 UStG and of section 34a UStDV,
  a seller snapshot, a customer snapshot, the lines and the totals from `internal/money`.
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
  mismatch, so altered bytes are detected even when the store would allow the change.
- A trigger on `files` refuses DELETE, and any change of key, hash or size, once the file is
  linked to a booked expense, booked other income or an issued document.
- Booked expenses, payments and issued documents are immutable by trigger. A booked row
  references the file by id, so the hash it was booked with cannot drift.
- The application has no delete path for an object. `storage.ObjectStore` has no Delete.
- The object key is generated (`<tenant id>/<file id>`). The filename is never a path or a key.
- Retention: section 147 Abs. 3 AO gives ten years for books and eight for Buchungsbelege. The
  default is ten years for everything, counted from the end of the calendar year.
- The PostgreSQL backup of the cluster covers the metadata. The bucket needs its own backup.

The store side is a hardening step, not a blocker for the milestone:

- Bucket versioning: an overwrite keeps the old version. solobill checks it at startup and logs
  a warning when it is off.
- Object lock in compliance mode with a retention date of the end of the year plus ten years,
  requested per object when `S3_OBJECT_LOCK` is on. A locked object cannot be deleted or
  overwritten before that date, even with admin credentials.
- Both are set up by the owner in the infrastructure repository: a new bucket created with
  object lock enabled (SeaweedFS allows it only at bucket creation), versioning on, and a bucket
  policy that adds the versioning, retention and legal hold actions.
- The application works against a bucket without lock and without versioning. The database
  hash and triggers alone already detect every change. The store side only prevents it.

What remains unverified:

- Whether SeaweedFS 4.47 supports versioning and object lock. The SeaweedFS wiki says it does,
  but names no version.
- Whether the AWS provider's `object_lock_enabled` works against SeaweedFS, and whether its IAM
  mapper accepts the versioning and retention action names.
- Whether the hash in the database plus triggers satisfies an auditor without object lock. The
  GoBD letter (BMF, 28 November 2019, amended 2024 and 2025) was not read in full.
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

### EÜR mapping as data per tax year

- The report (issue #80) maps line keys to the form line and Kennzahl of one tax year, as
  embedded data per year, not code.
- 2025 is verified against the BMF letter of 29 August 2025 (Anlage EÜR 2025), read as a copy
  on steuerschroeder.de, not on the BMF host.
- 2026 ships as a copy of 2025 marked `unverified`. The report shows a banner until the owner
  compares every line with the BMF letter of 1 September 2026 or the ELSTER form.
- Zufluss by `payments.received_on`, Abfluss by `expenses.paid_on`. Entries from 22 December
  to 10 January are listed for the owner to assign. The 10 day reading of section 11 EStG is
  from secondary sources only.

### EÜR line keys

- A key names a line of the Anlage EÜR 2025, never a form number and never a Kennzahl.
- A key is stable forever. Booked rows reference it.
- "nd" and "d" split a line into its non deductible and deductible Kennzahl.
- Kind: `income` and `expense` are chosen by the owner, `asset` comes from the register,
  `derived` is computed by the report.
- The Kennzahlen below are the 2025 form as read for the plan. They are data of issue #80.
- `internal/category` holds the keys. Its test fails when a key has no row here.

| Key | Kind | Zeile 2025 | Kz 2025 | Line |
| --- | --- | --- | --- | --- |
| `euer.z12` | income | 12 | 111 | Betriebseinnahmen als Kleinunternehmer |
| `euer.z15` | income | 15 | 112 | Umsatzsteuerpflichtige Betriebseinnahmen |
| `euer.z16` | income | 16 | 103 | Umsatzsteuerfreie, nicht steuerbare Einnahmen, section 13b |
| `euer.z17` | derived | 17 | 140 | Vereinnahmte Umsatzsteuer |
| `euer.z18` | income | 18 | 141 | Vom Finanzamt erstattete Umsatzsteuer |
| `euer.z19` | income | 19 | 102 | Veräußerung oder Entnahme von Anlagevermögen |
| `euer.z20` | income | 20 | 106 | Private Kfz-Nutzung |
| `euer.z21` | income | 21 | 108 | Sonstige Sach-, Nutzungs- und Leistungsentnahmen |
| `euer.z27` | expense | 27 | 100 | Waren, Rohstoffe und Hilfsstoffe |
| `euer.z29` | expense | 29 | 110 | Bezogene Fremdleistungen |
| `euer.z30` | expense | 30 | 120 | Ausgaben für eigenes Personal |
| `euer.z32` | asset | 32 | 131 | AfA auf immaterielle Wirtschaftsgüter |
| `euer.z33` | asset | 33 | 130 | AfA auf bewegliche Wirtschaftsgüter |
| `euer.z36` | asset | 36 | 132 | Geringwertige Wirtschaftsgüter |
| `euer.z37` | asset | 37 | 137 | Auflösung Sammelposten |
| `euer.z38` | asset | 38 | 135 | Restbuchwerte ausgeschiedener Anlagegüter |
| `euer.z39` | expense | 39 | 150 | Miete und Pacht für Geschäftsräume |
| `euer.z43` | expense | 43 | 280 | Telekommunikation |
| `euer.z44` | expense | 44 | 221 | Übernachtungs- und Reisenebenkosten |
| `euer.z45` | expense | 45 | 281 | Fortbildungskosten |
| `euer.z46` | expense | 46 | 194 | Rechts- und Steuerberatung, Buchführung |
| `euer.z47` | expense | 47 | 222 | Miete und Leasing für bewegliche Wirtschaftsgüter |
| `euer.z48` | expense | 48 | 225 | Erhaltungsaufwendungen |
| `euer.z49` | expense | 49 | 223 | Beiträge, Gebühren, Abgaben und Versicherungen |
| `euer.z50` | expense | 50 | 228 | Laufende EDV-Kosten |
| `euer.z51` | expense | 51 | 229 | Arbeitsmittel |
| `euer.z52` | expense | 52 | 226 | Abfallbeseitigung und Entsorgung |
| `euer.z53` | expense | 53 | 227 | Verpackung und Transport |
| `euer.z54` | expense | 54 | 224 | Werbekosten |
| `euer.z57` | derived | 57 | 185 | Abziehbare Vorsteuer |
| `euer.z58` | expense | 58 | 186 | An das Finanzamt gezahlte Umsatzsteuer |
| `euer.z60` | expense | 60 | 183 | Übrige unbeschränkt abziehbare Betriebsausgaben |
| `euer.z62.nd` | expense | 62 | 164 | Geschenke, nicht abziehbar |
| `euer.z62.d` | expense | 62 | 174 | Geschenke, abziehbar |
| `euer.z63.nd` | expense | 63 | 165 | Bewirtungsaufwendungen, nicht abziehbar |
| `euer.z63.d` | expense | 63 | 175 | Bewirtungsaufwendungen, abziehbar |
| `euer.z65.nd` | expense | 65 | 162 or 172 | Häusliches Arbeitszimmer, nicht abziehbar |
| `euer.z65.d` | expense | 65 | 162 or 172 | Häusliches Arbeitszimmer, abziehbar |
| `euer.z66` | expense | 66 | 163 | Tagespauschale häusliche Wohnung |

Open points of the set:

- Zeile 65 is in the set, but which of Kz 162 and 172 is the deductible one was not read.
  Issue #80 checks it before it maps the line.
- Zeilen 68 to 73 (Kfz-Kosten): the label of each single line was not read. Issue #76 adds the
  keys after reading the form. Car cost computation stays out of the milestone.
- Zeile 13 (Kz 119, nicht steuerbare Umsätze eines Kleinunternehmers): issue #80 decides
  whether it is derived or chosen.
- Zeile 10 also shows a printed 111 in the read copy. Issue #80 checks whether Kz 111 belongs
  to Zeile 10, Zeile 12 or both.

### Migration timestamp reservation

- Every issue owns one range of dbmate versions. A migration file outside its issue's range is
  a review finding.
- The ranges follow the dependency order, so a table only references tables of a lower range.
- A merged migration is never edited. A fix is a new file in the same range.
- dbmate applies a pending version that is lower than one already applied, unless run with
  `--strict`. Not verified against dbmate 2.38.
- Every new table also needs its grants in `docker-compose.yaml` (`grant-app-role`) and in
  `internal/storage/postgres/integration_test.go`. Both lists are shared: append, and on a
  conflict keep both lines.

| Issue | Range | Planned migrations |
| --- | --- | --- |
| #67 contract | none | ships no table |
| #68 seller profile | `20261121100000` to `20261121100099` | `seller_profiles`, `vat_regime_periods` |
| #69 customers, catalogue | `20261121110000` to `20261121110099` | `customers.document_language` |
| #70 documents | `20261121120000` to `20261121120099` | offer type, `original_number`, `source_offer_id`, `language`, `valid_until`, `service_date_to`, `payment_terms`, `notes` |
| #71 payments | `20261121130000` to `20261121130099` | `payments` with trigger |
| #72 Vue shell | none | Vue only |
| #73 Vue documents | none | Vue only |
| #74 PDF | `20261121140000` to `20261121140099` | reserved, none planned |
| #75 files on S3 | `20261121150000` to `20261121150099` | `files` with trigger, `seller_profiles.logo_file_id` |
| #76 expenses | `20261121160000` to `20261121160099` | `expenses`, `other_income`, both with trigger |
| #77 Vue expenses | none | Vue only |
| #78 assets | `20261121170000` to `20261121170099` | `assets` |
| #79 Vue assets | none | Vue only |
| #80 EÜR report | `20261121180000` to `20261121180099` | reserved, the mapping is embedded data |
| #81 import | `20261121190000` to `20261121190099` | `import_batches`, batch references on imported rows |
| #82 hardening | `20261121200000` to `20261121200099` | reserved for fixes |

### Conflict zones

- `internal/httpapi`: one `routes_<resource>.go` per resource with its `registerX`. `NewHandler`
  and `routes.go` are not edited again. `Repositories` already has every field.
- `cmd/solobill/repositories.go`: each issue sets only its own field.
- `internal/storage`: one domain file and one interface file per resource.
- `web/src/router/routes/<feature>.ts` and `web/src/i18n/locales/{de,en}/<feature>.json`: one
  per feature. The two index files are not edited again.
- `internal/storage/postgres/sqlcgen/models.go`: regenerated after a rebase, never hand merged.
- `AGENTS.md` layout: only issue #82 edits it.

### Unverified tax points

- The Anlage EÜR 2026 line numbers and Kennzahlen.
- Kz 162 and 172 order in Zeile 65, and Kz 111 on Zeile 10 or 12.
- The 10 day rule for regularly recurring payments, and how USt-Vorauszahlungen fall under it.
- The gross GWG and Sammelposten thresholds for a Kleinunternehmer.
- A one year useful life for computer hardware and software (BMF letter of 22 February 2022),
  and whether the acquisition year is pro rata.
- The pro rata VAT split of partial payments under the standard regime.
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
