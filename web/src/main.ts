import { createApp } from 'vue'
import { createGovueI18n, useTheme } from '@ostendorf-cloud/govue'

import App from './App.vue'
import { createAppI18n } from './i18n'
import { createAppRouter } from './router'
import { followUrlLocale, initialLocale } from './router/locale'
import './styles.css'

// The theme never varies: this app ships exactly one brand (see
// src/theme/solobill.css). Mode starts at "system", the visitor's own
// choice only overrides it once a later step adds a toggle, matching
// useTheme's own documented default.
useTheme({ theme: 'solobill' })

// The language comes from the URL, see initialLocale.
const i18n = createAppI18n(initialLocale(location.pathname))
const router = createAppRouter()

// govue's components (login form, TOTP enrolment, passkey button) render
// their own texts through govue's language layer. Sharing the app's
// vue-i18n instance makes them follow the app's language instead of
// falling back to English, and keeps the app's own locale choice.
const govueI18n = createGovueI18n({ i18n })
followUrlLocale(router, govueI18n.language)

const app = createApp(App)
app.use(router)
app.use(i18n)
app.use(govueI18n)
app.mount('#app')
