import type { RouteRecordRaw } from 'vue-router'

// Offers, invoices, corrections and their payments (issue #73): the list, the
// detail view and the draft editor.
export const documentRoutes: RouteRecordRaw[] = [
  {
    path: '/documents',
    name: 'documents',
    component: () => import('../../views/DocumentsView.vue'),
    meta: { nav: { labelKey: 'documents.nav', order: 20 } },
  },
  {
    // /documents/edit?type=offer starts a new draft, /documents/edit/<id>
    // opens a saved one. One record keeps the view alive when a first save
    // puts the id into the address. The static segment ranks above the
    // detail route's :id, so "edit" is never read as a document id.
    path: '/documents/edit/:id?',
    name: 'document-edit',
    component: () => import('../../views/DocumentEditorView.vue'),
  },
  {
    path: '/documents/:id',
    name: 'document-detail',
    component: () => import('../../views/DocumentDetailView.vue'),
  },
]
