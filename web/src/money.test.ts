import { describe, expect, it } from 'vitest'

import { centsToInput, formatCents, parseCents } from './money'

describe('parseCents', () => {
  it.each([
    ['95', 9500],
    ['95,5', 9550],
    ['95.50', 9550],
    ['0,07', 7],
    [' 12 ', 1200],
  ])('reads %s as %i cents', (text, cents) => {
    expect(parseCents(text)).toBe(cents)
  })

  it.each(['', 'abc', '1,234,50', '12.345', '-5', '1e3'])('rejects %j', (text) => {
    expect(parseCents(text)).toBeNull()
  })
})

describe('formatCents', () => {
  it('writes German and English amounts', () => {
    expect(formatCents(9500, 'de')).toBe('95,00 EUR')
    expect(formatCents(123456789, 'en')).toBe('1,234,567.89 EUR')
    expect(formatCents(5, 'de')).toBe('0,05 EUR')
  })
})

describe('centsToInput', () => {
  it('round trips through parseCents', () => {
    expect(centsToInput(9550)).toBe('95.50')
    expect(parseCents(centsToInput(7))).toBe(7)
  })
})
