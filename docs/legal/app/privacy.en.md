# Privacy notice of the solobill application

Draft. Not legal advice. The owner reviews the text before any public registration.
The German version is binding.

This notice covers the application at `app.solobill.de`. It describes what solobill itself
processes about its users. For the data a user stores about their own customers in the Service,
the user is the controller and solobill is their processor, see the data processing agreement.
The website solobill.de has its own policy.

Markers in the draft:

- `[OPEN: ...]` is a fact the owner must supply.
- `[NOT VERIFIED: ...]` is a statement the code or the chart does not prove.

## 1. Summary

- We store your account data to sign you in and to provide the Service.
- We set only the cookies needed for signing in. There are no advertising or analytics
  cookies.
- We run the Service on our own hardware in Germany, without a hosting provider.
- We use no analytics service in the application. `[NOT VERIFIED: A search of the code found
  none. The owner confirms.]`

## 2. Controller

Simon Ostendorf
Frankenstr. 8
52070 Aachen, Germany

Email: `info@solobill.de`

`[OPEN: Contact person for data subjects and, if required, a data protection officer.]`

## 3. Which data we process

### Account

If you have an account, we store:

- your email address
- your role (owner or member) and the status of the account
- your password as an Argon2id hash, not in plain text, if you sign in with a password
- your TOTP secret for the second factor and the counter of the last code used
- your passkeys: identifier, name, public key, counter, times of use
- the identifier (subject) of your account at the identity provider, if you sign in over OIDC
- the times the account was created and changed

`[NOT VERIFIED: The list follows a read of the migrations for accounts, invites and passkeys.
It is compared with the schemas before publication.]`

The TOTP secret sits in plain text in the database. We say so openly because it matters for
your risk assessment.

### Invitation

If someone invites you, we store your email address, the role, a hash of the invite token, the
expiry time and the time of redemption. An invitation is valid for seven days by default.

### Seller profile and entries

Your seller profile (name, address, tax number, VAT ID, bank details, email, phone, website)
and your entries are your data as a user. We process them to provide the Service. They may
contain data of your customers. The data processing agreement covers that.

### Registration

`[OPEN: Registration is not enabled in the code yet (issue 175). Per the concept the text names
the version of the terms of use and the time of acceptance. Further data at registration, for
example a confirmation email, is for the owner to set.]`

### Server logs

The application writes one log line per request with time, IP address, method, route pattern,
status code, number of bytes and duration. It contains neither your input nor the browser type.

`[NOT VERIFIED: The fields follow the govue access log (middleware.AccessLog) and must be
checked against the output of the application.]`

The ingress keeps its own access log. `[NOT VERIFIED: Content and retention of the ingress log.
Both are in the infrastructure repository.]`

Retention: `[OPEN: Retention of the logs. For ostendorf.tech it is 30 days. The owner sets the
period for solobill.]`

### Protection against overload and abuse

We limit login attempts per IP address and per account. The counters live in working memory and
expire on their own. For this limit the address of a request is evaluated. For IPv6 the network
of the first 64 bits counts.

## 4. Cookies and local storage

We set only technically necessary cookies. A consent banner is not needed for them (Section
25(2) no. 2 TDDDG).

| Cookie | Purpose | Duration |
| --- | --- | --- |
| `__Host-session` | keeps your session after sign in | until sign out or expiry |
| `__Host-login` | holds the state of a sign in in progress over OIDC | short, for that flow only |
| `solobill_invite_oidc` | holds an invitation in progress over OIDC | short, for that flow only |
| `__Host-govue-passkey` | holds the state of a passkey sign in in progress | short, for that flow only |

`[NOT VERIFIED: The names in the first, second and fourth row come from the govue
documentation and are checked against the running Service. Without TLS (development only) the
cookie prefix is missing. The govue session lasts 2 hours without activity and at most
12 hours. solobill sets no values of its own, this is still to be checked.]`

CSRF protection uses no cookie of its own. The token is derived from the session.

Your browser's local storage (localStorage) holds your colour mode choice (`govue-theme`) and
your language choice. Both stay on your device and only restore your choice. `[NOT VERIFIED:
Name of the key for the language choice.]`

## 5. Legal bases

- Account, sign in, session and provision of the Service: contract, Art. 6(1)(b) GDPR.
- Server logs and protection against abuse: legitimate interest, Art. 6(1)(f) GDPR. Our
  interest is secure and stable operation.
- Retention of bookkeeping data where we ourselves are legally bound: Art. 6(1)(c) GDPR.
  `[OPEN: A lawyer checks whether and which duties bind the provider itself. The retention
  duties of the user (Section 147 AO, Section 14b UStG) bind the user, not the provider.]`

## 6. Recipients

- We run the Service ourselves on our own hardware in Germany. There is no hosting provider.
- Email: as of the repository the application sends no email. The planned registration brings
  it. `[OPEN: Name and operator of the SMTP service, see annex 3 of the data processing
  agreement.]`
- Identity provider: if you sign in over OIDC, the sign in uses the identity provider of the
  Service (currently Keycloak). `[OPEN: Who runs it and whether it is open to users of the
  application. The owner decides whether OIDC sign in applies to public users.]`
- There are no further recipients. `[OPEN: The owner confirms.]`

No transfer to a third country takes place. `[OPEN: This also covers backups and the email
service.]`

## 7. Retention

- Account and entries: until the end of the contract. Issued documents, booked payments and
  expenses and receipts are immutable. The application does not delete them. `[NOT VERIFIED:
  There is no way to delete an account or its data. The owner decides when and how to delete
  after the contract ends, see the data processing agreement, section 9.]`
- Invitations: until redemption or expiry. `[NOT VERIFIED: whether expired invitations are
  deleted.]`
- Logs: `[OPEN: period]`
- Backups: `[OPEN: schedule and retention]`

## 8. Your rights

You have the right to:

- access (Art. 15 GDPR)
- rectification (Art. 16 GDPR)
- erasure (Art. 17 GDPR), unless a retention duty prevents it
- restriction of processing (Art. 18 GDPR)
- data portability (Art. 20 GDPR)
- object to processing based on Art. 6(1)(f) GDPR (Art. 21 GDPR)

Contact `info@solobill.de`. Data of your customers in the Service concerns the user as
controller. If you are a customer of a user and want access, contact that user.

You have the right to lodge a complaint with a supervisory authority.
`[OPEN: The competent supervisory authority with its address. Proposal: the State Commissioner
for Data Protection and Freedom of Information of North Rhine-Westphalia. The owner confirms.]`

The provision of data is contractual: without an email address and credentials we cannot run
an account. No automated decision making including profiling takes place.

## 9. Security

The connection to the application is encrypted with TLS. `[NOT VERIFIED: TLS at the ingress.]`
Further measures are in annex 2 of the data processing agreement.

## 10. Changes

We adapt this notice when the Service or the law changes.

Date: `[OPEN: Date of publication]`
