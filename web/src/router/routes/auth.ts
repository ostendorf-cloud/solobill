import type { RouteRecordRaw } from 'vue-router'

export const authRoutes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../../views/LoginView.vue'),
  },
  {
    path: '/accept-invite',
    name: 'accept-invite',
    component: () => import('../../views/AcceptInviteView.vue'),
  },
]
