import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { germanYear } from '../assets'
import { de, en } from '../i18n/locales'
import { clearSession, me, sessionFailed } from '../session'
import AssetsView from './AssetsView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const NOTE_EN =
  'For a small business owner the limits for low value assets (800 EUR), the pool (250 to 1,000 EUR) and the immediate expense (250 EUR) are applied to the gross price. This reading is not verified. Check it with your tax adviser.'
const NOTE_DE =
  'Die Grenzen für geringwertige Wirtschaftsgüter (800 EUR), den Sammelposten (250 bis 1.000 EUR) und den Sofortabzug (250 EUR) werden bei Kleinunternehmern auf den Bruttopreis angewendet. Diese Auslegung ist nicht geprüft. Klären Sie sie mit Ihrer Steuerberatung.'

const laptop = {
  id: 'a-1',
  name: 'Laptop',
  acquired_on: '2026-03-10',
  cost_basis_cents: 149900,
  method: 'linear',
  group: 'office_equipment',
  useful_life_months: 36,
  expense_id: 'e-1',
  opening_book_value_cents: null,
  opening_book_value_year: null,
  disposed_on: null,
  disposal_proceeds_cents: null,
  imported: false,
  book_value_cents: 149900,
  book_value_year: null,
  editable: true,
  created_at: '2026-11-20T10:00:00Z',
  updated_at: '2026-11-20T10:00:00Z',
}
const phone = {
  ...laptop,
  id: 'a-2',
  name: 'Phone',
  method: 'gwg',
  useful_life_months: null,
  expense_id: null,
  cost_basis_cents: 59900,
  disposed_on: '2026-05-01',
  // Figures the server sent. The view shows them and checks nothing.
  book_value_cents: 12345,
  book_value_year: 2025,
  editable: false,
}

function expense(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    state: 'booked',
    paid_on: '2026-03-10',
    receipt_date: '2026-03-10',
    supplier: 'Shop',
    description: '',
    gross_cents: 178381,
    net_cents: 149900,
    vat_rate_permyriad: 1900,
    vat_cents: 28481,
    category: 'euer.z27',
    file_id: null,
    is_asset: true,
    storno_of_id: null,
    imported: false,
    booked_at: '2026-03-11T10:00:00Z',
    ...over,
  }
}

const periods = [
  { valid_from: '2026-01-01', regime: 'kleinunternehmer', superseded: false },
  { valid_from: '2027-01-01', regime: 'standard', superseded: false },
]

function apiError(
  status: number,
  code: string,
  details?: Record<string, string>,
  message = 'failed',
) {
  return new ApiError({ code, message, status, details })
}

function mockApi(
  list: unknown = { assets: [laptop, phone] },
  expenses: unknown[] = [
    expense('e-1', { supplier: 'Taken Shop' }),
    expense('e-2', { supplier: 'Laptop Shop' }),
    expense('e-3', { supplier: 'No Flag', is_asset: false }),
    expense('e-4', { supplier: 'Reversal', storno_of_id: 'e-2' }),
  ],
) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/assets') {
      return Promise.resolve(list)
    }
    if (path === '/expenses') {
      return Promise.resolve({ expenses })
    }
    if (path === '/vat-regimes') {
      return Promise.resolve({ periods })
    }
    return Promise.reject(apiError(404, ErrorCode.notFound))
  })
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/assets', name: 'assets', component: AssetsView },
      { path: '/assets/:id', name: 'asset-detail', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(AssetsView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

async function choose(label: RegExp | string, option: string): Promise<void> {
  await fireEvent.keyDown(screen.getByRole('combobox', { name: label }), { key: 'ArrowDown' })
  const item = await screen.findByRole('option', { name: option })
  item.focus()
  await fireEvent.keyDown(item, { key: 'Enter' })
  await vi.waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
}

interface Deferred {
  resolve: (value: unknown) => void
  reject: (error: unknown) => void
}

const LAPTOP_OPTION = '2026-03-10 Laptop Shop 1,783.81 EUR'

/** openCreate opens the dialog and picks the laptop expense. */
async function openCreateAndPick(user: ReturnType<typeof userEvent.setup>, option = LAPTOP_OPTION) {
  await user.click(await screen.findByRole('button', { name: 'Create asset' }))
  const dialog = await screen.findByRole('dialog')
  await vi.waitFor(() =>
    expect(within(dialog).queryByText('Loading the expenses.')).not.toBeInTheDocument(),
  )
  await choose(/^Expense/, option)
  return dialog
}

describe('AssetsView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('lists date, cost, method and the current book value as the server returns them', async () => {
    mockApi()
    renderView()

    const table = await screen.findByRole('table', { name: 'List of assets' })
    expect(within(table).getByRole('columnheader', { name: 'Current book value' })).toBeVisible()
    const rows = within(table).getAllByRole('row')
    expect(within(rows[1]).getByRole('rowheader', { name: 'Laptop' })).toBeVisible()
    expect(within(rows[1]).getByText('2026-03-10')).toBeVisible()
    // Cost and book value: the same figure before any depreciation.
    expect(within(rows[1]).getAllByText('1,499.00 EUR')).toHaveLength(2)
    expect(within(rows[1]).getByText('before any depreciation')).toBeVisible()
    expect(within(rows[1]).getByText('Linear')).toBeVisible()
    expect(within(rows[1]).getByText('Editable')).toBeVisible()
    expect(within(rows[2]).getByText('Low value asset (GWG)')).toBeVisible()
    expect(within(rows[2]).getByText('123.45 EUR')).toBeVisible()
    expect(within(rows[2]).getByText('at 31 December 2025')).toBeVisible()
    expect(within(rows[2]).getByText('Disposed')).toBeVisible()
    expect(within(rows[2]).queryByText('Editable')).toBeNull()
    expect(screen.queryByText(/does not show a current book value/)).toBeNull()
  })

  it('shows the German strings and number format', async () => {
    mockApi()
    renderView('de')

    expect(await screen.findByRole('heading', { name: 'Anlagen' })).toBeVisible()
    expect((await screen.findAllByText('1.499,00 EUR')).length).toBeGreaterThan(0)
    expect(screen.getByText('123,45 EUR')).toBeVisible()
    expect(screen.getByText('zum 31. Dezember 2025')).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'Aktueller Buchwert' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Anlagegut anlegen' })).toBeVisible()
  })

  it.each([
    ['en', NOTE_EN],
    ['de', NOTE_DE],
  ] as const)('shows the unverified Kleinunternehmer note in %s', async (locale, text) => {
    mockApi()
    renderView(locale)

    const note = await screen.findByTestId('unverified-note')
    expect(note).toBeVisible()
    expect(note.textContent?.trim()).toBe(text)
  })

  it('offers the create action to the owner only', async () => {
    me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
    mockApi()
    renderView()

    await screen.findByRole('table')
    expect(screen.queryByRole('button', { name: 'Create asset' })).toBeNull()
  })

  it('shows the empty state', async () => {
    mockApi({ assets: [] })
    renderView()

    expect(await screen.findByText('No assets')).toBeVisible()
  })

  it('shows the error state and retries', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
    renderView()

    expect(await screen.findByRole('alert')).toHaveTextContent('The assets could not be loaded.')
    mockApi()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('rowheader', { name: 'Laptop' })).toBeVisible()
  })

  it('clears the session and goes to the login on a 401', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    const { router } = renderView()

    await vi.waitFor(() => expect(me.value).toBeNull())
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })

  // The owner sees Create during the first load, and a save reloads the list:
  // two loads are pending, and the OLDER one settles last.
  it.each([
    ['a stale list', (c: Deferred) => c.resolve({ assets: [phone] })],
    ['a stale failure', (c: Deferred) => c.reject(apiError(500, ErrorCode.internal))],
    ['a stale 401', (c: Deferred) => c.reject(apiError(401, ErrorCode.unauthenticated))],
  ])('ignores %s that settles after the newer load', async (_name, settleStale) => {
    const loads: Deferred[] = []
    mockApi()
    const base = vi.mocked(apiClient.get).getMockImplementation()
    if (!base) {
      throw new Error('mockApi installed no implementation')
    }
    vi.mocked(apiClient.get).mockImplementation((path: string, options?: unknown) => {
      if (path === '/assets') {
        return new Promise((resolve, reject) => {
          loads.push({ resolve, reject })
        })
      }
      return base(path, options as never)
    })
    vi.mocked(apiClient.post).mockResolvedValue(laptop)
    const user = userEvent.setup()
    const { router } = renderView()
    const push = vi.spyOn(router, 'push')
    await vi.waitFor(() => expect(loads).toHaveLength(1))

    const dialog = await openCreateAndPick(user)
    await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await vi.waitFor(() => expect(loads).toHaveLength(2))

    loads[1]?.resolve({ assets: [laptop] })
    expect(await screen.findByRole('rowheader', { name: 'Laptop' })).toBeVisible()
    const stale = loads[0]
    if (!stale) {
      throw new Error('the first load is missing')
    }
    settleStale(stale)
    await new Promise((resolve) => setTimeout(resolve, 20))

    expect(screen.getByRole('rowheader', { name: 'Laptop' })).toBeVisible()
    expect(screen.queryByRole('rowheader', { name: 'Phone' })).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(me.value).not.toBeNull()
    expect(push).not.toHaveBeenCalled()
  })

  it('cancels the load when the view goes away and ignores a late 401', async () => {
    let reject: (error: unknown) => void = () => {}
    vi.mocked(apiClient.get).mockImplementation(
      () =>
        new Promise((_resolve, rej) => {
          reject = rej
        }),
    )
    const { router, unmount } = renderView()
    await vi.waitFor(() => expect(apiClient.get).toHaveBeenCalled())
    const signal = vi.mocked(apiClient.get).mock.calls[0]?.[1]?.signal
    expect(signal).toBeInstanceOf(AbortSignal)
    expect(signal?.aborted).toBe(false)

    const push = vi.spyOn(router, 'push')
    const replace = vi.spyOn(router, 'replace')
    unmount()
    expect(signal?.aborted).toBe(true)
    reject(apiError(401, ErrorCode.unauthenticated))
    await new Promise((resolve) => setTimeout(resolve, 20))

    expect(me.value).not.toBeNull()
    expect(push).not.toHaveBeenCalled()
    expect(replace).not.toHaveBeenCalled()
  })

  it('opens an asset', async () => {
    mockApi()
    const { router } = renderView()

    await userEvent.setup().click(await screen.findByRole('button', { name: 'Open Laptop' }))

    await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('a-1'))
  })

  describe('create', () => {
    it('offers booked asset expenses that are not an asset yet and proposes the gross cost for a small business', async () => {
      mockApi()
      const user = userEvent.setup()
      renderView()

      const dialog = await openCreateAndPick(user)

      expect(within(dialog).getByLabelText(/^Name/)).toHaveValue('Laptop Shop')
      expect(within(dialog).getByLabelText('Acquired on')).toHaveValue('2026-03-10')
      expect(within(dialog).getByLabelText(/^Cost basis/)).toHaveValue('1783.81')
      // The listed expenses carry the flag: the taken, unflagged and storno rows are not offered.
      await fireEvent.keyDown(within(dialog).getByRole('combobox', { name: /^Expense/ }), {
        key: 'ArrowDown',
      })
      const options = await screen.findAllByRole('option')
      expect(options.map((o) => o.textContent?.trim())).toEqual([LAPTOP_OPTION])
      // The server narrows the list to booked expenses with the asset flag.
      expect(apiClient.get).toHaveBeenCalledWith('/expenses', {
        query: { year: String(germanYear()), state: 'booked', is_asset: 'true' },
        signal: expect.any(AbortSignal),
      })
    })

    it('proposes the net cost under the standard regime', async () => {
      mockApi(undefined, [expense('e-2', { supplier: 'Laptop Shop', paid_on: '2027-02-01' })])
      const user = userEvent.setup()
      renderView()

      const dialog = await openCreateAndPick(user, '2027-02-01 Laptop Shop 1,783.81 EUR')

      expect(within(dialog).getByLabelText(/^Cost basis/)).toHaveValue('1499.00')
    })

    it('sends the typed useful life and cost as integer cents', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockResolvedValue(laptop)
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)

      const cost = within(dialog).getByLabelText(/^Cost basis/)
      await user.clear(cost)
      await user.type(cost, '1499,50')
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      await vi.waitFor(() => expect(apiClient.post).toHaveBeenCalled())
      expect(vi.mocked(apiClient.post).mock.calls[0]?.[0]).toBe('/assets')
      expect(vi.mocked(apiClient.post).mock.calls[0]?.[1]).toEqual({
        name: 'Laptop Shop',
        acquired_on: '2026-03-10',
        cost_basis_cents: 149950,
        method: 'linear',
        group: 'office_equipment',
        useful_life_months: 36,
        expense_id: 'e-2',
        opening_book_value_cents: null,
        opening_book_value_year: null,
      })
      expect(await screen.findByRole('rowheader', { name: 'Laptop' })).toBeVisible()
      expect(useToasts().toasts.value.map((x) => x.title)).toContain('The asset is saved.')
    })

    it('refuses a linear asset without a useful life and sends nothing', async () => {
      mockApi()
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)

      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(await within(dialog).findByText('Enter a whole number of months.')).toBeVisible()
      await vi.waitFor(() => expect(within(dialog).getByLabelText(/^Useful life/)).toHaveFocus())
      expect(apiClient.post).not.toHaveBeenCalled()
    })

    it('puts API field errors on their fields and moves the focus to the first one', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          acquired_on: 'must not be in the future',
          cost_basis_cents: "must be 149900, the expense's cost basis under the standard regime",
        }),
      )
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(await within(dialog).findByText('The date must not be in the future.')).toBeVisible()
      expect(
        within(dialog).getByText('The cost basis of this expense is 1,499.00 EUR (standard VAT).'),
      ).toBeVisible()
      await vi.waitFor(() => expect(within(dialog).getByLabelText('Acquired on')).toHaveFocus())
      expect(within(dialog).getByText(/Check the marked fields/)).toBeVisible()
    })

    it('says so when an asset exists for the expense already, read from the code', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(apiError(409, 'expense_already_an_asset'))
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(
        (await within(dialog).findAllByText('An asset exists for this expense already.')).length,
      ).toBeGreaterThan(0)
      await vi.waitFor(() =>
        expect(within(dialog).getByRole('combobox', { name: /^Expense/ })).toHaveFocus(),
      )
    })

    it('clears the session on a 401 during save', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      const { router } = renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('names a 403 without the marked fields text', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(apiError(403, ErrorCode.permissionDenied))
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(await within(dialog).findByText('You do not have permission for this.')).toBeVisible()
      expect(within(dialog).queryByText(/Check the marked fields/)).toBeNull()
    })

    it('keeps Save disabled and says why when the session is lost, and sends nothing', async () => {
      mockApi()
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')

      me.value = null
      sessionFailed.value = true

      expect(await within(dialog).findByText(/The session could not be loaded/)).toBeVisible()
      const save = within(dialog).getByRole('button', { name: 'Save' })
      expect(save).toBeDisabled()
      await user.click(save)
      expect(apiClient.post).not.toHaveBeenCalled()
    })

    it('sends nothing when the form is submitted without a session', async () => {
      mockApi()
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')

      me.value = null
      // The submit event reaches save() without the disabled button.
      const form = dialog.querySelector('form')
      expect(form).not.toBeNull()
      await fireEvent.submit(form as HTMLFormElement)

      expect(apiClient.post).not.toHaveBeenCalled()
      expect(
        (await within(dialog).findAllByText(/The session could not be loaded/)).length,
      ).toBeGreaterThan(0)
    })

    it('cancels a running save when the view goes away and ignores a late 401', async () => {
      mockApi()
      let reject: (error: unknown) => void = () => {}
      vi.mocked(apiClient.post).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const user = userEvent.setup()
      const { router, unmount } = renderView()
      const dialog = await openCreateAndPick(user)
      await user.type(within(dialog).getByLabelText(/^Useful life/), '36')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))
      await vi.waitFor(() => expect(apiClient.post).toHaveBeenCalled())
      const signal = vi.mocked(apiClient.post).mock.calls[0]?.[2]?.signal
      expect(signal).toBeInstanceOf(AbortSignal)
      expect(signal?.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      expect(signal?.aborted).toBe(true)
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })

    it('hides the useful life for a method that has none', async () => {
      mockApi()
      const user = userEvent.setup()
      renderView()
      const dialog = await openCreateAndPick(user)

      await choose('Method', 'Low value asset (GWG)')

      expect(within(dialog).queryByLabelText(/^Useful life/)).toBeNull()
    })
  })
})
