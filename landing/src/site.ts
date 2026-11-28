import type { LegalPage } from '@ostendorf-cloud/govue'

/**
 * The origin of the site, for the canonical links, the hreflang alternates
 * and the sitemap. VITE_SITE_URL overrides it at build time, such as for a
 * preview. govue refuses a value that is not `https://host[:port]`.
 */
export const SITE_ORIGIN: string =
  (import.meta.env.VITE_SITE_URL as string | undefined) ?? 'https://solobill.de'

/** The app, which the login button of the header opens. */
export const APP_URL = 'https://app.solobill.de/'

/**
 * The legal pages of the site, in footer order. The site sells nothing yet,
 * so it has no terms: govue then serves neither /terms nor /agb.
 */
export const LEGAL: readonly LegalPage[] = ['imprint', 'privacy']
