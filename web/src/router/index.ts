import { createRouter, createWebHistory, type RouteRecordRaw, type Router } from 'vue-router'

import './meta'
import { assetRoutes } from './routes/assets'
import { authRoutes } from './routes/auth'
import { catalogueRoutes } from './routes/catalogue'
import { customerRoutes } from './routes/customers'
import { documentRoutes } from './routes/documents'
import { expenseRoutes } from './routes/expenses'
import { homeRoutes } from './routes/home'
import { importRoutes } from './routes/imports'
import { profileRoutes } from './routes/profile'
import { reportRoutes } from './routes/reports'

// One module per feature under ./routes, keyed by its file name, so each
// feature issue edits only its own file. This index is not edited again
// by a feature issue: see AGENTS.md's conflict zone rule. A test fails
// when a file under ./routes is missing here.
export const routeModules: Record<string, RouteRecordRaw[]> = {
  home: homeRoutes,
  auth: authRoutes,
  profile: profileRoutes,
  customers: customerRoutes,
  catalogue: catalogueRoutes,
  documents: documentRoutes,
  expenses: expenseRoutes,
  assets: assetRoutes,
  reports: reportRoutes,
  imports: importRoutes,
}

// The catch-all stays last.
export const routes: RouteRecordRaw[] = [
  ...Object.values(routeModules).flat(),
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
