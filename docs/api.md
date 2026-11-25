# solobill API

The HTTP contract of the year end milestone (issues #68 to #82).
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
- Until its issue merges, every endpoint answers `501 not_implemented` to a logged in caller and
  `401` to anyone else, whatever the method. The stubs are listed per routes file in
  `internal/httpapi/reserved_<resource>_test.go`.
- The file that registers each endpoint is named in each section. Only its issue edits it.

## Error shape

One shape for every error, written by govue's `api.WriteError`:

```json
{
  "error": {
    "code": "invalid_argument",
    "message": "the document is incomplete",
    "details": {
      "seller.tax_number": "a Steuernummer or USt-IdNr is required",
      "customer.address.city": "required",
      "lines[1].description": "required"
    },
    "request_id": "01JD..."
  }
}
```

- `details` maps a field path directly to a reason. There is no wrapper key. This is the place
  govue's JSON decoder already puts a wrongly typed field (`{"amount_cents": "wrong type"}`).
- Field path scheme: JSON names joined by `.`, an array index as `[i]`. The decoder's own
  paths carry no index.
- `details` is present on validation errors (`400 invalid_argument`) and on the two domain
  conflicts that name a field: `409 vat_regime_mismatch` (`{"vat_regime": "<period regime>"}`)
  and `409 correction_base_changed` (`{"correction_base_seq": "<effective sequence>"}`). Every
  other error has none.
- A 5xx never carries a cause. Its message is `internal error`, except `503` and `501`.

| Status | Code | When |
| --- | --- | --- |
| 400 | `invalid_argument` | Malformed JSON, unknown field, failed validation |
| 400 | `vat_regime_missing` | No VAT regime period covers the date |
| 401 | `unauthenticated` | No session |
| 403 | `permission_denied` | Wrong role, or a refused CSRF check |
| 404 | `not_found` | No such row in the caller's tenant |
| 409 | `conflict` | The row's state forbids the change, or a duplicate (a display number included) |
| 409 | `vat_regime_mismatch` | The draft's regime differs from the period on the issue date |
| 409 | `correction_base_changed` | The chain's effective document changed since the correction draft was created |
| 413 | `request_too_large` | Body over the endpoint's limit |
| 415 | `unsupported_media_type` | Wrong content type, or an upload outside the allowlist |
| 429 | `too_many_requests` | Rate limit, with `Retry-After` |
| 500 | `internal` | A bug |
| 501 | `not_implemented` | Reserved endpoint, issue not merged yet |
| 503 | `unavailable` | Session store down |
| 503 | `object_store_unavailable` | File routes only: bucket unreachable, or no versioning while it is required |
| 503 | `renderer_unavailable` | No PDF renderer configured or able to run: issue, correction and the PDF download |

## Seller profile

Registered in `internal/httpapi/routes_profile.go`. Owned by issue #68.

The profile JSON:

```json
{
  "legal_name": "Example Trading",
  "street": "Main Street 1",
  "address_line2": null,
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
  "number_formats": { "invoice": "RE-{year}{seq:4}", "offer": "AN-{year}{seq:4}" },
  "has_logo": false,
  "issue_ready": { "standard": false, "kleinunternehmer": true },
  "updated_at": "2026-11-20T10:00:00Z"
}
```

- `issue_ready` is computed by the seller validation function per regime. Read only.
- `has_logo` is read only. #68's handler computes it from `SellerProfileLogoRepository.Logo`
  when #75 has wired that repository, and reports `false` before. The logo has its own endpoint.
- `number_formats` holds one template for `invoice` and one for `offer`, parsed by
  `internal/numbering`: `{year}`, `{yy}`, `{seq}`, `{seq:N}`, `{{` and `}}` for a literal brace,
  every other character from `A-Z a-z 0-9 - _ . /`, a literal prefix first that starts with a
  letter, a digit or `_`, no `..` or `//`, N a single digit 1 to 9, at most 36 rendered
  characters. `RE-{year}{seq:4}` renders number 9 of 2026 as `RE-20260009`.
- A missing type uses its default: `RE-{year}{seq:4}` for an invoice, `AN-{year}{seq:4}` for an
  offer. A correction has no template.
- A changed template applies to the next issue only. Issued documents keep their stored
  `display_number`.
- Once a type has an issued document, only the prefix of its template may change, to one that
  does not overlap the old prefix (`numbering.ValidateChange`). Any other change is `400` with
  `details` on `number_formats.<type>`.

### `GET /api/v1/profile`

- Who: owner.
- Response `200`: the profile JSON.
- `404` when no profile was saved yet.

### `PUT /api/v1/profile`

- Who: owner.
- Request: the profile JSON without the read only fields. The whole profile is replaced.
- Response `200`: the saved profile.
- `400` with `details` for a malformed value: IBAN checksum, country code, regime, or a number
  template `numbering.Parse` refuses (unknown token, character outside the set, missing or
  repeated sequence or year token, too long). The path is `number_formats.invoice` or
  `number_formats.offer`.
- `400` with `details` on `number_formats` when the two prefixes overlap, so an invoice and an
  offer could render the same number (`numbering.ValidateSet`).
- Saving an incomplete profile is allowed. Completeness is checked at issue time.

## Seller logo

Registered in `internal/httpapi/routes_profile_logo.go`. Owned by issue #75, through
`SellerProfileLogoRepository` (`Logo`, `SetLogo`). `PUT /api/v1/profile` never changes the logo.

### `GET /api/v1/profile/logo`

- Who: any.
- Response `200`: the image bytes, `Content-Type` `image/png` or `image/jpeg`.
- Headers: `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`.
- `404` when no logo is set.

### `PUT /api/v1/profile/logo`

- Who: owner.
- Request: `multipart/form-data` with one part `file`. The whole request body is limited to
  512 KiB (524,288 bytes) by `http.MaxBytesReader`, multipart framing included.
- Accepted: PNG or JPEG, checked by magic bytes, that decodes completely and has at most 4096
  pixels per side and 4,194,304 pixels in total (`internal/logo`). The PDF render checks the
  stored logo the same way again.
- Response `200`: `{"file_id": "...", "sha256": "...", "size_bytes": 12345}`.
- `413` over the limit. `415` for any other type. `404` when no profile exists yet.
- `400` with `details.file` for an image that does not decode or exceeds the pixel budget.
- `503 object_store_unavailable` for both logo routes under the condition of the file routes.

## VAT regime periods

Registered in `internal/httpapi/routes_vat_regimes.go`. Owned by issue #68.

A period JSON:

```json
{
  "id": "...",
  "valid_from": "2026-01-01",
  "regime": "kleinunternehmer",
  "note": null,
  "supersedes_id": null,
  "superseded": false,
  "created_at": "2026-11-20T10:00:00Z"
}
```

- This table is the source of the regime. The profile's `default_vat_regime` only prefills a
  form when no period exists.
- `superseded` is derived: true when a later row names this one in `supersedes_id`.

### `GET /api/v1/vat-regimes`

- Who: any.
- Response `200`: `{"periods": [period, ...]}`, superseded rows included, ordered by
  `valid_from`, then `created_at`.

### `POST /api/v1/vat-regimes`

- Who: owner.
- Request: `{"valid_from": "2026-10-14", "regime": "standard", "note": "Grenze überschritten",`
  `"supersedes_id": null}`.
- A wrong row is corrected by a new row with `supersedes_id` naming it. The new row carries its
  own `valid_from`.
- Response `201`: the period.
- `409` when an effective period with the same `valid_from` exists and is not the superseded
  one, or when the named row is superseded already.
- A period is never edited or deleted. Issued documents keep their frozen regime.

## Customers

Registered in `internal/httpapi/routes_customers.go`. Owned by issue #69.

A customer JSON:

```json
{
  "id": "...",
  "name": "Customer Ltd",
  "number": "K-0042",
  "email": "billing@customer.test",
  "vat_id": null,
  "street": "Side Street 2",
  "address_line2": null,
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
  "customer_reference": "PO-7",
  "number": null,
  "number_value": null,
  "number_year": null,
  "correction_seq": null,
  "correction_base_id": null,
  "correction_base_seq": null,
  "original_number": null,
  "imported": false,
  "chain_status": null,
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

- `number` is the stored `display_number`, written once at issue: the profile's template
  rendered (`"RE-20260009"`), the original's number plus `-1`, `-2` for a correction
  (`"RE-20260009-1"`), or the original number of an imported row. `null` for a draft. List and
  read always return this stored value, never a fresh rendering. It is unique per tenant.
- `correction_base_id` and `correction_base_seq` are set on a correction draft: the effective
  document it copied its lines from.
- `totals` is always computed by `internal/money` on the server. The Vue app never computes money.
- `type` is `invoice`, `correction` or `offer`. `state` is `draft` or `issued`, the only stored
  states.
- `chain_status` is derived for an issued invoice or correction: `effective` for the newest
  issued document of its chain, `superseded` for every older one. `null` for a draft and an
  offer. It is never stored.
- An issued document is immutable. Every write to it answers `409`.

### `GET /api/v1/documents`

- Who: any.
- Query: `type`, `state`, `year` (the number year, or the creation year of a draft). All optional.
- Response `200`: `{"documents": [document without lines, ...]}`, newest first.

### `POST /api/v1/documents`

- Who: any.
- Request: `type` (`invoice` or `offer`), `customer_id`, `vat_regime`, `language`, the dates,
  the discount, `payment_terms`, `notes`, `customer_reference`, `lines`.
- `vat_regime`, when omitted, is the regime of the effective period that applies today. Only
  without any period it falls back to the profile's `default_vat_regime`.
- `language` defaults to the customer's `document_language`.
- A correction draft is created by `POST /api/v1/documents/{id}/correction`, never here.
- Response `201`: the draft.

### `POST /api/v1/documents/preview`

- Who: any.
- Request: the same body as `POST /api/v1/documents`. Nothing is stored.
- Response `200`: `{"totals": {...}, "lines": [{"net_after_line_discount_cents": 0,`
  `"document_discount_share_cents": 0, "net_after_all_discounts_cents": 0}, ...]}`.
- `400` with `details` when `internal/money` refuses the input, for example a VAT rate above 0
  under the Kleinunternehmer regime.

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
- In one transaction: evaluate the VAT regime period on the issue date, validate the seller
  profile, allocate the number (an invoice or an offer from `document_numbers`, a correction
  as the next `correction_seq` of its chain under a lock on the root row), render the display
  number and store it as `display_number`, build `FrozenPayloadV1`, validate it, freeze, hash.
- A correction draft is first checked against its base (`internal/docchain.CheckBase`).
- Response `200`: the issued document.
- `400 invalid_argument` with `details` when the seller validation or `docpayload.Validate`
  reports a problem. Both use the same path scheme; the seller's paths start with `seller.`.
- `400 vat_regime_missing` when no effective period covers the issue date.
- `409 vat_regime_mismatch` when the draft's `vat_regime` differs from that period. `details`
  is `{"vat_regime": "<regime of the period>"}`.
- `409 correction_base_changed` when the draft is a correction and the chain's effective
  document is no longer its base, for example because another correction was issued meanwhile.
  `details` is `{"correction_base_seq": "<sequence now effective>"}`. The draft stays; the owner
  discards it and starts a new correction.
- `409 conflict` when already issued, or when the rendered `display_number` already exists in
  the tenant.

### `POST /api/v1/documents/{id}/correction`

- Who: owner.
- Request: empty body. `{id}` is any issued invoice or correction of a chain.
- Response `201`: a new correction draft. Its lines are a copy of the chain's effective
  document, to be edited into the full corrected lines. `corrects_document_id` names the chain
  root, the original invoice. `correction_base_id` and `correction_base_seq` record the
  effective document copied.
- The draft gets its number and `correction_seq` only at issue.
- `409` when `{id}` is a draft or an offer.

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

- `document_id` is always the chain root, the original invoice.
- A refund is a negative amount without `reverses_payment_id`. A reversal of a recording
  mistake names the reversed row in `reverses_payment_id`.

### `GET /api/v1/documents/{id}/payments`

- Who: any.
- `{id}` is any issued document of a chain. The server resolves it to the chain root.
- Response `200`: `{"payments": [payment, ...], "paid_cents": 23750, "effective_document_id":`
  `"...", "open_cents": 0}`.
- `open_cents` is derived, never stored: the effective document's grand total minus the sum of
  the chain's payments. Negative means a refund is due.

### `POST /api/v1/documents/{id}/payments`

- Who: owner.
- `{id}` is any issued document of a chain. The payment is stored against the chain root.
- Request: `received_on`, `amount_cents`, `method`, `note`, and `reverses_payment_id` for a
  reversal.
- A reversal has a negative amount equal to the reversed payment.
- A refund has a negative amount, no `reverses_payment_id`, and `received_on` set to the day
  the money left.
- Response `201`: the payment.
- `409` when the document is a draft or an offer, or when a reversal names a payment of another
  chain or one already reversed.
- An overpayment is accepted and shows as a negative `open_cents`.
- Payments are never edited or deleted. The EÜR counts them, never document totals.
- Under the standard regime the VAT share of a payment uses the ratio of the chain's effective
  document on `received_on`. A later correction does not recompute earlier payments. The legal
  side is not verified (concept, "Payments").

## PDF

Registered in `internal/httpapi/routes_pdf.go`. Owned by issue #74.

### `GET /api/v1/documents/{id}/pdf`

- Who: any.
- Response `200`: `application/pdf`, `Content-Disposition: attachment;
  filename="<numbering.FileName(display_number)>"`: the stored display number with every
  character outside the number set, and `/`, replaced by `_`, plus `.pdf`. A draft is named
  `draft-<id>.pdf`. The PDF is in the document's language. The headers are those of a stored
  file download: `X-Content-Type-Options: nosniff`, the sandbox `Content-Security-Policy` and
  `Cache-Control: private, no-store`.
- An issued document: the first download renders once from the frozen payload, the template
  version it names and the logo file its SHA-256 names, and stores the PDF as a file of
  purpose `document_pdf`. Every later download returns the stored bytes, verified against
  their SHA-256; the logo is never read again.
- Storage rule: the stored file's id is derived from the document id (UUID version 5), so the
  issued document needs no link column and only one first download can store it. A second
  first download that loses that race serves the stored file. `GET /api/v1/files/{id}` reads
  its metadata like any other file.
- The logo is found by the SHA-256 frozen in the payload among the tenant's files of purpose
  `logo`, never by a name or the current profile, and its bytes are hashed again after reading.
- A draft renders live with an "Entwurf" or "Draft" watermark and without a number, from the
  current profile, customer and logo. It is never stored.
- This is a file route: `503 object_store_unavailable` while the bucket is gated, for the
  first and every later download and for a draft. Issuing a document never depends on the
  bucket.
- `400` with `details` when a draft lacks data the template needs.
- `404` for a document of another tenant, like a missing one.
- `409 conflict` when no logo file has the SHA-256 frozen in the payload, its bytes no longer
  hash to it, or it does not decode within the logo's pixel budget.
- `409 conflict` for an imported document: its original PDF is the document, downloaded
  through the files routes.
- `503 renderer_unavailable` when no renderer is configured (`PDF_TYPST_BINARY` unset) or it
  cannot run. Without a renderer, issuing and creating a correction answer the same code.

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
  `file`.
- Limit: the whole request body is at most 10 MiB plus 64 KiB (10,551,296 bytes), enforced by
  `http.MaxBytesReader`. The `file` part alone is at most 10 MiB (10,485,760 bytes), counted
  while streaming. The 64 KiB cover the multipart framing and the `purpose` part.
- Accepted by magic bytes: PDF, JPEG, PNG, XML. The client's content type is ignored.
- A file row is written once. The application role has `SELECT` and `INSERT` on `files` only,
  so no endpoint can change or delete it.
- Originals of a historic import are uploaded here first, with purpose `import_original`. The
  import rows then reference their file ids.
- Response `201`: `{"file": file, "duplicates": [file, ...]}`.
- `duplicates` lists earlier files with the same sha256. It is a warning, not a refusal.
- `413` over the limit. `415` outside the allowlist.
- `503 object_store_unavailable` while the bucket is unreachable or reports no versioning with
  `S3_REQUIRE_VERSIONING` on. Nothing is stored then.

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
- `503 object_store_unavailable` under the same condition as for the upload.

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
- Response `201`: the draft. `400` with `details` for an inconsistent amount or category.

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
- `409` when booked already. `400` with `details` when `paid_on` or the category is
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

- Who: owner.
- Request: the asset JSON without `id`, `disposed_on`, `disposal_proceeds_cents`, `imported` and
  the timestamps. `expense_id` names a booked expense with `is_asset` set.
- Response `201`. `400` with `details` when the method does not fit the cost
  (for example `gwg` above 800 EUR).

### `GET /api/v1/assets/{id}`

- Who: any. Response `200`.

### `PUT /api/v1/assets/{id}`

- Who: owner.
- Request: as for `POST`. Response `200`.
- Allowed only while the asset's first schedule year is open: the calendar year of
  `acquired_on`, or `opening_book_value_year` for an imported asset, has not ended.
- `409` afterwards, because a depreciation figure of a past year may already be in a filed EÜR.
  A later change is a disposal.

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
    "amount_cents": 0, "assigned_tax_year": null }],
  "warnings": [
    { "code": "missing_receipt", "kind": "expense", "id": "..." },
    { "code": "asset_expense_without_asset", "kind": "expense", "id": "..." }
  ],
  "disclaimer": "Computed from your entries. No tax advice."
}
```

- `sources[].kind` is `payment`, `expense`, `other_income` or `asset`.
- `boundary_entries` lists entries from 22 December to 10 January. `assigned_tax_year` is the
  effective assignment, or `null` when the entry counts in the year of its date.
- `warnings[].code` is one of `missing_receipt`, `asset_expense_without_asset` (a booked
  expense with `is_asset` set and no asset row), `unassigned_boundary_entry`.
- `mapping.status` is `verified` or `unverified`. The view shows a banner when unverified.
- `404` for a year whose documents are all imported archive years (2023 to 2025).
- `400 vat_regime_missing` for a year without any VAT regime period.

### `GET /api/v1/reports/euer/{year}/csv`

- Who: any.
- Response `200`: `text/csv; charset=utf-8`, semicolon separated, decimal comma,
  `Content-Disposition: attachment`.
- Columns: `key;zeile;kennzahl;label;amount;source_kind;source_id;source_amount`.
- One row per source, so every figure traces to its rows.

### `GET /api/v1/euer-assignments`

- Who: any.
- Query: `tax_year`, optional.
- Response `200`: `{"assignments": [assignment, ...]}`, effective rows only.
- An assignment JSON: `{"id": "...", "entry_kind": "expense", "entry_id": "...",`
  `"tax_year": 2026, "note": "Miete Dezember", "supersedes_id": null, "created_at": "..."}`.

### `POST /api/v1/euer-assignments`

- Who: owner.
- Request: `{"entry_kind": "expense", "entry_id": "...", "tax_year": 2026, "note": "...",`
  `"supersedes_id": null}`. `entry_kind` is `payment`, `expense` or `other_income`.
- Append-only. A changed decision is a new row with `supersedes_id` naming the old one.
- Response `201`: the assignment.
- `400` when the entry's date lies outside 22 December to 10 January, or `tax_year` is neither
  the year of the date nor the neighbouring year it belongs to.
- `409` when the entry already has an effective assignment and `supersedes_id` does not name it.

## Status

Registered in `internal/httpapi/routes_status.go`. Owned by issue #75.

### `GET /api/v1/status`

- Who: any.
- Response `200`:

```json
{
  "object_store": {
    "reachable": true,
    "versioning": false,
    "object_lock": false,
    "require_versioning": true,
    "file_routes_available": false,
    "gobd_protection_active": false,
    "checked_at": "2026-12-01T10:00:00Z"
  }
}
```

- The values are the last periodic check, never a check per request.
- `gobd_protection_active` is true only when the bucket is reachable and has versioning and
  object lock. The app shell of #72 shows a banner while it is false, decided by
  `gobdBannerVisible` in `web/src/status.ts`. A failed request (the 501 before #75, a 401)
  counts as unknown and shows no banner.
- `file_routes_available` is false while the file routes answer `503 object_store_unavailable`:
  the bucket is unreachable, has no versioning while `S3_REQUIRE_VERSIONING` is on, or has no
  object lock while `S3_OBJECT_LOCK` is on. The process starts in every case.
- `/readyz` does not depend on any of this. It reports the database only.

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
  "rows": [{ "entity": "document", "line": 3, "status": "ok", "original_number": "RE-20260003" }],
  "errors": [{ "entity": "expense", "line": 41, "field": "paid_on", "reason": "not a date" }],
  "collisions": [{ "entity": "document", "line": 7, "original_number": "RE-20260007",
    "reason": "number already in solobill" }]
}
```

Order of an import:

1. Upload every original PDF through `POST /api/v1/files` with purpose `import_original`.
2. Reference those file ids in the import rows and run the dry run here.
3. Commit. The frozen payload of each imported document names its original by file id and
   SHA-256, both known before the commit.

### `POST /api/v1/imports`

- Who: owner.
- Request: `multipart/form-data`, the parts named by `docs/import.md`. The whole request body
  is limited to 10 MiB (10,485,760 bytes) by `http.MaxBytesReader`. The import files hold rows,
  never PDFs.
- Runs the dry run. Nothing is written to the ledger. The batch and its findings are kept.
- The dry run reports as an error of its row: an original number that fails
  `numbering.ValidNumber` (character set, at most 36 characters, no leading `.`, `-` or `/`), and
  a correction whose original is neither in solobill nor a row earlier in the same batch. A
  batch with any error cannot be committed.
- The dry run reports a referenced file id that does not exist, or whose purpose is not
  `import_original`, as an error of its row.
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
