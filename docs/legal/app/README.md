# Data processing agreement and privacy notice of the app (draft)

Drafts for issue 165. They are review material for the owner and a lawyer. They are not legal
advice.

## Files

- `avv.de.md`: the agreement under Art. 28 GDPR in formal German. It is binding. Annex 1 holds
  the data, annex 2 the measures, annex 3 the sub-processor list.
- `avv.en.md`: English convenience translation.
- `privacy.de.md`: the privacy notice of the app in German. It is binding.
- `privacy.en.md`: English translation.

## Why markdown under docs/

- The app has no legal route, no `GovueLegalPage` wiring, no footer link and no registration
  step yet.
- Wiring them is application code and out of scope. The terms draft (issue 164) shares the
  route and footer work.
- The AVV as a download linked at registration needs the registration work of issue 175.

## Statement to code checklist

| Statement | Document | Evidence | Result |
| --- | --- | --- | --- |
| Tenant separation by row level security, app role without `BYPASSRLS` | AVV annex 2 | `db/migrations`, `AGENTS.md`, `charts/solobill/README.md` | read, role in production not verified |
| Passwords as Argon2id hash | AVV annex 2, privacy 3 | `internal/auth/login.go`, `internal/httpapi/auth_handlers.go` | read, parameters not verified |
| TOTP secret in plain text, 32 characters | AVV annex 2, privacy 3 | `db/migrations/20261112233249_create_accounts.sql` | read |
| Passkeys stored with name, counter, data | privacy 3 | `db/migrations/20261120090000_create_passkey_credentials.sql` | read |
| OIDC subject stored | privacy 3 | `accounts.oidc_subject` | read |
| Invite stores email, role, token hash, expiry | privacy 3 | `db/migrations/20261112233253_create_invites.sql`, `internal/config/config.go` (7 days) | read |
| Customer fields | AVV annex 1 | `db/migrations/20261113150832_create_customers.sql` | read |
| Customers soft deleted only | AVV 9 | same file, `deleted_at` | read |
| Documents, payments, expenses, receipts immutable | AVV 9, annex 2 | triggers, `docs/concepts/solobill.md` | read, not run |
| No delete path for objects | AVV 9 | `docs/concepts/solobill.md`, `storage.ObjectStore` | read |
| SHA-256 on receipts | AVV annex 2 | `docs/concepts/solobill.md` | read |
| Object lock and versioning | AVV annex 2 | `docs/concepts/solobill.md` | not verified, operating requirement |
| Single instance, sessions in memory | AVV annex 2 | `charts/solobill/README.md`, `cmd/solobill/main.go` | read |
| Login rate limits per address and account | AVV annex 2, privacy 3 | `internal/httpapi/login_limits.go` | read |
| Cookie `solobill_invite_oidc` | privacy 4 | `internal/auth/oidc.go` | read |
| Cookies `__Host-session`, `__Host-login`, `__Host-govue-passkey` | privacy 4 | govue v0.12.0 sources, `internal/config/config.go` | read in govue, not in the running app |
| CSRF without cookie | privacy 4 | govue `csrf`, `internal/httpapi` | read in an earlier pass, not re-run |
| Access log fields | privacy 3 | `internal/httpapi/httpapi.go` (`middleware.AccessLog`) | read, output not verified |
| No analytics in the app | privacy 1 | search of `web/src`, `internal`, `cmd`, `charts` for umami, analytics, gtag, plausible | no hit |
| No audit log | AVV annex 2 | search of `db/migrations`, `internal` | read |
| App sends no email | AVV annex 3, privacy 6 | `internal/config/config.go` has no SMTP setting | read |
| Backups, TLS, encryption at rest, incident process | AVV | none | not verified |

## Data flow to statement checklist

| Data flow | Statement in the drafts |
| --- | --- |
| Browser to ingress to frontend and API | logs and TLS, privacy 3 and 9, both marked not verified |
| API to PostgreSQL | AVV annex 1 and 2 |
| API to object store (receipts) | AVV annex 1, 2 and 9 |
| API to identity provider (OIDC) | privacy 6, open question on who runs it |
| API to SMTP | none yet, AVV annex 3 open |
| Backups | AVV 9 and annex 2, all not verified |
| Metrics endpoint | AVV annex 2, not public |
| Browser localStorage | privacy 4 |

## Discrepancies with the issue text

- The issue says the cookies are "session and CSRF only". The code derives the CSRF token from
  the session and sets no CSRF cookie. The real cookies are the session cookie, the OIDC login
  cookie, the OIDC invite cookie and the passkey ceremony cookie.
- The issue lists "audit data". The migrations hold no audit log table.
- The issue names the SMTP service as a sub-processor. The app has no mail code yet, so annex 3
  lists none and marks the mail service open.

## Open owner decisions

- Backups: schedule, location, retention, encryption, restore test.
- Retention: server logs, ingress logs, Umami (if used in the app), invites, backups.
- Deletion: how and when an account and its data are deleted after the contract ends, given
  that the application has no delete path.
- Export: a complete export of all data of an account, or on request.
- Sub-processors: SMTP service name and operator, backup storage, identity provider operator.
- Contact for data subjects, data protection officer, supervisory authority.
- Who may be a Customer (business only or consumers, issue 28).
- Separate acceptance of the AVV at registration.
- Breach notice period and incident process.
- Notice period for new sub-processors, audit mode.
- Encryption at rest and TOTP secret encryption.

## Lawyer questions

- The Art. 6(1)(c) basis in the privacy notice section 5.
- The cookie statement under Section 25(2) no. 2 TDDDG.
- The relation of the AVV liability clause to the terms of use.
- The general authorisation of sub-processors and the objection right.
