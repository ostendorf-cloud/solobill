import type { RouteRecordRaw } from 'vue-router'

/** NavMeta puts a route into the main navigation. A feature adds it to its
 *  own route's `meta.nav`; the navigation component (issue #72) renders
 *  navigationEntries(routes) and is never edited by another feature. */
export interface NavMeta {
  /** i18n key of the label, inside the feature's own namespace. */
  labelKey: string
  /** Position in the navigation, ascending. Steps of 10 leave room. */
  order: number
  /** Roles that see the entry. Absent means every logged in role. */
  roles?: readonly ('owner' | 'member')[]
}

declare module 'vue-router' {
  interface RouteMeta {
    nav?: NavMeta
  }
}

/** NavEntry is one navigation item derived from a named route. */
export interface NavEntry {
  name: string
  nav: NavMeta
}

/** navigationEntries lists every named route carrying meta.nav, ordered by
 *  nav.order. Child routes are not walked: navigation is one level. */
export function navigationEntries(routes: readonly RouteRecordRaw[]): NavEntry[] {
  return routes
    .flatMap((r) =>
      typeof r.name === 'string' && r.meta?.nav ? [{ name: r.name, nav: r.meta.nav }] : [],
    )
    .sort((a, b) => a.nav.order - b.nav.order)
}
