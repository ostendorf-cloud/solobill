import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import { uploadReceipt } from '../expenses'
import { de, en } from '../i18n/locales'
import { clearSession, me, sessionFailed } from '../session'
import ExpenseCaptureView from './ExpenseCaptureView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

vi.mock('../expenses', async (original) => ({
  ...(await original<object>()),
  uploadReceipt: vi.fn(),
}))

const periods = [
  { valid_from: '2026-01-01', regime: 'kleinunternehmer', superseded: false },
  { valid_from: '2027-01-01', regime: 'standard', superseded: false },
]

const categories = [
  { key: 'euer.z27', kind: 'expense', label_de: 'Waren und Hilfsstoffe', label_en: 'Goods' },
  { key: 'euer.z51', kind: 'expense', label_de: 'Bürobedarf', label_en: 'Office' },
  { key: 'euer.z14', kind: 'income', label_de: 'Sonstige Einnahmen', label_en: 'Other' },
]

const draft = {
  id: 'e-2',
  state: 'draft',
  paid_on: '2026-11-18',
  receipt_date: '2026-11-17',
  supplier: 'Draft Shop',
  description: 'Paper',
  gross_cents: 5950,
  net_cents: 5950,
  vat_rate_permyriad: 0,
  vat_cents: 0,
  category: 'euer.z51',
  file_id: 'f-1',
  is_asset: false,
  storno_of_id: null,
  imported: false,
  booked_at: null,
}
const booked = { ...draft, id: 'e-1', state: 'booked', booked_at: '2026-11-19T10:00:00Z' }

const imageFile = {
  id: 'f-1',
  sha256: 'abc',
  size_bytes: 100,
  content_type: 'image/png',
  original_filename: 'receipt.png',
  uploaded_at: '2026-11-20T10:00:00Z',
}
const pdfFile = { ...imageFile, content_type: 'application/pdf', original_filename: 'receipt.pdf' }

function mockApi(options: { expense?: unknown; file?: unknown; periods?: unknown } = {}) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/categories') {
      return Promise.resolve({ categories })
    }
    if (path === '/vat-regimes') {
      return options.periods === 'fail'
        ? Promise.reject(apiError(500, ErrorCode.internal))
        : Promise.resolve({ periods: options.periods ?? periods })
    }
    if (path.startsWith('/expenses/')) {
      return Promise.resolve(options.expense)
    }
    if (path.startsWith('/files/')) {
      return Promise.resolve(options.file ?? imageFile)
    }
    return Promise.reject(apiError(404, ErrorCode.notFound))
  })
}

async function renderView(path = '/expenses/capture', locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/expenses', name: 'expenses', component: { template: '<div />' } },
      { path: '/expenses/capture/:id?', name: 'expense-capture', component: ExpenseCaptureView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(ExpenseCaptureView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

/** choose opens a govue select by its label and picks an option by its text. */
async function choose(label: RegExp | string, option: string): Promise<void> {
  await fireEvent.keyDown(screen.getByRole('combobox', { name: label }), { key: 'ArrowDown' })
  const item = await screen.findByRole('option', { name: option })
  item.focus()
  await fireEvent.keyDown(item, { key: 'Enter' })
  // The list closes a few ticks later and makes the rest of the page inert
  // until then.
  await vi.waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
}

function userWithoutAccept() {
  // The file input filters by its accept list, which would hide the very
  // files these tests refuse.
  return userEvent.setup({ applyAccept: false })
}

function fileOf(name: string, type: string, size?: number): File {
  const file = new File(['content'], name, { type })
  if (size !== undefined) {
    Object.defineProperty(file, 'size', { value: size })
  }
  return file
}

async function skipUpload(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Continue without a receipt' }))
  await screen.findByLabelText(/^Paid on/)
}

describe('ExpenseCaptureView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
    mockApi()
  })

  describe('upload step', () => {
    it('starts with the upload step and no form', async () => {
      await renderView()

      expect(await screen.findByLabelText('Receipt file')).toBeVisible()
      expect(screen.getByText('PDF, JPEG, PNG or XML, at most 10 MiB.')).toBeVisible()
      expect(screen.queryByLabelText(/^Paid on/)).toBeNull()
    })

    it('refuses a file type outside the allowlist and sends nothing', async () => {
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('archive.zip', 'application/zip'),
      )

      expect(await screen.findByText(/This file type is not accepted/)).toBeVisible()
      expect(screen.getByLabelText('Receipt file')).toHaveAttribute('aria-invalid', 'true')
      expect(screen.getByRole('button', { name: 'Upload receipt' })).toBeDisabled()
      expect(uploadReceipt).not.toHaveBeenCalled()
    })

    it('refuses a file over 10 MiB and sends nothing', async () => {
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('big.pdf', 'application/pdf', 10 * 1024 * 1024 + 1),
      )

      expect(await screen.findByText('The file is larger than 10 MiB.')).toBeVisible()
      expect(screen.getByRole('button', { name: 'Upload receipt' })).toBeDisabled()
      expect(uploadReceipt).not.toHaveBeenCalled()
    })

    it('accepts a file of exactly 10 MiB', async () => {
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('edge.pdf', 'application/pdf', 10 * 1024 * 1024),
      )

      expect(screen.getByRole('button', { name: 'Upload receipt' })).toBeEnabled()
    })

    it.each([
      [413, ErrorCode.requestTooLarge, 'The server refused the file: it is too large.'],
      [
        415,
        ErrorCode.unsupportedMediaType,
        /file type or content is not accepted\. Allowed are PDF, JPEG, PNG and XML files without a DOCTYPE or stylesheet/,
      ],
      [503, 'object_store_unavailable', 'The receipt storage is not available. Try again later.'],
      [403, ErrorCode.permissionDenied, 'You do not have permission for this.'],
      [0, ErrorCode.network, 'The server cannot be reached. Check the connection.'],
      [0, ErrorCode.timeout, /The upload took too long/],
      [500, ErrorCode.internal, 'The receipt could not be uploaded. Try again.'],
    ])('names a %i answer of the upload (%s)', async (status, code, text) => {
      vi.mocked(uploadReceipt).mockRejectedValue(apiError(status, code))
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('a.pdf', 'application/pdf'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))

      expect(await screen.findByText(text)).toBeVisible()
      expect(screen.queryByLabelText(/^Paid on/)).toBeNull()
    })

    it('opens the form beside an inline image preview after the upload', async () => {
      vi.mocked(uploadReceipt).mockResolvedValue({ file: imageFile, duplicates: [] })
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('receipt.png', 'image/png'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))

      const image = await screen.findByRole('img', { name: 'Receipt receipt.png' })
      expect(image).toHaveAttribute('src', '/api/v1/files/f-1/content')
      expect(screen.getByLabelText(/^Paid on/)).toBeVisible()
    })

    it('shows a PDF as a download link, never in a frame', async () => {
      vi.mocked(uploadReceipt).mockResolvedValue({ file: pdfFile, duplicates: [] })
      const user = userWithoutAccept()
      const { container } = await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('receipt.pdf', 'application/pdf'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))

      const link = await screen.findByRole('link', { name: 'Download receipt.pdf' })
      expect(link).toHaveAttribute('href', '/api/v1/files/f-1/content')
      expect(container.querySelector('iframe, object, embed')).toBeNull()
      expect(screen.queryByRole('img')).toBeNull()
    })

    it('warns about a duplicate and lets the owner continue', async () => {
      vi.mocked(uploadReceipt).mockResolvedValue({
        file: imageFile,
        duplicates: [{ ...imageFile, id: 'f-0', original_filename: 'older.png' }],
      })
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('receipt.png', 'image/png'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))

      const warning = await screen.findByRole('alert')
      expect(warning).toHaveTextContent('This receipt is already stored')
      expect(warning).toHaveTextContent('older.png')
      expect(screen.queryByLabelText(/^Paid on/)).toBeNull()

      await user.click(within(warning).getByRole('button', { name: 'Continue anyway' }))

      expect(await screen.findByLabelText(/^Paid on/)).toBeVisible()
      expect(screen.getByRole('img', { name: 'Receipt receipt.png' })).toBeVisible()
    })

    it('lets the owner choose another file after a duplicate warning', async () => {
      vi.mocked(uploadReceipt).mockResolvedValue({
        file: imageFile,
        duplicates: [{ ...imageFile, id: 'f-0', original_filename: 'older.png' }],
      })
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('receipt.png', 'image/png'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))
      await user.click(await screen.findByRole('button', { name: 'Choose another file' }))

      expect(await screen.findByLabelText('Receipt file')).toBeVisible()
      expect(screen.queryByText('This receipt is already stored')).toBeNull()
    })

    it('clears the session on a 401 during the upload', async () => {
      vi.mocked(uploadReceipt).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userWithoutAccept()
      const { router } = await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('a.pdf', 'application/pdf'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('cancels a running upload when the view goes away and ignores a late 401', async () => {
      let reject: (error: unknown) => void = () => {}
      vi.mocked(uploadReceipt).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const user = userWithoutAccept()
      const { router, unmount } = await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('a.pdf', 'application/pdf'),
      )
      await user.click(screen.getByRole('button', { name: 'Upload receipt' }))
      const signal = vi.mocked(uploadReceipt).mock.calls[0]?.[1]
      expect(signal).toBeInstanceOf(AbortSignal)
      expect(signal?.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      // vue-router resets currentRoute to START_LOCATION when its last app
      // unmounts, so the route name alone cannot show "nothing happened".
      expect(router.currentRoute.value.matched).toHaveLength(0)
      expect(signal?.aborted).toBe(true)
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
      expect(router.currentRoute.value.matched).toHaveLength(0)
    })

    it('keeps the upload disabled without a session', async () => {
      me.value = null
      vi.mocked(apiClient.get).mockRejectedValue(apiError(503, ErrorCode.unavailable))
      sessionFailed.value = true
      const user = userWithoutAccept()
      await renderView()

      await user.upload(
        await screen.findByLabelText('Receipt file'),
        fileOf('a.pdf', 'application/pdf'),
      )

      expect(await screen.findByText(/The session could not be loaded/)).toBeVisible()
      expect(screen.getByRole('button', { name: 'Upload receipt' })).toBeDisabled()
      expect(uploadReceipt).not.toHaveBeenCalled()
    })
  })

  describe('regime', () => {
    it('shows no VAT fields to a Kleinunternehmer', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2026-11-18')

      expect(await screen.findByText(/VAT regime on this date: Kleinunternehmer/)).toBeVisible()
      expect(screen.getByLabelText(/^Gross amount/)).toBeVisible()
      expect(screen.queryByLabelText('VAT rate in percent')).toBeNull()
      expect(screen.queryByLabelText('VAT amount')).toBeNull()
      expect(screen.queryByLabelText('Net amount')).toBeNull()
    })

    it('shows the VAT fields under the standard regime', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2027-02-01')

      expect(await screen.findByText(/VAT regime on this date: standard/)).toBeVisible()
      expect(screen.getByLabelText('VAT rate in percent')).toBeVisible()
      expect(screen.getByLabelText('VAT amount')).toBeVisible()
      expect(screen.getByLabelText('Net amount')).toBeVisible()
    })

    it('follows the date when it changes', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)
      const date = screen.getByLabelText(/^Paid on/)

      await user.type(date, '2027-02-01')
      expect(await screen.findByLabelText('VAT amount')).toBeVisible()
      await user.clear(date)
      await user.type(date, '2026-11-18')

      await vi.waitFor(() => expect(screen.queryByLabelText('VAT amount')).toBeNull())
    })

    it('says so and shows the VAT fields when no period covers the date', async () => {
      mockApi({ periods: [] })
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2026-11-18')

      expect(await screen.findByText(/No VAT regime period covers this date/)).toBeVisible()
      expect(screen.getByLabelText('VAT amount')).toBeVisible()
    })

    it('shows the VAT fields when the regimes cannot be loaded', async () => {
      mockApi({ periods: 'fail' })
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      expect(await screen.findByText(/The VAT regime could not be loaded/)).toBeVisible()
      expect(screen.getByLabelText('VAT amount')).toBeVisible()
    })

    it('sends net equal to gross and no VAT as a Kleinunternehmer', async () => {
      vi.mocked(apiClient.post).mockResolvedValue({ ...draft, id: 'e-9' })
      const user = userEvent.setup()
      const { router } = await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2026-11-18')
      await user.type(screen.getByLabelText('Supplier'), 'Draft Shop')
      await user.type(screen.getByLabelText(/^Gross amount/), '59,50')
      await choose(/^Category/, 'Bürobedarf')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(apiClient.post).toHaveBeenCalledWith('/expenses', {
        paid_on: '2026-11-18',
        receipt_date: null,
        supplier: 'Draft Shop',
        description: '',
        gross_cents: 5950,
        net_cents: 5950,
        vat_rate_permyriad: 0,
        vat_cents: 0,
        category: 'euer.z51',
        file_id: null,
        is_asset: false,
      })
      await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('e-9'))
      expect(useToasts().toasts.value.map((x) => x.title)).toContain('The draft is saved.')
    })

    it('sends gross, rate, VAT and the net by integer subtraction under the standard regime', async () => {
      vi.mocked(apiClient.post).mockResolvedValue({ ...draft, id: 'e-9' })
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2027-02-01')
      await user.type(await screen.findByLabelText(/^Gross amount/), '119')
      await user.type(screen.getByLabelText('VAT rate in percent'), '19')
      await user.type(screen.getByLabelText('VAT amount'), '19,00')

      expect(screen.getByLabelText('Net amount')).toHaveValue('100.00')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(apiClient.post).toHaveBeenCalledWith(
        '/expenses',
        expect.objectContaining({
          gross_cents: 11900,
          net_cents: 10000,
          vat_rate_permyriad: 1900,
          vat_cents: 1900,
        }),
      )
    })

    it('does not send a VAT above the gross amount', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2027-02-01')
      await user.type(await screen.findByLabelText(/^Gross amount/), '10')
      await user.type(screen.getByLabelText('VAT amount'), '12')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(
        await screen.findByText('The VAT cannot be higher than the gross amount.'),
      ).toBeVisible()
      expect(apiClient.post).not.toHaveBeenCalled()
    })
  })

  describe('saving', () => {
    it('does not send a gross amount that is not an amount', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), '1.234,50')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(await screen.findByText('Enter an amount such as 59,50.')).toBeVisible()
      expect(apiClient.post).not.toHaveBeenCalled()
    })

    it('puts API field errors on their fields in the user language', async () => {
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          paid_on: 'must be a date such as 2026-11-18',
          category: 'must be a key of kind expense from GET /api/v1/categories',
          supplier: 'something new',
        }),
      )
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), '10')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(await screen.findByText('Enter a date such as 2026-11-18.')).toBeVisible()
      expect(screen.getByLabelText(/^Paid on/)).toHaveAttribute('aria-invalid', 'true')
      expect(screen.getByText('Choose a category from the list.')).toBeVisible()
      expect(screen.getByText('Invalid value.')).toBeVisible()
      expect(screen.queryByText('something new')).toBeNull()
      expect(screen.getByText(/Check the marked fields/)).toBeVisible()
    })

    it('puts vat_regime_missing on the payment date', async () => {
      vi.mocked(apiClient.post).mockRejectedValue(apiError(400, 'vat_regime_missing'))
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Paid on/), '2025-03-01')
      await user.type(screen.getByLabelText(/^Gross amount/), '10')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(await screen.findByText('No VAT regime period covers this date.')).toBeVisible()
      expect(screen.getByLabelText(/^Paid on/)).toHaveAttribute('aria-invalid', 'true')
    })

    it('moves the focus to the first invalid field after a refused save', async () => {
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          receipt_date: 'must be a date such as 2026-11-18',
          category: 'must be a key of kind expense from GET /api/v1/categories',
        }),
      )
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), '10')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(screen.getByLabelText('Receipt date')).toHaveFocus())
      expect(screen.getByText(/Check the marked fields/)).toBeVisible()
    })

    it('moves the focus to the amount field when the amount is not an amount', async () => {
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), 'abc')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(screen.getByLabelText(/^Gross amount/)).toHaveFocus())
    })

    it('clears the session on a 401 during save', async () => {
      vi.mocked(apiClient.post).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      const { router } = await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), '10')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('names a 403 during save without the marked fields text', async () => {
      vi.mocked(apiClient.post).mockRejectedValue(apiError(403, ErrorCode.permissionDenied))
      const user = userEvent.setup()
      await renderView()
      await skipUpload(user)

      await user.type(screen.getByLabelText(/^Gross amount/), '10')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(await screen.findByText('You do not have permission for this.')).toBeVisible()
      expect(screen.queryByText(/marked fields/)).toBeNull()
    })

    it('keeps Save disabled and says why when the session failed', async () => {
      me.value = null
      sessionFailed.value = true
      vi.mocked(apiClient.get).mockImplementation((path: string) =>
        path === '/me'
          ? Promise.reject(apiError(503, ErrorCode.unavailable))
          : Promise.resolve({ categories, periods }),
      )
      const user = userEvent.setup()
      await renderView()
      await user.click(await screen.findByRole('button', { name: 'Continue without a receipt' }))

      expect(await screen.findByText(/The session could not be loaded/)).toBeVisible()
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
      expect(apiClient.get).toHaveBeenCalledWith('/me')
    })

    it('updates a saved draft through PUT', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.put).mockResolvedValue({ ...draft, supplier: 'Renamed' })
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      const supplier = await screen.findByLabelText('Supplier')
      expect(supplier).toHaveValue('Draft Shop')
      expect(screen.getByLabelText(/^Gross amount/)).toHaveValue('59.50')
      await user.clear(supplier)
      await user.type(supplier, 'Renamed')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(apiClient.put).toHaveBeenCalledWith(
        '/expenses/e-2',
        expect.objectContaining({ supplier: 'Renamed', gross_cents: 5950, file_id: 'f-1' }),
      )
      expect(apiClient.post).not.toHaveBeenCalled()
    })

    it('shows the German strings', async () => {
      await renderView('/expenses/capture', 'de')

      expect(await screen.findByRole('heading', { name: 'Neue Ausgabe' })).toBeVisible()
      expect(screen.getByLabelText('Belegdatei')).toBeVisible()
    })

    it('shows the error state when the expense cannot be loaded', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) =>
        path.startsWith('/expenses/')
          ? Promise.reject(apiError(404, ErrorCode.notFound))
          : Promise.resolve({ categories, periods }),
      )
      await renderView('/expenses/capture/e-2')

      expect(await screen.findByText('The expense could not be loaded.')).toBeVisible()
    })
  })

  describe('booking', () => {
    it('books only after a confirmation that names the immutability', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.post).mockResolvedValue(booked)
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('Book this expense?')).toBeVisible()
      expect(within(dialog).getByText(/can never be changed or deleted again/)).toBeVisible()
      expect(apiClient.post).not.toHaveBeenCalled()

      await user.click(within(dialog).getByRole('button', { name: 'Book permanently' }))

      expect(apiClient.post).toHaveBeenCalledWith('/expenses/e-2/book')
      expect(await screen.findByText(/This expense is booked/)).toBeVisible()
      expect(useToasts().toasts.value.map((x) => x.title)).toContain('The expense is booked.')
    })

    it('does not book when the confirmation is cancelled', async () => {
      mockApi({ expense: draft })
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      expect(apiClient.post).not.toHaveBeenCalled()
    })

    it('blocks booking while the form has unsaved changes', async () => {
      mockApi({ expense: draft })
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.type(await screen.findByLabelText('Supplier'), ' X')

      expect(screen.getByRole('button', { name: 'Book' })).toBeDisabled()
      expect(screen.getByText('Save the draft before you book it.')).toBeVisible()
    })

    it('offers no booking to a member', async () => {
      me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
      mockApi({ expense: draft })
      await renderView('/expenses/capture/e-2')

      await screen.findByLabelText('Supplier')
      expect(screen.queryByRole('button', { name: 'Book' })).toBeNull()
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeVisible()
    })

    it('names a 403 on booking and stays a draft', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.post).mockRejectedValue(apiError(403, ErrorCode.permissionDenied))
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Book permanently' }),
      )

      await vi.waitFor(() =>
        expect(useToasts().toasts.value.map((x) => x.title)).toContain(
          'You do not have permission for this.',
        ),
      )
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeVisible()
    })

    it('clears the session on a 401 during booking', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.post).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Book permanently' }),
      )

      await vi.waitFor(() => expect(me.value).toBeNull())
    })

    it('puts the field errors of a refused booking on the fields', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, { category: 'required for booking' }),
      )
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Book permanently' }),
      )

      expect(await screen.findByText('This field is required for booking.')).toBeVisible()
    })

    it('moves the focus to the first invalid field after a refused booking', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, { category: 'required for booking' }),
      )
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Book' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Book permanently' }),
      )

      // The dialog hands the focus back to its opener while it closes, and
      // the call runs after that: the category field ends up focused.
      await vi.waitFor(() =>
        expect(screen.getByRole('combobox', { name: /^Category/ })).toHaveFocus(),
      )
    })

    it('offers no Book and disables Save and Delete without a session', async () => {
      me.value = null
      mockApi({ expense: draft })
      await renderView('/expenses/capture/e-2')

      await screen.findByLabelText('Supplier')
      expect(screen.queryByRole('button', { name: 'Book' })).toBeNull()
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
      expect(screen.getByRole('button', { name: 'Delete draft' })).toBeDisabled()
    })

    it('deletes a draft only after the confirmation', async () => {
      mockApi({ expense: draft })
      vi.mocked(apiClient.del).mockResolvedValue(undefined)
      const user = userEvent.setup()
      const { router } = await renderView('/expenses/capture/e-2')

      await user.click(await screen.findByRole('button', { name: 'Delete draft' }))
      const dialog = await screen.findByRole('dialog')
      expect(apiClient.del).not.toHaveBeenCalled()
      await user.click(within(dialog).getByRole('button', { name: 'Delete draft' }))

      expect(apiClient.del).toHaveBeenCalledWith('/expenses/e-2')
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('expenses'))
    })
  })

  describe('booked rows', () => {
    it('are read only', async () => {
      mockApi({ expense: booked })
      await renderView('/expenses/capture/e-1')

      expect(await screen.findByText(/This expense is booked/)).toBeVisible()
      expect(screen.getByLabelText('Supplier')).toBeDisabled()
      expect(screen.getByLabelText(/^Gross amount/)).toBeDisabled()
      expect(screen.getByLabelText(/^Paid on/)).toBeDisabled()
      expect(screen.queryByRole('button', { name: 'Save draft' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Book' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Delete draft' })).toBeNull()
    })

    it('book a storno only after a confirmation with the date', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockResolvedValue({ ...booked, id: 'e-5', storno_of_id: 'e-1' })
      const user = userEvent.setup()
      const { router } = await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText(/A storno cannot be undone or edited/)).toBeVisible()
      expect(apiClient.post).not.toHaveBeenCalled()

      const date = within(dialog).getByLabelText(/Date the money flowed back/)
      await user.clear(date)
      await user.type(date, '2026-11-25')
      await user.click(within(dialog).getByRole('button', { name: 'Book the storno' }))

      expect(apiClient.post).toHaveBeenCalledWith('/expenses/e-1/storno', { paid_on: '2026-11-25' })
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('expenses'))
      expect(useToasts().toasts.value.map((x) => x.title)).toContain('The storno is booked.')
    })

    it('say so when a storno is refused with a 409', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockRejectedValue(apiError(409, ErrorCode.conflict))
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Book the storno' }))

      expect(await within(dialog).findByText(/cannot be reversed/)).toBeVisible()
    })

    it('show the negated amounts of a storno row with their sign', async () => {
      mockApi({
        expense: {
          ...booked,
          id: 'e-5',
          paid_on: '2027-02-01',
          storno_of_id: 'e-1',
          gross_cents: -11900,
          net_cents: -10000,
          vat_rate_permyriad: 1900,
          vat_cents: -1900,
        },
      })
      await renderView('/expenses/capture/e-5')

      expect(await screen.findByLabelText(/^Gross amount/)).toHaveValue('-119.00')
      expect(screen.getByLabelText('VAT amount')).toHaveValue('-19.00')
      expect(screen.getByLabelText('Net amount')).toHaveValue('-100.00')
      expect(screen.getByLabelText('VAT rate in percent')).toHaveValue('19.00')
      expect(screen.getByLabelText(/^Gross amount/)).toBeDisabled()
      expect(screen.getByLabelText('VAT amount')).toBeDisabled()
      expect(screen.queryByRole('button', { name: 'Save draft' })).toBeNull()
    })

    it('show a negated Kleinunternehmer storno amount with its sign', async () => {
      mockApi({
        expense: {
          ...booked,
          id: 'e-5',
          storno_of_id: 'e-1',
          gross_cents: -5950,
          net_cents: -5950,
        },
      })
      await renderView('/expenses/capture/e-5')

      expect(await screen.findByLabelText(/^Gross amount/)).toHaveValue('-59.50')
      expect(screen.getByLabelText(/^Gross amount/)).toBeDisabled()
      expect(screen.getByLabelText('Supplier')).toBeDisabled()
      expect(screen.queryByRole('button', { name: 'Save draft' })).toBeNull()
    })

    it('move the focus to the storno date when it is refused', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          paid_on: 'must not be before the paid_on of the original',
        }),
      )
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Book the storno' }))

      await vi.waitFor(() =>
        expect(within(dialog).getByLabelText(/Date the money flowed back/)).toHaveFocus(),
      )
    })

    it('put a date error of the storno on the date field', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockRejectedValue(
        apiError(400, ErrorCode.invalidArgument, {
          paid_on: 'must not be before the paid_on of the original',
        }),
      )
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Book the storno' }))

      expect(
        await within(dialog).findByText(
          'The date must not be before the payment date of the original.',
        ),
      ).toBeVisible()
    })

    it('clear the session on a 401 during the storno', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Book the storno' }),
      )

      await vi.waitFor(() => expect(me.value).toBeNull())
    })

    it('name a 403 during the storno', async () => {
      mockApi({ expense: booked })
      vi.mocked(apiClient.post).mockRejectedValue(apiError(403, ErrorCode.permissionDenied))
      const user = userEvent.setup()
      await renderView('/expenses/capture/e-1')

      await user.click(await screen.findByRole('button', { name: 'Reverse with a storno' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Book the storno' }))

      expect(await within(dialog).findByText('You do not have permission for this.')).toBeVisible()
    })

    it('offer no storno to a member', async () => {
      me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
      mockApi({ expense: booked })
      await renderView('/expenses/capture/e-1')

      await screen.findByText(/This expense is booked/)
      expect(screen.queryByRole('button', { name: 'Reverse with a storno' })).toBeNull()
    })

    it('offer no storno of a storno row, but a link to the original', async () => {
      mockApi({ expense: { ...booked, id: 'e-5', storno_of_id: 'e-1' } })
      const user = userEvent.setup()
      const { router } = await renderView('/expenses/capture/e-5')

      expect(await screen.findByText(/This row is a storno booking/)).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Reverse with a storno' })).toBeNull()
      await user.click(screen.getByRole('button', { name: 'Open the original expense' }))
      await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('e-1'))
    })
  })

  describe('receipt of a saved expense', () => {
    it('loads the file and shows the inline image', async () => {
      mockApi({ expense: draft, file: imageFile })
      await renderView('/expenses/capture/e-2')

      expect(await screen.findByRole('img', { name: 'Receipt receipt.png' })).toHaveAttribute(
        'src',
        '/api/v1/files/f-1/content',
      )
      expect(apiClient.get).toHaveBeenCalledWith('/files/f-1')
    })

    it('says so when the expense has no receipt', async () => {
      mockApi({ expense: { ...draft, file_id: null } })
      await renderView('/expenses/capture/e-2')

      expect(await screen.findByText('No receipt is attached.')).toBeVisible()
    })
  })
})
