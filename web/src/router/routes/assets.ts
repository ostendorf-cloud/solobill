import type { RouteRecordRaw } from 'vue-router'

// Asset register and depreciation schedule (issue #79). Every role reads the
// register; creating, editing and disposing is the owner's, shown by the
// views from the session's role.
export const assetRoutes: RouteRecordRaw[] = [
  {
    path: '/assets',
    name: 'assets',
    component: () => import('../../views/AssetsView.vue'),
    meta: { nav: { labelKey: 'assets.nav', order: 70 } },
  },
  {
    path: '/assets/:id',
    name: 'asset-detail',
    component: () => import('../../views/AssetDetailView.vue'),
  },
]
