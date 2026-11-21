import { ApiError, ErrorCode, createGovueI18n } from '@ostendorf-cloud/govue'
import { fireEvent, render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import type { Doc } from '../documents'
import { de, en } from '../i18n/locales'
import { clearSession, me } from '../session'
import DocumentsView from './DocumentsView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

function doc(overrides: Partial<Doc>): Doc {
  return {
    id: 'd-1',
    type: 'invoice',
    state: 'issued',
    customer_id: 'c-1',
    corrects_document_id: null,
    source_offer_id: null,
    vat_regime: 'standard',
    language: 'de',
    service_date: '2026-11-01',
    service_date_to: null,
    due_date: null,
    valid_until: null,
    discount_percent_permyriad: null,
    discount_amount_cents: null,
    payment_terms: null,
    notes: null,
    customer_reference: null,
    number: 'RE-20260009',
    correction_seq: null,
    correction_base_seq: null,
    original_number: null,
    imported: false,
    chain_status: 'superseded',
    issued_at: '2026-11-02T10:00:00Z',
    totals: {
      net_total_cents: 20000,
      document_discount_cents: 0,
      net_after_document_discount_cents: 20000,
      vat: [],
      vat_total_cents: 3800,
      grand_total_cents: 23800,
    },
    ...overrides,
  }
}

const original = doc({})
const correction = doc({
  id: 'd-2',
  type: 'correction',
  number: 'RE-20260009-1',
  chain_status: 'effective',
  issued_at: '2026-11-20T23:30:00Z',
  totals: {
    net_total_cents: 10000,
    document_discount_cents: 0,
    net_after_document_discount_cents: 10000,
    vat: [],
    vat_total_cents: 1900,
    grand_total_cents: 11900,
  },
})
const draft = doc({
  id: 'd-3',
  state: 'draft',
  number: null,
  chain_status: null,
  issued_at: null,
  totals: null,
})
const offer = doc({ id: 'd-4', type: 'offer', number: 'AN-20260001', chain_status: null })

const customers = { customers: [{ id: 'c-1', name: 'Customer Ltd' }] }

function mockApi(list: Doc[] = [original, correction, draft, offer]) {
  vi.mocked(apiClient.get).mockImplementation((path: string) =>
    Promise.resolve(path === '/customers' ? customers : { documents: list }),
  )
}

function renderView(locale: 'en' | 'de' = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/documents', name: 'documents', component: DocumentsView },
      { path: '/documents/edit/:id?', name: 'document-edit', component: { template: '<div />' } },
      { path: '/documents/:id', name: 'document-detail', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(DocumentsView, { global: { plugins: [router, i18n, govueI18n] } })
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

/** listCalls are the options of every GET /documents, read after the fact. */
function listQueries(): Record<string, unknown>[] {
  return vi
    .mocked(apiClient.get)
    .mock.calls.filter(([path]) => path === '/documents')
    .map(([, options]) => (options as { query: Record<string, unknown> }).query)
}

describe('DocumentsView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('lists the documents with the stored number, the customer and the chain status', async () => {
    mockApi()
    renderView()

    const table = await screen.findByRole('table', { name: 'List of documents' })
    const rows = within(table).getAllByRole('row')
    expect(within(rows[1]).getByRole('rowheader')).toHaveTextContent('RE-20260009')
    expect(within(rows[1]).getByText('Superseded')).toBeVisible()
    expect(within(rows[1]).getByText('238.00 EUR')).toBeVisible()
    expect(within(rows[2]).getByRole('rowheader')).toHaveTextContent('RE-20260009-1')
    expect(within(rows[2]).getByText('Effective')).toBeVisible()
    expect(within(rows[2]).getByText('Correction')).toBeVisible()
    expect(within(rows[1]).getAllByText('Customer Ltd')).toHaveLength(1)
  })

  it('marks the superseded row and shows a draft without a number', async () => {
    mockApi()
    renderView()

    const table = await screen.findByRole('table', { name: 'List of documents' })
    const rows = within(table).getAllByRole('row')
    expect(rows[1]).toHaveAttribute('data-chain', 'superseded')
    expect(rows[2]).toHaveAttribute('data-chain', 'effective')
    const draftRow = rows[3]
    expect(within(draftRow).getByRole('rowheader')).toHaveTextContent('Draft')
    expect(within(draftRow).getByRole('rowheader')).not.toHaveTextContent(/RE-|AN-/)
    expect(within(draftRow).queryByText('Superseded')).toBeNull()
    expect(within(draftRow).queryByText('Effective')).toBeNull()
  })

  it('shows the issue day of the Berlin calendar', async () => {
    mockApi([correction])
    renderView()

    // 23:30 UTC on 20 November is already the 21st in Berlin.
    expect(await screen.findByText('2026-11-21')).toBeVisible()
  })

  it('shows the German strings and number format', async () => {
    mockApi()
    renderView('de')

    expect(await screen.findByRole('heading', { name: 'Belege' })).toBeVisible()
    expect(await screen.findByRole('button', { name: 'Neue Rechnung' })).toBeVisible()
    expect((await screen.findAllByText('238,00 EUR')).length).toBeGreaterThan(0)
    expect(screen.getByText('Ersetzt')).toBeVisible()
  })

  it('reloads with the chosen type and state', async () => {
    mockApi()
    renderView()
    await screen.findByRole('table', { name: 'List of documents' })

    await choose('Type', 'Offer')
    await vi.waitFor(() => expect(listQueries().at(-1)).toMatchObject({ type: 'offer' }))
    await choose('State', 'Draft')
    await vi.waitFor(() =>
      expect(listQueries().at(-1)).toMatchObject({ type: 'offer', state: 'draft' }),
    )
  })

  it('keeps a dash when the customer names cannot be read', async () => {
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      path.startsWith('/customers')
        ? Promise.reject(apiError(500, ErrorCode.internal))
        : Promise.resolve({ documents: [original] }),
    )
    renderView()

    const table = await screen.findByRole('table', { name: 'List of documents' })
    expect(within(table).getByRole('rowheader')).toHaveTextContent('RE-20260009')
    expect(within(table).getAllByText('–').length).toBeGreaterThan(0)
  })

  it('renders a customer name as text, never as markup', async () => {
    const evil = '<img src=x onerror=1>'
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      Promise.resolve(
        path === '/customers'
          ? { customers: [{ id: 'c-1', name: evil }] }
          : { documents: [original] },
      ),
    )
    renderView()

    const table = await screen.findByRole('table', { name: 'List of documents' })
    expect(within(table).getByText(evil)).toBeVisible()
    expect(table.querySelector('img')).toBeNull()
  })

  describe('customers the list of active customers does not hold', () => {
    const gone = (n: number) =>
      doc({ id: `d-gone-${n}`, customer_id: `c-gone-${n}`, number: `RE-${n}` })

    /** singleReads are the ids read through GET /customers/{id}. */
    function singleReads(): string[] {
      return vi
        .mocked(apiClient.get)
        .mock.calls.map(([path]) => path)
        .filter((path) => path.startsWith('/customers/'))
    }

    it('reads a deleted customer by id and shows the name', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve(customers)
        }
        if (path === '/customers/c-gone-1') {
          return Promise.resolve({ id: 'c-gone-1', name: 'Gone GmbH' })
        }
        return Promise.resolve({ documents: [original, gone(1)] })
      })
      renderView()

      expect(await screen.findByText('Gone GmbH')).toBeVisible()
      expect(screen.getByText('Customer Ltd')).toBeVisible()
      // The active customer is not read a second time.
      expect(singleReads()).toEqual(['/customers/c-gone-1'])
    })

    it('reads each missing customer once, however many documents name it', async () => {
      const same = (n: number) => doc({ id: `d-${n}`, customer_id: 'c-gone-1', number: `RE-${n}` })
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return Promise.resolve({ id: 'c-gone-1', name: 'Gone GmbH' })
        }
        return Promise.resolve({ documents: [same(1), same(2), same(3)] })
      })
      renderView()

      await screen.findAllByText('Gone GmbH')
      expect(singleReads()).toEqual(['/customers/c-gone-1'])
    })

    it('reads at most 20 customers', async () => {
      const many = Array.from({ length: 25 }, (_, i) => gone(i))
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return Promise.resolve({ id: path, name: 'Gone' })
        }
        return Promise.resolve({ documents: many })
      })
      renderView()

      await vi.waitFor(() => expect(singleReads()).toHaveLength(20))
      await new Promise((resolve) => setTimeout(resolve, 30))
      expect(singleReads()).toHaveLength(20)
      expect(new Set(singleReads()).size).toBe(20)
    })

    it('keeps a dash when one of the reads fails and shows the others', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path === '/customers/c-gone-1') {
          return Promise.reject(apiError(404, ErrorCode.notFound))
        }
        if (path === '/customers/c-gone-2') {
          return Promise.resolve({ id: 'c-gone-2', name: 'Two GmbH' })
        }
        return Promise.resolve({ documents: [gone(1), gone(2)] })
      })
      renderView()

      expect(await screen.findByText('Two GmbH')).toBeVisible()
      const rows = screen.getAllByRole('row')
      expect(within(rows[1]).getByText('–')).toBeVisible()
    })

    it('does not read a failed id again when the list reloads', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return Promise.reject(apiError(404, ErrorCode.notFound))
        }
        return Promise.resolve({ documents: [gone(1)] })
      })
      renderView()
      await vi.waitFor(() => expect(singleReads()).toHaveLength(1))

      await choose('State', 'Issued')
      await vi.waitFor(() => expect(listQueries().at(-1)).toMatchObject({ state: 'issued' }))
      await new Promise((resolve) => setTimeout(resolve, 40))

      expect(singleReads()).toEqual(['/customers/c-gone-1'])
    })

    it('reads no single customer when the list of active customers itself failed', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.reject(apiError(500, ErrorCode.internal))
        }
        if (path.startsWith('/customers/')) {
          return Promise.resolve({ id: path, name: 'Should not be read' })
        }
        return Promise.resolve({ documents: [gone(1), gone(2)] })
      })
      renderView()

      const table = await screen.findByRole('table', { name: 'List of documents' })
      await new Promise((resolve) => setTimeout(resolve, 40))
      expect(singleReads()).toHaveLength(0)
      expect(within(table).getAllByText('–')).toHaveLength(2)
    })

    it('cancels the single reads when a newer list load starts', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return new Promise(() => {})
        }
        return Promise.resolve({ documents: [gone(1)] })
      })
      renderView()
      await vi.waitFor(() => expect(singleReads()).toHaveLength(1))
      const call = vi.mocked(apiClient.get).mock.calls.find(([p]) => p === '/customers/c-gone-1')
      const signal = (call?.[1] as { signal: AbortSignal }).signal
      expect(signal.aborted).toBe(false)

      await choose('State', 'Issued')

      await vi.waitFor(() => expect(signal.aborted).toBe(true))
    })

    it('sends a 401 of a single read to the login', async () => {
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return Promise.reject(apiError(401, ErrorCode.unauthenticated))
        }
        return Promise.resolve({ documents: [gone(1)] })
      })
      const { router } = renderView()

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('aborts the reads on unmount and changes nothing when an answer arrives afterwards', async () => {
      let answer: (v: unknown) => void = () => {}
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve({ customers: [] })
        }
        if (path.startsWith('/customers/')) {
          return new Promise((resolve) => {
            answer = resolve
          })
        }
        return Promise.resolve({ documents: [gone(1)] })
      })
      const { router, unmount } = renderView()
      await vi.waitFor(() => expect(singleReads()).toHaveLength(1))
      const call = vi.mocked(apiClient.get).mock.calls.find(([p]) => p === '/customers/c-gone-1')
      const signal = (call?.[1] as { signal: AbortSignal }).signal
      expect(signal.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      unmount()
      expect(signal.aborted).toBe(true)
      answer({ id: 'c-gone-1', name: 'Late GmbH' })
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.queryByText('Late GmbH')).toBeNull()
      expect(push).not.toHaveBeenCalled()
    })
  })

  it('shows the empty state', async () => {
    mockApi([])
    renderView()

    expect(await screen.findByText('No documents')).toBeVisible()
  })

  it('shows the error state and retries', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
    renderView()

    expect(await screen.findByRole('alert')).toHaveTextContent('The documents could not be loaded.')
    mockApi()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('table', { name: 'List of documents' })).toBeVisible()
  })

  it('clears the session and goes to the login on a 401', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
    const { router } = renderView()

    await vi.waitFor(() => expect(me.value).toBeNull())
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })

  it('opens a document and starts a new invoice and a new offer', async () => {
    mockApi()
    const user = userEvent.setup()
    const { router } = renderView()

    await user.click(
      await screen.findByRole('button', { name: 'Open RE-20260009-1 for Customer Ltd' }),
    )
    await vi.waitFor(() => expect(router.currentRoute.value.params.id).toBe('d-2'))

    await router.push({ name: 'documents' })
    await user.click(await screen.findByRole('button', { name: 'New offer' }))
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('document-edit'))
    expect(router.currentRoute.value.query.type).toBe('offer')
  })

  describe('late answers', () => {
    /** deferredApi answers every /documents call with a promise the test
     *  settles itself, in the order the calls were made. */
    function deferredApi() {
      const calls: { resolve: (value: unknown) => void; reject: (error: unknown) => void }[] = []
      vi.mocked(apiClient.get).mockImplementation((path: string) => {
        if (path === '/customers') {
          return Promise.resolve(customers)
        }
        return new Promise((resolve, reject) => {
          calls.push({ resolve, reject })
        })
      })
      return calls
    }

    it.each([
      ['a failing', apiError(500, ErrorCode.internal)],
      ['an unauthenticated', apiError(401, ErrorCode.unauthenticated)],
    ])('ignores %s answer of an older request that arrives late', async (_name, lateError) => {
      const calls = deferredApi()
      const { router } = renderView()
      const push = vi.spyOn(router, 'push')
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      calls[0]?.resolve({ documents: [original] })
      await screen.findByRole('table', { name: 'List of documents' })

      await choose('State', 'Draft')
      await vi.waitFor(() => expect(calls).toHaveLength(2))
      await choose('State', 'Issued')
      await vi.waitFor(() => expect(calls).toHaveLength(3))
      calls[2]?.resolve({ documents: [offer] })
      expect(await screen.findByText('AN-20260001')).toBeVisible()

      calls[1]?.reject(lateError)
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.queryByRole('alert')).toBeNull()
      expect(screen.getByText('AN-20260001')).toBeVisible()
      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
    })

    it('ignores an older list that resolves after a newer one', async () => {
      const calls = deferredApi()
      renderView()
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      calls[0]?.resolve({ documents: [original] })
      await screen.findByRole('table', { name: 'List of documents' })

      await choose('State', 'Draft')
      await vi.waitFor(() => expect(calls).toHaveLength(2))
      await choose('State', 'Issued')
      await vi.waitFor(() => expect(calls).toHaveLength(3))
      calls[2]?.resolve({ documents: [offer] })
      await screen.findByText('AN-20260001')
      calls[1]?.resolve({ documents: [draft] })
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.getByText('AN-20260001')).toBeVisible()
      expect(screen.queryByText('Draft offer')).toBeNull()
      expect(screen.getAllByRole('row')).toHaveLength(2)
    })

    it('aborts its reads on unmount and changes nothing when a 401 arrives afterwards', async () => {
      const calls = deferredApi()
      const { router, unmount } = renderView()
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      const signals = vi
        .mocked(apiClient.get)
        .mock.calls.map(([, options]) => (options as { signal: AbortSignal }).signal)
      expect(signals.length).toBeGreaterThan(0)
      expect(signals.every((s) => s instanceof AbortSignal && !s.aborted)).toBe(true)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      expect(signals.every((s) => s.aborted)).toBe(true)
      calls[0]?.reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })
  })
})
