import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { de, en } from '../i18n/locales'
import CustomersView from './CustomersView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const customer = {
  id: 'c-1',
  name: 'Customer Ltd',
  number: 'K-0042',
  email: null,
  vat_id: null,
  street: 'Side Street 2',
  address_line2: 'Floor 3',
  postal_code: '54321',
  city: 'Other City',
  country_code: 'DE',
  document_language: 'de',
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/customers', name: 'customers', component: CustomersView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  return render(CustomersView, { global: { plugins: [router, i18n, govueI18n] } })
}

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

describe('CustomersView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useToasts().dismissAll()
  })

  it('lists the customers', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    renderView()

    expect(await screen.findByRole('rowheader', { name: 'Customer Ltd' })).toBeVisible()
    expect(screen.getByText('K-0042')).toBeVisible()
    expect(screen.getByText('Other City')).toBeVisible()
    expect(apiClient.get).toHaveBeenCalledWith('/customers')
  })

  it('shows the German strings', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    renderView('de')

    expect(await screen.findByRole('heading', { name: 'Kunden' })).toBeVisible()
    expect(await screen.findByRole('button', { name: 'Kunde anlegen' })).toBeVisible()
  })

  it('shows the empty state with a create action', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [] })
    renderView()

    expect(await screen.findByText('No customers yet')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Add customer' })).toBeVisible()
  })

  it('shows the error state and retries', async () => {
    vi.mocked(apiClient.get).mockRejectedValueOnce(apiError(500, ErrorCode.internal))
    vi.mocked(apiClient.get).mockResolvedValueOnce({ customers: [customer] })
    const user = userEvent.setup()
    renderView()

    expect(await screen.findByRole('alert')).toHaveTextContent('The customers could not be loaded.')
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('rowheader', { name: 'Customer Ltd' })).toBeVisible()
  })

  it('creates a customer and reloads the list', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({ customers: [] })
    vi.mocked(apiClient.get).mockResolvedValueOnce({ customers: [customer] })
    vi.mocked(apiClient.post).mockResolvedValue(customer)
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add customer' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'Customer Ltd')
    await user.type(within(dialog).getByLabelText('Customer number'), 'K-0042')
    await user.type(within(dialog).getByLabelText('Address line 2'), 'Floor 3')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(apiClient.post).toHaveBeenCalledWith(
      '/customers',
      expect.objectContaining({
        name: 'Customer Ltd',
        number: 'K-0042',
        address_line2: 'Floor 3',
        email: null,
        document_language: 'de',
      }),
    )
    expect(await screen.findByRole('rowheader', { name: 'Customer Ltd' })).toBeVisible()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(useToasts().toasts.value.map((t) => t.title)).toContain('The customer is saved.')
  })

  it('puts API field errors on their fields and keeps the dialog open', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.put).mockRejectedValue(
      apiError(400, ErrorCode.invalidArgument, {
        name: 'required',
        country_code: 'must be an ISO 3166-1 alpha-2 code',
      }),
    )
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByDisplayValue('K-0042')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(apiClient.put).toHaveBeenCalledWith('/customers/c-1', expect.any(Object))
    const countryField = within(dialog).getByLabelText(/^Country code/)
    expect(await within(dialog).findByText('must be an ISO 3166-1 alpha-2 code')).toBeVisible()
    expect(countryField).toHaveAttribute('aria-invalid', 'true')
    expect(within(dialog).getByText('required')).toBeVisible()
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('shows a duplicate number from a 409 on the number field', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [] })
    vi.mocked(apiClient.post).mockRejectedValue(apiError(409, ErrorCode.conflict))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add customer' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'Customer Ltd')
    await user.type(within(dialog).getByLabelText('Customer number'), 'K-0042')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText('This customer number is already in use.')).toBeVisible()
    expect(within(dialog).getByLabelText('Customer number')).toHaveAttribute('aria-invalid', 'true')
  })

  it('deletes only after the confirm dialog', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({ customers: [customer] })
    vi.mocked(apiClient.get).mockResolvedValueOnce({ customers: [] })
    vi.mocked(apiClient.del).mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Delete Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete this customer?')).toBeVisible()
    expect(apiClient.del).not.toHaveBeenCalled()

    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(apiClient.del).toHaveBeenCalledWith('/customers/c-1')
    expect(await screen.findByText('No customers yet')).toBeVisible()
  })

  it('does not delete when the confirm dialog is cancelled', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Delete Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(apiClient.del).not.toHaveBeenCalled()
    expect(screen.getByRole('rowheader', { name: 'Customer Ltd' })).toBeVisible()
  })
})
