import type { RouteRecordRaw } from 'vue-router'

// Asset register and depreciation schedule (issue #79). Every role reads the
// register; creating, editing and disposing is the owner's, shown by the
// views from the session's role.
//
// The paths are /fixed-assets, not /assets: the frontend server owns
// /assets/ for the hashed Vite bundle and answers an unknown name there
// with a 404, never index.html, so a reload of /assets/42 would break.
export const assetRoutes: RouteRecordRaw[] = [
  {
    path: '/fixed-assets',
    name: 'assets',
    component: () => import('../../views/AssetsView.vue'),
    meta: { nav: { labelKey: 'assets.nav', order: 70 } },
  },
  {
    path: '/fixed-assets/:id',
    name: 'asset-detail',
    component: () => import('../../views/AssetDetailView.vue'),
  },
]
