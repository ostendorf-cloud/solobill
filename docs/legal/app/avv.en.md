# Data processing agreement (DPA) for solobill

Draft. Not legal advice. The owner and a lawyer review the text before any public registration.
The German version is binding. This English text is a convenience translation.

Markers in the draft:

- `[OPEN: ...]` is a decision or fact the owner must supply.
- `[NOT VERIFIED: ...]` is a statement the code or the chart does not prove.

## Parties

- Controller (the "Customer"): the user of the Service, as stated at registration.
- Processor (the "Processor"): Simon Ostendorf, Frankenstr. 8, 52070 Aachen, Germany, email
  `info@solobill.de`.

`[OPEN: The Processor names a contact person for data protection questions and, if required, a
data protection officer. Without a decision the email address above applies.]`

## 1. Subject matter, duration and scope

1. The Processor runs the web application solobill at `app.solobill.de`. The Customer uses it
   for bookkeeping under the terms of use.
2. In doing so the Customer stores personal data of third parties in the Service, for example
   of customers, suppliers and contact persons. The Processor processes this data on behalf of
   the Customer under Art. 28 GDPR.
3. This agreement applies as long as the Processor processes such data for the Customer. It
   starts with registration and ends when the contract of use ends and the data is deleted or
   returned under section 9.
4. For the data the Processor holds about the Customer itself (account, credentials, logs) the
   Processor is the controller. The privacy notice of the Service covers that, not this
   agreement.
5. `[OPEN: Whether the Customer accepts the DPA separately at registration or accepting the
   terms of use is enough. The registration concept leaves the question open.]`
6. `[OPEN: The Service targets sole traders, see issue 28. A consumer who processes data only
   for purely personal purposes is outside the GDPR under Art. 2(2)(c). The owner decides who
   may be a Customer.]`

## 2. Nature and purpose of the processing

The Processor processes the data to provide the Service:

- store, retrieve and display
- compute offers and invoices and output them as PDF
- record payments, expenses, other income and receipts
- compute reports, for example the EÜR, and provide them as CSV
- back up and restore `[NOT VERIFIED: see annex 2 and section 7]`

The Processor processes the data for no other purpose unless a law requires it. In that case it
tells the Customer of the requirement beforehand, as far as the law permits.

## 3. Types of data and categories of data subjects

The data the Service can hold according to the database migrations is listed in annex 1.
Categories of data subjects:

- the Customer's customers and their contact persons
- the Customer's suppliers
- the Customer's users and their staff, if the Customer creates further accounts
- other persons whose data the Customer enters in free text fields and receipts

Special categories of personal data (Art. 9 GDPR) are not intended. The Customer does not enter
them in the Service. The Customer cannot rule out that an uploaded receipt contains such
details. `[OPEN: The owner checks this sentence.]`

## 4. Duties and rights of the Customer

1. The Customer is the controller under the GDPR. The Customer is responsible for the
   lawfulness of the processing and for safeguarding the rights of data subjects.
2. The Customer gives instructions in text form. Instructions are this agreement and the use of
   the Service. An individual instruction beyond that goes by email to `info@solobill.de`.
3. The Customer tells the Processor without delay if it finds errors or irregularities in the
   processing.
4. The Customer names a contact person for data protection. The email address of the account
   applies until the Customer names another.

## 5. Duties of the Processor

1. The Processor processes the data only on documented instructions of the Customer
   (Art. 28(3)(a) GDPR). If it considers an instruction unlawful, it tells the Customer without
   delay. It may suspend execution until the Customer confirms or changes it.
2. The Processor binds the persons with access to the data to confidentiality (Art. 28(3)(b)).
   Currently only the Processor itself has access. `[OPEN: The owner confirms that no one else
   has access.]`
3. It takes the technical and organisational measures of annex 2 (Art. 28(3)(c), Art. 32 GDPR).
4. It assists the Customer with suitable measures in answering requests of data subjects under
   Chapter III GDPR (Art. 28(3)(e)). If a data subject addresses the Processor with a request
   that concerns the Customer, the Processor forwards it without delay and does not answer it
   itself.
5. It assists the Customer with the duties under Art. 32 to 36 GDPR (security, breach
   notification, impact assessment, consultation) within its means (Art. 28(3)(f)).
6. It keeps a record of processing activities for this processing (Art. 30(2) GDPR).
   `[NOT VERIFIED: The record is not in the repository. The owner keeps it.]`
7. Its contact point for data protection questions is `info@solobill.de`.

## 6. Breach notification

1. The Processor notifies the Customer in text form without undue delay of a personal data
   breach it becomes aware of. `[OPEN: The owner sets a period, for example 48 hours after
   becoming aware.]`
2. The notice contains, as far as known: the nature of the breach, the categories and the
   approximate number of data subjects and records, the likely consequences and the measures
   taken or proposed.
3. The Processor assists the Customer with the notification to the supervisory authority and to
   data subjects. The notification to the authority and to data subjects rests with the
   Customer.
4. `[NOT VERIFIED: A documented incident process (detection, responsibility, deadlines) is
   missing in the repository. The owner defines it.]`

## 7. Sub-processors

1. The Customer gives a general authorisation for sub-processors (Art. 28(2) GDPR). Those
   authorised at the start are in annex 3.
2. The Processor informs the Customer by email at least `[OPEN: period, for example 30 days]`
   before it uses or replaces a sub-processor. Within that period the Customer may object on
   an important data protection ground. On an objection the parties look for a solution. If
   none is found, the Customer may terminate the contract of use effective on the date of use.
3. The Processor binds every sub-processor by contract to the same data protection duties as in
   this agreement. It is liable for the sub-processor's breach as for its own.
4. Processing by the Processor itself on its own hardware in Germany is not sub-processing.
   There is no hosting provider. `[OPEN: The owner confirms that the hardware is at their
   disposal and located in Germany.]`

## 8. Place of processing

The Processor processes the data in Germany, on its own hardware. No transfer to a third
country takes place. `[OPEN: This also covers backups and the SMTP service in annex 3. The
owner confirms.]`

## 9. Deletion and return

1. After the contract of use ends, the Processor returns the data on request and then deletes
   it, unless a law requires storage (Art. 28(3)(g) GDPR).
2. The Customer can download invoices as PDF and the EÜR as CSV itself. `[NOT VERIFIED: The
   code has no complete export of all data of an account. The owner decides whether to build
   it or to deliver on request, and in which format.]`
3. The Service has no way to delete an account or its data. Customers are only marked as
   deleted. Issued documents, booked payments and expenses and uploaded receipts are immutable
   and the application never deletes them. `[NOT VERIFIED: Deletion after the end of the
   contract is not provided for technically. The owner decides how and when to delete, also
   with a view to the retention duties of the Customer (Section 147 AO, Section 14b UStG) that
   bind it as a trader.]`
4. Backups are overwritten after `[OPEN: period]`. `[NOT VERIFIED: Schedule and retention of
   backups are unknown.]`

## 10. Evidence and audits

1. The Processor makes available to the Customer the information needed to show compliance with
   the duties under Art. 28 GDPR (Art. 28(3)(h)).
2. The Customer may check compliance with this agreement or have it checked by a bound third
   party. It announces the check with reasonable notice and does not disturb operations
   disproportionately.
3. `[OPEN: Whether the check first runs through written information and annex 2 and an on site
   visit only for good cause. The owner decides.]`

## 11. Liability

The liability clause of the terms of use applies. Liability towards data subjects stays
unaffected (Art. 82 GDPR). `[OPEN: A lawyer checks the relation to the liability clause of the
terms of use.]`

## 12. Final provisions

1. Changes and side agreements need text form.
2. If this agreement and the terms of use conflict, this agreement prevails for the processing
   of personal data.
3. German law applies. The contract language is German. The English version serves
   understanding. If they differ, the German version applies.
4. Invalid provisions do not affect the others.

Date: `[OPEN: Date and version of the agreement]`

## Annex 1: Data and data subjects

Source: `db/migrations`, state of the repository. The Customer decides which fields to fill.
Most fields are optional.

| Area | Data | Data subjects |
| --- | --- | --- |
| Customers | name, email, VAT ID, street, postcode, city, country, further document fields | customers, contact persons |
| Documents | frozen invoice and offer data with customer details, lines, amounts, numbers | customers |
| Payments | amounts, date, link to documents | customers |
| Expenses | supplier (free text, up to 200 characters), amounts, date, receipt link | suppliers |
| Other income | amounts, description, date | anyone, depending on free text |
| Receipts | files (image or PDF), file name, checksum, size, time | anyone, depending on content |
| Catalogue | items, prices | none |
| Seller profile | name, address, tax number, VAT ID, bank details, email, phone, website | the Customer itself |
| Customer accounts | email, role, status, password hash, TOTP secret, passkey data, OIDC subject | the Customer's users |

`[NOT VERIFIED: The list follows a read of the migrations and not every function of the code.
Before publication it is compared field by field with the schemas.]`

## Annex 2: Technical and organisational measures

Each line names where the statement comes from. A measure without evidence in the repository is
marked not verified.

### Confidentiality

- Tenant separation: every table with tenant data has row level security. The application
  connects with a role without superuser and without `BYPASSRLS`. Evidence: `db/migrations`,
  `AGENTS.md`, `charts/solobill/README.md`. The chart cannot enforce the right role.
  `[NOT VERIFIED: actual role in production]`
- Access control: passwords are stored as an Argon2id hash. TOTP as a second factor, passkeys
  and OIDC login exist. Evidence: `internal/auth`. `[NOT VERIFIED: Argon2id parameters]`
- The TOTP secret sits in plain text (Base32) in the database. There is no application level
  encryption. Evidence: `db/migrations/20261112233249_create_accounts.sql`.
- Sessions: a cookie with a signed identifier, HttpOnly, Secure and the `__Host-` prefix in
  production. Evidence: `internal/config/config.go`, govue `session`.
- CSRF protection for state changing requests. Evidence: govue `csrf`, `internal/httpapi`.
- Rate limits for login attempts per address and account. Evidence:
  `internal/httpapi/login_limits.go`.
- Encrypted transport: TLS at the ingress. `[NOT VERIFIED: The chart has no certificate
  management. The evidence is in the infrastructure repository.]`
- Encryption at rest: `[NOT VERIFIED: Disk, database and object store are not encrypted
  according to the repository. The owner states the current position.]`

### Integrity

- Issued documents, their lines, payments, booked expenses and receipt data are immutable
  through triggers and grants. Evidence: `db/migrations`, `docs/concepts/solobill.md`.
- Receipts carry a SHA-256 checksum. It is recomputed on download. Evidence:
  `docs/concepts/solobill.md`.
- The object store is to get versioning and object lock. `[NOT VERIFIED: The concept names both
  as an operating requirement. Whether they are active is not in the repository. Per the
  concept, support in SeaweedFS is not checked against the version in use.]`

### Availability and resilience

- The Service runs as one instance of the application. A restart signs all users out.
  Evidence: `charts/solobill/README.md`.
- Backup of the database and the object store: `[NOT VERIFIED: The concept names the backup of
  the cluster for the metadata and a separate backup of the bucket. Schedule, location,
  encryption and restore test are not evidenced.]`
- Health endpoints and metrics: `/healthz`, `/readyz`, `/metrics`, not routed publicly.
  Evidence: `AGENTS.md`.

### Separation

- The data of different Customers lives in the same database, separated by row level security.
  There are no separate databases per Customer.
- The object key contains the tenant id. Evidence: `docs/concepts/solobill.md`.

### Review

- Tests that check tenant separation run in CI (`task test:integration`). Evidence:
  `AGENTS.md`.
- Regular review of the measures and penetration test: `[NOT VERIFIED: not evidenced]`
- An access log with time, address, route and status. There is no log of access to individual
  records (audit log).

## Annex 3: Sub-processors

| Sub-processor | Service | Data | Location |
| --- | --- | --- | --- |
| none as of this draft | | | |

- Hosting: own operation in Germany. There is no hosting provider. `[OPEN: confirm]`
- Email sending: as of the repository the application sends no email. The planned registration
  (issue 175) brings sending over SMTP. `[OPEN: Name, seat and operator of the SMTP service.
  If it is an own mail server, there is no sub-processor. If it is a provider, it is entered
  here and needs its own contract.]`
- Backup storage: `[OPEN: Location and operator. If a third party holds it, that party is
  entered here.]`
