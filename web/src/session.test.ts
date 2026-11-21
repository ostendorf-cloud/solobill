import { ApiError, ErrorCode } from '@ostendorf-cloud/govue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { apiClient, csrfToken } from './api'
import { clearSession, loadSession, me, sessionFailed, status } from './session'

vi.mock('./api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const owner = { email: 'o@b.test', name: 'Owner', role: 'owner', csrf_token: 'csrf-owner' }

describe('session', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    clearSession()
  })

  it('loads the user, the token and the status', async () => {
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      Promise.resolve(path === '/me' ? owner : { object_store: {} }),
    )

    await loadSession()

    expect(me.value?.name).toBe('Owner')
    expect(csrfToken.value).toBe('csrf-owner')
    expect(status.value).not.toBeNull()
  })

  it('does not bring the old user back when /me answers after a clear', async () => {
    let answer: (value: unknown) => void = () => {}
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      path === '/me'
        ? new Promise((resolve) => {
            answer = resolve
          })
        : Promise.resolve({ object_store: {} }),
    )

    const pending = loadSession()
    clearSession()
    answer(owner)
    await pending

    expect(me.value).toBeNull()
    expect(csrfToken.value).toBeUndefined()
    expect(status.value).toBeNull()
  })

  it('does not write a late /status answer after a clear', async () => {
    let answer: (value: unknown) => void = () => {}
    vi.mocked(apiClient.get).mockImplementation((path: string) =>
      path === '/me'
        ? Promise.resolve(owner)
        : new Promise((resolve) => {
            answer = resolve
          }),
    )

    const pending = loadSession()
    await vi.waitFor(() => expect(me.value).not.toBeNull())
    clearSession()
    answer({ object_store: {} })
    await pending

    expect(status.value).toBeNull()
  })

  it('marks a failed /me and resets the mark on a clear', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(
      new ApiError({ code: ErrorCode.unavailable, message: 'down', status: 503 }),
    )

    await loadSession()
    expect(sessionFailed.value).toBe(true)
    expect(me.value).toBeNull()

    clearSession()
    expect(sessionFailed.value).toBe(false)
  })
})
