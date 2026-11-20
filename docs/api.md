# solobill API

The HTTP contract of the year end milestone (issues #68 to #81).
The design behind it lives in `docs/concepts/solobill.md`.

## Rules for every endpoint

- Base path `/api/v1`. JSON in and out, UTF-8, unless an endpoint says otherwise.
- Every endpoint below needs a session cookie. Without one it answers `401 unauthenticated`.
- Every unsafe method (`POST`, `PUT`, `DELETE`) also needs the `X-CSRF-Token` header.
  `GET /api/v1/me` returns the token.
- A refused CSRF check answers `403 permission_denied`.
- Roles: "owner" means the owner role only. "any" means owner or member.
- Everything is scoped to the caller's tenant. A row of another tenant answers `404`, never `403`.
- Money is integer cents (`_cents`). Rates and percentages are permyriad (`1900` is 19.00 %).
- Quantities are thousandths (`2500` is 2.5).
- Dates are `YYYY-MM-DD`. Timestamps are RFC 3339 in UTC.
- Ids are UUIDs.
- A JSON body is limited to 1 MiB. Uploads have their own limits.
- Unknown JSON fields are refused with `400`.
- Lists are not paginated in this milestone. The volume is a few hundred rows per year.
- Until its issue merges, every endpoint answers `501 not_implemented` to a logged in caller.
  The stub table lives in `internal/httpapi/reserved_endpoints_test.go`.
- The file that registers each endpoint is named in each section. Only its issue edits it.

## Error shape

One shape for every error, written by govue's `api.WriteError`:

```json
{
  "error": {
    "code": "invalid_argument",
    "message": "the seller profile is incomplete",
    "details": {
      "fields": {
        "seller.tax_number": "a Steuernummer or USt-IdNr is required",
        "customer.address.city": "required"
      }
    },
    "request_id": "01JD..."
  }
}
```

- `details.fields` maps a JSON path to a reason. Only validation errors carry it.
- A 5xx never carries a cause. Its message is `internal error`, except `503` and `501`.

| Status | Code | When |
| --- | --- | --- |
| 400 | `invalid_argument` | Malformed JSON, unknown field, failed validation |
| 401 | `unauthenticated` | No session |
| 403 | `permission_denied` | Wrong role, or a refused CSRF check |
| 404 | `not_found` | No such row in the caller's tenant |
| 409 | `conflict` | The row's state forbids the change, or a duplicate |
| 413 | `request_too_large` | Body over the endpoint's limit |
| 415 | `unsupported_media_type` | Wrong content type, or an upload outside the allowlist |
| 429 | `too_many_requests` | Rate limit, with `Retry-After` |
| 500 | `internal` | A bug |
| 501 | `not_implemented` | Reserved endpoint, issue not merged yet |
| 503 | `unavailable` | Session store or object store down |

## Seller profile

Registered in `internal/httpapi/routes_profile.go`. Owned by issue #68.

The profile JSON:

```json
{
  "legal_name": "Example Trading",
  "street": "Main Street 1",
  "postal_code": "12345",
  "city": "Example City",
  "country_code": "DE",
  "tax_number": "12/345/67890",
  "vat_id": null,
  "kleinunternehmer_id": null,
  "default_vat_regime": "kleinunternehmer",
  "default_language": "de",
  "bank_name": "Example Bank",
  "iban": "DE00000000000000000000",
  "bic": "EXAMPLEXXX",
  "email": "office@example.test",
  "phone": null,
  "website": null,
  "footer_text": null,
  "number_prefixes": { "invoice": "RE-", "correction": "KR-", "offer": "AN-" },
  "number_padding": 3,
  "has_logo": false,
  "issue_ready": { "standard": false, "kleinunternehmer": true },
  "updated_at": "2026-11-20T10:00:00Z"
}
```

- `issue_ready` is computed by the seller validation function per regime. Read only.
- `has_logo` is read only. The logo has its own endpoint.

### `GET /api/v1/profile`

- Who: owner.
- Response `200`: the profile JSON.
- `404` when no profile was saved yet.

### `PUT /api/v1/profile`

- Who: owner.
- Request: the profile JSON without the read only fields. The whole profile is replaced.
- Response `200`: the saved profile.
- `400` with `details.fields` for a malformed value (IBAN checksum, country code, regime).
- Saving an incomplete profile is allowed. Completeness is checked at issue time.

## Seller logo

Registered in `internal/httpapi/routes_profile_logo.go`. Owned by issue #75.

### `GET /api/v1/profile/logo`

- Who: any.
- Response `200`: the image bytes, `Content-Type` `image/png` or `image/jpeg`.
- Headers: `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`.
- `404` when no logo is set.

### `PUT /api/v1/profile/logo`

- Who: owner.
- Request: `multipart/form-data` with one part `file`. At most 512 KiB.
- Accepted: PNG or JPEG, checked by magic bytes.
- Response `200`: `{"file_id": "...", "sha256": "...", "size_bytes": 12345}`.
- `413` over the limit. `415` for any other type. `404` when no profile exists yet.

## VAT regime periods

Registered in `internal/httpapi/routes_vat_regimes.go`. Owned by issue #68.

A period JSON:

```json
{
  "id": "...",
  "valid_from": "2026-01-01",
  "regime": "kleinunternehmer",
  "note": null,
  "created_at": "2026-11-20T10:00:00Z"
}
```

### `GET /api/v1/vat-regimes`

- Who: any.
- Response `200`: `{"periods": [period, ...]}`, ordered by `valid_from`.

### `POST /api/v1/vat-regimes`

- Who: owner.
- Request: `{"valid_from": "2026-10-14", "regime": "standard", "note": "Grenze überschritten"}`.
- Response `201`: the period.
- `409` when a period with the same `valid_from` exists.
- A period is never edited or deleted. A mistake is corrected by a later period.

## Customers

Registered in `internal/httpapi/routes_customers.go`. Owned by issue #69.

A customer JSON:

```json
{
  "id": "...",
  "name": "Customer Ltd",
  "email": "billing@customer.test",
  "vat_id": null,
  "street": "Side Street 2",
  "postal_code": "54321",
  "city": "Other City",
  "country_code": "DE",
  "document_language": "de",
  "deleted_at": null,
  "created_at": "2026-11-20T10:00:00Z",
  "updated_at": "2026-11-20T10:00:00Z"
}
```

### `GET /api/v1/customers`

- Who: any.
- Response `200`: `{"customers": [customer, ...]}`. Active customers only.

### `POST /api/v1/customers`

- Who: any.
- Request: the customer JSON without `id`, `deleted_at` and the timestamps. `name` is required.
- Response `201`: the customer.

### `GET /api/v1/customers/{id}`

- Who: any.
- Response `200`: the customer, also when soft deleted.

### `PUT /api/v1/customers/{id}`

- Who: any.
- Request: as for `POST`. Full replace.
- Response `200`: the customer. `404` when missing or soft deleted.

### `DELETE /api/v1/customers/{id}`

- Who: any.
- Soft delete. Response `204`. `404` when missing or already deleted.

## Catalogue items

Registered in `internal/httpapi/routes_catalogue.go`. Owned by issue #69.

An item JSON:

```json
{
  "id": "...",
  "name": "Consulting hour",
  "description": null,
  "sku": "CONS-H",
  "unit_price_cents": 9500,
  "unit": "hour",
  "deleted_at": null,
  "created_at": "2026-11-20T10:00:00Z",
  "updated_at": "2026-11-20T10:00:00Z"
}
```

### `GET /api/v1/catalogue-items`

- Who: any.
- Response `200`: `{"items": [item, ...]}`. Active items only.

### `POST /api/v1/catalogue-items`

- Who: any.
- Request: the item JSON without `id`, `deleted_at` and the timestamps.
- Response `201`: the item. `409` for a duplicate `sku`.

### `GET /api/v1/catalogue-items/{id}`

- Who: any.
- Response `200`: the item, also when soft deleted.

### `PUT /api/v1/catalogue-items/{id}`

- Who: any.
- Request: as for `POST`. Full replace.
- Response `200`. `404` when missing or soft deleted. `409` for a duplicate `sku`.

### `DELETE /api/v1/catalogue-items/{id}`

- Who: any.
- Soft delete. Response `204`. `404` when missing or already deleted.

## Documents

Registered in `internal/httpapi/routes_documents.go`. Owned by issue #70.

A document JSON:

```json
{
  "id": "...",
  "type": "invoice",
  "state": "draft",
  "customer_id": "...",
  "corrects_document_id": null,
  "source_offer_id": null,
  "vat_regime": "kleinunternehmer",
  "language": "de",
  "service_date": "2026-11-01",
  "service_date_to": "2026-11-30",
  "due_date": "2026-12-15",
  "valid_until": null,
  "discount_percent_permyriad": null,
  "discount_amount_cents": null,
  "payment_terms": "Zahlbar innerhalb von 14 Tagen.",
  "notes": null,
  "number": null,
  "number_value": null,
  "number_year": null,
  "original_number": null,
  "imported": false,
  "issued_at": null,
  "frozen_payload_sha256": null,
  "lines": [
    {
      "position": 1,
      "catalogue_item_id": null,
      "description": "Consulting",
      "quantity_thousandths": 2500,
      "unit": "hour",
      "unit_price_cents": 9500,
      "vat_rate_permyriad": 0,
      "line_discount_percent_permyriad": null,
      "line_discount_amount_cents": null
    }
  ],
  "totals": {
    "net_total_cents": 23750,
    "document_discount_cents": 0,
    "net_after_document_discount_cents": 23750,
    "vat": [{ "rate_permyriad": 0, "net_cents": 23750, "vat_cents": 0 }],
    "vat_total_cents": 0,
    "grand_total_cents": 23750
  }
}
```

- `number` is the display number (`"RE-2026-018"`), or the original number of an imported row.
- `totals` is always computed by `internal/money` on the server. The Vue app never computes money.
- `type` is `invoice`, `correction` or `offer`. `state` is `draft` or `issued`.
- An issued document is immutable. Every write to it answers `409`.

### `GET /api/v1/documents`

- Who: any.
- Query: `type`, `state`, `year` (the number year, or the creation year of a draft). All optional.
- Response `200`: `{"documents": [document without lines, ...]}`, newest first.

### `POST /api/v1/documents`

- Who: any.
- Request: `type`, `customer_id`, `vat_regime`, `language`, the dates, the discount, `lines`.
- `vat_regime` defaults to the regime of the period that applies today.
- `language` defaults to the customer's `document_language`.
- A correction needs `corrects_document_id` naming an issued invoice.
- Response `201`: the draft.

### `POST /api/v1/documents/preview`

- Who: any.
- Request: the same body as `POST /api/v1/documents`. Nothing is stored.
- Response `200`: `{"totals": {...}, "lines": [{"net_after_line_discount_cents": 0,`
  `"document_discount_share_cents": 0, "net_after_all_discounts_cents": 0}, ...]}`.
- `400` with `details.fields` when `internal/money` refuses the input, for example a VAT rate
  above 0 under the Kleinunternehmer regime.

### `GET /api/v1/documents/{id}`

- Who: any.
- Response `200`: the document with lines and totals.

### `PUT /api/v1/documents/{id}`

- Who: any.
- Request: as for `POST`. Replaces the draft header and all lines.
- Response `200`. `409` when issued.

### `DELETE /api/v1/documents/{id}`

- Who: any.
- Response `204`. `409` when issued.

### `POST /api/v1/documents/{id}/issue`

- Who: owner.
- Request: empty body.
- In one transaction: validate the seller profile, build `FrozenPayloadV1`, validate it, allocate
  the number, freeze, hash.
- Response `200`: the issued document.
- `400` with `details.fields` when the seller validation or `docpayload.Validate` reports a gap.
  Field paths of the seller use the profile JSON names under `seller.`.
- `409` when already issued. `400` when no VAT regime period covers the issue date.

### `POST /api/v1/documents/{id}/correction`

- Who: owner.
- Request: empty body. `{id}` is an issued invoice.
- Response `201`: a new correction draft with the original's lines and `corrects_document_id`.
- `409` when `{id}` is not an issued invoice.

### `POST /api/v1/offers/{id}/to-invoice`

- Who: any.
- Request: empty body. `{id}` is an offer, draft or issued.
- Response `201`: a new invoice draft with the offer's lines and `source_offer_id` set.
- `409` when `{id}` is not an offer.

## Payments

Registered in `internal/httpapi/routes_payments.go`. Owned by issue #71.

A payment JSON:

```json
{
  "id": "...",
  "document_id": "...",
  "received_on": "2026-12-03",
  "amount_cents": 23750,
  "method": "bank_transfer",
  "note": null,
  "reverses_payment_id": null,
  "imported": false,
  "created_at": "2026-12-03T09:00:00Z"
}
```

### `GET /api/v1/documents/{id}/payments`

- Who: any.
- Response `200`: `{"payments": [payment, ...], "paid_cents": 23750, "open_cents": 0}`.
- `open_cents` is derived, never stored. It is the grand total minus the sum of payments.

### `POST /api/v1/documents/{id}/payments`

- Who: owner.
- Request: `received_on`, `amount_cents`, `method`, `note`, and `reverses_payment_id` for a
  reversal.
- A reversal has a negative amount equal to the reversed payment.
- Response `201`: the payment.
- `409` when the document is a draft or an offer, or when a reversal names a payment of another
  document or one already reversed.
- An overpayment is accepted and shows as a negative `open_cents`.
- Payments are never edited or deleted.

## PDF

Registered in `internal/httpapi/routes_pdf.go`. Owned by issue #74.

### `GET /api/v1/documents/{id}/pdf`

- Who: any.
- Response `200`: `application/pdf`, `Content-Disposition: attachment; filename="<number>.pdf"`.
- An issued document renders from its frozen payload and the template version it names.
- A draft renders with an "Entwurf" or "Draft" watermark and without a number.
- `400` with `details.fields` when a draft lacks data the template needs.
- `503` when the renderer is not available.

## Files

Registered in `internal/httpapi/routes_files.go`. Owned by issue #75.

A file JSON:

```json
{
  "id": "...",
  "purpose": "receipt",
  "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "size_bytes": 182734,
  "content_type": "application/pdf",
  "original_filename": "rechnung-november.pdf",
  "uploaded_at": "2026-11-20T10:00:00Z",
  "object_version_id": null,
  "retain_until": null
}
```

### `POST /api/v1/files`

- Who: any.
- Request: `multipart/form-data` with the parts `purpose` (`receipt` or `import_original`) and
  `file`. At most 10 MiB.
- Accepted by magic bytes: PDF, JPEG, PNG, XML. The client's content type is ignored.
- Response `201`: `{"file": file, "duplicates": [file, ...]}`.
- `duplicates` lists earlier files with the same sha256. It is a warning, not a refusal.
- `413` over the limit. `415` outside the allowlist. `503` when the object store is down.

### `GET /api/v1/files/{id}`

- Who: any.
- Response `200`: the file JSON.

### `GET /api/v1/files/{id}/content`

- Who: any.
- Response `200`: the bytes with the sniffed `Content-Type`.
- Headers: `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`,
  `Content-Security-Policy: sandbox`.
- The server recomputes the sha256 while streaming. On a mismatch it aborts the response and
  logs an integrity error. A client sees a truncated download, never wrong bytes as complete.
- `503` when the object store is down.

## Expenses

Registered in `internal/httpapi/routes_expenses.go`. Owned by issue #76.

An expense JSON:

```json
{
  "id": "...",
  "state": "draft",
  "paid_on": "2026-11-18",
  "receipt_date": "2026-11-17",
  "supplier": "Office Supplies Inc",
  "description": "Paper and toner",
  "gross_cents": 5950,
  "net_cents": 5950,
  "vat_rate_permyriad": 0,
  "vat_cents": 0,
  "category": "euer.z51",
  "file_id": "...",
  "is_asset": false,
  "storno_of_id": null,
  "imported": false,
  "booked_at": null,
  "created_at": "2026-11-20T10:00:00Z",
  "updated_at": "2026-11-20T10:00:00Z"
}
```

- `category` is a key of kind `expense` from `GET /api/v1/categories`.
- Under the Kleinunternehmer regime on `paid_on`, `net_cents` equals `gross_cents` and
  `vat_cents` is 0.
- `is_asset` sends the expense to the asset register. The EÜR then counts only depreciation.

### `GET /api/v1/expenses`

- Who: any.
- Query: `year` (of `paid_on`), `state`, `category`. All optional.
- Response `200`: `{"expenses": [expense, ...], "total_gross_cents": 0, "total_net_cents": 0}`.

### `POST /api/v1/expenses`

- Who: any.
- Request: the expense JSON without `id`, `state`, `storno_of_id`, `imported`, `booked_at` and
  the timestamps.
- Response `201`: the draft. `400` with `details.fields` for an inconsistent amount or category.

### `GET /api/v1/expenses/{id}`

- Who: any.
- Response `200`: the expense.

### `PUT /api/v1/expenses/{id}`

- Who: any.
- Request: as for `POST`. Response `200`. `409` when booked.

### `DELETE /api/v1/expenses/{id}`

- Who: any.
- Response `204`. `409` when booked.

### `POST /api/v1/expenses/{id}/book`

- Who: owner.
- Request: empty body. Response `200`: the booked expense.
- `409` when booked already. `400` with `details.fields` when `paid_on` or the category is
  missing.

### `POST /api/v1/expenses/{id}/storno`

- Who: owner.
- Request: `{"paid_on": "2026-11-25"}`, the date the money flowed back.
- Response `201`: the booked storno row with negated amounts and `storno_of_id`.
- `409` when the original is a draft, a storno or already reversed.

## Other income

Registered in `internal/httpapi/routes_other_income.go`. Owned by issue #76.

The JSON matches an expense, with `received_on` instead of `paid_on` and without `supplier`,
`receipt_date` and `is_asset`. `category` is a key of kind `income`.

### `GET /api/v1/other-income`

- Who: any.
- Query: `year` (of `received_on`), `state`, `category`.
- Response `200`: `{"other_income": [entry, ...], "total_gross_cents": 0}`.

### `POST /api/v1/other-income`

- Who: any. Response `201`: the draft.

### `GET /api/v1/other-income/{id}`

- Who: any. Response `200`.

### `PUT /api/v1/other-income/{id}`

- Who: any. Response `200`. `409` when booked.

### `DELETE /api/v1/other-income/{id}`

- Who: any. Response `204`. `409` when booked.

### `POST /api/v1/other-income/{id}/book`

- Who: owner. Response `200`. `409` when booked already.

### `POST /api/v1/other-income/{id}/storno`

- Who: owner.
- Request: `{"received_on": "2026-11-25"}`. Response `201`: the storno row.

## Categories

Registered in `internal/httpapi/routes_categories.go`. Owned by issue #76.

### `GET /api/v1/categories`

- Who: any.
- Response `200`:

```json
{
  "categories": [
    { "key": "euer.z27", "kind": "expense", "label_de": "Waren, Rohstoffe und Hilfsstoffe",
      "label_en": "Goods and materials" }
  ]
}
```

- The list is `category.All()` in form order. Keys never change.

## Assets

Registered in `internal/httpapi/routes_assets.go`. Owned by issue #78.

An asset JSON:

```json
{
  "id": "...",
  "name": "Laptop",
  "acquired_on": "2026-03-10",
  "cost_basis_cents": 149900,
  "method": "linear",
  "group": "office_equipment",
  "useful_life_months": 36,
  "expense_id": "...",
  "opening_book_value_cents": null,
  "opening_book_value_year": null,
  "disposed_on": null,
  "disposal_proceeds_cents": null,
  "imported": false,
  "created_at": "2026-11-20T10:00:00Z",
  "updated_at": "2026-11-20T10:00:00Z"
}
```

- `method` is `linear`, `gwg`, `sammelposten` or `immediate`. No degressive method.
- `group` is `intangible`, `office_equipment` or `other_movable`.
- `useful_life_months` is required for `linear` and refused otherwise.

### `GET /api/v1/assets`

- Who: any.
- Response `200`: `{"assets": [asset, ...]}`.

### `POST /api/v1/assets`

- Who: any.
- Request: the asset JSON without `id`, `disposed_on`, `disposal_proceeds_cents`, `imported` and
  the timestamps. `expense_id` names a booked expense with `is_asset` set.
- Response `201`. `400` with `details.fields` when the method does not fit the cost
  (for example `gwg` above 800 EUR).

### `GET /api/v1/assets/{id}`

- Who: any. Response `200`.

### `PUT /api/v1/assets/{id}`

- Who: any.
- Request: as for `POST`. Response `200`.
- `409` once a tax year with its depreciation is final.

### `POST /api/v1/assets/{id}/disposal`

- Who: owner.
- Request: `{"disposed_on": "2026-12-01", "proceeds_cents": 20000}`. `proceeds_cents` may be
  null for a withdrawal or a scrapping.
- Response `200`: the asset. `409` when already disposed.

### `GET /api/v1/assets/{id}/schedule`

- Who: any.
- Response `200`:

```json
{
  "asset_id": "...",
  "method": "linear",
  "years": [
    { "year": 2026, "opening_book_value_cents": 149900, "depreciation_cents": 41639,
      "closing_book_value_cents": 108261, "euer_key": "euer.z33" }
  ]
}
```

## EÜR report

Registered in `internal/httpapi/routes_reports.go`. Owned by issue #80.

### `GET /api/v1/reports/euer/{year}`

- Who: any.
- Response `200`:

```json
{
  "year": 2026,
  "mapping": { "tax_year": 2026, "status": "unverified", "source": "copy of 2025" },
  "vat_regimes": [{ "valid_from": "2026-01-01", "regime": "kleinunternehmer" }],
  "lines": [
    {
      "key": "euer.z12",
      "zeile": 12,
      "kennzahl": "111",
      "label_de": "Betriebseinnahmen als Kleinunternehmer",
      "amount_cents": 2375000,
      "sources": [{ "kind": "payment", "id": "...", "amount_cents": 23750 }]
    }
  ],
  "boundary_entries": [{ "kind": "expense", "id": "...", "date": "2027-01-08",
    "amount_cents": 0, "note": "regularly recurring, assign the year" }],
  "warnings": ["expense ... has no receipt"],
  "disclaimer": "Computed from your entries. No tax advice."
}
```

- `sources[].kind` is `payment`, `expense`, `other_income` or `asset`.
- `boundary_entries` lists entries from 22 December to 10 January for the owner to assign.
- `mapping.status` is `verified` or `unverified`. The view shows a banner when unverified.
- `404` for a year whose documents are all imported archive years (2023 to 2025).
- `400` for a year without any VAT regime period.

### `GET /api/v1/reports/euer/{year}/csv`

- Who: any.
- Response `200`: `text/csv; charset=utf-8`, semicolon separated, decimal comma,
  `Content-Disposition: attachment`.
- Columns: `key;zeile;kennzahl;label;amount;source_kind;source_id;source_amount`.
- One row per source, so every figure traces to its rows.

## Historic import

Registered in `internal/httpapi/routes_imports.go`. Owned by issue #81.

The import format is decided later and specified in `docs/import.md` by issue #81.
This section fixes only the flow and the batch shape.

A batch JSON:

```json
{
  "id": "...",
  "state": "dry_run",
  "created_at": "2026-12-14T10:00:00Z",
  "committed_at": null,
  "summary": { "documents": 18, "payments": 15, "expenses": 120, "assets": 4 },
  "rows": [{ "entity": "document", "line": 3, "status": "ok", "original_number": "RE-2026-003" }],
  "errors": [{ "entity": "expense", "line": 41, "field": "paid_on", "reason": "not a date" }],
  "collisions": [{ "entity": "document", "line": 7, "original_number": "RE-2026-007",
    "reason": "number already in solobill" }]
}
```

### `POST /api/v1/imports`

- Who: owner.
- Request: `multipart/form-data`, the parts named by `docs/import.md`.
- Runs the dry run. Nothing is written to the ledger. The batch and its findings are kept.
- Response `201`: the batch in state `dry_run`.

### `GET /api/v1/imports/{id}`

- Who: owner. Response `200`: the batch.

### `POST /api/v1/imports/{id}/commit`

- Who: owner.
- Request: empty body.
- Writes every row in one transaction, raises the 2026 counters, and moves the batch to
  `committed`.
- Response `200`: the batch.
- `409` when the batch has errors or collisions, is committed already, or the ledger changed
  since the dry run in a way that creates a new collision.
