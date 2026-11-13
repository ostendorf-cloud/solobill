import { render, screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import en from '../i18n/locales/en.json'
import { apiClient } from '../api'
import LoginView from './LoginView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

function renderLoginView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: LoginView },
    ],
  })
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

  return { router, ...render(LoginView, { global: { plugins: [router, i18n] } }) }
}

describe('LoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('submits the tenant, email, password and totp code to the login endpoint', async () => {
    vi.mocked(apiClient.post).mockResolvedValue(undefined)
    const user = userEvent.setup()
    const { router } = renderLoginView()

    await user.type(screen.getByLabelText(/Tenant/), 'acme')
    await user.type(screen.getByLabelText(/Email/), 'owner@example.com')
    await user.type(screen.getByLabelText(/Password/), 'super-secret')
    await user.type(screen.getByLabelText(/TOTP code/), '123456')
    await user.click(screen.getByRole('button', { name: 'Log in' }))

    expect(apiClient.post).toHaveBeenCalledWith('/auth/login', {
      tenant_slug: 'acme',
      email: 'owner@example.com',
      password: 'super-secret',
      totp_code: '123456',
    })
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('does not submit when a required field is left empty', async () => {
    const user = userEvent.setup()
    renderLoginView()

    await user.type(screen.getByLabelText(/Tenant/), 'acme')
    await user.click(screen.getByRole('button', { name: 'Log in' }))

    expect(apiClient.post).not.toHaveBeenCalled()
    expect(await screen.findByText('Please enter your email address.')).toBeVisible()
  })

  it('always renders the single sign-on link, regardless of whether OIDC is enabled', () => {
    renderLoginView()

    const link = screen.getByRole('link', { name: 'Log in with single sign-on' })
    expect(link).toHaveAttribute('href', '/auth/oidc/login')
  })
})
