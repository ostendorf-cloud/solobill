import { render, screen } from '@testing-library/vue'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'

import { en } from '../i18n/locales'
import { withLocale } from '../router/locale'
import NotFoundView from './NotFoundView.vue'

const stub = { template: '<div />' }

async function renderAt(path: string, preferred: 'de' | 'en') {
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: withLocale(
      [{ path: '/', name: 'home', component: stub }],
      () => router,
      () => preferred,
    ),
  })
  // The view reads the remembered choice itself, like the app does.
  localStorage.setItem('govue-language', preferred)
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  await router.push(path)
  await router.isReady()
  render(NotFoundView, { global: { plugins: [router, i18n] } })
  return router
}

describe('NotFoundView', () => {
  it('renders on an unknown prefix and links home in the preferred language', async () => {
    const router = await renderAt('/fr/documents/7', 'de')
    expect(router.currentRoute.value.name).toBe('not-found-bare')
    const link = await screen.findByRole('link', { name: en.notFound.home })
    expect(link.getAttribute('href')).toBe('/de')
  })

  it('links home in the language of a known prefix', async () => {
    const router = await renderAt('/en/nothing/here', 'de')
    expect(router.currentRoute.value.name).toBe('not-found')
    const link = await screen.findByRole('link', { name: en.notFound.home })
    expect(link.getAttribute('href')).toBe('/en')
  })
})
