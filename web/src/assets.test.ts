import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { describe, expect, it } from 'vitest'

import {
  assetConflict,
  assetFieldErrors,
  bookValueNote,
  editBlock,
  germanToday,
  germanYear,
  type Asset,
} from './assets'
import deAssets from './i18n/locales/de/assets.json'
import enAssets from './i18n/locales/en/assets.json'

const asset: Asset = {
  id: 'a-1',
  name: 'Laptop',
  acquired_on: '2026-03-10',
  cost_basis_cents: 149900,
  method: 'linear',
  group: 'office_equipment',
  useful_life_months: 36,
  expense_id: null,
  opening_book_value_cents: null,
  opening_book_value_year: null,
  disposed_on: null,
  disposal_proceeds_cents: null,
  imported: false,
  book_value_cents: 149900,
  book_value_year: null,
  editable: true,
  created_at: '',
  updated_at: '',
}

describe('germanYear and germanToday', () => {
  it('follow the German calendar, not UTC', () => {
    const newYearInBerlin = new Date('2026-12-31T23:30:00Z')
    expect(germanYear(newYearInBerlin)).toBe(2027)
    expect(germanToday(newYearInBerlin)).toBe('2027-01-01')
    expect(germanYear(new Date('2026-12-31T22:30:00Z'))).toBe(2026)
  })
})

describe('editBlock', () => {
  it('follows the editable flag of the server, not the dates', () => {
    // An acquisition long ago with the flag set stays editable: the browser
    // has no date rule of its own.
    expect(editBlock({ ...asset, acquired_on: '2020-01-01' })).toBeNull()
    expect(editBlock({ ...asset, editable: false })).toBe('yearEnded')
  })

  it('names the disposal of a disposed asset', () => {
    expect(editBlock({ ...asset, editable: false, disposed_on: '2026-05-01' })).toBe('disposed')
  })
})

describe('bookValueNote', () => {
  const t = (key: string, named?: Record<string, unknown>) =>
    named ? `${key}:${JSON.stringify(named)}` : key

  it('names the year the value closes, or that no year has ended', () => {
    expect(bookValueNote({ ...asset, book_value_year: 2026 }, t)).toBe(
      'assets.bookValue.atEndOf:{"year":2026}',
    )
    expect(bookValueNote(asset, t)).toBe('assets.bookValue.beforeDepreciation')
  })
})

describe('assetFieldErrors', () => {
  const t = (key: string, named?: Record<string, unknown>) =>
    named ? `${key}:${JSON.stringify(named)}` : key

  it('maps the API fields to the form fields and the reasons to keys', () => {
    expect(
      assetFieldErrors(
        {
          acquired_on: 'must not be in the future',
          useful_life_months: 'required for linear, from 1 to 600',
          expense_id: 'must be a booked expense',
          opening_book_value_year: 'must not be after the current year',
        },
        t,
        'en',
      ),
    ).toEqual({
      acquiredOn: 'assets.reasons.future',
      usefulLife: 'assets.reasons.usefulLifeRequired',
      expense: 'assets.reasons.notBooked',
      opening: 'assets.reasons.openingYearFuture',
    })
  })

  it('writes the cost basis of the server without computing it', () => {
    const out = assetFieldErrors(
      { cost_basis_cents: "must be 149900, the expense's cost basis under the standard regime" },
      t,
      'de',
    )
    expect(out.cost).toContain('"amount":"1.499,00 EUR"')
  })

  it('falls back to the shared text for an unknown reason, never to English', () => {
    expect(assetFieldErrors({ name: 'something new' }, t, 'en')).toEqual({
      name: 'common.fieldErrors.invalid',
    })
  })
})

describe('assetConflict', () => {
  const failure = (status: number, code: string) =>
    new ApiError({ code, status, message: 'the expense is already an asset' })

  it.each([
    ['asset_disposed', 'disposed'],
    ['asset_year_ended', 'yearEnded'],
    ['expense_already_an_asset', 'expenseTaken'],
    [ErrorCode.conflict, 'other'],
  ] as const)('tells the 409 %s by its code', (code, want) => {
    expect(assetConflict(failure(409, code))).toBe(want)
  })

  it('is null for anything but a 409, whatever the message says', () => {
    expect(assetConflict(failure(400, 'expense_already_an_asset'))).toBeNull()
    expect(assetConflict(new Error('x'))).toBeNull()
  })
})

describe('assets locale files', () => {
  function keys(value: unknown, prefix = ''): string[] {
    if (typeof value === 'object' && value !== null) {
      return Object.entries(value).flatMap(([k, v]) => keys(v, `${prefix}${k}.`))
    }
    return [prefix]
  }

  it('have the same keys in German and English', () => {
    expect(keys(deAssets).sort()).toEqual(keys(enAssets).sort())
  })
})
