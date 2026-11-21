import { describe, expect, it } from 'vitest'

import {
  docToForm,
  documentFieldErrors,
  emptyForm,
  emptyLine,
  fieldOfPath,
  formToBody,
} from './documentForm'
import type { Doc } from './documents'

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
    )

    expect(fields['line.1.vatRate']).toBe('[documents.reasons.exemptLine]')
    expect(general).toEqual([
      { path: 'seller.tax_number', text: '[documents.reasons.taxNumberRequired]' },
    ])
  })

  it('falls back to the generic text for a reason it does not know', () => {
    const { fields } = documentFieldErrors({ notes: 'something new the server says' }, t)

    expect(fields.notes).toBe('[documents.reasons.invalid]')
  })
})
