import { render, screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { en } from '../i18n/locales'
import { apiClient } from '../api'
import LoginView from './LoginView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

async function renderLoginView(path = '/login') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: LoginView },
    ],
  })
  await router.push(path)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

  return { router, ...render(LoginView, { global: { plugins: [router, i18n] } }) }
}

describe('LoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("submits govue's login, password and code to the tenant's login route", async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ login: 'owner@example.com' })
    const user = userEvent.setup()
    const { router } = await renderLoginView()

    await user.type(screen.getByLabelText(/Tenant/), 'acme')
    await user.type(screen.getByLabelText(/^Login/), 'owner@example.com')
    await user.type(screen.getByLabelText(/Password/), 'super-secret-passphrase')
    await user.type(screen.getByLabelText(/Code from your authenticator app/), '123456')
    await user.click(screen.getByRole('button', { name: 'Log in' }))

    expect(apiClient.post).toHaveBeenCalledWith(
      '/tenants/acme/auth/login',
      { login: 'owner@example.com', password: 'super-secret-passphrase', code: '123456' },
      { skipUnauthenticatedHook: true },
    )
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('home'))
  })

  it('takes the tenant from the query string', async () => {
    await renderLoginView('/login?tenant=acme')

    expect(screen.getByLabelText(/Tenant/)).toHaveValue('acme')
    expect(screen.getByLabelText(/Password/)).toBeEnabled()
  })

  it('keeps the login form disabled until a tenant is given', async () => {
    await renderLoginView()

    expect(screen.getByLabelText(/Password/)).toBeDisabled()
    expect(screen.getByText('Please enter the tenant.')).toBeVisible()
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('does not submit when a required field is left empty', async () => {
    const user = userEvent.setup()
    await renderLoginView('/login?tenant=acme')

    await user.type(screen.getByLabelText(/^Login/), 'owner@example.com')
    await user.click(screen.getByRole('button', { name: 'Log in' }))

    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('renders the single sign-on link when the backend reports OIDC', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ oidc: true })
    await renderLoginView()

    const link = await screen.findByRole('link', { name: 'Log in with single sign-on' })
    expect(link).toHaveAttribute('href', '/api/v1/auth/oidc/login')
    expect(apiClient.get).toHaveBeenCalledWith('/auth/methods', { skipUnauthenticatedHook: true })
  })

  it.each([
    [
      'the backend reports no OIDC',
      () => vi.mocked(apiClient.get).mockResolvedValue({ oidc: false }),
    ],
    [
      'the request fails',
      () => vi.mocked(apiClient.get).mockRejectedValue(new Error('network down')),
    ],
  ])('hides the single sign-on link when %s', async (_name, arrange) => {
    arrange()
    await renderLoginView()

    await vi.waitFor(() =>
      expect(apiClient.get).toHaveBeenCalledWith('/auth/methods', {
        skipUnauthenticatedHook: true,
      }),
    )
    await flushPromises()
    expect(
      screen.queryByRole('link', { name: 'Log in with single sign-on' }),
    ).not.toBeInTheDocument()
  })
})

// flushPromises lets the view's mount request settle before an absence is
// asserted, so a link that would appear late still fails the test.
async function flushPromises(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}
