import { ApiError, createGovueI18n } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'

import { apiClient } from './api'
import { de, en } from './i18n/locales'
import App from './App.vue'
import { followUrlLocale, withLocale } from './router/locale'
import { clearSession, me } from './session'
import type { StatusResponse } from './status'

vi.mock('./api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const stub = { template: '<main id="main">page</main>' }

function status(active: boolean): StatusResponse {
  return {
    object_store: {
      reachable: true,
      versioning: active,
      object_lock: active,
      require_versioning: true,
      file_routes_available: active,
      gobd_protection_active: active,
      checked_at: '2026-12-01T10:00:00Z',
    },
  }
}

function mockApi(role: string, statusAnswer: StatusResponse | Error) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/me') {
      return Promise.resolve({ email: 'a@b.test', name: 'Ada', role, csrf_token: 'csrf' })
    }
    if (path === '/status') {
      return statusAnswer instanceof Error
        ? Promise.reject(statusAnswer)
        : Promise.resolve(statusAnswer)
    }
    return Promise.reject(new Error(`unexpected GET ${path}`))
  })
}

async function renderApp(path: string) {
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: withLocale(
      [
        { path: '/', name: 'home', component: stub },
        { path: '/login', name: 'login', component: stub },
        {
          path: '/customers',
          name: 'customers',
          component: stub,
          meta: { nav: { labelKey: 'customers.nav', order: 30 } },
        },
        {
          path: '/settings',
          name: 'settings',
          component: stub,
          meta: { nav: { labelKey: 'profile.nav', order: 90, roles: ['owner'] } },
        },
      ],
      () => router,
      () => 'en',
    ),
  })
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  followUrlLocale(router, govueI18n.language)
  await router.push(path)
  await router.isReady()
  render(App, { global: { plugins: [router, i18n, govueI18n] } })
  return { router, i18n }
}

describe('App', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.restoreAllMocks()
    clearSession()
    localStorage.clear()
  })

  it('switches the language by the prefix, keeps path and query, and remembers it', async () => {
    mockApi('owner', status(true))
    const user = userEvent.setup()
    const { router, i18n } = await renderApp('/en/customers?tenant=acme#top')
    const group = await screen.findByRole('group', { name: 'Language' })
    expect(within(group).getByRole('link', { name: 'Deutsch' })).toHaveAttribute(
      'href',
      '/de/customers?tenant=acme#top',
    )

    await user.click(within(group).getByRole('link', { name: 'Deutsch' }))

    await vi.waitFor(() =>
      expect(router.currentRoute.value.fullPath).toBe('/de/customers?tenant=acme#top'),
    )
    expect(i18n.global.locale.value).toBe('de')
    expect(document.documentElement.lang).toBe('de')
    expect(localStorage.getItem('govue-language')).toBe('de')
  })

  it('follows the prefix of a link into the other language', async () => {
    mockApi('owner', status(true))
    const { router, i18n } = await renderApp('/en/')

    await router.push('/de/customers')

    expect(i18n.global.locale.value).toBe('de')
    expect(document.documentElement.lang).toBe('de')
  })

  it('keeps the app language on an unknown prefix and renders the 404 view', async () => {
    mockApi('owner', status(true))
    const { router, i18n } = await renderApp('/de/customers')

    await router.push('/fr/documents/7')

    expect(router.currentRoute.value.name).toBe('not-found-bare')
    expect(i18n.global.locale.value).toBe('de')
    expect(await screen.findByRole('link', { name: 'Zur Startseite' })).toHaveAttribute(
      'href',
      '/de',
    )
    expect(screen.getByRole('link', { name: 'Kunden' })).toHaveAttribute('href', '/de/customers')
  })

  it('signs out through the shell button and drops the header', async () => {
    mockApi('owner', status(true))
    vi.mocked(apiClient.post).mockResolvedValue(undefined)
    const user = userEvent.setup()
    const { router } = await renderApp('/en/')

    await user.click(await screen.findByRole('button', { name: 'Sign out' }))

    expect(apiClient.post).toHaveBeenCalledWith('/auth/logout')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    expect(screen.queryByRole('navigation')).toBeNull()
    expect(me.value).toBeNull()
  })

  it('shows no stale user after another sign out and a different sign in', async () => {
    mockApi('owner', status(false))
    const { router } = await renderApp('/en/')
    expect(await screen.findByRole('link', { name: 'Settings' })).toBeVisible()
    expect(await screen.findByTestId('gobd-banner')).toBeVisible()

    // A view's own sign out ends the session without the shell's button.
    await router.push('/en/login')
    await vi.waitFor(() => expect(me.value).toBeNull())
    expect(screen.queryByRole('link', { name: 'Settings' })).toBeNull()

    mockApi('member', status(true))
    await router.push('/en/')

    expect(await screen.findByRole('link', { name: 'Customers' })).toBeVisible()
    expect(screen.queryByRole('link', { name: 'Settings' })).toBeNull()
    expect(screen.queryByTestId('gobd-banner')).toBeNull()
    expect(screen.getByText('Signed in as Ada')).toBeInTheDocument()
  })

  it('renders the skip link ahead of the current route view', async () => {
    mockApi('owner', status(true))
    await renderApp('/en/')

    expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveAttribute('href', '#main')
    expect(await screen.findByText('page')).toBeVisible()
  })

  it('lists the navigation entries from route meta, owner only ones for the owner', async () => {
    mockApi('owner', status(true))
    await renderApp('/en/')

    expect(await screen.findByRole('link', { name: 'Customers' })).toHaveAttribute(
      'href',
      '/en/customers',
    )
    expect(screen.getByRole('link', { name: 'Settings' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible()
  })

  it('hides an owner only entry from a member', async () => {
    mockApi('member', status(true))
    await renderApp('/en/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByRole('link', { name: 'Settings' })).toBeNull()
  })

  it('shows the GoBD banner while the protection is inactive', async () => {
    mockApi('owner', status(false))
    await renderApp('/en/')

    expect(await screen.findByTestId('gobd-banner')).toBeVisible()
  })

  it('shows no banner when the protection is active', async () => {
    mockApi('owner', status(true))
    await renderApp('/en/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByTestId('gobd-banner')).toBeNull()
  })

  it('shows no banner when the status request fails', async () => {
    mockApi(
      'owner',
      new ApiError({ code: 'not_implemented', message: 'not implemented', status: 501 }),
    )
    await renderApp('/en/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByTestId('gobd-banner')).toBeNull()
  })

  it('renders no header on the login route and asks for no session', async () => {
    mockApi('owner', status(false))
    await renderApp('/en/login')

    expect(screen.queryByRole('navigation')).toBeNull()
    expect(apiClient.get).not.toHaveBeenCalled()
  })
})
