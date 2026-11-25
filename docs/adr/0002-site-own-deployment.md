# 0002 The solobill.de site is its own deployment

- Status: accepted
- Date: 2026-11-25
- Issues: #5, #154

## Context

- `solobill.de` needs public pages and a waitlist. The app at `app.solobill.de` is behind a login.
- A waitlist holds personal data of people who are not customers.
- govue's `frontend` serves prerendered pages and no API. A waitlist needs an API and a database.
- The app is released for the year end bookkeeping and must not wait for the site, or the reverse.

## Decision

- The site is its own deployment: chart `solobill-site` with a site frontend and a waitlist API.
- Decided by the owner on 2026-11-25.
- The site frontend is govue `frontend`, built from the top level folder `landing/`.
- The waitlist API has its own Postgres database and role, with no grant on app data.
- App and site share the repository and the release tag, and deploy separately.

## Consequences

- A site outage or rollout leaves the app untouched, and the reverse.
- A wrong grant cannot expose app data to the site, or waitlist addresses to the app.
- Two more images, a second chart, a second database: more release and Renovate config to keep.
- `site-api` runs one replica until govue has shared limiter and PoW stores.
- `landing/` is a second Vite project next to `web/`, with its own dependency lock.

## Alternatives

- Waitlist inside the app API: couples releases and puts addresses next to tenant data.
- Site pages inside the app SPA: one build for two audiences, and the public pages need prerender.
- One chart for app and site: one rollout for both, against the owner's decision.
- A hosted newsletter service: a third party processor with its own consent flow.

## Related

- Concept: [docs/concepts/site.md](../concepts/site.md).
- [ADR 0003](0003-api-host.md): where the APIs are served.
