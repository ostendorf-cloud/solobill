import { createApp } from 'vue'
import { useTheme } from '@ostendorf-cloud/govue'

import App from './App.vue'
import { createAppI18n, detectPreferredLocale, isLocale, type Locale } from './i18n'
import { createAppRouter } from './router'
import './styles.css'

// The theme never varies: this app ships exactly one brand (see
// src/theme/solobill.css). Mode starts at "system", the visitor's own
// choice only overrides it once a later step adds a toggle, matching
// useTheme's own documented default.
useTheme({ theme: 'solobill' })

function readStoredLocale(): Locale | undefined {
  try {
    const stored = localStorage.getItem('solobill-locale')
    return stored && isLocale(stored) ? stored : undefined
  } catch {
    // A private window, cleared site data, or storage that is blocked.
    return undefined
  }
}

const initialLocale =
  readStoredLocale() ?? detectPreferredLocale(navigator.languages ?? [navigator.language])

const i18n = createAppI18n(initialLocale)
const router = createAppRouter()

const app = createApp(App)
app.use(router)
app.use(i18n)
app.mount('#app')
