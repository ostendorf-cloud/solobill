import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { de, en } from '../i18n/locales'
import { me } from '../session'
import CatalogueView from './CatalogueView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const item = {
  id: 'i-1',
  name: 'Consulting hour',
  description: null,
  sku: 'CONS-H',
  unit_price_cents: 9500,
  unit: 'hour',
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/catalogue', name: 'catalogue', component: CatalogueView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  return render(CatalogueView, { global: { plugins: [router, i18n, govueI18n] } })
}

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

describe('CatalogueView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useToasts().dismissAll()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('lists the items with the price written from cents', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    renderView()

    expect(await screen.findByRole('rowheader', { name: 'Consulting hour' })).toBeVisible()
    expect(screen.getByText('95.00 EUR')).toBeVisible()
    expect(screen.getByText('CONS-H')).toBeVisible()
    expect(apiClient.get).toHaveBeenCalledWith('/catalogue-items')
  })

  it('writes the German price with a comma', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    renderView('de')

    expect(await screen.findByText('95,00 EUR')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Katalog' })).toBeVisible()
  })

  it('shows the empty state', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [] })
    renderView()

    expect(await screen.findByText('No items yet')).toBeVisible()
  })

  it('shows the error state', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
    renderView()

    expect(await screen.findByRole('alert')).toHaveTextContent('The catalogue could not be loaded.')
  })

  it('creates an item and sends the price as integer cents', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({ items: [] })
    vi.mocked(apiClient.get).mockResolvedValueOnce({ items: [item] })
    vi.mocked(apiClient.post).mockResolvedValue(item)
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add item' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'Consulting hour')
    await user.type(within(dialog).getByLabelText('SKU'), 'CONS-H')
    await user.type(within(dialog).getByLabelText(/^Price per unit/), '95,5')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(apiClient.post).toHaveBeenCalledWith(
      '/catalogue-items',
      expect.objectContaining({
        name: 'Consulting hour',
        sku: 'CONS-H',
        description: null,
        unit_price_cents: 9550,
        unit: 'piece',
      }),
    )
    expect(await screen.findByRole('rowheader', { name: 'Consulting hour' })).toBeVisible()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('rejects a price that is no amount without a request', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [] })
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add item' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'X')
    await user.type(within(dialog).getByLabelText(/^Price per unit/), 'abc')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(
      await within(dialog).findByText(
        'Enter the amount without thousands separators, for example 1234.56.',
      ),
    ).toBeVisible()
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('puts API field errors on their fields', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    vi.mocked(apiClient.put).mockRejectedValue(
      apiError(400, ErrorCode.invalidArgument, {
        name: 'required',
        unit: 'must be piece, hour or day',
      }),
    )
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Consulting hour' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByDisplayValue('95.00')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(apiClient.put).toHaveBeenCalledWith(
      '/catalogue-items/i-1',
      expect.objectContaining({ unit_price_cents: 9500 }),
    )
    expect(await within(dialog).findByText('This field is required.')).toBeVisible()
    expect(within(dialog).getByText('Choose piece, hour or day.')).toBeVisible()
    expect(within(dialog).queryByText('must be piece, hour or day')).toBeNull()
  })

  it('translates the negative price reason', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    vi.mocked(apiClient.put).mockRejectedValue(
      apiError(400, ErrorCode.invalidArgument, { unit_price_cents: 'must not be negative' }),
    )
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Consulting hour' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText('The value must not be negative.')).toBeVisible()
  })

  it('requires a price instead of saving a blank one as zero', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [] })
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add item' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'X')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText('This field is required.')).toBeVisible()
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('fills the price with the German decimal comma', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [{ ...item, unit_price_cents: 9550 }] })
    const user = userEvent.setup()
    renderView('de')

    await user.click(await screen.findByRole('button', { name: 'Consulting hour bearbeiten' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByDisplayValue('95,50')).toBeVisible()
  })

  it('names a server failure without the marked fields text', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    vi.mocked(apiClient.put).mockRejectedValue(apiError(500, ErrorCode.internal))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Consulting hour' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(
      await within(dialog).findByText('The server had an error. Try again later.'),
    ).toBeVisible()
    expect(within(dialog).queryByText(/marked fields/)).toBeNull()
  })

  it('keeps save disabled until the shell loaded the session', async () => {
    me.value = null
    vi.mocked(apiClient.get).mockResolvedValue({ items: [item] })
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Edit Consulting hour' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('clears the shell session on a 401', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    renderView()

    await vi.waitFor(() => expect(me.value).toBeNull())
  })

  it('shows a duplicate sku from a 409 on the sku field', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ items: [] })
    vi.mocked(apiClient.post).mockRejectedValue(apiError(409, ErrorCode.conflict))
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Add item' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Name/), 'X')
    await user.type(within(dialog).getByLabelText('SKU'), 'CONS-H')
    await user.type(within(dialog).getByLabelText(/^Price per unit/), '10')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByText('This SKU is already in use.')).toBeVisible()
    expect(within(dialog).getByLabelText('SKU')).toHaveAttribute('aria-invalid', 'true')
  })

  it('deletes only after the confirm dialog', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({ items: [item] })
    vi.mocked(apiClient.get).mockResolvedValueOnce({ items: [] })
    vi.mocked(apiClient.del).mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Delete Consulting hour' }))
    const dialog = await screen.findByRole('dialog')
    expect(apiClient.del).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(apiClient.del).toHaveBeenCalledWith('/catalogue-items/i-1')
    expect(await screen.findByText('No items yet')).toBeVisible()
  })
})
