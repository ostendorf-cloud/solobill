import { afterEach, describe, expect, it, vi } from 'vitest'

// The client binds the global fetch when it is created, so the stub goes in
// before the module is imported fresh.
async function loadApi(fetchStub: typeof fetch) {
  vi.resetModules()
  vi.stubGlobal('fetch', fetchStub)
  return import('./api')
}

function okResponse(): Response {
  return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
}

describe('api client', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the CSRF token the shell stored on a mutating call', async () => {
    const fetchStub = vi.fn<typeof fetch>(() => Promise.resolve(okResponse()))
    const { apiClient, csrfToken } = await loadApi(fetchStub)
    csrfToken.value = 'token-1'

    await apiClient.post('/customers', { name: 'X' })

    const [url, init] = fetchStub.mock.calls[0]
    expect(url).toBe('/api/v1/customers')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('token-1')
  })

  it('sends no token after the session was cleared', async () => {
    const fetchStub = vi.fn<typeof fetch>(() => Promise.resolve(okResponse()))
    const { apiClient, csrfToken } = await loadApi(fetchStub)
    const { me, clearSession } = await import('./session')
    csrfToken.value = 'token-1'
    me.value = { email: 'a@b.test', name: 'A', role: 'owner', csrf_token: 'token-1' }

    clearSession()
    await apiClient.post('/customers', { name: 'X' })

    const [, init] = fetchStub.mock.calls[0]
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBeNull()
  })
})
