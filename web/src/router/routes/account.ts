import type { RouteRecordRaw } from 'vue-router'

// The account settings of the signed in user: language and colour mode
// (issue #174). Every role sees it. It is linked from the header, not from
// the main navigation, so it carries no meta.nav. The view sends a visitor
// without a session to the login.
export const accountRoutes: RouteRecordRaw[] = [
  {
    path: '/account',
    name: 'account',
    component: () => import('../../views/AccountView.vue'),
  },
]
