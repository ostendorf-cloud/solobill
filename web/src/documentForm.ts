// The editor's form model, its conversion to the API body and back, and the
// translation of the server's field errors onto the form's fields. Typed
// amounts go through money.ts and documents.ts into integer cents and
// thousandths; nothing here adds, multiplies or rounds money.
import { centsToInput, parseCents } from './money'
import {
  LINE_UNITS,
  formatPermyriad,
  formatQuantity,
  isIsoDate,
  parsePermyriad,
  parseQuantity,
  type Doc,
  type DocumentBody,
  type DocumentLanguage,
  type DocumentLineBody,
  type DocumentType,
  type Regime,
} from './documents'

type Translate = (key: string) => string

export type DiscountMode = 'none' | 'percent' | 'amount'

export interface LineForm {
  /** A stable key for v-for. Never sent. */
  key: number
  catalogueItemId: string | null
  description: string
  quantity: string
  unit: string
  unitPrice: string
  vatRate: string
  discountMode: DiscountMode
  discountValue: string
}

export interface DocumentForm {
  type: DocumentType
  customerId: string
  regime: Regime | ''
  language: DocumentLanguage
  serviceDate: string
  serviceDateTo: string
  dueDate: string
  validUntil: string
  discountMode: DiscountMode
  discountValue: string
  paymentTerms: string
  notes: string
  customerReference: string
  lines: LineForm[]
}

let lineKey = 0

/** emptyLine is an ad hoc line. Under Kleinunternehmer the rate is 0 by law. */
export function emptyLine(regime: Regime | ''): LineForm {
  return {
    key: ++lineKey,
    catalogueItemId: null,
    description: '',
    quantity: '1',
    unit: 'piece',
    unitPrice: '',
    vatRate: regime === 'kleinunternehmer' ? '0' : '',
    discountMode: 'none',
    discountValue: '',
  }
}

export function emptyForm(type: DocumentType, regime: Regime | ''): DocumentForm {
  return {
    type,
    customerId: '',
    regime,
    language: 'de',
    serviceDate: '',
    serviceDateTo: '',
    dueDate: '',
    validUntil: '',
    discountMode: 'none',
    discountValue: '',
    paymentTerms: '',
    notes: '',
    customerReference: '',
    lines: [emptyLine(regime)],
  }
}

function discountModeOf(percent: number | null, amount: number | null): DiscountMode {
  if (percent !== null) {
    return 'percent'
  }
  return amount !== null ? 'amount' : 'none'
}

/** docToForm fills the form from a stored draft. */
export function docToForm(doc: Doc, locale: string): DocumentForm {
  const mode = discountModeOf(doc.discount_percent_permyriad, doc.discount_amount_cents)
  return {
    type: doc.type,
    customerId: doc.customer_id,
    regime: doc.vat_regime,
    language: doc.language,
    serviceDate: doc.service_date ?? '',
    serviceDateTo: doc.service_date_to ?? '',
    dueDate: doc.due_date ?? '',
    validUntil: doc.valid_until ?? '',
    discountMode: mode,
    discountValue:
      mode === 'percent'
        ? formatPermyriad(doc.discount_percent_permyriad ?? 0, locale)
        : mode === 'amount'
          ? centsToInput(doc.discount_amount_cents ?? 0, locale)
          : '',
    paymentTerms: doc.payment_terms ?? '',
    notes: doc.notes ?? '',
    customerReference: doc.customer_reference ?? '',
    lines: (doc.lines ?? []).map((l) => {
      const lineMode = discountModeOf(
        l.line_discount_percent_permyriad,
        l.line_discount_amount_cents,
      )
      return {
        key: ++lineKey,
        catalogueItemId: l.catalogue_item_id,
        description: l.description,
        quantity: formatQuantity(l.quantity_thousandths, locale),
        unit: l.unit,
        unitPrice: centsToInput(l.unit_price_cents, locale),
        vatRate: formatPermyriad(l.vat_rate_permyriad, locale),
        discountMode: lineMode,
        discountValue:
          lineMode === 'percent'
            ? formatPermyriad(l.line_discount_percent_permyriad ?? 0, locale)
            : lineMode === 'amount'
              ? centsToInput(l.line_discount_amount_cents ?? 0, locale)
              : '',
      }
    }),
  }
}

export interface BodyResult {
  /** null when a typed value cannot be read: the errors say which. */
  body: DocumentBody | null
  /** Translated texts keyed by the form's field names. */
  errors: Record<string, string>
}

function orNull(text: string): string | null {
  return text.trim() === '' ? null : text
}

/** formToBody reads every typed value into the API body. requireCustomer is
 *  true for a save and false for a preview, which needs no customer. */
export function formToBody(form: DocumentForm, t: Translate, requireCustomer: boolean): BodyResult {
  const errors: Record<string, string> = {}
  const reason = (key: string): string => t(`documents.reasons.${key}`)

  if (requireCustomer && form.customerId === '') {
    errors.customer = reason('required')
  }

  const isOffer = form.type === 'offer'
  const dates: Record<string, string> = {
    dueDate: form.dueDate,
    ...(isOffer ? { validUntil: form.validUntil } : {}),
    ...(isOffer ? {} : { serviceDate: form.serviceDate, serviceDateTo: form.serviceDateTo }),
  }
  for (const [field, value] of Object.entries(dates)) {
    if (value.trim() !== '' && !isIsoDate(value.trim())) {
      errors[field] = reason('invalidDate')
    }
  }

  let discountPercent: number | null = null
  let discountAmount: number | null = null
  if (form.discountMode === 'percent') {
    discountPercent = parsePermyriad(form.discountValue)
    if (discountPercent === null) {
      errors.discount = reason('badPercent')
    }
  } else if (form.discountMode === 'amount') {
    discountAmount = parseCents(form.discountValue)
    if (discountAmount === null) {
      errors.discount = reason('badAmount')
    }
  }

  const lines: DocumentLineBody[] = []
  form.lines.forEach((line, i) => {
    const at = (field: string): string => `line.${i}.${field}`
    if (line.description.trim() === '') {
      errors[at('description')] = reason('required')
    }
    const quantity = parseQuantity(line.quantity)
    if (quantity === null || quantity === 0) {
      errors[at('quantity')] = reason('badQuantity')
    }
    if (!LINE_UNITS.some((u) => u === line.unit)) {
      errors[at('unit')] = reason('invalidUnit')
    }
    const price = parseCents(line.unitPrice)
    if (price === null) {
      errors[at('unitPrice')] = reason('badAmount')
    }
    let rate: number | null = 0
    if (form.regime !== 'kleinunternehmer') {
      rate = parsePermyriad(line.vatRate)
      if (rate === null) {
        errors[at('vatRate')] = reason('badRate')
      }
    }
    let percent: number | null = null
    let amount: number | null = null
    if (line.discountMode === 'percent') {
      percent = parsePermyriad(line.discountValue)
      if (percent === null) {
        errors[at('discount')] = reason('badPercent')
      }
    } else if (line.discountMode === 'amount') {
      amount = parseCents(line.discountValue)
      if (amount === null) {
        errors[at('discount')] = reason('badAmount')
      }
    }
    lines.push({
      catalogue_item_id: line.catalogueItemId,
      description: line.description,
      quantity_thousandths: quantity ?? 0,
      unit: line.unit,
      unit_price_cents: price ?? 0,
      vat_rate_permyriad: rate ?? 0,
      line_discount_percent_permyriad: percent,
      line_discount_amount_cents: amount,
    })
  })
  if (form.lines.length === 0) {
    errors.lines = reason('noLines')
  }

  if (Object.keys(errors).length > 0) {
    return { body: null, errors }
  }
  return {
    errors,
    body: {
      type: form.type,
      customer_id: orNull(form.customerId),
      vat_regime: form.regime === '' ? null : form.regime,
      language: form.language,
      service_date: isOffer ? null : orNull(form.serviceDate),
      service_date_to: isOffer ? null : orNull(form.serviceDateTo),
      due_date: orNull(form.dueDate),
      valid_until: isOffer ? orNull(form.validUntil) : null,
      discount_percent_permyriad: discountPercent,
      discount_amount_cents: discountAmount,
      payment_terms: orNull(form.paymentTerms),
      notes: orNull(form.notes),
      customer_reference: orNull(form.customerReference),
      lines,
    },
  }
}

/** FIELD_OF maps the API's header field paths to the form's field names. */
const FIELD_OF: Record<string, string> = {
  customer_id: 'customer',
  vat_regime: 'regime',
  language: 'language',
  service_date: 'serviceDate',
  service_date_to: 'serviceDateTo',
  service_period: 'serviceDate',
  due_date: 'dueDate',
  valid_until: 'validUntil',
  payment_terms: 'paymentTerms',
  notes: 'notes',
  customer_reference: 'customerReference',
  discount_percent_permyriad: 'discount',
  discount_amount_cents: 'discount',
  lines: 'lines',
}

const LINE_FIELD_OF: Record<string, string> = {
  description: 'description',
  quantity_thousandths: 'quantity',
  unit: 'unit',
  unit_price_cents: 'unitPrice',
  vat_rate_permyriad: 'vatRate',
  line_discount_percent_permyriad: 'discount',
  line_discount_amount_cents: 'discount',
  catalogue_item_id: 'catalogue',
}

/** fieldOfPath maps an API path such as `lines[1].vat_rate_permyriad` to the
 *  form's field name (`line.1.vatRate`). null means no field of the editor
 *  carries it, for example `seller.tax_number`. */
export function fieldOfPath(path: string): string | null {
  const line = /^lines\[(\d+)\]\.(\w+)$/.exec(path)
  if (line) {
    const field = LINE_FIELD_OF[line[2] ?? '']
    return field ? `line.${line[1]}.${field}` : null
  }
  if (path.startsWith('customer.')) {
    return 'customer'
  }
  return FIELD_OF[path] ?? null
}

const EXACT_REASONS: Record<string, string> = {
  required: 'required',
  'an offer has no service period': 'offerNoPeriod',
  'required with service_date_to': 'periodFromRequired',
  'must not be before service_date': 'periodOrder',
  'only an offer has a validity date': 'validUntilOffer',
  'a percentage and an amount are mutually exclusive': 'discountExclusive',
  'must be 0 to 10000': 'rateRange',
  'must not be negative': 'negative',
  'must be positive': 'positive',
  'must be piece, hour or day': 'invalidUnit',
  'must be 0 under the Kleinunternehmer regime': 'kleinunternehmerRate',
  'a tax exempt line (rate 0) needs an exemption reason under the standard regime, which is not supported yet':
    'exemptLine',
  'no such customer': 'noCustomer',
  'the customer is deleted': 'customerDeleted',
  'no such catalogue item': 'noCatalogueItem',
  'must be de or en': 'invalidLanguage',
  'must be standard or kleinunternehmer': 'invalidRegime',
  'wrong type': 'wrongType',
  'a Steuernummer or USt-IdNr is required': 'taxNumberRequired',
  'required for an invoice': 'servicePeriodRequired',
  'at least one line is required': 'noLines',
}

const PATTERN_REASONS: [RegExp, string][] = [
  [/^must be a date/, 'invalidDate'],
  [/^at most \d+ lines$/, 'tooManyLines'],
  [/^required: no VAT regime period/, 'regimeUnknown'],
  [/^the lines cannot be calculated/, 'cannotCalculate'],
]

export function reasonKey(reason: string): string {
  return EXACT_REASONS[reason] ?? PATTERN_REASONS.find(([re]) => re.test(reason))?.[1] ?? 'invalid'
}

export interface GeneralProblem {
  path: string
  text: string
}

export interface MappedErrors {
  /** Texts keyed by the form's field names. */
  fields: Record<string, string>
  /** Problems without a field in the editor, such as the seller profile. */
  general: GeneralProblem[]
}

/** documentFieldErrors puts each reason of an answer's `details` at its field
 *  in the visitor's language. An unknown reason gets the generic text, never
 *  the server's English. */
export function documentFieldErrors(details: Record<string, string>, t: Translate): MappedErrors {
  const fields: Record<string, string> = {}
  const general: GeneralProblem[] = []
  for (const [path, reason] of Object.entries(details)) {
    const text = t(`documents.reasons.${reasonKey(reason)}`)
    const field = fieldOfPath(path)
    if (field === null) {
      general.push({ path, text })
    } else if (!(field in fields)) {
      fields[field] = text
    }
  }
  return { fields, general }
}
