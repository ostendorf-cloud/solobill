import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { describe, expect, it } from 'vitest'

import {
  assetFieldErrors,
  editBlock,
  germanToday,
  germanYear,
  isExpenseTaken,
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
  const now = new Date('2026-11-21T10:00:00Z')

  it('is null in the acquisition year', () => {
    expect(editBlock(asset, now)).toBeNull()
  })

  it('is yearEnded once the year of acquisition has ended in Germany', () => {
    expect(editBlock(asset, new Date('2026-12-31T23:00:00Z'))).toBe('yearEnded')
    expect(editBlock({ ...asset, acquired_on: '2025-12-31' }, now)).toBe('yearEnded')
  })

  it('reads the opening year of an imported asset', () => {
    const imported = {
      ...asset,
      acquired_on: '2020-01-01',
      opening_book_value_cents: 1000,
      opening_book_value_year: 2026,
    }
    expect(editBlock(imported, now)).toBeNull()
    expect(editBlock({ ...imported, opening_book_value_year: 2025 }, now)).toBe('yearEnded')
  })

  it('is disposed for a disposed asset', () => {
    expect(editBlock({ ...asset, disposed_on: '2026-05-01' }, now)).toBe('disposed')
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

describe('isExpenseTaken', () => {
  it('tells the 409 by its text', () => {
    const taken = new ApiError({
      code: ErrorCode.conflict,
      status: 409,
      message: 'the expense is already an asset',
    })
    const closed = new ApiError({ code: ErrorCode.conflict, status: 409, message: 'closed' })
    expect(isExpenseTaken(taken)).toBe(true)
    expect(isExpenseTaken(closed)).toBe(false)
    expect(isExpenseTaken(new Error('x'))).toBe(false)
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
