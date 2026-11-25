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
- Every page says the product is in development. No capability is claimed as available.

## Non goals

- A signup, a login or any link into the app from the site.
- Prices, a checkout or a terms page.
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
- 2026-11-25: the owner sets SPF, DKIM and DMARC for `solobill.de` in the infrastructure repository.
- 2026-11-25: API host wish: `api.solobill.de` if it works, else `/api` on each host.

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
- Recommended: `/api` on each host, see "API host" below and [ADR 0003](../adr/0003-api-host.md).
- `solobill.de/api/` goes to `site-api`. `app.solobill.de/api/` goes to the app API.
- `/` redirects by language through govue. Legal routes and aliases come from govue.
- A sitemap for `https://solobill.de` comes from the prerender. Each language has a 404 page.

### Waitlist states

- `unconfirmed`: created by a request with consent. One confirmation mail, no advertising in it.
- `confirmed`: the confirm link was used within 48 hours. The consent proof is recorded.
- `unsubscribed`: the unsubscribe link was used. What stays depends on an open question below.
- `deleted`: the row is gone. Unconfirmed rows go after 48 hours, others per the retention answer.
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
- Confirmed and unsubscribed rows follow the retention answer below.

### Mail path

- `site-api` builds a plain text mail with govue `mail` and sends it by SMTP.
- The relay is the SMTP service of `ostendorf.tech`. Sender `noreply@solobill.de`.
- govue `mail` sends once, no queue, no retry. A failed send answers the request with an error.
- govue `mail` does no DKIM signing. The SMTP service has to sign. Not verified.
- A `List-Unsubscribe` header needs extra headers in govue `mail`. Not verified, see govue #267.

### Frontend

- `GovueHeader` with the language switch in link mode and the theme switch, `GovueFooter`.
- Light and dark mode. Formal German through govue's i18n.
- The waitlist form is govue's waitlist form with the site's consent text and privacy link.
- Result pages: check your inbox, confirmed, unsubscribed, link expired. Both languages.
- Umami analytics through govue `analytics`, cookie free.

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

### Recommendation

- Option B: `/api` on each host. Same origin for the app and for the site.
- It removes CORS, the trust lists and the OIDC redirect change from the app.
- It keeps the site independent: own host, own routes, no shared hostname with the app chart.
- It matches issue #149 and `ostendorf.tech`.
- Option A stays possible later. The cost is the condition list above, mostly configuration.
- The passkey RP id does not depend on this choice. Keep it the app host.
- The owner decides. Until then ADR 0003 is `proposed`.

### What each option changes

| Issue | Option A: `api.solobill.de` | Option B: `/api` per host |
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

## Open questions

- Operator of `solobill.de`: same business as `ostendorf.tech`? Name, address, VAT ID.
  Owner: owner. Next: answer in #154.
- Imprint "carries over": provider data only, with an own text? Owner: owner. Next: answer in #154.
- Imprint sections: dispute resolution wording, MStV paragraph 18 section 2.
  Owner: owner. Next: answer in #154.
- UWG paragraph 5: exact framing of "in development" and "planned". Owner: owner.
  Next: answer in #154.
- May the page link to a login or say anything is usable today? Owner: owner. Next: answer in #154.
- "Free during development": free for whom without self signup? Invite only? Price disclaimer?
  Owner: owner. Next: answer in #154.
- Legal basis: consent under Art. 6(1)(a) GDPR, assumed. Owner: owner. Next: confirm in #154.
- Retention of a confirmed entry after launch. Owner: owner. Next: answer in #154.
- Unsubscribe: delete, or keep a minimal proof? Owner: owner. Next: answer in #154.
- Proof of consent: request time, confirmation time, consent text version, IP address?
  Owner: owner. Next: answer in #154.
- Hosting facts: cluster location, provider, access logs and their retention.
  Owner: owner. Next: answer in #154.
- Is the SMTP provider a processor to name in the privacy text? Owner: owner. Next: answer in #154.
- Brand: logo and theme of the site. Reuse the seed of `web/src/theme/`?
  Owner: owner. Next: answer in #154.
- Domain: is `solobill.de` registered, is its DNS in the infrastructure repository?
  Owner: owner. Next: answer in #154.
- Is the name checked against trademarks? Owner: owner. Next: answer in #154.
- Issue #28 (audience, price) shapes the page copy. Owner: owner. Next: settle #28 before the copy.
- `website-conventions` says "no dispute resolution".
  The owner rule of 2026-11-22 says there is one.
  Owner: maintainer. Next: skill update proposed separately.
- API host: option A or B. Owner: owner. Next: decide on ADR 0003 in #154.
- The govue issue number of `GovueWaitlistForm`. Owner: maintainer. Next: link it here.
- The passkey RP id configured for the app today. Owner: owner. Next: read it from the deployment.
