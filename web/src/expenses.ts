// Shared pieces of the expense views: the types of the API answers, the
// receipt checks and upload, the VAT regime lookup and the translation of
// the server's field reasons. No money is computed here beyond integer
// subtraction: amounts stay integer cents (see money.ts).
import { ApiError, ErrorCode, isApiError } from '@ostendorf-cloud/govue'

import { csrfToken } from './api'
import { translateDetails } from './fieldErrors'

export type Regime = 'kleinunternehmer' | 'standard'

export interface Expense {
  id: string
  state: 'draft' | 'booked'
  paid_on: string | null
  receipt_date: string | null
  supplier: string
  description: string
  gross_cents: number
  net_cents: number
  vat_rate_permyriad: number
  vat_cents: number
  category: string
  file_id: string | null
  is_asset: boolean
  storno_of_id: string | null
  imported: boolean
  booked_at: string | null
}

export interface Category {
  key: string
  kind: 'expense' | 'income'
  label_de: string
  label_en: string
}

export interface VatPeriod {
  valid_from: string
  regime: Regime
  superseded: boolean
}

export interface StoredFile {
  id: string
  sha256: string
  size_bytes: number
  content_type: string
  original_filename: string
  uploaded_at: string
}

export interface UploadResult {
  file: StoredFile
  duplicates: StoredFile[]
}

/** MAX_RECEIPT_BYTES is the limit of the file part, docs/api.md "Files". */
export const MAX_RECEIPT_BYTES = 10 * 1024 * 1024

const ALLOWED_TYPES = new Set([
  'application/pdf',
  'image/jpeg',
  'image/png',
  'application/xml',
  'text/xml',
])
const ALLOWED_EXTENSIONS = ['pdf', 'jpg', 'jpeg', 'png', 'xml']

export type FileRefusal = 'empty' | 'tooLarge' | 'unsupportedType'

/** checkReceiptFile applies the limits before anything is sent. The server
 *  checks again by magic bytes and is the authority; this only saves the
 *  round trip and gives a clear error at once. */
export function checkReceiptFile(file: {
  name: string
  size: number
  type: string
}): FileRefusal | null {
  if (file.size === 0) {
    return 'empty'
  }
  if (file.size > MAX_RECEIPT_BYTES) {
    return 'tooLarge'
  }
  // A browser reports an empty type for a file it does not know: the
  // extension decides then. A known, other type is refused.
  const extension = file.name.includes('.') ? (file.name.split('.').pop() ?? '').toLowerCase() : ''
  const allowed =
    ALLOWED_TYPES.has(file.type) || (file.type === '' && ALLOWED_EXTENSIONS.includes(extension))
  if (!allowed) {
    return 'unsupportedType'
  }
  return null
}

/** isInlineImage tells whether the browser may show the file in an img. */
export function isInlineImage(contentType: string): boolean {
  return contentType === 'image/jpeg' || contentType === 'image/png'
}

/** receiptContentUrl is the download route of a stored file. */
export function receiptContentUrl(id: string): string {
  return `/api/v1/files/${encodeURIComponent(id)}/content`
}

/** uploadReceipt sends the file as multipart/form-data. govue's client sends
 *  JSON only, so this is the one place besides it that calls fetch. It
 *  attaches the CSRF token and answers every failure as an ApiError, like the
 *  client does. */
export async function uploadReceipt(file: File): Promise<UploadResult> {
  const body = new FormData()
  body.append('purpose', 'receipt')
  body.append('file', file)
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (csrfToken.value) {
    headers['X-CSRF-Token'] = csrfToken.value
  }
  let response: Response
  try {
    response = await fetch('/api/v1/files', {
      method: 'POST',
      headers,
      body,
      credentials: 'same-origin',
      signal: AbortSignal.timeout(60_000),
    })
  } catch {
    throw new ApiError({ code: ErrorCode.network, message: 'The upload did not reach the server.' })
  }
  if (!response.ok) {
    let error: {
      code?: unknown
      message?: unknown
      details?: Record<string, unknown>
      request_id?: unknown
    } = {}
    try {
      error = ((await response.json()) as { error?: typeof error }).error ?? {}
    } catch {
      // A gateway answering HTML: the status is all there is.
    }
    throw new ApiError({
      code: typeof error.code === 'string' ? error.code : ErrorCode.internal,
      message: typeof error.message === 'string' ? error.message : 'The upload failed.',
      status: response.status,
      details: error.details,
      requestId: typeof error.request_id === 'string' ? error.request_id : undefined,
    })
  }
  return (await response.json()) as UploadResult
}

/** regimeOn is the regime of the effective period on date (YYYY-MM-DD): the
 *  latest valid_from at or before it among the periods that are not
 *  superseded. null means no period covers the date. */
export function regimeOn(periods: readonly VatPeriod[], date: string): Regime | null {
  let best: VatPeriod | null = null
  for (const period of periods) {
    if (period.superseded || period.valid_from > date) {
      continue
    }
    if (best === null || period.valid_from > best.valid_from) {
      best = period
    }
  }
  return best?.regime ?? null
}

/** FIELD_OF maps the API's field names to the form's. The net amount has no
 *  field of its own: it is gross minus VAT. */
const FIELD_OF: Record<string, string> = {
  paid_on: 'paidOn',
  receipt_date: 'receiptDate',
  supplier: 'supplier',
  description: 'description',
  gross_cents: 'gross',
  net_cents: 'gross',
  vat_cents: 'vat',
  vat_rate_permyriad: 'rate',
  category: 'category',
  file_id: 'file',
}

// Reasons of internal/httpapi/routes_expenses.go, as exact text or pattern,
// each mapped to a key under expenses.reasons.
const EXACT_REASONS: Record<string, string> = {
  'must be a date such as 2026-11-18': 'invalidDate',
  'must be between 2000-01-01 and one year from today': 'dateRange',
  'must be a file id': 'noReceipt',
  'no such receipt': 'noReceipt',
  'must be greater than 0': 'positive',
  'too large': 'tooLarge',
  'must be between 0 and 10000': 'invalidRate',
  'net_cents plus vat_cents must equal gross_cents': 'vatSum',
  'must equal gross_cents under the Kleinunternehmer regime': 'kleinunternehmer',
  'must be 0 under the Kleinunternehmer regime': 'kleinunternehmer',
  'cannot be computed from net_cents and vat_rate_permyriad': 'vatMismatch',
  'required for booking': 'requiredForBooking',
  'must not be before the paid_on of the original': 'stornoBefore',
}
const PATTERN_REASONS: [RegExp, string][] = [
  [/^must be at most \d+ characters$/, 'tooLong'],
  [/^must be a key of kind \w+ from GET \/api\/v1\/categories$/, 'invalidCategory'],
  [/^does not match net_cents at vat_rate_permyriad, expected \d+$/, 'vatMismatch'],
]

type Translate = (key: string) => string

/** expenseFieldErrors turns the `details` of an answer into texts keyed by
 *  the form's field names. A reason this module does not know falls back to
 *  the shared translation, never to raw English. */
export function expenseFieldErrors(
  details: Record<string, string>,
  t: Translate,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [apiField, reason] of Object.entries(details)) {
    const field = FIELD_OF[apiField] ?? apiField
    const own = EXACT_REASONS[reason] ?? PATTERN_REASONS.find(([re]) => re.test(reason))?.[1]
    out[field] = own
      ? t(`expenses.reasons.${own}`)
      : (translateDetails({ [apiField]: reason }, t)[apiField] ?? '')
  }
  return out
}

/** isRegimeMissing tells the answer "no VAT regime period covers the date". */
export function isRegimeMissing(error: unknown): boolean {
  return isApiError(error) && error.code === 'vat_regime_missing'
}
