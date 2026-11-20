import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { render, screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { en } from '../i18n/locales'
import { apiClient, csrfToken } from '../api'
import HomeView from './HomeView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const profile = {
  email: 'owner@example.com',
  name: 'Owner',
  role: 'owner',
  tenant_id: 'tenant-1',
  csrf_token: 'csrf-abc',
}

function renderHomeView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: HomeView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

  return { router, ...render(HomeView, { global: { plugins: [router, i18n] } }) }
}

describe('HomeView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    csrfToken.value = undefined
  })

  it('shows the profile GET /api/v1/me answers with and stores its csrf token', async () => {
    vi.mocked(apiClient.get).mockResolvedValue(profile)

    renderHomeView()

    expect(await screen.findByText('owner@example.com')).toBeVisible()
    expect(screen.getByText('Owner')).toBeVisible()
    expect(screen.getByText('owner')).toBeVisible()
    expect(screen.getByText('tenant-1')).toBeVisible()
    expect(csrfToken.value).toBe('csrf-abc')
  })

  it('logs out through a csrf-protected POST /api/v1/auth/logout when the button is clicked', async () => {
    vi.mocked(apiClient.get).mockResolvedValue(profile)
    vi.mocked(apiClient.post).mockResolvedValue(undefined)
    const user = userEvent.setup()
    const { router } = renderHomeView()

    await screen.findByText('owner@example.com')
    await user.click(screen.getByRole('button', { name: 'Log out' }))

    expect(apiClient.post).toHaveBeenCalledWith('/auth/logout')
    expect(router.currentRoute.value.name).toBe('login')
    expect(csrfToken.value).toBeUndefined()
  })

  it('redirects to login when the session is not authenticated', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(
      new ApiError({ code: ErrorCode.unauthenticated, message: 'log in first', status: 401 }),
    )
    const { router } = renderHomeView()

    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })
})
