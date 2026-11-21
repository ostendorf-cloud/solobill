import type { RouteRecordRaw } from 'vue-router'

// Seller profile and VAT regime settings (issue #68). Owner only, as the
// profile endpoints are.
export const profileRoutes: RouteRecordRaw[] = [
  {
    path: '/settings',
    name: 'settings',
    component: () => import('../../views/SettingsView.vue'),
    meta: { nav: { labelKey: 'profile.nav', order: 90, roles: ['owner'] } },
  },
]
