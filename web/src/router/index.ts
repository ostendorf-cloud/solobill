import { createRouter, createWebHistory, type Router } from 'vue-router'

// HomeView lives at "/": govue's oidc.HandlerConfig.Success defaults to "/"
// when solobill's own OIDC wiring leaves it unset (see
// internal/auth/oidc.go and cmd/solobill/main.go's buildOIDCHandlers), so an
// OIDC login lands here without a backend change. HomeView's own mount
// check (GET /api/v1/me) is the "simple check" that sends an unauthenticated
// visitor back to LoginView; a dedicated navigation guard would only
// duplicate that same call before the view ever got to make it.
const routes = [
  {
    path: '/',
    name: 'home',
    component: () => import('../views/HomeView.vue'),
  },
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/LoginView.vue'),
  },
  {
    path: '/accept-invite',
    name: 'accept-invite',
    component: () => import('../views/AcceptInviteView.vue'),
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: { name: 'home' },
  },
]

export function createAppRouter(): Router {
  return createRouter({
    history: createWebHistory(),
    routes,
  })
}
