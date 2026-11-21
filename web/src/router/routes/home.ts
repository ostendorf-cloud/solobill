import type { RouteRecordRaw } from 'vue-router'

// HomeView lives at "/": govue's oidc.HandlerConfig.Success defaults to "/"
// when solobill's own OIDC wiring leaves it unset (see
// internal/auth/oidc.go and cmd/solobill/main.go's buildOIDCHandlers), so an
// OIDC login lands here without a backend change. HomeView's own mount
// check (GET /api/v1/me) is the "simple check" that sends an unauthenticated
// visitor back to LoginView; a dedicated navigation guard would only
// duplicate that same call before the view ever got to make it.
export const homeRoutes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'home',
    component: () => import('../../views/HomeView.vue'),
  },
]
