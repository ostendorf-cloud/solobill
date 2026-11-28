# Self registration

A visitor creates an own account and an own tenant, behind a switch that is off by default.

Issue #175. Written before any code. Statements marked "unverified" name what was not checked.

## Problem

- A tenant exists only through the seed (`cmd/solobill/main.go`, `seedTenant`) or by hand.
- An account exists only through an invite or the seed. No signup route is mounted.
- An invite is created by an owner, and the API returns the raw token. The app sends no mail.
- A stranger cannot start. The owner of the tool must create the tenant and hand over a link.
- The landing page can only offer "Anmelden", because no signup exists (`site.md`).

## Goals

- A visitor registers with an email address, a password and a TOTP code, and owns a new tenant.
- An account is never usable before the person opened the link in a mail to that address.
- An unfinished registration is deleted after a period set in configuration, with its data.
- `REGISTRATION_ENABLED` is off by default. Off means no registration route and no register button.
- `GET /api/v1/auth/methods` reports the switch, so the SPA shows the button only when it is on.
- Registration records which versions of the terms the person accepted, and when.
- Abuse is bounded: no unlimited mail to a stranger, no unlimited rows, no cheap Argon2id load.
- Both factors are set in one step, as on the invite path. No password only account exists.

## Non goals

- The texts of the terms (#164), the data processing agreement and the privacy notice (#165).
- Registration through OIDC. The admission gate stays invite only (`internal/auth/doc.go`).
- Passkeys at registration. They are added later in the account settings (#174).
- Plans, prices, trials, quotas and payment.
- A password reset mail, a "send me my login" mail, and mail for invites.
- Self service deletion and export of a tenant. See the open questions.
- An onboarding wizard. A new tenant fills the seller profile like the seed tenant does.
- Captcha from a third party. See the alternatives.

## Decisions of the owner

- 2026-11-25: the email address is verified by a mail link before the account is usable.
- 2026-11-25: an unverified account is deleted after a period that this concept names.
- 2026-11-25: every user gets an own tenant at once. The registered user is its owner.
- 2026-11-25: password plus TOTP at registration, like the invite path.
- 2026-11-25: `REGISTRATION_ENABLED` is off by default, in general. The owner turns it on.
- 2026-11-25: the landing page register buttons follow the switch (landing issue #155).
- 2026-11-25: build order is #149, then #174, then this issue. All three touch the router.

## Approach

### Shape

- Registration is a self invite. The mail link proves the address, then one step sets both factors.
- No tenant and no account exist before the link is used. A pending registration is one row.
- That row is the only thing the purge deletes. It also keeps the existence of an address hidden.
- govue's `account.Service.SignUp` is not used. It creates an account with a later TOTP confirm.
  `solobill.md` rules that out ("Login on govue"). govue's own doc says SignUp is an existence oracle.

### Flow

1. `POST /api/v1/registrations` takes the email, the accepted terms versions and a proof of work.
   It answers `202` with the same body for every address, known or not. The mail leaves after the answer.
2. The mail carries a link to `/register/confirm` with a single use token.
   The token is 256 random bits, stored as a hash, valid for `REGISTRATION_TTL`.
3. `POST /api/v1/registrations/totp-secret` issues the TOTP secret for the token, server owned.
4. `POST /api/v1/registrations/confirm` takes the token, login, password and TOTP code.
   One transaction creates the tenant, the owner account and the terms record.
   The same transaction marks the token used.
5. The confirm step starts a session, as `handleInviteConfirm` does (`internal/httpapi/auth_handlers.go`).
6. A purge deletes pending registrations after `REGISTRATION_TTL` plus a short grace.

### Reuse and new parts

- Token helpers: `GenerateInviteToken` and `HashInviteToken` in `internal/auth`.
- TOTP and hashing: govue `account` through the shared `Hasher`, as `InviteManager` does.
- Rate limits: the `checked` pattern of `internal/httpapi/login_limits.go`, with own values.
- New: `internal/auth/registration.go`, a manager next to `InviteManager`.
- New: `internal/httpapi/routes_registration.go`, mounted only when the switch is on.
- New: `internal/config/config_registration.go` and `cmd/solobill/wire_registration.go`.
- New: `web/src/router/routes/register.ts`, two views, and `register.json` in `de` and `en`.
- New: a mail sender. Nothing in the app sends mail today (`charts/solobill/README.md`, egress).
  govue v0.12.0 ships `mail` (SMTP, STARTTLS by default, plain text, no storage). It is read from the
  module cache, not run.

### Data

- `registrations`: `id`, `email`, `token_hash`, `totp_secret`, `terms` (versions), `created_at`,
  `expires_at`, `used_at`, a send counter. No `tenant_id`, no row level security.
- `terms_acceptances`: `tenant_id`, `account_id`, `document`, `version`, `accepted_at`.
  Tenant scoped with row level security. The app role gets SELECT and INSERT only.
- No IP address and no user agent is stored. The `site.md` waitlist follows the same rule.
- Own tables mean no change to `accounts`, `invites` or `tenants`.

### Switch

- Off: the routes are not mounted and answer a JSON `404`, like the OIDC routes without OIDC.
- Off: `GET /api/v1/auth/methods` answers `registration: false`. The SPA hides the link and the route.
- On: startup refuses when the terms versions or the mail settings are missing.
  The switch cannot open a registration that cannot record acceptance or send a mail.
- A tenant created by registration is an ordinary tenant. Turning the switch off removes nothing.

### Abuse protection

- Every registration costs the sender a mail. The answer is the same for every address.
- Per mailbox: at most 3 mails in 48 hours, at least 10 minutes apart. The mailbox is the lower case
  address with a `+tag` removed. These are govue `optin`'s defaults, read from its doc.
- Per client address: a limit in front of the registration routes, own values from configuration.
- A global ceiling on mails per hour for the process. 50 per hour is govue `optin`'s default.
- A proof of work in front of step 1, from govue `pow`. It is a speed bump, the limits are the control.
- Argon2id runs only in step 4, after a valid token. The route sits behind the `checked` limit.
- The state of the limiters and of the proof of work is in process memory. The chart runs one replica.

## Alternatives

- Create the tenant and a pending account at step 1. Rejected: a stranger fills `tenants` with rows,
  the purge must delete two tables and a tenant, and a taken address would show.
- govue `account.Service.SignUp` and its handler. Rejected: password only window and existence oracle.
- Reuse `invites` for the pending state. Rejected: `invites.tenant_id` is required, so a tenant
  would have to exist before the address is proven.
- Magic link login instead of password and TOTP. Rejected: the owner decided on password plus TOTP.
- Hosted captcha. Rejected: a third party processor, which needs an entry in the privacy notice.
- Open registration with no switch. Rejected: the owner wants it off until the legal texts exist.

## Open questions

Each line: question, recommendation, owner. The owner decides all of them.

1. Is a pending registration row, with no account before the link, acceptable? The decision says
   "an unverified account is deleted". Recommend yes: the effect is the same and nothing is created
   for a stranger. Owner: owner.
2. How does a registered user get a tenant slug? The login path needs one, and an unknown tenant
   must keep answering like a wrong password. Recommend a generated slug of 12 random characters,
   shown once on the confirm page, with a login link `/login?tenant=<slug>`. Owner: owner.
3. Is a "send me my login" mail a follow up issue before the switch goes on? Users will forget the
   slug. Recommend yes, a separate issue. Owner: owner.
4. May one address register twice, as two tenants? The schema allows it, since email is unique per
   tenant only. Recommend yes, with the same answer, so no address shows as taken. Owner: owner.
5. What does the unfinished registration live for? Recommend `REGISTRATION_TTL` of 48 hours,
   bounded in configuration, the value of the waitlist in `site.md`. Owner: owner.
6. Which limits? Recommend per mailbox 3 per 48 hours, per client 5 registrations per hour,
   global 50 mails per hour. These are starting values from `optin` and the login limits. Owner: owner.
7. Is a proof of work wanted in front of step 1? Recommend yes. Whether govue's web package exposes
   a reusable solver is unverified, `GovueWaitlistForm` exists. A missing piece is a govue issue
   first. Owner: owner.
8. Which terms acceptance is recorded? Recommend `terms_acceptances` as an append only table with
   `document`, `version` and `accepted_at`. A table lets a later version ask again, columns on
   `accounts` do not. The request carries the version shown, and the server refuses a stale one.
   Owner: owner, with #164.
9. Which documents does registration record? Recommend three: the terms, the data processing
   agreement as a separate box, and the privacy notice as a seen version, not a consent. Whether
   the agreement belongs at registration is a legal question, unverified. Owner: owner, with #165.
10. What are the version identifiers? Recommend a date string such as `2026-12-01`, set in
    configuration next to the switch, so a text change and its version change together. Owner: owner.
11. Who builds the mail sender? Nothing sends mail today. Recommend this issue adds the minimal part:
    SMTP settings with `_FILE` secrets, a chart value, the egress rule and a small interface that the
    later document delivery reuses. Alternative: a small issue before this one. Owner: owner.
12. Which sender address and which SPF, DKIM and DMARC? `site.md` names `noreply@solobill.de` for
    the site and leaves the DNS records open. Recommend the same sender. Owner: owner.
13. Where does the purge run? Recommend an hourly in process task, since one replica runs and the
    delete is idempotent. Alternative: a CronJob like `files verify`. Owner: owner.
14. How does the landing page learn the switch? `site.md` says the site never reads app data.
    Recommend a value in the site chart, set together with the app value. The risk is drift between
    the two. Alternative: the site calls `/api/v1/auth/methods` at runtime. Owner: owner, with #155.
15. Where does the token sit in the link? Recommend the URL fragment, so it stays out of access logs.
    The SPA reads it and posts it. The invite link shape is unverified. Owner: owner.
16. What is the tenant name? `tenants.name` must not be empty. Recommend the slug until the seller
    profile sets a legal name. Owner: owner.
17. Does the table `registrations` need an entry in `tablesWithoutTenantID`? Recommend yes, with the
    reason. `solobill.md` says only #67 and #82 edit that list, so this is a contract change.
    Owner: owner.
18. Which migration range? The table "Migration timestamp reservation" ends at #82,
    `20261121200000` to `20261121200099`. Recommend `20261201100000` to `20261201100099`, after
    every reserved range, so dbmate order needs no `--strict` (unverified against dbmate 2.38).
    Grants go in `docker/postgres/grants/20261201100000_registration.sql`. The ranges of #149 and #155
    are unknown. Owner: owner.
19. Who adds the row for this issue to the reservation table? Only #67 edits `solobill.md` in the
    milestone. Recommend the pull request of this issue, once the milestone is closed. Owner: owner.
20. Do self service deletion and export of a tenant come before the switch goes on? A public service
    needs them for GDPR. Recommend yes, a separate issue. Owner: owner.
21. What does a new tenant see before it has a seller profile or a VAT regime period? Unverified.
    `routes_profile.go` handles `ErrNotFound`, but the full empty state was not exercised. Recommend
    a check in the implementation plan. Owner: maintainer.

## Related

- [site.md](site.md): the landing page, the waitlist limits and the legal texts decision.
- [solobill.md](solobill.md): "Login on govue", "Migration timestamp reservation", "Conflict zones".
- [ADR 0002](../adr/0002-site-own-deployment.md): the site is its own deployment.
- [ADR 0003](../adr/0003-api-host.md): `/api` on each host, so the app API is on the app host.
- Issues: #149 (split), #174 (account settings), #155 (landing buttons), #164 (terms), #165 (agreement).
