import { describe, expect, it } from 'vitest'

import HomePage from './components/HomePage.vue'
import NotFoundPage from './components/NotFoundPage.vue'
import { componentForPath } from './pages'

describe('componentForPath', () => {
  const cases = [
    { name: 'the German start page', path: '/de/', want: HomePage },
    { name: 'the English start page', path: '/en/', want: HomePage },
    { name: 'an unknown path', path: '/de/unknown', want: NotFoundPage },
    { name: 'a path without a language', path: '/unknown', want: NotFoundPage },
    { name: 'the slash form of a page', path: '/de/imprint/', want: NotFoundPage },
    { name: 'a terms page the site does not have', path: '/de/terms', want: NotFoundPage },
  ]

  for (const tc of cases) {
    it(tc.name, () => {
      expect(componentForPath(tc.path)).toBe(tc.want)
    })
  }

  it('gives the legal pages their own component, not the start page', () => {
    expect(componentForPath('/de/imprint')).not.toBe(HomePage)
    expect(componentForPath('/en/privacy')).not.toBe(NotFoundPage)
  })
})
