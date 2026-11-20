import type { RouteRecordRaw } from 'vue-router'

// Catalogue list with create, edit and soft delete (issue #72).
export const catalogueRoutes: RouteRecordRaw[] = [
  {
    path: '/catalogue',
    name: 'catalogue',
    component: () => import('../../views/CatalogueView.vue'),
    meta: { nav: { labelKey: 'catalogue.nav', order: 40 } },
  },
]
