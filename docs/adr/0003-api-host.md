# 0003 The APIs are served under /api on each host

- Status: proposed
- Date: 2026-11-25
- Issues: #149, #151, #153, #154

## Context

- The app is `app.solobill.de`, the site is `solobill.de`. Both are the one site `solobill.de`.
- The session cookie is `__Host-session`: host only, no `Domain`, `SameSite` Lax or Strict.
- A cross origin write needs a trusted origin in the CSRF check, CORS with credentials, and both
  trust lists of the account and passkey handlers.
- The OIDC callback redirects to `Success` or to a local path on the callback host only.
- The owner prefers one API host `api.solobill.de`, with `/api` per host as the fallback.

## Decision

- Each host serves its own API under `/api`: the app API on `app.solobill.de`, the waitlist on
  `solobill.de`.
- Same origin needs no CORS, no trusted origins and no change to the OIDC redirect target.
- It keeps the site and the app on separate hostnames, so each chart owns its routes.
- It is the shape issue #149 already plans, and the shape of `ostendorf.tech`.

## Consequences

- No single API host for a later MCP server or native app. They use `app.solobill.de/api`.
- The ingress of each chart routes `/api` itself. A mistake there breaks the frontend paths.
- The passkey RP id is not affected and stays the app host.
- Moving to `api.solobill.de` later costs the condition list in the concept, mostly configuration.

## Alternatives

- One API host `api.solobill.de`. It works with govue v0.5.0, but needs `ALLOWED_ORIGINS`, an
  absolute client `baseUrl` with `credentials: 'include'`, a CSP `connect-src` entry, an absolute
  OIDC `Success`, and loses deep links through OIDC `next`. Each write gets a preflight. Two
  charts share one hostname.
- A shared cookie on `.solobill.de`: govue's session sets no `Domain` and `__Host-` forbids one.

## Related

- Concept: [docs/concepts/site.md](../concepts/site.md), section "API host" with the evidence.
- [ADR 0002](0002-site-own-deployment.md): the site is its own deployment.
