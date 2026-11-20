import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { render, screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { de, en } from '../i18n/locales'
import { apiClient, csrfToken } from '../api'
import SettingsView from './SettingsView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const profile = {
  legal_name: 'Example Trading',
  street: 'Main Street 1',
  address_line2: null,
  postal_code: '12345',
  city: 'Example City',
  country_code: 'DE',
  tax_number: null,
  vat_id: null,
  kleinunternehmer_id: 'KU-1',
  default_vat_regime: 'kleinunternehmer',
  default_language: 'de',
  bank_name: null,
  iban: null,
  bic: null,
  email: null,
  phone: null,
  website: null,
  footer_text: null,
  number_formats: { invoice: 'RE-{year}{seq:4}', offer: 'AN-{year}{seq:4}' },
  has_logo: false,
  issue_ready: { standard: false, kleinunternehmer: true },
  updated_at: '2026-11-20T10:00:00Z',
}

function mockApi(role: string, profileAnswer: unknown, periods: unknown[] = []) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/me') return Promise.resolve({ role, csrf_token: 'csrf-1' })
    if (path === '/vat-regimes') return Promise.resolve({ periods })
    if (path === '/profile') {
      return profileAnswer instanceof Error
        ? Promise.reject(profileAnswer)
        : Promise.resolve(profileAnswer)
    }
    return Promise.reject(new Error(`unexpected GET ${path}`))
  })
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/settings', name: 'settings', component: SettingsView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })

  return render(SettingsView, { global: { plugins: [router, i18n] } })
}

describe('SettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    csrfToken.value = undefined
  })

  it('fills the form from the saved profile and keeps the csrf token', async () => {
    mockApi('owner', profile)

    renderView()

    expect(await screen.findByDisplayValue('Example Trading')).toBeVisible()
    expect(screen.getByDisplayValue('KU-1')).toBeVisible()
    expect(screen.getByDisplayValue('RE-{year}{seq:4}')).toBeVisible()
    expect(csrfToken.value).toBe('csrf-1')
  })

  it('starts with defaults when no profile was saved yet', async () => {
    mockApi('owner', new ApiError({ code: ErrorCode.notFound, message: 'none', status: 404 }))

    renderView()

    expect(await screen.findByDisplayValue('AN-{year}{seq:4}')).toBeVisible()
  })

  it('shows the German strings', async () => {
    mockApi('owner', profile)

    renderView('de')

    expect(await screen.findByRole('heading', { name: 'Verkäuferprofil' })).toBeVisible()
  })

  it('does not load or show the profile form for a member', async () => {
    mockApi('member', profile)

    renderView()

    expect(await screen.findByText('Only the owner can change the seller profile.')).toBeVisible()
    expect(apiClient.get).not.toHaveBeenCalledWith('/profile')
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
  })

  it('saves through PUT /profile and maps a field error back to its input', async () => {
    mockApi('owner', profile)
    vi.mocked(apiClient.put).mockRejectedValue(
      new ApiError({
        code: ErrorCode.invalidArgument,
        message: 'invalid',
        status: 400,
        details: { iban: 'not a valid IBAN' },
      }),
    )
    const user = userEvent.setup()
    renderView()

    await screen.findByDisplayValue('Example Trading')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(apiClient.put).toHaveBeenCalledWith(
      '/profile',
      expect.objectContaining({ legal_name: 'Example Trading', iban: null }),
    )
    expect(await screen.findByText('not a valid IBAN')).toBeVisible()
  })

  it('shows the error under the number_formats key under both template fields', async () => {
    mockApi('owner', profile)
    vi.mocked(apiClient.put).mockRejectedValue(
      new ApiError({
        code: ErrorCode.invalidArgument,
        message: 'invalid',
        status: 400,
        details: { number_formats: 'the prefixes of invoice and offer overlap' },
      }),
    )
    const user = userEvent.setup()
    renderView()

    await screen.findByDisplayValue('Example Trading')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findAllByText('the prefixes of invoice and offer overlap')).toHaveLength(2)
    expect(screen.getByLabelText('Invoice number format')).toHaveAccessibleDescription(
      'the prefixes of invoice and offer overlap',
    )
    expect(screen.getByLabelText('Offer number format')).toHaveAccessibleDescription(
      'the prefixes of invoice and offer overlap',
    )
  })

  it('shows a field specific template error only under its own field', async () => {
    mockApi('owner', profile)
    vi.mocked(apiClient.put).mockRejectedValue(
      new ApiError({
        code: ErrorCode.invalidArgument,
        message: 'invalid',
        status: 400,
        details: { 'number_formats.offer': 'unknown token' },
      }),
    )
    const user = userEvent.setup()
    renderView()

    await screen.findByDisplayValue('Example Trading')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findAllByText('unknown token')).toHaveLength(1)
  })

  it('uses a date input for the start of a period', async () => {
    mockApi('owner', profile)

    renderView()

    const input = await screen.findByLabelText(/Valid from/)
    expect(input).toHaveAttribute('type', 'date')
  })

  it('strikes only the corrected period and not its label or the others', async () => {
    mockApi('owner', profile, [
      {
        id: 'p1',
        valid_from: '2026-01-01',
        regime: 'kleinunternehmer',
        note: null,
        supersedes_id: null,
        superseded: true,
      },
      {
        id: 'p2',
        valid_from: '2026-02-01',
        regime: 'standard',
        note: 'fix',
        supersedes_id: 'p1',
        superseded: false,
      },
    ])

    renderView()

    const struck = await screen.findByText(/2026-01-01/)
    expect(struck).toHaveClass('line-through')
    expect(struck.textContent).not.toContain('(corrected)')

    const label = screen.getByText('(corrected)')
    expect(label).not.toHaveClass('line-through')
    expect(label.closest('.line-through')).toBeNull()

    expect(screen.getByText(/2026-02-01/)).not.toHaveClass('line-through')
  })
})
