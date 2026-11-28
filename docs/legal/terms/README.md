# Terms of use for the solobill app (draft)

Draft terms for the app at `app.solobill.de`, for issue 164.
They are review material for the owner and a lawyer. They are not legal advice.

## Files

- `terms.de.md`: formal German text. It is binding.
- `terms.en.md`: English convenience translation.

## Why markdown under docs/

- The app has no legal route, no `GovueLegalPage` wiring and no footer link yet.
- Wiring them is application code and is out of scope of this draft.
- The route `/terms` with the aliases `/agb` and the footer link come with the registration
  work (issue 175), see `website-conventions`.

## Deviations and lawyer questions

- The terms contain no dispute resolution clause, per the owner rule of the website conventions.
- Question for a lawyer: a business offering to consumers can owe the information under
  Section 36 VSBG. The draft states nothing, because the owner rule excludes the section.
  The question disappears if the service admits business users only.
- Question for a lawyer: the deemed consent in section 11.
- Question for a lawyer: the limit of liability in section 9 and the data loss clause in 9.5.

## Statement to code checklist

| Statement | Terms | Evidence | Result |
| --- | --- | --- | --- |
| Offers, invoices, payments, expenses, other income, receipts | 2.1 | `internal/httpapi/routes_*.go`, `db/migrations` | verified by file read |
| EÜR from the user's entries, disclaimer | 2.1, 3 | `routes_reports.go`, `web/src/i18n/locales/*/reports.json` | verified by file read |
| Issued documents, booked rows, receipts are immutable | 7.2 | triggers in `db/migrations`, `docs/concepts/solobill.md` | verified by file read, not run |
| Invoice PDF and EÜR CSV | 7.4 | `GET /api/v1/documents/{id}/pdf`, `GET /api/v1/reports/euer/{year}/csv` | verified by file read |
| Complete export of all account data | 7.4 | none | not verified, no such route |
| Single instance, sessions in memory, restart signs out | 6.3 | `charts/solobill/README.md` | verified by file read |
| Password, TOTP, passkey | 4.3 | `internal/auth`, migration `passkey_credentials` | verified by file read |
| Record of terms version and time | 4.5 | `docs/concepts/registration.md` on branch 175 | not verified, planned |
| Backups | 7.5 | none | not verified |
| Account deletion, deletion of receipts | 10.3 | none, `storage.ObjectStore` has no delete | not verified, no such path |
| Retention periods named | 7.3 | Section 147 AO, Section 14b UStG, `docs/concepts/solobill.md` | taken from the concept doc, not checked against the statute text |

## Open owner decisions

- Who the users are: business only or consumers too (issue 28). This changes sections 1, 9, 11
  and 12.
- Pricing model: price, billing period, payment, trial. Section 5 is empty until then.
- Liability wording and cap, section 9.
- Availability promise in percent, section 6.1.
- Notice periods for the Provider and the export period after termination, section 10.
- Notice period for term changes, section 11.
- Place of jurisdiction, section 12.2.
- Date and version of the terms.

## Placeholders in the text

- `[OPEN]` or `[OFFEN]` marks a decision or fact the owner supplies.
- `[NOT VERIFIED]` or `[NICHT GEPRÜFT]` marks a statement the code does not prove.
- Provider details come from `docs/concepts/site.md` and the ostendorf.tech imprint. The owner
  confirms them for the app.
