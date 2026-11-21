import type { RouteRecordRaw } from 'vue-router'

// Customer list with create, edit and soft delete (issue #72).
export const customerRoutes: RouteRecordRaw[] = [
  {
    path: '/customers',
    name: 'customers',
    component: () => import('../../views/CustomersView.vue'),
    meta: { nav: { labelKey: 'customers.nav', order: 30 } },
  },
]
