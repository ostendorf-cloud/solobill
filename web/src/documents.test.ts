import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import type * as ApiModule from './api'
import type * as DocumentsModule from './documents'
import type { Payment } from './documents'

// The client binds the global fetch when it is created, so the stub goes in
// before the modules are imported, once for the file. These tests use the
// real client: they check what goes over the wire, CSRF header included.
const fetchStub = vi.fn<typeof fetch>()
let api: typeof ApiModule
let docs: typeof DocumentsModule

beforeAll(async () => {
  vi.stubGlobal('fetch', fetchStub)
  api = await import('./api')
  docs = await import('./documents')
}, 30000)

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function payment(overrides: Partial<Payment>): Payment {
  return {
    id: 'p-1',
    document_id: 'd-1',
    received_on: '2026-12-03',
    amount_cents: 1000,
    method: 'bank_transfer',
    note: null,
    reverses_payment_id: null,
    imported: false,
    created_at: '2026-12-03T09:00:00Z',
    ...overrides,
  }
}

const body = {
  type: 'invoice' as const,
  customer_id: 'c-1',
  vat_regime: 'kleinunternehmer' as const,
  language: 'de' as const,
  service_date: null,
  service_date_to: null,
  due_date: null,
  valid_until: null,
  discount_percent_permyriad: null,
  discount_amount_cents: null,
  payment_terms: null,
  notes: null,
  customer_reference: null,
  lines: [],
}

describe('documents client', () => {
  beforeEach(() => {
    fetchStub.mockReset()
    fetchStub.mockImplementation(() => Promise.resolve(jsonResponse({})))
    api.csrfToken.value = 'token-1'
  })

  function lastCall(): { url: string; method: string; csrf: string | null; body: unknown } {
    const [url, init] = fetchStub.mock.calls[fetchStub.mock.calls.length - 1] ?? []
    return {
      url: typeof url === 'string' ? url : '',
      method: init?.method ?? '',
      csrf: new Headers(init?.headers).get('X-CSRF-Token'),
      body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
    }
  }

  it.each([
    ['createDocument', () => docs.createDocument(body), 'POST', '/api/v1/documents'],
    ['updateDocument', () => docs.updateDocument('d-1', body), 'PUT', '/api/v1/documents/d-1'],
    ['deleteDocument', () => docs.deleteDocument('d-1'), 'DELETE', '/api/v1/documents/d-1'],
    ['previewDocument', () => docs.previewDocument(body), 'POST', '/api/v1/documents/preview'],
    ['issueDocument', () => docs.issueDocument('d-1'), 'POST', '/api/v1/documents/d-1/issue'],
    [
      'createCorrection',
      () => docs.createCorrection('d-1'),
      'POST',
      '/api/v1/documents/d-1/correction',
    ],
    ['offerToInvoice', () => docs.offerToInvoice('o-1'), 'POST', '/api/v1/offers/o-1/to-invoice'],
    [
      'recordPayment',
      () =>
        docs.recordPayment('d-1', {
          received_on: '2026-12-03',
          amount_cents: 1000,
          method: 'cash',
          note: null,
          reverses_payment_id: null,
        }),
      'POST',
      '/api/v1/documents/d-1/payments',
    ],
  ])('%s sends the CSRF token with its unsafe request', async (_name, call, method, url) => {
    await call()

    const sent = lastCall()
    expect(sent.method).toBe(method)
    expect(sent.url).toBe(url)
    expect(sent.csrf).toBe('token-1')
  })

  it('sends no token with a read', async () => {
    await docs.listPayments('d-1')

    expect(lastCall().method).toBe('GET')
    expect(lastCall().csrf).toBeNull()
  })

  it('sends the body as given and the filters as a query', async () => {
    fetchStub.mockImplementation(() => Promise.resolve(jsonResponse({ documents: [] })))

    await docs.createDocument(body)
    expect(lastCall().body).toEqual(body)

    await docs.listDocuments({ type: 'offer', state: undefined, year: '2026' })
    expect(lastCall().url).toBe('/api/v1/documents?type=offer&year=2026')
  })

  it('ends a preview when its abort signal fires', async () => {
    const controller = new AbortController()
    fetchStub.mockImplementation((_url, init) => {
      return new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new DOMException('x', 'AbortError')))
      })
    })

    const pending = docs.previewDocument(body, { signal: controller.signal })
    controller.abort()

    await expect(pending).rejects.toBeInstanceOf(ApiError)
  })
})

describe('berlinToday', () => {
  it('uses the Berlin calendar, not UTC', () => {
    // 23:30 UTC on 20 November is 00:30 in Berlin on the 21st.
    expect(docs.berlinToday(new Date('2026-11-20T23:30:00Z'))).toBe('2026-11-21')
    expect(docs.berlinToday(new Date('2026-11-20T12:00:00Z'))).toBe('2026-11-20')
    // Summer time: 22:30 UTC is 00:30 the next day.
    expect(docs.berlinToday(new Date('2026-07-01T22:30:00Z'))).toBe('2026-07-02')
  })
})

describe('quantity and rate text', () => {
  it('reads quantities into thousandths without a float', () => {
    expect(docs.parseQuantity('2')).toBe(2000)
    expect(docs.parseQuantity('2,5')).toBe(2500)
    expect(docs.parseQuantity('0.125')).toBe(125)
    expect(docs.parseQuantity('1.1')).toBe(1100)
    expect(docs.parseQuantity('1.0005')).toBeNull()
    expect(docs.parseQuantity('')).toBeNull()
    expect(docs.parseQuantity('-1')).toBeNull()
    expect(docs.parseQuantity('abc')).toBeNull()
  })

  it('writes quantities and rates back with the locale separator', () => {
    expect(docs.formatQuantity(2500, 'de')).toBe('2,5')
    expect(docs.formatQuantity(2500, 'en')).toBe('2.5')
    expect(docs.formatQuantity(1000, 'en')).toBe('1')
    expect(docs.formatQuantity(125, 'en')).toBe('0.125')
    expect(docs.formatPermyriad(1900, 'de')).toBe('19')
    expect(docs.formatPermyriad(750, 'de')).toBe('7,5')
    expect(docs.formatPermyriad(5, 'en')).toBe('0.05')
    expect(docs.parsePermyriad('19')).toBe(1900)
    expect(docs.parsePermyriad('7,5')).toBe(750)
    expect(docs.parsePermyriad('100')).toBe(10000)
    expect(docs.parsePermyriad('100,01')).toBeNull()
  })
})

describe('payment helpers', () => {
  it('derives the refund due from a negative open amount only', () => {
    expect(docs.refundDue(-2500)).toBe(2500)
    expect(docs.refundDue(0)).toBe(0)
    expect(docs.refundDue(1500)).toBe(0)
  })

  it('negates without producing negative zero', () => {
    expect(docs.negate(1000)).toBe(-1000)
    expect(docs.negate(-1000)).toBe(1000)
    expect(Object.is(docs.negate(0), 0)).toBe(true)
  })

  it('lets a payment or refund be reversed once, and never a reversal', () => {
    const paid = payment({ id: 'p-1', amount_cents: 1000 })
    const refund = payment({ id: 'p-2', amount_cents: -300 })
    const reversal = payment({ id: 'p-3', amount_cents: -1000, reverses_payment_id: 'p-1' })
    const all = [paid, refund, reversal]

    expect(docs.canReverse(paid, all)).toBe(false)
    expect(docs.canReverse(refund, all)).toBe(true)
    expect(docs.canReverse(reversal, all)).toBe(false)
    expect(docs.paymentKind(paid)).toBe('payment')
    expect(docs.paymentKind(refund)).toBe('refund')
    expect(docs.paymentKind(reversal)).toBe('reversal')
  })
})

describe('classifyIssueError', () => {
  it('tells a draft that changed under the issue from a plain conflict', () => {
    const changed = new ApiError({
      code: 'conflict',
      message: 'the draft changed or is no draft of this type any more',
      status: 409,
    })

    expect(docs.classifyIssueError(changed).kind).toBe('draftChanged')
  })

  it.each([
    [503, 'renderer_unavailable', {}, 'renderer'],
    [409, 'vat_regime_mismatch', { vat_regime: 'standard' }, 'regimeMismatch'],
    [409, 'correction_base_changed', { correction_base_seq: '2' }, 'baseChanged'],
    [409, 'correction_regime_changed', {}, 'regimeChanged'],
    [400, 'vat_regime_missing', {}, 'regimeMissing'],
    [400, 'invalid_argument', { 'seller.tax_number': 'x' }, 'fields'],
    [409, 'conflict', {}, 'conflict'],
    [403, 'permission_denied', {}, 'forbidden'],
    [500, 'internal', {}, 'server'],
    [400, 'invalid_argument', {}, 'other'],
  ])('sorts %i %s as %s', (status, code, details, kind) => {
    const error = new ApiError({ code, message: 'm', status, details })
    expect(docs.classifyIssueError(error).kind).toBe(kind)
  })

  it('builds the PDF download route with an encoded id', () => {
    expect(docs.documentPdfUrl('d-1')).toBe('/api/v1/documents/d-1/pdf')
    expect(docs.documentPdfUrl('a/b?c')).toBe('/api/v1/documents/a%2Fb%3Fc/pdf')
  })

  it('fetches a PDF and takes the name from Content-Disposition', async () => {
    fetchStub.mockResolvedValueOnce(
      new Response('%PDF', {
        headers: { 'Content-Disposition': 'attachment; filename="RE-1.pdf"' },
      }),
    )
    const file = await docs.fetchDocumentPdf('d-1', 'fallback.pdf')
    expect(file.name).toBe('RE-1.pdf')
    expect(await file.blob.text()).toBe('%PDF')
  })

  it('prefers the UTF-8 filename* over filename', async () => {
    fetchStub.mockResolvedValueOnce(
      new Response('%PDF', {
        headers: {
          'Content-Disposition':
            'attachment; filename="Rechnung-Muller.pdf"; filename*=UTF-8\'\'Rechnung-M%C3%BCller.pdf',
        },
      }),
    )
    expect((await docs.fetchDocumentPdf('d-1', 'fallback.pdf')).name).toBe('Rechnung-Müller.pdf')
  })

  it('falls back to filename when filename* is malformed', async () => {
    fetchStub.mockResolvedValueOnce(
      new Response('%PDF', {
        headers: {
          'Content-Disposition': 'attachment; filename="RE-2.pdf"; filename*=UTF-8\'\'%E0%A4%A',
        },
      }),
    )
    expect((await docs.fetchDocumentPdf('d-1', 'fallback.pdf')).name).toBe('RE-2.pdf')
  })

  it('falls back to the given name without a Content-Disposition', async () => {
    fetchStub.mockResolvedValueOnce(new Response('%PDF'))
    expect((await docs.fetchDocumentPdf('d-1', 'fallback.pdf')).name).toBe('fallback.pdf')
  })

  it('answers a refused PDF as an ApiError with the code of the body', async () => {
    fetchStub.mockResolvedValueOnce(
      jsonResponse({ error: { code: 'renderer_unavailable', message: 'm' } }, 503),
    )
    const error = await docs.fetchDocumentPdf('d-1', 'x.pdf').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect(docs.classifyPdfError(error)).toBe('renderer')
  })

  it.each([
    [409, 'conflict', 'conflict'],
    [503, 'object_store_unavailable', 'objectStore'],
    [500, 'internal', 'other'],
  ])('sorts PDF answer %i %s as %s', (status, code, kind) => {
    expect(docs.classifyPdfError(new ApiError({ code, message: 'm', status }))).toBe(kind)
  })

  it('sorts a PDF transport failure and a foreign error', async () => {
    fetchStub.mockRejectedValueOnce(new TypeError('offline'))
    const error = await docs.fetchDocumentPdf('d-1', 'x.pdf').catch((e: unknown) => e)
    expect(docs.classifyPdfError(error)).toBe('transport')
    expect(docs.classifyPdfError(new Error('x'))).toBe('other')
  })

  it('sorts a transport failure and a foreign error', () => {
    expect(
      docs.classifyIssueError(new ApiError({ code: ErrorCode.network, message: 'm' })).kind,
    ).toBe('transport')
    expect(docs.classifyIssueError(new Error('x')).kind).toBe('other')
  })
})
