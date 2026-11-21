import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import type * as ApiModule from './api'
import type * as SessionModule from './session'

// The client binds the global fetch when it is created, so the stub goes in
// before the module is imported. The import happens once for the file: a
// fresh import per test loads govue again and is slow under load.
const fetchStub = vi.fn<typeof fetch>()
let api: typeof ApiModule
let session: typeof SessionModule

function okResponse(): Response {
  return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
}

describe('api client', () => {
  beforeAll(async () => {
    vi.stubGlobal('fetch', fetchStub)
    api = await import('./api')
    session = await import('./session')
  }, 30000)

  beforeEach(() => {
    fetchStub.mockReset()
    fetchStub.mockImplementation(() => Promise.resolve(okResponse()))
    session.clearSession()
  })

  it('sends the CSRF token the shell stored on a mutating call', async () => {
    api.csrfToken.value = 'token-1'

    await api.apiClient.post('/customers', { name: 'X' })

    const [url, init] = fetchStub.mock.calls[0]
    expect(url).toBe('/api/v1/customers')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('token-1')
  })

  it('sends no token after the session was cleared', async () => {
    api.csrfToken.value = 'token-1'
    session.me.value = { email: 'a@b.test', name: 'A', role: 'owner', csrf_token: 'token-1' }

    session.clearSession()
    await api.apiClient.post('/customers', { name: 'X' })

    const [, init] = fetchStub.mock.calls[0]
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBeNull()
  })
})
