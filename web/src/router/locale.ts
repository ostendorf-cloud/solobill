// The language lives in the URL: every app route sits under /de or /en. This
// module wraps the feature route modules (./routes) in that prefix, so a
// feature keeps writing plain paths such as '/documents'. govue owns the
// pieces: the language list, the storage key, pickLocale and localizedPath.
import {
  LANGUAGE_STORAGE_KEY,
  localeCodes,
  localeFromPath,
  localizedPath,
  pickLocale,
  type LanguageState,
  type LocaleCode,
} from '@ostendorf-cloud/govue'
import type {
  RouteLocationNormalizedLoaded,
  RouteLocationRaw,
  RouteRecordRaw,
  Router,
} from 'vue-router'

import NotFoundView from '../views/NotFoundView.vue'

const LOCALE_PARAM = `/:locale(${localeCodes.join('|')})`
const NOT_FOUND = 'not-found'
const NOT_FOUND_BARE = 'not-found-bare'

/** preferredLocale is the language for a URL that names none: the choice
 *  govue remembered, else the browser language, else German. */
export function preferredLocale(): LocaleCode {
  let stored: string | undefined
  try {
    stored = localStorage.getItem(LANGUAGE_STORAGE_KEY) ?? undefined
  } catch {
    // Blocked storage: no remembered choice.
  }
  return pickLocale(stored, navigator.languages ?? [navigator.language])
}

/** initialLocale is the language the app starts in: the one in the URL, else
 *  the preferred one. A path without a prefix is redirected by the router, so
 *  the preferred language only seeds that case. */
export function initialLocale(pathname: string): LocaleCode {
  return localeFromPath(pathname) ?? preferredLocale()
}

/** withLocale puts the feature routes under the language prefix and adds the
 *  routes that need no feature: '/' goes to the preferred language, a path
 *  without a prefix that is an app route goes there too, and anything else is
 *  a 404 view. */
export function withLocale(
  features: RouteRecordRaw[],
  getRouter: () => Router,
  preferred: () => LocaleCode = preferredLocale,
): RouteRecordRaw[] {
  const children = features.map((r) => ({ ...r, path: r.path.replace(/^\//, '') }))
  return [
    {
      path: '/',
      redirect: (to) => ({ path: `/${preferred()}/`, query: to.query, hash: to.hash }),
    },
    {
      path: LOCALE_PARAM,
      children: [
        ...children,
        { path: ':pathMatch(.*)*', name: NOT_FOUND, component: NotFoundView },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      name: NOT_FOUND_BARE,
      component: NotFoundView,
      // A deep link or a bookmark from before the prefix existed. Only a path
      // that is an app route under the prefix moves; /fr/documents stays a 404.
      beforeEnter: (to) => {
        const path = `/${preferred()}${to.path}`
        if (getRouter().resolve(path).name === NOT_FOUND) {
          return true
        }
        return { path, query: to.query, hash: to.hash, replace: true }
      },
    },
  ]
}

/** routeLocale is the language a link from this route is built in. The 404
 *  route for an unknown prefix has no locale param, and a named route needs
 *  one, so a link there takes the preferred language instead. */
export function routeLocale(route: RouteLocationNormalizedLoaded): string {
  const current = route.params.locale
  return typeof current === 'string' ? current : preferredLocale()
}

/** homeLocation is the link to the start page from this route. */
export function homeLocation(route: RouteLocationNormalizedLoaded): RouteLocationRaw {
  return { name: 'home', params: { locale: routeLocale(route) } }
}

/** localizedHref is the current page in another language: same path
 *  below the prefix, same query and hash. The 404 view of a path without a
 *  known prefix has no page to carry over, so it links to the start page of
 *  that language. */
export function localizedHref(router: Router, code: string): string {
  const current = router.currentRoute.value
  if (current.name === NOT_FOUND_BARE) {
    return router.resolve({ path: localizedPath('/', code as LocaleCode) }).href
  }
  return router.resolve({
    path: localizedPath(current.path, code as LocaleCode),
    query: current.query,
    hash: current.hash,
  }).href
}

/** followUrlLocale makes the language of the URL the app's language, which
 *  govue also writes to <html lang> and remembers. A URL with an unknown
 *  prefix names no language: the 404 view stays in the current app language. */
export function followUrlLocale(router: Router, language: LanguageState): void {
  router.beforeEach((to) => {
    const code = localeFromPath(to.path)
    if (code !== undefined && code !== language.locale.value) {
      language.setLocale(code)
    }
  })
}
