import { describe, expect, it } from 'vitest'

import { routes } from '.'

describe('routes', () => {
  it('keeps the existing named routes', () => {
    const names = routes.map((r) => r.name).filter(Boolean)
    expect(names).toEqual(expect.arrayContaining(['home', 'login', 'accept-invite']))
    expect(new Set(names).size).toBe(names.length)
  })

  it('keeps the catch-all last', () => {
    expect(routes.at(-1)?.path).toBe('/:pathMatch(.*)*')
  })
})
