import { render, screen } from '@testing-library/vue'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import en from './i18n/locales/en.json'
import App from './App.vue'

describe('App', () => {
  it('renders the skip link ahead of the current route view', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', name: 'home', component: { template: '<p>home</p>' } }],
    })
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
    await router.push('/')
    await router.isReady()

    render(App, { global: { plugins: [router, i18n] } })

    expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveAttribute('href', '#main')
    expect(await screen.findByText('home')).toBeVisible()
  })
})
