import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'

import { routes } from '.'
import { localizedHref, preferredLocale, withLocale } from './locale'

const stub = { template: '<div />' }

function makeRouter(preferred: () => 'de' | 'en' = () => 'de'): Router {
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: withLocale(
      [
        { path: '/', name: 'home', component: stub },
        { path: '/login', name: 'login', component: stub },
        { path: '/documents/:id', name: 'document-detail', component: stub },
      ],
      () => router,
      preferred,
    ),
  })
  return router
}

describe('withLocale', () => {
  it('sends / to the preferred language', async () => {
    const router = makeRouter(() => 'en')
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/en/')
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('serves a prefixed path and keeps the language on a named move', async () => {
    const router = makeRouter()
    await router.push('/en/documents/7')
    expect(router.currentRoute.value.name).toBe('document-detail')
    expect(router.currentRoute.value.params).toMatchObject({ locale: 'en', id: '7' })

    await router.push({ name: 'login' })
    expect(router.currentRoute.value.fullPath).toBe('/en/login')
  })

  it('redirects a deep link without a prefix and keeps query and hash', async () => {
    const router = makeRouter(() => 'en')
    await router.push('/documents/7?tab=lines#top')
    expect(router.currentRoute.value.fullPath).toBe('/en/documents/7?tab=lines#top')
    expect(router.currentRoute.value.name).toBe('document-detail')
  })

  it('shows the 404 view for an unknown prefix and an unknown path', async () => {
    const router = makeRouter()
    await router.push('/fr/documents/7')
    expect(router.currentRoute.value.name).toBe('not-found-bare')
    expect(router.currentRoute.value.fullPath).toBe('/fr/documents/7')

    await router.push('/de/nothing/here')
    expect(router.currentRoute.value.name).toBe('not-found')
  })

  it('builds the same page in the other language', async () => {
    const router = makeRouter()
    await router.push('/de/documents/7?tab=lines#top')
    expect(localizedHref(router, 'en')).toBe('/en/documents/7?tab=lines#top')
    await router.push('/de/')
    expect(localizedHref(router, 'en')).toBe('/en/')
  })
})

describe('app routes', () => {
  it('resolve under both prefixes', () => {
    const router: Router = createRouter({
      history: createMemoryHistory(),
      routes: withLocale(routes, () => router),
    })
    for (const code of ['de', 'en']) {
      expect(router.resolve(`/${code}/login`).name).toBe('login')
      expect(router.resolve(`/${code}/`).name).toBe('home')
      expect(router.resolve(`/${code}/documents/edit`).name).toBe('document-edit')
    }
  })
})

describe('preferredLocale', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('takes the remembered choice before the browser language', () => {
    localStorage.setItem('govue-language', 'de')
    expect(preferredLocale()).toBe('de')
  })

  it('falls back to the browser language and then to German', () => {
    expect(preferredLocale()).toBe('en')
  })
})
