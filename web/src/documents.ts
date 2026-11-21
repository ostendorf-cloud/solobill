// The typed client of the documents and payments routes (docs/api.md,
// "Documents" and "Payments") and the small pure helpers the document views
// share. The browser never computes money here: totals come from the server
// (GET a document, POST /documents/preview), amounts typed by the user are
// parsed into integer cents by money.ts, and the only arithmetic left is
// comparing integers and flipping the sign of one.
import { isApiError } from '@ostendorf-cloud/govue'

import { apiClient } from './api'
import { parseCents } from './money'

export type DocumentType = 'invoice' | 'correction' | 'offer'
export type DocumentState = 'draft' | 'issued'
export type ChainStatus = 'effective' | 'superseded'
export type Regime = 'kleinunternehmer' | 'standard'
export type DocumentLanguage = 'de' | 'en'
export type LineUnit = 'piece' | 'hour' | 'day'
export type PaymentMethod = 'bank_transfer' | 'cash' | 'card' | 'paypal' | 'other'

export const LINE_UNITS: readonly LineUnit[] = ['piece', 'hour', 'day']
export const PAYMENT_METHODS: readonly PaymentMethod[] = [
  'bank_transfer',
  'cash',
  'card',
  'paypal',
  'other',
]

export interface VatBucket {
  rate_permyriad: number
  net_cents: number
  vat_cents: number
}

export interface Totals {
  net_total_cents: number
  document_discount_cents: number
  net_after_document_discount_cents: number
  vat: VatBucket[]
  vat_total_cents: number
  grand_total_cents: number
}

export interface DocumentLine {
  position: number
  catalogue_item_id: string | null
  description: string
  quantity_thousandths: number
  unit: string
  unit_price_cents: number
  vat_rate_permyriad: number
  line_discount_percent_permyriad: number | null
  line_discount_amount_cents: number | null
}

/** Doc is the document JSON. `lines` is absent in a list answer. `number` is
 *  the stored display number, null for a draft: the browser never forms one. */
export interface Doc {
  id: string
  type: DocumentType
  state: DocumentState
  customer_id: string
  corrects_document_id: string | null
  source_offer_id: string | null
  vat_regime: Regime
  language: DocumentLanguage
  service_date: string | null
  service_date_to: string | null
  due_date: string | null
  valid_until: string | null
  discount_percent_permyriad: number | null
  discount_amount_cents: number | null
  payment_terms: string | null
  notes: string | null
  customer_reference: string | null
  number: string | null
  correction_seq: number | null
  correction_base_seq: number | null
  original_number: string | null
  imported: boolean
  chain_status: ChainStatus | null
  issued_at: string | null
  lines?: DocumentLine[]
  totals: Totals | null
}

/** DocumentBody is the body of POST /documents, PUT /documents/{id} and
 *  POST /documents/preview. */
export interface DocumentBody {
  type: DocumentType
  customer_id: string | null
  vat_regime: Regime | null
  language: DocumentLanguage
  service_date: string | null
  service_date_to: string | null
  due_date: string | null
  valid_until: string | null
  discount_percent_permyriad: number | null
  discount_amount_cents: number | null
  payment_terms: string | null
  notes: string | null
  customer_reference: string | null
  lines: DocumentLineBody[]
}

export interface DocumentLineBody {
  catalogue_item_id: string | null
  description: string
  quantity_thousandths: number
  unit: string
  unit_price_cents: number
  vat_rate_permyriad: number
  line_discount_percent_permyriad: number | null
  line_discount_amount_cents: number | null
}

export interface PreviewLine {
  net_after_line_discount_cents: number
  document_discount_share_cents: number
  net_after_all_discounts_cents: number
}

export interface PreviewAnswer {
  totals: Totals
  lines: PreviewLine[]
}

export interface Payment {
  id: string
  document_id: string
  received_on: string
  amount_cents: number
  method: PaymentMethod
  note: string | null
  reverses_payment_id: string | null
  imported: boolean
  created_at: string
}

export interface PaymentsAnswer {
  payments: Payment[]
  paid_cents: number
  effective_document_id: string
  /** Negative means a refund is due. */
  open_cents: number
}

export interface PaymentBody {
  received_on: string
  amount_cents: number
  method: PaymentMethod
  note: string | null
  reverses_payment_id: string | null
}

export interface ListFilters {
  type?: string
  state?: string
  year?: string
}

interface Options {
  signal?: AbortSignal
}

export function listDocuments(filters: ListFilters, options: Options = {}): Promise<Doc[]> {
  return apiClient
    .get<{ documents: Doc[] }>('/documents', {
      query: { type: filters.type, state: filters.state, year: filters.year },
      signal: options.signal,
    })
    .then((answer) => answer.documents)
}

export function getDocument(id: string, options: Options = {}): Promise<Doc> {
  return apiClient.get<Doc>(`/documents/${encodeURIComponent(id)}`, { signal: options.signal })
}

export function createDocument(body: DocumentBody): Promise<Doc> {
  return apiClient.post<Doc>('/documents', body)
}

export function updateDocument(id: string, body: DocumentBody): Promise<Doc> {
  return apiClient.put<Doc>(`/documents/${encodeURIComponent(id)}`, body)
}

export function deleteDocument(id: string): Promise<void> {
  return apiClient.del<void>(`/documents/${encodeURIComponent(id)}`)
}

/** previewDocument asks the server for the totals of a draft body. Nothing is
 *  stored. The editor shows these figures and computes none itself. */
export function previewDocument(body: DocumentBody, options: Options = {}): Promise<PreviewAnswer> {
  return apiClient.post<PreviewAnswer>('/documents/preview', body, { signal: options.signal })
}

export function issueDocument(id: string): Promise<Doc> {
  return apiClient.post<Doc>(`/documents/${encodeURIComponent(id)}/issue`)
}

export function createCorrection(id: string): Promise<Doc> {
  return apiClient.post<Doc>(`/documents/${encodeURIComponent(id)}/correction`)
}

export function offerToInvoice(id: string): Promise<Doc> {
  return apiClient.post<Doc>(`/offers/${encodeURIComponent(id)}/to-invoice`)
}

export function listPayments(id: string, options: Options = {}): Promise<PaymentsAnswer> {
  return apiClient.get<PaymentsAnswer>(`/documents/${encodeURIComponent(id)}/payments`, {
    signal: options.signal,
  })
}

export function recordPayment(id: string, body: PaymentBody): Promise<Payment> {
  return apiClient.post<Payment>(`/documents/${encodeURIComponent(id)}/payments`, body)
}

/** berlinToday is today's date in Europe/Berlin as YYYY-MM-DD, the calendar
 *  the server checks a payment's received_on against. */
export function berlinToday(now: Date = new Date()): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Berlin',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(now)
}

/** isIsoDate tells the shape of a date, not that it exists. The server
 *  checks the calendar. */
export function isIsoDate(text: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(text)
}

/** parseQuantity turns "2", "2.5" or "0,125" into thousandths by joining
 *  the digits as text, so no float is ever involved. null means the text is
 *  not a quantity with at most three decimals. */
export function parseQuantity(text: string): number | null {
  const match = /^(\d{1,9})(?:[.,](\d{1,3}))?$/.exec(text.trim())
  if (!match) {
    return null
  }
  return parseInt(`${match[1]}${(match[2] ?? '').padEnd(3, '0')}`, 10)
}

/** formatQuantity writes thousandths with the decimals that matter. */
export function formatQuantity(thousandths: number, locale: string): string {
  const digits = String(Math.abs(Math.trunc(thousandths))).padStart(4, '0')
  const whole = digits.slice(0, -3)
  const fraction = digits.slice(-3).replace(/0+$/, '')
  const decimal = locale.startsWith('de') ? ',' : '.'
  return `${thousandths < 0 ? '-' : ''}${whole}${fraction ? decimal + fraction : ''}`
}

/** formatPermyriad writes a rate or percentage such as 1900 as "19" and 750
 *  as "7,5" (no percent sign). */
export function formatPermyriad(permyriad: number, locale: string): string {
  const digits = String(Math.abs(Math.trunc(permyriad))).padStart(3, '0')
  const whole = digits.slice(0, -2)
  const fraction = digits.slice(-2).replace(/0+$/, '')
  const decimal = locale.startsWith('de') ? ',' : '.'
  return `${permyriad < 0 ? '-' : ''}${whole}${fraction ? decimal + fraction : ''}`
}

/** parsePermyriad reads a percentage such as "19" or "7,5" into permyriad.
 *  null means not a percentage between 0 and 100. */
export function parsePermyriad(text: string): number | null {
  const parsed = parseCents(text)
  return parsed === null || parsed > 10000 ? null : parsed
}

/** negate flips the sign of integer cents. A reversal carries the negated
 *  amount of the payment it undoes: the server demands exactly that. */
export function negate(cents: number): number {
  return cents === 0 ? 0 : -cents
}

/** refundDue is the amount a refund may pay back: the negated open amount
 *  when it is negative, otherwise nothing. */
export function refundDue(openCents: number): number {
  return openCents < 0 ? negate(openCents) : 0
}

/** isReversed tells whether a reversal row names the payment. */
export function isReversed(payment: Payment, payments: readonly Payment[]): boolean {
  return payments.some((p) => p.reverses_payment_id === payment.id)
}

/** canReverse is true for a payment or refund that is no reversal itself and
 *  was not reversed yet. */
export function canReverse(payment: Payment, payments: readonly Payment[]): boolean {
  return payment.reverses_payment_id === null && !isReversed(payment, payments)
}

export type PaymentKind = 'payment' | 'refund' | 'reversal'

export function paymentKind(payment: Payment): PaymentKind {
  if (payment.reverses_payment_id !== null) {
    return 'reversal'
  }
  return payment.amount_cents < 0 ? 'refund' : 'payment'
}

export type IssueFailureKind =
  | 'renderer'
  | 'regimeMismatch'
  | 'regimeMissing'
  | 'baseChanged'
  | 'regimeChanged'
  | 'conflict'
  | 'forbidden'
  | 'fields'
  | 'transport'
  | 'server'
  | 'other'

export interface IssueFailure {
  kind: IssueFailureKind
  details: Record<string, string>
}

/** classifyIssueError sorts the answers of the issue and correction routes by
 *  their code, not their text. 401 is not here: the caller handles it. */
export function classifyIssueError(error: unknown): IssueFailure {
  if (!isApiError(error)) {
    return { kind: 'other', details: {} }
  }
  const details = error.details
  switch (error.code) {
    case 'renderer_unavailable':
      return { kind: 'renderer', details }
    case 'vat_regime_mismatch':
      return { kind: 'regimeMismatch', details }
    case 'vat_regime_missing':
      return { kind: 'regimeMissing', details }
    case 'correction_base_changed':
      return { kind: 'baseChanged', details }
    case 'correction_regime_changed':
      return { kind: 'regimeChanged', details }
    default:
  }
  if (error.status === 400 && Object.keys(details).length > 0) {
    return { kind: 'fields', details }
  }
  if (error.status === 409) {
    return { kind: 'conflict', details }
  }
  if (error.status === 403) {
    return { kind: 'forbidden', details }
  }
  if (error.isTransport) {
    return { kind: 'transport', details }
  }
  if (error.status >= 500) {
    return { kind: 'server', details }
  }
  return { kind: 'other', details }
}
