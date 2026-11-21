import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { de, en } from '../i18n/locales'
import { clearSession, me } from '../session'
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
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
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
    expect(
      await within(dialog).findByText('Enter a two letter country code, for example DE.'),
    ).toBeVisible()
    expect(countryField).toHaveAttribute('aria-invalid', 'true')
    expect(within(dialog).getByText('This field is required.')).toBeVisible()
    expect(within(dialog).queryByText('required')).toBeNull()
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

  it('edits through PUT with the changed name and blank optionals as null', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.put).mockResolvedValue(customer)
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByLabelText(/^Name/)
    await user.clear(name)
    await user.type(name, 'Renamed Ltd')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(apiClient.put).toHaveBeenCalledWith(
      '/customers/c-1',
      expect.objectContaining({ name: 'Renamed Ltd', number: 'K-0042', email: null }),
    )
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('shows a generic text for an unknown reason', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.put).mockRejectedValue(
      apiError(400, ErrorCode.invalidArgument, { city: 'something new' }),
    )
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText('Invalid value.')).toBeVisible()
    expect(within(dialog).queryByText('something new')).toBeNull()
  })

  it.each([
    [403, ErrorCode.permissionDenied, 'You do not have permission for this.'],
    [500, ErrorCode.internal, 'The server had an error. Try again later.'],
    [0, ErrorCode.network, 'The server cannot be reached. Check the connection.'],
  ])('names a %i failure without the marked fields text', async (status, code, text) => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.put).mockRejectedValue(apiError(status, code))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText(text)).toBeVisible()
    expect(within(dialog).queryByText(/marked fields/)).toBeNull()
  })

  function mockListAndMe(meAnswer: 'ok' | 'fail') {
    vi.mocked(apiClient.get).mockImplementation((path: string) => {
      if (path === '/me') {
        return meAnswer === 'ok'
          ? Promise.resolve({ email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' })
          : Promise.reject(apiError(503, ErrorCode.unavailable))
      }
      return Promise.resolve({ customers: [customer] })
    })
  }

  it('keeps save disabled and says why when /me failed, after one retry', async () => {
    me.value = null
    mockListAndMe('fail')
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')

    expect(await within(dialog).findByText(/The session could not be loaded/)).toBeVisible()
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(apiClient.get).toHaveBeenCalledWith('/me')
  })

  it('enables save when the retry of /me succeeds', async () => {
    me.value = null
    mockListAndMe('ok')
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')

    await vi.waitFor(() =>
      expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled(),
    )
  })

  it('does not send a delete without a session', async () => {
    me.value = null
    mockListAndMe('fail')
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Delete Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(apiClient.del).not.toHaveBeenCalled()
    expect(useToasts().toasts.value.map((x) => x.title)).toContainEqual(
      expect.stringContaining('The session could not be loaded'),
    )
  })

  it('clears the shell session on a 401 during save', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.put).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    expect(me.value).not.toBeNull()
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await vi.waitFor(() => expect(me.value).toBeNull())
  })

  it('clears the shell session on a 401 during delete', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ customers: [customer] })
    vi.mocked(apiClient.del).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Delete Customer Ltd' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await vi.waitFor(() => expect(me.value).toBeNull())
    expect(useToasts().toasts.value).toHaveLength(0)
  })

  it('clears the shell session on a 401', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    renderView()

    await vi.waitFor(() => expect(me.value).toBeNull())
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
