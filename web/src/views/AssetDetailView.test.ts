import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { de, en } from '../i18n/locales'
import { clearSession, me, sessionFailed } from '../session'
import AssetDetailView from './AssetDetailView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const NOTE_EN =
  'For a small business owner the limits for low value assets (800 EUR), the pool (250 to 1,000 EUR) and the immediate expense (250 EUR) are applied to the gross price. This reading is not verified. Check it with your tax adviser.'

// The clock is fixed: 21 November 2026, so 2026 is the open year.
const NOW = new Date('2026-11-21T10:00:00Z')

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
  disposal_rest_cents: null,
  editable: true,
  created_at: '2026-11-20T10:00:00Z',
  updated_at: '2026-11-20T10:00:00Z',
}
const oldDesk = {
  ...laptop,
  id: 'a-2',
  name: 'Old desk',
  acquired_on: '2024-05-02',
  book_value_cents: 58294,
  book_value_year: 2025,
  editable: false,
}
const importedDesk = {
  ...laptop,
  id: 'a-3',
  name: 'Imported desk',
  acquired_on: '2020-05-02',
  expense_id: null,
  opening_book_value_cents: 50000,
  opening_book_value_year: 2026,
  imported: true,
  book_value_cents: 50000,
}
const disposedLaptop = {
  ...laptop,
  id: 'a-4',
  name: 'Gone laptop',
  disposed_on: '2026-06-01',
  book_value_cents: 0,
  book_value_year: 2026,
  disposal_rest_cents: 76667,
  editable: false,
}

const schedule = {
  asset_id: 'a-1',
  method: 'linear',
  years: [
    {
      year: 2026,
      opening_book_value_cents: 149900,
      depreciation_cents: 41639,
      closing_book_value_cents: 108261,
      euer_key: 'euer.z33',
    },
    {
      year: 2027,
      opening_book_value_cents: 108261,
      depreciation_cents: 49967,
      closing_book_value_cents: -100,
      euer_key: 'euer.z33',
    },
  ],
}
const categories = [
  {
    key: 'euer.z33',
    kind: 'expense',
    label_de: 'AfA auf bewegliche Wirtschaftsgüter',
    label_en: 'x',
  },
]

function apiError(
  status: number,
  code: string,
  details?: Record<string, string>,
  message = 'failed',
) {
  return new ApiError({ code, message, status, details })
}

function mockApi(asset: unknown = laptop, plan: unknown = schedule) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path.endsWith('/schedule')) {
      return Promise.resolve(plan)
    }
    if (path.startsWith('/assets/')) {
      return Promise.resolve(asset)
    }
    if (path === '/categories') {
      return Promise.resolve({ categories })
    }
    return Promise.reject(apiError(404, ErrorCode.notFound))
  })
}

async function renderView(id = 'a-1', locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/fixed-assets', name: 'assets', component: { template: '<div />' } },
      { path: '/fixed-assets/:id', name: 'asset-detail', component: AssetDetailView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  await router.push(`/fixed-assets/${id}`)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(AssetDetailView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

describe('AssetDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(NOW)
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  describe('schedule', () => {
    it('shows the schedule the server returned, per year, with the German line name', async () => {
      mockApi()
      await renderView()

      const table = await screen.findByRole('table', { name: 'Depreciation schedule per year' })
      const rows = within(table).getAllByRole('row')
      expect(rows).toHaveLength(3)
      expect(within(rows[1]).getByRole('rowheader', { name: '2026' })).toBeVisible()
      expect(within(rows[1]).getByText('1,499.00 EUR')).toBeVisible()
      expect(within(rows[1]).getByText('416.39 EUR')).toBeVisible()
      expect(within(rows[1]).getByText('1,082.61 EUR')).toBeVisible()
      expect(within(rows[1]).getByText('AfA auf bewegliche Wirtschaftsgüter')).toBeVisible()
      expect(apiClient.get).toHaveBeenCalledWith('/assets/a-1/schedule', expect.anything())
    })

    it('shows the current book value the server sent and what it stands for', async () => {
      mockApi(oldDesk)
      await renderView('a-2')

      const value = await screen.findByTestId('book-value')
      expect(value).toHaveTextContent('582.94 EUR')
      expect(value).toHaveTextContent('at 31 December 2025')
    })

    it('shows 0 and the rest at disposal for a disposed asset', async () => {
      mockApi(disposedLaptop)
      await renderView('a-4')

      const value = await screen.findByTestId('book-value')
      expect(value).toHaveTextContent('0.00 EUR')
      expect(value).toHaveTextContent('disposed in 2026')
      expect(screen.getByText('Book value left at disposal')).toBeVisible()
      expect(screen.getByTestId('disposal-rest')).toHaveTextContent('766.67 EUR')
    })

    it('shows the rest at disposal in German', async () => {
      mockApi(disposedLaptop)
      await renderView('a-4', 'de')

      expect(await screen.findByText('Restbuchwert beim Abgang')).toBeVisible()
      expect(screen.getByTestId('disposal-rest')).toHaveTextContent('766,67 EUR')
      expect(screen.getByTestId('book-value')).toHaveTextContent('Abgang im Jahr 2026')
    })

    it('shows no rest for an asset that is not disposed', async () => {
      mockApi()
      await renderView()

      await screen.findByTestId('book-value')
      expect(screen.queryByTestId('disposal-rest')).toBeNull()
    })

    it('says that no schedule year has ended yet', async () => {
      mockApi()
      await renderView('a-1', 'de')

      const value = await screen.findByTestId('book-value')
      expect(value).toHaveTextContent('1.499,00 EUR')
      expect(value).toHaveTextContent('vor jeder Abschreibung')
    })

    it('keeps the sign of a negative amount', async () => {
      mockApi()
      await renderView()

      const table = await screen.findByRole('table', { name: 'Depreciation schedule per year' })
      expect(within(table).getByText('-1.00 EUR')).toBeVisible()
    })

    it('shows the German strings and number format', async () => {
      mockApi()
      await renderView('a-1', 'de')

      expect(await screen.findByRole('table', { name: 'Abschreibungsplan pro Jahr' })).toBeVisible()
      expect(screen.getAllByText('1.082,61 EUR').length).toBeGreaterThan(0)
      expect(screen.getByRole('button', { name: 'Bearbeiten' })).toBeVisible()
    })

    it('says so when the schedule has no year', async () => {
      mockApi(importedDesk, { ...schedule, years: [] })
      await renderView('a-3')

      expect(await screen.findByText('The schedule has no year.')).toBeVisible()
    })

    it('shows the unverified Kleinunternehmer note', async () => {
      mockApi()
      await renderView()

      const note = await screen.findByTestId('unverified-note')
      expect(note.textContent?.trim()).toBe(NOTE_EN)
    })

    it('shows the error state and retries', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
      await renderView()

      expect(await screen.findByRole('alert')).toHaveTextContent('The asset could not be loaded.')
      mockApi()
      await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))

      expect(await screen.findByRole('table')).toBeVisible()
    })

    it('clears the session and goes to the login on a 401', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const { router } = await renderView()

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('ignores the answer of an older asset when the address changes', async () => {
      const pending: Record<string, (value: unknown) => void> = {}
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/categories') {
          return Promise.resolve({ categories })
        }
        return new Promise((resolve) => {
          pending[path] = resolve
        })
      })
      const { router } = await renderView('a-1')
      await vi.waitFor(() => expect(pending['/assets/a-1']).toBeDefined())
      await router.push('/fixed-assets/a-2')
      await vi.waitFor(() => expect(pending['/assets/a-2/schedule']).toBeDefined())

      pending['/assets/a-2']?.(oldDesk)
      pending['/assets/a-2/schedule']?.(schedule)
      expect(await screen.findByRole('heading', { name: 'Old desk' })).toBeVisible()
      pending['/assets/a-1']?.(laptop)
      pending['/assets/a-1/schedule']?.({ ...schedule, years: [] })
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.getByRole('heading', { name: 'Old desk' })).toBeVisible()
      expect(screen.queryByRole('heading', { name: 'Laptop' })).toBeNull()
      expect(screen.getByRole('table')).toBeVisible()
    })

    it('cancels the requests when the view goes away and ignores a late 401', async () => {
      let reject: (error: unknown) => void = () => {}
      vi.mocked(apiClient.get).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const { router, unmount } = await renderView()
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
  })

  describe('edit', () => {
    it('is enabled in the first schedule year and saves through PUT', async () => {
      mockApi()
      vi.mocked(apiClient.put).mockResolvedValue({ ...laptop, name: 'Laptop 2' })
      const user = userEvent.setup()
      await renderView()

      const edit = await screen.findByRole('button', { name: 'Edit' })
      expect(edit).toBeEnabled()
      expect(screen.queryByTestId('edit-blocked')).toBeNull()
      await user.click(edit)
      const dialog = await screen.findByRole('dialog')
      const name = within(dialog).getByLabelText(/^Name/)
      expect(name).toHaveValue('Laptop')
      expect(within(dialog).getByLabelText(/^Cost basis/)).toHaveValue('1499.00')
      expect(within(dialog).getByLabelText(/^Useful life/)).toHaveValue('36')
      await user.clear(name)
      await user.type(name, 'Laptop 2')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      await vi.waitFor(() => expect(apiClient.put).toHaveBeenCalled())
      expect(vi.mocked(apiClient.put).mock.calls[0]?.[0]).toBe('/assets/a-1')
      expect(vi.mocked(apiClient.put).mock.calls[0]?.[1]).toEqual({
        name: 'Laptop 2',
        acquired_on: '2026-03-10',
        cost_basis_cents: 149900,
        method: 'linear',
        group: 'office_equipment',
        useful_life_months: 36,
        expense_id: 'e-1',
        opening_book_value_cents: null,
        opening_book_value_year: null,
      })
      expect(useToasts().toasts.value.map((x) => x.title)).toContain('The asset is saved.')
    })

    it('is disabled after the first schedule year and names the reason', async () => {
      mockApi(oldDesk)
      await renderView('a-2')

      const edit = await screen.findByRole('button', { name: 'Edit' })
      expect(edit).toBeDisabled()
      expect(screen.getByTestId('edit-blocked')).toHaveTextContent(
        'The first year of the schedule has ended.',
      )
    })

    it('is enabled for an imported asset whose opening year is still open', async () => {
      mockApi(importedDesk)
      await renderView('a-3')

      expect(await screen.findByRole('button', { name: 'Edit' })).toBeEnabled()
    })

    it('follows the editable flag of the server, not its own date logic', async () => {
      // Acquired in the open year by the dates, refused by the server.
      mockApi({ ...laptop, editable: false })
      await renderView()

      expect(await screen.findByRole('button', { name: 'Edit' })).toBeDisabled()
      expect(screen.getByTestId('edit-blocked')).toHaveTextContent(
        'The first year of the schedule has ended.',
      )
    })

    it('is enabled when the server says so, whatever the dates are', async () => {
      mockApi({ ...oldDesk, editable: true })
      await renderView('a-2')

      expect(await screen.findByRole('button', { name: 'Edit' })).toBeEnabled()
      expect(screen.queryByTestId('edit-blocked')).toBeNull()
    })

    it('is disabled for a disposed asset with its own reason', async () => {
      mockApi(disposedLaptop)
      await renderView('a-4')

      expect(await screen.findByRole('button', { name: 'Edit' })).toBeDisabled()
      expect(screen.getByTestId('edit-blocked')).toHaveTextContent('The asset is disposed')
      expect(screen.getByRole('button', { name: 'Record disposal' })).toBeDisabled()
    })

    it('is not offered to a member', async () => {
      me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
      mockApi()
      await renderView()

      await screen.findByRole('table')
      expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Record disposal' })).toBeNull()
      expect(screen.queryByTestId('edit-blocked')).toBeNull()
    })

    it.each([
      [
        'asset_year_ended',
        'The first year of the schedule has ended, or the new date lies in a year that has ended. Record a disposal for a later change.',
      ],
      ['asset_disposed', 'The asset is disposed and can no longer be edited.'],
      [ErrorCode.conflict, 'The asset was changed meanwhile. Close the dialog and load it again.'],
    ])('names the 409 %s of the server by its code', async (code, text) => {
      mockApi()
      vi.mocked(apiClient.put).mockRejectedValue(apiError(409, code))
      const user = userEvent.setup()
      await renderView()

      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(await within(dialog).findByText(text)).toBeVisible()
      expect(within(dialog).queryByText(/An asset exists for this expense/)).toBeNull()
      expect(screen.queryByText(/Something went wrong/)).toBeNull()
    })

    it('tells the separate 409 of an expense another asset uses', async () => {
      mockApi()
      vi.mocked(apiClient.put).mockRejectedValue(apiError(409, 'expense_already_an_asset'))
      const user = userEvent.setup()
      await renderView()

      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(
        (await within(dialog).findAllByText('An asset exists for this expense already.')).length,
      ).toBeGreaterThan(0)
      expect(within(dialog).queryByText(/can no longer be edited/)).toBeNull()
    })

    it('puts API field errors on their fields and focuses the first one', async () => {
      mockApi()
      vi.mocked(apiClient.put).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          name: 'required',
          useful_life_months: 'required for linear, from 1 to 600',
        }),
      )
      const user = userEvent.setup()
      await renderView()

      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      expect(
        await within(dialog).findByText(
          'The linear method needs a useful life from 1 to 600 months.',
        ),
      ).toBeVisible()
      await vi.waitFor(() => expect(within(dialog).getByLabelText(/^Name/)).toHaveFocus())
    })

    it('keeps the opening book value of an imported asset in the PUT body', async () => {
      mockApi(importedDesk)
      vi.mocked(apiClient.put).mockResolvedValue(importedDesk)
      const user = userEvent.setup()
      await renderView('a-3')

      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Save' }))

      await vi.waitFor(() => expect(apiClient.put).toHaveBeenCalled())
      const body = vi.mocked(apiClient.put).mock.calls[0]?.[1] as Record<string, unknown>
      expect(body.opening_book_value_cents).toBe(50000)
      expect(body.opening_book_value_year).toBe(2026)
      expect(body.expense_id).toBeNull()
    })

    it('does not send without a session', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      const dialog = await screen.findByRole('dialog')

      me.value = null
      sessionFailed.value = true

      expect(await within(dialog).findByText(/The session could not be loaded/)).toBeVisible()
      const save = within(dialog).getByRole('button', { name: 'Save' })
      expect(save).toBeDisabled()
      await user.click(save)
      expect(apiClient.put).not.toHaveBeenCalled()
    })

    it('clears the session on a 401 during save', async () => {
      mockApi()
      vi.mocked(apiClient.put).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      const { router } = await renderView()

      await user.click(await screen.findByRole('button', { name: 'Edit' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Save' }),
      )

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })
  })

  describe('disposal', () => {
    async function openDisposal(user: ReturnType<typeof userEvent.setup>) {
      await user.click(await screen.findByRole('button', { name: 'Record disposal' }))
      return screen.findByRole('dialog')
    }

    it('sends the date only, after the dialog, and reloads', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockResolvedValue({ ...laptop, disposed_on: '2026-11-21' })
      const user = userEvent.setup()
      await renderView()

      const dialog = await openDisposal(user)
      expect(apiClient.post).not.toHaveBeenCalled()
      const date = within(dialog).getByLabelText(/^Date of the disposal/)
      expect(date).toHaveValue('2026-11-21')
      await user.clear(date)
      await user.type(date, '2026-11-25')
      await user.click(within(dialog).getByRole('button', { name: 'Record the disposal' }))

      await vi.waitFor(() => expect(apiClient.post).toHaveBeenCalled())
      expect(vi.mocked(apiClient.post).mock.calls[0]?.[0]).toBe('/assets/a-1/disposal')
      expect(vi.mocked(apiClient.post).mock.calls[0]?.[1]).toEqual({
        disposed_on: '2026-11-25',
        proceeds_cents: null,
      })
      await vi.waitFor(() =>
        expect(useToasts().toasts.value.map((x) => x.title)).toContain('The disposal is recorded.'),
      )
    })

    it('puts a date error on the date field and focuses it', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, { disposed_on: 'must not be in the future' }),
      )
      const user = userEvent.setup()
      await renderView()

      const dialog = await openDisposal(user)
      await user.click(within(dialog).getByRole('button', { name: 'Record the disposal' }))

      expect(await within(dialog).findByText('The date must not be in the future.')).toBeVisible()
      await vi.waitFor(() =>
        expect(within(dialog).getByLabelText(/^Date of the disposal/)).toHaveFocus(),
      )
    })

    it.each([
      ['asset_disposed', 'The disposal was refused: the asset is disposed already.'],
      ['asset_year_ended', 'The disposal was refused: the date lies in a year that has ended.'],
      [
        ErrorCode.conflict,
        'The disposal was refused: the asset was changed meanwhile. Load it again.',
      ],
    ])('names the 409 %s by its code', async (code, text) => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(apiError(409, code))
      const user = userEvent.setup()
      await renderView()

      const dialog = await openDisposal(user)
      await user.click(within(dialog).getByRole('button', { name: 'Record the disposal' }))

      expect(await within(dialog).findByText(text)).toBeVisible()
    })

    it('clears the session on a 401', async () => {
      mockApi()
      vi.mocked(apiClient.post).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      const { router } = await renderView()

      const dialog = await openDisposal(user)
      await user.click(within(dialog).getByRole('button', { name: 'Record the disposal' }))

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('sends nothing when the disposal form is submitted without a session', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      const dialog = await openDisposal(user)

      me.value = null
      // The submit event reaches confirmDisposal() without the disabled button.
      const form = dialog.querySelector('form')
      expect(form).not.toBeNull()
      await fireEvent.submit(form as HTMLFormElement)

      expect(apiClient.post).not.toHaveBeenCalled()
      expect(
        (await within(dialog).findAllByText(/The session could not be loaded/)).length,
      ).toBeGreaterThan(0)
    })

    it('does not send without a session', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      const dialog = await openDisposal(user)

      me.value = null
      sessionFailed.value = true

      expect(await within(dialog).findByText(/The session could not be loaded/)).toBeVisible()
      const confirm = within(dialog).getByRole('button', { name: 'Record the disposal' })
      expect(confirm).toBeDisabled()
      await user.click(confirm)
      expect(apiClient.post).not.toHaveBeenCalled()
    })
  })
})
