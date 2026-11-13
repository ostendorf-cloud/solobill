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

## Open questions

- Retention period to implement: the 2025 change to eight years for Buchungsbelege needs
  verification against the current law before it becomes a default.
- Whether a Verfahrensdokumentation is written as part of this repository, and by whom.
- Whether the import of 2023 to 2025 is typed in by hand, at roughly ten documents a year, or
  earns a CSV importer.
- Whether imported years need their figures reproduced as reports, or only archived as documents.
- Which bank interface a later import would use: FinTS directly, or an aggregator.
- Whether offers created by Eventory's webshop arrive through the API or through a shared table.
