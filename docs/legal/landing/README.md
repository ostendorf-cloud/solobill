# Landing site legal texts (draft)

Drafts of the imprint and the privacy policy of solobill.de, for issue 160.
They are review material for the owner and a lawyer. They are not legal advice.

## Files

- `imprint.de.md` and `imprint.en.md`: the imprint. The German text is binding.
- `privacy.de.md` and `privacy.en.md`: the privacy policy. The German text is binding.

## Why markdown under docs/

- The landing site (`landing/`, issues 154 and 155) does not exist on main yet.
- Wiring the texts into pages, routes and the footer is application code and is out of scope.
- When the site exists, its legal pages take these texts as the starting point, see
  `website-conventions`.

## Deviation from the issue

- Issue 160 and `docs/concepts/site.md` list a dispute resolution section in the imprint.
- The website conventions forbid it: no arbitration statement, no online dispute platform link.
- The draft omits it. Open question in `docs/concepts/site.md` already names the conflict.

## Facts taken from the repository or the issue

- Provider data (name, address, phone, VAT ID): `docs/concepts/site.md` and the ostendorf.tech
  imprint. The owner confirms they apply to solobill.de.
- Imprint mail `info@solobill.de`: `docs/concepts/site.md`.
- Waitlist rules (48 hours, immediate deletion on unsubscribe, 12 months after the launch mail,
  no IP address): `docs/concepts/site.md` and the govue `optin` package documentation.
- Umami, proof of work, theme key `govue-theme`, access log fields: govue v0.12.0 sources.

## Placeholders and open facts for the owner

- `[OPEN]` Hosting of solobill.de on the same own hardware as ostendorf.tech.
- `[OPEN]` Retention of server logs (ostendorf.tech: 30 days).
- `[OPEN]` Retention of Umami data (ostendorf.tech: 14 months).
- `[OPEN]` Name and operator of the SMTP service, and whether a data processing agreement is
  needed.
- `[OPEN]` Consent under Art. 6(1)(a) GDPR for the waitlist (assumed, not confirmed).
- `[OPEN]` Competent supervisory authority.
- `[OPEN]` Date of publication.

## Statements the code does not prove yet

- `[NOT VERIFIED]` The website uses no third party service. The code does not exist.
- `[NOT VERIFIED]` Fields of the website access log and of the ingress log.
- `[NOT VERIFIED]` Umami is wired through govue `analytics` as described.
- `[NOT VERIFIED]` The deletion of confirmed entries 12 months after the launch message. govue
  `optin` offers `DeleteConfirmedBefore`, but stores no launch time.
- `[NOT VERIFIED]` Interval of the purge job.
- `[NOT VERIFIED]` DKIM, SPF and DMARC for solobill.de.
- `[NOT VERIFIED]` TLS at the ingress.

## Checklist when the site is built

- Compare every statement in the privacy policy with the code of `landing/`.
- Remove each marker once the owner supplied the fact or the code proves it.
- Check that the footer links both pages on every page and that the routes redirect by locale.
