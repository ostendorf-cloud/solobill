import { ApiError, createGovueI18n } from '@ostendorf-cloud/govue'
import { render, screen } from '@testing-library/vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from './api'
import { en } from './i18n/locales'
import App from './App.vue'
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
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
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
  })
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  await router.push(path)
  await router.isReady()
  const govueI18n = createGovueI18n({ i18n })
  render(App, { global: { plugins: [router, i18n, govueI18n] } })
}

describe('App', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('renders the skip link ahead of the current route view', async () => {
    mockApi('owner', status(true))
    await renderApp('/')

    expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveAttribute('href', '#main')
    expect(await screen.findByText('page')).toBeVisible()
  })

  it('lists the navigation entries from route meta, owner only ones for the owner', async () => {
    mockApi('owner', status(true))
    await renderApp('/')

    expect(await screen.findByRole('link', { name: 'Customers' })).toHaveAttribute(
      'href',
      '/customers',
    )
    expect(screen.getByRole('link', { name: 'Settings' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible()
  })

  it('hides an owner only entry from a member', async () => {
    mockApi('member', status(true))
    await renderApp('/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByRole('link', { name: 'Settings' })).toBeNull()
  })

  it('shows the GoBD banner while the protection is inactive', async () => {
    mockApi('owner', status(false))
    await renderApp('/')

    expect(await screen.findByTestId('gobd-banner')).toBeVisible()
  })

  it('shows no banner when the protection is active', async () => {
    mockApi('owner', status(true))
    await renderApp('/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByTestId('gobd-banner')).toBeNull()
  })

  it('shows no banner when the status request fails', async () => {
    mockApi(
      'owner',
      new ApiError({ code: 'not_implemented', message: 'not implemented', status: 501 }),
    )
    await renderApp('/')

    await screen.findByRole('link', { name: 'Customers' })
    expect(screen.queryByTestId('gobd-banner')).toBeNull()
  })

  it('renders no header on the login route and asks for no session', async () => {
    mockApi('owner', status(false))
    await renderApp('/login')

    expect(screen.queryByRole('navigation')).toBeNull()
    expect(apiClient.get).not.toHaveBeenCalled()
  })
})
