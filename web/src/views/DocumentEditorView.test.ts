import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import { RouterView, createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import type { Doc, DocumentBody, PreviewAnswer } from '../documents'
import { de, en } from '../i18n/locales'
import { clearSession, me, sessionFailed } from '../session'
import DocumentEditorView from './DocumentEditorView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const customers = [
  { id: 'c-1', name: 'Customer Ltd', document_language: 'en' },
  { id: 'c-2', name: 'Kunde GmbH', document_language: 'de' },
]
const items = [
  {
    id: 'i-1',
    name: 'Consulting hour',
    description: null,
    unit_price_cents: 9500,
    unit: 'hour',
  },
]
const periods = [{ valid_from: '2026-01-01', regime: 'standard', superseded: false }]

function totalsOf(grand: number) {
  return {
    net_total_cents: 11111,
    document_discount_cents: 0,
    net_after_document_discount_cents: 11111,
    vat: [{ rate_permyriad: 1900, net_cents: 11111, vat_cents: 222 }],
    vat_total_cents: 222,
    grand_total_cents: grand,
  }
}

// The server's answer has nothing to do with the typed amounts on purpose:
// whatever the screen shows must come from here, not from the browser.
function previewAnswer(grand = 12345): PreviewAnswer {
  return {
    totals: totalsOf(grand),
    lines: [
      {
        net_after_line_discount_cents: 7777,
        document_discount_share_cents: 0,
        net_after_all_discounts_cents: 7777,
      },
    ],
  }
}

function draftDoc(overrides: Partial<Doc> = {}): Doc {
  return {
    id: 'd-1',
    type: 'invoice',
    state: 'draft',
    customer_id: 'c-1',
    corrects_document_id: null,
    source_offer_id: null,
    vat_regime: 'standard',
    language: 'en',
    service_date: '2026-11-01',
    service_date_to: null,
    due_date: '2026-12-15',
    valid_until: null,
    discount_percent_permyriad: null,
    discount_amount_cents: null,
    payment_terms: 'Net 14',
    notes: null,
    customer_reference: null,
    number: null,
    correction_seq: null,
    correction_base_seq: null,
    original_number: null,
    imported: false,
    chain_status: null,
    issued_at: null,
    lines: [
      {
        position: 1,
        catalogue_item_id: null,
        description: 'Consulting',
        quantity_thousandths: 2500,
        unit: 'hour',
        unit_price_cents: 9500,
        vat_rate_permyriad: 1900,
        line_discount_percent_permyriad: null,
        line_discount_amount_cents: null,
      },
    ],
    totals: totalsOf(12345),
    ...overrides,
  }
}

interface Recorded {
  previews: { body: DocumentBody; signal: AbortSignal | undefined }[]
  posts: { path: string; body: unknown }[]
  puts: { path: string; body: DocumentBody }[]
}
let rec: Recorded

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

interface Mock {
  stored?: Doc
  preview?: (body: DocumentBody) => Promise<PreviewAnswer>
  post?: (path: string) => Promise<unknown>
  put?: (path: string, body: DocumentBody) => Promise<unknown>
}

function mockApi(options: Mock = {}) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    switch (path) {
      case '/me':
      case '/status':
        return Promise.reject(apiError(503, ErrorCode.unavailable))
      case '/customers':
        return Promise.resolve({ customers })
      case '/catalogue-items':
        return Promise.resolve({ items })
      case '/vat-regimes':
        return Promise.resolve({ periods })
      default:
        return Promise.resolve(options.stored ?? draftDoc())
    }
  })
  vi.mocked(apiClient.post).mockImplementation(
    (path: string, body?: unknown, requestOptions?: { signal?: AbortSignal }) => {
      if (path === '/documents/preview') {
        rec.previews.push({ body: body as DocumentBody, signal: requestOptions?.signal })
        return options.preview
          ? options.preview(body as DocumentBody)
          : Promise.resolve(previewAnswer())
      }
      rec.posts.push({ path, body })
      if (options.post) {
        return options.post(path)
      }
      return Promise.resolve(draftDoc({ id: 'd-new' }))
    },
  )
  vi.mocked(apiClient.put).mockImplementation((path: string, body?: unknown) => {
    rec.puts.push({ path, body: body as DocumentBody })
    return options.put ? options.put(path, body as DocumentBody) : Promise.resolve(draftDoc())
  })
}

async function renderView(path = '/documents/edit', locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/documents', name: 'documents', component: { template: '<div />' } },
      { path: '/documents/edit/:id?', name: 'document-edit', component: DocumentEditorView },
      { path: '/documents/:id', name: 'document-detail', component: { template: '<div />' } },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  // Through a RouterView, like the app: the leave guard needs the route record.
  const Root = defineComponent({ components: { RouterView }, template: '<RouterView />' })
  const view = render(Root, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

/** choose opens a govue select by its name and picks an option by its text. */
async function choose(label: string | RegExp, option: string): Promise<void> {
  await fireEvent.keyDown(screen.getByRole('combobox', { name: label }), { key: 'ArrowDown' })
  const item = await screen.findByRole('option', { name: option })
  item.focus()
  await fireEvent.keyDown(item, { key: 'Enter' })
  await vi.waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
}

function toastTitles(): string[] {
  return useToasts().toasts.value.map((toast) => toast.title)
}

async function fillFirstLine(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText(/^Description \(1\)/), 'Consulting')
  await user.clear(screen.getByLabelText(/^Quantity \(1\)/))
  await user.type(screen.getByLabelText(/^Quantity \(1\)/), '2,5')
  await user.type(screen.getByLabelText(/^Unit price \(1\)/), '95')
  await user.type(screen.getByLabelText(/^VAT rate in percent \(1\)/), '19')
}

/** ready waits for the form: the page title shows before the data arrived. */
async function ready(): Promise<HTMLElement> {
  return screen.findByRole('button', { name: 'Save draft' })
}

/** clickIssue waits until the settled totals enable Issue, then clicks it. */
async function clickIssue(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Issue' })).toBeEnabled())
  await user.click(screen.getByRole('button', { name: 'Issue' }))
}

const wait = (ms = 20) => new Promise((resolve) => setTimeout(resolve, ms))

describe('DocumentEditorView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    rec = { previews: [], posts: [], puts: [] }
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  describe('a new document', () => {
    it('starts with the regime of today and asks for no totals while the line is incomplete', async () => {
      mockApi()
      await renderView()

      expect(await ready()).toBeVisible()
      expect(screen.getByRole('combobox', { name: 'VAT regime' })).toHaveTextContent(
        'Standard (with VAT)',
      )
      expect(await screen.findByText('Complete the lines to see the totals.')).toBeVisible()
      await wait(400)
      expect(rec.previews).toHaveLength(0)
    })

    it('starts an offer from the address', async () => {
      mockApi()
      await renderView('/documents/edit?type=offer')

      expect(await screen.findByRole('heading', { name: 'New offer' })).toBeVisible()
      expect(screen.getByLabelText('Valid until')).toBeVisible()
      expect(screen.queryByLabelText('Service date')).toBeNull()
    })

    it('shows exactly the totals the server answers and the line net, computing nothing itself', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()

      await fillFirstLine(user)

      const totals = await screen.findByTestId('preview-totals')
      expect(within(totals).getByText('123.45 EUR')).toBeVisible()
      expect(within(totals).getByText('111.11 EUR')).toBeVisible()
      expect(within(totals).getByText('2.22 EUR')).toBeVisible()
      expect(screen.getByTestId('line-net-0')).toHaveTextContent('77.77 EUR')
      expect(rec.previews.at(-1)?.body.lines[0]).toEqual({
        catalogue_item_id: null,
        description: 'Consulting',
        quantity_thousandths: 2500,
        unit: 'piece',
        unit_price_cents: 9500,
        vat_rate_permyriad: 1900,
        line_discount_percent_permyriad: null,
        line_discount_amount_cents: null,
      })
    })

    it('shows the German strings and number format', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit', 'de')

      expect(await screen.findByRole('heading', { name: 'Neue Rechnung' })).toBeVisible()
      await screen.findByRole('button', { name: 'Entwurf speichern' })
      await user.type(screen.getByLabelText(/^Beschreibung \(1\)/), 'Beratung')
      await user.clear(screen.getByLabelText(/^Menge \(1\)/))
      await user.type(screen.getByLabelText(/^Menge \(1\)/), '2,5')
      await user.type(screen.getByLabelText(/^Einzelpreis \(1\)/), '95,50')
      await user.type(screen.getByLabelText(/^USt-Satz in Prozent \(1\)/), '19')

      expect(
        await within(await screen.findByTestId('preview-totals')).findByText('123,45 EUR'),
      ).toBeVisible()
      expect(rec.previews.at(-1)?.body.lines[0]).toMatchObject({
        unit_price_cents: 9550,
        quantity_thousandths: 2500,
      })
    })

    it('asks for the totals once after typing stops', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), 'Consulting')
      await user.type(screen.getByLabelText(/^VAT rate in percent \(1\)/), '19')

      await user.type(screen.getByLabelText(/^Unit price \(1\)/), '12345')
      await wait(700)

      expect(rec.previews).toHaveLength(1)
      expect(rec.previews[0]?.body.lines[0]?.unit_price_cents).toBe(1234500)
    })

    it('fills a line from the catalogue and keeps the catalogue id', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()

      await choose('Catalogue item (1)', 'Consulting hour')
      await user.type(screen.getByLabelText(/^VAT rate in percent \(1\)/), '19')

      expect(screen.getByLabelText(/^Description \(1\)/)).toHaveValue('Consulting hour')
      expect(screen.getByLabelText(/^Unit price \(1\)/)).toHaveValue('95.00')
      expect(screen.getByRole('combobox', { name: 'Unit (1)' })).toHaveTextContent('Hour')
      await vi.waitFor(() => expect(rec.previews).toHaveLength(1))
      expect(rec.previews[0]?.body.lines[0]).toMatchObject({
        catalogue_item_id: 'i-1',
        unit: 'hour',
        unit_price_cents: 9500,
      })
    })

    it('sends a line discount and a document discount as cents and permyriad', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await fillFirstLine(user)

      await choose('Line discount (1)', 'Percentage')
      await user.type(screen.getByLabelText('Discount in percent (1)'), '7,5')
      await choose('Document discount', 'Amount')
      await user.type(screen.getByLabelText('Discount amount'), '5,50')

      await vi.waitFor(() => {
        const body = rec.previews.at(-1)?.body
        expect(body?.lines[0]?.line_discount_percent_permyriad).toBe(750)
        expect(body?.discount_amount_cents).toBe(550)
        expect(body?.discount_percent_permyriad).toBeNull()
      })
    })

    it('fixes the rate at 0 under the Kleinunternehmer regime', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), 'Consulting')
      await user.type(screen.getByLabelText(/^Unit price \(1\)/), '95')
      await user.type(screen.getByLabelText(/^VAT rate in percent \(1\)/), '19')

      await choose('VAT regime', 'Kleinunternehmer (no VAT)')

      const rate = screen.getByLabelText(/^VAT rate in percent \(1\)/)
      expect(rate).toHaveValue('0')
      expect(rate).toHaveAttribute('readonly')
      await vi.waitFor(() =>
        expect(rec.previews.at(-1)?.body).toMatchObject({ vat_regime: 'kleinunternehmer' }),
      )
      expect(rec.previews.at(-1)?.body.lines[0]?.vat_rate_permyriad).toBe(0)
      expect(screen.getByText(/no VAT is charged/)).toBeVisible()
    })

    it('puts the field error of a tax exempt line at its line before any save', async () => {
      mockApi({
        preview: () =>
          Promise.reject(
            apiError(400, ErrorCode.invalidArgument, {
              'lines[0].vat_rate_permyriad':
                'a tax exempt line (rate 0) needs an exemption reason under the standard regime, which is not supported yet',
            }),
          ),
      })
      const user = userEvent.setup()
      await renderView()
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), 'Consulting')
      await user.type(screen.getByLabelText(/^Unit price \(1\)/), '95')
      await user.type(screen.getByLabelText(/^VAT rate in percent \(1\)/), '0')

      expect(await screen.findByText(/A line without VAT needs an exemption reason/)).toBeVisible()
      expect(
        within(screen.getByTestId('preview')).getByText(
          'The server cannot calculate these lines. Check the marked fields.',
        ),
      ).toBeVisible()
      expect(screen.queryByText(/a tax exempt line/)).toBeNull()
    })
  })

  describe('the preview', () => {
    function deferredPreview() {
      const calls: { resolve: (v: PreviewAnswer) => void; reject: (e: unknown) => void }[] = []
      mockApi({
        preview: () =>
          new Promise<PreviewAnswer>((resolve, reject) => {
            calls.push({ resolve, reject })
          }),
      })
      return calls
    }

    it('aborts the request in flight when a newer one starts and drops a late answer', async () => {
      const calls = deferredPreview()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await fillFirstLine(user)
      await vi.waitFor(() => expect(calls).toHaveLength(1))

      await user.type(screen.getByLabelText(/^Unit price \(1\)/), '1')
      await vi.waitFor(() => expect(calls).toHaveLength(2))
      expect(rec.previews[0]?.signal?.aborted).toBe(true)
      expect(rec.previews[1]?.signal?.aborted).toBe(false)

      calls[1]?.resolve(previewAnswer(22222))
      expect(
        await within(await screen.findByTestId('preview-totals')).findByText('222.22 EUR'),
      ).toBeVisible()
      calls[0]?.resolve(previewAnswer(99999))
      await wait()

      expect(screen.getByText('222.22 EUR')).toBeVisible()
      expect(screen.queryByText('999.99 EUR')).toBeNull()
    })

    it('ignores an older failure and an older 401 after a newer answer', async () => {
      const calls = deferredPreview()
      const user = userEvent.setup()
      const { router } = await renderView()
      await ready()
      await fillFirstLine(user)
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      await user.type(screen.getByLabelText(/^Unit price \(1\)/), '1')
      await vi.waitFor(() => expect(calls).toHaveLength(2))
      const push = vi.spyOn(router, 'push')

      calls[1]?.resolve(previewAnswer(22222))
      await screen.findByText('222.22 EUR')
      calls[0]?.reject(apiError(401, ErrorCode.unauthenticated))
      await wait()

      expect(screen.queryByText('The totals could not be calculated.')).toBeNull()
      expect(screen.getByText('222.22 EUR')).toBeVisible()
      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
    })

    it('shows a failed preview and calculates again on request', async () => {
      let fail = true
      mockApi({
        preview: () =>
          fail
            ? Promise.reject(apiError(500, ErrorCode.internal))
            : Promise.resolve(previewAnswer(33333)),
      })
      const user = userEvent.setup()
      await renderView()
      await ready()
      await fillFirstLine(user)

      expect(
        await within(screen.getByTestId('preview')).findByText(
          'The totals could not be calculated.',
        ),
      ).toBeVisible()
      fail = false
      await user.click(screen.getByRole('button', { name: 'Calculate again' }))

      expect(await screen.findByText('333.33 EUR')).toBeVisible()
    })

    it('aborts the preview on unmount and changes nothing when a 401 arrives afterwards', async () => {
      const calls = deferredPreview()
      const user = userEvent.setup()
      const { router, unmount } = await renderView()
      await ready()
      await fillFirstLine(user)
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      const signal = rec.previews[0]?.signal
      expect(signal?.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      expect(signal?.aborted).toBe(true)
      calls[0]?.reject(apiError(401, ErrorCode.unauthenticated))
      await wait()

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })
  })

  describe('saving', () => {
    it('creates the draft with the typed values and keeps the editor alive under the new address', async () => {
      mockApi({ post: () => Promise.resolve(draftDoc({ id: 'd-new' })) })
      const user = userEvent.setup()
      const { router } = await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await user.type(screen.getByLabelText('Service date'), '2026-11-01')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(rec.posts).toHaveLength(1))
      expect(rec.posts[0]?.path).toBe('/documents')
      expect(rec.posts[0]?.body).toMatchObject({
        type: 'invoice',
        customer_id: 'c-1',
        vat_regime: 'standard',
        language: 'en',
        service_date: '2026-11-01',
      })
      await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('d-new'))
      expect(toastTitles()).toContain('The draft was saved.')
      expect(await ready()).toBeVisible()
      // The lines are still there: the view was not remounted.
      expect(screen.getByLabelText(/^Description \(1\)/)).toHaveValue('Consulting')
    })

    it('takes the language of the chosen customer', async () => {
      mockApi()
      await renderView()
      await ready()

      await choose(/^Customer/, 'Kunde GmbH')

      expect(screen.getByRole('combobox', { name: 'Language of the document' })).toHaveTextContent(
        'German',
      )
    })

    it('does not save without a customer and moves the focus to the first invalid field', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await fillFirstLine(user)

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() =>
        expect(screen.getByRole('combobox', { name: /^Customer/ })).toHaveFocus(),
      )
      expect(screen.getByText('The draft was not saved. Check the marked fields.')).toBeVisible()
      expect(rec.posts).toHaveLength(0)
    })

    it('does not save an amount that is no amount', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await user.clear(screen.getByLabelText(/^Unit price \(1\)/))
      await user.type(screen.getByLabelText(/^Unit price \(1\)/), 'abc')

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(screen.getByLabelText(/^Unit price \(1\)/)).toHaveFocus())
      expect(rec.posts).toHaveLength(0)
    })

    it('puts the server field errors at their fields in the visitor language', async () => {
      mockApi({
        post: () =>
          Promise.reject(
            apiError(400, ErrorCode.invalidArgument, {
              service_date: 'must be a date such as 2026-11-30',
              'lines[0].description': 'required',
            }),
          ),
      })
      const user = userEvent.setup()
      await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      expect(await screen.findByText('Enter a date such as 2026-11-30.')).toBeVisible()
      expect(screen.getByText('This field is required.')).toBeVisible()
      await vi.waitFor(() => expect(screen.getByLabelText('Service date')).toHaveFocus())
    })

    it('keeps the save disabled without a session and says why', async () => {
      me.value = null
      sessionFailed.value = true
      mockApi()
      await renderView()

      expect(await screen.findByRole('button', { name: 'Save draft' })).toBeDisabled()
      expect(screen.getByText(/The session could not be loaded/)).toBeVisible()
      expect(rec.posts).toHaveLength(0)
    })

    it('does not post when the session is gone by the time the form is sent', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)

      me.value = null
      const form = document.getElementById('document-form') as HTMLFormElement
      await fireEvent.submit(form)
      await wait()

      expect(rec.posts).toHaveLength(0)
      expect(screen.getAllByText(/The session could not be loaded/).length).toBeGreaterThan(0)
    })

    it('goes nowhere when a save answers 401 after unmount', async () => {
      let reject: (error: unknown) => void = () => {}
      mockApi({
        post: () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      })
      const user = userEvent.setup()
      const { router, unmount } = await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.posts).toHaveLength(1))

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      reject(apiError(401, ErrorCode.unauthenticated))
      await wait()

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })
  })

  describe('a stored draft', () => {
    it('fills the form from the draft and saves it with a PUT', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')

      expect(await ready()).toBeVisible()
      expect(screen.getByLabelText(/^Description \(1\)/)).toHaveValue('Consulting')
      expect(screen.getByLabelText(/^Quantity \(1\)/)).toHaveValue('2.5')
      expect(screen.getByLabelText(/^Unit price \(1\)/)).toHaveValue('95.00')
      expect(screen.getByLabelText(/^VAT rate in percent \(1\)/)).toHaveValue('19')
      expect(screen.getByLabelText('Due date')).toHaveValue('2026-12-15')
      expect(await screen.findByText('123.45 EUR')).toBeVisible()

      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))
      expect(rec.puts[0]?.path).toBe('/documents/d-1')
      expect(rec.puts[0]?.body).toMatchObject({
        type: 'invoice',
        customer_id: 'c-1',
        vat_regime: 'standard',
      })
      expect(rec.puts[0]?.body.lines[0]?.description).toBe('Consulting work')
      expect(rec.posts.filter((p) => p.path === '/documents')).toHaveLength(0)
    })

    it('locks the customer, the regime and the language of a correction draft', async () => {
      mockApi({
        stored: draftDoc({
          type: 'correction',
          corrects_document_id: 'd-0',
          correction_base_seq: 1,
        }),
      })
      await renderView('/documents/edit/d-1')

      expect(await screen.findByText(/starts as a copy of the effective document/)).toBeVisible()
      expect(screen.getByRole('combobox', { name: /^Customer/ })).toBeDisabled()
      expect(screen.getByRole('combobox', { name: 'VAT regime' })).toBeDisabled()
      expect(screen.getByRole('combobox', { name: 'Language of the document' })).toBeDisabled()
    })

    it('does not open an issued document for editing', async () => {
      mockApi({ stored: draftDoc({ state: 'issued', number: 'RE-20260009' }) })
      await renderView('/documents/edit/d-1')

      expect(await screen.findByText(/cannot be edited any more/)).toBeVisible()
      expect(screen.getByRole('link', { name: 'Open the document' })).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Save draft' })).toBeNull()
    })

    it('says that the draft does not exist on a 404', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) =>
        path.startsWith('/documents/')
          ? Promise.reject(apiError(404, ErrorCode.notFound))
          : Promise.resolve({ customers, items, periods }),
      )
      await renderView('/documents/edit/d-1')

      expect(await screen.findByText('Document not found')).toBeVisible()
    })

    it('clears the session and goes to the login on a 401', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const { router } = await renderView('/documents/edit/d-1')

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('shows the error state and retries', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) =>
        path.startsWith('/documents/')
          ? Promise.reject(apiError(500, ErrorCode.internal))
          : Promise.resolve({ customers, items, periods }),
      )
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')

      expect(await screen.findByRole('alert')).toHaveTextContent('The draft could not be loaded.')
      mockApi()
      await user.click(screen.getByRole('button', { name: 'Try again' }))

      expect(await ready()).toBeVisible()
    })
  })

  describe('issuing from the editor', () => {
    it('saves a changed draft first, then asks, and sends nothing until confirmed', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')

      await clickIssue(user)
      const dialog = await screen.findByRole('dialog')

      expect(rec.puts).toHaveLength(1)
      expect(within(dialog).getByText(/can never be changed or deleted again/)).toBeVisible()
      await wait()
      expect(rec.posts.filter((p) => p.path.endsWith('/issue'))).toHaveLength(0)

      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))
      await vi.waitFor(() => expect(rec.posts.map((p) => p.path)).toContain('/documents/d-1/issue'))
    })

    it('does not save again when nothing changed', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()

      await clickIssue(user)
      await screen.findByRole('dialog')

      expect(rec.puts).toHaveLength(0)
    })

    it('does not open the confirmation when the save fails', async () => {
      mockApi({ put: () => Promise.reject(apiError(500, ErrorCode.internal)) })
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')

      await clickIssue(user)
      await wait(50)

      expect(screen.queryByRole('dialog')).toBeNull()
      expect(screen.getByText('The server had an error. Try again later.')).toBeVisible()
    })

    it('puts the field errors of a refused issue at their fields and lists the rest', async () => {
      mockApi({
        post: () =>
          Promise.reject(
            apiError(400, ErrorCode.invalidArgument, {
              'lines[0].description': 'required',
              'seller.tax_number': 'a Steuernummer or USt-IdNr is required',
            }),
          ),
      })
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()

      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )

      expect(await screen.findByText('This field is required.')).toBeVisible()
      await vi.waitFor(() => expect(screen.getByLabelText(/^Description \(1\)/)).toHaveFocus())
      expect(screen.getByText('Seller tax number')).toBeVisible()
      expect(screen.queryByText('seller.tax_number')).toBeNull()
      expect(
        screen.getByText(/A tax number or VAT ID is required in the seller profile/),
      ).toBeVisible()
    })

    it('says that issuing is off on 503 renderer_unavailable and keeps the draft', async () => {
      mockApi({ post: () => Promise.reject(apiError(503, 'renderer_unavailable')) })
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()

      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )

      expect(await screen.findByTestId('issue-error')).toHaveTextContent(
        'Issuing is not available yet.',
      )
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeVisible()
    })

    it('opens the issued document after a successful issue', async () => {
      mockApi({
        post: () =>
          Promise.resolve(
            draftDoc({ state: 'issued', number: 'RE-20260009', chain_status: 'effective' }),
          ),
      })
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()

      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-detail'))
      expect(router.currentRoute.value.params.id).toBe('d-1')
    })

    it('offers a member no issue action', async () => {
      me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
      mockApi()
      await renderView('/documents/edit/d-1')
      await ready()

      expect(screen.queryByRole('button', { name: 'Issue' })).toBeNull()
      expect(screen.getByText('Only the owner issues documents.')).toBeVisible()
    })
  })

  describe('issue waits for the settled totals', () => {
    const issueButton = () => screen.getByRole('button', { name: 'Issue' })

    it('is enabled once the totals are settled and says nothing then', async () => {
      mockApi()
      await renderView('/documents/edit/d-1')
      await ready()

      await vi.waitFor(() => expect(issueButton()).toBeEnabled())
      expect(screen.queryByText(/Issuing waits/)).toBeNull()
    })

    it('is disabled during the debounce, while the old totals are still on screen', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await vi.waitFor(() => expect(issueButton()).toBeEnabled())
      await screen.findByText('123.45 EUR')

      await user.type(screen.getByLabelText(/^Description \(1\)/), ' x')

      expect(issueButton()).toBeDisabled()
      expect(screen.getByText('Issuing waits until the totals are calculated.')).toBeVisible()
      expect(screen.getByText('123.45 EUR')).toBeVisible()
      await vi.waitFor(() => expect(issueButton()).toBeEnabled())
    })

    it('is disabled while the preview request is in flight and enabled when it answers', async () => {
      const calls: ((v: PreviewAnswer) => void)[] = []
      mockApi({
        preview: () =>
          new Promise<PreviewAnswer>((resolve) => {
            calls.push(resolve)
          }),
      })
      await renderView('/documents/edit/d-1')
      await ready()
      await vi.waitFor(() => expect(calls).toHaveLength(1))

      expect(issueButton()).toBeDisabled()
      expect(screen.getByText('Issuing waits until the totals are calculated.')).toBeVisible()
      calls[0]?.(previewAnswer())

      await vi.waitFor(() => expect(issueButton()).toBeEnabled())
    })

    it('is disabled after a failed preview', async () => {
      mockApi({ preview: () => Promise.reject(apiError(500, ErrorCode.internal)) })
      await renderView('/documents/edit/d-1')
      await ready()

      expect(
        await screen.findByText('The totals could not be calculated. Calculate again to issue.'),
      ).toBeVisible()
      expect(issueButton()).toBeDisabled()
    })

    it('is disabled when the server cannot calculate the lines', async () => {
      mockApi({
        preview: () =>
          Promise.reject(
            apiError(400, ErrorCode.invalidArgument, {
              'lines[0].vat_rate_permyriad': 'must be 0 under the Kleinunternehmer regime',
            }),
          ),
      })
      await renderView('/documents/edit/d-1')
      await ready()

      expect(await screen.findByText(/Fix the marked fields to issue/)).toBeVisible()
      expect(issueButton()).toBeDisabled()
    })

    it('is disabled while the lines are incomplete', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await vi.waitFor(() => expect(issueButton()).toBeEnabled())

      await user.clear(screen.getByLabelText(/^Description \(1\)/))

      expect(await screen.findByText(/Complete the lines first/)).toBeVisible()
      expect(issueButton()).toBeDisabled()
    })

    it('shows the totals the server stored for the saved draft, not the preview', async () => {
      mockApi({ put: () => Promise.resolve(draftDoc({ totals: totalsOf(55555) })) })
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')

      await clickIssue(user)
      const dialog = await screen.findByRole('dialog')

      expect(
        within(dialog).getByText(
          'Invoice, Customer Ltd: Total 555.55 EUR (net 111.11 EUR, VAT 2.22 EUR)',
        ),
      ).toBeVisible()
      expect(within(dialog).queryByText(/123\.45/)).toBeNull()
    })

    it('saves first and opens the confirmation only after the save answered', async () => {
      let answer: (doc: Doc) => void = () => {}
      mockApi({
        put: () =>
          new Promise((resolve) => {
            answer = resolve
          }),
      })
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')
      await clickIssue(user)
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))

      await wait(50)
      expect(screen.queryByRole('dialog')).toBeNull()
      answer(draftDoc({ totals: totalsOf(77777) }))

      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText(/Total 777\.77 EUR/)).toBeVisible()
    })

    it('shows the totals of the loaded draft when nothing was changed', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()

      await clickIssue(user)
      const dialog = await screen.findByRole('dialog')

      expect(within(dialog).getByText(/Total 123\.45 EUR/)).toBeVisible()
      expect(rec.puts).toHaveLength(0)
    })
  })

  describe('answers that arrive late', () => {
    it('does not move to the new address or announce a save after unmount', async () => {
      let answer: (doc: Doc) => void = () => {}
      mockApi({
        post: () =>
          new Promise((resolve) => {
            answer = resolve
          }),
      })
      const user = userEvent.setup()
      const { router, unmount } = await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.posts).toHaveLength(1))

      const replace = vi.spyOn(router, 'replace')
      const push = vi.spyOn(router, 'push')
      unmount()
      answer(draftDoc({ id: 'd-new' }))
      await wait(30)

      expect(replace).not.toHaveBeenCalled()
      expect(push).not.toHaveBeenCalled()
      expect(toastTitles()).not.toContain('The draft was saved.')
    })

    it('shows the draft of the address the visitor is on, not an older one that answers late', async () => {
      mockApi()
      const base = vi.mocked(apiClient.get).getMockImplementation()
      const pending: Record<string, (doc: Doc) => void> = {}
      vi.mocked(apiClient.get).mockImplementation((path: string, options?: unknown) =>
        path.startsWith('/documents/d-')
          ? new Promise((resolve) => {
              pending[path] = resolve
            })
          : (base?.(path, options as never) ?? Promise.reject(new Error('no mock'))),
      )
      const { router } = await renderView('/documents/edit/d-1')
      await vi.waitFor(() => expect(pending['/documents/d-1']).toBeDefined())
      await router.push('/documents/edit/d-2')
      await vi.waitFor(() => expect(pending['/documents/d-2']).toBeDefined())
      const withDescription = (id: string, description: string) =>
        draftDoc({
          id,
          lines: (draftDoc().lines ?? []).map((line) => ({ ...line, description })),
        })

      pending['/documents/d-2']?.(withDescription('d-2', 'Newer draft'))
      expect(await screen.findByDisplayValue('Newer draft')).toBeVisible()
      pending['/documents/d-1']?.(withDescription('d-1', 'Older draft'))
      await wait(30)

      expect(screen.getByLabelText(/^Description \(1\)/)).toHaveValue('Newer draft')
      expect(screen.queryByDisplayValue('Older draft')).toBeNull()
    })
  })

  describe('discount errors', () => {
    it('marks the line discount value, not the mode, and moves the focus there', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await choose('Line discount (1)', 'Percentage')
      await user.type(screen.getByLabelText('Discount in percent (1)'), 'x')

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      const value = screen.getByLabelText('Discount in percent (1)')
      await vi.waitFor(() => expect(value).toHaveFocus())
      expect(value).toHaveAttribute('aria-invalid', 'true')
      expect(screen.getByRole('combobox', { name: 'Line discount (1)' })).not.toHaveAttribute(
        'aria-invalid',
      )
      expect(screen.getByText(/Enter a percentage from 0 to 100/)).toBeVisible()
      expect(rec.posts).toHaveLength(0)
    })

    it('marks the document discount value and moves the focus there', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()
      await choose(/^Customer/, 'Customer Ltd')
      await fillFirstLine(user)
      await choose('Document discount', 'Amount')
      await user.type(screen.getByLabelText('Discount amount'), 'x')

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      const value = screen.getByLabelText('Discount amount')
      await vi.waitFor(() => expect(value).toHaveFocus())
      expect(value).toHaveAttribute('aria-invalid', 'true')
      expect(screen.getByRole('combobox', { name: 'Document discount' })).not.toHaveAttribute(
        'aria-invalid',
      )
    })
  })

  describe('the live region', () => {
    it('announces only the settled grand total, not every step of a preview cycle', async () => {
      const calls: ((v: PreviewAnswer) => void)[] = []
      mockApi({
        preview: () =>
          new Promise<PreviewAnswer>((resolve) => {
            calls.push(resolve)
          }),
      })
      await renderView('/documents/edit/d-1')
      await ready()
      await vi.waitFor(() => expect(calls).toHaveLength(1))

      // While the request is in flight nothing is announced.
      expect(screen.getByTestId('live-summary')).toBeEmptyDOMElement()
      calls[0]?.(previewAnswer(33333))

      await vi.waitFor(() =>
        expect(screen.getByTestId('live-summary')).toHaveTextContent('Total 333.33 EUR.'),
      )
      // The list of totals itself is no live region.
      expect(screen.getByTestId('preview')).not.toHaveAttribute('aria-live')
      expect(screen.getByTestId('preview').closest('[aria-live]')).toBeNull()
      expect(screen.getByTestId('live-summary')).toHaveAttribute('aria-live', 'polite')
    })

    it('announces a failed preview', async () => {
      mockApi({ preview: () => Promise.reject(apiError(500, ErrorCode.internal)) })
      await renderView('/documents/edit/d-1')
      await ready()

      await vi.waitFor(() =>
        expect(screen.getByTestId('live-summary')).toHaveTextContent(
          'The totals could not be calculated.',
        ),
      )
    })
  })

  describe('edits during a save', () => {
    const description = () => screen.getByLabelText(/^Description \(1\)/)

    /** deferredPut answers the PUT when the test says so. */
    function deferredPut() {
      const control: { answer: (doc: Doc) => void } = { answer: () => {} }
      mockApi({
        put: () =>
          new Promise((resolve) => {
            control.answer = resolve
          }),
      })
      return control
    }

    it('blocks typing while the save is in flight', async () => {
      const put = deferredPut()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(description(), ' one')

      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))

      expect(description()).toBeDisabled()
      expect(document.querySelector('#document-form button[type="submit"]')).toBeDisabled()
      put.answer(draftDoc())
      await vi.waitFor(() => expect(description()).toBeEnabled())
    })

    it('does not mark an edit made during the save as saved: the leave guard still asks', async () => {
      const put = deferredPut()
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await user.type(description(), ' one')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))

      // A script or an assistive tool still gets an input event through.
      await fireEvent.update(description(), 'Consulting one MORE')
      put.answer(draftDoc())
      await vi.waitFor(() => expect(description()).toBeEnabled())
      void router.push('/documents')

      expect(await screen.findByText('Leave without saving?')).toBeVisible()
      expect(router.currentRoute.value.name).toBe('document-edit')
      expect(rec.puts[0]?.body.lines[0]?.description).toBe('Consulting one')
    })

    it('asks the browser to confirm a reload after such an edit', async () => {
      const put = deferredPut()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(description(), ' one')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))
      await fireEvent.update(description(), 'Consulting one MORE')
      put.answer(draftDoc())
      await vi.waitFor(() => expect(description()).toBeEnabled())

      await wait(30)
      const event = new Event('beforeunload', { cancelable: true })
      window.dispatchEvent(event)

      expect(event.defaultPrevented).toBe(true)
    })

    it('blocks typing while the save before an issue is in flight and does not offer an issue with an edit made meanwhile', async () => {
      const put = deferredPut()
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await user.type(description(), ' one')
      await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Issue' })).toBeEnabled())

      await user.click(screen.getByRole('button', { name: 'Issue' }))
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))
      expect(description()).toBeDisabled()
      await fireEvent.update(description(), 'Consulting one MORE')
      put.answer(draftDoc())

      expect(await screen.findByText(/changed while it was being saved/)).toBeVisible()
      await wait(30)
      expect(screen.queryByRole('dialog')).toBeNull()
      expect(rec.posts.filter((p) => p.path.endsWith('/issue'))).toHaveLength(0)
    })
  })

  describe('leaving without a question', () => {
    it('does not ask when the session is gone', async () => {
      mockApi({ put: () => Promise.reject(apiError(401, ErrorCode.unauthenticated)) })
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' one')

      await user.click(screen.getByRole('button', { name: 'Save draft' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
      expect(screen.queryByText('Leave without saving?')).toBeNull()
    })

    it('does not ask after the stale draft was discarded', async () => {
      mockApi({
        stored: draftDoc({ type: 'correction', corrects_document_id: 'd-0' }),
        post: () =>
          Promise.reject(apiError(409, 'correction_base_changed', { correction_base_seq: '2' })),
      })
      vi.mocked(apiClient.del).mockResolvedValue(undefined)
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )
      const discard = await screen.findByRole('button', { name: 'Discard the old draft' })
      // An unsaved edit makes the form dirty before the discard.
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' more')
      await user.click(discard)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Discard draft' }),
      )

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('documents'))
      expect(screen.queryByText('Leave without saving?')).toBeNull()
    })

    it('asks again after a confirmed move that another guard cancelled', async () => {
      mockApi()
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' one')
      router.beforeEach((to) => to.name !== 'settings')

      void router.push('/settings')
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Leave without saving',
        }),
      )
      await wait(40)
      expect(router.currentRoute.value.name).toBe('document-edit')

      void router.push('/documents')
      expect(await screen.findByText('Leave without saving?')).toBeVisible()
    })
  })

  describe('unsaved changes', () => {
    it('does not ask when nothing changed', async () => {
      mockApi()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()

      await router.push('/documents')

      expect(router.currentRoute.value.name).toBe('documents')
    })

    it('asks before leaving with changes, stays on cancel and leaves on confirm', async () => {
      mockApi()
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')

      void router.push('/documents')
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('Leave without saving?')).toBeVisible()
      expect(router.currentRoute.value.name).toBe('document-edit')

      await user.click(within(dialog).getByRole('button', { name: 'Keep editing' }))
      await wait(30)
      expect(router.currentRoute.value.name).toBe('document-edit')
      expect(screen.getByLabelText(/^Description \(1\)/)).toHaveValue('Consulting work')

      void router.push('/documents')
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Leave without saving',
        }),
      )
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('documents'))
    })

    it('does not ask after a save', async () => {
      mockApi()
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')
      await user.click(screen.getByRole('button', { name: 'Save draft' }))
      await vi.waitFor(() => expect(rec.puts).toHaveLength(1))

      await router.push('/documents')

      expect(router.currentRoute.value.name).toBe('documents')
    })

    it('asks the browser to confirm a reload only while there are unsaved changes', async () => {
      mockApi()
      const user = userEvent.setup()
      const { unmount } = await renderView('/documents/edit/d-1')
      await ready()
      const unload = () => {
        const event = new Event('beforeunload', { cancelable: true })
        window.dispatchEvent(event)
        return event.defaultPrevented
      }
      expect(unload()).toBe(false)

      await user.type(screen.getByLabelText(/^Description \(1\)/), ' work')
      expect(unload()).toBe(true)

      unmount()
      expect(unload()).toBe(false)
    })
  })

  describe('after a changed correction base', () => {
    function baseChanged() {
      return mockApi({
        stored: draftDoc({ type: 'correction', corrects_document_id: 'd-0' }),
        post: (path) =>
          path.endsWith('/issue')
            ? Promise.reject(apiError(409, 'correction_base_changed', { correction_base_seq: '2' }))
            : Promise.resolve(draftDoc({ id: 'd-9' })),
      })
    }

    it('offers to discard the stale draft only after a confirmation', async () => {
      baseChanged()
      vi.mocked(apiClient.del).mockResolvedValue(undefined)
      const user = userEvent.setup()
      const { router } = await renderView('/documents/edit/d-1')
      await ready()
      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )

      await user.click(await screen.findByRole('button', { name: 'Discard the old draft' }))
      const dialog = await screen.findByRole('dialog')
      await wait(20)
      expect(vi.mocked(apiClient.del)).not.toHaveBeenCalled()
      await user.click(within(dialog).getByRole('button', { name: 'Discard draft' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('documents'))
      expect(vi.mocked(apiClient.del).mock.calls.map(([path]) => path)).toEqual(['/documents/d-1'])
    })

    it('does not delete without a session', async () => {
      baseChanged()
      vi.mocked(apiClient.del).mockResolvedValue(undefined)
      const user = userEvent.setup()
      await renderView('/documents/edit/d-1')
      await ready()
      await clickIssue(user)
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', {
          name: 'Issue permanently',
        }),
      )
      await user.click(await screen.findByRole('button', { name: 'Discard the old draft' }))
      const dialog = await screen.findByRole('dialog')

      me.value = null
      await user.click(within(dialog).getByRole('button', { name: 'Discard draft' }))
      await wait(30)

      expect(vi.mocked(apiClient.del)).not.toHaveBeenCalled()
    })
  })

  describe('lines', () => {
    it('adds and removes lines', async () => {
      mockApi()
      const user = userEvent.setup()
      await renderView()
      await ready()

      await user.click(screen.getByRole('button', { name: 'Add line' }))
      expect(screen.getByLabelText(/^Description \(2\)/)).toBeVisible()

      await user.click(screen.getByRole('button', { name: 'Remove line 1' }))
      expect(screen.queryByLabelText(/^Description \(2\)/)).toBeNull()
      expect(screen.getByLabelText(/^Description \(1\)/)).toBeVisible()
    })
  })
})
