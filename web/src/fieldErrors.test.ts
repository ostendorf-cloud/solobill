import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

import { REASON_KEYS, saveErrorText, translateDetails } from './fieldErrors'
import { de, en } from './i18n/locales'

// The contract: every reason the server can put into `details` for the
// customer and catalogue endpoints. These mirror routes_customers.go,
// routes_catalogue.go and govue's decoder ("wrong type"). A new reason there
// needs a line here and a key under common.fieldErrors in both locales.
const SERVER_REASONS = [
  'required',
  'must be de or en',
  'must be an ISO 3166-1 alpha-2 code',
  'must not be negative',
  'must be piece, hour or day',
  'wrong type',
]

function translator(locale: 'de' | 'en') {
  return createI18n({ legacy: false, locale, messages: { de, en } }).global.t
}

describe('translateDetails', () => {
  it.each(SERVER_REASONS)('has a German and an English text for %j', (reason) => {
    const key = REASON_KEYS[reason]
    expect(key).toBeDefined()
    const fieldErrors = { de: de.common.fieldErrors, en: en.common.fieldErrors } as Record<
      string,
      Record<string, string>
    >
    expect(fieldErrors.de[key]).toBeTruthy()
    expect(fieldErrors.en[key]).toBeTruthy()
    expect(translateDetails({ f: reason }, translator('en')).f).toBe(fieldErrors.en[key])
    expect(translateDetails({ f: reason }, translator('de')).f).toBe(fieldErrors.de[key])
  })

  it('never shows the raw reason', () => {
    for (const reason of SERVER_REASONS) {
      expect(translateDetails({ f: reason }, translator('en')).f).not.toBe(reason)
    }
  })

  it('falls back to the generic text for an unknown reason', () => {
    expect(translateDetails({ f: 'brand new' }, translator('en')).f).toBe('Invalid value.')
    expect(translateDetails({ f: 'brand new' }, translator('de')).f).toBe('Ungültiger Wert.')
  })
})

describe('saveErrorText', () => {
  const t = translator('en')
  const failure = (status: number, code: string, details?: Record<string, string>) =>
    new ApiError({ code, message: 'x', status, details })

  it('names the fields only for field errors and conflicts', () => {
    const marked = 'The customer could not be saved. Check the marked fields.'
    expect(
      saveErrorText(
        failure(400, ErrorCode.invalidArgument, { name: 'required' }),
        t,
        'customers.saveError',
      ),
    ).toBe(marked)
    expect(saveErrorText(failure(409, ErrorCode.conflict), t, 'customers.saveError')).toBe(marked)
    expect(saveErrorText(failure(400, ErrorCode.invalidArgument), t, 'customers.saveError')).toBe(
      en.common.errors.generic,
    )
  })

  it('maps the status class', () => {
    expect(saveErrorText(failure(403, ErrorCode.permissionDenied), t, 'x')).toBe(
      en.common.errors.forbidden,
    )
    expect(saveErrorText(failure(503, ErrorCode.unavailable), t, 'x')).toBe(en.common.errors.server)
    expect(saveErrorText(failure(0, ErrorCode.network), t, 'x')).toBe(en.common.errors.network)
    expect(saveErrorText(new Error('boom'), t, 'x')).toBe(en.common.errors.generic)
  })
})
