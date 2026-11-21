import { describe, expect, it } from 'vitest'

import {
  docToForm,
  documentFieldErrors,
  emptyForm,
  emptyLine,
  fieldOfPath,
  formToBody,
  pathLabel,
  reasonKey,
  reasonText,
} from './documentForm'
import type { Doc } from './documents'
import { de, en } from './i18n/locales'

const t = (key: string) => `[${key}]`

function filled(type: 'invoice' | 'offer' = 'invoice') {
  const form = emptyForm(type, 'standard')
  form.customerId = 'c-1'
  form.serviceDate = '2026-11-01'
  form.serviceDateTo = '2026-11-30'
  form.dueDate = '2026-12-15'
  const line = form.lines[0]
  if (line) {
    line.description = 'Consulting'
    line.quantity = '2,5'
    line.unit = 'hour'
    line.unitPrice = '95'
    line.vatRate = '19'
  }
  return form
}

describe('formToBody', () => {
  it('reads typed text into cents, thousandths and permyriad', () => {
    const form = filled()
    const line = form.lines[0]
    if (line) {
      line.discountMode = 'percent'
      line.discountValue = '10'
    }
    form.discountMode = 'amount'
    form.discountValue = '5,50'

    const { body, errors } = formToBody(form, t, true)

    expect(errors).toEqual({})
    expect(body?.lines[0]).toMatchObject({
      quantity_thousandths: 2500,
      unit_price_cents: 9500,
      vat_rate_permyriad: 1900,
      line_discount_percent_permyriad: 1000,
      line_discount_amount_cents: null,
    })
    expect(body?.discount_amount_cents).toBe(550)
    expect(body?.discount_percent_permyriad).toBeNull()
    expect(body?.vat_regime).toBe('standard')
  })

  it('forces the rate to 0 under the Kleinunternehmer regime whatever was typed', () => {
    const form = filled()
    form.regime = 'kleinunternehmer'
    const line = form.lines[0]
    if (line) {
      line.vatRate = '19'
    }

    expect(formToBody(form, t, true).body?.lines[0]?.vat_rate_permyriad).toBe(0)
  })

  it('refuses unreadable values and names the field', () => {
    const form = filled()
    form.lines.push(emptyLine('standard'))
    const first = form.lines[0]
    if (first) {
      first.quantity = '1.2345'
      first.unitPrice = 'abc'
      first.vatRate = '120'
    }
    form.dueDate = '15.12.2026'
    form.discountMode = 'percent'
    form.discountValue = 'x'

    const { body, errors } = formToBody(form, t, true)

    expect(body).toBeNull()
    expect(Object.keys(errors).sort()).toEqual([
      'discount',
      'dueDate',
      'line.0.quantity',
      'line.0.unitPrice',
      'line.0.vatRate',
      'line.1.description',
      'line.1.unitPrice',
      'line.1.vatRate',
    ])
  })

  it('needs a customer for a save but not for a preview', () => {
    const form = filled()
    form.customerId = ''

    expect(formToBody(form, t, true).errors.customer).toBe('[documents.reasons.required]')
    expect(formToBody(form, t, false).body?.customer_id).toBeNull()
  })

  it('sends no service period for an offer and no validity for an invoice', () => {
    const offer = filled('offer')
    offer.validUntil = '2026-12-31'
    expect(formToBody(offer, t, true).body).toMatchObject({
      service_date: null,
      service_date_to: null,
      valid_until: '2026-12-31',
    })

    const invoice = filled()
    invoice.validUntil = '2026-12-31'
    expect(formToBody(invoice, t, true).body).toMatchObject({
      service_date: '2026-11-01',
      valid_until: null,
    })
  })

  it('refuses a zero quantity and a form without lines', () => {
    const form = filled()
    const line = form.lines[0]
    if (line) {
      line.quantity = '0'
    }
    expect(formToBody(form, t, true).errors['line.0.quantity']).toBeDefined()

    form.lines = []
    expect(formToBody(form, t, true).errors.lines).toBeDefined()
  })
})

describe('docToForm', () => {
  it('writes a stored draft back as editable text', () => {
    const doc = {
      id: 'd-1',
      type: 'correction',
      state: 'draft',
      customer_id: 'c-1',
      vat_regime: 'standard',
      language: 'en',
      service_date: '2026-11-01',
      service_date_to: null,
      due_date: null,
      valid_until: null,
      discount_percent_permyriad: 750,
      discount_amount_cents: null,
      payment_terms: 'Net 14',
      notes: null,
      customer_reference: 'PO-7',
      lines: [
        {
          position: 1,
          catalogue_item_id: 'i-1',
          description: 'Consulting',
          quantity_thousandths: 2500,
          unit: 'hour',
          unit_price_cents: 9500,
          vat_rate_permyriad: 1900,
          line_discount_percent_permyriad: null,
          line_discount_amount_cents: 1250,
        },
      ],
    } as unknown as Doc

    const form = docToForm(doc, 'de')

    expect(form).toMatchObject({
      type: 'correction',
      customerId: 'c-1',
      regime: 'standard',
      discountMode: 'percent',
      discountValue: '7,5',
      paymentTerms: 'Net 14',
      customerReference: 'PO-7',
    })
    expect(form.lines[0]).toMatchObject({
      catalogueItemId: 'i-1',
      quantity: '2,5',
      unitPrice: '95,00',
      vatRate: '19',
      discountMode: 'amount',
      discountValue: '12,50',
    })
  })
})

describe('documentFieldErrors', () => {
  it('maps API paths to the form fields', () => {
    expect(fieldOfPath('lines[1].vat_rate_permyriad')).toBe('line.1.vatRate')
    expect(fieldOfPath('lines[0].line_discount_amount_cents')).toBe('line.0.discount')
    expect(fieldOfPath('lines[2].unit_price_cents')).toBe('line.2.unitPrice')
    expect(fieldOfPath('customer.address.city')).toBe('customer')
    expect(fieldOfPath('service_period')).toBe('serviceDate')
    expect(fieldOfPath('discount_percent_permyriad')).toBe('discount')
    expect(fieldOfPath('seller.tax_number')).toBeNull()
  })

  it('translates a tax exempt line error onto its line, never as raw English', () => {
    const reason =
      'a tax exempt line (rate 0) needs an exemption reason under the standard regime, which is not supported yet'

    const { fields, general } = documentFieldErrors(
      {
        'lines[1].vat_rate_permyriad': reason,
        'seller.tax_number': 'a Steuernummer or USt-IdNr is required',
      },
      t,
      () => true,
    )

    expect(fields['line.1.vatRate']).toBe('[documents.reasons.exemptLine]')
    expect(general).toEqual([
      {
        path: 'seller.tax_number',
        label: '[documents.paths.seller_tax_number]',
        text: '[documents.reasons.taxNumberRequired]',
      },
    ])
  })

  it('falls back to the generic text for a reason it does not know', () => {
    const { fields } = documentFieldErrors({ notes: 'something new the server says' }, t)

    expect(fields.notes).toBe('[documents.reasons.invalid]')
  })
})

// Every reason the server can emit for a draft or an issue, read from
// routes_documents.go, docissue/ and docpayload/ (validate.go, printable.go).
const REASON_CASES: [string, string, string][] = [
  ['at most 5000 characters can be printed', 'notes', 'tooManyChars'],
  ['at most 100 lines can be printed', 'notes', 'tooManyLines'],
  ['at most 100 lines can be printed', 'lines', 'tooManyLines'],
  ['at most 100 lines', 'lines', 'tooManyLines'],
  ['control character U+0007 is not allowed', 'notes', 'controlChar'],
  ['bidirectional control character U+202E is not allowed', 'notes', 'bidiChar'],
  ["character '€' (U+20AC) cannot be printed with the bundled font", 'notes', 'unprintableChar'],
  ['invalid UTF-8 or replacement character U+FFFD is not allowed', 'payment_terms', 'invalidUtf8'],
  ['the document is too large to print: at most 1000 bytes of data', 'lines', 'tooLarge'],
  ['the type of a draft never changes', 'type', 'typeFixed'],
  ['must be invoice or offer', 'type', 'invoiceOrOffer'],
  ['required when a line is tax exempt', 'tax_note', 'taxNoteRequired'],
  ['from must not be after to', 'service_period', 'periodOrder'],
  ['must be empty for an offer', 'service_period', 'offerNoPeriod'],
  ['required for an invoice', 'service_period', 'servicePeriodRequired'],
  ['must be a date in the form YYYY-MM-DD', 'service_period.from', 'invalidDate'],
  [
    'must be an assigned ISO 3166-1 alpha-2 code such as DE',
    'customer.address.country_code',
    'countryCode',
  ],
  ['must be one of [piece hour day]', 'lines[0].unit', 'oneOf'],
  ['must be 1900 as internal/money computes it', 'totals.grand_total_cents', 'figuresMismatch'],
  ['must be 42', 'totals.vat[0].vat_cents', 'figuresMismatch'],
  ['must have 2 buckets, one per rate', 'totals.vat', 'figuresMismatch'],
  [
    'must be net_total_cents minus document_discount_cents',
    'totals.net_after_document_discount_cents',
    'figuresMismatch',
  ],
  [
    'the bucket nets must add up to net_after_document_discount_cents',
    'totals.vat',
    'figuresMismatch',
  ],
  ['must be the sum of the bucket VAT', 'totals.vat_total_cents', 'figuresMismatch'],
  ['one bucket per VAT rate is required', 'totals.vat', 'figuresMismatch'],
  ['must be positive', 'number_value', 'numbering'],
  ['must be 1 or higher for a correction', 'correction_seq', 'numbering'],
  ['required for a correction', 'corrects', 'numbering'],
  ['some numbering error', 'number', 'numbering'],
  ['some template error', 'seller.number_formats.invoice', 'numbering'],
  [
    'the customer is deleted: a document for a deleted customer cannot be issued',
    'customer_id',
    'customerDeleted',
  ],
  ['a Steuernummer or USt-IdNr is required', 'seller.tax_number', 'taxNumberRequired'],
  ['must be at most 12 characters', 'seller.iban', 'sellerInvalid'],
  ['something the customer lacks', 'customer.address.city', 'customerInvalid'],
  ['something new', 'notes', 'invalid'],
]

describe('every reason of the server', () => {
  it.each(REASON_CASES)('maps %s at %s to %s', (reason, path, key) => {
    expect(reasonKey(reason, path)).toBe(key)
  })

  it('has a German and an English text for every key it maps to', () => {
    for (const [, , key] of REASON_CASES) {
      expect(en.documents.reasons, key).toHaveProperty(key)
      expect(de.documents.reasons, key).toHaveProperty(key)
    }
  })

  it('names the offending character as text, with its code point', () => {
    const text = reasonText(
      "character '<' (U+003C) cannot be printed with the bundled font",
      'notes',
      (key, params) => [key, String(params?.char), String(params?.code)].join('|'),
    )

    expect(text).toBe('documents.reasons.unprintableChar|<|U+003C')
  })

  it('does not echo an escaped character as raw escape text', () => {
    const text = reasonText(
      "character '\\u200b' (U+200B) cannot be printed with the bundled font",
      'notes',
      (key, params) => [key, String(params?.char), String(params?.code)].join('|'),
    )

    expect(text).toBe('documents.reasons.unprintableChar||U+200B')
  })

  it.each([
    'service_period',
    'service_period.from',
    'service_period.to',
    'tax_note',
    'issue_date',
    'number',
    'correction_seq',
    'corrects',
    'seller.tax_number',
    'seller.address.city',
    'seller.number_formats.correction',
    'customer.address.postal_code',
    'lines[2].description',
    'totals.vat[0].vat_cents',
  ])('labels the path %s in words, never by the raw path', (path) => {
    const te = (key: string) =>
      key.replace('documents.paths.', '') in (en.documents.paths as Record<string, string>)
    const label = pathLabel(
      path,
      (key, params) => `${key}${params ? JSON.stringify(params) : ''}`,
      te,
    )

    expect(label).not.toContain('unknown')
    expect(label).toContain('documents.paths.')
  })

  it('has a label in both languages for every path it labels', () => {
    for (const key of Object.keys(en.documents.paths)) {
      expect(de.documents.paths, key).toHaveProperty(key)
    }
  })

  it('falls back to a generic label for a path it does not know', () => {
    const label = pathLabel(
      'something.else',
      (key) => key,
      () => false,
    )

    expect(label).toBe('documents.paths.unknown')
  })
})
