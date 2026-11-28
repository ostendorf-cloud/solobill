import { reactive } from 'vue'

/**
 * The runtime configuration of the site: where the login and the register
 * buttons lead, and whether registration is open at all. The pages are built
 * once and served in every environment, so the values cannot be baked in.
 * cmd/site-frontend serves them at /config.json from its environment
 * (SITE_LOGIN_URL, SITE_REGISTER_URL, SITE_REGISTRATION_ENABLED), and
 * main.ts loads them before it mounts. The key names are the JSON of
 * internal/siteserver's Links.
 */
export interface SiteConfig {
  loginUrl: string
  registerUrl: string
  registrationEnabled: boolean
}

/** Where the configuration is served. */
export const CONFIG_PATH = '/config.json'

/**
 * The values of an unset environment, the same as internal/siteserver's
 * defaults. They also apply to the prerendered pages. A failed request for the
 * configuration takes them except for the registration switch, see FAILED_CONFIG.
 */
export const DEFAULT_CONFIG: Readonly<SiteConfig> = {
  loginUrl: 'https://app.solobill.de/login',
  registerUrl: 'https://app.solobill.de/register',
  registrationEnabled: true,
}

/**
 * What a visitor gets when the configuration cannot be loaded: the default
 * URLs, with the registration closed. The server may have turned the
 * registration off, and a button that cannot be checked must not appear, so
 * the page fails closed. The login link stays, it leads to the default login.
 */
const FAILED_CONFIG: Readonly<SiteConfig> = {
  ...DEFAULT_CONFIG,
  registrationEnabled: false,
}

/** How long the start waits for the configuration before it gives up. */
const TIMEOUT_MS = 2000

/**
 * The configuration the components read. `loaded` is false in the prerendered
 * markup and until main.ts has the answer: a button that depends on the
 * configuration is then reserved but not shown, so it neither flashes nor
 * moves the page.
 */
export const siteConfig = reactive<SiteConfig & { loaded: boolean }>({
  ...DEFAULT_CONFIG,
  loaded: false,
})

/** An absolute http or https URL: the only kind a button may lead to. */
function isWebUrl(value: unknown): value is string {
  if (typeof value !== 'string') {
    return false
  }
  try {
    const { protocol, host } = new URL(value)
    return (protocol === 'http:' || protocol === 'https:') && host !== ''
  } catch {
    return false
  }
}

/**
 * The configuration of a response body. A field that is missing or of the
 * wrong kind takes its default, and a URL that is not http or https is never
 * taken, so a bad response cannot put a `javascript:` link on the page.
 */
export function parseSiteConfig(raw: unknown): SiteConfig {
  const body = typeof raw === 'object' && raw !== null ? (raw as Record<string, unknown>) : {}
  return {
    loginUrl: isWebUrl(body.loginUrl) ? body.loginUrl : DEFAULT_CONFIG.loginUrl,
    registerUrl: isWebUrl(body.registerUrl) ? body.registerUrl : DEFAULT_CONFIG.registerUrl,
    registrationEnabled:
      typeof body.registrationEnabled === 'boolean'
        ? body.registrationEnabled
        : DEFAULT_CONFIG.registrationEnabled,
  }
}

/**
 * Fetches the configuration into siteConfig and marks it loaded. It never
 * rejects: a failed, slow, non-OK or malformed answer takes FAILED_CONFIG, the
 * default URLs with the register button hidden. `cache: 'no-cache'` makes the
 * browser revalidate by the ETag, so a changed environment shows at the next
 * visit.
 */
export async function loadSiteConfig(fetcher: typeof fetch = fetch): Promise<void> {
  let config = FAILED_CONFIG
  try {
    const response = await fetcher(CONFIG_PATH, {
      cache: 'no-cache',
      headers: { Accept: 'application/json' },
      signal: AbortSignal.timeout(TIMEOUT_MS),
    })
    if (response.ok) {
      config = parseSiteConfig(await response.json())
    }
  } catch {
    // The failed configuration stays.
  }
  Object.assign(siteConfig, config, { loaded: true })
}
