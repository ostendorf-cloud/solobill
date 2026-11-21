import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import '../meta'
import { navigationEntries } from '../meta'
import { documentRoutes } from './documents'

const router = createRouter({ history: createMemoryHistory(), routes: documentRoutes })

describe('documentRoutes', () => {
  it('puts the list into the navigation under the documents namespace', () => {
    expect(navigationEntries(documentRoutes)).toEqual([
      { name: 'documents', nav: { labelKey: 'documents.nav', order: 20 } },
    ])
  })

  it('reads edit as the editor and never as a document id', () => {
    expect(router.resolve('/documents/edit').name).toBe('document-edit')
    expect(router.resolve('/documents/edit/d-1')).toMatchObject({
      name: 'document-edit',
      params: { id: 'd-1' },
    })
    expect(router.resolve('/documents/d-1')).toMatchObject({
      name: 'document-detail',
      params: { id: 'd-1' },
    })
    expect(router.resolve('/documents').name).toBe('documents')
  })
})
