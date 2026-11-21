import { ApiError, isApiError } from '@ostendorf-cloud/govue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { csrfToken } from './api'
import {
  MAX_RECEIPT_BYTES,
  checkReceiptFile,
  expenseFieldErrors,
  isInlineImage,
  regimeOn,
  signedCentsToInput,
  uploadReceipt,
  type VatPeriod,
} from './expenses'

const t = (key: string) => `[${key}]`

describe('checkReceiptFile', () => {
  it.each([
    ['receipt.pdf', 'application/pdf'],
    ['photo.jpg', 'image/jpeg'],
    ['scan.png', 'image/png'],
    ['invoice.xml', 'application/xml'],
    ['invoice.xml', 'text/xml'],
    ['unknown-type.PDF', ''],
  ])('accepts %s of type %s', (name, type) => {
    expect(checkReceiptFile({ name, size: 1000, type })).toBeNull()
  })

  it.each([
    ['archive.zip', 'application/zip'],
    ['script.exe', 'application/x-msdownload'],
    ['notes.txt', 'text/plain'],
    ['noextension', ''],
    ['image.gif', 'image/gif'],
    ['fake.pdf', 'text/html'],
  ])('refuses %s of type %s', (name, type) => {
    expect(checkReceiptFile({ name, size: 1000, type })).toBe('unsupportedType')
  })

  it('refuses a file over 10 MiB and accepts exactly 10 MiB', () => {
    const base = { name: 'a.pdf', type: 'application/pdf' }
    expect(checkReceiptFile({ ...base, size: MAX_RECEIPT_BYTES + 1 })).toBe('tooLarge')
    expect(checkReceiptFile({ ...base, size: MAX_RECEIPT_BYTES })).toBeNull()
  })

  it('refuses an empty file', () => {
    expect(checkReceiptFile({ name: 'a.pdf', size: 0, type: 'application/pdf' })).toBe('empty')
  })
})

describe('signedCentsToInput', () => {
  it('keeps the sign that centsToInput would clamp away', () => {
    expect(signedCentsToInput(-5950, 'en')).toBe('-59.50')
    expect(signedCentsToInput(-5950, 'de')).toBe('-59,50')
    expect(signedCentsToInput(-5, 'en')).toBe('-0.05')
  })

  it('writes a positive amount and zero without a sign', () => {
    expect(signedCentsToInput(5950, 'de')).toBe('59,50')
    expect(signedCentsToInput(0, 'en')).toBe('0.00')
  })
})

describe('isInlineImage', () => {
  it('shows JPEG and PNG inline and nothing else', () => {
    expect(isInlineImage('image/jpeg')).toBe(true)
    expect(isInlineImage('image/png')).toBe(true)
    expect(isInlineImage('application/pdf')).toBe(false)
    expect(isInlineImage('image/svg+xml')).toBe(false)
  })
})

describe('regimeOn', () => {
  const periods: VatPeriod[] = [
    { valid_from: '2026-01-01', regime: 'kleinunternehmer', superseded: false },
    { valid_from: '2026-10-14', regime: 'standard', superseded: false },
    { valid_from: '2026-06-01', regime: 'standard', superseded: true },
  ]

  it('takes the latest period at or before the date', () => {
    expect(regimeOn(periods, '2026-03-01')).toBe('kleinunternehmer')
    expect(regimeOn(periods, '2026-10-14')).toBe('standard')
  })

  it('ignores a superseded period', () => {
    expect(regimeOn(periods, '2026-07-01')).toBe('kleinunternehmer')
  })

  it('answers null before the first period', () => {
    expect(regimeOn(periods, '2025-12-31')).toBeNull()
  })
})

describe('expenseFieldErrors', () => {
  it('maps the server reasons to the form fields and texts', () => {
    expect(
      expenseFieldErrors(
        {
          gross_cents: 'must be greater than 0',
          net_cents: 'must equal gross_cents under the Kleinunternehmer regime',
          vat_cents: 'does not match net_cents at vat_rate_permyriad, expected 1900',
          category: 'must be a key of kind expense from GET /api/v1/categories',
          supplier: 'must be at most 200 characters',
          paid_on: 'required for booking',
        },
        t,
      ),
    ).toEqual({
      gross: '[expenses.reasons.kleinunternehmer]',
      vat: '[expenses.reasons.vatMismatch]',
      category: '[expenses.reasons.invalidCategory]',
      supplier: '[expenses.reasons.tooLong]',
      paidOn: '[expenses.reasons.requiredForBooking]',
    })
  })

  it('falls back to the shared texts, never to raw English', () => {
    const out = expenseFieldErrors({ description: 'required', receipt_date: 'something new' }, t)
    expect(out.description).toBe('[common.fieldErrors.required]')
    expect(out.receiptDate).toBe('[common.fieldErrors.invalid]')
  })
})

describe('uploadReceipt', () => {
  beforeEach(() => {
    csrfToken.value = 'csrf-token'
  })
  afterEach(() => {
    csrfToken.value = undefined
    vi.unstubAllGlobals()
    // The AbortSignal.timeout spy of one test must not reach the next.
    vi.restoreAllMocks()
  })

  const pdf = () => new File(['%PDF-1.7'], 'receipt.pdf', { type: 'application/pdf' })

  it('posts multipart with the purpose, the file and the CSRF header', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ file: { id: 'f-1' }, duplicates: [] }), { status: 201 }),
      )
    vi.stubGlobal('fetch', fetchMock)

    const result = await uploadReceipt(pdf())

    expect(result.file.id).toBe('f-1')
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/files')
    expect(init.method).toBe('POST')
    expect(init.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
    // The browser sets the multipart boundary: no Content-Type of our own.
    expect(init.headers).not.toHaveProperty('Content-Type')
    const body = init.body as FormData
    expect(body.get('purpose')).toBe('receipt')
    expect((body.get('file') as File).name).toBe('receipt.pdf')
  })

  it('turns an error answer into an ApiError with status and code', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: 'unsupported_media_type', message: 'x' } }), {
          status: 415,
        }),
      ),
    )

    const error = await uploadReceipt(pdf()).catch((e: unknown) => e)

    expect(isApiError(error)).toBe(true)
    expect(error).toMatchObject({ status: 415, code: 'unsupported_media_type' })
  })

  it('reports a timeout as its own kind', async () => {
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(AbortSignal.abort())
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new DOMException('x', 'TimeoutError')))

    const error = await uploadReceipt(pdf()).catch((e: unknown) => e)

    expect(error).toMatchObject({ code: 'timeout' })
  })

  it('passes the caller signal on and reports its abort as a network failure', async () => {
    const controller = new AbortController()
    const seen: boolean[] = []
    const fetchMock = vi.fn().mockImplementation((_url: string, init: RequestInit) => {
      seen.push(init.signal?.aborted ?? false)
      controller.abort()
      seen.push(init.signal?.aborted ?? false)
      return Promise.reject(new DOMException('x', 'AbortError'))
    })
    vi.stubGlobal('fetch', fetchMock)

    const error = await uploadReceipt(pdf(), controller.signal).catch((e: unknown) => e)

    // Asserted out here: an expect inside the mock would throw into fetch,
    // which uploadReceipt catches as a network failure, and still pass.
    expect(seen).toEqual([false, true])
    expect(error).toMatchObject({ code: 'network' })
  })

  it('turns a transport failure into a transport ApiError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))

    const error = await uploadReceipt(pdf()).catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).isTransport).toBe(true)
  })
})
