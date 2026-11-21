import type { RouteRecordRaw } from 'vue-router'

// Expenses: the list per year and the receipt capture and detail view
// (issue #77). Other income and the asset register are other issues.
export const expenseRoutes: RouteRecordRaw[] = [
  {
    path: '/expenses',
    name: 'expenses',
    component: () => import('../../views/ExpensesView.vue'),
    meta: { nav: { labelKey: 'expenses.nav', order: 60 } },
  },
  {
    // /expenses/capture starts with the upload step, /expenses/capture/<id>
    // opens a saved expense. One record keeps the view alive when a first
    // save puts the id into the address.
    path: '/expenses/capture/:id?',
    name: 'expense-capture',
    component: () => import('../../views/ExpenseCaptureView.vue'),
  },
]
