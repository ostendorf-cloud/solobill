import type { RouteRecordRaw } from 'vue-router'

// EÜR report view (issue #80). Every role reads the report; assigning an
// entry at the turn of the year is the owner's, shown by the view from the
// session's role. The year is part of the path so a report can be linked.
export const reportRoutes: RouteRecordRaw[] = [
  {
    path: '/reports/euer/:year(\\d{4})?',
    name: 'euer-report',
    component: () => import('../../views/EuerReportView.vue'),
    meta: { nav: { labelKey: 'reports.nav', order: 80 } },
  },
]
