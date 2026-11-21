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
  secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP',
  provisioning_uri:
    'otpauth://totp/solobill:invitee@example.com?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=solobill',
  email: 'invitee@example.com',
}

async function renderAcceptInviteView(path = '/accept-invite?token=invite-token&tenant=acme') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      { path: '/accept-invite', name: 'accept-invite', component: AcceptInviteView },
    ],
  })
  await router.push(path)
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

    expect(await screen.findByDisplayValue(totpSecretResponse.secret)).toBeVisible()
    expect(screen.getByDisplayValue(totpSecretResponse.provisioning_uri)).toBeVisible()
    expect(apiClient.post).toHaveBeenCalledWith('/invites/totp-secret', {
      tenant_slug: 'acme',
      token: 'invite-token',
    })
  })

  it('shows an error and never requests a secret when the route carries no tenant or token', async () => {
    await renderAcceptInviteView('/accept-invite')

    expect(await screen.findByRole('alert')).toBeVisible()
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it("confirms with govue's login, password and code, never with the secret or the token", async () => {
    vi.mocked(apiClient.post)
      .mockResolvedValueOnce(totpSecretResponse)
      .mockResolvedValueOnce({ login: 'invitee@example.com' })
    const user = userEvent.setup()
    const { router } = await renderAcceptInviteView()

    await screen.findByDisplayValue(totpSecretResponse.secret)
    await user.type(screen.getByLabelText(/Password/), 'a-new-long-password')
    await user.type(screen.getByLabelText(/Code from your authenticator app/), '654321')
    await user.click(screen.getByRole('button', { name: 'Confirm code' }))

    expect(apiClient.post).toHaveBeenLastCalledWith(
      '/invites/confirm',
      { login: 'invitee@example.com', password: 'a-new-long-password', code: '654321' },
      { skipUnauthenticatedHook: true },
    )
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('home'))
  })
})
