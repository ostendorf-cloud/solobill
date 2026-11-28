import { createGovueI18n, localeFromPath } from '@ostendorf-cloud/govue'
import { createApp } from 'vue'

import { componentForPath } from './pages'
import { loadSiteConfig } from './siteConfig'
import { startTheme } from './theme'
import './styles.css'

// The page is prerendered, see entry-server.ts. The client mounts with
// createApp, which replaces the prerendered markup and never hydrates it:
// the markup was rendered with no visitor, so a stored dark mode or a
// browser of the other language would mismatch it.
startTheme()

const path = location.pathname
// The language comes from the URL prefix. A 404 page at a path without one
// takes govue's own detection: the stored choice, then the browser.
const locale = localeFromPath(path)

async function start(): Promise<void> {
  // The links and the registration switch come from the server's environment,
  // see siteConfig.ts. The prerendered page stays on screen meanwhile, so the
  // wait is a request to the same origin, and loadSiteConfig never rejects.
  await loadSiteConfig()

  const app = createApp(componentForPath(path))
  app.use(createGovueI18n(locale === undefined ? {} : { locale }))
  app.mount('#app')
}

void start()
