import { describe, expect, it } from 'vitest'

import { countedYear, euerCsvUrl, otherYear, reportYears } from './euerReport'

describe('reportYears', () => {
  it.each([
    ['before the first report year', new Date(2025, 5, 1), [2026]],
    ['in the first report year', new Date(2026, 10, 25), [2026]],
    ['two years later, newest first', new Date(2028, 0, 2), [2028, 2027, 2026]],
  ])('%s', (_name, today, want) => {
    expect(reportYears(today)).toEqual(want)
  })
})

describe('otherYear', () => {
  it.each([
    ['2026-12-22', 2027],
    ['2026-12-31', 2027],
    ['2027-01-01', 2026],
    ['2027-01-10', 2026],
  ])('%s may move to %i', (date, want) => {
    expect(otherYear(date)).toBe(want)
  })
})

describe('countedYear', () => {
  const entry = { kind: 'payment' as const, id: 'p', date: '2026-12-28', amount_cents: 1 }

  it('is the year of the date without a decision', () => {
    expect(countedYear({ ...entry, assigned_tax_year: null })).toBe(2026)
  })

  it('is the assigned year with one', () => {
    expect(countedYear({ ...entry, assigned_tax_year: 2027 })).toBe(2027)
  })
})

describe('euerCsvUrl', () => {
  it('names the CSV route of the year', () => {
    expect(euerCsvUrl(2026)).toBe('/api/v1/reports/euer/2026/csv')
  })
})
