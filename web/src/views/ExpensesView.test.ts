import { ApiError, ErrorCode, createGovueI18n } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { de, en } from '../i18n/locales'
import { clearSession, me } from '../session'
import ExpensesView from './ExpensesView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const expense = {
  id: 'e-1',
  state: 'booked',
  paid_on: '2026-11-18',
  receipt_date: '2026-11-17',
  supplier: 'Office Supplies Inc',
  description: 'Paper',
  gross_cents: 595000,
  net_cents: 500000,
  vat_rate_permyriad: 1900,
  vat_cents: 95000,
  category: 'euer.z27',
  file_id: null,
  is_asset: false,
  storno_of_id: null,
  imported: false,
  booked_at: '2026-11-19T10:00:00Z',
}
const draft = { ...expense, id: 'e-2', state: 'draft', supplier: 'Draft Shop', booked_at: null }
const storno = {
  ...expense,
  id: 'e-3',
  supplier: 'Office Supplies Inc',
  gross_cents: -595000,
  net_cents: -500000,
  storno_of_id: 'e-1',
}

const categories = [
  { key: 'euer.z27', kind: 'expense', label_de: 'Waren und Hilfsstoffe', label_en: 'Goods' },
  { key: 'euer.z51', kind: 'expense', label_de: 'Bürobedarf', label_en: 'Office' },
  { key: 'euer.z14', kind: 'income', label_de: 'Sonstige Einnahmen', label_en: 'Other' },
]

function answer(list: unknown[], gross = 595000, net = 500000) {
  return { expenses: list, total_gross_cents: gross, total_net_cents: net }
}

function mockApi(list: unknown = answer([expense, draft, storno], 595000, 500000)) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/categories') {
      return Promise.resolve({ categories })
    }
    return Promise.resolve(list)
  })
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/expenses', name: 'expenses', component: ExpensesView },
      {
        path: '/expenses/capture/:id?',
        name: 'expense-capture',
        component: { template: '<div />' },
      },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(ExpensesView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

/** choose opens a govue select by its label and picks an option by its text. */
async function choose(label: string, option: string): Promise<void> {
  await fireEvent.keyDown(screen.getByRole('combobox', { name: label }), { key: 'ArrowDown' })
  const item = await screen.findByRole('option', { name: option })
  item.focus()
  await fireEvent.keyDown(item, { key: 'Enter' })
  await vi.waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
}

function apiError(status: number, code: string) {
  return new ApiError({ code, message: 'failed', status })
}

describe('ExpensesView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('lists the year with the German EÜR line name and the totals', async () => {
    mockApi()
    renderView()

    expect(await screen.findByRole('rowheader', { name: 'Draft Shop' })).toBeVisible()
    expect(screen.getAllByText('Waren und Hilfsstoffe').length).toBeGreaterThan(0)
    expect(screen.queryByText('euer.z27')).toBeNull()
    const table = screen.getByRole('table', { name: 'List of expenses' })
    expect(within(table).getByText('Draft')).toBeVisible()
    expect(within(table).getByText('Storno')).toBeVisible()
    const rows = within(table).getAllByRole('row')
    const footer = rows[rows.length - 1]
    expect(within(footer).getByText('5,950.00 EUR')).toBeVisible()
    expect(within(footer).getByText('5,000.00 EUR')).toBeVisible()
    expect(apiClient.get).toHaveBeenCalledWith(
      '/expenses',
      expect.objectContaining({
        query: expect.objectContaining({ year: String(new Date().getFullYear()) }),
      }),
    )
  })

  it('shows the German strings and German number format', async () => {
    mockApi()
    renderView('de')

    expect(await screen.findByRole('heading', { name: 'Ausgaben' })).toBeVisible()
    expect(await screen.findByRole('button', { name: 'Beleg erfassen' })).toBeVisible()
    expect(await screen.findAllByText('5.950,00 EUR')).not.toHaveLength(0)
  })

  it('reloads with the chosen state and category', async () => {
    mockApi()
    renderView()
    await screen.findByRole('rowheader', { name: 'Draft Shop' })

    await choose('State', 'Draft')
    await vi.waitFor(() =>
      expect(apiClient.get).toHaveBeenCalledWith(
        '/expenses',
        expect.objectContaining({ query: expect.objectContaining({ state: 'draft' }) }),
      ),
    )
    await choose('Category', 'Bürobedarf')
    await vi.waitFor(() =>
      expect(apiClient.get).toHaveBeenCalledWith(
        '/expenses',
        expect.objectContaining({
          query: expect.objectContaining({ state: 'draft', category: 'euer.z51' }),
        }),
      ),
    )
  })

  it('offers only expense categories in the filter, by their German name', async () => {
    mockApi()
    renderView()
    await screen.findByRole('rowheader', { name: 'Draft Shop' })

    await fireEvent.keyDown(screen.getByRole('combobox', { name: 'Category' }), {
      key: 'ArrowDown',
    })
    expect(screen.getByRole('option', { name: 'Bürobedarf' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Sonstige Einnahmen' })).toBeNull()
  })

  it('shows the empty state', async () => {
    mockApi(answer([], 0, 0))
    renderView()

    expect(await screen.findByText('No expenses')).toBeVisible()
  })

  it('shows the error state and retries', async () => {
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      path === '/categories'
        ? Promise.resolve({ categories })
        : Promise.reject(apiError(500, ErrorCode.internal)),
    )
    renderView()

    expect(await screen.findByRole('alert')).toHaveTextContent('The expenses could not be loaded.')
    mockApi()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('rowheader', { name: 'Draft Shop' })).toBeVisible()
  })

  it('clears the session and goes to the login on a 401', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    const { router } = renderView()

    await vi.waitFor(() => expect(me.value).toBeNull())
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })

  it('opens an expense and starts a capture', async () => {
    mockApi()
    const user = userEvent.setup()
    const { router } = renderView()

    await user.click(await screen.findByRole('button', { name: 'Open Draft Shop' }))
    await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('e-2'))

    await user.click(screen.getByRole('button', { name: 'Capture receipt' }))
    await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBeFalsy())
  })
})
