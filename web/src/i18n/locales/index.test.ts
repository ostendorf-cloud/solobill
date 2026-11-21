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

/** baseNames turns glob keys like './de/login.json' into ['login']. */
function baseNames(paths: string[]): string[] {
  return paths.map((p) => p.replace(/^.*\//, '').replace(/\.json$/, '')).sort()
}

describe('locales', () => {
  it('merges every German feature file, and only those', () => {
    expect(Object.keys(de).sort()).toEqual(baseNames(Object.keys(import.meta.glob('./de/*.json'))))
  })

  it('merges every English feature file, and only those', () => {
    expect(Object.keys(en).sort()).toEqual(baseNames(Object.keys(import.meta.glob('./en/*.json'))))
  })

  it('translates every German key into English and the reverse', () => {
    expect(keyPaths(en).sort()).toEqual(keyPaths(de).sort())
  })

  it('keeps the existing keys under their namespace', () => {
    expect(de.login.title).toBe('Anmelden')
    expect(en.acceptInvite.validation.token).toBe('Please enter the invite token.')
  })
})
