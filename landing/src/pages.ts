import {
  legalRouteDefs,
  legalRoutes,
  localizeRoutes,
  type LegalPages,
  type LocalizedRoute,
  type RouteDef,
} from '@ostendorf-cloud/govue'
import { h, type Component } from 'vue'

import HomePage from './components/HomePage.vue'
import LegalPlaceholder from './components/LegalPlaceholder.vue'
import NotFoundPage from './components/NotFoundPage.vue'

/** One component per legal page. A site with terms adds them here. */
const legalPages: LegalPages<Component> = {
  imprint: { render: () => h(LegalPlaceholder, { page: 'imprint' }) },
  privacy: { render: () => h(LegalPlaceholder, { page: 'privacy' }) },
}

/**
 * The route table of the site. Every public route is prerendered once per
 * language and listed in the sitemap, and the frontend server serves it at
 * its exact path. A new page is one line here and one case in componentFor.
 */
export const routes: RouteDef[] = [{ path: '/', public: true }, ...legalRouteDefs(legalPages)]

/** The records of the legal pages, to find the component of a variant. */
const legalRecords = legalRoutes(legalPages)

/** The component of one language variant of a route. */
export function componentFor(variant: LocalizedRoute): Component {
  if (variant.source === '/') {
    return HomePage
  }
  const record = legalRecords.find((r) => r.path === variant.path)
  if (record === undefined) {
    throw new Error(`landing: no component for the route ${variant.path}`)
  }
  return record.component
}

/** The prefixed paths of every page, the sitemap's locs. */
const variants = localizeRoutes(routes)

/**
 * The component of the page at a browser path, the 404 page for a path that
 * is no page. The prerendered markup is for the first paint and the client
 * mounts the same component over it.
 */
export function componentForPath(path: string): Component {
  const variant = variants.find((v) => v.path === path)
  return variant === undefined ? NotFoundPage : componentFor(variant)
}

export { NotFoundPage }
