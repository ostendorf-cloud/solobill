import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import type { Doc, PaymentsAnswer } from '../documents'
import { de, en } from '../i18n/locales'
import { clearSession, me, sessionFailed } from '../session'
import DocumentDetailView from './DocumentDetailView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const totals = {
  net_total_cents: 20000,
  document_discount_cents: 0,
  net_after_document_discount_cents: 20000,
  vat: [{ rate_permyriad: 1900, net_cents: 20000, vat_cents: 3800 }],
  vat_total_cents: 3800,
  grand_total_cents: 23800,
}

function doc(overrides: Partial<Doc>): Doc {
  return {
    id: 'd-1',
    type: 'invoice',
    state: 'draft',
    customer_id: 'c-1',
    corrects_document_id: null,
    source_offer_id: null,
    vat_regime: 'standard',
    language: 'de',
    service_date: '2026-11-01',
    service_date_to: '2026-11-30',
    due_date: '2026-12-15',
    valid_until: null,
    discount_percent_permyriad: null,
    discount_amount_cents: null,
    payment_terms: 'Net 14',
    notes: null,
    customer_reference: 'PO-7',
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
        unit_price_cents: 8000,
        vat_rate_permyriad: 1900,
        line_discount_percent_permyriad: null,
        line_discount_amount_cents: null,
      },
    ],
    totals,
    ...overrides,
  }
}

const draft = doc({})
const issued = doc({
  state: 'issued',
  number: 'RE-20260009',
  chain_status: 'effective',
  issued_at: '2026-11-02T10:00:00Z',
})
const superseded = doc({ ...issued, chain_status: 'superseded' })
const offer = doc({
  id: 'd-4',
  type: 'offer',
  number: null,
  service_date: null,
  service_date_to: null,
  due_date: null,
  valid_until: '2026-12-31',
})
const correctionDraft = doc({
  id: 'd-5',
  type: 'correction',
  corrects_document_id: 'd-0',
  correction_base_seq: 1,
})

const customer = { id: 'c-1', name: 'Customer Ltd', number: 'K-1', deleted_at: null }
const payments: PaymentsAnswer = {
  payments: [],
  paid_cents: 0,
  effective_document_id: 'd-2',
  open_cents: 23800,
}

let posts: { path: string }[] = []
let deletes: string[] = []

function mockApi(
  main: Doc,
  extra: { postResult?: (path: string) => Promise<unknown>; payments?: PaymentsAnswer } = {},
) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/me' || path === '/status') {
      // The one session retry of the view stays failed.
      return Promise.reject(apiError(503, ErrorCode.unavailable))
    }
    if (path.startsWith('/customers/')) {
      return Promise.resolve(customer)
    }
    if (path.endsWith('/payments')) {
      return Promise.resolve(extra.payments ?? payments)
    }
    return Promise.resolve(main)
  })
  vi.mocked(apiClient.post).mockImplementation((path: string) => {
    posts.push({ path })
    return extra.postResult ? extra.postResult(path) : Promise.resolve({ ...issued })
  })
  vi.mocked(apiClient.del).mockImplementation((path: string) => {
    deletes.push(path)
    return Promise.resolve(undefined)
  })
}

async function renderView(id = 'd-1', locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/documents', name: 'documents', component: { template: '<div />' } },
      { path: '/documents/:id', name: 'document-detail', component: DocumentDetailView },
      { path: '/documents/:id/edit', name: 'document-edit', component: { template: '<div />' } },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  await router.push({ name: 'document-detail', params: { id } })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(DocumentDetailView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

function toastTitles(): string[] {
  return useToasts().toasts.value.map((toast) => toast.title)
}

async function askIssue(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Issue' }))
  return screen.findByRole('dialog')
}

describe('DocumentDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    posts = []
    deletes = []
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  describe('a draft', () => {
    it('shows no number, the lines, the customer and the totals of the API', async () => {
      mockApi(draft)
      await renderView()

      expect(await screen.findByRole('heading', { name: 'Draft invoice' })).toBeVisible()
      expect(screen.getByText(/It has no number yet/)).toBeVisible()
      expect(screen.getByText('Customer Ltd')).toBeVisible()
      const lines = screen.getByRole('table', { name: 'Lines of the document' })
      expect(within(lines).getByText('Consulting')).toBeVisible()
      expect(within(lines).getByText('2.5')).toBeVisible()
      expect(within(lines).getByText('19 %')).toBeVisible()
      const totalsList = screen.getByTestId('totals')
      expect(within(totalsList).getByText('238.00 EUR')).toBeVisible()
      expect(within(totalsList).getByText('38.00 EUR')).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Correct invoice' })).toBeNull()
      expect(screen.queryByText('Payments')).toBeNull()
      expect(apiClient.get).not.toHaveBeenCalledWith('/documents/d-1/payments', expect.anything())
    })

    it('shows the German strings and number format', async () => {
      mockApi(draft)
      await renderView('d-1', 'de')

      expect(await screen.findByRole('heading', { name: 'Rechnungsentwurf' })).toBeVisible()
      expect(await screen.findByRole('button', { name: 'Ausstellen' })).toBeVisible()
      expect(within(screen.getByTestId('totals')).getByText('238,00 EUR')).toBeVisible()
    })

    it('opens the editor from the edit action', async () => {
      mockApi(draft)
      const user = userEvent.setup()
      const { router } = await renderView()

      await user.click(await screen.findByRole('button', { name: 'Edit draft' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-edit'))
    })

    it('asks before it issues, names the immutability and sends nothing until confirmed', async () => {
      mockApi(draft)
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)

      expect(within(dialog).getByText('Issue this invoice?')).toBeVisible()
      expect(within(dialog).getByText(/can never be changed or deleted again/)).toBeVisible()
      expect(within(dialog).getByText('Invoice, Customer Ltd')).toBeVisible()
      await new Promise((resolve) => setTimeout(resolve, 20))
      expect(posts).toHaveLength(0)
    })

    it('does not issue when the confirmation is cancelled', async () => {
      mockApi(draft)
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(posts).toHaveLength(0)
      expect(screen.getByRole('heading', { name: 'Draft invoice' })).toBeVisible()
    })

    it('issues after the confirmation and shows the stored number exactly as it came back', async () => {
      mockApi(draft, {
        postResult: () =>
          Promise.resolve({ ...issued, number: 'X/ß-7', chain_status: 'effective' }),
      })
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      expect(await screen.findByRole('heading', { name: 'X/ß-7' })).toBeVisible()
      expect(posts).toEqual([{ path: '/documents/d-1/issue' }])
      expect(toastTitles()).toContain('Issued as X/ß-7.')
      expect(screen.queryByRole('button', { name: 'Issue' })).toBeNull()
      expect(screen.getByRole('button', { name: 'Correct invoice' })).toBeVisible()
    })

    it('shows the translated message of 503 renderer_unavailable and keeps the draft', async () => {
      mockApi(draft, {
        postResult: () => Promise.reject(apiError(503, 'renderer_unavailable', {})),
      })
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      expect(await screen.findByTestId('issue-error')).toHaveTextContent(
        'Issuing is not available yet.',
      )
      expect(screen.getByTestId('issue-error')).not.toHaveTextContent('failed')
      expect(screen.getByRole('heading', { name: 'Draft invoice' })).toBeVisible()
      expect(screen.getByRole('button', { name: 'Issue' })).toBeVisible()
    })

    it('tells a VAT regime mismatch with the regime that applies', async () => {
      mockApi(draft, {
        postResult: () =>
          Promise.reject(apiError(409, 'vat_regime_mismatch', { vat_regime: 'kleinunternehmer' })),
      })
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      expect(await screen.findByTestId('issue-error')).toHaveTextContent(
        'differs from the regime that applies today (Kleinunternehmer (no VAT))',
      )
    })

    it('lists the field errors of an incomplete draft in the visitor language', async () => {
      mockApi(draft, {
        postResult: () =>
          Promise.reject(
            apiError(400, ErrorCode.invalidArgument, {
              'seller.tax_number': 'a Steuernummer or USt-IdNr is required',
              'lines[0].vat_rate_permyriad':
                'a tax exempt line (rate 0) needs an exemption reason under the standard regime, which is not supported yet',
              'customer.address.city': 'required',
            }),
          ),
      })
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      const alert = await screen.findByTestId('issue-error')
      expect(alert).toHaveTextContent('The document is incomplete.')
      const list = within(alert.parentElement as HTMLElement).getAllByRole('listitem')
      expect(list).toHaveLength(3)
      expect(screen.getByText('seller.tax_number')).toBeVisible()
      expect(
        screen.getByText(/A tax number or VAT ID is required in the seller profile/),
      ).toBeVisible()
      expect(screen.getByText(/needs an exemption reason/)).toBeVisible()
      expect(screen.queryByText(/a Steuernummer or USt-IdNr is required/)).toBeNull()
      expect(screen.getByRole('link', { name: 'Open the seller profile' })).toBeVisible()
    })

    it('tells that a newer correction became effective and starts a new one from it', async () => {
      const calls: string[] = []
      mockApi(correctionDraft, {
        postResult: (path) => {
          calls.push(path)
          return path.endsWith('/issue')
            ? Promise.reject(apiError(409, 'correction_base_changed', { correction_base_seq: '2' }))
            : Promise.resolve({ ...correctionDraft, id: 'd-6' })
        },
      })
      const user = userEvent.setup()
      const { router } = await renderView('d-5')

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      expect(await screen.findByTestId('issue-error')).toHaveTextContent('(correction 2)')
      expect(screen.getByRole('heading', { name: 'Draft correction' })).toBeVisible()

      await user.click(
        screen.getByRole('button', { name: 'Start a new correction from the effective document' }),
      )

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-edit'))
      expect(router.currentRoute.value.params.id).toBe('d-6')
      expect(calls).toEqual(['/documents/d-5/issue', '/documents/d-0/correction'])
      expect(deletes).toEqual([])
    })

    it('tells a refused correction regime change and a conflict', async () => {
      mockApi(draft, {
        postResult: () =>
          Promise.reject(apiError(409, 'correction_regime_changed', { vat_regime: 'standard' })),
      })
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      expect(await screen.findByTestId('issue-error')).toHaveTextContent(
        'A correction across a change of regime is not supported yet.',
      )
    })

    it('sends a 401 on issue to the login', async () => {
      mockApi(draft, {
        postResult: () => Promise.reject(apiError(401, ErrorCode.unauthenticated)),
      })
      const user = userEvent.setup()
      const { router } = await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('discards the draft only after the confirmation', async () => {
      mockApi(draft)
      const user = userEvent.setup()
      const { router } = await renderView()

      await user.click(await screen.findByRole('button', { name: 'Discard draft' }))
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('The draft is deleted. This cannot be undone.')).toBeVisible()
      expect(deletes).toHaveLength(0)
      await user.click(within(dialog).getByRole('button', { name: 'Discard draft' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('documents'))
      expect(deletes).toEqual(['/documents/d-1'])
    })

    it('reloads when the draft was issued in the meantime', async () => {
      mockApi(draft)
      vi.mocked(apiClient.del).mockRejectedValue(apiError(409, ErrorCode.conflict))
      const user = userEvent.setup()
      await renderView()

      await user.click(await screen.findByRole('button', { name: 'Discard draft' }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Discard draft' }),
      )

      await vi.waitFor(() =>
        expect(toastTitles()).toContain(
          'The document was issued in the meantime and cannot be deleted.',
        ),
      )
      await vi.waitFor(() =>
        expect(
          vi.mocked(apiClient.get).mock.calls.filter(([path]) => path === '/documents/d-1'),
        ).toHaveLength(2),
      )
    })
  })

  describe('an issued invoice', () => {
    it('is read only, shows its stored number, status and payments, and a disabled PDF placeholder', async () => {
      mockApi(issued)
      await renderView()

      expect(await screen.findByRole('heading', { name: 'RE-20260009' })).toBeVisible()
      expect(screen.getByText('Effective')).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Issue' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Edit draft' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Discard draft' })).toBeNull()
      expect(await screen.findByRole('heading', { name: 'Payments' })).toBeVisible()
      expect(screen.getByRole('button', { name: 'Download PDF' })).toBeDisabled()
      expect(screen.getByText(/PDF download is not available yet/)).toBeVisible()
    })

    it('starts a correction and opens its editor', async () => {
      mockApi(issued, { postResult: () => Promise.resolve({ ...correctionDraft, id: 'd-7' }) })
      const user = userEvent.setup()
      const { router } = await renderView()

      await user.click(await screen.findByRole('button', { name: 'Correct invoice' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-edit'))
      expect(router.currentRoute.value.params.id).toBe('d-7')
      expect(posts).toEqual([{ path: '/documents/d-1/correction' }])
    })

    it('says that issuing is off when the correction answers 503', async () => {
      mockApi(issued, {
        postResult: () => Promise.reject(apiError(503, 'renderer_unavailable', {})),
      })
      const user = userEvent.setup()
      await renderView()

      await user.click(await screen.findByRole('button', { name: 'Correct invoice' }))

      expect(await screen.findByTestId('action-error')).toHaveTextContent(
        'Issuing is not available yet.',
      )
    })

    it('marks a superseded document, offers no correction and links to the effective one', async () => {
      mockApi(superseded)
      await renderView()

      expect(await screen.findByTestId('superseded-note')).toHaveTextContent(
        'A newer correction replaced this document.',
      )
      expect(screen.getByText('Superseded')).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Correct invoice' })).toBeNull()
      expect(await screen.findByRole('link', { name: 'Open the effective document' })).toBeVisible()
    })

    it('offers a member neither a correction nor payment entry', async () => {
      me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
      mockApi(issued)
      await renderView()

      await screen.findByRole('heading', { name: 'Payments' })
      expect(screen.queryByRole('button', { name: 'Correct invoice' })).toBeNull()
      expect(screen.getByText('Only the owner corrects an invoice.')).toBeVisible()
      expect(screen.queryByRole('button', { name: 'Record payment' })).toBeNull()
    })
  })

  describe('an offer', () => {
    it('shows its validity and no payments, and creates an invoice draft from it', async () => {
      mockApi(offer, { postResult: () => Promise.resolve({ ...draft, id: 'd-8' }) })
      const user = userEvent.setup()
      const { router } = await renderView('d-4')

      expect(await screen.findByText('2026-12-31')).toBeVisible()
      expect(screen.queryByRole('heading', { name: 'Payments' })).toBeNull()
      await user.click(screen.getByRole('button', { name: 'Create invoice from offer' }))

      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-edit'))
      expect(router.currentRoute.value.params.id).toBe('d-8')
      expect(posts).toEqual([{ path: '/offers/d-4/to-invoice' }])
    })
  })

  describe('the session', () => {
    it('keeps every write disabled without a session and says why', async () => {
      me.value = null
      sessionFailed.value = true
      mockApi(draft)
      await renderView()

      expect(await screen.findByRole('heading', { name: 'Draft invoice' })).toBeVisible()
      expect(screen.getByText(/The session could not be loaded/)).toBeVisible()
      expect(screen.getByRole('button', { name: 'Discard draft' })).toBeDisabled()
      expect(posts).toHaveLength(0)
    })

    it('does not post an offer to invoice request without a session', async () => {
      me.value = null
      sessionFailed.value = true
      mockApi(offer)
      await renderView('d-4')

      expect(
        await screen.findByRole('button', { name: 'Create invoice from offer' }),
      ).toBeDisabled()
      expect(posts).toHaveLength(0)
    })

    it('does not issue when the session is gone by the time it is confirmed', async () => {
      mockApi(draft)
      const user = userEvent.setup()
      await renderView()

      const dialog = await askIssue(user)
      me.value = null
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(posts).toHaveLength(0)
      expect(toastTitles()).toContain(
        'The session could not be loaded. Saving and deleting are blocked. Reload the page.',
      )
    })
  })

  describe('loading', () => {
    it('says that the document does not exist on a 404', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(404, ErrorCode.notFound))
      await renderView()

      expect(await screen.findByText('Document not found')).toBeVisible()
    })

    it('shows the error state and retries', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
      const user = userEvent.setup()
      await renderView()

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'The document could not be loaded.',
      )
      mockApi(draft)
      await user.click(screen.getByRole('button', { name: 'Try again' }))

      expect(await screen.findByRole('heading', { name: 'Draft invoice' })).toBeVisible()
    })

    it('clears the session and goes to the login on a 401', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const { router } = await renderView()

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('ignores the answer of a document the visitor has left', async () => {
      const pending: Record<string, (value: unknown) => void> = {}
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path.startsWith('/customers/')) {
          return Promise.resolve(customer)
        }
        return new Promise((resolve) => {
          pending[path] = resolve
        })
      })
      const { router } = await renderView('d-1')
      await vi.waitFor(() => expect(pending['/documents/d-1']).toBeDefined())
      await router.push({ name: 'document-detail', params: { id: 'd-4' } })
      await vi.waitFor(() => expect(pending['/documents/d-4']).toBeDefined())

      pending['/documents/d-4']?.(offer)
      expect(await screen.findByText('2026-12-31')).toBeVisible()
      pending['/documents/d-1']?.(draft)
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.getByText('2026-12-31')).toBeVisible()
      expect(screen.getByRole('heading', { name: 'Draft offer' })).toBeVisible()
    })

    it('ignores a 401 of a document the visitor has left', async () => {
      const pending: Record<
        string,
        { resolve: (v: unknown) => void; reject: (e: unknown) => void }
      > = {}
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path.startsWith('/customers/')) {
          return Promise.resolve(customer)
        }
        return new Promise((resolve, reject) => {
          pending[path] = { resolve, reject }
        })
      })
      const { router } = await renderView('d-1')
      const push = vi.spyOn(router, 'push')
      await vi.waitFor(() => expect(pending['/documents/d-1']).toBeDefined())
      await router.push({ name: 'document-detail', params: { id: 'd-4' } })
      await vi.waitFor(() => expect(pending['/documents/d-4']).toBeDefined())
      push.mockClear()

      pending['/documents/d-4']?.resolve(offer)
      await screen.findByText('2026-12-31')
      pending['/documents/d-1']?.reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(screen.queryByRole('alert')).toBeNull()
    })

    it('aborts its read on unmount and changes nothing when a 401 arrives afterwards', async () => {
      let reject: (error: unknown) => void = () => {}
      vi.mocked(apiClient.get).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const { router, unmount } = await renderView()
      await vi.waitFor(() => expect(apiClient.get).toHaveBeenCalledTimes(1))
      const signal = (vi.mocked(apiClient.get).mock.calls[0]?.[1] as { signal: AbortSignal }).signal
      expect(signal.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      expect(signal.aborted).toBe(true)
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })

    it('goes nowhere when an issue answers 401 after unmount', async () => {
      let reject: (error: unknown) => void = () => {}
      mockApi(draft, {
        postResult: () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      })
      const user = userEvent.setup()
      const { router, unmount } = await renderView()

      const dialog = await askIssue(user)
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))
      await vi.waitFor(() => expect(posts).toHaveLength(1))

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })
  })
})
