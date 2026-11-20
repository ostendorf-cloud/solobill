import { describe, expect, it } from 'vitest'

import { de, en } from '.'

/** keyPaths lists every leaf key of a message tree as a dotted path. */
function keyPaths(tree: object, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([key, value]) =>
    typeof value === 'object' && value !== null
      ? keyPaths(value as object, `${prefix}${key}.`)
      : [`${prefix}${key}`],
  )
}

describe('locales', () => {
  it('merges the same feature namespaces for both languages', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(de).sort())
  })

  it('translates every German key into English and the reverse', () => {
    expect(keyPaths(en).sort()).toEqual(keyPaths(de).sort())
  })

  it('keeps the existing keys under their namespace', () => {
    expect(de.login.title).toBe('Anmelden')
    expect(en.acceptInvite.validation.token).toBe('Please enter the invite token.')
  })
})
