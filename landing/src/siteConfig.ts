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
 * defaults. They also apply when the request for the configuration fails, and
 * to the prerendered pages.
 */
export const DEFAULT_CONFIG: Readonly<SiteConfig> = {
  loginUrl: 'https://app.solobill.de/login',
  registerUrl: 'https://app.solobill.de/register',
  registrationEnabled: true,
}

/** How long the start waits for the configuration before it takes the defaults. */
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
 * rejects: a failed, slow or malformed answer leaves the defaults, which work
 * without any server configuration. `cache: 'no-cache'` makes the browser
 * revalidate by the ETag, so a changed environment shows at the next visit.
 */
export async function loadSiteConfig(fetcher: typeof fetch = fetch): Promise<void> {
  let config = DEFAULT_CONFIG
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
    // The defaults stay.
  }
  Object.assign(siteConfig, config, { loaded: true })
}
