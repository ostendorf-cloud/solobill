import { describe, expect, it } from 'vitest'

import { routeModules, routes } from '.'
import { navigationEntries } from './meta'

/** baseName turns './routes/assets.ts' into 'assets'. */
function baseName(path: string): string {
  return path.replace(/^.*\//, '').replace(/\.ts$/, '')
}

describe('routes', () => {
  it('keeps the existing named routes', () => {
    const names = routes.map((r) => r.name).filter(Boolean)
    expect(names).toEqual(expect.arrayContaining(['home', 'login', 'accept-invite']))
    expect(new Set(names).size).toBe(names.length)
  })

  it('keeps the catch-all last', () => {
    expect(routes.at(-1)?.path).toBe('/:pathMatch(.*)*')
  })

  // The frontend server (cmd/frontend, govue app mode) answers /assets and
  // /api itself, with the bundle and a JSON 404, and the Ingress sends /api
  // to the API. A route under either would never reach the app on a reload.
  it('keeps every route path off the prefixes the server owns', () => {
    const owned = ['/assets', '/api']
    const paths = Object.values(routeModules)
      .flat()
      .map((r) => r.path)
    expect(paths.length).toBeGreaterThan(0)
    for (const path of paths) {
      for (const prefix of owned) {
        expect(path === prefix || path.startsWith(`${prefix}/`), `${path} is under ${prefix}`).toBe(
          false,
        )
      }
    }
  })

  it('imports every route module file, and only those', () => {
    const files = Object.keys(import.meta.glob('./routes/*.ts'))
      .filter((p) => !p.endsWith('.test.ts'))
      .map(baseName)
      .sort()
    expect(Object.keys(routeModules).sort()).toEqual(files)
  })
})

describe('navigationEntries', () => {
  it('lists named routes with meta.nav by order', () => {
    const entries = navigationEntries([
      { path: '/b', name: 'b', component: {}, meta: { nav: { labelKey: 'b.nav', order: 20 } } },
      { path: '/x', name: 'x', component: {} },
      { path: '/a', name: 'a', component: {}, meta: { nav: { labelKey: 'a.nav', order: 10 } } },
    ])
    expect(entries.map((e) => e.name)).toEqual(['a', 'b'])
  })
})
