# solobill.de site and waitlist

The marketing site on `solobill.de` and its waitlist with double opt-in. Issue #154, parent #5.
The app itself is described in [solobill.md](solobill.md).

## Problem

- solobill has no public page. A visitor who hears the name finds nothing about the product.
- Interest in the product is not recorded anywhere, so nobody can be told when it launches.
- A waitlist collects personal data and sends advertising mail, which needs proven consent.
- The app runs at `app.solobill.de` behind a login. It cannot carry public pages or a signup.

## Goals

- `solobill.de` serves prerendered pages in German (Sie) and English under `/de` and `/en`.
- A visitor joins the waitlist with double opt-in. No address gets a second mail unconfirmed.
- An unconfirmed entry is gone 48 hours after its request.
- Every mail after the confirmation carries a working unsubscribe link.
- The site deploys, scales and fails independently of the app and never reads app data.
- A capability is called available only when it exists in the app's code. All else is planned.
- A login button at the top right leads into the app at `app.solobill.de`.

## Non goals

- A signup flow. The app has none today, so the button never promises a signup.
- Prices, a checkout or a promise of free use. The page only says prices follow before the start.
- Terms of use (AGB) and a data processing agreement (AVV) for the app. Separate issues.
- Sending the launch notice or product news. This concept covers collecting and confirming.
- A newsletter tool, HTML mail or mail tracking.
- A cookie banner. The site sets no cookie.
- Site code that govue provides or should provide. A missing piece becomes a govue issue first.

## Decisions of the owner

- 2026-11-25: the site is its own deployment, independent of the app.
- 2026-11-25: the site is a govue `frontend` plus a small waitlist API, own chart `solobill-site`.
- 2026-11-25: the site has its own database and role and no access to app data.
- 2026-11-25: the source lives in a top level `landing/` folder, not under `web/`.
- 2026-11-25: `landing/` is its own Vite project with its own routes and prerender.
- 2026-11-25: consent covers the launch notice and product news. That is advertising.
- 2026-11-25: the consent text says so, and every mail carries an unsubscribe link.
- 2026-11-25: German in the formal form (Sie), English second, `/de` and `/en` URLs.
- 2026-11-25: unconfirmed entries are deleted after 48 hours.
- 2026-11-25: web analytics through govue `analytics` (Umami), cookie free, own privacy text.
- 2026-11-25: preview images come from the real app with fake data, by a repeatable script.
- 2026-11-25: each preview image is labelled as a preview.
- 2026-11-25: confirmation mail from `noreply@solobill.de` through the SMTP service of
  `ostendorf.tech`.
- 2026-11-25: API host wish: `api.solobill.de` if it works, else `/api` on each host.
- 2026-11-25: operator of `solobill.de` is the provider of `ostendorf.tech`, same provider data.
- 2026-11-25: imprint e-mail `info@solobill.de`. The other provider data as on `ostendorf.tech`.
- 2026-11-25: the imprint has a dispute resolution section: neither obliged nor willing.
- 2026-11-25: the imprint has the MStV paragraph 18 section 2 section, same person.
- 2026-11-25: imprint and privacy text are written fresh for this domain. No sentence is copied.
- 2026-11-25: the `ostendorf.tech` imprint, privacy policy and terms are the model for structure.
- 2026-11-25: the app is open to everyone at `app.solobill.de`, each user with an own login.
- 2026-11-25: a self signup becomes a goal of the app, replacing the "later" in solobill.md.
- 2026-11-25: the page shows a login button at the top right, like other SaaS landing pages.
- 2026-11-25: until a signup exists, the button says "Anmelden" and serves existing accounts.
- 2026-11-25: UWG paragraph 5: what exists in the app today may be called available.
- 2026-11-25: everything else is called planned. Each statement is checked against the code.
- 2026-11-25: no promise of free use. The page notes that prices follow before the start.
- 2026-11-25: unsubscribe deletes the address at once. No suppression hash is kept.
- 2026-11-25: confirmed entries are deleted 12 months after the launch mail.
- 2026-11-25: an address that signs up again is entered again like a new one.
- 2026-11-25: no IP address is stored. Proof of consent is request time, confirmation time and
  consent text version.
- 2026-11-25: hosting as on `ostendorf.tech`: own hardware in Germany, no hosting provider.
- 2026-11-25: there is no processor for hosting.
- 2026-11-25: the privacy text names the SMTP service of `ostendorf.tech` as the mail sender.
- 2026-11-25: the site reuses the colour seed of the app in `web/src/theme/`.
- 2026-11-25: the site uses the logo of the app. None exists yet. A wordmark logo is its own issue.
- 2026-11-25: terms of use (AGB) and a data processing agreement (AVV) for the app are own issues.
- 2026-11-25: AGB and AVV exist before a public registration opens.
- 2026-11-25: the AGB of `ostendorf.tech` (sound engineering) do not apply to solobill.
- 2026-11-25: API host: option B, `/api` on each host. [ADR 0003](../adr/0003-api-host.md).
- 2026-11-25: the owner would have liked `api.solobill.de` and follows B for its lower costs.

## Approach

### Deployments and images

- Chart `charts/solobill-site`, separate from `charts/solobill`.
  [ADR 0002](../adr/0002-site-own-deployment.md).
- Deployment `site-frontend`: govue `frontend` serving the prerendered build of `landing/`.
- Deployment `site-api`: the waitlist binary, for example `cmd/site-api`, on govue `optin`.
- `site-api` runs one replica with strategy `Recreate`. govue limiters and PoW set are per process.
- Two images from the shared `Dockerfile`, for example `solobill-site-frontend` and
  `solobill-site-api`.
- Both images build for `linux/amd64` and `linux/arm64` with digest pinned bases, non root.
- One release tag covers the app images and the site images. Each chart deploys on its own.
- Own Postgres database and role for `site-api`. The role holds no grant on the app database.
- SMTP credentials and the database password reach `site-api` as mounted files.

### Hosts and routing

- `solobill.de`: the site. `app.solobill.de`: the app, split into frontend and API by issue #149.
- The site frontend answers every path except the API prefix. The ingress routes that prefix.
- `/api` on each host, see "API host" below and [ADR 0003](../adr/0003-api-host.md).
- `solobill.de/api/` goes to `site-api`. `app.solobill.de/api/` goes to the app API.
- `/` redirects by language through govue. Legal routes and aliases come from govue.
- A sitemap for `https://solobill.de` comes from the prerender. Each language has a 404 page.

### Waitlist states

- `unconfirmed`: created by a request with consent. One confirmation mail, no advertising in it.
- `confirmed`: the confirm link was used within 48 hours. The consent proof is recorded.
- Consent proof: request time, confirmation time, consent text version. No IP address.
- `unsubscribed`: the unsubscribe link was used. The row is deleted in the same step.
- No suppression hash or other trace of an unsubscribed address stays.
- `deleted`: the row is gone. Unconfirmed rows go after 48 hours.
- Confirmed rows go 12 months after the launch mail.
- An address that was deleted can sign up again and starts as `unconfirmed`.
- A request for a known address answers like one for a new address. No enumeration.
- Only the confirmation mail ever goes to an unconfirmed address. A test proves it.
- A per address limit caps confirmation mails, so the form cannot flood a third party.

### Token handling

- Each confirm and unsubscribe token is random and single purpose. Only its hash is stored.
- The confirm token expires 48 hours after the request and is single use.
- The unsubscribe token lives as long as the entry and goes into every mail.
- A mail link opens a site page. The page sends the token to the API with a `POST`.
- A `GET` never changes state, so a mail scanner that follows links confirms nothing.
- Logs, metrics and errors carry no address and no token.

### Purge

- An expired confirm token is refused at once, whether or not its row is purged yet.
- `site-api` deletes unconfirmed rows past their expiry on a fixed interval.
- The interval is short against 48 hours, so a row outlives its expiry by at most that interval.
- An unsubscribe deletes its row at once. The purge does not wait for it.
- The same job deletes confirmed rows 12 months after the launch mail.
- The launch mail time is recorded, so the job knows when the 12 months start.

### Mail path

- `site-api` builds a plain text mail with govue `mail` and sends it by SMTP.
- The relay is the SMTP service of `ostendorf.tech`. Sender `noreply@solobill.de`.
- govue `mail` sends once, no queue, no retry. A failed send answers the request with an error.
- govue `mail` does no DKIM signing. The SMTP service has to sign. Not verified.
- A `List-Unsubscribe` header needs extra headers in govue `mail`. Not verified, see govue #267.

### Frontend

- `GovueHeader` with the language switch in link mode and the theme switch, `GovueFooter`.
- A login button at the top right links to `app.solobill.de`. Label "Anmelden", "Log in".
- Light and dark mode from the app's colour seed. Formal German through govue's i18n.
- Every claim of the copy is either backed by code in the app or phrased as planned.
- The pull request of the copy lists each "available" claim with the code that backs it.
- The waitlist form is govue's waitlist form with the site's consent text and privacy link.
- Result pages: check your inbox, confirmed, unsubscribed, link expired. Both languages.
- Umami analytics through govue `analytics`, cookie free.

### Legal pages

- Imprint and privacy text for `solobill.de`, in German and English, through govue legal routes.
- Model for structure and facts: `ImprintView.vue` and `PrivacyView.vue` of `ostendorf.tech`.
- The texts are written fresh. No sentence of `ostendorf.tech` is copied.
- Imprint: provider data as on `ostendorf.tech`, e-mail `info@solobill.de`.
- Imprint sections: contact, VAT ID, MStV paragraph 18 section 2, dispute resolution.
- Privacy: own hardware in Germany, server logs, Umami, rate limits, waitlist, mail sender.
- Privacy: the waitlist facts above, consent as legal basis if confirmed (open question).
- No terms page on the site. The app's AGB and AVV are separate issues.

## API host

The question: can one API at `api.solobill.de` serve the app and the waitlist across the origins
`app.solobill.de` and `solobill.de`? Option A is that host. Option B is `/api` on each host.

### Evidence

Read from govue v0.5.0 (the version solobill pins), govue `origin/main` d9d2e5a, solobill `main`
bce0810 and Go 1.27.4 `net/http`.

- Site: `app.solobill.de`, `api.solobill.de` and `solobill.de` are one site, `solobill.de`.
- So every request between them is same site and cross origin.
- Session cookie: `__Host-session`, `Path=/`, no `Domain`, `HttpOnly`, `Secure`.
  Source: `session/session.go`.
- `Domain` is not configurable. The cookie stays on the host that set it. Source: `session.cookie`.
- `SameSite` is `Lax` by default, `Strict` with `SameSiteStrict`. Source: `session.Config`.
- A `Lax` or `Strict` cookie goes with a same site request. So `api` gets its cookie from `app`.
- This needs `credentials: 'include'` in the client. govue's `createClient` has that option.
- CORS: `middleware.NewCORS` takes exact origins and `Credentials`, never a wildcard.
- solobill wires `ALLOWED_ORIGINS` into CORS with credentials and `X-CSRF-Token`.
- The same list goes to `csrf.Protector.TrustOrigin` and the account and passkey handlers.
- Source: `internal/httpapi/httpapi.go`, `internal/auth/login.go`, `internal/config/config.go`.
- CSRF origin half: Go's `CrossOriginProtection` passes `Sec-Fetch-Site` `same-origin` and `none`.
- A `same-site` write from `app` to `api` is refused unless its origin is trusted.
- CSRF token half: the token is an HMAC of the session id. It works from any origin.
- Passkeys: the browser checks the page origin, not the API host.
- The app page stays at `app.solobill.de` in both options.
- So the RP id is `app.solobill.de` or `solobill.de`, whatever the API host.
- govue checks at startup that every passkey origin is the RP id or below it.
  Source: `passkey/config.go`.
- govue advises the application host as RP id, not a parent domain. Source: `passkey.Config.RPID`.
- The RP id cannot change later. Every registered passkey is bound to it.
- The passkey challenge cookie is host only and `Strict`. A same site fetch carries it.
- OIDC: the login cookie `__Host-login` is `Lax`, not configurable.
  Source: `session/logincookie.go`.
- The callback is a top level navigation, so `Lax` works on any host.
- After the callback govue redirects to `Success`, default `/`. Source: `oidc/handlers.go`.
- The `next` target must be a local path on the callback host. Source: `oidc.localPath`.
- solobill sets no `Success` today. Source: `cmd/solobill/main.go`.
- The waitlist: govue `contact` checks the origin the same way and takes `TrustedOrigins`.
- govue `optin` does not exist yet. Its origin handling is assumed to follow `contact`.
- The govue `frontend` CSP comes from the caller, so `connect-src` can name another host.
- govue `frontend` proxies `/api/` only in development. Production routing is the ingress.

### Result

- Option A works with govue v0.5.0 for session, CSRF, CORS and passkeys, under conditions.
- Condition: `ALLOWED_ORIGINS=https://app.solobill.de` on the app API, and only that origin.
- Condition: the app client uses `baseUrl` `https://api.solobill.de/api/v1`.
- Condition: the app client sends `credentials: 'include'`.
- Condition: the app frontend CSP adds `https://api.solobill.de` to `connect-src`.
- Condition: OIDC `Success` becomes `https://app.solobill.de/`. Deep links through `next` are lost.
- Condition: a deep link survives login only if the app stores it itself before the redirect.
- Condition: links to API resources in the app, such as PDF downloads, become absolute.
- Cost: every write is a CORS preflight. solobill sets no `MaxAge`, so preflights repeat often.
- Cost: `api.solobill.de` routes to two deployments from two charts on one hostname.
- Option B needs none of these conditions. It is the shape issue #149 already plans.
- Same site holds in both options. `SameSite` gives no separation between site and app in either.
- In both options the app API must never trust `https://solobill.de`.
- Not verified: browser behaviour in practice. No browser test ran for this concept.
- Not verified: Safari tracking prevention treats a same site credentialed fetch as first party.
- Not verified: the ingress controller of the cluster merges two routes on one hostname.
- Not verified: the browser default for a preflight cache without `Max-Age`.

### Decision

- Option B: `/api` on each host. Same origin for the app and for the site.
- Decided by the owner on 2026-11-25. He would have liked `api.solobill.de`.
- He follows B for its costs: no CORS, no trust lists, no OIDC redirect change in the app.
- It keeps the site independent: own host, own routes, no shared hostname with the app chart.
- It matches issue #149 and `ostendorf.tech`.
- Issues #149, #151, #153 and the landing issues already assume B. None of them changes.
- Option A stays possible later. The cost is the condition list above, mostly configuration.
- The passkey RP id does not depend on this choice. Keep it the app host.

### Dropped option A

The table keeps what option A would have changed. These changes are the reasons it lost.

| Issue | Option A: `api.solobill.de` (dropped) | Option B: `/api` per host (chosen) |
| --- | --- | --- |
| #149 split | Ingress host `api.solobill.de` for the API, frontend alone on `app`. Client `baseUrl` and `credentials: 'include'`, `ALLOWED_ORIGINS`, CSP `connect-src`, compose with two origins, end to end test across origins. The "one origin" context line goes. | No change. |
| #151 language URLs | The post login target cannot carry `/de/...` through OIDC `next`. The app keeps the target itself. | No change. |
| #153 OIDC paths | Redirect URL `https://api.solobill.de/api/v1/auth/oidc/callback`. `Success` set to the app URL. | Redirect URL on `app.solobill.de`. No `Success` change. |
| Landing frontend | CSP `connect-src` names `https://api.solobill.de`. Form endpoint absolute. | Form endpoint `/api/...`. |
| Waitlist API | Trusts origin `https://solobill.de`. CORS without credentials. Path prefix agreed with the app API. | Same origin. No CORS. |
| Waitlist form | Endpoint absolute, `credentials: 'omit'`. | Endpoint relative. |
| Legal texts | Privacy text names the API host. | No extra host. |
| Site chart | Route on `api.solobill.de` for the waitlist prefix next to the app chart's route. | Route `solobill.de/api` to `site-api`. |

## govue dependencies

- govue [#267](https://github.com/ostendorf-cloud/govue/issues/267): `optin` package with double
  opt-in. Blocks the waitlist API.
- govue `GovueWaitlistForm`: the Vue form for `optin`. Blocks the waitlist form.
- Check before the landing frontend: does `govue-prerender --out-dir` with `localizedFiles` cover
  a multi route site? If not, a govue issue first.
- Later, not for launch: a shared limiter and PoW store in govue, so `site-api` can scale out.

## Alternatives

- Waitlist inside the app API: couples site and app releases, and puts addresses next to tenants.
- Site pages inside the app SPA under `web/`: one build and one release for two audiences.
- One chart for app and site: the owner wants independent deployment.
- A hosted newsletter service: a third party processor, own consent flow, and govue cannot reuse it.
- A static host for the site: no waitlist API and no govue `frontend` headers.
- One API host `api.solobill.de`: works, but costs the changes in "Dropped option A".

## State on 2026-11-25

- Merged: #153, the OIDC routes under `/api`. Merged: #162 and #163.
- govue app mode (govue pull request 269) is merged and waits for a govue release.

## Open questions

- Self signup: the app has none today, only invites, local accounts and OIDC.
  Owner: owner. Next: a signup issue, then the button may promise a signup.
- Users beyond a Kleinunternehmer: who may sign up, and which tax setups the app must serve.
  Owner: owner. Next: decide with the signup issue and #28.
- Trademark check of the name solobill. Owner: owner. Next: check before the site goes live.
- Who sets SPF, DKIM and DMARC for `solobill.de` in the infrastructure repository, and when.
  Owner: owner. Next: name it in the site chart issue's list for the infrastructure repository.
- Domain: is `solobill.de` registered, is its DNS in the infrastructure repository?
  Owner: owner. Next: answer in #154.
- Issue #28 (audience, price) shapes the page copy. Owner: owner. Next: settle #28 before the copy.
- Legal basis: consent under Art. 6(1)(a) GDPR, assumed and not confirmed.
  Owner: owner. Next: confirm in #154.
- `website-conventions` says "no dispute resolution".
  The owner rule of 2026-11-22 says there is one.
  Owner: maintainer. Next: skill update proposed separately.
- The govue issue number of `GovueWaitlistForm`. Owner: maintainer. Next: link it here.
- The passkey RP id configured for the app today. Owner: owner. Next: read it from the deployment.
- Issue numbers of the wordmark logo, AGB and AVV issues. Owner: maintainer. Next: link them here.
