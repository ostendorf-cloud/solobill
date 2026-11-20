import { render, screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { en } from '../i18n/locales'
import { apiClient } from '../api'
import AcceptInviteView from './AcceptInviteView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const totpSecretResponse = {
  secret: 'SECRET123',
  provisioning_uri: 'otpauth://totp/solobill:invitee?secret=SECRET123&issuer=solobill',
}

async function renderAcceptInviteView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      { path: '/accept-invite', name: 'accept-invite', component: AcceptInviteView },
    ],
  })
  await router.push('/accept-invite?token=invite-token&tenant=acme')
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

  return { router, ...render(AcceptInviteView, { global: { plugins: [router, i18n] } }) }
}

describe('AcceptInviteView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('requests a totp secret on mount and shows it next to its provisioning uri', async () => {
    vi.mocked(apiClient.post).mockResolvedValue(totpSecretResponse)

    await renderAcceptInviteView()

    expect(await screen.findByText('SECRET123')).toBeVisible()
    expect(screen.getByText(totpSecretResponse.provisioning_uri)).toBeVisible()
    expect(apiClient.post).toHaveBeenCalledWith('/invites/totp-secret', {
      tenant_slug: 'acme',
      token: 'invite-token',
    })
  })

  it('shows an error and never requests a secret when the route carries no tenant or token', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', name: 'home', component: { template: '<div />' } },
        { path: '/accept-invite', name: 'accept-invite', component: AcceptInviteView },
      ],
    })
    await router.push('/accept-invite')
    await router.isReady()
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

    render(AcceptInviteView, { global: { plugins: [router, i18n] } })

    expect(await screen.findByRole('alert')).toBeVisible()
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('submits the tenant, token, password and totp code to the accept endpoint, without a secret', async () => {
    vi.mocked(apiClient.post)
      .mockResolvedValueOnce(totpSecretResponse)
      .mockResolvedValueOnce(undefined)
    const user = userEvent.setup()
    const { router } = await renderAcceptInviteView()

    await screen.findByText('SECRET123')
    await user.type(screen.getByLabelText(/Password/), 'new-password')
    await user.type(screen.getByLabelText(/TOTP code/), '654321')
    await user.click(screen.getByRole('button', { name: 'Accept invite' }))

    expect(apiClient.post).toHaveBeenCalledWith('/invites/accept', {
      tenant_slug: 'acme',
      token: 'invite-token',
      password: 'new-password',
      totp_code: '654321',
    })
    expect(router.currentRoute.value.name).toBe('home')
  })
})
